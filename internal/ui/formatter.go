// Package ui provides output formatting for the sd CLI.
// REQ-002-011: Human-readable output conventions
// REQ-002-012: JSON output format
// REQ-002-018: Error serialization
package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// CLIError is a JSON-serializable error with a machine-readable code.
// REQ-002-018
type CLIError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e CLIError) Error() string {
	return e.Message
}

// jsonSuccess is the JSON envelope for successful responses.
// REQ-002-012
type jsonSuccess struct {
	OK   bool `json:"ok"`
	Data any  `json:"data"`
}

// jsonError is the JSON envelope for error responses.
// REQ-002-012
type jsonError struct {
	OK    bool     `json:"ok"`
	Error CLIError `json:"error"`
}

// Formatter controls how command output is rendered.
// REQ-002-011, REQ-002-012
type Formatter struct {
	jsonMode bool
	stdout   io.Writer
	stderr   io.Writer
}

// NewFormatter creates a new Formatter.
// When jsonMode is true, structured JSON is written to stdout.
func NewFormatter(jsonMode bool) *Formatter {
	return &Formatter{
		jsonMode: jsonMode,
		stdout:   os.Stdout,
		stderr:   os.Stderr,
	}
}

// NewFormatterWithWriters creates a Formatter with custom writers (for testing).
func NewFormatterWithWriters(jsonMode bool, stdout, stderr io.Writer) *Formatter {
	return &Formatter{
		jsonMode: jsonMode,
		stdout:   stdout,
		stderr:   stderr,
	}
}

// JSONMode returns whether the formatter is in JSON mode.
func (f *Formatter) JSONMode() bool {
	return f.jsonMode
}

// Success outputs a successful result.
// In JSON mode, wraps data in {"ok": true, "data": ...} and writes to stdout.
// In human mode, writes formatted text to stdout.
// REQ-002-012
func (f *Formatter) Success(data any) {
	if f.jsonMode {
		enc := json.NewEncoder(f.stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(jsonSuccess{OK: true, Data: data})
	} else if s, ok := data.(string); ok {
		fmt.Fprintln(f.stdout, s)
	}
}

// SuccessData outputs structured data in the appropriate format.
// In JSON mode, writes the full JSON envelope.
// In human mode, the formatFunc is called to produce human-readable output.
func (f *Formatter) SuccessData(data any, formatFunc func() string) {
	if f.jsonMode {
		enc := json.NewEncoder(f.stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(jsonSuccess{OK: true, Data: data})
	} else if formatFunc != nil {
		fmt.Fprint(f.stdout, formatFunc())
	}
}

// Error outputs an error.
// In JSON mode, writes {"ok": false, "error": {...}} to stdout.
// In human mode, writes "Error: <message>" to stderr.
// REQ-002-012, REQ-002-018
func (f *Formatter) Error(err CLIError) {
	if f.jsonMode {
		enc := json.NewEncoder(f.stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(jsonError{OK: false, Error: err})
	} else {
		fmt.Fprintf(f.stderr, "Error: %s\n", err.Message)
	}
}

// Progress writes a progress message to stderr.
// In JSON mode, writes to stderr only (possibly suppressed).
// In human mode, writes to stderr.
// REQ-002-011
func (f *Formatter) Progress(msg string) {
	fmt.Fprintln(f.stderr, msg)
}

// Warn writes a warning message to stderr in both modes.
func (f *Formatter) Warn(msg string) {
	fmt.Fprintf(f.stderr, "Warning: %s\n", msg)
}
