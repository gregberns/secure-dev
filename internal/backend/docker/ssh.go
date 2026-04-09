// Package docker — SSH helpers for the Docker backend.
// REQ-008-009: Docker backend SSH key generation, injection, and execution.
package docker

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"sd/internal/backend"
)

// generateSSHKeys creates an ed25519 keypair at keyDir/id_ed25519 using
// the ssh-keygen CLI. Returns the path to the private key.
// REQ-008-009: SSH key generation for container access.
func generateSSHKeys(ctx context.Context, keyDir string) (string, error) {
	// Ensure the directory exists with restricted permissions.
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		return "", fmt.Errorf("creating SSH key directory %s: %w", keyDir, err)
	}

	keyPath := filepath.Join(keyDir, "id_ed25519")

	// Remove existing keys so ssh-keygen doesn't prompt for overwrite.
	_ = os.Remove(keyPath)
	_ = os.Remove(keyPath + ".pub")

	cmd := exec.CommandContext(ctx, "ssh-keygen", "-t", "ed25519", "-f", keyPath, "-N", "")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ssh-keygen: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	// Enforce key file permissions (ssh-keygen should set these, but be safe).
	if err := os.Chmod(keyPath, 0600); err != nil {
		return "", fmt.Errorf("setting permissions on %s: %w", keyPath, err)
	}
	if err := os.Chmod(keyPath+".pub", 0600); err != nil {
		return "", fmt.Errorf("setting permissions on %s.pub: %w", keyPath, err)
	}

	return keyPath, nil
}

// injectPublicKey reads the public key at pubKeyPath and writes it into the
// container's /home/ubuntu/.ssh/authorized_keys file, then sets ownership.
// REQ-008-009: Public key injection into container.
func injectPublicKey(ctx context.Context, containerName, pubKeyPath string) error {
	pubKey, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return fmt.Errorf("reading public key %s: %w", pubKeyPath, err)
	}

	// Write the key into authorized_keys via docker exec.
	content := strings.TrimSpace(string(pubKey))
	writeCmd := fmt.Sprintf("mkdir -p /home/ubuntu/.ssh && echo '%s' > /home/ubuntu/.ssh/authorized_keys && chmod 600 /home/ubuntu/.ssh/authorized_keys", content)
	if _, err := dockerCmd(ctx, "exec", containerName, "sh", "-c", writeCmd); err != nil {
		return fmt.Errorf("writing authorized_keys: %w", err)
	}

	// Set ownership to ubuntu:ubuntu.
	if _, err := dockerCmd(ctx, "exec", containerName, "chown", "-R", "ubuntu:ubuntu", "/home/ubuntu/.ssh"); err != nil {
		return fmt.Errorf("setting ownership on .ssh: %w", err)
	}

	return nil
}

// waitForSSH polls host:port until a TCP connection succeeds or the timeout
// (or context) expires. It uses a 500ms polling interval.
// REQ-008-009: SSH readiness polling.
func waitForSSH(ctx context.Context, host string, port int, timeout time.Duration) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for SSH at %s: %w", addr, ctx.Err())
		default:
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for SSH at %s after %s", addr, timeout)
		}

		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			conn.Close()
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for SSH at %s: %w", addr, ctx.Err())
		case <-ticker.C:
		}
	}
}

// getSSHConfig queries the container's mapped SSH port via `docker port` and
// returns a populated backend.SSHConfig.
// REQ-008-009: SSH configuration for Docker containers.
func getSSHConfig(ctx context.Context, containerName, vmName, keyPath string) (backend.SSHConfig, error) {
	// docker port <container> 22 returns lines like "0.0.0.0:32768" or
	// "0.0.0.0:32768\n:::32768".
	output, err := dockerCmd(ctx, "port", containerName, "22")
	if err != nil {
		return backend.SSHConfig{}, fmt.Errorf("querying SSH port for %s: %w", containerName, err)
	}

	port, err := parseDockerPort(output)
	if err != nil {
		return backend.SSHConfig{}, fmt.Errorf("parsing port from %q: %w", output, err)
	}

	return backend.SSHConfig{
		Host:         "127.0.0.1",
		Port:         port,
		User:         "ubuntu",
		IdentityFile: keyPath,
		Transport:    "tcp",
	}, nil
}

// parseDockerPort extracts the host port number from `docker port` output.
// The output format is one or more lines of "host:port", e.g.:
//
//	0.0.0.0:32768
//	:::32768
//
// We take the first IPv4 line's port (or any line's port as fallback).
func parseDockerPort(output string) (int, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip IPv6 lines (start with "[" or ":::") on first pass; prefer IPv4.
		if strings.HasPrefix(line, "[") || strings.HasPrefix(line, ":::") {
			continue
		}
		return extractPort(line)
	}
	// Fallback: try the first non-empty line regardless of format.
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return extractPort(line)
	}
	return 0, fmt.Errorf("no port mapping found in output")
}

// extractPort parses the port from a "host:port" string.
func extractPort(hostPort string) (int, error) {
	// Handle IPv6 bracket notation like [::]:32768
	if strings.HasPrefix(hostPort, "[") {
		// Format: [host]:port
		idx := strings.LastIndex(hostPort, "]:")
		if idx < 0 {
			return 0, fmt.Errorf("unexpected format %q", hostPort)
		}
		portStr := hostPort[idx+2:]
		return strconv.Atoi(portStr)
	}
	// Format: host:port — use the last colon to handle IPv4.
	idx := strings.LastIndex(hostPort, ":")
	if idx < 0 {
		return 0, fmt.Errorf("no colon in %q", hostPort)
	}
	return strconv.Atoi(hostPort[idx+1:])
}

// execViaSSH runs a command over SSH using the system's ssh binary and
// returns the result. It disables host key checking for ephemeral containers.
// REQ-008-009: Command execution via SSH.
func execViaSSH(ctx context.Context, cfg backend.SSHConfig, command []string) (backend.ExecResult, error) {
	args := []string{
		"-i", cfg.IdentityFile,
		"-p", strconv.Itoa(cfg.Port),
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		// Disable pseudo-terminal allocation for non-interactive use.
		"-T",
		fmt.Sprintf("%s@%s", cfg.User, cfg.Host),
	}
	// Shell-quote each part of the remote command and join into a single
	// string. SSH concatenates remote args with spaces on the remote side,
	// which destroys quoting of special characters (pipes, semicolons, etc.).
	// Passing one pre-quoted string preserves the caller's intent.
	quoted := make([]string, len(command))
	for i, arg := range command {
		quoted[i] = shellQuote(arg)
	}
	args = append(args, strings.Join(quoted, " "))

	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := backend.ExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			// Could not determine exit code — treat as general failure.
			result.ExitCode = 1
			result.Stderr = strings.TrimSpace(result.Stderr + "\n" + err.Error())
		}
	}

	return result, nil
}

// sshKeyDir returns the directory where SSH keys are stored for a given VM.
// Convention: <sdHome>/vms/<name>/ssh/
func sshKeyDir(vmName string) string {
	home := os.Getenv("SD_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			// Fallback if UserHomeDir fails (should never happen in practice).
			userHome = os.Getenv("HOME")
		}
		home = filepath.Join(userHome, ".sd")
	}
	return filepath.Join(home, "vms", vmName, "ssh")
}

// shellQuote returns a shell-safe representation of s. If s contains no
// special characters it is returned as-is; otherwise it is wrapped in single
// quotes with any embedded single quotes escaped as '\'' (end quote, literal
// quote, start quote). Empty strings are returned as ''.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	// If the string is "safe" (only alphanumeric, dash, underscore, dot, slash,
	// colon, plus, at, percent, comma, equal) return it unquoted.
	safe := true
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '/' || c == ':' || c == '+' || c == '@' || c == '%' || c == ',' || c == '=') {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	// Wrap in single quotes, escaping any embedded single quotes.
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
