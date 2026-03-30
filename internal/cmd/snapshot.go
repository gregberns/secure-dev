// Package cmd implements the snapshot command and its subcommands.
// REQ-002-003: VM Management Commands -- snapshot
package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"sd/internal/backend"
	"sd/internal/ui"
)

func init() {
	snapshotCmd := &cobra.Command{
		Use:   "snapshot <subcommand> <vm> [flags]",
		Short: "Manage VM snapshots",
		Long: `Create, list, restore, and delete VM snapshots.

Snapshots capture the complete state of a VM at a point in time,
allowing you to save and restore to known-good configurations.

Subcommands:
  create  <vm> --tag <tag>   Create a named snapshot
  list    <vm>               List snapshots for a VM
  restore <vm> --tag <tag>   Restore a VM to a snapshot
  delete  <vm> --tag <tag>   Delete a snapshot`,
		GroupID: "vm",
	}

	// --- snapshot create ---
	snapshotCreateCmd := &cobra.Command{
		Use:   "create <vm> --tag <tag>",
		Short: "Create a named snapshot",
		Long:  `Create a snapshot of the VM's current state with the given tag name.`,
		Args:  cobra.ExactArgs(1),
		RunE:  runSnapshotCreate,
	}
	snapshotCreateCmd.Flags().String("tag", "", "snapshot tag name (required)")
	snapshotCreateCmd.MarkFlagRequired("tag")

	// --- snapshot list ---
	snapshotListCmd := &cobra.Command{
		Use:   "list <vm>",
		Short: "List snapshots for a VM",
		Long:  `List all snapshots for the given VM, showing tag name, creation time, and size.`,
		Args:  cobra.ExactArgs(1),
		RunE:  runSnapshotList,
	}

	// --- snapshot restore ---
	snapshotRestoreCmd := &cobra.Command{
		Use:   "restore <vm> --tag <tag>",
		Short: "Restore a VM to a snapshot",
		Long:  `Restore the VM to the state captured by the named snapshot.`,
		Args:  cobra.ExactArgs(1),
		RunE:  runSnapshotRestore,
	}
	snapshotRestoreCmd.Flags().String("tag", "", "snapshot tag name (required)")
	snapshotRestoreCmd.MarkFlagRequired("tag")

	// --- snapshot delete ---
	snapshotDeleteCmd := &cobra.Command{
		Use:   "delete <vm> --tag <tag>",
		Short: "Delete a snapshot",
		Long:  `Delete the named snapshot from the VM.`,
		Args:  cobra.ExactArgs(1),
		RunE:  runSnapshotDelete,
	}
	snapshotDeleteCmd.Flags().String("tag", "", "snapshot tag name (required)")
	snapshotDeleteCmd.MarkFlagRequired("tag")

	snapshotCmd.AddCommand(snapshotCreateCmd, snapshotListCmd, snapshotRestoreCmd, snapshotDeleteCmd)
	rootCmd.AddCommand(snapshotCmd)
}

// resolveSnapshotBackend returns a backend that supports snapshots, or an error.
func resolveSnapshotBackend() (backend.Backend, backend.Snapshotter, error) {
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
		return nil, nil, ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("backend %q is not available: %v", backendName, err),
		}
	}

	if err := b.Available(); err != nil {
		return nil, nil, ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("backend %q not available: %v", backendName, err),
		}
	}

	s, ok := b.(backend.Snapshotter)
	if !ok {
		return nil, nil, ui.CLIError{
			Code:    "snapshot_not_supported",
			Message: fmt.Sprintf("backend %q does not support snapshots", backendName),
		}
	}

	return b, s, nil
}

// REQ-002-003: snapshot create
func runSnapshotCreate(cmd *cobra.Command, args []string) error {
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

	tag, _ := cmd.Flags().GetString("tag")
	if tag == "" {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: "--tag is required",
		}
	}

	_, s, err := resolveSnapshotBackend()
	if err != nil {
		return err
	}

	f.Progress(fmt.Sprintf("Creating snapshot %q for VM %q...", tag, name))

	if err := s.SnapshotCreate(cmd.Context(), name, tag); err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		return ui.CLIError{
			Code:    "snapshot_failed",
			Message: fmt.Sprintf("failed to create snapshot %q for VM %q: %v", tag, name, err),
		}
	}

	type snapshotCreateResult struct {
		VM  string `json:"vm"`
		Tag string `json:"tag"`
	}

	result := snapshotCreateResult{VM: name, Tag: tag}

	f.SuccessData(result, func() string {
		return fmt.Sprintf("Snapshot %q created for VM %q.\n", tag, name)
	})
	return nil
}

// REQ-002-003: snapshot list
func runSnapshotList(cmd *cobra.Command, args []string) error {
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

	_, s, err := resolveSnapshotBackend()
	if err != nil {
		return err
	}

	snaps, err := s.SnapshotList(cmd.Context(), name)
	if err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		return ui.CLIError{
			Code:    "snapshot_failed",
			Message: fmt.Sprintf("failed to list snapshots for VM %q: %v", name, err),
		}
	}

	if snaps == nil {
		snaps = []backend.SnapshotInfo{}
	}

	f.SuccessData(snaps, func() string {
		return formatSnapshotTable(snaps)
	})
	return nil
}

// REQ-002-003: snapshot restore
func runSnapshotRestore(cmd *cobra.Command, args []string) error {
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

	tag, _ := cmd.Flags().GetString("tag")
	if tag == "" {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: "--tag is required",
		}
	}

	_, s, err := resolveSnapshotBackend()
	if err != nil {
		return err
	}

	f.Progress(fmt.Sprintf("Restoring VM %q to snapshot %q...", name, tag))

	if err := s.SnapshotApply(cmd.Context(), name, tag); err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		if errors.Is(err, backend.ErrSnapshotNotFound) {
			return ui.CLIError{
				Code:    "snapshot_not_found",
				Message: fmt.Sprintf("snapshot %q does not exist for VM %q", tag, name),
			}
		}
		return ui.CLIError{
			Code:    "snapshot_failed",
			Message: fmt.Sprintf("failed to restore snapshot %q for VM %q: %v", tag, name, err),
		}
	}

	type snapshotRestoreResult struct {
		VM   string `json:"vm"`
		Tag  string `json:"tag"`
	}

	result := snapshotRestoreResult{VM: name, Tag: tag}

	f.SuccessData(result, func() string {
		return fmt.Sprintf("VM %q restored to snapshot %q.\n", name, tag)
	})
	return nil
}

// REQ-002-003: snapshot delete
func runSnapshotDelete(cmd *cobra.Command, args []string) error {
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

	tag, _ := cmd.Flags().GetString("tag")
	if tag == "" {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: "--tag is required",
		}
	}

	_, s, err := resolveSnapshotBackend()
	if err != nil {
		return err
	}

	f.Progress(fmt.Sprintf("Deleting snapshot %q from VM %q...", tag, name))

	if err := s.SnapshotDelete(cmd.Context(), name, tag); err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		if errors.Is(err, backend.ErrSnapshotNotFound) {
			return ui.CLIError{
				Code:    "snapshot_not_found",
				Message: fmt.Sprintf("snapshot %q does not exist for VM %q", tag, name),
			}
		}
		return ui.CLIError{
			Code:    "snapshot_failed",
			Message: fmt.Sprintf("failed to delete snapshot %q from VM %q: %v", tag, name, err),
		}
	}

	type snapshotDeleteResult struct {
		VM  string `json:"vm"`
		Tag string `json:"tag"`
	}

	result := snapshotDeleteResult{VM: name, Tag: tag}

	f.SuccessData(result, func() string {
		return fmt.Sprintf("Snapshot %q deleted from VM %q.\n", tag, name)
	})
	return nil
}

// formatSnapshotTable renders snapshot info as a human-readable table.
func formatSnapshotTable(snaps []backend.SnapshotInfo) string {
	if len(snaps) == 0 {
		return "No snapshots found.\n"
	}

	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TAG\tCREATED\tSIZE")
	for _, snap := range snaps {
		fmt.Fprintf(w, "%s\t%s\t%d\n",
			snap.Name,
			snap.CreatedAt.Format("2006-01-02 15:04:05"),
			snap.Size,
		)
	}
	w.Flush()
	return buf.String()
}
