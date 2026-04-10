# Wave 3 QA Review

**Reviewer:** QA agent
**Date:** 2026-04-09
**Scope:** T6 (Quick-start runbook output, cc_1) and T7 (Quick-start --check enhancement, cc_2)
**Spec:** specs/010-agent-bootstrap.md

---

## T6: Quick-start runbook output (cc_1)

**Worktree:** `.ntm/worktrees/secure-dev--wave3/cc_1`
**Files changed:** `internal/cmd/quick_start.go`, `internal/cmd/quick_start_test.go`
**Files added:** `internal/cmd/templates/quick_start_runbook.md`

### Test results

All 30 quick-start tests pass. The rapid property test (`TestRapid_NeedsSetupTrueIffComponentMissing`) passes.

### Requirement-by-requirement acceptance criteria coverage

#### REQ-010-004: Section 1 -- Introduction and Education

| Acceptance criterion | Test? | Verdict |
|---|---|---|
| Section 1 instructs the agent to explain workspace isolation to the user | `TestRunbook_Section1_IntroductionAndEducation` checks for "isolated from your personal files" | PASS |
| Section 1 explains the purpose of isolation (protection from malicious code) | Same test checks for "malicious" | PASS |
| Section 1 does not use undefined jargon | `TestRunbook_SecurityEducation_NoUndefinedJargon` checks jargon terms are defined before use | PASS |
| Section 1 provides suggested explanation text that is 2-4 sentences | Same test checks for specific phrases ("passwords, SSH keys, browser sessions") but does NOT programmatically count sentences | PARTIAL -- the runbook content qualitatively meets this (4 sentences in the blockquote) but there is no automated sentence-count assertion |

#### REQ-010-005: Section 2 -- Prerequisites

| Acceptance criterion | Test? | Verdict |
|---|---|---|
| Section 2 includes the command `sd doctor --json` | `TestRunbook_Section2_Prerequisites` | PASS |
| Section 2 provides interpretation guidance for the doctor output | Runbook includes interpretation table and instructions; test checks for `brew install lima` and `Docker Desktop` | PASS |
| Section 2 includes remediation instructions for each possible failed check | Test checks for Lima and Docker remediation; runbook covers ssh, lima, docker, sd_home | PASS |
| Section 2 instructs the agent to stop and assist the user if prerequisites are missing | Runbook text says "tell the user what to install and wait"; not directly asserted in tests | PARTIAL -- no explicit test assertion for "stop/wait" language |

#### REQ-010-006: Section 3 -- Project Analysis

| Acceptance criterion | Test? | Verdict |
|---|---|---|
| Section 3 includes the detection table with at least the 10 entries listed | `TestRunbook_Section3_DetectionTable` checks all 10 required file patterns | PASS |
| Section 3 instructs the agent to use its own judgment beyond the table | Test checks for "starting point, not exhaustive" | PASS |
| Section 3 instructs the agent to present findings and ask the user for confirmation | Test checks for "Are there other tools or packages" | PASS |
| Section 3 explicitly states the agent must not proceed without user input on dependencies | Test checks for "Do not silently assume" | PASS |

#### REQ-010-007: Section 4 -- Backend Selection

| Acceptance criterion | Test? | Verdict |
|---|---|---|
| Section 4 references `sd doctor --json` output for backend availability | Runbook says "based on the `sd doctor --json` output from Section 2"; test checks for "full virtual machine" and "container" | PASS |
| Section 4 provides plain-language descriptions of each backend option | Test checks for "full virtual machine" and "container" | PASS |
| Section 4 instructs the agent to ask the user when multiple backends are available | Test checks for "Which would you prefer" | PASS |
| Section 4 instructs the agent to auto-select and inform when only one backend is available | Test checks for "only one backend is available" | PASS |

#### REQ-010-008: Section 5 -- Environment Configuration

| Acceptance criterion | Test? | Verdict |
|---|---|---|
| Section 5 instructs the agent to detect the git remote URL | `TestRunbook_Section5_EnvironmentConfiguration` checks for "git remote get-url origin" | PASS |
| Section 5 maps detected runtimes to `.sd.yaml` modules | Test checks for "sd init" (modules are set via sd init --modules); runbook instructs setting modules | PASS |
| Section 5 maps detected tools to `.sd.yaml` packages | Runbook instructs "Set packages based on additional tools"; test checks for "sd init" | PASS |
| Section 5 instructs the agent to show the config and ask for user confirmation | Test checks for "Want me to adjust anything" | PASS |
| Section 5 includes a plain-language explanation of network restrictions | Test checks for "approved websites" | PASS |

#### REQ-010-009: Section 6 -- VM Creation

| Acceptance criterion | Test? | Verdict |
|---|---|---|
| Section 6 includes the `sd ensure` command | `TestRunbook_Section6_VMCreation` | PASS |
| Section 6 provides estimated creation time per backend | Test checks for "couple of minutes" and "few seconds" | PASS |
| Section 6 instructs the agent to communicate progress to the user | Runbook includes "I'll let you know when it's ready"; test covers time estimates | PASS |
| Section 6 includes error handling guidance | Runbook includes 4-step error handling; test does not explicitly assert error handling text | PARTIAL -- no test for error handling guidance specifically |

#### REQ-010-010: Section 7 -- Credential Setup

| Acceptance criterion | Test? | Verdict |
|---|---|---|
| Section 7 provides plain-language explanation of why scoped tokens are needed | `TestRunbook_Section7_CredentialSetup` checks for "only has access to this specific" | PASS |
| Section 7 walks through fine-grained PAT creation with specific steps | Test checks for "github.com/settings/tokens", "Contents", "Pull requests" | PASS |
| Section 7 defines the term "fine-grained personal access token" after the plain-language explanation | Test checks the term is present; `TestRunbook_SecurityEducation_NoUndefinedJargon` checks definition precedes term | PASS |
| Section 7 specifies the minimal token scopes (Contents, Pull requests, single repo) | Test checks for "Contents" and "Pull requests" | PASS |
| Section 7 instructs the agent to ask about API keys, not assume | Test checks for "Do you have API keys" | PASS |
| Section 7 explains that credentials are injected at connect time, not persisted | Test checks for "never saved to disk inside it" | PASS |

#### REQ-010-011: Section 8 -- Repo Clone and Verification

| Acceptance criterion | Test? | Verdict |
|---|---|---|
| Section 8 provides the clone-not-mount security explanation in plain language | `TestRunbook_Section8_RepoCloneAndVerification` checks for "cloning the repository inside the workspace instead of sharing" and "does not have access to your personal files" | PASS |
| Section 8 cross-references 004-security.md for the clone-not-mount rationale | Runbook says "See the sd security documentation for the full rationale" -- does NOT explicitly cite `004-security.md` by filename | SHOULD-FIX -- spec says "Cross-reference: 004-security.md" but runbook uses generic "sd security documentation" |
| Section 8 includes the `sd exec` clone command | Test checks for "sd exec <name> -- git clone" | PASS |
| Section 8 instructs the agent to ask about branch checkout | Test checks for "specific branch" | PASS |
| Section 8 includes verification commands for detected runtimes | Test checks go version, python3 --version, node --version, rustc --version | PASS |
| Section 8 includes troubleshooting guidance for verification failures | Test checks for "sd logs <name>" | PASS |

#### REQ-010-012: Section 9 -- AI Tool Setup

| Acceptance criterion | Test? | Verdict |
|---|---|---|
| Section 9 instructs the agent to ask the user which AI tool to install | `TestRunbook_Section9_AIToolSetup` checks for "Claude Code", "Codex", "Gemini CLI" | PASS |
| Section 9 explicitly states the agent must not assume the user's tool preference | Test checks for "Do not assume which tool" | PASS |
| Section 9 includes instructions to update `.sd.yaml` and re-provision if needed | Test checks for "sd provision <name>" | PASS |
| Section 9 handles the case where the user does not want an AI tool inside the VM | Test checks for "does not want an AI tool inside the workspace" | PASS |

#### REQ-010-013: Section 10 -- Handoff

| Acceptance criterion | Test? | Verdict |
|---|---|---|
| Section 10 tells the user how to connect | `TestRunbook_Section10_Handoff` checks for "sd connect <name>" and "sd c <name>" | PASS |
| Section 10 summarizes what was installed | Runbook includes a summary template with Backend, Modules, Tools verified, Credentials, Repository | PASS |
| Section 10 covers stop/ensure/destroy lifecycle commands | Test checks for "sd stop <name>", "sd ensure", "sd destroy <name>" | PASS |
| Section 10 explains credential refresh behavior | Test checks for "rotate a token" | PASS |
| Section 10 mentions file sync for getting changes back | Test checks for "sd sync from <name>" | PASS |

#### REQ-010-014: Security Education Requirements

| Acceptance criterion | Test? | Verdict |
|---|---|---|
| No jargon term appears without a preceding plain-language definition | `TestRunbook_SecurityEducation_NoUndefinedJargon` checks fine-grained PAT; checks "passed into the workspace" for credential injection; checks "approved websites" for egress | PARTIAL -- only checks 3 of the 6 terms listed in the spec ("egress", "credential injection", "fine-grained token" are verified; "PAT", "attack surface", "lateral movement" are not checked) |
| Security explanations appear at the point of relevance, not in a separate section | Test asserts `## Security` does not appear as a standalone header | PASS |
| Each individual explanation is 1-4 sentences | Not programmatically tested | NOT TESTED -- qualitative inspection shows compliance but no assertion exists |
| The terms listed are defined before use in every section where they appear | `TestRunbook_SecurityEducation_AtRelevantSections` checks sections 1, 7, 8 for specific security content | PASS |

#### REQ-010-017: Clone-Not-Mount as Default Workflow

| Acceptance criterion | Test? | Verdict |
|---|---|---|
| The runbook uses `git clone` inside the VM, not host directory mounts | `TestRunbook_CloneNotMount` checks for "git clone" | PASS |
| The runbook does not include any `--mount` instructions | Test asserts `--mount` does NOT appear | PASS |
| The runbook handles the case where the user has uncommitted work | Test checks for "push" but not the specific guidance text | PARTIAL -- weak assertion |
| The security rationale is explained in plain language | Covered by Section 8 tests | PASS |

#### REQ-010-018: Runbook Stability

| Acceptance criterion | Test? | Verdict |
|---|---|---|
| The runbook does not hardcode version-specific output formats | Not explicitly tested; manual inspection shows no version-specific hardcoding | NOT TESTED |
| The runbook references `sd guide --agent` | `TestRunbook_Stability` checks for "sd guide --agent" | PASS |
| Command examples use `<name>` or `<vm-name>` as placeholders | Test checks for "<name>" and "<repo-url>" | PASS |

### T6 missing tests

1. **MUST-FIX: No `--check --json` combination test.** REQ-010-002 acceptance criterion: "`sd quick-start --check --json` outputs the same JSON assessment as `--check` alone." The test `TestQuickStartCommand_CheckAlwaysJSON` tests `--check` without `--json` but never tests `--check --json` together.

2. **SHOULD-FIX: `packages_declared` is never populated in T6 implementation.** The `quickStartCheckResult` struct has the field and it is initialized as `map[string]int{}`, but the `runQuickStartCheck` function in cc_1 never reads `projCfg.Packages` to populate it. The field will always be `{}`. This is a spec gap that T7 addresses, but the T6 code should at minimum not regress if T7 is merged first. See cc_2 lines 138-154 for the proper implementation.

3. **SHOULD-FIX: REQ-010-014 jargon check is incomplete.** The spec lists 6 jargon terms that must be defined before use: "egress", "PAT", "personal access token", "fine-grained token", "credential injection", "attack surface", "lateral movement". The test only verifies 2-3 of these. The terms "attack surface" and "lateral movement" are not checked (and should ideally not appear in the runbook at all, which is also a valid outcome, but the test should assert that).

4. **SHOULD-FIX: No test for REQ-010-011 cross-reference to 004-security.md.** The spec says Section 8 must cross-reference 004-security.md. The runbook says "sd security documentation" generically. Either update the runbook to name 004-security.md or update the spec to accept a generic reference.

5. **NIT: Rapid property test is declared but empty at line 780.** The function signature and comment for `TestRapid_NeedsSetupTrueIffComponentMissing` appears at line 780 but has no body -- the actual implementation starts at line 1296. The empty declaration at line 780 is dead code (the comment reads as a section header, not a function). This is fine since Go does not allow empty named functions, but it could confuse readers.

### T6 test quality assessment

**Strengths:**
- Excellent per-section test coverage with dedicated test functions for each runbook section
- Good use of `TestRunbook_SecurityEducation_AtRelevantSections` to verify security content appears in correct sections by parsing section boundaries
- The rapid property test properly exercises all boolean combinations with logical constraint enforcement (no VM without config, no running without existing, etc.)
- Test infrastructure (`setupQuickStartTest`, `getRunbookOutput`) is clean and reusable

**Weaknesses:**
- String matching tests (e.g., "approved websites") are brittle -- if wording changes slightly, tests break. This is acceptable for spec-mandated phrasing but fragile for paraphrased content.
- No negative test for the embedded runbook -- there is no test that the template file actually gets embedded correctly at build time (only that `quickStartRunbook` is non-empty)

---

## T7: Quick-start --check enhancement (cc_2)

**Worktree:** `.ntm/worktrees/secure-dev--wave3/cc_2`
**Files changed:** `internal/cmd/quick_start.go`, `internal/cmd/quick_start_test.go`

### Test results

All 20 quick-start tests pass. Both rapid property tests (`TestRapid_NeedsSetupTrueIffComponentMissing`, `TestRapid_IssuesEmptyIffNeedsSetupFalse`) pass.

### Requirement-by-requirement acceptance criteria coverage: REQ-010-015

| Acceptance criterion | Test? | Verdict |
|---|---|---|
| `sd quick-start --check` outputs valid JSON matching the structure | `TestQuickStartCommand_CheckReturnsValidJSON` verifies all 11 fields, credential subfields, array types | PASS |
| All fields are present in the output, even when values are false or empty | `TestProperty_QuickStartCheckAlwaysValid` checks all 11 fields across 5 state combos; rapid test covers random combos | PASS |
| `needs_setup` is true when any required component is missing | `TestQuickStartCommand_CheckNoConfig_NeedsSetup`, `TestRapid_NeedsSetupTrueIffComponentMissing` | PASS |
| `issues` contains actionable descriptions for each problem found | `TestQuickStartCommand_CheckNoConfig_NeedsSetup` checks .sd.yaml issue; `TestQuickStartCommand_CheckStoppedVM` checks stopped VM issue; `TestQuickStartCommand_CheckRepoNotCloned` checks repo issue | PASS |
| `issues` is an empty array when `needs_setup` is false | `TestQuickStartCommand_CheckComplete_NeedsSetupFalse` asserts `assert.Empty(t, issues)` | PASS |
| Credential checks test host environment variables | `TestQuickStartCommand_CheckCredentials` table-driven test with 4 combinations | PASS |
| `repo_cloned` is verified by checking inside the VM | `TestQuickStartCommand_CheckRepoExecCommand` captures the exec command and verifies path `/home/ubuntu/projects/my-app/.git` | PASS |

### packages_declared coverage

| Aspect | Test? | Verdict |
|---|---|---|
| Populated with correct counts from PackageConfig | `TestQuickStartCommand_CheckPackagesDeclared` -- 5 package managers, verifies counts (apt=3, pip=1, npm=2, go=1) | PASS |
| Empty managers are omitted (not zero-valued) | Same test asserts `cargo` is absent when `Cargo: []string{}` | PASS |
| Nil PackageConfig produces empty object | `TestQuickStartCommand_CheckPackagesNil` | PASS |
| packages_declared always present as object | `TestQuickStartCommand_CheckReturnsValidJSON` checks type is `map[string]any` | PASS |

### issues array coverage

| Aspect | Test? | Verdict |
|---|---|---|
| No .sd.yaml found | `TestQuickStartCommand_CheckNoConfig_NeedsSetup` checks for "No .sd.yaml found in this directory -- run sd init to create one" | PASS |
| VM exists but stopped | `TestQuickStartCommand_CheckStoppedVM` checks for "VM my-app is stopped -- run sd ensure to start it" | PASS |
| GITHUB_TOKEN not set | Implicitly tested via `TestRapid_NeedsSetupTrueIffComponentMissing` (missing credential = issue), but no dedicated test checking the exact issue string for this case | PARTIAL |
| ANTHROPIC_API_KEY not set | Same as above | PARTIAL |
| Repo not cloned | `TestQuickStartCommand_CheckRepoNotCloned` checks for "Repository not cloned inside VM -- run sd connect then git clone" | PASS |
| VM does not exist | Not directly tested with a dedicated test (config exists but no VM) | MISSING |

### needs_setup logic

| Aspect | Test? | Verdict |
|---|---|---|
| needs_setup = true iff at least one component is missing | `TestRapid_NeedsSetupTrueIffComponentMissing` -- rapid property test with constrained random booleans | PASS |
| issues empty iff needs_setup is false | `TestRapid_IssuesEmptyIffNeedsSetupFalse` -- separate rapid property test | PASS |
| All-green scenario | `TestQuickStartCommand_CheckComplete_NeedsSetupFalse` | PASS |

### T7 missing tests

1. **SHOULD-FIX: No dedicated test for "VM does not exist" issue string.** There is no test where `.sd.yaml` exists and names a VM, but the VM has never been created. The rapid property test covers the boolean combo but does not verify the exact issue message (e.g., "VM test-vm does not exist -- run sd ensure to create it").

2. **SHOULD-FIX: No dedicated test for credential-missing issue strings.** The tests verify that `credentials.github_token` and `credentials.anthropic_api_key` are correctly boolean, but no test verifies the exact issue text "GITHUB_TOKEN not set on host -- export it before connecting" or "ANTHROPIC_API_KEY not set on host -- export it before connecting".

3. **SHOULD-FIX: No `--check --json` combination test.** Same gap as T6. REQ-010-002 says `--check --json` should produce the same output as `--check` alone.

4. **NIT: Runbook output is a placeholder.** cc_2's `runQuickStart` outputs "Quick-start runbook will be here..." as a placeholder string. This is expected since T7 only covers `--check`, but it means T7 cannot run any runbook content tests. This is fine -- the runbook content lives in T6.

### T7 test quality assessment

**Strengths:**
- Two independent rapid property tests cover the key invariants from different angles (needs_setup logic, issues-empty iff)
- `TestRapid_IssuesEmptyIffNeedsSetupFalse` also randomizes package counts, exercising the `packages_declared` path under fuzz
- Good edge case coverage: stopped VM, repo not cloned, nil Packages, empty Cargo slice
- Table-driven credential test with 4 combinations

**Weaknesses:**
- Issue message assertions use exact string matching, which is correct for spec compliance but makes the tests coupled to specific phrasing
- The `TestQuickStartCommand_CheckRepoNotCloned` issue text differs from T6: cc_2 expects "run sd connect then git clone" while cc_1 expects "run: sd exec my-app -- git clone <url> ~/projects/my-app" -- these implementations diverge on issue text style, which will need reconciliation at merge

---

## Cross-worktree conflicts

### Issue message text divergence (MUST-FIX at merge)

The two worktrees produce different issue message strings for the same conditions:

| Condition | cc_1 (T6) | cc_2 (T7) |
|---|---|---|
| No .sd.yaml | `"No .sd.yaml found -- run sd quick-start to set up"` | `"No .sd.yaml found in this directory -- run sd init to create one"` |
| VM does not exist | `"VM %q does not exist -- run sd ensure to create"` | `"VM %s does not exist -- run sd ensure to create it"` |
| VM stopped | `"VM %q exists but is stopped -- run sd ensure to start"` | `"VM %s is stopped -- run sd ensure to start it"` |
| Repo not cloned | `"Repository not cloned inside VM -- run: sd exec %s -- git clone <url> ~/projects/%s"` | `"Repository not cloned inside VM -- run sd connect then git clone"` |

The cc_1 messages use `%q` (quoted) while cc_2 uses `%s` (unquoted). The cc_1 repo-not-cloned message is more actionable (includes the actual command). The cc_2 .sd.yaml message suggests `sd init` while cc_1 suggests `sd quick-start`. These need to be reconciled to a single set of issue messages.

### packages_declared implementation gap

cc_1 does NOT populate `packages_declared` from `projCfg.Packages`. cc_2 does (lines 138-154). The cc_2 implementation is correct per the spec. At merge, the cc_2 implementation should be used.

---

## Summary of findings

### Must-fix (before merge)

| ID | Task | Finding |
|---|---|---|
| MF-1 | T6+T7 | Issue message text diverges between worktrees -- must reconcile before merging |
| MF-2 | T6 | `packages_declared` is never populated from config -- use cc_2's implementation |

### Should-fix

| ID | Task | Finding |
|---|---|---|
| SF-1 | T6+T7 | No `--check --json` combination test (REQ-010-002 acceptance criterion) |
| SF-2 | T6 | REQ-010-014 jargon check only covers 3 of 6 spec-listed terms |
| SF-3 | T6 | No test for 004-security.md cross-reference in Section 8 |
| SF-4 | T7 | No dedicated test for "VM does not exist" issue message |
| SF-5 | T7 | No dedicated test for credential-missing issue messages |

### Nits

| ID | Task | Finding |
|---|---|---|
| N-1 | T6 | Dead comment block at line 780 (section header for rapid test that lives at line 1296) |
| N-2 | T7 | Runbook is a placeholder string (expected, since runbook is T6's scope) |

### Test counts

| Worktree | Unit tests | Table-driven | Property-based (rapid) | Total assertions (approx) |
|---|---|---|---|---|
| cc_1 (T6) | 25 | 1 (credentials, 4 cases) | 1 | ~150 |
| cc_2 (T7) | 16 | 1 (credentials, 4 cases) | 2 | ~100 |
