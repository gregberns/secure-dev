// Package provision handles VM provisioning with composable modules.
// REQ-006-001: Built-in module set embedded in the binary.
// REQ-006-003: Module definition format with YAML schema.
package provision

import (
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"time"

	"sd/internal/provision/modules"
	"gopkg.in/yaml.v3"
)

// ModuleStatus represents the execution state of a module.
// REQ-006-009: Provisioning progress reporting.
type ModuleStatus string

const (
	StatusPending   ModuleStatus = "pending"
	StatusRunning   ModuleStatus = "running"
	StatusCompleted ModuleStatus = "completed"
	StatusFailed    ModuleStatus = "failed"
)

// Module represents a single provisioning module.
// REQ-006-003: Module definition format.
type Module struct {
	Name        string            `yaml:"name"         json:"name"`
	Description string            `yaml:"description"  json:"description"`
	DependsOn   []string          `yaml:"depends_on"   json:"depends_on,omitempty"`
	Scripts     []Script          `yaml:"scripts"      json:"scripts"`
	Checksums   map[string]string `yaml:"checksums,omitempty" json:"checksums,omitempty"`
	Probe       *Probe            `yaml:"probe,omitempty"   json:"probe,omitempty"`
}

// Script represents a single script block within a module.
// REQ-006-003: scripts list with mode and content.
type Script struct {
	Mode   ScriptMode `yaml:"mode"   json:"mode"`
	Script string     `yaml:"script" json:"script"`
}

// ScriptMode determines whether a script runs as root or the default user.
type ScriptMode string

const (
	ModeSystem ScriptMode = "system"
	ModeUser   ScriptMode = "user"
)

// Probe defines a readiness check for a module.
// REQ-006-008: Readiness probes.
type Probe struct {
	Command  string        `yaml:"command"  json:"command"`
	Interval time.Duration `yaml:"interval" json:"interval"`
	Timeout  time.Duration `yaml:"timeout"  json:"timeout"`
}

// DefaultProbeInterval is the default time between probe attempts.
const DefaultProbeInterval = 5 * time.Second

// DefaultProbeTimeout is the default max time to wait for probe success.
const DefaultProbeTimeout = 5 * time.Minute

// ModuleExecutionStatus tracks execution state for a single module.
// REQ-006-009: Per-module status tracking.
type ModuleExecutionStatus struct {
	Name      string       `json:"name"`
	Status    ModuleStatus `json:"status"`
	StartedAt *time.Time   `json:"started_at,omitempty"`
	EndedAt   *time.Time   `json:"ended_at,omitempty"`
	Error     string       `json:"error,omitempty"`
}

// ProvisionState tracks the overall provisioning progress for a VM.
// REQ-006-009: Provisioning progress reporting.
type ProvisionState struct {
	VMName   string                 `json:"vm_name"`
	Started  time.Time              `json:"started"`
	Finished *time.Time             `json:"finished,omitempty"`
	Modules  []ModuleExecutionStatus `json:"modules"`
}

// Validate checks a module for schema correctness.
// REQ-006-003: Validation of module definition format.
func (m *Module) Validate() error {
	var errs []string

	// Name is required and must be kebab-case.
	if m.Name == "" {
		errs = append(errs, "name is required")
	} else if !isValidModuleName(m.Name) {
		errs = append(errs, fmt.Sprintf("name %q must be lowercase kebab-case (a-z, 0-9, hyphens)", m.Name))
	}

	// Description is required.
	if m.Description == "" {
		errs = append(errs, "description is required")
	}

	// Scripts list is required and must not be empty.
	if len(m.Scripts) == 0 {
		errs = append(errs, "at least one script is required")
	}

	// Validate each script.
	for i, s := range m.Scripts {
		switch s.Mode {
		case ModeSystem, ModeUser:
			// valid
		default:
			errs = append(errs, fmt.Sprintf("scripts[%d].mode must be \"system\" or \"user\", got %q", i, s.Mode))
		}
		if strings.TrimSpace(s.Script) == "" {
			errs = append(errs, fmt.Sprintf("scripts[%d].script must not be empty", i))
		}
	}

	// Validate probe if present.
	if m.Probe != nil {
		if strings.TrimSpace(m.Probe.Command) == "" {
			errs = append(errs, "probe.command must not be empty")
		}
	}

	// Validate dependencies are valid module names.
	for _, dep := range m.DependsOn {
		if !isValidModuleName(dep) {
			errs = append(errs, fmt.Sprintf("depends_on entry %q is not a valid module name", dep))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("module %q validation failed: %s", m.Name, strings.Join(errs, "; "))
	}
	return nil
}

// ValidateForRegistration checks a module for name uniqueness and dependency validity
// against a set of known module names.
// REQ-006-004: Dependency validation.
// REQ-006-007: Custom module name conflict detection.
func (m *Module) ValidateForRegistration(knownNames map[string]bool) error {
	// Check for self-dependency.
	for _, dep := range m.DependsOn {
		if dep == m.Name {
			return fmt.Errorf("module %q depends on itself", m.Name)
		}
	}

	// Check that all dependencies reference known modules.
	// REQ-006-004: depends_on referencing a nonexistent module produces a validation error.
	for _, dep := range m.DependsOn {
		if !knownNames[dep] {
			return fmt.Errorf("module %q depends on unknown module %q", m.Name, dep)
		}
	}

	// Check for name conflicts with already-registered modules.
	// REQ-006-007: Custom module names MUST NOT conflict with built-in names.
	if knownNames[m.Name] {
		return fmt.Errorf("module %q is already registered", m.Name)
	}

	return nil
}

// ApplyProbeDefaults fills in default values for unset probe fields.
// REQ-006-008: Default probe interval is 5s, default timeout is 5m.
func (m *Module) ApplyProbeDefaults() {
	if m.Probe != nil {
		if m.Probe.Interval == 0 {
			m.Probe.Interval = DefaultProbeInterval
		}
		if m.Probe.Timeout == 0 {
			m.Probe.Timeout = DefaultProbeTimeout
		}
	}
}

// HasDownloads checks if any script in the module appears to download files.
func (m *Module) HasDownloads() bool {
	for _, s := range m.Scripts {
		if strings.Contains(s.Script, "curl") || strings.Contains(s.Script, "wget") {
			return true
		}
	}
	return false
}

// ValidateChecksums checks that modules which download binaries have checksums.
// REQ-006-016: Checksum verification for downloaded binaries.
func (m *Module) ValidateChecksums() error {
	if m.HasDownloads() && len(m.Checksums) == 0 {
		return fmt.Errorf("module %q downloads files but has no checksums declared", m.Name)
	}
	return nil
}

// ParseModule parses a Module from YAML bytes.
func ParseModule(data []byte) (*Module, error) {
	var m Module
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse module YAML: %w", err)
	}
	return &m, nil
}

// moduleNamePattern matches lowercase kebab-case identifiers.
var moduleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

func isValidModuleName(name string) bool {
	return moduleNamePattern.MatchString(name)
}

// BuiltinModuleNames is the list of built-in module names in the order they
// should appear in listings. REQ-006-001.
var BuiltinModuleNames = []string{
	"base",
	"ssh-hardening",
	"claude-code",
	"docker",
	"golang",
	"rust",
	"python",
	"github-cli",
}

// LoadBuiltinModules reads all embedded module YAML files, parses them,
// validates them, and returns them in canonical order.
// REQ-006-014: Built-in modules loaded from embedded FS.
// REQ-006-001: Built-in module set.
func LoadBuiltinModules() ([]Module, error) {
	entries, err := fs.ReadDir(modules.ModuleFS, ".")
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded modules: %w", err)
	}

	parsed := make(map[string]*Module)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		data, err := fs.ReadFile(modules.ModuleFS, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to read embedded module %q: %w", entry.Name(), err)
		}
		m, err := ParseModule(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse embedded module %q: %w", entry.Name(), err)
		}
		if err := m.Validate(); err != nil {
			return nil, fmt.Errorf("embedded module %q failed validation: %w", entry.Name(), err)
		}
		parsed[m.Name] = m
	}

	// Return in canonical order.
	result := make([]Module, 0, len(BuiltinModuleNames))
	for _, name := range BuiltinModuleNames {
		m, ok := parsed[name]
		if !ok {
			return nil, fmt.Errorf("built-in module %q not found in embedded FS", name)
		}
		result = append(result, *m)
		delete(parsed, name)
	}

	// Append any extra modules not in the canonical list (sorted for determinism).
	extraNames := make([]string, 0, len(parsed))
	for name := range parsed {
		extraNames = append(extraNames, name)
	}
	sort.Strings(extraNames)
	for _, name := range extraNames {
		result = append(result, *parsed[name])
	}

	return result, nil
}

