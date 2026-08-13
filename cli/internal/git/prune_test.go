package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestFilterProtected_SplitsCorrectly verifies that FilterProtected puts
// protected branches in `kept` and all others in `removed`.
func TestFilterProtected_SplitsCorrectly(t *testing.T) {
	branches := []string{"main", "homolog", "feat/old-feature", "fix/stale-bug", "staging"}
	protected := []string{"main", "homolog", "staging", "develop"}

	kept, removed := FilterProtected(branches, protected)

	// Check kept
	keptSet := make(map[string]bool)
	for _, b := range kept {
		keptSet[b] = true
	}
	for _, p := range []string{"main", "homolog", "staging"} {
		if !keptSet[p] {
			t.Errorf("expected %q in kept, but it was not", p)
		}
	}

	// Check removed
	removedSet := make(map[string]bool)
	for _, b := range removed {
		removedSet[b] = true
	}
	for _, r := range []string{"feat/old-feature", "fix/stale-bug"} {
		if !removedSet[r] {
			t.Errorf("expected %q in removed, but it was not", r)
		}
	}

	// Ensure counts are correct
	if len(kept) != 3 {
		t.Errorf("expected 3 kept, got %d: %v", len(kept), kept)
	}
	if len(removed) != 2 {
		t.Errorf("expected 2 removed, got %d: %v", len(removed), removed)
	}
}

// TestFilterProtected_EmptyProtected puts all branches into removed.
func TestFilterProtected_EmptyProtected(t *testing.T) {
	branches := []string{"feat/a", "fix/b"}
	kept, removed := FilterProtected(branches, nil)
	if len(kept) != 0 {
		t.Errorf("expected 0 kept, got %d", len(kept))
	}
	if len(removed) != 2 {
		t.Errorf("expected 2 removed, got %d", len(removed))
	}
}

// TestFilterProtected_EmptyBranches returns empty slices without panic.
func TestFilterProtected_EmptyBranches(t *testing.T) {
	kept, removed := FilterProtected(nil, []string{"main"})
	if kept != nil || removed != nil {
		t.Errorf("expected nil slices for empty input, got kept=%v removed=%v", kept, removed)
	}
}

// TestFilterProtected_CaseSensitive verifies exact case matching.
func TestFilterProtected_CaseSensitive(t *testing.T) {
	branches := []string{"Main", "MAIN", "main"}
	protected := []string{"main"}

	kept, removed := FilterProtected(branches, protected)
	if len(kept) != 1 || kept[0] != "main" {
		t.Errorf("expected only 'main' in kept, got %v", kept)
	}
	if len(removed) != 2 {
		t.Errorf("expected 2 in removed (Main, MAIN), got %v", removed)
	}
}

// TestListMergedBranches_ParsesGitOutput verifies parsing of local merged branches
// using a real temporary git repo (no network, no remote needed).
func TestListMergedBranches_ParsesGitOutput(t *testing.T) {
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v failed: %v\n%s", args, err, out)
		}
	}

	// Set up a minimal git repo
	run("git", "init")
	run("git", "config", "user.email", "test@test.com")
	run("git", "config", "user.name", "Test")
	run("git", "checkout", "-b", "main")

	// Initial commit on main
	f := filepath.Join(dir, "README.md")
	os.WriteFile(f, []byte("# test\n"), 0644)
	run("git", "add", ".")
	run("git", "commit", "-m", "initial")

	// Create a feature branch and commit
	run("git", "checkout", "-b", "feat/merged-feature")
	os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature\n"), 0644)
	run("git", "add", ".")
	run("git", "commit", "-m", "add feature")

	// Merge it back to main
	run("git", "checkout", "main")
	run("git", "merge", "--no-ff", "feat/merged-feature", "-m", "merge feature")

	// Create an unmerged branch
	run("git", "checkout", "-b", "feat/unmerged")
	os.WriteFile(filepath.Join(dir, "unmerged.txt"), []byte("not merged\n"), 0644)
	run("git", "add", ".")
	run("git", "commit", "-m", "unmerged work")
	run("git", "checkout", "main")

	// Change working directory to the temp repo for the git commands
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	os.Chdir(dir)

	branches, err := ListMergedBranches("main")
	if err != nil {
		t.Fatalf("ListMergedBranches failed: %v", err)
	}

	// feat/merged-feature should be in the merged list
	found := false
	for _, b := range branches {
		if b == "feat/merged-feature" {
			found = true
		}
		// feat/unmerged must NOT appear
		if b == "feat/unmerged" {
			t.Errorf("feat/unmerged should not appear in merged list but got: %v", branches)
		}
		// main itself must NOT appear
		if b == "main" {
			t.Errorf("against branch 'main' must not appear in list but got: %v", branches)
		}
	}
	if !found {
		t.Errorf("expected feat/merged-feature in merged list, got: %v", branches)
	}
}
