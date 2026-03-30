// Package cmd implements the create command.
// REQ-002-003: VM Management Commands -- create
package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"sd/internal/backend"
	"sd/internal/security"
	"sd/internal/ssh"
	"sd/internal/ui"
)

// captureHostKey captures the VM's SSH host key after creation.
// Overridden in tests with a digital twin.
var captureHostKey = ssh.CaptureHostKey

func init() {
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new VM with the given name",
		Long: `Create a new VM environment for running AI coding agents.
The VM is provisioned using the configured backend (default: lima).`,
		GroupID: "vm",
		Args:    cobra.ExactArgs(1),
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

	rootCmd.AddCommand(createCmd)
}

// runCreate executes the create command.
// REQ-002-003
func runCreate(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	name := args[0]
	if name == "" {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: "VM name must not be empty",
		}
	}

	// REQ-001-006: validate VM name format
	if err := backend.ValidateVMName(name); err != nil {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: err.Error(),
		}
	}

	// Resolve backend name: flag > config > default ("lima")
	backendName, _ := cmd.Flags().GetString("backend")
	if backendName == "" && Loader() != nil {
		cfg := Loader().Get()
		backendName = cfg.Defaults.Backend
	}
	if backendName == "" {
		backendName = "lima"
	}

	// Build VMConfig from flags and config defaults
	vmCfg := buildVMConfig(cmd, backendName)

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
	for _, m := range vmCfg.Mounts {
		mode := security.MountReadOnly
		if m.Writable {
			mode = security.MountReadWrite
		}
		if err := security.ValidateMountPath(m.HostPath, mode, nil); err != nil {
			return ui.CLIError{
				Code:    "mount_path_rejected",
				Message: err.Error(),
			}
		}
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

	// REQ-004-031: Capture SSH host key for TCP-based connections
	if l := Loader(); l != nil {
		sdHome := l.SDHome()
		if sshCfg, err := b.SSHConfig(cmd.Context(), name); err == nil {
			if sshCfg.Transport == "tcp" && sshCfg.Host != "" && sshCfg.Port > 0 {
				if hkErr := captureHostKey(sdHome, name, sshCfg.Host, sshCfg.Port); hkErr != nil {
					f.Progress(fmt.Sprintf("Warning: could not capture SSH host key: %v", hkErr))
				}
			}
		}
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

// buildVMConfig constructs a backend.VMConfig from CLI flags and config defaults.
// REQ-002-003
func buildVMConfig(cmd *cobra.Command, backendName string) backend.VMConfig {
	// Start with config defaults
	cfgCPUs := 4
	cfgMemory := "8GiB"
	cfgDisk := "100GiB"
	cfgImage := "ubuntu:24.04"

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

	// Override with flags if set
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
	var mounts []backend.Mount
	mountSpecs, _ := cmd.Flags().GetStringArray("mount")
	for _, spec := range mountSpecs {
		if spec == "" {
			continue
		}
		m := parseMountSpec(spec)
		mounts = append(mounts, m)
	}

	// Collect egress domains
	var egress []string
	egressFlags, _ := cmd.Flags().GetStringArray("allow-egress")
	for _, domain := range egressFlags {
		if domain != "" {
			egress = append(egress, domain)
		}
	}

	// Collect modules (for future provisioning integration)
	modules, _ := cmd.Flags().GetStringSlice("modules")
	_ = modules // consumed by provisioning system, not backend

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
