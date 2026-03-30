// Package cmd provides tests for the status command.
// REQ-002-003: VM Management Commands -- status
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/backend"
	"sd/internal/ui"
)

// mockStatusBackend is a digital twin of a backend for status command testing.
// It implements backend.Backend with configurable behavior and records calls.
type mockStatusBackend struct {
	name      string
	available bool
	vms       map[string]backend.VMInfo
	statusErr error
	listErr   error
}

func (m *mockStatusBackend) Name() string { return m.name }
func (m *mockStatusBackend) Available() error {
	if m.available {
		return nil
	}
	return fmt.Errorf("backend not available")
}
func (m *mockStatusBackend) Create(_ context.Context, _ string, _ backend.VMConfig) error {
	return nil
}
func (m *mockStatusBackend) Start(_ context.Context, _ string) error { return nil }
func (m *mockStatusBackend) Stop(_ context.Context, _ string) error  { return nil }
func (m *mockStatusBackend) Destroy(_ context.Context, _ string) error { return nil }
func (m *mockStatusBackend) Status(_ context.Context, name string) (backend.VMStatus, error) {
	if m.statusErr != nil {
		return "", m.statusErr
	}
	vm, ok := m.vms[name]
	if !ok {
		return "", fmt.Errorf("vm %q not found: %w", name, backend.ErrVMNotFound)
	}
	return vm.Status, nil
}
func (m *mockStatusBackend) List(_ context.Context) ([]backend.VMInfo, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.vms == nil {
		return []backend.VMInfo{}, nil
	}
	var result []backend.VMInfo
	for _, vm := range m.vms {
		result = append(result, vm)
	}
	return result, nil
}
func (m *mockStatusBackend) SSHConfig(_ context.Context, _ string) (backend.SSHConfig, error) {
	return backend.SSHConfig{}, nil
}
func (m *mockStatusBackend) Exec(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
	return backend.ExecResult{}, nil
}

// setupStatusTest configures the test environment with a mock backend.
func setupStatusTest(t *testing.T, mb *mockStatusBackend) {
	t.Helper()
	newRootTestEnv(t)

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })
}

// --- Unit tests ---

func TestStatusCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "status" {
			found = true
			assert.Equal(t, "vm", cmd.GroupID)
			break
		}
	}
	assert.True(t, found, "status command must be registered")
}

func TestStatusCommand_MaxArgs(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"status"})
	require.NoError(t, err)
	assert.NotNil(t, cmd.Args, "status must have an Args validator")
	// 0 args should pass
	assert.NoError(t, cmd.Args(cmd, nil))
	// 1 arg should pass
	assert.NoError(t, cmd.Args(cmd, []string{"myvm"}))
	// 2 args should fail
	assert.Error(t, cmd.Args(cmd, []string{"a", "b"}), "status must reject more than 1 arg")
}

func TestStatusCommand_SingleVM_HumanOutput(t *testing.T) {
	mb := &mockStatusBackend{
		name:      "mock",
		available: true,
		vms: map[string]backend.VMInfo{
			"testvm": {
				Name:    "testvm",
				Status:  backend.StatusRunning,
				Backend: "lima",
				CPUs:    4,
				Memory:  "8GiB",
				Disk:    "100GiB",
				IP:      "192.168.5.15",
			},
		},
	}
	setupStatusTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"status", "testvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	output := buf.String()
	assert.Contains(t, output, "testvm")
	assert.Contains(t, output, "running")
	assert.Contains(t, output, "lima")
	assert.Contains(t, output, "8GiB")
}

func TestStatusCommand_SingleVM_JSONOutput(t *testing.T) {
	mb := &mockStatusBackend{
		name:      "mock",
		available: true,
		vms: map[string]backend.VMInfo{
			"testvm": {
				Name:    "testvm",
				Status:  backend.StatusRunning,
				Backend: "lima",
				CPUs:    4,
				Memory:  "8GiB",
				Disk:    "100GiB",
				IP:      "192.168.5.15",
			},
		},
	}
	setupStatusTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "status", "testvm"})
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
	assert.Equal(t, "testvm", data["name"])
	assert.Equal(t, "running", data["status"])
	assert.Equal(t, "lima", data["backend"])
	assert.Equal(t, "192.168.5.15", data["ip"])
}

func TestStatusCommand_AllVMs_HumanOutput(t *testing.T) {
	mb := &mockStatusBackend{
		name:      "mock",
		available: true,
		vms: map[string]backend.VMInfo{
			"vm1": {Name: "vm1", Status: backend.StatusRunning, Backend: "lima", CPUs: 4, Memory: "8GiB", Disk: "100GiB"},
			"vm2": {Name: "vm2", Status: backend.StatusStopped, Backend: "lima", CPUs: 2, Memory: "4GiB", Disk: "50GiB"},
		},
	}
	setupStatusTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"status"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	output := buf.String()
	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "STATUS")
	assert.Contains(t, output, "vm1")
	assert.Contains(t, output, "vm2")
	assert.Contains(t, output, "running")
	assert.Contains(t, output, "stopped")
}

func TestStatusCommand_AllVMs_JSONOutput(t *testing.T) {
	mb := &mockStatusBackend{
		name:      "mock",
		available: true,
		vms: map[string]backend.VMInfo{
			"vm1": {Name: "vm1", Status: backend.StatusRunning, Backend: "lima", CPUs: 4, Memory: "8GiB", Disk: "100GiB"},
			"vm2": {Name: "vm2", Status: backend.StatusStopped, Backend: "lima", CPUs: 2, Memory: "4GiB", Disk: "50GiB"},
		},
	}
	setupStatusTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "status"})
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
	data := result["data"].([]any)
	assert.Len(t, data, 2)

	// Check that each entry has name and status
	for _, entry := range data {
		vm := entry.(map[string]any)
		assert.Contains(t, vm, "name")
		assert.Contains(t, vm, "status")
	}
}

func TestStatusCommand_AllVMs_EmptyList(t *testing.T) {
	mb := &mockStatusBackend{
		name:      "mock",
		available: true,
		vms:       map[string]backend.VMInfo{},
	}
	setupStatusTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"status"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "No VMs found")
}

func TestStatusCommand_AllVMs_EmptyList_JSON(t *testing.T) {
	mb := &mockStatusBackend{
		name:      "mock",
		available: true,
		vms:       map[string]backend.VMInfo{},
	}
	setupStatusTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "status"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.True(t, result["ok"].(bool))
	data := result["data"].([]any)
	assert.Empty(t, data)
}

func TestStatusCommand_VMNotFound(t *testing.T) {
	mb := &mockStatusBackend{
		name:      "mock",
		available: true,
		vms:       map[string]backend.VMInfo{}, // no VMs
	}
	setupStatusTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"status", "nonexistent"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
	assert.Contains(t, cliErr.Message, "nonexistent")
}

func TestStatusCommand_BackendUnavailable(t *testing.T) {
	mb := &mockStatusBackend{name: "mock", available: false}
	setupStatusTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"status", "testvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestStatusCommand_BackendGetError(t *testing.T) {
	newRootTestEnv(t)

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return nil, fmt.Errorf("no such backend")
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	root := RootCmd()
	root.SetArgs([]string{"status", "testvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestStatusCommand_BackendUnavailableForAll(t *testing.T) {
	mb := &mockStatusBackend{name: "mock", available: false}
	setupStatusTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"status"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestStatusCommand_ListError(t *testing.T) {
	mb := &mockStatusBackend{
		name:      "mock",
		available: true,
		listErr:   fmt.Errorf("connection refused"),
	}
	setupStatusTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"status"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
	assert.Contains(t, cliErr.Message, "connection refused")
}

func TestStatusCommand_StatusError(t *testing.T) {
	mb := &mockStatusBackend{
		name:       "mock",
		available:  true,
		statusErr:  fmt.Errorf("connection refused"),
	}
	setupStatusTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"status", "testvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
	assert.Contains(t, cliErr.Message, "connection refused")
}

func TestStatusCommand_StoppedVM(t *testing.T) {
	mb := &mockStatusBackend{
		name:      "mock",
		available: true,
		vms: map[string]backend.VMInfo{
			"stoppedvm": {
				Name:    "stoppedvm",
				Status:  backend.StatusStopped,
				Backend: "lima",
				CPUs:    2,
				Memory:  "4GiB",
				Disk:    "50GiB",
			},
		},
	}
	setupStatusTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "status", "stoppedvm"})
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
	assert.Equal(t, "stopped", data["status"])
}

func TestStatusCommand_ErrorStatusVM(t *testing.T) {
	mb := &mockStatusBackend{
		name:      "mock",
		available: true,
		vms: map[string]backend.VMInfo{
			"errorvm": {
				Name:    "errorvm",
				Status:  backend.StatusError,
				Backend: "lima",
				CPUs:    4,
				Memory:  "8GiB",
				Disk:    "100GiB",
			},
		},
	}
	setupStatusTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "status", "errorvm"})
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
	assert.Equal(t, "error", data["status"])
}

// --- Format helper tests ---

func TestFormatVMDetail_FullInfo(t *testing.T) {
	vm := backend.VMInfo{
		Name:      "testvm",
		Status:    backend.StatusRunning,
		Backend:   "lima",
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		IP:        "192.168.5.15",
		CreatedAt: time.Date(2026, 3, 29, 10, 0, 0, 0, time.UTC),
	}
	output := formatVMDetail(vm)
	assert.Contains(t, output, "testvm")
	assert.Contains(t, output, "running")
	assert.Contains(t, output, "lima")
	assert.Contains(t, output, "192.168.5.15")
	assert.Contains(t, output, "8GiB")
	assert.Contains(t, output, "100GiB")
	assert.Contains(t, output, "2026")
}

func TestFormatVMDetail_NoIP(t *testing.T) {
	vm := backend.VMInfo{
		Name:   "testvm",
		Status: backend.StatusStopped,
		Backend: "lima",
		CPUs:   2,
		Memory: "4GiB",
		Disk:   "50GiB",
	}
	output := formatVMDetail(vm)
	assert.Contains(t, output, "testvm")
	assert.NotContains(t, output, "IP:")
}

func TestFormatStatusTable_MultipleVMs(t *testing.T) {
	vms := []backend.VMInfo{
		{Name: "vm1", Status: backend.StatusRunning},
		{Name: "vm2", Status: backend.StatusStopped},
	}
	output := formatStatusTable(vms)
	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "STATUS")
	assert.Contains(t, output, "vm1")
	assert.Contains(t, output, "vm2")
}

func TestFormatStatusTable_Empty(t *testing.T) {
	output := formatStatusTable(nil)
	assert.Contains(t, output, "No VMs found")
}

// --- Property-based tests ---

// Property: JSON output from status (single VM) always has ok=true, data.name, data.status.
func TestProperty_StatusSingleJSONAlwaysValid(t *testing.T) {
	cases := []struct {
		name   string
		status backend.VMStatus
	}{
		{"vm-1", backend.StatusRunning},
		{"my-vm", backend.StatusStopped},
		{"test", backend.StatusError},
		{"a", backend.StatusCreating},
		{"production-vm-2024", backend.StatusRunning},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mb := &mockStatusBackend{
				name:      "mock",
				available: true,
				vms: map[string]backend.VMInfo{
					tc.name: {
						Name:    tc.name,
						Status:  tc.status,
						Backend: "lima",
						CPUs:    4,
						Memory:  "8GiB",
						Disk:    "100GiB",
					},
				},
			}
			setupStatusTest(t, mb)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"--json", "status", tc.name})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)

			var result map[string]any
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err, "JSON must parse for name %q: %s", tc.name, buf.String())
			assert.True(t, result["ok"].(bool), "ok must be true for %q", tc.name)

			data := result["data"].(map[string]any)
			assert.Equal(t, tc.name, data["name"], "data.name must match for %q", tc.name)
			assert.Equal(t, string(tc.status), data["status"], "data.status must match for %q", tc.name)
		})
	}
}

// Property: human output always mentions the VM name.
func TestProperty_StatusHumanContainsName(t *testing.T) {
	names := []string{"alpha", "beta", "gamma", "vm-with-dash", "vm-with-mixed-1"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := &mockStatusBackend{
				name:      "mock",
				available: true,
				vms: map[string]backend.VMInfo{
					name: {
						Name:    name,
						Status:  backend.StatusRunning,
						Backend: "lima",
						CPUs:    4,
						Memory:  "8GiB",
						Disk:    "100GiB",
					},
				},
			}
			setupStatusTest(t, mb)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"status", name})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)
			assert.Contains(t, buf.String(), name, "human output must contain VM name %q", name)
		})
	}
}

// Property: error codes are always snake_case.
func TestProperty_StatusErrorCodesSnakeCase(t *testing.T) {
	t.Run("vm_not_found", func(t *testing.T) {
		mb := &mockStatusBackend{
			name:      "mock",
			available: true,
			vms:       map[string]backend.VMInfo{},
		}
		setupStatusTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"status", "test"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "vm_not_found", cliErr.Code)
	})

	t.Run("backend_unavailable", func(t *testing.T) {
		mb := &mockStatusBackend{name: "mock", available: false}
		setupStatusTest(t, mb)

		root := RootCmd()
		root.SetArgs([]string{"status", "test"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "backend_unavailable", cliErr.Code)
	})
}

// Property: all-VMs JSON output always has ok=true and data is an array.
func TestProperty_StatusAllJSONAlwaysValid(t *testing.T) {
	cases := []struct {
		name string
		vms  map[string]backend.VMInfo
	}{
		{"empty", nil},
		{"single", map[string]backend.VMInfo{
			"vm1": {Name: "vm1", Status: backend.StatusRunning, Backend: "lima"},
		}},
		{"multiple", map[string]backend.VMInfo{
			"vm1": {Name: "vm1", Status: backend.StatusRunning, Backend: "lima"},
			"vm2": {Name: "vm2", Status: backend.StatusStopped, Backend: "lima"},
			"vm3": {Name: "vm3", Status: backend.StatusError, Backend: "lima"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mb := &mockStatusBackend{
				name:      "mock",
				available: true,
				vms:       tc.vms,
			}
			setupStatusTest(t, mb)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"--json", "status"})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)

			var result map[string]any
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err, "JSON must parse for case %q: %s", tc.name, buf.String())
			assert.True(t, result["ok"].(bool), "ok must be true for %q", tc.name)

			data, ok := result["data"].([]any)
			require.True(t, ok, "data must be an array for %q", tc.name)

			// Each entry must have name and status
			for _, entry := range data {
				vm := entry.(map[string]any)
				assert.Contains(t, vm, "name", "each entry must have 'name'")
				assert.Contains(t, vm, "status", "each entry must have 'status'")
			}
		})
	}
}

// Property: all-VMs human output always has header row.
func TestProperty_StatusAllHumanHasHeader(t *testing.T) {
	mb := &mockStatusBackend{
		name:      "mock",
		available: true,
		vms: map[string]backend.VMInfo{
			"vm1": {Name: "vm1", Status: backend.StatusRunning},
		},
	}
	setupStatusTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"status"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	output := buf.String()
	assert.Contains(t, output, "NAME", "human output must have NAME header")
	assert.Contains(t, output, "STATUS", "human output must have STATUS header")
}

// Property: status all-VMs table contains all VM names.
func TestProperty_StatusAllTableContainsAllNames(t *testing.T) {
	vms := []backend.VMInfo{
		{Name: "alpha", Status: backend.StatusRunning},
		{Name: "beta", Status: backend.StatusStopped},
		{Name: "gamma", Status: backend.StatusError},
	}
	output := formatStatusTable(vms)
	for _, vm := range vms {
		assert.Contains(t, output, vm.Name, "table must contain VM name %q", vm.Name)
	}
}

// Property: JSON required fields for single VM status.
func TestProperty_StatusSingleJSONRequiredFields(t *testing.T) {
	mb := &mockStatusBackend{
		name:      "mock",
		available: true,
		vms: map[string]backend.VMInfo{
			"myvm": {
				Name:    "myvm",
				Status:  backend.StatusRunning,
				Backend: "lima",
				CPUs:    4,
				Memory:  "8GiB",
				Disk:    "100GiB",
			},
		},
	}
	setupStatusTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "status", "myvm"})
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
	assert.Contains(t, data, "name", "data must have 'name' field")
	assert.Contains(t, data, "status", "data must have 'status' field")
}

// Property: dual-mode consistency - JSON and human both contain VM name.
func TestProperty_StatusDualModeConsistency(t *testing.T) {
	name := "dual-test-vm"
	mb := &mockStatusBackend{
		name:      "mock",
		available: true,
		vms: map[string]backend.VMInfo{
			name: {
				Name:    name,
				Status:  backend.StatusRunning,
				Backend: "lima",
				CPUs:    4,
				Memory:  "8GiB",
				Disk:    "100GiB",
			},
		},
	}

	// JSON mode
	t.Run("json_has_name", func(t *testing.T) {
		setupStatusTest(t, mb)

		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		require.NoError(t, err)
		os.Stdout = w

		root := RootCmd()
		root.SetArgs([]string{"--json", "status", name})
		execErr := root.Execute()

		w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)

		require.NoError(t, execErr)
		assert.Contains(t, buf.String(), name)
	})

	// Human mode
	t.Run("human_has_name", func(t *testing.T) {
		setupStatusTest(t, mb)

		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		require.NoError(t, err)
		os.Stdout = w

		root := RootCmd()
		root.SetArgs([]string{"status", name})
		execErr := root.Execute()

		w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)

		require.NoError(t, execErr)
		assert.Contains(t, buf.String(), name)
	})
}
