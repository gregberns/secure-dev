package ui

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// ProgressReporter tracks and displays progress for long-running operations.
type ProgressReporter interface {
	// Start begins displaying progress with the given message.
	Start(msg string)

	// Update changes the progress message.
	Update(msg string)

	// Done stops the progress indicator and prints a completion message.
	Done(msg string)

	// Fail stops the progress indicator and prints a failure message.
	Fail(msg string)
}

// NewProgressReporter creates a ProgressReporter appropriate for the context.
// If jsonMode is true, returns a no-op reporter (JSON mode suppresses progress).
// If w is a TTY, returns a spinner. Otherwise returns a line-based reporter.
func NewProgressReporter(w io.Writer, jsonMode bool) ProgressReporter {
	if jsonMode {
		return &noopProgress{}
	}
	if isTTY(w) {
		return &spinnerProgress{w: w}
	}
	return &lineProgress{w: w}
}

// isTTY checks if the writer is a terminal.
func isTTY(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		stat, err := f.Stat()
		if err != nil {
			return false
		}
		return (stat.Mode() & os.ModeCharDevice) != 0
	}
	return false
}

// noopProgress discards all progress output (used in JSON mode).
type noopProgress struct{}

func (p *noopProgress) Start(msg string) {}
func (p *noopProgress) Update(msg string) {}
func (p *noopProgress) Done(msg string) {}
func (p *noopProgress) Fail(msg string) {}

// lineProgress writes progress as plain lines (for non-TTY stderr).
type lineProgress struct {
	w io.Writer
}

func (p *lineProgress) Start(msg string) {
	fmt.Fprintln(p.w, msg+"...")
}

func (p *lineProgress) Update(msg string) {
	fmt.Fprintln(p.w, msg+"...")
}

func (p *lineProgress) Done(msg string) {
	fmt.Fprintln(p.w, msg)
}

func (p *lineProgress) Fail(msg string) {
	fmt.Fprintln(p.w, msg)
}

// spinnerProgress shows an animated spinner on a TTY.
type spinnerProgress struct {
	w      io.Writer
	msg    string
	mu     sync.Mutex
	done   chan struct{}
	active bool
}

var spinnerFrames = []string{"|", "/", "-", "\\"}

func (p *spinnerProgress) Start(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.active {
		return
	}

	p.msg = msg
	p.done = make(chan struct{})
	p.active = true

	go p.spin()
}

func (p *spinnerProgress) spin() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	i := 0
	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			p.mu.Lock()
			frame := spinnerFrames[i%len(spinnerFrames)]
			msg := p.msg
			p.mu.Unlock()

			fmt.Fprintf(p.w, "\r%s %s", frame, msg)
			i++
		}
	}
}

func (p *spinnerProgress) Update(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.msg = msg
}

func (p *spinnerProgress) stop() {
	if !p.active {
		return
	}
	close(p.done)
	p.active = false
	// Clear the spinner line
	fmt.Fprintf(p.w, "\r\033[K")
}

func (p *spinnerProgress) Done(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stop()
	fmt.Fprintln(p.w, msg)
}

func (p *spinnerProgress) Fail(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stop()
	fmt.Fprintln(p.w, msg)
}
