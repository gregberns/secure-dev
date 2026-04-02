// Package docker provides a stub implementation of the Docker (Apple Virtualization Framework) backend.
// REQ-001-005, REQ-003-020: Future backend stubs.
package docker

import (
	"context"
	"fmt"

	"sd/internal/backend"
)

func init() {
	backend.Register("docker", &dockerBackend{})
}

type dockerBackend struct{}

func (b *dockerBackend) Name() string { return "docker" }

func (b *dockerBackend) Available() error {
	return fmt.Errorf("Docker backend is not yet implemented: %w", backend.ErrBackendNotAvailable)
}

func (b *dockerBackend) Create(_ context.Context, _ string, _ backend.VMConfig) error {
	return backend.ErrNotImplemented
}

func (b *dockerBackend) Start(_ context.Context, _ string) error {
	return backend.ErrNotImplemented
}

func (b *dockerBackend) Stop(_ context.Context, _ string) error {
	return backend.ErrNotImplemented
}

func (b *dockerBackend) Destroy(_ context.Context, _ string) error {
	return backend.ErrNotImplemented
}

func (b *dockerBackend) Status(_ context.Context, _ string) (backend.VMStatus, error) {
	return "", backend.ErrNotImplemented
}

func (b *dockerBackend) List(_ context.Context) ([]backend.VMInfo, error) {
	return nil, backend.ErrNotImplemented
}

func (b *dockerBackend) SSHConfig(_ context.Context, _ string) (backend.SSHConfig, error) {
	return backend.SSHConfig{}, backend.ErrNotImplemented
}

func (b *dockerBackend) Exec(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
	return backend.ExecResult{}, backend.ErrNotImplemented
}
