// Package ssh — dedicated property-based tests for SSH key and config invariants.
// REQ-007-003: SSH Key Generation Per VM
// REQ-007-004: SSH Config Management
// REQ-007-005: VSOCK Transport Support
// REQ-007-007: Port Forwarding on Connect
// REQ-007-019: Environment Injection on Connect
// REQ-007-021: Connection Health and Error Reporting
package ssh

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================
// REQ-007-003: SSH Key Generation — Core Cryptographic Invariants
// ============================================================

// Property: Generated Ed25519 keys are in valid OpenSSH authorized_keys format.
// The public key blob must use the correct wire format:
//   uint32(len("ssh-ed25519")) + "ssh-ed25519" + uint32(32) + pubkey_bytes
func TestGenerateKeys_PublicKeyOpenSSHFormat_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`vm-[a-z0-9]{4,8}`).Draw(t, "name")
		dir, err := os.MkdirTemp("", "sd-ssh-rapid-*")
		require.NoError(t, err)
		defer os.RemoveAll(dir)
		sdHome := filepath.Join(dir, ".sd")

		require.NoError(t, GenerateKeys(sdHome, name))

		_, privPath, pubPath := KeyPaths(sdHome, name)

		// Parse private key to get expected public key.
		privData, err := os.ReadFile(privPath)
		require.NoError(t, err)
		block, _ := pem.Decode(privData)
		require.NotNil(t, block)
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		require.NoError(t, err)
		edKey := key.(ed25519.PrivateKey)
		expectedPub := edKey.Public().(ed25519.PublicKey)

		// Read and parse public key file.
		pubData, err := os.ReadFile(pubPath)
		require.NoError(t, err)
		pubStr := strings.TrimSpace(string(pubData))

		parts := strings.Fields(pubStr)
		require.GreaterOrEqual(t, len(parts), 2,
			"public key must have type, blob, and optional comment")

		assert.Equal(t, "ssh-ed25519", parts[0],
			"key type must be ssh-ed25519")

		blob, err := base64.StdEncoding.DecodeString(parts[1])
		require.NoError(t, err, "public key blob must be valid base64")

		// Verify wire format: uint32 length + "ssh-ed25519" + uint32 length + 32 bytes.
		require.GreaterOrEqual(t, len(blob), 4+11+4+32,
			"blob must contain wire-format header + 32-byte key")

		// Verify key type string.
		keyTypeLen := binary.BigEndian.Uint32(blob[0:4])
		assert.Equal(t, uint32(11), keyTypeLen, "key type length must be 11")
		assert.Equal(t, "ssh-ed25519", string(blob[4:4+11]))

		// Verify public key data length.
		pubLen := binary.BigEndian.Uint32(blob[15 : 15+4])
		assert.Equal(t, uint32(32), pubLen, "public key data length must be 32")

		// Verify the public key matches the private key's public portion.
		extractedPub := blob[19 : 19+32]
		assert.Equal(t, []byte(expectedPub), extractedPub,
			"public key from file must match private key's public portion")
	})
}

// Property: Private key is always valid PKCS8 PEM that parses as Ed25519.
func TestGenerateKeys_PrivateKeyValidEd25519_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`vm-[a-z0-9]{4,8}`).Draw(t, "name")
		dir, err := os.MkdirTemp("", "sd-ssh-rapid-*")
		require.NoError(t, err)
		defer os.RemoveAll(dir)
		sdHome := filepath.Join(dir, ".sd")

		require.NoError(t, GenerateKeys(sdHome, name))

		_, privPath, _ := KeyPaths(sdHome, name)
		privData, err := os.ReadFile(privPath)
		require.NoError(t, err)

		block, _ := pem.Decode(privData)
		require.NotNil(t, block, "private key must be valid PEM")
		assert.Equal(t, "PRIVATE KEY", block.Type)

		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		require.NoError(t, err)

		edKey, ok := key.(ed25519.PrivateKey)
		require.True(t, ok, "key must be Ed25519")
		assert.Equal(t, 64, len(edKey), "Ed25519 private key must be 64 bytes")
	})
}

// Property: Key pairs generated for different VMs are always cryptographically distinct.
func TestGenerateKeys_CrossVMDistinct_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name1 := rapid.StringMatching(`vm-[a-z0-9]{4,8}`).Draw(t, "name1")
		name2 := rapid.StringMatching(`vm-[a-z0-9]{4,8}`).Draw(t, "name2")
		if name1 == name2 {
			t.Skip("identical names")
		}

		dir, err := os.MkdirTemp("", "sd-ssh-rapid-*")
		require.NoError(t, err)
		defer os.RemoveAll(dir)
		sdHome := filepath.Join(dir, ".sd")

		require.NoError(t, GenerateKeys(sdHome, name1))
		require.NoError(t, GenerateKeys(sdHome, name2))

		_, priv1, _ := KeyPaths(sdHome, name1)
		_, priv2, _ := KeyPaths(sdHome, name2)

		k1, err := os.ReadFile(priv1)
		require.NoError(t, err)
		k2, err := os.ReadFile(priv2)
		require.NoError(t, err)

		assert.NotEqual(t, k1, k2, "keys for different VMs must be distinct")
	})
}

// Property: Private key file always has 0600 permissions.
func TestGenerateKeys_PrivKeyPerms_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`vm-[a-z0-9]{4,8}`).Draw(t, "name")
		dir, err := os.MkdirTemp("", "sd-ssh-rapid-*")
		require.NoError(t, err)
		defer os.RemoveAll(dir)
		sdHome := filepath.Join(dir, ".sd")

		require.NoError(t, GenerateKeys(sdHome, name))

		_, privPath, _ := KeyPaths(sdHome, name)
		info, err := os.Stat(privPath)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm(),
			"private key must be 0600")
	})
}

// Property: Key directory always has 0700 permissions.
func TestGenerateKeys_DirPerms_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`vm-[a-z0-9]{4,8}`).Draw(t, "name")
		dir, err := os.MkdirTemp("", "sd-ssh-rapid-*")
		require.NoError(t, err)
		defer os.RemoveAll(dir)
		sdHome := filepath.Join(dir, ".sd")

		require.NoError(t, GenerateKeys(sdHome, name))

		keyDir, _, _ := KeyPaths(sdHome, name)
		info, err := os.Stat(keyDir)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0700), info.Mode().Perm(),
			"key directory must be 0700")
	})
}

// ============================================================
// REQ-007-004 / REQ-007-005: SSH Config Fragment Invariants
// ============================================================

// Property: WriteFragment -> ReadFile round-trip preserves content.
func TestWriteFragment_RoundTrip_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`[a-z][a-z0-9-]{2,16}`).Draw(t, "vmName")
		transport := rapid.SampledFrom([]Transport{TransportTCP, TransportVSOCK}).Draw(t, "transport")

		dir, err := os.MkdirTemp("", "sd-ssh-rapid-*")
		require.NoError(t, err)
		defer os.RemoveAll(dir)
		sdHome := filepath.Join(dir, ".sd")
		opts := SSHFragmentOpts{
			VMName:       vmName,
			HostName:     "127.0.0.1",
			Port:         60022,
			User:         "dev",
			Transport:    transport,
			SDHome:       sdHome,
			ProxyCommand: "limactl ssh --stdio " + vmName,
		}

		require.NoError(t, WriteFragment(dir, opts))

		data, err := os.ReadFile(FragmentPath(dir, vmName))
		require.NoError(t, err)

		expected := GenerateFragment(opts)
		assert.Equal(t, expected, string(data),
			"written fragment must match generated content")
	})
}

// Property: All fragments contain the managed header as the first line.
func TestGenerateFragment_ManagedHeaderFirst_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`[a-z][a-z0-9-]{2,16}`).Draw(t, "vmName")
		transport := rapid.SampledFrom([]Transport{TransportTCP, TransportVSOCK}).Draw(t, "transport")

		opts := SSHFragmentOpts{
			VMName:       vmName,
			HostName:     "127.0.0.1",
			Port:         60022,
			User:         "dev",
			Transport:    transport,
			SDHome:       "/home/user/.sd",
			ProxyCommand: "limactl ssh --stdio " + vmName,
		}

		fragment := GenerateFragment(opts)
		lines := strings.SplitN(fragment, "\n", 2)
		require.GreaterOrEqual(t, len(lines), 1)
		assert.Equal(t, "# Managed by sd. Do not edit manually.", lines[0],
			"first line must be managed header")
	})
}

// Property: TCP and VSOCK fragments have mutually exclusive security settings.
func TestGenerateFragment_SecurityMutualExclusion_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`[a-z][a-z0-9-]{2,16}`).Draw(t, "vmName")

		tcpOpts := SSHFragmentOpts{
			VMName:    vmName,
			HostName:  "127.0.0.1",
			Port:      60022,
			User:      "dev",
			Transport: TransportTCP,
			SDHome:    "/home/user/.sd",
		}
		vsockOpts := SSHFragmentOpts{
			VMName:       vmName,
			User:         "dev",
			Transport:    TransportVSOCK,
			SDHome:       "/home/user/.sd",
			ProxyCommand: "limactl ssh --stdio " + vmName,
		}

		tcpFrag := GenerateFragment(tcpOpts)
		vsockFrag := GenerateFragment(vsockOpts)

		// TCP must have strict checking.
		assert.Contains(t, tcpFrag, "StrictHostKeyChecking yes")
		assert.NotContains(t, tcpFrag, "UserKnownHostsFile /dev/null")
		assert.Contains(t, tcpFrag, "known_hosts")

		// VSOCK must have relaxed checking with /dev/null.
		assert.Contains(t, vsockFrag, "StrictHostKeyChecking no")
		assert.Contains(t, vsockFrag, "UserKnownHostsFile /dev/null")
	})
}

// Property: RemoveFragment is always idempotent.
func TestRemoveFragment_AlwaysIdempotent_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`[a-z][a-z0-9-]{2,16}`).Draw(t, "vmName")
		dir, err := os.MkdirTemp("", "sd-ssh-rapid-*")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		// Remove nonexistent.
		assert.NoError(t, RemoveFragment(dir, vmName))
		// Remove again.
		assert.NoError(t, RemoveFragment(dir, vmName))

		// Write then remove twice.
		opts := SSHFragmentOpts{
			VMName:    vmName,
			HostName:  "127.0.0.1",
			Port:      22,
			User:      "dev",
			Transport: TransportTCP,
			SDHome:    filepath.Join(dir, ".sd"),
		}
		require.NoError(t, WriteFragment(dir, opts))
		assert.NoError(t, RemoveFragment(dir, vmName))
		assert.NoError(t, RemoveFragment(dir, vmName))
	})
}

// ============================================================
// REQ-007-007: Port Forwarding — Round-trip Invariants
// ============================================================

// Property: SSHArgs produces output that can be parsed back.
func TestPortForward_SSHArgsRoundTrip_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hostPort := rapid.IntRange(1, 65535).Draw(t, "hostPort")
		guestPort := rapid.IntRange(1, 65535).Draw(t, "guestPort")
		bindAddr := rapid.OneOf(
			rapid.Just("127.0.0.1"),
			rapid.Just("0.0.0.0"),
			rapid.Just("::1"),
		).Draw(t, "bindAddr")

		pf := PortForward{BindAddr: bindAddr, HostPort: hostPort, GuestPort: guestPort}
		args := pf.SSHArgs()

		parsed, err := ParsePortForward(args)
		require.NoError(t, err)
		assert.Equal(t, pf, parsed, "SSHArgs -> ParsePortForward must round-trip")
	})
}

// Property: Two-part format always defaults to 127.0.0.1.
func TestPortForward_TwoPartDefaultBind_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hostPort := rapid.IntRange(1, 65535).Draw(t, "hostPort")
		guestPort := rapid.IntRange(1, 65535).Draw(t, "guestPort")

		pf, err := ParsePortForward(intToStr(hostPort) + ":" + intToStr(guestPort))
		require.NoError(t, err)
		assert.Equal(t, "127.0.0.1", pf.BindAddr,
			"two-part format must default to localhost")
	})
}

// ============================================================
// REQ-007-019: Environment Variable Resolution Invariants
// ============================================================

// Property: SendEnvArgs only accepts variables with correct prefixes.
func TestSendEnvArgs_OnlyValidPrefixes_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		validVar := rapid.OneOf(
			rapid.Just("SD_FOO"),
			rapid.Just("ANTHROPIC_KEY"),
			rapid.Just("GITHUB_TOKEN"),
			rapid.Just("GH_PAT"),
			rapid.Just("SD_VM_NAME"),
		).Draw(t, "validVar")
		invalidVar := rapid.OneOf(
			rapid.Just("PATH"),
			rapid.Just("HOME"),
			rapid.Just("USER"),
			rapid.Just("LANG"),
			rapid.Just("TERM"),
			rapid.Just("SHELL"),
			rapid.Just("DISPLAY"),
		).Draw(t, "invalidVar")

		vars := map[string]string{validVar: "1", invalidVar: "2"}
		args := SendEnvArgs(vars)

		assert.Contains(t, args, validVar, "valid prefix variable must be included")
		assert.NotContains(t, args, invalidVar, "invalid prefix variable must be excluded")
	})
}

// Property: SendEnvArgs returns a subset of the input keys.
func TestSendEnvArgs_SubsetOfInput_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		key := rapid.StringMatching(`[A-Z][A-Z0-9_]{2,20}`).Draw(t, "key")
		vars := map[string]string{key: "value"}
		args := SendEnvArgs(vars)

		for _, arg := range args {
			_, ok := vars[arg]
			assert.True(t, ok, "SendEnvArgs returned key %q not in input", arg)
		}
	})
}

// ============================================================
// REQ-007-021: Connection Error — JSON Serialization Invariants
// ============================================================

// Property: All ConnectionError types serialize to valid JSON with required fields.
func TestConnectionError_JSONRoundTrip_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{4}`).Draw(t, "vmName")

		errors := []*ConnectionError{
			NewVMNotFoundError(vmName),
			NewVMNotRunningError(vmName),
			NewVMAutoStartError(vmName, "timeout"),
			NewSSHConnectionRefusedError(vmName, "127.0.0.1", 22),
			NewSSHAuthError(vmName, "/path/to/key"),
			NewSSHTimeoutError(vmName, 30e9),
			NewTmuxNotInstalledError(vmName),
			NewVMStoppedNoStartError(vmName),
		}

		for _, orig := range errors {
			data, err := json.Marshal(orig)
			require.NoError(t, err, "ConnectionError must serialize to JSON")

			var decoded ConnectionError
			require.NoError(t, json.Unmarshal(data, &decoded),
				"ConnectionError JSON must round-trip")

			assert.Equal(t, orig.Code, decoded.Code,
				"code must round-trip")
			assert.Equal(t, orig.Message, decoded.Message,
				"message must round-trip")
			assert.Equal(t, orig.VM, decoded.VM,
				"VM must round-trip")
		}
	})
}

// Property: All non-VM-specific errors still have valid codes and messages.
func TestConnectionError_NonVMErrors_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		errors := []*ConnectionError{
			NewPortInUseError(rapid.IntRange(1024, 65535).Draw(t, "port")),
			NewSessionNameError(rapid.StringMatching(`[a-z!@#]{2,10}`).Draw(t, "name")),
			NewMutualExclusionError("a", "b"),
			NewWatchDirectionError(),
			NewRsyncNotFoundError(),
		}

		for _, e := range errors {
			assert.NotEmpty(t, e.Code)
			assert.NotEmpty(t, e.Message)
			assert.NotEmpty(t, e.Error())
		}
	})
}
