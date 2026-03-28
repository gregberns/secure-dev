# Beads Review: sd

**Generated:** 2026-03-28
**Reviewers:** Forward (plan->beads), Reverse (beads->plan), Dependencies (graph integrity)

---

## Summary

| Category | P0 | P1 | P2 | Total |
|----------|----|----|----|----|
| Coverage Gaps | 0 | 0 | 1 | 1 |
| Conversion Scope Creep | 0 | 0 | 0 | 0 |
| Dependency Errors | 3 | 0 | 0 | 3 |
| Content Fidelity | 0 | 0 | 0 | 0 |
| **Total** | **3** | **0** | **1** | **4** |

---

## P0 Findings (Must Fix)

### 1. sd-ee7.5.2 missing blocker on sd-ee7.5.5 (provisioning engine)
- **Category:** Dependency Error
- **Found by:** Dependencies review
- **What:** `sd create` calls `Provisioner.Provision()` but the provisioner types from task 5.5a (the Provisioner interface + Engine struct) are not listed as a blocker. The bead lists blockers sd-ee7.2.2, sd-ee7.2.3, sd-ee7.3.2, sd-ee7.4.1, sd-ee7.4.2, sd-ee7.5.1 but omits sd-ee7.5.5.
- **Evidence:** Plan task 5.2 acceptance criteria require `--modules all` to provision every module (REQ-006-015). The App struct wires in the Provisioner, which must exist before create can compile.
- **Fix command(s):**
  ```bash
  bd dep add sd-ee7.5.2 sd-ee7.5.5
  ```

### 2. sd-ee7.7.7 missing blocker on sd-ee7.7.3 (audit logging)
- **Category:** Dependency Error
- **Found by:** Dependencies review
- **What:** `sd logs` in sd-ee7.7.7 aggregates from three log sources including "sd audit log filtered by VM name" which calls `AuditLogger.Query()`. Audit logging (task 7.2 / bead sd-ee7.7.3) must exist before `sd logs` can query it.
- **Evidence:** Plan task 7.6 description explicitly lists audit log as one of three log sources.
- **Fix command(s):**
  ```bash
  bd dep add sd-ee7.7.7 sd-ee7.7.3
  ```

### 3. sd-ee7.8.1 missing blocker on sd-ee7.7.4 (credential management)
- **Category:** Dependency Error
- **Found by:** Dependencies review
- **What:** Integration test suite (sd-ee7.8.1) includes `connect_integration_test.go` which tests SSH key + config and credential injection. The real CredentialInjector is implemented in task 7.3 / bead sd-ee7.7.4.
- **Evidence:** Plan specifies integration tests cover the complete connect path including `app.Creds.InjectEnv()`. Without the real credential injector, integration test coverage is incomplete. (Note: weakest of the three findings — noop stub could technically stand in.)
- **Fix command(s):**
  ```bash
  bd dep add sd-ee7.8.1 sd-ee7.7.4
  ```

---

## P1 Findings (Should Fix)

None.

---

## P2 Findings (Consider)

### 4. sd-ee7.5.7 description missing two claude-code.yaml details
- **Category:** Coverage Gap
- **Found by:** Forward review
- **What:** The bead description for built-in provisioning modules omits two implementation details from the plan's claude-code.yaml section: (1) configuring `git config user.name/user.email` from host git config, and (2) explicitly verifying `~/.git-credentials` is absent after setup.
- **Evidence:** These details are present in the acceptance criteria ("claude-code.yaml clears pre-existing credential helpers and verifies no ~/.git-credentials") but not in the description body. An implementer reading only the description would miss the user.name/user.email step.
- **Fix suggestion:** Add to sd-ee7.5.7 description: "claude-code.yaml also configures git user.name/user.email from host git config and verifies ~/.git-credentials is absent after setup."

---

## Parallelism Report

- **Dependency waves:** 7
- **Maximum parallel width:** 12 beads (wave 4)
- **Critical path:** 9 beads (1.3 -> 1.4 -> 5.1 -> ... -> 8.4)
- **Ready queue:** sd-ee7.1.1, sd-ee7.1.2, sd-ee7.1.3, sd-ee7.2.1, sd-ee7.4.1, sd-ee7.7.3

## Coverage Summary

**Forward (Plan->Beads):**
- Fully matched: 35 tasks
- Partially matched: 1 task (sd-ee7.5.7, P2 gap)
- No matching bead: 0

**Reverse (Beads->Plan):**
- Plan-backed: 36 beads
- Structural (epics): 9 beads
- Scope creep: 0 beads

**Dependencies:**
- Correctly constrained: 44 of 47
- Missing blockers: 3
- Over-constrained: 0
