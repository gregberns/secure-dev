# Architecture Design: sd

**Generated:** 2026-03-28
**Source:** 3-agent parallel design (system architect, interface designer, engineering strategist)

---

## System Architecture

### Module Decomposition

The system is organized around seven major subsystems, each with a single clearly-bounded
responsibility. The decomposition follows the spec's mandated package layout (REQ-001-014).

```
cmd/
  sd/
    main.go                    # Binary entry point. Calls internal/cmd.Execute(). <10 lines.

internal/
  cmd/                         # Cobra command tree. All user-facing entry points.
    root.go                    # Root command, persistent flags (--json, --verbose, --quiet, --config, --vm)
    create.go                  # sd create
    destroy.go                 # sd destroy
    start.go                   # sd start
    stop.go                    # sd stop
    list.go                    # sd list (alias: ls)
    status.go                  # sd status
    connect.go                 # sd connect (alias: c)
    exec.go                    # sd exec
    sync.go                    # sd sync to/from
    ssh_config.go              # sd ssh-config
    snapshot.go                # sd snapshot create/list/restore/delete
    config.go                  # sd config set/get/list/edit/validate/egress
    provision.go               # sd provision / sd provision list
    token.go                   # sd token github/rotate/revoke/list
    audit.go                   # sd audit
    security.go                # sd security status
    diff.go                    # sd diff
    doctor.go                  # sd doctor
    version.go                 # sd version
    logs.go                    # sd logs
    completion.go              # sd completion (bash/zsh/fish)

  backend/                     # REQ-001-001, REQ-003-001: Backend interface + registry + all implementations.
    backend.go                 # Backend interface, VMConfig, VMStatus, VMInfo, SSHConfig, ExecResult types
    errors.go                  # Sentinel errors: ErrVMNotFound, ErrVMAlreadyExists, etc.
    registry.go                # Register(), Get(), List(), Default() functions
    lima/
      lima.go                  # LimaBackend struct implementing Backend
      yaml.go                  # Lima YAML config generation from VMConfig
      snapshot.go              # Snapshotter interface implementation (APFS clone strategy)
      images.go                # Base image short-name resolution (ubuntu:24.04 -> URL)
    avf/
      avf.go                   # Stub: Available() returns ErrNotImplemented
    docker/
      docker.go                # Stub: Available() returns ErrNotImplemented
    incus/
      incus.go                 # Stub: Available() returns ErrNotImplemented

  config/                      # REQ-001-009, REQ-005-001..016: Viper-based configuration loading.
    config.go                  # Load(), Merge(), Validate() — Viper setup and precedence
    defaults.go                # Compiled-in defaults (backend, cpus, memory, disk, image, egress list)
    schema.go                  # Struct types for config file sections (Defaults, Security, VMsConfig)
    resolve.go                 # Env-var reference resolution ("${VAR}" -> host env value)

  state/                       # REQ-001-001, REQ-001-011: VM metadata persistence only.
    state.go                   # Read/write/delete $SD_HOME/vms/<name>/config.yaml
    types.go                   # VMState, VMMetadata structs

  provision/                   # REQ-001-001, REQ-006-001..016: Provisioning engine.
    provision.go               # Engine: orchestrate module execution, topological sort, readiness probes
    module.go                  # Module, Script, Probe types; load/validate module YAML
    loader.go                  # Load built-in modules (go:embed) + custom modules (~/.sd/provisions/)
    executor.go                # Execute scripts in VM via backend.Exec, apply set -eux -o pipefail
    modules/                   # Embedded module YAML files
      base.yaml
      claude-code.yaml
      docker.yaml
      golang.yaml
      rust.yaml
      python.yaml
      github-cli.yaml

  connection/                  # REQ-001-001, REQ-007-001..021: SSH, tmux, sync.
    connect.go                 # Connect(), Exec(), SSHConfig() — implements Connector interface
    keys.go                    # SSH key pair generation, per-VM key management (Ed25519)
    sshconfig.go               # Write/remove ~/.ssh/config.d/sd-<name> fragments
    tmux.go                    # tmux session attach/create logic
    sync.go                    # rsync-over-SSH, file sync (to/from), diff, watch
    env.go                     # Environment variable injection via SendEnv

  security/                    # REQ-001-001, REQ-004-001..027: Security enforcement.
    mount.go                   # Mount path validation; reject $HOME, sensitive dirs
    egress.go                  # Default allowlist; merge user allowlist; iptables + dnsmasq config generation
    credentials.go             # Credential scoping recommendations; PAT type detection
    audit.go                   # Audit log write/read/verify; hash chain (SHA-256 prev_hash)

  ui/                          # Cross-cutting: output formatting for all packages.
    output.go                  # Print to correct stream (stdout vs stderr); respect --quiet
    json.go                    # JSON output mode; structured error schema
    progress.go                # Progress indicators (spinner, step counters)
    table.go                   # Tabular output for list/status/config list
```

### Module Responsibility Boundaries

| Package | Owns exclusively |
|---|---|
| `cmd/` | CLI surface: flag parsing, argument validation, wiring services together, exit codes |
| `backend/` | Backend interface contract; all VM lifecycle operations; VMConfig/VMStatus/VMInfo types; backend registry; Lima YAML generation |
| `config/` | Viper setup; config file loading and precedence; env-var reference syntax; default values; config validation |
| `state/` | $SD_HOME/vms/<name>/config.yaml CRUD; directory creation with correct permissions (0700/0600) |
| `provision/` | Module YAML loading; topological dependency resolution; script execution ordering; readiness probes; embedded module files |
| `connection/` | SSH key generation; ~/.ssh/config.d/ management; interactive SSH sessions; tmux session management; rsync file sync; env injection |
| `security/` | Mount path validation; egress policy construction; audit log I/O and hash chain |
| `ui/` | All terminal output formatting; JSON serialization of command results; progress display |

No package may directly perform work that belongs in another's exclusive domain.

### Dependency Graph

The system has four layers. Dependencies only flow downward (or laterally to cross-cutting packages):

```
Layer 1 (User interface):
  internal/cmd/

Layer 2 (Domain services):
  internal/backend/        internal/provision/        internal/connection/
  internal/state/          internal/security/         internal/config/

Layer 3 (Infrastructure implementations):
  internal/backend/lima/   internal/backend/avf/
  internal/backend/docker/ internal/backend/incus/

Layer 4 (Cross-cutting, zero dependencies on layers 1-3):
  internal/ui/
```

Concrete dependency rules:

1. `internal/cmd/` imports: `backend`, `config`, `state`, `provision`, `connection`, `security`, `ui`. Never imports `lima/`, `avf/`, or other backend sub-packages directly — always through the registry.
2. `internal/backend/` (interface package) imports: nothing in `internal/` except `ui` for progress during long operations.
3. `internal/backend/lima/` imports: `backend` (for interface types), `ui`. Never imports `cmd/`, `provision/`, `connection/`.
4. `internal/provision/` imports: `backend` (for Exec), `config`, `ui`. Never imports `connection/` or `security/`.
5. `internal/connection/` imports: `backend` (for SSHConfig), `config`, `state`, `security` (for credential env var names), `ui`.
6. `internal/security/` imports: `config`, `ui`. No dependency on `backend/` or `connection/`.
7. `internal/state/` imports: `config` (for SD_HOME), `ui`. No dependency on `backend/`.
8. `internal/config/` imports: only `ui` for error formatting.
9. `internal/ui/` imports: nothing in `internal/`. Standard library only.

### Interface Locations (Dependency Inversion)

Following Go convention, interfaces are defined in the **consumer** package, not the provider:

- `backend.Backend` defined in `internal/backend/backend.go`. All backends implement it.
- `provision.Provisioner` defined in `internal/provision/provision.go`.
- `connection.Connector` defined in `internal/connection/connect.go`.
- `connection.FileSyncer` defined in `internal/connection/sync.go`.
- `backend.Snapshotter`, `backend.Cloner`, `backend.FileSync` are optional capability interfaces in `internal/backend/backend.go`. Callers use type assertions.

### Wiring at Startup

`internal/cmd/root.go` creates a single `App` struct (holding all injected dependencies)
in cobra's `PersistentPreRunE`. Backend resolution happens once per command invocation
via `backend.Get(cfg.Defaults.Backend)` from the registry. Lima's `init()` function in
`internal/backend/lima/lima.go` calls `backend.Register("lima", &LimaBackend{})`. The
blank import `_ "github.com/org/sd/internal/backend/lima"` in `cmd/sd/main.go` triggers
registration.

### Configuration File Locations

| File | Purpose |
|---|---|
| `$SD_HOME/config.yaml` (default: `~/.sd/config.yaml`) | User-level config |
| `.sd/config.yaml` (walk up from cwd) | Project-level config |
| `$SD_HOME/vms/<name>/config.yaml` | Per-VM state + config |
| `$SD_HOME/audit.log` | Audit log |
| `~/.ssh/config.d/sd-<name>` | SSH config fragments per VM |
| `$SD_HOME/vms/<name>/ssh/id_ed25519[.pub]` | Per-VM SSH key pair |
| `~/.sd/provisions/*.yaml` | User-defined custom provision modules |

Directory permissions: `$SD_HOME` and all VM directories created with mode `0700`.
SSH private keys use `0600`. (REQ-001-014)

### Security Architecture Summary

| Security Layer | Enforced by | Key mechanism |
|---|---|---|
| Layer 1: VM Isolation | `backend/` package | All operations go through Backend interface; Lima uses VZ/QEMU hypervisor |
| Layer 2: Mount Restrictions | `security/mount.go` called from `cmd/create.go` | Validate before any backend call; symlink resolution; reject $HOME and sensitive dirs |
| Layer 3: Egress Control | `security/egress.go` + provisioning scripts | Default allowlist embedded in `config/defaults.go`; iptables rules + dnsmasq config generated as provisioning scripts |
| Layer 4: Credential Scoping | `security/credentials.go` + `connection/env.go` | ${VAR} references only in config; resolved at connect time; injected via SSH SendEnv; never written to files |

### Greenfield Initialization Sequence

1. `go.mod` + `cmd/sd/main.go` (skeleton)
2. `internal/ui/` — no dependencies, enables clean output from day one
3. `internal/config/` — needed by everything
4. `internal/backend/backend.go` + `errors.go` + `registry.go` — interface definition
5. `internal/state/` — VM metadata CRUD
6. `internal/security/mount.go` — mount validation (pure function, testable immediately)
7. `internal/backend/lima/` — Lima implementation
8. `internal/backend/avf|docker|incus/` — stubs (trivial, register and return ErrNotImplemented)
9. `internal/connection/keys.go` + `sshconfig.go` — key gen and SSH config fragments
10. `internal/cmd/create.go` + `destroy.go` + `list.go` + `status.go` — core VM lifecycle
11. `internal/provision/` — module system
12. `internal/connection/connect.go` + `tmux.go` + `sync.go` — connect, exec, sync
13. `internal/security/egress.go` + `audit.go` + `credentials.go`
14. Remaining `cmd/` commands (config, token, audit, security, diff, doctor, snapshot, version, logs)

---

## Interfaces & Contracts

### Package `internal/backend` — Core Types

```go
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

// VMInfo describes a running or stopped VM instance.
// REQ-003-005
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
// REQ-003-006
type SSHConfig struct {
    Host         string `json:"host"`
    Port         int    `json:"port"`
    User         string `json:"user"`
    IdentityFile string `json:"identity_file"`
    // ProxyCommand is set for VSOCK transport (Lima+VZ). REQ-007-005
    ProxyCommand string `json:"proxy_command,omitempty"`
    ForwardAgent bool   `json:"forward_agent"` // always false; REQ-004-027
}

// ExecResult holds the result of a command run inside a VM.
// REQ-003-007
type ExecResult struct {
    Stdout   string `json:"stdout"`
    Stderr   string `json:"stderr"`
    ExitCode int    `json:"exit_code"`
}

// SnapshotInfo describes a VM snapshot.
// REQ-003-008, REQ-003-018
type SnapshotInfo struct {
    Name      string    `json:"name"`
    CreatedAt time.Time `json:"created_at"`
    Size      int64     `json:"size_bytes"`
}

// NetworkMode defines how a VM connects to the network.
// REQ-003-012
type NetworkMode string

const (
    NetworkNAT      NetworkMode = "nat"      // default; outbound through host NAT
    NetworkBridged  NetworkMode = "bridged"  // VM gets LAN-visible IP
    NetworkIsolated NetworkMode = "isolated" // no network
)

// Mount defines a host directory to mount inside the VM.
// REQ-003-011, REQ-004-003, REQ-004-004
type Mount struct {
    HostPath  string `json:"host_path"`
    GuestPath string `json:"guest_path"`
    Writable  bool   `json:"writable"` // false = read-only (default)
}

// ProvisionScript defines a script to run during VM provisioning.
// REQ-003-019, REQ-006-005
type ProvisionScript struct {
    Mode   string `json:"mode"`   // "system" (root) or "user"
    Script string `json:"script"` // shell script content with set -eux prepended
}

// VMConfig defines the desired state of a VM at creation time.
// Input to Backend.Create. Authoritative contract between CLI layer and all backends.
// REQ-003-011
type VMConfig struct {
    CPUs           int               `json:"cpus"`         // Default: 4 (REQ-005-004)
    Memory         string            `json:"memory"`       // e.g., "8GiB". Default: "8GiB"
    Disk           string            `json:"disk"`         // e.g., "100GiB". Default: "100GiB"
    BaseImage      string            `json:"base_image"`   // e.g., "ubuntu:24.04". Default: "ubuntu:24.04" (REQ-003-024)
    Mounts         []Mount           `json:"mounts"`       // Default: empty (REQ-003-017, REQ-004-003)
    NetworkMode    NetworkMode       `json:"network_mode"` // Default: NetworkNAT
    ProvisionScripts []ProvisionScript `json:"provision_scripts"`
    // EnvVars: sensitive values MUST NOT be placed in Lima's env YAML field.
    // Injected at runtime via SSH SendEnv/AcceptEnv. (REQ-003-023)
    EnvVars        map[string]string `json:"env_vars"`
    BackendOptions map[string]any    `json:"backend_options"` // backend-specific settings (e.g., vmType for Lima)
}
```

### Package `internal/backend` — Backend Interface

```go
// Backend is the core interface every VM runtime must implement.
// REQ-003-001, REQ-003-002, REQ-003-003
type Backend interface {
    // Name returns the backend's registered name (e.g., "lima").
    Name() string

    // Available returns nil if the backend can be used on this system.
    // Returns an actionable error naming the missing dependency otherwise.
    // REQ-003-002
    Available() error

    // Create provisions a new VM. Blocks until the VM is running or fails.
    // Returns ErrVMAlreadyExists if a VM with the name already exists.
    // REQ-003-003, REQ-003-022
    Create(ctx context.Context, name string, cfg VMConfig) error

    // Start boots a stopped VM. No-op if already running.
    Start(ctx context.Context, name string) error

    // Stop shuts down a running VM. No-op if already stopped.
    Stop(ctx context.Context, name string) error

    // Destroy removes a VM and all its resources.
    // Returns ErrVMNotFound if the VM does not exist.
    Destroy(ctx context.Context, name string) error

    // Status returns the current status of a named VM.
    // Returns ErrVMNotFound if the VM does not exist.
    // REQ-003-004
    Status(ctx context.Context, name string) (VMStatus, error)

    // List returns all VMs managed by this backend.
    // Returns an empty (non-nil) slice if no VMs exist.
    // REQ-003-005
    List(ctx context.Context) ([]VMInfo, error)

    // SSHConfig returns connection details for a running VM.
    // Returns ErrVMNotRunning if the VM is not running.
    // REQ-003-006
    SSHConfig(ctx context.Context, name string) (SSHConfig, error)

    // Exec runs a command inside the named VM.
    // Respects ctx cancellation for long-running commands.
    // REQ-003-007, REQ-003-022
    Exec(ctx context.Context, name string, command []string) (ExecResult, error)
}
```

### Package `internal/backend` — Optional Capability Interfaces

Callers MUST type-assert before using; never assume a Backend implements these.

```go
// Snapshotter is an optional interface for VM snapshot management.
// REQ-003-008
type Snapshotter interface {
    // On macOS, uses APFS clones (cp -c) for near-instant creation. REQ-003-018
    SnapshotCreate(ctx context.Context, name, tag string) error
    SnapshotApply(ctx context.Context, name, tag string) error
    SnapshotDelete(ctx context.Context, name, tag string) error
    SnapshotList(ctx context.Context, name string) ([]SnapshotInfo, error)
}

// Cloner is an optional interface for VM cloning.
// REQ-003-009
type Cloner interface {
    Clone(ctx context.Context, src, dst string) error
}

// FileSync is an optional interface for file synchronization.
// Named FileSync here to avoid collision with connection.FileSyncer.
// REQ-003-010
type FileSync interface {
    SyncTo(ctx context.Context, name, hostPath, guestPath string) error
    SyncFrom(ctx context.Context, name, guestPath, hostPath string) error
}
```

### Package `internal/backend` — Registry

```go
// internal/backend/registry.go  (REQ-003-013)
var (
    mu       sync.RWMutex
    backends = map[string]Backend{}
)

func Register(name string, b Backend) {
    mu.Lock()
    defer mu.Unlock()
    if _, exists := backends[name]; exists {
        panic(fmt.Sprintf("backend %q already registered", name))
    }
    backends[name] = b
}

func Get(name string) (Backend, error) {
    mu.RLock()
    defer mu.RUnlock()
    b, ok := backends[name]
    if !ok {
        return nil, fmt.Errorf("%w: %q (available: %s)",
            ErrBackendNotFound, name, strings.Join(List(), ", "))
    }
    return b, nil
}

// List returns sorted names of all registered backends.
func List() []string

// Default returns the first available backend, preferring "lima".
func Default() (Backend, error)
```

### Package `internal/backend` — Sentinel Errors

```go
// internal/backend/errors.go  (REQ-003-021)
var (
    ErrVMNotFound          = errors.New("vm not found")
    ErrVMAlreadyExists     = errors.New("vm already exists")
    ErrVMNotRunning        = errors.New("vm not running")
    ErrBackendNotAvailable = errors.New("backend not available")
    ErrBackendNotFound     = errors.New("backend not registered")
    ErrNotImplemented      = errors.New("not implemented")
)
```

### Package `internal/config` — Value Types

```go
package config

import "time"

// Config is the fully resolved configuration from all sources. REQ-005-006
type Config struct {
    Defaults Defaults         `yaml:"defaults"    mapstructure:"defaults"`
    Security Security         `yaml:"security"    mapstructure:"security"`
    VMs      map[string]VMDef `yaml:"vms"         mapstructure:"vms"`
}

// Defaults holds default values for VM creation. REQ-005-004
type Defaults struct {
    Backend string `yaml:"backend"  mapstructure:"backend"`   // default: "lima"
    CPUs    int    `yaml:"cpus"     mapstructure:"cpus"`      // default: 4
    Memory  string `yaml:"memory"   mapstructure:"memory"`   // default: "8GiB"
    Disk    string `yaml:"disk"     mapstructure:"disk"`     // default: "100GiB"
    Image   string `yaml:"image"    mapstructure:"image"`    // default: "ubuntu:24.04"
    VM      string `yaml:"vm"       mapstructure:"vm"`       // default: ""
}

// Security holds security policy settings. REQ-004-006, REQ-004-007, REQ-005-004
type Security struct {
    // EgressAllowlist: default is 11 entries from REQ-004-007.
    EgressAllowlist []string `yaml:"egress_allowlist" mapstructure:"egress_allowlist"`
    // MountPolicy valid values: MountPolicyNone | MountPolicyReadonly | MountPolicyProject
    MountPolicy    string   `yaml:"mount_policy"     mapstructure:"mount_policy"`
    // SensitivePaths merged with built-in list. REQ-004-023
    SensitivePaths []string `yaml:"sensitive_paths,omitempty" mapstructure:"sensitive_paths"`
}

const (
    MountPolicyNone     = "none"     // no mounts (default)
    MountPolicyReadonly = "readonly" // mount CWD as read-only
    MountPolicyProject  = "project"  // mount project root as read-write
)

// VMDef is a VM definition as written in user or project config file.
// Not the same as the persisted per-VM state file (VMConfig below). REQ-005-006
type VMDef struct {
    CPUs       int               `yaml:"cpus"       mapstructure:"cpus"`
    Memory     string            `yaml:"memory"     mapstructure:"memory"`
    Disk       string            `yaml:"disk"       mapstructure:"disk"`
    Image      string            `yaml:"image"      mapstructure:"image"`
    Backend    string            `yaml:"backend"    mapstructure:"backend"`
    Provisions []string          `yaml:"provisions" mapstructure:"provisions"`
    // Env holds ${VAR} references ONLY — never literal secrets. REQ-005-008
    Env        map[string]string `yaml:"env"        mapstructure:"env"`
}

// VMConfig is the full persisted state for a specific VM instance.
// Stored at $SD_HOME/vms/<name>/config.yaml. REQ-005-007
type VMConfig struct {
    Name        string            `yaml:"name"`
    Backend     string            `yaml:"backend"`
    CPUs        int               `yaml:"cpus"`
    Memory      string            `yaml:"memory"`
    Disk        string            `yaml:"disk"`
    Image       string            `yaml:"image"`
    Provisions  []string          `yaml:"provisions"`
    Env         map[string]string `yaml:"env"`          // ${VAR} references only
    State       VMState           `yaml:"state"`
    BackendMeta map[string]any    `yaml:"backend_meta"` // opaque backend-specific data
}

// VMState is the runtime state tracked inside the per-VM config file. REQ-005-007
type VMState struct {
    Status      string    `yaml:"status"`
    CreatedAt   time.Time `yaml:"created_at"`
    LastStarted time.Time `yaml:"last_started,omitempty"`
    LastStopped time.Time `yaml:"last_stopped,omitempty"`
}

// VMStatus constants for VMState.Status. REQ-005-007
const (
    VMStatusCreated = "created"
    VMStatusRunning = "running"
    VMStatusStopped = "stopped"
    VMStatusError   = "error"
)

// ValidationResult holds the result of validating one config file. REQ-005-013
type ValidationResult struct {
    Path   string        `json:"path"`
    Valid  bool          `json:"valid"`
    Errors []ConfigError `json:"errors"`
}

type ConfigError struct {
    Line    int    `json:"line,omitempty"`
    Key     string `json:"key,omitempty"`
    Message string `json:"message"`
}

// ConfigEntry is a single resolved key-value pair with source attribution.
// Used by sd config list --json. REQ-005-011
type ConfigEntry struct {
    Key    string `json:"key"`
    Value  any    `json:"value"`
    Source string `json:"source"`
}

const (
    ConfigSourceCLIFlag        = "cli flag"
    ConfigSourceEnvVar         = "environment variable"
    ConfigSourceProjectConfig  = "project-level config"
    ConfigSourceUserConfig     = "user-level config"
    ConfigSourceVMConfig       = "vm config"
    ConfigSourceBuiltinDefault = "built-in default"
)
```

### Package `internal/config` — Loader Interface

```go
// Loader provides access to the resolved, merged configuration.
// REQ-005-014
type Loader interface {
    Load() error
    Get() *Config
    // GetForVM returns resolved configuration for a specific VM (REQ-005-015).
    GetForVM(name string) (*VMConfig, error)
    // Source returns the origin of a config key; one of ConfigSource* constants.
    Source(key string) string
    Validate() ([]ValidationResult, error)
    // Set writes a key-value pair to "user" or "project" config. REQ-005-010
    Set(key string, value any, target string) error
    // Resolve expands ${VAR} references from host environment.
    // MUST NOT log or persist the resolved value. REQ-005-008
    Resolve(s string) (resolved string, unresolved []string)
    // List returns all keys with values and sources (sensitive values shown as ${VAR} reference). REQ-005-011
    List() []ConfigEntry
}
```

### Package `internal/provision` — Value Types

```go
package provision

import "time"

// Module represents a single provisioning module loaded from YAML. REQ-006-003
type Module struct {
    Name        string            `yaml:"name"`
    Description string            `yaml:"description"`
    DependsOn   []string          `yaml:"depends_on,omitempty"`
    Scripts     []Script          `yaml:"scripts"`
    // Checksums required for modules that download binaries. REQ-004-028, REQ-006-016
    Checksums   map[string]string `yaml:"checksums,omitempty"`
    Probe       *Probe            `yaml:"probe,omitempty"`
}

// Script represents one script block within a module. REQ-006-005
type Script struct {
    Mode   string `yaml:"mode"`   // "system" (root) or "user"
    Script string `yaml:"script"` // set -eux -o pipefail is prepended
}

// Probe defines a readiness check for a module. REQ-006-008
type Probe struct {
    Command  string        `yaml:"command"`
    Interval time.Duration `yaml:"interval"` // default: 5s
    Timeout  time.Duration `yaml:"timeout"`  // default: 5m
}

// ModuleStatus is the execution state of a module during provisioning. REQ-006-009
type ModuleStatus string

const (
    ModuleStatusPending   ModuleStatus = "pending"
    ModuleStatusRunning   ModuleStatus = "running"
    ModuleStatusCompleted ModuleStatus = "completed"
    ModuleStatusFailed    ModuleStatus = "failed"
)

// ProvisionState tracks overall provisioning progress for a VM.
// Exposed via sd status --json. REQ-006-009
type ProvisionState struct {
    VMName   string                  `json:"vm_name"`
    Started  time.Time               `json:"started"`
    Finished *time.Time              `json:"finished,omitempty"`
    Modules  []ModuleExecutionStatus `json:"modules"`
}

type ModuleExecutionStatus struct {
    Name      string       `json:"name"`
    Status    ModuleStatus `json:"status"`
    StartedAt *time.Time   `json:"started_at,omitempty"`
    EndedAt   *time.Time   `json:"ended_at,omitempty"`
    Error     string       `json:"error,omitempty"`
}
```

### Package `internal/provision` — Provisioner Interface

```go
// Provisioner runs provisioning modules against a VM.
// REQ-006-001, REQ-006-004, REQ-006-010
type Provisioner interface {
    // Provision runs the specified modules (and their transitive dependencies)
    // against the named VM. base module always runs first (REQ-006-002).
    Provision(ctx context.Context, vmName string, modules []string) (*ProvisionState, error)

    // ListModules returns all available modules (built-in and custom).
    // REQ-006-007, REQ-002-006
    ListModules() ([]*Module, error)

    GetModule(name string) (*Module, error)

    // ValidateModules checks that all module names are known and that the
    // dependency graph is acyclic. REQ-006-004
    ValidateModules(names []string) error
}
```

Provisioning pipeline (Template Method pattern):

```go
// internal/provision/provision.go
func (e *Engine) Run(ctx context.Context, vm string, modules []string) error {
    // REQ-006-004: topological sort
    ordered, err := e.resolve(modules)
    if err != nil { return err }

    for _, mod := range ordered {
        if err := e.execModule(ctx, vm, mod); err != nil {
            return fmt.Errorf("module %s: %w", mod.Name, err)
        }
    }

    // REQ-006-008: readiness probes
    for _, mod := range ordered {
        if mod.Probe != nil {
            if err := e.runProbe(ctx, vm, mod.Probe); err != nil {
                return fmt.Errorf("probe %s: %w", mod.Name, err)
            }
        }
    }
    return nil
}
```

### Package `internal/connection` — Value Types

```go
package connection

// ConnectOpts configures an interactive SSH+tmux session.
// REQ-007-001, REQ-007-007, REQ-007-008, REQ-007-009
type ConnectOpts struct {
    VMName    string
    Session   string            // tmux session name; default "sd-<VMName>" (REQ-007-008)
    NewWindow bool              // open new tmux window in existing session (REQ-007-010)
    NoTmux    bool              // bypass tmux for raw SSH session (REQ-007-012); mutually exclusive with NewWindow
    Forwards  []PortForward     // port-forward specifications (REQ-007-007)
    NoStart   bool              // prevent auto-starting a stopped VM (REQ-007-002)
    // EnvVars: resolved credential values to inject via SendEnv. REQ-007-019, REQ-004-011
    EnvVars   map[string]string
}

// PortForward specifies a host-to-guest port mapping. REQ-007-007
type PortForward struct {
    BindAddr  string // host bind address; default "127.0.0.1"
    HostPort  int
    GuestPort int
}

// ExecOpts configures a non-interactive command execution via SSH.
// REQ-007-013, REQ-007-014
type ExecOpts struct {
    EnvVars map[string]string // resolved values to inject via SendEnv (REQ-007-019)
    JSON    bool
}

// ExecResult holds the output of a non-interactive remote execution. REQ-007-014
// Note: mirrors backend.ExecResult but defined here to avoid a dependency cycle.
type ExecResult struct {
    ExitCode int    `json:"exit_code"`
    Stdout   string `json:"stdout"`
    Stderr   string `json:"stderr"`
}

// SSHConfigData holds parsed SSH configuration for a single VM. REQ-007-006
type SSHConfigData struct {
    Host         string `json:"host"`          // "sd-<name>"
    HostName     string `json:"hostname"`      // IP or empty if VSOCK
    Port         int    `json:"port,omitempty"`
    User         string `json:"user"`
    IdentityFile string `json:"identity_file"`
    ProxyCommand string `json:"proxy_command,omitempty"` // set for VSOCK
    Transport    string `json:"transport"`               // "tcp" or "vsock" (REQ-007-005)
}

// SyncOpts configures a file synchronization operation.
// REQ-007-015, REQ-007-016, REQ-007-017, REQ-007-018
type SyncOpts struct {
    VMName  string
    SrcPath string
    DstPath string
    Watch   bool // continuous sync from host to VM (valid only for SyncTo)
}

// DiffResult holds the output of a sync diff preview. REQ-007-017
type DiffResult struct {
    HasDifferences bool   `json:"has_differences"`
    Diff           string `json:"diff"`
}
```

### Package `internal/connection` — Connector and FileSyncer Interfaces

```go
// Connector manages interactive and non-interactive connections to VMs.
// REQ-007-001, REQ-007-013
type Connector interface {
    // Connect establishes an interactive SSH session.
    // Handles auto-start (REQ-007-002), port forwarding (REQ-007-007),
    // tmux attachment (REQ-007-008), and env injection (REQ-007-019).
    // Blocks until the session ends.
    Connect(ctx context.Context, opts ConnectOpts) error

    // Exec runs a command non-interactively via SSH.
    // Does NOT auto-start a stopped VM — returns ErrVMNotRunning instead. REQ-007-013
    Exec(ctx context.Context, vmName string, command []string, opts ExecOpts) (*ExecResult, error)

    // SSHConfigFor returns the SSH config fragment for the named VM. REQ-007-006
    SSHConfigFor(vmName string) (*SSHConfigData, error)
}

// FileSyncer manages rsync-based file synchronization between host and VM.
// REQ-007-015, REQ-007-016, REQ-007-017, REQ-007-018
type FileSyncer interface {
    SyncTo(ctx context.Context, opts SyncOpts) error
    SyncFrom(ctx context.Context, opts SyncOpts) error
    Diff(ctx context.Context, opts SyncOpts) (*DiffResult, error)
    // Watch monitors the host path and syncs incrementally on changes.
    // Valid only for SyncTo direction. REQ-007-018
    Watch(ctx context.Context, opts SyncOpts) error
}
```

### Package `internal/security` — Value Types

```go
package security

import "time"

// MountMode represents the access mode for a host directory mount. REQ-004-004
type MountMode int

const (
    MountReadOnly  MountMode = iota // default (REQ-004-004)
    MountReadWrite
)

// TokenType identifies the kind of credential. REQ-004-012, REQ-004-013
type TokenType int

const (
    TokenGitHubPAT         TokenType = iota // classic PAT (warned against)
    TokenGitHubFinegrained                   // fine-grained PAT (preferred)
    TokenAnthropicAPI                        // ANTHROPIC_API_KEY
)

// SecurityWarning is a non-fatal security observation. REQ-004-012
type SecurityWarning struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}

// EgressDomain represents an allowed outbound destination. REQ-004-006, REQ-004-008
type EgressDomain struct {
    Domain string       `json:"domain"`
    Source DomainSource `json:"source"` // "default" or "user"
}

type DomainSource string

const (
    DomainSourceDefault DomainSource = "default"
    DomainSourceUser    DomainSource = "user"
)

// CredentialEntry describes a configured credential without exposing its value. REQ-004-015
type CredentialEntry struct {
    Type       TokenType `json:"type"`
    Name       string    `json:"name"`
    Configured bool      `json:"configured"`
}

// CommandLogEntry records one CLI invocation in the audit log. REQ-004-021
type CommandLogEntry struct {
    Timestamp time.Time     `json:"timestamp"`
    Command   string        `json:"command"`
    // Args MUST have secrets redacted before this entry is constructed.
    Args      []string      `json:"args"`
    ExitCode  int           `json:"exit_code"`
    Duration  time.Duration `json:"duration_ms"`
}

// EventLogEntry records a VM lifecycle event in the audit log. REQ-004-022
type EventLogEntry struct {
    Timestamp time.Time         `json:"timestamp"`
    EventType string            `json:"event_type"` // create, start, stop, destroy, connect, etc.
    VMName    string            `json:"vm_name"`
    Metadata  map[string]string `json:"metadata,omitempty"`
}

// AuditEntry is the union type returned by AuditLogger.Query. REQ-004-022
type AuditEntry struct {
    Timestamp time.Time        `json:"timestamp"`
    Type      string           `json:"type"`      // "command" or "event"
    PrevHash  string           `json:"prev_hash"` // SHA-256 of preceding log line
    Command   *CommandLogEntry `json:"command,omitempty"`
    Event     *EventLogEntry   `json:"event,omitempty"`
}

// AuditFilter specifies criteria for querying the audit log. REQ-004-022
type AuditFilter struct {
    VMName *string
    Since  *time.Time
    Until  *time.Time
}

// ChainBreak describes a broken link in the audit log hash chain. REQ-004-022
type ChainBreak struct {
    LineNumber   int
    ExpectedHash string
    ActualHash   string
}

// SecurityPosture is the summary returned by sd security status. REQ-004-024
type SecurityPosture struct {
    VMName        string            `json:"vm_name"`
    Mounts        []MountSummary    `json:"mounts"`
    EgressDomains []EgressDomain    `json:"egress_domains"`
    Credentials   []CredentialEntry `json:"credentials"`
    SnapshotCount int               `json:"snapshot_count"`
    LastAuditEvent *AuditEntry      `json:"last_audit_event,omitempty"`
    Warnings      []SecurityWarning `json:"warnings,omitempty"`
}

type MountSummary struct {
    HostPath  string `json:"host_path"`
    GuestPath string `json:"guest_path"`
    Mode      string `json:"mode"`       // "ro" or "rw"
    IsWarning bool   `json:"is_warning"` // true for writable mounts
}
```

### Package `internal/security` — Interfaces

```go
// SecurityValidator validates security-sensitive operations before execution.
// REQ-004-003, REQ-004-004, REQ-004-005, REQ-004-012
type SecurityValidator interface {
    // ValidateMountPath checks whether a host path is safe to mount.
    // Resolves symlinks before checking. REQ-004-005
    ValidateMountPath(hostPath string, mode MountMode) error

    // ValidateToken inspects a token and returns a list of warnings,
    // e.g., detecting a classic PAT (prefix "ghp_"). REQ-004-012
    ValidateToken(token string, tokenType TokenType) []SecurityWarning
}

// EgressController manages outbound network rules inside a VM.
// REQ-004-006, REQ-004-008, REQ-004-009, REQ-004-010
type EgressController interface {
    // ApplyAllowlist provisions iptables rules and dnsmasq configuration. Called during provisioning.
    ApplyAllowlist(vmName string, domains []EgressDomain) error
    // AddDomain adds a domain to the running VM's allowlist without restarting. REQ-004-008
    AddDomain(vmName string, domain EgressDomain) error
    RemoveDomain(vmName string, domain string) error
    ListDomains(vmName string) ([]EgressDomain, error)
}

// CredentialInjector manages runtime credential injection into VM sessions.
// REQ-004-011, REQ-004-015
type CredentialInjector interface {
    // InjectEnv resolves all ${VAR} references for vmName from host environment.
    // MUST NOT write any value to any file. Values used only with SSH SendEnv. REQ-004-011, REQ-007-019
    InjectEnv(vmName string) (map[string]string, error)

    // Rotate updates the stored credential reference; newValue is a ${VAR} reference. REQ-004-015
    Rotate(vmName string, tokenType TokenType, newValue string) error
    Revoke(vmName string) error
    List(vmName string) ([]CredentialEntry, error)
}

// AuditLogger records sd operations and VM lifecycle events.
// REQ-004-021, REQ-004-022
type AuditLogger interface {
    // LogCommand records a CLI invocation. Secrets in Args must be redacted by the caller.
    LogCommand(entry CommandLogEntry) error
    LogEvent(entry EventLogEntry) error
    Query(filter AuditFilter) ([]AuditEntry, error)
    // VerifyChain validates the SHA-256 hash chain. Returns nil if intact. REQ-004-022
    VerifyChain() (*ChainBreak, error)
}
```

### Package `internal/state` — Manager Interface

```go
package state

import "sd/internal/config"

// Manager reads and writes per-VM state files under $SD_HOME/vms/.
// REQ-001-011, REQ-005-007
type Manager interface {
    // Create writes the initial config.yaml for a new VM.
    // Creates $SD_HOME/vms/<name>/ with mode 0700.
    Create(cfg *config.VMConfig) error

    // Get returns the persisted config for the named VM.
    // Returns ErrVMNotFound if no state file exists.
    Get(name string) (*config.VMConfig, error)

    // Update rewrites the config.yaml for an existing VM.
    Update(cfg *config.VMConfig) error

    // Delete removes the entire $SD_HOME/vms/<name>/ directory. Best-effort. REQ-001-008
    Delete(name string) error

    List() ([]string, error)
    Exists(name string) bool
}

// ErrVMNotFound mirrors backend.ErrVMNotFound but is defined here for state-layer
// use without importing the backend package.
var ErrVMNotFound = errors.New("vm state not found")
```

Directory/file permission invariants enforced by state package (REQ-005-016, REQ-001-014):

| Path | Mode |
|------|------|
| `$SD_HOME/` | `0700` |
| `$SD_HOME/vms/` | `0700` |
| `$SD_HOME/vms/<name>/` | `0700` |
| `$SD_HOME/vms/<name>/config.yaml` | `0600` |
| `$SD_HOME/vms/<name>/ssh/` | `0700` |
| `$SD_HOME/vms/<name>/ssh/id_ed25519` | `0600` |

### Package `internal/ui` — Printer Interface

```go
package ui

import "io"

// Printer abstracts all user-visible output. REQ-002-010, REQ-002-011
type Printer interface {
    Info(format string, args ...any)   // stderr; suppressed when --quiet active
    Warn(format string, args ...any)   // stderr; never suppressed
    Error(format string, args ...any)  // stderr; never suppressed
    // Data writes the primary output data.
    // In human mode: formatted table/text to stdout.
    // In JSON mode: v JSON-encoded to stdout. REQ-002-011, REQ-002-012
    Data(v any)
    Fatal(format string, args ...any) // writes error to stderr, exits with code 1
    Progress(label string) ProgressReporter
    IsJSON() bool
}

type ProgressReporter interface {
    Step(label string)
    Done()
    Fail(err error)
}

func NewPrinter(out, err io.Writer, json bool) Printer

// JSONError is the envelope for all error output in --json mode. REQ-001-010
type JSONError struct {
    Error JSONErrorBody `json:"error"`
}

type JSONErrorBody struct {
    Code    string         `json:"code"`
    Message string         `json:"message"`
    Details map[string]any `json:"details,omitempty"`
}

// Standard error codes (REQ-001-010 error table)
const (
    ErrCodeBackendNotFound           = "BACKEND_NOT_FOUND"
    ErrCodeBackendNotInstalled       = "BACKEND_NOT_INSTALLED"
    ErrCodeVMExists                  = "VM_EXISTS"
    ErrCodeVMNotFound                = "VM_NOT_FOUND"
    ErrCodeVMRunning                 = "VM_RUNNING"
    ErrCodeInvalidVMName             = "INVALID_VM_NAME"
    ErrCodeInvalidConfig             = "INVALID_CONFIG"
    ErrCodeMountRejected             = "MOUNT_REJECTED"
    ErrCodeCreateFailed              = "CREATE_FAILED"
    ErrCodeProvisionFailed           = "PROVISION_FAILED"
    ErrCodeCredentialInjectionFailed = "CREDENTIAL_INJECTION_FAILED"
    ErrCodeOrphanedState             = "ORPHANED_STATE"
    ErrCodeAlreadyRunning            = "ALREADY_RUNNING"
    ErrCodeAlreadyStopped            = "ALREADY_STOPPED"
)
```

### Package `internal/cmd` — App Struct and Root Contracts

```go
package cmd

// App holds the injected dependencies for all command handlers.
// Constructed once in root.go during cobra PersistentPreRunE.
type App struct {
    Config      config.Loader
    State       state.Manager
    UI          ui.Printer
    Backend     backend.Backend      // resolved at startup from flag/env/config
    Connector   connection.Connector
    Syncer      connection.FileSyncer
    Provisioner provision.Provisioner
    Validator   security.SecurityValidator
    Egress      security.EgressController
    Creds       security.CredentialInjector
    Audit       security.AuditLogger
}

// RootFlags are the global flags bound to the root Cobra command. REQ-002-010
type RootFlags struct {
    JSON       bool   // --json
    Verbose    bool   // --verbose / -v
    Quiet      bool   // --quiet / -q
    ConfigFile string // --config
    DefaultVM  string // --vm
}

// VMNamePattern is the compiled regexp for valid VM names. REQ-001-011
var VMNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

func ValidateVMName(name string) error
```

### Cross-Component Shared Types

| Type | Package | Consumed By |
|------|---------|-------------|
| `backend.VMConfig` | `internal/backend` | CLI layer, provisioner, Lima backend |
| `backend.VMStatus` | `internal/backend` | CLI layer, state manager, connection |
| `backend.SSHConfig` | `internal/backend` | Connection manager |
| `backend.ExecResult` | `internal/backend` | Provisioner (for running scripts) |
| `backend.SnapshotInfo` | `internal/backend` | CLI layer (snapshot list command) |
| `config.VMConfig` | `internal/config` | State manager, CLI layer, provisioner |
| `config.Config` | `internal/config` | CLI layer |
| `config.ValidationResult` | `internal/config` | CLI layer (config validate command) |
| `provision.ProvisionState` | `internal/provision` | CLI layer (status command, JSON output) |
| `security.EgressDomain` | `internal/security` | CLI layer, provisioner |
| `security.AuditEntry` | `internal/security` | CLI layer (audit command) |
| `ui.JSONError` | `internal/ui` | CLI layer (all error paths) |

**Naming conflict: two VMConfig types.** Use import aliases where both appear together:

```go
import (
    backendpkg "sd/internal/backend"
    configpkg  "sd/internal/config"
)
// backendpkg.VMConfig  — what you want the VM to be (resource spec for creation)
// configpkg.VMConfig   — what sd knows about the VM (state + metadata)
```

### Data Flows for Core Operations

**`sd create <name>`** (REQ-001-006):

```
cmd/create.go
    1. ValidateVMName(name)
    2. app.Config.GetForVM(name)          -> config.VMConfig (merged defaults)
    3. security.ValidateMountPath(...)    -> validates each --mount flag
    4. build backend.VMConfig from config.VMConfig + CLI flags
    5. app.State.Exists(name)            -> abort if true (VM_EXISTS)
    6. app.Backend.Create(ctx, name, vmcfg)    -> blocks until running
       on failure -> app.Backend.Destroy(ctx, name) [cleanup]
    7. app.Provisioner.Provision(ctx, name, modules)
       on failure -> app.Backend.Destroy(ctx, name) [cleanup]
    8. app.State.Create(&config.VMConfig{...})   -> persists state
    9. app.Audit.LogEvent(EventLogEntry{EventType:"create",...})
   10. app.UI.Data(result)
```

**`sd connect <name>`** (REQ-001-007):

```
cmd/connect.go
    1. app.State.Get(name)              -> config.VMConfig
    2. app.Backend.Status(ctx, name)   -> VMStatus
    3. if stopped && !opts.NoStart: app.Backend.Start(ctx, name)
    4. app.Creds.InjectEnv(name)        -> map[string]string (resolved ${VAR})
       on error: app.UI.Warn(...)  — non-fatal (REQ-001-007)
    5. app.Connector.Connect(ctx, ConnectOpts{VMName: name, EnvVars: resolved, ...})
       -> blocks; SSH session runs
    6. app.Audit.LogEvent(EventLogEntry{EventType:"connect",...})
    7. app.UI.Info("session ended; duration: ...")
```

**`sd destroy <name>`** (REQ-001-008):

```
cmd/destroy.go
    1. app.State.Get(name)             -> config.VMConfig
    2. status := app.Backend.Status(ctx, name)
    3. if status == Running && !force: app.UI.Fatal(VM_RUNNING, ...)
    4. if !noSnapshot:
           snap := app.Backend.(backend.Snapshotter)
           snap.SnapshotCreate(ctx, name, timestamp_tag)
    5. if status == Running: app.Backend.Stop(ctx, name)
    6. app.Backend.Destroy(ctx, name)
    7. app.State.Delete(name)   — best-effort even if Destroy errored
    8. remove ~/.ssh/config.d/sd-<name>
    9. app.Audit.LogEvent(EventLogEntry{EventType:"destroy",...})
   10. app.UI.Data(result)
```

### Invariants and Constraints

| Invariant | Enforced By | Spec Ref |
|-----------|-------------|----------|
| VM names match `^[a-z][a-z0-9-]{0,62}$` | cmd.ValidateVMName | REQ-001-011 |
| $HOME (and sensitive subdirs) never mounted | SecurityValidator | REQ-004-005 |
| Credentials never written to disk inside VM | CredentialInjector.InjectEnv | REQ-004-011 |
| Credentials never in Lima YAML env field | Lima backend.Create | REQ-003-023 |
| Default egress is deny-all | EgressController.ApplyAllowlist | REQ-004-006 |
| config.VMConfig.Env holds only ${VAR} references | Loader.Set validation | REQ-005-008 |
| security.* keys in project config are ignored | Loader.Load | REQ-005-017 |

---

## Engineering Strategy

### Design Patterns

**Strategy Pattern — Backend Interface** (`internal/backend/`)

REQ-001-003 and REQ-003-001 mandate VM lifecycle operations dispatched through a single
`Backend` interface. The `cmd` layer holds a `backend.Backend` value with zero knowledge
of whether it is talking to Lima, Docker, or a stub. Optional capabilities use type
assertions (REQ-003-008 through REQ-003-010):

```go
if s, ok := b.(Snapshotter); ok {
    if err := s.SnapshotCreate(ctx, name, tag); err != nil { ... }
} else {
    return fmt.Errorf("backend %T does not support snapshots", b)
}
```

Do NOT add backend-specific methods to the core `Backend` interface. Lima-specific flags
(e.g., `vmType`) go in `VMConfig.BackendOptions map[string]any` (REQ-003-011).

**Registry Pattern** (`internal/backend/registry.go`)

Narrowly scoped to backends only. Lima's `init()` calls `Register("lima", &LimaBackend{})`.
Do NOT use this pattern elsewhere — other components are wired at construction time via
explicit dependency injection.

**Dependency Injection** (`internal/cmd/`)

```go
// internal/cmd/create.go
type createCmd struct {
    registry    backend.Registry
    stateStore  state.Store
    provisioner provision.Engine
    ui          ui.Output
    cfg         *config.Config
}

func newCreateCmd(deps *Deps) *cobra.Command {
    c := &createCmd{deps: deps}
    cmd := &cobra.Command{Use: "create <name>", RunE: c.run}
    return cmd
}
```

The `App` struct (or `Deps` struct) is constructed once in `Execute()` and passed down.
Tests replace individual fields with test doubles.

**Adapter Pattern — Lima YAML Generation** (`internal/backend/lima/yaml.go`)

```go
func vmConfigToLima(cfg backend.VMConfig, arch string) (limaConfig, error) {
    vmType := "qemu"
    if arch == "arm64" {
        vmType = "vz"  // REQ-003-016: VZ defaults on apple silicon
    }
    mounts := make([]limaMount, 0)  // REQ-003-017: empty mounts list overrides Lima default
    for _, m := range cfg.Mounts { /* map VMMount -> limaMount */ }
    safeEnv := filterSensitiveEnv(cfg.EnvVars)  // REQ-003-023: never sensitive env in Lima YAML
    return limaConfig{VMType: vmType, Mounts: mounts, Env: safeEnv, ...}, nil
}
```

**Observer / Event Pattern — Audit Logging** (`internal/security/audit.go`)

All components call `audit.Log(event)` after each operation. Inject, do not use a global singleton.

**Deferred Rollback** (`internal/cmd/create.go`)

```go
var rollback func()
rollback = func() {}  // no-op initially

if err := b.Create(ctx, name, cfg); err != nil {
    return err
}
rollback = func() { _ = b.Destroy(context.Background(), name) }
defer func() {
    if rollbackErr := recover(); rollbackErr != nil || err != nil {
        rollback()
    }
}()
```

Use `context.Background()` for the rollback destroy (original context may be cancelled).

**Patterns to AVOID:**

- Global Viper singleton — always construct a `*viper.Viper` instance and pass it down.
- Goroutines without context — every subprocess call MUST use `exec.CommandContext(ctx, ...)`.
- Dynamic provisioning script assembly from user input (prevents injection, REQ-006-016).
- Monolithic command `RunE` functions — each command is a struct with a `run` method.

### Error Handling Strategy

**Three layers of error types:**

**Layer 1 — Sentinel errors** (package-level `var`, `errors.Is()` matching):

```go
// internal/backend/errors.go
var (
    ErrVMNotFound          = errors.New("VM not found")       // REQ-003-021
    ErrVMAlreadyExists     = errors.New("VM already exists")
    ErrVMNotRunning        = errors.New("VM not running")
    ErrBackendNotAvailable = errors.New("backend not available")
    ErrBackendNotFound     = errors.New("backend not registered")
    ErrNotImplemented      = errors.New("not implemented")
)
```

**Layer 2 — Structured errors** (user-facing output with codes):

```go
// internal/ui/errors.go
type SDError struct {
    Code    string         `json:"code"`
    Message string         `json:"message"`
    Details map[string]any `json:"details,omitempty"`
    cause   error          // not serialized
}

func (e *SDError) Error() string { return fmt.Sprintf("[%s] %s", e.Code, e.Message) }
func (e *SDError) Unwrap() error { return e.cause }

// Constructor helper example:
func ErrVMExists(name string) *SDError {
    return &SDError{
        Code:    "VM_EXISTS",
        Message: fmt.Sprintf("VM %q already exists; use a different name or destroy the existing VM", name),
        Details: map[string]any{"name": name},
    }
}
```

**Layer 3 — Wrapped contextual errors:**

```go
return fmt.Errorf("lima create %q: %w", name, err)
```

Error propagation chain:

```
limactl exits non-zero
    -> LimaBackend.Create wraps: "limactl start sd-myvm: exit status 1"
    -> Backend.Create returns: fmt.Errorf("create VM: %w", ErrVMAlreadyExists)
    -> cmd/create.go detects via errors.Is(err, backend.ErrVMAlreadyExists)
    -> maps to SDError{Code: "VM_EXISTS", ...}
    -> ui.Output.Fatal(sderr) — prints to stderr OR emits JSON to stdout
```

**Error display rules:**

| Category | Display | Log level |
|----------|---------|-----------|
| Fatal (exit != 0) | stderr (text) or stdout (JSON) | ERROR |
| Warning (continue) | stderr always | WARN |
| Silent (no display) | none | DEBUG |

When `--json` is active: stdout receives exactly one JSON object (success payload or
`{"error": {...}}`); stderr receives nothing; exit code still set.

### Testing Strategy

**Framework:**
- Test runner: Go standard `testing` package
- Assertions: `github.com/stretchr/testify/require` (fatal), `testify/assert` (non-fatal)
- Mocking: hand-written fakes preferred for stable interfaces; `testify/mock` for complex stubs
- Script tests: `rsc.io/script/scripttest` (same framework as the Go toolchain)
- All unit tests: table-driven `[]struct{ name, input, want }` format

**Testing Pyramid:**

```
          /\
         /  \
        / e2e\   ~5%: Full Lima lifecycle tests (tagged //go:build e2e)
       /------\
      /integr  \  ~20%: Real file I/O, real YAML parsing, real subprocess
     /----------\
    /   unit     \ ~75%: Pure logic, interfaces mocked, no subprocesses
   /--------------\
```

Build tags: no tag (unit only), `//go:build integration` (require limactl on PATH),
`//go:build e2e` (full end-to-end, require Lima + network).

**Unit test scope:**

| Package | Test targets |
|---------|-------------|
| `internal/backend` | Registry CRUD, sentinel error wrapping, interface compliance (compile-time) |
| `internal/backend/lima` | YAML generation from VMConfig; platform detection; sensitive env filtering (REQ-003-023) |
| `internal/config` | Precedence resolution, env var mapping (SD_ prefix), default values, `${VAR}` reference parsing |
| `internal/provision` | Topological sort, circular dependency detection (REQ-006-004), module YAML parsing |
| `internal/security` | Mount path validation (REQ-004-005); egress wildcard matching (one-level only, REQ-004-007); audit hash chain (REQ-004-022) |
| `internal/state` | CRUD on config.yaml; directory/file permission checks using temp dirs |
| `internal/ui` | JSON marshaling of success/error payloads; text formatter output |
| `internal/cmd/*` | Each command's RunE with backend/state/provision mocked; flag parsing; exit code |
| VM name validation | Regex `^[a-z][a-z0-9-]{0,62}$` (REQ-001-011) |

Hand-written fakes are used for the `Backend`, `state.Manager`, `provision.Provisioner`,
and `security.AuditLogger` interfaces.

**Integration test scope** (`//go:build integration`):
- Config file loading: write temp YAML files, run through Viper load, assert resolved values
- State persistence: write `config.yaml`, restart state loader, verify round-trip
- Module YAML parsing: parse the embedded module files against the schema
- Mount path validation with real `os.UserHomeDir()` and `filepath.EvalSymlinks()`
- SSH key pair generated with correct permissions (0600/0700) (REQ-007-003)

**E2E test scope** (`//go:build e2e`, require `limactl` + macOS):
- Full `sd create` / `sd destroy` lifecycle
- `sd connect` opens SSH connection and runs a command
- `sd provision` idempotency (run twice, verify same state)
- Egress rules: verify `curl http://example.com` fails inside a default VM

**Script tests** (`rsc.io/script/scripttest`, live in `testdata/scripts/`):

```
# testdata/scripts/create_json.txtar
exec sd create test-vm --json
stdout '"name":"test-vm"'
stdout '"backend":"lima"'
exit 0

# testdata/scripts/create_duplicate.txtar
exec sd create test-vm
exec sd create test-vm --json
stderr '"code":"VM_EXISTS"'
exit 1

# testdata/scripts/create_invalid_name.txtar
exec sd create INVALID --json
stderr '"code":"INVALID_VM_NAME"'
exit 1
```

**Key test scenarios:**

| Spec Req | Test scenario | Level |
|----------|---------------|-------|
| REQ-001-006 | create rollback on provision failure | unit |
| REQ-001-011 | name validation edge cases | unit |
| REQ-003-016 | Lima YAML sets vmType=vz on arm64, qemu on amd64 | unit |
| REQ-003-017 | Generated YAML has `mounts: []` when no mounts specified | unit |
| REQ-003-023 | Sensitive env vars (`*_TOKEN`, `*_KEY`) absent from Lima YAML env section | unit |
| REQ-004-005 | Mount path rejection: $HOME, ~/.ssh, Docker socket | unit |
| REQ-004-007 | Egress wildcard: `*.githubusercontent.com` matches one level only | unit |
| REQ-004-022 | Audit hash chain: second entry's prev_hash == SHA256 of first entry line | unit |
| REQ-005-001 | Config precedence: CLI > env var > project > user > default | integration |
| REQ-005-008 | `${VAR}` references resolved from host env; `$$LITERAL` escaping | unit |
| REQ-006-004 | Circular dependency A->B->A is caught at validation | unit |
| REQ-006-016 | Module YAML without checksums triggers warning | unit |
| REQ-007-003 | SSH key pair generated with correct permissions (0600/0700) | integration |

### Configuration and Environment

**Viper Instance Strategy** — never use `viper.GetString(...)` (global Viper):

```go
// internal/config/config.go
func Load(sdHome string, cwd string) (*Config, error) {
    v := viper.New()
    v.SetEnvPrefix("SD")
    v.AutomaticEnv()
    v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))  // REQ-005-005
    setDefaults(v)  // REQ-005-004

    // REQ-005-002: user-level config
    v.SetConfigFile(filepath.Join(sdHome, "config.yaml"))
    v.SetConfigType("yaml")
    if err := v.ReadInConfig(); err != nil {
        if !errors.Is(err, viper.ConfigFileNotFoundError{}) {
            return nil, fmt.Errorf("invalid config %s: %w", v.ConfigFileUsed(), err)
        }
    }

    // REQ-005-003: project-level config (walk up from cwd)
    if projectCfg := findProjectConfig(cwd); projectCfg != "" {
        v.SetConfigFile(projectCfg)
        if err := v.MergeInConfig(); err != nil {
            return nil, fmt.Errorf("invalid project config %s: %w", projectCfg, err)
        }
    }

    return &Config{v: v}, nil
}
```

**Sensitive Value Handling** — `${VAR}` references NOT resolved at load time (REQ-005-008):

```go
// Resolve expands ${VAR} references from os environment.
func (c *Config) Resolve(key string) (string, error) {
    raw := c.v.GetString(key)
    return expandEnvRefs(raw)
}

// Get returns the unexpanded string (safe to log/display).
func (c *Config) Get(key string) string {
    return c.v.GetString(key)
}
```

**SD_HOME Bootstrap:**

```go
func SDHome() string {
    if h := os.Getenv("SD_HOME"); h != "" {
        return h
    }
    home, _ := os.UserHomeDir()
    return filepath.Join(home, ".sd")
}
```

### Observability and Debugging

**Logging Library:** `log/slog` (stdlib, Go 1.21+). Do NOT use logrus, zap, or zerolog.

```go
// internal/cmd/root.go
func Execute() error {
    level := slog.LevelWarn
    if verbose {
        level = slog.LevelDebug
    }
    logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
    slog.SetDefault(logger)
}
```

When `--json` is active, warnings/info are suppressed entirely.

**Log Levels:**

| Level | Use | Examples |
|-------|-----|---------|
| DEBUG | Internal mechanics, visible with `--verbose` | limactl invocation args, YAML generated, env vars being forwarded (masked) |
| INFO | Normal operation milestones | "VM created", "provisioning complete" |
| WARN | Non-fatal warnings | CREDENTIAL_INJECTION_FAILED, ORPHANED_STATE, ${VAR} unresolved |
| ERROR | Fatal errors before exit | never logged; printed directly to stderr via `ui.Output.Fatal()` |

DEBUG logs MUST NOT include raw credential values or SSH private key material.
Values should be masked: `ANTHROPIC_API_KEY=sk-ant-...****`.

**Progress Reporting** (REQ-006-009, REQ-001-007):

```go
// internal/ui/progress.go
type Progress interface {
    Start(msg string)
    Update(msg string)
    Done(msg string)
    Fail(msg string)
}

func NewProgress(w io.Writer, isTTY bool) Progress {
    if isTTY {
        return newSpinnerProgress(w)
    }
    return newLineProgress(w)  // "... starting vm\n... vm started\n"
}
```

When `--json` is active, `Progress` is a no-op.

**`sd doctor` implementation** (REQ-002-007):

```go
type Check struct {
    Name string
    Run  func() error
}

checks := []Check{
    {"limactl installed", checkBinary("limactl")},
    {"ssh installed",     checkBinary("ssh")},
    {"tmux installed",    checkBinary("tmux")},
    {"rsync installed",   checkBinary("rsync")},
    {"config valid",      cfg.Validate},
    {"SD_HOME writable",  checkDir(sdHome)},
}
```

### Third-Party Dependencies

**Required:**

| Library | Version | Purpose |
|---------|---------|---------|
| `github.com/spf13/cobra` | v1.8+ | CLI command structure (REQ-001-010, mandated by AGENTS.md) |
| `github.com/spf13/viper` | v1.19+ | Config loading and merging (REQ-001-009, REQ-005-014) |
| `github.com/stretchr/testify` | v1.9+ | Test assertions and mocks (mandated by AGENTS.md) |
| `gopkg.in/yaml.v3` | v3.x | YAML parsing; v3 supports strict mode (unknown field warnings, REQ-005-006) |
| `golang.org/x/crypto/ssh` | latest | SSH key generation (Ed25519) and known_hosts management (REQ-007-003, REQ-007-004) |
| `rsc.io/script` | v0.0.3+ | Script-based CLI tests (.txtar format) (REQ-001 testing strategy) |

**Conditional / Optional:**

| Library | Purpose | Condition |
|---------|---------|-----------|
| `github.com/fsnotify/fsnotify` | File-system watching for `sd sync --watch` (REQ-007-018) | Only when `--watch` is implemented |
| `github.com/muesli/termenv` | Terminal color/style detection for progress | Optional; can defer |

**Dependencies to AVOID:**
- Any interactive prompt library (sd is non-interactive by default)
- `github.com/urfave/cli` (Cobra is required)
- `logrus` / `zap` (use `log/slog`)
- `github.com/google/uuid` (not needed; use timestamp + random suffix for snapshot IDs)
- `os/exec` without `CommandContext` (forbidden in backend code; always `exec.CommandContext`)
- `github.com/mitchellh/go-homedir` (use stdlib `os.UserHomeDir()`)

```
go.mod (placeholder module path — org name TBD):
module github.com/org/sd
go 1.22

require (
  github.com/spf13/cobra       v1.8+
  github.com/spf13/viper       v1.19+
  golang.org/x/crypto          latest
  gopkg.in/yaml.v3             v3.0+
  github.com/stretchr/testify  v1.9+
  rsc.io/script                latest
)
```

**Dependency Management:** `go mod tidy` before every commit. `govulncheck` in CI.
Prefer stdlib over third-party for small utilities.

### Build and Deployment

```
cmd/sd/main.go  ->  binary: sd
go build -o sd ./cmd/sd/

# Release build with version info:
go build -ldflags "-X github.com/org/sd/internal/cmd.Version=$(git describe --tags) \
                   -X github.com/org/sd/internal/cmd.Commit=$(git rev-parse HEAD) \
                   -X github.com/org/sd/internal/cmd.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o sd ./cmd/sd/
```

Embedded assets:

```go
// internal/provision/loader.go
//go:embed modules/*.yaml
var moduleFS embed.FS

// internal/config/defaults.go
//go:embed defaults.yaml.tmpl
var defaultConfigTemplate string
```

Makefile targets:
```makefile
build:       go build -o sd ./cmd/sd/
test:        go test ./...
test-int:    go test -tags integration ./...
test-e2e:    go test -tags e2e ./...
vet:         go vet ./...
lint:        golangci-lint run
install:     go install ./cmd/sd/
```

Dependency enforcement in CI:

```go
// internal/backend/backend.go — compile-time interface compliance check
var _ Backend = (*lima.LimaBackend)(nil)
```

---

## Design Tensions

### Tension 1: `state.Manager` depends on `config.VMConfig` (cross-package type coupling)

The `state` package imports `internal/config` (for the `VMConfig` type that it
persists/retrieves). The `interfaces-design` agent defined `state.Manager` with signatures
using `*config.VMConfig`. The `architecture-design` agent treated state as having its own
`VMMetadata` type in `internal/state/types.go`.

**Positions:**
- Architecture agent: state has its own `VMMetadata` type, no dependency on `config/`.
- Interface agent: state uses `*config.VMConfig` directly, creating a dependency on `config/`.

**Recommended resolution:** Use `*config.VMConfig` in the state manager interface, as the
interface agent specifies. The per-VM state file IS a `config.VMConfig` — there is no
benefit to a separate `VMMetadata` wrapper that would require a conversion step. The
`state/` package importing `config/` is a one-way, lower-to-lower dependency (both are in
Layer 2) and does not create a cycle. The architecture agent's `types.go` file can be
dropped; the types live in `config/schema.go`.

**Spec basis:** REQ-005-007 defines the per-VM state file format as `VMConfig`. The state
manager is the sole reader/writer of that file (REQ-001-011).

### Tension 2: Naming — `SDError` in `ui/` vs helper constructors per-package

The strategy agent placed `SDError` and error constructors (e.g., `ErrVMExists(name)`) in
`internal/ui/errors.go`. The interface agent placed `JSONError`/`JSONErrorBody` in `ui/`
and error codes as constants there, but did not specify a concrete `SDError` struct.

**Positions:**
- Strategy agent: `SDError` struct in `ui/` with per-error constructor helpers.
- Interface agent: `JSONError` envelope struct in `ui/`; did not define `SDError`.

**Recommended resolution:** Both coexist cleanly. Define `JSONError`/`JSONErrorBody` as
the serialization envelope (interface agent's design) and use `SDError` as the internal
wrapping type that carries the `cause` error chain (strategy agent's design). The `Printer`
interface's `Fatal` method accepts a string; the command layer constructs an `SDError`,
formats it for display, and marshals it to `JSONError` for JSON mode. Error code constants
(`ErrCodeVMExists`, etc.) live in `ui/` as the single source of truth for all error codes.

### Tension 3: `backend.ExecResult` vs `connection.ExecResult` — duplication

Both packages define an `ExecResult` with identical fields. The interface agent explicitly
notes this is intentional to avoid a circular import. The architecture agent did not call
this out but placed `ExecResult` only in `backend/`.

**Positions:**
- Architecture agent: single `backend.ExecResult`; connection package uses the backend type.
- Interface agent: separate `connection.ExecResult` to avoid importing backend from connection.

**Recommended resolution:** Accept the duplication as the interface agent recommends.
`connection/` importing `backend/` for a result type creates a tight coupling that makes
connection harder to test in isolation. The two types are structurally identical today and
can be kept in sync with a compile-time test. ALTERNATIVELY: a thin `internal/types`
package could hold shared value types (ExecResult, SSHConfig) with no dependencies.
Mark this as **OPEN DECISION** — either approach works.

### Tension 4: `Deps` struct naming — `App` vs `Deps`

The interface agent uses `App` as the name of the injected dependency struct in
`internal/cmd/`. The strategy agent uses `Deps`. These are identical in purpose.

**Recommended resolution:** Use `App` (interface agent's name). It is more idiomatic for a
CLI binary — the struct represents the application's wired-up services. Rename any
`Deps` references in the plan to `App`.

---

## Key Decisions Summary

| Decision | Choice | Rationale | Spec Basis |
|----------|--------|-----------|------------|
| VM backend abstraction | `backend.Backend` interface + registry | Enables future backends without CLI changes | REQ-001-003, REQ-003-001, REQ-003-013 |
| Config loading | Instance-based Viper (not global) | Testable; no global state; explicit dependency | REQ-001-002, REQ-001-009, REQ-005-014 |
| `state/` separate from `config/` | Separate packages; state imports config for VMConfig | Enforces single-writer rule for VM metadata | REQ-001-001, REQ-001-011 |
| Credentials in config | `${VAR}` references only; resolved at connect time via SSH SendEnv | Credentials never written to disk inside VM | REQ-004-011, REQ-004-013, REQ-005-008 |
| SSH key management | `connection/` owns per-VM Ed25519 key pairs | Backend stays focused on VM lifecycle; keys are a host-VM relationship concern | REQ-007-003 |
| Lima snapshot strategy | APFS `cp -c` clone (not native QEMU snapshots) | Lima+VZ does not support QEMU snapshots; APFS clone is near-instant | REQ-003-018 |
| Provisioning execution | Via `backend.Exec()`, not direct SSH | Future backends (Docker, Incus) work without changing provisioner | REQ-006-001, REQ-003-007 |
| Optional backend capabilities | Type assertions (`Snapshotter`, `Cloner`, `FileSync`) | Avoids polluting core interface with optional features | REQ-003-008 to REQ-003-010 |
| `ui/` zero internal dependencies | `ui/` imports stdlib only | Prevents circular deps; output format decisions passed as parameters | REQ-001-014 |
| Logging library | `log/slog` (stdlib) | No external dep needed; structured + leveled; JSON-compatible | AGENTS.md |
| Module system | Pure YAML data embedded in binary; custom modules in `~/.sd/provisions/` | Adding a new tool = one YAML file, no Go code changes | REQ-006-001, REQ-006-007, REQ-006-014 |
| Security keys in project config | Silently ignored by Loader during merge | Prevents per-project override of security policies | REQ-005-017 |
| Error codes | All-caps string constants in `ui/` package | Single source of truth; JSON-serializable; machine-readable | REQ-001-010 |
| `connection.ExecResult` duplication | Accept duplication (see Design Tensions §3) | Avoids circular import; OPEN DECISION on whether to introduce `internal/types` | REQ-007-013, REQ-007-014 |

---

## Open Questions

1. **Module path / org name**: `module github.com/org/sd` uses a placeholder. The actual
   Go module path needs to be confirmed before `go.mod` is written. This affects all import
   paths throughout the codebase.

2. **`connection.ExecResult` vs `backend.ExecResult` duplication** (see Design Tensions §3):
   Accept duplication as-is, or introduce `internal/types` for shared value types? The plan
   writer should decide before the connection package is implemented.

3. **`sd token` command and `CredentialInjector.Rotate`**: The `token` command (appearing
   in the cmd list) and `Rotate`/`Revoke` methods in `CredentialInjector` imply sd manages
   credential configuration, but the security model says credentials are resolved from the
   host environment at connect time via `${VAR}` references. Clarify: does `sd token` write
   to the VM's config file (updating the `${VAR}` reference target), or does it manage
   something else entirely? This affects what `Rotate` actually does at implementation time.

4. **Egress enforcement mechanism**: `EgressController.ApplyAllowlist` is specified to
   provision iptables rules and dnsmasq configuration inside the VM. The details of how
   this interacts with Lima's networking (VZ uses a userspace network stack; QEMU can use
   either) are not fully specified. The plan should include a spike task to validate the
   iptables/dnsmasq approach works within Lima's networking model before committing to it.

5. **`sd logs` command**: Listed in the cmd file list but not covered in the interface
   design. Clarify what logs are exposed (lima daemon logs? provisioning output? audit log
   is covered by `sd audit`). Should this command exist or should it be replaced by
   `sd audit` + `--verbose` on other commands?

6. **`backend.FileSync` vs `connection.FileSyncer`**: Both exist for file synchronization.
   The interface agent clarifies they solve different problems (`connection.FileSyncer` is
   the primary CLI interface via rsync; `backend.FileSync` is an optional backend
   alternative). Confirm whether any v1 backend will implement `backend.FileSync`, or
   whether it should be deferred entirely to avoid dead interface code.
