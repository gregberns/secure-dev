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
		Use:   "ensure <name>",
		Short: "Ensure a VM exists and is running",
		Long: `Ensure a VM exists and is running. Creates the VM if it doesn't exist,
starts it if stopped, and is a no-op if already running.

This is the idempotent command agents should use -- no need for
check-then-branch logic around create/start.

When the VM already exists, flags like --cpus, --memory, --modules are
ignored. ensure guarantees existence and running state, not configuration.`,
		GroupID: "vm",
		Aliases: []string{"up"},
		Args:    exactArgs(1, "<name> [flags]"),
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
func runEnsure(cmd *cobra.Command, args []string) error {
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

	// REQ-001-006: validate VM name format
	if err := backend.ValidateVMName(name); err != nil {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: err.Error(),
		}
	}

	// Resolve backend name: flag > per-VM config > global default
	backendName, _ := cmd.Flags().GetString("backend")
	if backendName == "" {
		backendName = resolveBackendName(name)
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

	// Build VMConfig from flags and config defaults
	vmCfg := buildVMConfig(cmd, backendName)

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

	modulesFlag, _ := cmd.Flags().GetStringSlice("modules")
	if err := doCreateVM(cmd.Context(), f, b, name, backendName, vmCfg, modulesFlag); err != nil {
		return err
	}

	result := ensureResult{
		Name:    name,
		Action:  "created",
		Backend: backendName,
		Status:  string(backend.StatusRunning),
	}

	f.SuccessData(result, func() string {
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

	f.SuccessData(result, func() string {
		return fmt.Sprintf("VM %q started.\n", name)
	})
	return nil
}
