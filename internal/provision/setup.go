// Package provision implements declarative setup command execution.
// REQ-009-007: Setup command execution.
// REQ-009-013: Failure handling.
package provision

import (
	"context"
	"fmt"
)

// RunSetupCommands executes setup commands sequentially inside the VM as
// the default non-root user with set -eu -o pipefail.
// REQ-009-007: Sequential, non-root, fail-fast.
// REQ-009-013: Fail-fast with command text + exit code in error.
func RunSetupCommands(ctx context.Context, execFn ExecFunc, vmName string, commands []string) error {
	for _, command := range commands {
		script := "set -eu -o pipefail\n" + command
		cmd := []string{"bash", "-c", script}
		_, stderr, exitCode, err := execFn(ctx, vmName, cmd)
		if err != nil {
			return fmt.Errorf("setup command failed: %q: %v", command, err)
		}
		if exitCode != 0 {
			return fmt.Errorf("setup command failed (exit %d): %q\n%s",
				exitCode, command, tailLines(stderr, 20))
		}
	}
	return nil
}
