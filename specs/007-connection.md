# 007: Connection and Interaction

| Field        | Value      |
|--------------|------------|
| Status       | draft      |
| Created      | 2026-03-27 |
| Last Updated | 2026-03-27 |
| Authors      | claude     |
| Reviewers    |            |

## Overview

This spec defines how users connect to and interact with VMs managed by `sd`. It covers the `sd connect`, `sd exec`, and `sd sync` commands, SSH key and config management, tmux session integration, environment variable injection at connection time, and connection health diagnostics. The goal is to provide a fast, secure, and ergonomic workflow for entering a VM, running commands remotely, and transferring files -- all without exposing host secrets or requiring manual SSH configuration.

## Goals

- G1: Provide a single command (`sd connect`) that handles starting, SSH-ing, and attaching to a tmux session in one step.
- G2: Manage SSH keys and config automatically so users never hand-edit SSH config for `sd` VMs.
- G3: Support non-interactive command execution inside VMs via `sd exec` for scripting and automation.
- G4: Provide efficient file synchronization between host and VM via `sd sync`.
- G5: Inject environment variables (including secrets resolved from the host) at connection time without persisting them to disk inside the VM.
- G6: Support multiple concurrent terminal connections to the same VM.
- G7: Surface actionable error messages when connections fail.

## Non-Goals

- NG1: This spec does not cover VM creation, deletion, or lifecycle management (see [003-vm-backend.md](003-vm-backend.md)).
- NG2: This spec does not cover provisioning of software inside the VM (see [006-provisioning.md](006-provisioning.md)).
- NG3: This spec does not define egress/firewall rules or credential scoping (see [004-security.md](004-security.md)).
- NG4: This spec does not cover GUI-based remote desktop or VNC connections.
- NG5: This spec does not cover port forwarding for services beyond the `--forward` flag on `sd connect`.

## Requirements

### REQ-007-001: Connect Command

The CLI MUST provide an `sd connect <vm-name>` command (alias `sd c <vm-name>`) that establishes an interactive session with the named VM.

**Acceptance criteria:**
- [ ] `sd connect myvm` and `sd c myvm` both initiate a connection to the VM named `myvm`.
- [ ] The command accepts a single positional argument: the VM name.
- [ ] If the VM name does not exist, the command MUST exit with code 1 and print an actionable error naming the VM and suggesting `sd list`.
- [ ] The command MUST support `--json` output for connection status and errors.

### REQ-007-002: Auto-Start on Connect

If the target VM is in a stopped state when `sd connect` is invoked, `sd` MUST automatically start the VM before connecting. This is non-interactive by default to support scripted workflows.

**Acceptance criteria:**
- [ ] When the VM is stopped, `sd connect` starts the VM automatically without prompting.
- [ ] The `--no-start` flag prevents auto-start; if the VM is stopped and `--no-start` is passed, the command exits with code 1 and an error message indicating the VM is stopped.
- [ ] If the VM fails to start, the command MUST exit with code 1 and print the start failure reason.
- [ ] If the VM is already running, no start is attempted.

### REQ-007-003: SSH Key Generation Per VM

At VM creation time, `sd` MUST generate a dedicated SSH key pair for that VM.

**Acceptance criteria:**
- [ ] An Ed25519 SSH key pair is generated and stored at `~/.sd/vms/<name>/ssh/id_ed25519` and `~/.sd/vms/<name>/ssh/id_ed25519.pub`.
- [ ] The private key file has permissions `0600`.
- [ ] The public key directory has permissions `0700`.
- [ ] Each VM has its own unique key pair; keys are never shared between VMs.
- [ ] The public key is injected into the VM's `authorized_keys` during creation.

### REQ-007-004: SSH Config Management

`sd` MUST manage SSH configuration entries for each VM so that standard SSH tooling (e.g., `ssh`, `scp`, `rsync`) can connect using the managed identity.

**Acceptance criteria:**
- [ ] For each VM, an SSH config fragment is written to `~/.ssh/config.d/sd-<name>`.
- [ ] The fragment defines `Host sd-<name>` with the correct hostname/IP, port, user, and `IdentityFile` pointing to the VM's dedicated key.
- [ ] For TCP-based SSH connections: `StrictHostKeyChecking` is set to `yes` and `UserKnownHostsFile` is set to `$SD_HOME/vms/<name>/ssh/known_hosts`. The host key is captured at VM creation time.
- [ ] For VSOCK-based SSH connections: `StrictHostKeyChecking` is set to `no` and `UserKnownHostsFile` is set to `/dev/null` (VSOCK does not traverse a network, so MITM is not possible).
- [ ] The fragment includes `ForwardAgent no` to prevent SSH agent forwarding by default.
- [ ] The fragment includes `ForwardX11 no` to prevent X11 forwarding.
- [ ] The fragment includes `LogLevel ERROR` to suppress SSH warnings during normal use.
- [ ] If `~/.ssh/config` does not include `Include config.d/*`, `sd` MUST warn the user with instructions to add it.
- [ ] The fragment is removed when the VM is destroyed.

### REQ-007-005: VSOCK Transport Support

When using the Lima+VZ (Apple Virtualization framework) backend, `sd` SHOULD use VSOCK transport for SSH connections to reduce latency.

**Acceptance criteria:**
- [ ] When the backend is Lima+VZ and VSOCK is available, the SSH config fragment uses `ProxyCommand limactl ssh --stdio <name>` instead of TCP host/port.
- [ ] When VSOCK is not available, the connection falls back to standard TCP SSH transparently.
- [ ] The transport in use is visible via `sd status <vm>` output.

### REQ-007-006: SSH Config Print Command

The CLI MUST provide `sd ssh-config <vm-name>` to print the SSH config fragment for a VM.

**Acceptance criteria:**
- [ ] The command prints the SSH config to stdout, suitable for appending to `~/.ssh/config` or piping.
- [ ] The output includes `Host`, `HostName`, `Port`, `User`, `IdentityFile`, and any other relevant directives.
- [ ] If the VM does not exist, the command exits with code 1 and prints an error.
- [ ] The command supports `--json` to output the config as a JSON object with individual fields.

### REQ-007-007: Port Forwarding on Connect

`sd connect` MUST support forwarding ports between the host and the VM.

**Acceptance criteria:**
- [ ] `sd connect <vm> --forward 8080:8080` establishes local port forwarding (host 8080 to VM 8080).
- [ ] Multiple `--forward` flags MAY be specified to forward multiple ports.
- [ ] The format is `<host-port>:<guest-port>` (bind address defaults to `127.0.0.1` on the host).
- [ ] An extended format `<bind-addr>:<host-port>:<guest-port>` MAY be used to specify a bind address.
- [ ] Port forwarding remains active for the duration of the connection.
- [ ] If a forwarded port is already in use on the host, the command MUST exit with code 1 and name the conflicting port.

### REQ-007-008: tmux as Default Session Manager

On `sd connect`, the CLI MUST attach to or create a tmux session inside the VM.

**Acceptance criteria:**
- [ ] If a tmux session named `sd-<vm-name>` exists inside the VM, the connection attaches to it.
- [ ] If no such session exists, a new tmux session named `sd-<vm-name>` is created and attached.
- [ ] The tmux session name is deterministic: `sd-<vm-name>`.
- [ ] On disconnect (detach or exit), the tmux session persists inside the VM for re-attachment.

### REQ-007-009: Named tmux Sessions

`sd connect` MUST support creating and attaching to named tmux sessions.

**Acceptance criteria:**
- [ ] `sd connect <vm> --session work` attaches to or creates a tmux session named `work`.
- [ ] The `--session` flag overrides the default `sd-<vm-name>` session name.
- [ ] Session names MUST consist of alphanumeric characters, hyphens, and underscores only.
- [ ] Invalid session names MUST produce an error with code 2 explaining the naming constraints.

### REQ-007-010: New tmux Window

`sd connect` MUST support opening a new window in an existing tmux session.

**Acceptance criteria:**
- [ ] `sd connect <vm> --new-window` connects to the VM and creates a new window in the existing `sd-<vm-name>` tmux session (or the session specified by `--session`).
- [ ] If the target session does not exist, it is created with the new window.
- [ ] The new window becomes the active window upon attachment.

### REQ-007-011: tmux Default Configuration

`sd` MUST provision a tmux configuration with sane defaults inside each VM.

**Acceptance criteria:**
- [ ] Mouse mode is enabled (`set -g mouse on`).
- [ ] The status bar displays the VM name.
- [ ] A reasonable set of keybindings is configured (e.g., `Ctrl-a` as prefix instead of `Ctrl-b`).
- [ ] The config is placed at a known path inside the VM (e.g., `~/.tmux.conf`) during provisioning.
- [ ] Users MAY override the config by editing it inside the VM; `sd` MUST NOT overwrite user modifications on subsequent connections.

### REQ-007-012: Raw SSH Without tmux

`sd connect` MUST support bypassing tmux for a raw SSH session.

**Acceptance criteria:**
- [ ] `sd connect <vm> --no-tmux` opens a direct SSH session without creating or attaching to any tmux session.
- [ ] All other connect behavior (auto-start, port forwarding, environment injection) still applies.
- [ ] `--no-tmux` and `--new-window` are mutually exclusive; specifying both MUST produce an error with code 2.

### REQ-007-013: Exec Command

The CLI MUST provide `sd exec <vm> -- <command> [args...]` for non-interactive remote command execution.

**Acceptance criteria:**
- [ ] `sd exec myvm -- ls -la /tmp` runs `ls -la /tmp` inside the VM `myvm` and prints the output.
- [ ] The double dash `--` separates `sd` flags from the remote command.
- [ ] stdout from the remote command is written to the local stdout.
- [ ] stderr from the remote command is written to the local stderr.
- [ ] The exit code of `sd exec` matches the exit code of the remote command.
- [ ] If the VM is not running, the command MUST exit with code 1 and print an error (it does NOT auto-start).

### REQ-007-014: Exec JSON Output

`sd exec` MUST support `--json` for structured output.

**Acceptance criteria:**
- [ ] `sd exec myvm --json -- echo hello` outputs a JSON object to stdout.
- [ ] The JSON object contains at minimum: `{"exit_code": 0, "stdout": "hello\n", "stderr": ""}`.
- [ ] When `--json` is active, only the JSON object is written to stdout; no other output is mixed in.
- [ ] stderr from `sd` itself (e.g., connection errors) is still written to stderr, not mixed into the JSON stdout.

### REQ-007-015: Sync To VM

The CLI MUST provide `sd sync to <vm> <host-path> [<guest-path>]` to copy files from the host into the VM.

**Acceptance criteria:**
- [ ] `sd sync to myvm ./src /home/user/project/src` copies `./src` from the host to `/home/user/project/src` in the VM.
- [ ] If `<guest-path>` is omitted, files are copied to the default working directory inside the VM (e.g., `~/<basename of host-path>`).
- [ ] The transfer uses rsync over SSH for incremental, efficient copying.
- [ ] Symlinks, permissions, and timestamps are preserved by default.
- [ ] If the VM is not running, the command MUST exit with code 1.

### REQ-007-016: Sync From VM

The CLI MUST provide `sd sync from <vm> <guest-path> [<host-path>]` to copy files from the VM to the host.

**Acceptance criteria:**
- [ ] `sd sync from myvm /home/user/project/src ./src` copies from the VM to the host.
- [ ] If `<host-path>` is omitted, files are copied to the current working directory on the host (`./<basename of guest-path>`).
- [ ] The transfer uses rsync over SSH.
- [ ] If the VM is not running, the command MUST exit with code 1.

### REQ-007-017: Sync Diff Preview

`sd sync from` MUST support a `--diff` flag to preview changes before transferring.

**Acceptance criteria:**
- [ ] `sd sync from myvm /path --diff` displays a diff of files that differ between the VM and the host, without copying anything.
- [ ] The diff output uses a unified diff format.
- [ ] Exit code is 0 if there are no differences, 1 if there are differences (and no errors).

### REQ-007-018: Continuous Sync with Watch

`sd sync` SHOULD support a `--watch` flag for continuous synchronization.

**Acceptance criteria:**
- [ ] `sd sync to myvm ./src /home/user/src --watch` monitors `./src` for changes and syncs incrementally on each change.
- [ ] File system monitoring uses fsnotify (or equivalent) on the host.
- [ ] The watch process runs in the foreground, logging each sync event to stderr.
- [ ] `Ctrl-C` cleanly stops the watch process.
- [ ] The `--watch` flag is only valid for `sd sync to` (host-to-VM direction). Using it with `sd sync from` MUST produce an error with code 2.

### REQ-007-019: Environment Injection on Connect

On each `sd connect` or `sd exec`, `sd` MUST inject configured environment variables into the remote session using the SSH `SendEnv`/`AcceptEnv` mechanism.

**Acceptance criteria:**
- [ ] Environment variables configured for the VM (via `sd` configuration) are set in the remote shell session.
- [ ] Variable values containing `${VAR}` references are resolved from the host environment at connection time.
- [ ] Unresolvable `${VAR}` references MUST produce a warning on stderr naming the unresolved variable, and the variable MUST be set to an empty string.
- [ ] Variables are injected via SSH `SendEnv` on the client side. The VM's sshd MUST be configured (during provisioning) with `AcceptEnv SD_* ANTHROPIC_* GITHUB_* GH_*` to accept these variables.
- [ ] Provisioning MUST configure `/etc/ssh/sshd_config` (or a drop-in file in `/etc/ssh/sshd_config.d/`) with the `AcceptEnv` directive and reload sshd.
- [ ] Injected variables are available in both `sd connect` (interactive) and `sd exec` (non-interactive) sessions.
- [ ] Variables MUST NOT be written to disk inside the VM; they exist only in the SSH session environment.

### REQ-007-020: Multiple Concurrent Connections

`sd` MUST support multiple terminals connected to the same VM simultaneously.

**Acceptance criteria:**
- [ ] Two or more `sd connect <vm>` invocations from different terminal windows succeed concurrently.
- [ ] Each connection attaches to a separate tmux window within the same tmux session, unless `--session` specifies different sessions.
- [ ] Concurrent `sd exec` invocations do not interfere with each other or with active `sd connect` sessions.

### REQ-007-021: Connection Health and Error Reporting

`sd` MUST detect and report SSH connection failures with actionable error messages.

**Acceptance criteria:**
- [ ] If SSH connection is refused, the error message includes the host, port, and suggests checking if the VM is running.
- [ ] If SSH authentication fails, the error message names the key path and suggests checking `~/.sd/vms/<name>/ssh/` or recreating the VM with `sd destroy <vm> && sd create <vm>`.
- [ ] If the connection times out, the error message includes the timeout duration and suggests checking VM status with `sd status <vm>`.
- [ ] If tmux is not installed inside the VM, the error message instructs the user to provision tmux or use `--no-tmux`.
- [ ] All connection errors include the VM name and are formatted for `--json` output when that flag is active.
- [ ] Connection errors MUST exit with code 1.

## Design

### CLI Surface

```
sd connect <vm-name>           # Connect with tmux (alias: sd c <vm-name>)
sd connect <vm> --session work # Named tmux session
sd connect <vm> --new-window   # New window in existing session
sd connect <vm> --no-tmux      # Raw SSH, no tmux
sd connect <vm> --forward 8080:8080  # Port forwarding
sd connect <vm> --no-start     # Do not auto-start stopped VM

sd exec <vm> -- <command>      # Run command non-interactively
sd exec <vm> --json -- <cmd>   # Structured output

sd ssh-config <vm>             # Print SSH config fragment
sd ssh-config <vm> --json      # SSH config as JSON

sd sync to <vm> <src> [<dst>]         # Host to VM
sd sync from <vm> <src> [<dst>]       # VM to host
sd sync from <vm> <src> --diff        # Preview differences
sd sync to <vm> <src> <dst> --watch   # Continuous sync
```

### Interfaces

```go
// Connector manages connections to VMs.
type Connector interface {
    // Connect establishes an interactive session with the VM.
    // It handles auto-start, SSH, tmux attachment, and environment injection.
    Connect(ctx context.Context, opts ConnectOpts) error

    // Exec runs a command in the VM and returns the result.
    // The ExecResult type reuses or wraps backend.ExecResult from the
    // backend package (see 003-vm-backend.md).
    Exec(ctx context.Context, vmName string, command []string, opts ExecOpts) (*ExecResult, error)

    // SSHConfig returns the SSH config fragment for the named VM.
    SSHConfig(vmName string) (*SSHConfigData, error)
}

// Syncer manages file synchronization between host and VM.
type Syncer interface {
    // SyncTo copies files from the host to the VM.
    SyncTo(ctx context.Context, opts SyncOpts) error

    // SyncFrom copies files from the VM to the host.
    SyncFrom(ctx context.Context, opts SyncOpts) error

    // Diff shows differences between host and VM files without copying.
    Diff(ctx context.Context, opts SyncOpts) (*DiffResult, error)

    // Watch monitors the host path and syncs changes to the VM continuously.
    Watch(ctx context.Context, opts SyncOpts) error
}
```

### Types

```go
// ConnectOpts configures a connection to a VM.
type ConnectOpts struct {
    VMName      string            // Required. Name of the VM to connect to.
    Session     string            // tmux session name. Default: "sd-<VMName>".
    NewWindow   bool              // Create a new window in the tmux session.
    NoTmux      bool              // Skip tmux, raw SSH session.
    Forwards    []PortForward     // Port forwarding specifications.
    NoStart     bool              // Do not auto-start a stopped VM.
    EnvVars     map[string]string // Environment variables to inject via SendEnv.
}

// PortForward specifies a host-to-guest port mapping.
type PortForward struct {
    BindAddr  string // Host bind address. Default: "127.0.0.1".
    HostPort  int    // Port on the host.
    GuestPort int    // Port inside the VM.
}

// ExecOpts configures a non-interactive command execution.
type ExecOpts struct {
    JSON    bool              // Return structured JSON output.
    EnvVars map[string]string // Environment variables to inject via SendEnv.
}

// ExecResult holds the result of a non-interactive command execution.
// This type wraps or reuses backend.ExecResult from the backend package
// (see 003-vm-backend.md) to maintain consistency across the codebase.
type ExecResult struct {
    ExitCode int    `json:"exit_code"`
    Stdout   string `json:"stdout"`
    Stderr   string `json:"stderr"`
}

// SSHConfigData holds parsed SSH configuration for a VM.
type SSHConfigData struct {
    Host             string `json:"host"`
    HostName         string `json:"hostname"`
    Port             int    `json:"port"`
    User             string `json:"user"`
    IdentityFile     string `json:"identity_file"`
    ProxyCommand     string `json:"proxy_command,omitempty"`
    Transport        string `json:"transport"` // "tcp" or "vsock"
}

// SyncOpts configures a file sync operation.
type SyncOpts struct {
    VMName    string // Required. Name of the VM.
    SrcPath   string // Source path (host path for SyncTo, guest path for SyncFrom).
    DstPath   string // Destination path (guest path for SyncTo, host path for SyncFrom).
    Watch     bool   // Enable continuous sync (SyncTo only).
}

// DiffResult holds the output of a diff preview.
type DiffResult struct {
    HasDifferences bool   `json:"has_differences"`
    Diff           string `json:"diff"`
}
```

### SSH Key Storage Layout

```
~/.sd/
  vms/
    <vm-name>/
      ssh/
        id_ed25519        # Private key (0600)
        id_ed25519.pub    # Public key
        known_hosts       # Known hosts for TCP connections
```

### SSH Config Fragment Layout

```
~/.ssh/
  config.d/
    sd-<vm-name>          # Per-VM SSH config fragment
```

Example fragment for TCP connection (`~/.ssh/config.d/sd-myvm`):

```ssh-config
# Managed by sd. Do not edit manually.
Host sd-myvm
    HostName 127.0.0.1
    Port 60022
    User dev
    IdentityFile ~/.sd/vms/myvm/ssh/id_ed25519
    StrictHostKeyChecking yes
    UserKnownHostsFile ~/.sd/vms/myvm/ssh/known_hosts
    ForwardAgent no
    ForwardX11 no
    LogLevel ERROR
    SendEnv SD_* ANTHROPIC_* GITHUB_* GH_*
```

Example fragment with VSOCK (Lima+VZ backend):

```ssh-config
# Managed by sd. Do not edit manually.
Host sd-myvm
    User dev
    IdentityFile ~/.sd/vms/myvm/ssh/id_ed25519
    ProxyCommand limactl ssh --stdio myvm
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
    ForwardAgent no
    ForwardX11 no
    LogLevel ERROR
    SendEnv SD_* ANTHROPIC_* GITHUB_* GH_*
```

### tmux Default Configuration

Provisioned at `~/.tmux.conf` inside the VM:

```tmux
# Managed by sd. User modifications are preserved.
set -g prefix C-a
unbind C-b
bind C-a send-prefix

set -g mouse on
set -g history-limit 50000
set -g default-terminal "tmux-256color"

# Status bar
set -g status-left "[#S] "
set -g status-right " %H:%M "
set -g status-style "bg=colour235,fg=colour248"

# Better split keybindings
bind | split-window -h -c "#{pane_current_path}"
bind - split-window -v -c "#{pane_current_path}"

# Reload config
bind r source-file ~/.tmux.conf \; display "Config reloaded"
```

### Connection Flow

1. User runs `sd connect myvm`.
2. `sd` resolves VM name and looks up its state.
3. If VM is stopped: auto-start the VM (unless `--no-start`), wait for SSH readiness.
4. `sd` resolves environment variables (expanding `${VAR}` from host env).
5. `sd` builds the SSH command using the managed config (key, host, port, transport) with `SendEnv` directives for environment injection.
6. If `--forward` flags are present, add `-L` arguments to the SSH command.
7. If `--no-tmux`: SSH directly into the VM with env vars.
8. Otherwise: SSH into the VM and run `tmux new-session -A -s <session-name>` (attaches if exists, creates if not).
9. If `--new-window`: use `tmux new-window -t <session-name>` then attach.
10. On disconnect: SSH exits, tmux session persists inside the VM.

### Exec Flow

1. User runs `sd exec myvm -- claude --dangerously-skip-permissions -p "fix the bug"`.
2. `sd` verifies the VM is running (no auto-start for exec).
3. `sd` resolves environment variables.
4. `sd` runs the command via SSH with `SendEnv` for environment injection. The VM's sshd accepts the variables via `AcceptEnv SD_* ANTHROPIC_* GITHUB_* GH_*`.
5. stdout and stderr are passed through.
6. `sd exec` exits with the remote command's exit code.
7. If `--json`: capture stdout/stderr and wrap in JSON.

### Sync Flow

1. User runs `sd sync to myvm ./src /home/dev/project/src`.
2. `sd` verifies the VM is running.
3. `sd` runs `rsync -avz -e "ssh -F /dev/null -i ~/.sd/vms/myvm/ssh/id_ed25519 -p <port>" ./src dev@<host>:/home/dev/project/src`.
4. rsync output is displayed to the user.
5. If `--watch`: after initial sync, start fsnotify watcher on `./src` and re-run rsync on changes.

## Error Handling

| Error Condition | Category | User Message | Exit Code |
|---|---|---|---|
| VM not found | Fatal | `Error: VM "foo" not found. Run "sd list" to see available VMs.` | 1 |
| VM not running (exec/sync) | Fatal | `Error: VM "foo" is not running. Start it with "sd start foo".` | 1 |
| VM failed to start | Fatal | `Error: Failed to start VM "foo": <reason>. Check "sd status foo" for details.` | 1 |
| SSH connection refused | Fatal | `Error: Connection refused to VM "foo" (127.0.0.1:60022). Is the VM running? Try "sd status foo".` | 1 |
| SSH auth failure | Fatal | `Error: Authentication failed for VM "foo" using key ~/.sd/vms/foo/ssh/id_ed25519. Check SSH key at ~/.sd/vms/foo/ssh/ or try "sd destroy foo && sd create foo" to recreate.` | 1 |
| SSH timeout | Fatal | `Error: Connection to VM "foo" timed out after 30s. Check VM status with "sd status foo".` | 1 |
| tmux not found in VM | Fatal | `Error: tmux is not installed in VM "foo". Provision it with "sd provision foo" or connect with --no-tmux.` | 1 |
| Port already in use | Fatal | `Error: Port 8080 is already in use on the host. Choose a different host port.` | 1 |
| VM stopped with --no-start | Fatal | `Error: VM "foo" is stopped. Start it with "sd start foo" or connect without --no-start.` | 1 |
| Unresolvable env var | Warning | `Warning: Environment variable ${SECRET_KEY} could not be resolved from host; set to empty string.` | N/A |
| Invalid session name | Usage | `Error: Session name "my session!" is invalid. Use only alphanumeric characters, hyphens, and underscores.` | 2 |
| --no-tmux with --new-window | Usage | `Error: --no-tmux and --new-window are mutually exclusive.` | 2 |
| --watch with sync from | Usage | `Error: --watch is only supported for "sd sync to" (host-to-VM direction).` | 2 |
| rsync not found on host | Fatal | `Error: rsync is not installed on the host. Install it with "brew install rsync".` | 1 |

All errors MUST be JSON-serializable when `--json` is active. JSON error format:

```json
{
  "error": {
    "code": "vm_not_found",
    "message": "VM \"foo\" not found. Run \"sd list\" to see available VMs.",
    "vm": "foo"
  }
}
```

## Security Considerations

### Trust boundaries crossed

- **Host to VM SSH**: Every connection crosses the host-VM trust boundary. SSH keys authenticate the connection. The private key on the host grants access to the VM; compromise of `~/.sd/vms/<name>/ssh/` gives an attacker VM access.
- **Environment injection**: Host environment variables (potentially containing secrets) are transmitted over SSH into the VM via the `SendEnv`/`AcceptEnv` mechanism. The SSH channel is encrypted, but the variables exist in-memory in the guest shell process and may appear in `/proc/<pid>/environ`.

### Credentials and secrets handled

- SSH private keys stored at `~/.sd/vms/<name>/ssh/id_ed25519`.
- Environment variables that may contain API tokens, PATs, or other secrets.

### Blast radius if compromised

- If an SSH key is leaked: the attacker gains access to the specific VM only (keys are per-VM, not shared).
- If environment variables are captured inside the VM: secrets injected at connection time are exposed. This is mitigated by using scoped, short-lived tokens and by not persisting variables to disk.

### Mitigations

- Per-VM SSH keys limit lateral movement between VMs.
- SSH keys use Ed25519 (modern, no known weaknesses).
- For TCP connections, `StrictHostKeyChecking yes` with a per-VM `known_hosts` file ensures host key verification. For VSOCK connections, `StrictHostKeyChecking no` is acceptable because VSOCK does not traverse a network, making MITM impossible.
- `ForwardAgent no` prevents SSH agent forwarding, limiting credential exposure.
- `ForwardX11 no` prevents X11 forwarding, reducing attack surface.
- Environment variables are injected at runtime via `SendEnv`/`AcceptEnv`, never written to files inside the VM.
- SSH config fragments include `LogLevel ERROR` to avoid leaking connection metadata to the terminal.

## Testing Strategy

### Unit Tests

| Requirement | Test |
|---|---|
| REQ-007-001 | Test command parsing: valid VM name, missing VM name, alias `c`. |
| REQ-007-002 | Test auto-start logic: VM stopped (auto-starts), VM stopped with `--no-start` (error), VM already running. |
| REQ-007-003 | Test key generation: key files created with correct permissions, unique per VM. |
| REQ-007-004 | Test SSH config fragment generation: correct content for TCP (StrictHostKeyChecking yes) and VSOCK (StrictHostKeyChecking no), correct file path, cleanup on destroy, ForwardAgent no, ForwardX11 no. |
| REQ-007-005 | Test VSOCK detection and fallback logic; verify ProxyCommand uses `limactl ssh --stdio`. |
| REQ-007-007 | Test port forward parsing: single, multiple, with bind address, invalid format. |
| REQ-007-009 | Test session name validation: valid names, invalid characters. |
| REQ-007-012 | Test mutual exclusion of `--no-tmux` and `--new-window`. |
| REQ-007-014 | Test JSON output structure for exec results. |
| REQ-007-019 | Test env var resolution and SendEnv construction: valid `${VAR}`, unresolvable `${VAR}`, nested references. |
| REQ-007-021 | Test error message formatting for each connection failure type (no `sd repair` references). |

### Integration Tests

| Requirement | Test |
|---|---|
| REQ-007-001, 008 | Start a real VM, run `sd connect`, verify tmux session is created, detach, verify session persists. |
| REQ-007-002 | Stop a VM, run `sd connect`, verify VM auto-starts and connection succeeds. |
| REQ-007-003, 004 | Create a VM, verify key files exist with correct permissions, verify SSH config fragment is valid. |
| REQ-007-006 | Run `sd ssh-config <vm>`, verify output can be used by `ssh` directly. |
| REQ-007-013 | Run `sd exec <vm> -- echo hello`, verify stdout is `hello`. |
| REQ-007-014 | Run `sd exec <vm> --json -- echo hello`, verify JSON structure. |
| REQ-007-015, 016 | Create a file on host, `sd sync to`, verify file exists in VM. Modify in VM, `sd sync from`, verify on host. |
| REQ-007-017 | Modify a file in VM, run `sd sync from --diff`, verify diff output without file transfer. |
| REQ-007-019 | Configure env vars with `${HOST_VAR}`, set `HOST_VAR` on host, `sd exec` and verify the variable is set inside the VM via SendEnv/AcceptEnv. |
| REQ-007-020 | Open two `sd connect` sessions to the same VM, verify both attach successfully. |

### Script Tests

| Requirement | Test |
|---|---|
| REQ-007-001 | `sd connect nonexistent` exits 1 with error mentioning `sd list`. |
| REQ-007-013 | `sd exec myvm -- false` exits with code 1. `sd exec myvm -- true` exits with code 0. |
| REQ-007-018 | `sd sync from myvm /path --watch` exits with code 2 and an error about `--watch` direction. |

## Dependencies

### Depends On

- [003-vm-backend.md](003-vm-backend.md) -- for VM state queries (running/stopped), start/stop operations, VM creation (key injection), and the `ExecResult` type.
- [006-provisioning.md](006-provisioning.md) -- for tmux installation, sshd_config setup (`AcceptEnv`), and config deployment inside VMs.
- [005-configuration.md](005-configuration.md) -- for reading per-VM environment variable settings.
- [002-cli.md](002-cli.md) -- for command registration, `--json` output formatting, alias handling.

### Depended On By

- Agent orchestration spec (TBD) -- for running AI agents inside VMs via `sd exec`.
- [004-security.md](004-security.md) -- for understanding how credentials are injected at connection time.

## Open Questions

- OQ1: Should `sd connect` support reverse port forwarding (`-R` style) in addition to local forwarding? Deferred until a concrete use case arises.
- OQ2: Should `sd sync` support exclusion patterns (e.g., `--exclude .git`)? Likely yes via rsync flags, but the interface needs design.
- OQ3: Should `sd exec` support `--timeout` to kill long-running commands? Useful for automation, but adds complexity.

## Revision History

| Date       | Author | Change Description |
|------------|--------|--------------------|
| 2026-03-27 | claude | Initial draft      |
| 2026-03-27 | claude | Review fixes: auto-start without prompting (remove --yes, add --no-start); add ForwardAgent no and ForwardX11 no to SSH fragments; TCP uses StrictHostKeyChecking yes with per-VM known_hosts, VSOCK uses StrictHostKeyChecking no; fix ProxyCommand to use limactl ssh --stdio; use SendEnv/AcceptEnv for env injection with sshd_config provisioning note; standardize error codes to snake_case; remove sd repair references; fix dependency section to use spec filenames; note ExecResult relationship to backend package |
