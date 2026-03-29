// Package lima provides integration tests for the Lima backend
// using the mocklimactl digital twin.
// REQ-003-003: VM Lifecycle Operations
// REQ-003-004: VM Status Reporting
// REQ-003-005: VM Listing
// REQ-003-006: SSH Configuration
// REQ-003-007: Command Execution
// REQ-003-013: Backend Registry
package lima

import (
	"context"
	"errors"
	"strings"
	"testing"

	"sd/internal/backend"
	"sd/internal/backend/lima/mocklimactl"
)

// newTestBackend creates a Lima backend wired to the mocklimactl digital twin.
func newTestBackend(t *testing.T) backend.Backend {
	t.Helper()
	mocklimactl.Reset()
	return NewWithExecutor(&mockExecutor{})
}

func init() {
	// Wire the package-level MockRun to the mocklimactl package.
	MockRun = mocklimactl.MockRun
}

// --- REQ-003-003: VM Lifecycle Operations ---

func TestLimaLifecycle_CreateStartStopDestroy(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	cfg := backend.VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}

	// Create
	if err := b.Create(ctx, "testvm", cfg); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Status should be stopped after create
	status, err := b.Status(ctx, "testvm")
	if err != nil {
		t.Fatalf("Status after create failed: %v", err)
	}
	if status != backend.StatusStopped {
		t.Errorf("Status after create = %q, want %q", status, backend.StatusStopped)
	}

	// Start
	if err := b.Start(ctx, "testvm"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	status, err = b.Status(ctx, "testvm")
	if err != nil {
		t.Fatalf("Status after start failed: %v", err)
	}
	if status != backend.StatusRunning {
		t.Errorf("Status after start = %q, want %q", status, backend.StatusRunning)
	}

	// Stop
	if err := b.Stop(ctx, "testvm"); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	status, err = b.Status(ctx, "testvm")
	if err != nil {
		t.Fatalf("Status after stop failed: %v", err)
	}
	if status != backend.StatusStopped {
		t.Errorf("Status after stop = %q, want %q", status, backend.StatusStopped)
	}

	// Destroy
	if err := b.Destroy(ctx, "testvm"); err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}

	// After destroy, Status should return ErrVMNotFound
	_, err = b.Status(ctx, "testvm")
	if err == nil {
		t.Fatal("Status after destroy should return error, got nil")
	}
	if !errors.Is(err, backend.ErrVMNotFound) {
		t.Errorf("error after destroy = %v, want ErrVMNotFound", err)
	}
}

// REQ-003-003: Start on already-running VM is a no-op
func TestLimaLifecycle_StartIdempotent(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	cfg := backend.VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}

	if err := b.Create(ctx, "testvm", cfg); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := b.Start(ctx, "testvm"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Second start should be a no-op
	if err := b.Start(ctx, "testvm"); err != nil {
		t.Errorf("Second Start should be a no-op, got: %v", err)
	}
}

// REQ-003-003: Stop on already-stopped VM is a no-op
func TestLimaLifecycle_StopIdempotent(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	cfg := backend.VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}

	if err := b.Create(ctx, "testvm", cfg); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// VM is stopped after create

	// Stop should be a no-op
	if err := b.Stop(ctx, "testvm"); err != nil {
		t.Errorf("Stop on already-stopped VM should be a no-op, got: %v", err)
	}
}

// REQ-003-003: Destroy on non-existent VM returns error
func TestLimaLifecycle_DestroyNonExistent(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	err := b.Destroy(ctx, "nonexistent")
	if err == nil {
		t.Fatal("Destroy on non-existent VM should return error")
	}
	if !errors.Is(err, backend.ErrVMNotFound) {
		t.Errorf("error = %v, want ErrVMNotFound", err)
	}
}

// REQ-003-003: Create with duplicate name returns error
func TestLimaLifecycle_CreateDuplicate(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	cfg := backend.VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}

	if err := b.Create(ctx, "dup", cfg); err != nil {
		t.Fatalf("First Create failed: %v", err)
	}

	err := b.Create(ctx, "dup", cfg)
	if err == nil {
		t.Fatal("Duplicate Create should return error")
	}
	if !errors.Is(err, backend.ErrVMAlreadyExists) {
		t.Errorf("error = %v, want ErrVMAlreadyExists", err)
	}
}

// --- REQ-003-004: VM Status Reporting ---

func TestLimaStatus_NonExistentVM(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	_, err := b.Status(ctx, "nope")
	if err == nil {
		t.Fatal("Status on non-existent VM should return error")
	}
	if !errors.Is(err, backend.ErrVMNotFound) {
		t.Errorf("error = %v, want ErrVMNotFound", err)
	}
}

// --- REQ-003-005: VM Listing ---

func TestLimaList_NoVMs(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	vms, err := b.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if vms == nil {
		t.Fatal("List returned nil, want empty slice")
	}
	if len(vms) != 0 {
		t.Errorf("List returned %d VMs, want 0", len(vms))
	}
}

func TestLimaList_MultipleVMs(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	cfg := backend.VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}

	if err := b.Create(ctx, "vm1", cfg); err != nil {
		t.Fatalf("Create vm1 failed: %v", err)
	}
	if err := b.Create(ctx, "vm2", cfg); err != nil {
		t.Fatalf("Create vm2 failed: %v", err)
	}

	vms, err := b.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(vms) != 2 {
		t.Fatalf("List returned %d VMs, want 2", len(vms))
	}

	names := map[string]bool{}
	for _, vm := range vms {
		names[vm.Name] = true
		if vm.Backend != "lima" {
			t.Errorf("vm.Backend = %q, want \"lima\"", vm.Backend)
		}
	}
	if !names["vm1"] || !names["vm2"] {
		t.Errorf("List returned unexpected names: %v", names)
	}
}

// --- REQ-003-006: SSH Configuration ---
// REQ-007-005: VSOCK/TCP Transport Detection

func TestLimaSSHConfig_RunningVM(t *testing.T) {
	// Force TCP transport for this test
	orig := isVSOCKTransport
	isVSOCKTransport = func() bool { return false }
	defer func() { isVSOCKTransport = orig }()

	b := newTestBackend(t)
	ctx := context.Background()

	cfg := backend.VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}

	if err := b.Create(ctx, "sshvm", cfg); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := b.Start(ctx, "sshvm"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	sshCfg, err := b.SSHConfig(ctx, "sshvm")
	if err != nil {
		t.Fatalf("SSHConfig failed: %v", err)
	}

	if sshCfg.User != "dev" {
		t.Errorf("SSHConfig.User = %q, want \"dev\"", sshCfg.User)
	}
	if sshCfg.ForwardAgent {
		t.Error("SSHConfig.ForwardAgent = true, want false")
	}
	if sshCfg.Transport != "tcp" {
		t.Errorf("SSHConfig.Transport = %q, want \"tcp\"", sshCfg.Transport)
	}
	if sshCfg.Host != "127.0.0.1" {
		t.Errorf("SSHConfig.Host = %q, want \"127.0.0.1\"", sshCfg.Host)
	}
	if sshCfg.Port == 0 {
		t.Error("SSHConfig.Port = 0, want non-zero SSH port from Lima")
	}
}

// REQ-007-005: VSOCK transport uses ProxyCommand on Apple Silicon
func TestLimaSSHConfig_VSOCKTransport(t *testing.T) {
	orig := isVSOCKTransport
	isVSOCKTransport = func() bool { return true }
	defer func() { isVSOCKTransport = orig }()

	b := newTestBackend(t)
	ctx := context.Background()

	cfg := backend.VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}

	if err := b.Create(ctx, "vsockvm", cfg); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := b.Start(ctx, "vsockvm"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	sshCfg, err := b.SSHConfig(ctx, "vsockvm")
	if err != nil {
		t.Fatalf("SSHConfig failed: %v", err)
	}

	// VSOCK must use ProxyCommand
	if sshCfg.Transport != "vsock" {
		t.Errorf("SSHConfig.Transport = %q, want \"vsock\"", sshCfg.Transport)
	}
	if sshCfg.ProxyCommand != "limactl ssh --stdio vsockvm" {
		t.Errorf("SSHConfig.ProxyCommand = %q, want \"limactl ssh --stdio vsockvm\"", sshCfg.ProxyCommand)
	}
	if sshCfg.User != "dev" {
		t.Errorf("SSHConfig.User = %q, want \"dev\"", sshCfg.User)
	}
	if sshCfg.ForwardAgent {
		t.Error("SSHConfig.ForwardAgent = true, want false")
	}
	if sshCfg.IdentityFile == "" {
		t.Error("SSHConfig.IdentityFile is empty, want a path")
	}
}

// REQ-007-005: TCP transport uses host:port when VSOCK unavailable
func TestLimaSSHConfig_TCPTransport(t *testing.T) {
	orig := isVSOCKTransport
	isVSOCKTransport = func() bool { return false }
	defer func() { isVSOCKTransport = orig }()

	b := newTestBackend(t)
	ctx := context.Background()

	cfg := backend.VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}

	if err := b.Create(ctx, "tcpvm", cfg); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := b.Start(ctx, "tcpvm"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	sshCfg, err := b.SSHConfig(ctx, "tcpvm")
	if err != nil {
		t.Fatalf("SSHConfig failed: %v", err)
	}

	// TCP must use host/port, no ProxyCommand
	if sshCfg.Transport != "tcp" {
		t.Errorf("SSHConfig.Transport = %q, want \"tcp\"", sshCfg.Transport)
	}
	if sshCfg.ProxyCommand != "" {
		t.Errorf("SSHConfig.ProxyCommand = %q, want empty for TCP transport", sshCfg.ProxyCommand)
	}
	if sshCfg.Host != "127.0.0.1" {
		t.Errorf("SSHConfig.Host = %q, want \"127.0.0.1\"", sshCfg.Host)
	}
	if sshCfg.Port == 0 {
		t.Error("SSHConfig.Port = 0, want non-zero port from Lima")
	}
}

// REQ-007-005: Transport is always set (never empty)
func TestLimaSSHConfig_TransportAlwaysSet(t *testing.T) {
	for _, vsock := range []bool{true, false} {
		label := "tcp"
		if vsock {
			label = "vsock"
		}
		t.Run(label, func(t *testing.T) {
			orig := isVSOCKTransport
			isVSOCKTransport = func() bool { return vsock }
			defer func() { isVSOCKTransport = orig }()

			b := newTestBackend(t)
			ctx := context.Background()

			cfg := backend.VMConfig{
				CPUs:      4,
				Memory:    "8GiB",
				Disk:      "100GiB",
				BaseImage: "ubuntu:24.04",
			}

			if err := b.Create(ctx, "tvm", cfg); err != nil {
				t.Fatalf("Create failed: %v", err)
			}
			if err := b.Start(ctx, "tvm"); err != nil {
				t.Fatalf("Start failed: %v", err)
			}

			sshCfg, err := b.SSHConfig(ctx, "tvm")
			if err != nil {
				t.Fatalf("SSHConfig failed: %v", err)
			}

			if sshCfg.Transport == "" {
				t.Error("SSHConfig.Transport is empty, must be \"tcp\" or \"vsock\"")
			}
			if sshCfg.Transport != "tcp" && sshCfg.Transport != "vsock" {
				t.Errorf("SSHConfig.Transport = %q, must be \"tcp\" or \"vsock\"", sshCfg.Transport)
			}
		})
	}
}

func TestLimaSSHConfig_StoppedVM(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	cfg := backend.VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}

	if err := b.Create(ctx, "stopped", cfg); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	_, err := b.SSHConfig(ctx, "stopped")
	if err == nil {
		t.Fatal("SSHConfig on stopped VM should return error")
	}
	if !errors.Is(err, backend.ErrVMNotRunning) {
		t.Errorf("error = %v, want ErrVMNotRunning", err)
	}
}

// --- REQ-003-007: Command Execution ---

func TestLimaExec_RunningVM(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	cfg := backend.VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}

	if err := b.Create(ctx, "execvm", cfg); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := b.Start(ctx, "execvm"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	result, err := b.Exec(ctx, "execvm", []string{"echo", "hello"})
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "hello") {
		t.Errorf("Stdout = %q, want to contain \"hello\"", result.Stdout)
	}
}

func TestLimaExec_StoppedVM(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	cfg := backend.VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}

	if err := b.Create(ctx, "stopped", cfg); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	_, err := b.Exec(ctx, "stopped", []string{"echo", "hello"})
	if err == nil {
		t.Fatal("Exec on stopped VM should return error")
	}
	if !errors.Is(err, backend.ErrVMNotRunning) {
		t.Errorf("error = %v, want ErrVMNotRunning", err)
	}
}

func TestLimaExec_NonExistentVM(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	_, err := b.Exec(ctx, "nonexistent", []string{"echo", "hello"})
	if err == nil {
		t.Fatal("Exec on non-existent VM should return error")
	}
	if !errors.Is(err, backend.ErrVMNotFound) {
		t.Errorf("error = %v, want ErrVMNotFound", err)
	}
}

// --- REQ-003-002: Backend Availability ---

func TestLimaBackend_AvailableWithMock(t *testing.T) {
	b := newTestBackend(t)

	// Mock-backed Lima should always report available
	if err := b.Available(); err != nil {
		t.Errorf("Available with mock executor should return nil, got: %v", err)
	}
}

// --- REQ-003-022: Context Cancellation ---

func TestLimaContext_Cancellation(t *testing.T) {
	b := newTestBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	cfg := backend.VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}

	err := b.Create(ctx, "cancelled", cfg)
	if err == nil {
		t.Error("Create with cancelled context should return error")
	}
}

// --- Property-based tests ---

// TestLimaLifecycle_MultipleVMsIsolated verifies multiple VMs are independent.
func TestLimaLifecycle_MultipleVMsIsolated(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	cfg := backend.VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}

	// Create two VMs
	if err := b.Create(ctx, "alpha", cfg); err != nil {
		t.Fatalf("Create alpha failed: %v", err)
	}
	if err := b.Create(ctx, "beta", cfg); err != nil {
		t.Fatalf("Create beta failed: %v", err)
	}

	// Start only alpha
	if err := b.Start(ctx, "alpha"); err != nil {
		t.Fatalf("Start alpha failed: %v", err)
	}

	// Verify alpha is running, beta is stopped
	alphaStatus, _ := b.Status(ctx, "alpha")
	betaStatus, _ := b.Status(ctx, "beta")

	if alphaStatus != backend.StatusRunning {
		t.Errorf("alpha status = %q, want running", alphaStatus)
	}
	if betaStatus != backend.StatusStopped {
		t.Errorf("beta status = %q, want stopped", betaStatus)
	}

	// Destroy alpha, beta should still exist
	if err := b.Destroy(ctx, "alpha"); err != nil {
		t.Fatalf("Destroy alpha failed: %v", err)
	}

	betaStatus, err := b.Status(ctx, "beta")
	if err != nil {
		t.Fatalf("Status beta after alpha destroy failed: %v", err)
	}
	if betaStatus != backend.StatusStopped {
		t.Errorf("beta status after alpha destroy = %q, want stopped", betaStatus)
	}
}

// TestLimaExec_CommandsAreIndependent verifies exec on different VMs don't interfere.
func TestLimaExec_CommandsAreIndependent(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	cfg := backend.VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}

	if err := b.Create(ctx, "vm1", cfg); err != nil {
		t.Fatalf("Create vm1 failed: %v", err)
	}
	if err := b.Create(ctx, "vm2", cfg); err != nil {
		t.Fatalf("Create vm2 failed: %v", err)
	}
	if err := b.Start(ctx, "vm1"); err != nil {
		t.Fatalf("Start vm1 failed: %v", err)
	}
	if err := b.Start(ctx, "vm2"); err != nil {
		t.Fatalf("Start vm2 failed: %v", err)
	}

	r1, err := b.Exec(ctx, "vm1", []string{"echo", "from-vm1"})
	if err != nil {
		t.Fatalf("Exec vm1 failed: %v", err)
	}

	r2, err := b.Exec(ctx, "vm2", []string{"echo", "from-vm2"})
	if err != nil {
		t.Fatalf("Exec vm2 failed: %v", err)
	}

	if r1.ExitCode != 0 || r2.ExitCode != 0 {
		t.Errorf("exit codes: vm1=%d vm2=%d, both want 0", r1.ExitCode, r2.ExitCode)
	}
}
