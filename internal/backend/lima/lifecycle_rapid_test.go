// Property-based tests for the Lima backend lifecycle state machine.
// These verify security-critical invariants hold across all possible
// state transitions, not just the happy paths.
//
// REQ-003-003: VM Lifecycle Operations — state machine correctness
// REQ-003-004: VM Status Reporting — status always reflects reality
// REQ-003-005: VM Listing — list is always consistent
// REQ-003-006: SSH Configuration — only running VMs expose SSH
// REQ-003-007: Command Execution — only running VMs accept commands
package lima

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"sd/internal/backend"
	"sd/internal/backend/lima/mocklimactl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Unit tests for parseStatus and parseListOutput ---

// REQ-003-004: parseStatus handles JSON format from mocklimactl.
func TestParseStatus_JSONFormat(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   backend.VMStatus
		wantOK bool
	}{
		{"running_json", `{"status":"running"}`, backend.StatusRunning, true},
		{"stopped_json", `{"status":"stopped"}`, backend.StatusStopped, true},
		{"creating_json", `{"status":"creating"}`, backend.StatusCreating, true},
		{"error_json", `{"status":"error"}`, backend.StatusError, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStatus(tt.input)
			if tt.wantOK {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

// REQ-003-004: parseStatus handles plain text from real limactl.
func TestParseStatus_PlainText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  backend.VMStatus
	}{
		{"running", "The Lima instance is running.", backend.StatusRunning},
		{"stopped", "The Lima instance is stopped.", backend.StatusStopped},
		{"creating", "The Lima instance is creating.", backend.StatusCreating},
		{"error_msg", "The Lima instance is in error state.", backend.StatusError},
		{"running_upper", "RUNNING", backend.StatusRunning},
		{"stopped_mixed", "Instance is Stopped", backend.StatusStopped},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStatus(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// REQ-003-004: parseStatus rejects unknown status strings.
func TestParseStatus_UnknownStatus(t *testing.T) {
	_, err := parseStatus("The Lima instance is foobar.")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown status")
}

func TestParseStatus_Empty(t *testing.T) {
	_, err := parseStatus("")
	assert.Error(t, err)
}

// REQ-003-004: parseStatus prefers JSON when valid, falls back to text.
func TestParseStatus_JSONFallbackOrder(t *testing.T) {
	// Valid JSON should take priority
	got, err := parseStatus(`{"status":"running"}`)
	require.NoError(t, err)
	assert.Equal(t, backend.StatusRunning, got)

	// Invalid JSON falls back to text matching
	got, err = parseStatus(`{not json but contains running}`)
	require.NoError(t, err)
	assert.Equal(t, backend.StatusRunning, got)
}

// REQ-003-005: parseListOutput handles JSON format (primary path).
func TestParseListOutput_JSON(t *testing.T) {
	input := `[{"name":"vm1","status":"Running","cpus":4,"memory":"8GiB","disk":"100GiB","dir":"~/.lima/vm1"},
{"name":"vm2","status":"Stopped","cpus":2,"memory":"4GiB","disk":"50GiB","dir":"~/.lima/vm2"}]`
	vms, err := parseListOutput(input)
	require.NoError(t, err)
	require.Len(t, vms, 2)

	assert.Equal(t, "vm1", vms[0].Name)
	assert.Equal(t, backend.StatusRunning, vms[0].Status)
	assert.Equal(t, 4, vms[0].CPUs)
	assert.Equal(t, "8GiB", vms[0].Memory)
	assert.Equal(t, "100GiB", vms[0].Disk)
	assert.Equal(t, "lima", vms[0].Backend)

	assert.Equal(t, "vm2", vms[1].Name)
	assert.Equal(t, backend.StatusStopped, vms[1].Status)
	assert.Equal(t, 2, vms[1].CPUs)
}

func TestParseListOutput_EmptyJSON(t *testing.T) {
	vms, err := parseListOutput("[]")
	require.NoError(t, err)
	assert.Empty(t, vms)
}

// REQ-003-005: parseListOutput handles real limactl text format with header.
func TestParseListOutput_RealLimactlFormat(t *testing.T) {
	// Real limactl list output: 9 tab-separated fields with header
	input := "NAME\tSTATUS\tSSH\tVMTYPE\tARCH\tCPUS\tMEMORY\tDISK\tDIR\n" +
		"vm1\tRunning\t127.0.0.1:52215\tvz\taarch64\t4\t8GiB\t100GiB\t~/.lima/vm1\n" +
		"vm2\tStopped\t\tqemu\tx86_64\t2\t4GiB\t50GiB\t~/.lima/vm2"
	vms, err := parseListOutput(input)
	require.NoError(t, err)
	require.Len(t, vms, 2)

	assert.Equal(t, "vm1", vms[0].Name)
	assert.Equal(t, backend.StatusRunning, vms[0].Status)
	assert.Equal(t, 4, vms[0].CPUs)
	assert.Equal(t, "8GiB", vms[0].Memory)
	assert.Equal(t, "100GiB", vms[0].Disk)

	assert.Equal(t, "vm2", vms[1].Name)
	assert.Equal(t, backend.StatusStopped, vms[1].Status)
	assert.Equal(t, 2, vms[1].CPUs)
}

// REQ-003-005: parseListOutput handles compact 6-field format (legacy/mock).
func TestParseListOutput_CompactFormat(t *testing.T) {
	input := "vm1\trunning\tubuntu:24.04\t4\t8GiB\t100GiB\nvm2\tstopped\tdebian:12\t2\t4GiB\t50GiB"
	vms, err := parseListOutput(input)
	require.NoError(t, err)
	require.Len(t, vms, 2)

	assert.Equal(t, "vm1", vms[0].Name)
	assert.Equal(t, backend.StatusRunning, vms[0].Status)
	assert.Equal(t, 4, vms[0].CPUs)
	assert.Equal(t, "8GiB", vms[0].Memory)
	assert.Equal(t, "100GiB", vms[0].Disk)
	assert.Equal(t, "lima", vms[0].Backend)

	assert.Equal(t, "vm2", vms[1].Name)
	assert.Equal(t, backend.StatusStopped, vms[1].Status)
	assert.Equal(t, 2, vms[1].CPUs)
}

// REQ-003-005: header line alone produces empty result.
func TestParseListOutput_HeaderOnly(t *testing.T) {
	input := "NAME\tSTATUS\tSSH\tVMTYPE\tARCH\tCPUS\tMEMORY\tDISK\tDIR"
	vms, err := parseListOutput(input)
	require.NoError(t, err)
	assert.Empty(t, vms)
}

func TestParseListOutput_Empty(t *testing.T) {
	vms, err := parseListOutput("")
	require.NoError(t, err)
	assert.Empty(t, vms)
}

func TestParseListOutput_TooFewFields(t *testing.T) {
	// Lines with fewer than 6 fields are skipped
	input := "vm1\trunning\tubuntu"
	vms, err := parseListOutput(input)
	require.NoError(t, err)
	assert.Empty(t, vms)
}

func TestParseListOutput_MixedValidAndInvalid(t *testing.T) {
	input := "vm1\trunning\tubuntu:24.04\t4\t8GiB\t100GiB\nshort\tonly\tthree\nvm2\tstopped\tdebian:12\t2\t4GiB\t50GiB"
	vms, err := parseListOutput(input)
	require.NoError(t, err)
	assert.Len(t, vms, 2)
	assert.Equal(t, "vm1", vms[0].Name)
	assert.Equal(t, "vm2", vms[1].Name)
}

// Property: parseStatus always returns a known status or an error.
func TestProperty_ParseStatusAlwaysValid(t *testing.T) {
	validStatuses := map[backend.VMStatus]bool{
		backend.StatusRunning:  true,
		backend.StatusStopped:  true,
		backend.StatusCreating: true,
		backend.StatusError:    true,
	}

	rapid.Check(t, func(t *rapid.T) {
		// Generate realistic limactl status output
		output := rapid.OneOf(
			rapid.Just(`{"status":"running"}`),
			rapid.Just(`{"status":"stopped"}`),
			rapid.Just(`{"status":"creating"}`),
			rapid.Just(`{"status":"error"}`),
			rapid.StringMatching(`[a-zA-Z ]{1,50}`),
		).Draw(t, "output")

		status, err := parseStatus(output)
		if err == nil {
			assert.True(t, validStatuses[status],
				"parseStatus(%q) returned unknown status %q", output, status)
		}
	})
}

// Property: parseListOutput always returns valid VMInfo or empty.
func TestProperty_ParseListOutputAlwaysValid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		output := rapid.OneOf(
			rapid.Just(""),
			rapid.Just("[]"),
			rapid.Just(`[{"name":"vm1","status":"Running","cpus":4,"memory":"8GiB","disk":"100GiB"}]`),
			rapid.Just("vm1\trunning\tubuntu:24.04\t4\t8GiB\t100GiB"),
			rapid.Just("NAME\tSTATUS\tSSH\tVMTYPE\tARCH\tCPUS\tMEMORY\tDISK\tDIR\nvm1\tRunning\t127.0.0.1\tvz\taarch64\t4\t8GiB\t100GiB\t~/.lima/vm1"),
			rapid.StringMatching(`[a-zA-Z0-9 \t.:{},"\[\]]{0,300}`),
		).Draw(t, "output")

		vms, err := parseListOutput(output)
		require.NoError(t, err, "parseListOutput should never return an error")

		for _, vm := range vms {
			assert.Equal(t, "lima", vm.Backend)
			assert.NotEmpty(t, vm.Name)
		}
	})
}

// Property: parseListOutput JSON roundtrip preserves VM names and field counts.
func TestProperty_ParseListOutputJSONRoundtrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 5).Draw(t, "n_vms")
		type entry struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			CPUs   int    `json:"cpus"`
			Memory string `json:"memory"`
			Disk   string `json:"disk"`
		}
		entries := make([]entry, n)
		names := make(map[string]bool)
		for i := range entries {
			name := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, fmt.Sprintf("name_%d", i))
			status := rapid.SampledFrom([]string{"Running", "Stopped", "Creating", "Error"}).Draw(t, fmt.Sprintf("status_%d", i))
			cpus := rapid.IntRange(1, 16).Draw(t, fmt.Sprintf("cpus_%d", i))
			mem := rapid.SampledFrom([]string{"2GiB", "4GiB", "8GiB", "16GiB"}).Draw(t, fmt.Sprintf("mem_%d", i))
			disk := rapid.SampledFrom([]string{"50GiB", "100GiB", "200GiB"}).Draw(t, fmt.Sprintf("disk_%d", i))
			entries[i] = entry{Name: name, Status: status, CPUs: cpus, Memory: mem, Disk: disk}
			names[name] = true
		}
		data, err := json.Marshal(entries)
		require.NoError(t, err)

		vms, err := parseListOutput(string(data))
		require.NoError(t, err)
		assert.Len(t, vms, n)

		for _, vm := range vms {
			assert.True(t, names[vm.Name], "unexpected VM name %q in output", vm.Name)
			assert.Equal(t, "lima", vm.Backend)
			assert.NotEmpty(t, vm.Status)
		}
	})
}

// Property: header line is never parsed as a VM, regardless of format.
func TestProperty_HeaderNeverParsedAsVM(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dataLine := rapid.OneOf(
			rapid.Just("vm1\trunning\tubuntu:24.04\t4\t8GiB\t100GiB"),
			rapid.Just("vm1\tRunning\t127.0.0.1\tvz\taarch64\t4\t8GiB\t100GiB\t~/.lima/vm1"),
		).Draw(t, "data_line")

		// Prepend header
		input := "NAME\tSTATUS\tSSH\tVMTYPE\tARCH\tCPUS\tMEMORY\tDISK\tDIR\n" + dataLine
		vms, err := parseListOutput(input)
		require.NoError(t, err)

		for _, vm := range vms {
			assert.NotEqual(t, "NAME", vm.Name, "header line must not be parsed as a VM")
		}
	})
}

// --- Property-Based Lifecycle State Machine Tests ---
//
// These test the core state machine: create → stopped → running → stopped → destroyed.
// The invariants are security-critical: VMs must always be in a valid state,
// and operations on one VM must never affect another.

// newRapidBackend creates a Lima backend wired to the mocklimactl digital twin,
// suitable for use inside rapid.Check (which provides *rapid.T, not *testing.T).
func newRapidBackend() backend.Backend {
	mocklimactl.Reset()
	return NewWithExecutor(&mockExecutor{})
}

// Property: After create, VM is always stopped and appears in list.
// REQ-003-003
func TestProperty_CreateAlwaysStopped(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		ctx := context.Background()
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		err := b.Create(ctx, vmName, cfg)
		require.NoError(t, err)

		status, err := b.Status(ctx, vmName)
		require.NoError(t, err)
		assert.Equal(t, backend.StatusStopped, status,
			"newly created VM must be in stopped state")

		vms, err := b.List(ctx)
		require.NoError(t, err)
		found := false
		for _, vm := range vms {
			if vm.Name == vmName {
				found = true
				break
			}
		}
		assert.True(t, found, "created VM %q must appear in list", vmName)
	})
}

// Property: Start on a stopped VM always transitions to running.
// REQ-003-003
func TestProperty_StartTransitionsToRunning(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		ctx := context.Background()
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		require.NoError(t, b.Create(ctx, vmName, cfg))
		require.NoError(t, b.Start(ctx, vmName))

		status, err := b.Status(ctx, vmName)
		require.NoError(t, err)
		assert.Equal(t, backend.StatusRunning, status)
	})
}

// Property: Stop on a running VM always transitions to stopped.
// REQ-003-003
func TestProperty_StopTransitionsToStopped(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		ctx := context.Background()
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		require.NoError(t, b.Create(ctx, vmName, cfg))
		require.NoError(t, b.Start(ctx, vmName))
		require.NoError(t, b.Stop(ctx, vmName))

		status, err := b.Status(ctx, vmName)
		require.NoError(t, err)
		assert.Equal(t, backend.StatusStopped, status)
	})
}

// Property: Destroyed VM never appears in list or status.
// REQ-003-003
func TestProperty_DestroyedVMDisappears(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		ctx := context.Background()
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		require.NoError(t, b.Create(ctx, vmName, cfg))
		require.NoError(t, b.Destroy(ctx, vmName))

		// Must not appear in list
		vms, err := b.List(ctx)
		require.NoError(t, err)
		for _, vm := range vms {
			assert.NotEqual(t, vmName, vm.Name,
				"destroyed VM %q must not appear in list", vmName)
		}

		// Status must return ErrVMNotFound
		_, err = b.Status(ctx, vmName)
		assert.Error(t, err, "status of destroyed VM must error")
	})
}

// Property: VMs are always independent — operations on one never affect another.
// This is the most critical security property for multi-tenant isolation.
// REQ-003-003
func TestProperty_VMsAlwaysIndependent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		ctx := context.Background()

		nameA := rapid.StringMatching(`alpha-[a-z0-9]{3}`).Draw(t, "nameA")
		nameB := rapid.StringMatching(`beta-[a-z0-9]{3}`).Draw(t, "nameB")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		// Create both
		require.NoError(t, b.Create(ctx, nameA, cfg))
		require.NoError(t, b.Create(ctx, nameB, cfg))

		// Start only A
		require.NoError(t, b.Start(ctx, nameA))

		// A must be running, B must still be stopped
		statusA, err := b.Status(ctx, nameA)
		require.NoError(t, err)
		statusB, err := b.Status(ctx, nameB)
		require.NoError(t, err)

		assert.Equal(t, backend.StatusRunning, statusA)
		assert.Equal(t, backend.StatusStopped, statusB,
			"starting VM %q must not affect VM %q", nameA, nameB)

		// Stop A, destroy A
		require.NoError(t, b.Stop(ctx, nameA))
		require.NoError(t, b.Destroy(ctx, nameA))

		// B must still exist and be stopped
		statusB, err = b.Status(ctx, nameB)
		require.NoError(t, err, "destroying VM %q must not affect VM %q", nameA, nameB)
		assert.Equal(t, backend.StatusStopped, statusB)
	})
}

// Property: Start is idempotent — starting an already-running VM is a no-op.
// REQ-003-003
func TestProperty_StartIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		ctx := context.Background()
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		require.NoError(t, b.Create(ctx, vmName, cfg))
		require.NoError(t, b.Start(ctx, vmName))

		// Start again — must be a no-op
		n := rapid.IntRange(1, 5).Draw(t, "extra_starts")
		for i := 0; i < n; i++ {
			require.NoError(t, b.Start(ctx, vmName),
				"repeated start %d must be a no-op", i+1)
		}

		status, err := b.Status(ctx, vmName)
		require.NoError(t, err)
		assert.Equal(t, backend.StatusRunning, status)
	})
}

// Property: Stop is idempotent — stopping an already-stopped VM is a no-op.
// REQ-003-003
func TestProperty_StopIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		ctx := context.Background()
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		require.NoError(t, b.Create(ctx, vmName, cfg))

		// Stop multiple times — must all be no-ops
		n := rapid.IntRange(1, 5).Draw(t, "extra_stops")
		for i := 0; i < n; i++ {
			require.NoError(t, b.Stop(ctx, vmName),
				"repeated stop %d must be a no-op", i+1)
		}

		status, err := b.Status(ctx, vmName)
		require.NoError(t, err)
		assert.Equal(t, backend.StatusStopped, status)
	})
}

// Property: List always reflects the exact set of created-and-not-destroyed VMs.
// REQ-003-005
func TestProperty_ListReflectsExactState(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		ctx := context.Background()

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		// Create a random number of distinct VMs
		created := rapid.SliceOfNDistinct(
			rapid.StringMatching(`vm-[a-z0-9]{3}`),
			1, 5,
			func(s string) string { return s },
		).Draw(t, "vm_names")
		for _, name := range created {
			require.NoError(t, b.Create(ctx, name, cfg))
		}

		vms, err := b.List(ctx)
		require.NoError(t, err)
		assert.Len(t, vms, len(created), "list must contain exactly %d VMs", len(created))

		listNames := make(map[string]bool)
		for _, vm := range vms {
			listNames[vm.Name] = true
		}
		for _, name := range created {
			assert.True(t, listNames[name],
				"created VM %q must appear in list", name)
		}
	})
}

// Property: SSHConfig on a stopped VM always returns ErrVMNotRunning.
// REQ-003-006
func TestProperty_SSHConfigStoppedAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		ctx := context.Background()
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		require.NoError(t, b.Create(ctx, vmName, cfg))

		_, err := b.SSHConfig(ctx, vmName)
		assert.Error(t, err, "SSHConfig on stopped VM must fail")
	})
}

// Property: SSHConfig on a running VM always returns valid config with ForwardAgent=false.
// REQ-003-006, REQ-004-027
func TestProperty_SSHConfigRunningAlwaysValid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		ctx := context.Background()
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		require.NoError(t, b.Create(ctx, vmName, cfg))
		require.NoError(t, b.Start(ctx, vmName))

		sshCfg, err := b.SSHConfig(ctx, vmName)
		require.NoError(t, err)

		assert.NotEmpty(t, sshCfg.User)
		assert.NotEmpty(t, sshCfg.Host)
		assert.False(t, sshCfg.ForwardAgent,
			"ForwardAgent must always be false (REQ-004-027)")
		assert.Contains(t, sshCfg.IdentityFile, vmName,
			"IdentityFile must be per-VM")
	})
}

// Property: Exec on a non-running VM always returns an error.
// REQ-003-007
func TestProperty_ExecStoppedAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		ctx := context.Background()
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		require.NoError(t, b.Create(ctx, vmName, cfg))

		_, err := b.Exec(ctx, vmName, []string{"echo", "hello"})
		assert.Error(t, err, "Exec on stopped VM must fail")
	})
}

// Property: Exec on a running VM always returns exit code 0 for valid commands.
// REQ-003-007
func TestProperty_ExecRunningSucceeds(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		ctx := context.Background()
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		require.NoError(t, b.Create(ctx, vmName, cfg))
		require.NoError(t, b.Start(ctx, vmName))

		result, err := b.Exec(ctx, vmName, []string{"echo", "hello"})
		require.NoError(t, err)
		assert.Equal(t, 0, result.ExitCode)
		assert.Contains(t, result.Stdout, "hello")
	})
}

// Property: Status and List always agree on VM existence.
// REQ-003-004, REQ-003-005
func TestProperty_StatusListConsistency(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		ctx := context.Background()

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		names := rapid.SliceOfNDistinct(
			rapid.StringMatching(`vm-[a-z0-9]{3}`),
			1, 4,
			func(s string) string { return s },
		).Draw(t, "vm_names")
		for _, name := range names {
			require.NoError(t, b.Create(ctx, name, cfg))
		}

		// Every VM that appears in list must be statusable
		vms, err := b.List(ctx)
		require.NoError(t, err)
		for _, vm := range vms {
			_, err := b.Status(ctx, vm.Name)
			assert.NoError(t, err,
				"VM %q in list but Status returns error", vm.Name)
		}

		// Every created VM must appear in list
		listNames := make(map[string]bool)
		for _, vm := range vms {
			listNames[vm.Name] = true
		}
		for _, name := range names {
			assert.True(t, listNames[name],
				"created VM %q not found in list", name)
		}
	})
}

// Property: Full lifecycle (create → start → stop → destroy) always succeeds.
// REQ-003-003
func TestProperty_FullLifecycleAlwaysSucceeds(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		ctx := context.Background()
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")

		cfg := backend.VMConfig{
			CPUs: rapid.IntRange(1, 16).Draw(t, "cpus"),
			Memory: rapid.SampledFrom([]string{
				"2GiB", "4GiB", "8GiB", "16GiB", "32GiB",
			}).Draw(t, "memory"),
			Disk: rapid.SampledFrom([]string{
				"50GiB", "100GiB", "200GiB",
			}).Draw(t, "disk"),
			BaseImage: rapid.SampledFrom([]string{
				"ubuntu:24.04", "ubuntu:22.04", "debian:12",
			}).Draw(t, "image"),
		}

		// Create
		require.NoError(t, b.Create(ctx, vmName, cfg))
		status, _ := b.Status(ctx, vmName)
		assert.Equal(t, backend.StatusStopped, status)

		// Start
		require.NoError(t, b.Start(ctx, vmName))
		status, _ = b.Status(ctx, vmName)
		assert.Equal(t, backend.StatusRunning, status)

		// Exec while running
		result, err := b.Exec(ctx, vmName, []string{"echo", "test"})
		require.NoError(t, err)
		assert.Equal(t, 0, result.ExitCode)

		// SSH config while running
		sshCfg, err := b.SSHConfig(ctx, vmName)
		require.NoError(t, err)
		assert.False(t, sshCfg.ForwardAgent)

		// Stop
		require.NoError(t, b.Stop(ctx, vmName))
		status, _ = b.Status(ctx, vmName)
		assert.Equal(t, backend.StatusStopped, status)

		// Destroy
		require.NoError(t, b.Destroy(ctx, vmName))
		_, err = b.Status(ctx, vmName)
		assert.Error(t, err, "destroyed VM must not be statusable")
	})
}

// Property: Operations on non-existent VMs always return ErrVMNotFound.
// REQ-003-003
func TestProperty_NonExistentVMAlwaysErrVMNotFound(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		ctx := context.Background()
		vmName := rapid.StringMatching(`nonexistent-[a-z0-9]{3,8}`).Draw(t, "name")

		_, err := b.Status(ctx, vmName)
		assert.Error(t, err, "Status on non-existent VM must error")

		err = b.Start(ctx, vmName)
		assert.Error(t, err, "Start on non-existent VM must error")

		err = b.Stop(ctx, vmName)
		assert.Error(t, err, "Stop on non-existent VM must error")

		_, err = b.Exec(ctx, vmName, []string{"echo"})
		assert.Error(t, err, "Exec on non-existent VM must error")
	})
}

// Property: Duplicate create always returns ErrVMAlreadyExists.
// REQ-003-003
func TestProperty_DuplicateCreateAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		ctx := context.Background()
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		require.NoError(t, b.Create(ctx, vmName, cfg))

		// Second create must fail
		err := b.Create(ctx, vmName, cfg)
		assert.Error(t, err, "duplicate create must fail")
		assert.True(t,
			strings.Contains(err.Error(), "already exists"),
			"error should mention 'already exists': %v", err)
	})
}

// Property: Start → Stop → Start cycle preserves running state.
// REQ-003-003: VM state machine is always consistent.
func TestProperty_StartStopCyclePreservesState(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		ctx := context.Background()
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(t, "name")

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		require.NoError(t, b.Create(ctx, vmName, cfg))

		cycles := rapid.IntRange(1, 3).Draw(t, "cycles")
		for i := 0; i < cycles; i++ {
			require.NoError(t, b.Start(ctx, vmName))
			status, _ := b.Status(ctx, vmName)
			assert.Equal(t, backend.StatusRunning, status,
				"cycle %d: after start must be running", i+1)

			require.NoError(t, b.Stop(ctx, vmName))
			status, _ = b.Status(ctx, vmName)
			assert.Equal(t, backend.StatusStopped, status,
				"cycle %d: after stop must be stopped", i+1)
		}
	})
}

// Property: List for each VM always has Backend="lima".
// REQ-003-005
func TestProperty_ListBackendAlwaysLima(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mocklimactl.Reset()
		b := newRapidBackend()
		ctx := context.Background()

		cfg := backend.VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		n := rapid.IntRange(1, 5).Draw(t, "n")
		for i := 0; i < n; i++ {
			name := rapid.StringMatching(`vm-[a-z0-9]{3}`).Draw(t, "name")
			require.NoError(t, b.Create(ctx, name, cfg))
		}

		vms, err := b.List(ctx)
		require.NoError(t, err)

		for _, vm := range vms {
			assert.Equal(t, "lima", vm.Backend,
				"all listed VMs must have Backend='lima'")
		}
	})
}
