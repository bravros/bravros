package plan_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/bravros/bravros/cli/internal/plan"
)

// writePlanFile creates a minimal plan file with YAML frontmatter in dir.
// depends is the list of plan IDs this plan depends on.
func writePlanFile(t *testing.T, dir, id string, depends []string) {
	t.Helper()
	var depYAML string
	if len(depends) == 0 {
		depYAML = "depends: []\n"
	} else {
		lines := []string{"depends:"}
		for _, d := range depends {
			lines = append(lines, "  - \""+d+"\"")
		}
		depYAML = strings.Join(lines, "\n") + "\n"
	}
	content := "---\nid: \"" + id + "\"\n" + depYAML + "---\n\n# Plan " + id + "\n"
	path := filepath.Join(dir, id+"-feat-test-todo.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writePlanFile %s: %v", id, err)
	}
}

// roundsEqual compares two [][]string round slices for semantic equality:
// round order is fixed, but within each round the order is ignored.
func roundsEqual(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		ac := append([]string{}, a[i]...)
		bc := append([]string{}, b[i]...)
		sort.Strings(ac)
		sort.Strings(bc)
		for j := range ac {
			if ac[j] != bc[j] {
				return false
			}
		}
	}
	return true
}

// TestPlanGraph_LinearChain: A → B → C yields [[A], [B], [C]].
func TestPlanGraph_LinearChain(t *testing.T) {
	dir := t.TempDir()
	writePlanFile(t, dir, "0001", nil)
	writePlanFile(t, dir, "0002", []string{"0001"})
	writePlanFile(t, dir, "0003", []string{"0002"})

	result, err := plan.BuildGraph(dir, []string{"0001", "0002", "0003"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := [][]string{{"0001"}, {"0002"}, {"0003"}}
	if !roundsEqual(result.Rounds, want) {
		t.Errorf("rounds = %v, want %v", result.Rounds, want)
	}
}

// TestPlanGraph_Diamond: A → B, A → C, B → D, C → D yields [[A], [B, C], [D]].
func TestPlanGraph_Diamond(t *testing.T) {
	dir := t.TempDir()
	writePlanFile(t, dir, "0001", nil)                      // A
	writePlanFile(t, dir, "0002", []string{"0001"})         // B → A
	writePlanFile(t, dir, "0003", []string{"0001"})         // C → A
	writePlanFile(t, dir, "0004", []string{"0002", "0003"}) // D → B, C

	result, err := plan.BuildGraph(dir, []string{"0001", "0002", "0003", "0004"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := [][]string{{"0001"}, {"0002", "0003"}, {"0004"}}
	if !roundsEqual(result.Rounds, want) {
		t.Errorf("rounds = %v, want %v", result.Rounds, want)
	}
}

// TestPlanGraph_Independent: A, B, C with no deps yields [[A, B, C]].
func TestPlanGraph_Independent(t *testing.T) {
	dir := t.TempDir()
	writePlanFile(t, dir, "0001", nil)
	writePlanFile(t, dir, "0002", nil)
	writePlanFile(t, dir, "0003", nil)

	result, err := plan.BuildGraph(dir, []string{"0001", "0002", "0003"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Rounds) != 1 {
		t.Fatalf("expected 1 round, got %d: %v", len(result.Rounds), result.Rounds)
	}
	want := []string{"0001", "0002", "0003"}
	got := append([]string{}, result.Rounds[0]...)
	sort.Strings(got)
	sort.Strings(want)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("round[0] = %v, want %v", got, want)
			break
		}
	}
}

// TestPlanGraph_Cycle: A → B → A should return an error.
func TestPlanGraph_Cycle(t *testing.T) {
	dir := t.TempDir()
	writePlanFile(t, dir, "0001", []string{"0002"})
	writePlanFile(t, dir, "0002", []string{"0001"})

	_, err := plan.BuildGraph(dir, []string{"0001", "0002"})
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error should mention 'cycle', got: %v", err)
	}
}

// TestPlanGraph_UnknownID: unknown plan ID should return an error.
func TestPlanGraph_UnknownID(t *testing.T) {
	dir := t.TempDir()
	writePlanFile(t, dir, "0001", nil)

	_, err := plan.BuildGraph(dir, []string{"0001", "9999"})
	if err == nil {
		t.Fatal("expected unknown-id error, got nil")
	}
	if !strings.Contains(err.Error(), "9999") {
		t.Errorf("error should mention plan ID '9999', got: %v", err)
	}
}

// TestPlanGraph_EmptyDepends: empty depends field treated same as missing.
func TestPlanGraph_EmptyDepends(t *testing.T) {
	dir := t.TempDir()
	// Write a plan with explicit empty depends list.
	content := "---\nid: \"0001\"\ndepends: []\n---\n\n# Plan 0001\n"
	path := filepath.Join(dir, "0001-feat-empty-todo.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	writePlanFile(t, dir, "0002", []string{"0001"})

	result, err := plan.BuildGraph(dir, []string{"0001", "0002"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := [][]string{{"0001"}, {"0002"}}
	if !roundsEqual(result.Rounds, want) {
		t.Errorf("rounds = %v, want %v", result.Rounds, want)
	}
}

// TestPlanGraph_ShortIDNormalization: "57" should be treated as "0057".
func TestPlanGraph_ShortIDNormalization(t *testing.T) {
	dir := t.TempDir()
	writePlanFile(t, dir, "0057", nil)
	writePlanFile(t, dir, "0058", []string{"0057"})

	// Pass short IDs — should normalize to 4-digit.
	result, err := plan.BuildGraph(dir, []string{"57", "58"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := [][]string{{"0057"}, {"0058"}}
	if !roundsEqual(result.Rounds, want) {
		t.Errorf("rounds = %v, want %v", result.Rounds, want)
	}
}

// TestPlanGraph_ExternalDepIgnored: a depends: entry pointing outside the
// provided set is ignored (treated as already satisfied).
func TestPlanGraph_ExternalDepIgnored(t *testing.T) {
	dir := t.TempDir()
	writePlanFile(t, dir, "0001", nil)
	// 0002 depends on 0001 (in set) AND on 9000 (outside set — should be ignored).
	writePlanFile(t, dir, "0002", []string{"0001", "9000"})

	result, err := plan.BuildGraph(dir, []string{"0001", "0002"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := [][]string{{"0001"}, {"0002"}}
	if !roundsEqual(result.Rounds, want) {
		t.Errorf("rounds = %v, want %v", result.Rounds, want)
	}
}
