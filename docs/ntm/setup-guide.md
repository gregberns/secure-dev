# NTM + Agent Mail Setup Guide

How to set up ntm (Named Tmux Manager) and Agent Mail for multi-agent orchestration.

## Prerequisites

- tmux
- Claude Code (`claude` CLI)
- Go (for building project binaries)
- Homebrew (macOS)

## 1. Install NTM

```bash
brew install dicklesworthstone/tap/ntm   # or however you installed it
```

Verify:
```bash
ntm --version
ntm deps              # shows required/optional dependencies
```

Global config lives at `~/.config/ntm/config.toml`. Minimal config:
```toml
projects_base = "/Users/you/github"
```

The `projects_base` is where ntm looks for project directories. Session names map to directory names under this base.

## 2. Install Agent Mail (Rust)

Agent Mail is a **separate service** — not built into ntm. It provides inter-agent messaging via MCP.

### Download and install

The official installer has a bug on fresh installs (v0.2.32) — duplicate column in SQLite schema. Use this workaround:

```bash
# Download installer
curl -fsSL "https://raw.githubusercontent.com/Dicklesworthstone/mcp_agent_mail_rust/main/install.sh" -o /tmp/am-install.sh

# Fix the bash 3.x compatibility bug (empty array expansion)
# Line 648: rc_files+=("${sourced_files[@]}") fails when array is empty
# Replace with:
#   if [ ${#sourced_files[@]} -gt 0 ]; then
#     rc_files+=("${sourced_files[@]}")
#   fi

# Run installer
bash /tmp/am-install.sh
```

This installs:
- `~/.local/bin/am` — operator CLI
- `~/.local/bin/mcp-agent-mail` — MCP server binary (stdio mode)

### Fix the SQLite schema bug (v0.2.32)

On first run, `am` creates a database with duplicate columns in the `agents` table. Fix it:

```bash
# Start the server once to create the DB, then kill it
am serve-http --no-tui --no-auth &
sleep 3
kill %1

# Apply the schema fix
sqlite3 ~/Library/Application\ Support/mcp-agent-mail/git_mailbox_repo/storage.sqlite3 "
PRAGMA writable_schema=ON;
UPDATE sqlite_master
SET sql='CREATE TABLE agents(id INTEGER PRIMARY KEY AUTOINCREMENT, project_id INTEGER NOT NULL, name TEXT NOT NULL, program TEXT NOT NULL, model TEXT NOT NULL, task_description TEXT NOT NULL DEFAULT '''', inception_ts INTEGER NOT NULL, last_active_ts INTEGER NOT NULL, attachments_policy TEXT NOT NULL DEFAULT ''auto'', contact_policy TEXT NOT NULL DEFAULT ''auto'', reaper_exempt INTEGER NOT NULL DEFAULT 0, registration_token TEXT DEFAULT NULL, UNIQUE (project_id, name), FOREIGN KEY(project_id) REFERENCES projects(id))'
WHERE type='table' AND name='agents';
PRAGMA writable_schema=OFF;
"

# Verify
sqlite3 ~/Library/Application\ Support/mcp-agent-mail/git_mailbox_repo/storage.sqlite3 "PRAGMA integrity_check;"
# Should output: ok
```

### Start the server

```bash
# Headless mode (for background operation)
nohup am serve-http --no-tui --no-auth > /tmp/am-server.log 2>&1 &

# Or with TUI (interactive dashboard)
am serve-http --no-auth
```

Server runs on `http://127.0.0.1:8765`. The `--no-auth` flag disables bearer token auth for local development.

Verify:
```bash
curl -s -X POST http://127.0.0.1:8765/mcp/ \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' | head -c 200
```

## 3. Configure Claude Code MCP Connection

**Critical**: Claude Code must connect to Agent Mail via **HTTP transport**, not stdio. The stdio binary (`mcp-agent-mail`) and the HTTP server (`am serve-http`) both lock the same SQLite database — they cannot run simultaneously.

The installer writes stdio configs to `.claude/settings.json` and `.claude/settings.local.json`. **Remove those** and use HTTP instead:

```bash
# Remove any stdio-based configs the installer added
# Edit .claude/settings.json and .claude/settings.local.json
# Remove the mcp-agent-mail entries from mcpServers

# Add HTTP-based config (per-project)
claude mcp add --transport http --scope project agent-mail http://127.0.0.1:8765/mcp/
```

This creates `.mcp.json` in the project root:
```json
{
  "mcpServers": {
    "agent-mail": {
      "type": "http",
      "url": "http://127.0.0.1:8765/mcp/"
    }
  }
}
```

**Every new Claude Code session** in this project directory will now have agent-mail MCP tools available (`ensure_project`, `register_agent`, `send_message`, `fetch_inbox`, etc.).

## 4. Initialize NTM for the Project

```bash
cd /your/project
ntm init --no-hooks    # creates .ntm/ directory
```

## 5. Verify the Full Stack

```bash
# 1. Ensure am server is running
lsof -i :8765

# 2. Add a test agent
ntm add yourproject --cc=1

# 3. Wait for it to be ready
ntm activity yourproject

# 4. Send a test prompt
ntm send yourproject --pane=N "Use your agent-mail MCP tools to call ensure_project with human_key /path/to/project, then register_agent, then report your assigned name."

# 5. Check mail
curl -s -X POST http://127.0.0.1:8765/mcp/ \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_agents","arguments":{"project_key":"/path/to/project"}}}' | python3 -m json.tool
```

## Architecture Diagram

```
You (human) ─── tmux pane 1 ─── Claude Code (primary agent)
                                      │
ntm (orchestrator) ───────────────────┤── tmux pane 2 ─── Claude Code (worker)
  manages sessions                    │── tmux pane 3 ─── Claude Code (worker)
  monitors activity                   │── tmux pane N ─── Claude Code (worker)
  sends prompts                       │
                                      │
Agent Mail (am serve-http:8765) ──────┘
  MCP tools available to all agents
  ensure_project, register_agent
  send_message, fetch_inbox
  file_reservation_paths
  Git-backed message archive
```

## Common Issues

| Problem | Cause | Fix |
|---------|-------|-----|
| `duplicate column name: reaper_exempt` | Schema bug in v0.2.32 | Apply sqlite_master fix (see above) |
| `no message channels available` | ntm can't reach agent-mail | Start `am serve-http` first |
| MCP tools not appearing in Claude session | Stdio/HTTP conflict | Use `claude mcp add --transport http`, remove stdio configs |
| `mailbox activity lock is busy` | Two processes accessing same DB | Only run ONE of: `am serve-http` OR `mcp-agent-mail` (stdio) |
| `ntm send` goes to wrong session | Labeled sessions have compound names | Use full name: `yourproject--label` |
| `claude --%` or `claude --Run` | ntm prompt injection bug | Don't use `--prompt` at spawn; use `ntm send` after agent is WAITING |
| Agent in ERROR state | Claude crashed or failed to start | `ntm respawn yourproject --panes=N` |
