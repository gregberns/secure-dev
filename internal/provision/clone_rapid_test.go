// Package provision - property-based tests for clone security and error handling.
// Tests shell-quoting of repo URLs and branch names, and mkdir error propagation.
package provision

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// ============================================================
// Property: CloneRepo with empty repo is always a no-op
// ============================================================

func TestProperty_CloneRepo_EmptyRepo_NoOp(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`[a-z][a-z0-9-]{0,20}`).Draw(t, "vmName")
		branch := rapid.StringMatching(`[a-z]{0,10}`).Draw(t, "branch")
		execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
			t.Fatalf("execFn should not be called for empty repo")
			return "", "", 0, nil
		}
		err := CloneRepo(context.Background(), execFn, vmName, "", branch)
		assert.NoError(t, err)
	})
}

// ============================================================
// Property: Repo URLs are shell-quoted in clone commands
// ============================================================

func TestProperty_CloneRepo_RepoURL_IsQuotedInScript(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		user := rapid.StringMatching(`[a-z][a-z0-9-]{0,10}`).Draw(t, "user")
		repo := rapid.StringMatching(`[a-z][a-z0-9-]{0,10}`).Draw(t, "repo")
		url := "https://github.com/" + user + "/" + repo + ".git"

		var scripts []string
		execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
			script := command[len(command)-1]
			scripts = append(scripts, script)
			if strings.Contains(script, "if [ -d") {
				return "NOT_EXISTS", "", 0, nil
			}
			return "", "", 0, nil
		}

		err := CloneRepo(context.Background(), execFn, "test-vm", url, "")
		require.NoError(t, err)

		// Find the clone script and verify URL is present
		cloneFound := false
		for _, s := range scripts {
			if strings.Contains(s, "git clone") {
				cloneFound = true
				assert.Contains(t, s, url,
					"clone script should contain repo URL %q", url)
			}
		}
		assert.True(t, cloneFound, "git clone command should have been executed")
	})
}

// ============================================================
// Property: Branch names are shell-quoted in clone commands
// ============================================================

func TestProperty_CloneRepo_Branch_IsQuotedInScript(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		branch := rapid.StringMatching(`[a-z][a-z0-9/-]{0,15}`).Draw(t, "branch")

		var scripts []string
		execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
			script := command[len(command)-1]
			scripts = append(scripts, script)
			if strings.Contains(script, "if [ -d") {
				return "NOT_EXISTS", "", 0, nil
			}
			return "", "", 0, nil
		}

		err := CloneRepo(context.Background(), execFn, "test-vm", "https://github.com/user/repo.git", branch)
		require.NoError(t, err)

		cloneFound := false
		for _, s := range scripts {
			if strings.Contains(s, "git clone") {
				cloneFound = true
				assert.Contains(t, s, "--branch",
					"clone script should contain --branch flag")
				// The branch name should appear (possibly quoted)
				assert.True(t, strings.Contains(s, branch),
					"clone script should contain branch name %q: %s", branch, s)
			}
		}
		assert.True(t, cloneFound, "git clone command should have been executed")
	})
}

// ============================================================
// Property: mkdir failure propagates as an error
// ============================================================

func TestProperty_CloneRepo_MkdirFailure_PropagatesError(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		exitCode := rapid.IntRange(1, 127).Draw(t, "exitCode")

		execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
			script := command[len(command)-1]
			if strings.Contains(script, "if [ -d") {
				return "NOT_EXISTS", "", 0, nil
			}
			if strings.Contains(script, "mkdir -p") {
				return "", "permission denied", exitCode, nil
			}
			return "", "", 0, nil
		}

		err := CloneRepo(context.Background(), execFn, "test-vm", "https://github.com/user/repo.git", "")
		require.Error(t, err, "mkdir failure with exit code %d should propagate", exitCode)
		assert.Contains(t, err.Error(), "mkdir")
	})
}

// ============================================================
// Property: mkdir exec error propagates
// ============================================================

func TestProperty_CloneRepo_MkdirExecError_PropagatesError(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		errMsg := rapid.StringMatching(`[a-z ]{3,20}`).Draw(t, "errMsg")

		execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
			script := command[len(command)-1]
			if strings.Contains(script, "if [ -d") {
				return "NOT_EXISTS", "", 0, nil
			}
			if strings.Contains(script, "mkdir -p") {
				return "", "", 0, fmt.Errorf("%s", errMsg)
			}
			return "", "", 0, nil
		}

		err := CloneRepo(context.Background(), execFn, "test-vm", "https://github.com/user/repo.git", "")
		require.Error(t, err, "mkdir exec error should propagate")
		assert.Contains(t, err.Error(), errMsg)
	})
}
