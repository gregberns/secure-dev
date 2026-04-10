// Package provision provides tests for repository cloning.
// REQ-009-008: Repository clone execution.
// REQ-009-010: Idempotency.
// REQ-009-013: Failure handling.
package provision

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloneRepo_EmptyRepo(t *testing.T) {
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		t.Fatal("execFn should not be called for empty repo")
		return "", "", 0, nil
	}
	err := CloneRepo(context.Background(), execFn, "test-vm", "", "")
	assert.NoError(t, err)
}

func TestCloneRepo_FreshClone(t *testing.T) {
	var scripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		script := command[len(command)-1]
		scripts = append(scripts, script)
		if strings.Contains(script, "if [ -d") {
			return "NOT_EXISTS", "", 0, nil
		}
		return "", "", 0, nil
	}

	err := CloneRepo(context.Background(), execFn, "test-vm", "https://github.com/user/repo.git", "")
	require.NoError(t, err)

	// Should have: check script, mkdir, clone
	cloneFound := false
	for _, s := range scripts {
		if strings.Contains(s, "git clone") {
			cloneFound = true
			assert.Contains(t, s, "https://github.com/user/repo.git")
			assert.Contains(t, s, "~/projects/test-vm")
			assert.NotContains(t, s, "--branch")
		}
	}
	assert.True(t, cloneFound, "git clone command should have been executed")
}

func TestCloneRepo_FreshCloneWithBranch(t *testing.T) {
	var scripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		script := command[len(command)-1]
		scripts = append(scripts, script)
		if strings.Contains(script, "if [ -d") {
			return "NOT_EXISTS", "", 0, nil
		}
		return "", "", 0, nil
	}

	err := CloneRepo(context.Background(), execFn, "test-vm", "https://github.com/user/repo.git", "develop")
	require.NoError(t, err)

	cloneFound := false
	for _, s := range scripts {
		if strings.Contains(s, "git clone") {
			cloneFound = true
			assert.Contains(t, s, "--branch develop")
			assert.Contains(t, s, "https://github.com/user/repo.git")
		}
	}
	assert.True(t, cloneFound)
}

func TestCloneRepo_ExistingWithCorrectRemote_Fetches(t *testing.T) {
	var scripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		script := command[len(command)-1]
		scripts = append(scripts, script)
		if strings.Contains(script, "if [ -d") {
			return "EXISTS_GIT:https://github.com/user/repo.git", "", 0, nil
		}
		return "", "", 0, nil
	}

	err := CloneRepo(context.Background(), execFn, "test-vm", "https://github.com/user/repo.git", "")
	require.NoError(t, err)

	fetchFound := false
	for _, s := range scripts {
		if strings.Contains(s, "git") && strings.Contains(s, "fetch") {
			fetchFound = true
		}
	}
	assert.True(t, fetchFound, "should run git fetch for existing repo")
}

func TestCloneRepo_ExistingWithCorrectRemote_CheckoutBranch(t *testing.T) {
	var scripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		script := command[len(command)-1]
		scripts = append(scripts, script)
		if strings.Contains(script, "if [ -d") {
			return "EXISTS_GIT:https://github.com/user/repo.git", "", 0, nil
		}
		return "", "", 0, nil
	}

	err := CloneRepo(context.Background(), execFn, "test-vm", "https://github.com/user/repo.git", "main")
	require.NoError(t, err)

	checkoutFound := false
	for _, s := range scripts {
		if strings.Contains(s, "checkout main") {
			checkoutFound = true
		}
	}
	assert.True(t, checkoutFound, "should checkout specified branch")
}

func TestCloneRepo_ExistingNotGitRepo_Error(t *testing.T) {
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		script := command[len(command)-1]
		if strings.Contains(script, "if [ -d") {
			return "EXISTS_NOT_GIT", "", 0, nil
		}
		return "", "", 0, nil
	}

	err := CloneRepo(context.Background(), execFn, "test-vm", "https://github.com/user/repo.git", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exists but is not a git repository")
}

func TestCloneRepo_ExistingDifferentRemote_Error(t *testing.T) {
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		script := command[len(command)-1]
		if strings.Contains(script, "if [ -d") {
			return "EXISTS_GIT:https://github.com/other/repo.git", "", 0, nil
		}
		return "", "", 0, nil
	}

	err := CloneRepo(context.Background(), execFn, "test-vm", "https://github.com/user/repo.git", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "different remote")
}

func TestCloneRepo_AuthFailure(t *testing.T) {
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		script := command[len(command)-1]
		if strings.Contains(script, "if [ -d") {
			return "NOT_EXISTS", "", 0, nil
		}
		if strings.Contains(script, "git clone") {
			return "", "fatal: Authentication failed for 'https://github.com/user/repo.git'", 128, nil
		}
		return "", "", 0, nil
	}

	err := CloneRepo(context.Background(), execFn, "test-vm", "https://github.com/user/repo.git", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication required")
}

func TestCloneRepo_NetworkFailure(t *testing.T) {
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		script := command[len(command)-1]
		if strings.Contains(script, "if [ -d") {
			return "NOT_EXISTS", "", 0, nil
		}
		if strings.Contains(script, "git clone") {
			return "", "fatal: unable to access 'https://github.com/user/repo.git': Could not resolve host: github.com", 128, nil
		}
		return "", "", 0, nil
	}

	err := CloneRepo(context.Background(), execFn, "test-vm", "https://github.com/user/repo.git", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git clone failed")
	assert.NotContains(t, err.Error(), "authentication required")
}

func TestCloneRepo_ExecError(t *testing.T) {
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		return "", "", 0, fmt.Errorf("VM not running")
	}

	err := CloneRepo(context.Background(), execFn, "test-vm", "https://github.com/user/repo.git", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "VM not running")
}

func TestCloneRepo_TargetPath(t *testing.T) {
	var scripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		script := command[len(command)-1]
		scripts = append(scripts, script)
		if strings.Contains(script, "if [ -d") {
			return "NOT_EXISTS", "", 0, nil
		}
		return "", "", 0, nil
	}

	err := CloneRepo(context.Background(), execFn, "my-app", "https://github.com/user/repo.git", "")
	require.NoError(t, err)

	// Target should be ~/projects/<vmName>
	cloneFound := false
	for _, s := range scripts {
		if strings.Contains(s, "git clone") {
			cloneFound = true
			assert.Contains(t, s, "~/projects/my-app")
		}
	}
	assert.True(t, cloneFound)
}

func TestCloneRepo_SSHUrl(t *testing.T) {
	var scripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		script := command[len(command)-1]
		scripts = append(scripts, script)
		if strings.Contains(script, "if [ -d") {
			return "NOT_EXISTS", "", 0, nil
		}
		return "", "", 0, nil
	}

	err := CloneRepo(context.Background(), execFn, "test-vm", "git@github.com:user/repo.git", "")
	require.NoError(t, err)

	cloneFound := false
	for _, s := range scripts {
		if strings.Contains(s, "git clone") {
			cloneFound = true
			assert.Contains(t, s, "git@github.com:user/repo.git")
		}
	}
	assert.True(t, cloneFound)
}
