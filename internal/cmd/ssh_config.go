// Package cmd implements the ssh-config command.
// REQ-007-006: SSH Config Print Command
package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"sd/internal/backend"
	"sd/internal/ui"
)

func init() {
	sshConfigCmd := &cobra.Command{
		Use:   "ssh-config <vm-name>",
		Short: "Print SSH config fragment for a VM",
		Long: `Print the SSH config fragment for a VM to stdout.
The output can be appended to ~/.ssh/config or piped to other commands.

Use --json to output the config fields as a structured JSON object.

Examples:
  sd ssh-config myvm              # Print SSH config fragment
  sd ssh-config myvm --json       # Output config as JSON`,
		GroupID: "connection",
		Args:    cobra.ExactArgs(1),
		RunE:    runSSHConfig,
	}

	rootCmd.AddCommand(sshConfigCmd)
}

// runSSHConfig executes the ssh-config command.
// REQ-007-006
func runSSHConfig(cmd *cobra.Command, args []string) error {
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

	sshCfg, err := b.SSHConfig(cmd.Context(), name)
	if err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		return ui.CLIError{
			Code:    "ssh_connection_failed",
			Message: fmt.Sprintf("failed to get SSH config for VM %q: %v", name, err),
		}
	}

	// REQ-007-006: JSON output
	if f.JSONMode() {
		type sshConfigResult struct {
			Host         string `json:"host"`
			HostName     string `json:"hostname"`
			Port         int    `json:"port"`
			User         string `json:"user"`
			IdentityFile string `json:"identity_file"`
			ProxyCommand string `json:"proxy_command,omitempty"`
			Transport    string `json:"transport"`
		}
		result := sshConfigResult{
			Host:         "sd-" + name,
			HostName:     sshCfg.Host,
			Port:         sshCfg.Port,
			User:         sshCfg.User,
			IdentityFile: sshCfg.IdentityFile,
			ProxyCommand: sshCfg.ProxyCommand,
			Transport:    sshCfg.Transport,
		}
		f.SuccessData(result, nil)
	} else {
		// REQ-007-006: Print SSH config fragment to stdout
		f.SuccessData(nil, func() string {
			return formatSSHConfigFragment(name, sshCfg)
		})
	}

	return nil
}

// formatSSHConfigFragment produces an SSH config fragment for a VM.
func formatSSHConfigFragment(vmName string, cfg backend.SSHConfig) string {
	host := "sd-" + vmName
	user := cfg.User
	if user == "" {
		user = "dev"
	}

	var lines []string
	lines = append(lines, "# Managed by sd. Do not edit manually.")
	lines = append(lines, fmt.Sprintf("Host %s", host))

	if cfg.ProxyCommand != "" {
		// VSOCK transport
		lines = append(lines, fmt.Sprintf("    User %s", user))
		lines = append(lines, fmt.Sprintf("    IdentityFile %s", cfg.IdentityFile))
		lines = append(lines, fmt.Sprintf("    ProxyCommand %s", cfg.ProxyCommand))
		lines = append(lines, "    StrictHostKeyChecking no")
		lines = append(lines, "    UserKnownHostsFile /dev/null")
	} else {
		// TCP transport
		hostname := cfg.Host
		if hostname == "" {
			hostname = "127.0.0.1"
		}
		lines = append(lines, fmt.Sprintf("    HostName %s", hostname))
		lines = append(lines, fmt.Sprintf("    Port %d", cfg.Port))
		lines = append(lines, fmt.Sprintf("    User %s", user))
		lines = append(lines, fmt.Sprintf("    IdentityFile %s", cfg.IdentityFile))
		lines = append(lines, "    StrictHostKeyChecking yes")
		lines = append(lines, fmt.Sprintf("    UserKnownHostsFile ~/.sd/vms/%s/ssh/known_hosts", vmName))
	}

	lines = append(lines, "    ForwardAgent no")
	lines = append(lines, "    ForwardX11 no")
	lines = append(lines, "    LogLevel ERROR")
	lines = append(lines, "    SendEnv SD_* ANTHROPIC_* GITHUB_* GH_*")

	// Join with newlines and add trailing newline
	result := ""
	for _, line := range lines {
		result += line + "\n"
	}
	return result
}
