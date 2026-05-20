# Wave 4 — Critic Review (int-ralph-loop)

Scope: 28 modified files + 3 new files (`internal/cmd/prune.go`,
`internal/cmd/prune_test.go`, `internal/config/created_at_test.go`).
Focus: correctness bugs, edge cases, security.

## Must-fix (blocks commit)

### MF-1 — `sd prune` will delete state for live VMs when a backend is transiently unavailable

File: `internal/cmd/prune.go:82-98`, function `findOrphans`.

```go
for _, name := range allBackendNames() {
    b, err := getBackendFunc(name)
    if err != nil { continue }
    if err := b.Available(); err != nil { continue }
    bvms, err := b.List(cmd.Context())
    if err != nil { continue }      // <-- silently drops backend
    for _, vm := range bvms { known[vm.Name] = true }
}
```

If `lima` is down, `limactl` exits non-zero, or there is a network/socket
hiccup, the loop swallows the error and proceeds with an empty `known` set
for that backend. Every state directory owned by VMs on that backend is then
classified as orphaned. With `--yes` (which the JSON envelope requires for
removal), `RemoveVMConfig` wipes `$SD_HOME/vms/<name>/` for *every live VM*.
That directory holds SSH keys (`ssh/id_ed25519`), the per-VM `config.yaml`,
audit logs, and provision logs — none of which can be regenerated from the
backend. Once removed, `sd connect` for that VM is permanently broken until
the user manually rebuilds SSH state.

Fix:
- Treat `b.Available()` failing or `b.List()` returning an error as a fatal
  refusal to prune (return `prune_failed` with a clear message: "backend X
  unavailable; refusing to prune to avoid deleting live-VM state").
- At minimum, require *all* registered backends to successfully enumerate
  before any directory is removed. Partial enumeration is unsafe.
- Add a property test that simulates backend `List` failure and asserts no
  state directory is removed.

### MF-2 — Provision log captures unredacted credentials (CWE-532)

Files: `internal/provision/provisioner.go:148-211` (`writeProvisionLog`,
`formatScriptFailure`), `internal/cmd/provision.go:36-49` (`openProvisionLog`).

Scripts run under `set -eux -o pipefail` (confirmed in
`provisioner_test.go:142` and module scripts), so bash echoes every command
to stderr with variable expansion intact: `+ curl -H 'Authorization: Bearer
ghp_...'`, `+ export ANTHROPIC_API_KEY=sk-ant-...`, `+ git clone
https://x-access-token:ghp_xxx@github.com/...`. The provisioner now writes
that stderr unfiltered to `$SD_HOME/vms/<name>/provision.log` (perms
`0o644` — world-readable) and additionally inlines the last 50 lines of
stderr into the error message returned to stdout / JSON envelope on failure
(`formatScriptFailure`).

This violates the spec's "never persist credentials to disk" rule (CLAUDE.md
Security Patterns, REQ-004-024 token policy). A failed `sd create` that
ships its JSON to a log aggregator now leaks tokens by default.

Fix (in priority order):
1. Open the log file `0o600`, not `0o644`.
2. Redact known token patterns before writing to the log *and* before
   embedding in error messages. At minimum match: `ghp_`, `gho_`,
   `github_pat_`, `sk-ant-`, `xoxb-`, `AKIA[0-9A-Z]{16}`, `Bearer
   <hex/base64>`, `Authorization: \S+`, `password=\S+`, `://[^:]+:[^@]+@`
   (URL userinfo).
3. Better: pass credentials via a stdin file descriptor or `env -i` shim
   that prevents bash `set -x` from echoing them. Today the credential
   injection path is the same env the script runs under, so `-x` always
   prints them.
4. Add a regression test that runs a script with a fake secret in env and
   asserts the value never appears in the log file or in
   `ProvisionResult.Error`.

### MF-3 — `VMInfo.CreatedAt` JSON tag is missing `omitempty`; emits `null`

File: `internal/backend/backend.go:32`.

```go
CreatedAt *time.Time `json:"created_at"`
```

Without `omitempty`, JSON marshal of a VMInfo whose CreatedAt is nil emits
`"created_at": null`. The previous shape was a string (zero time
`"0001-01-01T00:00:00Z"`). Both are wire-format changes; `null` will break
any downstream JSON consumer that expects a string. The corresponding
`VMState.CreatedAt` in `internal/config/types.go:74` *does* have
`omitempty` — they should match. Either:
- Add `omitempty` to the backend struct tag (preferred — consistent with the
  config type), or
- Document a Wave 4 schema breaking change and bump the JSON schema version.

This will also flip property-test invariants: `TestProperty_VMInfoRoundTrip`
now passes only because the generator always sets a non-nil pointer.

## Should-fix

### SF-1 — `sd snapshot restore`: backup-snapshot tag is silently abandoned on apply failure

File: `internal/cmd/snapshot.go:325-342`. When `SnapshotApply` fails after a
backup snapshot was successfully created, the error message does not mention
the backup tag, leaving the user unaware that an extra snapshot exists. Make
the error include `(a backup snapshot %q was created; you can delete it with
sd snapshot delete --tag %q)`.

### SF-2 — `sd provision` failure leaves VM in half-egressed state (SEC-001 concern)

File: `internal/cmd/provision.go:262-272`. On failure during re-provision,
unlike `sd create`, the command does not roll back or auto-restore from the
pre-provision snapshot. If `egress` script fails mid-way (e.g., after
`iptables -F` but before installing new rules) the VM is now wide open.
Either auto-restore the pre-provision snapshot on failure, or print the
exact restore command in the error message and tag the result with
`network_open=true` so calling automation can react.

### SF-3 — `init` no-op path does not validate existing `.sd.yaml`

File: `internal/cmd/init.go:121-134`. `os.Stat` succeeding is treated as
"already initialized" regardless of file contents. A truncated, empty, or
malformed `.sd.yaml` will be reported as success, and subsequent `sd create`
will fail with a confusing parse error. At minimum, parse the file and on
parse failure return a distinct `invalid_existing_config` code so the user
knows to `--force` or fix it.

### SF-4 — `config.Loader.GetForVM`: `IsSet` true + unparseable timestamp yields zero-time pointer

File: `internal/config/loader.go:250`. `vmViper.IsSet("state.created_at")`
returns true if any value is present, even a malformed string. `GetTime`
returns zero time for unparseable input. The resulting `*time.Time` is
non-nil but points to year 0001 — defeats the purpose of the pointer
migration. Add `if t.IsZero() { result.State.CreatedAt = nil }`.

### SF-5 — `ReadVMConfig` mtime fallback returns directory mtime, not creation time

File: `internal/config/loader.go:680-686`. If the VM's config has been
edited (e.g., `sd start` updates `last_started`), `fi.ModTime()` reflects
the latest edit, not creation. The "fallback CreatedAt" can drift forward in
time and even appear *after* `LastStarted`. Either use `os.Stat` on the
directory entry, statx the BTIME on Linux (if available), or persist a
sentinel field once and stop falling back. At a minimum, document that the
fallback is approximate.

### SF-6 — Provisioning log written `0o644` (also covered by MF-2)

`os.OpenFile(path, ..., 0o644)` — even after redaction, this should be
`0o600` to match the SSH-key directory permissions and the general
secrets-on-disk posture.

### SF-7 — `formatScriptFailure` may inflate error messages to megabytes

The last 50 lines of stdout *and* stderr are inlined into the
`ProvisionResult.Error` string with no per-line length cap. A misbehaving
script that emits 50 lines of multi-MB output will produce error messages
huge enough to choke JSON consumers and shell pipelines. Cap per-line and
total bytes (e.g., 4KiB per stream, 8KiB combined).

### SF-8 — Audit metadata `error` field contains raw error string

`internal/cmd/destroy.go:122`, `internal/cmd/snapshot.go:298`. The
`meta["error"] = snapErr.Error()` line stores the unredacted error string in
the audit log. If the backend's error includes paths, command lines, or
embedded creds, they land in the audit JSON. Apply the same redactor used
for the provision log (see MF-2).

### SF-9 — `pluralIES(0)` returns "ies"

`internal/cmd/list.go:155`. Harmless given the `orphans > 0` guard, but the
function answers grammatically wrong for zero. Tighten the contract or the
name.

## Nits

- `internal/cmd/init.go:124` — `initNoopResult.Status = "already_initialized"`
  collides with the envelope's own `status` field if `SuccessData` adds one;
  consider renaming to `init_status` or relying on `Action: "noop"`.
- `internal/cmd/list.go` — the orphan detection now happens twice (inline
  loop *and* `orphanCount` in `prune.go`). Consolidate to one helper.
- `internal/cmd/destroy.go:104-107` — comment cites
  `specs/004-security.md "Snapshot Creation Failure"`; verify that section
  actually exists in the spec at HEAD, otherwise update the citation.
- `internal/provision/provisioner.go:204` — log writer ignores all write
  errors silently. A disk-full or quota condition gets no signal at all.
  Consider logging once via `f.Progress` when the first write fails.
- `internal/cmd/snapshot.go:21-23` — `preRestoreSnapshotTag` uses
  `time.Now()` (local TZ) while `autoProvisionSnapshotTag` does the same.
  Inconsistent with the `created_at` migration that standardized on UTC.
  Use UTC for snapshot tags to ensure ordering across machines.
- `internal/cmd/prune.go:31` — `SetPruneStdin` exported just for tests; use
  an unexported var and a test-only setter via build tag, or pass via
  context.
- `internal/cmd/provision.go:212` — `provision_snapshot_failed` is a new
  error code; ensure it is documented in the spec alongside the existing
  `snapshot_failed` to keep the error vocabulary closed.
