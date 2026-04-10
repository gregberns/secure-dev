# Wave 1 Critic Review

Reviewer: critic
Date: 2026-04-09

---

## T1: Add packages/setup/repo/branch to ProjectConfig

**Worktree:** cc_1
**Files:** `internal/config/project.go`, `internal/config/project_test.go`
**Spec:** REQ-009-001, REQ-009-002, REQ-009-003, REQ-009-011

### Verdict: PASS

### Issues

#### 1. Security: Package names not sanitized for shell injection
- **File:** `internal/config/project.go:199`
- **Severity:** should-fix
- **Description:** Package entries are validated only for being non-empty strings. A package name like `jq; rm -rf /` would pass validation and later be passed to `apt-get install -y` or similar shell commands during provisioning. While shell injection defense belongs in the provisioning layer (not config validation), the validation here creates a false sense of safety. The spec (REQ-009-001) lists specific package manager commands that will receive these values, so at minimum, entries containing shell metacharacters (`;`, `|`, `&`, `$`, backticks) should be flagged or the spec should explicitly defer sanitization to the provisioning layer.
- **Note:** This is defense-in-depth for a security tool. The provisioning layer may or may not do its own sanitization.

#### 2. Branch validation incomplete vs git ref rules
- **File:** `internal/config/project.go:177-183`
- **Severity:** note
- **Description:** The branch validation rejects spaces, tabs, newlines, `..`, and control chars. However, valid git branch names also forbid: `~`, `^`, `:`, `?`, `*`, `[`, `\`, names ending in `.lock`, names starting with `-`, bare `.` components, and consecutive dots in some contexts. For example, `feature~1` or `refs/heads/..` or `my-branch.lock` would pass validation but are invalid git refs. The current set of checks is a reasonable pragmatic subset but is not comprehensive per `git check-ref-format` rules.
- **Recommendation:** Consider calling out in a code comment that this is a partial check, or add a note referencing `git check-ref-format`.

#### 3. Test for branch with control char may not exercise the intended path
- **File:** `internal/config/project_test.go:581-590`
- **Severity:** note
- **Description:** The test `TestLoadProjectConfig_BranchControlChar` writes the literal string `"feat\\x07ure"` to the YAML file. YAML double-quoted strings support `\x` escape sequences, so the YAML parser should decode this to a string containing byte 0x07. However, if the YAML parser does NOT decode `\x07` (some YAML implementations treat `\x` as literal), the test would still pass because the branch `feat\x07ure` literally contains no control chars -- it contains the ASCII characters `\`, `x`, `0`, `7`. This should be verified. A safer approach is to construct the string programmatically and marshal it to YAML.

#### 4. Missing test: packages field with unknown sub-keys
- **File:** `internal/config/project_test.go`
- **Severity:** note
- **Description:** There is no test for what happens when `.sd.yaml` contains `packages.brew: [htop]` or another unknown package manager key. The current YAML parser will silently ignore unknown keys (standard `yaml.v3` behavior), which is fine, but a test documenting this behavior would be valuable.

#### 5. Struct comment references REQ-009 correctly
- **File:** `internal/config/project.go:19`
- **Severity:** (positive observation)
- **Description:** The `ProjectConfig` struct comment properly references REQ-009-001, REQ-009-002, REQ-009-003. The new fields (`Repo`, `Branch`, `Packages`, `Setup`) are correctly added with appropriate yaml tags and omitempty.

### Summary

The implementation is solid. The new fields are well-integrated into the existing validation pipeline. Tests are thorough, covering all five package manager sub-keys, empty/whitespace entries, repo URL formats (HTTPS/SSH/invalid), branch-without-repo, branch validation, backward compatibility, and round-trip serialization. The `validateDeclarativeFields` function is cleanly separated. The only real concern is shell injection in package names (should-fix), which should be addressed either here or explicitly deferred to the provisioning layer.

---

## T4: Fix sd exec credential injection

**Worktree:** cc_2
**Files:** `internal/cmd/exec.go`, `internal/cmd/exec_test.go`
**Spec:** REQ-007-019

### Verdict: NEEDS-CHANGES

### Issues

#### 1. BUG: Regression -- backend.Exec replaced with direct SSH, removes sentinel error handling
- **File:** `internal/cmd/exec.go:220` (new code) vs original `exec.go:111-129`
- **Severity:** must-fix
- **Description:** The original `exec.go` called `b.Exec(cmd.Context(), name, command)` which goes through the backend interface. The new code replaces this with a direct SSH call via `execSSHRunner("ssh", sshArgs, creds)`. This is a fundamental architectural change that:
  1. **Bypasses the backend abstraction.** The `Backend.Exec` method is the contract for executing commands in VMs (REQ-003-007). Direct SSH calls mean the exec command no longer works with non-SSH backends or backends that implement Exec differently (e.g., Docker exec, Lima shell).
  2. **Loses sentinel error handling.** The original code handled `backend.ErrVMNotFound` and `backend.ErrVMNotRunning` from the Exec call. The new SSH-direct approach cannot distinguish these error types -- any SSH failure becomes a generic "exec_failed".
  3. **Duplicates connect logic.** The `buildExecSSHArgs` function is essentially a copy of SSH arg construction from `connect.go` but specialized for non-interactive mode. This creates maintenance burden.

  The spec (REQ-007-019) says environment injection should happen on `sd exec`, using the SendEnv/AcceptEnv mechanism. The correct fix is to inject credentials into the existing `b.Exec()` call path, either by passing them through the backend interface or by wrapping the backend's SSH transport layer -- NOT by replacing the backend call with direct SSH.

#### 2. BUG: execSSHRunner starts a real SSH process in tests
- **File:** `internal/cmd/exec_test.go:53-67`
- **Severity:** must-fix (related to #1)
- **Description:** The test mock for `execSSHRunner` intercepts the call and returns canned results, which is fine for unit tests. However, the production `defaultExecSSHRunner` (exec.go:27-55) calls `exec.Command("ssh", args...)` directly. This means the exec command now requires SSH to be installed on the host and the VM to have sshd configured to accept the specific SendEnv patterns. The original code delegated execution to the backend, which could use Lima's built-in shell, docker exec, or any other transport.

  This tight coupling to SSH as a transport breaks the backend abstraction and means exec will fail for any backend that doesn't use TCP SSH (e.g., Lima's default `limactl shell` transport).

#### 3. Security: SendEnv pattern is overly broad
- **File:** `internal/cmd/exec.go:77`
- **Severity:** should-fix
- **Description:** The SendEnv directive is `SendEnv=SD_* ANTHROPIC_* GITHUB_* GH_*`. This matches the `isCredentialKey` filter used for what gets set in the process environment. However, SendEnv tells the SSH client to forward ANY environment variable matching these patterns from the host environment, not just the ones explicitly set via `cmd.Env`. If the host has `GITHUB_ACTIONS=true` or `SD_DEBUG=1` or `GH_PAGER=less` in its environment, those will also be forwarded. The `isCredentialKey` filter (exec.go:208-209) only filters what goes into the `creds` map from `readCredFunc`, but the host's own environment variables matching `SD_*`, `ANTHROPIC_*`, `GITHUB_*`, `GH_*` will be forwarded regardless via SendEnv because the process inherits `os.Environ()` (exec.go:35).

  This could leak host-side debugging/CI variables into the VM. The original `sd connect` has the same pattern, so this may be by design, but for `sd exec` which is more automation-oriented, unintended env var leakage is a bigger concern.

#### 4. Test structure change: execCall lost VMName field
- **File:** `internal/cmd/exec_test.go:24-27`
- **Severity:** should-fix
- **Description:** The original `execCall` struct had `VMName` and `Command` fields. The new version has `Command` and `Env` fields but dropped `VMName`. This means tests can no longer verify that the SSH runner was called for the correct VM. The mock extracts the command from SSH args by looking for `--` separator, but the VM name target is embedded in the SSH args (as `user@host`) and is not extracted or verified.

  Original test `TestProperty_ExecRunningVM_CallsBackendOnce` asserted `assert.Equal(t, name, (*calls)[0].VMName)` -- the new version of this test (line 615) only checks `require.Len(t, *calls, 1)` without verifying the target VM.

#### 5. Missing test: original test for ErrVMNotRunning from Exec removed
- **File:** `internal/cmd/exec_test.go`
- **Severity:** should-fix
- **Description:** The original test file had `TestExecCommand_ExecBackendErrVMNotRunning` and `TestExecCommand_ExecBackendErrVMNotFound` which tested that errors from `b.Exec()` were correctly mapped to CLI error codes. These tests are gone in the new version because the backend Exec is no longer called. This is a test coverage regression -- those code paths previously existed for a reason.

#### 6. osExit called after JSON output but function returns nil
- **File:** `internal/cmd/exec.go:252-256`
- **Severity:** note
- **Description:** When the remote command exits non-zero, the code first outputs JSON (if in JSON mode) at line 229-249, then calls `osExit(result.ExitCode)` at line 253, then returns `nil`. The `osExit` call bypasses Go's normal cleanup. In production, `os.Exit` will terminate the process immediately, so the `return nil` is dead code. This is unchanged from the original and not a regression, but worth noting.

### Summary

The core problem with this change is that it replaces the backend abstraction (`b.Exec()`) with direct SSH calls to implement credential injection. While the credential injection itself (loading creds, filtering by prefix, setting env vars, SendEnv) is correctly implemented, the approach fundamentally breaks the architecture. The fix should inject credentials into the existing backend Exec flow, not bypass it. The test changes also regress -- sentinel error handling tests are removed and VM name verification is lost.

---

## T8: Implement hints system in JSON output

**Worktree:** cc_3
**Files:** `internal/ui/formatter.go`, `internal/ui/formatter_test.go`, `internal/cmd/create.go`, `internal/cmd/ensure.go`
**Spec:** REQ-010-016

### Verdict: PASS

### Issues

#### 1. ensure.go uses package-level variables for cross-function state
- **File:** `internal/cmd/ensure.go:54-57` (unchanged from original)
- **Severity:** note
- **Description:** The `ensureProjCfg` and `ensureProjDir` package-level variables are set in `runEnsure` and read in `ensureCreate`. This is a pre-existing pattern (not introduced by this change) but is worth noting as it makes concurrent usage unsafe and complicates testing. Not introduced by this PR.

#### 2. Hints are hardcoded, not contextual per spec language
- **File:** `internal/cmd/create.go:217-219`, `internal/cmd/ensure.go:208-211`, `internal/cmd/ensure.go:255-257`
- **Severity:** should-fix
- **Description:** REQ-010-016 says hints must be "contextual to what just happened." The current implementation uses identical hardcoded hints for create and ensure-create (`"Export GITHUB_TOKEN..."`, `"Run sd config egress list..."`). The ensure-start path has only the credential hint. This is reasonable as a starting point but the spec gives these examples:
  - After `sd ensure` creates a new VM: hint about credential setup (done)
  - After `sd connect` with no credentials detected: hint about exporting tokens (not in scope)
  - After `sd exec` fails with a network error: hint about egress rules (not in scope)

  The hints themselves are static strings. The spec says "contextual to what just happened" -- for the create/ensure paths, the current hints are appropriate. However, there is no mechanism to suppress hints when credentials are already configured, which would make them truly contextual. This is acceptable for a first implementation given the spec says hints are non-critical and agents MAY ignore them.

#### 3. SuccessDataWithHints correctly omits hints when nil or empty
- **File:** `internal/ui/formatter.go:111-119`
- **Severity:** (positive observation)
- **Description:** The `jsonSuccessWithHints` struct uses `omitempty` on the `Hints` field, which correctly omits the field entirely when nil or when the slice is empty (both produce no JSON output). This matches the spec requirement: "Be omitted from the JSON output when there are no hints (the field is optional, not required to be an empty array)." Tests verify both nil and empty-slice cases.

#### 4. Human mode correctly ignores hints
- **File:** `internal/ui/formatter.go:117-118`
- **Severity:** (positive observation)
- **Description:** In human mode, `SuccessDataWithHints` calls `formatFunc` and does not print hints. Test `TestFormatter_HumanMode_SuccessDataWithHints_NoHintsInOutput` verifies this. Matches spec: "The hints field MUST NOT appear in non-JSON (human-readable) output."

#### 5. Missing: FailureDataWithHints variant
- **File:** `internal/ui/formatter.go`
- **Severity:** note
- **Description:** There is no `FailureDataWithHints` method. Currently hints are only emitted on success. The spec says "All sd commands that produce JSON output MUST support an optional hints field." This could apply to failure cases too (e.g., after a failed create, hint about checking prerequisites). However, the spec examples only show hints on success, and it says hints "MUST NOT change exit code or ok status." This is acceptable for now.

#### 6. Test coverage is thorough
- **File:** `internal/ui/formatter_test.go:313-427`
- **Severity:** (positive observation)
- **Description:** Tests cover: hints present, nil hints, empty hints, human mode suppression, and a property test across all hint variants ensuring valid JSON. The property test correctly asserts that empty/nil hints produce no `hints` field while non-empty hints produce the correct array.

#### 7. ensure.go already_running path does not use hints
- **File:** `internal/cmd/ensure.go:147-151`
- **Severity:** note
- **Description:** When the VM is already running, `ensureStart` and the already-running path use `f.SuccessData` (no hints). Only the create and start paths emit hints. This makes sense -- if the VM is already running, the user likely already knows about credential setup. Reasonable design choice.

### Summary

The hints implementation is clean and correct. The `SuccessDataWithHints` method and `jsonSuccessWithHints` struct are well-designed. The omitempty behavior correctly handles nil/empty hints. Tests are comprehensive. The integration into create.go and ensure.go is minimal and non-disruptive. The only concern is that hints are static rather than truly contextual, but the spec allows this and it is a reasonable first implementation.

---

## Cross-Task Observations

1. **T4 is the only blocker.** The architectural regression (bypassing backend.Exec for direct SSH) must be resolved before merge. T1 and T8 are clean.

2. **Spec alignment:** T1 and T8 faithfully implement their specs. T4 implements the credential injection correctly but via the wrong mechanism.

3. **Test quality:** T1 and T8 have excellent test coverage with edge cases, property tests, and round-trip tests. T4's tests are thorough for the new SSH-based approach but lose coverage of the original backend-based error paths.
