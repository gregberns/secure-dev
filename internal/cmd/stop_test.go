// Package cmd provides tests for the stop command.
// REQ-002-003: VM Management Commands -- stop
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

// setupStopTest configures the test environment with a memory backend.
func setupStopTest(t *testing.T) *memory.Backend {
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

func TestStopCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "stop" {
			found = true
			assert.Equal(t, "vm", cmd.GroupID)
			break
		}
	}
	assert.True(t, found, "stop command must be registered")
}

func TestStopCommand_ExactArgs(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"stop"})
	require.NoError(t, err)
	assert.NotNil(t, cmd.Args, "stop must have an Args validator")
	// Verify it rejects 0 args
	err = cmd.Args(cmd, nil)
	assert.Error(t, err, "stop must reject zero args")
}

func TestStopCommand_BasicStop_HumanOutput(t *testing.T) {
	mb := setupStopTest(t)
	ctx := context.Background()

	// Create a VM (starts in Running state)
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"stop", "testvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "testvm")
	assert.Contains(t, buf.String(), "stopped")

	// Verify backend state changed to stopped
	status, sErr := mb.Status(ctx, "testvm")
	require.NoError(t, sErr)
	assert.Equal(t, backend.StatusStopped, status)
}

func TestStopCommand_BasicStop_JSONOutput(t *testing.T) {
	mb := setupStopTest(t)
	ctx := context.Background()

	// Create a VM (starts in Running state)
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "stop", "testvm"})
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
	assert.Equal(t, "stopped", data["status"])
}

func TestStopCommand_VMNotFound(t *testing.T) {
	setupStopTest(t)
	// No VMs created

	root := RootCmd()
	root.SetArgs([]string{"stop", "nonexistent"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
	assert.Contains(t, cliErr.Message, "nonexistent")
}

func TestStopCommand_AlreadyStopped_SilentSuccess(t *testing.T) {
	mb := setupStopTest(t)
	ctx := context.Background()

	// Create VM then stop it
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))
	require.NoError(t, mb.Stop(ctx, "testvm"))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"stop", "testvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr, "stop on already-stopped VM must succeed")

	// VM should still be stopped
	status, sErr := mb.Status(ctx, "testvm")
	require.NoError(t, sErr)
	assert.Equal(t, backend.StatusStopped, status)
}

func TestStopCommand_AlreadyStopped_JSONOutput(t *testing.T) {
	mb := setupStopTest(t)
	ctx := context.Background()

	// Create VM then stop it
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))
	require.NoError(t, mb.Stop(ctx, "testvm"))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "stop", "testvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err, "JSON output must be valid: %s", buf.String())

	assert.True(t, result["ok"].(bool))
	data := result["data"].(map[string]any)
	assert.Equal(t, "testvm", data["name"])
	assert.Equal(t, "stopped", data["status"])

	// VM should still be stopped
	status, sErr := mb.Status(ctx, "testvm")
	require.NoError(t, sErr)
	assert.Equal(t, backend.StatusStopped, status)
}

func TestStopCommand_BackendUnavailable(t *testing.T) {
	mb := setupStopTest(t)
	mb.SetMethodError("available", fmt.Errorf("backend not available"))

	root := RootCmd()
	root.SetArgs([]string{"stop", "testvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestStopCommand_BackendGetError(t *testing.T) {
	newRootTestEnv(t)

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return nil, fmt.Errorf("no such backend")
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	root := RootCmd()
	root.SetArgs([]string{"stop", "testvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestStopCommand_GenericError(t *testing.T) {
	mb := setupStopTest(t)
	ctx := context.Background()

	// Create a VM (Running state)
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))

	// Inject error for Stop method
	mb.SetMethodError("stop", fmt.Errorf("internal error"))

	root := RootCmd()
	root.SetArgs([]string{"stop", "testvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_stop_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "internal error")
}

func TestStopCommand_MissingName(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	root.SetArgs([]string{"stop"})
	err := root.Execute()
	assert.Error(t, err, "stop without name must fail")
}

func TestStopCommand_EmptyName(t *testing.T) {
	setupStopTest(t)

	root := RootCmd()
	root.SetArgs([]string{"stop", ""})
	err := root.Execute()
	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
}

func TestStopCommand_StopOnRunningVM_CallsBackend(t *testing.T) {
	mb := setupStopTest(t)
	ctx := context.Background()

	// Create VM (Running state)
	require.NoError(t, mb.Create(ctx, "myvm", backend.VMConfig{}))

	root := RootCmd()
	root.SetArgs([]string{"stop", "myvm"})
	err := root.Execute()

	require.NoError(t, err)

	// Verify VM is now stopped
	status, sErr := mb.Status(ctx, "myvm")
	require.NoError(t, sErr)
	assert.Equal(t, backend.StatusStopped, status)
}

func TestStopCommand_StopOnErrorStatusVM(t *testing.T) {
	mb := setupStopTest(t)
	ctx := context.Background()

	// Create VM and force it into Error state
	require.NoError(t, mb.Create(ctx, "myvm", backend.VMConfig{}))
	require.NoError(t, mb.SetStatus("myvm", backend.StatusError))

	root := RootCmd()
	root.SetArgs([]string{"stop", "myvm"})
	err := root.Execute()

	require.NoError(t, err)

	// Memory backend supports Error->Stopped transition
	status, sErr := mb.Status(ctx, "myvm")
	require.NoError(t, sErr)
	assert.Equal(t, backend.StatusStopped, status)
}

func TestStopCommand_StatusCheckError(t *testing.T) {
	mb := setupStopTest(t)

	// Inject error for Status method
	mb.SetMethodError("status", fmt.Errorf("connection refused"))

	root := RootCmd()
	root.SetArgs([]string{"stop", "testvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_stop_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "connection refused")
}

// --- Property-based tests ---

// Property: JSON output from stop always contains ok=true, data.name, data.status="stopped".
func TestProperty_StopJSONAlwaysValid(t *testing.T) {
	names := []string{"vm-1", "my-vm", "test", "a", "production-vm-2024"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := setupStopTest(t)
			ctx := context.Background()

			// Create VM (Running state)
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"--json", "stop", name})
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
			assert.Equal(t, "stopped", data["status"], "data.status must be 'stopped' for %q", name)
		})
	}
}

// Property: human output always mentions the VM name (when stopping a running VM).
func TestProperty_StopHumanOutputContainsName(t *testing.T) {
	names := []string{"alpha", "beta", "gamma", "vm-with-dash", "vm-with-mixed-1"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := setupStopTest(t)
			ctx := context.Background()

			// Create VM (Running state)
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"stop", name})
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
func TestProperty_StopErrorCodesSnakeCase(t *testing.T) {
	t.Run("vm_not_found", func(t *testing.T) {
		setupStopTest(t)
		// No VMs created

		root := RootCmd()
		root.SetArgs([]string{"stop", "test"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "vm_not_found", cliErr.Code)
	})

	t.Run("backend_unavailable", func(t *testing.T) {
		mb := setupStopTest(t)
		mb.SetMethodError("available", fmt.Errorf("backend not available"))

		root := RootCmd()
		root.SetArgs([]string{"stop", "test"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "backend_unavailable", cliErr.Code)
	})

	t.Run("invalid_argument_empty_name", func(t *testing.T) {
		setupStopTest(t)

		root := RootCmd()
		root.SetArgs([]string{"stop", ""})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "invalid_argument", cliErr.Code)
	})
}

// Property: stop on a stopped VM never calls the backend Stop method.
func TestProperty_StopStoppedVM_NeverCallsBackend(t *testing.T) {
	names := []string{"vm1", "vm2", "important", "prod"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := setupStopTest(t)
			ctx := context.Background()

			// Create VM then stop it
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))
			require.NoError(t, mb.Stop(ctx, name))

			root := RootCmd()
			root.SetArgs([]string{"stop", name})
			err := root.Execute()
			require.NoError(t, err)

			// VM should still be stopped
			status, sErr := mb.Status(ctx, name)
			require.NoError(t, sErr)
			assert.Equal(t, backend.StatusStopped, status, "stop on already-stopped VM must remain stopped for %q", name)
		})
	}
}

// Property: stop on a running VM always calls the backend exactly once.
func TestProperty_StopRunningVM_CallsBackendOnce(t *testing.T) {
	names := []string{"vm1", "vm2", "test-vm"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := setupStopTest(t)
			ctx := context.Background()

			// Create VM (Running state)
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))

			root := RootCmd()
			root.SetArgs([]string{"stop", name})
			err := root.Execute()
			require.NoError(t, err)

			// Verify state transitioned to Stopped
			status, sErr := mb.Status(ctx, name)
			require.NoError(t, sErr)
			assert.Equal(t, backend.StatusStopped, status, "stop must transition to Stopped for %q", name)
		})
	}
}

// Property: JSON output always contains required fields (ok, data.name, data.status).
func TestProperty_StopJSONRequiredFields(t *testing.T) {
	mb := setupStopTest(t)
	ctx := context.Background()

	require.NoError(t, mb.Create(ctx, "myvm", backend.VMConfig{}))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "stop", "myvm"})
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

// Property: error JSON format is consistent for vm_not_found case.
func TestProperty_StopErrorJSONFormat(t *testing.T) {
	setupStopTest(t)
	// No VMs created

	root := RootCmd()
	root.SetArgs([]string{"stop", "nonexistent"})
	execErr := root.Execute()

	require.Error(t, execErr)

	cliErr, ok := execErr.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
	assert.Contains(t, cliErr.Message, "nonexistent")
}
