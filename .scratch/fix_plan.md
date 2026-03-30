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

### Provision command implemented (REQ-006-001, REQ-006-010, REQ-006-015)
- `internal/provision/provisioner.go` with execution engine:
  - `ExecFunc` type abstracting backend.Exec for testability
  - `Provision()` function executing modules in order via backend.Exec
  - Prepends `set -eux -o pipefail` to all scripts (REQ-006-005)
  - System mode scripts run via `sudo bash -c` (REQ-006-005)
  - User mode scripts run via `bash -c` (REQ-006-005)
  - Tracks `ProvisionState` per module (pending/running/completed/failed)
  - Stops on first script failure with module and script index tracking
  - `FormatModuleList()` for human-readable output
  - `ModuleListEntry` for JSON output
- `internal/cmd/provision.go` with `sd provision` command and subcommands:
  - `sd provision <vm>` -- re-provision all modules on a running VM (REQ-006-010)
  - `sd provision <vm> --modules <list>` -- provision specific modules (REQ-006-010, REQ-006-015)
  - `sd provision list` -- list available built-in modules (REQ-006-001)
  - `sd provision list --json` -- list modules in JSON format
  - VM must be running for provisioning (REQ-006-010)
  - Module resolution via existing `ResolveRequested`/`ResolveAll` (REQ-006-004, REQ-006-002)
  - Error codes: `vm_not_found`, `vm_not_running`, `backend_unavailable`, `provision_failed`, `provision_script_failed`, `invalid_argument`
  - Added "provision" to no-config-required list in root.go
  - Injectable `loadBuiltinModules` for digital twin testing
- Tests in `internal/cmd/provision_test.go` (43 tests):
  - Unit tests: registration, flags, missing/empty name, VM not found, VM not running, backend unavailable, status check error, human output, JSON output, --modules flag filtering, unknown module, script failure, exec error
  - Provision list tests: human output, JSON output, rejects extra args, contains all builtin modules
  - Provisioner unit tests: success, system mode uses sudo, user mode no sudo, script preamble, script exit non-zero, exec error, multi-module stops on failure, multi-script in module, second script failure, empty modules, state tracking
  - Format tests: empty module list, modules with deps
  - Property tests: JSON always valid, error codes snake_case, running VM calls backend (3 VMs), stopped VM never calls backend (3 statuses), nonexistent VM never calls backend (3 names), JSON required fields, error JSON serializable, embedded FS loads all modules, preamble always applied
  - Digital twin: `mockProvisionBackend` recording Exec calls with configurable results/errors
  - `captureStdout` helper using `os.Pipe()` for stdout capture

Still missing: security status, diff.

### VM name validation implemented (REQ-001-006)
- `internal/backend/config.go`: Added `ValidateVMName(name string) error` function
  - Validates names against pattern `^[a-z][a-z0-9-]{0,62}$` per spec
  - Checks for empty, max length (63 chars), and format compliance
  - Returns `ErrInvalidVMName` sentinel error wrapped with actionable message
- `internal/backend/errors.go`: Added `ErrInvalidVMName` sentinel error
- Wired into all commands that accept VM names:
  - create, start, stop, destroy, connect, exec, provision
  - ssh-config, sync to/from, snapshot create/list/restore/delete
  - diff, security status, token rotate/revoke/list
  - config egress add/remove/list
- Updated test data: replaced invalid VM names (`vm_with_underscore`, `vm_123`) with valid names (`vm-with-mixed-1`, `vm-123`) in 6 test files
- Tests in `internal/backend/config_test.go` (27 new tests):
  - Unit tests: 9 valid names, 10 invalid names, 3 error message checks
  - Property tests: valid names always pass (100), digit start always fails (100), uppercase always fails (100), too long always fails (100), empty always fails, max length always passes (100), invalid chars always fail (100)

### Token command implemented (REQ-002-008, REQ-004-012, REQ-004-015)
- `internal/cmd/token.go` with `sd token` parent command and 4 subcommands:
  - `sd token github setup` -- show guidance for creating a fine-grained GitHub PAT (REQ-004-012)
  - `sd token rotate <vm>` -- rotate credentials for a VM from host environment variables (REQ-004-015)
  - `sd token revoke <vm>` -- revoke all stored credentials for a VM (REQ-004-015)
  - `sd token list <vm>` -- list configured credential types without revealing values (REQ-004-015)
- `sd token github setup` human output: step-by-step instructions with recommended scopes
- `sd token github setup --json`: outputs `{"ok": true, "data": {"pat_type": "fine-grained", "setup_url": ..., "recommended_scopes": [...], "notes": [...]}}`
- `sd token rotate <vm>` reads GITHUB_TOKEN and ANTHROPIC_API_KEY from host environment
- Validates tokens using security.ValidateToken (warns on classic PAT, unusual key formats)
- Stores credentials in VM config file at `$SD_HOME/vms/<name>/config.yaml` under `env` key
- Preserves non-credential env vars during rotate and revoke
- Error codes: `token_rotate_failed`, `token_revoke_failed`, `token_not_configured`, `invalid_argument`, `config_not_found`
- Injectable `readVMEnvFunc` and `writeVMEnvFunc` for digital twin testing
- Added "token" to no-config-required list in root.go
- Tests in `internal/cmd/token_test.go` (38 tests):
  - Registration: command registered, subcommands exist (github/rotate/revoke/list), github setup registered, no-config required
  - GitHub setup: human output, JSON output, rejects extra args
  - Rotate: human output, JSON output, no credentials error, classic PAT warning, only Anthropic key, preserves existing env, read error, write error, missing VM, empty VM
  - Revoke: human output, JSON output, no credentials, preserves other env, missing VM, empty VM, read error
  - List: human output, JSON output, none configured, all configured, missing VM, empty VM, read error
  - Format helpers: formatCredentialList (normal, empty, all configured), formatGithubSetupGuidance
  - Lifecycle: full rotate -> list -> revoke -> list cycle
  - Property tests: rotate JSON always valid (5 cases), error codes snake_case (4 cases), list JSON required fields, setup JSON required fields, never shows values (2 modes), rotate stores in VM config
  - Validation integration: key format validation

### Logs command implemented (REQ-002-007)
- `internal/cmd/logs.go` with `sd logs [name]` command
- Without name: reads sd's own audit log from `~/.sd/audit.log`
- With name: reads VM console log from `~/.lima/<name>/serial.log`
- `--tail <n>` flag (default 50) shows last N lines
- `--follow` / `-f` flag streams new log entries in real time (human mode only)
- JSON output: `{"ok": true, "data": [{"line": ..., "content": ..., "source": ..., "vm": ...}]}` array
- Human output: raw log lines; "No log entries found." for empty log
- VM existence verified via backend.Status before reading VM logs
- Error codes: `vm_not_found`, `backend_unavailable`, `logs_unavailable`, `invalid_argument`
- Injectable `readLogFileFunc` and `tailFileFunc` for digital twin testing
- Added "logs" to no-config-required list in root.go
- Digital twin mock backend (`mockLogsBackend`) with configurable VM status map
- Tests in `internal/cmd/logs_test.go` (31 tests):
  - Unit tests: registration, max args, flags (--tail, --follow/-f), group ID
  - Audit log tests: human output, JSON output, empty file, no file, no SDHome
  - Tail tests: --tail works, tail larger than file, invalid tail (0, negative)
  - VM log tests: human output, JSON output, VM not found, backend unavailable
  - Follow tests: --follow flag, -f short flag, JSON mode outputs existing only, follow with tail
  - Format tests: empty log human output
  - Unit tests: makeLogEntries (empty, with content, with VM, line numbers)
  - Property tests: JSON always valid (5 name variants), human contains content (5 strings), error codes snake_case (4 codes), VM not found never calls read log (3 names), JSON required fields (5 contents)

### Config command implemented (REQ-005-009 through REQ-005-013)
- `internal/cmd/config.go` with `sd config` parent and 5 subcommands:
  - `sd config get <key>` -- display resolved value with source (REQ-005-009)
  - `sd config set <key> <value>` -- write key-value to user-level config (REQ-005-010)
  - `sd config list` -- display all config values with sources (REQ-005-011)
  - `sd config edit` -- open config file in $EDITOR (REQ-005-012)
  - `sd config validate` -- validate all config files (REQ-005-013)
- Human output: tab-formatted tables for list, per-line status for get/validate
- JSON output: standard `{"ok": true, "data": ...}` envelope for all subcommands
- Value formatting: arrays show `[N entries]`, empty shows `(not set)` in human mode
- Type coercion: JSON arrays for list values, int for numeric keys like `defaults.cpus`
- Source tracking: `built-in default`, `environment variable`, `user-level config`, `project-level config`
- Set rollback: validates after write, rolls back on validation failure
- Edit: creates template file if not exists, rejects in JSON mode, uses $EDITOR/$VISUAL/vi
- Injectable `runEditorCmd` for digital twin testing
- Added `GetKey(key string) (any, error)` and `ProjectDir() string` to config.Loader
- Fixed `Source()` method bug: removed incorrect `viper.IsSet()` check that returned `user-level config` for default values
- Updated root.go no-config-required check to walk parent command chain (handles subcommands)
- Error codes: `invalid_argument`, `config_not_loaded`, `config_query_failed`, `config_set_failed`, `invalid_config`, `config_edit_failed`
- Added "config" to no-config-required list in root.go
- Tests: registration (with subcommand verification), get (human, JSON, defaults, all keys, unknown key, empty key, VM default), set (create, update, JSON, numeric, array, missing args), list (defaults, with config, JSON, extra args rejection), validate (no files, valid, invalid YAML, invalid mount policy, JSON valid, JSON invalid), edit (JSON rejection, opens editor, creates template, uses EDITOR env, editor failure)
- Unit tests: isKnownConfigKey, formatConfigValue, parseConfigValue, resolveEditor, formatConfigTable, formatValidateOutput
- Property tests: get JSON always valid (8 keys), list JSON always valid (3 configs), validate JSON always valid (3 cases), error codes snake_case (3 cases), list contains all known keys, set-get round-trip (3 keys), source tracking (env, user config, default)

### Audit command implemented (REQ-002-008, REQ-004-021, REQ-004-022)
- `internal/cmd/audit.go` with `sd audit [<vm>]` command
- `--since <timestamp>` flag filters events after ISO 8601 timestamp
- `--verify` flag validates hash chain integrity
- Human output: tabular format with timestamp, type, details per entry
- JSON output: `{"ok": true, "data": [...]}` envelope
- Error codes: `audit_chain_broken`, `audit_query_failed`, `invalid_argument`
- Injectable `newAuditLoggerFunc` for digital twin testing
- Bug fix: root.go PersistentPreRunE no longer passes `WithSDHome("")` which was overriding the default SDHome from environment
- Added "audit" to no-config-required list in root.go
- Unit tests: registration, max args, flags, empty log (human+JSON), with entries (human+JSON), filter by VM (human+JSON), since filter (human), verify valid chain (human+JSON), verify broken chain (human+JSON), verify empty log, invalid since format, no SDHome, formatAuditEntries (empty, command, event, event no meta)
- Property tests: JSON always valid (5 VMs), human output contains VM name (3 VMs), error codes snake_case (3 cases), verify valid chain always succeeds (3 VMs), verify tampered chain always fails (4 tampered hashes), JSON required fields, verify JSON required fields, VM filter excludes non-matching (3 VMs)

### Sync command implemented (REQ-007-015, REQ-007-016, REQ-007-017)
- `internal/cmd/sync.go` with `sd sync` parent and 2 subcommands:
  - `sd sync to <vm> <host-path> [<guest-path>]` -- sync files from host to VM
  - `sd sync from <vm> <guest-path> [<host-path>]` -- sync files from VM to host
- Default destination: `~/<basename>` for sync to, `./<basename>` for sync from
- `--diff` flag on `sync from` previews differences without copying
- `--watch` flag on `sync to` (declared but not yet functional -- future work)
- Checks backend implements `Syncer` interface; returns `sync_not_supported` if not
- Added `SyncDiff` method to `backend.Syncer` interface (REQ-007-017)
- Updated mock backends in `registry_test.go` with `SyncDiff` implementations
- Error codes: `vm_not_found`, `vm_not_running`, `backend_unavailable`, `sync_failed`, `sync_not_supported`, `invalid_argument`
- Added "sync" to no-config-required list in root.go
- Digital twin mock backends: `mockSyncBackend` (with Syncer), `mockNoSyncerBackend` (without)
- Tests: registration, arg validation (sync to/from), human output (to/from), JSON output (to/from), default paths, VM not found, VM not running, backend unavailable, non-syncer backend, sync error, empty name, backend get error, status check error, backend ErrVMNotRunning/ErrVMNotFound, diff human output, diff JSON output, diff error, diff VM not found
- Property tests: sync to JSON always valid (5 cases), sync from JSON always valid (3 cases), error codes snake_case (6 codes), running VM calls backend once (3 names), stopped VM never calls backend (3 statuses), nonexistent VM never calls backend (3 names), sync from running VM calls backend once (3 names), diff calls SyncDiff not SyncFrom, JSON required fields (sync to + diff), error JSON format

### Doctor command implemented (REQ-002-007)
- `internal/cmd/doctor.go` with `sd doctor` command
- Checks: required binaries (limactl, ssh, tmux, rsync), configuration validity, VM backend availability
- Human output: pass/fail indicators (checkmark/ballot X) per check
- JSON output: `{"ok": true, "data": [{"name": ..., "status": "pass"|"fail", "message": ...}]}` array
- Works without config file (in "no config required" set)
- Injectable `lookPath` variable for digital twin testing
- Error code: `doctor_check_failed` when any check fails in human mode; JSON mode always succeeds
- `mockDoctorBackend` digital twin implementing `backend.Backend` with configurable `Available()` behavior
- Tests: registration, human output (all pass, some fail), JSON output (all pass, some fail), no config required, rejects extra args, unit tests (checkBinary found/not found, formatDoctorOutput empty/mixed/all pass/all fail), property tests (JSON always valid 3 cases, human contains all check names, error code snake_case)

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

### Embedded provisioning modules implemented (REQ-006-001, REQ-006-014)
- `internal/provision/modules/` with 7 built-in module YAML files:
  - `base.yaml` -- git, curl, build-essential, ca-certificates, jq, tmux, vim
  - `claude-code.yaml` -- Node.js via nvm, Claude Code CLI via npm (with checksums)
  - `docker.yaml` -- Docker Engine with rootless setup
  - `golang.yaml` -- Go 1.23.4 toolchain (with checksums for amd64/arm64)
  - `rust.yaml` -- Rust toolchain via rustup (with checksums)
  - `python.yaml` -- Python 3 with pip and venv
  - `github-cli.yaml` -- GitHub CLI gh 2.67.0 (with checksums for amd64/arm64)
- `internal/provision/modules/embed.go` with `//go:embed *.yaml` directive
- `LoadBuiltinModules()` function loading embedded YAML, parsing, validating, returning in canonical order
- `BuiltinModuleNames` ordered list for consistent listing
- All modules follow REQ-006-003 YAML schema: name, description, depends_on, scripts (system/user), checksums, probe
- All modules have readiness probes (REQ-006-008)
- Non-base modules depend on base (REQ-006-002)
- Download modules (golang, claude-code, rust, github-cli) have SHA-256 checksums (REQ-006-016)
- No curl|sh patterns in any module (REQ-006-016)
- All modules use idempotent guards (command -v / already installed checks) (REQ-006-006)
- Tests in `internal/provision/modules_test.go` (22 tests):
  - Unit tests: all present, count=7, all valid, descriptions, scripts, base no deps, non-base depends on base, base installs all 7 packages, claude-code installs node+claude, golang has checksums, download modules have checksums, no curl|sh, all probes present, probe defaults, idempotent guards, script modes valid, resolve all succeeds, resolve single module (6 subtests)
  - Property tests: always succeeds (100), all names valid (100), resolve any subset (100), no duplicate names (100), scripts not empty (100)
  - Benchmarks: LoadBuiltinModules, ResolveBuiltinModules

### Syncer implementation in Lima backend (REQ-003-010, REQ-007-015, REQ-007-016, REQ-007-017)
- `internal/backend/lima/lima.go` additions: `SyncTo`, `SyncFrom`, `SyncDiff` methods using rsync over SSH
- Injectable `rsyncRun` variable for digital twin testing (defaults to `exec.Command`)
- `buildRsyncArgs` constructs rsync args with `-avz`, optional `--dry-run --itemize-changes`, and `-e` SSH wrapper
- `sshTarget` constructs `user@host:path` (TCP) or `user@localhost:path` (VSOCK) targets
- TCP transport: uses `-p <port>`, `StrictHostKeyChecking=yes`, per-VM `UserKnownHostsFile`
- VSOCK transport: uses `ProxyCommand`, `StrictHostKeyChecking=no`, `UserKnownHostsFile=/dev/null`
- Error mapping: "No such file or directory" and "No route to host" map to `backend.ErrVMNotRunning`
- Context cancellation checked before SSH config lookup
- `internal/backend/lima/sync_test.go` with 30 tests:
  - Unit tests: buildRsyncArgs (TCP, VSOCK, dryRun, normal), sshTarget (TCP, VSOCK)
  - Integration tests: SyncTo success, SyncFrom success, SyncDiff success, stopped VM, nonexistent VM, rsync errors, VMNotRunning error mapping (No route + No such file), SyncDiff VMNotRunning, SyncDiff rsync error, context cancellation (SyncTo + SyncFrom + SyncDiff), Syncer interface assertion, VSOCK transport
  - Property tests: rsync args always start with -avz (100), dry-run always includes flags (100), normal run never includes flags (100), sshTarget always has user+path (100), transport correctness (100), args always end with src/dst (100), stopped VM never calls rsync (100), nonexistent VM never calls rsync (100), cancelled context always fails with "cancelled" (100, all 3 ops)
  - Digital twin: `rsyncRecorder` mock recording calls, injectable via `rsyncRun` override

### Cloner implementation in Lima backend (REQ-003-009)
- `internal/backend/lima/lima.go`: `Clone(ctx, src, dst)` method on `limaBackend` implementing `backend.Cloner`
- Validates source and destination names are non-empty
- Checks source VM exists via Status; checks destination doesn't already exist
- Runs `limactl clone <src> <dst>` through executor
- Maps errors: not found -> ErrVMNotFound, already exists -> ErrVMAlreadyExists
- Context cancellation supported
- `internal/backend/lima/mocklimactl/mocklimactl.go`: Added `cloneVM` command to digital twin
  - Deep copies source VM state with new name, stopped status, fresh timestamps, empty snapshots
- `internal/backend/lima/clone_test.go` (15 tests):
  - Unit tests: success, independent lifecycle (start clone, verify source unaffected, destroy source, verify clone OK), same config inherited, source not found, destination already exists, clone of clone, many clones (5 from same source), empty names (src and dst), context cancelled, running source (clone succeeds, clone is stopped, source still running), Cloner interface assertion
  - Property tests: clone always creates independent VM (100), clone nonexistent source always ErrVMNotFound (100), clone to existing name always ErrVMAlreadyExists (100), cancelled context always fails with "cancelled" (100)

### SSH port forwarding restrictions implemented (REQ-004-026)
- `internal/provision/modules/ssh-hardening.yaml` with system-mode provisioning script:
  - Creates `/etc/ssh/sshd_config.d/99-sd-restrictions.conf` drop-in config
  - Sets `AllowTcpForwarding local` (allows -L only, blocks -R and -D)
  - Sets `GatewayPorts no` (local forward binds only to 127.0.0.1)
  - Sets `PermitTunnel no` (disables tun device forwarding)
  - Sets `X11Forwarding no` (disables X11 forwarding)
  - Reloads sshd (preserves existing sessions, safe for Lima management)
- Added "ssh-hardening" to `BuiltinModuleNames` (positioned after base)
- Depends on base (REQ-006-002)
- Probe verifies `AllowTcpForwarding local` in drop-in config
- Tests in `internal/provision/modules_test.go` (6 new tests):
  - All 4 required sshd directives present
  - Uses drop-in config (not main sshd_config)
  - Reloads (not restarts) sshd
  - System mode (requires root)
  - Depends on base
  - Has probe, no downloads
- Updated test counts from 7 to 8 modules throughout

### Egress firewall and DNS filtering modules implemented (REQ-004-009, REQ-004-025)
- `internal/provision/modules/dns-filter.yaml` with system-mode provisioning script:
  - Installs dnsmasq as local filtering DNS resolver
  - Captures upstream DNS from /etc/resolv.conf before overwriting
  - Configures dnsmasq to listen on 127.0.0.1 only, bind-interfaces, no-resolv
  - Forwards queries only for default egress allowlisted domains (REQ-004-007)
  - Points /etc/resolv.conf to 127.0.0.1 as sole nameserver
  - Locks /etc/resolv.conf with chattr to prevent DHCP overwrites
  - Logs DNS queries to /var/log/dnsmasq.log for audit trail
  - Depends on base module
  - Readiness probe: dig +short @127.0.0.1 api.anthropic.com
- `internal/provision/modules/egress.yaml` with system-mode provisioning script:
  - Installs iptables and iptables-persistent
  - Creates dedicated sd-egress chain for manageability (REQ-004-009)
  - Idempotent: flushes and recreates chain on re-provision
  - Allows loopback traffic
  - Allows established/related connections
  - Allows DNS to local filtering resolver only (127.0.0.1)
  - Allows dnsmasq to reach captured upstream DNS server
  - Blocks DNS to all other resolvers (UDP/TCP port 53)
  - Blocks DNS-over-TLS (port 853) to all destinations
  - Blocks DNS-over-HTTPS to known DoH providers (8.8.8.8, 8.8.4.4, 1.1.1.1, 1.0.0.1, 9.9.9.9, 149.112.112.112)
  - Allows SSH from host (--sport 22)
  - Resolves all default allowlisted domains via local resolver and adds ACCEPT rules
  - Resolves common wildcard subdomains (raw.githubusercontent.com, objects.githubusercontent.com)
  - Default DROP rule for all other outbound traffic (REQ-004-006)
  - Persists rules via iptables-save for reboot survival
  - Depends on base and dns-filter modules
  - Readiness probe: checks sd-egress chain exists via iptables -L
- Added "dns-filter" and "egress" to BuiltinModuleNames (positioned after ssh-hardening, before app modules)
- Updated all module count references from 8 to 10 throughout test files
- Tests in `internal/provision/modules_test.go` (22 new tests):
  - dns-filter: configures dnsmasq, system mode, depends on base, has probe, no downloads, captures upstream DNS
  - egress: configures iptables, system mode, depends on dns-filter, has probe, no downloads, resolves wildcards, persists rules, idempotent chain recreation
  - ResolveSingleModule: added dns-filter and egress test cases

### SSH host key verification implemented (REQ-004-031, REQ-007-004)
- `internal/ssh/ssh.go` additions: `KnownHostsPath`, `CaptureHostKey`, `StoreHostKey`, `ReadHostKey`, `VerifyHostKey`, `RemoveSSHDir`
- Injectable `HostKeyScanner` variable for digital twin testing (defaults to `ssh-keyscan`)
- `hostKeysMatch` compares parsed known_hosts entries by key type + material (host-agnostic)
- `parseHostKeys` extracts key type -> base64 material map from known_hosts data
- Sentinel errors: `ErrHostKeyChanged`, `ErrHostKeyScanFailed`, `ErrNoHostKey`
- `internal/ssh/hostkey_test.go` with 34 tests:
  - Unit tests: KnownHostsPath, CaptureHostKey (stores, scan fails, empty response), StoreHostKey (creates dir, overwrites), ReadHostKey (no key, returns data), VerifyHostKey (matching, changed, no stored, multiple types, missing type, added type), RemoveSSHDir (cleans up, idempotent, cleans known_hosts), parseHostKeys (basic, multiple, comments, empty, blank)
  - Digital twin tests: `digitalTwinHostKeyScanner` mock with configurable keys and failure modes, full Capture+Verify lifecycle, Capture+Destroy lifecycle
  - Integration test: full host key lifecycle (capture -> verify -> change detect -> re-capture -> destroy)
  - Property tests: Capture round-trip (100 cases x 4 key types), same key always valid (100), different key always fails (100), no key always ErrNoHostKey for Read (100) and Verify (100), KnownHostsPath suffix invariant (100), RemoveSSHDir always succeeds (100), Remove then Read fails (100), Store+Read exact round-trip with 1-4 key types (100), hostKeysMatch symmetric (100), hostKeysMatch different material fails (100)

Wired into create (CaptureHostKey), connect (VerifyHostKey), and destroy (RemoveSSHDir) in commit 65130d1.
- `create.go`: captures host key after VM creation for TCP transport
- `connect.go`: verifies host key before SSH for TCP transport, returns `ssh_host_key_changed` on mismatch
- `destroy.go`: cleans up SSH directory on VM destruction
- Tests in create_test.go, connect_test.go, destroy_test.go with digital twin overrides

### Completion command implemented (REQ-002-017)
- `internal/cmd/completion.go` with `sd completion <shell>` command
- Generates shell completion scripts for bash, zsh, and fish using Cobra's built-in generators
- Help text includes installation instructions for each shell (source commands, file paths)
- ValidArgs set to ["bash", "zsh", "fish"] for tab completion of shell names
- Unsupported shells return `invalid_argument` error with snake_case code
- `DisableFlagParsing` set to prevent flag interference with generated scripts
- Custom completion functions registered:
  - `vmNameCompletion`: completes VM names from backend.List()
  - `backendNameCompletion`: completes --backend from backend.List()
  - `snapshotTagCompletion`: completes --tag from Snapshotter.SnapshotList()
  - `moduleNameCompletion`: completes --modules from provision.BuiltinModuleNames
- `requiresVMCompletion` helper detects commands with VM name args via Use string
- Completion registered in "utility" command group
- Added to no-config-required list (already present in root.go)
- Tests in `internal/cmd/completion_test.go` (36 tests):
  - Unit tests: registration, group ID, no-config, exact args, bash/zsh/fish output, bash/zsh/fish contains "sd", unsupported shell (CLIError with invalid_argument code), help contains installation instructions for all 3 shells, valid args (bash/zsh/fish), disable flag parsing
  - Completion function tests: VM names (returns names, backend unavailable returns nil, empty list, list error), backend names, module names (all 7 built-in), snapshot tags (returns tags, no VM arg, non-snapshotter backend)
  - requiresVMCompletion table-driven tests (11 cases)
  - Property tests: bash always produces output, zsh always produces output, fish always produces output, invalid shell always fails, error codes snake_case (6 subtests), bash script contains "sd", module names always complete (NoFileComp + non-empty), VM names match list (4 sets), snapshot tags match list (3 sets), backend unavailable returns NoFileComp
  - Digital twins: mockCompletionBackend (Backend + Snapshotter), mockNonSnapshotterCompletionBackend (Backend only)

### CI Workflow Change Detection implemented (REQ-004-018)
- `internal/cmd/diff.go` with `sd diff <name>` command
- Human output: lists all changed files, flags CI/CD and git hook changes with warnings
- JSON output: `{"ok": true, "data": {"vm": ..., "changes": [...], "warnings": [...]}}`
- Runs `git status --porcelain` and `git rev-parse --git-dir` inside the VM via backend.Exec
- CI/CD patterns detected: `.github/workflows/*`, `.gitlab-ci.yml`, `Jenkinsfile`, `.circleci/*`, `.git/hooks/*`
- Detects non-sample git hooks present in the VM
- Deduplicates hook entries between git status and hook listing
- Error codes: `vm_not_found`, `vm_not_running`, `backend_unavailable`, `diff_failed`, `invalid_argument`
- Digital twin mock backend (`mockDiffBackend`) with configurable exec responses and status map
- Tests in `internal/cmd/diff_test.go` (42 tests):
  - Unit tests: registration, group ID, no-config, exact args, missing name
  - Human output: no changes, with CI changes, no CI changes
  - JSON output: no changes, with CI changes, mixed changes
  - Error handling: VM not found, VM not running, backend unavailable, backend not available, exec error, exec ErrVMNotRunning, empty name, status check error
  - Hooks detection: detects non-sample hooks, no duplication with git status
  - Helper unit tests: parseGitStatus (basic, empty, rename, deleted), isCICDPath (10 patterns), gitStatusToLabel (9 codes), formatDiffOutput (no changes, with changes and warnings, only changes)
  - Property tests: JSON always valid (5 cases), error codes snake_case (6 codes), human contains VM name (5 names), nonexistent VM never calls exec (3 names), JSON required fields, stopped VM never calls exec (3 statuses), all CI/CD patterns detected (5 patterns), CI/CD warning always has category, error codes consistent across modes (3 names x 2 modes)
- Added "diff" to no-config-required list in root.go

### Security posture summary implemented (REQ-004-024)
- `internal/cmd/security.go` with `sd security status <name>` command
- Human output: tab-formatted posture summary with mounts, egress rules, credentials, snapshots, last audit event, and warnings
- JSON output: `{"ok": true, "data": {"vm": ..., "status": ..., "mounts": [...], "egress": {...}, "credentials": [...], "snapshots": {...}, "last_audit": {...}, "warnings": [...]}}`
- Gathers data from: VM status (backend), mounts (VM config), egress rules (config + defaults), credentials (VM env), snapshots (Snapshotter interface), last audit event (audit log)
- Warnings for posture deviations: writable mounts, classic PATs (ghp_), no credentials, no snapshots
- Error codes: `vm_not_found`, `backend_unavailable`, `invalid_argument`
- Digital twin mocks: `mockSecurityBackend` (Backend + Snapshotter), `mockSecurityEnvStore` (preserves key case), `noSnapshotterMinimal` (no Snapshotter)
- Added "security" to no-config-required list in root.go
- Tests in `internal/cmd/security_test.go` (39 tests):
  - Unit tests: registration, no-config, human output, JSON output, VM not found, backend unavailable, empty name, no credentials warning, classic PAT warning, no snapshots warning, egress default domains, last audit event, no audit events, non-snapshotter backend
  - Format tests: no warnings, with warnings, no last audit, with mounts, snapshot error
  - Property tests: JSON always valid (5 cases), error codes snake_case (3 codes), JSON required fields, human contains VM name (5 names), nonexistent VM never succeeds (3 names), warnings include no credentials (3 VMs)

### Git credential cache prevention implemented (REQ-004-030)
- `internal/provision/modules/base.yaml`: Added user-mode script to:
  - Set `git config --global credential.helper ""` to clear default credential helpers
  - Remove `~/.git-credentials` if found (with warning)
- `internal/cmd/doctor.go`: Added `checkGitCredentials()` check that warns if `~/.git-credentials` exists on host
  - Injectable `statPath` variable for digital twin testing
- Tests in `internal/cmd/doctor_test.go` (8 new tests):
  - Unit tests: NoFile (pass), FileExists (fail), StatOverride digital twin, GitCredentialsInOutput (JSON), GitCredentialsFailInJSON
  - Property tests: git_credentials check always valid (100 cases)
  - Updated existing tests: check counts 6->7, git_credentials in all-checks property
- Tests in `internal/provision/modules_test.go` (2 new tests):
  - BaseHasGitCredentialPrevention: verifies user-mode script contains credential.helper and .git-credentials
  - BaseHasMultipleScripts: verifies base has system + user scripts

### VSOCK port detection in SSHConfig (fixed)
REQ-007-005: Implemented VSOCK/TCP transport detection in SSHConfig.
- Added `Transport` field to `backend.SSHConfig` ("tcp" or "vsock")
- `lima.go` SSHConfig detects VSOCK (Apple Silicon) vs TCP, sets ProxyCommand for VSOCK
- TCP transport retrieves actual SSH port from `limactl list --json` instead of hardcoded 0
- `connect.go` buildSSHArgs skips `-p` flag for VSOCK, uses VM name as SSH target
- mocklimactl updated with VMType field for testing both transports
- Tests: unit tests (VSOCK/TCP/transport-always-set), property tests (VSOCK has ProxyCommand, TCP has port, transport always valid), integration test updated, connect command VSOCK test enhanced
- `isVSOCKTransport` variable allows test override of transport detection

### Config egress command implemented (REQ-002-005, REQ-004-008)
- `internal/cmd/config_egress.go` with `sd config egress` parent and 3 subcommands:
  - `sd config egress add <vm> <domain>` -- add a domain to a VM's egress allowlist
  - `sd config egress remove <vm> <domain>` -- remove a domain from a VM's egress allowlist
  - `sd config egress list <vm>` -- list effective egress allowlist (defaults + user-added)
- Human output: confirmation messages for add/remove, tab-formatted table for list (DOMAIN + SOURCE columns)
- JSON output: `{"ok": true, "data": {"vm": ..., "domain": ..., "action": "added"|"removed"}}` for add/remove, `{"ok": true, "data": [{"domain": ..., "source": "default"|"user"}, ...]}` for list
- Domain validation via `security.ValidateEgressDomain` (empty, dot-prefix, consecutive dots, invalid wildcards rejected)
- Default domains cannot be removed (returns `invalid_argument`)
- Duplicate domains rejected (case-insensitive)
- User-added domains stored in VM config file at `$SD_HOME/vms/<name>/config.yaml` under `egress_allowlist` key
- Merges user-added with built-in defaults via `security.BuildEgressList`
- Error codes: `invalid_argument`, `egress_update_failed`, `egress_query_failed`, `config_not_loaded`
- Injectable `readVMEgressFunc` and `writeVMEgressFunc` for digital twin testing
- `egressTestState` digital twin with thread-safe mutex tracking reads/writes
- Tests in `internal/cmd/config_egress_test.go` (29 tests):
  - Registration: command registered with add/remove/list subcommands, no GroupID
  - Add: human output, JSON output, already user-added, already default, invalid domain (empty + dot prefix), empty VM, missing args, write error, multiple sequential adds, case insensitive duplicate detection
  - Remove: human output, JSON output, default domain rejection, not found, empty VM, missing args, write error, removes last clears list, case insensitive match
  - List: human output (table with defaults + user), JSON output (source field per entry), no user domains (only defaults), empty VM, missing args, read error
  - Round-trip: full add -> list -> remove lifecycle
  - Property tests: add JSON always valid (5 cases), remove JSON always valid (3 cases), list JSON always valid (3 cases), error codes snake_case (8 cases), list contains all defaults (3 runs), add calls write once (3 VMs), list JSON required fields (domain + source)

### Credential injection via SendEnv/AcceptEnv implemented (REQ-004-011)
- `internal/cmd/connect.go` additions:
  - `readCredFunc` injectable for loading VM credentials (digital twin support)
  - `isCredentialKey` filters env vars to only credential patterns: SD_*, ANTHROPIC_*, GITHUB_*, GH_*
  - `sshRunner` signature updated to accept `env map[string]string` parameter
  - `defaultSSHRunner` sets env vars on SSH process via `cmd.Env` (never written to disk)
  - `buildSSHArgs` includes `-o SendEnv=SD_* ANTHROPIC_* GITHUB_* GH_*`
  - `runConnect` loads credentials from VM config, filters, and passes to SSH runner
  - Progress message reports number of injected credentials
  - Credential read errors are non-fatal (connection proceeds without credentials)
- `internal/provision/modules/ssh-hardening.yaml`: Added `AcceptEnv SD_* ANTHROPIC_* GITHUB_* GH_*` to sshd config
- Tests in `internal/cmd/connect_test.go` (7 new tests):
  - Unit tests: SendEnv in SSH args (all 4 patterns), isCredentialKey (8 cases), credential injection (loads + filters), no credentials (empty env), read error non-fatal
  - Property tests: SendEnv always present (TCP + VSOCK), credential key filtering (4 include + 7 exclude)
- Updated all existing sshRunner overrides to accept env parameter (11 test sites)
- Updated modules_test.go: AcceptEnv added to required sshd directives list

### REQ-004-019: Snapshot Before Destructive Operations implemented
- `internal/cmd/destroy.go`: Added auto-snapshot before destructive destroy operation
  - `autoSnapshotTag` generates timestamp-based tag: `pre-destroy-YYYYMMDD-HHMMSS`
  - `--no-snapshot` flag to skip auto-snapshot for automation use cases
  - If backend supports `Snapshotter`, creates a safety snapshot before calling Destroy
  - Snapshot failure is non-fatal warning (destroy proceeds regardless)
  - If backend doesn't support `Snapshotter`, destroy proceeds without snapshot
  - `destroyResult` includes optional `snapshot_tag` field in JSON output
  - Human output mentions safety snapshot creation
- `internal/cmd/destroy_test.go` (19 new tests):
  - `mockDestroySnapshotBackend` digital twin implementing Backend + Snapshotter with configurable errors
  - Unit tests: NoSnapshotFlag, AutoSnapshot_Created, AutoSnapshot_HumanOutput, AutoSnapshot_JSONOutput, NoSnapshotFlag_SkipsSnapshot, NoSnapshotFlag_JSONOutput, SnapshotFailure_NonFatal, SnapshotFailure_JSONNoTag, NonSnapshotterBackend_NoAutoSnapshot, NonSnapshotterBackend_JSONNoTag
  - Property tests: SnapshotterAlwaysSnapshots (5 names), NoSnapshotNeverCreates (5 names), AutoSnapshotJSONValid (5 names), NonSnapshotterJSONNoTag (3 names), SnapshotFailureNeverBlocks (3 names), SnapshotTagPrefix (10 iterations)
