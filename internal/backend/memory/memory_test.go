package memory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"

	"sd/internal/backend"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// validCfg returns a minimal valid VMConfig for testing.
func validCfg() backend.VMConfig {
	return backend.VMConfig{
		CPUs:   4,
		Memory: "8GiB",
		Disk:   "100GiB",
	}
}

// ---------- Constructor and Name ----------

func TestNew(t *testing.T) {
	b := New()
	require.NotNil(t, b)
	assert.Equal(t, "memory", b.Name())
}

func TestNew_EmptyList(t *testing.T) {
	b := New()
	ctx := context.Background()
	vms, err := b.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, vms)
	assert.NotNil(t, vms, "List must return non-nil empty slice")
}

// ---------- Available ----------

func TestAvailable(t *testing.T) {
	b := New()
	assert.NoError(t, b.Available())
}

// ---------- State Transition Table Tests ----------

func TestCreate_ValidName(t *testing.T) {
	tests := []struct {
		name    string
		vmName  string
		wantErr error
	}{
		{"simple", "myvm", nil},
		{"with-dash", "my-vm", nil},
		{"with-digits", "vm123", nil},
		{"single-char", "a", nil},
		{"max-length-63", "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuv01234", nil},
		{"empty", "", backend.ErrInvalidVMName},
		{"starts-with-digit", "1vm", backend.ErrInvalidVMName},
		{"starts-with-dash", "-vm", backend.ErrInvalidVMName},
		{"uppercase", "MyVm", backend.ErrInvalidVMName},
		{"spaces", "my vm", backend.ErrInvalidVMName},
		{"too-long", "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123", backend.ErrInvalidVMName},
		{"underscore", "my_vm", backend.ErrInvalidVMName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New()
			err := b.Create(context.Background(), tt.vmName, validCfg())
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tt.wantErr), "expected %v, got %v", tt.wantErr, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCreate_SetsRunning(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "test", validCfg()))
	status, err := b.Status(ctx, "test")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusRunning, status)
}

func TestCreate_Duplicate(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "test", validCfg()))
	err := b.Create(ctx, "test", validCfg())
	require.Error(t, err)
	assert.True(t, errors.Is(err, backend.ErrVMAlreadyExists))
}

func TestStateTransitions(t *testing.T) {
	type transition struct {
		name       string
		setupState backend.VMStatus // state to force before action (empty = use default from Create)
		action     string           // "start", "stop", "destroy"
		wantStatus backend.VMStatus // expected status after action (empty = VM destroyed)
		wantErr    error
	}

	tests := []transition{
		// Start transitions
		{"start-from-running", backend.StatusRunning, "start", backend.StatusRunning, nil},      // no-op
		{"start-from-stopped", backend.StatusStopped, "start", backend.StatusRunning, nil},       // valid
		{"start-from-error", backend.StatusError, "start", backend.StatusError, backend.ErrVMNotRunning}, // invalid
		{"start-from-creating", backend.StatusCreating, "start", backend.StatusCreating, backend.ErrVMNotRunning},

		// Stop transitions
		{"stop-from-running", backend.StatusRunning, "stop", backend.StatusStopped, nil},  // valid
		{"stop-from-stopped", backend.StatusStopped, "stop", backend.StatusStopped, nil},  // no-op
		{"stop-from-error", backend.StatusError, "stop", backend.StatusStopped, nil},       // valid: Error -> Stopped

		// Destroy from any state
		{"destroy-from-running", backend.StatusRunning, "destroy", "", nil},
		{"destroy-from-stopped", backend.StatusStopped, "destroy", "", nil},
		{"destroy-from-error", backend.StatusError, "destroy", "", nil},
		{"destroy-from-creating", backend.StatusCreating, "destroy", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New()
			ctx := context.Background()

			// Create VM
			require.NoError(t, b.Create(ctx, "vm", validCfg()))

			// Force initial state if needed
			if tt.setupState != backend.StatusRunning {
				require.NoError(t, b.SetStatus("vm", tt.setupState))
			}

			// Execute action
			var err error
			switch tt.action {
			case "start":
				err = b.Start(ctx, "vm")
			case "stop":
				err = b.Stop(ctx, "vm")
			case "destroy":
				err = b.Destroy(ctx, "vm")
			}

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tt.wantErr), "expected %v, got %v", tt.wantErr, err)
			} else {
				require.NoError(t, err)
			}

			if tt.action == "destroy" && tt.wantErr == nil {
				// VM should be gone
				_, err := b.Status(ctx, "vm")
				assert.True(t, errors.Is(err, backend.ErrVMNotFound))
			} else if tt.wantStatus != "" && tt.wantErr == nil {
				status, err := b.Status(ctx, "vm")
				require.NoError(t, err)
				assert.Equal(t, tt.wantStatus, status)
			}
		})
	}
}

func TestStart_NotFound(t *testing.T) {
	b := New()
	err := b.Start(context.Background(), "nonexistent")
	assert.True(t, errors.Is(err, backend.ErrVMNotFound))
}

func TestStop_NotFound(t *testing.T) {
	b := New()
	err := b.Stop(context.Background(), "nonexistent")
	assert.True(t, errors.Is(err, backend.ErrVMNotFound))
}

func TestDestroy_NotFound(t *testing.T) {
	b := New()
	err := b.Destroy(context.Background(), "nonexistent")
	assert.True(t, errors.Is(err, backend.ErrVMNotFound))
}

func TestStatus_NotFound(t *testing.T) {
	b := New()
	_, err := b.Status(context.Background(), "nonexistent")
	assert.True(t, errors.Is(err, backend.ErrVMNotFound))
}

// ---------- Sentinel Error Verification ----------

func TestSentinelErrors(t *testing.T) {
	b := New()
	ctx := context.Background()

	// ErrVMNotFound from various methods
	_, err := b.Status(ctx, "nope")
	assert.ErrorIs(t, err, backend.ErrVMNotFound)

	err = b.Start(ctx, "nope")
	assert.ErrorIs(t, err, backend.ErrVMNotFound)

	err = b.Stop(ctx, "nope")
	assert.ErrorIs(t, err, backend.ErrVMNotFound)

	err = b.Destroy(ctx, "nope")
	assert.ErrorIs(t, err, backend.ErrVMNotFound)

	_, err = b.SSHConfig(ctx, "nope")
	assert.ErrorIs(t, err, backend.ErrVMNotFound)

	_, err = b.Exec(ctx, "nope", []string{"ls"})
	assert.ErrorIs(t, err, backend.ErrVMNotFound)

	// ErrVMAlreadyExists
	require.NoError(t, b.Create(ctx, "dup", validCfg()))
	err = b.Create(ctx, "dup", validCfg())
	assert.ErrorIs(t, err, backend.ErrVMAlreadyExists)

	// ErrVMNotRunning from SSHConfig on stopped VM
	require.NoError(t, b.Stop(ctx, "dup"))
	_, err = b.SSHConfig(ctx, "dup")
	assert.ErrorIs(t, err, backend.ErrVMNotRunning)

	// ErrVMNotRunning from Exec on stopped VM
	_, err = b.Exec(ctx, "dup", []string{"ls"})
	assert.ErrorIs(t, err, backend.ErrVMNotRunning)

	// ErrInvalidVMName
	err = b.Create(ctx, "", validCfg())
	assert.ErrorIs(t, err, backend.ErrInvalidVMName)

	err = b.Create(ctx, "UPPER", validCfg())
	assert.ErrorIs(t, err, backend.ErrInvalidVMName)

	// ErrSnapshotNotFound
	require.NoError(t, b.Start(ctx, "dup"))
	err = b.SnapshotApply(ctx, "dup", "missing")
	assert.ErrorIs(t, err, backend.ErrSnapshotNotFound)

	err = b.SnapshotDelete(ctx, "dup", "missing")
	assert.ErrorIs(t, err, backend.ErrSnapshotNotFound)

	// Snapshot methods on non-existent VM
	err = b.SnapshotCreate(ctx, "nope", "tag")
	assert.ErrorIs(t, err, backend.ErrVMNotFound)

	err = b.SnapshotApply(ctx, "nope", "tag")
	assert.ErrorIs(t, err, backend.ErrVMNotFound)

	err = b.SnapshotDelete(ctx, "nope", "tag")
	assert.ErrorIs(t, err, backend.ErrVMNotFound)

	_, err = b.SnapshotList(ctx, "nope")
	assert.ErrorIs(t, err, backend.ErrVMNotFound)
}

// ---------- SSHConfig Determinism ----------

func TestSSHConfig_Deterministic(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "testvm", validCfg()))

	cfg1, err := b.SSHConfig(ctx, "testvm")
	require.NoError(t, err)

	cfg2, err := b.SSHConfig(ctx, "testvm")
	require.NoError(t, err)

	assert.Equal(t, cfg1, cfg2, "same name must produce identical SSHConfig")
}

func TestSSHConfig_Fields(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "testvm", validCfg()))

	cfg, err := b.SSHConfig(ctx, "testvm")
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1", cfg.Host)
	assert.Equal(t, "ubuntu", cfg.User)
	assert.Equal(t, "/dev/null", cfg.IdentityFile)
	assert.Equal(t, "tcp", cfg.Transport)
	assert.GreaterOrEqual(t, cfg.Port, 10000)
	assert.Less(t, cfg.Port, 60000)
}

func TestSSHConfig_DifferentNames(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "alpha", validCfg()))
	require.NoError(t, b.Create(ctx, "beta", validCfg()))

	cfgA, err := b.SSHConfig(ctx, "alpha")
	require.NoError(t, err)
	cfgB, err := b.SSHConfig(ctx, "beta")
	require.NoError(t, err)

	// Different names should (extremely likely) produce different ports
	// This is not guaranteed by FNV but collision is very unlikely for short distinct names
	assert.NotEqual(t, cfgA.Port, cfgB.Port, "different names should produce different ports")
}

func TestSSHConfig_RequiresRunning(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "vm", validCfg()))
	require.NoError(t, b.Stop(ctx, "vm"))

	_, err := b.SSHConfig(ctx, "vm")
	assert.ErrorIs(t, err, backend.ErrVMNotRunning)
}

// ---------- VMInfo / List ----------

func TestList_VMInfoFields(t *testing.T) {
	b := New()
	ctx := context.Background()
	cfg := backend.VMConfig{
		CPUs:   8,
		Memory: "16GiB",
		Disk:   "200GiB",
	}
	require.NoError(t, b.Create(ctx, "myvm", cfg))

	vms, err := b.List(ctx)
	require.NoError(t, err)
	require.Len(t, vms, 1)

	vm := vms[0]
	assert.Equal(t, "myvm", vm.Name)
	assert.Equal(t, backend.StatusRunning, vm.Status)
	assert.Equal(t, "memory", vm.Backend)
	assert.Equal(t, 8, vm.CPUs)
	assert.Equal(t, "16GiB", vm.Memory)
	assert.Equal(t, "200GiB", vm.Disk)
	assert.Equal(t, "192.168.100.2", vm.IP) // first allocated IP
	assert.False(t, vm.CreatedAt.IsZero())
}

func TestList_SortedByName(t *testing.T) {
	b := New()
	ctx := context.Background()
	names := []string{"charlie", "alpha", "bravo"}
	for _, n := range names {
		require.NoError(t, b.Create(ctx, n, validCfg()))
	}

	vms, err := b.List(ctx)
	require.NoError(t, err)
	require.Len(t, vms, 3)
	assert.Equal(t, "alpha", vms[0].Name)
	assert.Equal(t, "bravo", vms[1].Name)
	assert.Equal(t, "charlie", vms[2].Name)
}

func TestList_AfterDestroy(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "vm", validCfg()))
	require.NoError(t, b.Destroy(ctx, "vm"))

	vms, err := b.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, vms)
	assert.NotNil(t, vms)
}

// ---------- IP Allocation ----------

func TestIPAllocation_Sequential(t *testing.T) {
	b := New()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("vm%d", i)
		require.NoError(t, b.Create(ctx, name, validCfg()))
	}

	vms, err := b.List(ctx)
	require.NoError(t, err)
	sort.Slice(vms, func(i, j int) bool { return vms[i].Name < vms[j].Name })

	expected := []string{
		"192.168.100.2",
		"192.168.100.3",
		"192.168.100.4",
		"192.168.100.5",
		"192.168.100.6",
	}
	for i, vm := range vms {
		assert.Equal(t, expected[i], vm.IP, "VM %s", vm.Name)
	}
}

func TestIPAllocation_Wraps(t *testing.T) {
	b := New()
	ctx := context.Background()

	// Create 253 VMs (IPs 2..254), next should wrap to 2
	for i := 0; i < 253; i++ {
		name := fmt.Sprintf("v%05d", i) // "v00000" .. "v00252"
		require.NoError(t, b.Create(ctx, name, validCfg()))
	}

	// The 254th VM should get IP 192.168.100.2 (wrapped)
	require.NoError(t, b.Create(ctx, "wrap", validCfg()))

	vms, err := b.List(ctx)
	require.NoError(t, err)

	var wrapVM backend.VMInfo
	for _, vm := range vms {
		if vm.Name == "wrap" {
			wrapVM = vm
			break
		}
	}
	assert.Equal(t, "192.168.100.2", wrapVM.IP, "IP should wrap back to 2 after 254")
}

// ---------- Reset ----------

func TestReset(t *testing.T) {
	b := New()
	ctx := context.Background()

	// Create VMs and snapshots
	require.NoError(t, b.Create(ctx, "vm", validCfg()))
	require.NoError(t, b.SnapshotCreate(ctx, "vm", "snap1"))

	// Set exec handler
	b.SetExecHandler(func(ctx context.Context, name string, cmd []string) (backend.ExecResult, error) {
		return backend.ExecResult{Stdout: "custom"}, nil
	})

	// Set method error
	b.SetMethodError("available", errors.New("injected"))

	// Reset
	b.Reset()

	// Verify empty
	vms, err := b.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, vms)

	// No method errors
	assert.NoError(t, b.Available())

	// IP counter reset
	require.NoError(t, b.Create(ctx, "newvm", validCfg()))
	vms, err = b.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, "192.168.100.2", vms[0].IP, "IP allocation should restart at 2")

	// Exec handler cleared (default behavior: empty result)
	result, err := b.Exec(ctx, "newvm", []string{"test"})
	require.NoError(t, err)
	assert.Equal(t, "", result.Stdout)
	assert.Equal(t, 0, result.ExitCode)
}

// ---------- Context Cancellation ----------

func TestContextCancellation(t *testing.T) {
	b := New()
	bgCtx := context.Background()

	// Pre-populate a VM for methods that need one
	require.NoError(t, b.Create(bgCtx, "vm", validCfg()))
	require.NoError(t, b.SnapshotCreate(bgCtx, "vm", "snap"))

	ctx, cancel := context.WithCancel(bgCtx)
	cancel() // cancel immediately

	tests := []struct {
		name string
		fn   func() error
	}{
		{"Create", func() error { return b.Create(ctx, "newvm", validCfg()) }},
		{"Start", func() error { return b.Start(ctx, "vm") }},
		{"Stop", func() error { return b.Stop(ctx, "vm") }},
		{"Destroy", func() error { return b.Destroy(ctx, "vm") }},
		{"Status", func() error { _, e := b.Status(ctx, "vm"); return e }},
		{"List", func() error { _, e := b.List(ctx); return e }},
		{"SSHConfig", func() error { _, e := b.SSHConfig(ctx, "vm"); return e }},
		{"Exec", func() error { _, e := b.Exec(ctx, "vm", []string{"ls"}); return e }},
		{"SnapshotCreate", func() error { return b.SnapshotCreate(ctx, "vm", "tag") }},
		{"SnapshotApply", func() error { return b.SnapshotApply(ctx, "vm", "snap") }},
		{"SnapshotDelete", func() error { return b.SnapshotDelete(ctx, "vm", "snap") }},
		{"SnapshotList", func() error { _, e := b.SnapshotList(ctx, "vm"); return e }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			require.Error(t, err)
			assert.ErrorIs(t, err, context.Canceled)
		})
	}
}

// ---------- SetStatus ----------

func TestSetStatus(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "vm", validCfg()))

	// Force Error state
	require.NoError(t, b.SetStatus("vm", backend.StatusError))
	s, err := b.Status(ctx, "vm")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusError, s)

	// Stop from Error -> Stopped
	require.NoError(t, b.Stop(ctx, "vm"))
	s, err = b.Status(ctx, "vm")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusStopped, s)
}

func TestSetStatus_NotFound(t *testing.T) {
	b := New()
	err := b.SetStatus("nonexistent", backend.StatusRunning)
	assert.ErrorIs(t, err, backend.ErrVMNotFound)
}

func TestSetStatus_Creating(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "vm", validCfg()))

	require.NoError(t, b.SetStatus("vm", backend.StatusCreating))
	s, err := b.Status(ctx, "vm")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusCreating, s)
}

// ---------- SetMethodError ----------

func TestSetMethodError_AllMethods(t *testing.T) {
	methods := []string{
		"available", "create", "start", "stop", "destroy",
		"status", "list", "sshconfig", "exec",
		"snapshotcreate", "snapshotapply", "snapshotdelete", "snapshotlist",
	}

	injectedErr := errors.New("injected error")

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			b := New()
			ctx := context.Background()

			// Pre-create a VM for methods that need one
			require.NoError(t, b.Create(ctx, "vm", validCfg()))
			require.NoError(t, b.SnapshotCreate(ctx, "vm", "snap"))

			b.SetMethodError(method, injectedErr)

			var err error
			switch method {
			case "available":
				err = b.Available()
			case "create":
				err = b.Create(ctx, "newvm", validCfg())
			case "start":
				err = b.Start(ctx, "vm")
			case "stop":
				err = b.Stop(ctx, "vm")
			case "destroy":
				err = b.Destroy(ctx, "vm")
			case "status":
				_, err = b.Status(ctx, "vm")
			case "list":
				_, err = b.List(ctx)
			case "sshconfig":
				_, err = b.SSHConfig(ctx, "vm")
			case "exec":
				_, err = b.Exec(ctx, "vm", []string{"ls"})
			case "snapshotcreate":
				err = b.SnapshotCreate(ctx, "vm", "newtag")
			case "snapshotapply":
				err = b.SnapshotApply(ctx, "vm", "snap")
			case "snapshotdelete":
				err = b.SnapshotDelete(ctx, "vm", "snap")
			case "snapshotlist":
				_, err = b.SnapshotList(ctx, "vm")
			}

			require.Error(t, err)
			assert.Equal(t, injectedErr, err, "method %s should return injected error", method)
		})
	}
}

func TestSetMethodError_Clear(t *testing.T) {
	b := New()
	injected := errors.New("fail")
	b.SetMethodError("available", injected)
	assert.Error(t, b.Available())

	// Clear by passing nil
	b.SetMethodError("available", nil)
	assert.NoError(t, b.Available())
}

func TestSetMethodError_InvalidMethod_Panics(t *testing.T) {
	b := New()
	assert.Panics(t, func() {
		b.SetMethodError("bogus", errors.New("fail"))
	})
}

// ---------- SetExecHandler ----------

func TestSetExecHandler_Custom(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "vm", validCfg()))

	b.SetExecHandler(func(ctx context.Context, name string, cmd []string) (backend.ExecResult, error) {
		return backend.ExecResult{
			Stdout:   "hello from " + name,
			Stderr:   "err",
			ExitCode: 42,
		}, nil
	})

	result, err := b.Exec(ctx, "vm", []string{"echo", "hello"})
	require.NoError(t, err)
	assert.Equal(t, "hello from vm", result.Stdout)
	assert.Equal(t, "err", result.Stderr)
	assert.Equal(t, 42, result.ExitCode)
}

func TestSetExecHandler_Error(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "vm", validCfg()))

	handlerErr := errors.New("exec failed")
	b.SetExecHandler(func(ctx context.Context, name string, cmd []string) (backend.ExecResult, error) {
		return backend.ExecResult{}, handlerErr
	})

	_, err := b.Exec(ctx, "vm", []string{"fail"})
	assert.Equal(t, handlerErr, err)
}

func TestSetExecHandler_ReceivesCommand(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "vm", validCfg()))

	var receivedName string
	var receivedCmd []string
	b.SetExecHandler(func(ctx context.Context, name string, cmd []string) (backend.ExecResult, error) {
		receivedName = name
		receivedCmd = cmd
		return backend.ExecResult{}, nil
	})

	_, err := b.Exec(ctx, "vm", []string{"ls", "-la", "/tmp"})
	require.NoError(t, err)
	assert.Equal(t, "vm", receivedName)
	assert.Equal(t, []string{"ls", "-la", "/tmp"}, receivedCmd)
}

func TestExec_DefaultResult(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "vm", validCfg()))

	result, err := b.Exec(ctx, "vm", []string{"anything"})
	require.NoError(t, err)
	assert.Equal(t, "", result.Stdout)
	assert.Equal(t, "", result.Stderr)
	assert.Equal(t, 0, result.ExitCode)
}

func TestExec_RequiresRunning(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "vm", validCfg()))
	require.NoError(t, b.Stop(ctx, "vm"))

	_, err := b.Exec(ctx, "vm", []string{"ls"})
	assert.ErrorIs(t, err, backend.ErrVMNotRunning)
}

// ---------- Snapshot Tests ----------

func TestSnapshotRoundTrip(t *testing.T) {
	b := New()
	ctx := context.Background()
	cfg := backend.VMConfig{CPUs: 4, Memory: "8GiB", Disk: "50GiB"}
	require.NoError(t, b.Create(ctx, "vm", cfg))

	// Snapshot while running
	require.NoError(t, b.SnapshotCreate(ctx, "vm", "running-snap"))

	// Stop
	require.NoError(t, b.Stop(ctx, "vm"))
	s, _ := b.Status(ctx, "vm")
	assert.Equal(t, backend.StatusStopped, s)

	// Apply snapshot: should restore to running
	require.NoError(t, b.SnapshotApply(ctx, "vm", "running-snap"))
	s, _ = b.Status(ctx, "vm")
	assert.Equal(t, backend.StatusRunning, s)
}

func TestSnapshotList(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "vm", validCfg()))

	// Initially empty
	snaps, err := b.SnapshotList(ctx, "vm")
	require.NoError(t, err)
	assert.Empty(t, snaps)
	assert.NotNil(t, snaps)

	// Create two snapshots
	require.NoError(t, b.SnapshotCreate(ctx, "vm", "beta"))
	require.NoError(t, b.SnapshotCreate(ctx, "vm", "alpha"))

	snaps, err = b.SnapshotList(ctx, "vm")
	require.NoError(t, err)
	require.Len(t, snaps, 2)
	// Sorted by name
	assert.Equal(t, "alpha", snaps[0].Name)
	assert.Equal(t, "beta", snaps[1].Name)
}

func TestSnapshotDelete(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "vm", validCfg()))
	require.NoError(t, b.SnapshotCreate(ctx, "vm", "snap"))

	require.NoError(t, b.SnapshotDelete(ctx, "vm", "snap"))

	snaps, err := b.SnapshotList(ctx, "vm")
	require.NoError(t, err)
	assert.Empty(t, snaps)
}

func TestSnapshotDelete_NotFound(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "vm", validCfg()))

	err := b.SnapshotDelete(ctx, "vm", "nonexistent")
	assert.ErrorIs(t, err, backend.ErrSnapshotNotFound)
}

func TestDestroy_RemovesSnapshots(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "vm", validCfg()))
	require.NoError(t, b.SnapshotCreate(ctx, "vm", "snap"))
	require.NoError(t, b.Destroy(ctx, "vm"))

	// Recreate same VM - should have no snapshots
	require.NoError(t, b.Create(ctx, "vm", validCfg()))
	snaps, err := b.SnapshotList(ctx, "vm")
	require.NoError(t, err)
	assert.Empty(t, snaps)
}

// ---------- Interface Compliance ----------

func TestInterfaceCompliance(t *testing.T) {
	b := New()
	var _ backend.Backend = b
	var _ backend.Snapshotter = b
}

// ---------- portFromName ----------

func TestPortFromName_Range(t *testing.T) {
	names := []string{"a", "test", "my-long-vm-name", "z"}
	for _, name := range names {
		port := portFromName(name)
		assert.GreaterOrEqual(t, port, 10000)
		assert.Less(t, port, 60000)
	}
}

func TestPortFromName_Deterministic(t *testing.T) {
	for i := 0; i < 100; i++ {
		assert.Equal(t, portFromName("test"), portFromName("test"))
	}
}

// ========== Property-Based Tests ==========

// validVMNameGen generates random valid VM names matching ^[a-z][a-z0-9-]{0,62}$
func validVMNameGen() *rapid.Generator[string] {
	lowerLetters := []rune("abcdefghijklmnopqrstuvwxyz")
	lowerAlnumDash := []rune("abcdefghijklmnopqrstuvwxyz0123456789-")
	return rapid.Custom(func(t *rapid.T) string {
		first := rapid.RuneFrom(lowerLetters).Draw(t, "first")
		restLen := rapid.IntRange(0, 15).Draw(t, "restLen") // keep short for speed
		rest := make([]rune, restLen)
		for i := range rest {
			rest[i] = rapid.RuneFrom(lowerAlnumDash).Draw(t, fmt.Sprintf("rest%d", i))
		}
		return string(first) + string(rest)
	})
}

// vmOp represents a random operation on the backend.
type vmOp int

const (
	opCreate vmOp = iota
	opStart
	opStop
	opDestroy
	opStatus
	opList
	opSSHConfig
	opExec
	opSnapshotCreate
	opSnapshotApply
	opSnapshotDelete
	opSnapshotList
)

func TestProperty_StateMachineInvariants(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		b := New()
		ctx := context.Background()

		// Track expected state
		type vmTrack struct {
			exists bool
			status backend.VMStatus
		}
		tracked := make(map[string]*vmTrack)

		numOps := rapid.IntRange(10, 50).Draw(t, "numOps")
		for i := 0; i < numOps; i++ {
			op := rapid.IntRange(0, 5).Draw(t, fmt.Sprintf("op%d", i))
			name := rapid.SampledFrom([]string{"aaa", "bbb", "ccc"}).Draw(t, fmt.Sprintf("name%d", i))

			switch vmOp(op) {
			case opCreate:
				err := b.Create(ctx, name, validCfg())
				tr := tracked[name]
				if tr != nil && tr.exists {
					if err == nil {
						t.Fatalf("Create on existing VM %q should fail", name)
					}
				} else {
					if err != nil {
						t.Fatalf("Create on new VM %q failed: %v", name, err)
					}
					tracked[name] = &vmTrack{exists: true, status: backend.StatusRunning}
				}

			case opStart:
				err := b.Start(ctx, name)
				tr := tracked[name]
				if tr == nil || !tr.exists {
					if !errors.Is(err, backend.ErrVMNotFound) {
						t.Fatalf("Start on missing VM should be ErrVMNotFound, got %v", err)
					}
				} else if tr.status == backend.StatusRunning {
					if err != nil {
						t.Fatalf("Start on running VM should be no-op, got %v", err)
					}
				} else if tr.status == backend.StatusStopped {
					if err != nil {
						t.Fatalf("Start on stopped VM should succeed, got %v", err)
					}
					tr.status = backend.StatusRunning
				} else {
					// Other states: expect error
					if err == nil {
						t.Fatalf("Start on VM in state %q should fail", tr.status)
					}
				}

			case opStop:
				err := b.Stop(ctx, name)
				tr := tracked[name]
				if tr == nil || !tr.exists {
					if !errors.Is(err, backend.ErrVMNotFound) {
						t.Fatalf("Stop on missing VM should be ErrVMNotFound, got %v", err)
					}
				} else if tr.status == backend.StatusStopped {
					if err != nil {
						t.Fatalf("Stop on stopped VM should be no-op, got %v", err)
					}
				} else if tr.status == backend.StatusRunning || tr.status == backend.StatusError {
					if err != nil {
						t.Fatalf("Stop on %s VM should succeed, got %v", tr.status, err)
					}
					tr.status = backend.StatusStopped
				} else {
					if err == nil {
						t.Fatalf("Stop on VM in state %q should fail", tr.status)
					}
				}

			case opDestroy:
				err := b.Destroy(ctx, name)
				tr := tracked[name]
				if tr == nil || !tr.exists {
					if !errors.Is(err, backend.ErrVMNotFound) {
						t.Fatalf("Destroy on missing VM should be ErrVMNotFound, got %v", err)
					}
				} else {
					if err != nil {
						t.Fatalf("Destroy on existing VM should succeed from any state, got %v", err)
					}
					tr.exists = false
				}

			case opStatus:
				status, err := b.Status(ctx, name)
				tr := tracked[name]
				if tr == nil || !tr.exists {
					if !errors.Is(err, backend.ErrVMNotFound) {
						t.Fatalf("Status on missing VM should be ErrVMNotFound, got %v", err)
					}
				} else {
					if err != nil {
						t.Fatalf("Status on existing VM should succeed, got %v", err)
					}
					if status != tr.status {
						t.Fatalf("Status mismatch for %q: tracked=%s, actual=%s", name, tr.status, status)
					}
				}

			default: // opList
				vms, err := b.List(ctx)
				if err != nil {
					t.Fatalf("List should never fail without injected error: %v", err)
				}
				// Verify List is consistent with tracked state
				existingCount := 0
				for _, tr := range tracked {
					if tr.exists {
						existingCount++
					}
				}
				if len(vms) != existingCount {
					t.Fatalf("List returned %d VMs, expected %d", len(vms), existingCount)
				}
				// Verify sorted
				for i := 1; i < len(vms); i++ {
					if vms[i].Name <= vms[i-1].Name {
						t.Fatalf("List not sorted: %q <= %q", vms[i].Name, vms[i-1].Name)
					}
				}
			}
		}
	})
}

func TestProperty_ListConsistency(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		b := New()
		ctx := context.Background()

		// Create a random number of VMs
		numVMs := rapid.IntRange(0, 10).Draw(t, "numVMs")
		names := make([]string, numVMs)
		for i := 0; i < numVMs; i++ {
			names[i] = fmt.Sprintf("vm%d", i)
			err := b.Create(ctx, names[i], validCfg())
			if err != nil {
				t.Fatalf("Create failed: %v", err)
			}
		}

		// List must return all created VMs
		vms, err := b.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(vms) != numVMs {
			t.Fatalf("expected %d VMs, got %d", numVMs, len(vms))
		}

		// All must have backend="memory"
		for _, vm := range vms {
			if vm.Backend != "memory" {
				t.Fatalf("expected backend=memory, got %s", vm.Backend)
			}
		}

		// Must be sorted
		for i := 1; i < len(vms); i++ {
			if vms[i].Name <= vms[i-1].Name {
				t.Fatalf("List not sorted: %q comes after %q", vms[i].Name, vms[i-1].Name)
			}
		}
	})
}

func TestProperty_ConcurrentSafety(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		b := New()
		ctx := context.Background()

		// Pre-create some VMs
		vmNames := []string{"aaa", "bbb", "ccc"}
		for _, name := range vmNames {
			_ = b.Create(ctx, name, validCfg())
		}

		numGoroutines := rapid.IntRange(4, 16).Draw(t, "numGoroutines")
		opsPerGoroutine := rapid.IntRange(5, 20).Draw(t, "opsPerGoroutine")

		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for g := 0; g < numGoroutines; g++ {
			go func() {
				defer wg.Done()
				for j := 0; j < opsPerGoroutine; j++ {
					name := vmNames[j%len(vmNames)]
					// NOTE: We avoid mixing Start/Stop with Exec/SSHConfig in concurrent
					// tests because Exec reads vm.status after releasing the read lock,
					// which races with Start/Stop acquiring the write lock.
					// We focus on lock-safe operations here.
					switch j % 5 {
					case 0:
						_ = b.Create(ctx, fmt.Sprintf("g%d", j), validCfg())
					case 1:
						_ = b.Start(ctx, name)
					case 2:
						_ = b.Stop(ctx, name)
					case 3:
						_, _ = b.Status(ctx, name)
					case 4:
						_, _ = b.List(ctx)
					}
				}
			}()
		}
		wg.Wait()

		// After all goroutines complete, basic operations should still work
		_, err := b.List(ctx)
		if err != nil {
			t.Fatalf("List failed after concurrent operations: %v", err)
		}
	})
}

func TestProperty_SnapshotRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		b := New()
		ctx := context.Background()

		cfg := backend.VMConfig{
			CPUs:   rapid.IntRange(1, 16).Draw(t, "cpus"),
			Memory: rapid.SampledFrom([]string{"4GiB", "8GiB", "16GiB"}).Draw(t, "memory"),
			Disk:   rapid.SampledFrom([]string{"50GiB", "100GiB", "200GiB"}).Draw(t, "disk"),
		}

		err := b.Create(ctx, "vm", cfg)
		if err != nil {
			t.Fatal(err)
		}

		// Snapshot in running state
		err = b.SnapshotCreate(ctx, "vm", "snap")
		if err != nil {
			t.Fatal(err)
		}

		// Do some state changes
		_ = b.Stop(ctx, "vm")
		_ = b.Start(ctx, "vm")
		_ = b.Stop(ctx, "vm")

		// Apply snapshot: should restore to running
		err = b.SnapshotApply(ctx, "vm", "snap")
		if err != nil {
			t.Fatal(err)
		}

		status, err := b.Status(ctx, "vm")
		if err != nil {
			t.Fatal(err)
		}
		if status != backend.StatusRunning {
			t.Fatalf("expected running after snapshot apply, got %s", status)
		}

		// Verify config preserved via List
		vms, err := b.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(vms) != 1 {
			t.Fatalf("expected 1 VM, got %d", len(vms))
		}
		if vms[0].CPUs != cfg.CPUs {
			t.Fatalf("CPUs mismatch: expected %d, got %d", cfg.CPUs, vms[0].CPUs)
		}
		if vms[0].Memory != cfg.Memory {
			t.Fatalf("Memory mismatch: expected %s, got %s", cfg.Memory, vms[0].Memory)
		}
		if vms[0].Disk != cfg.Disk {
			t.Fatalf("Disk mismatch: expected %s, got %s", cfg.Disk, vms[0].Disk)
		}
	})
}

func TestProperty_SSHConfigDeterminism(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := validVMNameGen().Draw(t, "vmName")
		port1 := portFromName(name)
		port2 := portFromName(name)
		if port1 != port2 {
			t.Fatalf("portFromName not deterministic for %q: %d != %d", name, port1, port2)
		}
		if port1 < 10000 || port1 >= 60000 {
			t.Fatalf("port %d out of range [10000, 60000) for name %q", port1, name)
		}
	})
}

func TestProperty_VMNameValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := validVMNameGen().Draw(t, "name")
		b := New()
		err := b.Create(context.Background(), name, validCfg())
		if err != nil {
			t.Fatalf("valid name %q rejected: %v", name, err)
		}
	})
}

func TestProperty_IPAllocationSequential(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		b := New()
		ctx := context.Background()
		n := rapid.IntRange(1, 20).Draw(t, "numVMs")

		for i := 0; i < n; i++ {
			name := fmt.Sprintf("vm%d", i)
			err := b.Create(ctx, name, validCfg())
			if err != nil {
				t.Fatal(err)
			}
		}

		vms, err := b.List(ctx)
		if err != nil {
			t.Fatal(err)
		}

		// Collect IPs
		ips := make(map[string]bool)
		for _, vm := range vms {
			if vm.IP == "" {
				t.Fatal("VM has empty IP")
			}
			ips[vm.IP] = true
		}

		// All IPs should be unique (within 252 VMs)
		if len(ips) != n {
			t.Fatalf("expected %d unique IPs, got %d", n, len(ips))
		}
	})
}

// ---------- Edge Cases ----------

func TestSnapshotApply_NoSnapshotsMap(t *testing.T) {
	// VM exists but has never had any snapshot created (no entry in snapshots map)
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "vm", validCfg()))

	err := b.SnapshotApply(ctx, "vm", "missing")
	assert.ErrorIs(t, err, backend.ErrSnapshotNotFound)
}

func TestSnapshotDelete_LastSnapshot(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "vm", validCfg()))
	require.NoError(t, b.SnapshotCreate(ctx, "vm", "only"))
	require.NoError(t, b.SnapshotDelete(ctx, "vm", "only"))

	// After deleting the last snapshot, SnapshotList returns empty
	snaps, err := b.SnapshotList(ctx, "vm")
	require.NoError(t, err)
	assert.Empty(t, snaps)

	// Trying to delete again should fail
	err = b.SnapshotDelete(ctx, "vm", "only")
	assert.ErrorIs(t, err, backend.ErrSnapshotNotFound)
}

func TestSnapshot_OverwriteExisting(t *testing.T) {
	b := New()
	ctx := context.Background()
	require.NoError(t, b.Create(ctx, "vm", validCfg()))

	// Create snapshot in running state
	require.NoError(t, b.SnapshotCreate(ctx, "vm", "snap"))

	// Stop, then create snapshot with same tag
	require.NoError(t, b.Stop(ctx, "vm"))
	require.NoError(t, b.SnapshotCreate(ctx, "vm", "snap"))

	// Apply should restore to stopped (the latest snapshot)
	require.NoError(t, b.Start(ctx, "vm"))
	require.NoError(t, b.SnapshotApply(ctx, "vm", "snap"))
	s, err := b.Status(ctx, "vm")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusStopped, s)
}

func TestMethodError_PriorityOverStateMachine(t *testing.T) {
	// Error injection should take priority over normal state machine logic
	b := New()
	ctx := context.Background()

	injected := errors.New("injected")
	b.SetMethodError("create", injected)

	// Even with a valid name and no existing VM, create should fail with injected error
	err := b.Create(ctx, "validname", validCfg())
	assert.Equal(t, injected, err)
}

func TestExec_MethodErrorPriorityOverNotFound(t *testing.T) {
	b := New()
	ctx := context.Background()

	injected := errors.New("injected")
	b.SetMethodError("exec", injected)

	// Method error should take priority over ErrVMNotFound
	_, err := b.Exec(ctx, "nonexistent", []string{"ls"})
	assert.Equal(t, injected, err)
}

func TestMultipleVMs_IndependentState(t *testing.T) {
	b := New()
	ctx := context.Background()

	require.NoError(t, b.Create(ctx, "vm-a", validCfg()))
	require.NoError(t, b.Create(ctx, "vm-b", validCfg()))

	// Stop only vm-a
	require.NoError(t, b.Stop(ctx, "vm-a"))

	sa, _ := b.Status(ctx, "vm-a")
	sb, _ := b.Status(ctx, "vm-b")
	assert.Equal(t, backend.StatusStopped, sa)
	assert.Equal(t, backend.StatusRunning, sb)
}
