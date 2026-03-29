// Package config defines configuration types and the loading system for sd.
// REQ-005-001: Configuration Precedence Order
// REQ-005-004: Built-In Defaults
// REQ-005-006: Config File Format
package config

import (
	"fmt"
	"strings"
	"time"
)

// Defaults holds default values for VM creation.
// REQ-005-004
type Defaults struct {
	Backend string `yaml:"backend" mapstructure:"backend"`
	CPUs    int    `yaml:"cpus" mapstructure:"cpus"`
	Memory  string `yaml:"memory" mapstructure:"memory"`
	Disk    string `yaml:"disk" mapstructure:"disk"`
	Image   string `yaml:"image" mapstructure:"image"`
	VM      string `yaml:"vm" mapstructure:"vm"`
}

// Security holds security policy settings.
// REQ-005-006
type Security struct {
	EgressAllowlist []string `yaml:"egress_allowlist" mapstructure:"egress_allowlist"`
	MountPolicy     string   `yaml:"mount_policy" mapstructure:"mount_policy"`
}

// MountPolicy valid values.
// REQ-005-018
const (
	MountPolicyNone     = "none"
	MountPolicyReadonly = "readonly"
	MountPolicyProject  = "project"
)

// ValidMountPolicies is the set of allowed mount_policy values.
var ValidMountPolicies = []string{MountPolicyNone, MountPolicyReadonly, MountPolicyProject}

// VMDef holds the definition of a VM as specified in config files.
// REQ-005-006
type VMDef struct {
	CPUs       int               `yaml:"cpus" mapstructure:"cpus"`
	Memory     string            `yaml:"memory" mapstructure:"memory"`
	Disk       string            `yaml:"disk" mapstructure:"disk"`
	Image      string            `yaml:"image" mapstructure:"image"`
	Backend    string            `yaml:"backend" mapstructure:"backend"`
	Provisions []string          `yaml:"provisions" mapstructure:"provisions"`
	Env        map[string]string `yaml:"env" mapstructure:"env"`
}

// VMConfig holds the full persisted config for a specific VM instance,
// including runtime state managed by sd.
// REQ-005-007
type VMConfig struct {
	Name        string         `yaml:"name"`
	Backend     string         `yaml:"backend"`
	CPUs        int            `yaml:"cpus"`
	Memory      string         `yaml:"memory"`
	Disk        string         `yaml:"disk"`
	Image       string         `yaml:"image"`
	Provisions  []string       `yaml:"provisions"`
	Env         map[string]string `yaml:"env"`
	State       VMState        `yaml:"state"`
	BackendMeta map[string]any `yaml:"backend_meta"`
}

// VMState tracks the runtime state of a VM instance.
// REQ-005-007
type VMState struct {
	Status      string    `yaml:"status"`
	CreatedAt   time.Time `yaml:"created_at"`
	LastStarted time.Time `yaml:"last_started,omitempty"`
	LastStopped time.Time `yaml:"last_stopped,omitempty"`
}

// VMStatus valid values.
// REQ-005-007
const (
	VMStatusCreated = "created"
	VMStatusRunning = "running"
	VMStatusStopped = "stopped"
	VMStatusError   = "error"
)

// ValidVMStatuses is the set of allowed VM status values.
var ValidVMStatuses = []string{VMStatusCreated, VMStatusRunning, VMStatusStopped, VMStatusError}

// Config represents the fully resolved configuration.
// REQ-005-006
type Config struct {
	Defaults Defaults          `yaml:"defaults" mapstructure:"defaults"`
	Security Security          `yaml:"security" mapstructure:"security"`
	VMs      map[string]VMDef  `yaml:"vms" mapstructure:"vms"`
}

// ValidationResult holds the result of validating a single config file.
// REQ-005-013
type ValidationResult struct {
	Path   string   `json:"path"`
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
}

// Source represents where a config value came from.
// REQ-005-001
type Source string

const (
	SourceCLI            Source = "cli flag"
	SourceEnv            Source = "environment variable"
	SourceProjectConfig  Source = "project-level config"
	SourceUserConfig     Source = "user-level config"
	SourceVMConfig       Source = "vm config"
	SourceDefault        Source = "built-in default"
)

// ValidateMountPolicy checks that a mount policy value is valid.
// REQ-005-018
func ValidateMountPolicy(policy string) error {
	for _, valid := range ValidMountPolicies {
		if policy == valid {
			return nil
		}
	}
	return fmt.Errorf("invalid mount_policy %q: must be one of %s",
		policy, strings.Join(ValidMountPolicies, ", "))
}

// ValidateVMStatus checks that a VM status value is valid.
// REQ-005-007
func ValidateVMStatus(status string) error {
	for _, valid := range ValidVMStatuses {
		if status == valid {
			return nil
		}
	}
	return fmt.Errorf("invalid vm status %q: must be one of %s",
		status, strings.Join(ValidVMStatuses, ", "))
}

// SecurityKeys is the set of config keys that are security-sensitive.
// REQ-005-017
var SecurityKeys = []string{
	"security.mount_policy",
	"security.egress_allowlist",
}

// IsSecurityKey returns true if the key is a security-sensitive key.
func IsSecurityKey(key string) bool {
	for _, sk := range SecurityKeys {
		if key == sk || strings.HasPrefix(key, sk+".") {
			return true
		}
	}
	return false
}
