# Spec 004 (Security Model) — Revival Audit

| Field | Value |
|-------|-------|
| Source spec | `specs/004-security.md` |
| Audit date  | 2026-05-20 |
| Branch      | `int-ralph-loop` |
| Auditor     | claude (delegated) |

This document inventories every REQ-004-NNN requirement against the current implementation, lists concrete gap-closure issues, and proposes a `kerf` work to drive the remediation.

## 1. Requirement classification

Status legend:
- DONE — implemented and referenced from code; tests exist
- PARTIAL — primary mechanism implemented but acceptance criteria not fully covered (or wiring incomplete)
- MISSING — no implementation found, or stubbed-only

| Req | Title (short) | Status | Evidence / Gap |
|-----|---------------|--------|----------------|
| REQ-004-001 | Threat model documentation | PARTIAL | Documented in spec, but `sd create --help` / `sd help security` do not surface a security model summary |
| REQ-004-002 | VM isolation (no host execution) | DONE | All paths go through `backend.Backend`; no host-execution mode exists |
| REQ-004-003 | No default mounts | DONE | `internal/cmd/create.go` defaults to empty mounts; surfaced in status JSON |
| REQ-004-004 | Read-only mounts by default | PARTIAL | Mount mode parsing exists, but `--mount host:guest:rw` syntax and ro/rw exposure in `sd status --json` need verification (not seen in `gatherMounts`) |
| REQ-004-005 | Mount path validation | DONE | `internal/security/mount.go` + `sensitive_paths_test.go`; symlink resolution covered |
| REQ-004-006 | Default-deny egress | PARTIAL | `modules/egress.yaml` exists, but module is NOT in `DefaultModuleNames` — opt-in only. Default VM is NOT default-deny |
| REQ-004-007 | Default egress allowlist | PARTIAL | `DefaultEgressAllowlist` defined; allowlist not applied to default VMs (see REQ-004-006). Wildcard matching unit-tested |
| REQ-004-008 | User-configurable egress | PARTIAL | `--allow-egress` flag accepted at create; `sd config egress add/remove/list` exists. Live update without restart unverified (egress module only re-runs at provision time) |
| REQ-004-009 | Egress via iptables | PARTIAL | Implemented in `modules/egress.yaml` but not fired by default; SSH-from-host rule present; integration test absent |
| REQ-004-010 | DNS-based allowlisting (re-resolution) | PARTIAL | `sd-egress-refresh` systemd timer exists in `modules/egress.yaml` (`OnBootSec=5min`); not validated end-to-end; interval not user-configurable |
| REQ-004-011 | Credential injection via SendEnv | PARTIAL | `ssh -o SendEnv=...` is set in `connect.go`; sshd `AcceptEnv` configured in `ssh-hardening` module. But **credentials are persisted in `$SD_HOME/vms/<name>/credentials.yaml`** (`config/loader.go` `WriteCredentials`), violating "MUST NOT be written to files" on host. Spec strictly forbids file persistence in the VM, not the host — but combined with REQ-004-013 the host-side storage path is fragile |
| REQ-004-012 | GitHub token scoping warnings | DONE | `security.ValidateToken` detects `ghp_`; called from `token` command |
| REQ-004-013 | Anthropic API key injection | PARTIAL | Reads from `credentials.yaml` (or host env) and injects via SendEnv. No host keychain integration (OQ-1 explicitly out of scope but file storage with mode 0600 is the current mitigation). VM-side filesystem grep test not present |
| REQ-004-014 | Per-VM SSH keys | DONE | `internal/ssh/GenerateKeys`; cleaned up on destroy |
| REQ-004-015 | Token rotate/revoke | DONE | `sd token rotate/revoke/list` implemented in `token.go` |
| REQ-004-016 | GitHub bot/service account | MISSING | No `config set github.bot_account`; no `GIT_AUTHOR_*` injection (tests explicitly exclude `GIT_AUTHOR_NAME`) |
| REQ-004-017 | Protected branch integration | MISSING | `sd token github setup` does not mention branch protection; no doc page |
| REQ-004-018 | CI workflow change detection | PARTIAL | `sd diff` lists CI patterns (`.github/workflows/`, `Jenkinsfile`, `.circleci/`, `.git/hooks/`), but JSON warning flagging not confirmed; tracked-and-untracked coverage unverified |
| REQ-004-019 | Snapshot before destructive ops | PARTIAL | `sd destroy` creates a safety snapshot; `--no-snapshot` flag present. BUT failure is non-fatal (proceeds with destroy) — spec mandates FATAL ("destructive operation MUST NOT proceed"). No `sd reset` command exists |
| REQ-004-020 | Manual snapshots | PARTIAL | `sd snapshot create/list/restore/delete` exists, uses `--tag` not `--label`; `size` field unverified |
| REQ-004-021 | Audit log: command logging | PARTIAL | Implemented in `internal/security/audit.go` + wired in `root.go`. Redaction by prefix match present. `audit.log_path` configurability via `sd config set` unverified |
| REQ-004-022 | Audit: VM lifecycle + hash chain | PARTIAL | Hash chain implemented and verified; `sd audit --verify` exists. **Coverage gap**: `snapshot-create`, `snapshot-restore`, `config-change` events not logged (no `LogEvent` calls in `snapshot.go`, `config.go`, `config_egress.go`). `disconnect` not logged |
| REQ-004-023 | Sensitive path list configurable | PARTIAL | `MergedSensitivePaths` exists; `sd config set security.sensitive_paths` wired via viper; `sd config get ... --json` source-tagging unverified |
| REQ-004-024 | Security posture summary | DONE | `sd security status <name>` implemented; writable mount warnings present |
| REQ-004-025 | Local filtering DNS resolver | PARTIAL | `modules/dns-filter.yaml` installs dnsmasq with allowlist forwarders; but module is opt-in (not in `DefaultModuleNames`). NXDOMAIN behavior + `address=/#/` catch-all need integration test |
| REQ-004-026 | SSH port-forwarding restrictions | DONE | `modules/ssh-hardening.yaml` configures `AllowTcpForwarding local`, `GatewayPorts no`, `PermitTunnel no`, `X11Forwarding no`; tested in `modules_test.go` |
| REQ-004-027 | Agent / X11 forwarding disabled | DONE | `ForwardAgent=no`, `ForwardX11=no` in `connect.go`; SSH config fragment generation in `internal/ssh/` |
| REQ-004-028 | Download checksum verification | PARTIAL | Module schema supports `checksums`; `ValidateChecksums` enforces presence when downloads exist. Checksums are embedded in inline shell (`sha256sum -c -`) rather than schema-driven; no automatic enforcement separate from the script; `sd doctor` warning for user-defined modules without checksums unverified |
| REQ-004-029 | Project-level config security boundary | DONE | `internal/config/types.go` lists ignored keys; loader strips `security.*` from project layer; `sd doctor` warning unverified |
| REQ-004-030 | Git credential cache prevention | MISSING | No code sets `credential.helper ""` inside the VM; no provisioning step removes `~/.git-credentials`; `sd doctor` check absent |
| REQ-004-031 | SSH host key verification (TCP) | DONE | `scanAndVerifyHostKey` in `connect.go`; `internal/ssh/hostkey.go` + tests; cleanup on destroy |

### Summary

| Status | Count |
|--------|-------|
| DONE     | 9 |
| PARTIAL  | 19 |
| MISSING  | 3 |
| **Total** | **31** |

## 2. Issues to file

Bead-style issues. Priority scale: P0 (security-critical, blocks default-secure posture) / P1 (closes spec gap) / P2 (polish).

### SEC-001 — Wire `egress` + `dns-filter` modules into default provisioning (P0)

**Description.** `internal/provision/module.go` defines `DefaultModuleNames = ["base", "ssh-hardening"]`. The egress and DNS-filter modules are opt-in. As a result REQ-004-006 (default-deny) is not satisfied for a default `sd create <name>` — newly created VMs can talk to anything on the internet. Spec language is "MUST" / "default".

**Acceptance criteria.**
- `DefaultModuleNames` includes `dns-filter` and `egress` (or a documented `network-hardening` umbrella).
- A flag `--no-network-hardening` (or equivalent) explicitly opts out, recorded in audit log.
- Integration test: create a default VM, `curl http://example.com` returns connect-refused / DNS NXDOMAIN.
- Integration test: each domain in `DefaultEgressAllowlist` is reachable from a default VM.
- `sd security status` reflects "default-deny: enabled".

**Dependencies.** None. **Priority.** P0.

### SEC-002 — Runtime credential path: keychain-backed read, transient host file at most (P0)

**Description.** Spec REQ-004-013 says the Anthropic key MUST come from "host's environment, keychain, or `sd` config — not from a file inside the VM". Today, `config.WriteCredentials` persists `GITHUB_TOKEN` and `ANTHROPIC_API_KEY` to `$SD_HOME/vms/<name>/credentials.yaml` in plaintext (mode 0600). This is fragile: host backup software, syncthing, dotfile managers, and `find ~ -name "*.yaml"` all read it. Spec OQ-1 explicitly calls out keychain integration as a follow-up — promote it.

**Acceptance criteria.**
- On macOS, credentials default to `security`-backed keychain (`/usr/bin/security add-generic-password -s "sd-<vmname>"`).
- On Linux, default to `secret-tool` / libsecret with fallback to existing file behavior.
- `sd token rotate` writes to keychain; `sd token list` reads from keychain; `sd connect` reads from keychain for SendEnv injection (still no file on host or guest).
- Existing `credentials.yaml` migrated automatically on first use, with backup `credentials.yaml.bak` then `credentials.yaml` deleted.
- `sd destroy` removes both keychain entry and any leftover file.
- Test: after revoke, no value retrievable via either path.

**Dependencies.** None. **Priority.** P0.

### SEC-003 — Snapshot failure must abort destructive operations (P1)

**Description.** Spec REQ-004-019 / error-handling section: "Snapshot Creation Failure — Severity: Fatal (the destructive operation MUST NOT proceed)". Current `internal/cmd/destroy.go` lines 111–115 logs a warning and proceeds. This is a silent downgrade from "safe by default" to "lose data on disk pressure".

**Acceptance criteria.**
- `destroy.go` returns `ui.CLIError{Code: "snapshot_failed", ...}` and exits 1 when `SnapshotCreate` fails, unless `--no-snapshot` is set.
- JSON output matches the spec error shape: `{"error":"snapshot_failed", "reason":"insufficient_disk_space", "operation":"destroy", "aborted":true}`.
- New `sd reset <name>` command (or extend an existing one) wired to the same pre-snapshot path.
- Unit test with a backend stub returning ErrSnapshotFailed.

**Dependencies.** None. **Priority.** P1.

### SEC-004 — Audit-log coverage: snapshot, config, disconnect events (P1)

**Description.** REQ-004-022 enumerates events that MUST be logged: `snapshot-create`, `snapshot-restore`, `config-change`, `disconnect`. Search confirms `LogEvent` is absent in `snapshot.go`, `config.go`, and `config_egress.go`. No "disconnect" hook exists in `connect.go` (only "connect").

**Acceptance criteria.**
- `runSnapshotCreate`, `runSnapshotRestore`, `runSnapshotDelete` emit `LogEvent` with `event` = `snapshot.create` / `snapshot.restore` / `snapshot.delete`, metadata includes `tag`.
- `runConfigSet` (and `config egress add/remove`) emit `event` = `config.change` with key path (no value if marked sensitive).
- `runConnect` emits `event` = `disconnect` when the SSH process returns (deferred LogEvent), with duration metadata.
- All `LogEvent` calls feed the hash chain (existing).
- Test asserts presence of each event after invocation.

**Dependencies.** None. **Priority.** P1.

### SEC-005 — Live egress allowlist updates without VM restart (P1)

**Description.** REQ-004-008: "Adding an egress rule post-creation updates the VM's firewall rules without requiring a restart." Today `sd config egress add` updates host config but does not push iptables/dnsmasq changes into the running VM.

**Acceptance criteria.**
- `sd config egress add <vm> <domain>` invokes a backend Exec path that:
  - appends `server=/<domain>/<upstream>` to `/etc/dnsmasq.d/sd-egress.conf`, reloads dnsmasq.
  - resolves and appends an ACCEPT rule to the `sd-egress` chain (before the trailing DROP).
- Symmetric behavior for `remove`.
- `sd security status --json` reflects the new domain immediately.
- Integration test: add domain, curl from VM succeeds without `sd restart`.

**Dependencies.** SEC-001. **Priority.** P1.

### SEC-006 — Doctor + CLI surfacing of the security model (P2)

**Description.** REQ-004-001 says the threat model is referenced in CLI help. REQ-004-027/029/030 each include "`sd doctor` warns…" criteria; verify or implement.

**Acceptance criteria.**
- `sd help security` page (Cobra long help on a `security` group or `sd security help`) summarizes the threat model + layered defenses.
- `sd create --help` includes a one-paragraph "Default security posture" stanza.
- `sd doctor` adds checks: `~/.git-credentials` presence (REQ-004-030); SSH fragment missing `ForwardAgent no` (REQ-004-027); project `.sd/config.yaml` containing `security.*` keys (REQ-004-029).
- Each warning printed both text and JSON.

**Dependencies.** SEC-007 (for git-credentials). **Priority.** P2.

### SEC-007 — Git credential cache prevention inside VM (P1)

**Description.** REQ-004-030 is fully MISSING. Spec mandates clearing `credential.helper` to empty string and removing any pre-existing `~/.git-credentials`.

**Acceptance criteria.**
- New provisioning step (extend `base` or new `git-hardening` module) runs as `dev` user:
  - `git config --global --unset-all credential.helper` then `git config --global credential.helper ""`.
  - If `~/.git-credentials` exists, delete it and log a warning event.
- `sd doctor` (or `sd security status`) flags `~/.git-credentials` if it re-appears.
- Test: provision a VM with a pre-seeded `~/.git-credentials`, verify it is removed.

**Dependencies.** None. **Priority.** P1.

### SEC-008 — Schema-driven checksum verification for downloads (P1)

**Description.** REQ-004-028 wants the module YAML schema to drive verification, not inline shell. Today checksums are duplicated: once in the `checksums:` schema field, once as a hard-coded `echo "... sha256sum -c -"` line in the `script:`. Drift between the two is silent.

**Acceptance criteria.**
- Module loader emits the verification step automatically from the `checksums:` field. The hand-rolled `sha256sum -c -` lines in `golang.yaml`, `github-cli.yaml`, `rust.yaml`, `claude-code.yaml`, `codex.yaml`, `gemini-cli.yaml` are removed.
- Mismatch aborts provisioning with the spec-defined error (`checksum_mismatch`).
- `sd doctor` warns on user-defined modules with `downloads:` but no `checksums:`.
- Property test: corrupt one byte of a downloaded archive, expect fatal.

**Dependencies.** None. **Priority.** P1.

### SEC-009 — `sd reset <name>` command (P2)

**Description.** REQ-004-019 mentions `sd reset` as a destructive operation that MUST pre-snapshot. No such command exists.

**Acceptance criteria.**
- `sd reset <name>` stops, reverts to the most-recent or named snapshot, and restarts.
- Pre-snapshot before reset (same code path as destroy).
- Audit-logged.
- Documented in `--help`.

**Dependencies.** SEC-003. **Priority.** P2.

### SEC-010 — GitHub bot identity wiring (P2)

**Description.** REQ-004-016 (SHOULD). `config set github.bot_account` and `GIT_AUTHOR_*` env injection are not implemented.

**Acceptance criteria.**
- Config key `github.bot_account` accepted and stored.
- `sd connect` injects `GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME`, `GIT_COMMITTER_EMAIL` (sourced from config) via SendEnv.
- sshd `AcceptEnv` extended to include `GIT_*`.
- Test asserts env propagation into the VM session.

**Dependencies.** None. **Priority.** P2.

### SEC-011 — Branch-protection guidance in `sd token github setup` (P2)

**Description.** REQ-004-017. `sd token github setup` output currently lists scopes only; spec wants explicit branch-protection guidance and a docs page.

**Acceptance criteria.**
- `sd token github setup` text and JSON include a `branch_protection_recommendations` section.
- New doc `docs/security/github-setup.md` covering recommended branch-protection settings.

**Dependencies.** None. **Priority.** P2.

### SEC-012 — Audit log path configurability + sink for write failures (P2)

**Description.** REQ-004-021 ACs include `sd config set audit.log_path <path>` and a graceful warning on write failure. Verify the path-set route end-to-end and the write-failure stderr warning.

**Acceptance criteria.**
- `sd config set audit.log_path /tmp/foo.log` is honored on next invocation.
- Permission-denied on the log file emits the spec-defined JSON warning and continues.
- Test using a chmod-stripped temp file.

**Dependencies.** None. **Priority.** P2.

### SEC-013 — CI/hook diff JSON warning field (P2)

**Description.** REQ-004-018 wants changed CI/hook files surfaced with a structured warning, including untracked files. Current `sd diff` enumerates patterns but the JSON output's warning shape needs to match the spec.

**Acceptance criteria.**
- `sd diff <name> --json` returns `{ "ci_changes": [...], "warnings": [{"code":"ci_file_changed", ...}] }`.
- Coverage includes untracked files (status `??`).
- Text output prefixes such lines with `WARNING:`.

**Dependencies.** None. **Priority.** P2.

### Issue total: 13.

## 3. kerf work proposal

Copy/paste body for the human to feed into `kerf new`:

- **Suggested codename.** `sec-revival` (or `sd-sec-default`).
- **Jig type.** `spec` — this is feature work that closes spec gaps across many subsystems; bug jig is too narrow.
- **Problem statement.** Spec 004 (Security Model) defines a default-deny, layered-defense posture for VMs running AI agents with bypass permissions. The current implementation lands the scaffolding (sensitive-path validation, hash-chained audit log, ssh-hardening module, host-key verification) but leaves the user-observable default posture insecure: the egress and DNS-filter modules are opt-in; credentials are persisted to a plaintext host file; safety snapshots fail open; audit coverage is patchy; and key spec mandates (git credential cache prevention, branch protection, schema-driven checksums, live egress updates) are absent. Result: a default `sd create foo` produces a VM that does NOT enforce the spec's primary mitigations.
- **Decomposition outline.**
  1. **Default-deny baseline** — SEC-001 (default modules), SEC-005 (live updates), SEC-007 (git credential helper). Shared theme: the default VM matches the spec's "Mitigations Summary" table.
  2. **Fail-safe destructive ops** — SEC-003 (snapshot abort), SEC-009 (`sd reset`). Theme: spec-defined fatal errors.
  3. **Credential hardening** — SEC-002 (keychain), SEC-010 (bot identity), SEC-011 (branch protection guidance).
  4. **Audit + observability** — SEC-004 (event coverage), SEC-012 (log path config), SEC-013 (CI diff JSON warnings), SEC-006 (doctor + help).
  5. **Supply chain** — SEC-008 (schema-driven checksum verification).
- **Pass plan.**
  - *Problem space.* Confirm the audit, lock the issue list, decide on keychain library (suggest `github.com/zalando/go-keyring`).
  - *Decomposition.* Group above into 5 implementation slices; each becomes a bd epic.
  - *Research.* dnsmasq live-reload semantics; macOS keychain access without prompt loops; lima exec semantics for live iptables updates.
  - *Detailed spec.* Update `specs/004-security.md` revision history with the resolved interpretations (e.g., snapshot-failure-is-fatal codified).
  - *Integration.* Where `sd create` plumbs into provisioning module selection; where `sd config egress` plumbs into backend exec.
  - *Tasks.* One bead per acceptance criterion above; mark P0 issues as blockers on `sec-revival`.

## 4. Notes for next agent

- The most-misleading current behavior is REQ-004-006/007: defaults look implemented because the modules exist and are tested, but they are not in `DefaultModuleNames`. Anyone reading test coverage alone would conclude the spec is honored.
- `credentials.yaml` is a real host-side spec-intent miss; treat SEC-002 as load-bearing for any external review of the security posture.
- The hash-chain audit log is good — preserve it; SEC-004 only adds events, no schema changes.
- Several "PARTIAL" classifications above (REQ-004-018, REQ-004-021, REQ-004-023, REQ-004-029) may resolve to DONE after deeper test inspection; verify before filing as separate work.
