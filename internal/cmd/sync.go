// Package cmd implements the sync command.
// REQ-007-015: Sync To VM
// REQ-007-016: Sync From VM
// REQ-007-017: Sync Diff Preview
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"sd/internal/backend"
	"sd/internal/ui"
)

func init() {
	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync files between host and VM",
		Long: `Synchronize files between the host and a VM using rsync over SSH.

Supports host-to-VM ("sync to") and VM-to-host ("sync from") directions.
If the destination path is omitted, it defaults to a sensible location
based on the source path basename.`,
		GroupID: "connection",
	}

	syncToCmd := &cobra.Command{
		Use:   "to <vm> <host-path> [<guest-path>]",
		Short: "Sync files from host to VM",
		Long: `Copy files from the host into the VM using rsync over SSH.

If <guest-path> is omitted, files are copied to ~/<basename of host-path>
inside the VM. Symlinks, permissions, and timestamps are preserved.`,
		Args: cobra.RangeArgs(2, 3),
		RunE: runSyncTo,
	}
	syncToCmd.Flags().Bool("watch", false, "Watch for changes and sync continuously (host-to-VM only)")

	syncFromCmd := &cobra.Command{
		Use:   "from <vm> <guest-path> [<host-path>]",
		Short: "Sync files from VM to host",
		Long: `Copy files from the VM to the host using rsync over SSH.

If <host-path> is omitted, files are copied to ./<basename of guest-path>
on the host.`,
		Args: cobra.RangeArgs(2, 3),
		RunE: runSyncFrom,
	}
	syncFromCmd.Flags().Bool("diff", false, "Preview differences without copying")

	syncCmd.AddCommand(syncToCmd, syncFromCmd)
	rootCmd.AddCommand(syncCmd)
}

// runSyncTo implements "sd sync to".
// REQ-007-015
func runSyncTo(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	name := args[0]
	hostPath := args[1]
	guestPath := ""
	if len(args) == 3 {
		guestPath = args[2]
	}

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

	if hostPath == "" {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: "host path must not be empty",
		}
	}

	// Default guest path: ~/<basename of host-path>
	if guestPath == "" {
		guestPath = "~/" + filepath.Base(hostPath)
	}

	b, err := resolveSyncBackend()
	if err != nil {
		return err
	}

	// Check backend implements Syncer
	syncer, ok := b.(backend.Syncer)
	if !ok {
		return ui.CLIError{
			Code:    "sync_not_supported",
			Message: fmt.Sprintf("backend %q does not support file sync", b.Name()),
		}
	}

	// Verify VM is running
	if err := requireVMRunning(cmd.Context(), b, name); err != nil {
		return err
	}

	// REQ-007-015: Sync host -> guest
	if err := syncer.SyncTo(cmd.Context(), name, hostPath, guestPath); err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		if errors.Is(err, backend.ErrVMNotRunning) {
			return ui.CLIError{
				Code:    "vm_not_running",
				Message: fmt.Sprintf("VM %q is not running. Start it with \"sd start %s\".", name, name),
			}
		}
		return ui.CLIError{
			Code:    "sync_failed",
			Message: fmt.Sprintf("failed to sync to VM %q: %v", name, err),
		}
	}

	if f.JSONMode() {
		type syncResult struct {
			Name      string `json:"name"`
			Direction string `json:"direction"`
			HostPath  string `json:"host_path"`
			GuestPath string `json:"guest_path"`
		}
		f.SuccessData(syncResult{
			Name:      name,
			Direction: "to",
			HostPath:  hostPath,
			GuestPath: guestPath,
		}, nil)
	} else {
		f.Progress(fmt.Sprintf("Syncing %s -> %s:%s...", hostPath, name, guestPath))
		fmt.Fprintf(os.Stdout, "Synced %s -> %s:%s\n", hostPath, name, guestPath)
	}

	return nil
}

// runSyncFrom implements "sd sync from".
// REQ-007-016, REQ-007-017
func runSyncFrom(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	name := args[0]
	guestPath := args[1]
	hostPath := ""
	if len(args) == 3 {
		hostPath = args[2]
	}

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

	if guestPath == "" {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: "guest path must not be empty",
		}
	}

	// Default host path: ./<basename of guest-path>
	if hostPath == "" {
		hostPath = "./" + filepath.Base(guestPath)
	}

	diff, _ := cmd.Flags().GetBool("diff")

	b, err := resolveSyncBackend()
	if err != nil {
		return err
	}

	// Check backend implements Syncer
	syncer, ok := b.(backend.Syncer)
	if !ok {
		return ui.CLIError{
			Code:    "sync_not_supported",
			Message: fmt.Sprintf("backend %q does not support file sync", b.Name()),
		}
	}

	// Verify VM is running
	if err := requireVMRunning(cmd.Context(), b, name); err != nil {
		return err
	}

	// REQ-007-017: --diff previews without copying
	if diff {
		diffOutput, err := syncer.SyncDiff(cmd.Context(), name, guestPath, hostPath)
		if err != nil {
			if errors.Is(err, backend.ErrVMNotFound) {
				return ui.CLIError{
					Code:    "vm_not_found",
					Message: fmt.Sprintf("VM %q does not exist", name),
				}
			}
			return ui.CLIError{
				Code:    "sync_failed",
				Message: fmt.Sprintf("failed to diff with VM %q: %v", name, err),
			}
		}

		if f.JSONMode() {
			type diffResult struct {
				Name      string `json:"name"`
				HostPath  string `json:"host_path"`
				GuestPath string `json:"guest_path"`
				Diff      string `json:"diff"`
			}
			f.SuccessData(diffResult{
				Name:      name,
				HostPath:  hostPath,
				GuestPath: guestPath,
				Diff:      diffOutput,
			}, nil)
		} else {
			fmt.Fprint(os.Stdout, diffOutput)
		}
		return nil
	}

	// REQ-007-016: Sync guest -> host
	if err := syncer.SyncFrom(cmd.Context(), name, guestPath, hostPath); err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		if errors.Is(err, backend.ErrVMNotRunning) {
			return ui.CLIError{
				Code:    "vm_not_running",
				Message: fmt.Sprintf("VM %q is not running. Start it with \"sd start %s\".", name, name),
			}
		}
		return ui.CLIError{
			Code:    "sync_failed",
			Message: fmt.Sprintf("failed to sync from VM %q: %v", name, err),
		}
	}

	if f.JSONMode() {
		type syncResult struct {
			Name      string `json:"name"`
			Direction string `json:"direction"`
			HostPath  string `json:"host_path"`
			GuestPath string `json:"guest_path"`
		}
		f.SuccessData(syncResult{
			Name:      name,
			Direction: "from",
			HostPath:  hostPath,
			GuestPath: guestPath,
		}, nil)
	} else {
		f.Progress(fmt.Sprintf("Syncing %s:%s -> %s...", name, guestPath, hostPath))
		fmt.Fprintf(os.Stdout, "Synced %s:%s -> %s\n", name, guestPath, hostPath)
	}

	return nil
}

// resolveSyncBackend resolves the backend for sync operations.
func resolveSyncBackend() (backend.Backend, error) {
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
		return nil, ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("backend %q is not available: %v", backendName, err),
		}
	}

	if err := b.Available(); err != nil {
		return nil, ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("backend %q not available: %v", backendName, err),
		}
	}

	return b, nil
}

// requireVMRunning checks that the VM is running and returns an appropriate error if not.
func requireVMRunning(ctx context.Context, b backend.Backend, name string) error {
	status, err := b.Status(ctx, name)
	if err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		return ui.CLIError{
			Code:    "sync_failed",
			Message: fmt.Sprintf("failed to check status of VM %q: %v", name, err),
		}
	}

	if status != backend.StatusRunning {
		return ui.CLIError{
			Code:    "vm_not_running",
			Message: fmt.Sprintf("VM %q is not running (status: %s). Start it with \"sd start %s\".", name, status, name),
		}
	}

	return nil
}
