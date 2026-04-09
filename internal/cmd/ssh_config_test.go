// Package cmd provides tests for the ssh-config command.
// REQ-007-006: SSH Config Print Command
// NOTE: Tests use global getBackendFunc — do not use t.Parallel().
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/backend"
	"sd/internal/backend/memory"
	"sd/internal/ui"
)

// memPortFromName computes the deterministic port the memory backend returns for a VM name.
func memPortFromName(name string) int {
	h := fnv.New32a()
	h.Write([]byte(name))
	return 10000 + int(h.Sum32()%50000)
}

// setupSSHConfigMemoryTest creates a memory backend with the given running VMs and injects it.
func setupSSHConfigMemoryTest(t *testing.T, vmNames []string) *memory.Backend {
	t.Helper()
	newRootTestEnv(t)

	mb := memory.New()
	for _, name := range vmNames {
		require.NoError(t, mb.Create(context.Background(), name, backend.VMConfig{}))
	}

	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend; mb.Reset() })
	return mb
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
	setupSSHConfigMemoryTest(t, []string{"myvm"})

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
	assert.Contains(t, output, fmt.Sprintf("Port %d", memPortFromName("myvm")))
	assert.Contains(t, output, "User ubuntu")
	assert.Contains(t, output, "IdentityFile /dev/null")
	assert.Contains(t, output, "StrictHostKeyChecking yes")
	assert.Contains(t, output, "ForwardAgent no")
	assert.Contains(t, output, "ForwardX11 no")
	assert.Contains(t, output, "LogLevel ERROR")
	assert.Contains(t, output, "SendEnv SD_* ANTHROPIC_* GITHUB_* GH_*")
	assert.Contains(t, output, "# Managed by sd. Do not edit manually.")
}

func TestSSHConfigCommand_HumanOutput_VSOCK(t *testing.T) {
	// NOTE: VSOCK transport is not supported by the memory backend.
	// VSOCK formatting is tested by TestFormatSSHConfigFragment_VSOCK.
	// This test verifies the command flow with a second VM using TCP transport.
	setupSSHConfigMemoryTest(t, []string{"testvm"})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"ssh-config", "testvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	output := buf.String()
	assert.Contains(t, output, "Host sd-testvm")
	assert.Contains(t, output, "HostName 127.0.0.1")
	assert.Contains(t, output, fmt.Sprintf("Port %d", memPortFromName("testvm")))
	assert.Contains(t, output, "User ubuntu")
	assert.Contains(t, output, "ForwardAgent no")
	assert.Contains(t, output, "ForwardX11 no")
}

func TestSSHConfigCommand_JSONOutput(t *testing.T) {
	setupSSHConfigMemoryTest(t, []string{"myvm"})

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
	assert.Equal(t, float64(memPortFromName("myvm")), data["port"])
	assert.Equal(t, "ubuntu", data["user"])
	assert.Equal(t, "/dev/null", data["identity_file"])
	assert.Equal(t, "tcp", data["transport"])
}

func TestSSHConfigCommand_JSONOutput_SecondVM(t *testing.T) {
	// NOTE: VSOCK transport is not supported by the memory backend.
	// VSOCK JSON output is tested by the formatSSHConfigFragment tests.
	// This test verifies JSON output for a second VM via the full command flow.
	setupSSHConfigMemoryTest(t, []string{"testvm"})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "ssh-config", "testvm"})
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
	assert.Equal(t, "tcp", data["transport"])
	assert.Equal(t, "sd-testvm", data["host"])
	assert.Equal(t, "ubuntu", data["user"])
	assert.Equal(t, float64(memPortFromName("testvm")), data["port"])
}

func TestSSHConfigCommand_VMNotFound(t *testing.T) {
	setupSSHConfigMemoryTest(t, nil)

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
	mb := setupSSHConfigMemoryTest(t, nil)
	mb.SetMethodError("available", fmt.Errorf("backend not available"))

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
	mb := setupSSHConfigMemoryTest(t, []string{"myvm"})
	mb.SetMethodError("sshconfig", fmt.Errorf("ssh config lookup failed"))

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
	setupSSHConfigMemoryTest(t, nil)

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
	vmNames := []string{"myvm", "testvm", "portvm", "lowport", "proxyvm"}
	for _, vmName := range vmNames {
		t.Run(vmName, func(t *testing.T) {
			setupSSHConfigMemoryTest(t, []string{vmName})

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"--json", "ssh-config", vmName})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)

			var result map[string]any
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err, "JSON must parse for %s: %s", vmName, buf.String())
			assert.True(t, result["ok"].(bool), "ok must be true for %s", vmName)

			data := result["data"].(map[string]any)
			assert.Equal(t, "sd-"+vmName, data["host"], "host must be sd-<name> for %s", vmName)
			assert.Equal(t, "ubuntu", data["user"], "user must match for %s", vmName)
			assert.Equal(t, "/dev/null", data["identity_file"], "identity_file must match for %s", vmName)
			assert.Equal(t, "tcp", data["transport"], "transport must match for %s", vmName)
		})
	}
}

// Property: human output always contains the VM name in the Host line.
func TestProperty_SSHConfigHumanContainsHostLine(t *testing.T) {
	names := []string{"myvm", "test-vm", "vm-123", "production"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			setupSSHConfigMemoryTest(t, []string{name})

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
		setupSSHConfigMemoryTest(t, nil)

		root := RootCmd()
		root.SetArgs([]string{"ssh-config", "ghost"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "vm_not_found", cliErr.Code)
	})

	t.Run("backend_unavailable", func(t *testing.T) {
		mb := setupSSHConfigMemoryTest(t, nil)
		mb.SetMethodError("available", fmt.Errorf("backend not available"))

		root := RootCmd()
		root.SetArgs([]string{"ssh-config", "vm1"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "backend_unavailable", cliErr.Code)
	})

	t.Run("invalid_argument_empty_name", func(t *testing.T) {
		setupSSHConfigMemoryTest(t, nil)

		root := RootCmd()
		root.SetArgs([]string{"ssh-config", ""})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "invalid_argument", cliErr.Code)
	})

	t.Run("ssh_connection_failed", func(t *testing.T) {
		mb := setupSSHConfigMemoryTest(t, []string{"vm1"})
		mb.SetMethodError("sshconfig", fmt.Errorf("internal error"))

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
			setupSSHConfigMemoryTest(t, nil)

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
	setupSSHConfigMemoryTest(t, []string{"myvm"})

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
	setupSSHConfigMemoryTest(t, []string{"myvm"})

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
// NOTE: VSOCK is tested via formatSSHConfigFragment (no backend needed).
// TCP is tested via the full command flow with the memory backend.
func TestProperty_SSHConfig_VSOCKHasProxyCommand_TCPHasPort(t *testing.T) {
	t.Run("vsock_has_proxy_command", func(t *testing.T) {
		// Test VSOCK formatting directly via formatSSHConfigFragment
		cfg := backend.SSHConfig{
			User:         "dev",
			IdentityFile: "/home/.sd/vms/myvm/ssh/id_ed25519",
			ProxyCommand: "limactl ssh --stdio myvm",
			Transport:    "vsock",
		}
		output := formatSSHConfigFragment("myvm", cfg)
		assert.Contains(t, output, "ProxyCommand")
	})

	t.Run("tcp_has_port", func(t *testing.T) {
		setupSSHConfigMemoryTest(t, []string{"myvm"})

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
		assert.Contains(t, buf.String(), fmt.Sprintf("Port %d", memPortFromName("myvm")))
	})
}

// --- Format flag tests ---

func TestSSHConfigCommand_FormatJSON(t *testing.T) {
	// --format=json without --json outputs raw JSON (no ok/data envelope)
	setupSSHConfigMemoryTest(t, []string{"myvm"})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"ssh-config", "myvm", "--format=json"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var data map[string]any
	err = json.Unmarshal(buf.Bytes(), &data)
	require.NoError(t, err, "output must be valid JSON: %s", buf.String())

	assert.Equal(t, "sd-myvm", data["host"])
	assert.Equal(t, "127.0.0.1", data["hostname"])
	assert.Equal(t, float64(memPortFromName("myvm")), data["port"])
	assert.Equal(t, "ubuntu", data["user"])
	assert.Equal(t, "/dev/null", data["identity_file"])
	assert.Equal(t, "tcp", data["transport"])
}

func TestSSHConfigCommand_FormatJSON_WithGlobalJSON(t *testing.T) {
	// --json --format=json wraps in {ok:true, data:...} envelope
	setupSSHConfigMemoryTest(t, []string{"myvm"})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "ssh-config", "myvm", "--format=json"})
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
	assert.Equal(t, "tcp", data["transport"])
}

func TestSSHConfigCommand_FormatVSCode(t *testing.T) {
	setupSSHConfigMemoryTest(t, []string{"myvm"})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"ssh-config", "myvm", "--format=vscode"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	output := buf.String()
	// Should be valid JSON snippet
	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err, "vscode format must be valid JSON: %s", output)

	assert.Equal(t, "sd-myvm", result["host"])
	assert.Contains(t, result, "ssh.remotePlatform")
	assert.Contains(t, result, "remote.SSH.configFile")

	platformMap := result["ssh.remotePlatform"].(map[string]any)
	assert.Equal(t, "linux", platformMap["sd-myvm"])
	assert.Equal(t, "~/.ssh/config", result["remote.SSH.configFile"])
}

func TestSSHConfigCommand_FormatVSCode_JSON(t *testing.T) {
	setupSSHConfigMemoryTest(t, []string{"myvm"})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "ssh-config", "myvm", "--format=vscode"})
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
	assert.Contains(t, data, "ssh.remotePlatform")
	assert.Contains(t, data, "remote.SSH.configFile")
}

func TestSSHConfigCommand_FormatSSH_Default(t *testing.T) {
	// --format=ssh should behave identically to no format flag
	setupSSHConfigMemoryTest(t, []string{"myvm"})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"ssh-config", "myvm", "--format=ssh"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	output := buf.String()
	assert.Contains(t, output, "Host sd-myvm")
	assert.Contains(t, output, "HostName 127.0.0.1")
	assert.Contains(t, output, "# Managed by sd. Do not edit manually.")
}

func TestSSHConfigCommand_FormatInvalid(t *testing.T) {
	setupSSHConfigMemoryTest(t, []string{"myvm"})

	root := RootCmd()
	root.SetArgs([]string{"ssh-config", "myvm", "--format=yaml"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
	assert.Contains(t, cliErr.Message, "yaml")
}

func TestSSHConfigCommand_FormatJSON_AllFields(t *testing.T) {
	// Verify all required fields are present in --format=json output (raw, no envelope)
	setupSSHConfigMemoryTest(t, []string{"myvm"})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"ssh-config", "myvm", "--format=json"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var data map[string]any
	err = json.Unmarshal(buf.Bytes(), &data)
	require.NoError(t, err, "output must be valid JSON: %s", buf.String())

	requiredFields := []string{"host", "hostname", "port", "user", "identity_file", "transport"}
	for _, field := range requiredFields {
		assert.Contains(t, data, field, "JSON output must contain %q field", field)
	}
}

// --- Unit test for formatVSCodeConfig ---

func TestFormatVSCodeConfig(t *testing.T) {
	result := formatVSCodeConfig("my-project")
	assert.Equal(t, "sd-my-project", result.Host)
	assert.Equal(t, "linux", result.SSHRemotePlatform["sd-my-project"])
	assert.Equal(t, "~/.ssh/config", result.RemoteSSHConfig)
}

func TestFormatVSCodeConfig_HostPrefix(t *testing.T) {
	names := []string{"test", "my-vm", "production"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			result := formatVSCodeConfig(name)
			assert.Equal(t, "sd-"+name, result.Host)
			assert.Contains(t, result.SSHRemotePlatform, "sd-"+name)
		})
	}
}

// Property: ForwardAgent and ForwardX11 are always "no".
func TestProperty_SSHConfig_ForwardAgentAndX11AlwaysNo(t *testing.T) {
	configs := []struct {
		name   string
		sshCfg backend.SSHConfig
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
