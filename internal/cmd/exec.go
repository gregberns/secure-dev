// Package cmd implements the exec command.
// REQ-007-013: Exec Command
// REQ-007-014: Exec JSON Output
package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"sd/internal/backend"
	"sd/internal/ui"
)

// osExit is overridden in tests to prevent os.Exit from killing the test process.
var osExit = os.Exit

func init() {
	execCmd := &cobra.Command{
		Use:   "exec <vm-name> -- <command> [args...]",
		Short: "Run a command inside a VM",
		Long: `Execute a non-interactive command inside a VM and return the output.

The double dash (--) separates sd flags from the remote command and its arguments.
The exit code of sd exec matches the exit code of the remote command.

Examples:
  sd exec myvm -- echo hello
  sd exec --json myvm -- ls -la /tmp`,
		GroupID: "connection",
		Args:    cobra.MinimumNArgs(2),
		RunE:    runExec,
	}

	rootCmd.AddCommand(execCmd)
}

// runExec executes the exec command.
// REQ-007-013, REQ-007-014
func runExec(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	// Cobra consumes "--" as a flag terminator, so:
	//   "sd exec myvm -- echo hello" → args = ["myvm", "echo", "hello"]
	//   "sd exec --json myvm -- echo hello" → args = ["myvm", "echo", "hello"]
	// args[0] = VM name, args[1:] = command
	name := args[0]
	command := args[1:]

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

	// Check backend availability
	if err := b.Available(); err != nil {
		return ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("backend %q not available: %v", backendName, err),
		}
	}

	// REQ-007-013: Verify VM is running (no auto-start for exec)
	status, err := b.Status(cmd.Context(), name)
	if err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		return ui.CLIError{
			Code:    "exec_failed",
			Message: fmt.Sprintf("failed to check status of VM %q: %v", name, err),
		}
	}

	if status != backend.StatusRunning {
		return ui.CLIError{
			Code:    "vm_not_running",
			Message: fmt.Sprintf("VM %q is not running (status: %s). Start it with \"sd start %s\".", name, status, name),
		}
	}

	// Execute command inside VM
	result, err := b.Exec(cmd.Context(), name, command)
	if err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		if errors.Is(err, backend.ErrVMNotRunning) {
			return ui.CLIError{
				Code:    "vm_not_running",
				Message: fmt.Sprintf("VM %q is not running", name),
			}
		}
		return ui.CLIError{
			Code:    "exec_failed",
			Message: fmt.Sprintf("failed to execute command in VM %q: %v", name, err),
		}
	}

	// REQ-007-014: JSON output
	if f.JSONMode() {
		type execResult struct {
			ExitCode int    `json:"exit_code"`
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
		}
		r := execResult{
			ExitCode: result.ExitCode,
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
		}
		f.SuccessData(r, nil)
	} else {
		// REQ-007-013: Pass stdout/stderr through
		if result.Stdout != "" {
			fmt.Fprint(os.Stdout, result.Stdout)
		}
		if result.Stderr != "" {
			fmt.Fprint(os.Stderr, result.Stderr)
		}
	}

	// REQ-007-013: Exit code matches remote command's exit code
	if result.ExitCode != 0 {
		osExit(result.ExitCode)
	}

	return nil
}
