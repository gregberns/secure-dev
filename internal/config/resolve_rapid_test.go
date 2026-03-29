// Property-based tests for environment variable resolution invariants.
// REQ-005-008: ${VAR}, $VAR, $$ escape, unset → empty string
//
// ResolveEnvVars is security-critical: it handles credential injection into VMs.
// These tests verify the regex-based expansion is correct for all possible inputs.
package config

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================
// ResolveEnvVars — Property-Based Tests
// ============================================================

// Property: Setting a variable and referencing it via ${VAR} always resolves.
func TestProperty_BracedRefAlwaysResolves(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`^[A-Z][A-Z0-9_]{2,20}$`).Draw(t, "name")
		value := rapid.StringMatching(`^[A-Za-z0-9_.:/-]{1,50}$`).Draw(t, "value")

		require.NoError(t, os.Setenv(name, value))
		defer os.Unsetenv(name)

		result := ResolveEnvVars("${"+name+"}", nil)
		assert.Equal(t, value, result,
			"${%s} must resolve to %q", name, value)
	})
}

// Property: Setting a variable and referencing it via $VAR always resolves.
func TestProperty_BareRefAlwaysResolves(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`^[A-Z][A-Z0-9_]{2,20}$`).Draw(t, "name")
		value := rapid.StringMatching(`^[A-Za-z0-9_.:/-]{1,50}$`).Draw(t, "value")

		require.NoError(t, os.Setenv(name, value))
		defer os.Unsetenv(name)

		result := ResolveEnvVars("$"+name, nil)
		assert.Equal(t, value, result,
			"$%s must resolve to %q", name, value)
	})
}

// Property: Both ${VAR} and $VAR produce the same result for the same variable.
func TestProperty_BracedAndBareEquivalent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`^[A-Z][A-Z0-9_]{2,20}$`).Draw(t, "name")
		value := rapid.StringMatching(`^[A-Za-z0-9_.:/-]{1,50}$`).Draw(t, "value")

		require.NoError(t, os.Setenv(name, value))
		defer os.Unsetenv(name)

		braced := ResolveEnvVars("${"+name+"}", nil)
		bare := ResolveEnvVars("$"+name, nil)

		assert.Equal(t, braced, bare,
			"${%s} and $%s must produce same result", name, name)
	})
}

// Property: Mixed ${VAR_A} and $VAR_B in the same string both resolve.
func TestProperty_MixedRefsBothResolve(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		nameA := rapid.StringMatching(`^VAR_A_[A-Z0-9]{1,5}$`).Draw(t, "nameA")
		nameB := rapid.StringMatching(`^VAR_B_[A-Z0-9]{1,5}$`).Draw(t, "nameB")
		valA := rapid.StringMatching(`^[a-z]{2,10}$`).Draw(t, "valA")
		valB := rapid.StringMatching(`^[a-z]{2,10}$`).Draw(t, "valB")

		require.NoError(t, os.Setenv(nameA, valA))
		defer os.Unsetenv(nameA)
		require.NoError(t, os.Setenv(nameB, valB))
		defer os.Unsetenv(nameB)

		template := "${" + nameA + "}-$" + nameB
		result := ResolveEnvVars(template, nil)

		assert.Equal(t, valA+"-"+valB, result,
			"mixed refs must both resolve correctly")
	})
}

// Property: Unset variable always produces empty string and exactly one warning.
func TestProperty_UnsetVarProducesEmptyAndWarns(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`^SURELY_UNSET_[A-Z0-9_]{3,20}$`).Draw(t, "name")

		os.Unsetenv(name)

		var warnings []string
		warn := func(msg string) { warnings = append(warnings, msg) }

		result := ResolveEnvVars("${"+name+"}", warn)
		assert.Equal(t, "", result, "unset var %s must produce empty string", name)
		require.Len(t, warnings, 1, "unset var must produce exactly 1 warning")
		assert.Contains(t, warnings[0], name,
			"warning must mention the variable name")
	})
}

// Property: $$ always produces literal $ regardless of surrounding context.
func TestProperty_EscapedDollarAlwaysLiteralInContext(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		prefix := rapid.StringMatching(`^[a-z]{1,10}$`).Draw(t, "prefix")
		suffix := rapid.StringMatching(`^[a-z]{1,10}$`).Draw(t, "suffix")

		result := ResolveEnvVars(prefix+"$$"+suffix, nil)
		assert.Equal(t, prefix+"$"+suffix, result,
			"$$ must always become literal $ in context")
	})
}

// Property: "$${VAR}" escapes the $, producing literal "${VAR}" (not resolved).
func TestProperty_EscapedDollarPreventsBracedResolution(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`^[A-Z][A-Z0-9_]{2,10}$`).Draw(t, "name")
		value := rapid.StringMatching(`^[a-z]{2,8}$`).Draw(t, "value")

		require.NoError(t, os.Setenv(name, value))
		defer os.Unsetenv(name)

		// "$${NAME}" → sentinel replaces $$ → sentinel{NAME} → no match → restore sentinel → "${NAME}" literally
		result := ResolveEnvVars("$${"+name+"}", nil)
		assert.Equal(t, "${"+name+"}", result,
			"$$ must escape the $, producing literal ${%s} without resolution", name)
		assert.NotContains(t, result, value,
			"escaped reference must not be resolved")
	})
}

// Property: Resolution is deterministic — same inputs always produce same outputs.
func TestProperty_ResolveDeterministic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`^[A-Z][A-Z0-9_]{2,10}$`).Draw(t, "name")
		value := rapid.StringMatching(`^[a-z]{2,10}$`).Draw(t, "value")
		text := rapid.StringMatching(`^[A-Za-z0-9_.:/-]{1,30}$`).Draw(t, "text")

		require.NoError(t, os.Setenv(name, value))
		defer os.Unsetenv(name)

		template := text + "${" + name + "}" + text
		r1 := ResolveEnvVars(template, nil)
		r2 := ResolveEnvVars(template, nil)

		assert.Equal(t, r1, r2, "resolution must be deterministic")
	})
}

// Property: Multiple refs in a template all resolve independently.
func TestProperty_MultipleIndependentRefs(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(2, 5).Draw(t, "n")
		names := make([]string, n)
		values := make([]string, n)

		for i := 0; i < n; i++ {
			names[i] = fmt.Sprintf("MULTI_%d_%s", i,
				rapid.StringMatching(`[A-Z]{2,5}`).Draw(t, fmt.Sprintf("suffix_%d", i)))
			values[i] = fmt.Sprintf("val%d_%s", i,
				rapid.StringMatching(`[a-z]{2,5}`).Draw(t, fmt.Sprintf("val_%d", i)))
			require.NoError(t, os.Setenv(names[i], values[i]))
			defer os.Unsetenv(names[i])
		}

		// Build template: "${A}/${B}/${C}..."
		parts := make([]string, n)
		for i, name := range names {
			parts[i] = "${" + name + "}"
		}
		template := strings.Join(parts, "/")

		result := ResolveEnvVars(template, nil)

		expectedParts := make([]string, n)
		for i, val := range values {
			expectedParts[i] = val
		}
		expected := strings.Join(expectedParts, "/")

		assert.Equal(t, expected, result,
			"all %d refs must resolve independently", n)
	})
}

// Property: Variables with underscores and numbers in names resolve correctly.
func TestProperty_VarNamesWithUnderscoresAndDigits(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`^[A-Z][A-Z0-9_]{2,30}$`).Draw(t, "name")
		value := rapid.StringMatching(`^[A-Za-z0-9]{1,20}$`).Draw(t, "value")

		require.NoError(t, os.Setenv(name, value))
		defer os.Unsetenv(name)

		braced := ResolveEnvVars("${"+name+"}", nil)
		assert.Equal(t, value, braced,
			"${%s} must resolve for complex name", name)
	})
}

// Property: Empty environment resolves all refs to empty strings.
func TestProperty_AllUnsetResolvesToEmpty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name1 := rapid.StringMatching(`^EMPTY_TEST_A_[A-Z0-9]{2,8}$`).Draw(t, "name1")
		name2 := rapid.StringMatching(`^EMPTY_TEST_B_[A-Z0-9]{2,8}$`).Draw(t, "name2")

		os.Unsetenv(name1)
		os.Unsetenv(name2)

		template := "${" + name1 + "}-$" + name2
		result := ResolveEnvVars(template, nil)
		assert.Equal(t, "-", result,
			"all unset vars must resolve to empty, leaving only separator")
	})
}

// Property: Warning count equals number of unset variable references.
func TestProperty_WarningCountMatchesUnsetRefs(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		unset1 := rapid.StringMatching(`^UNSET_W_A_[A-Z0-9]{2,5}$`).Draw(t, "unset1")
		unset2 := rapid.StringMatching(`^UNSET_W_B_[A-Z0-9]{2,5}$`).Draw(t, "unset2")
		setName := rapid.StringMatching(`^SET_W_[A-Z0-9]{2,5}$`).Draw(t, "set")

		os.Unsetenv(unset1)
		os.Unsetenv(unset2)
		require.NoError(t, os.Setenv(setName, "ok"))
		defer os.Unsetenv(setName)

		var warnings []string
		warn := func(msg string) { warnings = append(warnings, msg) }

		template := "${" + unset1 + "}/${" + setName + "}/$" + unset2
		_ = ResolveEnvVars(template, warn)

		assert.Len(t, warnings, 2,
			"must warn exactly once per unset variable, got %d warnings", len(warnings))
	})
}

// Property: The two-pass resolution (braced then bare) limits expansion depth.
// ${OUTER} → value of OUTER → if value contains $INNER, the bare pass resolves it.
// But values produced by the bare pass are NOT re-expanded (only two passes total).
func TestProperty_TwoPassExpansionLimited(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		innerName := rapid.StringMatching(`^INNER_[A-Z0-9]{2,8}$`).Draw(t, "inner")
		outerName := rapid.StringMatching(`^OUTER_[A-Z0-9]{2,8}$`).Draw(t, "outer")
		innerValue := rapid.StringMatching(`^[a-z]{2,8}$`).Draw(t, "innerValue")

		require.NoError(t, os.Setenv(innerName, innerValue))
		defer os.Unsetenv(innerName)
		// Outer value is a bare ref: $INNER_NAME
		require.NoError(t, os.Setenv(outerName, "$"+innerName))
		defer os.Unsetenv(outerName)

		// ${OUTER} → "$INNER" → bare pass → innerValue
		result := ResolveEnvVars("${"+outerName+"}", nil)
		assert.Equal(t, innerValue, result,
			"two-pass: ${%s} resolves to $%s which resolves to %s",
			outerName, innerName, innerValue)
	})
}

// Property: Bare refs in resolved values are NOT recursively re-expanded.
// Since the bare pass runs once, a bare ref in a bare-resolved value stays literal.
func TestProperty_BareRefsNotReExpanded(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		innerName := rapid.StringMatching(`^DEEP_INNER_[A-Z0-9]{2,5}$`).Draw(t, "inner")
		outerName := rapid.StringMatching(`^DEEP_OUTER_[A-Z0-9]{2,5}$`).Draw(t, "outer")
		innerValue := rapid.StringMatching(`^[a-z]{2,5}$`).Draw(t, "innerValue")

		require.NoError(t, os.Setenv(innerName, innerValue))
		defer os.Unsetenv(innerName)
		// Outer value is a bare ref: $INNER_NAME
		require.NoError(t, os.Setenv(outerName, "$"+innerName))
		defer os.Unsetenv(outerName)

		// $OUTER → "$INNER" via bare pass — but the bare pass already matched $OUTER,
		// and ReplaceAllStringFunc processes all matches in one pass.
		// So $INNER in the result is NOT re-expanded.
		result := ResolveEnvVars("$"+outerName, nil)
		assert.Equal(t, "$"+innerName, result,
			"bare pass does not re-expand: $%s resolves to $%s literally",
			outerName, innerName)
		assert.NotContains(t, result, innerValue,
			"bare-resolved value containing $ must not be recursively expanded")
	})
}

// Property: ResolveEnvVars is idempotent on already-resolved values.
func TestProperty_ResolveIdempotentOnPlain(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		text := rapid.StringMatching(`^[A-Za-z0-9_.:/-]{1,50}$`).Draw(t, "text")

		r1 := ResolveEnvVars(text, nil)
		r2 := ResolveEnvVars(r1, nil)

		assert.Equal(t, r1, r2, "resolving already-plain text must be idempotent")
	})
}

// Property: $$ at start, middle, and end all produce literal $.
func TestProperty_EscapedDollarAtAnyPosition(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		text := rapid.StringMatching(`^[a-z]{2,8}$`).Draw(t, "text")

		atStart := ResolveEnvVars("$$"+text, nil)
		assert.Equal(t, "$"+text, atStart, "$$ at start")

		atEnd := ResolveEnvVars(text+"$$", nil)
		assert.Equal(t, text+"$", atEnd, "$$ at end")

		atMiddle := ResolveEnvVars(text+"$$"+text, nil)
		assert.Equal(t, text+"$"+text, atMiddle, "$$ in middle")
	})
}

// Property: Multiple $$ in sequence each produce literal $.
func TestProperty_MultipleEscapedDollars(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		text := rapid.StringMatching(`^[a-z]{2,5}$`).Draw(t, "text")

		result := ResolveEnvVars("$$$$"+text, nil)
		assert.Equal(t, "$$"+text, result,
			"$$$$ must become $$ (two escaped dollars)")
	})
}

// ============================================================
// HasEnvVarRef — Property-Based Tests
// ============================================================

// Property: HasEnvVarRef returns false for plain text without $ or $$.
func TestProperty_HasEnvVarRefFalseForPlainText(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		text := rapid.StringMatching(`^[A-Za-z0-9_.:/-]{1,50}$`).Draw(t, "text")
		assert.False(t, HasEnvVarRef(text),
			"plain text must not be detected as having env var refs")
	})
}

// Property: HasEnvVarRef returns true for ${VAR} for any valid variable name.
func TestProperty_HasEnvVarRefTrueForBraced(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`^[A-Z_][A-Z0-9_]{1,20}$`).Draw(t, "name")
		assert.True(t, HasEnvVarRef("${"+name+"}"),
			"${%s} must be detected as env var ref", name)
	})
}

// Property: HasEnvVarRef returns true for $VAR for any valid variable name.
func TestProperty_HasEnvVarRefTrueForBare(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`^[A-Z_][A-Z0-9_]{1,20}$`).Draw(t, "name")
		assert.True(t, HasEnvVarRef("$"+name),
			"$%s must be detected as env var ref", name)
	})
}

// Property: HasEnvVarRef returns false for $$ (escaped dollar).
func TestProperty_HasEnvVarRefFalseForEscapedDollar(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		text := rapid.StringMatching(`^[A-Za-z0-9]{1,20}$`).Draw(t, "text")
		assert.False(t, HasEnvVarRef("$$"+text),
			"$$ must not be detected as env var ref")
	})
}

// Property: HasEnvVarRef returns false for $ followed by non-alpha/underscore.
func TestProperty_HasEnvVarRefFalseForInvalidRef(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		char := rapid.SampledFrom([]string{"0", "1", "-", ".", "/", " ", "$"}).Draw(t, "char")
		text := rapid.StringMatching(`^[a-z]{2,8}$`).Draw(t, "text")
		assert.False(t, HasEnvVarRef(text+"$"+char+text),
			"$ followed by %q must not be detected as env var ref", char)
	})
}

// Property: HasEnvVarRef agrees with whether ResolveEnvVars would change the string.
// If HasEnvVarRef returns false, ResolveEnvVars must return the input unchanged.
func TestProperty_HasEnvVarRefConsistentWithResolve(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		text := rapid.StringMatching(`^[A-Za-z0-9_.:/-]{1,50}$`).Draw(t, "text")

		if !HasEnvVarRef(text) {
			result := ResolveEnvVars(text, nil)
			assert.Equal(t, text, result,
				"if HasEnvVarRef is false, ResolveEnvVars must be identity")
		}
	})
}

// ============================================================
// MaskValue — Property-Based Tests
// ============================================================

// Property: MaskValue for values > 8 chars preserves first 5 and appends ****.
func TestProperty_MaskValueLongPreservesPrefix(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		value := rapid.StringMatching(`^[A-Za-z0-9]{9,100}$`).Draw(t, "value")
		masked := MaskValue(value)

		assert.True(t, strings.HasPrefix(masked, value[:5]),
			"masked value must start with first 5 chars")
		assert.True(t, strings.HasSuffix(masked, "****"),
			"masked value must end with ****")
		assert.NotContains(t, masked, value[5:],
			"masked value must not contain chars after position 5")
	})
}

// Property: MaskValue for values <= 8 chars always returns ****.
func TestProperty_MaskValueShortAlwaysFullyMasked(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		value := rapid.StringMatching(`^[A-Za-z0-9]{1,8}$`).Draw(t, "value")
		masked := MaskValue(value)

		assert.Equal(t, "****", masked,
			"short value %q must be fully masked", value)
	})
}

// Property: MaskValue is deterministic.
func TestProperty_MaskValueDeterministic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		value := rapid.StringMatching(`^[A-Za-z0-9]{1,100}$`).Draw(t, "value")

		m1 := MaskValue(value)
		m2 := MaskValue(value)
		assert.Equal(t, m1, m2, "MaskValue must be deterministic")
	})
}

// Property: MaskValue always contains ****.
func TestProperty_MaskValueAlwaysContainsMask(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		value := rapid.StringMatching(`^[A-Za-z0-9]{1,100}$`).Draw(t, "value")
		masked := MaskValue(value)

		assert.Contains(t, masked, "****",
			"masked value must always contain ****")
	})
}

// Property: MaskValue result is never equal to the original value.
func TestProperty_MaskValueNeverEqualsOriginal(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		value := rapid.StringMatching(`^[A-Za-z0-9]{1,100}$`).Draw(t, "value")
		masked := MaskValue(value)

		assert.NotEqual(t, value, masked,
			"masked value must never equal the original")
	})
}

// Property: MaskValue result length is bounded.
// Short values → 4 chars. Long values → 9 chars (5 + "****").
func TestProperty_MaskValueLengthBounded(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		value := rapid.StringMatching(`^[A-Za-z0-9]{1,200}$`).Draw(t, "value")
		masked := MaskValue(value)

		if len(value) <= 8 {
			assert.Equal(t, 4, len(masked))
		} else {
			assert.Equal(t, 9, len(masked))
		}
	})
}

// ============================================================
// Cross-Cutting: ResolveEnvVars + HasEnvVarRef Integration
// ============================================================

// Property: After resolution, the result has no remaining ${...} patterns
// unless a variable value itself contains ${...} (which should not re-expand).
func TestProperty_ResolveRemovesBracedPatterns(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`^[A-Z][A-Z0-9_]{2,10}$`).Draw(t, "name")
		value := rapid.StringMatching(`^[a-z]{2,10}$`).Draw(t, "value")

		require.NoError(t, os.Setenv(name, value))
		defer os.Unsetenv(name)

		template := "prefix${" + name + "}suffix"
		result := ResolveEnvVars(template, nil)

		assert.Equal(t, "prefix"+value+"suffix", result)
		assert.False(t, strings.Contains(result, "${"),
			"resolved result must not contain ${ patterns")
	})
}

// Property: Template with only $$ and plain text resolves to text with literal $.
func TestProperty_TemplateWithOnlyEscapesResolvesCleanly(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		text := rapid.StringMatching(`^[a-z]{2,10}$`).Draw(t, "text")

		template := "$$" + text + "$$" + text
		result := ResolveEnvVars(template, nil)

		assert.Equal(t, "$"+text+"$"+text, result,
			"template with only $$ and text must resolve cleanly")
	})
}
