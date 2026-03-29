package config

import "testing"

func TestNewDefaults(t *testing.T) {
	d := NewDefaults()

	// REQ-005-004: verify all built-in defaults.
	if d.Backend != "lima" {
		t.Errorf("Backend = %q, want %q", d.Backend, "lima")
	}
	if d.CPUs != 4 {
		t.Errorf("CPUs = %d, want %d", d.CPUs, 4)
	}
	if d.Memory != "8GiB" {
		t.Errorf("Memory = %q, want %q", d.Memory, "8GiB")
	}
	if d.Disk != "100GiB" {
		t.Errorf("Disk = %q, want %q", d.Disk, "100GiB")
	}
	if d.Image != "ubuntu:24.04" {
		t.Errorf("Image = %q, want %q", d.Image, "ubuntu:24.04")
	}
	if d.VM != "" {
		t.Errorf("VM = %q, want %q", d.VM, "")
	}
}

func TestNewSecurity(t *testing.T) {
	s := NewSecurity()

	if s.MountPolicy != MountPolicyNone {
		t.Errorf("MountPolicy = %q, want %q", s.MountPolicy, MountPolicyNone)
	}

	// REQ-004-007: exactly 11 entries.
	if len(s.EgressAllowlist) != 11 {
		t.Fatalf("EgressAllowlist has %d entries, want 11", len(s.EgressAllowlist))
	}

	expectedEntries := []string{
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

	for i, want := range expectedEntries {
		if s.EgressAllowlist[i] != want {
			t.Errorf("EgressAllowlist[%d] = %q, want %q", i, s.EgressAllowlist[i], want)
		}
	}
}

func TestNewSecurityCopiesSlice(t *testing.T) {
	s := NewSecurity()
	s.EgressAllowlist[0] = "mutated"

	// NewSecurity should return a copy, not a reference to the package-level slice.
	if DefaultEgressAllowlist[0] == "mutated" {
		t.Error("NewSecurity returned a reference to DefaultEgressAllowlist instead of a copy")
	}
}

func TestNewConfig(t *testing.T) {
	c := NewConfig()

	if c.Defaults.Backend != DefaultBackend {
		t.Errorf("Defaults.Backend = %q, want %q", c.Defaults.Backend, DefaultBackend)
	}
	if c.Security.MountPolicy != DefaultMountPolicy {
		t.Errorf("Security.MountPolicy = %q, want %q", c.Security.MountPolicy, DefaultMountPolicy)
	}
	if c.VMs == nil {
		t.Error("VMs map is nil, want initialized empty map")
	}
	if len(c.VMs) != 0 {
		t.Errorf("VMs has %d entries, want 0", len(c.VMs))
	}
}
