# sd Quick Start -- Agent Setup Runbook

You are setting up a secure development workspace for the user. Follow these
steps in order. Execute commands, ask the user questions at decision points,
and verify each step before proceeding.

For the complete command reference, run `sd guide --agent`.

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
the worst case, the damage is contained to the workspace and cannot reach the
user's personal files or other projects.

## 2. Prerequisites

**Instructions:** Verify that system prerequisites are met before attempting setup.

**Commands:**

```
sd doctor --json
```

**Instructions:** Interpret the JSON output:

- If `"ok": true` -- all checks pass, proceed to the next section.
- If `"ok": false` -- examine the `checks` array. Each failed check has a `name` and `message`.

**Remediation for common failures:**

| Check | What it means | How to fix |
|-------|---------------|------------|
| `ssh` | SSH client is not installed | macOS/Linux: should be pre-installed. If missing, ask the user to install OpenSSH. |
| `lima` | Lima VM manager is not installed | macOS: suggest `brew install lima`. Linux: Lima is macOS-only; use Docker instead. |
| `docker` | Docker is not available | Suggest installing Docker Desktop (macOS/Windows) or Docker Engine (Linux). |
| `sd_home` | The sd configuration directory has issues | Run `sd init` to create it, or check file permissions. |

**Instructions:** At least one backend (`lima` or `docker`) must be available. If
neither is available, tell the user what to install and wait for them to do it
before proceeding. Re-run `sd doctor --json` after installation to confirm.

## 3. Project Analysis

**Instructions:** Examine the current project to determine what runtimes, tools,
and packages the environment needs. You perform this analysis -- `sd` does not
detect dependencies for you. Look for these files and infer requirements:

| File | What It Indicates | Suggested Module or Package |
|------|-------------------|-----------------------------|
| `go.mod` / `go.sum` | Go project | `golang` module |
| `package.json` / `yarn.lock` / `pnpm-lock.yaml` | Node.js project | `nodejs` module |
| `Cargo.toml` / `Cargo.lock` | Rust project | `rust` module |
| `pyproject.toml` / `requirements.txt` / `setup.py` / `Pipfile` | Python project | `python` module |
| `Dockerfile` / `docker-compose.yml` | Docker usage | `docker` module |
| `.github/workflows/*.yml` | GitHub CI | `github-cli` module |
| `Makefile` / `Justfile` | Build system | Read the file and infer tool dependencies (jq, curl, etc.) |
| `Gemfile` / `Gemfile.lock` | Ruby project | `apt: ruby-full` package |
| `pom.xml` / `build.gradle` / `build.gradle.kts` | Java/Kotlin project | `apt: openjdk-*-jdk` package |
| `.csproj` / `*.sln` | .NET project | `apt: dotnet-sdk-*` package |
| `.tool-versions` | asdf version manager | Check for specific runtime versions |
| `.python-version` | pyenv Python version | `python` module with specific version |
| `.node-version` / `.nvmrc` | Node.js version | `nodejs` module with specific version |
| `.go-version` | Go version | `golang` module with specific version |

This table is a starting point, not exhaustive. Use your own knowledge of the
codebase -- read Makefiles, CI configs, README files, and scripts to identify
additional dependencies that the table does not cover.

**Question to ask the user:**

> I found this is a [language/framework] project that uses [tools]. I'll set up
> [list of modules and packages]. Are there other tools or packages you need in
> the development environment?

**Instructions:** Do not silently assume what the user needs. Present your
findings and wait for the user to confirm or add to the list before proceeding.

## 4. Backend Selection

**Instructions:** Determine which VM backend to use based on the `sd doctor --json`
output from Section 2.

**If only one backend is available:** Use it and inform the user.

**If multiple backends are available:** Present the choice with plain-language
trade-offs.

**Question to ask the user:**

> I can set up your workspace using one of these options:
>
> - **Lima** -- creates a full virtual machine. More isolated from your system,
>   takes a couple of minutes to set up. Best for security-sensitive work.
> - **Docker** -- creates a container. Faster to set up (about 10 seconds),
>   slightly less isolated. Good for quick tasks.
>
> Which would you prefer?

**Instructions:** If only one backend is available, skip the question and tell
the user which one you are using and why. Do not ask questions when there is no
real choice to make.

## 5. Environment Configuration

**Instructions:** Generate or update a `.sd.yaml` configuration file for the project.

**Security explanation to give the user:**

> The workspace can only connect to approved websites -- things like GitHub and
> package registries for your language. Which websites or services the workspace
> can connect to is controlled by a list you can see and edit. This is a safety
> net: if anything inside the workspace tries to send data somewhere unexpected,
> it gets blocked automatically. If you run into a site being blocked during
> development, we can add it to the approved list.

1. Detect the project's git remote URL:
   ```
   git remote get-url origin
   ```
2. Set `repo` in `.sd.yaml` to the detected URL
3. Set `modules` based on the runtimes detected in Section 3
4. Set `packages` based on additional tools and packages identified in Section 3
   and confirmed by the user
5. If the project needs access to specific websites beyond the defaults (like
   package registries for the detected language), add them to the approved
   websites list (the `allow_egress` field in `.sd.yaml`)

**Commands:**

```
sd init --modules=<detected-modules>
```

Review the generated `.sd.yaml` and adjust it based on your project analysis.

**Question to ask the user:**

> Here's the environment configuration I'll use. Want me to adjust anything?

**Instructions:** Show the user the contents of `.sd.yaml` and wait for approval
before proceeding. If the user wants changes, update the file accordingly.

## 6. VM Creation

**Instructions:** Create and start the VM.

**Commands:**

```
sd ensure
```

This reads `.sd.yaml` and creates the VM if it does not exist, starts it if it
is stopped, or confirms it is running if already active.

**Explanation to give the user:**

> Creating your development workspace now. This takes about [a couple of minutes
> for Lima / a few seconds for Docker]. I'll let you know when it's ready.

**Instructions:** Adjust the time estimate based on the backend selected in
Section 4. If creation fails:

1. Check the error message for specific guidance
2. Run `sd doctor --json` to verify prerequisites are still met
3. Check `sd logs <name>` for backend-specific errors
4. If the backend service is not running, suggest starting it:
   - Lima: `limactl start`
   - Docker: start Docker Desktop or the Docker daemon

Report the error to the user with the specific failure message and suggested fix.

## 7. Credential Setup

**Instructions:** Guide the user through creating credentials for the workspace.
Only set up credentials the project actually needs.

### GitHub Access

**Instructions:** If the project repository is on GitHub, or if the `github-cli`
module is included in `.sd.yaml`, guide the user through creating a scoped token.

**Explanation to give the user:**

> We'll create a special password for GitHub that only has access to this specific
> project. Think of it like a key that only opens one door instead of every door
> in the building. This limits what can happen if something goes wrong -- the
> workspace can only read and write code in this one repository, not your other
> projects or settings.
>
> GitHub calls this a "fine-grained personal access token" -- it's a token (a
> special password) you can limit to specific repositories and specific actions.

**Instructions:** Walk the user through creating the token:

1. Direct the user to: `https://github.com/settings/tokens?type=beta`
2. Tell them to select **only the repository** they are working on (not "All repositories")
3. Under permissions, set:
   - **Contents**: Read and write (for pushing and pulling code)
   - **Pull requests**: Read and write (for creating and reviewing PRs)
   - All other permissions: leave as "No access"
4. Tell them to copy the token and run:
   ```
   export GITHUB_TOKEN=<token>
   ```

**Instructions:** If the project is not on GitHub or does not need GitHub access,
skip this subsection entirely.

### API Keys for AI Tools

**Instructions:** Ask the user which AI service API keys they need. Do not assume.

**Question to ask the user:**

> Do you have API keys for any AI services you want to use inside the workspace?
> For example, an Anthropic API key for Claude, an OpenAI key, or a Google AI key.
> I'll set up whichever ones you need.

**Instructions:** For each key the user provides:

1. Tell them to export it on the host:
   ```
   export ANTHROPIC_API_KEY=<key>
   export OPENAI_API_KEY=<key>
   ```

**Security explanation to give the user:**

> These keys are passed into the workspace each time you connect -- they are
> never saved to disk inside it. So if the workspace were ever compromised, the
> keys are not there to find. This is what "passing your keys and tokens into
> the workspace when you connect" means -- they exist only in memory for that
> session.

**Instructions:** If the user does not have any API keys to set up now, that is
fine. They can add them later by exporting the variable and reconnecting.

## 8. Repo Clone and Verification

**Instructions:** Clone the project repository inside the VM and verify that
tools are installed correctly.

### Clone the Repository

**Security explanation to give the user:**

> We're cloning the repository inside the workspace instead of sharing your local
> copy. This means the workspace does not have access to your personal files,
> environment variables, or credentials that might be on your laptop. Even if the
> code encounters something malicious, your personal stuff stays safe. This
> approach -- keeping the workspace's files separate from your own -- is a core
> part of how sd protects you. (See the sd security documentation for the full
> rationale.)

**Instructions:** If the user has uncommitted local changes they need in the
workspace, tell them to push those changes to a branch first:

> If you have local changes you'd like to work on inside the workspace, push
> them to a branch first -- the workspace will clone from GitHub, so anything
> not pushed won't be there.

**Commands:**

```
sd exec <name> -- git clone <repo-url> ~/projects/<name>
```

This works because `sd exec` passes credentials configured on the host into the
workspace automatically.

**Question to ask the user:**

> Do you have a specific branch you'd like to check out, or should we work on
> the default branch?

If the user specifies a branch:

```
sd exec <name> -- git -C ~/projects/<name> checkout <branch>
```

### Verify Tools

**Commands:** Run verification commands for each detected runtime:

```
sd exec <name> -- git --version
sd exec <name> -- go version           # if Go
sd exec <name> -- python3 --version    # if Python
sd exec <name> -- node --version       # if Node.js
sd exec <name> -- rustc --version      # if Rust
sd exec <name> -- docker --version     # if Docker
sd exec <name> -- gh --version         # if github-cli
```

**Instructions:** If any verification command fails:

1. Check provisioning logs: `sd logs <name>`
2. Try re-provisioning: `sd provision <name>`
3. If the issue persists, report the specific error to the user and ask for help

Do not proceed to the next section until all expected tools are verified.

## 9. AI Tool Setup

**Instructions:** Set up the AI coding tool the user wants inside the workspace.

**Question to ask the user:**

> What AI coding tool would you like to use inside the workspace? For example:
> - Claude Code
> - Codex
> - Gemini CLI
> - Something else
> - None (I'll run my AI tool on the host and connect to the workspace via SSH)
>
> I can set up whichever you prefer.

**Instructions:** Do not assume which tool the user wants.

If the user selects a tool:

1. Check if the corresponding provisioning module is already in `.sd.yaml`
   (e.g., `claude-code` for Claude Code)
2. If the module is not present, add it to `.sd.yaml` and re-provision:
   ```
   sd provision <name> --modules <tool-module>
   ```
3. Verify the tool is installed:
   ```
   sd exec <name> -- claude --version    # for Claude Code
   ```

If the user does not want an AI tool inside the workspace (e.g., they will
connect from the host via SSH), skip this step entirely.

## 10. Handoff

**Instructions:** Summarize what was set up and tell the user how to use the
environment going forward.

**Explanation to give the user:**

> Your secure development workspace '<name>' is ready. Here's what was set up:
>
> - **Backend**: [lima/docker]
> - **Modules**: [list of installed modules]
> - **Tools verified**: [list of tools that passed verification]
> - **Credentials**: [GitHub: yes/no, API keys: list]
> - **Repository**: cloned to `~/projects/<name>` [on branch X]
>
> **To enter the workspace:**
> ```
> sd connect <name>
> ```
> (or the shorthand: `sd c <name>`)
>
> **To manage the workspace later:**
> ```
> sd stop <name>       # pause the workspace (saves resources)
> sd ensure            # restart it anytime
> sd destroy <name>    # remove it when you're done with the project
> ```
>
> **To sync files back to your machine:**
> ```
> sd sync from <name> <path-inside-workspace>
> ```
>
> Credentials are passed in fresh each time you connect. If you rotate a token,
> just export the new value and reconnect -- no reconfiguration needed.

**Instructions:** For the complete `sd` command reference (all commands, flags,
and usage patterns), run `sd guide --agent`.
