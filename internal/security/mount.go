package security

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// sensitivePathEntry is a resolved sensitive path with its original pattern and category.
type sensitivePathEntry struct {
	expanded string // Fully expanded absolute path
	pattern  string // Original pattern (e.g., "~/.ssh")
	category string // Category (e.g., "ssh")
	isHome   bool   // True for $HOME/~ entries
}

// sensitivePathEntries is the pre-resolved list of sensitive path entries.
// REQ-004-005: Mount Path Validation
var sensitivePathEntries []sensitivePathEntry

func init() {
	categories := map[string][]string{
		"home": {
			"$HOME",
			"~",
		},
		"ssh": {
			"~/.ssh",
		},
		"aws": {
			"~/.aws",
		},
		"config": {
			"~/.config",
		},
		"gnupg": {
			"~/.gnupg",
		},
		"kube": {
			"~/.kube",
		},
		"docker_dir": {
			"~/.docker",
		},
		"docker_socket": {
			"/var/run/docker.sock",
		},
		"browser": {
			"~/Library/Application Support/Google/Chrome",
			"~/Library/Application Support/Firefox",
			"~/.mozilla",
			"~/.config/google-chrome",
			"~/.config/chromium",
		},
	}

	for cat, patterns := range categories {
		for _, p := range patterns {
			expanded := expandHome(p)
			isHome := cat == "home"
			sensitivePathEntries = append(sensitivePathEntries, sensitivePathEntry{
				expanded: filepath.Clean(expanded),
				pattern:  p,
				category: cat,
				isHome:   isHome,
			})
		}
	}
}

// DefaultSensitivePaths returns the expanded list of all built-in sensitive paths.
// REQ-004-005
func DefaultSensitivePaths() []string {
	var paths []string
	for _, e := range sensitivePathEntries {
		paths = append(paths, e.expanded)
	}
	return paths
}

// ValidateMountPath checks whether a host path is safe to mount.
// Returns nil if allowed, or a MountValidationError describing why rejected.
// REQ-004-005: Rejects sensitive host directories.
// Validation resolves symlinks before checking.
func ValidateMountPath(hostPath string, mode MountMode, extraSensitivePaths []string) error {
	cleaned := filepath.Clean(hostPath)

	// Resolve symlinks
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		resolved = cleaned
	}

	// Resolve built-in sensitive paths once
	type resolvedEntry struct {
		category string
		resolved string
		isHome   bool
	}
	resolvedEntries := make([]resolvedEntry, len(sensitivePathEntries))
	for i, entry := range sensitivePathEntries {
		resolvedEntries[i] = resolvedEntry{
			category: entry.category,
			resolved: resolvePath(entry.expanded),
			isHome:   entry.isHome,
		}
	}

	// Pass 1: Check for exact matches first (most specific).
	// This ensures $HOME matches as "home", not as a parent of browser paths.
	for _, re := range resolvedEntries {
		if resolved == re.resolved {
			return &MountValidationError{
				Path:     hostPath,
				Category: re.category,
				Reason: fmt.Sprintf(
					"mount path %q is a sensitive directory and cannot be mounted. "+
						"Sensitive directories include: $HOME, ~/.ssh, ~/.aws, ~/.config, "+
						"~/.gnupg, ~/.kube, ~/.docker, browser profiles, and the Docker socket. "+
						"See \"sd help security\" for details.",
					hostPath,
				),
			}
		}
	}

	// Pass 2: Check for parent/child relationships.
	for _, re := range resolvedEntries {
		if pathConflicts(resolved, re.resolved, re.isHome) {
			return &MountValidationError{
				Path:     hostPath,
				Category: re.category,
				Reason: fmt.Sprintf(
					"mount path %q is a sensitive directory and cannot be mounted. "+
						"Sensitive directories include: $HOME, ~/.ssh, ~/.aws, ~/.config, "+
						"~/.gnupg, ~/.kube, ~/.docker, browser profiles, and the Docker socket. "+
						"See \"sd help security\" for details.",
					hostPath,
				),
			}
		}
	}

	// Check user-configured extra sensitive paths
	for _, sp := range extraSensitivePaths {
		spCleaned := filepath.Clean(expandHome(sp))
		spResolved := resolvePath(spCleaned)
		// For user-configured paths, apply the same logic as non-home entries
		if pathConflicts(resolved, spResolved, false) {
			return &MountValidationError{
				Path:     hostPath,
				Category: "user_configured",
				Reason: fmt.Sprintf(
					"mount path %q is a user-configured sensitive directory (%q). "+
						"See \"sd help security\" for details.",
					hostPath, sp,
				),
			}
		}
	}

	// Check for .env files at the mount root
	// REQ-004-005: Any path containing .env files at the mount root
	base := filepath.Base(resolved)
	if strings.HasPrefix(base, ".env") {
		return &MountValidationError{
			Path:     hostPath,
			Category: "env_file",
			Reason: fmt.Sprintf(
				"mount path %q appears to contain .env files at the mount root. "+
					"These may contain secrets. See \"sd help security\" for details.",
				hostPath,
			),
		}
	}

	return nil
}

// pathConflicts returns true if the target path conflicts with a sensitive path.
//
// For home directory entries (isHome=true):
//   - Reject exact match (mounting $HOME itself)
//   - Reject if target is a parent of sensitivePath (mounting a wider dir that contains home)
//   - Allow children of home (~/projects/my-repo is fine)
//
// For all other entries:
//   - Reject exact match
//   - Reject if target is a parent of sensitivePath
//   - Reject if target is inside sensitivePath (children)
func pathConflicts(target, sensitivePath string, isHome bool) bool {
	// Direct match
	if target == sensitivePath {
		return true
	}

	// Target is a parent of sensitivePath (target contains the sensitive dir)
	// e.g., mounting / when /var/run/docker.sock is sensitive
	if isParent(target, sensitivePath) {
		return true
	}

	// Target is inside sensitivePath (target is a child of sensitive dir)
	// e.g., mounting ~/.ssh/known_hosts
	// Skip this check for $HOME: ~/projects/my-repo is fine
	if !isHome && isParent(sensitivePath, target) {
		return true
	}

	return false
}

// isParent returns true if parent is a parent directory of child.
func isParent(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	// If child is inside parent, rel won't start with ".." and won't be "."
	return !strings.HasPrefix(rel, "..") && rel != "."
}

// resolvePath cleans a path and resolves symlinks if possible.
// Falls back to the cleaned path if the target doesn't exist.
func resolvePath(path string) string {
	cleaned := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return cleaned
	}
	return resolved
}

// expandHome expands ~ and $HOME in a path.
func expandHome(path string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return path
	}

	switch {
	case path == "~":
		return homeDir
	case strings.HasPrefix(path, "~/"):
		return filepath.Join(homeDir, path[2:])
	case path == "$HOME":
		return homeDir
	case strings.HasPrefix(path, "$HOME/"):
		return filepath.Join(homeDir, path[6:])
	}

	// Expand platform-specific paths
	if runtime.GOOS == "darwin" {
		path = strings.ReplaceAll(path, "~/Library/", homeDir+"/Library/")
	}

	return path
}
