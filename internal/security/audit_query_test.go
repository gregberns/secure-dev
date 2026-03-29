package security

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptrString(s string) *string { return &s }
func ptrTime(t time.Time) *time.Time { return &t }

// TestAuditLogger_QueryByVMName verifies VM name filtering.
// REQ-004-022: Query audit log by VM name.
func TestAuditLogger_QueryByVMName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l := NewAuditLogger(path)

	baseTime := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	// Log events for different VMs.
	require.NoError(t, l.LogEvent(EventLogEntry{
		Timestamp: baseTime, EventType: "create", VMName: "vm-alpha",
	}))
	require.NoError(t, l.LogEvent(EventLogEntry{
		Timestamp: baseTime.Add(1 * time.Second), EventType: "start", VMName: "vm-bravo",
	}))
	require.NoError(t, l.LogEvent(EventLogEntry{
		Timestamp: baseTime.Add(2 * time.Second), EventType: "stop", VMName: "vm-alpha",
	}))
	require.NoError(t, l.LogEvent(EventLogEntry{
		Timestamp: baseTime.Add(3 * time.Second), EventType: "create", VMName: "vm-gamma",
	}))
	require.NoError(t, l.LogCommand(CommandLogEntry{
		Timestamp: baseTime.Add(4 * time.Second), Command: "sd list", ExitCode: 0,
	}))

	// Query for vm-alpha: should get 2 events (create + stop).
	entries, err := l.Query(AuditFilter{VMName: ptrString("vm-alpha")})
	require.NoError(t, err)
	require.Len(t, entries, 2)
	for _, e := range entries {
		assert.Equal(t, "vm-alpha", e.VM)
	}

	// Query for vm-bravo: 1 event.
	entries, err = l.Query(AuditFilter{VMName: ptrString("vm-bravo")})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "vm-bravo", entries[0].VM)

	// Query for nonexistent VM: 0 events.
	entries, err = l.Query(AuditFilter{VMName: ptrString("vm-nonexistent")})
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// TestAuditLogger_QueryByTimeRange verifies since/until time filtering.
// REQ-004-022: Query audit log by time range.
func TestAuditLogger_QueryByTimeRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l := NewAuditLogger(path)

	baseTime := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	require.NoError(t, l.LogCommand(CommandLogEntry{
		Timestamp: baseTime, Command: "first", ExitCode: 0,
	}))
	require.NoError(t, l.LogCommand(CommandLogEntry{
		Timestamp: baseTime.Add(1 * time.Hour), Command: "second", ExitCode: 0,
	}))
	require.NoError(t, l.LogCommand(CommandLogEntry{
		Timestamp: baseTime.Add(2 * time.Hour), Command: "third", ExitCode: 0,
	}))

	// Query with "since" filter: only "third".
	since := baseTime.Add(90 * time.Minute)
	entries, err := l.Query(AuditFilter{Since: &since})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "third", entries[0].Command)

	// Query with "until" filter: only "first".
	until := baseTime.Add(30 * time.Minute)
	entries, err = l.Query(AuditFilter{Until: &until})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "first", entries[0].Command)

	// Query with both since and until: "second" only.
	since2 := baseTime.Add(30 * time.Minute)
	until2 := baseTime.Add(90 * time.Minute)
	entries, err = l.Query(AuditFilter{Since: &since2, Until: &until2})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "second", entries[0].Command)
}

// TestAuditLogger_QueryCombinedFilters verifies VM name + time range together.
// REQ-004-022: Combined filter criteria.
func TestAuditLogger_QueryCombinedFilters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l := NewAuditLogger(path)

	baseTime := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	require.NoError(t, l.LogEvent(EventLogEntry{
		Timestamp: baseTime, EventType: "create", VMName: "vm-alpha",
	}))
	require.NoError(t, l.LogEvent(EventLogEntry{
		Timestamp: baseTime.Add(1 * time.Hour), EventType: "start", VMName: "vm-alpha",
	}))
	require.NoError(t, l.LogEvent(EventLogEntry{
		Timestamp: baseTime.Add(2 * time.Hour), EventType: "create", VMName: "vm-bravo",
	}))
	require.NoError(t, l.LogEvent(EventLogEntry{
		Timestamp: baseTime.Add(3 * time.Hour), EventType: "stop", VMName: "vm-alpha",
	}))

	// Query: vm-alpha events after hour 1.5 -> only the "stop" event.
	since := baseTime.Add(90 * time.Minute)
	entries, err := l.Query(AuditFilter{
		VMName: ptrString("vm-alpha"),
		Since:  &since,
	})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "vm-alpha", entries[0].VM)
	assert.Equal(t, "stop", entries[0].EventType)
}

// TestAuditLogger_QueryEmptyLog verifies query on empty log.
// REQ-004-022: Empty log returns empty results.
func TestAuditLogger_QueryEmptyLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l := NewAuditLogger(path)

	// No entries written.
	entries, err := l.Query(AuditFilter{})
	require.NoError(t, err)
	assert.Empty(t, entries)
}
