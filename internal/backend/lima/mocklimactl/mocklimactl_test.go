// Package mocklimactl tests verify the digital twin accurately models
// real limactl behavior. These tests are critical because all Lima
// backend integration tests depend on this mock's correctness.
package mocklimactl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// helper: create a VM and return its name (uses the mock directly).
func createTestVM(name string) error {
	_, err := MockRun([]string{"create", name})
	return err
}

// helper: create and start a VM.
func createAndStartVM(name string) error {
	if err := createTestVM(name); err != nil {
		return err
	}
	_, err := MockRun([]string{"start", name})
	return err
}

// --- Unit Tests: Command Dispatch ---

func TestMockRun_EmptyArgs(t *testing.T) {
	_, err := MockRun([]string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "command required")
}

func TestMockRun_UnknownCommand(t *testing.T) {
	_, err := MockRun([]string{"bogus"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

// --- VM Lifecycle State Machine ---

func TestCreateVM_Basic(t *testing.T) {
	Reset()
	defer Reset()

	out, err := MockRun([]string{"create", "testvm"})
	require.NoError(t, err)
	assert.Contains(t, out, "Created VM")

	vms := GetVMs()
	require.Contains(t, vms, "testvm")
	assert.Equal(t, "stopped", vms["testvm"].Status)
	assert.False(t, vms["testvm"].CreatedAt.IsZero())
}

func TestCreateVM_NoArgs(t *testing.T) {
	Reset()
	defer Reset()

	_, err := MockRun([]string{"create"})
	assert.Error(t, err)
}

func TestCreateVM_Duplicate(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createTestVM("dup"))
	err := createTestVM("dup")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestStartVM_Basic(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createTestVM("testvm"))

	out, err := MockRun([]string{"start", "testvm"})
	require.NoError(t, err)
	assert.Contains(t, out, "Started VM")

	vms := GetVMs()
	assert.Equal(t, "running", vms["testvm"].Status)
	assert.NotNil(t, vms["testvm"].StartedAt)
}

func TestStartVM_NotFound(t *testing.T) {
	Reset()
	defer Reset()

	_, err := MockRun([]string{"start", "nope"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestStartVM_AlreadyRunning(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createAndStartVM("testvm"))

	out, err := MockRun([]string{"start", "testvm"})
	assert.NoError(t, err)
	assert.Contains(t, out, "already running")
}

func TestStartVM_NoArgs(t *testing.T) {
	Reset()
	defer Reset()

	_, err := MockRun([]string{"start"})
	assert.Error(t, err)
}

func TestStopVM_Basic(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createAndStartVM("testvm"))

	out, err := MockRun([]string{"stop", "testvm"})
	require.NoError(t, err)
	assert.Contains(t, out, "Stopped VM")

	vms := GetVMs()
	assert.Equal(t, "stopped", vms["testvm"].Status)
	assert.NotNil(t, vms["testvm"].StoppedAt)
}

func TestStopVM_NotFound(t *testing.T) {
	Reset()
	defer Reset()

	_, err := MockRun([]string{"stop", "nope"})
	assert.Error(t, err)
}

func TestStopVM_AlreadyStopped(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createTestVM("testvm"))

	out, err := MockRun([]string{"stop", "testvm"})
	assert.NoError(t, err)
	assert.Contains(t, out, "already stopped")
}

func TestStopVM_NoArgs(t *testing.T) {
	Reset()
	defer Reset()

	_, err := MockRun([]string{"stop"})
	assert.Error(t, err)
}

func TestDeleteVM_Basic(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createTestVM("testvm"))

	out, err := MockRun([]string{"delete", "testvm"})
	require.NoError(t, err)
	assert.Contains(t, out, "Deleted VM")

	_, exists := GetVMs()["testvm"]
	assert.False(t, exists)
}

func TestDeleteVM_Running(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createAndStartVM("testvm"))

	_, err := MockRun([]string{"delete", "testvm"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "running")
}

func TestDeleteVM_NotFound(t *testing.T) {
	Reset()
	defer Reset()

	_, err := MockRun([]string{"delete", "nope"})
	assert.Error(t, err)
}

func TestDeleteVM_NoArgs(t *testing.T) {
	Reset()
	defer Reset()

	_, err := MockRun([]string{"delete"})
	assert.Error(t, err)
}

// --- Full Lifecycle ---

func TestLifecycle_FullRoundTrip(t *testing.T) {
	Reset()
	defer Reset()

	// Create -> stopped
	require.NoError(t, createTestVM("life"))
	assert.Equal(t, "stopped", GetVMs()["life"].Status)

	// Start -> running
	_, err := MockRun([]string{"start", "life"})
	require.NoError(t, err)
	assert.Equal(t, "running", GetVMs()["life"].Status)

	// Stop -> stopped
	_, err = MockRun([]string{"stop", "life"})
	require.NoError(t, err)
	assert.Equal(t, "stopped", GetVMs()["life"].Status)

	// Delete -> gone
	_, err = MockRun([]string{"delete", "life"})
	require.NoError(t, err)
	_, exists := GetVMs()["life"]
	assert.False(t, exists)
}

// --- List ---

func TestListVMs_Empty(t *testing.T) {
	Reset()
	defer Reset()

	out, err := MockRun([]string{"list"})
	require.NoError(t, err)
	assert.Equal(t, "", out)
}

func TestListVMs_Multiple(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createTestVM("alpha"))
	require.NoError(t, createTestVM("beta"))

	out, err := MockRun([]string{"list"})
	require.NoError(t, err)
	assert.Contains(t, out, "alpha")
	assert.Contains(t, out, "beta")
	// Tab-separated fields
	assert.Contains(t, out, "stopped")
}

// --- Status ---

func TestStatusVM_Basic(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createTestVM("testvm"))

	out, err := MockRun([]string{"status", "testvm"})
	require.NoError(t, err)

	var vm VMState
	require.NoError(t, json.Unmarshal([]byte(out), &vm))
	assert.Equal(t, "testvm", vm.Name)
	assert.Equal(t, "stopped", vm.Status)
}

func TestStatusVM_NotFound(t *testing.T) {
	Reset()
	defer Reset()

	_, err := MockRun([]string{"status", "nope"})
	assert.Error(t, err)
}

func TestStatusVM_NoArgs(t *testing.T) {
	Reset()
	defer Reset()

	_, err := MockRun([]string{"status"})
	assert.Error(t, err)
}

// --- Shell Execution ---

func TestShell_Basic(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createAndStartVM("testvm"))

	out, err := MockRun([]string{"shell", "testvm", "--", "echo", "hello"})
	require.NoError(t, err)
	assert.Contains(t, out, "hello")
}

func TestShell_StoppedVM(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createTestVM("testvm"))

	_, err := MockRun([]string{"shell", "testvm", "--", "echo", "hi"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestShell_NoSeparator(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createAndStartVM("testvm"))

	_, err := MockRun([]string{"shell", "testvm", "echo", "hi"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--")
}

func TestShell_NoCommand(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createAndStartVM("testvm"))

	_, err := MockRun([]string{"shell", "testvm", "--"})
	assert.Error(t, err)
}

func TestShell_TrueCommand(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createAndStartVM("testvm"))

	out, err := MockRun([]string{"shell", "testvm", "--", "true"})
	assert.NoError(t, err)
	assert.Equal(t, "", out)
}

func TestShell_FalseCommand(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createAndStartVM("testvm"))

	_, err := MockRun([]string{"shell", "testvm", "--", "false"})
	assert.Error(t, err)
}

func TestShell_NotFound(t *testing.T) {
	Reset()
	defer Reset()

	_, err := MockRun([]string{"shell", "nope", "--", "echo"})
	assert.Error(t, err)
}

// --- Snapshots ---

func TestSnapshot_CreateAndList(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createAndStartVM("testvm"))

	out, err := MockRun([]string{"snapshot", "create", "testvm", "snap1"})
	require.NoError(t, err)
	assert.Contains(t, out, "Created snapshot")

	out, err = MockRun([]string{"snapshot", "list", "testvm"})
	require.NoError(t, err)
	assert.Contains(t, out, "snap1")
}

func TestSnapshot_NoSnapshots(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createTestVM("testvm"))

	out, err := MockRun([]string{"snapshot", "list", "testvm"})
	require.NoError(t, err)
	assert.Contains(t, out, "No snapshots")
}

func TestSnapshot_Duplicate(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createAndStartVM("testvm"))
	require.NoError(t, createSnapshotDirect("testvm", "dup"))

	_, err := MockRun([]string{"snapshot", "create", "testvm", "dup"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func createSnapshotDirect(vmName, tag string) error {
	_, err := MockRun([]string{"snapshot", "create", vmName, tag})
	return err
}

func TestSnapshot_Restore(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createAndStartVM("testvm"))
	require.NoError(t, createSnapshotDirect("testvm", "snap1"))

	// Stop the VM
	_, err := MockRun([]string{"stop", "testvm"})
	require.NoError(t, err)
	assert.Equal(t, "stopped", GetVMs()["testvm"].Status)

	// Restore snapshot (which was taken while running)
	out, err := MockRun([]string{"snapshot", "restore", "testvm", "snap1"})
	require.NoError(t, err)
	assert.Contains(t, out, "Restored")

	// VM state should be restored to running
	assert.Equal(t, "running", GetVMs()["testvm"].Status)
}

func TestSnapshot_Delete(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createAndStartVM("testvm"))
	require.NoError(t, createSnapshotDirect("testvm", "snap1"))

	out, err := MockRun([]string{"snapshot", "delete", "testvm", "snap1"})
	require.NoError(t, err)
	assert.Contains(t, out, "Deleted snapshot")

	// Snapshot should be gone
	out, err = MockRun([]string{"snapshot", "list", "testvm"})
	require.NoError(t, err)
	assert.Equal(t, "No snapshots", out)
}

func TestSnapshot_DeleteNonExistent(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createTestVM("testvm"))

	_, err := MockRun([]string{"snapshot", "delete", "testvm", "ghost"})
	assert.Error(t, err)
}

func TestSnapshot_RestoreNonExistent(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createTestVM("testvm"))

	_, err := MockRun([]string{"snapshot", "restore", "testvm", "ghost"})
	assert.Error(t, err)
}

func TestSnapshot_VMNotFound(t *testing.T) {
	Reset()
	defer Reset()

	_, err := MockRun([]string{"snapshot", "create", "nope", "snap1"})
	assert.Error(t, err)
}

func TestSnapshot_UnknownAction(t *testing.T) {
	Reset()
	defer Reset()

	_, err := MockRun([]string{"snapshot", "bogus"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown snapshot action")
}

func TestSnapshot_NoArgs(t *testing.T) {
	Reset()
	defer Reset()

	_, err := MockRun([]string{"snapshot"})
	assert.Error(t, err)
}

// --- Persistence ---
// DataDir is a const, so persistence tests work within the actual data
// directory. Reset() cleans it up via os.RemoveAll.

func TestPersistence_SaveLoad(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createTestVM("persist-test"))
	require.NoError(t, createAndStartVM("persist-run"))

	require.NoError(t, Save())

	// Clear in-memory state only (preserve data dir for Load)
	mu.Lock()
	state = make(map[string]*VMState)
	mu.Unlock()
	assert.Empty(t, GetVMs())

	// Reload from disk
	require.NoError(t, Load())
	vms := GetVMs()
	assert.Contains(t, vms, "persist-test")
	assert.Contains(t, vms, "persist-run")
	assert.Equal(t, "stopped", vms["persist-test"].Status)
	assert.Equal(t, "running", vms["persist-run"].Status)
}

func TestPersistence_LoadEmptyDir(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, Load())
	assert.Empty(t, GetVMs())
}

// --- Reset ---

func TestReset_ClearsAllState(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createTestVM("a"))
	require.NoError(t, createTestVM("b"))
	require.NoError(t, createAndStartVM("c"))

	Reset()
	assert.Empty(t, GetVMs())
}

// --- SetVM / GetVMs ---

func TestSetVM_Direct(t *testing.T) {
	Reset()
	defer Reset()

	SetVM("direct", &VMState{
		Name:   "direct",
		Status: "running",
	})

	vms := GetVMs()
	require.Contains(t, vms, "direct")
	assert.Equal(t, "running", vms["direct"].Status)
}

func TestGetVMs_ReturnsCopy(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createTestVM("orig"))
	vms := GetVMs()
	vms["orig"].Status = "tampered"

	// Original should be unchanged
	assert.Equal(t, "stopped", GetVMs()["orig"].Status)
}

// --- Concurrency ---

func TestConcurrency_ParallelCreates(t *testing.T) {
	Reset()
	defer Reset()

	const n = 100
	var wg sync.WaitGroup
	errCh := make(chan error, n)

	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := createTestVM("vm-concurrent"); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	// At least one should succeed; the rest may get duplicate errors
	assert.Contains(t, GetVMs(), "vm-concurrent")
}

func TestConcurrency_ParallelLifecycle(t *testing.T) {
	Reset()
	defer Reset()

	// Create VMs upfront
	const n = 10
	for i := range n {
		require.NoError(t, createTestVM("par-"+string(rune('a'+i))))
	}

	// Run parallel start/stop cycles
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "par-" + string(rune('a'+i))
			MockRun([]string{"start", name})
			MockRun([]string{"stop", name})
		}(i)
	}
	wg.Wait()

	// All should be stopped
	for i := range n {
		name := "par-" + string(rune('a'+i))
		assert.Equal(t, "stopped", GetVMs()[name].Status)
	}
}

// --- Isolation ---

func TestIsolation_DestroyOneDoesNotAffectOther(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createTestVM("keep"))
	require.NoError(t, createTestVM("remove"))

	_, err := MockRun([]string{"delete", "remove"})
	require.NoError(t, err)

	assert.Contains(t, GetVMs(), "keep")
	assert.NotContains(t, GetVMs(), "remove")
}

func TestIsolation_SnapshotsScopedToVM(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createAndStartVM("vm-a"))
	require.NoError(t, createAndStartVM("vm-b"))
	require.NoError(t, createSnapshotDirect("vm-a", "snap-a"))

	// vm-b should have no snapshots
	out, err := MockRun([]string{"snapshot", "list", "vm-b"})
	require.NoError(t, err)
	assert.Equal(t, "No snapshots", out)

	// vm-a should have the snapshot
	out, err = MockRun([]string{"snapshot", "list", "vm-a"})
	require.NoError(t, err)
	assert.Contains(t, out, "snap-a")
}

func TestIsolation_SnapshotNameCanRepeatAcrossVMs(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createAndStartVM("vm-a"))
	require.NoError(t, createAndStartVM("vm-b"))
	require.NoError(t, createSnapshotDirect("vm-a", "same-name"))
	require.NoError(t, createSnapshotDirect("vm-b", "same-name"))

	// Both should have their own snapshot with the same name
	outA, _ := MockRun([]string{"snapshot", "list", "vm-a"})
	outB, _ := MockRun([]string{"snapshot", "list", "vm-b"})
	assert.Contains(t, outA, "same-name")
	assert.Contains(t, outB, "same-name")
}

// --- Property-Based Tests ---

// validVMName generates a valid VM name for property-based tests.
func validVMName() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z][a-z0-9-]{0,30}`)
}

// TestProperty_CreateThenStatusExists verifies that after creating a VM,
// it always appears in status queries.
func TestProperty_CreateThenStatusExists(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		Reset()
		defer Reset()

		name := validVMName().Draw(t, "name")

		_, err := MockRun([]string{"create", name})
		require.NoError(t, err)

		out, err := MockRun([]string{"status", name})
		require.NoError(t, err)

		var vm VMState
		require.NoError(t, json.Unmarshal([]byte(out), &vm))
		assert.Equal(t, name, vm.Name)
		assert.Equal(t, "stopped", vm.Status)
	})
}

// TestProperty_CreateAppearsInList verifies created VMs always appear in list output.
func TestProperty_CreateAppearsInList(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		Reset()
		defer Reset()

		names := rapid.SliceOfN(validVMName(), 1, 5).Draw(t, "names")
		uniqueNames := make(map[string]bool)
		for _, n := range names {
			if uniqueNames[n] {
				continue
			}
			uniqueNames[n] = true
			_, err := MockRun([]string{"create", n})
			require.NoError(t, err)
		}

		out, err := MockRun([]string{"list"})
		require.NoError(t, err)

		for name := range uniqueNames {
			assert.Contains(t, out, name)
		}
	})
}

// TestProperty_DuplicateCreateAlwaysFails verifies creating a VM twice
// always returns an error.
func TestProperty_DuplicateCreateAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		Reset()
		defer Reset()

		name := validVMName().Draw(t, "name")

		_, err := MockRun([]string{"create", name})
		require.NoError(t, err)

		_, err = MockRun([]string{"create", name})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})
}

// TestProperty_LifecycleStateTransitions verifies valid state transitions
// always succeed and invalid ones always fail.
func TestProperty_LifecycleStateTransitions(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		Reset()
		defer Reset()

		name := validVMName().Draw(t, "name")

		// Create always works
		_, err := MockRun([]string{"create", name})
		require.NoError(t, err)
		assert.Equal(t, "stopped", GetVMs()[name].Status)

		// Start from stopped always works
		_, err = MockRun([]string{"start", name})
		require.NoError(t, err)
		assert.Equal(t, "running", GetVMs()[name].Status)

		// Start from running is idempotent
		_, err = MockRun([]string{"start", name})
		assert.NoError(t, err)
		assert.Equal(t, "running", GetVMs()[name].Status)

		// Stop from running always works
		_, err = MockRun([]string{"stop", name})
		require.NoError(t, err)
		assert.Equal(t, "stopped", GetVMs()[name].Status)

		// Stop from stopped is idempotent
		_, err = MockRun([]string{"stop", name})
		assert.NoError(t, err)
		assert.Equal(t, "stopped", GetVMs()[name].Status)

		// Delete from stopped always works
		_, err = MockRun([]string{"delete", name})
		require.NoError(t, err)
		_, exists := GetVMs()[name]
		assert.False(t, exists)

		// All operations on deleted VM fail
		_, err = MockRun([]string{"start", name})
		assert.Error(t, err)
		_, err = MockRun([]string{"stop", name})
		assert.Error(t, err)
		_, err = MockRun([]string{"delete", name})
		assert.Error(t, err)
		_, err = MockRun([]string{"status", name})
		assert.Error(t, err)
	})
}

// TestProperty_SnapshotRoundtrip verifies that creating a snapshot and
// restoring it always returns the VM to the snapshotted state.
func TestProperty_SnapshotRoundtrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		Reset()
		defer Reset()

		name := validVMName().Draw(t, "name")
		tag := rapid.StringMatching(`snap-[a-z0-9]+`).Draw(t, "tag")

		require.NoError(t, createAndStartVM(name))

		// Snapshot while running
		_, err := MockRun([]string{"snapshot", "create", name, tag})
		require.NoError(t, err)

		// Stop the VM
		_, err = MockRun([]string{"stop", name})
		require.NoError(t, err)
		assert.Equal(t, "stopped", GetVMs()[name].Status)

		// Restore the snapshot
		_, err = MockRun([]string{"snapshot", "restore", name, tag})
		require.NoError(t, err)

		// Should be running again (the state at snapshot time)
		assert.Equal(t, "running", GetVMs()[name].Status)
	})
}

// TestProperty_SnapshotDuplicateTagFails verifies that snapshot tags
// must be unique within a VM.
func TestProperty_SnapshotDuplicateTagFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		Reset()
		defer Reset()

		name := validVMName().Draw(t, "name")
		tag := rapid.StringMatching(`tag-[a-z0-9]+`).Draw(t, "tag")

		require.NoError(t, createAndStartVM(name))

		_, err := MockRun([]string{"snapshot", "create", name, tag})
		require.NoError(t, err)

		_, err = MockRun([]string{"snapshot", "create", name, tag})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})
}

// TestProperty_ShellRequiresRunningVM verifies shell commands only
// work on running VMs.
func TestProperty_ShellRequiresRunningVM(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		Reset()
		defer Reset()

		name := validVMName().Draw(t, "name")
		cmd := rapid.SliceOfN(rapid.StringMatching(`[a-z]+`), 1, 3).Draw(t, "cmd")

		require.NoError(t, createTestVM(name))

		// Shell on stopped VM must fail
		args := append([]string{"shell", name, "--"}, cmd...)
		_, err := MockRun(args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not running")
	})
}

// TestProperty_DeleteRunningVMAlwaysFails verifies you cannot delete
// a running VM.
func TestProperty_DeleteRunningVMAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		Reset()
		defer Reset()

		name := validVMName().Draw(t, "name")

		require.NoError(t, createAndStartVM(name))

		_, err := MockRun([]string{"delete", name})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "running")

		// VM should still exist
		assert.Contains(t, GetVMs(), name)
	})
}

// TestProperty_ResetClearsEverything verifies Reset produces a clean slate.
func TestProperty_ResetClearsEverything(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		Reset()
		defer Reset()

		// Generate unique names using a slice with dedup by key
		names := rapid.SliceOfNDistinct(validVMName(), 1, 5, func(s string) string {
			return s
		}).Draw(t, "names")

		for _, n := range names {
			require.NoError(t, createTestVM(n))
		}

		// Verify state exists
		assert.NotEmpty(t, GetVMs())

		Reset()

		// Everything should be gone
		assert.Empty(t, GetVMs())

		// Should be able to create fresh VMs with same names
		for _, n := range names {
			require.NoError(t, createTestVM(n))
		}
	})
}

// TestProperty_ListFormatContainsFields verifies list output always
// contains tab-separated fields with name, status, image, CPUs, memory, disk.
func TestProperty_ListFormatContainsFields(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		Reset()
		defer Reset()

		name := validVMName().Draw(t, "name")
		require.NoError(t, createTestVM(name))

		out, err := MockRun([]string{"list"})
		require.NoError(t, err)

		// Each line should have tab-separated fields
		lines := strings.Split(out, "\n")
		found := false
		for _, line := range lines {
			fields := strings.Split(line, "\t")
			if len(fields) > 0 && fields[0] == name {
				found = true
				assert.GreaterOrEqual(t, len(fields), 6,
					"list output should have at least 6 tab-separated fields")
				assert.Equal(t, name, fields[0])
				assert.Equal(t, "stopped", fields[1])
			}
		}
		assert.True(t, found, "VM %q not found in list output", name)
	})
}

// TestProperty_StatusOutputIsValidJSON verifies status output is always
// parseable JSON with the correct fields.
func TestProperty_StatusOutputIsValidJSON(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		Reset()
		defer Reset()

		name := validVMName().Draw(t, "name")
		require.NoError(t, createTestVM(name))

		out, err := MockRun([]string{"status", name})
		require.NoError(t, err)

		var vm VMState
		require.NoError(t, json.Unmarshal([]byte(out), &vm))
		assert.Equal(t, name, vm.Name)
		assert.NotEmpty(t, vm.Status)
		assert.Contains(t, []string{"stopped", "running", "creating", "error"}, vm.Status)
	})
}

// TestProperty_MultipleVMsIndependent verifies operations on one VM
// never affect another VM's state.
func TestProperty_MultipleVMsIndependent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		Reset()
		defer Reset()

		nameA := rapid.StringMatching(`vm-a-[a-z]+`).Draw(t, "nameA")
		nameB := rapid.StringMatching(`vm-b-[a-z]+`).Draw(t, "nameB")

		require.NoError(t, createAndStartVM(nameA))
		require.NoError(t, createTestVM(nameB))

		// A is running, B is stopped
		assert.Equal(t, "running", GetVMs()[nameA].Status)
		assert.Equal(t, "stopped", GetVMs()[nameB].Status)

		// Stop A
		_, err := MockRun([]string{"stop", nameA})
		require.NoError(t, err)

		// B should still be stopped and unaffected
		assert.Equal(t, "stopped", GetVMs()[nameB].Status)
	})
}

// TestProperty_PersistenceRoundTrip verifies Save+Load preserves all state.
func TestProperty_PersistenceRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		Reset()

		names := rapid.SliceOfNDistinct(validVMName(), 1, 3, func(s string) string {
			return s
		}).Draw(t, "names")

		// Create VMs in various states
		for i, n := range names {
			require.NoError(t, createTestVM(n))
			if i%2 == 0 {
				_, err := MockRun([]string{"start", n})
				require.NoError(t, err)
			}
		}

		before := GetVMs()

		require.NoError(t, Save())

		// Clear in-memory state only (preserve data dir for Load)
		mu.Lock()
		state = make(map[string]*VMState)
		mu.Unlock()

		require.NoError(t, Load())

		after := GetVMs()
		assert.Equal(t, len(before), len(after))

		for name, vmBefore := range before {
			vmAfter, ok := after[name]
			require.True(t, ok, "VM %q missing after load", name)
			assert.Equal(t, vmBefore.Name, vmAfter.Name)
			assert.Equal(t, vmBefore.Status, vmAfter.Status)
		}
	})
}

// TestProperty_SnapshotDeleteDoesNotAffectVM verifies deleting a snapshot
// doesn't change the VM's current state.
func TestProperty_SnapshotDeleteDoesNotAffectVM(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		Reset()
		defer Reset()

		name := validVMName().Draw(t, "name")
		tag := rapid.StringMatching(`del-[a-z0-9]+`).Draw(t, "tag")

		require.NoError(t, createAndStartVM(name))
		require.NoError(t, createSnapshotDirect(name, tag))

		statusBefore := GetVMs()[name].Status

		_, err := MockRun([]string{"snapshot", "delete", name, tag})
		require.NoError(t, err)

		statusAfter := GetVMs()[name].Status
		assert.Equal(t, statusBefore, statusAfter)
	})
}

// --- Save/Load edge cases ---

func TestSave_CreatesDataDir(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createTestVM("test"))
	require.NoError(t, Save())

	_, err := os.Stat(DataDir)
	assert.NoError(t, err)
}

func TestLoad_InvalidJSON(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, os.MkdirAll(DataDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(DataDir, "bad.json"),
		[]byte("not valid json"),
		0644,
	))

	err := Load()
	assert.Error(t, err)
}

// --- Timestamp invariants ---

func TestTimestamps_CreatedAtSet(t *testing.T) {
	Reset()
	defer Reset()

	before := time.Now()
	require.NoError(t, createTestVM("ts"))
	after := time.Now()

	vm := GetVMs()["ts"]
	assert.False(t, vm.CreatedAt.IsZero())
	assert.True(t, !vm.CreatedAt.Before(before) && !vm.CreatedAt.After(after))
}

func TestTimestamps_StartedAtSetOnStart(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createTestVM("ts"))
	assert.Nil(t, GetVMs()["ts"].StartedAt)

	// Start the already-created VM (don't use createAndStartVM which would double-create)
	_, err := MockRun([]string{"start", "ts"})
	require.NoError(t, err)
	assert.NotNil(t, GetVMs()["ts"].StartedAt)
}

func TestTimestamps_StoppedAtSetOnStop(t *testing.T) {
	Reset()
	defer Reset()

	require.NoError(t, createAndStartVM("ts"))
	assert.Nil(t, GetVMs()["ts"].StoppedAt)

	_, err := MockRun([]string{"stop", "ts"})
	require.NoError(t, err)
	assert.NotNil(t, GetVMs()["ts"].StoppedAt)
}
