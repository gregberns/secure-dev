package session

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sd/internal/session/mocktmux"
)

// mockRunner adapts the mocktmux server to the CommandRunner interface.
type mockRunner struct {
	server *mocktmux.Server
	vmRunning bool
}

func (r *mockRunner) Exec(_ context.Context, vmName string, command []string) (ExecResult, error) {
	if !r.vmRunning {
		return ExecResult{Stderr: "vm not running", ExitCode: 1}, nil
	}

	// If the command starts with "tmux", route to the mock tmux server.
	if len(command) > 0 && command[0] == "tmux" {
		result := r.server.Exec(command[1:])
		return ExecResult{
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
			ExitCode: result.ExitCode,
		}, nil
	}

	// Handle "which tmux" check.
	if len(command) == 2 && command[0] == "which" && command[1] == "tmux" {
		if r.server.IsInstalled() {
			return ExecResult{Stdout: "/usr/bin/tmux", ExitCode: 0}, nil
		}
		return ExecResult{Stderr: "tmux not found", ExitCode: 1}, nil
	}

	// Default: successful execution.
	return ExecResult{Stdout: "ok", ExitCode: 0}, nil
}

// IsInstalled is a helper to check the mock server state.
func (r *mockRunner) IsInstalled() bool {
	return r.server.IsInstalled()
}

// newTestManager creates a Manager backed by a mock tmux server.
func newTestManager() (*Manager, *mocktmux.Server) {
	srv := mocktmux.NewServer()
	runner := &mockRunner{server: srv, vmRunning: true}
	return NewManager(runner), srv
}

// newTestManagerWithoutTmux creates a Manager where tmux is not installed.
func newTestManagerWithoutTmux() *Manager {
	srv := mocktmux.NewServerWithoutTmux()
	runner := &mockRunner{server: srv, vmRunning: true}
	return NewManager(runner)
}

// ============================================================
// REQ-007-008: tmux as default session manager
// ============================================================

func TestEnsureSession_CreatesNewSession(t *testing.T) {
	mgr, srv := newTestManager()

	err := mgr.EnsureSession(context.Background(), Options{VMName: "testvm"})
	require.NoError(t, err)

	assert.True(t, srv.SessionExists("sd-testvm"), "default session sd-testvm should be created")
}

func TestEnsureSession_AttachesToExistingSession(t *testing.T) {
	mgr, srv := newTestManager()

	// Create the session first.
	err := mgr.EnsureSession(context.Background(), Options{VMName: "testvm"})
	require.NoError(t, err)

	// Attach to it again.
	err = mgr.EnsureSession(context.Background(), Options{VMName: "testvm"})
	require.NoError(t, err)

	assert.True(t, srv.SessionExists("sd-testvm"))
	assert.Equal(t, 1, srv.WindowCount("sd-testvm"), "should not create extra windows on re-attach")
}

func TestEnsureSession_DefaultSessionName(t *testing.T) {
	mgr, srv := newTestManager()

	err := mgr.EnsureSession(context.Background(), Options{VMName: "myproject"})
	require.NoError(t, err)

	assert.True(t, srv.SessionExists("sd-myproject"))
}

func TestEnsureSession_SessionPersists(t *testing.T) {
	mgr, srv := newTestManager()

	err := mgr.EnsureSession(context.Background(), Options{VMName: "testvm"})
	require.NoError(t, err)

	// Session persists after the call.
	assert.True(t, srv.SessionExists("sd-testvm"))
}

// Property: Creating a session always results in exactly one session and one window.
func TestEnsureSession_CreatesOneSessionOneWindow_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{2,8}`).Draw(t, "vmName")
		mgr, srv := newTestManager()

		err := mgr.EnsureSession(context.Background(), Options{VMName: vmName})
		require.NoError(t, err)

		sessionName := "sd-" + vmName
		assert.True(t, srv.SessionExists(sessionName))
		assert.Equal(t, 1, srv.WindowCount(sessionName))
	})
}

// Property: Repeated EnsureSession calls are idempotent.
func TestEnsureSession_Idempotent_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{2,8}`).Draw(t, "vmName")
		n := rapid.IntRange(1, 5).Draw(t, "repetitions")
		mgr, srv := newTestManager()

		sessionName := "sd-" + vmName
		for i := 0; i < n; i++ {
			err := mgr.EnsureSession(context.Background(), Options{VMName: vmName})
			require.NoError(t, err)
		}

		assert.True(t, srv.SessionExists(sessionName))
		assert.Equal(t, 1, srv.WindowCount(sessionName),
			"idempotent calls should not create extra windows")
	})
}

// ============================================================
// REQ-007-009: Named tmux sessions
// ============================================================

func TestEnsureSession_NamedSession(t *testing.T) {
	mgr, srv := newTestManager()

	err := mgr.EnsureSession(context.Background(), Options{
		VMName:  "testvm",
		Session: "work",
	})
	require.NoError(t, err)

	assert.True(t, srv.SessionExists("work"))
	assert.False(t, srv.SessionExists("sd-testvm"), "default session should not exist when custom name is used")
}

func TestEnsureSession_InvalidSessionName(t *testing.T) {
	invalidNames := []string{
		"my session", "session!", "a.b", "has/slash",
		"has space", "name@work", "colon:name",
	}

	for _, name := range invalidNames {
		t.Run(name, func(t *testing.T) {
			err := mgr_ensure(t, Options{
				VMName:  "testvm",
				Session: name,
			})
			assert.ErrorIs(t, err, ErrInvalidSession)
		})
	}
}

func TestEnsureSession_ValidSessionNames(t *testing.T) {
	validNames := []string{"work", "session-1", "my_session", "A", "abc123", "sd-myvm"}

	for _, name := range validNames {
		t.Run(name, func(t *testing.T) {
			mgr, srv := newTestManager()

			err := mgr.EnsureSession(context.Background(), Options{
				VMName:  "testvm",
				Session: name,
			})
			require.NoError(t, err)
			assert.True(t, srv.SessionExists(name))
		})
	}
}

func TestOptions_ResolvedSession(t *testing.T) {
	tests := []struct {
		name     string
		opts     Options
		expected string
	}{
		{"default", Options{VMName: "testvm"}, "sd-testvm"},
		{"custom", Options{VMName: "testvm", Session: "work"}, "work"},
		{"empty_custom", Options{VMName: "testvm", Session: ""}, "sd-testvm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.opts.ResolvedSession())
		})
	}
}

// Property: Valid session names (alphanumeric, hyphen, underscore) always pass validation.
func TestValidateName_ValidChars_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`[a-zA-Z0-9_-]{1,20}`).Draw(t, "name")
		if name == "" {
			t.Skip("empty")
		}
		assert.NoError(t, validateName(name))
	})
}

// Property: Names containing spaces always fail validation.
func TestValidateName_SpacesFail_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		part1 := rapid.StringMatching(`[a-z]{2,5}`).Draw(t, "part1")
		part2 := rapid.StringMatching(`[a-z]{2,5}`).Draw(t, "part2")
		name := part1 + " " + part2
		assert.ErrorIs(t, validateName(name), ErrInvalidSession)
	})
}

// Property: Names containing special characters always fail validation.
func TestValidateName_SpecialCharsFail_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		prefix := rapid.StringMatching(`[a-z]{2,5}`).Draw(t, "prefix")
		special := rapid.SampledFrom([]string{
			"!", "@", "#", "$", "%", ".", "/", "\\", ":", "(", ")",
		}).Draw(t, "special")
		suffix := rapid.StringMatching(`[a-z]{2,5}`).Draw(t, "suffix")
		name := prefix + special + suffix
		assert.ErrorIs(t, validateName(name), ErrInvalidSession)
	})
}

// ============================================================
// REQ-007-010: New tmux window
// ============================================================

func TestEnsureSession_NewWindow(t *testing.T) {
	mgr, srv := newTestManager()

	// First create the session.
	err := mgr.EnsureSession(context.Background(), Options{VMName: "testvm"})
	require.NoError(t, err)
	assert.Equal(t, 1, srv.WindowCount("sd-testvm"))

	// Then create a new window.
	err = mgr.EnsureSession(context.Background(), Options{
		VMName:    "testvm",
		NewWindow: true,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, srv.WindowCount("sd-testvm"))
}

func TestEnsureSession_NewWindowCreatesSessionIfMissing(t *testing.T) {
	mgr, srv := newTestManager()

	// NewWindow on a non-existent session: EnsureSession creates it first,
	// then adds a window. The session has one window after creation.
	err := mgr.EnsureSession(context.Background(), Options{
		VMName:    "testvm",
		NewWindow: true,
	})
	require.NoError(t, err)

	// Session is created with one window by new-session -A.
	// Then new-window is NOT called because the session was just created
	// (new-session -A already gives us a new session with one window).
	assert.True(t, srv.SessionExists("sd-testvm"))
	// After EnsureSession with NewWindow on a new session, we should have
	// the initial window from new-session, PLUS the new-window.
	// But actually, the flow is: new-session -A creates+attaches, then new-window.
	assert.Equal(t, 2, srv.WindowCount("sd-testvm"))
}

func TestNewWindow_Standalone(t *testing.T) {
	mgr, srv := newTestManager()

	// Create session first.
	err := mgr.EnsureSession(context.Background(), Options{VMName: "testvm"})
	require.NoError(t, err)

	// Add a window via NewWindow.
	err = mgr.NewWindow(context.Background(), Options{VMName: "testvm"})
	require.NoError(t, err)
	assert.Equal(t, 2, srv.WindowCount("sd-testvm"))
}

// Property: Adding N windows always results in N+1 total windows.
func TestNewWindow_WindowCount_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 5).Draw(t, "extraWindows")
		mgr, srv := newTestManager()

		err := mgr.EnsureSession(context.Background(), Options{VMName: "testvm"})
		require.NoError(t, err)

		for i := 0; i < n; i++ {
			err = mgr.NewWindow(context.Background(), Options{VMName: "testvm"})
			require.NoError(t, err)
		}

		assert.Equal(t, 1+n, srv.WindowCount("sd-testvm"))
	})
}

// ============================================================
// REQ-007-011: tmux default configuration
// ============================================================

func TestDefaultTmuxConfig_ContainsRequiredSettings(t *testing.T) {
	config := DefaultTmuxConfig()

	requiredSettings := []string{
		"set -g prefix C-a",
		"unbind C-b",
		"set -g mouse on",
		"set -g history-limit 50000",
		"set -g default-terminal \"tmux-256color\"",
		"set -g status-left",
		"set -g status-right",
		"set -g status-style",
		"bind | split-window -h",
		"bind - split-window -v",
		"bind r source-file",
	}

	for _, setting := range requiredSettings {
		assert.Contains(t, config, setting, "tmux config must contain %q", setting)
	}
}

func TestDefaultTmuxConfig_StartsWithManagedHeader(t *testing.T) {
	config := DefaultTmuxConfig()
	assert.True(t, strings.HasPrefix(config, "# Managed by sd."),
		"tmux config must start with managed header")
}

// Property: Default config always contains all required settings.
func TestDefaultTmuxConfig_AlwaysComplete_Property(t *testing.T) {
	// This is a constant, so we test it once, but the property form
	// documents the invariant.
	config := DefaultTmuxConfig()
	assert.Contains(t, config, "mouse on")
	assert.Contains(t, config, "prefix C-a")
	assert.Contains(t, config, "history-limit")
}

// ============================================================
// REQ-007-012: Raw SSH without tmux
// ============================================================

func TestEnsureSession_NoTmux(t *testing.T) {
	mgr, srv := newTestManager()

	err := mgr.EnsureSession(context.Background(), Options{
		VMName: "testvm",
		NoTmux: true,
	})
	require.NoError(t, err)

	// No session should be created.
	assert.False(t, srv.SessionExists("sd-testvm"))
}

func TestEnsureSession_NoTmuxWithNewWindow_Error(t *testing.T) {
	mgr, _ := newTestManager()

	err := mgr.EnsureSession(context.Background(), Options{
		VMName:    "testvm",
		NoTmux:    true,
		NewWindow: true,
	})
	assert.ErrorIs(t, err, ErrMutuallyExclusive)
}

func TestNewWindow_NoTmux_Error(t *testing.T) {
	mgr, _ := newTestManager()

	err := mgr.NewWindow(context.Background(), Options{
		VMName: "testvm",
		NoTmux: true,
	})
	assert.ErrorIs(t, err, ErrMutuallyExclusive)
}

// ============================================================
// REQ-007-021: tmux not installed error
// ============================================================

func TestEnsureSession_TmuxNotInstalled(t *testing.T) {
	mgr := newTestManagerWithoutTmux()

	err := mgr.EnsureSession(context.Background(), Options{VMName: "testvm"})
	assert.ErrorIs(t, err, ErrTmuxNotInstalled)
	assert.Contains(t, err.Error(), "testvm")
	assert.Contains(t, err.Error(), "--no-tmux")
}

func TestHasSession_TmuxNotInstalled(t *testing.T) {
	mgr := newTestManagerWithoutTmux()

	_, err := mgr.HasSession(context.Background(), "testvm", "sd-testvm")
	assert.ErrorIs(t, err, ErrTmuxNotInstalled)
}

func TestListSessions_TmuxNotInstalled(t *testing.T) {
	mgr := newTestManagerWithoutTmux()

	_, err := mgr.ListSessions(context.Background(), "testvm")
	assert.ErrorIs(t, err, ErrTmuxNotInstalled)
}

// ============================================================
// HasSession, KillSession, ListSessions
// ============================================================

func TestHasSession_Exists(t *testing.T) {
	mgr, _ := newTestManager()

	err := mgr.EnsureSession(context.Background(), Options{VMName: "testvm"})
	require.NoError(t, err)

	exists, err := mgr.HasSession(context.Background(), "testvm", "sd-testvm")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestHasSession_NotExists(t *testing.T) {
	mgr, _ := newTestManager()

	exists, err := mgr.HasSession(context.Background(), "testvm", "nonexistent")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestKillSession(t *testing.T) {
	mgr, srv := newTestManager()

	err := mgr.EnsureSession(context.Background(), Options{VMName: "testvm"})
	require.NoError(t, err)
	assert.True(t, srv.SessionExists("sd-testvm"))

	err = mgr.KillSession(context.Background(), "testvm", "sd-testvm")
	require.NoError(t, err)
	assert.False(t, srv.SessionExists("sd-testvm"))
}

func TestKillSession_NotFound(t *testing.T) {
	mgr, _ := newTestManager()

	err := mgr.KillSession(context.Background(), "testvm", "nonexistent")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestListSessions_Empty(t *testing.T) {
	mgr, _ := newTestManager()

	sessions, err := mgr.ListSessions(context.Background(), "testvm")
	require.NoError(t, err)
	assert.Empty(t, sessions)
}

func TestListSessions_Multiple(t *testing.T) {
	mgr, _ := newTestManager()

	err := mgr.EnsureSession(context.Background(), Options{VMName: "vm1", Session: "work"})
	require.NoError(t, err)
	err = mgr.EnsureSession(context.Background(), Options{VMName: "vm1", Session: "personal"})
	require.NoError(t, err)

	sessions, err := mgr.ListSessions(context.Background(), "vm1")
	require.NoError(t, err)
	assert.Len(t, sessions, 2)
	assert.Contains(t, sessions, "work")
	assert.Contains(t, sessions, "personal")
}

// Property: ListSessions always includes all created sessions.
func TestListSessions_ReflectsCreatedSessions_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mgr, _ := newTestManager()

		names := rapid.SliceOfN(
			rapid.StringMatching(`s-[a-z]{2,5}`),
			1, 5,
		).Draw(t, "sessionNames")

		created := make(map[string]bool)
		for _, name := range names {
			err := mgr.EnsureSession(context.Background(), Options{
				VMName:  "testvm",
				Session: name,
			})
			require.NoError(t, err)
			created[name] = true
		}

		sessions, err := mgr.ListSessions(context.Background(), "testvm")
		require.NoError(t, err)

		for name := range created {
			assert.Contains(t, sessions, name)
		}
		assert.Equal(t, len(created), len(sessions))
	})
}

// ============================================================
// Options validation
// ============================================================

func TestOptions_Validate_EmptyVMName(t *testing.T) {
	err := Options{VMName: ""}.Validate()
	assert.ErrorIs(t, err, ErrInvalidSession)
}

func TestOptions_Validate_ValidDefaults(t *testing.T) {
	err := Options{VMName: "testvm"}.Validate()
	assert.NoError(t, err)
}

func TestOptions_Validate_MutualExclusion(t *testing.T) {
	err := Options{VMName: "testvm", NoTmux: true, NewWindow: true}.Validate()
	assert.ErrorIs(t, err, ErrMutuallyExclusive)
}

// ============================================================
// BuildTmuxArgs
// ============================================================

func TestBuildTmuxArgs_Default(t *testing.T) {
	args := BuildTmuxArgs("sd-testvm", false)
	assert.Equal(t, []string{"tmux", "new-session", "-A", "-s", "sd-testvm"}, args)
}

func TestBuildTmuxArgs_NewWindow(t *testing.T) {
	args := BuildTmuxArgs("sd-testvm", true)
	assert.Equal(t, []string{"tmux", "new-window", "-t", "sd-testvm"}, args)
}

// ============================================================
// Integration: Full session lifecycle with digital twin
// ============================================================

func TestIntegration_FullSessionLifecycle(t *testing.T) {
	mgr, srv := newTestManager()
	ctx := context.Background()

	// 1. Create a session.
	err := mgr.EnsureSession(ctx, Options{VMName: "myvm"})
	require.NoError(t, err)
	assert.True(t, srv.SessionExists("sd-myvm"))

	// 2. Verify session exists.
	exists, err := mgr.HasSession(ctx, "myvm", "sd-myvm")
	require.NoError(t, err)
	assert.True(t, exists)

	// 3. Add windows.
	err = mgr.NewWindow(ctx, Options{VMName: "myvm"})
	require.NoError(t, err)
	assert.Equal(t, 2, srv.WindowCount("sd-myvm"))

	// 4. List sessions.
	sessions, err := mgr.ListSessions(ctx, "myvm")
	require.NoError(t, err)
	assert.Contains(t, sessions, "sd-myvm")

	// 5. Create a named session too.
	err = mgr.EnsureSession(ctx, Options{VMName: "myvm", Session: "debug"})
	require.NoError(t, err)
	sessions, err = mgr.ListSessions(ctx, "myvm")
	require.NoError(t, err)
	assert.Len(t, sessions, 2)

	// 6. Kill default session.
	err = mgr.KillSession(ctx, "myvm", "sd-myvm")
	require.NoError(t, err)
	sessions, err = mgr.ListSessions(ctx, "myvm")
	require.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Contains(t, sessions, "debug")

	// 7. Kill remaining session.
	err = mgr.KillSession(ctx, "myvm", "debug")
	require.NoError(t, err)
	sessions, err = mgr.ListSessions(ctx, "myvm")
	require.NoError(t, err)
	assert.Empty(t, sessions)
}

// ============================================================
// Integration: Multiple VMs have independent sessions
// ============================================================

func TestIntegration_MultipleVMs(t *testing.T) {
	srv := mocktmux.NewServer()
	runner := &mockRunner{server: srv, vmRunning: true}
	mgr := NewManager(runner)
	ctx := context.Background()

	err := mgr.EnsureSession(ctx, Options{VMName: "vm1"})
	require.NoError(t, err)

	err = mgr.EnsureSession(ctx, Options{VMName: "vm2"})
	require.NoError(t, err)

	// Both sessions exist in the mock tmux server.
	assert.True(t, srv.SessionExists("sd-vm1"))
	assert.True(t, srv.SessionExists("sd-vm2"))

	// Kill vm1's session.
	err = mgr.KillSession(ctx, "vm1", "sd-vm1")
	require.NoError(t, err)

	// vm2's session is unaffected.
	assert.False(t, srv.SessionExists("sd-vm1"))
	assert.True(t, srv.SessionExists("sd-vm2"))
}

// ============================================================
// Integration: Concurrent session operations
// ============================================================

func TestIntegration_ConcurrentSessionCreation(t *testing.T) {
	srv := mocktmux.NewServer()
	runner := &mockRunner{server: srv, vmRunning: true}
	mgr := NewManager(runner)
	ctx := context.Background()

	// Create sessions concurrently.
	errCh := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			name := fmt.Sprintf("worker-%d", idx)
			errCh <- mgr.EnsureSession(ctx, Options{VMName: "testvm", Session: name})
		}(i)
	}

	for i := 0; i < 10; i++ {
		require.NoError(t, <-errCh)
	}

	// All 10 sessions should exist.
	sessions, err := mgr.ListSessions(ctx, "testvm")
	require.NoError(t, err)
	assert.Len(t, sessions, 10)
}

// ============================================================
// DefaultSessionName
// ============================================================

func TestDefaultSessionName(t *testing.T) {
	assert.Equal(t, "sd-myvm", DefaultSessionName("myvm"))
	assert.Equal(t, "sd-", DefaultSessionName(""))
	assert.Equal(t, "sd-test-123", DefaultSessionName("test-123"))
}

// Property: DefaultSessionName always starts with "sd-".
func TestDefaultSessionName_AlwaysHasPrefix_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`[a-z][a-z0-9-]{0,16}`).Draw(t, "vmName")
		session := DefaultSessionName(vmName)
		assert.True(t, strings.HasPrefix(session, "sd-"))
		assert.Equal(t, "sd-"+vmName, session)
	})
}

// helper to create a manager and call EnsureSession in one step.
func mgr_ensure(t *testing.T, opts Options) error {
	t.Helper()
	mgr, _ := newTestManager()
	return mgr.EnsureSession(context.Background(), opts)
}
