package config

import (
	"os"
	"regexp"
	"strings"
)

// envVarRegex matches ${VAR_NAME} style references.
var envVarBracedRegex = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// envVarBareRegex matches $VAR_NAME style references (name continues until non-alphanumeric/underscore).
var envVarBareRegex = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)

// ResolveEnvVars expands environment variable references in a string value.
// REQ-005-008: ${VAR}, $VAR, $$ escape, unset → empty string
// Resolution is lazy: reads from the host environment at call time.
func ResolveEnvVars(value string, warn func(string)) string {
	// First, replace $$ with a sentinel to protect literal $
	sentinel := "\x00DOLLAR\x00"
	value = strings.ReplaceAll(value, "$$", sentinel)

	// Replace ${VAR} references
	value = envVarBracedRegex.ReplaceAllStringFunc(value, func(match string) string {
		varName := envVarBracedRegex.FindStringSubmatch(match)[1]
		resolved := os.Getenv(varName)
		if resolved == "" {
			if warn != nil {
				warn("environment variable " + varName + " is not set; using empty string")
			}
		}
		return resolved
	})

	// Replace $VAR references (but not the sentinel)
	value = envVarBareRegex.ReplaceAllStringFunc(value, func(match string) string {
		varName := envVarBareRegex.FindStringSubmatch(match)[1]
		resolved := os.Getenv(varName)
		if resolved == "" {
			if warn != nil {
				warn("environment variable " + varName + " is not set; using empty string")
			}
		}
		return resolved
	})

	// Restore literal $
	value = strings.ReplaceAll(value, sentinel, "$")

	return value
}

// HasEnvVarRef returns true if the value contains environment variable references.
// REQ-005-008
func HasEnvVarRef(value string) bool {
	if envVarBracedRegex.MatchString(value) {
		return true
	}
	// For bare $ refs, exclude $$ (escape)
	cleaned := strings.ReplaceAll(value, "$$", "")
	return envVarBareRegex.MatchString(cleaned)
}

// MaskValue masks a sensitive value for display.
// REQ-005-008: Resolved values are masked in output
func MaskValue(value string) string {
	if len(value) <= 8 {
		return "****"
	}
	return value[:5] + "****"
}
