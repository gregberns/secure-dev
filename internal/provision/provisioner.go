// Package provision - provisioning execution engine.
// REQ-006-005: Script execution environment.
// REQ-006-010: Re-provisioning existing VMs.
package provision

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ExecFunc runs a command and returns its output. Abstracts backend.Exec for testability.
type ExecFunc func(ctx context.Context, name string, command []string) (stdout, stderr string, exitCode int, err error)

// ProvisionResult holds the outcome of a provisioning run.
type ProvisionResult struct {
	State    ProvisionState `json:"state"`
	Failed   bool           `json:"failed"`
	Module   string         `json:"module,omitempty"`  // module that failed, if any
	Script   int            `json:"script,omitempty"`  // script index that failed, if any
	Error    string         `json:"error,omitempty"`
}

// Provision executes the given modules in order on the named VM using the provided
// exec function. It tracks state per module and aborts on the first script failure.
// REQ-006-005: All scripts execute with set -eux -o pipefail.
// REQ-006-005: system mode runs as root, user mode runs as default user.
func Provision(ctx context.Context, execFn ExecFunc, vmName string, modules []Module) ProvisionResult {
	state := ProvisionState{
		VMName:  vmName,
		Started: time.Now(),
	}
	for i := range modules {
		mod := &modules[i]
		modState := ModuleExecutionStatus{
			Name:   mod.Name,
			Status: StatusRunning,
		}
		startTime := time.Now()
		modState.StartedAt = &startTime
		state.Modules = append(state.Modules, modState)

		// REQ-004-028: Inject checksum env vars before script execution.
		checksumBlock := checksumEnvBlock(mod.Checksums)

		for scriptIdx, s := range mod.Scripts {
			script := checksumBlock + prependScriptPreamble(s.Script)
			var cmd []string
			if s.Mode == ModeSystem {
				cmd = []string{"sudo", "bash", "-c", script}
			} else {
				cmd = []string{"bash", "-c", script}
			}

			stdout, stderr, exitCode, err := execFn(ctx, vmName, cmd)
			_ = stdout
			_ = stderr
			if err != nil {
				endTime := time.Now()
				state.Modules[i].EndedAt = &endTime
				state.Modules[i].Status = StatusFailed
				state.Modules[i].Error = err.Error()
				finishTime := time.Now()
				state.Finished = &finishTime
				return ProvisionResult{
					State:  state,
					Failed: true,
					Module: mod.Name,
					Script: scriptIdx,
					Error:  err.Error(),
				}
			}
			if exitCode != 0 {
				endTime := time.Now()
				state.Modules[i].EndedAt = &endTime
				state.Modules[i].Status = StatusFailed
				state.Modules[i].Error = fmt.Sprintf("script exited with code %d", exitCode)
				finishTime := time.Now()
				state.Finished = &finishTime
				return ProvisionResult{
					State:  state,
					Failed: true,
					Module: mod.Name,
					Script: scriptIdx,
					Error:  fmt.Sprintf("module %q script[%d] failed (exit %d)", mod.Name, scriptIdx, exitCode),
				}
			}
		}

		// REQ-006-008: Execute readiness probe if defined
		if mod.Probe != nil {
			mod.ApplyProbeDefaults()
			if err := runProbe(ctx, execFn, vmName, mod); err != nil {
				endTime := time.Now()
				state.Modules[i].EndedAt = &endTime
				state.Modules[i].Status = StatusFailed
				state.Modules[i].Error = err.Error()
				finishTime := time.Now()
				state.Finished = &finishTime
				return ProvisionResult{
					State:  state,
					Failed: true,
					Module: mod.Name,
					Script: len(mod.Scripts), // probe is after all scripts
					Error:  err.Error(),
				}
			}
		}

		endTime := time.Now()
		state.Modules[i].EndedAt = &endTime
		state.Modules[i].Status = StatusCompleted
	}

	finishTime := time.Now()
	state.Finished = &finishTime
	return ProvisionResult{
		State:  state,
		Failed: false,
	}
}

// runProbe executes a module's readiness probe with retries.
// REQ-006-008: Readiness probes with interval and timeout.
func runProbe(ctx context.Context, execFn ExecFunc, vmName string, mod *Module) error {
	deadline := time.Now().Add(mod.Probe.Timeout)
	cmd := []string{"bash", "-c", mod.Probe.Command}

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("probe timed out for module %q after %s", mod.Name, mod.Probe.Timeout)
		}

		_, _, exitCode, err := execFn(ctx, vmName, cmd)
		if err == nil && exitCode == 0 {
			return nil // probe passed
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(mod.Probe.Interval):
			// retry
		}
	}
}

// prependScriptPreamble adds the required safety flags to a script.
// REQ-006-005: All provisioning scripts execute with set -eux -o pipefail.
func prependScriptPreamble(script string) string {
	return "set -eux -o pipefail\n" + script
}

// checksumEnvBlock generates shell export statements for a module's checksums.
// Each checksum key (typically a filename like "go1.23.4.linux-arm64.tar.gz") is
// converted to an environment variable name with the CHECKSUM_ prefix.
// REQ-004-028: Checksum verification for downloaded binaries.
func checksumEnvBlock(checksums map[string]string) string {
	if len(checksums) == 0 {
		return ""
	}
	keys := make([]string, 0, len(checksums))
	for k := range checksums {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var lines []string
	for _, k := range keys {
		envName := checksumKeyToEnvVar(k)
		lines = append(lines, fmt.Sprintf("export %s=%q", envName, checksums[k]))
	}
	return strings.Join(lines, "\n") + "\n"
}

// checksumKeyToEnvVar converts a checksum filename key to a shell environment
// variable name. Dots and hyphens become underscores; the result is uppercased
// and prefixed with CHECKSUM_.
// Example: "go1.23.4.linux-arm64.tar.gz" -> "CHECKSUM_GO1_23_4_LINUX_ARM64_TAR_GZ"
func checksumKeyToEnvVar(key string) string {
	result := strings.ToUpper(key)
	result = strings.NewReplacer(".", "_", "-", "_").Replace(result)
	return "CHECKSUM_" + result
}

// FormatModuleList renders a module list for human output.
func FormatModuleList(modules []Module) string {
	if len(modules) == 0 {
		return "No modules available.\n"
	}
	var b strings.Builder
	for _, m := range modules {
		deps := "none"
		if len(m.DependsOn) > 0 {
			deps = strings.Join(m.DependsOn, ", ")
		}
		fmt.Fprintf(&b, "  %-15s %s (depends: %s)\n", m.Name, m.Description, deps)
	}
	return b.String()
}

// ModuleListEntry is a simplified representation for JSON output.
type ModuleListEntry struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	DependsOn   []string `json:"depends_on,omitempty"`
	Probe       bool     `json:"has_probe"`
}
