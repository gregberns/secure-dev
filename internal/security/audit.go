// Package security implements the audit logging system for sd.
// REQ-004-021: Audit Logging — Command Logging
// REQ-004-022: Audit Logging — VM Lifecycle Events with Hash Chain
package security

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// GenesisHash is the well-known starting value for the hash chain.
// REQ-004-022: The first entry MUST use this genesis value.
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// AuditEntry is the union type for a single audit log line.
// REQ-004-022: Each entry includes a prev_hash field with SHA-256 of the preceding line.
type AuditEntry struct {
	Timestamp time.Time `json:"ts"`
	Type      string    `json:"type"` // "command" or "event"
	PrevHash  string    `json:"prev_hash"`

	// Command fields (when Type == "command")
	Command  string   `json:"command,omitempty"`
	Args     []string `json:"args,omitempty"`
	ExitCode int      `json:"exit_code,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`

	// Event fields (when Type == "event")
	EventType string            `json:"event,omitempty"`
	VM        string            `json:"vm,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
}

// CommandLogEntry represents a single CLI command invocation.
// REQ-004-021
type CommandLogEntry struct {
	Timestamp  time.Time
	Command    string
	Args       []string // secrets MUST be redacted before logging
	ExitCode   int
	DurationMs int64
}

// EventLogEntry represents a VM lifecycle event.
// REQ-004-022
type EventLogEntry struct {
	Timestamp time.Time
	EventType string // create, start, stop, destroy, connect, etc.
	VMName    string
	Metadata  map[string]string
}

// AuditFilter specifies criteria for querying the audit log.
type AuditFilter struct {
	VMName *string
	Since  *time.Time
	Until  *time.Time
}

// ChainBreak describes a broken link in the audit log hash chain.
type ChainBreak struct {
	LineNumber   int
	ExpectedHash string
	ActualHash   string
}

// AuditLogger records sd operations and VM lifecycle events.
// REQ-004-021, REQ-004-022
type AuditLogger struct {
	mu       sync.Mutex
	path     string
	lastHash string
}

// NewAuditLogger creates a new audit logger that appends to the given file.
// The file is created if it does not exist.
func NewAuditLogger(path string) *AuditLogger {
	return &AuditLogger{
		path:     path,
		lastHash: GenesisHash,
	}
}

// LogCommand records an sd CLI invocation.
// REQ-004-021: Credentials are redacted before logging.
func (l *AuditLogger) LogCommand(entry CommandLogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Redact secrets from args
	redactedArgs := redactArgs(entry.Args)

	auditEntry := AuditEntry{
		Timestamp:  entry.Timestamp,
		Type:       "command",
		PrevHash:   l.lastHash,
		Command:    entry.Command,
		Args:       redactedArgs,
		ExitCode:   entry.ExitCode,
		DurationMs: entry.DurationMs,
	}

	return l.appendEntry(auditEntry)
}

// LogEvent records a VM lifecycle event.
// REQ-004-022
func (l *AuditLogger) LogEvent(entry EventLogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	auditEntry := AuditEntry{
		Timestamp: entry.Timestamp,
		Type:      "event",
		PrevHash:  l.lastHash,
		EventType: entry.EventType,
		VM:        entry.VMName,
		Meta:      entry.Metadata,
	}

	return l.appendEntry(auditEntry)
}

// appendEntry serializes the entry, writes it to the log file,
// and updates the hash chain.
func (l *AuditLogger) appendEntry(entry AuditEntry) error {
	// Ensure directory exists
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create audit log directory: %w", err)
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal audit entry: %w", err)
	}

	// Append to file
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write audit entry: %w", err)
	}

	// Update hash chain: hash the line we just wrote
	l.lastHash = fmt.Sprintf("%x", sha256.Sum256(line))
	return nil
}

// Query returns log entries matching the given filter.
func (l *AuditLogger) Query(filter AuditFilter) ([]AuditEntry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entries, err := l.readEntries()
	if err != nil {
		return nil, err
	}

	var result []AuditEntry
	for _, e := range entries {
		if filter.VMName != nil && e.VM != *filter.VMName {
			continue
		}
		if filter.Since != nil && e.Timestamp.Before(*filter.Since) {
			continue
		}
		if filter.Until != nil && e.Timestamp.After(*filter.Until) {
			continue
		}
		result = append(result, e)
	}

	return result, nil
}

// VerifyChain validates the hash chain integrity of the audit log.
// REQ-004-022: Returns the index and details of the first broken link, or nil if intact.
func (l *AuditLogger) VerifyChain() (*ChainBreak, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // empty log is valid
		}
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()

	return verifyChainFromEntries(readLinesFromEntries(f))
}

// LastHash returns the current hash at the tip of the chain.
func (l *AuditLogger) LastHash() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastHash
}

// readEntries reads all entries from the log file.
func (l *AuditLogger) readEntries() ([]AuditEntry, error) {
	f, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	lines := readLinesFromEntries(f)
	var entries []AuditEntry
	for _, line := range lines {
		var entry AuditEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // skip malformed lines
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// verifyChainFromEntries verifies the hash chain of a sequence of log lines.
func verifyChainFromEntries(lines []string) (*ChainBreak, error) {
	prevHash := GenesisHash
	for i, line := range lines {
		if line == "" {
			continue
		}

		var entry AuditEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return &ChainBreak{
				LineNumber:   i + 1,
				ExpectedHash: prevHash,
				ActualHash:   "PARSE_ERROR",
			}, nil
		}

		if entry.PrevHash != prevHash {
			return &ChainBreak{
				LineNumber:   i + 1,
				ExpectedHash: prevHash,
				ActualHash:   entry.PrevHash,
			}, nil
		}

		prevHash = fmt.Sprintf("%x", sha256.Sum256([]byte(line)))
	}
	return nil, nil
}

// redactArgs redacts known sensitive argument patterns.
// REQ-004-021: Credentials and API key values are never written to the audit log.
func redactArgs(args []string) []string {
	sensitivePrefixes := []string{
		"ghp_",        // classic GitHub PAT
		"github_pat_", // fine-grained GitHub PAT
		"sk-ant-",     // Anthropic API key
		"gho_",        // GitHub OAuth token
		"ghu_",        // GitHub user-to-server token
		"ghs_",        // GitHub server-to-server token
	}

	result := make([]string, len(args))
	for i, arg := range args {
		redacted := false
		for _, prefix := range sensitivePrefixes {
			if strings.Contains(arg, prefix) {
				result[i] = RedactToken(arg)
				redacted = true
				break
			}
		}
		if !redacted {
			result[i] = arg
		}
	}
	return result
}

// readLinesFromEntries reads all non-empty lines from a reader.
func readLinesFromEntries(r io.Reader) []string {
	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// ComputeEntryHash computes the SHA-256 hash of a serialized audit entry.
// This is exported for testing purposes.
func ComputeEntryHash(line string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(line)))
}
