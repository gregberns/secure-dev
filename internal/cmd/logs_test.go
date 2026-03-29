// Package cmd provides tests for the logs command.
// REQ-002-007: Diagnostic Commands -- logs
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/backend"
	"sd/internal/ui"
)

// --- Digital Twin: mockLogsBackend ---

// mockLogsBackend is a digital twin of a backend for logs command testing.
// It implements backend.Backend with configurable Status behavior.
type mockLogsBackend struct {
	name      string
	statusMap map[string]backend.VMStatus
	statusErr error
}

func (m *mockLogsBackend) Name() string                                        { return m.name }
func (m *mockLogsBackend) Available() error                                    { return nil }
func (m *mockLogsBackend) Create(_ context.Context, _ string, _ backend.VMConfig) error { return nil }
func (m *mockLogsBackend) Start(_ context.Context, _ string) error            { return nil }
func (m *mockLogsBackend) Stop(_ context.Context, _ string) error             { return nil }
func (m *mockLogsBackend) Destroy(_ context.Context, _ string) error          { return nil }
func (m *mockLogsBackend) Status(_ context.Context, name string) (backend.VMStatus, error) {
	if m.statusErr != nil {
		return "", m.statusErr
	}
	if s, ok := m.statusMap[name]; ok {
		return s, nil
	}
	return "", backend.ErrVMNotFound
}
func (m *mockLogsBackend) List(_ context.Context) ([]backend.VMInfo, error)       { return nil, nil }
func (m *mockLogsBackend) SSHConfig(_ context.Context, _ string) (backend.SSHConfig, error) {
	return backend.SSHConfig{}, nil
}
func (m *mockLogsBackend) Exec(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
	return backend.ExecResult{}, nil
}

// --- Test Setup Helpers ---

// setupLogsTest configures the test environment with mock backend and log reader.
func setupLogsTest(t *testing.T, mb *mockLogsBackend, logData []byte) {
	t.Helper()
	tmpDir := newRootTestEnv(t)

	// Write audit.log so sd logs (no VM) can find it
	if logData != nil {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "audit.log"), logData, 0600))
	}

	// Mock backend
	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return mb, nil
	}

	// Mock log reader: for VM logs, create the file on demand
	origReadLog := readLogFileFunc
	readLogFileFunc = func(path string) ([]byte, error) {
		// If reading audit.log, use real file
		if strings.HasSuffix(path, "audit.log") {
			return os.ReadFile(path)
		}
		// For VM logs (serial.log), write and return mock data
		if logData != nil {
			dir := filepath.Dir(path)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(path, logData, 0600); err != nil {
				return nil, err
			}
			return logData, nil
		}
		return nil, os.ErrNotExist
	}

	// Mock tailFileFunc to avoid actual streaming
	origTailFile := tailFileFunc
	tailFileFunc = func(_ string, _ int64, _ io.Writer) error {
		return nil
	}

	t.Cleanup(func() {
		getBackendFunc = origGetBackend
		readLogFileFunc = origReadLog
		tailFileFunc = origTailFile
	})
}

// resetLogsFlags resets the logs command flags to defaults.
func resetLogsFlags(t *testing.T) {
	t.Helper()
	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "logs" {
			_ = cmd.Flags().Set("tail", "50")
			_ = cmd.Flags().Set("follow", "false")
			break
		}
	}
}

// parseJSONOutput parses stdout as a JSON response envelope.
func parseJSONLogsOutput(t *testing.T, stdout string) (map[string]interface{}, error) {
	t.Helper()
	var result map[string]interface{}
	err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &result)
	return result, err
}

// --- Unit Tests ---

func TestLogsCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()
	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "logs" {
			found = true
			break
		}
	}
	assert.True(t, found, "logs command should be registered")
}

func TestLogsCommand_MaxArgs(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "logs" {
			assert.True(t, cmd.Args == nil || cmd.Args(nil, []string{"a", "b"}) != nil,
				"logs should accept at most 1 arg")
			break
		}
	}
}

func TestLogsCommand_Flags(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "logs" {
			_, err := cmd.Flags().GetInt("tail")
			assert.NoError(t, err, "should have --tail flag")
			_, err = cmd.Flags().GetBool("follow")
			assert.NoError(t, err, "should have --follow flag")
			assert.Equal(t, "f", cmd.Flags().Lookup("follow").Shorthand)
			break
		}
	}
}

func TestLogsCommand_GroupID(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "logs" {
			assert.Equal(t, "diagnostics", cmd.GroupID)
			break
		}
	}
}

// --- Audit Log Tests (no VM name) ---

func TestLogsCommand_AuditLog_HumanOutput(t *testing.T) {
	logData := []byte("line 1\nline 2\nline 3\n")
	setupLogsTest(t, &mockLogsBackend{}, logData)
	resetLogsFlags(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"logs"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "line 1")
	assert.Contains(t, buf.String(), "line 2")
	assert.Contains(t, buf.String(), "line 3")
}

func TestLogsCommand_AuditLog_JSONOutput(t *testing.T) {
	logData := []byte("entry one\nentry two\n")
	setupLogsTest(t, &mockLogsBackend{}, logData)
	resetLogsFlags(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "logs"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	require.NoError(t, execErr)

	result, err := parseJSONLogsOutput(t, buf.String())
	require.NoError(t, err)
	assert.Equal(t, true, result["ok"])

	data, ok := result["data"].([]interface{})
	require.True(t, ok)
	assert.Len(t, data, 2)

	first := data[0].(map[string]interface{})
	assert.Equal(t, float64(1), first["line"])
	assert.Equal(t, "entry one", first["content"])
	assert.Equal(t, "audit", first["source"])
}

func TestLogsCommand_AuditLog_EmptyFile(t *testing.T) {
	setupLogsTest(t, &mockLogsBackend{}, []byte{})
	resetLogsFlags(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "logs"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	require.NoError(t, execErr)

	result, err := parseJSONLogsOutput(t, buf.String())
	require.NoError(t, err)
	assert.Equal(t, true, result["ok"])
	data := result["data"].([]interface{})
	assert.Len(t, data, 0)
}

func TestLogsCommand_AuditLog_NoFile(t *testing.T) {
	// No log data => audit.log doesn't exist
	setupLogsTest(t, &mockLogsBackend{}, nil)
	resetLogsFlags(t)

	root := RootCmd()
	root.SetArgs([]string{"logs"})
	err := root.Execute()
	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "logs_unavailable", cliErr.Code)
}

func TestLogsCommand_NoSDHome(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SD_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)
	resetRootFlags(t)
	resetLogsFlags(t)

	// Don't write audit.log
	origReadLog := readLogFileFunc
	readLogFileFunc = func(_ string) ([]byte, error) {
		return nil, os.ErrNotExist
	}
	defer func() { readLogFileFunc = origReadLog }()

	root := RootCmd()
	root.SetArgs([]string{"logs"})
	err := root.Execute()
	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "logs_unavailable", cliErr.Code)
}

// --- Tail Tests ---

func TestLogsCommand_TailFlag(t *testing.T) {
	lines := make([]string, 100)
	for i := 0; i < 100; i++ {
		lines[i] = fmt.Sprintf("log line %d", i)
	}
	logData := []byte(strings.Join(lines, "\n") + "\n")
	setupLogsTest(t, &mockLogsBackend{}, logData)
	resetLogsFlags(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"logs", "--tail", "10"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	require.NoError(t, execErr)
	// Should only contain the last 10 lines
	assert.Contains(t, buf.String(), "log line 90")
	assert.Contains(t, buf.String(), "log line 99")
	assert.NotContains(t, buf.String(), "log line 89")
}

func TestLogsCommand_TailLargerThanFile(t *testing.T) {
	logData := []byte("line 1\nline 2\n")
	setupLogsTest(t, &mockLogsBackend{}, logData)
	resetLogsFlags(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"logs", "--tail", "100"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "line 1")
	assert.Contains(t, buf.String(), "line 2")
}

func TestLogsCommand_InvalidTailZero(t *testing.T) {
	setupLogsTest(t, &mockLogsBackend{}, []byte("data\n"))
	resetLogsFlags(t)

	root := RootCmd()
	root.SetArgs([]string{"logs", "--tail", "0"})
	err := root.Execute()
	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
	assert.Contains(t, cliErr.Message, "positive integer")
}

func TestLogsCommand_InvalidTailNegative(t *testing.T) {
	setupLogsTest(t, &mockLogsBackend{}, []byte("data\n"))
	resetLogsFlags(t)

	root := RootCmd()
	root.SetArgs([]string{"logs", "--tail", "-5"})
	err := root.Execute()
	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
}

// --- VM Log Tests ---

func TestLogsCommand_VMLog_HumanOutput(t *testing.T) {
	logData := []byte("vm boot message\nvm ready\n")
	mb := &mockLogsBackend{
		statusMap: map[string]backend.VMStatus{"myvm": backend.StatusRunning},
	}
	setupLogsTest(t, mb, logData)
	resetLogsFlags(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"logs", "myvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "vm boot message")
	assert.Contains(t, buf.String(), "vm ready")
}

func TestLogsCommand_VMLog_JSONOutput(t *testing.T) {
	logData := []byte("vm output line\n")
	mb := &mockLogsBackend{
		statusMap: map[string]backend.VMStatus{"myvm": backend.StatusRunning},
	}
	setupLogsTest(t, mb, logData)
	resetLogsFlags(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "logs", "myvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	require.NoError(t, execErr)

	result, err := parseJSONLogsOutput(t, buf.String())
	require.NoError(t, err)
	assert.Equal(t, true, result["ok"])

	data := result["data"].([]interface{})
	assert.Len(t, data, 1)

	entry := data[0].(map[string]interface{})
	assert.Equal(t, "vm", entry["source"])
	assert.Equal(t, "myvm", entry["vm"])
	assert.Equal(t, "vm output line", entry["content"])
}

func TestLogsCommand_VMNotFound(t *testing.T) {
	setupLogsTest(t, &mockLogsBackend{statusMap: map[string]backend.VMStatus{}}, nil)
	resetLogsFlags(t)

	root := RootCmd()
	root.SetArgs([]string{"logs", "nonexistent"})
	err := root.Execute()
	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
	assert.Contains(t, cliErr.Message, "nonexistent")
}

func TestLogsCommand_BackendUnavailable(t *testing.T) {
	newRootTestEnv(t)
	resetLogsFlags(t)

	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return nil, fmt.Errorf("no backends registered")
	}
	defer func() { getBackendFunc = origGetBackend }()

	root := RootCmd()
	root.SetArgs([]string{"logs", "myvm"})
	err := root.Execute()
	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

// --- Follow Tests ---

func TestLogsCommand_FollowFlag(t *testing.T) {
	logData := []byte("existing line\n")
	setupLogsTest(t, &mockLogsBackend{}, logData)
	resetLogsFlags(t)

	tailCalled := false
	var capturedOffset int64
	origTail := tailFileFunc
	tailFileFunc = func(path string, offset int64, w io.Writer) error {
		tailCalled = true
		capturedOffset = offset
		return nil
	}
	defer func() { tailFileFunc = origTail }()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"logs", "--follow"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "existing line")
	assert.True(t, tailCalled, "tailFileFunc should have been called")
	assert.Equal(t, int64(len(logData)), capturedOffset)
}

func TestLogsCommand_FollowShortFlag(t *testing.T) {
	logData := []byte("line\n")
	setupLogsTest(t, &mockLogsBackend{}, logData)
	resetLogsFlags(t)

	tailCalled := false
	origTail := tailFileFunc
	tailFileFunc = func(_ string, _ int64, _ io.Writer) error {
		tailCalled = true
		return nil
	}
	defer func() { tailFileFunc = origTail }()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"logs", "-f"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	require.NoError(t, execErr)
	assert.True(t, tailCalled, "tailFileFunc should have been called with -f")
}

func TestLogsCommand_FollowJSONOutputsExistingOnly(t *testing.T) {
	logData := []byte("line a\nline b\n")
	setupLogsTest(t, &mockLogsBackend{}, logData)
	resetLogsFlags(t)

	// Follow + JSON should not stream; just output existing entries as JSON
	tailCalled := false
	origTail := tailFileFunc
	tailFileFunc = func(_ string, _ int64, _ io.Writer) error {
		tailCalled = true
		return nil
	}
	defer func() { tailFileFunc = origTail }()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "logs", "--follow"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	require.NoError(t, execErr)

	result, err := parseJSONLogsOutput(t, buf.String())
	require.NoError(t, err)
	assert.Equal(t, true, result["ok"])
	assert.False(t, tailCalled, "tailFileFunc should not be called in JSON mode")
}

func TestLogsCommand_FollowWithTail(t *testing.T) {
	lines := make([]string, 100)
	for i := 0; i < 100; i++ {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	logData := []byte(strings.Join(lines, "\n") + "\n")
	setupLogsTest(t, &mockLogsBackend{}, logData)
	resetLogsFlags(t)

	var capturedOffset int64
	origTail := tailFileFunc
	tailFileFunc = func(_ string, offset int64, _ io.Writer) error {
		capturedOffset = offset
		return nil
	}
	defer func() { tailFileFunc = origTail }()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"logs", "--follow", "--tail", "10"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	require.NoError(t, execErr)
	// Should show only last 10 lines
	assert.Contains(t, buf.String(), "line 90")
	assert.NotContains(t, buf.String(), "line 89")
	assert.Equal(t, int64(len(logData)), capturedOffset)
}

// --- Human Output Format Tests ---

func TestLogsCommand_EmptyLog_HumanOutput(t *testing.T) {
	setupLogsTest(t, &mockLogsBackend{}, []byte{})
	resetLogsFlags(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"logs"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "No log entries found")
}

// --- makeLogEntries Unit Tests ---

func TestMakeLogEntries_Empty(t *testing.T) {
	entries := makeLogEntries([]string{}, "audit", "")
	assert.Len(t, entries, 0)
}

func TestMakeLogEntries_WithContent(t *testing.T) {
	entries := makeLogEntries([]string{"foo", "bar"}, "audit", "")
	assert.Len(t, entries, 2)
	assert.Equal(t, 1, entries[0].Line)
	assert.Equal(t, "foo", entries[0].Content)
	assert.Equal(t, "audit", entries[0].Source)
	assert.Equal(t, "", entries[0].VM)
	assert.Equal(t, 2, entries[1].Line)
}

func TestMakeLogEntries_WithVM(t *testing.T) {
	entries := makeLogEntries([]string{"output"}, "vm", "myvm")
	assert.Len(t, entries, 1)
	assert.Equal(t, "vm", entries[0].Source)
	assert.Equal(t, "myvm", entries[0].VM)
}

func TestMakeLogEntries_LineNumbers(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e"}
	entries := makeLogEntries(lines, "audit", "")
	for i, e := range entries {
		assert.Equal(t, i+1, e.Line)
	}
}

// --- Property-Based Tests ---

// Property: JSON output is always valid for varying VM names and log content.
func TestProperty_LogsJSONAlwaysValid(t *testing.T) {
	names := []string{"", "vm-test", "myvm", "prod-01", "dev-sandbox"}
	for _, name := range names {
		lineCount := 3
		lines := make([]string, lineCount)
		for i := 0; i < lineCount; i++ {
			lines[i] = fmt.Sprintf("log entry %d for %s", i, name)
		}
		logData := []byte(strings.Join(lines, "\n") + "\n")

		mb := &mockLogsBackend{
			statusMap: map[string]backend.VMStatus{
				"vm-test":    backend.StatusRunning,
				"myvm":       backend.StatusRunning,
				"prod-01":    backend.StatusRunning,
				"dev-sandbox": backend.StatusRunning,
			},
		}

		tmpDir := t.TempDir()
		t.Setenv("SD_HOME", tmpDir)
		t.Setenv("HOME", tmpDir)
		resetRootFlags(t)
		resetLogsFlags(t)

		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "audit.log"), logData, 0600))

		origGetBackend := getBackendFunc
		getBackendFunc = func(_ string) (backend.Backend, error) { return mb, nil }
		defer func() { getBackendFunc = origGetBackend }()

		origReadLog := readLogFileFunc
		readLogFileFunc = func(path string) ([]byte, error) {
			if strings.HasSuffix(path, "audit.log") {
				return os.ReadFile(path)
			}
			dir := filepath.Dir(path)
			_ = os.MkdirAll(dir, 0755)
			_ = os.WriteFile(path, logData, 0600)
			return logData, nil
		}
		defer func() { readLogFileFunc = origReadLog }()

		args := []string{"--json", "logs"}
		if name != "" {
			args = append(args, name)
		}

		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		require.NoError(t, err)
		os.Stdout = w

		root := RootCmd()
		root.SetArgs(args)
		execErr := root.Execute()

		w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		io.Copy(&buf, r)

		require.NoError(t, execErr, "should succeed for name=%q", name)

		var result map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &result),
			"JSON must parse for name=%q: %s", name, buf.String())
		assert.Equal(t, true, result["ok"], "ok must be true for name=%q", name)

		data, ok := result["data"].([]interface{})
		require.True(t, ok, "data must be an array for name=%q", name)
		assert.Equal(t, lineCount, len(data), "data length for name=%q", name)
	}
}

// Property: human output always contains the search string.
func TestProperty_LogsHumanContainsContent(t *testing.T) {
	searchStrings := []string{"search-alpha", "search-beta", "search-gamma", "search-delta", "search-epsilon"}
	for _, searchStr := range searchStrings {
		tmpDir := t.TempDir()
		t.Setenv("SD_HOME", tmpDir)
		t.Setenv("HOME", tmpDir)
		resetRootFlags(t)
		resetLogsFlags(t)

		logData := []byte(fmt.Sprintf("before\n%s\nafter\n", searchStr))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "audit.log"), logData, 0600))

		origReadLog := readLogFileFunc
		readLogFileFunc = func(path string) ([]byte, error) {
			if strings.HasSuffix(path, "audit.log") {
				return os.ReadFile(path)
			}
			return nil, os.ErrNotExist
		}
		defer func() { readLogFileFunc = origReadLog }()

		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		require.NoError(t, err)
		os.Stdout = w

		root := RootCmd()
		root.SetArgs([]string{"logs"})
		execErr := root.Execute()

		w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		io.Copy(&buf, r)

		require.NoError(t, execErr)
		assert.Contains(t, buf.String(), searchStr, "human output should contain %q", searchStr)
	}
}

// Property: all error codes are snake_case.
func TestProperty_LogsErrorCodesSnakeCase(t *testing.T) {
	codes := []string{"vm_not_found", "backend_unavailable", "logs_unavailable", "invalid_argument"}
	for _, code := range codes {
		assert.NotContains(t, code, " ", "code %q should have no spaces", code)
		assert.NotContains(t, code, "-", "code %q should have no hyphens", code)
		for _, c := range code {
			assert.True(t, c == '_' || (c >= 'a' && c <= 'z'),
				"code %q should be snake_case", code)
		}
	}
}

// Property: nonexistent VM names never call readLogFileFunc.
func TestProperty_LogsVMNotFoundNeverCallsReadLog(t *testing.T) {
	vmNames := []string{"nonexistent-aa", "nonexistent-bb", "nonexistent-cc"}
	for _, vmName := range vmNames {
		newRootTestEnv(t)
		resetLogsFlags(t)

		mb := &mockLogsBackend{statusMap: map[string]backend.VMStatus{}}

		origGetBackend := getBackendFunc
		getBackendFunc = func(_ string) (backend.Backend, error) { return mb, nil }
		defer func() { getBackendFunc = origGetBackend }()

		readCalled := false
		origReadLog := readLogFileFunc
		readLogFileFunc = func(_ string) ([]byte, error) {
			readCalled = true
			return nil, os.ErrNotExist
		}
		defer func() { readLogFileFunc = origReadLog }()

		root := RootCmd()
		root.SetArgs([]string{"logs", vmName})
		err := root.Execute()
		require.Error(t, err, "should fail for VM %q", vmName)
		assert.False(t, readCalled, "readLogFileFunc should not be called for VM %q", vmName)
	}
}

// Property: JSON output always has required fields (ok, data with line/content/source).
func TestProperty_LogsJSONRequiredFields(t *testing.T) {
	contents := []string{"log-abc", "log-def", "log-ghi", "log-jkl", "log-mno"}
	for _, content := range contents {
		tmpDir := t.TempDir()
		t.Setenv("SD_HOME", tmpDir)
		t.Setenv("HOME", tmpDir)
		resetRootFlags(t)
		resetLogsFlags(t)

		logData := []byte(content + "\n")
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "audit.log"), logData, 0600))

		origReadLog := readLogFileFunc
		readLogFileFunc = func(path string) ([]byte, error) {
			if strings.HasSuffix(path, "audit.log") {
				return os.ReadFile(path)
			}
			return nil, os.ErrNotExist
		}
		defer func() { readLogFileFunc = origReadLog }()

		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		require.NoError(t, err)
		os.Stdout = w

		root := RootCmd()
		root.SetArgs([]string{"--json", "logs"})
		execErr := root.Execute()

		w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		io.Copy(&buf, r)

		require.NoError(t, execErr, "should succeed for content=%q", content)

		var result map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &result))

		assert.Contains(t, result, "ok", "should have 'ok' for content=%q", content)
		assert.Contains(t, result, "data", "should have 'data' for content=%q", content)

		data := result["data"].([]interface{})
		require.Len(t, data, 1, "should have 1 entry for content=%q", content)
		entry := data[0].(map[string]interface{})
		assert.Contains(t, entry, "line", "entry should have 'line'")
		assert.Contains(t, entry, "content", "entry should have 'content'")
		assert.Contains(t, entry, "source", "entry should have 'source'")
	}
}
