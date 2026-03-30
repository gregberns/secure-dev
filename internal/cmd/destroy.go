// Package cmd implements the destroy command.
// REQ-002-003: VM Management Commands -- destroy
package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"sd/internal/backend"
	"sd/internal/ssh"
	"sd/internal/ui"
)

// removeSSHDir removes the SSH directory for a VM (keys, known_hosts, etc.).
// Overridden in tests with a digital twin.
var removeSSHDir = ssh.RemoveSSHDir

// autoSnapshotTag generates a timestamp-based snapshot tag for pre-destroy snapshots.
// REQ-004-019
var autoSnapshotTag = func(name string) string {
	return fmt.Sprintf("pre-destroy-%s", time.Now().Format("20060102-150405"))
}

func init() {
	destroyCmd := &cobra.Command{
		Use:   "destroy <name>",
		Short: "Destroy a VM and its resources",
		Long: `Destroy a VM and all its associated resources (disk, configuration,
snapshots). This operation is irreversible.

Requires --force (-f) flag for non-interactive use.
A safety snapshot is automatically created before destruction (REQ-004-019).
Use --no-snapshot to skip the safety snapshot.`,
		GroupID: "vm",
		Args:    cobra.ExactArgs(1),
		RunE:    runDestroy,
	}

	// REQ-002-003: destroy flags
	destroyCmd.Flags().BoolP("force", "f", false, "skip confirmation (required for non-interactive use)")
	// REQ-004-019: skip auto-snapshot before destroy
	destroyCmd.Flags().Bool("no-snapshot", false, "skip automatic safety snapshot before destruction")

	rootCmd.AddCommand(destroyCmd)
}

// runDestroy executes the destroy command.
// REQ-002-003, REQ-004-019
func runDestroy(cmd *cobra.Command, args []string) error {
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

	// REQ-001-006: validate VM name format
	if err := backend.ValidateVMName(name); err != nil {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: err.Error(),
		}
	}

	// REQ-002-003: --force is required for non-interactive use
	force, _ := cmd.Flags().GetBool("force")
	if !force {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: fmt.Sprintf("--force (-f) is required to destroy VM %q (non-interactive mode)", name),
		}
	}

	// Resolve backend name: config > default ("lima")
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

	// Check backend availability
	if err := b.Available(); err != nil {
		return ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("backend %q not available: %v", backendName, err),
		}
	}

	// REQ-004-019: Auto-snapshot before destructive operations
	noSnapshot, _ := cmd.Flags().GetBool("no-snapshot")
	snapshotTag := ""
	if !noSnapshot {
		if s, ok := b.(backend.Snapshotter); ok {
			snapshotTag = autoSnapshotTag(name)
			f.Progress(fmt.Sprintf("Creating safety snapshot %q for VM %q...", snapshotTag, name))
			if err := s.SnapshotCreate(cmd.Context(), name, snapshotTag); err != nil {
				// Snapshot failure is a non-fatal warning; proceed with destroy
				f.Progress(fmt.Sprintf("Warning: failed to create safety snapshot: %v", err))
				snapshotTag = ""
			}
		}
	}

	f.Progress(fmt.Sprintf("Destroying VM %q...", name))

	if err := b.Destroy(cmd.Context(), name); err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		return ui.CLIError{
			Code:    "vm_destroy_failed",
			Message: fmt.Sprintf("failed to destroy VM %q: %v", name, err),
		}
	}

	// REQ-004-031: Clean up SSH keys and known_hosts
	if l := Loader(); l != nil {
		sdHome := l.SDHome()
		if err := removeSSHDir(sdHome, name); err != nil {
			f.Progress(fmt.Sprintf("Warning: failed to clean up SSH directory: %v", err))
		}
	}

	type destroyResult struct {
		Name        string `json:"name"`
		SnapshotTag string `json:"snapshot_tag,omitempty"` // REQ-004-019
	}

	result := destroyResult{Name: name, SnapshotTag: snapshotTag}

	f.SuccessData(result, func() string {
		msg := fmt.Sprintf("VM %q destroyed.", name)
		if snapshotTag != "" {
			msg += fmt.Sprintf(" Safety snapshot %q created.", snapshotTag)
		}
		return msg + "\n"
	})
	return nil
}
