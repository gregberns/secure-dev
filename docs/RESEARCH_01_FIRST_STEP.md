> I want to run Claude with bypass permissions. What do I need to do to do it safely?
> I assume running it in a VM is a good first step. 
> What VM setup is good for MacOS? 
> Are there ways to protect my github account and repos?
> Lets do a wide sweep of what the best engineers are doing, and how they're setting things up.


You’re right that a VM is the right starting point; the goal is to treat “bypass” Claude like an untrusted senior dev on your network and constrain blast radius with layers: VM, filesystem isolation, credentials hygiene, and Git/GitHub controls. [ibm](https://www.ibm.com/think/tutorials/ai-agent-security)

## Threat model in plain terms

When you run with bypass / `--dangerously-skip-permissions`, assume Claude (or any tools it calls) could:
- Read/write anything your user can reach (dotfiles, SSH keys, browser storage, tokens). [news.ycombinator](https://news.ycombinator.com/item?id=46690907)
- Implant hooks (git hooks, shell rc files, CI config) that execute later outside the sandbox. [news.ycombinator](https://news.ycombinator.com/item?id=46690907)
- Exfiltrate secrets over the network or via “legit” APIs (GitHub, Slack, etc.). [reddit](https://www.reddit.com/r/LocalLLaMA/comments/1r8gajo/running_untrusted_ai_agents_safely_container/)

So the core strategy is:
- Constrain where it runs (VM / micro‑VM).  
- Constrain what it can see (no home mounts, no host Docker socket).  
- Constrain what it can reach (egress controls, scoped tokens). [infralovers](https://www.infralovers.com/blog/2026-02-15-sandboxing-claude-code-macos/)

## VM setup on macOS

### 1) Overall pattern

Best current pattern for macOS dev tools and agents is: host macOS → Linux VM (Lima/Apple Virtualization framework/Incus) → Claude + tooling entirely inside the guest. [news.ycombinator](https://news.ycombinator.com/item?id=46613304)

- Do **not** mount your real `$HOME` into the VM. Work in repos that live inside the VM’s virtual disk. [metachris](https://www.metachris.dev/2025/11/sandbox-your-ai-dev-tools-a-practical-guide-for-vms-and-lima/)
- Install compilers, Docker, package managers inside the VM, rather than forwarding host resources. [infralovers](https://www.infralovers.com/blog/2026-02-15-sandboxing-claude-code-macos/)
- Use snapshots so you can roll back if an agent trashes the environment. [news.ycombinator](https://news.ycombinator.com/item?id=46613304)

### 2) Concrete options that work well on Apple Silicon

On Apple Silicon macOS, top options people actually use for agent sandboxes:

- **Lima + Linux guest**  
  - Lima is widely used for isolating AI dev tools; there’s even an “AI agents” example config. [metachris](https://www.metachris.dev/2025/11/sandbox-your-ai-dev-tools-a-practical-guide-for-vms-and-lima/)
  - Pattern: `limactl start sandbox.yaml` → SSH in, install Claude Code, run all “bypass” sessions in there. [infralovers](https://www.infralovers.com/blog/2026-02-15-sandboxing-claude-code-macos/)
  - Pros: simple CLI, good performance, integrates well with container workflows.  

- **Apple Virtualization framework front-ends (UTM, specialized tools like “Code on Incus”)**  
  - You can run a minimal Linux VM or Incus LXD-style containers nested inside; Code On Incus targets exactly “strong container security for AI coding tools”. [youtube](https://www.youtube.com/watch?v=t78-JUnTK5Q)
  - Pros: strong isolation boundary, good performance on Apple Silicon. [youtube](https://www.youtube.com/watch?v=t78-JUnTK5Q)

- **Per‑repo micro‑VMs**  
  - Some folks run “micro‑VM per repo” systems where each project has its own tiny VM you attach Claude to, so cross‑project lateral movement is limited. [reddit](https://www.reddit.com/r/ClaudeAI/comments/1qzps3m/i_built_a_microvm_sandbox_to_run_claude_code/)
  - Pros: smallest blast radius if something goes sideways.  

If you want a no‑nonsense starting point, Lima + a single hardened Linux VM is usually the sweet spot. [metachris](https://www.metachris.dev/2025/11/sandbox-your-ai-dev-tools-a-practical-guide-for-vms-and-lima/)

### 3) Hardening that VM for Claude bypass

Inside that VM, treat it like a semi‑disposable dev box:

- **No host bind mounts**  
  - Don’t mount host directories, especially not `$HOME` or dotfiles. [infralovers](https://www.infralovers.com/blog/2026-02-15-sandboxing-claude-code-macos/)
  - Clone repos you want Claude to touch directly into the VM using HTTPS and a scoped token (see GitHub section). [metachris](https://www.metachris.dev/2025/11/sandbox-your-ai-dev-tools-a-practical-guide-for-vms-and-lima/)

- **Install everything inside**  
  - Install Docker in the VM, don’t expose the host Docker socket; host Docker gives you effective host root if misused. [reddit](https://www.reddit.com/r/LocalLLaMA/comments/1r8gajo/running_untrusted_ai_agents_safely_container/)

- **Egress control**  
  - Add iptables/ufw rules or use an egress proxy with allowlists so the VM can only reach: package mirrors, GitHub, and a few specific endpoints. [obsidiansecurity](https://www.obsidiansecurity.com/blog/security-for-ai-agents)
  - Default‑deny outbound; allow only what you actually need. [obsidiansecurity](https://www.obsidiansecurity.com/blog/security-for-ai-agents)

- **Minimal secrets**  
  - No cloud master keys, no production DB creds in that VM. Give Claude only what it strictly needs (e.g., a repo‑scoped GitHub token). [ibm](https://www.ibm.com/think/tutorials/ai-agent-security)

- **Snapshots & rollback**  
  - Before big sessions, snapshot the VM; if Claude introduces sneaky hooks or weird changes, revert to clean. [news.ycombinator](https://news.ycombinator.com/item?id=46690907)

## Using Claude’s own sandbox vs full bypass

Claude Code now ships with an OS‑level sandboxed bash tool using macOS Seatbelt (and bubblewrap on Linux), which enforces FS and network isolation for its commands. [code.claude](https://code.claude.com/docs/en/sandboxing)

- For “normal” work, use the **built‑in sandbox**, which already constrains file access and network. [anthropic](https://www.anthropic.com/engineering/claude-code-sandboxing)
- Reserve `--dangerously-skip-permissions` for **inside** your hardened VM only, and ideally on a repo you can throw away or fully recreate. [anthropic](https://www.anthropic.com/engineering/claude-code-sandboxing)

This way, your host and primary dev environment never see bypass behavior directly. [code.claude](https://code.claude.com/docs/en/sandboxing)

## Protecting GitHub accounts and repos

### 1) Account‑level protections

- **Strong auth and device separation**  
  - Enable 2FA and passkeys; don’t store personal browser sessions inside the VM. [github](https://github.blog/changelog/2025-03-18-fine-grained-pats-are-now-generally-available/)
  - Use SSH keys or tokens generated only for the VM, not reused from your main account environment. [aembit](https://aembit.io/blog/replacing-a-github-personal-access-token-with-a-github-application/)

- **Separate “bot” identity**  
  - Create a separate GitHub user or org “bot” account that the VM uses for pushes/PRs. [aembit](https://aembit.io/blog/replacing-a-github-personal-access-token-with-a-github-application/)
  - Give that account limited repo access and no org‑owner privileges. [github](https://github.blog/changelog/2025-03-18-fine-grained-pats-are-now-generally-available/)

### 2) Tokens and permissions

Modern guidance is to avoid broad PATs and prefer fine‑grained tokens or GitHub Apps. [aembit](https://aembit.io/blog/replacing-a-github-personal-access-token-with-a-github-application/)

- **Fine‑grained PATs**  
  - Create fine‑grained PATs scoped to a single repo or small set of repos; grant only the operations needed (e.g., read/write code, no admin). [github](https://github.blog/changelog/2025-03-18-fine-grained-pats-are-now-generally-available/)
  - Store them **only** in the VM (or better, inject via env when starting the VM or agent session). [reddit](https://www.reddit.com/r/LocalLLaMA/comments/1r8gajo/running_untrusted_ai_agents_safely_container/)

- **Short‑lived / runtime‑injected tokens**  
  - Best practice is to inject tokens into the environment only at runtime and never persist them to disk where the agent can casually cat them later. [obsidiansecurity](https://www.obsidiansecurity.com/blog/security-for-ai-agents)
  - Some setups use a small host‑side script to mint or load tokens, then pass them into the VM/agent, and rotate or revoke routinely. [aembit](https://aembit.io/blog/replacing-a-github-personal-access-token-with-a-github-application/)

- **GitHub App instead of PAT**  
  - For CI‑like automation or more robust control, use a GitHub App with narrowly scoped permissions and short‑lived installation tokens instead of user PATs. [github](https://github.blog/changelog/2025-03-18-fine-grained-pats-are-now-generally-available/)

### 3) Repo‑level and workflow controls

- **Protected branches and reviews**  
  - Lock down `main`/`master` with required reviews, status checks, and no force‑push, so if Claude pushes something bad, it still needs human approval. [news.ycombinator](https://news.ycombinator.com/item?id=46690907)

- **Watch for hooks and pipeline edits**  
  - Make `.git/hooks`, `.github/workflows`, and any CI configs part of your regular diff review; an agent can sneak in code that runs on commit or in CI. [github](https://github.com/orgs/community/discussions/174507)

## Wider best practices “good engineers” are using

Pulling patterns from security and infra folks working on AI agents: [ibm](https://www.ibm.com/think/tutorials/ai-agent-security)

- **Treat agents as untrusted processes**  
  - “Never trust, always verify”: every tool the agent can call is permission‑gated, logged, and least‑privilege. [ibm](https://www.ibm.com/think/tutorials/ai-agent-security)

- **Layered isolation**  
  - VM/containers per agent or per repo, default‑deny egress, runtime credential injection, and hash‑chained audit logs for everything the agent does. [reddit](https://www.reddit.com/r/LocalLLaMA/comments/1r8gajo/running_untrusted_ai_agents_safely_container/)

- **Disposable environments**  
  - Use ephemeral or easily resettable sandboxes, especially for arbitrary third‑party MCP servers / tools. [reddit](https://www.reddit.com/r/ClaudeAI/comments/1qzps3m/i_built_a_microvm_sandbox_to_run_claude_code/)

- **Host remains “clean”**  
  - Keep day‑to‑day personal/dev environment non‑bypass and non‑privileged; only the sandbox VM runs with dangerous flags. [news.ycombinator](https://news.ycombinator.com/item?id=46613304)

An example pattern you might like: host macOS → Lima VM “claude-sandbox” (no home mounts, own disk) → inside that VM, Claude Code with `--dangerously-skip-permissions`, Docker, and a **bot** GitHub user configured with a single fine‑grained PAT for only the target repo; VM has outbound network allowlist and is snapshotted before big refactors. [infralovers](https://www.infralovers.com/blog/2026-02-15-sandboxing-claude-code-macos/)

Would you rather optimize this for maximum safety (even if it’s a bit clunky) or for speed/ergonomics close to your normal dev workflow?





> Are there projects that already help set all this up? 
> I wouldn't mind building out a project to do this - but if all the things already exist (in an AI first model) looking at those first could save time



Yes—there are already a few “AI‑first” sandbox projects and patterns you can crib from before you build your own, especially around Claude Code specifically. [code.claude](https://code.claude.com/docs/en/sandboxing)

## Existing Claude‑focused sandboxes

- **Claude Code’s own sandbox runtime (open source)**  
  - Claude’s Seatbelt/bubblewrap sandbox is exposed as an open‑source npm package you can embed into your own agent harnesses. [code.claude](https://code.claude.com/docs/en/sandboxing)
  - It gives you a standardized way to define filesystem/network boundaries and fail hard if sandboxing isn’t available (`sandbox.failIfUnavailable`). [code.claude](https://code.claude.com/docs/en/sandboxing)

- **Docker “Claude Code sandbox” templates**  
  - Docker now ships official sandbox templates for Claude Code: `docker sandbox run claude ~/project` spins up an isolated environment with only your project directory mounted. [mintmcp](https://www.mintmcp.com/blog/sandbox-claude-code)
  - They layer in configurable network allowlists/egress controls, so it’s a ready‑made pattern for a project that wants Docker‑first isolation. [docs.docker](https://docs.docker.com/ai/sandboxes/agents/claude-code/)

- **cbox (OS‑level snapshotting sandbox)**  
  - Community tool “cbox” is built specifically to give Claude full shell access inside a kernel‑enforced sandbox, then show you `diff` of changes and let you selectively merge. [reddit](https://www.reddit.com/r/ClaudeAI/comments/1s2v9g4/i_built_an_oslevel_sandbox_so_claude_code_can_run/)
  - It supports Linux namespaces and macOS via Docker, and already ships with Claude Code integrated, which is very close to what you described wanting. [reddit](https://www.reddit.com/r/ClaudeAI/comments/1s2v9g4/i_built_an_oslevel_sandbox_so_claude_code_can_run/)

- **Vagrant / VM‑style Claude sandboxes**  
  - There is at least one Vagrant‑based project that provisions an Ubuntu VM with Claude Code preinstalled and a sandboxed environment via Docker inside. [github](https://github.com/bgrgicak/vagrant-claude-sandbox)
  - It aims to give a “turnkey” isolated Claude box with minimal host coupling. [github](https://github.com/bgrgicak/vagrant-claude-sandbox)

## VM / Lima patterns people have documented

- **Lima templates for AI dev tools**  
  - There’s a “Sandbox Your AI Dev Tools” guide that walks through Lima VMs on macOS and explicitly calls out provisioning Claude (plus other AI tools) with per‑VM trust levels, snapshots, and restricted mounts. [engineered](https://engineered.at/articles/sandbox-your-ai-dev-tools-a-practical-guide-for-vms-and-lima)
  - The pattern is almost exactly what you sketched: Lima VM, no `$HOME` mount by default, per‑project VMs, and snapshots before you unleash agents. [engineered](https://engineered.at/articles/sandbox-your-ai-dev-tools-a-practical-guide-for-vms-and-lima)

- **Inception‑style Colima → Incus → Claude setups**  
  - A popular tutorial (“Code On Incus”) builds a nested sandbox: K‑Lima/Colima → Incus containers → Claude Code inside that, with only a specific folder mounted back to host. [youtube](https://www.youtube.com/watch?v=t78-JUnTK5Q)
  - It’s a strong reference if you want to see a fully worked “AI‑first” architecture using containers+VMs on macOS. [news.ycombinator](https://news.ycombinator.com/item?id=46613304)

- **VM‑only “Claude inside a box” flows**  
  - Several writeups just do “SSH into a dedicated Linux VM, run Claude there, give it root inside the VM, and destroy/restore from snapshot as needed.” [yieldcode](https://yieldcode.blog/post/isolating-claude-code/)
  - These are less ‘productized’ but good concrete examples of how people are wiring remote Claude → VM dev environments. [yieldcode](https://yieldcode.blog/post/isolating-claude-code/)

## Security‑oriented tooling around bypass

- **Lasso / defender projects for `--dangerously-skip-permissions`**  
  - Recent security research (Lasso et al.) shipped an open‑source “defender” that inspects Claude Code actions when you use `--dangerously-skip-permissions`, focusing on prompt‑injection vectors in repos and MCP content. [reddit](https://www.reddit.com/r/ClaudeAI/comments/1s2qdh0/claude_code_with_dangerouslyskippermissions_is_a/)
  - Worth studying if you’re thinking about guard rails or auditing around a sandbox, not just the sandbox itself. [reddit](https://www.reddit.com/r/ClaudeAI/comments/1s2qdh0/claude_code_with_dangerouslyskippermissions_is_a/)

- **Claude Code + hardened container described in best‑practices**  
  - There are best‑practice notes (GitHub issues/docs) that assume you’ll run `claude --dangerously-skip-permissions` inside an already‑hardened container/VM with firewall rules, and they describe the expected container profile. [github](https://github.com/anthropics/claude-code/issues/19978)

## What’s *not* really done yet

From the above, gaps that would still be interesting for you to build:

- A **Mac‑first, opinionated stack** that:  
  - Spins up a Lima/Apple‑Virtualization VM with a secure default (no `$HOME` mounts, pre‑set egress rules). [linkedin](https://www.linkedin.com/posts/ehaselwanter_claude-code-sandboxing-on-macos-activity-7437125747485523968-IY8p)
  - Injects **scoped GitHub tokens** / bot identities in a clean, revocable way.  
  - Integrates Claude’s official sandbox runtime plus a higher‑level policy engine (eg. project trust levels, allowlisted tools).  

- A **“Claude bypass profile”** that:  
  - Explicitly targets `--dangerously-skip-permissions` use cases,  
  - Enforces default‑deny networking and file access,  
  - Ships with diff/merge review like cbox but tailored to Git workflows and protected branches. [github](https://github.com/anthropics/claude-code/issues/19978)

- A **config generator** that reads your existing repos/org and spits out:  
  - Lima/VM configs,  
  - Docker sandbox definitions,  
  - Recommended GitHub fine‑grained token scopes and bot user setup.  

If you want to save time, I’d start by playing with cbox + Docker Claude sandbox + a Lima template, and then decide whether your project is “unify and polish these” or “VM‑first, opinionated Mac stack” on top. [mintmcp](https://www.mintmcp.com/blog/sandbox-claude-code)

Are you more interested in building a polished, reusable tool for other devs, or an internal setup that’s highly tuned to your own multi‑repo workflow?
