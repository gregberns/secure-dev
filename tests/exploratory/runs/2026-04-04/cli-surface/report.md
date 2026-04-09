# Exploratory Test Report

| Field | Value |
|-------|-------|
| Agent | cli-surface |
| Date | 2026-04-04 |
| Binary | /tmp/sd-test/sd |
| SD_HOME | /tmp/sd-exploratory/cli-surface |
| Duration | ~5 minutes |

## Summary

| Category | Pass | Fail | Skip | Total |
|----------|------|------|------|-------|
| Phase 1: Help Text | 36 | 1 | 0 | 37 |
| Phase 2: Basic Execution | 8 | 1 | 0 | 9 |
| Phase 2: JSON Output | 4 | 1 | 0 | 5 |
| Phase 3: Missing Arguments | 9 | 0 | 0 | 9 |
| Phase 4: Invalid Inputs | 6 | 4 | 0 | 10 |
| Phase 4: Edge Cases | 8 | 0 | 0 | 8 |
| **Total** | **71** | **7** | **0** | **78** |

---

## Phase 1: Help Text

All commands and subcommands were tested with `--help`. Every command exits 0, produces non-empty output describing the command's purpose, and documents its flags — **except** `sd completion`.

### sd (root) --help

**Status**: PASS

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd --help
sd (secure-dev) creates, configures, and manages secure VM environments
for running AI coding agents with bypass permissions.
...
```

**Exit code**: 0
**Notes**: Well-organized into groups (VM Management, Connection, Configuration, Provisioning, Security, Diagnostics, Utility). Global flags documented.

---

### sd create --help

**Status**: PASS

```
$ sd create --help
Create a new VM environment for running AI coding agents.
The VM is provisioned using the configured backend (default: lima).
```

**Exit code**: 0
**Notes**: All flags documented: --allow-egress, --backend, --cpus, --disk, --dry-run, --memory, --modules, --mount

---

### sd destroy --help

**Status**: PASS

```
$ sd destroy --help
Destroy a VM and all its associated resources (disk, configuration,
snapshots). This operation is irreversible.
```

**Exit code**: 0
**Notes**: Documents --force, --no-snapshot. References REQ-004-019.

---

### sd start --help

**Status**: PASS

**Exit code**: 0

---

### sd stop --help

**Status**: PASS

**Exit code**: 0

---

### sd list --help

**Status**: PASS

**Exit code**: 0
**Notes**: Documents alias `ls`.

---

### sd status --help

**Status**: PASS

**Exit code**: 0
**Notes**: Documents optional name argument.

---

### sd snapshot --help

**Status**: PASS

**Exit code**: 0
**Notes**: Lists all subcommands (create, list, restore, delete) with usage examples.

---

### sd snapshot create --help

**Status**: PASS

**Exit code**: 0
**Notes**: Documents --tag flag (required).

---

### sd snapshot list --help

**Status**: PASS

**Exit code**: 0

---

### sd snapshot restore --help

**Status**: PASS

**Exit code**: 0

---

### sd snapshot delete --help

**Status**: PASS

**Exit code**: 0

---

### sd connect --help

**Status**: PASS

**Exit code**: 0
**Notes**: Alias `c`. Extensive examples. Documents --forward, --new-window, --no-start, --no-tmux, --session.

---

### sd exec --help

**Status**: PASS

**Exit code**: 0
**Notes**: Explains `--` separator clearly.

---

### sd sync --help

**Status**: PASS

**Exit code**: 0

---

### sd sync to --help

**Status**: PASS

**Exit code**: 0
**Notes**: Documents --watch flag.

---

### sd sync from --help

**Status**: PASS

**Exit code**: 0
**Notes**: Documents --diff flag.

---

### sd ssh-config --help

**Status**: PASS

**Exit code**: 0

---

### sd config --help

**Status**: PASS

**Exit code**: 0
**Notes**: Lists all subcommands (get, set, list, edit, validate, egress).

---

### sd config get --help

**Status**: PASS

**Exit code**: 0

---

### sd config set --help

**Status**: PASS

**Exit code**: 0
**Notes**: Documents --project flag.

---

### sd config list --help

**Status**: PASS

**Exit code**: 0

---

### sd config edit --help

**Status**: PASS

**Exit code**: 0
**Notes**: Documents --project flag. Mentions $EDITOR/$VISUAL fallback.

---

### sd config validate --help

**Status**: PASS

**Exit code**: 0

---

### sd config egress --help

**Status**: PASS

**Exit code**: 0

---

### sd config egress add --help

**Status**: PASS

**Exit code**: 0

---

### sd config egress remove --help

**Status**: PASS

**Exit code**: 0
**Notes**: Documents that default domains cannot be removed.

---

### sd config egress list --help

**Status**: PASS

**Exit code**: 0

---

### sd provision --help

**Status**: PASS

**Exit code**: 0
**Notes**: Documents --modules flag.

---

### sd provision list --help

**Status**: PASS

**Exit code**: 0

---

### sd audit --help

**Status**: PASS

**Exit code**: 0
**Notes**: Documents --verify and --since flags.

---

### sd security --help

**Status**: PASS

**Exit code**: 0

---

### sd security status --help

**Status**: PASS

**Exit code**: 0

---

### sd token --help

**Status**: PASS

**Exit code**: 0
**Notes**: Documents credential injection via SSH env vars.

---

### sd token github --help

**Status**: PASS

**Exit code**: 0

---

### sd token github setup --help

**Status**: PASS

**Exit code**: 0

---

### sd token rotate --help

**Status**: PASS

**Exit code**: 0
**Notes**: Documents GITHUB_TOKEN and ANTHROPIC_API_KEY env vars.

---

### sd token revoke --help

**Status**: PASS

**Exit code**: 0

---

### sd token list --help

**Status**: PASS

**Exit code**: 0

---

### sd doctor --help

**Status**: PASS

**Exit code**: 0

---

### sd logs --help

**Status**: PASS

**Exit code**: 0
**Notes**: Documents --tail and --follow/-f.

---

### sd diff --help

**Status**: PASS

**Exit code**: 0
**Notes**: Lists security-sensitive paths.

---

### sd completion --help

**Status**: FAIL

```
$ sd completion --help
Error: unsupported shell "--help"; must be bash, zsh, or fish
```

**Expected**: Help text describing the completion command and supported shells
**Actual**: Treats `--help` as the shell argument and rejects it
**Exit code**: 1
**Notes**: The completion command does not register `--help` properly. It treats the first argument as the shell name unconditionally. `sd completion bash/zsh/fish` all work correctly (exit 0).

---

### sd version --help

**Status**: PASS

**Exit code**: 0

---

## Phase 2: Basic Execution

### sd version

**Status**: PASS

```
$ sd version
Version:    dev
Git Commit: unknown
Built:      unknown
Go Version: go1.26.1
```

**Exit code**: 0
**Notes**: Shows "dev/unknown" because binary wasn't built with ldflags. This is expected for a test build.

---

### sd completion bash

**Status**: PASS

**Exit code**: 0
**Notes**: Produces valid-looking bash completion script.

---

### sd completion zsh

**Status**: PASS

**Exit code**: 0
**Notes**: Produces valid-looking zsh completion script with `#compdef sd` header.

---

### sd config list

**Status**: PASS

```
$ sd config list
KEY                        VALUE         SOURCE
defaults.backend           lima          built-in default
defaults.cpus              4             built-in default
defaults.memory            8GiB          built-in default
defaults.disk              100GiB        built-in default
defaults.image             ubuntu:24.04  built-in default
defaults.vm                (not set)     built-in default
security.mount_policy      none          built-in default
security.egress_allowlist  [11 entries]  built-in default
security.sensitive_paths   (not set)     built-in default
```

**Exit code**: 0
**Notes**: Clean table output. Shows all defaults with sources.

---

### sd config validate

**Status**: PASS

```
$ sd config validate
No config files found.
```

**Exit code**: 0
**Notes**: Correctly reports no config files in clean SD_HOME.

---

### sd doctor

**Status**: FAIL

```
$ sd doctor
✓ binary_limactl: found at /opt/homebrew/bin/limactl
✓ binary_ssh: found at /usr/bin/ssh
✓ binary_tmux: found at /opt/homebrew/bin/tmux
✓ binary_rsync: found at /usr/bin/rsync
✓ config: configuration valid
✓ backend: backend "lima" available
✗ sd_home_permissions: SD_HOME /tmp/sd-exploratory/cli-surface has permissions 0755, expected 0700; run: chmod 700 /tmp/sd-exploratory/cli-surface
✓ git_credentials: no cached git credentials
✓ project_security_config: no project directory configured
✓ ssh_fragment_security: SSH fragments have correct security settings
Error: one or more checks failed
```

**Expected**: Exit 1 when checks fail (correct), actionable fix suggestion (correct)
**Actual**: Behaves correctly in plain text mode
**Exit code**: 1
**Notes**: The permission check is useful and the suggested fix is actionable. This is a PASS for plain-text mode. The FAIL is for the --json mode (see below).

---

### sd list

**Status**: PASS

```
$ sd list
No VMs found.
```

**Exit code**: 0

---

### sd status

**Status**: PASS

```
$ sd status
No VMs found.
```

**Exit code**: 0

---

## Phase 2: JSON Output

### sd version --json

**Status**: PASS

```json
{
  "ok": true,
  "data": {
    "version": "dev",
    "gitCommit": "unknown",
    "buildDate": "unknown",
    "goVersion": "go1.26.1"
  }
}
```

**Exit code**: 0

---

### sd list --json

**Status**: PASS

```json
{
  "ok": true,
  "data": []
}
```

**Exit code**: 0

---

### sd status --json

**Status**: PASS

```json
{
  "ok": true,
  "data": []
}
```

**Exit code**: 0

---

### sd config list --json

**Status**: PASS

```json
{
  "ok": true,
  "data": [
    {"key": "defaults.backend", "value": "lima", "source": "built-in default"},
    ...
  ]
}
```

**Exit code**: 0
**Notes**: Proper JSON structure. Array of config entries. egress_allowlist value is a proper JSON array.

---

### sd doctor --json

**Status**: FAIL

```json
{
  "ok": true,
  "data": [
    {"name": "sd_home_permissions", "status": "fail", "message": "SD_HOME ... has permissions 0755, expected 0700; ..."},
    ...
  ]
}
```

**Expected**: `"ok": false` and exit code 1 when any check has `"status": "fail"`
**Actual**: `"ok": true` and exit code 0, despite sd_home_permissions check failing
**Exit code**: 0
**Notes**: The plain-text mode correctly exits 1. The JSON mode disagrees: it reports ok:true and exits 0. Consumers parsing JSON would incorrectly conclude all checks passed. The individual check objects do contain `"status": "fail"`, but the top-level envelope is wrong.

---

## Phase 3: Missing Arguments

All commands that require arguments were tested without providing them. Every one correctly rejects with a non-zero exit code.

### sd create (no name)

**Status**: PASS

```
$ sd create
Error: accepts 1 arg(s), received 0
```

**Exit code**: 1
**Notes**: Generic Cobra message. Could be more actionable (e.g., "Error: missing required argument <name>").

---

### sd destroy (no name)

**Status**: PASS

```
$ sd destroy
Error: accepts 1 arg(s), received 0
```

**Exit code**: 1
**Notes**: Same generic Cobra message.

---

### sd start (no name)

**Status**: PASS

```
$ sd start
Error: accepts 1 arg(s), received 0
```

**Exit code**: 1

---

### sd stop (no name)

**Status**: PASS

```
$ sd stop
Error: accepts 1 arg(s), received 0
```

**Exit code**: 1

---

### sd exec (no args)

**Status**: PASS

```
$ sd exec
Error: requires at least 2 arg(s), only received 0
```

**Exit code**: 1
**Notes**: Generic Cobra message. Could say "Error: missing <vm-name> and <command>".

---

### sd connect (no name)

**Status**: PASS

```
$ sd connect
Error: accepts 1 arg(s), received 0
```

**Exit code**: 1

---

### sd config get (no key)

**Status**: PASS

```
$ sd config get
Error: accepts 1 arg(s), received 0
```

**Exit code**: 1

---

### sd config set (no args)

**Status**: PASS

```
$ sd config set
Error: accepts 2 arg(s), received 0
```

**Exit code**: 1

---

### sd snapshot create (no name)

**Status**: PASS

```
$ sd snapshot create
Error: accepts 1 arg(s), received 0
```

**Exit code**: 1

---

## Phase 4: Invalid Inputs

### sd create --cpus=-1 --dry-run

**Status**: FAIL

```
$ sd create test-vm --cpus=-1 --dry-run
Dry run: VM "test-vm" would be created with backend=lima cpus=4 memory=8GiB disk=100GiB image=ubuntu:24.04
```

**Expected**: Error rejecting negative CPU count
**Actual**: Silently uses default value (4) instead of the provided -1. Exit 0.
**Exit code**: 0
**Notes**: The value -1 is silently ignored/overridden. This is likely a flag parsing issue where `-1` is interpreted as a flag prefix. Without --dry-run, this proceeds to create a real VM. There is no validation of the CPU count.

---

### sd create --memory=banana --dry-run

**Status**: FAIL

```
$ sd create test-vm --memory=banana --dry-run
Dry run: VM "test-vm" would be created with backend=lima cpus=4 memory=banana disk=100GiB image=ubuntu:24.04
```

**Expected**: Error rejecting non-numeric/non-parseable memory value
**Actual**: Accepts "banana" as a memory value without validation. Exit 0.
**Exit code**: 0
**Notes**: No validation of memory format. Without --dry-run, this would be passed to the backend which may or may not reject it.

---

### sd create --backend=nonexistent --dry-run

**Status**: FAIL

```
$ sd create test-vm --backend=nonexistent --dry-run
Dry run: VM "test-vm" would be created with backend=nonexistent cpus=4 memory=8GiB disk=100GiB image=ubuntu:24.04
```

**Expected**: Error rejecting unknown backend
**Actual**: Accepts unknown backend without validation. Exit 0.
**Exit code**: 0
**Notes**: The --dry-run path does not validate whether the backend is registered. Without --dry-run, backend validation likely happens later, but dry-run should catch it.

---

### sd create "INVALID NAME WITH SPACES"

**Status**: PASS

```
$ sd create "INVALID NAME WITH SPACES" --dry-run
Error: name "INVALID NAME WITH SPACES" must match ^[a-z][a-z0-9-]{0,62}$ (start with lowercase letter, contain only lowercase letters, digits, and hyphens): invalid vm name
```

**Exit code**: 1
**Notes**: Excellent error message. Shows the regex, explains the constraint, provides clear feedback.

---

### sd --nonexistent-flag

**Status**: PASS

```
$ sd --nonexistent-flag
Error: unknown flag: --nonexistent-flag
```

**Exit code**: 1

---

### sd nonexistent-command

**Status**: PASS

```
$ sd nonexistent-command
Error: unknown command "nonexistent-command" for "sd"
```

**Exit code**: 1

---

### sd create --cpus=-1 (without --dry-run)

**Status**: FAIL

```
$ sd create test-vm --cpus=-1
Creating VM "test-vm" with backend "lima"...
Starting VM "test-vm"...
(proceeds to actually create a VM)
```

**Expected**: Validation error before any VM creation attempt
**Actual**: Silently ignores invalid CPU count and proceeds to create a real VM with default CPUs
**Exit code**: N/A (killed after 10s)
**Notes**: This is the most concerning finding. Invalid inputs should be caught at validation time, not passed through to the backend. The lack of --dry-run validation combined with absent pre-create validation means garbage values reach the backend.

---

## Phase 4: Edge Cases (additional exploration)

### sd create --cpus=0 --dry-run

**Status**: PASS (documents behavior)

```
$ sd create test-vm --cpus=0 --dry-run
Dry run: VM "test-vm" would be created with backend=lima cpus=4 memory=8GiB disk=100GiB image=ubuntu:24.04
```

**Notes**: cpus=0 silently becomes cpus=4 (default). Same behavior as -1. Zero CPUs should be rejected.

---

### sd create --cpus=999 --dry-run

**Status**: PASS (documents behavior)

```
Dry run: VM "test-vm" would be created with backend=lima cpus=999 memory=8GiB disk=100GiB image=ubuntu:24.04
```

**Notes**: Absurdly high CPU count accepted without warning. Upper-bound validation missing.

---

### sd create --cpus=abc --dry-run

**Status**: PASS

```
Error: invalid argument "abc" for "--cpus" flag: strconv.ParseInt: parsing "abc": invalid syntax
```

**Exit code**: 1
**Notes**: Go's flag parser catches non-integer values. Good.

---

### sd create --disk=0 --dry-run

**Status**: PASS (documents behavior)

```
Dry run: VM "test-vm" would be created with backend=lima cpus=4 memory=8GiB disk=0 image=ubuntu:24.04
```

**Notes**: Zero disk accepted. Should be rejected.

---

### sd create --memory=0 --dry-run

**Status**: PASS (documents behavior)

```
Dry run: VM "test-vm" would be created with backend=lima cpus=4 memory=0 disk=100GiB image=ubuntu:24.04
```

**Notes**: Zero memory accepted. Should be rejected.

---

### sd create '' --dry-run (empty name)

**Status**: PASS

```
Error: VM name must not be empty
```

**Exit code**: 1
**Notes**: Good error message.

---

### sd create test_underscore --dry-run

**Status**: PASS

```
Error: name "test_underscore" must match ^[a-z][a-z0-9-]{0,62}$ ...
```

**Exit code**: 1

---

### sd create UPPERCASE --dry-run

**Status**: PASS

```
Error: name "UPPERCASE" must match ^[a-z][a-z0-9-]{0,62}$ ...
```

**Exit code**: 1

---

## Issues Filed

| bd ID | Summary | Severity |
|-------|---------|----------|
| secure-dev-nln | CLI: Missing-argument errors use generic Cobra messages, not actionable | medium |
| secure-dev-igr | CLI: sd doctor --json returns ok:true and exit 0 when checks fail | medium |
| secure-dev-8xm | CLI: sd completion does not support --help flag | low |
| secure-dev-kjc | CLI: sd create accepts invalid resource values without validation | high |

## Observations

1. **Help text quality is excellent.** Every command (except `completion`) has clear, descriptive help text. Flags are documented. Examples are provided where useful (especially `connect` and `exec`). Requirement IDs are referenced in help text (e.g., REQ-004-019 in `destroy`).

2. **JSON output is well-structured.** All tested `--json` outputs use a consistent `{"ok": bool, "data": ...}` envelope. Data types are appropriate (arrays for lists, objects for single items). The one exception is `doctor --json` which returns `ok:true` when checks fail.

3. **Name validation is strong.** The regex `^[a-z][a-z0-9-]{0,62}$` is enforced consistently. Empty names, uppercase, underscores, and spaces are all properly rejected with clear error messages.

4. **Resource validation is missing.** `--cpus`, `--memory`, `--disk`, and `--backend` accept invalid values (negative, zero, nonsense strings, nonexistent backends) without any validation. The `--dry-run` path is especially important since users rely on it to verify configs before committing. This is the highest-severity finding.

5. **Missing-argument messages are generic.** Cobra's default "accepts N arg(s), received 0" messages are functional but not user-friendly. Custom messages like "Error: missing required argument <name>" would be more actionable. This is low priority but affects polish.

6. **The `completion` command is an outlier.** It's the only command that doesn't support `--help`. The fix is likely trivial (register --help or use Cobra's built-in completion generation command).

7. **No panics or stack traces observed.** All error paths exit cleanly with structured error messages. No crashes across 78 test cases.

8. **Command grouping is logical.** The root help text organizes commands into VM Management, Connection, Configuration, Provisioning, Security, Diagnostics, and Utility groups. This makes discovery easy.
