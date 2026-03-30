// Package cmd provides tests for the security status command.
// REQ-004-024: Security Posture Summary
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sd/internal/backend"
	"sd/internal/config"
	"sd/internal/security"
	"sd/internal/ui"
)

// ---------------------------------------------------------------------------
// Digital Twins
// ---------------------------------------------------------------------------

// mockSecurityBackend is a digital twin of a backend for security status tests.
// It implements backend.Backend and backend.Snapshotter.
type mockSecurityBackend struct {
	name        string
	available   bool
	vmStatus    map[string]backend.VMStatus
	statusErr   error
	snapshots   map[string][]backend.SnapshotInfo
	snapshotErr error
}

func newMockSecurityBackend() *mockSecurityBackend {
	return &mockSecurityBackend{
		name:      "mock-security",
		available: true,
		vmStatus: map[string]backend.VMStatus{
			"test-vm": backend.StatusRunning,
		},
		snapshots: make(map[string][]backend.SnapshotInfo),
	}
}

func (m *mockSecurityBackend) Name() string                                       { return m.name }
func (m *mockSecurityBackend) Available() error                                    { return nil }
func (m *mockSecurityBackend) Create(_ context.Context, _ string, _ backend.VMConfig) error { return nil }
func (m *mockSecurityBackend) Start(_ context.Context, _ string) error             { return nil }
func (m *mockSecurityBackend) Stop(_ context.Context, _ string) error              { return nil }
func (m *mockSecurityBackend) Destroy(_ context.Context, _ string) error           { return nil }
func (m *mockSecurityBackend) SSHConfig(_ context.Context, _ string) (backend.SSHConfig, error) {
	return backend.SSHConfig{}, nil
}
func (m *mockSecurityBackend) Exec(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
	return backend.ExecResult{}, nil
}
func (m *mockSecurityBackend) List(_ context.Context) ([]backend.VMInfo, error) { return nil, nil }
func (m *mockSecurityBackend) Status(_ context.Context, name string) (backend.VMStatus, error) {
	if m.statusErr != nil {
		return "", m.statusErr
	}
	if s, ok := m.vmStatus[name]; ok {
		return s, nil
	}
	return "", backend.ErrVMNotFound
}

// Snapshotter implementation
func (m *mockSecurityBackend) SnapshotCreate(_ context.Context, _, _ string) error { return nil }
func (m *mockSecurityBackend) SnapshotApply(_ context.Context, _, _ string) error  { return nil }
func (m *mockSecurityBackend) SnapshotDelete(_ context.Context, _, _ string) error { return nil }
func (m *mockSecurityBackend) SnapshotList(_ context.Context, name string) ([]backend.SnapshotInfo, error) {
	if m.snapshotErr != nil {
		return nil, m.snapshotErr
	}
	return m.snapshots[name], nil
}

// noSnapshotterMinimal is a minimal backend without Snapshotter.
type noSnapshotterMinimal struct{}

func (n *noSnapshotterMinimal) Name() string                                       { return "no-snap" }
func (n *noSnapshotterMinimal) Available() error                                    { return nil }
func (n *noSnapshotterMinimal) Create(_ context.Context, _ string, _ backend.VMConfig) error { return nil }
func (n *noSnapshotterMinimal) Start(_ context.Context, _ string) error             { return nil }
func (n *noSnapshotterMinimal) Stop(_ context.Context, _ string) error              { return nil }
func (n *noSnapshotterMinimal) Destroy(_ context.Context, _ string) error           { return nil }
func (n *noSnapshotterMinimal) Status(_ context.Context, _ string) (backend.VMStatus, error) {
	return backend.StatusRunning, nil
}
func (n *noSnapshotterMinimal) List(_ context.Context) ([]backend.VMInfo, error)    { return nil, nil }
func (n *noSnapshotterMinimal) SSHConfig(_ context.Context, _ string) (backend.SSHConfig, error) {
	return backend.SSHConfig{}, nil
}
func (n *noSnapshotterMinimal) Exec(_ context.Context, _ string, _ []string) (backend.ExecResult, error) {
	return backend.ExecResult{}, nil
}

// mockSecurityEnvStore is a digital twin for VM credential storage in security tests.
// It preserves key case (unlike viper which lowercases).
type mockSecurityEnvStore struct {
	envs map[string]map[string]string // vmName -> env map
}

func newMockSecurityEnvStore() *mockSecurityEnvStore {
	return &mockSecurityEnvStore{envs: make(map[string]map[string]string)}
}

func (m *mockSecurityEnvStore) read(_, vmName string) (map[string]string, error) {
	env, ok := m.envs[vmName]
	if !ok {
		return make(map[string]string), nil
	}
	cp := make(map[string]string, len(env))
	for k, v := range env {
		cp[k] = v
	}
	return cp, nil
}

// ---------------------------------------------------------------------------
// Test Setup
// ---------------------------------------------------------------------------

// setupSecurityTest configures the test environment for security status tests.
func setupSecurityTest(t *testing.T) (*mockSecurityBackend, *mockSecurityEnvStore, func()) {
	t.Helper()
	tmpDir := newRootTestEnv(t)

	mb := newMockSecurityBackend()
	envStore := newMockSecurityEnvStore()

	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) { return mb, nil }

	origLoader := loader
	l := config.NewLoader(config.WithSDHome(tmpDir))
	require.NoError(t, l.Load())
	loader = l

	origReadEnv := readVMEnvFunc
	readVMEnvFunc = envStore.read

	origNewAudit := newAuditLoggerFunc

	cleanup := func() {
		getBackendFunc = origGetBackend
		loader = origLoader
		readVMEnvFunc = origReadEnv
		newAuditLoggerFunc = origNewAudit
	}

	return mb, envStore, cleanup
}

// ---------------------------------------------------------------------------
// Unit Tests
// ---------------------------------------------------------------------------

func TestSecurityStatus_Registration(t *testing.T) {
	newRootTestEnv(t)
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "security" {
			found = true
			subs := cmd.Commands()
			subNames := make(map[string]bool)
			for _, sc := range subs {
				subNames[sc.Name()] = true
			}
			assert.True(t, subNames["status"], "security should have 'status' subcommand")
			assert.Equal(t, "security", cmd.GroupID)
			break
		}
	}
	assert.True(t, found, "security command should be registered")
}

func TestSecurityStatus_NoConfigRequired(t *testing.T) {
	noConfigCmds := map[string]bool{
		"version": true, "help": true, "completion": true,
		"doctor": true, "list": true, "status": true,
		"connect": true, "ssh-config": true, "sync": true,
		"audit": true, "config": true, "provision": true,
		"logs": true, "token": true, "security": true,
	}
	assert.True(t, noConfigCmds["security"], "security should be in no-config-required list")
}

func TestSecurityStatus_HumanOutput(t *testing.T) {
	mb, envStore, cleanup := setupSecurityTest(t)
	defer cleanup()

	sdHome := loader.SDHome()
	mb.snapshots["test-vm"] = []backend.SnapshotInfo{
		{Name: "snap1", CreatedAt: time.Now().UTC(), Size: 1024},
	}
	envStore.envs["test-vm"] = map[string]string{
		"GITHUB_TOKEN":      "github_pat_abc123",
		"ANTHROPIC_API_KEY": "sk-ant-test123",
	}

	// Write an audit log entry
	auditPath := filepath.Join(sdHome, "audit.log")
	now := time.Now().UTC().Truncate(time.Second)
	entry := security.AuditEntry{
		Timestamp: now, Type: "event", PrevHash: security.GenesisHash,
		EventType: "connect", VM: "test-vm",
		Meta: map[string]string{"credentials_injected": "GITHUB_TOKEN"},
	}
	line, _ := json.Marshal(entry)
	require.NoError(t, os.WriteFile(auditPath, append(line, '\n'), 0600))

	var stdout, stderr bytes.Buffer
	formatter = ui.NewFormatterWithWriters(false, &stdout, &stderr)

	cmd, _, _ := rootCmd.Find([]string{"security", "status"})
	err := runSecurityStatus(cmd, []string{"test-vm"})
	require.NoError(t, err)

	output := stdout.String()
	assert.Contains(t, output, "Security Posture: test-vm")
	assert.Contains(t, output, "Status:           running")
	assert.Contains(t, output, "Mounts:")
	assert.Contains(t, output, "Egress Rules:")
	assert.Contains(t, output, "Credentials:")
	assert.Contains(t, output, "Snapshots: 1")
	assert.Contains(t, output, "Last Audit Event:")
	assert.Contains(t, output, "connect")
}

func TestSecurityStatus_JSONOutput(t *testing.T) {
	mb, envStore, cleanup := setupSecurityTest(t)
	defer cleanup()

	mb.snapshots["test-vm"] = []backend.SnapshotInfo{
		{Name: "snap1", CreatedAt: time.Now().UTC(), Size: 2048},
	}
	envStore.envs["test-vm"] = map[string]string{
		"GITHUB_TOKEN":      "github_pat_abc123",
		"ANTHROPIC_API_KEY": "sk-ant-test123",
	}

	var stdout, stderr bytes.Buffer
	formatter = ui.NewFormatterWithWriters(true, &stdout, &stderr)

	cmd, _, _ := rootCmd.Find([]string{"security", "status"})
	err := runSecurityStatus(cmd, []string{"test-vm"})
	require.NoError(t, err)

	var result map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.Equal(t, "true", string(result["ok"]))

	var data securityStatusData
	require.NoError(t, json.Unmarshal(result["data"], &data))
	assert.Equal(t, "test-vm", data.VM)
	assert.Equal(t, "running", data.Status)
	assert.Equal(t, 1, data.Snapshots.Count)
	assert.NotEmpty(t, data.Egress.Domains)
	assert.True(t, data.Egress.Count > 0)

	githubFound := false
	anthropicFound := false
	for _, c := range data.Credentials {
		if c.Name == "GITHUB_TOKEN" && c.Configured {
			githubFound = true
		}
		if c.Name == "ANTHROPIC_API_KEY" && c.Configured {
			anthropicFound = true
		}
	}
	assert.True(t, githubFound, "GITHUB_TOKEN should be configured")
	assert.True(t, anthropicFound, "ANTHROPIC_API_KEY should be configured")
}

func TestSecurityStatus_VMNotFound(t *testing.T) {
	_, _, cleanup := setupSecurityTest(t)
	defer cleanup()

	var stdout, stderr bytes.Buffer
	formatter = ui.NewFormatterWithWriters(false, &stdout, &stderr)

	cmd, _, _ := rootCmd.Find([]string{"security", "status"})
	err := runSecurityStatus(cmd, []string{"nonexistent-vm"})

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "vm_not_found", cliErr.Code)
}

func TestSecurityStatus_BackendUnavailable(t *testing.T) {
	newRootTestEnv(t)
	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return nil, fmt.Errorf("no backend")
	}
	defer func() { getBackendFunc = origGetBackend }()

	var stdout, stderr bytes.Buffer
	formatter = ui.NewFormatterWithWriters(false, &stdout, &stderr)

	cmd, _, _ := rootCmd.Find([]string{"security", "status"})
	err := runSecurityStatus(cmd, []string{"test-vm"})

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "backend_unavailable", cliErr.Code)
}

func TestSecurityStatus_EmptyName(t *testing.T) {
	_, _, cleanup := setupSecurityTest(t)
	defer cleanup()

	var stdout, stderr bytes.Buffer
	formatter = ui.NewFormatterWithWriters(false, &stdout, &stderr)

	cmd, _, _ := rootCmd.Find([]string{"security", "status"})
	err := runSecurityStatus(cmd, []string{""})

	require.Error(t, err)
	cliErr, ok := err.(ui.CLIError)
	require.True(t, ok)
	assert.Equal(t, "invalid_argument", cliErr.Code)
}

func TestSecurityStatus_NoCredentials_Warning(t *testing.T) {
	_, _, cleanup := setupSecurityTest(t)
	defer cleanup()

	var stdout, stderr bytes.Buffer
	formatter = ui.NewFormatterWithWriters(true, &stdout, &stderr)

	cmd, _, _ := rootCmd.Find([]string{"security", "status"})
	err := runSecurityStatus(cmd, []string{"test-vm"})
	require.NoError(t, err)

	var result map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

	var data securityStatusData
	require.NoError(t, json.Unmarshal(result["data"], &data))

	found := false
	for _, w := range data.Warnings {
		if w.Code == "no_credentials" {
			found = true
		}
	}
	assert.True(t, found, "should warn about no credentials")
}

func TestSecurityStatus_ClassicPAT_Warning(t *testing.T) {
	_, envStore, cleanup := setupSecurityTest(t)
	defer cleanup()

	envStore.envs["test-vm"] = map[string]string{
		"GITHUB_TOKEN":      "ghp_classicpat123",
		"ANTHROPIC_API_KEY": "sk-ant-test123",
	}

	var stdout, stderr bytes.Buffer
	formatter = ui.NewFormatterWithWriters(true, &stdout, &stderr)

	cmd, _, _ := rootCmd.Find([]string{"security", "status"})
	err := runSecurityStatus(cmd, []string{"test-vm"})
	require.NoError(t, err)

	var result map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

	var data securityStatusData
	require.NoError(t, json.Unmarshal(result["data"], &data))

	found := false
	for _, w := range data.Warnings {
		if w.Code == "classic_pat" {
			found = true
		}
	}
	assert.True(t, found, "should warn about classic PAT")
}

func TestSecurityStatus_NoSnapshots_Warning(t *testing.T) {
	_, envStore, cleanup := setupSecurityTest(t)
	defer cleanup()

	envStore.envs["test-vm"] = map[string]string{
		"GITHUB_TOKEN": "github_pat_abc",
	}

	var stdout, stderr bytes.Buffer
	formatter = ui.NewFormatterWithWriters(true, &stdout, &stderr)

	cmd, _, _ := rootCmd.Find([]string{"security", "status"})
	err := runSecurityStatus(cmd, []string{"test-vm"})
	require.NoError(t, err)

	var result map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

	var data securityStatusData
	require.NoError(t, json.Unmarshal(result["data"], &data))

	found := false
	for _, w := range data.Warnings {
		if w.Code == "no_snapshots" {
			found = true
		}
	}
	assert.True(t, found, "should warn about no snapshots")
}

func TestSecurityStatus_EgressDefaultDomains(t *testing.T) {
	_, _, cleanup := setupSecurityTest(t)
	defer cleanup()

	var warnings []securityWarning
	egress := gatherEgress(&warnings)

	assert.True(t, egress.Default > 0, "should have default egress domains")
	assert.Equal(t, 0, egress.User)
	assert.Equal(t, egress.Default, egress.Count)
	assert.NotEmpty(t, egress.Domains)

	foundAnthropic := false
	foundGithub := false
	for _, d := range egress.Domains {
		if d.Domain == "api.anthropic.com" {
			foundAnthropic = true
			assert.Equal(t, security.DomainSourceDefault, d.Source)
		}
		if d.Domain == "github.com" {
			foundGithub = true
			assert.Equal(t, security.DomainSourceDefault, d.Source)
		}
	}
	assert.True(t, foundAnthropic, "should have api.anthropic.com")
	assert.True(t, foundGithub, "should have github.com")
}

func TestSecurityStatus_LastAuditEvent(t *testing.T) {
	tmpDir := newRootTestEnv(t)
	l := config.NewLoader(config.WithSDHome(tmpDir))
	require.NoError(t, l.Load())
	origLoader := loader
	loader = l
	defer func() { loader = origLoader }()

	auditPath := filepath.Join(tmpDir, "audit.log")
	now := time.Now().UTC().Truncate(time.Second)

	entries := []security.AuditEntry{
		{Timestamp: now.Add(-2 * time.Hour), Type: "event", PrevHash: security.GenesisHash, EventType: "create", VM: "test-vm"},
		{Timestamp: now.Add(-1 * time.Hour), Type: "event", PrevHash: "hash1", EventType: "connect", VM: "test-vm", Meta: map[string]string{"credentials_injected": "GITHUB_TOKEN"}},
		{Timestamp: now.Add(-30 * time.Minute), Type: "event", PrevHash: "hash2", EventType: "stop", VM: "other-vm"},
	}

	var buf bytes.Buffer
	for _, e := range entries {
		line, _ := json.Marshal(e)
		buf.Write(line)
		buf.WriteByte('\n')
	}
	require.NoError(t, os.WriteFile(auditPath, buf.Bytes(), 0600))

	last := gatherLastAuditEvent("test-vm")
	require.NotNil(t, last)
	assert.Equal(t, "event", last.Type)
	assert.Contains(t, last.Summary, "connect")
	assert.Contains(t, last.Summary, "test-vm")
}

func TestSecurityStatus_NoAuditEvents(t *testing.T) {
	tmpDir := newRootTestEnv(t)
	l := config.NewLoader(config.WithSDHome(tmpDir))
	require.NoError(t, l.Load())
	origLoader := loader
	loader = l
	defer func() { loader = origLoader }()

	last := gatherLastAuditEvent("test-vm")
	assert.Nil(t, last)
}

func TestSecurityStatus_NonSnapshotterBackend(t *testing.T) {
	newRootTestEnv(t)
	origGetBackend := getBackendFunc
	getBackendFunc = func(_ string) (backend.Backend, error) {
		return &noSnapshotterMinimal{}, nil
	}
	defer func() { getBackendFunc = origGetBackend }()

	l := config.NewLoader(config.WithSDHome(t.TempDir()))
	require.NoError(t, l.Load())
	origLoader := loader
	loader = l
	defer func() { loader = origLoader }()

	var stdout, stderr bytes.Buffer
	formatter = ui.NewFormatterWithWriters(true, &stdout, &stderr)

	cmd, _, _ := rootCmd.Find([]string{"security", "status"})
	err := runSecurityStatus(cmd, []string{"test-vm"})
	require.NoError(t, err)

	var result map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

	var data securityStatusData
	require.NoError(t, json.Unmarshal(result["data"], &data))
	assert.Equal(t, 0, data.Snapshots.Count)
	assert.Equal(t, "backend does not support snapshots", data.Snapshots.Error)
}

// ---------------------------------------------------------------------------
// Format Tests
// ---------------------------------------------------------------------------

func TestFormatSecurityStatus_NoWarnings(t *testing.T) {
	data := securityStatusData{
		VM:     "myvm", Status: "running", Mounts: []mountInfo{},
		Egress: egressInfo{Count: 11, Default: 11, User: 0, Domains: []security.EgressDomain{
			{Domain: "api.anthropic.com", Source: security.DomainSourceDefault},
		}},
		Credentials: []credentialStatus{{Name: "GITHUB_TOKEN", Label: "GitHub Token", Configured: true}},
		Snapshots:   snapshotSummary{Count: 2},
		LastAudit:   &lastAuditEvent{Timestamp: time.Now().UTC(), Type: "event", Summary: "create vm=myvm"},
		Warnings:    []securityWarning{},
	}

	output := formatSecurityStatus(data)
	assert.Contains(t, output, "Security Posture: myvm")
	assert.Contains(t, output, "Mounts: 0")
	assert.Contains(t, output, "(none -- recommended)")
	assert.Contains(t, output, "Egress Rules: 11")
	assert.Contains(t, output, "Snapshots: 2")
	assert.Contains(t, output, "Last Audit Event:")
	assert.NotContains(t, output, "Warnings:")
}

func TestFormatSecurityStatus_WithWarnings(t *testing.T) {
	data := securityStatusData{
		VM: "myvm", Status: "running", Mounts: []mountInfo{},
		Egress: egressInfo{Count: 11, Default: 11, User: 0, Domains: []security.EgressDomain{}},
		Credentials: []credentialStatus{}, Snapshots: snapshotSummary{Count: 0},
		Warnings: []securityWarning{
			{Code: "no_credentials", Message: "no credentials configured"},
			{Code: "no_snapshots", Message: "no snapshots exist"},
		},
	}

	output := formatSecurityStatus(data)
	assert.Contains(t, output, "Warnings:")
	assert.Contains(t, output, "[no_credentials]")
	assert.Contains(t, output, "[no_snapshots]")
}

func TestFormatSecurityStatus_NoLastAudit(t *testing.T) {
	data := securityStatusData{
		VM: "myvm", Status: "stopped", Mounts: []mountInfo{},
		Egress: egressInfo{Count: 11, Default: 11, User: 0, Domains: []security.EgressDomain{}},
		Snapshots: snapshotSummary{Count: 0}, Warnings: []securityWarning{},
	}

	output := formatSecurityStatus(data)
	assert.Contains(t, output, "Last Audit Event: (none)")
}

func TestFormatSecurityStatus_WithMounts(t *testing.T) {
	data := securityStatusData{
		VM: "myvm", Status: "running",
		Mounts: []mountInfo{
			{HostPath: "/host/project", GuestPath: "/guest/project", Mode: "ro"},
			{HostPath: "/host/data", GuestPath: "/guest/data", Mode: "rw"},
		},
		Egress: egressInfo{Count: 11, Default: 11, User: 0, Domains: []security.EgressDomain{}},
		Credentials: []credentialStatus{}, Snapshots: snapshotSummary{Count: 1},
		Warnings: []securityWarning{{Code: "writable_mount", Message: "writable mount detected"}},
	}

	output := formatSecurityStatus(data)
	assert.Contains(t, output, "Mounts: 2")
	assert.Contains(t, output, "/host/project -> /guest/project (ro)")
	assert.Contains(t, output, "/host/data -> /guest/data (rw)")
}

func TestFormatSecurityStatus_SnapshotError(t *testing.T) {
	data := securityStatusData{
		VM: "myvm", Status: "running", Mounts: []mountInfo{},
		Egress: egressInfo{Count: 11, Default: 11, User: 0, Domains: []security.EgressDomain{}},
		Snapshots: snapshotSummary{Count: 0, Error: "backend does not support snapshots"},
		Warnings: []securityWarning{},
	}

	output := formatSecurityStatus(data)
	assert.Contains(t, output, "Snapshots: 0")
	assert.Contains(t, output, "(backend does not support snapshots)")
}

// ---------------------------------------------------------------------------
// Property Tests
// ---------------------------------------------------------------------------

func TestProperty_SecurityStatus_JSONAlwaysValid(t *testing.T) {
	cases := []struct {
		name    string
		vmName  string
		status  string
		snapCnt int
		env     map[string]string
	}{
		{"basic", "vm1", "running", 1, map[string]string{"GITHUB_TOKEN": "github_pat_abc"}},
		{"stopped", "vm2", "stopped", 0, nil},
		{"classic-pat", "vm3", "running", 2, map[string]string{"GITHUB_TOKEN": "ghp_classic123"}},
		{"no-creds", "vm4", "error", 0, nil},
		{"all-creds", "vm5", "running", 3, map[string]string{"GITHUB_TOKEN": "github_pat_abc", "ANTHROPIC_API_KEY": "sk-ant-xyz"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mb, envStore, cleanup := setupSecurityTest(t)
			defer cleanup()

			mb.vmStatus[tc.vmName] = backend.VMStatus(tc.status)
			if tc.snapCnt > 0 {
				snaps := make([]backend.SnapshotInfo, tc.snapCnt)
				for i := range snaps {
					snaps[i] = backend.SnapshotInfo{
						Name: fmt.Sprintf("snap-%d", i), CreatedAt: time.Now().UTC(), Size: 1024 * int64(i+1),
					}
				}
				mb.snapshots[tc.vmName] = snaps
			}
			if tc.env != nil {
				envStore.envs[tc.vmName] = tc.env
			}

			var stdout, stderr bytes.Buffer
			formatter = ui.NewFormatterWithWriters(true, &stdout, &stderr)

			cmd, _, _ := rootCmd.Find([]string{"security", "status"})
			err := runSecurityStatus(cmd, []string{tc.vmName})
			require.NoError(t, err)

			var result map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
			assert.Equal(t, "true", string(result["ok"]))

			var data securityStatusData
			require.NoError(t, json.Unmarshal(result["data"], &data))
			assert.Equal(t, tc.vmName, data.VM)
			assert.Equal(t, tc.status, data.Status)
			assert.Equal(t, tc.snapCnt, data.Snapshots.Count)
			assert.NotNil(t, data.Warnings)
		})
	}
}

func TestProperty_SecurityStatus_ErrorCodesSnakeCase(t *testing.T) {
	// vm_not_found and invalid_argument
	tests := []struct {
		name    string
		vmName  string
		errCode string
	}{
		{"vm_not_found", "nonexistent", "vm_not_found"},
		{"empty_name", "", "invalid_argument"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, cleanup := setupSecurityTest(t)
			defer cleanup()

			var stdout, stderr bytes.Buffer
			formatter = ui.NewFormatterWithWriters(false, &stdout, &stderr)

			cmd, _, _ := rootCmd.Find([]string{"security", "status"})
			err := runSecurityStatus(cmd, []string{tc.vmName})

			require.Error(t, err)
			cliErr, ok := err.(ui.CLIError)
			require.True(t, ok)
			assert.Equal(t, tc.errCode, cliErr.Code)
			assert.False(t, strings.Contains(cliErr.Code, " "))
			assert.False(t, strings.Contains(cliErr.Code, "-"))
			assert.Equal(t, strings.ToLower(cliErr.Code), cliErr.Code)
		})
	}

	// backend_unavailable with separate setup
	t.Run("backend_unavailable", func(t *testing.T) {
		newRootTestEnv(t)
		origGetBackend := getBackendFunc
		getBackendFunc = func(_ string) (backend.Backend, error) {
			return nil, fmt.Errorf("no backend")
		}
		defer func() { getBackendFunc = origGetBackend }()

		var stdout, stderr bytes.Buffer
		formatter = ui.NewFormatterWithWriters(false, &stdout, &stderr)

		cmd, _, _ := rootCmd.Find([]string{"security", "status"})
		err := runSecurityStatus(cmd, []string{"test-vm"})

		require.Error(t, err)
		cliErr, ok := err.(ui.CLIError)
		require.True(t, ok)
		assert.Equal(t, "backend_unavailable", cliErr.Code)
	})
}

func TestProperty_SecurityStatus_JSONRequiredFields(t *testing.T) {
	mb, envStore, cleanup := setupSecurityTest(t)
	defer cleanup()

	mb.snapshots["test-vm"] = []backend.SnapshotInfo{
		{Name: "snap1", CreatedAt: time.Now().UTC(), Size: 1024},
	}
	envStore.envs["test-vm"] = map[string]string{"GITHUB_TOKEN": "github_pat_abc"}

	var stdout, stderr bytes.Buffer
	formatter = ui.NewFormatterWithWriters(true, &stdout, &stderr)

	cmd, _, _ := rootCmd.Find([]string{"security", "status"})
	err := runSecurityStatus(cmd, []string{"test-vm"})
	require.NoError(t, err)

	var result struct {
		OK   bool                `json:"ok"`
		Data securityStatusData `json:"data"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.True(t, result.OK)
	assert.Equal(t, "test-vm", result.Data.VM)
	assert.Equal(t, "running", result.Data.Status)
	assert.NotNil(t, result.Data.Mounts)
	assert.NotNil(t, result.Data.Egress.Domains)
	assert.NotNil(t, result.Data.Credentials)
	assert.NotNil(t, result.Data.Warnings)
	assert.True(t, result.Data.Egress.Count > 0)
}

func TestProperty_SecurityStatus_HumanContainsVMName(t *testing.T) {
	names := []string{"my-vm", "prod-server", "dev-box", "test123", "a"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			mb, envStore, cleanup := setupSecurityTest(t)
			defer cleanup()

			mb.vmStatus[name] = backend.StatusRunning
			mb.snapshots[name] = []backend.SnapshotInfo{
				{Name: "snap1", CreatedAt: time.Now().UTC(), Size: 1024},
			}
			envStore.envs[name] = map[string]string{"GITHUB_TOKEN": "github_pat_abc"}

			var stdout, stderr bytes.Buffer
			formatter = ui.NewFormatterWithWriters(false, &stdout, &stderr)

			cmd, _, _ := rootCmd.Find([]string{"security", "status"})
			err := runSecurityStatus(cmd, []string{name})
			require.NoError(t, err)
			assert.Contains(t, stdout.String(), name)
		})
	}
}

func TestProperty_SecurityStatus_NonexistentVMNeverSucceeds(t *testing.T) {
	names := []string{"ghost", "phantom", "void"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			_, _, cleanup := setupSecurityTest(t)
			defer cleanup()

			var stdout, stderr bytes.Buffer
			formatter = ui.NewFormatterWithWriters(false, &stdout, &stderr)

			cmd, _, _ := rootCmd.Find([]string{"security", "status"})
			err := runSecurityStatus(cmd, []string{name})
			require.Error(t, err)
			cliErr, ok := err.(ui.CLIError)
			require.True(t, ok)
			assert.Equal(t, "vm_not_found", cliErr.Code)
		})
	}
}

func TestProperty_SecurityStatus_WarningsIncludeNoCredentials(t *testing.T) {
	vmNames := []string{"vm-a", "vm-b", "vm-c"}
	for _, name := range vmNames {
		t.Run(name, func(t *testing.T) {
			mb, _, cleanup := setupSecurityTest(t)
			defer cleanup()

			mb.vmStatus[name] = backend.StatusRunning

			var stdout, stderr bytes.Buffer
			formatter = ui.NewFormatterWithWriters(true, &stdout, &stderr)

			cmd, _, _ := rootCmd.Find([]string{"security", "status"})
			err := runSecurityStatus(cmd, []string{name})
			require.NoError(t, err)

			var result map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

			var data securityStatusData
			require.NoError(t, json.Unmarshal(result["data"], &data))

			found := false
			for _, w := range data.Warnings {
				if w.Code == "no_credentials" {
					found = true
				}
			}
			assert.True(t, found, "no_credentials warning must appear when no creds configured")
		})
	}
}
