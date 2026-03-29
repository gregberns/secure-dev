package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestTable_Render(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable(&buf, "NAME", "STATUS", "BACKEND")
	tbl.AddRow("myvm", "running", "lima")
	tbl.AddRow("dev", "stopped", "lima")
	tbl.Render()

	got := buf.String()
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")

	if len(lines) != 4 { // header + separator + 2 rows
		t.Fatalf("expected 4 lines, got %d: %q", len(lines), got)
	}

	// Header line
	if !strings.Contains(lines[0], "NAME") || !strings.Contains(lines[0], "STATUS") || !strings.Contains(lines[0], "BACKEND") {
		t.Errorf("header line = %q, want NAME STATUS BACKEND", lines[0])
	}

	// Separator line
	if !strings.Contains(lines[1], "----") {
		t.Errorf("separator line = %q, want dashes", lines[1])
	}

	// Data rows
	if !strings.Contains(lines[2], "myvm") || !strings.Contains(lines[2], "running") {
		t.Errorf("row 0 = %q, want myvm running", lines[2])
	}
	if !strings.Contains(lines[3], "dev") || !strings.Contains(lines[3], "stopped") {
		t.Errorf("row 1 = %q, want dev stopped", lines[3])
	}
}

func TestTable_Render_Empty(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable(&buf, "NAME", "STATUS")
	tbl.Render()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 { // header + separator, no data rows
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}
}

func TestTable_Render_NoHeaders(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable(&buf)
	tbl.Render()

	if buf.Len() > 0 {
		t.Errorf("table with no headers should produce no output, got %q", buf.String())
	}
}

func TestTable_AddRow_PadsMissing(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable(&buf, "A", "B", "C")
	tbl.AddRow("only-one")
	tbl.Render()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[2], "only-one") {
		t.Errorf("row = %q, want 'only-one'", lines[2])
	}
}

func TestTable_ColumnAlignment(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable(&buf, "NAME", "STATUS")
	tbl.AddRow("a", "running")
	tbl.AddRow("longname", "stopped")
	tbl.Render()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")

	// The NAME column should be padded to "longname" width (8)
	// So "a" should be followed by spaces
	if len(lines) < 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}

	// Both "running" and "stopped" should start at the same column
	runIdx := strings.Index(lines[2], "running")
	stopIdx := strings.Index(lines[3], "stopped")
	if runIdx != stopIdx {
		t.Errorf("columns not aligned: 'running' at %d, 'stopped' at %d", runIdx, stopIdx)
	}
}
