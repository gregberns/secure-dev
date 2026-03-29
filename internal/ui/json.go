package ui

import "encoding/json"

// Error code constants from REQ-001-010 and REQ-002-018.
// All codes use snake_case per REQ-002-018.
const (
	ErrCodeBackendNotFound          = "backend_not_found"
	ErrCodeBackendNotInstalled      = "backend_not_installed"
	ErrCodeVMExists                 = "vm_exists"
	ErrCodeVMNotFound               = "vm_not_found"
	ErrCodeVMRunning                = "vm_running"
	ErrCodeInvalidVMName            = "invalid_vm_name"
	ErrCodeInvalidConfig            = "invalid_config"
	ErrCodeMountRejected            = "mount_rejected"
	ErrCodeCreateFailed             = "create_failed"
	ErrCodeProvisionFailed          = "provision_failed"
	ErrCodeCredentialInjectionFailed = "credential_injection_failed"
	ErrCodeOrphanedState            = "orphaned_state"
	ErrCodeAlreadyRunning           = "already_running"
	ErrCodeAlreadyStopped           = "already_stopped"
)

// SDError is a JSON-serializable error with a machine-readable code.
// It implements error and supports Unwrap for error chains.
type SDError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
	cause   error
}

// Error returns the human-readable error message.
func (e *SDError) Error() string {
	return e.Message
}

// Unwrap returns the underlying cause, supporting errors.Is/errors.As.
func (e *SDError) Unwrap() error {
	return e.cause
}

// NewSDError creates an SDError with the given code and message.
func NewSDError(code, message string) *SDError {
	return &SDError{Code: code, Message: message}
}

// WithDetails returns a copy of the error with the given details.
func (e *SDError) WithDetails(details map[string]any) *SDError {
	e.Details = details
	return e
}

// WithCause returns a copy of the error wrapping the given cause.
func (e *SDError) WithCause(cause error) *SDError {
	e.cause = cause
	return e
}

// Convenience constructors for common errors.

func ErrBackendNotFound(name string, available []string) *SDError {
	return &SDError{
		Code:    ErrCodeBackendNotFound,
		Message: "backend \"" + name + "\" is not registered",
		Details: map[string]any{"requested": name, "available": available},
	}
}

func ErrBackendNotInstalled(name string) *SDError {
	return &SDError{
		Code:    ErrCodeBackendNotInstalled,
		Message: "backend \"" + name + "\" is not installed",
		Details: map[string]any{"backend": name},
	}
}

func ErrVMExists(name string) *SDError {
	return &SDError{
		Code:    ErrCodeVMExists,
		Message: "VM \"" + name + "\" already exists",
		Details: map[string]any{"name": name},
	}
}

func ErrVMNotFound(name string) *SDError {
	return &SDError{
		Code:    ErrCodeVMNotFound,
		Message: "VM \"" + name + "\" does not exist",
		Details: map[string]any{"name": name},
	}
}

func ErrVMRunning(name string) *SDError {
	return &SDError{
		Code:    ErrCodeVMRunning,
		Message: "VM \"" + name + "\" is running; use --force to destroy",
		Details: map[string]any{"name": name},
	}
}

func ErrInvalidVMName(name string) *SDError {
	return &SDError{
		Code:    ErrCodeInvalidVMName,
		Message: "invalid VM name \"" + name + "\": must match ^[a-z][a-z0-9-]{0,62}$",
		Details: map[string]any{"name": name},
	}
}

func ErrInvalidConfig(path string, cause error) *SDError {
	e := &SDError{
		Code:    ErrCodeInvalidConfig,
		Message: "invalid configuration: " + cause.Error(),
		Details: map[string]any{"path": path},
		cause:   cause,
	}
	return e
}

func ErrMountRejected(path string) *SDError {
	return &SDError{
		Code:    ErrCodeMountRejected,
		Message: "mount rejected: \"" + path + "\" is a prohibited path",
		Details: map[string]any{"path": path},
	}
}

func ErrCreateFailed(name string, cause error) *SDError {
	return &SDError{
		Code:    ErrCodeCreateFailed,
		Message: "failed to create VM \"" + name + "\": " + cause.Error(),
		Details: map[string]any{"name": name},
		cause:   cause,
	}
}

func ErrProvisionFailed(name string, cause error) *SDError {
	return &SDError{
		Code:    ErrCodeProvisionFailed,
		Message: "provisioning failed for VM \"" + name + "\": " + cause.Error(),
		Details: map[string]any{"name": name},
		cause:   cause,
	}
}

// JSONResponse is the top-level JSON envelope for all command output.
// REQ-002-012: Success uses {"ok": true, "data": ...}, errors use {"ok": false, "error": {...}}.
type JSONResponse struct {
	OK    bool           `json:"ok"`
	Data  any            `json:"data,omitempty"`
	Error *JSONErrorBody `json:"error,omitempty"`
}

// JSONErrorBody is the error payload inside a JSONResponse.
type JSONErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// SuccessJSON returns the JSON bytes for a successful response.
func SuccessJSON(data any) ([]byte, error) {
	return json.Marshal(JSONResponse{OK: true, Data: data})
}

// ErrorJSON returns the JSON bytes for an error response.
func ErrorJSON(err *SDError) ([]byte, error) {
	return json.Marshal(JSONResponse{
		OK: false,
		Error: &JSONErrorBody{
			Code:    err.Code,
			Message: err.Message,
			Details: err.Details,
		},
	})
}
