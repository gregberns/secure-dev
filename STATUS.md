# Project Status — secure-dev (sd)

*Last updated: 2026-04-09*

## Executive Summary

`sd` is a Go CLI for creating secure VM environments for AI coding agents. The project has **8 specs**, **all 27 gap items implemented**, **1,814 passing tests**, and **47 tracked issues (44 closed, 2 closed-verified, 1 deferred)**. All code compiles and tests pass.

The tool has **never been tested end-to-end as a real user would use it**. All testing has been automated (unit, property-based, integration with mocks/memory/Docker backends, CLI surface exploratory). The next milestone is validating that `sd create && sd connect` actually works on a real machine with Lima.

---

## What Exists

### Specs (8 total)

| # | Title | Status | Notes |
|---|-------|--------|-------|
| 001 | System Architecture | draft | Package layout updated to match impl |
| 002 | CLI Interface Design | draft | All commands implemented |
| 003 | VM Backend System | draft | Lima + memory + Docker backends done |
| 004 | Security Model | draft | Egress, audit, credentials, permissions all wired |
| 005 | Configuration System | draft | Viper/YAML, VM config persistence done |
| 006 | VM Provisioning | draft | Modules, probes, checksums, custom modules done |
| 007 | Connection and Interaction | draft | Connect, exec, sync (with watch), ssh-config done |
| 008 | Test Backends | **approved** | Memory + Docker + conformance suite done |

### Implementation (all packages)

| Package | Status | Test files | Notes |
|---------|--------|------------|-------|
| backend/ | Complete | registry, conformance, memory, lima, mocklimactl | 3 backends (lima, memory, docker) + conformance suite |
| cmd/ | Complete | 38 test files covering all commands | Migrated to memory backend for isolation |
| config/ | Complete | loader, types, precedence | VM config read/write, credentials separated |
| provision/ | Complete | module, provisioner, dependency resolver | Built-in + custom modules, readiness probes |
| security/ | Complete | audit, egress, credential injection | Hash-chain audit, ip4+ip6 egress, SD_HOME perms |
| session/ | Complete | tmux, mocktmux | Session management with digital twin |
| ssh/ | Complete | key gen, config fragments, host keys | Fragment write/remove, NeedsInclude |
| ui/ | Complete | formatting, JSON output, progress | Consistent JSON envelope |

### Test Coverage

- **1,814 individual test cases** across 13 packages (all passing)
- **Property-based tests** (rapid): module resolution, config precedence, backend ops, CLI completion, loader
- **Conformance suite**: 39 tests verifying backend contract compliance
- **Integration tests**: Lima (11), Docker (7), audit logger, CLI
- **Exploratory tests**: Wave 1 CLI surface (78 test cases, 71 pass / 7 fail -- all 7 failures subsequently fixed)

### Gap Analysis Resolution

All 27 gaps from GAP-ANALYSIS-AND-PLAN.md have been addressed:

| Phase | Gaps | Status |
|-------|------|--------|
| 0: Security Foundations | G15, G16, G17 (audit chain, wiring, checksums) | All fixed |
| 1: Critical Infrastructure | G01, G02 (readiness probes, VM config) | All fixed |
| 2: Security Hardening | G08, G09, G20, G24 (virtiofs, DNS, ip6tables, perms) | All fixed |
| 3: Features | G03-G07, G10, G18, G21-G23 (12 items) | All fixed |
| 4: Polish | G11-G14, G19, G25-G27 (8 items) | All fixed |

### Issue Tracker (beads)

- **47 total issues**: 44 closed, 2 closed-verified, 1 deferred
- **Open**: None
- **Deferred**: secure-dev-8gl (exploratory test modes -- enhancement, deferred to 2026-05-01)

---

## What's NOT Been Done

### 1. Real End-to-End Testing (CRITICAL)

**No one has run `sd create myvm && sd connect myvm` on a real machine.** All backend testing uses mocks, memory backend, or Docker containers. The Lima backend integration tests use a mock `limactl` binary (mocklimactl). The actual user workflow has never been validated:

- Does `sd create` produce a working Lima VM?
- Does provisioning actually install tools inside the VM?
- Does `sd connect` SSH into the VM and attach tmux?
- Do egress rules actually block traffic?
- Does credential injection work end-to-end?
- Does snapshot create/restore actually work with Lima?

### 2. Spec Reviews (PROCESS)

Specs 001-007 are all still "draft". Per AGENTS.md, specs need 3-agent review (architect, critic, qa) before being considered ready. Only spec 008 has been approved. This is a process gap -- the code was written against draft specs.

### 3. Exploratory Testing Waves 2 & 3 (TESTING)

Only Wave 1 (CLI surface, no backend) has been run. Waves 2 and 3 need a working backend:

- **Wave 2**: VM lifecycle, provisioning, connection, sync, adversarial testing
- **Wave 3**: Per-spec REQ compliance verification

The `config-flags` and `json-contract` agents from Wave 1 produced empty reports (ran but didn't write output).

### 4. Validation Gaps Found by Exploratory Tests

The exploratory test found and issues were filed. All 4 were fixed:
- `completion --help` exits 1 -> fixed (06566ba)
- `doctor --json` returns ok:true on failure -> fixed (78db49f)  
- Generic Cobra arg errors -> fixed (6808afd)
- `create --cpus=-1` accepted -> fixed (9a95d83)

However, some edge cases from the report may still be open:
- `--memory=banana` accepted without validation on create
- `--backend=nonexistent` accepted by `--dry-run` path
- `--disk=0` and `--memory=0` accepted
- `--cpus=999` accepted (no upper bound)

### 5. Build & Release Pipeline

No CI/CD, no release builds, no version tagging. The binary is built with `go build` and shows `dev/unknown` for version info.

### 6. Documentation

No user-facing documentation beyond help text. No README (beyond AGENTS.md which is developer-facing). No installation instructions. No quickstart guide.

---

## Recommended Next Steps

### Priority 1: Validate It Actually Works

Run the real user workflow on macOS with Lima:
1. `go build -o sd ./cmd/sd`
2. `sd create test-vm`
3. `sd status test-vm`
4. `sd connect test-vm`
5. Verify provisioned tools inside VM
6. Test egress rules, credential injection
7. `sd snapshot create test-vm --tag=baseline`
8. `sd destroy test-vm`

Document what works, what breaks, what needs fixing. This is the single highest-value activity.

### Priority 2: Fix Remaining Validation Gaps

Address the edge cases the exploratory tests found:
- Memory format validation (must parse as size: `4GiB`, `512MiB`, etc.)
- Disk size validation (must be positive, parseable)
- Backend validation in dry-run path
- CPU upper bound (reasonable max, e.g., 256)

### Priority 3: Wave 2 Exploratory Testing

With a working backend (memory or Docker), run the lifecycle and provisioning agents to find integration-level bugs.

### Priority 4: Spec Review & Approval

Run the 3-agent review process on specs 001-007 to formalize them. This may surface spec gaps that need code changes.

### Priority 5: CI/CD & Release

- GitHub Actions for test, vet, build
- GoReleaser or equivalent for tagged releases
- Version injection via ldflags

---

## Quick Reference

```bash
# Build
go build -o sd ./cmd/sd

# Test (all)
go test ./...

# Test (specific package)
go test ./internal/cmd/...

# Test (verbose, uncached)
go test -count=1 -v ./internal/backend/memory/...

# Issue tracker
bd ready           # Open work
bd show <id>       # View issue
bd update <id>     # Update issue

# Exploratory tests
./tests/exploratory/launch.sh
```
