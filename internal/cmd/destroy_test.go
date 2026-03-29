// Package cmd provides tests for the destroy command.
// REQ-002-003: VM Management Commands -- destroy
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

// mockDestroyBackend is a digital twin of a backend for destroy command testing.
// It implements backend.Backend with configurable Destroy behavior and records calls.
type mockDestroyBackend struct {
	name        string
	available   bool
	destroyed   []string
	err         error // error to return from Destroy
	statusMap   map[string]backend.VMStatus
}

func (m *mockDestroyBackend) Name() string { return m.name }
func (m *mockDestroyBackend) Available() error {
	if m.available {
		return nil
	}
	return fmt.Errorf("backend not available")
}
func (m *mockDestroyBackend) Create(_ context.Context, name string, _ backend.VMConfig) error {
	return nil
}
func (m *mockDestroyBackend) Start(_ context.Context, _ string) error  { return nil }
func (m *mockDestroyBackend) Stop(_ context.Context, _ string) error   { return nil }
func (m *mockDestroyBackend) Destroy(_ context.Context, name string) error {
	if m.err != nil {
		return m.err
	}
	m.destroyed = append(m.destroyed, name)
	return nil
}
func (m *mockDestroyBackend) Status(_ context.Context, name string) (backend.VMStatus, error) {
	if s, ok := m.statusMap[name]; ok {
		return s, nil
	}
	return "", backend.ErrVMNotFound
}
func (m *mockDestroyBackend) List(_ context.Context) ([]backend.VMInfo, error) {
	return nil, nil
}
func (m *mockDestroyBackend) SSHConfig(_ context.Context, _ string) (backend.SSHConfig, error) {
	return backend.SSHConfig{}, nil
}
func (m *mockDestroyBackend) Exec(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
	return backend.ExecResult{}, nil
}

// setupDestroyTest configures the test environment with a mock backend.
// Returns the mock so tests can inspect recorded calls.
func setupDestroyTest(t *testing.T, mb *mockDestroyBackend) {
	t.Helper()
	newRootTestEnv(t)

	// Reset destroy command's local flags to prevent state leaking between tests
	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "destroy" {
			_ = cmd.Flags().Set("force", "false")
			break
		}
	}

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })
}

// --- Unit tests ---

func TestDestroyCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "destroy" {
			found = true
			assert.Equal(t, "vm", cmd.GroupID)
			break
		}
	}
	assert.True(t, found, "destroy command must be registered")
}

func TestDestroyCommand_ExactArgs(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"destroy"})
	require.NoError(t, err)
	assert.NotNil(t, cmd.Args, "destroy must have an Args validator")
	// Verify it rejects 0 args
	err = cmd.Args(cmd, nil)
	assert.Error(t, err, "destroy must reject zero args")
}

func TestDestroyCommand_ForceFlag(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"destroy"})
	require.NoError(t, err)

	flag := cmd.Flags().Lookup("force")
	require.NotNil(t, flag, "must have --force flag")
	assert.Equal(t, "f", flag.Shorthand)
	assert.Equal(t, "false", flag.DefValue)
}

func TestDestroyCommand_BasicDestroy_HumanOutput(t *testing.T) {
	mb := &mockDestroyBackend{name: "mock", available: true}
	setupDestroyTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"destroy", "testvm", "--force"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "testvm")
	assert.Contains(t, buf.String(), "destroyed")

	// Verify backend was called
	require.Len(t, mb.destroyed, 1)
	assert.Equal(t, "testvm", mb.destroyed[0])
}

func TestDestroyCommand_BasicDestroy_JSONOutput(t *testing.T) {
	mb := &mockDestroyBackend{name: "mock", available: true}
	setupDestroyTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "destroy", "testvm", "--force"})
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
}

func TestDestroyCommand_RequiresForce(t *testing.T) {
	mb := &mockDestroyBackend{name: "mock", available: true}
	setupDestroyTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"destroy", "testvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok, "must be a CLIError")
	assert.Equal(t, "invalid_argument", cliErr.Code)
	assert.Contains(t, cliErr.Message, "--force")
	assert.Contains(t, cliErr.Message, "testvm")

	// Verify backend was NOT called
	assert.Empty(t, mb.destroyed)
}

func TestDestroyCommand_ShortForceFlag(t *testing.T) {
	mb := &mockDestroyBackend{name: "mock", available: true}
	setupDestroyTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"destroy", "testvm", "-f"})
	err := root.Execute()

	require.NoError(t, err)
	require.Len(t, mb.destroyed, 1)
	assert.Equal(t, "testvm", mb.destroyed[0])
}

func TestDestroyCommand_VMNotFound(t *testing.T) {
	mb := &mockDestroyBackend{
		name:      "mock",
		available: true,
		err:       backend.ErrVMNotFound,
	}
	setupDestroyTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"destroy", "nonexistent", "--force"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
	assert.Contains(t, cliErr.Message, "nonexistent")
}

func TestDestroyCommand_BackendUnavailable(t *testing.T) {
	mb := &mockDestroyBackend{name: "mock", available: false}
	setupDestroyTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"destroy", "testvm", "--force"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestDestroyCommand_GenericError(t *testing.T) {
	mb := &mockDestroyBackend{
		name:      "mock",
		available: true,
		err:       fmt.Errorf("disk full"),
	}
	setupDestroyTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"destroy", "testvm", "--force"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_destroy_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "disk full")
}

func TestDestroyCommand_MissingName(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	root.SetArgs([]string{"destroy"})
	err := root.Execute()
	assert.Error(t, err, "destroy without name must fail")
}

func TestDestroyCommand_EmptyName(t *testing.T) {
	mb := &mockDestroyBackend{name: "mock", available: true}
	setupDestroyTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"destroy", "", "--force"})
	err := root.Execute()
	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
}

// --- Property-based tests ---

// Property: JSON output from destroy always contains ok=true and data.name matching the arg.
func TestProperty_DestroyJSONAlwaysValid(t *testing.T) {
	names := []string{"vm-1", "my-vm", "test", "a", "production-vm-2024"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := &mockDestroyBackend{name: "mock", available: true}
			setupDestroyTest(t, mb)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"--json", "destroy", name, "--force"})
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
		})
	}
}

// Property: human output always mentions the VM name.
func TestProperty_DestroyHumanOutputContainsName(t *testing.T) {
	names := []string{"alpha", "beta", "gamma", "vm-with-dash", "vm_with_underscore"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := &mockDestroyBackend{name: "mock", available: true}
			setupDestroyTest(t, mb)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"destroy", name, "--force"})
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
func TestProperty_DestroyErrorCodesSnakeCase(t *testing.T) {
	t.Run("vm_not_found", func(t *testing.T) {
		mb := &mockDestroyBackend{name: "mock", available: true, err: backend.ErrVMNotFound}
		setupDestroyTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"destroy", "test", "--force"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "vm_not_found", cliErr.Code)
	})

	t.Run("backend_unavailable", func(t *testing.T) {
		mb := &mockDestroyBackend{name: "mock", available: false}
		setupDestroyTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"destroy", "test", "--force"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "backend_unavailable", cliErr.Code)
	})

	t.Run("invalid_argument_no_force", func(t *testing.T) {
		mb := &mockDestroyBackend{name: "mock", available: true}
		setupDestroyTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"destroy", "test"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "invalid_argument", cliErr.Code)
	})
}

// Property: destroy without --force never calls the backend.
func TestProperty_DestroyWithoutForceNeverCallsBackend(t *testing.T) {
	names := []string{"vm1", "vm2", "important", "prod"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := &mockDestroyBackend{name: "mock", available: true}
			setupDestroyTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"destroy", name})
			err := root.Execute()
			require.Error(t, err)
			assert.Empty(t, mb.destroyed, "destroy without --force must not call backend for %q", name)
		})
	}
}

// Property: destroy with --force always calls the backend exactly once.
func TestProperty_DestroyWithForceCallsBackendOnce(t *testing.T) {
	names := []string{"vm1", "vm2", "test-vm"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := &mockDestroyBackend{name: "mock", available: true}
			setupDestroyTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"destroy", name, "--force"})
			err := root.Execute()
			require.NoError(t, err)
			require.Len(t, mb.destroyed, 1, "destroy must call backend exactly once for %q", name)
			assert.Equal(t, name, mb.destroyed[0])
		})
	}
}

// Property: JSON output always contains required fields (ok, data.name).
func TestProperty_DestroyJSONRequiredFields(t *testing.T) {
	mb := &mockDestroyBackend{name: "mock", available: true}
	setupDestroyTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "destroy", "myvm", "--force"})
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
}

// Property: error JSON format is consistent.
func TestProperty_DestroyErrorJSONFormat(t *testing.T) {
	mb := &mockDestroyBackend{name: "mock", available: true}
	setupDestroyTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "destroy", "testvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.Error(t, execErr)

	// The error output goes through Execute() -> formatter.Error() in production,
	// but in test mode the error is returned. Verify the CLIError structure.
	cliErr, ok := execErr.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
	assert.Contains(t, cliErr.Message, "--force")
}
