# 005: Configuration System

| Field        | Value      |
|--------------|------------|
| Status       | draft      |
| Created      | 2026-03-27 |
| Last Updated | 2026-03-27 |
| Authors      | claude     |
| Reviewers    |            |

## Overview

This spec defines how `sd` loads, merges, validates, and exposes configuration. Configuration flows from five sources in a strict precedence order: CLI flags, environment variables, project-level config, user-level config, and built-in defaults. The system uses Viper for config loading and merging, YAML for file format, and provides a `sd config` command family for inspection and mutation. Sensitive values (API keys, tokens) are never stored literally in config files; they are expressed as environment variable references and resolved at runtime.

## Goals

- G1: Provide a layered configuration system with clear, predictable precedence rules
- G2: Support per-project and per-user configuration files in YAML format
- G3: Enable environment-variable-based configuration for headless/CI use
- G4: Ensure sensitive values (credentials, tokens) are never persisted in config files
- G5: Provide CLI commands to inspect, set, and validate configuration
- G6: Support per-VM configuration overrides and state tracking

## Non-Goals

- NG1: Secret management or vault integration (credentials are injected via host environment variables, not managed by `sd`)
- NG2: Remote/networked configuration sources (no etcd, consul, etc.)
- NG3: Configuration file encryption at rest
- NG4: GUI or TUI configuration editors (the `sd config edit` command defers to `$EDITOR`)
- NG5: Automatic config migration between versions (may be addressed in a future spec)

## Requirements

### REQ-005-001: Configuration Precedence Order

The configuration system MUST resolve values using the following precedence order, from highest to lowest:

1. CLI flags (e.g., `--cpus 8`)
2. Environment variables (e.g., `SD_BACKEND=lima`)
3. Project-level config file (`.sd/config.yaml` in current directory or nearest parent)
4. User-level config file (`~/.sd/config.yaml`, or `$SD_HOME/config.yaml` if `SD_HOME` is set)
5. Built-in defaults compiled into the binary

A higher-precedence source MUST always override a lower-precedence source for the same key.

**Acceptance criteria:**
- [ ] Setting a value via CLI flag overrides the same value set in all other sources
- [ ] Setting a value via environment variable overrides project-level, user-level, and default values
- [ ] Project-level config overrides user-level config and defaults
- [ ] User-level config overrides defaults only
- [ ] When no source provides a value, the built-in default is used
- [ ] `sd config list` displays the active source for each resolved value

### REQ-005-002: User-Level Config File

The system MUST look for a user-level config file at `$SD_HOME/config.yaml`, where `SD_HOME` defaults to `~/.sd` if not set.

**Acceptance criteria:**
- [ ] `~/.sd/config.yaml` is loaded when `SD_HOME` is not set
- [ ] `$SD_HOME/config.yaml` is loaded when `SD_HOME` is set to a custom path
- [ ] The file is optional; its absence is not an error
- [ ] The file MUST be valid YAML if it exists; invalid YAML produces a fatal error with the file path and parse error

### REQ-005-003: Project-Level Config File

The system MUST search for `.sd/config.yaml` starting from the current working directory and walking up to the filesystem root. The first match found MUST be used as the project-level config.

**Acceptance criteria:**
- [ ] `.sd/config.yaml` in the current directory is found
- [ ] `.sd/config.yaml` in a parent directory is found when not present in the current directory
- [ ] The search stops at the filesystem root
- [ ] Only the first (nearest) match is used; parent matches are ignored if a closer match exists
- [ ] The file is optional; its absence is not an error
- [ ] The file MUST be valid YAML if it exists; invalid YAML produces a fatal error

### REQ-005-004: Built-In Defaults

The system MUST provide the following built-in default values:

| Key                       | Default Value  |
|---------------------------|----------------|
| `defaults.backend`        | `lima`         |
| `defaults.cpus`           | `4`            |
| `defaults.memory`         | `8GiB`         |
| `defaults.disk`           | `100GiB`       |
| `defaults.image`          | `ubuntu:24.04` |
| `defaults.vm`             | (empty string) |
| `security.mount_policy`   | `none`         |
| `security.egress_allowlist` | see below    |

The default `security.egress_allowlist` MUST match the default egress allowlist defined in [004-security.md](004-security.md) (REQ-004-007):

- `api.anthropic.com`
- `github.com`
- `*.githubusercontent.com`
- `archive.ubuntu.com`
- `security.ubuntu.com`
- `deb.debian.org`
- `registry.npmjs.org`
- `pypi.org`
- `files.pythonhosted.org`
- `proxy.golang.org`
- `sum.golang.org`

**Acceptance criteria:**
- [ ] Each default value is returned when no other source provides the key
- [ ] Defaults are compiled into the binary (not loaded from an external file at runtime)
- [ ] All default values are documented in `sd config list` output
- [ ] The default egress allowlist matches the entries in spec 004

### REQ-005-005: Environment Variable Mapping

The system MUST map environment variables with the `SD_` prefix to configuration keys. The following specific mappings MUST be supported:

| Environment Variable | Config Key          | Description                                    |
|----------------------|---------------------|------------------------------------------------|
| `SD_HOME`            | (base directory)    | Base directory for all `sd` data (default: `~/.sd`) |
| `SD_BACKEND`         | `defaults.backend`  | Default VM backend                             |
| `SD_DEFAULT_VM`      | `defaults.vm`       | Default VM name for commands accepting `--vm`  |
| `SD_JSON`            | `output.json`       | When set to `true` or `1`, equivalent to `--json` on all commands |

Additionally, Viper's `AutomaticEnv` with the `SD_` prefix MUST be enabled, so that any config key can be overridden by setting `SD_<UPPER_SNAKE_CASE_KEY>` (e.g., `SD_DEFAULTS_CPUS=8`).

**Acceptance criteria:**
- [ ] `SD_HOME` changes the base directory used for user-level config and VM data
- [ ] `SD_BACKEND=docker` causes `defaults.backend` to resolve to `docker`
- [ ] `SD_DEFAULT_VM=myvm` causes `defaults.vm` to resolve to `myvm`
- [ ] `SD_JSON=true` causes all commands to produce JSON output without `--json` flag
- [ ] `SD_JSON=1` is treated the same as `SD_JSON=true`
- [ ] Nested keys are mapped using underscore separators: `SD_DEFAULTS_CPUS` maps to `defaults.cpus`

### REQ-005-006: Config File Format

Config files MUST use YAML format and support the following top-level sections: `defaults`, `security`, and `vms`.

```yaml
# ~/.sd/config.yaml
defaults:
  backend: lima
  cpus: 4
  memory: 8GiB
  disk: 100GiB
  image: ubuntu:24.04
  vm: ""

security:
  egress_allowlist:
    - api.anthropic.com
    - github.com
    - "*.githubusercontent.com"
    - registry.npmjs.org
    - pypi.org
  mount_policy: none  # none | readonly | project

vms:
  myvm:
    cpus: 8
    memory: 16GiB
    backend: lima
    provisions:
      - claude-code
      - docker
      - golang
    env:
      ANTHROPIC_API_KEY: "${ANTHROPIC_API_KEY}"
      GITHUB_TOKEN: "${GITHUB_TOKEN}"
```

**Acceptance criteria:**
- [ ] A config file with all three sections (`defaults`, `security`, `vms`) is parsed correctly
- [ ] A config file with only a subset of sections is parsed correctly; missing sections use defaults
- [ ] Unknown top-level keys produce a warning (not an error) to allow forward compatibility
- [ ] The `security.mount_policy` field accepts only `none`, `readonly`, or `project`; other values produce a validation error
- [ ] The `security.egress_allowlist` field accepts glob patterns (e.g., `*.githubusercontent.com`)
- [ ] VM definitions under `vms.<name>` inherit from `defaults` for any unspecified keys

### REQ-005-007: VM Definition Files

Each VM MUST have its own config file at `$SD_HOME/vms/<name>/config.yaml`. This file stores VM-specific overrides and state metadata managed by `sd`.

```yaml
# ~/.sd/vms/myvm/config.yaml
name: myvm
backend: lima
cpus: 8
memory: 16GiB
disk: 100GiB
image: ubuntu:24.04

provisions:
  - claude-code
  - docker
  - golang

env:
  ANTHROPIC_API_KEY: "${ANTHROPIC_API_KEY}"
  GITHUB_TOKEN: "${GITHUB_TOKEN}"

state:
  status: stopped       # created | running | stopped | error
  created_at: "2026-03-27T10:30:00Z"
  last_started: "2026-03-27T14:00:00Z"
  last_stopped: "2026-03-27T18:00:00Z"

backend_meta:
  lima_instance: sd-myvm
  pid: 0
```

**Acceptance criteria:**
- [ ] VM config is created at `$SD_HOME/vms/<name>/config.yaml` when a VM is created
- [ ] The `state` section is managed exclusively by `sd` and MUST NOT be manually edited (validated on load)
- [ ] The `backend_meta` section stores backend-specific data opaque to the config system
- [ ] VM-specific values override user-level `defaults` but not CLI flags or environment variables
- [ ] The `state.status` field accepts only `created`, `running`, `stopped`, or `error`
- [ ] Timestamps in the `state` section MUST use RFC 3339 format

### REQ-005-008: Sensitive Value Handling

Values in config files that reference host environment variables MUST use the syntax `${VAR_NAME}` or `$VAR_NAME`. These references MUST be resolved from the host environment at runtime, never stored as literal values.

**Acceptance criteria:**
- [ ] `"${ANTHROPIC_API_KEY}"` in config resolves to the value of the host `ANTHROPIC_API_KEY` environment variable at runtime
- [ ] `"$GITHUB_TOKEN"` (without braces) is also resolved
- [ ] If a referenced environment variable is not set on the host, resolution produces an empty string and a warning is emitted
- [ ] Literal values that happen to start with `$` but are not environment variable references can be escaped with `$$` (e.g., `$$LITERAL` resolves to `$LITERAL`)
- [ ] `sd config list` and `sd config get` MUST display environment variable references as `${VAR_NAME}` (unexpanded), never the resolved secret value
- [ ] `sd config list --resolve` MAY show resolved values, but MUST mask them (e.g., `sk-ant-...****`) unless `--unmask` is also passed
- [ ] Resolved values MUST NOT be written to any log file or debug output

### REQ-005-009: Config Get Command

`sd config get <key>` MUST display the resolved value for a given configuration key along with its source.

```
$ sd config get defaults.backend
lima (source: user-level config ~/.sd/config.yaml)

$ sd config get defaults.cpus --json
{
  "key": "defaults.cpus",
  "value": 4,
  "source": "built-in default"
}
```

**Acceptance criteria:**
- [ ] Dotted keys navigate nested config (e.g., `defaults.backend`, `security.mount_policy`)
- [ ] The source is displayed: one of `cli flag`, `environment variable`, `project-level config`, `user-level config`, `vm config`, or `built-in default`
- [ ] Non-existent keys produce a non-zero exit code and an error message
- [ ] `--json` output includes `key`, `value`, and `source` fields
- [ ] Sensitive values are displayed as their `${VAR}` reference, not resolved

### REQ-005-010: Config Set Command

`sd config set <key> <value>` MUST write the given key-value pair to the user-level config file.

```
$ sd config set defaults.cpus 8
Set defaults.cpus = 8 in ~/.sd/config.yaml

$ sd config set security.egress_allowlist '["api.anthropic.com","github.com"]'
Set security.egress_allowlist in ~/.sd/config.yaml
```

**Acceptance criteria:**
- [ ] The user-level config file is created if it does not exist
- [ ] The parent directory `$SD_HOME` is created with mode `0700` if it does not exist
- [ ] Existing keys are updated; new keys are added
- [ ] Dotted keys create nested YAML structure (e.g., `defaults.cpus` becomes `defaults: { cpus: 8 }`)
- [ ] Array values are accepted as JSON-encoded strings and stored as YAML lists
- [ ] The command MUST NOT modify project-level config; use `--project` flag to write to project-level config instead
- [ ] `--project` writes to `.sd/config.yaml` in the nearest project root (where `.sd/` already exists, or current directory)
- [ ] `--json` output confirms the key, value, file path, and whether the key was created or updated
- [ ] Validation runs after the write; if the resulting config is invalid, the write is rolled back and an error is returned

### REQ-005-011: Config List Command

`sd config list` MUST display all configuration values with their resolved sources.

```
$ sd config list
KEY                         VALUE           SOURCE
defaults.backend            lima            user-level config
defaults.cpus               4               built-in default
defaults.memory             8GiB            built-in default
defaults.disk               100GiB          built-in default
defaults.image              ubuntu:24.04    built-in default
defaults.vm                 (not set)       built-in default
security.mount_policy       none            built-in default
security.egress_allowlist   [11 entries]    built-in default
```

**Acceptance criteria:**
- [ ] All known configuration keys are listed, even if using defaults
- [ ] Each row shows the key, current value, and source
- [ ] Array values show a summary (e.g., `[11 entries]`); use `sd config get <key>` for full values
- [ ] `--json` output is an array of objects with `key`, `value`, and `source` fields
- [ ] Sensitive values show `${VAR}` references, not resolved values

### REQ-005-012: Config Edit Command

`sd config edit` MUST open the user-level config file in `$EDITOR` (or `$VISUAL`, falling back to `vi`).

**Acceptance criteria:**
- [ ] The command opens `$SD_HOME/config.yaml` in the editor specified by `$EDITOR`, then `$VISUAL`, then `vi`
- [ ] If the config file does not exist, it is created with a commented template before opening
- [ ] After the editor exits, the file is validated; if invalid, a warning is printed and the user is asked to re-edit or abort
- [ ] `--project` opens the project-level config instead
- [ ] This command is inherently interactive and SHOULD NOT be used by agents; it MUST print a warning if `SD_JSON=true` is set (indicating non-interactive context) and exit with an error

### REQ-005-013: Config Validate Command

`sd config validate` MUST validate all discoverable config files and report errors.

```
$ sd config validate
Validating ~/.sd/config.yaml ... ok
Validating .sd/config.yaml ... ok
Validating ~/.sd/vms/myvm/config.yaml ... ok
All config files valid.

$ sd config validate --json
{
  "valid": true,
  "files": [
    {"path": "~/.sd/config.yaml", "valid": true, "errors": []},
    {"path": ".sd/config.yaml", "valid": true, "errors": []},
    {"path": "~/.sd/vms/myvm/config.yaml", "valid": true, "errors": []}
  ]
}
```

**Acceptance criteria:**
- [ ] Validates the user-level config file if it exists
- [ ] Validates the project-level config file if found
- [ ] Validates all VM config files under `$SD_HOME/vms/*/config.yaml`
- [ ] Reports YAML parse errors with file path and line number
- [ ] Reports schema validation errors (invalid enum values, wrong types, unknown required fields)
- [ ] Exits with code 0 if all files are valid, code 1 if any file is invalid
- [ ] `--json` output includes per-file validation results

### REQ-005-014: Viper Integration

The configuration system MUST use `github.com/spf13/viper` for config file loading, environment variable binding, and CLI flag binding.

**Acceptance criteria:**
- [ ] `viper.SetConfigName("config")` and `viper.SetConfigType("yaml")` are used
- [ ] `viper.AddConfigPath` is called for both `$SD_HOME` and the project-level `.sd/` directory
- [ ] `viper.AutomaticEnv()` is enabled with `viper.SetEnvPrefix("SD")`
- [ ] `viper.SetEnvKeyReplacer` is configured to replace `.` with `_` so that `defaults.cpus` maps to `SD_DEFAULTS_CPUS`
- [ ] CLI flags are bound to Viper keys using `viper.BindPFlag`
- [ ] Viper's merge behavior is used to implement the precedence order defined in REQ-005-001
- [ ] A single global Viper instance is initialized once during application startup and is accessible via an exported `config` package function

### REQ-005-015: VM Config Inheritance

When resolving configuration for a specific VM, values MUST be resolved in the following order (highest precedence first):

1. CLI flags
2. Environment variables
3. VM-specific values from `$SD_HOME/vms/<name>/config.yaml`
4. VM-specific values from the `vms.<name>` section of the project-level or user-level config
5. Values from the `defaults` section of the project-level config
6. Values from the `defaults` section of the user-level config
7. Built-in defaults

**Acceptance criteria:**
- [ ] A VM with `cpus: 8` in its own config file overrides `defaults.cpus: 4` from the user config
- [ ] A VM defined in the `vms` section of the user config inherits unspecified values from `defaults`
- [ ] CLI flags override all config file values
- [ ] The resolved config for a VM can be inspected with `sd config get defaults.cpus --vm myvm`

### REQ-005-016: Config Directory Structure

The `sd` data directory (`$SD_HOME`, defaulting to `~/.sd`) MUST follow this layout:

```
~/.sd/
  config.yaml          # User-level config
  vms/
    <name>/
      config.yaml      # VM-specific config and state
```

**Acceptance criteria:**
- [ ] `$SD_HOME` is created with mode `0700` on first use
- [ ] `$SD_HOME/vms/` is created with mode `0700` when the first VM is created
- [ ] VM directories are created with mode `0700`
- [ ] No files or directories outside `$SD_HOME` are created by the config system (project-level `.sd/` directories are created by other commands)

### REQ-005-017: Security-Sensitive Keys in Project-Level Config

Security-sensitive configuration keys (all keys under the `security.*` namespace) in project-level config files MUST be ignored. Only user-level config, CLI flags, and environment variables can set security settings. This prevents a malicious project-level `.sd/config.yaml` from weakening security policy.

**Acceptance criteria:**
- [ ] A project-level config with `security.mount_policy: project` is ignored; the user-level or default value is used instead
- [ ] A project-level config with `security.egress_allowlist` entries is ignored; user-level or default allowlist is used instead
- [ ] A warning is emitted when security keys are found in project-level config: `warning: security settings in project-level config are ignored (found: security.mount_policy); set these in ~/.sd/config.yaml or via CLI flags`
- [ ] `sd config validate` reports security keys in project-level config as a warning

### REQ-005-018: Mount Policy Values

The `security.mount_policy` setting controls how host directories may be mounted into VMs. The following values are defined:

| Value      | Behavior                                                                                      |
|------------|-----------------------------------------------------------------------------------------------|
| `none`     | No host directories are mounted. This is the default and most secure setting.                  |
| `readonly` | The current working directory is mounted into the VM as read-only at a conventional guest path. |
| `project`  | A specific project directory (the directory containing `.sd/config.yaml`) is mounted as writable at a conventional guest path. |

Mount validation rules from [004-security.md](004-security.md) (REQ-004-004, REQ-004-005) apply regardless of mount policy. Sensitive paths (e.g., `~/.ssh`, `~/.aws`) are always rejected even when `mount_policy` is `project`.

**Acceptance criteria:**
- [ ] `mount_policy: none` results in no mounts being configured in VMConfig
- [ ] `mount_policy: readonly` mounts the current working directory as read-only
- [ ] `mount_policy: project` mounts the project root (directory containing `.sd/config.yaml`) as writable
- [ ] All mount paths are validated against the sensitive path rules in spec 004
- [ ] An invalid `mount_policy` value produces a validation error listing the valid options

## Design

### Configuration Types

```go
package config

import "time"

// Config represents the fully resolved configuration.
type Config struct {
    Defaults Defaults          `yaml:"defaults" mapstructure:"defaults"`
    Security Security          `yaml:"security" mapstructure:"security"`
    VMs      map[string]VMDef  `yaml:"vms"      mapstructure:"vms"`
}

// Defaults holds default values for VM creation.
type Defaults struct {
    Backend string `yaml:"backend"  mapstructure:"backend"`
    CPUs    int    `yaml:"cpus"     mapstructure:"cpus"`
    Memory  string `yaml:"memory"   mapstructure:"memory"`
    Disk    string `yaml:"disk"     mapstructure:"disk"`
    Image   string `yaml:"image"    mapstructure:"image"`
    VM      string `yaml:"vm"       mapstructure:"vm"`
}

// Security holds security policy settings.
type Security struct {
    EgressAllowlist []string `yaml:"egress_allowlist" mapstructure:"egress_allowlist"`
    MountPolicy     string   `yaml:"mount_policy"     mapstructure:"mount_policy"`
}

// MountPolicy valid values.
const (
    MountPolicyNone     = "none"     // No mounts (default, most secure)
    MountPolicyReadonly = "readonly" // Mount CWD as read-only
    MountPolicyProject  = "project"  // Mount project directory as writable
)

// VMDef holds the definition of a VM as specified in config files.
type VMDef struct {
    CPUs       int               `yaml:"cpus"        mapstructure:"cpus"`
    Memory     string            `yaml:"memory"      mapstructure:"memory"`
    Disk       string            `yaml:"disk"        mapstructure:"disk"`
    Image      string            `yaml:"image"       mapstructure:"image"`
    Backend    string            `yaml:"backend"     mapstructure:"backend"`
    Provisions []string          `yaml:"provisions"  mapstructure:"provisions"`
    Env        map[string]string `yaml:"env"         mapstructure:"env"`
}

// VMConfig holds the full persisted config for a specific VM instance,
// including runtime state managed by sd.
type VMConfig struct {
    Name       string            `yaml:"name"`
    Backend    string            `yaml:"backend"`
    CPUs       int               `yaml:"cpus"`
    Memory     string            `yaml:"memory"`
    Disk       string            `yaml:"disk"`
    Image      string            `yaml:"image"`
    Provisions []string          `yaml:"provisions"`
    Env        map[string]string `yaml:"env"`
    State      VMState           `yaml:"state"`
    BackendMeta map[string]any   `yaml:"backend_meta"`
}

// VMState tracks the runtime state of a VM instance.
type VMState struct {
    Status      string    `yaml:"status"`
    CreatedAt   time.Time `yaml:"created_at"`
    LastStarted time.Time `yaml:"last_started,omitempty"`
    LastStopped time.Time `yaml:"last_stopped,omitempty"`
}

// VMStatus valid values.
const (
    VMStatusCreated = "created"
    VMStatusRunning = "running"
    VMStatusStopped = "stopped"
    VMStatusError   = "error"
)
```

### Configuration Loader Interface

```go
package config

// Loader provides access to the resolved configuration.
type Loader interface {
    // Load reads and merges all configuration sources. It MUST be called
    // once at application startup before any config values are accessed.
    // Security-sensitive keys from project-level config are ignored
    // during the merge (REQ-005-017).
    Load() error

    // Get returns the fully resolved Config.
    Get() *Config

    // GetForVM returns the resolved configuration for a specific VM,
    // applying the inheritance chain defined in REQ-005-015.
    GetForVM(name string) (*VMConfig, error)

    // Source returns the source that provided the value for the given key.
    // Returns one of: "cli flag", "environment variable",
    // "project-level config", "user-level config", "vm config",
    // "built-in default", or "" if the key is unknown.
    Source(key string) string

    // Validate checks all discoverable config files for errors.
    // Returns a slice of ValidationResult, one per file.
    Validate() ([]ValidationResult, error)

    // Set writes a key-value pair to the specified config file.
    // The target parameter selects "user" or "project" config.
    Set(key string, value any, target string) error

    // Resolve expands environment variable references in a string value.
    // "${VAR}" and "$VAR" are replaced with the host environment value.
    Resolve(value string) string
}

// ValidationResult holds the result of validating a single config file.
type ValidationResult struct {
    Path   string   `json:"path"`
    Valid  bool     `json:"valid"`
    Errors []string `json:"errors"`
}
```

### Environment Variable Resolution

Environment variable references in config values follow these rules:

1. `${VAR_NAME}` -- resolved from host environment at runtime
2. `$VAR_NAME` -- resolved from host environment at runtime (name continues until a non-alphanumeric, non-underscore character)
3. `$$` -- escape sequence, resolves to a literal `$`
4. References to unset variables resolve to the empty string with a warning

Resolution MUST happen lazily (at the point of use), not eagerly at config load time. This ensures that environment changes during a session are reflected.

### Viper Initialization

```go
package config

import (
    "strings"

    "github.com/spf13/viper"
)

func initViper(sdHome string, projectConfigDir string) *viper.Viper {
    v := viper.New()

    // File format
    v.SetConfigName("config")
    v.SetConfigType("yaml")

    // Search paths (order matters: first found wins, but we merge manually
    // to implement our precedence)
    if projectConfigDir != "" {
        v.AddConfigPath(projectConfigDir)
    }
    v.AddConfigPath(sdHome)

    // Environment variables
    v.SetEnvPrefix("SD")
    v.AutomaticEnv()
    v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

    // Built-in defaults
    v.SetDefault("defaults.backend", "lima")
    v.SetDefault("defaults.cpus", 4)
    v.SetDefault("defaults.memory", "8GiB")
    v.SetDefault("defaults.disk", "100GiB")
    v.SetDefault("defaults.image", "ubuntu:24.04")
    v.SetDefault("defaults.vm", "")
    v.SetDefault("security.mount_policy", "none")
    v.SetDefault("security.egress_allowlist", []string{
        "api.anthropic.com",
        "github.com",
        "*.githubusercontent.com",
        "archive.ubuntu.com",
        "security.ubuntu.com",
        "deb.debian.org",
        "registry.npmjs.org",
        "pypi.org",
        "files.pythonhosted.org",
        "proxy.golang.org",
        "sum.golang.org",
    })

    return v
}
```

### CLI Surface

```
sd config get <key> [--vm <name>] [--json]
sd config set <key> <value> [--project] [--json]
sd config list [--json] [--resolve] [--unmask]
sd config edit [--project]
sd config validate [--json]
```

## Error Handling

| Error Condition | Severity | User Message | Recovery |
|---|---|---|---|
| Config file is not valid YAML | Fatal | `error: failed to parse {path}: {parse_error} at line {N}` | Fix the YAML syntax in the indicated file |
| Unknown mount_policy value | Fatal | `error: invalid mount_policy "{value}" in {path}: must be one of: none, readonly, project` | Change to a valid value |
| Unknown VM status value | Fatal | `error: invalid vm status "{value}" in {path}: must be one of: created, running, stopped, error` | This indicates corruption; delete the VM config and recreate |
| Referenced env var not set | Warning | `warning: environment variable {VAR} is not set; using empty string` | Set the environment variable on the host |
| Config key not found (get) | Fatal | `error: unknown config key "{key}"` | Check available keys with `sd config list` |
| Cannot create $SD_HOME | Fatal | `error: cannot create config directory {path}: {os_error}` | Check filesystem permissions |
| Config set validation failure | Fatal | `error: setting {key}={value} would produce invalid config: {reason}; change was rolled back` | Use a valid value |
| $EDITOR not set (config edit) | Fatal | `error: no editor configured; set $EDITOR or $VISUAL` | Export EDITOR or VISUAL environment variable |
| Config edit in non-interactive context | Fatal | `error: "sd config edit" is interactive and cannot be used when SD_JSON=true` | Use `sd config set` instead |
| Security keys in project config | Warning | `warning: security settings in project-level config are ignored (found: {key}); set these in ~/.sd/config.yaml or via CLI flags` | Move security settings to user-level config |

All errors MUST be JSON-serializable when `--json` is active, using the structure:

```json
{
  "error": {
    "code": "invalid_config",
    "message": "failed to parse ~/.sd/config.yaml: yaml: line 5: did not find expected key",
    "file": "~/.sd/config.yaml",
    "line": 5
  }
}
```

## Security Considerations

### Trust Boundaries

The configuration system crosses trust boundaries between:
- The host environment (where environment variables containing secrets live)
- Config files on disk (which may be checked into version control)
- VM environments (which receive resolved credential values)

### Credential Handling

- Config files MUST store credentials as environment variable references (`${VAR_NAME}`), never as literal values.
- The `sd config list` and `sd config get` commands MUST NOT display resolved secret values by default.
- The `--resolve --unmask` flag combination is required to see actual secret values, and this combination SHOULD be used only for debugging.
- Resolved secret values MUST NOT appear in log output, debug output, or error messages.
- The `$SD_HOME` directory and all files within it MUST be created with restrictive permissions (`0700` for directories, `0600` for files) to prevent other users on the system from reading config.

### Project-Level Config Security

Project-level config files (`.sd/config.yaml`) may be checked into version control and come from untrusted sources (e.g., a cloned repository). To prevent privilege escalation:

- Security-sensitive keys (`security.*`) in project-level config MUST be ignored (REQ-005-017). Only user-level config, CLI flags, and environment variables can set security settings.
- The `security.egress_allowlist` in user-level config is authoritative. A project-level config cannot narrow or replace it.
- A malicious project config cannot change `$SD_HOME` or redirect where VM data is stored.

### Blast Radius

If the config system is compromised (e.g., a malicious `.sd/config.yaml` in a cloned repo):
- Project-level config can influence VM settings (CPUs, memory, image) but cannot override security policy set at the user level.
- Environment variable references in project-level config are resolved from the host environment; a malicious config cannot exfiltrate variables that are not already named in the config.
- A malicious project config cannot change `$SD_HOME` or redirect where VM data is stored.

### Mitigations

- File permissions enforcement on `$SD_HOME` (`0700`/`0600`).
- Security keys in project-level config are ignored (REQ-005-017).
- Project-level config is validated and produces warnings for unexpected keys.
- Secret values are masked in all output by default.
- The `config validate` command helps users audit their config files.

## Testing Strategy

### Unit Tests

| Requirement | Test Description |
|---|---|
| REQ-005-001 | Test precedence by setting the same key at all five levels and verifying the highest-precedence value wins |
| REQ-005-002 | Test user-level config loading from default and custom `SD_HOME` paths |
| REQ-005-003 | Test project-level config discovery by walking parent directories |
| REQ-005-004 | Test that all built-in defaults are returned when no config files exist |
| REQ-005-005 | Test environment variable mapping for all documented variables |
| REQ-005-006 | Test YAML parsing for valid configs, partial configs, and invalid configs |
| REQ-005-007 | Test VM definition file creation, loading, and state transitions |
| REQ-005-008 | Test `${VAR}`, `$VAR`, `$$` escape, and unset variable resolution |
| REQ-005-014 | Test Viper initialization, env prefix, and key replacer |
| REQ-005-015 | Test VM config inheritance chain with values at multiple levels |
| REQ-005-017 | Test that security keys in project-level config are ignored and produce warnings |
| REQ-005-018 | Test mount policy values: none produces no mounts, readonly mounts CWD, project mounts project root |

### Integration Tests

| Requirement | Test Description |
|---|---|
| REQ-005-002, REQ-005-003 | Test config loading with real files on disk in a temporary directory tree |
| REQ-005-005 | Test with actual environment variables set in the test process |
| REQ-005-010 | Test `sd config set` creates and modifies real YAML files |
| REQ-005-013 | Test `sd config validate` against a directory tree with valid and invalid config files |
| REQ-005-016 | Test directory creation with correct permissions |
| REQ-005-017 | Test that project-level security keys are ignored when loading from real files |

### Script Tests

| Requirement | Test Description |
|---|---|
| REQ-005-009 | `sd config get defaults.backend` returns expected value and source |
| REQ-005-009 | `sd config get defaults.backend --json` returns valid JSON with key, value, source |
| REQ-005-010 | `sd config set defaults.cpus 8` followed by `sd config get defaults.cpus` returns 8 |
| REQ-005-011 | `sd config list` displays all keys with sources in tabular format |
| REQ-005-011 | `sd config list --json` returns valid JSON array |
| REQ-005-013 | `sd config validate` exits 0 for valid config, exits 1 for invalid config |
| REQ-005-012 | `sd config edit` with `SD_JSON=true` exits with error |

## Dependencies

### Depends On

- [001-architecture.md](001-architecture.md) — overall system architecture and package layout
- [002-cli.md](002-cli.md) — CLI command structure and `--json` output conventions
- [004-security.md](004-security.md) — default egress allowlist, mount validation rules, sensitive path definitions
- `github.com/spf13/viper` -- config file loading, env binding, flag binding
- `github.com/spf13/cobra` -- CLI flag definitions for binding to Viper
- `gopkg.in/yaml.v3` -- YAML marshaling for `sd config set` writes (Viper uses this internally)

### Depended On By

- All other specs that consume configuration (VM lifecycle, backend selection, security policy, provisioning, session management) depend on this spec for resolved config values
- CLI command specs depend on this spec for `--json` global flag behavior via `SD_JSON`

## Revision History

| Date       | Author | Change Description |
|------------|--------|--------------------|
| 2026-03-27 | claude | Initial draft      |
| 2026-03-27 | claude | Review fixes: add default egress allowlist matching spec 004; add defaults.vm to defaults table and config example; add REQ-005-017 (security keys ignored in project config); add REQ-005-018 (mount_policy values and interaction with spec 004); fix dependency section to reference internal specs |
