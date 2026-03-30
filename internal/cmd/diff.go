// Package cmd implements the diff command.
// REQ-004-018: CI Workflow Change Detection
package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"sd/internal/backend"
	"sd/internal/ui"
)

func init() {
	diffCmd := &cobra.Command{
		Use:   "diff <name>",
		Short: "Detect changes to CI/CD and hook files inside a VM",
		Long: `Detect and alert on changes to CI/CD workflow files and git hooks
made during an agent session. This helps identify potential supply-chain
attacks where an AI agent modifies CI pipelines or git hooks.

The following paths are flagged as security-sensitive:
  .github/workflows/*
  .gitlab-ci.yml
  Jenkinsfile
  .circleci/*
  .git/hooks/*`,
		GroupID: "security",
		Args:    cobra.ExactArgs(1),
		RunE:    runDiff,
	}

	rootCmd.AddCommand(diffCmd)
}

// cicdPatterns lists file path patterns considered security-sensitive.
// REQ-004-018
var cicdPatterns = []string{
	".github/workflows/",
	".gitlab-ci.yml",
	"Jenkinsfile",
	".circleci/",
	".git/hooks/",
}

// diffEntry describes a single changed file detected inside the VM.
// REQ-004-018
type diffEntry struct {
	Path     string `json:"path"`
	Status   string `json:"status"`
	Warning  bool   `json:"warning"`
	Category string `json:"category,omitempty"`
}

// diffResult is the structured output for the diff command.
// REQ-004-018
type diffResult struct {
	VM       string       `json:"vm"`
	Changes  []diffEntry  `json:"changes"`
	Warnings []diffEntry  `json:"warnings"`
}

// runDiff executes the diff command.
// REQ-004-018
func runDiff(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	name := args[0]
	if name == "" {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: "VM name must not be empty",
		}
	}

	// Resolve backend
	backendName := ""
	if Loader() != nil {
		cfg := Loader().Get()
		backendName = cfg.Defaults.Backend
	}
	if backendName == "" {
		backendName = "lima"
	}

	b, err := getBackendFunc(backendName)
	if err != nil {
		return ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("backend %q is not available: %v", backendName, err),
		}
	}

	if err := b.Available(); err != nil {
		return ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("backend %q not available: %v", backendName, err),
		}
	}

	// Check VM exists and is running
	status, err := b.Status(cmd.Context(), name)
	if err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		return ui.CLIError{
			Code:    "diff_failed",
			Message: fmt.Sprintf("failed to check status of VM %q: %v", name, err),
		}
	}

	if status != backend.StatusRunning {
		return ui.CLIError{
			Code:    "vm_not_running",
			Message: fmt.Sprintf("VM %q is not running (status: %s)", name, status),
		}
	}

	// Run git status --porcelain inside the VM to get changed files
	result, err := b.Exec(cmd.Context(), name, []string{"sh", "-c",
		"git rev-parse --is-inside-work-tree 2>/dev/null && git status --porcelain 2>/dev/null || true"})
	if err != nil {
		if errors.Is(err, backend.ErrVMNotRunning) {
			return ui.CLIError{
				Code:    "vm_not_running",
				Message: fmt.Sprintf("VM %q is not running", name),
			}
		}
		return ui.CLIError{
			Code:    "diff_failed",
			Message: fmt.Sprintf("failed to inspect VM %q: %v", name, err),
		}
	}

	// Also check for untracked CI files that git status might not catch
	// (e.g., newly created .git/hooks/ files)
	hooksResult, _ := b.Exec(cmd.Context(), name, []string{"sh", "-c",
		"git rev-parse --git-dir 2>/dev/null && ls -1 \"$(git rev-parse --git-dir)/hooks/\" 2>/dev/null | grep -v '\\.sample$' || true"})

	entries := parseGitStatus(result.Stdout)
	entries = appendHooksEntries(entries, hooksResult.Stdout)

	var changes []diffEntry
	var warnings []diffEntry

	for _, e := range entries {
		e.Warning, e.Category = isCICDPath(e.Path)
		changes = append(changes, e)
		if e.Warning {
			warnings = append(warnings, e)
		}
	}

	if changes == nil {
		changes = []diffEntry{}
	}
	if warnings == nil {
		warnings = []diffEntry{}
	}

	data := diffResult{
		VM:       name,
		Changes:  changes,
		Warnings: warnings,
	}

	f.SuccessData(data, func() string {
		return formatDiffOutput(data)
	})
	return nil
}

// parseGitStatus parses git status --porcelain output into diffEntry slices.
// Each line has format: XY PATH or XY ORIG -> RENAMED
func parseGitStatus(output string) []diffEntry {
	if output == "" {
		return nil
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	var entries []diffEntry

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// git rev-parse output: skip "true"
		if line == "true" || line == "false" {
			continue
		}

		if len(line) < 4 {
			continue
		}

		// XY PATH format: first 2 chars are status, then path after optional space
		xy := line[:2]
		pathPart := strings.TrimSpace(line[2:])

		// Handle renames: "XY  old -> new"
		if strings.Contains(pathPart, " -> ") {
			parts := strings.SplitN(pathPart, " -> ", 2)
			pathPart = parts[1]
		}

		status := gitStatusToLabel(xy)
		entries = append(entries, diffEntry{
			Path:   pathPart,
			Status: status,
		})
	}

	return entries
}

// appendHooksEntries adds entries for non-sample git hooks found in the VM.
// REQ-004-018: The detection covers both tracked and untracked files.
func appendHooksEntries(entries []diffEntry, hooksOutput string) []diffEntry {
	if hooksOutput == "" {
		return entries
	}

	// Track which hook paths we already have
	existing := make(map[string]bool)
	for _, e := range entries {
		existing[e.Path] = true
	}

	lines := strings.Split(strings.TrimSpace(hooksOutput), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip git rev-parse output and non-hook lines
		if line == ".git" || !isHookName(line) {
			continue
		}

		hookPath := ".git/hooks/" + line
		if !existing[hookPath] {
			entries = append(entries, diffEntry{
				Path:   hookPath,
				Status: "present",
			})
		}
	}

	return entries
}

// isHookName checks if a filename is a known git hook.
func isHookName(name string) bool {
	hooks := []string{
		"applypatch-msg", "commit-msg", "fsmonitor-watchman",
		"pfsmonitor-watchman", "post-update", "pre-applypatch",
		"pre-commit", "pre-merge-commit", "pre-push",
		"pre-rebase", "pre-receive", "prepare-commit-msg",
		"push-to-checkout", "update",
	}
	for _, h := range hooks {
		if name == h {
			return true
		}
	}
	return false
}

// gitStatusToLabel converts XY status codes to a human-readable label.
func gitStatusToLabel(xy string) string {
	x := xy[0]
	y := byte(' ')
	if len(xy) > 1 {
		y = xy[1]
	}

	switch {
	case x == '?' && y == '?':
		return "untracked"
	case x == 'A' || y == 'A':
		return "added"
	case x == 'M' || y == 'M':
		return "modified"
	case x == 'D' || y == 'D':
		return "deleted"
	case x == 'R' || y == 'R':
		return "renamed"
	case x == 'C' || y == 'C':
		return "copied"
	default:
		return "changed"
	}
}

// isCICDPath checks if a file path matches CI/CD patterns.
// Returns whether it's a warning and the matching category.
// REQ-004-018
func isCICDPath(path string) (bool, string) {
	for _, pattern := range cicdPatterns {
		if strings.HasPrefix(path, pattern) || path == pattern {
			return true, pattern
		}
	}
	return false, ""
}

// formatDiffOutput renders the diff results as human-readable output.
// REQ-004-018
func formatDiffOutput(data diffResult) string {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "Diff: %s\n\n", data.VM)

	if len(data.Changes) == 0 {
		buf.WriteString("No changes detected.\n")
		return buf.String()
	}

	fmt.Fprintf(&buf, "Changes (%d):\n", len(data.Changes))
	for _, c := range data.Changes {
		marker := "  "
		if c.Warning {
			marker = "! "
		}
		fmt.Fprintf(&buf, "%s%-12s %s", marker, c.Status, c.Path)
		if c.Warning {
			fmt.Fprintf(&buf, "  [%s]", c.Category)
		}
		buf.WriteByte('\n')
	}

	if len(data.Warnings) > 0 {
		fmt.Fprintf(&buf, "\nWarnings: %d CI/CD or hook file(s) changed\n", len(data.Warnings))
		for _, w := range data.Warnings {
			fmt.Fprintf(&buf, "  ! %s (%s) -- %s\n", w.Path, w.Status, w.Category)
		}
	}

	return buf.String()
}
