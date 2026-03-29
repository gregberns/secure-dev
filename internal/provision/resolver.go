// Package provision - dependency resolution via topological sort.
// REQ-006-004: Module dependency ordering.
package provision

import (
	"fmt"
	"sort"
	"strings"
)

// Resolve computes execution order for the given module names,
// including transitive dependencies.
// REQ-006-004: Topological ordering based on depends_on.
// REQ-006-002: base module is always included.
func Resolve(modules []Module, requested []string) ([]Module, error) {
	// Build name -> module map.
	byName := make(map[string]*Module, len(modules))
	for i := range modules {
		byName[modules[i].Name] = &modules[i]
	}

	// Validate all requested modules exist.
	for _, name := range requested {
		if _, ok := byName[name]; !ok {
			return nil, fmt.Errorf("unknown module %q; run \"sd provision list\" to see available modules", name)
		}
	}

	// Collect the full set of modules needed (requested + transitive deps).
	needed := make(map[string]bool)
	for _, name := range requested {
		if err := collectDeps(byName, name, needed, nil); err != nil {
			return nil, err
		}
	}

	// Build adjacency list for the needed modules.
	// Edge from A to B means A depends on B (B must run before A).
	inDegree := make(map[string]int)
	edges := make(map[string][]string) // edges[B] = [A] means B is depended on by A

	for name := range needed {
		if _, ok := inDegree[name]; !ok {
			inDegree[name] = 0
		}
		mod := byName[name]
		for _, dep := range mod.DependsOn {
			if !needed[dep] {
				// dep is not in the needed set; skip (shouldn't happen since
				// collectDeps gathered everything, but be safe).
				continue
			}
			edges[dep] = append(edges[dep], name)
			inDegree[name]++
		}
	}

	// Kahn's algorithm for topological sort.
	var queue []string
	for name := range needed {
		if inDegree[name] == 0 {
			queue = append(queue, name)
		}
	}
	// Sort queue for deterministic ordering among equal-priority nodes.
	sort.Strings(queue)

	var order []string
	for len(queue) > 0 {
		// Pick the lexicographically first to ensure determinism.
		cur := queue[0]
		queue = queue[1:]
		order = append(order, cur)

		dependents := edges[cur]
		sort.Strings(dependents)
		for _, dep := range dependents {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
				sort.Strings(queue)
			}
		}
	}

	if len(order) != len(needed) {
		// Cycle detected — identify the cycle for the error message.
		cycle := findCycle(byName, needed)
		return nil, fmt.Errorf("circular dependency detected: %s", formatCycle(cycle))
	}

	// Build result slice in topological order.
	result := make([]Module, 0, len(order))
	for _, name := range order {
		result = append(result, *byName[name])
	}
	return result, nil
}

// collectDeps recursively gathers a module and all its transitive dependencies.
func collectDeps(byName map[string]*Module, name string, needed map[string]bool, path []string) error {
	if needed[name] {
		return nil // already collected
	}

	// Check for cycle in the current traversal path.
	for _, p := range path {
		if p == name {
			// Build the cycle for the error message.
			cycle := append(path, name)
			return fmt.Errorf("circular dependency detected: %s", formatCycle(cycle))
		}
	}

	mod, ok := byName[name]
	if !ok {
		return fmt.Errorf("module %q depends on unknown module %q", path[len(path)-1], name)
	}

	path = append(path, name)

	// Collect dependencies first.
	for _, dep := range mod.DependsOn {
		if err := collectDeps(byName, dep, needed, path); err != nil {
			return err
		}
	}

	needed[name] = true
	return nil
}

// findCycle attempts to find a cycle in the dependency graph for error reporting.
func findCycle(byName map[string]*Module, needed map[string]bool) []string {
	visited := make(map[string]color)
	stack := make([]string, 0)

	for name := range needed {
		if visited[name] == white {
			if cycle := dfsCycle(byName, name, visited, &stack); cycle != nil {
				return cycle
			}
		}
	}
	return nil
}

type color int

const (
	white color = iota
	gray
	black
)

func dfsCycle(byName map[string]*Module, name string, visited map[string]color, stack *[]string) []string {
	visited[name] = gray
	*stack = append(*stack, name)

	mod := byName[name]
	if mod != nil {
		for _, dep := range mod.DependsOn {
			if visited[dep] == gray {
				// Found cycle — extract it from the stack.
				start := -1
				for i, s := range *stack {
					if s == dep {
						start = i
						break
					}
				}
				cycle := make([]string, len(*stack)-start)
				copy(cycle, (*stack)[start:])
				return cycle
			}
			if visited[dep] == white {
				if cycle := dfsCycle(byName, dep, visited, stack); cycle != nil {
					return cycle
				}
			}
		}
	}

	*stack = (*stack)[:len(*stack)-1]
	visited[name] = black
	return nil
}

// formatCycle formats a dependency cycle for display.
func formatCycle(cycle []string) string {
	if len(cycle) == 0 {
		return "(unknown)"
	}
	return strings.Join(cycle, " -> ")
}

// ResolveAll resolves all modules, ensuring base is always included.
// REQ-006-002: Base module always runs.
func ResolveAll(modules []Module) ([]Module, error) {
	names := make([]string, len(modules))
	for i, m := range modules {
		names[i] = m.Name
	}
	return Resolve(modules, names)
}

// ResolveRequested resolves only the requested modules plus their dependencies.
// Always ensures the base module is included.
// REQ-006-002: Base module always runs.
// REQ-006-015: Module selection via CLI and config.
func ResolveRequested(modules []Module, requested []string) ([]Module, error) {
	// Ensure "base" is in the requested set.
	hasBase := false
	for _, name := range requested {
		if name == "base" {
			hasBase = true
			break
		}
	}
	if !hasBase {
		requested = append(requested, "base")
	}
	return Resolve(modules, requested)
}
