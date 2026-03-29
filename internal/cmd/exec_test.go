// Package cmd provides tests for the exec command.
// REQ-007-013: Exec Command
// REQ-007-014: Exec JSON Output
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/backend"
	"sd/internal/ui"
)

// mockExecBackend is a digital twin of a backend for exec command testing.
// It implements backend.Backend with configurable Exec behavior and records calls.
type mockExecBackend struct {
	name      string
	available bool
	// Records all Exec calls: (vmName, command)
	execCalls []execCall
	// Configurable results
	execResult backend.ExecResult
	execErr    error
	statusErr  error
	statusMap  map[string]backend.VMStatus
}

type execCall struct {
	VMName  string
	Command []string
}

func (m *mockExecBackend) Name() string { return m.name }
func (m *mockExecBackend) Available() error {
	if m.available {
		return nil
	}
	return fmt.Errorf("backend not available")
}
func (m *mockExecBackend) Create(_ context.Context, _ string, _ backend.VMConfig) error {
	return nil
}
func (m *mockExecBackend) Start(_ context.Context, _ string) error    { return nil }
func (m *mockExecBackend) Stop(_ context.Context, _ string) error     { return nil }
func (m *mockExecBackend) Destroy(_ context.Context, _ string) error  { return nil }
func (m *mockExecBackend) SSHConfig(_ context.Context, _ string) (backend.SSHConfig, error) {
	return backend.SSHConfig{}, nil
}
func (m *mockExecBackend) List(_ context.Context) ([]backend.VMInfo, error) {
	return nil, nil
}
func (m *mockExecBackend) Exec(_ context.Context, name string, command []string) (backend.ExecResult, error) {
	if m.execErr != nil {
		return backend.ExecResult{}, m.execErr
	}
	m.execCalls = append(m.execCalls, execCall{VMName: name, Command: command})
	return m.execResult, nil
}
func (m *mockExecBackend) Status(_ context.Context, name string) (backend.VMStatus, error) {
	if m.statusErr != nil {
		return "", m.statusErr
	}
	if s, ok := m.statusMap[name]; ok {
		return s, nil
	}
	return "", backend.ErrVMNotFound
}

// setupExecTest configures the test environment with a mock backend.
func setupExecTest(t *testing.T, mb *mockExecBackend) {
	t.Helper()
	newRootTestEnv(t)

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })
}

// --- Unit tests ---

func TestExecCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "exec" {
			found = true
			assert.Equal(t, "connection", cmd.GroupID)
			break
		}
	}
	assert.True(t, found, "exec command must be registered")
}

func TestExecCommand_MinArgs(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"exec"})
	require.NoError(t, err)
	assert.NotNil(t, cmd.Args, "exec must have an Args validator")

	// Zero args must fail
	err = cmd.Args(cmd, nil)
	assert.Error(t, err, "exec must reject zero args")

	// One arg must fail
	err = cmd.Args(cmd, []string{"myvm"})
	assert.Error(t, err, "exec must reject one arg (needs vm + command)")

	// Two args must pass
	err = cmd.Args(cmd, []string{"myvm", "echo"})
	assert.NoError(t, err, "exec must accept vm name + command")
}

func TestExecCommand_BasicExec_HumanOutput(t *testing.T) {
	mb := &mockExecBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		execResult: backend.ExecResult{
			ExitCode: 0,
			Stdout:   "hello world\n",
			Stderr:   "",
		},
	}
	setupExecTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "echo", "hello", "world"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Equal(t, "hello world\n", buf.String())

	// Verify backend was called with correct args
	require.Len(t, mb.execCalls, 1)
	assert.Equal(t, "myvm", mb.execCalls[0].VMName)
	assert.Equal(t, []string{"echo", "hello", "world"}, mb.execCalls[0].Command)
}

func TestExecCommand_BasicExec_JSONOutput(t *testing.T) {
	mb := &mockExecBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		execResult: backend.ExecResult{
			ExitCode: 0,
			Stdout:   "hello\n",
			Stderr:   "",
		},
	}
	setupExecTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "exec", "myvm", "--", "echo", "hello"})
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
	data := result["data"].(map[string]any)
	assert.Equal(t, float64(0), data["exit_code"])
	assert.Equal(t, "hello\n", data["stdout"])
	assert.Equal(t, "", data["stderr"])
}

func TestExecCommand_NonZeroExitCode_JSONOutput(t *testing.T) {
	mb := &mockExecBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		execResult: backend.ExecResult{
			ExitCode: 42,
			Stdout:   "",
			Stderr:   "error: something failed\n",
		},
	}
	setupExecTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	// Prevent os.Exit from killing the test process
	oldOsExit := osExit
	var exitCode int
	osExit = func(code int) { exitCode = code }
	defer func() { osExit = oldOsExit }()

	root := RootCmd()
	root.SetArgs([]string{"--json", "exec", "myvm", "--", "false"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Equal(t, 42, exitCode)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err, "output must be valid JSON: %s", buf.String())

	data := result["data"].(map[string]any)
	assert.Equal(t, float64(42), data["exit_code"])
	assert.Equal(t, "error: something failed\n", data["stderr"])
}

func TestExecCommand_VMNotFound(t *testing.T) {
	mb := &mockExecBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{},
	}
	setupExecTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"exec", "nonexistent", "--", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
	assert.Contains(t, cliErr.Message, "nonexistent")
}

func TestExecCommand_VMNotRunning(t *testing.T) {
	mb := &mockExecBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{"myvm": backend.StatusStopped},
	}
	setupExecTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_running", cliErr.Code)
	assert.Contains(t, cliErr.Message, "myvm")
	assert.Contains(t, cliErr.Message, "sd start")
}

func TestExecCommand_VMInErrorStatus(t *testing.T) {
	mb := &mockExecBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{"myvm": backend.StatusError},
	}
	setupExecTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_running", cliErr.Code)
}

func TestExecCommand_BackendUnavailable(t *testing.T) {
	mb := &mockExecBackend{name: "mock", available: false}
	setupExecTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestExecCommand_BackendGetError(t *testing.T) {
	newRootTestEnv(t)

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return nil, fmt.Errorf("no such backend")
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestExecCommand_ExecBackendError(t *testing.T) {
	mb := &mockExecBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		execErr:   fmt.Errorf("internal error"),
	}
	setupExecTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "exec_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "internal error")
}

func TestExecCommand_ExecBackendErrVMNotRunning(t *testing.T) {
	mb := &mockExecBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		execErr:   backend.ErrVMNotRunning,
	}
	setupExecTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_running", cliErr.Code)
}

func TestExecCommand_ExecBackendErrVMNotFound(t *testing.T) {
	mb := &mockExecBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		execErr:   backend.ErrVMNotFound,
	}
	setupExecTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
}

func TestExecCommand_EmptyName(t *testing.T) {
	mb := &mockExecBackend{name: "mock", available: true}
	setupExecTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"exec", "", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
}

func TestExecCommand_StatusCheckError(t *testing.T) {
	mb := &mockExecBackend{
		name:      "mock",
		available: true,
		statusErr: fmt.Errorf("connection refused"),
		statusMap: map[string]backend.VMStatus{},
	}
	setupExecTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "echo"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "exec_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "connection refused")
}

func TestExecCommand_StderrPassedThrough(t *testing.T) {
	mb := &mockExecBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		execResult: backend.ExecResult{
			ExitCode: 0,
			Stdout:   "out\n",
			Stderr:   "warning message\n",
		},
	}
	setupExecTest(t, mb)

	oldStdout := os.Stdout
	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = outW

	oldStderr := os.Stderr
	errR, errW, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = errW

	root := RootCmd()
	root.SetArgs([]string{"exec", "myvm", "--", "cmd"})
	execErr := root.Execute()

	outW.Close()
	errW.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var outBuf, errBuf bytes.Buffer
	_, _ = outBuf.ReadFrom(outR)
	_, _ = errBuf.ReadFrom(errR)

	require.NoError(t, execErr)
	assert.Equal(t, "out\n", outBuf.String())
	assert.Equal(t, "warning message\n", errBuf.String())
}

func TestExecCommand_CommandWithArgs(t *testing.T) {
	mb := &mockExecBackend{
		name:       "mock",
		available:  true,
		statusMap:  map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		execResult: backend.ExecResult{ExitCode: 0},
	}
	setupExecTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"--json", "exec", "myvm", "--", "ls", "-la", "/tmp"})
	err := root.Execute()

	require.NoError(t, err)
	require.Len(t, mb.execCalls, 1)
	assert.Equal(t, []string{"ls", "-la", "/tmp"}, mb.execCalls[0].Command)
}

func TestExecCommand_MissingVMName(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	root.SetArgs([]string{"exec"})
	err := root.Execute()
	assert.Error(t, err, "exec without args must fail")
}

func TestExecCommand_MissingCommand(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	root.SetArgs([]string{"exec", "myvm"})
	err := root.Execute()
	assert.Error(t, err, "exec with only VM name must fail")
}

// --- Property-based tests ---

// Property: JSON output from exec always contains ok=true, data.exit_code, data.stdout, data.stderr.
func TestProperty_ExecJSONAlwaysValid(t *testing.T) {
	cases := []struct {
		name     string
		vmName   string
		exitCode int
		stdout   string
		stderr   string
	}{
		{"zero_exit", "vm1", 0, "ok\n", ""},
		{"nonzero_exit", "vm2", 1, "", "failed\n"},
		{"high_exit", "vm3", 127, "out", "err"},
		{"empty_output", "vm4", 0, "", ""},
		{"large_exit", "vm5", 255, "x", "y"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mb := &mockExecBackend{
				name:      "mock",
				available: true,
				statusMap: map[string]backend.VMStatus{tc.vmName: backend.StatusRunning},
				execResult: backend.ExecResult{
					ExitCode: tc.exitCode,
					Stdout:   tc.stdout,
					Stderr:   tc.stderr,
				},
			}
			setupExecTest(t, mb)

			// Capture os.Exit for non-zero exit codes
			oldOsExit := osExit
			osExit = func(code int) {}
			defer func() { osExit = oldOsExit }()

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"--json", "exec", tc.vmName, "--", "cmd"})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)

			var result map[string]any
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err, "JSON must parse for %s: %s", tc.name, buf.String())
			assert.True(t, result["ok"].(bool), "ok must be true for %s", tc.name)

			data := result["data"].(map[string]any)
			assert.Equal(t, float64(tc.exitCode), data["exit_code"], "exit_code must match for %s", tc.name)
			assert.Equal(t, tc.stdout, data["stdout"], "stdout must match for %s", tc.name)
			assert.Equal(t, tc.stderr, data["stderr"], "stderr must match for %s", tc.name)
		})
	}
}

// Property: human output always passes through stdout from the remote command.
func TestProperty_ExecHumanOutputContainsStdout(t *testing.T) {
	outputs := []string{"hello\n", "multi\nline\n", "", "x", "long output with spaces and things\n"}
	for _, output := range outputs {
		t.Run(fmt.Sprintf("output_%q", output), func(t *testing.T) {
			mb := &mockExecBackend{
				name:      "mock",
				available: true,
				statusMap: map[string]backend.VMStatus{"vm1": backend.StatusRunning},
				execResult: backend.ExecResult{
					ExitCode: 0,
					Stdout:   output,
				},
			}
			setupExecTest(t, mb)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"exec", "vm1", "--", "cmd"})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)
			assert.Equal(t, output, buf.String(), "human output must match stdout for %q", output)
		})
	}
}

// Property: error codes are always snake_case.
func TestProperty_ExecErrorCodesSnakeCase(t *testing.T) {
	t.Run("vm_not_found", func(t *testing.T) {
		mb := &mockExecBackend{
			name:      "mock",
			available: true,
			statusMap: map[string]backend.VMStatus{},
		}
		setupExecTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"exec", "ghost", "--", "cmd"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "vm_not_found", cliErr.Code)
	})

	t.Run("vm_not_running", func(t *testing.T) {
		mb := &mockExecBackend{
			name:      "mock",
			available: true,
			statusMap: map[string]backend.VMStatus{"vm1": backend.StatusStopped},
		}
		setupExecTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"exec", "vm1", "--", "cmd"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "vm_not_running", cliErr.Code)
	})

	t.Run("backend_unavailable", func(t *testing.T) {
		mb := &mockExecBackend{name: "mock", available: false}
		setupExecTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"exec", "vm1", "--", "cmd"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "backend_unavailable", cliErr.Code)
	})

	t.Run("invalid_argument_empty_name", func(t *testing.T) {
		mb := &mockExecBackend{name: "mock", available: true}
		setupExecTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"exec", "", "cmd"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "invalid_argument", cliErr.Code)
	})

	t.Run("exec_failed", func(t *testing.T) {
		mb := &mockExecBackend{
			name:      "mock",
			available: true,
			statusMap: map[string]backend.VMStatus{"vm1": backend.StatusRunning},
			execErr:   fmt.Errorf("internal error"),
		}
		setupExecTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"exec", "vm1", "--", "cmd"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "exec_failed", cliErr.Code)
	})
}

// Property: running VM always calls backend Exec exactly once.
func TestProperty_ExecRunningVM_CallsBackendOnce(t *testing.T) {
	names := []string{"vm1", "vm2", "test-vm"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := &mockExecBackend{
				name:       "mock",
				available:  true,
				statusMap:  map[string]backend.VMStatus{name: backend.StatusRunning},
				execResult: backend.ExecResult{ExitCode: 0},
			}
			setupExecTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"--json", "exec", name, "--", "echo"})
			err := root.Execute()
			require.NoError(t, err)
			require.Len(t, mb.execCalls, 1, "exec must call backend exactly once for %q", name)
			assert.Equal(t, name, mb.execCalls[0].VMName)
		})
	}
}

// Property: stopped VM never calls backend Exec.
func TestProperty_ExecStoppedVM_NeverCallsBackend(t *testing.T) {
	statuses := []backend.VMStatus{backend.StatusStopped, backend.StatusCreating, backend.StatusError}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			mb := &mockExecBackend{
				name:      "mock",
				available: true,
				statusMap: map[string]backend.VMStatus{"vm1": status},
			}
			setupExecTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"exec", "vm1", "--", "echo"})
			err := root.Execute()
			require.Error(t, err)
			assert.Empty(t, mb.execCalls, "exec on non-running VM must not call backend.Exec for status %s", status)
		})
	}
}

// Property: JSON output always contains required fields.
func TestProperty_ExecJSONRequiredFields(t *testing.T) {
	mb := &mockExecBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		execResult: backend.ExecResult{
			ExitCode: 0,
			Stdout:   "test",
			Stderr:   "",
		},
	}
	setupExecTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "exec", "myvm", "--", "echo"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	assert.Contains(t, result, "ok", "JSON must have 'ok' field")
	assert.Contains(t, result, "data", "JSON must have 'data' field")

	data := result["data"].(map[string]any)
	assert.Contains(t, data, "exit_code", "data must have 'exit_code' field")
	assert.Contains(t, data, "stdout", "data must have 'stdout' field")
	assert.Contains(t, data, "stderr", "data must have 'stderr' field")
}

// Property: error JSON format is consistent for vm_not_found.
func TestProperty_ExecErrorJSONFormat(t *testing.T) {
	mb := &mockExecBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{},
	}
	setupExecTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"exec", "nonexistent", "--", "cmd"})
	execErr := root.Execute()

	require.Error(t, execErr)
	cliErr, ok := execErr.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
	assert.Contains(t, cliErr.Message, "nonexistent")
}
