// Package cmd provides tests for the version command.
// REQ-002-007: Diagnostic Commands -- version
package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/ui"
)

func TestVersionCommand_Registered(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "version" {
			found = true
			assert.Equal(t, "diagnostics", cmd.GroupID)
			break
		}
	}
	assert.True(t, found, "version command must be registered")
}

func TestVersionCommand_HumanOutput(t *testing.T) {
	newRootTestEnv(t)

	// Capture os.Stdout by piping through a file descriptor
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"version"})
	execErr := root.Execute()

	// Restore and read
	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	require.NoError(t, execErr)
	assert.Contains(t, output, "Version:")
	assert.Contains(t, output, "Git Commit:")
	assert.Contains(t, output, "Built:")
	assert.Contains(t, output, "Go Version:")
}

func TestVersionCommand_JSONOutput(t *testing.T) {
	newRootTestEnv(t)

	// Capture os.Stdout
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "version"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	require.NoError(t, execErr)
	assert.NotEmpty(t, output, "JSON output should not be empty")

	// Parse JSON envelope
	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err, "output must be valid JSON: %s", output)

	assert.True(t, result["ok"].(bool), "ok must be true")

	data, ok := result["data"].(map[string]any)
	require.True(t, ok, "data must be a JSON object")

	assert.Contains(t, data, "version")
	assert.Contains(t, data, "gitCommit")
	assert.Contains(t, data, "buildDate")
	assert.Contains(t, data, "goVersion")
}

func TestVersionCommand_NoConfigRequired(t *testing.T) {
	// REQ-002-015: version must succeed even without a config file
	tmpDir := t.TempDir()
	t.Setenv("SD_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)

	root := RootCmd()
	root.SetArgs([]string{"version"})

	err := root.Execute()
	assert.NoError(t, err, "version must succeed without config file")
}

func TestVersionCommand_BuildVars(t *testing.T) {
	// Verify build variables have default values
	assert.NotEmpty(t, Version)
	assert.NotEmpty(t, GitCommit)
	assert.NotEmpty(t, BuildDate)
}

func TestVersionCommand_GoVersionPopulated(t *testing.T) {
	newRootTestEnv(t)

	// Capture os.Stdout
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "version"})
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
	goVer := data["goVersion"].(string)
	assert.True(t, strings.HasPrefix(goVer, "go"), "Go version should start with 'go', got: %s", goVer)
}

func TestVersionInfo_Fields(t *testing.T) {
	// Unit test: versionInfo struct has all required fields
	info := versionInfo{
		Version:   "1.0.0",
		GitCommit: "abc123",
		BuildDate: "2026-03-29",
		GoVersion: runtime.Version(),
	}

	assert.Equal(t, "1.0.0", info.Version)
	assert.Equal(t, "abc123", info.GitCommit)
	assert.Equal(t, "2026-03-29", info.BuildDate)
	assert.Equal(t, runtime.Version(), info.GoVersion)
}

func TestVersionInfo_JSONSerialization(t *testing.T) {
	info := versionInfo{
		Version:   "1.2.3",
		GitCommit: "deadbeef",
		BuildDate: "2026-03-29T12:00:00Z",
		GoVersion: "go1.22.0",
	}

	data, err := json.Marshal(info)
	require.NoError(t, err)

	var parsed map[string]any
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, "1.2.3", parsed["version"])
	assert.Equal(t, "deadbeef", parsed["gitCommit"])
	assert.Equal(t, "2026-03-29T12:00:00Z", parsed["buildDate"])
	assert.Equal(t, "go1.22.0", parsed["goVersion"])
}

func TestVersionInfo_FormatterHuman(t *testing.T) {
	// Test version info rendering through the formatter in human mode
	var stdout, stderr bytes.Buffer
	f := ui.NewFormatterWithWriters(false, &stdout, &stderr)

	info := versionInfo{
		Version:   "0.1.0",
		GitCommit: "abc123",
		BuildDate: "2026-01-01",
		GoVersion: "go1.22.0",
	}

	f.SuccessData(info, func() string {
		return "Version:    0.1.0\n"
	})

	assert.Contains(t, stdout.String(), "Version:    0.1.0")
	assert.Empty(t, stderr.String())
}

func TestVersionInfo_FormatterJSON(t *testing.T) {
	// Test version info rendering through the formatter in JSON mode
	var stdout, stderr bytes.Buffer
	f := ui.NewFormatterWithWriters(true, &stdout, &stderr)

	info := versionInfo{
		Version:   "0.1.0",
		GitCommit: "abc123",
		BuildDate: "2026-01-01",
		GoVersion: "go1.22.0",
	}

	f.SuccessData(info, nil)

	var result map[string]any
	err := json.Unmarshal(stdout.Bytes(), &result)
	require.NoError(t, err)

	assert.True(t, result["ok"].(bool))
	data := result["data"].(map[string]any)
	assert.Equal(t, "0.1.0", data["version"])
	assert.Equal(t, "abc123", data["gitCommit"])
}

// Property: version command always succeeds regardless of environment
func TestProperty_VersionAlwaysSucceeds(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
	}{
		{"empty_home", map[string]string{"SD_HOME": t.TempDir(), "HOME": t.TempDir()}},
		{"nonexistent_path", map[string]string{"SD_HOME": "/nonexistent/path/that/does/not/exist", "HOME": "/nonexistent"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			root := RootCmd()
			root.SetArgs([]string{"version"})
			err := root.Execute()
			assert.NoError(t, err, "version must succeed in any environment")
		})
	}
}

// Property: JSON output from formatter always parses with required fields
func TestProperty_VersionFormatterJSON(t *testing.T) {
	versions := []versionInfo{
		{Version: "0.0.1", GitCommit: "a", BuildDate: "2026-01-01", GoVersion: "go1.22"},
		{Version: "99.99.99", GitCommit: strings.Repeat("f", 40), BuildDate: "2099-12-31T23:59:59Z", GoVersion: runtime.Version()},
		{Version: "dev", GitCommit: "unknown", BuildDate: "unknown", GoVersion: "go1.26.1"},
	}

	for _, info := range versions {
		t.Run(info.Version, func(t *testing.T) {
			var stdout bytes.Buffer
			f := ui.NewFormatterWithWriters(true, &stdout, ioDiscarder{})
			f.SuccessData(info, nil)

			var result map[string]any
			err := json.Unmarshal(stdout.Bytes(), &result)
			require.NoError(t, err, "JSON must always parse for version %s", info.Version)
			assert.True(t, result["ok"].(bool))

			data := result["data"].(map[string]any)
			for _, field := range []string{"version", "gitCommit", "buildDate", "goVersion"} {
				_, exists := data[field]
				assert.True(t, exists, "field %s must exist for version %s", field, info.Version)
			}
		})
	}
}

// ioDiscarder is an io.Writer that discards all output.
type ioDiscarder struct{}

func (ioDiscarder) Write(p []byte) (int, error) { return len(p), nil }
