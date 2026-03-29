// Package mocktmux provides a digital twin of tmux for testing.
// This simulates tmux session behavior without requiring tmux installation
// or a running VM, enabling full integration testing of the session package.
package mocktmux

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// SessionState represents a tmux session.
type SessionState struct {
	Name    string
	Windows []WindowState
}

// WindowState represents a tmux window within a session.
type WindowState struct {
	Name    string
	Active  bool
	Command string
}

// Server simulates a tmux server managing sessions.
type Server struct {
	mu       sync.RWMutex
	sessions map[string]*SessionState
	installed bool
}

// NewServer creates a new mock tmux server with tmux "installed".
func NewServer() *Server {
	return &Server{
		sessions:  make(map[string]*SessionState),
		installed: true,
	}
}

// NewServerWithoutTmux creates a server where tmux is not installed.
// This simulates the case where tmux hasn't been provisioned in the VM.
func NewServerWithoutTmux() *Server {
	return &Server{
		sessions:  make(map[string]*SessionState),
		installed: false,
	}
}

// ExecResult holds the result of a mock tmux command execution.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Exec simulates running a tmux command. The args should be tmux subcommand
// and its flags (i.e., what would follow `tmux` on the command line).
func (s *Server) Exec(args []string) ExecResult {
	if !s.installed {
		return ExecResult{
			Stderr:   "tmux: command not found",
			ExitCode: 127,
		}
	}

	if len(args) == 0 {
		return ExecResult{
			Stderr:   "usage: tmux [command]",
			ExitCode: 1,
		}
	}

	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "new-session":
		return s.newSession(cmdArgs...)
	case "has-session":
		return s.hasSession(cmdArgs...)
	case "kill-session":
		return s.killSession(cmdArgs...)
	case "list-sessions":
		return s.listSessions(cmdArgs...)
	case "new-window":
		return s.newWindow(cmdArgs...)
	case "list-windows":
		return s.listWindows(cmdArgs...)
	case "send-keys":
		return s.sendKeys(cmdArgs...)
	default:
		return ExecResult{
			Stderr:   fmt.Sprintf("unknown tmux command: %s", cmd),
			ExitCode: 1,
		}
	}
}

// newSession simulates `tmux new-session`.
// Supports: -A (attach if exists), -s <name>, -x <cols>, -y <rows>, -d (detached).
func (s *Server) newSession(args ...string) ExecResult {
	attachIfExists := false
	detached := false
	sessionName := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-A":
			attachIfExists = true
		case "-d":
			detached = true
		case "-s":
			i++
			if i >= len(args) {
				return ExecResult{Stderr: "missing session name after -s", ExitCode: 1}
			}
			sessionName = args[i]
		case "-x", "-y":
			i++ // skip value
		}
	}

	if sessionName == "" {
		return ExecResult{Stderr: "no session name specified", ExitCode: 1}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.sessions[sessionName]; ok {
		if !attachIfExists {
			return ExecResult{
				Stderr:   fmt.Sprintf("duplicate session: %s", sessionName),
				ExitCode: 1,
			}
		}
		// Attach to existing: make first window active.
		for j := range existing.Windows {
			existing.Windows[j].Active = j == 0
		}
		return ExecResult{Stdout: fmt.Sprintf("attached to session %s", sessionName), ExitCode: 0}
	}

	// Create new session.
	sess := &SessionState{
		Name: sessionName,
		Windows: []WindowState{
			{Name: "0", Active: true},
		},
	}
	s.sessions[sessionName] = sess

	if detached {
		return ExecResult{Stdout: fmt.Sprintf("created detached session %s", sessionName), ExitCode: 0}
	}
	return ExecResult{Stdout: fmt.Sprintf("created session %s", sessionName), ExitCode: 0}
}

// hasSession simulates `tmux has-session -t <name>`.
func (s *Server) hasSession(args ...string) ExecResult {
	target := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "-t" {
			i++
			if i >= len(args) {
				return ExecResult{Stderr: "missing target after -t", ExitCode: 1}
			}
			target = args[i]
		}
	}

	if target == "" {
		return ExecResult{Stderr: "no target specified", ExitCode: 1}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.sessions[target]; ok {
		return ExecResult{ExitCode: 0}
	}
	return ExecResult{
		Stderr:   fmt.Sprintf("no session found: %s", target),
		ExitCode: 1,
	}
}

// killSession simulates `tmux kill-session -t <name>`.
func (s *Server) killSession(args ...string) ExecResult {
	target := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "-t" {
			i++
			if i >= len(args) {
				return ExecResult{Stderr: "missing target after -t", ExitCode: 1}
			}
			target = args[i]
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.sessions[target]; !ok {
		return ExecResult{
			Stderr:   fmt.Sprintf("no session found: %s", target),
			ExitCode: 1,
		}
	}
	delete(s.sessions, target)
	return ExecResult{ExitCode: 0}
}

// listSessions simulates `tmux list-sessions`.
func (s *Server) listSessions(args ...string) ExecResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.sessions) == 0 {
		return ExecResult{
			Stderr:   "no tmux sessions",
			ExitCode: 1,
		}
	}

	var lines []string
	for _, sess := range s.sessions {
		lines = append(lines, fmt.Sprintf("%s: %d windows", sess.Name, len(sess.Windows)))
	}
	return ExecResult{Stdout: strings.Join(lines, "\n"), ExitCode: 0}
}

// newWindow simulates `tmux new-window -t <session>`.
func (s *Server) newWindow(args ...string) ExecResult {
	target := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-t":
			i++
			if i >= len(args) {
				return ExecResult{Stderr: "missing target after -t", ExitCode: 1}
			}
			target = args[i]
		case "-n":
			i++ // skip window name
		}
	}

	if target == "" {
		return ExecResult{Stderr: "no target session specified", ExitCode: 1}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[target]
	if !ok {
		return ExecResult{
			Stderr:   fmt.Sprintf("no session found: %s", target),
			ExitCode: 1,
		}
	}

	// Deactivate all existing windows.
	for j := range sess.Windows {
		sess.Windows[j].Active = false
	}

	// Add new active window.
	winName := fmt.Sprintf("%d", len(sess.Windows))
	sess.Windows = append(sess.Windows, WindowState{Name: winName, Active: true})

	return ExecResult{Stdout: fmt.Sprintf("created window %s in session %s", winName, target), ExitCode: 0}
}

// listWindows simulates `tmux list-windows -t <session>`.
func (s *Server) listWindows(args ...string) ExecResult {
	target := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "-t" {
			i++
			if i >= len(args) {
				return ExecResult{Stderr: "missing target after -t", ExitCode: 1}
			}
			target = args[i]
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[target]
	if !ok {
		return ExecResult{
			Stderr:   fmt.Sprintf("no session found: %s", target),
			ExitCode: 1,
		}
	}

	var lines []string
	for _, w := range sess.Windows {
		active := ""
		if w.Active {
			active = "*"
		}
		lines = append(lines, fmt.Sprintf("%s%s", w.Name, active))
	}
	return ExecResult{Stdout: strings.Join(lines, "\n"), ExitCode: 0}
}

// sendKeys simulates `tmux send-keys -t <target> <keys>`.
func (s *Server) sendKeys(args ...string) ExecResult {
	return ExecResult{ExitCode: 0}
}

// GetSessions returns a snapshot of all sessions (for test assertions).
func (s *Server) GetSessions() map[string]*SessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*SessionState, len(s.sessions))
	for k, v := range s.sessions {
		cp := *v
		cp.Windows = make([]WindowState, len(v.Windows))
		copy(cp.Windows, v.Windows)
		result[k] = &cp
	}
	return result
}

// SessionExists checks if a session exists (for test assertions).
func (s *Server) SessionExists(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.sessions[name]
	return ok
}

// WindowCount returns the number of windows in a session (for test assertions).
func (s *Server) WindowCount(sessionName string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[sessionName]
	if !ok {
		return 0
	}
	return len(sess.Windows)
}

// IsInstalled returns whether tmux is available on the mock server.
func (s *Server) IsInstalled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.installed
}

// Reset clears all session state.
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = make(map[string]*SessionState)
}

// Env is the environment variable name used to signal that tests
// should use the mock tmux server instead of the real tmux binary.
const EnvMockTmux = "SD_MOCK_TMUX"

// IsMocked checks if mock tmux is enabled via environment variable.
func IsMocked() bool {
	return os.Getenv(EnvMockTmux) != ""
}
