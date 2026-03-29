// Package cmd provides unit tests for the CLI root command and infrastructure.
// REQ-002-001: Entry point
// REQ-002-010: Global flags
// REQ-002-015: PersistentPreRun
// REQ-002-019: VM name resolution
package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRootTestEnv sets up an isolated environment for root command tests.
func newRootTestEnv(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("SD_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)
	return tmpDir
}

func TestRootCommand_HelpNoArgs(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	root.SetArgs([]string{})
	err := root.Execute()
	assert.NoError(t, err)
}

func TestRootCommand_ShortDescription(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	assert.Equal(t, "Manage secure VM environments for AI coding agents", root.Short)
}

func TestRootCommand_GlobalFlags(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()

	// REQ-002-010: All global flags must be registered
	jsonFlag, err := root.PersistentFlags().GetBool("json")
	assert.NoError(t, err)
	assert.False(t, jsonFlag)

	verboseFlag, err := root.PersistentFlags().GetBool("verbose")
	assert.NoError(t, err)
	assert.False(t, verboseFlag)

	quietFlag, err := root.PersistentFlags().GetBool("quiet")
	assert.NoError(t, err)
	assert.False(t, quietFlag)

	configFlag, err := root.PersistentFlags().GetString("config")
	assert.NoError(t, err)
	assert.Empty(t, configFlag)

	vmFlag, err := root.PersistentFlags().GetString("vm")
	assert.NoError(t, err)
	assert.Empty(t, vmFlag)

	// Verify short flags
	assert.NotNil(t, root.PersistentFlags().ShorthandLookup("v"))
	assert.NotNil(t, root.PersistentFlags().ShorthandLookup("q"))
}

func TestRootCommand_CommandGroups(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()

	groupIDs := make(map[string]string)
	for _, g := range root.Groups() {
		groupIDs[g.ID] = g.Title
	}

	// REQ-002-002: Required command groups
	assert.Contains(t, groupIDs, "vm")
	assert.Contains(t, groupIDs, "connection")
	assert.Contains(t, groupIDs, "config")
	assert.Contains(t, groupIDs, "provisioning")
	assert.Contains(t, groupIDs, "security")
	assert.Contains(t, groupIDs, "diagnostics")
	assert.Contains(t, groupIDs, "utility")

	assert.Equal(t, "VM Management", groupIDs["vm"])
	assert.Equal(t, "Connection", groupIDs["connection"])
	assert.Equal(t, "Configuration", groupIDs["config"])
}

// newCmdWithVMFlag creates a fresh command with a --vm flag for isolated testing.
// Uses the same flag registration pattern as the root command.
func newCmdWithVMFlag(vmValue string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("vm", "", "Default VM name")
	if vmValue != "" {
		_ = cmd.Flags().Set("vm", vmValue)
	}
	return cmd
}

func TestResolveVMName_PositionalArg(t *testing.T) {
	newRootTestEnv(t)

	cmd := newCmdWithVMFlag("")
	name, err := resolveVMName(cmd, []string{"myvm"})
	assert.NoError(t, err)
	assert.Equal(t, "myvm", name)
}

func TestResolveVMName_VMFlag(t *testing.T) {
	newRootTestEnv(t)

	cmd := newCmdWithVMFlag("flagvm")
	name, err := resolveVMName(cmd, nil)
	assert.NoError(t, err)
	assert.Equal(t, "flagvm", name)
}

func TestResolveVMName_MissingAll(t *testing.T) {
	newRootTestEnv(t)

	cmd := newCmdWithVMFlag("")
	name, err := resolveVMName(cmd, nil)
	assert.Error(t, err)
	assert.Empty(t, name)
	assert.Contains(t, err.Error(), "no VM specified")
}

func TestResolveVMName_PositionalOverridesFlag(t *testing.T) {
	newRootTestEnv(t)

	cmd := newCmdWithVMFlag("flagvm")
	name, err := resolveVMName(cmd, []string{"argvm"})
	assert.NoError(t, err)
	assert.Equal(t, "argvm", name)
}

func TestVerboseQuietMutualExclusivity(t *testing.T) {
	newRootTestEnv(t)

	// Test PersistentPreRunE directly with a subcommand that triggers it
	root := RootCmd()
	var preRunErr error
	testCmd := &cobra.Command{
		Use: "test-mutex",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	root.AddCommand(testCmd)

	// Test that --verbose --quiet triggers an error via PersistentPreRunE
	root.SetArgs([]string{"--verbose", "--quiet", "test-mutex"})
	err := root.Execute()
	assert.Error(t, err, "--verbose and --quiet together must fail")
	_ = preRunErr

	// Reset flags for other tests
	_ = root.PersistentFlags().Set("verbose", "false")
	_ = root.PersistentFlags().Set("quiet", "false")
}

func TestConfigFile_LoadFromCustomPath(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SD_HOME", tmpDir)

	// Create a minimal config file
	configContent := "defaults:\n  backend: test-backend\n"
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	root := RootCmd()
	// Add a dummy subcommand to trigger PersistentPreRunE
	testCmd := &cobra.Command{
		Use: "test-config",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	root.AddCommand(testCmd)
	root.SetArgs([]string{"--config", tmpDir, "test-config"})
	err = root.Execute()
	assert.NoError(t, err)

	// Reset flags
	_ = root.PersistentFlags().Set("config", "")
}

func TestPrefixMatchingEnabled(t *testing.T) {
	newRootTestEnv(t)

	// REQ-002-014: Prefix matching should be enabled globally
	assert.True(t, cobra.EnablePrefixMatching)
}

func TestVersionCommandRegistered(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	root.SetArgs([]string{"version"})
	err := root.Execute()
	assert.NoError(t, err, "version command should be registered")
}

func TestRootEntryDoesNotPanic(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	root.SetArgs([]string{"--help"})
	// Just ensure it doesn't panic
	_ = root.Execute()
}

func TestResolveVMName_ErrorMessage(t *testing.T) {
	newRootTestEnv(t)

	cmd := newCmdWithVMFlag("")
	_, err := resolveVMName(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no VM specified")
	assert.Contains(t, err.Error(), "--vm")
	assert.Contains(t, err.Error(), "defaults.vm")
}

// Property: positional arg always wins over flag
func TestProperty_PositionalAlwaysWins(t *testing.T) {
	newRootTestEnv(t)

	flagValues := []string{"flag-a", "flag-b", "", "x"}
	posValues := []string{"pos-a", "pos-b", "x"}

	for _, flag := range flagValues {
		for _, pos := range posValues {
			cmd := newCmdWithVMFlag(flag)
			name, err := resolveVMName(cmd, []string{pos})
			assert.NoError(t, err)
			assert.Equal(t, pos, name,
				"positional %q should override flag %q", pos, flag)
		}
	}
}
