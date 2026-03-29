// Package config tests the configuration loading system.
// REQ-005-001 through REQ-005-018
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- REQ-005-004: Built-In Defaults ---

func TestDefaultConfig_HasAllRequiredDefaults(t *testing.T) {
	cfg := DefaultConfig()

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
}

func TestDefaultEgressAllowlist_MatchesSpec(t *testing.T) {
	expected := []string{
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
	assert.Equal(t, expected, DefaultEgressAllowlist)
}

// --- REQ-005-001: Configuration Precedence Order ---

func TestLoader_Precedence_DefaultsOnly(t *testing.T) {
	dir := t.TempDir()
	l := NewLoader(WithSDHome(dir))
	require.NoError(t, l.Load())

	cfg := l.Get()
	assert.Equal(t, "lima", cfg.Defaults.Backend)
	assert.Equal(t, 4, cfg.Defaults.CPUs)
}

func TestLoader_Precedence_UserOverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, filepath.Join(dir, "config.yaml"), `
defaults:
  cpus: 8
  memory: 16GiB
`)

	l := NewLoader(WithSDHome(dir))
	require.NoError(t, l.Load())

	cfg := l.Get()
	assert.Equal(t, 8, cfg.Defaults.CPUs)
	assert.Equal(t, "16GiB", cfg.Defaults.Memory)
	// Non-overridden defaults still present
	assert.Equal(t, "lima", cfg.Defaults.Backend)
	assert.Equal(t, "100GiB", cfg.Defaults.Disk)
}

func TestLoader_Precedence_ProjectOverridesUser(t *testing.T) {
	sdHome := t.TempDir()
	projectDir := t.TempDir()

	writeConfig(t, filepath.Join(sdHome, "config.yaml"), `
defaults:
  cpus: 4
  memory: 8GiB
`)

	writeConfig(t, filepath.Join(projectDir, "config.yaml"), `
defaults:
  cpus: 16
`)

	l := NewLoader(WithSDHome(sdHome), WithProjectDir(projectDir))
	require.NoError(t, l.Load())

	cfg := l.Get()
	// Project overrides user for cpus
	assert.Equal(t, 16, cfg.Defaults.CPUs)
	// User value is preserved where project doesn't override
	assert.Equal(t, "8GiB", cfg.Defaults.Memory)
}

func TestLoader_Precedence_EnvOverridesAll(t *testing.T) {
	sdHome := t.TempDir()
	projectDir := t.TempDir()

	writeConfig(t, filepath.Join(sdHome, "config.yaml"), `
defaults:
  cpus: 4
`)

	writeConfig(t, filepath.Join(projectDir, "config.yaml"), `
defaults:
  cpus: 8
`)

	t.Setenv("SD_DEFAULTS_CPUS", "32")

	l := NewLoader(WithSDHome(sdHome), WithProjectDir(projectDir))
	require.NoError(t, l.Load())

	cfg := l.Get()
	// Environment overrides everything
	assert.Equal(t, 32, cfg.Defaults.CPUs)
}

// --- REQ-005-002: User-Level Config File ---

func TestLoader_UserConfig_DefaultSDHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SD_HOME", dir)

	writeConfig(t, filepath.Join(dir, "config.yaml"), `
defaults:
  backend: docker
`)

	l := NewLoader()
	require.NoError(t, l.Load())

	cfg := l.Get()
	assert.Equal(t, "docker", cfg.Defaults.Backend)
}

func TestLoader_UserConfig_CustomSDHome(t *testing.T) {
	dir := t.TempDir()

	writeConfig(t, filepath.Join(dir, "config.yaml"), `
defaults:
  backend: custom
`)

	l := NewLoader(WithSDHome(dir))
	require.NoError(t, l.Load())

	cfg := l.Get()
	assert.Equal(t, "custom", cfg.Defaults.Backend)
}

func TestLoader_UserConfig_AbsentIsNotError(t *testing.T) {
	dir := t.TempDir()
	l := NewLoader(WithSDHome(filepath.Join(dir, "nonexistent")))
	require.NoError(t, l.Load())

	cfg := l.Get()
	assert.Equal(t, "lima", cfg.Defaults.Backend)
}

func TestLoader_UserConfig_InvalidYAMLIsError(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, filepath.Join(dir, "config.yaml"), `
defaults:
  cpus: [broken
`)

	l := NewLoader(WithSDHome(dir))
	err := l.Load()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "user config")
}

// --- REQ-005-003: Project-Level Config File ---

func TestLoader_ProjectConfig_Loaded(t *testing.T) {
	sdHome := t.TempDir()
	projectDir := t.TempDir()

	writeConfig(t, filepath.Join(projectDir, "config.yaml"), `
defaults:
  image: debian:12
`)

	l := NewLoader(WithSDHome(sdHome), WithProjectDir(projectDir))
	require.NoError(t, l.Load())

	cfg := l.Get()
	assert.Equal(t, "debian:12", cfg.Defaults.Image)
}

func TestLoader_ProjectConfig_AbsentIsNotError(t *testing.T) {
	sdHome := t.TempDir()
	l := NewLoader(WithSDHome(sdHome), WithProjectDir(t.TempDir()))
	require.NoError(t, l.Load())
}

func TestLoader_ProjectConfig_InvalidYAMLIsError(t *testing.T) {
	sdHome := t.TempDir()
	projectDir := t.TempDir()

	writeConfig(t, filepath.Join(projectDir, "config.yaml"), `
defaults: {broken yaml
`)

	l := NewLoader(WithSDHome(sdHome), WithProjectDir(projectDir))
	err := l.Load()
	assert.Error(t, err)
}

// --- REQ-005-005: Environment Variable Mapping ---

func TestLoader_EnvVar_SD_BACKEND(t *testing.T) {
	t.Setenv("SD_BACKEND", "docker")
	dir := t.TempDir()

	l := NewLoader(WithSDHome(dir))
	require.NoError(t, l.Load())

	cfg := l.Get()
	assert.Equal(t, "docker", cfg.Defaults.Backend)
}

func TestLoader_EnvVar_SD_DEFAULT_VM(t *testing.T) {
	t.Setenv("SD_DEFAULT_VM", "myvm")
	dir := t.TempDir()

	l := NewLoader(WithSDHome(dir))
	require.NoError(t, l.Load())

	cfg := l.Get()
	assert.Equal(t, "myvm", cfg.Defaults.VM)
}

func TestLoader_EnvVar_NestedKeys(t *testing.T) {
	t.Setenv("SD_DEFAULTS_CPUS", "12")
	dir := t.TempDir()

	l := NewLoader(WithSDHome(dir))
	require.NoError(t, l.Load())

	cfg := l.Get()
	assert.Equal(t, 12, cfg.Defaults.CPUs)
}

// --- REQ-005-006: Config File Format ---

func TestLoader_FullConfigFile(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, filepath.Join(dir, "config.yaml"), `
defaults:
  backend: lima
  cpus: 4
  memory: 8GiB
  disk: 100GiB
  image: ubuntu:24.04
  vm: ""

security:
  egress_allowlist:
    - api.anthropic.com
    - github.com
  mount_policy: none

vms:
  myvm:
    cpus: 8
    memory: 16GiB
    backend: lima
    provisions:
      - claude-code
      - docker
    env:
      ANTHROPIC_API_KEY: "${ANTHROPIC_API_KEY}"
`)

	l := NewLoader(WithSDHome(dir))
	require.NoError(t, l.Load())

	cfg := l.Get()
	assert.Equal(t, "lima", cfg.Defaults.Backend)
	assert.Equal(t, 4, cfg.Defaults.CPUs)
	assert.Equal(t, 2, len(cfg.Security.EgressAllowlist))
	assert.Equal(t, MountPolicyNone, cfg.Security.MountPolicy)

	vm, ok := cfg.VMs["myvm"]
	require.True(t, ok)
	assert.Equal(t, 8, vm.CPUs)
	assert.Equal(t, "16GiB", vm.Memory)
	assert.Equal(t, []string{"claude-code", "docker"}, vm.Provisions)
}

func TestLoader_PartialConfigFile(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, filepath.Join(dir, "config.yaml"), `
defaults:
  cpus: 2
`)

	l := NewLoader(WithSDHome(dir))
	require.NoError(t, l.Load())

	cfg := l.Get()
	assert.Equal(t, 2, cfg.Defaults.CPUs)
	// Missing sections use defaults
	assert.Equal(t, MountPolicyNone, cfg.Security.MountPolicy)
	assert.Equal(t, DefaultEgressAllowlist, cfg.Security.EgressAllowlist)
}

// --- REQ-005-008: Sensitive Value Handling ---

func TestResolveEnvVars_BracedReference(t *testing.T) {
	t.Setenv("TEST_KEY", "resolved_value")
	result := ResolveEnvVars("${TEST_KEY}", nil)
	assert.Equal(t, "resolved_value", result)
}

func TestResolveEnvVars_BareReference(t *testing.T) {
	t.Setenv("TEST_KEY", "resolved_value")
	result := ResolveEnvVars("$TEST_KEY", nil)
	assert.Equal(t, "resolved_value", result)
}

func TestResolveEnvVars_EscapeDollar(t *testing.T) {
	result := ResolveEnvVars("$$LITERAL", nil)
	assert.Equal(t, "$LITERAL", result)
}

func TestResolveEnvVars_UnsetVariable(t *testing.T) {
	var warnings []string
	warn := func(msg string) { warnings = append(warnings, msg) }

	result := ResolveEnvVars("${SURELY_UNSET_VAR_12345}", warn)
	assert.Equal(t, "", result)
	assert.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "SURELY_UNSET_VAR_12345")
}

func TestResolveEnvVars_MultipleRefs(t *testing.T) {
	t.Setenv("VAR_A", "hello")
	t.Setenv("VAR_B", "world")

	result := ResolveEnvVars("${VAR_A} ${VAR_B}", nil)
	assert.Equal(t, "hello world", result)
}

func TestHasEnvVarRef(t *testing.T) {
	assert.True(t, HasEnvVarRef("${API_KEY}"))
	assert.True(t, HasEnvVarRef("$API_KEY"))
	assert.False(t, HasEnvVarRef("plain_text"))
	assert.False(t, HasEnvVarRef("$$escaped"))
}

func TestMaskValue(t *testing.T) {
	assert.Equal(t, "****", MaskValue("short"))
	assert.Equal(t, "sk-an****", MaskValue("sk-ant-api-key-12345"))
}

// --- REQ-005-017: Security Keys in Project Config Ignored ---

func TestLoader_ProjectConfig_SecurityKeysIgnored(t *testing.T) {
	sdHome := t.TempDir()
	projectDir := t.TempDir()

	writeConfig(t, filepath.Join(sdHome, "config.yaml"), `
security:
  mount_policy: none
`)

	writeConfig(t, filepath.Join(projectDir, "config.yaml"), `
security:
  mount_policy: project
  egress_allowlist:
    - evil.example.com
`)

	l := NewLoader(WithSDHome(sdHome), WithProjectDir(projectDir))
	require.NoError(t, l.Load())

	cfg := l.Get()
	// Security values from user config (or defaults) should be used, not project
	assert.Equal(t, MountPolicyNone, cfg.Security.MountPolicy)
	assert.NotContains(t, cfg.Security.EgressAllowlist, "evil.example.com")

	// Warning should have been emitted
	warnings := l.Warnings()
	assert.NotEmpty(t, warnings)
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "security.mount_policy") {
			found = true
		}
	}
	assert.True(t, found, "expected warning about security.mount_policy in project config")
}

// --- REQ-005-018: Mount Policy Validation ---

func TestValidateMountPolicy_Valid(t *testing.T) {
	for _, policy := range ValidMountPolicies {
		assert.NoError(t, ValidateMountPolicy(policy))
	}
}

func TestValidateMountPolicy_Invalid(t *testing.T) {
	err := ValidateMountPolicy("writable")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mount_policy")
	assert.Contains(t, err.Error(), "none, readonly, project")
}

// --- REQ-005-007: VM Status Validation ---

func TestValidateVMStatus_Valid(t *testing.T) {
	for _, status := range ValidVMStatuses {
		assert.NoError(t, ValidateVMStatus(status))
	}
}

func TestValidateVMStatus_Invalid(t *testing.T) {
	err := ValidateVMStatus("pending")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid vm status")
}

// --- REQ-005-010: Config Set Command ---

func TestLoader_Set_CreatesUserConfig(t *testing.T) {
	dir := t.TempDir()

	l := NewLoader(WithSDHome(dir))
	require.NoError(t, l.Load())

	err := l.Set("defaults.cpus", 8, "user")
	require.NoError(t, err)

	// Verify file was created
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "cpus")
}

func TestLoader_Set_UpdatesExisting(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, filepath.Join(dir, "config.yaml"), `
defaults:
  cpus: 4
`)

	l := NewLoader(WithSDHome(dir))
	require.NoError(t, l.Load())

	err := l.Set("defaults.cpus", 16, "user")
	require.NoError(t, err)

	// Reload and verify
	l2 := NewLoader(WithSDHome(dir))
	require.NoError(t, l2.Load())
	cfg := l2.Get()
	assert.Equal(t, 16, cfg.Defaults.CPUs)
}

func TestLoader_Set_ProjectConfig(t *testing.T) {
	sdHome := t.TempDir()
	projectDir := t.TempDir()

	l := NewLoader(WithSDHome(sdHome), WithProjectDir(projectDir))
	require.NoError(t, l.Load())

	err := l.Set("defaults.cpus", 8, "project")
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(projectDir, "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "cpus")
}

func TestLoader_Set_InvalidTarget(t *testing.T) {
	dir := t.TempDir()
	l := NewLoader(WithSDHome(dir))
	require.NoError(t, l.Load())

	err := l.Set("key", "value", "invalid")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid target")
}

// --- REQ-005-013: Config Validate ---

func TestLoader_Validate_ValidFiles(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, filepath.Join(dir, "config.yaml"), `
defaults:
  cpus: 4
security:
  mount_policy: none
`)

	l := NewLoader(WithSDHome(dir))
	require.NoError(t, l.Load())

	results, err := l.Validate()
	require.NoError(t, err)

	for _, r := range results {
		assert.True(t, r.Valid, "file %s should be valid, errors: %v", r.Path, r.Errors)
	}
}

func TestLoader_Validate_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, filepath.Join(dir, "config.yaml"), `
security:
  mount_policy: writable
`)

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
	assert.True(t, found, "expected validation error for invalid mount_policy")
}

// --- REQ-005-016: Config Directory Structure ---

func TestLoader_SDHome_DefaultsWithEnv(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SD_HOME", tmpDir)

	l := NewLoader()
	assert.Equal(t, tmpDir, l.SDHome())
}

// --- Property-based tests ---

// TestLoader_LoadIdempotent verifies that loading twice produces same result.
func TestLoader_LoadIdempotent(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, filepath.Join(dir, "config.yaml"), `
defaults:
  cpus: 4
  memory: 8GiB
`)

	l := NewLoader(WithSDHome(dir))
	require.NoError(t, l.Load())
	cfg1 := l.Get()

	require.NoError(t, l.Load())
	cfg2 := l.Get()

	assert.Equal(t, cfg1.Defaults.CPUs, cfg2.Defaults.CPUs)
	assert.Equal(t, cfg1.Defaults.Memory, cfg2.Defaults.Memory)
}

// TestLoader_MultipleVMs verifies multiple VM definitions are loaded.
func TestLoader_MultipleVMs(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, filepath.Join(dir, "config.yaml"), `
vms:
  vm1:
    cpus: 2
    memory: 4GiB
  vm2:
    cpus: 8
    memory: 16GiB
`)

	l := NewLoader(WithSDHome(dir))
	require.NoError(t, l.Load())

	cfg := l.Get()
	require.Len(t, cfg.VMs, 2)

	vm1 := cfg.VMs["vm1"]
	assert.Equal(t, 2, vm1.CPUs)
	assert.Equal(t, "4GiB", vm1.Memory)

	vm2 := cfg.VMs["vm2"]
	assert.Equal(t, 8, vm2.CPUs)
	assert.Equal(t, "16GiB", vm2.Memory)
}

// TestLoader_VMConfigInheritance tests REQ-005-015.
func TestLoader_VMConfigInheritance(t *testing.T) {
	sdHome := t.TempDir()

	writeConfig(t, filepath.Join(sdHome, "config.yaml"), `
defaults:
  backend: lima
  cpus: 4
  memory: 8GiB
  disk: 100GiB
  image: ubuntu:24.04
vms:
  myvm:
    cpus: 8
    memory: 16GiB
`)

	l := NewLoader(WithSDHome(sdHome))
	require.NoError(t, l.Load())

	vmCfg, err := l.GetForVM("myvm")
	require.NoError(t, err)

	// VM-specific values override defaults
	assert.Equal(t, 8, vmCfg.CPUs)
	assert.Equal(t, "16GiB", vmCfg.Memory)
	// Inherited from defaults
	assert.Equal(t, "100GiB", vmCfg.Disk)
	assert.Equal(t, "ubuntu:24.04", vmCfg.Image)
}

// TestLoader_VMConfigFileOverridesConfig tests REQ-005-015 step 3.
func TestLoader_VMConfigFileOverridesConfig(t *testing.T) {
	sdHome := t.TempDir()

	// User-level config defines VM with cpus=8
	writeConfig(t, filepath.Join(sdHome, "config.yaml"), `
defaults:
  cpus: 4
vms:
  testvm:
    cpus: 8
`)

	// VM-specific config file overrides to cpus=16
	vmDir := filepath.Join(sdHome, "vms", "testvm")
	require.NoError(t, os.MkdirAll(vmDir, 0755))
	writeConfig(t, filepath.Join(vmDir, "config.yaml"), `
cpus: 16
memory: 32GiB
state:
  status: running
`)

	l := NewLoader(WithSDHome(sdHome))
	require.NoError(t, l.Load())

	vmCfg, err := l.GetForVM("testvm")
	require.NoError(t, err)

	// VM file overrides user config
	assert.Equal(t, 16, vmCfg.CPUs)
	assert.Equal(t, "32GiB", vmCfg.Memory)
	assert.Equal(t, "running", vmCfg.State.Status)
}

// --- Helper functions ---

func writeConfig(t *testing.T, path string, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
}
