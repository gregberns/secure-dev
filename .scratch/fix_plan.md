# Fix Plan -- Incomplete/Unimplemented Functionality

## Completed

### Bug Fix: config/loader.go:287 -- Warnings() double RLock deadlock
`defer l.mu.RLock()` changed to `defer l.mu.RUnlock()`. Would deadlock on second call.

### Lima backend integration tests with mocklimactl digital twin
Added `internal/backend/lima/integration_test.go` exercising the full VM lifecycle through the mocklimactl digital twin:
- Full Create->Start->Exec->Stop->Destroy lifecycle
- Idempotent start/stop operations
- Duplicate VM name rejection (ErrVMAlreadyExists)
- Operations on nonexistent VMs (ErrVMNotFound)
- Exec on stopped VM (ErrVMNotRunning)
- SSHConfig on running vs stopped VMs
- List empty and with multiple VMs
- Destroy running VM (auto-stop)
- Invalid config rejection
- Context cancellation
- Concurrent VM creation
- Full snapshot lifecycle (create/list/apply/delete)
- Snapshot errors on nonexistent VMs and tags

### Concurrent audit logger tests
Added `internal/security/audit_concurrent_test.go`:
- 100 concurrent writes (50 commands + 50 events) preserve hash chain integrity
- Concurrent read/write safety (50 writers + 50 readers simultaneously)

### Flaky rapid test fix: TestProperty_ListBackendAlwaysLima
`internal/backend/lima/lifecycle_rapid_test.go:892` — changed name generation from
`vm-[a-z0-9]{3}` (which could duplicate) to `fmt.Sprintf("vm-%d-%s", i, suffix)` using
loop index to guarantee uniqueness.

### Audit logger Query filter tests
Added `internal/security/audit_query_test.go`:
- Query by VM name filters correctly
- query by time range (since/until) filters correctly
- combined VM name + time range filters
- query on empty log returns empty results

### Bug Fix: config.go -- Validate() Memory/Disk format validation
REQ-003-011 specifies Memory (e.g., "4GiB") and Disk (e.g., "50GiB") must use `<number><unit>` format.
Added `resourceSizePattern` regex validation for both fields. Added table-driven tests and property-based tests:
- Table tests: valid formats (K/KiB/KB/M/MiB/MB/G/GiB/GB/T/TiB/TB) pass, invalid formats (plain numbers, wrong case, spaces, negative, decimal) fail
- Property tests: valid resource sizes always pass, invalid always fail with ErrInvalidConfig, random valid combos pass, error messages include the bad value and "format" hint

### Bug Fix: backend/lima/lima.go -- parseListOutput header handling and field parsing
Fixed `parseListOutput` to handle real limactl output correctly:
- Switched `List` to call `limactl list --json` for structured, reliable parsing
- `parseListOutput` now tries JSON first, falls back to text parsing with header detection
- Text fallback detects and skips header lines (first field == "NAME")
- Text fallback handles both 9-field real limactl format and legacy 6-field compact format
- Updated mocklimactl `listVMs` to support `--json` flag and emit header line in text mode
- Updated `parseVMList` in executor.go to skip header lines
- Added tests: JSON parsing, real limactl text format, header-only detection, property-based JSON roundtrip, header-never-parsed-as-VM property
- Updated existing mocklimactl tests to account for new header line in text output

## Missing Functionality (Spec vs Code Gaps)

### CLI foundation implemented (partial)
REQ-002-001, REQ-002-010, REQ-002-014, REQ-002-015, REQ-002-018, REQ-002-019:
- `cmd/sd/main.go` entry point calling `internal/cmd.Execute()`
- `internal/cmd/root.go` with root command, global flags (--json, --verbose, --quiet, --config, --vm), command groups, PersistentPreRunE, prefix matching, VM name resolution
- `internal/ui/formatter.go` with CLIError type and Formatter (human + JSON output modes)
- Tests for all above (14 cmd tests, 18 ui tests)
- rootCmd initialized at declaration time to fix init() ordering with other command files

### Version command implemented (REQ-002-007)
- `internal/cmd/version.go` with build variables (Version, GitCommit, BuildDate) set via ldflags
- Human output: prints version, git commit, build date, go version
- JSON output: wraps in standard `{"ok": true, "data": {...}}` envelope
- Works without config file (in "no config required" set)
- Tests: registration, human output, JSON output, no-config-required, build var defaults, Go version populated, versionInfo struct fields, JSON serialization, formatter integration (human+JSON), property tests (always succeeds, formatter always produces valid JSON with all required fields)

### List command implemented (REQ-002-003, REQ-002-009)
- `internal/cmd/list.go` with `sd list` and `sd ls` alias
- Human output: tab-formatted table (NAME, STATUS, BACKEND, CPUS, MEMORY, DISK, IP)
- JSON output: `{"ok": true, "data": [...]}` envelope
- Works without config file (falls back to default backend)
- Uses getBackendFunc for testability (overridden in tests with mock backend digital twin)
- Tests: registration, alias, empty list (human+JSON), multiple VMs (human+JSON), backend error, backend unavailable, no-config-required, formatVMTable unit tests, property tests (JSON always valid, human always has header, table contains all names, dual-mode consistency), benchmarks

### Destroy command implemented (REQ-002-003)
- `internal/cmd/destroy.go` with `sd destroy <name> --force/-f` command
- `--force` / `-f` flag required for non-interactive use (returns `invalid_argument` without it)
- Human output: progress to stderr, success message with VM name
- JSON output: `{"ok": true, "data": {"name": ...}}`
- Error codes: `vm_not_found`, `backend_unavailable`, `vm_destroy_failed`, `invalid_argument`
- Digital twin mock backend (`mockDestroyBackend`) recording Destroy calls for assertions
- Tests: registration, flag existence, human output, JSON output, requires --force, short flag -f, VM not found, backend unavailable, generic error, missing name, empty name, property tests (JSON always valid, human contains name, error codes snake_case, no-force never calls backend, force calls backend once, JSON required fields, error JSON format)

### Start command implemented (REQ-002-003)
- `internal/cmd/start.go` with `sd start <name>` command
- Checks VM status before starting: rejects already-running VMs with `vm_already_running`
- Human output: progress to stderr, success message with VM name
- JSON output: `{"ok": true, "data": {"name": ..., "status": "running"}}`
- Error codes: `vm_not_found`, `vm_already_running`, `backend_unavailable`, `vm_start_failed`, `invalid_argument`
- Digital twin mock backend (`mockStartBackend`) recording Start calls for assertions
- Tests: registration, exact args, human output, JSON output, VM not found, already running, backend unavailable, backend get error, generic error, missing name, empty name, stopped VM calls backend, error status VM starts, status check error, property tests (JSON always valid, human contains name, error codes snake_case, running VM never calls backend, stopped VM calls backend once, JSON required fields, error JSON format)

### Stop command implemented (REQ-002-003)
- `internal/cmd/stop.go` with `sd stop <name>` command
- Checks VM status before stopping: already-stopped VMs succeed silently (no backend call)
- Human output: progress to stderr, success message with VM name
- JSON output: `{"ok": true, "data": {"name": ..., "status": "stopped"}}`
- Error codes: `vm_not_found`, `vm_stop_failed`, `backend_unavailable`, `invalid_argument`
- Digital twin mock backend (`mockStopBackend`) recording Stop calls for assertions
- Tests: registration, exact args, human output, JSON output, VM not found, already stopped (silent success + JSON), backend unavailable, backend get error, generic error, missing name, empty name, running VM calls backend, error status VM stops, status check error, property tests (JSON always valid, human contains name, error codes snake_case, stopped VM never calls backend, running VM calls backend once, JSON required fields, error JSON format)

### Status command implemented (REQ-002-003)
- `internal/cmd/status.go` with `sd status [name]` command
- Without name: shows all VMs' status (name + status table in human, array in JSON)
- With name: shows detailed VM info (key-value pairs in human, full VMInfo in JSON)
- Falls back to name+status if List fails but Status succeeds
- JSON output: `{"ok": true, "data": {"name": ..., "status": ..., "backend": ..., ...}}` (single) or `{"ok": true, "data": [{"name": ..., "status": ...}, ...]}` (all)
- Error codes: `vm_not_found`, `backend_unavailable`, `invalid_argument`
- Digital twin mock backend (`mockStatusBackend`) with configurable VMs, statusErr, listErr
- Added "status" to no-config-required list in root.go
- Tests: registration, max args, single VM (human+JSON), all VMs (human+JSON), empty list (human+JSON), VM not found, backend unavailable, backend get error, backend unavailable for all, list error, status error, stopped VM, error status VM, format helpers (full detail, no IP, multiple VMs, empty)
- Property tests: single VM JSON always valid (5 names x 4 statuses), human contains name (5 names), error codes snake_case (2 codes), all-VMs JSON always valid (empty/single/multiple), all-VMs human has header, table contains all names, JSON required fields, dual-mode consistency

Still missing: sync, config, provision, token, audit, security, diff, doctor, logs, completion.

### SSH Config command implemented (REQ-007-006)
- `internal/cmd/ssh_config.go` with `sd ssh-config <vm-name>` command
- Human output: prints SSH config fragment to stdout (suitable for appending to ~/.ssh/config)
- TCP format: Host, HostName, Port, User, IdentityFile, StrictHostKeyChecking yes, UserKnownHostsFile per-VM, ForwardAgent/X11 no, LogLevel ERROR, SendEnv
- VSOCK format: Host, User, IdentityFile, ProxyCommand, StrictHostKeyChecking no, UserKnownHostsFile /dev/null, same security directives
- JSON output: `{"ok": true, "data": {"host": "sd-<name>", "hostname": ..., "port": ..., "user": ..., "identity_file": ..., "proxy_command": ..., "transport": ...}}`
- Error codes: `vm_not_found`, `backend_unavailable`, `ssh_connection_failed`, `invalid_argument`
- Digital twin mock backend (`mockSSHConfigBackend`) with configurable SSH config map and errors
- Unit tests: registration, exact args, human output (TCP/VSOCK), JSON output (TCP/VSOCK), VM not found, backend unavailable, backend get error, SSH config error, empty name, missing name, formatSSHConfigFragment (TCP, VSOCK, default user, default host)
- Property tests: JSON always valid (5 cases: TCP standard, VSOCK standard, high port, low port, proxy), human contains Host line (4 names), error codes snake_case (4 codes), nonexistent VM never succeeds (3 names), JSON required fields, TCP has required directives, VSOCK has ProxyCommand vs TCP has Port, ForwardAgent/X11 always no (2 transports)
- Added "ssh-config" to no-config-required list in root.go
- `internal/cmd/snapshot.go` with `sd snapshot` parent and 4 subcommands:
  - `sd snapshot create <vm> --tag <tag>` -- create named snapshot
  - `sd snapshot list <vm>` -- list snapshots (tab-formatted human, JSON array)
  - `sd snapshot restore <vm> --tag <tag>` -- restore VM to snapshot
  - `sd snapshot delete <vm> --tag <tag>` -- delete a snapshot
- Checks backend implements `Snapshotter` interface; returns `snapshot_not_supported` if not
- Error codes: `vm_not_found`, `snapshot_not_found`, `snapshot_failed`, `snapshot_not_supported`, `backend_unavailable`, `invalid_argument`
- Digital twin mock backends: `mockSnapshotBackend` (with Snapshotter) and `mockNoSnapshotterBackend` (without)
- Tests: registration (with subcommand verification), human output (create/list/restore/delete), JSON output, VM not found, snapshot not found, missing tag, backend error, backend unavailable, empty name, non-snapshotter backend, empty snapshot list, formatSnapshotTable unit tests
- Property tests: JSON always valid (create 5 cases, list 3 cases), human output contains name+tag (3 cases), error codes snake_case (5 codes), create calls backend once (3 VMs), nonexistent VM never succeeds (3 names), list JSON valid (3 VMs), restore/delete JSON required fields, nonexistent VM restore never succeeds (3 names), nonexistent snapshot delete returns correct code (3 tags)

### Exec command implemented (REQ-007-013, REQ-007-014)
- `internal/cmd/exec.go` with `sd exec <vm-name> -- <command> [args...]` command
- Checks VM is running before execution (no auto-start, per spec)
- Human output: passes stdout/stderr through directly, exit code matches remote command
- JSON output: `{"ok": true, "data": {"exit_code": 0, "stdout": "...", "stderr": "..."}}`
- Error codes: `vm_not_found`, `vm_not_running`, `backend_unavailable`, `exec_failed`, `invalid_argument`
- Digital twin mock backend (`mockExecBackend`) recording Exec calls for assertions
- Overrideable `osExit` variable for testing non-zero exit codes without killing test process
- Tests: registration, min args, human output, JSON output, non-zero exit code (JSON), VM not found, VM not running (stopped/error/creating status), backend unavailable, backend get error, exec backend error, ErrVMNotRunning/ErrVMNotFound from backend, empty name, status check error, stderr passed through, command with args, missing VM name, missing command
- Property tests: JSON always valid (5 cases with varying exit codes/outputs), human output matches stdout (5 cases), error codes snake_case (5 codes), running VM calls backend once (3 names), stopped VM never calls backend (3 statuses), JSON required fields, error JSON format

### Create command implemented (REQ-002-003)
- `internal/cmd/create.go` with `sd create <name>` command
- Flags: `--backend`, `--cpus`, `--memory`, `--disk`, `--modules`, `--mount`, `--allow-egress`
- Human output: progress to stderr, success message with VM name
- JSON output: `{"ok": true, "data": {"name": ..., "backend": ..., "cpus": ..., "memory": ..., "disk": ...}}`
- Mount spec parsing: `host:guest[:ro|rw]` format, default read-only
- Config defaults applied when flags not provided (CPUs=4, Memory=8GiB, Disk=100GiB, Image=ubuntu:24.04)
- Error codes: `vm_already_exists`, `backend_unavailable`, `vm_create_failed`, `invalid_argument`
- Digital twin mock backend (`mockCreateBackend`) recording Create calls for assertions
- Tests: registration, flag existence, human output, JSON output, VM already exists, backend unavailable, no-config, missing name, individual flag propagation (cpus/memory/disk/backend/mount), mount read-write, all flags combined, mount parsing unit tests, property tests (JSON always valid, human output contains name, defaults applied, flags override defaults, mount spec parsing invariants, error codes snake_case)
- Proper pflag StringArray accumulation handling via `resetSliceFlag` with `unsafe.Pointer`

### Connect command implemented (REQ-007-001, REQ-007-002, REQ-007-007, REQ-007-008, REQ-007-009, REQ-007-010, REQ-007-012)
- `internal/cmd/connect.go` with `sd connect <vm-name>` command and alias `sd c`
- Auto-starts stopped VMs (REQ-007-002); `--no-start` flag prevents auto-start
- `--session <name>` for named tmux sessions with alphanumeric/hyphen/underscore validation (REQ-007-009)
- `--new-window` creates new window in existing tmux session (REQ-007-010)
- `--no-tmux` for raw SSH session (REQ-007-012); mutually exclusive with `--new-window`
- `--forward <spec>` for port forwarding (REQ-007-007); supports `<host-port>:<guest-port>` and `<bind-addr>:<host-port>:<guest-port>`
- Builds SSH command with ForwardAgent=no, ForwardX11=no, LogLevel=ERROR
- VSOCK transport uses ProxyCommand; TCP uses StrictHostKeyChecking=yes
- JSON output: `{"ok": true, "data": {"name": ..., "status": "running", "session": ..., "tmux": ...}}`
- Actionable error messages for SSH failures (connection refused, auth failed, timeout) per REQ-007-021
- Error codes: `vm_not_found`, `vm_not_running`, `vm_start_failed`, `ssh_connection_failed`, `backend_unavailable`, `invalid_argument`
- Digital twin mock backend (`mockConnectBackend`) recording Start/SSHConfig calls for assertions
- Overrideable `sshRunner` for testing SSH command construction without actual SSH
- Tests: registration, alias (`c`), exact args, flags, human output, JSON output, VM not found, backend unavailable, backend get error, auto-start stopped VM, running VM no start, --no-start stopped VM, auto-start failure, error status VM, --no-tmux/--new-window mutual exclusion, named session, invalid session names, valid session names, --no-tmux, --new-window, empty name, SSH config error, status check error, port forwarding (valid, invalid, with bind addr), auto-start JSON output
- Unit tests: SSH error formatting (connection refused, auth failed, timeout, generic), port forward parsing (2-part, 3-part, multiple, invalid, out of range, negative, empty), buildSSHArgs (default tmux, VSOCK, no tmux, new window, with forwards)
- Property tests: JSON always valid (5 cases), running VM calls SSH runner (5 names), error codes snake_case (6 codes), nonexistent VM never calls SSH runner (3 names), JSON required fields, invalid session names always fail (5 names), VSOCK no StrictHostKeyChecking, TCP has StrictHostKeyChecking, ForwardAgent always no, ForwardX11 always no
- Added "connect" to no-config-required list in root.go

### No embedded provisioning modules
REQ-006-001, REQ-006-014: No modules under internal/provision/modules/.

### No Syncer or Cloner implementations
REQ-003-009, REQ-003-010: Interfaces defined but no backend implements them.

### No iptables/egress implementation
REQ-004-009: Egress control is policy-level only.

### No local DNS resolver
REQ-004-025: DNS filtering resolver not implemented.

### No SSH host key verification
REQ-004-031, REQ-007-004: No code collects/verifies host keys.

### No CI workflow change detection
REQ-004-018: Not implemented.

### No security posture summary
REQ-004-024: Not implemented.

### No git credential cache prevention
REQ-004-030: Not implemented.

### VSOCK port detection in SSHConfig (fixed)
REQ-007-005: Implemented VSOCK/TCP transport detection in SSHConfig.
- Added `Transport` field to `backend.SSHConfig` ("tcp" or "vsock")
- `lima.go` SSHConfig detects VSOCK (Apple Silicon) vs TCP, sets ProxyCommand for VSOCK
- TCP transport retrieves actual SSH port from `limactl list --json` instead of hardcoded 0
- `connect.go` buildSSHArgs skips `-p` flag for VSOCK, uses VM name as SSH target
- mocklimactl updated with VMType field for testing both transports
- Tests: unit tests (VSOCK/TCP/transport-always-set), property tests (VSOCK has ProxyCommand, TCP has port, transport always valid), integration test updated, connect command VSOCK test enhanced
- `isVSOCKTransport` variable allows test override of transport detection
