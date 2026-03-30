// Package backend defines VM configuration types.
// REQ-003-011: VMConfig Struct
package backend

import (
	"fmt"
	"regexp"
)

// NetworkMode defines how a VM connects to the network.
// REQ-003-012
type NetworkMode string

const (
	// NetworkNAT allows outbound traffic through host NAT (default).
	// REQ-003-012
	NetworkNAT NetworkMode = "nat"

	// NetworkBridged gives the VM a network-visible IP on the host's LAN.
	// REQ-003-012
	NetworkBridged NetworkMode = "bridged"

	// NetworkIsolated provides no network connectivity.
	// REQ-003-012
	NetworkIsolated NetworkMode = "isolated"
)

// Mount defines a host directory to mount inside the VM.
// REQ-003-011
type Mount struct {
	// HostPath is the path on the host filesystem to mount.
	HostPath string `json:"host_path"`

	// GuestPath is the path inside the VM where the mount is attached.
	GuestPath string `json:"guest_path"`

	// Writable determines if the mount is read-write (true) or read-only (false).
	// Default is read-only for security.
	// REQ-004-004
	Writable bool `json:"writable"`
}

// ProvisionScript defines a script to run during VM provisioning.
// REQ-003-011
type ProvisionScript struct {
	// Mode is "system" (runs as root) or "user" (runs as default user).
	// REQ-006-005
	Mode string `json:"mode"`

	// Script is the shell script content to execute.
	// REQ-006-005
	Script string `json:"script"`
}

// VMConfig defines the desired state of a VM.
// REQ-003-011
type VMConfig struct {
	// CPUs is the number of virtual CPUs. Default: 4.
	CPUs int `json:"cpus"`

	// Memory is the amount of RAM (e.g., "4GiB", "8GiB"). Default: "8GiB".
	Memory string `json:"memory"`

	// Disk is the virtual disk size (e.g., "50GiB", "100GiB"). Default: "100GiB".
	Disk string `json:"disk"`

	// BaseImage is the OS image to use (e.g., "ubuntu:24.04").
	// Resolved via built-in short-name mapping embedded in binary.
	// Default: "ubuntu:24.04".
	BaseImage string `json:"base_image"`

	// Mounts defines host directories to mount in the guest.
	// Default is empty (no mounts) for security.
	// REQ-003-011
	Mounts []Mount `json:"mounts"`

	// NetworkMode controls network connectivity. Default: "nat".
	// REQ-003-011
	NetworkMode NetworkMode `json:"network_mode"`

	// ProvisionScripts are scripts to run inside the VM after creation.
	// REQ-003-011
	ProvisionScripts []ProvisionScript `json:"provision_scripts"`

	// EnvVars are environment variables to inject into the VM.
	// Sensitive values (tokens, keys) are injected at runtime via SSH
	// SendEnv/AcceptEnv — never persisted to disk in Lima YAML or in the guest.
	// Non-sensitive values may be passed through Lima's env field.
	// REQ-003-011, REQ-003-023
	EnvVars map[string]string `json:"env_vars"`

	// BackendOptions holds backend-specific configuration.
	// For Lima: vmType, mountType, sshType, etc.
	// REQ-003-011
	BackendOptions map[string]any `json:"backend_options"`
}

// resourceSizePattern matches resource size strings like "4GiB", "100GiB", "8G", "512MiB".
// REQ-003-011: Memory and Disk must use <number><unit> format.
var resourceSizePattern = regexp.MustCompile(`^[0-9]+([KMGT]i?B?)$`)

// vmNamePattern matches valid VM names.
// REQ-001-006: names must start with a lowercase letter, followed by lowercase
// letters, digits, or hyphens, total length 1-63 characters.
var vmNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// ValidateVMName checks that a VM name meets the naming requirements.
// REQ-001-006: VM names are validated against ^[a-z][a-z0-9-]{0,62}$
func ValidateVMName(name string) error {
	if name == "" {
		return wrapError(ErrInvalidVMName, "name must not be empty")
	}
	if len(name) > 63 {
		return wrapError(ErrInvalidVMName, fmt.Sprintf("name %q exceeds maximum length of 63 characters (got %d)", name, len(name)))
	}
	if !vmNamePattern.MatchString(name) {
		return wrapError(ErrInvalidVMName, fmt.Sprintf("name %q must match ^[a-z][a-z0-9-]{0,62}$ (start with lowercase letter, contain only lowercase letters, digits, and hyphens)", name))
	}
	return nil
}

// Validate checks if the VMConfig is valid according to backend rules.
// REQ-003-021: Error handling
func (cfg *VMConfig) Validate() error {
	if cfg.CPUs < 1 {
		return wrapError(ErrInvalidConfig, "cpus must be at least 1")
	}
	if cfg.Memory == "" {
		return wrapError(ErrInvalidConfig, "memory cannot be empty")
	}
	if !resourceSizePattern.MatchString(cfg.Memory) {
		return wrapError(ErrInvalidConfig, fmt.Sprintf("memory %q must be in <number><unit> format (e.g., 4GiB, 8G)", cfg.Memory))
	}
	if cfg.Disk == "" {
		return wrapError(ErrInvalidConfig, "disk cannot be empty")
	}
	if !resourceSizePattern.MatchString(cfg.Disk) {
		return wrapError(ErrInvalidConfig, fmt.Sprintf("disk %q must be in <number><unit> format (e.g., 50GiB, 100G)", cfg.Disk))
	}
	if cfg.BaseImage == "" {
		return wrapError(ErrInvalidConfig, "base_image cannot be empty")
	}

	// Validate network mode
	switch cfg.NetworkMode {
	case NetworkNAT, NetworkBridged, NetworkIsolated, "":
		// Valid or empty (defaults to NAT)
	default:
		return wrapError(ErrInvalidConfig, "invalid network_mode: must be 'nat', 'bridged', or 'isolated'")
	}

	// Validate mounts - check for sensitive paths would be done by security layer
	for _, m := range cfg.Mounts {
		if m.HostPath == "" {
			return wrapError(ErrInvalidConfig, "mount host_path cannot be empty")
		}
		if m.GuestPath == "" {
			return wrapError(ErrInvalidConfig, "mount guest_path cannot be empty")
		}
	}

	return nil
}

// wrapError wraps an error with additional context.
func wrapError(err error, msg string) error {
	if err == nil {
		return nil
	}
	if msg == "" {
		return err
	}
	return &wrappedError{
		err: err,
		msg: msg,
	}
}

// wrappedError provides context for backend errors.
type wrappedError struct {
	err error
	msg string
}

func (e *wrappedError) Error() string {
	if e.msg != "" {
		return e.msg + ": " + e.err.Error()
	}
	return e.err.Error()
}

func (e *wrappedError) Unwrap() error {
	return e.err
}
