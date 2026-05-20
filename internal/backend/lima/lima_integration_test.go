//go:build integration_lima

// Package lima — integration tests that exercise the real `limactl` binary.
//
// These tests are gated by the `integration_lima` build tag because they
// shell out to a host-installed limactl (and may create/start/stop/destroy
// real VMs). They are intentionally excluded from the default `go test ./...`
// run so that the suite stays fast and green on machines without Lima.
//
// To run these tests:
//
//	go test -tags=integration_lima ./internal/backend/lima/...
//
// Prerequisites: limactl must be installed (`brew install lima` on macOS) and
// functional. Tests may take several minutes and can hang if Lima is
// misconfigured.
package lima

import (
	"context"
	"testing"

	"sd/internal/backend"
)

// TestLimaBackend_InterfaceContract verifies Lima backend implements Backend
// and that each method can be invoked against the real limactl executor.
// REQ-003-001, REQ-003-014
func TestLimaBackend_InterfaceContract(t *testing.T) {
	b := New()

	// Verify it satisfies the Backend interface
	var _ backend.Backend = b

	// Verify it can be registered
	ctx := context.Background()

	// These calls verify methods exist (they will return ErrNotImplemented
	// or a real error from limactl, depending on host state).
	cfg := backend.VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}

	b.Create(ctx, "test", cfg)
	b.Start(ctx, "test")
	b.Stop(ctx, "test")
	b.Destroy(ctx, "test")
	b.Status(ctx, "test")
	b.List(ctx)
	b.SSHConfig(ctx, "test")
	b.Exec(ctx, "test", []string{"test"})
}
