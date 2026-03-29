package ui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewPrinter_Human(t *testing.T) {
	p := NewPrinter(nil, nil, false)
	if p.IsJSON() {
		t.Error("human printer should not be JSON mode")
	}
}

func TestNewPrinter_JSON(t *testing.T) {
	p := NewPrinter(nil, nil, true)
	if !p.IsJSON() {
		t.Error("json printer should be JSON mode")
	}
}

func TestHumanPrinter_Info(t *testing.T) {
	var stderr bytes.Buffer
	p := NewPrinter(nil, &stderr, false)
	p.Info("hello world")

	if got := stderr.String(); !strings.Contains(got, "hello world") {
		t.Errorf("Info output = %q, want to contain %q", got, "hello world")
	}
}

func TestHumanPrinter_Warn(t *testing.T) {
	var stderr bytes.Buffer
	p := NewPrinter(nil, &stderr, false)
	p.Warn("disk low")

	if got := stderr.String(); !strings.Contains(got, "Warning: disk low") {
		t.Errorf("Warn output = %q, want to contain %q", got, "Warning: disk low")
	}
}

func TestHumanPrinter_Error(t *testing.T) {
	var stderr bytes.Buffer
	p := NewPrinter(nil, &stderr, false)
	p.Error(ErrVMNotFound("myvm"))

	got := stderr.String()
	if !strings.Contains(got, "Error:") {
		t.Errorf("Error output = %q, want to contain %q", got, "Error:")
	}
	if !strings.Contains(got, "does not exist") {
		t.Errorf("Error output = %q, want to contain %q", got, "does not exist")
	}
}

func TestHumanPrinter_Data(t *testing.T) {
	var stdout bytes.Buffer
	p := NewPrinter(&stdout, nil, false)
	p.Data("some output")

	if got := strings.TrimSpace(stdout.String()); got != "some output" {
		t.Errorf("Data output = %q, want %q", got, "some output")
	}
}

func TestHumanPrinter_Progress(t *testing.T) {
	var stderr bytes.Buffer
	p := NewPrinter(nil, &stderr, false)
	p.Progress("creating VM...")

	if got := stderr.String(); !strings.Contains(got, "creating VM...") {
		t.Errorf("Progress output = %q, want to contain %q", got, "creating VM...")
	}
}

func TestHumanPrinter_StreamRouting(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := NewPrinter(&stdout, &stderr, false)

	p.Info("info msg")
	p.Warn("warn msg")
	p.Progress("progress msg")
	p.Data("data output")

	// Info, Warn, Progress go to stderr
	if !strings.Contains(stderr.String(), "info msg") {
		t.Error("Info should write to stderr")
	}
	if !strings.Contains(stderr.String(), "warn msg") {
		t.Error("Warn should write to stderr")
	}
	if !strings.Contains(stderr.String(), "progress msg") {
		t.Error("Progress should write to stderr")
	}

	// Data goes to stdout
	if !strings.Contains(stdout.String(), "data output") {
		t.Error("Data should write to stdout")
	}
	// Data should NOT go to stderr
	if strings.Contains(stderr.String(), "data output") {
		t.Error("Data should not write to stderr")
	}
}

func TestJSONPrinter_Data(t *testing.T) {
	var stdout bytes.Buffer
	p := NewPrinter(&stdout, nil, true)

	data := map[string]string{"name": "myvm"}
	p.Data(data)

	var resp JSONResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("Data output is not valid JSON: %v", err)
	}
	if !resp.OK {
		t.Error("OK should be true for Data")
	}
}

func TestJSONPrinter_Error(t *testing.T) {
	var stdout bytes.Buffer
	p := NewPrinter(&stdout, nil, true)

	p.Error(ErrVMNotFound("test"))

	var resp JSONResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("Error output is not valid JSON: %v", err)
	}
	if resp.OK {
		t.Error("OK should be false for Error")
	}
	if resp.Error == nil {
		t.Fatal("Error body should not be nil")
	}
	if resp.Error.Code != ErrCodeVMNotFound {
		t.Errorf("Error code = %q, want %q", resp.Error.Code, ErrCodeVMNotFound)
	}
}

func TestJSONPrinter_Progress_NoOp(t *testing.T) {
	var stdout bytes.Buffer
	p := NewPrinter(&stdout, nil, true)
	p.Progress("should not appear")

	if stdout.Len() > 0 {
		t.Errorf("JSON progress should be no-op on stdout, got %q", stdout.String())
	}
}

func TestJSONPrinter_StreamRouting(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := NewPrinter(&stdout, &stderr, true)

	p.Info("info msg")
	p.Warn("warn msg")
	p.Data(map[string]string{"key": "val"})
	p.Error(NewSDError("test_error", "test"))

	// Info and Warn go to stderr
	if !strings.Contains(stderr.String(), "info msg") {
		t.Error("Info should go to stderr in JSON mode")
	}
	if !strings.Contains(stderr.String(), "warn msg") {
		t.Error("Warn should go to stderr in JSON mode")
	}

	// Data and Error go to stdout as JSON
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSON lines on stdout, got %d: %q", len(lines), stdout.String())
	}

	// First line is Data (ok: true)
	var dataResp JSONResponse
	if err := json.Unmarshal([]byte(lines[0]), &dataResp); err != nil {
		t.Fatalf("line 0 is not valid JSON: %v", err)
	}
	if !dataResp.OK {
		t.Error("Data response should have ok: true")
	}

	// Second line is Error (ok: false)
	var errResp JSONResponse
	if err := json.Unmarshal([]byte(lines[1]), &errResp); err != nil {
		t.Fatalf("line 1 is not valid JSON: %v", err)
	}
	if errResp.OK {
		t.Error("Error response should have ok: false")
	}
}
