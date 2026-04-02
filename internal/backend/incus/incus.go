// Package incus provides a stub implementation of the Incus (Apple Virtualization Framework) backend.
// REQ-001-005, REQ-003-020: Future backend stubs.
package incus

import (
	"context"
	"fmt"

	"sd/internal/backend"
)

func init() {
	backend.Register("incus", &incusBackend{})
}

type incusBackend struct{}

func (b *incusBackend) Name() string { return "incus" }

func (b *incusBackend) Available() error {
	return fmt.Errorf("Incus backend is not yet implemented: %w", backend.ErrBackendNotAvailable)
}

func (b *incusBackend) Create(_ context.Context, _ string, _ backend.VMConfig) error {
	return backend.ErrNotImplemented
}

func (b *incusBackend) Start(_ context.Context, _ string) error {
	return backend.ErrNotImplemented
}

func (b *incusBackend) Stop(_ context.Context, _ string) error {
	return backend.ErrNotImplemented
}

func (b *incusBackend) Destroy(_ context.Context, _ string) error {
	return backend.ErrNotImplemented
}

func (b *incusBackend) Status(_ context.Context, _ string) (backend.VMStatus, error) {
	return "", backend.ErrNotImplemented
}

func (b *incusBackend) List(_ context.Context) ([]backend.VMInfo, error) {
	return nil, backend.ErrNotImplemented
}

func (b *incusBackend) SSHConfig(_ context.Context, _ string) (backend.SSHConfig, error) {
	return backend.SSHConfig{}, backend.ErrNotImplemented
}

func (b *incusBackend) Exec(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
	return backend.ExecResult{}, backend.ErrNotImplemented
}
