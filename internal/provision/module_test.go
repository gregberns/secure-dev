package provision

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// --- Unit Tests ---

func TestParseModule_ValidMinimal(t *testing.T) {
	yaml := `
name: my-tool
description: "Installs my tool"
scripts:
  - mode: system
    script: |
      apt-get install -y my-tool
`
	m, err := ParseModule([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, "my-tool", m.Name)
	assert.Equal(t, "Installs my tool", m.Description)
	require.Len(t, m.Scripts, 1)
	assert.Equal(t, ModeSystem, m.Scripts[0].Mode)
	assert.Contains(t, m.Scripts[0].Script, "apt-get install")
}

func TestParseModule_ValidFull(t *testing.T) {
	yaml := `
name: golang
description: "Go toolchain"
depends_on:
  - base
scripts:
  - mode: system
    script: |
      curl -fsSL https://go.dev/dl/go.tar.gz -o /tmp/go.tar.gz
      tar -C /usr/local -xzf /tmp/go.tar.gz
  - mode: user
    script: |
      echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile
checksums:
  go.tar.gz: "abc123def456"
probe:
  command: "go version"
  interval: 5s
  timeout: 2m
`
	m, err := ParseModule([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, "golang", m.Name)
	assert.Equal(t, []string{"base"}, m.DependsOn)
	require.Len(t, m.Scripts, 2)
	assert.Equal(t, ModeSystem, m.Scripts[0].Mode)
	assert.Equal(t, ModeUser, m.Scripts[1].Mode)
	assert.Equal(t, map[string]string{"go.tar.gz": "abc123def456"}, m.Checksums)
	require.NotNil(t, m.Probe)
	assert.Equal(t, "go version", m.Probe.Command)
}

func TestValidate_MissingName(t *testing.T) {
	m := &Module{
		Description: "A module",
		Scripts:     []Script{{Mode: ModeSystem, Script: "echo hi"}},
	}
	err := m.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestValidate_MissingDescription(t *testing.T) {
	m := &Module{
		Name:    "test-mod",
		Scripts: []Script{{Mode: ModeSystem, Script: "echo hi"}},
	}
	err := m.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "description is required")
}

func TestValidate_EmptyScripts(t *testing.T) {
	m := &Module{
		Name:        "test-mod",
		Description: "A module",
		Scripts:     []Script{},
	}
	err := m.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at least one script is required")
}

func TestValidate_InvalidMode(t *testing.T) {
	m := &Module{
		Name:        "test-mod",
		Description: "A module",
		Scripts:     []Script{{Mode: "admin", Script: "echo hi"}},
	}
	err := m.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), `must be "system" or "user"`)
}

func TestValidate_EmptyScript(t *testing.T) {
	m := &Module{
		Name:        "test-mod",
		Description: "A module",
		Scripts:     []Script{{Mode: ModeSystem, Script: "   "}},
	}
	err := m.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must not be empty")
}

func TestValidate_InvalidName(t *testing.T) {
	tests := []struct {
		name string
	}{
		{"UPPERCASE"},
		{"has spaces"},
		{"uses_underscores"},
		{"-starts-with-dash"},
		{"ends-with-dash-"},
		{"123starts-with-number"},
		{""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &Module{
				Name:        tc.name,
				Description: "A module",
				Scripts:     []Script{{Mode: ModeSystem, Script: "echo hi"}},
			}
			err := m.Validate()
			assert.Error(t, err, "expected error for name %q", tc.name)
		})
	}
}

func TestValidate_ValidNames(t *testing.T) {
	tests := []struct {
		name string
	}{
		{"base"},
		{"claude-code"},
		{"golang"},
		{"a"},
		{"my-tool-v2"},
		{"tool123"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &Module{
				Name:        tc.name,
				Description: "A module",
				Scripts:     []Script{{Mode: ModeSystem, Script: "echo hi"}},
			}
			err := m.Validate()
			assert.NoError(t, err, "expected no error for name %q", tc.name)
		})
	}
}

func TestValidate_ProbeEmptyCommand(t *testing.T) {
	m := &Module{
		Name:        "test-mod",
		Description: "A module",
		Scripts:     []Script{{Mode: ModeSystem, Script: "echo hi"}},
		Probe:       &Probe{Command: "  "},
	}
	err := m.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "probe.command must not be empty")
}

func TestApplyProbeDefaults(t *testing.T) {
	m := &Module{
		Probe: &Probe{Command: "echo hi"},
	}
	m.ApplyProbeDefaults()
	assert.Equal(t, DefaultProbeInterval, m.Probe.Interval)
	assert.Equal(t, DefaultProbeTimeout, m.Probe.Timeout)
}

func TestApplyProbeDefaults_DoesNotOverwrite(t *testing.T) {
	m := &Module{
		Probe: &Probe{Command: "echo hi", Interval: 10, Timeout: 30},
	}
	m.ApplyProbeDefaults()
	assert.Equal(t, time.Duration(10), m.Probe.Interval)
	assert.Equal(t, time.Duration(30), m.Probe.Timeout)
}

func TestValidateChecksums_ModuleWithCurl(t *testing.T) {
	m := &Module{
		Name: "test-mod",
		Scripts: []Script{
			{Mode: ModeSystem, Script: "curl -fsSL https://example.com/tool -o /tmp/tool"},
		},
	}
	err := m.ValidateChecksums()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "downloads files but has no checksums")
}

func TestValidateChecksums_ModuleWithChecksums(t *testing.T) {
	m := &Module{
		Name: "test-mod",
		Scripts: []Script{
			{Mode: ModeSystem, Script: "curl -fsSL https://example.com/tool -o /tmp/tool"},
		},
		Checksums: map[string]string{"tool": "abc123"},
	}
	err := m.ValidateChecksums()
	assert.NoError(t, err)
}

func TestValidateForRegistration_SelfDependency(t *testing.T) {
	m := &Module{
		Name:      "test-mod",
		DependsOn: []string{"test-mod"},
	}
	err := m.ValidateForRegistration(make(map[string]bool))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "depends on itself")
}

// REQ-006-004: depends_on referencing a nonexistent module produces a validation error.
func TestValidateForRegistration_UnknownDependency(t *testing.T) {
	m := &Module{
		Name:      "my-tool",
		DependsOn: []string{"base", "nonexistent"},
	}
	err := m.ValidateForRegistration(map[string]bool{"base": true})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "depends on unknown module")
	assert.Contains(t, err.Error(), "nonexistent")
}

// REQ-006-004: All known dependencies pass validation.
func TestValidateForRegistration_KnownDependencies(t *testing.T) {
	m := &Module{
		Name:      "my-tool",
		DependsOn: []string{"base", "golang"},
	}
	err := m.ValidateForRegistration(map[string]bool{"base": true, "golang": true})
	assert.NoError(t, err)
}

// REQ-006-007: Duplicate module name produces a validation error.
func TestValidateForRegistration_NameConflict(t *testing.T) {
	m := &Module{
		Name: "base",
	}
	err := m.ValidateForRegistration(map[string]bool{"base": true})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

// REQ-006-004: Module with no dependencies and no name conflict passes.
func TestValidateForRegistration_NoDepsNoConflict(t *testing.T) {
	m := &Module{
		Name: "new-mod",
	}
	err := m.ValidateForRegistration(map[string]bool{"base": true})
	assert.NoError(t, err)
}

// REQ-006-004: All deps unknown when map is empty.
func TestValidateForRegistration_AllDepsUnknown(t *testing.T) {
	m := &Module{
		Name:      "orphan",
		DependsOn: []string{"base", "golang"},
	}
	err := m.ValidateForRegistration(map[string]bool{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "depends on unknown module")
}

// REQ-006-007: Self-dependency takes precedence over unknown dep.
func TestValidateForRegistration_SelfDepBeforeUnknownDep(t *testing.T) {
	m := &Module{
		Name:      "a",
		DependsOn: []string{"a", "ghost"},
	}
	err := m.ValidateForRegistration(map[string]bool{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "depends on itself")
}

// --- Property-Based Tests ---

func TestProperty_ValidModuleAlwaysPassesValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`).Draw(t, "name")
		desc := rapid.StringN(1, 200, 200).Draw(t, "description")
		mode := rapid.SampledFrom([]ScriptMode{ModeSystem, ModeUser}).Draw(t, "mode")
		script := rapid.StringMatching(`[a-z]{1,10}`).Draw(t, "script")

		m := &Module{
			Name:        name,
			Description: desc,
			Scripts:     []Script{{Mode: mode, Script: script}},
		}
		assert.NoError(t, m.Validate())
	})
}

func TestProperty_InvalidNameAlwaysFailsValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate strings that are NOT valid kebab-case.
		name := rapid.OneOf(
			rapid.StringMatching(`^[A-Z]`),
			rapid.StringMatching(`^_`),
			rapid.StringMatching(`^-`),
			rapid.StringMatching(` `),
			rapid.Just(""),
		).Draw(t, "bad_name")

		m := &Module{
			Name:        name,
			Description: "A module",
			Scripts:     []Script{{Mode: ModeSystem, Script: "echo hi"}},
		}
		err := m.Validate()
		assert.Error(t, err, "expected error for invalid name %q", name)
	})
}

func TestProperty_ParseRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`).Draw(t, "name")
		desc := rapid.StringMatching(`^[A-Za-z0-9 ]{1,50}$`).Draw(t, "description")
		scriptContent := rapid.StringMatching(`[a-z]{1,10}`).Draw(t, "script")
		mode := rapid.SampledFrom([]string{"system", "user"}).Draw(t, "mode")

		yaml := strings.Join([]string{
			"name: " + name,
			"description: \"" + desc + "\"",
			"scripts:",
			"  - mode: " + mode,
			"    script: |",
			"      " + scriptContent,
		}, "\n")

		m, err := ParseModule([]byte(yaml))
		require.NoError(t, err)
		assert.Equal(t, name, m.Name)
		assert.Equal(t, desc, m.Description)
		require.Len(t, m.Scripts, 1)
		assert.Equal(t, ScriptMode(mode), m.Scripts[0].Mode)
	})
}

func TestProperty_ChecksumValidationConsistent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hasCurl := rapid.Bool().Draw(t, "has_curl")
		hasChecksums := rapid.Bool().Draw(t, "has_checksums")

		script := "echo hello"
		if hasCurl {
			script = "curl -fsSL https://example.com/file -o /tmp/file"
		}

		m := &Module{
			Name:    "test-mod",
			Scripts: []Script{{Mode: ModeSystem, Script: script}},
		}
		if hasChecksums {
			m.Checksums = map[string]string{"file": "abc123"}
		}

		err := m.ValidateChecksums()
		if hasCurl && !hasChecksums {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	})
}

func TestProperty_ProbeDefaultsIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hasProbe := rapid.Bool().Draw(t, "has_probe")
		m := &Module{
			Name:        "test-mod",
			Description: "test",
			Scripts:     []Script{{Mode: ModeSystem, Script: "echo hi"}},
		}
		if hasProbe {
			m.Probe = &Probe{Command: "echo ok"}
		}

		// Apply defaults twice.
		m.ApplyProbeDefaults()
		if hasProbe {
			interval1 := m.Probe.Interval
			timeout1 := m.Probe.Timeout
			m.ApplyProbeDefaults()
			assert.Equal(t, interval1, m.Probe.Interval, "second ApplyProbeDefaults changed interval")
			assert.Equal(t, timeout1, m.Probe.Timeout, "second ApplyProbeDefaults changed timeout")
		}
	})
}

// TestDefaultModuleNames_SecurityBaseline is a regression guard for SEC-001 /
// REQ-004-006. Every VM created by `sd create` (no --modules flag) MUST be
// default-deny at the network layer. dns-filter (REQ-004-025) and egress
// (REQ-004-006, REQ-004-007) MUST be in DefaultModuleNames. If this test
// fails, do NOT change the assertion -- restore the modules or update spec 004
// (and re-run the security review).
func TestDefaultModuleNames_SecurityBaseline(t *testing.T) {
	required := map[string]string{
		"base":          "REQ-006-002: every module depends transitively on base",
		"ssh-hardening": "REQ-004-026: SSH port forwarding restrictions",
		"dns-filter":    "REQ-004-025: local filtering DNS resolver",
		"egress":        "REQ-004-006/007: default-deny egress firewall + allowlist",
	}
	present := make(map[string]bool, len(DefaultModuleNames))
	for _, name := range DefaultModuleNames {
		present[name] = true
	}
	for name, why := range required {
		assert.Truef(t, present[name],
			"SECURITY REGRESSION: DefaultModuleNames is missing %q (%s). "+
				"Default-deny posture broken. See specs/004-security.md and "+
				"docs/security-004-revival-audit.md SEC-001. Current set: %v",
			name, why, DefaultModuleNames)
	}
}

// TestDefaultModuleNames_ResolveOrdering verifies the baseline modules
// resolve with correct topo order. REQ-004-006.
func TestDefaultModuleNames_ResolveOrdering(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	resolved, err := ResolveRequested(modules, DefaultModuleNames)
	require.NoError(t, err)

	pos := make(map[string]int, len(resolved))
	for i, m := range resolved {
		pos[m.Name] = i
	}

	for _, name := range []string{"base", "ssh-hardening", "dns-filter", "egress"} {
		_, ok := pos[name]
		require.Truef(t, ok, "resolved set must contain %q (got %d modules)", name, len(resolved))
	}

	assert.Less(t, pos["base"], pos["dns-filter"], "base before dns-filter")
	assert.Less(t, pos["base"], pos["egress"], "base before egress")
	assert.Less(t, pos["dns-filter"], pos["egress"], "dns-filter before egress")
}
