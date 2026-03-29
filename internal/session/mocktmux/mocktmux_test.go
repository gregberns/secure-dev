// Package mocktmux tests verify the digital twin accurately models
// real tmux behavior. These tests are critical because all session
// package integration tests depend on this mock's correctness.
package mocktmux

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// --- Command Dispatch ---

func TestExec_EmptyArgs(t *testing.T) {
	srv := NewServer()
	result := srv.Exec(nil)
	assert.Equal(t, 1, result.ExitCode)
	assert.Contains(t, result.Stderr, "usage")

	result = srv.Exec([]string{})
	assert.Equal(t, 1, result.ExitCode)
}

func TestExec_UnknownCommand(t *testing.T) {
	srv := NewServer()
	result := srv.Exec([]string{"bogus-command"})
	assert.Equal(t, 1, result.ExitCode)
	assert.Contains(t, result.Stderr, "unknown tmux command")
}

func TestExec_NotInstalled(t *testing.T) {
	srv := NewServerWithoutTmux()
	result := srv.Exec([]string{"new-session", "-s", "x"})
	assert.Equal(t, 127, result.ExitCode)
	assert.Contains(t, result.Stderr, "command not found")
}

func TestExec_NotInstalled_AnyCommand(t *testing.T) {
	srv := NewServerWithoutTmux()
	for _, args := range [][]string{
		{"new-session", "-s", "x"},
		{"has-session", "-t", "x"},
		{"kill-session", "-t", "x"},
		{"list-sessions"},
		{"new-window", "-t", "x"},
		{"list-windows", "-t", "x"},
		{"send-keys", "-t", "x", "hello"},
	} {
		result := srv.Exec(args)
		assert.Equal(t, 127, result.ExitCode, "expected exit 127 for args %v", args)
	}
}

// --- new-session ---

func TestNewSession_Basic(t *testing.T) {
	srv := NewServer()

	result := srv.Exec([]string{"new-session", "-s", "test-session"})
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "test-session")
	assert.True(t, srv.SessionExists("test-session"))
}

func TestNewSession_Detached(t *testing.T) {
	srv := NewServer()

	result := srv.Exec([]string{"new-session", "-d", "-s", "detached"})
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "detached")
	assert.True(t, srv.SessionExists("detached"))
}

func TestNewSession_AttachToExisting(t *testing.T) {
	srv := NewServer()

	// Create session
	result := srv.Exec([]string{"new-session", "-s", "existing"})
	require.Equal(t, 0, result.ExitCode)

	// Attach with -A flag
	result = srv.Exec([]string{"new-session", "-A", "-s", "existing"})
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "attached")
	assert.Equal(t, 1, srv.WindowCount("existing"))
}

func TestNewSession_DuplicateWithoutA(t *testing.T) {
	srv := NewServer()

	result := srv.Exec([]string{"new-session", "-s", "dup"})
	require.Equal(t, 0, result.ExitCode)

	result = srv.Exec([]string{"new-session", "-s", "dup"})
	assert.Equal(t, 1, result.ExitCode)
	assert.Contains(t, result.Stderr, "duplicate session")
}

func TestNewSession_NoName(t *testing.T) {
	srv := NewServer()

	result := srv.Exec([]string{"new-session"})
	assert.Equal(t, 1, result.ExitCode)
	assert.Contains(t, result.Stderr, "no session name")
}

func TestNewSession_MissingFlagValue(t *testing.T) {
	srv := NewServer()

	result := srv.Exec([]string{"new-session", "-s"})
	assert.Equal(t, 1, result.ExitCode)
}

func TestNewSession_CreatedWithOneWindow(t *testing.T) {
	srv := NewServer()

	result := srv.Exec([]string{"new-session", "-s", "win-test"})
	require.Equal(t, 0, result.ExitCode)
	assert.Equal(t, 1, srv.WindowCount("win-test"))
}

func TestNewSession_IgnoresDimensionFlags(t *testing.T) {
	srv := NewServer()

	result := srv.Exec([]string{"new-session", "-s", "dim", "-x", "200", "-y", "50"})
	assert.Equal(t, 0, result.ExitCode)
	assert.True(t, srv.SessionExists("dim"))
}

// --- has-session ---

func TestHasSession_Exists(t *testing.T) {
	srv := NewServer()
	srv.Exec([]string{"new-session", "-s", "exists"})

	result := srv.Exec([]string{"has-session", "-t", "exists"})
	assert.Equal(t, 0, result.ExitCode)
}

func TestHasSession_NotExists(t *testing.T) {
	srv := NewServer()

	result := srv.Exec([]string{"has-session", "-t", "nope"})
	assert.Equal(t, 1, result.ExitCode)
	assert.Contains(t, result.Stderr, "no session found")
}

func TestHasSession_NoTarget(t *testing.T) {
	srv := NewServer()

	result := srv.Exec([]string{"has-session"})
	assert.Equal(t, 1, result.ExitCode)
	assert.Contains(t, result.Stderr, "no target")
}

func TestHasSession_MissingTargetValue(t *testing.T) {
	srv := NewServer()

	result := srv.Exec([]string{"has-session", "-t"})
	assert.Equal(t, 1, result.ExitCode)
}

// --- kill-session ---

func TestKillSession_Basic(t *testing.T) {
	srv := NewServer()
	srv.Exec([]string{"new-session", "-s", "tokill"})

	result := srv.Exec([]string{"kill-session", "-t", "tokill"})
	assert.Equal(t, 0, result.ExitCode)
	assert.False(t, srv.SessionExists("tokill"))
}

func TestKillSession_NotFound(t *testing.T) {
	srv := NewServer()

	result := srv.Exec([]string{"kill-session", "-t", "ghost"})
	assert.Equal(t, 1, result.ExitCode)
	assert.Contains(t, result.Stderr, "no session found")
}

func TestKillSession_MissingTargetValue(t *testing.T) {
	srv := NewServer()

	result := srv.Exec([]string{"kill-session", "-t"})
	assert.Equal(t, 1, result.ExitCode)
}

// --- list-sessions ---

func TestListSessions_Empty(t *testing.T) {
	srv := NewServer()

	result := srv.Exec([]string{"list-sessions"})
	assert.Equal(t, 1, result.ExitCode)
	assert.Contains(t, result.Stderr, "no tmux sessions")
}

func TestListSessions_Multiple(t *testing.T) {
	srv := NewServer()
	srv.Exec([]string{"new-session", "-s", "alpha"})
	srv.Exec([]string{"new-session", "-s", "beta"})

	result := srv.Exec([]string{"list-sessions"})
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "alpha")
	assert.Contains(t, result.Stdout, "beta")
}

func TestListSessions_Format(t *testing.T) {
	srv := NewServer()
	srv.Exec([]string{"new-session", "-s", "fmt-test"})

	result := srv.Exec([]string{"list-sessions"})
	require.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "fmt-test:")
	assert.Contains(t, result.Stdout, "1 windows")
}

// --- new-window ---

func TestNewWindow_Basic(t *testing.T) {
	srv := NewServer()
	srv.Exec([]string{"new-session", "-s", "win"})

	result := srv.Exec([]string{"new-window", "-t", "win"})
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, 2, srv.WindowCount("win"))
}

func TestNewWindow_SessionNotFound(t *testing.T) {
	srv := NewServer()

	result := srv.Exec([]string{"new-window", "-t", "missing"})
	assert.Equal(t, 1, result.ExitCode)
	assert.Contains(t, result.Stderr, "no session found")
}

func TestNewWindow_NoTarget(t *testing.T) {
	srv := NewServer()

	result := srv.Exec([]string{"new-window"})
	assert.Equal(t, 1, result.ExitCode)
	assert.Contains(t, result.Stderr, "no target session")
}

func TestNewWindow_MissingTargetValue(t *testing.T) {
	srv := NewServer()

	result := srv.Exec([]string{"new-window", "-t"})
	assert.Equal(t, 1, result.ExitCode)
}

func TestNewWindow_WindowNumbering(t *testing.T) {
	srv := NewServer()
	srv.Exec([]string{"new-session", "-s", "num"})

	srv.Exec([]string{"new-window", "-t", "num"})
	srv.Exec([]string{"new-window", "-t", "num"})

	sessions := srv.GetSessions()
	sess := sessions["num"]
	require.Len(t, sess.Windows, 3)
	assert.Equal(t, "0", sess.Windows[0].Name)
	assert.Equal(t, "1", sess.Windows[1].Name)
	assert.Equal(t, "2", sess.Windows[2].Name)
}

func TestNewWindow_NewestWindowActive(t *testing.T) {
	srv := NewServer()
	srv.Exec([]string{"new-session", "-s", "active"})

	srv.Exec([]string{"new-window", "-t", "active"})

	sessions := srv.GetSessions()
	sess := sessions["active"]
	require.Len(t, sess.Windows, 2)
	assert.False(t, sess.Windows[0].Active)
	assert.True(t, sess.Windows[1].Active)
}

func TestNewWindow_WithNamedFlag(t *testing.T) {
	srv := NewServer()
	srv.Exec([]string{"new-session", "-s", "named"})

	// -n flag is accepted but name is overridden by number
	result := srv.Exec([]string{"new-window", "-t", "named", "-n", "mywindow"})
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, 2, srv.WindowCount("named"))
}

// --- list-windows ---

func TestListWindows_Basic(t *testing.T) {
	srv := NewServer()
	srv.Exec([]string{"new-session", "-s", "lw"})

	result := srv.Exec([]string{"list-windows", "-t", "lw"})
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "0*")
}

func TestListWindows_SessionNotFound(t *testing.T) {
	srv := NewServer()

	result := srv.Exec([]string{"list-windows", "-t", "missing"})
	assert.Equal(t, 1, result.ExitCode)
}

func TestListWindows_MissingTargetValue(t *testing.T) {
	srv := NewServer()

	result := srv.Exec([]string{"list-windows", "-t"})
	assert.Equal(t, 1, result.ExitCode)
}

func TestListWindows_ActiveMarker(t *testing.T) {
	srv := NewServer()
	srv.Exec([]string{"new-session", "-s", "mark"})
	srv.Exec([]string{"new-window", "-t", "mark"})

	result := srv.Exec([]string{"list-windows", "-t", "mark"})
	require.Equal(t, 0, result.ExitCode)

	lines := strings.Split(result.Stdout, "\n")
	assert.Contains(t, lines[0], "0")
	assert.NotContains(t, lines[0], "*")
	assert.Contains(t, lines[1], "1*")
}

// --- send-keys ---

func TestSendKeys_AlwaysSucceeds(t *testing.T) {
	srv := NewServer()
	srv.Exec([]string{"new-session", "-s", "keys"})

	result := srv.Exec([]string{"send-keys", "-t", "keys", "echo hello"})
	assert.Equal(t, 0, result.ExitCode)
}

// --- IsInstalled ---

func TestIsInstalled_True(t *testing.T) {
	srv := NewServer()
	assert.True(t, srv.IsInstalled())
}

func TestIsInstalled_False(t *testing.T) {
	srv := NewServerWithoutTmux()
	assert.False(t, srv.IsInstalled())
}

// --- Reset ---

func TestReset_ClearsSessions(t *testing.T) {
	srv := NewServer()
	srv.Exec([]string{"new-session", "-s", "a"})
	srv.Exec([]string{"new-session", "-s", "b"})

	srv.Reset()
	assert.Empty(t, srv.GetSessions())
}

func TestReset_AllowsRecreation(t *testing.T) {
	srv := NewServer()
	srv.Exec([]string{"new-session", "-s", "reuse"})
	srv.Reset()

	result := srv.Exec([]string{"new-session", "-s", "reuse"})
	assert.Equal(t, 0, result.ExitCode)
	assert.True(t, srv.SessionExists("reuse"))
}

// --- GetSessions returns a copy ---

func TestGetSessions_ReturnsCopy(t *testing.T) {
	srv := NewServer()
	srv.Exec([]string{"new-session", "-s", "copy-test"})

	sessions := srv.GetSessions()
	sessions["copy-test"].Name = "tampered"
	sessions["bogus"] = &SessionState{Name: "bogus"}

	// Original should be unchanged
	original := srv.GetSessions()
	assert.Equal(t, "copy-test", original["copy-test"].Name)
	assert.NotContains(t, original, "bogus")
}

// --- WindowCount returns 0 for missing sessions ---

func TestWindowCount_MissingSession(t *testing.T) {
	srv := NewServer()
	assert.Equal(t, 0, srv.WindowCount("nonexistent"))
}

// --- Concurrency ---

func TestConcurrency_ParallelSessionCreation(t *testing.T) {
	srv := NewServer()
	const n = 50
	var wg sync.WaitGroup

	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := string(rune('a' + idx%26)) + "-concurrent"
			srv.Exec([]string{"new-session", "-s", name})
		}(i)
	}
	wg.Wait()

	sessions := srv.GetSessions()
	assert.NotEmpty(t, sessions)
}

func TestConcurrency_ParallelNewWindow(t *testing.T) {
	srv := NewServer()
	srv.Exec([]string{"new-session", "-s", "conc-win"})

	const n = 20
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.Exec([]string{"new-window", "-t", "conc-win"})
		}()
	}
	wg.Wait()

	assert.Equal(t, 1+n, srv.WindowCount("conc-win"))
}

// --- Full Lifecycle Integration ---

func TestIntegration_FullSessionLifecycle(t *testing.T) {
	srv := NewServer()

	// 1. Create session
	result := srv.Exec([]string{"new-session", "-s", "lifecycle"})
	assert.Equal(t, 0, result.ExitCode)
	assert.True(t, srv.SessionExists("lifecycle"))
	assert.Equal(t, 1, srv.WindowCount("lifecycle"))

	// 2. Check session exists
	result = srv.Exec([]string{"has-session", "-t", "lifecycle"})
	assert.Equal(t, 0, result.ExitCode)

	// 3. Add windows
	srv.Exec([]string{"new-window", "-t", "lifecycle"})
	srv.Exec([]string{"new-window", "-t", "lifecycle"})
	assert.Equal(t, 3, srv.WindowCount("lifecycle"))

	// 4. List sessions
	result = srv.Exec([]string{"list-sessions"})
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "lifecycle")
	assert.Contains(t, result.Stdout, "3 windows")

	// 5. List windows
	result = srv.Exec([]string{"list-windows", "-t", "lifecycle"})
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, 3, len(strings.Split(result.Stdout, "\n")))

	// 6. Kill session
	result = srv.Exec([]string{"kill-session", "-t", "lifecycle"})
	assert.Equal(t, 0, result.ExitCode)
	assert.False(t, srv.SessionExists("lifecycle"))

	// 7. Verify gone
	result = srv.Exec([]string{"has-session", "-t", "lifecycle"})
	assert.Equal(t, 1, result.ExitCode)
}

func TestIntegration_MultipleSessionsIndependent(t *testing.T) {
	srv := NewServer()

	srv.Exec([]string{"new-session", "-s", "sess-a"})
	srv.Exec([]string{"new-session", "-s", "sess-b"})

	srv.Exec([]string{"new-window", "-t", "sess-a"})
	srv.Exec([]string{"new-window", "-t", "sess-a"})

	assert.Equal(t, 3, srv.WindowCount("sess-a"))
	assert.Equal(t, 1, srv.WindowCount("sess-b"))

	// Kill sess-a, sess-b unaffected
	srv.Exec([]string{"kill-session", "-t", "sess-a"})
	assert.False(t, srv.SessionExists("sess-a"))
	assert.True(t, srv.SessionExists("sess-b"))
	assert.Equal(t, 1, srv.WindowCount("sess-b"))
}

// --- Property-Based Tests ---

// validSessionName generates a valid session name for property tests.
func validSessionName() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z][a-z0-9_-]{0,20}`)
}

// TestProperty_NewSessionAlwaysCreatesWithOneWindow verifies that creating
// a session always results in exactly one session with exactly one window.
func TestProperty_NewSessionAlwaysCreatesWithOneWindow(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		srv := NewServer()
		name := validSessionName().Draw(t, "name")

		result := srv.Exec([]string{"new-session", "-s", name})
		require.Equal(t, 0, result.ExitCode)
		assert.True(t, srv.SessionExists(name))
		assert.Equal(t, 1, srv.WindowCount(name))
	})
}

// TestProperty_AttachIdempotent verifies that attaching to an existing
// session via -A flag never creates additional windows.
func TestProperty_AttachIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		srv := NewServer()
		name := validSessionName().Draw(t, "name")
		n := rapid.IntRange(1, 10).Draw(t, "attachCount")

		srv.Exec([]string{"new-session", "-s", name})

		for i := 0; i < n; i++ {
			result := srv.Exec([]string{"new-session", "-A", "-s", name})
			assert.Equal(t, 0, result.ExitCode)
		}

		assert.Equal(t, 1, srv.WindowCount(name),
			"attaching should never create additional windows")
	})
}

// TestProperty_DuplicateCreateAlwaysFails verifies creating a session
// twice (without -A) always fails.
func TestProperty_DuplicateCreateAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		srv := NewServer()
		name := validSessionName().Draw(t, "name")

		result := srv.Exec([]string{"new-session", "-s", name})
		require.Equal(t, 0, result.ExitCode)

		result = srv.Exec([]string{"new-session", "-s", name})
		assert.Equal(t, 1, result.ExitCode)
		assert.Contains(t, result.Stderr, "duplicate")
	})
}

// TestProperty_WindowCountIncreasesMonotonically verifies that each
// new-window call increases the window count by exactly one.
func TestProperty_WindowCountIncreasesMonotonically(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		srv := NewServer()
		name := validSessionName().Draw(t, "name")
		n := rapid.IntRange(0, 10).Draw(t, "extraWindows")

		srv.Exec([]string{"new-session", "-s", name})
		require.Equal(t, 1, srv.WindowCount(name))

		for i := 0; i < n; i++ {
			result := srv.Exec([]string{"new-window", "-t", name})
			require.Equal(t, 0, result.ExitCode)
		}

		assert.Equal(t, 1+n, srv.WindowCount(name))
	})
}

// TestProperty_KillRemovesFromList verifies that killing a session
// removes it from list-sessions output.
func TestProperty_KillRemovesFromList(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		srv := NewServer()
		name := validSessionName().Draw(t, "name")

		srv.Exec([]string{"new-session", "-s", name})

		result := srv.Exec([]string{"kill-session", "-t", name})
		require.Equal(t, 0, result.ExitCode)

		// list-sessions should be empty (exit 1) or not contain the name
		result = srv.Exec([]string{"list-sessions"})
		if result.ExitCode == 0 {
			assert.NotContains(t, result.Stdout, name)
		} else {
			assert.Contains(t, result.Stderr, "no tmux sessions")
		}
	})
}

// TestProperty_HasSessionConsistentWithSessionExists verifies that
// has-session exit code is consistent with the SessionExists helper.
func TestProperty_HasSessionConsistentWithSessionExists(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		srv := NewServer()
		name := validSessionName().Draw(t, "name")

		// Before creation
		result := srv.Exec([]string{"has-session", "-t", name})
		assert.Equal(t, 1, result.ExitCode)
		assert.False(t, srv.SessionExists(name))

		// Create
		srv.Exec([]string{"new-session", "-s", name})

		// After creation
		result = srv.Exec([]string{"has-session", "-t", name})
		assert.Equal(t, 0, result.ExitCode)
		assert.True(t, srv.SessionExists(name))
	})
}

// TestProperty_ResetAlwaysProducesEmptyServer verifies Reset clears
// all state regardless of how many sessions exist.
func TestProperty_ResetAlwaysProducesEmptyServer(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		srv := NewServer()
		names := rapid.SliceOfNDistinct(validSessionName(), 1, 5, func(s string) string {
			return s
		}).Draw(t, "names")

		for _, n := range names {
			srv.Exec([]string{"new-session", "-s", n})
		}
		assert.NotEmpty(t, srv.GetSessions())

		srv.Reset()
		assert.Empty(t, srv.GetSessions())

		// list-sessions should report empty
		result := srv.Exec([]string{"list-sessions"})
		assert.Equal(t, 1, result.ExitCode)
	})
}

// TestProperty_OperationsOnMissingSessionAlwaysFail verifies that
// kill-session, new-window, list-windows always fail for non-existent sessions.
func TestProperty_OperationsOnMissingSessionAlwaysFail(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		srv := NewServer()
		name := validSessionName().Draw(t, "name")

		// kill-session
		result := srv.Exec([]string{"kill-session", "-t", name})
		assert.Equal(t, 1, result.ExitCode)

		// new-window
		result = srv.Exec([]string{"new-window", "-t", name})
		assert.Equal(t, 1, result.ExitCode)

		// list-windows
		result = srv.Exec([]string{"list-windows", "-t", name})
		assert.Equal(t, 1, result.ExitCode)

		// has-session
		result = srv.Exec([]string{"has-session", "-t", name})
		assert.Equal(t, 1, result.ExitCode)
	})
}

// TestProperty_NewWindowDeactivatesPreviousWindows verifies that
// creating a new window always deactivates all previous windows.
func TestProperty_NewWindowDeactivatesPreviousWindows(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		srv := NewServer()
		name := validSessionName().Draw(t, "name")
		n := rapid.IntRange(1, 5).Draw(t, "windowCount")

		srv.Exec([]string{"new-session", "-s", name})
		for i := 0; i < n; i++ {
			srv.Exec([]string{"new-window", "-t", name})
		}

		sessions := srv.GetSessions()
		sess := sessions[name]
		require.NotNil(t, sess)

		// Only the last window should be active
		activeCount := 0
		for _, w := range sess.Windows {
			if w.Active {
				activeCount++
			}
		}
		assert.Equal(t, 1, activeCount, "exactly one window should be active")
		assert.True(t, sess.Windows[len(sess.Windows)-1].Active,
			"the last window should be the active one")
	})
}

// TestProperty_MultipleSessionsIndependent verifies operations on
// one session never affect another session.
func TestProperty_MultipleSessionsIndependent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		srv := NewServer()
		nameA := rapid.StringMatching(`a-[a-z]{2,5}`).Draw(t, "nameA")
		nameB := rapid.StringMatching(`b-[a-z]{2,5}`).Draw(t, "nameB")

		srv.Exec([]string{"new-session", "-s", nameA})
		srv.Exec([]string{"new-session", "-s", nameB})

		// Add windows to A
		srv.Exec([]string{"new-window", "-t", nameA})
		srv.Exec([]string{"new-window", "-t", nameA})

		assert.Equal(t, 3, srv.WindowCount(nameA))
		assert.Equal(t, 1, srv.WindowCount(nameB))

		// Kill A
		srv.Exec([]string{"kill-session", "-t", nameA})
		assert.False(t, srv.SessionExists(nameA))
		assert.True(t, srv.SessionExists(nameB))
		assert.Equal(t, 1, srv.WindowCount(nameB))
	})
}

// TestProperty_SendKeysAlwaysSucceeds verifies send-keys always
// returns exit code 0 on an installed server.
func TestProperty_SendKeysAlwaysSucceeds(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		srv := NewServer()
		keys := rapid.StringMatching(`[a-zA-Z0-9 _-]{1,20}`).Draw(t, "keys")

		result := srv.Exec([]string{"send-keys", "-t", "any", keys})
		assert.Equal(t, 0, result.ExitCode)
	})
}

// TestProperty_ListSessionsIncludesAllCreated verifies that list-sessions
// output contains every created session name.
func TestProperty_ListSessionsIncludesAllCreated(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		srv := NewServer()
		names := rapid.SliceOfNDistinct(validSessionName(), 1, 5, func(s string) string {
			return s
		}).Draw(t, "names")

		for _, n := range names {
			srv.Exec([]string{"new-session", "-s", n})
		}

		result := srv.Exec([]string{"list-sessions"})
		require.Equal(t, 0, result.ExitCode)

		for _, n := range names {
			assert.Contains(t, result.Stdout, n,
				"list-sessions should contain created session %q", n)
		}
	})
}

// TestProperty_GetSessionsSnapshotIsolation verifies that modifying
// the map returned by GetSessions never affects the server state.
func TestProperty_GetSessionsSnapshotIsolation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		srv := NewServer()
		name := validSessionName().Draw(t, "name")
		srv.Exec([]string{"new-session", "-s", name})

		// Tamper with the returned snapshot
		snapshot := srv.GetSessions()
		delete(snapshot, name)
		snapshot["fake"] = &SessionState{Name: "fake"}

		// Server state should be unchanged
		assert.True(t, srv.SessionExists(name))
		assert.False(t, srv.SessionExists("fake"))
	})
}

// TestProperty_NewSessionWindowNamedZero verifies that the initial window
// in a new session is always named "0".
func TestProperty_NewSessionWindowNamedZero(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		srv := NewServer()
		name := validSessionName().Draw(t, "name")

		srv.Exec([]string{"new-session", "-s", name})

		sessions := srv.GetSessions()
		sess := sessions[name]
		require.NotEmpty(t, sess.Windows)
		assert.Equal(t, "0", sess.Windows[0].Name)
		assert.True(t, sess.Windows[0].Active)
	})
}

// TestProperty_WindowNamesAreSequentialNumbers verifies that window
// names follow sequential numbering (0, 1, 2, ...).
func TestProperty_WindowNamesAreSequentialNumbers(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		srv := NewServer()
		name := validSessionName().Draw(t, "name")
		n := rapid.IntRange(0, 5).Draw(t, "extraWindows")

		srv.Exec([]string{"new-session", "-s", name})
		for i := 0; i < n; i++ {
			srv.Exec([]string{"new-window", "-t", name})
		}

		sessions := srv.GetSessions()
		sess := sessions[name]
		for i, w := range sess.Windows {
			expected := fmt.Sprintf("%d", i)
			assert.Equal(t, expected, w.Name,
				"window %d should be named %q, got %q", i, expected, w.Name)
		}
	})
}
