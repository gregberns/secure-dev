# Exploratory Testing Plan

## Overview

Automated exploratory testing of the `sd` CLI using multiple Claude Code agents deployed via ntm. Agents exercise the CLI binary directly, report findings as structured markdown, and file `bd` issues for anything broken.

## Waves

### Wave 1: CLI Surface (No Backend Required)

Tests that exercise CLI parsing, help text, flag validation, JSON output formatting, and config commands. These work without any VM backend because they fail or succeed before backend calls are made.

**Agents:**

| Agent | Prompt File | Mission |
|-------|-------------|---------|
| CLI Surface | `agents/cli-surface.md` | Run every command/subcommand with `--help`, basic args, invalid args |
| JSON Contract | `agents/json-contract.md` | Verify `--json` produces valid JSON on every command, including errors |
| Config & Flags | `agents/config-flags.md` | Exercise config subcommands, global flags, input validation |

**Pass criteria:**
- Every command's `--help` exits 0 and produces non-empty output
- Every command with `--json` produces parseable JSON (even on error)
- Invalid flags produce exit code 2 and actionable error messages
- Config commands work against a fresh `$SD_HOME`
- No panics or stack traces on any input

### Wave 2: Backend Integration (Requires In-Memory or Docker Backend)

Tests that exercise VM lifecycle, provisioning, connection, and sync. Requires a functioning backend.

**Agents (draft -- finalize when backends land):**

| Agent | Mission |
|-------|---------|
| Lifecycle | create -> start -> status -> stop -> start -> destroy |
| Provisioning | create -> provision (each module) -> verify |
| Workflow | Full user journey: create -> provision -> connect -> exec -> sync -> snapshot -> destroy |
| Adversarial | Invalid states, interrupted operations, concurrent access, resource exhaustion |

### Wave 3: Spec Compliance (Requires Working Backend)

One agent per spec, systematically verifying every REQ-NNN-MMM.

## Command Tree (Reference)

All commands that agents must exercise:

```
sd
  create          --backend, --cpus, --memory, --disk, --modules, --mount, --allow-egress, --dry-run
  destroy         --force
  start
  stop
  list
  status
  snapshot
    create
    list
    restore
    delete
  connect         --no-tmux
  exec
  sync
    to            --diff
    from          --diff
  ssh-config
  config
    get
    set
    list
    edit
    validate
    egress
      add
      remove
      list
  provision
    provision list
  audit
  security
    status
  token
    github setup
    rotate
    revoke
    list
  doctor
  logs
  diff
  completion      bash, zsh, fish, powershell
  version
```

**Global flags:** `--json`, `--verbose/-v`, `--quiet/-q`, `--config`, `--vm`

## Environment Setup

Each agent gets:
- **Binary**: freshly built `sd` at `$SD_TEST_BIN`
- **Isolated home**: `$SD_HOME=/tmp/sd-exploratory/{agent-name}` (prevents cross-agent state collision)
- **Report output**: `tests/exploratory/runs/{date}/{agent-name}/report.md`

## Reporting

Agents produce two outputs:

1. **Report file** (`report.md`) using the template at `report-template.md` -- structured findings with pass/fail per command
2. **bd issues** for anything broken -- filed via `bd create` with label `exploratory-test`

## Running

```bash
# Build and launch all wave 1 agents
./tests/exploratory/launch.sh

# Monitor
ntm activity secure-dev --watch
ntm watch secure-dev

# Collect results
ntm summary secure-dev --format markdown
```
