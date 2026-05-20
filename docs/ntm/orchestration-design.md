# Orchestration Design: Coordinator/Worker Agent Architecture

Status: **draft** — ideas to explore and validate

## Objective

Use ntm as an orchestrator to manage large sets of jobs through a coordinator/worker pattern. The system should handle the full lifecycle:

1. Human + primary agent collaborate on spec/plan
2. Hand off to a planning coordinator to process the plan into tasks
3. Hand off to an implementation coordinator to execute the tasks
4. Workers implement, report back, context gets cleared between tasks
5. Human gets notified when work is complete

## Architecture Overview

```
Human (you)
  │
  ├── Primary Agent (pane 1, opus)
  │     You work together on specs and plans.
  │     When a plan is ready, hand off the "epic" bead.
  │
  ├── Planning Coordinator (spawned on demand, sonnet)
  │     Receives an epic bead.
  │     Runs the dev-workflow: review, task breakdown, bead creation.
  │     Commits everything, messages human when planning is complete.
  │     Then: context cleared or agent torn down.
  │
  └── Implementation Coordinator (spawned on demand, sonnet)
        Reads all beads for the epic.
        Determines how many workers are needed.
        Spawns workers, assigns beads.
        Monitors progress, collects results.
        Messages human when implementation is complete.
        │
        ├── Worker 1 (sonnet or opus depending on complexity)
        │     Receives one bead. Implements. Tests. Commits.
        │     Context cleared. Receives next bead or is torn down.
        │
        ├── Worker 2
        │     Same pattern.
        │
        └── Worker N
```

## Key Design Decisions to Explore

### 1. Context Management

**Problem**: Agents accumulate context over a session. After completing a task, the old context is noise for the next task.

**Options to test**:

| Approach | How | Pros | Cons |
|----------|-----|------|------|
| `ntm respawn --panes=N` | Kills and restarts Claude in the pane | Clean context, same pane | Loses agent-mail registration |
| Kill pane + add new | `tmux kill-pane` then `ntm add` | Clean everything | Pane numbering shifts |
| `/clear` in Claude Code | Send `/clear` via ntm send | Preserves session | May not fully reset context |
| Subagents | Use Claude's Agent tool | Isolated context per task | Limited tool access, no MCP |

**Needs testing**: Does `ntm respawn` preserve the MCP connection? Does the restarted agent need to re-register with agent-mail?

### 2. Model Selection

**Strategy**: Use the cheapest model that can do the job.

| Role | Recommended Model | Reasoning |
|------|-------------------|-----------|
| Primary (human collaboration) | opus | Complex reasoning, spec authoring |
| Planning Coordinator | sonnet | Following established workflow, no novel design |
| Implementation Coordinator | sonnet | Reading beads, spawning workers, monitoring |
| Workers (simple tasks) | sonnet | Straightforward implementation |
| Workers (complex tasks) | opus | Architecture changes, complex refactors |

**ntm supports this**:
```bash
# Spawn coordinator as sonnet
ntm add yourproject --cc=1:sonnet

# Spawn workers mixed
ntm add yourproject --cc=2:sonnet --cc=1:opus

# Send to specific model variant
ntm send yourproject --cc=sonnet "implement this simple feature"
ntm send yourproject --cc=opus "refactor the auth system"
```

**Open question**: Can we change the model of a running agent? Or must we spawn a new one?

### 3. Coordinator Types

#### Planning Coordinator

**Trigger**: Human completes a spec/plan and hands off an epic bead ID.

**Responsibilities**:
1. Read the spec and plan
2. Run the review pipeline (3 agents: architect, critic, qa — could be subagents)
3. Create beads for each task in the plan
4. Link beads to the epic
5. Commit all artifacts
6. Message human via agent-mail: "Epic {id} planning complete. {N} beads created."

**Prompt template** (for `ntm controller --prompt`):
```
You are a planning coordinator for the secure-dev project.

Epic bead: {{BEAD_ID}}
Project: {{.ProjectDir}}

Your job:
1. Read the epic bead: bd show {{BEAD_ID}}
2. Read the associated spec and plan
3. Run /dev-workflow to process it through review and task breakdown
4. Create beads for each implementation task: bd create --title "..." --body "..."
5. When all beads are created, commit everything
6. Send a message via agent-mail to report completion
7. Then exit (/quit)
```

#### Implementation Coordinator

**Trigger**: Human (or planning coordinator) signals that beads are ready.

**Responsibilities**:
1. Read all beads for the epic: `bd list`
2. Determine dependency order
3. Spawn the right number of workers
4. Assign beads to workers via `ntm send`
5. Monitor worker progress via `ntm activity` and `ntm --robot-wait`
6. When a worker finishes, either assign next bead or tear down
7. When all beads are done, run integration tests
8. Message human: "Epic {id} implementation complete."

### 4. Worker Lifecycle

For a linear set of tasks, two patterns:

**Pattern A: Respawn between tasks**
```bash
# Worker finishes task 1
ntm respawn yourproject --panes=3 --force
# Wait for restart
ntm --robot-wait=yourproject --panes=3 --wait-until=idle
# Send next task
ntm send yourproject --pane=3 "Implement bead secure-dev-abc. Read bd show secure-dev-abc for details."
```

**Pattern B: Send /clear then next task**
```bash
# Worker finishes task 1
ntm send yourproject --pane=3 "/clear"
sleep 2
ntm send yourproject --pane=3 "Implement bead secure-dev-xyz."
```

**Needs testing**: Which preserves MCP tools? Which actually clears context effectively?

### 5. Communication Flow

```
Human ──[hands off epic bead]──> Planning Coordinator
                                       │
                                  [creates beads]
                                       │
                                  [agent-mail: "planning done"]
                                       │
Human ──[triggers impl]──────> Implementation Coordinator
                                       │
                                  [spawns workers]
                                       │
                               Worker 1 ──[agent-mail: "bead done"]──> Coordinator
                               Worker 2 ──[agent-mail: "bead done"]──> Coordinator
                                       │
                                  [all done]
                                       │
                                  [agent-mail: "epic complete"]
                                       │
Human <────────────────────────────────┘
```

### 6. Inbox Polling / Notification

**Problem**: How does a coordinator know when a worker is done?

**Options**:

| Approach | Mechanism | Latency |
|----------|-----------|---------|
| Worker sends agent-mail message | Coordinator polls `fetch_inbox` | Depends on poll interval |
| `ntm --robot-wait --wait-until=idle` | ntm monitors pane state | Real-time, but "idle" != "done" |
| Claude Code hook (`am check-inbox`) | PostToolUse hook checks mail | Fires after each tool use |
| `ntm --robot-attention --attention-condition=mail_pending` | Blocks until mail arrives | Real-time |
| Worker writes to a file, coordinator watches | File-based signaling | Needs fswatch/polling |

**Best combination**: Workers send agent-mail when done. Coordinator uses `ntm --robot-wait` to detect idle state, then checks inbox for status messages.

## Experiments Needed

### Experiment 1: Context Reset
- Spawn an agent, give it a task, let it complete
- Try `ntm respawn` — does the restarted agent have MCP tools?
- Try sending `/clear` — does context actually reset?
- Measure: token count before/after, MCP tool availability

### Experiment 2: Model Switching
- Spawn with `--cc=1:sonnet`
- Verify it's actually using Sonnet (check model in Claude startup banner)
- Test: Can coordinator (sonnet) effectively manage workers?

### Experiment 3: Coordinator Prompt
- Write a coordinator prompt template
- Test with `ntm controller yourproject --prompt coordinator.md`
- Verify it can: read beads, spawn workers, send mail, monitor progress

### Experiment 4: Worker Lifecycle
- Coordinator spawns worker, assigns bead
- Worker implements, commits, sends mail
- Coordinator detects completion (how?)
- Coordinator assigns next bead (respawn vs /clear vs new prompt)

### Experiment 5: End-to-End Pipeline
- Human creates spec + plan, files epic bead
- Planning coordinator processes it
- Implementation coordinator executes it
- Full cycle with real code changes

## Open Questions

1. **Worktrees**: Should each worker get a git worktree (`--worktrees`) to avoid merge conflicts? Or do we serialize tasks?
2. **Rate limits**: With 4+ Claude agents, will we hit API rate limits? ntm has `--stagger-mode=smart` but is it enough?
3. **Error recovery**: What happens when a worker fails mid-task? Does the coordinator retry? Reassign?
4. **Bead updates**: Should workers update bead status (`bd update --claim`, `bd close`) or should the coordinator manage all bead state?
5. **Git coordination**: Multiple agents committing — do we need a merge strategy? Branch-per-worker?
