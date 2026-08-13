package plan

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// PhaseInfo holds parsed information about a single phase in a plan file.
type PhaseInfo struct {
	// Name is the full phase heading text, e.g. "Phase 1: CLI — round verb [S]".
	Name string
	// Touches is the set of files listed in the **Touches:** line for this phase.
	// Empty if the Touches line is absent (treated as conflicting-with-all).
	Touches []string
	// HasTouches is false when the **Touches:** line is missing entirely.
	HasTouches bool
}

// RoundsResult is the JSON-serialisable output of plan-rounds.
type RoundsResult struct {
	Rounds [][]string `json:"rounds"`
}

var (
	// phaseHeadingRe matches lines like "### Phase 1: Name" or "### Phase 2: Name [S]".
	phaseHeadingRe = regexp.MustCompile(`^### Phase \d+:\s*(.+)`)
	// touchesLineRe matches the **Touches:** line, e.g.:
	//   **Touches:** `a.go` (new), `b.go`
	touchesLineRe = regexp.MustCompile(`(?i)^\*\*Touches:\*\*\s*(.+)`)
	// backtickTokenRe extracts file paths from backtick spans.
	backtickTokenRe = regexp.MustCompile("`([^`]+)`")
)

// parseTouchesLine extracts the file set from a **Touches:** line.
// E.g.: "`cli/cmd/plan_rounds.go` (new), `cli/cmd/root.go`"
// → ["cli/cmd/plan_rounds.go", "cli/cmd/root.go"]
func parseTouchesLine(line string) []string {
	matches := backtickTokenRe.FindAllStringSubmatch(line, -1)
	seen := make(map[string]bool)
	var files []string
	for _, m := range matches {
		// m[1] is the content inside backticks.
		file := strings.TrimSpace(m[1])
		if file == "" || seen[file] {
			continue
		}
		seen[file] = true
		files = append(files, file)
	}
	return files
}

// ParsePhases reads a plan file and returns the ordered list of phases with
// their Touches sets. Files can be a path on disk.
func ParsePhases(path string) ([]PhaseInfo, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading plan file %s: %w", path, err)
	}
	return ParsePhasesContent(string(content)), nil
}

// ParsePhasesContent parses phases from plan file content (without disk I/O).
// It is used by tests and by ParsePhases.
func ParsePhasesContent(content string) []PhaseInfo {
	var phases []PhaseInfo
	lines := strings.Split(content, "\n")

	currentPhase := -1 // index into phases

	for _, line := range lines {
		// Detect a new phase heading.
		if m := phaseHeadingRe.FindStringSubmatch(line); m != nil {
			phases = append(phases, PhaseInfo{
				Name:       strings.TrimSpace(m[1]),
				HasTouches: false,
			})
			currentPhase = len(phases) - 1
			continue
		}

		// Detect the **Touches:** line under the current phase.
		if currentPhase >= 0 && !phases[currentPhase].HasTouches {
			if m := touchesLineRe.FindStringSubmatch(line); m != nil {
				phases[currentPhase].Touches = parseTouchesLine(m[1])
				phases[currentPhase].HasTouches = true
			}
		}
	}

	return phases
}

// ComputeRounds takes an ordered slice of PhaseInfo and returns parallel
// execution rounds using a file-conflict graph.
//
// Algorithm:
//  1. Build a directed "must-run-after" graph: if phase B has any file overlap
//     with phase A (and A comes before B in the plan), then A→B (A must
//     complete before B starts). Phases without a Touches line conflict with
//     all other phases.
//  2. Run Kahn's topological sort over that graph; each wave of zero-in-degree
//     nodes forms one parallel round.
//
// The original phase order is preserved as a tiebreaker within rounds.
func ComputeRounds(phases []PhaseInfo) (*RoundsResult, error) {
	n := len(phases)
	if n == 0 {
		return &RoundsResult{Rounds: [][]string{}}, nil
	}

	// Build file → set-of-phase-indices map.
	fileToPhases := make(map[string][]int)
	for i, ph := range phases {
		if !ph.HasTouches {
			// No Touches line: use a sentinel that means "every file".
			continue
		}
		for _, f := range ph.Touches {
			fileToPhases[f] = append(fileToPhases[f], i)
		}
	}

	// adjacency[i] = set of phase indices that MUST run after phase i.
	successors := make([]map[int]bool, n)
	for i := range successors {
		successors[i] = make(map[int]bool)
	}

	addEdge := func(from, to int) {
		if from != to {
			successors[from][to] = true
		}
	}

	// File-conflict edges: for each file, phases touching it are totally ordered
	// by their original position in the plan.
	for _, phaseIdxs := range fileToPhases {
		// Sort by original order.
		sorted := append([]int{}, phaseIdxs...)
		sort.Ints(sorted)
		for k := 0; k < len(sorted)-1; k++ {
			addEdge(sorted[k], sorted[k+1])
		}
	}

	// Phases without a Touches line conflict with ALL others.
	// A no-touches phase must run after all preceding phases and before all
	// following phases (it acts as a barrier).
	for i, ph := range phases {
		if ph.HasTouches {
			continue
		}
		// All phases before i must complete before i.
		for j := 0; j < i; j++ {
			addEdge(j, i)
		}
		// i must complete before all phases after i.
		for j := i + 1; j < n; j++ {
			addEdge(i, j)
		}
	}

	// Kahn's algorithm on the successors graph.
	inDegree := make([]int, n)
	for i := 0; i < n; i++ {
		for j := range successors[i] {
			inDegree[j]++
		}
	}

	var rounds [][]string
	placed := make([]bool, n)
	totalPlaced := 0

	for totalPlaced < n {
		var round []int
		for i := 0; i < n; i++ {
			if !placed[i] && inDegree[i] == 0 {
				round = append(round, i)
			}
		}

		if len(round) == 0 {
			// Cycle detected — gather remaining nodes.
			var remaining []string
			for i := 0; i < n; i++ {
				if !placed[i] {
					remaining = append(remaining, phases[i].Name)
				}
			}
			return nil, fmt.Errorf("cycle detected among phases: %s", strings.Join(remaining, ", "))
		}

		// sort.Ints preserves original plan order for ties.
		sort.Ints(round)

		var names []string
		for _, i := range round {
			names = append(names, phases[i].Name)
			placed[i] = true
			totalPlaced++
			// Decrement successors' in-degrees.
			for j := range successors[i] {
				inDegree[j]--
			}
		}

		rounds = append(rounds, names)
	}

	return &RoundsResult{Rounds: rounds}, nil
}

// BuildRounds is the top-level entry point: parse phases from path, compute rounds.
func BuildRounds(path string) (*RoundsResult, error) {
	phases, err := ParsePhases(path)
	if err != nil {
		return nil, err
	}
	return ComputeRounds(phases)
}
