# 004: Security Model

| Field        | Value      |
|--------------|------------|
| Status       | draft      |
| Created      | 2026-03-27 |
| Last Updated | 2026-03-27 |
| Authors      | claude     |
| Reviewers    |            |

## Overview

This spec defines the security model for `sd`. When running Claude Code with `--dangerously-skip-permissions`, the agent has unrestricted access to the operating environment: it can read and write arbitrary files, execute arbitrary commands, and make arbitrary network requests. The `sd` tool MUST enforce a layered defense model so that even a fully compromised agent session cannot exfiltrate host secrets, tamper with host files, or gain broad access to external services. This spec covers the threat model, mount restrictions, egress control, credential management, GitHub protections, snapshot safety, and audit logging.

## Goals

- G1: Define the threat model for running AI coding agents with bypass permissions
- G2: Specify layered defenses that constrain blast radius to the VM boundary
- G3: Specify mount restrictions that prevent host filesystem exposure
- G4: Specify egress controls that limit outbound network access to an allowlist
- G5: Specify credential management that enforces scoped, runtime-injected, non-persisted secrets
- G6: Specify GitHub-specific protections for token scoping and repository safety
- G7: Specify snapshot and rollback requirements for destructive operation safety
- G8: Specify audit logging for all `sd` operations and VM lifecycle events

## Non-Goals

- NG1: Protecting against attacks that originate from the host toward the VM
- NG2: Hardening the VM guest OS beyond what is needed for agent containment (e.g., kernel-level exploit mitigation)
- NG3: Implementing a full intrusion detection system inside the VM
- NG4: Managing secrets for non-GitHub services (cloud providers, databases, etc.) — these may be addressed in future specs
- NG5: Enforcing code review policies on GitHub — `sd` recommends branch protections but does not implement them

## Requirements

### REQ-004-001: Threat Model Documentation

The `sd` project MUST treat any agent running with `--dangerously-skip-permissions` as an untrusted process. The assumed threat model is that the agent can:

1. Read and write any file accessible to the user running the agent
2. Implant persistent hooks (git hooks, shell rc files, CI workflow configurations) that execute later outside the agent session
3. Exfiltrate secrets over the network via direct connections or through legitimate APIs
4. Access any mounted host directories and their full contents
5. Execute arbitrary commands with the privileges of the VM user

All security controls in this spec MUST be designed to mitigate these threats.

**Acceptance criteria:**
- [ ] The threat model is documented and referenced in CLI help output for security-related commands
- [ ] `sd create --help` includes a summary of the security model applied by default

### REQ-004-002: Layer 1 — VM Isolation

The agent MUST run inside a VM, never directly on the host. The VM provides the primary isolation boundary.

**Acceptance criteria:**
- [ ] `sd` does not provide any mode that runs an agent directly on the host
- [ ] All agent sessions are established via SSH into a VM guest
- [ ] The VM guest runs a separate kernel and has its own filesystem, process space, and network stack

### REQ-004-003: Layer 2 — Mount Restrictions (Default)

By default, `sd create` MUST create VMs with NO host directory mounts. Repos SHOULD be cloned inside the VM using credentials injected at runtime.

**Acceptance criteria:**
- [ ] A VM created with `sd create <name>` has zero host mounts by default
- [ ] `sd create <name> --json` output includes a `mounts` field that is an empty array by default

### REQ-004-004: Layer 2 — Mount Restrictions (Optional Mounts)

When a user explicitly requests a mount via `sd create --mount <host-path>:<guest-path>`, the mount MUST be read-only by default. A writable mount MUST require the explicit flag `--mount <host-path>:<guest-path>:rw`.

**Acceptance criteria:**
- [ ] `sd create myvm --mount /path/to/project:/project` creates a read-only mount
- [ ] `sd create myvm --mount /path/to/project:/project:rw` creates a read-write mount
- [ ] The mount mode (ro/rw) is visible in `sd list --json` and `sd status <name> --json` output

### REQ-004-005: Layer 2 — Mount Path Validation

`sd create` MUST reject mount paths that include sensitive host directories. The following paths (and any path that is a parent of or resolves to these) MUST be rejected:

- `$HOME` (the entire home directory)
- `~/.ssh`
- `~/.aws`
- `~/.config`
- `~/.gnupg`
- `~/.kube`
- `~/.docker`
- Any path containing `.env` files at the mount root
- Browser profile directories (`~/Library/Application Support/Google/Chrome`, `~/Library/Application Support/Firefox`, `~/.mozilla`, `~/.config/google-chrome`, `~/.config/chromium`)
- `/var/run/docker.sock` (host Docker socket)

Validation MUST resolve symlinks before checking.

**Acceptance criteria:**
- [ ] `sd create myvm --mount ~:/home/user` exits with error code 1 and a message identifying the rejected path
- [ ] `sd create myvm --mount ~/.ssh:/keys` exits with error code 1
- [ ] `sd create myvm --mount /var/run/docker.sock:/var/run/docker.sock` exits with error code 1
- [ ] A symlink pointing to `$HOME` is resolved and rejected
- [ ] Error messages in both text and JSON format clearly state which path was rejected and why
- [ ] `sd create myvm --mount ~/projects/my-repo:/project` succeeds (subdirectory of home that is not a sensitive path)

### REQ-004-006: Layer 3 — Default-Deny Egress

VMs created by `sd` MUST have a default-deny outbound network policy. Only traffic to explicitly allowlisted destinations MUST be permitted.

**Acceptance criteria:**
- [ ] A newly created VM cannot make outbound connections to arbitrary hosts
- [ ] `curl http://example.com` from inside the VM fails by default
- [ ] Connections to allowlisted destinations succeed

### REQ-004-007: Layer 3 — Default Egress Allowlist

The following destinations MUST be allowed by default in the egress allowlist:

| Destination                     | Purpose                      |
|---------------------------------|------------------------------|
| `api.anthropic.com`             | Claude API                   |
| `github.com`                    | Git operations               |
| `*.githubusercontent.com`       | GitHub raw content / releases|
| `archive.ubuntu.com`            | Ubuntu package mirror        |
| `security.ubuntu.com`           | Ubuntu security updates      |
| `deb.debian.org`                | Debian package mirror        |
| `registry.npmjs.org`            | npm packages                 |
| `pypi.org`                      | Python packages              |
| `files.pythonhosted.org`        | Python package downloads     |
| `proxy.golang.org`              | Go module proxy              |
| `sum.golang.org`                | Go checksum database         |

Wildcard patterns in the egress allowlist match one subdomain level only. For example, `*.example.com` matches `foo.example.com` but NOT `bar.foo.example.com`. Multi-level subdomain matching requires explicit entries (e.g., `*.*.example.com` or listing each subdomain).

**Acceptance criteria:**
- [ ] All destinations in the table above are reachable from a default VM
- [ ] The default allowlist is defined in a single location in the codebase (embedded or config file)
- [ ] `sd status <name> --json` output includes the effective egress allowlist
- [ ] `*.githubusercontent.com` matches `raw.githubusercontent.com` but not `a.b.githubusercontent.com`

### REQ-004-008: Layer 3 — User-Configurable Egress Allowlist

Users MUST be able to extend the egress allowlist via:

1. CLI flag at creation time: `sd create <name> --allow-egress <domain>`
2. Configuration file: an `egress_allowlist` field in the `sd` config file
3. Post-creation modification: `sd config egress add <name> <domain>` and `sd config egress remove <name> <domain>`

User additions are merged with (not replacing) the default allowlist.

**Acceptance criteria:**
- [ ] `sd create myvm --allow-egress custom.api.example.com` adds the domain to the allowlist
- [ ] Multiple `--allow-egress` flags are supported in a single command
- [ ] Adding an egress rule post-creation updates the VM's firewall rules without requiring a restart
- [ ] `sd config egress list <name>` shows all allowed destinations, distinguishing default from user-added
- [ ] `sd config egress list <name> --json` outputs the allowlist as a JSON array with a `source` field per entry (`default` or `user`)

### REQ-004-009: Layer 3 — Egress Implementation via iptables

Egress control MUST be implemented using iptables rules provisioned inside the VM during `sd create` or `sd start`. DNS traffic MUST be routed exclusively through the local filtering resolver (see REQ-004-025).

**Acceptance criteria:**
- [ ] iptables rules are installed during VM provisioning
- [ ] The default policy for the OUTPUT chain (or a dedicated chain) is DROP for new outbound connections
- [ ] Allowlisted domains are resolved to IP addresses and added as ACCEPT rules
- [ ] DNS traffic (UDP/TCP port 53) is allowed ONLY to 127.0.0.1 (the local filtering resolver); all other DNS destinations are blocked
- [ ] DNS-over-TLS (port 853) to any destination is blocked unless the destination is on the egress allowlist
- [ ] DNS-over-HTTPS to known DoH providers (e.g., 8.8.8.8, 1.1.1.1, 9.9.9.9) on port 443 is blocked unless the provider is on the egress allowlist
- [ ] SSH traffic from the host to the VM is always allowed (required for `sd` operation)

### REQ-004-010: Layer 3 — DNS-Based Allowlisting

For domains that resolve to multiple or changing IP addresses, `sd` MUST support DNS-based allowlisting. A lightweight daemon or cron job inside the VM MUST periodically re-resolve allowlisted domains and update iptables rules.

**Acceptance criteria:**
- [ ] Domains in the allowlist are re-resolved at a configurable interval (default: 5 minutes)
- [ ] When a domain's resolved IPs change, iptables rules are updated without dropping existing connections
- [ ] The resolution mechanism logs changes to resolved IPs

### REQ-004-011: Layer 4 — Credential Injection via Environment Variables

Credentials MUST be injected into the VM as environment variables at session start via SSH's `SendEnv`/`AcceptEnv` mechanism. Credentials MUST NOT be written to files on the VM's filesystem, and MUST NOT be passed via command-line arguments (e.g., `env VAR=val command`) to avoid exposure in the process list.

**Acceptance criteria:**
- [ ] `sd connect <name>` injects configured credentials as environment variables in the SSH session
- [ ] No credential values appear in any file on the VM filesystem (including shell history, rc files, or config files)
- [ ] `sd create` and `sd connect` do not write credentials to the Lima/backend YAML configuration on disk in plaintext
- [ ] Environment variables are set via SSH `SendEnv`/`AcceptEnv` mechanism, not via writing to `.bashrc`, command-line `env VAR=val`, or similar
- [ ] The VM's sshd MUST be configured with `AcceptEnv SD_* ANTHROPIC_* GITHUB_* GH_*`
- [ ] The SSH client config fragment MUST include `SendEnv SD_* ANTHROPIC_* GITHUB_* GH_*`
- [ ] Credential values MUST NOT appear in `/proc/*/cmdline` output for any process in the VM

### REQ-004-012: Layer 4 — GitHub Token Scoping

`sd` MUST recommend and facilitate the use of fine-grained GitHub Personal Access Tokens scoped to specific repositories.

**Acceptance criteria:**
- [ ] `sd token github setup` provides instructions for creating a fine-grained PAT with minimal scopes
- [ ] The recommended scopes are documented: `contents:write` and `pull_requests:write` for the target repo(s), and nothing else unless explicitly needed
- [ ] `sd token github setup --json` outputs the recommended scopes as structured data
- [ ] `sd` warns if a classic (non-fine-grained) PAT is detected (by token prefix `ghp_`)

### REQ-004-013: Layer 4 — Anthropic API Key Injection

The Anthropic API key MUST be injected via the `ANTHROPIC_API_KEY` environment variable at session start. It MUST NOT be persisted in any file inside the VM.

**Acceptance criteria:**
- [ ] `sd connect <name>` sets `ANTHROPIC_API_KEY` in the session environment
- [ ] The key is sourced from the host's environment, keychain, or `sd` config — not from a file inside the VM
- [ ] After `sd connect` establishes a session, `grep -r` for the API key value across the VM filesystem returns no results (excluding `/proc` and `/dev`)

### REQ-004-014: Layer 4 — Per-VM SSH Keys

Each VM MUST have its own SSH key pair generated at creation time. Host SSH keys MUST NOT be copied into or mounted inside the VM.

**Acceptance criteria:**
- [ ] `sd create <name>` generates a new SSH key pair for the VM
- [ ] The generated key is used exclusively for that VM and is not shared with other VMs
- [ ] `~/.ssh/` from the host is never mounted or copied into the VM
- [ ] `sd destroy <name>` removes the VM-specific SSH key pair from the host

### REQ-004-015: Layer 4 — Token Rotation and Revocation

`sd` MUST provide CLI commands to rotate and revoke credentials associated with a VM.

**Acceptance criteria:**
- [ ] `sd token rotate <name>` prompts for or accepts new token values and updates the stored configuration
- [ ] `sd token revoke <name>` removes all stored credentials for the named VM from `sd` config
- [ ] After `sd token revoke <name>`, `sd connect <name>` does not inject any credentials (the session starts without `GITHUB_TOKEN` or `ANTHROPIC_API_KEY`)
- [ ] `sd token list <name> --json` shows which credential types are configured (without revealing values)

### REQ-004-016: GitHub Bot/Service Account Support

`sd` SHOULD support configuration of a separate GitHub bot or service account identity for VM operations.

**Acceptance criteria:**
- [ ] `sd config set github.bot_account <username>` stores the bot account name
- [ ] `sd token github setup` can configure tokens for the bot account
- [ ] Git operations inside the VM use the bot account's identity (via `GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME`, `GIT_COMMITTER_EMAIL` environment variables)

### REQ-004-017: GitHub Protected Branch Integration

`sd` SHOULD document and facilitate integration with GitHub protected branch rules so that an agent can push to feature branches but cannot merge directly to protected branches.

**Acceptance criteria:**
- [ ] `sd token github setup` output recommends enabling branch protection on `main`/`master`
- [ ] Documentation covers the recommended GitHub branch protection settings for use with `sd`
- [ ] Fine-grained PAT scopes recommended by `sd` do not include repository administration permissions

### REQ-004-018: CI Workflow Change Detection

`sd` SHOULD provide a mechanism to detect and alert on changes to CI/CD workflow files made during an agent session.

**Acceptance criteria:**
- [ ] `sd diff <name>` (or equivalent command) highlights changes to files matching `.github/workflows/*`, `.gitlab-ci.yml`, `Jenkinsfile`, `.circleci/*`, and `.git/hooks/*`
- [ ] Changed CI/CD files are flagged with a warning in both text and JSON output
- [ ] The detection covers both tracked and untracked files in the repository

### REQ-004-019: Snapshot Before Destructive Operations

`sd` MUST automatically create a VM snapshot before any destructive operation. Destructive operations include: `sd destroy`, `sd reset`, and any operation that modifies VM configuration.

**Acceptance criteria:**
- [ ] `sd destroy <name>` creates a snapshot named with a timestamp before destroying the VM
- [ ] `sd reset <name>` creates a snapshot before resetting the VM to a prior state
- [ ] Snapshots are stored in a recoverable location and can be listed with `sd snapshot list`
- [ ] `sd snapshot restore <name> <snapshot-id>` restores a VM from a snapshot
- [ ] Auto-snapshots can be disabled with `--no-snapshot` for automation use cases

### REQ-004-020: Manual Snapshots

Users MUST be able to create manual snapshots at any time.

**Acceptance criteria:**
- [ ] `sd snapshot create <name>` creates a snapshot of the named VM
- [ ] `sd snapshot create <name> --label <label>` attaches a human-readable label
- [ ] `sd snapshot list <name> --json` returns snapshots with id, timestamp, label, and size

### REQ-004-021: Audit Logging — Command Logging

`sd` MUST log every command it executes, including the full command line, timestamp, and outcome.

**Acceptance criteria:**
- [ ] Every invocation of `sd` is logged to a log file (default: `$SD_HOME/audit.log`)
- [ ] Each log entry includes: timestamp (ISO 8601), command, arguments (with secrets redacted), exit code, and duration
- [ ] Credentials and API key values are never written to the audit log — they are replaced with `[REDACTED]`
- [ ] The log file location is configurable via `sd config set audit.log_path <path>`

### REQ-004-022: Audit Logging — VM Lifecycle Events

`sd` MUST log all VM lifecycle events. Audit log entries SHOULD include a hash of the previous entry (hash chain) to make tampering detectable. Each entry MUST include a `prev_hash` field containing the SHA-256 hash of the preceding log line. The first entry in the log MUST use a well-known genesis value (`0000000000000000000000000000000000000000000000000000000000000000`).

**Acceptance criteria:**
- [ ] The following events are logged: create, start, stop, destroy, connect, disconnect, snapshot-create, snapshot-restore, token-rotate, token-revoke, config-change
- [ ] Each event log entry includes: timestamp, event type, VM name, and relevant metadata (e.g., snapshot ID for snapshot events)
- [ ] `sd audit <name>` displays the audit log filtered to a specific VM
- [ ] `sd audit <name> --json` outputs the filtered log as a JSON array
- [ ] `sd audit --since <timestamp>` filters events by time
- [ ] Each log entry includes a `prev_hash` field with the SHA-256 hash of the previous log line
- [ ] `sd audit --verify` validates the hash chain and reports any broken links

### REQ-004-023: Sensitive Directory List Configurability

The list of sensitive directories rejected by mount validation (REQ-004-005) SHOULD be user-extensible via configuration.

**Acceptance criteria:**
- [ ] `sd config set security.sensitive_paths` accepts additional paths to reject
- [ ] User-configured sensitive paths are merged with the built-in list
- [ ] `sd config get security.sensitive_paths --json` shows the complete list with source (`builtin` or `user`)

### REQ-004-024: Security Posture Summary

`sd` MUST provide a command that summarizes the security posture of a VM.

**Acceptance criteria:**
- [ ] `sd security status <name>` displays: mount status, egress rules (count and list), credential types configured, snapshot count, and last audit event
- [ ] `sd security status <name> --json` outputs the above as structured JSON
- [ ] Any deviations from the recommended security posture are flagged as warnings (e.g., writable mounts, classic PATs)

### REQ-004-025: Layer 3 — Local Filtering DNS Resolver

The VM MUST run a local filtering DNS resolver (e.g., dnsmasq) that only resolves domains present on the egress allowlist. This prevents data exfiltration via DNS queries to arbitrary domains.

**Acceptance criteria:**
- [ ] A local DNS resolver (e.g., dnsmasq) is installed and running inside the VM, bound to 127.0.0.1:53
- [ ] The resolver is configured to forward queries ONLY for domains on the egress allowlist to an upstream DNS server
- [ ] DNS queries for domains NOT on the allowlist MUST return NXDOMAIN
- [ ] The VM's `/etc/resolv.conf` MUST point to `127.0.0.1` as the sole nameserver
- [ ] Wildcard entries in the allowlist (e.g., `*.githubusercontent.com`) are correctly handled by the resolver
- [ ] The resolver configuration is regenerated whenever the egress allowlist is modified (via `sd config egress add/remove`)
- [ ] DNS-over-HTTPS (DoH) to known providers (8.8.8.8, 1.1.1.1, 9.9.9.9, and their IPv6 equivalents) on port 443 is blocked by iptables unless the provider is on the egress allowlist
- [ ] DNS-over-TLS (port 853) to any destination is blocked by iptables unless the destination is on the egress allowlist

### REQ-004-026: SSH Port Forwarding Restrictions

The VM's sshd MUST be configured to prevent SSH port forwarding from being used to bypass egress controls.

**Acceptance criteria:**
- [ ] The VM's sshd is configured with `AllowTcpForwarding local`
- [ ] The VM's sshd is configured with `GatewayPorts no`
- [ ] The VM's sshd is configured with `PermitTunnel no`
- [ ] The VM's sshd is configured with `X11Forwarding no`
- [ ] Local port forwarding (`-L`) binds only to `127.0.0.1`, never `0.0.0.0`
- [ ] Reverse port forwarding (`-R`) is disabled (blocked by `AllowTcpForwarding local`)
- [ ] Dynamic port forwarding / SOCKS proxy (`-D`) is disabled (blocked by `AllowTcpForwarding local`)
- [ ] These settings are provisioned during `sd create` and verified by `sd doctor`

### REQ-004-027: SSH Agent and X11 Forwarding Disabled

SSH agent forwarding and X11 forwarding MUST be explicitly disabled to prevent host credential leakage.

**Acceptance criteria:**
- [ ] The SSH config fragment for each VM includes `ForwardAgent no`
- [ ] The SSH config fragment for each VM includes `ForwardX11 no`
- [ ] The Lima YAML (or equivalent backend config) includes `ssh.forwardAgent: false`
- [ ] There is no CLI flag or configuration option to enable agent forwarding
- [ ] `sd doctor` warns if any SSH config fragment is missing `ForwardAgent no` or `ForwardX11 no`

### REQ-004-028: Download Integrity Verification

All built-in provisioning modules that download binaries or archives MUST verify integrity via SHA-256 checksum to prevent supply-chain attacks.

**Acceptance criteria:**
- [ ] The module YAML schema supports a `checksums` field mapping filename/platform to expected SHA-256 hash
- [ ] Provisioning scripts verify the SHA-256 checksum of every downloaded artifact against the expected value
- [ ] Checksum verification failure MUST abort the provisioning step with a fatal error and a message identifying the artifact and the expected vs. actual hash
- [ ] `curl | sh` patterns (piping downloaded scripts directly to a shell) MUST NOT be used in any built-in module
- [ ] `sd doctor` warns if any user-defined module downloads artifacts without a `checksums` field

### REQ-004-029: Project-Level Config Security Boundary

Security-sensitive configuration keys MUST NOT be settable from project-level config files. This prevents a malicious repository from weakening security controls via a checked-in `.sd/config.yaml`.

**Acceptance criteria:**
- [ ] All config keys under the `security.*` namespace in project-level `.sd/config.yaml` are ignored
- [ ] Security settings are only accepted from: user-level config (`$SD_HOME/config.yaml`), CLI flags, or environment variables
- [ ] `sd doctor` warns if a project-level `.sd/config.yaml` contains any `security.*` keys, with a message explaining they are ignored
- [ ] The ignored keys are logged at debug level for diagnostics

### REQ-004-030: Git Credential Cache Prevention

Git inside the VM MUST be configured to prevent credential caching to disk, ensuring credentials exist only in process memory during the session.

**Acceptance criteria:**
- [ ] Git is configured with `credential.helper ""` (empty string) to clear any default credential helpers before the custom helper is set
- [ ] Provisioning verifies that `~/.git-credentials` does not exist inside the VM; if found, it is deleted with a warning
- [ ] `git config --global credential.helper` inside the VM returns only the `sd`-configured helper, not a caching helper
- [ ] `sd doctor` checks for the presence of `~/.git-credentials` and warns if found

### REQ-004-031: SSH Host Key Verification

For TCP-based SSH connections (non-VSOCK), the VM's host key MUST be verified to prevent man-in-the-middle attacks.

**Acceptance criteria:**
- [ ] During `sd create`, the VM's SSH host key fingerprint is captured and stored at `$SD_HOME/vms/<name>/ssh/known_hosts`
- [ ] The SSH config fragment for TCP-based connections uses `StrictHostKeyChecking yes` with `UserKnownHostsFile` pointing to `$SD_HOME/vms/<name>/ssh/known_hosts`
- [ ] Only VSOCK-based connections (which do not traverse a network) may use `StrictHostKeyChecking no` with `UserKnownHostsFile /dev/null`
- [ ] If the VM's host key changes (e.g., after a recreate), `sd` detects the mismatch and prompts the user to accept the new key or abort
- [ ] `sd destroy <name>` removes the stored known_hosts file along with the VM's SSH key pair

## Design

### Interfaces

```go
// SecurityValidator validates security-sensitive operations before execution.
type SecurityValidator interface {
    // ValidateMountPath checks whether a host path is safe to mount.
    // Returns an error describing why the path is rejected, or nil if allowed.
    ValidateMountPath(hostPath string, mode MountMode) error

    // ValidateToken checks a token string and returns warnings
    // (e.g., classic PAT detected, overly broad scopes).
    ValidateToken(token string, tokenType TokenType) []SecurityWarning
}

// MountMode represents the access mode for a host mount.
type MountMode int

const (
    MountReadOnly  MountMode = iota
    MountReadWrite
)

// TokenType identifies the kind of credential being validated.
type TokenType int

const (
    TokenGitHubPAT TokenType = iota
    TokenGitHubFinegrained
    TokenAnthropicAPI
)

// SecurityWarning represents a non-fatal security observation.
type SecurityWarning struct {
    Code    string // machine-readable warning code, e.g., "CLASSIC_PAT"
    Message string // human-readable description
}
```

```go
// EgressController manages outbound network rules for a VM.
type EgressController interface {
    // ApplyAllowlist provisions iptables rules and the local DNS
    // resolver configuration inside the VM for the given set of
    // allowed domains.
    ApplyAllowlist(vmName string, domains []EgressDomain) error

    // AddDomain adds a domain to the running VM's allowlist,
    // updating both iptables rules and the DNS resolver config
    // without restarting the VM.
    AddDomain(vmName string, domain EgressDomain) error

    // RemoveDomain removes a domain from the running VM's allowlist,
    // updating both iptables rules and the DNS resolver config.
    RemoveDomain(vmName string, domain string) error

    // ListDomains returns the effective allowlist for a VM.
    ListDomains(vmName string) ([]EgressDomain, error)
}

// EgressDomain represents an allowed outbound destination.
type EgressDomain struct {
    Domain string       // e.g., "api.anthropic.com"
    Source DomainSource // "default" or "user"
}

// DomainSource indicates whether a domain is from the built-in
// default list or was added by the user.
type DomainSource string

const (
    DomainSourceDefault DomainSource = "default"
    DomainSourceUser    DomainSource = "user"
)
```

```go
// CredentialInjector manages runtime injection of credentials into VM sessions.
type CredentialInjector interface {
    // InjectEnv returns the environment variables to set for a VM session.
    // Values are sourced from sd config, host environment, or keychain.
    // This method MUST NOT write credentials to any file.
    // Credentials MUST be injected via SSH SendEnv/AcceptEnv, never via
    // command-line arguments.
    InjectEnv(vmName string) (map[string]string, error)

    // Rotate updates the stored credential for a VM.
    Rotate(vmName string, tokenType TokenType, newValue string) error

    // Revoke removes all stored credentials for a VM.
    Revoke(vmName string) error

    // List returns the configured credential types (without values) for a VM.
    List(vmName string) ([]CredentialEntry, error)
}

// CredentialEntry describes a configured credential without exposing its value.
type CredentialEntry struct {
    Type      TokenType
    Name      string // e.g., "GITHUB_TOKEN"
    Configured bool
}
```

```go
// AuditLogger records sd operations and VM lifecycle events.
type AuditLogger interface {
    // LogCommand records an sd CLI invocation.
    LogCommand(entry CommandLogEntry) error

    // LogEvent records a VM lifecycle event.
    LogEvent(entry EventLogEntry) error

    // Query returns log entries matching the given filter.
    Query(filter AuditFilter) ([]AuditEntry, error)

    // VerifyChain validates the hash chain integrity of the audit log.
    // Returns the index and details of the first broken link, or nil if intact.
    VerifyChain() (*ChainBreak, error)
}

// CommandLogEntry represents a single CLI command invocation.
type CommandLogEntry struct {
    Timestamp time.Time
    Command   string
    Args      []string // secrets MUST be redacted before logging
    ExitCode  int
    Duration  time.Duration
}

// EventLogEntry represents a VM lifecycle event.
type EventLogEntry struct {
    Timestamp time.Time
    EventType string // create, start, stop, destroy, connect, etc.
    VMName    string
    Metadata  map[string]string
}

// AuditFilter specifies criteria for querying the audit log.
type AuditFilter struct {
    VMName *string
    Since  *time.Time
    Until  *time.Time
}

// AuditEntry is the union type returned by Query.
type AuditEntry struct {
    Timestamp time.Time
    Type      string // "command" or "event"
    PrevHash  string // SHA-256 hash of the previous log entry
    Command   *CommandLogEntry
    Event     *EventLogEntry
}

// ChainBreak describes a broken link in the audit log hash chain.
type ChainBreak struct {
    LineNumber   int
    ExpectedHash string
    ActualHash   string
}
```

### CLI Surface

```
# Mount validation
sd create myvm                                        # No mounts (default)
sd create myvm --mount /path:/guest                   # Read-only mount
sd create myvm --mount /path:/guest:rw                # Read-write mount (explicit)
sd create myvm --mount ~/.ssh:/keys                   # ERROR: sensitive path

# Egress control
sd create myvm --allow-egress custom.example.com      # Add to allowlist at creation
sd config egress add myvm custom.example.com          # Add post-creation
sd config egress remove myvm custom.example.com       # Remove post-creation
sd config egress list myvm                            # Show effective allowlist
sd config egress list myvm --json                     # JSON output

# Credential management
sd token github setup                                 # Interactive PAT setup guidance
sd token github setup --json                          # Recommended scopes as JSON
sd token rotate myvm                                  # Rotate credentials
sd token revoke myvm                                  # Revoke all credentials
sd token list myvm --json                             # List configured credential types

# Snapshots
sd snapshot create myvm                               # Manual snapshot
sd snapshot create myvm --label "before-refactor"     # Labeled snapshot
sd snapshot list myvm --json                          # List snapshots
sd snapshot restore myvm <snapshot-id>                # Restore from snapshot

# Audit
sd audit myvm                                         # Show audit log for VM
sd audit myvm --json                                  # JSON output
sd audit --since 2026-03-27T00:00:00Z                 # Filter by time
sd audit --verify                                     # Verify hash chain integrity

# Security status
sd security status myvm                               # Security posture summary
sd security status myvm --json                        # JSON output
```

### Egress Configuration File Format

The egress allowlist is configured in the `sd` config file (default `$SD_HOME/config.yaml`):

```yaml
security:
  egress_allowlist:
    - custom.api.example.com
    - internal.registry.example.com
  sensitive_paths:
    - /path/to/additional/sensitive/dir
  dns_refresh_interval: 5m
```

### Audit Log Format

Each line in the audit log is a JSON object (JSON Lines format). Each entry includes a `prev_hash` field containing the SHA-256 hash of the preceding log line to form a tamper-evident hash chain:

```json
{"ts":"2026-03-27T10:15:30Z","type":"command","prev_hash":"0000000000000000000000000000000000000000000000000000000000000000","command":"sd create myvm","args":["create","myvm","--allow-egress","custom.example.com"],"exit_code":0,"duration_ms":4523}
{"ts":"2026-03-27T10:15:30Z","type":"event","prev_hash":"a1b2c3...","event":"create","vm":"myvm","meta":{"mounts":"0","egress_rules":"13"}}
{"ts":"2026-03-27T10:20:00Z","type":"command","prev_hash":"d4e5f6...","command":"sd connect myvm","args":["connect","myvm"],"exit_code":0,"duration_ms":1200}
{"ts":"2026-03-27T10:20:00Z","type":"event","prev_hash":"g7h8i9...","event":"connect","vm":"myvm","meta":{"credentials_injected":"GITHUB_TOKEN,ANTHROPIC_API_KEY"}}
```

### iptables Rule Structure

The provisioning script MUST install rules in a dedicated chain for manageability:

```
# Create sd-egress chain
iptables -N sd-egress
iptables -A OUTPUT -j sd-egress

# Allow loopback
iptables -A sd-egress -o lo -j ACCEPT

# Allow established connections
iptables -A sd-egress -m state --state ESTABLISHED,RELATED -j ACCEPT

# Allow DNS to local filtering resolver ONLY
iptables -A sd-egress -p udp --dport 53 -d 127.0.0.1 -j ACCEPT
iptables -A sd-egress -p tcp --dport 53 -d 127.0.0.1 -j ACCEPT

# Block DNS to all other resolvers
iptables -A sd-egress -p udp --dport 53 -j DROP
iptables -A sd-egress -p tcp --dport 53 -j DROP

# Block DNS-over-TLS (port 853) unless destination is on allowlist
iptables -A sd-egress -p tcp --dport 853 -j DROP

# Block DNS-over-HTTPS to known DoH providers unless on allowlist
iptables -A sd-egress -p tcp --dport 443 -d 8.8.8.8 -j DROP
iptables -A sd-egress -p tcp --dport 443 -d 8.8.4.4 -j DROP
iptables -A sd-egress -p tcp --dport 443 -d 1.1.1.1 -j DROP
iptables -A sd-egress -p tcp --dport 443 -d 1.0.0.1 -j DROP
iptables -A sd-egress -p tcp --dport 443 -d 9.9.9.9 -j DROP
iptables -A sd-egress -p tcp --dport 443 -d 149.112.112.112 -j DROP

# Allow SSH from host (required for sd operation)
iptables -A sd-egress -p tcp --sport 22 -j ACCEPT

# Per-domain ACCEPT rules (populated by DNS resolution)
iptables -A sd-egress -d <resolved-ip> -j ACCEPT
# ... one rule per resolved IP per domain ...

# Default deny
iptables -A sd-egress -j DROP
```

### Local DNS Resolver Configuration (dnsmasq)

The provisioning script MUST configure dnsmasq as a local filtering resolver:

```
# /etc/dnsmasq.d/sd-egress.conf

# Listen only on loopback
listen-address=127.0.0.1
bind-interfaces

# Default: return NXDOMAIN for all queries
address=/#/

# Forward queries for allowlisted domains to upstream DNS
server=/api.anthropic.com/<upstream-dns>
server=/github.com/<upstream-dns>
server=/githubusercontent.com/<upstream-dns>
server=/archive.ubuntu.com/<upstream-dns>
server=/security.ubuntu.com/<upstream-dns>
server=/deb.debian.org/<upstream-dns>
server=/registry.npmjs.org/<upstream-dns>
server=/pypi.org/<upstream-dns>
server=/files.pythonhosted.org/<upstream-dns>
server=/proxy.golang.org/<upstream-dns>
server=/sum.golang.org/<upstream-dns>

# Log DNS queries for audit trail
log-queries
log-facility=/var/log/dnsmasq.log
```

### SSH Config Fragment (TCP-based)

```
Host sd-<name>
    HostName 127.0.0.1
    Port <port>
    User dev
    IdentityFile $SD_HOME/vms/<name>/ssh/id_ed25519
    StrictHostKeyChecking yes
    UserKnownHostsFile $SD_HOME/vms/<name>/ssh/known_hosts
    ForwardAgent no
    ForwardX11 no
    SendEnv SD_* ANTHROPIC_* GITHUB_* GH_*
    LogLevel ERROR
```

### SSH Config Fragment (VSOCK-based)

```
Host sd-<name>
    User dev
    IdentityFile $SD_HOME/vms/<name>/ssh/id_ed25519
    ProxyCommand limactl tunnel <name> -- ssh -W %h:%p -o StrictHostKeyChecking=no
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
    ForwardAgent no
    ForwardX11 no
    SendEnv SD_* ANTHROPIC_* GITHUB_* GH_*
    LogLevel ERROR
```

### VM sshd Configuration

The following settings MUST be added to the VM's `/etc/ssh/sshd_config` or a drop-in file under `/etc/ssh/sshd_config.d/`:

```
# Accept environment variables from sd client
AcceptEnv SD_* ANTHROPIC_* GITHUB_* GH_*

# Disable all forwarding that could bypass egress controls
AllowTcpForwarding local
GatewayPorts no
PermitTunnel no
X11Forwarding no
```

### Module YAML Checksum Schema

Built-in provisioning modules MUST support a `checksums` field for download verification:

```yaml
name: go
version: "1.22.0"
downloads:
  - url: https://go.dev/dl/go1.22.0.linux-amd64.tar.gz
    dest: /usr/local/go1.22.0.linux-amd64.tar.gz
    checksums:
      sha256: "5901c52b7a78002aeff14a21f93e0f064f74ce1360fce51c6ee68cd471216a17"
install: |
  # Checksum is verified automatically before this script runs
  tar -C /usr/local -xzf /usr/local/go1.22.0.linux-amd64.tar.gz
```

## Error Handling

### Mount Path Rejected

- **Trigger**: User specifies a mount path that matches a sensitive directory
- **Severity**: Fatal
- **User sees (text)**: `Error: mount path "/Users/user/.ssh" is a sensitive directory and cannot be mounted. Sensitive directories include: $HOME, ~/.ssh, ~/.aws, ~/.config, ~/.gnupg, ~/.kube, ~/.docker, browser profiles, and the Docker socket. See "sd help security" for details.`
- **User sees (JSON)**: `{"error": "mount_path_rejected", "path": "/Users/user/.ssh", "reason": "sensitive_directory", "category": ".ssh"}`
- **Recovery**: Choose a non-sensitive path or clone the repo inside the VM instead

### Egress Connection Blocked

- **Trigger**: Process inside VM attempts to connect to a non-allowlisted destination
- **Severity**: Silent (from `sd`'s perspective; the connection simply fails inside the VM)
- **User sees**: Connection timeout or refusal inside the VM
- **Recovery**: Add the domain to the egress allowlist with `sd config egress add <name> <domain>`

### DNS Query Blocked

- **Trigger**: Process inside VM attempts to resolve a domain not on the egress allowlist
- **Severity**: Silent (from `sd`'s perspective; the resolver returns NXDOMAIN)
- **User sees**: DNS resolution failure (NXDOMAIN) inside the VM
- **Recovery**: Add the domain to the egress allowlist with `sd config egress add <name> <domain>`

### Classic PAT Detected

- **Trigger**: User provides a GitHub token with the `ghp_` prefix (indicating a classic PAT)
- **Severity**: Warning
- **User sees (text)**: `Warning: classic GitHub PAT detected (prefix "ghp_"). Fine-grained PATs (prefix "github_pat_") are recommended for better security scoping. See "sd token github setup" for guidance.`
- **User sees (JSON)**: `{"warning": "classic_pat_detected", "recommendation": "Use a fine-grained PAT scoped to specific repositories"}`
- **Recovery**: Create a fine-grained PAT and update with `sd token rotate <name>`

### Credential Injection Failure

- **Trigger**: `sd connect` cannot find a configured credential (not in config, not in host env, not in keychain)
- **Severity**: Warning
- **User sees (text)**: `Warning: ANTHROPIC_API_KEY not configured for VM "myvm". The session will start without it. Use "sd token rotate myvm" to configure.`
- **User sees (JSON)**: `{"warning": "credential_not_found", "credential": "ANTHROPIC_API_KEY", "vm": "myvm"}`
- **Recovery**: Configure the credential with `sd token rotate <name>` or set it in the host environment

### Download Checksum Mismatch

- **Trigger**: A downloaded artifact's SHA-256 hash does not match the expected value in the module's `checksums` field
- **Severity**: Fatal
- **User sees (text)**: `Error: checksum verification failed for "go1.22.0.linux-amd64.tar.gz". Expected SHA-256: 5901c52b..., got: a3f1b2c4.... Provisioning aborted. The downloaded file may have been tampered with.`
- **User sees (JSON)**: `{"error": "checksum_mismatch", "file": "go1.22.0.linux-amd64.tar.gz", "expected": "5901c52b...", "actual": "a3f1b2c4...", "aborted": true}`
- **Recovery**: Verify the expected checksum is correct, check network integrity, and retry

### Snapshot Creation Failure

- **Trigger**: Auto-snapshot before a destructive operation fails (e.g., insufficient disk space)
- **Severity**: Fatal (the destructive operation MUST NOT proceed)
- **User sees (text)**: `Error: failed to create safety snapshot before destroy: insufficient disk space. The destroy operation has been aborted. Free disk space and retry, or use --no-snapshot to skip (not recommended).`
- **User sees (JSON)**: `{"error": "snapshot_failed", "reason": "insufficient_disk_space", "operation": "destroy", "aborted": true}`
- **Recovery**: Free disk space and retry, or use `--no-snapshot` to proceed without a safety snapshot

### Audit Log Write Failure

- **Trigger**: `sd` cannot write to the audit log file (permissions, disk full)
- **Severity**: Warning (operations continue but log the failure to stderr)
- **User sees (text)**: `Warning: failed to write audit log: permission denied. Operations will continue but will not be logged.`
- **User sees (JSON)**: `{"warning": "audit_log_failure", "reason": "permission_denied", "path": "$SD_HOME/audit.log"}`
- **Recovery**: Fix file permissions or configure a different log path

### Project-Level Security Config Ignored

- **Trigger**: A project-level `.sd/config.yaml` contains `security.*` keys
- **Severity**: Warning
- **User sees (text)**: `Warning: project-level config ".sd/config.yaml" contains security keys (security.mount_policy, security.egress_allowlist) which are ignored. Security settings must be configured at the user level. See "sd help security" for details.`
- **User sees (JSON)**: `{"warning": "project_security_config_ignored", "keys": ["security.mount_policy", "security.egress_allowlist"], "file": ".sd/config.yaml"}`
- **Recovery**: Move security settings to user-level config (`$SD_HOME/config.yaml`)

## Security Considerations

This spec IS the security spec. The following analysis covers the security of the security model itself:

### Trust Boundaries

1. **Host to VM**: The VM boundary is the primary trust boundary. The host trusts itself but does not trust the VM guest. Mounts and credential injection cross this boundary and are the highest-risk surface area.
2. **VM to external services**: Egress rules constrain which external services the VM can reach. The allowlist is the trust boundary for network access. DNS filtering (REQ-004-025) ensures that even DNS queries cannot be used as a covert channel.
3. **sd CLI to stored config**: The `sd` config file on the host stores credential references and VM configuration. This file must have restrictive permissions (0600).
4. **Project config to security policy**: Project-level config files are untrusted input. Security-sensitive keys from project config are ignored (REQ-004-029) to prevent a malicious repository from weakening defenses.

### Credential Handling

- Credentials are stored in the `sd` config on the host. The config file MUST be created with mode 0600.
- Credentials are transmitted to the VM via SSH `SendEnv`/`AcceptEnv`. They exist in the VM's process memory but not on the VM's filesystem or in process command lines.
- Git credential helpers inside the VM MUST NOT cache credentials to disk. The default credential helper is cleared before the custom helper is set (REQ-004-030).
- The audit log MUST redact all credential values.

### Blast Radius if Compromised

- **VM compromised**: Agent has full access within the VM. Impact is limited to: the current project's code (if cloned inside), any mounted directories (read-only by default), and allowlisted network endpoints. Host filesystem, host credentials, and non-allowlisted networks are unreachable. DNS exfiltration is blocked by the local filtering resolver.
- **sd config compromised on host**: Attacker gains access to stored credential values. Mitigation: file permissions, and future integration with OS keychain (out of scope for this spec).
- **Audit log compromised**: No credential exposure (redacted). Attacker learns operational patterns. Hash chain (REQ-004-022) makes silent tampering detectable.

### Mitigations Summary

| Threat                         | Mitigation                              | Requirement  |
|--------------------------------|-----------------------------------------|--------------|
| Host file access               | VM isolation, no default mounts         | REQ-004-002, REQ-004-003 |
| Sensitive directory exposure   | Mount path validation                   | REQ-004-005  |
| Network exfiltration           | Default-deny egress, iptables allowlist | REQ-004-006, REQ-004-009 |
| DNS exfiltration               | Local filtering DNS resolver            | REQ-004-025  |
| SSH port forwarding bypass     | sshd forwarding restrictions            | REQ-004-026  |
| SSH agent credential leakage   | Agent/X11 forwarding disabled           | REQ-004-027  |
| Credential theft               | Runtime injection via SendEnv, no disk persistence | REQ-004-011  |
| Process list credential exposure | SendEnv/AcceptEnv instead of env VAR=val | REQ-004-011 |
| Broad GitHub access            | Fine-grained PATs, minimal scopes       | REQ-004-012  |
| Git credential caching         | Empty credential.helper, no .git-credentials | REQ-004-030 |
| Persistent hooks/backdoors     | Snapshots, CI change detection          | REQ-004-018, REQ-004-019 |
| Undetected malicious activity  | Audit logging with hash chain           | REQ-004-021, REQ-004-022 |
| Supply chain attacks           | SHA-256 checksum verification           | REQ-004-028  |
| Malicious project config       | Project-level security keys ignored     | REQ-004-029  |
| SSH MITM attacks               | Host key verification for TCP connections | REQ-004-031 |
| Audit log tampering            | Hash chain integrity verification       | REQ-004-022  |

## Testing Strategy

### Unit Tests

| Requirement   | Test Description                                                        |
|---------------|-------------------------------------------------------------------------|
| REQ-004-005   | `TestValidateMountPath_RejectsSensitivePaths` — table-driven test for every sensitive path category |
| REQ-004-005   | `TestValidateMountPath_ResolvesSymlinks` — create symlinks to sensitive paths, verify rejection |
| REQ-004-005   | `TestValidateMountPath_AcceptsValidPaths` — verify non-sensitive subdirectories are allowed |
| REQ-004-007   | `TestDefaultEgressAllowlist_ContainsRequiredDomains` — verify all default domains are present |
| REQ-004-007   | `TestWildcardMatching_SingleLevelOnly` — verify `*.example.com` matches `foo.example.com` but not `bar.foo.example.com` |
| REQ-004-008   | `TestEgressAllowlist_MergesUserAndDefault` — verify user additions are merged, not replacing defaults |
| REQ-004-012   | `TestValidateToken_DetectsClassicPAT` — verify `ghp_` prefix triggers warning |
| REQ-004-012   | `TestValidateToken_AcceptsFinegrainedPAT` — verify `github_pat_` prefix passes without warning |
| REQ-004-021   | `TestAuditLog_RedactsCredentials` — verify token values are replaced with `[REDACTED]` |
| REQ-004-022   | `TestAuditLog_HashChain` — verify each entry's `prev_hash` matches SHA-256 of the preceding line |
| REQ-004-023   | `TestSensitivePathList_MergesUserConfig` — verify user-added paths are included in validation |
| REQ-004-025   | `TestDnsResolver_BlocksNonAllowlisted` — verify NXDOMAIN for domains not on the allowlist |
| REQ-004-025   | `TestDnsResolver_AllowsAllowlisted` — verify resolution succeeds for allowlisted domains |
| REQ-004-028   | `TestChecksumVerification_FailsOnMismatch` — verify provisioning aborts on checksum mismatch |
| REQ-004-029   | `TestProjectConfig_SecurityKeysIgnored` — verify `security.*` keys in project config are ignored |
| REQ-004-030   | `TestGitCredentialHelper_ClearedBeforeCustom` — verify `credential.helper ""` is set before custom helper |

### Integration Tests

| Requirement   | Test Description                                                        |
|---------------|-------------------------------------------------------------------------|
| REQ-004-003   | Create a VM with default settings, verify no mounts via `sd status --json` |
| REQ-004-004   | Create a VM with a read-only mount, verify mount mode via `sd status --json` |
| REQ-004-006   | Create a VM, attempt `curl http://example.com` from inside, verify failure |
| REQ-004-007   | Create a VM, verify connectivity to each default allowlist domain |
| REQ-004-009   | Create a VM, inspect iptables rules inside the VM, verify sd-egress chain exists with DROP default |
| REQ-004-011   | Create a VM, connect, verify `GITHUB_TOKEN` and `ANTHROPIC_API_KEY` are set in the session environment |
| REQ-004-011   | After connecting, `grep -r` the VM filesystem for credential values, verify no results |
| REQ-004-011   | After connecting, verify credential values do not appear in `/proc/*/cmdline` for any process |
| REQ-004-014   | Create two VMs, verify they have different SSH key pairs |
| REQ-004-019   | Run `sd destroy`, verify a snapshot was created before destruction |
| REQ-004-025   | Create a VM, attempt DNS resolution of a non-allowlisted domain, verify NXDOMAIN |
| REQ-004-025   | Create a VM, attempt DNS-over-HTTPS to 8.8.8.8:443, verify connection blocked |
| REQ-004-026   | Create a VM, attempt `ssh -R` reverse port forwarding, verify it is denied |
| REQ-004-026   | Create a VM, attempt `ssh -D` dynamic forwarding, verify it is denied |
| REQ-004-027   | Create a VM, verify SSH config fragment contains `ForwardAgent no` and `ForwardX11 no` |
| REQ-004-030   | Create a VM, verify `git config --global credential.helper` does not return a caching helper |
| REQ-004-031   | Create a VM with TCP transport, verify SSH config uses `StrictHostKeyChecking yes` and the correct known_hosts file |

### Script Tests

| Requirement   | Test Description                                                        |
|---------------|-------------------------------------------------------------------------|
| REQ-004-005   | Script: attempt `sd create` with each sensitive path, verify exit code 1 and error message |
| REQ-004-008   | Script: `sd create` with `--allow-egress`, then `sd config egress list --json`, verify domain appears |
| REQ-004-015   | Script: `sd token rotate`, then `sd token list --json`, verify credential type is configured |
| REQ-004-015   | Script: `sd token revoke`, then `sd token list --json`, verify no credentials configured |
| REQ-004-020   | Script: `sd snapshot create` with label, then `sd snapshot list --json`, verify label and timestamp |
| REQ-004-022   | Script: `sd audit --verify` on a valid log, verify success; tamper with a line, verify failure |
| REQ-004-024   | Script: `sd security status --json`, verify all expected fields are present |
| REQ-004-028   | Script: create a module with checksums, tamper with download, verify provisioning aborts |
| REQ-004-029   | Script: create project-level config with `security.*` keys, run `sd doctor`, verify warning |

## Dependencies

### Depends On

- [003-vm-backend.md](003-vm-backend.md) — VM creation, start, stop, destroy, snapshot operations. Security controls layer on top of the VM backend.
- [002-cli.md](002-cli.md) — CLI framework, command structure, `--json` support, exit codes. Security commands follow the CLI conventions.
- [001-architecture.md](001-architecture.md) — Overall architecture, package structure. Security code lives in `internal/security/`.
- [005-configuration.md](005-configuration.md) — Configuration system, precedence rules, project-level vs. user-level config distinction. Security config boundary (REQ-004-029) depends on the config system's layering.
- [006-provisioning.md](006-provisioning.md) — Provisioning module system, module YAML schema. Download integrity verification (REQ-004-028) extends the module schema with a `checksums` field.
- [007-connection.md](007-connection.md) — SSH connection management, config fragments, environment variable injection. SSH hardening requirements (REQ-004-026, REQ-004-027, REQ-004-031) constrain the connection spec's SSH configuration.

### Depended On By

- All specs that involve VM creation or agent session management depend on this spec for security defaults.

## Open Questions

- **OQ-1**: Should `sd` integrate with the macOS Keychain (or Linux secret-service) for credential storage on the host, rather than storing in the config file? This would improve host-side security but adds platform-specific complexity.
- **OQ-2**: Should egress control support port-level granularity (e.g., allow HTTPS only, block SSH outbound) in addition to domain-level control?
- **OQ-3**: Should `sd` support a `--paranoid` mode that disables all mounts, all user egress additions, and restricts credentials to a single repo? This would be a convenience alias for the most restrictive configuration.

## Revision History

| Date       | Author | Change Description |
|------------|--------|--------------------|
| 2026-03-27 | claude | Initial draft      |
| 2026-03-27 | claude | Security review fixes: added DNS exfiltration prevention via local filtering resolver (REQ-004-025); SSH port forwarding restrictions (REQ-004-026); SSH agent/X11 forwarding disabled (REQ-004-027); download checksum verification (REQ-004-028); project-level security config boundary (REQ-004-029); git credential cache prevention (REQ-004-030); SSH host key verification for TCP connections (REQ-004-031); audit log hash chain for tamper detection; credential injection via SendEnv/AcceptEnv; wildcard depth clarification; fixed `sd info` to `sd status`; fixed audit log path to `$SD_HOME/audit.log`; added dependency references for specs 005, 006, 007 |
