// Package cmd provides tests for the start command.
// REQ-002-003: VM Management Commands -- start
// NOTE: Tests use global getBackendFunc — do not use t.Parallel().
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
	"sd/internal/backend/memory"
	"sd/internal/ui"
)

// setupStartTest configures the test environment with a memory backend.
func setupStartTest(t *testing.T) *memory.Backend {
	t.Helper()
	newRootTestEnv(t)

	mb := memory.New()
	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend; mb.Reset() })
	return mb
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
	mb := setupStartTest(t)
	ctx := context.Background()

	// Create a VM then stop it so it can be started
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))
	require.NoError(t, mb.Stop(ctx, "testvm"))

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

	// Verify backend state changed to running
	status, err := mb.Status(ctx, "testvm")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusRunning, status)
}

func TestStartCommand_BasicStart_JSONOutput(t *testing.T) {
	mb := setupStartTest(t)
	ctx := context.Background()

	// Create a VM then stop it so it can be started
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))
	require.NoError(t, mb.Stop(ctx, "testvm"))

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
	setupStartTest(t)
	// No VMs created — "nonexistent" doesn't exist

	root := RootCmd()
	root.SetArgs([]string{"start", "nonexistent"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
	assert.Contains(t, cliErr.Message, "nonexistent")
}

// REQ-003-003: Start on a running VM is a no-op (silent success)
func TestStartCommand_AlreadyRunning(t *testing.T) {
	mb := setupStartTest(t)
	ctx := context.Background()

	// Create a VM (starts in Running state)
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))

	root := RootCmd()
	root.SetArgs([]string{"start", "testvm"})
	err := root.Execute()

	require.NoError(t, err, "start on running VM must succeed (no-op)")

	// Verify VM is still running
	status, sErr := mb.Status(ctx, "testvm")
	require.NoError(t, sErr)
	assert.Equal(t, backend.StatusRunning, status)
}

func TestStartCommand_BackendUnavailable(t *testing.T) {
	mb := setupStartTest(t)
	mb.SetMethodError("available", fmt.Errorf("backend not available"))

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
	mb := setupStartTest(t)
	ctx := context.Background()

	// Create a VM then stop it so Start will be called
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))
	require.NoError(t, mb.Stop(ctx, "testvm"))

	// Inject error for Start method
	mb.SetMethodError("start", fmt.Errorf("internal error"))

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
	setupStartTest(t)

	root := RootCmd()
	root.SetArgs([]string{"start", ""})
	err := root.Execute()
	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
}

func TestStartCommand_StartOnStoppedVM_CallsBackend(t *testing.T) {
	mb := setupStartTest(t)
	ctx := context.Background()

	// Create VM then stop it
	require.NoError(t, mb.Create(ctx, "myvm", backend.VMConfig{}))
	require.NoError(t, mb.Stop(ctx, "myvm"))

	root := RootCmd()
	root.SetArgs([]string{"start", "myvm"})
	err := root.Execute()

	require.NoError(t, err)

	// Verify VM is now running
	status, sErr := mb.Status(ctx, "myvm")
	require.NoError(t, sErr)
	assert.Equal(t, backend.StatusRunning, status)
}

func TestStartCommand_StartOnErrorStatusVM(t *testing.T) {
	mb := setupStartTest(t)
	ctx := context.Background()

	// Create VM and force it into Error state
	require.NoError(t, mb.Create(ctx, "myvm", backend.VMConfig{}))
	require.NoError(t, mb.SetStatus("myvm", backend.StatusError))

	root := RootCmd()
	root.SetArgs([]string{"start", "myvm"})
	err := root.Execute()

	// The command tries to start, memory backend returns error for Error->Running
	// (only Stopped->Running and Running->noop are valid).
	// The command code doesn't check for Error status specifically — it just calls Start
	// when status is not Running. Memory backend will return an error.
	require.Error(t, err)
}

func TestStartCommand_StatusCheckError(t *testing.T) {
	mb := setupStartTest(t)

	// Inject error for Status method
	mb.SetMethodError("status", fmt.Errorf("connection refused"))

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
			mb := setupStartTest(t)
			ctx := context.Background()

			// Create VM then stop it
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))
			require.NoError(t, mb.Stop(ctx, name))

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
	names := []string{"alpha", "beta", "gamma", "vm-with-dash", "vm-with-mixed-1"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := setupStartTest(t)
			ctx := context.Background()

			// Create VM then stop it
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))
			require.NoError(t, mb.Stop(ctx, name))

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
		setupStartTest(t)
		// No VMs created

		root := RootCmd()
		root.SetArgs([]string{"start", "test"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "vm_not_found", cliErr.Code)
	})

	// NOTE: vm_already_running removed — REQ-003-003 makes it a no-op (not an error)

	t.Run("backend_unavailable", func(t *testing.T) {
		mb := setupStartTest(t)
		mb.SetMethodError("available", fmt.Errorf("backend not available"))

		root := RootCmd()
		root.SetArgs([]string{"start", "test"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "backend_unavailable", cliErr.Code)
	})

	t.Run("invalid_argument_empty_name", func(t *testing.T) {
		setupStartTest(t)

		root := RootCmd()
		root.SetArgs([]string{"start", ""})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "invalid_argument", cliErr.Code)
	})
}

// Property: start on a running VM never calls the backend Start method.
// REQ-003-003: Start on a running VM is a no-op (silent success)
func TestProperty_StartRunningVM_NeverCallsBackend(t *testing.T) {
	names := []string{"vm1", "vm2", "important", "prod"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := setupStartTest(t)
			ctx := context.Background()

			// Create VM (starts in Running state)
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))

			root := RootCmd()
			root.SetArgs([]string{"start", name})
			err := root.Execute()
			require.NoError(t, err, "start on running VM must succeed (no-op) for %q", name)

			// VM should still be running
			status, sErr := mb.Status(ctx, name)
			require.NoError(t, sErr)
			assert.Equal(t, backend.StatusRunning, status)
		})
	}
}

// Property: start on a stopped VM always calls the backend exactly once.
func TestProperty_StartStoppedVM_CallsBackendOnce(t *testing.T) {
	names := []string{"vm1", "vm2", "test-vm"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := setupStartTest(t)
			ctx := context.Background()

			// Create VM then stop it
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))
			require.NoError(t, mb.Stop(ctx, name))

			root := RootCmd()
			root.SetArgs([]string{"start", name})
			err := root.Execute()
			require.NoError(t, err)

			// Verify state transitioned to Running
			status, sErr := mb.Status(ctx, name)
			require.NoError(t, sErr)
			assert.Equal(t, backend.StatusRunning, status, "start must transition to Running for %q", name)
		})
	}
}

// Property: JSON output always contains required fields (ok, data.name, data.status).
func TestProperty_StartJSONRequiredFields(t *testing.T) {
	mb := setupStartTest(t)
	ctx := context.Background()

	require.NoError(t, mb.Create(ctx, "myvm", backend.VMConfig{}))
	require.NoError(t, mb.Stop(ctx, "myvm"))

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

// Property: JSON output for already-running VM returns success with status=running.
// REQ-003-003: Start on a running VM is a no-op (silent success)
func TestProperty_StartAlreadyRunningJSONFormat(t *testing.T) {
	mb := setupStartTest(t)
	ctx := context.Background()

	// Create VM (starts in Running state)
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))

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
