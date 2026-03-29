package config

// DefaultEgressAllowlist is the default set of allowed egress domains.
// REQ-005-004: Must match spec 004 REQ-004-007
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

// DefaultConfig returns the built-in default configuration.
// REQ-005-004
func DefaultConfig() *Config {
	return &Config{
		Defaults: Defaults{
			Backend: "lima",
			CPUs:    4,
			Memory:  "8GiB",
			Disk:    "100GiB",
			Image:   "ubuntu:24.04",
			VM:      "",
		},
		Security: Security{
			EgressAllowlist: DefaultEgressAllowlist,
			MountPolicy:     MountPolicyNone,
		},
		VMs: make(map[string]VMDef),
	}
}
