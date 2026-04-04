# 008: Test Backends (Memory and Docker)

| Field        | Value      |
|--------------|------------|
| Status       | approved   |
| Created      | 2026-04-03 |
| Last Updated | 2026-04-03 |
| Authors      | claude     |
| Reviewers    | architect, critic, qa |

## Overview

This spec defines two new backend implementations for automated testing of the `sd` CLI: an in-memory backend for fast, deterministic testing of CLI commands and state machine logic, and a Docker backend for integration testing of provisioning scripts, SSH plumbing, and security controls against real Linux environments. Neither backend is intended for production agent workloads — they exist solely to enable comprehensive automated testing without depending on Lima VMs (which take 30-60s to boot).

## Goals

- G1: Define an in-memory backend that implements the full `Backend` interface with no external dependencies, enabling sub-second lifecycle tests
- G2: Define a Docker backend that implements the `Backend` interface using Docker containers, enabling integration tests against real Linux in ~1s
- G3: Establish a test suite architecture that exercises the full CLI command surface using these backends
- G4: Ensure both backends enforce the same state machine semantics and error contracts as the Lima backend

## Non-Goals

- NG1: Replacing Lima as the production backend — these are test-only
- NG2: Making the Docker backend production-grade for agent workloads (no egress control, limited isolation)
- NG3: Exploratory or manual testing workflows — those are handled separately
- NG4: Changing the existing `Backend` interface — both backends implement it as-is
- NG5: Docker-in-Docker or host Docker socket mounting

## Requirements

### REQ-008-001: In-Memory Backend — Core Implementation

The system MUST include an in-memory backend that implements the `Backend` interface (REQ-003-001) entirely in Go with no external dependencies, no disk I/O, and no subprocess calls.

**Acceptance criteria:**
- [ ] In-memory backend is implemented in `internal/backend/memory/`
- [ ] The memory backend MUST NOT register itself via `init()`. Tests instantiate it via `memory.New()` and inject it through `getBackendFunc` or direct usage. The `internal/backend/memory/` package MUST NOT be imported (directly or transitively) by `cmd/sd/main.go`
- [ ] `Name()` returns `"memory"`
- [ ] `Available()` always returns `nil` (no prerequisites)
- [ ] All `Backend` interface methods are implemented
- [ ] All VM state is stored in Go maps protected by a mutex
- [ ] No files are created on disk during any operation
- [ ] No subprocesses are spawned during any operation
- [ ] VM name validation MUST be enforced: `Create` with a name that fails `ValidateVMName` returns `ErrInvalidVMName`

### REQ-008-002: In-Memory Backend — State Machine Fidelity

The in-memory backend MUST enforce the same state machine transitions defined in spec 003 (state machine section). Invalid transitions MUST produce the same sentinel errors as a real backend.

**Acceptance criteria:**
- [ ] `Create` on an existing name returns `ErrVMAlreadyExists`
- [ ] `Start` on a non-existent VM returns `ErrVMNotFound`
- [ ] `Start` on a running VM is a no-op (no error)
- [ ] `Stop` on a non-existent VM returns `ErrVMNotFound`
- [ ] `Stop` on a stopped VM is a no-op (no error)
- [ ] `Destroy` on a non-existent VM returns `ErrVMNotFound`
- [ ] `Status` on a non-existent VM returns `ErrVMNotFound`
- [ ] `Status` on an existing VM returns its current `VMStatus`
- [ ] `Exec` on a non-running VM returns `ErrVMNotRunning`
- [ ] `SSHConfig` on a non-running VM returns `ErrVMNotRunning`
- [ ] `Create` transitions to `StatusRunning` (matching Lima backend behavior; spec 003 permits either `Running` or `Stopped` as backend-dependent — the memory backend chooses `Running` for Lima fidelity)
- [ ] `Start` transitions `StatusStopped` to `StatusRunning`
- [ ] `Stop` transitions `StatusRunning` to `StatusStopped`
- [ ] `Stop` transitions `StatusError` to `StatusStopped` (per spec 003 state machine)
- [ ] `Destroy` removes the VM from any state (`Running`, `Stopped`, or `Error`)
- [ ] `Exec`/`SSHConfig` on a VM in `StatusError` returns `ErrVMNotRunning`
- [ ] A `SetStatus(name string, status VMStatus)` test hook allows tests to force a VM into any state (including `StatusError`) for testing error-state transitions

### REQ-008-003: In-Memory Backend — SSHConfig

The in-memory backend MUST return a valid `SSHConfig` struct for running VMs. The config does not need to correspond to a real SSH server but MUST be structurally valid.

**Acceptance criteria:**
- [ ] `SSHConfig` returns a struct with Host="127.0.0.1", a deterministic port derived from the VM name, User="ubuntu", IdentityFile="/dev/null", and Transport="tcp"
- [ ] The same VM name always produces the same port number
- [ ] `SSHConfig` returns `ErrVMNotRunning` for non-running VMs

### REQ-008-004: In-Memory Backend — Exec

The in-memory backend MUST support a configurable command execution model. By default, `Exec` returns an empty `ExecResult` with exit code 0. A test hook MUST allow tests to register custom command handlers.

**Acceptance criteria:**
- [ ] Default `Exec` returns `ExecResult{Stdout: "", Stderr: "", ExitCode: 0}`
- [ ] A `SetExecHandler(func(ctx context.Context, name string, command []string) (ExecResult, error))` method allows tests to override behavior
- [ ] When a handler is set, it receives the context, VM name, and command, and its return value is used
- [ ] `Exec` returns `ErrVMNotRunning` for non-running VMs (checked before handler invocation)

### REQ-008-004a: In-Memory Backend — Error Injection

The in-memory backend MUST support per-method error injection so that CLI tests can simulate backend failures (e.g., `Start` returning a connection error, `Available` returning "not installed") without needing ad-hoc mock backends.

**Acceptance criteria:**
- [ ] A `SetMethodError(method string, err error)` method configures a specific Backend method to return the given error unconditionally. Valid method names: `"available"`, `"create"`, `"start"`, `"stop"`, `"destroy"`, `"status"`, `"list"`, `"sshconfig"`, `"exec"`
- [ ] Passing `nil` as the error clears the injection for that method
- [ ] Injected errors are checked before state machine logic (e.g., `SetMethodError("start", someErr)` causes `Start` to return `someErr` even for a valid stopped VM)
- [ ] `Reset()` clears all injected errors
- [ ] Error injection is safe for concurrent use (protected by the same mutex)

### REQ-008-005: In-Memory Backend — List and VMInfo

The in-memory backend MUST return accurate `VMInfo` structs from `List()` that reflect the configuration passed to `Create`.

**Acceptance criteria:**
- [ ] `List()` returns a `VMInfo` for every VM that has been created and not destroyed
- [ ] `VMInfo.Name`, `VMInfo.Status`, `VMInfo.Backend` ("memory"), `VMInfo.CPUs`, `VMInfo.Memory`, `VMInfo.Disk`, `VMInfo.CreatedAt` are all populated
- [ ] `VMInfo.IP` is set to "192.168.100.N" where N is derived from creation order
- [ ] `List()` returns an empty slice (not nil) when no VMs exist
- [ ] VMs are returned sorted by name

### REQ-008-006: In-Memory Backend — Optional Interfaces

The in-memory backend MUST implement the `Snapshotter` interface. It SHOULD NOT implement `Cloner` or `Syncer` (these add complexity without significant test value).

**Acceptance criteria:**
- [ ] `SnapshotCreate` captures the full `vmState` (status, config, createdAt) under a tag name
- [ ] `SnapshotApply` restores the VM's status and config to the values captured at snapshot time
- [ ] `SnapshotDelete` removes a stored snapshot
- [ ] `SnapshotList` returns metadata for all snapshots of a VM
- [ ] Snapshot operations return `ErrVMNotFound` for non-existent VMs
- [ ] `SnapshotApply` with an unknown tag returns `ErrSnapshotNotFound`
- [ ] Type assertion `b.(backend.Snapshotter)` succeeds
- [ ] Type assertion `b.(backend.Cloner)` fails
- [ ] Type assertion `b.(backend.Syncer)` fails

### REQ-008-007: In-Memory Backend — Reset

The in-memory backend MUST provide a `Reset()` method that clears all state, returning the backend to its initial empty condition. This enables test isolation.

**Acceptance criteria:**
- [ ] `Reset()` removes all VMs, snapshots, and exec handlers
- [ ] After `Reset()`, `List()` returns an empty slice
- [ ] `Reset()` is safe to call concurrently with other operations (acquires the write lock)

### REQ-008-008: In-Memory Backend — Context Cancellation

The in-memory backend MUST respect context cancellation on all methods, consistent with REQ-003-022.

**Acceptance criteria:**
- [ ] A cancelled context causes methods to return `context.Canceled`
- [ ] A deadline-exceeded context causes methods to return `context.DeadlineExceeded`
- [ ] Context is checked at the start of each method before performing work

### REQ-008-009: Docker Backend — Core Implementation

The system MUST include a Docker backend that implements the `Backend` interface using Docker containers. Containers MUST run an SSH server so that `SSHConfig`, `Exec`, and provisioning work identically to the Lima backend from the caller's perspective.

This implementation replaces the existing Docker stub (REQ-003-020). The stub file at `internal/backend/docker/docker.go` is superseded by this full implementation.

**Acceptance criteria:**
- [ ] Docker backend is implemented in `internal/backend/docker/`
- [ ] It registers itself as `"docker"` in the backend registry via `init()` (replacing the existing stub registration)
- [ ] `Name()` returns `"docker"`
- [ ] `Available()` checks for the `docker` CLI in `$PATH` and verifies the Docker daemon is running (via `docker info`)
- [ ] `Available()` returns an actionable error if Docker is not installed or the daemon is not running
- [ ] All `Backend` interface methods are implemented
- [ ] VM name validation MUST be enforced: `Create` with a name that fails `ValidateVMName` returns `ErrInvalidVMName`

### REQ-008-010: Docker Backend — Container Lifecycle

The Docker backend MUST manage containers that map to the VM lifecycle model.

**Acceptance criteria:**
- [ ] `Create` builds or pulls an image with an SSH server, creates a container, and leaves it stopped. If image build fails, no container is left behind and a subsequent `Create` with the same name succeeds. The error wraps the build failure output for diagnosis.
- [ ] `Start` starts the stopped container and blocks until the SSH server is accepting connections (polling with timeout, max 30s). Returns an error if SSH readiness is not achieved within the timeout.
- [ ] `Stop` stops the running container
- [ ] `Destroy` removes the container and associated volumes
- [ ] Container names are prefixed with `sd-` to avoid collisions (e.g., `sd-myvm`)
- [ ] `Create` on an existing name returns `ErrVMAlreadyExists`
- [ ] State transitions match the spec 003 state machine

### REQ-008-011: Docker Backend — SSH Server

Docker containers MUST run an OpenSSH server so that SSH-based operations (connect, exec, provisioning) work the same way as with Lima VMs.

**Acceptance criteria:**
- [ ] The container image includes `openssh-server`
- [ ] SSHD is configured to accept key-based authentication
- [ ] SSHD is configured with `AcceptEnv SD_* GITHUB_* GH_* ANTHROPIC_*` for credential injection (matching 007-connection.md requirements)
- [ ] A generated SSH key pair is injected into the container's `authorized_keys`
- [ ] The SSH server listens on port 22 inside the container, mapped to a random host port

### REQ-008-012: Docker Backend — SSHConfig

The Docker backend MUST return correct `SSHConfig` for connecting to the container's SSH server.

**Acceptance criteria:**
- [ ] `SSHConfig.Host` is "127.0.0.1"
- [ ] `SSHConfig.Port` is the dynamically mapped host port for the container's SSH
- [ ] `SSHConfig.User` is "ubuntu" (matching Lima convention)
- [ ] `SSHConfig.IdentityFile` points to the generated key for this container
- [ ] `SSHConfig.Transport` is "tcp"
- [ ] The returned config enables a successful SSH connection to the container

### REQ-008-013: Docker Backend — Exec

The Docker backend MUST support command execution via SSH (not `docker exec`), ensuring the execution path matches real backend behavior.

**Acceptance criteria:**
- [ ] `Exec` connects via SSH using the config from `SSHConfig()` and runs the command
- [ ] `ExecResult` contains captured stdout, stderr, and exit code
- [ ] `Exec` returns `ErrVMNotRunning` for stopped containers
- [ ] `Exec` respects context cancellation

### REQ-008-014: Docker Backend — Container Image

The Docker backend MUST use a minimal container image that provides a Linux environment suitable for testing provisioning scripts.

**Acceptance criteria:**
- [ ] Base image is `ubuntu:24.04` (matching Lima default)
- [ ] Image includes: `openssh-server`, `sudo`, `bash`, `curl`, `git`
- [ ] A non-root user `ubuntu` exists with passwordless sudo
- [ ] The image is built via a Dockerfile at `internal/backend/docker/Dockerfile`
- [ ] The image is built automatically on first `Create` if it doesn't exist locally
- [ ] Image is tagged as `sd-test:latest`

### REQ-008-015: Docker Backend — VMInfo and List

The Docker backend MUST return accurate `VMInfo` from `List()` by inspecting Docker container state.

**Acceptance criteria:**
- [ ] `List()` queries Docker for containers with the `sd-` prefix
- [ ] Container status maps to `VMStatus`: running->StatusRunning, exited->StatusStopped, created->StatusStopped. Unmapped Docker states (restarting, paused, dead) map to `StatusError`
- [ ] `VMInfo.Backend` is "docker"
- [ ] `VMInfo.IP` is the container's IP on the Docker bridge network
- [ ] `List()` returns an empty slice when no sd containers exist

### REQ-008-016: Docker Backend — Optional Interfaces

The Docker backend SHOULD NOT implement `Snapshotter`, `Cloner`, or `Syncer` in the initial implementation. These are not needed for the integration testing use case.

**Acceptance criteria:**
- [ ] Type assertion `b.(backend.Snapshotter)` fails
- [ ] Type assertion `b.(backend.Cloner)` fails
- [ ] Type assertion `b.(backend.Syncer)` fails

### REQ-008-017: Docker Backend — Context Cancellation

The Docker backend MUST respect context cancellation on all methods, consistent with REQ-003-022.

**Acceptance criteria:**
- [ ] Long-running Docker commands (build, create, start) are cancelled when context is cancelled
- [ ] The Docker CLI is invoked with timeout derived from context deadline when available

### REQ-008-018: Docker Backend — Cleanup

The Docker backend MUST not leak containers or images. A `Destroy` call MUST remove the container completely.

**Acceptance criteria:**
- [ ] `Destroy` force-removes the container (`docker rm -f`)
- [ ] `Destroy` removes any volumes associated with the container
- [ ] If the container is running, `Destroy` stops it first
- [ ] `Destroy` on a non-existent container returns `ErrVMNotFound`

### REQ-008-019: Test Suite — Memory Backend Unit Tests

A comprehensive unit test suite MUST exercise the in-memory backend's state machine, error handling, and concurrency.

**Acceptance criteria:**
- [ ] Table-driven tests cover every state transition (valid and invalid)
- [ ] Property-based tests (rapid) verify: state machine invariants hold across random operation sequences, `List()` always reflects current state, concurrent operations don't corrupt state
- [ ] Sentinel error tests verify every error condition returns the correct sentinel
- [ ] VMConfig values passed to `Create` are correctly reflected in `List`/`Status`

### REQ-008-020: Test Suite — CLI Command Tests

The in-memory backend MUST be used to write tests for every CLI command in `internal/cmd/`.

**Acceptance criteria:**
- [ ] Each command file has a corresponding `_test.go` file
- [ ] Tests inject the memory backend via `getBackendFunc`
- [ ] Tests verify: stdout output (human and JSON modes), stderr output, exit codes, error messages
- [ ] Tests cover: success paths, error paths (invalid args, VM not found, etc.), flag combinations
- [ ] No test requires Lima, Docker, or any external tool to be installed
- [ ] CLI command tests MUST NOT use `t.Parallel()` because `getBackendFunc` is a package-level variable swapped per test. This constraint is documented in a comment at the top of each test file

### REQ-008-021: Test Suite — Docker Backend Integration Tests

Integration tests MUST verify the Docker backend works end-to-end with real containers.

**Acceptance criteria:**
- [ ] Tests are gated behind `//go:build integration` build tag
- [ ] Tests verify the full lifecycle: Create -> Start -> SSHConfig -> Exec -> Stop -> Destroy
- [ ] Tests verify SSH connectivity: connect, run command, check output
- [ ] Tests verify provisioning: a simple provisioning script creates a file, Exec confirms it exists
- [ ] Tests clean up all containers after completion (using t.Cleanup)
- [ ] Tests skip with a clear message if Docker is not available
- [ ] Tests MUST set `$SD_HOME` to a temporary directory to prevent collision with the user's production VM state
- [ ] The test package MUST include a `TestMain` function that performs cleanup of any leaked `sd-` prefixed containers before and after the test suite runs (`docker rm -f` on any containers matching `sd-*`). This handles the case where `t.Cleanup` does not run due to a panic

### REQ-008-022: Test Suite — Backend Conformance Tests

A shared conformance test suite MUST exist that can be run against ANY backend implementation to verify it satisfies the `Backend` interface contract.

**Acceptance criteria:**
- [ ] Conformance tests MUST be in `internal/backend/conformance/` as an importable package (not a `_test.go` file), so that backend-specific test files can call `conformance.RunAll(t, opts)`
- [ ] Tests accept a `ConformanceOpts` struct containing: the backend instance, a per-backend timeout, optional setup/teardown functions
- [ ] Conformance tests accept either `StatusRunning` or `StatusStopped` after `Create` (per spec 003's "backend-dependent" language)
- [ ] Tests cover: full lifecycle, state transitions, error semantics, list behavior, context cancellation
- [ ] The memory backend passes conformance tests in normal `go test`
- [ ] The Docker backend passes conformance tests with `//go:build integration`
- [ ] The Lima backend can pass conformance tests with `//go:build e2e` (not required in CI)

## Design

### In-Memory Backend Structure

```go
package memory

import (
    "context"
    "sync"
    "time"

    "sd/internal/backend"
)

// Backend is an in-memory implementation of backend.Backend for testing.
type Backend struct {
    mu          sync.RWMutex
    vms         map[string]*vmState
    snapshots   map[string]map[string]*vmState // vm -> tag -> state
    execHandler func(ctx context.Context, name string, command []string) (backend.ExecResult, error)
    nextIP      int
}

type vmState struct {
    name      string
    status    backend.VMStatus
    config    backend.VMConfig
    createdAt time.Time
    port      int
}

func New() *Backend { ... }
func (b *Backend) Reset() { ... }
func (b *Backend) SetExecHandler(h func(context.Context, string, []string) (backend.ExecResult, error)) { ... }
func (b *Backend) SetStatus(name string, status backend.VMStatus) error { ... }

// All backend.Backend methods ...
// Snapshotter methods ...
```

### Docker Backend Structure

```go
package docker

import (
    "context"
    "os/exec"

    "sd/internal/backend"
)

// Backend manages Docker containers as VM substitutes for testing.
type Backend struct {
    dockerBin string // path to docker CLI
}

func New() *Backend { ... }

// All backend.Backend methods ...
```

### Container Image (Dockerfile)

```dockerfile
FROM ubuntu:24.04

RUN apt-get update && apt-get install -y \
    openssh-server sudo bash curl git \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -s /bin/bash ubuntu \
    && echo "ubuntu ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers \
    && mkdir -p /home/ubuntu/.ssh \
    && chmod 700 /home/ubuntu/.ssh \
    && chown ubuntu:ubuntu /home/ubuntu/.ssh

RUN mkdir /run/sshd \
    && sed -i 's/#AcceptEnv/AcceptEnv/' /etc/ssh/sshd_config \
    && echo "AcceptEnv SD_* GITHUB_* GH_* ANTHROPIC_*" >> /etc/ssh/sshd_config

EXPOSE 22
CMD ["/usr/sbin/sshd", "-D"]
```

### SSH Key Management for Docker Backend

On `Create`:
1. Generate an ed25519 key pair in `$SD_HOME/vms/<name>/ssh/`
2. Inject the public key into the container's `/home/ubuntu/.ssh/authorized_keys`
3. Store the identity file path for `SSHConfig` responses

This mirrors the Lima backend's SSH key flow defined in spec 007.

### Conformance Test Pattern

```go
package conformance

import (
    "context"
    "testing"
    "time"

    "sd/internal/backend"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// ConformanceOpts configures the conformance test suite for a specific backend.
type ConformanceOpts struct {
    Backend  backend.Backend
    Timeout  time.Duration       // Per-test timeout (default 5s for memory, 60s for Docker)
    Setup    func(t *testing.T)  // Optional: called before each test
    Teardown func(t *testing.T)  // Optional: called after each test (via t.Cleanup)
}

// RunAll runs the full conformance suite against the given backend.
func RunAll(t *testing.T, opts ConformanceOpts) {
    t.Run("Lifecycle", func(t *testing.T) { testLifecycle(t, opts) })
    t.Run("StateTransitions", func(t *testing.T) { testStateTransitions(t, opts) })
    t.Run("ErrorSemantics", func(t *testing.T) { testErrorSemantics(t, opts) })
    t.Run("ListBehavior", func(t *testing.T) { testListBehavior(t, opts) })
    t.Run("ContextCancellation", func(t *testing.T) { testContextCancellation(t, opts) })
}

// Note: testStateTransitions accepts EITHER StatusRunning or StatusStopped
// after Create, per spec 003's "backend-dependent" language.
```
```

### Backend Registration

The Docker backend registers at init time (replacing the existing stub), same as Lima:

```go
// docker/docker.go
func init() {
    backend.Register("docker", New())
}
```

The memory backend does NOT register via init(). Tests instantiate it explicitly:

```go
// In test code:
mb := memory.New()
getBackendFunc = func(name string) (backend.Backend, error) { return mb, nil }
```

### Port Assignment (Memory Backend)

Deterministic port from VM name using hash:

```go
func portFromName(name string) int {
    h := fnv.New32a()
    h.Write([]byte(name))
    return 10000 + int(h.Sum32()%50000)
}
```

## Error Handling

### Memory Backend Errors

All errors use the sentinel errors from `internal/backend/errors.go`. No new error types are introduced.

| Condition | Error | Severity |
|-----------|-------|----------|
| Create with existing name | `ErrVMAlreadyExists` | Fatal |
| Operation on non-existent VM | `ErrVMNotFound` | Fatal |
| Exec/SSHConfig on non-running VM | `ErrVMNotRunning` | Fatal |
| Snapshot with unknown tag | `ErrSnapshotNotFound` | Fatal |
| Context cancelled | `context.Canceled` | Warning |

### Docker Backend Errors

| Condition | Error | Severity |
|-----------|-------|----------|
| Docker not installed | `ErrBackendNotAvailable` | Fatal |
| Docker daemon not running | `ErrBackendNotAvailable` | Fatal |
| Create with existing name | `ErrVMAlreadyExists` | Fatal |
| Operation on non-existent container | `ErrVMNotFound` | Fatal |
| Exec/SSHConfig on stopped container | `ErrVMNotRunning` | Fatal |
| Image build failure | Wrapped error with context | Fatal |
| SSH connection failure | Wrapped error with context | Fatal |
| Context cancelled | `context.Canceled` | Warning |

## Security Considerations

### Memory Backend

The memory backend has no security implications. It stores data only in process memory. No files, no network connections, no subprocesses.

### Docker Backend

- **Container isolation is weaker than VM isolation.** Containers share the host kernel. This is acceptable because the Docker backend is for testing only, not production agent workloads.
- **SSH keys are generated per-container** and stored under `$SD_HOME/vms/<name>/ssh/` with mode 0600, matching the Lima backend pattern.
- **No host Docker socket mounting** — containers cannot access the host Docker daemon.
- **Containers run with default Docker security settings** — no `--privileged`, no extra capabilities.
- **The Dockerfile does not install unnecessary packages** — minimal surface area.

### Blast Radius

If a test container is compromised:
- Access is limited to the container's filesystem and Docker bridge network
- No host filesystem mounts (same no-mount default as Lima)
- The container has no access to host credentials (SSH env injection only happens during explicit connect/exec)

## Testing Strategy

### Unit Tests (Memory Backend)

| Requirement | Test |
|---|---|
| REQ-008-001 | Verify interface compliance at compile time; verify no disk/subprocess calls |
| REQ-008-002 | Table-driven: every valid state transition succeeds; every invalid transition returns correct error |
| REQ-008-003 | Verify SSHConfig returns consistent values; verify error on non-running VM |
| REQ-008-004 | Verify default Exec returns exit 0; verify custom handler is called; verify error on non-running VM |
| REQ-008-005 | Verify List reflects creates/destroys; verify VMInfo fields; verify empty slice not nil |
| REQ-008-006 | Verify Snapshotter type assertion; verify snapshot create/apply/delete/list |
| REQ-008-007 | Verify Reset clears all state |
| REQ-008-008 | Verify context cancellation on every method |

### Property-Based Tests (Memory Backend)

Using `pgregory.net/rapid`:

1. **State machine invariant**: Generate random sequences of Create/Start/Stop/Destroy/Status operations. After every operation, verify: `List()` contains exactly the VMs that should exist, each VM's status matches what the state machine predicts, error returns match sentinel expectations.

2. **Concurrent safety**: Generate parallel operation sequences and verify no panics, no data races (run with `-race`), and `List()` is always consistent.

3. **Snapshot invariant**: After `SnapshotCreate` + modifications + `SnapshotApply`, the VM state matches the snapshot point.

### Integration Tests (Docker Backend)

Gated behind `//go:build integration`:

| Requirement | Test |
|---|---|
| REQ-008-009 | Verify `Available()` returns nil when Docker is running |
| REQ-008-010 | Full lifecycle: Create, Start, Stop, Destroy |
| REQ-008-011 | Verify SSH connectivity to container |
| REQ-008-012 | Verify SSHConfig returns connectable details |
| REQ-008-013 | Verify Exec via SSH: `echo hello` returns expected output |
| REQ-008-014 | Verify container has expected packages (curl, git, sudo) |
| REQ-008-015 | Verify List returns accurate container state |
| REQ-008-018 | Verify Destroy cleans up completely; `docker ps -a` shows no sd- container |

### Conformance Tests

| Backend | Build tag | CI? |
|---------|-----------|-----|
| Memory | (none) | Yes |
| Docker | `integration` | Optional |
| Lima | `e2e` | No |

## Dependencies

### Depends On

- [001-architecture.md](001-architecture.md) — package layout and component model
- [003-vm-backend.md](003-vm-backend.md) — Backend interface, types, registry, sentinel errors
- [007-connection.md](007-connection.md) — SSH key management pattern, AcceptEnv/SendEnv configuration
- Docker (runtime dependency for Docker backend only)
- `pgregory.net/rapid` (existing test dependency)
- `github.com/stretchr/testify` (existing test dependency)

### Depended On By

- All `internal/cmd/` test files — CLI tests use the memory backend
- Future backend implementations — can run conformance tests to verify correctness

## Alternatives Considered

### Alternative 1: Enhanced Configurable Mock Instead of Full State Machine Backend

Using a generic mock struct with configurable per-method responses (like the existing ad-hoc mocks in `internal/cmd/*_test.go`) instead of implementing a full state machine in the memory backend.

**Rejected because:** The existing 20+ hand-written mock backends in CLI test files (e.g., `mockListBackend`, `mockCreateBackend`) duplicate state machine logic inconsistently. A full state machine provides higher fidelity testing, enables property-based tests, and serves as the single source of truth for backend behavior. The configurable exec handler (`SetExecHandler`) provides the flexibility of a mock where needed.

### Alternative 2: `docker exec` Instead of SSH for Docker Backend

Using `docker exec` for command execution instead of running a real SSH server inside containers.

**Rejected because:** The SSH path is the production code path. Using `docker exec` would bypass SSH key management, AcceptEnv/SendEnv credential injection, and proxy command configuration — exactly the plumbing that integration tests need to validate. The ~2s overhead per test of SSH handshake is acceptable for the increased test fidelity.

### Alternative 3: Podman as an Alternative to Docker

Using Podman (daemonless, rootless containers) instead of or alongside Docker.

**Deferred.** Podman is CLI-compatible with Docker for the operations this backend needs. A future enhancement could check for Podman as a fallback when Docker is unavailable. The current design does not preclude this since all Docker interactions go through the `docker` CLI binary which Podman can alias.

## Revision History

| Date       | Author | Change Description |
|------------|--------|--------------------|
| 2026-04-03 | claude | Initial draft      |
| 2026-04-03 | claude | Review fixes: (1) Memory backend no longer registers via init() — tests use New() directly, preventing production build contamination. (2) Create transitions to StatusRunning matching Lima behavior. (3) Added Error state transitions and SetStatus test hook. (4) Added Transport="tcp" to SSHConfig. (5) Conformance tests mandated as importable subpackage with ConformanceOpts for per-backend config. (6) ExecHandler signature now receives context. (7) Added Name() and VM name validation to acceptance criteria. (8) Docker backend explicitly supersedes stub. (9) Added SSH readiness polling in Docker Start. (10) Added GH_* to Dockerfile AcceptEnv. (11) Added TestMain cleanup for Docker integration tests. (12) Added $SD_HOME temp dir requirement for Docker tests. (13) Added t.Parallel() constraint for CLI tests. (14) Added Alternatives Considered section. (15) Clarified snapshot state capture semantics. (16) Added unmapped Docker state handling. |
