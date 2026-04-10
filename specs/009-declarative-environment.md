# 009: Declarative Environment Configuration

| Field        | Value      |
|--------------|------------|
| Status       | draft      |
| Created      | 2026-04-09 |
| Last Updated | 2026-04-09 |
| Authors      | claude     |
| Reviewers    |            |

## Overview

This spec extends the `.sd.yaml` project configuration file and the provisioning pipeline to support declarative package installation, arbitrary setup commands, and repository cloning. Today, installing a tool like `jq` or `protobuf-compiler` requires creating a custom provisioning module YAML file. This spec introduces `packages`, `setup`, `repo`, and `branch` fields so that common environment requirements can be declared inline without writing module definitions. The new fields run after the existing module system completes, preserving full backward compatibility.

## Goals

- G1: Enable declarative package installation for apt, pip, npm, go, and cargo package managers directly in `.sd.yaml`
- G2: Support arbitrary setup commands that run after package installation
- G3: Support git repository cloning into the VM as part of provisioning
- G4: Validate runtime prerequisites (e.g., pip requires the python module) before attempting installation, with actionable error messages
- G5: Maintain backward compatibility with existing `.sd.yaml` files that lack the new fields
- G6: Ensure idempotent execution -- running provisioning twice produces the same end state
- G7: Extend `sd exec` to inject credentials the same way `sd connect` does, enabling scripted git clone operations

## Non-Goals

- NG1: Version pinning for packages (e.g., `jq=1.6`) -- package managers install the latest available version; version management is out of scope
- NG2: Lockfile generation or dependency resolution across package managers -- each manager resolves independently
- NG3: Replacing the module system -- modules handle complex multi-step installations (Go runtime, Claude Code); packages handle simple "install this tool" declarations
- NG4: Custom package manager support beyond the five defined (apt, pip, npm, go, cargo) -- additional managers may be added in future specs
- NG5: Package removal or uninstallation -- declarations are additive only
- NG6: Running setup commands as root -- all setup commands run as the default non-root user; use the module system for root-level setup

## Requirements

### REQ-009-001: Packages Field in .sd.yaml

The `.sd.yaml` file MUST support a `packages` field containing per-manager package lists. The following package managers MUST be supported:

| Key     | Package Manager Command         | Execution Mode |
|---------|---------------------------------|----------------|
| `apt`   | `sudo apt-get install -y`       | system (root)  |
| `pip`   | `pip3 install --user`           | user           |
| `npm`   | `npm install -g`                | user           |
| `go`    | `go install`                    | user           |
| `cargo` | `cargo install`                 | user           |

The `packages` field MUST be optional. When absent, no package installation occurs. Each sub-key (`apt`, `pip`, `npm`, `go`, `cargo`) MUST be optional. An empty list under a sub-key MUST be treated the same as the sub-key being absent.

```yaml
packages:
  apt:
    - jq
    - protobuf-compiler
    - postgresql-client
  pip:
    - black
    - mypy
  npm:
    - prettier
  go:
    - google.golang.org/protobuf/cmd/protoc-gen-go@latest
  cargo:
    - ripgrep
```

**Acceptance criteria:**
- [ ] A `.sd.yaml` with a `packages` field containing all five sub-keys parses without error
- [ ] A `.sd.yaml` with a `packages` field containing only a subset of sub-keys parses without error
- [ ] A `.sd.yaml` without a `packages` field parses without error and no packages are installed
- [ ] An empty sub-key (e.g., `apt: []`) is treated the same as the sub-key being absent
- [ ] Existing `.sd.yaml` files without the `packages` field continue to work without modification

### REQ-009-002: Setup Field in .sd.yaml

The `.sd.yaml` file MUST support a `setup` field containing an ordered list of shell commands. Each command MUST be a non-empty string. Commands MUST execute sequentially in the order listed, as the VM's default non-root user.

```yaml
setup:
  - echo 'export GOPATH=$HOME/go' >> ~/.profile
  - mkdir -p ~/bin
  - git config --global core.editor vim
```

The `setup` field MUST be optional. When absent, no setup commands run.

**Acceptance criteria:**
- [ ] A `.sd.yaml` with a `setup` field containing multiple commands parses without error
- [ ] Setup commands execute in the order they appear in the list
- [ ] Setup commands run as the default non-root user (not root)
- [ ] A `.sd.yaml` without a `setup` field parses without error and no setup commands run
- [ ] An empty string in the `setup` list produces a validation error

### REQ-009-003: Repo and Branch Fields in .sd.yaml

The `.sd.yaml` file MUST support `repo` and `branch` fields for git repository cloning.

- `repo`: A git repository URL. MUST be an HTTPS URL (`https://`) or SSH URL (`git@`). When present, the repository MUST be cloned into `~/projects/<name>` inside the VM, where `<name>` is the VM name from the `.sd.yaml` `name` field.
- `branch`: An optional git branch name. When present, the clone MUST check out the specified branch. When absent, the clone MUST use the repository's default branch.

```yaml
repo: https://github.com/user/my-app.git
branch: main
```

Both fields MUST be optional. When `repo` is absent, no clone operation occurs. When `branch` is present but `repo` is absent, a validation error MUST be produced.

**Acceptance criteria:**
- [ ] A `.sd.yaml` with `repo` and `branch` fields parses without error
- [ ] A `.sd.yaml` with `repo` but no `branch` parses without error; the default branch is used
- [ ] A `.sd.yaml` without `repo` parses without error; no clone occurs
- [ ] A `.sd.yaml` with `branch` but no `repo` produces a validation error
- [ ] An HTTPS URL (`https://github.com/user/repo.git`) is accepted for `repo`
- [ ] An SSH URL (`git@github.com:user/repo.git`) is accepted for `repo`
- [ ] An invalid URL (e.g., `not-a-url`) for `repo` produces a validation error
- [ ] The repository is cloned into `~/projects/<vm-name>` inside the VM

### REQ-009-004: Provisioning Pipeline Extension

The provisioning pipeline MUST execute the new declarative steps after all existing provisioning modules complete. The extended pipeline MUST follow this order:

1. **Existing module provisioning** (as defined in spec 006)
2. **Prerequisite validation** (REQ-009-005)
3. **Package installation** (REQ-009-006)
4. **Setup commands** (REQ-009-007)
5. **Repository clone** (REQ-009-008)

Each step MUST be fail-fast. If any step fails, all subsequent steps MUST be skipped and the provisioning MUST report the failure.

**Acceptance criteria:**
- [ ] Modules complete before any package installation begins
- [ ] Package installation completes before any setup command runs
- [ ] Setup commands complete before repository cloning begins
- [ ] A failure in package installation prevents setup commands and repo clone from running
- [ ] A failure in a setup command prevents repo clone from running
- [ ] When no `packages`, `setup`, or `repo` fields are present, the pipeline behaves identically to the existing module-only pipeline

### REQ-009-005: Runtime Prerequisite Validation

Before attempting package installation, the system MUST validate that required runtimes are available inside the VM. Validation MUST run inside the VM via command execution (as specified in REQ-003-007).

| Package Type | Required Runtime | Validation Command | Error Message |
|-------------|-----------------|-------------------|---------------|
| `apt`       | apt-get         | (always available) | N/A -- Ubuntu base image always has apt |
| `pip`       | python3 + pip   | `which pip3`       | `pip packages require the "python" module -- add it to your modules list` |
| `npm`       | node + npm      | `which npm`        | `npm packages require the "claude-code" or a Node.js module -- add one to your modules list` |
| `go`        | go binary       | `which go`         | `go packages require the "golang" module -- add it to your modules list` |
| `cargo`     | cargo binary    | `which cargo`      | `cargo packages require the "rust" module -- add it to your modules list` |

Validation MUST only check prerequisites for package types that have entries in the `packages` field. If `packages.pip` is absent or empty, no pip prerequisite check occurs.

**Acceptance criteria:**
- [ ] Requesting pip packages without the python module installed produces a fatal error with the documented message
- [ ] Requesting go packages without the golang module installed produces a fatal error with the documented message
- [ ] Requesting npm packages without node/npm installed produces a fatal error with the documented message
- [ ] Requesting cargo packages without the rust module installed produces a fatal error with the documented message
- [ ] Prerequisite validation runs inside the VM, not on the host
- [ ] Prerequisite validation runs before any package installation begins
- [ ] When no packages are declared, prerequisite validation is skipped entirely

### REQ-009-006: Package Installation Execution

Package installation MUST execute in a fixed order: apt, pip, npm, go, cargo. Within each package manager, all declared packages MUST be installed in a single command invocation where the package manager supports it.

The following commands MUST be used:

| Package Type | Command Template |
|-------------|-----------------|
| `apt`       | `sudo apt-get update && sudo apt-get install -y <packages>` |
| `pip`       | `pip3 install --user <packages>` |
| `npm`       | `npm install -g <packages>` |
| `go`        | `go install <package>` (one invocation per package) |
| `cargo`     | `cargo install <packages>` |

All commands MUST run with `set -eux -o pipefail` prepended, consistent with provisioning module script execution (REQ-006-005). The `apt` command MUST run as root. The `pip`, `npm`, `go`, and `cargo` commands MUST run as the default non-root user.

**Acceptance criteria:**
- [ ] apt packages are installed before pip packages
- [ ] pip packages are installed before npm packages
- [ ] npm packages are installed before go packages
- [ ] go packages are installed before cargo packages
- [ ] Multiple apt packages are combined into a single `apt-get install` invocation
- [ ] Multiple pip packages are combined into a single `pip3 install` invocation
- [ ] Each go package is installed with a separate `go install` invocation
- [ ] A failed package installation exits with a non-zero status and an actionable error message
- [ ] `apt-get update` runs before `apt-get install`

### REQ-009-007: Setup Command Execution

Each command in the `setup` list MUST be executed sequentially inside the VM as the default non-root user. Each command MUST run with `set -eux -o pipefail` prepended, consistent with provisioning module script execution (REQ-006-005).

**Acceptance criteria:**
- [ ] Setup commands run in the order they appear in `.sd.yaml`
- [ ] A setup command that exits non-zero causes provisioning to fail with the command text and exit code in the error message
- [ ] Setup commands run as the default non-root user
- [ ] All setup commands run after all package installation completes

### REQ-009-008: Repository Clone Execution

When the `repo` field is present, the system MUST clone the repository into `~/projects/<vm-name>` inside the VM. The clone MUST use the credentials injected by `sd exec` (per REQ-007-019 in spec 007-connection.md). If the `branch` field is present, the clone MUST check out the specified branch.

If the target directory already exists and contains a git repository with the same remote URL, the clone step MUST skip cloning and instead run `git fetch` and `git checkout <branch>` (if branch is specified). This provides idempotency for re-provisioning.

**Acceptance criteria:**
- [ ] The repository is cloned into `~/projects/<vm-name>` inside the VM
- [ ] When `branch` is specified, the clone checks out that branch
- [ ] When `branch` is not specified, the repository's default branch is used
- [ ] A clone failure (e.g., invalid URL, auth failure) produces a fatal error with an actionable message
- [ ] If `~/projects/<vm-name>` already exists with the correct remote, the clone is skipped and `git fetch` runs instead
- [ ] The clone uses credentials injected via environment variables, not persisted to disk

### REQ-009-009: Credential Availability in sd exec

The repository clone step (REQ-009-008) requires credentials to be available inside the VM during `sd exec` invocations. This is provided by the existing credential injection mechanism defined in spec 007-connection.md (REQ-007-019), which specifies that both `sd connect` and `sd exec` inject environment variables via the SSH `SendEnv`/`AcceptEnv` mechanism.

**Note:** If the current `sd exec` implementation does not inject credentials, that is an implementation gap against REQ-007-019, not a new requirement. This spec depends on REQ-007-019 being correctly implemented.

**Acceptance criteria:**
- [ ] `sd exec <vm> -- git clone <private-repo>` succeeds when a valid GitHub token is configured on the host
- [ ] Credentials are passed via environment variables, not written to files inside the VM

### REQ-009-010: Idempotency Guarantees

All declarative environment steps MUST be idempotent. Running provisioning twice on the same VM with the same `.sd.yaml` MUST produce the same end state as running it once.

The following idempotency properties MUST hold:

- **apt packages**: `apt-get install -y` is inherently idempotent for already-installed packages
- **pip packages**: `pip3 install --user` is inherently idempotent for already-installed packages
- **npm packages**: `npm install -g` is inherently idempotent for already-installed packages
- **go packages**: `go install` re-downloads and overwrites; this is acceptable
- **cargo packages**: `cargo install` skips packages that are already installed at the requested version
- **setup commands**: Setup commands are NOT inherently idempotent. Users MUST write idempotent commands (e.g., using `grep -q` guards or `mkdir -p`). The system SHOULD document this requirement in `sd init` template comments.
- **repo clone**: The clone step MUST check for an existing clone and skip if present (REQ-009-008)

**Acceptance criteria:**
- [ ] Running provisioning twice with the same `.sd.yaml` succeeds both times
- [ ] The second provisioning run completes faster than the first (skipping already-installed packages)
- [ ] The `sd init` template includes a comment warning that setup commands should be idempotent

### REQ-009-011: Validation Rules for New Fields

The following validation rules MUST be enforced when loading `.sd.yaml`:

| Field | Rule | Error Message |
|-------|------|---------------|
| `repo` | Must be empty, or start with `https://` or `git@` | `invalid repo "<value>": must be an HTTPS URL (https://...) or SSH URL (git@...)` |
| `branch` | Must be empty, or a valid git branch name (no spaces, no `..`, no control characters) | `invalid branch "<value>": must be a valid git branch name` |
| `branch` without `repo` | `branch` requires `repo` | `branch is set but repo is not; branch requires a repo URL` |
| `packages.<type>` entries | Each entry must be a non-empty string | `packages.<type>[<index>]: must be a non-empty string` |
| `setup` entries | Each entry must be a non-empty string | `setup[<index>]: must be a non-empty string` |

Validation MUST run at `.sd.yaml` load time, before any provisioning begins.

**Acceptance criteria:**
- [ ] An HTTPS `repo` URL passes validation
- [ ] An SSH `repo` URL passes validation
- [ ] A `repo` value that is not a valid URL produces a validation error
- [ ] A `branch` value with spaces produces a validation error
- [ ] A `branch` set without `repo` produces a validation error
- [ ] An empty string in `packages.apt` produces a validation error
- [ ] An empty string in `setup` produces a validation error
- [ ] Validation errors include the file path and field path

### REQ-009-012: sd init Template Updates

The `sd init` command MUST be updated to include the new fields in the generated `.sd.yaml` template:

1. **repo**: Auto-detected from the git remote URL of the current directory (if available). If the current directory is a git repository with an `origin` remote, the remote URL MUST be used as the `repo` value.
2. **branch**: Omitted by default (uses repository default branch).
3. **packages**: Included as a commented-out section with examples for each package manager.
4. **setup**: Included as a commented-out section with example commands.

```yaml
# .sd.yaml -- Secure Dev project configuration
name: my-app
modules:
  - base
  - golang
repo: https://github.com/user/my-app.git

# Packages to install inside the VM (in addition to modules).
# Uncomment and add packages as needed.
# packages:
#   apt:
#     - jq
#   pip:
#     - black
#   npm:
#     - prettier
#   go:
#     - golang.org/x/tools/cmd/goimports@latest
#   cargo:
#     - ripgrep

# Setup commands run after packages are installed (as non-root user).
# Each command should be idempotent (safe to run multiple times).
# setup:
#   - mkdir -p ~/bin
```

**Acceptance criteria:**
- [ ] `sd init` in a git repository with an `origin` remote populates the `repo` field
- [ ] `sd init` in a directory without a git remote leaves `repo` absent
- [ ] The generated `.sd.yaml` includes a commented-out `packages` section with examples
- [ ] The generated `.sd.yaml` includes a commented-out `setup` section with examples
- [ ] The commented-out `setup` section includes a note about idempotency

### REQ-009-013: Failure Handling

All failures in the declarative environment pipeline MUST follow the fail-fast pattern. When a step fails:

1. The current step's error MUST be captured with the last 20 lines of output.
2. All subsequent steps MUST be skipped.
3. The error MUST be reported to the user with the step name, command that failed, exit code, and output tail.
4. When `--json` is active, the error MUST be serializable to the standard JSON error format.

**Acceptance criteria:**
- [ ] A failed `apt-get install` reports which package(s) failed and shows the last 20 lines of apt output
- [ ] A failed setup command reports the command text and exit code
- [ ] A failed `git clone` reports the repository URL and whether it was an authentication failure or network failure
- [ ] After a failure, subsequent pipeline steps do not execute
- [ ] `--json` error output includes `code`, `message`, `step`, and `details` fields

## Design

### ProjectConfig Struct Changes

```go
// ProjectConfig represents the .sd.yaml project configuration file.
// REQ-009-001, REQ-009-002, REQ-009-003
type ProjectConfig struct {
    Name        string         `yaml:"name,omitempty"`
    Backend     string         `yaml:"backend,omitempty"`
    CPUs        int            `yaml:"cpus,omitempty"`
    Memory      string         `yaml:"memory,omitempty"`
    Disk        string         `yaml:"disk,omitempty"`
    Modules     []string       `yaml:"modules,omitempty"`
    Mounts      []string       `yaml:"mounts,omitempty"`
    AllowEgress []string       `yaml:"allow_egress,omitempty"`
    Repo        string         `yaml:"repo,omitempty"`
    Branch      string         `yaml:"branch,omitempty"`
    Packages    *PackageConfig `yaml:"packages,omitempty"`
    Setup       []string       `yaml:"setup,omitempty"`
}

// PackageConfig declares packages to install via system and language
// package managers. Each field is optional; absent or empty lists
// result in no installation for that manager.
// REQ-009-001
type PackageConfig struct {
    Apt   []string `yaml:"apt,omitempty"`
    Pip   []string `yaml:"pip,omitempty"`
    Npm   []string `yaml:"npm,omitempty"`
    Go    []string `yaml:"go,omitempty"`
    Cargo []string `yaml:"cargo,omitempty"`
}
```

### Package Installation Commands

Each package manager maps to a specific command template. The system constructs and executes these commands inside the VM:

```go
// PackageManager defines how a package type is installed.
type PackageManager struct {
    Name           string   // "apt", "pip", "npm", "go", "cargo"
    PrereqCommand  string   // Command to check runtime availability ("which pip3")
    PrereqError    string   // Error message if prerequisite is missing
    InstallCommand string   // Command template ("sudo apt-get install -y")
    UpdateCommand  string   // Pre-install command ("sudo apt-get update"), empty if none
    Mode           string   // "system" or "user"
    BatchInstall   bool     // Whether packages can be combined in one invocation
}
```

The package managers MUST be defined as:

| Manager | PrereqCommand | InstallCommand | UpdateCommand | Mode | BatchInstall |
|---------|--------------|----------------|---------------|------|-------------|
| apt | (none) | `sudo apt-get install -y` | `sudo apt-get update` | system | true |
| pip | `which pip3` | `pip3 install --user` | (none) | user | true |
| npm | `which npm` | `npm install -g` | (none) | user | true |
| go | `which go` | `go install` | (none) | user | false |
| cargo | `which cargo` | `cargo install` | (none) | user | true |

### Provisioning Pipeline Extension

The declarative environment steps execute as a post-module phase in the existing provisioning pipeline. They are invoked after all module provisioning (REQ-006-004) and all module readiness probes (REQ-006-008) have completed.

```
[Module provisioning: base -> claude-code -> golang -> ...]
  |
  v
[Prerequisite validation: check pip3, npm, go, cargo as needed]
  |
  v
[Package installation: apt -> pip -> npm -> go -> cargo]
  |
  v
[Setup commands: command1 -> command2 -> ...]
  |
  v
[Repo clone: git clone <repo> ~/projects/<name>]
```

Each step uses the backend's `Exec` method (REQ-003-007) to run commands inside the VM.

### Repository Clone Logic

```
if repo is set:
    target = ~/projects/<vm-name>
    if target exists AND is a git repo AND has matching remote:
        git fetch
        if branch is set:
            git checkout <branch>
            git pull
    else if target exists AND is NOT a git repo:
        fail with "target directory exists but is not a git repository"
    else:
        if branch is set:
            git clone --branch <branch> <repo> <target>
        else:
            git clone <repo> <target>
```

### Credential Injection in sd exec

The `sd exec` command MUST be extended to resolve environment variable references from the VM's `env` configuration (as defined in REQ-005-008) and pass them to the remote command via the backend's `Exec` method. The mechanism for passing environment variables is backend-specific:

- **Lima backend**: Environment variables are passed via `limactl shell --set-env` flags.
- **Docker backend**: Environment variables are passed via `docker exec -e` flags.

Credential injection into `sd exec` invocations relies on the existing SSH `SendEnv`/`AcceptEnv` mechanism (REQ-007-019). The `Backend.Exec` interface (REQ-003-007) does NOT need modification -- credentials are injected at the SSH transport layer, not as a function parameter.

### Validation Logic

```go
// validateDeclarativeFields validates repo, branch, packages, and setup.
// REQ-009-011
func validateDeclarativeFields(cfg *ProjectConfig, path string) error {
    // repo must be HTTPS or SSH URL
    if cfg.Repo != "" {
        if !strings.HasPrefix(cfg.Repo, "https://") &&
           !strings.HasPrefix(cfg.Repo, "git@") {
            return fmt.Errorf("%s: invalid repo %q: "+
                "must be an HTTPS URL (https://...) or SSH URL (git@...)",
                path, cfg.Repo)
        }
    }

    // branch requires repo
    if cfg.Branch != "" && cfg.Repo == "" {
        return fmt.Errorf("%s: branch is set but repo is not; "+
            "branch requires a repo URL", path)
    }

    // branch must be a valid git branch name
    if cfg.Branch != "" {
        if strings.ContainsAny(cfg.Branch, " \t\n\r") ||
           strings.Contains(cfg.Branch, "..") {
            return fmt.Errorf("%s: invalid branch %q: "+
                "must be a valid git branch name", path, cfg.Branch)
        }
    }

    // Validate package entries
    if cfg.Packages != nil {
        for i, p := range cfg.Packages.Apt {
            if strings.TrimSpace(p) == "" {
                return fmt.Errorf("%s: packages.apt[%d]: "+
                    "must be a non-empty string", path, i)
            }
        }
        // ... same for Pip, Npm, Go, Cargo
    }

    // Validate setup entries
    for i, s := range cfg.Setup {
        if strings.TrimSpace(s) == "" {
            return fmt.Errorf("%s: setup[%d]: "+
                "must be a non-empty string", path, i)
        }
    }

    return nil
}
```

### sd init Template

The `sd init` command MUST generate a template that includes the new fields. When running in a git repository, it MUST detect the `origin` remote URL:

```go
func detectGitRemote(dir string) string {
    // Run: git -C <dir> remote get-url origin
    // Return the URL if successful, empty string otherwise
}
```

### File Layout Changes

```
internal/config/
    project.go            # Updated ProjectConfig struct, validation
internal/provision/
    declarative.go        # Package installation, setup, repo clone logic
    prerequisite.go       # Runtime prerequisite validation
internal/cmd/
    init.go               # Updated template with new fields
    exec.go               # Updated with credential injection
```

### CLI Surface Changes

No new commands. Existing commands gain new behavior:

```
sd create [name]            # Now also processes packages/setup/repo from .sd.yaml
sd ensure [name]            # Now also processes packages/setup/repo from .sd.yaml
sd exec <vm> -- <command>   # Now injects credentials from VM env config
sd init                     # Now includes repo/packages/setup in template
```

### Example .sd.yaml

```yaml
name: my-app
modules:
  - base
  - claude-code
  - golang

repo: https://github.com/user/my-app.git
branch: main

packages:
  apt:
    - jq
    - protobuf-compiler
    - postgresql-client
  pip:
    - black
    - mypy
  go:
    - google.golang.org/protobuf/cmd/protoc-gen-go@latest
    - google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

setup:
  - echo 'export GOPATH=$HOME/go' >> ~/.profile
  - mkdir -p ~/bin

```

## Error Handling

| Condition | Severity | User Message | Recovery |
|-----------|----------|-------------|----------|
| `repo` is not a valid URL | Fatal | `error: .sd.yaml: invalid repo "<value>": must be an HTTPS URL (https://...) or SSH URL (git@...)` | Fix the URL in .sd.yaml |
| `branch` set without `repo` | Fatal | `error: .sd.yaml: branch is set but repo is not; branch requires a repo URL` | Add a repo field or remove branch |
| `branch` is invalid | Fatal | `error: .sd.yaml: invalid branch "<value>": must be a valid git branch name` | Fix the branch name |
| Empty string in packages list | Fatal | `error: .sd.yaml: packages.<type>[<index>]: must be a non-empty string` | Remove the empty entry |
| Empty string in setup list | Fatal | `error: .sd.yaml: setup[<index>]: must be a non-empty string` | Remove the empty entry |
| pip prerequisite missing | Fatal | `error: pip packages require the "python" module -- add it to your modules list` | Add `python` to the modules list |
| npm prerequisite missing | Fatal | `error: npm packages require the "claude-code" or a Node.js module -- add one to your modules list` | Add a module that provides Node.js |
| go prerequisite missing | Fatal | `error: go packages require the "golang" module -- add it to your modules list` | Add `golang` to the modules list |
| cargo prerequisite missing | Fatal | `error: cargo packages require the "rust" module -- add it to your modules list` | Add `rust` to the modules list |
| apt-get install failure | Fatal | `error: package installation failed (apt): exit code <N>\n<last 20 lines of output>` | Fix the package name or check VM network connectivity |
| pip/npm/go/cargo install failure | Fatal | `error: package installation failed (<manager>): exit code <N>\n<last 20 lines of output>` | Fix the package name or check prerequisites |
| Setup command failure | Fatal | `error: setup command failed (exit <N>): "<command>"\n<last 20 lines of output>` | Fix the command in .sd.yaml |
| Git clone auth failure | Fatal | `error: git clone failed for "<repo>": authentication required. Configure a GitHub token via the VM's env section.` | Add GITHUB_TOKEN to VM env configuration |
| Git clone network failure | Fatal | `error: git clone failed for "<repo>": <git error>\n<last 20 lines of output>` | Check the repo URL and VM network/egress settings |
| Clone target exists (not a git repo) | Fatal | `error: clone target ~/projects/<name> exists but is not a git repository` | Remove the directory manually or use a different VM name |
| Credential env var not set on host | Warning | `warning: environment variable <VAR> referenced in VM env config is not set on host; commands requiring authentication may fail` | Set the environment variable on the host |

All errors MUST be JSON-serializable when `--json` is active:

```json
{
  "ok": false,
  "error": {
    "code": "package_install_failed",
    "message": "package installation failed (apt): exit code 100",
    "step": "packages.apt",
    "details": "E: Unable to locate package nonexistent-pkg\n..."
  }
}
```

## Security Considerations

### Trust Boundaries

The declarative environment features operate within the existing VM trust boundary. All package installation, setup commands, and repository clones execute inside the guest VM, not on the host.

### Credential Handling

- **Repository cloning** requires a GitHub token (or equivalent) for private repositories. This token MUST be injected at runtime via environment variables, consistent with the credential injection mechanism in spec 007 (REQ-007-019, SendEnv/AcceptEnv) and the security requirement in spec 004 (REQ-004-011). The token MUST NOT be persisted to disk inside the VM.
- **Credential injection in `sd exec`** relies on the existing REQ-007-019 mechanism. Credentials flow from the host environment through environment variable references in the VM config, resolved at execution time.
- **Setup commands** run as the non-root user and have access to injected environment variables. A malicious `.sd.yaml` could craft setup commands that exfiltrate credentials. This is mitigated by the VM's egress allowlist (REQ-004-007) and the user's review of `.sd.yaml` before running `sd ensure`.

### Package Sources

- **apt packages** are installed from the VM's configured apt repositories (Ubuntu archive). Egress to `archive.ubuntu.com` and `security.ubuntu.com` is allowed by default.
- **pip, npm, go, cargo** packages are fetched from their respective registries. Egress to `pypi.org`, `registry.npmjs.org`, `proxy.golang.org`, and `crates.io` SHOULD be in the default egress allowlist to avoid installation failures. If `crates.io` is not in the default allowlist, a clear error will be produced, and the user can add it via `allow_egress`.
- **Setup commands** may download files from the internet. Downloads are subject to the egress allowlist.

### Blast Radius

- A malicious `.sd.yaml` (e.g., from a cloned repository) can specify arbitrary package names and setup commands. These execute inside the VM, which is the trust boundary. The VM's egress allowlist constrains what network access these commands have.
- Package manager commands can install arbitrary software inside the VM. This is intentional and the same trust model as the existing module system.
- Setup commands run as the non-root user, limiting the scope of system-level modifications.

### Mitigations

- The VM isolation boundary contains all execution.
- Egress allowlist (spec 004) constrains network access.
- Credentials are injected at runtime and never persisted to disk.
- Project-level `.sd.yaml` files are visible in version control for review.
- Security-sensitive settings (`security.*`) in `.sd.yaml` are ignored (REQ-005-017).

## Testing Strategy

### Unit Tests

| Requirement | Test Description |
|-------------|-----------------|
| REQ-009-001 | Parse `.sd.yaml` with packages field: all sub-keys, subset, absent, empty lists |
| REQ-009-002 | Parse `.sd.yaml` with setup field: multiple commands, absent, empty strings (error) |
| REQ-009-003 | Parse `.sd.yaml` with repo/branch: HTTPS URL, SSH URL, invalid URL (error), branch without repo (error) |
| REQ-009-011 | Validation rules for each field: repo URL format, branch validity, empty entries |
| REQ-009-006 | Package manager command construction: verify correct command templates for each manager type |
| REQ-009-006 | Batch vs per-package behavior: apt/pip/npm/cargo batch, go per-package |
| REQ-009-010 | Idempotency: repo clone logic when target directory exists with correct remote |

### Integration Tests

| Requirement | Test Description |
|-------------|-----------------|
| REQ-009-004 | Full pipeline: modules -> prerequisite check -> packages -> setup -> repo clone |
| REQ-009-005 | Prerequisite validation with missing runtimes: provision VM without golang module, declare go packages, verify error |
| REQ-009-006 | Package installation with a real VM: install apt packages, verify they are present |
| REQ-009-007 | Setup command execution: run commands in VM, verify effects |
| REQ-009-008 | Repository clone: clone a public repo into VM, verify directory and branch |
| REQ-009-009 | Credential injection in `sd exec`: configure GITHUB_TOKEN, verify it appears in `sd exec <vm> -- env` |
| REQ-009-010 | Idempotency: run provisioning twice, verify second run succeeds and is faster |
| REQ-009-013 | Failure propagation: trigger a package install failure, verify subsequent steps are skipped |

### Script Tests

| Requirement | Test Description |
|-------------|-----------------|
| REQ-009-012 | `sd init` in a git repo produces `.sd.yaml` with `repo` field populated from `origin` |
| REQ-009-012 | `sd init` in a non-git directory produces `.sd.yaml` without `repo` field |
| REQ-009-012 | Generated `.sd.yaml` includes commented-out packages and setup sections |
| REQ-009-013 | `--json` error output for package install failure includes `code`, `message`, `step`, `details` |

## Dependencies

### Depends On

- [005-configuration.md](005-configuration.md) -- `.sd.yaml` format, config loading (REQ-005-001), sensitive value handling (REQ-005-008), VM env config (REQ-005-007)
- [006-provisioning.md](006-provisioning.md) -- module system (REQ-006-001 through REQ-006-016), script execution environment (REQ-006-005), module dependency ordering (REQ-006-004)
- [007-connection.md](007-connection.md) -- credential injection in `sd exec` (REQ-007-019), required for repo clone
- [003-vm-backend.md](003-vm-backend.md) -- `Exec` method for running commands inside VMs (REQ-003-007)
- [004-security.md](004-security.md) -- egress allowlist defaults (REQ-004-007), credential injection model
- [002-cli.md](002-cli.md) -- `--json` output conventions, `sd exec` command definition

**Note:** This spec references `sd ensure` and `sd init`, which exist in the implementation but are not yet defined in spec 002-cli.md. Spec 002 must be updated with these commands before this spec is approved.

### Depended On By

- (Future spec: agent-driven bootstrap / `sd quick-start`) -- the quick-start runbook depends on declarative environment configuration for automated environment setup

## Open Questions

- OQ1: Should `crates.io` and `static.crates.io` be added to the default egress allowlist? Currently they are not listed in spec 004's defaults, which means `cargo install` will fail unless the user adds them via `allow_egress`.
- OQ2: Should there be a `--dry-run` flag on `sd ensure` / `sd create` that shows what packages, setup commands, and repo clones would execute without actually running them?
- OQ3: Should `go install` packages support version pinning beyond the `@latest` / `@v1.2.3` syntax already supported by `go install`? Current design delegates version syntax entirely to the `go install` command.

## Revision History

| Date       | Author | Change Description |
|------------|--------|--------------------|
| 2026-04-09 | claude | Initial draft      |
