# Wave 2 QA Review

Reviewer: QA Agent
Date: 2026-04-09

---

## T2: Declarative provisioning pipeline

**Worktree:** cc_1
**Files reviewed:**
- `internal/provision/packages.go` + `packages_test.go`
- `internal/provision/setup.go` + `setup_test.go`
- `internal/provision/clone.go` + `clone_test.go`
- `internal/cmd/create.go` (runDeclarativePipeline)
- `internal/cmd/ensure.go` (calls doCreateVM which calls runDeclarativePipeline)

### Spec Compliance Checklist

#### REQ-009-004: Provisioning Pipeline Extension

| Acceptance Criterion | Status |
|---|---|
| Modules complete before any package installation begins | COVERED -- `doCreateVM` calls `runCreateProvision` before `runDeclarativePipeline` |
| Package installation completes before any setup command runs | COVERED -- `runDeclarativePipeline` calls packages then setup then repo in sequence |
| Setup commands complete before repository cloning begins | COVERED -- same as above |
| A failure in package installation prevents setup commands and repo clone from running | COVERED -- `TestInstallPackages_PipFailurePreventsSubsequent` tests fail-fast within packages; pipeline returns error on package failure, skip setup/repo |
| A failure in a setup command prevents repo clone from running | COVERED -- `TestRunSetupCommands_FailFast` tests this at setup level; pipeline returns error on setup failure |
| When no packages, setup, or repo fields are present, pipeline behaves identically to existing module-only pipeline | COVERED -- `runDeclarativePipeline` returns nil early when all three are empty |

#### REQ-009-005: Runtime Prerequisite Validation

| Acceptance Criterion | Status |
|---|---|
| pip packages without python module produces fatal error with documented message | COVERED -- `TestInstallPackages_PipPrereqMissing` |
| go packages without golang module produces fatal error with documented message | COVERED -- `TestInstallPackages_GoPrereqMissing` |
| npm packages without node/npm produces fatal error with documented message | COVERED -- `TestInstallPackages_NpmPrereqMissing` |
| cargo packages without rust module produces fatal error with documented message | COVERED -- `TestInstallPackages_CargoPrereqMissing` |
| Prerequisite validation runs inside the VM, not on the host | COVERED -- all prereq checks go through `execFn` which is the backend Exec abstraction |
| Prerequisite validation runs before any package installation begins | COVERED -- code loops through all managers for prereq checks first, then loops again for installs |
| When no packages are declared, prerequisite validation is skipped entirely | COVERED -- `TestInstallPackages_PrereqOnlyCheckedForPresentManagers` |

#### REQ-009-006: Package Installation Execution

| Acceptance Criterion | Status |
|---|---|
| apt packages are installed before pip packages | COVERED -- `TestInstallPackages_OrderAptPipNpmGoCargo` |
| pip packages are installed before npm packages | COVERED -- same test |
| npm packages are installed before go packages | COVERED -- same test |
| go packages are installed before cargo packages | COVERED -- same test |
| Multiple apt packages combined into single apt-get install | COVERED -- `TestInstallPackages_AptBatched` |
| Multiple pip packages combined into single pip3 install | COVERED -- `TestInstallPackages_PipBatched` |
| Each go package installed with separate go install invocation | COVERED -- `TestInstallPackages_GoPerPackage` |
| A failed package installation exits with non-zero status and actionable error | COVERED -- `TestInstallPackages_AptFailure_ReportsError` |
| apt-get update runs before apt-get install | COVERED -- `TestInstallPackages_AptUpdateBeforeInstall` |

#### REQ-009-007: Setup Command Execution

| Acceptance Criterion | Status |
|---|---|
| Setup commands run in the order they appear | COVERED -- `TestRunSetupCommands_Sequential` |
| A setup command that exits non-zero causes failure with command text and exit code | COVERED -- `TestRunSetupCommands_ErrorIncludesCommandAndExitCode` |
| Setup commands run as non-root user | COVERED -- `TestRunSetupCommands_RunsAsUser` |
| All setup commands run after all package installation completes | COVERED -- enforced by pipeline order in `runDeclarativePipeline` |

#### REQ-009-008: Repository Clone Execution

| Acceptance Criterion | Status |
|---|---|
| Repository is cloned into ~/projects/<vm-name> | COVERED -- `TestCloneRepo_TargetPath` |
| When branch is specified, clone checks out that branch | COVERED -- `TestCloneRepo_FreshCloneWithBranch` |
| When branch is not specified, default branch is used | COVERED -- `TestCloneRepo_FreshClone` (no --branch flag) |
| Clone failure produces fatal error with actionable message | COVERED -- `TestCloneRepo_AuthFailure`, `TestCloneRepo_NetworkFailure` |
| If ~/projects/<vm-name> already exists with correct remote, clone is skipped and git fetch runs | COVERED -- `TestCloneRepo_ExistingWithCorrectRemote_Fetches` |
| Clone uses credentials injected via environment variables, not persisted to disk | NOT-COVERED -- no test verifies that credentials flow through env vars; this is partially a design-level guarantee via `sd exec` (REQ-009-009) |

#### REQ-009-010: Idempotency Guarantees

| Acceptance Criterion | Status |
|---|---|
| Running provisioning twice with same .sd.yaml succeeds both times | NOT-COVERED -- no integration-level idempotency test exists |
| Second provisioning run completes faster than first | NOT-COVERED -- performance test not present (acceptable for unit tests) |
| sd init template includes comment warning that setup commands should be idempotent | COVERED -- tested in T3 (init_test.go) |

#### REQ-009-013: Failure Handling

| Acceptance Criterion | Status |
|---|---|
| Failed apt-get install reports which package(s) failed and shows last 20 lines | COVERED -- `TestInstallPackages_AptFailure_ReportsError`, `TestInstallPackages_Last20Lines` |
| Failed setup command reports command text and exit code | COVERED -- `TestRunSetupCommands_ErrorIncludesCommandAndExitCode` |
| Failed git clone reports repository URL and distinguishes auth vs network failure | COVERED -- `TestCloneRepo_AuthFailure`, `TestCloneRepo_NetworkFailure` |
| After a failure, subsequent pipeline steps do not execute | COVERED -- `TestInstallPackages_PipFailurePreventsSubsequent`, `TestRunSetupCommands_FailFast` |
| --json error output includes code, message, step, details fields | NOT-COVERED -- no test verifies the full JSON error structure with step/details fields as specified in spec |

### Missing Tests

1. **No rapid / property-based tests for T2 code.** The project convention requires `pgregory.net/rapid` tests for invariants. Packages.go, setup.go, and clone.go have zero property-based tests. Suggested properties:
   - For any arbitrary `PackageConfig`, `InstallPackages` calls managers in apt->pip->npm->go->cargo order.
   - For any list of setup commands, they execute in declaration order.
   - `tailLines(s, n)` always returns at most n lines from the end.
   - For any non-empty repo string, `CloneRepo` always attempts to create `~/projects/<vmName>`.

2. **No test for npm batch install** (analogous to `TestInstallPackages_AptBatched` and `TestInstallPackages_PipBatched`). Cargo batch install also untested at the assertion level.

3. **No test for `set -eux -o pipefail` preamble on setup commands specifically verifying the safety flags** -- `TestRunSetupCommands_HasPreamble` checks the prefix string but doesn't verify all setup commands get it (only checks first).

4. **No integration test for `runDeclarativePipeline`** at the cmd level -- the function is tested indirectly through create/ensure integration but there is no dedicated test that constructs a `ProjectConfig` with packages+setup+repo and verifies the pipeline runs them in order via the backend.

5. **No test for the `ExistingDifferentRemote` error path in clone when branch is also set.**

6. **JSON error structure (REQ-009-013)** -- spec requires `code`, `message`, `step`, `details` fields in JSON error output. The `runDeclarativePipeline` wraps errors in `ui.CLIError` with `Code` and `Message` but does NOT include `step` or `details` fields. This is a **spec compliance gap** in both code and tests.

### Test Quality Assessment

- Test names are descriptive and follow `TestFunctionName_Scenario` convention. Good.
- Tests are properly isolated via `execFn` mock injection. Good.
- Assertions check the right things (error messages, command ordering, script content).
- `tailLines` has its own focused unit tests. Good.

### Verdict: **NEEDS-CHANGES**

**Must-fix:**
- MF-1: Add rapid property-based tests for packages, setup, and clone modules.
- MF-2: Add test for JSON error output structure (REQ-009-013 acceptance criteria: `code`, `message`, `step`, `details`). Either update `ui.CLIError` to support `step`/`details` or document why the spec acceptance criterion is satisfied differently.

**Should-fix:**
- SF-1: Add npm and cargo batch install tests.
- SF-2: Add an integration test for `runDeclarativePipeline` at the cmd level.
- SF-3: Add idempotency test for clone (call `CloneRepo` twice, verify second call fetches instead of cloning).

---

## T3: sd init updates

**Worktree:** cc_2
**Files reviewed:**
- `internal/cmd/init.go`
- `internal/cmd/init_test.go`

### Spec Compliance Checklist

#### REQ-009-012: sd init Template Updates

| Acceptance Criterion | Status |
|---|---|
| `sd init` in a git repository with an origin remote populates the repo field | COVERED -- `TestInitCommand_GitRemotePopulatesRepo` |
| `sd init` in a directory without a git remote leaves repo absent | COVERED -- `TestInitCommand_NoGitRemoteLeavesRepoAbsent` |
| Generated .sd.yaml includes a commented-out packages section with examples | COVERED -- `TestInitCommand_IncludesCommentedPackagesSection` |
| Generated .sd.yaml includes a commented-out setup section with examples | COVERED -- `TestInitCommand_IncludesCommentedSetupSection` |
| Commented-out setup section includes a note about idempotency | COVERED -- `TestInitCommand_SetupSectionIncludesIdempotencyNote` |

### Missing Tests

1. **No rapid / property-based tests.** The project convention requires them. Suggested properties:
   - For any directory name, `detectProjectType` returns modules that always include "base".
   - The generated .sd.yaml is always valid YAML that can be parsed back.

2. **No test that the packages comment includes examples for ALL five managers.** `TestInitCommand_IncludesCommentedPackagesSection` checks for apt, pip, npm, go, cargo individually, which is good -- but a more rigorous test would verify the full `packagesComment` constant includes all five. (Current test does check all five, so this is adequately covered.)

3. **No test that `detectGitRemote` handles SSH remote URLs** (e.g., `git@github.com:user/repo.git`). Only HTTPS is tested. The mock always returns a fixed value, so this is a mock limitation, not a code bug -- but a test with an SSH URL mock would improve coverage.

4. **No test for `detectGitRemote` failure modes** (e.g., git not installed, no remote named "origin"). The default mock returns "" which covers the "no remote" case, but there's no explicit test for the real function's error handling.

### Test Quality Assessment

- Tests properly mock `detectGitRemote` and `getWorkingDir` for isolation. Good.
- `setupInitTest` correctly resets flags and mocks between tests. Good.
- Test names are descriptive.
- JSON output test (`TestInitCommand_JSON`) verifies the full envelope structure. Good.

### Verdict: **NEEDS-CHANGES**

**Must-fix:**
- MF-1: Add at least one rapid property-based test (e.g., generated .sd.yaml is always parseable YAML for any directory name input).

**Should-fix:**
- SF-1: Add test with SSH remote URL mock to verify repo field formatting.
- SF-2: Add explicit test that `detectGitRemote` returns "" when called with a non-git directory (testing the real function, not the mock).

---

## T5: sd quick-start scaffold

**Worktree:** cc_3
**Files reviewed:**
- `internal/cmd/quick_start.go`
- `internal/cmd/quick_start_test.go`

### Spec Compliance Checklist

#### REQ-010-001: Quick-Start Command Registration

| Acceptance Criterion | Status |
|---|---|
| sd quick-start is registered as a command under the "Getting Started" group | COVERED -- `TestQuickStartCommand_Registered` checks `GroupID == "start"`. **NOTE:** Spec says "Getting Started" group but code uses GroupID "start". This may be correct if the group display name is "Getting Started" but the ID is "start". Acceptable if consistent with existing command group definitions. |
| sd help displays quick-start under the "Getting Started" group header | NOT-COVERED -- no test captures help output and checks group rendering. `TestQuickStartCommand_HelpExitsZero` checks it exits zero and mentions --check, but does not verify group placement in help output. |
| Running sd quick-start with no flags outputs the runbook to stdout | COVERED -- `TestQuickStartCommand_DefaultOutput` (outputs placeholder text) |
| The command MUST support --json for structured output | COVERED -- `TestQuickStartCommand_JSONOutput` |
| Command file MUST be at internal/cmd/quick_start.go | COVERED -- file exists at correct location |

#### REQ-010-002: Quick-Start Flags

| Acceptance Criterion | Status |
|---|---|
| sd quick-start outputs Markdown to stdout | COVERED -- `TestQuickStartCommand_DefaultOutput` (currently outputs placeholder, not real Markdown runbook) |
| sd quick-start --check outputs JSON assessment to stdout | COVERED -- `TestQuickStartCommand_CheckReturnsValidJSON` |
| sd quick-start --json outputs runbook wrapped in {"ok": true, "data": {"runbook": "..."}} | COVERED -- `TestQuickStartCommand_JSONOutput` |
| sd quick-start --check --json outputs same JSON assessment as --check alone | COVERED -- `TestQuickStartCommand_CheckAlwaysJSON` |

#### REQ-010-003: Runbook Output Format

| Acceptance Criterion | Status |
|---|---|
| Runbook output is valid Markdown | NOT-COVERED -- current implementation outputs a placeholder string, not the actual runbook. This is expected for a scaffold but the acceptance criterion is not testable yet. |
| Every section contains at least one labeled block | NOT-COVERED -- runbook content not implemented yet (placeholder only) |
| Runbook is addressed to "you" (the agent) | NOT-COVERED -- runbook content not implemented yet |
| When runbook references user, uses "ask the user" or "explain to the user" phrasing | NOT-COVERED -- runbook content not implemented yet |

#### REQ-010-015: Check Flag -- Setup Assessment

| Acceptance Criterion | Status |
|---|---|
| sd quick-start --check outputs valid JSON matching the structure | COVERED -- `TestQuickStartCommand_CheckReturnsValidJSON` checks all fields |
| All fields are present in the output, even when values are false or empty | COVERED -- `TestProperty_QuickStartCheckAlwaysValid` checks all fields across multiple state combinations |
| needs_setup is true when any required component is missing | COVERED -- `TestQuickStartCommand_CheckNoConfig_NeedsSetup`, `TestQuickStartCommand_CheckStoppedVM`, `TestQuickStartCommand_CheckRepoNotCloned` |
| issues contains actionable descriptions for each problem found | COVERED -- multiple tests check specific issue strings |
| issues is an empty array when needs_setup is false | COVERED -- `TestQuickStartCommand_CheckComplete_NeedsSetupFalse` |
| Credential checks test host environment variables | COVERED -- `TestQuickStartCommand_CheckCredentials` with table-driven cases |
| repo_cloned is verified by checking inside the VM | COVERED -- `TestQuickStartCommand_CheckRepoNotCloned` uses exec handler returning exit code 1 |

### Spec Compliance Gap: `packages_declared` field

The spec (REQ-010-015) defines `packages_declared` as a required field in the check output:
```json
"packages_declared": {"apt": 3, "pip": 0, "npm": 1}
```

The implementation's `quickStartCheckResult` struct does NOT include a `PackagesDeclared` field. This field is missing from both the code and the tests. This is a **spec compliance gap**.

### Missing Tests

1. **No rapid / property-based tests for T5 code.** Suggested properties:
   - For any combination of (config present, VM state, credentials set), the --check output always contains all required fields.
   - `needs_setup` is true if and only if `issues` is non-empty.
   - `NeedsSetup == (len(Issues) > 0)` is an invariant.

   Note: `TestProperty_QuickStartCheckAlwaysValid` is a good approximation of property testing using explicit state combinations, but it is not a true rapid property test with generated inputs.

2. **No test for `--check --json` combination** as a distinct test case. `TestQuickStartCommand_CheckAlwaysJSON` tests `--check` without `--json` and verifies JSON is produced, but the spec says `--check --json` should produce the same as `--check` alone. A test should explicitly pass both flags.

3. **No test for the `packages_declared` field** -- missing from implementation.

4. **No test for error in `findProjectConfigFunc`** -- the code has an error path (`findErr != nil`) but no test exercises it with a non-nil error to verify the issue is added correctly.

5. **No test for backend unavailability during --check** (e.g., `getBackendFunc` returns error). The code handles this gracefully (skips VM checks) but it's not tested.

### Test Quality Assessment

- `setupQuickStartTest` properly mocks all external dependencies (backend, env vars, config lookup, working dir). Excellent isolation.
- `TestProperty_QuickStartCheckAlwaysValid` covers 5 state combinations and validates all field presence -- good pseudo-property test.
- `TestQuickStartCommand_CheckCredentials` is properly table-driven. Good.
- Test names are descriptive.
- The `TestQuickStartCommand_RejectsExtraArgs` and `TestQuickStartCommand_NoConfigRequired` tests cover edge cases. Good.

### Verdict: **NEEDS-CHANGES**

**Must-fix:**
- MF-1: Add `packages_declared` field to `quickStartCheckResult` struct per spec REQ-010-015. Add corresponding test.
- MF-2: Add at least one rapid property-based test (e.g., needs_setup == (len(issues) > 0) for generated state combinations).

**Should-fix:**
- SF-1: Add explicit test for `--check --json` combined flags.
- SF-2: Add test for `findProjectConfigFunc` returning an error.
- SF-3: Add test for backend unavailability during --check.
- SF-4: Note that runbook content tests (REQ-010-003) are blocked until the runbook is implemented. The scaffold approach is reasonable but should be tracked as follow-up work.

---

## Summary

| Task | Verdict | Must-Fix Count | Should-Fix Count |
|------|---------|----------------|------------------|
| T2: Declarative provisioning pipeline | NEEDS-CHANGES | 2 | 3 |
| T3: sd init updates | NEEDS-CHANGES | 1 | 2 |
| T5: sd quick-start scaffold | NEEDS-CHANGES | 2 | 4 |

### Cross-cutting Findings

1. **No rapid property-based tests in any of the three tasks.** This is a project-wide convention (AGENTS.md: "Property-based tests using pgregory.net/rapid for invariants"). All three tasks violate this. The existing codebase has rapid tests in resolver, module, session, security, config, and backend packages, demonstrating the pattern is established. New code should follow it.

2. **JSON error structure gap (T2, REQ-009-013).** The spec defines `--json` error output with `step` and `details` fields. The `ui.CLIError` type used by `runDeclarativePipeline` only has `Code` and `Message`. Either the error type needs extension or the spec acceptance criterion needs revision. This affects both code and tests.

3. **`packages_declared` field missing (T5, REQ-010-015).** The spec explicitly defines this field in the --check output structure. It is absent from the implementation. A straightforward addition.

4. **Test isolation is consistently good across all three tasks.** External dependencies are properly mocked. No tests reach out to real file systems, real git repos, or real VMs.

5. **Test naming is consistently good.** Follows `TestFunction_Scenario` convention throughout.
