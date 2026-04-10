# Wave 3 Architect Review

Reviewer: architect
Date: 2026-04-09
Spec: specs/010-agent-bootstrap.md

---

## T6: Quick-start runbook output (cc_1)

**Verdict: PASS**

### Spec Compliance

T6 implements REQ-010-004 through REQ-010-013 (runbook sections 1-10), REQ-010-014 (security education), REQ-010-017 (clone-not-mount), and REQ-010-018 (runbook stability).

The runbook template at `internal/cmd/templates/quick_start_runbook.md` is complete and well-structured:
- All 10 sections present in correct order
- Each section contains labeled block types (Instructions, Commands, Questions, Explanations) per REQ-010-003
- Addressed to the agent as "you", references the human as "the user" throughout
- Security education is woven into relevant sections (1, 5, 7, 8) rather than segregated
- Jargon terms ("fine-grained personal access token", egress concepts) are defined in plain language before use per REQ-010-014
- Clone-not-mount is the default workflow with no `--mount` references per REQ-010-017
- Uses `<name>` and `<repo-url>` placeholders per REQ-010-018
- References `sd guide --agent` for extended documentation

### API Design

The implementation changes to `quick_start.go` are minimal and clean:
- Adds `//go:embed templates/quick_start_runbook.md` with a `quickStartRunbook` variable
- Replaces the placeholder string in `runQuickStart()` with the embedded runbook content
- Default output path uses `f.SuccessData(nil, func() string { return quickStartRunbook })` -- clean integration with the existing `ui.Formatter` pattern
- JSON path wraps the full runbook content in `{"ok": true, "data": {"runbook": "..."}}` per REQ-010-002

### Merge Composability

T6 modifies two areas of `quick_start.go`:
1. Adds the `//go:embed` directive and `quickStartRunbook` variable (new code, lines 20-24)
2. Replaces the `placeholder` string in `runQuickStart()` with direct use of `quickStartRunbook`

T7 (cc_2) does NOT touch the runbook output path at all -- it only modifies `runQuickStartCheck()` to add `packages_declared` population. These changes are in completely different regions of the file. **No merge conflict expected.**

### Issues

| # | Severity | Description |
|---|----------|-------------|
| A6-1 | note | The spec's Design section (line ~492) mentions using Go templates with `RunbookData` struct and template variables (e.g., `AvailableBackends`). The implementation uses a static embedded file instead. This is actually a better design choice -- the runbook is addressed to an agent that will dynamically adapt based on context, so Go template rendering is unnecessary complexity. The spec's Design section is informational, not a requirement, so this is not a violation. |
| A6-2 | note | The `_ "embed"` import is needed only for the embed directive side effect. Standard Go pattern, no issue. |
| A6-3 | should-fix | The runbook Section 4 (Backend Selection) says Lima is "macOS-only" in Section 2's remediation table but then presents Lima as an option for all users in Section 4. This is technically accurate (Lima does run on Linux too now) but the remediation table in Section 2 says "Linux: Lima is macOS-only; use Docker instead." This inconsistency in the runbook could confuse agents. Consider clarifying that Lima supports Linux as well, or making the remediation table consistent with Section 4. |
| A6-4 | note | Section 8 references "See the sd security documentation for the full rationale." The spec says to cross-reference `004-security.md`. The runbook does not link to the spec directly, but it does point to "sd security documentation" which is a reasonable agent-facing reference. Acceptable. |

### Test Coverage

Tests are thorough and cover all assigned requirements:
- `TestRunbook_ContainsAll10Sections` -- verifies all 10 sections exist in order
- `TestRunbook_SectionsHaveLabeledBlocks` -- checks labeled block types
- `TestRunbook_AddressedToAgent` -- verifies agent addressing
- Individual section tests for REQ-010-004 through REQ-010-013
- `TestRunbook_SecurityEducation_NoUndefinedJargon` -- jargon check
- `TestRunbook_SecurityEducation_AtRelevantSections` -- security education placement
- `TestRunbook_CloneNotMount` -- REQ-010-017
- `TestRunbook_Stability` -- REQ-010-018
- `TestRunbook_JSONOutput_ContainsRunbook` -- full runbook in JSON envelope
- `TestRunbook_Embedded_NotEmpty` -- sanity check on embedded content
- Property-based test `TestRapid_NeedsSetupTrueIffComponentMissing` for --check invariant

---

## T7: Quick-start --check enhancement (cc_2)

**Verdict: NEEDS-CHANGES**

### Spec Compliance

T7 implements REQ-010-015 enhancements: populating `packages_declared` from `ProjectConfig.Packages` and refining issue messages.

The `packages_declared` population logic (lines 138-154) correctly:
- Reads from `projCfg.Packages` when non-nil
- Populates counts for apt, pip, npm, go, cargo
- Omits package managers with zero packages (empty slices are not included)

### API Design

The `packages_declared` field semantics are well-designed:
- Empty map `{}` when no packages declared (not null)
- Only non-zero counts appear as keys
- Matches the spec's example: `{"apt": 3, "pip": 0, "npm": 1}`

**Wait** -- there is actually a discrepancy. The spec example shows `"pip": 0` but T7's implementation omits zero-count entries. The implementation says `if len(projCfg.Packages.Pip) > 0` which means `"pip": 0` would NOT appear. This is a better API design (why send zero-count entries?) but it diverges from the spec example.

### Issue Message Divergence

T7 changes issue message strings from the base. Comparing to what the spec prescribes (REQ-010-015):

| Spec example | Base (current HEAD) | T7 (cc_2) |
|---|---|---|
| `"No .sd.yaml found -- run sd quick-start to set up"` | `"No .sd.yaml found -- run sd quick-start to set up"` | `"No .sd.yaml found in this directory -- run sd init to create one"` |
| `"VM exists but is stopped -- run sd ensure to start"` | `"VM %q exists but is stopped -- run sd ensure to start"` | `"VM %s is stopped -- run sd ensure to start it"` |
| (not shown) | `"VM %q does not exist -- run sd ensure to create"` | `"VM %s does not exist -- run sd ensure to create it"` |
| `"Repository not cloned inside VM -- connect and clone with sd exec <name> -- git clone <url>"` | `"Repository not cloned inside VM -- run: sd exec %s -- git clone <url> ~/projects/%s"` | `"Repository not cloned inside VM -- run sd connect then git clone"` |

### Merge Composability

T7 modifies only `runQuickStartCheck()` to add `packages_declared` population and tweaks issue messages. T6 modifies the runbook output path (`runQuickStart()`) and adds the embedded template. **The changes are in non-overlapping code regions. No structural merge conflict expected.**

However, there IS a test merge conflict: both T6 and T7 have tests that assert exact issue message strings, and these strings differ between T6 and T7. Specifically:

- T6's `TestQuickStartCommand_CheckNoConfig_NeedsSetup` checks for `"No .sd.yaml found -- run sd quick-start to set up"` (matches base)
- T7's same test checks for `"No .sd.yaml found in this directory -- run sd init to create one"` (changed)
- T6's `TestQuickStartCommand_CheckStoppedVM` checks for `"VM \"my-app\" exists but is stopped -- run sd ensure to start"` (matches base with %q formatting)
- T7's same test checks for `"VM my-app is stopped -- run sd ensure to start it"` (changed, no quotes)
- T6's `TestQuickStartCommand_CheckRepoNotCloned` checks for `"Repository not cloned inside VM -- run: sd exec my-app -- git clone <url> ~/projects/my-app"` (matches base)
- T7's same test checks for `"Repository not cloned inside VM -- run sd connect then git clone"` (changed)

**This is the primary merge concern.** When merging, the orchestrator must decide which issue messages to keep. The production code will only have one set of messages, but both test files assert different strings.

### Issues

| # | Severity | Description |
|---|----------|-------------|
| A7-1 | must-fix | **Issue message for missing .sd.yaml diverges from spec.** The spec (REQ-010-015) gives the example `"No .sd.yaml found -- run sd quick-start to set up"`. T7 changes this to `"No .sd.yaml found in this directory -- run sd init to create one"`. The `sd init` suggestion may be more accurate (quick-start outputs a runbook, it does not create .sd.yaml), but this is a spec deviation. Either update the spec or match the spec's example. Recommend updating the spec since `sd init` is indeed the correct command. |
| A7-2 | must-fix | **Issue message for repo not cloned is less actionable.** T7 changes from `"Repository not cloned inside VM -- run: sd exec %s -- git clone <url> ~/projects/%s"` to `"Repository not cloned inside VM -- run sd connect then git clone"`. The base message is more actionable (provides the exact command). The spec says issues must contain "actionable descriptions." The T7 message loses the specific command syntax. Revert to the more specific message or provide equivalent specificity. |
| A7-3 | should-fix | **Inconsistent %q vs %s formatting.** The base uses `%q` for VM names in issue messages (producing `"my-app"` with quotes), while T7 uses `%s` (producing `my-app` without quotes). The `%q` formatting is better for clarity when VM names might contain spaces or special characters, and matches Go conventions for user-facing strings that embed identifiers. Recommend keeping `%q`. |
| A7-4 | should-fix | **packages_declared zero-count behavior differs from spec example.** The spec example shows `"pip": 0` as a value, but T7 omits zero-count package managers. This is arguably better API design (no noise from unused managers) but diverges from the spec example. Document the decision or update the spec to clarify that only non-zero counts are included. |
| A7-5 | note | **Cargo empty-slice check is correct.** The test `TestQuickStartCommand_CheckPackagesDeclared` correctly verifies that `Cargo: []string{}` (empty slice) does NOT produce a `"cargo": 0` entry. This is consistent with the `> 0` check. |
| A7-6 | note | **Test merge conflict anticipated.** Both T6 and T7 have overlapping test functions with different issue message assertions. When merging, the test file from whichever task lands second will need its exact-match assertions updated to match the final production code messages. The orchestrator should merge T6 first (it preserves base messages), then merge T7's `packages_declared` logic into production code, and reconcile issue messages and test assertions in one pass. |

### Merge Recommendation

1. Merge T6 first -- it is a clean addition (embedded runbook + template) with no behavioral changes to --check
2. Merge T7's `packages_declared` population into `runQuickStartCheck()` -- this is the core value of T7
3. For issue messages: keep the base messages (T6 preserves them) except for the .sd.yaml message where T7's `sd init` suggestion is arguably more correct. Resolve A7-1 by updating the spec example.
4. For the repo-not-cloned message: keep T6/base's more actionable version (A7-2)
5. Reconcile test assertions to match whichever messages are chosen

---

## Summary

| Task | Verdict | Must-Fix | Should-Fix | Notes |
|------|---------|----------|------------|-------|
| T6: Runbook output | PASS | 0 | 1 | Clean implementation, excellent runbook content, good test coverage |
| T7: --check enhancement | NEEDS-CHANGES | 2 | 2 | Core packages_declared logic is good; issue message changes need reconciliation with spec and base |

### Key Merge Notes

- **No structural merge conflict** between T6 and T7 in production code (different functions modified)
- **Test merge conflict** in exact-string assertions for issue messages -- must be reconciled during merge
- **Recommended merge order**: T6 first, then T7's packages_declared logic, then reconcile messages
