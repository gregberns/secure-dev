package security

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuditLogger_ConcurrentWrites verifies that concurrent LogCommand and
// LogEvent calls preserve hash chain integrity without deadlock.
// REQ-004-022: Hash chain integrity under concurrent writes.
func TestAuditLogger_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l := NewAuditLogger(path)

	var wg sync.WaitGroup
	errCh := make(chan error, 200)

	// Write 50 commands and 50 events concurrently.
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			err := l.LogCommand(CommandLogEntry{
				Timestamp:  time.Now(),
				Command:    "test-cmd",
				Args:       []string{"--idx"},
				ExitCode:   idx % 5,
				DurationMs: int64(idx),
			})
			if err != nil {
				errCh <- err
			}
		}(i)
		go func(idx int) {
			defer wg.Done()
			err := l.LogEvent(EventLogEntry{
				Timestamp: time.Now(),
				EventType: "create",
				VMName:    "test-vm",
				Metadata:  map[string]string{"idx": "event"},
			})
			if err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent write error: %v", err)
	}

	// Verify chain integrity.
	brk, err := l.VerifyChain()
	require.NoError(t, err)
	assert.Nil(t, brk, "hash chain should be intact after 100 concurrent writes")
}

// TestAuditLogger_ConcurrentReadsWhileWriting verifies reading while writing does not deadlock.
func TestAuditLogger_ConcurrentReadsWhileWriting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l := NewAuditLogger(path)

	// Pre-populate some entries.
	for i := 0; i < 10; i++ {
		require.NoError(t, l.LogCommand(CommandLogEntry{
			Timestamp: time.Now(),
			Command:   "init",
			ExitCode:  0,
		}))
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	// Concurrent writers.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := l.LogEvent(EventLogEntry{
				Timestamp: time.Now(),
				EventType: "start",
				VMName:    "concurrent-vm",
			})
			if err != nil {
				errCh <- err
			}
		}()
	}

	// Concurrent readers.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := l.Query(AuditFilter{})
			if err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent read/write error: %v", err)
	}

	// Chain should still be intact.
	brk, err := l.VerifyChain()
	require.NoError(t, err)
	assert.Nil(t, brk)
}
