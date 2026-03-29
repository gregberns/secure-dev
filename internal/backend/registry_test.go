// Package backend tests the backend registry.
// REQ-003-013: Backend Registry
package backend

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// TestBackendRegistry_Register verifies that backends can be registered.
// REQ-003-013
func TestBackendRegistry_Register(t *testing.T) {
	// Clear any existing backends
	ResetRegistry()

	// Register a test backend
	b := &mockBackend{name: "test"}
	Register("test", b)

	// Verify it was registered
	got, err := Get("test")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got != b {
		t.Errorf("got backend %p, want %p", got, b)
	}
}

// TestBackendRegistry_DuplicateRegistration panics.
// REQ-003-013: Duplicate registration should panic
func TestBackendRegistry_DuplicateRegistration(t *testing.T) {
	ResetRegistry()

	b1 := &mockBackend{name: "test1"}
	b2 := &mockBackend{name: "test2"}

	Register("test", b1)

	// This should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate registration, got nil")
		}
	}()

	Register("test", b2)
}

// TestBackendRegistry_List returns sorted backend names.
// REQ-003-013
func TestBackendRegistry_List(t *testing.T) {
	ResetRegistry()

	Register("zebra", &mockBackend{})
	Register("alpha", &mockBackend{})
	Register("middle", &mockBackend{})

	got := List()
	want := []string{"alpha", "middle", "zebra"}

	if len(got) != len(want) {
		t.Fatalf("got %d backends, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("List[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestBackendRegistry_ListEmpty returns empty slice when no backends.
// REQ-003-005: List returns empty slice, not nil
func TestBackendRegistry_ListEmpty(t *testing.T) {
	ResetRegistry()

	got := List()
	if got == nil {
		t.Fatal("List returned nil, want empty slice")
	}
	if len(got) != 0 {
		t.Errorf("List returned %d items, want 0", len(got))
	}
}

// TestBackendRegistry_GetUnknownBackend returns error.
// REQ-003-013
func TestBackendRegistry_GetUnknownBackend(t *testing.T) {
	ResetRegistry()

	_, err := Get("nonexistent")
	if err == nil {
		t.Error("Get returned nil error, want ErrBackendNotAvailable")
	}

	// Check error wraps ErrBackendNotAvailable
	if !errors.Is(err, ErrBackendNotAvailable) {
		t.Errorf("error does not wrap ErrBackendNotAvailable: %v", err)
	}
}

// TestBackendRegistry_DefaultReturnsAvailable.
// REQ-003-013: Default returns first available backend
func TestBackendRegistry_DefaultReturnsAvailable(t *testing.T) {
	ResetRegistry()

	available := &mockBackend{available: true}
	unavailable := &mockBackend{available: false}

	Register("lima", available)
	Register("docker", unavailable)

	got, err := Default()
	if err != nil {
		t.Fatalf("Default failed: %v", err)
	}
	if got != available {
		t.Errorf("got backend %p, want %p", got, available)
	}
}

// TestBackendRegistry_DefaultPrefersLima.
// REQ-003-013: Default prefers "lima" if available
func TestBackendRegistry_DefaultPrefersLima(t *testing.T) {
	ResetRegistry()

	lima := &mockBackend{name: "lima", available: true}
	other := &mockBackend{name: "other", available: true}

	Register("other", other)
	Register("lima", lima)

	got, err := Default()
	if err != nil {
		t.Fatalf("Default failed: %v", err)
	}
	if got != lima {
		t.Errorf("got backend %p, want lima", got)
	}
}

// TestBackendRegistry_DefaultFailsWhenNoneAvailable.
// REQ-003-013
func TestBackendRegistry_DefaultFailsWhenNoneAvailable(t *testing.T) {
	ResetRegistry()

	unavailable := &mockBackend{available: false}
	Register("test", unavailable)

	_, err := Default()
	if err == nil {
		t.Error("Default returned nil error, want ErrBackendNotAvailable")
	}
	if !errors.Is(err, ErrBackendNotAvailable) {
		t.Errorf("error does not wrap ErrBackendNotAvailable: %v", err)
	}
}

// TestBackendRegistry_ConcurrentAccess is thread-safe.
// Property: The registry must be safe for concurrent access
func TestBackendRegistry_ConcurrentAccess(t *testing.T) {
	ResetRegistry()

	// Test concurrent registration with unique names
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := fmt.Sprintf("backend-%d", n)
			Register(name, &mockBackend{name: name})
		}(i)
	}
	wg.Wait()

	// Test concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			List()
			Get("a")
			Default()
		}()
	}
	wg.Wait()
}

// TestMustImplementSnapshotter checks capability.
// REQ-003-008: Callers must use type assertion
func TestMustImplementSnapshotter(t *testing.T) {
	// mockBackend always implements all capabilities
	err := MustImplementSnapshotter(&mockBackend{})
	if err != nil {
		t.Errorf("MustImplementSnapshotter returned error for mockBackend: %v", err)
	}

	// mockBackendNoSnapshotter doesn't implement Snapshotter
	err = MustImplementSnapshotter(&mockBackendNoSnapshotter{})
	if err == nil {
		t.Error("MustImplementSnapshotter returned nil error for mockBackendNoSnapshotter, want error")
	}
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("error does not wrap ErrNotImplemented: %v", err)
	}
}

// TestMustImplementCloner checks capability.
// REQ-003-009: Callers must use type assertion
func TestMustImplementCloner(t *testing.T) {
	// mockBackend always implements all capabilities
	err := MustImplementCloner(&mockBackend{})
	if err != nil {
		t.Errorf("MustImplementCloner returned error for mockBackend: %v", err)
	}

	// mockBackendNoCloner doesn't implement Cloner
	err = MustImplementCloner(&mockBackendNoCloner{})
	if err == nil {
		t.Error("MustImplementCloner returned nil error for mockBackendNoCloner, want error")
	}
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("error does not wrap ErrNotImplemented: %v", err)
	}
}

// TestMustImplementSyncer checks capability.
// REQ-003-010: Callers must use type assertion
func TestMustImplementSyncer(t *testing.T) {
	// mockBackend always implements all capabilities
	err := MustImplementSyncer(&mockBackend{})
	if err != nil {
		t.Errorf("MustImplementSyncer returned error for mockBackend: %v", err)
	}

	// mockBackendNoSyncer doesn't implement Syncer
	err = MustImplementSyncer(&mockBackendNoSyncer{})
	if err == nil {
		t.Error("MustImplementSyncer returned nil error for mockBackendNoSyncer, want error")
	}
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("error does not wrap ErrNotImplemented: %v", err)
	}
}

// mockBackend is a test backend implementation.
// It always satisfies all capability interfaces for testing.
type mockBackend struct {
	name      string
	available bool
}

func (m *mockBackend) Name() string                    { return m.name }
func (m *mockBackend) Available() error {
	if m.available {
		return nil
	}
	return ErrBackendNotAvailable
}
func (m *mockBackend) Create(ctx context.Context, name string, cfg VMConfig) error {
	return nil
}
func (m *mockBackend) Start(ctx context.Context, name string) error       { return nil }
func (m *mockBackend) Stop(ctx context.Context, name string) error        { return nil }
func (m *mockBackend) Destroy(ctx context.Context, name string) error     { return nil }
func (m *mockBackend) Status(ctx context.Context, name string) (VMStatus, error) {
	return StatusRunning, nil
}
func (m *mockBackend) List(ctx context.Context) ([]VMInfo, error) {
	return []VMInfo{}, nil
}
func (m *mockBackend) SSHConfig(ctx context.Context, name string) (SSHConfig, error) {
	return SSHConfig{}, nil
}
func (m *mockBackend) Exec(ctx context.Context, name string, command []string) (ExecResult, error) {
	return ExecResult{}, nil
}
func (m *mockBackend) SnapshotCreate(ctx context.Context, name, tag string) error {
	return nil
}
func (m *mockBackend) SnapshotApply(ctx context.Context, name, tag string) error {
	return nil
}
func (m *mockBackend) SnapshotDelete(ctx context.Context, name, tag string) error {
	return nil
}
func (m *mockBackend) SnapshotList(ctx context.Context, name string) ([]SnapshotInfo, error) {
	return []SnapshotInfo{}, nil
}
func (m *mockBackend) Clone(ctx context.Context, src, dst string) error {
	return nil
}
func (m *mockBackend) SyncTo(ctx context.Context, name, hostPath, guestPath string) error {
	return nil
}
func (m *mockBackend) SyncFrom(ctx context.Context, name, guestPath, hostPath string) error {
	return nil
}

// mockBackendNoSnapshotter doesn't implement Snapshotter.
type mockBackendNoSnapshotter struct {
	name string
}

func (m *mockBackendNoSnapshotter) Name() string                           { return "" }
func (m *mockBackendNoSnapshotter) Available() error                  { return nil }
func (m *mockBackendNoSnapshotter) Create(ctx context.Context, name string, cfg VMConfig) error {
	return ErrNotImplemented
}
func (m *mockBackendNoSnapshotter) Start(ctx context.Context, name string) error       { return ErrNotImplemented }
func (m *mockBackendNoSnapshotter) Stop(ctx context.Context, name string) error        { return ErrNotImplemented }
func (m *mockBackendNoSnapshotter) Destroy(ctx context.Context, name string) error     { return ErrNotImplemented }
func (m *mockBackendNoSnapshotter) Status(ctx context.Context, name string) (VMStatus, error) {
	return StatusRunning, ErrNotImplemented
}
func (m *mockBackendNoSnapshotter) List(ctx context.Context) ([]VMInfo, error) {
	return []VMInfo{}, ErrNotImplemented
}
func (m *mockBackendNoSnapshotter) SSHConfig(ctx context.Context, name string) (SSHConfig, error) {
	return SSHConfig{}, ErrNotImplemented
}
func (m *mockBackendNoSnapshotter) Exec(ctx context.Context, name string, command []string) (ExecResult, error) {
	return ExecResult{}, ErrNotImplemented
}
func (m *mockBackendNoSnapshotter) Clone(ctx context.Context, src, dst string) error {
	return ErrNotImplemented
}
func (m *mockBackendNoSnapshotter) SyncTo(ctx context.Context, name, hostPath, guestPath string) error {
	return ErrNotImplemented
}
func (m *mockBackendNoSnapshotter) SyncFrom(ctx context.Context, name, guestPath, hostPath string) error {
	return ErrNotImplemented
}

// mockBackendNoCloner doesn't implement Cloner.
type mockBackendNoCloner struct {
	name string
}

func (m *mockBackendNoCloner) Name() string                           { return "" }
func (m *mockBackendNoCloner) Available() error                  { return nil }
func (m *mockBackendNoCloner) Create(ctx context.Context, name string, cfg VMConfig) error {
	return ErrNotImplemented
}
func (m *mockBackendNoCloner) Start(ctx context.Context, name string) error       { return ErrNotImplemented }
func (m *mockBackendNoCloner) Stop(ctx context.Context, name string) error        { return ErrNotImplemented }
func (m *mockBackendNoCloner) Destroy(ctx context.Context, name string) error     { return ErrNotImplemented }
func (m *mockBackendNoCloner) Status(ctx context.Context, name string) (VMStatus, error) {
	return StatusRunning, ErrNotImplemented
}
func (m *mockBackendNoCloner) List(ctx context.Context) ([]VMInfo, error) {
	return []VMInfo{}, ErrNotImplemented
}
func (m *mockBackendNoCloner) SSHConfig(ctx context.Context, name string) (SSHConfig, error) {
	return SSHConfig{}, ErrNotImplemented
}
func (m *mockBackendNoCloner) Exec(ctx context.Context, name string, command []string) (ExecResult, error) {
	return ExecResult{}, ErrNotImplemented
}
func (m *mockBackendNoCloner) SnapshotCreate(ctx context.Context, name, tag string) error {
	return ErrNotImplemented
}
func (m *mockBackendNoCloner) SnapshotApply(ctx context.Context, name, tag string) error {
	return ErrNotImplemented
}
func (m *mockBackendNoCloner) SnapshotDelete(ctx context.Context, name, tag string) error {
	return ErrNotImplemented
}
func (m *mockBackendNoCloner) SnapshotList(ctx context.Context, name string) ([]SnapshotInfo, error) {
	return []SnapshotInfo{}, ErrNotImplemented
}
// Clone method intentionally omitted to NOT satisfy Cloner interface
func (m *mockBackendNoCloner) SyncTo(ctx context.Context, name, hostPath, guestPath string) error {
	return ErrNotImplemented
}
func (m *mockBackendNoCloner) SyncFrom(ctx context.Context, name, guestPath, hostPath string) error {
	return ErrNotImplemented
}

// mockBackendNoSyncer doesn't implement Syncer.
type mockBackendNoSyncer struct {
	name string
}

func (m *mockBackendNoSyncer) Name() string                           { return "" }
func (m *mockBackendNoSyncer) Available() error                  { return nil }
func (m *mockBackendNoSyncer) Create(ctx context.Context, name string, cfg VMConfig) error {
	return ErrNotImplemented
}
func (m *mockBackendNoSyncer) Start(ctx context.Context, name string) error       { return ErrNotImplemented }
func (m *mockBackendNoSyncer) Stop(ctx context.Context, name string) error        { return ErrNotImplemented }
func (m *mockBackendNoSyncer) Destroy(ctx context.Context, name string) error     { return ErrNotImplemented }
func (m *mockBackendNoSyncer) Status(ctx context.Context, name string) (VMStatus, error) {
	return StatusRunning, ErrNotImplemented
}
func (m *mockBackendNoSyncer) List(ctx context.Context) ([]VMInfo, error) {
	return []VMInfo{}, ErrNotImplemented
}
func (m *mockBackendNoSyncer) SSHConfig(ctx context.Context, name string) (SSHConfig, error) {
	return SSHConfig{}, ErrNotImplemented
}
func (m *mockBackendNoSyncer) Exec(ctx context.Context, name string, command []string) (ExecResult, error) {
	return ExecResult{}, ErrNotImplemented
}
func (m *mockBackendNoSyncer) SnapshotCreate(ctx context.Context, name, tag string) error {
	return ErrNotImplemented
}
func (m *mockBackendNoSyncer) SnapshotApply(ctx context.Context, name, tag string) error {
	return ErrNotImplemented
}
func (m *mockBackendNoSyncer) SnapshotDelete(ctx context.Context, name, tag string) error {
	return ErrNotImplemented
}
func (m *mockBackendNoSyncer) SnapshotList(ctx context.Context, name string) ([]SnapshotInfo, error) {
	return []SnapshotInfo{}, ErrNotImplemented
}
func (m *mockBackendNoSyncer) Clone(ctx context.Context, src, dst string) error {
	return ErrNotImplemented
}
// SyncTo and SyncFrom methods intentionally omitted to NOT satisfy Syncer interface


// Ensure mockBackend satisfies all interfaces
var _ Backend = (*mockBackend)(nil)
var (
	_ Snapshotter = (*mockBackend)(nil)
	_ Cloner      = (*mockBackend)(nil)
	_ Syncer      = (*mockBackend)(nil)
)

// ResetRegistry clears the global registry state for testing.
func ResetRegistry() {
	backends = make(map[string]Backend)
}
