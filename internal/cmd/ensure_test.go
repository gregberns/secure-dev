// Package cmd provides tests for the ensure command.
// REQ-002-024: sd ensure <name> -- idempotent VM existence and running state
// NOTE: Tests use global getBackendFunc -- do not use t.Parallel().
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/backend"
	"sd/internal/backend/memory"
	"sd/internal/ui"
)

// setupEnsureTest configures the test environment with a memory backend.
func setupEnsureTest(t *testing.T) *memory.Backend {
	t.Helper()
	newRootTestEnv(t)

	// Reset ensure command's local flags to prevent StringArray accumulation.
	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "ensure" {
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

// --- Registration tests ---

func TestEnsureCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "ensure" {
			found = true
			assert.Equal(t, "vm", cmd.GroupID)
			break
		}
	}
	assert.True(t, found, "ensure command must be registered")
}

func TestEnsureCommand_AliasUp(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"ensure"})
	require.NoError(t, err)
	assert.Contains(t, cmd.Aliases, "up", "ensure must have alias 'up'")
}

func TestEnsureCommand_AliasUp_Functional(t *testing.T) {
	mb := setupEnsureTest(t)
	ctx := context.Background()

	root := RootCmd()
	root.SetArgs([]string{"up", "testvm"})
	err := root.Execute()
	require.NoError(t, err)

	// VM should have been created and be running
	status, sErr := mb.Status(ctx, "testvm")
	require.NoError(t, sErr)
	assert.Equal(t, backend.StatusRunning, status)
}

func TestEnsureCommand_RangeArgs(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"ensure"})
	require.NoError(t, err)
	assert.NotNil(t, cmd.Args, "ensure must have an Args validator")
	// REQ-005-021: 0 args is now valid at the args layer (name resolved from .sd.yaml)
	err = cmd.Args(cmd, nil)
	assert.NoError(t, err, "ensure must accept zero args (name resolved from .sd.yaml)")
	// 1 arg is still valid
	err = cmd.Args(cmd, []string{"test"})
	assert.NoError(t, err, "ensure must accept one arg")
	// 2 args is rejected
	err = cmd.Args(cmd, []string{"a", "b"})
	assert.Error(t, err, "ensure must reject two args")
}

func TestEnsureCommand_Flags(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"ensure"})
	require.NoError(t, err)

	// REQ-002-025: Must accept all create flags
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

// --- Core logic tests ---

func TestEnsureCommand_CreatesWhenNotFound(t *testing.T) {
	mb := setupEnsureTest(t)
	ctx := context.Background()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"ensure", "newvm"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "newvm")

	// VM should exist and be running
	status, sErr := mb.Status(ctx, "newvm")
	require.NoError(t, sErr)
	assert.Equal(t, backend.StatusRunning, status)
}

func TestEnsureCommand_StartsWhenStopped(t *testing.T) {
	mb := setupEnsureTest(t)
	ctx := context.Background()

	// Pre-create a VM and stop it
	require.NoError(t, mb.Create(ctx, "stoppedvm", backend.VMConfig{}))
	require.NoError(t, mb.Stop(ctx, "stoppedvm"))

	// Verify it's stopped
	status, err := mb.Status(ctx, "stoppedvm")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusStopped, status)

	root := RootCmd()
	root.SetArgs([]string{"ensure", "stoppedvm"})
	execErr := root.Execute()
	require.NoError(t, execErr)

	// VM should now be running
	status, err = mb.Status(ctx, "stoppedvm")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusRunning, status)
}

func TestEnsureCommand_NoopWhenRunning(t *testing.T) {
	mb := setupEnsureTest(t)
	ctx := context.Background()

	// Pre-create a VM (memory backend creates in Running state)
	require.NoError(t, mb.Create(ctx, "runningvm", backend.VMConfig{}))

	root := RootCmd()
	root.SetArgs([]string{"ensure", "runningvm"})
	execErr := root.Execute()
	require.NoError(t, execErr)

	// VM should still be running
	status, err := mb.Status(ctx, "runningvm")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusRunning, status)
}

func TestEnsureCommand_InvalidName(t *testing.T) {
	setupEnsureTest(t)

	root := RootCmd()
	root.SetArgs([]string{"ensure", "INVALID-VM-NAME"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok, "must be a CLIError")
	assert.Equal(t, "invalid_argument", cliErr.Code)
}

// --- JSON output tests ---

func TestEnsureCommand_JSON_Created(t *testing.T) {
	setupEnsureTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "ensure", "newvm"})
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
	assert.Equal(t, "newvm", data["name"])
	assert.Equal(t, "created", data["action"])
	assert.Equal(t, "running", data["status"])
	assert.NotEmpty(t, data["backend"])
}

// REQ-010-016: Verify ensure JSON output includes hints when VM is created
func TestEnsureCommand_JSON_Created_ContainsHints(t *testing.T) {
	setupEnsureTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "ensure", "newvm"})
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

	// REQ-010-016: hints field must be present when VM is created via ensure
	hintsRaw, hasHints := result["hints"]
	require.True(t, hasHints, "JSON output must contain 'hints' field when ensure creates a VM")
	hintsArr, ok := hintsRaw.([]any)
	require.True(t, ok, "hints must be a JSON array")
	assert.NotEmpty(t, hintsArr, "hints array must not be empty when ensure creates a VM")

	// Verify expected hint content
	hintsStrs := make([]string, len(hintsArr))
	for i, h := range hintsArr {
		hintsStrs[i] = h.(string)
	}
	assert.Contains(t, hintsStrs, "Export GITHUB_TOKEN on the host before running sd connect to inject credentials into the VM.")
	assert.Contains(t, hintsStrs, "Run sd config egress list to review which domains the VM can reach.")
}

// REQ-010-016: Verify ensure JSON output includes hints when VM is started
func TestEnsureCommand_JSON_Started_ContainsHints(t *testing.T) {
	mb := setupEnsureTest(t)
	ctx := context.Background()

	// Pre-create and stop
	require.NoError(t, mb.Create(ctx, "stoppedvm", backend.VMConfig{}))
	require.NoError(t, mb.Stop(ctx, "stoppedvm"))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "ensure", "stoppedvm"})
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
	assert.Equal(t, "started", data["action"])

	// REQ-010-016: hints field must be present when VM is started
	hintsRaw, hasHints := result["hints"]
	require.True(t, hasHints, "JSON output must contain 'hints' field when ensure starts a VM")
	hintsArr, ok := hintsRaw.([]any)
	require.True(t, ok, "hints must be a JSON array")
	assert.NotEmpty(t, hintsArr, "hints array must not be empty when ensure starts a VM")

	// Started branch has a single hint about GITHUB_TOKEN
	hintsStrs := make([]string, len(hintsArr))
	for i, h := range hintsArr {
		hintsStrs[i] = h.(string)
	}
	assert.Contains(t, hintsStrs, "Export GITHUB_TOKEN on the host before running sd connect to inject credentials into the VM.")
}

// REQ-010-016: Verify ensure JSON output has no hints when VM is already running
// (already_running branch uses SuccessData, not SuccessDataWithHints)
func TestEnsureCommand_JSON_AlreadyRunning_NoHints(t *testing.T) {
	mb := setupEnsureTest(t)
	ctx := context.Background()

	require.NoError(t, mb.Create(ctx, "runningvm", backend.VMConfig{}))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "ensure", "runningvm"})
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
	assert.Equal(t, "already_running", data["action"])

	// already_running uses SuccessData (no hints), so hints field must be absent
	_, hasHints := result["hints"]
	assert.False(t, hasHints, "JSON output must NOT contain 'hints' field when VM is already running")
}

func TestEnsureCommand_JSON_Started(t *testing.T) {
	mb := setupEnsureTest(t)
	ctx := context.Background()

	// Pre-create and stop
	require.NoError(t, mb.Create(ctx, "stoppedvm", backend.VMConfig{}))
	require.NoError(t, mb.Stop(ctx, "stoppedvm"))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "ensure", "stoppedvm"})
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
	assert.Equal(t, "started", data["action"])
	assert.Equal(t, "running", data["status"])
}

func TestEnsureCommand_JSON_AlreadyRunning(t *testing.T) {
	mb := setupEnsureTest(t)
	ctx := context.Background()

	require.NoError(t, mb.Create(ctx, "runningvm", backend.VMConfig{}))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "ensure", "runningvm"})
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
	assert.Equal(t, "runningvm", data["name"])
	assert.Equal(t, "already_running", data["action"])
	assert.Equal(t, "running", data["status"])
}

// --- Idempotency test ---

func TestEnsureCommand_Idempotent(t *testing.T) {
	mb := setupEnsureTest(t)
	ctx := context.Background()

	// Call ensure N times -- VM should always end up running
	for i := 0; i < 5; i++ {
		root := RootCmd()
		root.SetArgs([]string{"ensure", "idemvm"})
		err := root.Execute()
		require.NoError(t, err, "ensure call %d should succeed", i+1)
	}

	status, err := mb.Status(ctx, "idemvm")
	require.NoError(t, err)
	assert.Equal(t, backend.StatusRunning, status)

	// Only one VM should exist (not duplicated)
	vms, err := mb.List(ctx)
	require.NoError(t, err)
	count := 0
	for _, vm := range vms {
		if vm.Name == "idemvm" {
			count++
		}
	}
	assert.Equal(t, 1, count, "exactly one VM named 'idemvm' should exist")
}

// --- Error handling tests ---

func TestEnsureCommand_BackendUnavailable(t *testing.T) {
	mb := setupEnsureTest(t)
	mb.SetMethodError("available", fmt.Errorf("backend not available"))

	root := RootCmd()
	root.SetArgs([]string{"ensure", "testvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestEnsureCommand_StartFails(t *testing.T) {
	mb := setupEnsureTest(t)
	ctx := context.Background()

	// Pre-create and stop, then inject start error
	require.NoError(t, mb.Create(ctx, "failvm", backend.VMConfig{}))
	require.NoError(t, mb.Stop(ctx, "failvm"))
	mb.SetMethodError("start", fmt.Errorf("start engine broken"))

	root := RootCmd()
	root.SetArgs([]string{"ensure", "failvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_start_failed", cliErr.Code)
}

func TestEnsureCommand_StatusCheckFails(t *testing.T) {
	mb := setupEnsureTest(t)
	// Inject status error that is NOT ErrVMNotFound
	mb.SetMethodError("status", fmt.Errorf("status check broken"))

	root := RootCmd()
	root.SetArgs([]string{"ensure", "testvm"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_status_failed", cliErr.Code)
}

func TestEnsureCommand_MissingName(t *testing.T) {
	dir := newRootTestEnv(t)

	// Ensure CWD has no .sd.yaml
	origGetWD := getWorkingDir
	getWorkingDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { getWorkingDir = origGetWD })

	root := RootCmd()
	root.SetArgs([]string{"ensure"})
	err := root.Execute()
	assert.Error(t, err, "ensure without name must fail")
	assert.Contains(t, err.Error(), "missing VM name")
}

// --- .sd.yaml project config integration tests ---

func TestEnsureCommand_SDYaml_CreatesVM(t *testing.T) {
	mb := setupEnsureTest(t)
	ctx := context.Background()

	dir := t.TempDir()
	sdYaml := "name: yaml-ensure-vm\n"
	require.NoError(t, os.WriteFile(dir+"/.sd.yaml", []byte(sdYaml), 0644))

	origGetWD := getWorkingDir
	getWorkingDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { getWorkingDir = origGetWD })

	root := RootCmd()
	root.SetArgs([]string{"ensure"})
	err := root.Execute()
	require.NoError(t, err)

	// VM should be created with the .sd.yaml name
	status, sErr := mb.Status(ctx, "yaml-ensure-vm")
	require.NoError(t, sErr)
	assert.Equal(t, backend.StatusRunning, status)
}

func TestEnsureCommand_SDYaml_StartsIfStopped(t *testing.T) {
	mb := setupEnsureTest(t)
	ctx := context.Background()

	// Pre-create and stop the VM
	require.NoError(t, mb.Create(ctx, "yaml-stopped", backend.VMConfig{}))
	require.NoError(t, mb.Stop(ctx, "yaml-stopped"))

	dir := t.TempDir()
	sdYaml := "name: yaml-stopped\n"
	require.NoError(t, os.WriteFile(dir+"/.sd.yaml", []byte(sdYaml), 0644))

	origGetWD := getWorkingDir
	getWorkingDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { getWorkingDir = origGetWD })

	root := RootCmd()
	root.SetArgs([]string{"ensure"})
	err := root.Execute()
	require.NoError(t, err)

	// VM should be running
	status, sErr := mb.Status(ctx, "yaml-stopped")
	require.NoError(t, sErr)
	assert.Equal(t, backend.StatusRunning, status)
}

func TestEnsureCommand_SDYaml_NoopIfRunning(t *testing.T) {
	mb := setupEnsureTest(t)
	ctx := context.Background()

	// Pre-create the VM (memory backend starts in Running state)
	require.NoError(t, mb.Create(ctx, "yaml-running", backend.VMConfig{}))

	dir := t.TempDir()
	sdYaml := "name: yaml-running\n"
	require.NoError(t, os.WriteFile(dir+"/.sd.yaml", []byte(sdYaml), 0644))

	origGetWD := getWorkingDir
	getWorkingDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { getWorkingDir = origGetWD })

	root := RootCmd()
	root.SetArgs([]string{"ensure"})
	err := root.Execute()
	require.NoError(t, err)

	// VM should still be running
	status, sErr := mb.Status(ctx, "yaml-running")
	require.NoError(t, sErr)
	assert.Equal(t, backend.StatusRunning, status)
}
