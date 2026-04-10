// Package cmd implements the exec command.
// REQ-007-013: Exec Command
// REQ-007-014: Exec JSON Output
// REQ-007-019: Environment Injection on Exec
package cmd

import (
	"errors"
	"fmt"
	"os"
	"sort"

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
		Args:    minArgs(2, "<vm-name> -- <command> [args...]"),
		RunE:    runExec,
	}

	rootCmd.AddCommand(execCmd)
}

// buildEnvCommand wraps a command with credential environment variables using
// the env(1) utility. If creds is nil or empty, the original command is returned
// unchanged. The env vars are sorted for deterministic output.
// REQ-007-019
func buildEnvCommand(command []string, creds map[string]string) []string {
	if len(creds) == 0 {
		return command
	}

	// Sort keys for deterministic ordering in tests and logs.
	keys := make([]string, 0, len(creds))
	for k := range creds {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build: env KEY1=val1 KEY2=val2 <original-command> [args...]
	wrapped := make([]string, 0, 1+len(creds)+len(command))
	wrapped = append(wrapped, "env")
	for _, k := range keys {
		wrapped = append(wrapped, k+"="+creds[k])
	}
	wrapped = append(wrapped, command...)
	return wrapped
}

// runExec executes the exec command.
// REQ-007-013, REQ-007-014, REQ-007-019
func runExec(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	// Cobra consumes "--" as a flag terminator, so:
	//   "sd exec myvm -- echo hello" -> args = ["myvm", "echo", "hello"]
	//   "sd exec --json myvm -- echo hello" -> args = ["myvm", "echo", "hello"]
	// args[0] = VM name, args[1:] = command
	name := args[0]
	command := args[1:]

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

	// Resolve backend from per-VM config
	backendName := resolveBackendName(name)

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

	// REQ-007-019: Load credentials from VM config for env injection.
	var creds map[string]string
	if l := Loader(); l != nil {
		env, err := readCredFunc(l.SDHome(), name)
		if err == nil && len(env) > 0 {
			creds = make(map[string]string, len(env))
			for k, v := range env {
				if isCredentialKey(k) {
					creds[k] = v
				}
			}
		}
	}

	// REQ-007-019: Wrap command with env vars for credential injection.
	// Uses env(1) to prepend credentials: env KEY=val <command> [args...]
	// This preserves the backend abstraction — each backend (Lima, Docker, etc.)
	// executes via its native mechanism (limactl shell, docker exec, etc.).
	execCommand := buildEnvCommand(command, creds)

	// Execute command inside VM via the backend interface.
	result, err := b.Exec(cmd.Context(), name, execCommand)
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
