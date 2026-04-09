// Package config provides project-level .sd.yaml configuration support.
// REQ-005-020: .sd.yaml in project root defines VM config.
// REQ-005-021: sd create and sd ensure with no name read .sd.yaml.
// REQ-005-023: .sd.yaml is validated on load; invalid files produce actionable errors.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProjectConfig represents the .sd.yaml project configuration file.
// REQ-005-020: Defines VM name, backend, resources, modules, mounts, egress.
type ProjectConfig struct {
	Name        string   `yaml:"name,omitempty"`
	Backend     string   `yaml:"backend,omitempty"`
	CPUs        int      `yaml:"cpus,omitempty"`
	Memory      string   `yaml:"memory,omitempty"`
	Disk        string   `yaml:"disk,omitempty"`
	Modules     []string `yaml:"modules,omitempty"`
	Mounts      []string `yaml:"mounts,omitempty"`       // "host:guest:mode"
	AllowEgress []string `yaml:"allow_egress,omitempty"`
}

// ProjectConfigFile is the filename for project configuration.
const ProjectConfigFile = ".sd.yaml"

// vmNamePattern matches valid VM names: lowercase letter, then lowercase letters/digits/hyphens, 1-63 chars.
var projectVMNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// resourceSizePatternProject matches valid resource sizes like "4GiB", "512MiB", "100GB".
var resourceSizePatternProject = regexp.MustCompile(`^[1-9][0-9]*\s*([KMGT]i?B)$`)

// FindProjectConfig searches for .sd.yaml starting from dir and walking up
// to the filesystem root or home directory. Returns the path and parsed config,
// or ("", nil, nil) if not found.
// REQ-005-021
func FindProjectConfig(dir string) (string, *ProjectConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "" // best-effort: skip home dir boundary check
	}

	current, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, fmt.Errorf("resolve absolute path for %q: %w", dir, err)
	}

	for {
		candidate := filepath.Join(current, ProjectConfigFile)
		if _, err := os.Stat(candidate); err == nil {
			cfg, loadErr := LoadProjectConfig(candidate)
			if loadErr != nil {
				return candidate, nil, loadErr
			}
			// Auto-derive name from directory if not specified
			if cfg.Name == "" {
				cfg.Name = DeriveVMName(current)
			}
			return candidate, cfg, nil
		}

		// Stop at home directory -- don't walk into parent of $HOME
		if home != "" && current == home {
			break
		}

		parent := filepath.Dir(current)
		if parent == current {
			// Reached filesystem root
			break
		}
		current = parent
	}

	return "", nil, nil
}

// LoadProjectConfig parses and validates a .sd.yaml file.
// REQ-005-023: Validated on load with actionable errors.
func LoadProjectConfig(path string) (*ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if err := validateProjectConfig(&cfg, path); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// validateProjectConfig checks the project config for correctness.
// REQ-005-023
func validateProjectConfig(cfg *ProjectConfig, path string) error {
	// Validate name if specified (empty is OK -- will be derived from directory)
	if cfg.Name != "" && !projectVMNamePattern.MatchString(cfg.Name) {
		return fmt.Errorf("%s: invalid name %q: must match ^[a-z][a-z0-9-]{0,62}$ (lowercase letter start, then lowercase letters/digits/hyphens, max 63 chars)", path, cfg.Name)
	}

	// Validate resource values if specified
	if cfg.CPUs < 0 {
		return fmt.Errorf("%s: cpus must be positive, got %d", path, cfg.CPUs)
	}
	if cfg.Memory != "" && !resourceSizePatternProject.MatchString(cfg.Memory) {
		return fmt.Errorf("%s: invalid memory %q: must be a positive integer followed by a unit (e.g. \"4GiB\", \"8GiB\")", path, cfg.Memory)
	}
	if cfg.Disk != "" && !resourceSizePatternProject.MatchString(cfg.Disk) {
		return fmt.Errorf("%s: invalid disk %q: must be a positive integer followed by a unit (e.g. \"50GiB\", \"100GiB\")", path, cfg.Disk)
	}

	// Validate mount format: must have at least host:guest
	for i, m := range cfg.Mounts {
		parts := strings.SplitN(m, ":", 3)
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("%s: mounts[%d] %q: must be host:guest[:ro|rw]", path, i, m)
		}
		if len(parts) == 3 && parts[2] != "ro" && parts[2] != "rw" {
			return fmt.Errorf("%s: mounts[%d] %q: mode must be \"ro\" or \"rw\", got %q", path, i, m, parts[2])
		}
	}

	return nil
}

// DeriveVMName generates a valid VM name from a directory name.
// It lowercases, replaces invalid characters with hyphens, collapses
// consecutive hyphens, trims leading/trailing hyphens, and ensures
// the name starts with a letter.
func DeriveVMName(dir string) string {
	base := filepath.Base(dir)
	name := strings.ToLower(base)

	// Replace any character that is not a lowercase letter, digit, or hyphen
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	name = b.String()

	// Collapse consecutive hyphens
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}

	// Trim leading and trailing hyphens
	name = strings.Trim(name, "-")

	// Ensure starts with a letter
	if len(name) == 0 || name[0] < 'a' || name[0] > 'z' {
		name = "vm-" + name
	}

	// Truncate to 63 characters
	if len(name) > 63 {
		name = name[:63]
	}

	// Trim trailing hyphens after truncation
	name = strings.TrimRight(name, "-")

	// Final safety: if empty after all processing, use a fallback
	if name == "" {
		name = "vm"
	}

	return name
}

// ResolveMountPaths resolves "." in mount host paths relative to the directory
// containing the .sd.yaml file. Returns the resolved mount specs.
func ResolveMountPaths(mounts []string, projectDir string) []string {
	resolved := make([]string, len(mounts))
	for i, m := range mounts {
		parts := strings.SplitN(m, ":", 3)
		if len(parts) >= 1 && parts[0] == "." {
			parts[0] = projectDir
		} else if len(parts) >= 1 && !filepath.IsAbs(parts[0]) {
			parts[0] = filepath.Join(projectDir, parts[0])
		}
		resolved[i] = strings.Join(parts, ":")
	}
	return resolved
}
