package provision

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- checksumKeyToEnvVar Tests ---
// REQ-004-028: Checksum verification for downloaded binaries.

func TestChecksumKeyToEnvVar_GoTarball(t *testing.T) {
	got := checksumKeyToEnvVar("go1.23.4.linux-arm64.tar.gz")
	assert.Equal(t, "CHECKSUM_GO1_23_4_LINUX_ARM64_TAR_GZ", got)
}

func TestChecksumKeyToEnvVar_GoAmd64Tarball(t *testing.T) {
	got := checksumKeyToEnvVar("go1.23.4.linux-amd64.tar.gz")
	assert.Equal(t, "CHECKSUM_GO1_23_4_LINUX_AMD64_TAR_GZ", got)
}

func TestChecksumKeyToEnvVar_GhTarball(t *testing.T) {
	got := checksumKeyToEnvVar("gh_2.67.0_linux_arm64.tar.gz")
	assert.Equal(t, "CHECKSUM_GH_2_67_0_LINUX_ARM64_TAR_GZ", got)
}

func TestChecksumKeyToEnvVar_SimpleFilename(t *testing.T) {
	got := checksumKeyToEnvVar("nvm-install.sh")
	assert.Equal(t, "CHECKSUM_NVM_INSTALL_SH", got)
}

func TestChecksumKeyToEnvVar_AlreadyUppercase(t *testing.T) {
	got := checksumKeyToEnvVar("FILE.TAR.GZ")
	assert.Equal(t, "CHECKSUM_FILE_TAR_GZ", got)
}

func TestChecksumKeyToEnvVar_NoDotOrHyphen(t *testing.T) {
	got := checksumKeyToEnvVar("somefile")
	assert.Equal(t, "CHECKSUM_SOMEFILE", got)
}

// --- checksumEnvBlock Tests ---

func TestChecksumEnvBlock_Empty(t *testing.T) {
	got := checksumEnvBlock(nil)
	assert.Equal(t, "", got)
}

func TestChecksumEnvBlock_EmptyMap(t *testing.T) {
	got := checksumEnvBlock(map[string]string{})
	assert.Equal(t, "", got)
}

func TestChecksumEnvBlock_SingleEntry(t *testing.T) {
	checksums := map[string]string{
		"go1.23.4.linux-arm64.tar.gz": "16e5017863a7f6071363571571ee35fcdacf27bc0569a9a1e4be76e3a51e00a4",
	}
	got := checksumEnvBlock(checksums)
	assert.Contains(t, got, "export CHECKSUM_GO1_23_4_LINUX_ARM64_TAR_GZ=")
	assert.Contains(t, got, "16e5017863a7f6071363571571ee35fcdacf27bc0569a9a1e4be76e3a51e00a4")
	assert.True(t, strings.HasSuffix(got, "\n"), "block must end with newline")
}

func TestChecksumEnvBlock_MultipleEntries_DeterministicOrder(t *testing.T) {
	checksums := map[string]string{
		"go1.23.4.linux-arm64.tar.gz": "arm64hash",
		"go1.23.4.linux-amd64.tar.gz": "amd64hash",
	}
	got := checksumEnvBlock(checksums)
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	require.Len(t, lines, 2)
	// amd64 sorts before arm64
	assert.Contains(t, lines[0], "AMD64")
	assert.Contains(t, lines[1], "ARM64")
}

func TestChecksumEnvBlock_ValuesAreQuoted(t *testing.T) {
	checksums := map[string]string{
		"file.tar.gz": "abc123",
	}
	got := checksumEnvBlock(checksums)
	// The value should be shell-quoted
	assert.Contains(t, got, `"abc123"`)
}

func TestChecksumEnvBlock_DeterministicAcrossCalls(t *testing.T) {
	checksums := map[string]string{
		"z-file.tar.gz": "hash_z",
		"a-file.tar.gz": "hash_a",
		"m-file.tar.gz": "hash_m",
	}
	first := checksumEnvBlock(checksums)
	for i := 0; i < 10; i++ {
		got := checksumEnvBlock(checksums)
		assert.Equal(t, first, got, "checksumEnvBlock must be deterministic (iteration %d)", i)
	}
}

// --- Provision with checksum injection Tests ---

func TestProvision_InjectsChecksums(t *testing.T) {
	// REQ-004-028: Verify checksums are injected into scripts.
	var capturedScripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		// The last element in command is the script content
		if len(command) > 0 {
			capturedScripts = append(capturedScripts, command[len(command)-1])
		}
		return "", "", 0, nil
	}

	modules := []Module{
		{
			Name:        "test-mod",
			Description: "Test module with checksums",
			Scripts: []Script{
				{Mode: ModeSystem, Script: "echo hello\n"},
			},
			Checksums: map[string]string{
				"go1.23.4.linux-arm64.tar.gz": "arm64hash",
				"go1.23.4.linux-amd64.tar.gz": "amd64hash",
			},
		},
	}

	result := Provision(context.Background(), execFn, "test-vm", modules)
	require.False(t, result.Failed)
	require.Len(t, capturedScripts, 1)

	script := capturedScripts[0]
	assert.Contains(t, script, "export CHECKSUM_GO1_23_4_LINUX_ARM64_TAR_GZ=")
	assert.Contains(t, script, "export CHECKSUM_GO1_23_4_LINUX_AMD64_TAR_GZ=")
	assert.Contains(t, script, "arm64hash")
	assert.Contains(t, script, "amd64hash")
	assert.Contains(t, script, "set -eux -o pipefail")
}

func TestProvision_NoChecksums_NoInjection(t *testing.T) {
	var capturedScripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		if len(command) > 0 {
			capturedScripts = append(capturedScripts, command[len(command)-1])
		}
		return "", "", 0, nil
	}

	modules := []Module{
		{
			Name:        "test-mod",
			Description: "Test module without checksums",
			Scripts: []Script{
				{Mode: ModeUser, Script: "echo hello\n"},
			},
		},
	}

	result := Provision(context.Background(), execFn, "test-vm", modules)
	require.False(t, result.Failed)
	require.Len(t, capturedScripts, 1)

	script := capturedScripts[0]
	assert.NotContains(t, script, "CHECKSUM_")
	// Should still have preamble
	assert.True(t, strings.HasPrefix(script, "set -eux -o pipefail\n"))
}

func TestProvision_ChecksumsInjectedPerModule(t *testing.T) {
	// Verify each module gets its own checksums, not another module's.
	var capturedScripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		if len(command) > 0 {
			capturedScripts = append(capturedScripts, command[len(command)-1])
		}
		return "", "", 0, nil
	}

	modules := []Module{
		{
			Name:        "mod-a",
			Description: "Module A",
			Scripts:     []Script{{Mode: ModeSystem, Script: "echo a\n"}},
			Checksums:   map[string]string{"file-a.tar.gz": "hash_a"},
		},
		{
			Name:        "mod-b",
			Description: "Module B",
			Scripts:     []Script{{Mode: ModeSystem, Script: "echo b\n"}},
			Checksums:   map[string]string{"file-b.tar.gz": "hash_b"},
		},
	}

	result := Provision(context.Background(), execFn, "test-vm", modules)
	require.False(t, result.Failed)
	require.Len(t, capturedScripts, 2)

	// Module A's script should have file-a checksum, not file-b
	assert.Contains(t, capturedScripts[0], "CHECKSUM_FILE_A_TAR_GZ")
	assert.NotContains(t, capturedScripts[0], "CHECKSUM_FILE_B_TAR_GZ")

	// Module B's script should have file-b checksum, not file-a
	assert.Contains(t, capturedScripts[1], "CHECKSUM_FILE_B_TAR_GZ")
	assert.NotContains(t, capturedScripts[1], "CHECKSUM_FILE_A_TAR_GZ")
}

func TestProvision_ChecksumBlockComesBeforePreamble(t *testing.T) {
	// The checksum exports must come before set -eux so they're available
	// to the script body.
	var capturedScripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		if len(command) > 0 {
			capturedScripts = append(capturedScripts, command[len(command)-1])
		}
		return "", "", 0, nil
	}

	modules := []Module{
		{
			Name:        "test-mod",
			Description: "Test",
			Scripts:     []Script{{Mode: ModeSystem, Script: "echo hi\n"}},
			Checksums:   map[string]string{"file.tar.gz": "somehash"},
		},
	}

	Provision(context.Background(), execFn, "test-vm", modules)
	require.Len(t, capturedScripts, 1)

	script := capturedScripts[0]
	checksumIdx := strings.Index(script, "export CHECKSUM_")
	preambleIdx := strings.Index(script, "set -eux -o pipefail")
	assert.Greater(t, preambleIdx, -1, "preamble should be present")
	assert.Greater(t, checksumIdx, -1, "checksum export should be present")
	assert.Less(t, checksumIdx, preambleIdx,
		"checksum exports should come before preamble")
}

func TestProvision_MultipleScriptsGetSameChecksums(t *testing.T) {
	// Both scripts in the same module should get the same checksum block.
	var capturedScripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		if len(command) > 0 {
			capturedScripts = append(capturedScripts, command[len(command)-1])
		}
		return "", "", 0, nil
	}

	modules := []Module{
		{
			Name:        "test-mod",
			Description: "Test",
			Scripts: []Script{
				{Mode: ModeSystem, Script: "echo first\n"},
				{Mode: ModeUser, Script: "echo second\n"},
			},
			Checksums: map[string]string{"tool.tar.gz": "toolhash"},
		},
	}

	result := Provision(context.Background(), execFn, "test-vm", modules)
	require.False(t, result.Failed)
	require.Len(t, capturedScripts, 2)

	for i, script := range capturedScripts {
		assert.Contains(t, script, "export CHECKSUM_TOOL_TAR_GZ=",
			"script[%d] should contain checksum export", i)
	}
}

// --- Readiness Probe Tests ---
// REQ-006-008: Readiness probes.

func TestProvision_ProbePassesFirstTry(t *testing.T) {
	var probeCalls int32
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		// Detect probe vs script: probe uses "bash -c <command>" without sudo prefix
		// and the command matches the probe command.
		if len(command) == 3 && command[0] == "bash" && command[1] == "-c" && command[2] == "test -f /ready" {
			atomic.AddInt32(&probeCalls, 1)
		}
		return "", "", 0, nil
	}

	modules := []Module{
		{
			Name:        "test-mod",
			Description: "Module with probe",
			Scripts:     []Script{{Mode: ModeUser, Script: "echo setup\n"}},
			Probe: &Probe{
				Command:  "test -f /ready",
				Interval: 10 * time.Millisecond,
				Timeout:  1 * time.Second,
			},
		},
	}

	result := Provision(context.Background(), execFn, "test-vm", modules)
	require.False(t, result.Failed, "expected success but got: %s", result.Error)
	assert.Equal(t, StatusCompleted, result.State.Modules[0].Status)
	assert.Equal(t, int32(1), atomic.LoadInt32(&probeCalls), "probe should run exactly once")
}

func TestProvision_ProbePassesAfterRetries(t *testing.T) {
	var probeCalls int32
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		if len(command) == 3 && command[0] == "bash" && command[1] == "-c" && command[2] == "check-ready" {
			n := atomic.AddInt32(&probeCalls, 1)
			if n < 3 {
				return "", "not ready", 1, nil // fail first 2 attempts
			}
			return "", "", 0, nil // pass on 3rd attempt
		}
		return "", "", 0, nil // scripts succeed
	}

	modules := []Module{
		{
			Name:        "test-mod",
			Description: "Module with retrying probe",
			Scripts:     []Script{{Mode: ModeUser, Script: "echo setup\n"}},
			Probe: &Probe{
				Command:  "check-ready",
				Interval: 10 * time.Millisecond,
				Timeout:  2 * time.Second,
			},
		},
	}

	result := Provision(context.Background(), execFn, "test-vm", modules)
	require.False(t, result.Failed, "expected success after retries but got: %s", result.Error)
	assert.Equal(t, StatusCompleted, result.State.Modules[0].Status)
	assert.Equal(t, int32(3), atomic.LoadInt32(&probeCalls), "probe should have been called 3 times")
}

func TestProvision_ProbeTimesOut(t *testing.T) {
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		if len(command) == 3 && command[0] == "bash" && command[1] == "-c" && command[2] == "false" {
			return "", "fail", 1, nil // always fail
		}
		return "", "", 0, nil // scripts succeed
	}

	modules := []Module{
		{
			Name:        "test-mod",
			Description: "Module with failing probe",
			Scripts:     []Script{{Mode: ModeUser, Script: "echo setup\n"}},
			Probe: &Probe{
				Command:  "false",
				Interval: 10 * time.Millisecond,
				Timeout:  50 * time.Millisecond,
			},
		},
	}

	result := Provision(context.Background(), execFn, "test-vm", modules)
	require.True(t, result.Failed)
	assert.Equal(t, "test-mod", result.Module)
	assert.Contains(t, result.Error, "probe timed out")
	assert.Contains(t, result.Error, "test-mod")
	assert.Equal(t, StatusFailed, result.State.Modules[0].Status)
	assert.Equal(t, 1, result.Script, "script index should be len(scripts) for probe failure")
}

func TestProvision_NoProbe_SkipsProbeExecution(t *testing.T) {
	callCount := 0
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		callCount++
		return "", "", 0, nil
	}

	modules := []Module{
		{
			Name:        "test-mod",
			Description: "Module without probe",
			Scripts: []Script{
				{Mode: ModeUser, Script: "echo hello\n"},
				{Mode: ModeSystem, Script: "echo world\n"},
			},
		},
	}

	result := Provision(context.Background(), execFn, "test-vm", modules)
	require.False(t, result.Failed)
	assert.Equal(t, StatusCompleted, result.State.Modules[0].Status)
	// Should only have the 2 script calls, no probe calls
	assert.Equal(t, 2, callCount, "should only execute scripts, not a probe")
}
