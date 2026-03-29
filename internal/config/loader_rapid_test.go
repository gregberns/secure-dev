// Property-based tests for the configuration loading system using rapid.
// These tests verify critical invariants that must hold across all possible inputs.
// REQ-005-001: Precedence order
// REQ-005-004: Built-in defaults completeness
// REQ-005-008: Environment variable resolution
// REQ-005-017: Security key filtering in project config
package config

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

// tmpDir creates a temp directory for use inside a rapid.Check property.
func tmpDir() string {
	dir, err := os.MkdirTemp("", "sd-config-rapid-*")
	if err != nil {
		panic("tmpDir: " + err.Error())
	}
	return dir
}

// writeFile writes content to path, creating parent directories.
func writeFile(path, content string) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		panic("writeFile mkdir: " + err.Error())
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		panic("writeFile: " + err.Error())
	}
}

// --- Invariant: Defaults completeness ---

func TestProperty_DefaultsAlwaysPresent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir := tmpDir()
		defer os.RemoveAll(dir)

		l := NewLoader(WithSDHome(dir))
		require.NoError(t, l.Load())

		cfg := l.Get()

		assert.Equal(t, "lima", cfg.Defaults.Backend)
		assert.Equal(t, 4, cfg.Defaults.CPUs)
		assert.Equal(t, "8GiB", cfg.Defaults.Memory)
		assert.Equal(t, "100GiB", cfg.Defaults.Disk)
		assert.Equal(t, "ubuntu:24.04", cfg.Defaults.Image)
		assert.Equal(t, "", cfg.Defaults.VM)
		assert.Equal(t, MountPolicyNone, cfg.Security.MountPolicy)
		assert.Equal(t, DefaultEgressAllowlist, cfg.Security.EgressAllowlist)
		assert.NotNil(t, cfg.VMs)
		assert.Empty(t, cfg.VMs)
	})
}

// --- Invariant: User config overrides only specified keys ---

func TestProperty_UserConfigPartialOverride(t *testing.T) {
	cpuGen := rapid.IntRange(1, 64)

	rapid.Check(t, func(t *rapid.T) {
		dir := tmpDir()
		defer os.RemoveAll(dir)

		cpus := cpuGen.Draw(t, "cpus")

		writeFile(filepath.Join(dir, "config.yaml"), fmt.Sprintf(`
defaults:
  cpus: %d
`, cpus))

		l := NewLoader(WithSDHome(dir))
		require.NoError(t, l.Load())

		cfg := l.Get()

		assert.Equal(t, cpus, cfg.Defaults.CPUs)
		assert.Equal(t, "lima", cfg.Defaults.Backend)
		assert.Equal(t, "8GiB", cfg.Defaults.Memory)
		assert.Equal(t, "100GiB", cfg.Defaults.Disk)
		assert.Equal(t, "ubuntu:24.04", cfg.Defaults.Image)
		assert.Equal(t, MountPolicyNone, cfg.Security.MountPolicy)
		assert.Equal(t, DefaultEgressAllowlist, cfg.Security.EgressAllowlist)
	})
}

// --- Invariant: Environment variables always override config files ---

func TestProperty_EnvOverridesConfig(t *testing.T) {
	backendGen := rapid.SampledFrom([]string{"lima", "docker", "avf", "incus"})

	rapid.Check(t, func(t *rapid.T) {
		dir := tmpDir()
		defer os.RemoveAll(dir)

		configBackend := backendGen.Draw(t, "config_backend")
		envBackend := backendGen.Draw(t, "env_backend")

		writeFile(filepath.Join(dir, "config.yaml"), fmt.Sprintf(`
defaults:
  backend: %s
`, configBackend))

		require.NoError(t, os.Setenv("SD_DEFAULTS_BACKEND", envBackend))
		defer os.Unsetenv("SD_DEFAULTS_BACKEND")

		l := NewLoader(WithSDHome(dir))
		require.NoError(t, l.Load())

		cfg := l.Get()
		assert.Equal(t, envBackend, cfg.Defaults.Backend)
	})
}

// --- Invariant: Project config overrides user config ---

func TestProperty_ProjectOverridesUser(t *testing.T) {
	imageGen := rapid.SampledFrom([]string{
		"ubuntu:24.04", "ubuntu:22.04", "debian:12", "debian:11",
		"fedora:40", "alpine:3.19", "archlinux:latest",
	})

	rapid.Check(t, func(t *rapid.T) {
		sdHome := tmpDir()
		defer os.RemoveAll(sdHome)
		projectDir := tmpDir()
		defer os.RemoveAll(projectDir)

		userImage := imageGen.Draw(t, "user_image")
		projectImage := imageGen.Draw(t, "project_image")

		writeFile(filepath.Join(sdHome, "config.yaml"), fmt.Sprintf(`
defaults:
  image: %s
`, userImage))

		writeFile(filepath.Join(projectDir, "config.yaml"), fmt.Sprintf(`
defaults:
  image: %s
`, projectImage))

		l := NewLoader(WithSDHome(sdHome), WithProjectDir(projectDir))
		require.NoError(t, l.Load())

		cfg := l.Get()
		assert.Equal(t, projectImage, cfg.Defaults.Image)
	})
}

// --- Invariant: Security keys in project config are always ignored (REQ-005-017) ---

func TestProperty_SecurityKeysFilteredFromProjectConfig(t *testing.T) {
	policyGen := rapid.SampledFrom(ValidMountPolicies)
	domainGen := rapid.SampledFrom([]string{
		"evil.example.com", "attacker.com", "malware.test",
		"exfil.evil", "c2.dark.invalid", "steal.credentials.org",
	})

	rapid.Check(t, func(t *rapid.T) {
		sdHome := tmpDir()
		defer os.RemoveAll(sdHome)
		projectDir := tmpDir()
		defer os.RemoveAll(projectDir)

		userPolicy := policyGen.Draw(t, "user_policy")
		attackDomain := domainGen.Draw(t, "attack_domain")

		writeFile(filepath.Join(sdHome, "config.yaml"), fmt.Sprintf(`
security:
  mount_policy: %s
`, userPolicy))

		writeFile(filepath.Join(projectDir, "config.yaml"), fmt.Sprintf(`
security:
  mount_policy: project
  egress_allowlist:
    - %s
`, attackDomain))

		l := NewLoader(WithSDHome(sdHome), WithProjectDir(projectDir))
		require.NoError(t, l.Load())

		cfg := l.Get()

		assert.Equal(t, userPolicy, cfg.Security.MountPolicy)
		assert.NotContains(t, cfg.Security.EgressAllowlist, attackDomain)

		warnings := l.Warnings()
		assert.NotEmpty(t, warnings, "security key in project config must produce warning")
	})
}

// --- Invariant: All security keys are filtered from project config ---

func TestProperty_AllSecurityKeysFiltered(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		sdHome := tmpDir()
		defer os.RemoveAll(sdHome)
		projectDir := tmpDir()
		defer os.RemoveAll(projectDir)

		writeFile(filepath.Join(projectDir, "config.yaml"), `
security:
  mount_policy: project
  egress_allowlist:
    - evil.example.com
`)

		l := NewLoader(WithSDHome(sdHome), WithProjectDir(projectDir))
		require.NoError(t, l.Load())

		cfg := l.Get()
		assert.Equal(t, MountPolicyNone, cfg.Security.MountPolicy,
			"project config security keys must be ignored")
	})
}

// --- Invariant: Load is idempotent ---

func TestProperty_LoadIdempotent(t *testing.T) {
	cpuGen := rapid.IntRange(1, 128)
	memGen := rapid.SampledFrom([]string{"2GiB", "4GiB", "8GiB", "16GiB", "32GiB", "64GiB"})

	rapid.Check(t, func(t *rapid.T) {
		dir := tmpDir()
		defer os.RemoveAll(dir)

		cpus := cpuGen.Draw(t, "cpus")
		mem := memGen.Draw(t, "memory")

		writeFile(filepath.Join(dir, "config.yaml"), fmt.Sprintf(`
defaults:
  cpus: %d
  memory: %s
`, cpus, mem))

		l := NewLoader(WithSDHome(dir))
		require.NoError(t, l.Load())
		cfg1 := l.Get()

		require.NoError(t, l.Load())
		cfg2 := l.Get()

		assert.Equal(t, cfg1.Defaults.CPUs, cfg2.Defaults.CPUs)
		assert.Equal(t, cfg1.Defaults.Memory, cfg2.Defaults.Memory)
		assert.Equal(t, cfg1.Defaults.Backend, cfg2.Defaults.Backend)
		assert.Equal(t, cfg1.Defaults.Disk, cfg2.Defaults.Disk)
		assert.Equal(t, cfg1.Defaults.Image, cfg2.Defaults.Image)
		assert.Equal(t, cfg1.Security.MountPolicy, cfg2.Security.MountPolicy)
	})
}

// --- Invariant: Set -> Reload roundtrip ---

func TestProperty_SetReloadRoundtrip(t *testing.T) {
	cpuGen := rapid.IntRange(1, 128)

	rapid.Check(t, func(t *rapid.T) {
		dir := tmpDir()
		defer os.RemoveAll(dir)

		cpus := cpuGen.Draw(t, "cpus")

		l := NewLoader(WithSDHome(dir))
		require.NoError(t, l.Load())
		require.NoError(t, l.Set("defaults.cpus", cpus, "user"))

		l2 := NewLoader(WithSDHome(dir))
		require.NoError(t, l2.Load())
		cfg := l2.Get()

		assert.Equal(t, cpus, cfg.Defaults.CPUs)
	})
}

// --- Invariant: Multiple VMs are independent ---

func TestProperty_MultipleVMsIndependent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir := tmpDir()
		defer os.RemoveAll(dir)

		vm1Name := rapid.StringMatching(`^[a-z][a-z0-9]{1,15}$`).Draw(t, "vm1")
		vm2Name := rapid.StringMatching(`^[a-z][a-z0-9]{1,15}$`).Draw(t, "vm2")
		if vm1Name == vm2Name {
			vm2Name = vm1Name + "z"
		}

		vm1Cpus := rapid.IntRange(1, 64).Draw(t, "vm1_cpus")
		vm2Cpus := rapid.IntRange(1, 64).Draw(t, "vm2_cpus")

		writeFile(filepath.Join(dir, "config.yaml"), fmt.Sprintf(`
vms:
  %s:
    cpus: %d
  %s:
    cpus: %d
`, vm1Name, vm1Cpus, vm2Name, vm2Cpus))

		l := NewLoader(WithSDHome(dir))
		require.NoError(t, l.Load())

		cfg := l.Get()
		require.Len(t, cfg.VMs, 2)
		assert.Equal(t, vm1Cpus, cfg.VMs[vm1Name].CPUs)
		assert.Equal(t, vm2Cpus, cfg.VMs[vm2Name].CPUs)
	})
}

// --- Invariant: Env var resolution preserves literal text ---

func TestProperty_ResolvePreservesPlainText(t *testing.T) {
	textGen := rapid.StringMatching(`^[A-Za-z0-9._:/\-]{1,100}$`)

	rapid.Check(t, func(t *rapid.T) {
		text := textGen.Draw(t, "text")
		result := ResolveEnvVars(text, nil)
		assert.Equal(t, text, result)
	})
}

// --- Invariant: $$ always produces literal $ ---

func TestProperty_EscapedDollarAlwaysLiteral(t *testing.T) {
	textGen := rapid.StringMatching(`^[A-Za-z0-9]{1,20}$`)

	rapid.Check(t, func(t *rapid.T) {
		text := textGen.Draw(t, "text")
		result := ResolveEnvVars("$$"+text, nil)
		assert.Equal(t, "$"+text, result)
	})
}

// --- Invariant: HasEnvVarRef detects all reference forms ---

func TestProperty_HasEnvVarRefConsistent(t *testing.T) {
	varNameGen := rapid.StringMatching(`^[A-Z][A-Z0-9_]{2,20}$`)

	rapid.Check(t, func(t *rapid.T) {
		varName := varNameGen.Draw(t, "var_name")

		assert.True(t, HasEnvVarRef("${"+varName+"}"))
		assert.True(t, HasEnvVarRef("$"+varName))
	})
}

// --- Invariant: MaskValue hides content beyond prefix ---

func TestProperty_MaskValueHidesContent(t *testing.T) {
	valueGen := rapid.StringMatching(`^[A-Za-z0-9]{10,200}$`)

	rapid.Check(t, func(t *rapid.T) {
		value := valueGen.Draw(t, "value")
		masked := MaskValue(value)

		assert.NotContains(t, masked, value[5:])
		assert.Contains(t, masked, "****")
		assert.True(t, strings.HasPrefix(masked, value[:5]))
	})
}

// --- Invariant: Invalid mount policies always rejected ---

func TestProperty_ValidateRejectsInvalidMountPolicy(t *testing.T) {
	invalidPolicyGen := rapid.SampledFrom([]string{
		"writable", "full", "yes", "no", "true", "false", "rw", "ro",
		"all", "open", "closed", "readwrite", "permissive", "admin",
	})

	rapid.Check(t, func(t *rapid.T) {
		policy := invalidPolicyGen.Draw(t, "invalid_policy")
		assert.Error(t, ValidateMountPolicy(policy))
	})
}

// --- Invariant: Invalid VM statuses always rejected ---

func TestProperty_ValidateRejectsInvalidVMStatus(t *testing.T) {
	invalidStatusGen := rapid.SampledFrom([]string{
		"pending", "destroyed", "active", "inactive", "ready",
		"done", "failed", "ok", "starting", "stopping", "booting",
	})

	rapid.Check(t, func(t *rapid.T) {
		status := invalidStatusGen.Draw(t, "invalid_status")
		assert.Error(t, ValidateVMStatus(status))
	})
}

// --- Invariant: Source tracking returns valid values ---

func TestProperty_SourceReturnsValidValues(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir := tmpDir()
		defer os.RemoveAll(dir)

		writeFile(filepath.Join(dir, "config.yaml"), `
defaults:
  cpus: 8
`)

		l := NewLoader(WithSDHome(dir))
		require.NoError(t, l.Load())

		validSources := map[Source]bool{
			SourceCLI: true, SourceEnv: true, SourceProjectConfig: true,
			SourceUserConfig: true, SourceVMConfig: true, SourceDefault: true,
		}

		for _, key := range []string{
			"defaults.backend", "defaults.cpus", "defaults.memory",
			"defaults.disk", "defaults.image", "security.mount_policy",
		} {
			src := l.Source(key)
			assert.True(t, validSources[src], "Source(%q) = %q, want valid", key, src)
		}

		assert.Equal(t, SourceUserConfig, l.Source("defaults.cpus"))
		// Note: defaults.backend reports SourceUserConfig because Viper's
		// IsSet returns true for default values when a parent key was set
		// in the config file. This is a known limitation of the Source method.
	})
}

// --- Invariant: VM inheritance chain (REQ-005-015) ---

func TestProperty_VMInheritanceChain(t *testing.T) {
	cpuGen := rapid.IntRange(1, 128)

	rapid.Check(t, func(t *rapid.T) {
		sdHome := tmpDir()
		defer os.RemoveAll(sdHome)

		defaultCpus := cpuGen.Draw(t, "default_cpus")
		vmCpus := cpuGen.Draw(t, "vm_cpus")
		if defaultCpus == vmCpus {
			vmCpus = defaultCpus + 1
		}

		writeFile(filepath.Join(sdHome, "config.yaml"), fmt.Sprintf(`
defaults:
  cpus: %d
  memory: 8GiB
  disk: 100GiB
  image: ubuntu:24.04
vms:
  testvm:
    cpus: %d
`, defaultCpus, vmCpus))

		l := NewLoader(WithSDHome(sdHome))
		require.NoError(t, l.Load())

		vmCfg, err := l.GetForVM("testvm")
		require.NoError(t, err)

		assert.Equal(t, vmCpus, vmCfg.CPUs)
		assert.Equal(t, "8GiB", vmCfg.Memory)
		assert.Equal(t, "100GiB", vmCfg.Disk)
		assert.Equal(t, "ubuntu:24.04", vmCfg.Image)
	})
}

// --- Invariant: No config files never errors ---

func TestProperty_NoConfigFilesNeverErrors(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir := tmpDir()
		defer os.RemoveAll(dir)

		l := NewLoader(WithSDHome(dir))
		assert.NoError(t, l.Load())
		assert.Equal(t, DefaultConfig(), l.Get())
	})
}

// --- Invariant: Valid mount policies always accepted ---

func TestProperty_ValidateAcceptsValidMountPolicies(t *testing.T) {
	policyGen := rapid.SampledFrom(ValidMountPolicies)

	rapid.Check(t, func(t *rapid.T) {
		assert.NoError(t, ValidateMountPolicy(policyGen.Draw(t, "policy")))
	})
}

// --- Invariant: Valid VM statuses always accepted ---

func TestProperty_ValidateAcceptsValidVMStatuses(t *testing.T) {
	statusGen := rapid.SampledFrom(ValidVMStatuses)

	rapid.Check(t, func(t *rapid.T) {
		assert.NoError(t, ValidateVMStatus(statusGen.Draw(t, "status")))
	})
}

// --- Invariant: IsSecurityKey is correct for all keys ---

func TestProperty_IsSecurityKeyCoversAllDefinedKeys(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		for _, key := range SecurityKeys {
			assert.True(t, IsSecurityKey(key), "IsSecurityKey(%q) must be true", key)
		}
		for _, key := range []string{"defaults.backend", "defaults.cpus", "vms.myvm.cpus", "output.json"} {
			assert.False(t, IsSecurityKey(key), "IsSecurityKey(%q) must be false", key)
		}
	})
}

// --- Invariant: Validate detects invalid mount policy on disk ---

func TestProperty_ValidateDetectsInvalidMountPolicyOnDisk(t *testing.T) {
	invalidPolicyGen := rapid.SampledFrom([]string{
		"writable", "full", "yes", "no", "true", "false", "rw", "ro",
		"all", "open", "closed", "readwrite", "permissive", "admin",
	})

	rapid.Check(t, func(t *rapid.T) {
		dir := tmpDir()
		defer os.RemoveAll(dir)

		badPolicy := invalidPolicyGen.Draw(t, "bad_policy")

		writeFile(filepath.Join(dir, "config.yaml"), fmt.Sprintf(`
security:
  mount_policy: %s
`, badPolicy))

		l := NewLoader(WithSDHome(dir))
		require.NoError(t, l.Load())

		results, err := l.Validate()
		require.NoError(t, err)

		found := false
		for _, r := range results {
			if strings.Contains(r.Path, "config.yaml") && !r.Valid {
				found = true
				assert.Contains(t, strings.Join(r.Errors, " "), "mount_policy")
			}
		}
		assert.True(t, found, "validate must detect invalid mount_policy %q", badPolicy)
	})
}

// --- Invariant: Full precedence chain env > project > user ---

func TestProperty_FullPrecedenceChain(t *testing.T) {
	cpuGen := rapid.IntRange(1, 64)

	rapid.Check(t, func(t *rapid.T) {
		sdHome := tmpDir()
		defer os.RemoveAll(sdHome)
		projectDir := tmpDir()
		defer os.RemoveAll(projectDir)

		userCpus := cpuGen.Draw(t, "user_cpus")
		projectCpus := cpuGen.Draw(t, "project_cpus")
		envCpus := cpuGen.Draw(t, "env_cpus")

		writeFile(filepath.Join(sdHome, "config.yaml"), fmt.Sprintf(`
defaults:
  cpus: %d
`, userCpus))

		writeFile(filepath.Join(projectDir, "config.yaml"), fmt.Sprintf(`
defaults:
  cpus: %d
`, projectCpus))

		require.NoError(t, os.Setenv("SD_DEFAULTS_CPUS", fmt.Sprintf("%d", envCpus)))
		defer os.Unsetenv("SD_DEFAULTS_CPUS")

		l := NewLoader(WithSDHome(sdHome), WithProjectDir(projectDir))
		require.NoError(t, l.Load())

		cfg := l.Get()
		assert.Equal(t, envCpus, cfg.Defaults.CPUs)
	})
}

// --- Invariant: VM config file overrides VM section in user config (REQ-005-015) ---

func TestProperty_VMConfigFileOverridesVMSection(t *testing.T) {
	cpuGen := rapid.IntRange(1, 128)

	rapid.Check(t, func(t *rapid.T) {
		sdHome := tmpDir()
		defer os.RemoveAll(sdHome)

		vmSectionCpus := cpuGen.Draw(t, "vm_section_cpus")
		vmFileCpus := cpuGen.Draw(t, "vm_file_cpus")
		if vmSectionCpus == vmFileCpus {
			vmFileCpus = vmSectionCpus + 1
		}

		writeFile(filepath.Join(sdHome, "config.yaml"), fmt.Sprintf(`
defaults:
  cpus: 4
vms:
  testvm:
    cpus: %d
`, vmSectionCpus))

		vmDir := filepath.Join(sdHome, "vms", "testvm")
		if err := os.MkdirAll(vmDir, 0755); err != nil {
			t.Fatal(err)
		}
		writeFile(filepath.Join(vmDir, "config.yaml"), fmt.Sprintf(`
cpus: %d
memory: 32GiB
`, vmFileCpus))

		l := NewLoader(WithSDHome(sdHome))
		require.NoError(t, l.Load())

		vmCfg, err := l.GetForVM("testvm")
		require.NoError(t, err)

		assert.Equal(t, vmFileCpus, vmCfg.CPUs)
		assert.Equal(t, "32GiB", vmCfg.Memory)
	})
}

// --- Invariant: Validate detects invalid VM status on disk ---

func TestProperty_ValidateDetectsInvalidVMStatus(t *testing.T) {
	invalidStatusGen := rapid.SampledFrom([]string{
		"pending", "destroyed", "active", "inactive", "ready",
		"done", "failed", "ok", "starting", "stopping", "booting",
	})

	rapid.Check(t, func(t *rapid.T) {
		dir := tmpDir()
		defer os.RemoveAll(dir)

		badStatus := invalidStatusGen.Draw(t, "bad_status")

		writeFile(filepath.Join(dir, "config.yaml"), fmt.Sprintf(`
state:
  status: %s
`, badStatus))

		l := NewLoader(WithSDHome(dir))
		require.NoError(t, l.Load())

		results, err := l.Validate()
		require.NoError(t, err)

		found := false
		for _, r := range results {
			if strings.Contains(r.Path, "config.yaml") && !r.Valid {
				found = true
			}
		}
		assert.True(t, found, "validate must detect invalid VM status %q", badStatus)
	})
}
