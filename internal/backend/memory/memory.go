// Package memory provides an in-memory implementation of backend.Backend for testing.
// REQ-008-001: In-Memory Backend — Core Implementation
// REQ-008-002: In-Memory Backend — State Machine Fidelity
// REQ-008-003: In-Memory Backend — SSHConfig
// REQ-008-004: In-Memory Backend — Exec
// REQ-008-004a: In-Memory Backend — Error Injection
// REQ-008-005: In-Memory Backend — List and VMInfo
// REQ-008-006: In-Memory Backend — Optional Interfaces (Snapshotter)
// REQ-008-007: In-Memory Backend — Reset
// REQ-008-008: In-Memory Backend — Context Cancellation
//
// The memory backend MUST NOT register itself via init(). Tests instantiate it
// via memory.New() and inject it through getBackendFunc or direct usage.
package memory

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
	"time"

	"sd/internal/backend"
)

// validMethods lists the method names accepted by SetMethodError.
// REQ-008-004a
var validMethods = map[string]bool{
	"available":  true,
	"create":     true,
	"start":      true,
	"stop":       true,
	"destroy":    true,
	"status":     true,
	"list":       true,
	"sshconfig":  true,
	"exec":       true,
	"snapshotcreate": true,
	"snapshotapply":  true,
	"snapshotdelete": true,
	"snapshotlist":   true,
}

// vmState holds the internal state of a single VM.
type vmState struct {
	name      string
	status    backend.VMStatus
	config    backend.VMConfig
	createdAt time.Time
	ip        string
}

// snapshotState captures VM state at a point in time for snapshot operations.
// REQ-008-006
type snapshotState struct {
	status    backend.VMStatus
	config    backend.VMConfig
	createdAt time.Time
	takenAt   time.Time
}

// Backend is an in-memory implementation of backend.Backend for testing.
// REQ-008-001
type Backend struct {
	mu           sync.RWMutex
	vms          map[string]*vmState
	snapshots    map[string]map[string]*snapshotState // vm name -> tag -> state
	execHandler  func(ctx context.Context, name string, command []string) (backend.ExecResult, error)
	methodErrors map[string]error // method name -> injected error
	nextIP       int              // next IP suffix (starts at 2, wraps at 254)
}

// New creates a new in-memory backend in its initial empty state.
// REQ-008-001
func New() *Backend {
	return &Backend{
		vms:          make(map[string]*vmState),
		snapshots:    make(map[string]map[string]*snapshotState),
		methodErrors: make(map[string]error),
		nextIP:       2,
	}
}

// Reset clears all state, returning the backend to its initial empty condition.
// REQ-008-007
func (b *Backend) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.vms = make(map[string]*vmState)
	b.snapshots = make(map[string]map[string]*snapshotState)
	b.execHandler = nil
	b.methodErrors = make(map[string]error)
	b.nextIP = 2
}

// SetExecHandler registers a custom handler for Exec calls.
// When set, the handler receives the context, VM name, and command, and its
// return value is used instead of the default empty ExecResult.
// REQ-008-004
func (b *Backend) SetExecHandler(h func(ctx context.Context, name string, command []string) (backend.ExecResult, error)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.execHandler = h
}

// SetStatus forces a VM into any state (including StatusError) for testing
// error-state transitions.
// REQ-008-002
func (b *Backend) SetStatus(name string, status backend.VMStatus) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	vm, ok := b.vms[name]
	if !ok {
		return backend.ErrVMNotFound
	}
	vm.status = status
	return nil
}

// SetMethodError configures a specific Backend method to return the given error
// unconditionally. Valid method names: "available", "create", "start", "stop",
// "destroy", "status", "list", "sshconfig", "exec", "snapshotcreate",
// "snapshotapply", "snapshotdelete", "snapshotlist".
// Passing nil as the error clears the injection for that method.
// REQ-008-004a
func (b *Backend) SetMethodError(method string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !validMethods[method] {
		panic(fmt.Sprintf("memory.SetMethodError: invalid method %q", method))
	}
	if err == nil {
		delete(b.methodErrors, method)
	} else {
		b.methodErrors[method] = err
	}
}

// checkCtx returns an error if the context is already cancelled.
// Called at the start of each context-accepting method, before acquiring the lock.
// REQ-008-008
func checkCtx(ctx context.Context) error {
	return ctx.Err()
}

// methodError returns the injected error for a method, or nil. Caller must hold at least RLock.
func (b *Backend) methodError(method string) error {
	return b.methodErrors[method]
}

// allocateIP returns the next sequential IP address and advances the counter.
// IPs are "192.168.100.N" where N starts at 2 and wraps at 254.
// Caller must hold the write lock.
// REQ-008-005
func (b *Backend) allocateIP() string {
	ip := fmt.Sprintf("192.168.100.%d", b.nextIP)
	b.nextIP++
	if b.nextIP > 254 {
		b.nextIP = 2
	}
	return ip
}

// portFromName returns a deterministic port number derived from the VM name
// using FNV-1a hash. The same name always produces the same port.
// REQ-008-003
func portFromName(name string) int {
	h := fnv.New32a()
	h.Write([]byte(name))
	return 10000 + int(h.Sum32()%50000)
}

// --- Backend interface methods ---

// Name returns "memory".
// REQ-008-001
func (b *Backend) Name() string {
	return "memory"
}

// Available always returns nil (no prerequisites).
// REQ-008-001
func (b *Backend) Available() error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if err := b.methodError("available"); err != nil {
		return err
	}
	return nil
}

// Create provisions a new VM with the given name and configuration.
// The VM transitions to StatusRunning (matching Lima backend behavior).
// REQ-008-001, REQ-008-002
func (b *Backend) Create(ctx context.Context, name string, cfg backend.VMConfig) error {
	if err := checkCtx(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.methodError("create"); err != nil {
		return err
	}
	// REQ-008-001: VM name validation
	if err := backend.ValidateVMName(name); err != nil {
		return err
	}
	if _, exists := b.vms[name]; exists {
		return backend.ErrVMAlreadyExists
	}
	b.vms[name] = &vmState{
		name:      name,
		status:    backend.StatusRunning,
		config:    cfg,
		createdAt: time.Now(),
		ip:        b.allocateIP(),
	}
	return nil
}

// Start boots a stopped VM. Starting a running VM is a no-op.
// REQ-008-002
func (b *Backend) Start(ctx context.Context, name string) error {
	if err := checkCtx(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.methodError("start"); err != nil {
		return err
	}
	vm, ok := b.vms[name]
	if !ok {
		return backend.ErrVMNotFound
	}
	switch vm.status {
	case backend.StatusRunning:
		// No-op: already running
		return nil
	case backend.StatusStopped:
		vm.status = backend.StatusRunning
		return nil
	default:
		return fmt.Errorf("cannot start VM %q in state %q: %w", name, vm.status, backend.ErrVMNotRunning)
	}
}

// Stop shuts down a running VM. Stopping a stopped VM is a no-op.
// Stop on Error transitions to Stopped.
// REQ-008-002
func (b *Backend) Stop(ctx context.Context, name string) error {
	if err := checkCtx(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.methodError("stop"); err != nil {
		return err
	}
	vm, ok := b.vms[name]
	if !ok {
		return backend.ErrVMNotFound
	}
	switch vm.status {
	case backend.StatusStopped:
		// No-op: already stopped
		return nil
	case backend.StatusRunning, backend.StatusError:
		vm.status = backend.StatusStopped
		return nil
	default:
		return fmt.Errorf("cannot stop VM %q in state %q", name, vm.status)
	}
}

// Destroy removes a VM and all its associated resources from any state.
// REQ-008-002
func (b *Backend) Destroy(ctx context.Context, name string) error {
	if err := checkCtx(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.methodError("destroy"); err != nil {
		return err
	}
	if _, ok := b.vms[name]; !ok {
		return backend.ErrVMNotFound
	}
	delete(b.vms, name)
	delete(b.snapshots, name)
	return nil
}

// Status returns the current status of a named VM.
// REQ-008-002
func (b *Backend) Status(ctx context.Context, name string) (backend.VMStatus, error) {
	if err := checkCtx(ctx); err != nil {
		return "", err
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if err := b.methodError("status"); err != nil {
		return "", err
	}
	vm, ok := b.vms[name]
	if !ok {
		return "", backend.ErrVMNotFound
	}
	return vm.status, nil
}

// List returns all VMs managed by this backend, sorted by name.
// Returns an empty slice (not nil) when no VMs exist.
// REQ-008-005
func (b *Backend) List(ctx context.Context) ([]backend.VMInfo, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if err := b.methodError("list"); err != nil {
		return nil, err
	}
	result := make([]backend.VMInfo, 0, len(b.vms))
	for _, vm := range b.vms {
		result = append(result, backend.VMInfo{
			Name:      vm.name,
			Status:    vm.status,
			Backend:   "memory",
			CPUs:      vm.config.CPUs,
			Memory:    vm.config.Memory,
			Disk:      vm.config.Disk,
			IP:        vm.ip,
			CreatedAt: vm.createdAt,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// SSHConfig returns SSH connection details for a running VM.
// REQ-008-003
func (b *Backend) SSHConfig(ctx context.Context, name string) (backend.SSHConfig, error) {
	if err := checkCtx(ctx); err != nil {
		return backend.SSHConfig{}, err
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if err := b.methodError("sshconfig"); err != nil {
		return backend.SSHConfig{}, err
	}
	vm, ok := b.vms[name]
	if !ok {
		return backend.SSHConfig{}, backend.ErrVMNotFound
	}
	if vm.status != backend.StatusRunning {
		return backend.SSHConfig{}, backend.ErrVMNotRunning
	}
	return backend.SSHConfig{
		Host:         "127.0.0.1",
		Port:         portFromName(name),
		User:         "ubuntu",
		IdentityFile: "/dev/null",
		Transport:    "tcp",
	}, nil
}

// Exec runs a command inside the named VM and returns the result.
// Returns the default empty ExecResult (exit code 0) unless a custom handler
// is set via SetExecHandler.
// REQ-008-004
func (b *Backend) Exec(ctx context.Context, name string, command []string) (backend.ExecResult, error) {
	if err := checkCtx(ctx); err != nil {
		return backend.ExecResult{}, err
	}
	b.mu.RLock()
	injectedErr := b.methodError("exec")
	vm, ok := b.vms[name]
	handler := b.execHandler
	b.mu.RUnlock()

	if injectedErr != nil {
		return backend.ExecResult{}, injectedErr
	}
	if !ok {
		return backend.ExecResult{}, backend.ErrVMNotFound
	}
	if vm.status != backend.StatusRunning {
		return backend.ExecResult{}, backend.ErrVMNotRunning
	}
	if handler != nil {
		return handler(ctx, name, command)
	}
	return backend.ExecResult{Stdout: "", Stderr: "", ExitCode: 0}, nil
}

// --- Snapshotter interface methods ---
// REQ-008-006

// SnapshotCreate captures the full vmState (status, config, createdAt) under a tag name.
func (b *Backend) SnapshotCreate(ctx context.Context, name, tag string) error {
	if err := checkCtx(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.methodError("snapshotcreate"); err != nil {
		return err
	}
	vm, ok := b.vms[name]
	if !ok {
		return backend.ErrVMNotFound
	}
	if _, ok := b.snapshots[name]; !ok {
		b.snapshots[name] = make(map[string]*snapshotState)
	}
	b.snapshots[name][tag] = &snapshotState{
		status:    vm.status,
		config:    vm.config,
		createdAt: vm.createdAt,
		takenAt:   time.Now(),
	}
	return nil
}

// SnapshotApply restores a VM to a previously saved snapshot.
func (b *Backend) SnapshotApply(ctx context.Context, name, tag string) error {
	if err := checkCtx(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.methodError("snapshotapply"); err != nil {
		return err
	}
	vm, ok := b.vms[name]
	if !ok {
		return backend.ErrVMNotFound
	}
	snaps, ok := b.snapshots[name]
	if !ok {
		return backend.ErrSnapshotNotFound
	}
	snap, ok := snaps[tag]
	if !ok {
		return backend.ErrSnapshotNotFound
	}
	vm.status = snap.status
	vm.config = snap.config
	vm.createdAt = snap.createdAt
	return nil
}

// SnapshotDelete removes a named snapshot.
func (b *Backend) SnapshotDelete(ctx context.Context, name, tag string) error {
	if err := checkCtx(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.methodError("snapshotdelete"); err != nil {
		return err
	}
	if _, ok := b.vms[name]; !ok {
		return backend.ErrVMNotFound
	}
	snaps, ok := b.snapshots[name]
	if !ok {
		return backend.ErrSnapshotNotFound
	}
	if _, ok := snaps[tag]; !ok {
		return backend.ErrSnapshotNotFound
	}
	delete(snaps, tag)
	if len(snaps) == 0 {
		delete(b.snapshots, name)
	}
	return nil
}

// SnapshotList returns all snapshots for a given VM.
func (b *Backend) SnapshotList(ctx context.Context, name string) ([]backend.SnapshotInfo, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if err := b.methodError("snapshotlist"); err != nil {
		return nil, err
	}
	if _, ok := b.vms[name]; !ok {
		return nil, backend.ErrVMNotFound
	}
	snaps, ok := b.snapshots[name]
	if !ok {
		return []backend.SnapshotInfo{}, nil
	}
	result := make([]backend.SnapshotInfo, 0, len(snaps))
	for tag, snap := range snaps {
		result = append(result, backend.SnapshotInfo{
			Name:      tag,
			CreatedAt: snap.takenAt,
			Size:      0, // In-memory backend has no real size
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// --- Compile-time interface checks ---

var _ backend.Backend = (*Backend)(nil)
var _ backend.Snapshotter = (*Backend)(nil)
