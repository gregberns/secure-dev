// Package docker provides a Docker container backend for integration testing.
// REQ-001-005, REQ-003-020, REQ-008-009, REQ-008-014: Docker test backend.
package docker

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os/exec"
	"strings"

	"sd/internal/backend"
)

//go:embed Dockerfile
var dockerfile []byte

const imageName = "sd-test:latest"

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

// dockerCmd runs a docker CLI command and returns its stdout.
// On failure, the error wraps stderr content for diagnosis.
func dockerCmd(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// ensureImage checks whether the sd-test:latest image exists locally and
// builds it from the embedded Dockerfile if it does not.
// REQ-008-014: Container image built automatically on first Create.
func ensureImage(ctx context.Context) error {
	// Check if the image already exists.
	if _, err := dockerCmd(ctx, "image", "inspect", imageName); err == nil {
		return nil
	}

	// Build the image by piping the embedded Dockerfile via stdin.
	cmd := exec.CommandContext(ctx, "docker", "build", "-t", imageName, "-f-", ".")
	cmd.Stdin = bytes.NewReader(dockerfile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Clean up any partial image so a subsequent Create can retry.
		// Best-effort removal; ignore errors.
		_ = exec.CommandContext(ctx, "docker", "rmi", imageName).Run()
		return fmt.Errorf("building %s image: %w: %s", imageName, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
