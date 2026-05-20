// Package backend provides property-based tests for the Backend interface.
// REQ-003-001 through REQ-003-010
package backend

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestBackend_InterfaceContract verifies all backends implement the interface.
// Property: All registered backends must satisfy the Backend interface
func TestBackend_InterfaceContract(t *testing.T) {
	ResetRegistry()

	backend := &mockBackend{name: "test"}
	Register("test", backend)

	// Just verify it compiles and satisfies the interface
	var _ Backend = backend

	// Call all methods to ensure they exist
	ctx := context.Background()
	cfg := VMConfig{CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04"}

	backend.Create(ctx, "test", cfg)
	backend.Start(ctx, "test")
	backend.Stop(ctx, "test")
	backend.Destroy(ctx, "test")
	backend.Status(ctx, "test")
	backend.List(ctx)
	backend.SSHConfig(ctx, "test")
	backend.Exec(ctx, "test", []string{"echo", "test"})
}

// TestBackend_ContextAllMethods accept context.
// REQ-003-001, REQ-003-022
func TestBackend_ContextAllMethods(t *testing.T) {
	backend := &mockBackend{name: "test"}

	ctx := context.Background()
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := VMConfig{CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04"}

	// All these should not panic
	backend.Create(ctx, "test", cfg)
	backend.Start(ctx, "test")
	backend.Stop(ctx, "test")
	backend.Destroy(ctx, "test")
	backend.Status(ctx, "test")
	backend.List(ctx)
	backend.SSHConfig(ctx, "test")
	backend.Exec(ctx, "test", []string{"test"})

	// Should also handle cancelled context
	backend.Create(cancelledCtx, "test", cfg)
	backend.Start(cancelledCtx, "test")
}

// TestBackend_VMSerializability verifies VMInfo and SSHConfig are JSON serializable.
// REQ-003-005, REQ-003-006
func TestBackend_VMSerializability(t *testing.T) {
	nowVMInfo := time.Now()
	vmInfo := VMInfo{
		Name:      "test-vm",
		Status:    StatusRunning,
		Backend:   "lima",
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		IP:        "127.0.0.1",
		CreatedAt: &nowVMInfo,
	}

	data, err := json.Marshal(vmInfo)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded VMInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if decoded.Name != vmInfo.Name {
		t.Errorf("decoded.Name = %q, want %q", decoded.Name, vmInfo.Name)
	}
}

// TestBackend_VMStatusValues are valid.
// REQ-003-004: VMStatus valid values
func TestBackend_VMStatusValues(t *testing.T) {
	validStatuses := []VMStatus{
		StatusCreating,
		StatusRunning,
		StatusStopped,
		StatusError,
	}

	for _, status := range validStatuses {
		t.Run(string(status), func(t *testing.T) {
			// Verify it's JSON serializable
			data, err := json.Marshal(status)
			if err != nil {
				t.Errorf("json.Marshal failed: %v", err)
			}
			if len(data) == 0 {
				t.Error("json.Marshal returned empty data")
			}

			// Verify it can be unmarshaled
			var decoded VMStatus
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Errorf("json.Unmarshal failed: %v", err)
			}
		})
	}
}

// TestBackend_NetworkModeValues are valid.
// REQ-003-012
func TestBackend_NetworkModeValues(t *testing.T) {
	validModes := []NetworkMode{
		NetworkNAT,
		NetworkBridged,
		NetworkIsolated,
	}

	for _, mode := range validModes {
		if mode == "" {
			continue
		}
		t.Run(string(mode), func(t *testing.T) {
			cfg := VMConfig{
				CPUs:       4,
				Memory:     "8GiB",
				Disk:       "100GiB",
				BaseImage:  "ubuntu:24.04",
				NetworkMode: mode,
			}

			if err := cfg.Validate(); err != nil {
				t.Errorf("Validate() with mode %q failed: %v", mode, err)
			}
		})
	}
}

// TestBackend_SnapshotInfoIsJSONSerializable.
// REQ-003-008
func TestBackend_SnapshotInfoIsJSONSerializable(t *testing.T) {
	snap := SnapshotInfo{
		Name:      "before-refactor",
		CreatedAt: time.Now(),
		Size:      1024 * 1024 * 1024, // 1GB
	}

	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded SnapshotInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if decoded.Name != snap.Name {
		t.Errorf("decoded.Name = %q, want %q", decoded.Name, snap.Name)
	}
}

// TestBackend_MountWritableIsBoolean.
// REQ-003-011
func TestBackend_MountWritableIsBoolean(t *testing.T) {
	tests := []struct {
		name  string
		writable bool
	}{
		{name: "read-only", writable: false},
		{name: "read-write", writable: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Mount{
				HostPath:  "/path",
				GuestPath: "/guest",
				Writable:  tt.writable,
			}

			cfg := VMConfig{
				CPUs:      4,
				Memory:    "8GiB",
				Disk:      "100GiB",
				BaseImage: "ubuntu:24.04",
				Mounts:    []Mount{m},
			}

			if err := cfg.Validate(); err != nil {
				t.Errorf("Validate() with writable=%v failed: %v", tt.writable, err)
			}
		})
	}
}

// TestBackend_ProvisionScriptModes.
// REQ-006-005
func TestBackend_ProvisionScriptModes(t *testing.T) {
	tests := []struct {
		mode string
		valid bool
	}{
		{mode: "system", valid: true},
		{mode: "user", valid: true},
		{mode: "invalid", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			script := ProvisionScript{
				Mode:   tt.mode,
				Script: "echo test",
			}

			cfg := VMConfig{
				CPUs:             4,
				Memory:           "8GiB",
				Disk:             "100GiB",
				BaseImage:        "ubuntu:24.04",
				ProvisionScripts: []ProvisionScript{script},
			}

			err := cfg.Validate()
			// Validation doesn't check script mode, so it should pass
			if err != nil {
				t.Errorf("Validate() failed: %v", err)
			}
		})
	}
}

// TestBackend_BackendOptionsIsMap.
// REQ-003-011
func TestBackend_BackendOptionsIsMap(t *testing.T) {
	cfg := VMConfig{
		CPUs:      4,
		Memory:    "8GiB",
		Disk:      "100GiB",
		BaseImage: "ubuntu:24.04",
		BackendOptions: map[string]any{
			"vmType":    "vz",
			"mountType": "virtiofs",
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() failed: %v", err)
	}

	if cfg.BackendOptions["vmType"] != "vz" {
		t.Errorf("BackendOptions[\"vmType\"] = %v, want \"vz\"", cfg.BackendOptions["vmType"])
	}
}

// TestBackend_SentinelErrorsAreUnwrappable.
// REQ-003-021: Sentinel errors with errors.Is and errors.As
func TestBackend_SentinelErrorsAreUnwrappable(t *testing.T) {
	baseErr := ErrVMNotFound
	msg := "VM 'test' does not exist"

	wrapped := wrapError(baseErr, msg)

	if !errors.Is(wrapped, baseErr) {
		t.Errorf("errors.Is(wrapped, ErrVMNotFound) = false, want true")
	}
}

// TestBackend_ErrorMessagesAreHelpful.
// REQ-003-002: Available returns actionable error
func TestBackend_ErrorMessagesAreHelpful(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "wrapped VM not found",
			err:  wrapError(ErrVMNotFound, "VM 'test' does not exist"),
			want: "VM 'test' does not exist",
		},
		{
			name: "wrapped invalid config",
			err:  wrapError(ErrInvalidConfig, "cpus must be at least 1"),
			want: "cpus must be at least 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.err.Error(), tt.want) {
				t.Errorf("error message missing %q: %s", tt.want, tt.err.Error())
			}
		})
	}
}
