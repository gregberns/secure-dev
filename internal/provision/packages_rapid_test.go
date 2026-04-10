// Package provision - property-based tests for package installation security.
// Tests shell injection defenses and no-op behavior for empty/nil packages.
package provision

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"sd/internal/config"
)

// ============================================================
// Property: InstallPackages with nil packages is a no-op
// ============================================================

func TestProperty_InstallPackages_NilPackages_NoOp(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`[a-z][a-z0-9-]{0,20}`).Draw(t, "vmName")
		execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
			t.Fatalf("execFn should not be called for nil packages")
			return "", "", 0, nil
		}
		err := InstallPackages(context.Background(), execFn, vmName, nil)
		assert.NoError(t, err)
	})
}

// ============================================================
// Property: InstallPackages with empty PackageConfig is a no-op
// ============================================================

func TestProperty_InstallPackages_EmptyPackages_NoOp(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`[a-z][a-z0-9-]{0,20}`).Draw(t, "vmName")
		called := false
		execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
			called = true
			return "", "", 0, nil
		}
		err := InstallPackages(context.Background(), execFn, vmName, &config.PackageConfig{})
		assert.NoError(t, err)
		assert.False(t, called, "execFn should not be called for empty packages")
	})
}

// ============================================================
// Property: Safe package names are shell-quoted but remain functional
// ============================================================

func TestProperty_SafePackageNames_PreservedInScript(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate safe package names (alphanumeric, hyphens, dots, slashes, @)
		pkg := rapid.StringMatching(`[a-z][a-z0-9._/@+-]{0,30}`).Draw(t, "pkg")
		var scripts []string
		execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
			scripts = append(scripts, command[len(command)-1])
			return "", "", 0, nil
		}
		pkgs := &config.PackageConfig{Apt: []string{pkg}}
		err := InstallPackages(context.Background(), execFn, "test-vm", pkgs)
		require.NoError(t, err)

		// The package name should appear in the install script (possibly quoted)
		found := false
		for _, s := range scripts {
			if strings.Contains(s, "apt-get install") {
				found = true
				// The package name should be present either directly or quoted
				assert.True(t, strings.Contains(s, pkg) || strings.Contains(s, "'"+pkg+"'"),
					"package name %q should appear in script: %s", pkg, s)
			}
		}
		assert.True(t, found, "expected apt-get install script")
	})
}

// ============================================================
// Property: Shell metacharacters in package names are safely quoted
// ============================================================

func TestProperty_ShellMetacharsInPackages_AreQuoted(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a package name with a shell metacharacter injected.
		// Note: in real use, config validation would reject these.
		// This tests the defense-in-depth quoting layer.
		meta := rapid.SampledFrom([]string{";", "|", "&", "$", "`", "(", ")", "{", "}"}).Draw(t, "meta")
		pkg := "pkg" + meta + "evil"

		var scripts []string
		execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
			scripts = append(scripts, command[len(command)-1])
			return "", "", 0, nil
		}
		pkgs := &config.PackageConfig{Apt: []string{pkg}}
		err := InstallPackages(context.Background(), execFn, "test-vm", pkgs)
		require.NoError(t, err)

		// The metacharacter must be inside single quotes, not bare
		for _, s := range scripts {
			if strings.Contains(s, "apt-get install") {
				// The raw metacharacter should NOT appear outside of quotes
				// The package must be wrapped in single quotes
				assert.Contains(t, s, "'"+pkg+"'",
					"package with metachar %q should be single-quoted in script: %s", meta, s)
			}
		}
	})
}

// ============================================================
// Property: shellQuote never produces empty output for non-empty input
// ============================================================

func TestProperty_ShellQuote_NeverEmpty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.String().Draw(t, "input")
		result := shellQuote(s)
		assert.NotEmpty(t, result, "shellQuote should never return empty for any input")
	})
}

// ============================================================
// Property: shellQuote output never contains unescaped single quotes for dangerous input
// ============================================================

func TestProperty_ShellQuote_SafeAgainstQuoteBreakout(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate strings that contain single quotes to try to break quoting
		prefix := rapid.StringMatching(`[a-z]{1,5}`).Draw(t, "prefix")
		suffix := rapid.StringMatching(`[a-z]{1,5}`).Draw(t, "suffix")
		s := prefix + "'" + suffix
		result := shellQuote(s)
		// The result should be properly quoted: starts and ends with single quote
		// with escaped internal quotes
		assert.True(t, strings.HasPrefix(result, "'"), "quoted string should start with single quote")
		assert.True(t, strings.HasSuffix(result, "'"), "quoted string should end with single quote")
		// The embedded single quote should be escaped as '\''
		assert.Contains(t, result, "'\\''", "embedded single quote should be escaped")
	})
}

// ============================================================
// Property: Go packages are installed one at a time with quoting
// ============================================================

func TestProperty_GoPackages_PerPackageQuoting(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		pkg := rapid.StringMatching(`[a-z][a-z0-9./]*@[a-z]+`).Draw(t, "pkg")
		var scripts []string
		execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
			scripts = append(scripts, command[len(command)-1])
			return "", "", 0, nil
		}
		pkgs := &config.PackageConfig{Go: []string{pkg}}
		err := InstallPackages(context.Background(), execFn, "test-vm", pkgs)
		require.NoError(t, err)

		// Find the go install script
		goInstalls := 0
		for _, s := range scripts {
			if strings.Contains(s, "go install") {
				goInstalls++
				// Package name should be present
				assert.True(t, strings.Contains(s, pkg),
					"go install script should contain package %q: %s", pkg, s)
			}
		}
		assert.Equal(t, 1, goInstalls, "expected exactly one go install invocation")
	})
}
