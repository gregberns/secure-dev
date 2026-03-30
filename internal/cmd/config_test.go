// Package cmd provides tests for the config command and subcommands.
// REQ-005-009: Config Get
// REQ-005-010: Config Set
// REQ-005-011: Config List
// REQ-005-012: Config Edit
// REQ-005-013: Config Validate
package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/config"
	"sd/internal/ui"
)

// setupConfigTest creates an isolated test environment for config commands.
func setupConfigTest(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("SD_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)
	resetRootFlags(t)
	return tmpDir
}

// writeConfigFile writes a config file in the given directory.
func writeConfigFile(t *testing.T, dir, content string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0600)
	require.NoError(t, err)
}

// captureConfigOutput runs a command and captures stdout.
func captureConfigOutput(t *testing.T, args []string) (string, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := RootCmd()
	root.SetArgs(args)
	err := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String(), err
}

// ---
// Registration
// ---

func TestConfigCommand_Registered(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "config" {
			found = true
			assert.Equal(t, "config", cmd.GroupID)
			subNames := make(map[string]bool)
			for _, sub := range cmd.Commands() {
				subNames[sub.Name()] = true
			}
			assert.True(t, subNames["get"], "config must have 'get' subcommand")
			assert.True(t, subNames["set"], "config must have 'set' subcommand")
			assert.True(t, subNames["list"], "config must have 'list' subcommand")
			assert.True(t, subNames["edit"], "config must have 'edit' subcommand")
			assert.True(t, subNames["validate"], "config must have 'validate' subcommand")
			break
		}
	}
	assert.True(t, found, "config command must be registered")
}

// ---
// config get (REQ-005-009)
// ---

func TestConfigGet_HumanOutput(t *testing.T) {
	tmpDir := setupConfigTest(t)
	writeConfigFile(t, tmpDir, "defaults:\n  backend: docker\n")

	output, err := captureConfigOutput(t, []string{"config", "get", "defaults.backend"})
	require.NoError(t, err)
	assert.Contains(t, output, "docker")
	assert.Contains(t, output, "user-level config")
}

func TestConfigGet_JSONOutput(t *testing.T) {
	tmpDir := setupConfigTest(t)
	writeConfigFile(t, tmpDir, "defaults:\n  backend: docker\n")

	output, err := captureConfigOutput(t, []string{"--json", "config", "get", "defaults.backend"})
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err, "output must be valid JSON: %s", output)

	assert.True(t, result["ok"].(bool))
	data := result["data"].(map[string]any)
	assert.Equal(t, "defaults.backend", data["key"])
	assert.Equal(t, "docker", data["value"])
	assert.Equal(t, "user-level config", data["source"])
}

func TestConfigGet_DefaultsNoFile(t *testing.T) {
	setupConfigTest(t)

	output, err := captureConfigOutput(t, []string{"config", "get", "defaults.backend"})
	require.NoError(t, err)
	assert.Contains(t, output, "lima")
	assert.Contains(t, output, "built-in default")
}

func TestConfigGet_AllDefaultsWithJSON(t *testing.T) {
	setupConfigTest(t)

	keys := []string{"defaults.backend", "defaults.cpus", "defaults.memory", "defaults.disk", "defaults.image"}
	expectedDefaults := map[string]any{
		"defaults.backend": "lima",
		"defaults.cpus":    float64(4),
		"defaults.memory":  "8GiB",
		"defaults.disk":    "100GiB",
		"defaults.image":   "ubuntu:24.04",
	}

	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			output, err := captureConfigOutput(t, []string{"--json", "config", "get", key})
			require.NoError(t, err)

			var result map[string]any
			err = json.Unmarshal([]byte(output), &result)
			require.NoError(t, err)

			data := result["data"].(map[string]any)
			assert.Equal(t, key, data["key"])
			assert.Equal(t, expectedDefaults[key], data["value"])
			assert.Equal(t, "built-in default", data["source"])
		})
	}
}

func TestConfigGet_UnknownKey(t *testing.T) {
	setupConfigTest(t)

	root := RootCmd()
	root.SetArgs([]string{"config", "get", "nonexistent.key"})
	err := root.Execute()
	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
	assert.Contains(t, cliErr.Message, "unknown config key")
}

func TestConfigGet_EmptyKey(t *testing.T) {
	setupConfigTest(t)

	root := RootCmd()
	root.SetArgs([]string{"config", "get"})
	err := root.Execute()
	require.Error(t, err)
}

func TestConfigGet_VMDefault(t *testing.T) {
	setupConfigTest(t)

	output, err := captureConfigOutput(t, []string{"config", "get", "defaults.vm"})
	require.NoError(t, err)
	assert.Contains(t, output, "(not set)")
	assert.Contains(t, output, "built-in default")
}

// ---
// config set (REQ-005-010)
// ---

func TestConfigSet_CreatesFile(t *testing.T) {
	tmpDir := setupConfigTest(t)

	root := RootCmd()
	root.SetArgs([]string{"config", "set", "defaults.backend", "docker"})
	err := root.Execute()
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(tmpDir, "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "docker")
}

func TestConfigSet_UpdatesExisting(t *testing.T) {
	tmpDir := setupConfigTest(t)
	writeConfigFile(t, tmpDir, "defaults:\n  backend: lima\n")

	root := RootCmd()
	root.SetArgs([]string{"config", "set", "defaults.backend", "docker"})
	err := root.Execute()
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(tmpDir, "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "docker")
}

func TestConfigSet_JSONOutput(t *testing.T) {
	setupConfigTest(t)

	output, err := captureConfigOutput(t, []string{"--json", "config", "set", "defaults.backend", "docker"})
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err, "output must be valid JSON: %s", output)

	assert.True(t, result["ok"].(bool))
	data := result["data"].(map[string]any)
	assert.Equal(t, "defaults.backend", data["key"])
	assert.Equal(t, "docker", data["value"])
	assert.Contains(t, data["file"], "config.yaml")
	assert.Equal(t, true, data["updated"])
}

func TestConfigSet_NumericValue(t *testing.T) {
	tmpDir := setupConfigTest(t)

	root := RootCmd()
	root.SetArgs([]string{"config", "set", "defaults.cpus", "8"})
	err := root.Execute()
	require.NoError(t, err)

	output, err := captureConfigOutput(t, []string{"--json", "config", "get", "defaults.cpus"})
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err)
	data := result["data"].(map[string]any)
	assert.Equal(t, float64(8), data["value"])
	_ = tmpDir
}

func TestConfigSet_ArrayValue(t *testing.T) {
	setupConfigTest(t)

	root := RootCmd()
	root.SetArgs([]string{"config", "set", "security.egress_allowlist", `["api.anthropic.com","github.com"]`})
	err := root.Execute()
	require.NoError(t, err)

	output, err := captureConfigOutput(t, []string{"config", "get", "security.egress_allowlist"})
	require.NoError(t, err)
	assert.Contains(t, output, "[2 entries]")
}

func TestConfigSet_MissingArgs(t *testing.T) {
	setupConfigTest(t)

	root := RootCmd()
	root.SetArgs([]string{"config", "set", "defaults.backend"})
	err := root.Execute()
	require.Error(t, err)
}

// ---
// config list (REQ-005-011)
// ---

func TestConfigList_DefaultsNoFile(t *testing.T) {
	setupConfigTest(t)

	output, err := captureConfigOutput(t, []string{"config", "list"})
	require.NoError(t, err)

	assert.Contains(t, output, "KEY")
	assert.Contains(t, output, "VALUE")
	assert.Contains(t, output, "SOURCE")
	assert.Contains(t, output, "defaults.backend")
	assert.Contains(t, output, "lima")
	assert.Contains(t, output, "built-in default")
	assert.Contains(t, output, "defaults.cpus")
	assert.Contains(t, output, "defaults.memory")
	assert.Contains(t, output, "8GiB")
	assert.Contains(t, output, "defaults.disk")
	assert.Contains(t, output, "100GiB")
	assert.Contains(t, output, "defaults.image")
	assert.Contains(t, output, "ubuntu:24.04")
	assert.Contains(t, output, "defaults.vm")
	assert.Contains(t, output, "security.mount_policy")
	assert.Contains(t, output, "security.egress_allowlist")
}

func TestConfigList_WithConfig(t *testing.T) {
	tmpDir := setupConfigTest(t)
	writeConfigFile(t, tmpDir, "defaults:\n  backend: docker\n  cpus: 8\n")

	output, err := captureConfigOutput(t, []string{"config", "list"})
	require.NoError(t, err)
	assert.Contains(t, output, "docker")
	assert.Contains(t, output, "user-level config")
	assert.Contains(t, output, "built-in default")
}

func TestConfigList_JSONOutput(t *testing.T) {
	setupConfigTest(t)

	output, err := captureConfigOutput(t, []string{"--json", "config", "list"})
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err, "output must be valid JSON: %s", output)

	assert.True(t, result["ok"].(bool))
	data, ok := result["data"].([]any)
	require.True(t, ok, "data must be a JSON array")
	assert.Len(t, data, len(knownConfigKeys), "should list all known keys")

	for _, item := range data {
		entry := item.(map[string]any)
		assert.Contains(t, entry, "key")
		assert.Contains(t, entry, "value")
		assert.Contains(t, entry, "source")
	}
}

func TestConfigList_RejectsExtraArgs(t *testing.T) {
	setupConfigTest(t)

	root := RootCmd()
	root.SetArgs([]string{"config", "list", "extra"})
	err := root.Execute()
	require.Error(t, err)
}

// ---
// config validate (REQ-005-013)
// ---

func TestConfigValidate_NoFiles(t *testing.T) {
	setupConfigTest(t)

	output, err := captureConfigOutput(t, []string{"config", "validate"})
	require.NoError(t, err)
	assert.Contains(t, output, "No config files found")
}

func TestConfigValidate_ValidFile(t *testing.T) {
	tmpDir := setupConfigTest(t)
	writeConfigFile(t, tmpDir, "defaults:\n  backend: lima\n")

	output, err := captureConfigOutput(t, []string{"config", "validate"})
	require.NoError(t, err)
	assert.Contains(t, output, "ok")
	assert.Contains(t, output, "All config files valid")
}

func TestConfigValidate_InvalidYAML(t *testing.T) {
	tmpDir := setupConfigTest(t)
	writeConfigFile(t, tmpDir, "defaults:\n  backend: lima\n  invalid: [unclosed\n")

	root := RootCmd()
	root.SetArgs([]string{"config", "validate"})
	err := root.Execute()
	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_config", cliErr.Code)
}

func TestConfigValidate_InvalidMountPolicy(t *testing.T) {
	tmpDir := setupConfigTest(t)
	writeConfigFile(t, tmpDir, "security:\n  mount_policy: invalid_value\n")

	root := RootCmd()
	root.SetArgs([]string{"config", "validate"})
	err := root.Execute()
	require.Error(t, err)
}

func TestConfigValidate_JSONOutput_Valid(t *testing.T) {
	tmpDir := setupConfigTest(t)
	writeConfigFile(t, tmpDir, "defaults:\n  backend: lima\n")

	output, err := captureConfigOutput(t, []string{"--json", "config", "validate"})
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err)

	assert.True(t, result["ok"].(bool))
	data := result["data"].(map[string]any)
	assert.Equal(t, true, data["valid"])
	files, ok := data["files"].([]any)
	require.True(t, ok)
	for _, f := range files {
		fileEntry := f.(map[string]any)
		assert.Equal(t, true, fileEntry["valid"])
	}
}

func TestConfigValidate_JSONOutput_Invalid(t *testing.T) {
	tmpDir := setupConfigTest(t)
	writeConfigFile(t, tmpDir, "defaults:\n  backend: lima\n  invalid: [unclosed\n")

	output, _ := captureConfigOutput(t, []string{"--json", "config", "validate"})

	var result map[string]any
	err := json.Unmarshal([]byte(output), &result)
	require.NoError(t, err)

	data := result["data"].(map[string]any)
	assert.Equal(t, false, data["valid"])
}

// ---
// config edit (REQ-005-012)
// ---

func TestConfigEdit_RejectsJSONMode(t *testing.T) {
	setupConfigTest(t)

	root := RootCmd()
	root.SetArgs([]string{"--json", "config", "edit"})
	err := root.Execute()
	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
	assert.Contains(t, cliErr.Message, "interactive")
}

func TestConfigEdit_OpensEditor(t *testing.T) {
	setupConfigTest(t)

	var editorCalledWith []string
	origRunEditor := runEditorCmd
	runEditorCmd = func(editor string, args ...string) error {
		editorCalledWith = append([]string{editor}, args...)
		return nil
	}
	t.Cleanup(func() { runEditorCmd = origRunEditor })

	root := RootCmd()
	root.SetArgs([]string{"config", "edit"})
	err := root.Execute()
	require.NoError(t, err)

	assert.Equal(t, "vi", editorCalledWith[0])
	assert.Contains(t, editorCalledWith[1], "config.yaml")
}

func TestConfigEdit_CreatesTemplate(t *testing.T) {
	setupConfigTest(t)

	origRunEditor := runEditorCmd
	runEditorCmd = func(editor string, args ...string) error {
		data, err := os.ReadFile(args[0])
		require.NoError(t, err)
		assert.Contains(t, string(data), "# sd configuration file")
		return nil
	}
	t.Cleanup(func() { runEditorCmd = origRunEditor })

	root := RootCmd()
	root.SetArgs([]string{"config", "edit"})
	err := root.Execute()
	require.NoError(t, err)
}

func TestConfigEdit_UsesEditorEnv(t *testing.T) {
	setupConfigTest(t)
	t.Setenv("EDITOR", "nano")

	var capturedEditor string
	origRunEditor := runEditorCmd
	runEditorCmd = func(editor string, args ...string) error {
		capturedEditor = editor
		return nil
	}
	t.Cleanup(func() { runEditorCmd = origRunEditor })

	root := RootCmd()
	root.SetArgs([]string{"config", "edit"})
	err := root.Execute()
	require.NoError(t, err)
	assert.Equal(t, "nano", capturedEditor)
}

func TestConfigEdit_EditorFailure(t *testing.T) {
	setupConfigTest(t)

	origRunEditor := runEditorCmd
	runEditorCmd = func(editor string, args ...string) error {
		return fmt.Errorf("editor crashed")
	}
	t.Cleanup(func() { runEditorCmd = origRunEditor })

	root := RootCmd()
	root.SetArgs([]string{"config", "edit"})
	err := root.Execute()
	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "config_edit_failed", cliErr.Code)
}

// ---
// Unit tests for helper functions
// ---

func TestIsKnownConfigKey(t *testing.T) {
	assert.True(t, isKnownConfigKey("defaults.backend"))
	assert.True(t, isKnownConfigKey("defaults.cpus"))
	assert.True(t, isKnownConfigKey("defaults.memory"))
	assert.True(t, isKnownConfigKey("defaults.disk"))
	assert.True(t, isKnownConfigKey("defaults.image"))
	assert.True(t, isKnownConfigKey("defaults.vm"))
	assert.True(t, isKnownConfigKey("security.mount_policy"))
	assert.True(t, isKnownConfigKey("security.egress_allowlist"))
	assert.True(t, isKnownConfigKey("security.sensitive_paths"))
	assert.False(t, isKnownConfigKey("unknown.key"))
	assert.False(t, isKnownConfigKey(""))
}

func TestFormatConfigValue(t *testing.T) {
	assert.Equal(t, "(not set)", formatConfigValue(nil))
	assert.Equal(t, "(not set)", formatConfigValue(""))
	assert.Equal(t, "hello", formatConfigValue("hello"))
	assert.Equal(t, "[3 entries]", formatConfigValue([]interface{}{"a", "b", "c"}))
	assert.Equal(t, "[2 entries]", formatConfigValue([]string{"a", "b"}))
	assert.Equal(t, "42", formatConfigValue(42))
	assert.Equal(t, "3.14", formatConfigValue(3.14))
}

func TestParseConfigValue(t *testing.T) {
	// JSON array
	v := parseConfigValue("security.egress_allowlist", `["a","b"]`)
	arr, ok := v.([]string)
	assert.True(t, ok)
	assert.Equal(t, []string{"a", "b"}, arr)

	// Numeric key
	v = parseConfigValue("defaults.cpus", "8")
	n, ok := v.(int)
	assert.True(t, ok)
	assert.Equal(t, 8, n)

	// Plain string
	v = parseConfigValue("defaults.backend", "docker")
	s, ok := v.(string)
	assert.True(t, ok)
	assert.Equal(t, "docker", s)
}

func TestResolveEditor(t *testing.T) {
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	assert.Equal(t, "vi", resolveEditor())

	t.Setenv("EDITOR", "emacs")
	assert.Equal(t, "emacs", resolveEditor())

	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "code")
	assert.Equal(t, "code", resolveEditor())
}

func TestFormatConfigTable(t *testing.T) {
	entries := []configEntry{
		{Key: "defaults.backend", Value: "lima", Source: "built-in default"},
		{Key: "defaults.vm", Value: "", Source: "built-in default"},
	}
	output := formatConfigTable(entries)
	assert.Contains(t, output, "KEY")
	assert.Contains(t, output, "VALUE")
	assert.Contains(t, output, "SOURCE")
	assert.Contains(t, output, "defaults.backend")
	assert.Contains(t, output, "lima")
	assert.Contains(t, output, "(not set)")
}

func TestFormatValidateOutput(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		assert.Equal(t, "No config files found.\n", formatValidateOutput(nil))
	})

	t.Run("all valid", func(t *testing.T) {
		results := []config.ValidationResult{
			{Path: "/tmp/config.yaml", Valid: true},
		}
		output := formatValidateOutput(results)
		assert.Contains(t, output, "ok")
		assert.Contains(t, output, "All config files valid")
	})

	t.Run("with errors", func(t *testing.T) {
		results := []config.ValidationResult{
			{Path: "/tmp/config.yaml", Valid: false, Errors: []string{"bad yaml"}},
		}
		output := formatValidateOutput(results)
		assert.Contains(t, output, "FAILED")
		assert.Contains(t, output, "bad yaml")
		assert.NotContains(t, output, "All config files valid")
	})
}

// ---
// Property-based tests
// ---

func TestProperty_ConfigGetJSONAlwaysValid(t *testing.T) {
	keys := []string{
		"defaults.backend", "defaults.cpus", "defaults.memory",
		"defaults.disk", "defaults.image", "defaults.vm",
		"security.mount_policy", "security.egress_allowlist",
	}

	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			setupConfigTest(t)
			output, err := captureConfigOutput(t, []string{"--json", "config", "get", key})
			require.NoError(t, err)

			var result map[string]any
			err = json.Unmarshal([]byte(output), &result)
			require.NoError(t, err, "JSON must always parse for key %s: %s", key, output)

			assert.Contains(t, result, "ok")
			assert.Contains(t, result, "data")
			data := result["data"].(map[string]any)
			assert.Contains(t, data, "key")
			assert.Contains(t, data, "value")
			assert.Contains(t, data, "source")
			assert.Equal(t, key, data["key"])
		})
	}
}

func TestProperty_ConfigListJSONAlwaysValid(t *testing.T) {
	cases := []struct {
		name   string
		config string
	}{
		{"no_config", ""},
		{"with_backend", "defaults:\n  backend: docker\n"},
		{"full_config", "defaults:\n  backend: lima\n  cpus: 8\n  memory: 16GiB\nsecurity:\n  mount_policy: readonly\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := setupConfigTest(t)
			if tc.config != "" {
				writeConfigFile(t, tmpDir, tc.config)
			}

			output, err := captureConfigOutput(t, []string{"--json", "config", "list"})
			require.NoError(t, err)

			var result map[string]any
			err = json.Unmarshal([]byte(output), &result)
			require.NoError(t, err, "JSON must always parse: %s", output)

			assert.Contains(t, result, "ok")
			data, ok := result["data"].([]any)
			require.True(t, ok, "data must be array")
			assert.Len(t, data, len(knownConfigKeys))

			for _, item := range data {
				entry := item.(map[string]any)
				assert.Contains(t, entry, "key")
				assert.Contains(t, entry, "value")
				assert.Contains(t, entry, "source")
			}
		})
	}
}

func TestProperty_ConfigValidateJSONAlwaysValid(t *testing.T) {
	cases := []struct {
		name   string
		config string
	}{
		{"no_files", ""},
		{"valid", "defaults:\n  backend: lima\n"},
		{"invalid", "defaults:\n  backend: [broken\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := setupConfigTest(t)
			if tc.config != "" {
				writeConfigFile(t, tmpDir, tc.config)
			}

			output, _ := captureConfigOutput(t, []string{"--json", "config", "validate"})

			var result map[string]any
			err := json.Unmarshal([]byte(output), &result)
			require.NoError(t, err, "JSON must always parse: %s", output)

			assert.Contains(t, result, "ok")
			data, ok := result["data"].(map[string]any)
			require.True(t, ok)
			assert.Contains(t, data, "valid")
			assert.Contains(t, data, "files")
		})
	}
}

func TestProperty_ConfigErrorCodesSnakeCase(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code string
		setup func(t *testing.T, tmpDir string)
	}{
		{"unknown_key", []string{"config", "get", "bad.key"}, "invalid_argument", nil},
		{"validate_invalid", []string{"config", "validate"}, "invalid_config", func(t *testing.T, tmpDir string) {
			writeConfigFile(t, tmpDir, "defaults:\n  backend: [broken\n")
		}},
		{"edit_json_mode", []string{"--json", "config", "edit"}, "invalid_argument", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := setupConfigTest(t)
			if tc.setup != nil {
				tc.setup(t, tmpDir)
			}

			root := RootCmd()
			root.SetArgs(tc.args)
			err := root.Execute()
			require.Error(t, err)
			cliErr, ok := err.(ui.CLIError)
			require.True(t, ok, "error must be CLIError for %s", tc.name)
			assert.Equal(t, tc.code, cliErr.Code)
			assert.NotContains(t, cliErr.Code, " ")
			assert.True(t, cliErr.Code == strings.ToLower(cliErr.Code),
				"code must be lowercase: %s", cliErr.Code)
		})
	}
}

func TestProperty_ConfigListContainsAllKnownKeys(t *testing.T) {
	setupConfigTest(t)

	output, err := captureConfigOutput(t, []string{"config", "list"})
	require.NoError(t, err)

	for _, key := range knownConfigKeys {
		assert.Contains(t, output, key, "config list must contain key %s", key)
	}
}

func TestProperty_ConfigSetGetRoundTrip(t *testing.T) {
	keys := []struct {
		key   string
		value string
	}{
		{"defaults.backend", "docker"},
		{"defaults.memory", "16GiB"},
		{"defaults.image", "ubuntu:22.04"},
	}

	for _, tc := range keys {
		t.Run(tc.key, func(t *testing.T) {
			setupConfigTest(t)

			root := RootCmd()
			root.SetArgs([]string{"config", "set", tc.key, tc.value})
			err := root.Execute()
			require.NoError(t, err)

			output, err := captureConfigOutput(t, []string{"--json", "config", "get", tc.key})
			require.NoError(t, err)

			var result map[string]any
			err = json.Unmarshal([]byte(output), &result)
			require.NoError(t, err)

			data := result["data"].(map[string]any)
			assert.Equal(t, tc.value, data["value"], "round-trip: set %s=%s should be readable", tc.key, tc.value)
		})
	}
}

// ---
// Source tracking tests
// ---

func TestConfigGet_SourceEnv(t *testing.T) {
	setupConfigTest(t)
	t.Setenv("SD_BACKEND", "docker")

	output, err := captureConfigOutput(t, []string{"--json", "config", "get", "defaults.backend"})
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err)

	data := result["data"].(map[string]any)
	assert.Equal(t, "docker", data["value"])
	assert.Equal(t, "environment variable", data["source"])
}

func TestConfigGet_SourceUserConfig(t *testing.T) {
	tmpDir := setupConfigTest(t)
	writeConfigFile(t, tmpDir, "defaults:\n  backend: docker\n")

	output, err := captureConfigOutput(t, []string{"--json", "config", "get", "defaults.backend"})
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err)

	data := result["data"].(map[string]any)
	assert.Equal(t, "docker", data["value"])
	assert.Equal(t, "user-level config", data["source"])
}

func TestConfigGet_SourceDefault(t *testing.T) {
	setupConfigTest(t)

	output, err := captureConfigOutput(t, []string{"--json", "config", "get", "defaults.backend"})
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err)

	data := result["data"].(map[string]any)
	assert.Equal(t, "lima", data["value"])
	assert.Equal(t, "built-in default", data["source"])
}
