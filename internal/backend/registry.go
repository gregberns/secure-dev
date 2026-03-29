// Package backend provides the backend registry for runtime backend selection.
// REQ-003-013: Backend Registry
package backend

import (
	"fmt"
	"sort"
	"sync"
)

var (
	// mu protects concurrent access to the backends map.
	mu sync.RWMutex

	// backends holds the registered backends by name.
	backends = make(map[string]Backend)
)

// Register adds a backend to the registry.
// Panics if a backend with the same name is already registered.
// Called from backend init() functions.
// REQ-003-013
func Register(name string, b Backend) {
	mu.Lock()
	defer mu.Unlock()
	if _, exists := backends[name]; exists {
		panic(fmt.Sprintf("backend %q already registered", name))
	}
	backends[name] = b
}

// Get returns the backend registered under the given name.
// REQ-003-013
func Get(name string) (Backend, error) {
	mu.RLock()
	defer mu.RUnlock()
	b, ok := backends[name]
	if !ok {
		return nil, fmt.Errorf("backend %q not registered; available: %v: %w",
			name, List(), ErrBackendNotAvailable)
	}
	return b, nil
}

// List returns the names of all registered backends, sorted alphabetically.
// REQ-003-013
func List() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(backends))
	for name := range backends {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Default returns the first available backend, preferring "lima".
// REQ-003-013
func Default() (Backend, error) {
	mu.RLock()
	defer mu.RUnlock()

	// Prefer Lima if available.
	if b, ok := backends["lima"]; ok {
		if err := b.Available(); err == nil {
			return b, nil
		}
	}

	// Fall back to first available backend.
	for _, b := range backends {
		if err := b.Available(); err == nil {
			return b, nil
		}
	}

	return nil, fmt.Errorf("no available backends; registered: %v: %w",
		List(), ErrBackendNotAvailable)
}

// MustImplementSnapshotter returns an error if the backend doesn't implement Snapshotter.
// REQ-003-008: Callers must use type assertion to check for capability.
func MustImplementSnapshotter(b Backend) error {
	if _, ok := b.(Snapshotter); !ok {
		return fmt.Errorf("backend %q does not implement Snapshotter: %w", b.Name(), ErrNotImplemented)
	}
	return nil
}

// MustImplementCloner returns an error if the backend doesn't implement Cloner.
// REQ-003-009: Callers must use type assertion to check for capability.
func MustImplementCloner(b Backend) error {
	if _, ok := b.(Cloner); !ok {
		return fmt.Errorf("backend %q does not implement Cloner: %w", b.Name(), ErrNotImplemented)
	}
	return nil
}

// MustImplementSyncer returns an error if the backend doesn't implement Syncer.
// REQ-003-010: Callers must use type assertion to check for capability.
func MustImplementSyncer(b Backend) error {
	if _, ok := b.(Syncer); !ok {
		return fmt.Errorf("backend %q does not implement Syncer: %w", b.Name(), ErrNotImplemented)
	}
	return nil
}
