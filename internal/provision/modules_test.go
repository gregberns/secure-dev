package provision

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// --- LoadBuiltinModules Tests ---
// REQ-006-014: Embedded modules load correctly from //go:embed.
// REQ-006-001: Built-in module set.

func TestLoadBuiltinModules_AllPresent(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	names := make([]string, len(modules))
	for i, m := range modules {
		names[i] = m.Name
	}
	assert.Equal(t, BuiltinModuleNames, names, "all 7 built-in modules must be present in canonical order")
}

func TestLoadBuiltinModules_Count(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	assert.Len(t, modules, 7, "REQ-006-001 specifies exactly 7 built-in modules")
}

func TestLoadBuiltinModules_AllValid(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	for _, m := range modules {
		err := m.Validate()
		assert.NoError(t, err, "module %q should validate successfully", m.Name)
	}
}

func TestLoadBuiltinModules_AllHaveDescriptions(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	for _, m := range modules {
		assert.NotEmpty(t, m.Description, "module %q must have a description", m.Name)
	}
}

func TestLoadBuiltinModules_AllHaveScripts(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	for _, m := range modules {
		assert.NotEmpty(t, m.Scripts, "module %q must have at least one script", m.Name)
	}
}

func TestLoadBuiltinModules_BaseHasNoDependencies(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	base := modules[0]
	assert.Equal(t, "base", base.Name)
	assert.Empty(t, base.DependsOn, "base module has no dependencies")
}

func TestLoadBuiltinModules_NonBaseDependsOnBase(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	for _, m := range modules {
		if m.Name == "base" {
			continue
		}
		assert.Contains(t, m.DependsOn, "base",
			"module %q must depend on base (REQ-006-002)", m.Name)
	}
}

func TestLoadBuiltinModules_BaseInstallsRequiredPackages(t *testing.T) {
	// REQ-006-001: base module installs git, curl, build-essential, ca-certificates, jq, tmux, vim
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)
	base := modules[0]
	assert.Equal(t, "base", base.Name)

	// All scripts combined should mention each required package
	allScripts := ""
	for _, s := range base.Scripts {
		allScripts += s.Script + " "
	}

	requiredPackages := []string{"git", "curl", "build-essential", "ca-certificates", "jq", "tmux", "vim"}
	for _, pkg := range requiredPackages {
		assert.Contains(t, allScripts, pkg,
			"base module must install %q (REQ-006-001)", pkg)
	}
}

func TestLoadBuiltinModules_ClaudeCodeInstallsNodeAndClaude(t *testing.T) {
	// REQ-006-011: claude-code module installs Node.js via nvm and Claude Code CLI
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	var claudeCode *Module
	for i := range modules {
		if modules[i].Name == "claude-code" {
			claudeCode = &modules[i]
			break
		}
	}
	require.NotNil(t, claudeCode)

	allScripts := ""
	for _, s := range claudeCode.Scripts {
		allScripts += s.Script + " "
	}

	assert.Contains(t, allScripts, "nvm", "claude-code must install Node.js via nvm")
	assert.Contains(t, allScripts, "claude-code", "claude-code must install Claude Code CLI")
}

func TestLoadBuiltinModules_GolangHasChecksums(t *testing.T) {
	// REQ-006-016: modules that download must have checksums
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	var golang *Module
	for i := range modules {
		if modules[i].Name == "golang" {
			golang = &modules[i]
			break
		}
	}
	require.NotNil(t, golang)
	assert.NotEmpty(t, golang.Checksums, "golang module must have checksums (REQ-006-016)")
}

func TestLoadBuiltinModules_DownloadModulesHaveChecksums(t *testing.T) {
	// REQ-006-016: Built-in modules that download binaries (tar.gz, .sh, etc.)
	// MUST include checksums. System package downloads (apt) are excluded.
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	// Modules that download standalone binaries/archives
	requiresChecksums := map[string]bool{
		"golang":      true,
		"claude-code": true,
		"rust":        true,
		"github-cli":  true,
	}

	for _, m := range modules {
		if requiresChecksums[m.Name] {
			assert.NotEmpty(t, m.Checksums,
				"module %q downloads binaries and must have checksums (REQ-006-016)", m.Name)
		}
	}
}

func TestLoadBuiltinModules_NoCurlPipeSh(t *testing.T) {
	// REQ-006-016: No built-in module uses curl | sh patterns
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	for _, m := range modules {
		for i, s := range m.Scripts {
			// Check for dangerous pipe patterns
			assert.NotContains(t, s.Script, "curl | sh",
				"module %q scripts[%d]: curl | sh is forbidden (REQ-006-016)", m.Name, i)
			assert.NotContains(t, s.Script, "curl | bash",
				"module %q scripts[%d]: curl | bash is forbidden (REQ-006-016)", m.Name, i)
		}
	}
}

func TestLoadBuiltinModules_AllProbesPresent(t *testing.T) {
	// REQ-006-008: Modules should have readiness probes
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	for _, m := range modules {
		assert.NotNil(t, m.Probe, "module %q should have a readiness probe", m.Name)
		if m.Probe != nil {
			assert.NotEmpty(t, m.Probe.Command, "module %q probe must have a command", m.Name)
		}
	}
}

func TestLoadBuiltinModules_ProbeDefaultsApplied(t *testing.T) {
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	for _, m := range modules {
		if m.Probe != nil {
			m.ApplyProbeDefaults()
			assert.NotZero(t, m.Probe.Interval,
				"module %q probe interval should be non-zero after defaults", m.Name)
			assert.NotZero(t, m.Probe.Timeout,
				"module %q probe timeout should be non-zero after defaults", m.Name)
		}
	}
}

func TestLoadBuiltinModules_AllIdempotent(t *testing.T) {
	// REQ-006-006: Scripts should use command -v guards for idempotency
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	for _, m := range modules {
		if m.Name == "base" {
			// base always runs apt-get which is naturally idempotent
			continue
		}
		allScripts := ""
		for _, s := range m.Scripts {
			allScripts += s.Script + " "
		}
		// At least one script should check if tool is already installed
		assert.True(t,
			strings.Contains(allScripts, "command -v") || strings.Contains(allScripts, "already installed"),
			"module %q should check for existing installation (REQ-006-006)", m.Name)
	}
}

func TestLoadBuiltinModules_ScriptModes(t *testing.T) {
	// REQ-006-005: Scripts must use valid modes
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	for _, m := range modules {
		for i, s := range m.Scripts {
			assert.Contains(t, []ScriptMode{ModeSystem, ModeUser}, s.Mode,
				"module %q scripts[%d] has invalid mode %q", m.Name, i, s.Mode)
		}
	}
}

func TestLoadBuiltinModules_ResolveAllSucceeds(t *testing.T) {
	// REQ-006-004: All built-in modules can be resolved together
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	resolved, err := ResolveAll(modules)
	require.NoError(t, err, "all built-in modules should resolve without circular dependencies")
	assert.Len(t, resolved, len(modules))

	// base must come first
	assert.Equal(t, "base", resolved[0].Name, "base module must be first in execution order")
}

func TestLoadBuiltinModules_ResolveSingleModule(t *testing.T) {
	// REQ-006-002: requesting any module also includes base
	modules, err := LoadBuiltinModules()
	require.NoError(t, err)

	testCases := []struct{ requested, wantFirst, wantLast string }{
		{"golang", "base", "golang"},
		{"docker", "base", "docker"},
		{"claude-code", "base", "claude-code"},
		{"rust", "base", "rust"},
		{"python", "base", "python"},
		{"github-cli", "base", "github-cli"},
	}

	for _, tc := range testCases {
		t.Run(tc.requested, func(t *testing.T) {
			resolved, err := ResolveRequested(modules, []string{tc.requested})
			require.NoError(t, err)
			require.NotEmpty(t, resolved)
			assert.Equal(t, tc.wantFirst, resolved[0].Name,
				"%s: base must come first", tc.requested)
			assert.Equal(t, tc.wantLast, resolved[len(resolved)-1].Name,
				"%s: requested module must come last", tc.requested)
		})
	}
}

// --- Property-Based Tests ---

func TestProperty_LoadBuiltinModules_AlwaysSucceeds(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		modules, err := LoadBuiltinModules()
		if err != nil {
			t.Fatalf("LoadBuiltinModules should never fail: %v", err)
		}
		if len(modules) != 7 {
			t.Fatalf("expected 7 modules, got %d", len(modules))
		}
	})
}

func TestProperty_LoadBuiltinModules_AllNamesValid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		modules, err := LoadBuiltinModules()
		if err != nil {
			t.Fatalf("LoadBuiltinModules failed: %v", err)
		}
		for _, m := range modules {
			if !isValidModuleName(m.Name) {
				t.Fatalf("module name %q is not valid kebab-case", m.Name)
			}
		}
	})
}

func TestProperty_LoadBuiltinModules_ResolveAnySubset(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		idx := rapid.IntRange(0, len(BuiltinModuleNames)-1).Draw(t, "idx")
		requested := []string{BuiltinModuleNames[idx]}

		modules, err := LoadBuiltinModules()
		if err != nil {
			t.Fatalf("LoadBuiltinModules failed: %v", err)
		}

		resolved, err := ResolveRequested(modules, requested)
		if err != nil {
			t.Fatalf("ResolveRequested(%v) failed: %v", requested, err)
		}

		// base is always first
		if resolved[0].Name != "base" {
			t.Fatalf("base must always be first, got %q", resolved[0].Name)
		}
	})
}

func TestProperty_LoadBuiltinModules_NoDuplicateNames(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		modules, err := LoadBuiltinModules()
		if err != nil {
			t.Fatalf("LoadBuiltinModules failed: %v", err)
		}
		seen := make(map[string]bool)
		for _, m := range modules {
			if seen[m.Name] {
				t.Fatalf("duplicate module name: %q", m.Name)
			}
			seen[m.Name] = true
		}
	})
}

func TestProperty_LoadBuiltinModules_ScriptsNotEmpty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		modules, err := LoadBuiltinModules()
		if err != nil {
			t.Fatalf("LoadBuiltinModules failed: %v", err)
		}
		for _, m := range modules {
			for i, s := range m.Scripts {
				if strings.TrimSpace(s.Script) == "" {
					t.Fatalf("module %q scripts[%d] is empty", m.Name, i)
				}
			}
		}
	})
}

// Benchmarks

func BenchmarkLoadBuiltinModules(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := LoadBuiltinModules()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResolveBuiltinModules(b *testing.B) {
	modules, err := LoadBuiltinModules()
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ResolveAll(modules)
		if err != nil {
			b.Fatal(err)
		}
	}
}
