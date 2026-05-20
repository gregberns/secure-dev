// Package cmd provides tests for the snapshot command and its subcommands.
// REQ-002-003: VM Management Commands -- snapshot
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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/backend"
	"sd/internal/ui"
)

// mockSnapshotBackend is a digital twin of a backend with snapshot support.
// It implements backend.Backend and backend.Snapshotter, recording all
// snapshot calls for assertions.
type mockSnapshotBackend struct {
	name      string
	available bool
	snapshots map[string]map[string]backend.SnapshotInfo // vm -> tag -> info
	createErr error
	applyErr  error
	deleteErr error
	listErr   error
	// Recording of calls
	created []snapCall
	applied []snapCall
	deleted []snapCall
}

type snapCall struct {
	vm  string
	tag string
}

func newMockSnapshotBackend() *mockSnapshotBackend {
	return &mockSnapshotBackend{
		name:      "mock-snapshot",
		available: true,
		snapshots: make(map[string]map[string]backend.SnapshotInfo),
	}
}

func (m *mockSnapshotBackend) Name() string { return m.name }
func (m *mockSnapshotBackend) Available() error {
	if m.available {
		return nil
	}
	return fmt.Errorf("backend not available")
}
func (m *mockSnapshotBackend) Create(_ context.Context, name string, _ backend.VMConfig) error {
	return nil
}
func (m *mockSnapshotBackend) Start(_ context.Context, _ string) error   { return nil }
func (m *mockSnapshotBackend) Stop(_ context.Context, _ string) error    { return nil }
func (m *mockSnapshotBackend) Destroy(_ context.Context, _ string) error { return nil }
func (m *mockSnapshotBackend) Status(_ context.Context, name string) (backend.VMStatus, error) {
	if _, ok := m.snapshots[name]; ok {
		return backend.StatusRunning, nil
	}
	return "", backend.ErrVMNotFound
}
func (m *mockSnapshotBackend) List(_ context.Context) ([]backend.VMInfo, error) {
	return nil, nil
}
func (m *mockSnapshotBackend) SSHConfig(_ context.Context, _ string) (backend.SSHConfig, error) {
	return backend.SSHConfig{}, nil
}
func (m *mockSnapshotBackend) Exec(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
	return backend.ExecResult{}, nil
}

// Snapshotter implementation
func (m *mockSnapshotBackend) SnapshotCreate(_ context.Context, vm, tag string) error {
	if m.createErr != nil {
		return m.createErr
	}
	if _, ok := m.snapshots[vm]; !ok {
		return backend.ErrVMNotFound
	}
	if _, exists := m.snapshots[vm][tag]; exists {
		return fmt.Errorf("snapshot %q already exists", tag)
	}
	m.snapshots[vm][tag] = backend.SnapshotInfo{
		Name:      tag,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		Size:      1024,
	}
	m.created = append(m.created, snapCall{vm: vm, tag: tag})
	return nil
}

func (m *mockSnapshotBackend) SnapshotApply(_ context.Context, vm, tag string) error {
	if m.applyErr != nil {
		return m.applyErr
	}
	if _, ok := m.snapshots[vm]; !ok {
		return backend.ErrVMNotFound
	}
	if _, ok := m.snapshots[vm][tag]; !ok {
		return backend.ErrSnapshotNotFound
	}
	m.applied = append(m.applied, snapCall{vm: vm, tag: tag})
	return nil
}

func (m *mockSnapshotBackend) SnapshotDelete(_ context.Context, vm, tag string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if _, ok := m.snapshots[vm]; !ok {
		return backend.ErrVMNotFound
	}
	if _, ok := m.snapshots[vm][tag]; !ok {
		return backend.ErrSnapshotNotFound
	}
	delete(m.snapshots[vm], tag)
	m.deleted = append(m.deleted, snapCall{vm: vm, tag: tag})
	return nil
}

func (m *mockSnapshotBackend) SnapshotList(_ context.Context, vm string) ([]backend.SnapshotInfo, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if _, ok := m.snapshots[vm]; !ok {
		return nil, backend.ErrVMNotFound
	}
	var result []backend.SnapshotInfo
	for _, snap := range m.snapshots[vm] {
		result = append(result, snap)
	}
	return result, nil
}

// mockNoSnapshotterBackend implements Backend but NOT Snapshotter.
type mockNoSnapshotterBackend struct {
	name      string
	available bool
}

func (m *mockNoSnapshotterBackend) Name() string { return m.name }
func (m *mockNoSnapshotterBackend) Available() error {
	if m.available {
		return nil
	}
	return fmt.Errorf("backend not available")
}
func (m *mockNoSnapshotterBackend) Create(_ context.Context, _ string, _ backend.VMConfig) error {
	return nil
}
func (m *mockNoSnapshotterBackend) Start(_ context.Context, _ string) error   { return nil }
func (m *mockNoSnapshotterBackend) Stop(_ context.Context, _ string) error    { return nil }
func (m *mockNoSnapshotterBackend) Destroy(_ context.Context, _ string) error { return nil }
func (m *mockNoSnapshotterBackend) Status(_ context.Context, _ string) (backend.VMStatus, error) {
	return backend.StatusRunning, nil
}
func (m *mockNoSnapshotterBackend) List(_ context.Context) ([]backend.VMInfo, error) {
	return nil, nil
}
func (m *mockNoSnapshotterBackend) SSHConfig(_ context.Context, _ string) (backend.SSHConfig, error) {
	return backend.SSHConfig{}, nil
}
func (m *mockNoSnapshotterBackend) Exec(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
	return backend.ExecResult{}, nil
}

// setupSnapshotTest configures the test environment with a mock snapshot backend.
func setupSnapshotTest(t *testing.T, mb *mockSnapshotBackend) {
	t.Helper()
	newRootTestEnv(t)

	// Reset snapshot subcommand flags to prevent state leaking between tests
	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "snapshot" {
			for _, sub := range cmd.Commands() {
				_ = sub.Flags().Set("tag", "")
				// Reset restore-specific flag if present.
				if f := sub.Flags().Lookup("no-snapshot"); f != nil {
					_ = sub.Flags().Set("no-snapshot", "false")
				}
			}
			break
		}
	}

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })
}

// setupNoSnapshotterTest configures the test environment with a backend
// that does NOT implement Snapshotter.
func setupNoSnapshotterTest(t *testing.T, mb *mockNoSnapshotterBackend) {
	t.Helper()
	newRootTestEnv(t)
	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })
}

// helper to add a VM to the mock backend's snapshot map
func addVMToMock(mb *mockSnapshotBackend, vm string) {
	if mb.snapshots == nil {
		mb.snapshots = make(map[string]map[string]backend.SnapshotInfo)
	}
	if _, ok := mb.snapshots[vm]; !ok {
		mb.snapshots[vm] = make(map[string]backend.SnapshotInfo)
	}
}

// ========== Registration tests ==========

func TestSnapshotCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "snapshot" {
			found = true
			assert.Equal(t, "vm", cmd.GroupID)
			// Verify subcommands
			subNames := make(map[string]bool)
			for _, sub := range cmd.Commands() {
				subNames[sub.Name()] = true
			}
			assert.True(t, subNames["create"], "snapshot must have 'create' subcommand")
			assert.True(t, subNames["list"], "snapshot must have 'list' subcommand")
			assert.True(t, subNames["restore"], "snapshot must have 'restore' subcommand")
			assert.True(t, subNames["delete"], "snapshot must have 'delete' subcommand")
			break
		}
	}
	assert.True(t, found, "snapshot command must be registered")
}

// ========== snapshot create tests ==========

func TestSnapshotCreate_HumanOutput(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	setupSnapshotTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "create", "testvm", "--tag", "v1"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "v1")
	assert.Contains(t, buf.String(), "testvm")
	assert.Contains(t, buf.String(), "created")

	require.Len(t, mb.created, 1)
	assert.Equal(t, "testvm", mb.created[0].vm)
	assert.Equal(t, "v1", mb.created[0].tag)
}

func TestSnapshotCreate_JSONOutput(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	setupSnapshotTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "snapshot", "create", "testvm", "--tag", "v1"})
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
	assert.Equal(t, "testvm", data["vm"])
	assert.Equal(t, "v1", data["tag"])
}

func TestSnapshotCreate_VMNotFound(t *testing.T) {
	mb := newMockSnapshotBackend()
	setupSnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "create", "nonexistent", "--tag", "v1"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
	assert.Contains(t, cliErr.Message, "nonexistent")
}

func TestSnapshotCreate_MissingTag(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	setupSnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "create", "testvm"})
	err := root.Execute()
	require.Error(t, err)
}

func TestSnapshotCreate_BackendError(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	mb.createErr = fmt.Errorf("disk full")
	setupSnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "create", "testvm", "--tag", "v1"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "snapshot_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "disk full")
}

func TestSnapshotCreate_EmptyName(t *testing.T) {
	mb := newMockSnapshotBackend()
	setupSnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "create", "", "--tag", "v1"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
}

func TestSnapshotCreate_BackendUnavailable(t *testing.T) {
	mb := newMockSnapshotBackend()
	mb.available = false
	setupSnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "create", "testvm", "--tag", "v1"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

// ========== snapshot list tests ==========

func TestSnapshotList_HumanOutput(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	// Add a snapshot manually
	mb.snapshots["testvm"]["v1"] = backend.SnapshotInfo{
		Name:      "v1",
		CreatedAt: time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC),
		Size:      2048,
	}
	setupSnapshotTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "list", "testvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "TAG")
	assert.Contains(t, buf.String(), "v1")
	assert.Contains(t, buf.String(), "2048")
}

func TestSnapshotList_JSONOutput(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	mb.snapshots["testvm"]["v1"] = backend.SnapshotInfo{
		Name:      "v1",
		CreatedAt: time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC),
		Size:      2048,
	}
	setupSnapshotTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "snapshot", "list", "testvm"})
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
	data := result["data"].([]any)
	assert.Len(t, data, 1)
	snap := data[0].(map[string]any)
	assert.Equal(t, "v1", snap["name"])
}

func TestSnapshotList_Empty(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	setupSnapshotTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "snapshot", "list", "testvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.True(t, result["ok"].(bool))
	data := result["data"].([]any)
	assert.Empty(t, data)
}

func TestSnapshotList_VMNotFound(t *testing.T) {
	mb := newMockSnapshotBackend()
	setupSnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "list", "nonexistent"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
}

func TestSnapshotList_BackendError(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	mb.listErr = fmt.Errorf("io error")
	setupSnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "list", "testvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "snapshot_failed", cliErr.Code)
}

// ========== snapshot restore tests ==========

func TestSnapshotRestore_HumanOutput(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	mb.snapshots["testvm"]["v1"] = backend.SnapshotInfo{
		Name:      "v1",
		CreatedAt: time.Now().UTC(),
		Size:      1024,
	}
	setupSnapshotTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "restore", "testvm", "--tag", "v1"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "testvm")
	assert.Contains(t, buf.String(), "v1")
	assert.Contains(t, buf.String(), "restored")

	require.Len(t, mb.applied, 1)
	assert.Equal(t, "testvm", mb.applied[0].vm)
	assert.Equal(t, "v1", mb.applied[0].tag)
}

func TestSnapshotRestore_JSONOutput(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	mb.snapshots["testvm"]["v1"] = backend.SnapshotInfo{
		Name:      "v1",
		CreatedAt: time.Now().UTC(),
		Size:      1024,
	}
	setupSnapshotTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "snapshot", "restore", "testvm", "--tag", "v1"})
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
	assert.Equal(t, "testvm", data["vm"])
	assert.Equal(t, "v1", data["tag"])
}

func TestSnapshotRestore_VMNotFound(t *testing.T) {
	mb := newMockSnapshotBackend()
	setupSnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "restore", "nonexistent", "--tag", "v1"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
}

func TestSnapshotRestore_SnapshotNotFound(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	setupSnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "restore", "testvm", "--tag", "ghost"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "snapshot_not_found", cliErr.Code)
	assert.Contains(t, cliErr.Message, "ghost")
}

func TestSnapshotRestore_BackendError(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	mb.snapshots["testvm"]["v1"] = backend.SnapshotInfo{Name: "v1"}
	mb.applyErr = fmt.Errorf("corrupted snapshot")
	setupSnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "restore", "testvm", "--tag", "v1", "--no-snapshot"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "snapshot_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "corrupted snapshot")
}

// withFixedPreRestoreTag installs a deterministic backup-snapshot tag generator
// for the duration of the test. REQ-004-019.
func withFixedPreRestoreTag(t *testing.T, tag string) {
	t.Helper()
	orig := preRestoreSnapshotTag
	preRestoreSnapshotTag = func(string) string { return tag }
	t.Cleanup(func() { preRestoreSnapshotTag = orig })
}

// REQ-004-019: a pre-restore backup snapshot is created by default.
func TestSnapshotRestore_BackupSnapshotCreated(t *testing.T) {
	withFixedPreRestoreTag(t, "pre-restore-20260520-101010")
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	mb.snapshots["testvm"]["v1"] = backend.SnapshotInfo{Name: "v1"}
	setupSnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "restore", "testvm", "--tag", "v1"})
	require.NoError(t, root.Execute())

	// Backup snapshot must have been created before apply.
	require.Len(t, mb.created, 1, "exactly one backup snapshot must be created")
	assert.Equal(t, "testvm", mb.created[0].vm)
	assert.Equal(t, "pre-restore-20260520-101010", mb.created[0].tag)
	require.Len(t, mb.applied, 1)
	assert.Equal(t, "v1", mb.applied[0].tag)
}

// REQ-004-019: --no-snapshot skips the pre-restore snapshot.
func TestSnapshotRestore_BackupSnapshotSkippedWithFlag(t *testing.T) {
	withFixedPreRestoreTag(t, "pre-restore-should-not-appear")
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	mb.snapshots["testvm"]["v1"] = backend.SnapshotInfo{Name: "v1"}
	setupSnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "restore", "testvm", "--tag", "v1", "--no-snapshot"})
	require.NoError(t, root.Execute())

	assert.Len(t, mb.created, 0, "no backup snapshot must be created with --no-snapshot")
	require.Len(t, mb.applied, 1)
}

// REQ-004-019: backup snapshot failure is fatal — restore MUST NOT proceed.
func TestSnapshotRestore_BackupSnapshotFailure_Fatal(t *testing.T) {
	withFixedPreRestoreTag(t, "pre-restore-20260520-101010")
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	mb.snapshots["testvm"]["v1"] = backend.SnapshotInfo{Name: "v1"}
	mb.createErr = fmt.Errorf("disk full")
	setupSnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "restore", "testvm", "--tag", "v1"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "snapshot_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "aborted")
	assert.Contains(t, cliErr.Message, "--no-snapshot")
	// Restore must not have been applied.
	assert.Len(t, mb.applied, 0, "SnapshotApply must NOT be called when backup fails")
}

// REQ-004-019: backup snapshot failure is bypassed when --no-snapshot is set.
func TestSnapshotRestore_BackupSnapshotFailure_SkippedWithFlag(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	mb.snapshots["testvm"]["v1"] = backend.SnapshotInfo{Name: "v1"}
	mb.createErr = fmt.Errorf("should not be called")
	setupSnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "restore", "testvm", "--tag", "v1", "--no-snapshot"})
	require.NoError(t, root.Execute())

	assert.Len(t, mb.created, 0)
	require.Len(t, mb.applied, 1, "restore must proceed when --no-snapshot is passed")
}

// REQ-004-019: backup_tag is included in JSON output.
func TestSnapshotRestore_BackupTag_JSONOutput(t *testing.T) {
	withFixedPreRestoreTag(t, "pre-restore-20260520-101010")
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	mb.snapshots["testvm"]["v1"] = backend.SnapshotInfo{Name: "v1"}
	setupSnapshotTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "snapshot", "restore", "testvm", "--tag", "v1"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	var result map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))

	data := result["data"].(map[string]any)
	assert.Equal(t, "v1", data["tag"])
	assert.Equal(t, "pre-restore-20260520-101010", data["backup_tag"])
}

// ========== snapshot delete tests ==========

func TestSnapshotDelete_HumanOutput(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	mb.snapshots["testvm"]["v1"] = backend.SnapshotInfo{
		Name:      "v1",
		CreatedAt: time.Now().UTC(),
		Size:      1024,
	}
	setupSnapshotTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "delete", "testvm", "--tag", "v1"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "v1")
	assert.Contains(t, buf.String(), "deleted")

	require.Len(t, mb.deleted, 1)
	assert.Equal(t, "testvm", mb.deleted[0].vm)
	assert.Equal(t, "v1", mb.deleted[0].tag)

	// Verify snapshot was actually removed
	_, exists := mb.snapshots["testvm"]["v1"]
	assert.False(t, exists, "snapshot should be removed after delete")
}

func TestSnapshotDelete_JSONOutput(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	mb.snapshots["testvm"]["v1"] = backend.SnapshotInfo{Name: "v1"}
	setupSnapshotTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "snapshot", "delete", "testvm", "--tag", "v1"})
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
	assert.Equal(t, "testvm", data["vm"])
	assert.Equal(t, "v1", data["tag"])
}

func TestSnapshotDelete_VMNotFound(t *testing.T) {
	mb := newMockSnapshotBackend()
	setupSnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "delete", "nonexistent", "--tag", "v1"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
}

func TestSnapshotDelete_SnapshotNotFound(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	setupSnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "delete", "testvm", "--tag", "ghost"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "snapshot_not_found", cliErr.Code)
}

func TestSnapshotDelete_BackendError(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	mb.snapshots["testvm"]["v1"] = backend.SnapshotInfo{Name: "v1"}
	mb.deleteErr = fmt.Errorf("disk error")
	setupSnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "delete", "testvm", "--tag", "v1"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "snapshot_failed", cliErr.Code)
}

// ========== Non-snapshotter backend test ==========

func TestSnapshotCreate_NotSupported(t *testing.T) {
	mb := &mockNoSnapshotterBackend{name: "no-snap", available: true}
	setupNoSnapshotterTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "create", "testvm", "--tag", "v1"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "snapshot_not_supported", cliErr.Code)
}

func TestSnapshotList_NotSupported(t *testing.T) {
	mb := &mockNoSnapshotterBackend{name: "no-snap", available: true}
	setupNoSnapshotterTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "list", "testvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "snapshot_not_supported", cliErr.Code)
}

// ========== formatSnapshotTable unit tests ==========

func TestFormatSnapshotTable_Empty(t *testing.T) {
	output := formatSnapshotTable(nil)
	assert.Equal(t, "No snapshots found.\n", output)
}

func TestFormatSnapshotTable_Multiple(t *testing.T) {
	snaps := []backend.SnapshotInfo{
		{Name: "v1", CreatedAt: time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC), Size: 1024},
		{Name: "v2", CreatedAt: time.Date(2026, 3, 30, 15, 30, 0, 0, time.UTC), Size: 2048},
	}
	output := formatSnapshotTable(snaps)
	assert.Contains(t, output, "TAG")
	assert.Contains(t, output, "v1")
	assert.Contains(t, output, "v2")
	assert.Contains(t, output, "1024")
	assert.Contains(t, output, "2048")
}

// ========== Property-based tests ==========

// Property: JSON output from snapshot create always contains ok=true and required fields.
func TestProperty_SnapshotCreateJSONAlwaysValid(t *testing.T) {
	cases := []struct{ vm, tag string }{
		{"vm-1", "v1"},
		{"my-vm", "baseline"},
		{"test", "snap-001"},
		{"a", "b"},
		{"production", "2024-q4"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%s", tc.vm, tc.tag), func(t *testing.T) {
			mb := newMockSnapshotBackend()
			addVMToMock(mb, tc.vm)
			setupSnapshotTest(t, mb)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"--json", "snapshot", "create", tc.vm, "--tag", tc.tag})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)

			var result map[string]any
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err, "JSON must parse for %s/%s: %s", tc.vm, tc.tag, buf.String())
			assert.True(t, result["ok"].(bool))

			data := result["data"].(map[string]any)
			assert.Equal(t, tc.vm, data["vm"])
			assert.Equal(t, tc.tag, data["tag"])
		})
	}
}

// Property: human output from snapshot create always mentions VM name and tag.
func TestProperty_SnapshotCreateHumanOutputContainsNameAndTag(t *testing.T) {
	cases := []struct{ vm, tag string }{
		{"alpha", "snap-a"},
		{"beta", "snap-b"},
		{"vm-with-dash", "tag-with-dash"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%s", tc.vm, tc.tag), func(t *testing.T) {
			mb := newMockSnapshotBackend()
			addVMToMock(mb, tc.vm)
			setupSnapshotTest(t, mb)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"snapshot", "create", tc.vm, "--tag", tc.tag})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)
			assert.Contains(t, buf.String(), tc.vm)
			assert.Contains(t, buf.String(), tc.tag)
		})
	}
}

// Property: error codes are always snake_case.
func TestProperty_SnapshotErrorCodesSnakeCase(t *testing.T) {
	t.Run("vm_not_found", func(t *testing.T) {
		mb := newMockSnapshotBackend()
		setupSnapshotTest(t, mb)
		root := RootCmd()
		root.SetArgs([]string{"snapshot", "create", "nope", "--tag", "v1"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "vm_not_found", cliErr.Code)
	})

	t.Run("backend_unavailable", func(t *testing.T) {
		mb := newMockSnapshotBackend()
		mb.available = false
		setupSnapshotTest(t, mb)
		root := RootCmd()
		root.SetArgs([]string{"snapshot", "create", "test", "--tag", "v1"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "backend_unavailable", cliErr.Code)
	})

	t.Run("invalid_argument_empty_name", func(t *testing.T) {
		mb := newMockSnapshotBackend()
		setupSnapshotTest(t, mb)
		root := RootCmd()
		root.SetArgs([]string{"snapshot", "create", "", "--tag", "v1"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "invalid_argument", cliErr.Code)
	})

	t.Run("snapshot_not_found", func(t *testing.T) {
		mb := newMockSnapshotBackend()
		addVMToMock(mb, "testvm")
		setupSnapshotTest(t, mb)
		root := RootCmd()
		root.SetArgs([]string{"snapshot", "restore", "testvm", "--tag", "ghost"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "snapshot_not_found", cliErr.Code)
	})

	t.Run("snapshot_not_supported", func(t *testing.T) {
		mb := &mockNoSnapshotterBackend{name: "no-snap", available: true}
		setupNoSnapshotterTest(t, mb)
		root := RootCmd()
		root.SetArgs([]string{"snapshot", "create", "test", "--tag", "v1"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "snapshot_not_supported", cliErr.Code)
	})
}

// Property: snapshot create calls backend exactly once.
func TestProperty_SnapshotCreateCallsBackendOnce(t *testing.T) {
	vms := []string{"vm1", "vm2", "test-vm"}
	for _, vm := range vms {
		t.Run(vm, func(t *testing.T) {
			mb := newMockSnapshotBackend()
			addVMToMock(mb, vm)
			setupSnapshotTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"snapshot", "create", vm, "--tag", "snap"})
			err := root.Execute()
			require.NoError(t, err)
			require.Len(t, mb.created, 1)
			assert.Equal(t, vm, mb.created[0].vm)
		})
	}
}

// Property: snapshot create on nonexistent VM never calls backend successfully.
func TestProperty_SnapshotCreateNonexistentVMNeverSucceeds(t *testing.T) {
	vms := []string{"nope", "ghost", "does-not-exist"}
	for _, vm := range vms {
		t.Run(vm, func(t *testing.T) {
			mb := newMockSnapshotBackend()
			setupSnapshotTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"snapshot", "create", vm, "--tag", "v1"})
			err := root.Execute()
			require.Error(t, err)
			assert.Empty(t, mb.created)
		})
	}
}

// Property: JSON output from snapshot list always contains ok=true and data array.
func TestProperty_SnapshotListJSONAlwaysValid(t *testing.T) {
	cases := []string{"vm-1", "my-vm", "test"}
	for _, vm := range cases {
		t.Run(vm, func(t *testing.T) {
			mb := newMockSnapshotBackend()
			addVMToMock(mb, vm)
			setupSnapshotTest(t, mb)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"--json", "snapshot", "list", vm})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)

			var result map[string]any
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err, "JSON must parse for %s: %s", vm, buf.String())
			assert.True(t, result["ok"].(bool))
			assert.Contains(t, result, "data")
		})
	}
}

// Property: JSON output from snapshot restore always contains required fields.
func TestProperty_SnapshotRestoreJSONRequiredFields(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	mb.snapshots["testvm"]["v1"] = backend.SnapshotInfo{Name: "v1"}
	setupSnapshotTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "snapshot", "restore", "testvm", "--tag", "v1"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	assert.Contains(t, result, "ok")
	assert.Contains(t, result, "data")
	data := result["data"].(map[string]any)
	assert.Contains(t, data, "vm")
	assert.Contains(t, data, "tag")
}

// Property: JSON output from snapshot delete always contains required fields.
func TestProperty_SnapshotDeleteJSONRequiredFields(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	mb.snapshots["testvm"]["v1"] = backend.SnapshotInfo{Name: "v1"}
	setupSnapshotTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "snapshot", "delete", "testvm", "--tag", "v1"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	assert.Contains(t, result, "ok")
	assert.Contains(t, result, "data")
	data := result["data"].(map[string]any)
	assert.Contains(t, data, "vm")
	assert.Contains(t, data, "tag")
}

// Property: snapshot restore on nonexistent VM never calls backend successfully.
func TestProperty_SnapshotRestoreNonexistentVMNeverSucceeds(t *testing.T) {
	vms := []string{"nope", "ghost", "missing"}
	for _, vm := range vms {
		t.Run(vm, func(t *testing.T) {
			mb := newMockSnapshotBackend()
			setupSnapshotTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"snapshot", "restore", vm, "--tag", "v1"})
			err := root.Execute()
			require.Error(t, err)
			assert.Empty(t, mb.applied)
		})
	}
}

// Property: snapshot delete on nonexistent snapshot returns snapshot_not_found.
func TestProperty_SnapshotDeleteNonexistentSnapshotReturnsCode(t *testing.T) {
	tags := []string{"ghost", "old", "nonexistent"}
	for _, tag := range tags {
		t.Run(tag, func(t *testing.T) {
			mb := newMockSnapshotBackend()
			addVMToMock(mb, "testvm")
			setupSnapshotTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"snapshot", "delete", "testvm", "--tag", tag})
			err := root.Execute()
			require.Error(t, err)
			cliErr := err.(ui.CLIError)
			assert.Equal(t, "snapshot_not_found", cliErr.Code)
		})
	}
}

// readAuditEvents reads audit.log under SD_HOME and returns parsed JSONL entries.
func readAuditEvents(t *testing.T) []map[string]any {
	t.Helper()
	sdHome := os.Getenv("SD_HOME")
	if l := Loader(); l != nil && l.SDHome() != "" {
		sdHome = l.SDHome()
	}
	logPath := filepath.Join(sdHome, "audit.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if jerr := json.Unmarshal([]byte(line), &entry); jerr == nil {
			out = append(out, entry)
		}
	}
	return out
}

// Q1 / REQ-004-022: `sd snapshot restore` emits both a snapshot-create
// (backup) and a snapshot-restore audit event.
func TestSnapshotRestore_AuditsCreateAndRestore(t *testing.T) {
	withFixedPreRestoreTag(t, "pre-restore-20260520-101010")
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	mb.snapshots["testvm"]["v1"] = backend.SnapshotInfo{Name: "v1"}
	setupSnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "restore", "testvm", "--tag", "v1"})
	require.NoError(t, root.Execute())

	events := readAuditEvents(t)
	var sawCreate, sawRestore bool
	for _, e := range events {
		if e["event"] == "snapshot-create" && e["vm"] == "testvm" {
			sawCreate = true
		}
		if e["event"] == "snapshot-restore" && e["vm"] == "testvm" {
			sawRestore = true
		}
	}
	assert.True(t, sawCreate, "snapshot restore must emit snapshot-create (backup) audit event")
	assert.True(t, sawRestore, "snapshot restore must emit snapshot-restore audit event")
}

// Q1 / REQ-004-022: standalone `sd snapshot create` MUST emit a
// snapshot-create audit event (was missing in wave 4).
func TestSnapshotCreate_AuditsSnapshotCreate(t *testing.T) {
	mb := newMockSnapshotBackend()
	addVMToMock(mb, "testvm")
	setupSnapshotTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"snapshot", "create", "testvm", "--tag", "manual-tag"})
	require.NoError(t, root.Execute())

	events := readAuditEvents(t)
	var saw bool
	for _, e := range events {
		if e["event"] == "snapshot-create" && e["vm"] == "testvm" {
			if meta, ok := e["meta"].(map[string]any); ok {
				if meta["tag"] == "manual-tag" {
					saw = true
				}
			}
		}
	}
	assert.True(t, saw, "standalone snapshot create must emit snapshot-create audit event with tag")
}
