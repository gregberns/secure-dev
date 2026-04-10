# AGENTS.md — Secure Dev (sd) Project

This file governs how all coding agents operate in this repository. CLAUDE.md is a symlink to this file.

## Project Overview

`sd` (secure-dev) is a Go CLI tool that creates, configures, and manages secure VM environments for running AI coding agents (Claude Code, etc.) with bypass permissions. It automates VM lifecycle, SSH configuration, tool provisioning, credential injection, and session management.

## Cardinal Rules

1. **Spec-driven development.** Every feature MUST have a corresponding spec in `specs/` BEFORE implementation begins. Code that exists without a spec is a violation and subject to removal.
2. **No undocumented behavior.** If code does something the spec doesn't describe, either update the spec or remove the code.
3. **Specs are the source of truth.** When code and spec disagree, the spec wins. Fix the code.
4. **AI-first design.** Every command supports `--json` output. Every interface is non-interactive by default. Every error message is actionable.

## Repository Structure

```
AGENTS.md              # This file (CLAUDE.md symlinks here)
CLAUDE.md              # Symlink -> AGENTS.md
.claude/skills/        # Claude Code skills (slash commands)
  dev-workflow/        # /dev-workflow — spec-driven development lifecycle
docs/                  # Research documents, guides, references
specs/                 # Specifications (source of truth for all features)
  SPEC_FORMAT.md       # Meta-spec: how specs are written
  NNN-name.md          # Individual specs (numbered, kebab-case)
plans/                 # Implementation plans (plans/{yyyy}-{mm}-{name}/)
cmd/sd/                # CLI entry point and Cobra commands
internal/              # All internal packages
  backend/             # VM backend interface and implementations
  config/              # Configuration loading and types
  provision/           # VM provisioning scripts and logic
  ssh/                 # SSH key and config management
  session/             # tmux/session management
  security/            # Egress rules, credential injection
  ui/                  # Terminal output, styling, JSON formatting
go.mod
go.sum
```

## Planning with kerf

This project uses kerf for structured planning. Before implementing non-trivial
changes (new features, refactors, bug investigations), create a kerf work:

  kerf new <codename>

This creates a work on the bench and shows the process to follow. The jig
(process template) guides you through structured passes -- problem space,
decomposition, research, detailed spec, integration, and tasks.

### Key commands

  kerf new <codename>              Create a new work
  kerf show <codename>             See current state + jig instructions for next steps
  kerf status <codename>           Check current status
  kerf status <codename> <status>  Advance to next pass
  kerf shelve <codename>           Save progress when ending a session
  kerf resume <codename>           Pick up where you left off
  kerf square <codename>           Verify the work is complete
  kerf finalize <codename> --branch <name>  Package for implementation

### When to use kerf

- New features or subsystems -> kerf new --jig spec
- Bug investigations -> kerf new --jig bug
- Trivial changes (typos, one-line fixes) -> skip kerf, just make the change

### Workflow

1. kerf new <codename> -- read the output, it tells you exactly what to do
2. Follow each pass: write the artifacts, advance status
3. kerf show <codename> -- if you lose context, this shows where you are
4. kerf shelve / kerf resume -- for multi-session work
5. kerf square -- verify everything is complete
6. kerf finalize -- package into a git branch for implementation

Don't skip the planning process. Measure twice, cut once.

## Development Lifecycle

All significant changes MUST follow this pipeline. Use `/dev-workflow` to execute it.

```
Spec -> Spec Review (3 agents) -> Plan -> Plan Review (3 agents) -> Tasks -> Task Review -> Implement
```

### Phase Summary

| Phase | Artifact | Gate |
|-------|----------|------|
| 1. Spec | `specs/NNN-name.md` | All required sections, status=review |
| 2. Spec Review | 3 agents: architect, critic, qa | All must-fix issues resolved, status=approved |
| 3. Plan | `plans/{yyyy}-{mm}-{name}/PLAN.md` | All reqs traced to tasks, testing plan complete |
| 4. Plan Review | 3 agents: architect, critic, qa | All must-fix issues resolved, status=approved |
| 5. Tasks | Beads created via `bd create` | One bead per plan task, all have acceptance criteria |
| 6. Task Review | 1 agent verifies completeness | All beads match plan and spec |
| 7. Implement | Code + tests + review | See Implementation Process below |

### Implementation Process

Every implementation task follows this process. The orchestrator agent manages it.

```
1. Create bead (bd create)
2. Implement in worktree (ntm spawn --worktrees, or Agent tool with isolation)
3. Run tests -- all must pass
4. STOP -- do NOT commit. Report "ready for review"
5. Review: 3 agents (architect, critic, qa) review the diff
   - Architect: API design, consistency, spec compliance, extensibility
   - Critic: correctness, edge cases, bugs, security, error handling
   - QA: test coverage, spec acceptance criteria, property tests, missing tests
6. Fix must-fix issues from review
7. Re-run tests
8. Merge worktree + commit
9. Close bead (bd close)
```

**Rules:**
- Code is NEVER committed without review. Tests passing is necessary but not sufficient.
- Reviews write findings to `tests/reviews/` for traceability.
- All must-fix issues must be resolved before merge. Should-fix items tracked as follow-up beads.
- When using ntm, review agents run in the SAME tmux session as the orchestrator (not a separate window).
- The orchestrator coordinates: assigns work, triggers reviews, merges results.

### Testing Requirements

Every implementation task MUST include tests:
- **Property-based tests** using `pgregory.net/rapid` for invariants
- **Unit tests** with table-driven patterns and `testify`
- **Integration tests** for cross-component scenarios

### Spec Workflow

#### Writing Specs

1. Read `specs/SPEC_FORMAT.md` for the required structure.
2. Number specs sequentially: `001-architecture.md`, `002-cli.md`, etc.
3. Every spec MUST have: Status, Overview, Requirements (with IDs), Interface definitions (where applicable), Error handling, and Testing strategy.
4. Requirements use the format `REQ-NNN-MMM` where NNN is the spec number and MMM is the requirement number within that spec.

#### Reviewing Specs

Before any spec is considered ready for implementation:
1. Three independent review agents must review the spec (architect, critic, qa).
2. Reviews check for: completeness, internal consistency, cross-spec consistency, implementability, security implications, and testability.
3. All review findings must be resolved in the spec before implementation begins.
4. Review comments and resolutions are tracked in the spec's revision history section.

#### Plans

1. Plans live in `plans/{yyyy}-{mm}-{plan-name}/PLAN.md`.
2. Every spec requirement MUST map to at least one plan task.
3. Every task MUST define its testing component.
4. Plans are reviewed by 3 agents before task creation.

#### Implementing From Specs

1. Read the spec fully before writing any code.
2. Reference requirement IDs in code comments where a requirement is fulfilled: `// REQ-002-003: support --json on all commands`
3. Every public function, interface, and type must trace to a spec requirement.
4. If implementation reveals a spec gap, STOP and update the spec first. Do not implement undocumented behavior.

## Code Patterns

### Go Conventions

- **Entry point**: `cmd/sd/main.go` — minimal, calls `internal/cmd.Execute()`
- **CLI framework**: `github.com/spf13/cobra` + `github.com/spf13/viper`
- **Command files**: One file per command (or command family) in `internal/cmd/`
- **Large commands**: Split across files by concern: `vm.go`, `vm_create.go`, `vm_helpers.go`
- **Interfaces over implementations**: Define interfaces in the consumer package, not the provider
- **Errors**: Sentinel errors with `errors.Is`/`errors.As`. Wrap with context. Never swallow errors silently.
- **Build tags**: Use for platform-specific code: `_darwin.go`, `_linux.go`
- **`//go:embed`**: For bundling default configs, provisioning scripts, templates
- **Testing**: Table-driven tests. `testify` for assertions.

### CLI Conventions

- **All commands support `--json`** for machine-parseable output
- **Non-interactive by default** — no prompts, no editors, no pagers unless explicitly requested
- **Command groups** organized by function (VM management, Connection, Configuration, Diagnostics)
- **Aliases**: Short aliases for common commands (e.g., `c` for `connect`, `ls` for `list`)
- **Prefix matching enabled** via Cobra
- **Exit codes**: 0 success, 1 general error, 2 usage error
- **Stderr for messages, stdout for data** — when `--json` is used, only JSON goes to stdout

### Error Handling

Three error categories:
1. **Fatal**: Print error, exit non-zero. Use for unrecoverable failures.
2. **Warning**: Print warning, continue. Use for non-critical issues.
3. **Silent**: Log only. Use for expected conditions (e.g., "already running").

All errors must be JSON-serializable when `--json` is active.

### Security Patterns

- **Never mount host $HOME** into a VM
- **Never persist credentials to disk** inside a VM — inject at runtime via environment
- **Default-deny egress** — allowlist specific endpoints
- **Scoped tokens only** — no broad PATs, no org-admin tokens
- **Snapshot before destructive operations**

## Agent Operating Instructions

### Session Protocol

1. **On start**: Read this file. Read relevant specs for your task.
2. **Before coding**: Verify a spec exists for what you're implementing.
3. **While coding**: Reference requirement IDs. Run tests frequently.
4. **Before finishing**: Ensure all changes compile, tests pass, and `go vet` is clean.
5. **On handoff**: Push your branch. Summarize what was done and what remains.

### What NOT To Do

- Do NOT use interactive commands (editors, pagers, `git add -i`)
- Do NOT add features not in specs
- Do NOT use emoji in code, comments, or output (use simple unicode like ✓ ✗ for pass/fail)
- Do NOT create files outside the defined project structure without updating AGENTS.md
- Do NOT skip tests or use `-count=1` to hide flaky tests — fix them
- Do NOT use `-f` flags to force operations without understanding why they're needed

### Dependencies and Tools

- Go 1.22+
- Lima (for default VM backend)
- tmux (for session management inside VMs)
- SSH (OpenSSH client)
- git

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:ca08a54f -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd dolt push
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->
