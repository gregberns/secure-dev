package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// SDHome returns the sd data directory.
// Returns $SD_HOME if set, otherwise ~/.sd. REQ-005-002
func SDHome() string {
	if h := os.Getenv("SD_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".sd")
	}
	return filepath.Join(home, ".sd")
}

// FindProjectConfig walks up from dir looking for .sd/config.yaml.
// Returns the .sd directory path, or "" if not found. REQ-005-003
func FindProjectConfig(dir string) string {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(dir, ".sd", "config.yaml")
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return filepath.Join(dir, ".sd")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// knownKeys lists all config keys in display order. REQ-005-011
var knownKeys = []string{
	"defaults.backend",
	"defaults.cpus",
	"defaults.memory",
	"defaults.disk",
	"defaults.image",
	"defaults.vm",
	"security.mount_policy",
	"security.egress_allowlist",
}

// envAliases maps config keys to non-standard env var names. REQ-005-005
var envAliases = map[string]string{
	"defaults.backend": "SD_BACKEND",
	"defaults.vm":      "SD_DEFAULT_VM",
}

// Loader provides access to the resolved configuration.
// Uses instance-based Viper (not global). REQ-005-014
type Loader struct {
	v          *viper.Viper // merged config
	userV      *viper.Viper // user-level layer
	projectV   *viper.Viper // project-level layer
	sdHome     string
	projectDir string          // .sd dir containing project config
	searchDir  string          // override for project config search root
	cliKeys    map[string]bool // keys bound from CLI flags
	loaded     bool
}

// NewLoader creates a config Loader using the default SD home.
func NewLoader() *Loader {
	return &Loader{
		sdHome:  SDHome(),
		cliKeys: make(map[string]bool),
	}
}

// NewLoaderWithHome creates a Loader with a custom home directory.
func NewLoaderWithHome(sdHome string) *Loader {
	return &Loader{
		sdHome:  sdHome,
		cliKeys: make(map[string]bool),
	}
}

// SetSearchDir overrides the directory from which project config is discovered.
func (l *Loader) SetSearchDir(dir string) {
	l.searchDir = dir
}

// MarkCLIKey records that a key was set via a CLI flag.
func (l *Loader) MarkCLIKey(key string) {
	l.cliKeys[key] = true
}

func setViperDefaults(v *viper.Viper) {
	v.SetDefault("defaults.backend", DefaultBackend)
	v.SetDefault("defaults.cpus", DefaultCPUs)
	v.SetDefault("defaults.memory", DefaultMemory)
	v.SetDefault("defaults.disk", DefaultDisk)
	v.SetDefault("defaults.image", DefaultImage)
	v.SetDefault("defaults.vm", DefaultVM)
	v.SetDefault("security.mount_policy", DefaultMountPolicy)
	v.SetDefault("security.egress_allowlist", DefaultEgressAllowlist)
}

// Load reads and merges all configuration sources.
// Precedence (highest first): CLI > env > project > user > default.
// REQ-005-001, REQ-005-014
func (l *Loader) Load() error {
	l.v = viper.New()
	l.v.SetConfigType("yaml")

	// Built-in defaults (lowest precedence).
	setViperDefaults(l.v)

	// Environment variable mapping. REQ-005-005
	l.v.SetEnvPrefix("SD")
	l.v.AutomaticEnv()
	l.v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Explicit env var aliases. REQ-005-005
	for key, envVar := range envAliases {
		l.v.BindEnv(key, envVar)
	}

	// User-level config. REQ-005-002
	if err := l.loadUserConfig(); err != nil {
		return err
	}

	// Project-level config with security filtering. REQ-005-003, REQ-005-017
	if err := l.loadProjectConfig(); err != nil {
		return err
	}

	l.loaded = true
	return nil
}

func (l *Loader) loadUserConfig() error {
	cfgPath := filepath.Join(l.sdHome, "config.yaml")
	if _, err := os.Stat(cfgPath); err != nil {
		return nil // missing file is not an error
	}

	l.userV = viper.New()
	l.userV.SetConfigType("yaml")
	l.userV.SetConfigFile(cfgPath)

	if err := l.userV.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to parse %s: %w", cfgPath, err)
	}
	return l.v.MergeConfigMap(l.userV.AllSettings())
}

func (l *Loader) loadProjectConfig() error {
	startDir := l.searchDir
	if startDir == "" {
		var err error
		startDir, err = os.Getwd()
		if err != nil {
			return nil
		}
	}

	l.projectDir = FindProjectConfig(startDir)
	if l.projectDir == "" {
		return nil
	}

	cfgPath := filepath.Join(l.projectDir, "config.yaml")
	l.projectV = viper.New()
	l.projectV.SetConfigType("yaml")
	l.projectV.SetConfigFile(cfgPath)

	if err := l.projectV.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to parse %s: %w", cfgPath, err)
	}

	// Filter security.* keys from project config. REQ-005-017
	settings := l.projectV.AllSettings()
	if sec, hasSecurity := settings["security"]; hasSecurity {
		slog.Warn("security settings in project-level config are ignored",
			"found", fmt.Sprintf("%v", sec),
			"path", cfgPath)
		delete(settings, "security")
	}

	return l.v.MergeConfigMap(settings)
}

// Get returns the fully resolved Config. REQ-005-006
func (l *Loader) Get() *Config {
	c := NewConfig()
	if l.v == nil {
		return &c
	}

	c.Defaults.Backend = l.v.GetString("defaults.backend")
	c.Defaults.CPUs = l.v.GetInt("defaults.cpus")
	c.Defaults.Memory = l.v.GetString("defaults.memory")
	c.Defaults.Disk = l.v.GetString("defaults.disk")
	c.Defaults.Image = l.v.GetString("defaults.image")
	c.Defaults.VM = l.v.GetString("defaults.vm")

	c.Security.MountPolicy = l.v.GetString("security.mount_policy")
	if al := l.v.GetStringSlice("security.egress_allowlist"); len(al) > 0 {
		c.Security.EgressAllowlist = al
	}

	vmsRaw := l.v.GetStringMap("vms")
	for name := range vmsRaw {
		var vm VMDef
		sub := l.v.Sub("vms." + name)
		if sub != nil {
			vm.CPUs = sub.GetInt("cpus")
			vm.Memory = sub.GetString("memory")
			vm.Disk = sub.GetString("disk")
			vm.Image = sub.GetString("image")
			vm.Backend = sub.GetString("backend")
			vm.Provisions = sub.GetStringSlice("provisions")
			vm.Env = sub.GetStringMapString("env")
		}
		c.VMs[name] = vm
	}

	return &c
}

// GetForVM returns the resolved VMConfig for a named VM,
// applying the full inheritance chain. REQ-005-015
func (l *Loader) GetForVM(name string) (*VMConfig, error) {
	cfg := l.Get()

	// Start from defaults.
	vc := &VMConfig{
		Name:    name,
		Backend: cfg.Defaults.Backend,
		CPUs:    cfg.Defaults.CPUs,
		Memory:  cfg.Defaults.Memory,
		Disk:    cfg.Defaults.Disk,
		Image:   cfg.Defaults.Image,
	}

	// Apply VM definition from config files (vms.<name> section).
	if vmDef, ok := cfg.VMs[name]; ok {
		mergeVMDef(vc, &vmDef)
	}

	// Load VM-specific config file if it exists. REQ-005-007
	vmCfgPath := filepath.Join(l.sdHome, "vms", name, "config.yaml")
	if data, err := os.ReadFile(vmCfgPath); err == nil {
		var fileCfg VMConfig
		if err := yaml.Unmarshal(data, &fileCfg); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", vmCfgPath, err)
		}
		mergeVMFile(vc, &fileCfg)
	}

	return vc, nil
}

func mergeVMDef(dst *VMConfig, src *VMDef) {
	if src.Backend != "" {
		dst.Backend = src.Backend
	}
	if src.CPUs != 0 {
		dst.CPUs = src.CPUs
	}
	if src.Memory != "" {
		dst.Memory = src.Memory
	}
	if src.Disk != "" {
		dst.Disk = src.Disk
	}
	if src.Image != "" {
		dst.Image = src.Image
	}
	if len(src.Provisions) > 0 {
		dst.Provisions = src.Provisions
	}
	if len(src.Env) > 0 {
		dst.Env = src.Env
	}
}

func mergeVMFile(dst *VMConfig, src *VMConfig) {
	if src.Backend != "" {
		dst.Backend = src.Backend
	}
	if src.CPUs != 0 {
		dst.CPUs = src.CPUs
	}
	if src.Memory != "" {
		dst.Memory = src.Memory
	}
	if src.Disk != "" {
		dst.Disk = src.Disk
	}
	if src.Image != "" {
		dst.Image = src.Image
	}
	if len(src.Provisions) > 0 {
		dst.Provisions = src.Provisions
	}
	if len(src.Env) > 0 {
		dst.Env = src.Env
	}
	dst.State = src.State
	if len(src.BackendMeta) > 0 {
		dst.BackendMeta = src.BackendMeta
	}
}

// Source returns which configuration source provided the value for key.
// Returns one of the ConfigSource* constants, or "" if unknown. REQ-005-009
func (l *Loader) Source(key string) string {
	if l.v == nil || !l.v.IsSet(key) {
		return ""
	}

	// CLI flags (highest).
	if l.cliKeys[key] {
		return ConfigSourceCLIFlag
	}

	// Environment variable — check standard and alias names.
	standardEnv := "SD_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
	if _, ok := os.LookupEnv(standardEnv); ok {
		return ConfigSourceEnvVar
	}
	if alias, ok := envAliases[key]; ok {
		if _, ok := os.LookupEnv(alias); ok {
			return ConfigSourceEnvVar
		}
	}

	// Project-level config (security.* excluded per REQ-005-017).
	if l.projectV != nil && l.projectV.IsSet(key) &&
		!strings.HasPrefix(key, "security.") && key != "security" {
		return ConfigSourceProjectConfig
	}

	// User-level config.
	if l.userV != nil && l.userV.IsSet(key) {
		return ConfigSourceUserConfig
	}

	return ConfigSourceBuiltinDefault
}

// List returns all known config entries with source attribution. REQ-005-011
func (l *Loader) List() []ConfigEntry {
	entries := make([]ConfigEntry, 0, len(knownKeys))
	for _, key := range knownKeys {
		entries = append(entries, ConfigEntry{
			Key:    key,
			Value:  l.v.Get(key),
			Source: l.Source(key),
		})
	}
	return entries
}

// Validate checks all discoverable config files for errors. REQ-005-013
func (l *Loader) Validate() ([]ValidationResult, error) {
	var results []ValidationResult

	// User config.
	userPath := filepath.Join(l.sdHome, "config.yaml")
	if _, err := os.Stat(userPath); err == nil {
		results = append(results, validateConfigFile(userPath))
	}

	// Project config.
	if l.projectDir != "" {
		projPath := filepath.Join(l.projectDir, "config.yaml")
		if _, err := os.Stat(projPath); err == nil {
			r := validateConfigFile(projPath)
			checkProjectSecurity(projPath, &r)
			results = append(results, r)
		}
	}

	// VM configs.
	vmsDir := filepath.Join(l.sdHome, "vms")
	entries, err := os.ReadDir(vmsDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			vmPath := filepath.Join(vmsDir, e.Name(), "config.yaml")
			if _, err := os.Stat(vmPath); err == nil {
				results = append(results, validateConfigFile(vmPath))
			}
		}
	}

	return results, nil
}

func validateConfigFile(path string) ValidationResult {
	r := ValidationResult{Path: path, Valid: true}

	data, err := os.ReadFile(path)
	if err != nil {
		r.Valid = false
		r.Errors = append(r.Errors, ConfigError{Message: err.Error()})
		return r
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		r.Valid = false
		r.Errors = append(r.Errors, ConfigError{Message: fmt.Sprintf("invalid YAML: %s", err)})
		return r
	}

	// Validate known enum values.
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err == nil {
		if mp := cfg.Security.MountPolicy; mp != "" {
			switch mp {
			case MountPolicyNone, MountPolicyReadonly, MountPolicyProject:
			default:
				r.Valid = false
				r.Errors = append(r.Errors, ConfigError{
					Key:     "security.mount_policy",
					Message: fmt.Sprintf("invalid mount_policy %q: must be one of: none, readonly, project", mp),
				})
			}
		}
	}

	return r
}

func checkProjectSecurity(path string, r *ValidationResult) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return
	}
	if _, ok := raw["security"]; ok {
		r.Errors = append(r.Errors, ConfigError{
			Key:     "security",
			Message: "security settings in project-level config are ignored",
		})
	}
}

// Set writes a key-value pair to a config file.
// target is "user" or "project". Validates after write; rolls back on failure.
// REQ-005-010
func (l *Loader) Set(key string, value any, target string) error {
	var cfgPath string
	switch target {
	case "user":
		cfgPath = filepath.Join(l.sdHome, "config.yaml")
	case "project":
		if l.projectDir == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("cannot determine working directory: %w", err)
			}
			l.projectDir = filepath.Join(cwd, ".sd")
		}
		cfgPath = filepath.Join(l.projectDir, "config.yaml")
	default:
		return fmt.Errorf("invalid target %q: must be \"user\" or \"project\"", target)
	}

	// Ensure parent directory exists with 0700. REQ-005-016
	dir := filepath.Dir(cfgPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create config directory %s: %w", dir, err)
	}

	// Read existing file for rollback.
	origData, readErr := os.ReadFile(cfgPath)
	existed := readErr == nil

	raw := make(map[string]any)
	if existed {
		if err := yaml.Unmarshal(origData, &raw); err != nil {
			return fmt.Errorf("failed to parse %s: %w", cfgPath, err)
		}
		if raw == nil {
			raw = make(map[string]any)
		}
	}

	setNestedKey(raw, key, value)

	newData, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.WriteFile(cfgPath, newData, 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", cfgPath, err)
	}

	// Validate; rollback on failure.
	result := validateConfigFile(cfgPath)
	if !result.Valid {
		if existed {
			_ = os.WriteFile(cfgPath, origData, 0600)
		} else {
			_ = os.Remove(cfgPath)
		}
		msgs := make([]string, len(result.Errors))
		for i, e := range result.Errors {
			msgs[i] = e.Message
		}
		return fmt.Errorf("setting %s=%v would produce invalid config: %s; change was rolled back",
			key, value, strings.Join(msgs, "; "))
	}

	return nil
}

func setNestedKey(m map[string]any, key string, value any) {
	parts := strings.Split(key, ".")
	current := m
	for i, p := range parts {
		if i == len(parts)-1 {
			current[p] = value
			return
		}
		next, ok := current[p]
		if !ok {
			next = make(map[string]any)
			current[p] = next
		}
		if nextMap, ok := next.(map[string]any); ok {
			current = nextMap
		} else {
			nextMap := make(map[string]any)
			current[p] = nextMap
			current = nextMap
		}
	}
}
