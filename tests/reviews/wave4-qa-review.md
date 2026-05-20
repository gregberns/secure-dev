# Wave 4 — QA Review

Branch: `int-ralph-loop`. Reviewer: QA. Date: 2026-05-20.

## Scope

28 modified files + 2 new (`internal/cmd/prune.go`, `internal/cmd/prune_test.go`,
`internal/config/created_at_test.go`). Wave covers ~9 revival issues:
SEC-001 default-deny modules, bug-init-not-idempotent, bug-no-prune-command,
bug-created-at-zero, REQ-004-019 (destroy/provision/restore snapshot fatality
+ audit), REQ-006-005 (provision log + stderr surfacing).

## Test count (added/changed this wave)

| Area | Added | Notes |
|------|-------|-------|
| `internal/cmd/prune_test.go` (new) | 5 | dry-run, removal, no-orphans, JSON, register |
| `internal/config/created_at_test.go` (new) | 3 | mtime migration, preserve existing, roundtrip |
| `internal/cmd/destroy_test.go` | rewrite of 2 + property inversion | snapshot fatality, --no-snapshot bypass |
| `internal/cmd/init_test.go` | +2 + property update | idempotent no-op + JSON + --force JSON |
| `internal/cmd/snapshot_test.go` | +5 | backup snapshot create/skip/fatal/JSON |
| `internal/cmd/provision_test.go` | +9 | flag registration, snapshot, log file, error tail |
| `internal/provision/module_test.go` | +2 | SECURITY BASELINE regression guard + topo order |
| `internal/provision/provisioner_test.go` | +6 (incl. truncation, log writer) | REQ-006-005 stderr-tail surfacing |
| **Total new/changed test funcs** | **~32** | |

## Test run

`go test ./... -count=1` — all wave-touched packages pass.

Pre-existing flake (NOT caused by this wave): `sd/internal/session
TestProperty_CustomSessionNameOverridesDefault` — rapid generator can draw a
custom name that itself starts with `sd-`, then the assertion `not contains
"sd-"` fails. Predates this branch (commit 5f03d8a). Should be filed as a
separate bug; not blocking Wave 4.

## Must-fix (tests missing for a claimed-fixed bug)

### MF-1. REQ-004-022 — `sd snapshot create` does not log a `snapshot-create` audit event AND no test verifies any audit-event emission

The revival audit identifies REQ-004-022's coverage gap as "snapshot-create,
snapshot-restore, config-change not logged." Wave 4 added `LogEvent` calls in
**destroy.go** (pre-destroy safety snapshot) and **snapshot.go** (pre-restore
backup snapshot + post-restore restore event), but:

- `runSnapshotCreate` in `internal/cmd/snapshot.go` (the standalone `sd snapshot
  create` path, lines 129–189) emits **no** audit event. A user-initiated
  `sd snapshot create <vm> --tag <t>` therefore still produces no audit record
  — the requirement is not actually fixed for the most common path.
- No test in `destroy_test.go`, `snapshot_test.go`, or `provision_test.go`
  asserts the audit log received an event. The new code in destroy.go lines
  111–129 and snapshot.go lines 286–303, 344–354 is entirely untested.
  Tests must either drive a real `AuditLog()` and grep the log file, or
  install a fake via a new test-only setter.

Action: add audit-event tests for (a) destroy → `snapshot-create` and
`snapshot-create-failed`, (b) snapshot restore → `snapshot-create` (backup)
and `snapshot-restore`. Then either implement and test audit logging for
standalone `sd snapshot create` or move REQ-004-022 to "still PARTIAL" in the
revival audit.

### MF-2. `sd list` — no test for the new `sd prune` hint nor the `CreatedAt` overlay

`internal/cmd/list.go` gained two behaviors in this wave (`pluralIES`
hint after orphan warnings, and the per-VM overlay that pulls
`State.CreatedAt` from the persisted config when the backend returns nil).
`internal/cmd/list_test.go` has zero added tests. Both behaviors are
user-visible and trivially testable:
- Hint: seed an orphan dir, run list, assert progress stream contains
  ``run `sd prune` to clean``.
- Overlay: seed a VM whose backend `List()` returns `CreatedAt: nil` and
  whose `config.yaml` contains a `state.created_at` — assert the JSON output
  has the persisted timestamp.

Without these, bug-no-prune-command's "actionable warning" acceptance criterion
and bug-created-at-zero's "list shows created_at" criterion are unverified
through the user-facing entry point.

### MF-3. No regression guard that `sd init` no-op path goes to **stderr** (human mode)

`init.go` writes the "already exists" message to `os.Stderr`. The
`TestInitCommand_NoOverwriteWithoutForce` test captures stderr correctly, but
it does **not** also capture stdout and assert that stdout is empty. A
regression that accidentally routes the message to stdout would silently
break the AI-first contract (stdout for data, stderr for messages — cardinal
rule). Add an assertion that stdout is empty in the no-op human path.

## Should-fix (additional coverage gaps)

### SF-1. Property test for `bug-created-at-zero` migration

`created_at_test.go` has three example tests but no rapid property. Natural
invariant: "for any legacy config (no `state.created_at`), `ReadVMConfig`
returns a non-nil `CreatedAt` equal to the file mtime; for any config that
*has* `state.created_at`, `ReadVMConfig` returns exactly that timestamp."
Plug-in to `internal/provision/module_test.go`-style rapid.

### SF-2. Property test for `sd prune` invariant

`prune_test.go` has 5 example tests but no rapid property. The strongest
invariant — and one with real safety value — is: "for any set of VM-name
strings (live + orphan), `sd prune --json --yes` removes **exactly** the
orphan dirs and **never** touches a live VM's dir." Worth a 50-line rapid
test given the destructive nature of the command.

### SF-3. No test for `--no-backup-snapshot` flag default-value or registration

`snapshot_test.go` adds 5 restore-backup tests but does not include a
flag-registration test (analogous to
`TestProvisionCommand_NoProvisionSnapshotFlag_Registered`). A cobra-level
test that `snapshot restore` has the flag with `DefValue == "false"` would
prevent a future refactor from silently dropping the flag.

### SF-4. Snapshot-restore audit `backup_tag` metadata not asserted

`snapshot.go` adds `meta["backup_tag"] = backupTag` to the post-restore audit
event. No test inspects the audit metadata. Combined with MF-1, this is part
of the missing audit-assertion surface.

### SF-5. `--no-snapshot` flag does not record an audit event

The revival audit's SEC-001 acceptance criterion includes "explicit opt-out
…recorded in audit log." Code in `destroy.go` only logs when a snapshot is
*attempted*. Opting out via `--no-snapshot` is silent — no audit event. No
test for this. Either log an `snapshot-skipped` event with reason="user
opt-out" or update the revival audit to remove that AC.

### SF-6. `provision.log` permissions and atomicity

`openProvisionLog` opens with `0o644`. No test asserts the file's mode (should
arguably be `0o600` for tool-output that may contain script secrets, e.g. URLs
with tokens echoed by failing modules). Add a `Stat`-mode assertion.

### SF-7. `Provision` log writer error swallowing — untested

`writeProvisionLog` says "Errors are silently dropped: log failures must not
abort provisioning." There's no test exercising a writer that returns an
error (e.g., a `failingWriter`) to prove the provisioner still completes.
Easy to add.

### SF-8. No negative test for unreadable VM config in the list overlay

`list.go`'s new overlay calls `l.ReadVMConfig(vms[i].Name)` and discards the
error. If the file is malformed YAML, behavior is silent skip (correct). No
test asserts that malformed config does not break the list command —
worth one table case.

## Nits

- `internal/cmd/provision_test.go:setupProvisionTest_returningHome` —
  snake_case identifier in Go; convention is `setupProvisionTestReturningHome`.
- `TestDefaultModuleNames_SecurityBaseline` — the comment "do NOT change the
  assertion" is correct and welcome. Consider adding a `t.Logf` printing the
  full `DefaultModuleNames` slice on success too, so failures and successes
  both leave breadcrumbs.
- `withFixedPreRestoreTag` in `snapshot_test.go` and `autoProvisionSnapshotTag`
  in `provision_test.go` use different override patterns (one via `t.Cleanup`
  helper, one inline). Minor inconsistency; pick one and document it.
- `mockSnapshotBackend` formatting churn in `snapshot_test.go` (lines 21–66)
  is `gofmt` alignment-only — visually noisy in review but harmless.
- `internal/config/loader.go:250` — single-line `if vmViper.IsSet(...) { ...
  result.State.CreatedAt = &t }` is `gofmt`-clean but harder to read; the
  same logic in `ReadVMConfig` is multi-line. Consistency favours the
  multi-line form.

## Coverage matrix — claimed fixes vs. test evidence

| Issue / Req | Implementation? | Test asserts behavior? | Notes |
|-------------|-----------------|------------------------|-------|
| SEC-001 / REQ-004-006,025 default-deny modules | Yes (`DefaultModuleNames`) | **Yes** (regression guard + topo order) | Strong. |
| bug-init-not-idempotent | Yes | Yes (human + JSON + force) | Add stdout-empty assertion (MF-3). |
| bug-no-prune-command | Yes | Partial (no list-hint test, MF-2) | |
| bug-created-at-zero | Yes (loader + create + list overlay) | Partial (loader yes; list overlay no, MF-2) | |
| REQ-004-019 destroy fatality | Yes | Yes (incl. inverted property test) | |
| REQ-004-019 provision snapshot | Yes | Yes | |
| REQ-004-019 restore backup snapshot | Yes | Yes | |
| REQ-004-022 audit for destroy/restore | Code added | **No test** (MF-1) | |
| REQ-004-022 audit for standalone snapshot create | **No** | n/a | Not implemented. |
| REQ-006-005 stderr tail | Yes | Yes (truncation, 50-line tail) | |
| REQ-006-005 provision.log file | Yes | Yes (append/grow) | Mode/perm untested (SF-6). |

## Summary by severity

- Must-fix: **3**
- Should-fix: **8**
- Nits: **5**
