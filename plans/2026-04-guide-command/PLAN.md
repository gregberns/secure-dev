# Plan: `sd guide` Command

**Spec**: 002-cli (needs new requirements added)
**Status**: draft
**Priority**: P0 -- highest leverage improvement for agent usability
**Branch**: `feat/guide-command`

## Overview

Add an `sd guide` command that makes the CLI self-describing for both humans and AI agents. This is the primary mechanism for agent onboarding -- an agent runs `sd guide --agent` and immediately knows how to help the user.

Also update root `--help` to surface `sd guide` prominently.

## Design Reference

See `docs/usage-patterns-and-agent-onboarding.md` Part 3 for full design rationale.

## Requirements Trace

| New Requirement | Description |
|-----------------|-------------|
| REQ-002-020 | `sd guide` outputs human-readable getting-started guide with current system state |
| REQ-002-021 | `sd guide --agent` outputs agent-consumable Markdown with full command reference, current state, and workflow instructions |
| REQ-002-022 | `sd guide` supports `--json` for structured output |
| REQ-002-023 | Root `--help` text leads with `sd guide` discovery |

## Tasks

### Task 1: Add `sd guide` command scaffold

**File**: `internal/cmd/guide.go`

Create the command with flags:
- `--agent` -- output full agent-consumable guide (Markdown)
- `--json` -- structured JSON output
- No flags -- human-readable short guide

Register in root command. Place in a new "Getting Started" command group that appears first in `--help`.

**Tests**: `internal/cmd/guide_test.go`
- Command exists and has correct flags
- Default output is non-empty
- `--agent` output is non-empty and longer than default
- `--json` output is valid JSON

**Acceptance**: `sd guide --help` exits 0 with correct description.

### Task 2: Implement dynamic state gathering

**File**: `internal/cmd/guide.go`

The guide command gathers live system state:
1. Run doctor checks (reuse `runDoctorChecks()` or equivalent)
2. List VMs across all backends (reuse list logic)
3. Load available provisioning modules
4. Check backend availability

Package this into a `guideState` struct:
```go
type guideState struct {
    Backends     []backendInfo    // name, available, error
    VMs          []backend.VMInfo // from list
    Modules      []string         // available module names
    DoctorOK     bool             // all checks pass?
    DoctorIssues []string         // failing check names
    SDHome       string
    SDHomePerms  bool             // permissions OK?
}
```

**Tests**:
- State gathering works with memory backend
- Handles no VMs gracefully
- Handles unavailable backends gracefully

**Acceptance**: `guideState` populated correctly from live system.

### Task 3: Implement human-readable output (default mode)

**File**: `internal/cmd/guide.go`

Template the short guide:
```
sd: Secure Dev Environment Manager

Quick Start:
  1. sd doctor              Check prerequisites
  2. sd create <name>       Create a VM
  3. sd connect <name>      Enter the VM
  4. sd token github setup  Configure credentials

Current State:
  Backend: lima (available)
  VMs: 2 (my-app: running, infra: stopped)
  Health: all checks passing

Common Commands:
  sd list                   Show all VMs
  sd status <name>          VM details
  sd exec <name> -- <cmd>   Run command in VM
  sd sync to <name> <path>  Sync files into VM
  sd ensure <name>          Create or start VM
  sd destroy <name> -f      Remove VM
```

**Tests**:
- Output includes "Quick Start" section
- Output reflects actual VM count
- Output reflects doctor status
- No VMs shows appropriate message

**Acceptance**: Human can read the output and know what to do next.

### Task 4: Implement agent-consumable output (`--agent`)

**File**: `internal/cmd/guide.go`

Output a full Markdown document designed for LLM consumption. See the template in `docs/usage-patterns-and-agent-onboarding.md` Part 3 for the exact format.

Key sections:
1. **Header**: What sd is, one-line purpose
2. **Current State**: Dynamic -- backends, VMs, doctor status
3. **Capabilities**: Every command with flags and examples
4. **Workflow Guide**: Step-by-step for common tasks (setup, troubleshoot, manage)
5. **Important Notes**: Security model, credential handling, isolation guarantees

The command reference section should be generated from Cobra's command tree, not hardcoded. This ensures it stays in sync as commands are added.

**Tests**:
- Output is valid Markdown
- Output includes all registered commands
- Output includes current VM list
- Output includes doctor status
- Output changes when VMs are created/destroyed (dynamic)

**Acceptance**: An LLM reading this output can assist a user with any `sd` operation.

### Task 5: Implement JSON output (`--json`)

**File**: `internal/cmd/guide.go`

JSON envelope:
```json
{
  "ok": true,
  "data": {
    "backends": [...],
    "vms": [...],
    "modules": [...],
    "doctor": { "ok": true, "issues": [] },
    "commands": [
      { "name": "create", "usage": "create <name>", "flags": [...] },
      ...
    ],
    "guide_text": "..."  // the --agent markdown as a string
  }
}
```

**Tests**:
- Valid JSON structure
- All fields populated

### Task 6: Update root `--help` text

**File**: `internal/cmd/root.go`

Add a "Getting Started" command group that appears first:
```go
rootCmd.AddGroup(&cobra.Group{ID: "start", Title: "Getting Started:"})
```

Register `guide` in this group. Update the root command's Long description to mention `sd guide`.

Ensure the help output looks like:
```
sd (secure-dev) - Secure VM environments for AI coding agents

Getting Started:
  guide       Setup guide and command reference (try: sd guide --agent)

VM Management:
  create      Create a new VM
  ...
```

**Tests**:
- Root help output contains "Getting Started" group
- "guide" appears before other command groups

**Acceptance**: `sd --help` leads with the guide command.

## Dependency DAG

```
Task 1 (scaffold) --> Task 2 (state gathering) --> Task 3 (human output)
                                                --> Task 4 (agent output)
                                                --> Task 5 (JSON output)
Task 6 (help text) -- independent, can parallel with any task
```

## Testing Strategy

- Unit tests with memory backend for state gathering
- Table-driven tests for output formatting
- Integration test: create VM with memory backend, verify guide reflects it
- Verify `--agent` output has all commands by comparing with Cobra command tree
