package branch

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	gitpkg "github.com/bravros/bravros/cli/internal/git"
)

// makeGitRepo creates a bare-minimum git repo in a temp dir and returns the path.
// It sets the working directory to that path via os.Chdir and returns a cleanup func.
//
// Also sets BRAVROS_BRANCH_PRUNE_BYPASS_ACTIVE_CMD=1 so the active-command guard
// added in P-0161 follow-up doesn't trip when tests run inside a live Claude Code
// session (which itself maintains the marker). `t.Setenv` reverts at test end.
func makeGitRepo(t *testing.T) (dir string, restore func()) {
	t.Helper()
	t.Setenv("BRAVROS_BRANCH_PRUNE_BYPASS_ACTIVE_CMD", "1")
	dir = t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup cmd %v: %v\n%s", args, err, out)
		}
	}

	run("git", "init")
	run("git", "config", "user.email", "test@test.com")
	run("git", "config", "user.name", "Test")
	run("git", "checkout", "-b", "main")

	// Initial commit.
	f := filepath.Join(dir, "README.md")
	os.WriteFile(f, []byte("# test\n"), 0644)
	run("git", "add", ".")
	run("git", "commit", "-m", "initial")

	orig, _ := os.Getwd()
	os.Chdir(dir)
	return dir, func() { os.Chdir(orig) }
}

// ---- IsProtected tests -------------------------------------------------------

func TestIsProtected_WellKnownNames(t *testing.T) {
	for _, name := range []string{"main", "master", "homolog", "staging", "develop", "production"} {
		protected, reason := IsProtected(name)
		if !protected {
			t.Errorf("IsProtected(%q) = false, want true", name)
		}
		if reason == "" {
			t.Errorf("IsProtected(%q) returned empty reason", name)
		}
	}
}

func TestIsProtected_FeatureBranch_NotProtected(t *testing.T) {
	protected, _ := IsProtected("feat/some-feature")
	if protected {
		t.Error("IsProtected(feat/some-feature) = true, want false")
	}
}

// ---- IsCurrentHEAD tests -----------------------------------------------------

func TestIsCurrentHEAD_Match(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	head, err := IsCurrentHEAD("main")
	if err != nil {
		t.Fatalf("IsCurrentHEAD error: %v", err)
	}
	if !head {
		t.Error("IsCurrentHEAD(main) = false, want true when checked out on main")
	}
}

func TestIsCurrentHEAD_NoMatch(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	head, err := IsCurrentHEAD("feat/other-branch")
	if err != nil {
		t.Fatalf("IsCurrentHEAD error: %v", err)
	}
	if head {
		t.Error("IsCurrentHEAD(feat/other-branch) = true, want false when on main")
	}
}

// ---- HasOpenPlanRef tests ----------------------------------------------------

func TestHasOpenPlanRef_Match(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// Create a fake .planning dir with an approved plan that references our branch.
	planDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planDir, 0755)
	content := "---\nbranch: feat/my-feature\ntitle: test\n---\n# Plan\n"
	os.WriteFile(filepath.Join(planDir, "P-0001-test-approved.md"), []byte(content), 0644)

	has, path := HasOpenPlanRef("feat/my-feature")
	if !has {
		t.Error("HasOpenPlanRef(feat/my-feature) = false, want true")
	}
	if path == "" {
		t.Error("expected non-empty plan path")
	}
}

func TestHasOpenPlanRef_NoMatch(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	planDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planDir, 0755)
	content := "---\nbranch: feat/some-other-branch\ntitle: test\n---\n# Plan\n"
	os.WriteFile(filepath.Join(planDir, "P-0002-other-approved.md"), []byte(content), 0644)

	has, _ := HasOpenPlanRef("feat/my-feature")
	if has {
		t.Error("HasOpenPlanRef(feat/my-feature) = true, want false when branch doesn't match")
	}
}

func TestHasOpenPlanRef_NoPlanningDir(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	has, _ := HasOpenPlanRef("feat/anything")
	if has {
		t.Error("HasOpenPlanRef should return false when .planning/ doesn't exist")
	}
}

// ---- HasActiveWorktree tests -------------------------------------------------

func TestHasActiveWorktree_NoWorktrees(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	// Single worktree (primary) — no additional worktrees.
	has, _, err := HasActiveWorktree("feat/no-worktree")
	if err != nil {
		t.Fatalf("HasActiveWorktree error: %v", err)
	}
	if has {
		t.Error("HasActiveWorktree = true on a repo with no extra worktrees, want false")
	}
}

func TestHasActiveWorktree_WithWorktree(t *testing.T) {
	dir, restore := makeGitRepo(t)
	defer restore()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	// Create a branch and a worktree for it.
	wtDir := filepath.Join(t.TempDir(), "wt")
	run("git", "branch", "feat/in-worktree")
	run("git", "worktree", "add", wtDir, "feat/in-worktree")
	defer run("git", "worktree", "remove", "--force", wtDir)

	has, path, err := HasActiveWorktree("feat/in-worktree")
	if err != nil {
		t.Fatalf("HasActiveWorktree error: %v", err)
	}
	if !has {
		t.Error("HasActiveWorktree(feat/in-worktree) = false, want true")
	}
	if path == "" {
		t.Error("expected non-empty worktree path")
	}
}

// ---- containsBranch tests ----------------------------------------------------

func TestContainsBranch_Found(t *testing.T) {
	output := "  main\n  * feat/current\n  feat/other\n"
	if !containsBranch(output, "feat/other") {
		t.Error("containsBranch should find feat/other")
	}
}

func TestContainsBranch_NotFound(t *testing.T) {
	output := "  main\n  feat/current\n"
	if containsBranch(output, "feat/missing") {
		t.Error("containsBranch should not find feat/missing")
	}
}

// ---- WriteTombstone / GCTombstones tests ------------------------------------

func TestWriteTombstone_WritesRef(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	// Get current HEAD SHA.
	out, _, _ := runHelper("git", "rev-parse", "HEAD")
	sha := strings.TrimSpace(out)

	ref, err := WriteTombstone("feat/old-branch", sha)
	if err != nil {
		t.Fatalf("WriteTombstone error: %v", err)
	}
	if ref == "" {
		t.Fatal("expected non-empty ref name")
	}

	// Verify ref exists.
	verifyOut, _, verifyErr := runHelper("git", "show-ref", ref)
	if verifyErr != nil {
		t.Fatalf("ref %s not found after WriteTombstone: %v", ref, verifyErr)
	}
	if !strings.Contains(verifyOut, sha) {
		t.Errorf("tombstone ref %s doesn't point to SHA %s: %s", ref, sha, verifyOut)
	}
}

func TestGCTombstones_RemovesOldRefs(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	// Write 3 tombstones using the current HEAD SHA.
	out, _, _ := runHelper("git", "rev-parse", "HEAD")
	sha := strings.TrimSpace(out)

	for _, branch := range []string{"old-branch-1", "old-branch-2", "old-branch-3"} {
		if _, err := WriteTombstone(branch, sha); err != nil {
			t.Fatalf("WriteTombstone(%s): %v", branch, err)
		}
	}

	// Verify refs exist.
	refsOut, _, _ := runHelper("git", "for-each-ref", "--format=%(refname)", "refs/tombstones/")
	if !strings.Contains(refsOut, "old-branch-1") {
		t.Fatalf("tombstone not written; for-each-ref output: %s", refsOut)
	}

	// GC with "now" advanced 8 days into the future — all tombstones should be pruned.
	futureNow := time.Now().Add(8 * 24 * time.Hour)
	pruned, err := GCTombstonesAt(7*24*time.Hour, futureNow)
	if err != nil {
		t.Fatalf("GCTombstonesAt error: %v", err)
	}
	if len(pruned) < 3 {
		t.Errorf("expected at least 3 pruned refs, got %d: %v", len(pruned), pruned)
	}

	// Verify refs are gone.
	afterOut, _, _ := runHelper("git", "for-each-ref", "--format=%(refname)", "refs/tombstones/")
	for _, branch := range []string{"old-branch-1", "old-branch-2", "old-branch-3"} {
		refName := "refs/tombstones/" + branch
		if strings.Contains(afterOut, refName) {
			t.Errorf("expected %s to be GC'd but it still exists", refName)
		}
	}
}

func TestGCTombstones_KeepsRecentRefs(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	out, _, _ := runHelper("git", "rev-parse", "HEAD")
	sha := strings.TrimSpace(out)

	if _, err := WriteTombstone("recent-branch", sha); err != nil {
		t.Fatalf("WriteTombstone: %v", err)
	}

	// GC with now = now (0 seconds in future) — ref is fresh, should be kept.
	pruned, err := GCTombstonesAt(7*24*time.Hour, time.Now())
	if err != nil {
		t.Fatalf("GCTombstonesAt error: %v", err)
	}
	for _, p := range pruned {
		if strings.Contains(p, "recent-branch") {
			t.Errorf("recent-branch tombstone should not have been pruned: %v", pruned)
		}
	}
}

// ---- PruneBranch guard tests (unit — no real git deletions) ------------------

func TestPruneBranch_SkipsProtected(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	d := PruneBranch("main", PruneOpts{Base: "main", Apply: false})
	if !d.Skipped {
		t.Error("PruneBranch(main) should be skipped as protected")
	}
	if d.SkipReason != SkipProtected {
		t.Errorf("expected SkipProtected, got %q", d.SkipReason)
	}
}

func TestPruneBranch_SkipsCurrentHEAD(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()
	// Current HEAD is "main" — but main is also protected. Use a non-protected
	// name that is the current HEAD via checkout.

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	// Create and checkout a non-protected branch.
	run("git", "checkout", "-b", "feat/currently-checked-out")

	d := PruneBranch("feat/currently-checked-out", PruneOpts{Base: "main", Apply: false})
	if !d.Skipped {
		t.Error("PruneBranch should skip the currently checked-out branch")
	}
	if d.SkipReason != SkipCurrentHEAD {
		t.Errorf("expected SkipCurrentHEAD, got %q", d.SkipReason)
	}
}

func TestPruneBranch_SkipsUnmerged(t *testing.T) {
	dir, restore := makeGitRepo(t)
	defer restore()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	// Create an unmerged branch.
	run("git", "checkout", "-b", "feat/unmerged-branch")
	os.WriteFile(filepath.Join(dir, "unmerged.txt"), []byte("work\n"), 0644)
	run("git", "add", ".")
	run("git", "commit", "-m", "unmerged work")
	run("git", "checkout", "main")

	d := PruneBranch("feat/unmerged-branch", PruneOpts{Base: "main", Apply: false})
	if !d.Skipped {
		t.Error("PruneBranch should skip unmerged branch")
	}
	if d.SkipReason != SkipUnmerged {
		t.Errorf("expected SkipUnmerged, got %q", d.SkipReason)
	}
}

func TestPruneBranch_DryRunMergedViaGit(t *testing.T) {
	dir, restore := makeGitRepo(t)
	defer restore()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	// Create a branch and merge it.
	run("git", "checkout", "-b", "feat/merged-feature")
	os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("feat\n"), 0644)
	run("git", "add", ".")
	run("git", "commit", "-m", "feature work")
	run("git", "checkout", "main")
	run("git", "merge", "--no-ff", "feat/merged-feature", "-m", "merge feature")

	d := PruneBranch("feat/merged-feature", PruneOpts{Base: "main", Apply: false})
	if d.Skipped {
		t.Errorf("merged branch should not be skipped in dry-run, got skip reason: %q (%s)", d.SkipReason, d.SkipDetail)
	}
	if !d.Merged {
		t.Error("expected d.Merged = true for merged branch")
	}
	if d.MergeSource != MergeSourceGit {
		t.Errorf("expected MergeSourceGit, got %q", d.MergeSource)
	}
	if d.Deleted {
		t.Error("dry-run should not delete the branch")
	}
}

func TestPruneBranch_ApplyMergedViaGit_DeletesAndTombstones(t *testing.T) {
	dir, restore := makeGitRepo(t)
	defer restore()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	// Create and merge a branch.
	run("git", "checkout", "-b", "feat/to-delete")
	os.WriteFile(filepath.Join(dir, "del.txt"), []byte("del\n"), 0644)
	run("git", "add", ".")
	run("git", "commit", "-m", "work to delete")
	run("git", "checkout", "main")
	run("git", "merge", "--no-ff", "feat/to-delete", "-m", "merge to-delete")

	d := PruneBranch("feat/to-delete", PruneOpts{Base: "main", Apply: true})
	if !d.Deleted {
		t.Errorf("expected branch to be deleted; decision: %+v", d)
	}
	if d.Tombstone == "" {
		t.Error("expected tombstone ref to be written")
	}
	if d.Error != "" {
		t.Errorf("unexpected error: %s", d.Error)
	}

	// Verify local branch is gone.
	out, _, err := runHelper("git", "branch", "--list", "feat/to-delete")
	if err == nil && strings.TrimSpace(out) != "" {
		t.Error("local branch feat/to-delete still exists after apply")
	}
}

func TestPruneBranch_SkipsOpenPlanRef(t *testing.T) {
	dir, restore := makeGitRepo(t)
	defer restore()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	// Create and merge the branch.
	run("git", "checkout", "-b", "feat/has-open-plan")
	os.WriteFile(filepath.Join(dir, "plan.txt"), []byte("plan\n"), 0644)
	run("git", "add", ".")
	run("git", "commit", "-m", "plan work")
	run("git", "checkout", "main")
	run("git", "merge", "--no-ff", "feat/has-open-plan", "-m", "merge plan")

	// Create open plan referencing the branch.
	planDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planDir, 0755)
	content := "---\nbranch: feat/has-open-plan\ntitle: test plan\n---\n# Plan content\n"
	os.WriteFile(filepath.Join(planDir, "P-0099-test-approved.md"), []byte(content), 0644)

	d := PruneBranch("feat/has-open-plan", PruneOpts{Base: "main", Apply: false})
	if !d.Skipped {
		t.Error("PruneBranch should skip branch with open plan reference")
	}
	if d.SkipReason != SkipOpenPlan {
		t.Errorf("expected SkipOpenPlan, got %q", d.SkipReason)
	}
}

// ---- extractJSONField tests --------------------------------------------------

func TestExtractJSONField_Found(t *testing.T) {
	json := `[{"mergeCommit":{"oid":"abc123def456"},"state":"MERGED"}]`
	got := extractJSONField(json, "oid")
	if got != "abc123def456" {
		t.Errorf("extractJSONField: got %q, want %q", got, "abc123def456")
	}
}

func TestExtractJSONField_NotFound(t *testing.T) {
	json := `[{"state":"MERGED"}]`
	got := extractJSONField(json, "oid")
	if got != "" {
		t.Errorf("extractJSONField: got %q, want empty", got)
	}
}

// ---- planReferencingBranch tests ---------------------------------------------

func TestPlanReferencingBranch_Match(t *testing.T) {
	f, _ := os.CreateTemp(t.TempDir(), "plan*.md")
	content := "---\nbranch: feat/my-branch\ntitle: test\n---\n# body\n"
	f.WriteString(content)
	f.Close()

	if !planReferencingBranch(f.Name(), "feat/my-branch") {
		t.Error("planReferencingBranch should return true on match")
	}
}

func TestPlanReferencingBranch_NoMatch(t *testing.T) {
	f, _ := os.CreateTemp(t.TempDir(), "plan*.md")
	content := "---\nbranch: feat/other\ntitle: test\n---\n# body\n"
	f.WriteString(content)
	f.Close()

	if planReferencingBranch(f.Name(), "feat/my-branch") {
		t.Error("planReferencingBranch should return false when branch doesn't match")
	}
}

func TestPlanReferencingBranch_NoFrontmatter(t *testing.T) {
	f, _ := os.CreateTemp(t.TempDir(), "plan*.md")
	f.WriteString("# Just a plain document\nno frontmatter here\n")
	f.Close()

	if planReferencingBranch(f.Name(), "feat/my-branch") {
		t.Error("planReferencingBranch should return false when no frontmatter")
	}
}

// ---- PromoteJustCompleted scenario (regression test) ---------------------------

func TestPruneBranch_SkipsPromoteSourceBranch(t *testing.T) {
	// Regression test for Phase 8: verify that prune skips the source branch
	// of a just-completed promote/merge flow via the open plan guard.
	//
	// Scenario: /promote merged a branch and created a plan for it. Then prune
	// is auto-called. It should skip the branch (open plan guard).
	dir, restore := makeGitRepo(t)
	defer restore()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	// Step 1: create a merge branch with work
	run("git", "checkout", "-b", "rel/v1.0-release")
	os.WriteFile(filepath.Join(dir, "release.txt"), []byte("v1.0 release\n"), 0644)
	run("git", "add", ".")
	run("git", "commit", "-m", "v1.0 release notes")

	// Step 2: merge it to main (simulating promote)
	run("git", "checkout", "main")
	run("git", "merge", "--no-ff", "rel/v1.0-release", "-m", "release v1.0")

	// Step 3: Create an open plan that references the just-promoted branch.
	// This simulates the state after /promote completes: the branch is merged,
	// but there's still an associated plan in .planning/ referencing it.
	planDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planDir, 0755)
	content := "---\nbranch: rel/v1.0-release\ntitle: v1.0 release plan\n---\n# Release Plan\n"
	os.WriteFile(filepath.Join(planDir, "P-0888-v1-release-approved.md"), []byte(content), 0644)

	// Now prune the release branch — it should be skipped due to the open plan ref.
	// This guards against accidentally deleting a branch that still has an active plan.
	// The order of checks in PruneBranch is: protected → current HEAD → open plan.
	// Since rel/v1.0-release is neither protected nor current HEAD, it will be checked
	// for open plan and skipped due to that guard.
	d := PruneBranch("rel/v1.0-release", PruneOpts{Base: "main", Apply: false})
	if !d.Skipped {
		t.Errorf("release branch should be skipped due to open plan ref, but got: %+v", d)
	}
	if d.SkipReason != SkipOpenPlan {
		t.Errorf("expected SkipOpenPlan, got %q", d.SkipReason)
	}
}

// ---- WriteLog smoke test -----------------------------------------------------

func TestWriteLog_DoesNotPanic(t *testing.T) {
	// Override home to a temp dir so we don't write to real ~/.claude/logs.
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	// Should not panic.
	WriteLog("my-repo", "feat/branch", "deleted", "merged-via-git", "refs/tombstones/feat-branch")

	logPath := filepath.Join(tmpHome, ".claude", "logs", "branch-prune.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("log file not created: %v", err)
	}
	if !strings.Contains(string(data), "feat/branch") {
		t.Errorf("log file missing branch name; content: %s", string(data))
	}
}

// ---- helpers -----------------------------------------------------------------

// runHelper executes a git command in the current working directory.
func runHelper(args ...string) (string, string, error) {
	return gitpkg.Run(args...)
}

// ---- extractJSONField tests (PR #278 review request) ------------------------

func TestExtractJSONField_HappyPath(t *testing.T) {
	in := `{"mergeCommit":{"oid":"abc123def456"}}`
	got := extractJSONField(in, "oid")
	if got != "abc123def456" {
		t.Errorf("extractJSONField got %q, want %q", got, "abc123def456")
	}
}

func TestExtractJSONField_MissingKey(t *testing.T) {
	in := `{"state":"merged"}`
	got := extractJSONField(in, "oid")
	if got != "" {
		t.Errorf("missing key should return empty string, got %q", got)
	}
}

func TestExtractJSONField_EmptyValue(t *testing.T) {
	in := `{"oid":""}`
	got := extractJSONField(in, "oid")
	if got != "" {
		t.Errorf("empty value should return empty string, got %q", got)
	}
}

func TestExtractJSONField_NoQuotedCloser(t *testing.T) {
	// Pathological input — opening quote then EOF. Function should not panic.
	in := `{"oid":"abc`
	got := extractJSONField(in, "oid")
	if got != "" {
		t.Errorf("malformed input should return empty string, got %q", got)
	}
}

// ---- HasActiveCommand tests --------------------------------------------------

func TestHasActiveCommand_NoMarker_NoBypass(t *testing.T) {
	// Override TMPDIR to an isolated dir without any agent-audit markers.
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	// Ensure bypass is OFF — we're testing the real detection path.
	t.Setenv("BRAVROS_BRANCH_PRUNE_BYPASS_ACTIVE_CMD", "")
	// /tmp and /private/tmp may have real markers from a live session, so this
	// test is meaningful only when those are also clean; on macOS the live
	// session marker lives in $TMPDIR (which we just isolated) so /tmp paths
	// are usually empty. If a CI environment has a marker in /tmp, this test
	// will spuriously report active — acceptable test-environment fragility.
	if active, _ := HasActiveCommand(); active {
		t.Skip("system /tmp contains an unrelated active-command marker — skipping")
	}
}

func TestHasActiveCommand_BypassEnv(t *testing.T) {
	// Place a marker, then enable bypass — bypass should win.
	tmp := t.TempDir()
	markerDir := filepath.Join(tmp, "agent-audit-test123")
	if err := os.MkdirAll(markerDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(markerDir, "active-command"), []byte("test"), 0644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	t.Setenv("TMPDIR", tmp)
	t.Setenv("BRAVROS_BRANCH_PRUNE_BYPASS_ACTIVE_CMD", "1")

	active, markerPath := HasActiveCommand()
	if active {
		t.Errorf("bypass env var should suppress detection, got active=%v markerPath=%q", active, markerPath)
	}
}

func TestHasActiveCommand_MarkerPresent(t *testing.T) {
	// Marker present, bypass OFF — should detect.
	tmp := t.TempDir()
	markerDir := filepath.Join(tmp, "agent-audit-detect-me")
	if err := os.MkdirAll(markerDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	markerPath := filepath.Join(markerDir, "active-command")
	if err := os.WriteFile(markerPath, []byte("plan-approved"), 0644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	t.Setenv("TMPDIR", tmp)
	t.Setenv("BRAVROS_BRANCH_PRUNE_BYPASS_ACTIVE_CMD", "")
	// HOME isolated so writeLog doesn't touch real ~/.claude.
	t.Setenv("HOME", t.TempDir())

	active, hitPath := HasActiveCommand()
	if !active {
		t.Errorf("marker at %q should be detected, got active=false", markerPath)
	}
	if hitPath == "" {
		t.Errorf("detection should return marker path, got empty")
	}
}

// ---- planReferencingBranch tests for the start==0 strictness fix ------------

func TestPlanReferencingBranch_HappyPath(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "P-0099-feat-test-approved.md")
	content := "---\nbranch: feat/wired\ntitle: test plan\n---\n# Body\n"
	if err := os.WriteFile(plan, []byte(content), 0644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	if !planReferencingBranch(plan, "feat/wired") {
		t.Errorf("expected to find branch reference, returned false")
	}
}

func TestPlanReferencingBranch_NoFrontmatter_NoMatch(t *testing.T) {
	// File with `---` mid-document (inside a code block) but NO real frontmatter.
	// Before the fix, this triggered a false positive when "branch: <name>" appeared
	// elsewhere in the file. After the fix, frontmatter must start at byte 0.
	dir := t.TempDir()
	plan := filepath.Join(dir, "P-0100-feat-test-approved.md")
	content := "# Body before frontmatter\n```\n---\nbranch: feat/wired\n---\n```\n"
	if err := os.WriteFile(plan, []byte(content), 0644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	if planReferencingBranch(plan, "feat/wired") {
		t.Errorf("mid-file --- should NOT count as frontmatter; expected false")
	}
}

func TestPlanReferencingBranch_CRLF(t *testing.T) {
	// Same case but with Windows line endings.
	dir := t.TempDir()
	plan := filepath.Join(dir, "P-0101-feat-test-approved.md")
	content := "---\r\nbranch: feat/wired\r\ntitle: test\r\n---\r\nBody\r\n"
	if err := os.WriteFile(plan, []byte(content), 0644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	if !planReferencingBranch(plan, "feat/wired") {
		t.Errorf("CRLF frontmatter should match, returned false")
	}
}

func TestPlanReferencingBranch_BOM(t *testing.T) {
	// File with UTF-8 BOM prefix — common from Windows editors — should still parse.
	dir := t.TempDir()
	plan := filepath.Join(dir, "P-0102-feat-test-approved.md")
	bom := "\uFEFF"
	content := bom + "---\nbranch: feat/wired\n---\n"
	if err := os.WriteFile(plan, []byte(content), 0644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	if !planReferencingBranch(plan, "feat/wired") {
		t.Errorf("BOM-prefixed frontmatter should still match, returned false")
	}
}

// ---- ls-remote pre-check tests (B-0309 / P-0173 Phase 4) --------------------

// makeGitRepoWithRemote creates a local git repo wired to a bare "remote" repo
// in a sibling temp dir. Returns (localDir, remoteDir, restore).
// The local repo has "main" as HEAD and a fresh initial commit.
func makeGitRepoWithRemote(t *testing.T) (localDir, remoteDir string, restore func()) {
	t.Helper()
	t.Setenv("BRAVROS_BRANCH_PRUNE_BYPASS_ACTIVE_CMD", "1")
	t.Setenv("HOME", t.TempDir())

	remoteDir = t.TempDir()
	localDir = t.TempDir()

	runIn := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup cmd %v in %s: %v\n%s", args, dir, err, out)
		}
	}

	// Bare remote.
	runIn(remoteDir, "git", "init", "--bare")

	// Local clone.
	runIn(localDir, "git", "init")
	runIn(localDir, "git", "config", "user.email", "test@test.com")
	runIn(localDir, "git", "config", "user.name", "Test")
	runIn(localDir, "git", "checkout", "-b", "main")
	runIn(localDir, "git", "remote", "add", "origin", remoteDir)

	// Initial commit + push so remote has main.
	f := filepath.Join(localDir, "README.md")
	os.WriteFile(f, []byte("# test\n"), 0644)
	runIn(localDir, "git", "add", ".")
	runIn(localDir, "git", "commit", "-m", "initial")
	runIn(localDir, "git", "push", "-u", "origin", "main")

	orig, _ := os.Getwd()
	os.Chdir(localDir)
	return localDir, remoteDir, func() { os.Chdir(orig) }
}

// TestPruneBranch_AlreadyDeletedRemoteNoError verifies the end-to-end behavior:
// when a branch is merged and the server already deleted it (server-side auto-delete
// after PR merge), PruneBranch must NOT return an error. The two-part fix
// (git remote prune at start + ls-remote pre-check) both contribute to this:
// on a reachable remote, remote prune clears the stale tracking ref so HasRemote
// becomes false and no push is attempted; on an unreachable remote, the stale ref
// remains but ls-remote exit-2 intercepts before the failing push --delete.
//
// This test covers the reachable-remote path (the dominant case).
func TestPruneBranch_AlreadyDeletedRemoteNoError(t *testing.T) {
	localDir, remoteDir, restore := makeGitRepoWithRemote(t)
	defer restore()

	runIn := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v in %s: %v\n%s", args, dir, err, out)
		}
	}

	// Create a branch locally, commit, push to remote, merge into main.
	runIn(localDir, "git", "checkout", "-b", "feat/already-gone-remote")
	os.WriteFile(filepath.Join(localDir, "gone.txt"), []byte("gone\n"), 0644)
	runIn(localDir, "git", "add", ".")
	runIn(localDir, "git", "commit", "-m", "feature work")
	runIn(localDir, "git", "push", "origin", "feat/already-gone-remote")
	runIn(localDir, "git", "checkout", "main")
	runIn(localDir, "git", "merge", "--no-ff", "feat/already-gone-remote", "-m", "merge feature")
	runIn(localDir, "git", "push", "origin", "main")

	// Simulate GitHub's auto-delete-branch: delete from the bare remote directly,
	// WITHOUT running `git remote prune` locally first (stale tracking ref remains).
	runIn(remoteDir, "git", "branch", "-D", "feat/already-gone-remote")

	// Verify the local tracking ref is stale (HasRemote would be true before prune).
	_, _, showRefErr := gitpkg.Run("git", "show-ref", "--verify", "--quiet", "refs/remotes/origin/feat/already-gone-remote")
	if showRefErr != nil {
		t.Skip("precondition: stale tracking ref not present — cannot exercise remote-prune path")
	}

	d := PruneBranch("feat/already-gone-remote", PruneOpts{Base: "main", Apply: true, RepoName: "test-repo"})

	// Core assertion: no error, regardless of which internal mechanism handled it.
	if d.Error != "" {
		t.Errorf("PruneBranch should not return an error when remote branch is already gone; got: %s", d.Error)
	}
}

// TestPruneBranch_LsRemoteAlreadyGoneIsSkip directly exercises the ls-remote
// pre-check path by setting up the stale-tracking-ref scenario and preventing
// git remote prune from clearing it (pointing the remote to an unreachable path
// after push so prune cannot talk to origin). ls-remote is configured to speak
// to the original bare remote (still reachable via the local path) via a second
// remote alias, but the canonical "origin" points nowhere — so remote prune
// fails, tracking ref stays, but the ls-remote on the real path returns exit 2.
//
// Because this test requires controlling two different remote URLs within the same
// git invocation, we test the isRemoteAlreadyGone helper directly instead.
func TestIsRemoteAlreadyGone_ExitCode2(t *testing.T) {
	_, remoteDir, restore := makeGitRepoWithRemote(t)
	defer restore()

	// Branch does not exist on the remote — ls-remote should return exit 2.
	gone := isRemoteRefGone("refs/heads/feat/nonexistent-branch-xyz", remoteDir)
	if !gone {
		t.Error("isRemoteRefGone should return true for a non-existent ref (exit 2)")
	}
}

func TestIsRemoteAlreadyGone_ExitCode0(t *testing.T) {
	_, remoteDir, restore := makeGitRepoWithRemote(t)
	defer restore()

	// "main" was pushed to remote during setup — ls-remote should return exit 0.
	gone := isRemoteRefGone("refs/heads/main", remoteDir)
	if gone {
		t.Error("isRemoteRefGone should return false when the ref exists on remote")
	}
}

func TestIsRemoteAlreadyGone_Unreachable(t *testing.T) {
	// An unreachable remote (bad path) should return false — we can't confirm the
	// ref is gone, so we must NOT classify it as already-gone.
	gone := isRemoteRefGone("refs/heads/main", "/nonexistent/path/to/remote.git")
	if gone {
		t.Error("isRemoteRefGone should return false (not gone) when remote is unreachable")
	}
}

// TestPruneBranch_GenuinePushFailureIsError verifies that a genuine push failure
// (server rejects the delete for a reason other than "ref doesn't exist") is
// still classified as [ERROR] and not silently swallowed.
//
// We use receive.denyDeletes on the bare remote: ls-remote returns exit 0
// (ref present), but push --delete is rejected by the server.
func TestPruneBranch_GenuinePushFailureIsError(t *testing.T) {
	localDir, remoteDir, restore := makeGitRepoWithRemote(t)
	defer restore()

	runIn := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v in %s: %v\n%s", args, dir, err, out)
		}
	}

	// Create a remote-only branch by pushing but NOT having a local branch.
	// (Use a helper local branch then delete it locally after push.)
	runIn(localDir, "git", "checkout", "-b", "feat/deny-delete")
	os.WriteFile(filepath.Join(localDir, "deny.txt"), []byte("deny\n"), 0644)
	runIn(localDir, "git", "add", ".")
	runIn(localDir, "git", "commit", "-m", "deny delete work")
	runIn(localDir, "git", "push", "origin", "feat/deny-delete")
	runIn(localDir, "git", "checkout", "main")
	runIn(localDir, "git", "merge", "--no-ff", "feat/deny-delete", "-m", "merge deny-delete")
	runIn(localDir, "git", "push", "origin", "main")
	// Delete the local branch so the prune sees it as remote-only.
	runIn(localDir, "git", "branch", "-D", "feat/deny-delete")

	// Configure the bare remote to refuse deletes AFTER the local branch is gone
	// so this is a pure remote-only branch where the push failure is hard/fatal.
	runIn(remoteDir, "git", "config", "receive.denyDeletes", "true")

	d := PruneBranch("feat/deny-delete", PruneOpts{Base: "main", Apply: true, RepoName: "test-repo"})

	// The branch is remote-only. ls-remote returns exit 0 (ref present).
	// push --delete is rejected → must set d.Error (not silently skip).
	if d.Error == "" {
		t.Errorf("expected error for genuine push failure (receive.denyDeletes), got none; decision=%+v", d)
	}
	if d.SkipReason == SkipRemoteAlreadyGone {
		t.Errorf("genuine push failure should NOT be classified as SkipRemoteAlreadyGone")
	}
}

// ---- PruneBranch worktree is OFF-LIMITS (Apply mode) ------------------------

// A merged branch that still has an active worktree must be SKIPPED even in --apply
// mode: prune-merged never removes worktrees or deletes a worktree-backed branch.
// Worktree lifecycle is owned solely by the /worktree skill. The remote ref is pruned
// on a later pass, once /worktree destroy has removed the worktree + local branch.
func TestPruneBranch_Apply_SkipsAndPreservesWorktree(t *testing.T) {
	dir, restore := makeGitRepo(t)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %v\n%s", args, err, out)
		}
	}

	// Create a feature branch with a commit, merge it into main, then put a worktree on it.
	run("git", "checkout", "-b", "feat/merged-wt")
	if err := os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatalf("write feat: %v", err)
	}
	run("git", "add", "feat.txt")
	run("git", "commit", "-m", "feat work")
	run("git", "checkout", "main")
	run("git", "merge", "--no-ff", "feat/merged-wt", "-m", "merge feat")
	wtPath := filepath.Join(dir, "..", "wt-merged")
	run("git", "worktree", "add", wtPath, "feat/merged-wt")
	t.Cleanup(func() {
		_, _ = runOutputIn(".", "git", "worktree", "remove", "--force", wtPath)
		os.RemoveAll(wtPath)
	})

	// Apply prune must SKIP (worktree is off-limits) — even though the branch is merged.
	d := PruneBranch("feat/merged-wt", PruneOpts{Base: "main", Apply: true, RepoName: "test-repo"})

	if !d.Skipped {
		t.Fatalf("expected branch skipped (active worktree), got Deleted=%v", d.Deleted)
	}
	if d.SkipReason != SkipWorktree {
		t.Errorf("expected SkipReason=%q, got %q", SkipWorktree, d.SkipReason)
	}
	if d.Deleted {
		t.Errorf("worktree-backed branch must NOT be deleted, got Deleted=true: %+v", d)
	}
	// Worktree must still exist.
	out, _, _ := gitpkg.Run("git", "worktree", "list", "--porcelain")
	if !strings.Contains(out, wtPath) {
		t.Errorf("apply must NOT remove worktree, but %q is gone; output: %s", wtPath, out)
	}
	// Local branch must still exist.
	branchOut, _, _ := gitpkg.Run("git", "branch", "--list", "feat/merged-wt")
	if strings.TrimSpace(branchOut) == "" {
		t.Errorf("worktree-backed branch must survive apply, but feat/merged-wt is gone")
	}
}

func TestPruneBranch_DryRun_DoesNotTouchWorktree(t *testing.T) {
	dir, restore := makeGitRepo(t)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %v\n%s", args, err, out)
		}
	}
	run("git", "branch", "feat/dryrun-wt")
	wtPath := filepath.Join(dir, "..", "wt-dryrun")
	run("git", "worktree", "add", wtPath, "feat/dryrun-wt")

	// Dry-run on a worktree-active branch — should skip with helpful detail.
	d := PruneBranch("feat/dryrun-wt", PruneOpts{Base: "main", Apply: false, RepoName: "test-repo"})

	if !d.Skipped {
		t.Errorf("dry-run with active worktree should skip, got: %+v", d)
	}
	if d.SkipReason != SkipWorktree {
		t.Errorf("expected SkipWorktree, got %q", d.SkipReason)
	}
	// Worktree must still exist after dry-run.
	out, _, _ := gitpkg.Run("git", "worktree", "list", "--porcelain")
	if !strings.Contains(out, wtPath) {
		t.Errorf("dry-run should not remove worktree, but %q is gone; output: %s", wtPath, out)
	}

	// Follow-up lifecycle (the OTHER half of the OFF-LIMITS contract) — once
	// `/worktree destroy` removes the worktree + local ref, the branch is
	// remote-only and Guard 6 no longer fires, so the next prune pass cleans
	// up the merged remote ref. That path is covered by
	// TestPruneBranch_RemoteOnlyMerged.
}

// ---- PruneBranch active-command guard ----------------------------------------

func TestPruneBranch_SkipsOnActiveCommandMarker(t *testing.T) {
	dir, restore := makeGitRepo(t)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	// Create a real branch so the new "branch exists" guard doesn't short-circuit
	// with SkipUnmerged before we get to the active-command check.
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %v\n%s", args, err, out)
		}
	}
	run("git", "branch", "feat/active-cmd-test")

	// makeGitRepo set the bypass — flip it OFF for this test, then plant a marker.
	t.Setenv("BRAVROS_BRANCH_PRUNE_BYPASS_ACTIVE_CMD", "")
	tmp := t.TempDir()
	markerDir := filepath.Join(tmp, "agent-audit-pruneguard")
	if err := os.MkdirAll(markerDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(markerDir, "active-command"), []byte("plan-approved"), 0644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	t.Setenv("TMPDIR", tmp)

	d := PruneBranch("feat/active-cmd-test", PruneOpts{Base: "main", Apply: true, RepoName: "test-repo"})
	if !d.Skipped {
		t.Fatalf("expected skip due to active-command marker, got: %+v", d)
	}
	if d.SkipReason != SkipActiveCommand {
		t.Errorf("expected SkipActiveCommand, got %q", d.SkipReason)
	}
}

// ---- isMergedViaGit local-only branch regression -----------------------------

// TestIsMerged_LocalOnlyBranchWithRemote: regression for the early-return bug
// where a local-only branch that was fully merged into base returned (false, "")
// because `git branch -r --merged origin/<base>` succeeded with err == nil and
// returned only remote refs. Real-world repro: cc/trusting-driscoll-9bd5e9 after
// the P-0161 promote — fully merged tip, no remote counterpart, prune skipped it.
func TestIsMerged_LocalOnlyBranchWithRemote(t *testing.T) {
	dir, restore := makeGitRepo(t)
	defer restore()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	// Set up a bare repo to act as origin, then add it as a remote so the
	// `git branch -r --merged origin/<base>` call returns successfully (the
	// pre-fix code's failure path).
	bareDir := t.TempDir()
	bareRepo := filepath.Join(bareDir, "origin.git")
	if err := exec.Command("git", "init", "--bare", bareRepo).Run(); err != nil {
		t.Fatalf("bare init: %v", err)
	}
	run("git", "remote", "add", "origin", bareRepo)
	run("git", "push", "origin", "main")
	// Refresh remote-tracking refs.
	run("git", "fetch", "origin")

	// Create a feature branch locally, merge it into main, but DO NOT push it.
	// This is the cc/trusting-driscoll-9bd5e9 shape.
	run("git", "checkout", "-b", "feat/local-only-merged")
	if err := os.WriteFile(filepath.Join(dir, "local.txt"), []byte("work\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "local work")
	run("git", "checkout", "main")
	run("git", "merge", "--no-ff", "feat/local-only-merged", "-m", "merge local")

	// The branch tip is now an ancestor of main locally. origin/main doesn't
	// have this work yet (we didn't push after the merge). isMergedViaGit
	// should still detect the merge via the LOCAL check, not just remote.
	merged, src, err := IsMerged("feat/local-only-merged", "main")
	if err != nil {
		t.Fatalf("IsMerged error: %v", err)
	}
	if !merged {
		t.Errorf("local-only merged branch should be detected as merged; got merged=false")
	}
	if src != MergeSourceGit {
		t.Errorf("expected MergeSourceGit (came through local check), got %q", src)
	}
}

func TestPruneBranch_DryRun_DetectsLocalOnlyMerge(t *testing.T) {
	// End-to-end: same shape via PruneBranch — local-only merged branch should
	// surface as a CANDIDATE in dry-run, not SkipUnmerged.
	dir, restore := makeGitRepo(t)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	bareDir := t.TempDir()
	bareRepo := filepath.Join(bareDir, "origin.git")
	if err := exec.Command("git", "init", "--bare", bareRepo).Run(); err != nil {
		t.Fatalf("bare init: %v", err)
	}
	run("git", "remote", "add", "origin", bareRepo)
	run("git", "push", "origin", "main")
	run("git", "fetch", "origin")

	run("git", "checkout", "-b", "feat/local-only-prune")
	if err := os.WriteFile(filepath.Join(dir, "local.txt"), []byte("work\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "local")
	run("git", "checkout", "main")
	run("git", "merge", "--no-ff", "feat/local-only-prune", "-m", "merge")

	d := PruneBranch("feat/local-only-prune", PruneOpts{Base: "main", Apply: false, RepoName: "test-repo"})
	if d.Skipped {
		t.Fatalf("local-only merged branch should be a CANDIDATE, got skip=%q (%s)", d.SkipReason, d.SkipDetail)
	}
	if !d.Merged {
		t.Errorf("expected Merged=true")
	}
}

// ---- Remote-only branch handling (the v3.46.1 follow-up gap) ----------------

// setupRepoWithRemote initializes a test repo with a bare-repo "origin" remote,
// pushes main, and returns (dir, remotePath, restoreFunc). Subsequent test setup
// can push more branches to the bare remote to simulate the merged-PR-no-delete
// state observed in real usage.
func setupRepoWithRemote(t *testing.T) (string, string, func()) {
	t.Helper()
	dir, restore := makeGitRepo(t)
	t.Setenv("HOME", t.TempDir())

	bareDir := t.TempDir()
	bareRepo := filepath.Join(bareDir, "origin.git")
	if err := exec.Command("git", "init", "--bare", bareRepo).Run(); err != nil {
		t.Fatalf("bare init: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %v\n%s", args, err, out)
		}
	}
	run("git", "remote", "add", "origin", bareRepo)
	run("git", "push", "origin", "main")
	run("git", "fetch", "origin")
	return dir, bareRepo, restore
}

// TestListAllBranches_IncludesRemoteOnly: ListAllBranches must return branches
// that exist only on the remote (typical after `gh pr merge` without --delete-branch).
func TestListAllBranches_IncludesRemoteOnly(t *testing.T) {
	dir, _, restore := setupRepoWithRemote(t)
	defer restore()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %v\n%s", args, err, out)
		}
	}

	// Create a feature branch, push it, then delete it locally — simulates
	// a workspace where the merged PR's feature branch was cleaned up locally
	// but the remote ref still exists (the gh pr merge --delete-branch was
	// skipped, or the branch was never re-fetched).
	run("git", "checkout", "-b", "feat/remote-only-test")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "feat")
	run("git", "push", "origin", "feat/remote-only-test")
	run("git", "checkout", "main")
	run("git", "branch", "-D", "feat/remote-only-test")

	got, err := ListAllBranches()
	if err != nil {
		t.Fatalf("ListAllBranches: %v", err)
	}
	found := false
	for _, b := range got {
		if b == "feat/remote-only-test" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected remote-only branch in ListAllBranches output, got %v", got)
	}
}

// TestPruneBranch_RemoteOnlyMerged: a remote-only branch whose tip is merged
// into base should be a CANDIDATE (not SkipUnmerged) and on --apply should be
// removed from the remote without trying to do a local delete.
func TestPruneBranch_RemoteOnlyMerged(t *testing.T) {
	dir, bareRepo, restore := setupRepoWithRemote(t)
	defer restore()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %v\n%s", args, err, out)
		}
	}

	// Build a feature branch, merge it into main, push both, then delete the
	// local feature ref. State: origin has the branch + main has the merge;
	// locally main has the merge but the feature ref is gone.
	run("git", "checkout", "-b", "feat/remote-merged-1")
	if err := os.WriteFile(filepath.Join(dir, "rm.txt"), []byte("rm\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "feat work")
	run("git", "push", "origin", "feat/remote-merged-1")
	run("git", "checkout", "main")
	run("git", "merge", "--no-ff", "feat/remote-merged-1", "-m", "merge feat")
	run("git", "push", "origin", "main")
	run("git", "branch", "-D", "feat/remote-merged-1")
	run("git", "fetch", "origin", "--prune")

	// Dry-run: should report it as merged candidate, NOT SkipUnmerged.
	d := PruneBranch("feat/remote-merged-1", PruneOpts{Base: "main", Apply: false, RepoName: "test-repo"})
	if d.Skipped {
		t.Fatalf("remote-only merged branch should be CANDIDATE in dry-run, got skip=%q (%s)", d.SkipReason, d.SkipDetail)
	}
	if !d.Merged {
		t.Errorf("expected d.Merged=true, got %+v", d)
	}

	// Apply: should write tombstone + delete from remote. No local-delete call
	// because the local ref doesn't exist.
	d = PruneBranch("feat/remote-merged-1", PruneOpts{Base: "main", Apply: true, RepoName: "test-repo"})
	if !d.Deleted {
		t.Fatalf("apply should mark Deleted=true, got %+v", d)
	}
	if d.Tombstone == "" {
		t.Errorf("expected tombstone ref")
	}

	// Verify the branch is actually gone from the bare repo.
	cmd := exec.Command("git", "ls-remote", "--heads", bareRepo, "feat/remote-merged-1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ls-remote: %v", err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("remote branch still exists after --apply: %q", string(out))
	}

	// And of course it shouldn't have come back locally.
	cmd2 := exec.Command("git", "branch", "--list", "feat/remote-merged-1")
	cmd2.Dir = dir
	out2, _ := cmd2.CombinedOutput()
	if strings.TrimSpace(string(out2)) != "" {
		t.Errorf("local branch shouldn't exist: %q", string(out2))
	}
}

// TestPruneBranch_RemoteOnlyUnmerged: a remote-only branch whose tip is NOT
// merged should be skipped (unmerged), and the remote ref must remain.
func TestPruneBranch_RemoteOnlyUnmerged(t *testing.T) {
	dir, bareRepo, restore := setupRepoWithRemote(t)
	defer restore()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %v\n%s", args, err, out)
		}
	}

	run("git", "checkout", "-b", "feat/remote-unmerged-1")
	if err := os.WriteFile(filepath.Join(dir, "u.txt"), []byte("u\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "unmerged work")
	run("git", "push", "origin", "feat/remote-unmerged-1")
	run("git", "checkout", "main")
	run("git", "branch", "-D", "feat/remote-unmerged-1")
	run("git", "fetch", "origin", "--prune")

	d := PruneBranch("feat/remote-unmerged-1", PruneOpts{Base: "main", Apply: true, RepoName: "test-repo"})
	if !d.Skipped {
		t.Errorf("unmerged remote-only branch should skip, got: %+v", d)
	}
	if d.SkipReason != SkipUnmerged {
		t.Errorf("expected SkipUnmerged, got %q", d.SkipReason)
	}

	// Remote ref MUST still exist.
	cmd := exec.Command("git", "ls-remote", "--heads", bareRepo, "feat/remote-unmerged-1")
	out, _ := cmd.CombinedOutput()
	if strings.TrimSpace(string(out)) == "" {
		t.Errorf("unmerged remote branch should NOT be deleted")
	}
}

// TestPruneBranch_NonExistent: PruneBranch on a name that exists neither
// locally nor on origin should skip cleanly with a clear detail message.
func TestPruneBranch_NonExistent(t *testing.T) {
	_, _, restore := setupRepoWithRemote(t)
	defer restore()

	d := PruneBranch("feat/never-existed-anywhere", PruneOpts{Base: "main", Apply: true, RepoName: "test-repo"})
	if !d.Skipped {
		t.Errorf("non-existent branch should skip, got: %+v", d)
	}
	if !strings.Contains(d.SkipDetail, "does not exist") {
		t.Errorf("expected helpful skip detail for non-existent branch, got %q", d.SkipDetail)
	}
}

// ---- GCReviewStamps tests ----------------------------------------------------

// writeStamp creates a .planning/.review-stamp-<pr>.json file under the current
// working directory (set to a temp repo by makeGitRepo). Returns the path.
func writeStamp(t *testing.T, pr int) string {
	t.Helper()
	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("mkdir .planning: %v", err)
	}
	path := filepath.Join(".planning", ".review-stamp-"+strconv.Itoa(pr)+".json")
	body := `{"pr": ` + strconv.Itoa(pr) + `, "reviewer_verdict": "approved", "bypass": false}`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write stamp %s: %v", path, err)
	}
	return path
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// TestGCReviewStamps_MergedRemoved — a stamp whose PR is MERGED is reaped.
func TestGCReviewStamps_MergedRemoved(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	stamp := writeStamp(t, 123)
	lookup := func(pr int) (string, error) { return "MERGED", nil }

	reaped, err := GCReviewStampsWith(lookup)
	if err != nil {
		t.Fatalf("GCReviewStampsWith error: %v", err)
	}
	if len(reaped) != 1 {
		t.Fatalf("expected 1 reaped stamp, got %d: %v", len(reaped), reaped)
	}
	if fileExists(stamp) {
		t.Errorf("merged-PR stamp %s should have been removed", stamp)
	}
}

// TestGCReviewStamps_ClosedRemoved — a stamp whose PR is CLOSED is reaped.
func TestGCReviewStamps_ClosedRemoved(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	stamp := writeStamp(t, 77)
	lookup := func(pr int) (string, error) { return "CLOSED", nil }

	reaped, err := GCReviewStampsWith(lookup)
	if err != nil {
		t.Fatalf("GCReviewStampsWith error: %v", err)
	}
	if len(reaped) != 1 {
		t.Fatalf("expected 1 reaped stamp, got %d: %v", len(reaped), reaped)
	}
	if fileExists(stamp) {
		t.Errorf("closed-PR stamp %s should have been removed", stamp)
	}
}

// TestGCReviewStamps_OpenKept — a stamp whose PR is still OPEN is kept.
func TestGCReviewStamps_OpenKept(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	stamp := writeStamp(t, 200)
	lookup := func(pr int) (string, error) { return "OPEN", nil }

	reaped, err := GCReviewStampsWith(lookup)
	if err != nil {
		t.Fatalf("GCReviewStampsWith error: %v", err)
	}
	if len(reaped) != 0 {
		t.Fatalf("expected 0 reaped stamps for open PR, got %d: %v", len(reaped), reaped)
	}
	if !fileExists(stamp) {
		t.Errorf("open-PR stamp %s should have been kept", stamp)
	}
}

// TestGCReviewStamps_GhErrorFailClosed — on any gh lookup error (offline,
// unauthenticated, PR not found) the stamp is KEPT (fail-closed on uncertainty).
func TestGCReviewStamps_GhErrorFailClosed(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	stamp := writeStamp(t, 999)
	lookup := func(pr int) (string, error) {
		return "", errors.New("gh offline: dial tcp: lookup api.github.com")
	}

	reaped, err := GCReviewStampsWith(lookup)
	if err != nil {
		t.Fatalf("GCReviewStampsWith error: %v", err)
	}
	if len(reaped) != 0 {
		t.Fatalf("expected 0 reaped stamps on gh error, got %d: %v", len(reaped), reaped)
	}
	if !fileExists(stamp) {
		t.Errorf("stamp %s must be KEPT on gh-error (fail-closed), but it was removed", stamp)
	}
}

// TestGCReviewStamps_MixedStates — a directory with merged/open/closed/errored
// stamps reaps only the merged + closed ones; open + errored are kept.
func TestGCReviewStamps_MixedStates(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	merged := writeStamp(t, 1)
	open := writeStamp(t, 2)
	closed := writeStamp(t, 3)
	errored := writeStamp(t, 4)

	lookup := func(pr int) (string, error) {
		switch pr {
		case 1:
			return "MERGED", nil
		case 2:
			return "OPEN", nil
		case 3:
			return "CLOSED", nil
		default:
			return "", errors.New("gh lookup failed")
		}
	}

	reaped, err := GCReviewStampsWith(lookup)
	if err != nil {
		t.Fatalf("GCReviewStampsWith error: %v", err)
	}
	if len(reaped) != 2 {
		t.Fatalf("expected 2 reaped stamps (merged+closed), got %d: %v", len(reaped), reaped)
	}
	if fileExists(merged) {
		t.Errorf("merged stamp %s should be removed", merged)
	}
	if fileExists(closed) {
		t.Errorf("closed stamp %s should be removed", closed)
	}
	if !fileExists(open) {
		t.Errorf("open stamp %s should be kept", open)
	}
	if !fileExists(errored) {
		t.Errorf("errored stamp %s should be kept (fail-closed)", errored)
	}
}

// TestGCReviewStamps_NoStampsNoError — empty/absent .planning yields no reaps,
// no error.
func TestGCReviewStamps_NoStampsNoError(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	lookup := func(pr int) (string, error) { return "MERGED", nil }
	reaped, err := GCReviewStampsWith(lookup)
	if err != nil {
		t.Fatalf("GCReviewStampsWith error: %v", err)
	}
	if len(reaped) != 0 {
		t.Errorf("expected 0 reaped stamps when none exist, got %d: %v", len(reaped), reaped)
	}
}
