// Package cmd implements the snapshot command and its subcommands.
// REQ-002-003: VM Management Commands -- snapshot
package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"sd/internal/backend"
	"sd/internal/security"
	"sd/internal/ui"
)

// preRestoreSnapshotTag generates the backup snapshot tag created before
// `sd snapshot restore` applies a snapshot. Overridable in tests.
// REQ-004-019: backup snapshot before destructive (restore) operation.
var preRestoreSnapshotTag = func(name string) string {
	return fmt.Sprintf("pre-restore-%s", time.Now().Format("20060102-150405"))
}

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
		Args:  exactArgs(1, "<vm> --tag <tag>"),
		RunE:  runSnapshotCreate,
	}
	snapshotCreateCmd.Flags().String("tag", "", "snapshot tag name (required)")
	snapshotCreateCmd.MarkFlagRequired("tag")

	// --- snapshot list ---
	snapshotListCmd := &cobra.Command{
		Use:   "list <vm>",
		Short: "List snapshots for a VM",
		Long:  `List all snapshots for the given VM, showing tag name, creation time, and size.`,
		Args:  exactArgs(1, "<vm>"),
		RunE:  runSnapshotList,
	}

	// --- snapshot restore ---
	snapshotRestoreCmd := &cobra.Command{
		Use:   "restore <vm> --tag <tag>",
		Short: "Restore a VM to a snapshot",
		Long:  `Restore the VM to the state captured by the named snapshot.`,
		Args:  exactArgs(1, "<vm> --tag <tag>"),
		RunE:  runSnapshotRestore,
	}
	snapshotRestoreCmd.Flags().String("tag", "", "snapshot tag name (required)")
	snapshotRestoreCmd.MarkFlagRequired("tag")
	// REQ-004-019: opt-out for the pre-restore backup snapshot. Default false
	// (i.e., a backup snapshot is created before applying the restore). The
	// canonical flag name across all destructive operations is --no-snapshot
	// (matches `sd destroy` and `sd provision`).
	snapshotRestoreCmd.Flags().Bool("no-snapshot", false, "skip the pre-restore backup snapshot (not recommended)")

	// --- snapshot delete ---
	snapshotDeleteCmd := &cobra.Command{
		Use:   "delete <vm> --tag <tag>",
		Short: "Delete a snapshot",
		Long:  `Delete the named snapshot from the VM.`,
		Args:  exactArgs(1, "<vm> --tag <tag>"),
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

	createErr := s.SnapshotCreate(cmd.Context(), name, tag)
	// REQ-004-022, Q1: standalone `sd snapshot create` MUST emit a
	// snapshot-create audit event (success or snapshot-create-failed).
	if al := AuditLog(); al != nil {
		meta := map[string]string{
			"tag":       tag,
			"operation": "snapshot-create",
		}
		eventType := "snapshot-create"
		if createErr != nil {
			meta["error"] = createErr.Error()
			eventType = "snapshot-create-failed"
		}
		_ = al.LogEvent(security.EventLogEntry{
			Timestamp: time.Now().UTC(),
			EventType: eventType,
			VMName:    name,
			Metadata:  meta,
		})
	}
	if createErr != nil {
		if errors.Is(createErr, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		return ui.CLIError{
			Code:    "snapshot_failed",
			Message: fmt.Sprintf("failed to create snapshot %q for VM %q: %v", tag, name, createErr),
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

	// REQ-004-019: create a backup snapshot before applying the restore so the
	// user can undo. Failure is fatal unless --no-snapshot was passed.
	noBackup, _ := cmd.Flags().GetBool("no-snapshot")
	backupTag := ""
	if !noBackup {
		backupTag = preRestoreSnapshotTag(name)
		f.Progress(fmt.Sprintf("Creating backup snapshot %q for VM %q before restore...", backupTag, name))
		backupErr := s.SnapshotCreate(cmd.Context(), name, backupTag)
		// REQ-004-022: audit backup-snapshot outcome.
		if al := AuditLog(); al != nil {
			meta := map[string]string{
				"tag":       backupTag,
				"operation": "snapshot-restore",
			}
			eventType := "snapshot-create"
			if backupErr != nil {
				meta["error"] = backupErr.Error()
				eventType = "snapshot-create-failed"
			}
			_ = al.LogEvent(security.EventLogEntry{
				Timestamp: time.Now().UTC(),
				EventType: eventType,
				VMName:    name,
				Metadata:  meta,
			})
		}
		if backupErr != nil {
			if errors.Is(backupErr, backend.ErrVMNotFound) {
				return ui.CLIError{
					Code:    "vm_not_found",
					Message: fmt.Sprintf("VM %q does not exist", name),
				}
			}
			// REQ-004-019: restore MUST NOT proceed if backup snapshot fails.
			return ui.CLIError{
				Code: "snapshot_failed",
				Message: fmt.Sprintf(
					"failed to create backup snapshot before restore: %v. The restore operation has been aborted. Free disk space and retry, or use --no-snapshot to skip (not recommended).",
					backupErr),
			}
		}
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

	// REQ-004-022: log the snapshot-restore event.
	if al := AuditLog(); al != nil {
		meta := map[string]string{"tag": tag}
		if backupTag != "" {
			meta["backup_tag"] = backupTag
		}
		_ = al.LogEvent(security.EventLogEntry{
			Timestamp: time.Now().UTC(),
			EventType: "snapshot-restore",
			VMName:    name,
			Metadata:  meta,
		})
	}

	type snapshotRestoreResult struct {
		VM        string `json:"vm"`
		Tag       string `json:"tag"`
		BackupTag string `json:"backup_tag,omitempty"` // REQ-004-019
	}

	result := snapshotRestoreResult{VM: name, Tag: tag, BackupTag: backupTag}

	f.SuccessData(result, func() string {
		msg := fmt.Sprintf("VM %q restored to snapshot %q.", name, tag)
		if backupTag != "" {
			msg += fmt.Sprintf(" Backup snapshot %q created.", backupTag)
		}
		return msg + "\n"
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
