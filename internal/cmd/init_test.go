// Package cmd provides tests for the init command.
// REQ-005-024: sd init generates .sd.yaml template
// NOTE: Tests use global getWorkingDir -- do not use t.Parallel().
package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
	"gopkg.in/yaml.v3"

	"sd/internal/config"
)

func setupInitTest(t *testing.T) string {
	t.Helper()
	dir := newRootTestEnv(t)

	// Override getWorkingDir for tests
	origGetWD := getWorkingDir
	getWorkingDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { getWorkingDir = origGetWD })

	// Override detectGitRemote to return no remote by default
	origDetect := detectGitRemote
	detectGitRemote = func(dir string) string { return "" }
	t.Cleanup(func() { detectGitRemote = origDetect })

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

// bug-init-not-idempotent: sd init is now idempotent. When .sd.yaml already
// exists and --force is not passed, the command is a no-op that exits 0 with
// a message to stderr (human mode) or a structured result on stdout (JSON).
func TestInitCommand_NoOverwriteWithoutForce(t *testing.T) {
	dir := setupInitTest(t)

	// Create existing .sd.yaml
	sdYaml := filepath.Join(dir, ".sd.yaml")
	require.NoError(t, os.WriteFile(sdYaml, []byte("name: existing\n"), 0644))

	// Q3: capture BOTH stdout and stderr; stdout must be empty in human mode,
	// stderr must contain the actionable message. Cardinal rule: stdout for
	// data, stderr for messages.
	oldStderr := os.Stderr
	rErr, wErr, pipeErr := os.Pipe()
	require.NoError(t, pipeErr)
	os.Stderr = wErr

	oldStdout := os.Stdout
	rOut, wOut, pipeOutErr := os.Pipe()
	require.NoError(t, pipeOutErr)
	os.Stdout = wOut

	root := RootCmd()
	root.SetArgs([]string{"init"})
	err := root.Execute()

	wErr.Close()
	wOut.Close()
	os.Stderr = oldStderr
	os.Stdout = oldStdout

	var stderrBuf, stdoutBuf bytes.Buffer
	_, _ = stderrBuf.ReadFrom(rErr)
	_, _ = stdoutBuf.ReadFrom(rOut)

	// Idempotent: no error, exit 0.
	require.NoError(t, err, "init with existing .sd.yaml must be a no-op, not an error")
	// Q3: human no-op must keep stdout empty.
	assert.Empty(t, strings.TrimSpace(stdoutBuf.String()),
		"human no-op must write nothing to stdout (got %q)", stdoutBuf.String())

	// Verify stderr message format.
	stderrOut := stderrBuf.String()
	assert.Contains(t, stderrOut, ".sd.yaml already exists at")
	assert.Contains(t, stderrOut, "--force to overwrite")
	assert.Contains(t, stderrOut, sdYaml)

	// Verify existing file was not changed
	data, readErr := os.ReadFile(sdYaml)
	require.NoError(t, readErr)
	assert.Equal(t, "name: existing\n", string(data),
		"existing .sd.yaml must be byte-for-byte preserved on no-op")
}

// bug-init-not-idempotent: JSON mode no-op variant.
func TestInitCommand_NoOverwriteWithoutForce_JSON(t *testing.T) {
	dir := setupInitTest(t)

	sdYaml := filepath.Join(dir, ".sd.yaml")
	require.NoError(t, os.WriteFile(sdYaml, []byte("name: existing\n"), 0644))

	oldStdout := os.Stdout
	r, w, pipeErr := os.Pipe()
	require.NoError(t, pipeErr)
	os.Stdout = w

	// Q3: in JSON mode stderr must be empty (no human message duplicated).
	oldStderr := os.Stderr
	rErr, wErr, pipeErrErr := os.Pipe()
	require.NoError(t, pipeErrErr)
	os.Stderr = wErr

	root := RootCmd()
	root.SetArgs([]string{"--json", "init"})
	execErr := root.Execute()

	w.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var buf, stderrBuf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	_, _ = stderrBuf.ReadFrom(rErr)

	require.NoError(t, execErr, "init with existing .sd.yaml in --json mode must be a no-op")
	// Q3: JSON mode must not duplicate the message on stderr.
	assert.Empty(t, strings.TrimSpace(stderrBuf.String()),
		"JSON no-op must keep stderr empty (got %q)", stderrBuf.String())

	var envelope map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope),
		"JSON output must be valid: %s", buf.String())
	assert.True(t, envelope["ok"].(bool))
	data := envelope["data"].(map[string]any)
	// A2: unified envelope — `action` is the discriminator across noop/create/overwrote.
	assert.Equal(t, "noop", data["action"])
	assert.Equal(t, sdYaml, data["path"])
	assert.Equal(t, "", data["name"], "no-op envelope still carries name field (empty)")
	assert.NotNil(t, data["modules"], "no-op envelope still carries modules field (empty)")

	// Existing file unchanged.
	existing, readErr := os.ReadFile(sdYaml)
	require.NoError(t, readErr)
	assert.Equal(t, "name: existing\n", string(existing))
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

// bug-init-not-idempotent: --force in JSON mode still overwrites and emits
// the regular success envelope (not the no-op envelope).
func TestInitCommand_ForceOverwrite_JSON(t *testing.T) {
	dir := setupInitTest(t)

	sdYaml := filepath.Join(dir, ".sd.yaml")
	require.NoError(t, os.WriteFile(sdYaml, []byte("name: existing\n"), 0644))

	oldStdout := os.Stdout
	r, w, pipeErr := os.Pipe()
	require.NoError(t, pipeErr)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "init", "--force"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var envelope map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope),
		"JSON output must be valid: %s", buf.String())
	assert.True(t, envelope["ok"].(bool))
	data := envelope["data"].(map[string]any)
	// A2: unified envelope — overwrite emits action=overwrote.
	assert.Equal(t, "overwrote", data["action"])
	assert.NotEmpty(t, data["path"])
	assert.NotEmpty(t, data["name"])

	// File was actually overwritten.
	contents, readErr := os.ReadFile(sdYaml)
	require.NoError(t, readErr)
	assert.NotContains(t, string(contents), "existing")
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

// --- REQ-009-012: sd init template updates ---

func TestInitCommand_GitRemotePopulatesRepo(t *testing.T) {
	dir := setupInitTest(t)

	// Override detectGitRemote to simulate a git repo with origin
	detectGitRemote = func(d string) string {
		return "https://github.com/user/my-app.git"
	}

	root := RootCmd()
	root.SetArgs([]string{"init"})
	err := root.Execute()
	require.NoError(t, err)

	sdYaml := filepath.Join(dir, ".sd.yaml")
	data, err := os.ReadFile(sdYaml)
	require.NoError(t, err)

	assert.Contains(t, string(data), "repo: https://github.com/user/my-app.git")
}

func TestInitCommand_NoGitRemoteLeavesRepoAbsent(t *testing.T) {
	dir := setupInitTest(t)
	// detectGitRemote already returns "" from setupInitTest

	root := RootCmd()
	root.SetArgs([]string{"init"})
	err := root.Execute()
	require.NoError(t, err)

	sdYaml := filepath.Join(dir, ".sd.yaml")
	data, err := os.ReadFile(sdYaml)
	require.NoError(t, err)

	assert.NotContains(t, string(data), "repo:")
}

func TestInitCommand_IncludesCommentedPackagesSection(t *testing.T) {
	dir := setupInitTest(t)

	root := RootCmd()
	root.SetArgs([]string{"init"})
	err := root.Execute()
	require.NoError(t, err)

	sdYaml := filepath.Join(dir, ".sd.yaml")
	data, err := os.ReadFile(sdYaml)
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "# packages:")
	assert.Contains(t, content, "#   apt:")
	assert.Contains(t, content, "#   pip:")
	assert.Contains(t, content, "#   npm:")
	assert.Contains(t, content, "#   go:")
	assert.Contains(t, content, "#   cargo:")
}

func TestInitCommand_IncludesCommentedSetupSection(t *testing.T) {
	dir := setupInitTest(t)

	root := RootCmd()
	root.SetArgs([]string{"init"})
	err := root.Execute()
	require.NoError(t, err)

	sdYaml := filepath.Join(dir, ".sd.yaml")
	data, err := os.ReadFile(sdYaml)
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "# setup:")
	assert.Contains(t, content, "#   - mkdir -p ~/bin")
}

func TestInitCommand_SetupSectionIncludesIdempotencyNote(t *testing.T) {
	dir := setupInitTest(t)

	root := RootCmd()
	root.SetArgs([]string{"init"})
	err := root.Execute()
	require.NoError(t, err)

	sdYaml := filepath.Join(dir, ".sd.yaml")
	data, err := os.ReadFile(sdYaml)
	require.NoError(t, err)

	// REQ-009-010: The sd init template includes a comment warning that setup
	// commands should be idempotent.
	content := string(data)
	assert.Contains(t, content, "idempotent")
}

// --- Rapid property-based tests ---

// vmNamePattern matches the valid VM name format: starts with lowercase letter,
// then lowercase letters, digits, or hyphens, 1-63 characters total.
var vmNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// Property: DeriveVMName always produces a name matching ^[a-z][a-z0-9-]{0,62}$
// for any non-empty directory path.
func TestProperty_DeriveVMNameAlwaysValid(t *testing.T) {
	// Generate directory names that exercise edge cases: unicode, digits,
	// special characters, long strings, empty-ish names.
	dirNameGen := rapid.OneOf(
		// Typical directory names
		rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9._-]{0,80}`),
		// Names starting with digits
		rapid.StringMatching(`[0-9][a-z0-9]{0,20}`),
		// Names with special characters
		rapid.StringMatching(`[A-Za-z0-9!@#$%^&()_+=]{1,40}`),
		// Names with unicode
		rapid.StringMatching(`[a-z\x{00e0}-\x{00ff}]{1,20}`),
		// Long names that will be truncated
		rapid.StringMatching(`[a-z]{70,120}`),
		// Names that are all hyphens or dots
		rapid.SampledFrom([]string{"---", "...", "___", "-a-b-", ".hidden"}),
	)

	rapid.Check(t, func(t *rapid.T) {
		dirName := dirNameGen.Draw(t, "dir_name")
		// Build a path with this directory as the base name
		dir := filepath.Join("/tmp", dirName)
		name := config.DeriveVMName(dir)

		assert.NotEmpty(t, name, "DeriveVMName must never return empty for input %q", dirName)
		assert.Regexp(t, vmNamePattern, name,
			"DeriveVMName(%q) = %q must match ^[a-z][a-z0-9-]{0,62}$", dirName, name)
	})
}

// Property: sd init always produces valid YAML that parses without error,
// regardless of the module list provided.
func TestProperty_InitAlwaysProducesValidYAML(t *testing.T) {
	moduleGen := rapid.SliceOfN(
		rapid.SampledFrom([]string{
			"base", "golang", "nodejs", "python", "rust", "docker",
			"claude", "git-lfs", "terraform", "kubectl",
		}),
		0, 5,
	)

	rapid.Check(t, func(rt *rapid.T) {
		dir := setupInitTest(t)
		modules := moduleGen.Draw(rt, "modules")

		root := RootCmd()
		if len(modules) > 0 {
			modArg := ""
			for i, m := range modules {
				if i > 0 {
					modArg += ","
				}
				modArg += m
			}
			root.SetArgs([]string{"init", "--modules=" + modArg})
		} else {
			root.SetArgs([]string{"init"})
		}

		err := root.Execute()
		require.NoError(t, err)

		sdYaml := filepath.Join(dir, ".sd.yaml")
		data, err := os.ReadFile(sdYaml)
		require.NoError(t, err, "must be able to read generated .sd.yaml")
		assert.NotEmpty(t, data, "generated .sd.yaml must not be empty")

		// Strip comment lines (lines starting with #) before parsing,
		// because the file includes commented-out sections that are not valid YAML
		// when mixed in. But yaml.Unmarshal should handle comments fine.
		var parsed map[string]any
		err = yaml.Unmarshal(data, &parsed)
		assert.NoError(t, err, "generated .sd.yaml must be valid YAML; content:\n%s", string(data))

		// The parsed YAML must contain the core keys
		assert.Contains(t, parsed, "name", "YAML must have 'name' key")
		assert.Contains(t, parsed, "modules", "YAML must have 'modules' key")
		assert.Contains(t, parsed, "mounts", "YAML must have 'mounts' key")

		// Clean up for the next rapid iteration (setupInitTest uses global state)
		os.Remove(sdYaml)
	})
}

// Property: sd init never overwrites an existing .sd.yaml without --force.
// For any existing content, running init without --force is a no-op (exits 0,
// per bug-init-not-idempotent) and preserves the original byte-for-byte.
func TestProperty_InitNeverOverwritesWithoutForce(t *testing.T) {
	contentGen := rapid.StringMatching(`name: [a-z]{3,10}\nmodules:\n  - [a-z]{3,8}\n`)

	rapid.Check(t, func(rt *rapid.T) {
		dir := setupInitTest(t)
		originalContent := contentGen.Draw(rt, "original_content")

		sdYaml := filepath.Join(dir, ".sd.yaml")
		require.NoError(t, os.WriteFile(sdYaml, []byte(originalContent), 0644))

		// Swallow stderr so rapid output isn't polluted by the no-op message.
		oldStderr := os.Stderr
		_, wErr, pipeErr := os.Pipe()
		require.NoError(t, pipeErr)
		os.Stderr = wErr

		root := RootCmd()
		root.SetArgs([]string{"init"})
		err := root.Execute()

		wErr.Close()
		os.Stderr = oldStderr

		// Idempotent no-op: exits 0.
		require.NoError(t, err, "init without --force must be a no-op when .sd.yaml exists")

		// Original content must be unchanged
		data, readErr := os.ReadFile(sdYaml)
		require.NoError(t, readErr)
		assert.Equal(t, originalContent, string(data),
			"existing .sd.yaml must not be modified without --force")
	})
}
