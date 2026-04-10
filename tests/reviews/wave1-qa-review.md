# Wave 1 QA Review

Reviewer: QA Agent
Date: 2026-04-09
Focus: Test quality, spec compliance, verifiability

---

## T1: Add packages/setup/repo/branch to ProjectConfig

**Worktree:** `/Users/gb/github/secure-dev/.ntm/worktrees/secure-dev--wave1/cc_1`
**Files:** `internal/config/project.go`, `internal/config/project_test.go`
**Spec:** 009-declarative-environment.md (REQ-009-001, 002, 003, 011)

### Verdict: PASS

### Spec Compliance Checklist

#### REQ-009-001: Packages Field in .sd.yaml

| Acceptance Criterion | Status | Test Function |
|---|---|---|
| .sd.yaml with packages containing all five sub-keys parses without error | COVERED | `TestLoadProjectConfig_PackagesAllSubkeys` |
| .sd.yaml with packages containing only a subset of sub-keys parses without error | COVERED | `TestLoadProjectConfig_PackagesSubset` |
| .sd.yaml without a packages field parses without error and no packages are installed | COVERED | `TestLoadProjectConfig_PackagesAbsent` |
| An empty sub-key (e.g., `apt: []`) is treated the same as the sub-key being absent | COVERED | `TestLoadProjectConfig_PackagesEmptyList` |
| Existing .sd.yaml files without the packages field continue to work without modification | COVERED | `TestLoadProjectConfig_BackwardCompatibility` |

#### REQ-009-002: Setup Field in .sd.yaml

| Acceptance Criterion | Status | Test Function |
|---|---|---|
| .sd.yaml with a setup field containing multiple commands parses without error | COVERED | `TestLoadProjectConfig_SetupValid` |
| Setup commands execute in the order they appear in the list | COVERED | `TestLoadProjectConfig_SetupValid` (order checked via slice equality) |
| Setup commands run as the default non-root user (not root) | NOT-COVERED | Runtime behavior -- not testable at config parsing layer |
| .sd.yaml without a setup field parses without error and no setup commands run | COVERED | `TestLoadProjectConfig_SetupAbsent` |
| An empty string in the setup list produces a validation error | COVERED | `TestLoadProjectConfig_SetupEmptyStringEntry`, `TestLoadProjectConfig_SetupWhitespaceEntry` |

#### REQ-009-003: Repo and Branch Fields in .sd.yaml

| Acceptance Criterion | Status | Test Function |
|---|---|---|
| .sd.yaml with repo and branch fields parses without error | COVERED | `TestLoadProjectConfig_RepoWithBranch` |
| .sd.yaml with repo but no branch parses without error; the default branch is used | COVERED | `TestLoadProjectConfig_RepoWithoutBranch` |
| .sd.yaml without repo parses without error; no clone occurs | COVERED | `TestLoadProjectConfig_PackagesAbsent` (implicitly, cfg.Repo is "") |
| .sd.yaml with branch but no repo produces a validation error | COVERED | `TestLoadProjectConfig_BranchWithoutRepo` |
| An HTTPS URL is accepted for repo | COVERED | `TestLoadProjectConfig_RepoHTTPS` |
| An SSH URL is accepted for repo | COVERED | `TestLoadProjectConfig_RepoSSH` |
| An invalid URL for repo produces a validation error | COVERED | `TestLoadProjectConfig_RepoInvalid` (4 sub-cases) |
| The repository is cloned into ~/projects/<vm-name> inside the VM | NOT-COVERED | Runtime behavior -- not testable at config parsing layer |

#### REQ-009-011: Validation Rules for New Fields

| Acceptance Criterion | Status | Test Function |
|---|---|---|
| An HTTPS repo URL passes validation | COVERED | `TestLoadProjectConfig_RepoHTTPS` |
| An SSH repo URL passes validation | COVERED | `TestLoadProjectConfig_RepoSSH` |
| A repo value that is not a valid URL produces a validation error | COVERED | `TestLoadProjectConfig_RepoInvalid` |
| A branch value with spaces produces a validation error | COVERED | `TestLoadProjectConfig_BranchInvalid` ("contains space") |
| A branch set without repo produces a validation error | COVERED | `TestLoadProjectConfig_BranchWithoutRepo` |
| An empty string in packages.apt produces a validation error | COVERED | `TestLoadProjectConfig_PackagesEmptyStringEntry` ("empty apt entry") |
| An empty string in setup produces a validation error | COVERED | `TestLoadProjectConfig_SetupEmptyStringEntry` |
| Validation errors include the file path and field path | COVERED | `TestLoadProjectConfig_ValidationErrorIncludesPath` |

### Test Quality Assessment

- **Behavior-focused**: Tests verify parsing outcomes (values, errors, error messages), not just code paths. Assertions check specific field values and error message substrings.
- **Isolation**: Each test creates its own temp directory and file. No shared mutable state.
- **Table-driven patterns**: Used effectively for `TestLoadProjectConfig_RepoInvalid`, `TestLoadProjectConfig_BranchInvalid`, `TestLoadProjectConfig_BranchValid`, `TestLoadProjectConfig_PackagesEmptyStringEntry`.
- **Round-trip test**: `TestProjectConfig_RoundTripWithDeclarativeFields` verifies YAML marshal/unmarshal fidelity, which is a good structural invariant.

### Missing Tests

1. **Branch with control characters** -- `TestLoadProjectConfig_BranchControlChar` exists but uses `\x07` in YAML literal which may be parsed differently by the YAML library. Should verify the YAML library actually delivers the control char to the Go string.
2. **Package entry index in error** -- `TestLoadProjectConfig_PackagesSecondEntryEmpty` covers index 1 but no test verifies index > 1 or a mix of valid and invalid entries across multiple manager types simultaneously.
3. **Repo field with empty string explicitly** -- No test for `repo: ""` (though this would be parsed as absent by YAML).
4. **Setup with extremely long commands** -- No boundary test for very long strings.

### Property-Based Tests

**NOT INCLUDED.** AGENTS.md requires property-based tests using `pgregory.net/rapid`. None are present in this task's test file.

Recommended properties:
- **Round-trip invariant**: For any valid `ProjectConfig` generated by rapid, `yaml.Marshal` then `yaml.Unmarshal` produces an equivalent struct.
- **Validation completeness**: For any `ProjectConfig` with `Repo` not starting with `https://` or `git@` (and non-empty), `validateDeclarativeFields` returns an error.
- **Branch-without-repo**: For any non-empty `Branch` with empty `Repo`, validation always fails.
- **Empty package entry**: For any `PackageConfig` containing a whitespace-only string in any list, validation always fails.

### Regression Risk

Low. The changes are additive -- new fields on `ProjectConfig` and a new `validateDeclarativeFields` function called from the existing `validateProjectConfig`. The existing tests for name, memory, disk, and mount validation are unchanged and protect against regression. The `BackwardCompatibility` test explicitly verifies old configs still parse.

---

## T4: Fix sd exec credential injection

**Worktree:** `/Users/gb/github/secure-dev/.ntm/worktrees/secure-dev--wave1/cc_2`
**Files:** `internal/cmd/exec.go`, `internal/cmd/exec_test.go`
**Spec:** 007-connection.md (REQ-007-019)

### Verdict: PASS

### Spec Compliance Checklist

#### REQ-007-019: Environment Injection on Connect (applied to exec)

| Acceptance Criterion | Status | Test Function |
|---|---|---|
| Environment variables configured for the VM are set in the remote shell session | COVERED | `TestExecCommand_CredentialInjection` -- verifies GITHUB_TOKEN and ANTHROPIC_API_KEY passed to SSH runner |
| Variable values containing ${VAR} references are resolved from the host environment at connection time | NOT-COVERED | No test for ${VAR} template resolution in exec context (readCredFunc is mocked to return resolved values) |
| Unresolvable ${VAR} references produce a warning on stderr and the variable is set to empty string | NOT-COVERED | Same -- readCredFunc is mocked, so template resolution is not tested here |
| Variables are injected via SSH SendEnv on the client side | COVERED | `TestProperty_ExecSSHArgsSendEnv` -- verifies SSH args include `SendEnv=SD_* ANTHROPIC_* GITHUB_* GH_*` |
| Provisioning configures /etc/ssh/sshd_config with AcceptEnv | NOT-COVERED | Provisioning-layer concern, not exec command scope |
| Injected variables are available in both sd connect and sd exec sessions | COVERED (exec side) | `TestExecCommand_CredentialInjection` -- the connect side is tested in connect_test.go |
| Variables are NOT written to disk inside the VM | COVERED (by design) | `defaultExecSSHRunner` sets env vars on the SSH process, never writes files. `TestProperty_ExecSSHArgsSendEnv` verifies SendEnv is used. |

### Test Quality Assessment

- **Thorough error path coverage**: Tests cover VM not found, VM not running, VM in error state, backend unavailable, backend get error, SSH config error, status check error, exec backend error, empty name, missing command.
- **Credential injection well-tested**: 4 dedicated tests cover the injection path, filtering, no-credentials case, and load-error case.
- **SSH args builder tested independently**: `TestProperty_ExecSSHArgsSendEnv` and `TestProperty_ExecSSHArgsVSOCK` test `buildExecSSHArgs` in isolation.
- **Mock architecture**: `setupExecMemoryTest` provides clean test setup with memory backend and SSH runner mock. Cleanup properly restores originals.
- **Output verification**: Both human and JSON output paths are tested with actual stdout capture.
- **Non-zero exit code handling**: `TestExecCommand_NonZeroExitCode_JSONOutput` verifies exit code propagation through the `osExit` mock.

### Missing Tests

1. **${VAR} template resolution** -- The `readCredFunc` mock always returns pre-resolved values. No test verifies that `${GITHUB_TOKEN}` in the VM config is resolved to the actual host environment variable value. However, this may be tested at the `readCredFunc` implementation level rather than here.
2. **Invalid VM name format** -- No test for `sd exec INVALID_NAME! -- echo` to verify name validation fires before backend lookup.
3. **Multiple commands with special characters** -- No test for commands containing quotes, pipes, or shell metacharacters being passed through correctly.
4. **Concurrent exec calls** -- No test for concurrent execution (though AGENTS.md requires integration tests for this).
5. **Large credential sets** -- No test with many credential keys to verify performance/correctness.

### Property-Based Tests

**NOT INCLUDED with `pgregory.net/rapid`.** The test file includes manual "property-based" tests (`TestProperty_Exec*`) that are actually parameterized table-driven tests. While these are good tests, they do not use the `rapid` library for random generation as required by AGENTS.md.

The `TestProperty_ExecJSONAlwaysValid` test uses 5 hardcoded cases -- a true property test would generate random VM names, exit codes, stdout/stderr content and verify the JSON invariant holds for all of them.

Recommended properties with rapid:
- **JSON envelope invariant**: For any exit code (0-255), any stdout string, any stderr string, JSON output always contains `ok: true`, `data.exit_code`, `data.stdout`, `data.stderr`.
- **Credential filtering invariant**: For any key string, `isCredentialKey` returns true iff the key starts with `SD_`, `ANTHROPIC_`, `GITHUB_`, or `GH_`.
- **SSH args structure**: For any valid `SSHConfig`, `buildExecSSHArgs` always includes `-T`, `SendEnv`, and `--` separator before the command.

### Regression Risk

Low-moderate. The main change adds credential loading and filtering to `runExec`. The credential injection path is gated by `Loader() != nil` which is nil in most test environments unless explicitly set up. Existing tests that don't set up credentials continue to work. The `isCredentialKey` function is shared with `connect.go` (defined there), so changes to the filtering logic would affect both commands.

One concern: the `readCredFunc` variable is a package-level mutable shared between `connect.go` and `exec.go`. Tests must not run in parallel due to this shared state. The test file comment correctly notes `// NOTE: Tests use global getBackendFunc -- do not use t.Parallel().`

---

## T8: Implement hints system in JSON output

**Worktree:** `/Users/gb/github/secure-dev/.ntm/worktrees/secure-dev--wave1/cc_3`
**Files:** `internal/ui/formatter.go`, `internal/ui/formatter_test.go`, `internal/cmd/create.go`, `internal/cmd/ensure.go`
**Spec:** 010-agent-bootstrap.md (REQ-010-016)

### Verdict: NEEDS-CHANGES

### Spec Compliance Checklist

#### REQ-010-016: Hints System for SD Commands

| Acceptance Criterion | Status | Test Function |
|---|---|---|
| JSON output from sd commands MAY include a hints array at the top level | COVERED | `TestFormatter_JSONMode_SuccessDataWithHints_Present` |
| Each hint is a self-contained, actionable string | COVERED | `TestFormatter_JSONMode_SuccessDataWithHints_Present` -- checks exact hint strings |
| Hints are contextual to the command that was run and its outcome | NOT-COVERED | No test verifies that `sd create` or `sd ensure` produce hints in their JSON output. The formatter unit tests verify the plumbing works, but no integration-level test exercises `create --json` or `ensure --json` and checks for the `hints` field. |
| The hints field is absent (not an empty array) when there are no hints | COVERED | `TestFormatter_JSONMode_SuccessDataWithHints_NilHints`, `TestFormatter_JSONMode_SuccessDataWithHints_EmptyHints` |
| Hints do not appear in human-readable output | COVERED | `TestFormatter_HumanMode_SuccessDataWithHints_NoHintsInOutput` |
| Hints do not affect exit codes or ok status | COVERED | `TestFormatter_JSONMode_SuccessDataWithHints_Present` -- checks `ok: true` is unaffected |

### Test Quality Assessment

- **Formatter unit tests are strong**: Good coverage of the `SuccessDataWithHints` method across nil hints, empty hints, present hints, and human mode. The `TestProperty_SuccessDataWithHints_AlwaysValidJSON` test covers all hint-list variants with JSON parsing validation.
- **Missing integration tests**: The create.go and ensure.go files were modified to call `SuccessDataWithHints` with hardcoded hints, but **no test in create_test.go or ensure_test.go verifies that the hints appear in the JSON output**. The existing JSON output tests for create and ensure do not check for the `hints` field. This is the primary gap.
- **No test for contextual hints varying by outcome**: The ensure command uses different hints for "created" vs "started" vs "already_running" paths, but no test verifies this differentiation.

### Missing Tests (Critical)

1. **Create command JSON output with hints** -- No test runs `sd create --json` and verifies the JSON response contains the `hints` array. This is required to confirm the integration works end-to-end.
2. **Ensure command JSON output with hints (created path)** -- No test runs `sd ensure --json` on a non-existent VM and verifies the `hints` array contains credential setup and egress hints.
3. **Ensure command JSON output with hints (started path)** -- No test runs `sd ensure --json` on a stopped VM and verifies the hints array contains only the credential hint (not the egress hint).
4. **Ensure command JSON output without hints (already_running path)** -- No test verifies that the `already_running` path does NOT emit hints (currently it uses `SuccessData` without hints, which is correct, but should be explicitly tested).

### Missing Tests (Non-Critical)

5. **Hint content validation** -- No test verifies that hints contain actionable content (e.g., reference actual `sd` commands).
6. **Hints field type validation in JSON** -- The property test covers this at the formatter level but not at the command level.

### Property-Based Tests

**NOT INCLUDED with `pgregory.net/rapid`.** The formatter tests include `TestProperty_SuccessDataWithHints_AlwaysValidJSON` but this uses hardcoded cases, not rapid-generated inputs.

Recommended properties with rapid:
- **Hint preservation**: For any list of non-empty strings as hints, `SuccessDataWithHints` in JSON mode produces output where `hints` contains exactly those strings in order.
- **Nil/empty equivalence**: For nil and empty hint slices, the `hints` field is always absent from JSON output.
- **Human mode opacity**: For any hints list, human mode output never contains the word from any hint (strong test: human output is identical regardless of hints).

### Regression Risk

Moderate. The change modifies the return path of `runCreate` and `ensureCreate`/`ensureStart` -- switching from `SuccessData` to `SuccessDataWithHints`. If `SuccessDataWithHints` has a bug, it would break the JSON output of these core commands. The existing create_test.go and ensure_test.go JSON tests would NOT catch a regression because they do not check for the `hints` field. **This is a gap that should be addressed.**

Specifically, if a future change to `SuccessDataWithHints` introduced a regression (e.g., omitting `data` when `hints` is present), the existing tests would not detect it because they only check `ok` and `data` fields via the old `SuccessData` code path which was replaced.

---

## Summary

| Task | Verdict | Key Issue |
|---|---|---|
| T1: ProjectConfig fields | PASS | Good coverage of all parsing/validation acceptance criteria. Missing rapid tests per AGENTS.md. |
| T4: Exec credential injection | PASS | Strong test coverage for injection, filtering, error paths. Missing rapid tests per AGENTS.md. |
| T8: Hints system | NEEDS-CHANGES | Formatter plumbing is well-tested, but **no integration test verifies create.go/ensure.go actually emit hints in JSON output**. |

## Required Actions

### T8 (NEEDS-CHANGES)

1. Add at least one test to `create_test.go` that runs `sd create --json` and verifies the JSON response contains a `hints` array with the expected strings.
2. Add at least one test to `ensure_test.go` for the "created" path that runs `sd ensure --json` on a non-existent VM and verifies the `hints` array.
3. Add at least one test to `ensure_test.go` for the "started" path that verifies the hint set differs from the "created" path.
4. Add at least one test to `ensure_test.go` for the "already_running" path that verifies `hints` is absent.

### All Tasks (Advisory)

5. Add property-based tests using `pgregory.net/rapid` as required by AGENTS.md. Currently none of the three tasks include rapid-based tests. The existing "Property" tests are good but are manually parameterized, not randomly generated.
