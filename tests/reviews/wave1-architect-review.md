# Wave 1 Architect Review

Reviewer: architect
Date: 2026-04-09

---

## T1: Add packages/setup/repo/branch to ProjectConfig

**Worktree:** cc_1
**Files:** `internal/config/project.go`, `internal/config/project_test.go`
**Spec:** 009-declarative-environment.md (REQ-009-001, 002, 003, 011)

### Verdict: PASS

### Analysis

**API Design**

The new types are well-designed and follow the spec precisely:

- `PackageConfig` as a pointer (`*PackageConfig`) on `ProjectConfig` is the correct choice. It distinguishes "absent" (nil) from "present but empty" which matches the spec's requirement that absent and empty are treated identically. The `yaml:"packages,omitempty"` tag ensures clean round-trip serialization -- a nil pointer omits the field entirely, which is exactly what backward compatibility requires.

- Field ordering in `ProjectConfig` is logical: identity fields first (Name, Backend), resource fields (CPUs, Memory, Disk), module/mount infrastructure, then the new declarative fields (Repo, Branch, Packages, Setup). This matches the spec's example YAML layout.

- The `validateDeclarativeFields` function is cleanly separated from the existing `validateProjectConfig`, called at the end. Good separation of concerns.

**Consistency with Codebase**

- Follows the established pattern: `LoadProjectConfig` -> `validateProjectConfig` -> specific validators. The new code slots into this pipeline without restructuring.
- Error message format matches existing patterns: `"%s: <field description>: <constraint>"` with the file path prefix, consistent with mount and resource validation errors.
- REQ-ID comments are present on all new types and functions.
- Test patterns (table-driven, `testify`, temp directories) match existing tests exactly.

**Spec Compliance**

- REQ-009-001: All five package managers (apt, pip, npm, go, cargo) are fields on `PackageConfig`. Parsing and validation tested for all five. Empty list and absent field handled correctly. PASS.
- REQ-009-002: `Setup` as `[]string` with empty-string validation. PASS.
- REQ-009-003: `Repo` with HTTPS/SSH URL validation, `Branch` with whitespace/double-dot/control-char validation, branch-without-repo error. PASS.
- REQ-009-011: All validation rules from the spec table are implemented with matching error message formats. The `containsControlChars` helper is a clean addition for branch name validation. PASS.

**Test Coverage**

Comprehensive: all sub-keys, subset, absent, empty list, empty string entries for each manager type, valid/invalid URLs, branch validation (spaces, tabs, double-dots, control chars, valid names), branch-without-repo, full declarative config, backward compatibility, round-trip with new fields, validation error includes path, second-entry-empty for both setup and packages.

### Issues

1. **should-fix** `internal/config/project.go:178-180` -- The branch validation checks for whitespace and `..` but does not check for other git-invalid branch name characters like `~`, `^`, `:`, `?`, `*`, `[`, `\`, or sequences like `@{`. The spec says "no spaces, no `..`, no control characters" which the code implements, but `git check-ref-format` rejects more patterns. Consider adding at minimum `~`, `^`, `:`, `\`, `*`, `?`, `[`, and `@{` to the rejected set, or shell out to `git check-ref-format` in a follow-up. This is a should-fix because the spec explicitly lists only the three checks implemented, but real git usage will encounter failures with these characters.

2. **note** `internal/config/project.go:51` -- The variable is named `projectVMNamePattern` while the original was `projectVMNamePattern`. This is fine, just noting there was no rename collision since the original also used this name.

---

## T4: Fix sd exec credential injection

**Worktree:** cc_2
**Files:** `internal/cmd/exec.go`, `internal/cmd/exec_test.go`
**Spec:** 007-connection.md (REQ-007-019)

### Verdict: NEEDS-CHANGES

### Analysis

**API Design**

The approach is architecturally significant: T4 replaces the `b.Exec()` backend call with a direct SSH invocation via `execSSHRunner`. This is a fundamental change in how exec works.

- **Before (main branch):** `exec.go` calls `b.Exec(ctx, name, command)` which delegates to the backend's `Exec` method. The backend handles SSH details internally.
- **After (cc_2):** `exec.go` calls `b.SSHConfig(ctx, name)` to get connection details, builds SSH args itself (via `buildExecSSHArgs`), then runs the SSH command directly via `execSSHRunner`.

This architectural shift is necessary for credential injection because the `Backend.Exec` interface (per REQ-003-007) does not accept environment variables as a parameter, and the spec (009) explicitly says the Backend.Exec interface does NOT need modification. The SSH `SendEnv`/`AcceptEnv` mechanism operates at the transport layer, which requires control over the SSH command construction.

The `buildExecSSHArgs` function mirrors the pattern already established in `connect.go`'s `buildSSHArgs`, maintaining consistency.

**Spec Compliance**

- REQ-007-019: Credentials are injected via SSH `SendEnv` mechanism, filtered to only `SD_*`, `ANTHROPIC_*`, `GITHUB_*`, `GH_*` patterns. Environment variables are set on the SSH process (not written to disk). PASS.
- REQ-007-013: stdout/stderr passthrough, exit code matching. PASS.
- REQ-007-014: JSON output format unchanged. PASS.

**Consistency with Codebase**

The credential loading pattern reuses `readCredFunc` and `isCredentialKey` from `connect.go`. Good reuse. The `execSSHRunner` var-function pattern is consistent with other test-overridable functions in the codebase (e.g., `captureHostKey`, `generateSSHKeys`).

**Test Coverage**

Excellent: credential injection, credential filtering (verifying non-credential keys are excluded), no credentials, credential load error (gracefully ignored), SSH args contain SendEnv, VSOCK transport handling, plus all original tests preserved and adapted to the new SSH runner mock.

### Issues

1. **must-fix** `internal/cmd/exec.go:60-99` -- `buildExecSSHArgs` constructs a `UserKnownHostsFile` path inconsistently with `connect.go`. Looking at the `buildSSHArgs` function in `connect.go`, TCP connections use `StrictHostKeyChecking=yes` with `UserKnownHostsFile=<sd-home>/vms/<name>/ssh/known_hosts`, while VSOCK uses `StrictHostKeyChecking=no` with `UserKnownHostsFile=/dev/null`. In `buildExecSSHArgs`, line 71 only sets `StrictHostKeyChecking=yes` for non-ProxyCommand (TCP) but does NOT set `UserKnownHostsFile`. For VSOCK, it also does not set `StrictHostKeyChecking=no` or `UserKnownHostsFile=/dev/null`. This means TCP exec connections will use the default known_hosts file (potentially failing on unknown hosts), and VSOCK connections will also use default host key checking (potentially prompting, which breaks non-interactive exec). The function needs to match `connect.go`'s SSH config fragment handling.

2. **must-fix** `internal/cmd/exec.go:60` -- The function signature takes `cfg backend.SSHConfig` but never reads `cfg.IdentityFile` from the SD-managed SSH key path. In the original code, the backend's `Exec` handled SSH keys internally. Now that exec builds its own SSH command, it relies on `cfg.IdentityFile` from the backend. But the per-VM SSH keys generated by `setupSSH` during create are stored at `~/.sd/vms/<name>/ssh/id_ed25519`, and the backend's `SSHConfig()` may or may not return this path (depends on the backend implementation). For Lima, `SSHConfig` returns the key path from `limactl show-ssh`; for the memory backend in tests, it returns empty. The real Lima backend may not know about the SD-managed keys. Verify that `b.SSHConfig()` for Lima returns the correct SD-managed identity file, or fall back to the SD-home-based path when `cfg.IdentityFile` is empty.

3. **should-fix** `internal/cmd/exec.go` -- The original exec.go handled `backend.ErrVMNotFound` and `backend.ErrVMNotRunning` from the `b.Exec()` call (lines 112-129 in original). The new code removed the `b.Exec()` call and replaced it with direct SSH, but the SSH runner (`execSSHRunner`) returns generic errors -- it cannot distinguish between "VM not found" and "SSH connection refused". The new code only handles these sentinel errors from `b.Status()`, which is fine for the running check, but the error messages from SSH failures will be generic "failed to execute command" rather than the more specific messages the original code produced. Consider wrapping SSH connection errors with more actionable messages.

4. **note** `internal/cmd/exec.go:77` -- The `SendEnv` directive `"SendEnv=SD_* ANTHROPIC_* GITHUB_* GH_*"` puts all patterns in a single `-o` value. OpenSSH accepts this but it is slightly unusual -- most SSH configs use separate lines. This works and matches the SSH config fragment in `connect.go`, so it is fine.

---

## T8: Implement hints system in JSON output

**Worktree:** cc_3
**Files:** `internal/ui/formatter.go`, `internal/ui/formatter_test.go`, `internal/cmd/create.go`, `internal/cmd/ensure.go`
**Spec:** 010-agent-bootstrap.md (REQ-010-016)

### Verdict: PASS

### Analysis

**API Design**

The design is minimal and well-targeted:

- A new `jsonSuccessWithHints` struct alongside the existing `jsonSuccess` -- clean separation. The `Hints` field uses `json:"hints,omitempty"` which means nil and empty slices both omit the field, exactly matching the spec requirement: "Be omitted from the JSON output when there are no hints (the field is optional, not required to be an empty array)."

- `SuccessDataWithHints(data any, hints []string, formatFunc func() string)` extends the existing `SuccessData` pattern with a single additional parameter. This is the right abstraction level -- it does not require callers to restructure their output logic.

- The method delegates to the same JSON encoder pattern as `SuccessData`. In human mode, hints are completely ignored, satisfying: "The hints field MUST NOT appear in non-JSON (human-readable) output."

**Consistency with Codebase**

- The new method follows the exact pattern of `SuccessData` and `FailureData`: same parameter style, same encoder setup, same human-mode fallback to `formatFunc`.
- The `jsonSuccessWithHints` struct mirrors `jsonSuccess` with just the added `Hints` field. No unnecessary abstraction.
- In `create.go` and `ensure.go`, the migration from `f.SuccessData(result, formatFunc)` to `f.SuccessDataWithHints(result, hints, formatFunc)` is clean and minimal.

**Spec Compliance**

- REQ-010-016 point 1: Hints appear at top level alongside `ok` and `data`. PASS.
- REQ-010-016 point 2: Array of strings, each self-contained. PASS.
- REQ-010-016 point 3: Contextual -- create hints about credential setup and egress, ensure-start hints about credentials only. PASS.
- REQ-010-016 point 5: Hints are informational, non-critical. PASS.
- REQ-010-016 point 6: Absent when no hints (omitempty). PASS.
- Hints do not affect exit codes or ok status. PASS.

**Test Coverage**

Thorough: present hints, nil hints (absent from JSON), empty hints (absent from JSON), human mode (no hints in output), property test covering all cases with valid JSON verification. The tests correctly verify both the presence and absence semantics.

### Issues

1. **should-fix** `internal/cmd/create.go:216-219` and `internal/cmd/ensure.go:207-210,255-257` -- The hint strings are hardcoded inline. The spec design section suggests a centralized `hints` package with functions like `ForEnsure(created, credentialsDetected)` and `ForConnect(credentialsInjected)`. While the current inline approach works, it will not scale well as more commands add hints. Consider extracting a `hints.go` file (or an `internal/hints/` package) that centralizes hint generation, potentially making them context-sensitive (e.g., only hint about GITHUB_TOKEN when it is not already set). This is not a blocker for this task but should be addressed before T5 (quick-start) adds more hint call sites.

2. **note** `internal/cmd/ensure.go:148` -- The "already_running" branch still uses `f.SuccessData` (no hints). This is actually correct -- there is nothing actionable to hint about when the VM is already running. But consider adding a hint about credentials if `GITHUB_TOKEN` is not set, since the user may be running `sd ensure` as a precursor to `sd connect`.

3. **note** `internal/ui/formatter.go:35-39` -- The `jsonSuccessWithHints` struct duplicates `OK` and `Data` from `jsonSuccess`. An alternative design would embed `jsonSuccess` and add `Hints`, but the current approach is clearer and avoids Go embedding subtleties with JSON tags. The duplication is acceptable for two fields.

---

## Cross-Worktree Integration Analysis

### Will these changes compose when merged?

**T1 (config) + T4 (exec):** No conflicts. T1 modifies `internal/config/project.go`; T4 modifies `internal/cmd/exec.go`. They share no files. T4 uses `readCredFunc` from `connect.go` which is unchanged in T1.

**T1 (config) + T8 (hints):** No conflicts. T1 modifies `internal/config/project.go` and `project_test.go`; T8 modifies `internal/ui/formatter.go`, `formatter_test.go`, `internal/cmd/create.go`, and `internal/cmd/ensure.go`. No file overlap.

**T4 (exec) + T8 (hints):** No conflicts. T4 modifies `internal/cmd/exec.go` and `exec_test.go`; T8 modifies `internal/ui/formatter.go`, `formatter_test.go`, `internal/cmd/create.go`, and `internal/cmd/ensure.go`. No file overlap. However, a future task could add hints to `exec.go` output, which would require importing the hints from T8. The `SuccessDataWithHints` method from T8 is ready for this.

**Semantic consistency:** All three tasks reference consistent requirement IDs. T4's credential injection serves T1's repo clone feature (REQ-009-008 depends on REQ-007-019). T8's hints system serves the quick-start flow that will use T1's project config. The dependency chain is sound.

### Extensibility for Future Work

**T2 (provisioning pipeline):** T1's `PackageConfig` and `Setup` fields are ready to be consumed by a `provision/declarative.go` module. The struct is well-designed for this: `Packages != nil` cleanly gates whether package installation runs, and each sub-field's nil vs empty semantics are correct.

**T5 (quick-start):** T8's hints infrastructure is ready. The `SuccessDataWithHints` method can be adopted by any command. The hint strings should be centralized before T5 adds many more.

**exec credential injection for repo clone:** T4's `execSSHRunner` with credential injection means `sd exec <vm> -- git clone <private-repo>` will work with injected `GITHUB_TOKEN`, which is exactly what T1's repo clone step (REQ-009-008) needs.

---

## Summary

| Task | Verdict | Must-Fix | Should-Fix | Notes |
|------|---------|----------|------------|-------|
| T1: ProjectConfig fields | PASS | 0 | 1 | Branch validation could be stricter |
| T4: exec credential injection | NEEDS-CHANGES | 2 | 1 | SSH config missing known_hosts/stricthostkey; identity file fallback needed |
| T8: Hints system | PASS | 0 | 1 | Centralize hint strings before more commands adopt |
