// Package cmd implements the completion command.
// REQ-002-017: Shell Completions
package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"sd/internal/backend"
	"sd/internal/provision"
	"sd/internal/ui"
)

func init() {
	completionCmd := &cobra.Command{
		Use:   "completion <shell>",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for bash, zsh, and fish.

To load completions:

Bash:
  source <(sd completion bash)
  # or add to ~/.bashrc:
  echo 'source <(sd completion bash)' >> ~/.bashrc

Zsh:
  # If shell completion is not already enabled, run:
  autoload -Uz compinit && compinit
  # Add to ~/.zshrc:
  source <(sd completion zsh)
  # or write to a file in your fpath:
  sd completion zsh > "${fpath[1]}/_sd"

Fish:
  sd completion fish | source
  # or save to completions directory:
  sd completion fish > ~/.config/fish/completions/sd.fish`,
		GroupID:          "utility",
		Args:             exactArgs(1, "<shell>"),
		ValidArgs:        []string{"bash", "zsh", "fish"},
		RunE:             runCompletion,
	}

	rootCmd.AddCommand(completionCmd)

	// REQ-002-017: Register custom completions
	registerCustomCompletions()
}

// runCompletion generates the completion script for the requested shell.
// REQ-002-017
func runCompletion(cmd *cobra.Command, args []string) error {
	shell := args[0]
	switch shell {
	case "bash":
		return rootCmd.GenBashCompletion(cmd.OutOrStdout())
	case "zsh":
		return rootCmd.GenZshCompletion(cmd.OutOrStdout())
	case "fish":
		return rootCmd.GenFishCompletion(cmd.OutOrStdout(), true)
	default:
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: fmt.Sprintf("unsupported shell %q; must be bash, zsh, or fish", shell),
		}
	}
}

// registerCustomCompletions sets up dynamic shell completions for
// VM names, backend types, snapshot tags, and provisioning modules.
// REQ-002-017
func registerCustomCompletions() {
	// VM name completion: used by commands that take a VM name argument.
	rootCmd.ValidArgsFunction = vmNameCompletion

	// Register for specific subcommands that take VM names
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "completion" || cmd.Name() == "help" || cmd.Name() == "version" || cmd.Name() == "doctor" {
			continue
		}
		if cmd.HasSubCommands() {
			for _, sub := range cmd.Commands() {
				if requiresVMCompletion(sub) {
					sub.ValidArgsFunction = vmNameCompletion
				}
			}
		} else if requiresVMCompletion(cmd) {
			cmd.ValidArgsFunction = vmNameCompletion
		}
	}
}

// requiresVMCompletion returns true if a command's first positional arg
// is a VM name (determined by its Use string).
func requiresVMCompletion(cmd *cobra.Command) bool {
	use := cmd.Use
	// Commands that take <name>, <vm>, or [name] as first arg
	return strings.Contains(use, "<name>") ||
		strings.Contains(use, "<vm>") ||
		strings.Contains(use, "[name]")
}

// vmNameCompletion returns completion candidates from the list of known VMs.
// REQ-002-017
func vmNameCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Try to get the backend and list VMs
	var backendName string
	if l := Loader(); l != nil {
		cfg := l.Get()
		backendName = cfg.Defaults.Backend
	}

	b, err := getBackendFunc(backendName)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	vms, err := b.List(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	names := make([]string, len(vms))
	for i, vm := range vms {
		names[i] = vm.Name
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// backendNameCompletion returns completion candidates for --backend flag.
// REQ-002-017
func backendNameCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return backend.List(), cobra.ShellCompDirectiveNoFileComp
}

// snapshotTagCompletion returns completion candidates for --tag flag on snapshot commands.
// REQ-002-017
func snapshotTagCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	vmName := args[0]

	var backendName string
	if l := Loader(); l != nil {
		cfg := l.Get()
		backendName = cfg.Defaults.Backend
	}

	b, err := getBackendFunc(backendName)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	snap, ok := b.(backend.Snapshotter)
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	snapshots, err := snap.SnapshotList(cmd.Context(), vmName)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	tags := make([]string, len(snapshots))
	for i, s := range snapshots {
		tags[i] = s.Name
	}
	return tags, cobra.ShellCompDirectiveNoFileComp
}

// moduleNameCompletion returns completion candidates for --modules flag.
// REQ-002-017
func moduleNameCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return provision.BuiltinModuleNames, cobra.ShellCompDirectiveNoFileComp
}
