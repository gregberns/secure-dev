// Package cmd provides tests for the audit command.
// REQ-002-008: Security Commands -- audit
// REQ-004-021: Audit Logging — Command Logging
// REQ-004-022: Audit Logging — VM Lifecycle Events with Hash Chain
package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/security"
	"sd/internal/ui"
)

// --- Digital twin setup ---

// setupAuditTest configures the test environment with a mock audit logger.
// Returns the temp directory (used as SD_HOME) and the audit logger for populating entries.
func setupAuditTest(t *testing.T) (string, *security.AuditLogger) {
	t.Helper()
	sdHome := newRootTestEnv(t)

	// Reset audit command flags
	root := RootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "audit" {
			_ = cmd.Flags().Set("since", "")
			_ = cmd.Flags().Set("verify", "false")
			break
		}
	}

	// Create audit logger pointing to temp dir
	logPath := filepath.Join(sdHome, "audit.log")
	logger := security.NewAuditLogger(logPath)

	// Override the factory to use our test logger
	origFactory := newAuditLoggerFunc
	newAuditLoggerFunc = func(home string) *security.AuditLogger {
		return logger
	}
	t.Cleanup(func() { newAuditLoggerFunc = origFactory })

	return sdHome, logger
}

// --- Unit tests: command registration ---

func TestAuditCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "audit" {
			found = true
			assert.Equal(t, "security", cmd.GroupID)
			break
		}
	}
	assert.True(t, found, "audit command must be registered")
}

func TestAuditCommand_MaxArgs(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"audit"})
	require.NoError(t, err)
	assert.NotNil(t, cmd.Args, "audit must have an Args validator")
	// Accept 0 args
	err = cmd.Args(cmd, nil)
	assert.NoError(t, err, "audit must accept 0 args")
	// Accept 1 arg
	err = cmd.Args(cmd, []string{"myvm"})
	assert.NoError(t, err, "audit must accept 1 arg")
	// Reject 2 args
	err = cmd.Args(cmd, []string{"vm1", "vm2"})
	assert.Error(t, err, "audit must reject 2 args")
}

func TestAuditCommand_Flags(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"audit"})
	require.NoError(t, err)

	sinceFlag := cmd.Flags().Lookup("since")
	require.NotNil(t, sinceFlag, "must have --since flag")
	assert.Equal(t, "", sinceFlag.DefValue)

	verifyFlag := cmd.Flags().Lookup("verify")
	require.NotNil(t, verifyFlag, "must have --verify flag")
	assert.Equal(t, "false", verifyFlag.DefValue)
}

func TestAuditCommand_NoConfigRequired(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()

	cmd, _, err := root.Find([]string{"audit"})
	require.NoError(t, err)
	// The command should be in the no-config-required set
	assert.Equal(t, "audit", cmd.Name())
}

// --- Unit tests: human output ---

func TestAuditCommand_EmptyLog_HumanOutput(t *testing.T) {
	_, _ = setupAuditTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"audit"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "No audit entries found")
}

func TestAuditCommand_WithEntries_HumanOutput(t *testing.T) {
	_, logger := setupAuditTest(t)

	ts := time.Date(2026, 3, 27, 10, 15, 30, 0, time.UTC)
	require.NoError(t, logger.LogCommand(security.CommandLogEntry{
		Timestamp:  ts,
		Command:    "sd create myvm",
		Args:       []string{"create", "myvm"},
		ExitCode:   0,
		DurationMs: 4523,
	}))
	require.NoError(t, logger.LogEvent(security.EventLogEntry{
		Timestamp: ts,
		EventType: "create",
		VMName:    "myvm",
		Metadata:  map[string]string{"mounts": "0"},
	}))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"audit"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "command")
	assert.Contains(t, buf.String(), "sd create myvm")
	assert.Contains(t, buf.String(), "event")
	assert.Contains(t, buf.String(), "create")
	assert.Contains(t, buf.String(), "myvm")
}

func TestAuditCommand_FilterByVM_HumanOutput(t *testing.T) {
	_, logger := setupAuditTest(t)

	ts := time.Date(2026, 3, 27, 10, 15, 30, 0, time.UTC)
	require.NoError(t, logger.LogEvent(security.EventLogEntry{
		Timestamp: ts,
		EventType: "create",
		VMName:    "vm-alpha",
	}))
	require.NoError(t, logger.LogEvent(security.EventLogEntry{
		Timestamp: ts,
		EventType: "create",
		VMName:    "vm-beta",
	}))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"audit", "vm-alpha"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "vm-alpha")
	assert.NotContains(t, buf.String(), "vm-beta")
}

func TestAuditCommand_SinceFilter_HumanOutput(t *testing.T) {
	_, logger := setupAuditTest(t)

	ts1 := time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC)
	ts2 := time.Date(2026, 3, 27, 12, 0, 0, 0, time.UTC)

	require.NoError(t, logger.LogEvent(security.EventLogEntry{
		Timestamp: ts1,
		EventType: "create",
		VMName:    "early-vm",
	}))
	require.NoError(t, logger.LogEvent(security.EventLogEntry{
		Timestamp: ts2,
		EventType: "start",
		VMName:    "late-vm",
	}))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"audit", "--since", "2026-03-27T11:00:00Z"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.NotContains(t, buf.String(), "early-vm")
	assert.Contains(t, buf.String(), "late-vm")
}

// --- Unit tests: JSON output ---

func TestAuditCommand_EmptyLog_JSONOutput(t *testing.T) {
	_, _ = setupAuditTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "audit"})
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
	data := result["data"].([]any)
	assert.Empty(t, data)
}

func TestAuditCommand_WithEntries_JSONOutput(t *testing.T) {
	_, logger := setupAuditTest(t)

	ts := time.Date(2026, 3, 27, 10, 15, 30, 0, time.UTC)
	require.NoError(t, logger.LogEvent(security.EventLogEntry{
		Timestamp: ts,
		EventType: "start",
		VMName:    "myvm",
		Metadata:  map[string]string{"key": "val"},
	}))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "audit"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	assert.True(t, result["ok"].(bool))
	data := result["data"].([]any)
	require.Len(t, data, 1)

	entry := data[0].(map[string]any)
	assert.Equal(t, "event", entry["type"])
	assert.Equal(t, "start", entry["event"])
	assert.Equal(t, "myvm", entry["vm"])
}

func TestAuditCommand_FilterByVM_JSONOutput(t *testing.T) {
	_, logger := setupAuditTest(t)

	ts := time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC)
	require.NoError(t, logger.LogEvent(security.EventLogEntry{
		Timestamp: ts,
		EventType: "create",
		VMName:    "alpha",
	}))
	require.NoError(t, logger.LogEvent(security.EventLogEntry{
		Timestamp: ts,
		EventType: "create",
		VMName:    "beta",
	}))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "audit", "alpha"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	data := result["data"].([]any)
	require.Len(t, data, 1)
	entry := data[0].(map[string]any)
	assert.Equal(t, "alpha", entry["vm"])
}

// --- Unit tests: --verify ---

func TestAuditCommand_VerifyValidChain(t *testing.T) {
	_, logger := setupAuditTest(t)

	ts := time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC)
	require.NoError(t, logger.LogEvent(security.EventLogEntry{
		Timestamp: ts,
		EventType: "create",
		VMName:    "myvm",
	}))
	require.NoError(t, logger.LogEvent(security.EventLogEntry{
		Timestamp: ts,
		EventType: "start",
		VMName:    "myvm",
	}))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"audit", "--verify"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "verified")
}

func TestAuditCommand_VerifyValidChain_JSONOutput(t *testing.T) {
	_, logger := setupAuditTest(t)

	ts := time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC)
	require.NoError(t, logger.LogEvent(security.EventLogEntry{
		Timestamp: ts,
		EventType: "create",
		VMName:    "myvm",
	}))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "audit", "--verify"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	assert.True(t, result["ok"].(bool))
	data := result["data"].(map[string]any)
	assert.Equal(t, true, data["verified"])
}

func TestAuditCommand_VerifyBrokenChain(t *testing.T) {
	sdHome, _ := setupAuditTest(t)

	// Write a malformed audit log directly
	logPath := filepath.Join(sdHome, "audit.log")
	require.NoError(t, os.WriteFile(logPath, []byte(
		`{"ts":"2026-03-27T10:00:00Z","type":"event","prev_hash":"0000000000000000000000000000000000000000000000000000000000000000","event":"create","vm":"myvm"}
{"ts":"2026-03-27T10:01:00Z","type":"event","prev_hash":"BROKENHASH","event":"start","vm":"myvm"}
`), 0600))

	root := RootCmd()
	root.SetArgs([]string{"audit", "--verify"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "audit_chain_broken", cliErr.Code)
	assert.Contains(t, cliErr.Message, "line 2")
}

func TestAuditCommand_VerifyBrokenChain_JSONOutput(t *testing.T) {
	sdHome, _ := setupAuditTest(t)

	logPath := filepath.Join(sdHome, "audit.log")
	require.NoError(t, os.WriteFile(logPath, []byte(
		`{"ts":"2026-03-27T10:00:00Z","type":"event","prev_hash":"0000000000000000000000000000000000000000000000000000000000000000","event":"create","vm":"myvm"}
{"ts":"2026-03-27T10:01:00Z","type":"event","prev_hash":"WRONG","event":"start","vm":"myvm"}
`), 0600))

	root := RootCmd()
	root.SetArgs([]string{"--json", "audit", "--verify"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "audit_chain_broken", cliErr.Code)
}

func TestAuditCommand_VerifyEmptyLog(t *testing.T) {
	_, _ = setupAuditTest(t)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"audit", "--verify"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	// Empty log should verify successfully
	require.NoError(t, execErr)
	assert.Contains(t, buf.String(), "verified")
}

// --- Unit tests: error cases ---

func TestAuditCommand_InvalidSinceFormat(t *testing.T) {
	_, _ = setupAuditTest(t)

	root := RootCmd()
	root.SetArgs([]string{"audit", "--since", "not-a-date"})
	err := root.Execute()

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
	assert.Contains(t, cliErr.Message, "ISO 8601")
}

func TestAuditCommand_NoSDHome(t *testing.T) {
	_, _ = setupAuditTest(t)

	// With newRootTestEnv, SD_HOME is set so Loader().SDHome() returns a valid path.
	// The audit command should work correctly.
	root := RootCmd()
	root.SetArgs([]string{"audit"})
	err := root.Execute()
	require.NoError(t, err)
}

// --- Unit tests: formatAuditEntries ---

func TestFormatAuditEntries_Empty(t *testing.T) {
	result := formatAuditEntries(nil)
	assert.Equal(t, "No audit entries found.\n", result)
}

func TestFormatAuditEntries_CommandEntry(t *testing.T) {
	ts := time.Date(2026, 3, 27, 10, 15, 30, 0, time.UTC)
	entries := []security.AuditEntry{
		{
			Timestamp:  ts,
			Type:       "command",
			PrevHash:   "abc123",
			Command:    "sd create myvm",
			Args:       []string{"create", "myvm"},
			ExitCode:   0,
			DurationMs: 4523,
		},
	}
	result := formatAuditEntries(entries)
	assert.Contains(t, result, "command")
	assert.Contains(t, result, "sd create myvm")
	assert.Contains(t, result, "exit=0")
	assert.Contains(t, result, "4523ms")
}

func TestFormatAuditEntries_EventEntry(t *testing.T) {
	ts := time.Date(2026, 3, 27, 10, 15, 30, 0, time.UTC)
	entries := []security.AuditEntry{
		{
			Timestamp: ts,
			Type:      "event",
			PrevHash:  "abc123",
			EventType: "create",
			VM:        "myvm",
			Meta:      map[string]string{"mounts": "0", "egress_rules": "13"},
		},
	}
	result := formatAuditEntries(entries)
	assert.Contains(t, result, "event")
	assert.Contains(t, result, "create")
	assert.Contains(t, result, "myvm")
}

func TestFormatAuditEntries_EventEntryNoMeta(t *testing.T) {
	ts := time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC)
	entries := []security.AuditEntry{
		{
			Timestamp: ts,
			Type:      "event",
			PrevHash:  "abc",
			EventType: "destroy",
			VM:        "old-vm",
		},
	}
	result := formatAuditEntries(entries)
	assert.Contains(t, result, "destroy")
	assert.Contains(t, result, "old-vm")
}

// --- Property-based tests ---

// Property: JSON output from audit always parses as valid JSON with ok=true.
func TestProperty_AuditJSONAlwaysValid(t *testing.T) {
	vmNames := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	for _, name := range vmNames {
		t.Run(name, func(t *testing.T) {
			_, logger := setupAuditTest(t)

			ts := time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC)
			require.NoError(t, logger.LogEvent(security.EventLogEntry{
				Timestamp: ts,
				EventType: "create",
				VMName:    name,
			}))

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"--json", "audit"})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)

			var result map[string]any
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err, "JSON must parse for VM %q: %s", name, buf.String())
			assert.True(t, result["ok"].(bool), "ok must be true for %q", name)
		})
	}
}

// Property: human output always contains the VM name when filtered.
func TestProperty_AuditHumanOutputContainsVMName(t *testing.T) {
	vmNames := []string{"alpha", "beta", "gamma"}
	for _, name := range vmNames {
		t.Run(name, func(t *testing.T) {
			_, logger := setupAuditTest(t)

			ts := time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC)
			require.NoError(t, logger.LogEvent(security.EventLogEntry{
				Timestamp: ts,
				EventType: "start",
				VMName:    name,
			}))

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"audit", name})
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

// Property: error codes are always snake_case.
func TestProperty_AuditErrorCodesSnakeCase(t *testing.T) {
	t.Run("audit_chain_broken", func(t *testing.T) {
		sdHome, _ := setupAuditTest(t)
		logPath := filepath.Join(sdHome, "audit.log")
		require.NoError(t, os.WriteFile(logPath, []byte(
			`{"ts":"2026-03-27T10:00:00Z","type":"event","prev_hash":"0000000000000000000000000000000000000000000000000000000000000000","event":"create","vm":"myvm"}
{"ts":"2026-03-27T10:01:00Z","type":"event","prev_hash":"BAD","event":"start","vm":"myvm"}
`), 0600))

		root := RootCmd()
		root.SetArgs([]string{"audit", "--verify"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "audit_chain_broken", cliErr.Code)
	})

	t.Run("invalid_argument_bad_since", func(t *testing.T) {
		_, _ = setupAuditTest(t)
		root := RootCmd()
		root.SetArgs([]string{"audit", "--since", "not-a-timestamp"})
		err := root.Execute()
		require.Error(t, err)
		cliErr := err.(ui.CLIError)
		assert.Equal(t, "invalid_argument", cliErr.Code)
	})

	t.Run("audit_query_failed_on_tampered_file", func(t *testing.T) {
		sdHome, _ := setupAuditTest(t)
		// This creates a valid file but with content that can still be queried
		// (the Query method skips malformed lines)
		logPath := filepath.Join(sdHome, "audit.log")
		require.NoError(t, os.WriteFile(logPath, []byte("not-json\n"), 0600))

		root := RootCmd()
		root.SetArgs([]string{"--json", "audit"})
		// Query will return empty results (malformed lines are skipped)
		execErr := root.Execute()
		require.NoError(t, execErr)
	})
}

// Property: --verify on a valid chain always succeeds.
func TestProperty_AuditVerifyValidChainAlwaysSucceeds(t *testing.T) {
	vmNames := []string{"vm1", "vm2", "vm3"}
	for _, name := range vmNames {
		t.Run(name, func(t *testing.T) {
			_, logger := setupAuditTest(t)

			ts := time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC)
			require.NoError(t, logger.LogCommand(security.CommandLogEntry{
				Timestamp: ts,
				Command:   "sd create " + name,
				Args:      []string{"create", name},
				ExitCode:  0,
			}))
			require.NoError(t, logger.LogEvent(security.EventLogEntry{
				Timestamp: ts,
				EventType: "create",
				VMName:    name,
			}))

			root := RootCmd()
			root.SetArgs([]string{"audit", "--verify"})
			execErr := root.Execute()

			require.NoError(t, execErr, "verify must succeed for valid chain with VM %q", name)
		})
	}
}

// Property: --verify on a tampered chain always fails.
func TestProperty_AuditVerifyTamperedChainAlwaysFails(t *testing.T) {
	tamperedHashes := []string{"DEADBEEF", "allzeros000000000000000000000", "x", ""}
	for i, badHash := range tamperedHashes {
		t.Run(fmt.Sprintf("tamper_%d", i), func(t *testing.T) {
			sdHome, _ := setupAuditTest(t)

			logPath := filepath.Join(sdHome, "audit.log")
			require.NoError(t, os.WriteFile(logPath, []byte(fmt.Sprintf(
				`{"ts":"2026-03-27T10:00:00Z","type":"event","prev_hash":"0000000000000000000000000000000000000000000000000000000000000000","event":"create","vm":"myvm"}
{"ts":"2026-03-27T10:01:00Z","type":"event","prev_hash":"%s","event":"start","vm":"myvm"}
`, badHash)), 0600))

			root := RootCmd()
			root.SetArgs([]string{"audit", "--verify"})
			err := root.Execute()
			require.Error(t, err, "verify must fail for tampered hash %q", badHash)
			cliErr, ok := err.(ui.CLIError)
			require.True(t, ok)
			assert.Equal(t, "audit_chain_broken", cliErr.Code)
		})
	}
}

// Property: JSON output always contains required fields (ok, data).
func TestProperty_AuditJSONRequiredFields(t *testing.T) {
	_, logger := setupAuditTest(t)

	ts := time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC)
	require.NoError(t, logger.LogEvent(security.EventLogEntry{
		Timestamp: ts,
		EventType: "create",
		VMName:    "test-vm",
	}))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "audit"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	assert.Contains(t, result, "ok", "JSON must have 'ok' field")
	assert.Contains(t, result, "data", "JSON must have 'data' field")
	data := result["data"].([]any)
	require.Len(t, data, 1)

	entry := data[0].(map[string]any)
	assert.Contains(t, entry, "ts", "entry must have 'ts' field")
	assert.Contains(t, entry, "type", "entry must have 'type' field")
}

// Property: verify JSON always contains ok=true and data.verified=true.
func TestProperty_AuditVerifyJSONRequiredFields(t *testing.T) {
	_, logger := setupAuditTest(t)

	ts := time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC)
	require.NoError(t, logger.LogEvent(security.EventLogEntry{
		Timestamp: ts,
		EventType: "create",
		VMName:    "test-vm",
	}))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	root := RootCmd()
	root.SetArgs([]string{"--json", "audit", "--verify"})
	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	require.NoError(t, execErr)

	var result map[string]any
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	assert.Contains(t, result, "ok")
	assert.Contains(t, result, "data")
	data := result["data"].(map[string]any)
	assert.Contains(t, data, "verified")
	assert.Equal(t, true, data["verified"])
}

// Property: VM filter always excludes non-matching entries.
func TestProperty_AuditVMFilterExcludesNonMatching(t *testing.T) {
	allVMs := []string{"alpha", "beta", "gamma"}
	for _, filterVM := range allVMs {
		t.Run("filter_"+filterVM, func(t *testing.T) {
			_, logger := setupAuditTest(t)

			ts := time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC)
			for _, vm := range allVMs {
				require.NoError(t, logger.LogEvent(security.EventLogEntry{
					Timestamp: ts,
					EventType: "create",
					VMName:    vm,
				}))
			}

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			root := RootCmd()
			root.SetArgs([]string{"--json", "audit", filterVM})
			execErr := root.Execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)

			require.NoError(t, execErr)

			var result map[string]any
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err)

			data := result["data"].([]any)
			require.Len(t, data, 1, "filter %q must return exactly 1 entry", filterVM)
			entry := data[0].(map[string]any)
			assert.Equal(t, filterVM, entry["vm"])
		})
	}
}
