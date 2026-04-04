// Package cmd provides tests for the completion command.
// REQ-002-017: Shell Completions
package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
	"sd/internal/backend"
	"sd/internal/ui"
)

// --- Unit Tests ---

func TestCompletionCommand_Registered(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "completion" {
			found = true
			assert.Equal(t, "utility", cmd.GroupID)
			assert.Equal(t, "Generate shell completion scripts", cmd.Short)
			break
		}
	}
	assert.True(t, found, "completion command must be registered")
}

func TestCompletionCommand_NoConfigRequired(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	root.SetArgs([]string{"completion", "bash"})
	err := root.Execute()
	assert.NoError(t, err)
}

func TestCompletionCommand_ExactArgs(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()

	// No args should fail
	root.SetArgs([]string{"completion"})
	err := root.Execute()
	assert.Error(t, err)

	// Too many args should fail
	root.SetArgs([]string{"completion", "bash", "extra"})
	err = root.Execute()
	assert.Error(t, err)
}

func TestCompletionCommand_BashOutput(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	root.SetArgs([]string{"completion", "bash"})
	err := root.Execute()
	assert.NoError(t, err)
}

func TestCompletionCommand_ZshOutput(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	root.SetArgs([]string{"completion", "zsh"})
	err := root.Execute()
	assert.NoError(t, err)
}

func TestCompletionCommand_FishOutput(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	root.SetArgs([]string{"completion", "fish"})
	err := root.Execute()
	assert.NoError(t, err)
}

func TestCompletionCommand_BashContainsCompletionFunction(t *testing.T) {
	newRootTestEnv(t)

	var buf strings.Builder
	root := RootCmd()
	root.SetArgs([]string{"completion", "bash"})
	root.SetOut(&buf)
	err := root.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "sd")
	assert.Contains(t, output, "completion")
}

func TestCompletionCommand_ZshContainsCompletionFunction(t *testing.T) {
	newRootTestEnv(t)

	var buf strings.Builder
	root := RootCmd()
	root.SetArgs([]string{"completion", "zsh"})
	root.SetOut(&buf)
	err := root.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "sd")
}

func TestCompletionCommand_FishContainsCompletionFunction(t *testing.T) {
	newRootTestEnv(t)

	var buf strings.Builder
	root := RootCmd()
	root.SetArgs([]string{"completion", "fish"})
	root.SetOut(&buf)
	err := root.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "sd")
}

func TestCompletionCommand_UnsupportedShell(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	root.SetArgs([]string{"completion", "powershell"})
	err := root.Execute()
	assert.Error(t, err)

	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok, "error should be CLIError")
	assert.Equal(t, "invalid_argument", cliErr.Code)
	assert.Contains(t, cliErr.Message, "unsupported shell")
	assert.Contains(t, cliErr.Message, "powershell")
}

func TestCompletionCommand_HelpContainsInstallation(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	completionCmd, _, err := root.Find([]string{"completion"})
	require.NoError(t, err)
	require.NotNil(t, completionCmd)

	longDesc := completionCmd.Long
	assert.Contains(t, longDesc, "Bash", "help must include Bash instructions")
	assert.Contains(t, longDesc, "Zsh", "help must include Zsh instructions")
	assert.Contains(t, longDesc, "Fish", "help must include Fish instructions")
	assert.Contains(t, longDesc, "source", "help must include source command")
}

func TestCompletionCommand_ValidArgs(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	completionCmd, _, err := root.Find([]string{"completion"})
	require.NoError(t, err)
	require.NotNil(t, completionCmd)

	validArgs := completionCmd.ValidArgs
	assert.Contains(t, validArgs, "bash")
	assert.Contains(t, validArgs, "zsh")
	assert.Contains(t, validArgs, "fish")
	assert.Len(t, validArgs, 3)
}

func TestCompletionCommand_FlagParsingEnabled(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	completionCmd, _, err := root.Find([]string{"completion"})
	require.NoError(t, err)
	require.NotNil(t, completionCmd)

	// Flag parsing must be enabled so --help works correctly
	assert.False(t, completionCmd.DisableFlagParsing,
		"completion must allow flag parsing so --help is handled by Cobra")
}

// --- VM Name Completion Tests ---

func TestVMNameCompletion_ReturnsVMNames(t *testing.T) {
	newRootTestEnv(t)

	mb := &mockCompletionBackend{
		vms: []backend.VMInfo{
			{Name: "vm1", Status: backend.StatusRunning},
			{Name: "vm2", Status: backend.StatusStopped},
		},
	}
	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	root := RootCmd()
	names, directive := vmNameCompletion(root, nil, "")
	assert.Equal(t, []string{"vm1", "vm2"}, names)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

func TestVMNameCompletion_BackendUnavailable(t *testing.T) {
	newRootTestEnv(t)

	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return nil, assert.AnError
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	root := RootCmd()
	names, directive := vmNameCompletion(root, nil, "")
	assert.Nil(t, names)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

func TestVMNameCompletion_EmptyList(t *testing.T) {
	newRootTestEnv(t)

	mb := &mockCompletionBackend{vms: []backend.VMInfo{}}
	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	root := RootCmd()
	names, directive := vmNameCompletion(root, nil, "")
	assert.Empty(t, names)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

func TestVMNameCompletion_ListError(t *testing.T) {
	newRootTestEnv(t)

	mb := &mockCompletionBackend{listErr: assert.AnError}
	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	root := RootCmd()
	names, directive := vmNameCompletion(root, nil, "")
	assert.Nil(t, names)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

// --- Backend Name Completion Tests ---

func TestBackendNameCompletion_ReturnsBackends(t *testing.T) {
	newRootTestEnv(t)

	names, directive := backendNameCompletion(nil, nil, "")
	// Should return at least "lima" if registered, or empty if not in test
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	_ = names
}

// --- Module Name Completion Tests ---

func TestModuleNameCompletion_ReturnsModuleNames(t *testing.T) {
	newRootTestEnv(t)

	names, directive := moduleNameCompletion(nil, nil, "")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	assert.Contains(t, names, "base")
	assert.Contains(t, names, "golang")
	assert.Contains(t, names, "docker")
	assert.Contains(t, names, "claude-code")
	assert.Contains(t, names, "rust")
	assert.Contains(t, names, "python")
	assert.Contains(t, names, "github-cli")
}

// --- Snapshot Tag Completion Tests ---

func TestSnapshotTagCompletion_ReturnsTags(t *testing.T) {
	newRootTestEnv(t)

	mb := &mockCompletionBackend{
		vms: []backend.VMInfo{{Name: "vm1"}},
		snapshots: []backend.SnapshotInfo{
			{Name: "clean"},
			{Name: "dirty"},
		},
	}
	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	root := RootCmd()
	tags, directive := snapshotTagCompletion(root, []string{"vm1"}, "")
	assert.Equal(t, []string{"clean", "dirty"}, tags)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

func TestSnapshotTagCompletion_NoVMArg(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	tags, directive := snapshotTagCompletion(root, nil, "")
	assert.Nil(t, tags)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

func TestSnapshotTagCompletion_NonSnapshotterBackend(t *testing.T) {
	newRootTestEnv(t)

	mb := &mockNonSnapshotterCompletionBackend{}
	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	root := RootCmd()
	tags, directive := snapshotTagCompletion(root, []string{"vm1"}, "")
	assert.Nil(t, tags)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

// --- requiresVMCompletion Tests ---

func TestRequiresVMCompletion(t *testing.T) {
	tests := []struct {
		use   string
		match bool
	}{
		{"create <name>", true},
		{"destroy <name>", true},
		{"start <name>", true},
		{"stop <name>", true},
		{"status [name]", true},
		{"connect <name>", true},
		{"exec <name>", true},
		{"ssh-config <name>", true},
		{"provision <vm>", true},
		{"list", false},
		{"", false},
		{"completion <shell>", false},
	}
	for _, tc := range tests {
		cmd := &cobra.Command{Use: tc.use}
		got := requiresVMCompletion(cmd)
		assert.Equal(t, tc.match, got, "requiresVMCompletion(%q) = %v, want %v", tc.use, got, tc.match)
	}
}

// --- Property-Based Tests ---

func TestProperty_Completion_BashAlwaysProducesOutput(t *testing.T) {
	newRootTestEnv(t)
	rapid.Check(t, func(t *rapid.T) {
		var buf strings.Builder
		root := RootCmd()
		root.SetArgs([]string{"completion", "bash"})
		root.SetOut(&buf)
		err := root.Execute()
		if err != nil {
			t.Fatalf("bash completion should never fail: %v", err)
		}
		if buf.Len() == 0 {
			t.Fatal("bash completion should produce output")
		}
	})
}

func TestProperty_Completion_ZshAlwaysProducesOutput(t *testing.T) {
	newRootTestEnv(t)
	rapid.Check(t, func(t *rapid.T) {
		var buf strings.Builder
		root := RootCmd()
		root.SetArgs([]string{"completion", "zsh"})
		root.SetOut(&buf)
		err := root.Execute()
		if err != nil {
			t.Fatalf("zsh completion should never fail: %v", err)
		}
		if buf.Len() == 0 {
			t.Fatal("zsh completion should produce output")
		}
	})
}

func TestProperty_Completion_FishAlwaysProducesOutput(t *testing.T) {
	newRootTestEnv(t)
	rapid.Check(t, func(t *rapid.T) {
		var buf strings.Builder
		root := RootCmd()
		root.SetArgs([]string{"completion", "fish"})
		root.SetOut(&buf)
		err := root.Execute()
		if err != nil {
			t.Fatalf("fish completion should never fail: %v", err)
		}
		if buf.Len() == 0 {
			t.Fatal("fish completion should produce output")
		}
	})
}

func TestProperty_Completion_InvalidShellAlwaysFails(t *testing.T) {
	invalidShells := []string{"powershell", "tcsh", "ksh", "csh", "elvish", "nushell", "ion", "xonsh"}

	// Set up test env once for all rapid iterations
	newRootTestEnv(t)
	rapid.Check(t, func(t *rapid.T) {
		idx := rapid.IntRange(0, len(invalidShells)-1).Draw(t, "idx")
		shell := invalidShells[idx]

		root := RootCmd()
		root.SetArgs([]string{"completion", shell})
		err := root.Execute()
		if err == nil {
			t.Fatalf("shell %q should not be supported", shell)
		}
	})
}

func TestProperty_Completion_ErrorCodesSnakeCase(t *testing.T) {
	invalidShells := []string{"powershell", "tcsh", "ksh", "csh", "elvish", "nushell"}
	for _, shell := range invalidShells {
		t.Run(shell, func(t *testing.T) {
			newRootTestEnv(t)
			root := RootCmd()
			root.SetArgs([]string{"completion", shell})
			err := root.Execute()
			require.Error(t, err)

			cliErr, ok := err.(ui.CLIError)
			require.True(t, ok, "error should be CLIError")
			// Code uses snake_case: no spaces, no uppercase, no hyphens
			assert.NotContains(t, cliErr.Code, " ")
			assert.NotContains(t, cliErr.Code, "-")
			assert.Equal(t, strings.ToLower(cliErr.Code), cliErr.Code)
		})
	}
}

func TestProperty_Completion_BashScriptContainsSD(t *testing.T) {
	newRootTestEnv(t)
	rapid.Check(t, func(t *rapid.T) {
		var buf strings.Builder
		root := RootCmd()
		root.SetArgs([]string{"completion", "bash"})
		root.SetOut(&buf)

		err := root.Execute()
		if err != nil {
			t.Fatalf("should not fail: %v", err)
		}
		if !strings.Contains(buf.String(), "sd") {
			t.Fatal("bash completion must reference 'sd'")
		}
	})
}

func TestProperty_Completion_ModuleNamesAlwaysComplete(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		names, directive := moduleNameCompletion(nil, nil, "")
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Fatal("module completion should use NoFileComp directive")
		}
		if len(names) == 0 {
			t.Fatal("module names should always have completions")
		}
		for _, n := range names {
			if n == "" {
				t.Fatal("module names should not be empty")
			}
		}
	})
}

func TestProperty_Completion_VMNamesMatchList(t *testing.T) {
	vmSets := [][]string{
		{"vm1"},
		{"vm1", "vm2", "vm3"},
		{"test-vm", "prod-vm"},
		{"a", "b", "c", "d", "e"},
	}

	// Set up test env once outside rapid callback
	newRootTestEnv(t)
	origGetBackend := getBackendFunc
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	rapid.Check(t, func(t *rapid.T) {
		idx := rapid.IntRange(0, len(vmSets)-1).Draw(t, "idx")
		vmNames := vmSets[idx]

		vms := make([]backend.VMInfo, len(vmNames))
		for i, n := range vmNames {
			vms[i] = backend.VMInfo{Name: n}
		}
		mb := &mockCompletionBackend{vms: vms}
		getBackendFunc = func(_ string) (backend.Backend, error) {
			return mb, nil
		}

		root := RootCmd()
		completed, _ := vmNameCompletion(root, nil, "")
		if len(completed) != len(vmNames) {
			t.Fatalf("expected %d completions, got %d", len(vmNames), len(completed))
		}
		for i, name := range completed {
			if name != vmNames[i] {
				t.Fatalf("completion[%d] = %q, want %q", i, name, vmNames[i])
			}
		}
	})
}

func TestProperty_Completion_SnapshotTagsMatchList(t *testing.T) {
	tagSets := [][]string{
		{"v1"},
		{"clean", "dirty"},
		{"snap-1", "snap-2", "snap-3"},
	}

	// Set up test env once outside rapid callback
	newRootTestEnv(t)
	origGetBackend := getBackendFunc
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	rapid.Check(t, func(t *rapid.T) {
		idx := rapid.IntRange(0, len(tagSets)-1).Draw(t, "idx")
		tags := tagSets[idx]

		snaps := make([]backend.SnapshotInfo, len(tags))
		for i, tag := range tags {
			snaps[i] = backend.SnapshotInfo{Name: tag}
		}
		mb := &mockCompletionBackend{
			vms:       []backend.VMInfo{{Name: "testvm"}},
			snapshots: snaps,
		}
		getBackendFunc = func(_ string) (backend.Backend, error) {
			return mb, nil
		}

		root := RootCmd()
		completed, _ := snapshotTagCompletion(root, []string{"testvm"}, "")
		if len(completed) != len(tags) {
			t.Fatalf("expected %d snapshot tags, got %d", len(tags), len(completed))
		}
		for i, name := range completed {
			if name != tags[i] {
				t.Fatalf("snapshot[%d] = %q, want %q", i, name, tags[i])
			}
		}
	})
}

func TestProperty_Completion_BackendUnavailableReturnsNoFileComp(t *testing.T) {
	newRootTestEnv(t)
	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return nil, assert.AnError
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	rapid.Check(t, func(t *rapid.T) {
		root := RootCmd()
		names, directive := vmNameCompletion(root, nil, "")
		if names != nil {
			t.Fatal("should return nil names when backend unavailable")
		}
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Fatal("should return NoFileComp when backend unavailable")
		}
	})
}

// --- Digital Twin Mocks ---

// mockCompletionBackend implements backend.Backend and backend.Snapshotter
// for completion testing.
type mockCompletionBackend struct {
	vms       []backend.VMInfo
	snapshots []backend.SnapshotInfo
	listErr   error
}

func (m *mockCompletionBackend) Name() string    { return "mock" }
func (m *mockCompletionBackend) Available() error { return nil }
func (m *mockCompletionBackend) Create(_ context.Context, _ string, _ backend.VMConfig) error {
	return nil
}
func (m *mockCompletionBackend) Start(_ context.Context, _ string) error { return nil }
func (m *mockCompletionBackend) Stop(_ context.Context, _ string) error  { return nil }
func (m *mockCompletionBackend) Destroy(_ context.Context, _ string) error { return nil }
func (m *mockCompletionBackend) Status(_ context.Context, name string) (backend.VMStatus, error) {
	for _, vm := range m.vms {
		if vm.Name == name {
			return vm.Status, nil
		}
	}
	return "", backend.ErrVMNotFound
}
func (m *mockCompletionBackend) List(_ context.Context) ([]backend.VMInfo, error) {
	return m.vms, m.listErr
}
func (m *mockCompletionBackend) SSHConfig(_ context.Context, _ string) (backend.SSHConfig, error) {
	return backend.SSHConfig{}, nil
}
func (m *mockCompletionBackend) Exec(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
	return backend.ExecResult{}, nil
}

// Snapshotter implementation
func (m *mockCompletionBackend) SnapshotCreate(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockCompletionBackend) SnapshotApply(_ context.Context, _, _ string) error { return nil }
func (m *mockCompletionBackend) SnapshotDelete(_ context.Context, _, _ string) error { return nil }
func (m *mockCompletionBackend) SnapshotList(_ context.Context, _ string) ([]backend.SnapshotInfo, error) {
	return m.snapshots, nil
}

// mockNonSnapshotterCompletionBackend implements backend.Backend only
// (no Snapshotter) for testing non-snapshotter code path.
type mockNonSnapshotterCompletionBackend struct{}

func (m *mockNonSnapshotterCompletionBackend) Name() string    { return "mock-nosnap" }
func (m *mockNonSnapshotterCompletionBackend) Available() error { return nil }
func (m *mockNonSnapshotterCompletionBackend) Create(_ context.Context, _ string, _ backend.VMConfig) error {
	return nil
}
func (m *mockNonSnapshotterCompletionBackend) Start(_ context.Context, _ string) error  { return nil }
func (m *mockNonSnapshotterCompletionBackend) Stop(_ context.Context, _ string) error   { return nil }
func (m *mockNonSnapshotterCompletionBackend) Destroy(_ context.Context, _ string) error { return nil }
func (m *mockNonSnapshotterCompletionBackend) Status(_ context.Context, _ string) (backend.VMStatus, error) {
	return backend.StatusRunning, nil
}
func (m *mockNonSnapshotterCompletionBackend) List(_ context.Context) ([]backend.VMInfo, error) {
	return nil, nil
}
func (m *mockNonSnapshotterCompletionBackend) SSHConfig(_ context.Context, _ string) (backend.SSHConfig, error) {
	return backend.SSHConfig{}, nil
}
func (m *mockNonSnapshotterCompletionBackend) Exec(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
	return backend.ExecResult{}, nil
}
