# Wave 2 Architect Review

**Reviewer:** architect
**Date:** 2026-04-09
**Focus:** API design, codebase consistency, Wave 3 extensibility, spec compliance, cross-worktree integration

---

## T2: Declarative Provisioning Pipeline (secure-dev-5sj)

**Verdict: NEEDS-CHANGES**

### Spec compliance

The implementation covers REQ-009-004 (pipeline order), REQ-009-005 (prerequisite validation), REQ-009-006 (package installation), REQ-009-007 (setup commands), REQ-009-008 (repo clone), REQ-009-010 (idempotency), and REQ-009-013 (failure handling). All requirement IDs are referenced in code comments.

### API design

**packages.go:**
- The `packageManager` struct and `managers` slice are well-structured and follow the spec table exactly.
- `InstallPackages` uses the existing `ExecFunc` type from provisioner.go, maintaining consistency.
- `packagesForManager` dispatches cleanly via switch statement.

**setup.go:**
- Clean, minimal implementation. Correct use of `set -eux -o pipefail` preamble.

**clone.go:**
- The shell-script-based check (`EXISTS_GIT:`, `EXISTS_NOT_GIT`, `NOT_EXISTS`) is a reasonable approach for running complex logic inside the VM.
- The `cloneError` helper that distinguishes auth from network errors is good.

**create.go (runDeclarativePipeline):**
- The adapter from `backend.Exec` (returns `ExecResult`) to `ExecFunc` (returns 4-tuple) is correct.
- Returns `ui.CLIError` with appropriate codes, enabling `--json` serialization.

### Issues

| # | Severity | File | Issue |
|---|----------|------|-------|
| A2-1 | **must-fix** | `create.go` | REQ-009-013 requires JSON errors to include `step` and `details` fields. The current `ui.CLIError` returns only `code` and `message`. The `Details` map field on `CLIError` exists but is not populated. Each pipeline step error should set `Details: map[string]any{"step": "packages.apt", "details": "<output tail>"}` so that `--json` output conforms to the spec's error schema. |
| A2-2 | **must-fix** | `clone.go:73` | `mkdir -p ~/projects` error is silently discarded: `_, _, _, _ = execFn(...)`. If the mkdir fails, the subsequent clone will also fail but with a confusing error. Either check the error or wrap it into the clone error path. |
| A2-3 | **should-fix** | `clone.go:26-36` | The shell script uses `%s` format for the `target` path variable (which contains `~/projects/<name>`). If a VM name contained shell-special characters (spaces, backticks, etc.) this would be a shell injection vector. VM names are validated elsewhere to be `[a-z][a-z0-9-]{0,62}` so this is safe today, but the script should either quote `%s` with double quotes or add a comment noting the name-validation dependency. Defensive quoting is low-cost and prevents future regressions. |
| A2-4 | **should-fix** | `packages.go:33` | The `apt-get update` command has `-y` appended: `"sudo apt-get update -y"`. The `-y` flag is not a standard option for `apt-get update` (it's for `install`). It is harmless (silently ignored) but inconsistent with the spec table in REQ-009-006 which specifies only `sudo apt-get update`. Remove the `-y` to match the spec. |
| A2-5 | **note** | `create.go:268-270` | On declarative pipeline failure, the VM is destroyed. This is aggressive -- if module provisioning succeeded and only package installation failed, destroying the VM discards all module work. Consider whether a warning + leaving the VM in place is better. However, this follows the existing pattern for module provisioning failures, so it is consistent. Not blocking. |

### Wave 3 integration

- T6 (runbook) will need to call `runDeclarativePipeline` or its components. The current structure where `InstallPackages`, `RunSetupCommands`, and `CloneRepo` are separate public functions with the `ExecFunc` interface is good for composability.
- T7 (`--check` enhancement) needs to inspect declared packages. The `PackageConfig` struct on `ProjectConfig` provides clean access.
- The `ExecFunc` adapter in `runDeclarativePipeline` could be extracted to a shared utility if T6 needs the same pattern.

---

## T3: sd init Updates (secure-dev-3ak)

**Verdict: PASS** (with should-fix items)

### Spec compliance

Covers REQ-009-012 fully. The implementation:
- Auto-detects git remote via `detectGitRemote` (overridable for testing)
- Includes commented-out `packages` section with all five managers
- Includes commented-out `setup` section with idempotency note
- Populates `repo` field when origin remote is detected
- Leaves `repo` absent when no remote is detected

### API design

- `detectGitRemote` is a package-level `var` function, making it overridable for tests. This follows the existing pattern used by `getWorkingDir`, `getBackendFunc`, etc. throughout the cmd package. Good consistency.
- The `initResult` struct gains a `Repo` field, correctly tagged `omitempty`.
- Template assembly uses string concatenation (header + YAML + extra sections). This is simple and readable.

### Issues

| # | Severity | File | Issue |
|---|----------|------|-------|
| A3-1 | **should-fix** | `init.go:131-135` | The `ProjectConfig` struct does NOT include `Repo` in the marshaled object -- instead `repo:` is appended as raw text after the YAML. This means the `repo` line appears after a blank line and the commented sections, not adjacent to the other config fields. The visual order in the generated file should match the spec's example in REQ-009-012 (name, modules, repo, then commented packages/setup). Fix: either set `projCfg.Repo = repoURL` before marshaling (so YAML includes it), or reorder the `extra` string so `repo:` comes first before the blank line. |
| A3-2 | **should-fix** | `init.go:162` | The `extra` string starts with `"\n"` when no repo is detected, and `"repo: ...\n\n"` when there is one. This produces inconsistent whitespace between the YAML body and the commented sections. Minor formatting issue. |
| A3-3 | **note** | `init.go:131-135` | `Mounts` is hardcoded in the generated config. Since spec 009 promotes clone-not-mount as the default workflow (REQ-009-008), there is a tension: `sd init` generates a mount while `sd quick-start` and the runbook push clone. This is a pre-existing design issue, not introduced by this change. Not blocking. |

### Wave 3 integration

- T6 runbook will reference `sd init` output. The template format is stable and the commented sections provide good scaffolding.
- No conflicts expected.

---

## T5: sd quick-start Scaffold (secure-dev-oca)

**Verdict: NEEDS-CHANGES**

### Spec compliance

Covers REQ-010-001 (command registration), REQ-010-002 (flags), and REQ-010-015 (--check assessment). The runbook content is a placeholder, which is appropriate for a scaffold task -- the full runbook is T6 work.

The command is registered in the "start" group (GroupID `"start"`), matching the existing `guide` command, consistent with spec REQ-010-001.

### API design

- `quickStartCheckResult` struct has good field coverage matching the spec's JSON schema.
- `quickStartCredentials` is a named struct rather than `map[string]bool`. This is a deviation from the spec's `"credentials": {"github_token": true}` which uses a plain object. However, the named struct produces identical JSON output and is more type-safe. Good design choice.
- Testability via `getenvFunc`, `findProjectConfigFunc`, `getWorkingDir` overrides follows established patterns.

### Issues

| # | Severity | File | Issue |
|---|----------|------|-------|
| A5-1 | **must-fix** | `quick_start.go:22-32` | REQ-010-015 requires a `packages_declared` field: `"packages_declared": {"apt": 3, "pip": 0, "npm": 1}`. This field is completely absent from `quickStartCheckResult`. Add `PackagesDeclared map[string]int \`json:"packages_declared"\`` and populate it from the config's `Packages` field. |
| A5-2 | **must-fix** | `quick_start.go:156` | The `test -d ~/projects/<name>` command is passed as `[]string{"test", "-d", "~/projects/..."}`. The tilde (`~`) is a shell expansion feature -- it is NOT expanded by the `test` binary when invoked directly (not through a shell). In a real Lima/Docker backend, `Backend.Exec` ultimately runs `limactl shell` which does invoke a shell, so this might work in practice. But the memory backend test passes because it uses a mock handler that returns exit code 0 regardless. To be correct and portable, wrap the command in bash: `[]string{"bash", "-c", fmt.Sprintf("test -d ~/projects/%s", result.VMName)}` or use `$HOME` with the full path. |
| A5-3 | **should-fix** | `quick_start.go:86` | The placeholder text is a bare string, not a rendered runbook template. The spec design section calls for `//go:embed templates/quick_start_runbook.md`. This is fine for a scaffold, but the code structure should anticipate the embed -- consider adding a comment like `// TODO(T6): Replace with embedded runbook template` to make the Wave 3 integration point explicit. |
| A5-4 | **should-fix** | `quick_start.go:128-130` | The `Backend` field defaults to `"lima"` when `projCfg.Backend` is empty. This hardcoded default should reference a constant or use the backend registry's default logic to avoid drift if the default backend ever changes. |
| A5-5 | **note** | `quick_start.go:138-141` | The VM status check creates a new backend instance via `getBackendFunc` for each logical check (once for VM status, once for repo clone). This is fine for the memory backend but could be optimized to a single `getBackendFunc` call. Low priority. |

### Wave 3 integration

- T6 (runbook) will replace the placeholder string at line 86 with real embedded Markdown. The current scaffold provides a clean insertion point.
- T7 (`--check` enhancement) can extend `quickStartCheckResult` with additional fields. The struct is in the right place.
- The `--check` assessment logic is a good foundation. T7 may add package installation verification, which would add new checks to `runQuickStartCheck`.

---

## Cross-Worktree Integration Assessment

### File conflicts

| File | T2 | T3 | T5 | Conflict? |
|------|----|----|----|----|
| `internal/config/project.go` | Modified (adds fields + validation) | -- | -- | No |
| `internal/config/project_test.go` | Modified (adds validation tests) | -- | -- | No |
| `internal/cmd/create.go` | Modified (adds pipeline call) | -- | -- | No |
| `internal/cmd/ensure.go` | Modified (passes projCfg) | -- | -- | No |
| `internal/cmd/init.go` | -- | Modified (adds repo detection, template sections) | -- | No |
| `internal/cmd/init_test.go` | -- | Modified (adds template tests) | -- | No |
| `internal/cmd/quick_start.go` | -- | -- | New | No |
| `internal/cmd/quick_start_test.go` | -- | -- | New | No |
| `internal/cmd/root.go` | -- | -- | Modified (adds quick-start to noConfigCmds) | No |
| `internal/provision/packages.go` | New | -- | -- | No |
| `internal/provision/setup.go` | New | -- | -- | No |
| `internal/provision/clone.go` | New | -- | -- | No |
| `AGENTS.md` | Modified | Modified | Modified | **Trivial** -- same kerf section added in all three. Easy to resolve. |

### Dependency chain

T5's `--check` reads `PackageConfig` from `projCfg` (added by T2). T3's init template generates files that T5's `--check` reads. Merge order should be: **T2 first, then T3, then T5**. T5 depends on T2's `PackageConfig` type to implement the missing `packages_declared` field (A5-1).

### No semantic conflicts detected.

---

## Summary

| Task | Verdict | Must-fix count | Should-fix count |
|------|---------|---------------|-----------------|
| T2 | NEEDS-CHANGES | 2 | 2 |
| T3 | PASS | 0 | 2 |
| T5 | NEEDS-CHANGES | 2 | 2 |

### Must-fix items (blocking merge)

1. **A2-1**: T2 create.go -- populate `CLIError.Details` with `step` and `details` per REQ-009-013 JSON error schema.
2. **A2-2**: T2 clone.go -- do not silently discard `mkdir -p` error.
3. **A5-1**: T5 quick_start.go -- add missing `packages_declared` field to check output per REQ-010-015.
4. **A5-2**: T5 quick_start.go -- wrap `test -d` command in bash for tilde expansion correctness.
