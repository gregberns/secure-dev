# Plan: `sd ensure` Command

**Spec**: 002-cli (needs new requirements added)
**Status**: draft
**Priority**: P1 -- makes the common agent operation idempotent
**Branch**: `feat/ensure-command`

## Overview

Add an `sd ensure <name>` command that is fully idempotent: creates the VM if it doesn't exist, starts it if stopped, and is a no-op if already running. This is the command agents should call most of the time -- no need for check-then-branch logic.

## Motivation

Today, an agent that wants "make sure this VM exists and is running" must:
```
sd status my-vm --json  # check if exists
# parse output, branch:
#   not found -> sd create my-vm --modules=...
#   stopped   -> sd start my-vm
#   running   -> no-op
```

With `sd ensure`:
```
sd ensure my-vm --modules=base,claude-code
# Done. VM exists and is running, regardless of prior state.
```

## Requirements Trace

| New Requirement | Description |
|-----------------|-------------|
| REQ-002-024 | `sd ensure <name>` creates VM if not found, starts if stopped, no-ops if running |
| REQ-002-025 | `sd ensure` accepts all flags from `sd create` (backend, cpus, memory, modules, etc.) |
| REQ-002-026 | `sd ensure` supports `--json` output reporting the action taken |
| REQ-002-027 | When VM exists, `sd ensure` does NOT re-provision or change config (idempotent) |

## Tasks

### Task 1: Add `sd ensure` command scaffold

**File**: `internal/cmd/ensure.go`

Create the command:
```go
ensureCmd := &cobra.Command{
    Use:     "ensure <name>",
    Short:   "Ensure a VM exists and is running",
    Long:    "Create the VM if it doesn't exist, start it if stopped, no-op if running.",
    GroupID: "vm",
    Aliases: []string{"up"},
    Args:    exactArgs(1, "<name> [flags]"),
    RunE:    runEnsure,
}
```

Accept all `create` flags: `--backend`, `--cpus`, `--memory`, `--disk`, `--modules`, `--mount`, `--allow-egress`.

**Tests**: `internal/cmd/ensure_test.go`
- Command registered and has correct flags
- Alias `up` works

### Task 2: Implement ensure logic

**File**: `internal/cmd/ensure.go`

The logic:
1. Resolve backend (flag > per-VM config > global default)
2. Check if VM exists via `backend.Status(ctx, name)`
3. Branch:
   - **Not found** (`ErrVMNotFound`): Run full create flow (reuse `runCreate` logic or extract shared helper)
   - **Stopped**: Run `backend.Start(ctx, name)`, then verify running
   - **Running**: No-op
4. Report action taken

The result struct:
```go
type ensureResult struct {
    Name    string `json:"name"`
    Action  string `json:"action"`  // "created", "started", "already_running"
    Backend string `json:"backend"`
    Status  string `json:"status"`  // always "running" on success
}
```

**Important**: When the VM already exists, do NOT re-provision or change its config. The `--modules`, `--cpus`, etc. flags are only used when creating a new VM. If the VM exists with different settings, that's fine -- `ensure` guarantees existence and running state, not configuration.

**Tests** (using memory backend):
- VM doesn't exist: creates and starts it
- VM exists and stopped: starts it
- VM exists and running: no-op, returns "already_running"
- Flags are used for creation only
- JSON output has correct action field
- Error on invalid name

### Task 3: Extract shared create logic

**File**: `internal/cmd/create.go`, `internal/cmd/ensure.go`

Currently `runCreate` does everything inline. Extract the core create logic into a shared function that both `create` and `ensure` can call:

```go
// createVM handles the create+start+provision+persist flow.
// Used by both 'sd create' and 'sd ensure' (when VM doesn't exist).
func createVM(ctx context.Context, f *ui.Formatter, name, backendName string, vmCfg backend.VMConfig, modules []string) error
```

This avoids duplicating the create flow in ensure.

**Tests**:
- Existing create tests still pass
- Ensure-via-create path works

### Task 4: Integration with `.sd.yaml` (future-proof)

**File**: `internal/cmd/ensure.go`

Add a hook point for `.sd.yaml` project config (Task 3 of the project-config plan). When `.sd.yaml` exists in the current directory, `sd ensure` (with no name arg) should read it. For now, just add the flag resolution logic that will look for project config:

```go
// If no name provided and .sd.yaml exists, read from it
// (actual .sd.yaml loading will be implemented in the project-config plan)
```

Mark this with a TODO referencing the project-config plan. Do not implement .sd.yaml parsing here.

**Tests**: Not applicable until project-config plan is implemented.

## Dependency DAG

```
Task 1 (scaffold) --> Task 2 (ensure logic)
                  --> Task 3 (extract create helper)
Task 4 -- deferred, placeholder only
```

## Testing Strategy

- All tests use memory backend for speed
- Table-driven tests covering all three branches (create, start, no-op)
- Property test: ensure is idempotent -- calling it N times produces same state
- Verify JSON output for each action type
