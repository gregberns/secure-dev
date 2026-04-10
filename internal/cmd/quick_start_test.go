// Package cmd provides tests for the quick-start command.
// REQ-010-001: Quick-Start Command Registration
// REQ-010-002: Quick-Start Flags
// REQ-010-015: Check Flag -- Setup Assessment
// NOTE: Tests use global getBackendFunc, getenvFunc, findProjectConfigFunc,
// getWorkingDir -- do not use t.Parallel().
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
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
	assert.Contains(t, output, "Quick-start runbook will be here")
	assert.Contains(t, output, "sd guide --agent")
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
		if s, ok := issue.(string); ok && s == "No .sd.yaml found -- run sd quick-start to set up" {
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
