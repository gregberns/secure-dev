# Usage Patterns, Best Practices, and Agent Onboarding

*Design document -- April 2026*

## The Core Question

How should a human and their AI coding agents use `sd`? This doc works through the usage patterns, identifies the one we should optimize for, and designs the agent onboarding experience.

---

## Part 1: Usage Patterns

### Who are the actors?

There are three actors in an `sd` workflow:

1. **The Human** -- the developer who owns the machine, the repositories, and the credentials.
2. **The Host Agent** -- an AI coding agent (Claude Code, Cursor, etc.) running on the human's machine, directly in the terminal or IDE. It has access to `sd` as a CLI tool.
3. **The Guest Agent** -- an AI coding agent running *inside* the VM. It has the tools installed by provisioning (git, language runtimes, Claude Code itself) and injected credentials. This is the agent doing the actual work in the secure sandbox.

### Pattern A: "Human creates, agent works inside"

```
Human: sd create my-project --modules=base,claude-code
Human: sd connect my-project
       (now inside VM)
Human: claude   # launches Claude Code inside the VM
Agent: (works on code, uses git, runs tests -- all inside VM)
```

**The human** manages the VM lifecycle (create, destroy, credential setup). **The agent** runs entirely inside the VM and doesn't know or care about `sd`. It just sees a normal Linux environment with tools installed.

**Pros**: Simple mental model. Agent doesn't need to know about `sd`. Full isolation.
**Cons**: Human does all setup manually. No agent assistance for config/troubleshooting.

### Pattern B: "Host agent assists setup, guest agent works"

```
Human: "Help me set up a secure dev environment for this project"
Host Agent: sd create my-project --backend=lima --modules=base,claude-code,golang
Host Agent: sd token github setup  # guides human through credential setup
Host Agent: sd connect my-project
            (inside VM)
Human: claude   # launches Claude Code inside the VM
Guest Agent: (works on code inside VM)
```

**The host agent** helps the human create, configure, and troubleshoot the VM. **The guest agent** works inside it. The host agent acts as an "IT assistant" that knows how `sd` works.

**Pros**: Agent helps with complex setup. Human doesn't need to memorize commands. Agent can troubleshoot (`sd doctor`, check status).
**Cons**: Requires the host agent to understand `sd` deeply (hence the need for a skill/guide).

### Pattern C: "Agent manages a fleet"

```
Human: "I need sandboxed environments for three different repos"
Host Agent: sd create frontend --modules=base,claude-code,nodejs
Host Agent: sd create backend --modules=base,claude-code,golang
Host Agent: sd create infra --modules=base,claude-code,python
Host Agent: sd token github setup  # one-time for all VMs
```

The host agent orchestrates multiple VMs. Each VM is a separate sandbox for a different project.

### Pattern D: "Agent spawns disposable sandboxes"

```
Host Agent: sd create task-123 --modules=base,claude-code
Host Agent: sd exec task-123 -- git clone https://github.com/user/repo
Host Agent: sd exec task-123 -- claude --print "fix the bug in auth.go"
Host Agent: sd exec task-123 -- git push
Host Agent: sd destroy task-123 --force
```

The host agent creates ephemeral VMs for specific tasks, runs commands via `sd exec`, and destroys them when done. No human enters the VM.

**Pros**: Fully automated. Perfect for CI-like workflows or batch operations.
**Cons**: Slower (VM creation overhead). Requires `sd exec` to be robust.

---

### Recommended Primary Pattern: **B (Host Agent Assists Setup)**

This is the sweet spot for most users:

1. Human wants a secure environment for AI-assisted development.
2. They ask their local agent (Claude Code) for help.
3. The agent uses `sd` to create, configure, and manage the VM.
4. The agent can troubleshoot issues (`sd doctor`, `sd status`).
5. Once set up, the human (or another agent) works inside the VM.

Pattern D (disposable sandboxes) is the secondary pattern -- powerful for automation but more advanced.

---

## Part 2: Best Practices

### VM Topology

**Default: One VM per project/repository.**

Rationale:
- **Isolation**: A bug in one project's environment doesn't affect another.
- **Credential scoping**: You can give different VMs different tokens (e.g., one project uses a GitHub fine-grained PAT scoped to that repo only).
- **Clear lifecycle**: Create when you start working on a project, snapshot before risky operations, destroy when done.
- **Naming**: `sd create my-app` maps naturally to the project.
- **Resource control**: Different projects can have different CPU/memory allocations.

**But this doesn't scale past 2-3 active projects.** VMs are heavy -- each consumes CPU, memory, and disk. Running 5+ Lima VMs simultaneously is impractical on most laptops.

#### Topology options for multi-project work

| Topology | When to use | Trade-offs |
|----------|-------------|------------|
| **1 VM per repo** | 1-3 active projects, different languages/credentials | Best isolation, highest resource cost |
| **1 VM per workspace** | Multiple related repos (e.g., frontend + backend + shared lib) | Mount all repos, share tooling. Less isolation but practical. |
| **1 heavyweight VM** | Many small projects, same language/tools | Create one big VM, clone repos inside it. Least isolation, lowest overhead. |
| **Rotate VMs** | Many projects but only 1-2 active at any time | `sd stop inactive-project`, `sd start active-project`. Only running VMs use CPU/memory. |
| **Docker backend for lightweight tasks** | Quick explorations, CI-like tasks, testing | Faster creation, lower overhead, less isolation than full VMs. |

#### Recommended approach: "Active workspace" model

For most developers working across multiple repos:

```bash
# Create VMs for each major project (they start stopped)
sd create frontend --modules=base,claude-code,nodejs
sd create backend --modules=base,claude-code,golang
sd create infra --modules=base,claude-code,python

# Only run what you're actively working on
sd start backend
sd connect backend

# When switching projects
sd stop backend
sd start frontend
sd connect frontend
```

The key insight: **only run 1-2 VMs at a time**. Stopped VMs cost only disk space. `sd start` is much faster than `sd create` (seconds vs minutes).

For tightly coupled repos (e.g., a monorepo, or frontend+backend that share an API contract), use a single VM with multiple mounts:

```bash
sd create my-stack --modules=base,claude-code,nodejs,golang \
  --mount=~/code/frontend:/home/ubuntu/projects/frontend:rw \
  --mount=~/code/backend:/home/ubuntu/projects/backend:rw \
  --mount=~/code/proto:/home/ubuntu/projects/proto:ro
```

### Credential Lifecycle

```
# One-time setup per VM (or per token rotation)
sd token github setup        # interactive guide for GitHub PAT
export ANTHROPIC_API_KEY=... # set before connecting

# Credentials are injected at connect time via SSH env vars
sd connect my-project        # GITHUB_TOKEN, ANTHROPIC_API_KEY available inside
```

**Rules:**
1. Never store credentials on disk inside the VM.
2. Set credentials on the host, they're injected via SSH `SendEnv` at connect time.
3. Rotate tokens periodically (`sd token rotate`).
4. Use fine-grained PATs scoped to the specific repo, not org-wide tokens.

### Agent Isolation Model

The VM is the security boundary. Inside the VM, the agent has:
- Full sudo access (to install tools, modify config)
- Network access only to allowlisted domains (egress control)
- Injected credentials (scoped, rotatable, not on disk)
- No access to host filesystem (unless explicitly mounted)

The agent **cannot**:
- Read the host's SSH keys or credentials
- Access other VMs
- Reach arbitrary network endpoints
- Persist credentials to disk (they exist only in env vars for the session)

### Multi-Agent Inside a VM

The VM is a single box. One SSH pipe in. Everything runs inside.

The model is: **`sd connect` enters the VM, then you run agents inside it.** Multiple agents share the VM environment, coordinated via tmux:

```
Host                          VM (sd-my-project)
+----------+                  +---------------------------+
| sd       |  --- SSH --->    | tmux session              |
| connect  |                  |  window 0: claude agent 1 |
+----------+                  |  window 1: claude agent 2 |
                              |  window 2: human shell    |
                              +---------------------------+
```

`sd` doesn't know or care how many agents are running inside the VM. That's managed by whatever orchestration tool you use inside (ntm, tmux directly, etc.). The VM provides:
- Isolation from the host
- Shared filesystem for all agents inside
- Shared credentials (injected at connect time)
- Egress control (applies to all processes inside)

This means agent coordination (file locking, task assignment, conflict avoidance) is an **in-VM concern**, not an `sd` concern.

### When to use `sd exec` vs `sd connect`

| Use case | Command | Why |
|----------|---------|-----|
| Human working interactively | `sd connect` | Gets tmux session, full terminal |
| Host agent running a one-off command | `sd exec vm -- cmd` | Non-interactive, returns output |
| Host agent checking something | `sd exec vm -- git status` | Quick check without full session |
| Launching a guest agent | `sd connect` then `claude` | Agent needs interactive terminal |
| Automated pipeline (disposable sandbox) | `sd exec` in a loop | Scripted, no human enters VM |

### Snapshot Discipline

- **Snapshot before risky operations**: `sd snapshot create my-project --tag=before-refactor`
- **Snapshot at known-good states**: After successful provisioning, after getting tests passing
- **Don't hoard snapshots**: Delete old ones when no longer needed

---

## Part 3: Agent Onboarding -- The `sd guide` Command

### Problem

When a human asks their AI agent "help me set up a secure dev environment," the agent needs to know:
1. What `sd` is and what it can do
2. What the current state is (any VMs? dependencies installed?)
3. How to walk the human through setup step by step
4. How to troubleshoot common issues

Today, the agent has no way to learn this without reading source code or docs.

### Solution: `sd guide` command

A new CLI command that outputs structured, agent-consumable instructions. Two modes:

#### `sd guide` (human-readable)

Prints a getting-started guide to stderr:

```
sd: Secure Dev Environment Manager

Quick Start:
  1. Run 'sd doctor' to check prerequisites
  2. Run 'sd create <name>' to create a VM
  3. Run 'sd connect <name>' to enter the VM
  4. Run 'sd token github setup' to configure credentials

Current State:
  VMs: 2 (my-app: running, infra: stopped)
  Backend: lima (available)
  SD_HOME: ~/.sd (permissions OK)

Common Operations:
  sd list                     -- show all VMs
  sd status <name>            -- detailed VM status
  sd exec <name> -- <cmd>     -- run a command in a VM
  sd sync to <name> <path>    -- sync files into a VM
  sd destroy <name> --force   -- remove a VM
```

#### `sd guide --agent` (agent-consumable)

Outputs a structured document (Markdown or JSON) designed to be consumed as a Claude Code skill or system prompt. This is the key innovation -- it makes `sd` self-describing for agents.

```markdown
# sd (Secure Dev) -- Agent Instructions

You have access to `sd`, a CLI tool for managing secure VM environments.
Use it to help the user create, configure, and manage sandboxed development
environments for AI coding agents.

## Current State

<!-- dynamically generated -->
- Backend: lima (available, Docker Desktop also available)
- VMs: my-app (running), infra (stopped)
- SD_HOME: /Users/greg/.sd (permissions: OK)
- Doctor: all checks passing

## Capabilities

### Create a VM
sd create <name> [--backend=lima|docker] [--cpus=N] [--memory=NGiB] [--modules=base,claude-code,...]

Available modules: base, claude-code, docker, golang, rust, python, github-cli
Default modules (auto-applied): base, egress, port-forwarding

### Check Status
sd doctor [--json]         # system health
sd list [--json]           # all VMs
sd status <name> [--json]  # specific VM

### VM Lifecycle
sd start <name>            # start a stopped VM
sd stop <name>             # stop a running VM
sd destroy <name> --force  # remove VM and all data

### Work in a VM
sd connect <name>          # interactive SSH + tmux session
sd exec <name> -- <cmd>    # run a command non-interactively

### File Transfer
sd sync to <name> <local-path> [<guest-path>]    # host -> VM
sd sync from <name> <guest-path> [<local-path>]  # VM -> host

### Credentials
sd token github setup      # guided GitHub PAT configuration
sd token rotate            # rotate credentials
sd token list              # show configured credentials

### Security
sd config egress list      # show allowed domains
sd config egress add <domain>  # allow a domain
sd security status         # security posture summary
sd audit                   # view audit log

## Workflow Guide

When the user asks to set up a development environment:
1. Run `sd doctor --json` to verify prerequisites
2. Ask what project/repo they're working on
3. Run `sd create <project-name> --modules=base,claude-code`
   (add golang/rust/python/docker based on the project)
4. Run `sd token github setup` if they need GitHub access
5. Tell them to run `sd connect <name>` to enter the VM
6. Inside the VM, they can run `claude` to start coding

When troubleshooting:
1. `sd doctor --json` for system-level issues
2. `sd status <name> --json` for VM-specific issues
3. `sd logs <name>` for backend logs
4. Check egress rules if network issues: `sd config egress list`

## Important Notes
- All commands support --json for structured output
- VMs are isolated: no host $HOME access, egress-controlled, scoped credentials
- Credentials are injected at connect-time via env vars, never written to disk in VM
- One VM per project is the recommended topology
```

### Why this matters

The `--agent` output turns `sd` into a **self-documenting tool**. An agent doesn't need pre-loaded knowledge about `sd` -- it can run `sd guide --agent` and immediately understand what's available, what the current state is, and how to help the user.

This also means:
- Updates to `sd` automatically update the agent's knowledge
- The guide includes dynamic state (current VMs, backend availability)
- No manual maintenance of separate "how to use sd" documentation
- **Replaces the need for a separate Claude Code skill file** -- the command IS the skill

### Help text placement

`sd guide` should be the **first thing agents see**. The root `--help` output should lead with it:

```
sd (secure-dev) - Secure VM environments for AI coding agents

  Get started:  sd guide          # human-readable setup guide
  Agent mode:   sd guide --agent  # full agent-consumable instructions

VM Management:
  create    Create a new VM
  ...
```

This means an agent that runs `sd --help` immediately discovers `sd guide --agent` and can bootstrap its own knowledge.

### Implementation

The `sd guide` command would:
1. Run `sd doctor` checks internally to assess system state
2. Run `sd list` to enumerate current VMs
3. Load the available module list
4. Template the output with current state

The `--agent` variant outputs the full guide. The default (no flag) outputs a shorter human-readable version.

---

## Part 4: CLI Improvements for Agent Comfort

### Current friction points

1. **No discovery mechanism**: An agent has no way to learn what `sd` can do without `--help` on every subcommand. The `sd guide --agent` command solves this.

2. **`sd connect` is interactive-only**: The primary "enter the VM" command spawns an interactive SSH+tmux session. An agent on the host can't use this -- it needs `sd exec` for everything. But `sd exec` is more limited (no persistent session, no tmux).

3. **No "ensure" semantics**: If you want a VM to exist and be running, you must check status, then conditionally create or start. Would be better:
   ```
   sd ensure my-project --modules=base,claude-code
   # Creates if doesn't exist, starts if stopped, no-op if running
   ```

4. **Credential setup is multi-step**: Setting up GitHub tokens requires exporting env vars on the host, then using `sd token github setup`. Could be streamlined.

5. **No project-aware defaults**: If I'm in `/Users/greg/code/my-app`, `sd` could infer the VM name from the directory name, or look for a `.sd.yaml` in the project root.

6. **No status summary**: After `sd create`, the user needs to run `sd status`, `sd doctor`, etc. separately. A "here's everything you need to know" command is missing (the `guide` command addresses this).

### Proposed improvements (prioritized)

#### P0: `sd guide [--agent]`
The self-describing guide command described above. This is the single highest-leverage improvement for agent usability.

#### P1: `sd ensure <name> [flags]`
Idempotent "make sure this VM exists and is running":
- VM doesn't exist -> create it (with provided flags)
- VM exists but stopped -> start it
- VM exists and running -> no-op, report status

This is what an agent would call most of the time. Agents love idempotent operations.

#### P2: Project config file (`.sd.yaml`)
A per-project config file that lives in the repo root:
```yaml
# .sd.yaml
name: my-app          # VM name (default: directory name)
backend: lima
cpus: 4
memory: 8GiB
modules:
  - base
  - claude-code
  - golang
mount:
  - .:~/projects/my-app:rw
allow-egress:
  - pkg.go.dev
  - proxy.golang.org
```

Then `sd create` (with no name) in that directory reads the file and does the right thing. `sd ensure` becomes even more powerful:
```bash
cd ~/code/my-app
sd ensure    # reads .sd.yaml, creates/starts the VM, mounts the project
```

#### P3: `sd connect --json` returns connection info without connecting
Already implemented per the spec. Agents use this to get SSH details for scripting.

#### P4: Smarter `sd exec` with environment
`sd exec` should inject credentials the same way `sd connect` does. Currently unclear if it does.

---

## Part 5: Recommended Implementation Order

1. **`sd guide --agent`** -- highest leverage, unblocks all agent workflows
2. **`sd ensure`** -- makes the common operation idempotent
3. **`.sd.yaml` project config** -- makes per-project setup declarative
4. **Verify `sd exec` credential injection** -- needed for Pattern D
5. **Write the "Getting Started" content** into `sd guide` output
6. **Add `sd guide` output as a Claude Code skill** -- so agents auto-discover it

---

## Part 6: Open Questions & Decisions

### Decided

1. **`sd guide --agent` is dynamic.** It queries live system state (VMs, doctor, modules). Caching optional.

2. **No separate skill file needed.** `sd guide --agent` replaces the need for `.claude/skills/sd-setup.md`. The command IS the skill. Agents discover it via `sd --help`.

3. **Multi-agent is an in-VM concern.** One SSH pipe in, everything runs inside. `sd` provides the VM; tmux/ntm/etc. coordinate agents inside it. Not `sd`'s problem.

### Open

4. **Remote IDE integration**: Claude Code supports SSH remotes. Codex and Gemini CLI may also support remote execution. Should `sd` output config that lets these tools treat the VM as a remote? What does each tool need?
   - Claude Code: SSH config (host, port, key, user) -- `sd connect --json` may already provide this
   - Codex: needs research -- what remote execution model does it use?
   - Gemini CLI: needs research -- same question
   
5. **Should `sd exec` inject credentials like `sd connect` does?** Currently unclear. Needed for disposable sandbox workflows where the host agent automates everything via `sd exec`.

6. **Should `.sd.yaml` support multi-repo mounts?** E.g., defining that a VM needs repos X, Y, Z mounted. How does this interact with the "active workspace" topology?
