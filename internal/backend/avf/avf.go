// Package avf provides a stub implementation of the AVF (Apple Virtualization Framework) backend.
// REQ-001-005, REQ-003-020: Future backend stubs.
package avf

import (
	"context"
	"fmt"

	"sd/internal/backend"
)

func init() {
	backend.Register("avf", &avfBackend{})
}

type avfBackend struct{}

func (b *avfBackend) Name() string { return "avf" }

func (b *avfBackend) Available() error {
	return fmt.Errorf("AVF backend is not yet implemented: %w", backend.ErrBackendNotAvailable)
}

func (b *avfBackend) Create(_ context.Context, _ string, _ backend.VMConfig) error {
	return backend.ErrNotImplemented
}

func (b *avfBackend) Start(_ context.Context, _ string) error {
	return backend.ErrNotImplemented
}

func (b *avfBackend) Stop(_ context.Context, _ string) error {
	return backend.ErrNotImplemented
}

func (b *avfBackend) Destroy(_ context.Context, _ string) error {
	return backend.ErrNotImplemented
}

func (b *avfBackend) Status(_ context.Context, _ string) (backend.VMStatus, error) {
	return "", backend.ErrNotImplemented
}

func (b *avfBackend) List(_ context.Context) ([]backend.VMInfo, error) {
	return nil, backend.ErrNotImplemented
}

func (b *avfBackend) SSHConfig(_ context.Context, _ string) (backend.SSHConfig, error) {
	return backend.SSHConfig{}, backend.ErrNotImplemented
}

func (b *avfBackend) Exec(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
	return backend.ExecResult{}, backend.ErrNotImplemented
}
