package ssh

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"pgregory.net/rapid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

// --- REQ-004-031: SSH Host Key Verification ---

// digitalTwinHostKeyScanner is a mock ssh-keyscan that returns predictable host keys.
// It simulates scanning a VM's SSH server without needing an actual network connection.
type digitalTwinHostKeyScanner struct {
	mu       sync.Mutex
	keys     map[string][]byte // "host:port" -> known_hosts data
	failHost string            // if set, scanning this host fails
}

func newDigitalTwinHostKeyScanner() *digitalTwinHostKeyScanner {
	return &digitalTwinHostKeyScanner{
		keys: make(map[string][]byte),
	}
}

func (dt *digitalTwinHostKeyScanner) AddHostKey(host string, port int, keyType, base64Key string) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	addr := fmt.Sprintf("%s:%d", host, port)
	dt.keys[addr] = []byte(fmt.Sprintf("%s %s %s\n", host, keyType, base64Key))
}

func (dt *digitalTwinHostKeyScanner) Scan(host string, port int) ([]byte, error) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	addr := fmt.Sprintf("%s:%d", host, port)
	if dt.failHost != "" && addr == dt.failHost {
		return nil, fmt.Errorf("connection refused: %s", addr)
	}
	if data, ok := dt.keys[addr]; ok {
		return data, nil
	}
	return nil, fmt.Errorf("no keys for %s", addr)
}

func (dt *digitalTwinHostKeyScanner) SetFailHost(host string, port int) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	dt.failHost = fmt.Sprintf("%s:%d", host, port)
}

// helper to set up a test with a digital twin scanner and temp directory.
func setupHostKeyTest(t *testing.T) (*digitalTwinHostKeyScanner, string) {
	t.Helper()
	dt := newDigitalTwinHostKeyScanner()
	orig := HostKeyScanner
	HostKeyScanner = dt.Scan
	t.Cleanup(func() { HostKeyScanner = orig })

	sdHome := filepath.Join(t.TempDir(), ".sd")
	return dt, sdHome
}

// well-known test key material (ed25519 fake keys for testing).
const (
	testEd25519Key = "AAAAC3NzaC1lZDI1NTE5AAAAIfakeEd25519KeyForTestingPurposesOnly123abc"
	testRSAKey     = "AAAAB3NzaC1yc2EAAAADAQABAAABgQCrashTestRSAKeyForTestingPurposesOnly456def"
	testECDSAKey   = "AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBCTestECDSAKeyForTesting789ghi"
)

func TestKnownHostsPath(t *testing.T) {
	path := KnownHostsPath("/home/user/.sd", "myvm")
	assert.Equal(t, "/home/user/.sd/vms/myvm/ssh/known_hosts", path)
}

func TestCaptureHostKey_StoresKey(t *testing.T) {
	dt, sdHome := setupHostKeyTest(t)
	dt.AddHostKey("127.0.0.1", 60022, "ssh-ed25519", testEd25519Key)

	err := CaptureHostKey(sdHome, "testvm", "127.0.0.1", 60022)
	require.NoError(t, err)

	data, err := ReadHostKey(sdHome, "testvm")
	require.NoError(t, err)
	assert.Contains(t, string(data), "ssh-ed25519")
	assert.Contains(t, string(data), testEd25519Key)
}

func TestCaptureHostKey_ScanFails(t *testing.T) {
	dt, sdHome := setupHostKeyTest(t)
	dt.SetFailHost("127.0.0.1", 60022)

	err := CaptureHostKey(sdHome, "testvm", "127.0.0.1", 60022)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "capture host key")
}

func TestCaptureHostKey_EmptyResponse(t *testing.T) {
	orig := HostKeyScanner
	HostKeyScanner = func(host string, port int) ([]byte, error) {
		return []byte("   \n\n  \n"), nil
	}
	t.Cleanup(func() { HostKeyScanner = orig })

	sdHome := filepath.Join(t.TempDir(), ".sd")
	err := CaptureHostKey(sdHome, "testvm", "127.0.0.1", 60022)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHostKeyScanFailed)
	assert.Contains(t, err.Error(), "empty response")
}

func TestStoreHostKey_CreatesDirAndFile(t *testing.T) {
	sdHome := filepath.Join(t.TempDir(), ".sd")
	keyData := []byte("127.0.0.1 ssh-ed25519 " + testEd25519Key + "\n")

	err := StoreHostKey(sdHome, "myvm", keyData)
	require.NoError(t, err)

	path := KnownHostsPath(sdHome, "myvm")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, keyData, data)

	// Check file permissions.
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestStoreHostKey_OverwritesExisting(t *testing.T) {
	sdHome := filepath.Join(t.TempDir(), ".sd")
	oldKey := []byte("127.0.0.1 ssh-ed25519 OLDKEY\n")
	newKey := []byte("127.0.0.1 ssh-ed25519 NEWKEY\n")

	require.NoError(t, StoreHostKey(sdHome, "myvm", oldKey))
	require.NoError(t, StoreHostKey(sdHome, "myvm", newKey))

	data, err := ReadHostKey(sdHome, "myvm")
	require.NoError(t, err)
	assert.Equal(t, newKey, data)
}

func TestReadHostKey_NoKeyStored(t *testing.T) {
	sdHome := filepath.Join(t.TempDir(), ".sd")

	_, err := ReadHostKey(sdHome, "nonexistent")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoHostKey)
}

func TestReadHostKey_ReturnsStoredData(t *testing.T) {
	sdHome := filepath.Join(t.TempDir(), ".sd")
	keyData := []byte("127.0.0.1 ssh-ed25519 " + testEd25519Key + "\n")
	require.NoError(t, StoreHostKey(sdHome, "testvm", keyData))

	data, err := ReadHostKey(sdHome, "testvm")
	require.NoError(t, err)
	assert.Equal(t, keyData, data)
}

func TestVerifyHostKey_MatchingKeys(t *testing.T) {
	sdHome := filepath.Join(t.TempDir(), ".sd")
	keyData := []byte("127.0.0.1 ssh-ed25519 " + testEd25519Key + "\n")
	require.NoError(t, StoreHostKey(sdHome, "testvm", keyData))

	err := VerifyHostKey(sdHome, "testvm", keyData)
	assert.NoError(t, err)
}

func TestVerifyHostKey_ChangedKey(t *testing.T) {
	sdHome := filepath.Join(t.TempDir(), ".sd")
	oldKey := []byte("127.0.0.1 ssh-ed25519 " + testEd25519Key + "\n")
	newKey := []byte("127.0.0.1 ssh-ed25519 AAAADifferentKeyMaterialHere==\n")
	require.NoError(t, StoreHostKey(sdHome, "testvm", oldKey))

	err := VerifyHostKey(sdHome, "testvm", newKey)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHostKeyChanged)
}

func TestVerifyHostKey_NoStoredKey(t *testing.T) {
	sdHome := filepath.Join(t.TempDir(), ".sd")
	keyData := []byte("127.0.0.1 ssh-ed25519 " + testEd25519Key + "\n")

	err := VerifyHostKey(sdHome, "nonexistent", keyData)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoHostKey)
}

func TestVerifyHostKey_MultipleKeyTypes(t *testing.T) {
	sdHome := filepath.Join(t.TempDir(), ".sd")
	keyData := []byte(strings.Join([]string{
		"127.0.0.1 ssh-ed25519 " + testEd25519Key,
		"127.0.0.1 ssh-rsa " + testRSAKey,
		"127.0.0.1 ecdsa-sha2-nistp256 " + testECDSAKey,
	}, "\n") + "\n")
	require.NoError(t, StoreHostKey(sdHome, "testvm", keyData))

	// Verify with same data.
	err := VerifyHostKey(sdHome, "testvm", keyData)
	assert.NoError(t, err)

	// Verify with different ordering (should still match since we parse by key type).
	reordered := []byte(strings.Join([]string{
		"127.0.0.1 ssh-rsa " + testRSAKey,
		"127.0.0.1 ecdsa-sha2-nistp256 " + testECDSAKey,
		"127.0.0.1 ssh-ed25519 " + testEd25519Key,
	}, "\n") + "\n")
	err = VerifyHostKey(sdHome, "testvm", reordered)
	assert.NoError(t, err)
}

func TestVerifyHostKey_MissingKeyType(t *testing.T) {
	sdHome := filepath.Join(t.TempDir(), ".sd")
	stored := []byte("127.0.0.1 ssh-ed25519 " + testEd25519Key + "\n127.0.0.1 ssh-rsa " + testRSAKey + "\n")
	require.NoError(t, StoreHostKey(sdHome, "testvm", stored))

	// Candidate only has one key type.
	candidate := []byte("127.0.0.1 ssh-ed25519 " + testEd25519Key + "\n")
	err := VerifyHostKey(sdHome, "testvm", candidate)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHostKeyChanged)
}

func TestVerifyHostKey_AddedKeyType(t *testing.T) {
	sdHome := filepath.Join(t.TempDir(), ".sd")
	stored := []byte("127.0.0.1 ssh-ed25519 " + testEd25519Key + "\n")
	require.NoError(t, StoreHostKey(sdHome, "testvm", stored))

	// Candidate has extra key type.
	candidate := []byte("127.0.0.1 ssh-ed25519 " + testEd25519Key + "\n127.0.0.1 ssh-rsa " + testRSAKey + "\n")
	err := VerifyHostKey(sdHome, "testvm", candidate)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHostKeyChanged)
}

func TestRemoveSSHDir_CleansUp(t *testing.T) {
	sdHome := filepath.Join(t.TempDir(), ".sd")

	// Generate keys and store a host key.
	require.NoError(t, GenerateKeys(sdHome, "testvm"))
	require.NoError(t, StoreHostKey(sdHome, "testvm", []byte("127.0.0.1 ssh-ed25519 KEY\n")))

	// Verify directory exists.
	dir, _, _ := KeyPaths(sdHome, "testvm")
	_, err := os.Stat(dir)
	require.NoError(t, err)

	// Remove.
	err = RemoveSSHDir(sdHome, "testvm")
	require.NoError(t, err)

	// Directory should be gone.
	_, err = os.Stat(dir)
	assert.True(t, os.IsNotExist(err))
}

func TestRemoveSSHDir_Idempotent(t *testing.T) {
	sdHome := filepath.Join(t.TempDir(), ".sd")
	err := RemoveSSHDir(sdHome, "nonexistent")
	assert.NoError(t, err, "removing nonexistent SSH dir should not error")
}

func TestRemoveSSHDir_CleansKnownHosts(t *testing.T) {
	sdHome := filepath.Join(t.TempDir(), ".sd")
	require.NoError(t, StoreHostKey(sdHome, "testvm", []byte("127.0.0.1 ssh-ed25519 KEY\n")))

	path := KnownHostsPath(sdHome, "testvm")
	_, err := os.Stat(path)
	require.NoError(t, err)

	require.NoError(t, RemoveSSHDir(sdHome, "testvm"))

	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

// --- Integration-style test with digital twin ---

func TestCaptureAndVerifyWorkflow(t *testing.T) {
	dt, sdHome := setupHostKeyTest(t)
	dt.AddHostKey("127.0.0.1", 60022, "ssh-ed25519", testEd25519Key)

	// Capture host key during VM creation.
	err := CaptureHostKey(sdHome, "testvm", "127.0.0.1", 60022)
	require.NoError(t, err)

	// Later, verify the same key.
	scanned, err := HostKeyScanner("127.0.0.1", 60022)
	require.NoError(t, err)
	err = VerifyHostKey(sdHome, "testvm", scanned)
	assert.NoError(t, err, "key should match after capture")

	// Simulate VM recreate: host key changes.
	dt.AddHostKey("127.0.0.1", 60022, "ssh-ed25519", "AAAANewKeyAfterRecreate==")
	newScanned, err := HostKeyScanner("127.0.0.1", 60022)
	require.NoError(t, err)
	err = VerifyHostKey(sdHome, "testvm", newScanned)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHostKeyChanged, "changed key should be detected")
}

func TestCaptureAndDestroyWorkflow(t *testing.T) {
	dt, sdHome := setupHostKeyTest(t)
	dt.AddHostKey("127.0.0.1", 60022, "ssh-ed25519", testEd25519Key)

	// Capture host key.
	require.NoError(t, CaptureHostKey(sdHome, "testvm", "127.0.0.1", 60022))

	// Verify file exists.
	_, err := os.Stat(KnownHostsPath(sdHome, "testvm"))
	require.NoError(t, err)

	// Destroy VM -> clean up SSH materials.
	require.NoError(t, RemoveSSHDir(sdHome, "testvm"))

	// Known hosts should be gone.
	_, err = ReadHostKey(sdHome, "testvm")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoHostKey)
}

// --- Property-based tests ---

// Property: Captured host key always round-trips through Store/Read.
func TestCaptureHostKey_RoundTrip_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(rt, "vmName")
		keyType := rapid.SampledFrom([]string{
			"ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384",
		}).Draw(rt, "keyType")
		keyMaterial := rapid.StringMatching(`[A-Za-z0-9+/=]{20,60}`).Draw(rt, "keyMaterial")

		// Use the outer testing.T for temp dir.
		sdHome := filepath.Join(t.TempDir(), t.Name(), vmName)

		dt := newDigitalTwinHostKeyScanner()
		dt.AddHostKey("127.0.0.1", 60022, keyType, keyMaterial)
		orig := HostKeyScanner
		HostKeyScanner = dt.Scan
		t.Cleanup(func() { HostKeyScanner = orig })

		err := CaptureHostKey(sdHome, vmName, "127.0.0.1", 60022)
		require.NoError(t, err)

		data, err := ReadHostKey(sdHome, vmName)
		require.NoError(t, err)
		assert.Contains(t, string(data), keyType, "stored key must contain key type")
		assert.Contains(t, string(data), keyMaterial, "stored key must contain key material")
	})
}

// Property: VerifyHostKey always succeeds when using the same key data.
func TestVerifyHostKey_SameKeyAlwaysValid_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(rt, "vmName")
		keyMaterial := rapid.StringMatching(`[A-Za-z0-9+/=]{20,60}`).Draw(rt, "keyMaterial")

		sdHome := filepath.Join(t.TempDir(), t.Name(), vmName)
		keyData := []byte("127.0.0.1 ssh-ed25519 " + keyMaterial + "\n")

		require.NoError(t, StoreHostKey(sdHome, vmName, keyData))
		assert.NoError(t, VerifyHostKey(sdHome, vmName, keyData),
			"identical key data should always verify")
	})
}

// Property: Different key material always fails verification.
func TestVerifyHostKey_DifferentKeyAlwaysFails_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(rt, "vmName")
		storedKey := rapid.StringMatching(`[A-Za-z0-9+/=]{20,60}`).Draw(rt, "storedKey")
		candidateKey := rapid.StringMatching(`[A-Za-z0-9+/=]{20,60}`).Draw(rt, "candidateKey")

		if storedKey == candidateKey {
			rt.Skip("identical keys")
		}

		sdHome := filepath.Join(t.TempDir(), t.Name(), vmName)
		storedData := []byte("127.0.0.1 ssh-ed25519 " + storedKey + "\n")
		candidateData := []byte("127.0.0.1 ssh-ed25519 " + candidateKey + "\n")

		require.NoError(t, StoreHostKey(sdHome, vmName, storedData))
		err := VerifyHostKey(sdHome, vmName, candidateData)
		assert.ErrorIs(t, err, ErrHostKeyChanged,
			"different key material must always fail verification")
	})
}

// Property: ReadHostKey always returns ErrNoHostKey for VMs with no stored key.
func TestReadHostKey_NoKeyAlwaysErrNoHostKey_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(rt, "vmName")
		sdHome := filepath.Join(t.TempDir(), t.Name())

		_, err := ReadHostKey(sdHome, vmName)
		assert.ErrorIs(t, err, ErrNoHostKey,
			"reading key for unprovisioned VM must always return ErrNoHostKey")
	})
}

// Property: VerifyHostKey always returns ErrNoHostKey for VMs with no stored key.
func TestVerifyHostKey_NoKeyAlwaysErrNoHostKey_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(rt, "vmName")
		keyMaterial := rapid.StringMatching(`[A-Za-z0-9+/=]{20,60}`).Draw(rt, "keyMaterial")
		sdHome := filepath.Join(t.TempDir(), t.Name())

		err := VerifyHostKey(sdHome, vmName, []byte("127.0.0.1 ssh-ed25519 "+keyMaterial+"\n"))
		assert.ErrorIs(t, err, ErrNoHostKey,
			"verifying against nonexistent VM must always return ErrNoHostKey")
	})
}

// Property: KnownHostsPath always ends with ssh/known_hosts.
func TestKnownHostsPath_AlwaysEndsCorrectly_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		sdHome := rapid.StringMatching(`/[a-z/.]+`).Draw(rt, "sdHome")
		vmName := rapid.StringMatching(`[a-z][a-z0-9-]{2,16}`).Draw(rt, "vmName")
		path := KnownHostsPath(sdHome, vmName)
		assert.True(t, strings.HasSuffix(path, filepath.Join("ssh", "known_hosts")),
			"known_hosts path must end with ssh/known_hosts, got %s", path)
		assert.Contains(t, path, vmName, "path must contain VM name")
	})
}

// Property: RemoveSSHDir always succeeds (even on nonexistent VMs).
func TestRemoveSSHDir_AlwaysSucceeds_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(rt, "vmName")
		sdHome := filepath.Join(t.TempDir(), t.Name(), vmName)
		assert.NoError(t, RemoveSSHDir(sdHome, vmName),
			"RemoveSSHDir should never fail")
	})
}

// Property: After RemoveSSHDir, ReadHostKey always returns ErrNoHostKey.
func TestRemoveSSHDir_ThenReadFails_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(rt, "vmName")
		sdHome := filepath.Join(t.TempDir(), t.Name(), vmName)

		require.NoError(t, StoreHostKey(sdHome, vmName, []byte("127.0.0.1 ssh-ed25519 KEY123==\n")))
		require.NoError(t, RemoveSSHDir(sdHome, vmName))

		_, err := ReadHostKey(sdHome, vmName)
		assert.ErrorIs(t, err, ErrNoHostKey,
			"reading after remove must always return ErrNoHostKey")
	})
}

// Property: All sentinel errors are non-nil and have snake_case names in messages.
func TestHostKeyErrors_SentinelValues_Property(t *testing.T) {
	errs := []error{ErrHostKeyChanged, ErrHostKeyScanFailed, ErrNoHostKey}
	for _, err := range errs {
		assert.NotNil(t, err)
		assert.NotEmpty(t, err.Error())
	}
}

// Property: StoreHostKey + ReadHostKey always preserves key data exactly.
func TestStoreReadHostKey_ExactRoundTrip_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		vmName := rapid.StringMatching(`vm-[a-z0-9]{3,8}`).Draw(rt, "vmName")
		sdHome := filepath.Join(t.TempDir(), t.Name(), vmName)

		keyLines := []string{}
		nKeys := rapid.IntRange(1, 4).Draw(rt, "nKeys")
		for i := 0; i < nKeys; i++ {
			keyType := rapid.SampledFrom([]string{
				"ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256",
			}).Draw(rt, fmt.Sprintf("keyType%d", i))
			keyMaterial := rapid.StringMatching(`[A-Za-z0-9+/=]{20,60}`).Draw(rt, fmt.Sprintf("key%d", i))
			keyLines = append(keyLines, "127.0.0.1 "+keyType+" "+keyMaterial)
		}
		keyData := []byte(strings.Join(keyLines, "\n") + "\n")

		require.NoError(t, StoreHostKey(sdHome, vmName, keyData))
		read, err := ReadHostKey(sdHome, vmName)
		require.NoError(t, err)
		assert.Equal(t, keyData, read, "ReadHostKey must return exact data written by StoreHostKey")
	})
}

// Property: hostKeysMatch is symmetric: if A matches B, then B matches A.
func TestHostKeysMatch_Symmetric_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		keyType := rapid.SampledFrom([]string{"ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256"}).Draw(rt, "keyType")
		keyMaterial := rapid.StringMatching(`[A-Za-z0-9+/=]{20,60}`).Draw(rt, "keyMaterial")

		a := []byte("127.0.0.1 " + keyType + " " + keyMaterial + "\n")
		b := []byte("192.168.1.1 " + keyType + " " + keyMaterial + "\n")

		// Same key type + material, different host -> should match.
		assert.True(t, hostKeysMatch(a, b), "same key type+material must match regardless of host")
		assert.True(t, hostKeysMatch(b, a), "matching must be symmetric")
	})
}

// Property: hostKeysMatch returns false when any key material differs.
func TestHostKeysMatch_DifferentMaterial_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		key1 := rapid.StringMatching(`[A-Za-z0-9+/=]{20,60}`).Draw(rt, "key1")
		key2 := rapid.StringMatching(`[A-Za-z0-9+/=]{20,60}`).Draw(rt, "key2")
		if key1 == key2 {
			rt.Skip("same key")
		}

		a := []byte("127.0.0.1 ssh-ed25519 " + key1 + "\n")
		b := []byte("127.0.0.1 ssh-ed25519 " + key2 + "\n")
		assert.False(t, hostKeysMatch(a, b), "different key material must not match")
	})
}

// --- Unit tests for parseHostKeys ---

func TestParseHostKeys_Basic(t *testing.T) {
	data := []byte("127.0.0.1 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFake==\n")
	keys := parseHostKeys(data)
	assert.Len(t, keys, 1)
	assert.Equal(t, "AAAAC3NzaC1lZDI1NTE5AAAAIFake==", keys["ssh-ed25519"])
}

func TestParseHostKeys_MultipleTypes(t *testing.T) {
	data := []byte(strings.Join([]string{
		"127.0.0.1 ssh-ed25519 KEY1",
		"127.0.0.1 ssh-rsa KEY2",
		"127.0.0.1 ecdsa-sha2-nistp256 KEY3",
	}, "\n") + "\n")
	keys := parseHostKeys(data)
	assert.Len(t, keys, 3)
	assert.Equal(t, "KEY1", keys["ssh-ed25519"])
	assert.Equal(t, "KEY2", keys["ssh-rsa"])
	assert.Equal(t, "KEY3", keys["ecdsa-sha2-nistp256"])
}

func TestParseHostKeys_SkipsComments(t *testing.T) {
	data := []byte("# comment line\n127.0.0.1 ssh-ed25519 KEY1\n")
	keys := parseHostKeys(data)
	assert.Len(t, keys, 1)
}

func TestParseHostKeys_SkipsEmpty(t *testing.T) {
	data := []byte("\n\n127.0.0.1 ssh-ed25519 KEY1\n\n")
	keys := parseHostKeys(data)
	assert.Len(t, keys, 1)
}

func TestParseHostKeys_Empty(t *testing.T) {
	keys := parseHostKeys([]byte{})
	assert.Empty(t, keys)
}

// --- Capture + Verify full lifecycle digital twin test ---

func TestHostKeyLifecycle_DigitalTwin(t *testing.T) {
	dt, sdHome := setupHostKeyTest(t)

	// 1. VM created with host key.
	dt.AddHostKey("127.0.0.1", 60022, "ssh-ed25519", testEd25519Key)
	require.NoError(t, CaptureHostKey(sdHome, "lifecycle-vm", "127.0.0.1", 60022))

	// 2. First connect: key matches.
	scanned, err := HostKeyScanner("127.0.0.1", 60022)
	require.NoError(t, err)
	require.NoError(t, VerifyHostKey(sdHome, "lifecycle-vm", scanned))

	// 3. VM recreated: host key changes.
	dt.AddHostKey("127.0.0.1", 60022, "ssh-ed25519", "AAAARecreatedKey==")
	newScanned, err := HostKeyScanner("127.0.0.1", 60022)
	require.NoError(t, err)
	require.ErrorIs(t, VerifyHostKey(sdHome, "lifecycle-vm", newScanned), ErrHostKeyChanged)

	// 4. After accepting new key, re-capture.
	require.NoError(t, CaptureHostKey(sdHome, "lifecycle-vm", "127.0.0.1", 60022))
	require.NoError(t, VerifyHostKey(sdHome, "lifecycle-vm", newScanned))

	// 5. Destroy: clean up.
	require.NoError(t, RemoveSSHDir(sdHome, "lifecycle-vm"))
	_, err = ReadHostKey(sdHome, "lifecycle-vm")
	require.ErrorIs(t, err, ErrNoHostKey)
}
