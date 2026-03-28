# 001: System Architecture

| Field        | Value      |
|--------------|------------|
| Status       | draft      |
| Created      | 2026-03-27 |
| Last Updated | 2026-03-27 |
| Authors      | claude     |
| Reviewers    |            |

## Overview

This spec defines the overall architecture of `sd` (secure-dev), a Go CLI tool that creates, configures, and manages secure VM environments for running AI coding agents with bypass permissions. It establishes the system components, their relationships, data flow for core operations, the layered security model, and extension points. All subsequent specs build on the foundations defined here.

This spec is intentionally high-level. It defines architectural contracts (what components exist and how they relate) rather than implementation details (Go interfaces, config schemas, directory layouts) which are authoritatively defined in their respective dedicated specs.

## Goals

- G1: Define every top-level component of the `sd` system and its responsibility boundary
- G2: Establish the pluggable backend architecture so the VM runtime can be swapped without changing the rest of the system
- G3: Specify the data flow for the three core lifecycle commands: `sd create`, `sd connect`, `sd destroy`
- G4: Define the layered security model that governs all isolation and credential decisions
- G5: Identify extension points for future growth

## Non-Goals

- NG1: Detailed CLI surface (flags, subcommands, output formats) -- covered in 002-cli.md
- NG2: Go interface definitions, type definitions, or registry implementation for the backend -- covered in 003-vm-backend.md
- NG3: Provisioning script contents or tool-installation logic -- covered in 006-provisioning.md
- NG4: SSH key management details or connection protocol -- covered in 007-connection.md
- NG5: Egress rule syntax or firewall implementation -- covered in 004-security.md
- NG6: Configuration file format, schema, or precedence rules -- covered in 005-configuration.md
- NG7: Session management (tmux, multiplexing) -- covered in 007-connection.md

## Requirements

### REQ-001-001: System Components

The `sd` system MUST consist of the following top-level components:

1. **Host CLI (`sd`)** -- the Cobra-based command-line binary that users invoke on the host machine.
2. **Backend Interface** -- an abstract interface that VM runtimes implement; the system dispatches all VM lifecycle operations through this interface. The interface and its types are defined in 003-vm-backend.md.
3. **Provisioning Engine** -- responsible for installing tools, configuring the guest OS, and applying hardening after a VM is created. Defined in 006-provisioning.md.
4. **Connection Manager** -- responsible for establishing and managing SSH connections to VM instances, including port forwarding, tmux session management, and environment injection. Defined in 007-connection.md.
5. **Config System** -- Viper-based configuration loading from YAML files, environment variables, and CLI flags, with well-defined precedence. Defined in 005-configuration.md.
6. **Security System** -- responsible for enforcing egress control, credential scoping, mount restrictions, and audit logging. Defined in 004-security.md.
7. **State Manager** -- responsible for tracking named VM instances, their status, backend type, and metadata on the host filesystem.

**Acceptance criteria:**
- [ ] Each component listed above maps to a distinct Go package under `internal/`
- [ ] No component directly imports another component's internal types; communication happens through interfaces or shared types in a `types` package
- [ ] The Host CLI package (`internal/cmd/`) depends on all other components only through interfaces

### REQ-001-002: Component Relationships

The components MUST relate to each other as shown in the following diagram. Data flows downward from the CLI through the core engine to backends; cross-cutting concerns (config, security) are accessed horizontally.

```
                         +------------------+
                         |   User / Agent   |
                         +--------+---------+
                                  |
                                  v
                     +------------------------+
                     |   Host CLI (sd)        |
                     |   cmd/sd/ + internal/  |
                     |   cmd/                 |
                     +---+------+------+------+
                         |      |      |
              +----------+  +---+---+  +----------+
              |             |       |              |
              v             v       v              v
     +-----------+  +----------+  +---------+  +----------+
     | Connection |  |Provision |  |  State  |  | Security |
     | Manager    |  | Engine   |  | Manager |  | System   |
     +-----------+  +----------+  +---------+  +----------+
              |             |
              +------+------+
                     |
                     v
            +------------------+
            | Backend Interface|
            +--------+---------+
                     |
        +------------+------------+
        |            |            |
        v            v            v
   +--------+  +---------+  +--------+
   |  Lima   |  |   AVF   |  | Docker |
   |(default)|  | (future)|  |(future)|
   +--------+  +---------+  +--------+

   Cross-cutting (accessible by all components):
   +---------------------------------------------+
   |  Config System (Viper)  |  UI / Output       |
   +---------------------------------------------+
```

**Acceptance criteria:**
- [ ] The Connection Manager and Provisioning Engine operate on VMs exclusively through the Backend Interface
- [ ] The Config System is injected as a dependency, never imported as a global singleton
- [ ] The State Manager is the sole component that reads/writes VM metadata on the host filesystem

### REQ-001-003: Pluggable Backend Architecture

The system MUST define a Backend interface that all VM runtime implementations satisfy. The interface MUST support the complete VM lifecycle: create, start, stop, destroy, status, list, exec, and SSH configuration.

The Backend interface, its associated types (VMConfig, VMStatus, VMInfo, SSHConfig, ExecResult), optional capability interfaces (Snapshotter, Cloner, Syncer), the backend registry, and sentinel errors are all authoritatively defined in 003-vm-backend.md. This spec does not duplicate those definitions.

At the architectural level, the backend system provides the following guarantees:

1. All VM lifecycle operations go through a single interface -- no backend-specific code leaks into the CLI layer.
2. Backends register themselves at init time via a registry; the system selects a backend by name at runtime.
3. The Backend interface is sufficient to support all `sd` CLI commands without backend-specific branches.
4. Optional capabilities (snapshots, cloning, file sync) are discovered at runtime via type assertion.

**Acceptance criteria:**
- [ ] The Backend interface is defined in `internal/backend/backend.go` as specified in 003-vm-backend.md
- [ ] A `lima` package under `internal/backend/lima/` implements Backend using `limactl` as the default
- [ ] Backend implementations register via the registry defined in 003-vm-backend.md (REQ-003-013)
- [ ] All Backend methods accept a `context.Context` as their first parameter for cancellation and timeouts
- [ ] The interface is sufficient to support `sd create`, `sd start`, `sd stop`, `sd destroy`, `sd status`, `sd list`, `sd exec`, `sd connect`, and `sd snapshot` without backend-specific code leaking into the CLI layer

### REQ-001-004: Lima as Default Backend

Lima MUST be the default backend implementation. The Lima backend MUST:

1. Use Apple's Virtualization framework (VZ) as the VM type on Apple Silicon macOS for optimal performance.
2. Generate Lima YAML configuration programmatically from the VMConfig struct defined in 003-vm-backend.md.
3. Invoke `limactl` for all lifecycle operations.
4. Default to `ubuntu:24.04` as the guest image when no image is specified.
5. Disable host home directory mounts by default (no `$HOME` bind mounts).
6. Support writable project directory mounts only when explicitly requested.

The full Lima backend specification is in 003-vm-backend.md (REQ-003-014 through REQ-003-019).

**Acceptance criteria:**
- [ ] When no `--backend` flag is specified, the Lima backend is used
- [ ] Generated Lima YAML sets `vmType: vz` on `darwin/arm64` hosts
- [ ] Generated Lima YAML contains `mountType: virtiofs` for VZ-backed instances
- [ ] The Lima backend detects if `limactl` is installed and returns a clear error if it is not

### REQ-001-005: Future Backends

The architecture MUST accommodate future backends without changes to the core interface:

1. **Native Apple Virtualization Framework (AVF)** -- uses AVF directly via Go bindings instead of Lima. A stub MUST exist at `internal/backend/avf/`.
2. **Docker** -- runs agent environments inside containers. A stub MUST exist at `internal/backend/docker/`.
3. **Incus** -- runs agent environments inside Incus containers. A stub MUST exist at `internal/backend/incus/`.

Future backend stubs are defined in 003-vm-backend.md (REQ-003-020).

**Acceptance criteria:**
- [ ] The Backend interface does not contain any Lima-specific methods or types
- [ ] Stub packages exist for avf, docker, and incus backends
- [ ] No architectural decision in this spec prevents alternative backend implementations

### REQ-001-006: Data Flow -- `sd create`

When a user runs `sd create <name>`, the system MUST execute the following steps in order:

1. **Parse and validate** -- CLI parses flags, Viper merges config file defaults, validation confirms the VM name is unique and well-formed (alphanumeric plus hyphens, 1-63 characters).
2. **Resolve backend** -- determine which backend to use (flag > config file > default "lima"). Backend resolution uses the precedence defined in 005-configuration.md.
3. **Build VMConfig** -- merge CLI flags, config defaults, and any per-VM overrides into a VMConfig struct (defined in 003-vm-backend.md).
4. **Create VM** -- call `backend.Create(ctx, name, cfg)`. This blocks until the VM is running or fails.
5. **Provision** -- run the provisioning engine against the new VM: install base tools, apply hardening, configure SSH authorized keys. Provisioning is defined in 006-provisioning.md.
6. **Write state** -- persist VM metadata to `$SD_HOME/vms/<name>/config.yaml` as defined in 005-configuration.md (REQ-005-007).
7. **Report** -- print human-readable summary to stderr; if `--json`, print structured result to stdout.

**Acceptance criteria:**
- [ ] A failed step causes all subsequent steps to be skipped and the partially-created VM to be cleaned up (destroy called on backend)
- [ ] The `--dry-run` flag causes steps 4-6 to be skipped and the resolved VMConfig to be printed instead
- [ ] VM names are validated against the pattern `^[a-z][a-z0-9-]{0,62}$`
- [ ] The command returns exit code 0 on success, 1 on failure

### REQ-001-007: Data Flow -- `sd connect`

When a user runs `sd connect <name>`, the system MUST execute the following steps in order:

1. **Resolve VM** -- look up the named VM in state. Fail if not found.
2. **Check status** -- query the backend for the VM's current status. If stopped, auto-start the VM (this is the default behavior; use `--no-start` to disable). No interactive prompts.
3. **Inject credentials** -- resolve environment variables containing `${VAR}` references from the host environment and inject them into the guest session via SSH environment injection (never files). Credential injection is defined in 004-security.md.
4. **Establish connection** -- use the Connection Manager to open an interactive SSH session with tmux attachment. Connection details are defined in 007-connection.md.
5. **Report on disconnect** -- after the shell session ends, print a summary of session duration to stderr.

The `--json` flag outputs connection metadata (SSH config, VM status) to stdout without opening an interactive session. This enables scripts and tools to programmatically retrieve connection details.

**Acceptance criteria:**
- [ ] Connecting to a stopped VM auto-starts it (non-interactive, with a progress indicator) by default
- [ ] `--no-start` disables auto-start; connecting to a stopped VM with `--no-start` returns an error
- [ ] Credential injection failures are warnings, not fatal errors (the user can still connect)
- [ ] `--json` outputs connection metadata to stdout without opening an interactive session

### REQ-001-008: Data Flow -- `sd destroy`

When a user runs `sd destroy <name>`, the system MUST execute the following steps in order:

1. **Resolve VM** -- look up the named VM in state. Fail if not found.
2. **Confirm** -- if the VM is running and `--force` is not set, print a warning and require `--force`. In `--json` mode, return an error object (never prompt).
3. **Snapshot** -- by default, create a safety snapshot before destroying. Use `--no-snapshot` to skip (as defined in 004-security.md, REQ-004-019).
4. **Stop** -- if the VM is running, stop it.
5. **Destroy** -- call `backend.Destroy(ctx, name)` to remove the VM and its disk.
6. **Clean state** -- remove the VM's state directory from `$SD_HOME/vms/<name>/`.
7. **Report** -- confirm destruction to stderr; if `--json`, print structured result to stdout.

**Acceptance criteria:**
- [ ] Destroying a running VM without `--force` returns exit code 1 and a clear error message
- [ ] `--force` skips the running-VM check and proceeds directly to stop and destroy
- [ ] Auto-snapshot is the default; `--no-snapshot` opts out
- [ ] State directory cleanup succeeds even if the backend destroy partially fails (best-effort cleanup)

### REQ-001-009: Config System Architecture

The config system MUST use Viper to load configuration with the following precedence (highest to lowest):

1. CLI flags
2. Environment variables (prefixed `SD_`)
3. Project-level config file (`.sd/config.yaml`)
4. User-level config file (`$SD_HOME/config.yaml`)
5. Built-in defaults

The full configuration schema, file format, precedence rules, environment variable mapping, VM config inheritance, and CLI commands (`sd config get`, `sd config set`, `sd config list`, `sd config edit`, `sd config validate`) are authoritatively defined in 005-configuration.md.

**Acceptance criteria:**
- [ ] Viper is configured with the prefix `SD` for environment variable binding
- [ ] A missing config file is not an error (defaults are used)
- [ ] An invalid config file is a fatal error with a clear message identifying the parse failure
- [ ] `sd config list` outputs all configuration values with their resolved sources

### REQ-001-010: CLI Framework

The Host CLI MUST be built with Cobra and MUST support the following structural conventions:

1. All commands MUST support a `--json` flag that emits machine-parseable JSON to stdout.
2. All commands MUST be non-interactive by default. No prompts, no editors, no pagers unless explicitly requested.
3. Human-readable output (progress, warnings, errors) MUST go to stderr. Data output MUST go to stdout.
4. Exit codes: 0 for success, 1 for general error, 2 for usage error.

**Acceptance criteria:**
- [ ] The root command is `sd` with a version subcommand
- [ ] `--json` is a persistent flag available on all commands
- [ ] When `--json` is active, only valid JSON is written to stdout (one JSON object per command invocation)
- [ ] Error output in JSON mode follows a consistent schema: `{"error": {"code": "...", "message": "..."}}`

### REQ-001-011: Named VM Model

VMs MUST be managed as persistent named instances. A VM name uniquely identifies a VM across the system.

1. VM names MUST match the pattern `^[a-z][a-z0-9-]{0,62}$`.
2. VM names MUST be unique within a single `$SD_HOME`.
3. Each VM MUST track: name, backend type, creation timestamp, current status, and the configuration used to create it.
4. VM metadata MUST be stored in `$SD_HOME/vms/<name>/config.yaml` as defined in 005-configuration.md (REQ-005-007).

**Acceptance criteria:**
- [ ] Creating a VM with a duplicate name returns an error without modifying the existing VM
- [ ] VM metadata survives host reboots (it is persisted to disk)
- [ ] `sd list` reads VM metadata and queries backends for live status, merging both into the output
- [ ] Orphaned state (metadata exists but backend has no matching VM) is detected and reported as a warning

### REQ-001-012: Layered Security Model

The `sd` security model MUST enforce defense in depth through the following ordered layers. Each layer operates independently; compromise of one layer MUST NOT disable the others.

**Layer 1: VM Isolation**
- The guest runs in a hardware-virtualized VM (or container for the Docker backend), providing process and kernel isolation from the host.
- The host filesystem is NOT accessible to the guest except through explicit mounts.

**Layer 2: Mount Restrictions**
- The host `$HOME` directory MUST NOT be mounted into the guest under any circumstances.
- Only explicitly requested project directories MAY be mounted, and SHOULD default to read-only.
- Mounts MUST be specified at VM creation time and recorded in VM metadata.

**Layer 3: Egress Control**
- Guest network access MUST default to deny-all outbound.
- An allowlist of endpoints (domains or CIDR blocks) is configured per-VM or globally.
- The provisioning engine MUST install and configure firewall rules in the guest to enforce the allowlist.

**Layer 4: Credential Scoping**
- Credentials injected into the guest MUST be scoped to the minimum permissions required (e.g., single-repo fine-grained GitHub PATs, not org-wide tokens).
- Credentials MUST be injected at runtime via SSH environment injection, NOT persisted to the guest filesystem. Credentials never exist as files inside the guest.
- The system SHOULD support short-lived tokens that expire and require re-injection.

Full security requirements, including audit logging, token rotation, and snapshot-before-destroy, are defined in 004-security.md.

**Acceptance criteria:**
- [ ] Default VM creation produces a VM with no host mounts
- [ ] Attempting to mount `$HOME` or any parent of `$HOME` is rejected with a clear error
- [ ] Default egress policy is deny-all; the allowlist is applied during provisioning
- [ ] Credentials are injected via SSH environment variables only, never written to files inside the guest
- [ ] Each security layer is testable independently

### REQ-001-013: Guest VM Environment

Inside every guest VM provisioned by `sd`, the following environment MUST be established during provisioning:

1. A `~/.sd/bin/` directory added to `$PATH` for sd-provisioned tools.
2. A `~/projects/` directory as the default working directory for repos.
3. A tmux configuration for session management (defined in 007-connection.md, REQ-007-011).

Credentials flow through SSH environment injection only (never files). There is no `credentials.env` or equivalent file inside the guest. See 004-security.md for credential handling requirements.

The full provisioning process and guest setup are defined in 006-provisioning.md.

**Acceptance criteria:**
- [ ] The provisioning engine creates the guest environment during `sd create`
- [ ] The `~/.sd/bin/` directory is added to the guest user's `$PATH`
- [ ] The `projects/` directory is the default working directory when connecting
- [ ] No credential files exist inside the guest; credentials are environment-variable-only

### REQ-001-014: Package Layout

The Go source code MUST follow this package structure:

```
cmd/
  sd/
    main.go                  # Entry point, calls internal/cmd.Execute()
internal/
  cmd/                       # Cobra command definitions
    root.go                  # Root command, persistent flags (--json, --verbose)
    create.go                # sd create
    connect.go               # sd connect
    destroy.go               # sd destroy
    list.go                  # sd list
    status.go                # sd status
    config.go                # sd config (list, set, get, edit, validate)
    snapshot.go              # sd snapshot (create, restore, list)
    version.go               # sd version
  backend/                   # Backend interface and registry
    backend.go               # Interface, types, registry (spec 003)
    errors.go                # Sentinel errors (spec 003)
    registry.go              # Backend registry (spec 003)
    lima/                    # Lima backend implementation
      lima.go
      yaml.go                # Lima YAML generation
      snapshot.go            # Snapshotter implementation
    avf/                     # AVF backend stub
      avf.go
    docker/                  # Docker backend stub
      docker.go
    incus/                   # Incus backend stub
      incus.go
  config/                    # Viper-based config loading (spec 005)
    config.go                # Load, merge, validate
    defaults.go              # Built-in default values
  provision/                 # Guest provisioning (spec 006)
    provision.go             # Provisioning engine interface and orchestration
    scripts/                 # Embedded provisioning scripts
  connection/                # SSH and session management (spec 007)
    connect.go               # SSH connection establishment
    keys.go                  # Key generation and storage
    sync.go                  # File synchronization
  security/                  # Security controls (spec 004)
    egress.go                # Egress policy enforcement
    credentials.go           # Credential injection
    audit.go                 # Audit logging
  state/                     # VM state persistence
    state.go                 # Read/write VM config.yaml
  ui/                        # Terminal output and formatting
    output.go                # Human-readable output
    json.go                  # JSON output mode
    progress.go              # Progress indicators
```

**Acceptance criteria:**
- [ ] Each package has a single, well-defined responsibility
- [ ] The `internal/backend/` package owns all types related to the Backend interface
- [ ] The `internal/config/` package owns all types related to configuration
- [ ] The `internal/cmd/` package depends on other packages only through interfaces

## Design

### Backend Selection at Runtime

The CLI resolves which backend to use through the following logic:

```
1. If --backend flag is set, use that value.
2. Else if SD_BACKEND env var is set, use that value.
3. Else if config has a "defaults.backend" key, use that value.
4. Else use "lima".
```

The resolved backend name is passed to the backend registry (defined in 003-vm-backend.md, REQ-003-013) to retrieve the backend instance.

### VM State Machine

VM status values and valid transitions are defined in 003-vm-backend.md (REQ-003-004). The four states are: Creating, Running, Stopped, and Error.

At the architectural level, the state machine governs which operations are valid:

- `Create` transitions from non-existent to Running (or Stopped, backend-dependent)
- `Start` transitions from Stopped to Running
- `Stop` transitions from Running to Stopped
- `Destroy` removes the VM from any state
- Any state may transition to Error on failure
- Error can transition to Stopped (via Stop) or non-existent (via Destroy)

### Data Directory Layout

All `sd` state and configuration on the host resides under a single root directory, defaulting to `~/.sd/` and overridable via `$SD_HOME`. The directory structure is defined in 005-configuration.md (REQ-005-016):

```
$SD_HOME/                        # Default: ~/.sd/
  config.yaml                    # User-level configuration (spec 005)
  vms/                           # Per-VM state
    <name>/
      config.yaml                # VM-specific config and state (spec 005)
      ssh/                       # SSH keys for this VM (spec 007)
        id_ed25519               # Private key (0600)
        id_ed25519.pub           # Public key
```

The root directory MUST be created with mode `0700`. VM directories MUST be created with mode `0700`. SSH private keys MUST be stored with mode `0600`.

No raw secrets (tokens, passwords, API keys) are stored on disk under `$SD_HOME`. Configuration files store only environment variable references (`${VAR_NAME}`) that are resolved at runtime, as defined in 005-configuration.md (REQ-005-008).

## Error Handling

### Error Categories

All errors produced by `sd` fall into one of three categories:

| Category | Behavior | Example |
|----------|----------|---------|
| Fatal | Print error to stderr, exit non-zero | Backend not installed, invalid config, create failure |
| Warning | Print warning to stderr, continue | Credential injection failed, orphaned state detected |
| Silent | Log only (visible with `--verbose`) | VM already in target state |

### JSON Error Schema

When `--json` is active, all errors MUST be emitted as a JSON object to stdout:

```json
{
  "error": {
    "code": "BACKEND_NOT_FOUND",
    "message": "Backend \"avf\" is not registered. Available backends: lima",
    "details": {
      "requested": "avf",
      "available": ["lima"]
    }
  }
}
```

### Error Codes

| Code | Trigger | Category |
|------|---------|----------|
| `BACKEND_NOT_FOUND` | Requested backend is not registered | Fatal |
| `BACKEND_NOT_INSTALLED` | Backend runtime (e.g., limactl) is not on PATH | Fatal |
| `VM_EXISTS` | Creating a VM with a duplicate name | Fatal |
| `VM_NOT_FOUND` | Operating on a nonexistent VM | Fatal |
| `VM_RUNNING` | Destroying a running VM without --force | Fatal |
| `INVALID_VM_NAME` | Name does not match `^[a-z][a-z0-9-]{0,62}$` | Fatal |
| `INVALID_CONFIG` | Config file fails to parse or validate | Fatal |
| `MOUNT_REJECTED` | Attempted to mount $HOME or a prohibited path | Fatal |
| `CREATE_FAILED` | Backend create operation failed | Fatal |
| `PROVISION_FAILED` | Provisioning engine failed after VM creation | Fatal |
| `CREDENTIAL_INJECTION_FAILED` | Could not inject credentials via SSH | Warning |
| `ORPHANED_STATE` | State dir exists but backend has no matching VM | Warning |
| `ALREADY_RUNNING` | Start called on already-running VM | Silent |
| `ALREADY_STOPPED` | Stop called on already-stopped VM | Silent |

## Security Considerations

### Trust Boundaries

The `sd` system straddles two trust domains:

1. **Host (trusted)** -- the user's macOS host where `sd` runs, SSH keys reside, and credentials originate.
2. **Guest (untrusted)** -- the VM where AI agents run with bypass permissions. Treat as potentially compromised at all times.

The trust boundary is the hypervisor/container boundary enforced by the backend. All data crossing this boundary (mounts, credentials, SSH connections) MUST be explicitly authorized and minimal.

### Credential Handling

- Raw secrets (API tokens, SSH private keys for GitHub) MUST NOT be stored on disk inside the guest VM.
- Credentials MUST be injected via SSH environment variables at connect time. They are never written to files inside the guest.
- Host-side configuration files store only environment variable references (`${VAR_NAME}`) for how to retrieve credentials, NOT the credentials themselves. See 005-configuration.md (REQ-005-008).
- SSH keys for host-to-guest communication are per-VM and stored under `$SD_HOME/vms/<name>/ssh/` with mode 0600. See 007-connection.md (REQ-007-003).

### Blast Radius Analysis

If the guest VM is fully compromised (agent runs arbitrary code with root):

- **Filesystem**: Attacker sees only the guest disk and any explicitly mounted host directories. No access to host `$HOME`, dotfiles, browser storage, or other host data.
- **Network**: Attacker can reach only allowlisted endpoints (egress rules). Cannot reach LAN services, cloud metadata endpoints, or arbitrary internet hosts.
- **Credentials**: Attacker sees only the scoped tokens injected for this session. Tokens are single-repo and ideally short-lived. No access to host keychain, SSH agent, or broad organizational tokens.
- **Lateral movement**: Attacker cannot escape the VM to the host (hypervisor boundary). Cannot pivot to other VMs (each has its own network namespace and SSH keys).

### Mitigations

| Threat | Mitigation |
|--------|------------|
| Agent reads host dotfiles/secrets | No $HOME mount; explicit mount-only policy |
| Agent exfiltrates data over network | Default-deny egress with domain allowlist |
| Agent implants persistent backdoor | Auto-snapshots before destroy enable rollback; credentials injected at runtime only |
| Agent escalates to host | Hypervisor isolation (VZ/QEMU); no shared kernel |
| Agent uses broad token to damage org | Scoped fine-grained PATs; single-repo tokens only |
| Credential leakage to guest disk | SSH environment injection only; no credential files in guest |

## Testing Strategy

### Unit Tests

| Component | What is tested | Mocking strategy |
|-----------|---------------|------------------|
| Backend registry | Registration, lookup, listing | No mocks needed (pure logic) |
| Config system | Precedence, defaults, validation, env var binding | Viper test helpers; temp config files |
| State manager | CRUD on VM config.yaml, directory creation, permissions | Temp directories |
| VM name validation | Pattern matching, edge cases | No mocks needed |
| Mount validation | $HOME rejection, path normalization | Mock `os.UserHomeDir` |
| VMConfig builder | Flag/config merging | Mock Viper config |

### Integration Tests

| Scenario | What is tested | Requirements |
|----------|---------------|--------------|
| Lima backend lifecycle | Create, status, stop, start, destroy with real limactl | Lima installed; tagged `//go:build integration` |
| Config file loading | Real YAML parsing with Viper | Temp file system |
| State persistence | Write VM config.yaml, restart process, read it back | Temp file system |
| End-to-end create/destroy | Full `sd create` and `sd destroy` cycle | Lima installed; tagged `//go:build e2e` |

### Script Tests

CLI-level tests using `rsc.io/script` or equivalent test harness:

```
# Test: sd create with default backend
exec sd create test-vm --cpus 2 --memory 4GiB --json
stdout '"name":"test-vm"'
stdout '"backend":"lima"'

# Test: sd create rejects duplicate names
exec sd create test-vm --json
stderr 'VM_EXISTS'
exit 1

# Test: sd create rejects invalid names
exec sd create INVALID --json
stderr 'INVALID_VM_NAME'
exit 1

# Test: sd destroy with force (auto-snapshot by default)
exec sd destroy test-vm --force --json
stdout '"name":"test-vm"'
exit 0

# Test: sd destroy with --no-snapshot
exec sd destroy test-vm --force --no-snapshot --json
exit 0
```

### Requirement-to-Test Mapping

| Requirement | Unit | Integration | Script |
|-------------|------|-------------|--------|
| REQ-001-001 | Package structure verification | - | - |
| REQ-001-002 | Import graph analysis (go vet) | - | - |
| REQ-001-003 | Interface compliance (compile-time) | Lima lifecycle | - |
| REQ-001-004 | YAML generation tests | Lima create/destroy | sd create --backend lima |
| REQ-001-005 | Interface non-specificity (review) | - | - |
| REQ-001-006 | VMConfig builder, validation | Full create flow | sd create tests |
| REQ-001-007 | Connection logic, credential injection | Full connect flow | sd connect tests |
| REQ-001-008 | Destroy logic, state cleanup | Full destroy flow | sd destroy tests |
| REQ-001-009 | Precedence tests, validation | Config file loading | sd config list |
| REQ-001-010 | JSON output formatting | - | All --json tests |
| REQ-001-011 | Name validation, uniqueness | State persistence | sd create duplicate |
| REQ-001-012 | Mount rejection, egress defaults | Provisioned VM network test | - |
| REQ-001-013 | Provisioning script content | Provisioned VM inspection | - |
| REQ-001-014 | Package structure verification | - | - |

## Dependencies

### Depends On

- None (this is the foundational spec)

### Depended On By

- [002-cli.md](002-cli.md) -- CLI command structure and flags build on the component model and data flows defined here
- [003-vm-backend.md](003-vm-backend.md) -- Backend implementation details build on the backend architecture defined here
- [004-security.md](004-security.md) -- Security controls build on the layered security model defined here
- [005-configuration.md](005-configuration.md) -- Configuration system builds on the config architecture defined here
- [006-provisioning.md](006-provisioning.md) -- Provisioning logic builds on the guest environment and security layers defined here
- [007-connection.md](007-connection.md) -- Connection management builds on the component model and credential flow defined here

## Alternatives Considered

### Alternative 1: Docker-only, no VM

Using Docker containers instead of VMs as the primary isolation mechanism.

**Rejected because:** Docker shares the host kernel, which means a container escape gives full host access. For a tool explicitly designed to run AI agents with bypass permissions, hypervisor-level isolation is the appropriate default. Docker is preserved as an optional backend for users who accept the tradeoff.

### Alternative 2: Direct AVF (no Lima)

Using the Apple Virtualization framework directly via Go bindings instead of shelling out to Lima.

**Rejected as default because:** Lima provides a mature, well-tested CLI with disk management, network configuration, SSH setup, and cloud-init support. Building all of that from scratch for AVF would delay the initial release significantly. AVF is preserved as a future backend for users who want lower overhead or tighter integration.

### Alternative 3: Vagrant

Using Vagrant as the VM management layer.

**Rejected because:** Vagrant adds Ruby as a dependency, has slower boot times on macOS with modern providers, and is designed for reproducible dev environments rather than ephemeral security sandboxes. Lima is lighter, faster on Apple Silicon, and more closely aligned with the container/cloud-native tooling ecosystem.

## Open Questions

- **OQ-1**: Should `sd` support running multiple agents in the same VM simultaneously (separate tmux sessions), or enforce one-agent-per-VM? One-agent-per-VM is simpler and more secure but uses more resources.
- **OQ-2**: What is the credential rotation strategy for long-running VMs? Should `sd` automatically rotate tokens on a schedule, or only on explicit `sd connect`?
- **OQ-3**: Should the Docker backend use Docker-in-Docker or mount the host Docker socket? Neither is ideal from a security perspective; this needs resolution before implementing the Docker backend.

## Revision History

| Date       | Author | Change Description |
|------------|--------|--------------------|
| 2026-03-27 | claude | Initial draft      |
| 2026-03-27 | claude | Rewrite after cross-spec review: removed duplicated Go interfaces, type definitions, config schemas, and directory layouts that conflicted with specs 003/005/007. Spec now focuses on high-level architecture and references dedicated specs for implementation details. Fixed: Backend/registry conflicts with 003, directory layout conflicts with 005/007, config schema conflicts with 005, status value conflicts with 003, credential file references violating security spec, incorrect dependency filenames, `sd config show` -> `sd config list`, `--snapshot` opt-in -> auto-snapshot default with `--no-snapshot` opt-out, `sd connect` auto-start semantics, `--json` on connect behavior. |
