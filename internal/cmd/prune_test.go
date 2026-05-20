// Tests for `sd prune`.
//
// bug-no-prune-command: covers orphan detection, dry-run, removal, and JSON
// output shape. Tests use a memory backend and a temporary SD_HOME so they
// can simulate orphans by creating state directories directly.
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/backend"
	"sd/internal/backend/memory"
)

// resetPruneFlags clears prune-specific flags between tests. cobra flag
// values persist across rootCmd.Execute() calls because we share a singleton.
func resetPruneFlags(t *testing.T) {
	t.Helper()
	root := RootCmd()
	pruneCmd, _, err := root.Find([]string{"prune"})
	require.NoError(t, err)
	_ = pruneCmd.Flags().Set("dry-run", "false")
	_ = pruneCmd.Flags().Set("yes", "false")
	_ = pruneCmd.Flags().Set("include-unavailable-backends", "false")
}

// setupPruneTest mirrors setupListTest but explicitly seeds the memory backend
// with a list of "live" VM names; remaining directories under $SD_HOME/vms
// will be treated as orphans by `sd prune`.
func setupPruneTest(t *testing.T, liveVMs []string, orphanDirs []string) (string, *memory.Backend) {
	t.Helper()
	sdHome := newRootTestEnv(t)
	resetPruneFlags(t)
	t.Cleanup(func() { resetPruneFlags(t) })

	mb := memory.New()
	for _, n := range liveVMs {
		require.NoError(t, mb.Create(context.Background(), n, backend.VMConfig{
			CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04",
		}))
	}

	// Inject backend
	origGetBackend := getBackendFunc
	origAllBackendNames := allBackendNames
	getBackendFunc = func(_ string) (backend.Backend, error) { return mb, nil }
	allBackendNames = func() []string { return []string{"memory"} }
	t.Cleanup(func() {
		getBackendFunc = origGetBackend
		allBackendNames = origAllBackendNames
		mb.Reset()
	})

	// Seed orphan state directories.
	vmsDir := filepath.Join(sdHome, "vms")
	require.NoError(t, os.MkdirAll(vmsDir, 0700))
	for _, name := range orphanDirs {
		dir := filepath.Join(vmsDir, name)
		require.NoError(t, os.MkdirAll(dir, 0700))
		// Touch a config.yaml so the directory has content.
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
			[]byte("name: "+name+"\n"), 0600))
	}
	return sdHome, mb
}

func TestPruneCommand_Registered(t *testing.T) {
	newRootTestEnv(t)
	root := RootCmd()
	pruneCmd, _, err := root.Find([]string{"prune"})
	require.NoError(t, err)
	assert.Equal(t, "prune", pruneCmd.Name())
	assert.Equal(t, "vm", pruneCmd.GroupID)
}

func TestPrune_NoOrphans_JSON(t *testing.T) {
	setupPruneTest(t, []string{"alive"}, nil)
	root := RootCmd()
	root.SetArgs([]string{"prune", "--json", "--yes"})

	stdout := capturePruneStdout(t, func() { require.NoError(t, root.Execute()) })

	var env struct {
		OK   bool        `json:"ok"`
		Data pruneResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &env))
	assert.True(t, env.OK)
	assert.Empty(t, env.Data.Pruned)
	assert.Empty(t, env.Data.Kept)
}

func TestPrune_DryRunListsOrphans(t *testing.T) {
	sdHome, _ := setupPruneTest(t, nil, []string{"orph1", "orph2"})
	root := RootCmd()
	root.SetArgs([]string{"prune", "--json", "--dry-run"})

	stdout := capturePruneStdout(t, func() { require.NoError(t, root.Execute()) })

	var env struct {
		OK   bool        `json:"ok"`
		Data pruneResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &env))
	assert.True(t, env.Data.DryRun, "dry-run flag must be reflected")
	assert.ElementsMatch(t, []string{"orph1", "orph2"}, env.Data.Kept)
	assert.Empty(t, env.Data.Pruned)

	// Directories must still exist.
	for _, n := range []string{"orph1", "orph2"} {
		_, err := os.Stat(filepath.Join(sdHome, "vms", n))
		assert.NoError(t, err, "dry-run must not remove %s", n)
	}
}

func TestPrune_RemovesOrphans_WithYes(t *testing.T) {
	sdHome, _ := setupPruneTest(t, []string{"alive"}, []string{"orph1", "orph2"})
	root := RootCmd()
	root.SetArgs([]string{"prune", "--json", "--yes"})

	stdout := capturePruneStdout(t, func() { require.NoError(t, root.Execute()) })

	var env struct {
		OK   bool        `json:"ok"`
		Data pruneResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &env))
	assert.False(t, env.Data.DryRun)
	assert.ElementsMatch(t, []string{"orph1", "orph2"}, env.Data.Pruned)
	assert.Empty(t, env.Data.Kept)

	// Orphan directories must be gone; alive one untouched (there isn't one
	// on disk in this test, but verify the dir doesn't reappear).
	for _, n := range []string{"orph1", "orph2"} {
		_, err := os.Stat(filepath.Join(sdHome, "vms", n))
		assert.True(t, os.IsNotExist(err), "orphan %s should be removed", n)
	}
}

func TestPrune_JSONWithoutYes_IsDryRun(t *testing.T) {
	// JSON mode without --yes must default to dry-run to keep automation
	// from accidentally deleting state.
	sdHome, _ := setupPruneTest(t, nil, []string{"orph1"})
	root := RootCmd()
	root.SetArgs([]string{"prune", "--json"})

	stdout := capturePruneStdout(t, func() { require.NoError(t, root.Execute()) })
	var env struct {
		OK   bool        `json:"ok"`
		Data pruneResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &env))
	assert.True(t, env.Data.DryRun)
	_, err := os.Stat(filepath.Join(sdHome, "vms", "orph1"))
	assert.NoError(t, err, "JSON without --yes must not remove")
}

// failingListBackend embeds memory.Backend but List() returns an error.
// Used to verify that prune fails closed on backend errors rather than
// classifying live VMs as orphans.
type failingListBackend struct{ *memory.Backend }

func (f *failingListBackend) List(_ context.Context) ([]backend.VMInfo, error) {
	return nil, fmt.Errorf("simulated lima socket failure")
}

// bug-no-prune-command: regression test that prune fails closed when a backend
// cannot enumerate VMs. Without this guard a transient backend hiccup would
// wipe every live VM's state directory.
func TestPrune_BackendListError_FailsClosed(t *testing.T) {
	sdHome := newRootTestEnv(t)
	resetPruneFlags(t)
	t.Cleanup(func() { resetPruneFlags(t) })

	mb := memory.New()
	require.NoError(t, mb.Create(context.Background(), "alive", backend.VMConfig{
		CPUs: 4, Memory: "8GiB", Disk: "100GiB", BaseImage: "ubuntu:24.04",
	}))
	failing := &failingListBackend{Backend: mb}

	origGetBackend := getBackendFunc
	origAllBackendNames := allBackendNames
	getBackendFunc = func(_ string) (backend.Backend, error) { return failing, nil }
	allBackendNames = func() []string { return []string{"memory"} }
	t.Cleanup(func() {
		getBackendFunc = origGetBackend
		allBackendNames = origAllBackendNames
		mb.Reset()
	})

	// Seed a directory that LOOKS orphan because backend.List fails.
	vmsDir := filepath.Join(sdHome, "vms")
	require.NoError(t, os.MkdirAll(filepath.Join(vmsDir, "alive"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(vmsDir, "alive", "config.yaml"),
		[]byte("name: alive\n"), 0600))

	root := RootCmd()
	root.SetArgs([]string{"prune", "--json", "--yes"})
	err := root.Execute()
	require.Error(t, err, "prune must fail closed when backend list errors")
	// Directory must NOT have been removed.
	_, statErr := os.Stat(filepath.Join(vmsDir, "alive"))
	require.NoError(t, statErr, "live VM state must survive backend list failure")
}

// captureStdout redirects os.Stdout for the duration of fn and returns
// whatever was written.
func capturePruneStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		io.Copy(&buf, r)
		close(done)
	}()

	fn()

	w.Close()
	os.Stdout = old
	<-done
	return strings.TrimSpace(buf.String()) + "\n"
}
