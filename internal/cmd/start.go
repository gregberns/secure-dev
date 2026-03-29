// Package cmd implements the start command.
// REQ-002-003: VM Management Commands -- start
package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"sd/internal/backend"
	"sd/internal/ui"
)

func init() {
	startCmd := &cobra.Command{
		Use:   "start <name>",
		Short: "Start a stopped VM",
		Long: `Start a stopped VM, transitioning it to Running state.

If the VM is already running, this command reports an error.`,
		GroupID: "vm",
		Args:    cobra.ExactArgs(1),
		RunE:    runStart,
	}

	rootCmd.AddCommand(startCmd)
}

// runStart executes the start command.
// REQ-002-003
func runStart(cmd *cobra.Command, args []string) error {
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

	// Check current status before starting
	status, err := b.Status(cmd.Context(), name)
	if err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		return ui.CLIError{
			Code:    "vm_start_failed",
			Message: fmt.Sprintf("failed to check status of VM %q: %v", name, err),
		}
	}

	if status == backend.StatusRunning {
		return ui.CLIError{
			Code:    "vm_already_running",
			Message: fmt.Sprintf("VM %q is already running", name),
		}
	}

	f.Progress(fmt.Sprintf("Starting VM %q...", name))

	if err := b.Start(cmd.Context(), name); err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		if errors.Is(err, backend.ErrVMNotRunning) {
			// Should not happen after status check, but handle defensively
			return ui.CLIError{
				Code:    "vm_not_running",
				Message: fmt.Sprintf("VM %q cannot be started: %v", name, err),
			}
		}
		return ui.CLIError{
			Code:    "vm_start_failed",
			Message: fmt.Sprintf("failed to start VM %q: %v", name, err),
		}
	}

	type startResult struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}

	result := startResult{Name: name, Status: string(backend.StatusRunning)}

	f.SuccessData(result, func() string {
		return fmt.Sprintf("VM %q started.\n", name)
	})
	return nil
}
