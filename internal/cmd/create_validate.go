// Package cmd provides resource validation for the create command.
// REQ-002-003: VM Management Commands -- create
package cmd

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"sd/internal/backend"
	"sd/internal/ui"
)

// sizePattern matches valid size strings like "4GiB", "512MiB", "100GB", "1TB".
// REQ-002-003: resource flag validation
var sizePattern = regexp.MustCompile(`^[1-9][0-9]*\s*(B|[kKMGTP]i?B)$`)

// validateCreateResources validates --cpus, --memory, and --disk flag values.
// Only validates flags that were explicitly set by the user; unset flags are
// filled with defaults by buildVMConfig and do not need validation.
// REQ-002-003: resource flag validation
func validateCreateResources(cmd *cobra.Command) error {
	// Validate --cpus if explicitly set
	if cmd.Flags().Changed("cpus") {
		cpus, _ := cmd.Flags().GetInt("cpus")
		if cpus < 1 || cpus > 256 {
			return ui.CLIError{
				Code:    "invalid_cpus",
				Message: fmt.Sprintf("--cpus must be between 1 and 256, got %d", cpus),
			}
		}
	}

	// Validate --memory if explicitly set
	if cmd.Flags().Changed("memory") {
		memory, _ := cmd.Flags().GetString("memory")
		if err := validateSizeString(memory); err != nil {
			return ui.CLIError{
				Code:    "invalid_memory",
				Message: fmt.Sprintf("--memory: %s", err),
			}
		}
	}

	// Validate --disk if explicitly set
	if cmd.Flags().Changed("disk") {
		disk, _ := cmd.Flags().GetString("disk")
		if err := validateSizeString(disk); err != nil {
			return ui.CLIError{
				Code:    "invalid_disk",
				Message: fmt.Sprintf("--disk: %s", err),
			}
		}
	}

	return nil
}

// validateSizeString checks that a size string is a valid positive size like "4GiB", "512MiB".
// Returns an error if the string is empty, zero, or not a parseable size.
// REQ-002-003: resource flag validation
func validateSizeString(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("size must not be empty")
	}
	if !sizePattern.MatchString(s) {
		return fmt.Errorf("invalid size %q: must be a positive integer followed by a unit (e.g. \"4GiB\", \"512MiB\")", s)
	}
	// Extract the numeric part and verify it is positive (regex already enforces [1-9][0-9]*)
	numStr := strings.TrimRight(s, "BbKkMmGgTtPpIi")
	num, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil || num <= 0 {
		return fmt.Errorf("invalid size %q: numeric value must be a positive integer", s)
	}
	return nil
}

// validateBackendFunc checks that the given backend name is valid.
// Overridable in tests to use the test backend registry.
// REQ-002-003: backend flag validation
var validateBackendFunc = defaultValidateBackend

func defaultValidateBackend(name string) error {
	_, err := backend.Get(name)
	if err != nil {
		return ui.CLIError{
			Code:    "invalid_backend",
			Message: fmt.Sprintf("unknown backend %q: registered backends are %v", name, backend.List()),
		}
	}
	return nil
}
