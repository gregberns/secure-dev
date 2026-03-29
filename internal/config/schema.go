package config

import "time"

// Config is the fully resolved configuration from all sources. REQ-005-006
type Config struct {
	Defaults Defaults         `yaml:"defaults"  mapstructure:"defaults"`
	Security Security         `yaml:"security"  mapstructure:"security"`
	VMs      map[string]VMDef `yaml:"vms"       mapstructure:"vms"`
}

// Defaults holds default values for VM creation. REQ-005-004
type Defaults struct {
	Backend string `yaml:"backend" mapstructure:"backend"` // default: "lima"
	CPUs    int    `yaml:"cpus"    mapstructure:"cpus"`    // default: 4
	Memory  string `yaml:"memory"  mapstructure:"memory"`  // default: "8GiB"
	Disk    string `yaml:"disk"    mapstructure:"disk"`    // default: "100GiB"
	Image   string `yaml:"image"   mapstructure:"image"`   // default: "ubuntu:24.04"
	VM      string `yaml:"vm"      mapstructure:"vm"`      // default: ""
}

// Security holds security policy settings. REQ-004-006, REQ-004-007, REQ-005-004
type Security struct {
	EgressAllowlist []string `yaml:"egress_allowlist" mapstructure:"egress_allowlist"`
	MountPolicy     string   `yaml:"mount_policy"     mapstructure:"mount_policy"`
	SensitivePaths  []string `yaml:"sensitive_paths,omitempty" mapstructure:"sensitive_paths"`
}

// MountPolicy valid values. REQ-005-018
const (
	MountPolicyNone     = "none"     // no mounts (default)
	MountPolicyReadonly = "readonly" // mount CWD as read-only
	MountPolicyProject  = "project"  // mount project root as read-write
)

// VMDef is a VM definition as written in user or project config file.
// Not the same as the persisted per-VM state file (VMConfig). REQ-005-006
type VMDef struct {
	CPUs       int               `yaml:"cpus"       mapstructure:"cpus"`
	Memory     string            `yaml:"memory"     mapstructure:"memory"`
	Disk       string            `yaml:"disk"       mapstructure:"disk"`
	Image      string            `yaml:"image"      mapstructure:"image"`
	Backend    string            `yaml:"backend"    mapstructure:"backend"`
	Provisions []string          `yaml:"provisions" mapstructure:"provisions"`
	Env        map[string]string `yaml:"env"        mapstructure:"env"` // ${VAR} references only
}

// VMConfig is the full persisted state for a specific VM instance.
// Stored at $SD_HOME/vms/<name>/config.yaml. REQ-005-007
type VMConfig struct {
	Name        string            `yaml:"name"`
	Backend     string            `yaml:"backend"`
	CPUs        int               `yaml:"cpus"`
	Memory      string            `yaml:"memory"`
	Disk        string            `yaml:"disk"`
	Image       string            `yaml:"image"`
	Provisions  []string          `yaml:"provisions"`
	Env         map[string]string `yaml:"env"`          // ${VAR} references only
	State       VMState           `yaml:"state"`
	BackendMeta map[string]any    `yaml:"backend_meta"` // opaque backend-specific data
}

// VMState is the runtime state tracked inside the per-VM config file. REQ-005-007
type VMState struct {
	Status      string    `yaml:"status"`
	CreatedAt   time.Time `yaml:"created_at"`
	LastStarted time.Time `yaml:"last_started,omitempty"`
	LastStopped time.Time `yaml:"last_stopped,omitempty"`
}

// VMStatus constants for VMState.Status. REQ-005-007
const (
	VMStatusCreated = "created"
	VMStatusRunning = "running"
	VMStatusStopped = "stopped"
	VMStatusError   = "error"
)

// ValidationResult holds the result of validating one config file. REQ-005-013
type ValidationResult struct {
	Path   string        `json:"path"`
	Valid  bool          `json:"valid"`
	Errors []ConfigError `json:"errors"`
}

// ConfigError describes a single validation error within a config file.
type ConfigError struct {
	Line    int    `json:"line,omitempty"`
	Key     string `json:"key,omitempty"`
	Message string `json:"message"`
}

// ConfigEntry is a single resolved key-value pair with source attribution.
// Used by sd config list --json. REQ-005-011
type ConfigEntry struct {
	Key    string `json:"key"`
	Value  any    `json:"value"`
	Source string `json:"source"`
}

// ConfigSource constants for ConfigEntry.Source. REQ-005-009
const (
	ConfigSourceCLIFlag        = "cli flag"
	ConfigSourceEnvVar         = "environment variable"
	ConfigSourceProjectConfig  = "project-level config"
	ConfigSourceUserConfig     = "user-level config"
	ConfigSourceVMConfig       = "vm config"
	ConfigSourceBuiltinDefault = "built-in default"
)

// IsSensitiveKey reports whether a config key name matches a pattern indicating
// it holds a sensitive value (*_TOKEN, *_KEY, *_SECRET, *_PASSWORD).
// Used to mask values in sd config list output. REQ-005-008
func IsSensitiveKey(key string) bool {
	n := len(key)
	if n < 4 {
		return false
	}
	// Check common sensitive suffixes (case-insensitive would add complexity
	// for minimal gain — config keys are conventionally uppercase or snake_case).
	suffixes := [4]string{"_TOKEN", "_KEY", "_SECRET", "_PASSWORD"}
	for _, s := range suffixes {
		sn := len(s)
		if n >= sn && key[n-sn:] == s {
			return true
		}
	}
	return false
}
