// Package cmd implements the config command and its subcommands.
// REQ-005-009: Config Get
// REQ-005-010: Config Set
// REQ-005-011: Config List
// REQ-005-012: Config Edit
// REQ-005-013: Config Validate
package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"sd/internal/config"
	"sd/internal/ui"
)

// knownConfigKeys lists all config keys that can be inspected.
// REQ-005-004, REQ-005-011
var knownConfigKeys = []string{
	"defaults.backend",
	"defaults.cpus",
	"defaults.memory",
	"defaults.disk",
	"defaults.image",
	"defaults.vm",
	"security.mount_policy",
	"security.egress_allowlist",
}

// configEntry represents a single config key-value-source triplet.
// REQ-005-011
type configEntry struct {
	Key    string `json:"key"`
	Value  any    `json:"value"`
	Source string `json:"source"`
}

// runEditorCmd runs the editor command. Overridden in tests with a digital twin.
var runEditorCmd = defaultRunEditorCmd

func defaultRunEditorCmd(editor string, args ...string) error {
	cmd := exec.Command(editor, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// configEditTemplate is the template for new config files.
// REQ-005-012
const configEditTemplate = `# sd configuration file
# See documentation for available options.

defaults:
  # backend: lima
  # cpus: 4
  # memory: 8GiB
  # disk: 100GiB
  # image: ubuntu:24.04
  # vm: ""

# security:
#   mount_policy: none
#   egress_allowlist:
#     - api.anthropic.com
#     - github.com

# vms: {}
`

func init() {
	configCmd := &cobra.Command{
		Use:   "config <subcommand>",
		Short: "Manage configuration",
		Long: `View and modify sd configuration.

Subcommands:
  get <key>              Get a config value with its source
  set <key> <value>      Set a config value in user-level config
  list                   List all config values with sources
  edit                   Open config file in $EDITOR
  validate               Validate all config files`,
		GroupID: "config",
	}

	// REQ-005-009: config get
	configGetCmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Long:  `Display the resolved value for a configuration key along with its source.`,
		Args:  cobra.ExactArgs(1),
		RunE:  runConfigGet,
	}

	// REQ-005-010: config set
	configSetCmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Long:  `Write a key-value pair to the user-level config file. Use --project to write to project-level config.`,
		Args:  cobra.ExactArgs(2),
		RunE:  runConfigSet,
	}
	configSetCmd.Flags().Bool("project", false, "write to project-level config instead of user-level")

	// REQ-005-011: config list
	configListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all config values",
		Long:  `Display all configuration values with their resolved sources.`,
		Args:  cobra.NoArgs,
		RunE:  runConfigList,
	}

	// REQ-005-012: config edit
	configEditCmd := &cobra.Command{
		Use:   "edit",
		Short: "Open config file in editor",
		Long:  `Open the user-level config file in $EDITOR (or $VISUAL, falling back to vi).`,
		Args:  cobra.NoArgs,
		RunE:  runConfigEdit,
	}
	configEditCmd.Flags().Bool("project", false, "edit project-level config instead of user-level")

	// REQ-005-013: config validate
	configValidateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate config files",
		Long:  `Validate all discoverable config files and report errors.`,
		Args:  cobra.NoArgs,
		RunE:  runConfigValidate,
	}

	configCmd.AddCommand(configGetCmd, configSetCmd, configListCmd, configEditCmd, configValidateCmd)
	rootCmd.AddCommand(configCmd)
}

// isKnownConfigKey returns true if the key is a recognized config key.
func isKnownConfigKey(key string) bool {
	for _, k := range knownConfigKeys {
		if k == key {
			return true
		}
	}
	return false
}

// formatConfigValue formats a config value for human-readable output.
// REQ-005-011: arrays show [N entries], empty shows (not set)
func formatConfigValue(v any) string {
	switch val := v.(type) {
	case nil:
		return "(not set)"
	case string:
		if val == "" {
			return "(not set)"
		}
		return val
	case []interface{}:
		return fmt.Sprintf("[%d entries]", len(val))
	case []string:
		return fmt.Sprintf("[%d entries]", len(val))
	default:
		return fmt.Sprintf("%v", val)
	}
}

// parseConfigValue attempts to coerce a string value to the appropriate type.
// REQ-005-010: array values accepted as JSON, numeric values parsed as int
func parseConfigValue(key, raw string) any {
	// Try JSON array
	var jsonArr []string
	if err := json.Unmarshal([]byte(raw), &jsonArr); err == nil {
		return jsonArr
	}

	// Try integer for known numeric keys
	switch key {
	case "defaults.cpus":
		if n, err := strconv.Atoi(raw); err == nil {
			return n
		}
	}

	return raw
}

// resolveEditor returns the editor command to use.
// REQ-005-012: $EDITOR, $VISUAL, vi
func resolveEditor() string {
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor
	}
	return "vi"
}

// REQ-005-009: config get
func runConfigGet(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	key := args[0]
	if !isKnownConfigKey(key) {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: fmt.Sprintf("unknown config key %q; use 'sd config list' to see available keys", key),
		}
	}

	l := Loader()
	if l == nil {
		return ui.CLIError{
			Code:    "config_not_loaded",
			Message: "configuration not loaded",
		}
	}

	value, err := l.GetKey(key)
	if err != nil {
		return ui.CLIError{
			Code:    "config_query_failed",
			Message: fmt.Sprintf("failed to get config key %q: %v", key, err),
		}
	}

	source := l.Source(key)

	type configGetResult struct {
		Key    string `json:"key"`
		Value  any    `json:"value"`
		Source string `json:"source"`
	}

	result := configGetResult{
		Key:    key,
		Value:  value,
		Source: string(source),
	}

	f.SuccessData(result, func() string {
		valStr := formatConfigValue(value)
		return fmt.Sprintf("%s (source: %s)\n", valStr, string(source))
	})

	return nil
}

// REQ-005-010: config set
func runConfigSet(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	key := args[0]
	valueStr := args[1]

	l := Loader()
	if l == nil {
		return ui.CLIError{
			Code:    "config_not_loaded",
			Message: "configuration not loaded",
		}
	}

	value := parseConfigValue(key, valueStr)

	target := "user"
	if project, _ := cmd.Flags().GetBool("project"); project {
		target = "project"
	}

	// Determine config file path for rollback
	var configPath string
	switch target {
	case "user":
		configPath = filepath.Join(l.SDHome(), "config.yaml")
	case "project":
		pd := l.ProjectDir()
		if pd == "" {
			return ui.CLIError{
				Code:    "invalid_argument",
				Message: "no project directory found; cannot use --project",
			}
		}
		configPath = filepath.Join(pd, "config.yaml")
	}

	// Save current content for rollback (REQ-005-010)
	var rollbackData []byte
	if data, err := os.ReadFile(configPath); err == nil {
		rollbackData = data
	}

	// Set the value
	if err := l.Set(key, value, target); err != nil {
		return ui.CLIError{
			Code:    "config_set_failed",
			Message: fmt.Sprintf("failed to set %s: %v", key, err),
		}
	}

	// Validate after write; rollback on failure
	results, _ := l.Validate()
	for _, r := range results {
		if r.Path == configPath && !r.Valid {
			// Rollback
			if rollbackData != nil {
				_ = os.WriteFile(configPath, rollbackData, 0600)
			} else {
				_ = os.Remove(configPath)
			}
			return ui.CLIError{
				Code:    "invalid_config",
				Message: fmt.Sprintf("setting %s=%s would produce invalid config: %s; change was rolled back", key, valueStr, strings.Join(r.Errors, ", ")),
			}
		}
	}

	type configSetResult struct {
		Key     string `json:"key"`
		Value   any    `json:"value"`
		File    string `json:"file"`
		Updated bool   `json:"updated"`
	}

	result := configSetResult{
		Key:     key,
		Value:   value,
		File:    configPath,
		Updated: true,
	}

	f.SuccessData(result, func() string {
		return fmt.Sprintf("Set %s in %s\n", key, configPath)
	})

	return nil
}

// REQ-005-011: config list
func runConfigList(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	l := Loader()
	if l == nil {
		return ui.CLIError{
			Code:    "config_not_loaded",
			Message: "configuration not loaded",
		}
	}

	var entries []configEntry
	for _, key := range knownConfigKeys {
		value, _ := l.GetKey(key)
		source := l.Source(key)
		entries = append(entries, configEntry{
			Key:    key,
			Value:  value,
			Source: string(source),
		})
	}

	f.SuccessData(entries, func() string {
		return formatConfigTable(entries)
	})

	return nil
}

// formatConfigTable renders config entries as a human-readable table.
func formatConfigTable(entries []configEntry) string {
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KEY\tVALUE\tSOURCE")
	for _, e := range entries {
		fmt.Fprintf(w, "%s\t%s\t%s\n", e.Key, formatConfigValue(e.Value), e.Source)
	}
	w.Flush()
	return buf.String()
}

// REQ-005-012: config edit
func runConfigEdit(cmd *cobra.Command, args []string) error {
	// REQ-005-012: reject in non-interactive (JSON) context
	jsonMode := false
	if f := Formatter(); f != nil {
		jsonMode = f.JSONMode()
	}
	if !jsonMode {
		if v := os.Getenv("SD_JSON"); v == "true" || v == "1" {
			jsonMode = true
		}
	}
	if jsonMode {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: "\"sd config edit\" is interactive and cannot be used when SD_JSON=true; use \"sd config set\" instead",
		}
	}

	l := Loader()
	if l == nil {
		return ui.CLIError{
			Code:    "config_not_loaded",
			Message: "configuration not loaded",
		}
	}

	// Determine config file path
	var configPath string
	if project, _ := cmd.Flags().GetBool("project"); project {
		pd := l.ProjectDir()
		if pd == "" {
			return ui.CLIError{
				Code:    "invalid_argument",
				Message: "no project directory found; cannot use --project",
			}
		}
		configPath = filepath.Join(pd, "config.yaml")
	} else {
		configPath = filepath.Join(l.SDHome(), "config.yaml")
	}

	// Create file with template if not exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		dir := filepath.Dir(configPath)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return ui.CLIError{
				Code:    "config_set_failed",
				Message: fmt.Sprintf("cannot create config directory %s: %v", dir, err),
			}
		}
		if err := os.WriteFile(configPath, []byte(configEditTemplate), 0600); err != nil {
			return ui.CLIError{
				Code:    "config_set_failed",
				Message: fmt.Sprintf("cannot create config file %s: %v", configPath, err),
			}
		}
	}

	// Open editor
	editor := resolveEditor()
	if err := runEditorCmd(editor, configPath); err != nil {
		return ui.CLIError{
			Code:    "config_edit_failed",
			Message: fmt.Sprintf("editor failed: %v", err),
		}
	}

	// Validate after edit
	results, _ := l.Validate()
	for _, r := range results {
		if r.Path == configPath && !r.Valid {
			fmt.Fprintf(os.Stderr, "Warning: config file %s has errors:\n", configPath)
			for _, e := range r.Errors {
				fmt.Fprintf(os.Stderr, "  %s\n", e)
			}
			return nil
		}
	}

	fmt.Fprintf(os.Stderr, "Config file %s is valid.\n", configPath)
	return nil
}

// REQ-005-013: config validate
func runConfigValidate(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	l := Loader()
	if l == nil {
		return ui.CLIError{
			Code:    "config_not_loaded",
			Message: "configuration not loaded",
		}
	}

	results, _ := l.Validate()

	// Filter out nil results (non-existent files)
	var validResults []config.ValidationResult
	for _, r := range results {
		if r.Path != "" {
			validResults = append(validResults, r)
		}
	}

	allValid := len(validResults) == 0
	if len(validResults) > 0 {
		allValid = true
		for _, r := range validResults {
			if !r.Valid {
				allValid = false
				break
			}
		}
	}

	type validateResult struct {
		Valid bool                         `json:"valid"`
		Files []config.ValidationResult    `json:"files"`
	}

	f.SuccessData(validateResult{
		Valid: allValid,
		Files: validResults,
	}, func() string {
		return formatValidateOutput(validResults)
	})

	if !allValid && !f.JSONMode() {
		return ui.CLIError{
			Code:    "invalid_config",
			Message: "one or more config files are invalid",
		}
	}

	return nil
}

// formatValidateOutput renders validation results as human-readable output.
func formatValidateOutput(results []config.ValidationResult) string {
	if len(results) == 0 {
		return "No config files found.\n"
	}

	var buf strings.Builder
	allValid := true
	for _, r := range results {
		if r.Valid {
			buf.WriteString(fmt.Sprintf("Validating %s ... ok\n", r.Path))
		} else {
			allValid = false
			buf.WriteString(fmt.Sprintf("Validating %s ... FAILED\n", r.Path))
			for _, e := range r.Errors {
				buf.WriteString(fmt.Sprintf("  %s\n", e))
			}
		}
	}
	if allValid {
		buf.WriteString("All config files valid.\n")
	}
	return buf.String()
}
