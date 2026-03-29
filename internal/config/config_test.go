package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSDHome(t *testing.T) {
	t.Run("uses SD_HOME when set", func(t *testing.T) {
		t.Setenv("SD_HOME", "/custom/path")
		if got := SDHome(); got != "/custom/path" {
			t.Errorf("SDHome() = %q, want %q", got, "/custom/path")
		}
	})

	t.Run("defaults to ~/.sd", func(t *testing.T) {
		t.Setenv("SD_HOME", "")
		home, _ := os.UserHomeDir()
		want := filepath.Join(home, ".sd")
		if got := SDHome(); got != want {
			t.Errorf("SDHome() = %q, want %q", got, want)
		}
	})
}

func TestFindProjectConfig(t *testing.T) {
	tmp := t.TempDir()

	t.Run("finds config in current directory", func(t *testing.T) {
		projDir := filepath.Join(tmp, "proj1")
		sdDir := filepath.Join(projDir, ".sd")
		writeFile(t, filepath.Join(sdDir, "config.yaml"), "defaults:\n  cpus: 2\n")

		got := FindProjectConfig(projDir)
		if got != sdDir {
			t.Errorf("FindProjectConfig(%q) = %q, want %q", projDir, got, sdDir)
		}
	})

	t.Run("walks up to find config in parent", func(t *testing.T) {
		parentDir := filepath.Join(tmp, "proj2")
		sdDir := filepath.Join(parentDir, ".sd")
		writeFile(t, filepath.Join(sdDir, "config.yaml"), "defaults:\n  cpus: 2\n")

		childDir := filepath.Join(parentDir, "sub", "deep")
		if err := os.MkdirAll(childDir, 0700); err != nil {
			t.Fatal(err)
		}

		got := FindProjectConfig(childDir)
		if got != sdDir {
			t.Errorf("FindProjectConfig(%q) = %q, want %q", childDir, got, sdDir)
		}
	})

	t.Run("returns empty when no config found", func(t *testing.T) {
		emptyDir := filepath.Join(tmp, "empty")
		if err := os.MkdirAll(emptyDir, 0700); err != nil {
			t.Fatal(err)
		}
		got := FindProjectConfig(emptyDir)
		if got != "" {
			t.Errorf("FindProjectConfig(%q) = %q, want empty", emptyDir, got)
		}
	})

	t.Run("uses nearest match", func(t *testing.T) {
		outer := filepath.Join(tmp, "proj3")
		inner := filepath.Join(outer, "inner")
		outerSD := filepath.Join(outer, ".sd")
		innerSD := filepath.Join(inner, ".sd")
		writeFile(t, filepath.Join(outerSD, "config.yaml"), "defaults:\n  cpus: 2\n")
		writeFile(t, filepath.Join(innerSD, "config.yaml"), "defaults:\n  cpus: 4\n")

		got := FindProjectConfig(inner)
		if got != innerSD {
			t.Errorf("FindProjectConfig(%q) = %q, want %q", inner, got, innerSD)
		}
	})
}

func TestLoadDefaults(t *testing.T) {
	tmp := t.TempDir()
	// No config files — should use all defaults.
	ldr := NewLoaderWithHome(filepath.Join(tmp, "nohome"))
	ldr.SetSearchDir(filepath.Join(tmp, "noproj"))
	if err := ldr.Load(); err != nil {
		t.Fatal(err)
	}

	cfg := ldr.Get()
	if cfg.Defaults.Backend != DefaultBackend {
		t.Errorf("Backend = %q, want %q", cfg.Defaults.Backend, DefaultBackend)
	}
	if cfg.Defaults.CPUs != DefaultCPUs {
		t.Errorf("CPUs = %d, want %d", cfg.Defaults.CPUs, DefaultCPUs)
	}
	if cfg.Defaults.Memory != DefaultMemory {
		t.Errorf("Memory = %q, want %q", cfg.Defaults.Memory, DefaultMemory)
	}
	if cfg.Defaults.Disk != DefaultDisk {
		t.Errorf("Disk = %q, want %q", cfg.Defaults.Disk, DefaultDisk)
	}
	if cfg.Defaults.Image != DefaultImage {
		t.Errorf("Image = %q, want %q", cfg.Defaults.Image, DefaultImage)
	}
	if cfg.Security.MountPolicy != DefaultMountPolicy {
		t.Errorf("MountPolicy = %q, want %q", cfg.Security.MountPolicy, DefaultMountPolicy)
	}
	if len(cfg.Security.EgressAllowlist) != len(DefaultEgressAllowlist) {
		t.Errorf("EgressAllowlist has %d entries, want %d",
			len(cfg.Security.EgressAllowlist), len(DefaultEgressAllowlist))
	}
}

func TestLoadUserConfig(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	writeFile(t, filepath.Join(home, "config.yaml"), `
defaults:
  cpus: 16
  memory: 32GiB
  backend: docker
`)

	ldr := NewLoaderWithHome(home)
	ldr.SetSearchDir(filepath.Join(tmp, "noproj"))
	if err := ldr.Load(); err != nil {
		t.Fatal(err)
	}

	cfg := ldr.Get()
	if cfg.Defaults.CPUs != 16 {
		t.Errorf("CPUs = %d, want 16", cfg.Defaults.CPUs)
	}
	if cfg.Defaults.Memory != "32GiB" {
		t.Errorf("Memory = %q, want %q", cfg.Defaults.Memory, "32GiB")
	}
	if cfg.Defaults.Backend != "docker" {
		t.Errorf("Backend = %q, want %q", cfg.Defaults.Backend, "docker")
	}
	// Unset keys should still use defaults.
	if cfg.Defaults.Disk != DefaultDisk {
		t.Errorf("Disk = %q, want %q", cfg.Defaults.Disk, DefaultDisk)
	}

	// Source attribution.
	if src := ldr.Source("defaults.cpus"); src != ConfigSourceUserConfig {
		t.Errorf("Source(defaults.cpus) = %q, want %q", src, ConfigSourceUserConfig)
	}
	if src := ldr.Source("defaults.disk"); src != ConfigSourceBuiltinDefault {
		t.Errorf("Source(defaults.disk) = %q, want %q", src, ConfigSourceBuiltinDefault)
	}
}

func TestLoadProjectConfig(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	projRoot := filepath.Join(tmp, "proj")
	writeFile(t, filepath.Join(home, "config.yaml"), `
defaults:
  cpus: 8
  backend: lima
`)
	writeFile(t, filepath.Join(projRoot, ".sd", "config.yaml"), `
defaults:
  cpus: 2
`)

	ldr := NewLoaderWithHome(home)
	ldr.SetSearchDir(projRoot)
	if err := ldr.Load(); err != nil {
		t.Fatal(err)
	}

	cfg := ldr.Get()
	// Project overrides user.
	if cfg.Defaults.CPUs != 2 {
		t.Errorf("CPUs = %d, want 2 (project overrides user)", cfg.Defaults.CPUs)
	}
	// User value preserved for keys not in project.
	if cfg.Defaults.Backend != "lima" {
		t.Errorf("Backend = %q, want %q (from user config)", cfg.Defaults.Backend, "lima")
	}
	if src := ldr.Source("defaults.cpus"); src != ConfigSourceProjectConfig {
		t.Errorf("Source(defaults.cpus) = %q, want %q", src, ConfigSourceProjectConfig)
	}
}

func TestPrecedenceOrder(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	projRoot := filepath.Join(tmp, "proj")

	// User config: backend=lima
	writeFile(t, filepath.Join(home, "config.yaml"), "defaults:\n  backend: lima\n")
	// Project config: backend=docker
	writeFile(t, filepath.Join(projRoot, ".sd", "config.yaml"), "defaults:\n  backend: docker\n")

	// Env var: backend=podman (via alias SD_BACKEND)
	t.Setenv("SD_BACKEND", "podman")

	ldr := NewLoaderWithHome(home)
	ldr.SetSearchDir(projRoot)
	if err := ldr.Load(); err != nil {
		t.Fatal(err)
	}

	cfg := ldr.Get()
	// Env var should win over project and user.
	if cfg.Defaults.Backend != "podman" {
		t.Errorf("Backend = %q, want %q (env overrides project and user)", cfg.Defaults.Backend, "podman")
	}
	if src := ldr.Source("defaults.backend"); src != ConfigSourceEnvVar {
		t.Errorf("Source(defaults.backend) = %q, want %q", src, ConfigSourceEnvVar)
	}

	// CLI flag should win over everything.
	ldr.MarkCLIKey("defaults.backend")
	if src := ldr.Source("defaults.backend"); src != ConfigSourceCLIFlag {
		t.Errorf("Source(defaults.backend) after CLI = %q, want %q", src, ConfigSourceCLIFlag)
	}
}

func TestEnvVarOverride(t *testing.T) {
	tmp := t.TempDir()

	t.Run("SD_BACKEND overrides defaults.backend", func(t *testing.T) {
		t.Setenv("SD_BACKEND", "docker")
		ldr := NewLoaderWithHome(filepath.Join(tmp, "h1"))
		ldr.SetSearchDir(filepath.Join(tmp, "p1"))
		if err := ldr.Load(); err != nil {
			t.Fatal(err)
		}
		cfg := ldr.Get()
		if cfg.Defaults.Backend != "docker" {
			t.Errorf("Backend = %q, want %q", cfg.Defaults.Backend, "docker")
		}
	})

	t.Run("SD_DEFAULT_VM overrides defaults.vm", func(t *testing.T) {
		t.Setenv("SD_DEFAULT_VM", "myvm")
		ldr := NewLoaderWithHome(filepath.Join(tmp, "h2"))
		ldr.SetSearchDir(filepath.Join(tmp, "p2"))
		if err := ldr.Load(); err != nil {
			t.Fatal(err)
		}
		cfg := ldr.Get()
		if cfg.Defaults.VM != "myvm" {
			t.Errorf("VM = %q, want %q", cfg.Defaults.VM, "myvm")
		}
	})

	t.Run("SD_DEFAULTS_CPUS overrides defaults.cpus", func(t *testing.T) {
		t.Setenv("SD_DEFAULTS_CPUS", "32")
		ldr := NewLoaderWithHome(filepath.Join(tmp, "h3"))
		ldr.SetSearchDir(filepath.Join(tmp, "p3"))
		if err := ldr.Load(); err != nil {
			t.Fatal(err)
		}
		cfg := ldr.Get()
		if cfg.Defaults.CPUs != 32 {
			t.Errorf("CPUs = %d, want 32", cfg.Defaults.CPUs)
		}
	})
}

func TestSecurityKeyFiltering(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	projRoot := filepath.Join(tmp, "proj")

	// User config with security settings.
	writeFile(t, filepath.Join(home, "config.yaml"), `
security:
  mount_policy: readonly
`)
	// Project config tries to set security.
	writeFile(t, filepath.Join(projRoot, ".sd", "config.yaml"), `
security:
  mount_policy: project
defaults:
  cpus: 2
`)

	ldr := NewLoaderWithHome(home)
	ldr.SetSearchDir(projRoot)
	if err := ldr.Load(); err != nil {
		t.Fatal(err)
	}

	cfg := ldr.Get()
	// security.mount_policy from project should be ignored; user value wins. REQ-005-017
	if cfg.Security.MountPolicy != "readonly" {
		t.Errorf("MountPolicy = %q, want %q (project security ignored)", cfg.Security.MountPolicy, "readonly")
	}
	// Non-security keys from project config should still apply.
	if cfg.Defaults.CPUs != 2 {
		t.Errorf("CPUs = %d, want 2 (non-security project key)", cfg.Defaults.CPUs)
	}
	// Source for security key should be user, not project.
	if src := ldr.Source("security.mount_policy"); src != ConfigSourceUserConfig {
		t.Errorf("Source(security.mount_policy) = %q, want %q", src, ConfigSourceUserConfig)
	}
}

func TestGetForVM(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")

	// User config with VM definition and defaults.
	writeFile(t, filepath.Join(home, "config.yaml"), `
defaults:
  backend: lima
  cpus: 4
  memory: 8GiB
  disk: 100GiB
  image: ubuntu:24.04
vms:
  testvm:
    cpus: 8
    memory: 16GiB
    provisions:
      - docker
    env:
      API_KEY: "${API_KEY}"
`)

	// VM-specific config file overrides cpus further.
	writeFile(t, filepath.Join(home, "vms", "testvm", "config.yaml"), `
name: testvm
backend: lima
cpus: 16
state:
  status: running
  created_at: "2026-03-27T10:00:00Z"
`)

	ldr := NewLoaderWithHome(home)
	ldr.SetSearchDir(filepath.Join(tmp, "noproj"))
	if err := ldr.Load(); err != nil {
		t.Fatal(err)
	}

	vc, err := ldr.GetForVM("testvm")
	if err != nil {
		t.Fatal(err)
	}

	// VM file overrides VM def and defaults. REQ-005-015
	if vc.CPUs != 16 {
		t.Errorf("CPUs = %d, want 16 (from VM file)", vc.CPUs)
	}
	// VM def overrides defaults.
	if vc.Memory != "16GiB" {
		t.Errorf("Memory = %q, want %q (from VM def)", vc.Memory, "16GiB")
	}
	// Defaults used for unset fields.
	if vc.Disk != "100GiB" {
		t.Errorf("Disk = %q, want %q (from defaults)", vc.Disk, "100GiB")
	}
	if vc.Image != "ubuntu:24.04" {
		t.Errorf("Image = %q, want %q (from defaults)", vc.Image, "ubuntu:24.04")
	}
	// State from VM file.
	if vc.State.Status != "running" {
		t.Errorf("State.Status = %q, want %q", vc.State.Status, "running")
	}

	// VM that only exists in config (no file).
	vc2, err := ldr.GetForVM("unknown")
	if err != nil {
		t.Fatal(err)
	}
	if vc2.CPUs != DefaultCPUs {
		t.Errorf("unknown VM CPUs = %d, want %d (defaults)", vc2.CPUs, DefaultCPUs)
	}
}

func TestMissingConfigNotError(t *testing.T) {
	tmp := t.TempDir()
	ldr := NewLoaderWithHome(filepath.Join(tmp, "nohome"))
	ldr.SetSearchDir(filepath.Join(tmp, "noproj"))

	if err := ldr.Load(); err != nil {
		t.Errorf("Load() with no config files returned error: %v", err)
	}
}

func TestInvalidYAMLFatal(t *testing.T) {
	tmp := t.TempDir()

	t.Run("invalid user config", func(t *testing.T) {
		home := filepath.Join(tmp, "badhome")
		writeFile(t, filepath.Join(home, "config.yaml"), "not: valid: yaml: [[[")

		ldr := NewLoaderWithHome(home)
		ldr.SetSearchDir(filepath.Join(tmp, "noproj"))
		if err := ldr.Load(); err == nil {
			t.Error("Load() should fail on invalid YAML, got nil")
		}
	})

	t.Run("invalid project config", func(t *testing.T) {
		home := filepath.Join(tmp, "goodhome")
		projRoot := filepath.Join(tmp, "badproj")
		writeFile(t, filepath.Join(projRoot, ".sd", "config.yaml"), "not: valid: yaml: [[[")

		ldr := NewLoaderWithHome(home)
		ldr.SetSearchDir(projRoot)
		if err := ldr.Load(); err == nil {
			t.Error("Load() should fail on invalid project YAML, got nil")
		}
	})
}

func TestSet(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")

	ldr := NewLoaderWithHome(home)
	ldr.SetSearchDir(filepath.Join(tmp, "noproj"))
	if err := ldr.Load(); err != nil {
		t.Fatal(err)
	}

	t.Run("creates user config file", func(t *testing.T) {
		if err := ldr.Set("defaults.cpus", 8, "user"); err != nil {
			t.Fatal(err)
		}
		// Verify the file was created.
		data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if len(data) == 0 {
			t.Error("config file is empty")
		}
	})

	t.Run("updates existing key", func(t *testing.T) {
		if err := ldr.Set("defaults.cpus", 16, "user"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("rolls back invalid config", func(t *testing.T) {
		err := ldr.Set("security.mount_policy", "invalid_value", "user")
		if err == nil {
			t.Error("Set() should fail for invalid mount_policy value")
		}
	})

	t.Run("invalid target", func(t *testing.T) {
		err := ldr.Set("defaults.cpus", 8, "invalid")
		if err == nil {
			t.Error("Set() should fail for invalid target")
		}
	})
}

func TestList(t *testing.T) {
	tmp := t.TempDir()
	ldr := NewLoaderWithHome(filepath.Join(tmp, "nohome"))
	ldr.SetSearchDir(filepath.Join(tmp, "noproj"))
	if err := ldr.Load(); err != nil {
		t.Fatal(err)
	}

	entries := ldr.List()
	if len(entries) != len(knownKeys) {
		t.Fatalf("List() returned %d entries, want %d", len(entries), len(knownKeys))
	}

	// All entries should have source "built-in default" when no config files exist.
	for _, e := range entries {
		if e.Source != ConfigSourceBuiltinDefault {
			t.Errorf("List() entry %q source = %q, want %q", e.Key, e.Source, ConfigSourceBuiltinDefault)
		}
		if e.Value == nil {
			t.Errorf("List() entry %q has nil value", e.Key)
		}
	}
}

func TestValidate(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	projRoot := filepath.Join(tmp, "proj")

	writeFile(t, filepath.Join(home, "config.yaml"), `
defaults:
  backend: lima
  cpus: 4
`)
	writeFile(t, filepath.Join(projRoot, ".sd", "config.yaml"), `
defaults:
  cpus: 2
security:
  mount_policy: readonly
`)
	writeFile(t, filepath.Join(home, "vms", "vm1", "config.yaml"), `
name: vm1
backend: lima
cpus: 4
`)

	ldr := NewLoaderWithHome(home)
	ldr.SetSearchDir(projRoot)
	if err := ldr.Load(); err != nil {
		t.Fatal(err)
	}

	results, err := ldr.Validate()
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 3 {
		t.Fatalf("Validate() returned %d results, want 3", len(results))
	}

	// All files should be valid YAML.
	for _, r := range results {
		if !r.Valid {
			t.Errorf("file %q invalid: %v", r.Path, r.Errors)
		}
	}

	// Project config should have a warning about security keys.
	projResult := results[1]
	foundWarning := false
	for _, e := range projResult.Errors {
		if e.Key == "security" {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Error("Validate() should warn about security keys in project config")
	}
}

func TestValidateInvalidFile(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")

	writeFile(t, filepath.Join(home, "config.yaml"), `
security:
  mount_policy: badvalue
`)

	ldr := NewLoaderWithHome(home)
	ldr.SetSearchDir(filepath.Join(tmp, "noproj"))
	if err := ldr.Load(); err != nil {
		t.Fatal(err)
	}

	results, _ := ldr.Validate()
	if len(results) == 0 {
		t.Fatal("expected validation results")
	}

	if results[0].Valid {
		t.Error("config with invalid mount_policy should be invalid")
	}
}

func TestSourceForUnknownKey(t *testing.T) {
	tmp := t.TempDir()
	ldr := NewLoaderWithHome(filepath.Join(tmp, "nohome"))
	ldr.SetSearchDir(filepath.Join(tmp, "noproj"))
	if err := ldr.Load(); err != nil {
		t.Fatal(err)
	}

	if src := ldr.Source("nonexistent.key"); src != "" {
		t.Errorf("Source(nonexistent.key) = %q, want empty", src)
	}
}

func TestVMsFromConfig(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")

	writeFile(t, filepath.Join(home, "config.yaml"), `
vms:
  dev:
    cpus: 8
    memory: 16GiB
    backend: docker
    provisions:
      - golang
      - node
    env:
      TOKEN: "${MY_TOKEN}"
`)

	ldr := NewLoaderWithHome(home)
	ldr.SetSearchDir(filepath.Join(tmp, "noproj"))
	if err := ldr.Load(); err != nil {
		t.Fatal(err)
	}

	cfg := ldr.Get()
	vm, ok := cfg.VMs["dev"]
	if !ok {
		t.Fatal("expected VM 'dev' in config")
	}
	if vm.CPUs != 8 {
		t.Errorf("VM dev CPUs = %d, want 8", vm.CPUs)
	}
	if vm.Memory != "16GiB" {
		t.Errorf("VM dev Memory = %q, want %q", vm.Memory, "16GiB")
	}
	if vm.Backend != "docker" {
		t.Errorf("VM dev Backend = %q, want %q", vm.Backend, "docker")
	}
	if len(vm.Provisions) != 2 {
		t.Errorf("VM dev Provisions = %v, want 2 entries", vm.Provisions)
	}
}
