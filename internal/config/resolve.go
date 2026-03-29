package config

import (
	"os"
	"strings"
)

// Resolve expands environment variable references in s from the host environment.
// Resolution rules (REQ-005-008):
//   - ${VAR_NAME} is replaced with os.Getenv("VAR_NAME")
//   - $VAR_NAME (no braces) is replaced; the name extends until the first
//     character that is not a letter, digit, or underscore
//   - $$ is an escape sequence that resolves to a literal $
//   - A reference to an unset variable resolves to the empty string;
//     the variable name is appended to the returned unresolved slice
//
// Resolution is lazy (called at use-time, not load-time) per REQ-005-008.
// Resolved values MUST NOT be logged or persisted.
func Resolve(s string) (resolved string, unresolved []string) {
	if !strings.Contains(s, "$") {
		return s, nil
	}

	var b strings.Builder
	b.Grow(len(s))

	i := 0
	for i < len(s) {
		if s[i] != '$' {
			b.WriteByte(s[i])
			i++
			continue
		}

		// At a '$'
		if i+1 >= len(s) {
			// Trailing '$' — emit as-is.
			b.WriteByte('$')
			i++
			continue
		}

		next := s[i+1]

		// $$ escape → literal $
		if next == '$' {
			b.WriteByte('$')
			i += 2
			continue
		}

		// ${VAR_NAME} form
		if next == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				// Unclosed brace — emit remainder as-is.
				b.WriteString(s[i:])
				i = len(s)
				continue
			}
			name := s[i+2 : i+2+end]
			val, ok := os.LookupEnv(name)
			if !ok {
				unresolved = append(unresolved, name)
			}
			b.WriteString(val)
			i = i + 2 + end + 1 // skip past '}'
			continue
		}

		// $VAR_NAME form (bare identifier)
		if isNameStart(next) {
			j := i + 2
			for j < len(s) && isNameChar(s[j]) {
				j++
			}
			name := s[i+1 : j]
			val, ok := os.LookupEnv(name)
			if !ok {
				unresolved = append(unresolved, name)
			}
			b.WriteString(val)
			i = j
			continue
		}

		// $ followed by something that isn't a name or { — emit as-is.
		b.WriteByte('$')
		i++
	}

	return b.String(), unresolved
}

func isNameStart(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_'
}

func isNameChar(c byte) bool {
	return isNameStart(c) || (c >= '0' && c <= '9')
}
