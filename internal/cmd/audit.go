// Package cmd implements the audit command.
// REQ-002-008: Security Commands -- audit
// REQ-004-021: Audit Logging — Command Logging
// REQ-004-022: Audit Logging — VM Lifecycle Events with Hash Chain
package cmd

import (
	"bytes"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"sd/internal/security"
	"sd/internal/ui"
)

// newAuditLoggerFunc creates a new AuditLogger for the given SD_HOME directory.
// Overridden in tests with a digital twin.
var newAuditLoggerFunc = defaultNewAuditLogger

func defaultNewAuditLogger(sdHome string) *security.AuditLogger {
	path := filepath.Join(sdHome, "audit.log")
	return security.NewAuditLogger(path)
}

func init() {
	auditCmd := &cobra.Command{
		Use:   "audit [<vm>]",
		Short: "Query the audit log for operations",
		Long: `Query the audit log, optionally filtered to a specific VM.
Use --verify to check hash chain integrity.
Use --since to filter events after a timestamp.`,
		GroupID: "security",
		Args:    cobra.MaximumNArgs(1),
		RunE:    runAudit,
	}

	// REQ-002-008: audit flags
	auditCmd.Flags().String("since", "", "filter events after ISO 8601 timestamp (e.g., 2026-03-27T00:00:00Z)")
	auditCmd.Flags().Bool("verify", false, "verify hash chain integrity")

	rootCmd.AddCommand(auditCmd)
}

// runAudit executes the audit command.
// REQ-002-008, REQ-004-021, REQ-004-022
func runAudit(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	// Determine SD_HOME from config loader
	sdHome := ""
	if l := Loader(); l != nil {
		sdHome = l.SDHome()
	}
	if sdHome == "" {
		return ui.CLIError{
			Code:    "config_not_found",
			Message: "cannot determine SD_HOME for audit log path",
		}
	}

	logger := newAuditLoggerFunc(sdHome)

	// REQ-004-022: --verify flag validates hash chain integrity
	verify, _ := cmd.Flags().GetBool("verify")
	if verify {
		chainBreak, err := logger.VerifyChain()
		if err != nil {
			return ui.CLIError{
				Code:    "audit_query_failed",
				Message: fmt.Sprintf("failed to verify audit log: %v", err),
			}
		}
		if chainBreak != nil {
			return ui.CLIError{
				Code:    "audit_chain_broken",
				Message: fmt.Sprintf("hash chain broken at line %d: expected %s, got %s",
					chainBreak.LineNumber, chainBreak.ExpectedHash, chainBreak.ActualHash),
			}
		}

		type verifyResult struct {
			Verified bool `json:"verified"`
		}

		f.SuccessData(verifyResult{Verified: true}, func() string {
			return "Hash chain integrity verified.\n"
		})
		return nil
	}

	// Build filter from args and flags
	filter := security.AuditFilter{}

	// Optional VM name filter
	if len(args) > 0 && args[0] != "" {
		vmName := args[0]
		filter.VMName = &vmName
	}

	// REQ-002-008: --since flag for time filtering
	sinceStr, _ := cmd.Flags().GetString("since")
	if sinceStr != "" {
		since, err := time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			return ui.CLIError{
				Code:    "invalid_argument",
				Message: fmt.Sprintf("invalid --since timestamp %q: expected ISO 8601 format (e.g., 2026-03-27T00:00:00Z)", sinceStr),
			}
		}
		filter.Since = &since
	}

	entries, err := logger.Query(filter)
	if err != nil {
		return ui.CLIError{
			Code:    "audit_query_failed",
			Message: fmt.Sprintf("failed to query audit log: %v", err),
		}
	}

	// Ensure non-nil slice for JSON serialization ([] not null)
	if entries == nil {
		entries = []security.AuditEntry{}
	}

	f.SuccessData(entries, func() string {
		return formatAuditEntries(entries)
	})
	return nil
}

// formatAuditEntries renders audit entries as human-readable output.
func formatAuditEntries(entries []security.AuditEntry) string {
	if len(entries) == 0 {
		return "No audit entries found.\n"
	}

	var buf bytes.Buffer
	for _, e := range entries {
		ts := e.Timestamp.Format("2006-01-02T15:04:05Z07:00")
		switch e.Type {
		case "command":
			fmt.Fprintf(&buf, "%s  command  %s %v  (exit=%d, duration=%dms)\n",
				ts, e.Command, e.Args, e.ExitCode, e.DurationMs)
		case "event":
			metaStr := ""
			if len(e.Meta) > 0 {
				var metaBuf bytes.Buffer
				first := true
				for k, v := range e.Meta {
					if !first {
						metaBuf.WriteString(", ")
					}
					fmt.Fprintf(&metaBuf, "%s=%s", k, v)
					first = false
				}
				metaStr = "  " + metaBuf.String()
			}
			fmt.Fprintf(&buf, "%s  event    %s  vm=%s%s\n",
				ts, e.EventType, e.VM, metaStr)
		}
	}
	return buf.String()
}
