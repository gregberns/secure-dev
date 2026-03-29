package security

import (
	"fmt"
	"strings"
)

// ValidateToken checks a token string and returns warnings.
// REQ-004-012: GitHub Token Scoping
// REQ-004-013: Anthropic API Key Injection
func ValidateToken(token string, tokenType TokenType) []SecurityWarning {
	switch tokenType {
	case TokenGitHubPAT:
		return validateGitHubToken(token)
	case TokenGitHubFinegrained:
		return validateGitHubFinegrainedToken(token)
	case TokenAnthropicAPI:
		return validateAnthropicToken(token)
	default:
		return nil
	}
}

// validateGitHubToken checks a classic GitHub PAT.
// REQ-004-012: Warns if a classic (non-fine-grained) PAT is detected (by token prefix ghp_)
func validateGitHubToken(token string) []SecurityWarning {
	var warnings []SecurityWarning

	if strings.HasPrefix(token, "ghp_") {
		warnings = append(warnings, SecurityWarning{
			Code: "CLASSIC_PAT",
			Message: fmt.Sprintf(
				"classic GitHub PAT detected (prefix \"ghp_\"). "+
					"Fine-grained PATs (prefix \"github_pat_\") are recommended for "+
					"better security scoping. See \"sd token github setup\" for guidance.",
			),
		})
	}

	if strings.HasPrefix(token, "github_pat_") {
		// Fine-grained PAT - no warning
		return warnings
	}

	if len(token) > 0 && !strings.HasPrefix(token, "ghp_") && !strings.HasPrefix(token, "github_pat_") {
		warnings = append(warnings, SecurityWarning{
			Code:    "UNKNOWN_TOKEN_FORMAT",
			Message: "token does not match expected GitHub PAT format (ghp_ or github_pat_ prefix)",
		})
	}

	return warnings
}

// validateGitHubFinegrainedToken validates a fine-grained PAT.
// REQ-004-012
func validateGitHubFinegrainedToken(token string) []SecurityWarning {
	var warnings []SecurityWarning

	if strings.HasPrefix(token, "ghp_") {
		warnings = append(warnings, SecurityWarning{
			Code: "CLASSIC_PAT",
			Message: "classic GitHub PAT detected. Fine-grained PATs (prefix \"github_pat_\") " +
				"are recommended for better security scoping.",
		})
	}

	return warnings
}

// validateAnthropicToken validates an Anthropic API key.
// REQ-004-013
func validateAnthropicToken(token string) []SecurityWarning {
	var warnings []SecurityWarning

	if token == "" {
		warnings = append(warnings, SecurityWarning{
			Code:    "EMPTY_API_KEY",
			Message: "ANTHROPIC_API_KEY is empty; Claude Code will not be functional",
		})
		return warnings
	}

	if !strings.HasPrefix(token, "sk-ant-") {
		warnings = append(warnings, SecurityWarning{
			Code:    "UNUSUAL_KEY_FORMAT",
			Message: "API key does not have expected \"sk-ant-\" prefix",
		})
	}

	return warnings
}

// RedactToken redacts a token for safe display in logs.
// REQ-004-021: Credentials and API key values are never written to the audit log
func RedactToken(token string) string {
	if len(token) <= 8 {
		return "[REDACTED]"
	}
	return token[:5] + "..." + "[REDACTED]"
}
