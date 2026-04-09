// Package cmd provides tests for the custom argument validators.
package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExactArgs_AcceptsCorrectCount(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	validator := exactArgs(1, "<name>")
	err := validator(cmd, []string{"myvm"})
	assert.NoError(t, err)
}

func TestExactArgs_RejectsZeroArgs(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	validator := exactArgs(1, "<name>")
	err := validator(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument(s)")
	assert.Contains(t, err.Error(), "<name>")
}

func TestExactArgs_RejectsTooManyArgs(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	validator := exactArgs(1, "<name>")
	err := validator(cmd, []string{"a", "b"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument(s)")
}

func TestExactArgs_TwoArgs(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	validator := exactArgs(2, "<key> <value>")

	// Accepts 2
	err := validator(cmd, []string{"k", "v"})
	assert.NoError(t, err)

	// Rejects 0
	err = validator(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "<key> <value>")

	// Rejects 1
	err = validator(cmd, []string{"k"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument(s)")

	// Rejects 3
	err = validator(cmd, []string{"a", "b", "c"})
	require.Error(t, err)
}

func TestMinArgs_AcceptsExact(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	validator := minArgs(2, "<vm-name> -- <command> [args...]")
	err := validator(cmd, []string{"vm", "cmd"})
	assert.NoError(t, err)
}

func TestMinArgs_AcceptsMore(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	validator := minArgs(2, "<vm-name> -- <command> [args...]")
	err := validator(cmd, []string{"vm", "echo", "hello"})
	assert.NoError(t, err)
}

func TestMinArgs_RejectsFewerThanRequired(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	validator := minArgs(2, "<vm-name> -- <command> [args...]")

	// Zero args
	err := validator(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument(s)")
	assert.Contains(t, err.Error(), "<vm-name> -- <command> [args...]")

	// One arg
	err = validator(cmd, []string{"vm"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument(s)")
}

func TestRangeArgs_AcceptsMinimum(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	validator := rangeArgs(2, 3, "<vm> <path> [<dest>]")
	err := validator(cmd, []string{"vm", "/src"})
	assert.NoError(t, err)
}

func TestRangeArgs_AcceptsMaximum(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	validator := rangeArgs(2, 3, "<vm> <path> [<dest>]")
	err := validator(cmd, []string{"vm", "/src", "/dst"})
	assert.NoError(t, err)
}

func TestRangeArgs_RejectsBelowMinimum(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	validator := rangeArgs(2, 3, "<vm> <path> [<dest>]")

	err := validator(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument(s)")
	assert.Contains(t, err.Error(), "<vm> <path> [<dest>]")

	err = validator(cmd, []string{"vm"})
	require.Error(t, err)
}

func TestRangeArgs_RejectsAboveMaximum(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	validator := rangeArgs(2, 3, "<vm> <path> [<dest>]")
	err := validator(cmd, []string{"a", "b", "c", "d"})
	require.Error(t, err)
}

// TestExactArgs_ErrorIncludesCommandPath verifies the error message
// includes the full command path for nested commands.
func TestExactArgs_ErrorIncludesCommandPath(t *testing.T) {
	parent := &cobra.Command{Use: "sd"}
	child := &cobra.Command{Use: "create"}
	parent.AddCommand(child)

	validator := exactArgs(1, "<name> [flags]")
	err := validator(child, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sd create")
	assert.Contains(t, err.Error(), "<name> [flags]")
}

// Test that real commands produce actionable error messages.

func TestCreateCommand_ActionableError(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"create"})
	require.NoError(t, err)

	// REQ-005-021: 0 args is accepted at args layer (name comes from .sd.yaml)
	err = cmd.Args(cmd, nil)
	assert.NoError(t, err, "create accepts 0 args since .sd.yaml may provide name")

	// 2 args is still rejected
	err = cmd.Args(cmd, []string{"a", "b"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument(s)")
}

func TestExecCommand_ActionableError(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"exec"})
	require.NoError(t, err)

	err = cmd.Args(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument(s)")
	assert.Contains(t, err.Error(), "<vm-name>")
	assert.Contains(t, err.Error(), "<command>")
}

func TestConfigSetCommand_ActionableError(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	// Navigate to config set
	configCmd, _, err := root.Find([]string{"config", "set"})
	require.NoError(t, err)

	err = configCmd.Args(configCmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument(s)")
	assert.Contains(t, err.Error(), "<key>")
	assert.Contains(t, err.Error(), "<value>")
}

func TestConfigGetCommand_ActionableError(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"config", "get"})
	require.NoError(t, err)

	err = cmd.Args(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument(s)")
	assert.Contains(t, err.Error(), "<key>")
}

func TestSnapshotCreateCommand_ActionableError(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"snapshot", "create"})
	require.NoError(t, err)

	err = cmd.Args(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument(s)")
	assert.Contains(t, err.Error(), "<vm>")
}

func TestSyncToCommand_ActionableError(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"sync", "to"})
	require.NoError(t, err)

	// Zero args should fail
	err = cmd.Args(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument(s)")
	assert.Contains(t, err.Error(), "<vm>")
	assert.Contains(t, err.Error(), "<host-path>")

	// Two args should succeed
	err = cmd.Args(cmd, []string{"vm", "/path"})
	assert.NoError(t, err)

	// Three args should succeed
	err = cmd.Args(cmd, []string{"vm", "/path", "/dest"})
	assert.NoError(t, err)
}
