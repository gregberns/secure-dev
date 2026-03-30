// Package security — tests for REQ-004-023: Sensitive Directory List Configurability.
package security

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"pgregory.net/rapid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Unit tests ---

func TestMergedSensitivePaths_EmptyExtra(t *testing.T) {
	result := MergedSensitivePaths(nil)

	// Must contain all built-in paths
	assert.NotEmpty(t, result, "merged list must not be empty")

	for _, entry := range result {
		assert.Equal(t, SensitivePathBuiltin, entry.Source,
			"with no extra paths, all entries must be builtin: %q", entry.Path)
	}
}

func TestMergedSensitivePaths_WithExtraPaths(t *testing.T) {
	tmpDir := t.TempDir()
	extraPath := filepath.Join(tmpDir, "custom-secrets")

	result := MergedSensitivePaths([]string{extraPath})

	// Must have builtin + user entry
	builtinCount := 0
	userCount := 0
	for _, entry := range result {
		if entry.Source == SensitivePathBuiltin {
			builtinCount++
		}
		if entry.Source == SensitivePathUser {
			userCount++
			assert.Equal(t, extraPath, entry.Path,
				"user entry must match the extra path")
		}
	}
	assert.Equal(t, len(sensitivePathEntries), builtinCount,
		"all builtin entries must be present")
	assert.Equal(t, 1, userCount, "exactly one user entry")
}

func TestMergedSensitivePaths_MultipleExtraPaths(t *testing.T) {
	tmpDir := t.TempDir()
	extra1 := filepath.Join(tmpDir, "secrets-a")
	extra2 := filepath.Join(tmpDir, "secrets-b")

	result := MergedSensitivePaths([]string{extra1, extra2})

	userCount := 0
	for _, entry := range result {
		if entry.Source == SensitivePathUser {
			userCount++
		}
	}
	assert.Equal(t, 2, userCount, "both user paths must be present")
}

func TestMergedSensitivePaths_ExpandsHome(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	result := MergedSensitivePaths([]string{"~/custom-secrets"})

	found := false
	for _, entry := range result {
		if entry.Source == SensitivePathUser {
			assert.Equal(t, filepath.Join(homeDir, "custom-secrets"), entry.Path,
				"~/ must be expanded to $HOME")
			found = true
		}
	}
	assert.True(t, found, "user path must be present in merged list")
}

func TestMergedSensitivePaths_CleansPaths(t *testing.T) {
	result := MergedSensitivePaths([]string{"/tmp/../opt/secrets//"})

	found := false
	for _, entry := range result {
		if entry.Source == SensitivePathUser {
			assert.Equal(t, "/opt/secrets", entry.Path,
				"user path must be cleaned")
			found = true
		}
	}
	assert.True(t, found, "user path must be present")
}

func TestMergedSensitivePaths_BuiltinsAlwaysPresent(t *testing.T) {
	// Even with extra paths, all builtins must be there
	extra := []string{"/custom/path1", "/custom/path2"}
	result := MergedSensitivePaths(extra)

	builtinPaths := make(map[string]bool)
	for _, entry := range result {
		if entry.Source == SensitivePathBuiltin {
			builtinPaths[entry.Path] = true
		}
	}

	defaultPaths := DefaultSensitivePaths()
	for _, dp := range defaultPaths {
		assert.True(t, builtinPaths[dp],
			"builtin path %q must be in merged list", dp)
	}
}

func TestValidateMountPath_WithMergedExtraPaths(t *testing.T) {
	tmpDir := t.TempDir()
	customSecrets := filepath.Join(tmpDir, "custom-secrets")

	// Mounting the user-configured sensitive path must be rejected
	err := ValidateMountPath(customSecrets, MountReadOnly, []string{customSecrets})
	require.Error(t, err)
	var mve *MountValidationError
	require.ErrorAs(t, err, &mve)
	assert.Equal(t, "user_configured", mve.Category)

	// A sibling that isn't configured should be fine
	safePath := filepath.Join(tmpDir, "workspace")
	err = ValidateMountPath(safePath, MountReadOnly, []string{customSecrets})
	assert.NoError(t, err)
}

func TestValidateMountPath_ExtraPathsRejectsChildren(t *testing.T) {
	tmpDir := t.TempDir()
	customSecrets := filepath.Join(tmpDir, "secrets")
	childPath := filepath.Join(customSecrets, "nested-file")

	err := ValidateMountPath(childPath, MountReadOnly, []string{customSecrets})
	assert.Error(t, err, "children of user-configured sensitive path must be rejected")
}

func TestSensitivePathEntry_JSONSerialization(t *testing.T) {
	entry := SensitivePathEntry{
		Path:   "/home/user/.ssh",
		Source: SensitivePathBuiltin,
	}

	data, err := json.Marshal(entry)
	require.NoError(t, err)

	var decoded SensitivePathEntry
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, entry.Path, decoded.Path)
	assert.Equal(t, entry.Source, decoded.Source)
}

// --- Property-based tests ---

// Property: MergedSensitivePaths always contains all default paths.
func TestProperty_MergedContainsAllDefaults(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		extra := rapid.SliceOfN(
			rapid.StringMatching(`/tmp/extra-[a-z0-9]{2,8}`),
			0, 5,
		).Draw(t, "extraPaths")

		result := MergedSensitivePaths(extra)

		defaultPaths := DefaultSensitivePaths()
		resultMap := make(map[string]SensitivePathSource)
		for _, entry := range result {
			resultMap[entry.Path] = entry.Source
		}

		for _, dp := range defaultPaths {
			_, ok := resultMap[dp]
			assert.True(t, ok, "default path %q must be in merged result", dp)
		}
	})
}

// Property: User entries always have source "user", builtin always "builtin".
func TestProperty_SourceConsistency(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		extra := rapid.SliceOfN(
			rapid.StringMatching(`/custom/[a-z0-9_-]{2,10}`),
			0, 5,
		).Draw(t, "extraPaths")

		result := MergedSensitivePaths(extra)

		builtinPaths := make(map[string]bool)
		for _, e := range sensitivePathEntries {
			builtinPaths[e.expanded] = true
		}

		for _, entry := range result {
			if builtinPaths[entry.Path] {
				assert.Equal(t, SensitivePathBuiltin, entry.Source,
					"builtin path must have source 'builtin': %q", entry.Path)
			}
		}
	})
}

// Property: Adding extra paths never reduces protection (builtin rejections still work).
func TestProperty_ExtraPathsNeverReduceProtection(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	builtinRejected := []string{
		filepath.Join(homeDir, ".ssh"),
		filepath.Join(homeDir, ".aws"),
		"/var/run/docker.sock",
	}

	rapid.Check(t, func(t *rapid.T) {
		path := rapid.SampledFrom(builtinRejected).Draw(t, "path")
		extra := rapid.SliceOfN(
			rapid.StringMatching(`/tmp/extra-[a-z0-9]{2,8}`),
			0, 5,
		).Draw(t, "extraPaths")

		err := ValidateMountPath(path, MountReadOnly, extra)
		assert.Error(t, err,
			"builtin sensitive path %q must still be rejected with extra paths", path)
	})
}

// Property: User-configured paths always reject their own path.
func TestProperty_UserConfiguredPathsAlwaysRejectThemselves(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		segment := rapid.StringMatching(`secret-[a-z0-9]{2,8}`).Draw(rt, "segment")
		tmpDir := t.TempDir()
		sensitivePath := filepath.Join(tmpDir, segment)

		err := ValidateMountPath(sensitivePath, MountReadOnly, []string{sensitivePath})
		assert.Error(t, err,
			"user-configured sensitive path must be rejected: %q", sensitivePath)
	})
}

// Property: MergedSensitivePaths JSON round-trip is lossless.
func TestProperty_MergedPathsJSONRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		extra := rapid.SliceOfN(
			rapid.StringMatching(`/custom/[a-z0-9_-]{2,10}`),
			0, 3,
		).Draw(t, "extraPaths")

		result := MergedSensitivePaths(extra)

		data, err := json.Marshal(result)
		require.NoError(t, err, "JSON marshal must succeed")

		var decoded []SensitivePathEntry
		require.NoError(t, json.Unmarshal(data, &decoded), "JSON unmarshal must succeed")

		assert.Equal(t, len(result), len(decoded),
			"round-trip must preserve entry count")

		for i, original := range result {
			assert.Equal(t, original.Path, decoded[i].Path,
				"path must round-trip at index %d", i)
			assert.Equal(t, original.Source, decoded[i].Source,
				"source must round-trip at index %d", i)
		}
	})
}

// Property: MergedSensitivePaths always produces non-nil result.
func TestProperty_MergedNeverNil(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		extra := rapid.SliceOfN(
			rapid.StringMatching(`/x/[a-z]{1,5}`),
			0, 3,
		).Draw(t, "extraPaths")

		result := MergedSensitivePaths(extra)
		assert.NotNil(t, result, "MergedSensitivePaths must never return nil")
		assert.NotEmpty(t, result, "MergedSensitivePaths must always contain builtins")
	})
}

// Property: Empty extra paths produce exactly the builtin count.
func TestProperty_EmptyExtraEqualsBuiltin(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		result := MergedSensitivePaths(nil)
		assert.Equal(t, len(sensitivePathEntries), len(result),
			"nil extra must produce exactly builtin count")

		result2 := MergedSensitivePaths([]string{})
		assert.Equal(t, len(sensitivePathEntries), len(result2),
			"empty slice extra must produce exactly builtin count")
	})
}
