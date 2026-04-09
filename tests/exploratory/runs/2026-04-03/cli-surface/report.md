# Exploratory Test Report

| Field | Value |
|-------|-------|
| Agent | cli-surface |
| Date | 2026-04-03 |
| Binary | /tmp/sd-test/sd |
| SD_HOME | /tmp/sd-exploratory/cli-surface |
| Duration | ~5 minutes |

## Summary

| Category | Pass | Fail | Skip | Total |
|----------|------|------|------|-------|
| Phase 1: Help Text | 34 | 1 | 0 | 35 |
| Phase 2: Basic Execution | 8 | 0 | 0 | 8 |
| Phase 3: Missing Arguments | 0 | 9 | 0 | 9 |
| Phase 4: Invalid Inputs | 5 | 1 | 0 | 6 |
| Bonus: --json Output | 4 | 1 | 0 | 5 |
| **Total** | **51** | **12** | **0** | **63** |

---

## Phase 1: Help Text

Every command and subcommand was run with `--help`. All must exit 0 with non-empty, descriptive output.

### sd --help

**Status**: PASS

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd --help
sd (secure-dev) creates, configures, and manages secure VM environments
for running AI coding agents with bypass permissions.
...
Use "sd [command] --help" for more information about a command.
```

**Exit code**: 0
**Notes**: Clean output, well-organized command groups (VM Management, Connection, Configuration, Provisioning, Security, Diagnostics, Utility). All global flags documented.

---

### sd create --help

**Status**: PASS

**Exit code**: 0
**Notes**: All flags documented (--cpus, --memory, --disk, --backend, --modules, --mount, --allow-egress, --dry-run).

---

### sd destroy --help

**Status**: PASS

**Exit code**: 0
**Notes**: Documents --force and --no-snapshot flags. Mentions REQ-004-019.

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

---

### sd snapshot --help

**Status**: PASS

**Exit code**: 0
**Notes**: Lists all subcommands with usage examples.

---

### sd snapshot create --help

**Status**: PASS

**Exit code**: 0

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
**Notes**: Good examples section. Documents alias `c`, all flags (--forward, --no-start, --no-tmux, --session, --new-window).

---

### sd exec --help

**Status**: PASS

**Exit code**: 0
**Notes**: Documents `--` separator convention with examples.

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
**Notes**: Documents --project flag.

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

---

### sd config egress list --help

**Status**: PASS

**Exit code**: 0

---

### sd provision --help

**Status**: PASS

**Exit code**: 0

---

### sd provision list --help

**Status**: PASS

**Exit code**: 0

---

### sd audit --help

**Status**: PASS

**Exit code**: 0
**Notes**: Documents --since and --verify flags.

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
**Notes**: Documents --tail and --follow/-f flags.

---

### sd diff --help

**Status**: PASS

**Exit code**: 0
**Notes**: Lists all security-sensitive paths that are monitored.

---

### sd completion --help

**Status**: FAIL

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd completion --help
Error: unsupported shell "--help"; must be bash, zsh, or fish
```

**Expected**: Exit 0 with help text describing the completion command and supported shells.
**Actual**: Exit 1 with error treating `--help` as a shell argument.
**Exit code**: 1
**Notes**: Also fails with `-h`. The command does not register Cobra's help flag, so both `--help` and `-h` are parsed as the shell positional argument. `sd help completion` works as a workaround. `sd completion bash`, `sd completion zsh`, and `sd completion fish` all work correctly.

---

### sd version --help

**Status**: PASS

**Exit code**: 0

---

## Phase 2: Basic Execution

Commands that should work without a running VM backend.

### sd version

**Status**: PASS

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd version
Version:    dev
Git Commit: unknown
Built:      unknown
Go Version: go1.26.1
```

**Exit code**: 0

---

### sd completion bash

**Status**: PASS

**Exit code**: 0
**Notes**: Produces valid bash completion script (> 100 lines).

---

### sd completion zsh

**Status**: PASS

**Exit code**: 0
**Notes**: Produces valid zsh completion script.

---

### sd config list

**Status**: PASS

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd config list
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
**Notes**: Clean tabular output with source attribution. All expected defaults present.

---

### sd config validate

**Status**: PASS

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd config validate
No config files found.
```

**Exit code**: 0
**Notes**: Correctly reports no config files in the test SD_HOME.

---

### sd doctor

**Status**: PASS

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd doctor
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

**Exit code**: 1
**Notes**: Correct behavior -- exits non-zero because SD_HOME permissions check fails. Error message is actionable (tells you exactly what command to run). See Phase "Bonus" for --json inconsistency.

---

### sd list

**Status**: PASS

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd list
No VMs found.
```

**Exit code**: 0

---

### sd status

**Status**: PASS

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd status
No VMs found.
```

**Exit code**: 0

---

## Phase 3: Missing Arguments

Commands that require arguments, run without providing them. Each must exit non-zero with an actionable error message telling you what's missing.

### sd create (no name)

**Status**: FAIL

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd create
Error: accepts 1 arg(s), received 0
```

**Expected**: Non-zero exit with message like `Error: missing required argument <name>` or showing usage.
**Actual**: Generic Cobra message "accepts 1 arg(s), received 0" -- does not name the missing argument.
**Exit code**: 1

---

### sd destroy (no name)

**Status**: FAIL

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd destroy
Error: accepts 1 arg(s), received 0
```

**Expected**: Message naming the missing `<name>` argument.
**Actual**: Generic Cobra message.
**Exit code**: 1

---

### sd start (no name)

**Status**: FAIL

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd start
Error: accepts 1 arg(s), received 0
```

**Expected**: Message naming the missing `<name>` argument.
**Actual**: Generic Cobra message.
**Exit code**: 1

---

### sd stop (no name)

**Status**: FAIL

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd stop
Error: accepts 1 arg(s), received 0
```

**Expected**: Message naming the missing `<name>` argument.
**Actual**: Generic Cobra message.
**Exit code**: 1

---

### sd exec (no name, no command)

**Status**: FAIL

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd exec
Error: requires at least 2 arg(s), only received 0
```

**Expected**: Message like `Error: missing <vm-name> and <command>`.
**Actual**: Generic Cobra message.
**Exit code**: 1

---

### sd connect (no name)

**Status**: FAIL

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd connect
Error: accepts 1 arg(s), received 0
```

**Expected**: Message naming the missing `<vm-name>` argument.
**Actual**: Generic Cobra message.
**Exit code**: 1

---

### sd config get (no key)

**Status**: FAIL

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd config get
Error: accepts 1 arg(s), received 0
```

**Expected**: Message naming the missing `<key>` argument.
**Actual**: Generic Cobra message.
**Exit code**: 1

---

### sd config set (no key, no value)

**Status**: FAIL

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd config set
Error: accepts 2 arg(s), received 0
```

**Expected**: Message naming the missing `<key>` and `<value>` arguments.
**Actual**: Generic Cobra message.
**Exit code**: 1

---

### sd snapshot create (no name)

**Status**: FAIL

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd snapshot create
Error: accepts 1 arg(s), received 0
```

**Expected**: Message naming the missing `<vm>` argument.
**Actual**: Generic Cobra message.
**Exit code**: 1

---

## Phase 4: Invalid Inputs

### sd create --cpus=-1

**Status**: FAIL

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd create testvm --cpus=-1
Creating VM "testvm" with backend "lima"...
Starting VM "testvm"...
```

**Expected**: Immediate validation error rejecting negative CPU count.
**Actual**: The command accepted --cpus=-1 and attempted to create a real VM via limactl. The process had to be killed after it hung waiting for the backend. No input validation for CPU count.
**Exit code**: 143 (SIGTERM -- killed manually)
**Notes**: This is a significant validation gap. Negative CPUs should be caught before any backend call.

---

### sd create --memory=banana

**Status**: PASS

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd create testvm --memory=banana
Creating VM "testvm" with backend "lima"...
Error: failed to create VM "testvm": invalid vm config for "testvm": memory "banana" must be in <number><unit> format (e.g., 4GiB, 8G): invalid vm config
```

**Exit code**: 1
**Notes**: Good error message with format hint and example.

---

### sd create --backend=nonexistent

**Status**: PASS

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd create testvm --backend=nonexistent
Error: backend "nonexistent" is not available: backend "nonexistent" not registered; available: [avf docker incus lima]: backend not available
```

**Exit code**: 1
**Notes**: Excellent error message -- lists available backends.

---

### sd create "INVALID NAME WITH SPACES"

**Status**: PASS

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd create "INVALID NAME WITH SPACES"
Error: name "INVALID NAME WITH SPACES" must match ^[a-z][a-z0-9-]{0,62}$ (start with lowercase letter, contain only lowercase letters, digits, and hyphens): invalid vm name
```

**Exit code**: 1
**Notes**: Excellent error message -- shows the regex and explains the naming rules.

---

### sd --nonexistent-flag

**Status**: PASS

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd --nonexistent-flag
Error: unknown flag: --nonexistent-flag
```

**Exit code**: 1

---

### sd nonexistent-command

**Status**: PASS

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd nonexistent-command
Error: unknown command "nonexistent-command" for "sd"
```

**Exit code**: 1

---

## Bonus: --json Output Checks

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
**Notes**: Clean JSON envelope with ok/data structure.

---

### sd list --json

**Status**: PASS

```json
{"ok": true, "data": []}
```

**Exit code**: 0

---

### sd status --json

**Status**: PASS

```json
{"ok": true, "data": []}
```

**Exit code**: 0

---

### sd config list --json

**Status**: PASS

**Exit code**: 0
**Notes**: Full JSON output with key/value/source for each config entry. Egress allowlist correctly serialized as array.

---

### sd doctor --json

**Status**: FAIL

```
$ SD_HOME=/tmp/sd-exploratory/cli-surface /tmp/sd-test/sd doctor --json
{
  "ok": true,
  "data": [
    ...
    {
      "name": "sd_home_permissions",
      "status": "fail",
      "message": "SD_HOME ... has permissions 0755, expected 0700; ..."
    },
    ...
  ]
}
```

**Expected**: `"ok": false` and exit code 1, matching the text-mode behavior (which exits 1 with "one or more checks failed").
**Actual**: `"ok": true` and exit code 0, despite containing a check with `"status": "fail"`.
**Exit code**: 0 (text mode exits 1)
**Notes**: The JSON and text modes disagree on whether failing checks constitute an overall failure. JSON consumers cannot rely on the `ok` field to detect problems -- they must inspect individual check statuses.

---

## Issues Filed

| bd ID | Summary | Severity |
|-------|---------|----------|
| secure-dev-p1i | `sd completion --help` exits 1 -- treats --help as shell arg | medium |
| secure-dev-5im | `sd create --cpus=-1` accepted without validation, attempts VM creation | high |
| secure-dev-igr | `sd doctor --json` returns ok:true and exit 0 when checks fail | high |
| secure-dev-nln | Missing-argument errors use generic Cobra messages, not actionable | low |

## Observations

1. **Help text quality is excellent.** 34/35 commands have well-structured help with examples, flag documentation, and clear descriptions. The completion command is the only exception.

2. **JSON output is well-structured.** Consistent `{ok, data}` envelope across all commands tested. The doctor --json bug is the only inconsistency.

3. **Input validation is inconsistent.** Memory format (`banana`) and VM names (`INVALID NAME WITH SPACES`) are validated with excellent error messages. But CPU count (`-1`) passes validation entirely. Likely disk size has the same gap.

4. **Missing-argument errors are a systematic UX issue.** All 9 commands tested produce Cobra's default "accepts N arg(s), received 0" message. None tell the user WHAT argument is needed. This could be fixed once with a custom Cobra `Args` validator that includes the usage line.

5. **No panics or stack traces observed.** Every invalid input produced a clean error message (except the CPU validation gap). The binary is robust against unexpected inputs.

6. **Backend error messages are helpful.** The `--backend=nonexistent` error lists all available backends. The `--memory=banana` error shows the expected format with examples. These are the gold standard for the project.

7. **The `sd doctor` command is well-designed** -- checks are comprehensive, actionable (tells you exact fix commands), and the individual check structure in JSON is good. The only issue is the top-level `ok` field not aggregating sub-check failures.
