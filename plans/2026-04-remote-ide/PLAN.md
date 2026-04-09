# Plan: Remote IDE/Agent Integration

**Spec**: 007-connection (needs new requirements added)
**Status**: draft
**Priority**: P3 -- enables seamless agent-in-VM workflows
**Branch**: `feat/remote-ide`
**Depends on**: Bug fix for per-VM backend resolution (BUG-3)

## Overview

Enable AI coding tools (Claude Code, Gemini CLI, OpenAI Codex) to connect to `sd` VMs as remote development environments. Each tool has different remote execution capabilities -- `sd` should output connection config in formats each tool can consume.

## Landscape Research (April 2026)

### Claude Code
- **Native SSH support**: Install Claude Code on the remote machine, SSH in, run `claude`.
- **Remote Control**: Bridge local terminal session with claude.ai/code (research preview).
- **MCP SSH**: Third-party MCP server (adremote-mcp) for SSH access.
- **Best integration path**: `sd connect` already opens an SSH+tmux session where `claude` can be launched. Main improvement: output SSH config in a format that allows `claude` to be configured with the VM as its workspace.

### Gemini CLI
- **Native SSH tools**: `ssh_connect`, `ssh_execute`, `ssh_disconnect` built-in tools.
- **Can be installed on remote**: Works in terminal-only, no GUI needed.
- **Sandbox support**: Can sandbox tool execution.
- **Best integration path**: `sd` can output SSH connection details that map to Gemini's SSH tools, or Gemini CLI can be installed inside the VM via a provisioning module.

### OpenAI Codex CLI
- **No native SSH remote support yet**: Feature requested (github.com/openai/codex/issues/11862).
- **Device code auth**: Works in headless/SSH environments.
- **Sandbox**: OS-level firewall rules for network isolation.
- **Best integration path**: Install Codex inside the VM (like Claude Code). SSH in and run it. No remote execution API to integrate with.

## Strategy

Two approaches, both valuable:

### Approach A: "Install agent inside the VM" (provisioning modules)

Add provisioning modules that install each AI agent CLI inside the VM:
- `claude-code` module already exists
- Add `codex` module
- Add `gemini-cli` module

The user provisions the VM with their preferred agent(s), then `sd connect` enters the VM where the agent is ready to use.

### Approach B: "Connect agent to the VM" (SSH config export)

Add an `sd ssh-config` enhancement that outputs connection info in formats consumable by each tool:
- OpenSSH config (already exists via SSH fragments)
- JSON with host/port/user/key (already exists via `sd connect --json`)
- Gemini CLI connection config
- VS Code Remote-SSH config

## Tasks

### Task 1: Add `codex` provisioning module

**File**: `internal/provision/modules/codex.yaml`

Install OpenAI Codex CLI inside the VM:
- Install Node.js (dependency, or add `depends: [base]`)
- Install Codex CLI via npm
- Configure device code auth flow (non-interactive)

**Tests**: Module YAML validates. Dependencies resolve. Checksum fields present.

### Task 2: Add `gemini-cli` provisioning module

**File**: `internal/provision/modules/gemini-cli.yaml`

Install Google Gemini CLI inside the VM:
- Install Node.js (dependency)
- Install Gemini CLI
- Set up sandbox config if applicable

**Tests**: Module YAML validates. Dependencies resolve.

### Task 3: Enhance `sd ssh-config` output

**File**: `internal/cmd/ssh_config.go`

Add format options to `sd ssh-config`:
```bash
sd ssh-config <name>              # OpenSSH config format (existing)
sd ssh-config <name> --format=json    # JSON with all SSH details
sd ssh-config <name> --format=vscode  # VS Code Remote-SSH settings
```

The JSON format should include everything needed to connect:
```json
{
  "host": "127.0.0.1",
  "port": 32768,
  "user": "ubuntu",
  "identity_file": "/Users/greg/.sd/vms/my-app/ssh/id_ed25519",
  "hostname_alias": "sd-my-app",
  "proxy_command": null,
  "transport": "tcp"
}
```

**Tests**: Each format produces valid output. JSON round-trips.

### Task 4: Add `sd guide --agent` integration hints

**File**: `internal/cmd/guide.go` (from guide-command plan)

In the agent guide output, include a section on how to connect IDE tools:
```markdown
## Connecting AI Tools to This VM

### Claude Code (inside VM)
sd connect <name>    # enter VM
claude               # launch Claude Code

### Gemini CLI (SSH tools)
# Use these details with Gemini's ssh_connect:
sd ssh-config <name> --format=json

### VS Code Remote-SSH
sd ssh-config <name> --format=vscode
```

This task depends on the guide command being implemented first.

**Tests**: Guide output includes IDE integration section.

### Task 5: Document the integration patterns

**File**: Update `docs/usage-patterns-and-agent-onboarding.md`

Add a section on each tool's integration path with worked examples.

## Dependency DAG

```
Task 1 (codex module) -- independent
Task 2 (gemini module) -- independent
Task 3 (ssh-config formats) -- independent
Task 4 (guide integration) -- depends on guide-command plan
Task 5 (docs) -- after Tasks 1-4
```

## Testing Strategy

- Module YAML validation tests (existing pattern)
- SSH config format output tests (table-driven)
- Integration: provision codex/gemini module in Docker backend, verify CLI installed
- No live API testing (would require API keys)

## Research Sources

- [Codex CLI reference](https://developers.openai.com/codex/cli/reference)
- [Codex SSH remote request](https://github.com/openai/codex/issues/11862)
- [Gemini CLI sandbox docs](https://geminicli.com/docs/cli/sandbox/)
- [Gemini CLI SSH PR](https://github.com/google-gemini/gemini-cli/pull/4709)
- [Claude Code remote setup](https://smartscope.blog/en/generative-ai/claude/claude-code-remote-access/)
