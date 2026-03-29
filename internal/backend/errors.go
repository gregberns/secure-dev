// Package backend defines sentinel errors for backend operations.
// REQ-003-021
package backend

import "errors"

// Sentinel errors that MUST be used across all backend implementations.
// REQ-003-021
var (
	// ErrVMNotFound is returned when a named VM does not exist.
	ErrVMNotFound = errors.New("vm not found")

	// ErrVMAlreadyExists is returned when creating a VM with a name that is already in use.
	ErrVMAlreadyExists = errors.New("vm already exists")

	// ErrVMNotRunning is returned when an operation requires a running VM but it is not running.
	ErrVMNotRunning = errors.New("vm not running")

	// ErrBackendNotAvailable is returned when a backend's prerequisites are not met.
	ErrBackendNotAvailable = errors.New("backend not available")

	// ErrNotImplemented is returned by stub backends for unimplemented operations.
	ErrNotImplemented = errors.New("not implemented")

	// ErrSnapshotNotFound is returned when a named snapshot does not exist.
	ErrSnapshotNotFound = errors.New("snapshot not found")

	// ErrInvalidConfig is returned when VMConfig fails validation.
	ErrInvalidConfig = errors.New("invalid vm config")
)
