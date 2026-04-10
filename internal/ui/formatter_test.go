// Package ui provides tests for the output formatter.
// REQ-002-011, REQ-002-012, REQ-002-018
package ui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestCLIError_Error(t *testing.T) {
	err := CLIError{
		Code:    "vm_not_found",
		Message: "VM 'myvm' does not exist",
	}
	assert.Equal(t, "VM 'myvm' does not exist", err.Error())
}

func TestCLIError_WithDetails(t *testing.T) {
	err := CLIError{
		Code:    "vm_not_found",
		Message: "VM 'myvm' does not exist",
		Details: map[string]any{"name": "myvm"},
	}
	assert.Equal(t, "VM 'myvm' does not exist", err.Error())
}

// --- Human-mode tests (REQ-002-011) ---

func TestFormatter_HumanMode_Success(t *testing.T) {
	var stdout, stderr bytes.Buffer
	f := NewFormatterWithWriters(false, &stdout, &stderr)

	f.Success("VM created successfully")
	assert.Contains(t, stdout.String(), "VM created successfully")
	assert.Empty(t, stderr.String())
}

func TestFormatter_HumanMode_Error(t *testing.T) {
	var stdout, stderr bytes.Buffer
	f := NewFormatterWithWriters(false, &stdout, &stderr)

	f.Error(CLIError{
		Code:    "vm_not_found",
		Message: "VM 'myvm' does not exist",
	})
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "Error: VM 'myvm' does not exist")
}

func TestFormatter_HumanMode_Progress(t *testing.T) {
	var stdout, stderr bytes.Buffer
	f := NewFormatterWithWriters(false, &stdout, &stderr)

	f.Progress("Creating VM...")
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "Creating VM...")
}

func TestFormatter_HumanMode_Warn(t *testing.T) {
	var stdout, stderr bytes.Buffer
	f := NewFormatterWithWriters(false, &stdout, &stderr)

	f.Warn("Something seems off")
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "Warning: Something seems off")
}

func TestFormatter_HumanMode_SuccessData(t *testing.T) {
	var stdout, stderr bytes.Buffer
	f := NewFormatterWithWriters(false, &stdout, &stderr)

	f.SuccessData("raw-data", func() string {
		return "formatted output"
	})
	assert.Contains(t, stdout.String(), "formatted output")
	assert.Empty(t, stderr.String())
}

// --- JSON-mode tests (REQ-002-012) ---

func TestFormatter_JSONMode_Success(t *testing.T) {
	var stdout, stderr bytes.Buffer
	f := NewFormatterWithWriters(true, &stdout, &stderr)

	f.Success(map[string]string{"name": "myvm"})

	output := stdout.String()
	assert.Empty(t, stderr.String())

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.True(t, result["ok"].(bool))
	data := result["data"].(map[string]any)
	assert.Equal(t, "myvm", data["name"])
}

func TestFormatter_JSONMode_SuccessWithEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	f := NewFormatterWithWriters(true, &stdout, &stderr)

	f.Success([]string{"vm1", "vm2"})

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout.String()), &result))
	assert.True(t, result["ok"].(bool))

	data := result["data"].([]any)
	assert.Len(t, data, 2)
}

func TestFormatter_JSONMode_Error(t *testing.T) {
	var stdout, stderr bytes.Buffer
	f := NewFormatterWithWriters(true, &stdout, &stderr)

	f.Error(CLIError{
		Code:    "vm_not_found",
		Message: "VM 'myvm' does not exist",
		Details: map[string]any{"name": "myvm"},
	})

	assert.Empty(t, stderr.String())

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout.String()), &result))
	assert.False(t, result["ok"].(bool))

	errObj := result["error"].(map[string]any)
	assert.Equal(t, "vm_not_found", errObj["code"])
	assert.Equal(t, "VM 'myvm' does not exist", errObj["message"])
	details := errObj["details"].(map[string]any)
	assert.Equal(t, "myvm", details["name"])
}

func TestFormatter_JSONMode_Progress_NoStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	f := NewFormatterWithWriters(true, &stdout, &stderr)

	f.Progress("Creating VM...")
	// In JSON mode, progress goes to stderr, not stdout
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "Creating VM...")
}

func TestFormatter_JSONMode_Warn(t *testing.T) {
	var stdout, stderr bytes.Buffer
	f := NewFormatterWithWriters(true, &stdout, &stderr)

	f.Warn("Something seems off")
	// Warnings always go to stderr
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "Warning: Something seems off")
}

// --- JSON structure property tests (REQ-002-012, REQ-002-018) ---

func TestJSONErrorStructure_AllCodes(t *testing.T) {
	// REQ-002-018: Error codes use snake_case
	codes := []string{
		"vm_not_found", "vm_already_exists", "vm_not_running",
		"backend_unavailable", "config_invalid", "config_not_found",
		"no_vm_specified", "invalid_argument",
	}

	for _, code := range codes {
		t.Run(code, func(t *testing.T) {
			var buf bytes.Buffer
			f := NewFormatterWithWriters(true, &buf, ioDiscard{})
			f.Error(CLIError{Code: code, Message: "test"})

			var result map[string]any
			require.NoError(t, json.Unmarshal([]byte(buf.String()), &result))
			assert.False(t, result["ok"].(bool))

			errObj := result["error"].(map[string]any)
			assert.Equal(t, code, errObj["code"])
			assert.Equal(t, "test", errObj["message"])

			// Verify snake_case: no spaces, no camelCase
			assert.NotContains(t, code, " ")
			assert.NotContains(t, code, "-")
		})
	}
}

func TestJSONSuccessStructure(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatterWithWriters(true, &buf, ioDiscard{})

	f.Success(map[string]any{
		"vms": []map[string]any{
			{"name": "vm1", "status": "running"},
		},
	})

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(buf.String()), &result))
	assert.True(t, result["ok"].(bool))
	assert.NotNil(t, result["data"])
}

// --- Property-based tests ---

func TestProperty_CLIErrorAlwaysImplementsError(t *testing.T) {
	// CLIError must always satisfy the error interface
	var err error = CLIError{Code: "test", Message: "test message"}
	assert.NotNil(t, err)
	assert.Equal(t, "test message", err.Error())
}

func TestProperty_JSONOutputAlwaysParseable(t *testing.T) {
	// Any call to Success or Error in JSON mode must produce valid JSON
	testCases := []struct {
		name string
		fn   func(*Formatter)
	}{
		{"success_string", func(f *Formatter) { f.Success("hello") }},
		{"success_nil", func(f *Formatter) { f.Success(nil) }},
		{"success_struct", func(f *Formatter) { f.Success(map[string]int{"count": 5}) }},
		{"error_simple", func(f *Formatter) { f.Error(CLIError{Code: "x", Message: "y"}) }},
		{"error_with_details", func(f *Formatter) {
			f.Error(CLIError{Code: "x", Message: "y", Details: map[string]any{"k": "v"}})
		}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			f := NewFormatterWithWriters(true, &buf, ioDiscard{})
			tc.fn(f)

			output := buf.String()
			require.NotEmpty(t, output, "JSON mode must produce output")

			var result map[string]any
			require.NoError(t, json.Unmarshal([]byte(output), &result),
				"output must be valid JSON: %s", output)

			// Every JSON response must have "ok" field
			_, hasOK := result["ok"]
			assert.True(t, hasOK, "JSON response must have 'ok' field")
		})
	}
}

func TestProperty_JSONSuccessAlwaysHasOkTrue(t *testing.T) {
	dataValues := []any{
		"string",
		42,
		[]string{"a", "b"},
		map[string]string{"key": "val"},
		nil,
	}

	for _, data := range dataValues {
		var buf bytes.Buffer
		f := NewFormatterWithWriters(true, &buf, ioDiscard{})
		f.Success(data)

		var result map[string]any
		require.NoError(t, json.Unmarshal([]byte(buf.String()), &result))
		assert.True(t, result["ok"].(bool),
			"Success must set ok=true for data type %T", data)
	}
}

func TestProperty_JSONErrorAlwaysHasOkFalse(t *testing.T) {
	errors := []CLIError{
		{Code: "x", Message: "y"},
		{Code: "long_code_with_underscores", Message: strings.Repeat("x", 1000)},
		{Code: "c", Message: "m", Details: map[string]any{"a": "b"}},
	}

	for _, cliErr := range errors {
		var buf bytes.Buffer
		f := NewFormatterWithWriters(true, &buf, ioDiscard{})
		f.Error(cliErr)

		var result map[string]any
		require.NoError(t, json.Unmarshal([]byte(buf.String()), &result))
		assert.False(t, result["ok"].(bool),
			"Error must set ok=false for code %s", cliErr.Code)

		errObj := result["error"].(map[string]any)
		assert.Equal(t, cliErr.Code, errObj["code"])
		assert.Equal(t, cliErr.Message, errObj["message"])
	}
}

func TestProperty_HumanErrorAlwaysStartsWithError(t *testing.T) {
	errors := []CLIError{
		{Code: "vm_not_found", Message: "VM not found"},
		{Code: "config_invalid", Message: "Bad config"},
		{Code: "test", Message: ""},
	}

	for _, cliErr := range errors {
		var stderr bytes.Buffer
		f := NewFormatterWithWriters(false, ioDiscard{}, &stderr)
		f.Error(cliErr)

		if cliErr.Message != "" {
			assert.True(t, strings.HasPrefix(stderr.String(), "Error: "),
				"Human error output must start with 'Error: ': got %q", stderr.String())
		}
	}
}

// --- SuccessDataWithHints tests (REQ-010-016) ---

func TestFormatter_JSONMode_SuccessDataWithHints_Present(t *testing.T) {
	var stdout bytes.Buffer
	f := NewFormatterWithWriters(true, &stdout, ioDiscard{})

	hints := []string{
		"Export GITHUB_TOKEN on the host before running sd connect.",
		"Run sd config egress list to review allowed domains.",
	}
	f.SuccessDataWithHints(map[string]string{"name": "myvm"}, hints, func() string {
		return "VM created.\n"
	})

	var result map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.True(t, result["ok"].(bool))

	// hints field must be present with correct values
	hintsRaw, ok := result["hints"]
	require.True(t, ok, "hints field must be present in JSON output")
	hintsArr := hintsRaw.([]any)
	assert.Len(t, hintsArr, 2)
	assert.Equal(t, "Export GITHUB_TOKEN on the host before running sd connect.", hintsArr[0])
	assert.Equal(t, "Run sd config egress list to review allowed domains.", hintsArr[1])

	// data must still be present
	data := result["data"].(map[string]any)
	assert.Equal(t, "myvm", data["name"])
}

func TestFormatter_JSONMode_SuccessDataWithHints_NilHints(t *testing.T) {
	var stdout bytes.Buffer
	f := NewFormatterWithWriters(true, &stdout, ioDiscard{})

	f.SuccessDataWithHints(map[string]string{"name": "myvm"}, nil, func() string {
		return "VM created.\n"
	})

	var result map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.True(t, result["ok"].(bool))

	// hints field must be absent when nil
	_, hasHints := result["hints"]
	assert.False(t, hasHints, "hints field must be absent when hints is nil")
}

func TestFormatter_JSONMode_SuccessDataWithHints_EmptyHints(t *testing.T) {
	var stdout bytes.Buffer
	f := NewFormatterWithWriters(true, &stdout, ioDiscard{})

	f.SuccessDataWithHints(map[string]string{"name": "myvm"}, []string{}, func() string {
		return "VM created.\n"
	})

	var result map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.True(t, result["ok"].(bool))

	// hints field must be absent when empty slice
	_, hasHints := result["hints"]
	assert.False(t, hasHints, "hints field must be absent when hints is empty")
}

func TestFormatter_HumanMode_SuccessDataWithHints_NoHintsInOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	f := NewFormatterWithWriters(false, &stdout, &stderr)

	hints := []string{"This hint should not appear in human output."}
	f.SuccessDataWithHints("data", hints, func() string {
		return "VM created.\n"
	})

	// Human output should show the formatted text, not hints
	assert.Equal(t, "VM created.\n", stdout.String())
	assert.Empty(t, stderr.String())
	assert.NotContains(t, stdout.String(), "hint")
}

func TestProperty_SuccessDataWithHints_AlwaysValidJSON(t *testing.T) {
	testCases := []struct {
		name  string
		hints []string
	}{
		{"nil_hints", nil},
		{"empty_hints", []string{}},
		{"single_hint", []string{"hint one"}},
		{"multiple_hints", []string{"hint one", "hint two", "hint three"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			f := NewFormatterWithWriters(true, &buf, ioDiscard{})
			f.SuccessDataWithHints(map[string]string{"k": "v"}, tc.hints, nil)

			var result map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &result),
				"output must be valid JSON: %s", buf.String())
			assert.True(t, result["ok"].(bool))

			if len(tc.hints) > 0 {
				hintsArr := result["hints"].([]any)
				assert.Len(t, hintsArr, len(tc.hints))
				for i, h := range tc.hints {
					assert.Equal(t, h, hintsArr[i])
				}
			} else {
				_, hasHints := result["hints"]
				assert.False(t, hasHints, "hints must be absent when nil or empty")
			}
		})
	}
}

// --- Property-based tests using rapid (REQ-010-016) ---

// Property: for any non-empty hint string, SuccessDataWithHints always includes
// it in JSON output. This is the core invariant of the hints system.
func TestRapid_SuccessDataWithHints_NonEmptyHintAlwaysPresent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate 1-5 non-empty hint strings
		n := rapid.IntRange(1, 5).Draw(t, "numHints")
		hints := make([]string, n)
		for i := range hints {
			hints[i] = rapid.StringMatching(`.{1,100}`).Draw(t, "hint")
		}

		var buf bytes.Buffer
		f := NewFormatterWithWriters(true, &buf, ioDiscard{})
		f.SuccessDataWithHints(map[string]string{"key": "value"}, hints, nil)

		var result map[string]any
		require.NoError(t, json.Unmarshal(buf.Bytes(), &result),
			"output must be valid JSON: %s", buf.String())
		assert.True(t, result["ok"].(bool))

		// hints field must be present
		hintsRaw, hasHints := result["hints"]
		require.True(t, hasHints, "hints field must be present for non-empty hints slice")
		hintsArr := hintsRaw.([]any)
		assert.Len(t, hintsArr, n, "hints array length must match input")

		// Every input hint must appear in the output
		for i, h := range hints {
			assert.Equal(t, h, hintsArr[i], "hint at index %d must match input", i)
		}
	})
}

// Property: for nil or empty hints, the hints field is always omitted from JSON output.
func TestRapid_SuccessDataWithHints_EmptyHintsAlwaysOmitted(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Randomly choose nil or empty slice
		useNil := rapid.Bool().Draw(t, "useNil")
		var hints []string
		if !useNil {
			hints = []string{}
		}

		var buf bytes.Buffer
		f := NewFormatterWithWriters(true, &buf, ioDiscard{})
		f.SuccessDataWithHints(map[string]string{"key": "value"}, hints, nil)

		var result map[string]any
		require.NoError(t, json.Unmarshal(buf.Bytes(), &result),
			"output must be valid JSON: %s", buf.String())
		assert.True(t, result["ok"].(bool))

		_, hasHints := result["hints"]
		assert.False(t, hasHints, "hints field must be absent when hints is nil or empty")
	})
}

// ioDiscard is a io.Writer that discards all output.
type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
