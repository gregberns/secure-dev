// Package cmd provides tests for the ssh-config command.
// REQ-007-006: SSH Config Print Command
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

// mockSSHConfigBackend is a digital twin of a backend for ssh-config command testing.
// It implements backend.Backend with configurable SSHConfig behavior.
type mockSSHConfigBackend struct {
	name       string
	available  bool
	sshCfgMap  map[string]backend.SSHConfig
	sshCfgErr  error
	statusErr  error
	statusMap  map[string]backend.VMStatus
}

func (m *mockSSHConfigBackend) Name() string { return m.name }
func (m *mockSSHConfigBackend) Available() error {
	if m.available {
		return nil
	}
	return fmt.Errorf("backend not available")
}
func (m *mockSSHConfigBackend) Create(_ context.Context, _ string, _ backend.VMConfig) error {
	return nil
}
func (m *mockSSHConfigBackend) Start(_ context.Context, _ string) error    { return nil }
func (m *mockSSHConfigBackend) Stop(_ context.Context, _ string) error     { return nil }
func (m *mockSSHConfigBackend) Destroy(_ context.Context, _ string) error  { return nil }
func (m *mockSSHConfigBackend) List(_ context.Context) ([]backend.VMInfo, error) {
	return nil, nil
}
func (m *mockSSHConfigBackend) Exec(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
	return backend.ExecResult{}, nil
}
func (m *mockSSHConfigBackend) Status(_ context.Context, name string) (backend.VMStatus, error) {
	if m.statusErr != nil {
		return "", m.statusErr
	}
	if s, ok := m.statusMap[name]; ok {
		return s, nil
	}
	return "", backend.ErrVMNotFound
}
func (m *mockSSHConfigBackend) SSHConfig(_ context.Context, name string) (backend.SSHConfig, error) {
	if m.sshCfgErr != nil {
		return backend.SSHConfig{}, m.sshCfgErr
	}
	if cfg, ok := m.sshCfgMap[name]; ok {
		return cfg, nil
	}
	return backend.SSHConfig{}, backend.ErrVMNotFound
}

// setupSSHConfigTest configures the test environment with a mock backend.
func setupSSHConfigTest(t *testing.T, mb *mockSSHConfigBackend) {
	t.Helper()
	newRootTestEnv(t)

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })
}

// --- Unit tests ---

func TestSSHConfigCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "ssh-config" {
			found = true
			assert.Equal(t, "connection", cmd.GroupID)
			break
		}
	}
	assert.True(t, found, "ssh-config command must be registered")
}

func TestSSHConfigCommand_ExactArgs(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"ssh-config"})
	require.NoError(t, err)
	assert.NotNil(t, cmd.Args, "ssh-config must have an Args validator")

	// Zero args must fail
	err = cmd.Args(cmd, nil)
	assert.Error(t, err, "ssh-config must reject zero args")

	// One arg must pass
	err = cmd.Args(cmd, []string{"myvm"})
	assert.NoError(t, err, "ssh-config must accept one arg")

	// Two args must fail
	err = cmd.Args(cmd, []string{"myvm", "extra"})
	assert.Error(t, err, "ssh-config must reject two args")
}

func TestSSHConfigCommand_HumanOutput_TCP(t *testing.T) {
	mb := &mockSSHConfigBackend{
		name:      "mock",
		available: true,
		sshCfgMap: map[string]backend.SSHConfig{
			"myvm": {
				Host:         "127.0.0.1",
				Port:         60022,
				User:         "dev",
				IdentityFile: "/home/user/.sd/vms/myvm/ssh/id_ed25519",
				Transport:    "tcp",
			},
		},
	}
	setupSSHConfigTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"ssh-config", "myvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	output := buf.String()
	assert.Contains(t, output, "Host sd-myvm")
	assert.Contains(t, output, "HostName 127.0.0.1")
	assert.Contains(t, output, "Port 60022")
	assert.Contains(t, output, "User dev")
	assert.Contains(t, output, "IdentityFile /home/user/.sd/vms/myvm/ssh/id_ed25519")
	assert.Contains(t, output, "StrictHostKeyChecking yes")
	assert.Contains(t, output, "ForwardAgent no")
	assert.Contains(t, output, "ForwardX11 no")
	assert.Contains(t, output, "LogLevel ERROR")
	assert.Contains(t, output, "SendEnv SD_* ANTHROPIC_* GITHUB_* GH_*")
	assert.Contains(t, output, "# Managed by sd. Do not edit manually.")
}

func TestSSHConfigCommand_HumanOutput_VSOCK(t *testing.T) {
	mb := &mockSSHConfigBackend{
		name:      "mock",
		available: true,
		sshCfgMap: map[string]backend.SSHConfig{
			"myvm": {
				User:         "dev",
				IdentityFile: "/home/user/.sd/vms/myvm/ssh/id_ed25519",
				ProxyCommand: "limactl ssh --stdio myvm",
				Transport:    "vsock",
			},
		},
	}
	setupSSHConfigTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"ssh-config", "myvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	output := buf.String()
	assert.Contains(t, output, "Host sd-myvm")
	assert.NotContains(t, output, "HostName", "VSOCK config should not have HostName")
	assert.NotContains(t, output, "Port ", "VSOCK config should not have Port")
	assert.Contains(t, output, "ProxyCommand limactl ssh --stdio myvm")
	assert.Contains(t, output, "StrictHostKeyChecking no")
	assert.Contains(t, output, "UserKnownHostsFile /dev/null")
	assert.Contains(t, output, "User dev")
	assert.Contains(t, output, "ForwardAgent no")
	assert.Contains(t, output, "ForwardX11 no")
}

func TestSSHConfigCommand_JSONOutput(t *testing.T) {
	mb := &mockSSHConfigBackend{
		name:      "mock",
		available: true,
		sshCfgMap: map[string]backend.SSHConfig{
			"myvm": {
				Host:         "127.0.0.1",
				Port:         60022,
				User:         "dev",
				IdentityFile: "/home/user/.sd/vms/myvm/ssh/id_ed25519",
				Transport:    "tcp",
			},
		},
	}
	setupSSHConfigTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "ssh-config", "myvm"})
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
	assert.Equal(t, "sd-myvm", data["host"])
	assert.Equal(t, "127.0.0.1", data["hostname"])
	assert.Equal(t, float64(60022), data["port"])
	assert.Equal(t, "dev", data["user"])
	assert.Equal(t, "/home/user/.sd/vms/myvm/ssh/id_ed25519", data["identity_file"])
	assert.Equal(t, "tcp", data["transport"])
}

func TestSSHConfigCommand_JSONOutput_VSOCK(t *testing.T) {
	mb := &mockSSHConfigBackend{
		name:      "mock",
		available: true,
		sshCfgMap: map[string]backend.SSHConfig{
			"myvm": {
				User:         "dev",
				IdentityFile: "/home/user/.sd/vms/myvm/ssh/id_ed25519",
				ProxyCommand: "limactl ssh --stdio myvm",
				Transport:    "vsock",
			},
		},
	}
	setupSSHConfigTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "ssh-config", "myvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	data := result["data"].(map[string]any)
	assert.Equal(t, "vsock", data["transport"])
	assert.Equal(t, "limactl ssh --stdio myvm", data["proxy_command"])
	assert.Equal(t, "sd-myvm", data["host"])
}

func TestSSHConfigCommand_VMNotFound(t *testing.T) {
	mb := &mockSSHConfigBackend{
		name:      "mock",
		available: true,
		sshCfgMap: map[string]backend.SSHConfig{},
	}
	setupSSHConfigTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"ssh-config", "nonexistent"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
	assert.Contains(t, cliErr.Message, "nonexistent")
}

func TestSSHConfigCommand_BackendUnavailable(t *testing.T) {
	mb := &mockSSHConfigBackend{name: "mock", available: false}
	setupSSHConfigTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"ssh-config", "myvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestSSHConfigCommand_BackendGetError(t *testing.T) {
	newRootTestEnv(t)

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return nil, fmt.Errorf("no such backend")
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	root := RootCmd()
	root.SetArgs([]string{"ssh-config", "myvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestSSHConfigCommand_SSHConfigError(t *testing.T) {
	mb := &mockSSHConfigBackend{
		name:      "mock",
		available: true,
		sshCfgErr: fmt.Errorf("ssh config lookup failed"),
	}
	setupSSHConfigTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"ssh-config", "myvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "ssh_connection_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "ssh config lookup failed")
}

func TestSSHConfigCommand_EmptyName(t *testing.T) {
	mb := &mockSSHConfigBackend{name: "mock", available: true}
	setupSSHConfigTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"ssh-config", ""})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
}

func TestSSHConfigCommand_MissingName(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	root.SetArgs([]string{"ssh-config"})
	err := root.Execute()
	assert.Error(t, err, "ssh-config without args must fail")
}

// --- Unit tests for formatSSHConfigFragment ---

func TestFormatSSHConfigFragment_TCP(t *testing.T) {
	cfg := backend.SSHConfig{
		Host:         "127.0.0.1",
		Port:         60022,
		User:         "dev",
		IdentityFile: "/home/user/.sd/vms/myvm/ssh/id_ed25519",
		Transport:    "tcp",
	}

	result := formatSSHConfigFragment("myvm", cfg)
	assert.Contains(t, result, "Host sd-myvm")
	assert.Contains(t, result, "    HostName 127.0.0.1")
	assert.Contains(t, result, "    Port 60022")
	assert.Contains(t, result, "    User dev")
	assert.Contains(t, result, "    IdentityFile /home/user/.sd/vms/myvm/ssh/id_ed25519")
	assert.Contains(t, result, "    StrictHostKeyChecking yes")
	assert.Contains(t, result, "    UserKnownHostsFile ~/.sd/vms/myvm/ssh/known_hosts")
	assert.Contains(t, result, "    ForwardAgent no")
	assert.Contains(t, result, "    ForwardX11 no")
	assert.Contains(t, result, "    LogLevel ERROR")
	assert.Contains(t, result, "    SendEnv SD_* ANTHROPIC_* GITHUB_* GH_*")
}

func TestFormatSSHConfigFragment_VSOCK(t *testing.T) {
	cfg := backend.SSHConfig{
		User:         "dev",
		IdentityFile: "/home/user/.sd/vms/myvm/ssh/id_ed25519",
		ProxyCommand: "limactl ssh --stdio myvm",
		Transport:    "vsock",
	}

	result := formatSSHConfigFragment("myvm", cfg)
	assert.Contains(t, result, "Host sd-myvm")
	assert.NotContains(t, result, "HostName")
	assert.NotContains(t, result, "Port ")
	assert.Contains(t, result, "    ProxyCommand limactl ssh --stdio myvm")
	assert.Contains(t, result, "    StrictHostKeyChecking no")
	assert.Contains(t, result, "    UserKnownHostsFile /dev/null")
}

func TestFormatSSHConfigFragment_DefaultUser(t *testing.T) {
	cfg := backend.SSHConfig{
		Host:         "127.0.0.1",
		Port:         22,
		User:         "",
		IdentityFile: "/home/user/.sd/vms/test/ssh/id_ed25519",
		Transport:    "tcp",
	}

	result := formatSSHConfigFragment("test", cfg)
	assert.Contains(t, result, "    User dev", "empty user should default to 'dev'")
}

func TestFormatSSHConfigFragment_DefaultHost(t *testing.T) {
	cfg := backend.SSHConfig{
		Host:         "",
		Port:         22,
		User:         "dev",
		IdentityFile: "/home/user/.sd/vms/test/ssh/id_ed25519",
		Transport:    "tcp",
	}

	result := formatSSHConfigFragment("test", cfg)
	assert.Contains(t, result, "    HostName 127.0.0.1", "empty host should default to '127.0.0.1'")
}

// --- Property-based tests ---

// Property: JSON output from ssh-config always contains ok=true and all required fields.
func TestProperty_SSHConfigJSONAlwaysValid(t *testing.T) {
	cases := []struct {
		name     string
		vmName   string
		host     string
		port     int
		user     string
		identity string
		proxy    string
		transport string
	}{
		{"tcp_standard", "myvm", "127.0.0.1", 60022, "dev", "/home/.sd/vms/myvm/ssh/id_ed25519", "", "tcp"},
		{"vsock_standard", "testvm", "", 0, "dev", "/home/.sd/vms/testvm/ssh/id_ed25519", "limactl ssh --stdio testvm", "vsock"},
		{"tcp_high_port", "portvm", "192.168.1.1", 65535, "ubuntu", "/keys/id_ed25519", "", "tcp"},
		{"tcp_low_port", "lowport", "10.0.0.1", 22, "root", "/ssh/key", "", "tcp"},
		{"vsock_proxy", "proxyvm", "", 0, "user", "/home/.sd/vms/proxyvm/ssh/key", "limactl ssh --stdio proxyvm", "vsock"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mb := &mockSSHConfigBackend{
				name:      "mock",
				available: true,
				sshCfgMap: map[string]backend.SSHConfig{
					tc.vmName: {
						Host:         tc.host,
						Port:         tc.port,
						User:         tc.user,
						IdentityFile: tc.identity,
						ProxyCommand: tc.proxy,
						Transport:    tc.transport,
					},
				},
			}
			setupSSHConfigTest(t, mb)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"--json", "ssh-config", tc.vmName})
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
			assert.Equal(t, "sd-"+tc.vmName, data["host"], "host must be sd-<name> for %s", tc.name)
			assert.Equal(t, tc.user, data["user"], "user must match for %s", tc.name)
			assert.Equal(t, tc.identity, data["identity_file"], "identity_file must match for %s", tc.name)
			assert.Equal(t, tc.transport, data["transport"], "transport must match for %s", tc.name)
		})
	}
}

// Property: human output always contains the VM name in the Host line.
func TestProperty_SSHConfigHumanContainsHostLine(t *testing.T) {
	names := []string{"myvm", "test-vm", "vm-123", "production"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := &mockSSHConfigBackend{
				name:      "mock",
				available: true,
				sshCfgMap: map[string]backend.SSHConfig{
					name: {
						Host:         "127.0.0.1",
						Port:         60022,
						User:         "dev",
						IdentityFile: "/home/.sd/vms/" + name + "/ssh/id_ed25519",
						Transport:    "tcp",
					},
				},
			}
			setupSSHConfigTest(t, mb)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"ssh-config", name})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)
			assert.Contains(t, buf.String(), "Host sd-"+name, "human output must contain Host line for %q", name)
		})
	}
}

// Property: error codes are always snake_case.
func TestProperty_SSHConfigErrorCodesSnakeCase(t *testing.T) {
	t.Run("vm_not_found", func(t *testing.T) {
		mb := &mockSSHConfigBackend{
			name:      "mock",
			available: true,
			sshCfgMap: map[string]backend.SSHConfig{},
		}
		setupSSHConfigTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"ssh-config", "ghost"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "vm_not_found", cliErr.Code)
	})

	t.Run("backend_unavailable", func(t *testing.T) {
		mb := &mockSSHConfigBackend{name: "mock", available: false}
		setupSSHConfigTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"ssh-config", "vm1"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "backend_unavailable", cliErr.Code)
	})

	t.Run("invalid_argument_empty_name", func(t *testing.T) {
		mb := &mockSSHConfigBackend{name: "mock", available: true}
		setupSSHConfigTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"ssh-config", ""})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "invalid_argument", cliErr.Code)
	})

	t.Run("ssh_connection_failed", func(t *testing.T) {
		mb := &mockSSHConfigBackend{
			name:      "mock",
			available: true,
			sshCfgErr: fmt.Errorf("internal error"),
		}
		setupSSHConfigTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"ssh-config", "vm1"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "ssh_connection_failed", cliErr.Code)
	})
}

// Property: nonexistent VM never succeeds.
func TestProperty_SSHConfigNonexistentVM_NeverSucceeds(t *testing.T) {
	names := []string{"ghost", "missing", "does-not-exist"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := &mockSSHConfigBackend{
				name:      "mock",
				available: true,
				sshCfgMap: map[string]backend.SSHConfig{},
			}
			setupSSHConfigTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"ssh-config", name})
			err := root.Execute()
			require.Error(t, err)
			cliErr := err.(ui.CLIError)
			assert.Equal(t, "vm_not_found", cliErr.Code)
		})
	}
}

// Property: JSON output always contains required fields.
func TestProperty_SSHConfigJSONRequiredFields(t *testing.T) {
	mb := &mockSSHConfigBackend{
		name:      "mock",
		available: true,
		sshCfgMap: map[string]backend.SSHConfig{
			"myvm": {
				Host:         "127.0.0.1",
				Port:         60022,
				User:         "dev",
				IdentityFile: "/home/.sd/vms/myvm/ssh/id_ed25519",
				Transport:    "tcp",
			},
		},
	}
	setupSSHConfigTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "ssh-config", "myvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	assert.Contains(t, result, "ok", "JSON must have 'ok' field")
	assert.Contains(t, result, "data", "JSON must have 'data' field")

	data := result["data"].(map[string]any)
	assert.Contains(t, data, "host", "data must have 'host' field")
	assert.Contains(t, data, "user", "data must have 'user' field")
	assert.Contains(t, data, "identity_file", "data must have 'identity_file' field")
	assert.Contains(t, data, "transport", "data must have 'transport' field")
}

// Property: human output for TCP always contains key SSH directives.
func TestProperty_SSHConfigTCP_HasRequiredDirectives(t *testing.T) {
	mb := &mockSSHConfigBackend{
		name:      "mock",
		available: true,
		sshCfgMap: map[string]backend.SSHConfig{
			"myvm": {
				Host:         "127.0.0.1",
				Port:         60022,
				User:         "dev",
				IdentityFile: "/home/.sd/vms/myvm/ssh/id_ed25519",
				Transport:    "tcp",
			},
		},
	}
	setupSSHConfigTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"ssh-config", "myvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	output := buf.String()

	requiredDirectives := []string{
		"Host ", "HostName ", "Port ", "User ", "IdentityFile ",
		"StrictHostKeyChecking", "ForwardAgent no", "ForwardX11 no",
		"LogLevel ERROR", "SendEnv",
	}
	for _, directive := range requiredDirectives {
		assert.True(t, strings.Contains(output, directive),
			"TCP config must contain %q", directive)
	}
}

// Property: VSOCK has ProxyCommand, TCP has Port.
func TestProperty_SSHConfig_VSOCKHasProxyCommand_TCPHasPort(t *testing.T) {
	t.Run("vsock_has_proxy_command", func(t *testing.T) {
		mb := &mockSSHConfigBackend{
			name:      "mock",
			available: true,
			sshCfgMap: map[string]backend.SSHConfig{
				"myvm": {
					User:         "dev",
					IdentityFile: "/home/.sd/vms/myvm/ssh/id_ed25519",
					ProxyCommand: "limactl ssh --stdio myvm",
					Transport:    "vsock",
				},
			},
		}
		setupSSHConfigTest(t, mb)

		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		require.NoError(t, err)
		os.Stdout = w

		root := RootCmd()
		root.SetArgs([]string{"ssh-config", "myvm"})
		execErr := root.Execute()

		w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)

		require.NoError(t, execErr)
		assert.Contains(t, buf.String(), "ProxyCommand")
	})

	t.Run("tcp_has_port", func(t *testing.T) {
		mb := &mockSSHConfigBackend{
			name:      "mock",
			available: true,
			sshCfgMap: map[string]backend.SSHConfig{
				"myvm": {
					Host:         "127.0.0.1",
					Port:         60022,
					User:         "dev",
					IdentityFile: "/home/.sd/vms/myvm/ssh/id_ed25519",
					Transport:    "tcp",
				},
			},
		}
		setupSSHConfigTest(t, mb)

		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		require.NoError(t, err)
		os.Stdout = w

		root := RootCmd()
		root.SetArgs([]string{"ssh-config", "myvm"})
		execErr := root.Execute()

		w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)

		require.NoError(t, execErr)
		assert.Contains(t, buf.String(), "Port 60022")
	})
}

// Property: ForwardAgent and ForwardX11 are always "no".
func TestProperty_SSHConfig_ForwardAgentAndX11AlwaysNo(t *testing.T) {
	configs := []struct {
		name    string
		sshCfg  backend.SSHConfig
	}{
		{"tcp", backend.SSHConfig{Host: "127.0.0.1", Port: 22, User: "dev", IdentityFile: "/key", Transport: "tcp"}},
		{"vsock", backend.SSHConfig{User: "dev", IdentityFile: "/key", ProxyCommand: "limactl ssh --stdio vm", Transport: "vsock"}},
	}
	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			output := formatSSHConfigFragment("vm", tc.sshCfg)
			assert.Contains(t, output, "ForwardAgent no", "ForwardAgent must be 'no' for %s", tc.name)
			assert.Contains(t, output, "ForwardX11 no", "ForwardX11 must be 'no' for %s", tc.name)
		})
	}
}
