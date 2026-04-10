// Package config - property-based tests for package name validation security.
// Tests that safe package names pass and shell metacharacters are rejected.
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// ============================================================
// Property: Valid package names always pass validation
// ============================================================

func TestProperty_ValidPackageNames_PassValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate safe package names: alphanumeric, hyphens, dots, slashes, @, +, _, =, :
		pkg := rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9._/@+=:-]{0,40}`).Draw(t, "pkg")
		err := validatePackageList([]string{pkg}, "apt", "test.yaml")
		assert.NoError(t, err, "valid package name %q should pass validation", pkg)
	})
}

// ============================================================
// Property: Package names with shell metacharacters are always rejected
// ============================================================

func TestProperty_ShellMetacharPackageNames_Rejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		meta := rapid.SampledFrom([]string{
			";", "|", "&", "$", "`", "(", ")", "{", "}", ">", "<",
			"\n", "\r", "\"", "'", "\\",
		}).Draw(t, "meta")
		prefix := rapid.StringMatching(`[a-z]{1,5}`).Draw(t, "prefix")
		suffix := rapid.StringMatching(`[a-z]{1,5}`).Draw(t, "suffix")
		pkg := prefix + meta + suffix

		err := validatePackageList([]string{pkg}, "apt", "test.yaml")
		assert.Error(t, err, "package name with metacharacter %q should be rejected: %s", meta, pkg)
	})
}

// ============================================================
// Property: Package names with only safe chars always pass for all manager types
// ============================================================

func TestProperty_SafePackageNames_AllManagers(t *testing.T) {
	managers := []string{"apt", "pip", "npm", "go", "cargo"}
	rapid.Check(t, func(t *rapid.T) {
		mgr := rapid.SampledFrom(managers).Draw(t, "mgr")
		pkg := rapid.StringMatching(`[a-z][a-z0-9._/@+=:-]{0,20}`).Draw(t, "pkg")
		err := validatePackageList([]string{pkg}, mgr, "test.yaml")
		assert.NoError(t, err, "safe package name %q for %s should pass", pkg, mgr)
	})
}

// ============================================================
// Property: Repo URLs with shell metacharacters are rejected
// ============================================================

func TestProperty_RepoURL_ShellMetacharsRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		meta := rapid.SampledFrom([]string{
			";", "|", "&", "$", "`", "(", ")", "{", "}",
		}).Draw(t, "meta")
		// Build a URL that passes the HTTPS prefix check but contains a metachar
		repoURL := "https://github.com/user" + meta + "/repo.git"

		cfg := &ProjectConfig{
			Name: "test-vm",
			Repo: repoURL,
		}
		err := validateDeclarativeFields(cfg, "test.yaml")
		assert.Error(t, err, "repo URL with metacharacter %q should be rejected", meta)
	})
}

// ============================================================
// Property: Valid HTTPS repo URLs pass validation
// ============================================================

func TestProperty_ValidHTTPSRepoURL_PassesValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		user := rapid.StringMatching(`[a-z][a-z0-9-]{0,10}`).Draw(t, "user")
		repo := rapid.StringMatching(`[a-z][a-z0-9-]{0,10}`).Draw(t, "repo")
		repoURL := "https://github.com/" + user + "/" + repo + ".git"

		cfg := &ProjectConfig{
			Name: "test-vm",
			Repo: repoURL,
		}
		err := validateDeclarativeFields(cfg, "test.yaml")
		assert.NoError(t, err, "valid HTTPS repo URL %q should pass", repoURL)
	})
}

// ============================================================
// Property: Branch names with shell metacharacters are rejected
// ============================================================

func TestProperty_BranchName_ShellMetacharsRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		meta := rapid.SampledFrom([]string{
			";", "|", "&", "$", "`", "(", ")", "{", "}",
		}).Draw(t, "meta")
		branch := "feature" + meta + "evil"

		cfg := &ProjectConfig{
			Name:   "test-vm",
			Repo:   "https://github.com/user/repo.git",
			Branch: branch,
		}
		err := validateDeclarativeFields(cfg, "test.yaml")
		assert.Error(t, err, "branch with metacharacter %q should be rejected", meta)
	})
}

// ============================================================
// Property: containsShellMeta is consistent with the declared metacharacters
// ============================================================

func TestProperty_ContainsShellMeta_DetectsAllDeclaredChars(t *testing.T) {
	declaredMetas := []string{
		";", "|", "&", "$", "`", "(", ")", "{", "}", ">", "<",
		"\n", "\r", "\"", "'", "\\", "!",
	}
	rapid.Check(t, func(t *rapid.T) {
		meta := rapid.SampledFrom(declaredMetas).Draw(t, "meta")
		prefix := rapid.StringMatching(`[a-z]{0,5}`).Draw(t, "prefix")
		suffix := rapid.StringMatching(`[a-z]{0,5}`).Draw(t, "suffix")
		s := prefix + meta + suffix
		assert.True(t, containsShellMeta(s),
			"containsShellMeta should detect metachar %q in %q", meta, s)
	})
}

// ============================================================
// Property: Strings without shell metacharacters pass containsShellMeta
// ============================================================

func TestProperty_ContainsShellMeta_SafeStringsPass(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.StringMatching(`[a-zA-Z0-9._/@+=:~,-]{0,50}`).Draw(t, "safe")
		assert.False(t, containsShellMeta(s),
			"containsShellMeta should not flag safe string %q", s)
	})
}

// ============================================================
// Property: validatePackageList rejects empty-string entries
// ============================================================

func TestProperty_ValidatePackageList_RejectsEmptyStrings(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 5).Draw(t, "n")
		emptyIdx := rapid.IntRange(0, n-1).Draw(t, "emptyIdx")
		pkgs := make([]string, n)
		for i := range pkgs {
			if i == emptyIdx {
				// Empty or whitespace-only
				pkgs[i] = rapid.SampledFrom([]string{"", " ", "\t", "  "}).Draw(t, "empty")
			} else {
				pkgs[i] = rapid.StringMatching(`[a-z][a-z0-9-]{0,10}`).Draw(t, "pkg")
			}
		}
		err := validatePackageList(pkgs, "apt", "test.yaml")
		require.Error(t, err, "empty package at index %d should be rejected", emptyIdx)
		assert.Contains(t, err.Error(), "must be a non-empty string")
	})
}

// ============================================================
// Integration: End-to-end validation through LoadProjectConfig with file I/O
// ============================================================

func TestProperty_LoadProjectConfig_ShellInjectionInPackages_Rejected(t *testing.T) {
	// This test uses file I/O so it runs as a standard table-driven test
	// wrapping rapid for the property part.
	dir := t.TempDir()
	metas := []string{";", "|", "&", "$", "`", "(", ")"}
	for _, meta := range metas {
		t.Run("meta="+meta, func(t *testing.T) {
			pkg := "safe" + meta + "evil"
			content := "name: test-vm\npackages:\n  apt:\n    - '" + pkg + "'\n"
			path := filepath.Join(dir, ".sd.yaml")
			require.NoError(t, os.WriteFile(path, []byte(content), 0644))

			_, err := LoadProjectConfig(path)
			assert.Error(t, err, "package %q with metachar %q should be rejected", pkg, meta)
		})
	}
}

func TestProperty_LoadProjectConfig_SafePackages_Accepted(t *testing.T) {
	dir := t.TempDir()
	safePackages := []string{
		"jq",
		"python3-dev",
		"golang.org/x/tools/cmd/goimports@latest",
		"black",
		"prettier",
		"ripgrep",
		"package@v1.2.3",
		"some.org/pkg/cmd@v0.1.0",
	}
	for _, pkg := range safePackages {
		t.Run("pkg="+pkg, func(t *testing.T) {
			content := "name: test-vm\npackages:\n  apt:\n    - " + pkg + "\n"
			path := filepath.Join(dir, ".sd.yaml")
			require.NoError(t, os.WriteFile(path, []byte(content), 0644))

			_, err := LoadProjectConfig(path)
			assert.NoError(t, err, "safe package %q should be accepted", pkg)
		})
	}
}
