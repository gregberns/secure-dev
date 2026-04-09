# Plan: `.sd.yaml` Project Configuration

**Spec**: 005-configuration (needs new requirements added)
**Status**: draft
**Priority**: P2 -- makes per-project setup declarative
**Branch**: `feat/project-config`
**Depends on**: `sd ensure` command (plan: 2026-04-ensure-command)

## Overview

Add support for a `.sd.yaml` file in a project repository root. This file declares the desired VM configuration for that project. When present, commands like `sd ensure` (no args) and `sd create` (no name) read from it, eliminating manual flag passing.

## Motivation

Today, creating a VM for a project requires remembering the right flags:
```bash
sd create my-app --backend=lima --cpus=4 --memory=8GiB \
  --modules=base,claude-code,golang \
  --mount=.:/home/ubuntu/projects/my-app:rw \
  --allow-egress=pkg.go.dev --allow-egress=proxy.golang.org
```

With `.sd.yaml`:
```bash
cd ~/code/my-app
sd ensure    # reads .sd.yaml, creates/starts VM with declared config
```

The config file is checked into the repo, so every developer (and every agent) gets the same environment.

## Requirements Trace

| New Requirement | Description |
|-----------------|-------------|
| REQ-005-020 | `.sd.yaml` in project root defines VM name, backend, resources, modules, mounts, egress |
| REQ-005-021 | `sd create` and `sd ensure` with no name argument read `.sd.yaml` from CWD (or parent dirs) |
| REQ-005-022 | CLI flags override `.sd.yaml` values (flags > project config > user config > defaults) |
| REQ-005-023 | `.sd.yaml` is validated on load; invalid files produce actionable errors |
| REQ-005-024 | `sd init` generates a `.sd.yaml` template in the current directory |

## `.sd.yaml` Format

```yaml
# .sd.yaml -- Secure Dev project configuration
# Checked into the repository. Used by 'sd create' and 'sd ensure'.

# VM name. Default: directory name, lowercased, with invalid chars replaced.
name: my-app

# Backend. Default: from user config or "lima".
backend: lima

# Resource allocation. Defaults from user config.
cpus: 4
memory: 8GiB
disk: 100GiB

# Provisioning modules to install.
modules:
  - base
  - claude-code
  - golang

# Host directories to mount into the VM.
# Format: host_path:guest_path:mode (ro or rw)
# "." is resolved to the project root (where .sd.yaml lives).
mounts:
  - .:~/projects/my-app:rw

# Additional domains to allow through egress firewall.
allow_egress:
  - pkg.go.dev
  - proxy.golang.org
  - sum.golang.org
```

## Tasks

### Task 1: Define `.sd.yaml` schema and types

**File**: `internal/config/project.go`

Define the ProjectConfig type:
```go
type ProjectConfig struct {
    Name        string   `yaml:"name"`
    Backend     string   `yaml:"backend,omitempty"`
    CPUs        int      `yaml:"cpus,omitempty"`
    Memory      string   `yaml:"memory,omitempty"`
    Disk        string   `yaml:"disk,omitempty"`
    Modules     []string `yaml:"modules,omitempty"`
    Mounts      []string `yaml:"mounts,omitempty"`      // "host:guest:mode"
    AllowEgress []string `yaml:"allow_egress,omitempty"` 
}
```

Add validation:
- Name must match VM name pattern (or be empty for auto-derivation)
- Mounts must be valid mount specs
- Modules must be valid module names (or "all")
- Resources must be positive

**Tests**: `internal/config/project_test.go`
- Valid configs parse correctly
- Invalid names rejected
- Invalid mounts rejected
- Empty/missing fields use zero values (not defaults -- defaults are applied at merge time)

### Task 2: Implement project config discovery and loading

**File**: `internal/config/project.go`

```go
// FindProjectConfig searches for .sd.yaml starting from dir and walking
// up to root. Returns the path and parsed config, or ("", nil, nil) if not found.
func FindProjectConfig(dir string) (string, *ProjectConfig, error)
```

Walk up from the given directory looking for `.sd.yaml`. Stop at filesystem root or home directory. Parse and validate if found.

Auto-derive name from directory if not specified:
```go
func deriveVMName(dir string) string {
    // Take the directory name, lowercase, replace invalid chars with hyphens
    // e.g., "My-Project_v2" -> "my-project-v2"
}
```

**Tests**:
- Finds `.sd.yaml` in current directory
- Finds `.sd.yaml` in parent directory
- Stops at home directory (doesn't walk into `/`)
- Returns nil when not found
- Name derivation from various directory names

### Task 3: Integrate with `sd create`

**File**: `internal/cmd/create.go`

When `sd create` is called with no positional argument:
1. Call `FindProjectConfig(cwd)`
2. If found, use its values as defaults (CLI flags still override)
3. If not found, return error "missing VM name -- provide a name or create .sd.yaml"

Update the Args validator to allow 0 or 1 args when project config exists.

Precedence: CLI flags > .sd.yaml > user config > built-in defaults.

Resolve `.` in mount paths relative to the `.sd.yaml` location, not CWD.

**Tests**:
- `sd create` with `.sd.yaml` creates VM with correct config
- CLI flags override `.sd.yaml` values
- `sd create my-name` with `.sd.yaml` uses the explicit name, not `.sd.yaml` name
- Mount path resolution works (`.` -> project root)
- Missing `.sd.yaml` and no name -> error

### Task 4: Integrate with `sd ensure`

**File**: `internal/cmd/ensure.go`

Same logic as Task 3 but for `sd ensure`. When no name arg is provided, read `.sd.yaml`.

This is the primary use case: developer `cd`s into a project and runs `sd ensure`.

**Tests**:
- `sd ensure` with `.sd.yaml` creates if needed
- `sd ensure` with `.sd.yaml` starts if stopped
- `sd ensure` with `.sd.yaml` is no-op if running

### Task 5: Add `sd init` command

**File**: `internal/cmd/init.go`

Generate a `.sd.yaml` template in the current directory:
```bash
sd init                    # generate .sd.yaml with sensible defaults
sd init --modules=golang   # generate with specific modules
```

Auto-detect project type by looking for:
- `go.mod` -> suggest golang module
- `package.json` -> suggest nodejs module
- `Cargo.toml` -> suggest rust module
- `pyproject.toml` / `requirements.txt` -> suggest python module
- `Dockerfile` -> suggest docker module

**Tests**:
- Generates valid `.sd.yaml`
- Auto-detects Go project
- Auto-detects Node project
- Does not overwrite existing `.sd.yaml` without `--force`
- Derived name from directory is valid

## Dependency DAG

```
Task 1 (schema) --> Task 2 (discovery) --> Task 3 (create integration)
                                       --> Task 4 (ensure integration)
Task 5 (init) -- independent after Task 1
```

## Testing Strategy

- Unit tests for config parsing and validation
- Unit tests for directory walking and name derivation
- Integration tests with memory backend for create/ensure flows
- Property test: any valid `.sd.yaml` round-trips through parse/serialize
