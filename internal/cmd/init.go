// Package cmd implements the init command for generating .sd.yaml project config.
// REQ-005-024: sd init generates a .sd.yaml template in the current directory.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"sd/internal/config"
	"sd/internal/ui"
)

func init() {
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Generate a .sd.yaml project configuration file",
		Long: `Generate a .sd.yaml project configuration file in the current directory.

Auto-detects the project type by looking for language-specific files
(go.mod, package.json, Cargo.toml, pyproject.toml, Dockerfile) and
suggests appropriate provisioning modules.

The generated file can be checked into the repository so that every
developer and agent gets the same VM environment via 'sd ensure'.`,
		GroupID: "config",
		Args:    cobra.NoArgs,
		RunE:    runInit,
	}

	initCmd.Flags().StringSlice("modules", nil, "provisioning modules to include (comma-separated)")
	initCmd.Flags().Bool("force", false, "overwrite existing .sd.yaml")

	rootCmd.AddCommand(initCmd)
}

// initResult is the structured output for the init command.
type initResult struct {
	Path     string   `json:"path"`
	Name     string   `json:"name"`
	Modules  []string `json:"modules"`
	Detected string   `json:"detected,omitempty"` // detected project type
}

// runInit executes the init command.
// REQ-005-024
func runInit(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	cwd, err := getWorkingDir()
	if err != nil {
		return ui.CLIError{
			Code:    "cwd_unavailable",
			Message: fmt.Sprintf("cannot determine current directory: %v", err),
		}
	}

	sdYamlPath := filepath.Join(cwd, config.ProjectConfigFile)

	// Check for existing .sd.yaml
	force, _ := cmd.Flags().GetBool("force")
	if !force {
		if _, err := os.Stat(sdYamlPath); err == nil {
			return ui.CLIError{
				Code:    "file_exists",
				Message: fmt.Sprintf("%s already exists; use --force to overwrite", sdYamlPath),
			}
		}
	}

	// Derive VM name from directory
	vmName := config.DeriveVMName(cwd)

	// Auto-detect project type and suggest modules
	detected, suggestedModules := detectProjectType(cwd)

	// CLI --modules flag overrides auto-detected modules
	modules, _ := cmd.Flags().GetStringSlice("modules")
	if len(modules) == 0 {
		modules = suggestedModules
	}

	// Build project config
	projCfg := config.ProjectConfig{
		Name:    vmName,
		Modules: modules,
		Mounts:  []string{fmt.Sprintf(".:/home/ubuntu/projects/%s:rw", vmName)},
	}

	// Marshal to YAML
	data, err := yaml.Marshal(&projCfg)
	if err != nil {
		return ui.CLIError{
			Code:    "marshal_failed",
			Message: fmt.Sprintf("failed to generate .sd.yaml: %v", err),
		}
	}

	// Prepend a header comment
	header := "# .sd.yaml -- Secure Dev project configuration\n" +
		"# Checked into the repository. Used by 'sd create' and 'sd ensure'.\n" +
		"#\n" +
		"# See: https://github.com/secure-dev/sd for documentation\n\n"

	if err := os.WriteFile(sdYamlPath, []byte(header+string(data)), 0644); err != nil {
		return ui.CLIError{
			Code:    "write_failed",
			Message: fmt.Sprintf("failed to write %s: %v", sdYamlPath, err),
		}
	}

	result := initResult{
		Path:     sdYamlPath,
		Name:     vmName,
		Modules:  modules,
		Detected: detected,
	}

	f.SuccessData(result, func() string {
		msg := fmt.Sprintf("Created %s\n", sdYamlPath)
		if detected != "" {
			msg += fmt.Sprintf("  Detected project type: %s\n", detected)
		}
		msg += fmt.Sprintf("  VM name: %s\n", vmName)
		if len(modules) > 0 {
			msg += fmt.Sprintf("  Modules: %v\n", modules)
		}
		msg += "\nEdit the file to customize, then run 'sd ensure' to create the VM.\n"
		return msg
	})

	return nil
}

// detectProjectType inspects the directory for language-specific files
// and returns the detected type name and suggested modules.
// REQ-005-024
func detectProjectType(dir string) (string, []string) {
	// Base modules always included
	base := []string{"base"}

	checks := []struct {
		file    string
		name    string
		modules []string
	}{
		{"go.mod", "go", []string{"golang"}},
		{"package.json", "node", []string{"nodejs"}},
		{"Cargo.toml", "rust", []string{"rust"}},
		{"pyproject.toml", "python", []string{"python"}},
		{"requirements.txt", "python", []string{"python"}},
		{"Dockerfile", "docker", []string{"docker"}},
	}

	for _, c := range checks {
		if _, err := os.Stat(filepath.Join(dir, c.file)); err == nil {
			return c.name, append(base, c.modules...)
		}
	}

	return "", base
}
