// Package lima provides integration tests for the Lima backend Syncer interface.
// Uses a digital twin mock for rsyncRun to test sync operations without rsync.
// REQ-003-010: Optional Syncer Interface
// REQ-007-015: Sync To VM
// REQ-007-016: Sync From VM
// REQ-007-017: Sync Diff Preview
package lima

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"sd/internal/backend"
	"sd/internal/backend/lima/mocklimactl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Unit Tests: buildRsyncArgs ---

func TestBuildRsyncArgs_TCPTransport(t *testing.T) {
	cfg := backend.SSHConfig{
		Host:         "127.0.0.1",
		Port:         54321,
		User:         "dev",
		IdentityFile: "/home/user/.ssh/id_ed25519",
		Transport:    "tcp",
	}

	args := buildRsyncArgs(cfg, "/src", "dev@127.0.0.1:/dst", false)

	require.NotEmpty(t, args)
	assert.Equal(t, "-avz", args[0])
	assert.Contains(t, strings.Join(args, " "), "-e")
	assert.Contains(t, strings.Join(args, " "), "ssh -p 54321")
	assert.Contains(t, strings.Join(args, " "), "StrictHostKeyChecking=yes")
	assert.Contains(t, strings.Join(args, " "), "/home/user/.ssh/id_ed25519")
	// Last two args should be src and dst
	assert.Equal(t, "/src", args[len(args)-2])
	assert.Equal(t, "dev@127.0.0.1:/dst", args[len(args)-1])
}

func TestBuildRsyncArgs_VSOCKTransport(t *testing.T) {
	cfg := backend.SSHConfig{
		User:         "dev",
		IdentityFile: "/home/user/.ssh/id_ed25519",
		Transport:    "vsock",
		ProxyCommand: "limactl shell --tty=false test-vm -- ssh -o none",
	}

	args := buildRsyncArgs(cfg, "/src", "dev@localhost:/dst", false)

	argStr := strings.Join(args, " ")
	assert.Contains(t, argStr, "ProxyCommand=")
	assert.Contains(t, argStr, "StrictHostKeyChecking=no")
	assert.Contains(t, argStr, "UserKnownHostsFile=/dev/null")
}

func TestBuildRsyncArgs_DryRun(t *testing.T) {
	cfg := backend.SSHConfig{
		Host:         "127.0.0.1",
		Port:         22,
		User:         "dev",
		IdentityFile: "/home/user/.ssh/key",
		Transport:    "tcp",
	}

	args := buildRsyncArgs(cfg, "/src", "/dst", true)

	argStr := strings.Join(args, " ")
	assert.Contains(t, argStr, "--dry-run")
	assert.Contains(t, argStr, "--itemize-changes")
}

func TestBuildRsyncArgs_NormalRun_NoDryRunFlags(t *testing.T) {
	cfg := backend.SSHConfig{
		Host:         "127.0.0.1",
		Port:         22,
		User:         "dev",
		IdentityFile: "/home/user/.ssh/key",
		Transport:    "tcp",
	}

	args := buildRsyncArgs(cfg, "/src", "/dst", false)

	argStr := strings.Join(args, " ")
	assert.NotContains(t, argStr, "--dry-run")
	assert.NotContains(t, argStr, "--itemize-changes")
}

// --- Unit Tests: sshTarget ---

func TestSshTarget_TCP(t *testing.T) {
	cfg := backend.SSHConfig{
		Host:      "192.168.1.100",
		User:      "dev",
		Transport: "tcp",
	}

	target := sshTarget(cfg, "/home/dev/project")
	assert.Equal(t, "dev@192.168.1.100:/home/dev/project", target)
}

func TestSshTarget_VSOCK(t *testing.T) {
	cfg := backend.SSHConfig{
		User:      "dev",
		Transport: "vsock",
	}

	target := sshTarget(cfg, "/home/dev/project")
	assert.Equal(t, "dev@localhost:/home/dev/project", target)
}

// --- Integration Tests: Syncer through mocklimactl digital twin ---

// setupSyncTest creates a backend, mocklimactl environment, and injects a
// mock rsyncRun that records calls.
func setupSyncTest(t *testing.T) (backend.Backend, *rsyncRecorder) {
	t.Helper()
	mocklimactl.Reset()
	t.Cleanup(mocklimactl.Reset)
	MockRun = mocklimactl.MockRun

	recorder := &rsyncRecorder{}

	// Replace rsyncRun with a mock that records and returns configurable results.
	origRsyncRun := rsyncRun
	rsyncRun = recorder.run
	t.Cleanup(func() { rsyncRun = origRsyncRun })

	// Force TCP for deterministic assertions
	origVSOCK := isVSOCKTransport
	isVSOCKTransport = func() bool { return false }
	t.Cleanup(func() { isVSOCKTransport = origVSOCK })

	return NewWithExecutor(&mockExecutor{}), recorder
}

// rsyncRecorder records rsync invocations for test assertions.
type rsyncRecorder struct {
	calls []rsyncCall
	err   error // if set, all calls return this error
}

type rsyncCall struct {
	cmd  string
	args []string
}

func (r *rsyncRecorder) run(name string, args ...string) (string, error) {
	r.calls = append(r.calls, rsyncCall{cmd: name, args: args})
	if r.err != nil {
		return "", r.err
	}
	return "sent 1K bytes  received 100 bytes\n", nil
}

// createAndStartVM is a helper that creates and starts a VM for sync tests.
func createAndStartVM(t *testing.T, b backend.Backend, name string) {
	t.Helper()
	ctx := context.Background()
	cfg := backend.VMConfig{CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04"}
	require.NoError(t, b.Create(ctx, name, cfg))
	require.NoError(t, b.Start(ctx, name))
}

// TestIntegration_SyncTo_Success verifies SyncTo calls rsync with correct args.
// REQ-007-015
func TestIntegration_SyncTo_Success(t *testing.T) {
	b, rec := setupSyncTest(t)
	createAndStartVM(t, b, "sync-vm")

	syncer := b.(backend.Syncer)
	err := syncer.SyncTo(context.Background(), "sync-vm", "/host/path", "/guest/path")
	require.NoError(t, err)

	require.Len(t, rec.calls, 1)
	assert.Equal(t, "rsync", rec.calls[0].cmd)
	assert.Equal(t, "/host/path", rec.calls[0].args[len(rec.calls[0].args)-2])
	// Destination should contain user@host:/guest/path
	dst := rec.calls[0].args[len(rec.calls[0].args)-1]
	assert.Contains(t, dst, "/guest/path")
	assert.Contains(t, dst, "dev@")
}

// TestIntegration_SyncFrom_Success verifies SyncFrom calls rsync with correct args.
// REQ-007-016
func TestIntegration_SyncFrom_Success(t *testing.T) {
	b, rec := setupSyncTest(t)
	createAndStartVM(t, b, "sync-vm")

	syncer := b.(backend.Syncer)
	err := syncer.SyncFrom(context.Background(), "sync-vm", "/guest/path", "/host/path")
	require.NoError(t, err)

	require.Len(t, rec.calls, 1)
	assert.Equal(t, "rsync", rec.calls[0].cmd)
	// Source should contain user@host:/guest/path
	src := rec.calls[0].args[len(rec.calls[0].args)-2]
	assert.Contains(t, src, "/guest/path")
	assert.Contains(t, src, "dev@")
	assert.Equal(t, "/host/path", rec.calls[0].args[len(rec.calls[0].args)-1])
}

// TestIntegration_SyncDiff_Success verifies SyncDiff returns rsync output.
// REQ-007-017
func TestIntegration_SyncDiff_Success(t *testing.T) {
	b, rec := setupSyncTest(t)
	createAndStartVM(t, b, "sync-vm")

	syncer := b.(backend.Syncer)
	out, err := syncer.SyncDiff(context.Background(), "sync-vm", "/guest/path", "/host/path")
	require.NoError(t, err)
	assert.Contains(t, out, "sent")

	require.Len(t, rec.calls, 1)
	argStr := strings.Join(rec.calls[0].args, " ")
	assert.Contains(t, argStr, "--dry-run")
	assert.Contains(t, argStr, "--itemize-changes")
}

// TestIntegration_SyncTo_StoppedVM verifies SyncTo fails on stopped VM.
// REQ-003-010: SSHConfig requires running VM
func TestIntegration_SyncTo_StoppedVM(t *testing.T) {
	b, _ := setupSyncTest(t)
	ctx := context.Background()
	cfg := backend.VMConfig{CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04"}
	require.NoError(t, b.Create(ctx, "stopped-sync", cfg))
	// Do NOT start the VM

	syncer := b.(backend.Syncer)
	err := syncer.SyncTo(ctx, "stopped-sync", "/host/path", "/guest/path")
	assert.Error(t, err, "SyncTo on stopped VM should fail")
}

// TestIntegration_SyncFrom_NonexistentVM verifies SyncFrom fails on missing VM.
func TestIntegration_SyncFrom_NonexistentVM(t *testing.T) {
	b, _ := setupSyncTest(t)

	syncer := b.(backend.Syncer)
	err := syncer.SyncFrom(context.Background(), "ghost-vm", "/guest/path", "/host/path")
	assert.Error(t, err)
}

// TestIntegration_SyncTo_RsyncError wraps generic rsync failures.
func TestIntegration_SyncTo_RsyncError(t *testing.T) {
	b, rec := setupSyncTest(t)
	createAndStartVM(t, b, "sync-err")
	rec.err = fmt.Errorf("rsync: permission denied")

	syncer := b.(backend.Syncer)
	err := syncer.SyncTo(context.Background(), "sync-err", "/host/path", "/guest/path")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sync-to failed")
}

// TestIntegration_SyncFrom_VMNotRunning_RsyncError maps connection errors to ErrVMNotRunning.
func TestIntegration_SyncFrom_VMNotRunning_RsyncError(t *testing.T) {
	b, rec := setupSyncTest(t)
	createAndStartVM(t, b, "sync-noroute")
	rec.err = fmt.Errorf("rsync: failed: No route to host")

	syncer := b.(backend.Syncer)
	err := syncer.SyncFrom(context.Background(), "sync-noroute", "/guest/path", "/host/path")
	assert.ErrorIs(t, err, backend.ErrVMNotRunning)
}

// TestIntegration_SyncTo_VMNotRunning_NoSuchFile maps file errors to ErrVMNotRunning.
func TestIntegration_SyncTo_VMNotRunning_NoSuchFile(t *testing.T) {
	b, rec := setupSyncTest(t)
	createAndStartVM(t, b, "sync-nofile")
	rec.err = fmt.Errorf("rsync: No such file or directory (2)")

	syncer := b.(backend.Syncer)
	err := syncer.SyncTo(context.Background(), "sync-nofile", "/host/path", "/guest/path")
	assert.ErrorIs(t, err, backend.ErrVMNotRunning)
}

// TestIntegration_SyncDiff_VMNotRunning maps connection errors to ErrVMNotRunning.
func TestIntegration_SyncDiff_VMNotRunning(t *testing.T) {
	b, rec := setupSyncTest(t)
	createAndStartVM(t, b, "sync-diff-nr")
	rec.err = fmt.Errorf("rsync: No route to host")

	syncer := b.(backend.Syncer)
	_, err := syncer.SyncDiff(context.Background(), "sync-diff-nr", "/guest/path", "/host/path")
	assert.ErrorIs(t, err, backend.ErrVMNotRunning)
}

// TestIntegration_SyncDiff_RsyncError wraps generic rsync failures.
func TestIntegration_SyncDiff_RsyncError(t *testing.T) {
	b, rec := setupSyncTest(t)
	createAndStartVM(t, b, "sync-diff-err")
	rec.err = fmt.Errorf("rsync: some error")

	syncer := b.(backend.Syncer)
	_, err := syncer.SyncDiff(context.Background(), "sync-diff-err", "/guest/path", "/host/path")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sync-diff failed")
}

// TestIntegration_Sync_ContextCancelled verifies sync respects context cancellation.
func TestIntegration_Sync_ContextCancelled(t *testing.T) {
	b, _ := setupSyncTest(t)
	createAndStartVM(t, b, "sync-cancel")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	syncer := b.(backend.Syncer)
	err := syncer.SyncTo(ctx, "sync-cancel", "/host/path", "/guest/path")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

// TestIntegration_SyncFrom_ContextCancelled verifies sync-from respects context cancellation.
func TestIntegration_SyncFrom_ContextCancelled(t *testing.T) {
	b, _ := setupSyncTest(t)
	createAndStartVM(t, b, "sync-cancel-from")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	syncer := b.(backend.Syncer)
	err := syncer.SyncFrom(ctx, "sync-cancel-from", "/guest/path", "/host/path")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

// TestIntegration_SyncDiff_ContextCancelled verifies sync-diff respects context cancellation.
func TestIntegration_SyncDiff_ContextCancelled(t *testing.T) {
	b, _ := setupSyncTest(t)
	createAndStartVM(t, b, "sync-cancel-diff")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	syncer := b.(backend.Syncer)
	_, err := syncer.SyncDiff(ctx, "sync-cancel-diff", "/guest/path", "/host/path")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

// TestIntegration_SyncerInterfaceAssertion verifies limaBackend implements Syncer.
func TestIntegration_SyncerInterfaceAssertion(t *testing.T) {
	b, _ := setupSyncTest(t)
	_, ok := b.(backend.Syncer)
	assert.True(t, ok, "limaBackend should implement backend.Syncer")
}

// TestIntegration_SyncTo_VSOCKTransport verifies SyncTo builds correct args for VSOCK.
func TestIntegration_SyncTo_VSOCKTransport(t *testing.T) {
	mocklimactl.Reset()
	t.Cleanup(mocklimactl.Reset)
	MockRun = mocklimactl.MockRun

	rec := &rsyncRecorder{}
	origRsyncRun := rsyncRun
	rsyncRun = rec.run
	t.Cleanup(func() { rsyncRun = origRsyncRun })

	// Force VSOCK transport
	origVSOCK := isVSOCKTransport
	isVSOCKTransport = func() bool { return true }
	t.Cleanup(func() { isVSOCKTransport = origVSOCK })

	b := NewWithExecutor(&mockExecutor{})
	createAndStartVM(t, b, "vsock-sync")

	syncer := b.(backend.Syncer)
	err := syncer.SyncTo(context.Background(), "vsock-sync", "/host/path", "/guest/path")
	require.NoError(t, err)

	require.Len(t, rec.calls, 1)
	argStr := strings.Join(rec.calls[0].args, " ")
	assert.Contains(t, argStr, "ProxyCommand=")
	assert.Contains(t, argStr, "StrictHostKeyChecking=no")
	assert.Contains(t, argStr, "UserKnownHostsFile=/dev/null")
	// Destination for VSOCK should use localhost
	dst := rec.calls[0].args[len(rec.calls[0].args)-1]
	assert.Contains(t, dst, "dev@localhost:")
}

// --- Property-Based Tests ---

// Property: buildRsyncArgs always starts with -avz
func TestProperty_RsyncArgs_AlwaysStartsWithArchive(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cfg := backend.SSHConfig{
			Host:         rapid.SampledFrom([]string{"127.0.0.1", "10.0.0.1", "192.168.1.1"}).Draw(t, "host"),
			Port:         int(rapid.IntRange(1, 65535).Draw(t, "port")),
			User:         rapid.SampledFrom([]string{"dev", "ubuntu", "root"}).Draw(t, "user"),
			IdentityFile: "/home/user/.ssh/id_ed25519",
			Transport:    "tcp",
		}
		dryRun := rapid.Bool().Draw(t, "dryRun")

		args := buildRsyncArgs(cfg, "/src", "/dst", dryRun)

		require.NotEmpty(t, args)
		assert.Equal(t, "-avz", args[0])
	})
}

// Property: dry-run always includes both --dry-run and --itemize-changes
func TestProperty_RsyncArgs_DryRunFlags(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		port := int(rapid.IntRange(1, 65535).Draw(t, "port"))
		cfg := backend.SSHConfig{
			Host:         "127.0.0.1",
			Port:         port,
			User:         "dev",
			IdentityFile: "/home/user/.ssh/key",
			Transport:    "tcp",
		}

		args := buildRsyncArgs(cfg, "/src", "/dst", true)
		argStr := strings.Join(args, " ")
		assert.Contains(t, argStr, "--dry-run")
		assert.Contains(t, argStr, "--itemize-changes")
	})
}

// Property: non-dry-run never includes dry-run flags
func TestProperty_RsyncArgs_NormalRun_NoDryRunFlags(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		port := int(rapid.IntRange(1, 65535).Draw(t, "port"))
		cfg := backend.SSHConfig{
			Host:         "127.0.0.1",
			Port:         port,
			User:         "dev",
			IdentityFile: "/home/user/.ssh/key",
			Transport:    "tcp",
		}

		args := buildRsyncArgs(cfg, "/src", "/dst", false)
		argStr := strings.Join(args, " ")
		assert.NotContains(t, argStr, "--dry-run")
		assert.NotContains(t, argStr, "--itemize-changes")
	})
}

// Property: sshTarget always includes user and path
func TestProperty_SshTarget_ContainsUserAndPath(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		user := rapid.SampledFrom([]string{"dev", "ubuntu", "root", "test-user"}).Draw(t, "user")
		path := rapid.SampledFrom([]string{"/home/dev/proj", "/tmp", "/var/log/app"}).Draw(t, "path")
		transport := rapid.SampledFrom([]string{"tcp", "vsock"}).Draw(t, "transport")

		cfg := backend.SSHConfig{
			Host:      "192.168.1.100",
			User:      user,
			Transport: transport,
		}

		target := sshTarget(cfg, path)
		assert.Contains(t, target, user+"@")
		assert.Contains(t, target, path)
	})
}

// Property: VSOCK transport always uses localhost, TCP uses the Host
func TestProperty_SshTarget_TransportCorrect(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		user := rapid.OneOf(
			rapid.Just("dev"),
			rapid.Just("ubuntu"),
		).Draw(t, "user")

		tcpCfg := backend.SSHConfig{Host: "10.0.0.5", User: user, Transport: "tcp"}
		tcpTarget := sshTarget(tcpCfg, "/path")
		assert.Contains(t, tcpTarget, "10.0.0.5")
		assert.NotContains(t, tcpTarget, "localhost")

		vsockCfg := backend.SSHConfig{User: user, Transport: "vsock"}
		vsockTarget := sshTarget(vsockCfg, "/path")
		assert.Contains(t, vsockTarget, "localhost")
	})
}

// Property: buildRsyncArgs always ends with src dst
func TestProperty_RsyncArgs_EndsWithSrcDst(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cfg := backend.SSHConfig{
			Host:         "127.0.0.1",
			Port:         22,
			User:         "dev",
			IdentityFile: "/home/user/.ssh/key",
			Transport:    "tcp",
		}
		src := rapid.SampledFrom([]string{"/a", "/b/c", "/tmp/x"}).Draw(t, "src")
		dst := rapid.SampledFrom([]string{"dev@h:/a", "dev@h:/b/c"}).Draw(t, "dst")
		dryRun := rapid.Bool().Draw(t, "dryRun")

		args := buildRsyncArgs(cfg, src, dst, dryRun)
		assert.Equal(t, src, args[len(args)-2])
		assert.Equal(t, dst, args[len(args)-1])
	})
}

// Property: SyncTo on stopped VM never calls rsync
func TestProperty_SyncTo_StoppedVM_NeverCallsRsync(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		MockRun = mocklimactl.MockRun

		name := rapid.StringMatching(`[a-z][a-z0-9\-]{2,14}`).Draw(t, "name")
		rec := &rsyncRecorder{}
		origRsyncRun := rsyncRun
		rsyncRun = rec.run
		defer func() { rsyncRun = origRsyncRun }()

		origVSOCK := isVSOCKTransport
		isVSOCKTransport = func() bool { return false }
		defer func() { isVSOCKTransport = origVSOCK }()

		b := NewWithExecutor(&mockExecutor{})
		ctx := context.Background()
		cfg := backend.VMConfig{CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04"}
		require.NoError(t, b.Create(ctx, name, cfg))
		// VM is stopped — do NOT start it

		syncer := b.(backend.Syncer)
		err := syncer.SyncTo(ctx, name, "/host", "/guest")
		assert.Error(t, err)
		assert.Empty(t, rec.calls, "rsync should never be called on stopped VM")
	})
}

// Property: SyncTo on nonexistent VM never calls rsync
func TestProperty_SyncTo_NonexistentVM_NeverCallsRsync(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		MockRun = mocklimactl.MockRun

		name := rapid.StringMatching(`ghost-[a-z0-9]{3,8}`).Draw(t, "name")
		rec := &rsyncRecorder{}
		origRsyncRun := rsyncRun
		rsyncRun = rec.run
		defer func() { rsyncRun = origRsyncRun }()

		origVSOCK := isVSOCKTransport
		isVSOCKTransport = func() bool { return false }
		defer func() { isVSOCKTransport = origVSOCK }()

		b := NewWithExecutor(&mockExecutor{})

		syncer := b.(backend.Syncer)
		err := syncer.SyncTo(context.Background(), name, "/host", "/guest")
		assert.Error(t, err)
		assert.Empty(t, rec.calls, "rsync should never be called on nonexistent VM")
	})
}

// Property: cancelled context always returns error with "cancelled"
func TestProperty_Sync_CancelledContext_AlwaysFails(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		mocklimactl.Reset()
		MockRun = mocklimactl.MockRun

		name := rapid.StringMatching(`cancel-[a-z]{2,6}`).Draw(rt, "name")
		rec := &rsyncRecorder{}
		origRsyncRun := rsyncRun
		rsyncRun = rec.run
		defer func() { rsyncRun = origRsyncRun }()

		origVSOCK := isVSOCKTransport
		isVSOCKTransport = func() bool { return false }
		defer func() { isVSOCKTransport = origVSOCK }()

		b := NewWithExecutor(&mockExecutor{})
		ctx := context.Background()
		cfg := backend.VMConfig{CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04"}
		require.NoError(t, b.Create(ctx, name, cfg))
		require.NoError(t, b.Start(ctx, name))

		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel()

		syncer := b.(backend.Syncer)
		op := rapid.SampledFrom([]string{"to", "from", "diff"}).Draw(rt, "op")

		var err error
		switch op {
		case "to":
			err = syncer.SyncTo(cancelCtx, name, "/host", "/guest")
		case "from":
			err = syncer.SyncFrom(cancelCtx, name, "/guest", "/host")
		case "diff":
			_, err = syncer.SyncDiff(cancelCtx, name, "/guest", "/host")
		}

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cancelled")
		assert.Empty(t, rec.calls)
	})
}
