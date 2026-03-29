// Package cmd implements the CLI command tree for sd.
// REQ-002-001: Command Entry Point
// REQ-002-010: Global Flags
// REQ-002-015: PersistentPreRun
// REQ-002-016: File Organization
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"sd/internal/config"
	"sd/internal/ui"
)

// rootCmd is initialized at declaration time so that other files' init()
// functions can safely call rootCmd.AddCommand() regardless of file ordering.
var (
	rootCmd = &cobra.Command{
		Use:   "sd",
		Short: "Manage secure VM environments for AI coding agents",
		Long: `sd (secure-dev) creates, configures, and manages secure VM environments
for running AI coding agents with bypass permissions.

It automates VM lifecycle, SSH configuration, tool provisioning,
credential injection, and session management.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// REQ-002-015: Validate mutual exclusivity of --verbose and --quiet
			verbose, _ := cmd.Flags().GetBool("verbose")
			quiet, _ := cmd.Flags().GetBool("quiet")
			if verbose && quiet {
				return ui.CLIError{
					Code:    "invalid_argument",
					Message: "--verbose and --quiet are mutually exclusive",
				}
			}

			// Set up output formatter based on --json flag
			jsonMode, _ := cmd.Flags().GetBool("json")
			formatter = ui.NewFormatter(jsonMode)

			// REQ-002-015: Load config from --config path
			configPath, _ := cmd.Flags().GetString("config")
			var loaderOpts []config.LoaderOption
			if configPath != "" {
				loaderOpts = append(loaderOpts, config.WithSDHome(configPath))
			}
			loader = config.NewLoader(loaderOpts...)

			// Commands that don't require config should still succeed
			// when config file doesn't exist. Walk parent chain so that
			// subcommands of no-config parents (e.g., "config get") are
			// also covered.
			requiresConfig := true
			noConfigCmds := map[string]bool{
				"version": true, "help": true, "completion": true,
				"doctor": true, "list": true, "status": true,
				"connect": true, "ssh-config": true, "sync": true,
				"audit": true, "config": true, "provision": true,
				"logs": true,
			}
			for c := cmd; c != nil; c = c.Parent() {
				if noConfigCmds[c.Name()] {
					requiresConfig = false
					break
				}
			}

			if err := loader.Load(); err != nil {
				if requiresConfig {
					return ui.CLIError{
						Code:    "config_not_found",
						Message: fmt.Sprintf("failed to load config: %v", err),
					}
				}
				// Non-fatal for commands that don't need config
			}

			return nil
		},
	}
	loader    *config.Loader
	formatter *ui.Formatter
)

func init() {
	// REQ-002-010: Global flags
	rootCmd.PersistentFlags().Bool("json", false, "Output structured JSON to stdout")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().BoolP("quiet", "q", false, "Suppress non-error output")
	rootCmd.PersistentFlags().String("config", "", "Path to config file (default ~/.sd/config.yaml)")
	rootCmd.PersistentFlags().String("vm", "", "Default VM name")

	// Bind to viper
	_ = viper.BindPFlag("json", rootCmd.PersistentFlags().Lookup("json"))
	_ = viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	_ = viper.BindPFlag("quiet", rootCmd.PersistentFlags().Lookup("quiet"))
	_ = viper.BindPFlag("vm", rootCmd.PersistentFlags().Lookup("vm"))

	// REQ-002-014: Enable prefix matching
	cobra.EnablePrefixMatching = true

	// REQ-002-002: Command groups
	rootCmd.AddGroup(
		&cobra.Group{ID: "vm", Title: "VM Management"},
		&cobra.Group{ID: "connection", Title: "Connection"},
		&cobra.Group{ID: "config", Title: "Configuration"},
		&cobra.Group{ID: "provisioning", Title: "Provisioning"},
		&cobra.Group{ID: "security", Title: "Security"},
		&cobra.Group{ID: "diagnostics", Title: "Diagnostics"},
		&cobra.Group{ID: "utility", Title: "Utility"},
	)
}

// Execute runs the root command.
// REQ-002-001
func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		if cliErr, ok := err.(ui.CLIError); ok {
			if formatter != nil {
				formatter.Error(cliErr)
			} else {
				fmt.Fprintf(os.Stderr, "Error: %s\n", cliErr.Message)
			}
			os.Exit(1)
		}
		// Non-CLIError: print as-is
		fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
		os.Exit(1)
	}
	return nil
}

// RootCmd returns the root cobra command (for testing).
func RootCmd() *cobra.Command {
	return rootCmd
}

// Formatter returns the current output formatter (set during PersistentPreRunE).
func Formatter() *ui.Formatter {
	return formatter
}

// Loader returns the current config loader (set during PersistentPreRunE).
func Loader() *config.Loader {
	return loader
}

// resolveVMName determines the VM name from args, flags, or config.
// REQ-002-019
func resolveVMName(cmd *cobra.Command, args []string) (string, error) {
	// 1. Positional argument
	if len(args) > 0 && args[0] != "" {
		return args[0], nil
	}

	// 2. --vm flag
	if vm, _ := cmd.Flags().GetString("vm"); vm != "" {
		return vm, nil
	}

	// 3. Config default
	if loader != nil {
		cfg := loader.Get()
		if cfg.Defaults.VM != "" {
			return cfg.Defaults.VM, nil
		}
	}

	return "", ui.CLIError{
		Code:    "no_vm_specified",
		Message: "no VM specified. Provide a name, use --vm, or set defaults.vm in config.",
	}
}
