// Package provision - dedicated property-based tests for Module invariants.
// REQ-006-003: Module definition format validation.
// REQ-006-004: Dependency validation.
// REQ-006-007: Custom module name conflict detection.
// REQ-006-008: Readiness probes.
// REQ-006-016: Checksum verification for downloaded binaries.
//
// This file contains deeper invariant tests beyond those in module_test.go,
// focusing on name pattern characterization, dependency validation,
// download detection, JSON round-trips, and error message quality.
package provision

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// validModule returns a minimally valid Module for property-based testing.
func validModule() *Module {
	return &Module{
		Name:        "test-mod",
		Description: "A test module",
		Scripts:     []Script{{Mode: ModeSystem, Script: "echo hello"}},
	}
}

// ============================================================
// Module Name Pattern Characterization (REQ-006-003)
// ============================================================

// Property: Single lowercase letter names are always valid.
func TestProperty_SingleLetterNameValid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		letter := rapid.StringMatching(`^[a-z]$`).Draw(t, "letter")
		m := validModule()
		m.Name = letter
		assert.NoError(t, m.Validate(), "single lowercase letter %q should be valid", letter)
	})
}

// Property: Names starting with a digit are always invalid.
func TestProperty_NameStartingWithDigitInvalid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`^[0-9][a-z0-9-]{0,10}`).Draw(t, "name")
		m := validModule()
		m.Name = name
		err := m.Validate()
		assert.Error(t, err, "name %q starting with digit should be invalid", name)
	})
}

// Property: Names with uppercase letters are always invalid.
func TestProperty_NameWithUppercaseInvalid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`[A-Z]{1,3}`).Draw(t, "name")
		m := validModule()
		m.Name = name
		err := m.Validate()
		assert.Error(t, err, "name %q with uppercase should be invalid", name)
	})
}

// Property: Names with underscores are always invalid.
func TestProperty_NameWithUnderscoreInvalid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		prefix := rapid.StringMatching(`[a-z]{1,5}`).Draw(t, "prefix")
		suffix := rapid.StringMatching(`[a-z]{1,5}`).Draw(t, "suffix")
		m := validModule()
		m.Name = prefix + "_" + suffix
		err := m.Validate()
		assert.Error(t, err, "name with underscore %q should be invalid", m.Name)
	})
}

// Property: Names with spaces are always invalid.
func TestProperty_NameWithSpacesInvalid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		prefix := rapid.StringMatching(`[a-z]{1,5}`).Draw(t, "prefix")
		suffix := rapid.StringMatching(`[a-z]{1,5}`).Draw(t, "suffix")
		m := validModule()
		m.Name = prefix + " " + suffix
		err := m.Validate()
		assert.Error(t, err, "name with space %q should be invalid", m.Name)
	})
}

// Property: Names starting with a hyphen are always invalid.
func TestProperty_NameStartingWithHyphenInvalid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rest := rapid.StringMatching(`[a-z0-9-]{1,10}`).Draw(t, "rest")
		m := validModule()
		m.Name = "-" + rest
		err := m.Validate()
		assert.Error(t, err, "name starting with hyphen %q should be invalid", m.Name)
	})
}

// Property: Multi-segment kebab-case names are always valid.
func TestProperty_MultiSegmentKebabValid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		segment := rapid.StringMatching(`[a-z][a-z0-9]{0,8}`).Draw(t, "segment")
		nSegments := rapid.IntRange(2, 5).Draw(t, "n")
		name := segment
		for i := 1; i < nSegments; i++ {
			seg := rapid.StringMatching(`[a-z0-9][a-z0-9]{0,8}`).Draw(t, "seg")
			name = name + "-" + seg
		}
		m := validModule()
		m.Name = name
		assert.NoError(t, m.Validate(), "kebab-case name %q should be valid", name)
	})
}

// ============================================================
// Dependency Validation (REQ-006-004)
// ============================================================

// Property: Dependencies with valid names never cause Validate to fail
// (on the name check alone).
func TestProperty_ValidDependencyNamesNeverFailOnNameCheck(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		nDeps := rapid.IntRange(0, 5).Draw(t, "nDeps")
		deps := make([]string, nDeps)
		for i := 0; i < nDeps; i++ {
			deps[i] = rapid.StringMatching(`[a-z][a-z0-9]*(-[a-z0-9]+)*`).Draw(t, "dep")
		}
		m := validModule()
		m.DependsOn = deps
		// Validation should not fail on dependency name grounds
		err := m.Validate()
		assert.NoError(t, err, "valid dependency names should pass: %v", deps)
	})
}

// Property: Dependencies with invalid names always fail.
func TestProperty_InvalidDependencyNamesAlwaysFail(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		badDep := rapid.OneOf(
			rapid.StringMatching(`[A-Z]{1,5}`),
			rapid.StringMatching(`[0-9][a-z]{1,5}`),
			rapid.StringMatching(`[a-z]+_[a-z]+`),
			rapid.StringMatching(` `),
		).Draw(t, "badDep")
		m := validModule()
		m.DependsOn = []string{badDep}
		err := m.Validate()
		require.Error(t, err, "invalid dependency %q should fail", badDep)
		assert.Contains(t, err.Error(), "depends_on")
	})
}

// Property: Self-dependency always fails ValidateForRegistration.
func TestProperty_SelfDependencyAlwaysFailsRegistration(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`[a-z][a-z0-9]*(-[a-z0-9]+)*`).Draw(t, "name")
		m := validModule()
		m.Name = name
		m.DependsOn = []string{name}
		err := m.ValidateForRegistration(make(map[string]bool))
		require.Error(t, err, "module %q depending on itself should fail", name)
		assert.Contains(t, err.Error(), "depends on itself")
	})
}

// Property: ValidateForRegistration with no self-dep always succeeds.
func TestProperty_NoSelfDependencyAlwaysPassesRegistration(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		m := validModule()
		m.DependsOn = []string{"other-module", "base"}
		err := m.ValidateForRegistration(map[string]bool{
			"other-module": true,
			"base":         true,
		})
		assert.NoError(t, err, "no self-dep should pass registration check")
	})
}

// ============================================================
// Download Detection (REQ-006-016)
// ============================================================

// Property: Modules without curl or wget never require checksums.
func TestProperty_NoDownloadsNeverRequireChecksums(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		script := rapid.StringMatching(`[a-z][a-z -]{5,30}`).Draw(t, "script")
		// Ensure no curl or wget
		if strings.Contains(script, "curl") || strings.Contains(script, "wget") {
			t.Skip("script contains download command")
		}
		m := validModule()
		m.Scripts = []Script{{Mode: ModeSystem, Script: script}}
		m.Checksums = nil
		assert.NoError(t, m.ValidateChecksums(),
			"module without downloads should not require checksums")
	})
}

// Property: Modules with curl always require checksums when none are provided.
func TestProperty_CurlAlwaysRequiresChecksumsWhenMissing(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		url := rapid.StringMatching(`https://[a-z0-9.-/]{5,30}`).Draw(t, "url")
		m := validModule()
		m.Scripts = []Script{{Mode: ModeSystem, Script: "curl -fsSL " + url + " -o /tmp/file"}}
		m.Checksums = nil
		err := m.ValidateChecksums()
		require.Error(t, err, "module with curl should require checksums")
		assert.Contains(t, err.Error(), "downloads files but has no checksums")
	})
}

// Property: Modules with wget always require checksums when none are provided.
func TestProperty_WgetAlwaysRequiresChecksumsWhenMissing(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		url := rapid.StringMatching(`https://[a-z0-9.-/]{5,30}`).Draw(t, "url")
		m := validModule()
		m.Scripts = []Script{{Mode: ModeUser, Script: "wget " + url}}
		m.Checksums = nil
		err := m.ValidateChecksums()
		require.Error(t, err, "module with wget should require checksums")
	})
}

// Property: Script mode does not affect download detection.
func TestProperty_ScriptModeDoesNotAffectDownloadDetection(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mode := rapid.SampledFrom([]ScriptMode{ModeSystem, ModeUser}).Draw(t, "mode")
		m := validModule()
		m.Scripts = []Script{{Mode: mode, Script: "curl -fsSL https://example.com/file"}}
		m.Checksums = nil
		err := m.ValidateChecksums()
		require.Error(t, err, "curl detection should work regardless of script mode")
	})
}

// Property: Modules with curl and non-empty checksums always pass.
func TestProperty_CurlWithChecksumsAlwaysPasses(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hash := rapid.StringMatching(`[a-f0-9]{32,64}`).Draw(t, "hash")
		m := validModule()
		m.Scripts = []Script{{Mode: ModeSystem, Script: "curl -fsSL https://example.com/file -o /tmp/file"}}
		m.Checksums = map[string]string{"file": hash}
		assert.NoError(t, m.ValidateChecksums())
	})
}

// ============================================================
// Script Validation (REQ-006-003)
// ============================================================

// Property: Multiple valid scripts always pass.
func TestProperty_MultipleValidScriptsAlwaysPass(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 10).Draw(t, "n")
		scripts := make([]Script, n)
		for i := 0; i < n; i++ {
			mode := rapid.SampledFrom([]ScriptMode{ModeSystem, ModeUser}).Draw(t, "mode")
			script := rapid.StringMatching(`[a-z]{1,20} [a-z]{1,20}`).Draw(t, "script")
			scripts[i] = Script{Mode: mode, Script: script}
		}
		m := validModule()
		m.Scripts = scripts
		assert.NoError(t, m.Validate(), "multiple valid scripts should pass")
	})
}

// Property: Any empty script (whitespace only) in any position always fails.
func TestProperty_EmptyScriptInAnyPositionAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(2, 5).Draw(t, "n")
		badIdx := rapid.IntRange(0, n-1).Draw(t, "badIdx")
		scripts := make([]Script, n)
		for i := 0; i < n; i++ {
			if i == badIdx {
				scripts[i] = Script{Mode: ModeSystem, Script: "   "}
			} else {
				scripts[i] = Script{Mode: ModeSystem, Script: "echo valid"}
			}
		}
		m := validModule()
		m.Scripts = scripts
		err := m.Validate()
		require.Error(t, err, "empty script at position %d should fail", badIdx)
		assert.Contains(t, err.Error(), "must not be empty")
	})
}

// Property: Any invalid mode in any position always fails.
func TestProperty_InvalidModeInAnyPositionAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		badMode := rapid.SampledFrom([]string{
			"admin", "root", "sudo", "privileged", "container",
		}).Draw(t, "badMode")
		n := rapid.IntRange(2, 5).Draw(t, "n")
		badIdx := rapid.IntRange(0, n-1).Draw(t, "badIdx")
		scripts := make([]Script, n)
		for i := 0; i < n; i++ {
			if i == badIdx {
				scripts[i] = Script{Mode: ScriptMode(badMode), Script: "echo bad"}
			} else {
				scripts[i] = Script{Mode: ModeSystem, Script: "echo valid"}
			}
		}
		m := validModule()
		m.Scripts = scripts
		err := m.Validate()
		require.Error(t, err, "invalid mode at position %d should fail", badIdx)
		assert.Contains(t, err.Error(), `must be "system" or "user"`)
	})
}

// ============================================================
// Description Validation (REQ-006-003)
// ============================================================

// Property: Empty description always fails.
func TestProperty_EmptyDescriptionAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		m := validModule()
		m.Description = ""
		err := m.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "description is required")
	})
}

// Property: Non-empty description always passes description check.
func TestProperty_NonEmptyDescriptionPasses(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		desc := rapid.StringMatching(`[A-Za-z0-9 .,!-]{1,100}`).Draw(t, "desc")
		m := validModule()
		m.Description = desc
		assert.NoError(t, m.Validate(), "non-empty description %q should pass", desc)
	})
}

// ============================================================
// JSON Round-Trip Invariants
// ============================================================

// Property: Module round-trips through JSON.
func TestProperty_ModuleJSONRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`[a-z][a-z0-9]*(-[a-z0-9]+)*`).Draw(t, "name")
		desc := rapid.StringMatching(`[A-Za-z0-9 ]{1,50}`).Draw(t, "desc")
		mode := rapid.SampledFrom([]string{"system", "user"}).Draw(t, "mode")
		script := rapid.StringMatching(`[a-z ]{1,30}`).Draw(t, "script")

		original := &Module{
			Name:        name,
			Description: desc,
			Scripts:     []Script{{Mode: ScriptMode(mode), Script: script}},
		}
		data, err := json.Marshal(original)
		require.NoError(t, err)

		var decoded Module
		require.NoError(t, json.Unmarshal(data, &decoded))
		assert.Equal(t, original.Name, decoded.Name)
		assert.Equal(t, original.Description, decoded.Description)
		require.Len(t, decoded.Scripts, 1)
		assert.Equal(t, original.Scripts[0].Mode, decoded.Scripts[0].Mode)
		assert.Equal(t, original.Scripts[0].Script, decoded.Scripts[0].Script)
	})
}

// Property: Module with full fields round-trips through JSON.
func TestProperty_ModuleFullJSONRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		m := validModule()
		m.DependsOn = []string{"base"}
		m.Checksums = map[string]string{"file.tar.gz": "abc123"}
		m.Probe = &Probe{Command: "echo ok"}

		data, err := json.Marshal(m)
		require.NoError(t, err)

		var decoded Module
		require.NoError(t, json.Unmarshal(data, &decoded))
		assert.Equal(t, m.Name, decoded.Name)
		assert.Equal(t, m.DependsOn, decoded.DependsOn)
		assert.Equal(t, m.Checksums, decoded.Checksums)
		require.NotNil(t, decoded.Probe)
		assert.Equal(t, m.Probe.Command, decoded.Probe.Command)
	})
}

// ============================================================
// Error Message Quality Invariants
// ============================================================

// Property: Validation errors always contain the module name.
func TestProperty_ValidationErrorsContainModuleName(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`[a-z]{1,10}`).Draw(t, "name")
		m := validModule()
		m.Name = name
		m.Scripts = nil // Make it invalid
		err := m.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), name,
			"validation error should contain module name %q", name)
	})
}

// Property: Multiple validation errors are joined with semicolons.
func TestProperty_MultipleErrorsSemicolonSeparated(t *testing.T) {
	m := &Module{Name: "", Description: "", Scripts: nil}
	err := m.Validate()
	require.Error(t, err)
	errStr := err.Error()
	assert.Contains(t, errStr, "name is required")
	assert.Contains(t, errStr, "description is required")
	assert.Contains(t, errStr, "at least one script is required")
	assert.Contains(t, errStr, ";",
		"multiple errors should be joined with semicolons")
}

// ============================================================
// Validation Determinism
// ============================================================

// Property: Validate is deterministic - same input always produces same result.
func TestProperty_ValidateDeterministic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`[A-Z]{1,5}`).Draw(t, "name")
		m := validModule()
		m.Name = name
		err1 := m.Validate()
		err2 := m.Validate()
		if err1 == nil {
			assert.Nil(t, err2)
		} else {
			require.Error(t, err2)
			assert.Equal(t, err1.Error(), err2.Error(),
				"Validate must be deterministic")
		}
	})
}

// ============================================================
// ValidateForRegistration Dependency Validation (REQ-006-004)
// ============================================================

// Property: Any dependency on a module not in knownNames always fails.
func TestProperty_UnknownDepAlwaysFailsRegistration(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		knownDep := rapid.StringMatching(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`).Draw(t, "known")
		unknownDep := "z-missing-" + rapid.StringMatching(`[a-z0-9]{3}`).Draw(t, "unknown")
		moduleName := "mod-" + rapid.StringMatching(`[a-z0-9]{3}`).Draw(t, "mod")

		m := validModule()
		m.Name = moduleName
		m.DependsOn = []string{knownDep, unknownDep}

		err := m.ValidateForRegistration(map[string]bool{knownDep: true})
		require.Error(t, err, "unknown dep %q should fail registration", unknownDep)
		assert.Contains(t, err.Error(), "depends on unknown module")
		assert.Contains(t, err.Error(), unknownDep)
	})
}

// Property: All dependencies known always passes the dependency check.
func TestProperty_AllDepsKnownAlwaysPassesRegistration(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		nDeps := rapid.IntRange(1, 5).Draw(t, "nDeps")
		knownNames := map[string]bool{"base": true}
		var deps []string
		for i := 0; i < nDeps; i++ {
			dep := fmt.Sprintf("dep%d", i)
			knownNames[dep] = true
			deps = append(deps, dep)
		}

		moduleName := "mod-" + rapid.StringMatching(`[a-z0-9]{3}`).Draw(t, "mod")
		m := validModule()
		m.Name = moduleName
		m.DependsOn = deps

		err := m.ValidateForRegistration(knownNames)
		assert.NoError(t, err, "all deps known should pass registration")
	})
}

// Property: A single unknown dependency among many known ones always fails.
func TestProperty_OneUnknownAmongManyAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		nKnown := rapid.IntRange(2, 6).Draw(t, "nKnown")
		knownNames := map[string]bool{}
		var deps []string
		for i := 0; i < nKnown; i++ {
			dep := fmt.Sprintf("k%d", i)
			knownNames[dep] = true
			deps = append(deps, dep)
		}

		unknown := "z-unknown-" + rapid.StringMatching(`[a-z0-9]{3}`).Draw(t, "unk")
		pos := rapid.IntRange(0, len(deps)).Draw(t, "pos")
		deps = append(deps[:pos], append([]string{unknown}, deps[pos:]...)...)

		moduleName := "mod-" + rapid.StringMatching(`[a-z0-9]{3}`).Draw(t, "mod")
		m := validModule()
		m.Name = moduleName
		m.DependsOn = deps

		err := m.ValidateForRegistration(knownNames)
		require.Error(t, err, "unknown dep among known should fail")
		assert.Contains(t, err.Error(), "depends on unknown module")
	})
}

// Property: Empty known names map causes any module with deps to fail.
func TestProperty_EmptyKnownNamesRejectsAllDeps(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dep := rapid.StringMatching(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`).Draw(t, "dep")
		moduleName := "mod-" + rapid.StringMatching(`[a-z0-9]{3}`).Draw(t, "mod")

		m := validModule()
		m.Name = moduleName
		m.DependsOn = []string{dep}

		err := m.ValidateForRegistration(map[string]bool{})
		require.Error(t, err, "dep %q should fail with empty known names", dep)
		assert.Contains(t, err.Error(), "depends on unknown module")
	})
}

// Property: Registering a module whose name is already in knownNames always fails.
func TestProperty_DuplicateNameAlwaysFailsRegistration(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`).Draw(t, "name")

		m := validModule()
		m.Name = name

		err := m.ValidateForRegistration(map[string]bool{name: true})
		require.Error(t, err, "duplicate name %q should fail", name)
		assert.Contains(t, err.Error(), "already registered")
	})
}

// Property: A module whose name is NOT in knownNames always passes the name check.
func TestProperty_UniqueNameAlwaysPassesNameCheck(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		moduleName := "mod-" + rapid.StringMatching(`[a-z0-9]{4}`).Draw(t, "mod")
		existingName := "existing-" + rapid.StringMatching(`[a-z0-9]{4}`).Draw(t, "existing")

		m := validModule()
		m.Name = moduleName
		m.DependsOn = nil

		err := m.ValidateForRegistration(map[string]bool{existingName: true})
		assert.NoError(t, err, "unique name should pass")
	})
}

// Property: Registration error always contains the module name.
func TestProperty_RegistrationErrorContainsModuleName(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`^[a-z][a-z0-9-]{2,10}`).Draw(t, "name")

		m := validModule()
		m.Name = name
		m.DependsOn = []string{name} // self-dep

		err := m.ValidateForRegistration(map[string]bool{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), name, "error should mention module name")
	})
}

// Property: Registration check is deterministic.
func TestProperty_RegistrationDeterministic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`^[a-z]{2,8}`).Draw(t, "name")
		m := validModule()
		m.Name = name
		m.DependsOn = []string{"ghost"}

		err1 := m.ValidateForRegistration(map[string]bool{})
		err2 := m.ValidateForRegistration(map[string]bool{})
		assert.Equal(t, err1 == nil, err2 == nil, "registration check must be deterministic")
		if err1 != nil && err2 != nil {
			assert.Equal(t, err1.Error(), err2.Error())
		}
	})
}

// Property: Registration check is independent of dependency order.
func TestProperty_RegistrationOrderIndependent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		m1 := validModule()
		m1.Name = "test-mod"
		m1.DependsOn = []string{"alpha", "beta", "gamma"}

		m2 := validModule()
		m2.Name = "test-mod"
		m2.DependsOn = []string{"gamma", "alpha", "beta"}

		known := map[string]bool{"alpha": true, "beta": true, "gamma": true}

		err1 := m1.ValidateForRegistration(known)
		err2 := m2.ValidateForRegistration(known)
		assert.Equal(t, err1 == nil, err2 == nil,
			"registration should be independent of dep order")
	})
}
