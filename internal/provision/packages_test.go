// Package provision provides tests for declarative package installation.
// REQ-009-005: Prerequisite validation.
// REQ-009-006: Package installation execution.
// REQ-009-013: Failure handling.
package provision

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sd/internal/config"
)

// --- Prerequisite Validation Tests (REQ-009-005) ---

func TestInstallPackages_NilPackages(t *testing.T) {
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		t.Fatal("execFn should not be called for nil packages")
		return "", "", 0, nil
	}
	err := InstallPackages(context.Background(), execFn, "test-vm", nil)
	assert.NoError(t, err)
}

func TestInstallPackages_EmptyPackages(t *testing.T) {
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		t.Fatal("execFn should not be called for empty packages")
		return "", "", 0, nil
	}
	err := InstallPackages(context.Background(), execFn, "test-vm", &config.PackageConfig{})
	assert.NoError(t, err)
}

func TestInstallPackages_PipPrereqMissing(t *testing.T) {
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		script := command[len(command)-1]
		if script == "which pip3" {
			return "", "", 1, nil // pip3 not found
		}
		return "", "", 0, nil
	}
	pkgs := &config.PackageConfig{Pip: []string{"black"}}
	err := InstallPackages(context.Background(), execFn, "test-vm", pkgs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `pip packages require the "python" module`)
}

func TestInstallPackages_NpmPrereqMissing(t *testing.T) {
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		script := command[len(command)-1]
		if script == "which npm" {
			return "", "", 1, nil
		}
		return "", "", 0, nil
	}
	pkgs := &config.PackageConfig{Npm: []string{"prettier"}}
	err := InstallPackages(context.Background(), execFn, "test-vm", pkgs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `npm packages require the "claude-code" or a Node.js module`)
}

func TestInstallPackages_GoPrereqMissing(t *testing.T) {
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		script := command[len(command)-1]
		if script == "which go" {
			return "", "", 1, nil
		}
		return "", "", 0, nil
	}
	pkgs := &config.PackageConfig{Go: []string{"golang.org/x/tools/cmd/goimports@latest"}}
	err := InstallPackages(context.Background(), execFn, "test-vm", pkgs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `go packages require the "golang" module`)
}

func TestInstallPackages_CargoPrereqMissing(t *testing.T) {
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		script := command[len(command)-1]
		if script == "which cargo" {
			return "", "", 1, nil
		}
		return "", "", 0, nil
	}
	pkgs := &config.PackageConfig{Cargo: []string{"ripgrep"}}
	err := InstallPackages(context.Background(), execFn, "test-vm", pkgs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `cargo packages require the "rust" module`)
}

func TestInstallPackages_AptNoPrereqCheck(t *testing.T) {
	// apt should never check prerequisites -- it's always available.
	var scripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		scripts = append(scripts, command[len(command)-1])
		return "", "", 0, nil
	}
	pkgs := &config.PackageConfig{Apt: []string{"jq"}}
	err := InstallPackages(context.Background(), execFn, "test-vm", pkgs)
	require.NoError(t, err)
	// Should NOT have a "which" command for apt
	for _, s := range scripts {
		assert.NotContains(t, s, "which", "apt should not check prerequisites")
	}
}

func TestInstallPackages_PrereqOnlyCheckedForPresentManagers(t *testing.T) {
	// If only apt packages are declared, pip prereq should not be checked.
	var commands []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		commands = append(commands, command[len(command)-1])
		return "", "", 0, nil
	}
	pkgs := &config.PackageConfig{Apt: []string{"jq"}}
	err := InstallPackages(context.Background(), execFn, "test-vm", pkgs)
	require.NoError(t, err)
	for _, c := range commands {
		assert.NotContains(t, c, "which pip3")
		assert.NotContains(t, c, "which npm")
		assert.NotContains(t, c, "which go")
		assert.NotContains(t, c, "which cargo")
	}
}

// --- Installation Order Tests (REQ-009-006) ---

func TestInstallPackages_OrderAptPipNpmGoCargo(t *testing.T) {
	var scripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		scripts = append(scripts, command[len(command)-1])
		return "", "", 0, nil
	}

	pkgs := &config.PackageConfig{
		Apt:   []string{"jq"},
		Pip:   []string{"black"},
		Npm:   []string{"prettier"},
		Go:    []string{"golang.org/x/tools/cmd/goimports@latest"},
		Cargo: []string{"ripgrep"},
	}
	err := InstallPackages(context.Background(), execFn, "test-vm", pkgs)
	require.NoError(t, err)

	// Find the install scripts (skip prereq checks).
	var installScripts []string
	for _, s := range scripts {
		if strings.Contains(s, "apt-get") ||
			strings.Contains(s, "pip3 install") ||
			strings.Contains(s, "npm install") ||
			strings.Contains(s, "go install") ||
			strings.Contains(s, "cargo install") {
			installScripts = append(installScripts, s)
		}
	}

	// Expected order: apt-get update, apt-get install, pip3 install, npm install, go install, cargo install
	require.GreaterOrEqual(t, len(installScripts), 6, "expected at least 6 install scripts, got %d", len(installScripts))
	assert.Contains(t, installScripts[0], "apt-get update")
	assert.Contains(t, installScripts[1], "apt-get install -y")
	assert.Contains(t, installScripts[2], "pip3 install --user")
	assert.Contains(t, installScripts[3], "npm install -g")
	assert.Contains(t, installScripts[4], "go install")
	assert.Contains(t, installScripts[5], "cargo install")
}

func TestInstallPackages_AptBatched(t *testing.T) {
	var scripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		scripts = append(scripts, command[len(command)-1])
		return "", "", 0, nil
	}
	pkgs := &config.PackageConfig{Apt: []string{"jq", "curl", "git"}}
	err := InstallPackages(context.Background(), execFn, "test-vm", pkgs)
	require.NoError(t, err)

	// Find the apt install script
	var installScript string
	for _, s := range scripts {
		if strings.Contains(s, "apt-get install") {
			installScript = s
			break
		}
	}
	require.NotEmpty(t, installScript, "expected apt-get install script")
	assert.Contains(t, installScript, "jq")
	assert.Contains(t, installScript, "curl")
	assert.Contains(t, installScript, "git")
}

func TestInstallPackages_PipBatched(t *testing.T) {
	var scripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		scripts = append(scripts, command[len(command)-1])
		return "", "", 0, nil
	}
	pkgs := &config.PackageConfig{Pip: []string{"black", "mypy"}}
	err := InstallPackages(context.Background(), execFn, "test-vm", pkgs)
	require.NoError(t, err)

	var installScript string
	for _, s := range scripts {
		if strings.Contains(s, "pip3 install") {
			installScript = s
			break
		}
	}
	require.NotEmpty(t, installScript)
	assert.Contains(t, installScript, "black mypy")
}

func TestInstallPackages_GoPerPackage(t *testing.T) {
	var scripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		scripts = append(scripts, command[len(command)-1])
		return "", "", 0, nil
	}
	pkgs := &config.PackageConfig{Go: []string{"pkg1@latest", "pkg2@latest"}}
	err := InstallPackages(context.Background(), execFn, "test-vm", pkgs)
	require.NoError(t, err)

	var goInstalls []string
	for _, s := range scripts {
		if strings.Contains(s, "go install") {
			goInstalls = append(goInstalls, s)
		}
	}
	// Each go package should get its own invocation.
	require.Len(t, goInstalls, 2)
	assert.Contains(t, goInstalls[0], "pkg1@latest")
	assert.Contains(t, goInstalls[1], "pkg2@latest")
}

func TestInstallPackages_ScriptHasPreamble(t *testing.T) {
	var scripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		scripts = append(scripts, command[len(command)-1])
		return "", "", 0, nil
	}
	pkgs := &config.PackageConfig{Apt: []string{"jq"}}
	err := InstallPackages(context.Background(), execFn, "test-vm", pkgs)
	require.NoError(t, err)

	for _, s := range scripts {
		if strings.Contains(s, "apt-get") {
			assert.True(t, strings.HasPrefix(s, "set -eux -o pipefail\n"),
				"install script must start with safety preamble")
		}
	}
}

// --- Failure Handling Tests (REQ-009-013) ---

func TestInstallPackages_AptFailure_ReportsError(t *testing.T) {
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		script := command[len(command)-1]
		if strings.Contains(script, "apt-get install") {
			return "", "E: Unable to locate package nonexistent-pkg\nMore output here", 100, nil
		}
		return "", "", 0, nil
	}
	pkgs := &config.PackageConfig{Apt: []string{"nonexistent-pkg"}}
	err := InstallPackages(context.Background(), execFn, "test-vm", pkgs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "package installation failed (apt)")
	assert.Contains(t, err.Error(), "exit code 100")
}

func TestInstallPackages_PipFailurePreventsSubsequent(t *testing.T) {
	var scripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		scripts = append(scripts, command[len(command)-1])
		script := command[len(command)-1]
		if strings.Contains(script, "pip3 install") {
			return "", "error installing", 1, nil
		}
		return "", "", 0, nil
	}
	pkgs := &config.PackageConfig{
		Pip: []string{"bad-pkg"},
		Npm: []string{"prettier"},
	}
	err := InstallPackages(context.Background(), execFn, "test-vm", pkgs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pip")

	// npm should never have been attempted
	for _, s := range scripts {
		assert.NotContains(t, s, "npm install", "npm should not run after pip failure")
	}
}

func TestInstallPackages_ExecError(t *testing.T) {
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		script := command[len(command)-1]
		if strings.Contains(script, "apt-get update") {
			return "", "", 0, fmt.Errorf("connection lost")
		}
		return "", "", 0, nil
	}
	pkgs := &config.PackageConfig{Apt: []string{"jq"}}
	err := InstallPackages(context.Background(), execFn, "test-vm", pkgs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection lost")
}

func TestInstallPackages_Last20Lines(t *testing.T) {
	// Verify that error output is truncated to last 20 lines.
	var longOutput strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&longOutput, "line %d\n", i)
	}
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		script := command[len(command)-1]
		if strings.Contains(script, "apt-get install") {
			return "", longOutput.String(), 1, nil
		}
		return "", "", 0, nil
	}
	pkgs := &config.PackageConfig{Apt: []string{"bad-pkg"}}
	err := InstallPackages(context.Background(), execFn, "test-vm", pkgs)
	require.Error(t, err)
	// The first 10 lines should be truncated
	assert.NotContains(t, err.Error(), "line 0\n")
	assert.NotContains(t, err.Error(), "line 9\n")
	// The last lines should be present
	assert.Contains(t, err.Error(), "line 29")
}

// --- tailLines Tests ---

func TestTailLines_ShortInput(t *testing.T) {
	result := tailLines("a\nb\nc", 5)
	assert.Equal(t, "a\nb\nc", result)
}

func TestTailLines_ExactLines(t *testing.T) {
	result := tailLines("a\nb\nc", 3)
	assert.Equal(t, "a\nb\nc", result)
}

func TestTailLines_Truncated(t *testing.T) {
	result := tailLines("a\nb\nc\nd\ne", 3)
	assert.Equal(t, "c\nd\ne", result)
}

func TestTailLines_Empty(t *testing.T) {
	result := tailLines("", 5)
	assert.Equal(t, "", result)
}

// --- AptGetUpdate runs before install ---

func TestInstallPackages_AptUpdateBeforeInstall(t *testing.T) {
	var scripts []string
	execFn := func(ctx context.Context, name string, command []string) (string, string, int, error) {
		scripts = append(scripts, command[len(command)-1])
		return "", "", 0, nil
	}
	pkgs := &config.PackageConfig{Apt: []string{"jq"}}
	err := InstallPackages(context.Background(), execFn, "test-vm", pkgs)
	require.NoError(t, err)

	updateIdx := -1
	installIdx := -1
	for i, s := range scripts {
		if strings.Contains(s, "apt-get update") {
			updateIdx = i
		}
		if strings.Contains(s, "apt-get install") {
			installIdx = i
		}
	}
	require.NotEqual(t, -1, updateIdx, "apt-get update must run")
	require.NotEqual(t, -1, installIdx, "apt-get install must run")
	assert.Less(t, updateIdx, installIdx, "apt-get update must run before apt-get install")
}
