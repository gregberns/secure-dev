// Package cmd provides tests for the list command.
// REQ-002-003: VM Management Commands -- list
// REQ-002-009: Command Aliases (list -> ls)
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/backend"
	"sd/internal/ui"
)

// mockListBackend is a digital twin of a backend for list command testing.
// It implements backend.Backend with configurable List behavior.
type mockListBackend struct {
	vms []backend.VMInfo
	err error
}

func (m *mockListBackend) Name() string { return "mock" }
func (m *mockListBackend) Available() error { return nil }
func (m *mockListBackend) Create(_ context.Context, _ string, _ backend.VMConfig) error {
	return nil
}
func (m *mockListBackend) Start(_ context.Context, _ string) error  { return nil }
func (m *mockListBackend) Stop(_ context.Context, _ string) error   { return nil }
func (m *mockListBackend) Destroy(_ context.Context, _ string) error { return nil }
func (m *mockListBackend) Status(_ context.Context, _ string) (backend.VMStatus, error) {
	return "", nil
}
func (m *mockListBackend) List(_ context.Context) ([]backend.VMInfo, error) {
	return m.vms, m.err
}
func (m *mockListBackend) SSHConfig(_ context.Context, _ string) (backend.SSHConfig, error) {
	return backend.SSHConfig{}, nil
}
func (m *mockListBackend) Exec(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
	return backend.ExecResult{}, nil
}

// setupListTest configures the test environment and overrides getBackendFunc.
// Returns a restore function that MUST be called (via defer) to clean up.
func setupListTest(t *testing.T, mb *mockListBackend) {
	t.Helper()
	newRootTestEnv(t)
	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })
}

func TestListCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "list" {
			found = true
			assert.Equal(t, "vm", cmd.GroupID)
			assert.Contains(t, cmd.Aliases, "ls", "list must have 'ls' alias")
			break
		}
	}
	assert.True(t, found, "list command must be registered")
}

func TestListCommand_AliasLs(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "list" {
			found = true
			assert.Contains(t, cmd.Aliases, "ls")
			break
		}
	}
	require.True(t, found)

	// Verify ls resolves to the list command via alias
	listCmd, _, err := root.Find([]string{"ls"})
	require.NoError(t, err)
	assert.Equal(t, "list", listCmd.Name())
}

func TestListCommand_EmptyList_HumanOutput(t *testing.T) {
	setupListTest(t, &mockListBackend{vms: nil})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"list"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	require.NoError(t, execErr)
	assert.Contains(t, output, "No VMs found")
}

func TestListCommand_EmptyList_JSONOutput(t *testing.T) {
	setupListTest(t, &mockListBackend{vms: nil})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "list"})
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
	data, ok := result["data"].([]any)
	require.True(t, ok, "data must be a JSON array")
	assert.Empty(t, data)
}

func TestListCommand_MultipleVMs_HumanOutput(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	vms := []backend.VMInfo{
		{
			Name: "vm-alpha", Status: backend.StatusRunning, Backend: "lima",
			CPUs: 4, Memory: "8GiB", Disk: "100GiB", IP: "192.168.5.15", CreatedAt: now,
		},
		{
			Name: "vm-beta", Status: backend.StatusStopped, Backend: "lima",
			CPUs: 2, Memory: "4GiB", Disk: "50GiB", IP: "", CreatedAt: now.Add(-time.Hour),
		},
	}
	setupListTest(t, &mockListBackend{vms: vms})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"list"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	require.NoError(t, execErr)

	// Verify table headers
	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "STATUS")
	assert.Contains(t, output, "BACKEND")
	assert.Contains(t, output, "CPUS")
	assert.Contains(t, output, "MEMORY")
	assert.Contains(t, output, "DISK")
	assert.Contains(t, output, "IP")

	// Verify VM data
	assert.Contains(t, output, "vm-alpha")
	assert.Contains(t, output, "running")
	assert.Contains(t, output, "vm-beta")
	assert.Contains(t, output, "stopped")
	assert.Contains(t, output, "192.168.5.15")
	assert.Contains(t, output, "8GiB")
	assert.Contains(t, output, "4GiB")
}

func TestListCommand_MultipleVMs_JSONOutput(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	vms := []backend.VMInfo{
		{
			Name: "vm-alpha", Status: backend.StatusRunning, Backend: "lima",
			CPUs: 4, Memory: "8GiB", Disk: "100GiB", IP: "192.168.5.15", CreatedAt: now,
		},
		{
			Name: "vm-beta", Status: backend.StatusStopped, Backend: "lima",
			CPUs: 2, Memory: "4GiB", Disk: "50GiB", IP: "", CreatedAt: now.Add(-time.Hour),
		},
	}
	setupListTest(t, &mockListBackend{vms: vms})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "list"})
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
	data, ok := result["data"].([]any)
	require.True(t, ok, "data must be a JSON array")
	require.Len(t, data, 2)

	// Verify first VM
	vm0 := data[0].(map[string]any)
	assert.Equal(t, "vm-alpha", vm0["name"])
	assert.Equal(t, "running", vm0["status"])
	assert.Equal(t, "lima", vm0["backend"])
	assert.Equal(t, float64(4), vm0["cpus"])
	assert.Equal(t, "8GiB", vm0["memory"])
	assert.Equal(t, "100GiB", vm0["disk"])
	assert.Equal(t, "192.168.5.15", vm0["ip"])

	// Verify second VM (no IP)
	vm1 := data[1].(map[string]any)
	assert.Equal(t, "vm-beta", vm1["name"])
	assert.Equal(t, "stopped", vm1["status"])
	_, hasIP := vm1["ip"]
	assert.False(t, hasIP, "stopped VM without IP should omit ip field")
}

func TestListCommand_BackendError(t *testing.T) {
	setupListTest(t, &mockListBackend{
		err: fmt.Errorf("limactl not found"),
	})

	root := RootCmd()
	root.SetArgs([]string{"list"})
	err := root.Execute()

	assert.Error(t, err)
}

func TestListCommand_BackendUnavailable(t *testing.T) {
	newRootTestEnv(t)
	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return nil, fmt.Errorf("no backends registered")
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })

	root := RootCmd()
	root.SetArgs([]string{"list"})
	err := root.Execute()

	assert.Error(t, err)
}

func TestListCommand_NoConfigRequired(t *testing.T) {
	// REQ-002-015: list must succeed even without a config file
	tmpDir := t.TempDir()
	t.Setenv("SD_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)

	setupListTest(t, &mockListBackend{vms: nil})

	root := RootCmd()
	root.SetArgs([]string{"list"})
	err := root.Execute()
	assert.NoError(t, err, "list must succeed without config file")
}

func TestFormatVMTable_Empty(t *testing.T) {
	output := formatVMTable(nil)
	assert.Equal(t, "No VMs found.\n", output)
}

func TestFormatVMTable_SingleVM(t *testing.T) {
	vms := []backend.VMInfo{
		{
			Name: "test-vm", Status: backend.StatusRunning, Backend: "lima",
			CPUs: 4, Memory: "8GiB", Disk: "100GiB", IP: "10.0.0.1",
		},
	}
	output := formatVMTable(vms)
	assert.Contains(t, output, "test-vm")
	assert.Contains(t, output, "running")
	assert.Contains(t, output, "10.0.0.1")
}

func TestFormatVMTable_NoIP(t *testing.T) {
	vms := []backend.VMInfo{
		{
			Name: "stopped-vm", Status: backend.StatusStopped, Backend: "lima",
			CPUs: 2, Memory: "4GiB", Disk: "50GiB",
		},
	}
	output := formatVMTable(vms)
	assert.Contains(t, output, "stopped-vm")
	// IP column should show "-" for VMs without an IP
	lines := strings.Split(output, "\n")
	require.Len(t, lines, 3, "expected header + 1 VM + trailing newline")
	assert.Contains(t, lines[1], "-")
}

// Property: JSON output from list command always contains the ok field
// and data as an array, regardless of VM count.
func TestProperty_ListJSONAlwaysValid(t *testing.T) {
	cases := []struct {
		name string
		vms  []backend.VMInfo
	}{
		{"empty", nil},
		{"single", []backend.VMInfo{
			{Name: "a", Status: backend.StatusRunning, Backend: "lima", CPUs: 1, Memory: "1GiB", Disk: "10GiB"},
		}},
		{"many", []backend.VMInfo{
			{Name: "a", Status: backend.StatusRunning, Backend: "lima", CPUs: 1, Memory: "1GiB", Disk: "10GiB"},
			{Name: "b", Status: backend.StatusStopped, Backend: "lima", CPUs: 2, Memory: "2GiB", Disk: "20GiB"},
			{Name: "c", Status: backend.StatusError, Backend: "lima", CPUs: 8, Memory: "16GiB", Disk: "200GiB"},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupListTest(t, &mockListBackend{vms: tc.vms})

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"--json", "list"})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)

			var result map[string]any
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err, "JSON must always parse for case %s: %s", tc.name, buf.String())
			assert.True(t, result["ok"].(bool), "ok must be true for case %s", tc.name)

			data, ok := result["data"].([]any)
			require.True(t, ok, "data must be an array for case %s", tc.name)
			assert.Len(t, data, len(tc.vms), "data length must match VM count for case %s", tc.name)

			// Verify each VM has required fields
			for i, vm := range data {
				vmMap := vm.(map[string]any)
				for _, field := range []string{"name", "status", "backend", "cpus", "memory", "disk"} {
					_, exists := vmMap[field]
					assert.True(t, exists, "VM[%d] must have field %s in case %s", i, field, tc.name)
				}
			}
		})
	}
}

// Property: Human output always has a header row when VMs exist.
func TestProperty_HumanOutputAlwaysHasHeader(t *testing.T) {
	vms := []backend.VMInfo{
		{Name: "x", Status: backend.StatusRunning, Backend: "lima", CPUs: 1, Memory: "1GiB", Disk: "10GiB"},
	}

	output := formatVMTable(vms)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	require.GreaterOrEqual(t, len(lines), 2, "must have header + at least one data row")
	assert.Contains(t, lines[0], "NAME")
	assert.Contains(t, lines[0], "STATUS")
}

// Property: formatVMTable output always contains the VM name for each VM.
func TestProperty_TableContainsAllVMNames(t *testing.T) {
	vms := []backend.VMInfo{
		{Name: "alpha", Status: backend.StatusRunning, Backend: "lima", CPUs: 1, Memory: "1GiB", Disk: "10GiB"},
		{Name: "beta", Status: backend.StatusStopped, Backend: "lima", CPUs: 2, Memory: "2GiB", Disk: "20GiB"},
		{Name: "gamma", Status: backend.StatusError, Backend: "lima", CPUs: 4, Memory: "4GiB", Disk: "40GiB"},
	}

	output := formatVMTable(vms)
	for _, vm := range vms {
		assert.Contains(t, output, vm.Name, "table must contain VM name %s", vm.Name)
	}
}

// Property: JSON and human output both succeed for the same data (dual-mode consistency).
func TestProperty_DualModeConsistency(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	vms := []backend.VMInfo{
		{Name: "test", Status: backend.StatusRunning, Backend: "lima",
			CPUs: 4, Memory: "8GiB", Disk: "100GiB", IP: "10.0.0.1", CreatedAt: now},
	}

	for _, jsonMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("json=%v", jsonMode), func(t *testing.T) {
			setupListTest(t, &mockListBackend{vms: vms})

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			args := []string{"list"}
			if jsonMode {
				args = []string{"--json", "list"}
			}
			root.SetArgs(args)
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)
			assert.NotEmpty(t, buf.String(), "output must not be empty (json=%v)", jsonMode)

			if jsonMode {
				var result map[string]any
				err = json.Unmarshal(buf.Bytes(), &result)
				require.NoError(t, err, "JSON mode must produce valid JSON")
			}
		})
	}
}

// Benchmark: list command with many VMs.
func BenchmarkFormatVMTable(b *testing.B) {
	vms := make([]backend.VMInfo, 100)
	for i := range vms {
		vms[i] = backend.VMInfo{
			Name: fmt.Sprintf("vm-%03d", i), Status: backend.StatusRunning, Backend: "lima",
			CPUs: 4, Memory: "8GiB", Disk: "100GiB", IP: fmt.Sprintf("10.0.0.%d", i+1),
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		formatVMTable(vms)
	}
}

// Benchmark: JSON output with many VMs.
func BenchmarkListJSON(b *testing.B) {
	vms := make([]backend.VMInfo, 100)
	for i := range vms {
		vms[i] = backend.VMInfo{
			Name: fmt.Sprintf("vm-%03d", i), Status: backend.StatusRunning, Backend: "lima",
			CPUs: 4, Memory: "8GiB", Disk: "100GiB", IP: fmt.Sprintf("10.0.0.%d", i+1),
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		f := ui.NewFormatterWithWriters(true, &buf, ioDiscarder{})
		f.SuccessData(vms, func() string { return "" })
	}
}
