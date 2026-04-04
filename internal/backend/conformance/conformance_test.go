package conformance_test

import (
	"testing"
	"time"

	"sd/internal/backend/conformance"
	"sd/internal/backend/memory"
)

// TestMemoryBackendConformance runs the full conformance suite against
// the in-memory backend. This validates that both the conformance suite
// itself and the memory backend satisfy the Backend interface contract.
// REQ-008-022
func TestMemoryBackendConformance(t *testing.T) {
	mb := memory.New()
	conformance.RunAll(t, conformance.ConformanceOpts{
		Backend: mb,
		Timeout: 5 * time.Second,
		Setup:   func(t *testing.T) { mb.Reset() },
	})
}
