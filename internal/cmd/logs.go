// Package cmd implements the logs command.
// REQ-002-007: Diagnostic Commands -- logs
package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"sd/internal/ui"
)

// logEntry represents a single log line for JSON output.
type logEntry struct {
	Line    int    `json:"line"`
	Content string `json:"content"`
	Source  string `json:"source"` // "audit" or "vm"
	VM      string `json:"vm,omitempty"`
}

// readLogFileFunc reads and returns the entire log file content.
// Overridden in tests with a digital twin.
var readLogFileFunc = defaultReadLogFile

func defaultReadLogFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// tailFileFunc streams new lines appended to a file after a starting offset.
// Returns when the context is cancelled or an error occurs.
// Overridden in tests with a digital twin.
var tailFileFunc = defaultTailFile

func defaultTailFile(path string, startOffset int64, w io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Seek(startOffset, io.SeekStart); err != nil {
		return err
	}

	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// Wait briefly then try again
				time.Sleep(500 * time.Millisecond)
				continue
			}
			return err
		}
		fmt.Fprint(w, line)
	}
}

func init() {
	logsCmd := &cobra.Command{
		Use:   "logs [name]",
		Short: "Show logs for a VM or for sd itself",
		Long: `Display logs for sd or a specific VM.

Without a name, shows sd's audit log.
With a name, shows the VM's console log.

Use --tail to show the last N lines (default 50).
Use --follow/-f to stream new log entries in real time.`,
		GroupID: "diagnostics",
		Args:    cobra.MaximumNArgs(1),
		RunE:    runLogs,
	}

	// REQ-002-007: logs flags
	logsCmd.Flags().Int("tail", 50, "show last N lines")
	logsCmd.Flags().BoolP("follow", "f", false, "stream new log entries")

	rootCmd.AddCommand(logsCmd)
}

// runLogs executes the logs command.
// REQ-002-007
func runLogs(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	// REQ-002-007: --tail <n> (default 50)
	tail, _ := cmd.Flags().GetInt("tail")
	if tail < 1 {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: "--tail must be a positive integer",
		}
	}

	// REQ-002-007: --follow/-f
	follow, _ := cmd.Flags().GetBool("follow")

	vmName := ""
	if len(args) > 0 {
		vmName = args[0]
	}

	// Determine log file path and source
	logPath, source, err := resolveLogPath(cmd, vmName)
	if err != nil {
		return err
	}

	// Read the log file
	data, err := readLogFileFunc(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ui.CLIError{
				Code:    "logs_unavailable",
				Message: fmt.Sprintf("log file not found: %s", logPath),
			}
		}
		return ui.CLIError{
			Code:    "logs_unavailable",
			Message: fmt.Sprintf("failed to read log: %v", err),
		}
	}

	content := strings.TrimRight(string(data), "\n")
	var lines []string
	if content == "" {
		lines = []string{}
	} else {
		lines = strings.Split(content, "\n")
	}

	// Apply tail: keep only last N lines
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}

	// REQ-002-007: --follow streams new entries
	if follow && !f.JSONMode() {
		// Print existing lines
		for _, line := range lines {
			fmt.Println(line)
		}
		// Stream new lines from end of file
		offset := int64(len(data))
		return tailFileFunc(logPath, offset, os.Stdout)
	}

	// Output existing entries
	entries := makeLogEntries(lines, source, vmName)

	f.SuccessData(entries, func() string {
		if len(lines) == 0 {
			return "No log entries found.\n"
		}
		return strings.Join(lines, "\n") + "\n"
	})

	return nil
}

// resolveLogPath determines the log file path and source type.
func resolveLogPath(cmd *cobra.Command, vmName string) (path string, source string, err error) {
	if vmName == "" {
		// sd's own audit log
		sdHome := ""
		if l := Loader(); l != nil {
			sdHome = l.SDHome()
		}
		if sdHome == "" {
			return "", "", ui.CLIError{
				Code:    "logs_unavailable",
				Message: "cannot determine SD_HOME for log path",
			}
		}
		return filepath.Join(sdHome, "audit.log"), "audit", nil
	}

	// VM log: verify VM exists via backend
	var backendName string
	if l := Loader(); l != nil {
		cfg := l.Get()
		backendName = cfg.Defaults.Backend
	}
	b, backendErr := getBackendFunc(backendName)
	if backendErr != nil {
		return "", "", ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("no available backend: %v", backendErr),
		}
	}

	if _, statusErr := b.Status(cmd.Context(), vmName); statusErr != nil {
		return "", "", ui.CLIError{
			Code:    "vm_not_found",
			Message: fmt.Sprintf("VM %q not found: %v", vmName, statusErr),
		}
	}

	// Lima stores serial logs at ~/.lima/<name>/serial.log
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".lima", vmName, "serial.log"), "vm", nil
}

// makeLogEntries converts log lines to structured logEntry objects.
func makeLogEntries(lines []string, source, vmName string) []logEntry {
	entries := make([]logEntry, len(lines))
	for i, line := range lines {
		e := logEntry{
			Line:    i + 1,
			Content: line,
			Source:  source,
		}
		if vmName != "" {
			e.VM = vmName
		}
		entries[i] = e
	}
	return entries
}
