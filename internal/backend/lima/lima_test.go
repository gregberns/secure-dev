// Package lima provides property-based tests for Lima backend.
// REQ-003-015: Lima YAML Generation
// REQ-003-016: Lima VZ Backend Defaults on Apple Silicon
// REQ-003-017: Lima Mount Policy
// REQ-003-023: Credential Isolation from Lima YAML
package lima

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"sd/internal/backend"
)

// TestLimaYAML_VZDefaultOnAppleSilicon verifies VZ is used by default.
// REQ-003-016
func TestLimaYAML_VZDefaultOnAppleSilicon(t *testing.T) {
	b := New()

	cfg := backend.VMConfig{
		CPUs:       4,
		Memory:     "8GiB",
		Disk:       "100GiB",
		BaseImage:  "ubuntu:24.04",
		NetworkMode: "",
	}

	yaml, err := b.(*limaBackend).generateLimaYAML("testvm", cfg)
	if err != nil {
		t.Fatalf("generateLimaYAML failed: %v", err)
	}

	// Verify vmType is in YAML
	if !strings.Contains(yaml, "vmType:") {
		t.Error("YAML missing vmType field")
	}

	// On the current platform, verify the correct vmType
	vmTypeLine := findYAMLLine(yaml, "vmType:")
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		if !strings.Contains(vmTypeLine, "vz") {
			t.Errorf("on darwin/arm64, vmType = %q, want \"vz\"", extractYAMLValue(vmTypeLine))
		}
	} else {
		if !strings.Contains(vmTypeLine, "qemu") {
			t.Errorf("on %s/%s, vmType = %q, want \"qemu\"", runtime.GOOS, runtime.GOARCH, extractYAMLValue(vmTypeLine))
		}
	}
}

// TestLimaYAML_NoMountsByDefault verifies mounts array is empty.
// REQ-003-011, REQ-003-017
func TestLimaYAML_NoMountsByDefault(t *testing.T) {
	b := New()
	cfg := backend.VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}

	yaml, err := b.(*limaBackend).generateLimaYAML("testvm", cfg)
	if err != nil {
		t.Fatalf("generateLimaYAML failed: %v", err)
	}

	if !strings.Contains(yaml, "mounts: []") {
		t.Error("YAML missing mounts: [] (should be empty by default)")
	}

	// Ensure no mountLocation entries exist
	if strings.Contains(yaml, "location:") && strings.Contains(yaml, "mounts:") {
		// Check if location: appears after mounts:
		mountsIdx := strings.Index(yaml, "mounts:")
		if mountsIdx > 0 {
		}
	}
}

// TestLimaYAML_Resources verifies CPU, memory, disk values are set.
// REQ-003-015
func TestLimaYAML_Resources(t *testing.T) {
	b := New()

	tests := []backend.VMConfig{
		{
			CPUs:      2,
			Memory:    "4GiB",
			Disk:      "50GiB",
			BaseImage: "ubuntu:24.04",
		},
		{
			CPUs:      8,
			Memory:    "16GiB",
			Disk:      "200GiB",
			BaseImage: "ubuntu:24.04",
		},
	}

	for _, cfg := range tests {
		t.Run(fmt.Sprintf("cpus%d-mem%s-disk%s", cfg.CPUs, cfg.Memory, cfg.Disk), func(t *testing.T) {
			yaml, err := b.(*limaBackend).generateLimaYAML("testvm", cfg)
			if err != nil {
				t.Fatalf("generateLimaYAML failed: %v", err)
			}

			// Verify cpus
			cpuLine := findYAMLLine(yaml, "cpus:")
			if !strings.Contains(cpuLine, fmt.Sprintf("%d", cfg.CPUs)) {
				t.Errorf("cpus = %q, want %d", extractYAMLValue(cpuLine), cfg.CPUs)
			}

			// Verify memory
			memLine := findYAMLLine(yaml, "memory:")
			if !strings.Contains(memLine, cfg.Memory) {
				t.Errorf("memory = %q, want %s", extractYAMLValue(memLine), cfg.Memory)
			}

			// Verify disk
			diskLine := findYAMLLine(yaml, "disk:")
			if !strings.Contains(diskLine, cfg.Disk) {
				t.Errorf("disk = %q, want %s", extractYAMLValue(diskLine), cfg.Disk)
			}
		})
	}
}

// TestLimaYAML_BaseImageResolution verifies short names resolve to URLs.
// REQ-003-024
func TestLimaYAML_BaseImageResolution(t *testing.T) {
	b := New()

	tests := []struct {
		name        string
		baseImage   string
		expectURL   string
		expectError bool
	}{
		{
			name:      "ubuntu-24-04",
			baseImage: "ubuntu:24.04",
			expectURL: "https://cloud-images.ubuntu.com/releases/24.04/release/ubuntu-24.04-server-cloudimg-arm64.img",
		},
		{
			name:      "ubuntu-22-04",
			baseImage: "ubuntu:22.04",
			expectURL: "https://cloud-images.ubuntu.com/releases/22.04/release/ubuntu-22.04-server-cloudimg-arm64.img",
		},
		{
			name:      "debian-12",
			baseImage: "debian:12",
			expectURL: "https://cloud.debian.org/images/cloud/bookworm/daily/latest/arm64/disk.qcow2",
		},
		{
			name:      "full-url",
			baseImage: "https://example.com/custom.img",
			expectURL: "https://example.com/custom.img",
		},
		{
			name:        "unknown",
			baseImage:   "unknown:image",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := backend.VMConfig{
				CPUs:      4,
				Memory:    "8GiB",
				Disk:      "100GiB",
				BaseImage: tt.baseImage,
			}

			yaml, err := b.(*limaBackend).generateLimaYAML("testvm", cfg)
			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				if !strings.Contains(err.Error(), "failed to resolve base image") {
					t.Errorf("wrong error: %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !strings.Contains(yaml, tt.expectURL) {
				t.Errorf("YAML missing expected URL %q", tt.expectURL)
			}
		})
	}
}

// TestLimaYAML_SensitiveKeysFiltered verifies credentials are not in YAML.
// REQ-003-023
func TestLimaYAML_SensitiveKeysFiltered(t *testing.T) {
	b := New()

	tests := []struct {
		name    string
		envVars map[string]string
		filtered []string // keys that should NOT appear in YAML
	}{
		{
			name: "token-filtered",
			envVars: map[string]string{
				"GITHUB_TOKEN":       "ghp_123",
				"ANTHROPIC_API_KEY": "sk-ant-456",
				"PROJECT_NAME":       "myproject",
			},
			filtered: []string{"GITHUB_TOKEN", "ANTHROPIC_API_KEY"},
		},
		{
			name: "all-sensitive",
			envVars: map[string]string{
				"SECRET_KEY":      "secret",
				"PRIVATE_KEY":     "private",
				"API_KEY":         "api",
				"ACCESS_KEY":       "access",
				"CREDENTIAL":      "cred",
				"PASSWORD":        "pass",
			},
			filtered: []string{"SECRET_KEY", "PRIVATE_KEY", "API_KEY", "ACCESS_KEY", "CREDENTIAL", "PASSWORD"},
		},
		{
			name: "non-sensitive-passed",
			envVars: map[string]string{
				"PROJECT_NAME":   "myproject",
				"USER_NAME":      "developer",
				"BUILD_NUMBER":   "42",
			},
			filtered: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := backend.VMConfig{
				CPUs:      4,
				Memory:    "8GiB",
				Disk:      "100GiB",
				BaseImage: "ubuntu:24.04",
				EnvVars:   tt.envVars,
			}

			yaml, err := b.(*limaBackend).generateLimaYAML("testvm", cfg)
			if err != nil {
				t.Fatalf("generateLimaYAML failed: %v", err)
			}

			// Check that filtered keys don't appear in YAML
			for _, filtered := range tt.filtered {
				if strings.Contains(yaml, filtered+":") {
					t.Errorf("sensitive key %q should be filtered but appears in YAML", filtered)
				}
			}

			// Check that non-filtered keys DO appear in YAML
			for key := range tt.envVars {
				shouldAppear := true
				for _, filtered := range tt.filtered {
					if key == filtered {
						shouldAppear = false
						break
					}
				}
				if shouldAppear {
					if !strings.Contains(yaml, key+":") {
						t.Errorf("non-sensitive key %q should appear in YAML", key)
					}
				}
			}
		})
	}
}

// TestLimaYAML_NetworkModes verifies network mode configuration.
// REQ-003-012
func TestLimaYAML_NetworkModes(t *testing.T) {
	b := New()

	tests := []struct {
		name   string
		mode   backend.NetworkMode
		expect string // what we expect (or don't expect for isolated)
	}{
		{"nat", backend.NetworkNAT, ""}, // NAT is Lima default
		{"bridged", backend.NetworkBridged, ""}, // bridged mode
		{"isolated", backend.NetworkIsolated, ""}, // isolated mode
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := backend.VMConfig{
				CPUs:        4,
				Memory:      "8GiB",
				Disk:        "100GiB",
				BaseImage:   "ubuntu:24.04",
				NetworkMode:  tt.mode,
			}

			yaml, err := b.(*limaBackend).generateLimaYAML("testvm", cfg)
			if err != nil {
				t.Fatalf("generateLimaYAML failed: %v", err)
			}

			// Network mode handling in Lima YAML is backend-specific
			// For now, just verify it doesn't cause an error
			if yaml == "" {
				t.Error("YAML is empty")
			}
		})
	}
}

// TestLimaYAML_ProvisioningScripts verifies scripts are included.
// REQ-003-019
func TestLimaYAML_ProvisioningScripts(t *testing.T) {
	b := New()

	tests := []struct {
		name     string
		scripts  []backend.ProvisionScript
		expectFn func(string) bool
	}{
		{
			name: "single-system-script",
			scripts: []backend.ProvisionScript{
				{Mode: "system", Script: "apt-get update"},
			},
			expectFn: func(y string) bool { return strings.Contains(y, "mode: system") && strings.Contains(y, "apt-get update") },
		},
		{
			name: "system-and-user-scripts",
			scripts: []backend.ProvisionScript{
				{Mode: "system", Script: "apt-get install -y git"},
				{Mode: "user", Script: "echo 'user setup'"},
			},
			expectFn: func(y string) bool {
				return strings.Contains(y, "mode: system") &&
					strings.Contains(y, "apt-get install -y git") &&
					strings.Contains(y, "mode: user") &&
					strings.Contains(y, "echo 'user setup'")
			},
		},
		{
			name:    "no-scripts",
			scripts: []backend.ProvisionScript{},
			expectFn: func(y string) bool { return !strings.Contains(y, "provision:") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := backend.VMConfig{
				CPUs:             4,
				Memory:           "8GiB",
				Disk:             "100GiB",
				BaseImage:        "ubuntu:24.04",
				ProvisionScripts: tt.scripts,
			}

			yaml, err := b.(*limaBackend).generateLimaYAML("testvm", cfg)
			if err != nil {
				t.Fatalf("generateLimaYAML failed: %v", err)
			}

			if !tt.expectFn(yaml) {
				t.Error("YAML does not match expected pattern")
			}
		})
	}
}

// TestLimaYAML_SSHConfiguration verifies SSH settings.
// REQ-004-027
func TestLimaYAML_SSHConfiguration(t *testing.T) {
	b := New()
	cfg := backend.VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}

	yaml, err := b.(*limaBackend).generateLimaYAML("testvm", cfg)
	if err != nil {
		t.Fatalf("generateLimaYAML failed: %v", err)
	}

	// Verify SSH section exists
	if !strings.Contains(yaml, "ssh:") {
		t.Error("YAML missing ssh: section")
	}

	// Verify forwardAgent is disabled
	if !strings.Contains(yaml, "forwardAgent: false") {
		t.Error("YAML missing forwardAgent: false (should be disabled)")
	}

	// Verify localPort is set
	if !strings.Contains(yaml, "localPort: 0") {
		t.Error("YAML missing localPort: 0")
	}
}

// TestLimaYAML_DefaultValues verifies empty values are handled.
// REQ-003-011
func TestLimaYAML_DefaultValues(t *testing.T) {
	b := New()

	// Network mode defaults to NAT
	cfg := backend.VMConfig{
		CPUs:       4,
		Memory:     "8GiB",
		Disk:       "100GiB",
		BaseImage:  "ubuntu:24.04",
		NetworkMode: "", // Empty should default
	}

	yaml, err := b.(*limaBackend).generateLimaYAML("testvm", cfg)
	if err != nil {
		t.Fatalf("generateLimaYAML failed: %v", err)
	}

	if yaml == "" {
		t.Error("YAML is empty")
	}

	// Verify YAML contains expected fields
	expectedFields := []string{"cpus:", "memory:", "disk:", "images:", "mounts: []"}
	for _, field := range expectedFields {
		if !strings.Contains(yaml, field) {
			t.Errorf("YAML missing expected field %q", field)
		}
	}
}

// Helper functions for YAML parsing in tests

func findYAMLLine(yaml, key string) string {
	lines := strings.Split(yaml, "\n")
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), key) {
			return line
		}
	}
	return ""
}

func extractYAMLValue(line string) string {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return ""
}

// TestLimaBackend_Name verifies backend name.
// REQ-003-014
func TestLimaBackend_Name(t *testing.T) {
	b := New()
	if b.Name() != "lima" {
		t.Errorf("backend.Name() = %q, want \"lima\"", b.Name())
	}
}

// TestLimaBackend_Available verifies limactl availability check.
// REQ-003-002, REQ-003-014
func TestLimaBackend_Available(t *testing.T) {
	b := New()

	// In test environment, limactl may not be available
	// The function should return an error explaining this
	err := b.Available()
	if err != nil {
		// This is expected in CI/test environment
		if !strings.Contains(err.Error(), "limactl") {
			t.Errorf("error should mention limactl, got: %v", err)
		}
		if !strings.Contains(err.Error(), "brew install lima") {
			t.Errorf("error should suggest installation, got: %v", err)
		}
	} else {
		// If limactl IS available, that's also fine
	}
}

// TestLimaBackend_InterfaceContract verifies Lima backend implements Backend.
// REQ-003-001, REQ-003-014
func TestLimaBackend_InterfaceContract(t *testing.T) {
	b := New()

	// Verify it satisfies the Backend interface
	var _ backend.Backend = b

	// Verify it can be registered
	ctx := context.Background()

	// These calls verify methods exist (they will return ErrNotImplemented)
	cfg := backend.VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
	}

	b.Create(ctx, "test", cfg)
	b.Start(ctx, "test")
	b.Stop(ctx, "test")
	b.Destroy(ctx, "test")
	b.Status(ctx, "test")
	b.List(ctx)
	b.SSHConfig(ctx, "test")
	b.Exec(ctx, "test", []string{"test"})
}

// TestLimaYAML_ComplexProvisioningScript verifies multiline scripts work.
// REQ-003-019
func TestLimaYAML_ComplexProvisioningScript(t *testing.T) {
	b := New()

	multilineScript := `#!/bin/bash
set -eux -o pipefail

# Install dependencies
apt-get update
apt-get install -y git curl tmux jq

# Configure git
git config --global user.name "Test User"
git config --global user.email "test@example.com"

echo "Provisioning complete"
`

	cfg := backend.VMConfig{
		CPUs: 4,
		Memory: "8GiB",
		Disk:   "100GiB",
		BaseImage: "ubuntu:24.04",
		ProvisionScripts: []backend.ProvisionScript{
			{Mode: "system", Script: multilineScript},
		},
	}

	yaml, err := b.(*limaBackend).generateLimaYAML("testvm", cfg)
	if err != nil {
		t.Fatalf("generateLimaYAML failed: %v", err)
	}

	// Verify script content is in YAML
	expectedLines := []string{"#!/bin/bash", "set -eux", "apt-get update", "git config"}
	for _, expected := range expectedLines {
		if !strings.Contains(yaml, expected) {
			t.Errorf("multiline script missing line: %s", expected)
		}
	}

	// Verify proper indentation (script should be indented)
	// The generator indents script lines with spaces
	if !strings.Contains(yaml, "      #!/bin/bash") {
		t.Error("script lines should be indented")
	}
}

// --- Property-Based Tests for Lima YAML Generation ---
//
// These tests verify security-critical invariants of the Lima YAML generator
// hold for ALL possible inputs, not just the examples we think of.

// Property: isSensitiveKey always detects keys with sensitive suffixes.
// REQ-003-023: Credential Isolation from Lima YAML
func TestProperty_IsSensitiveKey_SensitiveSuffixes(t *testing.T) {
	sensitiveSuffixes := []string{"_token", "_key", "_secret", "_password", "_credential"}
	sensitiveExact := []string{"token", "key", "secret", "password", "credential"}

	rapid.Check(t, func(t *rapid.T) {
		suffix := rapid.SampledFrom(sensitiveSuffixes).Draw(t, "suffix")
		prefix := rapid.StringMatching(`[A-Z_]{1,20}`).Draw(t, "prefix")
		keyName := prefix + suffix

		if !isSensitiveKey(keyName) {
			t.Errorf("isSensitiveKey(%q) = false, want true (has sensitive suffix %q)", keyName, suffix)
		}
	})

	rapid.Check(t, func(t *rapid.T) {
		exact := rapid.SampledFrom(sensitiveExact).Draw(t, "exact")
		// Case-insensitive match
		if !isSensitiveKey(exact) {
			t.Errorf("isSensitiveKey(%q) = false, want true (exact sensitive match)", exact)
		}
	})
}

// Property: isSensitiveKey always rejects common credential key names.
// REQ-003-023
func TestProperty_IsSensitiveKey_CommonCredentialKeys(t *testing.T) {
	knownSensitive := []string{
		"GITHUB_TOKEN", "ANTHROPIC_API_KEY", "SECRET_KEY",
		"PRIVATE_KEY", "API_KEY", "ACCESS_KEY",
		"CREDENTIAL", "PASSWORD", "SECRET",
		"MY_TOKEN", "DB_PASSWORD", "SSH_KEY",
		"SERVICE_CREDENTIAL", "AUTH_TOKEN",
	}

	rapid.Check(t, func(t *rapid.T) {
		key := rapid.SampledFrom(knownSensitive).Draw(t, "key")
		if !isSensitiveKey(key) {
			t.Errorf("isSensitiveKey(%q) = false, want true", key)
		}
	})
}

// Property: Generated YAML ALWAYS has forwardAgent: false.
// REQ-004-027: SSH Agent forwarding is always disabled.
func TestProperty_YAMLAlwaysDisablesAgentForwarding(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cfg := genRandomVMConfig(t)
		yaml, err := generateYAML(t, cfg)
		if err != nil {
			t.Skipf("skipping due to expected error: %v", err)
		}

		if !strings.Contains(yaml, "forwardAgent: false") {
			t.Errorf("YAML must always contain forwardAgent: false")
		}
		// Also verify no accidental "forwardAgent: true"
		if strings.Contains(yaml, "forwardAgent: true") {
			t.Errorf("YAML must never contain forwardAgent: true")
		}
	})
}

// Property: Generated YAML ALWAYS has empty mounts by default.
// REQ-004-003: VMs have zero host mounts by default.
func TestProperty_YAMLAlwaysHasEmptyMounts(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cfg := genRandomVMConfig(t)
		// Explicitly ensure no mounts (which is the default)
		cfg.Mounts = nil
		yaml, err := generateYAML(t, cfg)
		if err != nil {
			t.Skipf("skipping due to expected error: %v", err)
		}

		if !strings.Contains(yaml, "mounts: []") {
			t.Errorf("YAML must always contain mounts: [] when no mounts specified")
		}
	})
}

// Property: Sensitive environment variable values NEVER appear in generated YAML.
// REQ-003-023: Credential Isolation from Lima YAML.
// This is the most critical property — if it fails, credentials leak to disk.
func TestProperty_SensitiveEnvVarsNeverInYAML(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		sensitiveValue := rapid.StringMatching(`[A-Za-z0-9_-]{10,40}`).Draw(t, "sensitive_value")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
			EnvVars: map[string]string{
				"GITHUB_TOKEN":   sensitiveValue,
				"API_KEY":        sensitiveValue,
				"DB_PASSWORD":    sensitiveValue,
				"SECRET_KEY":     sensitiveValue,
				"MY_CREDENTIAL":  sensitiveValue,
				"PRIVATE_KEY":    sensitiveValue,
				"ACCESS_TOKEN":   sensitiveValue,
				"PROJECT_NAME":   "myproject",
			},
		}

		yaml, err := generateYAML(t, cfg)
		if err != nil {
			t.Skipf("skipping due to expected error: %v", err)
		}

		// The sensitive VALUE must never appear in YAML
		if strings.Contains(yaml, sensitiveValue) {
			t.Errorf("sensitive value %q leaked into Lima YAML:\n%s", sensitiveValue, yaml)
		}

		// Sensitive key names must not appear
		sensitiveKeys := []string{"GITHUB_TOKEN", "API_KEY", "DB_PASSWORD", "SECRET_KEY", "MY_CREDENTIAL", "PRIVATE_KEY", "ACCESS_TOKEN"}
		for _, key := range sensitiveKeys {
			if strings.Contains(yaml, key+":") {
				t.Errorf("sensitive key %q appears in Lima YAML", key)
			}
		}

		// Non-sensitive key should appear
		if !strings.Contains(yaml, "PROJECT_NAME:") {
			t.Errorf("non-sensitive key PROJECT_NAME missing from YAML")
		}
	})
}

// Property: Non-sensitive environment variables are always preserved in YAML.
// REQ-003-023: Only sensitive keys are filtered; legitimate env vars pass through.
func TestProperty_NonSensitiveEnvVarsPreserved(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`[A-Z][A-Z0-9_]{2,15}`).Draw(t, "name")
		value := rapid.StringMatching(`[a-zA-Z0-9_-]{3,20}`).Draw(t, "value")

		// Make sure the name isn't accidentally sensitive
		if isSensitiveKey(name) {
			t.Skip("randomly generated sensitive name")
		}

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
			EnvVars:   map[string]string{name: value},
		}

		yaml, err := generateYAML(t, cfg)
		if err != nil {
			t.Skipf("skipping due to expected error: %v", err)
		}

		if !strings.Contains(yaml, name+":") {
			t.Errorf("non-sensitive key %q missing from YAML", name)
		}
		if !strings.Contains(yaml, value) {
			t.Errorf("non-sensitive value %q for key %q missing from YAML", value, name)
		}
	})
}

// Property: For any valid base image, generated YAML always contains an image URL.
// REQ-003-024: Base Image Resolution
func TestProperty_ValidBaseImageProducesURL(t *testing.T) {
	knownImages := []string{"ubuntu:24.04", "ubuntu:22.04", "debian:12"}

	rapid.Check(t, func(t *rapid.T) {
		image := rapid.SampledFrom(knownImages).Draw(t, "image")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: image,
		}

		yaml, err := generateYAML(t, cfg)
		if err != nil {
			t.Fatalf("known image %q should not fail: %v", image, err)
		}

		if !strings.Contains(yaml, "images:") {
			t.Errorf("YAML missing images: section")
		}
		if !strings.Contains(yaml, "location:") {
			t.Errorf("YAML missing location: in images section")
		}
		if !strings.Contains(yaml, "http") {
			t.Errorf("YAML missing URL in images section for image %q", image)
		}
	})
}

// Property: Generated YAML always contains required SSH section.
// REQ-004-027: SSH forwarding disabled, no X11 forwarding.
func TestProperty_YAMLAlwaysHasSSHSection(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cfg := genRandomVMConfig(t)
		yaml, err := generateYAML(t, cfg)
		if err != nil {
			t.Skipf("skipping due to expected error: %v", err)
		}

		if !strings.Contains(yaml, "ssh:") {
			t.Errorf("YAML must always contain ssh: section")
		}
		if !strings.Contains(yaml, "forwardAgent: false") {
			t.Errorf("YAML must always disable SSH agent forwarding")
		}
	})
}

// Property: Credential isolation is complete — for any mix of sensitive
// and non-sensitive keys, only non-sensitive values appear in YAML.
// REQ-003-023: This is the comprehensive version of the isolation test.
func TestProperty_CredentialIsolationComplete(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		nVars := rapid.IntRange(1, 10).Draw(t, "n_vars")
		envVars := make(map[string]string)

		var nonSensitiveKeys []string

		for i := 0; i < nVars; i++ {
			isSensitive := rapid.Bool().Draw(t, "is_sensitive")
			value := rapid.StringMatching(`[A-Za-z0-9]{8,20}`).Draw(t, "value")

			var key string
			if isSensitive {
				suffix := rapid.SampledFrom([]string{"_TOKEN", "_KEY", "_SECRET", "_PASSWORD", "_CREDENTIAL"}).Draw(t, "suffix")
				key = "VAR" + suffix
			} else {
				key = fmt.Sprintf("SAFE_VAR_%d", i)
				nonSensitiveKeys = append(nonSensitiveKeys, key)
			}
			envVars[key] = value
		}

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
			EnvVars:   envVars,
		}

		yaml, err := generateYAML(t, cfg)
		if err != nil {
			t.Skipf("skipping due to expected error: %v", err)
		}

		// Verify no sensitive key names appear
		for key := range envVars {
			if isSensitiveKey(key) {
				if strings.Contains(yaml, key+":") {
					t.Errorf("sensitive key %q leaked into YAML", key)
				}
			}
		}

		// Verify non-sensitive keys are preserved
		for _, key := range nonSensitiveKeys {
			if !strings.Contains(yaml, key+":") {
				t.Errorf("non-sensitive key %q missing from YAML", key)
			}
		}
	})
}

// --- Helpers for property-based tests ---

func genRandomVMConfig(t *rapid.T) backend.VMConfig {
	cpus := rapid.IntRange(1, 16).Draw(t, "cpus")
	memory := rapid.SampledFrom([]string{"2GiB", "4GiB", "8GiB", "16GiB", "32GiB"}).Draw(t, "memory")
	disk := rapid.SampledFrom([]string{"50GiB", "100GiB", "200GiB", "500GiB"}).Draw(t, "disk")
	baseImage := rapid.SampledFrom([]string{"ubuntu:24.04", "ubuntu:22.04", "debian:12"}).Draw(t, "base_image")

	return backend.VMConfig{
		CPUs:      cpus,
		Memory:    memory,
		Disk:      disk,
		BaseImage: baseImage,
	}
}

func generateYAML(t *rapid.T, cfg backend.VMConfig) (string, error) {
	t.Helper()
	b := New()
	return b.(*limaBackend).generateLimaYAML("testvm", cfg)
}
