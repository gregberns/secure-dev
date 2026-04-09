// Package cmd implements the status command.
// REQ-002-003: VM Management Commands -- status
package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"sd/internal/backend"
	"sd/internal/ui"
)

func init() {
	statusCmd := &cobra.Command{
		Use:   "status [name]",
		Short: "Show status of one or all VMs",
		Long: `Show the current status of a specific VM or all managed VMs.

When called without arguments, displays a summary of all VMs.
When called with a VM name, displays detailed status information.`,
		GroupID: "vm",
		Args:    cobra.MaximumNArgs(1),
		RunE:    runStatus,
	}

	rootCmd.AddCommand(statusCmd)
}

// runStatus executes the status command.
// REQ-002-003
func runStatus(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	// Resolve backend: use per-VM config for named VM, global default otherwise.
	var backendName string
	if len(args) > 0 && args[0] != "" {
		backendName = resolveBackendName(args[0])
	} else {
		if Loader() != nil {
			cfg := Loader().Get()
			backendName = cfg.Defaults.Backend
		}
		if backendName == "" {
			backendName = "lima"
		}
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

	// Determine if we're querying a specific VM or all VMs
	if len(args) > 0 && args[0] != "" {
		return showSingleVMStatus(cmd, b, args[0], f)
	}
	return showAllVMStatus(cmd, b, f)
}

// showSingleVMStatus displays detailed status for a named VM.
func showSingleVMStatus(cmd *cobra.Command, b backend.Backend, name string, f *ui.Formatter) error {
	// Use Status first to check existence and get status
	status, err := b.Status(cmd.Context(), name)
	if err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		return ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("failed to get status for VM %q: %v", name, err),
		}
	}

	// Get full VM info from List for detailed display
	vms, listErr := b.List(cmd.Context())
	var vmInfo *backend.VMInfo
	if listErr == nil {
		for i := range vms {
			if vms[i].Name == name {
				vmInfo = &vms[i]
				break
			}
		}
	}

	// REQ-006-009: Show provisioning state if available
	var provisions []string
	if l := Loader(); l != nil {
		if vmCfg, err := l.ReadVMConfig(name); err == nil && len(vmCfg.Provisions) > 0 {
			provisions = vmCfg.Provisions
		}
	}

	// If we have full VMInfo, use it; otherwise build from Status result
	if vmInfo != nil {
		type vmInfoWithProvisions struct {
			backend.VMInfo
			Provisions []string `json:"provisions,omitempty"`
		}
		result := vmInfoWithProvisions{VMInfo: *vmInfo, Provisions: provisions}
		f.SuccessData(result, func() string {
			out := formatVMDetail(*vmInfo)
			if len(provisions) > 0 {
				out += fmt.Sprintf("Modules: %s\n", strings.Join(provisions, ", "))
			}
			return out
		})
	} else {
		// Fallback: only have name and status
		type statusInfo struct {
			Name   string             `json:"name"`
			Status backend.VMStatus  `json:"status"`
		}
		info := statusInfo{Name: name, Status: status}
		f.SuccessData(info, func() string {
			return fmt.Sprintf("Name:   %s\nStatus: %s\n", name, status)
		})
	}

	return nil
}

// showAllVMStatus displays a status summary for all VMs.
func showAllVMStatus(cmd *cobra.Command, b backend.Backend, f *ui.Formatter) error {
	vms, err := b.List(cmd.Context())
	if err != nil {
		return ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("failed to list VMs: %v", err),
		}
	}

	// Ensure non-nil slice for JSON serialization
	if vms == nil {
		vms = []backend.VMInfo{}
	}

	// Build status-only view for all VMs
	type vmStatus struct {
		Name   string            `json:"name"`
		Status backend.VMStatus `json:"status"`
	}

	statuses := make([]vmStatus, len(vms))
	for i, vm := range vms {
		statuses[i] = vmStatus{Name: vm.Name, Status: vm.Status}
	}

	f.SuccessData(statuses, func() string {
		return formatStatusTable(vms)
	})

	return nil
}

// formatVMDetail renders detailed VM information as human-readable key-value pairs.
func formatVMDetail(vm backend.VMInfo) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Name:     %s\n", vm.Name)
	fmt.Fprintf(&buf, "Status:   %s\n", vm.Status)
	fmt.Fprintf(&buf, "Backend:  %s\n", vm.Backend)
	fmt.Fprintf(&buf, "CPUs:     %d\n", vm.CPUs)
	fmt.Fprintf(&buf, "Memory:   %s\n", vm.Memory)
	fmt.Fprintf(&buf, "Disk:     %s\n", vm.Disk)
	if vm.IP != "" {
		fmt.Fprintf(&buf, "IP:       %s\n", vm.IP)
	}
	if !vm.CreatedAt.IsZero() {
		fmt.Fprintf(&buf, "Created:  %s\n", vm.CreatedAt.Format(time.RFC3339))
	}
	return buf.String()
}

// formatStatusTable renders all VMs as a human-readable name/status table.
func formatStatusTable(vms []backend.VMInfo) string {
	if len(vms) == 0 {
		return "No VMs found.\n"
	}

	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATUS")
	for _, vm := range vms {
		fmt.Fprintf(w, "%s\t%s\n", vm.Name, vm.Status)
	}
	w.Flush()
	return buf.String()
}
