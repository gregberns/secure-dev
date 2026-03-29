// Package provision — dedicated property-based tests for resolver invariants.
// REQ-006-002: Base module always runs.
// REQ-006-004: Topological ordering based on depends_on.
// REQ-006-015: Module selection via CLI and config.
//
// This file contains deeper graph-theoretic invariant tests beyond those in
// resolver_test.go, focusing on minimality, input order independence,
// commutativity, deep/wide graph stress, and JSON round-trips.
package provision

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// ============================================================
// Minimality: ResolveRequested includes exactly the needed set
// ============================================================

// Property: ResolveRequested includes only the requested modules and their
// transitive dependencies — nothing extra.
func TestProperty_ResolveRequestedIsMinimal_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Build a DAG where "base" has no deps, and names[i] can depend on
		// any subset of names[0..i-1]. ResolveRequested always adds "base",
		// so we must include it in the module set.
		n := rapid.IntRange(3, 12).Draw(t, "n")
		seen := map[string]bool{"base": true}
		names := []string{"base"}
		for len(names) < n {
			name := rapid.StringMatching(`^[a-z][a-z0-9]{1,6}`).Draw(t, "name")
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}

		// "base" has no deps.
		deps := make(map[string][]string)
		deps["base"] = nil
		for i := 1; i < n; i++ {
			var depList []string
			for j := 0; j < i; j++ {
				if rapid.Bool().Draw(t, "edge") {
					depList = append(depList, names[j])
				}
			}
			deps[names[i]] = depList
		}

		modules := makeModules(deps)

		// Pick a random subset of non-base modules to request.
		// ResolveRequested always adds base automatically.
		var requested []string
		for i := 1; i < n; i++ {
			if rapid.Bool().Draw(t, "req_"+names[i]) {
				requested = append(requested, names[i])
			}
		}
		if len(requested) == 0 {
			requested = []string{names[1]}
		}

		result, err := ResolveRequested(modules, requested)
		require.NoError(t, err)

		// Compute the expected set: requested + base + transitive deps.
		byName := make(map[string]*Module, len(modules))
		for i := range modules {
			byName[modules[i].Name] = &modules[i]
		}
		expected := make(map[string]bool)
		// Always includes base
		expected["base"] = true
		var collectTransitive func(name string)
		collectTransitive = func(name string) {
			if expected[name] {
				return
			}
			expected[name] = true
			mod := byName[name]
			if mod != nil {
				for _, dep := range mod.DependsOn {
					collectTransitive(dep)
				}
			}
		}
		for _, r := range requested {
			collectTransitive(r)
		}

		// Verify result contains exactly the expected set.
		resultSet := make(map[string]bool)
		for _, m := range result {
			resultSet[m.Name] = true
		}

		assert.Equal(t, len(expected), len(resultSet),
			"result has %d modules but expected %d", len(resultSet), len(expected))
		for name := range expected {
			assert.True(t, resultSet[name], "expected module %q in result", name)
		}
		for name := range resultSet {
			assert.True(t, expected[name], "unexpected module %q in result", name)
		}
	})
}

// ============================================================
// Input order independence
// ============================================================

// Property: The same dependency graph expressed with modules in different
// slice orders produces identical results.
func TestProperty_ResolveInputOrderIndependent_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Create a fixed DAG.
		deps := map[string][]string{
			"base":   {},
			"alpha":  {"base"},
			"beta":   {"base"},
			"gamma":  {"alpha", "beta"},
			"delta":  {"base"},
			"epsilon": {"gamma", "delta"},
		}

		modules := makeModules(deps)
		allNames := []string{"base", "alpha", "beta", "gamma", "delta", "epsilon"}

		// Resolve with original order.
		r1, err1 := Resolve(modules, allNames)
		require.NoError(t, err1)

		// Shuffle modules and resolve again.
		shuffled := make([]Module, len(modules))
		copy(shuffled, modules)
		for i := len(shuffled) - 1; i > 0; i-- {
			j := rapid.IntRange(0, i).Draw(t, "shuffle")
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		}

		r2, err2 := Resolve(shuffled, allNames)
		require.NoError(t, err2)

		names1 := make([]string, len(r1))
		names2 := make([]string, len(r2))
		for i, m := range r1 {
			names1[i] = m.Name
		}
		for i, m := range r2 {
			names2[i] = m.Name
		}
		assert.Equal(t, names1, names2, "module input order should not affect result")
	})
}

// ============================================================
// Union commutativity: Resolve(A,B) = union(Resolve(A), Resolve(B))
// ============================================================

// Property: Resolving two modules together produces the union of resolving
// each separately (assuming no cycles between the two sets).
func TestProperty_ResolveUnionCommutativity_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		deps := map[string][]string{
			"base":   {},
			"alpha":  {"base"},
			"beta":   {"base"},
			"gamma":  {"alpha"},
			"delta":  {"beta"},
		}
		modules := makeModules(deps)

		// Resolve each pair of non-base modules separately and together.
		pairs := [][2]string{{"alpha", "beta"}, {"gamma", "delta"}, {"alpha", "delta"}}
		pair := rapid.SampledFrom(pairs).Draw(t, "pair")

		rBoth, err := Resolve(modules, []string{pair[0], pair[1]})
		require.NoError(t, err)

		rA, err := Resolve(modules, []string{pair[0]})
		require.NoError(t, err)

		rB, err := Resolve(modules, []string{pair[1]})
		require.NoError(t, err)

		// Compute union of names from individual resolves.
		unionSet := make(map[string]bool)
		for _, m := range rA {
			unionSet[m.Name] = true
		}
		for _, m := range rB {
			unionSet[m.Name] = true
		}

		bothSet := make(map[string]bool)
		for _, m := range rBoth {
			bothSet[m.Name] = true
		}

		assert.Equal(t, unionSet, bothSet,
			"Resolve(%s,%s) should equal union of individual resolves", pair[0], pair[1])
	})
}

// ============================================================
// Deep chain invariant
// ============================================================

// Property: A linear chain of depth N resolves to exactly N modules in
// strict dependency order.
func TestProperty_ResolveDeepChain_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		depth := rapid.IntRange(2, 30).Draw(t, "depth")

		// Build a chain: m0 <- m1 <- m2 <- ... <- m{depth-1}
		deps := make(map[string][]string)
		names := make([]string, depth)
		for i := 0; i < depth; i++ {
			names[i] = "m" + itoa(i)
			if i == 0 {
				deps[names[i]] = nil
			} else {
				deps[names[i]] = []string{names[i-1]}
			}
		}

		modules := makeModules(deps)
		result, err := Resolve(modules, []string{names[depth-1]})
		require.NoError(t, err)

		assert.Len(t, result, depth, "chain of depth %d should produce %d modules", depth, depth)

		// Verify strict order: m0, m1, m2, ...
		for i, m := range result {
			assert.Equal(t, names[i], m.Name,
				"chain module at position %d should be %s, got %s", i, names[i], m.Name)
		}
	})
}

// ============================================================
// Wide fan-out invariant
// ============================================================

// Property: A root with N direct children resolves to exactly N+1 modules.
func TestProperty_ResolveWideFanOut_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		width := rapid.IntRange(2, 25).Draw(t, "width")

		deps := map[string][]string{
			"root": {},
		}
		var children []string
		for i := 0; i < width; i++ {
			name := "child" + itoa(i)
			deps[name] = []string{"root"}
			children = append(children, name)
		}

		modules := makeModules(deps)
		result, err := Resolve(modules, children)
		require.NoError(t, err)

		assert.Len(t, result, width+1,
			"root with %d children should produce %d modules", width, width+1)

		// Root must be first.
		assert.Equal(t, "root", result[0].Name)

		// All children must be present after root.
		resultNames := make(map[string]bool)
		for _, m := range result {
			resultNames[m.Name] = true
		}
		for _, c := range children {
			assert.True(t, resultNames[c], "child %q must be in result", c)
		}
	})
}

// ============================================================
// ResolveAll includes all modules
// ============================================================

// Property: ResolveAll returns every module in the input.
func TestProperty_ResolveAllIncludesEverything_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(2, 15).Draw(t, "n")
		seen := make(map[string]bool)
		names := make([]string, 0, n)
		for len(names) < n {
			name := rapid.StringMatching(`^[a-z][a-z0-9]{1,6}`).Draw(t, "name")
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}

		// Build DAG: index-based ordering.
		deps := make(map[string][]string)
		for i, name := range names {
			var depList []string
			for j := 0; j < i; j++ {
				if rapid.Bool().Draw(t, "edge") {
					depList = append(depList, names[j])
				}
			}
			deps[name] = depList
		}

		modules := makeModules(deps)
		result, err := ResolveAll(modules)
		require.NoError(t, err)

		assert.Len(t, result, n, "ResolveAll must return all %d modules", n)

		resultSet := make(map[string]bool)
		for _, m := range result {
			resultSet[m.Name] = true
		}
		for _, name := range names {
			assert.True(t, resultSet[name], "ResolveAll must include %q", name)
		}
	})
}

// ============================================================
// Disconnected components
// ============================================================

// Property: Multiple independent subgraphs (sharing only base) resolve
// correctly and the result is the union of each subgraph.
func TestProperty_ResolveDisconnectedComponents_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numComponents := rapid.IntRange(2, 5).Draw(t, "numComponents")
		nodesPerComponent := rapid.IntRange(1, 4).Draw(t, "nodesPerComponent")

		deps := map[string][]string{
			"base": {},
		}

		var allRequested []string
		for c := 0; c < numComponents; c++ {
			// Each component: c{i}root depends on base, c{i}leaf depends on c{i}root.
			rootName := "c" + itoa(c) + "root"
			deps[rootName] = []string{"base"}
			allRequested = append(allRequested, rootName)

			for n := 1; n < nodesPerComponent; n++ {
				leafName := "c" + itoa(c) + "n" + itoa(n)
				deps[leafName] = []string{rootName}
				allRequested = append(allRequested, leafName)
				rootName = leafName // Chain within component.
			}
		}

		modules := makeModules(deps)
		result, err := Resolve(modules, allRequested)
		require.NoError(t, err)

		// Base must be first.
		assert.Equal(t, "base", result[0].Name)

		// Total = 1 (base) + numComponents * nodesPerComponent.
		expected := 1 + numComponents*nodesPerComponent
		assert.Len(t, result, expected,
			"expected %d modules (1 base + %d components * %d nodes)", expected, numComponents, nodesPerComponent)

		// Verify ordering: within each component, deps come before dependents.
		position := make(map[string]int)
		for i, m := range result {
			position[m.Name] = i
		}
		for _, m := range result {
			for _, dep := range m.DependsOn {
				assert.Less(t, position[dep], position[m.Name],
					"dep %q must come before %q", dep, m.Name)
			}
		}
	})
}

// ============================================================
// Stability under subsets
// ============================================================

// Property: Resolving a subset of modules produces a result that is a
// subsequence of resolving the full set (with consistent ordering).
func TestProperty_ResolveSubsetConsistency_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		deps := map[string][]string{
			"base":   {},
			"alpha":  {"base"},
			"beta":   {"base"},
			"gamma":  {"alpha"},
			"delta":  {"beta"},
			"epsilon": {"gamma", "delta"},
		}
		modules := makeModules(deps)
		allNames := []string{"base", "alpha", "beta", "gamma", "delta", "epsilon"}

		// Resolve all.
		allResult, err := Resolve(modules, allNames)
		require.NoError(t, err)

		// Resolve a subset.
		subsetResult, err := Resolve(modules, []string{"gamma"})
		require.NoError(t, err)

		// Every module in the subset result must maintain its relative order
		// from the full result.
		allPos := make(map[string]int)
		for i, m := range allResult {
			allPos[m.Name] = i
		}

		for i := 1; i < len(subsetResult); i++ {
			prev := subsetResult[i-1].Name
			cur := subsetResult[i].Name
			assert.Less(t, allPos[prev], allPos[cur],
				"relative order from full resolve must be preserved: %q (pos %d) before %q (pos %d)",
				prev, allPos[prev], cur, allPos[cur])
		}
	})
}

// ============================================================
// Base module invariants for ResolveRequested
// ============================================================

// Property: Base module always appears at position 0 in ResolveRequested results.
func TestProperty_ResolveRequestedBaseAlwaysFirst_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(2, 10).Draw(t, "n")
		seen := make(map[string]bool)
		names := make([]string, 0, n)
		// First name is always "base".
		names = append(names, "base")
		seen["base"] = true
		for len(names) < n {
			name := rapid.StringMatching(`^[a-z][a-z0-9]{1,6}`).Draw(t, "name")
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}

		deps := make(map[string][]string)
		deps["base"] = nil
		for i := 1; i < n; i++ {
			var depList []string
			for j := 0; j < i; j++ {
				if rapid.Bool().Draw(t, "edge") {
					depList = append(depList, names[j])
				}
			}
			// Ensure every non-base module depends on base (common pattern).
			hasBase := false
			for _, d := range depList {
				if d == "base" {
					hasBase = true
					break
				}
			}
			if !hasBase {
				depList = append(depList, "base")
			}
			deps[names[i]] = depList
		}

		modules := makeModules(deps)

		// Pick a random non-base module to request.
		req := []string{names[rapid.IntRange(1, n-1).Draw(t, "pick")]}

		result, err := ResolveRequested(modules, req)
		require.NoError(t, err)

		require.NotEmpty(t, result, "result must not be empty")
		assert.Equal(t, "base", result[0].Name,
			"base module must always be at position 0 in ResolveRequested result")
	})
}

// ============================================================
// JSON round-trip invariants
// ============================================================

// Property: Module survives JSON marshal/unmarshal round-trip.
func TestProperty_ModuleJSONRoundTrip_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`).Draw(t, "name")
		desc := rapid.StringMatching(`^[A-Za-z0-9 ]{1,50}$`).Draw(t, "description")
		mode := rapid.SampledFrom([]ScriptMode{ModeSystem, ModeUser}).Draw(t, "mode")
		script := rapid.StringMatching(`[a-z]{1,20}`).Draw(t, "script")
		hasProbe := rapid.Bool().Draw(t, "has_probe")
		numDeps := rapid.IntRange(0, 3).Draw(t, "num_deps")
		hasChecksums := rapid.Bool().Draw(t, "has_checksums")

		m := &Module{
			Name:        name,
			Description: desc,
			Scripts:     []Script{{Mode: mode, Script: script}},
		}

		for i := 0; i < numDeps; i++ {
			m.DependsOn = append(m.DependsOn, "dep-"+itoa(i))
		}

		if hasProbe {
			m.Probe = &Probe{
				Command:  "echo test",
				Interval: 5,
				Timeout:  30,
			}
		}

		if hasChecksums {
			m.Checksums = map[string]string{"file.tar.gz": "abc123"}
		}

		data, err := json.Marshal(m)
		require.NoError(t, err)

		var decoded Module
		require.NoError(t, json.Unmarshal(data, &decoded))

		assert.Equal(t, m.Name, decoded.Name)
		assert.Equal(t, m.Description, decoded.Description)
		require.Len(t, decoded.Scripts, 1)
		assert.Equal(t, m.Scripts[0].Mode, decoded.Scripts[0].Mode)
		assert.Equal(t, m.Scripts[0].Script, decoded.Scripts[0].Script)

		if hasProbe {
			require.NotNil(t, decoded.Probe)
			assert.Equal(t, m.Probe.Command, decoded.Probe.Command)
		} else {
			assert.Nil(t, decoded.Probe)
		}

		if hasChecksums {
			assert.Equal(t, m.Checksums, decoded.Checksums)
		}
	})
}

// Property: ModuleExecutionStatus survives JSON round-trip.
func TestProperty_ModuleExecutionStatusJSONRoundTrip_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`^[a-z]{2,10}`).Draw(t, "name")
		status := rapid.SampledFrom([]ModuleStatus{
			StatusPending, StatusRunning, StatusCompleted, StatusFailed,
		}).Draw(t, "status")
		hasError := rapid.Bool().Draw(t, "has_error")

		s := ModuleExecutionStatus{
			Name:   name,
			Status: status,
		}
		if hasError {
			s.Error = "something went wrong"
		}

		data, err := json.Marshal(s)
		require.NoError(t, err)

		var decoded ModuleExecutionStatus
		require.NoError(t, json.Unmarshal(data, &decoded))

		assert.Equal(t, s.Name, decoded.Name)
		assert.Equal(t, s.Status, decoded.Status)
		assert.Equal(t, s.Error, decoded.Error)
	})
}

// Property: ProvisionState survives JSON round-trip.
func TestProperty_ProvisionStateJSONRoundTrip_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vmName := rapid.StringMatching(`^[a-z][a-z0-9-]{2,16}`).Draw(t, "vmName")
		numModules := rapid.IntRange(1, 5).Draw(t, "num_modules")

		state := ProvisionState{
			VMName: vmName,
			Modules: make([]ModuleExecutionStatus, numModules),
		}
		for i := 0; i < numModules; i++ {
			state.Modules[i] = ModuleExecutionStatus{
				Name:   "mod-" + itoa(i),
				Status: StatusCompleted,
			}
		}

		data, err := json.Marshal(state)
		require.NoError(t, err)

		var decoded ProvisionState
		require.NoError(t, json.Unmarshal(data, &decoded))

		assert.Equal(t, state.VMName, decoded.VMName)
		require.Len(t, decoded.Modules, numModules)
		for i, m := range decoded.Modules {
			assert.Equal(t, state.Modules[i].Name, m.Name)
			assert.Equal(t, state.Modules[i].Status, m.Status)
		}
	})
}

// ============================================================
// Topological sort validity with duplicate dependencies
// ============================================================

// Property: Modules with duplicate entries in depends_on still resolve correctly.
func TestProperty_ResolveDuplicateDeps_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		deps := map[string][]string{
			"base": {},
			"tool": {"base", "base"}, // duplicate dep
		}
		modules := makeModules(deps)

		result, err := Resolve(modules, []string{"tool"})
		require.NoError(t, err)

		// Should produce exactly 2 modules: base, tool.
		assert.Len(t, result, 2)
		assert.Equal(t, "base", result[0].Name)
		assert.Equal(t, "tool", result[1].Name)
	})
}

// ============================================================
// Result name set is always sorted among equal-priority nodes
// ============================================================

// Property: Among modules with the same dependency depth, the result is
// always in lexicographic order (determinism guarantee).
func TestProperty_ResolveLexicographicAmongEqualPriority_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Create modules that all depend only on base.
		n := rapid.IntRange(2, 15).Draw(t, "n")
		seen := make(map[string]bool)
		names := make([]string, 0, n)
		for len(names) < n {
			name := rapid.StringMatching(`^[a-z][a-z0-9]{1,6}`).Draw(t, "name")
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}

		deps := map[string][]string{
			"base": {},
		}
		for _, name := range names {
			deps[name] = []string{"base"}
		}

		modules := makeModules(deps)
		result, err := Resolve(modules, names)
		require.NoError(t, err)

		// After base, modules should be in lexicographic order.
		sorted := make([]string, len(names))
		copy(sorted, names)
		sort.Strings(sorted)

		for i, expected := range sorted {
			actualIdx := i + 1 // +1 because base is at position 0
			if actualIdx < len(result) {
				assert.Equal(t, expected, result[actualIdx].Name,
					"module at position %d should be %q (lexicographic order)", actualIdx, expected)
			}
		}
	})
}

// ============================================================
// ResolveAll determinism across multiple calls
// ============================================================

// Property: ResolveAll produces the same result every time, even with
// different map iteration orders (Go maps are unordered).
func TestProperty_ResolveAllDeterministic_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(3, 10).Draw(t, "n")
		seen := make(map[string]bool)
		names := make([]string, 0, n)
		for len(names) < n {
			name := rapid.StringMatching(`^[a-z][a-z0-9]{1,6}`).Draw(t, "name")
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}

		deps := make(map[string][]string)
		for i, name := range names {
			var depList []string
			for j := 0; j < i; j++ {
				if rapid.Bool().Draw(t, "edge") {
					depList = append(depList, names[j])
				}
			}
			deps[name] = depList
		}

		modules := makeModules(deps)

		var orders [][]string
		for run := 0; run < 5; run++ {
			result, err := ResolveAll(modules)
			require.NoError(t, err)
			var order []string
			for _, m := range result {
				order = append(order, m.Name)
			}
			orders = append(orders, order)
		}

		for i := 1; i < len(orders); i++ {
			assert.Equal(t, orders[0], orders[i],
				"ResolveAll must be deterministic across calls")
		}
	})
}

// itoa converts a non-negative int to its decimal string representation.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
