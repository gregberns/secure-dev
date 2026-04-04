//go:build integration

package docker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"sd/internal/backend"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain handles suite-level setup and teardown:
//   - Sets SD_HOME to a temp directory
//   - Cleans up leaked sd- containers before and after the suite
func TestMain(m *testing.M) {
	// Check Docker availability before doing anything.
	b := &Backend{}
	if err := b.Available(); err != nil {
		fmt.Fprintf(os.Stderr, "skipping docker integration tests: %v\n", err)
		os.Exit(0)
	}

	// Create a temp SD_HOME so SSH keys don't pollute the real home.
	tmpHome, err := os.MkdirTemp("", "sd-docker-integration-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp SD_HOME: %v\n", err)
		os.Exit(1)
	}
	os.Setenv("SD_HOME", tmpHome)

	// Clean up leaked containers from previous failed runs.
	cleanupLeakedContainers()

	code := m.Run()

	// Post-suite cleanup.
	cleanupLeakedContainers()
	os.RemoveAll(tmpHome)

	os.Exit(code)
}

// cleanupLeakedContainers removes all Docker containers with the sd- prefix.
func cleanupLeakedContainers() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := dockerCmd(ctx, "ps", "-a", "--filter", "name=^sd-", "--format", "{{.Names}}")
	if err != nil {
		return
	}
	for _, name := range strings.Split(strings.TrimSpace(output), "\n") {
		name = strings.TrimSpace(name)
		if name == "" || !strings.HasPrefix(name, containerPrefix) {
			continue
		}
		_, _ = dockerCmd(ctx, "rm", "-f", name)
	}
}

// cleanupContainers is a test helper that removes all sd- prefixed containers.
func cleanupContainers(t *testing.T) {
	t.Helper()
	cleanupLeakedContainers()
}

// newTestBackend creates a Backend suitable for testing. The SD_HOME env var
// is already set by TestMain, so SSH keys go to a temp directory.
func newTestBackend(t *testing.T) *Backend {
	t.Helper()
	b := &Backend{}
	require.NoError(t, b.Available(), "docker must be available for integration tests")
	return b
}

// uniqueName returns a short, unique VM name for a test to avoid collisions.
func uniqueName(t *testing.T, suffix string) string {
	t.Helper()
	// Use a timestamp-based suffix for uniqueness across parallel runs.
	ts := time.Now().UnixNano() % 100000
	return fmt.Sprintf("t%d-%s", ts, suffix)
}

// TestDockerLifecycle exercises the full lifecycle:
// Create -> Status(Running) -> SSHConfig -> Exec("echo hello") ->
// Stop -> Status(Stopped) -> Start -> Status(Running) -> Destroy -> Status(ErrVMNotFound)
func TestDockerLifecycle(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	name := uniqueName(t, "life")
	cfg := backend.VMConfig{}

	// Ensure cleanup even if the test fails partway through.
	t.Cleanup(func() {
		_ = b.Destroy(context.Background(), name)
	})

	// 1. Create
	require.NoError(t, b.Create(ctx, name, cfg), "Create should succeed")

	// 2. Status should be Running (Docker Create starts the container).
	status, err := b.Status(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, backend.StatusRunning, status, "status after create should be running")

	// 3. SSHConfig should return valid fields.
	sshCfg, err := b.SSHConfig(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1", sshCfg.Host)
	assert.Greater(t, sshCfg.Port, 0, "SSH port must be positive")
	assert.Equal(t, "ubuntu", sshCfg.User)
	assert.NotEmpty(t, sshCfg.IdentityFile, "identity file must be set")
	assert.Equal(t, "tcp", sshCfg.Transport)

	// 4. Exec a command.
	result, err := b.Exec(ctx, name, []string{"echo", "hello"})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode, "exit code should be 0")
	assert.Contains(t, result.Stdout, "hello", "stdout should contain 'hello'")

	// 5. Stop.
	require.NoError(t, b.Stop(ctx, name), "Stop should succeed")

	// 6. Status should be Stopped.
	status, err = b.Status(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, backend.StatusStopped, status, "status after stop should be stopped")

	// 7. Start.
	require.NoError(t, b.Start(ctx, name), "Start should succeed")

	// 8. Status should be Running again.
	status, err = b.Status(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, backend.StatusRunning, status, "status after restart should be running")

	// 9. Destroy.
	require.NoError(t, b.Destroy(ctx, name), "Destroy should succeed")

	// 10. Status should return ErrVMNotFound.
	_, err = b.Status(ctx, name)
	assert.ErrorIs(t, err, backend.ErrVMNotFound, "status after destroy should be ErrVMNotFound")
}

// TestDockerSSHConnectivity verifies that the SSH port is reachable via TCP
// after container creation.
func TestDockerSSHConnectivity(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	name := uniqueName(t, "ssh")

	t.Cleanup(func() {
		_ = b.Destroy(context.Background(), name)
	})

	require.NoError(t, b.Create(ctx, name, backend.VMConfig{}))

	sshCfg, err := b.SSHConfig(ctx, name)
	require.NoError(t, err)

	// Verify the SSH port responds to a TCP connection.
	addr := net.JoinHostPort(sshCfg.Host, strconv.Itoa(sshCfg.Port))
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	require.NoError(t, err, "should be able to connect to SSH port %s", addr)
	conn.Close()
}

// TestDockerExecCommand verifies command execution inside the container.
func TestDockerExecCommand(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	name := uniqueName(t, "exec")

	t.Cleanup(func() {
		_ = b.Destroy(context.Background(), name)
	})

	require.NoError(t, b.Create(ctx, name, backend.VMConfig{}))

	result, err := b.Exec(ctx, name, []string{"echo", "hello"})
	require.NoError(t, err)
	assert.Contains(t, result.Stdout, "hello")
	assert.Equal(t, 0, result.ExitCode)
}

// TestDockerList creates two VMs and verifies they both appear in the list,
// sorted by name, with the correct backend name.
func TestDockerList(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	name1 := uniqueName(t, "lista")
	name2 := uniqueName(t, "listb")

	t.Cleanup(func() {
		_ = b.Destroy(context.Background(), name1)
		_ = b.Destroy(context.Background(), name2)
	})

	require.NoError(t, b.Create(ctx, name1, backend.VMConfig{}))
	require.NoError(t, b.Create(ctx, name2, backend.VMConfig{}))

	vms, err := b.List(ctx)
	require.NoError(t, err)

	// Find our two VMs in the list (there may be others from parallel runs).
	found := map[string]backend.VMInfo{}
	for _, vm := range vms {
		if vm.Name == name1 || vm.Name == name2 {
			found[vm.Name] = vm
		}
	}

	require.Len(t, found, 2, "both test VMs should appear in the list")
	assert.Equal(t, "docker", found[name1].Backend)
	assert.Equal(t, "docker", found[name2].Backend)

	// Verify sorted order: the list overall should be sorted.
	for i := 1; i < len(vms); i++ {
		assert.LessOrEqual(t, vms[i-1].Name, vms[i].Name,
			"list should be sorted by name: %q should come before %q", vms[i-1].Name, vms[i].Name)
	}
}

// TestDockerCleanup verifies that after Destroy, the underlying Docker
// container is fully removed (docker inspect fails).
func TestDockerCleanup(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	name := uniqueName(t, "clean")

	require.NoError(t, b.Create(ctx, name, backend.VMConfig{}))
	require.NoError(t, b.Destroy(ctx, name))

	// Verify the container no longer exists via docker inspect.
	cName := containerName(name)
	cmd := exec.CommandContext(ctx, "docker", "inspect", cName)
	err := cmd.Run()
	assert.Error(t, err, "docker inspect should fail after container is destroyed")
}

// TestDockerInvalidName verifies that Create rejects invalid VM names.
func TestDockerInvalidName(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		vmName  string
		wantErr error
	}{
		{"empty name", "", backend.ErrInvalidVMName},
		{"starts with digit", "1bad", backend.ErrInvalidVMName},
		{"uppercase", "BadName", backend.ErrInvalidVMName},
		{"special chars", "bad_name!", backend.ErrInvalidVMName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := b.Create(ctx, tt.vmName, backend.VMConfig{})
			require.Error(t, err)
			assert.True(t, errors.Is(err, tt.wantErr),
				"expected %v, got %v", tt.wantErr, err)
		})
	}
}

// TestDockerDuplicateCreate verifies that creating a VM with an existing name
// returns ErrVMAlreadyExists.
func TestDockerDuplicateCreate(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	name := uniqueName(t, "dup")

	t.Cleanup(func() {
		_ = b.Destroy(context.Background(), name)
	})

	require.NoError(t, b.Create(ctx, name, backend.VMConfig{}))

	err := b.Create(ctx, name, backend.VMConfig{})
	require.Error(t, err)
	assert.ErrorIs(t, err, backend.ErrVMAlreadyExists,
		"duplicate create should return ErrVMAlreadyExists")
}
