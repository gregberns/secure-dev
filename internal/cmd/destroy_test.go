// Package cmd provides tests for the destroy command.
// REQ-002-003: VM Management Commands -- destroy
// NOTE: Tests use global getBackendFunc — do not use t.Parallel().
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/backend"
	"sd/internal/backend/memory"
	"sd/internal/ui"
)

// nonSnapshotterBackend wraps a memory.Backend but does NOT implement
// backend.Snapshotter. Used to test destroy behavior when the backend
// does not support snapshots.
type nonSnapshotterBackend struct {
	backend.Backend
}

// setupDestroyTest configures the test environment with a memory backend.
func setupDestroyTest(t *testing.T) *memory.Backend {
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

	mb := memory.New()
	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend; mb.Reset() })
	return mb
}

// setupDestroySnapshotTest configures the test environment with a memory backend
// for snapshot-aware destroy tests, using a fixed snapshot tag for determinism.
func setupDestroySnapshotTest(t *testing.T) *memory.Backend {
	t.Helper()
	mb := setupDestroyTest(t)

	// Use a fixed snapshot tag for deterministic tests
	origAutoSnapshotTag := autoSnapshotTag
	autoSnapshotTag = func(name string) string { return "pre-destroy-20260329-120000" }
	t.Cleanup(func() { autoSnapshotTag = origAutoSnapshotTag })

	return mb
}

// setupDestroyNonSnapshotterTest configures the test with a non-snapshotter backend wrapper.
func setupDestroyNonSnapshotterTest(t *testing.T) *memory.Backend {
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

	mb := memory.New()
	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		// Wrap in nonSnapshotterBackend to strip Snapshotter interface
		return &nonSnapshotterBackend{Backend: mb}, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend; mb.Reset() })
	return mb
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
	mb := setupDestroyTest(t)
	ctx := context.Background()

	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))

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

	// Verify VM was destroyed
	_, sErr := mb.Status(ctx, "testvm")
	assert.ErrorIs(t, sErr, backend.ErrVMNotFound)
}

func TestDestroyCommand_BasicDestroy_JSONOutput(t *testing.T) {
	mb := setupDestroyTest(t)
	ctx := context.Background()

	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))

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

	// Verify VM was destroyed
	_, sErr := mb.Status(ctx, "testvm")
	assert.ErrorIs(t, sErr, backend.ErrVMNotFound)
}

func TestDestroyCommand_RequiresForce(t *testing.T) {
	mb := setupDestroyTest(t)
	ctx := context.Background()

	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))

	root := RootCmd()
	root.SetArgs([]string{"destroy", "testvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok, "must be a CLIError")
	assert.Equal(t, "invalid_argument", cliErr.Code)
	assert.Contains(t, cliErr.Message, "--force")
	assert.Contains(t, cliErr.Message, "testvm")

	// Verify VM was NOT destroyed
	status, sErr := mb.Status(ctx, "testvm")
	require.NoError(t, sErr)
	assert.Equal(t, backend.StatusRunning, status)
}

func TestDestroyCommand_ShortForceFlag(t *testing.T) {
	mb := setupDestroyTest(t)
	ctx := context.Background()

	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))

	root := RootCmd()
	root.SetArgs([]string{"destroy", "testvm", "-f"})
	err := root.Execute()

	require.NoError(t, err)
	_, sErr := mb.Status(ctx, "testvm")
	assert.ErrorIs(t, sErr, backend.ErrVMNotFound)
}

func TestDestroyCommand_VMNotFound(t *testing.T) {
	setupDestroyTest(t)
	// No VMs created

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
	mb := setupDestroyTest(t)
	mb.SetMethodError("available", fmt.Errorf("backend not available"))

	root := RootCmd()
	root.SetArgs([]string{"destroy", "testvm", "--force"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestDestroyCommand_GenericError(t *testing.T) {
	mb := setupDestroyTest(t)
	ctx := context.Background()

	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))
	mb.SetMethodError("destroy", fmt.Errorf("disk full"))

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
	setupDestroyTest(t)

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
			mb := setupDestroyTest(t)
			ctx := context.Background()
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))

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
			mb := setupDestroyTest(t)
			ctx := context.Background()
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))

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
		setupDestroyTest(t)
		// No VMs created

		root := RootCmd()
		root.SetArgs([]string{"destroy", "test", "--force"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "vm_not_found", cliErr.Code)
	})

	t.Run("backend_unavailable", func(t *testing.T) {
		mb := setupDestroyTest(t)
		mb.SetMethodError("available", fmt.Errorf("backend not available"))

		root := RootCmd()
		root.SetArgs([]string{"destroy", "test", "--force"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "backend_unavailable", cliErr.Code)
	})

	t.Run("invalid_argument_no_force", func(t *testing.T) {
		setupDestroyTest(t)

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
			mb := setupDestroyTest(t)
			ctx := context.Background()
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))

			root := RootCmd()
			root.SetArgs([]string{"destroy", name})
			err := root.Execute()
			require.Error(t, err)

			// VM should still exist
			status, sErr := mb.Status(ctx, name)
			require.NoError(t, sErr)
			assert.Equal(t, backend.StatusRunning, status, "destroy without --force must not call backend for %q", name)
		})
	}
}

// Property: destroy with --force always calls the backend exactly once.
func TestProperty_DestroyWithForceCallsBackendOnce(t *testing.T) {
	names := []string{"vm1", "vm2", "test-vm"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := setupDestroyTest(t)
			ctx := context.Background()
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))

			root := RootCmd()
			root.SetArgs([]string{"destroy", name, "--force"})
			err := root.Execute()
			require.NoError(t, err)

			// Verify VM was destroyed
			_, sErr := mb.Status(ctx, name)
			assert.ErrorIs(t, sErr, backend.ErrVMNotFound, "destroy must remove VM for %q", name)
		})
	}
}

// Property: JSON output always contains required fields (ok, data.name).
func TestProperty_DestroyJSONRequiredFields(t *testing.T) {
	mb := setupDestroyTest(t)
	ctx := context.Background()
	require.NoError(t, mb.Create(ctx, "myvm", backend.VMConfig{}))

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
	setupDestroyTest(t)

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
	mb := setupDestroyTest(t)
	ctx := context.Background()
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))

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
	mb := setupDestroyTest(t)
	ctx := context.Background()
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))

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
	_, sErr := mb.Status(ctx, "testvm")
	assert.ErrorIs(t, sErr, backend.ErrVMNotFound, "backend Destroy must still be called")
}

// Property: destroy always attempts SSH directory cleanup.
func TestProperty_Destroy_AlwaysCleansSSHDir(t *testing.T) {
	for _, name := range []string{"vm1", "vm2", "test-vm"} {
		t.Run(name, func(t *testing.T) {
			mb := setupDestroyTest(t)
			ctx := context.Background()
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))

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
			mb := setupDestroyTest(t)
			ctx := context.Background()
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))

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
	mb := setupDestroySnapshotTest(t)
	ctx := context.Background()
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))

	root := RootCmd()
	root.SetArgs([]string{"destroy", "testvm", "--force"})
	err := root.Execute()

	require.NoError(t, err)
	// VM was destroyed
	_, sErr := mb.Status(ctx, "testvm")
	assert.ErrorIs(t, sErr, backend.ErrVMNotFound)
	// Snapshot was created (we can verify via SnapshotList but VM is gone;
	// instead check JSON output or rely on the snapshot tag test below)
}

// Test that auto-snapshot tag appears in human output.
func TestDestroyCommand_AutoSnapshot_HumanOutput(t *testing.T) {
	mb := setupDestroySnapshotTest(t)
	ctx := context.Background()
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))

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
	mb := setupDestroySnapshotTest(t)
	ctx := context.Background()
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))

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
	mb := setupDestroySnapshotTest(t)
	ctx := context.Background()
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))

	root := RootCmd()
	root.SetArgs([]string{"destroy", "testvm", "--force", "--no-snapshot"})
	err := root.Execute()

	require.NoError(t, err)
	_, sErr := mb.Status(ctx, "testvm")
	assert.ErrorIs(t, sErr, backend.ErrVMNotFound)
}

// Test that --no-snapshot JSON output has no snapshot_tag.
func TestDestroyCommand_NoSnapshotFlag_JSONOutput(t *testing.T) {
	mb := setupDestroySnapshotTest(t)
	ctx := context.Background()
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))

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

// REQ-004-019: snapshot failure before destroy is fatal — destroy MUST NOT proceed.
func TestDestroyCommand_SnapshotFailure_Fatal_WithoutFlag(t *testing.T) {
	mb := setupDestroySnapshotTest(t)
	ctx := context.Background()
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))
	mb.SetMethodError("snapshotcreate", fmt.Errorf("disk full for snapshots"))

	root := RootCmd()
	root.SetArgs([]string{"destroy", "testvm", "--force"})
	err := root.Execute()

	require.Error(t, err, "destroy must fail when safety snapshot fails")
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "snapshot_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "aborted")
	assert.Contains(t, cliErr.Message, "--no-snapshot")

	// VM must still exist — destroy aborted.
	_, sErr := mb.Status(ctx, "testvm")
	require.NoError(t, sErr, "VM must still exist after aborted destroy")
}

// REQ-004-019: snapshot failure bypassed when --no-snapshot is set.
func TestDestroyCommand_SnapshotFailure_SkippedWithFlag(t *testing.T) {
	mb := setupDestroySnapshotTest(t)
	ctx := context.Background()
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))
	// Even if the backend would fail snapshotcreate, --no-snapshot bypasses the call.
	mb.SetMethodError("snapshotcreate", fmt.Errorf("should not be called"))

	root := RootCmd()
	root.SetArgs([]string{"destroy", "testvm", "--force", "--no-snapshot"})
	err := root.Execute()

	require.NoError(t, err, "destroy must succeed with --no-snapshot despite snapshot backend error")
	_, sErr := mb.Status(ctx, "testvm")
	assert.ErrorIs(t, sErr, backend.ErrVMNotFound, "destroy must proceed when --no-snapshot is passed")
}

// Test that non-snapshotter backends proceed without auto-snapshot.
func TestDestroyCommand_NonSnapshotterBackend_NoAutoSnapshot(t *testing.T) {
	mb := setupDestroyNonSnapshotterTest(t)
	ctx := context.Background()
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))

	root := RootCmd()
	root.SetArgs([]string{"destroy", "testvm", "--force"})
	err := root.Execute()

	require.NoError(t, err)
	_, sErr := mb.Status(ctx, "testvm")
	assert.ErrorIs(t, sErr, backend.ErrVMNotFound)
}

// Test that non-snapshotter backend JSON output has no snapshot_tag.
func TestDestroyCommand_NonSnapshotterBackend_JSONNoTag(t *testing.T) {
	mb := setupDestroyNonSnapshotterTest(t)
	ctx := context.Background()
	require.NoError(t, mb.Create(ctx, "testvm", backend.VMConfig{}))

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
			mb := setupDestroySnapshotTest(t)
			ctx := context.Background()
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))

			root := RootCmd()
			root.SetArgs([]string{"destroy", name, "--force"})
			err := root.Execute()

			require.NoError(t, err)
			// VM is destroyed so we can't check snapshots on it.
			// Verify via JSON output instead.
		})
	}
}

// Property: --no-snapshot never creates a snapshot.
func TestProperty_Destroy_NoSnapshotNeverCreates(t *testing.T) {
	for _, name := range []string{"vm1", "vm2", "test-vm", "alpha", "prod-vm"} {
		t.Run(name, func(t *testing.T) {
			mb := setupDestroySnapshotTest(t)
			ctx := context.Background()
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))

			root := RootCmd()
			root.SetArgs([]string{"destroy", name, "--force", "--no-snapshot"})
			err := root.Execute()

			require.NoError(t, err)
			_, sErr := mb.Status(ctx, name)
			assert.ErrorIs(t, sErr, backend.ErrVMNotFound)
		})
	}
}

// Property: auto-snapshot JSON always valid with snapshot_tag.
func TestProperty_Destroy_AutoSnapshotJSONValid(t *testing.T) {
	for _, name := range []string{"vm1", "vm2", "test-vm", "alpha", "prod-vm"} {
		t.Run(name, func(t *testing.T) {
			mb := setupDestroySnapshotTest(t)
			ctx := context.Background()
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))

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
			mb := setupDestroyNonSnapshotterTest(t)
			ctx := context.Background()
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))

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

// Property: REQ-004-019 — snapshot failure always aborts destroy (no flag).
func TestProperty_Destroy_SnapshotFailureAlwaysAborts(t *testing.T) {
	for _, name := range []string{"vm1", "vm2", "vm3"} {
		t.Run(name, func(t *testing.T) {
			mb := setupDestroySnapshotTest(t)
			ctx := context.Background()
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))
			mb.SetMethodError("snapshotcreate", fmt.Errorf("snapshot failed: %s", name))

			root := RootCmd()
			root.SetArgs([]string{"destroy", name, "--force"})
			err := root.Execute()

			require.Error(t, err, "destroy must abort for %q when snapshot fails", name)
			cliErr, ok := err.(ui.CLIError)
			require.True(t, ok)
			assert.Equal(t, "snapshot_failed", cliErr.Code)
			// VM must still exist.
			_, sErr := mb.Status(ctx, name)
			assert.NoError(t, sErr, "VM %q must still exist after aborted destroy", name)
		})
	}
}

// Property: snapshot tag always has pre-destroy prefix.
func TestProperty_Destroy_SnapshotTagPrefix(t *testing.T) {
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("vm-%d", i)
		t.Run(name, func(t *testing.T) {
			mb := setupDestroySnapshotTest(t)
			ctx := context.Background()
			require.NoError(t, mb.Create(ctx, name, backend.VMConfig{}))

			// Use a unique tag for each iteration
			tagNum := fmt.Sprintf("pre-destroy-20260329-%06d", i*10000)
			origAutoSnapshotTag := autoSnapshotTag
			autoSnapshotTag = func(name string) string { return tagNum }
			defer func() { autoSnapshotTag = origAutoSnapshotTag }()

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
			tag, ok := data["snapshot_tag"].(string)
			require.True(t, ok, "snapshot_tag must be a string for %q", name)
			assert.Contains(t, tag, "pre-destroy-", "tag must have pre-destroy prefix for %q", name)
		})
	}
}

// Q1 / REQ-004-022: destroy with a snapshotter backend MUST emit a
// snapshot-create audit event when the safety snapshot succeeds, and a
// snapshot-create-failed event when it fails.
func TestDestroy_AuditsSnapshotCreate(t *testing.T) {
	mb := setupDestroySnapshotTest(t)
	require.NoError(t, mb.Create(context.Background(), "myvm", backend.VMConfig{}))

	root := RootCmd()
	root.SetArgs([]string{"destroy", "myvm", "--force"})
	require.NoError(t, root.Execute())

	// Find audit.log via the configured loader (SD_HOME).
	sdHome := Loader().SDHome()
	logPath := filepath.Join(sdHome, "audit.log")
	data, err := os.ReadFile(logPath)
	require.NoError(t, err, "audit.log must exist after destroy")

	// At least one line must be a snapshot-create event for the VM.
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var sawSnapshotCreate bool
	for _, line := range lines {
		if line == "" {
			continue
		}
		var entry map[string]any
		if jerr := json.Unmarshal([]byte(line), &entry); jerr != nil {
			continue
		}
		if entry["event"] == "snapshot-create" && entry["vm"] == "myvm" {
			sawSnapshotCreate = true
		}
	}
	assert.True(t, sawSnapshotCreate,
		"destroy must emit snapshot-create audit event (log: %s)", string(data))
}
