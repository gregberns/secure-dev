# Wave 2 Re-check Review

Date: 2026-04-09

## T2: Security Fixes (cc_1 worktree)

### Issue 1: Shell injection via package names

**STATUS: RESOLVED**

Defense-in-depth is complete:

1. **Validation at config load** (`internal/config/project.go`):
   - `validatePackageList()` rejects any package name that fails `safePackageNamePattern`
     (regex whitelist: `^[a-zA-Z0-9._/@+=:~\[\],-]+$`) OR contains `shellMetaChars`
     (`;|&$\`(){}><\n\r\"'\\!`). Both checks must pass.
   - `containsShellMeta()` is a secondary guard using `strings.ContainsAny`.

2. **Quoting at execution** (`internal/provision/packages.go`):
   - Every package name is wrapped with `shellQuote()` before interpolation into
     `bash -c` scripts, for both batch-install (`strings.Join(quoted, " ")`) and
     per-package (`shellQuote(pkg)`) paths.

3. **shellQuote** (`internal/provision/shellquote.go`):
   - Wraps in single quotes with `'\\''` escape for embedded single quotes.
   - Returns `''` for empty strings. Returns unquoted only for a strict safe-char set.

4. **Tests**: Property-based tests in `project_rapid_test.go` (config validation) and
   `packages_rapid_test.go` (execution quoting) cover metachar rejection, safe name acceptance,
   quote breakout defense, and empty-string rejection. Integration test validates
   end-to-end through `LoadProjectConfig` with file I/O.

### Issue 2: Shell injection via repo URL and branch

**STATUS: RESOLVED**

1. **Validation at config load** (`internal/config/project.go` `validateDeclarativeFields()`):
   - Repo must start with `https://` or `git@`, then is checked via `containsShellMeta()`.
   - Branch is checked for whitespace, `..` sequences, and `containsShellMeta()`.
   - `shellMetaChars` does NOT include `:` or `/` (correct -- these appear in valid URLs).

2. **Quoting at execution** (`internal/provision/clone.go`):
   - `qTarget := shellQuote(target)`, `qRepo := shellQuote(repo)`, `qBranch := shellQuote(branch)`
     are computed at the top of `CloneRepo()` and used consistently in all script
     interpolation: the idempotency check script, the fetch/checkout script, and the
     fresh clone script.

3. **Tests**: Property-based tests in `project_rapid_test.go` cover repo URL metachar
   rejection, valid HTTPS URL acceptance, and branch metachar rejection.
   `clone_rapid_test.go` covers URL quoting in clone scripts, branch quoting, and
   no-op for empty repo.

### Issue 3: mkdir error swallowed

**STATUS: RESOLVED**

In `internal/provision/clone.go` the fresh-clone path (line 74-84):
- `mkdir -p ~/projects` is executed as a separate command with `set -eux -o pipefail`.
- Both the exec error (`mkdirErr != nil`) and non-zero exit code (`mkdirExit != 0`)
  are checked and returned as errors with context.
- Property-based tests in `clone_rapid_test.go` verify that both mkdir exit-code
  failures and exec errors propagate correctly.

---

## T5: Quick-Start Fixes (cc_3 worktree)

### Tilde expansion fixed?

**STATUS: RESOLVED**

In `internal/cmd/quick_start.go` line 159:
```go
testPath := fmt.Sprintf("/home/ubuntu/projects/%s/.git", result.VMName)
testCmd := []string{"test", "-d", testPath}
```
Uses absolute `/home/ubuntu/projects/...` path instead of `~/projects/...`.
The command is `[]string{"test", "-d", testPath}` -- executed directly, not via
a shell, so no tilde expansion issue.

Comment on line 155 explicitly documents the rationale:
"Uses explicit /home/ubuntu path because tilde expansion requires a shell."

### `/.git` suffix added to repo check?

**STATUS: RESOLVED**

Line 159 checks `test -d /home/ubuntu/projects/<name>/.git` (not just the
directory itself). This correctly detects a cloned git repository rather than
just an arbitrary directory.

### `packages_declared` field present in JSON struct?

**STATUS: RESOLVED**

`quickStartCheckResult` struct (line 28):
```go
PackagesDeclared map[string]int `json:"packages_declared"`
```
Initialized as `map[string]int{}` on line 109. Tests in `quick_start_test.go`
(lines 208, 230-232, 384-386, 757) verify the field is present and is a JSON object.

---

## T3: Init Template + Rapid Tests (cc_2 worktree)

### Rapid tests added?

**STATUS: RESOLVED**

Three rapid property-based tests added in `internal/cmd/init_test.go` (uncommitted):

1. `TestProperty_DeriveVMNameAlwaysValid` -- verifies DeriveVMName always produces a name
   matching `^[a-z][a-z0-9-]{0,62}$` for arbitrary directory names (unicode, special chars,
   digits, long names).

2. `TestProperty_InitAlwaysProducesValidYAML` -- verifies sd init always produces valid,
   parseable YAML with required keys (name, modules, mounts) for any module combination.

3. `TestProperty_InitNeverOverwritesWithoutForce` -- verifies existing .sd.yaml is never
   modified by init without --force, regardless of file contents.

Additionally, conventional unit tests cover: git remote detection populating repo field,
absent remote leaving repo absent, commented packages/setup sections, and idempotency note.

Note: These changes are uncommitted (`M internal/cmd/init.go`, `M internal/cmd/init_test.go`).

---

## Summary

| Issue | Status |
|-------|--------|
| T2-1: Shell injection via package names | RESOLVED |
| T2-2: Shell injection via repo/branch | RESOLVED |
| T2-3: mkdir error swallowed | RESOLVED |
| T5-1: Tilde expansion in exec args | RESOLVED |
| T5-2: .git suffix on repo check | RESOLVED |
| T5-3: packages_declared in JSON struct | RESOLVED |
| T3: Rapid tests for init | RESOLVED |

All security-critical items have defense-in-depth: validation rejects bad input at
config load time, AND quoting neutralizes metacharacters at execution time even if
validation is somehow bypassed. Property-based tests cover the critical invariants.
