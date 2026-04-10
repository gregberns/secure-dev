// Package provision implements repository cloning into VMs.
// REQ-009-008: Repository clone execution.
// REQ-009-010: Idempotency -- skip clone if directory exists with correct remote.
// REQ-009-013: Failure handling with actionable errors.
package provision

import (
	"context"
	"fmt"
	"strings"
)

// CloneRepo clones a git repository into ~/projects/<vmName> inside the VM.
// If the target directory already exists with the correct remote, it runs
// git fetch and (optionally) git checkout instead.
// REQ-009-008, REQ-009-010, REQ-009-013
func CloneRepo(ctx context.Context, execFn ExecFunc, vmName, repo, branch string) error {
	if repo == "" {
		return nil
	}

	target := fmt.Sprintf("~/projects/%s", vmName)
	qTarget := shellQuote(target)
	qRepo := shellQuote(repo)
	qBranch := shellQuote(branch)

	// Check if target directory exists and is a git repo with matching remote.
	// REQ-009-010: Idempotency.
	checkScript := fmt.Sprintf(`set -eu -o pipefail
if [ -d %s ]; then
  if [ -d %s/.git ]; then
    remote=$(git -C %s remote get-url origin 2>/dev/null || echo "")
    echo "EXISTS_GIT:${remote}"
  else
    echo "EXISTS_NOT_GIT"
  fi
else
  echo "NOT_EXISTS"
fi`, qTarget, qTarget, qTarget)

	stdout, stderr, exitCode, err := execFn(ctx, vmName, []string{"bash", "-c", checkScript})
	if err != nil {
		return fmt.Errorf("git clone failed for %q: %v", repo, err)
	}
	if exitCode != 0 {
		return fmt.Errorf("git clone failed for %q: %s", repo, tailLines(stderr, 20))
	}

	result := strings.TrimSpace(stdout)

	switch {
	case result == "EXISTS_NOT_GIT":
		return fmt.Errorf("clone target %s exists but is not a git repository", target)

	case strings.HasPrefix(result, "EXISTS_GIT:"):
		existingRemote := strings.TrimPrefix(result, "EXISTS_GIT:")
		if existingRemote != repo {
			return fmt.Errorf("clone target %s exists with different remote %q (expected %q)", target, existingRemote, repo)
		}
		// Fetch and optionally checkout the branch.
		fetchScript := fmt.Sprintf("set -eux -o pipefail\ngit -C %s fetch", qTarget)
		if branch != "" {
			fetchScript += fmt.Sprintf("\ngit -C %s checkout %s", qTarget, qBranch)
		}
		_, stderr, exitCode, err := execFn(ctx, vmName, []string{"bash", "-c", fetchScript})
		if err != nil {
			return fmt.Errorf("git fetch failed for %q: %v", repo, err)
		}
		if exitCode != 0 {
			return cloneError(repo, stderr)
		}

	default:
		// Clone fresh.
		// Fix 3: Check mkdir error instead of silently discarding it.
		mkdirScript := "set -eux -o pipefail\nmkdir -p ~/projects"
		_, mkdirStderr, mkdirExit, mkdirErr := execFn(ctx, vmName, []string{"bash", "-c", mkdirScript})
		if mkdirErr != nil {
			return fmt.Errorf("git clone failed for %q: mkdir ~/projects: %v", repo, mkdirErr)
		}
		if mkdirExit != 0 {
			return fmt.Errorf("git clone failed for %q: mkdir ~/projects: exit code %d\n%s",
				repo, mkdirExit, tailLines(mkdirStderr, 20))
		}

		cloneCmd := "set -eux -o pipefail\ngit clone"
		if branch != "" {
			cloneCmd += " --branch " + qBranch
		}
		cloneCmd += " " + qRepo + " " + qTarget

		_, stderr, exitCode, err := execFn(ctx, vmName, []string{"bash", "-c", cloneCmd})
		if err != nil {
			return fmt.Errorf("git clone failed for %q: %v", repo, err)
		}
		if exitCode != 0 {
			return cloneError(repo, stderr)
		}
	}

	return nil
}

// cloneError produces an actionable error from git clone/fetch output.
// REQ-009-013: Distinguish auth failure from network failure.
func cloneError(repo, stderr string) error {
	output := tailLines(stderr, 20)
	lower := strings.ToLower(output)
	if strings.Contains(lower, "authentication") ||
		strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "could not read from remote") {
		return fmt.Errorf("git clone failed for %q: authentication required. Configure a GitHub token via the VM's env section.\n%s", repo, output)
	}
	return fmt.Errorf("git clone failed for %q:\n%s", repo, output)
}
