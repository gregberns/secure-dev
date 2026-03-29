package lima

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"sd/internal/backend"
	"sd/internal/backend/lima/mocklimactl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupIntegrationTest creates a Lima backend backed by mocklimactl digital twin.
func setupIntegrationTest(t *testing.T) backend.Backend {
	t.Helper()
	mocklimactl.Reset()
	t.Cleanup(mocklimactl.Reset)
	MockRun = mocklimactl.MockRun
	return NewWithExecutor(&mockExecutor{})
}

// TestIntegration_FullVMLifecycle exercises create -> start -> exec -> stop -> destroy.
// REQ-003-003: Full lifecycle
func TestIntegration_FullVMLifecycle(t *testing.T) {
	b := setupIntegrationTest(t)
	ctx := context.Background()

	cfg := backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04",
	}

	// 1. Create
	require.NoError(t, b.Create(ctx, "life-vm", cfg))

	// 2. Status should be stopped after creation
	status, err := b.Status(ctx, "life-vm")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusStopped, status)

	// 3. Start
	require.NoError(t, b.Start(ctx, "life-vm"))

	// 4. Status should be running
	status, err = b.Status(ctx, "life-vm")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusRunning, status)

	// 5. Exec a command
	result, err := b.Exec(ctx, "life-vm", []string{"echo", "hello"})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "hello")

	// 6. Stop
	require.NoError(t, b.Stop(ctx, "life-vm"))

	// 7. Destroy
	require.NoError(t, b.Destroy(ctx, "life-vm"))

	// 8. Status should return VMNotFound after destroy
	_, err = b.Status(ctx, "life-vm")
	assert.ErrorIs(t, err, backend.ErrVMNotFound)
}

// TestIntegration_StartIdempotent verifies starting an already-running VM is a no-op.
// REQ-003-003: no-op semantics for already-running
func TestIntegration_StartIdempotent(t *testing.T) {
	b := setupIntegrationTest(t)
	ctx := context.Background()

	require.NoError(t, b.Create(ctx, "idem-vm", backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04",
	}))
	require.NoError(t, b.Start(ctx, "idem-vm"))
	require.NoError(t, b.Start(ctx, "idem-vm"), "starting already-running VM should be no-op")

	status, err := b.Status(ctx, "idem-vm")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusRunning, status)
}

// TestIntegration_StopIdempotent verifies stopping an already-stopped VM is a no-op.
// REQ-003-003: no-op semantics for already-stopped
func TestIntegration_StopIdempotent(t *testing.T) {
	b := setupIntegrationTest(t)
	ctx := context.Background()

	require.NoError(t, b.Create(ctx, "idem-stop-vm", backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04",
	}))
	// VM is already stopped — stop should be no-op
	require.NoError(t, b.Stop(ctx, "idem-stop-vm"))
}

// TestIntegration_CreateDuplicateFails verifies duplicate VM name is rejected.
// REQ-003-003: ErrVMAlreadyExists
func TestIntegration_CreateDuplicateFails(t *testing.T) {
	b := setupIntegrationTest(t)
	ctx := context.Background()
	cfg := backend.VMConfig{CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04"}

	require.NoError(t, b.Create(ctx, "dup-vm", cfg))
	assert.ErrorIs(t, b.Create(ctx, "dup-vm", cfg), backend.ErrVMAlreadyExists)
}

// TestIntegration_OperationsOnNonexistentVM verifies all operations on missing VMs.
// REQ-003-003, REQ-003-004, REQ-003-006, REQ-003-007: ErrVMNotFound
func TestIntegration_OperationsOnNonexistentVM(t *testing.T) {
	b := setupIntegrationTest(t)
	ctx := context.Background()

	_, err := b.Status(ctx, "ghost-vm")
	assert.ErrorIs(t, err, backend.ErrVMNotFound)

	assert.ErrorIs(t, b.Start(ctx, "ghost-vm"), backend.ErrVMNotFound)
	assert.ErrorIs(t, b.Stop(ctx, "ghost-vm"), backend.ErrVMNotFound)
	_, err = b.SSHConfig(ctx, "ghost-vm")
	assert.Error(t, err, "SSHConfig on nonexistent VM should fail")
	_, err = b.Exec(ctx, "ghost-vm", []string{"echo"})
	assert.ErrorIs(t, err, backend.ErrVMNotFound)
}

// TestIntegration_ExecOnStoppedVM verifies exec fails on non-running VM.
// REQ-003-007: Command execution requires running VM
func TestIntegration_ExecOnStoppedVM(t *testing.T) {
	b := setupIntegrationTest(t)
	ctx := context.Background()

	require.NoError(t, b.Create(ctx, "stopped-exec-vm", backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04",
	}))
	_, err := b.Exec(ctx, "stopped-exec-vm", []string{"echo", "hi"})
	assert.ErrorIs(t, err, backend.ErrVMNotRunning)
}

// TestIntegration_SSHConfigRequiresRunning verifies SSHConfig fails on stopped VM.
// REQ-003-006: SSH config requires running VM
func TestIntegration_SSHConfigRequiresRunning(t *testing.T) {
	b := setupIntegrationTest(t)
	ctx := context.Background()

	require.NoError(t, b.Create(ctx, "ssh-stopped-vm", backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04",
	}))
	_, err := b.SSHConfig(ctx, "ssh-stopped-vm")
	assert.ErrorIs(t, err, backend.ErrVMNotRunning)
}

// TestIntegration_SSHConfigOnRunningVM verifies SSHConfig returns valid details.
// REQ-003-006, REQ-007-005
func TestIntegration_SSHConfigOnRunningVM(t *testing.T) {
	// Force TCP transport for deterministic assertions
	orig := isVSOCKTransport
	isVSOCKTransport = func() bool { return false }
	defer func() { isVSOCKTransport = orig }()

	b := setupIntegrationTest(t)
	ctx := context.Background()

	require.NoError(t, b.Create(ctx, "ssh-vm", backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04",
	}))
	require.NoError(t, b.Start(ctx, "ssh-vm"))

	sshCfg, err := b.SSHConfig(ctx, "ssh-vm")
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1", sshCfg.Host)
	assert.Equal(t, "dev", sshCfg.User)
	assert.False(t, sshCfg.ForwardAgent)
	assert.Contains(t, sshCfg.IdentityFile, "ssh-vm")
	assert.Contains(t, sshCfg.IdentityFile, "id_ed25519")
	assert.Equal(t, "tcp", sshCfg.Transport)
}

// TestIntegration_ListVMs verifies listing multiple VMs.
// REQ-003-005
func TestIntegration_ListVMs(t *testing.T) {
	b := setupIntegrationTest(t)
	ctx := context.Background()
	cfg := backend.VMConfig{CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04"}

	require.NoError(t, b.Create(ctx, "list-a", cfg))
	require.NoError(t, b.Create(ctx, "list-b", cfg))

	vms, err := b.List(ctx)
	require.NoError(t, err)

	names := make(map[string]bool)
	for _, vm := range vms {
		names[vm.Name] = true
		assert.Equal(t, "lima", vm.Backend)
	}
	assert.True(t, names["list-a"])
	assert.True(t, names["list-b"])
}

// TestIntegration_ListEmpty verifies listing when no VMs exist.
// REQ-003-005
func TestIntegration_ListEmpty(t *testing.T) {
	b := setupIntegrationTest(t)
	ctx := context.Background()

	vms, err := b.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, vms)
}

// TestIntegration_DestroyRunningVM verifies destroy stops before deleting.
// REQ-003-003: Stop before destroy
func TestIntegration_DestroyRunningVM(t *testing.T) {
	b := setupIntegrationTest(t)
	ctx := context.Background()

	require.NoError(t, b.Create(ctx, "destroy-running", backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04",
	}))
	require.NoError(t, b.Start(ctx, "destroy-running"))
	require.NoError(t, b.Destroy(ctx, "destroy-running"))
	_, err := b.Status(ctx, "destroy-running")
	assert.ErrorIs(t, err, backend.ErrVMNotFound)
}

// TestIntegration_InvalidConfigFailsCreate verifies Create rejects bad configs.
// REQ-003-021
func TestIntegration_InvalidConfigFailsCreate(t *testing.T) {
	b := setupIntegrationTest(t)
	ctx := context.Background()

	err := b.Create(ctx, "bad-config", backend.VMConfig{
		CPUs: 0, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04",
	})
	assert.Error(t, err)
}

// TestIntegration_ContextCancellation verifies operations respect context cancellation.
// REQ-003-022
func TestIntegration_ContextCancellation(t *testing.T) {
	b := setupIntegrationTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := b.Create(ctx, "cancelled-vm", backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04",
	})
	assert.Error(t, err)
}

// TestIntegration_ConcurrentVMOperations verifies safety under concurrent access.
func TestIntegration_ConcurrentVMOperations(t *testing.T) {
	b := setupIntegrationTest(t)
	ctx := context.Background()
	cfg := backend.VMConfig{CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04"}

	var wg sync.WaitGroup
	errCh := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("concur-%d", idx)
			if err := b.Create(ctx, name, cfg); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent create failed: %v", err)
	}

	vms, err := b.List(ctx)
	require.NoError(t, err)
	assert.Len(t, vms, 10)
}

// TestIntegration_SnapshotLifecycle verifies snapshot create/list/apply/delete.
// REQ-003-008, REQ-003-018
func TestIntegration_SnapshotLifecycle(t *testing.T) {
	b := setupIntegrationTest(t)
	ctx := context.Background()
	snapshotter := b.(backend.Snapshotter)

	require.NoError(t, b.Create(ctx, "snap-vm", backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04",
	}))
	require.NoError(t, b.Start(ctx, "snap-vm"))

	// Create snapshots
	require.NoError(t, snapshotter.SnapshotCreate(ctx, "snap-vm", "v1"))
	require.NoError(t, snapshotter.SnapshotCreate(ctx, "snap-vm", "v2"))

	// List snapshots
	snaps, err := snapshotter.SnapshotList(ctx, "snap-vm")
	require.NoError(t, err)
	require.Len(t, snaps, 2)

	// Apply snapshot
	require.NoError(t, snapshotter.SnapshotApply(ctx, "snap-vm", "v1"))

	// Delete snapshot
	require.NoError(t, snapshotter.SnapshotDelete(ctx, "snap-vm", "v1"))
	snaps, err = snapshotter.SnapshotList(ctx, "snap-vm")
	require.NoError(t, err)
	assert.Len(t, snaps, 1)
	assert.Equal(t, "v2", snaps[0].Name)
}

// TestIntegration_SnapshotNotFound verifies snapshot errors.
// REQ-003-008
func TestIntegration_SnapshotNotFound(t *testing.T) {
	b := setupIntegrationTest(t)
	ctx := context.Background()
	snapshotter := b.(backend.Snapshotter)

	require.NoError(t, b.Create(ctx, "snap-err-vm", backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04",
	}))

	assert.ErrorIs(t, snapshotter.SnapshotApply(ctx, "snap-err-vm", "nonexistent"), backend.ErrSnapshotNotFound)
	assert.ErrorIs(t, snapshotter.SnapshotDelete(ctx, "snap-err-vm", "nonexistent"), backend.ErrSnapshotNotFound)
}

// TestIntegration_SnapshotOnNonexistentVM verifies snapshot ops on missing VM.
// REQ-003-008
func TestIntegration_SnapshotOnNonexistentVM(t *testing.T) {
	b := setupIntegrationTest(t)
	ctx := context.Background()
	snapshotter := b.(backend.Snapshotter)

	assert.ErrorIs(t, snapshotter.SnapshotCreate(ctx, "no-vm", "v1"), backend.ErrVMNotFound)
	_, err := snapshotter.SnapshotList(ctx, "no-vm")
	assert.ErrorIs(t, err, backend.ErrVMNotFound)
}
