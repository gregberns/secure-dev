// Package security — unit and property-based tests for the audit logging system.
// REQ-004-021: Audit Logging — Command Logging
// REQ-004-022: Audit Logging — VM Lifecycle Events with Hash Chain
package security

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- REQ-004-021: Command Logging ---

func TestAuditLogger_LogCommand_WritesJSONLine(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	logger := NewAuditLogger(logPath)

	ts := time.Date(2026, 3, 27, 10, 15, 30, 0, time.UTC)
	err := logger.LogCommand(CommandLogEntry{
		Timestamp:  ts,
		Command:    "sd create myvm",
		Args:       []string{"create", "myvm", "--allow-egress", "custom.example.com"},
		ExitCode:   0,
		DurationMs: 4523,
	})
	require.NoError(t, err)

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)

	var entry AuditEntry
	require.NoError(t, json.Unmarshal(data, &entry))

	assert.Equal(t, "command", entry.Type)
	assert.Equal(t, "sd create myvm", entry.Command)
	assert.Equal(t, []string{"create", "myvm", "--allow-egress", "custom.example.com"}, entry.Args)
	assert.Equal(t, 0, entry.ExitCode)
	assert.Equal(t, int64(4523), entry.DurationMs)
	assert.Equal(t, GenesisHash, entry.PrevHash)
}

func TestAuditLogger_LogCommand_RedactsTokens(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	logger := NewAuditLogger(logPath)

	err := logger.LogCommand(CommandLogEntry{
		Timestamp: time.Now(),
		Command:   "sd connect myvm",
		Args:      []string{"connect", "myvm", "--token", "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ"},
		ExitCode:  0,
	})
	require.NoError(t, err)

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)

	// The raw log must NOT contain the token value
	assert.NotContains(t, string(data), "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	assert.Contains(t, string(data), "[REDACTED]")
}

func TestAuditLogger_LogCommand_RedactsAnthropicKey(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	logger := NewAuditLogger(logPath)

	err := logger.LogCommand(CommandLogEntry{
		Timestamp: time.Now(),
		Command:   "sd connect myvm",
		Args:      []string{"connect", "myvm", "--api-key", "sk-ant-api-key-1234567890"},
		ExitCode:  0,
	})
	require.NoError(t, err)

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)

	assert.NotContains(t, string(data), "sk-ant-api-key-1234567890")
	assert.Contains(t, string(data), "[REDACTED]")
}

func TestAuditLogger_LogCommand_RedactsFinegrainedPAT(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	logger := NewAuditLogger(logPath)

	err := logger.LogCommand(CommandLogEntry{
		Timestamp: time.Now(),
		Command:   "sd connect myvm",
		Args:      []string{"connect", "myvm", "--token", "github_pat_ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890"},
		ExitCode:  0,
	})
	require.NoError(t, err)

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)

	assert.NotContains(t, string(data), "github_pat_ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890")
	assert.Contains(t, string(data), "[REDACTED]")
}

func TestAuditLogger_LogCommand_PreservesNonSensitiveArgs(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	logger := NewAuditLogger(logPath)

	err := logger.LogCommand(CommandLogEntry{
		Timestamp: time.Now(),
		Command:   "sd create myvm",
		Args:      []string{"create", "myvm", "--cpus", "8", "--memory", "16GiB"},
		ExitCode:  0,
	})
	require.NoError(t, err)

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)

	assert.Contains(t, string(data), "create")
	assert.Contains(t, string(data), "myvm")
	assert.Contains(t, string(data), "16GiB")
	assert.NotContains(t, string(data), "[REDACTED]")
}

// --- REQ-004-022: VM Lifecycle Events with Hash Chain ---

func TestAuditLogger_LogEvent_WritesJSONLine(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	logger := NewAuditLogger(logPath)

	ts := time.Date(2026, 3, 27, 10, 15, 30, 0, time.UTC)
	err := logger.LogEvent(EventLogEntry{
		Timestamp: ts,
		EventType: "create",
		VMName:    "myvm",
		Metadata:  map[string]string{"mounts": "0", "egress_rules": "13"},
	})
	require.NoError(t, err)

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)

	var entry AuditEntry
	require.NoError(t, json.Unmarshal(data, &entry))

	assert.Equal(t, "event", entry.Type)
	assert.Equal(t, "create", entry.EventType)
	assert.Equal(t, "myvm", entry.VM)
	assert.Equal(t, map[string]string{"mounts": "0", "egress_rules": "13"}, entry.Meta)
	assert.Equal(t, GenesisHash, entry.PrevHash)
}

func TestAuditLogger_HashChain_TwoEntries(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	logger := NewAuditLogger(logPath)

	// Write first entry
	err := logger.LogCommand(CommandLogEntry{
		Timestamp: time.Date(2026, 3, 27, 10, 15, 30, 0, time.UTC),
		Command:   "sd create myvm",
		Args:      []string{"create", "myvm"},
		ExitCode:  0,
	})
	require.NoError(t, err)

	firstHash := logger.LastHash()
	assert.NotEqual(t, GenesisHash, firstHash, "hash should change after first entry")

	// Write second entry
	err = logger.LogEvent(EventLogEntry{
		Timestamp: time.Date(2026, 3, 27, 10, 15, 30, 0, time.UTC),
		EventType: "create",
		VMName:    "myvm",
	})
	require.NoError(t, err)

	// Read both entries and verify chain
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.Len(t, lines, 2)

	// First entry's prev_hash must be genesis
	var first AuditEntry
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first))
	assert.Equal(t, GenesisHash, first.PrevHash)

	// Second entry's prev_hash must be SHA-256 of first line
	var second AuditEntry
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &second))
	expectedHash := ComputeEntryHash(lines[0])
	assert.Equal(t, expectedHash, second.PrevHash)
}

func TestAuditLogger_VerifyChain_Intact(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	logger := NewAuditLogger(logPath)

	for i := 0; i < 5; i++ {
		err := logger.LogEvent(EventLogEntry{
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			EventType: "start",
			VMName:    fmt.Sprintf("vm-%d", i),
		})
		require.NoError(t, err)
	}

	breakInfo, err := logger.VerifyChain()
	require.NoError(t, err)
	assert.Nil(t, breakInfo, "chain should be intact")
}

func TestAuditLogger_VerifyChain_DetectsTampering(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	logger := NewAuditLogger(logPath)

	// Write some entries
	for i := 0; i < 5; i++ {
		err := logger.LogEvent(EventLogEntry{
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			EventType: "start",
			VMName:    fmt.Sprintf("vm-%d", i),
		})
		require.NoError(t, err)
	}

	// Tamper with the third line
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	lines := strings.Split(string(data), "\n")
	require.GreaterOrEqual(t, len(lines), 4)

	// Tamper: replace the third entry with different content
	var tampered AuditEntry
	require.NoError(t, json.Unmarshal([]byte(lines[2]), &tampered))
	tampered.VM = "TAMPERED"
	tamperedLine, err := json.Marshal(tampered)
	require.NoError(t, err)
	lines[2] = string(tamperedLine)

	// Write back
	tamperedData := strings.Join(lines, "\n")
	require.NoError(t, os.WriteFile(logPath, []byte(tamperedData), 0600))

	// Verify should detect the break at the next line (line 4, 1-indexed),
	// since the tampered line 3's content no longer matches line 4's prev_hash.
	breakInfo, err := logger.VerifyChain()
	require.NoError(t, err)
	require.NotNil(t, breakInfo, "tampered chain should be detected")
	assert.Equal(t, 4, breakInfo.LineNumber)
}

func TestAuditLogger_VerifyChain_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	logger := NewAuditLogger(logPath)

	breakInfo, err := logger.VerifyChain()
	require.NoError(t, err)
	assert.Nil(t, breakInfo, "empty log should be valid")
}

// --- REQ-004-022: Query with Filter ---

func TestAuditLogger_Query_FilterByVMName(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	logger := NewAuditLogger(logPath)

	// Log events for different VMs
	for _, vmName := range []string{"vm-a", "vm-b", "vm-a", "vm-c"} {
		err := logger.LogEvent(EventLogEntry{
			Timestamp: time.Now(),
			EventType: "start",
			VMName:    vmName,
		})
		require.NoError(t, err)
	}

	vmName := "vm-a"
	entries, err := logger.Query(AuditFilter{VMName: &vmName})
	require.NoError(t, err)
	assert.Len(t, entries, 2)

	for _, e := range entries {
		assert.Equal(t, "vm-a", e.VM)
	}
}

func TestAuditLogger_Query_FilterByTime(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	logger := NewAuditLogger(logPath)

	baseTime := time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		err := logger.LogEvent(EventLogEntry{
			Timestamp: baseTime.Add(time.Duration(i) * time.Hour),
			EventType: "start",
			VMName:    "myvm",
		})
		require.NoError(t, err)
	}

	since := baseTime.Add(2 * time.Hour)
	entries, err := logger.Query(AuditFilter{Since: &since})
	require.NoError(t, err)
	assert.Len(t, entries, 3) // hours 2, 3, 4

	until := baseTime.Add(3 * time.Hour)
	entries, err = logger.Query(AuditFilter{Since: &since, Until: &until})
	require.NoError(t, err)
	assert.Len(t, entries, 2) // hours 2, 3
}

// --- Genesis Hash ---

func TestGenesisHash_Is64HexChars(t *testing.T) {
	assert.Len(t, GenesisHash, 64, "SHA-256 hex must be 64 chars")
	for _, c := range GenesisHash {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"genesis hash must be hex: got %c", c)
	}
	assert.Equal(t, "0000000000000000000000000000000000000000000000000000000000000000", GenesisHash)
}

// --- File Permissions ---

func TestAuditLogger_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "deep", "nested", "audit.log")
	logger := NewAuditLogger(logPath)

	err := logger.LogCommand(CommandLogEntry{
		Timestamp: time.Now(),
		Command:   "sd create myvm",
		ExitCode:  0,
	})
	require.NoError(t, err)

	_, err = os.Stat(logPath)
	assert.NoError(t, err)
}

func TestAuditLogger_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	logger := NewAuditLogger(logPath)

	err := logger.LogCommand(CommandLogEntry{
		Timestamp: time.Now(),
		Command:   "sd create myvm",
		ExitCode:  0,
	})
	require.NoError(t, err)

	info, err := os.Stat(logPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm(), "audit log must be 0600")
}

// ============================================================
// Property-Based Tests
// ============================================================

// Property: Every log entry's prev_hash matches the SHA-256 of the preceding line.
// REQ-004-022
func TestAuditLogger_HashChainIntegrity_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir, err := os.MkdirTemp("", "sd-audit-*")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		logPath := filepath.Join(dir, "audit.log")
		logger := NewAuditLogger(logPath)

		// Generate 1-20 entries
		n := rapid.IntRange(1, 20).Draw(t, "n")

		for i := 0; i < n; i++ {
			entryType := rapid.SampledFrom([]string{"command", "event"}).Draw(t, "type")
			if entryType == "command" {
				err := logger.LogCommand(CommandLogEntry{
					Timestamp:  time.Now().Add(time.Duration(i) * time.Second),
					Command:    fmt.Sprintf("sd cmd-%d", i),
					Args:       []string{fmt.Sprintf("arg-%d", i)},
					ExitCode:   rapid.IntRange(0, 2).Draw(t, "exit"),
					DurationMs: int64(rapid.IntRange(0, 10000).Draw(t, "duration")),
				})
				require.NoError(t, err)
			} else {
				err := logger.LogEvent(EventLogEntry{
					Timestamp: time.Now().Add(time.Duration(i) * time.Second),
					EventType: rapid.SampledFrom([]string{
						"create", "start", "stop", "destroy", "connect",
						"disconnect", "snapshot-create", "snapshot-restore",
						"token-rotate", "token-revoke", "config-change",
					}).Draw(t, "eventType"),
					VMName: fmt.Sprintf("vm-%d", i%3),
				})
				require.NoError(t, err)
			}
		}

		// Verify chain
		breakInfo, err := logger.VerifyChain()
		require.NoError(t, err)
		assert.Nil(t, breakInfo, "hash chain must be intact for %d entries", n)
	})
}

// Property: First entry always has genesis hash as prev_hash.
func TestAuditLogger_FirstEntryGenesisHash_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir, err := os.MkdirTemp("", "sd-audit-*")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		logPath := filepath.Join(dir, "audit.log")
		logger := NewAuditLogger(logPath)

		err = logger.LogCommand(CommandLogEntry{
			Timestamp: time.Now(),
			Command:   rapid.StringMatching(`sd [a-z-]{2,20}`).Draw(t, "cmd"),
			ExitCode:  rapid.IntRange(0, 2).Draw(t, "exit"),
		})
		require.NoError(t, err)

		data, err := os.ReadFile(logPath)
		require.NoError(t, err)

		var entry AuditEntry
		require.NoError(t, json.Unmarshal(data, &entry))
		assert.Equal(t, GenesisHash, entry.PrevHash)
	})
}

// Property: Each entry's prev_hash equals SHA-256 of the line before it.
func TestAuditLogger_PrevHashMatchesPreviousLine_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir, err := os.MkdirTemp("", "sd-audit-*")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		logPath := filepath.Join(dir, "audit.log")
		logger := NewAuditLogger(logPath)

		n := rapid.IntRange(2, 10).Draw(t, "n")

		for i := 0; i < n; i++ {
			err := logger.LogEvent(EventLogEntry{
				Timestamp: time.Now().Add(time.Duration(i) * time.Second),
				EventType: "start",
				VMName:    fmt.Sprintf("vm-%d", i),
			})
			require.NoError(t, err)
		}

		data, err := os.ReadFile(logPath)
		require.NoError(t, err)
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")

		for i := 1; i < len(lines); i++ {
			var entry AuditEntry
			require.NoError(t, json.Unmarshal([]byte(lines[i]), &entry))

			expectedPrev := ComputeEntryHash(lines[i-1])
			assert.Equal(t, expectedPrev, entry.PrevHash,
				"line %d prev_hash must be SHA-256 of line %d", i+1, i)
		}
	})
}

// Property: VerifyChain detects any single-line tampering of non-last lines.
func TestAuditLogger_DetectsSingleLineTamper_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir, err := os.MkdirTemp("", "sd-audit-*")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		logPath := filepath.Join(dir, "audit.log")
		logger := NewAuditLogger(logPath)

		n := rapid.IntRange(3, 10).Draw(t, "n")
		for i := 0; i < n; i++ {
			err := logger.LogEvent(EventLogEntry{
				Timestamp: time.Now().Add(time.Duration(i) * time.Second),
				EventType: "start",
				VMName:    fmt.Sprintf("vm-%d", i),
			})
			require.NoError(t, err)
		}

		// Pick a line to tamper — must not be the last line, since tampering
		// the last line can only be detected by comparing content, not chain links.
		tamperIdx := rapid.IntRange(0, n-2).Draw(t, "tamperIdx")

		data, err := os.ReadFile(logPath)
		require.NoError(t, err)
		lines := strings.Split(string(data), "\n")

		// Tamper the line by modifying its VM name
		var entry AuditEntry
		require.NoError(t, json.Unmarshal([]byte(lines[tamperIdx]), &entry))
		entry.VM = "TAMPERED_" + entry.VM
		tamperedLine, err := json.Marshal(entry)
		require.NoError(t, err)
		lines[tamperIdx] = string(tamperedLine)

		require.NoError(t, os.WriteFile(logPath, []byte(strings.Join(lines, "\n")), 0600))

		breakInfo, err := logger.VerifyChain()
		require.NoError(t, err)
		assert.NotNil(t, breakInfo,
			"tampering line %d of %d must be detected", tamperIdx, n)
	})
}

// Property: Token values are never written to the audit log.
// REQ-004-021
func TestAuditLogger_NeverWritesTokenValues_Property(t *testing.T) {
	sensitiveTokens := []string{
		"ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"github_pat_ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890",
		"sk-ant-api-key-1234567890",
		"gho_ABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"ghu_ABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"ghs_ABCDEFGHIJKLMNOPQRSTUVWXYZ",
	}

	rapid.Check(t, func(t *rapid.T) {
		dir, err := os.MkdirTemp("", "sd-audit-*")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		logPath := filepath.Join(dir, "audit.log")
		logger := NewAuditLogger(logPath)

		token := rapid.SampledFrom(sensitiveTokens).Draw(t, "token")

		err = logger.LogCommand(CommandLogEntry{
			Timestamp: time.Now(),
			Command:   "sd connect myvm",
			Args:      []string{"connect", "myvm", "--token", token},
			ExitCode:  0,
		})
		require.NoError(t, err)

		data, err := os.ReadFile(logPath)
		require.NoError(t, err)

		assert.NotContains(t, string(data), token,
			"token value must not appear in audit log")
	})
}

// Property: Audit log entries are valid JSON.
func TestAuditLogger_EntriesAreValidJSON_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir, err := os.MkdirTemp("", "sd-audit-*")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		logPath := filepath.Join(dir, "audit.log")
		logger := NewAuditLogger(logPath)

		n := rapid.IntRange(1, 10).Draw(t, "n")
		for i := 0; i < n; i++ {
			err := logger.LogCommand(CommandLogEntry{
				Timestamp:  time.Now().Add(time.Duration(i) * time.Second),
				Command:    fmt.Sprintf("sd cmd-%d", i),
				Args:       []string{fmt.Sprintf("arg-%d", i)},
				ExitCode:   0,
				DurationMs: 100,
			})
			require.NoError(t, err)
		}

		data, err := os.ReadFile(logPath)
		require.NoError(t, err)
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")

		assert.Len(t, lines, n)
		for i, line := range lines {
			var entry AuditEntry
			assert.NoError(t, json.Unmarshal([]byte(line), &entry),
				"line %d must be valid JSON: %s", i, line)
		}
	})
}

// Property: LogEvent entries always have type "event".
func TestAuditLogger_EventTypeIsEvent_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir, err := os.MkdirTemp("", "sd-audit-*")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		logPath := filepath.Join(dir, "audit.log")
		logger := NewAuditLogger(logPath)

		eventType := rapid.SampledFrom([]string{
			"create", "start", "stop", "destroy", "connect",
		}).Draw(t, "eventType")

		err = logger.LogEvent(EventLogEntry{
			Timestamp: time.Now(),
			EventType: eventType,
			VMName:    "testvm",
		})
		require.NoError(t, err)

		data, err := os.ReadFile(logPath)
		require.NoError(t, err)

		var entry AuditEntry
		require.NoError(t, json.Unmarshal(data, &entry))
		assert.Equal(t, "event", entry.Type)
		assert.Equal(t, eventType, entry.EventType)
	})
}

// Property: LogCommand entries always have type "command".
func TestAuditLogger_CommandTypeIsCommand_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir, err := os.MkdirTemp("", "sd-audit-*")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		logPath := filepath.Join(dir, "audit.log")
		logger := NewAuditLogger(logPath)

		err = logger.LogCommand(CommandLogEntry{
			Timestamp: time.Now(),
			Command:   "sd test",
			ExitCode:  0,
		})
		require.NoError(t, err)

		data, err := os.ReadFile(logPath)
		require.NoError(t, err)

		var entry AuditEntry
		require.NoError(t, json.Unmarshal(data, &entry))
		assert.Equal(t, "command", entry.Type)
		assert.Equal(t, "sd test", entry.Command)
	})
}

// Property: LastHash changes after every write.
func TestAuditLogger_LastHashChanges_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir, err := os.MkdirTemp("", "sd-audit-*")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		logPath := filepath.Join(dir, "audit.log")
		logger := NewAuditLogger(logPath)

		assert.Equal(t, GenesisHash, logger.LastHash())

		prevHash := logger.LastHash()
		n := rapid.IntRange(1, 10).Draw(t, "n")

		for i := 0; i < n; i++ {
			err := logger.LogEvent(EventLogEntry{
				Timestamp: time.Now().Add(time.Duration(i) * time.Second),
				EventType: "start",
				VMName:    "testvm",
			})
			require.NoError(t, err)

			newHash := logger.LastHash()
			assert.NotEqual(t, prevHash, newHash,
				"hash must change after each write")
			prevHash = newHash
		}
	})
}

// Property: Query with no filter returns all entries.
func TestAuditLogger_QueryNoFilterReturnsAll_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir, err := os.MkdirTemp("", "sd-audit-*")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		logPath := filepath.Join(dir, "audit.log")
		logger := NewAuditLogger(logPath)

		n := rapid.IntRange(1, 20).Draw(t, "n")

		for i := 0; i < n; i++ {
			err := logger.LogCommand(CommandLogEntry{
				Timestamp: time.Now().Add(time.Duration(i) * time.Second),
				Command:   fmt.Sprintf("cmd-%d", i),
				ExitCode:  0,
			})
			require.NoError(t, err)
		}

		entries, err := logger.Query(AuditFilter{})
		require.NoError(t, err)
		assert.Len(t, entries, n)
	})
}

// Property: Non-sensitive args are never redacted.
func TestAuditLogger_NonSensitiveArgsPreserved_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		arg := rapid.StringMatching(`[a-z0-9_-]{2,20}`).Draw(t, "arg")

		result := redactArgs([]string{arg})
		assert.Equal(t, arg, result[0],
			"non-sensitive arg must be preserved")
	})
}

// Property: Any arg containing a sensitive prefix is always redacted.
func TestAuditLogger_SensitiveArgsAlwaysRedacted_Property(t *testing.T) {
	prefixes := []string{
		"ghp_", "github_pat_", "sk-ant-", "gho_", "ghu_", "ghs_",
	}

	rapid.Check(t, func(t *rapid.T) {
		prefix := rapid.SampledFrom(prefixes).Draw(t, "prefix")
		suffix := rapid.StringMatching(`[A-Za-z0-9_-]{10,40}`).Draw(t, "suffix")
		token := prefix + suffix

		result := redactArgs([]string{token})
		assert.NotContains(t, result[0], token,
			"sensitive token must be redacted")
		assert.Contains(t, result[0], "[REDACTED]",
			"redacted arg must contain [REDACTED]")
	})
}

// Property: VerifyChain on an empty/nonexistent file returns nil (valid).
func TestAuditLogger_EmptyFileChainValid_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir, err := os.MkdirTemp("", "sd-audit-*")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		logPath := filepath.Join(dir, "audit.log")
		logger := NewAuditLogger(logPath)

		breakInfo, err := logger.VerifyChain()
		require.NoError(t, err)
		assert.Nil(t, breakInfo)
	})
}

// Property: AuditEntry JSON always contains required fields.
func TestAuditEntry_JSONRequiredFields_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir, err := os.MkdirTemp("", "sd-audit-*")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		logPath := filepath.Join(dir, "audit.log")
		logger := NewAuditLogger(logPath)

		_ = logger.LogCommand(CommandLogEntry{
			Timestamp: time.Now(),
			Command:   "sd test",
			ExitCode:  0,
		})
		require.NoError(t, err)

		data, err := os.ReadFile(logPath)
		require.NoError(t, err)

		// Must contain required JSON fields
		assert.Contains(t, string(data), `"ts"`)
		assert.Contains(t, string(data), `"type"`)
		assert.Contains(t, string(data), `"prev_hash"`)
	})
}

// --- Benchmarks ---

func BenchmarkAuditLogger_LogCommand(b *testing.B) {
	dir := b.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	logger := NewAuditLogger(logPath)

	entry := CommandLogEntry{
		Timestamp:  time.Now(),
		Command:    "sd create myvm",
		Args:       []string{"create", "myvm"},
		ExitCode:   0,
		DurationMs: 100,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry.Timestamp = time.Now()
		_ = logger.LogCommand(entry)
	}
}

func BenchmarkAuditLogger_VerifyChain(b *testing.B) {
	dir := b.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	logger := NewAuditLogger(logPath)

	// Pre-populate 100 entries
	for i := 0; i < 100; i++ {
		_ = logger.LogEvent(EventLogEntry{
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			EventType: "start",
			VMName:    "benchvm",
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = logger.VerifyChain()
	}
}
