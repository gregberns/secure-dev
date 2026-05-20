# NTM Operations Reference

Validated commands and patterns for multi-agent orchestration with ntm + Agent Mail.

## Session Management

### Spawning Sessions

```bash
# New labeled session (creates yourproject--label)
ntm spawn yourproject --label taskname --cc=3 --no-user

# Add agents to existing session
ntm add yourproject --cc=2

# Add with specific model
ntm add yourproject --cc=1:opus      # Claude Opus
ntm add yourproject --cc=1:sonnet    # Claude Sonnet

# Add with persona (predefined role + model)
ntm add yourproject --persona=architect     # opus, architecture focus
ntm add yourproject --persona=implementer   # sonnet, fast implementation
ntm add yourproject --persona=reviewer      # sonnet, code review
ntm add yourproject --persona=tester        # sonnet, testing focus
ntm add yourproject --persona=documenter    # sonnet, documentation
```

**Gotcha**: `ntm add` adds N panes to the existing count. If you have 3 panes and `add --cc=4`, you get 7 total — not 4.

**Gotcha**: `ntm spawn --prompt "..."` has a bug where it concatenates `claude --{prompt}` without a space. Don't use `--prompt` at spawn time. Instead, spawn without a prompt, wait for WAITING state, then use `ntm send`.

### Session Names

```bash
# Base session
ntm spawn yourproject --cc=2          # session: yourproject

# Labeled session (for parallel swarms)
ntm spawn yourproject --label test-1  # session: yourproject--test-1
```

All subsequent commands must use the full session name (e.g., `yourproject--test-1`).

### Killing and Restarting

```bash
# Restart agents (clears context, fresh Claude Code)
ntm respawn yourproject --panes=3        # restart specific pane
ntm respawn yourproject --type=cc        # restart all Claude agents
ntm respawn yourproject --force          # skip confirmation

# Kill specific panes (use tmux directly)
tmux kill-pane -t "%PANE_ID"

# Find pane IDs
tmux list-panes -t yourproject -F "#{pane_index} #{pane_id} #{pane_title}"

# Kill entire session
ntm kill yourproject
```

`ntm respawn` is the key command for **context reset** — it kills the agent process and starts a fresh Claude Code instance in the same pane.

## Sending Prompts

```bash
# To specific pane
ntm send yourproject --pane=3 "your prompt here"

# To all agents
ntm send yourproject --all "your prompt here"

# To all Claude agents
ntm send yourproject --cc "your prompt here"

# To specific model variant
ntm send yourproject --cc=opus "complex task"
ntm send yourproject --cc=sonnet "simple task"

# From a file
ntm send yourproject --pane=3 --file prompts/task.md

# With file context injection
ntm send yourproject --pane=3 -c src/main.go "Review this file"

# Staggered broadcast (avoids rate limits)
ntm send yourproject --all "prompt" --delay 5s
```

## Monitoring

```bash
# Agent states (WAITING, GENERATING, ERROR, etc.)
ntm activity yourproject
ntm activity yourproject --watch    # live updates

# Stream agent output
ntm watch yourproject               # all panes
ntm watch yourproject --cc          # Claude agents only

# Session summary
ntm summary yourproject --format markdown

# Check if agents are working
ntm --robot-is-working=yourproject

# Wait for all agents to finish
ntm --robot-wait=yourproject --wait-until=idle --timeout=30m

# Wait for state transition (send prompt, wait for completion)
ntm --robot-wait=yourproject --wait-until=idle --transition
```

## Personas and Models

### Built-in Personas

| Persona | Model | Focus |
|---------|-------|-------|
| architect | opus | System design, architecture, complex refactors |
| implementer | sonnet | Fast implementation, feature work |
| reviewer | sonnet | Code review, quality, bug detection |
| tester | sonnet | Test authoring, QA |
| documenter | sonnet | Technical writing, documentation |

### Profile Sets (Pre-built Teams)

| Set | Composition | Use Case |
|-----|-------------|----------|
| backend-team | 4 agents | Full backend development |
| full-stack | 5 agents | Complete development team |
| quick-impl | 2 agents | Fast implementation pair |
| review-team | 3 agents | Code review focused |

```bash
# Use a profile set
ntm spawn yourproject -r backend-team
```

### Model Selection Strategy

- **Opus**: Coordinators needing complex reasoning, architects, spec review
- **Sonnet**: Workers doing implementation, testing, documentation — cheaper and faster
- **Mix**: `ntm add yourproject --cc=1:opus --cc=3:sonnet` for 1 coordinator + 3 workers

## Controller Pattern

ntm has a built-in controller concept — a dedicated agent in pane 1 that coordinates others:

```bash
# Launch controller with default coordination prompt
ntm controller yourproject

# Custom controller prompt
ntm controller yourproject --prompt controller-prompt.md

# Controller prompt supports template variables:
#   {{.Session}}    - session name
#   {{.AgentList}}  - list of other agents
#   {{.ProjectDir}} - project directory path
```

## Coordinator (Automated)

The coordinator is an automated background system (not an agent):

```bash
# Check coordinator status
ntm coordinator status yourproject

# Generate digest (summary of session state)
ntm coordinator digest yourproject

# List file conflicts between agents
ntm coordinator conflicts yourproject

# Trigger work assignment to idle agents
ntm coordinator assign yourproject

# Enable features
ntm coordinator enable auto-assign
ntm coordinator enable digest --interval=30m
```

## Agent Mail Operations

### From Agent (MCP Tools)

Agents use these MCP tools (available automatically if `.mcp.json` is configured):

```
ensure_project(human_key="/abs/path/to/repo")
register_agent(project_key="/abs/path", program="claude-code", model="opus-4")
send_message(project_key="...", sender_name="RusticDuck", to=["BlueLake"], subject="...", body_md="...")
fetch_inbox(project_key="...", agent_name="RusticDuck")
acknowledge_message(project_key="...", agent_name="RusticDuck", message_id=123)
list_agents(project_key="...")
file_reservation_paths(project_key="...", agent_name="RusticDuck", paths=["src/**"], ttl_seconds=3600, exclusive=true)
```

Agent names are auto-generated (adjective+noun: "RusticDuck", "BrownRiver"). You cannot choose names.

### Macro: Session Boot

Agents can use the `macro_start_session` tool to do ensure_project + register in one call:

```
macro_start_session(human_key="/abs/path", program="claude-code", model="opus-4.6")
```

### Checking Mail (for Hooks)

```bash
# CLI check (designed for git hooks / editor integrations)
am check-inbox --agent RusticDuck --project /abs/path --json

# Rate-limited by default (120s between checks)
# Override: --rate-limit 0
```

### Attention System

```bash
# Block until something needs attention (mail, conflicts, stalls)
ntm --robot-attention --attention-condition=mail_pending

# Non-blocking digest
ntm --robot-digest --profile=minimal

# Wait for mail specifically
ntm --robot-wait=yourproject --wait-until=mail_pending
```

## Pane Identification

```bash
# List panes with details
tmux list-panes -t yourproject -F "#{pane_index} #{pane_id} #{pane_title}"

# Capture pane contents (for debugging)
tmux capture-pane -t "%PANE_ID" -p -S -40

# Note: ntm pane indices and tmux pane indices may differ
# Always use tmux list-panes to get the actual pane ID
```

## Worktree Isolation

For agents that modify files, use worktrees to prevent conflicts:

```bash
ntm spawn yourproject --cc=3 --worktrees

# Each agent gets its own git worktree branch: ntm/<session>/<agent>
ntm worktrees list
ntm worktrees merge claude_1    # merge agent's work back
```
