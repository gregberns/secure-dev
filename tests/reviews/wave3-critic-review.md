# Wave 3 Critic Review

Reviewer: critic
Date: 2026-04-09

---

## T6: Quick-start runbook output

**Worktree:** cc_1
**Files:** `internal/cmd/quick_start.go` (diff), `internal/cmd/quick_start_test.go` (diff), `internal/cmd/templates/quick_start_runbook.md` (new)
**Spec:** REQ-010-004 through 010-013, REQ-010-014, REQ-010-017, REQ-010-018

### Verdict: NEEDS-CHANGES

### Issues

#### 1. PLAIN-LANGUAGE: Bare jargon term "allow_egress" in Section 5
- **File:** `internal/cmd/templates/quick_start_runbook.md:132`
- **Severity:** must-fix
- **Description:** Line 132 says: "add them to `allow_egress`". The term "egress" appears here without any preceding definition. REQ-010-014 explicitly lists "egress" as a term that MUST NOT appear without a preceding plain-language definition ("which websites or services the workspace can connect to"). The security explanation blockquote on lines 144-149 does define the concept in plain language, but it comes *after* the `allow_egress` reference on line 132. An agent reading top-to-bottom encounters the jargon before the explanation.
- **Spec says:** REQ-010-014 requirement 2: "egress -- explain as 'which websites or services the workspace can connect to'". The definition must *precede* the term.
- **Recommendation:** Move the plain-language explanation before line 132, or change line 132 to avoid the raw term. For example: "add them to the approved-sites list (`allow_egress` in `.sd.yaml`)". Alternatively, swap the order so the security explanation blockquote appears before the numbered instruction list.

#### 2. SPEC-COMPLIANCE: Missing explicit cross-reference to 004-security.md in Section 8
- **File:** `internal/cmd/templates/quick_start_runbook.md:268-269`
- **Severity:** should-fix
- **Description:** REQ-010-011 acceptance criterion 2 states: "Section 8 cross-references 004-security.md for the clone-not-mount rationale." The runbook says "(See the sd security documentation for the full rationale.)" This is vague -- it does not name `004-security.md` or provide a way for the agent to find it. However, REQ-010-018 says the runbook should not hardcode version-specific paths. There is a tension between these two requirements.
- **Recommendation:** Reference the spec by name in a way that is stable: "(See spec 004-security for the full rationale.)" or "Run `sd guide --agent` for the full security rationale." This satisfies REQ-010-011's cross-reference requirement without hardcoding a file path.

#### 3. SECURITY: Injection risk assessment -- no vulnerability found (informational)
- **Severity:** note
- **Description:** The runbook is statically embedded via `//go:embed` from a file under source control. No user input, environment variables, or config values are interpolated into the template at runtime. The `quick_start.go` diff confirms the runbook string is used directly without template rendering (`quickStartRunbook` is output as-is). This means there is no injection vector: a malicious `.sd.yaml`, environment variable, or user input cannot alter the runbook content. The spec design section mentions Go templates (`text/template`) but the implementation chose to embed a static Markdown file instead, which is the safer approach.
- **Assessment:** No injection risk. The runbook is immutable at runtime. Good.

#### 4. CORRECTNESS: Runbook instructions reference `sd provision <name>` but spec says `sd provision <name> --modules <tool-module>`
- **File:** `internal/cmd/templates/quick_start_runbook.md:343`
- **Severity:** note
- **Description:** In Section 9, the re-provision command is `sd provision <name> --modules <tool-module>`, which matches the spec (REQ-010-012). This is correct.

#### 5. PLAIN-LANGUAGE: Token explanation in Section 7 is excellent but could add "door/key" metaphor attribution
- **File:** `internal/cmd/templates/quick_start_runbook.md:200-207`
- **Severity:** note
- **Description:** The explanation "Think of it like a key that only opens one door instead of every door in the building" is excellent plain language. The spec (REQ-010-010) says: "We'll create a token that only has access to this specific project." The runbook goes beyond the spec's suggested phrasing with a better metaphor. This is a positive deviation -- the spec's suggested phrasings are not mandatory exact text, and the runbook's version is clearer.

#### 6. EDGE-CASE: Runbook Section 2 remediation table does not cover `tmux` check failure
- **File:** `internal/cmd/templates/quick_start_runbook.md:47-53`
- **Severity:** should-fix
- **Description:** The remediation table covers `ssh`, `lima`, `docker`, and `sd_home` checks. But `sd doctor` also checks for `tmux` (used for session management). If tmux is missing, the agent has no remediation guidance.
- **Recommendation:** Add a `tmux` row: "Session manager is not installed. macOS: `brew install tmux`. Linux: `sudo apt-get install tmux`."

#### 7. TEST-QUALITY: Jargon audit test (TestRunbook_SecurityEducation_NoUndefinedJargon) is incomplete
- **File:** `internal/cmd/quick_start_test.go` (cc_1 diff)
- **Severity:** must-fix
- **Description:** REQ-010-014 lists seven jargon terms that must not appear without preceding definitions: "egress", "PAT", "fine-grained token", "credential injection", "attack surface", "lateral movement", "blast radius". The test only checks two terms ("fine-grained personal access token" defined, and its mention of "specific repositories"). It does NOT check that:
  - "egress" is preceded by its definition (and in fact it is NOT -- see issue #1)
  - "credential injection" is preceded by its definition
  - "attack surface", "blast radius", "lateral movement" are either absent or defined
  The test also checks for "approved websites" and "controlled by a list" which is good, but those are verification of the *definition*, not verification that the *term* precedes the *definition* in document order. A term-before-definition ordering check is what REQ-010-014 requires.
- **Recommendation:** For each term in the REQ-010-014 list, either (a) assert the term does not appear in the runbook, or (b) assert that the plain-language definition appears *before* the term in byte order. The current test would pass even if "egress" appeared 100 times without definition (because it only checks the two "fine-grained" cases).

#### 8. TEST-QUALITY: TestRunbook_CloneNotMount assertion for uncommitted work is too weak
- **File:** `internal/cmd/quick_start_test.go` (cc_1 diff), around the CloneNotMount test
- **Severity:** should-fix
- **Description:** The test asserts `Contains(output, "push")` to verify the runbook handles uncommitted work. The word "push" is extremely common and could match anything (e.g., "push your workspace" or "push the button"). The runbook actually says "push them to a branch first" which is the correct behavior. A more specific assertion like `Contains(output, "push them to a branch")` or `Contains(output, "push those changes to a branch")` would be more meaningful.

---

## T7: Quick-start --check enhancement

**Worktree:** cc_2
**Files:** `internal/cmd/quick_start.go` (diff), `internal/cmd/quick_start_test.go` (diff)
**Spec:** REQ-010-015

### Verdict: NEEDS-CHANGES

### Issues

#### 1. BUG: Double-counted issues when FindProjectConfig returns error
- **File:** `internal/cmd/quick_start.go` (cc_2), lines 123-125 and 187-188
- **Severity:** must-fix
- **Description:** When `findProjectConfigFunc` returns a non-nil error AND a nil config (e.g., the `.sd.yaml` file exists but is malformed YAML), the code appends `"Error reading .sd.yaml: <err>"` to issues (line 124), then because `projCfg == nil`, `SDYamlExists` stays false, causing line 188 to also append `"No .sd.yaml found in this directory -- run sd init to create one"`. The user sees two contradictory issues: one says there was an error *reading* the file (implying it exists), and the other says no file was found (implying it does not exist).
- **Impact:** An agent following the `--check` output would get confused: does the file exist or not? Should it run `sd init` (which might overwrite the malformed file) or fix the existing file?
- **Recommendation:** When `findErr != nil`, set a flag or use an `else if` to skip the "No .sd.yaml found" issue. Something like:
  ```go
  if findErr != nil {
      result.Issues = append(result.Issues, fmt.Sprintf("Error reading .sd.yaml: %v", findErr))
  } else if projCfg == nil {
      result.Issues = append(result.Issues, "No .sd.yaml found in this directory -- run sd init to create one")
  }
  ```

#### 2. SPEC-COMPLIANCE: Issue message wording deviates from spec examples
- **File:** `internal/cmd/quick_start.go` (cc_2), lines 187-205
- **Severity:** note
- **Description:** The spec (REQ-010-015) provides example issue strings. cc_2 changed several of them:
  - Spec: `"No .sd.yaml found -- run sd quick-start to set up"` -> cc_2: `"No .sd.yaml found in this directory -- run sd init to create one"`. The `sd init` suggestion is arguably more correct than `sd quick-start` since quick-start outputs a runbook, not a config file. This is a positive change.
  - Spec: `"VM exists but is stopped -- run sd ensure to start"` -> cc_2: `"VM my-app is stopped -- run sd ensure to start it"`. Dropped "exists but" which is slightly less informative but more natural. Acceptable.
  - Spec: `"Repository not cloned inside VM -- run: sd exec <name> -- git clone <url> ~/projects/<name>"` -> cc_2: `"Repository not cloned inside VM -- run sd connect then git clone"`. This lost the specific `sd exec` command with its full syntax.
- **Assessment:** The spec says "Examples:" before the issue list, so these are illustrative, not prescriptive exact strings. The wording changes are generally improvements except the repo-clone message, which lost actionable detail (the exact command to run). Consider keeping the `sd exec` syntax or at minimum including the target path.

#### 3. BUG: `packages_declared` omits package managers with zero entries when Packages struct is non-nil
- **File:** `internal/cmd/quick_start.go` (cc_2), lines 138-153
- **Severity:** should-fix
- **Description:** The spec (REQ-010-015) shows this example output: `"packages_declared": {"apt": 3, "pip": 0, "npm": 1}`. Note `"pip": 0` is included. The implementation only adds entries when `len > 0`. If `Packages` is non-nil but `Pip` is an empty slice (not nil), the output omits `"pip"` entirely. The spec's example explicitly shows zero-count entries.
- **Impact:** An agent checking `packages_declared` might interpret the absence of a key as "not configured" vs. "configured but empty". The spec example suggests zero-count entries should be present.
- **Recommendation:** When `Packages` is non-nil, always populate all five keys (`apt`, `pip`, `npm`, `go`, `cargo`), even when the count is zero. This matches the spec example. Alternatively, document the behavior as "only non-zero counts appear" and update the spec example.

#### 4. CORRECTNESS: Repo-not-cloned issue message lost the exec command
- **File:** `internal/cmd/quick_start.go` (cc_2), line 205
- **Severity:** should-fix
- **Description:** The original message was: `"Repository not cloned inside VM -- run: sd exec <name> -- git clone <url> ~/projects/<name>"`. cc_2 changed it to: `"Repository not cloned inside VM -- run sd connect then git clone"`. The original gave the agent an exact command it could execute. The new version tells the agent to connect first and then clone, but without the target path or the exec syntax. The `--check` output is specifically designed for agents (REQ-010-015), so providing an exact executable command is more valuable than a vague instruction.
- **Recommendation:** Either restore the original message with the `sd exec` command, or provide the full connect+clone sequence: `"Repository not cloned inside VM -- run: sd exec <vm-name> -- git clone <url> ~/projects/<vm-name>"`. If the VM name is known (it is -- it's `result.VMName`), substituting it would be even more helpful.

#### 5. TEST-QUALITY: No test for findProjectConfigFunc error path
- **File:** `internal/cmd/quick_start_test.go` (cc_2)
- **Severity:** must-fix
- **Description:** There is no test covering the case where `findProjectConfigFunc` returns a non-nil error (e.g., `.sd.yaml` exists but has invalid YAML). This is the exact scenario that triggers issue #1 (double-counted issues). Without a test, the bug would not have been caught.
- **Recommendation:** Add a test case where `findProjectConfigFunc` returns `(path, nil, someError)` and verify that the issues array contains exactly one error-related issue (not both "Error reading" and "No .sd.yaml found").

#### 6. TEST-QUALITY: Property test correctly strengthened with issues-empty-iff check
- **Severity:** note (positive)
- **Description:** The addition to `TestRapid_NeedsSetupTrueIffComponentMissing` that checks `len(issues) == 0 iff !needsSetup` is a good strengthening. The new dedicated `TestRapid_IssuesEmptyIffNeedsSetupFalse` test further exercises this with random package counts, which is thorough. The package count randomization also exercises the new `packages_declared` code path, which is a nice cross-cutting validation.

#### 7. CORRECTNESS: Issue messages removed quotes around VM name, could cause ambiguity
- **File:** `internal/cmd/quick_start.go` (cc_2), lines 192, 196
- **Severity:** note
- **Description:** The original code used `%q` (Go-quoted string) for VM names in issue messages, e.g., `VM "my-app" does not exist`. cc_2 changed to `%s`, producing `VM my-app does not exist`. For typical VM names (lowercase alphanumeric + hyphens), this is fine. But if a VM name were ever empty string (e.g., `.sd.yaml` has `name: ""`), the message would read "VM  does not exist" (with a double space), which is confusing. The `%q` format would produce `VM "" does not exist`, which is clearer for the empty case.
- **Assessment:** VM names are validated at config load time to be non-empty and match `[a-z][a-z0-9-]{0,62}`, so the empty case should not occur in practice. This is a minor robustness concern; leaving it as `%s` is acceptable.

---

## Summary

| Task | Verdict | Must-fix | Should-fix | Notes |
|------|---------|----------|------------|-------|
| T6 (runbook) | NEEDS-CHANGES | 2 | 2 | 4 |
| T7 (--check) | NEEDS-CHANGES | 2 | 2 | 3 |

### T6 Must-fix items
1. **"allow_egress" jargon before definition** (issue #1) -- move the plain-language explanation before the term, or rephrase
2. **Jargon audit test is incomplete** (issue #7) -- test must check all seven REQ-010-014 terms, not just "fine-grained personal access token"

### T7 Must-fix items
1. **Double-counted issues on config read error** (issue #1) -- use else-if to prevent contradictory messages
2. **No test for config error path** (issue #5) -- add test for findProjectConfigFunc returning error
