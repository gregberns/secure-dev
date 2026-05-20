// Tests for bug-created-at-zero migration in config.Loader.
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadVMConfig_MigratesMissingCreatedAtFromMtime(t *testing.T) {
	sdHome := t.TempDir()
	vmDir := filepath.Join(sdHome, "vms", "legacy")
	require.NoError(t, os.MkdirAll(vmDir, 0700))

	// Legacy config without state.created_at.
	yamlBody := []byte(
		"name: legacy\n" +
			"backend: lima\n" +
			"cpus: 4\n" +
			"memory: 8GiB\n" +
			"disk: 100GiB\n" +
			"image: ubuntu:24.04\n" +
			"state:\n" +
			"  status: running\n",
	)
	cfgPath := filepath.Join(vmDir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, yamlBody, 0600))

	// Force a known mtime so we can assert against it.
	want := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(cfgPath, want, want))

	l := NewLoader(WithSDHome(sdHome))
	cfg, err := l.ReadVMConfig("legacy")
	require.NoError(t, err)
	require.NotNil(t, cfg.State.CreatedAt, "migration must set CreatedAt for legacy configs")
	assert.Equal(t, want.Unix(), cfg.State.CreatedAt.Unix(),
		"CreatedAt should equal the state file's mtime when not persisted")
}

func TestReadVMConfig_PreservesExistingCreatedAt(t *testing.T) {
	sdHome := t.TempDir()
	vmDir := filepath.Join(sdHome, "vms", "fresh")
	require.NoError(t, os.MkdirAll(vmDir, 0700))

	want := time.Date(2026, 3, 1, 9, 30, 0, 0, time.UTC)
	yamlBody := []byte(
		"name: fresh\n" +
			"backend: lima\n" +
			"cpus: 4\n" +
			"memory: 8GiB\n" +
			"disk: 100GiB\n" +
			"image: ubuntu:24.04\n" +
			"state:\n" +
			"  status: running\n" +
			"  created_at: " + want.Format(time.RFC3339) + "\n",
	)
	require.NoError(t, os.WriteFile(filepath.Join(vmDir, "config.yaml"), yamlBody, 0600))

	l := NewLoader(WithSDHome(sdHome))
	cfg, err := l.ReadVMConfig("fresh")
	require.NoError(t, err)
	require.NotNil(t, cfg.State.CreatedAt)
	assert.Equal(t, want.Unix(), cfg.State.CreatedAt.Unix(),
		"persisted created_at must not be overwritten by mtime migration")
}

func TestWriteThenReadVMConfig_RoundtripsCreatedAt(t *testing.T) {
	sdHome := t.TempDir()
	l := NewLoader(WithSDHome(sdHome))

	when := time.Date(2026, 5, 20, 14, 0, 0, 0, time.UTC)
	require.NoError(t, l.WriteVMConfig(&VMConfig{
		Name:    "rt",
		Backend: "memory",
		CPUs:    2,
		Memory:  "4GiB",
		Disk:    "20GiB",
		Image:   "ubuntu:24.04",
		State:   VMState{Status: VMStatusRunning, CreatedAt: &when},
	}))

	got, err := l.ReadVMConfig("rt")
	require.NoError(t, err)
	require.NotNil(t, got.State.CreatedAt)
	assert.Equal(t, when.Unix(), got.State.CreatedAt.Unix())
}
