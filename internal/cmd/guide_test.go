// Package cmd provides tests for the guide command.
// REQ-002-020: Human-readable getting-started guide
// REQ-002-021: Agent-consumable Markdown guide
// REQ-002-022: JSON output for guide
// REQ-002-023: Root --help leads with guide
// NOTE: Tests use global getBackendFunc -- do not use t.Parallel().
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
)

// resetGuideFlags resets the guide command's local flags to defaults.
// Required because commands are singletons and flag values persist across tests.
func resetGuideFlags(t *testing.T) {
	t.Helper()
	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "guide" {
			_ = cmd.Flags().Set("agent", "false")
			_ = cmd.Flags().Set("help", "false")
			break
		}
	}
}

// setupGuideTest configures the test environment with a memory backend.
func setupGuideTest(t *testing.T) *memory.Backend {
	t.Helper()
	newRootTestEnv(t)
	resetGuideFlags(t)

	mb := memory.New()
	origGetBackend := getBackendFunc
	origAllBackendNames := allBackendNames
	origLookPath := lookPath
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return mb, nil
	}
	allBackendNames = func() []string { return []string{"memory"} }
	// Provide all binaries as "found" so doctor checks pass
	lookPath = func(name string) (string, error) {
		return "/usr/bin/" + name, nil
	}
	t.Cleanup(func() {
		getBackendFunc = origGetBackend
		allBackendNames = origAllBackendNames
		lookPath = origLookPath
		resetGuideFlags(t)
		mb.Reset()
	})
	return mb
}

func TestGuideCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "guide" {
			found = true
			assert.Equal(t, "start", cmd.GroupID, "guide must be in 'start' group")

			// Check flags
			agentFlag := cmd.Flags().Lookup("agent")
			assert.NotNil(t, agentFlag, "guide must have --agent flag")
			break
		}
	}
	assert.True(t, found, "guide command must be registered")
}

func TestGuideCommand_DefaultOutput_NonEmpty(t *testing.T) {
	setupGuideTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"guide"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	require.NoError(t, execErr)
	assert.NotEmpty(t, output)
	assert.Contains(t, output, "Quick Start")
	assert.Contains(t, output, "sd doctor")
	assert.Contains(t, output, "sd create")
	assert.Contains(t, output, "sd connect")
}

func TestGuideCommand_DefaultOutput_ReflectsVMs(t *testing.T) {
	mb := setupGuideTest(t)
	ctx := context.Background()

	require.NoError(t, mb.Create(ctx, "my-app", backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB",
	}))
	require.NoError(t, mb.Create(ctx, "infra", backend.VMConfig{
		CPUs: 2, Memory: "4GiB", Disk: "50GiB",
	}))
	require.NoError(t, mb.Stop(ctx, "infra"))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"guide"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	require.NoError(t, execErr)
	assert.Contains(t, output, "VMs: 2")
	assert.Contains(t, output, "my-app")
	assert.Contains(t, output, "infra")
}

func TestGuideCommand_DefaultOutput_NoVMs(t *testing.T) {
	setupGuideTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"guide"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	require.NoError(t, execErr)
	assert.Contains(t, output, "VMs: none")
}

func TestGuideCommand_DefaultOutput_DoctorStatus(t *testing.T) {
	setupGuideTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"guide"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	require.NoError(t, execErr)
	assert.Contains(t, output, "all checks passing")
}

func TestGuideCommand_AgentOutput_ContainsAllCommands(t *testing.T) {
	setupGuideTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"guide", "--agent"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	require.NoError(t, execErr)
	assert.NotEmpty(t, output)

	// Check that the output contains a reference to every visible command
	for _, cmd := range root.Commands() {
		if cmd.Hidden || cmd.Name() == "help" || cmd.Name() == "completion" {
			continue
		}
		assert.Contains(t, output, cmd.Name(),
			"--agent output must reference command %q", cmd.Name())
	}
}

func TestGuideCommand_AgentOutput_IncludesCurrentVMs(t *testing.T) {
	mb := setupGuideTest(t)
	ctx := context.Background()

	require.NoError(t, mb.Create(ctx, "test-project", backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB",
	}))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"guide", "--agent"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	require.NoError(t, execErr)
	assert.Contains(t, output, "test-project")
	assert.Contains(t, output, "running")
}

func TestGuideCommand_AgentOutput_ContainsStructure(t *testing.T) {
	setupGuideTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"guide", "--agent"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	require.NoError(t, execErr)

	// Verify key Markdown sections exist
	assert.Contains(t, output, "# sd (Secure Dev) -- Agent Instructions")
	assert.Contains(t, output, "## Current State")
	assert.Contains(t, output, "## Capabilities")
	assert.Contains(t, output, "## Workflow Guide")
	assert.Contains(t, output, "## Important Notes")
	assert.Contains(t, output, "## Available Modules")
}

func TestGuideCommand_AgentOutput_LongerThanDefault(t *testing.T) {
	setupGuideTest(t)

	// Get default output
	oldStdout := os.Stdout
	r1, w1, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w1

	root := RootCmd()
	root.SetArgs([]string{"guide"})
	require.NoError(t, root.Execute())

	w1.Close()
	os.Stdout = oldStdout

	var buf1 bytes.Buffer
	_, _ = buf1.ReadFrom(r1)
	defaultOutput := buf1.String()

	// Reset agent flag before next execution
	resetGuideFlags(t)

	// Get agent output
	r2, w2, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w2

	root = RootCmd()
	root.SetArgs([]string{"guide", "--agent"})
	require.NoError(t, root.Execute())

	w2.Close()
	os.Stdout = oldStdout

	var buf2 bytes.Buffer
	_, _ = buf2.ReadFrom(r2)
	agentOutput := buf2.String()

	assert.Greater(t, len(agentOutput), len(defaultOutput),
		"--agent output (%d bytes) should be longer than default (%d bytes)",
		len(agentOutput), len(defaultOutput))
}

func TestGuideCommand_JSONOutput_Valid(t *testing.T) {
	setupGuideTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "guide"})
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

	data, ok := result["data"].(map[string]any)
	require.True(t, ok, "data must be a JSON object")

	// Check required fields
	assert.Contains(t, data, "backends")
	assert.Contains(t, data, "vms")
	assert.Contains(t, data, "modules")
	assert.Contains(t, data, "doctor")
	assert.Contains(t, data, "commands")
	assert.Contains(t, data, "guide_text")

	// VMs should be an array
	vms, ok := data["vms"].([]any)
	require.True(t, ok, "vms must be an array")
	assert.Empty(t, vms) // no VMs created

	// Commands should be a non-empty array
	cmds, ok := data["commands"].([]any)
	require.True(t, ok, "commands must be an array")
	assert.NotEmpty(t, cmds, "commands array must not be empty")

	// Guide text should be a non-empty string
	guideText, ok := data["guide_text"].(string)
	require.True(t, ok, "guide_text must be a string")
	assert.NotEmpty(t, guideText)
	assert.Contains(t, guideText, "# sd (Secure Dev)")
}

func TestGuideCommand_JSONOutput_WithVMs(t *testing.T) {
	mb := setupGuideTest(t)
	ctx := context.Background()

	require.NoError(t, mb.Create(ctx, "vm-one", backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB",
	}))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "guide"})
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
	vms := data["vms"].([]any)
	require.Len(t, vms, 1)
	vm := vms[0].(map[string]any)
	assert.Equal(t, "vm-one", vm["name"])
}

func TestGuideCommand_JSONOutput_CommandsMatchCobra(t *testing.T) {
	setupGuideTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "guide"})
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
	cmds := data["commands"].([]any)

	// Build a set of command names from JSON
	cmdNames := make(map[string]bool)
	for _, c := range cmds {
		cmdMap := c.(map[string]any)
		cmdNames[cmdMap["name"].(string)] = true
	}

	// Every visible top-level command should be in JSON
	for _, cmd := range root.Commands() {
		if cmd.Hidden || cmd.Name() == "help" || cmd.Name() == "completion" {
			continue
		}
		assert.True(t, cmdNames[cmd.Name()],
			"JSON commands must include %q", cmd.Name())
	}
}

func TestGuideCommand_NoConfigRequired(t *testing.T) {
	// REQ-002-015: guide must succeed even without a config file
	tmpDir := t.TempDir()
	t.Setenv("SD_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)

	setupGuideTest(t)

	root := RootCmd()
	root.SetArgs([]string{"guide"})
	err := root.Execute()
	assert.NoError(t, err, "guide must succeed without config file")
}

func TestGuideCommand_RejectsExtraArgs(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	root.SetArgs([]string{"guide", "extra-arg"})
	err := root.Execute()
	assert.Error(t, err, "guide should reject extra arguments")
}

func TestGuideCommand_HelpOutput(t *testing.T) {
	newRootTestEnv(t)
	resetGuideFlags(t)

	// Use Cobra's SetOut to capture help text, which Cobra writes
	// to its configured output writer rather than os.Stdout.
	var buf bytes.Buffer
	root := RootCmd()
	root.SetOut(&buf)
	root.SetArgs([]string{"guide", "--help"})
	execErr := root.Execute()

	// Reset the output writer so subsequent tests use os.Stdout
	root.SetOut(nil)

	assert.NoError(t, execErr)
	assert.Contains(t, buf.String(), "--agent")
}

// --- Root help text tests ---

func TestRootHelp_ContainsGettingStartedGroup(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	groups := root.Groups()

	// Verify "start" group exists
	found := false
	for _, g := range groups {
		if g.ID == "start" {
			found = true
			assert.Equal(t, "Getting Started", g.Title)
			break
		}
	}
	assert.True(t, found, "root must have 'Getting Started' group")
}

func TestRootHelp_GettingStartedAppearsFirst(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	groups := root.Groups()

	require.NotEmpty(t, groups, "root must have groups")
	assert.Equal(t, "start", groups[0].ID,
		"'Getting Started' must be the first group, got %q", groups[0].ID)
}

// --- Dynamic state tests ---

func TestGatherGuideState_NoVMs(t *testing.T) {
	setupGuideTest(t)

	root := RootCmd()
	root.SetArgs([]string{"guide"})
	// Create a dummy cobra command for context
	guideCmd, _, _ := root.Find([]string{"guide"})
	require.NotNil(t, guideCmd)

	state := gatherGuideState(guideCmd)
	assert.Empty(t, state.VMs)
	assert.NotEmpty(t, state.Backends)
	assert.True(t, state.Doctor.OK)
}

func TestGatherGuideState_WithVMs(t *testing.T) {
	mb := setupGuideTest(t)
	ctx := context.Background()

	require.NoError(t, mb.Create(ctx, "alpha", backend.VMConfig{
		CPUs: 2, Memory: "4GiB", Disk: "50GiB",
	}))
	require.NoError(t, mb.Create(ctx, "beta", backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB",
	}))
	require.NoError(t, mb.Stop(ctx, "beta"))

	root := RootCmd()
	guideCmd, _, _ := root.Find([]string{"guide"})
	require.NotNil(t, guideCmd)

	state := gatherGuideState(guideCmd)
	assert.Len(t, state.VMs, 2)
}

func TestGatherGuideState_BackendUnavailable(t *testing.T) {
	newRootTestEnv(t)

	origGetBackend := getBackendFunc
	origAllBackendNames := allBackendNames
	origLookPath := lookPath
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return nil, fmt.Errorf("no backends registered")
	}
	allBackendNames = func() []string { return []string{"missing"} }
	lookPath = func(name string) (string, error) {
		return "/usr/bin/" + name, nil
	}
	t.Cleanup(func() {
		getBackendFunc = origGetBackend
		allBackendNames = origAllBackendNames
		lookPath = origLookPath
	})

	root := RootCmd()
	guideCmd, _, _ := root.Find([]string{"guide"})
	require.NotNil(t, guideCmd)

	state := gatherGuideState(guideCmd)
	assert.NotEmpty(t, state.Backends)
	assert.False(t, state.Backends[0].Available)
	assert.Empty(t, state.VMs) // no VMs when backend is unavailable
}

func TestGatherGuideState_Modules(t *testing.T) {
	setupGuideTest(t)

	root := RootCmd()
	guideCmd, _, _ := root.Find([]string{"guide"})
	require.NotNil(t, guideCmd)

	state := gatherGuideState(guideCmd)
	assert.NotEmpty(t, state.Modules, "modules list should be populated from builtin modules")
	assert.Contains(t, state.Modules, "base")
	assert.Contains(t, state.Modules, "claude-code")
}

func TestGatherGuideState_DoctorFailures(t *testing.T) {
	newRootTestEnv(t)

	mb := memory.New()
	origGetBackend := getBackendFunc
	origAllBackendNames := allBackendNames
	origLookPath := lookPath
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return mb, nil
	}
	allBackendNames = func() []string { return []string{"memory"} }
	// Simulate limactl missing
	lookPath = func(name string) (string, error) {
		if name == "limactl" {
			return "", &execError{name: name}
		}
		return "/usr/bin/" + name, nil
	}
	t.Cleanup(func() {
		getBackendFunc = origGetBackend
		allBackendNames = origAllBackendNames
		lookPath = origLookPath
		mb.Reset()
	})

	root := RootCmd()
	guideCmd, _, _ := root.Find([]string{"guide"})
	require.NotNil(t, guideCmd)

	state := gatherGuideState(guideCmd)
	assert.False(t, state.Doctor.OK)
	assert.Contains(t, state.Doctor.Issues, "binary_limactl")
}

// --- Agent output dynamic behavior ---

func TestGuideCommand_AgentOutput_DynamicVMs(t *testing.T) {
	mb := setupGuideTest(t)
	ctx := context.Background()

	// First: no VMs
	oldStdout := os.Stdout
	r1, w1, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w1

	root := RootCmd()
	root.SetArgs([]string{"guide", "--agent"})
	require.NoError(t, root.Execute())

	w1.Close()
	os.Stdout = oldStdout

	var buf1 bytes.Buffer
	_, _ = buf1.ReadFrom(r1)
	noVMOutput := buf1.String()
	assert.Contains(t, noVMOutput, "VMs: none")

	// Second: create a VM
	require.NoError(t, mb.Create(ctx, "dynamic-vm", backend.VMConfig{
		CPUs: 2, Memory: "4GiB", Disk: "50GiB",
	}))

	r2, w2, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w2

	root = RootCmd()
	root.SetArgs([]string{"guide", "--agent"})
	require.NoError(t, root.Execute())

	w2.Close()
	os.Stdout = oldStdout

	var buf2 bytes.Buffer
	_, _ = buf2.ReadFrom(r2)
	withVMOutput := buf2.String()
	assert.Contains(t, withVMOutput, "dynamic-vm")
	assert.NotContains(t, withVMOutput, "VMs: none")
}

// --- collectCommands unit tests ---

func TestCollectCommands_IncludesVisibleCommands(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmds := collectCommands(root)
	assert.NotEmpty(t, cmds)

	// Build a set of top-level names
	nameSet := make(map[string]bool)
	for _, c := range cmds {
		// Top-level commands have single-word names
		parts := strings.Fields(c.Name)
		nameSet[parts[0]] = true
	}

	// Check a few known commands are present
	assert.True(t, nameSet["guide"], "should include guide")
	assert.True(t, nameSet["list"], "should include list")
	assert.True(t, nameSet["doctor"], "should include doctor")
	assert.True(t, nameSet["version"], "should include version")

	// help and completion should be excluded
	assert.False(t, nameSet["help"], "should not include help")
	assert.False(t, nameSet["completion"], "should not include completion")
}

func TestCollectCommands_HasRequiredFields(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmds := collectCommands(root)

	for _, c := range cmds {
		assert.NotEmpty(t, c.Name, "every command must have a name")
		assert.NotEmpty(t, c.Usage, "every command must have usage")
	}
}

// Property: JSON output always has valid structure regardless of state
func TestProperty_GuideJSONAlwaysValid(t *testing.T) {
	cases := []struct {
		name   string
		vmDefs []struct {
			name   string
			config backend.VMConfig
		}
	}{
		{"no_vms", nil},
		{"one_vm", []struct {
			name   string
			config backend.VMConfig
		}{
			{"test", backend.VMConfig{CPUs: 2, Memory: "4GiB", Disk: "50GiB"}},
		}},
		{"many_vms", []struct {
			name   string
			config backend.VMConfig
		}{
			{"a", backend.VMConfig{CPUs: 1, Memory: "1GiB", Disk: "10GiB"}},
			{"b", backend.VMConfig{CPUs: 2, Memory: "2GiB", Disk: "20GiB"}},
			{"c", backend.VMConfig{CPUs: 4, Memory: "4GiB", Disk: "40GiB"}},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mb := setupGuideTest(t)
			ctx := context.Background()

			for _, vm := range tc.vmDefs {
				require.NoError(t, mb.Create(ctx, vm.name, vm.config))
			}

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"--json", "guide"})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)

			var result map[string]any
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err, "JSON must always parse for case %s: %s", tc.name, buf.String())
			assert.True(t, result["ok"].(bool))

			data, ok := result["data"].(map[string]any)
			require.True(t, ok, "data must be an object")

			// All required fields present
			for _, field := range []string{"backends", "vms", "modules", "doctor", "commands", "guide_text"} {
				_, exists := data[field]
				assert.True(t, exists, "field %s must exist for case %s", field, tc.name)
			}

			// VMs count matches
			vms := data["vms"].([]any)
			assert.Len(t, vms, len(tc.vmDefs), "VM count must match for case %s", tc.name)
		})
	}
}
