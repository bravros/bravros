package plan

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ─── Fixture helpers ──────────────────────────────────────────────────────────

// worktreeFixture represents a temporary git repository with one primary
// worktree and two linked worktrees, each on a distinct branch, each
// containing pre-seeded .planning/ files at known IDs.
type worktreeFixture struct {
	// Primary repo root.
	PrimaryRoot string
	// Linked worktree 1 root.
	Worktree1Root string
	// Linked worktree 2 root.
	Worktree2Root string
	// Branch names.
	PrimaryBranch   string // main
	Worktree1Branch string // wt1
	Worktree2Branch string // wt2
	// cleanup tears down the temp tree.
	cleanup func()
}

// runIn runs a command in dir, failing the test on error.
func runIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command %v in %s failed: %v\n%s", args, dir, err, out)
	}
}

// runInE runs a command in dir, returning the error (without failing).
func runInE(dir string, args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command %v in %s failed: %w\noutput: %s", args, dir, err, out)
	}
	return nil
}

// writeFile creates a file with the given content, making parent directories as needed.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

// newWorktreeFixture creates a fresh git repo with:
//   - primary worktree on branch "main": seeds .planning/ with plan ID 0020
//   - linked worktree wt1 on branch "wt1": seeds .planning/ with plan ID 0050
//   - linked worktree wt2 on branch "wt2": seeds .planning/backlog/ with backlog ID 0030
//
// The caller must call f.cleanup() when done.
func newWorktreeFixture(t *testing.T) *worktreeFixture {
	t.Helper()

	tmp := t.TempDir()
	primary := filepath.Join(tmp, "primary")
	wt1Dir := filepath.Join(tmp, "wt1")
	wt2Dir := filepath.Join(tmp, "wt2")

	// ── Create primary repo ───────────────────────────────────────────────────
	if err := os.MkdirAll(primary, 0o755); err != nil {
		t.Fatalf("MkdirAll primary: %v", err)
	}
	runIn(t, primary, "git", "init", "-b", "main")
	runIn(t, primary, "git", "config", "user.email", "test@example.com")
	runIn(t, primary, "git", "config", "user.name", "Test")

	// Seed initial commit on main with a plan at ID 0020.
	writeFile(t, filepath.Join(primary, ".planning", "P-0020-initial-plan-todo.md"), "# plan 0020\n")
	runIn(t, primary, "git", "add", ".")
	runIn(t, primary, "git", "commit", "-m", "chore: initial commit")

	// ── Create branch wt1 from main, add plan 0050 ───────────────────────────
	runIn(t, primary, "git", "checkout", "-b", "wt1")
	writeFile(t, filepath.Join(primary, ".planning", "P-0050-wt1-plan-todo.md"), "# plan 0050\n")
	runIn(t, primary, "git", "add", ".")
	runIn(t, primary, "git", "commit", "-m", "chore: add wt1 plan")

	// ── Create branch wt2 from main, add backlog 0030 ────────────────────────
	runIn(t, primary, "git", "checkout", "main")
	runIn(t, primary, "git", "checkout", "-b", "wt2")
	writeFile(t, filepath.Join(primary, ".planning", "backlog", "B-0030-wt2-backlog.md"), "# backlog 0030\n")
	runIn(t, primary, "git", "add", ".")
	runIn(t, primary, "git", "commit", "-m", "chore: add wt2 backlog")

	// Return to main.
	runIn(t, primary, "git", "checkout", "main")

	// ── Add linked worktrees ──────────────────────────────────────────────────
	runIn(t, primary, "git", "worktree", "add", wt1Dir, "wt1")
	runIn(t, primary, "git", "worktree", "add", wt2Dir, "wt2")

	f := &worktreeFixture{
		PrimaryRoot:     primary,
		Worktree1Root:   wt1Dir,
		Worktree2Root:   wt2Dir,
		PrimaryBranch:   "main",
		Worktree1Branch: "wt1",
		Worktree2Branch: "wt2",
		cleanup:         func() { /* t.TempDir() handles removal */ },
	}
	return f
}

// runScanInDir changes the working directory for ScanAllSources by relying on
// runGitCommand using cmd.Dir (it doesn't — it uses the process cwd). We must
// chdir into the fixture's repo so git commands resolve correctly.
//
// Returns (highest, sources, err) from ScanAllSources, restoring cwd afterwards.
func runScanInDir(t *testing.T, dir, prefix string) (int, []ScanSource, error) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir %s: %v", dir, err)
	}
	defer func() {
		if err := os.Chdir(orig); err != nil {
			t.Fatalf("Chdir restore %s: %v", orig, err)
		}
	}()
	return ScanAllSources(prefix)
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestScanAllSources_PlansAcrossWorktrees verifies that when wt1 branch has a
// plan at ID 0050 and main has 0020, ScanAllSources returns 50 as the highest.
func TestScanAllSources_PlansAcrossWorktrees(t *testing.T) {
	f := newWorktreeFixture(t)
	defer f.cleanup()

	// Run from the primary repo root so git commands find the right repo.
	highest, sources, err := runScanInDir(t, f.PrimaryRoot, "P")
	if err != nil {
		t.Fatalf("ScanAllSources: %v", err)
	}

	// Must have scanned at least one source.
	if len(sources) == 0 {
		t.Fatal("ScanAllSources returned no sources")
	}

	// The highest plan ID across all branches/worktrees must be 50 (from wt1).
	if highest != 50 {
		t.Errorf("highest plan ID: got %d, want 50 (from wt1 branch)", highest)
	}
}

// TestScanAllSources_BacklogsAcrossWorktrees verifies that when wt2 branch has
// a backlog at ID 0030, ScanAllSources("B") returns 30.
func TestScanAllSources_BacklogsAcrossWorktrees(t *testing.T) {
	f := newWorktreeFixture(t)
	defer f.cleanup()

	highest, sources, err := runScanInDir(t, f.PrimaryRoot, "B")
	if err != nil {
		t.Fatalf("ScanAllSources: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("ScanAllSources returned no sources")
	}
	if highest != 30 {
		t.Errorf("highest backlog ID: got %d, want 30 (from wt2 branch)", highest)
	}
}

// TestScanAllSources_AllFiveEntities verifies that each of the five entity
// prefixes (P/B/R/U/D) can be scanned without error and returns a non-negative
// integer. Uses a fixture that seeds at least one file of each type.
func TestScanAllSources_AllFiveEntities(t *testing.T) {
	tmp := t.TempDir()
	primary := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(primary, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	runIn(t, primary, "git", "init", "-b", "main")
	runIn(t, primary, "git", "config", "user.email", "test@example.com")
	runIn(t, primary, "git", "config", "user.name", "Test")

	// Seed one file per entity in .planning/.
	seeds := map[string]string{
		"P": filepath.Join(primary, ".planning", "P-0001-plan.md"),
		"B": filepath.Join(primary, ".planning", "backlog", "B-0002-backlog.md"),
		"R": filepath.Join(primary, ".planning", "reports", "R-0003-report.md"),
		"U": filepath.Join(primary, ".planning", "user-reports", "U-0004-user.md"),
		"S": "", // scout is directory-kind; create the dir itself
	}
	for _, path := range seeds {
		if path == "" {
			continue
		}
		writeFile(t, path, "# entity\n")
	}
	// Scout dir: create S-0005-slug-open/ directory.
	scoutDir := filepath.Join(primary, ".planning", "scout", "S-0005-slug-open")
	if err := os.MkdirAll(scoutDir, 0o755); err != nil {
		t.Fatalf("MkdirAll scout: %v", err)
	}

	runIn(t, primary, "git", "add", ".")
	runIn(t, primary, "git", "commit", "-m", "chore: seed all entities")

	// Expected highest IDs per entity.
	want := map[string]int{
		"P": 1,
		"B": 2,
		"R": 3,
		"U": 4,
		"S": 5,
	}

	for _, prefix := range []string{"P", "B", "R", "U", "S"} {
		prefix := prefix
		t.Run(prefix, func(t *testing.T) {
			highest, sources, err := runScanInDir(t, primary, prefix)
			if err != nil {
				t.Fatalf("ScanAllSources(%q): %v", prefix, err)
			}
			if len(sources) == 0 {
				t.Fatalf("ScanAllSources(%q): no sources returned", prefix)
			}
			if highest < 0 {
				t.Errorf("ScanAllSources(%q): highest < 0: %d", prefix, highest)
			}
			if highest != want[prefix] {
				t.Errorf("ScanAllSources(%q): highest=%d, want %d", prefix, highest, want[prefix])
			}
		})
	}
}

// TestScanAllSources_PlaceholdersCountedInWorktreeFS verifies that an
// uncommitted .placeholder file on the current worktree's filesystem is counted
// by the worktree-fs leg, ensuring the reserved ID is not reused.
func TestScanAllSources_PlaceholdersCountedInWorktreeFS(t *testing.T) {
	tmp := t.TempDir()
	primary := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(primary, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	runIn(t, primary, "git", "init", "-b", "main")
	runIn(t, primary, "git", "config", "user.email", "test@example.com")
	runIn(t, primary, "git", "config", "user.name", "Test")

	// Seed an initial commit (any file) so the repo is non-empty.
	writeFile(t, filepath.Join(primary, "README.md"), "# test\n")
	runIn(t, primary, "git", "add", ".")
	runIn(t, primary, "git", "commit", "-m", "chore: initial")

	// Write a placeholder file at P-0099 — NOT committed.
	phPath := filepath.Join(primary, ".planning", "P-0099.placeholder")
	if err := os.MkdirAll(filepath.Dir(phPath), 0o755); err != nil {
		t.Fatalf("MkdirAll planning: %v", err)
	}
	if err := os.WriteFile(phPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile placeholder: %v", err)
	}

	highest, _, err := runScanInDir(t, primary, "P")
	if err != nil {
		t.Fatalf("ScanAllSources: %v", err)
	}

	// The placeholder must be found; highest must be at least 99.
	if highest < 99 {
		t.Errorf("expected placeholder P-0099 to be counted; got highest=%d, want >=99", highest)
	}
}

// TestScanAllSources_PhantomHighIDInRemoteRef verifies that when a "remote" branch
// (simulated as a second local branch) contains a phantom-high plan ID (e.g. P-2026),
// ScanAllSources returns that high value, and per-source diagnostic calls surface
// the ref name and the high number. This covers the B-0312 --verbose plumbing scenario.
func TestScanAllSources_PhantomHighIDInRemoteRef(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	runIn(t, repo, "git", "init", "-b", "main")
	runIn(t, repo, "git", "config", "user.email", "test@example.com")
	runIn(t, repo, "git", "config", "user.name", "Test")

	// Seed main with a normal plan ID 0010.
	writeFile(t, filepath.Join(repo, ".planning", "P-0010-normal-plan-todo.md"), "# plan 0010\n")
	runIn(t, repo, "git", "add", ".")
	runIn(t, repo, "git", "commit", "-m", "chore: initial commit")

	// Create a branch "phantom" with an unusually high plan ID (P-2026).
	runIn(t, repo, "git", "checkout", "-b", "phantom")
	writeFile(t, filepath.Join(repo, ".planning", "P-2026-phantom-plan-todo.md"), "# plan 2026\n")
	runIn(t, repo, "git", "add", ".")
	runIn(t, repo, "git", "commit", "-m", "chore: phantom high ID")
	runIn(t, repo, "git", "checkout", "main")

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()

	// ScanAllSources must return 2026 as the highest (phantom branch is visible via branch-tree leg).
	highest, sources, err := ScanAllSources("P")
	if err != nil {
		t.Fatalf("ScanAllSources: %v", err)
	}
	if highest < 2026 {
		t.Errorf("expected highest >= 2026 (phantom branch P-2026 should be visible); got %d", highest)
	}
	if len(sources) == 0 {
		t.Fatal("ScanAllSources returned no sources")
	}

	// Find which source contributed the highest via per-source diagnostic functions —
	// this is what the cmd-layer --verbose diagnostic calls.
	entity, ok := EntityByPrefix("P")
	if !ok {
		t.Fatal("EntityByPrefix P not found")
	}

	phantomRefFound := false
	for _, src := range sources {
		var n int
		var refName string
		if src.Kind == "worktree-fs" {
			n, _ = ScanWorktreeFSForDiag(src.Path, entity)
			refName = src.Path
		} else {
			n, _ = ScanBranchTreeForDiag(src.Branch, entity)
			refName = src.Branch
		}
		// The "phantom" branch-tree source should surface highest=2026.
		if n >= 2026 {
			if !strings.Contains(refName, "phantom") {
				t.Errorf("expected ref name to contain 'phantom'; got %q", refName)
			}
			phantomRefFound = true
		}
	}
	if !phantomRefFound {
		t.Error("no source reported a highest >= 2026 — phantom branch was not detected by per-source diagnostic scan")
	}
}

// TestScanAllSources_GitTreeError_PropagatesError verifies that a git subprocess
// error (e.g. from a corrupt / unusable worktree reference) is propagated as an
// error rather than silently ignored.
//
// Strategy: we create a repo with no commits and then call ScanAllSources;
// `git for-each-ref` succeeds on empty repos, but `git ls-tree` on a
// non-existent branch fails. We inject a fake branch name into a source list
// by monkey-patching ResolveScanRoots via a temporary branch that doesn't
// actually have the listed ref, achieved by calling scanBranchTree directly
// with a bogus branch name.
func TestScanAllSources_GitTreeError_PropagatesError(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	runIn(t, repo, "git", "init", "-b", "main")
	runIn(t, repo, "git", "config", "user.email", "test@example.com")
	runIn(t, repo, "git", "config", "user.name", "Test")

	// Single commit so git ls-tree against a real branch works.
	writeFile(t, filepath.Join(repo, "README.md"), "# test\n")
	runIn(t, repo, "git", "add", ".")
	runIn(t, repo, "git", "commit", "-m", "chore: initial")

	// Call scanBranchTree directly with a bogus branch name to verify error propagation.
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()

	entity, ok := EntityByPrefix("P")
	if !ok {
		t.Fatal("EntityByPrefix P not found")
	}

	// "refs/heads/nonexistent-branch-zzz" should not exist → git ls-tree errors.
	_, scanErr := scanBranchTree("nonexistent-branch-zzz-abc", entity)
	if scanErr == nil {
		t.Error("expected error from scanBranchTree with nonexistent branch, got nil")
	}
	if !strings.Contains(scanErr.Error(), "git ls-tree") && !strings.Contains(scanErr.Error(), "scanBranchTree") {
		t.Errorf("error message should mention git ls-tree or scanBranchTree; got: %v", scanErr)
	}
}

// TestScanWorktreeFS_IncludesPlanFolderDirs verifies that scanWorktreeFS
// (P-0180 Phase 2) counts a P-NNNN-<slug>/ folder-plan directory alongside
// numbered *.md files when scanning the dual-kind plan entity, and that the
// highest ID wins regardless of which shape it came from.
func TestScanWorktreeFS_IncludesPlanFolderDirs(t *testing.T) {
	tmp := t.TempDir()

	planDef, ok := EntityByPrefix("P")
	if !ok {
		t.Fatal("EntityByPrefix P not found")
	}

	// A numbered file at 0020 and a folder-plan directory at 0180 — the
	// folder-plan is the highest and must win.
	writeFile(t, filepath.Join(tmp, ".planning", "P-0020-file-plan-todo.md"), "# plan 0020\n")
	folderDir := filepath.Join(tmp, ".planning", "P-0180-feat-folder-plan")
	if err := os.MkdirAll(folderDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(folderDir, "PLAN.md"), []byte("---\nid: P-0180\n---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	highest, scanErr := scanWorktreeFS(tmp, planDef)
	if scanErr != nil {
		t.Fatalf("scanWorktreeFS: %v", scanErr)
	}
	if highest != 180 {
		t.Errorf("scanWorktreeFS with folder-plan present: got %d, want 180", highest)
	}
}

// TestScanWorktreeFS_PlanFolderCompleteSuffixStillCounted verifies that a
// "-complete" suffixed folder-plan directory (post-/finish, P-0180 locked
// decision #6) is still counted toward the highest ID — completed
// folder-plans must not free up their ID for reuse.
func TestScanWorktreeFS_PlanFolderCompleteSuffixStillCounted(t *testing.T) {
	tmp := t.TempDir()

	planDef, ok := EntityByPrefix("P")
	if !ok {
		t.Fatal("EntityByPrefix P not found")
	}

	completeDir := filepath.Join(tmp, ".planning", "P-0199-feat-done-complete")
	if err := os.MkdirAll(completeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	highest, scanErr := scanWorktreeFS(tmp, planDef)
	if scanErr != nil {
		t.Fatalf("scanWorktreeFS: %v", scanErr)
	}
	if highest != 199 {
		t.Errorf("scanWorktreeFS with -complete folder-plan: got %d, want 199", highest)
	}
}

// TestScanAllSources_PlanFolderAcrossBranchTree verifies the git-tree leg
// (scanBranchTree) also counts a folder-plan directory committed on a
// sibling branch — the highest ID across worktrees must include folder-plans,
// not just numbered *.md files.
func TestScanAllSources_PlanFolderAcrossBranchTree(t *testing.T) {
	tmp := t.TempDir()
	primary := filepath.Join(tmp, "primary")
	wtDir := filepath.Join(tmp, "wt-folder-plan")

	if err := os.MkdirAll(primary, 0o755); err != nil {
		t.Fatalf("MkdirAll primary: %v", err)
	}
	runIn(t, primary, "git", "init", "-b", "main")
	runIn(t, primary, "git", "config", "user.email", "test@example.com")
	runIn(t, primary, "git", "config", "user.name", "Test")

	writeFile(t, filepath.Join(primary, ".planning", "P-0010-initial-plan-todo.md"), "# plan 0010\n")
	runIn(t, primary, "git", "add", ".")
	runIn(t, primary, "git", "commit", "-m", "chore: initial commit")

	// Sibling branch carries a folder-plan at a higher ID.
	runIn(t, primary, "git", "checkout", "-b", "wt-folder-plan")
	writeFile(t, filepath.Join(primary, ".planning", "P-0090-feat-folder-plan", "PLAN.md"), "---\nid: P-0090\n---\n")
	runIn(t, primary, "git", "add", ".")
	runIn(t, primary, "git", "commit", "-m", "chore: add folder-plan")
	runIn(t, primary, "git", "checkout", "main")
	runIn(t, primary, "git", "worktree", "add", wtDir, "wt-folder-plan")

	highest, sources, err := runScanInDir(t, primary, "P")
	if err != nil {
		t.Fatalf("ScanAllSources: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("ScanAllSources returned no sources")
	}
	if highest != 90 {
		t.Errorf("highest plan ID with folder-plan on sibling branch: got %d, want 90", highest)
	}
}

// TestScanAllSources_DeletedIDNotRecycled covers the failure that reissued a
// backlog id in production: an item was closed and deleted in one session, and
// the next `backlog add` handed the same number straight back out while a commit
// message still referenced the original meaning.
//
// Every other scan leg reads a CURRENT tree (ls-tree per branch, worktree
// filesystem), so a deleted id is invisible to all of them. Ids must be
// monotonic — deleting an entry leaves a permanent gap, never a free number.
func TestScanAllSources_DeletedIDNotRecycled(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runIn(t, repo, "git", "init", "-b", "main")
	runIn(t, repo, "git", "config", "user.email", "test@example.com")
	runIn(t, repo, "git", "config", "user.name", "Test")

	writeFile(t, filepath.Join(repo, ".planning", "backlog", "B-0007-thing-todo.md"), "# thing\n")
	runIn(t, repo, "git", "add", ".")
	runIn(t, repo, "git", "commit", "-m", "chore: add B-0007")

	// Close it out and delete the file, exactly as the parallel session did.
	runIn(t, repo, "git", "rm", "-q", ".planning/backlog/B-0007-thing-todo.md")
	runIn(t, repo, "git", "commit", "-m", "chore: close + delete B-0007")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	highest, _, err := ScanAllSources("B")
	if err != nil {
		t.Fatalf("ScanAllSources: %v", err)
	}
	if highest != 7 {
		t.Errorf("deleted id must still count as allocated: got highest=%d, want 7 "+
			"(next id would be B-%04d, recycling the deleted number)", highest, highest+1)
	}
}

// TestScanBranchTree_FolderPlanIDVisible covers folder-plan ids being invisible
// on the branch leg. A recursive ls-tree yields `.planning/P-0011-slug/PLAN.md`,
// whose BASENAME is `PLAN.md` and carries no id — so keying on the basename let a
// folder plan living on another branch fail to block its own id.
func TestScanBranchTree_FolderPlanIDVisible(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runIn(t, repo, "git", "init", "-b", "main")
	runIn(t, repo, "git", "config", "user.email", "test@example.com")
	runIn(t, repo, "git", "config", "user.name", "Test")

	writeFile(t, filepath.Join(repo, ".planning", "P-0011-folder-plan", "PLAN.md"), "# folder plan\n")
	runIn(t, repo, "git", "add", ".")
	runIn(t, repo, "git", "commit", "-m", "chore: folder plan P-0011")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	planDef, ok := EntityByPrefix("P")
	if !ok {
		t.Fatal("plan entity not found")
	}
	n, err := scanBranchTree("main", planDef)
	if err != nil {
		t.Fatalf("scanBranchTree: %v", err)
	}
	if n != 11 {
		t.Errorf("folder-plan id must be visible on the branch leg: got %d, want 11", n)
	}
}
