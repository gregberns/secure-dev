// Package session manages tmux sessions inside sd VMs.
// REQ-007-008: tmux as default session manager
// REQ-007-009: Named tmux sessions
// REQ-007-010: New tmux window
// REQ-007-011: tmux default configuration
// REQ-007-012: Raw SSH without tmux
package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors.
var (
	ErrSessionExists     = errors.New("session already exists")
	ErrSessionNotFound   = errors.New("session not found")
	ErrTmuxNotInstalled  = errors.New("tmux not installed in VM")
	ErrInvalidSession    = errors.New("invalid session configuration")
	ErrMutuallyExclusive = errors.New("mutually exclusive options")
)

// CommandRunner abstracts running commands on a remote VM.
// This is satisfied by backend.Backend's Exec method, allowing
// the session package to work with any backend implementation.
type CommandRunner interface {
	// Exec runs a command inside the named VM and returns the result.
	Exec(ctx context.Context, vmName string, command []string) (ExecResult, error)
}

// ExecResult holds the result of a remote command execution.
// This mirrors backend.ExecResult to avoid a direct dependency.
type ExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// Options configures session creation and attachment.
// REQ-007-008, REQ-007-009, REQ-007-010, REQ-007-012
type Options struct {
	// VMName is the name of the VM to connect to.
	VMName string

	// Session is the tmux session name. Default: "sd-<VMName>".
	Session string

	// NewWindow creates a new window in an existing tmux session.
	// Mutually exclusive with NoTmux.
	NewWindow bool

	// NoTmux skips tmux and uses a raw SSH session.
	// Mutually exclusive with NewWindow.
	NoTmux bool
}

// Validate checks session options for consistency.
func (o Options) Validate() error {
	if o.VMName == "" {
		return fmt.Errorf("%w: VM name is required", ErrInvalidSession)
	}

	if o.NoTmux && o.NewWindow {
		return fmt.Errorf("%w: --no-tmux and --new-window are mutually exclusive", ErrMutuallyExclusive)
	}

	if o.NoTmux {
		return nil
	}

	if o.Session == "" {
		o.Session = DefaultSessionName(o.VMName)
	}

	return validateName(o.Session)
}

// ResolvedSession returns the effective session name, applying the default
// if none is specified.
func (o Options) ResolvedSession() string {
	if o.Session != "" {
		return o.Session
	}
	return DefaultSessionName(o.VMName)
}

// DefaultSessionName returns the default tmux session name for a VM.
// REQ-007-008: Session name is deterministic: sd-<vm-name>.
func DefaultSessionName(vmName string) string {
	return "sd-" + vmName
}

// Manager manages tmux sessions inside VMs via a CommandRunner.
type Manager struct {
	runner CommandRunner
}

// NewManager creates a session Manager that uses the given CommandRunner
// to execute tmux commands inside VMs.
func NewManager(runner CommandRunner) *Manager {
	return &Manager{runner: runner}
}

// EnsureSession creates a tmux session if one doesn't exist, or attaches
// to an existing one. This implements the `tmux new-session -A` pattern.
// REQ-007-008: If a tmux session named sd-<vm-name> exists, attach to it.
// If no such session exists, create a new one and attach.
func (m *Manager) EnsureSession(ctx context.Context, opts Options) error {
	if err := opts.Validate(); err != nil {
		return err
	}

	if opts.NoTmux {
		return nil
	}

	sessionName := opts.ResolvedSession()

	// Check if tmux is available in the VM.
	if err := m.checkTmuxInstalled(ctx, opts.VMName); err != nil {
		return err
	}

	// `tmux new-session -A -s <name>` creates the session if it doesn't
	// exist, or attaches if it does. This is the standard idempotent pattern.
	cmd := []string{"tmux", "new-session", "-A", "-s", sessionName}
	result, err := m.runner.Exec(ctx, opts.VMName, cmd)
	if err != nil {
		return fmt.Errorf("tmux new-session: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("tmux new-session failed: %s", result.Stderr)
	}

	// If NewWindow is requested, create a new window in the session.
	if opts.NewWindow {
		return m.newWindow(ctx, opts.VMName, sessionName)
	}

	return nil
}

// NewWindow creates a new window in an existing tmux session.
// REQ-007-010: Creates a new window that becomes active upon attachment.
func (m *Manager) NewWindow(ctx context.Context, opts Options) error {
	if err := opts.Validate(); err != nil {
		return err
	}

	if opts.NoTmux {
		return fmt.Errorf("%w: cannot create window with --no-tmux", ErrMutuallyExclusive)
	}

	sessionName := opts.ResolvedSession()

	if err := m.checkTmuxInstalled(ctx, opts.VMName); err != nil {
		return err
	}

	return m.newWindow(ctx, opts.VMName, sessionName)
}

// HasSession checks whether a tmux session exists in the VM.
func (m *Manager) HasSession(ctx context.Context, vmName, sessionName string) (bool, error) {
	if err := m.checkTmuxInstalled(ctx, vmName); err != nil {
		return false, err
	}

	result, err := m.runner.Exec(ctx, vmName, []string{"tmux", "has-session", "-t", sessionName})
	if err != nil {
		return false, err
	}
	return result.ExitCode == 0, nil
}

// KillSession kills a tmux session in the VM.
func (m *Manager) KillSession(ctx context.Context, vmName, sessionName string) error {
	result, err := m.runner.Exec(ctx, vmName, []string{"tmux", "kill-session", "-t", sessionName})
	if err != nil {
		return fmt.Errorf("kill session %s: %w", sessionName, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("%w: session %s not found", ErrSessionNotFound, sessionName)
	}
	return nil
}

// ListSessions returns the names of all tmux sessions in the VM.
func (m *Manager) ListSessions(ctx context.Context, vmName string) ([]string, error) {
	if err := m.checkTmuxInstalled(ctx, vmName); err != nil {
		return nil, err
	}

	result, err := m.runner.Exec(ctx, vmName, []string{"tmux", "list-sessions"})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		// No sessions is not an error, return empty list.
		if strings.Contains(result.Stderr, "no tmux sessions") ||
			strings.Contains(result.Stderr, "no server running") {
			return nil, nil
		}
		return nil, fmt.Errorf("list sessions: %s", result.Stderr)
	}

	var sessions []string
	for _, line := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
		if line == "" {
			continue
		}
		// tmux list-sessions output format: "session-name: N windows (created ...)"
		name := strings.SplitN(line, ":", 2)[0]
		sessions = append(sessions, name)
	}
	return sessions, nil
}

// checkTmuxInstalled verifies tmux is available in the VM.
// REQ-007-021: If tmux is not installed, return actionable error.
func (m *Manager) checkTmuxInstalled(ctx context.Context, vmName string) error {
	result, err := m.runner.Exec(ctx, vmName, []string{"which", "tmux"})
	if err != nil {
		return fmt.Errorf("%w: vm %q", ErrTmuxNotInstalled, vmName)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("%w: vm %q (install with: sd provision %s or use --no-tmux)", ErrTmuxNotInstalled, vmName, vmName)
	}
	return nil
}

// newWindow creates a new window in an existing tmux session.
func (m *Manager) newWindow(ctx context.Context, vmName, sessionName string) error {
	result, err := m.runner.Exec(ctx, vmName, []string{"tmux", "new-window", "-t", sessionName})
	if err != nil {
		return fmt.Errorf("tmux new-window: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("tmux new-window failed: %s", result.Stderr)
	}
	return nil
}

// --- REQ-007-011: tmux Default Configuration ---

const defaultTmuxConfig = `# Managed by sd. User modifications are preserved.
set -g prefix C-a
unbind C-b
bind C-a send-prefix

set -g mouse on
set -g history-limit 50000
set -g default-terminal "tmux-256color"

# Status bar
set -g status-left "[#S] "
set -g status-right " %H:%M "
set -g status-style "bg=colour235,fg=colour248"

# Better split keybindings
bind | split-window -h -c "#{pane_current_path}"
bind - split-window -v -c "#{pane_current_path}"

# Reload config
bind r source-file ~/.tmux.conf \; display "Config reloaded"
`

// DefaultTmuxConfig returns the default tmux configuration content
// that is provisioned inside each VM at ~/.tmux.conf.
// REQ-007-011: Mouse mode enabled, status bar with VM name, C-a prefix.
func DefaultTmuxConfig() string {
	return defaultTmuxConfig
}

// --- Session name validation ---

// validateName checks that a session name is valid for tmux.
// tmux session names must not contain periods or colons.
// REQ-007-009: alphanumeric, hyphens, and underscores only.
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: session name cannot be empty", ErrInvalidSession)
	}
	for _, ch := range name {
		if !isValidSessionChar(ch) {
			return fmt.Errorf("%w: session name %q contains invalid character %q; use only alphanumeric characters, hyphens, and underscores",
				ErrInvalidSession, name, string(ch))
		}
	}
	return nil
}

func isValidSessionChar(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') || ch == '-' || ch == '_'
}

// BuildTmuxArgs constructs the tmux command arguments for creating/attaching
// a session. This is useful for building the SSH command line.
func BuildTmuxArgs(sessionName string, newWindow bool) []string {
	args := []string{"tmux", "new-session", "-A", "-s", sessionName}
	if newWindow {
		args = []string{"tmux", "new-window", "-t", sessionName}
	}
	return args
}
