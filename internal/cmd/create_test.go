// Package cmd provides tests for the create command.
// REQ-002-003: VM Management Commands -- create
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"unsafe"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/backend"
	"sd/internal/ui"
)

// resetSliceFlag resets a pflag slice-type flag's accumulated value.
// pflag slice types (StringArray, StringSlice) track an internal `changed`
// field that causes Set() to append rather than replace. We use unsafe
// to reset this unexported field so Set() replaces on the next call.
func resetSliceFlag(cmd *cobra.Command, name string) {
	f := cmd.Flags().Lookup(name)
	if f == nil {
		return
	}
	// Reset the internal changed field via unsafe (unexported, reflect can't set it)
	v := reflect.ValueOf(f.Value)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	changed := v.FieldByName("changed")
	if changed.IsValid() {
		// Use unsafe to write to the unexported field
		changedPtr := unsafe.Pointer(changed.UnsafeAddr())
		*(*bool)(changedPtr) = false
	}
	// Now Set will replace instead of append
	_ = f.Value.Set("")
	f.Changed = false
}

// mockCreateBackend is a digital twin of a backend for create command testing.
// It implements backend.Backend with configurable Create behavior and records calls.
type mockCreateBackend struct {
	name      string
	available bool
	created   []createCall
	err       error // error to return from Create
	sshCfg    backend.SSHConfig
}

type createCall struct {
	name string
	cfg  backend.VMConfig
}

func (m *mockCreateBackend) Name() string { return m.name }
func (m *mockCreateBackend) Available() error {
	if m.available {
		return nil
	}
	return fmt.Errorf("backend not available")
}
func (m *mockCreateBackend) Create(_ context.Context, name string, cfg backend.VMConfig) error {
	if m.err != nil {
		return m.err
	}
	m.created = append(m.created, createCall{name: name, cfg: cfg})
	return nil
}
func (m *mockCreateBackend) Start(_ context.Context, _ string) error  { return nil }
func (m *mockCreateBackend) Stop(_ context.Context, _ string) error   { return nil }
func (m *mockCreateBackend) Destroy(_ context.Context, _ string) error { return nil }
func (m *mockCreateBackend) Status(_ context.Context, _ string) (backend.VMStatus, error) {
	return "", nil
}
func (m *mockCreateBackend) List(_ context.Context) ([]backend.VMInfo, error) {
	return nil, nil
}
func (m *mockCreateBackend) SSHConfig(_ context.Context, _ string) (backend.SSHConfig, error) {
	return m.sshCfg, nil
}
func (m *mockCreateBackend) Exec(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
	return backend.ExecResult{}, nil
}

// setupCreateTest configures the test environment with a mock backend.
// Returns the mock so tests can inspect recorded calls.
func setupCreateTest(t *testing.T, mb *mockCreateBackend) {
	t.Helper()
	newRootTestEnv(t)

	// Reset create command's local flags to prevent StringArray accumulation
	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "create" {
			resetSliceFlag(cmd, "mount")
			resetSliceFlag(cmd, "allow-egress")
			resetSliceFlag(cmd, "modules")
			_ = cmd.Flags().Set("backend", "")
			_ = cmd.Flags().Set("cpus", "0")
			_ = cmd.Flags().Set("memory", "")
			_ = cmd.Flags().Set("disk", "")
			break
		}
	}

	origGetBackend := getBackendFunc
	getBackendFunc = func(name string) (backend.Backend, error) {
		return mb, nil
	}
	t.Cleanup(func() { getBackendFunc = origGetBackend })
}

// --- Unit tests ---

func TestCreateCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "create" {
			found = true
			assert.Equal(t, "vm", cmd.GroupID)
			break
		}
	}
	assert.True(t, found, "create command must be registered")
}

func TestCreateCommand_ExactArgs(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	// Find create command
	cmd, _, err := root.Find([]string{"create"})
	require.NoError(t, err)
	assert.NotNil(t, cmd.Args, "create must have an Args validator")
	// Verify it rejects 0 args
	err = cmd.Args(cmd, nil)
	assert.Error(t, err, "create must reject zero args")
}

func TestCreateCommand_Flags(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"create"})
	require.NoError(t, err)

	// REQ-002-003: Required flags
	_, err = cmd.Flags().GetString("backend")
	assert.NoError(t, err, "must have --backend flag")

	_, err = cmd.Flags().GetInt("cpus")
	assert.NoError(t, err, "must have --cpus flag")

	_, err = cmd.Flags().GetString("memory")
	assert.NoError(t, err, "must have --memory flag")

	_, err = cmd.Flags().GetString("disk")
	assert.NoError(t, err, "must have --disk flag")

	_, err = cmd.Flags().GetStringSlice("modules")
	assert.NoError(t, err, "must have --modules flag")

	_, err = cmd.Flags().GetStringArray("mount")
	assert.NoError(t, err, "must have --mount flag")

	_, err = cmd.Flags().GetStringArray("allow-egress")
	assert.NoError(t, err, "must have --allow-egress flag")
}

func TestCreateCommand_BasicCreate_HumanOutput(t *testing.T) {
	mb := &mockCreateBackend{name: "mock", available: true}
	setupCreateTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "testvm")
	assert.Contains(t, buf.String(), "created")

	// Verify backend was called
	require.Len(t, mb.created, 1)
	assert.Equal(t, "testvm", mb.created[0].name)
	assert.Equal(t, "mock", mb.name)
}

func TestCreateCommand_BasicCreate_JSONOutput(t *testing.T) {
	mb := &mockCreateBackend{name: "mock", available: true}
	setupCreateTest(t, mb)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "create", "testvm"})
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
	// Backend name in result is the resolved name (from config/default), not the mock's Name()
	assert.Equal(t, "lima", data["backend"])
}

func TestCreateCommand_VMAlreadyExists(t *testing.T) {
	mb := &mockCreateBackend{
		name:      "mock",
		available: true,
		err:       backend.ErrVMAlreadyExists,
	}
	setupCreateTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"create", "dupvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok, "must be a CLIError")
	assert.Equal(t, "vm_already_exists", cliErr.Code)
	assert.Contains(t, cliErr.Message, "dupvm")
}

func TestCreateCommand_BackendUnavailable(t *testing.T) {
	mb := &mockCreateBackend{name: "mock", available: false}
	setupCreateTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestCreateCommand_NoConfigRequired(t *testing.T) {
	// REQ-002-015: create should attempt to load config but this is tested
	// via the "no config required" set. However, create DOES require config.
	// Here we just ensure it works when config dir exists (even if empty).
	mb := &mockCreateBackend{name: "mock", available: true}
	setupCreateTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"create", "myvm"})
	err := root.Execute()
	assert.NoError(t, err, "create should succeed with empty config dir when backend is available")
}

func TestCreateCommand_MissingName(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	root.SetArgs([]string{"create"})
	err := root.Execute()
	assert.Error(t, err, "create without name must fail")
}

// --- Flag propagation tests ---

func TestCreateCommand_CPUsFlag(t *testing.T) {
	mb := &mockCreateBackend{name: "mock", available: true}
	setupCreateTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--cpus", "8"})
	err := root.Execute()

	require.NoError(t, err)
	require.Len(t, mb.created, 1)
	assert.Equal(t, 8, mb.created[0].cfg.CPUs)
}

func TestCreateCommand_MemoryFlag(t *testing.T) {
	mb := &mockCreateBackend{name: "mock", available: true}
	setupCreateTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--memory", "16GiB"})
	err := root.Execute()

	require.NoError(t, err)
	require.Len(t, mb.created, 1)
	assert.Equal(t, "16GiB", mb.created[0].cfg.Memory)
}

func TestCreateCommand_DiskFlag(t *testing.T) {
	mb := &mockCreateBackend{name: "mock", available: true}
	setupCreateTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--disk", "200GiB"})
	err := root.Execute()

	require.NoError(t, err)
	require.Len(t, mb.created, 1)
	assert.Equal(t, "200GiB", mb.created[0].cfg.Disk)
}

func TestCreateCommand_BackendFlag(t *testing.T) {
	mb := &mockCreateBackend{name: "custom", available: true}
	setupCreateTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--backend", "custom"})
	err := root.Execute()

	require.NoError(t, err)
	require.Len(t, mb.created, 1)
	assert.Equal(t, "testvm", mb.created[0].name)
}

func TestCreateCommand_MountFlag(t *testing.T) {
	mb := &mockCreateBackend{name: "mock", available: true}
	setupCreateTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--mount", "/host/path:/guest/path"})
	err := root.Execute()

	require.NoError(t, err)
	require.Len(t, mb.created, 1)
	require.Len(t, mb.created[0].cfg.Mounts, 1)
	assert.Equal(t, "/host/path", mb.created[0].cfg.Mounts[0].HostPath)
	assert.Equal(t, "/guest/path", mb.created[0].cfg.Mounts[0].GuestPath)
	assert.False(t, mb.created[0].cfg.Mounts[0].Writable, "default mount must be read-only")
}

func TestCreateCommand_MountFlagReadWrite(t *testing.T) {
	mb := &mockCreateBackend{name: "mock", available: true}
	setupCreateTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--mount", "/host/path:/guest/path:rw"})
	err := root.Execute()

	require.NoError(t, err)
	require.Len(t, mb.created, 1)
	require.Len(t, mb.created[0].cfg.Mounts, 1)
	assert.True(t, mb.created[0].cfg.Mounts[0].Writable, "explicit :rw must be writable")
}

func TestCreateCommand_AllFlags(t *testing.T) {
	mb := &mockCreateBackend{name: "mock", available: true}
	setupCreateTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{
		"create", "myvm",
		"--backend", "lima",
		"--cpus", "8",
		"--memory", "16GiB",
		"--disk", "200GiB",
		"--modules", "claude-code,docker",
		"--mount", "~/project:/project",
		"--mount", "/data:/data:rw",
		"--allow-egress", "custom.example.com",
	})
	err := root.Execute()

	require.NoError(t, err)
	require.Len(t, mb.created, 1)
	call := mb.created[0]
	assert.Equal(t, "myvm", call.name)
	assert.Equal(t, 8, call.cfg.CPUs)
	assert.Equal(t, "16GiB", call.cfg.Memory)
	assert.Equal(t, "200GiB", call.cfg.Disk)
	require.Len(t, call.cfg.Mounts, 2)
	assert.Equal(t, "~/project", call.cfg.Mounts[0].HostPath)
	assert.Equal(t, "/project", call.cfg.Mounts[0].GuestPath)
	assert.False(t, call.cfg.Mounts[0].Writable)
	assert.Equal(t, "/data", call.cfg.Mounts[1].HostPath)
	assert.Equal(t, "/data", call.cfg.Mounts[1].GuestPath)
	assert.True(t, call.cfg.Mounts[1].Writable)
}

// --- Mount parsing unit tests ---

func TestParseMountSpec_TwoParts(t *testing.T) {
	m := parseMountSpec("/host:/guest")
	assert.Equal(t, "/host", m.HostPath)
	assert.Equal(t, "/guest", m.GuestPath)
	assert.False(t, m.Writable)
}

func TestParseMountSpec_ThreePartsRO(t *testing.T) {
	m := parseMountSpec("/host:/guest:ro")
	assert.Equal(t, "/host", m.HostPath)
	assert.Equal(t, "/guest", m.GuestPath)
	assert.False(t, m.Writable)
}

func TestParseMountSpec_ThreePartsRW(t *testing.T) {
	m := parseMountSpec("/host:/guest:rw")
	assert.Equal(t, "/host", m.HostPath)
	assert.Equal(t, "/guest", m.GuestPath)
	assert.True(t, m.Writable)
}

func TestParseMountSpec_OnePart(t *testing.T) {
	m := parseMountSpec("/hostonly")
	assert.Equal(t, "/hostonly", m.HostPath)
	assert.Equal(t, "", m.GuestPath)
	assert.False(t, m.Writable)
}

// --- Property-based tests ---

// Property: JSON output from create always contains ok=true and data.name matching the arg.
func TestProperty_CreateJSONAlwaysValid(t *testing.T) {
	names := []string{"vm-1", "my-vm", "test", "a", "production-vm-2024"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := &mockCreateBackend{name: "mock", available: true}
			setupCreateTest(t, mb)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"--json", "create", name})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)

			var result map[string]any
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err, "JSON must parse for name %q: %s", name, buf.String())
			assert.True(t, result["ok"].(bool), "ok must be true for %q", name)

			data := result["data"].(map[string]any)
			assert.Equal(t, name, data["name"], "data.name must match for %q", name)

			// Required fields in data
			for _, field := range []string{"name", "backend", "cpus", "memory", "disk"} {
				_, exists := data[field]
				assert.True(t, exists, "data must have field %q for %q", field, name)
			}
		})
	}
}

// Property: human output always mentions the VM name.
func TestProperty_HumanOutputContainsName(t *testing.T) {
	names := []string{"alpha", "beta", "gamma", "vm-with-dash", "vm-with-mixed-1"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb := &mockCreateBackend{name: "mock", available: true}
			setupCreateTest(t, mb)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"create", name})
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

// Property: default values are applied when flags are not provided.
func TestProperty_DefaultsApplied(t *testing.T) {
	mb := &mockCreateBackend{name: "mock", available: true}
	setupCreateTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"create", "defvm"})
	err := root.Execute()

	require.NoError(t, err)
	require.Len(t, mb.created, 1)
	cfg := mb.created[0].cfg

	// From config/defaults.go
	assert.Equal(t, 4, cfg.CPUs, "default CPUs must be 4")
	assert.Equal(t, "8GiB", cfg.Memory, "default memory must be 8GiB")
	assert.Equal(t, "100GiB", cfg.Disk, "default disk must be 100GiB")
	assert.Equal(t, "ubuntu:24.04", cfg.BaseImage, "default image must be ubuntu:24.04")
}

// Property: CLI flags always override config defaults.
func TestProperty_FlagsOverrideDefaults(t *testing.T) {
	mb := &mockCreateBackend{name: "mock", available: true}
	setupCreateTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{
		"create", "flagvm",
		"--cpus", "2",
		"--memory", "4GiB",
		"--disk", "50GiB",
	})
	err := root.Execute()

	require.NoError(t, err)
	require.Len(t, mb.created, 1)
	cfg := mb.created[0].cfg

	assert.Equal(t, 2, cfg.CPUs, "flag must override default CPUs")
	assert.Equal(t, "4GiB", cfg.Memory, "flag must override default memory")
	assert.Equal(t, "50GiB", cfg.Disk, "flag must override default disk")
}

// Property: mount spec parsing roundtrip -- parse and check invariants.
func TestProperty_MountSpecParsing(t *testing.T) {
	cases := []struct {
		spec     string
		host     string
		guest    string
		writable bool
	}{
		{"/a:/b", "/a", "/b", false},
		{"/a:/b:ro", "/a", "/b", false},
		{"/a:/b:rw", "/a", "/b", true},
		{"/a", "/a", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			m := parseMountSpec(tc.spec)
			assert.Equal(t, tc.host, m.HostPath)
			assert.Equal(t, tc.guest, m.GuestPath)
			assert.Equal(t, tc.writable, m.Writable)
		})
	}
}

// Property: error codes are always snake_case.
func TestProperty_ErrorCodesSnakeCase(t *testing.T) {
	// Test vm_already_exists
	mb := &mockCreateBackend{name: "mock", available: true, err: backend.ErrVMAlreadyExists}
	setupCreateTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"create", "dup"})
	err := root.Execute()
	require.Error(t, err)
	cliErr := err.(ui.CLIError)
	assert.Equal(t, "vm_already_exists", cliErr.Code)

	// Test backend_unavailable
	mb2 := &mockCreateBackend{name: "mock", available: false}
	setupCreateTest(t, mb2)

	root2 := RootCmd()
	root2.SetArgs([]string{"create", "test"})
	err2 := root2.Execute()
	require.Error(t, err2)
	cliErr2 := err2.(ui.CLIError)
	assert.Equal(t, "backend_unavailable", cliErr2.Code)
}

// --- Host key capture tests (REQ-004-031) ---

func TestCreateCommand_HostKeyCapture_TCP(t *testing.T) {
	mb := &mockCreateBackend{
		name:      "mock",
		available: true,
		sshCfg: backend.SSHConfig{
			Host:      "127.0.0.1",
			Port:      60022,
			User:      "dev",
			Transport: "tcp",
		},
	}
	setupCreateTest(t, mb)

	captureCalled := false
	origCapture := captureHostKey
	captureHostKey = func(sdHome, vmName, host string, port int) error {
		captureCalled = true
		assert.Equal(t, "testvm", vmName)
		assert.Equal(t, "127.0.0.1", host)
		assert.Equal(t, 60022, port)
		return nil
	}
	defer func() { captureHostKey = origCapture }()

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm"})
	err := root.Execute()

	require.NoError(t, err)
	assert.True(t, captureCalled, "captureHostKey must be called for TCP transport")
}

func TestCreateCommand_HostKeyCapture_VSOCK_Skipped(t *testing.T) {
	mb := &mockCreateBackend{
		name:      "mock",
		available: true,
		sshCfg: backend.SSHConfig{
			User:         "dev",
			ProxyCommand: "limactl ssh --stdio testvm",
			Transport:    "vsock",
		},
	}
	setupCreateTest(t, mb)

	captureCalled := false
	origCapture := captureHostKey
	captureHostKey = func(sdHome, vmName, host string, port int) error {
		captureCalled = true
		return nil
	}
	defer func() { captureHostKey = origCapture }()

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm"})
	err := root.Execute()

	require.NoError(t, err)
	assert.False(t, captureCalled, "captureHostKey must NOT be called for VSOCK transport")
}

func TestCreateCommand_HostKeyCapture_Failure_NonFatal(t *testing.T) {
	mb := &mockCreateBackend{
		name:      "mock",
		available: true,
		sshCfg: backend.SSHConfig{
			Host:      "127.0.0.1",
			Port:      60022,
			Transport: "tcp",
		},
	}
	setupCreateTest(t, mb)

	origCapture := captureHostKey
	captureHostKey = func(sdHome, vmName, host string, port int) error {
		return fmt.Errorf("ssh-keyscan failed: connection refused")
	}
	defer func() { captureHostKey = origCapture }()

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm"})
	err := root.Execute()

	// Create should still succeed even if host key capture fails
	require.NoError(t, err)
	require.Len(t, mb.created, 1)
}

func TestCreateCommand_HostKeyCapture_EmptyTransport_Skipped(t *testing.T) {
	mb := &mockCreateBackend{
		name:      "mock",
		available: true,
		sshCfg:    backend.SSHConfig{}, // empty transport
	}
	setupCreateTest(t, mb)

	captureCalled := false
	origCapture := captureHostKey
	captureHostKey = func(sdHome, vmName, host string, port int) error {
		captureCalled = true
		return nil
	}
	defer func() { captureHostKey = origCapture }()

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm"})
	err := root.Execute()

	require.NoError(t, err)
	assert.False(t, captureCalled, "captureHostKey must not be called with empty transport")
}

// Property: TCP transport always attempts host key capture
func TestProperty_Create_TCP_AlwaysCapturesHostKey(t *testing.T) {
	cases := []struct {
		name string
		host string
		port int
	}{
		{"vm1", "127.0.0.1", 60022},
		{"vm2", "192.168.1.1", 22},
		{"vm3", "10.0.0.1", 2222},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mb := &mockCreateBackend{
				name:      "mock",
				available: true,
				sshCfg: backend.SSHConfig{
					Host:      tc.host,
					Port:      tc.port,
					Transport: "tcp",
				},
			}
			setupCreateTest(t, mb)

			captured := false
			origCapture := captureHostKey
			captureHostKey = func(sdHome, vmName, host string, port int) error {
				captured = true
				return nil
			}
			defer func() { captureHostKey = origCapture }()

			root := RootCmd()
			root.SetArgs([]string{"create", tc.name})
			err := root.Execute()

			require.NoError(t, err)
			assert.True(t, captured, "TCP transport must attempt host key capture for %q", tc.name)
		})
	}
}
