// Package cmd implements the prune command.
//
// bug-no-prune-command: sd list warns about state directories under
// $SD_HOME/vms/<name>/ whose backing VM no longer exists, but until now there
// was no command to clean them up. `sd prune` detects orphan state
// directories, optionally lists them (--dry-run), and removes them when
// --yes is passed (or, in human mode, after interactive confirmation).
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"sd/internal/backend"
	"sd/internal/security"
	"sd/internal/ui"
)

// pruneStdin is the source read for interactive confirmation. Overridable in
// tests via SetPruneStdin so we can simulate "yes" / "no" without a TTY.
var pruneStdin *os.File = os.Stdin

// SetPruneStdin overrides the stdin used by `sd prune` confirmation prompts.
// Intended for tests only.
func SetPruneStdin(f *os.File) { pruneStdin = f }

func init() {
	pruneCmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove orphaned VM state directories",
		Long: `Detect and remove orphaned VM state directories under $SD_HOME/vms/<name>/.

A state directory is considered orphaned when no registered backend reports a
VM with that name (e.g., the VM was destroyed via the backend directly, or the
host was reset). By default prune runs in dry-run mode and only reports what
would be removed. Pass --yes to skip the interactive confirmation in human
mode (required for --json mode).

Examples:
  sd prune                  # dry-run, show orphans
  sd prune --yes            # remove without prompt
  sd prune --json --yes     # machine-readable, non-interactive removal`,
		GroupID: "vm",
		RunE:    runPrune,
	}
	pruneCmd.Flags().Bool("dry-run", false, "report orphan state directories without removing (default behavior)")
	pruneCmd.Flags().Bool("yes", false, "skip confirmation prompt and remove orphan state directories")
	// bug-no-prune-command: opt-in escape hatch when a backend is unreachable.
	// Without this flag, any backend error during enumeration aborts prune so
	// we never misclassify live VMs as orphans.
	pruneCmd.Flags().Bool("include-unavailable-backends", false, "prune even if a backend cannot be queried (dangerous; may delete state for live VMs)")
	rootCmd.AddCommand(pruneCmd)
}

// pruneResult is the JSON envelope returned by `sd prune`.
// bug-no-prune-command: stable shape so agents can parse outcomes.
type pruneResult struct {
	Pruned []string `json:"pruned"`
	Kept   []string `json:"kept"`
	DryRun bool     `json:"dry_run"`
}

// findOrphans returns the names of state directories under
// $SD_HOME/vms/ whose name is not present in any registered backend's List().
// The returned slice is sorted lexicographically for deterministic output.
//
// bug-no-prune-command: a transient backend failure used to be silently
// ignored; that classified every live VM behind that backend as an orphan and
// `--yes` then wiped its state directory (SSH keys, credentials, audit log).
// We now fail closed: any backend that fails Available() or List() aborts
// prune unless the caller passes --include-unavailable-backends.
func findOrphans(cmd *cobra.Command) ([]string, error) {
	l := Loader()
	if l == nil {
		return nil, fmt.Errorf("config loader not initialized")
	}
	vmsDir := filepath.Join(l.SDHome(), "vms")
	entries, err := os.ReadDir(vmsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("read vms directory: %w", err)
	}

	includeUnavailable, _ := cmd.Flags().GetBool("include-unavailable-backends")

	known := make(map[string]bool)
	for _, name := range allBackendNames() {
		b, err := getBackendFunc(name)
		if err != nil {
			if includeUnavailable {
				continue
			}
			return nil, fmt.Errorf("backend %q could not be loaded: %v; refusing to prune to avoid deleting live-VM state (use --include-unavailable-backends to override)", name, err)
		}
		if err := b.Available(); err != nil {
			if includeUnavailable {
				continue
			}
			return nil, fmt.Errorf("backend %q is unavailable: %v; refusing to prune to avoid deleting live-VM state (use --include-unavailable-backends to override)", name, err)
		}
		bvms, err := b.List(cmd.Context())
		if err != nil {
			if includeUnavailable {
				continue
			}
			return nil, fmt.Errorf("backend %q list failed: %v; refusing to prune to avoid deleting live-VM state (use --include-unavailable-backends to override)", name, err)
		}
		for _, vm := range bvms {
			known[vm.Name] = true
		}
	}

	var orphans []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !known[name] {
			orphans = append(orphans, name)
		}
	}
	sort.Strings(orphans)
	return orphans, nil
}

// confirmPrune reads a yes/no answer from pruneStdin. Returns true on
// "y" or "yes" (case-insensitive). Any other input (or EOF) returns false.
func confirmPrune(f *ui.Formatter, orphans []string) bool {
	f.Progress(fmt.Sprintf("About to remove %d orphan state directory/directories:", len(orphans)))
	for _, n := range orphans {
		f.Progress("  " + n)
	}
	fmt.Fprint(os.Stderr, "Proceed? [y/N]: ")
	r := bufio.NewReader(pruneStdin)
	line, _ := r.ReadString('\n')
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes"
}

// runPrune executes `sd prune`.
// bug-no-prune-command
func runPrune(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")

	orphans, err := findOrphans(cmd)
	if err != nil {
		return ui.CLIError{Code: "prune_failed", Message: err.Error()}
	}

	// In JSON mode, --yes is required for actual removal. Treat absence as
	// dry-run to keep the command safe for automation.
	if f.JSONMode() && !yes {
		dryRun = true
	}

	result := pruneResult{Pruned: []string{}, Kept: []string{}, DryRun: dryRun}

	if len(orphans) == 0 {
		f.SuccessData(result, func() string { return "No orphan VM state directories found.\n" })
		return nil
	}

	// Dry-run: list and exit.
	if dryRun {
		result.Kept = append(result.Kept, orphans...)
		f.SuccessData(result, func() string {
			var b strings.Builder
			fmt.Fprintf(&b, "Found %d orphan state directory/directories (dry-run):\n", len(orphans))
			for _, n := range orphans {
				fmt.Fprintf(&b, "  %s\n", n)
			}
			fmt.Fprintf(&b, "Re-run with --yes to remove.\n")
			return b.String()
		})
		return nil
	}

	// Removal path. In human mode without --yes, ask for confirmation.
	if !yes && !f.JSONMode() {
		if !confirmPrune(f, orphans) {
			result.Kept = append(result.Kept, orphans...)
			f.SuccessData(result, func() string { return "Aborted; no directories removed.\n" })
			return nil
		}
	}

	l := Loader()
	for _, name := range orphans {
		if err := l.RemoveVMConfig(name); err != nil {
			// Track failures as "kept" so callers can see what remains.
			result.Kept = append(result.Kept, name)
			f.Warn(fmt.Sprintf("failed to remove state for %q: %v", name, err))
			continue
		}
		result.Pruned = append(result.Pruned, name)
		// bug-no-prune-command: audit every successful prune.
		if al := AuditLog(); al != nil {
			_ = al.LogEvent(security.EventLogEntry{
				Timestamp: time.Now().UTC(),
				EventType: "vm-prune",
				VMName:    name,
			})
		}
	}

	f.SuccessData(result, func() string {
		var b strings.Builder
		fmt.Fprintf(&b, "Pruned %d orphan state directory/directories.\n", len(result.Pruned))
		for _, n := range result.Pruned {
			fmt.Fprintf(&b, "  removed: %s\n", n)
		}
		for _, n := range result.Kept {
			fmt.Fprintf(&b, "  kept:    %s (removal failed)\n", n)
		}
		return b.String()
	})
	return nil
}

// orphanCount returns the number of orphan state directories. Exposed so
// `sd list` can hint at running `sd prune` without duplicating logic.
// bug-no-prune-command
func orphanCount(cmd *cobra.Command, knownVMs []backend.VMInfo) int {
	l := Loader()
	if l == nil {
		return 0
	}
	vmsDir := filepath.Join(l.SDHome(), "vms")
	entries, err := os.ReadDir(vmsDir)
	if err != nil {
		return 0
	}
	known := make(map[string]bool, len(knownVMs))
	for _, vm := range knownVMs {
		known[vm.Name] = true
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() && !known[e.Name()] {
			n++
		}
	}
	return n
}
