# sd - Implementation Plan

**Created:** 2026-03-28
**Status:** Draft
**Source Specs:** specs/001-architecture.md through specs/007-connection.md
**Design Source:** plans/sd/03-plan/design-context.md

---

## Overview

`sd` (secure-dev) is a Go CLI tool that creates, configures, and manages secure VM environments for running AI coding agents with bypass permissions. It automates VM lifecycle, SSH configuration, tool provisioning, credential injection, and session management.

The implementation follows a layered architecture: a Cobra-based CLI layer (`internal/cmd/`) depends on domain service packages (`internal/backend/`, `internal/config/`, `internal/state/`, `internal/provision/`, `internal/connection/`, `internal/security/`) through interfaces, with a cross-cutting output layer (`internal/ui/`) that has zero internal dependencies. Lima is the default and first backend implementation.

Key architectural decisions that shape the plan: (1) instance-based Viper for testable configuration, (2) the Backend interface + registry pattern enables pluggable backends without CLI changes, (3) credentials are never written to disk -- they flow through SSH `SendEnv`/`AcceptEnv` at connection time, (4) the `App` struct in `internal/cmd/` holds all injected dependencies for wiring.

---

## Architecture Decisions

| Decision | Choice | Rationale | Spec Basis |
|----------|--------|-----------|------------|
| Module structure | 8 packages under `internal/` | Single-responsibility; clear dependency graph | REQ-001-001, REQ-001-014 |
| VM backend abstraction | `backend.Backend` interface + registry | Pluggable backends without CLI changes | REQ-001-003, REQ-003-001, REQ-003-013 |
| Config loading | Instance-based Viper (not global) | Testable; no global state; explicit DI | REQ-001-009, REQ-005-014 |
| State separate from config | `state/` imports `config.VMConfig` | Single-writer rule for VM metadata | REQ-001-001, REQ-001-011 |
| Credential handling | `${VAR}` references resolved at connect time via SSH SendEnv | Never written to disk inside VM | REQ-004-011, REQ-005-008 |
| SSH key management | `connection/` owns per-VM Ed25519 key pairs | Backend stays focused on VM lifecycle | REQ-007-003 |
| Lima snapshots | APFS `cp -c` clone | Lima+VZ has no native QEMU snapshots | REQ-003-018 |
| Provisioning execution | Via `backend.Exec()` | Future backends work without changing provisioner | REQ-006-001 |
| Optional backend capabilities | Type assertions (`Snapshotter`, `Cloner`, `FileSync`) | Avoids polluting core interface | REQ-003-008 to REQ-003-010 |
| Logging library | `log/slog` (stdlib) | No external dep needed; structured + leveled | AGENTS.md |
| Error types | Sentinel errors + `SDError` struct + `JSONError` envelope | Three layers: matching, wrapping, display | REQ-001-010, REQ-003-021 |
| `ExecResult` duplication | Accept separate types in `backend/` and `connection/` | Avoids circular import; testability | REQ-007-013, REQ-007-014 |
| Dependency struct | `App` struct in `internal/cmd/` | Idiomatic CLI wiring; single construction point | REQ-001-002 |

---

## Shared Abstractions

### 1. `ui.Printer` interface
- **Location:** `internal/ui/output.go`
- **Purpose:** All user-visible output (Info, Warn, Error, Data, Fatal, Progress). Respects `--json` and `--quiet` modes.
- **Consumers:** Every package that produces output; every CLI command.
- **Definition:** See design-context.md `internal/ui/` section.

### 2. `backend.Backend` interface + types
- **Location:** `internal/backend/backend.go`
- **Purpose:** Core VM lifecycle interface. Types: `VMConfig`, `VMStatus`, `VMInfo`, `SSHConfig`, `ExecResult`, `SnapshotInfo`, `Mount`, `NetworkMode`, `ProvisionScript`.
- **Consumers:** CLI commands, provisioner, connection manager, Lima backend.
- **Definition:** See design-context.md Interfaces & Contracts section.

### 3. `backend` sentinel errors
- **Location:** `internal/backend/errors.go`
- **Purpose:** `ErrVMNotFound`, `ErrVMAlreadyExists`, `ErrVMNotRunning`, `ErrBackendNotAvailable`, `ErrBackendNotFound`, `ErrNotImplemented`.
- **Consumers:** All backend implementations, CLI error mapping.

### 4. `backend` registry
- **Location:** `internal/backend/registry.go`
- **Purpose:** `Register()`, `Get()`, `List()`, `Default()` for runtime backend selection.
- **Consumers:** CLI layer (backend resolution), backend `init()` functions.

### 5. `config.Config` + `config.Loader` interface
- **Location:** `internal/config/config.go`, `internal/config/schema.go`
- **Purpose:** Layered config loading with 5-level precedence. Types: `Config`, `Defaults`, `Security`, `VMDef`, `VMConfig`, `VMState`.
- **Consumers:** Every CLI command, provisioner, connection manager.

### 6. `state.Manager` interface
- **Location:** `internal/state/state.go`
- **Purpose:** CRUD for per-VM state files at `$SD_HOME/vms/<name>/config.yaml`.
- **Consumers:** CLI commands (create, destroy, list, status).

### 7. `cmd.App` struct
- **Location:** `internal/cmd/root.go`
- **Purpose:** Holds all injected dependencies (Config, State, UI, Backend, Connector, Syncer, Provisioner, Validator, Egress, Creds, Audit).
- **Consumers:** Every CLI command handler.

### 8. `ui.SDError` + `ui.JSONError`
- **Location:** `internal/ui/json.go`
- **Purpose:** Structured error type with code, message, details, and cause chain. `JSONError` is the serialization envelope for `--json` mode.
- **Consumers:** All CLI command error paths.

---

## Phased Delivery

### Phase 1: Project Scaffolding & Core Types

**Objective:** Establish the build system, module path, entry point, and the two zero-dependency foundation packages (`ui/` and `config/` types).
**Prerequisites:** None (first phase)

#### Tasks

**1.1 Initialize Go module and entry point**
- **What:** Create `go.mod`, `cmd/sd/main.go`, and `Makefile`.
- **Files:**
  - Create: `go.mod` -- module path `github.com/gberns/sd`, Go 1.22+
  - Create: `cmd/sd/main.go` -- calls `internal/cmd.Execute()`, blank import for Lima registration
  - Create: `Makefile` -- targets: build, test, test-int, test-e2e, vet, lint, install
- **Key details:** Module path uses `github.com/gberns/sd` (confirmed from git remote). `main.go` is <10 lines. Makefile includes `go build -o sd ./cmd/sd/`, ldflags for version/commit/date injection.
- **Acceptance criteria:**
  - [ ] `go build ./cmd/sd/` compiles (even if Execute() is a stub)
  - [ ] `make build` produces the `sd` binary
  - [ ] `make test` runs `go test ./...`
- **Dependencies:** None

**1.2 Implement `internal/ui/` package**
- **What:** Output formatting layer with zero internal dependencies.
- **Files:**
  - Create: `internal/ui/output.go` -- `Printer` interface, `NewPrinter()`, `Info/Warn/Error/Data/Fatal/Progress` methods
  - Create: `internal/ui/json.go` -- `JSONError`, `JSONErrorBody`, `SDError` struct with `Error()/Unwrap()`, error code constants (`ErrCodeVMNotFound`, etc.), error constructor helpers
  - Create: `internal/ui/progress.go` -- `ProgressReporter` interface, spinner for TTY, line-based for non-TTY, no-op for JSON mode
  - Create: `internal/ui/table.go` -- tabular output for list/status/config commands
- **Key details:** `Printer` writes Info/Progress to stderr, Data to stdout. In JSON mode: `Data(v)` writes `{"ok": true, "data": v}` to stdout; errors write `{"ok": false, "error": {...}}`. `SDError` carries Code, Message, Details, and cause error. All error codes from REQ-001-010 and REQ-002-018.
- **Acceptance criteria:**
  - [ ] `ui.NewPrinter(out, err, true)` produces JSON output only on stdout
  - [ ] `ui.NewPrinter(out, err, false)` produces human-readable output
  - [ ] `SDError` implements `error` and `Unwrap()`
  - [ ] All error code constants from spec 001/002 are defined
  - [ ] Tests: JSON serialization, stream routing, progress modes
- **Dependencies:** None

**1.3 Implement `internal/config/` types and defaults**
- **What:** Configuration value types, schema structs, and built-in defaults. Not the full Viper loader yet (that needs testing infrastructure).
- **Files:**
  - Create: `internal/config/schema.go` -- `Config`, `Defaults`, `Security`, `VMDef`, `VMConfig`, `VMState` structs, `MountPolicy*` constants, `VMStatus*` constants, `ConfigSource*` constants, `ConfigEntry`, `ConfigError`, `ValidationResult` types
  - Create: `internal/config/defaults.go` -- compiled-in defaults (backend=lima, cpus=4, memory=8GiB, disk=100GiB, image=ubuntu:24.04, egress allowlist from REQ-004-007)
  - Create: `internal/config/resolve.go` -- `${VAR}` reference expansion from host environment, `$$` escaping
- **Key details:** Types use both `yaml` and `mapstructure` struct tags. Default egress allowlist: 11 entries from REQ-004-007. `Resolve()` is lazy (called at use-time, not load-time per REQ-005-008). Sensitive values (`*_TOKEN`, `*_KEY`, `*_SECRET`, `*_PASSWORD`) are identified by pattern.
- **Acceptance criteria:**
  - [ ] All config types from design-context.md are defined with correct tags
  - [ ] Default values match REQ-005-004 exactly
  - [ ] `Resolve("${HOME}")` returns the host's HOME value
  - [ ] `Resolve("$$LITERAL")` returns `$LITERAL`
  - [ ] Unset variables resolve to empty string (caller handles warning)
  - [ ] Tests: env var resolution, escaping, defaults
- **Dependencies:** None

**1.4 Implement `internal/config/` Viper-based loader**
- **What:** Full configuration loader with 5-level precedence, project config discovery, and security key filtering.
- **Files:**
  - Create: `internal/config/config.go` -- `Loader` interface implementation, `Load()`, `Get()`, `GetForVM()`, `Source()`, `Validate()`, `Set()`, `List()` methods, Viper initialization with `SD_` prefix, env key replacer, project config walk-up discovery
- **Key details:** Instance-based Viper (not global). `SetEnvPrefix("SD")`, `AutomaticEnv()`, `SetEnvKeyReplacer("." -> "_")`. Project config found by walking up from cwd looking for `.sd/config.yaml`. Security keys (`security.*`) from project config are silently ignored with debug log (REQ-005-017). `Set()` validates after write and rolls back on failure. `SDHome()` function returns `$SD_HOME` or `~/.sd`.
- **Acceptance criteria:**
  - [ ] Precedence: CLI > env > project > user > default (REQ-005-001)
  - [ ] Missing config file is not an error; invalid YAML is fatal
  - [ ] `security.*` keys in project config are ignored with warning
  - [ ] `SD_BACKEND=docker` overrides `defaults.backend`
  - [ ] `GetForVM()` applies inheritance chain (REQ-005-015)
  - [ ] Tests: precedence with temp files, env var override, project config discovery, security key filtering
- **Dependencies:** 1.3

#### Phase 1 Exit Criteria
- [ ] `go build ./cmd/sd/` compiles successfully
- [ ] `internal/ui/` has full test coverage for JSON/human output modes
- [ ] `internal/config/` loads config from all 5 sources with correct precedence
- [ ] All shared types are defined and importable

---

### Phase 2: Backend Interface, State Manager & Security Foundations

**Objective:** Establish the backend abstraction layer, VM state persistence, and pure-function security validations.
**Prerequisites:** Phase 1 -- `ui/` for error formatting, `config/` for types

#### Tasks

**2.1 Implement `internal/backend/` interface package**
- **What:** Backend interface, types, registry, sentinel errors, and optional capability interfaces.
- **Files:**
  - Create: `internal/backend/backend.go` -- `Backend` interface (Name, Available, Create, Start, Stop, Destroy, Status, List, SSHConfig, Exec), `VMStatus`, `VMInfo`, `SSHConfig`, `ExecResult`, `SnapshotInfo`, `NetworkMode`, `Mount`, `ProvisionScript`, `VMConfig` types, optional interfaces (`Snapshotter`, `Cloner`, `FileSync`)
  - Create: `internal/backend/errors.go` -- sentinel errors: `ErrVMNotFound`, `ErrVMAlreadyExists`, `ErrVMNotRunning`, `ErrBackendNotAvailable`, `ErrBackendNotFound`, `ErrNotImplemented`
  - Create: `internal/backend/registry.go` -- `Register()`, `Get()`, `List()`, `Default()` with sync.RWMutex
- **Key details:** All Backend methods take `context.Context`. `Register()` panics on duplicate name. `Default()` prefers "lima". `VMConfig.EnvVars` note: sensitive values must not be in Lima YAML (REQ-003-023). `Mount.Writable` defaults to false (REQ-004-004).
- **Acceptance criteria:**
  - [ ] Compile-time interface compliance check: `var _ Backend = (*lima.LimaBackend)(nil)` compiles
  - [ ] Registry: Register, Get, List, Default, duplicate-panic all tested
  - [ ] Sentinel errors work with `errors.Is`
  - [ ] `VMStatus` JSON-serializes as lowercase strings
  - [ ] `List()` returns empty slice (not nil) from registry
- **Dependencies:** None (stdlib only, ui for future progress during long ops)

**2.2 Implement `internal/state/` package**
- **What:** VM metadata persistence at `$SD_HOME/vms/<name>/config.yaml`.
- **Files:**
  - Create: `internal/state/state.go` -- `Manager` interface implementation: `Create()`, `Get()`, `Update()`, `Delete()`, `List()`, `Exists()`. Directory creation with mode 0700, file creation with mode 0600.
  - Create: `internal/state/errors.go` -- `ErrVMNotFound` (state-layer specific)
- **Key details:** Uses `config.VMConfig` as the persisted type (per design tension #1 resolution). Creates `$SD_HOME/vms/<name>/` with 0700. Writes `config.yaml` with 0600. `Delete()` is best-effort (REQ-001-008). Timestamps in RFC 3339. YAML marshaling via `gopkg.in/yaml.v3`. File-level advisory locking (`flock`) on `$SD_HOME/vms/<name>/config.yaml` to prevent concurrent `sd` invocations from corrupting state.
- **Acceptance criteria:**
  - [ ] Create/Get round-trip preserves all VMConfig fields
  - [ ] Directory permissions are 0700, file permissions are 0600
  - [ ] `Delete()` removes entire VM directory
  - [ ] `Get()` returns `ErrVMNotFound` for nonexistent VMs
  - [ ] `List()` returns all VM names from `$SD_HOME/vms/`
  - [ ] Tests: CRUD with temp directories, permission checks
- **Dependencies:** 1.3 (config types)

**2.3 Implement `internal/security/mount.go`**
- **What:** Mount path validation -- pure function, testable immediately.
- **Files:**
  - Create: `internal/security/mount.go` -- `ValidateMountPath(hostPath string, mode MountMode) error`, `MountMode` type, sensitive path list (from REQ-004-005), symlink resolution before checking
  - Create: `internal/security/types.go` -- `MountMode`, `TokenType`, `SecurityWarning`, `EgressDomain`, `DomainSource`, `CredentialEntry`, `CommandLogEntry`, `EventLogEntry`, `AuditEntry`, `AuditFilter`, `ChainBreak`, `SecurityPosture`, `MountSummary` types
- **Key details:** Sensitive paths: `$HOME`, `~/.ssh`, `~/.aws`, `~/.config`, `~/.gnupg`, `~/.kube`, `~/.docker`, browser profile dirs, `/var/run/docker.sock`. Uses `filepath.EvalSymlinks()` before checking. User-extensible via `security.sensitive_paths` config (REQ-004-023). `ValidateToken()` detects classic PATs by `ghp_` prefix.
- **Acceptance criteria:**
  - [ ] `ValidateMountPath("~")` returns error
  - [ ] `ValidateMountPath("~/.ssh")` returns error
  - [ ] `ValidateMountPath("/var/run/docker.sock")` returns error
  - [ ] Symlinks to $HOME are resolved and rejected
  - [ ] `ValidateMountPath("~/projects/my-repo")` succeeds
  - [ ] `ValidateToken("ghp_...", TokenGitHubPAT)` returns CLASSIC_PAT warning
  - [ ] Tests: all sensitive paths, symlink resolution, subdirectory allowed
- **Dependencies:** 1.3 (config types for sensitive_paths)

**2.4 Implement backend stubs (avf, docker, incus)**
- **What:** Stub implementations that register and return `ErrNotImplemented`.
- **Files:**
  - Create: `internal/backend/avf/avf.go` -- implements `Backend`, all methods return `ErrNotImplemented`
  - Create: `internal/backend/docker/docker.go` -- same pattern
  - Create: `internal/backend/incus/incus.go` -- same pattern
- **Key details:** Each has `init()` calling `backend.Register()`. `Available()` returns `ErrNotImplemented` with message naming the backend. Each file is ~60 lines.
- **Acceptance criteria:**
  - [ ] All stubs compile
  - [ ] All stubs register in the registry
  - [ ] `Available()` returns actionable error
  - [ ] All methods return `ErrNotImplemented`
- **Dependencies:** 2.1

#### Phase 2 Exit Criteria
- [ ] Backend interface and registry are complete with full test coverage
- [ ] State manager handles CRUD with correct permissions
- [ ] Mount path validation covers all sensitive paths from REQ-004-005
- [ ] All backend stubs compile and register

---

### Phase 3: Lima Backend Implementation

**Objective:** Implement the default Lima backend -- the first real backend that can create and manage VMs.
**Prerequisites:** Phase 2 -- backend interface, registry, types

#### Tasks

**3.1 Implement Lima YAML generation**
- **What:** Convert `backend.VMConfig` to Lima YAML configuration.
- **Files:**
  - Create: `internal/backend/lima/yaml.go` -- `vmConfigToLima()` function, `limaConfig` internal type, platform detection (arm64 -> vz, amd64 -> qemu), mount mapping, sensitive env filtering
  - Create: `internal/backend/lima/images.go` -- built-in `imageMap` from short names to Lima image URLs (ubuntu:24.04, ubuntu:22.04, debian:12 with arm64/amd64 variants)
- **Key details:** On arm64 darwin: `vmType: vz`, `mountType: virtiofs`. Empty mounts array overrides Lima default (REQ-003-017). Sensitive env vars (`*_TOKEN`, `*_KEY`, `*_SECRET`, `*_PASSWORD`) are filtered from the `env` YAML field (REQ-003-023). Non-sensitive env vars pass through. `ssh.forwardAgent: false` always.
- **Acceptance criteria:**
  - [ ] Generated YAML sets `vmType: vz` on arm64
  - [ ] Generated YAML has `mounts: []` when no mounts specified
  - [ ] Sensitive env vars are absent from generated YAML env section
  - [ ] Non-sensitive env vars are present in YAML env section
  - [ ] Short name `ubuntu:24.04` resolves to correct image URLs
  - [ ] Unknown short name produces fatal error listing available names
  - [ ] Tests: YAML generation for various VMConfig inputs, platform detection, env filtering
- **Dependencies:** 2.1

**3.2 Implement Lima backend lifecycle**
- **What:** Full Backend interface implementation using `limactl`.
- **Files:**
  - Create: `internal/backend/lima/lima.go` -- `LimaBackend` struct, `init()` registration, `Name()`, `Available()`, `Create()`, `Start()`, `Stop()`, `Destroy()`, `Status()`, `List()`, `SSHConfig()`, `Exec()`. All use `exec.CommandContext()`.
- **Key details:** `Available()` checks `limactl` in PATH. `Create()` writes temp YAML, runs `limactl create --name=sd-<name> <yaml> --tty=false`, blocks until running. `Start/Stop/Destroy` call `limactl start/stop/delete`. `Status()` parses `limactl list --json`. `SSHConfig()` returns host/port/user from `limactl show-ssh --format=config`. `Exec()` runs `limactl shell sd-<name> -- <cmd>`. Instance names prefixed with `sd-` to namespace. Context cancellation kills subprocesses.
- **Acceptance criteria:**
  - [ ] `Available()` returns nil when `limactl` is in PATH
  - [ ] `Available()` returns actionable error when `limactl` is missing
  - [ ] `Create()` generates Lima YAML and runs limactl
  - [ ] `Status()` correctly maps limactl JSON to VMStatus
  - [ ] `SSHConfig()` returns valid connection details
  - [ ] All methods use `exec.CommandContext(ctx, ...)`
  - [ ] Tests: unit tests with mock command execution; integration tests (tagged) with real limactl
- **Dependencies:** 3.1

**3.3 Implement Lima snapshot support**
- **What:** `Snapshotter` interface using APFS clones.
- **Files:**
  - Create: `internal/backend/lima/snapshot.go` -- `SnapshotCreate`, `SnapshotApply`, `SnapshotDelete`, `SnapshotList`. Clone-based strategy with `cp -c` on macOS.
- **Key details:** Flow: stop VM -> `cp -c ~/.lima/sd-<name> ~/.lima/.snapshots/sd-<name>/<tag>/` -> restart. Metadata in `$SD_HOME/vms/<name>/snapshots.yaml`. `SnapshotApply` replaces instance dir. Atomic: failed snapshot does not corrupt running VM.
- **Acceptance criteria:**
  - [ ] `SnapshotCreate` uses APFS clones (`cp -c`) on macOS
  - [ ] Snapshot metadata is written to `$SD_HOME/vms/<name>/snapshots.yaml`
  - [ ] `SnapshotList` returns name, timestamp, size
  - [ ] Failed snapshot does not corrupt the running VM
  - [ ] Tests: unit tests for metadata management; integration tests for full snapshot lifecycle
- **Dependencies:** 3.2

#### Phase 3 Exit Criteria
- [ ] Lima backend passes all unit tests
- [ ] YAML generation correctly handles all VMConfig scenarios
- [ ] `limactl` integration works for create/start/stop/destroy lifecycle
- [ ] Snapshot support uses APFS clones

---

### Phase 4: Connection Infrastructure & SSH Management

**Objective:** Establish SSH key management, config fragment generation, and environment variable injection.
**Prerequisites:** Phase 2 -- backend types (for SSHConfig), config (for env var resolution)

#### Tasks

**4.1 Implement SSH key generation**
- **What:** Per-VM Ed25519 SSH key pair generation and management.
- **Files:**
  - Create: `internal/connection/keys.go` -- `GenerateKeyPair(vmDir string) error`, `KeyPath(vmDir string) string`, `PubKeyPath(vmDir string) string`. Uses `golang.org/x/crypto/ssh` for Ed25519 key generation.
- **Key details:** Keys stored at `$SD_HOME/vms/<name>/ssh/id_ed25519[.pub]`. Private key mode 0600. SSH directory mode 0700. Each VM gets a unique key pair. Public key is injected into VM's `authorized_keys` during creation.
- **Acceptance criteria:**
  - [ ] Generated keys are Ed25519
  - [ ] Private key file has permissions 0600
  - [ ] SSH directory has permissions 0700
  - [ ] Each VM gets a unique key pair
  - [ ] Tests: key generation, file permissions, unique keys per call
- **Dependencies:** None (stdlib + golang.org/x/crypto)

**4.2 Implement SSH config management**
- **What:** Write/remove `~/.ssh/config.d/sd-<name>` fragments.
- **Files:**
  - Create: `internal/connection/sshconfig.go` -- `WriteSSHConfig(name string, cfg backend.SSHConfig) error`, `RemoveSSHConfig(name string) error`, `SSHConfigFor(name string) (*SSHConfigData, error)`. Generates correct fragment for TCP vs VSOCK transport.
- **Key details:** TCP: `StrictHostKeyChecking yes`, `UserKnownHostsFile $SD_HOME/vms/<name>/ssh/known_hosts`. VSOCK: `StrictHostKeyChecking no`, `UserKnownHostsFile /dev/null`. Always: `ForwardAgent no`, `ForwardX11 no`, `LogLevel ERROR`, `SendEnv SD_* ANTHROPIC_* GITHUB_* GH_*`. Warn if `~/.ssh/config` doesn't include `config.d/*`.
- **Acceptance criteria:**
  - [ ] TCP fragment has `StrictHostKeyChecking yes` + known_hosts
  - [ ] VSOCK fragment has `ProxyCommand limactl ssh --stdio <name>`
  - [ ] All fragments include `ForwardAgent no`, `ForwardX11 no`
  - [ ] All fragments include `SendEnv` patterns
  - [ ] `RemoveSSHConfig` deletes the fragment file
  - [ ] Tests: fragment generation for TCP and VSOCK, removal
- **Dependencies:** 2.1 (backend SSHConfig type)

**4.3 Implement environment variable injection**
- **What:** Resolve `${VAR}` references and prepare env vars for SSH SendEnv.
- **Files:**
  - Create: `internal/connection/env.go` -- `ResolveEnvVars(envMap map[string]string) (resolved map[string]string, warnings []string)`. Resolves `${VAR}` from host env. Returns warnings for unset variables.
- **Key details:** Uses `config.Resolve()` for each value. Unresolvable variables set to empty string with warning. Sensitive values are never logged. The connection layer prepares `-o SendEnv=VAR` SSH args for each resolved variable.
- **Acceptance criteria:**
  - [ ] `${HOME}` resolves to host HOME value
  - [ ] Unset variables produce warnings and resolve to empty string
  - [ ] Resolved values are not logged at any level
  - [ ] Tests: resolution, unset handling, empty values
- **Dependencies:** 1.3 (config resolve)

#### Phase 4 Exit Criteria
- [ ] SSH key generation produces valid Ed25519 keys with correct permissions
- [ ] SSH config fragments are correct for TCP and VSOCK
- [ ] Environment variable resolution works with `${VAR}` syntax

---

### Phase 5: CLI Core & Provisioning Engine

**Objective:** Build the CLI framework (root command, App struct, PersistentPreRunE) and the core VM lifecycle commands, plus the provisioning module system.
**Prerequisites:** Phase 1-4 -- all foundation packages

#### Tasks

**5.1 Implement root command and App struct**
- **What:** CLI entry point, global flags, dependency wiring, and the `App` struct.
- **Files:**
  - Create: `internal/cmd/root.go` -- `Execute()`, root cobra.Command, `PersistentPreRunE` (load config, validate --verbose/--quiet mutual exclusivity, init logger, set up output formatter), `App` struct with all dependency fields, `RootFlags`, `VMNamePattern`, `ValidateVMName()`, `resolveVMName()` helper
- **Key details:** Global flags: `--json`, `--verbose`/`-v`, `--quiet`/`-q`, `--config`, `--vm`. `PersistentPreRunE` constructs the `App` struct. Backend resolved via `backend.Get(cfg.Defaults.Backend)`. Command groups: VM Management, Connection, Configuration, Provisioning, Security, Diagnostics, Utility. Enable prefix matching. `SilenceUsage: true`, `SilenceErrors: true`. Logger: `log/slog` with TextHandler to stderr. `PersistentPreRunE` skips config loading and App construction for `version`, `completion`, and `help` commands (name-based check). Phase-7 security fields (`Egress`, `Creds`, `Audit`) are initialized with no-op stubs (`noopAuditLogger`, `noopEgressController`, `noopCredentialInjector`) in `internal/security/noop.go`; replaced with real implementations in Phase 7.
- **Acceptance criteria:**
  - [ ] `sd` with no args prints help and exits 0
  - [ ] `--verbose --quiet` exits with code 2
  - [ ] `--json` flag available on all commands
  - [ ] `sd version` works without config file
  - [ ] VM name validation regex: `^[a-z][a-z0-9-]{0,62}$`
  - [ ] Tests: flag parsing, mutual exclusivity, name validation
- **Dependencies:** 1.2, 1.4, 2.1

**5.2 Implement `sd create` command**
- **What:** VM creation with full lifecycle: validate, resolve backend, build VMConfig, create, provision, write state.
- **Files:**
  - Create: `internal/cmd/create.go` -- `createCmd` struct, flags: `--backend`, `--cpus`, `--memory`, `--disk`, `--modules`, `--mount`, `--allow-egress`. Implements rollback on failure (destroy partial VM).
- **Key details:** Data flow per REQ-001-006: ValidateVMName -> State.Exists (reject duplicates early) -> Config.GetForVM -> SecurityValidator.ValidateMountPath for each mount -> GenerateKeyPair (must happen before Backend.Create so public key can be included in Lima YAML) -> build backend.VMConfig (include public key path) -> Backend.Create -> capture SSH host key via `ssh-keyscan` and write to `$SD_HOME/vms/<name>/ssh/known_hosts` (REQ-007-004) -> WriteSSHConfig -> Provisioner.Provision -> State.Create -> Audit.LogEvent. On failure: Backend.Destroy for cleanup (using `context.Background()` for rollback). `--dry-run` prints resolved VMConfig without executing. Long help text includes threat model summary per REQ-004-001.
- **Acceptance criteria:**
  - [ ] `sd create myvm` creates VM with defaults
  - [ ] `sd create myvm --cpus 4 --memory 8GiB` passes flags to backend
  - [ ] `sd create myvm --mount ~/projects:/project` creates read-only mount
  - [ ] `sd create myvm --mount ~:/home` rejected by mount validation
  - [ ] SSH key pair generated before Backend.Create, public key included in Lima YAML
  - [ ] SSH host key captured via `ssh-keyscan` after VM start, written to known_hosts
  - [ ] Failed provisioning triggers VM destroy (rollback)
  - [ ] `--json` outputs structured result
  - [ ] `--modules all` provisions every available module (REQ-006-015)
  - [ ] Long help text includes threat model summary (REQ-004-001)
  - [ ] Tests: flag parsing, rollback logic, mount validation integration, host key capture
- **Dependencies:** 5.1, 2.2, 2.3, 3.2, 4.1, 4.2

**5.3 Implement `sd destroy`, `sd start`, `sd stop` commands**
- **What:** VM lifecycle management commands.
- **Files:**
  - Create: `internal/cmd/destroy.go` -- `--force`, `--no-snapshot`, auto-snapshot before destroy (REQ-004-019), state cleanup, SSH config removal
  - Create: `internal/cmd/start.go` -- start stopped VM
  - Create: `internal/cmd/stop.go` -- stop running VM
- **Key details:** Destroy flow per REQ-001-008: State.Get -> Backend.Status -> if running && !force: error -> if !noSnapshot: SnapshotCreate (if snapshot fails and --force: warn and proceed; if snapshot fails and !--force: abort with error requiring `--force --no-snapshot`) -> if running: Stop -> Destroy -> State.Delete -> RemoveSSHConfig -> delete SSH key files at `$SD_HOME/vms/<name>/ssh/` (REQ-004-014) -> Audit.LogEvent. Start/Stop are thin wrappers with status check. Idempotent: start on running is no-op, stop on stopped is no-op.
- **Acceptance criteria:**
  - [ ] `sd destroy myvm` without `--force` errors on running VM
  - [ ] `sd destroy myvm --force` proceeds
  - [ ] Auto-snapshot before destroy (default behavior)
  - [ ] `--no-snapshot` skips auto-snapshot
  - [ ] State cleanup succeeds even if backend destroy partially fails
  - [ ] SSH key files deleted from `$SD_HOME/vms/<name>/ssh/` on destroy (REQ-004-014)
  - [ ] Failed auto-snapshot with --force: warn and proceed
  - [ ] Failed auto-snapshot without --force: abort with actionable error
  - [ ] Start/stop are idempotent
  - [ ] Tests: destroy flow, force flag, snapshot behavior, key cleanup
- **Dependencies:** 5.1, 2.2, 3.2, 3.3

**5.4 Implement `sd list` and `sd status` commands**
- **What:** VM listing and status reporting.
- **Files:**
  - Create: `internal/cmd/list.go` -- alias `ls`, tabular output, `--json`
  - Create: `internal/cmd/status.go` -- single VM or all VMs, includes provisioning state
- **Key details:** `list` merges state manager data with backend live status. Detects orphaned state (metadata exists but backend has no VM) and reports as warning. `status` shows: name, status, backend, resources, IP, created time, provisioning state (if available). Both support `--json`.
- **Acceptance criteria:**
  - [ ] `sd list` shows all VMs in tabular format
  - [ ] `sd ls` is equivalent to `sd list`
  - [ ] `sd list --json` outputs JSON array
  - [ ] Orphaned state detected and warned
  - [ ] `sd status myvm --json` includes provisioning info
  - [ ] Tests: output formatting, orphan detection
- **Dependencies:** 5.1, 2.2, 3.2

**5.5a Implement provisioning engine core**
- **What:** Module types, YAML parsing, dependency resolution, and module loading.
- **Files:**
  - Create: `internal/provision/module.go` -- `Module`, `Script`, `Probe`, `ModuleStatus`, `ProvisionState`, `ModuleExecutionStatus` types, YAML parsing and validation
  - Create: `internal/provision/loader.go` -- load built-in modules via `//go:embed`, discover custom modules from `~/.sd/provisions/`, name conflict detection
  - Create: `internal/provision/resolver.go` -- topological sort (Kahn's algorithm) for dependency ordering, circular dependency detection
  - Create: `internal/provision/state.go` -- `ProvisionState` persistence to `$SD_HOME/vms/<name>/provision-state.yaml`, per-module status tracking (pending/running/completed/failed) for `sd status` to read (REQ-006-009)
- **Key details:** `base` module always runs first (REQ-006-002). Circular deps detected before any execution. `ProvisionState` written to disk after each module completes so `sd status` can read provisioning progress independently.
- **Acceptance criteria:**
  - [ ] Module YAML files parse correctly
  - [ ] Topological sort produces correct ordering
  - [ ] Circular dependency A->B->A is caught at validation time
  - [ ] `base` always runs first regardless of module selection
  - [ ] Custom modules from `~/.sd/provisions/` are discovered
  - [ ] Name conflict between custom and built-in produces fatal error
  - [ ] `ProvisionState` written to disk, readable by `sd status`
  - [ ] Tests: YAML parsing, topo sort, circular detection, state persistence
- **Dependencies:** 1.3 (for types)

**5.5b Implement provisioning executor and probes**
- **What:** Script execution via backend.Exec(), readiness probe polling.
- **Files:**
  - Create: `internal/provision/provision.go` -- `Engine` struct implementing `Provisioner` interface, `Provision()`, `ListModules()`, `GetModule()`, `ValidateModules()`
  - Create: `internal/provision/executor.go` -- execute scripts via `backend.Exec()`, prepend `set -eux -o pipefail`, handle system/user modes, update `ProvisionState` after each module
  - Create: `internal/provision/probe.go` -- readiness probe polling with interval and timeout
- **Key details:** Scripts executed via `backend.Exec()` (not direct SSH -- enables future backends). `set -eux -o pipefail` prepended to every script. System scripts run as root, user scripts as default user. Checksum verification: download to temp, verify SHA-256, then install (REQ-006-016). No `curl | sh` patterns.
- **Acceptance criteria:**
  - [ ] `set -eux -o pipefail` is prepended to all scripts
  - [ ] Readiness probes poll at configured interval
  - [ ] Probe timeout produces actionable error
  - [ ] `ProvisionState` updated after each module execution
  - [ ] Tests: executor, probe timing, state updates
- **Dependencies:** 5.5a, 2.1 (for Exec)

**5.5c Implement built-in provisioning modules**
- **What:** YAML module definitions for all built-in modules.
- **Files:**
  - Create: `internal/provision/modules/base.yaml` -- git, curl, build-essential, ca-certificates, jq, tmux, vim. Also: sshd config drop-in at `/etc/ssh/sshd_config.d/sd-security.conf` with `AcceptEnv SD_* ANTHROPIC_* GITHUB_* GH_*` (REQ-004-011, REQ-007-019), `AllowTcpForwarding local`, `GatewayPorts no`, `PermitTunnel no`, `X11Forwarding no` (REQ-004-026), then reload sshd.
  - Create: `internal/provision/modules/claude-code.yaml` -- Node.js via nvm (pin version + checksum), Claude Code CLI via npm. Git credential helper: write custom credential helper script that reads from `$GITHUB_TOKEN` env var, configure `git config --global credential.helper ""` then `git config --global credential.helper /usr/local/bin/sd-git-credential-helper`, configure `user.name`/`user.email` from host git config, verify `~/.git-credentials` does not exist (REQ-006-012, REQ-004-030). Detect and preserve `CLAUDE.md`/`AGENTS.md` in cloned repos (REQ-006-013).
  - Create: `internal/provision/modules/docker.yaml` -- Docker Engine rootless (requires newuidmap, kernel namespaces, loginctl configuration). Pin version + checksum. Probe: `docker info`.
  - Create: `internal/provision/modules/golang.yaml` -- Go toolchain. Pin version (e.g., 1.22.x) + SHA-256 checksum. Install to `/usr/local/go`, add to PATH. Probe: `go version`.
  - Create: `internal/provision/modules/rust.yaml` -- Rust via rustup. Pin rustup-init checksum. User mode (not root). Probe: `rustc --version`.
  - Create: `internal/provision/modules/python.yaml` -- Python 3 with pip and venv. System package install. Probe: `python3 --version`.
  - Create: `internal/provision/modules/github-cli.yaml` -- GitHub CLI from official apt repo. Pin version. Probe: `gh --version`.
- **Key details:** Each module specifies: version pins, SHA-256 checksums for downloads, system vs user mode, probe command, and dependencies. `base` module includes critical sshd security configuration.
- **Acceptance criteria:**
  - [ ] All module YAML files parse and validate
  - [ ] `base.yaml` configures sshd AcceptEnv and port forwarding restrictions
  - [ ] `claude-code.yaml` sets up git credential helper from env var (REQ-006-012)
  - [ ] `claude-code.yaml` clears pre-existing credential helpers and verifies no ~/.git-credentials
  - [ ] All download modules have SHA-256 checksums
  - [ ] Each module has a readiness probe command
  - [ ] Tests: YAML parsing, module validation
- **Dependencies:** 5.5a

**5.6 Implement `sd version` command**
- **What:** Version information output.
- **Files:**
  - Create: `internal/cmd/version.go` -- prints version, commit, build date, Go version. Supports `--json`.
- **Key details:** Version, Commit, BuildDate injected via ldflags at build time. Works without config file (PersistentPreRunE should not fail on missing config for this command).
- **Acceptance criteria:**
  - [ ] `sd version` prints version, commit, date, Go version
  - [ ] `sd version --json` outputs JSON object
  - [ ] Works without config file
- **Dependencies:** 5.1

#### Phase 5 Exit Criteria
- [ ] `sd create` / `sd destroy` / `sd start` / `sd stop` / `sd list` / `sd status` / `sd version` all work
- [ ] Provisioning engine handles module dependencies, script execution, and readiness probes
- [ ] All built-in module YAML files are embedded and parseable
- [ ] Root command wires all dependencies correctly

---

### Phase 6: Connection & Interaction Commands

**Objective:** Implement connect, exec, sync, and ssh-config commands.
**Prerequisites:** Phase 4 (SSH infrastructure), Phase 5 (CLI framework, provisioning for tmux setup)

#### Tasks

**6.1 Implement connection manager**
- **What:** `Connector` interface implementation for interactive SSH + tmux sessions.
- **Files:**
  - Create: `internal/connection/connect.go` -- `Connector` interface implementation, `Connect()` method. Builds SSH command with correct key, host, port, transport, SendEnv args, port forwarding (-L) args. Handles tmux attach/create logic.
  - Create: `internal/connection/tmux.go` -- tmux session management: `tmux new-session -A -s <name>`, new window creation, session name validation (alphanumeric, hyphens, underscores only)
- **Key details:** Connect flow: resolve VM -> check status -> auto-start if stopped (unless --no-start) -> resolve env vars -> build SSH command -> if no-tmux: direct SSH; else: SSH + `tmux new-session -A -s sd-<name>`. For --new-window: `tmux new-window -t <session>` then attach. For concurrent connections (REQ-007-020): if session `sd-<name>` already exists, create a new window with `tmux new-window -t sd-<name>` and attach, so each concurrent `sd connect` gets its own tmux window. Port forwarding: `-L 127.0.0.1:<host>:<guest>` for each forward. SSH command builds connection args directly from managed state (identity file, host, port, user, SendEnv) rather than using `-F /dev/null` — for VSOCK transport, the ProxyCommand (`limactl ssh --stdio sd-<name>`) is passed as `-o ProxyCommand=...` on the command line. This avoids the conflict where `-F /dev/null` would discard the VSOCK ProxyCommand from the config fragment.
- **Acceptance criteria:**
  - [ ] `Connect()` establishes SSH + tmux session
  - [ ] Auto-start works for stopped VMs
  - [ ] `--no-start` returns error for stopped VM
  - [ ] Port forwarding adds correct `-L` arguments
  - [ ] `--no-tmux` bypasses tmux
  - [ ] `--new-window` creates new tmux window
  - [ ] `--no-tmux` + `--new-window` is rejected (code 2)
  - [ ] Concurrent connections each get separate tmux windows (REQ-007-020)
  - [ ] VSOCK transport uses `-o ProxyCommand=...` on command line (not from config fragment)
  - [ ] Tests: SSH command construction, tmux args, mutual exclusivity, concurrent windows
- **Dependencies:** 4.1, 4.2, 4.3

**6.2 Implement `sd connect` CLI command**
- **What:** CLI wiring for the connect command.
- **Files:**
  - Create: `internal/cmd/connect.go` -- alias `c`, flags: `--session`, `--new-window`, `--no-tmux`, `--forward`, `--no-start`. Resolves VM name, calls Connector.Connect().
- **Key details:** Credential injection: `app.Creds.InjectEnv(name)` -> map of resolved env vars. Failures are warnings, not fatal (REQ-001-007). After session ends: log duration, audit event.
- **Acceptance criteria:**
  - [ ] `sd connect myvm` and `sd c myvm` both work
  - [ ] `--json` outputs connection metadata without opening session
  - [ ] Credential injection failure is a warning, not fatal
  - [ ] Tests: flag parsing, credential injection error handling
- **Dependencies:** 5.1, 6.1

**6.3 Implement `sd exec` CLI command**
- **What:** Non-interactive remote command execution.
- **Files:**
  - Create: `internal/cmd/exec.go` -- `--` separator, pass-through stdout/stderr, match remote exit code, `--json` wraps in ExecResult
- **Key details:** Does NOT auto-start stopped VM (returns error). Resolves env vars and passes via SendEnv. `--json` captures stdout/stderr and wraps in `{"exit_code": N, "stdout": "...", "stderr": "..."}`. Without `--json`, stdout/stderr stream through directly.
- **Acceptance criteria:**
  - [ ] `sd exec myvm -- ls -la` passes through output
  - [ ] Exit code matches remote command
  - [ ] Stopped VM returns error (no auto-start)
  - [ ] `--json` wraps output correctly
  - [ ] Tests: exit code propagation, JSON output structure
- **Dependencies:** 5.1, 6.1

**6.4 Implement file sync**
- **What:** rsync-based file synchronization.
- **Files:**
  - Create: `internal/connection/sync.go` -- `FileSyncer` interface implementation: `SyncTo()`, `SyncFrom()`, `Diff()`, `Watch()`. Uses rsync over SSH with managed identity.
  - Create: `internal/cmd/sync.go` -- `sd sync to` and `sd sync from` subcommands, `--diff`, `--watch`
- **Key details:** rsync command: `rsync -avz -e "ssh -i <key> -p <port> -o StrictHostKeyChecking=yes -o UserKnownHostsFile=<knownhosts>" <src> <user>@<host>:<dst>`. `--watch` uses `fsnotify` for file system monitoring (host side only, SyncTo direction only). `--diff` uses `rsync --dry-run --itemize-changes`. Default dst: `~/<basename>` for SyncTo, `./<basename>` for SyncFrom.
- **Acceptance criteria:**
  - [ ] `sd sync to myvm ./src /home/user/src` copies files
  - [ ] `sd sync from myvm /path ./local` copies back
  - [ ] `--diff` shows differences without copying
  - [ ] `--watch` is SyncTo only; using with SyncFrom exits code 2
  - [ ] Tests: rsync command construction, direction validation
- **Dependencies:** 4.2, 5.1

**6.5 Implement `sd ssh-config` command**
- **What:** Print SSH config fragment for a VM.
- **Files:**
  - Create: `internal/cmd/ssh_config.go` -- prints fragment to stdout, supports `--json` for individual fields
- **Acceptance criteria:**
  - [ ] Prints valid SSH config fragment to stdout
  - [ ] `--json` outputs fields as JSON object
  - [ ] Nonexistent VM exits code 1
- **Dependencies:** 5.1, 4.2

#### Phase 6 Exit Criteria
- [ ] `sd connect` opens SSH + tmux sessions with env injection
- [ ] `sd exec` runs commands non-interactively with correct exit codes
- [ ] `sd sync to/from` transfers files via rsync
- [ ] `sd ssh-config` prints valid fragments

---

### Phase 7: Security Operations & Remaining Commands

**Objective:** Implement egress enforcement, audit logging, credential management, and all remaining CLI commands.
**Prerequisites:** Phase 5-6 -- core commands working, provisioning engine

#### Tasks

**7.0 Spike: Validate iptables/dnsmasq inside Lima VZ**
- **What:** Verify that iptables and dnsmasq work inside a Lima VM with VZ userspace networking before building the full egress system.
- **Key details:** Create a minimal Lima VM with VZ backend, install iptables and dnsmasq, verify: (a) iptables OUTPUT chain rules take effect, (b) dnsmasq can bind to 127.0.0.1:53, (c) DNS queries route through dnsmasq, (d) blocked domains actually fail to resolve. If VZ userspace stack doesn't support iptables, document the fallback approach.
- **Acceptance criteria:**
  - [ ] Spike results documented (works / doesn't work / partial)
  - [ ] If iptables doesn't work: fallback approach identified
- **Dependencies:** 3.2

**7.1 Implement egress control**
- **What:** Egress rule generation (pure functions) and dnsmasq/iptables script generation. The `security/` package generates scripts but does NOT execute them — execution is done by the CLI layer via `backend.Exec()`.
- **Files:**
  - Create: `internal/security/egress.go` -- `EgressController` interface: `GenerateAllowlistScripts(ctx context.Context, domains []EgressDomain) []backend.ProvisionScript`, `GenerateAddDomainScript(ctx context.Context, domain string) backend.ProvisionScript`, `GenerateRemoveDomainScript(ctx context.Context, domain string) backend.ProvisionScript`, `ListDomains(vmName string) ([]EgressDomain, error)`. Pure functions that return `ProvisionScript` structs; caller executes via `backend.Exec()`.
- **Key details:** Architecture: `security/egress.go` generates script content as pure functions returning `backend.ProvisionScript`. The CLI layer (e.g., `config_egress.go`) calls `backend.Exec()` to run the generated scripts inside the VM. This keeps `security/` free of the `backend` import. iptables: OUTPUT chain default DROP for new outbound. DNS to 127.0.0.1 only. Block DoH to known providers (8.8.8.8, 1.1.1.1, 9.9.9.9) on port 443. Block DoT (port 853). SSH from host always allowed. dnsmasq: bound to 127.0.0.1:53, forward only allowlisted domains, NXDOMAIN for everything else. Wildcard `*.example.com` matches one subdomain level only (REQ-004-007). Domain re-resolution at configurable interval (default 5 min, REQ-004-010). Installation path: egress scripts are applied during `sd create` as a provisioning step (after base module, as a `security-egress` internal module). dnsmasq runs as a systemd service (`sd-dnsmasq.service`). `AddDomain` post-creation: generates an iptables append + dnsmasq config update script, executed via `backend.Exec()` — works without VM restart. All `EgressController` methods take `context.Context` for cancellability.
- **Acceptance criteria:**
  - [ ] Default policy is DROP for outbound
  - [ ] Allowlisted domains are resolvable and reachable
  - [ ] `*.githubusercontent.com` matches `raw.githubusercontent.com` but not `a.b.githubusercontent.com`
  - [ ] DNS queries for non-allowlisted domains return NXDOMAIN
  - [ ] `AddDomain` works without VM restart (live iptables + dnsmasq update)
  - [ ] `security/egress.go` does NOT import `backend/` — returns `ProvisionScript` for caller to execute
  - [ ] dnsmasq installed as systemd service
  - [ ] Tests: wildcard matching, iptables rule generation, dnsmasq config generation (all pure function tests)
- **Dependencies:** 7.0 (spike), 2.3, 5.5b (provisioning executor)

**7.2 Implement audit logging**
- **What:** Command and event logging with SHA-256 hash chain.
- **Files:**
  - Create: `internal/security/audit.go` -- `AuditLogger` interface implementation: `LogCommand()`, `LogEvent()`, `Query()`, `VerifyChain()`. Append-only log at `$SD_HOME/audit.log`. Each entry has `prev_hash` (SHA-256 of previous line). Genesis entry uses all-zeros hash.
- **Key details:** JSON lines format (one JSON object per line). `LogCommand` records: timestamp, command, args (secrets redacted), exit code, duration. `LogEvent` records: timestamp, event_type, vm_name, metadata. `Query` supports filtering by VM name, since/until timestamps. `VerifyChain` validates hash chain integrity and returns first `ChainBreak` if found. Secrets in args replaced with `[REDACTED]`.
- **Acceptance criteria:**
  - [ ] Every command invocation logged
  - [ ] Secrets are redacted in log entries
  - [ ] Hash chain: entry N's prev_hash = SHA-256(entry N-1 line)
  - [ ] `VerifyChain()` detects tampering
  - [ ] `Query()` filters by VM and time range
  - [ ] Tests: hash chain integrity, secret redaction, query filtering
- **Dependencies:** None (stdlib)

**7.3 Implement credential management**
- **What:** `CredentialInjector` for runtime credential injection.
- **Files:**
  - Create: `internal/security/credentials.go` -- `CredentialInjector` interface implementation: `InjectEnv()`, `Rotate()`, `Revoke()`, `List()`. Resolves `${VAR}` references from VM config, returns resolved map. Never writes to files.
- **Key details:** `InjectEnv()` reads VM's `env` config, resolves each `${VAR}` from host env, returns map. `Rotate()` updates which host environment variable name a `${VAR}` reference points to (e.g., `${GITHUB_TOKEN}` -> `${GITHUB_TOKEN_V2}`), NOT the token value itself — `sd` never stores or handles literal token values, consistent with the credential-never-on-disk security model. `Revoke()` removes all env entries from VM config. `List()` returns credential types without values. `ValidateToken()` for classic PAT detection.
- **Acceptance criteria:**
  - [ ] `InjectEnv()` resolves all `${VAR}` references
  - [ ] No credential values written to any file
  - [ ] `Revoke()` ensures no credentials injected on next connect
  - [ ] `List()` shows types without values
  - [ ] Tests: resolution, revoke, list
- **Dependencies:** 1.3, 2.2

**7.4 Implement configuration CLI commands**
- **What:** `sd config set/get/list/edit/validate/egress` commands.
- **Files:**
  - Create: `internal/cmd/config.go` -- `sd config` parent command, `set`, `get`, `list`, `edit`, `validate` subcommands
  - Create: `internal/cmd/config_egress.go` -- `sd config egress add/remove/list` subcommands
- **Key details:** `set` writes to user config by default, `--project` for project config. `get` prints value to stdout (no decoration) for scripting. `list` tabular with KEY/VALUE/SOURCE columns. `edit` opens `$EDITOR` (or `$VISUAL`, or `vi`), errors if `SD_JSON=true`. `validate` checks all config files. Egress commands call `EgressController` methods. `config get` shows `${VAR}` references, not resolved values.
- **Acceptance criteria:**
  - [ ] `sd config set defaults.cpus 8` persists
  - [ ] `sd config get defaults.cpus` prints `8` to stdout
  - [ ] `sd config list` shows all keys with sources
  - [ ] `sd config validate` checks all files
  - [ ] `sd config egress add/remove/list` work
  - [ ] All support `--json`
- **Dependencies:** 5.1, 1.4

**7.5 Implement security CLI commands**
- **What:** Token, audit, security status, and diff commands.
- **Files:**
  - Create: `internal/cmd/token.go` -- `sd token github setup`, `sd token rotate`, `sd token revoke`, `sd token list`
  - Create: `internal/cmd/audit.go` -- `sd audit [vm] --since --json`
  - Create: `internal/cmd/security.go` -- `sd security status <vm>`
  - Create: `internal/cmd/diff.go` -- `sd diff <vm>` detects CI/CD workflow changes
- **Key details:** `token github setup` outputs PAT creation guidance including recommended scopes for fine-grained tokens; if token resolves and starts with `ghp_` prefix, warn that classic PATs are not recommended (REQ-004-012) and show the recommended fine-grained token scopes. `--json` output includes `recommended_scopes` field. `token rotate` updates `${VAR}` reference. `token revoke` removes credentials. `audit` queries audit log with optional VM filter and time range. `security status` shows mounts, egress domains, credentials, snapshots, last audit event, warnings. `diff` detects changes to `.github/workflows/*`, `.gitlab-ci.yml`, `Jenkinsfile`, `.circleci/*`, `.git/hooks/*`.
- **Acceptance criteria:**
  - [ ] `sd token github setup` outputs guidance
  - [ ] `sd audit myvm --since <ts> --json` filters and outputs JSON
  - [ ] `sd security status myvm` shows posture summary
  - [ ] `sd diff myvm` flags CI/CD changes
  - [ ] All support `--json`
- **Dependencies:** 5.1, 7.2, 7.3

**7.6 Implement remaining CLI commands**
- **What:** snapshot, provision, doctor, logs, completion commands.
- **Files:**
  - Create: `internal/cmd/snapshot.go` -- `sd snapshot create/list/restore/delete`
  - Create: `internal/cmd/provision.go` -- `sd provision [vm] --modules`, `sd provision list`
  - Create: `internal/cmd/doctor.go` -- checks limactl, ssh, tmux, rsync, config validity, SD_HOME writable, backend availability, network connectivity to default egress allowlist endpoints (REQ-002-007)
  - Create: `internal/cmd/logs.go` -- `sd logs [name] --tail --follow`. Log sources: (1) provisioning output from `$SD_HOME/vms/<name>/provision-state.yaml`, (2) sd audit log filtered by VM name, (3) VM journal via `limactl shell <name> -- journalctl --user -n <N>`. `--tail N` limits to last N entries. `--follow` streams VM journal via `journalctl --follow` exec'd through `backend.Exec()` (long-running, blocks until Ctrl-C).
  - Create: `internal/cmd/completion.go` -- bash/zsh/fish completion generation with custom completers for VM names, backends, snapshot tags, module names
- **Key details:** `doctor` runs checks with pass/fail indicators. Network connectivity check probes each of the 11 default egress allowlist endpoints with a TCP connect or HTTPS HEAD (REQ-002-007). `logs` aggregates three sources and presents chronologically. `provision list` shows all modules. `completion` generates shell scripts. Custom completions registered for VM names (from state.List), backend names (from backend.List), module names (from provisioner.ListModules).
- **Acceptance criteria:**
  - [ ] `sd snapshot create/list/restore/delete` work
  - [ ] `sd provision list` shows all modules
  - [ ] `sd doctor` checks all prerequisites including network connectivity to egress endpoints
  - [ ] `sd logs myvm --tail 20` shows last 20 log entries
  - [ ] `sd logs myvm --follow` streams journal output
  - [ ] `sd completion bash/zsh/fish` outputs valid scripts
  - [ ] All support `--json` where applicable
- **Dependencies:** 5.1, 5.5, 3.3

**7.7 Implement `sd reset` command**
- **What:** Reset a VM to a prior snapshot state (REQ-004-019).
- **Files:**
  - Create: `internal/cmd/reset.go` -- `sd reset <name> [--snapshot <tag>]`, auto-snapshots before reset (destructive operation per REQ-004-019), restores from specified snapshot or latest.
- **Key details:** Reset flow: State.Get -> Backend.Status -> auto-snapshot (same logic as destroy) -> SnapshotApply(tag) -> Backend.Start -> Audit.LogEvent. If no `--snapshot` specified, uses the most recent snapshot. `--force` skips auto-snapshot. `--no-snapshot` skips auto-snapshot. Both `--force` and `--no-snapshot` behave identically to `sd destroy` flags.
- **Acceptance criteria:**
  - [ ] `sd reset myvm` creates auto-snapshot then restores latest
  - [ ] `sd reset myvm --snapshot v1` restores specific snapshot
  - [ ] Auto-snapshot before reset (REQ-004-019)
  - [ ] `--json` outputs structured result
  - [ ] Tests: reset flow, snapshot selection
- **Dependencies:** 5.1, 3.3, 2.2

#### Phase 7 Exit Criteria
- [ ] All CLI commands from REQ-002-002 through REQ-002-019 are implemented
- [ ] Egress control generates iptables + dnsmasq config
- [ ] Audit logging with hash chain works
- [ ] Credential injection resolved without disk writes
- [ ] All commands support `--json`

---

### Phase 8: Integration, Testing & Polish

**Objective:** End-to-end verification, integration tests, script tests, and polish.
**Prerequisites:** Phase 7 -- all features implemented

#### Tasks

**8.1 Integration test suite**
- **What:** Real-system integration tests tagged `//go:build integration`.
- **Files:**
  - Create: `internal/backend/lima/lima_integration_test.go` -- full lifecycle test
  - Create: `internal/config/config_integration_test.go` -- real file loading
  - Create: `internal/state/state_integration_test.go` -- persistence round-trip
  - Create: `internal/provision/provision_integration_test.go` -- module execution
  - Create: `internal/connection/connect_integration_test.go` -- SSH key + config
- **Acceptance criteria:**
  - [ ] Integration tests pass with real limactl (where available)
  - [ ] Config round-trip with real files works
  - [ ] State persistence survives process restart

**8.2 Script test suite**
- **What:** CLI-level script tests using `rsc.io/script/scripttest`.
- **Files:**
  - Create: `testdata/scripts/create_json.txtar`
  - Create: `testdata/scripts/create_duplicate.txtar`
  - Create: `testdata/scripts/create_invalid_name.txtar`
  - Create: `testdata/scripts/destroy_force.txtar`
  - Create: `testdata/scripts/config_set_get.txtar`
  - Create: `testdata/scripts/version.txtar`
- **Acceptance criteria:**
  - [ ] Script tests cover core workflows
  - [ ] JSON output validated in script tests

**8.3 Command alias and completion verification**
- **What:** Verify all aliases (`c`, `ls`) and shell completions.
- **Acceptance criteria:**
  - [ ] `sd c myvm` = `sd connect myvm`
  - [ ] `sd ls` = `sd list`
  - [ ] Prefix matching: `sd cre` = `sd create`
  - [ ] Ambiguous prefix `sd co` lists matching commands
  - [ ] Completion scripts are syntactically valid

**8.4 Final build and polish**
- **What:** Ensure `go vet`, `golangci-lint`, `go mod tidy` all pass. Update go.sum. Verify all compile-time interface checks.
- **Acceptance criteria:**
  - [ ] `go vet ./...` clean
  - [ ] `golangci-lint run` clean
  - [ ] `go mod tidy` produces no changes
  - [ ] All interface compliance checks compile
  - [ ] `make build` produces a working binary

#### Phase 8 Exit Criteria
- [ ] All unit, integration, and script tests pass
- [ ] Linting clean
- [ ] Binary builds and runs correctly

---

## Cross-Cutting Concerns

### Error Handling

Three-layer error strategy from design-context.md:

**Layer 1 -- Sentinel errors** (package-level `var`, `errors.Is()` matching):
```go
// internal/backend/errors.go
var ErrVMNotFound = errors.New("vm not found")
```

**Layer 2 -- Structured errors** (user-facing with codes):
```go
// internal/ui/json.go
type SDError struct {
    Code    string         `json:"code"`
    Message string         `json:"message"`
    Details map[string]any `json:"details,omitempty"`
    cause   error
}
```

**Layer 3 -- Wrapped contextual errors:**
```go
return fmt.Errorf("lima create %q: %w", name, err)
```

Error propagation: backend returns sentinel -> CLI maps to SDError -> Printer formats for JSON or human output. All errors JSON-serializable. Exit codes: 0=success, 1=error, 2=usage.

### Testing Strategy

- **Framework:** `testing` + `testify/require` + `testify/assert`
- **Script tests:** `rsc.io/script/scripttest`
- **Unit tests:** Table-driven `[]struct{name, input, want}`, ~75%
- **Integration tests:** `//go:build integration`, ~20%
- **E2E tests:** `//go:build e2e`, ~5%
- **Fakes:** Hand-written for `Backend`, `state.Manager`, `provision.Provisioner`, `security.AuditLogger`
- **Build tags:** no tag=unit, `integration`=require real files, `e2e`=require limactl+network

### Configuration

Viper-based with 5-level precedence: CLI > env (SD_*) > project (.sd/config.yaml) > user ($SD_HOME/config.yaml) > built-in defaults. Instance-based Viper (not global). `${VAR}` references resolved lazily at use-time. Security keys ignored in project config.

### Build System

```makefile
build:       go build -o sd ./cmd/sd/
test:        go test ./...
test-int:    go test -tags integration ./...
test-e2e:    go test -tags e2e ./...
vet:         go vet ./...
lint:        golangci-lint run
install:     go install ./cmd/sd/
```

Release builds inject version/commit/date via ldflags.

---

## Technical Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Lima YAML format changes between versions | M | H | Pin minimum limactl version; test YAML generation against known-good output |
| APFS clone (`cp -c`) not available on non-macOS | L | M | Snapshot support gracefully degrades; fallback to regular `cp` with warning |
| iptables/dnsmasq interaction with Lima networking (VZ userspace stack) | H | H | Spike task in Phase 7 to validate egress approach; have fallback to Lima's built-in network filtering if iptables doesn't work |
| VSOCK availability varies by Lima version | M | M | Detect at runtime; transparent fallback to TCP SSH |
| `golang.org/x/crypto/ssh` API changes | L | L | Pin dependency version |
| Module path/org name changes | L | M | Defined as `github.com/gberns/sd` based on git remote; update if repo moves |

---

## Spec Coverage Matrix

| Spec File | Requirement | Plan Phase | Task |
|-----------|-------------|------------|------|
| 001-architecture.md | REQ-001-001: System Components | P1, P2 | 1.2-1.4, 2.1-2.3 |
| 001-architecture.md | REQ-001-002: Component Relationships | P2, P5 | 2.1, 5.1 |
| 001-architecture.md | REQ-001-003: Pluggable Backend Architecture | P2 | 2.1, 2.4 |
| 001-architecture.md | REQ-001-004: Lima as Default Backend | P3 | 3.1, 3.2 |
| 001-architecture.md | REQ-001-005: Future Backends | P2 | 2.4 |
| 001-architecture.md | REQ-001-006: Data Flow -- sd create | P5 | 5.2 |
| 001-architecture.md | REQ-001-007: Data Flow -- sd connect | P6 | 6.1, 6.2 |
| 001-architecture.md | REQ-001-008: Data Flow -- sd destroy | P5 | 5.3 |
| 001-architecture.md | REQ-001-009: Config System Architecture | P1 | 1.3, 1.4 |
| 001-architecture.md | REQ-001-010: CLI Framework | P1, P5 | 1.2, 5.1 |
| 001-architecture.md | REQ-001-011: Named VM Model | P2 | 2.2 |
| 001-architecture.md | REQ-001-012: Layered Security Model | P2, P7 | 2.3, 7.1-7.3 |
| 001-architecture.md | REQ-001-013: Guest VM Environment | P5 | 5.5 |
| 001-architecture.md | REQ-001-014: Package Layout | P1-P7 | All tasks |
| 002-cli.md | REQ-002-001: Command Entry Point | P1, P5 | 1.1, 5.1 |
| 002-cli.md | REQ-002-002: Command Groups | P5 | 5.1 |
| 002-cli.md | REQ-002-003: VM Management Commands | P5 | 5.2-5.4 |
| 002-cli.md | REQ-002-004: Connection Commands | P6 | 6.2-6.5 |
| 002-cli.md | REQ-002-005: Configuration Commands | P7 | 7.4 |
| 002-cli.md | REQ-002-006: Provisioning Commands | P7 | 7.6 |
| 002-cli.md | REQ-002-007: Diagnostic Commands | P5, P7 | 5.6, 7.6 |
| 002-cli.md | REQ-002-008: Security Commands | P7 | 7.5 |
| 002-cli.md | REQ-002-009: Command Aliases | P5, P6 | 5.4, 6.2 |
| 002-cli.md | REQ-002-010: Global Flags | P5 | 5.1 |
| 002-cli.md | REQ-002-011: Output Format -- Human | P1 | 1.2 |
| 002-cli.md | REQ-002-012: Output Format -- JSON | P1 | 1.2 |
| 002-cli.md | REQ-002-013: Exit Codes | P5 | 5.1 |
| 002-cli.md | REQ-002-014: Prefix Matching | P5 | 5.1 |
| 002-cli.md | REQ-002-015: PersistentPreRun | P5 | 5.1 |
| 002-cli.md | REQ-002-016: File Organization | P5-P7 | All cmd tasks |
| 002-cli.md | REQ-002-017: Shell Completions | P7 | 7.6 |
| 002-cli.md | REQ-002-018: Error Serialization | P1 | 1.2 |
| 002-cli.md | REQ-002-019: Default VM Resolution | P5 | 5.1 |
| 003-vm-backend.md | REQ-003-001: Backend Interface | P2 | 2.1 |
| 003-vm-backend.md | REQ-003-002: Backend Availability | P3 | 3.2 |
| 003-vm-backend.md | REQ-003-003: VM Lifecycle Ops | P3 | 3.2 |
| 003-vm-backend.md | REQ-003-004: VM Status Reporting | P3 | 3.2 |
| 003-vm-backend.md | REQ-003-005: VM Listing | P3 | 3.2 |
| 003-vm-backend.md | REQ-003-006: SSH Configuration | P3 | 3.2 |
| 003-vm-backend.md | REQ-003-007: Command Execution | P3 | 3.2 |
| 003-vm-backend.md | REQ-003-008: Optional Snapshotter | P2, P3 | 2.1, 3.3 |
| 003-vm-backend.md | REQ-003-009: Optional Cloner | P2 | 2.1 |
| 003-vm-backend.md | REQ-003-010: Optional Syncer | P2 | 2.1 |
| 003-vm-backend.md | REQ-003-011: VMConfig Struct | P2 | 2.1 |
| 003-vm-backend.md | REQ-003-012: Network Mode | P2 | 2.1 |
| 003-vm-backend.md | REQ-003-013: Backend Registry | P2 | 2.1 |
| 003-vm-backend.md | REQ-003-014: Lima Backend | P3 | 3.2 |
| 003-vm-backend.md | REQ-003-015: Lima YAML Generation | P3 | 3.1 |
| 003-vm-backend.md | REQ-003-016: Lima VZ Defaults | P3 | 3.1 |
| 003-vm-backend.md | REQ-003-017: Lima Mount Policy | P3 | 3.1 |
| 003-vm-backend.md | REQ-003-018: Lima Snapshot | P3 | 3.3 |
| 003-vm-backend.md | REQ-003-019: Lima Provisioning | P3 | 3.2 |
| 003-vm-backend.md | REQ-003-020: Future Backend Stubs | P2 | 2.4 |
| 003-vm-backend.md | REQ-003-021: Backend Error Semantics | P2 | 2.1 |
| 003-vm-backend.md | REQ-003-022: Context Cancellation | P3 | 3.2 |
| 003-vm-backend.md | REQ-003-023: Credential Isolation | P3 | 3.1 |
| 003-vm-backend.md | REQ-003-024: Base Image Resolution | P3 | 3.1 |
| 004-security.md | REQ-004-001: Threat Model | P5, P7 | 5.2, 7.5 |
| 004-security.md | REQ-004-002: VM Isolation | P3 | 3.2 |
| 004-security.md | REQ-004-003: Default No Mounts | P3 | 3.1 |
| 004-security.md | REQ-004-004: Optional Mounts | P5 | 5.2 |
| 004-security.md | REQ-004-005: Mount Path Validation | P2 | 2.3 |
| 004-security.md | REQ-004-006: Default-Deny Egress | P7 | 7.1 |
| 004-security.md | REQ-004-007: Default Egress Allowlist | P1, P7 | 1.3, 7.1 |
| 004-security.md | REQ-004-008: User Egress Config | P7 | 7.1, 7.4 |
| 004-security.md | REQ-004-009: Egress via iptables | P7 | 7.1 |
| 004-security.md | REQ-004-010: DNS-Based Allowlisting | P7 | 7.1 |
| 004-security.md | REQ-004-011: Credential Injection | P4, P6 | 4.3, 6.1 |
| 004-security.md | REQ-004-012: GitHub Token Scoping | P7 | 7.5 |
| 004-security.md | REQ-004-013: Anthropic API Key | P6 | 6.2 |
| 004-security.md | REQ-004-014: Per-VM SSH Keys | P4 | 4.1 |
| 004-security.md | REQ-004-015: Token Rotation/Revocation | P7 | 7.3, 7.5 |
| 004-security.md | REQ-004-016: GitHub Bot Support | P7 | 7.5 |
| 004-security.md | REQ-004-017: Protected Branch Integration | P7 | 7.5 |
| 004-security.md | REQ-004-018: CI Workflow Detection | P7 | 7.5 |
| 004-security.md | REQ-004-019: Snapshot Before Destructive | P5, P7 | 5.3, 7.7 |
| 004-security.md | REQ-004-020: Manual Snapshots | P7 | 7.6 |
| 004-security.md | REQ-004-021: Audit Command Logging | P7 | 7.2 |
| 004-security.md | REQ-004-022: Audit VM Events | P7 | 7.2 |
| 004-security.md | REQ-004-023: Sensitive Dir Configurability | P2 | 2.3 |
| 004-security.md | REQ-004-024: Security Posture Summary | P7 | 7.5 |
| 004-security.md | REQ-004-025: Local Filtering DNS | P7 | 7.1 |
| 004-security.md | REQ-004-026: SSH Port Forwarding Restrictions | P5 | 5.5 |
| 004-security.md | REQ-004-027: SSH Agent/X11 Disabled | P4 | 4.2 |
| 004-security.md | REQ-004-028: Download Integrity | P5 | 5.5 |
| 004-security.md | REQ-004-029: Project Config Security | P1 | 1.4 |
| 004-security.md | REQ-004-030: Git Credential Cache Prevention | P5 | 5.5 |
| 004-security.md | REQ-004-031: SSH Host Key Verification | P4 | 4.2 |
| 005-configuration.md | REQ-005-001: Precedence Order | P1 | 1.4 |
| 005-configuration.md | REQ-005-002: User-Level Config | P1 | 1.4 |
| 005-configuration.md | REQ-005-003: Project-Level Config | P1 | 1.4 |
| 005-configuration.md | REQ-005-004: Built-In Defaults | P1 | 1.3 |
| 005-configuration.md | REQ-005-005: Env Var Mapping | P1 | 1.4 |
| 005-configuration.md | REQ-005-006: Config File Format | P1 | 1.3, 1.4 |
| 005-configuration.md | REQ-005-007: VM Definition Files | P2 | 2.2 |
| 005-configuration.md | REQ-005-008: Sensitive Value Handling | P1 | 1.3 |
| 005-configuration.md | REQ-005-009: Config Get | P7 | 7.4 |
| 005-configuration.md | REQ-005-010: Config Set | P7 | 7.4 |
| 005-configuration.md | REQ-005-011: Config List | P7 | 7.4 |
| 005-configuration.md | REQ-005-012: Config Edit | P7 | 7.4 |
| 005-configuration.md | REQ-005-013: Config Validate | P7 | 7.4 |
| 005-configuration.md | REQ-005-014: Viper Integration | P1 | 1.4 |
| 005-configuration.md | REQ-005-015: VM Config Inheritance | P1 | 1.4 |
| 005-configuration.md | REQ-005-016: Config Dir Structure | P2 | 2.2 |
| 005-configuration.md | REQ-005-017: Security Keys in Project Config | P1 | 1.4 |
| 005-configuration.md | REQ-005-018: Mount Policy Values | P5 | 5.2 |
| 006-provisioning.md | REQ-006-001: Built-in Module Set | P5 | 5.5 |
| 006-provisioning.md | REQ-006-002: Base Module Always Runs | P5 | 5.5 |
| 006-provisioning.md | REQ-006-003: Module Definition Format | P5 | 5.5 |
| 006-provisioning.md | REQ-006-004: Module Dependency Ordering | P5 | 5.5 |
| 006-provisioning.md | REQ-006-005: Script Execution Environment | P5 | 5.5 |
| 006-provisioning.md | REQ-006-006: Script Idempotency | P5 | 5.5 |
| 006-provisioning.md | REQ-006-007: Custom Module Support | P5 | 5.5 |
| 006-provisioning.md | REQ-006-008: Readiness Probes | P5 | 5.5 |
| 006-provisioning.md | REQ-006-009: Provisioning Progress | P5 | 5.4 |
| 006-provisioning.md | REQ-006-010: Re-provisioning | P7 | 7.6 |
| 006-provisioning.md | REQ-006-011: Claude Code Provisioning | P5 | 5.5 |
| 006-provisioning.md | REQ-006-012: Git Configuration | P5 | 5.5 |
| 006-provisioning.md | REQ-006-013: CLAUDE.md Propagation | P5 | 5.5 |
| 006-provisioning.md | REQ-006-014: Embedded Module Storage | P5 | 5.5 |
| 006-provisioning.md | REQ-006-015: Module Selection | P5, P7 | 5.2, 7.6 |
| 006-provisioning.md | REQ-006-016: Checksum Verification | P5 | 5.5 |
| 007-connection.md | REQ-007-001: Connect Command | P6 | 6.2 |
| 007-connection.md | REQ-007-002: Auto-Start on Connect | P6 | 6.1 |
| 007-connection.md | REQ-007-003: SSH Key Generation | P4 | 4.1 |
| 007-connection.md | REQ-007-004: SSH Config Management | P4 | 4.2 |
| 007-connection.md | REQ-007-005: VSOCK Transport | P4 | 4.2 |
| 007-connection.md | REQ-007-006: SSH Config Print | P6 | 6.5 |
| 007-connection.md | REQ-007-007: Port Forwarding | P6 | 6.1, 6.2 |
| 007-connection.md | REQ-007-008: tmux Default Session | P6 | 6.1 |
| 007-connection.md | REQ-007-009: Named tmux Sessions | P6 | 6.1 |
| 007-connection.md | REQ-007-010: New tmux Window | P6 | 6.1 |
| 007-connection.md | REQ-007-011: tmux Default Config | P5 | 5.5 |
| 007-connection.md | REQ-007-012: Raw SSH Without tmux | P6 | 6.1 |
| 007-connection.md | REQ-007-013: Exec Command | P6 | 6.3 |
| 007-connection.md | REQ-007-014: Exec JSON Output | P6 | 6.3 |
| 007-connection.md | REQ-007-015: Sync To VM | P6 | 6.4 |
| 007-connection.md | REQ-007-016: Sync From VM | P6 | 6.4 |
| 007-connection.md | REQ-007-017: Sync Diff Preview | P6 | 6.4 |
| 007-connection.md | REQ-007-018: Continuous Sync Watch | P6 | 6.4 |
| 007-connection.md | REQ-007-019: Environment Injection | P4, P6 | 4.3, 6.1 |
| 007-connection.md | REQ-007-020: Multiple Concurrent Connections | P6 | 6.1 |
| 007-connection.md | REQ-007-021: Connection Health/Errors | P6 | 6.1 |

---

## Appendix: File Manifest

### Directory Tree

```
cmd/
  sd/
    main.go

internal/
  cmd/
    root.go
    create.go
    destroy.go
    start.go
    stop.go
    list.go
    status.go
    connect.go
    exec.go
    sync.go
    ssh_config.go
    snapshot.go
    config.go
    config_egress.go
    provision.go
    token.go
    audit.go
    security.go
    diff.go
    doctor.go
    version.go
    logs.go
    reset.go
    completion.go

  backend/
    backend.go
    errors.go
    registry.go
    lima/
      lima.go
      yaml.go
      snapshot.go
      images.go
    avf/
      avf.go
    docker/
      docker.go
    incus/
      incus.go

  config/
    config.go
    defaults.go
    schema.go
    resolve.go

  state/
    state.go
    errors.go

  provision/
    provision.go
    module.go
    loader.go
    executor.go
    resolver.go
    probe.go
    state.go
    modules/
      base.yaml
      claude-code.yaml
      docker.yaml
      golang.yaml
      rust.yaml
      python.yaml
      github-cli.yaml

  connection/
    connect.go
    keys.go
    sshconfig.go
    tmux.go
    sync.go
    env.go

  security/
    mount.go
    types.go
    egress.go
    credentials.go
    audit.go
    noop.go

  ui/
    output.go
    json.go
    progress.go
    table.go

testdata/
  scripts/
    create_json.txtar
    create_duplicate.txtar
    create_invalid_name.txtar
    destroy_force.txtar
    config_set_get.txtar
    version.txtar

go.mod
go.sum
Makefile
```

### Files by Phase

| Path | Phase | Task | Purpose |
|------|-------|------|---------|
| `go.mod` | P1 | 1.1 | Go module definition |
| `cmd/sd/main.go` | P1 | 1.1 | Binary entry point |
| `Makefile` | P1 | 1.1 | Build targets |
| `internal/ui/output.go` | P1 | 1.2 | Printer interface and implementation |
| `internal/ui/json.go` | P1 | 1.2 | JSON output, SDError, error codes |
| `internal/ui/progress.go` | P1 | 1.2 | Progress indicators |
| `internal/ui/table.go` | P1 | 1.2 | Tabular output |
| `internal/config/schema.go` | P1 | 1.3 | Config type definitions |
| `internal/config/defaults.go` | P1 | 1.3 | Built-in default values |
| `internal/config/resolve.go` | P1 | 1.3 | ${VAR} reference resolution |
| `internal/config/config.go` | P1 | 1.4 | Viper-based config loader |
| `internal/backend/backend.go` | P2 | 2.1 | Backend interface and types |
| `internal/backend/errors.go` | P2 | 2.1 | Sentinel errors |
| `internal/backend/registry.go` | P2 | 2.1 | Backend registry |
| `internal/state/state.go` | P2 | 2.2 | VM state manager |
| `internal/state/errors.go` | P2 | 2.2 | State-layer errors |
| `internal/security/mount.go` | P2 | 2.3 | Mount path validation |
| `internal/security/types.go` | P2 | 2.3 | Security type definitions |
| `internal/backend/avf/avf.go` | P2 | 2.4 | AVF backend stub |
| `internal/backend/docker/docker.go` | P2 | 2.4 | Docker backend stub |
| `internal/backend/incus/incus.go` | P2 | 2.4 | Incus backend stub |
| `internal/backend/lima/yaml.go` | P3 | 3.1 | Lima YAML generation |
| `internal/backend/lima/images.go` | P3 | 3.1 | Base image mapping |
| `internal/backend/lima/lima.go` | P3 | 3.2 | Lima backend implementation |
| `internal/backend/lima/snapshot.go` | P3 | 3.3 | Lima snapshot support |
| `internal/connection/keys.go` | P4 | 4.1 | SSH key generation |
| `internal/connection/sshconfig.go` | P4 | 4.2 | SSH config management |
| `internal/connection/env.go` | P4 | 4.3 | Environment variable injection |
| `internal/cmd/root.go` | P5 | 5.1 | Root command, App struct |
| `internal/cmd/create.go` | P5 | 5.2 | sd create |
| `internal/cmd/destroy.go` | P5 | 5.3 | sd destroy |
| `internal/cmd/start.go` | P5 | 5.3 | sd start |
| `internal/cmd/stop.go` | P5 | 5.3 | sd stop |
| `internal/cmd/list.go` | P5 | 5.4 | sd list |
| `internal/cmd/status.go` | P5 | 5.4 | sd status |
| `internal/provision/module.go` | P5 | 5.5a | Module types |
| `internal/provision/loader.go` | P5 | 5.5a | Module loading |
| `internal/provision/resolver.go` | P5 | 5.5a | Dependency resolution |
| `internal/provision/state.go` | P5 | 5.5a | Provision state persistence |
| `internal/provision/provision.go` | P5 | 5.5b | Provisioner engine |
| `internal/provision/executor.go` | P5 | 5.5b | Script execution |
| `internal/provision/probe.go` | P5 | 5.5b | Readiness probes |
| `internal/provision/modules/*.yaml` | P5 | 5.5c | Built-in modules (7 files) |
| `internal/cmd/version.go` | P5 | 5.6 | sd version |
| `internal/connection/connect.go` | P6 | 6.1 | Connection manager |
| `internal/connection/tmux.go` | P6 | 6.1 | tmux session management |
| `internal/cmd/connect.go` | P6 | 6.2 | sd connect |
| `internal/cmd/exec.go` | P6 | 6.3 | sd exec |
| `internal/connection/sync.go` | P6 | 6.4 | File sync (rsync) |
| `internal/cmd/sync.go` | P6 | 6.4 | sd sync |
| `internal/cmd/ssh_config.go` | P6 | 6.5 | sd ssh-config |
| `internal/security/noop.go` | P5 | 5.1 | No-op stubs for Phase-7 interfaces |
| `internal/security/egress.go` | P7 | 7.1 | Egress control |
| `internal/security/audit.go` | P7 | 7.2 | Audit logging |
| `internal/security/credentials.go` | P7 | 7.3 | Credential management |
| `internal/cmd/config.go` | P7 | 7.4 | sd config |
| `internal/cmd/config_egress.go` | P7 | 7.4 | sd config egress |
| `internal/cmd/token.go` | P7 | 7.5 | sd token |
| `internal/cmd/audit.go` | P7 | 7.5 | sd audit |
| `internal/cmd/security.go` | P7 | 7.5 | sd security |
| `internal/cmd/diff.go` | P7 | 7.5 | sd diff |
| `internal/cmd/snapshot.go` | P7 | 7.6 | sd snapshot |
| `internal/cmd/provision.go` | P7 | 7.6 | sd provision |
| `internal/cmd/doctor.go` | P7 | 7.6 | sd doctor |
| `internal/cmd/logs.go` | P7 | 7.6 | sd logs |
| `internal/cmd/reset.go` | P7 | 7.7 | sd reset |
| `internal/cmd/completion.go` | P7 | 7.6 | sd completion |
| `testdata/scripts/*.txtar` | P8 | 8.2 | Script tests |
