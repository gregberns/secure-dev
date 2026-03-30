// Package cmd implements the doctor command.
// REQ-002-007: Diagnostic Commands -- doctor
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"sd/internal/ui"
)

// doctorCheck represents a single diagnostic check result.
// REQ-002-007
type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "pass" or "fail"
	Message string `json:"message,omitempty"`
}

// lookPath resolves a binary name to its path. Overridden in tests with a digital twin.
var lookPath = exec.LookPath

// statPath checks if a file exists. Overridden in tests with a digital twin.
var statPath = os.Stat

// requiredBinaries lists the binaries that sd depends on.
// REQ-002-007
var requiredBinaries = []string{"limactl", "ssh", "tmux", "rsync"}

func init() {
	doctorCmd := &cobra.Command{
		Use:     "doctor",
		Short:   "Check system prerequisites and report status",
		Long: `Check that all required tools and configuration are in place
for sd to function correctly.

Checks for required binaries (limactl, ssh, tmux, rsync),
valid configuration, and VM backend availability.`,
		GroupID: "diagnostics",
		Args:    cobra.NoArgs,
		RunE:    runDoctor,
	}

	rootCmd.AddCommand(doctorCmd)
}

// runDoctor executes the doctor command.
// REQ-002-007
func runDoctor(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	var checks []doctorCheck

	// Check required binaries
	for _, bin := range requiredBinaries {
		checks = append(checks, checkBinary(bin))
	}

	// Check configuration
	checks = append(checks, checkConfig())

	// Check VM backend availability
	checks = append(checks, checkBackend())

	// Check for cached git credentials (REQ-004-030)
	checks = append(checks, checkGitCredentials())

	// Determine overall status
	allPassed := true
	for _, c := range checks {
		if c.Status != "pass" {
			allPassed = false
			break
		}
	}

	f.SuccessData(checks, func() string {
		return formatDoctorOutput(checks)
	})

	if !allPassed && !f.JSONMode() {
		// In human mode, exit non-zero if any check failed
		return ui.CLIError{
			Code:    "doctor_check_failed",
			Message: "one or more checks failed",
		}
	}

	return nil
}

// checkBinary checks if a required binary is available in PATH.
func checkBinary(name string) doctorCheck {
	path, err := lookPath(name)
	if err != nil {
		return doctorCheck{
			Name:    fmt.Sprintf("binary_%s", name),
			Status:  "fail",
			Message: fmt.Sprintf("%s not found in PATH", name),
		}
	}
	return doctorCheck{
		Name:    fmt.Sprintf("binary_%s", name),
		Status:  "pass",
		Message: fmt.Sprintf("found at %s", path),
	}
}

// checkConfig checks if the configuration file is valid.
func checkConfig() doctorCheck {
	if Loader() == nil {
		return doctorCheck{
			Name:   "config",
			Status: "fail",
			Message: "configuration loader not initialized",
		}
	}
	cfg := Loader().Get()
	if cfg == nil {
		return doctorCheck{
			Name:   "config",
			Status: "fail",
			Message: "no configuration loaded",
		}
	}
	return doctorCheck{
		Name:   "config",
		Status: "pass",
		Message: "configuration valid",
	}
}

// checkBackend checks if the VM backend is available.
func checkBackend() doctorCheck {
	backendName := "lima"
	if Loader() != nil {
		cfg := Loader().Get()
		if cfg.Defaults.Backend != "" {
			backendName = cfg.Defaults.Backend
		}
	}

	b, err := getBackendFunc(backendName)
	if err != nil {
		return doctorCheck{
			Name:    "backend",
			Status:  "fail",
			Message: fmt.Sprintf("backend %q not registered: %v", backendName, err),
		}
	}

	if err := b.Available(); err != nil {
		return doctorCheck{
			Name:    "backend",
			Status:  "fail",
			Message: fmt.Sprintf("backend %q not available: %v", backendName, err),
		}
	}

	return doctorCheck{
		Name:    "backend",
		Status:  "pass",
		Message: fmt.Sprintf("backend %q available", backendName),
	}
}

// checkGitCredentials checks for ~/.git-credentials on the host.
// REQ-004-030: Git credential caching to disk is a security risk.
func checkGitCredentials() doctorCheck {
	home, err := os.UserHomeDir()
	if err != nil {
		return doctorCheck{
			Name:    "git_credentials",
			Status:  "pass",
			Message: "cannot determine home directory for credential check",
		}
	}
	credPath := filepath.Join(home, ".git-credentials")
	if _, err := statPath(credPath); err == nil {
		return doctorCheck{
			Name:    "git_credentials",
			Status:  "fail",
			Message: "~/.git-credentials exists -- credential caching is a security risk (REQ-004-030)",
		}
	}
	return doctorCheck{
		Name:    "git_credentials",
		Status:  "pass",
		Message: "no cached git credentials",
	}
}

// formatDoctorOutput renders the check results as human-readable output.
func formatDoctorOutput(checks []doctorCheck) string {
	var output string
	for _, c := range checks {
		var indicator string
		if c.Status == "pass" {
			indicator = "\u2713" // checkmark
		} else {
			indicator = "\u2717" // ballot X
		}
		output += fmt.Sprintf("%s %s: %s\n", indicator, c.Name, c.Message)
	}
	return output
}
