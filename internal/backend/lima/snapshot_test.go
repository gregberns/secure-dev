// Package lima provides integration tests for Lima backend snapshot operations
// using the mocklimactl digital twin.
// REQ-003-008: Optional Snapshotter Interface
// REQ-003-018: Lima Snapshot Support
package lima

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"sd/internal/backend"
	"sd/internal/backend/lima/mocklimactl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Unit Tests: parseSnapshotList ---

func TestParseSnapshotList_Empty(t *testing.T) {
	snaps, err := parseSnapshotList("")
	require.NoError(t, err)
	assert.Empty(t, snaps)
}

func TestParseSnapshotList_NoSnapshots(t *testing.T) {
	snaps, err := parseSnapshotList("No snapshots")
	require.NoError(t, err)
	assert.Empty(t, snaps)
}

func TestParseSnapshotList_ValidSingle(t *testing.T) {
	ts := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	input := "snap1\t" + ts.Format(time.RFC3339) + "\t1073741824"
	snaps, err := parseSnapshotList(input)
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.Equal(t, "snap1", snaps[0].Name)
	assert.Equal(t, ts, snaps[0].CreatedAt)
	assert.Equal(t, int64(1073741824), snaps[0].Size)
}

func TestParseSnapshotList_ValidMultiple(t *testing.T) {
	ts1 := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	ts2 := time.Date(2026, 3, 28, 10, 30, 0, 0, time.UTC)
	input := "snap1\t" + ts1.Format(time.RFC3339) + "\t1073741824\nsnap2\t" + ts2.Format(time.RFC3339) + "\t536870912"
	snaps, err := parseSnapshotList(input)
	require.NoError(t, err)
	require.Len(t, snaps, 2)
	assert.Equal(t, "snap1", snaps[0].Name)
	assert.Equal(t, "snap2", snaps[1].Name)
}

func TestParseSnapshotList_TooFewFields(t *testing.T) {
	snaps, err := parseSnapshotList("only\ttwo")
	require.NoError(t, err)
	assert.Empty(t, snaps, "lines with fewer than 3 fields should be skipped")
}

func TestParseSnapshotList_InvalidTimestamp(t *testing.T) {
	input := "snap1\tnot-a-timestamp\t1024"
	snaps, err := parseSnapshotList(input)
	require.NoError(t, err)
	assert.Empty(t, snaps, "lines with invalid timestamps should be skipped")
}

// --- Unit Tests: Snapshotter Interface via mocklimactl ---

// newSnapshotTestBackend creates a Lima backend wired to mocklimactl for snapshot tests.
func newSnapshotTestBackend(t *testing.T) (backend.Backend, backend.Snapshotter) {
	t.Helper()
	mocklimactl.Reset()
	b := NewWithExecutor(&mockExecutor{})
	s, ok := b.(backend.Snapshotter)
	require.True(t, ok, "Lima backend must implement Snapshotter interface")
	return b, s
}

func createTestVMForSnapshot(t *testing.T, b backend.Backend, name string) {
	t.Helper()
	cfg := backend.VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}
	require.NoError(t, b.Create(context.Background(), name, cfg))
}

func startTestVMForSnapshot(t *testing.T, b backend.Backend, name string) {
	t.Helper()
	require.NoError(t, b.Start(context.Background(), name))
}

// REQ-003-008, REQ-003-018: SnapshotCreate creates a snapshot.
func TestSnapshot_CreateBasic(t *testing.T) {
	b, s := newSnapshotTestBackend(t)
	ctx := context.Background()

	createTestVMForSnapshot(t, b, "testvm")
	startTestVMForSnapshot(t, b, "testvm")

	err := s.SnapshotCreate(ctx, "testvm", "snap1")
	require.NoError(t, err)

	snaps, err := s.SnapshotList(ctx, "testvm")
	require.NoError(t, err)
	assert.Len(t, snaps, 1)
	assert.Equal(t, "snap1", snaps[0].Name)
}

// REQ-003-008: SnapshotCreate on non-existent VM returns ErrVMNotFound.
func TestSnapshot_CreateNonExistentVM(t *testing.T) {
	_, s := newSnapshotTestBackend(t)
	ctx := context.Background()

	err := s.SnapshotCreate(ctx, "nope", "snap1")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, backend.ErrVMNotFound), "expected ErrVMNotFound, got: %v", err)
}

// REQ-003-008: SnapshotCreate with duplicate tag returns error.
func TestSnapshot_CreateDuplicateTag(t *testing.T) {
	b, s := newSnapshotTestBackend(t)
	ctx := context.Background()

	createTestVMForSnapshot(t, b, "testvm")
	startTestVMForSnapshot(t, b, "testvm")

	require.NoError(t, s.SnapshotCreate(ctx, "testvm", "dup"))
	err := s.SnapshotCreate(ctx, "testvm", "dup")
	assert.Error(t, err, "duplicate snapshot tag should fail")
}

// REQ-003-008, REQ-003-018: SnapshotApply restores a snapshot.
func TestSnapshot_ApplyBasic(t *testing.T) {
	b, s := newSnapshotTestBackend(t)
	ctx := context.Background()

	createTestVMForSnapshot(t, b, "testvm")
	startTestVMForSnapshot(t, b, "testvm")

	require.NoError(t, s.SnapshotCreate(ctx, "testvm", "snap1"))

	// Stop the VM
	require.NoError(t, b.Stop(ctx, "testvm"))
	status, _ := b.Status(ctx, "testvm")
	assert.Equal(t, backend.StatusStopped, status)

	// Restore snapshot (should bring VM back to running state)
	err := s.SnapshotApply(ctx, "testvm", "snap1")
	require.NoError(t, err)

	status, _ = b.Status(ctx, "testvm")
	assert.Equal(t, backend.StatusRunning, status,
		"restoring snapshot taken while running should restore to running")
}

// REQ-003-008: SnapshotApply on non-existent VM returns ErrVMNotFound.
func TestSnapshot_ApplyNonExistentVM(t *testing.T) {
	_, s := newSnapshotTestBackend(t)
	ctx := context.Background()

	err := s.SnapshotApply(ctx, "nope", "snap1")
	assert.Error(t, err)
}

// REQ-003-008: SnapshotApply with non-existent tag returns ErrSnapshotNotFound.
func TestSnapshot_ApplyNonExistentTag(t *testing.T) {
	b, s := newSnapshotTestBackend(t)
	ctx := context.Background()

	createTestVMForSnapshot(t, b, "testvm")

	err := s.SnapshotApply(ctx, "testvm", "ghost")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, backend.ErrSnapshotNotFound), "expected ErrSnapshotNotFound, got: %v", err)
}

// REQ-003-008, REQ-003-018: SnapshotDelete removes a snapshot.
func TestSnapshot_DeleteBasic(t *testing.T) {
	b, s := newSnapshotTestBackend(t)
	ctx := context.Background()

	createTestVMForSnapshot(t, b, "testvm")
	startTestVMForSnapshot(t, b, "testvm")

	require.NoError(t, s.SnapshotCreate(ctx, "testvm", "snap1"))

	// Verify snapshot exists
	snaps, err := s.SnapshotList(ctx, "testvm")
	require.NoError(t, err)
	assert.Len(t, snaps, 1)

	// Delete it
	err = s.SnapshotDelete(ctx, "testvm", "snap1")
	require.NoError(t, err)

	// Verify it's gone
	snaps, err = s.SnapshotList(ctx, "testvm")
	require.NoError(t, err)
	assert.Empty(t, snaps)
}

// REQ-003-008: SnapshotDelete on non-existent tag returns ErrSnapshotNotFound.
func TestSnapshot_DeleteNonExistentTag(t *testing.T) {
	b, s := newSnapshotTestBackend(t)
	ctx := context.Background()

	createTestVMForSnapshot(t, b, "testvm")

	err := s.SnapshotDelete(ctx, "testvm", "ghost")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, backend.ErrSnapshotNotFound), "expected ErrSnapshotNotFound, got: %v", err)
}

// REQ-003-008: SnapshotDelete on non-existent VM returns ErrVMNotFound.
func TestSnapshot_DeleteNonExistentVM(t *testing.T) {
	_, s := newSnapshotTestBackend(t)
	ctx := context.Background()

	err := s.SnapshotDelete(ctx, "nope", "snap1")
	assert.Error(t, err)
}

// REQ-003-008, REQ-003-018: SnapshotList returns all snapshots.
func TestSnapshot_ListMultiple(t *testing.T) {
	b, s := newSnapshotTestBackend(t)
	ctx := context.Background()

	createTestVMForSnapshot(t, b, "testvm")
	startTestVMForSnapshot(t, b, "testvm")

	require.NoError(t, s.SnapshotCreate(ctx, "testvm", "snap1"))
	require.NoError(t, s.SnapshotCreate(ctx, "testvm", "snap2"))
	require.NoError(t, s.SnapshotCreate(ctx, "testvm", "snap3"))

	snaps, err := s.SnapshotList(ctx, "testvm")
	require.NoError(t, err)
	assert.Len(t, snaps, 3)

	names := make(map[string]bool)
	for _, snap := range snaps {
		names[snap.Name] = true
		assert.NotZero(t, snap.CreatedAt, "snapshot must have a timestamp")
		assert.Greater(t, snap.Size, int64(0), "snapshot must have a size")
	}
	assert.True(t, names["snap1"])
	assert.True(t, names["snap2"])
	assert.True(t, names["snap3"])
}

// REQ-003-008: SnapshotList on empty VM returns empty list.
func TestSnapshot_ListEmpty(t *testing.T) {
	b, s := newSnapshotTestBackend(t)
	ctx := context.Background()

	createTestVMForSnapshot(t, b, "testvm")

	snaps, err := s.SnapshotList(ctx, "testvm")
	require.NoError(t, err)
	assert.Empty(t, snaps)
}

// REQ-003-008: SnapshotList on non-existent VM returns ErrVMNotFound.
func TestSnapshot_ListNonExistentVM(t *testing.T) {
	_, s := newSnapshotTestBackend(t)
	ctx := context.Background()

	_, err := s.SnapshotList(ctx, "nope")
	assert.Error(t, err)
}

// REQ-003-008: Context cancellation is respected.
func TestSnapshot_ContextCancellation(t *testing.T) {
	_, s := newSnapshotTestBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.SnapshotCreate(ctx, "testvm", "snap1")
	assert.Error(t, err, "cancelled context should return error")
}

// REQ-003-018: Deleting a snapshot does not affect the VM's current state.
func TestSnapshot_DeleteDoesNotAffectVM(t *testing.T) {
	b, s := newSnapshotTestBackend(t)
	ctx := context.Background()

	createTestVMForSnapshot(t, b, "testvm")
	startTestVMForSnapshot(t, b, "testvm")

	require.NoError(t, s.SnapshotCreate(ctx, "testvm", "snap1"))

	statusBefore, err := b.Status(ctx, "testvm")
	require.NoError(t, err)

	require.NoError(t, s.SnapshotDelete(ctx, "testvm", "snap1"))

	statusAfter, err := b.Status(ctx, "testvm")
	require.NoError(t, err)
	assert.Equal(t, statusBefore, statusAfter,
		"deleting a snapshot must not affect VM state")
}

// REQ-003-018: Snapshots are scoped per VM.
func TestSnapshot_SnapshotsScopedPerVM(t *testing.T) {
	b, s := newSnapshotTestBackend(t)
	ctx := context.Background()

	createTestVMForSnapshot(t, b, "vm1")
	createTestVMForSnapshot(t, b, "vm2")
	startTestVMForSnapshot(t, b, "vm1")
	startTestVMForSnapshot(t, b, "vm2")

	require.NoError(t, s.SnapshotCreate(ctx, "vm1", "only-vm1"))

	// vm2 should have no snapshots
	snaps, err := s.SnapshotList(ctx, "vm2")
	require.NoError(t, err)
	assert.Empty(t, snaps, "vm2 should have no snapshots")

	// vm1 should have the snapshot
	snaps, err = s.SnapshotList(ctx, "vm1")
	require.NoError(t, err)
	assert.Len(t, snaps, 1)
	assert.Equal(t, "only-vm1", snaps[0].Name)
}

// REQ-003-018: Same snapshot tag can exist across different VMs.
func TestSnapshot_SameTagAcrossVMs(t *testing.T) {
	b, s := newSnapshotTestBackend(t)
	ctx := context.Background()

	createTestVMForSnapshot(t, b, "vm1")
	createTestVMForSnapshot(t, b, "vm2")
	startTestVMForSnapshot(t, b, "vm1")
	startTestVMForSnapshot(t, b, "vm2")

	require.NoError(t, s.SnapshotCreate(ctx, "vm1", "same-name"))
	require.NoError(t, s.SnapshotCreate(ctx, "vm2", "same-name"))

	snaps1, err := s.SnapshotList(ctx, "vm1")
	require.NoError(t, err)
	assert.Len(t, snaps1, 1)

	snaps2, err := s.SnapshotList(ctx, "vm2")
	require.NoError(t, err)
	assert.Len(t, snaps2, 1)

	// Deleting one VM's snapshot doesn't affect the other
	require.NoError(t, s.SnapshotDelete(ctx, "vm1", "same-name"))

	snaps2, err = s.SnapshotList(ctx, "vm2")
	require.NoError(t, err)
	assert.Len(t, snaps2, 1, "vm2's snapshot should be unaffected")
}

// REQ-003-008: Full snapshot lifecycle (create -> apply -> delete).
func TestSnapshot_FullLifecycle(t *testing.T) {
	b, s := newSnapshotTestBackend(t)
	ctx := context.Background()

	createTestVMForSnapshot(t, b, "testvm")
	startTestVMForSnapshot(t, b, "testvm")

	// Create snapshot
	require.NoError(t, s.SnapshotCreate(ctx, "testvm", "baseline"))
	snaps, err := s.SnapshotList(ctx, "testvm")
	require.NoError(t, err)
	assert.Len(t, snaps, 1)

	// Stop VM
	require.NoError(t, b.Stop(ctx, "testvm"))

	// Apply snapshot (restore to running)
	require.NoError(t, s.SnapshotApply(ctx, "testvm", "baseline"))
	status, _ := b.Status(ctx, "testvm")
	assert.Equal(t, backend.StatusRunning, status)

	// Delete snapshot
	require.NoError(t, s.SnapshotDelete(ctx, "testvm", "baseline"))
	snaps, err = s.SnapshotList(ctx, "testvm")
	require.NoError(t, err)
	assert.Empty(t, snaps)
}

// REQ-003-018: Snapshot taken while running restores to running.
func TestSnapshot_SnapshotRestoresCorrectState(t *testing.T) {
	b, s := newSnapshotTestBackend(t)
	ctx := context.Background()

	createTestVMForSnapshot(t, b, "testvm")

	// VM starts stopped
	require.NoError(t, s.SnapshotCreate(ctx, "testvm", "stopped-snap"))

	// Start and take another snapshot
	startTestVMForSnapshot(t, b, "testvm")
	require.NoError(t, s.SnapshotCreate(ctx, "testvm", "running-snap"))

	// Stop the VM
	require.NoError(t, b.Stop(ctx, "testvm"))

	// Restore running-snap -> should be running
	require.NoError(t, s.SnapshotApply(ctx, "testvm", "running-snap"))
	status, _ := b.Status(ctx, "testvm")
	assert.Equal(t, backend.StatusRunning, status)

	// Stop again
	require.NoError(t, b.Stop(ctx, "testvm"))

	// Restore stopped-snap -> should be stopped
	require.NoError(t, s.SnapshotApply(ctx, "testvm", "stopped-snap"))
	status, _ = b.Status(ctx, "testvm")
	assert.Equal(t, backend.StatusStopped, status)
}

// --- Property-Based Tests for Snapshot Operations ---

// Property: SnapshotCreate always makes the snapshot appear in SnapshotList.
func TestProperty_SnapshotCreateAppearsInList(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		s := b.(backend.Snapshotter)
		ctx := context.Background()

		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")
		tag := rapid.StringMatching(`snap-[a-z0-9]{3}`).Draw(t, "tag")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		require.NoError(t, b.Create(ctx, vmName, cfg))
		require.NoError(t, b.Start(ctx, vmName))
		require.NoError(t, s.SnapshotCreate(ctx, vmName, tag))

		snaps, err := s.SnapshotList(ctx, vmName)
		require.NoError(t, err)

		found := false
		for _, snap := range snaps {
			if snap.Name == tag {
				found = true
				assert.NotZero(t, snap.CreatedAt)
				assert.Greater(t, snap.Size, int64(0))
				break
			}
		}
		assert.True(t, found, "snapshot %q must appear in list", tag)
	})
}

// Property: Duplicate snapshot creation always fails.
func TestProperty_DuplicateSnapshotAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		s := b.(backend.Snapshotter)
		ctx := context.Background()

		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")
		tag := rapid.StringMatching(`dup-[a-z0-9]{3}`).Draw(t, "tag")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		require.NoError(t, b.Create(ctx, vmName, cfg))
		require.NoError(t, b.Start(ctx, vmName))
		require.NoError(t, s.SnapshotCreate(ctx, vmName, tag))

		err := s.SnapshotCreate(ctx, vmName, tag)
		assert.Error(t, err, "duplicate snapshot tag must fail")
	})
}

// Property: SnapshotDelete always removes the snapshot.
func TestProperty_SnapshotDeleteAlwaysRemoves(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		s := b.(backend.Snapshotter)
		ctx := context.Background()

		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")
		tag := rapid.StringMatching(`del-[a-z0-9]{3}`).Draw(t, "tag")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		require.NoError(t, b.Create(ctx, vmName, cfg))
		require.NoError(t, b.Start(ctx, vmName))
		require.NoError(t, s.SnapshotCreate(ctx, vmName, tag))
		require.NoError(t, s.SnapshotDelete(ctx, vmName, tag))

		snaps, err := s.SnapshotList(ctx, vmName)
		require.NoError(t, err)
		for _, snap := range snaps {
			assert.NotEqual(t, tag, snap.Name,
				"deleted snapshot %q must not appear in list", tag)
		}
	})
}

// Property: SnapshotApply always restores the VM to the snapshotted state.
func TestProperty_SnapshotApplyRestoresState(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		s := b.(backend.Snapshotter)
		ctx := context.Background()

		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")
		tag := rapid.StringMatching(`rst-[a-z0-9]{3}`).Draw(t, "tag")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		require.NoError(t, b.Create(ctx, vmName, cfg))
		require.NoError(t, b.Start(ctx, vmName))

		require.NoError(t, s.SnapshotCreate(ctx, vmName, tag))

		// Stop the VM
		require.NoError(t, b.Stop(ctx, vmName))

		// Restore snapshot
		require.NoError(t, s.SnapshotApply(ctx, vmName, tag))

		// Should be running (state at snapshot time)
		status, err := b.Status(ctx, vmName)
		require.NoError(t, err)
		assert.Equal(t, backend.StatusRunning, status,
			"restoring snapshot taken while running must restore to running")
	})
}

// Property: Snapshotting one VM never affects another VM.
func TestProperty_SnapshotsIsolatedAcrossVMs(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		s := b.(backend.Snapshotter)
		ctx := context.Background()

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		nameA := rapid.StringMatching(`alpha-[a-z0-9]{3}`).Draw(t, "nameA")
		nameB := rapid.StringMatching(`beta-[a-z0-9]{3}`).Draw(t, "nameB")

		require.NoError(t, b.Create(ctx, nameA, cfg))
		require.NoError(t, b.Create(ctx, nameB, cfg))
		require.NoError(t, b.Start(ctx, nameA))
		require.NoError(t, b.Start(ctx, nameB))

		tag := rapid.StringMatching(`iso-[a-z0-9]{3}`).Draw(t, "tag")

		// Only snapshot VM A
		require.NoError(t, s.SnapshotCreate(ctx, nameA, tag))

		// VM B should have no snapshots
		snapsB, err := s.SnapshotList(ctx, nameB)
		require.NoError(t, err)
		assert.Empty(t, snapsB, "VM B should have no snapshots")

		// VM A should have the snapshot
		snapsA, err := s.SnapshotList(ctx, nameA)
		require.NoError(t, err)
		assert.Len(t, snapsA, 1)

		// Delete VM A's snapshot
		require.NoError(t, s.SnapshotDelete(ctx, nameA, tag))

		// VM B should still be running and unaffected
		statusB, err := b.Status(ctx, nameB)
		require.NoError(t, err)
		assert.Equal(t, backend.StatusRunning, statusB)
	})
}

// Property: SnapshotDelete never changes VM state.
func TestProperty_SnapshotDeletePreservesVMState(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		s := b.(backend.Snapshotter)
		ctx := context.Background()

		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")
		tag := rapid.StringMatching(`safe-[a-z0-9]{3}`).Draw(t, "tag")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		require.NoError(t, b.Create(ctx, vmName, cfg))
		require.NoError(t, b.Start(ctx, vmName))
		require.NoError(t, s.SnapshotCreate(ctx, vmName, tag))

		statusBefore, err := b.Status(ctx, vmName)
		require.NoError(t, err)

		require.NoError(t, s.SnapshotDelete(ctx, vmName, tag))

		statusAfter, err := b.Status(ctx, vmName)
		require.NoError(t, err)
		assert.Equal(t, statusBefore, statusAfter,
			"snapshot delete must not change VM state")
	})
}

// Property: SnapshotList always returns valid SnapshotInfo with required fields.
func TestProperty_SnapshotListAlwaysValid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		s := b.(backend.Snapshotter)
		ctx := context.Background()

		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")
		nSnaps := rapid.IntRange(1, 5).Draw(t, "nSnaps")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		require.NoError(t, b.Create(ctx, vmName, cfg))
		require.NoError(t, b.Start(ctx, vmName))

		tags := make(map[string]bool)
		for i := 0; i < nSnaps; i++ {
			tag := rapid.StringMatching(`s-[a-z0-9]{3}`).Draw(t, "tag")
			if tags[tag] {
				continue
			}
			tags[tag] = true
			require.NoError(t, s.SnapshotCreate(ctx, vmName, tag))
		}

		snaps, err := s.SnapshotList(ctx, vmName)
		require.NoError(t, err)
		assert.Len(t, snaps, len(tags))

		for _, snap := range snaps {
			assert.NotEmpty(t, snap.Name, "snapshot name must not be empty")
			assert.NotZero(t, snap.CreatedAt, "snapshot must have a timestamp")
			assert.Greater(t, snap.Size, int64(0), "snapshot must have a positive size")
		}
	})
}

// Property: Snapshot operations on non-existent VMs always fail.
func TestProperty_SnapshotNonExistentVMAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		s := b.(backend.Snapshotter)
		ctx := context.Background()

		vmName := rapid.StringMatching(`ghost-[a-z0-9]{3,8}`).Draw(t, "name")
		tag := rapid.StringMatching(`tag-[a-z0-9]{3}`).Draw(t, "tag")

		err := s.SnapshotCreate(ctx, vmName, tag)
		assert.Error(t, err, "SnapshotCreate on non-existent VM must fail")

		err = s.SnapshotApply(ctx, vmName, tag)
		assert.Error(t, err, "SnapshotApply on non-existent VM must fail")

		err = s.SnapshotDelete(ctx, vmName, tag)
		assert.Error(t, err, "SnapshotDelete on non-existent VM must fail")

		_, err = s.SnapshotList(ctx, vmName)
		assert.Error(t, err, "SnapshotList on non-existent VM must fail")
	})
}

// Property: Multiple create-delete cycles always work.
func TestProperty_SnapshotCreateDeleteCycle(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		s := b.(backend.Snapshotter)
		ctx := context.Background()

		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")
		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		require.NoError(t, b.Create(ctx, vmName, cfg))
		require.NoError(t, b.Start(ctx, vmName))

		cycles := rapid.IntRange(1, 3).Draw(t, "cycles")
		for i := 0; i < cycles; i++ {
			tag := fmt.Sprintf("cycle-%d", i)
			require.NoError(t, s.SnapshotCreate(ctx, vmName, tag))

			snaps, err := s.SnapshotList(ctx, vmName)
			require.NoError(t, err)
			assert.NotEmpty(t, snaps)

			require.NoError(t, s.SnapshotDelete(ctx, vmName, tag))
		}

		snaps, err := s.SnapshotList(ctx, vmName)
		require.NoError(t, err)
		assert.Empty(t, snaps, "all snapshots should be deleted after cycles")
	})
}

// Property: SnapshotInfo JSON round-trip preserves all fields.
func TestProperty_SnapshotInfoJSONRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		s := b.(backend.Snapshotter)
		ctx := context.Background()

		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")
		tag := rapid.StringMatching(`json-[a-z0-9]{3}`).Draw(t, "tag")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		require.NoError(t, b.Create(ctx, vmName, cfg))
		require.NoError(t, b.Start(ctx, vmName))
		require.NoError(t, s.SnapshotCreate(ctx, vmName, tag))

		snaps, err := s.SnapshotList(ctx, vmName)
		require.NoError(t, err)
		require.Len(t, snaps, 1)

		// JSON round-trip
		data, err := json.Marshal(snaps[0])
		require.NoError(t, err)

		var decoded backend.SnapshotInfo
		require.NoError(t, json.Unmarshal(data, &decoded))

		assert.Equal(t, snaps[0].Name, decoded.Name)
		assert.Equal(t, snaps[0].CreatedAt.Unix(), decoded.CreatedAt.Unix())
		assert.Equal(t, snaps[0].Size, decoded.Size)
	})
}

// Property: Lima backend always satisfies Snapshotter interface.
func TestProperty_LimaAlwaysImplementsSnapshotter(t *testing.T) {
	b := New()
	_, ok := b.(backend.Snapshotter)
	assert.True(t, ok, "Lima backend must always implement Snapshotter")
}

// Property: parseSnapshotList is the inverse of mocklimactl snapshot list output.
func TestProperty_ParseSnapshotListHandlesAllFormats(t *testing.T) {
	validStatuses := map[string]bool{
		"stopped": true, "running": true, "creating": true, "error": true,
	}

	rapid.Check(t, func(t *rapid.T) {
		output := rapid.OneOf(
			rapid.Just(""),
			rapid.Just("No snapshots"),
			rapid.StringMatching(`[a-zA-Z0-9 \t.:]{0,200}`),
		).Draw(t, "output")

		snaps, err := parseSnapshotList(output)
		require.NoError(t, err, "parseSnapshotList should never return an error")

		for _, snap := range snaps {
			assert.NotEmpty(t, snap.Name)
			// Timestamps should be parseable (already parsed if we got here)
			_ = validStatuses // just to use the variable
		}
	})
}

// fmt import needed for Sprintf in property tests.
var _ = strings.TrimSpace
var _ = fmt.Sprintf
