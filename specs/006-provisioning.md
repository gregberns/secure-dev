# 006: VM Provisioning

| Field        | Value      |
|--------------|------------|
| Status       | draft      |
| Created      | 2026-03-27 |
| Last Updated | 2026-03-27 |
| Authors      | claude     |
| Reviewers    |            |

## Overview

This spec defines how `sd` provisions VMs with development tools, language runtimes, and agent-specific configurations after VM creation. Provisioning uses a composable module system where each module is a named, self-contained unit that installs and configures a specific tool or capability. The system ensures idempotent execution, dependency ordering, and readiness verification so that a freshly created VM is fully usable when `sd create` returns.

## Goals

- G1: Define a composable, dependency-aware module system for VM provisioning
- G2: Provide built-in modules for common development tools and the Claude Code agent
- G3: Support user-defined custom provisioning modules
- G4: Ensure all provisioning scripts are idempotent and safe to re-run
- G5: Provide readiness probes so `sd create` blocks until tools are verified available
- G6: Configure git identity, credentials, and repo cloning inside the VM
- G7: Support re-provisioning existing VMs without recreating them

## Non-Goals

- NG1: Package management for the host machine -- provisioning targets the guest VM only
- NG2: Provisioning Windows guests -- only Linux guests are supported
- NG3: Runtime version management (e.g., switching between Go 1.21 and 1.22) -- modules install a single current version
- NG4: Orchestrating provisioning across multiple VMs simultaneously -- each VM is provisioned independently
- NG5: Building custom VM images or golden images -- provisioning runs at boot/create time

## Requirements

### REQ-006-001: Built-in Module Set

The `sd` binary MUST include the following built-in provisioning modules, embedded via `//go:embed`:

| Module        | Installs                                          |
|---------------|---------------------------------------------------|
| `base`        | git, curl, build-essential, ca-certificates, jq, tmux, vim |
| `claude-code` | Node.js (via nvm), Claude Code CLI (via npm)      |
| `docker`      | Docker Engine with rootless setup                 |
| `golang`      | Go toolchain (current stable)                     |
| `rust`        | Rust toolchain via rustup                         |
| `python`      | Python 3 with pip and venv                        |
| `github-cli`  | GitHub CLI (`gh`)                                 |

**Acceptance criteria:**
- [ ] Each module listed above exists as an embedded YAML file in the binary
- [ ] `sd provision list` displays all built-in modules with their descriptions
- [ ] The `base` module installs all seven packages listed

### REQ-006-002: Base Module Always Runs

The `base` module MUST run on every provisioning operation, regardless of which modules are requested. All other modules implicitly depend on `base`.

**Acceptance criteria:**
- [ ] Creating a VM with no explicit module selection still runs `base`
- [ ] Creating a VM with `--modules docker,golang` runs `base` before `docker` and `golang`
- [ ] The `base` module cannot be excluded or skipped

### REQ-006-003: Module Definition Format

Each provisioning module MUST be defined as a YAML file with the following schema:

```yaml
name: <string>           # Required. Unique identifier, kebab-case.
description: <string>    # Required. Human-readable summary.
depends_on:              # Optional. List of module names that must run first.
  - <module-name>
scripts:                 # Required. Ordered list of script blocks.
  - mode: system|user    # Required. "system" runs as root, "user" runs as default user.
    script: |            # Required. Shell script content.
      <shell commands>
checksums:               # Required for modules that download binaries.
  <filename>: <sha256>   # SHA-256 checksum for each downloaded file.
probe:                   # Optional. Readiness probe command.
  command: <string>      # Shell command that exits 0 when the module's tools are ready.
  interval: <duration>   # Time between probe attempts. Default: 5s.
  timeout: <duration>    # Max time to wait for probe success. Default: 5m.
```

**Acceptance criteria:**
- [ ] A module YAML file with all required fields parses without error
- [ ] A module YAML file missing `name`, `description`, or `scripts` produces a validation error
- [ ] `mode` values other than `system` or `user` produce a validation error
- [ ] `depends_on` referencing a nonexistent module produces a validation error
- [ ] Built-in modules that download binaries MUST include a `checksums` field

### REQ-006-004: Module Dependency Ordering

The provisioning system MUST execute modules in topological order based on their `depends_on` declarations. If two modules have no dependency relationship, their relative execution order is undefined but deterministic for a given set of modules.

**Acceptance criteria:**
- [ ] A module with `depends_on: [base]` runs after `base` completes
- [ ] A module with `depends_on: [golang]` causes `golang` (and transitively `base`) to run first
- [ ] A circular dependency (A depends on B, B depends on A) produces a fatal error at validation time, not at runtime
- [ ] Dependency resolution is computed before any scripts execute

### REQ-006-005: Script Execution Environment

All provisioning scripts MUST execute with `set -eux -o pipefail` prepended. Scripts with `mode: system` MUST run as root. Scripts with `mode: user` MUST run as the VM's default non-root user.

**Acceptance criteria:**
- [ ] A script that references an undefined variable causes the provisioning step to fail
- [ ] A script with a failing command causes the provisioning step to fail
- [ ] `mode: system` scripts run with UID 0
- [ ] `mode: user` scripts run with the default user's UID (non-zero)

### REQ-006-006: Script Idempotency

All built-in module scripts MUST be idempotent. Scripts SHOULD use `command -v` guards or equivalent checks to skip installation of already-present tools. Running a module twice on the same VM MUST produce the same end state as running it once.

**Acceptance criteria:**
- [ ] Running `sd provision <vm>` twice in succession succeeds both times
- [ ] The second run completes faster than the first (skipping already-installed tools)
- [ ] No duplicate entries in PATH, apt sources, or configuration files after re-provisioning

### REQ-006-007: Custom Module Support

Users MUST be able to define custom provisioning modules by placing YAML files in `~/.sd/provisions/`. Custom modules follow the same schema as built-in modules (REQ-006-003). Custom module names MUST NOT conflict with built-in module names.

**Acceptance criteria:**
- [ ] A YAML file placed in `~/.sd/provisions/` is discovered by `sd provision list`
- [ ] A custom module can declare `depends_on` referencing built-in modules
- [ ] A custom module with the same name as a built-in module produces a fatal error
- [ ] Custom modules are included in provisioning when specified via `--modules`

### REQ-006-008: Readiness Probes

After all provisioning scripts complete, the system MUST execute readiness probes for each module that defines one. Probes MUST be polled at the configured interval until they succeed or the timeout expires.

**Acceptance criteria:**
- [ ] A module with `probe.command: "command -v go"` blocks until `go` is on PATH
- [ ] A probe that does not succeed within its timeout causes `sd create` to exit with a non-zero status and an actionable error message
- [ ] `sd create` does not return success until all probes pass
- [ ] Default probe interval is 5 seconds and default timeout is 5 minutes

### REQ-006-009: Provisioning Progress Reporting

The `sd status <vm>` command MUST report provisioning state. During provisioning, it MUST show which modules have completed, which are in progress, and which are pending. The `--json` output MUST include a `provisioning` object with per-module status.

**Acceptance criteria:**
- [ ] `sd status <vm>` during provisioning shows module-level progress
- [ ] `sd status <vm> --json` includes `"provisioning": { "modules": [...] }` with status per module
- [ ] Completed modules show as `"completed"`, running as `"running"`, pending as `"pending"`, failed as `"failed"`

### REQ-006-010: Re-provisioning Existing VMs

The `sd provision <vm>` command MUST re-run all provisioning modules on an existing, running VM. If the VM is stopped, the command MUST return an error instructing the user to start it first.

**Acceptance criteria:**
- [ ] `sd provision <vm>` on a running VM executes all configured modules
- [ ] `sd provision <vm>` on a stopped VM exits with a non-zero status and a message indicating the VM must be running
- [ ] `sd provision <vm> --modules golang,rust` runs only the specified modules (plus their dependencies)
- [ ] Re-provisioning respects idempotency (REQ-006-006)

### REQ-006-011: Claude Code Provisioning

The `claude-code` module MUST perform the following setup:

1. Install Node.js via nvm
2. Install Claude Code CLI globally via `npm install -g @anthropic-ai/claude-code`
3. Configure `~/.claude/settings.json` with bypass permissions as defined in the VM's security policy
4. Inject `ANTHROPIC_API_KEY` from the host environment into the VM's runtime environment

The API key MUST NOT be persisted to disk inside the VM. It MUST be injected via environment variable at session start.

**Acceptance criteria:**
- [ ] After provisioning, `command -v claude` succeeds inside the VM
- [ ] `~/.claude/settings.json` exists with the configured bypass permissions
- [ ] `ANTHROPIC_API_KEY` is available in the agent's shell environment
- [ ] `ANTHROPIC_API_KEY` does not appear in any file on the VM's disk
- [ ] If `ANTHROPIC_API_KEY` is not set on the host, provisioning emits a warning but does not fail

### REQ-006-012: Git Configuration Inside VM

Provisioning MUST configure git inside the VM with:

1. `user.name` and `user.email` from the host's git config or from `sd` configuration
2. A credential helper that uses the injected GitHub token for HTTPS operations, with defaults cleared first
3. Clone of any repositories specified in the VM configuration, using a scoped GitHub token

The GitHub token MUST be scoped (fine-grained PAT or equivalent) and MUST NOT be persisted to disk. It MUST be injected at runtime. The credential helper setup MUST clear any pre-existing credential helpers and verify that `~/.git-credentials` does not exist.

**Acceptance criteria:**
- [ ] `git config user.name` inside the VM returns the configured name
- [ ] `git config user.email` inside the VM returns the configured email
- [ ] `git config --global credential.helper` is set to the custom helper script that reads from an environment variable
- [ ] `git config --global --get-all credential.helper` does not include any default helpers (cleared via `credential.helper ""` before setting custom helper)
- [ ] `~/.git-credentials` does not exist inside the VM
- [ ] `git clone https://github.com/org/repo` succeeds using the injected token without manual authentication
- [ ] The GitHub token does not appear in any file on the VM's disk (no `.gitconfig` credential store, no `.git-credentials`)
- [ ] If no GitHub token is provided, git clone operations that require authentication fail with an actionable error

### REQ-006-013: CLAUDE.md and AGENTS.md Propagation

When a repository is cloned into the VM, the `claude-code` module MUST detect and preserve any `CLAUDE.md` or `AGENTS.md` files present in the repository root. If the `sd` project configuration specifies additional agent instructions, they MUST be appended to the existing file (not overwrite it).

**Acceptance criteria:**
- [ ] A cloned repo with an existing `CLAUDE.md` retains its contents after provisioning
- [ ] Additional instructions from `sd` config are appended below a clear separator comment
- [ ] A cloned repo without `CLAUDE.md` or `AGENTS.md` does not have one created unless the `sd` config specifies instructions

### REQ-006-014: Embedded Module Storage

Built-in provisioning modules MUST be embedded in the `sd` Go binary using `//go:embed` directives. They MUST be stored under `internal/provision/modules/` in the source tree.

**Acceptance criteria:**
- [ ] Built-in module YAML files exist at `internal/provision/modules/*.yaml`
- [ ] The compiled binary contains the module definitions without requiring external files
- [ ] Adding a new built-in module requires only adding a YAML file and updating the embed directive

### REQ-006-015: Module Selection via Configuration and CLI

Users MUST be able to specify which modules to provision via:

1. The `sd` configuration file (persistent default for all VMs or per-VM)
2. The `--modules` flag on `sd create` and `sd provision` (comma-separated list)

CLI flags MUST override configuration file values. The `base` module runs regardless of selection (REQ-006-002).

**Acceptance criteria:**
- [ ] `sd create --modules claude-code,docker` provisions only `base`, `claude-code`, and `docker`
- [ ] A config file specifying `modules: [claude-code, golang]` applies when no `--modules` flag is given
- [ ] `--modules` flag overrides the config file module list entirely (not additive)
- [ ] `--modules all` provisions every available module (built-in and custom)

### REQ-006-016: Checksum Verification for Downloaded Binaries

Built-in modules that download binaries MUST verify SHA-256 checksums after download. The expected checksums MUST be declared in the module's `checksums` field. Scripts MUST NOT use `curl | sh` patterns; binaries MUST be downloaded to a temporary file, verified, and then installed.

**Acceptance criteria:**
- [ ] Every built-in module that downloads a binary includes a `checksums` field in its YAML
- [ ] Downloaded files are verified against their declared SHA-256 checksum before installation
- [ ] A checksum mismatch produces a fatal error with the expected and actual checksums
- [ ] No built-in module uses `curl | sh` or `curl | bash` patterns
- [ ] Custom modules are not required to have checksums but a warning is emitted if a custom module downloads files without verification

## Design

### Module System Architecture

```go
// Package provision handles VM provisioning with composable modules.
package provision

// Module represents a single provisioning module.
type Module struct {
    Name        string            `yaml:"name"`
    Description string            `yaml:"description"`
    DependsOn   []string          `yaml:"depends_on"`
    Scripts     []Script          `yaml:"scripts"`
    Checksums   map[string]string `yaml:"checksums,omitempty"`
    Probe       *Probe            `yaml:"probe,omitempty"`
}

// Script represents a single script block within a module.
type Script struct {
    Mode   string `yaml:"mode"`   // "system" or "user"
    Script string `yaml:"script"` // Shell script content
}

// Probe defines a readiness check for a module.
type Probe struct {
    Command  string        `yaml:"command"`
    Interval time.Duration `yaml:"interval"` // Default: 5s
    Timeout  time.Duration `yaml:"timeout"`  // Default: 5m
}

// ModuleStatus represents the execution state of a module.
type ModuleStatus string

const (
    ModuleStatusPending   ModuleStatus = "pending"
    ModuleStatusRunning   ModuleStatus = "running"
    ModuleStatusCompleted ModuleStatus = "completed"
    ModuleStatusFailed    ModuleStatus = "failed"
)

// ProvisionState tracks the overall provisioning progress for a VM.
type ProvisionState struct {
    VMName   string                    `json:"vm_name"`
    Started  time.Time                 `json:"started"`
    Finished *time.Time                `json:"finished,omitempty"`
    Modules  []ModuleExecutionStatus   `json:"modules"`
}

// ModuleExecutionStatus tracks execution state for a single module.
type ModuleExecutionStatus struct {
    Name      string       `json:"name"`
    Status    ModuleStatus `json:"status"`
    StartedAt *time.Time   `json:"started_at,omitempty"`
    EndedAt   *time.Time   `json:"ended_at,omitempty"`
    Error     string       `json:"error,omitempty"`
}
```

### Provisioner Interface

```go
// Provisioner orchestrates module execution on a VM.
type Provisioner interface {
    // LoadModules discovers and loads all available modules (built-in and custom).
    // Returns an error if any module fails validation.
    LoadModules() ([]Module, error)

    // Resolve computes execution order for the given module names,
    // including transitive dependencies. Returns an error on circular
    // dependencies or missing modules.
    Resolve(moduleNames []string) ([]Module, error)

    // Execute runs the given modules in order on the specified VM.
    // It updates ProvisionState as modules progress.
    Execute(ctx context.Context, vmName string, modules []Module) (*ProvisionState, error)

    // RunProbes executes readiness probes for modules that define them.
    // Blocks until all probes pass or any probe times out.
    RunProbes(ctx context.Context, vmName string, modules []Module) error

    // State returns the current provisioning state for a VM.
    State(vmName string) (*ProvisionState, error)
}
```

### Module Discovery and Loading

1. **Built-in modules** are loaded from `//go:embed` resources under `internal/provision/modules/`.
2. **Custom modules** are loaded from `~/.sd/provisions/*.yaml`.
3. All modules are validated at load time: schema correctness, dependency references, name uniqueness, and checksum presence for download modules.
4. If a custom module name conflicts with a built-in module name, loading fails with a fatal error.

### Script Execution Pipeline

For each module, in dependency order:

1. Set module status to `running`.
2. For each script in the module's `scripts` list (in order):
   a. Prepend `set -eux -o pipefail\n` to the script content.
   b. If `mode: system`, execute via `ssh <vm> sudo bash -c '<script>'`.
   c. If `mode: user`, execute via `ssh <vm> bash -c '<script>'`.
   d. If the script exits non-zero, set module status to `failed` and abort provisioning.
3. Set module status to `completed`.

### Checksum Verification

Built-in modules that download binaries MUST follow this pattern:

```bash
# Download to temp file
curl -fsSL "https://example.com/tool-v1.0.tar.gz" -o /tmp/tool.tar.gz

# Verify SHA-256 checksum (expected checksum from module's checksums field)
echo "${EXPECTED_CHECKSUM}  /tmp/tool.tar.gz" | sha256sum -c -

# Install only after verification
tar -C /usr/local -xzf /tmp/tool.tar.gz
rm /tmp/tool.tar.gz
```

The `curl | sh` anti-pattern MUST NOT be used in any built-in module.

### Credential Injection

Credentials are injected into the VM at session start, not at provisioning time. The provisioning system configures the mechanism but does not persist secrets:

- **ANTHROPIC_API_KEY**: Read from the host's environment. Passed to the VM via `ssh -o SendEnv` or equivalent mechanism at session start. The provisioning step configures `AcceptEnv` in the VM's sshd config.
- **GitHub token**: Configured via a git credential helper script that reads from an environment variable (`GH_TOKEN`). The helper script is written to disk; the token value is not. Before setting the custom helper, `credential.helper ""` is run to clear any default credential helpers, and a check is performed to ensure `~/.git-credentials` does not exist.

### File Layout

```
internal/provision/
    modules/              # Built-in module YAML files (embedded)
        base.yaml
        claude-code.yaml
        docker.yaml
        golang.yaml
        rust.yaml
        python.yaml
        github-cli.yaml
    embed.go              # //go:embed directives
    module.go             # Module type definitions and validation
    resolver.go           # Dependency resolution (topological sort)
    provisioner.go        # Provisioner implementation
    probe.go              # Readiness probe execution
    checksum.go           # Checksum verification logic
```

### CLI Surface

```
sd create --modules <comma-separated-list>   # Select modules during VM creation
sd provision <vm>                            # Re-provision all configured modules
sd provision <vm> --modules <list>           # Re-provision specific modules
sd provision list                            # List all available modules
sd provision list --json                     # List modules in JSON format
sd status <vm>                               # Show provisioning progress
```

### Example Built-in Module: golang.yaml

```yaml
name: golang
description: "Go toolchain (current stable release)"
depends_on:
  - base
scripts:
  - mode: system
    script: |
      if command -v go >/dev/null 2>&1; then
        echo "Go already installed: $(go version)"
        exit 0
      fi
      GO_VERSION="1.23.4"
      ARCH="$(dpkg --print-architecture)"
      curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" \
        -o /tmp/go.tar.gz
      echo "${GO_CHECKSUM}  /tmp/go.tar.gz" | sha256sum -c -
      rm -rf /usr/local/go
      tar -C /usr/local -xzf /tmp/go.tar.gz
      rm /tmp/go.tar.gz
  - mode: user
    script: |
      grep -q '/usr/local/go/bin' ~/.profile 2>/dev/null || \
        echo 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin' >> ~/.profile
checksums:
  go1.23.4.linux-arm64.tar.gz: "16e5017863a7f6071363571571ee35fcdacf27bc0569a9a1e4be76e3a51e00a4"
  go1.23.4.linux-amd64.tar.gz: "6924efde5de86fe277676e929dc9917d466efa02fb934197bc2eba35d5680971"
probe:
  command: "go version"
  interval: 5s
  timeout: 2m
```

### Example Custom Module

```yaml
# ~/.sd/provisions/custom-tool.yaml
name: custom-tool
description: "Install my custom tool"
depends_on:
  - base
scripts:
  - mode: system
    script: |
      apt-get install -y libfoo-dev
  - mode: user
    script: |
      cargo install custom-tool
probe:
  command: "command -v custom-tool"
  interval: 5s
  timeout: 5m
```

## Error Handling

| Condition | Severity | User Message | Recovery |
|-----------|----------|-------------|----------|
| Module YAML parse failure | Fatal | `error: failed to parse module "<name>": <parse error>` | Fix the YAML syntax in the module file |
| Missing dependency in `depends_on` | Fatal | `error: module "<name>" depends on unknown module "<dep>"` | Add the missing module or fix the dependency name |
| Circular dependency detected | Fatal | `error: circular dependency detected: <A> -> <B> -> <A>` | Remove the cycle from module dependencies |
| Custom module name conflicts with built-in | Fatal | `error: custom module "<name>" conflicts with built-in module` | Rename the custom module |
| Provisioning script exits non-zero | Fatal | `error: module "<name>" script failed (exit <code>):\n<last 20 lines of output>` | Fix the script or the VM state, then re-provision |
| Readiness probe timeout | Fatal | `error: module "<name>" readiness probe timed out after <duration>` | Check the VM's state manually; re-provision if needed |
| Checksum verification failure | Fatal | `error: checksum mismatch for "<filename>": expected <expected>, got <actual>` | Verify the download URL and update checksums if the upstream release has changed |
| ANTHROPIC_API_KEY not set on host | Warning | `warning: ANTHROPIC_API_KEY not set; Claude Code will not be functional until the key is provided` | Set the env var on the host and reconnect |
| GitHub token not provided | Warning | `warning: no GitHub token configured; authenticated git operations will fail` | Configure a token via `sd config` |
| VM not running (for `sd provision`) | Fatal | `error: VM "<name>" is not running; start it with "sd start <name>"` | Start the VM first |
| `--modules` references unknown module | Fatal | `error: unknown module "<name>"; run "sd provision list" to see available modules` | Use a valid module name |

All errors MUST be serializable to JSON when `--json` is active. The JSON error format:

```json
{
  "error": {
    "code": "provision_script_failed",
    "message": "module \"golang\" script failed (exit 1)",
    "module": "golang",
    "details": "last 20 lines of script output..."
  }
}
```

## Security Considerations

### Trust Boundaries

Provisioning scripts run inside the VM guest. `system` mode scripts run as root within the guest. This is acceptable because the VM itself is the trust boundary -- the guest has no access to host resources beyond what is explicitly configured.

### Credential Handling

- **ANTHROPIC_API_KEY** and **GitHub tokens** MUST NOT be written to disk inside the VM. They are injected via environment variables at session start time.
- The git credential helper is a script on disk that reads the token from an environment variable; the token value itself is never on disk.
- Pre-existing credential helpers are cleared (`credential.helper ""`) and `~/.git-credentials` is verified to not exist.
- Custom module scripts run with the same trust level as built-in modules. Users are responsible for reviewing custom module scripts they add.

### Blast Radius

- A compromised provisioning script can modify anything inside the VM guest. This is mitigated by the VM isolation boundary.
- A malicious custom module could install a backdoor inside the VM. Mitigation: custom modules are user-authored and stored in the user's config directory (`~/.sd/provisions/`), so this requires the user's filesystem to already be compromised.
- Built-in modules are embedded in the binary and cannot be tampered with at runtime.

### Supply Chain

- Built-in modules download tools from upstream sources (go.dev, nodejs.org, etc.) over HTTPS. The `base` module installs `ca-certificates` to ensure TLS verification.
- Built-in modules that download binaries MUST verify SHA-256 checksums (REQ-006-016). Checksums are embedded in the module YAML within the binary, making them tamper-resistant.
- The `curl | sh` anti-pattern is prohibited in built-in modules to prevent execution of unverified code.

## Testing Strategy

### Unit Tests

| Requirement | Test |
|-------------|------|
| REQ-006-003 | Parse valid and invalid module YAML; verify validation errors |
| REQ-006-004 | Dependency resolution with linear chains, diamonds, and cycles |
| REQ-006-007 | Custom module discovery from a temporary directory |
| REQ-006-014 | Embedded modules load correctly from `//go:embed` |
| REQ-006-015 | CLI flag parsing for `--modules` with various inputs |
| REQ-006-016 | Checksum verification: correct checksum passes, incorrect fails |

### Integration Tests

| Requirement | Test |
|-------------|------|
| REQ-006-001 | Provision a VM with each built-in module; verify tool is installed |
| REQ-006-002 | Provision with `--modules docker`; verify `base` packages are present |
| REQ-006-005 | Verify `system` scripts run as root, `user` scripts run as non-root |
| REQ-006-006 | Run provisioning twice; verify idempotent result |
| REQ-006-008 | Define a module with a probe; verify `sd create` blocks until probe passes |
| REQ-006-010 | Run `sd provision` on a running VM; verify modules execute |
| REQ-006-011 | Provision `claude-code` module; verify `claude` command is available |
| REQ-006-012 | Provision with git config; verify `git config` values, credential helper setup, and clone works |
| REQ-006-016 | Provision golang module; verify checksum is validated before installation |

### Script Tests

| Requirement | Test |
|-------------|------|
| REQ-006-009 | Run `sd status <vm> --json` during provisioning; verify JSON schema |
| REQ-006-010 | Run `sd provision <vm>` on a stopped VM; verify error message |
| REQ-006-015 | Run `sd create --modules all`; verify all modules are provisioned |
| REQ-006-013 | Clone a repo with CLAUDE.md; verify contents preserved after provisioning |

## Dependencies

### Depends On

- [001-architecture.md](001-architecture.md) -- VM backend interface for executing commands inside VMs
- [002-cli.md](002-cli.md) -- Command structure, `--json` flag, output conventions
- [003-vm-backend.md](003-vm-backend.md) -- VM lifecycle (create, start, stop) and SSH access
- [004-security.md](004-security.md) -- Credential injection mechanism, bypass permission definitions

### Depended On By

- [007-connection.md](007-connection.md) -- Connection spec depends on provisioning for tmux and sshd_config setup
- (Future specs covering session management and agent orchestration will depend on provisioning being complete)

## Open Questions

- OQ1: Should built-in module versions (e.g., Go 1.23.4) be pinned in the embedded YAML, or resolved to "latest stable" at provision time? Pinning is more reproducible; resolving is less maintenance. Current draft pins versions.
- OQ2: Should there be a `sd provision --dry-run` that shows what would be executed without running anything?
- OQ3: Should provisioning support a `--parallel` flag to run independent modules concurrently, or is sequential execution sufficient for v1?

## Revision History

| Date       | Author | Change Description |
|------------|--------|--------------------|
| 2026-03-27 | claude | Initial draft      |
| 2026-03-27 | claude | Review fixes: use "modules" consistently (remove "profile" references); add REQ-006-016 for checksum verification; add checksums field to module YAML schema; fix curl\|sh patterns; fix dependency reference from 005-security to 004-security; use `sd provision list` subcommand syntax; add credential.helper clearing and ~/.git-credentials check for git setup |
