// Package cmd implements the doctor command.
// REQ-002-007: Diagnostic Commands -- doctor
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
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

// readProjectConfigFunc reads and parses a project-level config file.
// Returns nil if the file doesn't exist. Overridden in tests with a digital twin.
// REQ-004-029
var readProjectConfigFunc = readProjectConfigFromDisk

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

	// Check SD_HOME permissions (REQ-005-016)
	checks = append(checks, checkSDHomePermissions())

	// Check for cached git credentials (REQ-004-030)
	checks = append(checks, checkGitCredentials())

	// Check for security keys in project-level config (REQ-004-029)
	checks = append(checks, checkProjectSecurityConfig())

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

// checkSDHomePermissions verifies SD_HOME directory permissions.
// REQ-005-016
func checkSDHomePermissions() doctorCheck {
	if Loader() == nil {
		return doctorCheck{
			Name:    "sd_home_permissions",
			Status:  "pass",
			Message: "no loader available",
		}
	}

	sdHome := Loader().SDHome()
	info, err := os.Stat(sdHome)
	if err != nil {
		if os.IsNotExist(err) {
			return doctorCheck{
				Name:    "sd_home_permissions",
				Status:  "pass",
				Message: fmt.Sprintf("SD_HOME %s does not exist yet", sdHome),
			}
		}
		return doctorCheck{
			Name:    "sd_home_permissions",
			Status:  "fail",
			Message: fmt.Sprintf("cannot check SD_HOME: %v", err),
		}
	}

	mode := info.Mode().Perm()
	if mode != 0700 {
		return doctorCheck{
			Name:    "sd_home_permissions",
			Status:  "fail",
			Message: fmt.Sprintf("SD_HOME %s has permissions %04o, expected 0700; run: chmod 700 %s", sdHome, mode, sdHome),
		}
	}

	return doctorCheck{
		Name:    "sd_home_permissions",
		Status:  "pass",
		Message: fmt.Sprintf("SD_HOME %s has correct permissions (0700)", sdHome),
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

// readProjectConfigFromDisk reads a YAML config file from disk and returns
// the top-level keys. Returns nil if the file doesn't exist.
// REQ-004-029
func readProjectConfigFromDisk(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}
	return raw, nil
}

// checkProjectSecurityConfig checks that project-level config does not contain
// security keys, which are ignored by the config loader.
// REQ-004-029: sd doctor warns if a project-level .sd/config.yaml contains any security.* keys
func checkProjectSecurityConfig() doctorCheck {
	if Loader() == nil {
		return doctorCheck{
			Name:    "project_security_config",
			Status:  "pass",
			Message: "no loader available to check project config",
		}
	}

	projectDir := Loader().ProjectDir()
	if projectDir == "" {
		return doctorCheck{
			Name:    "project_security_config",
			Status:  "pass",
			Message: "no project directory configured",
		}
	}

	configPath := filepath.Join(projectDir, "config.yaml")
	raw, err := readProjectConfigFunc(configPath)
	if err != nil {
		return doctorCheck{
			Name:    "project_security_config",
			Status:  "fail",
			Message: fmt.Sprintf("cannot read project config: %v", err),
		}
	}
	if raw == nil {
		return doctorCheck{
			Name:    "project_security_config",
			Status:  "pass",
			Message: "no project-level config file found",
		}
	}

	// Check for security.* keys
	securityVal, hasSecurity := raw["security"]
	if !hasSecurity {
		return doctorCheck{
			Name:    "project_security_config",
			Status:  "pass",
			Message: "project config has no security keys",
		}
	}

	// Found security keys - report which ones
	securityMap, ok := securityVal.(map[string]any)
	var foundKeys []string
	if ok {
		for k := range securityMap {
			foundKeys = append(foundKeys, "security."+k)
		}
	} else {
		foundKeys = []string{"security"}
	}

	// Build actionable message listing all found keys
	keyList := ""
	for i, k := range foundKeys {
		if i > 0 {
			keyList += ", "
		}
		keyList += k
	}

	return doctorCheck{
		Name:    "project_security_config",
		Status:  "fail",
		Message: fmt.Sprintf("project config contains security keys (%s) which are ignored; set these in ~/.sd/config.yaml or via CLI flags (REQ-004-029)", keyList),
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
