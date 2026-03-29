// Package cmd provides tests for the start command.
// REQ-002-003: VM Management Commands -- start
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

// mockStartBackend is a digital twin of a backend for start command testing.
// It implements backend.Backend with configurable Start behavior and records calls.
type mockStartBackend struct {
	name       string
	available  bool
	started    []string
	err        error // error to return from Start
	statusErr  error // error to return from Status
	statusMap  map[string]backend.VMStatus
}

func (m *mockStartBackend) Name() string { return m.name }
func (m *mockStartBackend) Available() error {
	if m.available {
		return nil
	}
	return fmt.Errorf("backend not available")
}
func (m *mockStartBackend) Create(_ context.Context, _ string, _ backend.VMConfig) error {
	return nil
}
func (m *mockStartBackend) Start(_ context.Context, name string) error {
	if m.err != nil {
		return m.err
	}
	m.started = append(m.started, name)
	return nil
}
func (m *mockStartBackend) Stop(_ context.Context, _ string) error   { return nil }
func (m *mockStartBackend) Destroy(_ context.Context, _ string) error { return nil }
func (m *mockStartBackend) Status(_ context.Context, name string) (backend.VMStatus, error) {
	if m.statusErr != nil {
		return "", m.statusErr
	}
	if s, ok := m.statusMap[name]; ok {
		return s, nil
	}
	return "", backend.ErrVMNotFound
}
func (m *mockStartBackend) List(_ context.Context) ([]backend.VMInfo, error) {
	return nil, nil
}
func (m *mockStartBackend) SSHConfig(_ context.Context, _ string) (backend.SSHConfig, error) {
	return backend.SSHConfig{}, nil
}
func (m *mockStartBackend) Exec(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
	return backend.ExecResult{}, nil
}

// setupStartTest configures the test environment with a mock backend.
// Returns the mock so tests can inspect recorded calls.
func setupStartTest(t *testing.T, mb *mockStartBackend) {
	t.Helper()
	newRootTestEnv(t)

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })
}

// --- Unit tests ---

func TestStartCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "start" {
			found = true
			assert.Equal(t, "vm", cmd.GroupID)
			break
		}
	}
	assert.True(t, found, "start command must be registered")
}

func TestStartCommand_ExactArgs(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"start"})
	require.NoError(t, err)
	assert.NotNil(t, cmd.Args, "start must have an Args validator")
	// Verify it rejects 0 args
	err = cmd.Args(cmd, nil)
	assert.Error(t, err, "start must reject zero args")
}

func TestStartCommand_BasicStart_HumanOutput(t *testing.T) {
	mb := &mockStartBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{"testvm": backend.StatusStopped},
	}
	setupStartTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"start", "testvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "testvm")
	assert.Contains(t, buf.String(), "started")

	// Verify backend was called
	require.Len(t, mb.started, 1)
	assert.Equal(t, "testvm", mb.started[0])
}

func TestStartCommand_BasicStart_JSONOutput(t *testing.T) {
	mb := &mockStartBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{"testvm": backend.StatusStopped},
	}
	setupStartTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "start", "testvm"})
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
	assert.Equal(t, "testvm", data["name"])
	assert.Equal(t, "running", data["status"])
}

func TestStartCommand_VMNotFound(t *testing.T) {
	mb := &mockStartBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{}, // empty: no VMs exist
	}
	setupStartTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"start", "nonexistent"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
	assert.Contains(t, cliErr.Message, "nonexistent")
}

func TestStartCommand_AlreadyRunning(t *testing.T) {
	mb := &mockStartBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{"testvm": backend.StatusRunning},
	}
	setupStartTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"start", "testvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_already_running", cliErr.Code)
	assert.Contains(t, cliErr.Message, "testvm")

	// Verify backend Start was NOT called
	assert.Empty(t, mb.started, "Start must not be called when VM is already running")
}

func TestStartCommand_BackendUnavailable(t *testing.T) {
	mb := &mockStartBackend{name: "mock", available: false}
	setupStartTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"start", "testvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestStartCommand_BackendGetError(t *testing.T) {
	newRootTestEnv(t)

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return nil, fmt.Errorf("no such backend")
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	root := RootCmd()
	root.SetArgs([]string{"start", "testvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestStartCommand_GenericError(t *testing.T) {
	mb := &mockStartBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{"testvm": backend.StatusStopped},
		err:       fmt.Errorf("internal error"),
	}
	setupStartTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"start", "testvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_start_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "internal error")
}

func TestStartCommand_MissingName(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	root.SetArgs([]string{"start"})
	err := root.Execute()
	assert.Error(t, err, "start without name must fail")
}

func TestStartCommand_EmptyName(t *testing.T) {
	mb := &mockStartBackend{name: "mock", available: true}
	setupStartTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"start", ""})
	err := root.Execute()
	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
}

func TestStartCommand_StartOnStoppedVM_CallsBackend(t *testing.T) {
	mb := &mockStartBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{"myvm": backend.StatusStopped},
	}
	setupStartTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"start", "myvm"})
	err := root.Execute()

	require.NoError(t, err)
	require.Len(t, mb.started, 1)
	assert.Equal(t, "myvm", mb.started[0])
}

func TestStartCommand_StartOnErrorStatusVM(t *testing.T) {
	mb := &mockStartBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{"myvm": backend.StatusError},
	}
	setupStartTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"start", "myvm"})
	err := root.Execute()

	require.NoError(t, err)
	require.Len(t, mb.started, 1)
}

func TestStartCommand_StatusCheckError(t *testing.T) {
	mb := &mockStartBackend{
		name:       "mock",
		available:  true,
		statusErr:  fmt.Errorf("connection refused"),
		statusMap:  map[string]backend.VMStatus{},
	}
	setupStartTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"start", "testvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_start_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "connection refused")
}

// --- Property-based tests ---

// Property: JSON output from start always contains ok=true, data.name, data.status="running".
func TestProperty_StartJSONAlwaysValid(t *testing.T) {
	names := []string{"vm-1", "my-vm", "test", "a", "production-vm-2024"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := &mockStartBackend{
				name:      "mock",
				available: true,
				statusMap: map[string]backend.VMStatus{name: backend.StatusStopped},
			}
			setupStartTest(t, mb)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"--json", "start", name})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)

			var result map[string]any
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err, "JSON must parse for name %q: %s", name, buf.String())
			assert.True(t, result["ok"].(bool), "ok must be true for %q", name)

			data := result["data"].(map[string]any)
			assert.Equal(t, name, data["name"], "data.name must match for %q", name)
			assert.Equal(t, "running", data["status"], "data.status must be 'running' for %q", name)
		})
	}
}

// Property: human output always mentions the VM name.
func TestProperty_StartHumanOutputContainsName(t *testing.T) {
	names := []string{"alpha", "beta", "gamma", "vm-with-dash", "vm_with_underscore"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := &mockStartBackend{
				name:      "mock",
				available: true,
				statusMap: map[string]backend.VMStatus{name: backend.StatusStopped},
			}
			setupStartTest(t, mb)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"start", name})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)
			assert.Contains(t, buf.String(), name, "human output must contain VM name %q", name)
		})
	}
}

// Property: error codes are always snake_case.
func TestProperty_StartErrorCodesSnakeCase(t *testing.T) {
	t.Run("vm_not_found", func(t *testing.T) {
		mb := &mockStartBackend{
			name:      "mock",
			available: true,
			statusMap: map[string]backend.VMStatus{},
		}
		setupStartTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"start", "test"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "vm_not_found", cliErr.Code)
	})

	t.Run("vm_already_running", func(t *testing.T) {
		mb := &mockStartBackend{
			name:      "mock",
			available: true,
			statusMap: map[string]backend.VMStatus{"test": backend.StatusRunning},
		}
		setupStartTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"start", "test"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "vm_already_running", cliErr.Code)
	})

	t.Run("backend_unavailable", func(t *testing.T) {
		mb := &mockStartBackend{name: "mock", available: false}
		setupStartTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"start", "test"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "backend_unavailable", cliErr.Code)
	})

	t.Run("invalid_argument_empty_name", func(t *testing.T) {
		mb := &mockStartBackend{name: "mock", available: true}
		setupStartTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"start", ""})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "invalid_argument", cliErr.Code)
	})
}

// Property: start on a running VM never calls the backend Start method.
func TestProperty_StartRunningVM_NeverCallsBackend(t *testing.T) {
	names := []string{"vm1", "vm2", "important", "prod"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := &mockStartBackend{
				name:      "mock",
				available: true,
				statusMap: map[string]backend.VMStatus{name: backend.StatusRunning},
			}
			setupStartTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"start", name})
			err := root.Execute()
			require.Error(t, err)
			assert.Empty(t, mb.started, "start on running VM must not call backend.Start for %q", name)
		})
	}
}

// Property: start on a stopped VM always calls the backend exactly once.
func TestProperty_StartStoppedVM_CallsBackendOnce(t *testing.T) {
	names := []string{"vm1", "vm2", "test-vm"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := &mockStartBackend{
				name:      "mock",
				available: true,
				statusMap: map[string]backend.VMStatus{name: backend.StatusStopped},
			}
			setupStartTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"start", name})
			err := root.Execute()
			require.NoError(t, err)
			require.Len(t, mb.started, 1, "start must call backend exactly once for %q", name)
			assert.Equal(t, name, mb.started[0])
		})
	}
}

// Property: JSON output always contains required fields (ok, data.name, data.status).
func TestProperty_StartJSONRequiredFields(t *testing.T) {
	mb := &mockStartBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{"myvm": backend.StatusStopped},
	}
	setupStartTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "start", "myvm"})
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
	assert.Contains(t, data, "name", "data must have 'name' field")
	assert.Contains(t, data, "status", "data must have 'status' field")
}

// Property: error JSON format is consistent for already-running case.
func TestProperty_StartErrorJSONFormat(t *testing.T) {
	mb := &mockStartBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{"testvm": backend.StatusRunning},
	}
	setupStartTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"start", "testvm"})
	execErr := root.Execute()

	require.Error(t, execErr)

	// Verify the CLIError structure
	cliErr, ok := execErr.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_already_running", cliErr.Code)
	assert.Contains(t, cliErr.Message, "testvm")
}
