package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Printer is the output formatting interface for all sd commands.
// Info and Progress write to stderr; Data writes to stdout.
// In JSON mode, Data wraps output in {"ok": true, "data": ...}.
type Printer interface {
	// Info writes an informational message to stderr.
	Info(msg string)

	// Warn writes a warning message to stderr.
	Warn(msg string)

	// Error writes an SDError. In JSON mode, outputs to stdout as JSON.
	// In human mode, prints "Error: <message>" to stderr.
	Error(err *SDError)

	// Data writes structured data to stdout. In JSON mode, wraps in
	// {"ok": true, "data": ...}. In human mode, prints the formatted string.
	Data(v any)

	// Fatal writes an error and exits with the given code.
	Fatal(err *SDError, exitCode int)

	// Progress writes a progress/status message to stderr.
	// In JSON mode, this is a no-op unless verbose.
	Progress(msg string)

	// IsJSON returns true if the printer is in JSON mode.
	IsJSON() bool
}

// NewPrinter creates a Printer. If jsonMode is true, Data and Error output
// goes to stdout as JSON; all other output goes to stderr.
func NewPrinter(stdout, stderr io.Writer, jsonMode bool) Printer {
	if jsonMode {
		return &jsonPrinter{stdout: stdout, stderr: stderr}
	}
	return &humanPrinter{stdout: stdout, stderr: stderr}
}

// humanPrinter writes human-readable output.
type humanPrinter struct {
	stdout io.Writer
	stderr io.Writer
}

func (p *humanPrinter) Info(msg string) {
	fmt.Fprintln(p.stderr, msg)
}

func (p *humanPrinter) Warn(msg string) {
	fmt.Fprintln(p.stderr, "Warning: "+msg)
}

func (p *humanPrinter) Error(err *SDError) {
	fmt.Fprintln(p.stderr, "Error: "+err.Message)
}

func (p *humanPrinter) Data(v any) {
	fmt.Fprintln(p.stdout, v)
}

func (p *humanPrinter) Fatal(err *SDError, exitCode int) {
	p.Error(err)
	os.Exit(exitCode)
}

func (p *humanPrinter) Progress(msg string) {
	fmt.Fprintln(p.stderr, msg)
}

func (p *humanPrinter) IsJSON() bool {
	return false
}

// jsonPrinter writes JSON-formatted output.
type jsonPrinter struct {
	stdout io.Writer
	stderr io.Writer
}

func (p *jsonPrinter) Info(msg string) {
	// In JSON mode, info messages go to stderr only
	fmt.Fprintln(p.stderr, msg)
}

func (p *jsonPrinter) Warn(msg string) {
	fmt.Fprintln(p.stderr, "Warning: "+msg)
}

func (p *jsonPrinter) Error(err *SDError) {
	b, _ := ErrorJSON(err)
	fmt.Fprintln(p.stdout, string(b))
}

func (p *jsonPrinter) Data(v any) {
	b, _ := SuccessJSON(v)
	fmt.Fprintln(p.stdout, string(b))
}

func (p *jsonPrinter) Fatal(err *SDError, exitCode int) {
	p.Error(err)
	os.Exit(exitCode)
}

func (p *jsonPrinter) Progress(msg string) {
	// No-op for JSON mode on stdout; progress goes to stderr only if verbose.
	// Callers should check IsJSON() and decide whether to log.
}

func (p *jsonPrinter) IsJSON() bool {
	return true
}

// MarshalData is a helper that marshals v to indented JSON.
func MarshalData(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
