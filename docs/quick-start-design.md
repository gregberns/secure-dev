# Design: `sd quick-start` -- Agent-Driven Bootstrap

## The One-Liner

A new user pastes this into their coding agent:

> Run `sd quick-start` and follow the setup instructions to create a secure dev environment for this project.

That's it. The agent handles the rest.

## Why This Works

1. **The agent knows the project.** It can read go.mod, package.json, Cargo.toml, Makefiles, CI configs, and infer what tools are needed. No human needs to specify `--modules=golang,docker,github-cli` -- the agent figures it out.

2. **The agent can ask questions.** When there's a real choice (Lima vs Docker, which GitHub repos to scope the PAT to), the agent asks. For everything else, it just does the right thing.

3. **The agent can verify.** After setup, the agent runs commands inside the VM to verify tools are installed, credentials work, and the environment is functional.

4. **One sentence to remember.** Users don't need to learn `sd` commands. They paste one sentence, the agent does the work.

## Output Design

`sd quick-start` outputs a structured Markdown document that serves as an **executable runbook** for the agent. It's not documentation -- it's instructions the agent follows step by step.

### Output of `sd quick-start`

```markdown
# sd Quick Start -- Agent Setup Runbook

You are setting up a secure development VM for the user. Follow these
steps in order. Execute commands, ask the user questions at decision
points, and verify each step before proceeding.

## Prerequisites

Run `sd doctor --json` and check the results.

Required:
- ssh: must be available
- One of: lima or docker backend must be available

If neither backend is available:
- macOS: suggest `brew install lima`
- Linux: suggest installing Docker
- Tell the user what to install and wait for them to do it

## Project Analysis

Examine the current project to determine what tools and runtimes are needed
inside the VM. Look for these files and infer requirements:

| File | Indicates | Module | Extra tools |
|------|-----------|--------|-------------|
| go.mod | Go project | golang | |
| go.sum | Go project | golang | |
| package.json | Node.js project | (base includes node via nvm) | |
| Cargo.toml | Rust project | rust | |
| pyproject.toml | Python project | python | |
| requirements.txt | Python project | python | |
| Dockerfile | Docker usage | docker | |
| .github/workflows/*.yml | GitHub CI | github-cli | gh |
| Makefile | Build system | | parse for tool deps (jq, curl, etc.) |
| .tool-versions | asdf version manager | | specific runtime versions |
| .python-version | pyenv | python | |
| .node-version | nvm/fnm | | specific node version |
| .go-version | Go version | golang | |

Build a list of modules needed. Always include: base, claude-code.
Add language modules based on what you find.

## Backend Selection

Available backends from `sd doctor --json`:
- **lima**: Full Linux VM via Apple Virtualization. Best isolation. ~2 min to create. macOS only.
- **docker**: Linux container. Faster (~10s). Less isolation. Works everywhere.

Present the user with a clear choice:
"I'll create a [lima/docker] VM with [detected modules]. This gives you
[isolation level]. Want me to proceed, or prefer [the other option]?"

If only one backend is available, use it without asking.

## VM Creation

1. Generate .sd.yaml if one doesn't exist:
   ```
   sd init --modules=<detected-modules>
   ```
   Review the generated file. Adjust if your project analysis found
   additional needs.

2. Create and start the VM:
   ```
   sd ensure
   ```
   This reads .sd.yaml and creates the VM if it doesn't exist.

3. Verify the VM is running:
   ```
   sd status <name> --json
   ```

## Credential Setup

Ask the user about credentials they need inside the VM:

### GitHub Access
"Do you need GitHub access inside the VM (for git push, gh CLI, etc.)?"

If yes:
1. Run `sd token github setup` -- this prints guidance for creating a PAT
2. Tell the user to create a fine-grained PAT at https://github.com/settings/tokens
   - Scope it to the specific repository (or repositories) they're working on
   - Required permissions: Contents (read/write), Pull Requests (read/write)
3. Have them export it: `export GITHUB_TOKEN=<their-token>`
4. The token will be injected into the VM at connect time via SSH env vars

### Anthropic API Key (for Claude Code inside VM)
"Do you have an Anthropic API key for running Claude Code inside the VM?"

If yes:
1. Have them export it: `export ANTHROPIC_API_KEY=<their-key>`
2. It will be injected at connect time

If no:
- Skip. They can set it up later.

## Verification

Run these checks to verify the environment is working:

```bash
# VM is running
sd status <name> --json

# Can execute commands
sd exec <name> -- echo "VM is accessible"

# Tools are installed (adjust based on detected modules)
sd exec <name> -- go version        # if golang module
sd exec <name> -- node --version    # if node needed
sd exec <name> -- python3 --version # if python module
sd exec <name> -- cargo --version   # if rust module
sd exec <name> -- gh --version      # if github-cli module
sd exec <name> -- git --version     # always

# Credentials work (if configured)
sd exec <name> -- bash -c 'test -n "$GITHUB_TOKEN" && echo "GitHub token present" || echo "No GitHub token"'
```

Report results to the user. If anything fails, diagnose and fix before
proceeding.

## Handoff

Tell the user:

"Your secure dev environment '<name>' is ready. Here's what was set up:
- Backend: [lima/docker]
- Modules: [list]
- Tools verified: [list of verified tools]
- Credentials: [GitHub: yes/no, Anthropic: yes/no]

To enter the VM:
  sd connect <name>

Inside the VM, you can run `claude` to start coding with Claude Code.

To manage this VM later:
  sd status <name>     -- check status
  sd stop <name>       -- pause (saves resources)
  sd ensure <name>     -- restart anytime
  sd destroy <name> -f -- remove when done"

## Troubleshooting

If any step fails:

| Problem | Diagnosis | Fix |
|---------|-----------|-----|
| `sd doctor` shows missing binary | Tool not installed | Tell user to install it |
| `sd ensure` fails with backend error | Backend not running | Lima: `limactl start`; Docker: start Docker Desktop |
| Provisioning fails | Script error in module | Check `sd logs <name>` for details |
| `sd exec` can't connect | SSH not ready | Wait 10s, retry. Check `sd status <name>` |
| Credential not injected | Not exported on host | Remind user to `export GITHUB_TOKEN=...` before connecting |
```

### Flags

```
sd quick-start              # Output the runbook (agent executes it)
sd quick-start --json       # JSON-structured version of the runbook
sd quick-start --check      # Just check: is this project already set up?
```

`--check` mode is useful for agents that want to verify before re-running setup:
```json
{
  "ok": true,
  "data": {
    "project_config_exists": true,
    "vm_exists": true,
    "vm_running": true,
    "vm_name": "my-app",
    "modules_installed": ["base", "golang", "claude-code"],
    "credentials": { "github": true, "anthropic": false },
    "needs_setup": false
  }
}
```

## What Makes This Different From `sd guide --agent`

| Aspect | `sd guide --agent` | `sd quick-start` |
|--------|-------------------|-------------------|
| Purpose | Reference docs | Executable runbook |
| When to use | Agent needs to look something up | First-time setup |
| Content | All commands, all flags | Step-by-step workflow |
| Decision points | None (informational) | "Ask the user this" |
| Project awareness | None (generic) | "Analyze these files" |
| State | Shows current state | Checks and acts on state |
| Output | Static Markdown | Actionable instructions |

## The Complete User Experience

```
Day 1: User installs sd (brew install sd)
        Pastes "Run sd quick-start and set up this project" into Claude Code
        Agent runs quick-start, analyzes project, asks 1-2 questions
        Agent creates VM, installs tools, sets up credentials
        User runs sd connect and starts working
        
Day 2: User opens project
        Agent runs sd ensure (or sd quick-start --check)
        VM starts in seconds
        User runs sd connect
        
Day N: User is done with project
        sd destroy my-app -f
```

## Implementation Notes

- `sd quick-start` should include the current project analysis hints inline (the file detection table), but the agent does the actual file inspection -- `sd` doesn't analyze the project itself.
- The runbook format should be stable -- agents will build muscle memory for it.
- `--check` mode is cheap (reads .sd.yaml + queries backend) and can be called frequently.
- The output deliberately uses imperative instructions ("Run this", "Ask the user") because it's addressed to the agent, not the human.
