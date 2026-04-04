// Package cmd implements the security status command.
// REQ-004-024: Security Posture Summary
package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"sd/internal/backend"
	"sd/internal/security"
	"sd/internal/ui"
)

func init() {
	securityCmd := &cobra.Command{
		Use:   "security",
		Short: "Security commands",
		Long: `Security-related commands for inspecting and managing VM security posture.

Subcommands:
  status <vm>  Show security posture summary for a VM`,
		GroupID: "security",
	}

	securityStatusCmd := &cobra.Command{
		Use:   "status <name>",
		Short: "Show security posture summary for a VM",
		Long: `Display a summary of the security posture for a named VM, including
mount status, egress rules, configured credentials, snapshot count,
and the last audit event. Deviations from recommended posture are
flagged as warnings.`,
		Args: exactArgs(1, "<name>"),
		RunE: runSecurityStatus,
	}

	securityCmd.AddCommand(securityStatusCmd)
	rootCmd.AddCommand(securityCmd)
}

// securityStatusData is the structured JSON output for security status.
// REQ-004-024
type securityStatusData struct {
	VM           string              `json:"vm"`
	Status       string              `json:"status"`
	Mounts       []mountInfo         `json:"mounts"`
	Egress       egressInfo          `json:"egress"`
	Credentials  []credentialStatus  `json:"credentials"`
	Snapshots    snapshotSummary      `json:"snapshots"`
	LastAudit    *lastAuditEvent     `json:"last_audit,omitempty"`
	Warnings     []securityWarning   `json:"warnings"`
}

// mountInfo describes a single mount on a VM.
type mountInfo struct {
	HostPath  string `json:"host_path"`
	GuestPath string `json:"guest_path"`
	Mode      string `json:"mode"`
}

// egressInfo describes the egress configuration for a VM.
type egressInfo struct {
	Count       int                       `json:"count"`
	Default     int                       `json:"default_count"`
	User        int                       `json:"user_count"`
	Domains     []security.EgressDomain   `json:"domains"`
}

// credentialStatus describes whether a credential type is configured.
type credentialStatus struct {
	Name       string `json:"name"`
	Label      string `json:"label"`
	Configured bool   `json:"configured"`
}

// snapshotSummary describes the snapshot state for a VM.
type snapshotSummary struct {
	Count int    `json:"count"`
	Error string `json:"error,omitempty"`
}

// lastAuditEvent describes the most recent audit entry for a VM.
type lastAuditEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Summary   string    `json:"summary"`
}

// securityWarning represents a security posture deviation.
type securityWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// runSecurityStatus executes the security status command.
// REQ-004-024
func runSecurityStatus(cmd *cobra.Command, args []string) error {
	f := Formatter()
	if f == nil {
		f = ui.NewFormatter(false)
	}

	name := args[0]
	if name == "" {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: "VM name must not be empty",
		}
	}

	// REQ-001-006: validate VM name format
	if err := backend.ValidateVMName(name); err != nil {
		return ui.CLIError{
			Code:    "invalid_argument",
			Message: err.Error(),
		}
	}

	// Resolve backend
	backendName := ""
	if Loader() != nil {
		cfg := Loader().Get()
		backendName = cfg.Defaults.Backend
	}
	if backendName == "" {
		backendName = "lima"
	}

	b, err := getBackendFunc(backendName)
	if err != nil {
		return ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("backend %q is not available: %v", backendName, err),
		}
	}

	if err := b.Available(); err != nil {
		return ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("backend %q not available: %v", backendName, err),
		}
	}

	// Check VM exists
	status, err := b.Status(cmd.Context(), name)
	if err != nil {
		if errors.Is(err, backend.ErrVMNotFound) {
			return ui.CLIError{
				Code:    "vm_not_found",
				Message: fmt.Sprintf("VM %q does not exist", name),
			}
		}
		return ui.CLIError{
			Code:    "backend_unavailable",
			Message: fmt.Sprintf("failed to get status for VM %q: %v", name, err),
		}
	}

	var warnings []securityWarning

	// Gather mount info from VM config
	mounts := gatherMounts(name, &warnings)

	// Gather egress info from config
	egress := gatherEgress(&warnings)

	// Gather credential info from VM config
	creds := gatherCredentials(name, &warnings)

	// Gather snapshot count
	snaps := gatherSnapshotSummary(cmd, b, name)

	// Gather last audit event
	lastAudit := gatherLastAuditEvent(name)

	// Check for no credentials warning
	hasAnyCred := false
	for _, c := range creds {
		if c.Configured {
			hasAnyCred = true
			break
		}
	}
	if !hasAnyCred {
		warnings = append(warnings, securityWarning{
			Code:    "no_credentials",
			Message: "no credentials configured; sessions will start without API tokens",
		})
	}

	// Check for no snapshots warning
	if snaps.Count == 0 && snaps.Error == "" {
		warnings = append(warnings, securityWarning{
			Code:    "no_snapshots",
			Message: "no snapshots exist; consider creating one before destructive operations",
		})
	}

	if warnings == nil {
		warnings = []securityWarning{}
	}

	data := securityStatusData{
		VM:          name,
		Status:      string(status),
		Mounts:      mounts,
		Egress:      egress,
		Credentials: creds,
		Snapshots:   snaps,
		LastAudit:   lastAudit,
		Warnings:    warnings,
	}

	f.SuccessData(data, func() string {
		return formatSecurityStatus(data)
	})
	return nil
}

// gatherMounts collects mount information from the VM config and generates warnings.
func gatherMounts(vmName string, warnings *[]securityWarning) []mountInfo {
	var mounts []mountInfo

	sdHome := ""
	if l := Loader(); l != nil {
		sdHome = l.SDHome()
	}
	if sdHome == "" {
		return mounts
	}

	vmCfg, err := Loader().GetForVM(vmName)
	if err != nil || vmCfg == nil {
		return mounts
	}

	// Check VM config for mount-related backend meta
	if vmCfg.BackendMeta != nil {
		if mountsRaw, ok := vmCfg.BackendMeta["mounts"]; ok {
			if mountsList, ok := mountsRaw.([]interface{}); ok {
				for _, m := range mountsList {
					if mMap, ok := m.(map[string]interface{}); ok {
						mi := mountInfo{}
						if hp, ok := mMap["host_path"].(string); ok {
							mi.HostPath = hp
						}
						if gp, ok := mMap["guest_path"].(string); ok {
							mi.GuestPath = gp
						}
						if mode, ok := mMap["mode"].(string); ok {
							mi.Mode = mode
						} else {
							mi.Mode = "ro"
						}
						mounts = append(mounts, mi)

						// REQ-004-024: Warn on writable mounts
						if mi.Mode == "rw" {
							*warnings = append(*warnings, securityWarning{
								Code:    "writable_mount",
								Message: fmt.Sprintf("writable mount detected: %s -> %s", mi.HostPath, mi.GuestPath),
							})
						}
					}
				}
			}
		}
	}

	if mounts == nil {
		mounts = []mountInfo{}
	}

	return mounts
}

// gatherEgress collects egress allowlist information from config.
func gatherEgress(warnings *[]securityWarning) egressInfo {
	var userDomains []string
	if Loader() != nil {
		cfg := Loader().Get()
		userDomains = cfg.Security.EgressAllowlist
	}

	allDomains := security.BuildEgressList(userDomains)

	defaultCount := 0
	userCount := 0
	for _, d := range allDomains {
		switch d.Source {
		case security.DomainSourceDefault:
			defaultCount++
		case security.DomainSourceUser:
			userCount++
		}
	}

	return egressInfo{
		Count:   len(allDomains),
		Default: defaultCount,
		User:    userCount,
		Domains: allDomains,
	}
}

// gatherCredentials collects credential status from VM config.
func gatherCredentials(vmName string, warnings *[]securityWarning) []credentialStatus {
	var creds []credentialStatus

	sdHome := ""
	if l := Loader(); l != nil {
		sdHome = l.SDHome()
	}
	if sdHome == "" {
		for _, key := range knownCredentialKeys {
			creds = append(creds, credentialStatus{
				Name:       key,
				Label:      credentialLabels[key],
				Configured: false,
			})
		}
		return creds
	}

	env, err := readVMEnvFunc(sdHome, vmName)
	if err != nil {
		for _, key := range knownCredentialKeys {
			creds = append(creds, credentialStatus{
				Name:       key,
				Label:      credentialLabels[key],
				Configured: false,
			})
		}
		return creds
	}

	for _, key := range knownCredentialKeys {
		val, configured := env[key]
		creds = append(creds, credentialStatus{
			Name:       key,
			Label:      credentialLabels[key],
			Configured: configured,
		})

		// REQ-004-024: Warn on classic PAT
		if configured && key == credKeyGithubToken && strings.HasPrefix(val, "ghp_") {
			*warnings = append(*warnings, securityWarning{
				Code:    "classic_pat",
				Message: "classic GitHub PAT (ghp_) detected; fine-grained PAT recommended for better scoping",
			})
		}
	}

	return creds
}

// gatherSnapshotSummary returns snapshot count for the VM.
func gatherSnapshotSummary(cmd *cobra.Command, b backend.Backend, name string) snapshotSummary {
	s, ok := b.(backend.Snapshotter)
	if !ok {
		return snapshotSummary{Count: 0, Error: "backend does not support snapshots"}
	}

	snaps, err := s.SnapshotList(cmd.Context(), name)
	if err != nil {
		return snapshotSummary{Count: 0, Error: "failed to query snapshots"}
	}

	return snapshotSummary{Count: len(snaps)}
}

// gatherLastAuditEvent returns the most recent audit event for the VM.
func gatherLastAuditEvent(vmName string) *lastAuditEvent {
	sdHome := ""
	if l := Loader(); l != nil {
		sdHome = l.SDHome()
	}
	if sdHome == "" {
		return nil
	}

	logger := newAuditLoggerFunc(sdHome)
	vmNamePtr := vmName
	filter := security.AuditFilter{VMName: &vmNamePtr}

	entries, err := logger.Query(filter)
	if err != nil || len(entries) == 0 {
		return nil
	}

	last := entries[len(entries)-1]
	summary := ""
	switch last.Type {
	case "command":
		summary = fmt.Sprintf("%s %v (exit=%d)", last.Command, last.Args, last.ExitCode)
	case "event":
		summary = fmt.Sprintf("%s vm=%s", last.EventType, last.VM)
	}

	return &lastAuditEvent{
		Timestamp: last.Timestamp,
		Type:      last.Type,
		Summary:   summary,
	}
}

// formatSecurityStatus renders the security posture summary as human-readable output.
func formatSecurityStatus(data securityStatusData) string {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "Security Posture: %s\n", data.VM)
	fmt.Fprintf(&buf, "Status:           %s\n", data.Status)
	buf.WriteString("\n")

	// Mounts
	fmt.Fprintf(&buf, "Mounts: %d\n", len(data.Mounts))
	if len(data.Mounts) == 0 {
		buf.WriteString("  (none -- recommended)\n")
	} else {
		for _, m := range data.Mounts {
			fmt.Fprintf(&buf, "  %s -> %s (%s)\n", m.HostPath, m.GuestPath, m.Mode)
		}
	}
	buf.WriteString("\n")

	// Egress
	fmt.Fprintf(&buf, "Egress Rules: %d (%d default, %d user)\n",
		data.Egress.Count, data.Egress.Default, data.Egress.User)
	if len(data.Egress.Domains) > 0 {
		w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
		for _, d := range data.Egress.Domains {
			fmt.Fprintf(w, "  %s\t[%s]\n", d.Domain, d.Source)
		}
		w.Flush()
	}
	buf.WriteString("\n")

	// Credentials
	buf.WriteString("Credentials:\n")
	for _, c := range data.Credentials {
		status := "not configured"
		if c.Configured {
			status = "configured"
		}
		fmt.Fprintf(&buf, "  %-25s %s\n", c.Label, status)
	}
	buf.WriteString("\n")

	// Snapshots
	fmt.Fprintf(&buf, "Snapshots: %d\n", data.Snapshots.Count)
	if data.Snapshots.Error != "" {
		fmt.Fprintf(&buf, "  (%s)\n", data.Snapshots.Error)
	}
	buf.WriteString("\n")

	// Last Audit Event
	if data.LastAudit != nil {
		ts := data.LastAudit.Timestamp.Format("2006-01-02T15:04:05Z07:00")
		fmt.Fprintf(&buf, "Last Audit Event:\n")
		fmt.Fprintf(&buf, "  %s  %s  %s\n", ts, data.LastAudit.Type, data.LastAudit.Summary)
	} else {
		fmt.Fprintf(&buf, "Last Audit Event: (none)\n")
	}

	// Warnings
	if len(data.Warnings) > 0 {
		buf.WriteString("\nWarnings:\n")
		for _, w := range data.Warnings {
			fmt.Fprintf(&buf, "  ! [%s] %s\n", w.Code, w.Message)
		}
	}

	return buf.String()
}
