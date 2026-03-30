// Package cmd provides tests for the config egress subcommands.
// REQ-002-005: Configuration Commands -- Egress Management
// REQ-004-008: User-defined Egress Allowlist
package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/security"
	"sd/internal/ui"
)

// egressTestState is a digital twin for tracking egress reads/writes.
type egressTestState struct {
	mu      sync.Mutex
	domains []string
	readErr error
	writeErr error
}

func (s *egressTestState) read(sdHome, vmName string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readErr != nil {
		return nil, s.readErr
	}
	result := make([]string, len(s.domains))
	copy(result, s.domains)
	return result, nil
}

func (s *egressTestState) write(sdHome, vmName string, domains []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writeErr != nil {
		return s.writeErr
	}
	if domains == nil {
		s.domains = nil
	} else {
		s.domains = make([]string, len(domains))
		copy(s.domains, domains)
	}
	return nil
}

// setupEgressTest creates an isolated test environment for egress commands.
func setupEgressTest(t *testing.T) *egressTestState {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("SD_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)
	resetRootFlags(t)

	// Create minimal config file so loader succeeds
	os.MkdirAll(tmpDir, 0700)
	os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte("defaults:\n  backend: lima\n"), 0600)

	state := &egressTestState{}
	t.Cleanup(func() {
		readVMEgressFunc = defaultReadVMEgress
		writeVMEgressFunc = defaultWriteVMEgress
	})
	readVMEgressFunc = state.read
	writeVMEgressFunc = state.write
	return state
}

// captureEgressOutput runs a command and captures stdout.
func captureEgressOutput(t *testing.T, args []string) (string, error) {
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

func TestConfigEgress_Registered(t *testing.T) {
	newRootTestEnv(t)

	root := RootCmd()
	var configCmd *cobra.Command
	for _, cmd := range root.Commands() {
		if cmd.Name() == "config" {
			configCmd = cmd
			break
		}
	}
	require.NotNil(t, configCmd, "config command must exist")

	var egressCmd *cobra.Command
	for _, cmd := range configCmd.Commands() {
		if cmd.Name() == "egress" {
			egressCmd = cmd
			break
		}
	}
	require.NotNil(t, egressCmd, "config egress command must exist")

	subNames := make(map[string]bool)
	for _, sub := range egressCmd.Commands() {
		subNames[sub.Name()] = true
	}
	assert.True(t, subNames["add"], "egress must have 'add' subcommand")
	assert.True(t, subNames["remove"], "egress must have 'remove' subcommand")
	assert.True(t, subNames["list"], "egress must have 'list' subcommand")
}

func TestConfigEgress_NoGroupID(t *testing.T) {
	// egress is a subcommand of config, not a root-level command, so no GroupID
	newRootTestEnv(t)
	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "config" {
			for _, sub := range cmd.Commands() {
				if sub.Name() == "egress" {
					assert.Empty(t, sub.GroupID, "egress subcommand should not have GroupID")
				}
			}
		}
	}
}

// ---
// config egress add (REQ-004-008)
// ---

func TestConfigEgressAdd_HumanOutput(t *testing.T) {
	state := setupEgressTest(t)

	output, err := captureEgressOutput(t, []string{"config", "egress", "add", "myvm", "custom.example.com"})
	require.NoError(t, err)
	assert.Contains(t, output, "custom.example.com")
	assert.Contains(t, output, "myvm")
	assert.Contains(t, output, "Added")

	state.mu.Lock()
	assert.Equal(t, []string{"custom.example.com"}, state.domains)
	state.mu.Unlock()
}

func TestConfigEgressAdd_JSONOutput(t *testing.T) {
	state := setupEgressTest(t)

	output, err := captureEgressOutput(t, []string{"--json", "config", "egress", "add", "myvm", "custom.example.com"})
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err, "output must be valid JSON: %s", output)

	assert.True(t, result["ok"].(bool))
	data := result["data"].(map[string]any)
	assert.Equal(t, "myvm", data["vm"])
	assert.Equal(t, "custom.example.com", data["domain"])
	assert.Equal(t, "added", data["action"])

	state.mu.Lock()
	assert.Equal(t, []string{"custom.example.com"}, state.domains)
	state.mu.Unlock()
}

func TestConfigEgressAdd_AlreadyUserAdded(t *testing.T) {
	state := setupEgressTest(t)
	state.domains = []string{"custom.example.com"}

	_, err := captureEgressOutput(t, []string{"config", "egress", "add", "myvm", "custom.example.com"})
	assert.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
	assert.Contains(t, cliErr.Message, "already")
}

func TestConfigEgressAdd_AlreadyDefault(t *testing.T) {
	setupEgressTest(t)

	_, err := captureEgressOutput(t, []string{"config", "egress", "add", "myvm", "github.com"})
	assert.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
	assert.Contains(t, cliErr.Message, "default")
}

func TestConfigEgressAdd_InvalidDomain(t *testing.T) {
	setupEgressTest(t)

	_, err := captureEgressOutput(t, []string{"config", "egress", "add", "myvm", ""})
	assert.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
}

func TestConfigEgressAdd_InvalidDomainDotPrefix(t *testing.T) {
	setupEgressTest(t)

	_, err := captureEgressOutput(t, []string{"config", "egress", "add", "myvm", ".example.com"})
	assert.Error(t, err)
}

func TestConfigEgressAdd_EmptyVM(t *testing.T) {
	setupEgressTest(t)

	_, err := captureEgressOutput(t, []string{"config", "egress", "add", "", "custom.example.com"})
	assert.Error(t, err)
}

func TestConfigEgressAdd_MissingArgs(t *testing.T) {
	setupEgressTest(t)

	_, err := captureEgressOutput(t, []string{"config", "egress", "add", "myvm"})
	assert.Error(t, err)
}

func TestConfigEgressAdd_WriteError(t *testing.T) {
	state := setupEgressTest(t)
	state.writeErr = fmt.Errorf("disk full")

	_, err := captureEgressOutput(t, []string{"config", "egress", "add", "myvm", "custom.example.com"})
	assert.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "egress_update_failed", cliErr.Code)
}

func TestConfigEgressAdd_MultipleDomains(t *testing.T) {
	state := setupEgressTest(t)

	_, err := captureEgressOutput(t, []string{"config", "egress", "add", "myvm", "first.example.com"})
	require.NoError(t, err)

	_, err = captureEgressOutput(t, []string{"config", "egress", "add", "myvm", "second.example.com"})
	require.NoError(t, err)

	state.mu.Lock()
	assert.Equal(t, []string{"first.example.com", "second.example.com"}, state.domains)
	state.mu.Unlock()
}

func TestConfigEgressAdd_CaseInsensitive(t *testing.T) {
	state := setupEgressTest(t)
	state.domains = []string{"Custom.Example.COM"}

	_, err := captureEgressOutput(t, []string{"config", "egress", "add", "myvm", "custom.example.com"})
	assert.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Contains(t, cliErr.Message, "already")
}

// ---
// config egress remove (REQ-004-008)
// ---

func TestConfigEgressRemove_HumanOutput(t *testing.T) {
	state := setupEgressTest(t)
	state.domains = []string{"custom.example.com", "other.example.com"}

	output, err := captureEgressOutput(t, []string{"config", "egress", "remove", "myvm", "custom.example.com"})
	require.NoError(t, err)
	assert.Contains(t, output, "custom.example.com")
	assert.Contains(t, output, "Removed")

	state.mu.Lock()
	assert.Equal(t, []string{"other.example.com"}, state.domains)
	state.mu.Unlock()
}

func TestConfigEgressRemove_JSONOutput(t *testing.T) {
	state := setupEgressTest(t)
	state.domains = []string{"custom.example.com"}

	output, err := captureEgressOutput(t, []string{"--json", "config", "egress", "remove", "myvm", "custom.example.com"})
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err, "output must be valid JSON: %s", output)

	assert.True(t, result["ok"].(bool))
	data := result["data"].(map[string]any)
	assert.Equal(t, "myvm", data["vm"])
	assert.Equal(t, "custom.example.com", data["domain"])
	assert.Equal(t, "removed", data["action"])
}

func TestConfigEgressRemove_DefaultDomain(t *testing.T) {
	setupEgressTest(t)

	_, err := captureEgressOutput(t, []string{"config", "egress", "remove", "myvm", "github.com"})
	assert.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
	assert.Contains(t, cliErr.Message, "default")
}

func TestConfigEgressRemove_NotFound(t *testing.T) {
	setupEgressTest(t)

	_, err := captureEgressOutput(t, []string{"config", "egress", "remove", "myvm", "not.present.com"})
	assert.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
	assert.Contains(t, cliErr.Message, "not in the user-added")
}

func TestConfigEgressRemove_EmptyVM(t *testing.T) {
	setupEgressTest(t)

	_, err := captureEgressOutput(t, []string{"config", "egress", "remove", "", "custom.example.com"})
	assert.Error(t, err)
}

func TestConfigEgressRemove_MissingArgs(t *testing.T) {
	setupEgressTest(t)

	_, err := captureEgressOutput(t, []string{"config", "egress", "remove", "myvm"})
	assert.Error(t, err)
}

func TestConfigEgressRemove_WriteError(t *testing.T) {
	state := setupEgressTest(t)
	state.domains = []string{"custom.example.com"}
	state.writeErr = fmt.Errorf("disk full")

	_, err := captureEgressOutput(t, []string{"config", "egress", "remove", "myvm", "custom.example.com"})
	assert.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "egress_update_failed", cliErr.Code)
}

func TestConfigEgressRemove_RemovesAllClearsList(t *testing.T) {
	state := setupEgressTest(t)
	state.domains = []string{"custom.example.com"}

	_, err := captureEgressOutput(t, []string{"config", "egress", "remove", "myvm", "custom.example.com"})
	require.NoError(t, err)

	state.mu.Lock()
	assert.Nil(t, state.domains)
	state.mu.Unlock()
}

func TestConfigEgressRemove_CaseInsensitive(t *testing.T) {
	state := setupEgressTest(t)
	state.domains = []string{"Custom.Example.COM"}

	_, err := captureEgressOutput(t, []string{"config", "egress", "remove", "myvm", "custom.example.com"})
	require.NoError(t, err)

	state.mu.Lock()
	assert.Nil(t, state.domains)
	state.mu.Unlock()
}

// ---
// config egress list (REQ-002-005)
// ---

func TestConfigEgressList_HumanOutput(t *testing.T) {
	state := setupEgressTest(t)
	state.domains = []string{"custom.example.com"}

	output, err := captureEgressOutput(t, []string{"config", "egress", "list", "myvm"})
	require.NoError(t, err)

	// Should contain default domains
	assert.Contains(t, output, "api.anthropic.com")
	assert.Contains(t, output, "default")
	// Should contain user-added domain
	assert.Contains(t, output, "custom.example.com")
	assert.Contains(t, output, "user")
	assert.Contains(t, output, "DOMAIN")
	assert.Contains(t, output, "SOURCE")
}

func TestConfigEgressList_JSONOutput(t *testing.T) {
	state := setupEgressTest(t)
	state.domains = []string{"custom.example.com"}

	output, err := captureEgressOutput(t, []string{"--json", "config", "egress", "list", "myvm"})
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err, "output must be valid JSON: %s", output)

	assert.True(t, result["ok"].(bool))
	data := result["data"].([]any)

	// Check that default domains are present
	foundDefault := false
	foundUser := false
	for _, entry := range data {
		e := entry.(map[string]any)
		domain := e["domain"].(string)
		source := e["source"].(string)
		if domain == "api.anthropic.com" && source == "default" {
			foundDefault = true
		}
		if domain == "custom.example.com" && source == "user" {
			foundUser = true
		}
	}
	assert.True(t, foundDefault, "should contain default domain")
	assert.True(t, foundUser, "should contain user-added domain")
}

func TestConfigEgressList_NoUserDomains(t *testing.T) {
	setupEgressTest(t)

	output, err := captureEgressOutput(t, []string{"--json", "config", "egress", "list", "myvm"})
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err)

	data := result["data"].([]any)
	// Should only contain default domains
	for _, entry := range data {
		e := entry.(map[string]any)
		assert.Equal(t, "default", e["source"])
	}
}

func TestConfigEgressList_EmptyVM(t *testing.T) {
	setupEgressTest(t)

	_, err := captureEgressOutput(t, []string{"config", "egress", "list", ""})
	assert.Error(t, err)
}

func TestConfigEgressList_MissingArgs(t *testing.T) {
	setupEgressTest(t)

	_, err := captureEgressOutput(t, []string{"config", "egress", "list"})
	assert.Error(t, err)
}

func TestConfigEgressList_ReadError(t *testing.T) {
	state := setupEgressTest(t)
	state.readErr = fmt.Errorf("read failure")

	_, err := captureEgressOutput(t, []string{"config", "egress", "list", "myvm"})
	assert.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "egress_query_failed", cliErr.Code)
}

// ---
// Unit tests for helpers
// ---

func TestFormatEgressList(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		out := formatEgressList(nil)
		assert.Contains(t, out, "No egress rules")
	})

	t.Run("with domains", func(t *testing.T) {
		domains := []security.EgressDomain{
			{Domain: "api.anthropic.com", Source: security.DomainSourceDefault},
			{Domain: "custom.example.com", Source: security.DomainSourceUser},
		}
		out := formatEgressList(domains)
		assert.Contains(t, out, "DOMAIN")
		assert.Contains(t, out, "api.anthropic.com")
		assert.Contains(t, out, "default")
		assert.Contains(t, out, "custom.example.com")
		assert.Contains(t, out, "user")
	})
}

// ---
// Round-trip / lifecycle tests
// ---

func TestConfigEgress_AddRemoveList(t *testing.T) {
	state := setupEgressTest(t)

	// Add domain
	_, err := captureEgressOutput(t, []string{"--json", "config", "egress", "add", "myvm", "custom.example.com"})
	require.NoError(t, err)

	// List should show it
	output, err := captureEgressOutput(t, []string{"--json", "config", "egress", "list", "myvm"})
	require.NoError(t, err)
	var listResult map[string]any
	json.Unmarshal([]byte(output), &listResult)
	data := listResult["data"].([]any)
	found := false
	for _, entry := range data {
		e := entry.(map[string]any)
		if e["domain"] == "custom.example.com" && e["source"] == "user" {
			found = true
		}
	}
	assert.True(t, found, "added domain should appear in list")

	// Remove domain
	_, err = captureEgressOutput(t, []string{"--json", "config", "egress", "remove", "myvm", "custom.example.com"})
	require.NoError(t, err)

	state.mu.Lock()
	assert.Nil(t, state.domains)
	state.mu.Unlock()
}

// ---
// Property-based tests
// ---

func TestProperty_ConfigEgressAdd_JSONAlwaysValid(t *testing.T) {
	cases := []struct {
		vm     string
		domain string
	}{
		{"vm1", "a.example.com"},
		{"vm2", "b.test.org"},
		{"my-prod-vm", "api.mycorp.io"},
		{"test123", "cdn.example.net"},
		{"dev", "uploads.example.com"},
	}

	for _, tc := range cases {
		t.Run(tc.vm+"_"+tc.domain, func(t *testing.T) {
			setupEgressTest(t)

			output, err := captureEgressOutput(t, []string{"--json", "config", "egress", "add", tc.vm, tc.domain})
			require.NoError(t, err)

			var result map[string]any
			err = json.Unmarshal([]byte(output), &result)
			require.NoError(t, err, "JSON must be valid: %s", output)
			assert.True(t, result["ok"].(bool))

			data := result["data"].(map[string]any)
			assert.Equal(t, tc.vm, data["vm"])
			assert.Equal(t, tc.domain, data["domain"])
			assert.Equal(t, "added", data["action"])
		})
	}
}

func TestProperty_ConfigEgressRemove_JSONAlwaysValid(t *testing.T) {
	cases := []struct {
		vm     string
		domain string
	}{
		{"vm1", "a.example.com"},
		{"vm2", "b.test.org"},
		{"my-vm", "api.mycorp.io"},
	}

	for _, tc := range cases {
		t.Run(tc.vm+"_"+tc.domain, func(t *testing.T) {
			state := setupEgressTest(t)
			state.domains = []string{tc.domain}

			output, err := captureEgressOutput(t, []string{"--json", "config", "egress", "remove", tc.vm, tc.domain})
			require.NoError(t, err)

			var result map[string]any
			err = json.Unmarshal([]byte(output), &result)
			require.NoError(t, err, "JSON must be valid: %s", output)
			assert.True(t, result["ok"].(bool))
		})
	}
}

func TestProperty_ConfigEgressList_JSONAlwaysValid(t *testing.T) {
	cases := []struct {
		name    string
		domains []string
	}{
		{"no_user", nil},
		{"one_domain", []string{"custom.example.com"}},
		{"multiple_domains", []string{"a.example.com", "b.test.org", "c.mycorp.io"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := setupEgressTest(t)
			state.domains = tc.domains

			output, err := captureEgressOutput(t, []string{"--json", "config", "egress", "list", "myvm"})
			require.NoError(t, err)

			var result map[string]any
			err = json.Unmarshal([]byte(output), &result)
			require.NoError(t, err, "JSON must be valid: %s", output)
			assert.True(t, result["ok"].(bool))

			data := result["data"].([]any)
			// Must always contain all default domains
			assert.GreaterOrEqual(t, len(data), len(security.DefaultEgressAllowlist))
		})
	}
}

func TestProperty_ConfigEgress_ErrorCodesSnakeCase(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code string
	}{
		{"add_empty_vm", []string{"config", "egress", "add", "", "x.com"}, "invalid_argument"},
		{"add_invalid_domain", []string{"config", "egress", "add", "vm", ".bad"}, "invalid_argument"},
		{"add_default_domain", []string{"config", "egress", "add", "vm", "github.com"}, "invalid_argument"},
		{"remove_default_domain", []string{"config", "egress", "remove", "vm", "github.com"}, "invalid_argument"},
		{"remove_not_found", []string{"config", "egress", "remove", "vm", "not.found.com"}, "invalid_argument"},
		{"remove_empty_vm", []string{"config", "egress", "remove", "", "x.com"}, "invalid_argument"},
		{"list_empty_vm", []string{"config", "egress", "list", ""}, "invalid_argument"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupEgressTest(t)

			_, err := captureEgressOutput(t, tc.args)
			require.Error(t, err)
			cliErr, ok := err.(ui.CLIError)
			require.True(t, ok, "error must be CLIError: %v", err)
			assert.Equal(t, tc.code, cliErr.Code)
			assert.False(t, strings.Contains(cliErr.Code, " "), "code must be snake_case")
			assert.False(t, strings.Contains(cliErr.Code, "-"), "code must use underscores not hyphens")
		})
	}
}

func TestProperty_ConfigEgressList_ContainsAllDefaults(t *testing.T) {
	for i := 0; i < 3; i++ {
		t.Run(fmt.Sprintf("run_%d", i), func(t *testing.T) {
			setupEgressTest(t)

			output, err := captureEgressOutput(t, []string{"--json", "config", "egress", "list", "myvm"})
			require.NoError(t, err)

			var result map[string]any
			json.Unmarshal([]byte(output), &result)
			data := result["data"].([]any)

			dataDomains := make(map[string]bool)
			for _, entry := range data {
				e := entry.(map[string]any)
				dataDomains[e["domain"].(string)] = true
			}

			for _, d := range security.DefaultEgressAllowlist {
				assert.True(t, dataDomains[d], "default domain %q must be in list", d)
			}
		})
	}
}

func TestProperty_ConfigEgressAdd_CallsWriteOnce(t *testing.T) {
	vms := []string{"vm1", "vm2", "vm3"}
	for _, vm := range vms {
		t.Run(vm, func(t *testing.T) {
			state := setupEgressTest(t)

			_, err := captureEgressOutput(t, []string{"--json", "config", "egress", "add", vm, "custom.example.com"})
			require.NoError(t, err)

			state.mu.Lock()
			assert.Equal(t, []string{"custom.example.com"}, state.domains)
			state.mu.Unlock()
		})
	}
}

func TestProperty_ConfigEgressList_JSONRequiredFields(t *testing.T) {
	state := setupEgressTest(t)
	state.domains = []string{"custom.example.com"}

	output, err := captureEgressOutput(t, []string{"--json", "config", "egress", "list", "myvm"})
	require.NoError(t, err)

	var result map[string]any
	json.Unmarshal([]byte(output), &result)
	data := result["data"].([]any)

	for _, entry := range data {
		e := entry.(map[string]any)
		_, hasDomain := e["domain"]
		_, hasSource := e["source"]
		assert.True(t, hasDomain, "each entry must have 'domain' field")
		assert.True(t, hasSource, "each entry must have 'source' field")
		assert.Contains(t, []string{"default", "user"}, e["source"], "source must be 'default' or 'user'")
	}
}
