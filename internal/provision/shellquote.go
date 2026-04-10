// Package provision -- shell quoting for safe interpolation into bash -c scripts.
package provision

import "strings"

// shellQuote returns a shell-safe representation of s. If s contains no
// special characters it is returned as-is; otherwise it is wrapped in single
// quotes with any embedded single quotes escaped as '\'' (end quote, literal
// quote, start quote). Empty strings are returned as ''.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	// If the string is "safe" (only alphanumeric, dash, underscore, dot, slash,
	// colon, plus, at, percent, comma, equal, tilde) return it unquoted.
	safe := true
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '/' || c == ':' || c == '+' || c == '@' || c == '%' || c == ',' || c == '=' || c == '~') {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	// Wrap in single quotes, escaping any embedded single quotes.
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
