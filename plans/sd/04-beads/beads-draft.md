# Beads Draft: sd

**Generated:** 2026-03-28
**Source:** plans/sd/03-plan/plan.md
**Plan review status:** Reviewed (all P0/P1 resolved, 14 P2s documented for implementer reference)

---

## Structure

### Feature Epic: sd

**Type:** epic
**Priority:** P1
**Description:** Go CLI tool that creates, configures, and manages secure VM environments for running AI coding agents with bypass permissions. Automates VM lifecycle, SSH configuration, tool provisioning, credential injection, and session management.

---

### Sub-Epic: Phase 1 — Project Scaffolding & Core Types

**Type:** epic
**Priority:** P1
**Parent:** Feature epic
**Description:** Establish the build system, module path, entry point, and the two zero-dependency foundation packages (`ui/` and `config/` types).

#### Issue: Initialize Go module and entry point (1.1)

**Type:** task
**Priority:** P1
**Parent:** Phase 1
**Dependencies:** None
**Description:**
Create `go.mod`, `cmd/sd/main.go`, and `Makefile`.

Files:
- Create: `go.mod` — module path `github.com/gberns/sd`, Go 1.22+
- Create: `cmd/sd/main.go` — calls `internal/cmd.Execute()`, blank import for Lima registration
- Create: `Makefile` — targets: build, test, test-int, test-e2e, vet, lint, install

Module path uses `github.com/gberns/sd` (confirmed from git remote). `main.go` is <10 lines. Makefile includes `go build -o sd ./cmd/sd/`, ldflags for version/commit/date injection.

**Acceptance Criteria:**
- [ ] `go build ./cmd/sd/` compiles (even if Execute() is a stub)
- [ ] `make build` produces the `sd` binary
- [ ] `make test` runs `go test ./...`

#### Issue: Implement `internal/ui/` package (1.2)

**Type:** task
**Priority:** P1
**Parent:** Phase 1
**Dependencies:** None
**Description:**
Output formatting layer with zero internal dependencies.

Files:
- Create: `internal/ui/output.go` — `Printer` interface, `NewPrinter()`, `Info/Warn/Error/Data/Fatal/Progress` methods
- Create: `internal/ui/json.go` — `JSONError`, `JSONErrorBody`, `SDError` struct with `Error()/Unwrap()`, error code constants (`ErrCodeVMNotFound`, etc.), error constructor helpers
- Create: `internal/ui/progress.go` — `ProgressReporter` interface, spinner for TTY, line-based for non-TTY, no-op for JSON mode
- Create: `internal/ui/table.go` — tabular output for list/status/config commands

`Printer` writes Info/Progress to stderr, Data to stdout. In JSON mode: `Data(v)` writes `{"ok": true, "data": v}` to stdout; errors write `{"ok": false, "error": {...}}`. `SDError` carries Code, Message, Details, and cause error. All error codes from REQ-001-010 and REQ-002-018.

**Acceptance Criteria:**
- [ ] `ui.NewPrinter(out, err, true)` produces JSON output only on stdout
- [ ] `ui.NewPrinter(out, err, false)` produces human-readable output
- [ ] `SDError` implements `error` and `Unwrap()`
- [ ] All error code constants defined: `ErrCodeVMNotFound`, `ErrCodeVMAlreadyExists`, `ErrCodeVMNotRunning`, `ErrCodeBackendNotAvailable`, `ErrCodeBackendNotFound`, `ErrCodeConfigInvalid`, `ErrCodeProvisionFailed`, `ErrCodeNotImplemented` (enumerate from specs/001-architecture.md REQ-001-010 and specs/002-cli.md REQ-002-018)
- [ ] Tests: JSON serialization, stream routing, progress modes

#### Issue: Implement `internal/config/` types and defaults (1.3)

**Type:** task
**Priority:** P1
**Parent:** Phase 1
**Dependencies:** None
**Description:**
Configuration value types, schema structs, and built-in defaults. Not the full Viper loader yet (that needs testing infrastructure).

Files:
- Create: `internal/config/schema.go` — `Config`, `Defaults`, `Security`, `VMDef`, `VMConfig`, `VMState` structs, `MountPolicy*` constants, `VMStatus*` constants, `ConfigSource*` constants, `ConfigEntry`, `ConfigError`, `ValidationResult` types
- Create: `internal/config/defaults.go` — compiled-in defaults (backend=lima, cpus=4, memory=8GiB, disk=100GiB, image=ubuntu:24.04, egress allowlist from REQ-004-007)
- Create: `internal/config/resolve.go` — `${VAR}` reference expansion from host environment, `$$` escaping

Types use both `yaml` and `mapstructure` struct tags. Default egress allowlist: 11 entries from REQ-004-007. `Resolve()` is lazy (called at use-time, not load-time per REQ-005-008). Sensitive values (`*_TOKEN`, `*_KEY`, `*_SECRET`, `*_PASSWORD`) are identified by pattern.

**Acceptance Criteria:**
- [ ] All config types from `plans/sd/03-plan/design-context.md` `internal/config/` section are defined with both `yaml:` and `mapstructure:` struct tags
- [ ] Default values match REQ-005-004 exactly
- [ ] `Resolve("${HOME}")` returns the host's HOME value
- [ ] `Resolve("$$LITERAL")` returns `$LITERAL`
- [ ] Unset variables resolve to empty string (caller handles warning)
- [ ] Tests: env var resolution, escaping, defaults

#### Issue: Implement `internal/config/` Viper-based loader (1.4)

**Type:** task
**Priority:** P1
**Parent:** Phase 1
**Dependencies:** Task 1.3 (Implement `internal/config/` types and defaults)
**Description:**
Full configuration loader with 5-level precedence, project config discovery, and security key filtering.

Files:
- Create: `internal/config/config.go` — `Loader` interface implementation, `Load()`, `Get()`, `GetForVM()`, `Source()`, `Validate()`, `Set()`, `List()` methods, Viper initialization with `SD_` prefix, env key replacer, project config walk-up discovery

Instance-based Viper (not global). `SetEnvPrefix("SD")`, `AutomaticEnv()`, `SetEnvKeyReplacer("." -> "_")`. Project config found by walking up from cwd looking for `.sd/config.yaml`. Security keys (`security.*`) from project config are silently ignored with debug log (REQ-005-017). `Set()` validates after write and rolls back on failure. `SDHome()` function returns `$SD_HOME` or `~/.sd`. `PersistentPreRunE` skips config loading for `version`, `completion`, and `help` commands (name-based check).

**Acceptance Criteria:**
- [ ] Precedence: CLI > env > project > user > default (REQ-005-001)
- [ ] Missing config file is not an error; invalid YAML is fatal
- [ ] `security.*` keys in project config are ignored with warning
- [ ] `SD_BACKEND=docker` overrides `defaults.backend`
- [ ] `GetForVM()` applies inheritance chain (REQ-005-015)
- [ ] Tests: precedence with temp files, env var override, project config discovery, security key filtering

---

### Sub-Epic: Phase 2 — Backend Interface, State Manager & Security Foundations

**Type:** epic
**Priority:** P2
**Parent:** Feature epic
**Description:** Establish the backend abstraction layer, VM state persistence, and pure-function security validations.

#### Issue: Implement `internal/backend/` interface package (2.1)

**Type:** task
**Priority:** P2
**Parent:** Phase 2
**Dependencies:** None
**Description:**
Backend interface, types, registry, sentinel errors, and optional capability interfaces.

Files:
- Create: `internal/backend/backend.go` — `Backend` interface (Name, Available, Create, Start, Stop, Destroy, Status, List, SSHConfig, Exec), `VMStatus`, `VMInfo`, `SSHConfig`, `ExecResult`, `SnapshotInfo`, `NetworkMode`, `Mount`, `ProvisionScript`, `VMConfig` types, optional interfaces (`Snapshotter`, `Cloner`, `FileSync`)
- Create: `internal/backend/errors.go` — sentinel errors: `ErrVMNotFound`, `ErrVMAlreadyExists`, `ErrVMNotRunning`, `ErrBackendNotAvailable`, `ErrBackendNotFound`, `ErrNotImplemented`
- Create: `internal/backend/registry.go` — `Register()`, `Get()`, `List()`, `Default()` with sync.RWMutex

All Backend methods take `context.Context`. `Register()` panics on duplicate name. `Default()` prefers "lima". `VMConfig.EnvVars` note: sensitive values must not be in Lima YAML (REQ-003-023). `Mount.Writable` defaults to false (REQ-004-004).

**Acceptance Criteria:**
- [ ] Compile-time interface compliance check: `var _ Backend = (*lima.LimaBackend)(nil)` compiles
- [ ] Registry: Register, Get, List, Default, duplicate-panic all tested
- [ ] Sentinel errors work with `errors.Is`
- [ ] `VMStatus` JSON-serializes as lowercase strings
- [ ] `List()` returns empty slice (not nil) from registry

#### Issue: Implement `internal/state/` package (2.2)

**Type:** task
**Priority:** P2
**Parent:** Phase 2
**Dependencies:** Task 1.3 (Implement `internal/config/` types and defaults)
**Description:**
VM metadata persistence at `$SD_HOME/vms/<name>/config.yaml`.

Files:
- Create: `internal/state/state.go` — `Manager` interface implementation: `Create()`, `Get()`, `Update()`, `Delete()`, `List()`, `Exists()`. Directory creation with mode 0700, file creation with mode 0600.
- Create: `internal/state/errors.go` — `ErrVMNotFound` (state-layer specific)

Uses `config.VMConfig` as the persisted type. Creates `$SD_HOME/vms/<name>/` with 0700. Writes `config.yaml` with 0600. `Delete()` is best-effort (REQ-001-008). Timestamps in RFC 3339. YAML marshaling via `gopkg.in/yaml.v3`. File-level advisory locking (`flock`) on `$SD_HOME/vms/<name>/config.yaml` to prevent concurrent `sd` invocations from corrupting state.

**Acceptance Criteria:**
- [ ] Create/Get round-trip preserves all VMConfig fields
- [ ] Directory permissions are 0700, file permissions are 0600
- [ ] `Delete()` removes entire VM directory
- [ ] `Get()` returns `ErrVMNotFound` for nonexistent VMs
- [ ] `List()` returns all VM names from `$SD_HOME/vms/`
- [ ] Tests: CRUD with temp directories, permission checks

#### Issue: Implement `internal/security/mount.go` (2.3)

**Type:** task
**Priority:** P2
**Parent:** Phase 2
**Dependencies:** Task 1.3 (Implement `internal/config/` types and defaults)
**Description:**
Mount path validation — pure function, testable immediately.

Files:
- Create: `internal/security/mount.go` — `ValidateMountPath(hostPath string, mode MountMode) error`, `MountMode` type, sensitive path list (from REQ-004-005), symlink resolution before checking
- Create: `internal/security/types.go` — `MountMode`, `TokenType`, `SecurityWarning`, `EgressDomain`, `DomainSource`, `CredentialEntry`, `CommandLogEntry`, `EventLogEntry`, `AuditEntry`, `AuditFilter`, `ChainBreak`, `SecurityPosture`, `MountSummary`, `ProvisionScript` types. Note: all security package types are co-located here to prevent circular imports between `security/audit.go`, `security/egress.go`, and `security/credentials.go` in Phase 7. Audit and egress types are defined now but consumed later.

Sensitive paths: `$HOME`, `~/.ssh`, `~/.aws`, `~/.config`, `~/.gnupg`, `~/.kube`, `~/.docker`, browser profile dirs, `/var/run/docker.sock`. Uses `filepath.EvalSymlinks()` before checking. User-extensible via `security.sensitive_paths` config (REQ-004-023). `ValidateToken()` detects classic PATs by `ghp_` prefix.

**Acceptance Criteria:**
- [ ] `ValidateMountPath("~")` returns error
- [ ] `ValidateMountPath("~/.ssh")` returns error
- [ ] `ValidateMountPath("/var/run/docker.sock")` returns error
- [ ] Symlinks to $HOME are resolved and rejected
- [ ] `ValidateMountPath("~/projects/my-repo")` succeeds
- [ ] `ValidateToken("ghp_...", TokenGitHubPAT)` returns CLASSIC_PAT warning
- [ ] Tests: all sensitive paths, symlink resolution, subdirectory allowed

#### Issue: Implement backend stubs (avf, docker, incus) (2.4)

**Type:** task
**Priority:** P2
**Parent:** Phase 2
**Dependencies:** Task 2.1 (Implement `internal/backend/` interface package)
**Description:**
Stub implementations that register and return `ErrNotImplemented`.

Files:
- Create: `internal/backend/avf/avf.go` — implements `Backend`, all methods return `ErrNotImplemented`
- Create: `internal/backend/docker/docker.go` — same pattern
- Create: `internal/backend/incus/incus.go` — same pattern

Each has `init()` calling `backend.Register()`. `Available()` returns `ErrNotImplemented` with message naming the backend. Each file is ~60 lines.

**Acceptance Criteria:**
- [ ] All stubs compile
- [ ] All stubs register in the registry
- [ ] `Available()` returns actionable error
- [ ] All methods return `ErrNotImplemented`

---

### Sub-Epic: Phase 3 — Lima Backend Implementation

**Type:** epic
**Priority:** P2
**Parent:** Feature epic
**Description:** Implement the default Lima backend — the first real backend that can create and manage VMs.

#### Issue: Implement Lima YAML generation (3.1)

**Type:** task
**Priority:** P2
**Parent:** Phase 3
**Dependencies:** Task 2.1 (Implement `internal/backend/` interface package)
**Description:**
Convert `backend.VMConfig` to Lima YAML configuration.

Files:
- Create: `internal/backend/lima/yaml.go` — `vmConfigToLima()` function, `limaConfig` internal type, platform detection (arm64 -> vz, amd64 -> qemu), mount mapping, sensitive env filtering
- Create: `internal/backend/lima/images.go` — built-in `imageMap` from short names to Lima image URLs (ubuntu:24.04, ubuntu:22.04, debian:12 with arm64/amd64 variants)

On arm64 darwin: `vmType: vz`, `mountType: virtiofs`. Empty mounts array overrides Lima default (REQ-003-017). Sensitive env vars (`*_TOKEN`, `*_KEY`, `*_SECRET`, `*_PASSWORD`) are filtered from the `env` YAML field (REQ-003-023). Non-sensitive env vars pass through. `ssh.forwardAgent: false` always.

**Acceptance Criteria:**
- [ ] Generated YAML sets `vmType: vz` on arm64
- [ ] Generated YAML has `mounts: []` when no mounts specified
- [ ] Sensitive env vars are absent from generated YAML env section
- [ ] Non-sensitive env vars are present in YAML env section
- [ ] Short name `ubuntu:24.04` resolves to correct image URLs
- [ ] Unknown short name produces fatal error listing available names
- [ ] Tests: YAML generation for various VMConfig inputs, platform detection, env filtering

#### Issue: Implement Lima backend lifecycle (3.2)

**Type:** task
**Priority:** P2
**Parent:** Phase 3
**Dependencies:** Task 3.1 (Implement Lima YAML generation)
**Description:**
Full Backend interface implementation using `limactl`.

Files:
- Create: `internal/backend/lima/lima.go` — `LimaBackend` struct, `init()` registration, `Name()`, `Available()`, `Create()`, `Start()`, `Stop()`, `Destroy()`, `Status()`, `List()`, `SSHConfig()`, `Exec()`. All use `exec.CommandContext()`.

`Available()` checks `limactl` in PATH. `Create()` writes temp YAML, runs `limactl create --name=sd-<name> <yaml> --tty=false`, blocks until running. `Start/Stop/Destroy` call `limactl start/stop/delete`. `Status()` parses `limactl list --json`. `SSHConfig()` returns host/port/user from `limactl show-ssh --format=config`. `Exec()` runs `limactl shell sd-<name> -- <cmd>`. Instance names prefixed with `sd-` to namespace. Context cancellation kills subprocesses.

**Acceptance Criteria:**
- [ ] `Available()` returns nil when `limactl` is in PATH
- [ ] `Available()` returns actionable error when `limactl` is missing
- [ ] `Create()` generates Lima YAML and runs limactl
- [ ] `Status()` correctly maps limactl JSON to VMStatus
- [ ] `SSHConfig()` returns valid connection details
- [ ] All methods use `exec.CommandContext(ctx, ...)`
- [ ] Tests: unit tests with mock command execution; integration tests (tagged) with real limactl

#### Issue: Implement Lima snapshot support (3.3)

**Type:** task
**Priority:** P2
**Parent:** Phase 3
**Dependencies:** Task 3.2 (Implement Lima backend lifecycle)
**Description:**
`Snapshotter` interface using APFS clones.

Files:
- Create: `internal/backend/lima/snapshot.go` — `SnapshotCreate`, `SnapshotApply`, `SnapshotDelete`, `SnapshotList`. Clone-based strategy with `cp -c` on macOS.

Flow: stop VM -> `cp -c ~/.lima/sd-<name> ~/.lima/.snapshots/sd-<name>/<tag>/` -> restart. Metadata in `$SD_HOME/vms/<name>/snapshots.yaml`. `SnapshotApply` replaces instance dir. Atomic: failed snapshot does not corrupt running VM.

**Acceptance Criteria:**
- [ ] `SnapshotCreate` uses APFS clones (`cp -c`) on macOS
- [ ] Snapshot metadata is written to `$SD_HOME/vms/<name>/snapshots.yaml`
- [ ] `SnapshotList` returns name, timestamp, size
- [ ] Failed snapshot does not corrupt the running VM
- [ ] Tests: unit tests for metadata management; integration tests for full snapshot lifecycle

---

### Sub-Epic: Phase 4 — Connection Infrastructure & SSH Management

**Type:** epic
**Priority:** P2
**Parent:** Feature epic
**Description:** Establish SSH key management, config fragment generation, and environment variable injection.

#### Issue: Implement SSH key generation (4.1)

**Type:** task
**Priority:** P2
**Parent:** Phase 4
**Dependencies:** None
**Description:**
Per-VM Ed25519 SSH key pair generation and management.

Files:
- Create: `internal/connection/keys.go` — `GenerateKeyPair(vmDir string) error`, `KeyPath(vmDir string) string`, `PubKeyPath(vmDir string) string`. Uses `golang.org/x/crypto/ssh` for Ed25519 key generation.

Keys stored at `$SD_HOME/vms/<name>/ssh/id_ed25519[.pub]`. Private key mode 0600. SSH directory mode 0700. Each VM gets a unique key pair. Public key is injected into VM's `authorized_keys` during creation.

**Acceptance Criteria:**
- [ ] Generated keys are Ed25519
- [ ] Private key file has permissions 0600
- [ ] SSH directory has permissions 0700
- [ ] Each VM gets a unique key pair
- [ ] Tests: key generation, file permissions, unique keys per call

#### Issue: Implement SSH config management (4.2)

**Type:** task
**Priority:** P2
**Parent:** Phase 4
**Dependencies:** Task 2.1 (Implement `internal/backend/` interface package)
**Description:**
Write/remove `~/.ssh/config.d/sd-<name>` fragments.

Files:
- Create: `internal/connection/sshconfig.go` — `WriteSSHConfig(name string, cfg backend.SSHConfig) error`, `RemoveSSHConfig(name string) error`, `SSHConfigFor(name string) (*SSHConfigData, error)`. Generates correct fragment for TCP vs VSOCK transport.

TCP: `StrictHostKeyChecking yes`, `UserKnownHostsFile $SD_HOME/vms/<name>/ssh/known_hosts`. VSOCK: `StrictHostKeyChecking no`, `UserKnownHostsFile /dev/null`. Always: `ForwardAgent no`, `ForwardX11 no`, `LogLevel ERROR`, `SendEnv SD_* ANTHROPIC_* GITHUB_* GH_*`. Warn if `~/.ssh/config` doesn't include `config.d/*`.

**Acceptance Criteria:**
- [ ] TCP fragment has `StrictHostKeyChecking yes` + known_hosts
- [ ] VSOCK fragment has `ProxyCommand limactl ssh --stdio <name>`
- [ ] All fragments include `ForwardAgent no`, `ForwardX11 no`
- [ ] All fragments include `SendEnv` patterns
- [ ] `RemoveSSHConfig` deletes the fragment file
- [ ] Tests: fragment generation for TCP and VSOCK, removal

#### Issue: Implement environment variable injection (4.3)

**Type:** task
**Priority:** P2
**Parent:** Phase 4
**Dependencies:** Task 1.3 (Implement `internal/config/` types and defaults)
**Description:**
Resolve `${VAR}` references and prepare env vars for SSH SendEnv.

Files:
- Create: `internal/connection/env.go` — `ResolveEnvVars(envMap map[string]string) (resolved map[string]string, warnings []string)`. Resolves `${VAR}` from host env. Returns warnings for unset variables.

Uses `config.Resolve()` for each value. Unresolvable variables set to empty string with warning. Sensitive values are never logged. The connection layer prepares `-o SendEnv=VAR` SSH args for each resolved variable.

**Acceptance Criteria:**
- [ ] `${HOME}` resolves to host HOME value
- [ ] Unset variables produce warnings and resolve to empty string
- [ ] Resolved values are not logged at any level
- [ ] Tests: resolution, unset handling, empty values

---

### Sub-Epic: Phase 5 — CLI Core & Provisioning Engine

**Type:** epic
**Priority:** P2
**Parent:** Feature epic
**Description:** Build the CLI framework (root command, App struct, PersistentPreRunE) and the core VM lifecycle commands, plus the provisioning module system.

#### Issue: Implement root command and App struct (5.1)

**Type:** task
**Priority:** P2
**Parent:** Phase 5
**Dependencies:** Task 1.2 (Implement `internal/ui/` package), Task 1.4 (Implement `internal/config/` Viper-based loader), Task 2.1 (Implement `internal/backend/` interface package)
**Description:**
CLI entry point, global flags, dependency wiring, and the `App` struct.

Files:
- Create: `internal/cmd/root.go` — `Execute()`, root cobra.Command, `PersistentPreRunE` (load config, validate --verbose/--quiet mutual exclusivity, init logger, set up output formatter), `App` struct with all dependency fields, `RootFlags`, `VMNamePattern`, `ValidateVMName()`, `resolveVMName()` helper
- Create: `internal/security/noop.go` — `noopAuditLogger`, `noopEgressController`, `noopCredentialInjector` stub implementations that satisfy Phase 7 interfaces with no-op behavior, used as defaults in App struct until Phase 7 replaces them

Global flags: `--json`, `--verbose`/`-v`, `--quiet`/`-q`, `--config`, `--vm`. `PersistentPreRunE` constructs the `App` struct. Backend resolved via `backend.Get(cfg.Defaults.Backend)`. Command groups: VM Management, Connection, Configuration, Provisioning, Security, Diagnostics, Utility. Enable prefix matching. `SilenceUsage: true`, `SilenceErrors: true`. Logger: `log/slog` with TextHandler to stderr. `PersistentPreRunE` skips config loading and App construction for `version`, `completion`, and `help` commands (name-based check). Phase-7 security fields (`Egress`, `Creds`, `Audit`) are initialized with no-op stubs (`noopAuditLogger`, `noopEgressController`, `noopCredentialInjector`) in `internal/security/noop.go`; replaced with real implementations in Phase 7.

**Acceptance Criteria:**
- [ ] `sd` with no args prints help and exits 0
- [ ] `--verbose --quiet` exits with code 2
- [ ] `--json` flag available on all commands
- [ ] `sd version` works without config file
- [ ] VM name validation regex: `^[a-z][a-z0-9-]{0,62}$`
- [ ] Tests: flag parsing, mutual exclusivity, name validation

#### Issue: Implement `sd create` command (5.2)

**Type:** task
**Priority:** P2
**Parent:** Phase 5
**Dependencies:** Task 5.1 (Implement root command and App struct), Task 2.2 (Implement `internal/state/` package), Task 2.3 (Implement `internal/security/mount.go`), Task 3.2 (Implement Lima backend lifecycle), Task 4.1 (Implement SSH key generation), Task 4.2 (Implement SSH config management), Task 5.5b (Implement provisioning executor and probes)
**Description:**
VM creation with full lifecycle: validate, resolve backend, build VMConfig, create, provision, write state.

Files:
- Create: `internal/cmd/create.go` — `createCmd` struct, flags: `--backend`, `--cpus`, `--memory`, `--disk`, `--modules`, `--mount`, `--allow-egress`. Implements rollback on failure (destroy partial VM).

Data flow per REQ-001-006: ValidateVMName -> State.Exists (reject duplicates early) -> Config.GetForVM -> SecurityValidator.ValidateMountPath for each mount -> GenerateKeyPair (must happen before Backend.Create so public key can be included in Lima YAML) -> build backend.VMConfig (include public key path) -> Backend.Create -> capture SSH host key via `ssh-keyscan` and write to `$SD_HOME/vms/<name>/ssh/known_hosts` (REQ-007-004) -> WriteSSHConfig -> Provisioner.Provision -> State.Create -> Audit.LogEvent. On failure: Backend.Destroy for cleanup (using `context.Background()` for rollback). `--dry-run` prints resolved VMConfig without executing. Long help text includes threat model summary per REQ-004-001. `--modules all` provisions every available module (REQ-006-015).

**Acceptance Criteria:**
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
- [ ] `--dry-run` prints resolved VMConfig to stdout without calling Backend.Create
- [ ] Tests: flag parsing, rollback logic, mount validation integration, host key capture

#### Issue: Implement `sd destroy`, `sd start`, `sd stop` commands (5.3)

**Type:** task
**Priority:** P2
**Parent:** Phase 5
**Dependencies:** Task 5.1 (Implement root command and App struct), Task 2.2 (Implement `internal/state/` package), Task 3.2 (Implement Lima backend lifecycle), Task 3.3 (Implement Lima snapshot support)
**Description:**
VM lifecycle management commands.

Files:
- Create: `internal/cmd/destroy.go` — `--force`, `--no-snapshot`, auto-snapshot before destroy (REQ-004-019), state cleanup, SSH config removal
- Create: `internal/cmd/start.go` — start stopped VM
- Create: `internal/cmd/stop.go` — stop running VM

Destroy flow per REQ-001-008: State.Get -> Backend.Status -> if running && !force: error -> if !noSnapshot: SnapshotCreate (if snapshot fails and --force: warn and proceed; if snapshot fails and !--force: abort with error requiring `--force --no-snapshot`) -> if running: Stop -> Destroy -> State.Delete -> RemoveSSHConfig -> delete SSH key files at `$SD_HOME/vms/<name>/ssh/` (REQ-004-014) -> Audit.LogEvent. Start/Stop are thin wrappers with status check. Idempotent: start on running is no-op, stop on stopped is no-op.

**Acceptance Criteria:**
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

#### Issue: Implement `sd list` and `sd status` commands (5.4)

**Type:** task
**Priority:** P2
**Parent:** Phase 5
**Dependencies:** Task 5.1 (Implement root command and App struct), Task 2.2 (Implement `internal/state/` package), Task 3.2 (Implement Lima backend lifecycle)
**Description:**
VM listing and status reporting.

Files:
- Create: `internal/cmd/list.go` — alias `ls`, tabular output, `--json`
- Create: `internal/cmd/status.go` — single VM or all VMs, includes provisioning state

`list` merges state manager data with backend live status. Detects orphaned state (metadata exists but backend has no VM) and reports as warning. `status` shows: name, status, backend, resources, IP, created time, provisioning state (if available). Both support `--json`.

**Acceptance Criteria:**
- [ ] `sd list` shows all VMs in tabular format
- [ ] `sd ls` is equivalent to `sd list`
- [ ] `sd list --json` outputs JSON array
- [ ] Orphaned state detected and warned
- [ ] `sd status myvm --json` includes provisioning info
- [ ] Tests: output formatting, orphan detection

#### Issue: Implement provisioning engine core (5.5a)

**Type:** task
**Priority:** P2
**Parent:** Phase 5
**Dependencies:** Task 1.3 (Implement `internal/config/` types and defaults)
**Description:**
Module types, YAML parsing, dependency resolution, and module loading.

Files:
- Create: `internal/provision/module.go` — `Module`, `Script`, `Probe`, `ModuleStatus`, `ProvisionState`, `ModuleExecutionStatus` types, YAML parsing and validation
- Create: `internal/provision/loader.go` — load built-in modules via `//go:embed`, discover custom modules from `~/.sd/provisions/`, name conflict detection
- Create: `internal/provision/resolver.go` — topological sort (Kahn's algorithm) for dependency ordering, circular dependency detection
- Create: `internal/provision/state.go` — `ProvisionState` persistence to `$SD_HOME/vms/<name>/provision-state.yaml`, per-module status tracking (pending/running/completed/failed) for `sd status` to read (REQ-006-009)

`base` module always runs first (REQ-006-002). Circular deps detected before any execution. `ProvisionState` written to disk after each module completes so `sd status` can read provisioning progress independently.

**Acceptance Criteria:**
- [ ] Module YAML files parse correctly
- [ ] Topological sort produces correct ordering
- [ ] Circular dependency A->B->A is caught at validation time
- [ ] `base` always runs first regardless of module selection
- [ ] Custom modules from `~/.sd/provisions/` are discovered
- [ ] Name conflict between custom and built-in produces fatal error
- [ ] `ProvisionState` written to disk, readable by `sd status`
- [ ] Tests: YAML parsing, topo sort, circular detection, state persistence

#### Issue: Implement provisioning executor and probes (5.5b)

**Type:** task
**Priority:** P2
**Parent:** Phase 5
**Dependencies:** Task 5.5a (Implement provisioning engine core), Task 2.1 (Implement `internal/backend/` interface package)
**Description:**
Script execution via backend.Exec(), readiness probe polling.

Files:
- Create: `internal/provision/provision.go` — `Engine` struct implementing `Provisioner` interface, `Provision()`, `ListModules()`, `GetModule()`, `ValidateModules()`
- Create: `internal/provision/executor.go` — execute scripts via `backend.Exec()`, prepend `set -eux -o pipefail`, handle system/user modes, update `ProvisionState` after each module
- Create: `internal/provision/probe.go` — readiness probe polling with interval and timeout

Scripts executed via `backend.Exec()` (not direct SSH — enables future backends). `set -eux -o pipefail` prepended to every script. System scripts run as root, user scripts as default user. Checksum verification: download to temp, verify SHA-256, then install (REQ-006-016). No `curl | sh` patterns.

**Acceptance Criteria:**
- [ ] `set -eux -o pipefail` is prepended to all scripts
- [ ] Readiness probes poll at configured interval
- [ ] Probe timeout produces actionable error
- [ ] `ProvisionState` updated after each module execution
- [ ] Tests: executor, probe timing, state updates

#### Issue: Implement built-in provisioning modules (5.5c)

**Type:** task
**Priority:** P2
**Parent:** Phase 5
**Dependencies:** Task 5.5a (Implement provisioning engine core)
**Description:**
YAML module definitions for all built-in modules.

Files:
- Create: `internal/provision/modules/base.yaml` — git, curl, build-essential, ca-certificates, jq, tmux, vim. Also: sshd config drop-in at `/etc/ssh/sshd_config.d/sd-security.conf` with `AcceptEnv SD_* ANTHROPIC_* GITHUB_* GH_*` (REQ-004-011, REQ-007-019), `AllowTcpForwarding local`, `GatewayPorts no`, `PermitTunnel no`, `X11Forwarding no` (REQ-004-026), then reload sshd.
- Create: `internal/provision/modules/claude-code.yaml` — Node.js via nvm (pin version + checksum), Claude Code CLI via npm. Git credential helper: write custom credential helper script that reads from `$GITHUB_TOKEN` env var, configure `git config --global credential.helper ""` then `git config --global credential.helper /usr/local/bin/sd-git-credential-helper`, configure `user.name`/`user.email` from host git config, verify `~/.git-credentials` does not exist (REQ-006-012, REQ-004-030). Detect and preserve `CLAUDE.md`/`AGENTS.md` in cloned repos (REQ-006-013).
- Create: `internal/provision/modules/docker.yaml` — Docker Engine rootless (requires newuidmap, kernel namespaces, loginctl configuration). Pin version + checksum. Probe: `docker info`.
- Create: `internal/provision/modules/golang.yaml` — Go toolchain. Pin version (e.g., 1.22.x) + SHA-256 checksum. Install to `/usr/local/go`, add to PATH. Probe: `go version`.
- Create: `internal/provision/modules/rust.yaml` — Rust via rustup. Pin rustup-init checksum. User mode (not root). Probe: `rustc --version`.
- Create: `internal/provision/modules/python.yaml` — Python 3 with pip and venv. System package install. Probe: `python3 --version`.
- Create: `internal/provision/modules/github-cli.yaml` — GitHub CLI from official apt repo. Pin version. Probe: `gh --version`.

Each module specifies: version pins, SHA-256 checksums for downloads, system vs user mode, probe command, and dependencies. `base` module includes critical sshd security configuration.

**Acceptance Criteria:**
- [ ] All module YAML files parse and validate
- [ ] `base.yaml` configures sshd AcceptEnv and port forwarding restrictions
- [ ] `claude-code.yaml` sets up git credential helper from env var (REQ-006-012)
- [ ] `claude-code.yaml` clears pre-existing credential helpers and verifies no ~/.git-credentials
- [ ] All download modules have SHA-256 checksums
- [ ] Each module has a readiness probe command
- [ ] Tests: YAML parsing, module validation

#### Issue: Implement `sd version` command (5.6)

**Type:** task
**Priority:** P2
**Parent:** Phase 5
**Dependencies:** Task 5.1 (Implement root command and App struct)
**Description:**
Version information output.

Files:
- Create: `internal/cmd/version.go` — prints version, commit, build date, Go version. Supports `--json`.

Version, Commit, BuildDate injected via ldflags at build time. Works without config file (PersistentPreRunE should not fail on missing config for this command).

**Acceptance Criteria:**
- [ ] `sd version` prints version, commit, date, Go version
- [ ] `sd version --json` outputs JSON object
- [ ] Works without config file

---

### Sub-Epic: Phase 6 — Connection & Interaction Commands

**Type:** epic
**Priority:** P2
**Parent:** Feature epic
**Description:** Implement connect, exec, sync, and ssh-config commands.

#### Issue: Implement connection manager (6.1)

**Type:** task
**Priority:** P2
**Parent:** Phase 6
**Dependencies:** Task 4.1 (Implement SSH key generation), Task 4.2 (Implement SSH config management), Task 4.3 (Implement environment variable injection)
**Description:**
`Connector` interface implementation for interactive SSH + tmux sessions.

Files:
- Create: `internal/connection/connect.go` — `Connector` interface implementation, `Connect()` method. Builds SSH command with correct key, host, port, transport, SendEnv args, port forwarding (-L) args. Handles tmux attach/create logic.
- Create: `internal/connection/tmux.go` — tmux session management: `tmux new-session -A -s <name>`, new window creation, session name validation (alphanumeric, hyphens, underscores only)

Connect flow: resolve VM -> check status -> auto-start if stopped (unless --no-start) -> resolve env vars -> build SSH command -> if no-tmux: direct SSH; else: SSH + `tmux new-session -A -s sd-<name>`. For --new-window: `tmux new-window -t <session>` then attach. For concurrent connections (REQ-007-020): if session `sd-<name>` already exists, create a new window with `tmux new-window -t sd-<name>` and attach. Port forwarding: `-L 127.0.0.1:<host>:<guest>` for each forward. VSOCK transport uses `-o ProxyCommand=...` on command line (not from config fragment) to avoid conflict with `-F /dev/null`.

**Acceptance Criteria:**
- [ ] `Connect()` establishes SSH + tmux session
- [ ] Auto-start works for stopped VMs
- [ ] `--no-start` returns error for stopped VM
- [ ] Port forwarding adds correct `-L` arguments
- [ ] `--no-tmux` bypasses tmux
- [ ] `--new-window` creates new tmux window
- [ ] `--no-tmux` + `--new-window` is rejected (code 2)
- [ ] Concurrent connections each get separate tmux windows (REQ-007-020)
- [ ] VSOCK transport uses `-o ProxyCommand=...` on command line
- [ ] Tests: SSH command construction, tmux args, mutual exclusivity, concurrent windows

#### Issue: Implement `sd connect` CLI command (6.2)

**Type:** task
**Priority:** P2
**Parent:** Phase 6
**Dependencies:** Task 5.1 (Implement root command and App struct), Task 6.1 (Implement connection manager)
**Description:**
CLI wiring for the connect command.

Files:
- Create: `internal/cmd/connect.go` — alias `c`, flags: `--session`, `--new-window`, `--no-tmux`, `--forward`, `--no-start`. Resolves VM name, calls Connector.Connect().

Credential injection: `app.Creds.InjectEnv(name)` -> map of resolved env vars. Failures are warnings, not fatal (REQ-001-007). After session ends: log duration, audit event (interactive mode only, not when `--json` is used).

**Acceptance Criteria:**
- [ ] `sd connect myvm` and `sd c myvm` both work
- [ ] `--json` outputs connection metadata without opening session
- [ ] Credential injection failure is a warning, not fatal
- [ ] Tests: flag parsing, credential injection error handling

#### Issue: Implement `sd exec` CLI command (6.3)

**Type:** task
**Priority:** P2
**Parent:** Phase 6
**Dependencies:** Task 5.1 (Implement root command and App struct), Task 6.1 (Implement connection manager)
**Description:**
Non-interactive remote command execution.

Files:
- Create: `internal/cmd/exec.go` — `--` separator, pass-through stdout/stderr, match remote exit code, `--json` wraps in ExecResult

Does NOT auto-start stopped VM (returns error). Resolves env vars and passes via SendEnv. `--json` captures stdout/stderr and wraps in `{"exit_code": N, "stdout": "...", "stderr": "..."}`. Without `--json`, stdout/stderr stream through directly.

**Acceptance Criteria:**
- [ ] `sd exec myvm -- ls -la` passes through output
- [ ] Exit code matches remote command
- [ ] Stopped VM returns error (no auto-start)
- [ ] `--json` wraps output correctly
- [ ] Tests: exit code propagation, JSON output structure

#### Issue: Implement file sync (6.4)

**Type:** task
**Priority:** P2
**Parent:** Phase 6
**Dependencies:** Task 4.2 (Implement SSH config management), Task 5.1 (Implement root command and App struct)
**Description:**
rsync-based file synchronization.

Files:
- Create: `internal/connection/sync.go` — `FileSyncer` interface implementation: `SyncTo()`, `SyncFrom()`, `Diff()`, `Watch()`. Uses rsync over SSH with managed identity.
- Create: `internal/cmd/sync.go` — `sd sync to` and `sd sync from` subcommands, `--diff`, `--watch`

rsync command: `rsync -avz -e "ssh -i <key> -p <port> -o StrictHostKeyChecking=yes -o UserKnownHostsFile=<knownhosts>" <src> <user>@<host>:<dst>` where `<user>`, `<host>`, `<port>`, and `<key>` come from `backend.SSHConfig` returned by `Backend.SSHConfig(name)` and `connection.KeyPath(vmDir)`. `--watch` uses `fsnotify` for file system monitoring (host side only, SyncTo direction only). `--diff` uses `rsync --dry-run --itemize-changes`. Default dst: `~/<basename>` for SyncTo, `./<basename>` for SyncFrom.

**Acceptance Criteria:**
- [ ] `sd sync to myvm ./src /home/user/src` copies files
- [ ] `sd sync from myvm /path ./local` copies back
- [ ] `--diff` shows differences without copying
- [ ] `--watch` is SyncTo only; using with SyncFrom exits code 2
- [ ] Tests: rsync command construction, direction validation

#### Issue: Implement `sd ssh-config` command (6.5)

**Type:** task
**Priority:** P2
**Parent:** Phase 6
**Dependencies:** Task 5.1 (Implement root command and App struct), Task 4.2 (Implement SSH config management)
**Description:**
Print SSH config fragment for a VM.

Files:
- Create: `internal/cmd/ssh_config.go` — prints fragment to stdout, supports `--json` for individual fields

**Acceptance Criteria:**
- [ ] Prints valid SSH config fragment to stdout
- [ ] `--json` outputs fields as JSON object
- [ ] Nonexistent VM exits code 1

---

### Sub-Epic: Phase 7 — Security Operations & Remaining Commands

**Type:** epic
**Priority:** P2
**Parent:** Feature epic
**Description:** Implement egress enforcement, audit logging, credential management, and all remaining CLI commands.

#### Issue: Spike: Validate iptables/dnsmasq inside Lima VZ (7.0)

**Type:** task
**Priority:** P2
**Parent:** Phase 7
**Dependencies:** Task 3.2 (Implement Lima backend lifecycle)
**Description:**
Verify that iptables and dnsmasq work inside a Lima VM with VZ userspace networking before building the full egress system.

Create a minimal Lima VM with VZ backend, install iptables and dnsmasq, verify: (a) iptables OUTPUT chain rules take effect, (b) dnsmasq can bind to 127.0.0.1:53, (c) DNS queries route through dnsmasq, (d) blocked domains actually fail to resolve. If VZ userspace stack doesn't support iptables, document the fallback approach.

**Acceptance Criteria:**
- [ ] Spike results documented (works / doesn't work / partial)
- [ ] If iptables doesn't work: fallback approach identified

#### Issue: Implement egress control (7.1)

**Type:** task
**Priority:** P2
**Parent:** Phase 7
**Dependencies:** Task 7.0 (Spike: Validate iptables/dnsmasq inside Lima VZ), Task 2.3 (Implement `internal/security/mount.go`)
**Description:**
Egress rule generation (pure functions) and dnsmasq/iptables script generation. The `security/` package generates scripts but does NOT execute them — execution is done by the CLI layer via `backend.Exec()`.

Files:
- Create: `internal/security/egress.go` — `EgressController` interface: `GenerateAllowlistScripts(ctx, domains) []security.ProvisionScript`, `GenerateAddDomainScript(ctx, domain) security.ProvisionScript`, `GenerateRemoveDomainScript(ctx, domain) security.ProvisionScript`, `ListDomains(vmName) ([]EgressDomain, error)`. Returns `security.ProvisionScript` structs (defined in `security/types.go` — NOT `backend.ProvisionScript`) so that `security/` does not import `backend/`. The CLI layer converts `security.ProvisionScript` to `backend.ProvisionScript` and executes via `backend.Exec()`.

iptables: OUTPUT chain default DROP for new outbound. DNS to 127.0.0.1 only. Block DoH to known providers (8.8.8.8, 1.1.1.1, 9.9.9.9) on port 443. Block DoT (port 853). SSH from host always allowed. dnsmasq: bound to 127.0.0.1:53, forward only allowlisted domains, NXDOMAIN for everything else. Wildcard `*.example.com` matches one subdomain level only (REQ-004-007). Domain re-resolution at configurable interval (default 5 min, REQ-004-010). Installation path: egress scripts are applied during `sd create` as a provisioning step. dnsmasq runs as a systemd service (`sd-dnsmasq.service`). `AddDomain` post-creation: generates iptables append + dnsmasq config update script, executed via `backend.Exec()` — works without VM restart. `security/egress.go` does NOT import `backend/`.

**Acceptance Criteria:**
- [ ] Default policy is DROP for outbound
- [ ] Allowlisted domains are resolvable and reachable
- [ ] `*.githubusercontent.com` matches `raw.githubusercontent.com` but not `a.b.githubusercontent.com`
- [ ] DNS queries for non-allowlisted domains return NXDOMAIN
- [ ] `AddDomain` works without VM restart (live iptables + dnsmasq update)
- [ ] `security/egress.go` does NOT import `backend/` — returns `ProvisionScript` for caller to execute
- [ ] dnsmasq installed as systemd service
- [ ] Tests: wildcard matching, iptables rule generation, dnsmasq config generation (all pure function tests)

#### Issue: Implement audit logging (7.2)

**Type:** task
**Priority:** P2
**Parent:** Phase 7
**Dependencies:** None
**Description:**
Command and event logging with SHA-256 hash chain.

Files:
- Create: `internal/security/audit.go` — `AuditLogger` interface implementation: `LogCommand()`, `LogEvent()`, `Query()`, `VerifyChain()`. Append-only log at `$SD_HOME/audit.log`. Each entry has `prev_hash` (SHA-256 of previous line). Genesis entry uses all-zeros hash.

JSON lines format (one JSON object per line). `LogCommand` records: timestamp, command, args (secrets redacted), exit code, duration. `LogEvent` records: timestamp, event_type, vm_name, metadata. `Query` supports filtering by VM name, since/until timestamps. `VerifyChain` validates hash chain integrity and returns first `ChainBreak` if found. Secrets in args replaced with `[REDACTED]`.

**Acceptance Criteria:**
- [ ] Every command invocation logged
- [ ] Secrets are redacted in log entries
- [ ] Hash chain: entry N's prev_hash = SHA-256(entry N-1 line)
- [ ] `VerifyChain()` detects tampering
- [ ] `Query()` filters by VM and time range
- [ ] Tests: hash chain integrity, secret redaction, query filtering

#### Issue: Implement credential management (7.3)

**Type:** task
**Priority:** P2
**Parent:** Phase 7
**Dependencies:** Task 1.3 (Implement `internal/config/` types and defaults), Task 2.2 (Implement `internal/state/` package)
**Description:**
`CredentialInjector` for runtime credential injection.

Files:
- Create: `internal/security/credentials.go` — `CredentialInjector` interface implementation: `InjectEnv()`, `Rotate()`, `Revoke()`, `List()`. Resolves `${VAR}` references from VM config, returns resolved map. Never writes to files.

`InjectEnv()` reads VM's `env` config, resolves each `${VAR}` from host env, returns map. `Rotate()` updates which host environment variable name a `${VAR}` reference points to (e.g., `${GITHUB_TOKEN}` -> `${GITHUB_TOKEN_V2}`), NOT the token value itself. `Revoke()` removes all env entries from VM config. `List()` returns credential types without values. `ValidateToken()` for classic PAT detection.

**Acceptance Criteria:**
- [ ] `InjectEnv()` resolves all `${VAR}` references
- [ ] No credential values written to any file
- [ ] `Revoke()` ensures no credentials injected on next connect
- [ ] `List()` shows types without values
- [ ] Tests: resolution, revoke, list

#### Issue: Implement configuration CLI commands (7.4)

**Type:** task
**Priority:** P2
**Parent:** Phase 7
**Dependencies:** Task 5.1 (Implement root command and App struct), Task 1.4 (Implement `internal/config/` Viper-based loader), Task 7.1 (Implement egress control)
**Description:**
`sd config set/get/list/edit/validate/egress` commands.

Files:
- Create: `internal/cmd/config.go` — `sd config` parent command, `set`, `get`, `list`, `edit`, `validate` subcommands
- Create: `internal/cmd/config_egress.go` — `sd config egress add/remove/list` subcommands

`set` writes to user config by default, `--project` for project config. `get` prints value to stdout (no decoration) for scripting. `list` tabular with KEY/VALUE/SOURCE columns. `edit` opens `$EDITOR` (or `$VISUAL`, or `vi`), errors if `SD_JSON=true`. `validate` checks all config files. Egress commands call `EgressController` methods. `config get` shows `${VAR}` references, not resolved values.

**Acceptance Criteria:**
- [ ] `sd config set defaults.cpus 8` persists
- [ ] `sd config get defaults.cpus` prints `8` to stdout
- [ ] `sd config list` shows all keys with sources
- [ ] `sd config validate` checks all files
- [ ] `sd config egress add/remove/list` work
- [ ] All support `--json`

#### Issue: Implement security CLI commands (7.5)

**Type:** task
**Priority:** P2
**Parent:** Phase 7
**Dependencies:** Task 5.1 (Implement root command and App struct), Task 7.2 (Implement audit logging), Task 7.3 (Implement credential management)
**Description:**
Token, audit, security status, and diff commands.

Files:
- Create: `internal/cmd/token.go` — `sd token github setup`, `sd token rotate`, `sd token revoke`, `sd token list`
- Create: `internal/cmd/audit.go` — `sd audit [vm] --since --json`
- Create: `internal/cmd/security.go` — `sd security status <vm>`
- Create: `internal/cmd/diff.go` — `sd diff <vm>` detects CI/CD workflow changes

`token github setup` outputs PAT creation guidance including recommended scopes for fine-grained tokens; if token resolves and starts with `ghp_` prefix, warn that classic PATs are not recommended (REQ-004-012) and show the recommended fine-grained token scopes. `--json` output includes `recommended_scopes` field. `token rotate` updates `${VAR}` reference. `token revoke` removes credentials. `audit` queries audit log with optional VM filter and time range. `security status` shows mounts, egress domains, credentials, snapshots, last audit event, warnings. `diff` detects changes to `.github/workflows/*`, `.gitlab-ci.yml`, `Jenkinsfile`, `.circleci/*`, `.git/hooks/*`.

**Acceptance Criteria:**
- [ ] `sd token github setup` outputs guidance
- [ ] `sd audit myvm --since <ts> --json` filters and outputs JSON
- [ ] `sd security status myvm` shows posture summary
- [ ] `sd diff myvm` flags CI/CD changes
- [ ] All support `--json`

#### Issue: Implement remaining CLI commands (7.6)

**Type:** task
**Priority:** P2
**Parent:** Phase 7
**Dependencies:** Task 5.1 (Implement root command and App struct), Task 5.5a (Implement provisioning engine core), Task 5.5b (Implement provisioning executor and probes), Task 3.3 (Implement Lima snapshot support), Task 7.2 (Implement audit logging)
**Description:**
snapshot, provision, doctor, logs, completion commands.

Files:
- Create: `internal/cmd/snapshot.go` — `sd snapshot create/list/restore/delete`
- Create: `internal/cmd/provision.go` — `sd provision [vm] --modules`, `sd provision list`
- Create: `internal/cmd/doctor.go` — checks limactl, ssh, tmux, rsync, config validity, SD_HOME writable, backend availability, network connectivity to default egress allowlist endpoints (REQ-002-007)
- Create: `internal/cmd/logs.go` — `sd logs [name] --tail --follow`. Log sources: (1) provisioning output from `$SD_HOME/vms/<name>/provision-state.yaml`, (2) sd audit log filtered by VM name, (3) VM journal via `limactl shell <name> -- journalctl --user -n <N>`. `--tail N` limits to last N entries. `--follow` streams VM journal via `journalctl --follow` exec'd through `backend.Exec()`.
- Create: `internal/cmd/completion.go` — bash/zsh/fish completion generation with custom completers for VM names, backends, snapshot tags, module names

`doctor` runs checks with pass/fail indicators including network connectivity to each of the 11 default egress allowlist endpoints. `logs` aggregates three sources and presents chronologically. `provision list` shows all modules. `completion` generates shell scripts. Custom completions registered for VM names (from state.List), backend names (from backend.List), module names (from provisioner.ListModules).

**Acceptance Criteria:**
- [ ] `sd snapshot create/list/restore/delete` work
- [ ] `sd provision list` shows all modules
- [ ] `sd doctor` checks all prerequisites including network connectivity to egress endpoints
- [ ] `sd logs myvm --tail 20` shows last 20 log entries
- [ ] `sd logs myvm --follow` streams journal output
- [ ] `sd completion bash/zsh/fish` outputs valid scripts
- [ ] All support `--json` where applicable

#### Issue: Implement `sd reset` command (7.7)

**Type:** task
**Priority:** P2
**Parent:** Phase 7
**Dependencies:** Task 5.1 (Implement root command and App struct), Task 3.3 (Implement Lima snapshot support), Task 2.2 (Implement `internal/state/` package)
**Description:**
Reset a VM to a prior snapshot state (REQ-004-019).

Files:
- Create: `internal/cmd/reset.go` — `sd reset <name> [--snapshot <tag>]`, auto-snapshots before reset (destructive operation per REQ-004-019), restores from specified snapshot or latest.

Reset flow: State.Get -> Backend.Status -> auto-snapshot (same logic as destroy) -> SnapshotApply(tag) -> Backend.Start -> Audit.LogEvent. If no `--snapshot` specified, uses the most recent snapshot. `--force` skips auto-snapshot. `--no-snapshot` skips auto-snapshot. Both `--force` and `--no-snapshot` behave identically to `sd destroy` flags.

**Acceptance Criteria:**
- [ ] `sd reset myvm` creates auto-snapshot then restores latest
- [ ] `sd reset myvm --snapshot v1` restores specific snapshot
- [ ] Auto-snapshot before reset (REQ-004-019)
- [ ] `--json` outputs structured result
- [ ] Tests: reset flow, snapshot selection

---

### Sub-Epic: Phase 8 — Integration, Testing & Polish

**Type:** epic
**Priority:** P2
**Parent:** Feature epic
**Description:** End-to-end verification, integration tests, script tests, and polish.

#### Issue: Integration test suite (8.1)

**Type:** task
**Priority:** P2
**Parent:** Phase 8
**Dependencies:** Task 7.6 (Implement remaining CLI commands)
**Description:**
Real-system integration tests tagged `//go:build integration`.

Files:
- Create: `internal/backend/lima/lima_integration_test.go` — full lifecycle test
- Create: `internal/config/config_integration_test.go` — real file loading
- Create: `internal/state/state_integration_test.go` — persistence round-trip
- Create: `internal/provision/provision_integration_test.go` — module execution
- Create: `internal/connection/connect_integration_test.go` — SSH key + config

**Acceptance Criteria:**
- [ ] All tests gated with `//go:build integration` tag; tests `t.Skip` when required tools (e.g., `limactl`) are not in PATH
- [ ] Lima integration test covers full create/start/stop/destroy lifecycle
- [ ] Config integration test verifies real file loading with 5-level precedence
- [ ] State integration test verifies CRUD persistence survives process restart
- [ ] Provision integration test verifies module execution via backend.Exec()
- [ ] Connection integration test verifies SSH key generation and config fragment writing

#### Issue: Script test suite (8.2)

**Type:** task
**Priority:** P2
**Parent:** Phase 8
**Dependencies:** Task 7.6 (Implement remaining CLI commands)
**Description:**
CLI-level script tests using `rsc.io/script/scripttest`.

Files:
- Create: `testdata/scripts/create_json.txtar`
- Create: `testdata/scripts/create_duplicate.txtar`
- Create: `testdata/scripts/create_invalid_name.txtar`
- Create: `testdata/scripts/destroy_force.txtar`
- Create: `testdata/scripts/config_set_get.txtar`
- Create: `testdata/scripts/version.txtar`

**Acceptance Criteria:**
- [ ] Script tests cover core workflows
- [ ] JSON output validated in script tests

#### Issue: Command alias and completion verification (8.3)

**Type:** task
**Priority:** P2
**Parent:** Phase 8
**Dependencies:** Task 7.6 (Implement remaining CLI commands)
**Description:**
Verify all aliases (`c`, `ls`) and shell completions.

**Acceptance Criteria:**
- [ ] `sd c myvm` = `sd connect myvm`
- [ ] `sd ls` = `sd list`
- [ ] Prefix matching: `sd cre` = `sd create`
- [ ] Ambiguous prefix `sd co` lists matching commands
- [ ] Completion scripts are syntactically valid

#### Issue: Final build and polish (8.4)

**Type:** task
**Priority:** P2
**Parent:** Phase 8
**Dependencies:** Task 8.1 (Integration test suite), Task 8.2 (Script test suite), Task 8.3 (Command alias and completion verification)
**Description:**
Ensure `go vet`, `golangci-lint`, `go mod tidy` all pass. Update go.sum. Verify all compile-time interface checks.

**Acceptance Criteria:**
- [ ] `go vet ./...` clean
- [ ] `golangci-lint run` clean
- [ ] `go mod tidy` produces no changes
- [ ] All interface compliance checks compile
- [ ] `make build` produces a working binary

---

## Dependencies

| Blocked Task | Blocked By | Reason |
|-------------|------------|--------|
| Task 1.4 (Viper-based loader) | Task 1.3 (Config types and defaults) | Loader depends on config type definitions |
| Task 2.2 (State package) | Task 1.3 (Config types and defaults) | State persists config.VMConfig type |
| Task 2.3 (Mount validation) | Task 1.3 (Config types and defaults) | Uses config types for sensitive_paths |
| Task 2.4 (Backend stubs) | Task 2.1 (Backend interface) | Stubs implement Backend interface |
| Task 3.1 (Lima YAML) | Task 2.1 (Backend interface) | Uses backend.VMConfig types |
| Task 3.2 (Lima lifecycle) | Task 3.1 (Lima YAML) | Create() depends on YAML generation |
| Task 3.3 (Lima snapshots) | Task 3.2 (Lima lifecycle) | Snapshot needs running backend |
| Task 4.2 (SSH config) | Task 2.1 (Backend interface) | Uses backend.SSHConfig type |
| Task 4.3 (Env var injection) | Task 1.3 (Config types and defaults) | Uses config.Resolve() |
| Task 5.1 (Root command) | Task 1.2 (UI package) | App struct uses Printer |
| Task 5.1 (Root command) | Task 1.4 (Viper loader) | PersistentPreRunE loads config |
| Task 5.1 (Root command) | Task 2.1 (Backend interface) | Backend resolution in App |
| Task 5.2 (Create command) | Task 5.1 (Root command) | CLI framework required |
| Task 5.2 (Create command) | Task 2.2 (State package) | State.Exists/Create calls |
| Task 5.2 (Create command) | Task 2.3 (Mount validation) | ValidateMountPath calls |
| Task 5.2 (Create command) | Task 3.2 (Lima lifecycle) | Backend.Create calls |
| Task 5.2 (Create command) | Task 4.1 (SSH key generation) | GenerateKeyPair before Create |
| Task 5.2 (Create command) | Task 4.2 (SSH config) | WriteSSHConfig after Create |
| Task 5.2 (Create command) | Task 5.5b (Provisioning executor) | sd create --modules requires provisioning executor |
| Task 5.3 (Destroy/start/stop) | Task 5.1 (Root command) | CLI framework required |
| Task 5.3 (Destroy/start/stop) | Task 2.2 (State package) | State.Delete calls |
| Task 5.3 (Destroy/start/stop) | Task 3.2 (Lima lifecycle) | Backend lifecycle calls |
| Task 5.3 (Destroy/start/stop) | Task 3.3 (Lima snapshots) | Auto-snapshot before destroy |
| Task 5.4 (List/status) | Task 5.1 (Root command) | CLI framework required |
| Task 5.4 (List/status) | Task 2.2 (State package) | State.List calls |
| Task 5.4 (List/status) | Task 3.2 (Lima lifecycle) | Backend.Status/List calls |
| Task 5.5a (Provisioning core) | Task 1.3 (Config types and defaults) | Uses config types |
| Task 5.5b (Provisioning executor) | Task 5.5a (Provisioning core) | Uses module types |
| Task 5.5b (Provisioning executor) | Task 2.1 (Backend interface) | Uses backend.Exec() |
| Task 5.5c (Built-in modules) | Task 5.5a (Provisioning core) | Module YAML format defined by core |
| Task 5.6 (Version command) | Task 5.1 (Root command) | CLI framework required |
| Task 6.1 (Connection manager) | Task 4.1 (SSH key generation) | Uses managed SSH keys |
| Task 6.1 (Connection manager) | Task 4.2 (SSH config) | Uses SSH config data |
| Task 6.1 (Connection manager) | Task 4.3 (Env var injection) | Uses resolved env vars |
| Task 6.2 (Connect CLI) | Task 5.1 (Root command) | CLI framework required |
| Task 6.2 (Connect CLI) | Task 6.1 (Connection manager) | Calls Connector.Connect() |
| Task 6.3 (Exec CLI) | Task 5.1 (Root command) | CLI framework required |
| Task 6.3 (Exec CLI) | Task 6.1 (Connection manager) | Uses connection infrastructure |
| Task 6.4 (File sync) | Task 4.2 (SSH config) | rsync uses managed SSH identity |
| Task 6.4 (File sync) | Task 5.1 (Root command) | CLI framework required |
| Task 6.5 (SSH config CLI) | Task 5.1 (Root command) | CLI framework required |
| Task 6.5 (SSH config CLI) | Task 4.2 (SSH config) | Reads SSH config data |
| Task 7.0 (Egress spike) | Task 3.2 (Lima lifecycle) | Needs running Lima VM |
| Task 7.1 (Egress control) | Task 7.0 (Egress spike) | Must validate approach first |
| Task 7.1 (Egress control) | Task 2.3 (Mount validation) | Uses security types |
| Task 7.3 (Credential management) | Task 1.3 (Config types and defaults) | Uses config.Resolve() |
| Task 7.3 (Credential management) | Task 2.2 (State package) | Reads VM config from state |
| Task 7.4 (Config CLI) | Task 5.1 (Root command) | CLI framework required |
| Task 7.4 (Config CLI) | Task 1.4 (Viper loader) | Uses config loader |
| Task 7.4 (Config CLI) | Task 7.1 (Egress control) | sd config egress commands require EgressController |
| Task 7.5 (Security CLI) | Task 5.1 (Root command) | CLI framework required |
| Task 7.5 (Security CLI) | Task 7.2 (Audit logging) | Queries audit log |
| Task 7.5 (Security CLI) | Task 7.3 (Credential management) | Uses credential injector |
| Task 7.6 (Remaining commands) | Task 5.1 (Root command) | CLI framework required |
| Task 7.6 (Remaining commands) | Task 5.5a (Provisioning core) | provision list uses modules |
| Task 7.6 (Remaining commands) | Task 5.5b (Provisioning executor) | provision command uses executor |
| Task 7.6 (Remaining commands) | Task 3.3 (Lima snapshots) | snapshot commands use snapshotter |
| Task 7.6 (Remaining commands) | Task 7.2 (Audit logging) | sd logs audit-log source requires AuditLogger |
| Task 7.7 (Reset command) | Task 5.1 (Root command) | CLI framework required |
| Task 7.7 (Reset command) | Task 3.3 (Lima snapshots) | Uses snapshot apply |
| Task 7.7 (Reset command) | Task 2.2 (State package) | Reads VM state |
| Task 8.1 (Integration tests) | Task 7.6 (Remaining commands) | Tests need all features |
| Task 8.2 (Script tests) | Task 7.6 (Remaining commands) | Tests need all features |
| Task 8.3 (Alias verification) | Task 7.6 (Remaining commands) | Tests need all commands |
| Task 8.4 (Final polish) | Task 8.1 (Integration tests) | Must pass tests first |
| Task 8.4 (Final polish) | Task 8.2 (Script tests) | Must pass tests first |
| Task 8.4 (Final polish) | Task 8.3 (Alias verification) | Must pass tests first |

**Reading this table:** each row means the "Blocked Task" cannot start until "Blocked By" completes. This matches `bd dep add` argument order: `bd dep add <blocked-task-id> <blocked-by-id>`.

## Coverage Matrix

| Plan Task | Bead Title | Sub-Epic |
|-----------|------------|----------|
| 1.1 Initialize Go module and entry point | Initialize Go module and entry point | Phase 1: Project Scaffolding & Core Types |
| 1.2 Implement `internal/ui/` package | Implement `internal/ui/` package | Phase 1: Project Scaffolding & Core Types |
| 1.3 Implement `internal/config/` types and defaults | Implement `internal/config/` types and defaults | Phase 1: Project Scaffolding & Core Types |
| 1.4 Implement `internal/config/` Viper-based loader | Implement `internal/config/` Viper-based loader | Phase 1: Project Scaffolding & Core Types |
| 2.1 Implement `internal/backend/` interface package | Implement `internal/backend/` interface package | Phase 2: Backend Interface, State Manager & Security Foundations |
| 2.2 Implement `internal/state/` package | Implement `internal/state/` package | Phase 2: Backend Interface, State Manager & Security Foundations |
| 2.3 Implement `internal/security/mount.go` | Implement `internal/security/mount.go` | Phase 2: Backend Interface, State Manager & Security Foundations |
| 2.4 Implement backend stubs (avf, docker, incus) | Implement backend stubs (avf, docker, incus) | Phase 2: Backend Interface, State Manager & Security Foundations |
| 3.1 Implement Lima YAML generation | Implement Lima YAML generation | Phase 3: Lima Backend Implementation |
| 3.2 Implement Lima backend lifecycle | Implement Lima backend lifecycle | Phase 3: Lima Backend Implementation |
| 3.3 Implement Lima snapshot support | Implement Lima snapshot support | Phase 3: Lima Backend Implementation |
| 4.1 Implement SSH key generation | Implement SSH key generation | Phase 4: Connection Infrastructure & SSH Management |
| 4.2 Implement SSH config management | Implement SSH config management | Phase 4: Connection Infrastructure & SSH Management |
| 4.3 Implement environment variable injection | Implement environment variable injection | Phase 4: Connection Infrastructure & SSH Management |
| 5.1 Implement root command and App struct | Implement root command and App struct | Phase 5: CLI Core & Provisioning Engine |
| 5.2 Implement `sd create` command | Implement `sd create` command | Phase 5: CLI Core & Provisioning Engine |
| 5.3 Implement `sd destroy`, `sd start`, `sd stop` commands | Implement `sd destroy`, `sd start`, `sd stop` commands | Phase 5: CLI Core & Provisioning Engine |
| 5.4 Implement `sd list` and `sd status` commands | Implement `sd list` and `sd status` commands | Phase 5: CLI Core & Provisioning Engine |
| 5.5a Implement provisioning engine core | Implement provisioning engine core | Phase 5: CLI Core & Provisioning Engine |
| 5.5b Implement provisioning executor and probes | Implement provisioning executor and probes | Phase 5: CLI Core & Provisioning Engine |
| 5.5c Implement built-in provisioning modules | Implement built-in provisioning modules | Phase 5: CLI Core & Provisioning Engine |
| 5.6 Implement `sd version` command | Implement `sd version` command | Phase 5: CLI Core & Provisioning Engine |
| 6.1 Implement connection manager | Implement connection manager | Phase 6: Connection & Interaction Commands |
| 6.2 Implement `sd connect` CLI command | Implement `sd connect` CLI command | Phase 6: Connection & Interaction Commands |
| 6.3 Implement `sd exec` CLI command | Implement `sd exec` CLI command | Phase 6: Connection & Interaction Commands |
| 6.4 Implement file sync | Implement file sync | Phase 6: Connection & Interaction Commands |
| 6.5 Implement `sd ssh-config` command | Implement `sd ssh-config` command | Phase 6: Connection & Interaction Commands |
| 7.0 Spike: Validate iptables/dnsmasq inside Lima VZ | Spike: Validate iptables/dnsmasq inside Lima VZ | Phase 7: Security Operations & Remaining Commands |
| 7.1 Implement egress control | Implement egress control | Phase 7: Security Operations & Remaining Commands |
| 7.2 Implement audit logging | Implement audit logging | Phase 7: Security Operations & Remaining Commands |
| 7.3 Implement credential management | Implement credential management | Phase 7: Security Operations & Remaining Commands |
| 7.4 Implement configuration CLI commands | Implement configuration CLI commands | Phase 7: Security Operations & Remaining Commands |
| 7.5 Implement security CLI commands | Implement security CLI commands | Phase 7: Security Operations & Remaining Commands |
| 7.6 Implement remaining CLI commands | Implement remaining CLI commands | Phase 7: Security Operations & Remaining Commands |
| 7.7 Implement `sd reset` command | Implement `sd reset` command | Phase 7: Security Operations & Remaining Commands |
| 8.1 Integration test suite | Integration test suite | Phase 8: Integration, Testing & Polish |
| 8.2 Script test suite | Script test suite | Phase 8: Integration, Testing & Polish |
| 8.3 Command alias and completion verification | Command alias and completion verification | Phase 8: Integration, Testing & Polish |
| 8.4 Final build and polish | Final build and polish | Phase 8: Integration, Testing & Polish |

**Plan tasks:** 36
**Beads mapped:** 36
**Coverage:** 100%

## Summary

- Feature epic: 1
- Sub-epics (phases): 8
- Issues (tasks): 36
- Blocker dependencies: 57
- Items ready immediately (no blockers): 6 (Tasks 1.1, 1.2, 1.3, 2.1, 4.1, 7.2)
