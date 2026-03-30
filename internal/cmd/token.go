// Package cmd implements the token command.
// REQ-002-008: Security Commands -- Token Management
// REQ-004-012: GitHub Token Scoping
// REQ-004-015: Token Rotation and Revocation
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"sd/internal/backend"
	"sd/internal/security"
	"sd/internal/ui"
)

// Well-known credential environment variable keys.
const (
	credKeyGithubToken = "GITHUB_TOKEN"
	credKeyAnthropicKey = "ANTHROPIC_API_KEY"
)

// knownCredentialKeys lists the credential keys managed by sd.
var knownCredentialKeys = []string{
	credKeyGithubToken,
	credKeyAnthropicKey,
}

// credentialEntry describes a configured credential without exposing its value.
// REQ-004-015
type credentialEntry struct {
	Name       string `json:"name"`
	Label      string `json:"label"`
	Configured bool   `json:"configured"`
}

// credentialLabels maps env var names to human-readable labels.
var credentialLabels = map[string]string{
	credKeyGithubToken: "GitHub Token",
	credKeyAnthropicKey: "Anthropic API Key",
}

// readVMEnvFunc reads the env map from a VM's config file.
// Overridden in tests with a digital twin.
var readVMEnvFunc = defaultReadVMEnv

// writeVMEnvFunc writes the env map to a VM's config file.
// Overridden in tests with a digital twin.
var writeVMEnvFunc = defaultWriteVMEnv

func defaultReadVMEnv(sdHome, vmName string) (map[string]string, error) {
	path := filepath.Join(sdHome, "vms", vmName, "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, fmt.Errorf("cannot read VM config: %w", err)
	}
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(string(data))); err != nil {
		return nil, fmt.Errorf("cannot parse VM config: %w", err)
	}
	env := v.GetStringMapString("env")
	if env == nil {
		env = make(map[string]string)
	}
	return env, nil
}

func defaultWriteVMEnv(sdHome, vmName string, env map[string]string) error {
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

	v.Set("env", env)

	if err := v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("cannot write VM config: %w", err)
	}
	os.Chmod(path, 0600)
	return nil
}

func init() {
	tokenCmd := &cobra.Command{
		Use:   "token",
		Short: "Manage credentials for VMs",
		Long: `Manage credentials (tokens, API keys) for VMs.

Credentials are stored in the sd configuration and injected into VM
sessions via SSH environment variables. They are never written to the
VM filesystem.

Subcommands:
  github setup  Show guidance for creating a fine-grained GitHub PAT
  rotate <vm>   Rotate credentials for a VM (reads from environment variables)
  revoke <vm>   Remove all stored credentials for a VM
  list <vm>     List configured credential types (without values)`,
		GroupID: "security",
	}

	// token github setup
	githubSetupCmd := &cobra.Command{
		Use:   "setup",
		Short: "Show guidance for creating a fine-grained GitHub PAT",
		Long: `Provides step-by-step instructions for creating a fine-grained
GitHub Personal Access Token with minimal scopes for use with sd.`,
		Args: cobra.NoArgs,
		RunE: runTokenGithubSetup,
	}

	// token github (parent for setup, future subcommands)
	githubCmd := &cobra.Command{
		Use:   "github",
		Short: "GitHub token management",
	}
	githubCmd.AddCommand(githubSetupCmd)

	// token rotate <vm>
	rotateCmd := &cobra.Command{
		Use:   "rotate <vm>",
		Short: "Rotate credentials for a VM",
		Long: `Rotate credentials for a VM by reading new values from environment
variables on the host.

Reads GITHUB_TOKEN and ANTHROPIC_API_KEY from the host environment.
Only variables that are set will be updated; others remain unchanged.`,
		Args: cobra.ExactArgs(1),
		RunE: runTokenRotate,
	}

	// token revoke <vm>
	revokeCmd := &cobra.Command{
		Use:   "revoke <vm>",
		Short: "Revoke all stored credentials for a VM",
		Long: `Remove all stored credentials for a VM. After revocation,
sd connect will not inject any credentials into the session.`,
		Args: cobra.ExactArgs(1),
		RunE: runTokenRevoke,
	}

	// token list <vm>
	listCmd := &cobra.Command{
		Use:   "list <vm>",
		Short: "List configured credential types for a VM",
		Long: `Show which credential types are configured for a VM without
revealing values. Use --json for structured output.`,
		Args: cobra.ExactArgs(1),
		RunE: runTokenList,
	}

	tokenCmd.AddCommand(githubCmd, rotateCmd, revokeCmd, listCmd)
	rootCmd.AddCommand(tokenCmd)
}

// runTokenGithubSetup outputs guidance for creating a fine-grained GitHub PAT.
// REQ-004-012
func runTokenGithubSetup(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	// REQ-004-012: Recommended scopes for fine-grained PAT
	type scopeInfo struct {
		Permission string `json:"permission"`
		Access     string `json:"access"`
	}

	type githubSetupData struct {
		PATType           string     `json:"pat_type"`
		SetupURL          string     `json:"setup_url"`
		RecommendedScopes []scopeInfo `json:"recommended_scopes"`
		Notes             []string   `json:"notes"`
	}

	data := githubSetupData{
		PATType:  "fine-grained",
		SetupURL: "https://github.com/settings/personal-access-tokens/new",
		RecommendedScopes: []scopeInfo{
			{Permission: "contents", Access: "write"},
			{Permission: "pull_requests", Access: "write"},
		},
		Notes: []string{
			"Create a fine-grained PAT scoped to specific repositories only.",
			"Do NOT use a classic PAT (ghp_ prefix) -- fine-grained tokens (github_pat_ prefix) provide better security.",
			"Enable branch protection on main/master before granting write access.",
			"Set GITHUB_TOKEN=<your-token> in your host environment, then run: sd token rotate <vm>",
		},
	}

	f.SuccessData(data, func() string {
		scopes := make([]struct{ Permission, Access string }, len(data.RecommendedScopes))
		for i, s := range data.RecommendedScopes {
			scopes[i] = struct{ Permission, Access string }{s.Permission, s.Access}
		}
		return formatGithubSetupGuidance(data.PATType, data.SetupURL, scopes, data.Notes)
	})
	return nil
}

// runTokenRotate reads credential values from host environment variables
// and stores them in the VM's config.
// REQ-004-015
func runTokenRotate(cmd *cobra.Command, args []string) error {
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

	sdHome := ""
	if l := Loader(); l != nil {
		sdHome = l.SDHome()
	}
	if sdHome == "" {
		return ui.CLIError{
			Code:    "config_not_found",
			Message: "cannot determine SD_HOME for VM config path",
		}
	}

	// Read existing VM env
	env, err := readVMEnvFunc(sdHome, vmName)
	if err != nil {
		return ui.CLIError{
			Code:    "token_rotate_failed",
			Message: fmt.Sprintf("failed to read VM config: %v", err),
		}
	}

	// Read credential values from host environment
	var rotated []string
	var warnings []security.SecurityWarning

	if token := os.Getenv(credKeyGithubToken); token != "" {
		warnings = append(warnings, security.ValidateToken(token, security.TokenGitHubPAT)...)
		env[credKeyGithubToken] = token
		rotated = append(rotated, credKeyGithubToken)
	}

	if key := os.Getenv(credKeyAnthropicKey); key != "" {
		warnings = append(warnings, security.ValidateToken(key, security.TokenAnthropicAPI)...)
		env[credKeyAnthropicKey] = key
		rotated = append(rotated, credKeyAnthropicKey)
	}

	if len(rotated) == 0 {
		return ui.CLIError{
			Code:    "token_rotate_failed",
			Message: "no credentials found in host environment. Set GITHUB_TOKEN and/or ANTHROPIC_API_KEY before running this command.",
		}
	}

	// Write updated env back to VM config
	if err := writeVMEnvFunc(sdHome, vmName, env); err != nil {
		return ui.CLIError{
			Code:    "token_rotate_failed",
			Message: fmt.Sprintf("failed to write VM config: %v", err),
		}
	}

	// Emit warnings for security observations
	for _, w := range warnings {
		f.Warn(w.Message)
	}

	type rotateResult struct {
		Name              string   `json:"name"`
		CredentialsRotated []string `json:"credentials_rotated"`
		Warnings          []string `json:"warnings,omitempty"`
	}

	result := rotateResult{
		Name:              vmName,
		CredentialsRotated: rotated,
	}
	for _, w := range warnings {
		result.Warnings = append(result.Warnings, w.Code+": "+w.Message)
	}

	f.SuccessData(result, func() string {
		return fmt.Sprintf("Rotated credentials for VM %q: %s\n",
			vmName, strings.Join(rotated, ", "))
	})
	return nil
}

// runTokenRevoke removes all stored credentials for a VM.
// REQ-004-015
func runTokenRevoke(cmd *cobra.Command, args []string) error {
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

	sdHome := ""
	if l := Loader(); l != nil {
		sdHome = l.SDHome()
	}
	if sdHome == "" {
		return ui.CLIError{
			Code:    "config_not_found",
			Message: "cannot determine SD_HOME for VM config path",
		}
	}

	// Read existing VM env
	env, err := readVMEnvFunc(sdHome, vmName)
	if err != nil {
		return ui.CLIError{
			Code:    "token_revoke_failed",
			Message: fmt.Sprintf("failed to read VM config: %v", err),
		}
	}

	// Remove credential keys
	var revoked []string
	for _, key := range knownCredentialKeys {
		if _, exists := env[key]; exists {
			delete(env, key)
			revoked = append(revoked, key)
		}
	}

	// Write updated env back to VM config
	if err := writeVMEnvFunc(sdHome, vmName, env); err != nil {
		return ui.CLIError{
			Code:    "token_revoke_failed",
			Message: fmt.Sprintf("failed to write VM config: %v", err),
		}
	}

	type revokeResult struct {
		Name               string   `json:"name"`
		CredentialsRevoked []string `json:"credentials_revoked"`
	}

	result := revokeResult{
		Name:               vmName,
		CredentialsRevoked: revoked,
	}

	if len(revoked) == 0 {
		result.CredentialsRevoked = []string{}
	}

	f.SuccessData(result, func() string {
		if len(revoked) == 0 {
			return fmt.Sprintf("No credentials configured for VM %q.\n", vmName)
		}
		return fmt.Sprintf("Revoked credentials for VM %q: %s\n",
			vmName, strings.Join(revoked, ", "))
	})
	return nil
}

// runTokenList shows which credential types are configured for a VM.
// REQ-004-015
func runTokenList(cmd *cobra.Command, args []string) error {
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

	sdHome := ""
	if l := Loader(); l != nil {
		sdHome = l.SDHome()
	}
	if sdHome == "" {
		return ui.CLIError{
			Code:    "config_not_found",
			Message: "cannot determine SD_HOME for VM config path",
		}
	}

	// Read VM env
	env, err := readVMEnvFunc(sdHome, vmName)
	if err != nil {
		return ui.CLIError{
			Code:    "token_not_configured",
			Message: fmt.Sprintf("failed to read VM config: %v", err),
		}
	}

	// Build credential list
	var entries []credentialEntry
	for _, key := range knownCredentialKeys {
		_, configured := env[key]
		entries = append(entries, credentialEntry{
			Name:       key,
			Label:      credentialLabels[key],
			Configured: configured,
		})
	}

	f.SuccessData(entries, func() string {
		return formatCredentialList(entries)
	})
	return nil
}

// formatGithubSetupGuidance produces human-readable setup instructions.
func formatGithubSetupGuidance(patType, setupURL string, scopes []struct{ Permission, Access string }, notes []string) string {
	var buf strings.Builder
	buf.WriteString("GitHub Fine-Grained Personal Access Token Setup\n")
	buf.WriteString("================================================\n\n")
	buf.WriteString(fmt.Sprintf("Create a new token at:\n  %s\n\n", setupURL))
	buf.WriteString("Recommended permissions:\n")
	for _, s := range scopes {
		buf.WriteString(fmt.Sprintf("  - %s: %s\n", s.Permission, s.Access))
	}
	buf.WriteString("\nNotes:\n")
	for _, n := range notes {
		buf.WriteString(fmt.Sprintf("  - %s\n", n))
	}
	return buf.String()
}

// formatCredentialList renders credential entries as human-readable output.
func formatCredentialList(entries []credentialEntry) string {
	var buf strings.Builder
	for _, e := range entries {
		status := "not configured"
		if e.Configured {
			status = "configured"
		}
		buf.WriteString(fmt.Sprintf("%-25s %-20s %s\n", e.Label, e.Name, status))
	}
	return buf.String()
}
