// Package cmd provides tests for the quick-start command.
// REQ-010-001: Quick-Start Command Registration
// REQ-010-002: Quick-Start Flags
// REQ-010-003: Runbook Output Format
// REQ-010-004 through REQ-010-013: Runbook Sections 1-10
// REQ-010-014: Security Education Requirements
// REQ-010-015: Check Flag -- Setup Assessment
// REQ-010-017: Clone-Not-Mount
// REQ-010-018: Runbook Stability
// NOTE: Tests use global getBackendFunc, getenvFunc, findProjectConfigFunc,
// getWorkingDir -- do not use t.Parallel().
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
	"pgregory.net/rapid"
	"sd/internal/backend"
	"sd/internal/backend/memory"
	"sd/internal/config"
)

// resetQuickStartFlags resets the quick-start command's local flags to defaults.
func resetQuickStartFlags(t *testing.T) {
	t.Helper()
	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "quick-start" {
			_ = cmd.Flags().Set("check", "false")
			_ = cmd.Flags().Set("help", "false")
			break
		}
	}
}

// setupQuickStartTest configures the test environment with a memory backend
// and default mocks for all testable vars used by the quick-start command.
func setupQuickStartTest(t *testing.T) *memory.Backend {
	t.Helper()
	newRootTestEnv(t)
	resetQuickStartFlags(t)

	mb := memory.New()
	origGetBackend := getBackendFunc
	origAllBackendNames := allBackendNames
	origLookPath := lookPath
	origGetEnv := getenvFunc
	origFindConfig := findProjectConfigFunc
	origGetWD := getWorkingDir

	getBackendFunc = func(_ string) (backend.Backend, error) {
		return mb, nil
	}
	allBackendNames = func() []string { return []string{"memory"} }
	lookPath = func(name string) (string, error) {
		return "/usr/bin/" + name, nil
	}
	// Default: no env vars set
	getenvFunc = func(_ string) string { return "" }
	// Default: no project config found
	findProjectConfigFunc = func(_ string) (string, *config.ProjectConfig, error) {
		return "", nil, nil
	}
	tmpDir := t.TempDir()
	getWorkingDir = func() (string, error) {
		return tmpDir, nil
	}

	t.Cleanup(func() {
		getBackendFunc = origGetBackend
		allBackendNames = origAllBackendNames
		lookPath = origLookPath
		getenvFunc = origGetEnv
		findProjectConfigFunc = origFindConfig
		getWorkingDir = origGetWD
		resetQuickStartFlags(t)
		mb.Reset()
	})
	return mb
}

// --- Command registration tests ---

func TestQuickStartCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "quick-start" {
			found = true
			assert.Equal(t, "start", cmd.GroupID, "quick-start must be in 'start' group")

			checkFlag := cmd.Flags().Lookup("check")
			assert.NotNil(t, checkFlag, "quick-start must have --check flag")
			break
		}
	}
	assert.True(t, found, "quick-start command must be registered")
}

func TestQuickStartCommand_HelpExitsZero(t *testing.T) {
	newRootTestEnv(t)
	resetQuickStartFlags(t)

	var buf bytes.Buffer
	root := RootCmd()
	root.SetOut(&buf)
	root.SetArgs([]string{"quick-start", "--help"})
	err := root.Execute()
	root.SetOut(nil)

	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "--check")
}

// --- Default output tests ---

func TestQuickStartCommand_DefaultOutput(t *testing.T) {
	setupQuickStartTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"quick-start"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	require.NoError(t, execErr)
	// REQ-010-003: Output must be the full runbook, not a placeholder
	assert.Contains(t, output, "# sd Quick Start -- Agent Setup Runbook")
	assert.Contains(t, output, "sd guide --agent")
	assert.Contains(t, output, "## 1. Introduction and Education")
}

func TestQuickStartCommand_JSONOutput(t *testing.T) {
	setupQuickStartTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "quick-start"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err, "output must be valid JSON: %s", buf.String())
	assert.True(t, result["ok"].(bool))

	data, ok := result["data"].(map[string]any)
	require.True(t, ok, "data must be a JSON object")
	assert.Contains(t, data, "runbook")
	assert.Contains(t, data["runbook"].(string), "sd guide --agent")
}

// --- --check JSON structure tests ---

func TestQuickStartCommand_CheckReturnsValidJSON(t *testing.T) {
	setupQuickStartTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"quick-start", "--check"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err, "output must be valid JSON: %s", buf.String())
	assert.True(t, result["ok"].(bool))

	data, ok := result["data"].(map[string]any)
	require.True(t, ok, "data must be a JSON object")

	// REQ-010-015: All required fields must be present
	for _, field := range []string{
		"sd_yaml_exists", "vm_exists", "vm_running",
		"vm_name", "backend", "modules", "packages_declared",
		"credentials", "repo_cloned", "needs_setup", "issues",
	} {
		_, exists := data[field]
		assert.True(t, exists, "field %q must exist in check output", field)
	}

	// Credentials must have expected keys
	creds, ok := data["credentials"].(map[string]any)
	require.True(t, ok, "credentials must be a JSON object")
	assert.Contains(t, creds, "github_token")
	assert.Contains(t, creds, "anthropic_api_key")

	// Modules and issues must be arrays (not nil)
	modules, ok := data["modules"].([]any)
	require.True(t, ok, "modules must be an array")
	assert.NotNil(t, modules)

	issues, ok := data["issues"].([]any)
	require.True(t, ok, "issues must be an array")
	assert.NotNil(t, issues)

	// packages_declared must be an object
	pkgsDeclared, ok := data["packages_declared"].(map[string]any)
	require.True(t, ok, "packages_declared must be a JSON object")
	assert.NotNil(t, pkgsDeclared)
}

func TestQuickStartCommand_CheckAlwaysJSON(t *testing.T) {
	// REQ-010-002: --check output is always JSON even without --json flag
	setupQuickStartTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"quick-start", "--check"}) // no --json
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err, "--check without --json must still produce valid JSON: %s", buf.String())
	assert.True(t, result["ok"].(bool))
	assert.Contains(t, result, "data")
}

// --- --check with no .sd.yaml (needs_setup=true) ---

func TestQuickStartCommand_CheckNoConfig_NeedsSetup(t *testing.T) {
	setupQuickStartTest(t)
	// findProjectConfigFunc default returns nil (no config)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"quick-start", "--check"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	data := result["data"].(map[string]any)
	assert.False(t, data["sd_yaml_exists"].(bool))
	assert.False(t, data["vm_exists"].(bool))
	assert.False(t, data["vm_running"].(bool))
	assert.Equal(t, "", data["vm_name"])
	assert.True(t, data["needs_setup"].(bool))

	issues := data["issues"].([]any)
	assert.NotEmpty(t, issues)

	// Must mention missing .sd.yaml
	foundIssue := false
	for _, issue := range issues {
		if s, ok := issue.(string); ok && s == "No .sd.yaml found in this directory -- run sd init to create one" {
			foundIssue = true
		}
	}
	assert.True(t, foundIssue, "issues should mention missing .sd.yaml")
}

// --- --check with .sd.yaml + running VM (needs_setup=false) ---

func TestQuickStartCommand_CheckComplete_NeedsSetupFalse(t *testing.T) {
	mb := setupQuickStartTest(t)
	ctx := context.Background()

	// Create a running VM
	require.NoError(t, mb.Create(ctx, "test-project", backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB",
	}))

	// Mock: .sd.yaml exists with matching config
	findProjectConfigFunc = func(_ string) (string, *config.ProjectConfig, error) {
		return "/tmp/.sd.yaml", &config.ProjectConfig{
			Name:    "test-project",
			Backend: "memory",
			Modules: []string{"base", "golang"},
		}, nil
	}

	// Mock: both credentials set
	getenvFunc = func(key string) string {
		switch key {
		case "GITHUB_TOKEN":
			return "ghp_test123"
		case "ANTHROPIC_API_KEY":
			return "sk-ant-test123"
		}
		return ""
	}

	// Mock: repo is cloned (exec test -d returns 0)
	mb.SetExecHandler(func(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
		return backend.ExecResult{ExitCode: 0}, nil
	})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"quick-start", "--check"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	data := result["data"].(map[string]any)
	assert.True(t, data["sd_yaml_exists"].(bool))
	assert.True(t, data["vm_exists"].(bool))
	assert.True(t, data["vm_running"].(bool))
	assert.Equal(t, "test-project", data["vm_name"])
	assert.Equal(t, "memory", data["backend"])
	assert.True(t, data["repo_cloned"].(bool))
	assert.False(t, data["needs_setup"].(bool))

	issues := data["issues"].([]any)
	assert.Empty(t, issues, "no issues when setup is complete")

	// Verify modules are populated
	modules := data["modules"].([]any)
	assert.Len(t, modules, 2)

	// Verify packages_declared is present (empty when ProjectConfig has no Packages)
	pkgsDeclared, ok := data["packages_declared"].(map[string]any)
	require.True(t, ok, "packages_declared must be a JSON object")
	assert.Empty(t, pkgsDeclared)
}

// --- --check credentials detection ---

func TestQuickStartCommand_CheckCredentials(t *testing.T) {
	tests := []struct {
		name          string
		envVars       map[string]string
		wantGH        bool
		wantAnthropic bool
	}{
		{
			name:          "no_credentials",
			envVars:       map[string]string{},
			wantGH:        false,
			wantAnthropic: false,
		},
		{
			name:          "github_only",
			envVars:       map[string]string{"GITHUB_TOKEN": "ghp_test"},
			wantGH:        true,
			wantAnthropic: false,
		},
		{
			name:          "anthropic_only",
			envVars:       map[string]string{"ANTHROPIC_API_KEY": "sk-ant-test"},
			wantGH:        false,
			wantAnthropic: true,
		},
		{
			name:          "both_credentials",
			envVars:       map[string]string{"GITHUB_TOKEN": "ghp_test", "ANTHROPIC_API_KEY": "sk-ant-test"},
			wantGH:        true,
			wantAnthropic: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupQuickStartTest(t)

			getenvFunc = func(key string) string {
				return tc.envVars[key]
			}

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"quick-start", "--check"})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)

			var result map[string]any
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err)

			data := result["data"].(map[string]any)
			creds := data["credentials"].(map[string]any)

			assert.Equal(t, tc.wantGH, creds["github_token"].(bool),
				"github_token should be %v", tc.wantGH)
			assert.Equal(t, tc.wantAnthropic, creds["anthropic_api_key"].(bool),
				"anthropic_api_key should be %v", tc.wantAnthropic)
		})
	}
}

// --- VM state edge cases ---

func TestQuickStartCommand_CheckStoppedVM(t *testing.T) {
	mb := setupQuickStartTest(t)
	ctx := context.Background()

	// Create and stop VM
	require.NoError(t, mb.Create(ctx, "my-app", backend.VMConfig{
		CPUs: 2, Memory: "4GiB", Disk: "50GiB",
	}))
	require.NoError(t, mb.Stop(ctx, "my-app"))

	findProjectConfigFunc = func(_ string) (string, *config.ProjectConfig, error) {
		return "/tmp/.sd.yaml", &config.ProjectConfig{
			Name:    "my-app",
			Backend: "memory",
			Modules: []string{"base"},
		}, nil
	}

	getenvFunc = func(key string) string {
		if key == "GITHUB_TOKEN" || key == "ANTHROPIC_API_KEY" {
			return "set"
		}
		return ""
	}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"quick-start", "--check"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	data := result["data"].(map[string]any)
	assert.True(t, data["sd_yaml_exists"].(bool))
	assert.True(t, data["vm_exists"].(bool))
	assert.False(t, data["vm_running"].(bool))
	assert.False(t, data["repo_cloned"].(bool), "repo_cloned must be false when VM is stopped")
	assert.True(t, data["needs_setup"].(bool))

	// Issues should mention stopped VM
	issues := data["issues"].([]any)
	foundStoppedIssue := false
	for _, issue := range issues {
		if s, ok := issue.(string); ok {
			if s == "VM \"my-app\" exists but is stopped -- run sd ensure to start" {
				foundStoppedIssue = true
			}
		}
	}
	assert.True(t, foundStoppedIssue, "issues should mention stopped VM")
}

func TestQuickStartCommand_CheckRepoExecCommand(t *testing.T) {
	// Verify the exec command uses /home/ubuntu (not ~) and checks .git suffix.
	mb := setupQuickStartTest(t)
	ctx := context.Background()

	require.NoError(t, mb.Create(ctx, "my-app", backend.VMConfig{
		CPUs: 2, Memory: "4GiB", Disk: "50GiB",
	}))

	findProjectConfigFunc = func(_ string) (string, *config.ProjectConfig, error) {
		return "/tmp/.sd.yaml", &config.ProjectConfig{
			Name:    "my-app",
			Backend: "memory",
		}, nil
	}

	getenvFunc = func(key string) string {
		if key == "GITHUB_TOKEN" || key == "ANTHROPIC_API_KEY" {
			return "set"
		}
		return ""
	}

	var capturedCmd []string
	mb.SetExecHandler(func(_ context.Context, _ string, cmd []string) (backend.ExecResult, error) {
		capturedCmd = cmd
		return backend.ExecResult{ExitCode: 0}, nil
	})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"quick-start", "--check"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	// Verify the command sent to exec
	require.Len(t, capturedCmd, 3, "exec command must have 3 elements")
	assert.Equal(t, "test", capturedCmd[0])
	assert.Equal(t, "-d", capturedCmd[1])
	assert.Equal(t, "/home/ubuntu/projects/my-app/.git", capturedCmd[2],
		"must use explicit /home/ubuntu path (no tilde) and check .git directory")
}

func TestQuickStartCommand_CheckRepoNotCloned(t *testing.T) {
	mb := setupQuickStartTest(t)
	ctx := context.Background()

	require.NoError(t, mb.Create(ctx, "my-app", backend.VMConfig{
		CPUs: 2, Memory: "4GiB", Disk: "50GiB",
	}))

	findProjectConfigFunc = func(_ string) (string, *config.ProjectConfig, error) {
		return "/tmp/.sd.yaml", &config.ProjectConfig{
			Name:    "my-app",
			Backend: "memory",
		}, nil
	}

	getenvFunc = func(key string) string {
		if key == "GITHUB_TOKEN" || key == "ANTHROPIC_API_KEY" {
			return "set"
		}
		return ""
	}

	// Exec handler: test -d returns exit code 1 (directory not found)
	mb.SetExecHandler(func(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
		return backend.ExecResult{ExitCode: 1}, nil
	})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"quick-start", "--check"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	data := result["data"].(map[string]any)
	assert.True(t, data["vm_running"].(bool))
	assert.False(t, data["repo_cloned"].(bool))
	assert.True(t, data["needs_setup"].(bool))

	// Issues should mention repo not cloned
	issues := data["issues"].([]any)
	foundRepoIssue := false
	for _, issue := range issues {
		if s, ok := issue.(string); ok {
			if s == "Repository not cloned inside VM -- run: sd exec my-app -- git clone <url> ~/projects/my-app" {
				foundRepoIssue = true
			}
		}
	}
	assert.True(t, foundRepoIssue, "issues should mention repo not cloned")
}

func TestQuickStartCommand_NoConfigRequired(t *testing.T) {
	// quick-start must succeed without a config file (it's a setup command)
	tmpDir := t.TempDir()
	t.Setenv("SD_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)

	setupQuickStartTest(t)

	root := RootCmd()
	root.SetArgs([]string{"quick-start"})
	err := root.Execute()
	assert.NoError(t, err, "quick-start must succeed without config file")
}

func TestQuickStartCommand_RejectsExtraArgs(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	root.SetArgs([]string{"quick-start", "extra-arg"})
	err := root.Execute()
	assert.Error(t, err, "quick-start should reject extra arguments")
}

// --- Property: --check JSON always has valid structure regardless of state ---

func TestProperty_QuickStartCheckAlwaysValid(t *testing.T) {
	cases := []struct {
		name      string
		hasConfig bool
		hasVM     bool
		vmRunning bool
		hasCreds  bool
	}{
		{"nothing", false, false, false, false},
		{"config_only", true, false, false, false},
		{"config_and_stopped_vm", true, true, false, false},
		{"config_and_running_vm", true, true, true, false},
		{"full_setup", true, true, true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mb := setupQuickStartTest(t)
			ctx := context.Background()

			if tc.hasConfig {
				findProjectConfigFunc = func(_ string) (string, *config.ProjectConfig, error) {
					return "/tmp/.sd.yaml", &config.ProjectConfig{
						Name:    "test-vm",
						Backend: "memory",
						Modules: []string{"base"},
					}, nil
				}
			}

			if tc.hasVM {
				require.NoError(t, mb.Create(ctx, "test-vm", backend.VMConfig{
					CPUs: 2, Memory: "4GiB", Disk: "50GiB",
				}))
				if !tc.vmRunning {
					require.NoError(t, mb.Stop(ctx, "test-vm"))
				}
			}

			if tc.hasCreds {
				getenvFunc = func(key string) string {
					if key == "GITHUB_TOKEN" || key == "ANTHROPIC_API_KEY" {
						return "set"
					}
					return ""
				}
				mb.SetExecHandler(func(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
					return backend.ExecResult{ExitCode: 0}, nil
				})
			}

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"quick-start", "--check"})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)

			var result map[string]any
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err, "JSON must always parse for case %s: %s", tc.name, buf.String())
			assert.True(t, result["ok"].(bool))

			data, ok := result["data"].(map[string]any)
			require.True(t, ok, "data must be an object")

			// All required fields present
			for _, field := range []string{
				"sd_yaml_exists", "vm_exists", "vm_running", "vm_name",
				"backend", "modules", "packages_declared", "credentials",
				"repo_cloned", "needs_setup", "issues",
			} {
				_, exists := data[field]
				assert.True(t, exists, "field %s must exist for case %s", field, tc.name)
			}
		})
	}
}

// --- Rapid property-based test ---

// TestRapid_NeedsSetupTrueIffComponentMissing verifies that for any combination
// of present/absent setup components, needs_setup is true if and only if at
// least one component is missing. REQ-010-015
// --- Runbook content tests ---

// getRunbookOutput captures the Markdown runbook from sd quick-start.
func getRunbookOutput(t *testing.T) string {
	t.Helper()
	setupQuickStartTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"quick-start"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	require.NoError(t, execErr)
	return buf.String()
}

// REQ-010-003: Runbook must contain exactly 10 sections in order.
func TestRunbook_ContainsAll10Sections(t *testing.T) {
	output := getRunbookOutput(t)

	sections := []string{
		"## 1. Introduction and Education",
		"## 2. Prerequisites",
		"## 3. Project Analysis",
		"## 4. Backend Selection",
		"## 5. Environment Configuration",
		"## 6. VM Creation",
		"## 7. Credential Setup",
		"## 8. Repo Clone and Verification",
		"## 9. AI Tool Setup",
		"## 10. Handoff",
	}

	for _, section := range sections {
		assert.Contains(t, output, section,
			"runbook must contain section: %s", section)
	}

	// Verify sections appear in order
	lastIdx := -1
	for _, section := range sections {
		idx := strings.Index(output, section)
		require.NotEqual(t, -1, idx, "section %q must exist", section)
		assert.Greater(t, idx, lastIdx,
			"section %q must appear after previous section", section)
		lastIdx = idx
	}
}

// REQ-010-003: Each section must contain labeled block types.
func TestRunbook_SectionsHaveLabeledBlocks(t *testing.T) {
	output := getRunbookOutput(t)

	// Every section should have at least one of these block types
	blockTypes := []string{
		"**Instructions:**",
		"**Commands:**",
		"**Question to ask the user:**",
		"**Explanation to give the user:**",
		"**Security explanation to give the user:**",
	}

	// Check that at least some block types appear in the output
	foundCount := 0
	for _, bt := range blockTypes {
		if strings.Contains(output, bt) {
			foundCount++
		}
	}
	assert.GreaterOrEqual(t, foundCount, 3,
		"runbook must contain at least 3 different block types")

	// Instructions must appear frequently (at least once per section)
	instructionCount := strings.Count(output, "**Instructions:**")
	assert.GreaterOrEqual(t, instructionCount, 10,
		"runbook must have at least 10 instruction blocks (one per section)")
}

// REQ-010-003: The runbook is addressed to "you" (the agent), not "the user".
func TestRunbook_AddressedToAgent(t *testing.T) {
	output := getRunbookOutput(t)

	// Opening paragraph should address the agent
	assert.Contains(t, output, "You are setting up",
		"runbook must address the agent as 'you'")

	// When referencing the user, should say "the user"
	assert.Contains(t, output, "ask the user",
		"runbook must reference the human as 'the user'")
	assert.Contains(t, output, "explain to the user",
		"runbook must use 'explain to the user' phrasing")
}

// REQ-010-004: Section 1 -- Introduction and Education
func TestRunbook_Section1_IntroductionAndEducation(t *testing.T) {
	output := getRunbookOutput(t)

	// Must explain workspace isolation
	assert.Contains(t, output, "isolated from your personal files",
		"Section 1 must explain workspace isolation")

	// Must explain purpose of isolation (protection from malicious code)
	assert.Contains(t, output, "malicious",
		"Section 1 must mention protection from malicious content")

	// Must provide suggested explanation text
	assert.Contains(t, output, "passwords, SSH keys, browser sessions",
		"Section 1 must provide specific examples of protected items")
}

// REQ-010-005: Section 2 -- Prerequisites
func TestRunbook_Section2_Prerequisites(t *testing.T) {
	output := getRunbookOutput(t)

	assert.Contains(t, output, "sd doctor --json",
		"Section 2 must include sd doctor --json command")

	// Remediation table
	assert.Contains(t, output, "brew install lima",
		"Section 2 must include Lima installation remediation")
	assert.Contains(t, output, "Docker Desktop",
		"Section 2 must include Docker installation remediation")
}

// REQ-010-006: Section 3 -- Project Analysis with detection table
func TestRunbook_Section3_DetectionTable(t *testing.T) {
	output := getRunbookOutput(t)

	// The detection table must have at least 10 entries (REQ-010-006)
	detectionEntries := []string{
		"go.mod",
		"package.json",
		"Cargo.toml",
		"pyproject.toml",
		"Dockerfile",
		".github/workflows",
		"Makefile",
		"Gemfile",
		"pom.xml",
		".csproj",
	}

	for _, entry := range detectionEntries {
		assert.Contains(t, output, entry,
			"detection table must include %q", entry)
	}

	// Additional entries beyond the minimum 10
	additionalEntries := []string{
		".tool-versions",
		".python-version",
		".node-version",
		".go-version",
	}
	for _, entry := range additionalEntries {
		assert.Contains(t, output, entry,
			"detection table should include %q", entry)
	}

	// Must instruct agent to use its own judgment
	assert.Contains(t, output, "starting point, not exhaustive",
		"Section 3 must state detection table is not exhaustive")

	// Must instruct agent to ask user for confirmation
	assert.Contains(t, output, "Are there other tools or packages",
		"Section 3 must ask user for confirmation")

	// Must state agent should not assume
	assert.Contains(t, output, "Do not silently assume",
		"Section 3 must instruct agent not to assume user needs")
}

// REQ-010-007: Section 4 -- Backend Selection
func TestRunbook_Section4_BackendSelection(t *testing.T) {
	output := getRunbookOutput(t)

	// Plain-language descriptions of backends
	assert.Contains(t, output, "full virtual machine",
		"Section 4 must describe Lima in plain language")
	assert.Contains(t, output, "container",
		"Section 4 must describe Docker in plain language")

	// Must instruct agent to ask user
	assert.Contains(t, output, "Which would you prefer",
		"Section 4 must ask user which backend they prefer")

	// Must handle single-backend case
	assert.Contains(t, output, "only one backend is available",
		"Section 4 must handle single-backend case")
}

// REQ-010-008: Section 5 -- Environment Configuration
func TestRunbook_Section5_EnvironmentConfiguration(t *testing.T) {
	output := getRunbookOutput(t)

	// Git remote detection
	assert.Contains(t, output, "git remote get-url origin",
		"Section 5 must detect git remote URL")

	// sd init command
	assert.Contains(t, output, "sd init",
		"Section 5 must use sd init")

	// Must show config to user
	assert.Contains(t, output, "Want me to adjust anything",
		"Section 5 must ask user to confirm configuration")

	// Security explanation about network restrictions
	assert.Contains(t, output, "approved websites",
		"Section 5 must explain network restrictions in plain language")
}

// REQ-010-009: Section 6 -- VM Creation
func TestRunbook_Section6_VMCreation(t *testing.T) {
	output := getRunbookOutput(t)

	assert.Contains(t, output, "sd ensure",
		"Section 6 must include sd ensure command")

	// Time estimates
	assert.Contains(t, output, "couple of minutes",
		"Section 6 must include Lima time estimate")
	assert.Contains(t, output, "few seconds",
		"Section 6 must include Docker time estimate")
}

// REQ-010-010: Section 7 -- Credential Setup with security education
func TestRunbook_Section7_CredentialSetup(t *testing.T) {
	output := getRunbookOutput(t)

	// Plain-language explanation of scoped tokens
	assert.Contains(t, output, "only has access to this specific",
		"Section 7 must explain scoped tokens in plain language")

	// Defines "fine-grained personal access token" AFTER plain-language explanation
	assert.Contains(t, output, "fine-grained personal access token",
		"Section 7 must introduce the term 'fine-grained personal access token'")

	// Token creation steps
	assert.Contains(t, output, "github.com/settings/tokens",
		"Section 7 must link to GitHub token creation page")
	assert.Contains(t, output, "Contents",
		"Section 7 must specify Contents permission")
	assert.Contains(t, output, "Pull requests",
		"Section 7 must specify Pull requests permission")

	// Must ask about API keys, not assume
	assert.Contains(t, output, "Do you have API keys",
		"Section 7 must ask about API keys rather than assuming")

	// Credentials injected, not persisted
	assert.Contains(t, output, "never saved to disk inside it",
		"Section 7 must explain credentials are not persisted")

	// export commands
	assert.Contains(t, output, "export GITHUB_TOKEN=",
		"Section 7 must include GITHUB_TOKEN export")
	assert.Contains(t, output, "export ANTHROPIC_API_KEY=",
		"Section 7 must include ANTHROPIC_API_KEY export")
}

// REQ-010-011: Section 8 -- Repo Clone and Verification
func TestRunbook_Section8_RepoCloneAndVerification(t *testing.T) {
	output := getRunbookOutput(t)

	// Clone-not-mount explanation (REQ-010-017)
	assert.Contains(t, output, "cloning the repository inside the workspace instead of sharing",
		"Section 8 must explain clone-not-mount")
	assert.Contains(t, output, "does not have access to your personal files",
		"Section 8 must explain why clone-not-mount protects personal files")

	// Clone command
	assert.Contains(t, output, "sd exec <name> -- git clone",
		"Section 8 must include clone command")

	// Branch checkout question
	assert.Contains(t, output, "specific branch",
		"Section 8 must ask about branch checkout")

	// Verification commands
	assert.Contains(t, output, "go version",
		"Section 8 must include Go verification")
	assert.Contains(t, output, "python3 --version",
		"Section 8 must include Python verification")
	assert.Contains(t, output, "node --version",
		"Section 8 must include Node.js verification")
	assert.Contains(t, output, "rustc --version",
		"Section 8 must include Rust verification")

	// Troubleshooting guidance
	assert.Contains(t, output, "sd logs <name>",
		"Section 8 must include troubleshooting with sd logs")
}

// REQ-010-012: Section 9 -- AI Tool Setup
func TestRunbook_Section9_AIToolSetup(t *testing.T) {
	output := getRunbookOutput(t)

	// Must ask user what tool they want
	assert.Contains(t, output, "Claude Code",
		"Section 9 must mention Claude Code as option")
	assert.Contains(t, output, "Codex",
		"Section 9 must mention Codex as option")
	assert.Contains(t, output, "Gemini CLI",
		"Section 9 must mention Gemini CLI as option")

	// Must not assume
	assert.Contains(t, output, "Do not assume which tool",
		"Section 9 must state not to assume user's tool preference")

	// Must handle case where user doesn't want AI tool inside VM
	assert.Contains(t, output, "does not want an AI tool inside the workspace",
		"Section 9 must handle no-AI-tool case")

	// Re-provision instructions
	assert.Contains(t, output, "sd provision <name>",
		"Section 9 must include re-provision command")
}

// REQ-010-013: Section 10 -- Handoff
func TestRunbook_Section10_Handoff(t *testing.T) {
	output := getRunbookOutput(t)

	// How to connect
	assert.Contains(t, output, "sd connect <name>",
		"Section 10 must tell user how to connect")
	assert.Contains(t, output, "sd c <name>",
		"Section 10 must mention the shorthand alias")

	// Lifecycle commands
	assert.Contains(t, output, "sd stop <name>",
		"Section 10 must mention stop command")
	assert.Contains(t, output, "sd ensure",
		"Section 10 must mention ensure for restart")
	assert.Contains(t, output, "sd destroy <name>",
		"Section 10 must mention destroy command")

	// Credential refresh
	assert.Contains(t, output, "rotate a token",
		"Section 10 must explain credential refresh behavior")

	// File sync
	assert.Contains(t, output, "sd sync from <name>",
		"Section 10 must mention file sync")
}

// REQ-010-014: Security education -- jargon terms must be defined before use.
// All 7 terms from REQ-010-014 are checked: either the term does not appear
// in the runbook, or a plain-language definition appears BEFORE its first use.
func TestRunbook_SecurityEducation_NoUndefinedJargon(t *testing.T) {
	output := getRunbookOutput(t)
	lower := strings.ToLower(output)

	// Each entry: the jargon term to search for (case-insensitive), and a
	// plain-language phrase that MUST appear before the term's first occurrence.
	// If the term does not appear at all, the check passes automatically.
	jargonChecks := []struct {
		term       string // jargon term (searched case-insensitively)
		definition string // plain-language phrase that must precede it (case-insensitive)
		desc       string // assertion message
	}{
		{
			term:       "egress",
			definition: "approved websites",
			desc:       "REQ-010-014: 'egress' must be preceded by plain-language explanation ('approved websites')",
		},
		{
			term:       "fine-grained personal access token",
			definition: "a special password for github",
			desc:       "REQ-010-014: 'fine-grained personal access token' must be preceded by plain-language explanation",
		},
		{
			term:       "personal access token",
			definition: "a special password for github",
			desc:       "REQ-010-014: 'personal access token' must be preceded by plain-language explanation",
		},
		{
			term:       "credential injection",
			definition: "passed into the workspace",
			desc:       "REQ-010-014: 'credential injection' must be preceded by plain-language explanation",
		},
		{
			term:       "attack surface",
			definition: "the number of ways something could go wrong",
			desc:       "REQ-010-014: 'attack surface' must be preceded by plain-language explanation",
		},
		{
			term:       "blast radius",
			definition: "damage is contained",
			desc:       "REQ-010-014: 'blast radius' must be preceded by plain-language explanation",
		},
		{
			term:       "lateral movement",
			definition: "getting from one system to another",
			desc:       "REQ-010-014: 'lateral movement' must be preceded by plain-language explanation",
		},
	}

	for _, tc := range jargonChecks {
		termLower := strings.ToLower(tc.term)
		defLower := strings.ToLower(tc.definition)

		termIdx := strings.Index(lower, termLower)
		if termIdx == -1 {
			// Term does not appear in runbook -- passes automatically
			continue
		}

		defIdx := strings.Index(lower, defLower)
		if defIdx == -1 {
			t.Errorf("%s: term %q appears at position %d but definition phrase %q not found anywhere in runbook",
				tc.desc, tc.term, termIdx, tc.definition)
			continue
		}

		assert.Less(t, defIdx, termIdx,
			"%s: definition %q (pos %d) must appear BEFORE term %q (pos %d)",
			tc.desc, tc.definition, defIdx, tc.term, termIdx)
	}

	// REQ-010-014: Security explanations must appear at point of relevance,
	// not in a separate "security" section. Verify there is no standalone
	// security section header.
	assert.NotContains(t, output, "## Security",
		"security explanations must be woven into steps, not in a separate section")

	// Additional plain-language checks for terms that DO appear:

	// Egress: verify the explanation mentions user-visible/editable list
	assert.Contains(t, output, "controlled by a list",
		"egress explanation must mention that the allowlist is user-visible and editable")

	// Credential passing: verify plain-language explanation exists
	assert.Contains(t, output, "passed into the workspace",
		"credential injection must be explained in plain language")

	// Fine-grained PAT: verify repo scoping is mentioned
	if strings.Contains(lower, "fine-grained personal access token") {
		assert.Contains(t, lower, "specific repositories",
			"fine-grained PAT definition must mention repo scoping")
	}
}

// REQ-010-014: Security education at specific sections (1, 7, 8)
func TestRunbook_SecurityEducation_AtRelevantSections(t *testing.T) {
	output := getRunbookOutput(t)

	// Find section boundaries
	sec1Start := strings.Index(output, "## 1. Introduction and Education")
	sec2Start := strings.Index(output, "## 2. Prerequisites")
	sec7Start := strings.Index(output, "## 7. Credential Setup")
	sec8Start := strings.Index(output, "## 8. Repo Clone and Verification")
	sec9Start := strings.Index(output, "## 9. AI Tool Setup")

	require.NotEqual(t, -1, sec1Start)
	require.NotEqual(t, -1, sec2Start)
	require.NotEqual(t, -1, sec7Start)
	require.NotEqual(t, -1, sec8Start)
	require.NotEqual(t, -1, sec9Start)

	sec1 := output[sec1Start:sec2Start]
	sec7 := output[sec7Start:sec8Start]
	sec8 := output[sec8Start:sec9Start]

	// Section 1: must explain isolation
	assert.Contains(t, sec1, "isolated",
		"Section 1 must contain security education about isolation")

	// Section 7: must explain scoped tokens
	assert.Contains(t, sec7, "only has access to this specific",
		"Section 7 must contain security education about scoped tokens")

	// Section 7: must explain credential injection
	assert.Contains(t, sec7, "never saved to disk",
		"Section 7 must explain credential injection")

	// Section 8: must explain clone-not-mount
	assert.Contains(t, sec8, "cloning the repository inside the workspace",
		"Section 8 must contain clone-not-mount security education")
}

// REQ-010-017: Clone-not-mount as default workflow
func TestRunbook_CloneNotMount(t *testing.T) {
	output := getRunbookOutput(t)

	// Must use git clone inside VM
	assert.Contains(t, output, "git clone",
		"runbook must use git clone inside VM")

	// Must NOT include --mount instructions for project directory
	assert.NotContains(t, output, "--mount",
		"runbook must not include --mount instructions")

	// Must handle uncommitted work
	assert.Contains(t, output, "push",
		"runbook must handle uncommitted local changes by pushing to a branch")
}

// REQ-010-018: Runbook stability -- uses placeholders, references sd guide --agent
func TestRunbook_Stability(t *testing.T) {
	output := getRunbookOutput(t)

	// Must use placeholder names
	assert.Contains(t, output, "<name>",
		"runbook must use <name> placeholder")
	assert.Contains(t, output, "<repo-url>",
		"runbook must use <repo-url> placeholder")

	// Must reference sd guide --agent
	assert.Contains(t, output, "sd guide --agent",
		"runbook must reference sd guide --agent for command reference")
}

// REQ-010-002: --json wraps runbook in JSON envelope with full content
func TestRunbook_JSONOutput_ContainsRunbook(t *testing.T) {
	setupQuickStartTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "quick-start"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	data := result["data"].(map[string]any)
	runbook := data["runbook"].(string)

	// JSON runbook must contain all 10 sections
	assert.Contains(t, runbook, "## 1. Introduction and Education")
	assert.Contains(t, runbook, "## 10. Handoff")
	assert.Contains(t, runbook, "sd guide --agent")
}

// REQ-010-003: Runbook is embedded (not generated dynamically)
func TestRunbook_Embedded_NotEmpty(t *testing.T) {
	// Verify the embedded string is non-trivial
	assert.Greater(t, len(quickStartRunbook), 1000,
		"embedded runbook must be substantial (>1000 chars)")
	assert.True(t, strings.HasPrefix(quickStartRunbook, "# sd Quick Start"),
		"embedded runbook must start with expected title")
}

func TestRapid_NeedsSetupTrueIffComponentMissing(t *testing.T) {
	// Setup is done once at the outer testing.T level; the rapid callback
	// reconfigures the mutable globals on each iteration and resets the
	// memory backend.
	mb := setupQuickStartTest(t)

	rapid.Check(t, func(rt *rapid.T) {
		hasConfig := rapid.Bool().Draw(rt, "hasConfig")
		hasVM := rapid.Bool().Draw(rt, "hasVM")
		vmRunning := rapid.Bool().Draw(rt, "vmRunning")
		hasGHToken := rapid.Bool().Draw(rt, "hasGHToken")
		hasAnthropicKey := rapid.Bool().Draw(rt, "hasAnthropicKey")
		repoCloned := rapid.Bool().Draw(rt, "repoCloned")

		// Enforce logical constraints: can't have VM without config,
		// can't be running without existing, can't clone without running.
		if !hasConfig {
			hasVM = false
		}
		if !hasVM {
			vmRunning = false
		}
		if !vmRunning {
			repoCloned = false
		}

		// Reset memory backend state for this iteration
		mb.Reset()
		resetQuickStartFlags(t)

		ctx := context.Background()

		if hasConfig {
			findProjectConfigFunc = func(_ string) (string, *config.ProjectConfig, error) {
				return "/tmp/.sd.yaml", &config.ProjectConfig{
					Name:    "test-vm",
					Backend: "memory",
					Modules: []string{"base"},
				}, nil
			}
		} else {
			findProjectConfigFunc = func(_ string) (string, *config.ProjectConfig, error) {
				return "", nil, nil
			}
		}

		if hasVM {
			if err := mb.Create(ctx, "test-vm", backend.VMConfig{
				CPUs: 2, Memory: "4GiB", Disk: "50GiB",
			}); err != nil {
				rt.Fatal(err)
			}
			if !vmRunning {
				if err := mb.Stop(ctx, "test-vm"); err != nil {
					rt.Fatal(err)
				}
			}
		}

		getenvFunc = func(key string) string {
			switch key {
			case "GITHUB_TOKEN":
				if hasGHToken {
					return "ghp_test"
				}
			case "ANTHROPIC_API_KEY":
				if hasAnthropicKey {
					return "sk-ant-test"
				}
			}
			return ""
		}

		if repoCloned {
			mb.SetExecHandler(func(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
				return backend.ExecResult{ExitCode: 0}, nil
			})
		} else {
			mb.SetExecHandler(func(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
				return backend.ExecResult{ExitCode: 1}, nil
			})
		}

		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			rt.Fatal(err)
		}
		os.Stdout = w

		root := RootCmd()
		root.SetArgs([]string{"quick-start", "--check"})
		execErr := root.Execute()

		w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)

		if execErr != nil {
			rt.Fatalf("execute failed: %v", execErr)
		}

		var result map[string]any
		if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
			rt.Fatalf("JSON parse failed: %v -- output: %s", err, buf.String())
		}

		data := result["data"].(map[string]any)
		needsSetup := data["needs_setup"].(bool)

		// All components must be present for needs_setup to be false
		allPresent := hasConfig && hasVM && vmRunning && hasGHToken && hasAnthropicKey && repoCloned
		if allPresent && needsSetup {
			rt.Fatalf("needs_setup is true but all components are present")
		}
		if !allPresent && !needsSetup {
			rt.Fatalf("needs_setup is false but components are missing: "+
				"config=%v vm=%v running=%v gh=%v anthropic=%v repo=%v",
				hasConfig, hasVM, vmRunning, hasGHToken, hasAnthropicKey, repoCloned)
		}
	})
}
func TestQuickStartCommand_CheckConfigError_SingleIssue(t *testing.T) {
	setupQuickStartTest(t)

	// findProjectConfigFunc returns an error (e.g., malformed YAML)
	findProjectConfigFunc = func(_ string) (string, *config.ProjectConfig, error) {
		return "", nil, fmt.Errorf("yaml: line 3: mapping values are not allowed here")
	}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"quick-start", "--check"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	data := result["data"].(map[string]any)
	assert.True(t, data["needs_setup"].(bool), "needs_setup must be true on config error")
	assert.False(t, data["sd_yaml_exists"].(bool), "sd_yaml_exists must be false on config error")

	issues := data["issues"].([]any)

	// Count how many issues mention ".sd.yaml"
	sdYamlIssueCount := 0
	foundErrorIssue := false
	for _, issue := range issues {
		s, ok := issue.(string)
		if !ok {
			continue
		}
		if s == "No .sd.yaml found in this directory -- run sd init to create one" {
			sdYamlIssueCount++
		}
		if s == "Error reading .sd.yaml: yaml: line 3: mapping values are not allowed here" {
			foundErrorIssue = true
			sdYamlIssueCount++
		}
	}

	assert.True(t, foundErrorIssue, "issues must contain the config read error")
	assert.Equal(t, 1, sdYamlIssueCount,
		"must have exactly one .sd.yaml-related issue (the error), not both error AND 'not found'")
}

func TestQuickStartCommand_CheckPackagesDeclared(t *testing.T) {
	mb := setupQuickStartTest(t)
	ctx := context.Background()

	require.NoError(t, mb.Create(ctx, "pkg-test", backend.VMConfig{
		CPUs: 2, Memory: "4GiB", Disk: "50GiB",
	}))

	findProjectConfigFunc = func(_ string) (string, *config.ProjectConfig, error) {
		return "/tmp/.sd.yaml", &config.ProjectConfig{
			Name:    "pkg-test",
			Backend: "memory",
			Modules: []string{"base", "golang", "nodejs"},
			Packages: &config.PackageConfig{
				Apt:   []string{"curl", "jq", "tree"},
				Pip:   []string{"requests"},
				Npm:   []string{"typescript", "eslint"},
				Go:    []string{"golang.org/x/tools/gopls@latest"},
				Cargo: []string{},
			},
		}, nil
	}

	getenvFunc = func(key string) string {
		if key == "GITHUB_TOKEN" || key == "ANTHROPIC_API_KEY" {
			return "set"
		}
		return ""
	}
	mb.SetExecHandler(func(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
		return backend.ExecResult{ExitCode: 0}, nil
	})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"quick-start", "--check"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	data := result["data"].(map[string]any)
	pkgs := data["packages_declared"].(map[string]any)

	assert.Equal(t, float64(3), pkgs["apt"], "apt should have 3 packages")
	assert.Equal(t, float64(1), pkgs["pip"], "pip should have 1 package")
	assert.Equal(t, float64(2), pkgs["npm"], "npm should have 2 packages")
	assert.Equal(t, float64(1), pkgs["go"], "go should have 1 package")
	_, hasCargo := pkgs["cargo"]
	assert.False(t, hasCargo, "cargo should not appear when empty")
}

func TestQuickStartCommand_CheckPackagesNil(t *testing.T) {
	mb := setupQuickStartTest(t)
	ctx := context.Background()

	require.NoError(t, mb.Create(ctx, "no-pkg", backend.VMConfig{
		CPUs: 2, Memory: "4GiB", Disk: "50GiB",
	}))

	findProjectConfigFunc = func(_ string) (string, *config.ProjectConfig, error) {
		return "/tmp/.sd.yaml", &config.ProjectConfig{
			Name:    "no-pkg",
			Backend: "memory",
			Modules: []string{"base"},
			// Packages is nil
		}, nil
	}

	getenvFunc = func(key string) string {
		if key == "GITHUB_TOKEN" || key == "ANTHROPIC_API_KEY" {
			return "set"
		}
		return ""
	}
	mb.SetExecHandler(func(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
		return backend.ExecResult{ExitCode: 0}, nil
	})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"quick-start", "--check"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	data := result["data"].(map[string]any)
	pkgs := data["packages_declared"].(map[string]any)
	assert.Empty(t, pkgs, "packages_declared should be empty when Packages is nil")
}

func TestRapid_IssuesEmptyIffNeedsSetupFalse(t *testing.T) {
	mb := setupQuickStartTest(t)

	rapid.Check(t, func(rt *rapid.T) {
		hasConfig := rapid.Bool().Draw(rt, "hasConfig")
		hasVM := rapid.Bool().Draw(rt, "hasVM")
		vmRunning := rapid.Bool().Draw(rt, "vmRunning")
		hasGHToken := rapid.Bool().Draw(rt, "hasGHToken")
		hasAnthropicKey := rapid.Bool().Draw(rt, "hasAnthropicKey")
		repoCloned := rapid.Bool().Draw(rt, "repoCloned")

		// Logical constraints
		if !hasConfig {
			hasVM = false
		}
		if !hasVM {
			vmRunning = false
		}
		if !vmRunning {
			repoCloned = false
		}

		mb.Reset()
		resetQuickStartFlags(t)

		ctx := context.Background()

		if hasConfig {
			// Randomize package counts to exercise packages_declared path
			numApt := rapid.IntRange(0, 5).Draw(rt, "numApt")
			aptPkgs := make([]string, numApt)
			for i := range aptPkgs {
				aptPkgs[i] = fmt.Sprintf("pkg-%d", i)
			}
			var pkgCfg *config.PackageConfig
			if numApt > 0 {
				pkgCfg = &config.PackageConfig{Apt: aptPkgs}
			}
			findProjectConfigFunc = func(_ string) (string, *config.ProjectConfig, error) {
				return "/tmp/.sd.yaml", &config.ProjectConfig{
					Name:     "test-vm",
					Backend:  "memory",
					Modules:  []string{"base"},
					Packages: pkgCfg,
				}, nil
			}
		} else {
			findProjectConfigFunc = func(_ string) (string, *config.ProjectConfig, error) {
				return "", nil, nil
			}
		}

		if hasVM {
			if err := mb.Create(ctx, "test-vm", backend.VMConfig{
				CPUs: 2, Memory: "4GiB", Disk: "50GiB",
			}); err != nil {
				rt.Fatal(err)
			}
			if !vmRunning {
				if err := mb.Stop(ctx, "test-vm"); err != nil {
					rt.Fatal(err)
				}
			}
		}

		getenvFunc = func(key string) string {
			switch key {
			case "GITHUB_TOKEN":
				if hasGHToken {
					return "ghp_test"
				}
			case "ANTHROPIC_API_KEY":
				if hasAnthropicKey {
					return "sk-ant-test"
				}
			}
			return ""
		}

		if repoCloned {
			mb.SetExecHandler(func(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
				return backend.ExecResult{ExitCode: 0}, nil
			})
		} else {
			mb.SetExecHandler(func(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
				return backend.ExecResult{ExitCode: 1}, nil
			})
		}

		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			rt.Fatal(err)
		}
		os.Stdout = w

		root := RootCmd()
		root.SetArgs([]string{"quick-start", "--check"})
		execErr := root.Execute()

		w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)

		if execErr != nil {
			rt.Fatalf("execute failed: %v", execErr)
		}

		var result map[string]any
		if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
			rt.Fatalf("JSON parse failed: %v -- output: %s", err, buf.String())
		}

		data := result["data"].(map[string]any)
		needsSetup := data["needs_setup"].(bool)
		issues := data["issues"].([]any)

		// Core property: issues empty iff needs_setup false
		if needsSetup && len(issues) == 0 {
			rt.Fatalf("needs_setup=true but issues is empty")
		}
		if !needsSetup && len(issues) > 0 {
			rt.Fatalf("needs_setup=false but issues has %d entries: %v", len(issues), issues)
		}
	})
}

