// Package cmd implements the connect command.
// REQ-007-001: Connect Command
// REQ-007-002: Auto-Start on Connect
// REQ-007-007: Port Forwarding on Connect
// REQ-007-008: tmux as Default Session Manager
// REQ-007-009: Named tmux Sessions
// REQ-007-010: New tmux Window
// REQ-007-012: Raw SSH Without tmux
package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"sd/internal/backend"
	"sd/internal/ui"
)

// sessionNamePattern validates tmux session names: alphanumeric, hyphens, underscores only.
// REQ-007-009
var sessionNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// sshRunner is a function that runs an SSH command. Overridden in tests.
var sshRunner = defaultSSHRunner

func defaultSSHRunner(name string, args []string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func init() {
	connectCmd := &cobra.Command{
		Use:     "connect <vm-name>",
		Aliases: []string{"c"},
		Short:   "Connect to a VM via SSH with tmux",
		Long: `Connect to a VM by establishing an SSH session and attaching to a tmux session.
If the VM is stopped, it is automatically started first (use --no-start to prevent this).

Examples:
  sd connect myvm               # Connect with default tmux session
  sd c myvm                     # Same, using alias
  sd connect myvm --no-tmux     # Raw SSH, no tmux
  sd connect myvm --session work  # Named tmux session
  sd connect myvm --new-window  # New window in existing session
  sd connect myvm --forward 8080:8080  # With port forwarding`,
		GroupID: "connection",
		Args:    cobra.ExactArgs(1),
		RunE:    runConnect,
	}

	connectCmd.Flags().Bool("no-start", false, "Do not auto-start a stopped VM")
	connectCmd.Flags().String("session", "", "tmux session name (default: sd-<vm-name>)")
	connectCmd.Flags().Bool("new-window", false, "Create a new tmux window in the session")
	connectCmd.Flags().Bool("no-tmux", false, "Raw SSH session without tmux")
	connectCmd.Flags().StringArray("forward", nil, "Port forwarding spec: <host-port>:<guest-port> or <bind-addr>:<host-port>:<guest-port>")

	rootCmd.AddCommand(connectCmd)
}

// runConnect executes the connect command.
// REQ-007-001, REQ-007-002
func runConnect(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	name := args[0]
	if name == "" {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: "VM name must not be empty",
		}
	}

	// REQ-007-012: --no-tmux and --new-window are mutually exclusive
	noTmux, _ := cmd.Flags().GetBool("no-tmux")
	newWindow, _ := cmd.Flags().GetBool("new-window")
	if noTmux && newWindow {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: "--no-tmux and --new-window are mutually exclusive",
		}
	}

	// REQ-007-009: Validate session name
	sessionName, _ := cmd.Flags().GetString("session")
	if sessionName != "" && !sessionNamePattern.MatchString(sessionName) {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: fmt.Sprintf("session name %q is invalid. Use only alphanumeric characters, hyphens, and underscores.", sessionName),
		}
	}

	// Default session name: sd-<vm-name>
	if sessionName == "" {
		sessionName = "sd-" + name
	}

	// REQ-007-007: Parse port forwarding specs
	forwardSpecs, _ := cmd.Flags().GetStringArray("forward")
	forwards, err := parsePortForwards(forwardSpecs)
	if err != nil {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: err.Error(),
		}
	}

	// Resolve backend
	backendName := ""
	if Loader() != nil {
		cfg := Loader().Get()
		backendName = cfg.Defaults.Backend
	}
	if backendName == "" {
		backendName = "lima"
	}

	b, err := getBackendFunc(backendName)
	if err != nil {
		return ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("backend %q is not available: %v", backendName, err),
		}
	}

	if err := b.Available(); err != nil {
		return ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("backend %q not available: %v", backendName, err),
		}
	}

	// Check current VM status
	status, err := b.Status(cmd.Context(), name)
	if err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist. Run \"sd list\" to see available VMs.", name),
			}
		}
		return ui.CLIError{
			Code:    "ssh_connection_failed",
			Message: fmt.Sprintf("failed to check status of VM %q: %v", name, err),
		}
	}

	noStart, _ := cmd.Flags().GetBool("no-start")

	// REQ-007-002: Auto-start stopped VMs
	if status != backend.StatusRunning {
		if status == backend.StatusStopped {
			if noStart {
				return ui.CLIError{
					Code:    "vm_not_running",
					Message: fmt.Sprintf("VM %q is stopped. Start it with \"sd start %s\" or connect without --no-start.", name, name),
				}
			}
			f.Progress(fmt.Sprintf("Auto-starting VM %q...", name))
			if err := b.Start(cmd.Context(), name); err != nil {
				return ui.CLIError{
					Code:    "vm_start_failed",
					Message: fmt.Sprintf("failed to start VM %q: %v. Check \"sd status %s\" for details.", name, err, name),
				}
			}
		} else {
			// Creating or Error status
			return ui.CLIError{
				Code:    "vm_not_running",
				Message: fmt.Sprintf("VM %q is not running (status: %s). Check \"sd status %s\" for details.", name, status, name),
			}
		}
	}

	// Get SSH config for the VM
	sshCfg, err := b.SSHConfig(cmd.Context(), name)
	if err != nil {
		return ui.CLIError{
			Code:    "ssh_connection_failed",
			Message: fmt.Sprintf("failed to get SSH config for VM %q: %v", name, err),
		}
	}

	// Build SSH command arguments
	sshArgs := buildSSHArgs(sshCfg, name, sessionName, noTmux, newWindow, forwards)

	// JSON output: report connection info and exit (interactive session can't produce JSON)
	if f.JSONMode() {
		type connectResult struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			Session string `json:"session"`
			Tmux    bool   `json:"tmux"`
		}
		result := connectResult{
			Name:    name,
			Status:  string(backend.StatusRunning),
			Session: sessionName,
			Tmux:    !noTmux,
		}
		f.SuccessData(result, nil)
		return nil
	}

	// Run the SSH command (interactive session)
	if err := sshRunner("ssh", sshArgs); err != nil {
		// Distinguish between SSH errors and exit codes from the remote session
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Remote command exited with non-zero -- not a connection error
			return nil
		}
		return ui.CLIError{
			Code:    "ssh_connection_failed",
			Message: formatSSHError(err, name, sshCfg),
		}
	}

	return nil
}

// parsePortForwards parses port forwarding specifications.
// REQ-007-007
func parsePortForwards(specs []string) ([]portForward, error) {
	var forwards []portForward
	for _, spec := range specs {
		if spec == "" {
			continue
		}
		parts := strings.Split(spec, ":")
		var pf portForward
		switch len(parts) {
		case 2:
			// <host-port>:<guest-port>
			if _, err := fmt.Sscanf(parts[0], "%d", &pf.HostPort); err != nil {
				return nil, fmt.Errorf("invalid port forwarding spec %q: expected <host-port>:<guest-port>", spec)
			}
			if _, err := fmt.Sscanf(parts[1], "%d", &pf.GuestPort); err != nil {
				return nil, fmt.Errorf("invalid port forwarding spec %q: expected <host-port>:<guest-port>", spec)
			}
			pf.BindAddr = "127.0.0.1"
		case 3:
			// <bind-addr>:<host-port>:<guest-port>
			pf.BindAddr = parts[0]
			if _, err := fmt.Sscanf(parts[1], "%d", &pf.HostPort); err != nil {
				return nil, fmt.Errorf("invalid port forwarding spec %q: expected <bind-addr>:<host-port>:<guest-port>", spec)
			}
			if _, err := fmt.Sscanf(parts[2], "%d", &pf.GuestPort); err != nil {
				return nil, fmt.Errorf("invalid port forwarding spec %q: expected <bind-addr>:<host-port>:<guest-port>", spec)
			}
		default:
			return nil, fmt.Errorf("invalid port forwarding spec %q: use <host-port>:<guest-port> or <bind-addr>:<host-port>:<guest-port>", spec)
		}
		if pf.HostPort <= 0 || pf.HostPort > 65535 {
			return nil, fmt.Errorf("invalid host port %d in spec %q: must be 1-65535", pf.HostPort, spec)
		}
		if pf.GuestPort <= 0 || pf.GuestPort > 65535 {
			return nil, fmt.Errorf("invalid guest port %d in spec %q: must be 1-65535", pf.GuestPort, spec)
		}
		forwards = append(forwards, pf)
	}
	return forwards, nil
}

// portForward represents a host-to-guest port mapping.
// REQ-007-007
type portForward struct {
	BindAddr  string
	HostPort  int
	GuestPort int
}

// buildSSHArgs constructs SSH command arguments for connecting to a VM.
func buildSSHArgs(cfg backend.SSHConfig, vmName, session string, noTmux, newWindow bool, forwards []portForward) []string {
	args := []string{}

	// Identity file
	if cfg.IdentityFile != "" {
		args = append(args, "-i", cfg.IdentityFile)
	}

	// ProxyCommand for VSOCK transport
	if cfg.ProxyCommand != "" {
		args = append(args, "-o", "ProxyCommand="+cfg.ProxyCommand)
	} else {
		// TCP transport
		args = append(args, "-o", "StrictHostKeyChecking=yes")
	}

	args = append(args, "-o", "ForwardAgent=no")
	args = append(args, "-o", "ForwardX11=no")
	args = append(args, "-o", "LogLevel=ERROR")

	// Port forwarding
	for _, fwd := range forwards {
		args = append(args, "-L", fmt.Sprintf("%s:%d:localhost:%d", fwd.BindAddr, fwd.HostPort, fwd.GuestPort))
	}

	// Host and user
	host := cfg.Host
	if host == "" {
		host = "127.0.0.1"
	}
	user := cfg.User
	if user == "" {
		user = "dev"
	}

	args = append(args, "-p", fmt.Sprintf("%d", cfg.Port))
	args = append(args, fmt.Sprintf("%s@%s", user, host))

	// tmux command
	if !noTmux {
		if newWindow {
			// REQ-007-010: New window in existing session
			args = append(args, "-t", session, "tmux", "new-window", "-t", session, "&&", "tmux", "attach", "-t", session)
		} else {
			// REQ-007-008: Attach or create tmux session
			args = append(args, "tmux", "new-session", "-A", "-s", session)
		}
	}

	return args
}

// formatSSHError produces an actionable error message for SSH failures.
// REQ-007-021
func formatSSHError(err error, vmName string, cfg backend.SSHConfig) string {
	errMsg := err.Error()
	switch {
	case strings.Contains(errMsg, "connection refused"):
		return fmt.Sprintf("Connection refused to VM %q (%s:%d). Is the VM running? Try \"sd status %s\".", vmName, cfg.Host, cfg.Port, vmName)
	case strings.Contains(errMsg, "permission denied") || strings.Contains(errMsg, "authentication"):
		return fmt.Sprintf("Authentication failed for VM %q using key %s. Try \"sd destroy %s && sd create %s\" to recreate.", vmName, cfg.IdentityFile, vmName, vmName)
	case strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "timed out"):
		return fmt.Sprintf("Connection to VM %q timed out. Check VM status with \"sd status %s\".", vmName, vmName)
	default:
		return fmt.Sprintf("Failed to connect to VM %q: %v", vmName, err)
	}
}
