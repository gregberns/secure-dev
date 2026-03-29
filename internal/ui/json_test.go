package ui

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestSDError_Error(t *testing.T) {
	err := NewSDError(ErrCodeVMNotFound, "VM \"test\" does not exist")
	if err.Error() != "VM \"test\" does not exist" {
		t.Errorf("Error() = %q, want %q", err.Error(), "VM \"test\" does not exist")
	}
}

func TestSDError_Unwrap(t *testing.T) {
	cause := errors.New("connection refused")
	err := NewSDError(ErrCodeCreateFailed, "create failed").WithCause(cause)

	if err.Unwrap() != cause {
		t.Errorf("Unwrap() = %v, want %v", err.Unwrap(), cause)
	}

	if !errors.Is(err, cause) {
		t.Error("errors.Is should find the cause")
	}
}

func TestSDError_Unwrap_Nil(t *testing.T) {
	err := NewSDError(ErrCodeVMNotFound, "not found")
	if err.Unwrap() != nil {
		t.Errorf("Unwrap() = %v, want nil", err.Unwrap())
	}
}

func TestSDError_WithDetails(t *testing.T) {
	err := NewSDError(ErrCodeBackendNotFound, "not found").
		WithDetails(map[string]any{"requested": "avf", "available": []string{"lima"}})

	if err.Details["requested"] != "avf" {
		t.Errorf("Details[requested] = %v, want avf", err.Details["requested"])
	}
}

func TestSuccessJSON(t *testing.T) {
	data := map[string]string{"name": "myvm", "status": "running"}
	b, err := SuccessJSON(data)
	if err != nil {
		t.Fatalf("SuccessJSON() error: %v", err)
	}

	var resp JSONResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if !resp.OK {
		t.Error("OK should be true")
	}
	if resp.Error != nil {
		t.Error("Error should be nil")
	}
}

func TestErrorJSON(t *testing.T) {
	sdErr := ErrVMNotFound("myvm")
	b, err := ErrorJSON(sdErr)
	if err != nil {
		t.Fatalf("ErrorJSON() error: %v", err)
	}

	var resp JSONResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if resp.OK {
		t.Error("OK should be false")
	}
	if resp.Error == nil {
		t.Fatal("Error should not be nil")
	}
	if resp.Error.Code != ErrCodeVMNotFound {
		t.Errorf("Code = %q, want %q", resp.Error.Code, ErrCodeVMNotFound)
	}
	if resp.Error.Message != "VM \"myvm\" does not exist" {
		t.Errorf("Message = %q", resp.Error.Message)
	}
	if resp.Error.Details["name"] != "myvm" {
		t.Errorf("Details[name] = %v, want myvm", resp.Error.Details["name"])
	}
}

func TestErrorJSON_OmitsEmptyDetails(t *testing.T) {
	err := NewSDError("custom_error", "something went wrong")
	b, _ := ErrorJSON(err)

	var raw map[string]json.RawMessage
	json.Unmarshal(b, &raw)

	var errObj map[string]json.RawMessage
	json.Unmarshal(raw["error"], &errObj)

	if _, ok := errObj["details"]; ok {
		t.Error("details should be omitted when empty")
	}
}

func TestErrorConstructors(t *testing.T) {
	tests := []struct {
		name string
		err  *SDError
		code string
	}{
		{"BackendNotFound", ErrBackendNotFound("avf", []string{"lima"}), ErrCodeBackendNotFound},
		{"BackendNotInstalled", ErrBackendNotInstalled("lima"), ErrCodeBackendNotInstalled},
		{"VMExists", ErrVMExists("myvm"), ErrCodeVMExists},
		{"VMNotFound", ErrVMNotFound("myvm"), ErrCodeVMNotFound},
		{"VMRunning", ErrVMRunning("myvm"), ErrCodeVMRunning},
		{"InvalidVMName", ErrInvalidVMName("INVALID"), ErrCodeInvalidVMName},
		{"InvalidConfig", ErrInvalidConfig("/path", errors.New("parse error")), ErrCodeInvalidConfig},
		{"MountRejected", ErrMountRejected("/home/user"), ErrCodeMountRejected},
		{"CreateFailed", ErrCreateFailed("myvm", errors.New("timeout")), ErrCodeCreateFailed},
		{"ProvisionFailed", ErrProvisionFailed("myvm", errors.New("script error")), ErrCodeProvisionFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.code {
				t.Errorf("Code = %q, want %q", tt.err.Code, tt.code)
			}
			if tt.err.Error() == "" {
				t.Error("Error() should not be empty")
			}
		})
	}
}

func TestErrorConstants(t *testing.T) {
	// Verify all error codes are snake_case (REQ-002-018)
	codes := []string{
		ErrCodeBackendNotFound,
		ErrCodeBackendNotInstalled,
		ErrCodeVMExists,
		ErrCodeVMNotFound,
		ErrCodeVMRunning,
		ErrCodeInvalidVMName,
		ErrCodeInvalidConfig,
		ErrCodeMountRejected,
		ErrCodeCreateFailed,
		ErrCodeProvisionFailed,
		ErrCodeCredentialInjectionFailed,
		ErrCodeOrphanedState,
		ErrCodeAlreadyRunning,
		ErrCodeAlreadyStopped,
	}

	for _, code := range codes {
		for _, c := range code {
			if c >= 'A' && c <= 'Z' {
				t.Errorf("Error code %q contains uppercase character %c; must be snake_case", code, c)
			}
		}
	}
}
