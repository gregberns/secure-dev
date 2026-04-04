// Package cmd provides custom argument validators for CLI commands.
// These replace Cobra's generic ExactArgs/MinimumNArgs with actionable error messages.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// exactArgs returns a PositionalArgs validator that requires exactly n arguments
// and produces an actionable error message including the expected usage pattern.
func exactArgs(n int, usage string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == n {
			return nil
		}
		return fmt.Errorf("missing required argument(s). Usage: %s %s", cmd.CommandPath(), usage)
	}
}

// minArgs returns a PositionalArgs validator that requires at least n arguments
// and produces an actionable error message including the expected usage pattern.
func minArgs(n int, usage string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) >= n {
			return nil
		}
		return fmt.Errorf("missing required argument(s). Usage: %s %s", cmd.CommandPath(), usage)
	}
}

// rangeArgs returns a PositionalArgs validator that requires between min and max
// arguments and produces an actionable error message including the expected usage pattern.
func rangeArgs(min, max int, usage string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) >= min && len(args) <= max {
			return nil
		}
		return fmt.Errorf("missing required argument(s). Usage: %s %s", cmd.CommandPath(), usage)
	}
}
