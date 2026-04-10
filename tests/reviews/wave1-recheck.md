# Wave 1 Fix Re-Review

Date: 2026-04-09

## T4: Exec Credential Injection Fix

Worktree: cc_2
Files: `internal/cmd/exec.go`, `internal/cmd/exec_test.go`

### Issue 1: Implementation bypassed b.Exec() with direct SSH calls

**RESOLVED.**

`exec.go` line 159 calls `b.Exec(cmd.Context(), name, execCommand)` -- the backend
interface method. There is no direct SSH invocation anywhere in the file. No imports
of `os/exec`, `ssh`, or any shell-out mechanism. The command goes through the backend
abstraction exclusively.

### Issue 2: Missing SSH config options

**RESOLVED.**

Since all execution routes through `b.Exec()`, SSH config is handled by each backend's
own implementation (e.g., Lima's `limactl shell`). The exec command has zero SSH
awareness, which is the correct design.

### Issue 3: Error mapping tests (ErrVMNotFound, ErrVMNotRunning) should be restored

**RESOLVED.**

Both sentinel-error mapping tests exist:

- `TestExecCommand_ExecBackendErrVMNotFound` (line 334): injects `backend.ErrVMNotFound`
  into the exec handler, asserts `vm_not_found` error code.
- `TestExecCommand_ExecBackendErrVMNotRunning` (line 317): injects `backend.ErrVMNotRunning`
  into the exec handler, asserts `vm_not_running` error code.

These test the error paths from `b.Exec()` itself (lines 162-172 in exec.go), not just
the pre-flight status check.

### Credential injection (bonus verification)

`buildEnvCommand()` (line 45) wraps commands with `env KEY=val ...` prefix when
credentials are present. Credentials are filtered via `isCredentialKey()` (line 145).
Tests cover: injection with creds, filtering of non-credential keys, no-creds passthrough,
credential load error tolerance, and deterministic ordering. All use the memory backend
via `SetExecHandler` to verify the exact command slice passed to `b.Exec()`.

---

## T8: Hints Integration Tests Fix

Worktree: cc_3
Files: `internal/cmd/create_test.go`, `internal/cmd/ensure_test.go`, `internal/ui/formatter_test.go`

### Issue: No integration tests verifying hints appear in create/ensure JSON output

**RESOLVED.**

#### create_test.go

- `TestCreateCommand_JSON_ContainsHints` (line 228): Runs `--json create testvm`,
  parses JSON output, asserts `hints` field is present as a non-empty array, and verifies
  two specific hint strings (GITHUB_TOKEN and egress list). Tagged with REQ-010-016.

#### ensure_test.go

Three integration tests added:

- `TestEnsureCommand_JSON_Created_ContainsHints` (line 272): Runs ensure on a
  non-existent VM (triggers create path), parses JSON, asserts hints array is present
  with both expected hint strings.
- `TestEnsureCommand_JSON_Started_ContainsHints` (line 314): Runs ensure on a stopped
  VM (triggers start path), parses JSON, asserts hints array present with the
  GITHUB_TOKEN hint.
- `TestEnsureCommand_JSON_AlreadyRunning_NoHints` (line 364): Runs ensure on a running
  VM (noop path), parses JSON, asserts hints field is absent. This is a negative test
  confirming hints are only emitted when action is taken.

#### formatter_test.go (rapid property tests)

Two rapid-based property tests added:

- `TestRapid_SuccessDataWithHints_NonEmptyHintAlwaysPresent` (line 434): Generates 1-5
  random hint strings via rapid, calls `SuccessDataWithHints`, asserts every hint appears
  in the JSON output array at the correct index.
- `TestRapid_SuccessDataWithHints_EmptyHintsAlwaysOmitted` (line 466): Randomly chooses
  nil or empty slice, asserts hints field is always absent from JSON output.

Additional table-driven tests cover nil hints, empty hints, single/multiple hints,
and human-mode suppression of hints.

---

## Summary

| Fix | Issue | Verdict |
|-----|-------|---------|
| T4  | b.Exec() used (not direct SSH) | RESOLVED |
| T4  | SSH config options no longer relevant | RESOLVED |
| T4  | ErrVMNotFound/ErrVMNotRunning error mapping tests | RESOLVED |
| T8  | create JSON output hints integration test | RESOLVED |
| T8  | ensure JSON output hints integration tests (created, started, already_running) | RESOLVED |
| T8  | rapid property-based tests for hints | RESOLVED |

All must-fix items from the initial review have been addressed.
