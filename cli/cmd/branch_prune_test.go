package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	branchpkg "github.com/bravros/bravros/cli/internal/branch"
)

// makeBranchPruneTestRepo creates a minimal git repo in a temp dir, changes into it,
// and returns (dir, restoreFunc). Always use defer restore().
//
// Sets KAISSER_BRANCH_PRUNE_BYPASS_ACTIVE_CMD=1 so the active-command guard does not
// fire when tests run inside a live Claude Code session (which maintains the marker).
// `t.Setenv` reverts at test end.
func makeBranchPruneTestRepo(t *testing.T) (string, func()) {
	t.Helper()
	t.Setenv("KAISSER_BRANCH_PRUNE_BYPASS_ACTIVE_CMD", "1")
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup: %v: %v\n%s", args, err, out)
		}
	}

	run("git", "init")
	run("git", "config", "user.email", "test@test.com")
	run("git", "config", "user.name", "Test")
	run("git", "checkout", "-b", "main")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0644)
	run("git", "add", ".")
	run("git", "commit", "-m", "initial")

	orig, _ := os.Getwd()
	os.Chdir(dir)
	return dir, func() { os.Chdir(orig) }
}

// invokeBranchPrune sets package-level flag vars and calls RunE directly,
// returning stdout output. Callers set the flag vars they need; all are
// reset to defaults after the call.
func invokeBranchPrune(t *testing.T, apply, gc bool, base string) (string, error) {
	t.Helper()
	return invokeBranchPruneFull(t, apply, gc, base, false)
}

// invokeBranchPruneFull is the full-arity form, adding --exclude-rejected for the
// rejected-PR tests. Every flag var is reset to its default after the call.
func invokeBranchPruneFull(t *testing.T, apply, gc bool, base string, excludeRejected bool) (string, error) {
	t.Helper()
	// Set flags.
	branchPruneApply = apply
	branchPruneGC = gc
	branchPruneBase = base
	branchPruneExcludeRejected = excludeRejected

	var buf bytes.Buffer
	branchPruneCmd.SetOut(&buf)
	err := branchPruneCmd.RunE(branchPruneCmd, nil)

	// Reset flags.
	branchPruneApply = false
	branchPruneGC = false
	branchPruneBase = ""
	branchPruneExcludeRejected = false

	return buf.String(), err
}

// stubGHPRIndex puts a fake `gh` first on PATH so runBranchPrune's ONE upfront
// PR fetch returns a known index. The temp repos these tests build have no GitHub
// remote, so the real `gh api` call fails and the command degrades to an
// UNAVAILABLE index — which classifies nothing and is exactly the surface these
// tests need to exercise.
//
// The stub answers the two batched calls runBranchPrune makes:
//
//   - repos/{owner}/{repo}/pulls?state=all — the newline-delimited JSON that
//     prListJQ emits, rendered here from the PRInfo fixtures.
//   - repos/{owner}/{repo}/branches?protected=true — empty output, i.e. the
//     remote has no protected branches (an empty non-nil set, not a fallback).
//
// Any other gh invocation exits non-zero, as it would with no remote configured.
func stubGHPRIndex(t *testing.T, prs []branchpkg.PRInfo) {
	t.Helper()

	var payload strings.Builder
	for _, pr := range prs {
		fmt.Fprintf(&payload, "{\"number\":%d,\"state\":%q,\"head\":%q,\"mergeCommit\":%q,\"headSha\":%q}\n",
			pr.Number, string(pr.State), pr.HeadRefName, pr.MergeCommit, pr.HeadSHA)
	}

	binDir := t.TempDir()
	payloadPath := filepath.Join(binDir, "pulls.ndjson")
	if err := os.WriteFile(payloadPath, []byte(payload.String()), 0644); err != nil {
		t.Fatalf("writing PR payload: %v", err)
	}

	script := "#!/bin/sh\n" +
		"for arg in \"$@\"; do\n" +
		"  case \"$arg\" in\n" +
		"    *pulls*) cat " + payloadPath + "; exit 0 ;;\n" +
		"    *protected=true*) exit 0 ;;\n" +
		"  esac\n" +
		"done\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "gh"), []byte(script), 0755); err != nil {
		t.Fatalf("writing gh stub: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// gitRunner returns a fatal-on-error git/exec helper bound to dir.
func gitRunner(t *testing.T, dir string) func(args ...string) {
	t.Helper()
	return func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
}

// branchExists reports whether a local branch ref is still present in dir.
func branchExists(t *testing.T, dir, branch string) bool {
	t.Helper()
	cmd := exec.Command("git", "branch", "--list", branch)
	cmd.Dir = dir
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out)) != ""
}

// TestBranchPrune_DryRun_NoBranches verifies output when no local branches exist.
func TestBranchPrune_DryRun_NoBranches(t *testing.T) {
	_, restore := makeBranchPruneTestRepo(t)
	defer restore()

	out, err := invokeBranchPrune(t, false, false, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No local branches") {
		t.Errorf("expected 'No local branches' message, got: %s", out)
	}
}

// TestBranchPrune_DryRun_ShowsCandidates verifies merged branches are listed.
func TestBranchPrune_DryRun_ShowsCandidates(t *testing.T) {
	dir, restore := makeBranchPruneTestRepo(t)
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
	run("git", "checkout", "-b", "feat/merged-cmd-test")
	os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("feat\n"), 0644)
	run("git", "add", ".")
	run("git", "commit", "-m", "feature work")
	run("git", "checkout", "main")
	run("git", "merge", "--no-ff", "feat/merged-cmd-test", "-m", "merge feat")

	out, err := invokeBranchPrune(t, false, false, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "feat/merged-cmd-test") {
		t.Errorf("expected candidate branch in output, got: %s", out)
	}
	if !strings.Contains(out, "CANDIDATE") {
		t.Errorf("expected [CANDIDATE] marker in dry-run output, got: %s", out)
	}
	if !strings.Contains(out, "no changes made") {
		t.Errorf("expected dry-run notice, got: %s", out)
	}
}

// TestBranchPrune_DryRun_ShowsSkippedProtected verifies protected branches are reported as skipped.
func TestBranchPrune_DryRun_ShowsSkippedProtected(t *testing.T) {
	dir, restore := makeBranchPruneTestRepo(t)
	defer restore()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	// Create a "homolog" branch (protected) that is also merged, plus a real feature branch.
	run("git", "checkout", "-b", "homolog")
	os.WriteFile(filepath.Join(dir, "homolog.txt"), []byte("homolog\n"), 0644)
	run("git", "add", ".")
	run("git", "commit", "-m", "homolog work")
	run("git", "checkout", "main")
	run("git", "merge", "--no-ff", "homolog", "-m", "merge homolog")

	out, err := invokeBranchPrune(t, false, false, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "SKIP") {
		t.Errorf("expected [SKIP] for homolog branch, got: %s", out)
	}
}

// TestBranchPrune_Apply_DeletesMergedBranch verifies apply mode deletes and tombstones.
func TestBranchPrune_Apply_DeletesMergedBranch(t *testing.T) {
	dir, restore := makeBranchPruneTestRepo(t)
	defer restore()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	// Create and merge a feature branch.
	run("git", "checkout", "-b", "feat/apply-delete-test")
	os.WriteFile(filepath.Join(dir, "apply.txt"), []byte("apply\n"), 0644)
	run("git", "add", ".")
	run("git", "commit", "-m", "apply work")
	run("git", "checkout", "main")
	run("git", "merge", "--no-ff", "feat/apply-delete-test", "-m", "merge apply")

	out, err := invokeBranchPrune(t, true, false, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "DELETED") {
		t.Errorf("expected [DELETED] in apply output, got: %s", out)
	}
	if !strings.Contains(out, "Tombstones written") {
		t.Errorf("expected tombstone notice in apply output, got: %s", out)
	}

	// Verify local branch is gone.
	cmd := exec.Command("git", "branch", "--list", "feat/apply-delete-test")
	cmd.Dir = dir
	listOut, _ := cmd.Output()
	if strings.TrimSpace(string(listOut)) != "" {
		t.Error("feat/apply-delete-test still exists after apply")
	}
}

// TestBranchPrune_GC_NoTombstones verifies GC mode reports nothing when no
// tombstones, orphaned review-stamps, or expired .trash/ entries exist — the
// --gc block sweeps all three (P-0184 added the trash sweep).
func TestBranchPrune_GC_NoTombstones(t *testing.T) {
	_, restore := makeBranchPruneTestRepo(t)
	defer restore()

	out, err := invokeBranchPrune(t, false, true, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No tombstones, orphaned review-stamps, or expired trash entries eligible for GC") {
		t.Errorf("expected combined 'nothing eligible for GC' message, got: %s", out)
	}
}

// TestBranchPrune_DryRun_UnmergedBranchSkipped verifies unmerged branches are skipped.
func TestBranchPrune_DryRun_UnmergedBranchSkipped(t *testing.T) {
	dir, restore := makeBranchPruneTestRepo(t)
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
	run("git", "checkout", "-b", "feat/not-merged")
	os.WriteFile(filepath.Join(dir, "notmerged.txt"), []byte("nope\n"), 0644)
	run("git", "add", ".")
	run("git", "commit", "-m", "unmerged")
	run("git", "checkout", "main")

	out, err := invokeBranchPrune(t, false, false, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should see SKIP with unmerged reason or "0 candidate(s)"
	if strings.Contains(out, "CANDIDATE") {
		t.Errorf("unmerged branch should not appear as CANDIDATE, got: %s", out)
	}
}

// TestBranchPrune_DryRun_RejectedCandidateAndSummary verifies the rejected-PR
// surface in dry-run: the candidate line carries source=rejected with the closed
// PR numbers, and the summary breaks the rejected count out of the total.
//
// The repo holds one git-merged branch and one branch whose only PR is CLOSED, so
// the summary must read "2 candidate(s) … (1 rejected-PR)" — proving the breakout
// counts rejected branches only, not every candidate.
func TestBranchPrune_DryRun_RejectedCandidateAndSummary(t *testing.T) {
	dir, restore := makeBranchPruneTestRepo(t)
	defer restore()
	run := gitRunner(t, dir)

	// A normally merged branch (git ancestry).
	run("git", "checkout", "-b", "feat/merged-alongside")
	os.WriteFile(filepath.Join(dir, "merged.txt"), []byte("merged\n"), 0644)
	run("git", "add", ".")
	run("git", "commit", "-m", "merged work")
	run("git", "checkout", "main")
	run("git", "merge", "--no-ff", "feat/merged-alongside", "-m", "merge alongside")

	// A branch that was never merged and whose only PR was closed.
	run("git", "checkout", "-b", "feat/rejected-dry")
	os.WriteFile(filepath.Join(dir, "rejected.txt"), []byte("rejected\n"), 0644)
	run("git", "add", ".")
	run("git", "commit", "-m", "rejected work")
	run("git", "checkout", "main")

	stubGHPRIndex(t, []branchpkg.PRInfo{
		{Number: 101, State: branchpkg.PRStateClosed, HeadRefName: "feat/rejected-dry"},
	})

	out, err := invokeBranchPrune(t, false, false, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}

	if !strings.Contains(out, "PR index: 1 pull request(s) — rejected branches deleted by default") {
		t.Errorf("expected PR-index header with default rejected policy, got: %s", out)
	}
	if !strings.Contains(out, "[CANDIDATE] feat/rejected-dry  source=rejected (all PRs closed: #101)") {
		t.Errorf("expected rejected candidate line with PR number, got: %s", out)
	}
	if !strings.Contains(out, "2 candidate(s) eligible for deletion (1 rejected-PR)") {
		t.Errorf("expected summary breaking out 1 rejected-PR of 2 candidates, got: %s", out)
	}
	if !strings.Contains(out, "no changes made") {
		t.Errorf("expected dry-run notice, got: %s", out)
	}
	if !branchExists(t, dir, "feat/rejected-dry") {
		t.Error("dry-run deleted feat/rejected-dry")
	}
}

// TestBranchPrune_Apply_DeletesRejectedBranch verifies --apply deletes a
// rejected-PR branch under the same tombstone contract as a merged one, and that
// the apply summary breaks the rejected count out of the deleted total.
func TestBranchPrune_Apply_DeletesRejectedBranch(t *testing.T) {
	dir, restore := makeBranchPruneTestRepo(t)
	defer restore()
	run := gitRunner(t, dir)

	run("git", "checkout", "-b", "feat/rejected-apply")
	os.WriteFile(filepath.Join(dir, "rejected.txt"), []byte("rejected\n"), 0644)
	run("git", "add", ".")
	run("git", "commit", "-m", "rejected work")
	run("git", "checkout", "main")

	stubGHPRIndex(t, []branchpkg.PRInfo{
		{Number: 202, State: branchpkg.PRStateClosed, HeadRefName: "feat/rejected-apply"},
		{Number: 203, State: branchpkg.PRStateClosed, HeadRefName: "feat/rejected-apply"},
	})

	out, err := invokeBranchPrune(t, true, false, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}

	if !strings.Contains(out, "[DELETED]   feat/rejected-apply  source=rejected (all PRs closed: #202, #203)") {
		t.Errorf("expected rejected deletion line with both PR numbers, got: %s", out)
	}
	if !strings.Contains(out, "tombstone=refs/tombstones/feat-rejected-apply") {
		t.Errorf("expected tombstone ref for the rejected branch, got: %s", out)
	}
	if !strings.Contains(out, "1 deleted (1 rejected-PR)") {
		t.Errorf("expected apply summary breaking out the rejected deletion, got: %s", out)
	}
	if branchExists(t, dir, "feat/rejected-apply") {
		t.Error("feat/rejected-apply still exists after apply")
	}
}

// TestBranchPrune_ExcludeRejected_HoldsBranch verifies --exclude-rejected end to
// end: the rejected branch is reported as SkipRejectedHeld, the header states the
// opt-out policy, nothing is deleted, and the branch survives.
func TestBranchPrune_ExcludeRejected_HoldsBranch(t *testing.T) {
	dir, restore := makeBranchPruneTestRepo(t)
	defer restore()
	run := gitRunner(t, dir)

	run("git", "checkout", "-b", "feat/rejected-held")
	os.WriteFile(filepath.Join(dir, "held.txt"), []byte("held\n"), 0644)
	run("git", "add", ".")
	run("git", "commit", "-m", "rejected work")
	run("git", "checkout", "main")

	stubGHPRIndex(t, []branchpkg.PRInfo{
		{Number: 77, State: branchpkg.PRStateClosed, HeadRefName: "feat/rejected-held"},
	})

	// --apply AND --exclude-rejected: the hold must survive the deleting mode.
	out, err := invokeBranchPruneFull(t, true, false, "main", true)
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}

	if !strings.Contains(out, "PR index: 1 pull request(s) — rejected branches held (--exclude-rejected)") {
		t.Errorf("expected PR-index header stating the hold policy, got: %s", out)
	}
	if !strings.Contains(out, "[SKIP]      feat/rejected-held  reason=rejected-held") {
		t.Errorf("expected rejected-held skip line, got: %s", out)
	}
	if !strings.Contains(out, "all PRs closed (#77) — held by --exclude-rejected") {
		t.Errorf("expected skip detail naming the closed PR, got: %s", out)
	}
	if strings.Contains(out, "[DELETED]") {
		t.Errorf("nothing should be deleted under --exclude-rejected, got: %s", out)
	}
	if !strings.Contains(out, "0 deleted (0 rejected-PR)") {
		t.Errorf("expected zero-deletion summary, got: %s", out)
	}
	if !branchExists(t, dir, "feat/rejected-held") {
		t.Error("feat/rejected-held was deleted despite --exclude-rejected")
	}
}
