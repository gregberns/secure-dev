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

// ioDiscard is a io.Writer that discards all output.
type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
