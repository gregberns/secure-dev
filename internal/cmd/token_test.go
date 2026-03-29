package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/security"
	"sd/internal/ui"
)

// ---------------------------------------------------------------------------
// Digital Twins
// ---------------------------------------------------------------------------

// mockVMEnvStore is a digital twin for VM credential storage.
type mockVMEnvStore struct {
	envs     map[string]map[string]string // vmName -> env map
	readErr  error
	writeErr error
}

func newMockVMEnvStore() *mockVMEnvStore {
	return &mockVMEnvStore{
		envs: make(map[string]map[string]string),
	}
}

func (m *mockVMEnvStore) read(sdHome, vmName string) (map[string]string, error) {
	if m.readErr != nil {
		return nil, m.readErr
	}
	env, ok := m.envs[vmName]
	if !ok {
		return make(map[string]string), nil
	}
	cp := make(map[string]string, len(env))
	for k, v := range env {
		cp[k] = v
	}
	return cp, nil
}

func (m *mockVMEnvStore) write(sdHome, vmName string, env map[string]string) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	m.envs[vmName] = env
	return nil
}

// setupTokenTest creates a test environment with a mock VM env store.
func setupTokenTest(t *testing.T) *mockVMEnvStore {
	t.Helper()
	_ = newRootTestEnv(t)

	store := newMockVMEnvStore()
	origRead := readVMEnvFunc
	origWrite := writeVMEnvFunc
	readVMEnvFunc = store.read
	writeVMEnvFunc = store.write
	t.Cleanup(func() {
		readVMEnvFunc = origRead
		writeVMEnvFunc = origWrite
	})

	return store
}

// captureStdout runs a root command with the given args and captures stdout.
func captureTokenStdout(t *testing.T, args []string) (string, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs(args)
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.String(), execErr
}

// parseTokenJSON captures stdout and parses as JSON.
func parseTokenJSON(t *testing.T, args []string) (map[string]interface{}, error) {
	t.Helper()
	output, err := captureTokenStdout(t, args)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if jsonErr := json.Unmarshal([]byte(output), &result); jsonErr != nil {
		return nil, fmt.Errorf("invalid JSON: %s", output)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Registration Tests
// ---------------------------------------------------------------------------

func TestTokenCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "token" {
			found = true
			assert.Equal(t, "security", cmd.GroupID)

			// Verify subcommands
			subNames := make(map[string]bool)
			for _, sub := range cmd.Commands() {
				subNames[sub.Name()] = true
			}
			assert.True(t, subNames["github"], "github subcommand must exist")
			assert.True(t, subNames["rotate"], "rotate subcommand must exist")
			assert.True(t, subNames["revoke"], "revoke subcommand must exist")
			assert.True(t, subNames["list"], "list subcommand must exist")
			break
		}
	}
	assert.True(t, found, "token command must be registered")
}

func TestTokenCommand_GithubSetupRegistered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"token", "github", "setup"})
	require.NoError(t, err)
	assert.Equal(t, "setup", cmd.Name())
}

func TestTokenCommand_NoConfigRequired(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()
	cmd, _, err := root.Find([]string{"token"})
	require.NoError(t, err)
	assert.Equal(t, "token", cmd.Name())
	// token is in the no-config-required set, so it works without config file
}

// ---------------------------------------------------------------------------
// GitHub Setup Tests
// ---------------------------------------------------------------------------

func TestTokenGithubSetup_HumanOutput(t *testing.T) {
	setupTokenTest(t)

	output, err := captureTokenStdout(t, []string{"token", "github", "setup"})
	require.NoError(t, err)

	assert.Contains(t, output, "GitHub Fine-Grained Personal Access Token Setup")
	assert.Contains(t, output, "github.com/settings/personal-access-tokens/new")
	assert.Contains(t, output, "contents")
	assert.Contains(t, output, "write")
	assert.Contains(t, output, "pull_requests")
	assert.Contains(t, output, "fine-grained")
}

func TestTokenGithubSetup_JSONOutput(t *testing.T) {
	setupTokenTest(t)

	output, err := captureTokenStdout(t, []string{"--json", "token", "github", "setup"})
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	assert.True(t, result["ok"].(bool))
	data := result["data"].(map[string]interface{})
	assert.Equal(t, "fine-grained", data["pat_type"])
	assert.Contains(t, data["setup_url"].(string), "github.com")
	assert.NotEmpty(t, data["recommended_scopes"])
	assert.NotEmpty(t, data["notes"])

	scopes := data["recommended_scopes"].([]interface{})
	require.Len(t, scopes, 2)
	for _, s := range scopes {
		scope := s.(map[string]interface{})
		assert.Contains(t, scope, "permission")
		assert.Contains(t, scope, "access")
	}
}

func TestTokenGithubSetup_RejectsExtraArgs(t *testing.T) {
	setupTokenTest(t)

	_, err := captureTokenStdout(t, []string{"token", "github", "setup", "extra"})
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Rotate Tests
// ---------------------------------------------------------------------------

func TestTokenRotate_HumanOutput(t *testing.T) {
	store := setupTokenTest(t)

	t.Setenv("GITHUB_TOKEN", "github_pat_test123")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-testkey123")

	output, err := captureTokenStdout(t, []string{"token", "rotate", "testvm"})
	require.NoError(t, err)

	assert.Contains(t, output, "Rotated credentials for VM")
	assert.Contains(t, output, "testvm")
	assert.Contains(t, output, "GITHUB_TOKEN")
	assert.Contains(t, output, "ANTHROPIC_API_KEY")

	env := store.envs["testvm"]
	assert.Equal(t, "github_pat_test123", env["GITHUB_TOKEN"])
	assert.Equal(t, "sk-ant-testkey123", env["ANTHROPIC_API_KEY"])
}

func TestTokenRotate_JSONOutput(t *testing.T) {
	store := setupTokenTest(t)

	t.Setenv("GITHUB_TOKEN", "github_pat_test123")

	output, err := captureTokenStdout(t, []string{"--json", "token", "rotate", "myvm"})
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	assert.True(t, result["ok"].(bool))
	data := result["data"].(map[string]interface{})
	assert.Equal(t, "myvm", data["name"])
	rotated := data["credentials_rotated"].([]interface{})
	assert.Contains(t, rotated, "GITHUB_TOKEN")

	env := store.envs["myvm"]
	assert.Equal(t, "github_pat_test123", env["GITHUB_TOKEN"])
}

func TestTokenRotate_NoCredentials(t *testing.T) {
	setupTokenTest(t)

	os.Unsetenv("GITHUB_TOKEN")
	os.Unsetenv("ANTHROPIC_API_KEY")

	_, err := captureTokenStdout(t, []string{"token", "rotate", "myvm"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no credentials found")
}

func TestTokenRotate_ClassicPATWarning(t *testing.T) {
	store := setupTokenTest(t)

	t.Setenv("GITHUB_TOKEN", "ghp_classicpat123")

	// Capture stderr for warnings
	oldStderr := os.Stderr
	rErr, wErr, _ := os.Pipe()
	os.Stderr = wErr

	_, err := captureTokenStdout(t, []string{"token", "rotate", "myvm"})

	wErr.Close()
	os.Stderr = oldStderr

	var stderrBuf bytes.Buffer
	_, _ = stderrBuf.ReadFrom(rErr)

	require.NoError(t, err)
	assert.Contains(t, stderrBuf.String(), "classic")
	assert.Contains(t, stderrBuf.String(), "ghp_")

	env := store.envs["myvm"]
	assert.Equal(t, "ghp_classicpat123", env["GITHUB_TOKEN"])
}

func TestTokenRotate_OnlyAnthropicKey(t *testing.T) {
	store := setupTokenTest(t)

	os.Unsetenv("GITHUB_TOKEN")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-testkey456")

	output, err := captureTokenStdout(t, []string{"token", "rotate", "myvm"})
	require.NoError(t, err)

	assert.Contains(t, output, "ANTHROPIC_API_KEY")
	assert.NotContains(t, output, "GITHUB_TOKEN")

	env := store.envs["myvm"]
	assert.Equal(t, "sk-ant-testkey456", env["ANTHROPIC_API_KEY"])
	_, hasGithub := env["GITHUB_TOKEN"]
	assert.False(t, hasGithub)
}

func TestTokenRotate_PreservesExistingEnv(t *testing.T) {
	store := setupTokenTest(t)
	store.envs["myvm"] = map[string]string{
		"GITHUB_TOKEN":   "old_token",
		"SOME_OTHER_VAR": "preserved",
	}

	t.Setenv("GITHUB_TOKEN", "new_token")
	os.Unsetenv("ANTHROPIC_API_KEY")

	_, err := captureTokenStdout(t, []string{"token", "rotate", "myvm"})
	require.NoError(t, err)

	env := store.envs["myvm"]
	assert.Equal(t, "new_token", env["GITHUB_TOKEN"])
	assert.Equal(t, "preserved", env["SOME_OTHER_VAR"])
}

func TestTokenRotate_ReadError(t *testing.T) {
	store := setupTokenTest(t)
	store.readErr = fmt.Errorf("disk error")

	t.Setenv("GITHUB_TOKEN", "github_pat_test")

	_, err := captureTokenStdout(t, []string{"token", "rotate", "myvm"})
	assert.Error(t, err)
}

func TestTokenRotate_WriteError(t *testing.T) {
	store := setupTokenTest(t)
	store.writeErr = fmt.Errorf("disk full")

	t.Setenv("GITHUB_TOKEN", "github_pat_test")

	_, err := captureTokenStdout(t, []string{"token", "rotate", "myvm"})
	assert.Error(t, err)
}

func TestTokenRotate_MissingVM(t *testing.T) {
	setupTokenTest(t)

	_, err := captureTokenStdout(t, []string{"token", "rotate"})
	assert.Error(t, err)
}

func TestTokenRotate_EmptyVM(t *testing.T) {
	setupTokenTest(t)

	_, err := captureTokenStdout(t, []string{"token", "rotate", ""})
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Revoke Tests
// ---------------------------------------------------------------------------

func TestTokenRevoke_HumanOutput(t *testing.T) {
	store := setupTokenTest(t)
	store.envs["myvm"] = map[string]string{
		"GITHUB_TOKEN":    "github_pat_test",
		"ANTHROPIC_API_KEY": "sk-ant-test",
	}

	output, err := captureTokenStdout(t, []string{"token", "revoke", "myvm"})
	require.NoError(t, err)

	assert.Contains(t, output, "Revoked credentials for VM")
	assert.Contains(t, output, "myvm")
	assert.Contains(t, output, "GITHUB_TOKEN")
	assert.Contains(t, output, "ANTHROPIC_API_KEY")

	env := store.envs["myvm"]
	_, hasGithub := env["GITHUB_TOKEN"]
	_, hasAnthropic := env["ANTHROPIC_API_KEY"]
	assert.False(t, hasGithub)
	assert.False(t, hasAnthropic)
}

func TestTokenRevoke_JSONOutput(t *testing.T) {
	store := setupTokenTest(t)
	store.envs["myvm"] = map[string]string{
		"GITHUB_TOKEN": "github_pat_test",
	}

	output, err := captureTokenStdout(t, []string{"--json", "token", "revoke", "myvm"})
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	assert.True(t, result["ok"].(bool))
	data := result["data"].(map[string]interface{})
	assert.Equal(t, "myvm", data["name"])
	revoked := data["credentials_revoked"].([]interface{})
	assert.Contains(t, revoked, "GITHUB_TOKEN")
}

func TestTokenRevoke_NoCredentials(t *testing.T) {
	setupTokenTest(t)

	output, err := captureTokenStdout(t, []string{"token", "revoke", "myvm"})
	require.NoError(t, err)
	assert.Contains(t, output, "No credentials configured")
}

func TestTokenRevoke_PreservesOtherEnv(t *testing.T) {
	store := setupTokenTest(t)
	store.envs["myvm"] = map[string]string{
		"GITHUB_TOKEN":   "github_pat_test",
		"SOME_OTHER_VAR": "preserved",
	}

	_, err := captureTokenStdout(t, []string{"token", "revoke", "myvm"})
	require.NoError(t, err)

	env := store.envs["myvm"]
	assert.Equal(t, "preserved", env["SOME_OTHER_VAR"])
	_, hasGithub := env["GITHUB_TOKEN"]
	assert.False(t, hasGithub)
}

func TestTokenRevoke_MissingVM(t *testing.T) {
	setupTokenTest(t)

	_, err := captureTokenStdout(t, []string{"token", "revoke"})
	assert.Error(t, err)
}

func TestTokenRevoke_EmptyVM(t *testing.T) {
	setupTokenTest(t)

	_, err := captureTokenStdout(t, []string{"token", "revoke", ""})
	assert.Error(t, err)
}

func TestTokenRevoke_ReadError(t *testing.T) {
	store := setupTokenTest(t)
	store.readErr = fmt.Errorf("disk error")

	_, err := captureTokenStdout(t, []string{"token", "revoke", "myvm"})
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// List Tests
// ---------------------------------------------------------------------------

func TestTokenList_HumanOutput(t *testing.T) {
	store := setupTokenTest(t)
	store.envs["myvm"] = map[string]string{
		"GITHUB_TOKEN": "github_pat_test",
	}

	output, err := captureTokenStdout(t, []string{"token", "list", "myvm"})
	require.NoError(t, err)

	assert.Contains(t, output, "GitHub Token")
	assert.Contains(t, output, "GITHUB_TOKEN")
	assert.Contains(t, output, "configured")
	assert.Contains(t, output, "Anthropic API Key")
	assert.Contains(t, output, "ANTHROPIC_API_KEY")
	assert.Contains(t, output, "not configured")
}

func TestTokenList_JSONOutput(t *testing.T) {
	store := setupTokenTest(t)
	store.envs["myvm"] = map[string]string{
		"GITHUB_TOKEN": "github_pat_test",
	}

	output, err := captureTokenStdout(t, []string{"--json", "token", "list", "myvm"})
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	assert.True(t, result["ok"].(bool))
	data := result["data"].([]interface{})
	assert.Len(t, data, 2)

	githubEntry := data[0].(map[string]interface{})
	assert.Equal(t, "GITHUB_TOKEN", githubEntry["name"])
	assert.Equal(t, "GitHub Token", githubEntry["label"])
	assert.True(t, githubEntry["configured"].(bool))

	anthropicEntry := data[1].(map[string]interface{})
	assert.Equal(t, "ANTHROPIC_API_KEY", anthropicEntry["name"])
	assert.False(t, anthropicEntry["configured"].(bool))
}

func TestTokenList_NoneConfigured(t *testing.T) {
	setupTokenTest(t)

	output, err := captureTokenStdout(t, []string{"token", "list", "myvm"})
	require.NoError(t, err)
	assert.Contains(t, output, "not configured")
}

func TestTokenList_AllConfigured(t *testing.T) {
	store := setupTokenTest(t)
	store.envs["myvm"] = map[string]string{
		"GITHUB_TOKEN":    "github_pat_test",
		"ANTHROPIC_API_KEY": "sk-ant-test",
	}

	output, err := captureTokenStdout(t, []string{"token", "list", "myvm"})
	require.NoError(t, err)

	assert.Equal(t, 2, bytes.Count([]byte(output), []byte("configured")))
	assert.NotContains(t, output, "not configured")
}

func TestTokenList_MissingVM(t *testing.T) {
	setupTokenTest(t)

	_, err := captureTokenStdout(t, []string{"token", "list"})
	assert.Error(t, err)
}

func TestTokenList_EmptyVM(t *testing.T) {
	setupTokenTest(t)

	_, err := captureTokenStdout(t, []string{"token", "list", ""})
	assert.Error(t, err)
}

func TestTokenList_ReadError(t *testing.T) {
	store := setupTokenTest(t)
	store.readErr = fmt.Errorf("disk error")

	_, err := captureTokenStdout(t, []string{"token", "list", "myvm"})
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Unit Tests: format helpers
// ---------------------------------------------------------------------------

func TestFormatCredentialList(t *testing.T) {
	entries := []credentialEntry{
		{Name: "GITHUB_TOKEN", Label: "GitHub Token", Configured: true},
		{Name: "ANTHROPIC_API_KEY", Label: "Anthropic API Key", Configured: false},
	}
	output := formatCredentialList(entries)
	assert.Contains(t, output, "GitHub Token")
	assert.Contains(t, output, "GITHUB_TOKEN")
	assert.Contains(t, output, "configured")
	assert.Contains(t, output, "Anthropic API Key")
	assert.Contains(t, output, "ANTHROPIC_API_KEY")
	assert.Contains(t, output, "not configured")
}

func TestFormatCredentialList_Empty(t *testing.T) {
	output := formatCredentialList([]credentialEntry{})
	assert.Empty(t, output)
}

func TestFormatCredentialList_AllConfigured(t *testing.T) {
	entries := []credentialEntry{
		{Name: "GITHUB_TOKEN", Label: "GitHub Token", Configured: true},
		{Name: "ANTHROPIC_API_KEY", Label: "Anthropic API Key", Configured: true},
	}
	output := formatCredentialList(entries)
	assert.Equal(t, 2, bytes.Count([]byte(output), []byte("configured")))
	assert.NotContains(t, output, "not configured")
}

func TestFormatGithubSetupGuidance(t *testing.T) {
	scopes := []struct{ Permission, Access string }{
		{"contents", "write"},
		{"pull_requests", "write"},
	}
	notes := []string{
		"Use fine-grained tokens only.",
		"Enable branch protection.",
	}
	output := formatGithubSetupGuidance("fine-grained",
		"https://github.com/settings/personal-access-tokens/new",
		scopes, notes)

	assert.Contains(t, output, "GitHub Fine-Grained")
	assert.Contains(t, output, "github.com/settings")
	assert.Contains(t, output, "contents")
	assert.Contains(t, output, "pull_requests")
	assert.Contains(t, output, "fine-grained")
	assert.Contains(t, output, "branch protection")
}

// ---------------------------------------------------------------------------
// Rotate-Revoke-List Lifecycle Test
// ---------------------------------------------------------------------------

func TestToken_RotateRevokeListLifecycle(t *testing.T) {
	store := setupTokenTest(t)

	// Step 1: List (nothing configured)
	output, err := captureTokenStdout(t, []string{"token", "list", "myvm"})
	require.NoError(t, err)
	assert.Contains(t, output, "not configured")

	// Step 2: Rotate (add credentials)
	t.Setenv("GITHUB_TOKEN", "github_pat_lifecycle123")
	output, err = captureTokenStdout(t, []string{"token", "rotate", "myvm"})
	require.NoError(t, err)
	assert.Contains(t, output, "Rotated credentials")

	// Step 3: List (GitHub configured)
	output, err = captureTokenStdout(t, []string{"token", "list", "myvm"})
	require.NoError(t, err)
	assert.Contains(t, output, "GITHUB_TOKEN")
	assert.Contains(t, output, "configured")

	// Step 4: Revoke
	output, err = captureTokenStdout(t, []string{"token", "revoke", "myvm"})
	require.NoError(t, err)
	assert.Contains(t, output, "Revoked")

	// Step 5: List (nothing configured again)
	output, err = captureTokenStdout(t, []string{"token", "list", "myvm"})
	require.NoError(t, err)
	assert.Contains(t, output, "not configured")

	// Verify store state
	env := store.envs["myvm"]
	_, hasGithub := env["GITHUB_TOKEN"]
	assert.False(t, hasGithub)
}

// ---------------------------------------------------------------------------
// Property-Based Tests
// ---------------------------------------------------------------------------

func TestToken_RotateJSONAlwaysValid(t *testing.T) {
	cases := []struct {
		name string
		vm   string
		env  map[string]string
	}{
		{"github_only", "vm1", map[string]string{"GITHUB_TOKEN": "github_pat_test1"}},
		{"anthropic_only", "vm2", map[string]string{"ANTHROPIC_API_KEY": "sk-ant-test1"}},
		{"both", "vm3", map[string]string{"GITHUB_TOKEN": "github_pat_test2", "ANTHROPIC_API_KEY": "sk-ant-test2"}},
		{"finegrained", "vm4", map[string]string{"GITHUB_TOKEN": "github_pat_ABCDEFGHIJKLMNOP123456"}},
		{"sk_prefix", "vm5", map[string]string{"ANTHROPIC_API_KEY": "sk-ant-api03-longkeyvalue"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_ = setupTokenTest(t)
			os.Unsetenv("GITHUB_TOKEN")
			os.Unsetenv("ANTHROPIC_API_KEY")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			output, err := captureTokenStdout(t, []string{"--json", "token", "rotate", tc.vm})
			require.NoError(t, err)

			var result map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(output), &result), "output should be valid JSON: %s", output)
			assert.True(t, result["ok"].(bool), "ok should be true")
			data := result["data"].(map[string]interface{})
			assert.Equal(t, tc.vm, data["name"])
			assert.NotEmpty(t, data["credentials_rotated"])
		})
	}
}

func TestToken_ErrorCodesSnakeCase(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
	}{
		{"rotate_no_creds", []string{"token", "rotate", "myvm"}, nil},
		{"rotate_empty_vm", []string{"token", "rotate", ""}, nil},
		{"revoke_empty_vm", []string{"token", "revoke", ""}, nil},
		{"list_empty_vm", []string{"token", "list", ""}, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_ = setupTokenTest(t)
			os.Unsetenv("GITHUB_TOKEN")
			os.Unsetenv("ANTHROPIC_API_KEY")
			if tc.env != nil {
				for k, v := range tc.env {
					if v != "" {
						t.Setenv(k, v)
					}
				}
			}

			_, err := captureTokenStdout(t, tc.args)
			if err != nil {
				cliErr, ok := err.(ui.CLIError)
				if ok {
					assert.Regexp(t, `^[a-z][a-z0-9_]*$`, cliErr.Code,
						"error code should be snake_case: %s", cliErr.Code)
				}
			}
		})
	}
}

func TestToken_ListJSONRequiredFields(t *testing.T) {
	store := setupTokenTest(t)
	store.envs["myvm"] = map[string]string{
		"GITHUB_TOKEN": "github_pat_test",
	}

	output, err := captureTokenStdout(t, []string{"--json", "token", "list", "myvm"})
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.Contains(t, result, "ok")
	assert.Contains(t, result, "data")

	data := result["data"].([]interface{})
	for _, entry := range data {
		e := entry.(map[string]interface{})
		assert.Contains(t, e, "name")
		assert.Contains(t, e, "label")
		assert.Contains(t, e, "configured")
	}
}

func TestToken_GithubSetupJSONRequiredFields(t *testing.T) {
	setupTokenTest(t)

	output, err := captureTokenStdout(t, []string{"--json", "token", "github", "setup"})
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.Contains(t, result, "ok")
	assert.Contains(t, result, "data")

	data := result["data"].(map[string]interface{})
	assert.Contains(t, data, "pat_type")
	assert.Contains(t, data, "setup_url")
	assert.Contains(t, data, "recommended_scopes")
	assert.Contains(t, data, "notes")
}

func TestToken_RevokeAfterRotate_NeverShowsValues(t *testing.T) {
	store := setupTokenTest(t)
	store.envs["myvm"] = map[string]string{
		"GITHUB_TOKEN": "github_pat_SECRETVALUE123",
	}

	// Human list should not reveal values
	output, err := captureTokenStdout(t, []string{"token", "list", "myvm"})
	require.NoError(t, err)
	assert.NotContains(t, output, "SECRETVALUE123")

	// JSON list should not reveal values
	output, err = captureTokenStdout(t, []string{"--json", "token", "list", "myvm"})
	require.NoError(t, err)
	assert.NotContains(t, output, "SECRETVALUE123")
}

func TestToken_RotateStoresInVMConfig(t *testing.T) {
	store := setupTokenTest(t)

	t.Setenv("GITHUB_TOKEN", "github_pat_stored")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-stored")

	_, err := captureTokenStdout(t, []string{"token", "rotate", "testvm"})
	require.NoError(t, err)

	env := store.envs["testvm"]
	assert.Equal(t, "github_pat_stored", env["GITHUB_TOKEN"])
	assert.Equal(t, "sk-ant-stored", env["ANTHROPIC_API_KEY"])
}

// ---------------------------------------------------------------------------
// Validate Token Integration
// ---------------------------------------------------------------------------

func TestTokenRotate_ValidatesKeyFormat(t *testing.T) {
	warnings := security.ValidateToken("sk-ant-test", security.TokenAnthropicAPI)
	assert.Empty(t, warnings)

	warnings = security.ValidateToken("unusual-format", security.TokenAnthropicAPI)
	assert.NotEmpty(t, warnings)
	found := false
	for _, w := range warnings {
		if w.Code == "UNUSUAL_KEY_FORMAT" {
			found = true
		}
	}
	assert.True(t, found)
}
