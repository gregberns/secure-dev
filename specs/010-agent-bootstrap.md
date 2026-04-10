# 010: Agent-Driven Bootstrap

| Field        | Value      |
|--------------|------------|
| Status       | draft      |
| Created      | 2026-04-09 |
| Last Updated | 2026-04-09 |
| Authors      | claude     |
| Reviewers    |            |

## Overview

This spec defines the `sd quick-start` command, which outputs an agent-executable runbook for first-time project setup. The runbook is addressed to an AI coding agent, not to a human. It tells the agent what commands to run, what to explain to the user, and what questions to ask the user. The runbook weaves security education into the setup flow in plain language so that any developer understands why the environment is designed the way it is -- without needing security expertise.

## Goals

- G1: Provide a single command (`sd quick-start`) that outputs a structured runbook an AI agent can follow to set up a secure development environment from scratch
- G2: Embed security education at each step in plain, jargon-free language that agents relay to users naturally during the setup conversation
- G3: Support idempotent setup assessment via `--check` so agents can skip setup if it is already complete
- G4: Establish a `hints` system for contextual best practices in JSON output across all `sd` commands
- G5: Guide agents through collaborative project analysis where the agent inspects the project and works with the user to determine what the environment needs
- G6: Enforce clone-not-mount as the default workflow, with clear explanation of why

## Non-Goals

- NG1: Interactive wizards or multi-step CLI flows -- the runbook is static Markdown, not a stateful process
- NG2: Automatic dependency detection by `sd` itself -- the agent performs project analysis using its own judgment; `sd` provides guidance only
- NG3: Defining the `.sd.yaml` schema or package declaration format (see [009-declarative-environment.md](009-declarative-environment.md) and [005-configuration.md](005-configuration.md))
- NG4: Defining VM backend internals or provisioning module behavior
- NG5: Supporting a specific AI coding tool -- the runbook is tool-agnostic and works with any agent that can read Markdown and execute commands
- NG6: `sd` installation -- the runbook assumes `sd` is already installed

## Requirements

### REQ-010-001: Quick-Start Command Registration

The CLI MUST provide an `sd quick-start` command in the "Getting Started" command group alongside `sd guide`.

**Acceptance criteria:**
- [ ] `sd quick-start` is registered as a command under the "Getting Started" group
- [ ] `sd help` displays `quick-start` under the "Getting Started" group header
- [ ] Running `sd quick-start` with no flags outputs the runbook to stdout
- [ ] The command MUST support `--json` for structured output (as specified in REQ-002-012)
- [ ] The command file MUST be located at `internal/cmd/quick_start.go` (as specified in REQ-002-016)

### REQ-010-002: Quick-Start Flags

The `sd quick-start` command MUST accept the following flags:

| Flag | Default | Description |
|------|---------|-------------|
| (none) | N/A | Output the full agent-executable runbook as Markdown to stdout |
| `--check` | `false` | Output a JSON assessment of setup completeness instead of the runbook |
| `--json` | `false` | Structured JSON output (global flag, per REQ-002-010) |

When `--check` is provided, the command MUST output the setup assessment (see REQ-010-011) regardless of whether `--json` is also set. The `--check` output is always JSON.

When neither `--check` nor `--json` is provided, the command MUST output the runbook as Markdown to stdout.

When `--json` is provided without `--check`, the command MUST output the runbook content wrapped in the standard JSON envelope: `{"ok": true, "data": {"runbook": "<markdown content>"}}`.

**Acceptance criteria:**
- [ ] `sd quick-start` outputs Markdown to stdout
- [ ] `sd quick-start --check` outputs JSON assessment to stdout
- [ ] `sd quick-start --json` outputs the runbook wrapped in `{"ok": true, "data": {"runbook": "..."}}`
- [ ] `sd quick-start --check --json` outputs the same JSON assessment as `--check` alone (both produce JSON)

### REQ-010-003: Runbook Output Format

The runbook MUST be valid Markdown addressed to the AI agent as the executor. The runbook MUST NOT be addressed to the human user. The agent is responsible for interpreting the runbook, executing commands, and communicating with the user.

Each section of the runbook MUST contain one or more of the following block types, clearly labeled:

- **Instructions** -- directives to the agent about what to do
- **Commands** -- exact `sd` or shell commands for the agent to execute
- **Questions** -- suggested phrasing for questions the agent should ask the user
- **Explanations** -- plain-language text the agent should relay to the user

The runbook MUST contain exactly 10 sections, in the order specified by REQ-010-004 through REQ-010-013.

**Acceptance criteria:**
- [ ] The runbook output is valid Markdown
- [ ] Every section contains at least one labeled block (Instructions, Commands, Questions, or Explanations)
- [ ] The runbook is addressed to "you" (the agent), not to "the user"
- [ ] When the runbook references the user, it uses phrasing like "ask the user" or "explain to the user"

### REQ-010-004: Section 1 -- Introduction and Education

The first section MUST instruct the agent to explain to the user what is about to happen and why, before any commands are run.

The agent MUST be instructed to convey these concepts in plain language:

1. `sd` creates a separate development workspace (a virtual machine or container) that is isolated from the user's personal files, credentials, and system configuration
2. This isolation protects the user when AI agents work with code that might contain malicious content or behave unexpectedly
3. The setup takes a few minutes and the agent will handle the technical steps

The section MUST NOT use the following terms without defining them in context: "egress", "PAT", "fine-grained token", "attack surface", "blast radius", "trust boundary", "lateral movement". When a concept requires a technical term, the runbook MUST first explain the concept in everyday language and then optionally introduce the term.

The tone MUST be conversational and brief -- not a security lecture. Two to four sentences of explanation to the user is sufficient.

**Acceptance criteria:**
- [ ] Section 1 instructs the agent to explain workspace isolation to the user
- [ ] Section 1 explains the purpose of isolation (protection from malicious code)
- [ ] Section 1 does not use undefined jargon
- [ ] Section 1 provides suggested explanation text that is 2-4 sentences

### REQ-010-005: Section 2 -- Prerequisites

The second section MUST instruct the agent to verify that system prerequisites are met by running `sd doctor --json` and interpreting the results.

The section MUST include:

- The command to run: `sd doctor --json`
- Instructions for interpreting the JSON output (what "ok" means, what failed checks mean)
- For each possible failed check, an explanation the agent should give the user about what to install and how
- A directive to proceed only when all checks pass

**Acceptance criteria:**
- [ ] Section 2 includes the command `sd doctor --json`
- [ ] Section 2 provides interpretation guidance for the doctor output
- [ ] Section 2 includes remediation instructions for each possible failed check
- [ ] Section 2 instructs the agent to stop and assist the user if prerequisites are missing

### REQ-010-006: Section 3 -- Project Analysis

The third section MUST instruct the agent to analyze the current project to determine what runtimes, tools, and packages the environment needs. The agent performs this analysis -- `sd` does not.

The section MUST include:

1. A detection table mapping files to what they indicate:

| File | What It Indicates | Suggested Module or Package |
|------|-------------------|-----------------------------|
| `go.mod` / `go.sum` | Go project | `golang` module |
| `package.json` / `yarn.lock` / `pnpm-lock.yaml` | Node.js project | `nodejs` module |
| `Cargo.toml` / `Cargo.lock` | Rust project | `rust` module |
| `pyproject.toml` / `requirements.txt` / `setup.py` / `Pipfile` | Python project | `python` module |
| `Dockerfile` / `docker-compose.yml` | Docker usage | `docker` module |
| `.github/workflows/*.yml` | GitHub CI | `github-cli` module |
| `Makefile` / `Justfile` | Build system | Agent should read and infer tool dependencies |
| `Gemfile` | Ruby project | `apt: ruby-full` package |
| `pom.xml` / `build.gradle` / `build.gradle.kts` | Java/Kotlin project | `apt: openjdk-*-jdk` package |
| `.csproj` / `*.sln` | .NET project | `apt: dotnet-sdk-*` package |

2. An explicit instruction that the detection table is a starting point, not exhaustive. The agent MUST use its own knowledge of the codebase (reading Makefiles, CI configs, README files, scripts) to identify additional dependencies.

3. An instruction to present findings to the user and ask for confirmation and additions. Suggested phrasing: "I found this is a [language] project that uses [tools]. I'll set up [list]. Are there other tools or packages you need in the development environment?"

4. An instruction that the agent MUST NOT silently assume what the user needs. The user's input is required before proceeding.

**Acceptance criteria:**
- [ ] Section 3 includes the detection table with at least the 10 entries listed above
- [ ] Section 3 instructs the agent to use its own judgment beyond the table
- [ ] Section 3 instructs the agent to present findings and ask the user for confirmation
- [ ] Section 3 explicitly states the agent must not proceed without user input on dependencies

### REQ-010-007: Section 4 -- Backend Selection

The fourth section MUST instruct the agent to determine which VM backend to use based on the output of `sd doctor --json`.

The section MUST include:

- If only one backend is available: use it and inform the user which one was selected
- If multiple backends are available: present the choice to the user with plain-language trade-offs. The runbook MUST provide suggested explanations:
  - Lima: "Creates a full virtual machine -- more isolated from your system, takes a couple of minutes to set up"
  - Docker: "Creates a container -- faster to set up, slightly less isolated"
- The agent MUST ask the user which they prefer, not choose for them (unless only one is available)

**Acceptance criteria:**
- [ ] Section 4 references `sd doctor --json` output for backend availability
- [ ] Section 4 provides plain-language descriptions of each backend option
- [ ] Section 4 instructs the agent to ask the user when multiple backends are available
- [ ] Section 4 instructs the agent to auto-select and inform when only one backend is available

### REQ-010-008: Section 5 -- Environment Configuration

The fifth section MUST instruct the agent to generate or update a `.sd.yaml` configuration file for the project.

The section MUST include instructions to:

1. Set `repo` to the project's git remote URL (detected from `.git/config` or by running `git remote get-url origin`)
2. Set `modules` based on the runtimes detected in Section 3
3. Set `packages` based on the tools and packages identified in Section 3 and confirmed by the user (per the format defined in [009-declarative-environment.md](009-declarative-environment.md))
4. Set `allow_egress` entries if the project needs access to specific domains beyond the defaults (per [004-security.md](004-security.md))
5. Show the generated configuration to the user and ask for confirmation before writing: "Here's the environment configuration I'll use. Want me to adjust anything?"

The section MUST include a security explanation for the agent to relay about network restrictions. Suggested phrasing: "The workspace can only connect to approved websites like GitHub and package registries. This is a safety net -- if something tries to send data somewhere unexpected, it gets blocked. If you run into issues with a site being blocked, we can add it to the approved list."

**Acceptance criteria:**
- [ ] Section 5 instructs the agent to detect the git remote URL
- [ ] Section 5 maps detected runtimes to `.sd.yaml` modules
- [ ] Section 5 maps detected tools to `.sd.yaml` packages
- [ ] Section 5 instructs the agent to show the config and ask for user confirmation
- [ ] Section 5 includes a plain-language explanation of network restrictions

### REQ-010-009: Section 6 -- VM Creation

The sixth section MUST instruct the agent to create and start the VM using `sd ensure`.

The section MUST include:

- The command to run: `sd ensure` (which reads `.sd.yaml` and creates/starts the VM)
- An instruction to tell the user that creation is in progress and approximately how long it takes: "Creating your development workspace. This takes about [a couple of minutes for Lima / a few seconds for Docker]."
- Instructions for interpreting the output and handling errors

**Acceptance criteria:**
- [ ] Section 6 includes the `sd ensure` command
- [ ] Section 6 provides estimated creation time per backend
- [ ] Section 6 instructs the agent to communicate progress to the user
- [ ] Section 6 includes error handling guidance

### REQ-010-010: Section 7 -- Credential Setup

The seventh section MUST instruct the agent to guide the user through setting up credentials. This section MUST include security education.

The section MUST cover:

**GitHub access** (when the repo is on GitHub or the `github-cli` module is included):

1. The agent MUST explain why a separate token is needed, in plain language. The runbook MUST provide this explanation: "We'll create a token that only has access to this specific project. This limits what can happen if something goes wrong, and prevents the AI from accidentally making changes beyond this repository."
2. The agent MUST walk the user through creating a fine-grained personal access token:
   - Direct the user to `https://github.com/settings/tokens?type=beta`
   - Instruct the user to scope the token to the specific repository only
   - Instruct the user to grant only "Contents" (read/write) and "Pull requests" (read/write) permissions
   - The term "fine-grained personal access token" MUST be introduced after the plain-language explanation of what it is: "GitHub calls this a 'fine-grained personal access token' -- it's a token you can limit to specific repositories and specific actions."
3. The agent MUST instruct the user to export the token: `export GITHUB_TOKEN=<token>`

**API keys** (for AI tools inside the VM):

1. The agent MUST ask the user which AI service API keys they need (Anthropic, OpenAI, Google, etc.) -- the agent MUST NOT assume
2. The agent MUST instruct the user to export the relevant key (e.g., `export ANTHROPIC_API_KEY=<key>`)
3. The agent MUST explain that these keys are passed into the workspace at connect time and are not stored inside it: "These keys are passed into the workspace each time you connect. They're never saved to disk inside it, so if the workspace is compromised, the keys aren't there to find."

**Acceptance criteria:**
- [ ] Section 7 provides plain-language explanation of why scoped tokens are needed
- [ ] Section 7 walks through fine-grained PAT creation with specific steps
- [ ] Section 7 defines the term "fine-grained personal access token" after the plain-language explanation
- [ ] Section 7 specifies the minimal token scopes (Contents, Pull requests, single repo)
- [ ] Section 7 instructs the agent to ask about API keys, not assume
- [ ] Section 7 explains that credentials are injected at connect time, not persisted

### REQ-010-011: Section 8 -- Repo Clone and Verification

The eighth section MUST instruct the agent to clone the project repository inside the VM and verify that tools work correctly.

The section MUST include:

1. A security explanation about clone-not-mount for the agent to relay. Suggested phrasing: "We're cloning the repository inside the workspace instead of sharing your local copy. This means the workspace doesn't have access to your personal files, environment variables, or credentials that might be on your laptop. Even if the AI encounters something malicious in the code, your personal stuff stays safe." (Cross-reference: [004-security.md](004-security.md) for the security rationale behind clone-not-mount.)

2. The clone command:
   ```
   sd exec <name> -- git clone <repo-url> ~/projects/<name>
   ```
   This works because `sd exec` injects credentials configured on the host (per REQ-007-019).

3. An instruction to ask the user if they have a branch to check out:
   - Suggested phrasing: "Do you have a branch you'd like to check out, or should we work on the default branch?"
   - If yes: `sd exec <name> -- git -C ~/projects/<name> checkout <branch>`

4. Verification commands for each detected runtime:
   ```
   sd exec <name> -- go version           # if Go
   sd exec <name> -- python3 --version    # if Python
   sd exec <name> -- node --version       # if Node.js
   sd exec <name> -- rustc --version      # if Rust
   sd exec <name> -- docker --version     # if Docker
   ```

5. An instruction that if any verification command fails, the agent MUST troubleshoot before proceeding (check provisioning logs, re-run provisioning, or ask the user for help).

**Acceptance criteria:**
- [ ] Section 8 provides the clone-not-mount security explanation in plain language
- [ ] Section 8 cross-references 004-security.md for the clone-not-mount rationale
- [ ] Section 8 includes the `sd exec` clone command
- [ ] Section 8 instructs the agent to ask about branch checkout
- [ ] Section 8 includes verification commands for detected runtimes
- [ ] Section 8 includes troubleshooting guidance for verification failures

### REQ-010-012: Section 9 -- AI Tool Setup

The ninth section MUST instruct the agent to set up AI coding tools inside the VM based on the user's preference.

The section MUST include:

1. An instruction to ask the user what AI coding tool they want inside the VM. Suggested phrasing: "What AI coding tool do you use? Claude Code, Codex, Gemini CLI, or something else? I can set up whichever you prefer."
2. The agent MUST NOT assume which tool the user wants
3. If the selected tool's provisioning module is not already in `.sd.yaml`, the agent MUST add it and re-provision:
   - Update `.sd.yaml` to include the module
   - Run `sd provision <name> --modules <tool-module>` to install it
4. If the user does not want an AI tool inside the VM (e.g., they run it on the host and connect via SSH), the agent MUST skip this step

**Acceptance criteria:**
- [ ] Section 9 instructs the agent to ask the user which AI tool to install
- [ ] Section 9 explicitly states the agent must not assume the user's tool preference
- [ ] Section 9 includes instructions to update `.sd.yaml` and re-provision if needed
- [ ] Section 9 handles the case where the user does not want an AI tool inside the VM

### REQ-010-013: Section 10 -- Handoff

The tenth and final section MUST instruct the agent to summarize what was set up and tell the user how to use the environment going forward.

The section MUST instruct the agent to communicate:

1. How to enter the workspace: `sd connect <name>` (or `sd c <name>`)
2. What is inside: the list of installed runtimes, tools, and the cloned repository
3. How to manage the workspace: `sd stop <name>` to pause, `sd ensure` to restart, `sd destroy <name>` to remove
4. A reminder that credentials are injected fresh each time they connect -- if they rotate a token, they just export the new value and reconnect
5. How to sync changes back to the host if needed: `sd sync from <name> <path>`

**Acceptance criteria:**
- [ ] Section 10 tells the user how to connect
- [ ] Section 10 summarizes what was installed
- [ ] Section 10 covers stop/ensure/destroy lifecycle commands
- [ ] Section 10 explains credential refresh behavior
- [ ] Section 10 mentions file sync for getting changes back

### REQ-010-014: Security Education Requirements

Across all sections of the runbook, security explanations MUST meet these requirements:

1. Every security concept MUST be explained in plain language before any technical term is introduced
2. The following terms MUST NOT appear without a preceding plain-language definition in the same section:
   - "egress" -- explain as "which websites or services the workspace can connect to"
   - "PAT" or "personal access token" -- explain as "a special password for GitHub that you can limit to specific projects and actions"
   - "fine-grained token" -- explain as "a token you can restrict to only certain repositories and certain types of access"
   - "credential injection" -- explain as "passing your keys and tokens into the workspace when you connect"
   - "attack surface" -- explain as "the number of ways something could go wrong"
   - "lateral movement" -- explain as "an attacker getting from one system to another"
3. Security explanations MUST be woven into the relevant step, not collected into a separate "security" section
4. Each explanation MUST be 1-4 sentences. No walls of text.

**Acceptance criteria:**
- [ ] No jargon term appears in the runbook without a preceding plain-language definition
- [ ] Security explanations appear at the point of relevance, not in a separate section
- [ ] Each individual explanation is 1-4 sentences
- [ ] The terms listed above are defined before use in every section where they appear

### REQ-010-015: Check Flag -- Setup Assessment

The `--check` flag MUST cause `sd quick-start` to output a JSON assessment of whether setup is already complete for the current project directory.

The JSON output MUST conform to this structure:

```json
{
  "ok": true,
  "data": {
    "sd_yaml_exists": true,
    "vm_exists": true,
    "vm_running": true,
    "vm_name": "my-app",
    "backend": "lima",
    "modules": ["base", "claude-code", "golang"],
    "packages_declared": {"apt": 3, "pip": 0, "npm": 1},
    "credentials": {
      "github_token": true,
      "anthropic_api_key": false
    },
    "repo_cloned": true,
    "needs_setup": false,
    "issues": []
  }
}
```

Field definitions:

| Field | Type | Description |
|-------|------|-------------|
| `sd_yaml_exists` | boolean | Whether a `.sd.yaml` file exists in the current directory or any parent |
| `vm_exists` | boolean | Whether a VM matching the `.sd.yaml` configuration exists |
| `vm_running` | boolean | Whether the VM is currently running |
| `vm_name` | string | The name of the VM (from `.sd.yaml` or empty string if no config) |
| `backend` | string | The backend in use (from `.sd.yaml` or empty string) |
| `modules` | string[] | List of provisioning modules configured |
| `packages_declared` | object | Count of declared packages by manager (`apt`, `pip`, `npm`, etc.) |
| `credentials` | object | Map of credential names to boolean (true if the environment variable is set on the host) |
| `repo_cloned` | boolean | Whether the project repo is cloned inside the VM (checked via `sd exec <name> -- test -d ~/projects/<name>/.git`) |
| `needs_setup` | boolean | True if any required component is missing or misconfigured |
| `issues` | string[] | Actionable descriptions of what needs attention |

The `credentials` object MUST check at minimum:
- `github_token`: whether `GITHUB_TOKEN` is set in the host environment
- `anthropic_api_key`: whether `ANTHROPIC_API_KEY` is set in the host environment

The `issues` array MUST contain actionable strings when `needs_setup` is true. Examples:
- `"No .sd.yaml found -- run sd quick-start to set up"`
- `"VM exists but is stopped -- run sd ensure to start"`
- `"GITHUB_TOKEN not set -- export it before connecting"`
- `"Repository not cloned inside VM -- connect and clone with sd exec <name> -- git clone <url>"`

When `needs_setup` is false and all checks pass, `issues` MUST be an empty array.

**Acceptance criteria:**
- [ ] `sd quick-start --check` outputs valid JSON matching the structure above
- [ ] All fields are present in the output, even when values are false or empty
- [ ] `needs_setup` is true when any required component is missing
- [ ] `issues` contains actionable descriptions for each problem found
- [ ] `issues` is an empty array when `needs_setup` is false
- [ ] Credential checks test host environment variables
- [ ] `repo_cloned` is verified by checking inside the VM

### REQ-010-016: Hints System for SD Commands

All `sd` commands that produce JSON output MUST support an optional `hints` field in their JSON response. Hints are contextual best practices that agents can relay to users.

The `hints` field MUST:

1. Appear at the top level of the JSON response, alongside `ok` and `data`:
   ```json
   {
     "ok": true,
     "data": { ... },
     "hints": [
       "Export GITHUB_TOKEN on the host before running sd connect to inject credentials into the VM.",
       "If builds fail with connection errors, check blocked domains: sd config egress list"
     ]
   }
   ```
2. Be an array of strings. Each string is a complete, self-contained suggestion.
3. Be contextual to what just happened. For example:
   - After `sd ensure` creates a new VM: hint about credential setup
   - After `sd connect` with no credentials detected: hint about exporting tokens
   - After `sd exec` fails with a network error: hint about egress rules
4. Be phrased so that agents can relay them to users as-is or paraphrase them
5. Be non-critical -- agents MAY ignore hints that are not relevant to their current task
6. Be omitted from the JSON output when there are no hints (the field is optional, not required to be an empty array)

The `hints` field MUST NOT appear in non-JSON (human-readable) output. It is exclusively a machine-readable feature for agent consumption.

Commands MUST NOT change their exit code or `ok` status based on hints. Hints are informational only.

**Acceptance criteria:**
- [ ] JSON output from `sd` commands MAY include a `hints` array at the top level
- [ ] Each hint is a self-contained, actionable string
- [ ] Hints are contextual to the command that was run and its outcome
- [ ] The `hints` field is absent (not an empty array) when there are no hints
- [ ] Hints do not appear in human-readable output
- [ ] Hints do not affect exit codes or `ok` status

### REQ-010-017: Clone-Not-Mount as Default Workflow

The runbook MUST establish clone-not-mount as the default workflow for getting project code into the VM. This means the project repository is cloned from its remote inside the VM, rather than mounting the host directory.

The runbook MUST NOT include instructions to mount the host project directory into the VM. If the user has uncommitted work, the runbook MUST instruct the agent to tell the user to push their work to a branch first, then clone and check out that branch inside the VM.

The security rationale (per [004-security.md](004-security.md)) MUST be explained in plain language in Section 8 (REQ-010-011). The explanation MUST NOT assume the user understands concepts like "trust boundary" or "host isolation" -- it must use everyday language.

**Acceptance criteria:**
- [ ] The runbook uses `git clone` inside the VM, not host directory mounts
- [ ] The runbook does not include any `--mount` instructions for the project directory
- [ ] The runbook handles the case where the user has uncommitted work (push to branch first)
- [ ] The security rationale is explained in plain language

### REQ-010-018: Runbook Stability

The runbook MUST reference `sd` commands generically where possible so that the runbook remains valid as command implementations evolve. The runbook MUST NOT embed version-specific output formats or hardcode paths that may change between releases.

The runbook MUST reference `sd guide --agent` as the authoritative command reference for detailed `sd` usage beyond what the runbook covers.

**Acceptance criteria:**
- [ ] The runbook does not hardcode version-specific output formats
- [ ] The runbook references `sd guide --agent` for detailed command documentation
- [ ] Command examples in the runbook use `<name>` or `<vm-name>` as placeholders, not hardcoded names

## Design

### CLI Surface

```
sd quick-start             # Output the agent-executable runbook (Markdown)
sd quick-start --check     # Output JSON assessment of setup completeness
sd quick-start --json      # Runbook wrapped in JSON envelope
```

### Command Registration

The `quick-start` command MUST be registered in the "Getting Started" command group. This group MUST be added to the root command alongside the existing groups defined in REQ-002-002:

```go
rootCmd.AddGroup(
    &cobra.Group{ID: "getting-started", Title: "Getting Started"},
    // ... existing groups ...
)
```

The command file MUST be `internal/cmd/quick_start.go`.

### Runbook Template Structure

The runbook is embedded in the binary using `//go:embed` and rendered with Go templates. Template variables allow the runbook to adapt to the current system state (e.g., available backends).

```go
//go:embed templates/quick_start_runbook.md
var runbookTemplate string
```

Template data:

```go
type RunbookData struct {
    AvailableBackends []string // From sd doctor output
    DetectionTable    []DetectionEntry
}

type DetectionEntry struct {
    Files           string // File patterns to look for
    Indicates       string // What the files indicate
    SuggestedAction string // Module or package to use
}
```

### Check Output Structure

```go
type QuickStartCheck struct {
    SDYamlExists      bool              `json:"sd_yaml_exists"`
    VMExists          bool              `json:"vm_exists"`
    VMRunning         bool              `json:"vm_running"`
    VMName            string            `json:"vm_name"`
    Backend           string            `json:"backend"`
    Modules           []string          `json:"modules"`
    PackagesDeclared  map[string]int    `json:"packages_declared"`
    Credentials       map[string]bool   `json:"credentials"`
    RepoCloned        bool              `json:"repo_cloned"`
    NeedsSetup        bool              `json:"needs_setup"`
    Issues            []string          `json:"issues"`
}
```

### Hints Integration

The hints system is implemented as an optional field on the standard JSON response type:

```go
package ui

// JSONResponse is the standard envelope for JSON output from sd commands.
type JSONResponse struct {
    OK    bool     `json:"ok"`
    Data  any      `json:"data,omitempty"`
    Error *CLIError `json:"error,omitempty"`
    Hints []string `json:"hints,omitempty"`
}
```

Each command implementation determines which hints to include based on the command outcome. Hint generation is centralized in a `hints` package:

```go
package hints

// ForEnsure returns hints appropriate after an sd ensure operation.
func ForEnsure(created bool, credentialsDetected bool) []string { ... }

// ForConnect returns hints appropriate after an sd connect operation.
func ForConnect(credentialsInjected map[string]bool) []string { ... }

// ForExec returns hints appropriate after an sd exec operation.
func ForExec(exitCode int, stderr string) []string { ... }
```

### Example Runbook Excerpt (Section 1)

```markdown
## 1. Introduction and Education

**Instructions:** Before running any commands, explain to the user what is about to happen.

**Explanation to give the user:**

> I'm going to set up a separate development workspace for this project. This is a
> virtual machine (or container) that's isolated from your personal files and
> credentials. This way, even if I encounter something unexpected in the code, your
> personal stuff -- passwords, SSH keys, browser sessions -- stays completely safe.
>
> The setup takes a few minutes. I'll handle the technical steps and check in with
> you when I need input.

**Instructions:** Wait for the user to acknowledge before proceeding. If they have
questions about why this is needed, explain that AI coding agents sometimes work
with code from external sources (dependencies, open-source repos, pull requests)
that could contain malicious content. The isolated workspace ensures that even in
the worst case, the damage is contained.
```

### Example Runbook Excerpt (Section 7 -- Credentials)

```markdown
## 7. Credential Setup

**Instructions:** Guide the user through creating credentials for the workspace.
Only set up credentials the project actually needs.

### GitHub Access

**Instructions:** If the project repository is on GitHub, or if the `github-cli`
module is included in .sd.yaml, guide the user through creating a scoped token.

**Explanation to give the user:**

> We need to create a special token for GitHub that only has access to this
> specific repository. GitHub calls this a "fine-grained personal access token."
> The idea is simple: instead of using your regular GitHub credentials (which
> can access everything), we create one that can only read and write code in
> this one repo and create pull requests. That way, if something goes wrong,
> nothing else is affected.

**Instructions:** Walk the user through these steps:

1. Direct them to: https://github.com/settings/tokens?type=beta
2. Have them select "Only select repositories" and choose this project's repository
3. Under "Repository permissions," set:
   - Contents: Read and write
   - Pull requests: Read and write
4. Generate the token

**Commands:** Once the user has the token:

```
export GITHUB_TOKEN=<the token the user created>
```
```

## Error Handling

| Error Condition | Category | User Message | Exit Code |
|---|---|---|---|
| `.sd.yaml` not found (runbook mode) | Silent | N/A -- the runbook handles this case as part of setup | 0 |
| `.sd.yaml` not found (`--check` mode) | N/A | Reported in `issues` array: `"No .sd.yaml found -- run sd quick-start to set up"` | 0 |
| VM not reachable (`--check` mode) | N/A | Reported in `issues` array: `"VM exists but is not reachable -- check sd status <name>"` | 0 |
| `sd doctor` unavailable | Fatal | `Error: sd doctor failed: <reason>. Cannot assess system prerequisites.` | 1 |
| Template rendering failure | Fatal | `Error: Failed to render runbook template: <reason>.` | 1 |

The `--check` flag MUST NOT exit with a non-zero code when it detects missing setup components. Missing components are reported in the `issues` array, not as errors. The command only exits non-zero if it cannot perform the check itself (e.g., internal error).

All errors MUST be JSON-serializable when `--json` is active (per REQ-002-012 and REQ-002-018).

## Security Considerations

### Trust boundaries crossed

The `sd quick-start` command itself runs on the host and reads local files (`.sd.yaml`, `.git/config`). It does not cross any trust boundaries directly. The runbook it produces instructs the agent to cross trust boundaries (host-to-VM via SSH), which are governed by [007-connection.md](007-connection.md).

### Credentials and secrets handled

The `--check` flag checks whether credential environment variables are set on the host but MUST NOT read or output their values. It reports only boolean presence (set / not set).

The runbook instructs users to create and export credentials. The credentials themselves are handled by the session/connection subsystem (per REQ-007-019), not by the quick-start command.

### Blast radius if compromised

If the runbook template is compromised (e.g., a malicious build embeds altered instructions), an agent following it could:
- Direct users to create overly permissive tokens
- Skip security steps
- Clone from a malicious repository

Mitigation: The runbook is embedded at build time via `//go:embed`. Binary integrity verification (checksums) at distribution time mitigates template tampering.

### Security education as a security feature

The runbook's plain-language security explanations are themselves a security mechanism. A user who understands why the environment is configured a certain way is less likely to bypass security controls. The education requirements (REQ-010-014) are therefore security-critical.

## Testing Strategy

### Unit Tests

| Requirement | Test |
|---|---|
| REQ-010-001 | Test command registration: `quick-start` is in the "Getting Started" group, help text is correct. |
| REQ-010-002 | Test flag parsing: `--check`, `--json`, and their combinations produce expected output modes. |
| REQ-010-003 | Test runbook output is valid Markdown and contains all 10 section headers. |
| REQ-010-004 | Test Section 1 content: contains introduction, explanation text, no undefined jargon. |
| REQ-010-006 | Test Section 3 content: detection table has all required entries. |
| REQ-010-014 | Test that no jargon term listed in REQ-010-014 appears in the runbook without a preceding definition. |
| REQ-010-015 | Test `--check` output structure: all required fields present, types correct, `needs_setup` logic. |
| REQ-010-016 | Test hints JSON structure: optional field, array of strings, absent when empty. |
| REQ-010-017 | Test runbook does not contain `--mount` instructions for the project directory. |
| REQ-010-018 | Test runbook uses `<name>` placeholders, not hardcoded VM names. |

### Integration Tests

| Requirement | Test |
|---|---|
| REQ-010-002 | Run `sd quick-start` and verify Markdown output to stdout. Run `sd quick-start --check` and parse JSON. |
| REQ-010-005 | Run `sd quick-start`, extract Section 2, verify it includes `sd doctor --json`. |
| REQ-010-015 | With no `.sd.yaml`: run `--check`, verify `sd_yaml_exists` is false and `needs_setup` is true. With a configured VM: run `--check`, verify all fields reflect actual state. |
| REQ-010-016 | Run `sd ensure --json` after creating a VM, verify `hints` field is present and contains credential setup hint. |

### Script Tests

| Requirement | Test |
|---|---|
| REQ-010-001 | `sd quick-start` exits 0 and produces output on stdout. |
| REQ-010-002 | `sd quick-start --check` exits 0 and produces valid JSON. `sd quick-start --json` exits 0 and produces JSON with `runbook` field. |
| REQ-010-015 | Full lifecycle: create `.sd.yaml`, run `sd ensure`, export credentials, run `sd quick-start --check`, verify `needs_setup` is false. |

### Property-Based Tests

| Requirement | Test |
|---|---|
| REQ-010-015 | For any combination of present/absent setup components (`.sd.yaml`, VM, credentials), `needs_setup` is true if and only if at least one component is missing. |
| REQ-010-016 | For any hint string generated by the hints package, the string is non-empty and does not contain control characters. |

## Dependencies

### Depends On

- [002-cli.md](002-cli.md) -- command registration, `--json` output formatting, command groups, exit codes, global flags
- [004-security.md](004-security.md) -- clone-not-mount rationale, egress control defaults, credential scoping policy
- [005-configuration.md](005-configuration.md) -- `.sd.yaml` file format and configuration loading
- [007-connection.md](007-connection.md) -- `sd exec` for running commands inside VMs, `sd connect` for interactive sessions, credential injection via `SendEnv`/`AcceptEnv`
- [009-declarative-environment.md](009-declarative-environment.md) -- `.sd.yaml` `packages` field format for declaring system packages, language tools, and custom setup commands

**Note:** This spec references `sd guide`, `sd ensure`, and `sd init`, which exist in the implementation but are not yet defined in spec 002-cli.md. Spec 002 must be updated with these commands and the "Getting Started" command group before this spec is approved.

### Depended On By

- No other specs currently depend on this spec. Future onboarding or tutorial specs may reference the quick-start workflow.

## Open Questions

- OQ1: Should `sd quick-start --check` verify that packages declared in `.sd.yaml` are actually installed inside the VM, or only that they are declared? Verifying installation requires running commands inside the VM, which adds latency. Current spec checks only declaration.
- OQ2: Should the hints system have a way to suppress hints (e.g., `--no-hints` flag or a config setting)? Deferred until user feedback indicates whether hints are perceived as noisy.
- OQ3: Should the runbook adapt its content based on the detected project type (e.g., a Go-specific runbook vs. a Python-specific runbook), or should it always output the full generic runbook? Current spec outputs the full runbook and relies on the agent to apply relevant sections.

## Revision History

| Date       | Author | Change Description           |
|------------|--------|------------------------------|
| 2026-04-09 | claude | Initial draft                |
