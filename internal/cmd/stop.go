// Package cmd implements the stop command.
// REQ-002-003: VM Management Commands -- stop
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
	stopCmd := &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop a running VM",
		Long: `Stop a running VM, transitioning it to Stopped state.

If the VM is already stopped, this command succeeds silently.`,
		GroupID: "vm",
		Args:    exactArgs(1, "<name>"),
		RunE:    runStop,
	}

	rootCmd.AddCommand(stopCmd)
}

// runStop executes the stop command.
// REQ-002-003
func runStop(cmd *cobra.Command, args []string) error {
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

	// Resolve backend name: config > default ("lima")
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

	// Check backend availability
	if err := b.Available(); err != nil {
		return ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("backend %q not available: %v", backendName, err),
		}
	}

	// Check current status before stopping
	status, err := b.Status(cmd.Context(), name)
	if err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		return ui.CLIError{
			Code:    "vm_stop_failed",
			Message: fmt.Sprintf("failed to check status of VM %q: %v", name, err),
		}
	}

	// REQ-003-003: Stop on already-stopped VM is a no-op (silent success)
	if status == backend.StatusStopped {
		// REQ-004-022: Log VM lifecycle event (already stopped)
		if al := AuditLog(); al != nil {
			_ = al.LogEvent(security.EventLogEntry{
				Timestamp: time.Now(),
				EventType: "vm.stop",
				VMName:    name,
			})
		}
		type stopResult struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		}
		result := stopResult{Name: name, Status: string(backend.StatusStopped)}
		f.SuccessData(result, func() string { return "" })
		return nil
	}

	f.Progress(fmt.Sprintf("Stopping VM %q...", name))

	if err := b.Stop(cmd.Context(), name); err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		return ui.CLIError{
			Code:    "vm_stop_failed",
			Message: fmt.Sprintf("failed to stop VM %q: %v", name, err),
		}
	}

	// REQ-005-007: Track VM state
	if l := Loader(); l != nil {
		_ = l.UpdateVMState(name, func(s *config.VMState) {
			s.Status = config.VMStatusStopped
			s.LastStopped = time.Now()
		})
	}

	// REQ-004-022: Log VM lifecycle event
	if al := AuditLog(); al != nil {
		_ = al.LogEvent(security.EventLogEntry{
			Timestamp: time.Now(),
			EventType: "vm.stop",
			VMName:    name,
		})
	}

	type stopResult struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}

	result := stopResult{Name: name, Status: string(backend.StatusStopped)}

	f.SuccessData(result, func() string {
		return fmt.Sprintf("VM %q stopped.\n", name)
	})
	return nil
}
