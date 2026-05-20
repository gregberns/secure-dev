// Package cmd provides tests for the list command.
// REQ-002-003: VM Management Commands -- list
// REQ-002-009: Command Aliases (list -> ls)
// NOTE: Tests use global getBackendFunc — do not use t.Parallel().
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
	"sd/internal/backend/memory"
	"sd/internal/config"
	"sd/internal/ui"
)

// setupListTest configures the test environment with a memory backend.
func setupListTest(t *testing.T) *memory.Backend {
	t.Helper()
	newRootTestEnv(t)

	mb := memory.New()
	origGetBackend := getBackendFunc
	origAllBackendNames := allBackendNames
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return mb, nil
	}
	allBackendNames = func() []string { return []string{"memory"} }
	t.Cleanup(func() {
		getBackendFunc = origGetBackend
		allBackendNames = origAllBackendNames
		mb.Reset()
	})
	return mb
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
	setupListTest(t)
	// No VMs created

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
	setupListTest(t)
	// No VMs created

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
	mb := setupListTest(t)
	ctx := context.Background()

	// Create VMs with specific configs
	require.NoError(t, mb.Create(ctx, "vm-alpha", backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB",
	}))
	require.NoError(t, mb.Create(ctx, "vm-beta", backend.VMConfig{
		CPUs: 2, Memory: "4GiB", Disk: "50GiB",
	}))
	// Stop vm-beta to put it in Stopped state
	require.NoError(t, mb.Stop(ctx, "vm-beta"))

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
	assert.Contains(t, output, "8GiB")
	assert.Contains(t, output, "4GiB")
}

func TestListCommand_MultipleVMs_JSONOutput(t *testing.T) {
	mb := setupListTest(t)
	ctx := context.Background()

	// Create VMs with specific configs
	require.NoError(t, mb.Create(ctx, "vm-alpha", backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB",
	}))
	require.NoError(t, mb.Create(ctx, "vm-beta", backend.VMConfig{
		CPUs: 2, Memory: "4GiB", Disk: "50GiB",
	}))
	// Stop vm-beta to put it in Stopped state
	require.NoError(t, mb.Stop(ctx, "vm-beta"))

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

	// Memory backend sorts by name, so vm-alpha is first, vm-beta second
	vm0 := data[0].(map[string]any)
	assert.Equal(t, "vm-alpha", vm0["name"])
	assert.Equal(t, "running", vm0["status"])
	assert.Equal(t, "memory", vm0["backend"])
	assert.Equal(t, float64(4), vm0["cpus"])
	assert.Equal(t, "8GiB", vm0["memory"])
	assert.Equal(t, "100GiB", vm0["disk"])
	// IP is auto-assigned by memory backend
	assert.NotEmpty(t, vm0["ip"], "running VM must have an IP")

	// Verify second VM
	vm1 := data[1].(map[string]any)
	assert.Equal(t, "vm-beta", vm1["name"])
	assert.Equal(t, "stopped", vm1["status"])
}

func TestListCommand_BackendError(t *testing.T) {
	mb := setupListTest(t)
	mb.SetMethodError("list", fmt.Errorf("limactl not found"))

	// With multi-backend listing, a failing backend is skipped with a warning
	// rather than causing the entire command to fail.
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

	assert.NoError(t, execErr, "list should succeed even when a backend fails")
	assert.Contains(t, buf.String(), "No VMs found")
}

func TestListCommand_BackendUnavailable(t *testing.T) {
	newRootTestEnv(t)
	origGetBackend := getBackendFunc
	origAllBackendNames := allBackendNames
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return nil, fmt.Errorf("no backends registered")
	}
	allBackendNames = func() []string { return []string{"missing"} }
	t.Cleanup(func() {
		getBackendFunc = origGetBackend
		allBackendNames = origAllBackendNames
	})

	// With multi-backend listing, unavailable backends are skipped.
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

	assert.NoError(t, execErr, "list should succeed even when no backends are available")
	assert.Contains(t, buf.String(), "No VMs found")
}

func TestListCommand_NoConfigRequired(t *testing.T) {
	// REQ-002-015: list must succeed even without a config file
	tmpDir := t.TempDir()
	t.Setenv("SD_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)

	setupListTest(t)
	// No VMs created

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
		name   string
		vmDefs []struct {
			name   string
			config backend.VMConfig
			status backend.VMStatus
		}
	}{
		{"empty", nil},
		{"single", []struct {
			name   string
			config backend.VMConfig
			status backend.VMStatus
		}{
			{"a", backend.VMConfig{CPUs: 1, Memory: "1GiB", Disk: "10GiB"}, backend.StatusRunning},
		}},
		{"many", []struct {
			name   string
			config backend.VMConfig
			status backend.VMStatus
		}{
			{"a", backend.VMConfig{CPUs: 1, Memory: "1GiB", Disk: "10GiB"}, backend.StatusRunning},
			{"b", backend.VMConfig{CPUs: 2, Memory: "2GiB", Disk: "20GiB"}, backend.StatusStopped},
			{"c", backend.VMConfig{CPUs: 8, Memory: "16GiB", Disk: "200GiB"}, backend.StatusError},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mb := setupListTest(t)
			ctx := context.Background()

			for _, vm := range tc.vmDefs {
				require.NoError(t, mb.Create(ctx, vm.name, vm.config))
				if vm.status != backend.StatusRunning {
					require.NoError(t, mb.SetStatus(vm.name, vm.status))
				}
			}

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
			assert.Len(t, data, len(tc.vmDefs), "data length must match VM count for case %s", tc.name)

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
	for _, jsonMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("json=%v", jsonMode), func(t *testing.T) {
			mb := setupListTest(t)
			ctx := context.Background()

			require.NoError(t, mb.Create(ctx, "test", backend.VMConfig{
				CPUs: 4, Memory: "8GiB", Disk: "100GiB",
			}))

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

// TestListCommand_MultipleBackends verifies that list queries all registered
// backends and merges results.
func TestListCommand_MultipleBackends(t *testing.T) {
	newRootTestEnv(t)

	mb1 := memory.New()
	mb2 := memory.New()

	ctx := context.Background()
	require.NoError(t, mb1.Create(ctx, "lima-vm", backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB",
	}))
	require.NoError(t, mb2.Create(ctx, "docker-vm", backend.VMConfig{
		CPUs: 2, Memory: "4GiB", Disk: "50GiB",
	}))

	origGetBackend := getBackendFunc
	origAllBackendNames := allBackendNames
	getBackendFunc = func(name string) (backend.Backend, error) {
		switch name {
		case "backend-a":
			return mb1, nil
		case "backend-b":
			return mb2, nil
		}
		return nil, fmt.Errorf("unknown backend %q", name)
	}
	allBackendNames = func() []string { return []string{"backend-a", "backend-b"} }
	t.Cleanup(func() {
		getBackendFunc = origGetBackend
		allBackendNames = origAllBackendNames
		mb1.Reset()
		mb2.Reset()
	})

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
	require.Len(t, data, 2, "should see VMs from both backends")

	// Verify both VMs are present
	names := make(map[string]bool)
	for _, vm := range data {
		vmMap := vm.(map[string]any)
		names[vmMap["name"].(string)] = true
	}
	assert.True(t, names["lima-vm"], "should contain lima-vm from backend-a")
	assert.True(t, names["docker-vm"], "should contain docker-vm from backend-b")
}

// TestResolveBackendName_PerVMConfig verifies that resolveBackendName reads the
// per-VM config and returns the stored backend name.
func TestResolveBackendName_PerVMConfig(t *testing.T) {
	tmpDir := newRootTestEnv(t)

	// Create a loader pointing at our temp dir and write a VM config
	l := config.NewLoader(config.WithSDHome(tmpDir))
	require.NoError(t, l.Load())
	require.NoError(t, l.WriteVMConfig(&config.VMConfig{
		Name:    "docker-vm",
		Backend: "docker",
		CPUs:    2,
		Memory:  "4GiB",
		Disk:    "50GiB",
	}))

	// Set the global loader so resolveBackendName can find it
	oldLoader := loader
	loader = l
	t.Cleanup(func() { loader = oldLoader })

	// Verify that resolveBackendName reads the per-VM config
	assert.Equal(t, "docker", resolveBackendName("docker-vm"))
}

// TestResolveBackendName_FallbackToGlobalDefault verifies that resolveBackendName
// falls back to the global default when no per-VM config exists.
func TestResolveBackendName_FallbackToGlobalDefault(t *testing.T) {
	tmpDir := newRootTestEnv(t)

	l := config.NewLoader(config.WithSDHome(tmpDir))
	require.NoError(t, l.Load())

	oldLoader := loader
	loader = l
	t.Cleanup(func() { loader = oldLoader })

	// No VM config written - should fall back to global default ("lima")
	assert.Equal(t, "lima", resolveBackendName("nonexistent-vm"))
}

// TestResolveBackendName_NoLoader verifies that resolveBackendName returns "lima"
// when no config loader is available.
func TestResolveBackendName_NoLoader(t *testing.T) {
	oldLoader := loader
	loader = nil
	t.Cleanup(func() { loader = oldLoader })

	assert.Equal(t, "lima", resolveBackendName("any-vm"))
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

// Q2 / bug-no-prune-command: list must emit an actionable hint when orphan
// state directories exist, pointing the user at `sd prune` to clean them.
func TestList_OrphanHintPointsAtPrune(t *testing.T) {
	setupListTest(t)
	sdHome := os.Getenv("SD_HOME")
	require.NoError(t, os.MkdirAll(sdHome, 0o700))

	// Seed an orphan state directory not present in the memory backend.
	orphanDir := fmt.Sprintf("%s/vms/orphan-vm", sdHome)
	require.NoError(t, os.MkdirAll(orphanDir, 0o700))
	require.NoError(t, os.WriteFile(orphanDir+"/config.yaml", []byte("name: orphan-vm\n"), 0o600))

	// Capture stderr (progress hints go to stderr).
	oldStderr := os.Stderr
	rErr, wErr, perr := os.Pipe()
	require.NoError(t, perr)
	os.Stderr = wErr

	root := RootCmd()
	root.SetArgs([]string{"list"})
	require.NoError(t, root.Execute())

	wErr.Close()
	os.Stderr = oldStderr

	var stderrBuf bytes.Buffer
	_, _ = stderrBuf.ReadFrom(rErr)
	output := stderrBuf.String()
	assert.Contains(t, output, "sd prune", "orphan-hint must reference sd prune")
	assert.Contains(t, output, "orphan", "orphan-hint must call out orphan(s)")
}

// Q2 / bug-created-at-zero: when backend.List returns nil CreatedAt, the
// list command must overlay State.CreatedAt from the persisted config.
func TestList_CreatedAtOverlayFromState(t *testing.T) {
	mb := setupListTest(t)
	require.NoError(t, mb.Create(context.Background(), "alive", backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04",
	}))
	// Force backend CreatedAt to nil by writing the per-VM config separately
	// with a known timestamp, and clearing any backend timestamp via Reset.
	// Memory backend always sets CreatedAt; for the overlay path we need a
	// backend whose List() returns nil. Use a minimal overriding backend.
	overlayTime := "2026-04-15T12:34:56Z"

	// Persist a VM config with state.created_at set so list overlay picks it up.
	sdHome := os.Getenv("SD_HOME")
	vmDir := fmt.Sprintf("%s/vms/alive", sdHome)
	require.NoError(t, os.MkdirAll(vmDir, 0o700))
	cfgYAML := fmt.Sprintf("name: alive\nbackend: memory\ncpus: 4\nmemory: 8GiB\ndisk: 100GiB\nimage: ubuntu:24.04\nstate:\n  status: running\n  created_at: %s\n", overlayTime)
	require.NoError(t, os.WriteFile(vmDir+"/config.yaml", []byte(cfgYAML), 0o600))

	// Swap getBackendFunc to a wrapper that strips CreatedAt to nil.
	origGet := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return &nilCreatedAtBackend{Backend: mb}, nil
	}
	t.Cleanup(func() { getBackendFunc = origGet })

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	root := RootCmd()
	root.SetArgs([]string{"--json", "list"})
	require.NoError(t, root.Execute())
	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	var env struct {
		OK   bool              `json:"ok"`
		Data []backend.VMInfo  `json:"data"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env), "json: %s", buf.String())
	require.Len(t, env.Data, 1)
	require.NotNil(t, env.Data[0].CreatedAt, "list must overlay CreatedAt from state")
	assert.Equal(t, "2026-04-15", env.Data[0].CreatedAt.Format("2006-01-02"))
}

// nilCreatedAtBackend wraps a backend and forces VMInfo.CreatedAt to nil so
// the list-overlay code path is exercised.
type nilCreatedAtBackend struct {
	backend.Backend
}

func (n *nilCreatedAtBackend) List(ctx context.Context) ([]backend.VMInfo, error) {
	vms, err := n.Backend.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range vms {
		vms[i].CreatedAt = nil
	}
	return vms, nil
}
