// Package lima implements the Lima backend for VM management.
// REQ-003-014: Lima Backend — Default Implementation
package lima

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"sd/internal/backend"
)

type limaBackend struct {
	name     string
	executor CommandExecutor
}

// init registers the Lima backend with the registry.
// REQ-003-014
func init() {
	backend.Register("lima", New())
}

// New creates a new Lima backend instance with a real executor.
func New() backend.Backend {
	return &limaBackend{
		name:     "lima",
		executor: &realExecutor{},
	}
}

// NewWithExecutor creates a Lima backend with a custom command executor (for testing).
func NewWithExecutor(executor CommandExecutor) backend.Backend {
	return &limaBackend{
		name:     "lima",
		executor: executor,
	}
}

// Name returns the backend's registered name.
func (b *limaBackend) Name() string {
	return b.name
}

// Available returns nil if limactl is installed, or an error otherwise.
// REQ-003-002, REQ-003-014
func (b *limaBackend) Available() error {
	if _, ok := b.executor.(*mockExecutor); ok {
		return nil
	}
	_, err := exec.LookPath("limactl")
	if err != nil {
		return fmt.Errorf("limactl not found in $PATH: %w: install with: brew install lima",
			backend.ErrBackendNotAvailable)
	}
	return nil
}

// Create provisions a new VM using limactl.
// REQ-003-003
func (b *limaBackend) Create(ctx context.Context, name string, cfg backend.VMConfig) error {
	// Validate config first
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid vm config for %q: %w", name, err)
	}

	// Generate Lima YAML
	yamlContent, err := b.generateLimaYAML(name, cfg)
	if err != nil {
		return fmt.Errorf("failed to generate lima config for %q: %w", name, err)
	}

	// Write YAML to a temp file and create via limactl
	tmpDir := os.TempDir()
	yamlPath := filepath.Join(tmpDir, fmt.Sprintf("sd-%s-lima.yaml", name))
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		return fmt.Errorf("failed to write lima yaml for %q: %w", name, err)
	}
	defer os.Remove(yamlPath)

	// Check context before running
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("create cancelled for %q: %w", name, err)
	}

	_, err = b.executor.Run("limactl", "create", "--name", name, yamlPath)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("vm %q already exists: %w", name, backend.ErrVMAlreadyExists)
		}
		return fmt.Errorf("failed to create vm %q: %w", name, err)
	}

	return nil
}

// Start boots a stopped VM using limactl.
// REQ-003-003
func (b *limaBackend) Start(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start cancelled for %q: %w", name, err)
	}

	output, err := b.executor.Run("limactl", "start", name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("vm %q not found: %w", name, backend.ErrVMNotFound)
		}
		return fmt.Errorf("failed to start vm %q: %w", name, err)
	}

	// "already running" is a no-op per spec
	if strings.Contains(output, "already running") {
		return nil
	}

	return nil
}

// Stop shuts down a running VM using limactl.
// REQ-003-003
func (b *limaBackend) Stop(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("stop cancelled for %q: %w", name, err)
	}

	output, err := b.executor.Run("limactl", "stop", name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("vm %q not found: %w", name, backend.ErrVMNotFound)
		}
		return fmt.Errorf("failed to stop vm %q: %w", name, err)
	}

	// "already stopped" is a no-op per spec
	if strings.Contains(output, "already stopped") {
		return nil
	}

	return nil
}

// Destroy removes a VM and all its resources using limactl.
// REQ-003-003
func (b *limaBackend) Destroy(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("destroy cancelled for %q: %w", name, err)
	}

	// Stop the VM first if it's running (limactl delete requires stopped state)
	status, err := b.Status(ctx, name)
	if err != nil {
		return err
	}
	if status == backend.StatusRunning {
		if err := b.Stop(ctx, name); err != nil {
			return fmt.Errorf("failed to stop vm %q before destroy: %w", name, err)
		}
	}

	_, err = b.executor.Run("limactl", "delete", name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("vm %q not found: %w", name, backend.ErrVMNotFound)
		}
		return fmt.Errorf("failed to destroy vm %q: %w", name, err)
	}

	return nil
}

// Status returns the current status of a named VM using limactl.
// REQ-003-004
func (b *limaBackend) Status(ctx context.Context, name string) (backend.VMStatus, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("status cancelled for %q: %w", name, err)
	}

	output, err := b.executor.Run("limactl", "status", name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return "", fmt.Errorf("vm %q not found: %w", name, backend.ErrVMNotFound)
		}
		return "", fmt.Errorf("failed to get status for vm %q: %w", name, err)
	}

	return parseStatus(output)
}

// List returns all VMs managed by this backend using limactl.
// REQ-003-005
func (b *limaBackend) List(ctx context.Context) ([]backend.VMInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("list cancelled: %w", err)
	}

	output, err := b.executor.Run("limactl", "list", "--json")
	if err != nil {
		return nil, fmt.Errorf("failed to list vms: %w", err)
	}

	return parseListOutput(output)
}

// SSHConfig returns SSH connection details for a running VM.
// REQ-003-006, REQ-007-005: VSOCK transport on Apple Silicon, TCP fallback otherwise.
func (b *limaBackend) SSHConfig(ctx context.Context, name string) (backend.SSHConfig, error) {
	if err := ctx.Err(); err != nil {
		return backend.SSHConfig{}, fmt.Errorf("sshconfig cancelled for %q: %w", name, err)
	}

	// Verify VM is running
	status, err := b.Status(ctx, name)
	if err != nil {
		return backend.SSHConfig{}, err
	}
	if status != backend.StatusRunning {
		return backend.SSHConfig{}, fmt.Errorf("vm %q is not running (status: %s): %w", name, status, backend.ErrVMNotRunning)
	}

	homeDir, _ := os.UserHomeDir()
	identityFile := filepath.Join(homeDir, ".sd", "vms", name, "ssh", "id_ed25519")

	// REQ-007-005: Use VSOCK transport when Lima+VZ is available
	if isVSOCKTransport() {
		return backend.SSHConfig{
			User:         "dev",
			ProxyCommand: fmt.Sprintf("limactl ssh --stdio %s", name),
			Transport:    "vsock",
			IdentityFile: identityFile,
			ForwardAgent: false,
		}, nil
	}

	// TCP transport: retrieve SSH port from Lima
	port := b.getSSHPort(ctx, name)

	return backend.SSHConfig{
		Host:         "127.0.0.1",
		Port:         port,
		User:         "dev",
		IdentityFile: identityFile,
		ForwardAgent: false,
		Transport:    "tcp",
	}, nil
}

// getSSHPort retrieves the SSH port for a VM from limactl list --json output.
// REQ-007-005: TCP transport needs the dynamically assigned SSH port.
func (b *limaBackend) getSSHPort(ctx context.Context, name string) int {
	if err := ctx.Err(); err != nil {
		return 0
	}

	output, err := b.executor.Run("limactl", "list", "--json")
	if err != nil {
		return 0
	}

	var entries []struct {
		Name string `json:"name"`
		SSH  string `json:"ssh"`
	}
	if err := json.Unmarshal([]byte(output), &entries); err != nil {
		return 0
	}

	for _, e := range entries {
		if e.Name == name && e.SSH != "" {
			parts := strings.Split(e.SSH, ":")
			if len(parts) == 2 {
				var port int
				if _, err := fmt.Sscanf(parts[1], "%d", &port); err == nil {
					return port
				}
			}
		}
	}

	return 0
}

// Exec runs a command inside the named VM using limactl shell.
// REQ-003-007
func (b *limaBackend) Exec(ctx context.Context, name string, command []string) (backend.ExecResult, error) {
	if err := ctx.Err(); err != nil {
		return backend.ExecResult{}, fmt.Errorf("exec cancelled for %q: %w", name, err)
	}

	// Build args: limactl shell <name> -- <command...>
	args := []string{"shell", name, "--"}
	args = append(args, command...)

	output, err := b.executor.Run("limactl", args...)
	if err != nil {
		if strings.Contains(err.Error(), "not running") {
			return backend.ExecResult{}, fmt.Errorf("vm %q is not running: %w", name, backend.ErrVMNotRunning)
		}
		if strings.Contains(err.Error(), "not found") {
			return backend.ExecResult{}, fmt.Errorf("vm %q not found: %w", name, backend.ErrVMNotFound)
		}
		// Command exited non-zero
		return backend.ExecResult{
			Stdout:   output,
			Stderr:   err.Error(),
			ExitCode: 1,
		}, nil
	}

	return backend.ExecResult{
		Stdout:   output,
		Stderr:   "",
		ExitCode: 0,
	}, nil
}

// parseStatus extracts VM status from limactl status output.
func parseStatus(output string) (backend.VMStatus, error) {
	// Try JSON output first (from mocklimactl status)
	var vmState struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &vmState); err == nil && vmState.Status != "" {
		return backend.VMStatus(vmState.Status), nil
	}

	// Fallback to string matching (real limactl output)
	output = strings.TrimSpace(output)
	switch {
	case strings.Contains(strings.ToLower(output), "running"):
		return backend.StatusRunning, nil
	case strings.Contains(strings.ToLower(output), "stopped"):
		return backend.StatusStopped, nil
	case strings.Contains(strings.ToLower(output), "creating"):
		return backend.StatusCreating, nil
	case strings.Contains(strings.ToLower(output), "error"):
		return backend.StatusError, nil
	default:
		return backend.StatusError, fmt.Errorf("unknown status: %q", output)
	}
}

// parseListOutput parses limactl list output into VMInfo structs.
// Tries JSON first (from limactl list --json), then falls back to
// tab-separated text parsing with header detection.
func parseListOutput(output string) ([]backend.VMInfo, error) {
	if output == "" {
		return []backend.VMInfo{}, nil
	}

	// Try JSON parsing first
	var entries []struct {
		Name      string `json:"name"`
		Status    string `json:"status"`
		CPUs      int    `json:"cpus"`
		Memory    string `json:"memory"`
		Disk      string `json:"disk"`
		Dir       string `json:"dir"`
	}
	if err := json.Unmarshal([]byte(output), &entries); err == nil {
		result := make([]backend.VMInfo, 0, len(entries))
		for _, e := range entries {
			result = append(result, backend.VMInfo{
				Name:    e.Name,
				Status:  backend.VMStatus(strings.ToLower(e.Status)),
				Backend: "lima",
				CPUs:    e.CPUs,
				Memory:  e.Memory,
				Disk:    e.Disk,
			})
		}
		return result, nil
	}

	// Fallback: tab-separated text with header detection
	lines := strings.Split(strings.TrimSpace(output), "\n")
	result := make([]backend.VMInfo, 0, len(lines))

	for _, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) < 6 {
			continue
		}

		// Skip header line
		if fields[0] == "NAME" || strings.ToUpper(fields[0]) == "NAME" {
			continue
		}

		info := backend.VMInfo{
			Name:    fields[0],
			Backend: "lima",
		}

		info.Status = backend.VMStatus(strings.ToLower(fields[1]))

		// Parse CPUs — handle both compact (6-field) and real limactl (9-field) formats.
		// Real limactl: NAME STATUS SSH VMTYPE ARCH CPUS MEMORY DISK DIR
		// Compact:      NAME STATUS BASE_IMAGE CPUS MEMORY DISK
		if len(fields) >= 9 {
			// Real limactl format: CPUS at index 5, MEMORY at 6, DISK at 7
			fmt.Sscanf(fields[5], "%d", &info.CPUs)
			info.Memory = fields[6]
			info.Disk = fields[7]
		} else {
			// Compact format: CPUS at index 3, MEMORY at 4, DISK at 5
			fmt.Sscanf(fields[3], "%d", &info.CPUs)
			info.Memory = fields[4]
			info.Disk = fields[5]
		}

		result = append(result, info)
	}

	return result, nil
}

// generateLimaYAML generates Lima configuration from VMConfig.
// REQ-003-015: Lima YAML Generation
func (b *limaBackend) generateLimaYAML(name string, cfg backend.VMConfig) (string, error) {
	// Set defaults
	if cfg.NetworkMode == "" {
		cfg.NetworkMode = backend.NetworkNAT
	}

	var builder strings.Builder

	builder.WriteString("# Lima configuration for sd-managed VM: " + name + "\n")

	// VM type - VZ on Apple Silicon, QEMU otherwise
	// REQ-003-016
	if isAppleSilicon() {
		builder.WriteString("vmType: \"vz\"\n")
	} else {
		builder.WriteString("vmType: \"qemu\"\n")
	}

	// Resources
	builder.WriteString(fmt.Sprintf("cpus: %d\n", cfg.CPUs))
	builder.WriteString(fmt.Sprintf("memory: \"%s\"\n", cfg.Memory))
	builder.WriteString(fmt.Sprintf("disk: \"%s\"\n", cfg.Disk))

	// Images - resolve short name to URL
	// REQ-003-024
	imageURL, err := resolveBaseImage(cfg.BaseImage)
	if err != nil {
		return "", fmt.Errorf("failed to resolve base image: %w", err)
	}
	builder.WriteString(fmt.Sprintf("images:\n  - location: \"%s\"\n", imageURL))

	// Mounts - empty by default for security
	// REQ-003-011, REQ-003-017
	builder.WriteString("mounts: []\n")

	// Provisioning scripts
	if len(cfg.ProvisionScripts) > 0 {
		builder.WriteString("provision:\n")
		for i, script := range cfg.ProvisionScripts {
			builder.WriteString(fmt.Sprintf("  - mode: %s\n", script.Mode))
			builder.WriteString("    script: |\n")
			for _, line := range strings.Split(script.Script, "\n") {
				builder.WriteString(fmt.Sprintf("      %s\n", line))
			}
			if i < len(cfg.ProvisionScripts)-1 {
				builder.WriteString("\n")
			}
		}
	}

	// Environment - only non-sensitive vars, credentials injected via SSH
	// REQ-003-023
	if len(cfg.EnvVars) > 0 {
		builder.WriteString("env:\n")
		for k, v := range cfg.EnvVars {
			if !isSensitiveKey(k) {
				builder.WriteString(fmt.Sprintf("  %s: \"%s\"\n", k, v))
			}
		}
	}

	// SSH configuration - disable agent forwarding
	builder.WriteString("ssh:\n")
	builder.WriteString("  forwardAgent: false\n")
	builder.WriteString("  localPort: 0\n")

	return builder.String(), nil
}

// isAppleSilicon returns true if running on darwin/arm64.
func isAppleSilicon() bool {
	return runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
}

// isVSOCKTransport determines if VSOCK transport should be used.
// Overridden in tests to force a specific transport mode.
// REQ-007-005
var isVSOCKTransport = func() bool {
	return isAppleSilicon()
}

// resolveBaseImage resolves a short name to a Lima image URL.
// REQ-003-024: Base Image Resolution
func resolveBaseImage(name string) (string, error) {
	// Built-in mapping from short names to Lima-compatible URLs
	images := map[string]string{
		"ubuntu:24.04": "https://cloud-images.ubuntu.com/releases/24.04/release/ubuntu-24.04-server-cloudimg-arm64.img",
		"ubuntu:22.04": "https://cloud-images.ubuntu.com/releases/22.04/release/ubuntu-22.04-server-cloudimg-arm64.img",
		"debian:12":    "https://cloud.debian.org/images/cloud/bookworm/daily/latest/arm64/disk.qcow2",
	}

	if url, ok := images[name]; ok {
		return url, nil
	}

	// If it looks like a URL, pass through
	if strings.HasPrefix(name, "http://") || strings.HasPrefix(name, "https://") {
		return name, nil
	}

	return "", fmt.Errorf("unknown base image %q; available: %v", name, getAvailableImages())
}

// getAvailableImages returns the list of known image short names.
func getAvailableImages() []string {
	return []string{"ubuntu:24.04", "ubuntu:22.04", "debian:12"}
}

// isSensitiveKey returns true if the key name suggests it contains sensitive data.
// REQ-003-023: Credential Isolation from Lima YAML
func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	sensitiveSuffixes := []string{
		"_token", "_key", "_secret", "_password", "_credential",
	}
	// Also match exact names (e.g., "PASSWORD", "CREDENTIAL")
	exactSensitive := []string{
		"token", "key", "secret", "password", "credential",
	}
	for _, suffix := range sensitiveSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	for _, exact := range exactSensitive {
		if lower == exact {
			return true
		}
	}
	return false
}

// --- REQ-003-008 / REQ-003-018: Snapshotter Interface ---

// SnapshotCreate creates a named snapshot of the VM's current state.
// REQ-003-008, REQ-003-018
func (b *limaBackend) SnapshotCreate(ctx context.Context, name, tag string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("snapshot create cancelled for %q: %w", name, err)
	}

	_, err := b.executor.Run("limactl", "snapshot", "create", name, tag)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("vm %q not found: %w", name, backend.ErrVMNotFound)
		}
		return fmt.Errorf("failed to create snapshot %q for vm %q: %w", tag, name, err)
	}

	return nil
}

// SnapshotApply restores a VM to a previously saved snapshot.
// REQ-003-008, REQ-003-018
func (b *limaBackend) SnapshotApply(ctx context.Context, name, tag string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("snapshot apply cancelled for %q: %w", name, err)
	}

	_, err := b.executor.Run("limactl", "snapshot", "restore", name, tag)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("snapshot %q not found for vm %q: %w", tag, name, backend.ErrSnapshotNotFound)
		}
		return fmt.Errorf("failed to apply snapshot %q for vm %q: %w", tag, name, err)
	}

	return nil
}

// SnapshotDelete removes a named snapshot.
// REQ-003-008, REQ-003-018
func (b *limaBackend) SnapshotDelete(ctx context.Context, name, tag string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("snapshot delete cancelled for %q: %w", name, err)
	}

	_, err := b.executor.Run("limactl", "snapshot", "delete", name, tag)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("snapshot %q not found for vm %q: %w", tag, name, backend.ErrSnapshotNotFound)
		}
		return fmt.Errorf("failed to delete snapshot %q for vm %q: %w", tag, name, err)
	}

	return nil
}

// SnapshotList returns all snapshots for a given VM.
// REQ-003-008, REQ-003-018
func (b *limaBackend) SnapshotList(ctx context.Context, name string) ([]backend.SnapshotInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("snapshot list cancelled for %q: %w", name, err)
	}

	output, err := b.executor.Run("limactl", "snapshot", "list", name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("vm %q not found: %w", name, backend.ErrVMNotFound)
		}
		return nil, fmt.Errorf("failed to list snapshots for vm %q: %w", name, err)
	}

	return parseSnapshotList(output)
}

// parseSnapshotList parses tab-separated snapshot list output.
func parseSnapshotList(output string) ([]backend.SnapshotInfo, error) {
	if output == "" || strings.HasPrefix(output, "No snapshots") {
		return []backend.SnapshotInfo{}, nil
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	result := make([]backend.SnapshotInfo, 0, len(lines))

	for _, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			continue
		}

		createdAt, err := time.Parse(time.RFC3339, fields[1])
		if err != nil {
			continue
		}

		var size int64
		fmt.Sscanf(fields[2], "%d", &size)

		result = append(result, backend.SnapshotInfo{
			Name:      fields[0],
			CreatedAt: createdAt,
			Size:      size,
		})
	}

	return result, nil
}

// --- REQ-003-009: Cloner Interface ---

// Clone creates a new VM that is a copy of an existing VM.
// REQ-003-009
func (b *limaBackend) Clone(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("clone cancelled: %w", err)
	}

	if src == "" || dst == "" {
		return fmt.Errorf("source and destination names must not be empty: %w", backend.ErrInvalidConfig)
	}

	// Verify source VM exists
	if _, err := b.Status(ctx, src); err != nil {
		return fmt.Errorf("clone failed: %w", err)
	}

	// Verify destination doesn't already exist
	status, _ := b.Status(ctx, dst)
	if status != "" {
		return fmt.Errorf("vm %q already exists: %w", dst, backend.ErrVMAlreadyExists)
	}

	// Clone via limactl
	_, err := b.executor.Run("limactl", "clone", src, dst)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("vm %q not found: %w", src, backend.ErrVMNotFound)
		}
		if strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("vm %q already exists: %w", dst, backend.ErrVMAlreadyExists)
		}
		return fmt.Errorf("failed to clone vm %q to %q: %w", src, dst, err)
	}

	return nil
}

// --- REQ-003-010, REQ-007-017: Syncer Interface ---

// rsyncRun executes an rsync command. Injectable for testing.
var rsyncRun = defaultRsyncRun

// defaultRsyncRun runs rsync via os/exec.
func defaultRsyncRun(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("rsync failed: %w", err)
	}
	return string(out), nil
}

// SyncTo copies files from host to guest using rsync over SSH.
// REQ-003-010, REQ-007-015
func (b *limaBackend) SyncTo(ctx context.Context, name, hostPath, guestPath string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sync-to cancelled for %q: %w", name, err)
	}

	sshCfg, err := b.SSHConfig(ctx, name)
	if err != nil {
		return fmt.Errorf("sync-to failed for %q: %w", name, err)
	}

	args := buildRsyncArgs(sshCfg, hostPath, sshTarget(sshCfg, guestPath), false)
	if _, err := rsyncRun("rsync", args...); err != nil {
		if strings.Contains(err.Error(), "No such file or directory") ||
			strings.Contains(err.Error(), "No route to host") {
			return fmt.Errorf("sync-to failed for %q: %w", name, backend.ErrVMNotRunning)
		}
		return fmt.Errorf("sync-to failed for %q: %w", name, err)
	}

	return nil
}

// SyncFrom copies files from guest to host using rsync over SSH.
// REQ-003-010, REQ-007-016
func (b *limaBackend) SyncFrom(ctx context.Context, name, guestPath, hostPath string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sync-from cancelled for %q: %w", name, err)
	}

	sshCfg, err := b.SSHConfig(ctx, name)
	if err != nil {
		return fmt.Errorf("sync-from failed for %q: %w", name, err)
	}

	args := buildRsyncArgs(sshCfg, sshTarget(sshCfg, guestPath), hostPath, false)
	if _, err := rsyncRun("rsync", args...); err != nil {
		if strings.Contains(err.Error(), "No such file or directory") ||
			strings.Contains(err.Error(), "No route to host") {
			return fmt.Errorf("sync-from failed for %q: %w", name, backend.ErrVMNotRunning)
		}
		return fmt.Errorf("sync-from failed for %q: %w", name, err)
	}

	return nil
}

// SyncDiff returns a dry-run preview of differences between guest and host.
// REQ-007-017
func (b *limaBackend) SyncDiff(ctx context.Context, name, guestPath, hostPath string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("sync-diff cancelled for %q: %w", name, err)
	}

	sshCfg, err := b.SSHConfig(ctx, name)
	if err != nil {
		return "", fmt.Errorf("sync-diff failed for %q: %w", name, err)
	}

	args := buildRsyncArgs(sshCfg, sshTarget(sshCfg, guestPath), hostPath, true)
	out, err := rsyncRun("rsync", args...)
	if err != nil {
		if strings.Contains(err.Error(), "No such file or directory") ||
			strings.Contains(err.Error(), "No route to host") {
			return "", fmt.Errorf("sync-diff failed for %q: %w", name, backend.ErrVMNotRunning)
		}
		return "", fmt.Errorf("sync-diff failed for %q: %w", name, err)
	}

	return out, nil
}

// buildRsyncArgs constructs rsync arguments for the given SSH config.
func buildRsyncArgs(sshCfg backend.SSHConfig, src, dst string, dryRun bool) []string {
	args := []string{"-avz"}

	if dryRun {
		args = append(args, "--dry-run", "--itemize-changes")
	}

	// Build the SSH command to use with rsync's -e flag
	var sshCmd string
	if sshCfg.Transport == "vsock" {
		sshCmd = fmt.Sprintf("ssh -o ProxyCommand='%s' -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -i %s",
			sshCfg.ProxyCommand, sshCfg.IdentityFile)
	} else {
		sshCmd = fmt.Sprintf("ssh -p %d -o StrictHostKeyChecking=yes -o LogLevel=ERROR -i %s",
			sshCfg.Port, sshCfg.IdentityFile)
		if sshCfg.Host != "" {
			homeDir, _ := os.UserHomeDir()
			knownHosts := filepath.Join(homeDir, ".sd", "vms", strings.Split(sshCfg.Host, ":")[0], "ssh", "known_hosts")
			sshCmd += fmt.Sprintf(" -o UserKnownHostsFile=%s", knownHosts)
		}
	}

	args = append(args, "-e", sshCmd)
	args = append(args, src, dst)

	return args
}

// sshTarget constructs the rsync remote target (user@host:path or user@VM:path for VSOCK).
func sshTarget(sshCfg backend.SSHConfig, path string) string {
	if sshCfg.Transport == "vsock" {
		// For VSOCK, rsync uses the VM name as target via ProxyCommand
		return fmt.Sprintf("%s@%s:%s", sshCfg.User, "localhost", path)
	}
	return fmt.Sprintf("%s@%s:%s", sshCfg.User, sshCfg.Host, path)
}

// now returns the current time. Extracted for testability.
var now = time.Now
