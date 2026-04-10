// Package cmd implements the quick-start command.
// REQ-010-001: Quick-Start Command Registration
// REQ-010-002: Quick-Start Flags
// REQ-010-003: Runbook Output Format
// NOTE: Tests use global getBackendFunc, getenvFunc, findProjectConfigFunc,
// getWorkingDir -- do not use t.Parallel().
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"sd/internal/backend"
	"sd/internal/config"
	"sd/internal/ui"
)

// quickStartCheckResult is the JSON structure for --check output.
// REQ-010-015
type quickStartCheckResult struct {
	SDYamlExists     bool                  `json:"sd_yaml_exists"`
	VMExists         bool                  `json:"vm_exists"`
	VMRunning        bool                  `json:"vm_running"`
	VMName           string                `json:"vm_name"`
	Backend          string                `json:"backend"`
	Modules          []string              `json:"modules"`
	PackagesDeclared map[string]int        `json:"packages_declared"`
	Credentials      quickStartCredentials `json:"credentials"`
	RepoCloned       bool                  `json:"repo_cloned"`
	NeedsSetup       bool                  `json:"needs_setup"`
	Issues           []string              `json:"issues"`
}

// quickStartCredentials tracks host credential availability.
// REQ-010-015
type quickStartCredentials struct {
	GithubToken     bool `json:"github_token"`
	AnthropicAPIKey bool `json:"anthropic_api_key"`
}

// getenvFunc wraps os.Getenv for testing. Overridden in tests.
var getenvFunc = os.Getenv

// findProjectConfigFunc wraps config.FindProjectConfig for testing.
var findProjectConfigFunc = config.FindProjectConfig

func init() {
	quickStartCmd := &cobra.Command{
		Use:   "quick-start",
		Short: "Agent-executable runbook for first-time project setup",
		Long: `Output an agent-executable runbook for setting up a secure development
environment from scratch. The runbook is addressed to an AI coding agent.

Use --check to output a JSON assessment of setup completeness.
Use sd guide --agent for the complete command reference.`,
		GroupID: "start",
		Args:    cobra.NoArgs,
		RunE:    runQuickStart,
	}

	quickStartCmd.Flags().Bool("check", false, "Output JSON assessment of setup completeness")

	rootCmd.AddCommand(quickStartCmd)
}

// runQuickStart executes the quick-start command.
// REQ-010-001, REQ-010-002
func runQuickStart(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	checkMode, _ := cmd.Flags().GetBool("check")

	if checkMode {
		// REQ-010-002: --check output is always JSON regardless of --json flag
		jsonF := f
		if !f.JSONMode() {
			jsonF = ui.NewFormatter(true)
		}
		return runQuickStartCheck(cmd, jsonF)
	}

	// REQ-010-002: --json wraps runbook in JSON envelope
	placeholder := "Quick-start runbook will be here. Use sd guide --agent for command reference."
	if f.JSONMode() {
		type runbookData struct {
			Runbook string `json:"runbook"`
		}
		f.SuccessData(runbookData{Runbook: placeholder}, nil)
		return nil
	}

	// Default: output placeholder message
	f.SuccessData(nil, func() string {
		return placeholder + "\n"
	})
	return nil
}

// runQuickStartCheck gathers environment state and outputs a JSON assessment.
// REQ-010-015
func runQuickStartCheck(cmd *cobra.Command, f *ui.Formatter) error {
	result := quickStartCheckResult{
		Modules:          []string{},
		PackagesDeclared: map[string]int{},
		Issues:           []string{},
	}

	// Check credentials from host environment
	result.Credentials.GithubToken = getenvFunc("GITHUB_TOKEN") != ""
	result.Credentials.AnthropicAPIKey = getenvFunc("ANTHROPIC_API_KEY") != ""

	// Find .sd.yaml via config.FindProjectConfig
	cwd, err := getWorkingDir()
	if err != nil {
		cwd = "."
	}

	_, projCfg, findErr := findProjectConfigFunc(cwd)
	if findErr != nil {
		result.Issues = append(result.Issues, fmt.Sprintf("Error reading .sd.yaml: %v", findErr))
	}

	if projCfg != nil {
		result.SDYamlExists = true
		result.VMName = projCfg.Name
		result.Backend = projCfg.Backend
		if result.Backend == "" {
			result.Backend = "lima"
		}
		if len(projCfg.Modules) > 0 {
			result.Modules = projCfg.Modules
		}
	}

	// Check VM state if we have a name
	if result.VMName != "" {
		b, bErr := getBackendFunc(result.Backend)
		if bErr == nil {
			if aErr := b.Available(); aErr == nil {
				status, sErr := b.Status(cmd.Context(), result.VMName)
				if sErr == nil {
					result.VMExists = true
					result.VMRunning = status == backend.StatusRunning
				}
			}
		}
	}

	// Check if repo is cloned inside VM (only if running)
	// REQ-010-015: verified via test -d ~/projects/<name>/.git
	// Uses explicit /home/ubuntu path because tilde expansion requires a shell.
	if result.VMRunning && result.VMName != "" {
		b, bErr := getBackendFunc(result.Backend)
		if bErr == nil {
			testPath := fmt.Sprintf("/home/ubuntu/projects/%s/.git", result.VMName)
			testCmd := []string{"test", "-d", testPath}
			execResult, execErr := b.Exec(cmd.Context(), result.VMName, testCmd)
			if execErr == nil && execResult.ExitCode == 0 {
				result.RepoCloned = true
			}
		}
	}

	// Build issues list
	if !result.SDYamlExists {
		result.Issues = append(result.Issues, "No .sd.yaml found -- run sd quick-start to set up")
	}
	if result.SDYamlExists && !result.VMExists {
		result.Issues = append(result.Issues,
			fmt.Sprintf("VM %q does not exist -- run sd ensure to create", result.VMName))
	}
	if result.VMExists && !result.VMRunning {
		result.Issues = append(result.Issues,
			fmt.Sprintf("VM %q exists but is stopped -- run sd ensure to start", result.VMName))
	}
	if !result.Credentials.GithubToken {
		result.Issues = append(result.Issues, "GITHUB_TOKEN not set -- export it before connecting")
	}
	if !result.Credentials.AnthropicAPIKey {
		result.Issues = append(result.Issues, "ANTHROPIC_API_KEY not set -- export it before connecting")
	}
	if result.VMRunning && !result.RepoCloned {
		result.Issues = append(result.Issues,
			fmt.Sprintf("Repository not cloned inside VM -- run: sd exec %s -- git clone <url> ~/projects/%s",
				result.VMName, result.VMName))
	}

	result.NeedsSetup = len(result.Issues) > 0

	f.SuccessData(result, nil)
	return nil
}
