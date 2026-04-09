// Package cmd implements the list command.
// REQ-002-003: VM Management Commands -- list
// REQ-002-009: Command Aliases (list -> ls)
package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
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

// resolveBackendName returns the backend name for a specific VM by reading
// its per-VM config from $SD_HOME/vms/<name>/config.yaml. Falls back to the
// global default backend if no per-VM config exists or the backend field is empty.
func resolveBackendName(vmName string) string {
	if l := Loader(); l != nil {
		if vmCfg, err := l.ReadVMConfig(vmName); err == nil && vmCfg.Backend != "" {
			return vmCfg.Backend
		}
		// Fall back to global default
		cfg := l.Get()
		if cfg.Defaults.Backend != "" {
			return cfg.Defaults.Backend
		}
	}
	return "lima"
}

// allBackendNames returns the names of all registered backends.
// Overridden in tests to control which backends are queried.
var allBackendNames = backend.List

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

	// Query all registered backends and merge results.
	var vms []backend.VMInfo
	seen := make(map[string]bool)

	for _, name := range allBackendNames() {
		b, err := getBackendFunc(name)
		if err != nil {
			continue // skip backends that can't be loaded
		}
		if err := b.Available(); err != nil {
			continue // skip unavailable backends
		}
		bvms, err := b.List(cmd.Context())
		if err != nil {
			f.Progress(fmt.Sprintf("Warning: failed to list VMs from backend %q: %v", name, err))
			continue
		}
		for _, vm := range bvms {
			if !seen[vm.Name] {
				seen[vm.Name] = true
				vms = append(vms, vm)
			}
		}
	}

	// Ensure non-nil slice for JSON serialization ([] not null)
	if vms == nil {
		vms = []backend.VMInfo{}
	}

	// REQ-001-011: Detect orphaned VM state
	if l := Loader(); l != nil {
		sdHome := l.SDHome()
		vmsDir := filepath.Join(sdHome, "vms")
		if entries, err := os.ReadDir(vmsDir); err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				vmName := entry.Name()
				found := false
				for _, vm := range vms {
					if vm.Name == vmName {
						found = true
						break
					}
				}
				if !found {
					f.Progress(fmt.Sprintf("Warning: orphaned VM state for %q in %s (no matching VM in backend)", vmName, vmsDir))
				}
			}
		}
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
