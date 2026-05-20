package provision

import (
	"bytes"
	"context"
	"fmt"
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
	assert.Contains(t, script, "set -eu -o pipefail")
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
	assert.True(t, strings.HasPrefix(script, "set -eu -o pipefail\n"))
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
	preambleIdx := strings.Index(script, "set -eu -o pipefail")
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

// --- REQ-006-005: script failure error includes captured stdout/stderr ---

func TestProvision_ScriptFailure_ErrorContainsStderrTail(t *testing.T) {
	execFn := func(_ context.Context, _ string, _ []string) (string, string, int, error) {
		return "some progress\noutput line", "boom: package not found\nfailed step", 17, nil
	}
	mods := []Module{
		{Name: "base", Scripts: []Script{{Mode: ModeSystem, Script: "false"}}},
	}

	result := Provision(context.Background(), execFn, "vm", mods)
	require.True(t, result.Failed)
	assert.Equal(t, "base", result.Module)
	// Backwards-compatible prefix retained so existing log tooling keeps working.
	assert.Contains(t, result.Error, `module "base" script[0] failed (exit 17)`)
	// Stderr tail must be surfaced for diagnosis.
	assert.Contains(t, result.Error, "boom: package not found")
	assert.Contains(t, result.Error, "failed step")
	assert.Contains(t, result.Error, "--- stderr")
	// Stdout tail must also be surfaced.
	assert.Contains(t, result.Error, "some progress")
	assert.Contains(t, result.Error, "--- stdout")
	// ModuleExecutionStatus.Error should mirror the surfaced message.
	assert.Equal(t, result.Error, result.State.Modules[0].Error)
}

func TestProvision_ExecError_ErrorContainsStderrTail(t *testing.T) {
	execFn := func(_ context.Context, _ string, _ []string) (string, string, int, error) {
		return "partial stdout", "ssh transport noise", 0, fmt.Errorf("connection reset")
	}
	mods := []Module{
		{Name: "base", Scripts: []Script{{Mode: ModeSystem, Script: "echo hi"}}},
	}

	result := Provision(context.Background(), execFn, "vm", mods)
	require.True(t, result.Failed)
	assert.Contains(t, result.Error, "connection reset")
	// Even on exec error, any captured output is included.
	assert.Contains(t, result.Error, "ssh transport noise")
	assert.Contains(t, result.Error, "partial stdout")
}

func TestProvision_ScriptFailure_StderrTailTruncatedTo50Lines(t *testing.T) {
	// Produce 200 lines of stderr; only the last 50 should appear in the
	// error. The earliest lines must NOT be present.
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, fmt.Sprintf("err-line-%03d", i))
	}
	stderr := strings.Join(lines, "\n")
	execFn := func(_ context.Context, _ string, _ []string) (string, string, int, error) {
		return "", stderr, 1, nil
	}
	mods := []Module{
		{Name: "base", Scripts: []Script{{Mode: ModeSystem, Script: "false"}}},
	}

	result := Provision(context.Background(), execFn, "vm", mods)
	require.True(t, result.Failed)
	// Earliest line is dropped...
	assert.NotContains(t, result.Error, "err-line-000")
	assert.NotContains(t, result.Error, "err-line-149")
	// ...and the last 50 (150..199) are retained.
	assert.Contains(t, result.Error, "err-line-150")
	assert.Contains(t, result.Error, "err-line-199")
}

// --- REQ-006-005: provisioning log writer captures per-script transcript ---

func TestProvisionWithOptions_LogWriterReceivesTranscript(t *testing.T) {
	execFn := func(_ context.Context, _ string, _ []string) (string, string, int, error) {
		return "hello stdout", "hello stderr", 0, nil
	}
	mods := []Module{
		{Name: "base", Scripts: []Script{
			{Mode: ModeSystem, Script: "echo hi"},
			{Mode: ModeUser, Script: "echo hi2"},
		}},
	}

	var buf bytes.Buffer
	result := ProvisionWithOptions(context.Background(), execFn, "vm", mods, Options{LogWriter: &buf})
	require.False(t, result.Failed)

	got := buf.String()
	// Both scripts are represented in the transcript.
	assert.Contains(t, got, "module=base script=0 mode=system exit=0")
	assert.Contains(t, got, "module=base script=1 mode=user exit=0")
	assert.Contains(t, got, "--- stdout ---")
	assert.Contains(t, got, "hello stdout")
	assert.Contains(t, got, "--- stderr ---")
	assert.Contains(t, got, "hello stderr")
}

func TestProvisionWithOptions_LogWriterCapturesFailingScript(t *testing.T) {
	execFn := func(_ context.Context, _ string, _ []string) (string, string, int, error) {
		return "", "fatal: nope", 1, nil
	}
	mods := []Module{
		{Name: "base", Scripts: []Script{{Mode: ModeSystem, Script: "false"}}},
	}

	var buf bytes.Buffer
	result := ProvisionWithOptions(context.Background(), execFn, "vm", mods, Options{LogWriter: &buf})
	require.True(t, result.Failed)

	got := buf.String()
	assert.Contains(t, got, "module=base script=0 mode=system exit=1")
	assert.Contains(t, got, "fatal: nope")
}

func TestProvisionWithOptions_NilLogWriter_DoesNotPanic(t *testing.T) {
	execFn := func(_ context.Context, _ string, _ []string) (string, string, int, error) {
		return "out", "err", 0, nil
	}
	mods := []Module{
		{Name: "base", Scripts: []Script{{Mode: ModeSystem, Script: "echo x"}}},
	}
	result := ProvisionWithOptions(context.Background(), execFn, "vm", mods, Options{LogWriter: nil})
	assert.False(t, result.Failed)
}

// REQ-006-005, REQ-004-024: credentials in script output must be redacted in
// both the provision log file and the error envelope. Bash trace and reflected
// env vars in failing scripts are the most common leak vector.
func TestProvision_RedactsCredentialsInLogAndError(t *testing.T) {
	leaky := strings.Join([]string{
		"ANTHROPIC_API_KEY=sk-ant-abc123def456ghi789",
		"curl -H 'Authorization: Bearer ghp_abcdef1234567890ABCDEF'",
		"git clone https://x-access-token:ghp_topsecretXYZ12345@github.com/foo/bar",
		"export GITHUB_TOKEN=github_pat_11ABCDEFG0xyz0987654321",
		"AKIAEXAMPLEACCESSKEY",
	}, "\n")

	execFn := func(_ context.Context, _ string, _ []string) (string, string, int, error) {
		return leaky, leaky, 2, nil
	}
	mods := []Module{
		{Name: "leak", Scripts: []Script{{Mode: ModeSystem, Script: "true"}}},
	}

	var buf bytes.Buffer
	result := ProvisionWithOptions(context.Background(), execFn, "vm", mods, Options{LogWriter: &buf})
	require.True(t, result.Failed)

	logOut := buf.String()
	for _, secret := range []string{
		"sk-ant-abc123def456ghi789",
		"ghp_abcdef1234567890ABCDEF",
		"ghp_topsecretXYZ12345",
		"github_pat_11ABCDEFG0xyz0987654321",
		"AKIAEXAMPLEACCESSKEY",
	} {
		assert.NotContains(t, logOut, secret,
			"provision log must not contain raw credential %q", secret)
		assert.NotContains(t, result.Error, secret,
			"error envelope must not contain raw credential %q", secret)
	}
	assert.Contains(t, logOut, "[REDACTED]", "log must contain redaction placeholder")
	assert.Contains(t, result.Error, "[REDACTED]", "error must contain redaction placeholder")
}

// REQ-006-005, REQ-004-024: redactCredentials unit test for representative
// patterns; keeps the redaction surface checked as patterns evolve.
func TestRedactCredentials_Patterns(t *testing.T) {
	cases := []struct{ in, mustNotContain string }{
		{"prefix ghp_AAAABBBBCCCCDDDD suffix", "ghp_AAAABBBBCCCCDDDD"},
		{"sk-ant-abcdefghij more", "sk-ant-abcdefghij"},
		{"Bearer xxxxxxxxxxxxxx", "Bearer xxxxxxxxxxxxxx"},
		{"https://user:hunter2@example.com/path", "hunter2"},
		{"AKIA0123456789ABCDEF", "AKIA0123456789ABCDEF"},
	}
	for _, c := range cases {
		got := redactCredentials(c.in)
		assert.NotContains(t, got, c.mustNotContain,
			"redactCredentials must strip %q from %q (got %q)", c.mustNotContain, c.in, got)
	}
}
