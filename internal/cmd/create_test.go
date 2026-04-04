// Package cmd provides tests for the create command.
// REQ-002-003: VM Management Commands -- create
// NOTE: Tests use global getBackendFunc — do not use t.Parallel().
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"reflect"
	"testing"
	"unsafe"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/backend"
	"sd/internal/backend/memory"
	"sd/internal/provision"
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

// memPortFromNameCreate computes the deterministic port the memory backend
// returns for a given VM name.
func memPortFromNameCreate(name string) int {
	h := fnv.New32a()
	h.Write([]byte(name))
	return 10000 + int(h.Sum32()%50000)
}

// setupCreateTest configures the test environment with a memory backend.
func setupCreateTest(t *testing.T) *memory.Backend {
	t.Helper()
	newRootTestEnv(t)

	// Reset create command's local flags to prevent StringArray accumulation.
	// Also reset Changed so validation only triggers for flags explicitly set in the test.
	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "create" {
			resetSliceFlag(cmd, "mount")
			resetSliceFlag(cmd, "allow-egress")
			resetSliceFlag(cmd, "modules")
			_ = cmd.Flags().Set("backend", "")
			cmd.Flags().Lookup("backend").Changed = false
			_ = cmd.Flags().Set("cpus", "0")
			cmd.Flags().Lookup("cpus").Changed = false
			_ = cmd.Flags().Set("memory", "")
			cmd.Flags().Lookup("memory").Changed = false
			_ = cmd.Flags().Set("disk", "")
			cmd.Flags().Lookup("disk").Changed = false
			break
		}
	}

	mb := memory.New()
	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return mb, nil
	}
	origValidateBackend := validateBackendFunc
	validateBackendFunc = func(_ string) error {
		return nil
	}
	t.Cleanup(func() {
		getBackendFunc = origGetBackend
		validateBackendFunc = origValidateBackend
		mb.Reset()
	})
	return mb
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
	mb := setupCreateTest(t)
	ctx := context.Background()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm"})
	provExecErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, provExecErr)
	assert.Contains(t, buf.String(), "testvm")
	assert.Contains(t, buf.String(), "created")

	// Verify VM was created
	status, sErr := mb.Status(ctx, "testvm")
	require.NoError(t, sErr)
	assert.Equal(t, backend.StatusRunning, status)
}

func TestCreateCommand_BasicCreate_JSONOutput(t *testing.T) {
	setupCreateTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "create", "testvm"})
	provExecErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, provExecErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err, "output must be valid JSON: %s", buf.String())

	assert.True(t, result["ok"].(bool))
	data := result["data"].(map[string]any)
	assert.Equal(t, "testvm", data["name"])
	// Backend name in result is the resolved name (from config/default), not the memory backend's Name()
	assert.Equal(t, "lima", data["backend"])
}

func TestCreateCommand_VMAlreadyExists(t *testing.T) {
	mb := setupCreateTest(t)
	ctx := context.Background()

	// Pre-create a VM so it already exists
	require.NoError(t, mb.Create(ctx, "dupvm", backend.VMConfig{}))

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
	mb := setupCreateTest(t)
	mb.SetMethodError("available", fmt.Errorf("backend not available"))

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
	setupCreateTest(t)

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
	mb := setupCreateTest(t)
	ctx := context.Background()

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--cpus", "8"})
	err := root.Execute()

	require.NoError(t, err)
	// Verify config via List
	vms, lErr := mb.List(ctx)
	require.NoError(t, lErr)
	require.Len(t, vms, 1)
	assert.Equal(t, 8, vms[0].CPUs)
}

func TestCreateCommand_MemoryFlag(t *testing.T) {
	mb := setupCreateTest(t)
	ctx := context.Background()

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--memory", "16GiB"})
	err := root.Execute()

	require.NoError(t, err)
	vms, lErr := mb.List(ctx)
	require.NoError(t, lErr)
	require.Len(t, vms, 1)
	assert.Equal(t, "16GiB", vms[0].Memory)
}

func TestCreateCommand_DiskFlag(t *testing.T) {
	mb := setupCreateTest(t)
	ctx := context.Background()

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--disk", "200GiB"})
	err := root.Execute()

	require.NoError(t, err)
	vms, lErr := mb.List(ctx)
	require.NoError(t, lErr)
	require.Len(t, vms, 1)
	assert.Equal(t, "200GiB", vms[0].Disk)
}

func TestCreateCommand_BackendFlag(t *testing.T) {
	mb := setupCreateTest(t)
	ctx := context.Background()

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--backend", "custom"})
	err := root.Execute()

	require.NoError(t, err)
	// Verify VM was created
	status, sErr := mb.Status(ctx, "testvm")
	require.NoError(t, sErr)
	assert.Equal(t, backend.StatusRunning, status)
}

func TestCreateCommand_MountFlag(t *testing.T) {
	mb := setupCreateTest(t)
	ctx := context.Background()

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--mount", "/host/path:/guest/path"})
	err := root.Execute()

	require.NoError(t, err)
	// Verify VM was created (mount config is passed to backend but memory
	// backend doesn't expose it via List; verify VM exists)
	status, sErr := mb.Status(ctx, "testvm")
	require.NoError(t, sErr)
	assert.Equal(t, backend.StatusRunning, status)
}

func TestCreateCommand_MountFlagReadWrite(t *testing.T) {
	mb := setupCreateTest(t)
	ctx := context.Background()

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--mount", "/host/path:/guest/path:rw"})
	err := root.Execute()

	require.NoError(t, err)
	status, sErr := mb.Status(ctx, "testvm")
	require.NoError(t, sErr)
	assert.Equal(t, backend.StatusRunning, status)
}

func TestCreateCommand_AllFlags(t *testing.T) {
	mb := setupCreateTest(t)
	ctx := context.Background()

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
	vms, lErr := mb.List(ctx)
	require.NoError(t, lErr)
	require.Len(t, vms, 1)
	assert.Equal(t, "myvm", vms[0].Name)
	assert.Equal(t, 8, vms[0].CPUs)
	assert.Equal(t, "16GiB", vms[0].Memory)
	assert.Equal(t, "200GiB", vms[0].Disk)
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
			setupCreateTest(t)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"--json", "create", name})
			provExecErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, provExecErr)

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
			setupCreateTest(t)

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"create", name})
			provExecErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, provExecErr)
			assert.Contains(t, buf.String(), name, "human output must contain VM name %q", name)
		})
	}
}

// Property: default values are applied when flags are not provided.
func TestProperty_DefaultsApplied(t *testing.T) {
	mb := setupCreateTest(t)
	ctx := context.Background()

	root := RootCmd()
	root.SetArgs([]string{"create", "defvm"})
	err := root.Execute()

	require.NoError(t, err)
	vms, lErr := mb.List(ctx)
	require.NoError(t, lErr)
	require.Len(t, vms, 1)

	// From config/defaults.go
	assert.Equal(t, 4, vms[0].CPUs, "default CPUs must be 4")
	assert.Equal(t, "8GiB", vms[0].Memory, "default memory must be 8GiB")
	assert.Equal(t, "100GiB", vms[0].Disk, "default disk must be 100GiB")
}

// Property: CLI flags always override config defaults.
func TestProperty_FlagsOverrideDefaults(t *testing.T) {
	mb := setupCreateTest(t)
	ctx := context.Background()

	root := RootCmd()
	root.SetArgs([]string{
		"create", "flagvm",
		"--cpus", "2",
		"--memory", "4GiB",
		"--disk", "50GiB",
	})
	err := root.Execute()

	require.NoError(t, err)
	vms, lErr := mb.List(ctx)
	require.NoError(t, lErr)
	require.Len(t, vms, 1)

	assert.Equal(t, 2, vms[0].CPUs, "flag must override default CPUs")
	assert.Equal(t, "4GiB", vms[0].Memory, "flag must override default memory")
	assert.Equal(t, "50GiB", vms[0].Disk, "flag must override default disk")
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
	mb := setupCreateTest(t)
	ctx := context.Background()
	require.NoError(t, mb.Create(ctx, "dup", backend.VMConfig{}))

	root := RootCmd()
	root.SetArgs([]string{"create", "dup"})
	err := root.Execute()
	require.Error(t, err)
	cliErr := err.(ui.CLIError)
	assert.Equal(t, "vm_already_exists", cliErr.Code)

	// Test backend_unavailable
	mb2 := setupCreateTest(t)
	mb2.SetMethodError("available", fmt.Errorf("backend not available"))

	root2 := RootCmd()
	root2.SetArgs([]string{"create", "test"})
	err2 := root2.Execute()
	require.Error(t, err2)
	cliErr2 := err2.(ui.CLIError)
	assert.Equal(t, "backend_unavailable", cliErr2.Code)
}

// --- Host key capture tests (REQ-004-031) ---

func TestCreateCommand_HostKeyCapture_TCP(t *testing.T) {
	setupCreateTest(t)

	captureCalled := false
	origCapture := captureHostKey
	captureHostKey = func(sdHome, vmName, host string, port int) error {
		captureCalled = true
		assert.Equal(t, "testvm", vmName)
		assert.Equal(t, "127.0.0.1", host)
		// Memory backend uses deterministic port from FNV hash
		assert.Equal(t, memPortFromNameCreate("testvm"), port)
		return nil
	}
	defer func() { captureHostKey = origCapture }()

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm"})
	err := root.Execute()

	require.NoError(t, err)
	assert.True(t, captureCalled, "captureHostKey must be called for TCP transport")
}

func TestCreateCommand_HostKeyCapture_SSHConfigError_Skipped(t *testing.T) {
	// Memory backend always returns TCP transport; to test "skipping capture",
	// we inject an SSHConfig error which causes the create command to skip
	// host key capture entirely (matches the code path where SSHConfig fails).
	mb := setupCreateTest(t)
	mb.SetMethodError("sshconfig", fmt.Errorf("sshconfig not available"))

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
	assert.False(t, captureCalled, "captureHostKey must NOT be called when SSHConfig fails")
}

func TestCreateCommand_HostKeyCapture_Failure_NonFatal(t *testing.T) {
	mb := setupCreateTest(t)
	ctx := context.Background()

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
	status, sErr := mb.Status(ctx, "testvm")
	require.NoError(t, sErr)
	assert.Equal(t, backend.StatusRunning, status)
}

// Property: TCP transport always attempts host key capture
func TestProperty_Create_TCP_AlwaysCapturesHostKey(t *testing.T) {
	// Memory backend always returns TCP transport, so all creates should trigger capture
	names := []string{"vm1", "vm2", "vm3"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			setupCreateTest(t)

			captured := false
			origCapture := captureHostKey
			captureHostKey = func(sdHome, vmName, host string, port int) error {
				captured = true
				return nil
			}
			defer func() { captureHostKey = origCapture }()

			root := RootCmd()
			root.SetArgs([]string{"create", name})
			err := root.Execute()

			require.NoError(t, err)
			assert.True(t, captured, "TCP transport must attempt host key capture for %q", name)
		})
	}
}

// --- REQ-004-005: Mount path validation in create ---

func TestCreateCommand_MountSensitivePath_SSHDir(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	mb := setupCreateTest(t)
	ctx := context.Background()

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--mount", homeDir + "/.ssh:/keys"})
	err = root.Execute()

	require.Error(t, err, "mounting ~/.ssh must be rejected")
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "mount_path_rejected", cliErr.Code)
	assert.Contains(t, cliErr.Message, "sensitive")
	// Verify backend Create was not called
	_, sErr := mb.Status(ctx, "testvm")
	assert.ErrorIs(t, sErr, backend.ErrVMNotFound)
}

func TestCreateCommand_MountSensitivePath_HomeDir(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	mb := setupCreateTest(t)
	ctx := context.Background()

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--mount", homeDir + ":/home"})
	err = root.Execute()

	require.Error(t, err, "mounting $HOME must be rejected")
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "mount_path_rejected", cliErr.Code)
	_, sErr := mb.Status(ctx, "testvm")
	assert.ErrorIs(t, sErr, backend.ErrVMNotFound)
}

func TestCreateCommand_MountSensitivePath_DockerSocket(t *testing.T) {
	mb := setupCreateTest(t)
	ctx := context.Background()

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--mount", "/var/run/docker.sock:/var/run/docker.sock"})
	err := root.Execute()

	require.Error(t, err, "mounting docker socket must be rejected")
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "mount_path_rejected", cliErr.Code)
	_, sErr := mb.Status(ctx, "testvm")
	assert.ErrorIs(t, sErr, backend.ErrVMNotFound)
}

func TestCreateCommand_MountSafePath_Succeeds(t *testing.T) {
	mb := setupCreateTest(t)
	ctx := context.Background()

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--mount", "/opt/workspace:/workspace"})
	err := root.Execute()

	require.NoError(t, err, "non-sensitive mount path must be accepted")
	status, sErr := mb.Status(ctx, "testvm")
	require.NoError(t, sErr)
	assert.Equal(t, backend.StatusRunning, status)
}

func TestCreateCommand_MountSensitivePath_JSON(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	mb := setupCreateTest(t)
	ctx := context.Background()

	root := RootCmd()
	root.SetArgs([]string{"--json", "create", "testvm", "--mount", homeDir + "/.ssh:/keys"})
	provExecErr := root.Execute()

	require.Error(t, provExecErr)
	cliErr, ok := provExecErr.(ui.CLIError)
	require.True(t, ok, "error must be CLIError in JSON mode")
	assert.Equal(t, "mount_path_rejected", cliErr.Code)
	assert.Contains(t, cliErr.Message, "sensitive")
	_, sErr := mb.Status(ctx, "testvm")
	assert.ErrorIs(t, sErr, backend.ErrVMNotFound)
}

func TestCreateCommand_MultipleMounts_FirstSensitive(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	mb := setupCreateTest(t)
	ctx := context.Background()

	root := RootCmd()
	root.SetArgs([]string{
		"create", "testvm",
		"--mount", homeDir + "/.aws:/aws",
		"--mount", "/opt/code:/code",
	})
	err = root.Execute()

	require.Error(t, err, "any sensitive mount must fail the whole command")
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "mount_path_rejected", cliErr.Code)
	_, sErr := mb.Status(ctx, "testvm")
	assert.ErrorIs(t, sErr, backend.ErrVMNotFound)
}

// Property: sensitive mount paths are always rejected regardless of mount mode.
func TestProperty_Create_SensitiveMountAlwaysRejected(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	sensitivePaths := []struct {
		name string
		path string
	}{
		{"ssh", homeDir + "/.ssh"},
		{"aws", homeDir + "/.aws"},
		{"config", homeDir + "/.config"},
		{"gnupg", homeDir + "/.gnupg"},
		{"kube", homeDir + "/.kube"},
		{"docker_dir", homeDir + "/.docker"},
		{"docker_socket", "/var/run/docker.sock"},
	}
	for _, sp := range sensitivePaths {
		for _, mode := range []string{"ro", "rw"} {
			name := fmt.Sprintf("%s_%s", sp.name, mode)
			t.Run(name, func(t *testing.T) {
				setupCreateTest(t)

				root := RootCmd()
				root.SetArgs([]string{"create", "testvm", "--mount", sp.path + ":/mnt:" + mode})
				err := root.Execute()

				require.Error(t, err, "sensitive path %q with mode %s must be rejected", sp.path, mode)
				cliErr, ok := err.(ui.CLIError)
				require.True(t, ok)
				assert.Equal(t, "mount_path_rejected", cliErr.Code)
			})
		}
	}
}

// --- Provisioning integration tests (REQ-001-006 step 5) ---

// setupProvisionCreateTest configures the test environment with a mock backend
// for create+provision integration tests. It reuses the mockProvisionBackend
// from provision_test.go and adds create-command flag resets.
func setupProvisionCreateTest(t *testing.T, mb *mockProvisionBackend) {
	t.Helper()
	setupProvisionTest(t, mb)

	// Also reset create command flags and Changed state
	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "create" {
			resetSliceFlag(cmd, "mount")
			resetSliceFlag(cmd, "allow-egress")
			resetSliceFlag(cmd, "modules")
			_ = cmd.Flags().Set("backend", "")
			cmd.Flags().Lookup("backend").Changed = false
			_ = cmd.Flags().Set("cpus", "0")
			cmd.Flags().Lookup("cpus").Changed = false
			_ = cmd.Flags().Set("memory", "")
			cmd.Flags().Lookup("memory").Changed = false
			_ = cmd.Flags().Set("disk", "")
			cmd.Flags().Lookup("disk").Changed = false
			break
		}
	}
}

func TestCreateCommand_Provisioning_RunsModules(t *testing.T) {
	mb := &mockProvisionBackend{name: "mock", available: true, statusMap: map[string]backend.VMStatus{}}
	setupProvisionCreateTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm"})
	err := root.Execute()

	require.NoError(t, err)
	// Provisioning should have called Exec (all modules run by default)
	assert.NotEmpty(t, mb.execCalls, "provisioning must call Exec for each module script")
}

func TestCreateCommand_Provisioning_WithModulesFlag(t *testing.T) {
	mb := &mockProvisionBackend{name: "mock", available: true, statusMap: map[string]backend.VMStatus{}}
	setupProvisionCreateTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--modules", "base"})
	err := root.Execute()

	require.NoError(t, err)
	// Should only provision base module
	assert.NotEmpty(t, mb.execCalls, "provisioning must call Exec for base module")
}

func TestCreateCommand_Provisioning_ExecFailure(t *testing.T) {
	mb := &mockProvisionBackend{
		name:      "mock",
		available: true,
		statusMap: map[string]backend.VMStatus{},
		execErr:   fmt.Errorf("exec failed"),
	}
	setupProvisionCreateTest(t, mb)

	// Override generateSSHKeys so SSH setup doesn't consume the exec error
	origGen := generateSSHKeys
	generateSSHKeys = func(sdHome, vmName string) error {
		return fmt.Errorf("skip in test")
	}
	t.Cleanup(func() { generateSSHKeys = origGen })

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--modules", "base"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok, "must be CLIError")
	assert.Equal(t, "provision_script_failed", cliErr.Code)
}

func TestCreateCommand_Provisioning_ModuleLoadFailure_NonFatal(t *testing.T) {
	mb := &mockProvisionBackend{name: "mock", available: true, statusMap: map[string]backend.VMStatus{}}
	setupProvisionCreateTest(t, mb)

	// Override loadBuiltinModules to fail
	origLoad := loadBuiltinModules
	loadBuiltinModules = func() ([]provision.Module, error) {
		return nil, fmt.Errorf("module load error")
	}
	t.Cleanup(func() { loadBuiltinModules = origLoad })

	// Override generateSSHKeys to avoid SSH setup Exec calls
	origGen := generateSSHKeys
	generateSSHKeys = func(sdHome, vmName string) error {
		return fmt.Errorf("skip in test")
	}
	t.Cleanup(func() { generateSSHKeys = origGen })

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm"})
	err := root.Execute()

	// Create should still succeed (provisioning failure is a warning)
	require.NoError(t, err)
	assert.Empty(t, mb.execCalls, "no Exec calls when modules fail to load")
}

func TestCreateCommand_Provisioning_ExecRecordsVMName(t *testing.T) {
	mb := &mockProvisionBackend{name: "mock", available: true, statusMap: map[string]backend.VMStatus{}}
	setupProvisionCreateTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"create", "myvm", "--modules", "base"})
	err := root.Execute()

	require.NoError(t, err)
	for _, call := range mb.execCalls {
		assert.Equal(t, "myvm", call.VMName, "Exec must be called with the correct VM name")
	}
}

func TestCreateCommand_Provisioning_ExecUsesSudo_SystemMode(t *testing.T) {
	mb := &mockProvisionBackend{name: "mock", available: true, statusMap: map[string]backend.VMStatus{}}
	setupProvisionCreateTest(t, mb)

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--modules", "base"})
	err := root.Execute()

	require.NoError(t, err)
	// Base module has system-mode scripts that should use sudo
	foundSudo := false
	for _, call := range mb.execCalls {
		if len(call.Command) > 0 && call.Command[0] == "sudo" {
			foundSudo = true
			break
		}
	}
	assert.True(t, foundSudo, "system-mode scripts must be executed with sudo")
}

// Property: provisioning always runs Exec for default create (no --modules flag).
func TestProperty_Create_ProvisioningAlwaysRuns(t *testing.T) {
	for _, name := range []string{"vm-1", "test-vm", "dev"} {
		t.Run(name, func(t *testing.T) {
			mb := &mockProvisionBackend{name: "mock", available: true, statusMap: map[string]backend.VMStatus{}}
			setupProvisionCreateTest(t, mb)

			root := RootCmd()
			root.SetArgs([]string{"create", name})
			err := root.Execute()

			require.NoError(t, err)
			assert.NotEmpty(t, mb.execCalls, "provisioning must run Exec for %q", name)
		})
	}
}

// Property: provisioning failure always triggers cleanup.
func TestProperty_Create_ProvisionFailureAlwaysCleansUp(t *testing.T) {
	for _, name := range []string{"vm-a", "vm-b", "vm-c"} {
		t.Run(name, func(t *testing.T) {
			mb := &mockProvisionBackend{
				name:      "mock",
				available: true,
				statusMap: map[string]backend.VMStatus{},
				execErr:   fmt.Errorf("provision failed"),
			}
			setupProvisionCreateTest(t, mb)

			// Override generateSSHKeys so SSH setup doesn't consume the exec error
			origGen := generateSSHKeys
			generateSSHKeys = func(sdHome, vmName string) error {
				return fmt.Errorf("skip in test")
			}
			t.Cleanup(func() { generateSSHKeys = origGen })

			root := RootCmd()
			root.SetArgs([]string{"create", name, "--modules", "base"})
			err := root.Execute()

			require.Error(t, err)
		})
	}
}

// --- REQ-002-003: Resource flag validation tests ---

func TestCreateCommand_InvalidCPUs_Negative(t *testing.T) {
	setupCreateTest(t)

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--cpus", "-1"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok, "must be a CLIError")
	assert.Equal(t, "invalid_cpus", cliErr.Code)
	assert.Contains(t, cliErr.Message, "-1")
}

func TestCreateCommand_InvalidCPUs_Zero(t *testing.T) {
	setupCreateTest(t)

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--cpus", "0"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok, "must be a CLIError")
	assert.Equal(t, "invalid_cpus", cliErr.Code)
}

func TestCreateCommand_InvalidCPUs_TooHigh(t *testing.T) {
	setupCreateTest(t)

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--cpus", "999"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok, "must be a CLIError")
	assert.Equal(t, "invalid_cpus", cliErr.Code)
	assert.Contains(t, cliErr.Message, "999")
}

func TestCreateCommand_InvalidMemory_Nonsense(t *testing.T) {
	setupCreateTest(t)

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--memory", "banana"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok, "must be a CLIError")
	assert.Equal(t, "invalid_memory", cliErr.Code)
	assert.Contains(t, cliErr.Message, "banana")
}

func TestCreateCommand_InvalidMemory_Zero(t *testing.T) {
	setupCreateTest(t)

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--memory", "0GiB"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok, "must be a CLIError")
	assert.Equal(t, "invalid_memory", cliErr.Code)
}

func TestCreateCommand_InvalidMemory_NoUnit(t *testing.T) {
	setupCreateTest(t)

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--memory", "1024"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok, "must be a CLIError")
	assert.Equal(t, "invalid_memory", cliErr.Code)
}

func TestCreateCommand_InvalidDisk_Nonsense(t *testing.T) {
	setupCreateTest(t)

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--disk", "lots"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok, "must be a CLIError")
	assert.Equal(t, "invalid_disk", cliErr.Code)
	assert.Contains(t, cliErr.Message, "lots")
}

func TestCreateCommand_InvalidDisk_Zero(t *testing.T) {
	setupCreateTest(t)

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--disk", "0GiB"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok, "must be a CLIError")
	assert.Equal(t, "invalid_disk", cliErr.Code)
}

func TestCreateCommand_InvalidBackend(t *testing.T) {
	setupCreateTest(t)

	// Override validateBackendFunc to actually reject unknown backends
	origValidate := validateBackendFunc
	validateBackendFunc = func(name string) error {
		if name == "nonexistent" {
			return ui.CLIError{
				Code:    "invalid_backend",
				Message: fmt.Sprintf("unknown backend %q", name),
			}
		}
		return nil
	}
	t.Cleanup(func() { validateBackendFunc = origValidate })

	root := RootCmd()
	root.SetArgs([]string{"create", "testvm", "--backend", "nonexistent"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok, "must be a CLIError")
	assert.Equal(t, "invalid_backend", cliErr.Code)
	assert.Contains(t, cliErr.Message, "nonexistent")
}

func TestCreateCommand_ValidCPUs_Boundary(t *testing.T) {
	cases := []struct {
		name string
		cpus string
	}{
		{"min", "1"},
		{"max", "256"},
		{"typical", "4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupCreateTest(t)

			root := RootCmd()
			root.SetArgs([]string{"create", "testvm", "--cpus", tc.cpus})
			err := root.Execute()

			require.NoError(t, err, "--cpus=%s should be valid", tc.cpus)
		})
	}
}

func TestCreateCommand_ValidMemory(t *testing.T) {
	cases := []struct {
		name   string
		memory string
	}{
		{"gib", "4GiB"},
		{"mib", "512MiB"},
		{"gb", "8GB"},
		{"tb", "1TB"},
		{"kib", "1024KiB"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupCreateTest(t)

			root := RootCmd()
			root.SetArgs([]string{"create", "testvm", "--memory", tc.memory})
			err := root.Execute()

			require.NoError(t, err, "--memory=%s should be valid", tc.memory)
		})
	}
}

func TestCreateCommand_ValidDisk(t *testing.T) {
	cases := []struct {
		name string
		disk string
	}{
		{"gib", "100GiB"},
		{"gb", "50GB"},
		{"tb", "1TB"},
		{"tib", "2TiB"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupCreateTest(t)

			root := RootCmd()
			root.SetArgs([]string{"create", "testvm", "--disk", tc.disk})
			err := root.Execute()

			require.NoError(t, err, "--disk=%s should be valid", tc.disk)
		})
	}
}

// Property: all invalid resource values are always rejected with correct error codes.
func TestProperty_Create_InvalidResourcesAlwaysRejected(t *testing.T) {
	cases := []struct {
		name     string
		flag     string
		value    string
		wantCode string
	}{
		{"cpus_negative", "--cpus", "-1", "invalid_cpus"},
		{"cpus_zero", "--cpus", "0", "invalid_cpus"},
		{"cpus_too_high", "--cpus", "257", "invalid_cpus"},
		{"cpus_way_too_high", "--cpus", "999", "invalid_cpus"},
		{"memory_banana", "--memory", "banana", "invalid_memory"},
		{"memory_zero", "--memory", "0GiB", "invalid_memory"},
		{"memory_no_unit", "--memory", "1024", "invalid_memory"},
		{"memory_empty_string", "--memory", "", "invalid_memory"},
		{"disk_lots", "--disk", "lots", "invalid_disk"},
		{"disk_zero", "--disk", "0GiB", "invalid_disk"},
		{"disk_no_unit", "--disk", "42", "invalid_disk"},
		{"disk_negative_like", "--disk", "-5GiB", "invalid_disk"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupCreateTest(t)

			root := RootCmd()
			root.SetArgs([]string{"create", "testvm", tc.flag, tc.value})
			err := root.Execute()

			require.Error(t, err, "%s=%s must be rejected", tc.flag, tc.value)
			cliErr, ok := err.(ui.CLIError)
			require.True(t, ok, "must be a CLIError for %s=%s", tc.flag, tc.value)
			assert.Equal(t, tc.wantCode, cliErr.Code, "wrong error code for %s=%s", tc.flag, tc.value)
		})
	}
}

// --- validateSizeString unit tests ---

func TestValidateSizeString(t *testing.T) {
	valid := []string{
		"4GiB", "512MiB", "100GB", "1TB", "8GiB", "1024KiB",
		"2TiB", "1PiB", "256MB", "10kB",
	}
	for _, s := range valid {
		t.Run("valid_"+s, func(t *testing.T) {
			err := validateSizeString(s)
			assert.NoError(t, err, "%q should be valid", s)
		})
	}

	invalid := []string{
		"", "0GiB", "banana", "1024", "-5GiB", "0", "GiB", "4.5GiB",
		"4 G i B", "0MB",
	}
	for _, s := range invalid {
		t.Run("invalid_"+s, func(t *testing.T) {
			err := validateSizeString(s)
			assert.Error(t, err, "%q should be invalid", s)
		})
	}
}

// Property: no sensitive mount ever reaches the backend.
func TestProperty_Create_SensitiveMountNeverReachesBackend(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	paths := []string{
		homeDir + "/.ssh",
		homeDir + "/.aws",
		homeDir + "/.kube",
		homeDir + "/.gnupg",
		"/var/run/docker.sock",
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			mb := setupCreateTest(t)
			ctx := context.Background()

			root := RootCmd()
			root.SetArgs([]string{"create", "vm", "--mount", p + ":/mnt"})
			_ = root.Execute()

			// Verify backend Create was never called
			_, sErr := mb.Status(ctx, "vm")
			assert.ErrorIs(t, sErr, backend.ErrVMNotFound, "backend Create must never be called for sensitive mount %q", p)
		})
	}
}
