// Package cmd provides tests for the provision command.
// REQ-006-001: Built-in module listing.
// REQ-006-010: Re-provisioning existing VMs.
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/backend"
	"sd/internal/provision"
	"sd/internal/ui"
)

// mockProvisionBackend is a digital twin of a backend for provision command testing.
// It implements backend.Backend with configurable behavior and records Exec calls.
type mockProvisionBackend struct {
	name      string
	available bool
	statusMap map[string]backend.VMStatus
	statusErr error
	// Records all Exec calls
	execCalls []provisionExecCall
	// Configurable results per call index
	execResults []backend.ExecResult
	execErr     error
}

type provisionExecCall struct {
	VMName  string
	Command []string
}

func (m *mockProvisionBackend) Name() string { return m.name }
func (m *mockProvisionBackend) Available() error {
	if m.available {
		return nil
	}
	return fmt.Errorf("backend not available")
}
func (m *mockProvisionBackend) Create(_ context.Context, _ string, _ backend.VMConfig) error {
	return nil
}
func (m *mockProvisionBackend) Start(_ context.Context, _ string) error    { return nil }
func (m *mockProvisionBackend) Stop(_ context.Context, _ string) error     { return nil }
func (m *mockProvisionBackend) Destroy(_ context.Context, _ string) error  { return nil }
func (m *mockProvisionBackend) SSHConfig(_ context.Context, _ string) (backend.SSHConfig, error) {
	return backend.SSHConfig{}, nil
}
func (m *mockProvisionBackend) List(_ context.Context) ([]backend.VMInfo, error) {
	return nil, nil
}
func (m *mockProvisionBackend) Exec(_ context.Context, name string, command []string) (backend.ExecResult, error) {
	if m.execErr != nil {
		return backend.ExecResult{}, m.execErr
	}
	m.execCalls = append(m.execCalls, provisionExecCall{VMName: name, Command: command})
	idx := len(m.execCalls) - 1
	if idx < len(m.execResults) {
		return m.execResults[idx], nil
	}
	return backend.ExecResult{ExitCode: 0, Stdout: "", Stderr: ""}, nil
}
func (m *mockProvisionBackend) Status(_ context.Context, name string) (backend.VMStatus, error) {
	if m.statusErr != nil {
		return "", m.statusErr
	}
	if s, ok := m.statusMap[name]; ok {
		return s, nil
	}
	return "", backend.ErrVMNotFound
}

// setupProvisionTest configures the test environment with a mock backend and
// simplified modules for faster testing.
func setupProvisionTest(t *testing.T, mb *mockProvisionBackend) {
	t.Helper()
	newRootTestEnv(t)

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return mb, nil
	}
	origValidateBackend := validateBackendFunc
	validateBackendFunc = func(_ string) error {
		return nil
	}
	t.Cleanup(func() {
		getBackendFunc = origGetBackend
		validateBackendFunc = origValidateBackend
	})

	// Reset --modules flag on provision subcommand to prevent leakage between tests.
	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "provision" {
			_ = cmd.Flags().Set("modules", "")
			break
		}
	}

	// Default to simplified modules; individual tests can override.
	origLoad := loadBuiltinModules
	loadBuiltinModules = func() ([]provision.Module, error) {
		return []provision.Module{
			{Name: "base", Description: "Base", Scripts: []provision.Script{
				{Mode: provision.ModeSystem, Script: "echo base"},
			}},
		}, nil
	}
	t.Cleanup(func() { loadBuiltinModules = origLoad })
}

// captureStdout captures os.Stdout during the execution of fn and returns the output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.String()
}

// --- Unit tests ---

func TestProvisionCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "provision" {
			found = true
			assert.Equal(t, "provisioning", cmd.GroupID)
			subNames := make(map[string]bool)
			for _, sub := range cmd.Commands() {
				subNames[sub.Name()] = true
			}
			assert.True(t, subNames["list"], "provision must have 'list' subcommand")
			break
		}
	}
	assert.True(t, found, "provision command must be registered")
}

func TestProvisionCommand_Flags(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"provision"})
	require.NoError(t, err)

	flag := cmd.Flags().Lookup("modules")
	assert.NotNil(t, flag, "provision must have --modules flag")
	assert.Equal(t, "", flag.DefValue, "--modules default should be empty")
}

func TestProvisionCommand_MissingName(t *testing.T) {
	mb := &mockProvisionBackend{name: "test", available: true, statusMap: map[string]backend.VMStatus{}}
	setupProvisionTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"provision"})
	err := root.Execute()
	assert.Error(t, err)

	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
}

func TestProvisionCommand_EmptyName(t *testing.T) {
	mb := &mockProvisionBackend{name: "test", available: true, statusMap: map[string]backend.VMStatus{}}
	setupProvisionTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"provision", ""})
	err := root.Execute()
	assert.Error(t, err)

	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
}

func TestProvisionCommand_VMNotFound(t *testing.T) {
	mb := &mockProvisionBackend{name: "test", available: true, statusMap: map[string]backend.VMStatus{}}
	setupProvisionTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"provision", "nonexistent"})
	err := root.Execute()
	assert.Error(t, err)

	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
}

func TestProvisionCommand_VMNotRunning(t *testing.T) {
	mb := &mockProvisionBackend{
		name:      "test",
		available: true,
		statusMap: map[string]backend.VMStatus{
			"myvm": backend.StatusStopped,
		},
	}
	setupProvisionTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"provision", "myvm"})
	err := root.Execute()
	assert.Error(t, err)

	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_running", cliErr.Code)
	assert.Contains(t, cliErr.Message, "not running")
}

func TestProvisionCommand_BackendUnavailable(t *testing.T) {
	mb := &mockProvisionBackend{name: "test", available: false}
	setupProvisionTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"provision", "myvm"})
	err := root.Execute()
	assert.Error(t, err)

	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestProvisionCommand_StatusCheckError(t *testing.T) {
	mb := &mockProvisionBackend{
		name:      "test",
		available: true,
		statusErr: fmt.Errorf("connection lost"),
	}
	setupProvisionTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"provision", "myvm"})
	err := root.Execute()
	assert.Error(t, err)

	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "provision_failed", cliErr.Code)
}

func TestProvisionCommand_HumanOutput(t *testing.T) {
	mb := &mockProvisionBackend{
		name:      "test",
		available: true,
		statusMap: map[string]backend.VMStatus{
			"myvm": backend.StatusRunning,
		},
	}
	setupProvisionTest(t, mb)

	output := captureStdout(t, func() {
		root := RootCmd()
		root.SetArgs([]string{"provision", "myvm"})
		err := root.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Provisioned VM \"myvm\"")
	assert.Contains(t, output, "base")
}

func TestProvisionCommand_JSONOutput(t *testing.T) {
	mb := &mockProvisionBackend{
		name:      "test",
		available: true,
		statusMap: map[string]backend.VMStatus{
			"myvm": backend.StatusRunning,
		},
	}
	setupProvisionTest(t, mb)

	output := captureStdout(t, func() {
		root := RootCmd()
		root.SetArgs([]string{"--json", "provision", "myvm"})
		err := root.Execute()
		require.NoError(t, err)
	})

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.True(t, result["ok"].(bool))

	data := result["data"].(map[string]interface{})
	assert.Equal(t, "myvm", data["vm"])

	modules := data["modules"].([]interface{})
	assert.Len(t, modules, 1)
	mod := modules[0].(map[string]interface{})
	assert.Equal(t, "base", mod["name"])
	assert.Equal(t, "completed", mod["status"])
}

func TestProvisionCommand_WithModulesFlag(t *testing.T) {
	mb := &mockProvisionBackend{
		name:      "test",
		available: true,
		statusMap: map[string]backend.VMStatus{
			"myvm": backend.StatusRunning,
		},
	}
	setupProvisionTest(t, mb)

	// Override modules for this test
	loadBuiltinModules = func() ([]provision.Module, error) {
		return []provision.Module{
			{Name: "base", Description: "Base", Scripts: []provision.Script{{Mode: provision.ModeSystem, Script: "echo base"}}},
			{Name: "golang", Description: "Go", DependsOn: []string{"base"}, Scripts: []provision.Script{{Mode: provision.ModeSystem, Script: "echo go"}}},
			{Name: "docker", Description: "Docker", DependsOn: []string{"base"}, Scripts: []provision.Script{{Mode: provision.ModeSystem, Script: "echo docker"}}},
		}, nil
	}

	output := captureStdout(t, func() {
		root := RootCmd()
		root.SetArgs([]string{"--json", "provision", "myvm", "--modules", "golang"})
		err := root.Execute()
		require.NoError(t, err)
	})

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.True(t, result["ok"].(bool))

	data := result["data"].(map[string]interface{})
	moduleList := data["modules"].([]interface{})

	names := make(map[string]bool)
	for _, m := range moduleList {
		mod := m.(map[string]interface{})
		names[mod["name"].(string)] = true
	}
	assert.True(t, names["base"])
	assert.True(t, names["golang"])
	assert.False(t, names["docker"])
}

func TestProvisionCommand_UnknownModule(t *testing.T) {
	mb := &mockProvisionBackend{
		name:      "test",
		available: true,
		statusMap: map[string]backend.VMStatus{
			"myvm": backend.StatusRunning,
		},
	}
	setupProvisionTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"provision", "myvm", "--modules", "nonexistent"})
	err := root.Execute()
	assert.Error(t, err)

	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "provision_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "unknown module")
}

func TestProvisionCommand_ScriptFailure(t *testing.T) {
	mb := &mockProvisionBackend{
		name:      "test",
		available: true,
		statusMap: map[string]backend.VMStatus{
			"myvm": backend.StatusRunning,
		},
		execResults: []backend.ExecResult{
			{ExitCode: 1, Stderr: "something went wrong"},
		},
	}
	setupProvisionTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"provision", "myvm"})
	err := root.Execute()
	assert.Error(t, err)

	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "provision_script_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "base")
}

func TestProvisionCommand_ExecError(t *testing.T) {
	mb := &mockProvisionBackend{
		name:      "test",
		available: true,
		statusMap: map[string]backend.VMStatus{
			"myvm": backend.StatusRunning,
		},
		execErr: fmt.Errorf("SSH connection failed"),
	}
	setupProvisionTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"provision", "myvm"})
	err := root.Execute()
	assert.Error(t, err)

	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "provision_script_failed", cliErr.Code)
}

// --- Provision List tests ---

func TestProvisionList_HumanOutput(t *testing.T) {
	newRootTestEnv(t)

	output := captureStdout(t, func() {
		root := RootCmd()
		root.SetArgs([]string{"provision", "list"})
		err := root.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, output, "base")
	assert.Contains(t, output, "golang")
	assert.Contains(t, output, "claude-code")
}

func TestProvisionList_JSONOutput(t *testing.T) {
	newRootTestEnv(t)

	output := captureStdout(t, func() {
		root := RootCmd()
		root.SetArgs([]string{"--json", "provision", "list"})
		err := root.Execute()
		require.NoError(t, err)
	})

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.True(t, result["ok"].(bool))

	data := result["data"].([]interface{})
	assert.GreaterOrEqual(t, len(data), 12, "should have at least 12 built-in modules")

	first := data[0].(map[string]interface{})
	assert.Contains(t, first, "name")
	assert.Contains(t, first, "description")
	assert.Contains(t, first, "has_probe")
}

func TestProvisionList_RejectsExtraArgs(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()
	root.SetArgs([]string{"provision", "list", "extra"})

	err := root.Execute()
	assert.Error(t, err)
}

// --- Provisioner unit tests ---

func TestProvisioner_Success(t *testing.T) {
	execFn := func(_ context.Context, vmName string, command []string) (string, string, int, error) {
		return "", "", 0, nil
	}

	mods := []provision.Module{
		{Name: "base", Scripts: []provision.Script{
			{Mode: provision.ModeSystem, Script: "apt-get update"},
			{Mode: provision.ModeUser, Script: "echo done"},
		}},
	}

	result := provision.Provision(context.Background(), execFn, "test-vm", mods)
	assert.False(t, result.Failed)
	assert.Equal(t, "test-vm", result.State.VMName)
	assert.Len(t, result.State.Modules, 1)
	assert.Equal(t, provision.StatusCompleted, result.State.Modules[0].Status)
	assert.NotNil(t, result.State.Finished)
}

func TestProvisioner_SystemModeUsesSudo(t *testing.T) {
	var capturedCmd []string
	execFn := func(_ context.Context, _ string, command []string) (string, string, int, error) {
		capturedCmd = command
		return "", "", 0, nil
	}

	mods := []provision.Module{
		{Name: "base", Scripts: []provision.Script{
			{Mode: provision.ModeSystem, Script: "apt-get install -y curl"},
		}},
	}

	result := provision.Provision(context.Background(), execFn, "vm", mods)
	assert.False(t, result.Failed)
	assert.Equal(t, []string{"sudo", "bash", "-c"}, capturedCmd[:3])
	assert.Contains(t, capturedCmd[3], "set -eu -o pipefail")
	assert.Contains(t, capturedCmd[3], "apt-get install -y curl")
}

func TestProvisioner_UserModeNoSudo(t *testing.T) {
	var capturedCmd []string
	execFn := func(_ context.Context, _ string, command []string) (string, string, int, error) {
		capturedCmd = command
		return "", "", 0, nil
	}

	mods := []provision.Module{
		{Name: "base", Scripts: []provision.Script{
			{Mode: provision.ModeUser, Script: "echo hello"},
		}},
	}

	result := provision.Provision(context.Background(), execFn, "vm", mods)
	assert.False(t, result.Failed)
	assert.Equal(t, "bash", capturedCmd[0])
	assert.NotContains(t, capturedCmd, "sudo")
}

func TestProvisioner_ScriptPreamble(t *testing.T) {
	var capturedScript string
	execFn := func(_ context.Context, _ string, command []string) (string, string, int, error) {
		if len(command) >= 3 {
			capturedScript = command[2]
		}
		return "", "", 0, nil
	}

	mods := []provision.Module{
		{Name: "test", Scripts: []provision.Script{
			{Mode: provision.ModeUser, Script: "echo test"},
		}},
	}

	provision.Provision(context.Background(), execFn, "vm", mods)
	assert.True(t, strings.HasPrefix(capturedScript, "set -eu -o pipefail\n"))
}

func TestProvisioner_ScriptExitNonZero(t *testing.T) {
	execFn := func(_ context.Context, _ string, _ []string) (string, string, int, error) {
		return "", "error output", 1, nil
	}

	mods := []provision.Module{
		{Name: "base", Scripts: []provision.Script{
			{Mode: provision.ModeSystem, Script: "false"},
		}},
	}

	result := provision.Provision(context.Background(), execFn, "vm", mods)
	assert.True(t, result.Failed)
	assert.Equal(t, "base", result.Module)
	assert.Equal(t, provision.StatusFailed, result.State.Modules[0].Status)
}

func TestProvisioner_ExecError(t *testing.T) {
	execFn := func(_ context.Context, _ string, _ []string) (string, string, int, error) {
		return "", "", 0, fmt.Errorf("SSH connection refused")
	}

	mods := []provision.Module{
		{Name: "base", Scripts: []provision.Script{
			{Mode: provision.ModeSystem, Script: "echo hi"},
		}},
	}

	result := provision.Provision(context.Background(), execFn, "vm", mods)
	assert.True(t, result.Failed)
	assert.Equal(t, "base", result.Module)
	assert.Contains(t, result.Error, "SSH connection refused")
}

func TestProvisioner_MultiModuleStopsOnFailure(t *testing.T) {
	callCount := 0
	execFn := func(_ context.Context, _ string, _ []string) (string, string, int, error) {
		callCount++
		if callCount == 1 {
			return "", "", 1, nil
		}
		return "", "", 0, nil
	}

	mods := []provision.Module{
		{Name: "base", Scripts: []provision.Script{
			{Mode: provision.ModeSystem, Script: "fail here"},
		}},
		{Name: "golang", Scripts: []provision.Script{
			{Mode: provision.ModeSystem, Script: "should not run"},
		}},
	}

	result := provision.Provision(context.Background(), execFn, "vm", mods)
	assert.True(t, result.Failed)
	assert.Equal(t, "base", result.Module)
	assert.Equal(t, provision.StatusFailed, result.State.Modules[0].Status)
	assert.Len(t, result.State.Modules, 1)
}

func TestProvisioner_MultiScriptInModule(t *testing.T) {
	var scripts []string
	execFn := func(_ context.Context, _ string, command []string) (string, string, int, error) {
		// Extract script from ["sudo", "bash", "-c", script] or ["bash", "-c", script]
		for i, arg := range command {
			if arg == "-c" && i+1 < len(command) {
				scripts = append(scripts, command[i+1])
				break
			}
		}
		return "", "", 0, nil
	}

	mods := []provision.Module{
		{Name: "base", Scripts: []provision.Script{
			{Mode: provision.ModeSystem, Script: "first"},
			{Mode: provision.ModeUser, Script: "second"},
			{Mode: provision.ModeUser, Script: "third"},
		}},
	}

	result := provision.Provision(context.Background(), execFn, "vm", mods)
	assert.False(t, result.Failed)
	assert.Len(t, scripts, 3)
	assert.Contains(t, scripts[0], "first")
	assert.Contains(t, scripts[1], "second")
	assert.Contains(t, scripts[2], "third")
}

func TestProvisioner_SecondScriptFailure(t *testing.T) {
	callCount := 0
	execFn := func(_ context.Context, _ string, _ []string) (string, string, int, error) {
		callCount++
		if callCount == 2 {
			return "", "failure", 1, nil
		}
		return "", "", 0, nil
	}

	mods := []provision.Module{
		{Name: "base", Scripts: []provision.Script{
			{Mode: provision.ModeSystem, Script: "first"},
			{Mode: provision.ModeSystem, Script: "second-fails"},
			{Mode: provision.ModeUser, Script: "third"},
		}},
	}

	result := provision.Provision(context.Background(), execFn, "vm", mods)
	assert.True(t, result.Failed)
	assert.Equal(t, "base", result.Module)
	assert.Equal(t, 1, result.Script)
	assert.Equal(t, 2, callCount)
}

func TestProvisioner_EmptyModules(t *testing.T) {
	execFn := func(_ context.Context, _ string, _ []string) (string, string, int, error) {
		return "", "", 0, nil
	}

	result := provision.Provision(context.Background(), execFn, "vm", nil)
	assert.False(t, result.Failed)
	assert.Empty(t, result.State.Modules)
}

func TestProvisioner_StateTracking(t *testing.T) {
	execFn := func(_ context.Context, _ string, _ []string) (string, string, int, error) {
		return "", "", 0, nil
	}

	mods := []provision.Module{
		{Name: "base", Scripts: []provision.Script{{Mode: provision.ModeSystem, Script: "echo base"}}},
		{Name: "golang", Scripts: []provision.Script{{Mode: provision.ModeSystem, Script: "echo go"}}},
	}

	result := provision.Provision(context.Background(), execFn, "vm", mods)
	assert.False(t, result.Failed)

	assert.Equal(t, "vm", result.State.VMName)
	assert.False(t, result.State.Started.IsZero())
	assert.NotNil(t, result.State.Finished)

	assert.Len(t, result.State.Modules, 2)
	assert.Equal(t, "base", result.State.Modules[0].Name)
	assert.Equal(t, provision.StatusCompleted, result.State.Modules[0].Status)
	assert.NotNil(t, result.State.Modules[0].StartedAt)
	assert.NotNil(t, result.State.Modules[0].EndedAt)

	assert.Equal(t, "golang", result.State.Modules[1].Name)
	assert.Equal(t, provision.StatusCompleted, result.State.Modules[1].Status)
}

// --- FormatModuleList tests ---

func TestFormatModuleList_Empty(t *testing.T) {
	result := provision.FormatModuleList(nil)
	assert.Equal(t, "No modules available.\n", result)
}

func TestFormatModuleList_WithModules(t *testing.T) {
	mods := []provision.Module{
		{Name: "base", Description: "Base system packages"},
		{Name: "golang", Description: "Go toolchain", DependsOn: []string{"base"}},
	}
	result := provision.FormatModuleList(mods)
	assert.Contains(t, result, "base")
	assert.Contains(t, result, "Base system packages")
	assert.Contains(t, result, "none")
	assert.Contains(t, result, "golang")
	assert.Contains(t, result, "Go toolchain")
}

// --- Property-based tests ---

func TestProvisionList_JSONAlwaysValid(t *testing.T) {
	newRootTestEnv(t)

	output := captureStdout(t, func() {
		root := RootCmd()
		root.SetArgs([]string{"--json", "provision", "list"})
		err := root.Execute()
		require.NoError(t, err)
	})

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.True(t, result["ok"].(bool))
	assert.NotNil(t, result["data"])
}

func TestProvision_ErrorCodesSnakeCase(t *testing.T) {
	codes := []string{
		"vm_not_found",
		"vm_not_running",
		"backend_unavailable",
		"provision_failed",
		"provision_script_failed",
		"invalid_argument",
	}
	for _, code := range codes {
		t.Run(code, func(t *testing.T) {
			assert.True(t, isValidProvisionErrorCode(code), "error code %q should be snake_case", code)
		})
	}
}

func isValidProvisionErrorCode(code string) bool {
	for _, c := range code {
		if c >= 'A' && c <= 'Z' {
			return false
		}
		if c == ' ' {
			return false
		}
	}
	return strings.Contains(code, "_")
}

func TestProvision_RunningVMCallsBackend(t *testing.T) {
	for _, vmName := range []string{"dev", "test", "prod"} {
		t.Run(vmName, func(t *testing.T) {
			mb := &mockProvisionBackend{
				name:      "test",
				available: true,
				statusMap: map[string]backend.VMStatus{vmName: backend.StatusRunning},
			}
			setupProvisionTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"provision", vmName})
			err := root.Execute()
			require.NoError(t, err)
			assert.GreaterOrEqual(t, len(mb.execCalls), 1, "should have called Exec at least once")
			assert.Equal(t, vmName, mb.execCalls[0].VMName)
		})
	}
}

func TestProvision_StoppedVMNeverCallsBackend(t *testing.T) {
	for _, status := range []backend.VMStatus{backend.StatusStopped, backend.StatusCreating, backend.StatusError} {
		t.Run(string(status), func(t *testing.T) {
			mb := &mockProvisionBackend{
				name:      "test",
				available: true,
				statusMap: map[string]backend.VMStatus{"myvm": status},
			}
			setupProvisionTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"provision", "myvm"})
			err := root.Execute()
			assert.Error(t, err)
			assert.Empty(t, mb.execCalls, "should not call Exec for non-running VM")
		})
	}
}

func TestProvision_NonexistentVMNeverCallsBackend(t *testing.T) {
	for _, vmName := range []string{"ghost", "missing", "nope"} {
		t.Run(vmName, func(t *testing.T) {
			mb := &mockProvisionBackend{
				name:      "test",
				available: true,
				statusMap: map[string]backend.VMStatus{},
			}
			setupProvisionTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"provision", vmName})
			err := root.Execute()
			assert.Error(t, err)
			assert.Empty(t, mb.execCalls, "should not call Exec for nonexistent VM")
		})
	}
}

func TestProvision_JSONRequiredFields(t *testing.T) {
	mb := &mockProvisionBackend{
		name:      "test",
		available: true,
		statusMap: map[string]backend.VMStatus{"myvm": backend.StatusRunning},
	}
	setupProvisionTest(t, mb)

	output := captureStdout(t, func() {
		root := RootCmd()
		root.SetArgs([]string{"--json", "provision", "myvm"})
		err := root.Execute()
		require.NoError(t, err)
	})

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	data := result["data"].(map[string]interface{})
	assert.Contains(t, data, "vm")
	assert.Contains(t, data, "modules")

	modules := data["modules"].([]interface{})
	require.Len(t, modules, 1)
	mod := modules[0].(map[string]interface{})
	assert.Contains(t, mod, "name")
	assert.Contains(t, mod, "status")
}

func TestProvision_ErrorJSONSerializable(t *testing.T) {
	mb := &mockProvisionBackend{
		name:      "test",
		available: true,
		statusMap: map[string]backend.VMStatus{},
	}
	setupProvisionTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"--json", "provision", "nonexistent"})
	err := root.Execute()
	assert.Error(t, err)

	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)

	errJSON, jsonErr := json.Marshal(cliErr)
	require.NoError(t, jsonErr)
	assert.Contains(t, string(errJSON), "vm_not_found")
}

func TestProvisionList_ContainsAllBuiltinModules(t *testing.T) {
	newRootTestEnv(t)

	output := captureStdout(t, func() {
		root := RootCmd()
		root.SetArgs([]string{"--json", "provision", "list"})
		err := root.Execute()
		require.NoError(t, err)
	})

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	data := result["data"].([]interface{})
	names := make(map[string]bool)
	for _, entry := range data {
		m := entry.(map[string]interface{})
		names[m["name"].(string)] = true
	}

	for _, name := range provision.BuiltinModuleNames {
		assert.True(t, names[name], "built-in module %q should appear in list", name)
	}
}

func TestProvisionList_EmbeddedFS(t *testing.T) {
	mods, err := provision.LoadBuiltinModules()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(mods), 8)

	for _, name := range provision.BuiltinModuleNames {
		found := false
		for _, m := range mods {
			if m.Name == name {
				found = true
				break
			}
		}
		assert.True(t, found, "module %q should be in loaded modules", name)
	}
}

func TestProvisioner_PreambleApplied(t *testing.T) {
	var captured string
	execFn := func(_ context.Context, _ string, command []string) (string, string, int, error) {
		for i, arg := range command {
			if arg == "-c" && i+1 < len(command) {
				captured = command[i+1]
				break
			}
		}
		return "", "", 0, nil
	}

	mods := []provision.Module{
		{Name: "test", Scripts: []provision.Script{
			{Mode: provision.ModeUser, Script: "echo hello"},
		}},
	}

	provision.Provision(context.Background(), execFn, "vm", mods)
	assert.True(t, strings.HasPrefix(captured, "set -eu -o pipefail\n"))
	assert.Contains(t, captured, "echo hello")
}

// --- REQ-004-019, REQ-006-005: re-provision auto-snapshot + provision log ---

// snapshotterProvisionBackend extends mockProvisionBackend with the
// backend.Snapshotter interface so the provision command will attempt the
// pre-provision safety snapshot path.
type snapshotterProvisionBackend struct {
	*mockProvisionBackend
	snapshots   []string
	snapshotErr error
}

func (s *snapshotterProvisionBackend) SnapshotCreate(_ context.Context, _ string, tag string) error {
	if s.snapshotErr != nil {
		return s.snapshotErr
	}
	s.snapshots = append(s.snapshots, tag)
	return nil
}

func (s *snapshotterProvisionBackend) SnapshotList(_ context.Context, _ string) ([]backend.SnapshotInfo, error) {
	return nil, nil
}

func (s *snapshotterProvisionBackend) SnapshotDelete(_ context.Context, _ string, _ string) error {
	return nil
}

func (s *snapshotterProvisionBackend) SnapshotApply(_ context.Context, _ string, _ string) error {
	return nil
}

// setupProvisionSnapshotTest wires a snapshotter-capable backend with a fixed
// snapshot tag for deterministic assertions, and resets the
// --no-snapshot flag between tests.
func setupProvisionSnapshotTest(t *testing.T, sb *snapshotterProvisionBackend) {
	t.Helper()
	setupProvisionTest(t, sb.mockProvisionBackend)

	// Replace the getBackendFunc again to return the *snapshotter* wrapper
	// (setupProvisionTest already redirected it, but to the inner mock).
	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) { return sb, nil }
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	// Reset --no-snapshot between tests.
	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "provision" {
			_ = cmd.Flags().Set("no-snapshot", "false")
			break
		}
	}

	// Deterministic snapshot tag.
	origTag := autoProvisionSnapshotTag
	autoProvisionSnapshotTag = func(_ string) string { return "pre-provision-20260520-120000" }
	t.Cleanup(func() { autoProvisionSnapshotTag = origTag })
}

func TestProvisionCommand_NoProvisionSnapshotFlag_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()
	cmd, _, err := root.Find([]string{"provision"})
	require.NoError(t, err)

	flag := cmd.Flags().Lookup("no-snapshot")
	require.NotNil(t, flag, "provision must have --no-snapshot flag")
	assert.Equal(t, "false", flag.DefValue)
}

// REQ-004-019: re-provision creates a safety snapshot by default.
func TestProvisionCommand_ReProvision_CreatesSnapshot(t *testing.T) {
	sb := &snapshotterProvisionBackend{
		mockProvisionBackend: &mockProvisionBackend{
			name:      "test",
			available: true,
			statusMap: map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		},
	}
	setupProvisionSnapshotTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"provision", "myvm"})
	require.NoError(t, root.Execute())

	require.Len(t, sb.snapshots, 1, "expected exactly one pre-provision snapshot")
	assert.Equal(t, "pre-provision-20260520-120000", sb.snapshots[0])
}

// REQ-004-019: snapshot tag is surfaced in JSON output.
func TestProvisionCommand_ReProvision_SnapshotTagInJSON(t *testing.T) {
	sb := &snapshotterProvisionBackend{
		mockProvisionBackend: &mockProvisionBackend{
			name:      "test",
			available: true,
			statusMap: map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		},
	}
	setupProvisionSnapshotTest(t, sb)

	output := captureStdout(t, func() {
		root := RootCmd()
		root.SetArgs([]string{"--json", "provision", "myvm"})
		require.NoError(t, root.Execute())
	})

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	data := result["data"].(map[string]interface{})
	assert.Equal(t, "pre-provision-20260520-120000", data["snapshot_tag"])
}

// REQ-004-019: --no-snapshot skips the auto-snapshot.
func TestProvisionCommand_NoProvisionSnapshotFlag_SkipsSnapshot(t *testing.T) {
	sb := &snapshotterProvisionBackend{
		mockProvisionBackend: &mockProvisionBackend{
			name:      "test",
			available: true,
			statusMap: map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		},
	}
	setupProvisionSnapshotTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"provision", "myvm", "--no-snapshot"})
	require.NoError(t, root.Execute())

	assert.Empty(t, sb.snapshots, "no snapshot should have been created")
}

// REQ-004-019: snapshot failure is fatal unless --no-snapshot was passed.
func TestProvisionCommand_SnapshotFailure_Fatal(t *testing.T) {
	sb := &snapshotterProvisionBackend{
		mockProvisionBackend: &mockProvisionBackend{
			name:      "test",
			available: true,
			statusMap: map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		},
		snapshotErr: fmt.Errorf("disk full"),
	}
	setupProvisionSnapshotTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"provision", "myvm"})
	err := root.Execute()
	require.Error(t, err)

	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "snapshot_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "disk full")
	assert.Contains(t, cliErr.Message, "--no-snapshot")
	// No exec calls should have occurred — we aborted before provisioning.
	assert.Empty(t, sb.mockProvisionBackend.execCalls, "must not provision if snapshot failed")
}

// REQ-004-019: snapshot failure is non-fatal if user opted out with the flag.
func TestProvisionCommand_SnapshotFailure_IgnoredWithFlag(t *testing.T) {
	sb := &snapshotterProvisionBackend{
		mockProvisionBackend: &mockProvisionBackend{
			name:      "test",
			available: true,
			statusMap: map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		},
		snapshotErr: fmt.Errorf("disk full"),
	}
	setupProvisionSnapshotTest(t, sb)

	root := RootCmd()
	root.SetArgs([]string{"provision", "myvm", "--no-snapshot"})
	require.NoError(t, root.Execute())
	assert.Empty(t, sb.snapshots)
}

// REQ-006-005: provision.log file grows across runs (append, with header).
func TestProvisionCommand_ProvisionLogFileGrows(t *testing.T) {
	mb := &mockProvisionBackend{
		name:      "test",
		available: true,
		statusMap: map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		execResults: []backend.ExecResult{
			{ExitCode: 0, Stdout: "first-run-stdout", Stderr: "first-run-stderr"},
		},
	}
	tmp := setupProvisionTest_returningHome(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"provision", "myvm", "--no-snapshot"})
	require.NoError(t, root.Execute())

	logPath := filepath.Join(tmp, "vms", "myvm", "provision.log")
	data, err := os.ReadFile(logPath)
	require.NoError(t, err, "provision.log should exist")
	firstLen := len(data)
	require.Greater(t, firstLen, 0, "provision.log should not be empty")
	assert.Contains(t, string(data), "=== sd provision myvm @")
	assert.Contains(t, string(data), "first-run-stdout")
	assert.Contains(t, string(data), "first-run-stderr")
	// REQ-006-005, REQ-004-024: log must be 0o600 to limit credential leak surface.
	fi, statErr := os.Stat(logPath)
	require.NoError(t, statErr)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "provision.log must be 0o600")

	// Second invocation must append, not truncate.
	mb.execCalls = nil
	mb.execResults = []backend.ExecResult{
		{ExitCode: 0, Stdout: "second-run-stdout", Stderr: ""},
	}
	root.SetArgs([]string{"provision", "myvm", "--no-snapshot"})
	require.NoError(t, root.Execute())

	data2, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Greater(t, len(data2), firstLen, "provision.log must grow on second run")
	// Both runs are present.
	assert.Contains(t, string(data2), "first-run-stdout")
	assert.Contains(t, string(data2), "second-run-stdout")
}

// REQ-006-005: script-failure error surfaces stderr tail and log path.
func TestProvisionCommand_ScriptFailure_ErrorIncludesStderr(t *testing.T) {
	mb := &mockProvisionBackend{
		name:      "test",
		available: true,
		statusMap: map[string]backend.VMStatus{"myvm": backend.StatusRunning},
		execResults: []backend.ExecResult{
			{ExitCode: 2, Stderr: "apt: package not found"},
		},
	}
	tmp := setupProvisionTest_returningHome(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"provision", "myvm", "--no-snapshot"})
	err := root.Execute()
	require.Error(t, err)

	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "provision_script_failed", cliErr.Code)
	assert.Contains(t, cliErr.Message, "apt: package not found")
	assert.Contains(t, cliErr.Message, "full log:")
	assert.Contains(t, cliErr.Message, filepath.Join(tmp, "vms", "myvm", "provision.log"))
}

// setupProvisionTest_returningHome is like setupProvisionTest but returns the
// SD_HOME directory created for the test (so log-file assertions can locate
// provision.log).
func setupProvisionTest_returningHome(t *testing.T, mb *mockProvisionBackend) string {
	t.Helper()
	tmp := newRootTestEnv(t)

	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) { return mb, nil }
	origValidateBackend := validateBackendFunc
	validateBackendFunc = func(_ string) error { return nil }
	t.Cleanup(func() {
		getBackendFunc = origGetBackend
		validateBackendFunc = origValidateBackend
	})

	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "provision" {
			_ = cmd.Flags().Set("modules", "")
			_ = cmd.Flags().Set("no-snapshot", "false")
			break
		}
	}

	origLoad := loadBuiltinModules
	loadBuiltinModules = func() ([]provision.Module, error) {
		return []provision.Module{
			{Name: "base", Description: "Base", Scripts: []provision.Script{
				{Mode: provision.ModeSystem, Script: "echo base"},
			}},
		}, nil
	}
	t.Cleanup(func() { loadBuiltinModules = origLoad })

	return tmp
}
