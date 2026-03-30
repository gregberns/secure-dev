// Package cmd implements the config egress subcommands.
// REQ-002-005: Configuration Commands -- Egress Management
// REQ-004-008: User-defined Egress Allowlist
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"sd/internal/backend"
	"sd/internal/config"
	"sd/internal/security"
	"sd/internal/ui"
)

// readVMEgressFunc reads the egress_allowlist from a VM's config file.
// Overridden in tests with a digital twin.
var readVMEgressFunc = defaultReadVMEgress

// writeVMEgressFunc writes the egress_allowlist to a VM's config file.
// Overridden in tests with a digital twin.
var writeVMEgressFunc = defaultWriteVMEgress

func defaultReadVMEgress(sdHome, vmName string) ([]string, error) {
	path := filepath.Join(sdHome, "vms", vmName, "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read VM config: %w", err)
	}
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(string(data))); err != nil {
		return nil, fmt.Errorf("cannot parse VM config: %w", err)
	}
	list := v.GetStringSlice("egress_allowlist")
	if list == nil {
		return nil, nil
	}
	return list, nil
}

func defaultWriteVMEgress(sdHome, vmName string, allowlist []string) error {
	dir := filepath.Join(sdHome, "vms", vmName)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create VM config directory: %w", err)
	}

	path := filepath.Join(dir, "config.yaml")
	v := viper.New()
	v.SetConfigType("yaml")

	// Load existing config to preserve other fields
	if data, err := os.ReadFile(path); err == nil {
		v.ReadConfig(strings.NewReader(string(data)))
	}

	if len(allowlist) == 0 {
		// Remove the key if empty
		// viper doesn't have a clean "unset", so we marshal manually
		type vmCfg struct {
			EgressAllowlist []string               `yaml:"egress_allowlist,omitempty"`
			Env             map[string]string      `yaml:"env,omitempty"`
			Backend         string                 `yaml:"backend,omitempty"`
			CPUs            int                    `yaml:"cpus,omitempty"`
			Memory          string                 `yaml:"memory,omitempty"`
			Disk            string                 `yaml:"disk,omitempty"`
			Image           string                 `yaml:"image,omitempty"`
			Provisions      []string               `yaml:"provisions,omitempty"`
			State           map[string]any         `yaml:"state,omitempty"`
			BackendMeta     map[string]any         `yaml:"backend_meta,omitempty"`
		}
		cfg := vmCfg{}
		// Re-read into a clean structure to drop the egress field
		rawData, _ := os.ReadFile(path)
		if rawData != nil {
			v2 := viper.New()
			v2.SetConfigType("yaml")
			v2.ReadConfig(strings.NewReader(string(rawData)))
			cfg.Env = v2.GetStringMapString("env")
			cfg.Backend = v2.GetString("backend")
			cfg.CPUs = v2.GetInt("cpus")
			cfg.Memory = v2.GetString("memory")
			cfg.Disk = v2.GetString("disk")
			cfg.Image = v2.GetString("image")
			cfg.Provisions = v2.GetStringSlice("provisions")
		}
		// Don't set EgressAllowlist (omitted)
		out, err := json.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("cannot marshal VM config: %w", err)
		}
		if err := os.WriteFile(path, out, 0600); err != nil {
			return fmt.Errorf("cannot write VM config: %w", err)
		}
		return nil
	}

	v.Set("egress_allowlist", allowlist)
	if err := v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("cannot write VM config: %w", err)
	}
	os.Chmod(path, 0600)
	return nil
}

func init() {
	// REQ-002-005: sd config egress parent command
	configEgressCmd := &cobra.Command{
		Use:   "egress <subcommand>",
		Short: "Manage per-VM egress allowlist",
		Long: `Manage the egress (outbound network) allowlist for a VM.

Subcommands:
  add <vm> <domain>      Add a domain to the VM's egress allowlist
  remove <vm> <domain>   Remove a domain from the VM's egress allowlist
  list <vm>              List the effective egress allowlist for a VM`,
	}

	// REQ-004-008: sd config egress add
	configEgressAddCmd := &cobra.Command{
		Use:   "add <vm> <domain>",
		Short: "Add a domain to VM egress allowlist",
		Long:  `Add a domain to the VM's egress allowlist. The domain is merged with the default allowlist.`,
		Args:  cobra.ExactArgs(2),
		RunE:  runConfigEgressAdd,
	}

	// REQ-004-008: sd config egress remove
	configEgressRemoveCmd := &cobra.Command{
		Use:   "remove <vm> <domain>",
		Short: "Remove a domain from VM egress allowlist",
		Long:  `Remove a domain from the VM's egress allowlist. Default domains cannot be removed.`,
		Args:  cobra.ExactArgs(2),
		RunE:  runConfigEgressRemove,
	}

	// REQ-002-005: sd config egress list
	configEgressListCmd := &cobra.Command{
		Use:   "list <vm>",
		Short: "List effective egress allowlist for a VM",
		Long:  `List all allowed outbound destinations for a VM, distinguishing default from user-added domains.`,
		Args:  cobra.ExactArgs(1),
		RunE:  runConfigEgressList,
	}

	configEgressCmd.AddCommand(configEgressAddCmd, configEgressRemoveCmd, configEgressListCmd)

	// Find the config command and add egress as a subcommand
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "config" {
			cmd.AddCommand(configEgressCmd)
			break
		}
	}
}

// REQ-004-008: config egress add
func runConfigEgressAdd(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	vmName := args[0]
	domain := args[1]

	if vmName == "" {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: "VM name cannot be empty",
		}
	}

	// REQ-001-006: validate VM name format
	if err := backend.ValidateVMName(vmName); err != nil {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: err.Error(),
		}
	}

	// Validate domain format
	if err := security.ValidateEgressDomain(domain); err != nil {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: err.Error(),
		}
	}

	l := Loader()
	if l == nil {
		return ui.CLIError{
			Code:    "config_not_loaded",
			Message: "configuration not loaded",
		}
	}

	// Read current user-added domains for this VM
	current, err := readVMEgressFunc(l.SDHome(), vmName)
	if err != nil {
		return ui.CLIError{
			Code:    "egress_update_failed",
			Message: fmt.Sprintf("failed to read egress config for VM %q: %v", vmName, err),
		}
	}

	// Check if domain already in user list
	normalized := strings.ToLower(strings.TrimSpace(domain))
	for _, existing := range current {
		if strings.ToLower(strings.TrimSpace(existing)) == normalized {
			return ui.CLIError{
				Code:    "invalid_argument",
				Message: fmt.Sprintf("domain %q is already in the egress allowlist for VM %q", domain, vmName),
			}
		}
	}

	// Also check if it's a default domain
	for _, def := range config.DefaultEgressAllowlist {
		if strings.ToLower(strings.TrimSpace(def)) == normalized {
			return ui.CLIError{
				Code:    "invalid_argument",
				Message: fmt.Sprintf("domain %q is already in the default egress allowlist", domain),
			}
		}
	}

	// Add domain
	updated := append(current, domain)
	if err := writeVMEgressFunc(l.SDHome(), vmName, updated); err != nil {
		return ui.CLIError{
			Code:    "egress_update_failed",
			Message: fmt.Sprintf("failed to update egress config for VM %q: %v", vmName, err),
		}
	}

	type egressAddResult struct {
		VM     string `json:"vm"`
		Domain string `json:"domain"`
		Action string `json:"action"`
	}

	result := egressAddResult{
		VM:     vmName,
		Domain: domain,
		Action: "added",
	}

	f.SuccessData(result, func() string {
		return fmt.Sprintf("Added %q to egress allowlist for VM %q\n", domain, vmName)
	})

	return nil
}

// REQ-004-008: config egress remove
func runConfigEgressRemove(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	vmName := args[0]
	domain := args[1]

	if vmName == "" {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: "VM name cannot be empty",
		}
	}

	// REQ-001-006: validate VM name format
	if err := backend.ValidateVMName(vmName); err != nil {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: err.Error(),
		}
	}

	// Check if trying to remove a default domain
	normalized := strings.ToLower(strings.TrimSpace(domain))
	for _, def := range config.DefaultEgressAllowlist {
		if strings.ToLower(strings.TrimSpace(def)) == normalized {
			return ui.CLIError{
				Code:    "invalid_argument",
				Message: fmt.Sprintf("cannot remove default domain %q; default domains are always present", domain),
			}
		}
	}

	l := Loader()
	if l == nil {
		return ui.CLIError{
			Code:    "config_not_loaded",
			Message: "configuration not loaded",
		}
	}

	// Read current user-added domains for this VM
	current, err := readVMEgressFunc(l.SDHome(), vmName)
	if err != nil {
		return ui.CLIError{
			Code:    "egress_update_failed",
			Message: fmt.Sprintf("failed to read egress config for VM %q: %v", vmName, err),
		}
	}

	// Find and remove the domain
	found := false
	var updated []string
	for _, existing := range current {
		if strings.ToLower(strings.TrimSpace(existing)) == normalized {
			found = true
			continue
		}
		updated = append(updated, existing)
	}

	if !found {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: fmt.Sprintf("domain %q is not in the user-added egress allowlist for VM %q", domain, vmName),
		}
	}

	if err := writeVMEgressFunc(l.SDHome(), vmName, updated); err != nil {
		return ui.CLIError{
			Code:    "egress_update_failed",
			Message: fmt.Sprintf("failed to update egress config for VM %q: %v", vmName, err),
		}
	}

	type egressRemoveResult struct {
		VM     string `json:"vm"`
		Domain string `json:"domain"`
		Action string `json:"action"`
	}

	result := egressRemoveResult{
		VM:     vmName,
		Domain: domain,
		Action: "removed",
	}

	f.SuccessData(result, func() string {
		return fmt.Sprintf("Removed %q from egress allowlist for VM %q\n", domain, vmName)
	})

	return nil
}

// REQ-002-005: config egress list
func runConfigEgressList(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	vmName := args[0]

	if vmName == "" {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: "VM name cannot be empty",
		}
	}

	// REQ-001-006: validate VM name format
	if err := backend.ValidateVMName(vmName); err != nil {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: err.Error(),
		}
	}

	l := Loader()
	if l == nil {
		return ui.CLIError{
			Code:    "config_not_loaded",
			Message: "configuration not loaded",
		}
	}

	// Read user-added domains for this VM
	userDomains, err := readVMEgressFunc(l.SDHome(), vmName)
	if err != nil {
		return ui.CLIError{
			Code:    "egress_query_failed",
			Message: fmt.Sprintf("failed to read egress config for VM %q: %v", vmName, err),
		}
	}

	// Merge with defaults
	merged := security.BuildEgressList(userDomains)

	f.SuccessData(merged, func() string {
		return formatEgressList(merged)
	})

	return nil
}

// formatEgressList renders the egress allowlist as a human-readable table.
func formatEgressList(domains []security.EgressDomain) string {
	if len(domains) == 0 {
		return "No egress rules configured.\n"
	}

	var buf strings.Builder
	buf.WriteString("DOMAIN                                  SOURCE\n")
	for _, d := range domains {
		padded := d.Domain
		if len(padded) < 39 {
			padded += strings.Repeat(" ", 39-len(padded))
		}
		buf.WriteString(fmt.Sprintf("%s%s\n", padded, string(d.Source)))
	}
	return buf.String()
}
