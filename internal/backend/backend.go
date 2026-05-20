// Package backend defines the VM backend interface and associated types.
// REQ-003-001: Backend Interface
// REQ-003-004: VM Status Reporting
package backend

import (
	"context"
	"time"
)

// VMStatus represents the current state of a VM.
// REQ-003-004
type VMStatus string

const (
	StatusCreating VMStatus = "creating"
	StatusRunning  VMStatus = "running"
	StatusStopped  VMStatus = "stopped"
	StatusError    VMStatus = "error"
)

// VMInfo describes a VM instance.
// REQ-003-005
//
// Timestamp fields are *time.Time + omitempty so absence (backend doesn't
// know) is distinguishable from a zero value, and the JSON wire format omits
// the field entirely rather than emitting null or "0001-01-01T00:00:00Z".
type VMInfo struct {
	Name        string     `json:"name"`
	Status      VMStatus   `json:"status"`
	Backend     string     `json:"backend"`
	CPUs        int        `json:"cpus"`
	Memory      string     `json:"memory"`
	Disk        string     `json:"disk"`
	IP          string     `json:"ip,omitempty"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	LastStarted *time.Time `json:"last_started,omitempty"`
	LastStopped *time.Time `json:"last_stopped,omitempty"`
}

// SSHConfig holds the information needed to SSH into a VM.
// REQ-003-006, REQ-007-005
type SSHConfig struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	User         string `json:"user"`
	IdentityFile string `json:"identity_file"`
	ProxyCommand string `json:"proxy_command,omitempty"` // Used for VSOCK transport
	ForwardAgent bool   `json:"forward_agent"`           // Default: false
	Transport    string `json:"transport"`               // "tcp" or "vsock", REQ-007-005
}

// ExecResult holds the result of a command executed inside a VM.
// REQ-003-007
type ExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// SnapshotInfo describes a VM snapshot.
// REQ-003-008
type SnapshotInfo struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Size      int64     `json:"size_bytes"`
}

// Backend is the core interface that all VM backends MUST implement.
// REQ-003-001
type Backend interface {
	// Name returns the backend's registered name (e.g., "lima", "docker").
	Name() string

	// Available returns nil if the backend is usable on this system,
	// or an error explaining why not (missing binary, wrong OS, etc.).
	// REQ-003-002
	Available() error

	// Create provisions a new VM with the given name and configuration.
	// REQ-003-003
	Create(ctx context.Context, name string, cfg VMConfig) error

	// Start boots a stopped VM.
	// REQ-003-003
	Start(ctx context.Context, name string) error

	// Stop shuts down a running VM.
	// REQ-003-003
	Stop(ctx context.Context, name string) error

	// Destroy removes a VM and all its associated resources.
	// REQ-003-003
	Destroy(ctx context.Context, name string) error

	// Status returns the current status of a named VM.
	// REQ-003-004
	Status(ctx context.Context, name string) (VMStatus, error)

	// List returns all VMs managed by this backend.
	// REQ-003-005
	List(ctx context.Context) ([]VMInfo, error)

	// SSHConfig returns SSH connection details for a running VM.
	// REQ-003-006
	SSHConfig(ctx context.Context, name string) (SSHConfig, error)

	// Exec runs a command inside the named VM and returns the result.
	// REQ-003-007
	Exec(ctx context.Context, name string, command []string) (ExecResult, error)
}

// Snapshotter is an optional interface for backends that support snapshots.
// REQ-003-008
type Snapshotter interface {
	// SnapshotCreate creates a named snapshot of the VM's current state.
	SnapshotCreate(ctx context.Context, name, tag string) error

	// SnapshotApply restores a VM to a previously saved snapshot.
	SnapshotApply(ctx context.Context, name, tag string) error

	// SnapshotDelete removes a named snapshot.
	SnapshotDelete(ctx context.Context, name, tag string) error

	// SnapshotList returns all snapshots for a given VM.
	SnapshotList(ctx context.Context, name string) ([]SnapshotInfo, error)
}

// Cloner is an optional interface for backends that support VM cloning.
// REQ-003-009
type Cloner interface {
	// Clone creates a new VM that is a copy of an existing VM.
	Clone(ctx context.Context, src, dst string) error
}

// Syncer is an optional interface for backends that support file sync.
// REQ-003-010, REQ-007-017
type Syncer interface {
	// SyncTo copies files from host to guest.
	SyncTo(ctx context.Context, name, hostPath, guestPath string) error

	// SyncFrom copies files from guest to host.
	SyncFrom(ctx context.Context, name, guestPath, hostPath string) error

	// SyncDiff returns a unified diff of files between guest and host without copying.
	// REQ-007-017
	SyncDiff(ctx context.Context, name, guestPath, hostPath string) (string, error)
}
