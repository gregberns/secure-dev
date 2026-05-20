# Wave 4 — Architect Review

**Branch:** `int-ralph-loop`
**Scope:** 28 modified files + 3 new (uncommitted) across 9 revival items (SEC-001/003, autosnapshot-restore/provision, init-idempotent, prune, created-at-zero, doctor-ssh-include, module-script-no-diag).
**Reviewer:** architect (API design, spec compliance, extensibility, cross-spec consistency).

---

## Must-fix (blocks commit)

### MF-1. `bug-doctor-ssh-include` is NOT implemented but claimed in brief

The brief lists item 7 (`bug-doctor-ssh-include` — "new `ssh_config_include` doctor check") as part of Wave 4. A repo-wide grep finds **zero** references to `ssh_config_include`, `ssh_include`, or `SSHConfigInclude` anywhere in `internal/`. No file in the diff touches `doctor`. This is either a missing implementation or a mis-labeled brief. Confirm scope: implement, drop from the wave manifest, or file a follow-up bead and remove from the merge claim.

### MF-2. `sd init` JSON envelope shape diverges between no-op and overwrite paths (CLI contract break)

`internal/cmd/init.go` (no-op branch, lines ~120-130) emits `{"status": "already_initialized", "path": ..., "action": "noop"}` while the normal/forced path emits `initResult{Path, Name, Modules, Repo}`. Same command, same `--json` flag, two unrelated `data` shapes — agents cannot write a single parser. Spec 002 REQ-002-003 mandates `--json` produce stable machine-parseable output. Either:

- Make the no-op envelope a superset of `initResult` (return `{path, name: "", modules: [], action: "noop"}`), OR
- Add a discriminator field `action` to both envelopes (so it is always present) and document both shapes in the spec.

Also: when `.sd.yaml` exists and `--force` is omitted, the prior contract was `exit 2 / file_exists`. Moving this to `exit 0` is a **breaking change** to spec 005 (`sd init` REQ-005-024). No spec edit accompanies the behavior change. Update specs/005-configuration.md REQ-005-024 acceptance criteria to document idempotency, or revert.

### MF-3. Auto-snapshot opt-out flag names are inconsistent across commands

Three commands now have opt-out flags for the same architectural pattern (REQ-004-019 auto-snapshot-before-destructive-op):

| Command              | Flag                       | Error code               |
|----------------------|----------------------------|--------------------------|
| `sd destroy`         | `--no-snapshot`            | `snapshot_failed`        |
| `sd snapshot restore`| `--no-backup-snapshot`     | `snapshot_failed`        |
| `sd provision`       | `--no-provision-snapshot`  | `provision_snapshot_failed` |

Three flag names. Two error codes. Spec 004 (REQ-004-019) acceptance criterion 5 explicitly names `--no-snapshot`. Either:

- Unify on `--no-snapshot` everywhere (recommended; matches spec text and AI-first principle), OR
- Update REQ-004-019 to enumerate all three flag names and justify per-command divergence in a spec rationale block.

The error code split (`provision_snapshot_failed` vs `snapshot_failed`) is also unmotivated — both fail the same precondition, both should surface identically.

### MF-4. `CreatedAt *time.Time` migration inconsistent with sibling timestamps

`internal/config/types.go`:
```go
type VMState struct {
    Status      string     `yaml:"status"`
    CreatedAt   *time.Time `yaml:"created_at,omitempty"`
    LastStarted time.Time  `yaml:"last_started,omitempty"`
    LastStopped time.Time  `yaml:"last_stopped,omitempty"`
}
```

Only `CreatedAt` was migrated to pointer. `LastStarted`/`LastStopped` retain `time.Time`. If the rationale for the pointer is "absence is distinguishable from zero," the rationale applies equally to the other two. Either migrate all three to `*time.Time` or document why only `CreatedAt` needed it. Without consistency, the next person to touch this struct will get it wrong.

Additionally, `backend.VMInfo.CreatedAt` JSON tag is **`json:"created_at"`** (no `omitempty`). A `*time.Time` with `nil` marshals to `"created_at": null` rather than being absent, which is a JSON-shape change vs. the prior `time.Time` zero-value behavior (which emitted `"0001-01-01T00:00:00Z"`). Add `omitempty` and add a `TestProperty_VMInfoNilCreatedAt_OmittedInJSON` regression test.

### MF-5. `create.go` mixes UTC and local-time timestamps in the same struct literal

`internal/cmd/create.go` (~ line 285):
```go
now := time.Now().UTC()
return config.VMState{
    Status:      config.VMStatusRunning,
    CreatedAt:   &now,           // UTC
    LastStarted: time.Now(),     // local
}
```

The bug-created-at-zero work standardized on UTC for `CreatedAt`. The adjacent `LastStarted` is local time. Pick one; mixing them within a struct literal will cause cross-VM timestamp comparisons to misbehave on hosts with non-UTC `TZ`. Same issue in `destroy.go` and `snapshot.go` audit logging: `Timestamp: time.Now()` (local) — recommend `time.Now().UTC()` across all audit-log call sites for hash-chain reproducibility.

---

## Should-fix (follow-up bead acceptable)

### SF-1. Audit event_type naming inconsistent

`internal/cmd/prune.go` emits `event_type: "vm.prune"` (dotted). `destroy.go` and `snapshot.go` emit `snapshot-create`, `snapshot-restore`, `snapshot-create-failed` (hyphenated). Spec 004 REQ-004-022 enumerates events as `create, start, stop, destroy, connect, disconnect, snapshot-create, snapshot-restore, token-rotate, token-revoke, config-change` — all hyphenated, no dots. Either rename to `vm-prune` or extend the spec enumeration.

`vm.prune` is also not in REQ-004-022's enumerated event list at all. Add it to the spec.

### SF-2. Duplicate orphan-detection logic in `list.go` and `prune.go`

`internal/cmd/list.go` (the `REQ-001-011` block) and `internal/cmd/prune.go::orphanCount` and `prune.go::findOrphans` all walk `$SD_HOME/vms` and intersect with backend.List. Three implementations. `list.go` should call into `orphanCount` (already exported for this purpose per its comment) — currently the comment is aspirational only. Either delete `orphanCount` or refactor `list.go` to use it.

### SF-3. `provision.log` path hardcoded, no rotation, no spec entry

`internal/cmd/provision.go::openProvisionLog` hardcodes `<sdHome>/vms/<name>/provision.log` with mode `0o644` and append-only. No rotation, no size cap, no `audit.log_path`-style configurability. Spec 006 (REQ-006-005) does not mention a per-VM provisioning log file at all — the spec talks about capturing stdout/stderr for diagnostic messages, not persisting them to a file. Either:

- Update spec 006 REQ-006-005 to specify the log file location, format, mode, and rotation policy, OR
- Make it configurable via `sd config set provision.log_path` and document the default.

Hard-coded path + no rotation is a long-term sharp edge.

### SF-4. `scriptLogTailLines = 50` is a package constant, not configurable

`internal/provision/provisioner.go` defines `const scriptLogTailLines = 50`. Reasonable default but worth either (a) a brief comment on why 50, or (b) plumbing through `Options.LogTailLines`. Minor — file for follow-up.

### SF-5. `provision.go` snapshot tag generator is per-command, not unified

`autoSnapshotTag` (destroy), `autoProvisionSnapshotTag` (provision), `preRestoreSnapshotTag` (snapshot restore) are three independent package-level vars, each generating a tag in a slightly different format (`pre-destroy-…`, `pre-provision-…`, `pre-restore-…`). Recommend a single `internal/backend.AutoSnapshotTag(op, name string) string` helper so the format stays uniform and the test override surface is one var.

### SF-6. `pluralIES` helper in `list.go` is over-engineered for one call site

Single use; inline the conditional. Trivial.

---

## Optional / nits

- N-1. `internal/backend/docker/docker.go` line 313: `var createdPtr *time.Time; if !createdAt.IsZero() { tUTC := createdAt.UTC(); createdPtr = &tUTC }` — readability suffers from single-line `if`. Standard format.
- N-2. `internal/cmd/create.go` uses an anonymous IIFE to build `VMState`:
  ```go
  State: func() config.VMState { now := time.Now().UTC(); return config.VMState{...} }(),
  ```
  Hoist `now` to a local before the struct literal. The IIFE adds noise.
- N-3. `internal/cmd/snapshot.go`: the audit-log block for backup-snapshot outcome is ~30 lines and structurally identical to the one in `destroy.go`. Extract a helper `logSnapshotOutcome(al, op, vm, tag string, err error)`.
- N-4. `docs/security-004-revival-audit.md` says REQ-004-007 is "DONE (SEC-001 fix)" but the fix is `DefaultModuleNames` membership only — the doc claim of "applied to default VMs" is correct but underspecified (allowlist live-update REQ-004-008 is still PARTIAL and they share infrastructure). Note the linkage.
- N-5. `internal/cmd/prune.go::pruneStdin *os.File` global — minor testability smell. Considered acceptable for `bug-no-prune-command` scope.
- N-6. The `// REQ-NNN-MMM` traceability comments are present in new code (good), but `prune.go` references only `bug-no-prune-command` (the bead ID) and never a `REQ-NNN-MMM`. There is no spec REQ for `sd prune`. Either add a requirement to spec 002 (CLI) for the new subcommand or call this out in the spec-gap section.

---

## Summary

- **Must-fix: 5** (missing implementation, JSON contract break, flag-name inconsistency, pointer-time inconsistency, UTC/local mixing)
- **Should-fix: 6**
- **Optional: 6**

Top concern: MF-1 (missing `bug-doctor-ssh-include`) needs immediate scope clarification before this branch can claim "9 revival items complete." MF-2 and MF-3 are the user-facing contract regressions; MF-4/MF-5 are internal consistency that will compound debt if not fixed now.

No spec edits in the diff materially change the security contract — REQ-004-006/007/025 doc audit update is correct. But REQ-005-024 (idempotent init) and REQ-004-019 (flag names) need spec edits to legitimize the code changes.
