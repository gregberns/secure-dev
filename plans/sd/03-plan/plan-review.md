# Plan Review: sd

**Generated:** 2026-03-28
**Reviewers:** Architect, Critic, Spec Alignment
**Resolution:** All P0 and P1 findings resolved (plan updated). P2 findings documented for implementer reference.

---

## Summary

| Category | P0 | P1 | P2 | Total |
|----------|----|----|----|----|
| Structural Issues | 1 | 3 | 3 | 7 |
| Gaps & Risks | 3 | 9 | 4 | 16 |
| Coverage Issues | 1 | 2 | 2 | 5 |
| Over-Engineering | 0 | 1 | 3 | 4 |
| Consistency Issues | 0 | 2 | 2 | 4 |
| **Total** | **5** | **17** | **14** | **36** |

---

## P0 Findings (Must Fix)

### 1. SSH Host Key Capture at VM Creation is Unplanned [CORROBORATED]
- **Category:** Gap
- **Found by:** Architect (P1) + Critic (P0) — independently identified
- **What:** REQ-007-004 requires `StrictHostKeyChecking yes` with host key captured at VM creation time. The plan creates the SSH config fragment (task 4.2) referencing `known_hosts`, but no task captures the host key (e.g., via `ssh-keyscan`) during `sd create`. First `sd connect` will fail with "Host key verification failed."
- **Action:** Update plan
- **Recommendation:** Add a step to Phase 3 `Create()` flow (task 3.2) or the end of `sd create` (task 5.2) that runs `ssh-keyscan` after VM start and writes to `$SD_HOME/vms/<name>/ssh/known_hosts`. Alternatively use `limactl show-ssh` host key fingerprint.

### 2. EgressController Hidden Backend Dependency [CORROBORATED]
- **Category:** Structural
- **Found by:** Architect (P0) + Critic (P1, under-specification)
- **What:** `EgressController.ApplyAllowlist()` and related methods must execute commands inside a running VM (iptables, dnsmasq). The plan says these use `backend.Exec()` (task 7.1), but `security/` is defined as not importing `backend/`. The interface accepts only `vmName string` with no execution pathway to the VM.
- **Action:** Update plan
- **Recommendation:** Option A (cleanest): Security generates scripts as pure functions returning `ProvisionScript`; the provisioner/CLI layer invokes via `backend.Exec()`. Option B: Pass a `func(ctx, cmd []string) (ExecResult, error)` adapter to EgressController at construction. Option C: Accept that security imports backend and update dependency rules.

### 3. sshd AcceptEnv Provisioning is Missing [CORROBORATED]
- **Category:** Gap
- **Found by:** Critic (P0) + Alignment (P1, REQ-004-026 implicit)
- **What:** REQ-004-011 and REQ-007-019 require the VM's sshd to be configured with `AcceptEnv SD_* ANTHROPIC_* GITHUB_* GH_*`. The plan covers the SSH client-side `SendEnv` (task 4.2) but has no task for the sshd drop-in file inside the VM. Without server-side `AcceptEnv`, credential injection silently fails — env vars are sent but rejected by sshd, with no error.
- **Action:** Update plan
- **Recommendation:** Add explicit provisioning step to the `base` module: write `/etc/ssh/sshd_config.d/sd-acceptenv.conf` and reload sshd. Also add SSH port forwarding restrictions (`AllowTcpForwarding local`, `GatewayPorts no`, `PermitTunnel no`, `X11Forwarding no`) per REQ-004-026 to the same sshd config step.

### 4. Git Credential Helper Setup is Unplanned
- **Category:** Gap
- **Found by:** Critic (P0)
- **What:** REQ-006-012 requires provisioning to configure git with a credential helper reading from an injected env var, clear pre-existing helpers, and verify `~/.git-credentials` does not exist. No task describes the credential helper script content, `credential.helper ""` reset, or `user.name`/`user.email` from host git config. Git operations inside the VM either fail or use insecure storage — this is the primary AI agent workflow.
- **Action:** Update plan
- **Recommendation:** Expand task 5.5 to explicitly plan git configuration content inside the `claude-code` module: credential helper script, git config commands, environment variable reading pattern.

### 5. REQ-007-020 Multiple Concurrent Connections Dropped
- **Category:** Coverage
- **Found by:** Alignment (P0)
- **What:** The spec requires multiple concurrent `sd connect` calls each attach to a separate tmux window within the same session. Plan task 6.1 uses `tmux new-session -A` (attach-or-create same session) but does not address the multi-window concurrent scenario. REQ-007-020 is also absent from the spec coverage matrix entirely.
- **Action:** Update plan
- **Recommendation:** Add explicit handling in task 6.1: if a session already exists, create a new window with `tmux new-window -t sd-<name>`. Update coverage matrix to include REQ-007-020.

---

## P1 Findings (Should Fix)

### 6. Phase 5 App Struct Has Nil Phase-7 Fields [CORROBORATED]
- **Category:** Structural
- **Found by:** Architect (P1) + Critic (risk)
- **What:** The `App` struct (task 5.1) includes `Egress`, `Creds`, and `Audit` fields implemented in Phase 7. During Phase 5/6 development, these are nil. `sd create` calls `app.Audit.LogEvent()` (design step 9), causing nil pointer dereference.
- **Action:** Update plan
- **Recommendation:** Define no-op stubs (`noopAuditLogger`, `noopEgressController`, `noopCredentialInjector`) in Phase 5 task 5.1. Replace with real implementations in Phase 7.

### 7. `sd reset` Command Missing from Plan [CORROBORATED]
- **Category:** Gap
- **Found by:** Critic (P1) + Alignment (coverage matrix inconsistency)
- **What:** REQ-004-019 explicitly names `sd reset` as a destructive operation requiring auto-snapshot. Neither the command list, file manifest, nor coverage matrix includes it. The matrix incorrectly claims full REQ-004-019 coverage via task 5.3 (destroy only).
- **Action:** Update plan
- **Recommendation:** Add `sd reset` as a task in Phase 7 or explicitly defer with justification. Update coverage matrix.

### 8. Task 5.5 Oversized and Module Content Undefined [CORROBORATED]
- **Category:** Structural
- **Found by:** Architect (P1, feasibility) + Critic (P1, under-specification)
- **What:** Task 5.5 combines topo sort, cycle detection, embedded module loader, script executor, probe poller, AND seven YAML module files — 7+ files of non-trivial logic. Additionally, module YAML content has zero guidance: implementers of `golang.yaml`, `rust.yaml`, `docker.yaml` (rootless Docker is complex) have no version pins, script strategies, or probe commands specified.
- **Action:** Update plan
- **Recommendation:** Split 5.5 into sub-tasks: (a) module types, YAML loader, embedded files; (b) resolver (topo sort + cycles); (c) executor + probe wiring. Add minimum content guidance per module: version pins, install strategy, probe command, system vs user mode.

### 9. Provisioning Progress Reporting + Streaming Exec Gap
- **Category:** Gap
- **Found by:** Critic (P1 gap + H-likelihood risk)
- **What:** REQ-006-009 requires `sd status` to show per-module provisioning state during provisioning. No task describes how `ProvisionState` is written, stored, or queried. Additionally, `backend.Exec()` has no streaming output support — a non-streaming exec that returns only after completion cannot report per-module status during execution.
- **Action:** Update plan
- **Recommendation:** Add task describing `ProvisionState` persistence (e.g., `$SD_HOME/vms/<name>/provision-state.yaml`), how `sd status` reads it, and whether `Exec()` needs a streaming variant or if state is written between module executions.

### 10. `sd destroy` Auto-Snapshot Error Handling Undefined
- **Category:** Structural
- **Found by:** Architect (P1)
- **What:** The destroy flow calls `SnapshotCreate` before destroying. If snapshot fails (non-APFS, insufficient disk), the plan doesn't specify whether destroy aborts or proceeds. REQ-004-019 says "auto-snapshot before destructive ops" but doesn't define failure semantics.
- **Action:** Update plan
- **Recommendation:** In task 5.3: if auto-snapshot fails and `--force` is set, warn and proceed; if `--force` is not set, abort requiring user to use `--force --no-snapshot`.

### 11. `sd destroy` Missing SSH Key Cleanup
- **Category:** Gap
- **Found by:** Critic (P1)
- **What:** REQ-004-014 requires destroy to remove VM-specific SSH key pair. Task 5.3 covers state cleanup and SSH config removal but doesn't mention deleting `$SD_HOME/vms/<name>/ssh/` (private key files).
- **Action:** Update plan
- **Recommendation:** Add key file deletion to destroy sequence in task 5.3.

### 12. `sd logs` Has No Implementation Detail
- **Category:** Gap
- **Found by:** Critic (P1)
- **What:** Task 7.6 creates `logs.go` with `sd logs --tail --follow` but defines no log source, no streaming architecture for `--follow`, and no mapping of `--tail N`.
- **Action:** Update plan
- **Recommendation:** Define log sources (Lima journal, provisioning output, sd audit log), `--tail` mapping, and `--follow` mechanism (exec streaming command or tail audit log).

### 13. `sd doctor` Network Connectivity Check Missing
- **Category:** Gap
- **Found by:** Critic (P1)
- **What:** REQ-002-007 requires `sd doctor` to check network connectivity to required endpoints. Task 7.6 omits this entirely — only checks limactl, ssh, tmux, rsync, config validity, SD_HOME writable.
- **Action:** Update plan
- **Recommendation:** Add network connectivity checks (probe default egress allowlist endpoints) to task 7.6.

### 14. `sd token rotate` Semantics Contradict Plan vs Spec
- **Category:** Gap
- **Found by:** Critic (P1)
- **What:** Task 7.3 says `Rotate()` "updates the `${VAR}` reference" (change which env var). REQ-004-015 says "accepts new token values and updates stored configuration." The security model says never store literal values, so these interpretations conflict.
- **Action:** Update plan (resolve ambiguity)
- **Recommendation:** State explicitly that `rotate` updates which env var name the `${VAR}` reference points to (e.g., `GITHUB_TOKEN` -> `GITHUB_TOKEN_V2`), not the token value itself. Update spec if needed.

### 15. Egress Implementation Mechanism Underspecified
- **Category:** Structural
- **Found by:** Critic (P1)
- **What:** Task 7.1 says "Generates iptables rules... scripts executed via backend.Exec()" but doesn't specify: (a) when scripts run — provisioning module or separate create step? (b) how dnsmasq stays running — systemd service? (c) how `AddDomain` updates iptables live post-creation?
- **Action:** Update plan
- **Recommendation:** Describe installation path (provisioning module), service management (systemd unit), and the iptables update mechanism for dynamic domain addition.

### 16. SSH Connection Manager VSOCK Construction Underspecified
- **Category:** Structural
- **Found by:** Critic (P1)
- **What:** Connection manager uses `-F /dev/null` to ignore user SSH config, but VSOCK ProxyCommand lives in the SSH config fragment. If `-F /dev/null` is used, the ProxyCommand is never applied. How does the connection manager use VSOCK without reading the fragment?
- **Action:** Update plan
- **Recommendation:** Choose one approach: either build ProxyCommand into the SSH command line directly (duplicating fragment logic), or use a selective `-F <path>` pointing to the sd-specific fragment.

### 17. iptables/dnsmasq Egress Needs Spike Task
- **Category:** Gap (Risk)
- **Found by:** Critic (High likelihood, High impact risk)
- **What:** Lima + VZ userspace network stack may not support iptables. The plan's risk section mentions a spike but Phase 7 has no spike task defined.
- **Action:** Update plan
- **Recommendation:** Add an explicit spike task at the start of Phase 7 (or end of Phase 3) to verify iptables works inside Lima VZ before building the full egress system.

### 18. Concurrent `sd` Invocations May Corrupt State
- **Category:** Gap (Risk)
- **Found by:** Critic (M-likelihood, H-impact risk)
- **What:** `state.Manager` has no file locking. Two concurrent `sd create` operations could race on the state directory.
- **Action:** Update plan
- **Recommendation:** Add file locking requirement to task 2.2 (state manager). Consider `flock`-based advisory locking on `$SD_HOME/vms/<name>/config.yaml`.

### 19. Threat Model Summary in `sd create --help` Uncovered
- **Category:** Coverage
- **Found by:** Alignment (P1)
- **What:** REQ-004-001 requires the threat model summary in `sd create --help` text. Plan only assigns this to `security status` (task 7.5), missing the create help text coverage.
- **Action:** Update plan
- **Recommendation:** Add a note to task 5.2 or 7.5 to include threat model summary in `sd create --help` long description.

### 20. Classic PAT Detection Wiring Unclear
- **Category:** Coverage
- **Found by:** Alignment (P1)
- **What:** REQ-004-012 requires warning when a classic PAT (`ghp_` prefix) is used and recommending fine-grained tokens. `ValidateToken` exists in task 2.3 but the wiring to `sd token github setup --json` output for recommended scopes is not explicit.
- **Action:** Update plan
- **Recommendation:** Add explicit wiring note in task 7.5 for classic PAT detection → warning display.

### 21. SSH Key Injection Sequencing Not Specified
- **Category:** Gap (Risk)
- **Found by:** Critic (H-likelihood, H-impact risk)
- **What:** Task 4.1 generates keys, task 5.2 creates the VM, but there's no explicit plan for injecting the public key into `authorized_keys` inside the VM. Lima's cloud-init handles this only if the key is in the Lima YAML at creation time — requiring key gen before `Backend.Create()`. The sequencing is not specified.
- **Action:** Update plan
- **Recommendation:** Explicitly state in the `sd create` flow (task 5.2) that key generation (4.1) must complete before Lima YAML generation (3.1), and the public key path is included in the Lima YAML.

### 22. `PersistentPreRunE` Skip Mechanism for `sd version`
- **Category:** Gap (Risk)
- **Found by:** Critic (M-likelihood, M-impact risk)
- **What:** Task 5.6 notes `sd version` must work without config, but the mechanism for skipping `PersistentPreRunE` (which loads config and constructs the App struct) for specific commands is not described.
- **Action:** Update plan
- **Recommendation:** Note the mechanism in task 5.1: command annotation or name-based check in `PersistentPreRunE` to skip for `version`, `completion`, and `help`.

---

## P2 Findings (Consider)

### 23. ExecResult Duplication Inconsistency
- **Category:** Consistency
- **Found by:** Critic (P1 — downgraded to P2 as implementation decision)
- **What:** Architecture decision table says accept separate `ExecResult` types to avoid circular import. But `connection` already imports `backend` for `SSHConfig`, so no circular import exists. The second type is unnecessary.
- **Recommendation:** Remove duplicate; use `backend.ExecResult` in connection.

### 24. `state.Manager` types.go Inconsistency
- **Category:** Consistency
- **Found by:** Critic (P1 — downgraded to P2 as minor manifest issue)
- **What:** Task 2.2 says state persists `config.VMConfig`, but design-context.md lists `state/types.go` with `VMState, VMMetadata` structs. If the persisted type is `config.VMConfig`, what does types.go contain?
- **Recommendation:** Remove `types.go` from state manifest or clarify its contents.

### 25. Duplicate ErrVMNotFound Sentinels
- **Category:** Over-Engineering
- **Found by:** Critic (P1 — downgraded to P2 as minor design choice)
- **What:** `state.ErrVMNotFound` and `backend.ErrVMNotFound` are separate sentinels for the same concept. CLI must handle both with no meaningful distinction.
- **Recommendation:** Have `state` wrap `backend.ErrVMNotFound` rather than defining its own.

### 26. `connection` -> `security` Coupling for Env Var Names
- **Category:** Structural
- **Found by:** Architect (P2)
- **What:** `connection` imports `security` only for credential env var name patterns (`SD_* ANTHROPIC_* GITHUB_* GH_*`). This is a thin, arguably unstable coupling.
- **Recommendation:** Move pattern constants to `config/defaults.go` or `connection/env.go` directly.

### 27. VMConfig Dual-Type Cognitive Burden
- **Category:** Structural
- **Found by:** Architect (P2)
- **What:** `backend.VMConfig` and `config.VMConfig` are distinct types with the same name. Import aliases work but create cognitive burden.
- **Recommendation:** Watch during implementation. Consider renaming one (e.g., `backend.CreateParams`).

### 28. EgressController Interface Missing Context Parameters
- **Category:** Structural
- **Found by:** Architect (P2)
- **What:** `EgressController` methods don't accept `context.Context`, unlike all backend methods. Egress operations may involve VM-side execution.
- **Recommendation:** Add `ctx context.Context` to all `EgressController` methods. Same for `Connector.SSHConfigFor`.

### 29. Backend Stubs Front-Loaded Unnecessarily
- **Category:** Over-Engineering
- **Found by:** Critic (P2)
- **What:** Plan builds avf/docker/incus backend stubs in Phase 2 before any command exists. These stubs add 3 files (~60 lines each) with zero return.
- **Recommendation:** Move backend stubs to Phase 5 or later.

### 30. `resolver.go` and `probe.go` as Separate Files
- **Category:** Over-Engineering
- **Found by:** Critic (P2)
- **What:** Topo sort (~40-60 lines) and probe polling (~30-50 lines) each get dedicated files in provisioning. Both are simple algorithms.
- **Recommendation:** Implement inline in `provision.go` unless they exceed ~80 lines each.

### 31. `CLAUDE.md`/`AGENTS.md` Propagation Not in Module Description
- **Category:** Gap
- **Found by:** Critic (P2)
- **What:** REQ-006-013 requires the claude-code module to detect and preserve `CLAUDE.md`/`AGENTS.md` in cloned repos. Not mentioned in task 5.5 module descriptions.
- **Recommendation:** Add note to task 5.5 under claude-code.yaml content.

### 32. `--modules all` Flag Not Mentioned
- **Category:** Gap
- **Found by:** Critic (P2)
- **What:** REQ-006-015 requires `--modules all` to provision every available module. Task 5.2 lists `--modules` but not the special `all` value.
- **Recommendation:** Add `--modules all` to accepted values in task 5.2.

### 33. `sd sync from --diff` Exit Code Semantics Missing
- **Category:** Gap
- **Found by:** Critic (P2)
- **What:** REQ-007-017 specifies exit 0 for no differences, exit 1 for differences. Task 6.4 mentions `--diff` but not these distinct exit codes.
- **Recommendation:** Note distinct exit codes in task 6.4.

### 34. `sd connect --json` Session-End Logic Mismatch
- **Category:** Consistency
- **Found by:** Critic (P2)
- **What:** Task 6.2 says "after session ends: log duration, audit event" but `--json` exits immediately without opening a session. Session-end logic should apply to interactive mode only.
- **Recommendation:** Note in task 6.2 that session-end logic is interactive-mode only.

### 35. `sd create` Data Flow Step Ordering
- **Category:** Gap
- **Found by:** Architect (P2)
- **What:** Duplicate-name check (`State.Exists()`) happens after mount validation and VMConfig construction. Should be second check (after `ValidateVMName`).
- **Recommendation:** Move `State.Exists()` to step 2 in create flow.

### 36. Script Test Suite Too Small
- **Category:** Over-Engineering (inverse — under-testing)
- **Found by:** Critic (P2)
- **What:** Six txtar files for the entire CLI (~20 commands). No tests for connect, exec, sync, ssh-config, snapshot, config, provision, doctor, or security commands.
- **Recommendation:** Expand script test list or clarify these are minimum required with organic expansion expected.

---

## Reviewer Summaries

**Architect:**
- Architecture quality: Good — module boundaries clean, dependency graph acyclic
- Phase ordering: Minor issues — hidden dependencies in Phases 4, 5, 7
- Key concern: EgressController has no path to execute commands inside VMs (structural flaw in security/backend boundary)

**Critic:**
- Gaps found: 14 (3 P0, 7 P1, 4 P2)
- Risks identified: 9 (iptables feasibility, streaming exec, state corruption highest)
- Key concern: Three critical provisioning gaps (sshd AcceptEnv, SSH host keys, git credential helper) that will cause silent failures in the core security model

**Spec Alignment:**
- Forward coverage: 86/94 fully covered, 6 partial, 2 missing
- Reverse traceability: 0 scope creep items — every task traces to spec
- Key concern: REQ-007-020 (concurrent connections) entirely absent from plan and coverage matrix
