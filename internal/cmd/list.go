// Package cmd implements the list command.
// REQ-002-003: VM Management Commands -- list
// REQ-002-009: Command Aliases (list -> ls)
package cmd

import (
	"bytes"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"sd/internal/backend"
	"sd/internal/ui"
)

// getBackendFunc returns a backend by name, or the default backend if empty.
// Overridden in tests to inject mock backends.
var getBackendFunc = defaultGetBackend

func defaultGetBackend(name string) (backend.Backend, error) {
	if name == "" {
		return backend.Default()
	}
	return backend.Get(name)
}

func init() {
	listCmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all managed VMs",
		Long: `List all VMs managed by sd, displaying their name, status,
backend, resource allocation, and IP address (if available).`,
		GroupID: "vm",
		RunE:    runList,
	}
	rootCmd.AddCommand(listCmd)
}

// runList executes the list command.
// REQ-002-003
func runList(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	// Determine backend name from config
	var backendName string
	if l := Loader(); l != nil {
		cfg := l.Get()
		backendName = cfg.Defaults.Backend
	}

	b, err := getBackendFunc(backendName)
	if err != nil {
		return ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("no available backend: %v", err),
		}
	}

	vms, err := b.List(cmd.Context())
	if err != nil {
		return ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("failed to list VMs: %v", err),
		}
	}

	// Ensure non-nil slice for JSON serialization ([] not null)
	if vms == nil {
		vms = []backend.VMInfo{}
	}

	f.SuccessData(vms, func() string {
		return formatVMTable(vms)
	})

	return nil
}

// formatVMTable renders VM info as a human-readable table.
func formatVMTable(vms []backend.VMInfo) string {
	if len(vms) == 0 {
		return "No VMs found.\n"
	}

	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATUS\tBACKEND\tCPUS\tMEMORY\tDISK\tIP")
	for _, vm := range vms {
		ip := vm.IP
		if ip == "" {
			ip = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
			vm.Name, vm.Status, vm.Backend, vm.CPUs, vm.Memory, vm.Disk, ip)
	}
	w.Flush()
	return buf.String()
}
