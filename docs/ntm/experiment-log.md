# NTM Experiment Log

Running experiments to validate the coordinator/worker orchestration design.

---

## Experiment 1: Model Switching

**Goal**: Verify that `--cc=1:sonnet` actually spawns a Sonnet agent, not Opus.

**Method**: Spawn an agent with `:sonnet`, capture the startup banner, verify model.

### Results — PASSED

- `ntm add secure-dev --cc=1:sonnet` spawns a Claude agent with `--model 'claude-sonnet-4-6'`
- Startup banner confirms: "Sonnet 4.6 with high effort"
- Pane title includes `_sonnet` suffix (e.g., `secure-dev__cc_3_sonnet`)
- Agent self-reports as `claude-sonnet-4-6` when asked

**Conclusion**: Model switching works as expected. Use `--cc=N:sonnet` or `--cc=N:opus` at add time.

---

## Experiment 2: Context Reset

**Goal**: Does context reset give a clean slate? Do MCP tools survive? Which method works best?

**Method**: Spawn agent, give it tasks (register with agent-mail, read files), then test three reset approaches.

### Results

#### Approach A: `ntm respawn --panes=N --force` — FAILED

- Command reports "Restarted panes: 3" — but Claude does NOT restart
- Pane drops to a bare `zsh` shell prompt
- `ntm activity` shows pane as UNKNOWN
- Tried twice — same result both times
- Root cause: `tmux respawn-pane -k` restarts the shell but does not re-run the Claude launch command
- **Verdict: Do not use `ntm respawn` for context reset — it kills the agent without restarting it**

#### Approach B: `/clear` via `ntm send` — PASSED (RECOMMENDED)

- Sent `/clear` via `ntm send secure-dev --pane=3 "/clear"`
- Post-clear probe confirmed:
  - **Context wiped**: Agent has no memory of previous conversation
  - **MCP tools survived**: All agent-mail tools (register_agent, send_message, fetch_inbox) still available
  - **Agent-mail registration survived**: CoralCat was registered pre-clear, fetch_inbox still works post-clear (registration is server-side)
  - **Model unchanged**: Still claude-sonnet-4-6
- Fast — takes ~2 seconds
- **Verdict: Best option for between-task context reset. Clean, fast, preserves MCP and registrations.**

#### Approach C: Kill pane + `ntm add` — PASSED

- `tmux kill-pane -t "%PANE_ID"` then `ntm add secure-dev --cc=1:sonnet`
- Completely fresh Claude instance with all MCP tools available
- Agent has zero memory (completely new process)
- Must re-register with agent-mail (new agent identity)
- Takes ~8-10 seconds (Claude startup time)
- **Verdict: Use when you need a completely fresh agent (new identity, different model, etc.)**

### Summary Table

| Method | Context Clean? | MCP Tools? | Agent-Mail Reg? | Speed | Use When |
|--------|---------------|------------|-----------------|-------|----------|
| `ntm respawn` | N/A — broken | N/A | N/A | N/A | **Don't use** |
| `/clear` | Yes | Preserved | Preserved | ~2s | Between tasks (same worker) |
| Kill + add | Yes | Fresh load | Must re-register | ~10s | New worker identity needed |

---

## Experiment 3: Worktree Isolation

**Goal**: Can workers operate in isolated git worktrees and have their work integrated back?

**Method**: Test two approaches:
- A) Claude Code's built-in Agent tool with `isolation: "worktree"`
- B) ntm's `--worktrees` flag at spawn time

### Results

#### Approach A: Claude Code Agent tool with `isolation: "worktree"` — PASSED

Spawned a Sonnet subagent with `isolation: "worktree"`:

- Agent got its own branch: `worktree-agent-af3ba5c8`
- Working directory: `.claude/worktrees/agent-af3ba5c8` (under the repo)
- Agent made changes, committed them — all isolated from the parent branch
- Verified from main repo: `worktree-test.txt` did NOT exist in our working directory
- `git worktree list` showed both worktrees correctly
- **Integration via cherry-pick worked cleanly**: `git cherry-pick <hash> --no-commit`
- **Integration via merge**: Conflicts if branches have diverged (standard git behavior — not a problem, just needs planning)
- Cleanup: `git worktree remove <path> && git branch -D <branch>`

**Key findings**:
- Worktree agents automatically get their own branch based on their agent ID
- Changes are completely isolated — parent can't see them until merge/cherry-pick
- The worktree is auto-cleaned if the agent makes no changes; persists if changes were made
- Cherry-pick is better than merge when the worker branch diverges from the coordinator's branch
- Subagent had full tool access (Read, Write, Edit, Bash, Glob, Grep)

#### Approach B: ntm `--worktrees` at spawn — NOT TESTED (documented)

ntm has built-in worktree support:
```bash
ntm spawn myproject --cc=3 --worktrees           # Each agent gets isolated worktree
ntm worktrees list                                 # View worktrees
ntm worktrees merge claude_1                       # Merge agent work back
ntm worktrees remove claude_1                      # Remove a worktree
ntm worktrees clean                                # Clean up all worktrees for session
```

This only works with `ntm spawn` (not `ntm add`). Each agent gets branch `ntm/<session>/<agent>`.

**Not tested yet** because it would require spawning a new session with `--worktrees`. Will test when we run the full coordinator pipeline.

### Summary

| Approach | Works? | Integration | Cleanup | Best For |
|----------|--------|-------------|---------|----------|
| Claude Agent `isolation: "worktree"` | Yes | cherry-pick or merge | `git worktree remove` + `git branch -D` | Coordinator spawning subagent workers |
| ntm `--worktrees` | Documented | `ntm worktrees merge` | `ntm worktrees clean` | Full ntm-managed pipeline |

**Recommendation**: For coordinator/worker pattern, use Claude's Agent tool with `isolation: "worktree"` — it's well-integrated, gives each worker isolation, and the coordinator can cherry-pick results back. For full ntm-managed pipelines with multiple tmux pane agents, use ntm's `--worktrees` at spawn.

---

## Experiment 4: Coordinator Spawns Workers

**Goal**: Can a coordinator agent spawn isolated workers, delegate tasks, and collect results?

**Method**: Spawn a Sonnet coordinator in a tmux pane. Instruct it to:
1. Register with agent-mail
2. Spawn a worker subagent with worktree isolation via the Agent tool
3. Worker creates a file and commits
4. Coordinator checks whether worker's changes are visible
5. Coordinator reports via agent-mail

### Results — PASSED

The coordinator (registered as "AzureOwl" via agent-mail):

1. **Agent-mail registration**: Auto-generated name required — "BrightCoordinator" was rejected (must be adjective+noun like "AzureOwl"). Coordinators should omit the `name` parameter and let the system generate one.

2. **Worker spawned successfully**: Used Claude's Agent tool with `isolation: "worktree"`. Worker ran on branch `worktree-agent-a3ec5616` in `.claude/worktrees/agent-a3ec5616/`.

3. **Worker completed task**: Created `experiment-4-result.txt`, ran git status. Worker had full tool access (Read, Write, Bash, etc.).

4. **Isolation confirmed**: Coordinator verified the worker's file was NOT visible at `/Users/gb/github/secure-dev/experiment-4-result.txt` — only in the worktree path.

5. **Agent-mail report sent**: Message ID 3, AzureOwl→AzureOwl (self-send, since HumanOverseer naming is incompatible with Rust agent-mail).

**Key findings**:

| Aspect | Result |
|--------|--------|
| Coordinator spawns worker via Agent tool | Works |
| Worker gets worktree isolation | Works (automatic branch creation) |
| Worker changes isolated from coordinator | Confirmed |
| Worker results returned to coordinator | Via Agent tool return value |
| Agent-mail reporting | Works (with auto-generated names) |
| Total time (coordinator task) | ~63 seconds |

**Important discovery**: Communication between coordinator and worker happens through the **Agent tool return value**, not shared filesystem state. The coordinator receives the worker's text output when the Agent call completes. This is the primary data channel.

**Agent-mail naming constraint**: The Rust agent-mail enforces adjective+noun format (e.g., "GreenLake", "AzureOwl"). Descriptive names like "Coordinator" or "Worker1" are rejected. Agents must either use auto-generated names or follow the format strictly. This complicates the "HumanOverseer" pattern — need a workaround for human-addressed messages.

---
