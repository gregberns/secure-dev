# Implementation Prompt

Paste this into a new Claude Code session on branch `int-ralph-loop`.

---

## Prompt

You are implementing gap fixes for the `sd` (secure-dev) Go CLI project. A gap analysis compared 7 specs (143 requirements) against the codebase and found 28 gaps. These are tracked as beads (`bd` command). The full analysis is in `GAP-ANALYSIS-AND-PLAN.md`.

**Your workflow:**
1. Run `bd prime` to load beads context
2. Run `bd ready` to see what's available
3. Pick the highest-priority ready bead
4. Run `bd update <id> --claim` to claim it
5. Read the bead (`bd show <id>`) — it has specs, files, and test expectations
6. Read the relevant spec(s) in `specs/` and the files listed in the bead
7. Implement the fix
8. Run `go build ./...` and `go test ./...` to verify
9. Commit (reference the bead ID in the commit message)
10. Run `bd close <id>` — this unblocks dependent beads
11. Repeat from step 2

**Critical rules:**
- Follow AGENTS.md / CLAUDE.md strictly — this is a spec-driven project
- Reference REQ-NNN-MMM IDs in code comments where requirements are fulfilled
- Run tests after every change. Fix what you break.
- Never implement behavior not described in specs
- Commit after each bead (one bead = one commit)
- Push regularly (`git push`)
- Never ask the human questions or wait for input
- Persist progress ONLY via editing files in this repo or closing beads
- Just perform the work and then terminate your response

**Execution order matters.** The beads have dependencies. `bd ready` only shows unblocked work. The critical path is:

```
Phase 0 (P0 — do these FIRST):
  secure-dev-75y: Fix audit hash chain (read last log line to recover chain tip)
  secure-dev-dk0: Wire checksum injection (map module checksums → env vars in provisioner)
  secure-dev-08p: Wire audit logging (PersistentPreRunE/PostRunE hooks + per-command LogEvent) [blocked by 75y]

Phase 1 (P1 — after Phase 0):
  secure-dev-7kv: SD_HOME permission enforcement
  secure-dev-t3w: Fix sd start idempotency (one-liner)
  secure-dev-wfr: VM config write/read + create integration [blocked by 7kv, t3w]
  secure-dev-q5r: Separate credentials into credentials.yaml [blocked by wfr]
  secure-dev-ww5: VM state lifecycle tracking [blocked by wfr]
  secure-dev-9o1: Readiness probe execution [blocked by dk0, wfr, ww5]

Phase 2 (security hardening, can parallel Phase 1):
  secure-dev-088: ip6tables egress rules
  secure-dev-2yx: DNS periodic re-resolution
  secure-dev-2pt: VZ virtiofs mount type

Phase 3 (features, mostly independent):
  secure-dev-uyd: SSH fragment removal on destroy
  secure-dev-4ti: Custom provisioning modules → then secure-dev-u4m: --modules all
  secure-dev-aus: Guest VM env setup → then secure-dev-zt7: CLAUDE.md propagation
  secure-dev-4z3: Base image map x86_64
  secure-dev-o2r: Backend stubs (AVF, Docker, Incus)
  secure-dev-9v3: --dry-run on create
  secure-dev-c59: Sync watch mode (needs fsnotify dep)
  secure-dev-9ee: Doctor SSH fragment checks
  secure-dev-fxu: tmux default config

Phase 4 (polish):
  secure-dev-wux: Provisioning progress reporting [blocked by ww5]
  secure-dev-9ve: Orphaned state detection [blocked by wfr]
  secure-dev-3fa, secure-dev-al1: Bot account + branch protection guidance
  secure-dev-xd2: Spec update (LAST)
```

**File conflict awareness.** Multiple beads touch the same files. Key hotspots:
- `internal/cmd/create.go` — touched by 08p, wfr, 9v3, uyd, 4ti, u4m
- `internal/cmd/destroy.go` — touched by 08p, wfr/ww5, uyd
- `internal/cmd/start.go` — touched by 08p, ww5, t3w
- `internal/provision/provisioner.go` — touched by dk0, 9o1, wux
- `internal/provision/modules/egress.yaml` — touched by 088, 2yx
- `internal/provision/modules/base.yaml` — touched by aus, fxu
- `internal/config/loader.go` — touched by 7kv, wfr

When working on a bead that shares files with a later bead, don't refactor beyond what the current bead needs.

**Start with the two P0 beads that are ready now: `secure-dev-75y` (audit hash chain) and `secure-dev-dk0` (checksum injection). These are independent and can be done in either order. Then `secure-dev-08p` unblocks.**

Work through as many beads as you can. Prioritize by: P0 first, then P1, then unblock the critical path. If a bead is too complex or you're unsure about a design decision, create a comment on it with `bd note <id> "question..."` and move to the next ready bead.
