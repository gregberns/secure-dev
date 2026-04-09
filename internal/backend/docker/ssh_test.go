package docker

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple word",
			input: "hello",
			want:  "hello",
		},
		{
			name:  "word with spaces",
			input: "hello world",
			want:  "'hello world'",
		},
		{
			name:  "word with pipe",
			input: "echo hello | tr h H",
			want:  "'echo hello | tr h H'",
		},
		{
			name:  "word with single quotes",
			input: "it's",
			want:  "'it'\\''s'",
		},
		{
			name:  "empty string",
			input: "",
			want:  "''",
		},
		{
			name:  "word with dollar sign",
			input: "$HOME",
			want:  "'$HOME'",
		},
		{
			name:  "word with double quotes",
			input: `say "hello"`,
			want:  `'say "hello"'`,
		},
		{
			name:  "word with semicolons",
			input: "cmd1; cmd2",
			want:  "'cmd1; cmd2'",
		},
		{
			name:  "word with backtick",
			input: "`whoami`",
			want:  "'`whoami`'",
		},
		{
			name:  "word with newline",
			input: "line1\nline2",
			want:  "'line1\nline2'",
		},
		{
			name:  "safe characters pass through",
			input: "/usr/bin/env",
			want:  "/usr/bin/env",
		},
		{
			name:  "alphanumeric only",
			input: "abc123",
			want:  "abc123",
		},
		{
			name:  "dashes and underscores",
			input: "my-flag_name",
			want:  "my-flag_name",
		},
		{
			name:  "equals sign",
			input: "KEY=value",
			want:  "KEY=value",
		},
		{
			name:  "parentheses need quoting",
			input: "(subshell)",
			want:  "'(subshell)'",
		},
		{
			name:  "multiple single quotes",
			input: "it's a 'test'",
			want:  "'it'\\''s a '\\''test'\\'''",
		},
		{
			name:  "ampersand needs quoting",
			input: "cmd1 && cmd2",
			want:  "'cmd1 && cmd2'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellQuote(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestShellQuoteJoinedCommand verifies that a realistic multi-argument command
// is properly joined for SSH remote execution.
func TestShellQuoteJoinedCommand(t *testing.T) {
	command := []string{"bash", "-c", "echo hello | tr h H"}
	quoted := make([]string, len(command))
	for i, arg := range command {
		quoted[i] = shellQuote(arg)
	}
	result := strings.Join(quoted, " ")

	// bash and -c are safe, but the script body gets quoted.
	assert.Equal(t, "bash -c 'echo hello | tr h H'", result)
}

// TestParseDockerPort tests the helper that extracts port numbers from
// docker port output. (Existing logic, but was never unit-tested.)
func TestParseDockerPort(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    int
		wantErr bool
	}{
		{
			name:   "ipv4 single line",
			output: "0.0.0.0:32768",
			want:   32768,
		},
		{
			name:   "ipv4 and ipv6",
			output: "0.0.0.0:32768\n:::32768",
			want:   32768,
		},
		{
			name:   "ipv6 only",
			output: ":::32768",
			want:   32768,
		},
		{
			name:    "empty output",
			output:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDockerPort(tt.output)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
