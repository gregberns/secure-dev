// Package cmd provides tests for the sync command.
// REQ-007-015: Sync To VM
// REQ-007-016: Sync From VM
// REQ-007-017: Sync Diff Preview
//
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

// ---------------------------------------------------------------------------
// Thin Syncer wrapper
// ---------------------------------------------------------------------------

// testSyncerBackend embeds *memory.Backend and adds Syncer interface for testing.
// It delegates all Backend methods to the memory backend and only adds the
// missing Syncer interface. This is NOT a full ad-hoc mock — it is the
// approved approach from the plan for interfaces the memory backend does not
// implement.
type testSyncerBackend struct {
	*memory.Backend

	syncToCalls   []syncCall
	syncFromCalls []syncCall
	syncDiffCalls []syncCall

	syncToErr   error
	syncFromErr error
	syncDiffRes string
	syncDiffErr error
}

type syncCall struct {
	VMName    string
	HostPath  string
	GuestPath string
}

func (s *testSyncerBackend) SyncTo(_ context.Context, name, hostPath, guestPath string) error {
	if s.syncToErr != nil {
		return s.syncToErr
	}
	s.syncToCalls = append(s.syncToCalls, syncCall{VMName: name, HostPath: hostPath, GuestPath: guestPath})
	return nil
}

func (s *testSyncerBackend) SyncFrom(_ context.Context, name, guestPath, hostPath string) error {
	if s.syncFromErr != nil {
		return s.syncFromErr
	}
	s.syncFromCalls = append(s.syncFromCalls, syncCall{VMName: name, HostPath: hostPath, GuestPath: guestPath})
	return nil
}

func (s *testSyncerBackend) SyncDiff(_ context.Context, name, guestPath, hostPath string) (string, error) {
	if s.syncDiffErr != nil {
		return "", s.syncDiffErr
	}
	s.syncDiffCalls = append(s.syncDiffCalls, syncCall{VMName: name, HostPath: hostPath, GuestPath: guestPath})
	return s.syncDiffRes, nil
}

// newSyncerBackend creates a testSyncerBackend with a running VM.
func newSyncerBackend(t *testing.T, vmNames ...string) *testSyncerBackend {
	t.Helper()
	mb := memory.New()
	for _, name := range vmNames {
		require.NoError(t, mb.Create(context.Background(), name, backend.VMConfig{}))
	}
	return &testSyncerBackend{Backend: mb}
}

// ---------------------------------------------------------------------------
// Test Setup
// ---------------------------------------------------------------------------

// setupSyncTest configures the test environment with any backend.Backend.
func setupSyncTest(t *testing.T, b backend.Backend) {
	t.Helper()
	newRootTestEnv(t)

	// Reset subcommand local flags (they persist on singleton commands)
	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "sync" {
			for _, sub := range cmd.Commands() {
				_ = sub.Flags().Set("diff", "false")
				_ = sub.Flags().Set("watch", "false")
			}
		}
	}

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return b, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })
}

// --- Registration tests ---

func TestSyncCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "sync" {
			found = true
			assert.Equal(t, "connection", cmd.GroupID)
			// Verify subcommands
			subNames := make(map[string]bool)
			for _, sub := range cmd.Commands() {
				subNames[sub.Name()] = true
			}
			assert.True(t, subNames["to"], "sync must have 'to' subcommand")
			assert.True(t, subNames["from"], "sync must have 'from' subcommand")
			break
		}
	}
	assert.True(t, found, "sync command must be registered")
}

func TestSyncToCommand_ArgValidation(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"sync", "to"})
	require.NoError(t, err)
	assert.NotNil(t, cmd.Args, "sync to must have an Args validator")

	// One arg must fail (need at least vm + host-path)
	err = cmd.Args(cmd, []string{"myvm"})
	assert.Error(t, err, "sync to must reject one arg")

	// Two args must pass
	err = cmd.Args(cmd, []string{"myvm", "./src"})
	assert.NoError(t, err, "sync to must accept vm + host-path")

	// Three args must pass
	err = cmd.Args(cmd, []string{"myvm", "./src", "/home/dev/src"})
	assert.NoError(t, err, "sync to must accept vm + host-path + guest-path")

	// Four args must fail
	err = cmd.Args(cmd, []string{"myvm", "./src", "/home/dev/src", "extra"})
	assert.Error(t, err, "sync to must reject four args")
}

func TestSyncFromCommand_ArgValidation(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"sync", "from"})
	require.NoError(t, err)
	assert.NotNil(t, cmd.Args, "sync from must have an Args validator")

	// One arg must fail
	err = cmd.Args(cmd, []string{"myvm"})
	assert.Error(t, err, "sync from must reject one arg")

	// Two args must pass
	err = cmd.Args(cmd, []string{"myvm", "/home/dev/src"})
	assert.NoError(t, err, "sync from must accept vm + guest-path")

	// Three args must pass
	err = cmd.Args(cmd, []string{"myvm", "/home/dev/src", "./src"})
	assert.NoError(t, err, "sync from must accept vm + guest-path + host-path")
}

// --- Sync To tests ---

func TestSyncTo_HumanOutput(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	setupSyncTest(t, sb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"sync", "to", "myvm", "./src", "/home/dev/src"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "Synced ./src -> myvm:/home/dev/src")

	require.Len(t, sb.syncToCalls, 1)
	assert.Equal(t, "myvm", sb.syncToCalls[0].VMName)
	assert.Equal(t, "./src", sb.syncToCalls[0].HostPath)
	assert.Equal(t, "/home/dev/src", sb.syncToCalls[0].GuestPath)
}

func TestSyncTo_JSONOutput(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	setupSyncTest(t, sb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "sync", "to", "myvm", "./src", "/home/dev/src"})
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
	assert.Equal(t, "myvm", data["name"])
	assert.Equal(t, "to", data["direction"])
	assert.Equal(t, "./src", data["host_path"])
	assert.Equal(t, "/home/dev/src", data["guest_path"])
}

func TestSyncTo_DefaultGuestPath(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"--json", "sync", "to", "myvm", "./src"})
	execErr := root.Execute()

	require.NoError(t, execErr)
	require.Len(t, sb.syncToCalls, 1)
	assert.Equal(t, "~/src", sb.syncToCalls[0].GuestPath, "default guest path should be ~/<basename>")
}

func TestSyncTo_VMNotFound(t *testing.T) {
	sb := newSyncerBackend(t) // no VMs created
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"sync", "to", "nonexistent", "./src"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
	assert.Contains(t, cliErr.Message, "nonexistent")
}

func TestSyncTo_VMNotRunning(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	require.NoError(t, sb.Stop(context.Background(), "myvm"))
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"sync", "to", "myvm", "./src"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_running", cliErr.Code)
	assert.Contains(t, cliErr.Message, "sd start")
	assert.Empty(t, sb.syncToCalls, "stopped VM must not trigger sync")
}

func TestSyncTo_BackendUnavailable(t *testing.T) {
	sb := newSyncerBackend(t)
	sb.SetMethodError("available", fmt.Errorf("backend not available"))
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"sync", "to", "myvm", "./src"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestSyncTo_NonSyncerBackend(t *testing.T) {
	// Use plain memory.Backend (no Syncer) to test non-Syncer path
	mb := memory.New()
	require.NoError(t, mb.Create(context.Background(), "myvm", backend.VMConfig{}))
	setupSyncTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"sync", "to", "myvm", "./src"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "sync_not_supported", cliErr.Code)
}

func TestSyncTo_SyncError(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	sb.syncToErr = fmt.Errorf("rsync failed: connection refused")
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"sync", "to", "myvm", "./src"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "sync_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "rsync failed")
}

func TestSyncTo_EmptyName(t *testing.T) {
	sb := newSyncerBackend(t)
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"sync", "to", "", "./src"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
}

func TestSyncTo_BackendGetError(t *testing.T) {
	newRootTestEnv(t)

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return nil, fmt.Errorf("no such backend")
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	root := RootCmd()
	root.SetArgs([]string{"sync", "to", "myvm", "./src"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestSyncTo_StatusCheckError(t *testing.T) {
	sb := newSyncerBackend(t)
	sb.SetMethodError("status", fmt.Errorf("connection refused"))
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"sync", "to", "myvm", "./src"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "sync_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "connection refused")
}

func TestSyncTo_SyncErrVMNotRunning(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	sb.syncToErr = backend.ErrVMNotRunning
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"sync", "to", "myvm", "./src"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_running", cliErr.Code)
}

func TestSyncTo_SyncErrVMNotFound(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	sb.syncToErr = backend.ErrVMNotFound
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"sync", "to", "myvm", "./src"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
}

// --- Sync From tests ---

func TestSyncFrom_HumanOutput(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	setupSyncTest(t, sb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"sync", "from", "myvm", "/home/dev/src", "./src"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "Synced myvm:/home/dev/src -> ./src")

	require.Len(t, sb.syncFromCalls, 1)
	assert.Equal(t, "myvm", sb.syncFromCalls[0].VMName)
	assert.Equal(t, "/home/dev/src", sb.syncFromCalls[0].GuestPath)
	assert.Equal(t, "./src", sb.syncFromCalls[0].HostPath)
}

func TestSyncFrom_JSONOutput(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	setupSyncTest(t, sb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "sync", "from", "myvm", "/home/dev/src", "./src"})
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
	assert.Equal(t, "myvm", data["name"])
	assert.Equal(t, "from", data["direction"])
	assert.Equal(t, "./src", data["host_path"])
	assert.Equal(t, "/home/dev/src", data["guest_path"])
}

func TestSyncFrom_DefaultHostPath(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"--json", "sync", "from", "myvm", "/home/dev/project"})
	execErr := root.Execute()

	require.NoError(t, execErr)
	require.Len(t, sb.syncFromCalls, 1)
	assert.Equal(t, "./project", sb.syncFromCalls[0].HostPath, "default host path should be ./<basename>")
}

func TestSyncFrom_VMNotFound(t *testing.T) {
	sb := newSyncerBackend(t) // no VMs
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"sync", "from", "nonexistent", "/home/dev/src"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
}

func TestSyncFrom_VMNotRunning(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	require.NoError(t, sb.Stop(context.Background(), "myvm"))
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"sync", "from", "myvm", "/home/dev/src"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_running", cliErr.Code)
	assert.Empty(t, sb.syncFromCalls)
}

func TestSyncFrom_SyncError(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	sb.syncFromErr = fmt.Errorf("rsync failed")
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"sync", "from", "myvm", "/home/dev/src"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "sync_failed", cliErr.Code)
}

func TestSyncFrom_EmptyName(t *testing.T) {
	sb := newSyncerBackend(t)
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"sync", "from", "", "/home/dev/src"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
}

func TestSyncFrom_NonSyncerBackend(t *testing.T) {
	// Use plain memory.Backend (no Syncer) to test non-Syncer path
	mb := memory.New()
	require.NoError(t, mb.Create(context.Background(), "myvm", backend.VMConfig{}))
	setupSyncTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"sync", "from", "myvm", "/home/dev/src"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "sync_not_supported", cliErr.Code)
}

func TestSyncFrom_SyncErrVMNotRunning(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	sb.syncFromErr = backend.ErrVMNotRunning
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"sync", "from", "myvm", "/home/dev/src"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_running", cliErr.Code)
}

func TestSyncFrom_SyncErrVMNotFound(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	sb.syncFromErr = backend.ErrVMNotFound
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"sync", "from", "myvm", "/home/dev/src"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
}

// --- Diff tests ---

func TestSyncFrom_DiffHumanOutput(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	sb.syncDiffRes = "--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-old\n+new\n"
	setupSyncTest(t, sb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"sync", "from", "myvm", "/home/dev/src", "--diff"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "--- a/file.txt")
	assert.Contains(t, buf.String(), "+new")

	require.Len(t, sb.syncDiffCalls, 1)
	assert.Empty(t, sb.syncFromCalls, "diff must not call SyncFrom")
}

func TestSyncFrom_DiffJSONOutput(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	sb.syncDiffRes = "--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-old\n+new\n"
	setupSyncTest(t, sb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "sync", "from", "myvm", "/home/dev/src", "--diff"})
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
	assert.Equal(t, "myvm", data["name"])
	assert.Contains(t, data["diff"], "--- a/file.txt")
	assert.Contains(t, data["host_path"], "./")
	assert.Equal(t, "/home/dev/src", data["guest_path"])
}

func TestSyncFrom_DiffError(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	sb.syncDiffErr = fmt.Errorf("diff failed")
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"sync", "from", "myvm", "/home/dev/src", "--diff"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "sync_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "diff failed")
}

func TestSyncFrom_DiffVMNotFound(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	sb.syncDiffErr = backend.ErrVMNotFound
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"sync", "from", "myvm", "/home/dev/src", "--diff"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
}

// --- Property-based tests ---

// Property: sync to JSON output always valid with required fields.
func TestProperty_SyncToJSONAlwaysValid(t *testing.T) {
	cases := []struct {
		name      string
		vmName    string
		hostPath  string
		guestPath string
	}{
		{"basic", "vm1", "./src", "/home/dev/src"},
		{"with_explicit_guest", "vm2", "./code", "/opt/code"},
		{"default_guest", "vm3", "./project", ""},
		{"deep_path", "vm4", "/Users/dev/project/src", "/home/dev/src"},
		{"relative_path", "vm5", ".", "/home/dev/dotfiles"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sb := newSyncerBackend(t, tc.vmName)
			setupSyncTest(t, sb)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			args := []string{"--json", "sync", "to", tc.vmName, tc.hostPath}
			if tc.guestPath != "" {
				args = append(args, tc.guestPath)
			}

			root := RootCmd()
			root.SetArgs(args)
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
			assert.Equal(t, tc.vmName, data["name"], "name must match for %s", tc.name)
			assert.Equal(t, "to", data["direction"], "direction must be 'to' for %s", tc.name)
			assert.Contains(t, data, "host_path", "must have host_path for %s", tc.name)
			assert.Contains(t, data, "guest_path", "must have guest_path for %s", tc.name)
		})
	}
}

// Property: sync from JSON output always valid with required fields.
func TestProperty_SyncFromJSONAlwaysValid(t *testing.T) {
	cases := []struct {
		name      string
		vmName    string
		guestPath string
		hostPath  string
	}{
		{"basic", "vm1", "/home/dev/src", "./src"},
		{"explicit_host", "vm2", "/opt/code", "./local-code"},
		{"default_host", "vm3", "/home/dev/project", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sb := newSyncerBackend(t, tc.vmName)
			setupSyncTest(t, sb)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			args := []string{"--json", "sync", "from", tc.vmName, tc.guestPath}
			if tc.hostPath != "" {
				args = append(args, tc.hostPath)
			}

			root := RootCmd()
			root.SetArgs(args)
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

			data, ok := result["data"].(map[string]any)
			if !ok {
				t.Fatalf("data is not a map for %s: %v (raw: %s)", tc.name, result["data"], buf.String())
			}
			assert.Equal(t, tc.vmName, data["name"], "name must match for %s", tc.name)
			assert.Equal(t, "from", data["direction"], "direction must be 'from' for %s", tc.name)
			assert.Contains(t, data, "host_path", "must have host_path for %s", tc.name)
			assert.Contains(t, data, "guest_path", "must have guest_path for %s", tc.name)
		})
	}
}

// Property: error codes are always snake_case.
func TestProperty_SyncErrorCodesSnakeCase(t *testing.T) {
	t.Run("vm_not_found", func(t *testing.T) {
		sb := newSyncerBackend(t) // no VMs
		setupSyncTest(t, sb)
		root := RootCmd()
		root.SetArgs([]string{"sync", "to", "ghost", "./src"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "vm_not_found", cliErr.Code)
	})

	t.Run("vm_not_running", func(t *testing.T) {
		sb := newSyncerBackend(t, "vm1")
		require.NoError(t, sb.Stop(context.Background(), "vm1"))
		setupSyncTest(t, sb)
		root := RootCmd()
		root.SetArgs([]string{"sync", "to", "vm1", "./src"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "vm_not_running", cliErr.Code)
	})

	t.Run("backend_unavailable", func(t *testing.T) {
		sb := newSyncerBackend(t)
		sb.SetMethodError("available", fmt.Errorf("backend not available"))
		setupSyncTest(t, sb)
		root := RootCmd()
		root.SetArgs([]string{"sync", "to", "vm1", "./src"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "backend_unavailable", cliErr.Code)
	})

	t.Run("sync_failed", func(t *testing.T) {
		sb := newSyncerBackend(t, "vm1")
		sb.syncToErr = fmt.Errorf("rsync error")
		setupSyncTest(t, sb)
		root := RootCmd()
		root.SetArgs([]string{"sync", "to", "vm1", "./src"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "sync_failed", cliErr.Code)
	})

	t.Run("invalid_argument_empty_name", func(t *testing.T) {
		sb := newSyncerBackend(t)
		setupSyncTest(t, sb)
		root := RootCmd()
		root.SetArgs([]string{"sync", "to", "", "./src"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "invalid_argument", cliErr.Code)
	})

	t.Run("sync_not_supported", func(t *testing.T) {
		// Use plain memory.Backend (no Syncer)
		mb := memory.New()
		require.NoError(t, mb.Create(context.Background(), "vm1", backend.VMConfig{}))
		setupSyncTest(t, mb)
		root := RootCmd()
		root.SetArgs([]string{"sync", "to", "vm1", "./src"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "sync_not_supported", cliErr.Code)
	})
}

// Property: running VM always calls SyncTo exactly once.
func TestProperty_SyncToRunningVM_CallsBackendOnce(t *testing.T) {
	names := []string{"vm1", "vm2", "test-vm"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			sb := newSyncerBackend(t, name)
			setupSyncTest(t, sb)

			root := RootCmd()
			root.SetArgs([]string{"--json", "sync", "to", name, "./src"})
			err := root.Execute()
			require.NoError(t, err)
			require.Len(t, sb.syncToCalls, 1, "sync to must call backend exactly once for %q", name)
			assert.Equal(t, name, sb.syncToCalls[0].VMName)
		})
	}
}

// Property: stopped VM never calls backend SyncTo.
func TestProperty_SyncToStoppedVM_NeverCallsBackend(t *testing.T) {
	statuses := []backend.VMStatus{backend.StatusStopped, backend.StatusCreating, backend.StatusError}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			sb := newSyncerBackend(t, "vm1")
			require.NoError(t, sb.SetStatus("vm1", status))
			setupSyncTest(t, sb)

			root := RootCmd()
			root.SetArgs([]string{"sync", "to", "vm1", "./src"})
			err := root.Execute()
			require.Error(t, err)
			assert.Empty(t, sb.syncToCalls, "sync to on non-running VM must not call SyncTo for status %s", status)
		})
	}
}

// Property: nonexistent VM never calls backend SyncTo.
func TestProperty_SyncToNonexistentVM_NeverCallsBackend(t *testing.T) {
	names := []string{"ghost", "nonexistent", "missing-vm"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			sb := newSyncerBackend(t) // no VMs
			setupSyncTest(t, sb)

			root := RootCmd()
			root.SetArgs([]string{"sync", "to", name, "./src"})
			err := root.Execute()
			require.Error(t, err)
			assert.Empty(t, sb.syncToCalls, "sync to nonexistent VM must not call SyncTo for %q", name)
		})
	}
}

// Property: running VM always calls SyncFrom exactly once.
func TestProperty_SyncFromRunningVM_CallsBackendOnce(t *testing.T) {
	names := []string{"vm1", "vm2", "test-vm"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			sb := newSyncerBackend(t, name)
			setupSyncTest(t, sb)

			root := RootCmd()
			root.SetArgs([]string{"--json", "sync", "from", name, "/home/dev/src"})
			err := root.Execute()
			require.NoError(t, err)
			require.Len(t, sb.syncFromCalls, 1, "sync from must call backend exactly once for %q", name)
			assert.Equal(t, name, sb.syncFromCalls[0].VMName)
		})
	}
}

// Property: diff mode calls SyncDiff but not SyncFrom.
func TestProperty_SyncDiff_CallsDiffNotSync(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	sb.syncDiffRes = "some diff output"
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"--json", "sync", "from", "myvm", "/home/dev/src", "--diff"})
	err := root.Execute()

	require.NoError(t, err)
	require.Len(t, sb.syncDiffCalls, 1, "diff must call SyncDiff exactly once")
	assert.Empty(t, sb.syncFromCalls, "diff must not call SyncFrom")
}

// Property: JSON required fields for sync to.
func TestProperty_SyncToJSONRequiredFields(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	setupSyncTest(t, sb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "sync", "to", "myvm", "./src", "/dst"})
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
	assert.Contains(t, data, "direction", "data must have 'direction' field")
	assert.Contains(t, data, "host_path", "data must have 'host_path' field")
	assert.Contains(t, data, "guest_path", "data must have 'guest_path' field")
}

// Property: diff JSON required fields.
func TestProperty_SyncDiffJSONRequiredFields(t *testing.T) {
	sb := newSyncerBackend(t, "myvm")
	sb.syncDiffRes = "diff output"
	setupSyncTest(t, sb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "sync", "from", "myvm", "/home/dev/src", "--diff"})
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
	assert.Contains(t, data, "name", "data must have 'name'")
	assert.Contains(t, data, "host_path", "data must have 'host_path'")
	assert.Contains(t, data, "guest_path", "data must have 'guest_path'")
	assert.Contains(t, data, "diff", "data must have 'diff'")
}

// Property: error JSON format is consistent.
func TestProperty_SyncErrorJSONFormat(t *testing.T) {
	sb := newSyncerBackend(t) // no VMs
	setupSyncTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"sync", "to", "nonexistent", "./src"})
	execErr := root.Execute()

	require.Error(t, execErr)
	cliErr, ok := execErr.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
	assert.Contains(t, cliErr.Message, "nonexistent")
}
