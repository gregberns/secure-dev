// Package backend defines VM configuration types.
// REQ-003-011: VMConfig Struct
package backend

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

// Validate checks if the VMConfig is valid according to backend rules.
// REQ-003-021: Error handling
func (cfg *VMConfig) Validate() error {
	if cfg.CPUs < 1 {
		return wrapError(ErrInvalidConfig, "cpus must be at least 1")
	}
	if cfg.Memory == "" {
		return wrapError(ErrInvalidConfig, "memory cannot be empty")
	}
	if cfg.Disk == "" {
		return wrapError(ErrInvalidConfig, "disk cannot be empty")
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
