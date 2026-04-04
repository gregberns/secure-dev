// Package conformance provides a shared test suite that any backend.Backend
// implementation can run to verify it satisfies the Backend interface contract.
// REQ-008-022: Backend Conformance Tests
//
// Usage from a backend test file:
//
//	func TestMyBackendConformance(t *testing.T) {
//	    b := mybackend.New()
//	    conformance.RunAll(t, conformance.ConformanceOpts{
//	        Backend: b,
//	        Timeout: 5 * time.Second,
//	    })
//	}
package conformance

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"sd/internal/backend"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ConformanceOpts configures the conformance test suite for a specific backend.
// REQ-008-022
type ConformanceOpts struct {
	// Backend is the backend implementation under test.
	Backend backend.Backend

	// Timeout is the per-test context timeout. Defaults to 30s if zero.
	Timeout time.Duration

	// Setup is called before each sub-test. May be nil.
	Setup func(t *testing.T)

	// Teardown is registered via t.Cleanup after each sub-test. May be nil.
	Teardown func(t *testing.T)
}

// RunAll runs the full conformance suite against the given backend.
// REQ-008-022
func RunAll(t *testing.T, opts ConformanceOpts) {
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}

	t.Run("Lifecycle", func(t *testing.T) { testLifecycle(t, opts) })
	t.Run("StateTransitions", func(t *testing.T) { testStateTransitions(t, opts) })
	t.Run("ErrorSemantics", func(t *testing.T) { testErrorSemantics(t, opts) })
	t.Run("ListBehavior", func(t *testing.T) { testListBehavior(t, opts) })
	t.Run("ContextCancellation", func(t *testing.T) { testContextCancellation(t, opts) })
	t.Run("SnapshotLifecycle", func(t *testing.T) { testSnapshotLifecycle(t, opts) })
}

// testVMName generates a unique, valid VM name for a test.
// VM names must match ^[a-z][a-z0-9-]{0,62}$.
// We use a hash of the test name to avoid collisions and ensure validity.
func testVMName(t *testing.T, suffix string) string {
	h := sha256.Sum256([]byte(t.Name() + suffix))
	name := fmt.Sprintf("t-%x", h[:6])
	return name
}

// validConfig returns a minimal valid VMConfig for testing.
func validConfig() backend.VMConfig {
	return backend.VMConfig{
		CPUs:      2,
		Memory:    "4GiB",
		Disk:      "50GiB",
		BaseImage: "ubuntu:24.04",
	}
}

// setupTest calls opts.Setup and registers opts.Teardown, returning a context
// with the configured timeout.
func setupTest(t *testing.T, opts ConformanceOpts) context.Context {
	t.Helper()
	if opts.Setup != nil {
		opts.Setup(t)
	}
	if opts.Teardown != nil {
		t.Cleanup(func() { opts.Teardown(t) })
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	t.Cleanup(cancel)
	return ctx
}

// ensureRunning creates a VM and ensures it is in StatusRunning.
// Some backends may leave a VM in StatusStopped after Create; this helper
// starts it if needed.
func ensureRunning(t *testing.T, ctx context.Context, b backend.Backend, name string) {
	t.Helper()
	status, err := b.Status(ctx, name)
	require.NoError(t, err, "Status after Create")
	require.Contains(t,
		[]backend.VMStatus{backend.StatusRunning, backend.StatusStopped},
		status,
		"Status after Create must be Running or Stopped",
	)
	if status == backend.StatusStopped {
		require.NoError(t, b.Start(ctx, name), "Start after Create (was Stopped)")
	}
	status, err = b.Status(ctx, name)
	require.NoError(t, err)
	require.Equal(t, backend.StatusRunning, status, "VM must be Running after ensureRunning")
}

// testLifecycle verifies the full lifecycle:
// Create -> verify running or stopped -> ensure running -> Stop -> verify stopped
// -> Start -> verify running -> Stop -> Destroy -> verify gone.
func testLifecycle(t *testing.T, opts ConformanceOpts) {
	ctx := setupTest(t, opts)
	b := opts.Backend
	name := testVMName(t, "lifecycle")

	// Create
	err := b.Create(ctx, name, validConfig())
	require.NoError(t, err, "Create")

	// Verify running or stopped after Create, then ensure running
	ensureRunning(t, ctx, b, name)

	// Stop
	err = b.Stop(ctx, name)
	require.NoError(t, err, "Stop")
	status, err := b.Status(ctx, name)
	require.NoError(t, err, "Status after Stop")
	assert.Equal(t, backend.StatusStopped, status, "VM must be Stopped after Stop")

	// Start again
	err = b.Start(ctx, name)
	require.NoError(t, err, "Start after Stop")
	status, err = b.Status(ctx, name)
	require.NoError(t, err, "Status after Start")
	assert.Equal(t, backend.StatusRunning, status, "VM must be Running after Start")

	// Stop again
	err = b.Stop(ctx, name)
	require.NoError(t, err, "Stop again")
	status, err = b.Status(ctx, name)
	require.NoError(t, err, "Status after second Stop")
	assert.Equal(t, backend.StatusStopped, status, "VM must be Stopped after second Stop")

	// Destroy
	err = b.Destroy(ctx, name)
	require.NoError(t, err, "Destroy")

	// Verify gone
	_, err = b.Status(ctx, name)
	require.True(t, errors.Is(err, backend.ErrVMNotFound),
		"Status after Destroy must return ErrVMNotFound, got: %v", err)
}

// testStateTransitions verifies error returns for invalid state transitions.
func testStateTransitions(t *testing.T, opts ConformanceOpts) {
	ctx := setupTest(t, opts)
	b := opts.Backend

	nonExistent := testVMName(t, "nonexistent")

	t.Run("StartNonExistent", func(t *testing.T) {
		err := b.Start(ctx, nonExistent)
		require.True(t, errors.Is(err, backend.ErrVMNotFound),
			"Start on non-existent VM must return ErrVMNotFound, got: %v", err)
	})

	t.Run("StopNonExistent", func(t *testing.T) {
		err := b.Stop(ctx, nonExistent)
		require.True(t, errors.Is(err, backend.ErrVMNotFound),
			"Stop on non-existent VM must return ErrVMNotFound, got: %v", err)
	})

	t.Run("DestroyNonExistent", func(t *testing.T) {
		err := b.Destroy(ctx, nonExistent)
		require.True(t, errors.Is(err, backend.ErrVMNotFound),
			"Destroy on non-existent VM must return ErrVMNotFound, got: %v", err)
	})

	t.Run("StatusNonExistent", func(t *testing.T) {
		_, err := b.Status(ctx, nonExistent)
		require.True(t, errors.Is(err, backend.ErrVMNotFound),
			"Status on non-existent VM must return ErrVMNotFound, got: %v", err)
	})

	t.Run("CreateDuplicate", func(t *testing.T) {
		name := testVMName(t, "dup")
		err := b.Create(ctx, name, validConfig())
		require.NoError(t, err, "first Create")
		t.Cleanup(func() { _ = b.Destroy(ctx, name) })

		err = b.Create(ctx, name, validConfig())
		require.True(t, errors.Is(err, backend.ErrVMAlreadyExists),
			"Create duplicate must return ErrVMAlreadyExists, got: %v", err)
	})

	t.Run("StartOnRunning", func(t *testing.T) {
		name := testVMName(t, "startrun")
		err := b.Create(ctx, name, validConfig())
		require.NoError(t, err)
		t.Cleanup(func() { _ = b.Destroy(ctx, name) })
		ensureRunning(t, ctx, b, name)

		// Start on an already running VM should be a no-op (no error)
		err = b.Start(ctx, name)
		assert.NoError(t, err, "Start on running VM must be a no-op")
	})

	t.Run("StopOnStopped", func(t *testing.T) {
		name := testVMName(t, "stopstopped")
		err := b.Create(ctx, name, validConfig())
		require.NoError(t, err)
		t.Cleanup(func() { _ = b.Destroy(ctx, name) })
		ensureRunning(t, ctx, b, name)

		err = b.Stop(ctx, name)
		require.NoError(t, err, "Stop running VM")

		// Stop on an already stopped VM should be a no-op (no error)
		err = b.Stop(ctx, name)
		assert.NoError(t, err, "Stop on stopped VM must be a no-op")
	})

	t.Run("ExecOnStopped", func(t *testing.T) {
		name := testVMName(t, "execstopped")
		err := b.Create(ctx, name, validConfig())
		require.NoError(t, err)
		t.Cleanup(func() { _ = b.Destroy(ctx, name) })
		ensureRunning(t, ctx, b, name)

		err = b.Stop(ctx, name)
		require.NoError(t, err, "Stop")

		_, err = b.Exec(ctx, name, []string{"echo", "hello"})
		require.True(t, errors.Is(err, backend.ErrVMNotRunning),
			"Exec on stopped VM must return ErrVMNotRunning, got: %v", err)
	})

	t.Run("SSHConfigOnStopped", func(t *testing.T) {
		name := testVMName(t, "sshstopped")
		err := b.Create(ctx, name, validConfig())
		require.NoError(t, err)
		t.Cleanup(func() { _ = b.Destroy(ctx, name) })
		ensureRunning(t, ctx, b, name)

		err = b.Stop(ctx, name)
		require.NoError(t, err, "Stop")

		_, err = b.SSHConfig(ctx, name)
		require.True(t, errors.Is(err, backend.ErrVMNotRunning),
			"SSHConfig on stopped VM must return ErrVMNotRunning, got: %v", err)
	})
}

// testErrorSemantics verifies sentinel error usage.
func testErrorSemantics(t *testing.T, opts ConformanceOpts) {
	ctx := setupTest(t, opts)
	b := opts.Backend

	t.Run("InvalidNames", func(t *testing.T) {
		invalidNames := []string{
			"INVALID",  // uppercase
			"123",      // starts with digit
			"-bad",     // starts with hyphen
			"",         // empty
			"a b",      // space
		}
		for _, name := range invalidNames {
			t.Run(name, func(t *testing.T) {
				err := b.Create(ctx, name, validConfig())
				require.Error(t, err, "Create with invalid name %q must fail", name)
				require.True(t, errors.Is(err, backend.ErrInvalidVMName),
					"Create with invalid name %q must return ErrInvalidVMName, got: %v", name, err)
			})
		}
	})

	t.Run("ValidName", func(t *testing.T) {
		name := testVMName(t, "valid")
		err := b.Create(ctx, name, validConfig())
		require.NoError(t, err, "Create with valid name must succeed")
		t.Cleanup(func() { _ = b.Destroy(ctx, name) })
	})

	t.Run("SentinelErrorsDistinguishable", func(t *testing.T) {
		// Verify that sentinel errors are distinct from each other
		sentinels := []error{
			backend.ErrVMNotFound,
			backend.ErrVMAlreadyExists,
			backend.ErrVMNotRunning,
			backend.ErrBackendNotAvailable,
			backend.ErrNotImplemented,
			backend.ErrSnapshotNotFound,
			backend.ErrInvalidConfig,
			backend.ErrInvalidVMName,
		}
		for i, a := range sentinels {
			for j, b := range sentinels {
				if i != j {
					assert.False(t, errors.Is(a, b),
						"sentinel errors %v and %v must not be equal", a, b)
				}
			}
		}
	})
}

// testListBehavior verifies List() contract.
func testListBehavior(t *testing.T, opts ConformanceOpts) {
	ctx := setupTest(t, opts)
	b := opts.Backend

	t.Run("EmptyList", func(t *testing.T) {
		list, err := b.List(ctx)
		require.NoError(t, err, "List on empty backend")
		require.NotNil(t, list, "List must return non-nil slice")
		assert.Len(t, list, 0, "List must be empty")
	})

	t.Run("OneVM", func(t *testing.T) {
		name := testVMName(t, "listone")
		err := b.Create(ctx, name, validConfig())
		require.NoError(t, err)
		t.Cleanup(func() { _ = b.Destroy(ctx, name) })

		list, err := b.List(ctx)
		require.NoError(t, err, "List after Create")
		require.Len(t, list, 1, "List must contain 1 VM")
		assert.Equal(t, name, list[0].Name, "VMInfo.Name must match")
		assert.Equal(t, b.Name(), list[0].Backend, "VMInfo.Backend must match backend Name()")
	})

	t.Run("TwoVMsSorted", func(t *testing.T) {
		// Create two VMs with names that sort predictably.
		// Use "a-" prefix and "b-" prefix for sort order.
		nameA := "a-" + testVMName(t, "two-a")
		nameB := "b-" + testVMName(t, "two-b")
		require.NoError(t, b.Create(ctx, nameB, validConfig()), "Create B first")
		t.Cleanup(func() { _ = b.Destroy(ctx, nameB) })
		require.NoError(t, b.Create(ctx, nameA, validConfig()), "Create A second")
		t.Cleanup(func() { _ = b.Destroy(ctx, nameA) })

		list, err := b.List(ctx)
		require.NoError(t, err, "List after two Creates")
		require.Len(t, list, 2, "List must contain 2 VMs")
		assert.Equal(t, nameA, list[0].Name, "First VM must be alphabetically first")
		assert.Equal(t, nameB, list[1].Name, "Second VM must be alphabetically second")
	})

	t.Run("AfterDestroy", func(t *testing.T) {
		name := testVMName(t, "destroylist")
		require.NoError(t, b.Create(ctx, name, validConfig()))

		list, err := b.List(ctx)
		require.NoError(t, err)
		require.Len(t, list, 1)

		require.NoError(t, b.Destroy(ctx, name))
		list, err = b.List(ctx)
		require.NoError(t, err, "List after Destroy")
		assert.Len(t, list, 0, "List must be empty after Destroy")
	})

	t.Run("BackendNameMatches", func(t *testing.T) {
		name := testVMName(t, "backendname")
		require.NoError(t, b.Create(ctx, name, validConfig()))
		t.Cleanup(func() { _ = b.Destroy(ctx, name) })

		list, err := b.List(ctx)
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, b.Name(), list[0].Backend,
			"VMInfo.Backend must match b.Name()")
	})
}

// testContextCancellation verifies that methods respect context cancellation.
func testContextCancellation(t *testing.T, opts ConformanceOpts) {
	_ = setupTest(t, opts)
	b := opts.Backend

	t.Run("CreateCancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately
		name := testVMName(t, "ctxcreate")
		err := b.Create(ctx, name, validConfig())
		require.Error(t, err, "Create with cancelled context must fail")
		assert.True(t, errors.Is(err, context.Canceled),
			"Create with cancelled context must return context.Canceled, got: %v", err)
	})

	t.Run("StatusCancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		name := testVMName(t, "ctxstatus")
		_, err := b.Status(ctx, name)
		require.Error(t, err, "Status with cancelled context must fail")
		assert.True(t, errors.Is(err, context.Canceled),
			"Status with cancelled context must return context.Canceled, got: %v", err)
	})

	t.Run("ListCancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := b.List(ctx)
		require.Error(t, err, "List with cancelled context must fail")
		assert.True(t, errors.Is(err, context.Canceled),
			"List with cancelled context must return context.Canceled, got: %v", err)
	})

	t.Run("StartCancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		name := testVMName(t, "ctxstart")
		err := b.Start(ctx, name)
		require.Error(t, err, "Start with cancelled context must fail")
		assert.True(t, errors.Is(err, context.Canceled),
			"Start with cancelled context must return context.Canceled, got: %v", err)
	})

	t.Run("StopCancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		name := testVMName(t, "ctxstop")
		err := b.Stop(ctx, name)
		require.Error(t, err, "Stop with cancelled context must fail")
		assert.True(t, errors.Is(err, context.Canceled),
			"Stop with cancelled context must return context.Canceled, got: %v", err)
	})

	t.Run("DestroyCancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		name := testVMName(t, "ctxdestroy")
		err := b.Destroy(ctx, name)
		require.Error(t, err, "Destroy with cancelled context must fail")
		assert.True(t, errors.Is(err, context.Canceled),
			"Destroy with cancelled context must return context.Canceled, got: %v", err)
	})
}

// testSnapshotLifecycle tests the Snapshotter interface if supported.
// Skipped if the backend does not implement backend.Snapshotter.
func testSnapshotLifecycle(t *testing.T, opts ConformanceOpts) {
	ctx := setupTest(t, opts)
	b := opts.Backend

	snapshotter, ok := b.(backend.Snapshotter)
	if !ok {
		t.Skip("backend does not implement Snapshotter")
	}

	t.Run("CreateAndApply", func(t *testing.T) {
		name := testVMName(t, "snap")
		require.NoError(t, b.Create(ctx, name, validConfig()))
		t.Cleanup(func() { _ = b.Destroy(ctx, name) })
		ensureRunning(t, ctx, b, name)

		// Take snapshot while running
		require.NoError(t, snapshotter.SnapshotCreate(ctx, name, "snap1"),
			"SnapshotCreate")

		// Change state: stop the VM
		require.NoError(t, b.Stop(ctx, name), "Stop after snapshot")
		status, err := b.Status(ctx, name)
		require.NoError(t, err)
		assert.Equal(t, backend.StatusStopped, status, "VM must be Stopped")

		// Apply snapshot: restores to Running
		require.NoError(t, snapshotter.SnapshotApply(ctx, name, "snap1"),
			"SnapshotApply")
		status, err = b.Status(ctx, name)
		require.NoError(t, err)
		assert.Equal(t, backend.StatusRunning, status,
			"VM must be restored to Running after SnapshotApply")
	})

	t.Run("SnapshotList", func(t *testing.T) {
		name := testVMName(t, "snaplist")
		require.NoError(t, b.Create(ctx, name, validConfig()))
		t.Cleanup(func() { _ = b.Destroy(ctx, name) })
		ensureRunning(t, ctx, b, name)

		require.NoError(t, snapshotter.SnapshotCreate(ctx, name, "tag-a"))
		require.NoError(t, snapshotter.SnapshotCreate(ctx, name, "tag-b"))

		snaps, err := snapshotter.SnapshotList(ctx, name)
		require.NoError(t, err, "SnapshotList")
		require.Len(t, snaps, 2, "must have 2 snapshots")

		// Verify both tags are present
		names := make([]string, len(snaps))
		for i, s := range snaps {
			names[i] = s.Name
		}
		assert.Contains(t, names, "tag-a")
		assert.Contains(t, names, "tag-b")
	})

	t.Run("SnapshotDelete", func(t *testing.T) {
		name := testVMName(t, "snapdel")
		require.NoError(t, b.Create(ctx, name, validConfig()))
		t.Cleanup(func() { _ = b.Destroy(ctx, name) })
		ensureRunning(t, ctx, b, name)

		require.NoError(t, snapshotter.SnapshotCreate(ctx, name, "del-me"))
		require.NoError(t, snapshotter.SnapshotDelete(ctx, name, "del-me"),
			"SnapshotDelete")

		snaps, err := snapshotter.SnapshotList(ctx, name)
		require.NoError(t, err)
		assert.Len(t, snaps, 0, "snapshot list must be empty after delete")
	})

	t.Run("ApplyUnknownTag", func(t *testing.T) {
		name := testVMName(t, "snapbad")
		require.NoError(t, b.Create(ctx, name, validConfig()))
		t.Cleanup(func() { _ = b.Destroy(ctx, name) })

		err := snapshotter.SnapshotApply(ctx, name, "nonexistent-tag")
		require.Error(t, err)
		assert.True(t, errors.Is(err, backend.ErrSnapshotNotFound),
			"SnapshotApply with unknown tag must return ErrSnapshotNotFound, got: %v", err)
	})
}
