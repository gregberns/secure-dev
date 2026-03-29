// Package cmd provides tests for the doctor command.
// REQ-002-007: Diagnostic Commands -- doctor
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/backend"
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
	assert.Len(t, data, 6, "should have 4 binary + 1 config + 1 backend checks")

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

	// JSON mode should succeed (no CLIError)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err)

	assert.True(t, result["ok"].(bool), "JSON envelope ok should be true (data is the checks)")

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
	assert.Equal(t, 3, passCount, "ssh, rsync, config checks pass")
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
			assert.Len(t, data, 6, "always 6 checks")

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
