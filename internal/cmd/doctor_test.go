// Package cmd provides tests for the doctor command.
// REQ-002-007: Diagnostic Commands -- doctor
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
	"sd/internal/backend"
	"sd/internal/config"
	"sd/internal/ui"
)

// mockDoctorPaths is a digital twin for binary lookups.
// Keys are binary names, values are their paths (or empty string for not found).
type mockDoctorPaths map[string]string

// mockDoctorBackend is a digital twin of a backend for doctor command testing.
// It implements backend.Backend with configurable Available behavior.
type mockDoctorBackend struct {
	available bool
}

func (m *mockDoctorBackend) Name() string                                                { return "mock" }
func (m *mockDoctorBackend) Available() error {
	if !m.available {
		return backend.ErrBackendNotAvailable
	}
	return nil
}
func (m *mockDoctorBackend) Create(_ context.Context, _ string, _ backend.VMConfig) error { return nil }
func (m *mockDoctorBackend) Start(_ context.Context, _ string) error                     { return nil }
func (m *mockDoctorBackend) Stop(_ context.Context, _ string) error                      { return nil }
func (m *mockDoctorBackend) Destroy(_ context.Context, _ string) error                   { return nil }
func (m *mockDoctorBackend) Status(_ context.Context, _ string) (backend.VMStatus, error) {
	return backend.StatusRunning, nil
}
func (m *mockDoctorBackend) List(_ context.Context) ([]backend.VMInfo, error) {
	return nil, nil
}
func (m *mockDoctorBackend) SSHConfig(_ context.Context, _ string) (backend.SSHConfig, error) {
	return backend.SSHConfig{}, nil
}
func (m *mockDoctorBackend) Exec(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
	return backend.ExecResult{}, nil
}

// execError simulates exec.LookPath error for digital twin.
type execError struct {
	name string
}

func (e *execError) Error() string {
	return "executable file not found in $PATH: " + e.name
}

// setupDoctorTest configures the test environment and overrides lookPath and getBackendFunc.
func setupDoctorTest(t *testing.T, paths mockDoctorPaths, backendAvail bool) {
	t.Helper()
	tmpDir := t.TempDir()
	// REQ-005-016: SD_HOME must be 0700 for permission checks to pass
	os.Chmod(tmpDir, 0700)
	t.Setenv("SD_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)
	resetRootFlags(t)

	origLookPath := lookPath
	lookPath = func(name string) (string, error) {
		if p, ok := paths[name]; ok {
			if p == "" {
				return "", &execError{name: name}
			}
			return p, nil
		}
		return "", &execError{name: name}
	}
	t.Cleanup(func() { lookPath = origLookPath })

	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return &mockDoctorBackend{available: backendAvail}, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })
}

func TestDoctorCommand_Registered(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "doctor" {
			found = true
			assert.Equal(t, "diagnostics", cmd.GroupID)
			break
		}
	}
	assert.True(t, found, "doctor command must be registered")
}

func TestDoctorCommand_HumanOutput_AllPass(t *testing.T) {
	paths := mockDoctorPaths{
		"limactl": "/usr/local/bin/limactl",
		"ssh":     "/usr/bin/ssh",
		"tmux":    "/usr/bin/tmux",
		"rsync":   "/usr/bin/rsync",
	}
	setupDoctorTest(t, paths, true)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"doctor"})
	err := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	require.NoError(t, err, "doctor should succeed when all checks pass")

	assert.Contains(t, output, "\u2713")   // checkmark
	assert.NotContains(t, output, "\u2717") // no ballot X
	assert.Contains(t, output, "binary_limactl")
	assert.Contains(t, output, "binary_ssh")
	assert.Contains(t, output, "binary_tmux")
	assert.Contains(t, output, "binary_rsync")
	assert.Contains(t, output, "config")
	assert.Contains(t, output, "backend")
}

func TestDoctorCommand_HumanOutput_SomeFail(t *testing.T) {
	paths := mockDoctorPaths{
		"limactl": "", // missing
		"ssh":     "/usr/bin/ssh",
		"tmux":    "/usr/bin/tmux",
		"rsync":   "/usr/bin/rsync",
	}
	setupDoctorTest(t, paths, false)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"doctor"})
	err := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok, "error must be CLIError")
	assert.Equal(t, "doctor_check_failed", cliErr.Code)

	assert.Contains(t, output, "\u2717") // ballot X for limactl
	assert.Contains(t, output, "binary_limactl")
	assert.Contains(t, output, "not found in PATH")
}

func TestDoctorCommand_JSONOutput_AllPass(t *testing.T) {
	paths := mockDoctorPaths{
		"limactl": "/usr/local/bin/limactl",
		"ssh":     "/usr/bin/ssh",
		"tmux":    "/usr/bin/tmux",
		"rsync":   "/usr/bin/rsync",
	}
	setupDoctorTest(t, paths, true)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "doctor"})
	err := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err, "output must be valid JSON: %s", output)

	assert.True(t, result["ok"].(bool))

	data, ok := result["data"].([]any)
	require.True(t, ok, "data must be a JSON array")
	assert.Len(t, data, 10, "should have 4 binary + 1 config + 1 backend + 1 sd_home_permissions + 1 git_credentials + 1 project_security_config + 1 ssh_fragment_security checks")

	for _, item := range data {
		check := item.(map[string]any)
		assert.Contains(t, check, "name")
		assert.Contains(t, check, "status")
		assert.Equal(t, "pass", check["status"], "check %s should pass", check["name"])
	}
}

func TestDoctorCommand_JSONOutput_SomeFail(t *testing.T) {
	paths := mockDoctorPaths{
		"limactl": "", // missing
		"ssh":     "/usr/bin/ssh",
		"tmux":    "",
		"rsync":   "/usr/bin/rsync",
	}
	setupDoctorTest(t, paths, false)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "doctor"})
	err := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// REQ-002-007: JSON mode must return error when checks fail
	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok, "error must be CLIError")
	assert.Equal(t, "doctor_check_failed", cliErr.Code)

	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err)

	assert.False(t, result["ok"].(bool), "JSON envelope ok must be false when checks fail")

	data := result["data"].([]any)

	passCount := 0
	failCount := 0
	for _, item := range data {
		check := item.(map[string]any)
		switch check["status"].(string) {
		case "pass":
			passCount++
		case "fail":
			failCount++
		}
	}
	assert.Equal(t, 3, failCount, "limactl, tmux, and backend should fail")
	assert.Equal(t, 7, passCount, "ssh, rsync, config, sd_home_permissions, git_credentials, project_security_config, ssh_fragment_security checks pass")
}

func TestDoctorCommand_NoConfigRequired(t *testing.T) {
	// REQ-002-015: doctor must succeed even without a config file
	paths := mockDoctorPaths{
		"limactl": "/usr/local/bin/limactl",
		"ssh":     "/usr/bin/ssh",
		"tmux":    "/usr/bin/tmux",
		"rsync":   "/usr/bin/rsync",
	}
	setupDoctorTest(t, paths, true)

	root := RootCmd()
	root.SetArgs([]string{"doctor"})
	err := root.Execute()
	assert.NoError(t, err, "doctor must succeed without config file")
}

func TestDoctorCommand_RejectsExtraArgs(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	root.SetArgs([]string{"doctor", "extra-arg"})
	err := root.Execute()
	assert.Error(t, err, "doctor should reject extra arguments")
}

// Unit tests for checkBinary
func TestCheckBinary_Found(t *testing.T) {
	orig := lookPath
	lookPath = func(name string) (string, error) {
		return "/usr/bin/" + name, nil
	}
	defer func() { lookPath = orig }()

	check := checkBinary("ssh")
	assert.Equal(t, "binary_ssh", check.Name)
	assert.Equal(t, "pass", check.Status)
	assert.Contains(t, check.Message, "/usr/bin/ssh")
}

func TestCheckBinary_NotFound(t *testing.T) {
	orig := lookPath
	lookPath = func(name string) (string, error) {
		return "", &execError{name: name}
	}
	defer func() { lookPath = orig }()

	check := checkBinary("limactl")
	assert.Equal(t, "binary_limactl", check.Name)
	assert.Equal(t, "fail", check.Status)
	assert.Contains(t, check.Message, "not found in PATH")
}

// Unit tests for formatDoctorOutput
func TestFormatDoctorOutput(t *testing.T) {
	checks := []doctorCheck{
		{Name: "binary_ssh", Status: "pass", Message: "found at /usr/bin/ssh"},
		{Name: "binary_limactl", Status: "fail", Message: "not found in PATH"},
	}

	output := formatDoctorOutput(checks)

	assert.Contains(t, output, "\u2713 binary_ssh: found at /usr/bin/ssh")
	assert.Contains(t, output, "\u2717 binary_limactl: not found in PATH")
}

func TestFormatDoctorOutput_Empty(t *testing.T) {
	output := formatDoctorOutput(nil)
	assert.Empty(t, output)
}

func TestFormatDoctorOutput_AllPass(t *testing.T) {
	checks := []doctorCheck{
		{Name: "binary_ssh", Status: "pass", Message: "found at /usr/bin/ssh"},
		{Name: "config", Status: "pass", Message: "configuration valid"},
	}
	output := formatDoctorOutput(checks)
	for _, c := range checks {
		assert.Contains(t, output, "\u2713 "+c.Name)
		assert.NotContains(t, output, "\u2717")
	}
}

func TestFormatDoctorOutput_AllFail(t *testing.T) {
	checks := []doctorCheck{
		{Name: "binary_ssh", Status: "fail", Message: "not found"},
		{Name: "config", Status: "fail", Message: "invalid"},
	}
	output := formatDoctorOutput(checks)
	for _, c := range checks {
		assert.Contains(t, output, "\u2717 "+c.Name)
		assert.NotContains(t, output, "\u2713")
	}
}

// Property: JSON output from doctor always parses as valid JSON with required structure
func TestProperty_DoctorJSONAlwaysValid(t *testing.T) {
	cases := []struct {
		name    string
		paths   mockDoctorPaths
		backend bool
	}{
		{
			"all_present",
			mockDoctorPaths{"limactl": "/usr/bin/limactl", "ssh": "/usr/bin/ssh", "tmux": "/usr/bin/tmux", "rsync": "/usr/bin/rsync"},
			true,
		},
		{
			"all_missing",
			mockDoctorPaths{"limactl": "", "ssh": "", "tmux": "", "rsync": ""},
			false,
		},
		{
			"mixed",
			mockDoctorPaths{"limactl": "/usr/bin/limactl", "ssh": "", "tmux": "/usr/bin/tmux", "rsync": ""},
			true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupDoctorTest(t, tc.paths, tc.backend)

			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"--json", "doctor"})
			root.Execute() // ignore error in JSON mode

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			buf.ReadFrom(r)

			var result map[string]any
			err := json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err, "JSON must always parse for case %s: %s", tc.name, buf.String())
			assert.Contains(t, result, "ok")
			assert.Contains(t, result, "data")

			data, ok := result["data"].([]any)
			require.True(t, ok, "data must be an array")
			assert.Len(t, data, 10, "always 10 checks")

			for _, item := range data {
				check := item.(map[string]any)
				assert.Contains(t, check, "name")
				assert.Contains(t, check, "status")
				status := check["status"].(string)
				assert.Contains(t, []string{"pass", "fail"}, status, "status must be pass or fail")
			}
		})
	}
}

// --- Project Security Config Tests (REQ-004-029) ---

func TestCheckProjectSecurityConfig_NoLoader(t *testing.T) {
	newRootTestEnv(t)
	// Ensure loader is nil for this test
	origLoader := loader
	loader = nil
	defer func() { loader = origLoader }()

	check := checkProjectSecurityConfig()
	assert.Equal(t, "project_security_config", check.Name)
	assert.Equal(t, "pass", check.Status)
	assert.Contains(t, check.Message, "no loader")
}

func TestCheckProjectSecurityConfig_NoProjectDir(t *testing.T) {
	newRootTestEnv(t)
	// Loader exists but ProjectDir is empty
	l := config.NewLoader()
	l.Load()
	origLoader := loader
	loader = l
	defer func() { loader = origLoader }()

	check := checkProjectSecurityConfig()
	assert.Equal(t, "project_security_config", check.Name)
	assert.Equal(t, "pass", check.Status)
	assert.Contains(t, check.Message, "no project directory")
}

func TestCheckProjectSecurityConfig_NoConfigFile(t *testing.T) {
	newRootTestEnv(t)
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "myproject")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))

	l := config.NewLoader(config.WithProjectDir(projectDir))
	require.NoError(t, l.Load())
	origLoader := loader
	loader = l
	defer func() { loader = origLoader }()

	// Override readProjectConfigFunc with digital twin that returns nil (no file)
	origRead := readProjectConfigFunc
	readProjectConfigFunc = func(path string) (map[string]any, error) {
		return nil, nil
	}
	defer func() { readProjectConfigFunc = origRead }()

	check := checkProjectSecurityConfig()
	assert.Equal(t, "project_security_config", check.Name)
	assert.Equal(t, "pass", check.Status)
	assert.Contains(t, check.Message, "no project-level config")
}

func TestCheckProjectSecurityConfig_NoSecurityKeys(t *testing.T) {
	newRootTestEnv(t)
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "myproject")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))

	l := config.NewLoader(config.WithProjectDir(projectDir))
	require.NoError(t, l.Load())
	origLoader := loader
	loader = l
	defer func() { loader = origLoader }()

	// Digital twin: config with no security keys
	origRead := readProjectConfigFunc
	readProjectConfigFunc = func(path string) (map[string]any, error) {
		return map[string]any{
			"defaults": map[string]any{
				"cpus":  4,
				"memory": "8GiB",
			},
		}, nil
	}
	defer func() { readProjectConfigFunc = origRead }()

	check := checkProjectSecurityConfig()
	assert.Equal(t, "project_security_config", check.Name)
	assert.Equal(t, "pass", check.Status)
	assert.Contains(t, check.Message, "no security keys")
}

func TestCheckProjectSecurityConfig_HasSecurityKeys(t *testing.T) {
	newRootTestEnv(t)
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "myproject")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))

	l := config.NewLoader(config.WithProjectDir(projectDir))
	require.NoError(t, l.Load())
	origLoader := loader
	loader = l
	defer func() { loader = origLoader }()

	// Digital twin: config WITH security keys
	origRead := readProjectConfigFunc
	readProjectConfigFunc = func(path string) (map[string]any, error) {
		return map[string]any{
			"defaults": map[string]any{"cpus": 4},
			"security": map[string]any{
				"mount_policy":      "readonly",
				"egress_allowlist": []string{"custom.example.com"},
			},
		}, nil
	}
	defer func() { readProjectConfigFunc = origRead }()

	check := checkProjectSecurityConfig()
	assert.Equal(t, "project_security_config", check.Name)
	assert.Equal(t, "fail", check.Status)
	assert.Contains(t, check.Message, "security.mount_policy")
	assert.Contains(t, check.Message, "security.egress_allowlist")
	assert.Contains(t, check.Message, "REQ-004-029")
}

func TestCheckProjectSecurityConfig_SecurityNotMap(t *testing.T) {
	newRootTestEnv(t)
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "myproject")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))

	l := config.NewLoader(config.WithProjectDir(projectDir))
	require.NoError(t, l.Load())
	origLoader := loader
	loader = l
	defer func() { loader = origLoader }()

	// Digital twin: security key is a string, not a map
	origRead := readProjectConfigFunc
	readProjectConfigFunc = func(path string) (map[string]any, error) {
		return map[string]any{
			"security": "some-string-value",
		}, nil
	}
	defer func() { readProjectConfigFunc = origRead }()

	check := checkProjectSecurityConfig()
	assert.Equal(t, "project_security_config", check.Name)
	assert.Equal(t, "fail", check.Status)
	assert.Contains(t, check.Message, "security")
	assert.Contains(t, check.Message, "REQ-004-029")
}

func TestCheckProjectSecurityConfig_ReadError(t *testing.T) {
	newRootTestEnv(t)
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "myproject")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))

	l := config.NewLoader(config.WithProjectDir(projectDir))
	require.NoError(t, l.Load())
	origLoader := loader
	loader = l
	defer func() { loader = origLoader }()

	// Digital twin: simulate read error
	origRead := readProjectConfigFunc
	readProjectConfigFunc = func(path string) (map[string]any, error) {
		return nil, fmt.Errorf("permission denied")
	}
	defer func() { readProjectConfigFunc = origRead }()

	check := checkProjectSecurityConfig()
	assert.Equal(t, "project_security_config", check.Name)
	assert.Equal(t, "fail", check.Status)
	assert.Contains(t, check.Message, "cannot read project config")
}

func TestCheckProjectSecurityConfig_InDoctorOutput(t *testing.T) {
	paths := mockDoctorPaths{
		"limactl": "/usr/local/bin/limactl",
		"ssh":     "/usr/bin/ssh",
		"tmux":    "/usr/bin/tmux",
		"rsync":   "/usr/bin/rsync",
	}
	setupDoctorTest(t, paths, true)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "doctor"})
	root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)

	var result map[string]any
	err := json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	data := result["data"].([]any)
	found := false
	for _, item := range data {
		check := item.(map[string]any)
		if check["name"] == "project_security_config" {
			found = true
			assert.Equal(t, "pass", check["status"])
			break
		}
	}
	assert.True(t, found, "project_security_config check must appear in doctor output")
}

// Property: checkProjectSecurityConfig always returns valid name and status
func TestProperty_ProjectSecurityConfigAlwaysValid(t *testing.T) {
	newRootTestEnv(t)

	rapid.Check(t, func(rt *rapid.T) {
		hasLoader := rapid.Bool().Draw(rt, "hasLoader")
		hasProjectDir := rapid.Bool().Draw(rt, "hasProjectDir")
		hasSecurity := rapid.Bool().Draw(rt, "hasSecurity")

		if hasLoader {
			if hasProjectDir {
				tmpDir := t.TempDir()
				projectDir := filepath.Join(tmpDir, "proj")
				os.MkdirAll(projectDir, 0o755)
				l := config.NewLoader(config.WithProjectDir(projectDir))
				l.Load()
				origLoader := loader
				loader = l
				defer func() { loader = origLoader }()

				origRead := readProjectConfigFunc
				readProjectConfigFunc = func(path string) (map[string]any, error) {
					if hasSecurity {
						return map[string]any{
							"security": map[string]any{
								"mount_policy": "readonly",
							},
						}, nil
					}
					return map[string]any{"defaults": map[string]any{"cpus": 4}}, nil
				}
				defer func() { readProjectConfigFunc = origRead }()
			} else {
				l := config.NewLoader()
				l.Load()
				origLoader := loader
				loader = l
				defer func() { loader = origLoader }()
			}
		}
		// If !hasLoader, loader stays nil

		check := checkProjectSecurityConfig()
		assert.Equal(t, "project_security_config", check.Name)
		assert.Contains(t, []string{"pass", "fail"}, check.Status)
	})
}

// Property: security keys always cause fail, empty or no security always passes
func TestProperty_ProjectSecurityConfigSecurityKeysAlwaysFail(t *testing.T) {
	newRootTestEnv(t)

	rapid.Check(t, func(rt *rapid.T) {
		tmpDir := t.TempDir()
		projectDir := filepath.Join(tmpDir, "proj")
		os.MkdirAll(projectDir, 0o755)

		l := config.NewLoader(config.WithProjectDir(projectDir))
		l.Load()
		origLoader := loader
		loader = l
		defer func() { loader = origLoader }()

		// Generate random security keys
		nKeys := rapid.IntRange(1, 5).Draw(rt, "nKeys")
		securityMap := make(map[string]any)
		for i := 0; i < nKeys; i++ {
			key := rapid.StringMatching(`[a-z_]{3,10}`).Draw(rt, "key")
			securityMap[key] = "value"
		}

		origRead := readProjectConfigFunc
		readProjectConfigFunc = func(path string) (map[string]any, error) {
			return map[string]any{"security": securityMap}, nil
		}
		defer func() { readProjectConfigFunc = origRead }()

		check := checkProjectSecurityConfig()
		assert.Equal(t, "fail", check.Status, "any security keys should cause fail")
		assert.Contains(t, check.Message, "REQ-004-029")
	})
}

// Property: human output always contains every check name
func TestProperty_DoctorHumanContainsAllChecks(t *testing.T) {
	paths := mockDoctorPaths{
		"limactl": "/usr/bin/limactl",
		"ssh":     "/usr/bin/ssh",
		"tmux":    "/usr/bin/tmux",
		"rsync":   "/usr/bin/rsync",
	}
	setupDoctorTest(t, paths, true)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"doctor"})
	root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	for _, bin := range requiredBinaries {
		assert.Contains(t, output, "binary_"+bin, "output must contain binary_%s check", bin)
	}
	assert.Contains(t, output, "config")
	assert.Contains(t, output, "backend")
	assert.Contains(t, output, "git_credentials")
}

// Property: error code is always snake_case
func TestProperty_DoctorErrorCodeSnakeCase(t *testing.T) {
	paths := mockDoctorPaths{
		"limactl": "", // cause failure
		"ssh":     "/usr/bin/ssh",
		"tmux":    "/usr/bin/tmux",
		"rsync":   "/usr/bin/rsync",
	}
	setupDoctorTest(t, paths, false)

	root := RootCmd()
	root.SetArgs([]string{"doctor"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok, "error must be CLIError")
	assert.Equal(t, "doctor_check_failed", cliErr.Code)
}

// --- Git Credential Cache Prevention Tests (REQ-004-030) ---

func TestCheckGitCredentials_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	check := checkGitCredentials()
	assert.Equal(t, "git_credentials", check.Name)
	assert.Equal(t, "pass", check.Status)
	assert.Contains(t, check.Message, "no cached git credentials")
}

func TestCheckGitCredentials_FileExists(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Create ~/.git-credentials
	err := os.WriteFile(tmpDir+"/.git-credentials", []byte("https://user:pass@example.com"), 0o600)
	require.NoError(t, err)

	check := checkGitCredentials()
	assert.Equal(t, "git_credentials", check.Name)
	assert.Equal(t, "fail", check.Status)
	assert.Contains(t, check.Message, ".git-credentials exists")
	assert.Contains(t, check.Message, "security risk")
}

func TestCheckGitCredentials_StatOverride(t *testing.T) {
	// Digital twin: override statPath to simulate file existence without creating one
	origStat := statPath
	statPath = func(name string) (os.FileInfo, error) {
		if len(name) > 0 && name[len(name)-1:] == "s" {
			// Simulate file exists
			return nil, nil
		}
		return nil, &os.PathError{Op: "stat", Path: name, Err: os.ErrNotExist}
	}
	defer func() { statPath = origStat }()

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	check := checkGitCredentials()
	assert.Equal(t, "git_credentials", check.Name)
}

func TestDoctorCommand_GitCredentialsInOutput(t *testing.T) {
	// REQ-004-030: git_credentials check appears in doctor output
	paths := mockDoctorPaths{
		"limactl": "/usr/local/bin/limactl",
		"ssh":     "/usr/bin/ssh",
		"tmux":    "/usr/bin/tmux",
		"rsync":   "/usr/bin/rsync",
	}
	setupDoctorTest(t, paths, true)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "doctor"})
	root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)

	var result map[string]any
	err := json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	data := result["data"].([]any)
	found := false
	for _, item := range data {
		check := item.(map[string]any)
		if check["name"] == "git_credentials" {
			found = true
			assert.Equal(t, "pass", check["status"])
			break
		}
	}
	assert.True(t, found, "git_credentials check must appear in doctor output")
}

func TestDoctorCommand_GitCredentialsFailInJSON(t *testing.T) {
	// REQ-004-030: doctor reports git_credentials failure when file exists
	paths := mockDoctorPaths{
		"limactl": "/usr/local/bin/limactl",
		"ssh":     "/usr/bin/ssh",
		"tmux":    "/usr/bin/tmux",
		"rsync":   "/usr/bin/rsync",
	}
	setupDoctorTest(t, paths, true)

	// Create ~/.git-credentials in the test HOME
	home := os.Getenv("HOME")
	err := os.WriteFile(home+"/.git-credentials", []byte("https://token@github.com"), 0o600)
	require.NoError(t, err)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "doctor"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	// REQ-002-007: JSON mode must return error when checks fail
	require.Error(t, execErr)

	var buf bytes.Buffer
	buf.ReadFrom(r)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	assert.False(t, result["ok"].(bool), "JSON envelope ok must be false when git_credentials fails")

	data := result["data"].([]any)
	found := false
	for _, item := range data {
		check := item.(map[string]any)
		if check["name"] == "git_credentials" {
			found = true
			assert.Equal(t, "fail", check["status"])
			assert.Contains(t, check["message"], ".git-credentials exists")
			break
		}
	}
	assert.True(t, found, "git_credentials check must appear")
}

// Property: git_credentials check always has correct name and valid status
func TestProperty_GitCredentialsCheckAlwaysValid(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	origStat := statPath
	statPath = func(name string) (os.FileInfo, error) {
		return nil, &os.PathError{Op: "stat", Path: name, Err: os.ErrNotExist}
	}
	defer func() { statPath = origStat }()

	rapid.Check(t, func(t *rapid.T) {
		check := checkGitCredentials()
		if check.Name != "git_credentials" {
			t.Fatalf("expected name git_credentials, got %q", check.Name)
		}
		if check.Status != "pass" && check.Status != "fail" {
			t.Fatalf("expected pass or fail, got %q", check.Status)
		}
	})
}
