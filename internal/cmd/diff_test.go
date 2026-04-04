// Package cmd provides tests for the diff command.
// REQ-004-018: CI Workflow Change Detection
//
// NOTE: Tests use global getBackendFunc — do not use t.Parallel().
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/backend"
	"sd/internal/backend/memory"
	"sd/internal/ui"
)

// ---------------------------------------------------------------------------
// Test Setup
// ---------------------------------------------------------------------------

// setupDiffTest configures the test environment with a memory backend.
func setupDiffTest(t *testing.T, mb *memory.Backend) {
	t.Helper()
	newRootTestEnv(t)

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })
}

// newDiffMemBackend builds a memory backend with "test-vm" running and an
// exec handler that responds to git status and git rev-parse commands.
func newDiffMemBackend(t *testing.T, gitStatus string, hooksOutput string) *memory.Backend {
	t.Helper()
	mb := memory.New()
	require.NoError(t, mb.Create(context.Background(), "test-vm", backend.VMConfig{}))

	mb.SetExecHandler(func(_ context.Context, name string, command []string) (backend.ExecResult, error) {
		cmdStr := strings.Join(command, " ")
		if strings.Contains(cmdStr, "git status --porcelain") {
			return backend.ExecResult{ExitCode: 0, Stdout: "true\n" + gitStatus}, nil
		}
		if strings.Contains(cmdStr, "git rev-parse --git-dir") {
			return backend.ExecResult{ExitCode: 0, Stdout: ".git\n" + hooksOutput}, nil
		}
		return backend.ExecResult{ExitCode: 0, Stdout: ""}, nil
	})

	return mb
}

// --- Unit tests: Registration ---

func TestDiffCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "diff" {
			found = true
			assert.Equal(t, "security", cmd.GroupID)
			break
		}
	}
	assert.True(t, found, "diff command must be registered")
}

func TestDiffCommand_NoConfigRequired(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	diffCmd, _, err := root.Find([]string{"diff"})
	require.NoError(t, err)
	assert.Equal(t, "security", diffCmd.GroupID)
}

func TestDiffCommand_ExactArgs(t *testing.T) {
	mb := newDiffMemBackend(t, "", "")
	setupDiffTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"--json", "diff", "test-vm"})
	err := root.Execute()
	require.NoError(t, err)
}

func TestDiffCommand_MissingName(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	root.SetArgs([]string{"diff"})
	err := root.Execute()
	assert.Error(t, err, "diff without args must fail")
}

// --- Unit tests: Human output ---

func TestDiffCommand_HumanOutput_NoChanges(t *testing.T) {
	mb := newDiffMemBackend(t, "", "")
	setupDiffTest(t, mb)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"diff", "test-vm"})
	err := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No changes detected")
	assert.Contains(t, buf.String(), "test-vm")
}

func TestDiffCommand_HumanOutput_WithCIChanges(t *testing.T) {
	gitStatus := "M .github/workflows/build.yml\nA .gitlab-ci.yml\nM src/main.go"
	mb := newDiffMemBackend(t, gitStatus, "")
	setupDiffTest(t, mb)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"diff", "test-vm"})
	err := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, ".github/workflows/build.yml")
	assert.Contains(t, output, ".gitlab-ci.yml")
	assert.Contains(t, output, "src/main.go")
	assert.Contains(t, output, "Warnings")
	assert.Contains(t, output, ".github/workflows/")
	assert.Contains(t, output, ".gitlab-ci.yml")
}

func TestDiffCommand_HumanOutput_NoCIChanges(t *testing.T) {
	gitStatus := "M src/main.go\nA README.md"
	mb := newDiffMemBackend(t, gitStatus, "")
	setupDiffTest(t, mb)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"diff", "test-vm"})
	err := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "src/main.go")
	assert.Contains(t, output, "README.md")
	assert.NotContains(t, output, "Warnings")
}

// --- Unit tests: JSON output ---

func TestDiffCommand_JSONOutput_NoChanges(t *testing.T) {
	mb := newDiffMemBackend(t, "", "")
	setupDiffTest(t, mb)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "diff", "test-vm"})
	err := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, err)

	var result map[string]any
	jsonErr := json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, jsonErr)
	assert.True(t, result["ok"].(bool))

	data := result["data"].(map[string]any)
	assert.Equal(t, "test-vm", data["vm"])
	changes := data["changes"].([]any)
	assert.Empty(t, changes)
	warnings := data["warnings"].([]any)
	assert.Empty(t, warnings)
}

func TestDiffCommand_JSONOutput_WithCIChanges(t *testing.T) {
	gitStatus := "M .github/workflows/ci.yml\n?? Jenkinsfile"
	mb := newDiffMemBackend(t, gitStatus, "")
	setupDiffTest(t, mb)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "diff", "test-vm"})
	err := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, err)

	var result map[string]any
	jsonErr := json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, jsonErr)

	data := result["data"].(map[string]any)
	changes := data["changes"].([]any)
	assert.Len(t, changes, 2)

	warnings := data["warnings"].([]any)
	assert.Len(t, warnings, 2)

	// Verify each warning has the expected fields
	for _, w := range warnings {
		entry := w.(map[string]any)
		assert.True(t, entry["warning"].(bool))
		assert.NotEmpty(t, entry["category"])
	}
}

func TestDiffCommand_JSONOutput_MixedChanges(t *testing.T) {
	gitStatus := "M .github/workflows/test.yml\nM src/app.go\nD README.md"
	mb := newDiffMemBackend(t, gitStatus, "")
	setupDiffTest(t, mb)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "diff", "test-vm"})
	err := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, err)

	var result map[string]any
	jsonErr := json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, jsonErr)

	data := result["data"].(map[string]any)
	changes := data["changes"].([]any)
	assert.Len(t, changes, 3)

	warnings := data["warnings"].([]any)
	assert.Len(t, warnings, 1)
}

// --- Unit tests: Error handling ---

func TestDiffCommand_VMNotFound(t *testing.T) {
	mb := memory.New()
	setupDiffTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"diff", "nonexistent"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
}

func TestDiffCommand_VMNotRunning(t *testing.T) {
	mb := memory.New()
	require.NoError(t, mb.Create(context.Background(), "stopped-vm", backend.VMConfig{}))
	require.NoError(t, mb.Stop(context.Background(), "stopped-vm"))
	setupDiffTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"diff", "stopped-vm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_running", cliErr.Code)
}

func TestDiffCommand_BackendUnavailable(t *testing.T) {
	newRootTestEnv(t)
	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return nil, fmt.Errorf("no such backend")
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	root := RootCmd()
	root.SetArgs([]string{"diff", "myvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestDiffCommand_BackendNotAvailable(t *testing.T) {
	mb := memory.New()
	mb.SetMethodError("available", fmt.Errorf("backend not available"))
	setupDiffTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"diff", "myvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestDiffCommand_ExecError(t *testing.T) {
	mb := memory.New()
	require.NoError(t, mb.Create(context.Background(), "test-vm", backend.VMConfig{}))
	mb.SetExecHandler(func(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
		return backend.ExecResult{}, fmt.Errorf("ssh connection refused")
	})
	setupDiffTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"diff", "test-vm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "diff_failed", cliErr.Code)
}

func TestDiffCommand_ExecErrVMNotRunning(t *testing.T) {
	mb := memory.New()
	require.NoError(t, mb.Create(context.Background(), "test-vm", backend.VMConfig{}))
	mb.SetExecHandler(func(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
		return backend.ExecResult{}, backend.ErrVMNotRunning
	})
	setupDiffTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"diff", "test-vm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_running", cliErr.Code)
}

func TestDiffCommand_EmptyName(t *testing.T) {
	mb := memory.New()
	setupDiffTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"diff", ""})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
}

func TestDiffCommand_StatusCheckError(t *testing.T) {
	mb := memory.New()
	mb.SetMethodError("status", fmt.Errorf("connection refused"))
	setupDiffTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"diff", "test-vm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "diff_failed", cliErr.Code)
}

// --- Unit tests: Git hooks detection ---

func TestDiffCommand_HooksDetection(t *testing.T) {
	gitStatus := "M src/main.go"
	hooksOutput := "pre-commit\ncommit-msg\napplypatch-msg.sample"
	mb := newDiffMemBackend(t, gitStatus, hooksOutput)
	setupDiffTest(t, mb)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "diff", "test-vm"})
	err := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, err)

	var result map[string]any
	jsonErr := json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, jsonErr)

	data := result["data"].(map[string]any)
	changes := data["changes"].([]any)

	// Should have: src/main.go, .git/hooks/pre-commit, .git/hooks/commit-msg
	// (.sample files are filtered out)
	foundPreCommit := false
	foundCommitMsg := false
	for _, c := range changes {
		entry := c.(map[string]any)
		path := entry["path"].(string)
		if path == ".git/hooks/pre-commit" {
			foundPreCommit = true
			assert.True(t, entry["warning"].(bool))
		}
		if path == ".git/hooks/commit-msg" {
			foundCommitMsg = true
			assert.True(t, entry["warning"].(bool))
		}
	}
	assert.True(t, foundPreCommit, "should detect pre-commit hook")
	assert.True(t, foundCommitMsg, "should detect commit-msg hook")
}

func TestDiffCommand_HooksNotDuplicated(t *testing.T) {
	// If a hook already appears in git status, it shouldn't be added again
	gitStatus := "M .git/hooks/pre-commit\nM src/main.go"
	hooksOutput := "pre-commit"
	mb := newDiffMemBackend(t, gitStatus, hooksOutput)
	setupDiffTest(t, mb)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "diff", "test-vm"})
	err := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, err)

	var result map[string]any
	jsonErr := json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, jsonErr)

	data := result["data"].(map[string]any)
	changes := data["changes"].([]any)

	// Count occurrences of .git/hooks/pre-commit
	count := 0
	for _, c := range changes {
		entry := c.(map[string]any)
		if entry["path"] == ".git/hooks/pre-commit" {
			count++
		}
	}
	assert.Equal(t, 1, count, "hook should appear exactly once, not duplicated")
}

// --- Unit tests: parseGitStatus ---

func TestParseGitStatus_Basic(t *testing.T) {
	entries := parseGitStatus("true\nM file.go\nA new.go\n?? untracked.txt")
	require.Len(t, entries, 3)
	assert.Equal(t, "file.go", entries[0].Path)
	assert.Equal(t, "modified", entries[0].Status)
	assert.Equal(t, "new.go", entries[1].Path)
	assert.Equal(t, "added", entries[1].Status)
	assert.Equal(t, "untracked.txt", entries[2].Path)
	assert.Equal(t, "untracked", entries[2].Status)
}

func TestParseGitStatus_Empty(t *testing.T) {
	entries := parseGitStatus("")
	assert.Nil(t, entries)
}

func TestParseGitStatus_Rename(t *testing.T) {
	entries := parseGitStatus("R  old.go -> new.go")
	require.Len(t, entries, 1)
	assert.Equal(t, "new.go", entries[0].Path)
	assert.Equal(t, "renamed", entries[0].Status)
}

func TestParseGitStatus_Deleted(t *testing.T) {
	entries := parseGitStatus("D removed.go")
	require.Len(t, entries, 1)
	assert.Equal(t, "removed.go", entries[0].Path)
	assert.Equal(t, "deleted", entries[0].Status)
}

// --- Unit tests: isCICDPath ---

func TestIsCICDPath(t *testing.T) {
	tests := []struct {
		path      string
		isWarning bool
		category  string
	}{
		{".github/workflows/build.yml", true, ".github/workflows/"},
		{".github/workflows/ci.yaml", true, ".github/workflows/"},
		{".gitlab-ci.yml", true, ".gitlab-ci.yml"},
		{"Jenkinsfile", true, "Jenkinsfile"},
		{".circleci/config.yml", true, ".circleci/"},
		{".git/hooks/pre-commit", true, ".git/hooks/"},
		{"src/main.go", false, ""},
		{"README.md", false, ""},
		{"go.mod", false, ""},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			warning, category := isCICDPath(tc.path)
			assert.Equal(t, tc.isWarning, warning)
			assert.Equal(t, tc.category, category)
		})
	}
}

// --- Unit tests: gitStatusToLabel ---

func TestGitStatusToLabel(t *testing.T) {
	tests := []struct {
		xy     string
		label  string
	}{
		{"??", "untracked"},
		{"A ", "added"},
		{" M", "modified"},
		{"M ", "modified"},
		{"D ", "deleted"},
		{" D", "deleted"},
		{"R ", "renamed"},
		{"C ", "copied"},
		{"AM", "added"},
	}

	for _, tc := range tests {
		t.Run(tc.xy, func(t *testing.T) {
			assert.Equal(t, tc.label, gitStatusToLabel(tc.xy))
		})
	}
}

// --- Unit tests: formatDiffOutput ---

func TestFormatDiffOutput_NoChanges(t *testing.T) {
	data := diffResult{VM: "myvm", Changes: []diffEntry{}, Warnings: []diffEntry{}}
	output := formatDiffOutput(data)
	assert.Contains(t, output, "No changes detected")
}

func TestFormatDiffOutput_WithChanges(t *testing.T) {
	data := diffResult{
		VM: "myvm",
		Changes: []diffEntry{
			{Path: "src/main.go", Status: "modified"},
			{Path: ".github/workflows/build.yml", Status: "modified", Warning: true, Category: ".github/workflows/"},
		},
		Warnings: []diffEntry{
			{Path: ".github/workflows/build.yml", Status: "modified", Warning: true, Category: ".github/workflows/"},
		},
	}
	output := formatDiffOutput(data)
	assert.Contains(t, output, "src/main.go")
	assert.Contains(t, output, ".github/workflows/build.yml")
	assert.Contains(t, output, "Warnings")
	assert.Contains(t, output, ".github/workflows/")
}

func TestFormatDiffOutput_OnlyChangesNoWarnings(t *testing.T) {
	data := diffResult{
		VM: "myvm",
		Changes: []diffEntry{
			{Path: "src/main.go", Status: "modified"},
		},
		Warnings: []diffEntry{},
	}
	output := formatDiffOutput(data)
	assert.Contains(t, output, "src/main.go")
	assert.NotContains(t, output, "Warnings")
}

// --- Property tests ---

func TestProperty_DiffJSONAlwaysValid(t *testing.T) {
	cases := []struct {
		name       string
		vmName     string
		gitStatus  string
		hooks      string
	}{
		{"no_changes", "vm1", "", ""},
		{"ci_changes", "vm2", "M .github/workflows/ci.yml", ""},
		{"mixed_changes", "vm3", "M src/app.go\nA README.md", ""},
		{"hooks_only", "vm4", "", "pre-commit"},
		{"all_types", "vm5", "M .gitlab-ci.yml\n?? new.go\nD old.go", "commit-msg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mb := memory.New()
			require.NoError(t, mb.Create(context.Background(), tc.vmName, backend.VMConfig{}))
			mb.SetExecHandler(func(_ context.Context, _ string, command []string) (backend.ExecResult, error) {
				cmdStr := strings.Join(command, " ")
				if strings.Contains(cmdStr, "git status --porcelain") {
					return backend.ExecResult{ExitCode: 0, Stdout: "true\n" + tc.gitStatus}, nil
				}
				if strings.Contains(cmdStr, "git rev-parse --git-dir") {
					return backend.ExecResult{ExitCode: 0, Stdout: ".git\n" + tc.hooks}, nil
				}
				return backend.ExecResult{ExitCode: 0, Stdout: ""}, nil
			})
			setupDiffTest(t, mb)

			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"--json", "diff", tc.vmName})
			err := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, err)

			var result map[string]any
			jsonErr := json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, jsonErr, "JSON must parse for %s: %s", tc.name, buf.String())
			assert.True(t, result["ok"].(bool), "ok must be true for %s", tc.name)

			data := result["data"].(map[string]any)
			assert.Equal(t, tc.vmName, data["vm"])
			assert.NotNil(t, data["changes"])
			assert.NotNil(t, data["warnings"])
		})
	}
}

func TestProperty_DiffErrorCodesSnakeCase(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		setupFn    func(t *testing.T)
		wantCode   string
	}{
		{
			"vm_not_found",
			[]string{"diff", "missing"},
			func(t *testing.T) {
				mb := memory.New()
				setupDiffTest(t, mb)
			},
			"vm_not_found",
		},
		{
			"vm_not_running",
			[]string{"diff", "stopped"},
			func(t *testing.T) {
				mb := memory.New()
				require.NoError(t, mb.Create(context.Background(), "stopped", backend.VMConfig{}))
				require.NoError(t, mb.Stop(context.Background(), "stopped"))
				setupDiffTest(t, mb)
			},
			"vm_not_running",
		},
		{
			"invalid_argument",
			[]string{"diff", ""},
			func(t *testing.T) {
				mb := memory.New()
				setupDiffTest(t, mb)
			},
			"invalid_argument",
		},
		{
			"backend_unavailable",
			[]string{"diff", "vm1"},
			func(t *testing.T) {
				mb := memory.New()
				mb.SetMethodError("available", fmt.Errorf("backend not available"))
				setupDiffTest(t, mb)
			},
			"backend_unavailable",
		},
		{
			"diff_failed_on_status_err",
			[]string{"diff", "vm1"},
			func(t *testing.T) {
				mb := memory.New()
				mb.SetMethodError("status", fmt.Errorf("fail"))
				setupDiffTest(t, mb)
			},
			"diff_failed",
		},
		{
			"diff_failed_on_exec_err",
			[]string{"diff", "vm1"},
			func(t *testing.T) {
				mb := memory.New()
				require.NoError(t, mb.Create(context.Background(), "vm1", backend.VMConfig{}))
				mb.SetExecHandler(func(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
					return backend.ExecResult{}, fmt.Errorf("fail")
				})
				setupDiffTest(t, mb)
			},
			"diff_failed",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.setupFn(t)

			root := RootCmd()
			root.SetArgs(tc.args)
			err := root.Execute()

			require.Error(t, err)
			cliErr, ok := err.(ui.CLIError)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, cliErr.Code)
			// Verify snake_case: only lowercase, underscores, no spaces
			assert.NotContains(t, cliErr.Code, " ")
			assert.Equal(t, strings.ToLower(cliErr.Code), cliErr.Code)
		})
	}
}

func TestProperty_DiffHumanContainsVMName(t *testing.T) {
	vmNames := []string{"my-vm", "prod-server", "dev-box", "test", "vm-with-long-name"}
	for _, vmName := range vmNames {
		t.Run(vmName, func(t *testing.T) {
			mb := memory.New()
			require.NoError(t, mb.Create(context.Background(), vmName, backend.VMConfig{}))
			mb.SetExecHandler(func(_ context.Context, _ string, command []string) (backend.ExecResult, error) {
				cmdStr := strings.Join(command, " ")
				if strings.Contains(cmdStr, "git status") {
					return backend.ExecResult{ExitCode: 0, Stdout: "true\n"}, nil
				}
				if strings.Contains(cmdStr, "git rev-parse") {
					return backend.ExecResult{ExitCode: 0, Stdout: ""}, nil
				}
				return backend.ExecResult{ExitCode: 0}, nil
			})
			setupDiffTest(t, mb)

			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"diff", vmName})
			err := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, err)
			assert.Contains(t, buf.String(), vmName)
		})
	}
}

func TestProperty_DiffNonexistentVMNeverCallsExec(t *testing.T) {
	vmNames := []string{"ghost", "phantom", "missing"}
	for _, vmName := range vmNames {
		t.Run(vmName, func(t *testing.T) {
			mb := memory.New()
			execCalled := false
			mb.SetExecHandler(func(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
				execCalled = true
				return backend.ExecResult{}, nil
			})
			setupDiffTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"diff", vmName})
			err := root.Execute()

			require.Error(t, err)
			assert.False(t, execCalled, "exec should never be called for nonexistent VM")
		})
	}
}

func TestProperty_DiffJSONRequiredFields(t *testing.T) {
	gitStatus := "M .github/workflows/test.yml\nA src/main.go"
	mb := newDiffMemBackend(t, gitStatus, "pre-commit")
	setupDiffTest(t, mb)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "diff", "test-vm"})
	err := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, err)

	var result map[string]any
	jsonErr := json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, jsonErr)

	data := result["data"].(map[string]any)
	assert.Contains(t, data, "vm")
	assert.Contains(t, data, "changes")
	assert.Contains(t, data, "warnings")

	// Each change entry has required fields
	for _, c := range data["changes"].([]any) {
		entry := c.(map[string]any)
		assert.Contains(t, entry, "path")
		assert.Contains(t, entry, "status")
		assert.Contains(t, entry, "warning")
	}
}

func TestProperty_DiffStoppedVMNeverCallsExec(t *testing.T) {
	statuses := []backend.VMStatus{backend.StatusStopped, backend.StatusError, backend.StatusCreating}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			mb := memory.New()
			require.NoError(t, mb.Create(context.Background(), "vm1", backend.VMConfig{}))
			require.NoError(t, mb.SetStatus("vm1", status))
			execCalled := false
			mb.SetExecHandler(func(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
				execCalled = true
				return backend.ExecResult{}, nil
			})
			setupDiffTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"diff", "vm1"})
			err := root.Execute()

			require.Error(t, err)
			assert.False(t, execCalled, "exec should not be called for non-running VM")
		})
	}
}

func TestProperty_DiffAllCICDPatternsDetected(t *testing.T) {
	// Every defined CI/CD pattern should be detected
	for _, pattern := range cicdPatterns {
		t.Run(pattern, func(t *testing.T) {
			var testPath string
			if strings.HasSuffix(pattern, "/") {
				testPath = pattern + "test-file"
			} else {
				testPath = pattern
			}
			warning, category := isCICDPath(testPath)
			assert.True(t, warning, "path %q should match CI/CD pattern %q", testPath, pattern)
			assert.Equal(t, pattern, category)
		})
	}
}

func TestProperty_DiffCICDWarningAlwaysHasCategory(t *testing.T) {
	// Generate git status output with all CI/CD patterns
	var statusLines []string
	for _, pattern := range cicdPatterns {
		if strings.HasSuffix(pattern, "/") {
			statusLines = append(statusLines, fmt.Sprintf("M %stest.yml", pattern))
		} else {
			statusLines = append(statusLines, fmt.Sprintf("M %s", pattern))
		}
	}
	gitStatus := strings.Join(statusLines, "\n")
	mb := newDiffMemBackend(t, gitStatus, "")
	setupDiffTest(t, mb)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "diff", "test-vm"})
	err := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, err)

	var result map[string]any
	jsonErr := json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, jsonErr)

	data := result["data"].(map[string]any)
	warnings := data["warnings"].([]any)
	assert.Equal(t, len(cicdPatterns), len(warnings), "all CI/CD patterns should generate warnings")

	for _, w := range warnings {
		entry := w.(map[string]any)
		assert.True(t, entry["warning"].(bool))
		assert.NotEmpty(t, entry["category"].(string), "warning must have a category")
	}
}

// --- Property: error JSON format ---

// Property: error codes are consistent regardless of JSON mode
func TestProperty_DiffErrorCodesConsistent(t *testing.T) {
	vmNames := []string{"a", "b", "c"}
	for _, vmName := range vmNames {
		t.Run(vmName, func(t *testing.T) {
			// Both JSON and human mode should produce the same error code
			for _, jsonFlag := range []bool{false, true} {
				name := "human"
				if jsonFlag {
					name = "json"
				}
				t.Run(name, func(t *testing.T) {
					mb := memory.New()
					setupDiffTest(t, mb)

					args := []string{"diff", vmName}
					if jsonFlag {
						args = append([]string{"--json"}, args...)
					}

					root := RootCmd()
					root.SetArgs(args)
					err := root.Execute()

					require.Error(t, err)
					cliErr, ok := err.(ui.CLIError)
					require.True(t, ok)
					assert.Equal(t, "vm_not_found", cliErr.Code)
				})
			}
		})
	}
}
