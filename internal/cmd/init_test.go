// Package cmd provides tests for the init command.
// REQ-005-024: sd init generates .sd.yaml template
// NOTE: Tests use global getWorkingDir -- do not use t.Parallel().
package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sd/internal/config"
)

func setupInitTest(t *testing.T) string {
	t.Helper()
	dir := newRootTestEnv(t)

	// Override getWorkingDir for tests
	origGetWD := getWorkingDir
	getWorkingDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { getWorkingDir = origGetWD })

	// Reset init command's local flags to prevent accumulation
	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "init" {
			resetSliceFlag(cmd, "modules")
			_ = cmd.Flags().Set("force", "false")
			cmd.Flags().Lookup("force").Changed = false
			break
		}
	}

	return dir
}

// --- Registration tests ---

func TestInitCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "init" {
			found = true
			assert.Equal(t, "config", cmd.GroupID)
			break
		}
	}
	assert.True(t, found, "init command must be registered")
}

// --- Core logic tests ---

func TestInitCommand_GeneratesSDYaml(t *testing.T) {
	dir := setupInitTest(t)

	root := RootCmd()
	root.SetArgs([]string{"init"})
	err := root.Execute()
	require.NoError(t, err)

	// Verify .sd.yaml was created
	sdYaml := filepath.Join(dir, ".sd.yaml")
	data, err := os.ReadFile(sdYaml)
	require.NoError(t, err)
	assert.Contains(t, string(data), "name:")
	assert.Contains(t, string(data), "modules:")
	assert.Contains(t, string(data), "mounts:")
}

func TestInitCommand_DetectsGoProject(t *testing.T) {
	dir := setupInitTest(t)

	// Create a go.mod file to trigger Go detection
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0644))

	root := RootCmd()
	root.SetArgs([]string{"init"})
	err := root.Execute()
	require.NoError(t, err)

	// Read and verify the config includes golang module
	sdYaml := filepath.Join(dir, ".sd.yaml")
	cfg, loadErr := config.LoadProjectConfig(sdYaml)
	require.NoError(t, loadErr)
	assert.Contains(t, cfg.Modules, "golang")
	assert.Contains(t, cfg.Modules, "base")
}

func TestInitCommand_DetectsNodeProject(t *testing.T) {
	dir := setupInitTest(t)

	// Create package.json
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644))

	root := RootCmd()
	root.SetArgs([]string{"init"})
	err := root.Execute()
	require.NoError(t, err)

	sdYaml := filepath.Join(dir, ".sd.yaml")
	cfg, loadErr := config.LoadProjectConfig(sdYaml)
	require.NoError(t, loadErr)
	assert.Contains(t, cfg.Modules, "nodejs")
}

func TestInitCommand_NoOverwriteWithoutForce(t *testing.T) {
	dir := setupInitTest(t)

	// Create existing .sd.yaml
	sdYaml := filepath.Join(dir, ".sd.yaml")
	require.NoError(t, os.WriteFile(sdYaml, []byte("name: existing\n"), 0644))

	root := RootCmd()
	root.SetArgs([]string{"init"})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
	assert.Contains(t, err.Error(), "--force")

	// Verify existing file was not changed
	data, readErr := os.ReadFile(sdYaml)
	require.NoError(t, readErr)
	assert.Contains(t, string(data), "existing")
}

func TestInitCommand_ForceOverwrite(t *testing.T) {
	dir := setupInitTest(t)

	// Create existing .sd.yaml
	sdYaml := filepath.Join(dir, ".sd.yaml")
	require.NoError(t, os.WriteFile(sdYaml, []byte("name: existing\n"), 0644))

	root := RootCmd()
	root.SetArgs([]string{"init", "--force"})
	err := root.Execute()
	require.NoError(t, err)

	// Verify file was overwritten
	data, readErr := os.ReadFile(sdYaml)
	require.NoError(t, readErr)
	assert.NotContains(t, string(data), "existing")
}

func TestInitCommand_ExplicitModules(t *testing.T) {
	dir := setupInitTest(t)

	root := RootCmd()
	root.SetArgs([]string{"init", "--modules=golang,docker"})
	err := root.Execute()
	require.NoError(t, err)

	sdYaml := filepath.Join(dir, ".sd.yaml")
	cfg, loadErr := config.LoadProjectConfig(sdYaml)
	require.NoError(t, loadErr)
	assert.Equal(t, []string{"golang", "docker"}, cfg.Modules)
}

func TestInitCommand_DerivedNameIsValid(t *testing.T) {
	dir := setupInitTest(t)

	root := RootCmd()
	root.SetArgs([]string{"init"})
	err := root.Execute()
	require.NoError(t, err)

	sdYaml := filepath.Join(dir, ".sd.yaml")
	cfg, loadErr := config.LoadProjectConfig(sdYaml)
	require.NoError(t, loadErr)
	// Name should be non-empty and valid
	assert.NotEmpty(t, cfg.Name)
	assert.Regexp(t, `^[a-z][a-z0-9-]{0,62}$`, cfg.Name)
}

// --- JSON output tests ---

func TestInitCommand_JSON(t *testing.T) {
	dir := setupInitTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "init"})
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
	assert.NotEmpty(t, data["path"])
	assert.NotEmpty(t, data["name"])
	assert.NotNil(t, data["modules"])

	// Verify the file was actually created
	sdYaml := filepath.Join(dir, ".sd.yaml")
	_, statErr := os.Stat(sdYaml)
	assert.NoError(t, statErr)
}
