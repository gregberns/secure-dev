package config

import (
	"testing"
)

func TestResolve(t *testing.T) {
	// Set up test env vars.
	t.Setenv("HOME", "/home/testuser")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")
	t.Setenv("EMPTY_VAR", "")

	tests := []struct {
		name       string
		input      string
		want       string
		wantUnres  []string
	}{
		{
			name:  "no references",
			input: "plain string",
			want:  "plain string",
		},
		{
			name:  "braced reference",
			input: "${HOME}/projects",
			want:  "/home/testuser/projects",
		},
		{
			name:  "bare reference",
			input: "$HOME/projects",
			want:  "/home/testuser/projects",
		},
		{
			name:  "multiple references",
			input: "${HOME}:${ANTHROPIC_API_KEY}",
			want:  "/home/testuser:sk-ant-test-key",
		},
		{
			name:  "dollar-dollar escape",
			input: "$$LITERAL",
			want:  "$LITERAL",
		},
		{
			name:  "escape in middle",
			input: "price is $$5.00",
			want:  "price is $5.00",
		},
		{
			name:      "unset variable braced",
			input:     "${NONEXISTENT_VAR}",
			want:      "",
			wantUnres: []string{"NONEXISTENT_VAR"},
		},
		{
			name:      "unset variable bare",
			input:     "$NONEXISTENT_VAR",
			want:      "",
			wantUnres: []string{"NONEXISTENT_VAR"},
		},
		{
			name:  "empty variable",
			input: "${EMPTY_VAR}",
			want:  "",
		},
		{
			name:  "mixed text and references",
			input: "user=${HOME} key=${ANTHROPIC_API_KEY}!",
			want:  "user=/home/testuser key=sk-ant-test-key!",
		},
		{
			name:  "bare ref stops at non-identifier",
			input: "$HOME/path/$ANTHROPIC_API_KEY.txt",
			want:  "/home/testuser/path/sk-ant-test-key.txt",
		},
		{
			name:  "trailing dollar",
			input: "end$",
			want:  "end$",
		},
		{
			name:  "dollar followed by non-identifier",
			input: "cost $5",
			want:  "cost $5",
		},
		{
			name:  "unclosed brace",
			input: "${UNCLOSED",
			want:  "${UNCLOSED",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:      "multiple unresolved",
			input:     "${FOO}-${BAR}",
			want:      "-",
			wantUnres: []string{"FOO", "BAR"},
		},
		{
			name:  "consecutive escapes",
			input: "$$$$",
			want:  "$$",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotUnres := Resolve(tt.input)
			if got != tt.want {
				t.Errorf("Resolve(%q) = %q, want %q", tt.input, got, tt.want)
			}
			if len(gotUnres) != len(tt.wantUnres) {
				t.Errorf("Resolve(%q) unresolved = %v, want %v", tt.input, gotUnres, tt.wantUnres)
			} else {
				for i := range gotUnres {
					if gotUnres[i] != tt.wantUnres[i] {
						t.Errorf("Resolve(%q) unresolved[%d] = %q, want %q", tt.input, i, gotUnres[i], tt.wantUnres[i])
					}
				}
			}
		})
	}
}
