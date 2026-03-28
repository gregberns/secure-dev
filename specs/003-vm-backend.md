# 003: VM Backend System

| Field        | Value      |
|--------------|------------|
| Status       | draft      |
| Created      | 2026-03-27 |
| Last Updated | 2026-03-27 |
| Authors      | claude     |
| Reviewers    |            |

## Overview

This spec defines the pluggable VM backend system for `sd`. The backend system abstracts VM lifecycle management behind a Go interface so that different virtualization technologies (Lima, Apple Virtualization Framework, Docker, Incus) can be used interchangeably. Lima is the default and first implementation. The design enforces security-first defaults: no host mounts, NAT networking, and runtime credential injection rather than persisted secrets.

## Goals

- G1: Define a stable `Backend` interface that all VM backends MUST implement
- G2: Define optional capability interfaces (`Snapshotter`, `Cloner`, `Syncer`) that backends MAY implement
- G3: Define the `VMConfig` struct that describes a VM's desired state
- G4: Implement the Lima backend as the default, production-ready backend
- G5: Establish a backend registry pattern for runtime backend selection
- G6: Stub future backends (AVF, Docker, Incus) with clear extension points

## Non-Goals

- NG1: Implementing non-Lima backends beyond stubs — those are future work
- NG2: Defining provisioning script content — that belongs in a provisioning spec
- NG3: Defining CLI commands — that belongs in the CLI spec
- NG4: Defining SSH key management — that belongs in an SSH spec
- NG5: Defining egress/firewall rules — that belongs in a security spec
- NG6: Multi-host or remote VM orchestration

## Requirements

### REQ-003-001: Backend Interface

All VM backends MUST implement the `Backend` interface. This interface defines the minimum set of operations required for VM lifecycle management. No backend may be registered or used without implementing every method in this interface.

**Acceptance criteria:**
- [ ] `Backend` interface is defined in `internal/backend/backend.go`
- [ ] All methods have context-aware signatures (accept `context.Context`)
- [ ] A backend that does not implement every method fails to compile

### REQ-003-002: Backend Availability Check

Every backend MUST implement an `Available()` method that returns `nil` if the backend is usable on the current system, or an error explaining why it is not (e.g., missing `limactl` binary, unsupported OS, missing kernel module).

**Acceptance criteria:**
- [ ] `Available()` returns `nil` when all prerequisites are met
- [ ] `Available()` returns an actionable error message when prerequisites are missing
- [ ] The error message names the missing dependency and suggests how to install it

### REQ-003-003: VM Lifecycle Operations

Every backend MUST support the full VM lifecycle: Create, Start, Stop, Destroy.

**Acceptance criteria:**
- [ ] `Create` provisions a new VM from a `VMConfig` and leaves it in `Stopped` or `Running` state
- [ ] `Start` transitions a `Stopped` VM to `Running`
- [ ] `Stop` transitions a `Running` VM to `Stopped`
- [ ] `Destroy` removes the VM and all associated resources (disk, config)
- [ ] Calling `Start` on an already-running VM is a no-op (no error)
- [ ] Calling `Stop` on an already-stopped VM is a no-op (no error)
- [ ] Calling `Destroy` on a non-existent VM returns an error

### REQ-003-004: VM Status Reporting

Every backend MUST report VM status via the `Status` method, returning one of the defined `VMStatus` values.

**Acceptance criteria:**
- [ ] `Status` returns the current state of a named VM
- [ ] Status values are limited to: `Creating`, `Running`, `Stopped`, `Error`
- [ ] `Status` returns an error for a non-existent VM name
- [ ] The `VMStatus` type is JSON-serializable as a string

### REQ-003-005: VM Listing

Every backend MUST support listing all VMs it manages via the `List` method.

**Acceptance criteria:**
- [ ] `List` returns a slice of `VMInfo` structs
- [ ] Each `VMInfo` contains: Name, Status, Backend, CPUs, Memory, Disk, IP, CreatedAt
- [ ] `List` returns an empty slice (not nil) when no VMs exist
- [ ] `VMInfo` is JSON-serializable

### REQ-003-006: SSH Configuration

Every backend MUST provide SSH connection details for a running VM via the `SSHConfig` method.

**Acceptance criteria:**
- [ ] `SSHConfig` returns host, port, user, identity file path, and optionally a proxy command
- [ ] `SSHConfig` returns an error if the VM is not running
- [ ] The returned config is sufficient to establish an SSH connection without additional discovery

### REQ-003-007: Command Execution

Every backend MUST support executing commands inside a VM via the `Exec` method.

**Acceptance criteria:**
- [ ] `Exec` runs the given command inside the named VM
- [ ] `ExecResult` contains stdout, stderr, and exit code
- [ ] `Exec` returns an error if the VM is not running
- [ ] `Exec` respects context cancellation for long-running commands

### REQ-003-008: Optional Snapshotter Interface

Backends MAY implement the `Snapshotter` interface for snapshot management. Callers MUST use a type assertion to check for capability before calling snapshot methods.

**Acceptance criteria:**
- [ ] `Snapshotter` interface is defined separately from `Backend`
- [ ] `SnapshotCreate` creates a named snapshot of the VM's current state
- [ ] `SnapshotApply` restores a VM to a previously saved snapshot
- [ ] `SnapshotDelete` removes a named snapshot
- [ ] `SnapshotList` returns all snapshots for a given VM
- [ ] Code that uses snapshots checks `if s, ok := b.(Snapshotter); ok` before calling

### REQ-003-009: Optional Cloner Interface

Backends MAY implement the `Cloner` interface for VM cloning. Callers MUST use a type assertion to check for capability.

**Acceptance criteria:**
- [ ] `Cloner` interface is defined separately from `Backend`
- [ ] `Clone` creates a new VM that is a copy of an existing VM
- [ ] The cloned VM has an independent lifecycle from the source
- [ ] Code that uses cloning checks `if c, ok := b.(Cloner); ok` before calling

### REQ-003-010: Optional Syncer Interface

Backends MAY implement the `Syncer` interface for file synchronization between host and guest. Callers MUST use a type assertion to check for capability.

**Acceptance criteria:**
- [ ] `Syncer` interface is defined separately from `Backend`
- [ ] `SyncTo` copies files from host to guest
- [ ] `SyncFrom` copies files from guest to host
- [ ] Both methods accept absolute paths for source and destination
- [ ] Code that uses syncing checks `if s, ok := b.(Syncer); ok` before calling

### REQ-003-011: VMConfig Struct

The `VMConfig` struct MUST define all parameters needed to create a VM. Security-first defaults MUST be enforced: no mounts, NAT networking.

**Acceptance criteria:**
- [ ] `VMConfig` contains fields for: CPUs, Memory, Disk, BaseImage, Mounts, NetworkMode, ProvisionScripts, EnvVars, BackendOptions
- [ ] Default `Mounts` is an empty slice (no host directories mounted)
- [ ] Default `NetworkMode` is `NAT`
- [ ] `BackendOptions` is typed as `map[string]any` for backend-specific settings
- [ ] `VMConfig` is JSON-serializable and deserializable

### REQ-003-012: Network Mode

`VMConfig` MUST support three network modes: NAT, Bridged, and Isolated.

**Acceptance criteria:**
- [ ] `NetworkMode` is a string type with defined constants
- [ ] `NAT` allows outbound traffic through host NAT (default)
- [ ] `Bridged` gives the VM a network-visible IP on the host's LAN
- [ ] `Isolated` provides no network connectivity
- [ ] Backends that do not support a requested network mode MUST return an error at creation time

### REQ-003-013: Backend Registry

The system MUST use a registry pattern where backends register themselves at init time. The registry MUST support selecting a backend by name from configuration or CLI flags.

**Acceptance criteria:**
- [ ] A `Register(name string, b Backend)` function exists for backend registration
- [ ] A `Get(name string) (Backend, error)` function retrieves a registered backend
- [ ] A `List() []string` function returns names of all registered backends
- [ ] A `Default() (Backend, error)` function returns the first available backend
- [ ] Duplicate registration for the same name panics at startup
- [ ] `Get` returns an actionable error if the named backend is not registered

### REQ-003-014: Lima Backend — Default Implementation

The Lima backend MUST be the default and first fully implemented backend. It MUST use the `limactl` CLI to manage VMs.

**Acceptance criteria:**
- [ ] Lima backend is implemented in `internal/backend/lima/`
- [ ] It registers itself as `"lima"` in the backend registry
- [ ] `Available()` checks for `limactl` in `$PATH` and returns an error if missing
- [ ] All `Backend` interface methods are implemented using `limactl` subcommands

### REQ-003-015: Lima YAML Generation

The Lima backend MUST generate Lima YAML configuration from a `VMConfig` struct.

**Acceptance criteria:**
- [ ] Generated YAML is a valid Lima instance configuration
- [ ] `VMConfig.CPUs` maps to Lima `cpus`
- [ ] `VMConfig.Memory` maps to Lima `memory`
- [ ] `VMConfig.Disk` maps to Lima `disk`
- [ ] `VMConfig.BaseImage` maps to Lima `images` with appropriate arch detection
- [ ] `VMConfig.ProvisionScripts` maps to Lima `provision` entries
- [ ] `VMConfig.Mounts` maps to Lima `mounts` (empty by default, meaning no mounts)
- [ ] Credentials MUST NOT be passed through Lima's `env` YAML field; credential injection flows exclusively through SSH (SendEnv/AcceptEnv) as defined in [007-connection.md](007-connection.md)

### REQ-003-016: Lima VZ Backend Defaults on Apple Silicon

On Apple Silicon (arm64 darwin), the Lima backend MUST default to the Apple Virtualization framework (`vmType: "vz"`) with Virtiofs and VSOCK.

**Acceptance criteria:**
- [ ] `vmType` defaults to `"vz"` on `darwin/arm64`
- [ ] `mountType` defaults to `"virtiofs"` when `vmType` is `"vz"` and mounts are configured
- [ ] SSH transport uses VSOCK when `vmType` is `"vz"`
- [ ] On non-Apple-Silicon platforms, `vmType` falls back to `"qemu"`

### REQ-003-017: Lima Mount Policy

The Lima backend MUST enforce a no-mount default. When no mounts are specified in `VMConfig`, the Lima YAML MUST contain an empty mounts array, overriding Lima's default behavior of mounting the home directory.

**Acceptance criteria:**
- [ ] Generated YAML includes `mounts: []` when `VMConfig.Mounts` is empty
- [ ] The host `$HOME` is never mounted unless explicitly requested in `VMConfig.Mounts`
- [ ] Mount locations are validated: mounts pointing to `$HOME` or `/` on the host produce a warning

### REQ-003-018: Lima Snapshot Support

The Lima backend MUST implement the `Snapshotter` interface. Since the VZ vmType does not support native snapshots, the Lima backend MUST use a clone-based snapshot strategy. Snapshot instance data lives under `~/.lima/` (managed by Lima). Snapshot metadata is tracked in `$SD_HOME/vms/<name>/snapshots.yaml`. On macOS, APFS clones (`cp -c`) MUST be used for near-instant snapshot creation.

**Acceptance criteria:**
- [ ] Lima backend implements the `Snapshotter` interface
- [ ] `SnapshotCreate` stops the VM, clones the instance directory using `cp -c` (APFS clone) on macOS, and restarts it
- [ ] `SnapshotApply` restores from the cloned instance
- [ ] `SnapshotList` returns available snapshots with name, timestamp, and size
- [ ] Snapshot operations are atomic — a failed snapshot does not corrupt the running VM
- [ ] Snapshot metadata (name, timestamp, source VM) is written to `$SD_HOME/vms/<name>/snapshots.yaml`

### REQ-003-019: Lima Provisioning

The Lima backend MUST support provisioning scripts that run inside the VM after creation. These MUST be executed via Lima's native provisioning mechanism.

**Acceptance criteria:**
- [ ] `VMConfig.ProvisionScripts` are mapped to Lima `provision` blocks with `mode: system` or `mode: user` as specified
- [ ] Provisioning scripts run in order
- [ ] Provisioning failures are reported as VM creation errors
- [ ] Provisioning scripts have access to environment variables from `VMConfig.EnvVars`

### REQ-003-020: Future Backend Stubs

The codebase MUST include stub implementations for future backends: `avf` (Apple Virtualization Framework), `docker` (Docker Desktop), and `incus` (Incus containers). Stubs MUST implement the `Backend` interface but return "not implemented" errors for all operations.

**Acceptance criteria:**
- [ ] Stub files exist at `internal/backend/avf/`, `internal/backend/docker/`, `internal/backend/incus/`
- [ ] Each stub registers itself in the backend registry
- [ ] `Available()` returns an error indicating the backend is not yet implemented
- [ ] All other methods return a descriptive "not implemented" error
- [ ] Stubs compile and do not break the build

### REQ-003-021: Backend Error Semantics

All backend methods MUST return errors that are categorizable using `errors.Is` and `errors.As`. Common sentinel errors MUST be defined for standard failure modes.

**Acceptance criteria:**
- [ ] `ErrVMNotFound` is returned when a named VM does not exist
- [ ] `ErrVMAlreadyExists` is returned when creating a VM with a name that is already in use
- [ ] `ErrVMNotRunning` is returned when an operation requires a running VM but it is not running
- [ ] `ErrBackendNotAvailable` is returned when a backend's prerequisites are not met
- [ ] `ErrNotImplemented` is returned by stub backends
- [ ] All errors wrap with context using `fmt.Errorf("...: %w", err)`

### REQ-003-022: Context Cancellation

All backend methods that accept `context.Context` MUST respect context cancellation and deadlines. Long-running operations (Create, Start, Stop, Destroy, Exec) MUST terminate promptly when the context is cancelled.

**Acceptance criteria:**
- [ ] `Create` aborts and cleans up partial state on context cancellation
- [ ] `Exec` kills the remote process on context cancellation
- [ ] Methods return `context.Canceled` or `context.DeadlineExceeded` as appropriate
- [ ] No goroutine leaks occur when context is cancelled

### REQ-003-023: Credential Isolation from Lima YAML

Credentials MUST NOT be passed through Lima's `env` YAML field. Lima's `env` field writes values to the instance config file on disk, which violates the "never persist credentials to disk" rule. All credential injection MUST flow exclusively through SSH using the `SendEnv`/`AcceptEnv` mechanism as defined in [007-connection.md](007-connection.md).

**Acceptance criteria:**
- [ ] The generated Lima YAML `env` field MUST NOT contain any values from `VMConfig.EnvVars` that are marked as sensitive (keys matching `*_TOKEN`, `*_KEY`, `*_SECRET`, `*_PASSWORD`)
- [ ] Non-sensitive environment variables (e.g., `PROJECT_NAME`) MAY be passed through Lima's `env` field
- [ ] Sensitive credentials are injected at connection time via SSH `SendEnv`/`AcceptEnv`
- [ ] No credential values appear in any file under `~/.lima/`

### REQ-003-024: Base Image Resolution

Base image resolution MUST use a built-in mapping from short names (e.g., `ubuntu:24.04`) to Lima template references. The mapping is embedded in the binary and does not require external lookups.

**Acceptance criteria:**
- [ ] Short names like `ubuntu:24.04`, `ubuntu:22.04`, `debian:12` are resolved to Lima-compatible image URLs with architecture-appropriate variants
- [ ] The mapping is compiled into the binary (not loaded from an external file at runtime)
- [ ] An unrecognized short name produces a fatal error listing available short names
- [ ] Full URLs are passed through unmodified for advanced use cases

## Design

### Core Interfaces

All interfaces are defined in `internal/backend/backend.go`:

```go
package backend

import (
    "context"
    "time"
)

// VMStatus represents the current state of a VM.
type VMStatus string

const (
    StatusCreating VMStatus = "creating"
    StatusRunning  VMStatus = "running"
    StatusStopped  VMStatus = "stopped"
    StatusError    VMStatus = "error"
)

// VMInfo describes a VM instance.
type VMInfo struct {
    Name      string    `json:"name"`
    Status    VMStatus  `json:"status"`
    Backend   string    `json:"backend"`
    CPUs      int       `json:"cpus"`
    Memory    string    `json:"memory"`
    Disk      string    `json:"disk"`
    IP        string    `json:"ip,omitempty"`
    CreatedAt time.Time `json:"created_at"`
}

// SSHConfig holds the information needed to SSH into a VM.
type SSHConfig struct {
    Host         string `json:"host"`
    Port         int    `json:"port"`
    User         string `json:"user"`
    IdentityFile string `json:"identity_file"`
    ProxyCommand string `json:"proxy_command,omitempty"` // Used for VSOCK transport
    ForwardAgent bool   `json:"forward_agent"`           // Default: false
}

// ExecResult holds the result of a command executed inside a VM.
type ExecResult struct {
    Stdout   string `json:"stdout"`
    Stderr   string `json:"stderr"`
    ExitCode int    `json:"exit_code"`
}

// SnapshotInfo describes a VM snapshot.
type SnapshotInfo struct {
    Name      string    `json:"name"`
    CreatedAt time.Time `json:"created_at"`
    Size      int64     `json:"size_bytes"`
}

// Backend is the core interface that all VM backends MUST implement.
type Backend interface {
    // Name returns the backend's registered name (e.g., "lima", "docker").
    Name() string

    // Available returns nil if the backend is usable on this system,
    // or an error explaining why not (missing binary, wrong OS, etc.).
    Available() error

    // Create provisions a new VM with the given name and configuration.
    Create(ctx context.Context, name string, cfg VMConfig) error

    // Start boots a stopped VM.
    Start(ctx context.Context, name string) error

    // Stop shuts down a running VM.
    Stop(ctx context.Context, name string) error

    // Destroy removes a VM and all its associated resources.
    Destroy(ctx context.Context, name string) error

    // Status returns the current status of a named VM.
    Status(ctx context.Context, name string) (VMStatus, error)

    // List returns all VMs managed by this backend.
    List(ctx context.Context) ([]VMInfo, error)

    // SSHConfig returns SSH connection details for a running VM.
    SSHConfig(ctx context.Context, name string) (SSHConfig, error)

    // Exec runs a command inside the named VM and returns the result.
    Exec(ctx context.Context, name string, command []string) (ExecResult, error)
}

// Snapshotter is an optional interface for backends that support snapshots.
type Snapshotter interface {
    SnapshotCreate(ctx context.Context, name, tag string) error
    SnapshotApply(ctx context.Context, name, tag string) error
    SnapshotDelete(ctx context.Context, name, tag string) error
    SnapshotList(ctx context.Context, name string) ([]SnapshotInfo, error)
}

// Cloner is an optional interface for backends that support VM cloning.
type Cloner interface {
    Clone(ctx context.Context, src, dst string) error
}

// Syncer is an optional interface for backends that support file sync.
type Syncer interface {
    SyncTo(ctx context.Context, name, hostPath, guestPath string) error
    SyncFrom(ctx context.Context, name, guestPath, hostPath string) error
}
```

### VMConfig

```go
package backend

// NetworkMode defines how a VM connects to the network.
type NetworkMode string

const (
    NetworkNAT      NetworkMode = "nat"
    NetworkBridged  NetworkMode = "bridged"
    NetworkIsolated NetworkMode = "isolated"
)

// Mount defines a host directory to mount inside the VM.
type Mount struct {
    HostPath  string `json:"host_path"`
    GuestPath string `json:"guest_path"`
    Writable  bool   `json:"writable"`
}

// ProvisionScript defines a script to run during VM provisioning.
type ProvisionScript struct {
    // Mode is "system" (runs as root) or "user" (runs as default user).
    Mode   string `json:"mode"`
    Script string `json:"script"`
}

// VMConfig defines the desired state of a VM.
type VMConfig struct {
    // CPUs is the number of virtual CPUs. Default: 4.
    CPUs int `json:"cpus"`

    // Memory is the amount of RAM (e.g., "4GiB", "8GiB"). Default: "8GiB".
    Memory string `json:"memory"`

    // Disk is the virtual disk size (e.g., "50GiB", "100GiB"). Default: "100GiB".
    Disk string `json:"disk"`

    // BaseImage is the OS image to use (e.g., "ubuntu:24.04").
    // Resolved via built-in short-name mapping embedded in the binary.
    // Default: "ubuntu:24.04".
    BaseImage string `json:"base_image"`

    // Mounts defines host directories to mount in the guest.
    // Default: empty (no mounts — security first).
    Mounts []Mount `json:"mounts"`

    // NetworkMode controls network connectivity. Default: "nat".
    NetworkMode NetworkMode `json:"network_mode"`

    // ProvisionScripts are scripts to run inside the VM after creation.
    ProvisionScripts []ProvisionScript `json:"provision_scripts"`

    // EnvVars are environment variables to inject into the VM.
    // Sensitive values (tokens, keys) are injected at runtime via SSH
    // SendEnv/AcceptEnv — never persisted to disk in Lima YAML or in the guest.
    // Non-sensitive values may be passed through Lima's env field.
    EnvVars map[string]string `json:"env_vars"`

    // BackendOptions holds backend-specific configuration.
    // For Lima: vmType, mountType, sshType, etc.
    BackendOptions map[string]any `json:"backend_options"`
}
```

### Backend Registry

The registry is defined in `internal/backend/registry.go`:

```go
package backend

import (
    "fmt"
    "sort"
    "sync"
)

var (
    mu       sync.RWMutex
    backends = make(map[string]Backend)
)

// Register adds a backend to the registry. Panics if a backend with the
// same name is already registered. Called from backend init() functions.
func Register(name string, b Backend) {
    mu.Lock()
    defer mu.Unlock()
    if _, exists := backends[name]; exists {
        panic(fmt.Sprintf("backend %q already registered", name))
    }
    backends[name] = b
}

// Get returns the backend registered under the given name.
func Get(name string) (Backend, error) {
    mu.RLock()
    defer mu.RUnlock()
    b, ok := backends[name]
    if !ok {
        return nil, fmt.Errorf("backend %q not registered; available: %v: %w",
            name, List(), ErrBackendNotAvailable)
    }
    return b, nil
}

// List returns the names of all registered backends, sorted alphabetically.
func List() []string {
    mu.RLock()
    defer mu.RUnlock()
    names := make([]string, 0, len(backends))
    for name := range backends {
        names = append(names, name)
    }
    sort.Strings(names)
    return names
}

// Default returns the first available backend, preferring "lima".
func Default() (Backend, error) {
    mu.RLock()
    defer mu.RUnlock()

    // Prefer Lima if available.
    if b, ok := backends["lima"]; ok {
        if err := b.Available(); err == nil {
            return b, nil
        }
    }

    // Fall back to first available backend.
    for _, b := range backends {
        if err := b.Available(); err == nil {
            return b, nil
        }
    }

    return nil, fmt.Errorf("no available backends; registered: %v: %w",
        List(), ErrBackendNotAvailable)
}
```

### Sentinel Errors

Defined in `internal/backend/errors.go`:

```go
package backend

import "errors"

var (
    ErrVMNotFound         = errors.New("vm not found")
    ErrVMAlreadyExists    = errors.New("vm already exists")
    ErrVMNotRunning       = errors.New("vm not running")
    ErrBackendNotAvailable = errors.New("backend not available")
    ErrNotImplemented     = errors.New("not implemented")
)
```

### Lima Backend Structure

The Lima backend resides in `internal/backend/lima/` with the following files:

- `lima.go` — `Backend` interface implementation, `init()` registration
- `yaml.go` — Lima YAML generation from `VMConfig`
- `snapshot.go` — `Snapshotter` interface implementation (clone-based)
- `images.go` — Built-in short-name to image URL mapping
- `lima_test.go` — unit tests

Key implementation details for the Lima backend:

**Registration:**
```go
package lima

import "sd/internal/backend"

func init() {
    backend.Register("lima", New())
}
```

**YAML generation mapping:**

| VMConfig field     | Lima YAML field          | Notes                                              |
|--------------------|--------------------------|-----------------------------------------------------|
| CPUs               | `cpus`                   | Direct integer mapping                               |
| Memory             | `memory`                 | Direct string mapping (e.g., "8GiB")                 |
| Disk               | `disk`                   | Direct string mapping (e.g., "100GiB")               |
| BaseImage          | `images[].location`      | Resolved via built-in short-name mapping             |
| Mounts             | `mounts`                 | Empty array when no mounts specified                 |
| NetworkMode        | `networks` / none        | NAT is default Lima behavior; bridged adds `networks` entry |
| ProvisionScripts   | `provision`              | Array of `{mode, script}` objects                    |
| EnvVars (non-sensitive) | `env`               | Only non-sensitive env vars; credentials excluded    |
| BackendOptions     | Various                  | `vmType`, `mountType`, etc. merged into top-level YAML |

**Example generated Lima YAML:**

```yaml
vmType: "vz"
cpus: 4
memory: "8GiB"
disk: "100GiB"
images:
  - location: "https://cloud-images.ubuntu.com/releases/24.04/release/ubuntu-24.04-server-cloudimg-arm64.img"
    arch: "aarch64"
mounts: []
provision:
  - mode: system
    script: |
      apt-get update && apt-get install -y git curl
  - mode: user
    script: |
      echo "user setup complete"
env:
  PROJECT_NAME: "my-project"
ssh:
  localPort: 0
  forwardAgent: false
```

Note: Credentials such as `GITHUB_TOKEN` and `ANTHROPIC_API_KEY` are NOT included in the `env` section. They are injected at connection time via SSH `SendEnv`/`AcceptEnv` (see [007-connection.md](007-connection.md)).

**Clone-based snapshot flow (since VZ lacks native snapshots):**

1. `SnapshotCreate(ctx, "myvm", "before-refactor")`:
   - Stop VM if running (remember previous state)
   - Clone instance directory `~/.lima/myvm` to `~/.lima/.snapshots/myvm/before-refactor/` using `cp -c` (APFS clone) on macOS for near-instant creation
   - Write metadata to `$SD_HOME/vms/myvm/snapshots.yaml`
   - Restart VM if it was running
2. `SnapshotApply(ctx, "myvm", "before-refactor")`:
   - Stop VM
   - Replace `~/.lima/myvm` with contents from `~/.lima/.snapshots/myvm/before-refactor/`
   - Start VM
3. `SnapshotDelete(ctx, "myvm", "before-refactor")`:
   - Remove `~/.lima/.snapshots/myvm/before-refactor/`
   - Update `$SD_HOME/vms/myvm/snapshots.yaml`
4. `SnapshotList(ctx, "myvm")`:
   - Read `$SD_HOME/vms/myvm/snapshots.yaml` for metadata
   - List directories under `~/.lima/.snapshots/myvm/` for on-disk verification

**Base image short-name mapping (embedded in binary):**

```go
// imageMap is the built-in mapping from short names to Lima image URLs.
var imageMap = map[string][]ImageRef{
    "ubuntu:24.04": {
        {Location: "https://cloud-images.ubuntu.com/releases/24.04/release/ubuntu-24.04-server-cloudimg-arm64.img", Arch: "aarch64"},
        {Location: "https://cloud-images.ubuntu.com/releases/24.04/release/ubuntu-24.04-server-cloudimg-amd64.img", Arch: "x86_64"},
    },
    "ubuntu:22.04": {
        {Location: "https://cloud-images.ubuntu.com/releases/22.04/release/ubuntu-22.04-server-cloudimg-arm64.img", Arch: "aarch64"},
        {Location: "https://cloud-images.ubuntu.com/releases/22.04/release/ubuntu-22.04-server-cloudimg-amd64.img", Arch: "x86_64"},
    },
    // Additional mappings for debian, etc.
}
```

### Future Backend Stubs

Each stub follows this pattern (example for `avf`):

```go
package avf

import (
    "context"
    "fmt"

    "sd/internal/backend"
)

type avfBackend struct{}

func init() {
    backend.Register("avf", &avfBackend{})
}

func (b *avfBackend) Name() string { return "avf" }

func (b *avfBackend) Available() error {
    return fmt.Errorf("Apple Virtualization Framework backend is not yet implemented: %w",
        backend.ErrNotImplemented)
}

func (b *avfBackend) Create(ctx context.Context, name string, cfg backend.VMConfig) error {
    return fmt.Errorf("avf.Create: %w", backend.ErrNotImplemented)
}

// ... all other Backend methods return ErrNotImplemented similarly
```

### State Machine

VM state transitions enforced by all backends:

```
                  Create()
    (not exists) ---------> Creating
                                |
                                | (provisioning complete)
                                v
                  Start()    Stopped <-----+
    Running <-------------- /      \       |
       |        Stop()     /        \      |
       +------------------>          \     |
       |                    Destroy() \    | Stop()
       |   Destroy()           |       \   |
       +----------+            v        \  |
                  |        (not exists)  \ |
                  v                       \|
              (not exists)            Running
                                          |
                                          | (crash / error)
                                          v
                                        Error
                                          |
                                          | Stop() / Destroy()
                                          v
                                    Stopped / (not exists)
```

Valid transitions:
- `(none)` -> `Creating` (via `Create`)
- `Creating` -> `Running` or `Stopped` (backend-dependent; Lima leaves VMs running after create)
- `Stopped` -> `Running` (via `Start`)
- `Running` -> `Stopped` (via `Stop`)
- `Running` -> `Error` (via crash or internal failure)
- `Error` -> `Stopped` (via `Stop`)
- `Stopped` -> `(none)` (via `Destroy`)
- `Running` -> `(none)` (via `Destroy`)
- `Error` -> `(none)` (via `Destroy`)

## Error Handling

| Error Condition | Trigger | Severity | User Message | Recovery |
|---|---|---|---|---|
| Backend not available | `Available()` fails | Fatal | "Backend 'lima' is not available: limactl not found in $PATH. Install with: brew install lima" | Install the dependency |
| VM not found | Operation on non-existent VM | Fatal | "VM 'myvm' not found" | Check name with `sd list` |
| VM already exists | `Create` with existing name | Fatal | "VM 'myvm' already exists" | Use a different name or destroy first |
| VM not running | SSH/Exec on stopped VM | Fatal | "VM 'myvm' is not running. Start it with: sd start myvm" | Start the VM |
| Creation failed | `Create` encounters error | Fatal | "Failed to create VM 'myvm': [underlying error]" | Check logs, retry |
| Snapshot failed | Disk full or I/O error | Fatal | "Failed to create snapshot: [underlying error]" | Free disk space |
| Not implemented | Stub backend operation | Fatal | "Backend 'avf' does not implement this operation" | Use a different backend |
| Context cancelled | User cancels or timeout | Warning | "Operation cancelled" | Retry when ready |
| Mount warning | Mount points to $HOME or / | Warning | "Warning: mounting host $HOME into VM reduces security isolation" | Remove mount or acknowledge risk |

All errors are JSON-serializable when `--json` is active:

```json
{
  "error": {
    "code": "vm_not_found",
    "message": "VM 'myvm' not found",
    "backend": "lima"
  }
}
```

## Security Considerations

### Trust Boundaries

The backend system crosses the host-guest trust boundary. The VM is the primary isolation mechanism for running untrusted agent code.

- **Host -> Guest:** VMConfig, provisioning scripts, and environment variables flow from host to guest. Provisioning scripts MUST be treated as trusted (they come from the user's config, not from agents).
- **Guest -> Host:** Only SSH connections and file sync (if Syncer is used) flow back. Mount access MUST be explicitly opted into.

### Credentials

- Environment variables in `VMConfig.EnvVars` may contain secrets (tokens, API keys).
- Sensitive credentials MUST NOT be written to Lima YAML or any persistent config file. They MUST be injected at connection time via SSH `SendEnv`/`AcceptEnv` as defined in [007-connection.md](007-connection.md).
- Non-sensitive environment variables (e.g., `PROJECT_NAME`) MAY be passed through Lima's `env` field.

### Blast Radius

- If a VM is compromised, the blast radius is limited to: the VM's virtual disk, any mounted host directories, any network endpoints reachable from the VM's network mode.
- No-mount default ensures the host filesystem is not exposed.
- NAT default ensures the VM cannot be reached from the network.

### Mitigations

1. **No mounts by default** (REQ-003-017) — the host filesystem is never exposed unless explicitly configured
2. **Snapshot before destructive operations** — enables rollback if an agent corrupts the environment
3. **Credential isolation from Lima YAML** (REQ-003-023) — credentials are injected at runtime via SSH, never persisted to Lima config files
4. **Warning on dangerous mounts** — mounting $HOME produces a visible warning
5. **Network isolation option** — `NetworkIsolated` mode cuts off all network access

## Testing Strategy

### Unit Tests

| Requirement | Test Description |
|---|---|
| REQ-003-001 | Compile-time verification: code that assigns a non-conforming type to `Backend` fails to compile |
| REQ-003-002 | Mock backend returns error from `Available()`; verify error message is actionable |
| REQ-003-004 | Verify `VMStatus` JSON marshaling produces lowercase strings |
| REQ-003-005 | Verify `List()` returns empty slice, not nil, when no VMs exist |
| REQ-003-011 | Verify `VMConfig` default values: no mounts, NAT network |
| REQ-003-012 | Verify `NetworkMode` constants and JSON serialization |
| REQ-003-013 | Verify registry: Register, Get, List, Default, duplicate panic |
| REQ-003-015 | Verify Lima YAML generation from various `VMConfig` inputs |
| REQ-003-016 | Verify VZ defaults on arm64, QEMU fallback on other architectures |
| REQ-003-017 | Verify generated YAML has `mounts: []` when no mounts configured |
| REQ-003-021 | Verify sentinel errors work with `errors.Is` |
| REQ-003-023 | Verify Lima YAML does not contain sensitive env var values |
| REQ-003-024 | Verify base image short-name resolution and error on unknown name |

### Integration Tests

| Requirement | Test Description |
|---|---|
| REQ-003-002 | Lima `Available()` succeeds when `limactl` is installed |
| REQ-003-003 | Full lifecycle test: Create -> Start -> Stop -> Destroy a Lima VM |
| REQ-003-006 | SSH into a running Lima VM using returned `SSHConfig` |
| REQ-003-007 | Execute `echo hello` inside a Lima VM and verify output |
| REQ-003-018 | Create snapshot, modify VM, apply snapshot, verify state restored |
| REQ-003-019 | Create VM with provision script, verify script executed |

Integration tests MUST be gated behind a build tag (`//go:build integration`) since they require `limactl` and take significant time.

### Script Tests

| Requirement | Test Description |
|---|---|
| REQ-003-013 | Script test: `sd list --backend lima` returns empty list |
| REQ-003-020 | Script test: `sd create --backend avf myvm` returns "not implemented" error |

## Dependencies

### Depends On

- [001-architecture.md](001-architecture.md) — overall system architecture and package layout
- [002-cli.md](002-cli.md) — CLI command structure and output conventions
- Lima (`limactl` CLI) — runtime dependency for the default backend
- Go standard library `os/exec` — for running `limactl` commands
- Go standard library `encoding/json` — for VMInfo/SSHConfig serialization

### Depended On By

- [005-configuration.md](005-configuration.md) — configuration system reads backend defaults
- [006-provisioning.md](006-provisioning.md) — provisioning scripts are passed through VMConfig
- [007-connection.md](007-connection.md) — SSH connections use SSHConfig from the backend
- Session spec (TBD) — session management depends on VM lifecycle
- [004-security.md](004-security.md) — egress rules are applied at the network/VM level

## Revision History

| Date       | Author | Change Description |
|------------|--------|--------------------|
| 2026-03-27 | claude | Initial draft      |
| 2026-03-27 | claude | Review fixes: resolve OQ-1 (credential isolation from Lima YAML), OQ-2 (snapshot storage with APFS clones), OQ-3 (built-in image mapping); add ProxyCommand and ForwardAgent to SSHConfig; update defaults to CPUs=4/Memory=8GiB/Disk=100GiB; fix dependency references; fix test command syntax |
