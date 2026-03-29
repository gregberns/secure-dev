// Property-based tests for the session manager lifecycle state machine.
// These verify that session management invariants hold across ALL possible
// state transitions, not just the happy paths.
//
// REQ-007-008: tmux as default session manager — state machine correctness
// REQ-007-009: Named tmux sessions — naming invariants
// REQ-007-010: New tmux window — window counting invariants
// REQ-007-012: Raw SSH without tmux — bypass invariants
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

// ============================================================
// Session Lifecycle State Machine — Property-Based Tests
// ============================================================
//
// The session state machine has these states and transitions:
//
//	[no session] --EnsureSession--> [session exists, 1 window]
//	[session exists] --EnsureSession--> [session exists, same windows] (idempotent)
//	[session exists] --NewWindow--> [session exists, +1 window]
//	[session exists] --KillSession--> [no session]
//	[no session] --KillSession--> error
//	[no session] --NewWindow--> [session exists, 2 windows] (auto-creates)
//	[any] --HasSession--> bool (no side effects)
//
// Invariants:
//   I1. EnsureSession is always idempotent: N calls = 1 session, 1 window
//   I2. KillSession always removes from ListSessions
//   I3. HasSession agrees with SessionExists after every transition
//   I4. Windows are always numbered sequentially starting from 0
//   I5. Exactly one window is active at all times
//   I6. Operations on different sessions are independent
//   I7. KillSession then re-create yields fresh session
//   I8. NoTmux never creates any session
//   I9. NewWindow+EnsureSession on fresh session yields correct window count

// Property: EnsureSession always results in exactly one session with one window.
// I1: Idempotency of EnsureSession.
// REQ-007-008
func TestProperty_EnsureSessionCreatesOneSession(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{2,8}`).Draw(t, "vmName")
		srv := mocktmux.NewServer()
		mgr := NewManager(&mockRunner{server: srv, vmRunning: true})
		ctx := context.Background()

		err := mgr.EnsureSession(ctx, Options{VMName: vmName})
		require.NoError(t, err)

		sessionName := "sd-" + vmName
		assert.True(t, srv.SessionExists(sessionName), "session must exist")
		assert.Equal(t, 1, srv.WindowCount(sessionName), "must have exactly 1 window")
	})
}

// Property: Repeated EnsureSession calls are always idempotent.
// I1: Any number of calls yields exactly 1 session, 1 window.
// REQ-007-008
func TestProperty_EnsureSessionAlwaysIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{2,8}`).Draw(t, "vmName")
		n := rapid.IntRange(1, 10).Draw(t, "repetitions")
		srv := mocktmux.NewServer()
		mgr := NewManager(&mockRunner{server: srv, vmRunning: true})
		ctx := context.Background()

		sessionName := "sd-" + vmName
		for i := 0; i < n; i++ {
			err := mgr.EnsureSession(ctx, Options{VMName: vmName})
			require.NoError(t, err, "call %d/%d should succeed", i+1, n)
		}

		assert.True(t, srv.SessionExists(sessionName))
		assert.Equal(t, 1, srv.WindowCount(sessionName),
			"idempotent calls must not create extra windows")
	})
}

// Property: KillSession always removes the session from ListSessions.
// I2: Kill makes the session invisible to List.
// REQ-007-008
func TestProperty_KillSessionRemovesFromList(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{2,8}`).Draw(t, "vmName")
		srv := mocktmux.NewServer()
		mgr := NewManager(&mockRunner{server: srv, vmRunning: true})
		ctx := context.Background()

		sessionName := "sd-" + vmName
		require.NoError(t, mgr.EnsureSession(ctx, Options{VMName: vmName}))
		require.NoError(t, mgr.KillSession(ctx, vmName, sessionName))

		// Must not appear in list
		sessions, err := mgr.ListSessions(ctx, vmName)
		require.NoError(t, err)
		for _, s := range sessions {
			assert.NotEqual(t, sessionName, s,
				"killed session %q must not appear in list", sessionName)
		}

		// Must not exist in server
		assert.False(t, srv.SessionExists(sessionName))
	})
}

// Property: HasSession agrees with mock server's SessionExists after every transition.
// I3: Consistency between HasSession and server state.
// REQ-007-008
func TestProperty_HasSessionConsistentWithServer(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{2,8}`).Draw(t, "vmName")
		srv := mocktmux.NewServer()
		mgr := NewManager(&mockRunner{server: srv, vmRunning: true})
		ctx := context.Background()

		sessionName := "sd-" + vmName

		// Before creation: both must agree "not exists"
		exists, err := mgr.HasSession(ctx, vmName, sessionName)
		require.NoError(t, err)
		assert.False(t, exists)
		assert.False(t, srv.SessionExists(sessionName))

		// After creation: both must agree "exists"
		require.NoError(t, mgr.EnsureSession(ctx, Options{VMName: vmName}))
		exists, err = mgr.HasSession(ctx, vmName, sessionName)
		require.NoError(t, err)
		assert.True(t, exists)
		assert.True(t, srv.SessionExists(sessionName))

		// After kill: both must agree "not exists"
		require.NoError(t, mgr.KillSession(ctx, vmName, sessionName))
		exists, err = mgr.HasSession(ctx, vmName, sessionName)
		require.NoError(t, err)
		assert.False(t, exists)
		assert.False(t, srv.SessionExists(sessionName))
	})
}

// Property: Windows are always numbered sequentially from 0.
// I4: Sequential window numbering.
// REQ-007-010
func TestProperty_WindowsAlwaysSequential(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{2,8}`).Draw(t, "vmName")
		nWindows := rapid.IntRange(0, 6).Draw(t, "extraWindows")
		srv := mocktmux.NewServer()
		mgr := NewManager(&mockRunner{server: srv, vmRunning: true})
		ctx := context.Background()

		sessionName := "sd-" + vmName
		require.NoError(t, mgr.EnsureSession(ctx, Options{VMName: vmName}))

		for i := 0; i < nWindows; i++ {
			require.NoError(t, mgr.NewWindow(ctx, Options{VMName: vmName}))
		}

		sessions := srv.GetSessions()
		sess, ok := sessions[sessionName]
		require.True(t, ok, "session must exist")
		require.Len(t, sess.Windows, 1+nWindows)

		for i, w := range sess.Windows {
			expected := fmt.Sprintf("%d", i)
			assert.Equal(t, expected, w.Name,
				"window %d should be named %q, got %q", i, expected, w.Name)
		}
	})
}

// Property: Exactly one window is always active in a session.
// I5: Single active window invariant.
// REQ-007-010
func TestProperty_ExactlyOneActiveWindow(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{2,8}`).Draw(t, "vmName")
		nWindows := rapid.IntRange(0, 6).Draw(t, "extraWindows")
		srv := mocktmux.NewServer()
		mgr := NewManager(&mockRunner{server: srv, vmRunning: true})
		ctx := context.Background()

		sessionName := "sd-" + vmName
		require.NoError(t, mgr.EnsureSession(ctx, Options{VMName: vmName}))

		for i := 0; i < nWindows; i++ {
			require.NoError(t, mgr.NewWindow(ctx, Options{VMName: vmName}))
		}

		sessions := srv.GetSessions()
		sess := sessions[sessionName]
		require.NotNil(t, sess)

		activeCount := 0
		for _, w := range sess.Windows {
			if w.Active {
				activeCount++
			}
		}
		assert.Equal(t, 1, activeCount,
			"exactly 1 window must be active, got %d", activeCount)

		// The last window should be the active one
		assert.True(t, sess.Windows[len(sess.Windows)-1].Active,
			"the newest window should be active")
	})
}

// Property: Operations on different sessions are always independent.
// I6: Session isolation — no cross-session effects.
// REQ-007-008, REQ-007-009
func TestProperty_SessionsAlwaysIndependent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		srv := mocktmux.NewServer()
		mgr := NewManager(&mockRunner{server: srv, vmRunning: true})
		ctx := context.Background()

		nameA := rapid.StringMatching(`a-[a-z]{2,5}`).Draw(t, "nameA")
		nameB := rapid.StringMatching(`b-[a-z]{2,5}`).Draw(t, "nameB")

		// Create both sessions
		require.NoError(t, mgr.EnsureSession(ctx, Options{VMName: "vm", Session: nameA}))
		require.NoError(t, mgr.EnsureSession(ctx, Options{VMName: "vm", Session: nameB}))

		// Add windows to A only
		require.NoError(t, mgr.NewWindow(ctx, Options{VMName: "vm", Session: nameA}))
		require.NoError(t, mgr.NewWindow(ctx, Options{VMName: "vm", Session: nameA}))

		// A should have 3 windows (1 + 2), B should still have 1
		assert.Equal(t, 3, srv.WindowCount(nameA))
		assert.Equal(t, 1, srv.WindowCount(nameB))

		// Kill A
		require.NoError(t, mgr.KillSession(ctx, "vm", nameA))

		// B must be unaffected
		assert.False(t, srv.SessionExists(nameA))
		assert.True(t, srv.SessionExists(nameB))
		assert.Equal(t, 1, srv.WindowCount(nameB))
	})
}

// Property: KillSession then re-create yields a fresh session with 1 window.
// I7: Kill-then-recreate is clean.
// REQ-007-008
func TestProperty_KillThenRecreateIsClean(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{2,8}`).Draw(t, "vmName")
		srv := mocktmux.NewServer()
		mgr := NewManager(&mockRunner{server: srv, vmRunning: true})
		ctx := context.Background()

		sessionName := "sd-" + vmName

		// Create, add windows, kill, recreate
		require.NoError(t, mgr.EnsureSession(ctx, Options{VMName: vmName}))
		require.NoError(t, mgr.NewWindow(ctx, Options{VMName: vmName}))
		require.NoError(t, mgr.NewWindow(ctx, Options{VMName: vmName}))
		assert.Equal(t, 3, srv.WindowCount(sessionName))

		require.NoError(t, mgr.KillSession(ctx, vmName, sessionName))
		assert.False(t, srv.SessionExists(sessionName))

		// Recreate — should be fresh
		require.NoError(t, mgr.EnsureSession(ctx, Options{VMName: vmName}))
		assert.True(t, srv.SessionExists(sessionName))
		assert.Equal(t, 1, srv.WindowCount(sessionName),
			"recreated session must start fresh with 1 window")
	})
}

// Property: NoTmux never creates any session regardless of other options.
// I8: NoTmux bypass invariant.
// REQ-007-012
func TestProperty_NoTmuxNeverCreatesSession(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{2,8}`).Draw(t, "vmName")
		srv := mocktmux.NewServer()
		mgr := NewManager(&mockRunner{server: srv, vmRunning: true})
		ctx := context.Background()

		err := mgr.EnsureSession(ctx, Options{VMName: vmName, NoTmux: true})
		require.NoError(t, err)

		// No session should exist at all
		sessions := srv.GetSessions()
		assert.Empty(t, sessions, "NoTmux must never create any session")
	})
}

// Property: NoTmux + NewWindow always returns ErrMutuallyExclusive.
// I8: Mutual exclusion is always enforced.
// REQ-007-012
func TestProperty_NoTmuxNewWindowAlwaysErrors(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{2,8}`).Draw(t, "vmName")
		srv := mocktmux.NewServer()
		mgr := NewManager(&mockRunner{server: srv, vmRunning: true})
		ctx := context.Background()

		err := mgr.EnsureSession(ctx, Options{
			VMName:    vmName,
			NoTmux:    true,
			NewWindow: true,
		})
		assert.ErrorIs(t, err, ErrMutuallyExclusive)
	})
}

// Property: Full lifecycle always succeeds (ensure → has → window → list → kill → verify).
// REQ-007-008, REQ-007-010
func TestProperty_FullLifecycleAlwaysSucceeds(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{2,8}`).Draw(t, "vmName")
		sessionName := rapid.StringMatching(`s-[a-z]{2,5}`).Draw(t, "sessionName")
		nWindows := rapid.IntRange(0, 3).Draw(t, "extraWindows")
		srv := mocktmux.NewServer()
		mgr := NewManager(&mockRunner{server: srv, vmRunning: true})
		ctx := context.Background()

		// 1. Create
		require.NoError(t, mgr.EnsureSession(ctx, Options{
			VMName:  vmName,
			Session: sessionName,
		}))
		assert.True(t, srv.SessionExists(sessionName))
		assert.Equal(t, 1, srv.WindowCount(sessionName))

		// 2. Verify exists
		exists, err := mgr.HasSession(ctx, vmName, sessionName)
		require.NoError(t, err)
		assert.True(t, exists)

		// 3. Add windows
		for i := 0; i < nWindows; i++ {
			require.NoError(t, mgr.NewWindow(ctx, Options{VMName: vmName, Session: sessionName}))
		}
		assert.Equal(t, 1+nWindows, srv.WindowCount(sessionName))

		// 4. List contains our session
		sessions, err := mgr.ListSessions(ctx, vmName)
		require.NoError(t, err)
		assert.Contains(t, sessions, sessionName)

		// 5. Kill
		require.NoError(t, mgr.KillSession(ctx, vmName, sessionName))
		assert.False(t, srv.SessionExists(sessionName))

		// 6. Verify gone
		exists, err = mgr.HasSession(ctx, vmName, sessionName)
		require.NoError(t, err)
		assert.False(t, exists)
	})
}

// Property: ListSessions always contains exactly the set of created sessions.
// REQ-007-008
func TestProperty_ListReflectsExactState(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		srv := mocktmux.NewServer()
		mgr := NewManager(&mockRunner{server: srv, vmRunning: true})
		ctx := context.Background()

		names := rapid.SliceOfNDistinct(
			rapid.StringMatching(`s-[a-z]{2,5}`),
			1, 5,
			func(s string) string { return s },
		).Draw(t, "sessionNames")

		created := make(map[string]bool)
		for _, name := range names {
			require.NoError(t, mgr.EnsureSession(ctx, Options{
				VMName:  "testvm",
				Session: name,
			}))
			created[name] = true
		}

		sessions, err := mgr.ListSessions(ctx, "testvm")
		require.NoError(t, err)
		assert.Len(t, sessions, len(created))

		for name := range created {
			assert.Contains(t, sessions, name)
		}
	})
}

// Property: KillSession on a nonexistent session always returns ErrSessionNotFound.
// REQ-007-008
func TestProperty_KillNonexistentAlwaysErrors(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		srv := mocktmux.NewServer()
		mgr := NewManager(&mockRunner{server: srv, vmRunning: true})
		ctx := context.Background()

		name := rapid.StringMatching(`ghost-[a-z0-9]{3,8}`).Draw(t, "name")
		err := mgr.KillSession(ctx, "vm", name)
		assert.ErrorIs(t, err, ErrSessionNotFound)
	})
}

// Property: EnsureSession with a named session creates that exact session name.
// REQ-007-009
func TestProperty_NamedSessionCreatesExactName(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		sessionName := rapid.StringMatching(`[a-z][a-z0-9_-]{2,15}`).Draw(t, "sessionName")
		srv := mocktmux.NewServer()
		mgr := NewManager(&mockRunner{server: srv, vmRunning: true})
		ctx := context.Background()

		require.NoError(t, mgr.EnsureSession(ctx, Options{
			VMName:  "testvm",
			Session: sessionName,
		}))

		assert.True(t, srv.SessionExists(sessionName))
		// Default session should NOT exist
		assert.False(t, srv.SessionExists("sd-testvm"))
	})
}

// Property: Options.ResolvedSession always returns "sd-<vmName>" when Session is empty.
// REQ-007-008
func TestProperty_DefaultSessionNameAlwaysPrefixed(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`[a-z][a-z0-9-]{0,16}`).Draw(t, "vmName")
		opts := Options{VMName: vmName}
		resolved := opts.ResolvedSession()

		assert.True(t, strings.HasPrefix(resolved, "sd-"),
			"resolved session must start with 'sd-'")
		assert.Equal(t, "sd-"+vmName, resolved)
	})
}

// Property: Options.ResolvedSession returns custom name when Session is set.
// REQ-007-009
func TestProperty_CustomSessionNameOverridesDefault(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{2,8}`).Draw(t, "vmName")
		customName := rapid.StringMatching(`[a-z][a-z0-9_-]{2,10}`).Draw(t, "customName")
		opts := Options{VMName: vmName, Session: customName}
		resolved := opts.ResolvedSession()

		assert.Equal(t, customName, resolved)
		assert.NotContains(t, resolved, "sd-")
	})
}

// Property: NewWindow on fresh session via EnsureSession(NewWindow=true) creates 2 windows.
// I9: NewWindow on fresh = create + new-window.
// REQ-007-010
func TestProperty_NewWindowOnFreshSessionTwoWindows(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{2,8}`).Draw(t, "vmName")
		srv := mocktmux.NewServer()
		mgr := NewManager(&mockRunner{server: srv, vmRunning: true})
		ctx := context.Background()

		sessionName := "sd-" + vmName
		require.NoError(t, mgr.EnsureSession(ctx, Options{
			VMName:    vmName,
			NewWindow: true,
		}))

		assert.True(t, srv.SessionExists(sessionName))
		assert.Equal(t, 2, srv.WindowCount(sessionName),
			"NewWindow on fresh session should yield 2 windows")
	})
}

// Property: Tmux not installed always returns ErrTmuxNotInstalled.
// REQ-007-021
func TestProperty_TmuxNotInstalledAlwaysErrors(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{2,8}`).Draw(t, "vmName")
		srv := mocktmux.NewServerWithoutTmux()
		mgr := NewManager(&mockRunner{server: srv, vmRunning: true})
		ctx := context.Background()

		err := mgr.EnsureSession(ctx, Options{VMName: vmName})
		assert.ErrorIs(t, err, ErrTmuxNotInstalled)
	})
}

// Property: Concurrent session creation on different names always succeeds.
// REQ-007-020
func TestProperty_ConcurrentCreationAlwaysSucceeds(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		srv := mocktmux.NewServer()
		mgr := NewManager(&mockRunner{server: srv, vmRunning: true})
		ctx := context.Background()

		n := rapid.IntRange(2, 10).Draw(t, "n")
		names := make([]string, n)
		for i := 0; i < n; i++ {
			names[i] = fmt.Sprintf("worker-%d", i)
		}

		errCh := make(chan error, n)
		for _, name := range names {
			go func(n string) {
				errCh <- mgr.EnsureSession(ctx, Options{
					VMName:  "testvm",
					Session: n,
				})
			}(name)
		}

		for range names {
			require.NoError(t, <-errCh)
		}

		// All sessions must exist
		sessions, err := mgr.ListSessions(ctx, "testvm")
		require.NoError(t, err)
		assert.Len(t, sessions, n)

		for _, name := range names {
			assert.True(t, srv.SessionExists(name),
				"session %q must exist after concurrent creation", name)
		}
	})
}

// Property: BuildTmuxArgs with newWindow=false always produces new-session command.
// REQ-007-008
func TestProperty_BuildTmuxArgsDefaultIsNewSession(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`[a-z][a-z0-9_-]{2,10}`).Draw(t, "name")
		args := BuildTmuxArgs(name, false)

		assert.Equal(t, []string{"tmux", "new-session", "-A", "-s", name}, args)
	})
}

// Property: BuildTmuxArgs with newWindow=true always produces new-window command.
// REQ-007-010
func TestProperty_BuildTmuxArgsNewWindowIsNewWindow(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`[a-z][a-z0-9_-]{2,10}`).Draw(t, "name")
		args := BuildTmuxArgs(name, true)

		assert.Equal(t, []string{"tmux", "new-window", "-t", name}, args)
	})
}

// Property: validateName accepts all alphanumeric + hyphen + underscore names.
// REQ-007-009
func TestProperty_ValidateNameAcceptsValidChars(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`[a-zA-Z0-9_-]{1,30}`).Draw(t, "name")
		if name == "" {
			t.Skip("empty")
		}
		assert.NoError(t, validateName(name))
	})
}

// Property: validateName rejects all names containing special characters.
// REQ-007-009
func TestProperty_ValidateNameRejectsSpecialChars(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		prefix := rapid.StringMatching(`[a-z]{2,5}`).Draw(t, "prefix")
		special := rapid.SampledFrom([]string{
			"!", "@", "#", "$", "%", ".", "/", "\\", ":", "(", ")", " ", "\t",
		}).Draw(t, "special")
		suffix := rapid.StringMatching(`[a-z]{2,5}`).Draw(t, "suffix")
		name := prefix + special + suffix

		assert.ErrorIs(t, validateName(name), ErrInvalidSession)
	})
}

// Property: validateName always rejects the empty string.
// REQ-007-009
func TestProperty_ValidateNameRejectsEmpty(t *testing.T) {
	assert.ErrorIs(t, validateName(""), ErrInvalidSession)
}

// Property: Options.Validate always rejects empty VMName.
// REQ-007-008
func TestProperty_ValidateRejectsEmptyVMName(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		session := rapid.StringMatching(`[a-z]{2,8}`).Draw(t, "session")
		err := Options{VMName: "", Session: session}.Validate()
		assert.ErrorIs(t, err, ErrInvalidSession)
	})
}

// Property: Options.Validate always accepts valid options.
// REQ-007-008
func TestProperty_ValidateAcceptsValidOptions(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{2,8}`).Draw(t, "vmName")
		session := rapid.StringMatching(`[a-z][a-z0-9_-]{2,10}`).Draw(t, "session")
		noTmux := rapid.Bool().Draw(t, "noTmux")

		opts := Options{
			VMName:  vmName,
			Session: session,
			NoTmux:  noTmux,
		}
		assert.NoError(t, opts.Validate())
	})
}

// Property: Options.Validate always rejects NoTmux + NewWindow.
// REQ-007-012
func TestProperty_ValidateRejectsNoTmuxNewWindow(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{2,8}`).Draw(t, "vmName")
		opts := Options{
			VMName:    vmName,
			NoTmux:    true,
			NewWindow: true,
		}
		err := opts.Validate()
		assert.ErrorIs(t, err, ErrMutuallyExclusive)
	})
}

// Property: DefaultTmuxConfig always contains all required settings.
// REQ-007-011
func TestProperty_DefaultTmuxConfigAlwaysComplete(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		config := DefaultTmuxConfig()
		required := []string{
			"Managed by sd",
			"prefix C-a",
			"mouse on",
			"history-limit 50000",
			"split-window -h",
			"split-window -v",
			"source-file",
		}
		for _, r := range required {
			assert.Contains(t, config, r, "config must contain %q", r)
		}
	})
}

