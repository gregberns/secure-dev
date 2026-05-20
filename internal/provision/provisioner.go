// Package provision - provisioning execution engine.
// REQ-006-005: Script execution environment.
// REQ-006-010: Re-provisioning existing VMs.
package provision

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ExecFunc runs a command and returns its output. Abstracts backend.Exec for testability.
type ExecFunc func(ctx context.Context, name string, command []string) (stdout, stderr string, exitCode int, err error)

// ProvisionResult holds the outcome of a provisioning run.
type ProvisionResult struct {
	State  ProvisionState `json:"state"`
	Failed bool           `json:"failed"`
	Module string         `json:"module,omitempty"` // module that failed, if any
	Script int            `json:"script,omitempty"` // script index that failed, if any
	Error  string         `json:"error,omitempty"`
}

// Options configures a provisioning run.
// REQ-006-005: When LogWriter is non-nil, the provisioner appends per-script
// stdout/stderr (with timestamps) for diagnostic history.
type Options struct {
	// LogWriter receives a chronological transcript of every script the
	// provisioner runs. May be nil.
	LogWriter io.Writer
}

// scriptLogTailLines is the number of trailing lines of stdout/stderr included
// in script-failure error messages (REQ-006-005).
const scriptLogTailLines = 50

// Provision executes the given modules in order on the named VM using the provided
// exec function. It tracks state per module and aborts on the first script failure.
// REQ-006-005: All scripts execute with set -eu -o pipefail.
// REQ-006-005: system mode runs as root, user mode runs as default user.
func Provision(ctx context.Context, execFn ExecFunc, vmName string, modules []Module) ProvisionResult {
	return ProvisionWithOptions(ctx, execFn, vmName, modules, Options{})
}

// ProvisionWithOptions is Provision with additional options (such as a log
// writer for capturing per-script stdout/stderr). REQ-006-005.
func ProvisionWithOptions(ctx context.Context, execFn ExecFunc, vmName string, modules []Module, opts Options) ProvisionResult {
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

			// REQ-006-005: Append script transcript to the provisioning log
			// (if configured) so diagnostic history is preserved even on
			// errors below.
			writeProvisionLog(opts.LogWriter, mod.Name, scriptIdx, s.Mode, stdout, stderr, exitCode, err)

			if err != nil {
				endTime := time.Now()
				state.Modules[i].EndedAt = &endTime
				state.Modules[i].Status = StatusFailed
				msg := formatScriptFailure(mod.Name, scriptIdx, 0, stdout, stderr, err)
				state.Modules[i].Error = msg
				finishTime := time.Now()
				state.Finished = &finishTime
				return ProvisionResult{
					State:  state,
					Failed: true,
					Module: mod.Name,
					Script: scriptIdx,
					Error:  msg,
				}
			}
			if exitCode != 0 {
				endTime := time.Now()
				state.Modules[i].EndedAt = &endTime
				state.Modules[i].Status = StatusFailed
				msg := formatScriptFailure(mod.Name, scriptIdx, exitCode, stdout, stderr, nil)
				state.Modules[i].Error = msg
				finishTime := time.Now()
				state.Finished = &finishTime
				return ProvisionResult{
					State:  state,
					Failed: true,
					Module: mod.Name,
					Script: scriptIdx,
					Error:  msg,
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

// formatScriptFailure builds an actionable error message for a failed script,
// including the trailing stdout and stderr so users can diagnose without
// digging through logs. REQ-006-005.
func formatScriptFailure(modName string, scriptIdx, exitCode int, stdout, stderr string, execErr error) string {
	var b strings.Builder
	if execErr != nil {
		fmt.Fprintf(&b, "module %q script[%d] exec error: %v", modName, scriptIdx, execErr)
	} else {
		fmt.Fprintf(&b, "module %q script[%d] failed (exit %d)", modName, scriptIdx, exitCode)
	}
	if tail := trimmedTail(stderr, scriptLogTailLines); tail != "" {
		fmt.Fprintf(&b, "\n--- stderr (last %d lines) ---\n%s", scriptLogTailLines, redactCredentials(tail))
	}
	if tail := trimmedTail(stdout, scriptLogTailLines); tail != "" {
		fmt.Fprintf(&b, "\n--- stdout (last %d lines) ---\n%s", scriptLogTailLines, redactCredentials(tail))
	}
	return b.String()
}

// trimmedTail returns the last n lines of s, with any trailing newline
// stripped first to avoid emitting a phantom blank line. Returns "" if s is
// empty or only whitespace.
func trimmedTail(s string, n int) string {
	if s == "" {
		return ""
	}
	trimmed := strings.TrimRight(s, "\n")
	if trimmed == "" {
		return ""
	}
	return tailLines(trimmed, n)
}

// writeProvisionLog appends a per-script transcript section to w. Errors are
// silently dropped: log failures must not abort provisioning. REQ-006-005.
func writeProvisionLog(w io.Writer, modName string, scriptIdx int, mode ScriptMode, stdout, stderr string, exitCode int, execErr error) {
	if w == nil {
		return
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] module=%s script=%d mode=%s exit=%d", ts, modName, scriptIdx, mode, exitCode)
	if execErr != nil {
		fmt.Fprintf(&b, " exec_err=%v", execErr)
	}
	b.WriteString("\n")
	// REQ-006-005, REQ-004-024: redact credentials before persisting to disk.
	if stdout != "" {
		redacted := redactCredentials(stdout)
		b.WriteString("--- stdout ---\n")
		b.WriteString(redacted)
		if !strings.HasSuffix(redacted, "\n") {
			b.WriteString("\n")
		}
	}
	if stderr != "" {
		redacted := redactCredentials(stderr)
		b.WriteString("--- stderr ---\n")
		b.WriteString(redacted)
		if !strings.HasSuffix(redacted, "\n") {
			b.WriteString("\n")
		}
	}
	_, _ = w.Write([]byte(b.String()))
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
// REQ-006-005: All provisioning scripts execute with `set -eu -o pipefail`.
// We intentionally drop `-x` (xtrace) because trace output echoes expanded
// environment variables and command lines (including credentials like
// ANTHROPIC_API_KEY or GITHUB_TOKEN) to stderr, which then lands in
// provision.log and can leak into error envelopes (CWE-532).
func prependScriptPreamble(script string) string {
	return "set -eu -o pipefail\n" + script
}

// credentialRedactor matches well-known credential token patterns. Substrings
// matching these are replaced with `[REDACTED]` before being written to the
// provisioning log or returned in error envelopes. REQ-004-024, REQ-006-005.
//
// Patterns covered:
//   - GitHub PATs (ghp_, github_pat_, gho_)
//   - Anthropic API keys (sk-ant-...)
//   - Slack bot tokens (xoxb-...)
//   - AWS access key IDs (AKIA[A-Z0-9]+)
//   - "Bearer <token>" Authorization values
//   - Generic Authorization: header values
//   - URL userinfo (e.g. https://user:pass@host/...)
var credentialPatterns = []*regexp.Regexp{
	regexp.MustCompile(`ghp_[A-Za-z0-9_]{8,}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{8,}`),
	regexp.MustCompile(`gho_[A-Za-z0-9_]{8,}`),
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{8,}`),
	regexp.MustCompile(`xoxb-[A-Za-z0-9\-]{8,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{12,}`),
	regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9_\-\.=:+/]{8,}`),
	regexp.MustCompile(`(?i)Authorization:\s*\S+`),
	regexp.MustCompile(`([A-Za-z0-9+\-]+)://([^:/\s]+):([^@\s]+)@`),
}

// redactCredentials returns s with any matched credential patterns replaced by
// `[REDACTED]`. Applied before writing provision logs and embedding script
// output into error messages. REQ-004-024, REQ-006-005.
func redactCredentials(s string) string {
	if s == "" {
		return s
	}
	for _, re := range credentialPatterns {
		// For URL userinfo we want to preserve scheme://host but strip the
		// embedded user:pass. The other patterns simply replace the whole
		// match with [REDACTED].
		if strings.HasPrefix(re.String(), "([A-Za-z0-9") {
			s = re.ReplaceAllString(s, "$1://[REDACTED]@")
			continue
		}
		s = re.ReplaceAllString(s, "[REDACTED]")
	}
	return s
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
