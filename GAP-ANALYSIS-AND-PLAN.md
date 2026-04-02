# Spec/Implementation Gap Analysis & Resolution Plan (v2)

*Updated after architect, critic, and security reviews. Changes marked with [R1] for first review.*

## Gap Inventory

### CRITICAL — Blocks core functionality or security promises

#### G01: Readiness Probe Execution Missing
**Specs**: REQ-006-008  
**Status**: MISSING — Module YAML files define probes (command, interval, timeout) but `Provision()` in `internal/provision/provisioner.go` never executes them.  
**Impact**: Provisioning can silently fail. A module's scripts may complete but the tool isn't usable.  
**Files**: `internal/provision/provisioner.go`, `internal/provision/module.go`

#### G02: VM Definition Files Incomplete
**Specs**: REQ-005-007, REQ-005-015, REQ-001-006 step 6  
**Status**: PARTIAL — `$SD_HOME/vms/<name>/config.yaml` is written by token/egress commands for partial env/egress data, but the full VM definition (CPUs, memory, disk, image, backend, provisions, mounts, network mode) is never persisted. The `state` section is not written.  
**Impact**: Cannot re-provision, cannot display accurate `sd status`, cannot implement VM config inheritance.  
**Complexity note** [R1]: Must consolidate existing partial writers (`defaultWriteVMEnv` in token.go, `defaultWriteVMEgress` in config_egress.go) into a single write path. Must include `backend_meta` section. Must handle backward compatibility with existing partial config files.  
**Files**: `internal/cmd/create.go`, `internal/config/types.go`, `internal/config/loader.go`, `internal/cmd/token.go`, `internal/cmd/config_egress.go`

#### G15: Audit Logging Not Wired Into Commands [R1]
**Specs**: REQ-004-021, REQ-004-022  
**Status**: MISSING — `AuditLogger` exists with `LogCommand()` and `LogEvent()` methods, but NO command handler calls them. Zero invocations of `sd` are logged. The audit system is completely inoperative despite the infrastructure existing.  
**Impact**: `sd audit` always returns empty. The entire hash-chain audit trail — a core security promise — is non-functional.  
**Files**: `internal/security/audit.go` (infrastructure), `internal/cmd/root.go` (needs PersistentPreRunE/PostRunE hooks), all command files

#### G16: Audit Logger Hash Chain Broken on Restart [R1]
**Specs**: REQ-004-022  
**Status**: BUG — `NewAuditLogger()` initializes `lastHash` to `GenesisHash` on every invocation. Since each `sd` CLI invocation creates a new process (and new AuditLogger), the second entry's `prev_hash` will be `GenesisHash` instead of the hash of the first entry. The chain is broken after the very first invocation.  
**Impact**: `sd audit --verify` will always report a broken chain after the first command. Undermines tamper detection.  
**Files**: `internal/security/audit.go` — `NewAuditLogger()` must read last line of existing log to recover chain tip

#### G17: Provisioning Checksum Injection Not Wired [R1]
**Specs**: REQ-004-028  
**Status**: BUG — Module YAML files have `checksums` fields and scripts reference `${GO_CHECKSUM}`, `${GH_CHECKSUM}` etc., but the provisioner never maps checksum entries to environment variables injected into scripts. These variables are always empty, making checksum verification a no-op. Additionally, `claude-code.yaml` runs `npm install -g @anthropic-ai/claude-code` with no version pin or checksum. `docker.yaml` downloads from `download.docker.com` (not on egress allowlist).  
**Impact**: Supply-chain attack surface. Checksums are a false promise — the mechanism exists but doesn't work.  
**Files**: `internal/provision/provisioner.go`, `internal/provision/modules/*.yaml`

### IMPORTANT — Spec compliance & security hardening

#### G03: SSH Config Fragment Removal on Destroy [R1 — reclassified]
**Specs**: REQ-007-004  
**Status**: PARTIAL — `WriteFragment()` and `RemoveFragment()` exist in `internal/ssh/ssh.go`. `create.go` calls `WriteFragment()`. But `destroy.go` never calls `RemoveFragment()`, leaving orphaned SSH config fragments in `~/.ssh/config.d/`. Also, `NeedsInclude()` exists but is never called to warn users about the missing `Include` directive.  
**Impact**: Orphaned SSH configs accumulate after VM destruction. Users may connect to wrong hosts.  
**Files**: `internal/cmd/destroy.go`, `internal/cmd/create.go`

#### G04: Future Backend Stubs Missing
**Specs**: REQ-001-005, REQ-003-020  
**Status**: MISSING — No AVF, Docker, or Incus backend stubs.  
**Files**: Need new `internal/backend/avf/`, `internal/backend/docker/`, `internal/backend/incus/`

#### G05: Custom Provisioning Module Support Missing
**Specs**: REQ-006-007  
**Status**: MISSING — Only built-in modules loaded via embedded FS.  
**Files**: `internal/provision/module.go`

#### G06: CLAUDE.md/AGENTS.md Propagation Missing
**Specs**: REQ-006-013  
**Status**: MISSING — No implementation.  
**Security note** [R1]: ELEVATED RISK. A malicious CLAUDE.md in a cloned repo is an instruction injection vector for AI agents. Must NOT auto-propagate untrusted project CLAUDE.md. Only propagate sd's own instructions. Consider `--trust-project-config` flag or user review step.  
**Files**: `internal/provision/modules/claude-code.yaml` (add to existing module per spec, not separate module)

#### G07: Sync Watch Mode Not Implemented
**Specs**: REQ-007-018  
**Status**: STUB — `--watch` flag declared but never read.  
**Complexity note** [R1]: fsnotify doesn't recursively watch new subdirectories. Large git checkouts cause debounce storms. Need robust error recovery in watch loop.  
**Files**: `internal/cmd/sync.go`

#### G08: Lima VZ virtiofs Mount Type Not Set
**Specs**: REQ-003-016  
**Status**: PARTIAL — `vmType: "vz"` set, `mountType: "virtiofs"` missing.  
**Files**: `internal/backend/lima/lima.go`

#### G09: DNS Periodic Re-Resolution Missing
**Specs**: REQ-004-010  
**Status**: MISSING  
**Security notes** [R1]: Re-resolution script must be root-owned (0700, `/usr/local/sbin/`) since VM user (agent) is the threat actor. Must use flush-and-replace (not append) for iptables rules. Brief overlap period for old+new IPs to avoid dropping established connections.  
**Files**: `internal/provision/modules/egress.yaml`

#### G10: Base Image Map Incomplete
**Specs**: REQ-003-024  
**Status**: PARTIAL — arm64 only, no x86_64.  
**Files**: `internal/backend/lima/lima.go`

#### G18: `sd start` Violates Idempotency Spec [R1]
**Specs**: REQ-003-003  
**Status**: BUG — `start.go` returns `vm_already_running` error when VM is already running. Spec says `Start` on a running VM is a no-op (no error).  
**Files**: `internal/cmd/start.go`

#### G19: `sd destroy` Does Not Clean VM State Directory [R1]
**Specs**: REQ-001-008 step 6  
**Status**: BUG — `destroy.go` removes `$SD_HOME/vms/<name>/ssh/` but not the parent `$SD_HOME/vms/<name>/` directory, leaving behind config.yaml and other files.  
**Files**: `internal/cmd/destroy.go`

#### G20: No ip6tables Egress Rules [R1]
**Specs**: REQ-004-009  
**Status**: MISSING — `egress.yaml` only configures iptables (IPv4). If VM has IPv6, all egress controls are bypassed.  
**Files**: `internal/provision/modules/egress.yaml`

#### G21: Guest VM Environment Missing `~/.sd/bin/` and `~/projects/` [R1]
**Specs**: REQ-001-013  
**Status**: MISSING — Base module installs packages but does not create `~/.sd/bin/`, add it to `$PATH`, or create `~/projects/`.  
**Files**: `internal/provision/modules/base.yaml`

#### G22: `--dry-run` on Create Not Implemented [R1]
**Specs**: REQ-001-006  
**Status**: MISSING — Spec says `--dry-run` causes steps 4-6 to be skipped and resolved VMConfig printed. No flag defined.  
**Files**: `internal/cmd/create.go`

#### G23: `--modules all` Not Handled [R1]
**Specs**: REQ-006-015  
**Status**: MISSING — No handling for the special value `all` in `--modules` flag.  
**Files**: `internal/cmd/create.go`, `internal/cmd/provision.go`

#### G24: SD_HOME Permission Enforcement Missing [R1]
**Specs**: REQ-005-016  
**Status**: MISSING — No verification that `$SD_HOME` (0700) and config files (0600) have correct permissions. Credentials stored in VM config could be exposed via wrong permissions. Also, `viper.WriteConfigAs` creates files with default umask permissions, then `os.Chmod` runs after — a TOCTOU race.  
**Files**: `internal/config/loader.go`, `internal/cmd/token.go`

### LOW — Polish items

#### G11: GitHub Bot Account Config
**Specs**: REQ-004-016 (SHOULD, not MUST)  
**Status**: MISSING  
**Files**: `internal/config/types.go`

#### G12: Package Layout Divergence
**Specs**: REQ-001-014  
**Status**: DIVERGENT — Code works, spec needs updating.  
**Files**: `specs/001-architecture.md`

#### G13: Provisioning Progress Reporting
**Specs**: REQ-006-009  
**Status**: PARTIAL — In-memory state exists, not queryable via `sd status` during provisioning.  
**Note** [R1]: During provisioning, state is in-memory (`ProvisionState`). Only final outcome should be persisted to VM config after completion.  
**Files**: `internal/cmd/status.go`, `internal/provision/provisioner.go`

#### G14: tmux Default Configuration Provisioning
**Specs**: REQ-007-011  
**Status**: MISSING  
**Files**: `internal/provision/modules/base.yaml`

#### G25: Orphaned State Detection Missing [R1]
**Specs**: REQ-001-011  
**Status**: MISSING — Neither `sd list` nor `sd status` detects orphaned state (metadata exists but backend has no matching VM).  
**Files**: `internal/cmd/list.go`, `internal/cmd/status.go`

#### G26: `sd doctor` Missing SSH Fragment Checks [R1]
**Specs**: REQ-004-027  
**Status**: MISSING — Doctor doesn't inspect SSH config fragments for `ForwardAgent no` / `ForwardX11 no`.  
**Files**: `internal/cmd/doctor.go`

#### G27: GitHub Branch Protection Guidance [R1]
**Specs**: REQ-004-017 (SHOULD)  
**Status**: MISSING — `sd token github setup` doesn't recommend branch protection.  
**Files**: `internal/cmd/token.go`

---

## Resolution Plan

### Phase 0: Security Foundations [R1 — new phase]
*These fix broken security promises. Must complete before any feature work.*

#### Task 0.1: Fix Audit Logger Hash Chain Continuity (G16)
`NewAuditLogger()` must read the last line of the existing log file on initialization to recover the hash chain tip. If the log doesn't exist or is empty, use `GenesisHash`.

**Files to modify**:
- `internal/security/audit.go` — add `recoverLastHash()` called from `NewAuditLogger()`

**Tests**: Create logger with existing log, verify chain continuity. Create with empty/missing log, verify genesis. Property test: N sequential loggers produce valid chain.

#### Task 0.2: Wire Audit Logging Into All Commands (G15)
Add audit logging hooks to the CLI. `PersistentPreRunE` creates an `AuditLogger` and records the start. `PersistentPostRunE` records the command completion (exit code, duration). Lifecycle commands (`create`, `destroy`, `start`, `stop`, `connect`, `token rotate`, `token revoke`) additionally call `LogEvent()` with VM-specific metadata.

**Files to modify**:
- `internal/cmd/root.go` — add AuditLogger to persistent state, add PostRunE hook
- `internal/cmd/create.go` — add `LogEvent("vm.create", ...)`
- `internal/cmd/destroy.go` — add `LogEvent("vm.destroy", ...)`
- `internal/cmd/start.go` — add `LogEvent("vm.start", ...)`
- `internal/cmd/stop.go` — add `LogEvent("vm.stop", ...)`
- `internal/cmd/connect.go` — add `LogEvent("vm.connect", ...)`
- `internal/cmd/token.go` — add `LogEvent("token.rotate", ...)`, `LogEvent("token.revoke", ...)`

**Tests**: Integration test: run command, verify audit log entry exists with valid hash. Verify secret redaction in args.

**Dependency**: Task 0.1 (hash chain must work first)

#### Task 0.3: Wire Provisioning Checksum Injection (G17)
The provisioner must map module `checksums` entries to environment variables injected into script execution. For each checksum entry `key: hash`, export `CHECKSUM_<NORMALIZED_KEY>=hash` before running scripts. Pin `claude-code.yaml` to a specific npm version. Add `download.docker.com` to docker module docs/egress notes.

**Files to modify**:
- `internal/provision/provisioner.go` — inject checksum env vars before script execution
- `internal/provision/modules/claude-code.yaml` — pin npm package version
- `internal/provision/modules/golang.yaml` — update to use `CHECKSUM_*` vars
- `internal/provision/modules/github-cli.yaml` — update to use `CHECKSUM_*` vars

**Tests**: Unit test: checksums are injected as env vars. Module scripts reference correct var names.

### Phase 1: Critical Infrastructure

#### Task 1.1: Implement VM Definition File Persistence (G02)
Write full VM config to `$SD_HOME/vms/<name>/config.yaml` during `sd create`. Consolidate `defaultWriteVMEnv` (token.go) and `defaultWriteVMEgress` (config_egress.go) into a centralized `WriteVMConfig()` in the config loader. Separate credentials into `$SD_HOME/vms/<name>/credentials.yaml` (0600) [R1 security review]. Include `backend_meta` section. Add `state` section. Handle backward compatibility with existing partial config files (missing fields → use defaults).

Use `os.OpenFile` with explicit 0600 mode instead of write-then-chmod to avoid TOCTOU race [R1].

**Files to modify**:
- `internal/config/types.go` — ensure full VMConfig struct with YAML tags, add CredentialConfig type
- `internal/config/loader.go` — add `WriteVMConfig()`, `ReadVMConfig()`, `WriteCredentials()`, `ReadCredentials()`, use `os.OpenFile` with 0600
- `internal/cmd/create.go` — persist full config after creation
- `internal/cmd/start.go` — update `state.last_started`
- `internal/cmd/stop.go` — update `state.last_stopped`
- `internal/cmd/destroy.go` — remove full `$SD_HOME/vms/<name>/` directory (G19)
- `internal/cmd/token.go` — migrate to centralized write path for credentials
- `internal/cmd/config_egress.go` — migrate to centralized write path

**Tests**: Write/read round-trip. State transitions. Backward compat with partial files. Concurrent access (file locking). Credential separation. TOCTOU race prevention.

**Dependency**: Task 0.1, 0.2 (audit logging should be in place to log lifecycle events during testing)

#### Task 1.2: Implement Readiness Probe Execution (G01)
Add `ExecuteProbes()` to provisioner. After each module's scripts complete, run its probe with retry (interval, timeout). Fail the module if probe times out. Persist final provisioning outcome to VM config (requires Task 1.1).

**Files to modify**:
- `internal/provision/provisioner.go` — add probe execution loop after script execution per module
- `internal/provision/module.go` — add probe helper methods

**Tests**: Mock exec: probe passes first try, probe passes after retries, probe times out, module with no probe skips.

**Dependency**: Task 1.1 (for persisting probe results)

### Phase 2: Security Hardening

#### Task 2.1: DNS Periodic Re-Resolution (G09)
Install a root-owned (0700) re-resolution script at `/usr/local/sbin/sd-egress-refresh` that:
1. Re-resolves all allowlisted domains
2. Builds new iptables rule set
3. Atomically swaps the sd-egress chain (flush + re-add with brief overlap for established connections)
4. Logs changes

Add a systemd timer (or cron) running every 5 minutes as root.

**Files to modify**:
- `internal/provision/modules/egress.yaml` — add re-resolution script and timer

**Tests**: Module YAML validation. Script is root-owned. Timer is installed. Verify flush-and-replace logic doesn't drop default-deny.

#### Task 2.2: Lima VZ virtiofs Mount Type (G08)
Set `mountType: "virtiofs"` when `vmType: "vz"` on Apple Silicon.

**Files to modify**:
- `internal/backend/lima/lima.go` — add to YAML generation

**Tests**: Update existing YAML generation tests.

#### Task 2.3: Add ip6tables Egress Rules (G20) [R1]
Mirror all iptables rules with ip6tables equivalents in the egress module. If IPv6 is not needed, add blanket `ip6tables -A OUTPUT -j DROP`.

**Files to modify**:
- `internal/provision/modules/egress.yaml` — add ip6tables rules

**Tests**: Module validation. Verify ip6tables chain exists.

#### Task 2.4: SD_HOME Permission Enforcement (G24) [R1]
On startup, verify `$SD_HOME` is 0700. On file writes, use `os.OpenFile` with explicit mode. Add `sd doctor` check for permissions.

**Files to modify**:
- `internal/config/loader.go` — add permission checks on Load()
- `internal/cmd/doctor.go` — add SD_HOME permission check

**Tests**: Permission verification. Warning on wrong mode.

### Phase 3: Feature Completeness

#### Task 3.1: SSH Config Fragment Cleanup on Destroy (G03) [R1 — rescoped]
Wire `ssh.RemoveFragment()` into `destroy.go`. Call `ssh.NeedsInclude()` during `create` and warn if `~/.ssh/config` lacks `Include config.d/sd-*` (use `sd-*` prefix scope, not `*`, per security review).

**Files to modify**:
- `internal/cmd/destroy.go` — call `ssh.RemoveFragment()`
- `internal/cmd/create.go` — call `ssh.NeedsInclude()` and warn

**Tests**: Fragment removed after destroy. Warning emitted when Include missing.

#### Task 3.2: Custom Provisioning Module Support (G05)
Add `LoadUserModules(sdHome)` that reads YAML from `$SD_HOME/provisions/`. Merge with built-in modules, error on name conflicts. Update all call sites (provision.go, create.go).

**Files to modify**:
- `internal/provision/module.go` — add `LoadUserModules()`, update `LoadBuiltinModules()` to `LoadAllModules()`
- `internal/cmd/provision.go` — update var override and calls
- `internal/cmd/create.go` — update calls

**Tests**: Load user modules, name conflict, empty dir, invalid YAML handling.

#### Task 3.3: Sync Watch Mode (G07)
Implement using `fsnotify`. Debounce at 500ms. Handle recursive directory watching. Error recovery (failed rsync → log and continue). Error on `--watch` with `sync from`.

**Files to modify**:
- `go.mod` — add `github.com/fsnotify/fsnotify`
- `internal/cmd/sync.go` — implement watch loop, validate `--watch` only on `sync to`

**Tests**: Debounce logic. Error recovery. `--watch` with `sync from` returns error code 2.

#### Task 3.4: Future Backend Stubs (G04)
Create stubs returning `ErrNotImplemented`. `Available()` returns descriptive "not yet implemented" error so `sd doctor` can distinguish from "not installed".

**Files to create**:
- `internal/backend/avf/avf.go`
- `internal/backend/docker/docker.go`
- `internal/backend/incus/incus.go`

**Tests**: All methods return ErrNotImplemented. Registration works. Doctor handles gracefully.

#### Task 3.5: CLAUDE.md Propagation (G06) [R1 — security-aware design]
Add logic to `claude-code.yaml` module (not a separate module) that writes sd-specific agent instructions to `~/.claude/CLAUDE.md` inside the VM. Do NOT auto-propagate untrusted project CLAUDE.md from cloned repos — the project's own CLAUDE.md is preserved as-is when repos are cloned, but sd only appends its own operational instructions (SSH, credential handling, tmux usage) to a separate location.

**Files to modify**:
- `internal/provision/modules/claude-code.yaml` — add script to write sd agent instructions

**Tests**: sd instructions written. Existing project CLAUDE.md in cloned repo untouched.

#### Task 3.6: Base Image Map Completion (G10)
Add x86_64/amd64 entries for ubuntu:24.04, ubuntu:22.04, debian:12.

**Files to modify**:
- `internal/backend/lima/lima.go` — expand imageMap with amd64 URLs

**Tests**: Resolution for both architectures.

#### Task 3.7: Guest VM Environment Setup (G21) [R1]
Add to base module: create `~/.sd/bin/`, add to `$PATH` via `~/.profile`, create `~/projects/`.

**Files to modify**:
- `internal/provision/modules/base.yaml` — add user-mode script

**Tests**: Directories exist. PATH includes `~/.sd/bin/`.

#### Task 3.8: `--dry-run` on Create (G22) [R1]
Add `--dry-run` flag to `sd create`. When set, resolve VMConfig and print it (JSON or human), skip actual creation.

**Files to modify**:
- `internal/cmd/create.go` — add flag, short-circuit before backend.Create()

**Tests**: Dry run outputs config. No VM created. Works with --json.

#### Task 3.9: `--modules all` Support (G23) [R1]
Handle `all` as special value for `--modules` flag in create and provision commands.

**Files to modify**:
- `internal/cmd/create.go` — handle "all" in module parsing
- `internal/cmd/provision.go` — handle "all" in module parsing

**Tests**: `--modules all` provisions every available module.

#### Task 3.10: Fix `sd start` Idempotency (G18) [R1]
Change `start.go` to return success (no error) when VM is already running, per REQ-003-003.

**Files to modify**:
- `internal/cmd/start.go` — change error to success with message

**Tests**: Start on running VM succeeds. Start on stopped VM starts. Start on non-existent VM errors.

### Phase 4: Polish & Spec Alignment

#### Task 4.1: tmux Default Configuration (G14)
Add tmux config to base module: mouse on, Ctrl-a prefix, VM name in status bar.

**Files to modify**:
- `internal/provision/modules/base.yaml` — add tmux config script

**Tests**: Config file written with expected content.

#### Task 4.2: Provisioning Progress Reporting (G13)
Persist final provisioning outcome (per-module status) to VM config. `sd status` reads and displays it.

**Files to modify**:
- `internal/provision/provisioner.go` — persist final state to disk
- `internal/cmd/status.go` — read and display

**Dependency**: Task 1.1

**Tests**: Completed provision state visible in status.

#### Task 4.3: Orphaned State Detection (G25) [R1]
`sd list` and `sd status` check for orphaned VM state (metadata exists, backend has no matching VM). Report as warning.

**Files to modify**:
- `internal/cmd/list.go` — cross-reference backend list with SD_HOME
- `internal/cmd/status.go` — cross-reference

**Tests**: Orphaned state detected and warned.

#### Task 4.4: Doctor SSH Fragment Checks (G26) [R1]
`sd doctor` inspects SSH config fragments for required security directives.

**Files to modify**:
- `internal/cmd/doctor.go` — add SSH fragment check

**Tests**: Missing ForwardAgent/ForwardX11 detected.

#### Task 4.5: GitHub Bot Account Config (G11)
Add `github.bot_account` to config types (SHOULD, not MUST).

**Files to modify**:
- `internal/config/types.go`
- `internal/cmd/token.go`

**Tests**: Config parsing.

#### Task 4.6: GitHub Branch Protection Guidance (G27) [R1]
Add branch protection recommendations to `sd token github setup` output.

**Files to modify**:
- `internal/cmd/token.go` — add guidance text

**Tests**: Output includes recommendations.

#### Task 4.7: Spec Update for Package Layout (G12)
Update spec 001 REQ-001-014 to match actual package layout.

**Files to modify**:
- `specs/001-architecture.md`

**Tests**: N/A

---

## Execution Order & Dependencies

```
Phase 0 (Security Foundations) — MUST COMPLETE FIRST:
  0.1 Fix Audit Hash Chain ──→ 0.2 Wire Audit Logging ──→ (all other phases)
  0.3 Checksum Injection ────────────────────────────────→ (independent of 0.1/0.2)

Phase 1 (Critical Infrastructure):
  1.1 VM Config Persistence ──→ 1.2 Readiness Probes
  (1.1 depends on 0.2 being complete so lifecycle events are audited)

Phase 2 (Security Hardening) — can run parallel with Phase 1:
  2.1 DNS Re-Resolution    (independent)
  2.2 VZ virtiofs          (independent)
  2.3 ip6tables            (independent, can parallel with 2.1)
  2.4 SD_HOME Permissions  (independent)

Phase 3 (Features) — after Phase 1:
  3.1 SSH Fragment Cleanup  (no dep on 1.1)
  3.2 Custom Modules        (independent)
  3.3 Sync Watch Mode       (independent)
  3.4 Backend Stubs         (independent)
  3.5 CLAUDE.md             (independent)
  3.6 Base Image Map        (independent)
  3.7 Guest VM Env          (independent)
  3.8 --dry-run             (independent)
  3.9 --modules all         (independent)
  3.10 Start Idempotency    (independent)

Phase 4 (Polish) — after Phase 1, 3:
  4.1 tmux Config           (independent)
  4.2 Provision Progress    (depends on 1.1)
  4.3 Orphaned State        (depends on 1.1)
  4.4 Doctor SSH Checks     (independent)
  4.5 Bot Account Config    (independent)
  4.6 Branch Protection     (independent)
  4.7 Spec Update           (LAST — after all changes stabilize)
```

## Risk Assessment

| Task | Risk | Mitigation |
|------|------|------------|
| 0.1 Audit Hash Chain | Low — isolated fix | Read last line, compute hash. Tested easily. |
| 0.2 Audit Wiring | Medium — touches every command | Use PersistentPreRunE/PostRunE hooks for command logging. Per-command event logging is additive. |
| 0.3 Checksums | Medium — changes script env | Env var naming convention must match scripts. Update all module YAMLs. |
| 1.1 VM Config | High — consolidates competing writers, touches all lifecycle commands | Atomic writes (temp file + rename). File locking for concurrent access. Backward compat: handle missing fields gracefully. Separate credentials from config. |
| 1.2 Probes | Low — additive to provisioner | Timeout prevents hanging. |
| 2.1 DNS Re-Resolution | Medium — cron/systemd in guest | Root-owned script. Flush-and-replace with overlap. |
| 2.3 ip6tables | Low — mirrors existing rules | Can default to blanket DROP if IPv6 not needed. |
| 3.3 Sync Watch | Medium — new dependency | fsnotify well-tested. Debounce + error recovery. |
| 3.5 CLAUDE.md | Medium — instruction injection risk | Only write sd's own instructions. Never auto-propagate untrusted project files. |
