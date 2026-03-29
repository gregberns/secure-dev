// Package backend tests VMConfig validation.
// REQ-003-011: VMConfig Struct
// REQ-003-021: Error Semantics
package backend

import (
	"errors"
	"strings"
	"testing"
)

// TestVMConfig_ValidConfig passes validation.
// Property: Valid configs should return nil error
func TestVMConfig_ValidConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  VMConfig
	}{
		{
			name: "minimal valid",
			cfg: VMConfig{
				CPUs:      4,
				Memory:    "8GiB",
				Disk:      "100GiB",
				BaseImage: "ubuntu:24.04",
			},
		},
		{
			name: "with mounts",
			cfg: VMConfig{
				CPUs:      4,
				Memory:    "8GiB",
				Disk:      "100GiB",
				BaseImage: "ubuntu:24.04",
				Mounts: []Mount{
					{HostPath: "/path/to/project", GuestPath: "/project", Writable: false},
				},
			},
		},
		{
			name: "with network mode",
			cfg: VMConfig{
				CPUs:       4,
				Memory:     "8GiB",
				Disk:       "100GiB",
				BaseImage:  "ubuntu:24.04",
				NetworkMode: NetworkNAT,
			},
		},
		{
			name: "with env vars",
			cfg: VMConfig{
				CPUs:      4,
				Memory:    "8GiB",
				Disk:      "100GiB",
				BaseImage: "ubuntu:24.04",
				EnvVars: map[string]string{
					"PROJECT_NAME": "my-project",
					"BUILD_TAG":   "v1.2.3",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err != nil {
				t.Errorf("Validate() returned error: %v", err)
			}
		})
	}
}

// TestVMConfig_InvalidCPUs fails validation.
// Property: CPUs must be at least 1
func TestVMConfig_InvalidCPUs(t *testing.T) {
	tests := []struct {
		name string
		cpus int
	}{
		{name: "zero cpus", cpus: 0},
		{name: "negative cpus", cpus: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.CPUs = tt.cpus

			err := cfg.Validate()
			if err == nil {
				t.Error("Validate() returned nil error, want error")
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("error does not wrap ErrInvalidConfig: %v", err)
			}
		})
	}
}

// TestVMConfig_EmptyRequiredFields fails validation.
// Property: Memory, Disk, BaseImage cannot be empty
func TestVMConfig_EmptyRequiredFields(t *testing.T) {
	tests := []struct {
		name     string
		modifier func(*VMConfig)
	}{
		{
			name: "empty memory",
			modifier: func(c *VMConfig) { c.Memory = "" },
		},
		{
			name: "empty disk",
			modifier: func(c *VMConfig) { c.Disk = "" },
		},
		{
			name: "empty base image",
			modifier: func(c *VMConfig) { c.BaseImage = "" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modifier(&cfg)

			err := cfg.Validate()
			if err == nil {
				t.Error("Validate() returned nil error, want error")
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("error does not wrap ErrInvalidConfig: %v", err)
			}
		})
	}
}

// TestVMConfig_InvalidNetworkMode fails validation.
// REQ-003-012: NetworkMode valid values
func TestVMConfig_InvalidNetworkMode(t *testing.T) {
	cfg := validConfig()
	cfg.NetworkMode = "invalid"

	err := cfg.Validate()
	if err == nil {
		t.Error("Validate() returned nil error, want error")
	}
	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("error does not wrap ErrInvalidConfig: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid network_mode") {
		t.Errorf("error message missing 'invalid network_mode': %v", err)
	}
}

// TestVMConfig_ValidNetworkModes all pass.
// REQ-003-012
func TestVMConfig_ValidNetworkModes(t *testing.T) {
	validModes := []NetworkMode{NetworkNAT, NetworkBridged, NetworkIsolated}

	for _, mode := range validModes {
		t.Run(string(mode), func(t *testing.T) {
			cfg := validConfig()
			cfg.NetworkMode = mode

			err := cfg.Validate()
			if err != nil {
				t.Errorf("Validate() with mode %q returned error: %v", mode, err)
			}
		})
	}
}

// TestVMConfig_EmptyNetworkMode defaults to NAT.
// REQ-003-012
func TestVMConfig_EmptyNetworkMode(t *testing.T) {
	cfg := validConfig()
	cfg.NetworkMode = "" // Empty string should be valid (defaults to NAT)

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Validate() returned error: %v", err)
	}
}

// TestVMConfig_InvalidMounts fail validation.
// REQ-003-011
func TestVMConfig_InvalidMounts(t *testing.T) {
	tests := []struct {
		name     string
		mounts   []Mount
	}{
		{
			name: "empty host path",
			mounts: []Mount{{HostPath: "", GuestPath: "/project"}},
		},
		{
			name: "empty guest path",
			mounts: []Mount{{HostPath: "/path", GuestPath: ""}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Mounts = tt.mounts

			err := cfg.Validate()
			if err == nil {
				t.Error("Validate() returned nil error, want error")
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("error does not wrap ErrInvalidConfig: %v", err)
			}
		})
	}
}

// TestVMConfig_NoMounts is valid.
// REQ-003-011: Default is empty (no mounts)
func TestVMConfig_NoMounts(t *testing.T) {
	cfg := validConfig()
	cfg.Mounts = []Mount{} // Empty slice

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Validate() returned error: %v", err)
	}
}

// TestWrappedError_wrapsCorrectly.
// REQ-003-021: Errors wrap with context
func TestWrappedError_wrapsCorrectly(t *testing.T) {
	baseErr := ErrVMNotFound
	msg := "VM 'test' does not exist"

	wrapped := wrapError(baseErr, msg)

	if wrapped == nil {
		t.Fatal("wrapError returned nil")
	}

	unwrapped := errors.Unwrap(wrapped)
	if unwrapped != baseErr {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, baseErr)
	}

	errStr := wrapped.Error()
	if !strings.Contains(errStr, msg) {
		t.Errorf("error message missing %q: %s", msg, errStr)
	}
	if !strings.Contains(errStr, baseErr.Error()) {
		t.Errorf("error message missing base error: %s", errStr)
	}
}

// TestWrappedError_withNilBase.
func TestWrappedError_withNilBase(t *testing.T) {
	wrapped := wrapError(nil, "message")

	if wrapped != nil {
		t.Error("wrapError with nil base should return nil")
	}
}

// TestWrappedError_withEmptyMessage.
func TestWrappedError_withEmptyMessage(t *testing.T) {
	baseErr := ErrVMNotFound

	wrapped := wrapError(baseErr, "")

	if wrapped != baseErr {
		t.Errorf("wrapError with empty message = %v, want %v", wrapped, baseErr)
	}
}

// TestWrappedError_withNilErrorAndMessage.
func TestWrappedError_withNilErrorAndMessage(t *testing.T) {
	wrapped := wrapError(nil, "")

	if wrapped != nil {
		t.Error("wrapError with nil error and message should return nil")
	}
}

// validConfig returns a valid VMConfig for testing.
func validConfig() VMConfig {
	return VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}
}
