// Property-based tests for backend types and invariants.
// REQ-003-011: VMConfig validation
// REQ-003-021: Error semantics
// REQ-003-004: VMStatus values
// REQ-003-013: Backend registry
package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================
// REQ-003-011: VMConfig Validation — Property-Based Tests
// ============================================================

// Property: Any config with positive CPUs, non-empty memory/disk/image passes.
func TestProperty_ValidVMConfigAlwaysPasses(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cpus := rapid.IntRange(1, 128).Draw(t, "cpus")
		memory := rapid.SampledFrom([]string{
			"1GiB", "2GiB", "4GiB", "8GiB", "16GiB", "32GiB", "64GiB", "128GiB",
		}).Draw(t, "memory")
		disk := rapid.SampledFrom([]string{
			"10GiB", "50GiB", "100GiB", "200GiB", "500GiB", "1TiB",
		}).Draw(t, "disk")
		image := rapid.SampledFrom([]string{
			"ubuntu:24.04", "ubuntu:22.04", "debian:12", "debian:11",
			"fedora:40", "alpine:3.19",
		}).Draw(t, "image")

		cfg := VMConfig{
			CPUs:      cpus,
			Memory:    memory,
			Disk:      disk,
			BaseImage: image,
		}

		err := cfg.Validate()
		assert.NoError(t, err,
			"config with cpus=%d memory=%s disk=%s image=%s should be valid",
			cpus, memory, disk, image)
	})
}

// Property: Any config with CPUs <= 0 always fails.
func TestProperty_ZeroOrNegativeCPUsAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cpus := rapid.IntRange(-1000, 0).Draw(t, "cpus")
		cfg := VMConfig{
			CPUs:      cpus,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		err := cfg.Validate()
		require.Error(t, err, "cpus=%d should fail validation", cpus)
		assert.True(t, errors.Is(err, ErrInvalidConfig),
			"error should wrap ErrInvalidConfig, got: %v", err)
		assert.Contains(t, err.Error(), "cpus",
			"error message should mention cpus")
	})
}

// Property: Any config missing a required string field always fails.
func TestProperty_MissingRequiredFieldAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		field := rapid.SampledFrom([]string{"memory", "disk", "base_image"}).Draw(t, "field")

		cfg := VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}

		switch field {
		case "memory":
			cfg.Memory = ""
		case "disk":
			cfg.Disk = ""
		case "base_image":
			cfg.BaseImage = ""
		}

		err := cfg.Validate()
		require.Error(t, err, "blanking %s should fail validation", field)
		assert.True(t, errors.Is(err, ErrInvalidConfig))
	})
}

// Property: Any valid network mode passes.
func TestProperty_ValidNetworkModesAlwaysPass(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mode := rapid.SampledFrom([]NetworkMode{
			NetworkNAT, NetworkBridged, NetworkIsolated, "",
		}).Draw(t, "mode")

		cfg := VMConfig{
			CPUs:        4,
			Memory:      "8GiB",
			Disk:        "100GiB",
			BaseImage:   "ubuntu:24.04",
			NetworkMode: mode,
		}

		err := cfg.Validate()
		assert.NoError(t, err, "network mode %q should be valid", mode)
	})
}

// Property: Any invalid network mode always fails.
func TestProperty_InvalidNetworkModeAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mode := rapid.SampledFrom([]string{
			"invalid", "open", "closed", "public", "private",
			"transparent", "host", "bridge", "NAT", "NAT ", " nat",
		}).Draw(t, "invalid_mode")

		cfg := VMConfig{
			CPUs:        4,
			Memory:      "8GiB",
			Disk:        "100GiB",
			BaseImage:   "ubuntu:24.04",
			NetworkMode: NetworkMode(mode),
		}

		err := cfg.Validate()
		require.Error(t, err, "network mode %q should fail", mode)
		assert.True(t, errors.Is(err, ErrInvalidConfig))
		assert.Contains(t, err.Error(), "network_mode")
	})
}

// Property: Any mount with non-empty host and guest paths passes.
func TestProperty_ValidMountsAlwaysPass(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hostPath := rapid.StringMatching(`/[a-z][a-z0-9/_-]{2,30}`).Draw(t, "hostPath")
		guestPath := rapid.StringMatching(`/[a-z][a-z0-9/_-]{2,30}`).Draw(t, "guestPath")
		writable := rapid.Bool().Draw(t, "writable")

		cfg := VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
			Mounts: []Mount{
				{HostPath: hostPath, GuestPath: guestPath, Writable: writable},
			},
		}

		err := cfg.Validate()
		assert.NoError(t, err,
			"mount with host=%s guest=%s writable=%v should be valid",
			hostPath, guestPath, writable)
	})
}

// Property: Any mount with empty host or guest path always fails.
func TestProperty_EmptyMountPathsAlwaysFail(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		blankField := rapid.SampledFrom([]string{"host", "guest"}).Draw(t, "blankField")
		otherPath := "/valid/path"

		m := Mount{}
		switch blankField {
		case "host":
			m.HostPath = ""
			m.GuestPath = otherPath
		case "guest":
			m.HostPath = otherPath
			m.GuestPath = ""
		}

		cfg := VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
			Mounts:    []Mount{m},
		}

		err := cfg.Validate()
		require.Error(t, err, "mount with empty %s path should fail", blankField)
		assert.True(t, errors.Is(err, ErrInvalidConfig))
	})
}

// Property: Valid resource size formats always pass for Memory and Disk.
func TestProperty_ValidResourceSizeFormatsAlwaysPass(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		size := rapid.SampledFrom([]string{
			"1KiB", "512MiB", "4GiB", "8GiB", "16GiB", "100GiB", "1TiB",
			"1K", "512M", "4G", "8G", "100G", "1T",
			"1KB", "512MB", "4GB", "8GB", "100GB", "1TB",
		}).Draw(t, "size")

		cfg := VMConfig{
			CPUs:      4,
			Memory:    size,
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}
		assert.NoError(t, cfg.Validate(), "memory=%q should be valid", size)

		cfg.Memory = "8GiB"
		cfg.Disk = size
		assert.NoError(t, cfg.Validate(), "disk=%q should be valid", size)
	})
}

// Property: Invalid resource size formats always fail for Memory and Disk.
func TestProperty_InvalidResourceSizeFormatsAlwaysFail(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		invalid := rapid.SampledFrom([]string{
			"abc", "4gb", "8gib", "100", "-4GiB", "4 GiB",
			"GiB", "4gib", "8g", "4.5GiB", "4 GiB ",
			"4GiB ", " 4GiB", "4gIB", "4GIB", "4gib",
		}).Draw(t, "invalid")

		// Memory
		cfg := VMConfig{
			CPUs:      4,
			Memory:    invalid,
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}
		err := cfg.Validate()
		require.Error(t, err, "memory=%q should fail", invalid)
		assert.True(t, errors.Is(err, ErrInvalidConfig))
		assert.Contains(t, err.Error(), "memory")

		// Disk
		cfg.Memory = "8GiB"
		cfg.Disk = invalid
		err = cfg.Validate()
		require.Error(t, err, "disk=%q should fail", invalid)
		assert.True(t, errors.Is(err, ErrInvalidConfig))
		assert.Contains(t, err.Error(), "disk")
	})
}

// Property: Random valid resource sizes generated from components always pass.
func TestProperty_RandomValidResourceSizesPass(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		amount := rapid.IntRange(1, 9999).Draw(t, "amount")
		unit := rapid.SampledFrom([]string{
			"K", "M", "G", "T",
			"KiB", "MiB", "GiB", "TiB",
			"KB", "MB", "GB", "TB",
		}).Draw(t, "unit")
		size := fmt.Sprintf("%d%s", amount, unit)

		cfg := VMConfig{
			CPUs:      4,
			Memory:    size,
			Disk:      size,
			BaseImage: "ubuntu:24.04",
		}
		assert.NoError(t, cfg.Validate(), "size=%q should be valid", size)
	})
}

// Property: Error message for bad format always includes the value and format hint.
func TestProperty_ResourceSizeErrorIncludesValueAndHint(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		badValue := rapid.StringMatching(`[a-z]{2,10}`).Draw(t, "badValue")

		cfg := VMConfig{
			CPUs:      4,
			Memory:    badValue,
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
		}
		err := cfg.Validate()
		require.Error(t, err)
		errMsg := err.Error()
		assert.Contains(t, errMsg, badValue, "error should include the bad value")
		assert.Contains(t, strings.ToLower(errMsg), "format",
			"error should mention format")
	})
}

// Property: Multiple valid mounts always pass.
func TestProperty_MultipleValidMountsAlwaysPass(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 5).Draw(t, "n_mounts")
		mounts := make([]Mount, n)
		for i := 0; i < n; i++ {
			mounts[i] = Mount{
				HostPath:  fmt.Sprintf("/host/path/%d", i),
				GuestPath: fmt.Sprintf("/guest/path/%d", i),
				Writable:  i%2 == 0,
			}
		}

		cfg := VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
			Mounts:    mounts,
		}

		assert.NoError(t, cfg.Validate())
	})
}

// Property: EnvVars do not affect validation.
func TestProperty_EnvVarsDoNotAffectValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cfg := VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
			EnvVars: map[string]string{
				"PROJECT": "test",
				"TOKEN":   "secret",
			},
		}

		assert.NoError(t, cfg.Validate())
	})
}

// Property: BackendOptions do not affect validation.
func TestProperty_BackendOptionsDoNotAffectValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cfg := VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
			BackendOptions: map[string]any{
				"vmType":    "vz",
				"mountType": "virtiofs",
			},
		}

		assert.NoError(t, cfg.Validate())
	})
}

// Property: Validation error always wraps ErrInvalidConfig.
func TestProperty_ValidationErrorsAlwaysWrapErrInvalidConfig(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cfg := VMConfig{} // All fields zero/empty

		err := cfg.Validate()
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrInvalidConfig),
			"validation error must wrap ErrInvalidConfig")
	})
}

// Property: Validation error message always mentions the problematic field.
func TestProperty_ValidationErrorAlwaysActionable(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		field := rapid.SampledFrom([]string{"cpus", "memory", "disk", "base_image", "network_mode"}).Draw(t, "field")

		cfg := VMConfig{
			CPUs:        4,
			Memory:      "8GiB",
			Disk:        "100GiB",
			BaseImage:   "ubuntu:24.04",
			NetworkMode: NetworkNAT,
		}

		switch field {
		case "cpus":
			cfg.CPUs = 0
		case "memory":
			cfg.Memory = ""
		case "disk":
			cfg.Disk = ""
		case "base_image":
			cfg.BaseImage = ""
		case "network_mode":
			cfg.NetworkMode = "garbage"
		}

		err := cfg.Validate()
		require.Error(t, err)
		errMsg := strings.ToLower(err.Error())
		assert.Contains(t, errMsg, field,
			"validation error should mention field %q: %s", field, err.Error())
	})
}

// Property: Mount Writable field does not affect validation outcome.
func TestProperty_MountWritableDoesNotAffectValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		writable := rapid.Bool().Draw(t, "writable")

		cfg := VMConfig{
			CPUs:      4,
			Memory:    "8GiB",
			Disk:      "100GiB",
			BaseImage: "ubuntu:24.04",
			Mounts: []Mount{
				{HostPath: "/project", GuestPath: "/workspace", Writable: writable},
			},
		}

		assert.NoError(t, cfg.Validate())
	})
}

// Property: ProvisionScripts do not affect validation.
func TestProperty_ProvisionScriptsDoNotAffectValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		nScripts := rapid.IntRange(0, 5).Draw(t, "nScripts")
		scripts := make([]ProvisionScript, nScripts)
		for i := 0; i < nScripts; i++ {
			scripts[i] = ProvisionScript{
				Mode:   rapid.SampledFrom([]string{"system", "user"}).Draw(t, "mode"),
				Script: "echo hello",
			}
		}

		cfg := VMConfig{
			CPUs:             4,
			Memory:           "8GiB",
			Disk:             "100GiB",
			BaseImage:        "ubuntu:24.04",
			ProvisionScripts: scripts,
		}

		assert.NoError(t, cfg.Validate())
	})
}

// ============================================================
// REQ-003-004: VMStatus / VMInfo JSON — Property-Based Tests
// ============================================================

// Property: All VMStatus values round-trip through JSON.
func TestProperty_VMStatusRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		status := rapid.SampledFrom([]VMStatus{
			StatusCreating, StatusRunning, StatusStopped, StatusError,
		}).Draw(t, "status")

		data, err := json.Marshal(status)
		require.NoError(t, err)

		var decoded VMStatus
		require.NoError(t, json.Unmarshal(data, &decoded))
		assert.Equal(t, status, decoded)
	})
}

// Property: VMInfo round-trips through JSON.
func TestProperty_VMInfoRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		info := VMInfo{
			Name:      rapid.StringMatching(`vm-[a-z0-9]{2,8}`).Draw(t, "name"),
			Status:    rapid.SampledFrom([]VMStatus{StatusCreating, StatusRunning, StatusStopped, StatusError}).Draw(t, "status"),
			Backend:   "lima",
			CPUs:      rapid.IntRange(1, 32).Draw(t, "cpus"),
			Memory:    "8GiB",
			Disk:      "100GiB",
			CreatedAt: func() *time.Time { t := time.Now(); return &t }(),
		}

		data, err := json.Marshal(info)
		require.NoError(t, err)

		var decoded VMInfo
		require.NoError(t, json.Unmarshal(data, &decoded))
		assert.Equal(t, info.Name, decoded.Name)
		assert.Equal(t, info.Status, decoded.Status)
		assert.Equal(t, info.Backend, decoded.Backend)
		assert.Equal(t, info.CPUs, decoded.CPUs)
		assert.Equal(t, info.Memory, decoded.Memory)
		assert.Equal(t, info.Disk, decoded.Disk)
	})
}

// Property: SnapshotInfo round-trips through JSON.
func TestProperty_SnapshotInfoRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		snap := SnapshotInfo{
			Name:      rapid.StringMatching(`snap-[a-z0-9]{2,8}`).Draw(t, "name"),
			CreatedAt: time.Now(),
			Size:      int64(rapid.Int64Range(0, 1e12).Draw(t, "size")),
		}

		data, err := json.Marshal(snap)
		require.NoError(t, err)

		var decoded SnapshotInfo
		require.NoError(t, json.Unmarshal(data, &decoded))
		assert.Equal(t, snap.Name, decoded.Name)
		assert.Equal(t, snap.Size, decoded.Size)
	})
}

// Property: SSHConfig round-trips through JSON.
func TestProperty_SSHConfigRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cfg := SSHConfig{
			Host:         "127.0.0.1",
			Port:         rapid.IntRange(1024, 65535).Draw(t, "port"),
			User:         "dev",
			IdentityFile: "/home/user/.sd/vms/test/ssh/id_ed25519",
			ForwardAgent: false,
		}

		data, err := json.Marshal(cfg)
		require.NoError(t, err)

		var decoded SSHConfig
		require.NoError(t, json.Unmarshal(data, &decoded))
		assert.Equal(t, cfg.Host, decoded.Host)
		assert.Equal(t, cfg.Port, decoded.Port)
		assert.Equal(t, cfg.User, decoded.User)
		assert.Equal(t, cfg.IdentityFile, decoded.IdentityFile)
		assert.Equal(t, cfg.ForwardAgent, decoded.ForwardAgent)
	})
}

// ============================================================
// REQ-003-021: Error Semantics — Property-Based Tests
// ============================================================

// Property: Sentinel errors are distinct.
func TestProperty_SentinelErrorsAreDistinct(t *testing.T) {
	sentinels := []error{
		ErrVMNotFound, ErrVMAlreadyExists, ErrVMNotRunning,
		ErrBackendNotAvailable, ErrNotImplemented, ErrSnapshotNotFound,
		ErrInvalidConfig,
	}

	rapid.Check(t, func(t *rapid.T) {
		i := rapid.IntRange(0, len(sentinels)-1).Draw(t, "i")
		j := rapid.IntRange(0, len(sentinels)-1).Draw(t, "j")

		if i != j {
			assert.False(t, errors.Is(sentinels[i], sentinels[j]))
		} else {
			assert.True(t, errors.Is(sentinels[i], sentinels[j]))
		}
	})
}

// Property: wrapError produces errors that unwrap correctly.
func TestProperty_WrapErrorUnwrapsCorrectly(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		base := ErrVMNotFound
		vmName := rapid.StringMatching(`vm-[a-z0-9]{2,8}`).Draw(t, "vmName")
		msg := fmt.Sprintf("VM %q not found", vmName)

		wrapped := wrapError(base, msg)
		assert.True(t, errors.Is(wrapped, ErrVMNotFound))
		assert.Contains(t, wrapped.Error(), msg)
		assert.Contains(t, wrapped.Error(), base.Error())
	})
}

// Property: wrapError(nil, ...) always returns nil.
func TestProperty_WrapErrorNilBaseReturnsNil(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		msg := rapid.StringMatching(`[a-z ]{5,50}`).Draw(t, "msg")
		assert.Nil(t, wrapError(nil, msg))
	})
}

// Property: wrapError(base, "") returns base unchanged.
func TestProperty_WrapErrorEmptyMessageReturnsBase(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		base := ErrInvalidConfig
		result := wrapError(base, "")
		assert.Equal(t, base, result)
	})
}

// ============================================================
// REQ-003-013: Backend Registry — Property-Based Tests
// ============================================================

// Property: Register + Get always round-trips.
func TestProperty_RegisterGetRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ResetRegistry()
		name := rapid.StringMatching(`backend-[a-z0-9]{2,8}`).Draw(t, "name")
		b := &mockBackend{name: name}

		Register(name, b)
		got, err := Get(name)

		require.NoError(t, err)
		assert.Equal(t, b, got)
	})
}

// Property: List is always sorted.
func TestProperty_ListAlwaysSorted(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ResetRegistry()

		names := rapid.SliceOfN(
			rapid.StringMatching(`b-[a-z]{2,4}`),
			3, 8,
		).Draw(t, "names")

		// Deduplicate
		seen := make(map[string]bool)
		for _, n := range names {
			if !seen[n] {
				seen[n] = true
				Register(n, &mockBackend{name: n})
			}
		}

		list := List()
		for i := 1; i < len(list); i++ {
			assert.True(t, list[i-1] <= list[i],
				"List() not sorted: %v", list)
		}
	})
}

// Property: Get of unknown name always returns ErrBackendNotAvailable.
func TestProperty_GetUnknownAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ResetRegistry()

		name := rapid.StringMatching(`unknown-[a-z0-9]{4,12}`).Draw(t, "name")
		_, err := Get(name)

		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrBackendNotAvailable))
	})
}

// Property: Concurrent register + List is safe.
func TestProperty_ConcurrentRegistryAccess(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ResetRegistry()

		n := rapid.IntRange(5, 20).Draw(t, "n")

		var wg sync.WaitGroup

		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				name := fmt.Sprintf("concurrent-%d", idx)
				Register(name, &mockBackend{name: name})
			}(i)
		}
		wg.Wait()

		// Concurrent reads
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				List()
			}()
		}
		wg.Wait()

		// Verify all registered
		list := List()
		assert.Len(t, list, n)
	})
}

// Property: Default prefers lima when available.
func TestProperty_DefaultPrefersLima(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ResetRegistry()

		lima := &mockBackend{name: "lima", available: true}
		Register("lima", lima)

		// Add other available backends
		nExtra := rapid.IntRange(0, 3).Draw(t, "n_extra")
		for i := 0; i < nExtra; i++ {
			name := fmt.Sprintf("other-%d", i)
			Register(name, &mockBackend{name: name, available: true})
		}

		got, err := Default()
		require.NoError(t, err)
		assert.Equal(t, lima, got, "Default must prefer lima")
	})
}

// Property: Default fails when no backend is available.
func TestProperty_DefaultFailsWhenNoneAvailable(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ResetRegistry()

		n := rapid.IntRange(1, 5).Draw(t, "n")
		for i := 0; i < n; i++ {
			name := fmt.Sprintf("unavail-%d", i)
			Register(name, &mockBackend{name: name, available: false})
		}

		_, err := Default()
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrBackendNotAvailable))
	})
}

// Property: Capability detection is always correct.
func TestProperty_CapabilityDetectionCorrect(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		full := &mockBackend{}
		assert.NoError(t, MustImplementSnapshotter(full))
		assert.NoError(t, MustImplementCloner(full))
		assert.NoError(t, MustImplementSyncer(full))

		assert.Error(t, MustImplementSnapshotter(&mockBackendNoSnapshotter{}))
		assert.Error(t, MustImplementCloner(&mockBackendNoCloner{}))
		assert.Error(t, MustImplementSyncer(&mockBackendNoSyncer{}))

		err := MustImplementSnapshotter(&mockBackendNoSnapshotter{})
		assert.True(t, errors.Is(err, ErrNotImplemented))
	})
}
