package plan_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/bravros/bravros/cli/internal/plan"
)

// Helper: build a mini plan string from named phases with optional Touches lines.
// phases is a slice of [name, touches] pairs (touches may be empty string = no Touches line).
func buildPlanContent(phases [][2]string) string {
	var sb strings.Builder
	sb.WriteString("---\nid: \"P-0001\"\ntitle: \"test plan\"\n---\n\n# Test Plan\n\n## Phases\n\n")
	for i, ph := range phases {
		sb.WriteString("### Phase ")
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString(": ")
		sb.WriteString(ph[0])
		sb.WriteString("\n\n")
		if ph[1] != "" {
			sb.WriteString("**Touches:** ")
			sb.WriteString(ph[1])
			sb.WriteString("\n\n")
		}
		sb.WriteString("- [ ] [S] Do the thing\n\n")
	}
	return sb.String()
}

// TestRounds_SinglePhase: a plan with exactly one phase produces one round.
func TestRounds_SinglePhase(t *testing.T) {
	content := buildPlanContent([][2]string{
		{"Alpha", "`cli/cmd/alpha.go` (new)"},
	})
	phases := plan.ParsePhasesContent(content)
	if len(phases) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(phases))
	}
	result, err := plan.ComputeRounds(phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Rounds) != 1 {
		t.Fatalf("expected 1 round, got %d: %v", len(result.Rounds), result.Rounds)
	}
	if result.Rounds[0][0] != "Alpha [S] Do the thing" && !strings.Contains(result.Rounds[0][0], "Alpha") {
		t.Errorf("unexpected phase name in round: %v", result.Rounds[0])
	}
}

// TestRounds_Parallel: two phases with no file overlap run in round 1 together.
func TestRounds_Parallel(t *testing.T) {
	content := buildPlanContent([][2]string{
		{"Alpha", "`cli/cmd/alpha.go`"},
		{"Beta", "`cli/cmd/beta.go`"},
	})
	phases := plan.ParsePhasesContent(content)
	result, err := plan.ComputeRounds(phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Rounds) != 1 {
		t.Fatalf("expected 1 round (parallel), got %d: %v", len(result.Rounds), result.Rounds)
	}
	if len(result.Rounds[0]) != 2 {
		t.Errorf("expected 2 phases in round 1, got %d: %v", len(result.Rounds[0]), result.Rounds[0])
	}
}

// TestRounds_Sequential: two phases sharing a file must run sequentially.
func TestRounds_Sequential(t *testing.T) {
	content := buildPlanContent([][2]string{
		{"Alpha", "`cli/cmd/shared.go` (new)"},
		{"Beta", "`cli/cmd/shared.go`"},
	})
	phases := plan.ParsePhasesContent(content)
	result, err := plan.ComputeRounds(phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Rounds) != 2 {
		t.Fatalf("expected 2 rounds (sequential), got %d: %v", len(result.Rounds), result.Rounds)
	}
}

// TestRounds_FileConflictPartial: three phases where only first and third share a file.
// Phase1 → Phase3 (conflict), Phase2 has no overlap → runs parallel with Phase1 in round 1.
func TestRounds_FileConflictPartial(t *testing.T) {
	content := buildPlanContent([][2]string{
		{"Alpha", "`shared.go`"},
		{"Beta", "`unrelated.go`"},
		{"Gamma", "`shared.go`"},
	})
	phases := plan.ParsePhasesContent(content)
	result, err := plan.ComputeRounds(phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Alpha and Beta can run in round 1 (no conflict between them).
	// Gamma must wait for Alpha (shared.go conflict).
	if len(result.Rounds) != 2 {
		t.Fatalf("expected 2 rounds, got %d: %v", len(result.Rounds), result.Rounds)
	}
	if len(result.Rounds[0]) != 2 {
		t.Errorf("expected 2 phases in round 1, got %d: %v", len(result.Rounds[0]), result.Rounds[0])
	}
	if len(result.Rounds[1]) != 1 {
		t.Errorf("expected 1 phase in round 2, got %d: %v", len(result.Rounds[1]), result.Rounds[1])
	}
}

// TestRounds_AllSequential: three phases all touching the same file run one-by-one.
func TestRounds_AllSequential(t *testing.T) {
	content := buildPlanContent([][2]string{
		{"Alpha", "`main.go`"},
		{"Beta", "`main.go`"},
		{"Gamma", "`main.go`"},
	})
	phases := plan.ParsePhasesContent(content)
	result, err := plan.ComputeRounds(phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Rounds) != 3 {
		t.Fatalf("expected 3 rounds, got %d: %v", len(result.Rounds), result.Rounds)
	}
}

// TestRounds_MissingTouches: a phase without a **Touches:** line conflicts with all.
func TestRounds_MissingTouches(t *testing.T) {
	content := buildPlanContent([][2]string{
		{"Alpha", "`alpha.go`"},
		{"NoTouches", ""}, // no Touches line
		{"Gamma", "`gamma.go`"},
	})
	phases := plan.ParsePhasesContent(content)
	if !phases[0].HasTouches {
		t.Fatal("Alpha should have HasTouches=true")
	}
	if phases[1].HasTouches {
		t.Fatal("NoTouches should have HasTouches=false")
	}
	result, err := plan.ComputeRounds(phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Alpha must run before NoTouches; NoTouches before Gamma — 3 sequential rounds.
	if len(result.Rounds) != 3 {
		t.Fatalf("expected 3 rounds (barrier), got %d: %v", len(result.Rounds), result.Rounds)
	}
}

// TestRounds_ParsePhasesContent: verify phase name extraction from headings.
func TestRounds_ParsePhasesContent(t *testing.T) {
	content := `---
id: "P-0001"
---

## Phases

### Phase 1: CLI — round verb [S]

**Touches:** ` + "`" + `cli/cmd/plan_rounds.go` + "`" + ` (new), ` + "`" + `cli/cmd/root.go` + "`" + `

- [ ] [S] Do something

### Phase 2: Skill rewrites

**Touches:** ` + "`" + `skills/plan/SKILL.md` + "`" + `

- [ ] [H] Rewrite skill
`
	phases := plan.ParsePhasesContent(content)
	if len(phases) != 2 {
		t.Fatalf("expected 2 phases, got %d", len(phases))
	}
	if !strings.Contains(phases[0].Name, "CLI") {
		t.Errorf("phase[0].Name = %q, expected to contain 'CLI'", phases[0].Name)
	}
	if len(phases[0].Touches) != 2 {
		t.Errorf("phase[0].Touches = %v, expected 2 files", phases[0].Touches)
	}
	if !phases[0].HasTouches {
		t.Error("phase[0].HasTouches should be true")
	}
}

// TestRounds_ParseTouchesVariants: backtick-delimited file paths are extracted correctly.
func TestRounds_ParseTouchesVariants(t *testing.T) {
	content := `---
id: "P-0002"
---

## Phases

### Phase 1: Alpha

**Touches:** ` + "`" + `a/b.go` + "`" + ` (new), ` + "`" + `c/d.go` + "`" + `, ` + "`" + `e/f_test.go` + "`" + ` (new + test)

- [ ] [S] task
`
	phases := plan.ParsePhasesContent(content)
	if len(phases) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(phases))
	}
	want := []string{"a/b.go", "c/d.go", "e/f_test.go"}
	got := phases[0].Touches
	if len(got) != len(want) {
		t.Fatalf("Touches = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Touches[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRounds_BuildRoundsFromFile: integration test using a temp file.
func TestRounds_BuildRoundsFromFile(t *testing.T) {
	content := buildPlanContent([][2]string{
		{"Alpha", "`cli/a.go`"},
		{"Beta", "`cli/b.go`"},
		{"Gamma", "`cli/a.go`"}, // conflicts with Alpha
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "P-0001-test-approved.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	result, err := plan.BuildRounds(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Round 1: Alpha + Beta (no conflict); Round 2: Gamma (waits for Alpha).
	if len(result.Rounds) != 2 {
		t.Fatalf("expected 2 rounds, got %d: %v", len(result.Rounds), result.Rounds)
	}
	if len(result.Rounds[0]) != 2 {
		t.Errorf("expected 2 phases in round 1, got %d: %v", len(result.Rounds[0]), result.Rounds[0])
	}
}
