package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewProgressReporter_JSONMode(t *testing.T) {
	var buf bytes.Buffer
	pr := NewProgressReporter(&buf, true)

	pr.Start("creating")
	pr.Update("provisioning")
	pr.Done("done")
	pr.Fail("failed")

	if buf.Len() > 0 {
		t.Errorf("JSON mode progress should produce no output, got %q", buf.String())
	}
}

func TestNewProgressReporter_NonTTY(t *testing.T) {
	var buf bytes.Buffer
	pr := NewProgressReporter(&buf, false)

	pr.Start("creating VM")
	pr.Update("provisioning")
	pr.Done("VM ready")

	got := buf.String()
	if !strings.Contains(got, "creating VM...") {
		t.Errorf("expected 'creating VM...' in output, got %q", got)
	}
	if !strings.Contains(got, "provisioning...") {
		t.Errorf("expected 'provisioning...' in output, got %q", got)
	}
	if !strings.Contains(got, "VM ready") {
		t.Errorf("expected 'VM ready' in output, got %q", got)
	}
}

func TestLineProgress_Fail(t *testing.T) {
	var buf bytes.Buffer
	pr := &lineProgress{w: &buf}

	pr.Start("working")
	pr.Fail("operation failed")

	got := buf.String()
	if !strings.Contains(got, "operation failed") {
		t.Errorf("expected 'operation failed' in output, got %q", got)
	}
}

func TestIsTTY_Buffer(t *testing.T) {
	var buf bytes.Buffer
	if isTTY(&buf) {
		t.Error("bytes.Buffer should not be a TTY")
	}
}
