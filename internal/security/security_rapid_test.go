// Package security — dedicated property-based tests for security invariants.
// REQ-004-001 through REQ-004-031
//
// This file contains deeper invariant tests beyond those in security_test.go,
// focusing on cross-cutting security properties, monotonicity, mode independence,
// egress immutability, and JSON serialization round-trips.
package security

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================
// REQ-004-005: Mount Path Validation — Deeper Invariants
// ============================================================

// Property: ValidateMountPath result is independent of MountMode.
// Both ReadOnly and ReadWrite modes must agree on accept/reject.
func TestMountValidation_ModeIndependent_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		path := rapid.OneOf(
			rapid.StringMatching(`/tmp/[a-z0-9_-]{2,20}`),
			rapid.StringMatching(`/opt/[a-z0-9_-]{2,20}`),
			rapid.StringMatching(`/srv/[a-z0-9_-]{2,20}`),
			rapid.Just("/var/run/docker.sock"),
			rapid.Just(os.Getenv("HOME")),
		).Draw(t, "path")

		errRO := ValidateMountPath(path, MountReadOnly, nil)
		errRW := ValidateMountPath(path, MountReadWrite, nil)

		// Both modes must agree.
		if errRO == nil {
			assert.NoError(t, errRW, "ReadOnly accepted %q but ReadWrite rejected it", path)
		} else {
			assert.Error(t, errRW, "ReadOnly rejected %q but ReadWrite accepted it", path)
		}
	})
}

// Property: Mount path monotonicity — if a child is rejected for being inside a
// sensitive directory, any parent path closer to root is also rejected.
// This applies to non-home sensitive dirs only.
func TestMountValidation_ChildRejectionImpliesParentRejection_Property(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	sensitiveDirs := []struct {
		name string
		path string
	}{
		{"ssh", filepath.Join(homeDir, ".ssh")},
		{"aws", filepath.Join(homeDir, ".aws")},
		{"kube", filepath.Join(homeDir, ".kube")},
		{"gnupg", filepath.Join(homeDir, ".gnupg")},
		{"docker_dir", filepath.Join(homeDir, ".docker")},
	}

	rapid.Check(t, func(t *rapid.T) {
		dir := rapid.SampledFrom(sensitiveDirs).Draw(t, "dir")
		child := filepath.Join(dir.path, rapid.StringMatching(`[a-z0-9_-]{2,10}`).Draw(t, "child"))

		// Child must be rejected.
		err := ValidateMountPath(child, MountReadOnly, nil)
		require.Error(t, err, "child %q of sensitive dir must be rejected", child)

		// Parent (the sensitive dir itself) must also be rejected.
		err = ValidateMountPath(dir.path, MountReadOnly, nil)
		assert.Error(t, err, "parent %q of rejected child must also be rejected", dir.path)
	})
}

// Property: Path normalization — redundant slashes, trailing slashes, and .
// components do not change the validation result.
func TestMountValidation_NormalizationStable_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		base := rapid.OneOf(
			rapid.Just("/tmp"),
			rapid.Just("/opt"),
			rapid.Just("/srv"),
		).Draw(t, "base")
		segment := rapid.StringMatching(`[a-z0-9]{2,8}`).Draw(t, "segment")

		clean := filepath.Join(base, segment)
		withSlash := clean + "/"
		withDot := filepath.Join(base, ".", segment)

		err1 := ValidateMountPath(clean, MountReadOnly, nil)
		err2 := ValidateMountPath(withSlash, MountReadOnly, nil)
		err3 := ValidateMountPath(withDot, MountReadOnly, nil)

		assert.Equal(t, err1 == nil, err2 == nil,
			"trailing slash changed result: clean=%q slash=%q", clean, withSlash)
		assert.Equal(t, err1 == nil, err3 == nil,
			"dot component changed result: clean=%q dot=%q", clean, withDot)
	})
}

// Property: Every path in DefaultSensitivePaths is always rejected.
func TestMountValidation_AllBuiltinsRejected_Property(t *testing.T) {
	paths := DefaultSensitivePaths()
	rapid.Check(t, func(t *rapid.T) {
		idx := rapid.IntRange(0, len(paths)-1).Draw(t, "idx")
		path := paths[idx]

		err := ValidateMountPath(path, MountReadOnly, nil)
		assert.Error(t, err, "built-in sensitive path %q must always be rejected", path)
	})
}

// Property: Extra sensitive paths extend but never reduce protection.
// If a path is rejected without extra paths, it remains rejected with them.
func TestMountValidation_ExtraPathsNeverReduceProtection_Property(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	rejectedPaths := []string{
		filepath.Join(homeDir, ".ssh"),
		filepath.Join(homeDir, ".aws"),
		"/var/run/docker.sock",
	}

	rapid.Check(t, func(t *rapid.T) {
		path := rapid.SampledFrom(rejectedPaths).Draw(t, "path")
		extra := rapid.SliceOfN(
			rapid.StringMatching(`/tmp/extra-[a-z0-9]{2,8}`),
			0, 5,
		).Draw(t, "extraPaths")

		err := ValidateMountPath(path, MountReadOnly, extra)
		assert.Error(t, err, "path %q must stay rejected even with extra sensitive paths", path)
	})
}

// Property: User-configured extra sensitive paths are enforced.
func TestMountValidation_ExtraPathsAreEnforced_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		segment := rapid.StringMatching(`secret-[a-z0-9]{2,8}`).Draw(rt, "segment")
		tmpDir := t.TempDir()
		sensitivePath := filepath.Join(tmpDir, segment)

		err := ValidateMountPath(sensitivePath, MountReadOnly, []string{sensitivePath})
		assert.Error(t, err,
			"user-configured sensitive path %q must be rejected", sensitivePath)

		// A sibling that isn't configured as sensitive should be accepted.
		safePath := filepath.Join(tmpDir, "safe-"+segment)
		err = ValidateMountPath(safePath, MountReadOnly, []string{sensitivePath})
		assert.NoError(t, err,
			"non-sensitive sibling %q should be accepted", safePath)
	})
}

// Property: .env at the mount root is always rejected regardless of path prefix.
func TestMountValidation_EnvFilesAlwaysRejected_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		prefix := rapid.OneOf(
			rapid.Just("/tmp"),
			rapid.Just("/opt"),
			rapid.Just("/srv"),
			rapid.Just("/home/user/projects"),
		).Draw(t, "prefix")
		envName := rapid.OneOf(
			rapid.Just(".env"),
			rapid.Just(".env.local"),
			rapid.Just(".env.production"),
			rapid.Just(".env.staging"),
		).Draw(t, "envName")

		path := filepath.Join(prefix, envName)
		err := ValidateMountPath(path, MountReadOnly, nil)
		assert.Error(t, err, ".env path %q must be rejected", path)

		var mve *MountValidationError
		require.ErrorAs(t, err, &mve)
		assert.Equal(t, "env_file", mve.Category)
	})
}

// ============================================================
// REQ-004-006 / REQ-004-007: Egress Control — Deeper Invariants
// ============================================================

// Property: BuildEgressList always includes all default domains, regardless of user input.
// REQ-004-008: User additions are merged with, never replacing, defaults.
func TestBuildEgressList_DefaultsNeverRemoved_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random user domains that could be confusing.
		userDomains := rapid.SliceOfN(
			rapid.OneOf(
				rapid.StringMatching(`[a-z]{4,12}\.com`),
				rapid.Just("github.com"),       // same as default
				rapid.Just("api.anthropic.com"), // same as default
				rapid.Just(""),
			),
			0, 5,
		).Draw(t, "userDomains")

		result := BuildEgressList(userDomains)

		resultMap := make(map[string]DomainSource)
		for _, d := range result {
			resultMap[strings.ToLower(d.Domain)] = d.Source
		}

		// Every default domain must be present.
		for _, d := range DefaultEgressAllowlist {
			_, ok := resultMap[strings.ToLower(d)]
			assert.True(t, ok, "default domain %q must always be in the result", d)
		}
	})
}

// Property: BuildEgressList never returns duplicate domains.
func TestBuildEgressList_NoDuplicates_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		userDomains := rapid.SliceOfN(
			rapid.StringMatching(`[a-z]{2,8}\.[a-z]{2,4}`),
			0, 10,
		).Draw(t, "userDomains")

		result := BuildEgressList(userDomains)

		seen := make(map[string]bool)
		for _, d := range result {
			key := strings.ToLower(d.Domain)
			assert.False(t, seen[key], "duplicate domain %q in result", d.Domain)
			seen[key] = true
		}
	})
}

// Property: BuildEgressList result size is bounded.
// len(result) <= len(defaults) + len(unique user domains).
func TestBuildEgressList_SizeBound_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		userDomains := rapid.SliceOfN(
			rapid.StringMatching(`[a-z]{2,8}\.[a-z]{2,4}`),
			0, 10,
		).Draw(t, "userDomains")

		result := BuildEgressList(userDomains)

		// Count unique user domains (non-empty, non-default).
		defaultSet := make(map[string]bool)
		for _, d := range DefaultEgressAllowlist {
			defaultSet[strings.ToLower(d)] = true
		}
		uniqueUser := 0
		userSeen := make(map[string]bool)
		for _, d := range userDomains {
			key := strings.ToLower(strings.TrimSpace(d))
			if key != "" && !defaultSet[key] && !userSeen[key] {
				uniqueUser++
				userSeen[key] = true
			}
		}

		assert.LessOrEqual(t, len(result), len(DefaultEgressAllowlist)+uniqueUser,
			"result size must not exceed defaults + unique user domains")
		assert.GreaterOrEqual(t, len(result), len(DefaultEgressAllowlist),
			"result must contain at least all defaults")
	})
}

// Property: Wildcard matching rejects multi-level subdomains.
// *.example.com must NOT match sub.sub.example.com.
func TestDomainMatches_WildcardRejectsMultiLevel_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		domain := rapid.StringMatching(`[a-z]{3,8}\.[a-z]{2,5}`).Draw(t, "domain")
		pattern := "*." + domain

		// Single level must match.
		label := rapid.StringMatching(`[a-z]{2,6}`).Draw(t, "label")
		single := label + "." + domain
		assert.True(t, DomainMatches(pattern, single),
			"*.%s should match %s", domain, single)

		// Multi-level must NOT match.
		multi := "sub." + label + "." + domain
		assert.False(t, DomainMatches(pattern, multi),
			"*.%s should NOT match multi-level %s", domain, multi)

		// Bare domain must NOT match.
		assert.False(t, DomainMatches(pattern, domain),
			"*.%s should NOT match bare domain %s", domain, domain)
	})
}

// Property: Domain matching is commutative with respect to case normalization.
// DomainMatches(A, B) == DomainMatches(upper(A), upper(B)).
func TestDomainMatches_CaseCommutative_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		pattern := rapid.StringMatching(`\*?\.[a-z]{2,8}\.[a-z]{2,5}`).Draw(t, "pattern")
		hostname := rapid.StringMatching(`[a-z]{2,8}\.[a-z]{2,8}\.[a-z]{2,5}`).Draw(t, "hostname")

		result1 := DomainMatches(pattern, hostname)
		result2 := DomainMatches(strings.ToUpper(pattern), strings.ToUpper(hostname))
		result3 := DomainMatches(strings.ToLower(pattern), strings.ToLower(hostname))

		assert.Equal(t, result1, result2,
			"case should not affect matching: pattern=%q hostname=%q", pattern, hostname)
		assert.Equal(t, result1, result3,
			"lowercase should match mixed case: pattern=%q hostname=%q", pattern, hostname)
	})
}

// Property: ValidateEgressDomain rejects all malformed patterns.
func TestValidateEgressDomain_RejectsMalformed_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		badDomain := rapid.OneOf(
			rapid.Just(""),
			rapid.StringMatching(`^\.[a-z]+`),
			rapid.StringMatching(`[a-z]+\.\.[a-z]+`),
			rapid.Just("*.com"),
		).Draw(t, "badDomain")

		err := ValidateEgressDomain(badDomain)
		assert.Error(t, err, "malformed domain %q should be rejected", badDomain)
	})
}

// Property: ValidateEgressDomain accepts all well-formed domains with 2+ labels.
func TestValidateEgressDomain_AcceptsWellFormedMultiLabel_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		label1 := rapid.StringMatching(`[a-z0-9]([a-z0-9-]*[a-z0-9])?`).Draw(t, "label1")
		label2 := rapid.StringMatching(`[a-z]{2,10}`).Draw(t, "label2")

		domain := label1 + "." + label2
		err := ValidateEgressDomain(domain)
		assert.NoError(t, err, "well-formed domain %q should be accepted", domain)
	})
}

// Property: IsEgressAllowed is consistent across multiple calls with same inputs.
func TestIsEgressAllowed_Deterministic_Property(t *testing.T) {
	allowlist := BuildEgressList(nil)
	rapid.Check(t, func(t *rapid.T) {
		hostname := rapid.OneOf(
			rapid.Just("api.anthropic.com"),
			rapid.Just("github.com"),
			rapid.Just("raw.githubusercontent.com"),
			rapid.StringMatching(`[a-z]{8}\.[a-z]{8}\.invalid`),
		).Draw(t, "hostname")

		r1 := IsEgressAllowed(hostname, allowlist)
		r2 := IsEgressAllowed(hostname, allowlist)
		assert.Equal(t, r1, r2, "IsEgressAllowed must be deterministic for %q", hostname)
	})
}

// ============================================================
// REQ-004-012 / REQ-004-013: Token Validation — Deeper Invariants
// ============================================================

// Property: Empty tokens produce no warnings for GitHub PATs.
// Empty means absent, which is distinct from malformed.
func TestValidateToken_EmptyGitHubNoWarnings_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		tokenType := rapid.SampledFrom([]TokenType{
			TokenGitHubPAT,
			TokenGitHubFinegrained,
		}).Draw(t, "tokenType")

		warnings := ValidateToken("", tokenType)
		assert.Empty(t, warnings, "empty token should produce no warnings")
	})
}

// Property: Anthropic keys starting with sk-ant- never produce warnings.
func TestValidateToken_ValidAnthropicNoWarnings_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		suffix := rapid.StringMatching(`[A-Za-z0-9_-]{10,40}`).Draw(t, "suffix")
		token := "sk-ant-" + suffix

		warnings := ValidateToken(token, TokenAnthropicAPI)
		assert.Empty(t, warnings, "valid Anthropic key prefix should produce no warnings")
	})
}

// Property: Non-sk-ant- prefixed Anthropic keys always warn.
func TestValidateToken_NonAnthropicPrefixWarns_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		prefix := rapid.OneOf(
			rapid.StringMatching(`sk-[a-z]{1,5}`),
			rapid.Just("key-"),
			rapid.StringMatching(`[a-z]{3,10}-`),
		).Draw(t, "prefix")
		suffix := rapid.StringMatching(`[A-Za-z0-9_-]{10,40}`).Draw(t, "suffix")
		token := prefix + suffix

		// Skip if it happens to start with sk-ant-
		if strings.HasPrefix(token, "sk-ant-") {
			t.Skip("collision with valid prefix")
		}

		warnings := ValidateToken(token, TokenAnthropicAPI)
		found := false
		for _, w := range warnings {
			if w.Code == "UNUSUAL_KEY_FORMAT" {
				found = true
			}
		}
		assert.True(t, found, "non-sk-ant- Anthropic key %q should warn", prefix+"...")
	})
}

// Property: RedactToken never reveals any part of tokens longer than 8 chars
// beyond the first 5 characters.
func TestRedactToken_LeakageBound_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		token := rapid.StringMatching(`[A-Za-z0-9_-]{9,60}`).Draw(t, "token")
		redacted := RedactToken(token)

		// Must not contain any 4-char substring from position 5 onwards.
		for i := 5; i <= len(token)-4; i++ {
			substr := token[i : i+4]
			assert.NotContains(t, redacted, substr,
				"redacted form leaks substring from position %d of token", i)
		}
	})
}

// Property: RedactToken is deterministic.
func TestRedactToken_Deterministic_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		token := rapid.StringMatching(`[A-Za-z0-9_-]{1,60}`).Draw(t, "token")

		r1 := RedactToken(token)
		r2 := RedactToken(token)
		assert.Equal(t, r1, r2, "RedactToken must be deterministic")
	})
}

// Property: Short tokens (<=8 chars) are always fully redacted.
func TestRedactToken_ShortAlwaysFullyRedacted_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		token := rapid.StringMatching(`[A-Za-z0-9_-]{1,8}`).Draw(t, "token")
		redacted := RedactToken(token)

		assert.Equal(t, "[REDACTED]", redacted,
			"short token must be fully redacted")
	})
}

// ============================================================
// JSON Serialization Invariants
// ============================================================

// Property: MountValidationError serializes to valid JSON with all fields.
func TestMountValidationError_JSONRoundTrip_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		path := rapid.StringMatching(`/[a-z0-9/_.-]{2,40}`).Draw(t, "path")
		category := rapid.SampledFrom([]string{
			"home", "ssh", "aws", "config", "gnupg", "kube",
			"docker_dir", "docker_socket", "browser", "env_file", "user_configured",
		}).Draw(t, "category")
		reason := "test reason for " + path

		original := &MountValidationError{
			Path:     path,
			Category: category,
			Reason:   reason,
		}

		data, err := json.Marshal(original)
		require.NoError(t, err)

		var decoded MountValidationError
		require.NoError(t, json.Unmarshal(data, &decoded))

		assert.Equal(t, original.Path, decoded.Path)
		assert.Equal(t, original.Category, decoded.Category)
		assert.Equal(t, original.Reason, decoded.Reason)
	})
}

// Property: EgressDomain serializes to valid JSON with all fields.
func TestEgressDomain_JSONRoundTrip_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		domain := rapid.StringMatching(`[a-z0-9.*-]{4,30}`).Draw(t, "domain")
		source := rapid.SampledFrom([]DomainSource{DomainSourceDefault, DomainSourceUser}).Draw(t, "source")

		original := EgressDomain{Domain: domain, Source: source}

		data, err := json.Marshal(original)
		require.NoError(t, err)

		var decoded EgressDomain
		require.NoError(t, json.Unmarshal(data, &decoded))

		assert.Equal(t, original.Domain, decoded.Domain)
		assert.Equal(t, original.Source, decoded.Source)
	})
}

// Property: SecurityWarning serializes to valid JSON with all fields.
func TestSecurityWarning_JSONRoundTrip_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		code := rapid.StringMatching(`[A-Z_]{3,20}`).Draw(t, "code")
		message := rapid.StringMatching(`[A-Za-z0-9 .,"']{10,80}`).Draw(t, "message")

		original := SecurityWarning{Code: code, Message: message}

		data, err := json.Marshal(original)
		require.NoError(t, err)

		var decoded SecurityWarning
		require.NoError(t, json.Unmarshal(data, &decoded))

		assert.Equal(t, original.Code, decoded.Code)
		assert.Equal(t, original.Message, decoded.Message)
	})
}

// ============================================================
// Cross-Cutting Invariants
// ============================================================

// Property: ValidateMountPath Error() is always non-empty when an error is returned.
func TestMountValidationError_NeverEmptyError_Property(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	rejectedPaths := []string{
		homeDir,
		filepath.Join(homeDir, ".ssh"),
		filepath.Join(homeDir, ".aws"),
		"/var/run/docker.sock",
		"/tmp/.env",
	}

	rapid.Check(t, func(t *rapid.T) {
		path := rapid.SampledFrom(rejectedPaths).Draw(t, "path")
		err := ValidateMountPath(path, MountReadOnly, nil)
		require.Error(t, err)

		var mve *MountValidationError
		require.ErrorAs(t, err, &mve)
		assert.NotEmpty(t, mve.Error(), "Error() must not be empty for %q", path)
		assert.NotEmpty(t, mve.Reason, "Reason must not be empty for %q", path)
		assert.NotEmpty(t, mve.Category, "Category must not be empty for %q", path)
		assert.Equal(t, mve.Reason, mve.Error(), "Error() must return Reason")
	})
}

// Property: SecurityWarnings always have non-empty Code and Message.
func TestSecurityWarning_NeverEmptyFields_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		token := rapid.StringMatching(`[A-Za-z0-9_-]{5,50}`).Draw(t, "token")
		tokenType := rapid.SampledFrom([]TokenType{
			TokenGitHubPAT,
			TokenGitHubFinegrained,
			TokenAnthropicAPI,
		}).Draw(t, "tokenType")

		warnings := ValidateToken(token, tokenType)
		for _, w := range warnings {
			assert.NotEmpty(t, w.Code, "warning code must not be empty")
			assert.NotEmpty(t, w.Message, "warning message must not be empty for code %s", w.Code)
		}
	})
}

// Property: DefaultEgressAllowlist is non-empty and all entries are valid domains.
func TestDefaultEgressAllowlist_NonEmptyAndValid_Property(t *testing.T) {
	assert.NotEmpty(t, DefaultEgressAllowlist, "default allowlist must not be empty")

	for _, d := range DefaultEgressAllowlist {
		err := ValidateEgressDomain(d)
		assert.NoError(t, err, "default domain %q must be valid", d)
	}
}

// Property: DefaultSensitivePaths is non-empty and all entries are absolute paths.
func TestDefaultSensitivePaths_NonEmptyAndAbsolute_Property(t *testing.T) {
	paths := DefaultSensitivePaths()
	assert.NotEmpty(t, paths, "sensitive paths list must not be empty")

	for _, p := range paths {
		assert.True(t, filepath.IsAbs(p),
			"sensitive path %q must be absolute", p)
	}
}
