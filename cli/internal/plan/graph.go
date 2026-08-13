package plan

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// PlanFrontmatter holds the minimal frontmatter fields needed for graph resolution.
// Accepts both `depends:` (canonical) and `dependsOn:` (legacy) keys.
type PlanFrontmatter struct {
	ID        string   `yaml:"id"`
	Depends   []string `yaml:"depends"`
	DependsOn []string `yaml:"dependsOn"`
}

// GraphResult is the JSON output of the plan-graph command.
type GraphResult struct {
	Rounds [][]string `json:"rounds"`
}

// NormalizePlanID strips any planning ID prefix (`P-`, `B-`, `R-`, `U-`) and
// zero-pads the numeric part to 4 digits.
// "57" → "0057", "0057" → "0057", "P-0091" → "0091", "B-0145" → "0145".
func NormalizePlanID(raw string) string {
	raw = strings.TrimSpace(raw)
	// Strip known planning ID prefixes — derived from the canonical registry so
	// new entities are handled automatically without editing this function.
	for _, prefix := range AllPrefixes() {
		if strings.HasPrefix(raw, prefix) {
			raw = raw[len(prefix):]
			break
		}
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return raw // non-numeric: return as-is (will fail lookup later)
	}
	return fmt.Sprintf("%04d", n)
}

// parsePlanDepends reads the YAML frontmatter of a plan file and returns the
// `depends:` list (normalized to 4-digit IDs). Returns an empty slice on any
// error (missing file, no frontmatter, missing field).
func parsePlanDepends(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	text := string(content)
	if !strings.HasPrefix(text, "---") {
		return nil, nil
	}

	parts := strings.SplitN(text, "---", 3)
	if len(parts) < 3 {
		return nil, nil
	}

	var fm PlanFrontmatter
	if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
		return nil, fmt.Errorf("YAML parse error in %s: %w", path, err)
	}

	// Merge `depends:` and `dependsOn:` (legacy) into one list.
	raw := append([]string{}, fm.Depends...)
	raw = append(raw, fm.DependsOn...)
	normalized := make([]string, 0, len(raw))
	for _, dep := range raw {
		normalized = append(normalized, NormalizePlanID(dep))
	}
	return normalized, nil
}

// BuildGraph takes a set of plan IDs (already normalized to 4 digits), reads
// their `depends:` frontmatter from planningDir, and returns the parallel rounds
// via Kahn's algorithm.
//
// Rules:
//   - Unknown IDs (no matching file in planningDir) → error
//   - Dependencies that point outside the provided set are ignored (they are
//     considered satisfied by the caller).
//   - Cycle → error listing the cycle members.
//   - Plans with no deps (or empty deps) go in Round 1 together.
func BuildGraph(planningDir string, ids []string) (*GraphResult, error) {
	// Normalize all input IDs to 4-digit form and deduplicate while preserving order.
	seen := make(map[string]bool, len(ids))
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		norm := NormalizePlanID(id)
		if !seen[norm] {
			seen[norm] = true
			unique = append(unique, norm)
		}
	}
	ids = unique

	// Validate all IDs exist and build the dependency map restricted to the set.
	depMap := make(map[string][]string, len(ids))
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	for _, id := range ids {
		path := findPlanByID(planningDir, id)
		if path == "" {
			return nil, fmt.Errorf("unknown plan ID: %s (no matching file in %s)", id, planningDir)
		}
		deps, err := parsePlanDepends(path)
		if err != nil {
			return nil, fmt.Errorf("error reading plan %s: %w", id, err)
		}
		// Keep only deps that are within the provided set.
		restricted := make([]string, 0)
		for _, dep := range deps {
			if idSet[dep] {
				restricted = append(restricted, dep)
			}
		}
		depMap[id] = restricted
	}

	// Kahn's algorithm: compute in-degrees.
	inDegree := make(map[string]int, len(ids))
	for _, id := range ids {
		inDegree[id] = 0
	}
	for _, id := range ids {
		for range depMap[id] {
			inDegree[id]++ // id depends on a predecessor → id's in-degree++
		}
	}

	// Successors map: dep → list of plans that depend on dep.
	successors := make(map[string][]string, len(ids))
	for _, id := range ids {
		for _, dep := range depMap[id] {
			successors[dep] = append(successors[dep], id)
		}
	}

	var rounds [][]string
	placed := 0

	for placed < len(ids) {
		// Collect all zero-in-degree nodes as the next round.
		var round []string
		for _, id := range ids {
			if inDegree[id] == 0 {
				round = append(round, id)
			}
		}

		if len(round) == 0 {
			// Cycle detected — gather remaining nodes.
			var cycleNodes []string
			for _, id := range ids {
				if inDegree[id] > 0 {
					cycleNodes = append(cycleNodes, id)
				}
			}
			sort.Strings(cycleNodes)
			return nil, fmt.Errorf("cycle detected among plans: %s", strings.Join(cycleNodes, ", "))
		}

		// Sort round for deterministic output.
		sort.Strings(round)

		// Mark these as placed by setting their in-degree to -1.
		for _, id := range round {
			inDegree[id] = -1
			placed++
			// Decrement successors.
			for _, succ := range successors[id] {
				inDegree[succ]--
			}
		}

		rounds = append(rounds, round)
	}

	return &GraphResult{Rounds: rounds}, nil
}
