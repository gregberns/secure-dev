# Spec Format

This document defines the required structure for all specifications in the `sd` project. Every spec MUST follow this format. Deviations are violations.

## File Naming

- Pattern: `NNN-kebab-case-name.md`
- NNN is a zero-padded three-digit number, assigned sequentially
- Examples: `001-architecture.md`, `002-cli.md`, `003-vm-backend.md`

## Required Sections

Every spec MUST contain the following sections in this order:

### 1. Title and Metadata Block

```markdown
# NNN: Spec Title

| Field       | Value                    |
|-------------|--------------------------|
| Status      | draft / review / approved |
| Created     | YYYY-MM-DD               |
| Last Updated| YYYY-MM-DD               |
| Authors     | agent-name or human-name |
| Reviewers   | (filled during review)   |
```

**Status values:**
- `draft` — being written, not ready for review
- `review` — ready for review agents to evaluate
- `approved` — reviewed and accepted, ready for implementation

### 2. Overview

A concise description (2-5 sentences) of what this spec covers and why it exists. State the problem being solved and the approach taken.

### 3. Goals and Non-Goals

Explicit lists of what this spec IS and IS NOT trying to accomplish. This prevents scope creep and sets clear boundaries for implementation.

```markdown
## Goals
- G1: [goal description]
- G2: [goal description]

## Non-Goals
- NG1: [explicitly excluded scope]
- NG2: [explicitly excluded scope]
```

### 4. Requirements

The core of the spec. Each requirement has a unique ID and is testable.

```markdown
## Requirements

### REQ-NNN-001: Requirement Title

[Description of the requirement. What MUST the system do?]

**Acceptance criteria:**
- [ ] [Specific, testable criterion]
- [ ] [Specific, testable criterion]
```

**Requirement ID format:** `REQ-NNN-MMM`
- NNN = spec number (matches filename)
- MMM = requirement number within this spec (sequential, starting at 001)

**Requirement language:**
- MUST / MUST NOT — absolute requirements
- SHOULD / SHOULD NOT — recommended but not mandatory
- MAY — optional

### 5. Design

Technical design details. This section should contain enough detail for an agent to implement without ambiguity.

Include as appropriate:
- **Interfaces** — Go interface definitions with method signatures and documentation
- **Types** — Go struct definitions with field descriptions
- **Data flow** — How data moves through the system
- **State machines** — Valid state transitions
- **File formats** — Config file schemas, with examples
- **CLI surface** — Command signatures, flags, output format

Use Go code blocks for interface/type definitions. These are the contract — implementation must match.

### 6. Error Handling

How errors are surfaced, categorized, and handled. For each error condition:
- What triggers it
- Whether it's fatal, warning, or silent
- What the user sees (text and JSON)
- What recovery action is possible

### 7. Security Considerations

Explicit analysis of security implications. Every spec must address:
- What trust boundaries does this feature cross?
- What credentials or secrets does it handle?
- What is the blast radius if this feature is compromised?
- What mitigations are in place?

If the spec has no security implications, state that explicitly with justification.

### 8. Testing Strategy

How this spec's requirements will be verified:
- **Unit tests** — what is tested in isolation
- **Integration tests** — what requires real system interaction
- **Script tests** — CLI-level tests using script-driven testing (rsc.io/script format or equivalent)

Each requirement should map to at least one test.

### 9. Dependencies

What this spec depends on (other specs, external tools, libraries) and what depends on it.

```markdown
## Dependencies

### Depends On
- [NNN-name.md](NNN-name.md) — what is needed from it

### Depended On By
- [NNN-name.md](NNN-name.md) — what it needs from this spec
```

### 10. Revision History

Track significant changes to the spec.

```markdown
## Revision History

| Date       | Author | Change Description           |
|------------|--------|------------------------------|
| YYYY-MM-DD | name   | Initial draft                |
```

## Optional Sections

These sections may be included when relevant:

- **Alternatives Considered** — other approaches that were evaluated and why they were rejected
- **Migration / Upgrade Path** — how to transition from current state to the specified design
- **Performance Considerations** — latency, throughput, resource usage constraints
- **Open Questions** — unresolved decisions that need input (must be resolved before status moves to `approved`)

## Writing Guidelines

1. **Be precise.** Avoid "should work" or "might need." State what MUST happen.
2. **Be complete.** If an agent can't implement from the spec alone, the spec is incomplete.
3. **Be testable.** Every requirement must have acceptance criteria that can be verified programmatically.
4. **Use examples.** Show concrete CLI invocations, config file snippets, expected output.
5. **Define terms.** If a term could be ambiguous, define it in the spec where it's first used.
6. **Cross-reference.** When referencing other specs, use requirement IDs: "as specified in REQ-001-005."
7. **No implementation details.** Specs define WHAT and WHY, not HOW (unless the HOW is the contract, like an interface definition).
