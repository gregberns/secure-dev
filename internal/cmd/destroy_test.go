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

// mockDestroySnapshotBackend extends mockDestroyBackend with Snapshotter support.
// REQ-004-019: digital twin for auto-snapshot before destroy testing.
type mockDestroySnapshotBackend struct {
	mockDestroyBackend
	snapshots    []string // tags of created snapshots
	snapshotErr  error    // error to return from SnapshotCreate
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

// Snapshotter interface methods for mockDestroySnapshotBackend.
func (m *mockDestroySnapshotBackend) SnapshotCreate(_ context.Context, _, tag string) error {
	if m.snapshotErr != nil {
		return m.snapshotErr
	}
	m.snapshots = append(m.snapshots, tag)
	return nil
}
func (m *mockDestroySnapshotBackend) SnapshotApply(_ context.Context, _, _ string) error { return nil }
func (m *mockDestroySnapshotBackend) SnapshotDelete(_ context.Context, _, _ string) error { return nil }
func (m *mockDestroySnapshotBackend) SnapshotList(_ context.Context, _ string) ([]backend.SnapshotInfo, error) {
	return nil, nil
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
			_ = cmd.Flags().Set("no-snapshot", "false")
			break
		}
	}

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })
}

// setupDestroySnapshotTest configures the test environment with a snapshot-capable mock backend.
func setupDestroySnapshotTest(t *testing.T, mb *mockDestroySnapshotBackend) {
	t.Helper()
	newRootTestEnv(t)

	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "destroy" {
			_ = cmd.Flags().Set("force", "false")
			_ = cmd.Flags().Set("no-snapshot", "false")
			break
		}
	}

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	// Use a fixed snapshot tag for deterministic tests
	origAutoSnapshotTag := autoSnapshotTag
	autoSnapshotTag = func(name string) string { return "pre-destroy-20260329-120000" }
	t.Cleanup(func() { autoSnapshotTag = origAutoSnapshotTag })
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
	names := []string{"alpha", "beta", "gamma", "vm-with-dash", "vm-with-mixed-1"}
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

// --- REQ-004-031: SSH cleanup tests ---

func TestDestroyCommand_CleansUpSSHDir(t *testing.T) {
	mb := &mockDestroyBackend{name: "mock", available: true}
	setupDestroyTest(t, mb)

	cleaned := false
	origRemove := removeSSHDir
	removeSSHDir = func(sdHome, vmName string) error {
		cleaned = true
		return nil
	}
	defer func() { removeSSHDir = origRemove }()

	root := RootCmd()
	root.SetArgs([]string{"destroy", "testvm", "--force"})
	err := root.Execute()

	require.NoError(t, err)
	assert.True(t, cleaned, "destroy must clean up SSH directory")
}

func TestDestroyCommand_SSHCleanupFailure_NonFatal(t *testing.T) {
	mb := &mockDestroyBackend{name: "mock", available: true}
	setupDestroyTest(t, mb)

	origRemove := removeSSHDir
	removeSSHDir = func(sdHome, vmName string) error {
		return fmt.Errorf("permission denied")
	}
	defer func() { removeSSHDir = origRemove }()

	root := RootCmd()
	root.SetArgs([]string{"destroy", "testvm", "--force"})
	err := root.Execute()

	// Destroy should still succeed despite SSH cleanup failure
	require.NoError(t, err)
	require.Len(t, mb.destroyed, 1, "backend Destroy must still be called")
}

// Property: destroy always attempts SSH directory cleanup.
func TestProperty_Destroy_AlwaysCleansSSHDir(t *testing.T) {
	for _, name := range []string{"vm1", "vm2", "test-vm"} {
		t.Run(name, func(t *testing.T) {
			mb := &mockDestroyBackend{name: "mock", available: true}
			setupDestroyTest(t, mb)

			cleaned := false
			origRemove := removeSSHDir
			removeSSHDir = func(sdHome, vmName string) error {
				cleaned = true
				return nil
			}
			defer func() { removeSSHDir = origRemove }()

			root := RootCmd()
			root.SetArgs([]string{"destroy", name, "--force"})
			err := root.Execute()

			require.NoError(t, err)
			assert.True(t, cleaned, "destroy must attempt SSH cleanup for %q", name)
		})
	}
}

// Property: SSH cleanup failure never prevents destroy from succeeding.
func TestProperty_Destroy_SSHCleanupFailureNonFatal(t *testing.T) {
	for _, name := range []string{"vm1", "vm2", "vm3"} {
		t.Run(name, func(t *testing.T) {
			mb := &mockDestroyBackend{name: "mock", available: true}
			setupDestroyTest(t, mb)

			origRemove := removeSSHDir
			removeSSHDir = func(sdHome, vmName string) error {
				return fmt.Errorf("simulated failure")
			}
			defer func() { removeSSHDir = origRemove }()

			root := RootCmd()
			root.SetArgs([]string{"destroy", name, "--force"})
			err := root.Execute()

			require.NoError(t, err, "destroy must succeed despite SSH cleanup failure for %q", name)
		})
	}
}

// --- REQ-004-019: Snapshot Before Destructive Operations ---

func TestDestroyCommand_NoSnapshotFlag(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"destroy"})
	require.NoError(t, err)

	flag := cmd.Flags().Lookup("no-snapshot")
	require.NotNil(t, flag, "must have --no-snapshot flag")
	assert.Equal(t, "false", flag.DefValue)
}

// Test that auto-snapshot is created when backend supports Snapshotter.
func TestDestroyCommand_AutoSnapshot_Created(t *testing.T) {
	mb := &mockDestroySnapshotBackend{
		mockDestroyBackend: mockDestroyBackend{name: "mock", available: true},
	}
	setupDestroySnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"destroy", "testvm", "--force"})
	err := root.Execute()

	require.NoError(t, err)
	require.Len(t, mb.destroyed, 1)
	assert.Equal(t, "testvm", mb.destroyed[0])
	require.Len(t, mb.snapshots, 1, "auto-snapshot must be created before destroy")
	assert.Equal(t, "pre-destroy-20260329-120000", mb.snapshots[0])
}

// Test that auto-snapshot tag appears in human output.
func TestDestroyCommand_AutoSnapshot_HumanOutput(t *testing.T) {
	mb := &mockDestroySnapshotBackend{
		mockDestroyBackend: mockDestroyBackend{name: "mock", available: true},
	}
	setupDestroySnapshotTest(t, mb)

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
	output := buf.String()
	assert.Contains(t, output, "testvm")
	assert.Contains(t, output, "destroyed")
	assert.Contains(t, output, "Safety snapshot")
	assert.Contains(t, output, "pre-destroy-20260329-120000")
}

// Test that auto-snapshot tag appears in JSON output.
func TestDestroyCommand_AutoSnapshot_JSONOutput(t *testing.T) {
	mb := &mockDestroySnapshotBackend{
		mockDestroyBackend: mockDestroyBackend{name: "mock", available: true},
	}
	setupDestroySnapshotTest(t, mb)

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
	assert.Equal(t, "pre-destroy-20260329-120000", data["snapshot_tag"])
}

// Test that --no-snapshot skips auto-snapshot.
func TestDestroyCommand_NoSnapshotFlag_SkipsSnapshot(t *testing.T) {
	mb := &mockDestroySnapshotBackend{
		mockDestroyBackend: mockDestroyBackend{name: "mock", available: true},
	}
	setupDestroySnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"destroy", "testvm", "--force", "--no-snapshot"})
	err := root.Execute()

	require.NoError(t, err)
	require.Len(t, mb.destroyed, 1)
	assert.Empty(t, mb.snapshots, "no snapshot should be created with --no-snapshot")
}

// Test that --no-snapshot JSON output has no snapshot_tag.
func TestDestroyCommand_NoSnapshotFlag_JSONOutput(t *testing.T) {
	mb := &mockDestroySnapshotBackend{
		mockDestroyBackend: mockDestroyBackend{name: "mock", available: true},
	}
	setupDestroySnapshotTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "destroy", "testvm", "--force", "--no-snapshot"})
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
	assert.Equal(t, "testvm", data["name"])
	assert.Nil(t, data["snapshot_tag"], "snapshot_tag must be absent with --no-snapshot")
}

// Test that snapshot failure is non-fatal (destroy proceeds).
func TestDestroyCommand_SnapshotFailure_NonFatal(t *testing.T) {
	mb := &mockDestroySnapshotBackend{
		mockDestroyBackend: mockDestroyBackend{name: "mock", available: true},
		snapshotErr:        fmt.Errorf("disk full for snapshots"),
	}
	setupDestroySnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"destroy", "testvm", "--force"})
	err := root.Execute()

	require.NoError(t, err, "destroy must succeed despite snapshot failure")
	require.Len(t, mb.destroyed, 1, "destroy must proceed after snapshot failure")
	assert.Empty(t, mb.snapshots, "no snapshot should be recorded on failure")
}

// Test that snapshot failure JSON has no snapshot_tag.
func TestDestroyCommand_SnapshotFailure_JSONNoTag(t *testing.T) {
	mb := &mockDestroySnapshotBackend{
		mockDestroyBackend: mockDestroyBackend{name: "mock", available: true},
		snapshotErr:        fmt.Errorf("snapshot error"),
	}
	setupDestroySnapshotTest(t, mb)

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
	require.NoError(t, err)

	data := result["data"].(map[string]any)
	assert.Nil(t, data["snapshot_tag"], "snapshot_tag must be absent on snapshot failure")
}

// Test that non-snapshotter backends proceed without auto-snapshot.
func TestDestroyCommand_NonSnapshotterBackend_NoAutoSnapshot(t *testing.T) {
	mb := &mockDestroyBackend{name: "mock", available: true}
	setupDestroyTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"destroy", "testvm", "--force"})
	err := root.Execute()

	require.NoError(t, err)
	require.Len(t, mb.destroyed, 1)
}

// Test that non-snapshotter backend JSON output has no snapshot_tag.
func TestDestroyCommand_NonSnapshotterBackend_JSONNoTag(t *testing.T) {
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
	require.NoError(t, err)

	data := result["data"].(map[string]any)
	assert.Nil(t, data["snapshot_tag"], "snapshot_tag must be absent for non-snapshotter backend")
}

// --- REQ-004-019 Property-based tests ---

// Property: destroy with snapshotting backend always creates exactly one snapshot.
func TestProperty_Destroy_SnapshotterAlwaysSnapshots(t *testing.T) {
	for _, name := range []string{"vm1", "vm2", "test-vm", "alpha", "prod-vm"} {
		t.Run(name, func(t *testing.T) {
			mb := &mockDestroySnapshotBackend{
				mockDestroyBackend: mockDestroyBackend{name: "mock", available: true},
			}
			setupDestroySnapshotTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"destroy", name, "--force"})
			err := root.Execute()

			require.NoError(t, err)
			require.Len(t, mb.snapshots, 1, "must create exactly one auto-snapshot for %q", name)
			assert.Contains(t, mb.snapshots[0], "pre-destroy-", "snapshot tag must contain prefix for %q", name)
		})
	}
}

// Property: --no-snapshot never creates a snapshot.
func TestProperty_Destroy_NoSnapshotNeverCreates(t *testing.T) {
	for _, name := range []string{"vm1", "vm2", "test-vm", "alpha", "prod-vm"} {
		t.Run(name, func(t *testing.T) {
			mb := &mockDestroySnapshotBackend{
				mockDestroyBackend: mockDestroyBackend{name: "mock", available: true},
			}
			setupDestroySnapshotTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"destroy", name, "--force", "--no-snapshot"})
			err := root.Execute()

			require.NoError(t, err)
			assert.Empty(t, mb.snapshots, "--no-snapshot must never create snapshots for %q", name)
		})
	}
}

// Property: auto-snapshot JSON always valid with snapshot_tag.
func TestProperty_Destroy_AutoSnapshotJSONValid(t *testing.T) {
	for _, name := range []string{"vm1", "vm2", "test-vm", "alpha", "prod-vm"} {
		t.Run(name, func(t *testing.T) {
			mb := &mockDestroySnapshotBackend{
				mockDestroyBackend: mockDestroyBackend{name: "mock", available: true},
			}
			setupDestroySnapshotTest(t, mb)

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
			require.NoError(t, err, "JSON must parse for %q: %s", name, buf.String())

			assert.True(t, result["ok"].(bool))
			data := result["data"].(map[string]any)
			assert.Equal(t, name, data["name"])
			assert.NotNil(t, data["snapshot_tag"], "snapshot_tag must be present for %q", name)
		})
	}
}

// Property: non-snapshotter backend never has snapshot_tag in JSON.
func TestProperty_Destroy_NonSnapshotterJSONNoTag(t *testing.T) {
	for _, name := range []string{"vm1", "vm2", "test-vm"} {
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
			require.NoError(t, err)

			data := result["data"].(map[string]any)
			assert.Nil(t, data["snapshot_tag"], "non-snapshotter backend must not have snapshot_tag for %q", name)
		})
	}
}

// Property: snapshot failure never prevents destroy.
func TestProperty_Destroy_SnapshotFailureNeverBlocks(t *testing.T) {
	for _, name := range []string{"vm1", "vm2", "vm3"} {
		t.Run(name, func(t *testing.T) {
			mb := &mockDestroySnapshotBackend{
				mockDestroyBackend: mockDestroyBackend{name: "mock", available: true},
				snapshotErr:        fmt.Errorf("snapshot failed: %s", name),
			}
			setupDestroySnapshotTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"destroy", name, "--force"})
			err := root.Execute()

			require.NoError(t, err, "destroy must succeed despite snapshot failure for %q", name)
			require.Len(t, mb.destroyed, 1, "destroy must be called for %q", name)
		})
	}
}

// Property: snapshot tag always has pre-destroy prefix.
func TestProperty_Destroy_SnapshotTagPrefix(t *testing.T) {
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("vm-%d", i)
		t.Run(name, func(t *testing.T) {
			mb := &mockDestroySnapshotBackend{
				mockDestroyBackend: mockDestroyBackend{name: "mock", available: true},
			}
			setupDestroySnapshotTest(t, mb)

			// Use a unique tag for each iteration
			tagNum := fmt.Sprintf("pre-destroy-20260329-%06d", i*10000)
			origAutoSnapshotTag := autoSnapshotTag
			autoSnapshotTag = func(name string) string { return tagNum }
			defer func() { autoSnapshotTag = origAutoSnapshotTag }()

			root := RootCmd()
			root.SetArgs([]string{"destroy", name, "--force"})
			err := root.Execute()

			require.NoError(t, err)
			require.Len(t, mb.snapshots, 1)
			assert.Contains(t, mb.snapshots[0], "pre-destroy-", "tag must have pre-destroy prefix for %q", name)
		})
	}
}
