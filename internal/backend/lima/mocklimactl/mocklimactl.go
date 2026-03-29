// Package mocklimactl provides a digital twin of limactl for testing.
// This simulates Lima CLI behavior without requiring actual Lima installation.
package mocklimactl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// DataDir is where mock VM state is stored.
	DataDir = ".mocklimactl-data"
)

// VMState represents a VM's runtime state.
type VMState struct {
	Name       string    `json:"name"`
	Status     string    `json:"status"` // creating, running, stopped, error
	Config     VMConfig  `json:"config"`
	CreatedAt  time.Time `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	StoppedAt  *time.Time `json:"stopped_at,omitempty"`
	Snapshots  []Snapshot `json:"snapshots"`
}

// VMConfig represents the VM configuration stored by lima.
type VMConfig struct {
	CPUs      int    `json:"cpus"`
	Memory    string `json:"memory"`
	Disk      string `json:"disk"`
	BaseImage string `json:"base_image"`
}

// Snapshot represents a VM snapshot.
type Snapshot struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	VMState   VMState   `json:"vm_state"`
	Size      int64     `json:"size_bytes"`
}

var state = make(map[string]*VMState)

// MockRun simulates a limactl command.
func MockRun(args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("mocklimactl: command required")
	}

	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "create":
		return createVM(cmdArgs...)
	case "start":
		return startVM(cmdArgs...)
	case "stop":
		return stopVM(cmdArgs...)
	case "delete":
		return deleteVM(cmdArgs...)
	case "list":
		return listVMs(cmdArgs...)
	case "status":
		return statusVM(cmdArgs...)
	case "shell":
		return execShell(cmdArgs...)
	case "snapshot":
		return snapshotCmd(cmdArgs...)
	default:
		return "", fmt.Errorf("mocklimactl: unknown command %q", cmd)
	}
}

// createVM creates a new VM.
func createVM(args ...string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: mocklimactl create <name>")
	}
	name := args[0]

	mu.Lock()
	defer mu.Unlock()

	if _, exists := state[name]; exists {
		return "", fmt.Errorf("vm %q already exists", name)
	}

	vm := &VMState{
		Name:      name,
		Status:    "stopped", // Lima creates VMs in stopped state by default
		Config:    VMConfig{CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04"},
		CreatedAt: time.Now(),
		Snapshots: []Snapshot{},
	}

	state[name] = vm
	return fmt.Sprintf("Created VM %q", name), nil
}

// startVM starts a stopped VM.
func startVM(args ...string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: mocklimactl start <name>")
	}
	name := args[0]

	mu.Lock()
	defer mu.Unlock()

	vm, exists := state[name]
	if !exists {
		return "", fmt.Errorf("vm %q not found", name)
	}

	if vm.Status == "running" {
		return fmt.Sprintf("VM %q is already running", name), nil
	}

	if vm.Status != "stopped" {
		return "", fmt.Errorf("vm %q is not in a startable state (current: %s)", name, vm.Status)
	}

	now := time.Now()
	vm.Status = "running"
	vm.StartedAt = &now

	return fmt.Sprintf("Started VM %q", name), nil
}

// stopVM stops a running VM.
func stopVM(args ...string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: mocklimactl stop <name>")
	}
	name := args[0]

	mu.Lock()
	defer mu.Unlock()

	vm, exists := state[name]
	if !exists {
		return "", fmt.Errorf("vm %q not found", name)
	}

	if vm.Status == "stopped" {
		return fmt.Sprintf("VM %q is already stopped", name), nil
	}

	if vm.Status != "running" {
		return "", fmt.Errorf("vm %q is not running (current: %s)", name, vm.Status)
	}

	now := time.Now()
	vm.Status = "stopped"
	vm.StoppedAt = &now

	return fmt.Sprintf("Stopped VM %q", name), nil
}

// deleteVM deletes a VM.
func deleteVM(args ...string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: mocklimactl delete <name>")
	}
	name := args[0]

	mu.Lock()
	defer mu.Unlock()

	vm, exists := state[name]
	if !exists {
		return "", fmt.Errorf("vm %q not found", name)
	}

	if vm.Status == "running" {
		return "", fmt.Errorf("vm %q is running; stop it first", name)
	}

	delete(state, name)
	return fmt.Sprintf("Deleted VM %q", name), nil
}

// listVMs lists all VMs.
func listVMs(args ...string) (string, error) {
	mu.RLock()
	defer mu.RUnlock()

	if len(state) == 0 {
		return "", nil
	}

	var output []string
	for name, vm := range state {
		output = append(output, fmt.Sprintf("%s\t%s\t%s\t%d\t%s\t%s",
			name,
			vm.Status,
			vm.Config.BaseImage,
			vm.Config.CPUs,
			vm.Config.Memory,
			vm.Config.Disk,
		))
	}

	return strings.Join(output, "\n"), nil
}

// statusVM shows VM status.
func statusVM(args ...string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: mocklimactl status <name>")
	}
	name := args[0]

	mu.RLock()
	defer mu.RUnlock()

	vm, exists := state[name]
	if !exists {
		return "", fmt.Errorf("vm %q not found", name)
	}

	data, _ := json.MarshalIndent(vm, "", "  ")
	return string(data), nil
}

// execShell executes a shell command in a VM.
func execShell(args ...string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: mocklimactl shell <name> -- <command>")
	}

	// Find the -- separator
	sepIdx := -1
	for i, arg := range args {
		if arg == "--" {
			sepIdx = i
			break
		}
	}

	if sepIdx == -1 {
		return "", fmt.Errorf("missing -- separator")
	}

	name := args[0]
	command := args[sepIdx+1:]

	if len(command) == 0 {
		return "", fmt.Errorf("no command specified after --")
	}

	mu.RLock()
	defer mu.RUnlock()

	vm, exists := state[name]
	if !exists {
		return "", fmt.Errorf("vm %q not found", name)
	}

	if vm.Status != "running" {
		return "", fmt.Errorf("vm %q is not running", name)
	}

	// Simulate command execution
	fullCmd := strings.Join(command, " ")

	// For test purposes, return predictable output
	switch {
	case strings.Contains(fullCmd, "echo"):
		parts := strings.SplitN(fullCmd, " ", 2)
		if len(parts) > 1 {
			return strings.Trim(parts[1], `"'"`), nil
		}
		return "", nil
	case fullCmd == "true":
		return "", nil
	case fullCmd == "false":
		return "", fmt.Errorf("command exited with status 1")
	default:
		return fmt.Sprintf("executed: %s", fullCmd), nil
	}
}

// snapshotCmd handles snapshot subcommands.
func snapshotCmd(args ...string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: mocklimactl snapshot <create|list|restore|delete> <name> [tag]")
	}

	action := args[0]
	remaining := args[1:]

	switch action {
	case "create":
		return createSnapshot(remaining...)
	case "list":
		return listSnapshots(remaining...)
	case "restore":
		return restoreSnapshot(remaining...)
	case "delete":
		return deleteSnapshot(remaining...)
	default:
		return "", fmt.Errorf("unknown snapshot action: %s", action)
	}
}

// createSnapshot creates a VM snapshot.
func createSnapshot(args ...string) (string, error) {
	if len(args) < 2 {
		return "", fmt.Errorf("usage: mocklimactl snapshot create <name> <tag>")
	}
	name := args[0]
	tag := args[1]

	mu.Lock()
	defer mu.Unlock()

	vm, exists := state[name]
	if !exists {
		return "", fmt.Errorf("vm %q not found", name)
	}

	for _, snap := range vm.Snapshots {
		if snap.Name == tag {
			return "", fmt.Errorf("snapshot %q already exists for VM %q", tag, name)
		}
	}

	// Clone current VM state
	vmState := *vm
	snap := Snapshot{
		Name:      tag,
		CreatedAt: time.Now(),
		VMState:   vmState,
		Size:      1024 * 1024 * 1024, // 1GB mock size
	}

	vm.Snapshots = append(vm.Snapshots, snap)
	return fmt.Sprintf("Created snapshot %q for VM %q", tag, name), nil
}

// listSnapshots lists VM snapshots.
func listSnapshots(args ...string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: mocklimactl snapshot list <name>")
	}
	name := args[0]

	mu.RLock()
	defer mu.RUnlock()

	vm, exists := state[name]
	if !exists {
		return "", fmt.Errorf("vm %q not found", name)
	}

	if len(vm.Snapshots) == 0 {
		return "No snapshots", nil
	}

	var output []string
	for _, snap := range vm.Snapshots {
		output = append(output, fmt.Sprintf("%s\t%s\t%d",
			snap.Name,
			snap.CreatedAt.Format(time.RFC3339),
			snap.Size,
		))
	}

	return strings.Join(output, "\n"), nil
}

// restoreSnapshot restores a VM from a snapshot.
func restoreSnapshot(args ...string) (string, error) {
	if len(args) < 2 {
		return "", fmt.Errorf("usage: mocklimactl snapshot restore <name> <tag>")
	}
	name := args[0]
	tag := args[1]

	mu.Lock()
	defer mu.Unlock()

	vm, exists := state[name]
	if !exists {
		return "", fmt.Errorf("vm %q not found", name)
	}

	var snap *Snapshot
	for i := range vm.Snapshots {
		if vm.Snapshots[i].Name == tag {
			snap = &vm.Snapshots[i]
			break
		}
	}

	if snap == nil {
		return "", fmt.Errorf("snapshot %q not found for VM %q", tag, name)
	}

	// Restore VM state from snapshot
	vm.Status = snap.VMState.Status
	vm.StartedAt = snap.VMState.StartedAt
	vm.StoppedAt = snap.VMState.StoppedAt

	return fmt.Sprintf("Restored VM %q from snapshot %q", name, tag), nil
}

// deleteSnapshot deletes a VM snapshot.
func deleteSnapshot(args ...string) (string, error) {
	if len(args) < 2 {
		return "", fmt.Errorf("usage: mocklimactl snapshot delete <name> <tag>")
	}
	name := args[0]
	tag := args[1]

	mu.Lock()
	defer mu.Unlock()

	vm, exists := state[name]
	if !exists {
		return "", fmt.Errorf("vm %q not found", name)
	}

	for i, snap := range vm.Snapshots {
		if snap.Name == tag {
			vm.Snapshots = append(vm.Snapshots[:i], vm.Snapshots[i+1:]...)
			return fmt.Sprintf("Deleted snapshot %q for VM %q", tag, name), nil
		}
	}

	return "", fmt.Errorf("snapshot %q not found for VM %q", tag, name)
}

// Save persists VM state to disk for testing persistence.
func Save() error {
	mu.RLock()
	defer mu.RUnlock()

	if err := os.MkdirAll(DataDir, 0755); err != nil {
		return err
	}

	for name, vm := range state {
		data, err := json.MarshalIndent(vm, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(DataDir, name+".json"), data, 0644); err != nil {
			return err
		}
	}
	return nil
}

// Load loads VM state from disk.
func Load() error {
	mu.Lock()
	defer mu.Unlock()

	entries, err := os.ReadDir(DataDir)
	if os.IsNotExist(err) {
		return nil // No state file is OK
	}
	if err != nil {
		return err
	}

	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".json") {
			path := filepath.Join(DataDir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			var vm VMState
			if err := json.Unmarshal(data, &vm); err != nil {
				return err
			}
			state[vm.Name] = &vm
		}
	}
	return nil
}

// Reset clears all VM state (for testing cleanup).
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	state = make(map[string]*VMState)
	os.RemoveAll(DataDir)
}

// mu protects concurrent access to state.
var mu sync.RWMutex
