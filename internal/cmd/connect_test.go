// Package cmd provides tests for the connect command.
// REQ-007-001: Connect Command
// REQ-007-002: Auto-Start on Connect
// REQ-007-007: Port Forwarding on Connect
// REQ-007-008: tmux as Default Session Manager
// REQ-007-009: Named tmux Sessions
// REQ-007-010: New tmux Window
// REQ-007-012: Raw SSH Without tmux
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/backend"
	"sd/internal/ui"
)

// mockConnectBackend is a digital twin of a backend for connect command testing.
// It implements backend.Backend with configurable behavior and records calls.
type mockConnectBackend struct {
	name      string
	available bool
	// Records all calls
	startCalls   []string
	sshConfigMap map[string]backend.SSHConfig
	// Configurable results
	startErr    error
	statusErr   error
	statusMap   map[string]backend.VMStatus
	sshConfig   backend.SSHConfig
	sshConfigErr error
}

func (m *mockConnectBackend) Name() string { return m.name }
func (m *mockConnectBackend) Available() error {
	if m.available {
		return nil
	}
	return fmt.Errorf("backend not available")
}
func (m *mockConnectBackend) Create(_ context.Context, _ string, _ backend.VMConfig) error {
	return nil
}
func (m *mockConnectBackend) Start(_ context.Context, name string) error {
	if m.startErr != nil {
		return m.startErr
	}
	m.startCalls = append(m.startCalls, name)
	return nil
}
func (m *mockConnectBackend) Stop(_ context.Context, _ string) error    { return nil }
func (m *mockConnectBackend) Destroy(_ context.Context, _ string) error  { return nil }
func (m *mockConnectBackend) List(_ context.Context) ([]backend.VMInfo, error) {
	return nil, nil
}
func (m *mockConnectBackend) Exec(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
	return backend.ExecResult{}, nil
}
func (m *mockConnectBackend) SSHConfig(_ context.Context, name string) (backend.SSHConfig, error) {
	if m.sshConfigErr != nil {
		return backend.SSHConfig{}, m.sshConfigErr
	}
	if m.sshConfigMap != nil {
		if cfg, ok := m.sshConfigMap[name]; ok {
			return cfg, nil
		}
	}
	return m.sshConfig, nil
}
func (m *mockConnectBackend) Status(_ context.Context, name string) (backend.VMStatus, error) {
	if m.statusErr != nil {
		return "", m.statusErr
	}
	if s, ok := m.statusMap[name]; ok {
		return s, nil
	}
	return "", backend.ErrVMNotFound
}

// setupConnectTest configures the test environment with a mock backend.
func setupConnectTest(t *testing.T, mb *mockConnectBackend) {
	t.Helper()
	newRootTestEnv(t)

	// Reset connect subcommand flags (newRootTestEnv only resets root flags)
	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "connect" {
			_ = cmd.Flags().Set("no-start", "false")
			_ = cmd.Flags().Set("session", "")
			_ = cmd.Flags().Set("new-window", "false")
			_ = cmd.Flags().Set("no-tmux", "false")
			resetSliceFlag(cmd, "forward")
			break
		}
	}

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	// Capture SSH runner calls instead of actually running SSH
	origSSHRunner := sshRunner
	sshRunner = func(name string, args []string) error {
		return nil
	}
	t.Cleanup(func() { sshRunner = origSSHRunner })
}

// defaultMockConnectBackend returns a mock with sensible defaults for testing.
func defaultMockConnectBackend() *mockConnectBackend {
	return &mockConnectBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		sshConfig: backend.SSHConfig{
			Host:         "127.0.0.1",
			Port:         60022,
			User:         "dev",
			IdentityFile: "/home/user/.sd/vms/myvm/ssh/id_ed25519",
		},
	}
}

// --- Unit tests ---

func TestConnectCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "connect" {
			found = true
			assert.Equal(t, "connection", cmd.GroupID)
			// REQ-002-009: Alias
			assert.Contains(t, cmd.Aliases, "c", "connect must have 'c' alias")
			break
		}
	}
	assert.True(t, found, "connect command must be registered")
}

func TestConnectCommand_Alias(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"c"})
	require.NoError(t, err)
	assert.Equal(t, "connect", cmd.Name(), "'c' alias must resolve to connect")
}

func TestConnectCommand_ExactArgs(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"connect"})
	require.NoError(t, err)
	assert.NotNil(t, cmd.Args, "connect must have an Args validator")

	// Zero args must fail
	err = cmd.Args(cmd, nil)
	assert.Error(t, err, "connect must reject zero args")

	// One arg must pass
	err = cmd.Args(cmd, []string{"myvm"})
	assert.NoError(t, err, "connect must accept one VM name")

	// Two args must fail
	err = cmd.Args(cmd, []string{"myvm", "extra"})
	assert.Error(t, err, "connect must reject more than one arg")
}

func TestConnectCommand_Flags(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"connect"})
	require.NoError(t, err)

	assert.NotNil(t, cmd.Flags().Lookup("no-start"), "must have --no-start flag")
	assert.NotNil(t, cmd.Flags().Lookup("session"), "must have --session flag")
	assert.NotNil(t, cmd.Flags().Lookup("new-window"), "must have --new-window flag")
	assert.NotNil(t, cmd.Flags().Lookup("no-tmux"), "must have --no-tmux flag")
	assert.NotNil(t, cmd.Flags().Lookup("forward"), "must have --forward flag")
}

func TestConnectCommand_RunningVM_HumanOutput(t *testing.T) {
	mb := defaultMockConnectBackend()
	setupConnectTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"connect", "myvm"})
	err := root.Execute()
	require.NoError(t, err)
}

func TestConnectCommand_RunningVM_JSONOutput(t *testing.T) {
	mb := defaultMockConnectBackend()
	setupConnectTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "connect", "myvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err, "output must be valid JSON: %s", buf.String())

	assert.True(t, result["ok"].(bool))
	data := result["data"].(map[string]any)
	assert.Equal(t, "myvm", data["name"])
	assert.Equal(t, "running", data["status"])
	assert.Equal(t, "sd-myvm", data["session"])
	assert.Equal(t, true, data["tmux"])
}

func TestConnectCommand_VMNotFound(t *testing.T) {
	mb := &mockConnectBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{},
	}
	setupConnectTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"connect", "nonexistent"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
	assert.Contains(t, cliErr.Message, "nonexistent")
	assert.Contains(t, cliErr.Message, "sd list")
}

func TestConnectCommand_BackendUnavailable(t *testing.T) {
	mb := &mockConnectBackend{name: "mock", available: false}
	setupConnectTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"connect", "myvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestConnectCommand_BackendGetError(t *testing.T) {
	newRootTestEnv(t)

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return nil, fmt.Errorf("no such backend")
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	root := RootCmd()
	root.SetArgs([]string{"connect", "myvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

// REQ-007-002: Auto-start
func TestConnectCommand_AutoStart_StoppedVM(t *testing.T) {
	mb := defaultMockConnectBackend()
	mb.statusMap["stoppedvm"] = backend.StatusStopped
	setupConnectTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"connect", "stoppedvm"})
	err := root.Execute()

	require.NoError(t, err)
	require.Len(t, mb.startCalls, 1, "connect must auto-start stopped VM")
	assert.Equal(t, "stoppedvm", mb.startCalls[0])
}

// REQ-007-002: Already running, no start attempted
func TestConnectCommand_RunningVM_NoStart(t *testing.T) {
	mb := defaultMockConnectBackend()
	setupConnectTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"connect", "myvm"})
	err := root.Execute()

	require.NoError(t, err)
	assert.Empty(t, mb.startCalls, "connect must not start an already-running VM")
}

// REQ-007-002: --no-start with stopped VM
func TestConnectCommand_NoStartFlag_StoppedVM(t *testing.T) {
	mb := defaultMockConnectBackend()
	mb.statusMap["stoppedvm"] = backend.StatusStopped
	setupConnectTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"connect", "stoppedvm", "--no-start"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_running", cliErr.Code)
	assert.Contains(t, cliErr.Message, "stopped")
	assert.Contains(t, cliErr.Message, "--no-start")
	assert.Empty(t, mb.startCalls, "must not call Start with --no-start")
}

// REQ-007-002: Start failure
func TestConnectCommand_AutoStart_Failure(t *testing.T) {
	mb := defaultMockConnectBackend()
	mb.statusMap["brokenvm"] = backend.StatusStopped
	mb.startErr = fmt.Errorf("hypervisor error")
	setupConnectTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"connect", "brokenvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_start_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "brokenvm")
}

// REQ-007-002: VM in error status
func TestConnectCommand_ErrorStatusVM(t *testing.T) {
	mb := defaultMockConnectBackend()
	mb.statusMap["errvm"] = backend.StatusError
	setupConnectTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"connect", "errvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_running", cliErr.Code)
	assert.Contains(t, cliErr.Message, "errvm")
}

// REQ-007-012: --no-tmux and --new-window are mutually exclusive
func TestConnectCommand_NoTmux_NewWindow_MutuallyExclusive(t *testing.T) {
	mb := defaultMockConnectBackend()
	setupConnectTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"connect", "myvm", "--no-tmux", "--new-window"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
	assert.Contains(t, cliErr.Message, "mutually exclusive")
}

// REQ-007-009: Named session
func TestConnectCommand_NamedSession(t *testing.T) {
	mb := defaultMockConnectBackend()
	setupConnectTest(t, mb)

	var capturedArgs []string
	origSSHRunner := sshRunner
	sshRunner = func(name string, args []string) error {
		capturedArgs = args
		return nil
	}
	defer func() { sshRunner = origSSHRunner }()

	root := RootCmd()
	root.SetArgs([]string{"connect", "myvm", "--session", "work"})
	err := root.Execute()

	require.NoError(t, err)
	// Verify tmux command uses the named session
	found := false
	for i, arg := range capturedArgs {
		if arg == "tmux" && i+4 < len(capturedArgs) && capturedArgs[i+1] == "new-session" {
			found = true
			assert.Equal(t, "work", capturedArgs[i+4])
			break
		}
	}
	assert.True(t, found, "SSH args must contain tmux new-session with named session")
}

// REQ-007-009: Invalid session name
func TestConnectCommand_InvalidSessionName(t *testing.T) {
	mb := defaultMockConnectBackend()
	setupConnectTest(t, mb)

	for _, name := range []string{"my session", "session!", "sess@ion", "session.name"} {
		t.Run(name, func(t *testing.T) {
			root := RootCmd()
			root.SetArgs([]string{"connect", "myvm", "--session", name})
			err := root.Execute()

			require.Error(t, err)
			cliErr, ok := err.(ui.CLIError)
			require.True(t, ok)
			assert.Equal(t, "invalid_argument", cliErr.Code)
			assert.Contains(t, cliErr.Message, "invalid")
		})
	}
}

// REQ-007-009: Valid session names
func TestConnectCommand_ValidSessionNames(t *testing.T) {
	mb := defaultMockConnectBackend()
	setupConnectTest(t, mb)

	for _, name := range []string{"work", "my-session", "session_1", "ABC123"} {
		t.Run(name, func(t *testing.T) {
			root := RootCmd()
			root.SetArgs([]string{"--json", "connect", "myvm", "--session", name})
			err := root.Execute()
			require.NoError(t, err)
		})
	}
}

// REQ-007-012: --no-tmux
func TestConnectCommand_NoTmux(t *testing.T) {
	mb := defaultMockConnectBackend()
	setupConnectTest(t, mb)

	var capturedArgs []string
	origSSHRunner := sshRunner
	sshRunner = func(name string, args []string) error {
		capturedArgs = args
		return nil
	}
	defer func() { sshRunner = origSSHRunner }()

	root := RootCmd()
	root.SetArgs([]string{"connect", "myvm", "--no-tmux"})
	err := root.Execute()

	require.NoError(t, err)
	// Verify no tmux command in args
	for _, arg := range capturedArgs {
		assert.NotEqual(t, "tmux", arg, "no-tmux mode must not include tmux in SSH args")
	}
}

// REQ-007-010: --new-window
func TestConnectCommand_NewWindow(t *testing.T) {
	mb := defaultMockConnectBackend()
	setupConnectTest(t, mb)

	var capturedArgs []string
	origSSHRunner := sshRunner
	sshRunner = func(name string, args []string) error {
		capturedArgs = args
		return nil
	}
	defer func() { sshRunner = origSSHRunner }()

	root := RootCmd()
	root.SetArgs([]string{"connect", "myvm", "--new-window"})
	err := root.Execute()

	require.NoError(t, err)
	// Verify new-window command in args
	found := false
	for _, arg := range capturedArgs {
		if arg == "new-window" {
			found = true
			break
		}
	}
	assert.True(t, found, "new-window mode must include 'new-window' in SSH args")
}

func TestConnectCommand_EmptyName(t *testing.T) {
	mb := defaultMockConnectBackend()
	setupConnectTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"connect", ""})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
}

func TestConnectCommand_SSHConfigError(t *testing.T) {
	mb := defaultMockConnectBackend()
	mb.sshConfigErr = fmt.Errorf("ssh config unavailable")
	setupConnectTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"connect", "myvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "ssh_connection_failed", cliErr.Code)
}

func TestConnectCommand_StatusCheckError(t *testing.T) {
	mb := &mockConnectBackend{
		name:      "mock",
		available: true,
		statusErr: fmt.Errorf("connection refused"),
	}
	setupConnectTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"connect", "myvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "ssh_connection_failed", cliErr.Code)
}

// REQ-007-007: Port forwarding
func TestConnectCommand_PortForwarding(t *testing.T) {
	mb := defaultMockConnectBackend()
	setupConnectTest(t, mb)

	var capturedArgs []string
	origSSHRunner := sshRunner
	sshRunner = func(name string, args []string) error {
		capturedArgs = args
		return nil
	}
	defer func() { sshRunner = origSSHRunner }()

	root := RootCmd()
	root.SetArgs([]string{"connect", "myvm", "--forward", "8080:8080"})
	err := root.Execute()

	require.NoError(t, err)
	// Verify -L flag present
	found := false
	for i, arg := range capturedArgs {
		if arg == "-L" && i+1 < len(capturedArgs) {
			assert.Equal(t, "127.0.0.1:8080:localhost:8080", capturedArgs[i+1])
			found = true
			break
		}
	}
	assert.True(t, found, "SSH args must include port forwarding -L flag")
}

func TestConnectCommand_PortForwarding_InvalidFormat(t *testing.T) {
	mb := defaultMockConnectBackend()
	setupConnectTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"connect", "myvm", "--forward", "bad"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
	assert.Contains(t, cliErr.Message, "port forwarding")
}

func TestConnectCommand_PortForwarding_WithBindAddr(t *testing.T) {
	mb := defaultMockConnectBackend()
	setupConnectTest(t, mb)

	var capturedArgs []string
	origSSHRunner := sshRunner
	sshRunner = func(name string, args []string) error {
		capturedArgs = args
		return nil
	}
	defer func() { sshRunner = origSSHRunner }()

	root := RootCmd()
	root.SetArgs([]string{"connect", "myvm", "--forward", "0.0.0.0:8080:80"})
	err := root.Execute()

	require.NoError(t, err)
	found := false
	for i, arg := range capturedArgs {
		if arg == "-L" && i+1 < len(capturedArgs) {
			assert.Equal(t, "0.0.0.0:8080:localhost:80", capturedArgs[i+1])
			found = true
			break
		}
	}
	assert.True(t, found)
}

// REQ-007-002: Auto-start with JSON output
func TestConnectCommand_AutoStart_JSONOutput(t *testing.T) {
	mb := defaultMockConnectBackend()
	mb.statusMap["stoppedvm"] = backend.StatusStopped
	setupConnectTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "connect", "stoppedvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err, "output must be valid JSON: %s", buf.String())

	assert.True(t, result["ok"].(bool))
	data := result["data"].(map[string]any)
	assert.Equal(t, "stoppedvm", data["name"])
	require.Len(t, mb.startCalls, 1)
}

// --- SSH error formatting tests (REQ-007-021) ---

func TestFormatSSHError_ConnectionRefused(t *testing.T) {
	cfg := backend.SSHConfig{Host: "127.0.0.1", Port: 60022}
	msg := formatSSHError(fmt.Errorf("connection refused"), "myvm", cfg)
	assert.Contains(t, strings.ToLower(msg), "connection refused")
	assert.Contains(t, msg, "myvm")
	assert.Contains(t, msg, "sd status")
}

func TestFormatSSHError_AuthFailed(t *testing.T) {
	cfg := backend.SSHConfig{IdentityFile: "/keys/id_ed25519"}
	msg := formatSSHError(fmt.Errorf("permission denied"), "myvm", cfg)
	assert.Contains(t, msg, "Authentication failed")
	assert.Contains(t, msg, "myvm")
	assert.Contains(t, msg, "/keys/id_ed25519")
}

func TestFormatSSHError_Timeout(t *testing.T) {
	cfg := backend.SSHConfig{Host: "127.0.0.1", Port: 60022}
	msg := formatSSHError(fmt.Errorf("connection timed out"), "myvm", cfg)
	assert.Contains(t, msg, "timed out")
	assert.Contains(t, msg, "myvm")
}

func TestFormatSSHError_Generic(t *testing.T) {
	cfg := backend.SSHConfig{Host: "127.0.0.1", Port: 60022}
	msg := formatSSHError(fmt.Errorf("unknown error"), "myvm", cfg)
	assert.Contains(t, msg, "Failed to connect")
	assert.Contains(t, msg, "myvm")
}

// --- Port forward parsing tests (REQ-007-007) ---

func TestParsePortForwards_ValidTwoPart(t *testing.T) {
	fwd, err := parsePortForwards([]string{"8080:80"})
	require.NoError(t, err)
	require.Len(t, fwd, 1)
	assert.Equal(t, "127.0.0.1", fwd[0].BindAddr)
	assert.Equal(t, 8080, fwd[0].HostPort)
	assert.Equal(t, 80, fwd[0].GuestPort)
}

func TestParsePortForwards_ValidThreePart(t *testing.T) {
	fwd, err := parsePortForwards([]string{"0.0.0.0:8080:80"})
	require.NoError(t, err)
	require.Len(t, fwd, 1)
	assert.Equal(t, "0.0.0.0", fwd[0].BindAddr)
	assert.Equal(t, 8080, fwd[0].HostPort)
	assert.Equal(t, 80, fwd[0].GuestPort)
}

func TestParsePortForwards_Multiple(t *testing.T) {
	fwd, err := parsePortForwards([]string{"8080:80", "9090:9090"})
	require.NoError(t, err)
	require.Len(t, fwd, 2)
}

func TestParsePortForwards_InvalidFormat(t *testing.T) {
	_, err := parsePortForwards([]string{"bad"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid port forwarding")
}

func TestParsePortForwards_TooManyColons(t *testing.T) {
	_, err := parsePortForwards([]string{"a:b:c:d"})
	require.Error(t, err)
}

func TestParsePortForwards_InvalidPort(t *testing.T) {
	_, err := parsePortForwards([]string{"abc:80"})
	require.Error(t, err)
}

func TestParsePortForwards_PortOutOfRange(t *testing.T) {
	_, err := parsePortForwards([]string{"99999:80"})
	require.Error(t, err)
}

func TestParsePortForwards_NegativePort(t *testing.T) {
	_, err := parsePortForwards([]string{"0:80"})
	require.Error(t, err)
}

func TestParsePortForwards_Empty(t *testing.T) {
	fwd, err := parsePortForwards(nil)
	require.NoError(t, err)
	assert.Empty(t, fwd)
}

// --- buildSSHArgs tests ---

func TestBuildSSHArgs_DefaultTmux(t *testing.T) {
	cfg := backend.SSHConfig{
		Host:         "127.0.0.1",
		Port:         60022,
		User:         "dev",
		IdentityFile: "/keys/id_ed25519",
	}
	args := buildSSHArgs(cfg, "myvm", "sd-myvm", false, false, nil)

	assert.Contains(t, args, "-i")
	assert.Contains(t, args, "/keys/id_ed25519")
	assert.Contains(t, args, "-o")
	assert.Contains(t, args, "StrictHostKeyChecking=yes")
	assert.Contains(t, args, "-o")
	assert.Contains(t, args, "ForwardAgent=no")
	assert.Contains(t, args, "tmux")
	assert.Contains(t, args, "new-session")
	assert.Contains(t, args, "-A")
	assert.Contains(t, args, "-s")
	assert.Contains(t, args, "sd-myvm")
}

func TestBuildSSHArgs_VSOCKTransport(t *testing.T) {
	cfg := backend.SSHConfig{
		User:         "dev",
		Port:         22,
		IdentityFile: "/keys/id_ed25519",
		ProxyCommand: "limactl ssh --stdio myvm",
	}
	args := buildSSHArgs(cfg, "myvm", "sd-myvm", false, false, nil)

	assert.Contains(t, args, "ProxyCommand=limactl ssh --stdio myvm")
	assert.NotContains(t, args, "StrictHostKeyChecking=yes")
	// REQ-007-005: VSOCK should not include -p flag; target uses VM name
	assert.NotContains(t, args, "-p")
	assert.Contains(t, args, "dev@myvm", "VSOCK target should use VM name")
}

func TestBuildSSHArgs_NoTmux(t *testing.T) {
	cfg := backend.SSHConfig{
		Host: "127.0.0.1",
		Port: 22,
		User: "dev",
	}
	args := buildSSHArgs(cfg, "myvm", "sd-myvm", true, false, nil)

	assert.NotContains(t, args, "tmux")
}

func TestBuildSSHArgs_NewWindow(t *testing.T) {
	cfg := backend.SSHConfig{
		Host: "127.0.0.1",
		Port: 22,
		User: "dev",
	}
	args := buildSSHArgs(cfg, "myvm", "sd-myvm", false, true, nil)

	assert.Contains(t, args, "new-window")
}

func TestBuildSSHArgs_WithForwards(t *testing.T) {
	cfg := backend.SSHConfig{
		Host: "127.0.0.1",
		Port: 22,
		User: "dev",
	}
	forwards := []portForward{
		{BindAddr: "127.0.0.1", HostPort: 8080, GuestPort: 80},
		{BindAddr: "0.0.0.0", HostPort: 9090, GuestPort: 9090},
	}
	args := buildSSHArgs(cfg, "myvm", "sd-myvm", false, false, forwards)

	found8080 := false
	found9090 := false
	for i, arg := range args {
		if arg == "-L" && i+1 < len(args) {
			if args[i+1] == "127.0.0.1:8080:localhost:80" {
				found8080 = true
			}
			if args[i+1] == "0.0.0.0:9090:localhost:9090" {
				found9090 = true
			}
		}
	}
	assert.True(t, found8080, "must include forward for 8080:80")
	assert.True(t, found9090, "must include forward for 9090:9090")
}

// --- Property-based tests ---

// Property: JSON output from connect always contains ok=true, data.name, data.status, data.session, data.tmux.
func TestProperty_ConnectJSONAlwaysValid(t *testing.T) {
	cases := []struct {
		name     string
		vmName   string
		session  string
		noTmux   bool
	}{
		{"default_session", "vm1", "", false},
		{"named_session", "vm2", "work", false},
		{"another_session", "vm3", "debug-2", false},
		{"no_tmux", "vm4", "", true},
		{"simple", "vm5", "test_session", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mb := defaultMockConnectBackend()
			mb.statusMap[tc.vmName] = backend.StatusRunning
			setupConnectTest(t, mb)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			args := []string{"--json", "connect", tc.vmName}
			if tc.session != "" {
				args = append(args, "--session", tc.session)
			}
			if tc.noTmux {
				args = append(args, "--no-tmux")
			}

			root := RootCmd()
			root.SetArgs(args)
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)

			var result map[string]any
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err, "JSON must parse for %s: %s", tc.name, buf.String())
			assert.True(t, result["ok"].(bool), "ok must be true for %s", tc.name)

			data := result["data"].(map[string]any)
			assert.Equal(t, tc.vmName, data["name"], "name must match for %s", tc.name)
			assert.Equal(t, "running", data["status"], "status must be running for %s", tc.name)
			assert.Contains(t, data, "session", "must have session for %s", tc.name)
			assert.Contains(t, data, "tmux", "must have tmux for %s", tc.name)
		})
	}
}

// Property: human output always calls SSH runner for running VMs.
func TestProperty_ConnectRunningVM_CallsSSHRunner(t *testing.T) {
	names := []string{"vm1", "vm2", "test-vm", "myproject", "dev"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := defaultMockConnectBackend()
			mb.statusMap[name] = backend.StatusRunning
			setupConnectTest(t, mb)

			sshCalled := false
			origSSHRunner := sshRunner
			sshRunner = func(cmdName string, args []string) error {
				sshCalled = true
				return nil
			}
			defer func() { sshRunner = origSSHRunner }()

			root := RootCmd()
			root.SetArgs([]string{"connect", name})
			err := root.Execute()

			require.NoError(t, err)
			assert.True(t, sshCalled, "SSH runner must be called for running VM %q", name)
		})
	}
}

// Property: error codes are always snake_case.
func TestProperty_ConnectErrorCodesSnakeCase(t *testing.T) {
	t.Run("vm_not_found", func(t *testing.T) {
		mb := &mockConnectBackend{
			name:      "mock",
			available: true,
			statusMap: map[string]backend.VMStatus{},
		}
		setupConnectTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"connect", "ghost"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "vm_not_found", cliErr.Code)
	})

	t.Run("backend_unavailable", func(t *testing.T) {
		mb := &mockConnectBackend{name: "mock", available: false}
		setupConnectTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"connect", "vm1"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "backend_unavailable", cliErr.Code)
	})

	t.Run("invalid_argument_mutually_exclusive", func(t *testing.T) {
		mb := defaultMockConnectBackend()
		setupConnectTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"connect", "vm1", "--no-tmux", "--new-window"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "invalid_argument", cliErr.Code)
	})

	t.Run("vm_not_running_no_start", func(t *testing.T) {
		mb := defaultMockConnectBackend()
		mb.statusMap["stopped"] = backend.StatusStopped
		setupConnectTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"connect", "stopped", "--no-start"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "vm_not_running", cliErr.Code)
	})

	t.Run("vm_start_failed", func(t *testing.T) {
		mb := defaultMockConnectBackend()
		mb.statusMap["broken"] = backend.StatusStopped
		mb.startErr = fmt.Errorf("fail")
		setupConnectTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"connect", "broken"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "vm_start_failed", cliErr.Code)
	})

	t.Run("ssh_connection_failed", func(t *testing.T) {
		mb := defaultMockConnectBackend()
		mb.sshConfigErr = fmt.Errorf("no config")
		setupConnectTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"connect", "myvm"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "ssh_connection_failed", cliErr.Code)
	})
}

// Property: nonexistent VM never calls SSH runner.
func TestProperty_ConnectNonexistentVM_NeverCallsSSHRunner(t *testing.T) {
	names := []string{"ghost", "missing", "does-not-exist"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := &mockConnectBackend{
				name:      "mock",
				available: true,
				statusMap: map[string]backend.VMStatus{},
			}
			sshCalled := false
			origSSHRunner := sshRunner
			sshRunner = func(cmdName string, args []string) error {
				sshCalled = true
				return nil
			}
			defer func() { sshRunner = origSSHRunner }()
			setupConnectTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"connect", name})
			err := root.Execute()

			require.Error(t, err)
			assert.False(t, sshCalled, "SSH runner must not be called for nonexistent VM %q", name)
		})
	}
}

// Property: JSON required fields always present.
func TestProperty_ConnectJSONRequiredFields(t *testing.T) {
	mb := defaultMockConnectBackend()
	setupConnectTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "connect", "myvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	assert.Contains(t, result, "ok")
	assert.Contains(t, result, "data")

	data := result["data"].(map[string]any)
	assert.Contains(t, data, "name")
	assert.Contains(t, data, "status")
	assert.Contains(t, data, "session")
	assert.Contains(t, data, "tmux")
}

// Property: session names with special characters always produce invalid_argument.
func TestProperty_ConnectInvalidSession_AlwaysFails(t *testing.T) {
	invalidNames := []string{"has space", "has.dot", "has@at", "has/slash", "has!bang"}
	for _, name := range invalidNames {
		t.Run(name, func(t *testing.T) {
			mb := defaultMockConnectBackend()
			setupConnectTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"connect", "myvm", "--session", name})
			err := root.Execute()

			require.Error(t, err)
			cliErr, ok := err.(ui.CLIError)
			require.True(t, ok)
			assert.Equal(t, "invalid_argument", cliErr.Code, "session %q must produce invalid_argument", name)
		})
	}
}

// Property: VSOCK transport uses ProxyCommand, not StrictHostKeyChecking.
func TestProperty_ConnectVSOCK_NoStrictHostKeyChecking(t *testing.T) {
	cfg := backend.SSHConfig{
		User:         "dev",
		Port:         22,
		IdentityFile: "/keys/id_ed25519",
		ProxyCommand: "limactl ssh --stdio testvm",
	}
	args := buildSSHArgs(cfg, "testvm", "sd-testvm", false, false, nil)

	hasProxy := false
	hasStrictHostKey := false
	for i, arg := range args {
		if strings.HasPrefix(arg, "ProxyCommand=") {
			hasProxy = true
		}
		if arg == "StrictHostKeyChecking=yes" {
			hasStrictHostKey = true
		}
		_ = i
	}
	assert.True(t, hasProxy, "VSOCK transport must include ProxyCommand")
	assert.False(t, hasStrictHostKey, "VSOCK transport must not include StrictHostKeyChecking=yes")
}

// Property: TCP transport uses StrictHostKeyChecking.
func TestProperty_ConnectTCP_StrictHostKeyChecking(t *testing.T) {
	cfg := backend.SSHConfig{
		Host:         "127.0.0.1",
		Port:         60022,
		User:         "dev",
		IdentityFile: "/keys/id_ed25519",
	}
	args := buildSSHArgs(cfg, "testvm", "sd-testvm", false, false, nil)

	hasStrictHostKey := false
	for _, arg := range args {
		if arg == "StrictHostKeyChecking=yes" {
			hasStrictHostKey = true
		}
	}
	assert.True(t, hasStrictHostKey, "TCP transport must include StrictHostKeyChecking=yes")
}

// Property: ForwardAgent is always no.
func TestProperty_Connect_AlwaysForwardAgentNo(t *testing.T) {
	cfg := backend.SSHConfig{
		Host:         "127.0.0.1",
		Port:         60022,
		User:         "dev",
		IdentityFile: "/keys/id_ed25519",
	}
	args := buildSSHArgs(cfg, "testvm", "sd-testvm", false, false, nil)

	found := false
	for i, arg := range args {
		if arg == "ForwardAgent=no" {
			found = true
		}
		_ = i
	}
	assert.True(t, found, "SSH args must include ForwardAgent=no")
}

// Property: ForwardX11 is always no.
func TestProperty_Connect_AlwaysForwardX11No(t *testing.T) {
	cfg := backend.SSHConfig{
		Host:         "127.0.0.1",
		Port:         60022,
		User:         "dev",
		IdentityFile: "/keys/id_ed25519",
	}
	args := buildSSHArgs(cfg, "testvm", "sd-testvm", false, false, nil)

	found := false
	for _, arg := range args {
		if arg == "ForwardX11=no" {
			found = true
		}
	}
	assert.True(t, found, "SSH args must include ForwardX11=no")
}
