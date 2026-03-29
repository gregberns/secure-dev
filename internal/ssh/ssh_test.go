package ssh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- REQ-007-003: SSH Key Generation ---

func TestGenerateKeys_CreatesKeyPair(t *testing.T) {
	dir := t.TempDir()
	sdHome := filepath.Join(dir, ".sd")

	err := GenerateKeys(sdHome, "testvm")
	require.NoError(t, err)

	_, privPath, pubPath := KeyPaths(sdHome, "testvm")

	// Private key exists with correct permissions.
	info, err := os.Stat(privPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm(), "private key must be 0600")

	// Directory has 0700.
	sshDir := filepath.Dir(privPath)
	dirInfo, err := os.Stat(sshDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0700), dirInfo.Mode().Perm(), "ssh directory must be 0700")

	// Public key exists.
	_, err = os.Stat(pubPath)
	require.NoError(t, err)

	// Public key contains ssh-ed25519.
	pubContent, err := os.ReadFile(pubPath)
	require.NoError(t, err)
	assert.Contains(t, string(pubContent), "ssh-ed25519")
}

func TestGenerateKeys_RejectsOverwrite(t *testing.T) {
	dir := t.TempDir()
	sdHome := filepath.Join(dir, ".sd")

	err := GenerateKeys(sdHome, "testvm")
	require.NoError(t, err)

	err = GenerateKeys(sdHome, "testvm")
	assert.ErrorIs(t, err, ErrKeyExists)
}

func TestGenerateKeys_KeysAreUnique(t *testing.T) {
	dir := t.TempDir()
	sdHome := filepath.Join(dir, ".sd")

	err := GenerateKeys(sdHome, "vm1")
	require.NoError(t, err)
	err = GenerateKeys(sdHome, "vm2")
	require.NoError(t, err)

	_, priv1, pub1 := KeyPaths(sdHome, "vm1")
	_, priv2, pub2 := KeyPaths(sdHome, "vm2")

	key1, err := os.ReadFile(priv1)
	require.NoError(t, err)
	key2, err := os.ReadFile(priv2)
	require.NoError(t, err)
	assert.NotEqual(t, key1, key2, "different VMs must have different private keys")

	pk1, err := os.ReadFile(pub1)
	require.NoError(t, err)
	pk2, err := os.ReadFile(pub2)
	require.NoError(t, err)
	assert.NotEqual(t, pk1, pk2, "different VMs must have different public keys")
}

func TestGenerateKeys_PathStructure(t *testing.T) {
	dir := t.TempDir()
	sdHome := filepath.Join(dir, ".sd")

	dirPath, privPath, pubPath := KeyPaths(sdHome, "myvm")

	assert.Equal(t, filepath.Join(sdHome, "vms", "myvm", "ssh"), dirPath)
	assert.Equal(t, filepath.Join(sdHome, "vms", "myvm", "ssh", "id_ed25519"), privPath)
	assert.Equal(t, filepath.Join(sdHome, "vms", "myvm", "ssh", "id_ed25519.pub"), pubPath)
}

// Property: Each call to GenerateKeys produces a unique key pair.
func TestGenerateKeys_KeysAlwaysUnique_Property(t *testing.T) {
	dir := t.TempDir()
	sdHome := filepath.Join(dir, ".sd")

	rapid.Check(t, func(t *rapid.T) {
		name1 := rapid.StringMatching(`vm-[a-z0-9]{4,8}`).Draw(t, "name1")
		name2 := rapid.StringMatching(`vm-[a-z0-9]{4,8}`).Draw(t, "name2")
		if name1 == name2 {
			t.Skip("identical names")
		}

		subDir := t.Name()
		sdSub := filepath.Join(sdHome, subDir)
		require.NoError(t, GenerateKeys(sdSub, name1))
		require.NoError(t, GenerateKeys(sdSub, name2))

		_, priv1, _ := KeyPaths(sdSub, name1)
		_, priv2, _ := KeyPaths(sdSub, name2)

		k1, err := os.ReadFile(priv1)
		require.NoError(t, err)
		k2, err := os.ReadFile(priv2)
		require.NoError(t, err)
		assert.NotEqual(t, k1, k2, "keys for different VMs must differ")
	})
}

// Property: Key directory always has 0700 permissions.
func TestGenerateKeys_DirectoryPermissions_Property(t *testing.T) {
	dir := t.TempDir()
	sdHome := filepath.Join(dir, ".sd")

	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`vm-[a-z0-9]{4,8}`).Draw(t, "name")
		sdSub := filepath.Join(sdHome, t.Name())

		require.NoError(t, GenerateKeys(sdSub, name))

		keyDir, _, _ := KeyPaths(sdSub, name)
		info, err := os.Stat(keyDir)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0700), info.Mode().Perm())
	})
}

// --- REQ-007-004: SSH Config Fragment ---

func TestGenerateFragment_TCP(t *testing.T) {
	opts := SSHFragmentOpts{
		VMName:    "myvm",
		HostName:  "127.0.0.1",
		Port:      60022,
		User:      "dev",
		Transport: TransportTCP,
		SDHome:    "/home/user/.sd",
	}

	fragment := GenerateFragment(opts)
	require.Contains(t, fragment, "Host sd-myvm")
	require.Contains(t, fragment, "HostName 127.0.0.1")
	require.Contains(t, fragment, "Port 60022")
	require.Contains(t, fragment, "User dev")
	require.Contains(t, fragment, "IdentityFile /home/user/.sd/vms/myvm/ssh/id_ed25519")
	require.Contains(t, fragment, "StrictHostKeyChecking yes")
	require.Contains(t, fragment, "UserKnownHostsFile /home/user/.sd/vms/myvm/ssh/known_hosts")
	require.Contains(t, fragment, "ForwardAgent no")
	require.Contains(t, fragment, "ForwardX11 no")
	require.Contains(t, fragment, "LogLevel ERROR")
	require.Contains(t, fragment, "SendEnv SD_* ANTHROPIC_* GITHUB_* GH_*")
}

func TestGenerateFragment_VSOCK(t *testing.T) {
	opts := SSHFragmentOpts{
		VMName:       "myvm",
		User:         "dev",
		Transport:    TransportVSOCK,
		SDHome:       "/home/user/.sd",
		ProxyCommand: "limactl ssh --stdio myvm",
	}

	fragment := GenerateFragment(opts)
	require.Contains(t, fragment, "Host sd-myvm")
	require.Contains(t, fragment, "ProxyCommand limactl ssh --stdio myvm")
	require.Contains(t, fragment, "StrictHostKeyChecking no")
	require.Contains(t, fragment, "UserKnownHostsFile /dev/null")
	require.Contains(t, fragment, "ForwardAgent no")
	require.Contains(t, fragment, "ForwardX11 no")
	require.Contains(t, fragment, "LogLevel ERROR")

	// VSOCK should NOT contain HostName or Port.
	assert.NotContains(t, fragment, "HostName")
	assert.NotContains(t, fragment, "Port 60022")
}

func TestGenerateFragment_ManagedHeader(t *testing.T) {
	opts := SSHFragmentOpts{
		VMName:    "testvm",
		HostName:  "127.0.0.1",
		Port:      22,
		User:      "dev",
		Transport: TransportTCP,
		SDHome:    "/tmp/test",
	}
	fragment := GenerateFragment(opts)
	require.Contains(t, fragment, "# Managed by sd. Do not edit manually.")
}

func TestWriteFragment_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	opts := SSHFragmentOpts{
		VMName:    "myvm",
		HostName:  "127.0.0.1",
		Port:      60022,
		User:      "dev",
		Transport: TransportTCP,
		SDHome:    filepath.Join(dir, ".sd"),
	}

	err := WriteFragment(dir, opts)
	require.NoError(t, err)

	fragmentPath := FragmentPath(dir, "myvm")
	data, err := os.ReadFile(fragmentPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "Host sd-myvm")
}

func TestWriteRemoveFragment_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	opts := SSHFragmentOpts{
		VMName:    "testvm",
		HostName:  "127.0.0.1",
		Port:      22,
		User:      "dev",
		Transport: TransportTCP,
		SDHome:    filepath.Join(dir, ".sd"),
	}

	require.NoError(t, WriteFragment(dir, opts))
	require.NoError(t, RemoveFragment(dir, "testvm"))

	_, err := os.Stat(FragmentPath(dir, "testvm"))
	assert.True(t, os.IsNotExist(err))
}

func TestRemoveFragment_Idempotent(t *testing.T) {
	dir := t.TempDir()
	err := RemoveFragment(dir, "nonexistent")
	assert.NoError(t, err, "removing nonexistent fragment should not error")
}

func TestNeedsInclude(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", true},
		{"no_include", "Host foo\n    HostName 1.2.3.4\n", true},
		{"has_include", "Include config.d/*\n", false},
		{"has_include_with_other", "Host foo\n    HostName 1.2.3.4\n\nInclude config.d/*\n", false},
		{"wrong_include", "Include other/*\n", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NeedsInclude([]byte(tt.content)))
		})
	}
}

// Property: TCP fragments always contain HostName, Port, StrictHostKeyChecking yes.
func TestGenerateFragment_TCPAlwaysStrict_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`[a-z][a-z0-9-]{2,16}`).Draw(t, "vmName")
		ip := rapid.StringMatching(`127\.0\.0\.[0-9]+`).Draw(t, "ip")
		port := rapid.IntRange(1024, 65535).Draw(t, "port")

		opts := SSHFragmentOpts{
			VMName:    vmName,
			HostName:  ip,
			Port:      port,
			User:      "dev",
			Transport: TransportTCP,
			SDHome:    "/home/user/.sd",
		}
		fragment := GenerateFragment(opts)

		assert.Contains(t, fragment, "StrictHostKeyChecking yes")
		assert.Contains(t, fragment, "HostName "+ip)
		assert.Contains(t, fragment, "UserKnownHostsFile")
		assert.Contains(t, fragment, "ForwardAgent no")
		assert.Contains(t, fragment, "ForwardX11 no")
	})
}

// Property: VSOCK fragments always contain StrictHostKeyChecking no, never HostName.
func TestGenerateFragment_VSOCKNeverHostName_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`[a-z][a-z0-9-]{2,16}`).Draw(t, "vmName")

		opts := SSHFragmentOpts{
			VMName:       vmName,
			User:         "dev",
			Transport:    TransportVSOCK,
			SDHome:       "/home/user/.sd",
			ProxyCommand: "limactl ssh --stdio " + vmName,
		}
		fragment := GenerateFragment(opts)

		assert.Contains(t, fragment, "StrictHostKeyChecking no")
		assert.Contains(t, fragment, "UserKnownHostsFile /dev/null")
		assert.NotContains(t, fragment, "HostName")
		assert.Contains(t, fragment, "ProxyCommand limactl ssh --stdio "+vmName)
	})
}

// Property: Fragment always starts with managed header and contains required directives.
func TestGenerateFragment_RequiredDirectives_Property(t *testing.T) {
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

		assert.True(t, strings.HasPrefix(fragment, "# Managed by sd."),
			"fragment must start with managed header")
		assert.Contains(t, fragment, "Host sd-"+vmName)
		assert.Contains(t, fragment, "User dev")
		assert.Contains(t, fragment, "IdentityFile")
		assert.Contains(t, fragment, "ForwardAgent no")
		assert.Contains(t, fragment, "ForwardX11 no")
		assert.Contains(t, fragment, "LogLevel ERROR")
		assert.Contains(t, fragment, "SendEnv SD_* ANTHROPIC_* GITHUB_* GH_*")
	})
}

// --- REQ-007-007: Port Forwarding ---

func TestParsePortForward_TwoPart(t *testing.T) {
	pf, err := ParsePortForward("8080:80")
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1", pf.BindAddr)
	assert.Equal(t, 8080, pf.HostPort)
	assert.Equal(t, 80, pf.GuestPort)
}

func TestParsePortForward_ThreePart(t *testing.T) {
	pf, err := ParsePortForward("0.0.0.0:8080:80")
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", pf.BindAddr)
	assert.Equal(t, 8080, pf.HostPort)
	assert.Equal(t, 80, pf.GuestPort)
}

func TestParsePortForward_InvalidFormats(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		contains string // substring that must appear in error message
	}{
		{"empty", "", true, "invalid port forward"},
		{"no_colon", "8080", true, "invalid port forward"},
		{"too_many_colons", "a:b:c:d", true, "invalid port forward"},
		{"non_numeric", "abc:def", true, "invalid port forward"},
		{"zero_port", "0:80", true, "port out of valid range"},
		{"negative", "-1:80", true, "invalid port forward"},
		{"out_of_range", "99999:80", true, "port out of valid range"},
		{"guest_zero", "8080:0", true, "port out of valid range"},
		{"guest_out_of_range", "8080:99999", true, "port out of valid range"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePortForward(tt.input)
			require.Error(t, err, "ParsePortForward(%q) should error", tt.input)
			assert.Contains(t, err.Error(), tt.contains)
		})
	}
}

func TestPortForward_SSHArgs(t *testing.T) {
	pf := PortForward{BindAddr: "127.0.0.1", HostPort: 8080, GuestPort: 80}
	assert.Equal(t, "127.0.0.1:8080:80", pf.SSHArgs())

	pf2 := PortForward{BindAddr: "0.0.0.0", HostPort: 3000, GuestPort: 3000}
	assert.Equal(t, "0.0.0.0:3000:3000", pf2.SSHArgs())
}

// Property: Round-trip for two-part port forwards.
func TestParsePortForward_RoundTrip_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hostPort := rapid.IntRange(1, 65535).Draw(t, "hostPort")
		guestPort := rapid.IntRange(1, 65535).Draw(t, "guestPort")

		spec := intToStr(hostPort) + ":" + intToStr(guestPort)
		pf, err := ParsePortForward(spec)
		require.NoError(t, err)
		assert.Equal(t, "127.0.0.1", pf.BindAddr)
		assert.Equal(t, hostPort, pf.HostPort)
		assert.Equal(t, guestPort, pf.GuestPort)
	})
}

// Property: Any parsed two-part spec has BindAddr "127.0.0.1".
func TestParsePortForward_TwoPartDefaultBindAddr_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hostPort := rapid.IntRange(1, 65535).Draw(t, "hostPort")
		guestPort := rapid.IntRange(1, 65535).Draw(t, "guestPort")

		// Can't use strconv in rapid directly, so use a different approach
		spec := intToStr(hostPort) + ":" + intToStr(guestPort)

		pf, err := ParsePortForward(spec)
		require.NoError(t, err)
		assert.Equal(t, "127.0.0.1", pf.BindAddr)
		assert.Equal(t, hostPort, pf.HostPort)
		assert.Equal(t, guestPort, pf.GuestPort)
	})
}

// Property: Any parsed three-part spec preserves the bind address exactly.
func TestParsePortForward_ThreePartPreservesBindAddr_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		bindAddr := rapid.OneOf(
			rapid.Just("127.0.0.1"),
			rapid.Just("0.0.0.0"),
			rapid.Just("::1"),
			rapid.Just("192.168.1.1"),
		).Draw(t, "bindAddr")
		hostPort := rapid.IntRange(1, 65535).Draw(t, "hostPort")
		guestPort := rapid.IntRange(1, 65535).Draw(t, "guestPort")

		spec := bindAddr + ":" + intToStr(hostPort) + ":" + intToStr(guestPort)
		pf, err := ParsePortForward(spec)
		require.NoError(t, err)
		assert.Equal(t, bindAddr, pf.BindAddr)
		assert.Equal(t, hostPort, pf.HostPort)
		assert.Equal(t, guestPort, pf.GuestPort)
	})
}

// Property: Ports outside 1-65535 always fail.
func TestParsePortForward_InvalidPortsFail_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		invalidPort := rapid.IntRange(-1000, 0).Draw(t, "invalidPort")
		spec := intToStr(invalidPort) + ":8080"
		_, err := ParsePortForward(spec)
		assert.Error(t, err, "port %d should be invalid", invalidPort)
	})
}

// intToStr converts int to string for use in rapid generators.
func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}

// --- REQ-007-009: Session Name Validation ---

func TestValidateSessionName_Valid(t *testing.T) {
	valid := []string{"work", "session-1", "my_session", "A", "abc123", "sd-myvm"}
	for _, name := range valid {
		t.Run(name, func(t *testing.T) {
			assert.NoError(t, ValidateSessionName(name))
		})
	}
}

func TestValidateSessionName_Invalid(t *testing.T) {
	invalid := []string{
		"", "my session", "session!", "name@work", "a.b", "has/slash",
		"has space", "tab\there", "new\nline", "unicode\xc0",
	}
	for _, name := range invalid {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, ValidateSessionName(name), ErrInvalidSessionName)
		})
	}
}

func TestDefaultSessionName(t *testing.T) {
	assert.Equal(t, "sd-myvm", DefaultSessionName("myvm"))
	assert.Equal(t, "sd-", DefaultSessionName(""))
}

// Property: Alphanumeric, hyphen, underscore names always pass validation.
func TestValidateSessionName_ValidChars_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`[a-zA-Z0-9_-]{1,20}`).Draw(t, "name")
		if name == "" {
			t.Skip("empty name")
		}
		// Filter out names that might be empty after regex
		if len(name) > 0 {
			assert.NoError(t, ValidateSessionName(name))
		}
	})
}

// Property: Names containing spaces always fail.
func TestValidateSessionName_SpacesAlwaysFail_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		part1 := rapid.StringMatching(`[a-z]{2,5}`).Draw(t, "part1")
		part2 := rapid.StringMatching(`[a-z]{2,5}`).Draw(t, "part2")
		name := part1 + " " + part2
		assert.ErrorIs(t, ValidateSessionName(name), ErrInvalidSessionName)
	})
}

// Property: Names containing special chars always fail.
func TestValidateSessionName_SpecialCharsFail_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		prefix := rapid.StringMatching(`[a-z]{2,5}`).Draw(t, "prefix")
		special := rapid.SampledFrom([]string{"!", "@", "#", "$", "%", ".", "/", "\\"}).Draw(t, "special")
		suffix := rapid.StringMatching(`[a-z]{2,5}`).Draw(t, "suffix")
		name := prefix + special + suffix
		assert.ErrorIs(t, ValidateSessionName(name), ErrInvalidSessionName)
	})
}

// --- REQ-007-019: Environment Variable Resolution ---

func TestResolveEnvVars_NoReferences(t *testing.T) {
	vars := map[string]string{
		"SD_FOO": "hello",
		"SD_BAR": "world",
	}
	result := ResolveEnvVars(vars)
	assert.Equal(t, "hello", result.Resolved["SD_FOO"])
	assert.Equal(t, "world", result.Resolved["SD_BAR"])
	assert.Empty(t, result.Warnings)
}

func TestResolveEnvVars_ResolvesHostVars(t *testing.T) {
	t.Setenv("TEST_HOST_VAR", "resolved_value")

	vars := map[string]string{
		"SD_TOKEN": "${TEST_HOST_VAR}",
	}
	result := ResolveEnvVars(vars)
	assert.Equal(t, "resolved_value", result.Resolved["SD_TOKEN"])
	assert.Empty(t, result.Warnings)
}

func TestResolveEnvVars_UnresolvableProducesWarning(t *testing.T) {
	vars := map[string]string{
		"SD_MISSING": "${NONEXISTENT_VAR_12345}",
	}
	result := ResolveEnvVars(vars)
	assert.Equal(t, "", result.Resolved["SD_MISSING"])
	require.NotEmpty(t, result.Warnings)
	assert.Contains(t, result.Warnings[0], "NONEXISTENT_VAR_12345")
}

func TestResolveEnvVars_MultipleReferencesInValue(t *testing.T) {
	t.Setenv("TEST_HOST_A", "aaa")
	t.Setenv("TEST_HOST_B", "bbb")

	vars := map[string]string{
		"SD_COMBINED": "${TEST_HOST_A}-${TEST_HOST_B}",
	}
	result := ResolveEnvVars(vars)
	assert.Equal(t, "aaa-bbb", result.Resolved["SD_COMBINED"])
	assert.Empty(t, result.Warnings)
}

func TestResolveEnvVars_MixedResolvedAndUnresolved(t *testing.T) {
	t.Setenv("TEST_RESOLVE_ME", "yes")

	vars := map[string]string{
		"SD_GOOD": "${TEST_RESOLVE_ME}",
		"SD_BAD":  "${IMPOSSIBLE_TO_FIND_12345}",
	}
	result := ResolveEnvVars(vars)
	assert.Equal(t, "yes", result.Resolved["SD_GOOD"])
	assert.Equal(t, "", result.Resolved["SD_BAD"])
	assert.Len(t, result.Warnings, 1)
}

func TestResolveEnvVars_EmptyInput(t *testing.T) {
	result := ResolveEnvVars(map[string]string{})
	assert.Empty(t, result.Resolved)
	assert.Empty(t, result.Warnings)
}

// Property: Values without ${...} references are always returned unchanged.
func TestResolveEnvVars_NoRefUnchanged_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		value := rapid.StringMatching(`[a-zA-Z0-9._:/-]{1,50}`).Draw(t, "value")
		vars := map[string]string{"SD_TEST": value}
		result := ResolveEnvVars(vars)
		assert.Equal(t, value, result.Resolved["SD_TEST"])
		assert.Empty(t, result.Warnings)
	})
}

// Property: Resolving a variable that exists always succeeds without warnings.
func TestResolveEnvVars_ExistingVarNoWarning_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		value := rapid.StringMatching(`[a-zA-Z0-9]{4,12}`).Draw(t, "value")
		envName := "TEST_RAPID_" + value
		os.Setenv(envName, value)
		defer os.Unsetenv(envName)

		vars := map[string]string{"SD_VAL": "${" + envName + "}"}
		result := ResolveEnvVars(vars)
		assert.Equal(t, value, result.Resolved["SD_VAL"])
		assert.Empty(t, result.Warnings)
	})
}

// Property: Unresolvable references always produce warnings.
func TestResolveEnvVars_UnresolvableAlwaysWarns_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		suffix := rapid.StringMatching(`[A-Z0-9]{4,12}`).Draw(t, "suffix")
		varName := "NONEXISTENT_" + suffix
		vars := map[string]string{"SD_VAL": "${" + varName + "}"}
		result := ResolveEnvVars(vars)
		assert.Equal(t, "", result.Resolved["SD_VAL"])
		assert.NotEmpty(t, result.Warnings, "unresolvable ${%s} should produce a warning", varName)
	})
}

// --- SendEnvArgs ---

func TestSendEnvArgs_FilterByPrefix(t *testing.T) {
	vars := map[string]string{
		"SD_FOO":           "1",
		"ANTHROPIC_KEY":    "2",
		"GITHUB_TOKEN":     "3",
		"GH_PAT":           "4",
		"OTHER_VAR":        "5",
		"PATH":             "6",
	}
	args := SendEnvArgs(vars)

	assert.Contains(t, args, "SD_FOO")
	assert.Contains(t, args, "ANTHROPIC_KEY")
	assert.Contains(t, args, "GITHUB_TOKEN")
	assert.Contains(t, args, "GH_PAT")
	assert.NotContains(t, args, "OTHER_VAR")
	assert.NotContains(t, args, "PATH")
}

func TestSendEnvArgs_Empty(t *testing.T) {
	args := SendEnvArgs(map[string]string{})
	assert.Empty(t, args)
}

// --- REQ-007-021: Connection Error Reporting ---

func TestConnectionError_VMNotFound(t *testing.T) {
	err := NewVMNotFoundError("testvm")
	assert.Equal(t, "vm_not_found", err.Code)
	assert.Contains(t, err.Message, "testvm")
	assert.Contains(t, err.Message, "sd list")
	assert.Equal(t, "testvm", err.VM)
}

func TestConnectionError_VMNotRunning(t *testing.T) {
	err := NewVMNotRunningError("myvm")
	assert.Equal(t, "vm_not_running", err.Code)
	assert.Contains(t, err.Message, "myvm")
	assert.Contains(t, err.Message, "sd start")
}

func TestConnectionError_VMAutoStart(t *testing.T) {
	err := NewVMAutoStartError("myvm", "timeout")
	assert.Equal(t, "vm_start_failed", err.Code)
	assert.Contains(t, err.Message, "timeout")
	assert.Contains(t, err.Message, "sd status")
}

func TestConnectionError_SSHConnectionRefused(t *testing.T) {
	err := NewSSHConnectionRefusedError("myvm", "127.0.0.1", 60022)
	assert.Equal(t, "ssh_connection_refused", err.Code)
	assert.Contains(t, err.Message, "127.0.0.1:60022")
	assert.Contains(t, err.Message, "sd status")
}

func TestConnectionError_SSHAuth(t *testing.T) {
	err := NewSSHAuthError("myvm", "/home/user/.sd/vms/myvm/ssh/id_ed25519")
	assert.Equal(t, "ssh_auth_failed", err.Code)
	assert.Contains(t, err.Message, "id_ed25519")
	assert.Contains(t, err.Message, "sd destroy")
}

func TestConnectionError_SSHTimeout(t *testing.T) {
	err := NewSSHTimeoutError("myvm", 30*1e9) // 30s
	assert.Equal(t, "ssh_timeout", err.Code)
	assert.Contains(t, err.Message, "30s")
	assert.Contains(t, err.Message, "sd status")
}

func TestConnectionError_TmuxNotInstalled(t *testing.T) {
	err := NewTmuxNotInstalledError("myvm")
	assert.Equal(t, "tmux_not_installed", err.Code)
	assert.Contains(t, err.Message, "sd provision")
	assert.Contains(t, err.Message, "--no-tmux")
}

func TestConnectionError_PortInUse(t *testing.T) {
	err := NewPortInUseError(8080)
	assert.Equal(t, "port_in_use", err.Code)
	assert.Contains(t, err.Message, "8080")
}

func TestConnectionError_VMStoppedNoStart(t *testing.T) {
	err := NewVMStoppedNoStartError("myvm")
	assert.Equal(t, "vm_stopped_no_start", err.Code)
	assert.Contains(t, err.Message, "--no-start")
}

func TestConnectionError_SessionName(t *testing.T) {
	err := NewSessionNameError("bad name!")
	assert.Equal(t, "invalid_session_name", err.Code)
	assert.Contains(t, err.Message, "bad name!")
}

func TestConnectionError_MutualExclusion(t *testing.T) {
	err := NewMutualExclusionError("no-tmux", "new-window")
	assert.Equal(t, "mutual_exclusion", err.Code)
	assert.Contains(t, err.Message, "no-tmux")
	assert.Contains(t, err.Message, "new-window")
}

func TestConnectionError_WatchDirection(t *testing.T) {
	err := NewWatchDirectionError()
	assert.Equal(t, "invalid_watch_direction", err.Code)
	assert.Contains(t, err.Message, "sync to")
}

func TestConnectionError_RsyncNotFound(t *testing.T) {
	err := NewRsyncNotFoundError()
	assert.Equal(t, "rsync_not_found", err.Code)
	assert.Contains(t, err.Message, "brew install rsync")
}

// Property: All connection errors contain the VM name when applicable.
func TestConnectionError_VMNameInMessage_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`[a-z][a-z0-9-]{2,16}`).Draw(t, "vmName")

		errors := []*ConnectionError{
			NewVMNotFoundError(vmName),
			NewVMNotRunningError(vmName),
			NewVMAutoStartError(vmName, "reason"),
			NewSSHConnectionRefusedError(vmName, "127.0.0.1", 22),
			NewSSHAuthError(vmName, "/path/to/key"),
			NewSSHTimeoutError(vmName, 30e9),
			NewTmuxNotInstalledError(vmName),
			NewVMStoppedNoStartError(vmName),
		}

		for _, err := range errors {
			assert.Contains(t, err.Message, vmName,
				"error code %s should contain VM name %q", err.Code, vmName)
			assert.Equal(t, vmName, err.VM)
		}
	})
}

// Property: Error() returns the message.
func TestConnectionError_ErrorReturnsMessage_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`[a-z][a-z0-9-]{2,16}`).Draw(t, "vmName")
		err := NewVMNotFoundError(vmName)
		assert.Equal(t, err.Message, err.Error())
	})
}

// Property: All error codes are snake_case.
func TestConnectionError_SnakeCaseCodes_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{4}`).Draw(t, "vmName")
		errors := []*ConnectionError{
			NewVMNotFoundError(vmName),
			NewVMNotRunningError(vmName),
			NewVMAutoStartError(vmName, "reason"),
			NewSSHConnectionRefusedError(vmName, "127.0.0.1", 22),
			NewSSHAuthError(vmName, "/key"),
			NewSSHTimeoutError(vmName, 30e9),
			NewTmuxNotInstalledError(vmName),
			NewPortInUseError(8080),
			NewSessionNameError("bad"),
			NewMutualExclusionError("a", "b"),
			NewVMStoppedNoStartError(vmName),
			NewWatchDirectionError(),
			NewRsyncNotFoundError(),
		}

		for _, err := range errors {
			for _, ch := range err.Code {
				assert.True(t, ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9'),
					"error code %q must be snake_case", err.Code)
			}
			assert.True(t, len(err.Code) > 0, "error code must not be empty")
		}
	})
}
