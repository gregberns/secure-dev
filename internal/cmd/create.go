// Package cmd implements the create command.
// REQ-002-003: VM Management Commands -- create
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"sd/internal/backend"
	"sd/internal/config"
	"sd/internal/provision"
	"sd/internal/security"
	"sd/internal/ssh"
	"sd/internal/ui"
)

// captureHostKey captures the VM's SSH host key after creation.
// Overridden in tests with a digital twin.
var captureHostKey = ssh.CaptureHostKey

func init() {
	createCmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a new VM with the given name",
		Long: `Create a new VM environment for running AI coding agents.
The VM is provisioned using the configured backend (default: lima).

When no name is given, reads .sd.yaml from the current directory (or parent
directories) for project configuration including VM name, resources, modules,
and mounts.`,
		GroupID: "vm",
		Args:    rangeArgs(0, 1, "[name] [flags]"),
		RunE:    runCreate,
	}

	// REQ-002-003: create flags
	createCmd.Flags().String("backend", "", "VM backend to use (default: lima)")
	createCmd.Flags().Int("cpus", 0, "number of CPUs (default: 4)")
	createCmd.Flags().String("memory", "", "memory allocation, e.g. 4GiB (default: 8GiB)")
	createCmd.Flags().String("disk", "", "disk size, e.g. 50GiB (default: 100GiB)")
	createCmd.Flags().StringSlice("modules", nil, "provisioning modules to apply (comma-separated)")
	createCmd.Flags().StringArray("mount", nil, "mount host:guest[:ro|rw] (repeatable)")
	createCmd.Flags().StringArray("allow-egress", nil, "add domain to egress allowlist (repeatable)")
	createCmd.Flags().Bool("dry-run", false, "print resolved config without creating VM")

	rootCmd.AddCommand(createCmd)
}

// runCreate executes the create command.
// REQ-002-003, REQ-005-021
func runCreate(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	// REQ-005-021: When no positional arg, read .sd.yaml for project config
	name, projCfg, projDir, err := resolveCreateName(args)
	if err != nil {
		return err
	}

	// REQ-001-006: validate VM name format
	if err := backend.ValidateVMName(name); err != nil {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: err.Error(),
		}
	}

	// Resolve backend name: flag > .sd.yaml > config > default ("lima")
	// REQ-005-022: CLI flags override .sd.yaml values
	backendName, _ := cmd.Flags().GetString("backend")
	if backendName == "" && projCfg != nil && projCfg.Backend != "" {
		backendName = projCfg.Backend
	}
	if backendName == "" && Loader() != nil {
		cfg := Loader().Get()
		backendName = cfg.Defaults.Backend
	}
	if backendName == "" {
		backendName = "lima"
	}

	// REQ-002-003: Validate resource flags before building config
	if err := validateCreateResources(cmd); err != nil {
		return err
	}

	// REQ-002-003: Validate backend name is registered
	if err := validateBackendFunc(backendName); err != nil {
		return err
	}

	// Build VMConfig from flags, project config, and config defaults
	// REQ-005-022: Precedence: CLI flags > .sd.yaml > user config > defaults
	vmCfg := buildVMConfig(cmd, backendName, projCfg, projDir)

	// Validate mount specs early
	for _, m := range vmCfg.Mounts {
		if m.HostPath == "" || m.GuestPath == "" {
			return ui.CLIError{
				Code:    "invalid_argument",
				Message: fmt.Sprintf("invalid mount spec: host and guest paths are required (got host=%q guest=%q)", m.HostPath, m.GuestPath),
			}
		}
	}

	// REQ-004-005: Validate mount paths against sensitive directories
	// REQ-004-023: Include user-configured extra sensitive paths
	var extraSensitivePaths []string
	if ldr := Loader(); ldr != nil {
		cfg := ldr.Get()
		extraSensitivePaths = cfg.Security.SensitivePaths
	}
	for _, m := range vmCfg.Mounts {
		mode := security.MountReadOnly
		if m.Writable {
			mode = security.MountReadWrite
		}
		if err := security.ValidateMountPath(m.HostPath, mode, extraSensitivePaths); err != nil {
			return ui.CLIError{
				Code:    "mount_path_rejected",
				Message: err.Error(),
			}
		}
	}

	// REQ-001-006: --dry-run prints resolved config and exits
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		type dryRunResult struct {
			Name    string `json:"name"`
			Backend string `json:"backend"`
			CPUs    int    `json:"cpus"`
			Memory  string `json:"memory"`
			Disk    string `json:"disk"`
			Image   string `json:"image"`
			DryRun  bool   `json:"dry_run"`
		}
		result := dryRunResult{
			Name:    name,
			Backend: backendName,
			CPUs:    vmCfg.CPUs,
			Memory:  vmCfg.Memory,
			Disk:    vmCfg.Disk,
			Image:   vmCfg.BaseImage,
			DryRun:  true,
		}
		f.SuccessData(result, func() string {
			return fmt.Sprintf("Dry run: VM %q would be created with backend=%s cpus=%d memory=%s disk=%s image=%s\n",
				name, backendName, vmCfg.CPUs, vmCfg.Memory, vmCfg.Disk, vmCfg.BaseImage)
		})
		return nil
	}

	b, err := getBackendFunc(backendName)
	if err != nil {
		return ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("backend %q is not available: %v", backendName, err),
		}
	}

	// Check backend availability
	if err := b.Available(); err != nil {
		return ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("backend %q not available: %v", backendName, err),
		}
	}

	f.Progress(fmt.Sprintf("Creating VM %q with backend %q...", name, backendName))

	if err := b.Create(cmd.Context(), name, vmCfg); err != nil {
		if errors.Is(err, backend.ErrVMAlreadyExists) {
			return ui.CLIError{
				Code:    "vm_already_exists",
				Message: fmt.Sprintf("VM %q already exists", name),
			}
		}
		return ui.CLIError{
			Code:    "vm_create_failed",
			Message: fmt.Sprintf("failed to create VM %q: %v", name, err),
		}
	}

	modulesFlag := resolveModules(cmd, projCfg)
	if err := doCreateVM(cmd.Context(), f, b, name, backendName, vmCfg, modulesFlag); err != nil {
		return err
	}

	type createResult struct {
		Name    string `json:"name"`
		Backend string `json:"backend"`
		CPUs    int    `json:"cpus"`
		Memory  string `json:"memory"`
		Disk    string `json:"disk"`
	}

	result := createResult{
		Name:    name,
		Backend: backendName,
		CPUs:    vmCfg.CPUs,
		Memory:  vmCfg.Memory,
		Disk:    vmCfg.Disk,
	}

	f.SuccessData(result, func() string {
		return fmt.Sprintf("VM %q created successfully.\n", name)
	})
	return nil
}

// doCreateVM handles the start+provision+persist+audit flow after a VM has been
// created by the backend. Used by both 'sd create' and 'sd ensure'.
// REQ-002-024
func doCreateVM(ctx context.Context, f *ui.Formatter, b backend.Backend, name, backendName string, vmCfg backend.VMConfig, modules []string) error {
	// REQ-001-006 step 4: Start the VM before provisioning.
	// Lima's create only defines the VM config; start boots it.
	f.Progress(fmt.Sprintf("Starting VM %q...", name))
	if err := b.Start(ctx, name); err != nil {
		// Start failed -- clean up the created-but-not-started VM
		b.Destroy(ctx, name)
		return ui.CLIError{
			Code:    "vm_start_failed",
			Message: fmt.Sprintf("failed to start VM %q after creation: %v", name, err),
		}
	}

	// REQ-007-003: Generate per-VM SSH keys and inject public key into VM.
	// REQ-007-004: Write SSH config fragment for standard SSH tools.
	// REQ-004-031: Capture SSH host key for TCP-based connections.
	sdHome := ""
	if l := Loader(); l != nil {
		sdHome = l.SDHome()
	}
	if sdHome == "" {
		sdHome = defaultSDHome()
	}
	setupSSH(ctx, f, b, name, sdHome)

	// REQ-001-006 step 5: Run provisioning modules after VM creation.
	provResult := runCreateProvision(ctx, f, b, name, modules)
	if provResult != nil && provResult.Failed {
		// Provisioning failed -- clean up the partially-created VM
		f.Progress(fmt.Sprintf("Provisioning failed, cleaning up VM %q...", name))
		b.Destroy(ctx, name)
		return ui.CLIError{
			Code:    "provision_script_failed",
			Message: fmt.Sprintf("module %q failed: %s", provResult.Module, provResult.Error),
		}
	}

	// REQ-001-006 step 6: Persist VM configuration
	// REQ-005-007: Set initial VM state
	if l := Loader(); l != nil {
		vmConfigPersist := &config.VMConfig{
			Name:    name,
			Backend: backendName,
			CPUs:    vmCfg.CPUs,
			Memory:  vmCfg.Memory,
			Disk:    vmCfg.Disk,
			Image:   vmCfg.BaseImage,
			State: config.VMState{
				Status:      config.VMStatusRunning,
				CreatedAt:   time.Now(),
				LastStarted: time.Now(),
			},
		}
		if len(modules) > 0 {
			vmConfigPersist.Provisions = modules
		}
		if err := l.WriteVMConfig(vmConfigPersist); err != nil {
			f.Progress(fmt.Sprintf("Warning: could not persist VM config: %v", err))
		}
	}

	// REQ-004-022: Log VM lifecycle event
	if al := AuditLog(); al != nil {
		_ = al.LogEvent(security.EventLogEntry{
			Timestamp: time.Now(),
			EventType: "vm.create",
			VMName:    name,
		})
	}

	return nil
}

// defaultSDHome returns the default SD home directory (~/.sd).
func defaultSDHome() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".sd")
}

// setupSSH generates SSH keys, injects the public key into the VM, captures host keys,
// and writes the SSH config fragment. All errors are non-fatal warnings.
// REQ-007-003, REQ-007-004, REQ-004-031
func setupSSH(ctx context.Context, f *ui.Formatter, b backend.Backend, name, sdHome string) {
	// Step 1: Generate per-VM SSH keys
	if err := generateSSHKeys(sdHome, name); err != nil {
		f.Progress(fmt.Sprintf("Warning: could not generate SSH keys: %v", err))
		return
	}

	// Step 2: Read the public key
	_, _, pubKeyPath := ssh.KeyPaths(sdHome, name)
	pubKeyBytes, err := os.ReadFile(pubKeyPath)
	if err != nil {
		f.Progress(fmt.Sprintf("Warning: could not read public key: %v", err))
		return
	}

	// Step 3: Inject public key into VM's authorized_keys
	pubKey := strings.TrimSpace(string(pubKeyBytes))
	injectCmd := fmt.Sprintf("mkdir -p ~/.ssh && chmod 700 ~/.ssh && echo %q >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys", pubKey)
	if _, execErr := b.Exec(ctx, name, []string{"bash", "-c", injectCmd}); execErr != nil {
		f.Progress(fmt.Sprintf("Warning: could not inject SSH public key: %v", execErr))
	}

	// Step 4: Get SSH config from backend and write config fragment + capture host key
	sshCfg, err := b.SSHConfig(ctx, name)
	if err != nil {
		f.Progress(fmt.Sprintf("Warning: could not get SSH config: %v", err))
		return
	}

	// REQ-004-031: Capture host key for TCP connections
	if sshCfg.Transport == "tcp" && sshCfg.Host != "" && sshCfg.Port > 0 {
		if hkErr := captureHostKey(sdHome, name, sshCfg.Host, sshCfg.Port); hkErr != nil {
			f.Progress(fmt.Sprintf("Warning: could not capture SSH host key: %v", hkErr))
		}
	}

	// REQ-007-004: Write SSH config fragment
	home, _ := os.UserHomeDir()
	sshDir := filepath.Join(home, ".ssh")
	fragmentOpts := ssh.SSHFragmentOpts{
		VMName:       name,
		HostName:     sshCfg.Host,
		Port:         sshCfg.Port,
		User:         sshCfg.User,
		SDHome:       sdHome,
		ProxyCommand: sshCfg.ProxyCommand,
	}
	if sshCfg.Transport == "vsock" {
		fragmentOpts.Transport = ssh.TransportVSOCK
	} else {
		fragmentOpts.Transport = ssh.TransportTCP
	}
	if err := ssh.WriteFragment(sshDir, fragmentOpts); err != nil {
		f.Progress(fmt.Sprintf("Warning: could not write SSH config fragment: %v", err))
	}

	// REQ-007-004: Warn if SSH config doesn't include sd fragments
	sshConfigPath := filepath.Join(sshDir, "config")
	if sshConfigData, err := os.ReadFile(sshConfigPath); err == nil {
		if ssh.NeedsInclude(sshConfigData) {
			f.Progress("Warning: ~/.ssh/config does not include 'Include config.d/*'. SSH shortcuts for sd VMs will not work. Add this line to the top of ~/.ssh/config: Include config.d/*")
		}
	}
}

// generateSSHKeys generates per-VM SSH keys. Overrideable for testing.
var generateSSHKeys = ssh.GenerateKeys

// runCreateProvision runs provisioning modules on a newly created VM.
// REQ-001-006 step 5: Provision after VM creation.
// Returns nil if no modules are requested, otherwise the provisioning result.
func runCreateProvision(ctx context.Context, f *ui.Formatter, b backend.Backend, name string, modulesFlag []string) *provision.ProvisionResult {
	// Resolve which modules to run
	allModules, err := loadBuiltinModules()
	if err != nil {
		f.Progress(fmt.Sprintf("Warning: could not load provisioning modules: %v", err))
		return nil
	}

	var resolved []provision.Module
	if len(modulesFlag) == 1 && strings.TrimSpace(modulesFlag[0]) == "all" {
		// REQ-006-015: --modules all provisions every available module
		resolved, err = provision.ResolveAll(allModules)
	} else if len(modulesFlag) > 0 {
		// Trim whitespace from module names
		requested := make([]string, len(modulesFlag))
		for i, m := range modulesFlag {
			requested[i] = strings.TrimSpace(m)
		}
		resolved, err = provision.ResolveRequested(allModules, requested)
	} else {
		// No modules specified -- provision default set (base + security hardening).
		// App modules (claude-code, docker, golang, etc.) are opt-in via --modules.
		// REQ-004-006, REQ-004-025, REQ-004-026.
		resolved, err = provision.ResolveRequested(allModules, provision.DefaultModuleNames)
	}
	if err != nil {
		f.Progress(fmt.Sprintf("Warning: could not resolve modules: %v", err))
		return nil
	}

	if len(resolved) == 0 {
		return nil
	}

	f.Progress(fmt.Sprintf("Provisioning VM %q with %d module(s)...", name, len(resolved)))

	// Execute provisioning via backend Exec
	execFn := func(execCtx context.Context, vmName string, command []string) (string, string, int, error) {
		result, execErr := b.Exec(execCtx, vmName, command)
		if execErr != nil {
			return "", "", 0, execErr
		}
		return result.Stdout, result.Stderr, result.ExitCode, nil
	}

	result := provision.Provision(ctx, execFn, name, resolved)
	return &result
}

// buildVMConfig constructs a backend.VMConfig from CLI flags, project config, and config defaults.
// REQ-002-003, REQ-005-022: Precedence: CLI flags > .sd.yaml > user config > defaults
func buildVMConfig(cmd *cobra.Command, backendName string, projCfg *config.ProjectConfig, projDir string) backend.VMConfig {
	// Start with built-in defaults
	cfgCPUs := 4
	cfgMemory := "8GiB"
	cfgDisk := "100GiB"
	cfgImage := "ubuntu:24.04"

	// Layer 1: user config (~/.sd/config.yaml) overrides built-in defaults
	if Loader() != nil {
		cfg := Loader().Get()
		if cfg.Defaults.CPUs > 0 {
			cfgCPUs = cfg.Defaults.CPUs
		}
		if cfg.Defaults.Memory != "" {
			cfgMemory = cfg.Defaults.Memory
		}
		if cfg.Defaults.Disk != "" {
			cfgDisk = cfg.Defaults.Disk
		}
		if cfg.Defaults.Image != "" {
			cfgImage = cfg.Defaults.Image
		}
	}

	// Layer 2: .sd.yaml overrides user config
	if projCfg != nil {
		if projCfg.CPUs > 0 {
			cfgCPUs = projCfg.CPUs
		}
		if projCfg.Memory != "" {
			cfgMemory = projCfg.Memory
		}
		if projCfg.Disk != "" {
			cfgDisk = projCfg.Disk
		}
	}

	// Layer 3: CLI flags override .sd.yaml
	cpus, _ := cmd.Flags().GetInt("cpus")
	if cpus <= 0 {
		cpus = cfgCPUs
	}
	memory, _ := cmd.Flags().GetString("memory")
	if memory == "" {
		memory = cfgMemory
	}
	disk, _ := cmd.Flags().GetString("disk")
	if disk == "" {
		disk = cfgDisk
	}

	// Parse mount specs: host:guest[:mode]
	// CLI mounts take precedence; if none specified, use .sd.yaml mounts
	var mounts []backend.Mount
	mountSpecs, _ := cmd.Flags().GetStringArray("mount")
	if len(mountSpecs) == 0 && projCfg != nil && len(projCfg.Mounts) > 0 {
		// Resolve "." and relative paths relative to .sd.yaml location
		resolved := config.ResolveMountPaths(projCfg.Mounts, projDir)
		for _, spec := range resolved {
			if spec == "" {
				continue
			}
			m := parseMountSpec(spec)
			mounts = append(mounts, m)
		}
	} else {
		for _, spec := range mountSpecs {
			if spec == "" {
				continue
			}
			m := parseMountSpec(spec)
			mounts = append(mounts, m)
		}
	}

	// Collect egress domains
	var egress []string
	egressFlags, _ := cmd.Flags().GetStringArray("allow-egress")
	if len(egressFlags) == 0 && projCfg != nil && len(projCfg.AllowEgress) > 0 {
		egress = projCfg.AllowEgress
	} else {
		for _, domain := range egressFlags {
			if domain != "" {
				egress = append(egress, domain)
			}
		}
	}

	// Collect modules -- consumed by runCreateProvision, not backend
	modules, _ := cmd.Flags().GetStringSlice("modules")
	_ = modules

	_ = egress // consumed by security subsystem, not backend

	return backend.VMConfig{
		CPUs:      cpus,
		Memory:    memory,
		Disk:      disk,
		BaseImage: cfgImage,
		Mounts:    mounts,
		EnvVars:   make(map[string]string),
	}
}

// getWorkingDir returns the current working directory. Overridable for testing.
var getWorkingDir = os.Getwd

// resolveCreateName determines the VM name from args or .sd.yaml.
// REQ-005-021: When no positional arg, read .sd.yaml from CWD.
func resolveCreateName(args []string) (name string, projCfg *config.ProjectConfig, projDir string, err error) {
	if len(args) > 0 && args[0] != "" {
		// Explicit name provided -- still try to load .sd.yaml for other settings
		name = args[0]
		cwd, cwdErr := getWorkingDir()
		if cwdErr == nil {
			configPath, cfg, findErr := config.FindProjectConfig(cwd)
			if findErr == nil && cfg != nil {
				projCfg = cfg
				projDir = filepath.Dir(configPath)
			}
		}
		return name, projCfg, projDir, nil
	}

	// No positional arg -- .sd.yaml is required
	cwd, err := getWorkingDir()
	if err != nil {
		return "", nil, "", ui.CLIError{
			Code:    "cwd_unavailable",
			Message: fmt.Sprintf("cannot determine current directory: %v", err),
		}
	}

	configPath, cfg, findErr := config.FindProjectConfig(cwd)
	if findErr != nil {
		return "", nil, "", ui.CLIError{
			Code:    "project_config_invalid",
			Message: findErr.Error(),
		}
	}
	if cfg == nil {
		return "", nil, "", ui.CLIError{
			Code:    "missing_vm_name",
			Message: "missing VM name -- provide a name or create .sd.yaml",
		}
	}

	return cfg.Name, cfg, filepath.Dir(configPath), nil
}

// resolveModules determines which modules to use.
// CLI flags take precedence over .sd.yaml modules.
// REQ-005-022
func resolveModules(cmd *cobra.Command, projCfg *config.ProjectConfig) []string {
	modulesFlag, _ := cmd.Flags().GetStringSlice("modules")
	if len(modulesFlag) > 0 {
		return modulesFlag
	}
	if projCfg != nil && len(projCfg.Modules) > 0 {
		return projCfg.Modules
	}
	return nil
}

// parseMountSpec parses a mount specification string host:guest[:mode].
// REQ-002-003
func parseMountSpec(spec string) backend.Mount {
	parts := strings.SplitN(spec, ":", 3)
	m := backend.Mount{
		HostPath:  parts[0],
		GuestPath: "",
		Writable:  false, // default ro per spec
	}
	if len(parts) >= 2 {
		m.GuestPath = parts[1]
	}
	if len(parts) >= 3 {
		m.Writable = parts[2] == "rw"
	}
	return m
}
