package lima

import (
	"context"
	"fmt"
	"testing"

	"sd/internal/backend"
	"sd/internal/backend/lima/mocklimactl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// setupCloneTest creates a backend with mocklimactl and a pre-existing source VM.
func setupCloneTest(t *testing.T) (backend.Backend, context.Context) {
	t.Helper()
	mocklimactl.Reset()
	t.Cleanup(mocklimactl.Reset)
	MockRun = mocklimactl.MockRun
	b := NewWithExecutor(&mockExecutor{})
	ctx := context.Background()

	// Create a source VM
	cfg := backend.VMConfig{CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04"}
	require.NoError(t, b.Create(ctx, "source-vm", cfg))

	return b, ctx
}

// --- Unit Tests ---

// TestClone_Success clones a stopped VM.
// REQ-003-009
func TestClone_Success(t *testing.T) {
	b, ctx := setupCloneTest(t)

	err := b.(backend.Cloner).Clone(ctx, "source-vm", "cloned-vm")
	require.NoError(t, err)

	// Cloned VM should exist and be stopped
	status, err := b.Status(ctx, "cloned-vm")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusStopped, status)

	// Source VM should still exist
	status, err = b.Status(ctx, "source-vm")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusStopped, status)
}

// TestClone_CloneHasIndependentLifecycle verifies cloned VM lifecycle is independent.
// REQ-003-009: The cloned VM has an independent lifecycle from the source.
func TestClone_CloneHasIndependentLifecycle(t *testing.T) {
	b, ctx := setupCloneTest(t)

	err := b.(backend.Cloner).Clone(ctx, "source-vm", "cloned-vm")
	require.NoError(t, err)

	// Start the cloned VM
	require.NoError(t, b.Start(ctx, "cloned-vm"))

	// Source should still be stopped
	srcStatus, err := b.Status(ctx, "source-vm")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusStopped, srcStatus)

	// Cloned should be running
	cloneStatus, err := b.Status(ctx, "cloned-vm")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusRunning, cloneStatus)

	// Stop the cloned VM — source unaffected
	require.NoError(t, b.Stop(ctx, "cloned-vm"))
	srcStatus, _ = b.Status(ctx, "source-vm")
	assert.Equal(t, backend.StatusStopped, srcStatus)
}

// TestClone_CloneHasSameConfig verifies cloned VM inherits source config.
// REQ-003-009
func TestClone_CloneHasSameConfig(t *testing.T) {
	b, ctx := setupCloneTest(t)

	err := b.(backend.Cloner).Clone(ctx, "source-vm", "cloned-vm")
	require.NoError(t, err)

	// Get VM info via List
	vms, err := b.List(ctx)
	require.NoError(t, err)

	var srcVM, dstVM *backend.VMInfo
	for i := range vms {
		if vms[i].Name == "source-vm" {
			srcVM = &vms[i]
		}
		if vms[i].Name == "cloned-vm" {
			dstVM = &vms[i]
		}
	}

	require.NotNil(t, srcVM)
	require.NotNil(t, dstVM)
	assert.Equal(t, srcVM.CPUs, dstVM.CPUs)
	assert.Equal(t, srcVM.Memory, dstVM.Memory)
	assert.Equal(t, srcVM.Disk, dstVM.Disk)
}

// TestClone_SourceNotFound tests cloning a nonexistent VM.
// REQ-003-009
func TestClone_SourceNotFound(t *testing.T) {
	b, ctx := setupCloneTest(t)

	err := b.(backend.Cloner).Clone(ctx, "no-such-vm", "clone-vm")
	require.Error(t, err)
	assert.ErrorIs(t, err, backend.ErrVMNotFound)
}

// TestClone_DestinationAlreadyExists tests cloning to an existing VM name.
// REQ-003-009
func TestClone_DestinationAlreadyExists(t *testing.T) {
	b, ctx := setupCloneTest(t)

	err := b.(backend.Cloner).Clone(ctx, "source-vm", "source-vm")
	require.Error(t, err)
	assert.ErrorIs(t, err, backend.ErrVMAlreadyExists)
}

// TestClone_CloneOfClone tests cloning a cloned VM.
// REQ-003-009
func TestClone_CloneOfClone(t *testing.T) {
	b, ctx := setupCloneTest(t)

	require.NoError(t, b.(backend.Cloner).Clone(ctx, "source-vm", "clone-1"))
	require.NoError(t, b.(backend.Cloner).Clone(ctx, "clone-1", "clone-2"))

	status, err := b.Status(ctx, "clone-2")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusStopped, status)
}

// TestClone_ManyClones tests multiple clones from same source.
// REQ-003-009
func TestClone_ManyClones(t *testing.T) {
	b, ctx := setupCloneTest(t)

	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("clone-%d", i)
		err := b.(backend.Cloner).Clone(ctx, "source-vm", name)
		require.NoError(t, err, "clone %s failed", name)
	}

	vms, err := b.List(ctx)
	require.NoError(t, err)
	assert.Len(t, vms, 6) // source + 5 clones
}

// TestClone_EmptyNames tests clone with empty source/dest names.
// REQ-003-009
func TestClone_EmptyNames(t *testing.T) {
	b, ctx := setupCloneTest(t)

	err := b.(backend.Cloner).Clone(ctx, "", "dest")
	require.Error(t, err)

	err = b.(backend.Cloner).Clone(ctx, "source-vm", "")
	require.Error(t, err)
}

// TestClone_ContextCancelled tests clone with cancelled context.
// REQ-003-009
func TestClone_ContextCancelled(t *testing.T) {
	b, _ := setupCloneTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := b.(backend.Cloner).Clone(ctx, "source-vm", "cloned-vm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

// TestClone_RunningSource tests cloning a running VM (should succeed, cloning current state).
// REQ-003-009
func TestClone_RunningSource(t *testing.T) {
	b, ctx := setupCloneTest(t)

	// Start the source
	require.NoError(t, b.Start(ctx, "source-vm"))

	// Clone should still work — the clone gets a stopped copy
	err := b.(backend.Cloner).Clone(ctx, "source-vm", "cloned-vm")
	require.NoError(t, err)

	// Clone should be stopped (freshly created)
	cloneStatus, err := b.Status(ctx, "cloned-vm")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusStopped, cloneStatus)

	// Source should still be running
	srcStatus, err := b.Status(ctx, "source-vm")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusRunning, srcStatus)
}

// TestClonerInterface verifies Lima backend implements Cloner.
// REQ-003-009
func TestClonerInterface(t *testing.T) {
	b := New()
	_, ok := b.(backend.Cloner)
	assert.True(t, ok, "Lima backend should implement Cloner interface")
}

// --- Property-Based Tests ---

// TestProperty_CloneAlwaysCreatesIndependentVM tests that cloning always
// produces a VM with independent lifecycle.
// REQ-003-009
func TestProperty_CloneAlwaysCreatesIndependentVM(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		MockRun = mocklimactl.MockRun
		b := NewWithExecutor(&mockExecutor{})
		ctx := context.Background()

		srcName := rapid.StringMatching(`[a-z][a-z0-9-]{2,8}`).Draw(t, "srcName")
		dstName := rapid.StringMatching(`[a-z][a-z0-9-]{2,8}`).Draw(t, "dstName")

		// Skip if names collide
		if srcName == dstName {
			return
		}

		cfg := backend.VMConfig{CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04"}
		if err := b.Create(ctx, srcName, cfg); err != nil {
			return // name may already exist from previous iteration with same rapid seed
		}

		err := b.(backend.Cloner).Clone(ctx, srcName, dstName)
		if err != nil {
			return // dst may already exist
		}

		// Start clone — source must remain stopped
		_ = b.Start(ctx, dstName)
		srcStatus, _ := b.Status(ctx, srcName)
		assert.Equal(t, backend.StatusStopped, srcStatus, "source should remain stopped after clone starts")

		// Destroy source — clone must still be queryable
		_ = b.Stop(ctx, dstName)
		_ = b.Destroy(ctx, srcName)
		cloneStatus, err := b.Status(ctx, dstName)
		assert.NoError(t, err, "clone should be queryable after source destroyed")
		assert.Equal(t, backend.StatusStopped, cloneStatus)
	})
}

// TestProperty_CloneErrorOnNonexistentSource tests that cloning a nonexistent VM
// always returns ErrVMNotFound.
// REQ-003-009
func TestProperty_CloneErrorOnNonexistentSource(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		MockRun = mocklimactl.MockRun
		b := NewWithExecutor(&mockExecutor{})
		ctx := context.Background()

		name := rapid.StringMatching(`[a-z][a-z0-9-]{2,8}`).Draw(t, "name")
		err := b.(backend.Cloner).Clone(ctx, name, "dest")
		require.Error(t, err)
		assert.ErrorIs(t, err, backend.ErrVMNotFound)
	})
}

// TestProperty_CloneErrorOnDuplicateDest tests that cloning to an existing name
// always returns ErrVMAlreadyExists.
// REQ-003-009
func TestProperty_CloneErrorOnDuplicateDest(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		MockRun = mocklimactl.MockRun
		b := NewWithExecutor(&mockExecutor{})
		ctx := context.Background()

		name := rapid.StringMatching(`[a-z][a-z0-9-]{2,8}`).Draw(t, "name")
		cfg := backend.VMConfig{CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04"}
		if err := b.Create(ctx, name, cfg); err != nil {
			return
		}

		err := b.(backend.Cloner).Clone(ctx, name, name)
		require.Error(t, err)
		assert.ErrorIs(t, err, backend.ErrVMAlreadyExists)
	})
}

// TestProperty_CloneCancelledContextAlwaysFails tests that clone with cancelled
// context always returns an error containing "cancelled".
// REQ-003-009
func TestProperty_CloneCancelledContextAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		MockRun = mocklimactl.MockRun
		b := NewWithExecutor(&mockExecutor{})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		src := rapid.StringMatching(`[a-z]{3,8}`).Draw(t, "src")
		dst := rapid.StringMatching(`[a-z]{3,8}`).Draw(t, "dst")
		err := b.(backend.Cloner).Clone(ctx, src, dst)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cancelled")
	})
}
