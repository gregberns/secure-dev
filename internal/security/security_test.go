// Package security provides property-based and unit tests for the security model.
// REQ-004-001 through REQ-004-031
package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- REQ-004-005: Mount Path Validation ---

func TestValidateMountPath_RejectsSensitivePaths(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	tests := []struct {
		name     string
		hostPath string
		wantCat  string
	}{
		{"home", homeDir, "home"},
		{"ssh", filepath.Join(homeDir, ".ssh"), "ssh"},
		{"aws", filepath.Join(homeDir, ".aws"), "aws"},
		{"config", filepath.Join(homeDir, ".config"), "config"},
		{"gnupg", filepath.Join(homeDir, ".gnupg"), "gnupg"},
		{"kube", filepath.Join(homeDir, ".kube"), "kube"},
		{"docker_dir", filepath.Join(homeDir, ".docker"), "docker_dir"},
		{"docker_socket", "/var/run/docker.sock", "docker_socket"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMountPath(tt.hostPath, MountReadOnly, nil)
			require.Error(t, err, "path %q should be rejected", tt.hostPath)
			var mve *MountValidationError
			require.ErrorAs(t, err, &mve)
			assert.Equal(t, tt.wantCat, mve.Category)
			assert.Contains(t, mve.Reason, "sensitive directory")
		})
	}
}

func TestValidateMountPath_AcceptsValidPaths(t *testing.T) {
	homeDir, _ := os.UserHomeDir()

	tests := []struct {
		name     string
		hostPath string
	}{
		{"project_subdir", filepath.Join(homeDir, "projects", "my-repo")},
		{"tmp", "/tmp/workspace"},
		{"opt", "/opt/tools"},
		{"usr_local", "/usr/local/src"},
		{"home_repo", filepath.Join(homeDir, "my-repo")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMountPath(tt.hostPath, MountReadOnly, nil)
			assert.NoError(t, err, "path %q should be accepted", tt.hostPath)
		})
	}
}

func TestValidateMountPath_ChildOfSensitiveRejected(t *testing.T) {
	homeDir, _ := os.UserHomeDir()

	// Children of sensitive dirs (non-home) should be rejected
	tests := []struct {
		name     string
		hostPath string
	}{
		{"ssh_child", filepath.Join(homeDir, ".ssh", "known_hosts")},
		{"aws_child", filepath.Join(homeDir, ".aws", "credentials")},
		{"kube_child", filepath.Join(homeDir, ".kube", "config")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMountPath(tt.hostPath, MountReadOnly, nil)
			assert.Error(t, err, "child of sensitive path %q should be rejected", tt.hostPath)
		})
	}
}

func TestValidateMountPath_EnvFilesRejected(t *testing.T) {
	tests := []struct {
		name     string
		hostPath string
		wantErr  bool
	}{
		{"dot_env", "/project/.env", true},
		{"dot_env_local", "/project/.env.local", true},
		{"regular_dir", "/project/src", false},
		{"config_yaml", "/project/config.yaml", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMountPath(tt.hostPath, MountReadOnly, nil)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateMountPath_ExtraSensitivePaths(t *testing.T) {
	dir := t.TempDir()
	sensitivePath := filepath.Join(dir, "secrets")

	err := ValidateMountPath(sensitivePath, MountReadOnly, []string{sensitivePath})
	assert.Error(t, err)

	// A non-sensitive sibling should be fine
	safePath := filepath.Join(dir, "workspace")
	err = ValidateMountPath(safePath, MountReadOnly, []string{sensitivePath})
	assert.NoError(t, err)
}

func TestValidateMountPath_ResolvesSymlinks(t *testing.T) {
	dir := t.TempDir()
	homeDir, _ := os.UserHomeDir()

	// Create a symlink pointing to home dir
	linkPath := filepath.Join(dir, "home_link")
	err := os.Symlink(homeDir, linkPath)
	require.NoError(t, err)

	// Mounting the symlink should be rejected since it resolves to $HOME
	err = ValidateMountPath(linkPath, MountReadOnly, nil)
	assert.Error(t, err, "symlink to $HOME should be rejected")

	// Create a symlink to a safe directory
	safeDir := filepath.Join(dir, "safe_dir")
	require.NoError(t, os.MkdirAll(safeDir, 0755))
	safeLink := filepath.Join(dir, "safe_link")
	require.NoError(t, os.Symlink(safeDir, safeLink))

	err = ValidateMountPath(safeLink, MountReadOnly, nil)
	assert.NoError(t, err, "symlink to safe dir should be accepted")
}

// Property: $HOME itself is always rejected.
func TestValidateMountPath_HomeAlwaysRejected_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		homeDir, _ := os.UserHomeDir()
		err := ValidateMountPath(homeDir, MountReadOnly, nil)
		require.Error(t, err, "$HOME (%q) must always be rejected", homeDir)
	})
}

// Property: /tmp and /opt subdirectories are always allowed.
func TestValidateMountPath_SafePrefixes_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		prefix := rapid.SampledFrom([]string{"/tmp", "/opt", "/var/tmp", "/srv"}).Draw(t, "prefix")
		subPath := rapid.StringMatching(`[a-zA-Z0-9_-]+(\/[a-zA-Z0-9_-]+)*`).Draw(t, "subPath")
		fullPath := filepath.Join(prefix, subPath)

		err := ValidateMountPath(fullPath, MountReadOnly, nil)
		assert.NoError(t, err, "path under %q should be accepted: %s", prefix, fullPath)
	})
}

// Property: Non-sensitive subdirectories of $HOME are always allowed.
func TestValidateMountPath_HomeSubdirsAllowed_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		homeDir, _ := os.UserHomeDir()
		// Generate non-sensitive subdir names
		name := rapid.StringMatching(`[a-zA-Z0-9][a-zA-Z0-9_-]*`).Draw(t, "name")
		// Skip names starting with "." that could match sensitive dirs
		fullPath := filepath.Join(homeDir, name)

		err := ValidateMountPath(fullPath, MountReadOnly, nil)
		assert.NoError(t, err, "non-dotfile subdir of $HOME should be accepted: %s", fullPath)
	})
}

// Property: Dotfile children of $HOME that aren't in the sensitive list are allowed.
func TestValidateMountPath_NonSensitiveDotfiles_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		homeDir, _ := os.UserHomeDir()
		// Generate dotfile names that aren't in the sensitive list
		sensitive := map[string]bool{
			".ssh": true, ".aws": true, ".config": true, ".gnupg": true,
			".kube": true, ".docker": true, ".mozilla": true,
		}
		name := rapid.StringMatching(`\.([a-z0-9][a-z0-9_-]*)`).Draw(t, "name")
		if sensitive[name] {
			t.Skip("hit a known sensitive dir")
		}
		fullPath := filepath.Join(homeDir, name)

		err := ValidateMountPath(fullPath, MountReadOnly, nil)
		assert.NoError(t, err, "non-sensitive dotfile %q should be allowed", fullPath)
	})
}

// --- REQ-004-007: Wildcard Matching ---

func TestDomainMatches_ExactMatch(t *testing.T) {
	assert.True(t, DomainMatches("example.com", "example.com"))
	assert.True(t, DomainMatches("api.anthropic.com", "api.anthropic.com"))
	assert.False(t, DomainMatches("example.com", "other.com"))
}

func TestDomainMatches_WildcardSingleLevel(t *testing.T) {
	assert.True(t, DomainMatches("*.example.com", "foo.example.com"))
	assert.True(t, DomainMatches("*.example.com", "bar.example.com"))
	assert.True(t, DomainMatches("*.githubusercontent.com", "raw.githubusercontent.com"))
	assert.False(t, DomainMatches("*.example.com", "bar.foo.example.com"), "multi-level should not match")
	assert.False(t, DomainMatches("*.example.com", "example.com"), "bare domain should not match")
}

func TestDomainMatches_CaseInsensitive(t *testing.T) {
	assert.True(t, DomainMatches("EXAMPLE.COM", "example.com"))
	assert.True(t, DomainMatches("*.Example.COM", "foo.example.com"))
	assert.True(t, DomainMatches("*.example.com", "FOO.EXAMPLE.COM"))
}

func TestDomainMatches_Empty(t *testing.T) {
	assert.False(t, DomainMatches("", "example.com"))
	assert.False(t, DomainMatches("*.example.com", ""))
}

// Property: For any non-wildcard pattern, only exact match returns true.
func TestDomainMatches_ExactMatchOnly_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hostname := rapid.StringMatching(`[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+`).Draw(t, "hostname")
		pattern := rapid.StringMatching(`[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+`).Draw(t, "pattern")

		matches := DomainMatches(pattern, hostname)
		if pattern != hostname && !strings.HasPrefix(pattern, "*.") {
			assert.False(t, matches, "non-wildcard %q should not match %q", pattern, hostname)
		}
	})
}

// Property: Wildcard patterns match exactly one label.
func TestDomainMatches_WildcardSingleLabel_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		label := rapid.StringMatching(`[a-z0-9]([a-z0-9-]*[a-z0-9])?`).Draw(t, "label")
		domain := rapid.StringMatching(`[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+`).Draw(t, "domain")

		pattern := "*." + domain
		hostname := label + "." + domain

		assert.True(t, DomainMatches(pattern, hostname),
			"*.%s should match %s.%s", domain, label, domain)

		// Multi-level should not match
		multiLevel := "sub." + label + "." + domain
		assert.False(t, DomainMatches(pattern, multiLevel),
			"*.%s should NOT match %s", domain, multiLevel)
	})
}

// --- REQ-004-007: Default Egress Allowlist ---

func TestDefaultEgressAllowlist_ContainsRequiredDomains(t *testing.T) {
	required := []string{
		"api.anthropic.com",
		"github.com",
		"*.githubusercontent.com",
		"archive.ubuntu.com",
		"security.ubuntu.com",
		"deb.debian.org",
		"registry.npmjs.org",
		"pypi.org",
		"files.pythonhosted.org",
		"proxy.golang.org",
		"sum.golang.org",
	}

	for _, d := range required {
		found := false
		for _, a := range DefaultEgressAllowlist {
			if a == d {
				found = true
				break
			}
		}
		assert.True(t, found, "default allowlist missing %q", d)
	}
}

// Property: Every domain in the default allowlist is allowed.
func TestDefaultEgressAllowlist_AllDomainsAllowed_Property(t *testing.T) {
	allowlist := BuildEgressList(nil)
	rapid.Check(t, func(t *rapid.T) {
		idx := rapid.IntRange(0, len(DefaultEgressAllowlist)-1).Draw(t, "idx")
		domain := DefaultEgressAllowlist[idx]

		var hostname string
		if strings.HasPrefix(domain, "*.") {
			label := rapid.StringMatching(`[a-z0-9]([a-z0-9-]*[a-z0-9])?`).Draw(t, "label")
			hostname = label + domain[1:]
		} else {
			hostname = domain
		}

		assert.True(t, IsEgressAllowed(hostname, allowlist),
			"default domain %q should allow hostname %q", domain, hostname)
	})
}

// Property: Arbitrary hostnames are denied by default.
func TestDefaultEgressAllowlist_ArbitraryDenied_Property(t *testing.T) {
	allowlist := BuildEgressList(nil)
	rapid.Check(t, func(t *rapid.T) {
		label1 := rapid.StringMatching(`[a-z]{8}`).Draw(t, "label1")
		label2 := rapid.StringMatching(`[a-z]{8}`).Draw(t, "label2")
		hostname := label1 + "." + label2 + ".invalid"

		assert.False(t, IsEgressAllowed(hostname, allowlist),
			"arbitrary hostname %q should be denied", hostname)
	})
}

// --- REQ-004-008: Egress Allowlist Merging ---

func TestBuildEgressList_MergesUserAndDefault(t *testing.T) {
	userDomains := []string{"custom.api.example.com", "internal.registry.example.com"}
	result := BuildEgressList(userDomains)

	// All defaults should be present
	for _, d := range DefaultEgressAllowlist {
		found := false
		for _, ed := range result {
			if ed.Domain == d && ed.Source == DomainSourceDefault {
				found = true
				break
			}
		}
		assert.True(t, found, "default domain %q missing from merged list", d)
	}

	// User domains should be present
	for _, d := range userDomains {
		found := false
		for _, ed := range result {
			if ed.Domain == d && ed.Source == DomainSourceUser {
				found = true
				break
			}
		}
		assert.True(t, found, "user domain %q missing from merged list", d)
	}
}

func TestBuildEgressList_Deduplicates(t *testing.T) {
	result := BuildEgressList([]string{"github.com"})

	count := 0
	for _, ed := range result {
		if ed.Domain == "github.com" {
			count++
		}
	}
	assert.Equal(t, 1, count, "github.com should appear exactly once")
}

func TestBuildEgressList_EmptyUserDomains(t *testing.T) {
	result := BuildEgressList(nil)
	assert.Len(t, result, len(DefaultEgressAllowlist))
}

// --- REQ-004-012: Token Validation ---

func TestValidateToken_DetectsClassicPAT(t *testing.T) {
	warnings := ValidateToken("ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ", TokenGitHubPAT)
	require.Len(t, warnings, 1)
	assert.Equal(t, "CLASSIC_PAT", warnings[0].Code)
	assert.Contains(t, warnings[0].Message, "ghp_")
}

func TestValidateToken_AcceptsFinegrainedPAT(t *testing.T) {
	warnings := ValidateToken("github_pat_ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890", TokenGitHubPAT)
	assert.Empty(t, warnings)
}

func TestValidateToken_EmptyToken(t *testing.T) {
	warnings := ValidateToken("", TokenGitHubPAT)
	assert.Empty(t, warnings, "empty token should produce no warnings (it's just absent)")
}

func TestValidateToken_UnknownFormat(t *testing.T) {
	warnings := ValidateToken("some_random_string", TokenGitHubPAT)
	found := false
	for _, w := range warnings {
		if w.Code == "UNKNOWN_TOKEN_FORMAT" {
			found = true
		}
	}
	assert.True(t, found, "unknown format should produce UNKNOWN_TOKEN_FORMAT warning")
}

func TestValidateToken_AnthropicKey(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		wantCode string
		wantWarn bool
	}{
		{"valid_key", "sk-ant-api-key-12345", "", false},
		{"empty_key", "", "EMPTY_API_KEY", true},
		{"wrong_prefix", "sk-openai-key-123", "UNUSUAL_KEY_FORMAT", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := ValidateToken(tt.token, TokenAnthropicAPI)
			if tt.wantWarn {
				require.NotEmpty(t, warnings)
				assert.Equal(t, tt.wantCode, warnings[0].Code)
			} else {
				assert.Empty(t, warnings)
			}
		})
	}
}

// Property: Any token starting with "ghp_" always produces a CLASSIC_PAT warning.
func TestValidateToken_ClassicPATAlwaysWarns_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		suffix := rapid.StringMatching(`[A-Za-z0-9_]{10,}`).Draw(t, "suffix")
		token := "ghp_" + suffix

		warnings := ValidateToken(token, TokenGitHubPAT)
		found := false
		for _, w := range warnings {
			if w.Code == "CLASSIC_PAT" {
				found = true
			}
		}
		assert.True(t, found, "ghp_ token should always produce CLASSIC_PAT warning")
	})
}

// Property: Any token starting with "github_pat_" never produces a CLASSIC_PAT warning.
func TestValidateToken_FinegrainedNeverWarns_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		suffix := rapid.StringMatching(`[A-Za-z0-9_]{10,}`).Draw(t, "suffix")
		token := "github_pat_" + suffix

		warnings := ValidateToken(token, TokenGitHubPAT)
		for _, w := range warnings {
			assert.NotEqual(t, "CLASSIC_PAT", w.Code,
				"github_pat_ token should never produce CLASSIC_PAT warning")
		}
	})
}

// --- REQ-004-021: Credential Redaction ---

func TestRedactToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{"short", "short", "[REDACTED]"},
		{"exact8", "12345678", "[REDACTED]"},
		{"long", "sk-ant-api-key-1234567890", "sk-an...[REDACTED]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, RedactToken(tt.token))
		})
	}
}

// Property: Redacted token never contains the original token for tokens > 8 chars.
func TestRedactToken_NeverLeaks_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		token := rapid.StringMatching(`[A-Za-z0-9_-]{9,50}`).Draw(t, "token")
		redacted := RedactToken(token)
		assert.NotContains(t, redacted, token,
			"redacted token should not contain the original")
	})
}

// --- REQ-004-007: Wildcard Matching Edge Cases ---

func TestDomainMatches_WildcardEdgeCases(t *testing.T) {
	tests := []struct {
		pattern  string
		hostname string
		matches  bool
		desc     string
	}{
		{"*.com", "example.com", true, "single-label TLD wildcard"},
		{"*.com", "foo.example.com", false, "multi-label vs single TLD wildcard"},
		{"*", "anything", false, "bare star is not valid"},
		{"example.com", "example.com.", false, "trailing dot mismatch"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			result := DomainMatches(tt.pattern, tt.hostname)
			assert.Equal(t, tt.matches, result, "DomainMatches(%q, %q) = %v, want %v",
				tt.pattern, tt.hostname, result, tt.matches)
		})
	}
}

// --- REQ-004-025: DNS Resolver Domain Matching ---

func TestIsEgressAllowed_DefaultAllowlist(t *testing.T) {
	allowlist := BuildEgressList(nil)

	tests := []struct {
		hostname string
		allowed  bool
	}{
		{"api.anthropic.com", true},
		{"github.com", true},
		{"raw.githubusercontent.com", true},
		{"objects.githubusercontent.com", true},
		{"archive.ubuntu.com", true},
		{"proxy.golang.org", true},
		{"evil.example.com", false},
		{"attacker.com", false},
		{"api.evil.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.hostname, func(t *testing.T) {
			result := IsEgressAllowed(tt.hostname, allowlist)
			assert.Equal(t, tt.allowed, result, "IsEgressAllowed(%q) = %v, want %v",
				tt.hostname, result, tt.allowed)
		})
	}
}

// Property: IsEgressAllowed is consistent with DomainMatches for the default list.
func TestIsEgressAllowed_ConsistentWithDomainMatches_Property(t *testing.T) {
	allowlist := BuildEgressList(nil)
	rapid.Check(t, func(t *rapid.T) {
		idx := rapid.IntRange(0, len(DefaultEgressAllowlist)-1).Draw(t, "idx")
		pattern := DefaultEgressAllowlist[idx]

		var hostname string
		if strings.HasPrefix(pattern, "*.") {
			label := rapid.StringMatching(`[a-z0-9]([a-z0-9-]*[a-z0-9])?`).Draw(t, "label")
			hostname = label + pattern[1:]
		} else {
			hostname = pattern
		}

		dm := DomainMatches(pattern, hostname)
		ea := IsEgressAllowed(hostname, allowlist)
		assert.Equal(t, dm, ea, "DomainMatches(%q,%q)=%v but IsEgressAllowed(%q)=%v",
			pattern, hostname, dm, hostname, ea)
	})
}

// --- EgressDomain Validation ---

func TestValidateEgressDomain(t *testing.T) {
	tests := []struct {
		domain  string
		wantErr bool
	}{
		{"example.com", false},
		{"*.example.com", false},
		{"", true},
		{".example.com", true},
		{"example..com", true},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			err := ValidateEgressDomain(tt.domain)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Property: ValidateEgressDomain accepts well-formed domains.
func TestValidateEgressDomain_AcceptsWellFormed_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		label := rapid.StringMatching(`[a-z0-9]([a-z0-9-]*[a-z0-9])?`).Draw(t, "label")
		tld := rapid.StringMatching(`[a-z]{2,10}`).Draw(t, "tld")
		domain := label + "." + tld

		err := ValidateEgressDomain(domain)
		assert.NoError(t, err, "well-formed domain %q should be valid", domain)
	})
}

// --- Integration: Full Egress + Mount flow ---

func TestEgressList_UserDomainsAccessible(t *testing.T) {
	userDomains := []string{"custom.api.example.com", "repo.internal.example.com"}
	allowlist := BuildEgressList(userDomains)

	for _, d := range userDomains {
		assert.True(t, IsEgressAllowed(d, allowlist),
			"user-added domain %q should be accessible", d)
	}

	assert.True(t, IsEgressAllowed("api.anthropic.com", allowlist))
	assert.True(t, IsEgressAllowed("github.com", allowlist))
	assert.False(t, IsEgressAllowed("evil.example.com", allowlist))
}

// --- Benchmarks ---

func BenchmarkDomainMatches(b *testing.B) {
	for i := 0; i < b.N; i++ {
		DomainMatches("*.githubusercontent.com", "raw.githubusercontent.com")
	}
}

func BenchmarkIsEgressAllowed(b *testing.B) {
	allowlist := BuildEgressList(nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsEgressAllowed("raw.githubusercontent.com", allowlist)
	}
}

func BenchmarkValidateMountPath(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ValidateMountPath("/opt/workspace", MountReadOnly, nil)
	}
}

// Example demonstrating usage.
func ExampleBuildEgressList() {
	allowlist := BuildEgressList([]string{"custom.api.example.com"})
	for _, d := range allowlist {
		if d.Source == DomainSourceUser {
			fmt.Printf("user: %s\n", d.Domain)
		}
	}
	// Output: user: custom.api.example.com
}

func ExampleDomainMatches() {
	fmt.Println(DomainMatches("*.example.com", "foo.example.com"))
	fmt.Println(DomainMatches("*.example.com", "bar.foo.example.com"))
	// Output:
	// true
	// false
}
