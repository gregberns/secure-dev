// Package provision implements declarative package installation.
// REQ-009-005: Runtime prerequisite validation.
// REQ-009-006: Package installation execution.
// REQ-009-013: Failure handling with last 20 lines of output.
package provision

import (
	"context"
	"fmt"
	"strings"

	"sd/internal/config"
)

// packageManager defines how a package type is installed.
// REQ-009-006
type packageManager struct {
	Name           string
	PrereqCommand  string // "which pip3", empty if always available
	PrereqError    string // actionable error if prerequisite missing
	InstallCommand string // "pip3 install --user"
	UpdateCommand  string // "sudo apt-get update", empty if none
	BatchInstall   bool   // whether packages can be combined in one invocation
}

// managers defines the fixed installation order: apt, pip, npm, go, cargo.
// REQ-009-006
var managers = []packageManager{
	{
		Name:           "apt",
		InstallCommand: "sudo apt-get install -y",
		UpdateCommand:  "sudo apt-get update -y",
		BatchInstall:   true,
	},
	{
		Name:           "pip",
		PrereqCommand:  "which pip3",
		PrereqError:    `pip packages require the "python" module -- add it to your modules list`,
		InstallCommand: "pip3 install --user",
		BatchInstall:   true,
	},
	{
		Name:           "npm",
		PrereqCommand:  "which npm",
		PrereqError:    `npm packages require the "claude-code" or a Node.js module -- add one to your modules list`,
		InstallCommand: "npm install -g",
		BatchInstall:   true,
	},
	{
		Name:           "go",
		PrereqCommand:  "which go",
		PrereqError:    `go packages require the "golang" module -- add it to your modules list`,
		InstallCommand: "go install",
		BatchInstall:   false,
	},
	{
		Name:           "cargo",
		PrereqCommand:  "which cargo",
		PrereqError:    `cargo packages require the "rust" module -- add it to your modules list`,
		InstallCommand: "cargo install",
		BatchInstall:   true,
	},
}

// packagesForManager returns the package list for the given manager name.
func packagesForManager(pkgs *config.PackageConfig, name string) []string {
	if pkgs == nil {
		return nil
	}
	switch name {
	case "apt":
		return pkgs.Apt
	case "pip":
		return pkgs.Pip
	case "npm":
		return pkgs.Npm
	case "go":
		return pkgs.Go
	case "cargo":
		return pkgs.Cargo
	default:
		return nil
	}
}

// tailLines returns the last n lines from s.
func tailLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// InstallPackages validates prerequisites and installs packages in the
// fixed order: apt, pip, npm, go, cargo.
// REQ-009-005: Prerequisite validation before installation.
// REQ-009-006: Package installation with set -eu -o pipefail.
// REQ-009-013: Fail-fast with last 20 lines on error.
func InstallPackages(ctx context.Context, execFn ExecFunc, vmName string, pkgs *config.PackageConfig) error {
	if pkgs == nil {
		return nil
	}

	// REQ-009-005: Validate prerequisites for all managers that have packages.
	for _, mgr := range managers {
		pkgList := packagesForManager(pkgs, mgr.Name)
		if len(pkgList) == 0 {
			continue
		}
		if mgr.PrereqCommand != "" {
			cmd := []string{"bash", "-c", mgr.PrereqCommand}
			_, _, exitCode, err := execFn(ctx, vmName, cmd)
			if err != nil {
				return fmt.Errorf("prerequisite check for %s failed: %v", mgr.Name, err)
			}
			if exitCode != 0 {
				return fmt.Errorf("%s", mgr.PrereqError)
			}
		}
	}

	// REQ-009-006: Install in order: apt, pip, npm, go, cargo.
	for _, mgr := range managers {
		pkgList := packagesForManager(pkgs, mgr.Name)
		if len(pkgList) == 0 {
			continue
		}

		// Run update command if defined (e.g., apt-get update).
		if mgr.UpdateCommand != "" {
			script := "set -eu -o pipefail\n" + mgr.UpdateCommand
			cmd := []string{"bash", "-c", script}
			_, stderr, exitCode, err := execFn(ctx, vmName, cmd)
			if err != nil {
				return fmt.Errorf("package installation failed (%s): %v", mgr.Name, err)
			}
			if exitCode != 0 {
				return fmt.Errorf("package installation failed (%s): exit code %d\n%s",
					mgr.Name, exitCode, tailLines(stderr, 20))
			}
		}

		if mgr.BatchInstall {
			// Batch: single invocation with all packages.
			// Shell-quote each package name to prevent injection.
			quoted := make([]string, len(pkgList))
			for i, p := range pkgList {
				quoted[i] = shellQuote(p)
			}
			script := "set -eu -o pipefail\n" + mgr.InstallCommand + " " + strings.Join(quoted, " ")
			cmd := []string{"bash", "-c", script}
			_, stderr, exitCode, err := execFn(ctx, vmName, cmd)
			if err != nil {
				return fmt.Errorf("package installation failed (%s): %v", mgr.Name, err)
			}
			if exitCode != 0 {
				return fmt.Errorf("package installation failed (%s): exit code %d\n%s",
					mgr.Name, exitCode, tailLines(stderr, 20))
			}
		} else {
			// Per-package: one invocation per package (e.g., go install).
			// Shell-quote each package name to prevent injection.
			for _, pkg := range pkgList {
				script := "set -eu -o pipefail\n" + mgr.InstallCommand + " " + shellQuote(pkg)
				cmd := []string{"bash", "-c", script}
				_, stderr, exitCode, err := execFn(ctx, vmName, cmd)
				if err != nil {
					return fmt.Errorf("package installation failed (%s): %v", mgr.Name, err)
				}
				if exitCode != 0 {
					return fmt.Errorf("package installation failed (%s): exit code %d\n%s",
						mgr.Name, exitCode, tailLines(stderr, 20))
				}
			}
		}
	}

	return nil
}
