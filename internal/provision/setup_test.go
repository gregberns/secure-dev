// Package provision provides tests for setup command execution.
// REQ-009-007: Setup command execution.
// REQ-009-013: Failure handling.
package provision

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunSetupCommands_Empty(t *testing.T) {
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		t.Fatal("execFn should not be called for empty commands")
		return "", "", 0, nil
	}
	err := RunSetupCommands(context.Background(), execFn, "test-vm", nil)
	assert.NoError(t, err)
}

func TestRunSetupCommands_Sequential(t *testing.T) {
	var order []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		script := command[len(command)-1]
		// Extract the user command after the preamble
		lines := strings.Split(script, "\n")
		if len(lines) > 1 {
			order = append(order, lines[1])
		}
		return "", "", 0, nil
	}

	commands := []string{"echo first", "echo second", "echo third"}
	err := RunSetupCommands(context.Background(), execFn, "test-vm", commands)
	require.NoError(t, err)
	assert.Equal(t, []string{"echo first", "echo second", "echo third"}, order)
}

func TestRunSetupCommands_HasPreamble(t *testing.T) {
	var scripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		scripts = append(scripts, command[len(command)-1])
		return "", "", 0, nil
	}

	err := RunSetupCommands(context.Background(), execFn, "test-vm", []string{"echo hi"})
	require.NoError(t, err)
	require.Len(t, scripts, 1)
	assert.True(t, strings.HasPrefix(scripts[0], "set -eu -o pipefail\n"))
}

func TestRunSetupCommands_RunsAsUser(t *testing.T) {
	// Verify commands use "bash -c" not "sudo bash -c".
	var commands [][]string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		commands = append(commands, command)
		return "", "", 0, nil
	}

	err := RunSetupCommands(context.Background(), execFn, "test-vm", []string{"echo hi"})
	require.NoError(t, err)
	require.Len(t, commands, 1)
	assert.Equal(t, "bash", commands[0][0])
	assert.Equal(t, "-c", commands[0][1])
}

func TestRunSetupCommands_FailFast(t *testing.T) {
	callCount := 0
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		callCount++
		if callCount == 2 {
			return "", "error output", 42, nil
		}
		return "", "", 0, nil
	}

	commands := []string{"cmd1", "cmd2-fails", "cmd3-should-not-run"}
	err := RunSetupCommands(context.Background(), execFn, "test-vm", commands)
	require.Error(t, err)
	assert.Equal(t, 2, callCount, "third command should not run after second fails")
}

func TestRunSetupCommands_ErrorIncludesCommandAndExitCode(t *testing.T) {
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		return "", "some error output", 42, nil
	}

	err := RunSetupCommands(context.Background(), execFn, "test-vm", []string{"bad command"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad command")
	assert.Contains(t, err.Error(), "exit 42")
}

func TestRunSetupCommands_ExecError(t *testing.T) {
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		return "", "", 0, fmt.Errorf("connection lost")
	}

	err := RunSetupCommands(context.Background(), execFn, "test-vm", []string{"echo hi"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection lost")
}

func TestRunSetupCommands_ErrorIncludesLast20Lines(t *testing.T) {
	var longOutput strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&longOutput, "line %d\n", i)
	}
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		return "", longOutput.String(), 1, nil
	}

	err := RunSetupCommands(context.Background(), execFn, "test-vm", []string{"bad cmd"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "line 0\n")
	assert.Contains(t, err.Error(), "line 29")
}
