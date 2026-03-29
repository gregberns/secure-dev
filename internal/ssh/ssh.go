// Package ssh manages SSH keys, config fragments, and connection utilities for sd VMs.
// REQ-007-003: SSH Key Generation Per VM
// REQ-007-004: SSH Config Management
// REQ-007-005: VSOCK Transport Support
// REQ-007-007: Port Forwarding on Connect
// REQ-007-009: Named tmux Sessions (validation)
// REQ-007-019: Environment Injection on Connect
// REQ-007-021: Connection Health and Error Reporting
package ssh

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors.
var (
	ErrInvalidPortForward = errors.New("invalid port forward specification")
	ErrInvalidSessionName = errors.New("invalid session name")
	ErrKeyExists          = errors.New("ssh key already exists")
	ErrPortOutOfRange     = errors.New("port out of valid range")
	ErrHostKeyChanged     = errors.New("ssh host key has changed")
	ErrHostKeyScanFailed  = errors.New("ssh host key scan failed")
	ErrNoHostKey          = errors.New("no host key stored for vm")
)

// Session name: alphanumeric, hyphens, underscores only.
var sessionNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Port forward: host:guest or bind:host:guest
var portForwardTwo = regexp.MustCompile(`^(\d+):(\d+)$`)
var portForwardThree = regexp.MustCompile(`^(.+):(\d+):(\d+)$`)

// Env var reference pattern: ${VAR_NAME}
var envVarRefRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// --- REQ-007-003: SSH Key Generation ---

// KeyPaths returns the directory and file paths for a VM's SSH keys.
func KeyPaths(sdHome, vmName string) (dir, privateKey, publicKey string) {
	dir = filepath.Join(sdHome, "vms", vmName, "ssh")
	return dir, filepath.Join(dir, "id_ed25519"), filepath.Join(dir, "id_ed25519.pub")
}

// GenerateKeys creates an Ed25519 key pair at the standard location for a VM.
// The directory is created with 0700 and the private key with 0600.
// Returns an error if keys already exist.
func GenerateKeys(sdHome, vmName string) error {
	dir, privPath, pubPath := KeyPaths(sdHome, vmName)

	if _, err := os.Stat(privPath); err == nil {
		return fmt.Errorf("%w: %s", ErrKeyExists, privPath)
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create key directory: %w", err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate ed25519 key: %w", err)
	}

	// Write private key.
	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return fmt.Errorf("marshal private key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})
	if err := os.WriteFile(privPath, privPEM, 0600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}

	// Write public key in OpenSSH format.
	pubKeyStr, err := publicKeyToOpenSSH(pub)
	if err != nil {
		return fmt.Errorf("encode public key: %w", err)
	}
	if err := os.WriteFile(pubPath, []byte(pubKeyStr+"\n"), 0644); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}

	return nil
}

// publicKeyToOpenSSH converts an ed25519 public key to OpenSSH authorized_keys format.
func publicKeyToOpenSSH(pub ed25519.PublicKey) (string, error) {
	// OpenSSH wire format: string("ssh-ed25519") + string(pubkey_bytes)
	// Each string is length-prefixed as uint32 big-endian.
	const keyType = "ssh-ed25519"
	buf := make([]byte, 0, 4+len(keyType)+4+ed25519.PublicKeySize)
	buf = appendUint32BE(buf, uint32(len(keyType)))
	buf = append(buf, keyType...)
	buf = appendUint32BE(buf, uint32(len(pub)))
	buf = append(buf, pub...)
	return fmt.Sprintf("ssh-ed25519 %s sd-generated", base64.StdEncoding.EncodeToString(buf)), nil
}

// appendUint32BE appends a big-endian uint32 to buf.
func appendUint32BE(buf []byte, v uint32) []byte {
	return append(buf, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// KeyFingerprint returns the fingerprint of the public key for a VM.
func KeyFingerprint(pubKeyPath string) (string, error) {
	// Try ssh-keygen first for an accurate fingerprint.
	path, err := exec.LookPath("ssh-keygen")
	if err == nil {
		out, err := exec.Command(path, "-lf", pubKeyPath).Output()
		if err == nil {
			return strings.TrimSpace(string(out)), nil
		}
	}
	return "", fmt.Errorf("cannot compute fingerprint for %s", pubKeyPath)
}

// --- REQ-007-004 / REQ-007-005: SSH Config Fragment ---

// Transport represents the SSH transport type.
type Transport string

const (
	TransportTCP   Transport = "tcp"
	TransportVSOCK Transport = "vsock"
)

// SSHFragmentOpts configures SSH config fragment generation.
type SSHFragmentOpts struct {
	VMName       string
	HostName     string // IP address for TCP, empty for VSOCK
	Port         int    // TCP port, ignored for VSOCK
	User         string
	Transport    Transport
	SDHome       string
	ProxyCommand string // For VSOCK: e.g., "limactl ssh --stdio <name>"
}

// GenerateFragment produces an SSH config fragment string.
// REQ-007-004: TCP connections use StrictHostKeyChecking yes with per-VM known_hosts.
// REQ-007-005: VSOCK connections use StrictHostKeyChecking no with /dev/null.
func GenerateFragment(opts SSHFragmentOpts) string {
	_, privPath, _ := KeyPaths(opts.SDHome, opts.VMName)
	var sb strings.Builder

	sb.WriteString("# Managed by sd. Do not edit manually.\n")
	fmt.Fprintf(&sb, "Host sd-%s\n", opts.VMName)
	fmt.Fprintf(&sb, "    User %s\n", opts.User)
	fmt.Fprintf(&sb, "    IdentityFile %s\n", privPath)
	fmt.Fprintf(&sb, "    ForwardAgent no\n")
	fmt.Fprintf(&sb, "    ForwardX11 no\n")
	fmt.Fprintf(&sb, "    LogLevel ERROR\n")
	fmt.Fprintf(&sb, "    SendEnv SD_* ANTHROPIC_* GITHUB_* GH_*\n")

	switch opts.Transport {
	case TransportTCP:
		fmt.Fprintf(&sb, "    HostName %s\n", opts.HostName)
		fmt.Fprintf(&sb, "    Port %d\n", opts.Port)
		sb.WriteString("    StrictHostKeyChecking yes\n")
		knownHosts := filepath.Join(opts.SDHome, "vms", opts.VMName, "ssh", "known_hosts")
		fmt.Fprintf(&sb, "    UserKnownHostsFile %s\n", knownHosts)
	case TransportVSOCK:
		if opts.ProxyCommand != "" {
			fmt.Fprintf(&sb, "    ProxyCommand %s\n", opts.ProxyCommand)
		}
		sb.WriteString("    StrictHostKeyChecking no\n")
		sb.WriteString("    UserKnownHostsFile /dev/null\n")
	}

	return sb.String()
}

// FragmentPath returns the path for a VM's SSH config fragment.
func FragmentPath(sshDir, vmName string) string {
	return filepath.Join(sshDir, "config.d", "sd-"+vmName)
}

// WriteFragment writes the SSH config fragment to disk.
func WriteFragment(sshDir string, opts SSHFragmentOpts) error {
	fragmentPath := FragmentPath(sshDir, opts.VMName)
	configDir := filepath.Dir(fragmentPath)

	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("create SSH config directory: %w", err)
	}

	content := GenerateFragment(opts)
	if err := os.WriteFile(fragmentPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write SSH fragment: %w", err)
	}

	return nil
}

// RemoveFragment removes the SSH config fragment for a VM.
func RemoveFragment(sshDir, vmName string) error {
	fragmentPath := FragmentPath(sshDir, vmName)
	if err := os.Remove(fragmentPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove SSH fragment: %w", err)
	}
	return nil
}

// NeedsInclude checks whether ~/.ssh/config includes "Include config.d/*".
func NeedsInclude(sshConfig []byte) bool {
	for _, line := range strings.Split(string(sshConfig), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Include ") &&
			strings.Contains(trimmed, "config.d/") {
			return false
		}
	}
	return true
}

// --- REQ-004-031: SSH Host Key Verification ---

// HostKeyScanner scans a host and returns its SSH host keys in known_hosts format.
// This is the digital twin injection point: tests override this to avoid calling ssh-keyscan.
var HostKeyScanner = defaultHostKeyScanner

func defaultHostKeyScanner(host string, port int) ([]byte, error) {
	path, err := exec.LookPath("ssh-keyscan")
	if err != nil {
		return nil, fmt.Errorf("%w: ssh-keyscan not found in PATH", ErrHostKeyScanFailed)
	}
	out, err := exec.Command(path, "-p", strconv.Itoa(port), host).Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHostKeyScanFailed, err)
	}
	return out, nil
}

// KnownHostsPath returns the path to the per-VM known_hosts file.
func KnownHostsPath(sdHome, vmName string) string {
	return filepath.Join(sdHome, "vms", vmName, "ssh", "known_hosts")
}

// CaptureHostKey scans the VM's SSH server and stores the host key in known_hosts.
// REQ-004-031: During sd create, the VM's SSH host key is captured and stored.
func CaptureHostKey(sdHome, vmName, host string, port int) error {
	keyData, err := HostKeyScanner(host, port)
	if err != nil {
		return fmt.Errorf("capture host key for %q: %w", vmName, err)
	}

	if len(bytes.TrimSpace(keyData)) == 0 {
		return fmt.Errorf("capture host key for %q: %w: empty response", vmName, ErrHostKeyScanFailed)
	}

	return StoreHostKey(sdHome, vmName, keyData)
}

// StoreHostKey writes host key data to the per-VM known_hosts file.
// The directory is created if it does not exist. The file is overwritten if it exists
// (e.g., after a VM recreate).
func StoreHostKey(sdHome, vmName string, keyData []byte) error {
	path := KnownHostsPath(sdHome, vmName)
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create known_hosts directory: %w", err)
	}

	if err := os.WriteFile(path, keyData, 0600); err != nil {
		return fmt.Errorf("write known_hosts: %w", err)
	}

	return nil
}

// ReadHostKey reads the stored host key data for a VM.
// Returns ErrNoHostKey if no host key has been stored.
func ReadHostKey(sdHome, vmName string) ([]byte, error) {
	path := KnownHostsPath(sdHome, vmName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNoHostKey, vmName)
		}
		return nil, fmt.Errorf("read known_hosts for %q: %w", vmName, err)
	}
	return data, nil
}

// VerifyHostKey checks whether the given host key data matches what is stored for the VM.
// REQ-004-031: Detects host key changes (e.g., after a recreate).
// Returns nil if the key matches, ErrHostKeyChanged if it differs, ErrNoHostKey if none stored.
func VerifyHostKey(sdHome, vmName string, keyData []byte) error {
	stored, err := ReadHostKey(sdHome, vmName)
	if err != nil {
		return err
	}

	if !hostKeysMatch(stored, keyData) {
		return fmt.Errorf("%w: host key for VM %q does not match stored key", ErrHostKeyChanged, vmName)
	}

	return nil
}

// hostKeysMatch compares two sets of host key data by parsing known_hosts lines.
// It compares the key types and base64 key material, ignoring whitespace and comments.
func hostKeysMatch(stored, candidate []byte) bool {
	storedKeys := parseHostKeys(stored)
	candidateKeys := parseHostKeys(candidate)

	if len(storedKeys) == 0 || len(candidateKeys) == 0 {
		return false
	}

	// Every candidate key must have a matching stored key.
	for keyType, keyMaterial := range candidateKeys {
		storedMaterial, ok := storedKeys[keyType]
		if !ok || storedMaterial != keyMaterial {
			return false
		}
	}

	// Every stored key must be present in candidate.
	for keyType, keyMaterial := range storedKeys {
		candidateMaterial, ok := candidateKeys[keyType]
		if !ok || candidateMaterial != keyMaterial {
			return false
		}
	}

	return true
}

// parseHostKeys parses known_hosts format data into a map of key type -> base64 key material.
// Lines are formatted as: [host] [keytype] [base64key] [optional-comment]
func parseHostKeys(data []byte) map[string]string {
	keys := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			keys[fields[1]] = fields[2]
		}
	}
	return keys
}

// RemoveSSHDir removes the entire SSH directory for a VM (keys, known_hosts, etc.).
// REQ-004-031: sd destroy removes the stored known_hosts along with SSH key pair.
func RemoveSSHDir(sdHome, vmName string) error {
	dir, _, _ := KeyPaths(sdHome, vmName)
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove SSH dir for %q: %w", vmName, err)
	}
	return nil
}

// --- REQ-007-007: Port Forwarding ---

// PortForward specifies a host-to-guest port mapping.
type PortForward struct {
	BindAddr  string // Host bind address. Default: "127.0.0.1".
	HostPort  int    // Port on the host.
	GuestPort int    // Port inside the VM.
}

// ParsePortForward parses a port forward specification.
// Formats: "hostPort:guestPort" or "bindAddr:hostPort:guestPort".
func ParsePortForward(spec string) (PortForward, error) {
	if m := portForwardThree.FindStringSubmatch(spec); m != nil {
		hostPort, err := strconv.Atoi(m[2])
		if err != nil {
			return PortForward{}, fmt.Errorf("%w: invalid host port %q", ErrInvalidPortForward, m[2])
		}
		guestPort, err := strconv.Atoi(m[3])
		if err != nil {
			return PortForward{}, fmt.Errorf("%w: invalid guest port %q", ErrInvalidPortForward, m[3])
		}
		if err := validatePort(hostPort); err != nil {
			return PortForward{}, fmt.Errorf("%w: host port: %v", ErrInvalidPortForward, err)
		}
		if err := validatePort(guestPort); err != nil {
			return PortForward{}, fmt.Errorf("%w: guest port: %v", ErrInvalidPortForward, err)
		}
		return PortForward{BindAddr: m[1], HostPort: hostPort, GuestPort: guestPort}, nil
	}

	if m := portForwardTwo.FindStringSubmatch(spec); m != nil {
		hostPort, err := strconv.Atoi(m[1])
		if err != nil {
			return PortForward{}, fmt.Errorf("%w: invalid host port %q", ErrInvalidPortForward, m[1])
		}
		guestPort, err := strconv.Atoi(m[2])
		if err != nil {
			return PortForward{}, fmt.Errorf("%w: invalid guest port %q", ErrInvalidPortForward, m[2])
		}
		if err := validatePort(hostPort); err != nil {
			return PortForward{}, fmt.Errorf("%w: host port: %v", ErrInvalidPortForward, err)
		}
		if err := validatePort(guestPort); err != nil {
			return PortForward{}, fmt.Errorf("%w: guest port: %v", ErrInvalidPortForward, err)
		}
		return PortForward{BindAddr: "127.0.0.1", HostPort: hostPort, GuestPort: guestPort}, nil
	}

	return PortForward{}, fmt.Errorf("%w: expected hostPort:guestPort or bindAddr:hostPort:guestPort, got %q",
		ErrInvalidPortForward, spec)
}

func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%w: %d (must be 1-65535)", ErrPortOutOfRange, port)
	}
	return nil
}

// SSHArgs returns the SSH -L arguments for the port forward.
func (pf PortForward) SSHArgs() string {
	return fmt.Sprintf("%s:%d:%d", pf.BindAddr, pf.HostPort, pf.GuestPort)
}

// --- REQ-007-009: Session Name Validation ---

// ValidateSessionName checks that a session name contains only
// alphanumeric characters, hyphens, and underscores.
func ValidateSessionName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: session name cannot be empty", ErrInvalidSessionName)
	}
	if !sessionNameRe.MatchString(name) {
		return fmt.Errorf("%w: %q contains invalid characters; use only alphanumeric, hyphens, and underscores",
			ErrInvalidSessionName, name)
	}
	return nil
}

// DefaultSessionName returns the default tmux session name for a VM.
func DefaultSessionName(vmName string) string {
	return "sd-" + vmName
}

// --- REQ-007-019: Environment Variable Resolution ---

// EnvResolveResult holds the results of environment variable resolution.
type EnvResolveResult struct {
	Resolved map[string]string // Fully resolved variables.
	Warnings []string          // Warnings for unresolvable references.
}

// ResolveEnvVars resolves ${VAR} references in environment variable values
// against the host environment. Unresolvable references produce warnings and
// are replaced with empty strings.
func ResolveEnvVars(vars map[string]string) EnvResolveResult {
	result := EnvResolveResult{
		Resolved: make(map[string]string, len(vars)),
	}

	for key, value := range vars {
		resolved, warnings := resolveValue(value)
		result.Resolved[key] = resolved
		result.Warnings = append(result.Warnings, warnings...)
	}

	return result
}

// resolveValue expands ${VAR} references in a single value.
func resolveValue(value string) (string, []string) {
	var warnings []string
	resolved := envVarRefRe.ReplaceAllStringFunc(value, func(match string) string {
		varName := envVarRefRe.FindStringSubmatch(match)[1]
		hostVal, ok := os.LookupEnv(varName)
		if !ok {
			warnings = append(warnings, fmt.Sprintf(
				"Environment variable ${%s} could not be resolved from host; set to empty string.", varName))
			return ""
		}
		return hostVal
	})
	return resolved, warnings
}

// SendEnvArgs returns the list of variable names to pass via SSH SendEnv.
// Only variables with the SD_, ANTHROPIC_, GITHUB_, or GH_ prefixes are included,
// matching the AcceptEnv configuration in the VM's sshd.
func SendEnvArgs(vars map[string]string) []string {
	prefixes := []string{"SD_", "ANTHROPIC_", "GITHUB_", "GH_"}
	var args []string
	for key := range vars {
		for _, prefix := range prefixes {
			if strings.HasPrefix(key, prefix) {
				args = append(args, key)
				break
			}
		}
	}
	return args
}

// --- REQ-007-021: Connection Error Reporting ---

// ConnectionError represents a structured SSH connection error.
type ConnectionError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	VM      string `json:"vm,omitempty"`
}

func (e *ConnectionError) Error() string { return e.Message }

// NewVMNotFoundError creates an error for a missing VM.
func NewVMNotFoundError(vmName string) *ConnectionError {
	return &ConnectionError{
		Code:    "vm_not_found",
		Message: fmt.Sprintf("VM %q not found. Run \"sd list\" to see available VMs.", vmName),
		VM:      vmName,
	}
}

// NewVMNotRunningError creates an error for a stopped VM.
func NewVMNotRunningError(vmName string) *ConnectionError {
	return &ConnectionError{
		Code:    "vm_not_running",
		Message: fmt.Sprintf("VM %q is not running. Start it with \"sd start %s\".", vmName, vmName),
		VM:      vmName,
	}
}

// NewVMAutoStartError creates an error when auto-start fails.
func NewVMAutoStartError(vmName, reason string) *ConnectionError {
	return &ConnectionError{
		Code:    "vm_start_failed",
		Message: fmt.Sprintf("Failed to start VM %q: %s. Check \"sd status %s\" for details.", vmName, reason, vmName),
		VM:      vmName,
	}
}

// NewSSHConnectionRefusedError creates an error for connection refused.
func NewSSHConnectionRefusedError(vmName, host string, port int) *ConnectionError {
	return &ConnectionError{
		Code:    "ssh_connection_refused",
		Message: fmt.Sprintf("Connection refused to VM %q (%s:%d). Is the VM running? Try \"sd status %s\".", vmName, host, port, vmName),
		VM:      vmName,
	}
}

// NewSSHAuthError creates an error for authentication failure.
func NewSSHAuthError(vmName, keyPath string) *ConnectionError {
	return &ConnectionError{
		Code:    "ssh_auth_failed",
		Message: fmt.Sprintf("Authentication failed for VM %q using key %s. Check SSH key at %s or try \"sd destroy %s && sd create %s\" to recreate.", vmName, keyPath, filepath.Dir(keyPath), vmName, vmName),
		VM:      vmName,
	}
}

// NewSSHTimeoutError creates an error for connection timeout.
func NewSSHTimeoutError(vmName string, timeout time.Duration) *ConnectionError {
	return &ConnectionError{
		Code:    "ssh_timeout",
		Message: fmt.Sprintf("Connection to VM %q timed out after %s. Check VM status with \"sd status %s\".", vmName, timeout, vmName),
		VM:      vmName,
	}
}

// NewTmuxNotInstalledError creates an error when tmux is missing in the VM.
func NewTmuxNotInstalledError(vmName string) *ConnectionError {
	return &ConnectionError{
		Code:    "tmux_not_installed",
		Message: fmt.Sprintf("tmux is not installed in VM %q. Provision it with \"sd provision %s\" or connect with --no-tmux.", vmName, vmName),
		VM:      vmName,
	}
}

// NewPortInUseError creates an error when a forwarded port is already in use.
func NewPortInUseError(port int) *ConnectionError {
	return &ConnectionError{
		Code:    "port_in_use",
		Message: fmt.Sprintf("Port %d is already in use on the host. Choose a different host port.", port),
	}
}

// NewSessionNameError creates an error for invalid session names.
func NewSessionNameError(name string) *ConnectionError {
	return &ConnectionError{
		Code:    "invalid_session_name",
		Message: fmt.Sprintf("Session name %q is invalid. Use only alphanumeric characters, hyphens, and underscores.", name),
	}
}

// NewMutualExclusionError creates an error for mutually exclusive flags.
func NewMutualExclusionError(flag1, flag2 string) *ConnectionError {
	return &ConnectionError{
		Code:    "mutual_exclusion",
		Message: fmt.Sprintf("--%s and --%s are mutually exclusive.", flag1, flag2),
	}
}

// NewVMStoppedNoStartError creates an error when VM is stopped with --no-start.
func NewVMStoppedNoStartError(vmName string) *ConnectionError {
	return &ConnectionError{
		Code:    "vm_stopped_no_start",
		Message: fmt.Sprintf("VM %q is stopped. Start it with \"sd start %s\" or connect without --no-start.", vmName, vmName),
		VM:      vmName,
	}
}

// NewWatchDirectionError creates an error when --watch is used with sync from.
func NewWatchDirectionError() *ConnectionError {
	return &ConnectionError{
		Code:    "invalid_watch_direction",
		Message: "--watch is only supported for \"sd sync to\" (host-to-VM direction).",
	}
}

// NewRsyncNotFoundError creates an error when rsync is not installed.
func NewRsyncNotFoundError() *ConnectionError {
	return &ConnectionError{
		Code:    "rsync_not_found",
		Message: "rsync is not installed on the host. Install it with \"brew install rsync\".",
	}
}
