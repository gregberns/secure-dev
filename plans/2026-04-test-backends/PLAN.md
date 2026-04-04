# Plan: Test Backends (Memory and Docker)

| Field        | Value                          |
|--------------|--------------------------------|
| Spec         | specs/008-test-backends.md     |
| Status       | approved                       |
| Created      | 2026-04-03                     |
| Branch       | test-backends                  |

## Summary

Implement two new backend implementations — an in-memory backend for fast unit/CLI testing and a Docker backend for integration testing — plus a shared conformance test suite and migration of existing CLI tests to use the memory backend. This eliminates the need for Lima VMs during automated testing.

## Architecture Decisions

1. **Memory backend does not register via init()** (REQ-008-001). Tests instantiate via `memory.New()` and inject through `getBackendFunc`. This prevents contamination of the production binary.
2. **Docker backend replaces the existing stub** at `internal/backend/docker/docker.go` (REQ-008-009). The stub's `init()` registration is preserved but the implementation becomes real.
3. **Conformance tests live in an importable subpackage** `internal/backend/conformance/` (REQ-008-022) so all backends can run the same suite.
4. **Memory backend Create transitions to StatusRunning** matching Lima's behavior for maximum fidelity (REQ-008-002).
5. **Docker backend uses real SSH** (not `docker exec`) to match the production code path (REQ-008-013). SSH operations shell out to the `ssh` binary (not `golang.org/x/crypto/ssh`) to avoid a new dependency and match how Lima's Exec works.
6. **Docker backend Create leaves container in StatusStopped** (requiring explicit Start), while memory backend transitions to StatusRunning. This matches each backend's real-world analog. Conformance tests handle this by accepting either state after Create.
7. **Dockerfile is embedded via `//go:embed`** so the Docker backend works from an installed binary without the source tree.
8. **Memory backend implementation stays in a single file** (`memory.go`). The struct's unexported fields are shared across all methods; splitting would create coupling between files.

## Dependency DAG

```
Tasks 1-2 (memory backend)
    |
    +---> Task 3 (memory tests)
    |         |
    |         +---> Task 4 (conformance suite)
    |                    |
    |                    +---> Task 9 (Docker conformance)
    |
    +---> Task 10a-c (CLI test migration, can start after Task 3)
    |
    +--- (independent) ---> Tasks 5-7 (Docker backend, parallel stream)
                                |
                                +---> Task 8 (Docker integration tests)
                                |
                                +---> Task 9 (Docker conformance)
```

Tasks 3/4/10 and Tasks 5-7/8 are independent streams that can run in parallel after Tasks 1-2 complete.

## Implementation Order

### Task 1: In-Memory Backend Core [secure-dev-xqp]

- **Spec refs:** REQ-008-001, REQ-008-002, REQ-008-003, REQ-008-004, REQ-008-004a, REQ-008-005, REQ-008-007, REQ-008-008
- **Files:**
  - Create `internal/backend/memory/memory.go`
- **Description:** Implement the full `Backend` interface as an in-memory store. Includes:
  - `Backend` struct with mutex-protected VM map, snapshot map, exec handler, error injections, IP counter
  - All 10 `Backend` interface methods (`Name`, `Available`, `Create`, `Start`, `Stop`, `Destroy`, `Status`, `List`, `SSHConfig`, `Exec`)
  - State machine enforcement with correct sentinel errors
  - `New()`, `Reset()`, `SetExecHandler()`, `SetStatus()` public API
  - `SetMethodError(method string, err error)` for per-method error injection (methods: "available", "create", "start", "stop", "destroy", "status", "list", "sshconfig", "exec"). Injected errors are checked before state machine logic. `nil` clears injection. `Reset()` clears all.
  - VM name validation via `backend.ValidateVMName`
  - Context cancellation checks at method entry
  - Deterministic SSHConfig with Transport="tcp"
  - No init() registration
- **Tests:** Covered by Task 3
- **Acceptance:** `go build ./internal/backend/memory/` succeeds; compile-time interface check passes

### Task 2: In-Memory Backend Snapshotter [secure-dev-rg7]

- **Spec refs:** REQ-008-006
- **Files:**
  - Add snapshot methods to `internal/backend/memory/memory.go`
- **Description:** Implement the `Snapshotter` interface on the memory backend. Snapshots capture full `vmState` (status, config, createdAt). Apply restores these values. The backend must NOT implement `Cloner` or `Syncer`.
- **Tests:** Covered by Task 3
- **Acceptance:** Type assertion `b.(backend.Snapshotter)` succeeds; `b.(backend.Cloner)` fails; `b.(backend.Syncer)` fails

### Task 3: In-Memory Backend Unit and Property Tests [secure-dev-sdn]

- **Spec refs:** REQ-008-019
- **Files:**
  - Create `internal/backend/memory/memory_test.go`
- **Description:** Comprehensive test suite for the memory backend:
  - Table-driven tests for every state transition (valid and invalid)
  - Sentinel error verification for every error condition
  - Property-based tests (rapid): random operation sequences verify state machine invariants, List() consistency, concurrent safety
  - Snapshot round-trip property test
  - SSHConfig determinism and VMInfo field verification
  - Reset() clears all state
  - Context cancellation on every method
  - SetStatus test hook for Error state transitions
  - SetMethodError injection tests
- **Tests:** This IS the test task
- **Acceptance:** `go test -race ./internal/backend/memory/` passes; all property tests pass

### Task 4: Conformance Test Suite [secure-dev-nwb]

- **Spec refs:** REQ-008-022
- **Files:**
  - Create `internal/backend/conformance/conformance.go`
  - Create `internal/backend/conformance/conformance_test.go` (tests the conformance suite against memory backend)
- **Description:** Shared conformance test suite that any backend can run. Includes:
  - `ConformanceOpts` struct (Backend, Timeout, Setup, Teardown)
  - `RunAll(t, opts)` entry point
  - Sub-tests: Lifecycle, StateTransitions, ErrorSemantics, ListBehavior, ContextCancellation
  - `ErrorSemantics` explicitly tests `ErrInvalidVMName` (Create with invalid name)
  - Accepts either StatusRunning or StatusStopped after Create — tests query `Status()` after `Create()` and conditionally call `Stop()` before testing Stopped-to-Running transitions
  - Optional `SnapshotLifecycle` sub-test: skipped if `opts.Backend.(backend.Snapshotter)` fails; otherwise tests create/apply/delete/list cycle
  - Memory backend conformance test in the `_test.go` file (runs in normal `go test`)
- **Tests:** Memory backend conformance test in same task
- **Acceptance:** `go test ./internal/backend/conformance/` passes

### Task 5: Docker Backend — Dockerfile and Image Build [secure-dev-y37]

- **Spec refs:** REQ-008-014, REQ-008-011
- **Files:**
  - Create `internal/backend/docker/Dockerfile`
  - Add image build logic to `internal/backend/docker/docker.go`
- **Description:** Create the container image definition and build infrastructure:
  - Dockerfile based on ubuntu:24.04 with openssh-server, sudo, bash, curl, git
  - Non-root `ubuntu` user with passwordless sudo
  - SSHD configured with AcceptEnv SD_* GITHUB_* GH_* ANTHROPIC_*
  - Embed Dockerfile into Go binary via `//go:embed Dockerfile` so the backend works from installed binary without source tree. Build function pipes embedded content to `docker build -f- .`
  - Image build function that builds `sd-test:latest` on first Create if not present
  - Build failure produces wrapped error, no partial image left behind. Subsequent Create retries build.
- **Tests:** Covered by Task 8
- **Acceptance:** `docker build -t sd-test:latest internal/backend/docker/` succeeds; container starts with working SSHD

### Task 6: Docker Backend — Core Lifecycle [secure-dev-xfh]

- **Spec refs:** REQ-008-009, REQ-008-010, REQ-008-015, REQ-008-016, REQ-008-017, REQ-008-018
- **Files:**
  - Rewrite `internal/backend/docker/docker.go` (replacing the stub)
- **Description:** Implement the full `Backend` interface using Docker containers:
  - Replace stub with real implementation; keep init() registration as "docker" using `New()` constructor
  - `Available()` checks for `docker` CLI and running daemon (`docker info`)
  - `Create` builds image if needed, creates container (`sd-` prefix), generates SSH keys, creates `$SD_HOME/vms/<name>/ssh/` directory (mode 0700)
  - `Start` starts container, polls for SSH readiness (max 30s)
  - `Stop` stops container
  - `Destroy` force-removes container and volumes
  - `Status` maps Docker states to VMStatus (running->Running, exited/created->Stopped, other->Error)
  - `List` queries `sd-` prefixed containers
  - VM name validation via `backend.ValidateVMName`
  - Context cancellation on all methods
  - No optional interfaces: type assertions for `Snapshotter`, `Cloner`, `Syncer` must all fail (REQ-008-016)
- **Tests:** Covered by Task 8
- **Acceptance:** `go build ./internal/backend/docker/` succeeds; compile-time interface check passes

### Task 7: Docker Backend — SSH Operations [secure-dev-7p2]

- **Spec refs:** REQ-008-012, REQ-008-013
- **Files:**
  - Add SSH methods to `internal/backend/docker/ssh.go` (separate file — SSH key generation and connection management are a distinct concern from container lifecycle)
- **Description:** Implement SSHConfig and Exec using real SSH connections:
  - `SSHConfig` returns Host="127.0.0.1", dynamically mapped port, User="ubuntu", Transport="tcp", IdentityFile pointing to generated key
  - `Exec` shells out to the `ssh` binary (not `golang.org/x/crypto/ssh`) to avoid a new dependency and match how Lima's backend works. Constructs SSH command from SSHConfig.
  - SSH key generation (ed25519) on Create, public key injected into container's authorized_keys via `docker cp`
  - Keys stored under `$SD_HOME/vms/<name>/ssh/` with mode 0600
- **Tests:** Covered by Task 8
- **Acceptance:** Manual verification: SSH into running container with returned config

### Task 8: Docker Backend Integration Tests [secure-dev-j30]

- **Spec refs:** REQ-008-021
- **Files:**
  - Create `internal/backend/docker/integration_test.go`
- **Description:** Integration test suite gated behind `//go:build integration`:
  - `TestMain` for cleanup of leaked `sd-` containers before/after suite
  - `$SD_HOME` set to temp directory
  - Full lifecycle test: Create -> Start -> SSHConfig -> Exec -> Stop -> Destroy
  - SSH connectivity verification
  - Provisioning test: script creates file, Exec confirms
  - List accuracy test
  - Cleanup verification: no leaked containers after Destroy
  - Skip with message if Docker unavailable
- **Tests:** This IS the test task
- **Acceptance:** `go test -tags integration ./internal/backend/docker/` passes (with Docker running)

### Task 9: Docker Backend Conformance Tests [secure-dev-6s0]

- **Spec refs:** REQ-008-022
- **Files:**
  - Add conformance test call to `internal/backend/docker/integration_test.go`
- **Description:** Wire the Docker backend into the conformance test suite:
  - Call `conformance.RunAll(t, ConformanceOpts{Backend: dockerBackend, Timeout: 300*time.Second, ...})` (300s to account for container start + SSH polling per VM)
  - Gated behind `//go:build integration`
  - Setup creates backend with temp $SD_HOME; Teardown cleans up
- **Tests:** This IS the test task
- **Acceptance:** Docker backend passes all conformance tests

### Task 10a: CLI Test Migration — Lifecycle Commands [secure-dev-il7]

- **Spec refs:** REQ-008-020
- **Files (modify):**
  - `internal/cmd/create_test.go`
  - `internal/cmd/start_test.go`
  - `internal/cmd/stop_test.go`
  - `internal/cmd/destroy_test.go`
  - `internal/cmd/list_test.go`
  - `internal/cmd/status_test.go`
- **Description:** Migrate the 6 core lifecycle command tests to use memory backend:
  - Remove per-file mock backend structs (e.g., `mockCreateBackend`, `mockStopBackend`)
  - Import `memory` package, instantiate via `memory.New()`
  - Inject via `getBackendFunc`
  - Use `SetMethodError` for tests that inject errors on Start, Stop, Status, etc.
  - Use `SetStatus` for Error state tests
  - Add `t.Cleanup(func() { mb.Reset() })` for isolation
  - Add comment at top of each file: no `t.Parallel()` due to global getBackendFunc
  - Preserve the existing `resetSliceFlag` pattern in create_test.go (tech debt, not addressed here)
- **Tests:** Existing tests preserved but use new backend
- **Acceptance:** `go test -run 'Test(Create|Start|Stop|Destroy|List|Status)' ./internal/cmd/` passes; no mock backend structs remain in these 6 files

### Task 10b: CLI Test Migration — SSH/Exec/Connect Commands [secure-dev-9iq]

- **Spec refs:** REQ-008-020
- **Files (modify):**
  - `internal/cmd/exec_test.go`
  - `internal/cmd/connect_test.go` (HIGH RISK: 1632 lines, complex mock with per-VM state)
  - `internal/cmd/ssh_config_test.go`
  - `internal/cmd/doctor_test.go`
  - `internal/cmd/logs_test.go`
  - `internal/cmd/completion_test.go`
- **Description:** Migrate SSH/exec-oriented command tests:
  - `connect_test.go` requires special attention: the existing `mockConnectBackend` has per-VM SSHConfig, startErr, available flag. Use `SetMethodError` for error injection. Review SSH command assertions for hardcoded port/identity values that may differ from memory backend's deterministic values.
  - `completion_test.go` uses listErr injection — use `SetMethodError("list", err)`
  - Use `SetExecHandler` for tests needing custom command output
- **Tests:** Existing tests preserved but use new backend
- **Acceptance:** `go test -run 'Test(Exec|Connect|SSHConfig|Doctor|Logs|Completion)' ./internal/cmd/` passes; no mock backend structs remain in these 6 files

### Task 10c: CLI Test Migration — Optional Interface Commands [secure-dev-xue]

- **Spec refs:** REQ-008-020
- **Files (modify):**
  - `internal/cmd/snapshot_test.go`
  - `internal/cmd/provision_test.go`
  - `internal/cmd/security_test.go`
  - `internal/cmd/diff_test.go`
  - `internal/cmd/sync_test.go`
- **Description:** Migrate tests for commands that use optional interfaces:
  - `snapshot_test.go`: memory backend implements `Snapshotter`, so these migrate directly
  - `provision_test.go`: uses `SetExecHandler` for mock provisioning script results
  - `security_test.go`: standard lifecycle + exec mock
  - `diff_test.go` and `sync_test.go`: these tests require `Syncer` interface which memory backend does NOT implement (per REQ-008-006). **Strategy:** keep thin wrapper types that embed `*memory.Backend` and add `Syncer` methods for these specific test files only. These are NOT ad-hoc full mocks — they delegate all Backend methods to the memory backend and only add the missing optional interface.
- **Tests:** Existing tests preserved but use new backend
- **Acceptance:** `go test -run 'Test(Snapshot|Provision|Security|Diff|Sync)' ./internal/cmd/` passes; full mock backend structs removed (thin Syncer wrappers allowed for diff/sync)

### Excluded CLI Test Files (no backend dependency)

The following test files do NOT use `getBackendFunc` or mock backends and require no migration:
- `internal/cmd/audit_test.go`
- `internal/cmd/config_test.go`
- `internal/cmd/config_egress_test.go`
- `internal/cmd/root_test.go`
- `internal/cmd/token_test.go`
- `internal/cmd/version_test.go`

## Testing Plan

### Property-Based Tests

Using `pgregory.net/rapid`:

- **State machine invariant:** Random sequences of Create/Start/Stop/Destroy/Status against memory backend. After each operation, verify List() matches expected state, Status() returns correct value, errors match sentinels.
- **Concurrent safety:** Parallel random operations on memory backend under `-race`. Verify no panics, no data corruption.
- **Snapshot round-trip:** SnapshotCreate + random modifications + SnapshotApply = original state.

Generators needed:
- VM name generator (valid names matching `^[a-z][a-z0-9-]{0,62}$`)
- VMConfig generator (random CPUs/Memory/Disk within valid ranges)
- Operation sequence generator (random mix of lifecycle operations)

### Unit Tests

Memory backend (`memory_test.go`):
- Table-driven state transition matrix (every from-state x operation combination)
- Sentinel error verification per error condition
- SSHConfig determinism (same name = same port, always)
- VMInfo field accuracy after Create
- Reset clears everything
- SetStatus forces Error state; verify Stop/Destroy/Exec behavior from Error

### Integration Tests

Docker backend (`integration_test.go`, gated `//go:build integration`):
- Full lifecycle: create, start, exec, stop, destroy
- SSH connectivity with returned SSHConfig
- Provisioning: script creates `/tmp/test-marker`, Exec verifies
- List accuracy across state changes
- Cleanup: no leaked containers post-destroy
- TestMain safety net for panics

### Conformance Tests

`internal/backend/conformance/`:
- Lifecycle: create -> start -> stop -> destroy (clean path)
- State transitions: all valid + invalid combinations
- Error semantics: every sentinel error triggered and verified (including `ErrInvalidVMName`)
- Snapshot lifecycle (optional): create, apply, delete, list — skipped if backend doesn't implement Snapshotter
- List: empty, one VM, multiple VMs, after destroy
- Context cancellation: cancelled context returns correct error

Run against: memory (always), Docker (integration tag), Lima (e2e tag, not required)

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Docker image build flaky (network, apt mirrors) | Image is cached after first build; TestMain handles cleanup of partial state; subsequent Create retries build |
| SSH readiness timing in Docker containers | Start polls with backoff up to 30s; conformance test timeout is 300s |
| CLI test migration breaks existing assertions | Incremental migration per-file; run `go test` after each file; split into 3 sub-tasks by complexity |
| Memory backend diverges from Lima behavior over time | Conformance suite runs against both; any new Lima behavior must also pass in conformance |
| getBackendFunc swap not parallel-safe | Documented constraint: no t.Parallel() in cmd tests |
| connect_test.go SSH command assertions have hardcoded values | Review assertions for port/identity values before migration; use SetMethodError for error injection |
| diff/sync tests need Syncer interface memory backend doesn't implement | Thin wrapper types that embed memory.Backend and add only Syncer methods |
| Memory backend exec handler insufficient for complex command routing | Fallback: allow thin wrapper types that embed memory.Backend and override Exec for specific test files |

## Dependencies

- Docker (must be installed and running for integration tests; not required for unit tests)
- `pgregory.net/rapid` (already in go.mod)
- `github.com/stretchr/testify` (already in go.mod)
- No new Go dependencies required
