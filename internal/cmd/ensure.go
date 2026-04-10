// Package cmd implements the ensure command.
// REQ-002-024: sd ensure <name> -- idempotent VM existence and running state
package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"sd/internal/backend"
	"sd/internal/config"
	"sd/internal/security"
	"sd/internal/ui"
)

func init() {
	ensureCmd := &cobra.Command{
		Use:   "ensure [name]",
		Short: "Ensure a VM exists and is running",
		Long: `Ensure a VM exists and is running. Creates the VM if it doesn't exist,
starts it if stopped, and is a no-op if already running.

This is the idempotent command agents should use -- no need for
check-then-branch logic around create/start.

When no name is given, reads .sd.yaml from the current directory (or parent
directories) for project configuration. This is the primary use case:
  cd ~/code/my-app && sd ensure

When the VM already exists, flags like --cpus, --memory, --modules are
ignored. ensure guarantees existence and running state, not configuration.`,
		GroupID: "vm",
		Aliases: []string{"up"},
		Args:    rangeArgs(0, 1, "[name] [flags]"),
		RunE:    runEnsure,
	}

	// REQ-002-025: Accept all create flags
	ensureCmd.Flags().String("backend", "", "VM backend to use (default: lima)")
	ensureCmd.Flags().Int("cpus", 0, "number of CPUs (default: 4)")
	ensureCmd.Flags().String("memory", "", "memory allocation, e.g. 4GiB (default: 8GiB)")
	ensureCmd.Flags().String("disk", "", "disk size, e.g. 50GiB (default: 100GiB)")
	ensureCmd.Flags().StringSlice("modules", nil, "provisioning modules to apply (comma-separated)")
	ensureCmd.Flags().StringArray("mount", nil, "mount host:guest[:ro|rw] (repeatable)")
	ensureCmd.Flags().StringArray("allow-egress", nil, "add domain to egress allowlist (repeatable)")

	rootCmd.AddCommand(ensureCmd)
}

// ensureProjCfg and ensureProjDir hold the project config discovered during
// name resolution, passed from runEnsure to ensureCreate.
var (
	ensureProjCfg *config.ProjectConfig
	ensureProjDir string
)

// ensureResult is the structured output for the ensure command.
// REQ-002-026
type ensureResult struct {
	Name    string `json:"name"`
	Action  string `json:"action"`  // "created", "started", "already_running"
	Backend string `json:"backend"`
	Status  string `json:"status"`  // always "running" on success
}

// runEnsure executes the ensure command.
// REQ-002-024: Creates VM if not found, starts if stopped, no-ops if running.
// REQ-002-027: When VM exists, does NOT re-provision or change config.
// REQ-005-021: When no name arg, read .sd.yaml from CWD.
func runEnsure(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	// REQ-005-021: When no positional arg, read .sd.yaml for project config
	name, projCfg, projDir, err := resolveCreateName(args)
	if err != nil {
		return err
	}

	// REQ-001-006: validate VM name format
	if err := backend.ValidateVMName(name); err != nil {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: err.Error(),
		}
	}

	// Resolve backend name: flag > .sd.yaml > per-VM config > global default
	// REQ-005-022: CLI flags override .sd.yaml values
	backendName, _ := cmd.Flags().GetString("backend")
	if backendName == "" && projCfg != nil && projCfg.Backend != "" {
		backendName = projCfg.Backend
	}
	if backendName == "" {
		backendName = resolveBackendName(name)
	}

	// Store project config for use by ensureCreate
	ensureProjCfg = projCfg
	ensureProjDir = projDir

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

	// Check current VM state
	status, err := b.Status(cmd.Context(), name)
	if err != nil && !errors.Is(err, backend.ErrVMNotFound) {
		return ui.CLIError{
			Code:    "vm_status_failed",
			Message: fmt.Sprintf("failed to check status of VM %q: %v", name, err),
		}
	}

	switch {
	case errors.Is(err, backend.ErrVMNotFound):
		// VM doesn't exist -- create it with all the flags
		return ensureCreate(cmd, f, b, name, backendName)

	case status == backend.StatusStopped:
		// VM exists but is stopped -- start it
		return ensureStart(cmd, f, b, name, backendName)

	case status == backend.StatusRunning:
		// VM already running -- no-op
		// REQ-002-027: Do not re-provision or change config
		result := ensureResult{
			Name:    name,
			Action:  "already_running",
			Backend: backendName,
			Status:  string(backend.StatusRunning),
		}
		f.SuccessData(result, func() string {
			return fmt.Sprintf("VM %q is already running.\n", name)
		})
		return nil

	default:
		// VM in unexpected state (creating, error, etc.) -- try starting it
		return ensureStart(cmd, f, b, name, backendName)
	}
}

// ensureCreate handles the "VM not found" branch: validate flags, create, start,
// provision, persist config, and audit.
func ensureCreate(cmd *cobra.Command, f *ui.Formatter, b backend.Backend, name, backendName string) error {
	// REQ-002-003: Validate resource flags before building config
	if err := validateCreateResources(cmd); err != nil {
		return err
	}

	// REQ-002-003: Validate backend name is registered
	if err := validateBackendFunc(backendName); err != nil {
		return err
	}

	// Build VMConfig from flags, project config, and config defaults
	// REQ-005-022: Precedence: CLI flags > .sd.yaml > user config > defaults
	vmCfg := buildVMConfig(cmd, backendName, ensureProjCfg, ensureProjDir)

	// Validate mount specs early
	for _, m := range vmCfg.Mounts {
		if m.HostPath == "" || m.GuestPath == "" {
			return ui.CLIError{
				Code:    "invalid_argument",
				Message: fmt.Sprintf("invalid mount spec: host and guest paths are required (got host=%q guest=%q)", m.HostPath, m.GuestPath),
			}
		}
	}

	f.Progress(fmt.Sprintf("Creating VM %q with backend %q...", name, backendName))

	if err := b.Create(cmd.Context(), name, vmCfg); err != nil {
		return ui.CLIError{
			Code:    "vm_create_failed",
			Message: fmt.Sprintf("failed to create VM %q: %v", name, err),
		}
	}

	modulesFlag := resolveModules(cmd, ensureProjCfg)
	if err := doCreateVM(cmd.Context(), f, b, name, backendName, vmCfg, modulesFlag, ensureProjCfg); err != nil {
		return err
	}

	result := ensureResult{
		Name:    name,
		Action:  "created",
		Backend: backendName,
		Status:  string(backend.StatusRunning),
	}

	// REQ-010-016: Contextual hints after VM creation via ensure
	hints := []string{
		"Export GITHUB_TOKEN on the host before running sd connect to inject credentials into the VM.",
		"Run sd config egress list to review which domains the VM can reach.",
	}

	f.SuccessDataWithHints(result, hints, func() string {
		return fmt.Sprintf("VM %q created and started.\n", name)
	})
	return nil
}

// ensureStart handles the "VM stopped" branch: start the VM and update state.
func ensureStart(cmd *cobra.Command, f *ui.Formatter, b backend.Backend, name, backendName string) error {
	f.Progress(fmt.Sprintf("Starting VM %q...", name))

	if err := b.Start(cmd.Context(), name); err != nil {
		return ui.CLIError{
			Code:    "vm_start_failed",
			Message: fmt.Sprintf("failed to start VM %q: %v", name, err),
		}
	}

	// REQ-005-007: Track VM state
	if l := Loader(); l != nil {
		_ = l.UpdateVMState(name, func(s *config.VMState) {
			s.Status = config.VMStatusRunning
			s.LastStarted = time.Now()
		})
	}

	// REQ-004-022: Log VM lifecycle event
	if al := AuditLog(); al != nil {
		_ = al.LogEvent(security.EventLogEntry{
			Timestamp: time.Now(),
			EventType: "vm.start",
			VMName:    name,
		})
	}

	result := ensureResult{
		Name:    name,
		Action:  "started",
		Backend: backendName,
		Status:  string(backend.StatusRunning),
	}

	// REQ-010-016: Contextual hints after starting a stopped VM
	hints := []string{
		"Export GITHUB_TOKEN on the host before running sd connect to inject credentials into the VM.",
	}

	f.SuccessDataWithHints(result, hints, func() string {
		return fmt.Sprintf("VM %q started.\n", name)
	})
	return nil
}
