package config

import "testing"

func TestIsSensitiveKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"ANTHROPIC_API_KEY", true},
		{"GITHUB_TOKEN", true},
		{"DB_SECRET", true},
		{"ADMIN_PASSWORD", true},
		{"HOME", false},
		{"PATH", false},
		{"BACKEND", false},
		{"", false},
		{"KEY", false}, // too short to have a prefix + _KEY
		{"MY_KEY", true},
		{"x_TOKEN", true},
		{"_SECRET", true},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := IsSensitiveKey(tt.key)
			if got != tt.want {
				t.Errorf("IsSensitiveKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestMountPolicyConstants(t *testing.T) {
	// REQ-005-018: verify the three valid mount policy values.
	if MountPolicyNone != "none" {
		t.Errorf("MountPolicyNone = %q, want %q", MountPolicyNone, "none")
	}
	if MountPolicyReadonly != "readonly" {
		t.Errorf("MountPolicyReadonly = %q, want %q", MountPolicyReadonly, "readonly")
	}
	if MountPolicyProject != "project" {
		t.Errorf("MountPolicyProject = %q, want %q", MountPolicyProject, "project")
	}
}

func TestVMStatusConstants(t *testing.T) {
	// REQ-005-007: verify the four valid VM status values.
	if VMStatusCreated != "created" {
		t.Errorf("VMStatusCreated = %q", VMStatusCreated)
	}
	if VMStatusRunning != "running" {
		t.Errorf("VMStatusRunning = %q", VMStatusRunning)
	}
	if VMStatusStopped != "stopped" {
		t.Errorf("VMStatusStopped = %q", VMStatusStopped)
	}
	if VMStatusError != "error" {
		t.Errorf("VMStatusError = %q", VMStatusError)
	}
}

func TestConfigSourceConstants(t *testing.T) {
	// REQ-005-009: verify source labels match expected strings.
	sources := map[string]string{
		"ConfigSourceCLIFlag":        ConfigSourceCLIFlag,
		"ConfigSourceEnvVar":         ConfigSourceEnvVar,
		"ConfigSourceProjectConfig":  ConfigSourceProjectConfig,
		"ConfigSourceUserConfig":     ConfigSourceUserConfig,
		"ConfigSourceVMConfig":       ConfigSourceVMConfig,
		"ConfigSourceBuiltinDefault": ConfigSourceBuiltinDefault,
	}
	for name, val := range sources {
		if val == "" {
			t.Errorf("%s is empty", name)
		}
	}
}
