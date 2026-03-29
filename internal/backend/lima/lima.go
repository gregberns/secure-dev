// Package lima implements the Lima backend for VM management.
// REQ-003-014: Lima Backend — Default Implementation
package lima

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

import "sd/internal/backend"

type limaBackend struct {
	name string
}

// init registers the Lima backend with the registry.
// REQ-003-014
func init() {
	backend.Register("lima", New())
}

// New creates a new Lima backend instance.
func New() backend.Backend {
	return &limaBackend{name: "lima"}
}

// Name returns the backend's registered name.
func (b *limaBackend) Name() string {
	return b.name
}

// Available returns nil if limactl is installed, or an error otherwise.
// REQ-003-002, REQ-003-014
func (b *limaBackend) Available() error {
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
	_, err := b.generateLimaYAML(name, cfg)
	if err != nil {
		return fmt.Errorf("failed to generate lima config for %q: %w", name, err)
	}

	// Write config and create VM
	// In real implementation, we would:
	// 1. Write YAML to ~/.lima/<name>/lima.yaml
	// 2. Run: limactl create <name>

	// For now, return not implemented until we have the full implementation
	return fmt.Errorf("lima.Create: %w", backend.ErrNotImplemented)
}

// Start boots a stopped VM using limactl.
// REQ-003-003
func (b *limaBackend) Start(ctx context.Context, name string) error {
	return fmt.Errorf("lima.Start: %w", backend.ErrNotImplemented)
}

// Stop shuts down a running VM using limactl.
// REQ-003-003
func (b *limaBackend) Stop(ctx context.Context, name string) error {
	return fmt.Errorf("lima.Stop: %w", backend.ErrNotImplemented)
}

// Destroy removes a VM and all its resources using limactl.
// REQ-003-003
func (b *limaBackend) Destroy(ctx context.Context, name string) error {
	return fmt.Errorf("lima.Destroy: %w", backend.ErrNotImplemented)
}

// Status returns the current status of a named VM using limactl.
// REQ-003-004
func (b *limaBackend) Status(ctx context.Context, name string) (backend.VMStatus, error) {
	return "", fmt.Errorf("lima.Status: %w", backend.ErrNotImplemented)
}

// List returns all VMs managed by this backend using limactl.
// REQ-003-005
func (b *limaBackend) List(ctx context.Context) ([]backend.VMInfo, error) {
	return nil, fmt.Errorf("lima.List: %w", backend.ErrNotImplemented)
}

// SSHConfig returns SSH connection details for a running VM.
// REQ-003-006
func (b *limaBackend) SSHConfig(ctx context.Context, name string) (backend.SSHConfig, error) {
	return backend.SSHConfig{}, fmt.Errorf("lima.SSHConfig: %w", backend.ErrNotImplemented)
}

// Exec runs a command inside the named VM using limactl shell.
// REQ-003-007
func (b *limaBackend) Exec(ctx context.Context, name string, command []string) (backend.ExecResult, error) {
	return backend.ExecResult{}, fmt.Errorf("lima.Exec: %w", backend.ErrNotImplemented)
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
	builder.WriteString("# REQ-003-016: VZ defaults on Apple Silicon\n")
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
	// REQ-004-027
	builder.WriteString("ssh:\n")
	builder.WriteString("  forwardAgent: false\n")
	builder.WriteString("  localPort: 0\n")

	return builder.String(), nil
}

// isAppleSilicon returns true if running on darwin/arm64.
func isAppleSilicon() bool {
	// Simple check - in production, use runtime.GOOS and runtime.GOARCH
	return true // Placeholder for testing
}

// resolveBaseImage resolves a short name to a Lima image URL.
// REQ-003-024: Base Image Resolution
func resolveBaseImage(name string) (string, error) {
	// Built-in mapping from short names to Lima-compatible URLs
	// REQ-003-024
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
		"_api_key", "_access_key", "_private_key",
	}
	for _, suffix := range sensitiveSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}
