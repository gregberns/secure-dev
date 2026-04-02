package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Loader provides access to the resolved configuration.
// REQ-005-001 through REQ-005-018
type Loader struct {
	mu            sync.RWMutex
	v             *viper.Viper
	sdHome        string
	projectDir    string
	loaded        bool
	warnings      []string
	sourceMap     map[string]Source
}

// LoaderOption configures a Loader during creation.
type LoaderOption func(*Loader)

// WithSDHome sets a custom SD_HOME directory.
func WithSDHome(path string) LoaderOption {
	return func(l *Loader) { l.sdHome = path }
}

// WithProjectDir sets the project-level config directory.
func WithProjectDir(path string) LoaderOption {
	return func(l *Loader) { l.projectDir = path }
}

// NewLoader creates a new configuration Loader.
func NewLoader(opts ...LoaderOption) *Loader {
	l := &Loader{
		sdHome:    defaultSDHome(),
		sourceMap: make(map[string]Source),
		warnings:  make([]string, 0),
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// defaultSDHome returns the default SD_HOME path.
func defaultSDHome() string {
	if home := os.Getenv("SD_HOME"); home != "" {
		return home
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".sd")
}

// SDHome returns the configured SD_HOME directory.
func (l *Loader) SDHome() string {
	return l.sdHome
}

// Load reads and merges all configuration sources.
// REQ-005-001: Precedence order
// REQ-005-017: Security keys in project config are ignored
func (l *Loader) Load() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.v = viper.New()
	l.sourceMap = make(map[string]Source)
	l.warnings = l.warnings[:0]

	// Set built-in defaults
	l.setDefaults()

	// Configure Viper
	l.v.SetConfigName("config")
	l.v.SetConfigType("yaml")
	l.v.SetEnvPrefix("SD")
	l.v.AutomaticEnv()
	l.v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Bind documented short-form env vars from REQ-005-005
	l.bindEnvVar("defaults.backend", "SD_BACKEND")
	l.bindEnvVar("defaults.vm", "SD_DEFAULT_VM")
	l.bindEnvVar("output.json", "SD_JSON")

	// Snapshot env var overrides before loading config files.
	// Viper's Set() from file loading would override env values,
	// so we capture them here and reapply after.
	envSnapshot := l.snapshotEnvOverrides()

	// Load user-level config
	userConfigPath := filepath.Join(l.sdHome, "config.yaml")
	if err := l.loadConfigFile(userConfigPath, SourceUserConfig); err != nil {
		// File not existing is OK; parse errors are fatal
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to load user config %s: %w", userConfigPath, err)
		}
	}

	// Load project-level config (with security key filtering)
	if l.projectDir != "" {
		projectConfigPath := filepath.Join(l.projectDir, "config.yaml")
		if err := l.loadProjectConfig(projectConfigPath); err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("failed to load project config %s: %w", projectConfigPath, err)
			}
		}
	}

	// Restore env var overrides so they take precedence over config files.
	l.restoreEnvOverrides(envSnapshot)

	// REQ-005-016: Verify SD_HOME directory permissions
	if err := l.verifyPermissions(); err != nil {
		l.warnings = append(l.warnings, err.Error())
	}

	l.loaded = true
	return nil
}

// Get returns the fully resolved Config.
func (l *Loader) Get() *Config {
	l.mu.RLock()
	defer l.mu.RUnlock()

	cfg := DefaultConfig()

	if l.v == nil {
		return cfg
	}

	cfg.Defaults.Backend = l.v.GetString("defaults.backend")
	cfg.Defaults.CPUs = l.v.GetInt("defaults.cpus")
	cfg.Defaults.Memory = l.v.GetString("defaults.memory")
	cfg.Defaults.Disk = l.v.GetString("defaults.disk")
	cfg.Defaults.Image = l.v.GetString("defaults.image")
	cfg.Defaults.VM = l.v.GetString("defaults.vm")

	cfg.Security.MountPolicy = l.v.GetString("security.mount_policy")
	cfg.Security.EgressAllowlist = l.v.GetStringSlice("security.egress_allowlist")
	cfg.Security.SensitivePaths = l.v.GetStringSlice("security.sensitive_paths")

	// Try to unmarshal VMs
	vmKeys := l.v.GetStringMap("vms")
	if len(vmKeys) > 0 {
		cfg.VMs = make(map[string]VMDef)
		for name := range vmKeys {
			var vmDef VMDef
			keyPrefix := "vms." + name + "."
			vmDef.Backend = l.v.GetString(keyPrefix + "backend")
			vmDef.CPUs = l.v.GetInt(keyPrefix + "cpus")
			vmDef.Memory = l.v.GetString(keyPrefix + "memory")
			vmDef.Disk = l.v.GetString(keyPrefix + "disk")
			vmDef.Image = l.v.GetString(keyPrefix + "image")
			vmDef.Provisions = l.v.GetStringSlice(keyPrefix + "provisions")
			vmDef.Env = l.v.GetStringMapString(keyPrefix + "env")
			cfg.VMs[name] = vmDef
		}
	}

	return cfg
}

// GetForVM returns the resolved configuration for a specific VM,
// applying the inheritance chain defined in REQ-005-015.
func (l *Loader) GetForVM(name string) (*VMConfig, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.v == nil {
		return nil, fmt.Errorf("config not loaded")
	}

	cfg := DefaultConfig()
	result := &VMConfig{
		Name:        name,
		Backend:     cfg.Defaults.Backend,
		CPUs:        cfg.Defaults.CPUs,
		Memory:      cfg.Defaults.Memory,
		Disk:        cfg.Defaults.Disk,
		Image:       cfg.Defaults.Image,
		Env:         make(map[string]string),
		BackendMeta: make(map[string]any),
	}

	// Check for VM definition in config
	vmPrefix := "vms." + name + "."
	if l.v.IsSet(vmPrefix + "backend") {
		result.Backend = l.v.GetString(vmPrefix + "backend")
	}
	if l.v.IsSet(vmPrefix + "cpus") {
		result.CPUs = l.v.GetInt(vmPrefix + "cpus")
	}
	if l.v.IsSet(vmPrefix + "memory") {
		result.Memory = l.v.GetString(vmPrefix + "memory")
	}
	if l.v.IsSet(vmPrefix + "disk") {
		result.Disk = l.v.GetString(vmPrefix + "disk")
	}
	if l.v.IsSet(vmPrefix + "image") {
		result.Image = l.v.GetString(vmPrefix + "image")
	}
	if l.v.IsSet(vmPrefix + "provisions") {
		result.Provisions = l.v.GetStringSlice(vmPrefix + "provisions")
	}
	if env := l.v.GetStringMapString(vmPrefix + "env"); len(env) > 0 {
		result.Env = env
	}

	// Load from VM config file if it exists
	vmConfigPath := filepath.Join(l.sdHome, "vms", name, "config.yaml")
	if data, err := os.ReadFile(vmConfigPath); err == nil {
		vmViper := viper.New()
		vmViper.SetConfigType("yaml")
		vmViper.ReadConfig(strings.NewReader(string(data)))

		if vmViper.IsSet("backend") {
			result.Backend = vmViper.GetString("backend")
		}
		if vmViper.IsSet("cpus") {
			result.CPUs = vmViper.GetInt("cpus")
		}
		if vmViper.IsSet("memory") {
			result.Memory = vmViper.GetString("memory")
		}
		if vmViper.IsSet("disk") {
			result.Disk = vmViper.GetString("disk")
		}
		if vmViper.IsSet("image") {
			result.Image = vmViper.GetString("image")
		}
		if vmViper.IsSet("provisions") {
			result.Provisions = vmViper.GetStringSlice("provisions")
		}
		if env := vmViper.GetStringMapString("env"); len(env) > 0 {
			for k, v := range env {
				result.Env[k] = v
			}
		}

		// Load state section
		result.State.Status = vmViper.GetString("state.status")
		result.State.CreatedAt = vmViper.GetTime("state.created_at")
		result.State.LastStarted = vmViper.GetTime("state.last_started")
		result.State.LastStopped = vmViper.GetTime("state.last_stopped")

		// Load backend_meta
		result.BackendMeta = vmViper.GetStringMap("backend_meta")
	}

	return result, nil
}

// Source returns the source that provided the value for the given key.
// REQ-005-009
func (l *Loader) Source(key string) Source {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.v == nil {
		return SourceDefault
	}

	// Check explicit source map first
	if src, ok := l.sourceMap[key]; ok {
		return src
	}

	// Check if set via environment variable (for keys not in snapshot)
	envKey := "SD_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
	if os.Getenv(envKey) != "" {
		return SourceEnv
	}

	// Not in sourceMap and not from env: it's a built-in default
	return SourceDefault
}

// GetKey returns the resolved value for a configuration key.
// REQ-005-009: Used by config get command to retrieve values by dotted key.
func (l *Loader) GetKey(key string) (any, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.v == nil {
		return nil, fmt.Errorf("config not loaded")
	}
	return l.v.Get(key), nil
}

// ProjectDir returns the project-level config directory path.
func (l *Loader) ProjectDir() string {
	return l.projectDir
}

// Warnings returns warnings collected during loading.
func (l *Loader) Warnings() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	result := make([]string, len(l.warnings))
	copy(result, l.warnings)
	return result
}

// Resolve expands environment variable references in a string value.
// REQ-005-008
func (l *Loader) Resolve(value string) string {
	return ResolveEnvVars(value, func(msg string) {
		l.mu.Lock()
		l.warnings = append(l.warnings, msg)
		l.mu.Unlock()
	})
}

// Set writes a key-value pair to the specified config file.
// REQ-005-010
func (l *Loader) Set(key string, value interface{}, target string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var filePath string
	switch target {
	case "user":
		filePath = filepath.Join(l.sdHome, "config.yaml")
	case "project":
		if l.projectDir == "" {
			return fmt.Errorf("no project directory configured")
		}
		filePath = filepath.Join(l.projectDir, "config.yaml")
	default:
		return fmt.Errorf("invalid target %q: must be \"user\" or \"project\"", target)
	}

	// Ensure directory exists with correct permissions
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create config directory %s: %w", dir, err)
	}

	// Load existing config or create new
	v := viper.New()
	v.SetConfigFile(filePath)
	if data, err := os.ReadFile(filePath); err == nil {
		v.SetConfigType("yaml")
		v.ReadConfig(strings.NewReader(string(data)))
	}

	// Set the value
	v.Set(key, value)

	// REQ-005-016: Write with explicit 0600 permissions to prevent TOCTOU
	if err := l.writeConfigSecure(v.AllSettings(), filePath); err != nil {
		return fmt.Errorf("failed to write config to %s: %w", filePath, err)
	}

	return nil
}

// Validate checks all discoverable config files for errors.
// REQ-005-013
func (l *Loader) Validate() ([]ValidationResult, error) {
	var results []ValidationResult

	// Validate user-level config
	userConfig := filepath.Join(l.sdHome, "config.yaml")
	results = append(results, l.validateFile(userConfig)...)

	// Validate project-level config
	if l.projectDir != "" {
		projectConfig := filepath.Join(l.projectDir, "config.yaml")
		results = append(results, l.validateFile(projectConfig)...)
	}

	// Validate VM configs
	vmsDir := filepath.Join(l.sdHome, "vms")
	if entries, err := os.ReadDir(vmsDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				vmConfig := filepath.Join(vmsDir, entry.Name(), "config.yaml")
				results = append(results, l.validateFile(vmConfig)...)
			}
		}
	}

	return results, nil
}

// validateFile validates a single config file.
func (l *Loader) validateFile(path string) []ValidationResult {
	result := ValidationResult{Path: path, Valid: true}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // File not existing is not an error
	}
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return []ValidationResult{result}
	}

	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(string(data))); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("YAML parse error: %v", err))
		return []ValidationResult{result}
	}

	// Validate mount_policy
	if v.IsSet("security.mount_policy") {
		policy := v.GetString("security.mount_policy")
		if err := ValidateMountPolicy(policy); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, err.Error())
		}
	}

	// Validate VM statuses in state section
	if v.IsSet("state.status") {
		status := v.GetString("state.status")
		if err := ValidateVMStatus(status); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, err.Error())
		}
	}

	return []ValidationResult{result}
}

// bindEnvVar binds a Viper key to a specific environment variable.
// REQ-005-005
func (l *Loader) bindEnvVar(key, envVar string) {
	_ = l.v.BindEnv(key, envVar)
}

// envOverride represents a key-value pair from an environment variable.
type envOverride struct {
	key   string
	value interface{}
}

// snapshotEnvOverrides captures current env var values that map to config keys.
// This is needed because Viper's Set() from file loading overrides env values.
func (l *Loader) snapshotEnvOverrides() []envOverride {
	var overrides []envOverride

	// Check all known keys for env var overrides
	knownKeys := []string{
		"defaults.backend", "defaults.cpus", "defaults.memory",
		"defaults.disk", "defaults.image", "defaults.vm",
		"security.mount_policy",
	}

	for _, key := range knownKeys {
		// Check via the automatic env mechanism
		envKey := "SD_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		if val, ok := os.LookupEnv(envKey); ok {
			overrides = append(overrides, envOverride{key: key, value: val})
		}
	}

	// Check short-form aliases
	if val, ok := os.LookupEnv("SD_BACKEND"); ok {
		overrides = append(overrides, envOverride{key: "defaults.backend", value: val})
	}
	if val, ok := os.LookupEnv("SD_DEFAULT_VM"); ok {
		overrides = append(overrides, envOverride{key: "defaults.vm", value: val})
	}
	if val, ok := os.LookupEnv("SD_JSON"); ok {
		overrides = append(overrides, envOverride{key: "output.json", value: val})
	}

	return overrides
}

// restoreEnvOverrides reapplies env var values that were captured before config file loading.
func (l *Loader) restoreEnvOverrides(overrides []envOverride) {
	for _, o := range overrides {
		l.v.Set(o.key, o.value)
		l.sourceMap[o.key] = SourceEnv
	}
}

// setDefaults configures Viper with built-in defaults.
// REQ-005-004
func (l *Loader) setDefaults() {
	l.v.SetDefault("defaults.backend", "lima")
	l.v.SetDefault("defaults.cpus", 4)
	l.v.SetDefault("defaults.memory", "8GiB")
	l.v.SetDefault("defaults.disk", "100GiB")
	l.v.SetDefault("defaults.image", "ubuntu:24.04")
	l.v.SetDefault("defaults.vm", "")
	l.v.SetDefault("security.mount_policy", MountPolicyNone)
	l.v.SetDefault("security.egress_allowlist", DefaultEgressAllowlist)
}

// loadConfigFile loads a config file and records source for keys.
func (l *Loader) loadConfigFile(path string, source Source) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(string(data))); err != nil {
		return err
	}

	// Record all keys from this source
	keys := v.AllKeys()
	for _, key := range keys {
		l.sourceMap[key] = source
		// Set the value in our main viper
		l.v.Set(key, v.Get(key))
	}

	return nil
}

// loadProjectConfig loads project config, filtering security keys.
// REQ-005-017
func (l *Loader) loadProjectConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(string(data))); err != nil {
		return err
	}

	// Filter out security keys and emit warnings
	keys := v.AllKeys()
	for _, key := range keys {
		if IsSecurityKey(key) {
			l.warnings = append(l.warnings,
				fmt.Sprintf("warning: security settings in project-level config are ignored (found: %s); set these in ~/.sd/config.yaml or via CLI flags", key))
			continue
		}
		l.sourceMap[key] = SourceProjectConfig
		l.v.Set(key, v.Get(key))
	}

	return nil
}

// verifyPermissions checks that SD_HOME and its files have secure permissions.
// REQ-005-016: SD_HOME must be 0700, config files must be 0600.
func (l *Loader) verifyPermissions() error {
	info, err := os.Stat(l.sdHome)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // SD_HOME doesn't exist yet
		}
		return fmt.Errorf("cannot check SD_HOME permissions: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("SD_HOME %s is not a directory", l.sdHome)
	}
	mode := info.Mode().Perm()
	if mode != 0700 {
		return fmt.Errorf("SD_HOME %s has permissions %04o, expected 0700; run: chmod 700 %s", l.sdHome, mode, l.sdHome)
	}

	configPath := filepath.Join(l.sdHome, "config.yaml")
	if fi, err := os.Stat(configPath); err == nil {
		if perm := fi.Mode().Perm(); perm&0077 != 0 {
			return fmt.Errorf("config file %s has permissions %04o, expected 0600; run: chmod 600 %s", configPath, perm, configPath)
		}
	}

	return nil
}

// writeConfigSecure writes configuration data to a file with explicit 0600
// permissions, avoiding the TOCTOU race of WriteConfigAs + Chmod.
// REQ-005-016
func (l *Loader) writeConfigSecure(settings map[string]any, filePath string) error {
	data, err := yaml.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	dir := filepath.Dir(filePath)
	tmp, err := os.OpenFile(
		filepath.Join(dir, ".config.yaml.tmp"),
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600,
	)
	if err != nil {
		return fmt.Errorf("failed to create temp config file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("failed to write config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to close temp config file: %w", err)
	}

	if err := os.Rename(tmpName, filePath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to rename config file: %w", err)
	}
	return nil
}

// WriteVMConfig persists a VM configuration to $SD_HOME/vms/<name>/config.yaml.
// REQ-005-007: VM configuration file format.
func (l *Loader) WriteVMConfig(cfg *VMConfig) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if cfg.Name == "" {
		return fmt.Errorf("VM config name must not be empty")
	}

	dir := filepath.Join(l.sdHome, "vms", cfg.Name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create VM config directory: %w", err)
	}

	filePath := filepath.Join(dir, "config.yaml")
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal VM config: %w", err)
	}

	// REQ-005-016: Write with explicit 0600 mode via atomic temp+rename
	tmpPath := filePath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create temp VM config: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write VM config: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close VM config: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename VM config: %w", err)
	}
	return nil
}

// ReadVMConfig reads a VM configuration from $SD_HOME/vms/<name>/config.yaml.
// REQ-005-007
func (l *Loader) ReadVMConfig(name string) (*VMConfig, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	filePath := filepath.Join(l.sdHome, "vms", name, "config.yaml")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read VM config: %w", err)
	}

	var cfg VMConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse VM config: %w", err)
	}
	return &cfg, nil
}

// EnsureSDHome creates the SD_HOME directory with correct permissions if it
// does not exist. REQ-005-016
func (l *Loader) EnsureSDHome() error {
	info, err := os.Stat(l.sdHome)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("SD_HOME %s exists but is not a directory", l.sdHome)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("cannot check SD_HOME: %w", err)
	}
	if err := os.MkdirAll(l.sdHome, 0700); err != nil {
		return fmt.Errorf("failed to create SD_HOME %s: %w", l.sdHome, err)
	}
	return nil
}
