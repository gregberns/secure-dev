package config

// Built-in default values compiled into the binary. REQ-005-004
// These are the lowest-precedence source in the config resolution chain.

// DefaultBackend is the default VM backend.
const DefaultBackend = "lima"

// DefaultCPUs is the default number of CPUs for a new VM.
const DefaultCPUs = 4

// DefaultMemory is the default memory allocation for a new VM.
const DefaultMemory = "8GiB"

// DefaultDisk is the default disk size for a new VM.
const DefaultDisk = "100GiB"

// DefaultImage is the default base image for a new VM.
const DefaultImage = "ubuntu:24.04"

// DefaultVM is the default VM name (empty means no default).
const DefaultVM = ""

// DefaultMountPolicy is the default mount policy (most secure). REQ-005-018
const DefaultMountPolicy = MountPolicyNone

// DefaultEgressAllowlist contains the default allowed egress destinations.
// Matches REQ-004-007 exactly.
var DefaultEgressAllowlist = []string{
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

// NewDefaults returns a Defaults struct populated with built-in default values.
func NewDefaults() Defaults {
	return Defaults{
		Backend: DefaultBackend,
		CPUs:    DefaultCPUs,
		Memory:  DefaultMemory,
		Disk:    DefaultDisk,
		Image:   DefaultImage,
		VM:      DefaultVM,
	}
}

// NewSecurity returns a Security struct populated with built-in default values.
func NewSecurity() Security {
	allowlist := make([]string, len(DefaultEgressAllowlist))
	copy(allowlist, DefaultEgressAllowlist)
	return Security{
		EgressAllowlist: allowlist,
		MountPolicy:     DefaultMountPolicy,
	}
}

// NewConfig returns a Config struct populated with all built-in default values.
func NewConfig() Config {
	return Config{
		Defaults: NewDefaults(),
		Security: NewSecurity(),
		VMs:      make(map[string]VMDef),
	}
}
