// Package backend tests VMConfig validation.
// REQ-003-011: VMConfig Struct
// REQ-003-021: Error Semantics
// REQ-001-006: VM name validation
package backend

import (
	"errors"
	"strings"
	"testing"

	"pgregory.net/rapid"
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

// TestVMConfig_InvalidMemoryFormat fails validation.
// REQ-003-011: Memory must be in <number><unit> format
func TestVMConfig_InvalidMemoryFormat(t *testing.T) {
	tests := []struct {
		name   string
		memory string
	}{
		{name: "plain number", memory: "8"},
		{name: "wrong case", memory: "8gib"},
		{name: "space in value", memory: "8 GiB"},
		{name: "no number", memory: "GiB"},
		{name: "negative", memory: "-4GiB"},
		{name: "decimal", memory: "4.5GiB"},
		{name: "random text", memory: "lots"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Memory = tt.memory

			err := cfg.Validate()
			if err == nil {
				t.Errorf("memory=%q should fail validation", tt.memory)
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("error does not wrap ErrInvalidConfig: %v", err)
			}
			if !strings.Contains(err.Error(), "memory") {
				t.Errorf("error should mention memory: %v", err)
			}
		})
	}
}

// TestVMConfig_InvalidDiskFormat fails validation.
// REQ-003-011: Disk must be in <number><unit> format
func TestVMConfig_InvalidDiskFormat(t *testing.T) {
	tests := []struct {
		name string
		disk string
	}{
		{name: "plain number", disk: "100"},
		{name: "wrong case", disk: "100gib"},
		{name: "space in value", disk: "100 GiB"},
		{name: "no number", disk: "GiB"},
		{name: "random text", disk: "big"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Disk = tt.disk

			err := cfg.Validate()
			if err == nil {
				t.Errorf("disk=%q should fail validation", tt.disk)
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("error does not wrap ErrInvalidConfig: %v", err)
			}
			if !strings.Contains(err.Error(), "disk") {
				t.Errorf("error should mention disk: %v", err)
			}
		})
	}
}

// TestVMConfig_ValidResourceFormats pass validation.
// REQ-003-011: Valid <number><unit> formats for Memory and Disk
func TestVMConfig_ValidResourceFormats(t *testing.T) {
	tests := []struct {
		name string
		fmt  string
	}{
		{name: "GiB", fmt: "8GiB"},
		{name: "G", fmt: "8G"},
		{name: "GB", fmt: "8GB"},
		{name: "MiB", fmt: "512MiB"},
		{name: "M", fmt: "512M"},
		{name: "TiB", fmt: "1TiB"},
		{name: "T", fmt: "1T"},
		{name: "KiB", fmt: "1024KiB"},
		{name: "K", fmt: "1024K"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test as Memory
			cfg := validConfig()
			cfg.Memory = tt.fmt
			if err := cfg.Validate(); err != nil {
				t.Errorf("memory=%q should be valid: %v", tt.fmt, err)
			}

			// Test as Disk
			cfg = validConfig()
			cfg.Disk = tt.fmt
			if err := cfg.Validate(); err != nil {
				t.Errorf("disk=%q should be valid: %v", tt.fmt, err)
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

// --- VM Name Validation Tests ---
// REQ-001-006: VM names validated against ^[a-z][a-z0-9-]{0,62}$

// TestValidateVMName_Valid passes for spec-compliant names.
func TestValidateVMName_Valid(t *testing.T) {
	tests := []struct {
		name string
		vm   string
	}{
		{name: "single letter", vm: "a"},
		{name: "simple", vm: "myvm"},
		{name: "with hyphens", vm: "my-vm"},
		{name: "with digits", vm: "vm1"},
		{name: "mixed", vm: "my-vm-123"},
		{name: "max length 63", vm: strings.Repeat("a", 63)},
		{name: "boundary 2 chars", vm: "ab"},
		{name: "hyphenated project", vm: "secure-dev"},
		{name: "double hyphen", vm: "my--vm"},
		{name: "trailing hyphen", vm: "myvm-"},
		{name: "starts with letter has digits", vm: "a0-1b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVMName(tt.vm)
			if err != nil {
				t.Errorf("ValidateVMName(%q) returned error: %v", tt.vm, err)
			}
		})
	}
}

// TestValidateVMName_Invalid rejects non-compliant names.
func TestValidateVMName_Invalid(t *testing.T) {
	tests := []struct {
		name string
		vm   string
	}{
		{name: "empty", vm: ""},
		{name: "starts with digit", vm: "1vm"},
		{name: "starts with hyphen", vm: "-vm"},
		{name: "uppercase", vm: "MyVM"},
		{name: "underscore", vm: "my_vm"},
		{name: "space", vm: "my vm"},
		{name: "dot", vm: "my.vm"},
		{name: "too long 64", vm: strings.Repeat("a", 64)},
		{name: "special chars", vm: "vm!@#"},
		{name: "unicode", vm: "vm-\u00e9"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVMName(tt.vm)
			if err == nil {
				t.Errorf("ValidateVMName(%q) returned nil, want error", tt.vm)
			}
			if tt.vm != "" && !errors.Is(err, ErrInvalidVMName) {
				t.Errorf("error does not wrap ErrInvalidVMName: %v", err)
			}
		})
	}
}

// TestValidateVMName_ErrorMessages contain useful context.
func TestValidateVMName_ErrorMessages(t *testing.T) {
	t.Run("empty mentions empty", func(t *testing.T) {
		err := ValidateVMName("")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "empty") {
			t.Errorf("error should mention 'empty': %v", err)
		}
	})

	t.Run("too long mentions length", func(t *testing.T) {
		longName := strings.Repeat("a", 64)
		err := ValidateVMName(longName)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "63") {
			t.Errorf("error should mention '63': %v", err)
		}
	})

	t.Run("invalid format mentions pattern", func(t *testing.T) {
		err := ValidateVMName("MyVM")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "^[a-z]") {
			t.Errorf("error should mention pattern: %v", err)
		}
	})
}

// --- Property-based tests for ValidateVMName ---

// Property: valid names always pass validation
func TestProperty_ValidateVMName_ValidAlwaysPasses(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate valid VM names: lowercase letter + lowercase/digits/hyphens
		first := rapid.StringMatching(`[a-z]`).Draw(t, "first")
		rest := rapid.StringMatching(`[a-z0-9-]{0,20}`).Draw(t, "rest")
		name := first + rest

		err := ValidateVMName(name)
		if err != nil {
			t.Errorf("ValidateVMName(%q) returned error: %v", name, err)
		}
	})
}

// Property: names starting with digits always fail
func TestProperty_ValidateVMName_DigitStartAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`[0-9][a-z0-9-]{0,10}`).Draw(t, "name")

		err := ValidateVMName(name)
		if err == nil {
			t.Errorf("ValidateVMName(%q) should fail (starts with digit)", name)
		}
		if !errors.Is(err, ErrInvalidVMName) {
			t.Errorf("error should wrap ErrInvalidVMName: %v", err)
		}
	})
}

// Property: uppercase always fails
func TestProperty_ValidateVMName_UppercaseAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		prefix := rapid.StringMatching(`[A-Z]`).Draw(t, "prefix")
		suffix := rapid.StringMatching(`[a-z0-9-]{0,10}`).Draw(t, "suffix")
		name := prefix + suffix

		err := ValidateVMName(name)
		if err == nil {
			t.Errorf("ValidateVMName(%q) should fail (uppercase)", name)
		}
	})
}

// Property: names exceeding 63 chars always fail
func TestProperty_ValidateVMName_TooLongAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		length := rapid.IntRange(64, 200).Draw(t, "length")
		name := strings.Repeat("a", length)

		err := ValidateVMName(name)
		if err == nil {
			t.Errorf("ValidateVMName(len=%d) should fail", length)
		}
		if !errors.Is(err, ErrInvalidVMName) {
			t.Errorf("error should wrap ErrInvalidVMName: %v", err)
		}
	})
}

// Property: empty always fails
func TestProperty_ValidateVMName_EmptyAlwaysFails(t *testing.T) {
	err := ValidateVMName("")
	if err == nil {
		t.Error("ValidateVMName('') should fail")
	}
	if !errors.Is(err, ErrInvalidVMName) {
		t.Errorf("error should wrap ErrInvalidVMName: %v", err)
	}
}

// Property: valid names at max length always pass
func TestProperty_ValidateVMName_MaxLengthAlwaysPasses(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate names of exactly 63 chars
		rest := rapid.StringMatching(`[a-z0-9-]{62}`).Draw(t, "rest")
		name := "a" + rest
		if len(name) != 63 {
			t.Skip("generated wrong length")
		}

		err := ValidateVMName(name)
		if err != nil {
			t.Errorf("ValidateVMName(%q len=%d) should pass: %v", name[:10], len(name), err)
		}
	})
}

// Property: invalid characters always fail
func TestProperty_ValidateVMName_InvalidCharsAlwaysFail(t *testing.T) {
	invalidChars := []string{"_", ".", " ", "!", "@", "#", "$", "%", "^", "&", "*"}
	rapid.Check(t, func(t *rapid.T) {
		char := rapid.SampledFrom(invalidChars).Draw(t, "char")
		name := "vm" + char + "test"

		err := ValidateVMName(name)
		if err == nil {
			t.Errorf("ValidateVMName(%q) should fail (contains %q)", name, char)
		}
	})
}
