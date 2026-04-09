# E2E Docker Backend Test Report

**Date:** 2026-04-09
**Binary:** built from `./cmd/sd` on branch `int-ralph-loop`
**Platform:** darwin/arm64 (Docker Desktop)
**SD_HOME:** `/tmp/sd-e2e-test-<timestamp>` (isolated)
**SD_BACKEND:** `docker` (via env var)

## Summary Table

| # | Test | Command | Exit Code | Result |
|---|------|---------|-----------|--------|
| 1a | doctor | `sd doctor` | 0 | PASS |
| 1b | doctor --json | `sd doctor --json` | 0 | PASS |
| 2a | create | `sd create test-e2e --backend=docker --modules=none` | 0 | PASS (with warnings) |
| 2b | create --json (dup) | `sd create test-e2e --backend=docker --modules=none --json` | 1 | PASS |
| 3a | list | `sd list` | 0 | PASS |
| 3b | list --json | `sd list --json` | 0 | PASS |
| 4a | status | `sd status test-e2e` | 0 | PASS |
| 4b | status --json | `sd status test-e2e --json` | 0 | PASS |
| 5a | exec whoami | `sd exec test-e2e -- whoami` | 0 | PASS (returns "ubuntu") |
| 5b | exec whoami --json | `sd exec test-e2e --json -- whoami` | 0 | PASS |
| 6a | exec uname | `sd exec test-e2e -- uname -a` | 0 | PASS |
| 6b | exec uname --json | `sd exec test-e2e --json -- uname -a` | 0 | PASS |
| 7a | stop | `sd stop test-e2e` | 0 | PASS |
| 7b | stop --json (idempotent) | `sd stop test-e2e --json` | 0 | PASS |
| 8a | status (stopped) | `sd status test-e2e` | 0 | PASS (shows "stopped") |
| 8b | status --json (stopped) | `sd status test-e2e --json` | 0 | PASS |
| 9a | start | `sd start test-e2e` | 0 | PASS |
| 9b | start --json (idempotent) | `sd start test-e2e --json` | 0 | PASS |
| 10a | status (restarted) | `sd status test-e2e` | 0 | PASS (shows "running") |
| 10b | status --json (restarted) | `sd status test-e2e --json` | 0 | PASS |
| 11a | exec after restart | `sd exec test-e2e -- echo hello` | 0 | PASS |
| 11b | exec after restart --json | `sd exec test-e2e --json -- echo hello` | 0 | PASS |
| 12a | destroy (no --force) | `sd destroy test-e2e` | 1 | PASS (correct error) |
| 12b | destroy -f | `sd destroy test-e2e -f` | 0 | PASS |
| 12c | destroy --json (already gone) | `sd destroy test-e2e -f --json` | 1 | PASS |
| 13a | list (after destroy) | `sd list` | 0 | PASS (empty) |
| 13b | list --json (after destroy) | `sd list --json` | 0 | PASS |
| 14 | dup create | `sd create` x2 | 1 | PASS |
| 15 | destroy nonexistent | `sd destroy nonexistent -f` | 1 | PASS |
| 16 | exec nonexistent | `sd exec nonexistent -- ls` | 1 | PASS |
| 17 | stop nonexistent | `sd stop nonexistent` | 1 | PASS |
| 18 | start nonexistent | `sd start nonexistent` | 1 | PASS |
| 19 | status nonexistent | `sd status nonexistent` | 1 | PASS |
| 20 | create (default provisioning) | `sd create test-prov --backend=docker` | 1 | **FAIL** |
| 21 | create (--modules=base) | `sd create test-prov --backend=docker --modules=base` | 1 | **FAIL** |
| 22 | exec multi-word | `sd exec test -- echo "hello world"` | 0 | PASS |
| 23 | exec bash -c pipe | `sd exec test -- bash -c "echo hello \| tr h H"` | 0 | **FAIL** (empty output) |
| 24 | exec bash -c env var | `sd exec test -- bash -c "echo $USER"` | 0 | **FAIL** (empty output) |
| 25 | exec sudo bash -c | `sd exec test -- sudo bash -c "apt-get update"` | varies | **FAIL** (quoting bug) |
| 26 | exec simple commands | `sd exec test -- ls /home` | 0 | PASS |
| 27 | create prov --json | `sd create test --backend=docker --json` | 1 | PASS (correct error JSON) |
| 28 | exec on stopped VM | `sd exec test -- whoami` (after stop) | 1 | PASS |
| 29 | exec false | `sd exec test -- false` | 1 | PASS (exit code 1) |
| 30 | exec exit 42 | `sd exec test -- bash -c "exit 42"` | 0 | **FAIL** (returns 0, quoting bug) |
| 31 | exec id | `sd exec test -- id` | 0 | PASS |
| 32 | exec ls -la | `sd exec test -- ls -la /home/ubuntu/.ssh/` | 0 | PASS |
| 33 | list without SD_BACKEND | `sd list` (no SD_BACKEND) | 0 | **FAIL** (shows nothing) |
| 34 | status without SD_BACKEND | `sd status test-exec` (no SD_BACKEND) | 1 | **FAIL** (not found) |

**Pass: 28 | Fail: 7 | Total: 35**

## Bugs Found

### BUG-1 (Critical): SSH command quoting breaks `bash -c` patterns

**Severity:** Critical -- blocks all provisioning via Docker backend
**Affected:** `internal/backend/docker/ssh.go:execViaSSH()`

**Description:**
When `execViaSSH()` passes command arguments to SSH, it appends them as separate args:
```go
args = append(args, command...)  // e.g., ["sudo", "bash", "-c", "<script>"]
```

SSH concatenates remote command args with spaces before executing on the remote side. This means `bash -c "multi line script"` becomes `bash -c multi line script`, where `bash` only receives `multi` as the `-c` argument. The remaining words are treated as positional parameters or separate shell commands.

**Evidence:**
- `BASH_EXECUTION_STRING=set` (only first word of script)
- `SUDO_COMMAND='/usr/bin/bash -c set -eux -o pipefail'` (truncated)
- `apt-get update -y` runs outside sudo context as user ubuntu, fails with exit 100
- `bash -c "exit 42"` returns 0 (bash sees `exit` as arg, `42` as separate command)
- `bash -c "echo hello | tr h H"` returns empty output

**Reproduction:**
```bash
SD_BACKEND=docker sd create test --backend=docker  # fails during provisioning
sd exec test -- bash -c "echo hello | tr h H"       # empty output
sd exec test -- bash -c "exit 42"                    # returns 0 instead of 42
```

**Root Cause:**
`execViaSSH` at `internal/backend/docker/ssh.go:201` does:
```go
args = append(args, command...)
```
This passes each element of `command` as a separate arg to the `ssh` binary. SSH then concatenates them with spaces on the remote side, losing quoting for the `-c` argument.

**Fix:**
The command slice should be joined into a single shell-escaped string before passing to SSH:
```go
// Join command into a single properly-quoted remote command string
quotedParts := make([]string, len(command))
for i, part := range command {
    quotedParts[i] = shellQuote(part)
}
remoteCmd := strings.Join(quotedParts, " ")
args = append(args, remoteCmd)
```

Or more simply, concatenate with proper quoting:
```go
args = append(args, strings.Join(command, " "))
// But this fails for args with spaces -- need shell quoting
```

The correct approach is to use `shellescape` or manually quote each argument that contains spaces/special chars.

---

### BUG-2 (Medium): Duplicate SSH key generation on Docker backend

**Severity:** Medium -- warning noise, not a functional failure
**Affected:** `internal/cmd/create.go:setupSSH()` + `internal/backend/docker/docker.go:Create()`

**Description:**
The Docker backend's `Create()` method generates SSH keys at `$SD_HOME/vms/<name>/ssh/id_ed25519`. Then `create.go:setupSSH()` tries to generate them again and gets "ssh key already exists" warning.

**Evidence:**
```
Warning: could not generate SSH keys: ssh key already exists: /tmp/.../vms/test-e2e/ssh/id_ed25519
```

This appears on every `sd create --backend=docker`.

**Root Cause:**
Both the Docker backend (`docker.go:107-110`) and the create command (`create.go:289`) independently generate SSH keys. The Docker backend does it because it needs the key for its own SSH setup, and the create command does it as a generic post-creation step.

**Fix Options:**
1. Have the create command skip SSH key generation when the backend already generated them (check if keys exist before calling `generateSSHKeys`)
2. Have the Docker backend not generate keys, relying on the create command's `setupSSH()` to do it
3. Make `generateSSHKeys` idempotent (return success if keys already exist)

Option 3 is the safest and least disruptive.

---

### BUG-3 (Medium): Commands default to lima backend, ignoring per-VM backend

**Severity:** Medium -- Docker VMs invisible without `SD_BACKEND` env var
**Affected:** `list.go`, `status.go`, `exec.go`, `stop.go`, `start.go`, `destroy.go`

**Description:**
All lifecycle commands (`list`, `status`, `exec`, `stop`, `start`, `destroy`) resolve the backend from the global config default (which defaults to "lima"). There is no `--backend` flag on these commands, and they don't read the per-VM config to determine which backend a VM was created with.

**Evidence:**
```bash
# Create with docker
SD_BACKEND=docker sd create test --backend=docker --modules=none

# Without SD_BACKEND, list sees nothing
sd list
# => "No VMs found." + orphaned state warnings
```

**Root Cause:**
Each command resolves the backend name from config defaults and hardcodes "lima" as fallback. The per-VM config (`config.VMConfig.Backend`) stores the backend name but no command reads it.

**Fix:**
For named-VM commands (status, exec, stop, start, destroy), read `config.VMConfig.Backend` for the VM first and use that. For `list`, either query all registered backends or add a `--backend` flag.

---

### BUG-4 (Low): No way to skip default provisioning modules

**Severity:** Low -- workaround exists (`--modules=none` triggers warning but skips)
**Affected:** `internal/cmd/create.go:runCreateProvision()`

**Description:**
When `--modules` is not specified, `create` always provisions with default modules (`base`, `ssh-hardening`). There is no `--modules=none` or `--no-provision` flag to explicitly skip provisioning. Using `--modules=none` works as a workaround because "none" is not a valid module name, so resolution fails with a warning and provisioning is skipped.

**Fix:**
Add either `--no-provision` flag or handle `--modules=none` / `--modules=""` explicitly to skip all provisioning.

---

### BUG-5 (Low): List shows CPUs=0, empty memory/disk for Docker VMs

**Severity:** Low -- cosmetic
**Affected:** `internal/backend/docker/docker.go:List()`

**Description:**
The Docker backend's `List()` method doesn't populate CPUs, Memory, or Disk in the `VMInfo` struct, resulting in `0`, `""`, `""` in output.

**Evidence:**
```
NAME      STATUS   BACKEND  CPUS  MEMORY  DISK  IP
test-e2e  running  docker   0                   -
```

**Fix:**
Either query Docker for container resource limits or display "n/a" instead of zero/empty values for Docker containers.

---

### BUG-6 (Low): destroy --json leaks "Destroying VM..." progress to stdout

**Severity:** Low -- cosmetic, progress goes to stderr correctly in most cases

**Description:**
When running `sd destroy test -f --json` on a VM that doesn't exist, the progress message "Destroying VM..." appears before the JSON error. Investigation shows progress messages correctly go to stderr (verified with output redirection), so this is only visually confusing when stderr and stdout are interleaved in the terminal.

**Actual behavior:** Correct (stderr for progress, stdout for JSON). This is not a bug -- just a note that terminal interleaving can look confusing.

## Detailed Test Outputs

### TEST 1a: sd doctor
```
$ sd doctor
[checkmark] binary_limactl: found at /opt/homebrew/bin/limactl
[checkmark] binary_ssh: found at /usr/bin/ssh
[checkmark] binary_tmux: found at /opt/homebrew/bin/tmux
[checkmark] binary_rsync: found at /usr/bin/rsync
[checkmark] config: configuration valid
[checkmark] backend: backend "docker" available
[checkmark] sd_home_permissions: SD_HOME has correct permissions (0700)
[checkmark] git_credentials: no cached git credentials
[checkmark] project_security_config: no project directory configured
[checkmark] ssh_fragment_security: SSH fragments have correct security settings
EXIT_CODE=0
```

### TEST 2a: sd create test-e2e --backend=docker --modules=none
```
$ sd create test-e2e --backend=docker --modules=none
Creating VM "test-e2e" with backend "docker"...
Starting VM "test-e2e"...
Warning: could not generate SSH keys: ssh key already exists (BUG-2)
Warning: could not resolve modules: unknown module "none" (BUG-4 workaround)
VM "test-e2e" created successfully.
EXIT_CODE=0
```

### TEST 5a: sd exec test-e2e -- whoami
```
$ sd exec test-e2e -- whoami
ubuntu
EXIT_CODE=0
```

### TEST 20: sd create test-prov --backend=docker (default provisioning)
```
$ sd create test-prov --backend=docker
Creating VM "test-prov" with backend "docker"...
Starting VM "test-prov"...
Warning: could not generate SSH keys: ssh key already exists
Provisioning VM "test-prov" with 2 module(s)...
Provisioning failed, cleaning up VM "test-prov"...
Error: module "base" failed: module "base" script[0] failed (exit 100)
EXIT_CODE=1
```

### TEST 23: exec bash -c with pipe (BUG-1 demonstration)
```
$ sd exec test -- bash -c "echo hello | tr h H"
(empty output -- bash -c receives only "echo", rest is lost)
EXIT_CODE=0
```

### TEST 30: exec exit code quoting bug (BUG-1 demonstration)
```
$ sd exec test -- bash -c "exit 42"
(empty output -- bash -c receives only "exit", "42" is separate)
EXIT_CODE=0  (should be 42)
```

### TEST 33: list without SD_BACKEND (BUG-3 demonstration)
```
$ sd list  # without SD_BACKEND env var
Warning: orphaned VM state for "test-exec" in .../vms (no matching VM in backend)
No VMs found.
EXIT_CODE=0
```

## Recommendations

### Priority 1: Fix SSH command quoting (BUG-1)
This blocks all provisioning on Docker backend. The fix in `execViaSSH()` should properly shell-quote command arguments before passing them as the SSH remote command. This is the only critical bug.

### Priority 2: Per-VM backend resolution (BUG-3)
Commands should read the per-VM config to determine which backend to use, rather than always defaulting to the global default. This is essential for multi-backend workflows.

### Priority 3: Deduplicate SSH key generation (BUG-2)
Make `generateSSHKeys` idempotent or have the create command detect existing keys.

### Priority 4: Add --no-provision flag (BUG-4)
Add an explicit way to skip provisioning on create.

## Environment
- Go: 1.22+
- Docker Desktop for Mac (arm64)
- macOS Darwin 25.3.0
- sd binary built from commit 06566ba
