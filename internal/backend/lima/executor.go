// Package lima provides the executor abstraction for running limactl commands.
// REQ-003-014: Lima Backend — Default Implementation
package lima

import (
	"fmt"
	"os/exec"
	"strings"

	"sd/internal/backend"
)

// CommandExecutor abstracts running external commands (limactl).
// This allows injecting a mock for testing without real Lima installation.
type CommandExecutor interface {
	// Run executes a command and returns stdout and stderr combined.
	// Returns an error if the command exits non-zero.
	Run(name string, args ...string) (string, error)
}

// realExecutor runs actual limactl commands via os/exec.
type realExecutor struct{}

func (e *realExecutor) Run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w", string(out), err)
	}
	return string(out), nil
}

// mockExecutor delegates to mocklimactl for testing.
type mockExecutor struct{}

func (e *mockExecutor) Run(name string, args ...string) (string, error) {
	if name != "limactl" {
		return "", fmt.Errorf("mockExecutor: unexpected command %q", name)
	}
	return MockRun(args)
}

// MockRun delegates to the mocklimactl package. Tests can replace this.
var MockRun = func(args []string) (string, error) {
	return "", fmt.Errorf("MockRun not initialized: %w", backend.ErrNotImplemented)
}

// parseVMList parses the tab-separated output of limactl list.
func parseVMList(output string) ([]string, error) {
	if output == "" {
		return []string{}, nil
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	names := make([]string, 0, len(lines))
	for _, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) > 0 && fields[0] != "" && fields[0] != "NAME" {
			names = append(names, fields[0])
		}
	}
	return names, nil
}
