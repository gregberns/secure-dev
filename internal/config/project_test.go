// Package config provides tests for .sd.yaml project configuration.
// REQ-005-020, REQ-005-021, REQ-005-023
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestFindProjectConfig_CurrentDir(t *testing.T) {
	dir := t.TempDir()
	sdYaml := filepath.Join(dir, ".sd.yaml")
	err := os.WriteFile(sdYaml, []byte("name: my-project\n"), 0644)
	require.NoError(t, err)

	path, cfg, err := FindProjectConfig(dir)
	require.NoError(t, err)
	assert.Equal(t, sdYaml, path)
	require.NotNil(t, cfg)
	assert.Equal(t, "my-project", cfg.Name)
}

func TestFindProjectConfig_ParentDir(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "subdir")
	require.NoError(t, os.MkdirAll(child, 0755))

	sdYaml := filepath.Join(parent, ".sd.yaml")
	err := os.WriteFile(sdYaml, []byte("name: parent-project\n"), 0644)
	require.NoError(t, err)

	path, cfg, err := FindProjectConfig(child)
	require.NoError(t, err)
	assert.Equal(t, sdYaml, path)
	require.NotNil(t, cfg)
	assert.Equal(t, "parent-project", cfg.Name)
}

func TestFindProjectConfig_StopsAtHomeDir(t *testing.T) {
	// Create a temp dir that simulates home
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Put .sd.yaml in the fake home -- it should NOT be found
	// because search stops AT home (doesn't walk above it)
	// But a .sd.yaml IN home should still be found if we start there
	child := filepath.Join(fakeHome, "projects", "myapp")
	require.NoError(t, os.MkdirAll(child, 0755))

	// Put .sd.yaml above fakeHome's projects (but inside home) -- at the home level
	homeSD := filepath.Join(fakeHome, ".sd.yaml")
	err := os.WriteFile(homeSD, []byte("name: home-project\n"), 0644)
	require.NoError(t, err)

	// Starting from child, it should find .sd.yaml at home level
	path, cfg, err := FindProjectConfig(child)
	require.NoError(t, err)
	assert.Equal(t, homeSD, path)
	require.NotNil(t, cfg)
	assert.Equal(t, "home-project", cfg.Name)
}

func TestFindProjectConfig_StopsAtHomeBoundary(t *testing.T) {
	// Create a structure where .sd.yaml is ABOVE the home directory
	// The search should stop at home and not find it
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Put .sd.yaml in the parent of fakeHome
	parentOfHome := filepath.Dir(fakeHome)
	parentSD := filepath.Join(parentOfHome, ".sd.yaml")
	// Only write if we can (might fail on some systems, skip test if so)
	if err := os.WriteFile(parentSD, []byte("name: should-not-find\n"), 0644); err != nil {
		t.Skip("cannot write above temp dir")
	}
	t.Cleanup(func() { os.Remove(parentSD) })

	// Start search from inside home
	searchDir := filepath.Join(fakeHome, "work")
	require.NoError(t, os.MkdirAll(searchDir, 0755))

	path, cfg, err := FindProjectConfig(searchDir)
	require.NoError(t, err)
	assert.Empty(t, path, "should not find .sd.yaml above home")
	assert.Nil(t, cfg)
}

func TestFindProjectConfig_NotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// No .sd.yaml anywhere
	path, cfg, err := FindProjectConfig(dir)
	require.NoError(t, err)
	assert.Empty(t, path)
	assert.Nil(t, cfg)
}

func TestFindProjectConfig_DerivesNameWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "My-Cool-Project")
	require.NoError(t, os.MkdirAll(projectDir, 0755))

	sdYaml := filepath.Join(projectDir, ".sd.yaml")
	err := os.WriteFile(sdYaml, []byte("cpus: 4\n"), 0644)
	require.NoError(t, err)

	_, cfg, err := FindProjectConfig(projectDir)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "my-cool-project", cfg.Name, "name should be derived from directory")
}

func TestLoadProjectConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	content := `name: test-vm
backend: lima
cpus: 8
memory: 16GiB
disk: 200GiB
modules:
  - base
  - golang
mounts:
  - .:/home/ubuntu/project:rw
allow_egress:
  - pkg.go.dev
`
	path := filepath.Join(dir, ".sd.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	cfg, err := LoadProjectConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "test-vm", cfg.Name)
	assert.Equal(t, "lima", cfg.Backend)
	assert.Equal(t, 8, cfg.CPUs)
	assert.Equal(t, "16GiB", cfg.Memory)
	assert.Equal(t, "200GiB", cfg.Disk)
	assert.Equal(t, []string{"base", "golang"}, cfg.Modules)
	assert.Equal(t, []string{".:/home/ubuntu/project:rw"}, cfg.Mounts)
	assert.Equal(t, []string{"pkg.go.dev"}, cfg.AllowEgress)
}

func TestLoadProjectConfig_InvalidName(t *testing.T) {
	dir := t.TempDir()
	content := "name: INVALID_NAME!\n"
	path := filepath.Join(dir, ".sd.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	_, err := LoadProjectConfig(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid name")
}

func TestLoadProjectConfig_InvalidMemory(t *testing.T) {
	dir := t.TempDir()
	content := "memory: not-a-size\n"
	path := filepath.Join(dir, ".sd.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	_, err := LoadProjectConfig(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid memory")
}

func TestLoadProjectConfig_InvalidDisk(t *testing.T) {
	dir := t.TempDir()
	content := "disk: abc\n"
	path := filepath.Join(dir, ".sd.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	_, err := LoadProjectConfig(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid disk")
}

func TestLoadProjectConfig_InvalidMount(t *testing.T) {
	dir := t.TempDir()
	content := "mounts:\n  - \"badmount\"\n"
	path := filepath.Join(dir, ".sd.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	_, err := LoadProjectConfig(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be host:guest")
}

func TestLoadProjectConfig_InvalidMountMode(t *testing.T) {
	dir := t.TempDir()
	content := "mounts:\n  - \"./src:/guest:xx\"\n"
	path := filepath.Join(dir, ".sd.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	_, err := LoadProjectConfig(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mode must be")
}

func TestLoadProjectConfig_EmptyFields(t *testing.T) {
	dir := t.TempDir()
	content := "# empty config\n"
	path := filepath.Join(dir, ".sd.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	cfg, err := LoadProjectConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "", cfg.Name)
	assert.Equal(t, 0, cfg.CPUs)
	assert.Equal(t, "", cfg.Memory)
	assert.Nil(t, cfg.Modules)
}

func TestLoadProjectConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	content := ": bad yaml [["
	path := filepath.Join(dir, ".sd.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	_, err := LoadProjectConfig(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

func TestLoadProjectConfig_NegativeCPUs(t *testing.T) {
	dir := t.TempDir()
	content := "cpus: -1\n"
	path := filepath.Join(dir, ".sd.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	_, err := LoadProjectConfig(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cpus must be positive")
}

// --- DeriveVMName tests ---

func TestDeriveVMName(t *testing.T) {
	tests := []struct {
		dir      string
		expected string
	}{
		{"/home/user/my-project", "my-project"},
		{"/home/user/My-Project_v2", "my-project-v2"},
		{"/home/user/UPPERCASE", "uppercase"},
		{"/home/user/has spaces here", "has-spaces-here"},
		{"/home/user/special!@#chars", "special-chars"},
		{"/home/user/123numeric", "vm-123numeric"},
		{"/home/user/-leading-hyphen", "leading-hyphen"},
		{"/home/user/a", "a"},
		{"/home/user/go-project", "go-project"},
		{"/home/user/My___App", "my-app"},
	}

	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			result := DeriveVMName(tt.dir)
			assert.Equal(t, tt.expected, result)
			// Every derived name must be a valid VM name
			assert.Regexp(t, `^[a-z][a-z0-9-]{0,62}$`, result,
				"derived name %q must match VM name pattern", result)
		})
	}
}

// --- Round-trip test ---

func TestProjectConfig_RoundTrip(t *testing.T) {
	original := ProjectConfig{
		Name:        "round-trip",
		Backend:     "lima",
		CPUs:        4,
		Memory:      "8GiB",
		Disk:        "100GiB",
		Modules:     []string{"base", "golang"},
		Mounts:      []string{".:/home/ubuntu/project:rw"},
		AllowEgress: []string{"pkg.go.dev", "proxy.golang.org"},
	}

	data, err := yaml.Marshal(&original)
	require.NoError(t, err)

	var restored ProjectConfig
	err = yaml.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, original, restored)
}

// --- ResolveMountPaths tests ---

func TestResolveMountPaths(t *testing.T) {
	projectDir := "/home/user/my-project"

	tests := []struct {
		name     string
		mounts   []string
		expected []string
	}{
		{
			name:     "dot resolves to project dir",
			mounts:   []string{".:/guest:rw"},
			expected: []string{"/home/user/my-project:/guest:rw"},
		},
		{
			name:     "relative path resolves to project dir",
			mounts:   []string{"src:/guest:ro"},
			expected: []string{"/home/user/my-project/src:/guest:ro"},
		},
		{
			name:     "absolute path left unchanged",
			mounts:   []string{"/abs/path:/guest:rw"},
			expected: []string{"/abs/path:/guest:rw"},
		},
		{
			name:     "multiple mounts",
			mounts:   []string{".:/guest1:rw", "/abs:/guest2:ro"},
			expected: []string{"/home/user/my-project:/guest1:rw", "/abs:/guest2:ro"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveMountPaths(tt.mounts, projectDir)
			assert.Equal(t, tt.expected, result)
		})
	}
}
