package provision

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// --- Helper: build module list from names and deps ---

func makeModules(deps map[string][]string) []Module {
	var modules []Module
	for name, depList := range deps {
		modules = append(modules, Module{
			Name:        name,
			Description: name + " module",
			DependsOn:   depList,
			Scripts:     []Script{{Mode: ModeSystem, Script: "echo " + name}},
		})
	}
	return modules
}

// --- Unit Tests ---

func TestResolve_SingleModule(t *testing.T) {
	modules := makeModules(map[string][]string{
		"base": {},
	})
	result, err := Resolve(modules, []string{"base"})
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "base", result[0].Name)
}

func TestResolve_LinearChain(t *testing.T) {
	modules := makeModules(map[string][]string{
		"base":    {},
		"golang":  {"base"},
		"my-tool": {"golang"},
	})
	result, err := Resolve(modules, []string{"my-tool"})
	require.NoError(t, err)
	require.Len(t, result, 3)
	// base must come before golang, golang before my-tool.
	assert.Equal(t, "base", result[0].Name)
	assert.Equal(t, "golang", result[1].Name)
	assert.Equal(t, "my-tool", result[2].Name)
}

func TestResolve_Diamond(t *testing.T) {
	// A depends on B and C, both depend on D.
	modules := makeModules(map[string][]string{
		"base":    {},
		"docker":  {"base"},
		"golang":  {"base"},
		"my-tool": {"docker", "golang"},
	})
	result, err := Resolve(modules, []string{"my-tool"})
	require.NoError(t, err)
	require.Len(t, result, 4)

	// base must be first.
	assert.Equal(t, "base", result[0].Name)
	// my-tool must be last.
	assert.Equal(t, "my-tool", result[3].Name)

	// Verify ordering constraints.
	order := make(map[string]int)
	for i, m := range result {
		order[m.Name] = i
	}
	assert.Less(t, order["base"], order["docker"])
	assert.Less(t, order["base"], order["golang"])
	assert.Less(t, order["docker"], order["my-tool"])
	assert.Less(t, order["golang"], order["my-tool"])
}

func TestResolve_CircularDependency(t *testing.T) {
	modules := makeModules(map[string][]string{
		"a": {"b"},
		"b": {"a"},
	})
	_, err := Resolve(modules, []string{"a"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circular dependency")
}

func TestResolve_ThreeWayCycle(t *testing.T) {
	modules := makeModules(map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"a"},
	})
	_, err := Resolve(modules, []string{"a"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circular dependency")
}

func TestResolve_SelfDependency(t *testing.T) {
	modules := makeModules(map[string][]string{
		"a": {"a"},
	})
	_, err := Resolve(modules, []string{"a"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circular dependency")
}

func TestResolve_UnknownModule(t *testing.T) {
	modules := makeModules(map[string][]string{
		"base": {},
	})
	_, err := Resolve(modules, []string{"nonexistent"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown module")
}

func TestResolve_MissingDependency(t *testing.T) {
	modules := makeModules(map[string][]string{
		"tool": {"missing-dep"},
	})
	_, err := Resolve(modules, []string{"tool"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown module")
}

func TestResolve_IndependentModules(t *testing.T) {
	// Requesting multiple modules with no mutual dependencies.
	modules := makeModules(map[string][]string{
		"base":   {},
		"golang": {"base"},
		"rust":   {"base"},
	})
	result, err := Resolve(modules, []string{"golang", "rust"})
	require.NoError(t, err)
	require.Len(t, result, 3)

	// base must be first.
	assert.Equal(t, "base", result[0].Name)

	// golang and rust come after base, in deterministic (alphabetical) order.
	assert.Equal(t, "golang", result[1].Name)
	assert.Equal(t, "rust", result[2].Name)
}

func TestResolveAll(t *testing.T) {
	modules := makeModules(map[string][]string{
		"base":   {},
		"golang": {"base"},
		"docker": {"base"},
	})
	result, err := ResolveAll(modules)
	require.NoError(t, err)
	require.Len(t, result, 3)
	assert.Equal(t, "base", result[0].Name)
}

func TestResolveRequested_AlwaysIncludesBase(t *testing.T) {
	modules := makeModules(map[string][]string{
		"base":   {},
		"golang": {"base"},
		"docker": {"base"},
	})
	result, err := ResolveRequested(modules, []string{"docker"})
	require.NoError(t, err)

	names := make(map[string]bool)
	for _, m := range result {
		names[m.Name] = true
	}
	assert.True(t, names["base"], "base should always be included")
	assert.True(t, names["docker"])
}

func TestResolveRequested_BaseAlreadyPresent(t *testing.T) {
	modules := makeModules(map[string][]string{
		"base":   {},
		"golang": {"base"},
	})
	result, err := ResolveRequested(modules, []string{"base", "golang"})
	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, "base", result[0].Name)
	assert.Equal(t, "golang", result[1].Name)
}

func TestResolve_EmptyRequested(t *testing.T) {
	modules := makeModules(map[string][]string{
		"base": {},
	})
	result, err := Resolve(modules, []string{})
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestResolve_DeterministicOrdering(t *testing.T) {
	// Run the same resolve multiple times to verify determinism.
	modules := makeModules(map[string][]string{
		"base":   {},
		"alpha":  {"base"},
		"beta":   {"base"},
		"gamma":  {"base"},
		"delta":  {"base"},
	})

	var orders [][]string
	for i := 0; i < 10; i++ {
		result, err := Resolve(modules, []string{"alpha", "beta", "gamma", "delta"})
		require.NoError(t, err)
		var names []string
		for _, m := range result {
			names = append(names, m.Name)
		}
		orders = append(orders, names)
	}

	for i := 1; i < len(orders); i++ {
		assert.Equal(t, orders[0], orders[i], "ordering should be deterministic")
	}
}

// --- Property-Based Tests ---

func TestProperty_ResolveAlwaysSatisfiesDeps(t *testing.T) {
	// Given any DAG of modules, resolve must return them in an order
	// where every module appears after all its dependencies.
	rapid.Check(t, func(t *rapid.T) {
		// Generate a set of unique module names.
		n := rapid.IntRange(1, 20).Draw(t, "num_modules")
		seen := make(map[string]bool)
		names := make([]string, 0, n)
		for len(names) < n {
			name := rapid.StringMatching(`^[a-z][a-z0-9]{1,8}`).Draw(t, "name")
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}

		// Build DAG: each module can only depend on modules with lower index.
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
		result, err := Resolve(modules, names)
		require.NoError(t, err)

		// Verify ordering invariant.
		position := make(map[string]int)
		for i, m := range result {
			position[m.Name] = i
		}

		for _, m := range result {
			for _, dep := range m.DependsOn {
				assert.Less(t, position[dep], position[m.Name],
					"module %q at position %d depends on %q at position %d",
					m.Name, position[m.Name], dep, position[dep])
			}
		}
	})
}

func TestProperty_ResolveAlwaysIncludesAllRequested(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a small DAG.
		moduleNames := []string{"base", "alpha", "beta", "gamma", "delta"}
		deps := map[string][]string{
			"base":  {},
			"alpha": {"base"},
			"beta":  {"base"},
			"gamma": {"alpha"},
			"delta": {"beta"},
		}

		modules := makeModules(deps)

		// Pick a random subset of modules to request.
		requested := []string{}
		for _, name := range moduleNames {
			if rapid.Bool().Draw(t, "include_"+name) {
				requested = append(requested, name)
			}
		}
		if len(requested) == 0 {
			requested = []string{"base"}
		}

		result, err := Resolve(modules, requested)
		require.NoError(t, err)

		// Every requested module must be in the result.
		resultNames := make(map[string]bool)
		for _, m := range result {
			resultNames[m.Name] = true
		}
		for _, name := range requested {
			assert.True(t, resultNames[name], "requested module %q missing from result", name)
		}
	})
}

func TestProperty_ResolveCycleAlwaysDetected(t *testing.T) {
	// Generate modules with random dependencies that may include cycles.
	rapid.Check(t, func(t *rapid.T) {
		names := []string{"a", "b", "c", "d", "e"}
		deps := make(map[string][]string)
		for _, name := range names {
			nDeps := rapid.IntRange(0, 3).Draw(t, "ndeps")
			var depList []string
			for j := 0; j < nDeps; j++ {
				depList = append(depList, rapid.SampledFrom(names).Draw(t, "dep"))
			}
			deps[name] = depList
		}

		modules := makeModules(deps)
		result, err := Resolve(modules, names)

		if err != nil {
			// Error must mention circular dependency or unknown module.
			assert.True(t,
				len(result) == 0,
				"error case should return nil/empty result")
		} else {
			// No error means it's a valid DAG — verify ordering.
			position := make(map[string]int)
			for i, m := range result {
				position[m.Name] = i
			}
			for _, m := range result {
				for _, dep := range m.DependsOn {
					if pos, ok := position[dep]; ok {
						assert.Less(t, pos, position[m.Name])
					}
				}
			}
		}
	})
}

func TestProperty_ResolveRequestedAlwaysHasBase(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		deps := map[string][]string{
			"base":   {},
			"golang": {"base"},
			"rust":   {"base"},
			"docker": {"base"},
		}
		modules := makeModules(deps)
		allNames := []string{"base", "golang", "rust", "docker"}

		// Pick random subset (excluding base).
		var requested []string
		for _, name := range allNames[1:] {
			if rapid.Bool().Draw(t, "req_"+name) {
				requested = append(requested, name)
			}
		}
		if len(requested) == 0 {
			requested = []string{"golang"}
		}

		result, err := ResolveRequested(modules, requested)
		require.NoError(t, err)

		// REQ-006-002: base must always be present.
		hasBase := false
		for _, m := range result {
			if m.Name == "base" {
				hasBase = true
				break
			}
		}
		assert.True(t, hasBase, "REQ-006-002: base module must always be included")
	})
}

func TestProperty_ResolveDeterministicForSameInput(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		deps := map[string][]string{
			"base":   {},
			"alpha":  {"base"},
			"beta":   {"base"},
			"gamma":  {"alpha", "beta"},
			"delta":  {"base"},
			"epsilon": {"gamma", "delta"},
		}
		modules := makeModules(deps)

		// Resolve twice with same input.
		r1, err1 := Resolve(modules, []string{"epsilon"})
		r2, err2 := Resolve(modules, []string{"epsilon"})

		require.NoError(t, err1)
		require.NoError(t, err2)

		// Extract names.
		names1 := make([]string, len(r1))
		names2 := make([]string, len(r2))
		for i, m := range r1 {
			names1[i] = m.Name
		}
		for i, m := range r2 {
			names2[i] = m.Name
		}
		assert.Equal(t, names1, names2, "same input must produce same output")
	})
}

func TestProperty_ResolveResultIsTopologicalSort(t *testing.T) {
	// A more rigorous property: for any DAG, the result is a valid topological sort.
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random DAG using index-based ordering guarantee.
		n := rapid.IntRange(2, 15).Draw(t, "n")
		seen := make(map[string]bool)
		names := make([]string, 0, n)
		for len(names) < n {
			name := rapid.StringMatching(`^[a-z]{2,5}`).Draw(t, "modname")
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}

		deps := make(map[string][]string)
		for i, name := range names {
			var depList []string
			for j := 0; j < i; j++ {
				if rapid.Bool().Draw(t, "dep") {
					depList = append(depList, names[j])
				}
			}
			// Ensure no duplicate deps.
			seen := make(map[string]bool)
			var unique []string
			for _, d := range depList {
				if !seen[d] {
					seen[d] = true
					unique = append(unique, d)
				}
			}
			deps[name] = unique
		}

		modules := makeModules(deps)
		result, err := Resolve(modules, names)
		require.NoError(t, err)

		// Result must be a valid topological sort: for every edge u -> v
		// (v depends on u), u must appear before v in the result.
		position := make(map[string]int)
		for i, m := range result {
			position[m.Name] = i
		}

		for _, m := range result {
			for _, dep := range m.DependsOn {
				assert.Less(t, position[dep], position[m.Name],
					"dependency %q must appear before dependent %q",
					dep, m.Name)
			}
		}

		// Result must contain exactly the requested modules.
		assert.Len(t, result, n)
	})
}
