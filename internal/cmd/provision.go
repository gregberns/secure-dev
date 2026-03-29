// Package cmd implements the provision command and its subcommands.
// REQ-006-001: Built-in module listing.
// REQ-006-010: Re-provisioning existing VMs.
package cmd

import (
	"errors"
	"fmt"
	"strings"

	"context"

	"github.com/spf13/cobra"
	"sd/internal/backend"
	"sd/internal/provision"
	"sd/internal/ui"
)

// loadBuiltinModules loads built-in provisioning modules.
// Overridden in tests with a digital twin.
var loadBuiltinModules = provision.LoadBuiltinModules

func init() {
	provisionCmd := &cobra.Command{
		Use:   "provision <vm> [--modules <list>]",
		Short: "Provision or re-provision a VM",
		Long: `Provision a VM with development tools and agent configurations.

If no modules are specified, all configured modules are provisioned.
Use --modules to provision only specific modules (plus their dependencies).

Subcommands:
  list              List available provisioning modules`,
		GroupID: "provisioning",
		RunE:    runProvision,
	}
	provisionCmd.Flags().String("modules", "", "comma-separated list of modules to provision")

	// --- provision list ---
	provisionListCmd := &cobra.Command{
		Use:   "list",
		Short: "List available provisioning modules",
		Long:  `Display all built-in and custom provisioning modules with descriptions and dependencies.`,
		Args:  cobra.NoArgs,
		RunE:  runProvisionList,
	}

	provisionCmd.AddCommand(provisionListCmd)
	rootCmd.AddCommand(provisionCmd)
}

// runProvision executes the provision command.
// REQ-006-010: Re-provision existing VMs.
func runProvision(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	if len(args) == 0 {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: "VM name is required",
		}
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

	// REQ-006-010: VM must be running for provisioning
	status, err := b.Status(cmd.Context(), name)
	if err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		return ui.CLIError{
			Code:    "provision_failed",
			Message: fmt.Sprintf("failed to check status of VM %q: %v", name, err),
		}
	}

	if status != backend.StatusRunning {
		return ui.CLIError{
			Code:    "vm_not_running",
			Message: fmt.Sprintf("VM %q is not running (status: %s). Start it with \"sd start %s\".", name, status, name),
		}
	}

	// Load available modules
	allModules, err := loadBuiltinModules()
	if err != nil {
		return ui.CLIError{
			Code:    "provision_failed",
			Message: fmt.Sprintf("failed to load modules: %v", err),
		}
	}

	// Resolve which modules to run
	modulesFlag, _ := cmd.Flags().GetString("modules")
	var resolved []provision.Module
	if modulesFlag != "" {
		requested := strings.Split(modulesFlag, ",")
		// Trim whitespace
		for i := range requested {
			requested[i] = strings.TrimSpace(requested[i])
		}
		resolved, err = provision.ResolveRequested(allModules, requested)
	} else {
		resolved, err = provision.ResolveAll(allModules)
	}
	if err != nil {
		return ui.CLIError{
			Code:    "provision_failed",
			Message: err.Error(),
		}
	}

	f.Progress(fmt.Sprintf("Provisioning VM %q with %d module(s)...", name, len(resolved)))

	// Execute provisioning via backend Exec
	execFn := func(ctx context.Context, vmName string, command []string) (string, string, int, error) {
		result, execErr := b.Exec(ctx, vmName, command)
		if execErr != nil {
			return "", "", 0, execErr
		}
		return result.Stdout, result.Stderr, result.ExitCode, nil
	}

	result := provision.Provision(cmd.Context(), execFn, name, resolved)
	if result.Failed {
		return ui.CLIError{
			Code:    "provision_script_failed",
			Message: fmt.Sprintf("module %q failed: %s", result.Module, result.Error),
		}
	}

	type provisionSuccess struct {
		VM          string                       `json:"vm"`
		Modules     []provision.ModuleExecutionStatus `json:"modules"`
	}

	successData := provisionSuccess{
		VM:      name,
		Modules: result.State.Modules,
	}

	f.SuccessData(successData, func() string {
		var completed []string
		for _, m := range result.State.Modules {
			if m.Status == provision.StatusCompleted {
				completed = append(completed, m.Name)
			}
		}
		return fmt.Sprintf("Provisioned VM %q with modules: %s\n", name, strings.Join(completed, ", "))
	})
	return nil
}

// runProvisionList lists available modules.
// REQ-006-001: sd provision list displays all built-in modules.
func runProvisionList(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	modules, err := loadBuiltinModules()
	if err != nil {
		return ui.CLIError{
			Code:    "provision_failed",
			Message: fmt.Sprintf("failed to load modules: %v", err),
		}
	}

	entries := make([]provision.ModuleListEntry, len(modules))
	for i, m := range modules {
		entries[i] = provision.ModuleListEntry{
			Name:        m.Name,
			Description: m.Description,
			DependsOn:   m.DependsOn,
			Probe:       m.Probe != nil,
		}
	}

	f.SuccessData(entries, func() string {
		return provision.FormatModuleList(modules)
	})
	return nil
}
