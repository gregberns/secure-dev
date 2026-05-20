// Package docker provides a Docker container backend for sd.
// REQ-001-005, REQ-003-020, REQ-008-009, REQ-008-014: Docker backend.
package docker

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"sd/internal/backend"
)

//go:embed Dockerfile
var dockerfile []byte

const imageName = "sd-test:latest"

// containerPrefix is prepended to VM names for Docker container naming.
const containerPrefix = "sd-"

// Compile-time interface check.
var _ backend.Backend = (*Backend)(nil)

func init() {
	backend.Register("docker", &Backend{})
}

// Backend implements backend.Backend using Docker containers.
// REQ-003-020: Docker backend.
type Backend struct {
	mu sync.Mutex
}

// Name returns the backend's registered name.
func (b *Backend) Name() string { return "docker" }

// Available returns nil if the docker CLI is in PATH and the daemon is running.
// REQ-003-002
func (b *Backend) Available() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found in $PATH: %w: install Docker Desktop or Docker Engine",
			backend.ErrBackendNotAvailable)
	}
	// Check that the daemon is responding.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := dockerCmd(ctx, "info"); err != nil {
		return fmt.Errorf("docker daemon not running: %w: %w",
			backend.ErrBackendNotAvailable, err)
	}
	return nil
}

// containerName returns the Docker container name for a given VM name.
func containerName(name string) string {
	return containerPrefix + name
}

// Create provisions a new Docker container with SSH access.
// REQ-003-003, REQ-008-009
func (b *Backend) Create(ctx context.Context, name string, cfg backend.VMConfig) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Validate name.
	if err := backend.ValidateVMName(name); err != nil {
		return err
	}

	// Check context.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("create cancelled for %q: %w", name, err)
	}

	// Ensure the Docker image is built.
	if err := ensureImage(ctx); err != nil {
		return fmt.Errorf("create %q: %w", name, err)
	}

	cName := containerName(name)

	// Check for existing container.
	if _, err := dockerCmd(ctx, "inspect", cName); err == nil {
		return fmt.Errorf("container %q already exists: %w", cName, backend.ErrVMAlreadyExists)
	}

	// Create the container with port mapping for SSH.
	if _, err := dockerCmd(ctx, "create", "--name", cName, "-p", "0:22", imageName); err != nil {
		return fmt.Errorf("create container %q: %w", cName, err)
	}

	// Start the container immediately.
	if _, err := dockerCmd(ctx, "start", cName); err != nil {
		// Clean up the created container on start failure.
		_, _ = dockerCmd(context.Background(), "rm", "-f", cName)
		return fmt.Errorf("start container %q after create: %w", cName, err)
	}

	// Generate SSH ed25519 keypair using ssh-keygen (from ssh.go).
	keyDir := sshKeyDir(name)
	keyPath, err := generateSSHKeys(ctx, keyDir)
	if err != nil {
		// Clean up container on SSH key failure.
		_, _ = dockerCmd(context.Background(), "rm", "-f", cName)
		return fmt.Errorf("generate ssh keys for %q: %w", name, err)
	}

	// Copy public key into the container (from ssh.go).
	pubKeyPath := keyPath + ".pub"
	if err := injectPublicKey(ctx, cName, pubKeyPath); err != nil {
		// Clean up on failure.
		_, _ = dockerCmd(context.Background(), "rm", "-f", cName)
		_ = os.RemoveAll(keyDir)
		return fmt.Errorf("inject ssh key for %q: %w", name, err)
	}

	// Get the mapped SSH port for readiness polling.
	sshCfg, err := getSSHConfig(ctx, cName, name, keyPath)
	if err != nil {
		return fmt.Errorf("get ssh config for %q: %w", name, err)
	}

	// Poll for SSH readiness (max 30s) using ssh.go helper.
	if err := waitForSSH(ctx, sshCfg.Host, sshCfg.Port, 30*time.Second); err != nil {
		// Don't destroy the container on SSH timeout -- it may still be useful.
		return fmt.Errorf("ssh not ready for %q: %w", name, err)
	}

	return nil
}

// Start boots a stopped container.
// REQ-003-003
func (b *Backend) Start(ctx context.Context, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start cancelled for %q: %w", name, err)
	}

	cName := containerName(name)

	// Check container exists.
	status, err := inspectStatus(ctx, cName)
	if err != nil {
		return fmt.Errorf("vm %q: %w", name, backend.ErrVMNotFound)
	}

	// No-op if already running.
	if status == "running" {
		return nil
	}

	if _, err := dockerCmd(ctx, "start", cName); err != nil {
		return fmt.Errorf("start container %q: %w", cName, err)
	}
	return nil
}

// Stop shuts down a running container.
// REQ-003-003
func (b *Backend) Stop(ctx context.Context, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("stop cancelled for %q: %w", name, err)
	}

	cName := containerName(name)

	// Check container exists.
	status, err := inspectStatus(ctx, cName)
	if err != nil {
		return fmt.Errorf("vm %q: %w", name, backend.ErrVMNotFound)
	}

	// No-op if already stopped.
	if status == "exited" || status == "created" {
		return nil
	}

	if _, err := dockerCmd(ctx, "stop", cName); err != nil {
		return fmt.Errorf("stop container %q: %w", cName, err)
	}
	return nil
}

// Destroy removes a container and its associated SSH keys.
// REQ-003-003
func (b *Backend) Destroy(ctx context.Context, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("destroy cancelled for %q: %w", name, err)
	}

	cName := containerName(name)

	// Check container exists.
	if _, err := inspectStatus(ctx, cName); err != nil {
		return fmt.Errorf("vm %q: %w", name, backend.ErrVMNotFound)
	}

	// Force remove the container (works whether running or stopped).
	if _, err := dockerCmd(ctx, "rm", "-f", cName); err != nil {
		return fmt.Errorf("destroy container %q: %w", cName, err)
	}

	// Remove SSH keys.
	dir := sshKeyDir(name)
	if err := os.RemoveAll(dir); err != nil {
		// Non-fatal: log-worthy but don't fail the destroy.
		return fmt.Errorf("remove ssh keys for %q: %w", name, err)
	}

	return nil
}

// Status returns the current status of a named VM.
// REQ-003-004
func (b *Backend) Status(ctx context.Context, name string) (backend.VMStatus, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("status check cancelled for %q: %w", name, err)
	}

	cName := containerName(name)
	status, err := inspectStatus(ctx, cName)
	if err != nil {
		return "", fmt.Errorf("vm %q: %w", name, backend.ErrVMNotFound)
	}

	return mapDockerStatus(status), nil
}

// inspectStatus returns the raw Docker state status string for a container.
func inspectStatus(ctx context.Context, cName string) (string, error) {
	output, err := dockerCmd(ctx, "inspect", "--format", "{{.State.Status}}", cName)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// mapDockerStatus maps Docker container states to backend.VMStatus values.
func mapDockerStatus(dockerState string) backend.VMStatus {
	switch dockerState {
	case "running":
		return backend.StatusRunning
	case "exited", "created":
		return backend.StatusStopped
	default:
		return backend.StatusError
	}
}

// List returns all VMs managed by this backend (containers with sd- prefix).
// REQ-003-005
func (b *Backend) List(ctx context.Context) ([]backend.VMInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("list cancelled: %w", err)
	}

	// List all containers with the sd- prefix.
	output, err := dockerCmd(ctx, "ps", "-a", "--filter", "name=^sd-", "--format", "{{.Names}}")
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	if output == "" {
		return []backend.VMInfo{}, nil
	}

	names := strings.Split(output, "\n")
	var vms []backend.VMInfo

	for _, cName := range names {
		cName = strings.TrimSpace(cName)
		if cName == "" {
			continue
		}
		// Only include containers that match our prefix exactly.
		if !strings.HasPrefix(cName, containerPrefix) {
			continue
		}

		vmName := strings.TrimPrefix(cName, containerPrefix)

		// Get status for each container.
		status, err := inspectStatus(ctx, cName)
		if err != nil {
			continue
		}

		// Get creation time.
		createdStr, _ := dockerCmd(ctx, "inspect", "--format", "{{.Created}}", cName)
		createdAt, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(createdStr))
		var createdPtr *time.Time
		if !createdAt.IsZero() { tUTC := createdAt.UTC(); createdPtr = &tUTC }

		vms = append(vms, backend.VMInfo{
			Name:      vmName,
			Status:    mapDockerStatus(status),
			Backend:   "docker",
			CreatedAt: createdPtr,
		})
	}

	sort.Slice(vms, func(i, j int) bool {
		return vms[i].Name < vms[j].Name
	})
	return vms, nil
}

// SSHConfig returns SSH connection details for a running container.
// REQ-003-006
func (b *Backend) SSHConfig(ctx context.Context, name string) (backend.SSHConfig, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return backend.SSHConfig{}, fmt.Errorf("sshconfig cancelled for %q: %w", name, err)
	}

	cName := containerName(name)

	// Check container exists and is running.
	status, err := inspectStatus(ctx, cName)
	if err != nil {
		return backend.SSHConfig{}, fmt.Errorf("vm %q: %w", name, backend.ErrVMNotFound)
	}
	if status != "running" {
		return backend.SSHConfig{}, fmt.Errorf("vm %q is %s: %w", name, status, backend.ErrVMNotRunning)
	}

	// Delegate to ssh.go helper which queries docker port and builds the config.
	keyDir := sshKeyDir(name)
	keyPath := keyDir + "/id_ed25519"
	return getSSHConfig(ctx, cName, name, keyPath)
}

// Exec runs a command inside the named container via SSH.
// Falls back to docker exec if SSH is not available.
// REQ-003-007
func (b *Backend) Exec(ctx context.Context, name string, command []string) (backend.ExecResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return backend.ExecResult{}, fmt.Errorf("exec cancelled for %q: %w", name, err)
	}

	cName := containerName(name)

	// Check container exists and is running.
	status, err := inspectStatus(ctx, cName)
	if err != nil {
		return backend.ExecResult{}, fmt.Errorf("vm %q: %w", name, backend.ErrVMNotFound)
	}
	if status != "running" {
		return backend.ExecResult{}, fmt.Errorf("vm %q is %s: %w", name, status, backend.ErrVMNotRunning)
	}

	// Try SSH-based execution first (from ssh.go) for better fidelity.
	keyDir := sshKeyDir(name)
	keyPath := keyDir + "/id_ed25519"
	sshCfg, err := getSSHConfig(ctx, cName, name, keyPath)
	if err == nil {
		return execViaSSH(ctx, sshCfg, command)
	}

	// Fallback: use docker exec directly.
	return execViaDockerExec(ctx, cName, command)
}

// execViaDockerExec runs a command inside the container using docker exec as ubuntu user.
func execViaDockerExec(ctx context.Context, cName string, command []string) (backend.ExecResult, error) {
	args := append([]string{"exec", "-u", "ubuntu", cName}, command...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := backend.ExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			// Non-exit error (e.g. context cancelled, command not found).
			return result, fmt.Errorf("docker exec in %q: %w", cName, err)
		}
	}

	return result, nil
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
