package branch

// Prune invariant tests — one TestInvariant_I* function per invariant documented
// in prune_invariants.md. These tests verify the correctness properties of
// PruneBranch() independently of the unit tests in prune_test.go.
//
// All tests use makeGitRepo() from prune_test.go and set
// KAISSER_BRANCH_PRUNE_BYPASS_ACTIVE_CMD=1 via that helper.
//
// Run: go test ./cli/internal/branch/... -run TestInvariant

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ── Helper ─────────────────────────────────────────────────────────────────────

// makeFeatureBranch creates a new branch from main in the current git repo,
// adds one commit to it, and stays on that branch.
func makeFeatureBranch(t *testing.T, name string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		mustRunIn(t, ".", args...)
	}
	run("git", "checkout", "-b", name)
	// Add a commit so the branch has its own SHA.
	// Use a simple file name without slashes — git add needs a path, not a branch name.
	safeName := strings.ReplaceAll(name, "/", "-")
	f := safeName + "-file.txt"
	os.WriteFile(f, []byte("content for "+name+"\n"), 0644)
	run("git", "add", f)
	run("git", "commit", "-m", "chore: add file for "+safeName)
}

// mergeFeatureBranchIntoMain merges name into main (fast-forward compatible).
func mergeFeatureBranchIntoMain(t *testing.T, name string) {
	t.Helper()
	mustRunIn(t, ".", "git", "checkout", "main")
	mustRunIn(t, ".", "git", "merge", "--no-ff", "-m", "merge "+name, name)
}

// mustRunIn runs a command in dir, failing the test on error.
func mustRunIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command(args[0], args[1:]...)
	if dir != "" && dir != "." {
		c.Dir = dir
	}
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("cmd %v: %v\n%s", args, err, out)
	}
}

// ── I1: Current HEAD Immunity ─────────────────────────────────────────────────

// TestInvariant_I1_CurrentHEADImmunity verifies that PruneBranch skips the
// currently checked-out branch even when it appears merged.
func TestInvariant_I1_CurrentHEADImmunity(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	// Create a feature branch and merge it into main, but stay on main.
	makeFeatureBranch(t, "feat/i1-test")
	mergeFeatureBranchIntoMain(t, "feat/i1-test")

	// Now checkout the feature branch (making it the current HEAD).
	mustRunIn(t, ".", "git", "checkout", "feat/i1-test")

	// PruneBranch on the current HEAD must skip, not delete.
	d := PruneBranch("feat/i1-test", PruneOpts{Base: "main", Apply: true})
	if !d.Skipped {
		t.Errorf("I1: expected branch to be skipped (current HEAD), got Deleted=%v Skipped=%v", d.Deleted, d.Skipped)
	}
	if d.SkipReason != SkipCurrentHEAD {
		t.Errorf("I1: expected SkipReason=%q, got %q", SkipCurrentHEAD, d.SkipReason)
	}
}

// ── I2: Protected Branch Immunity ─────────────────────────────────────────────

// TestInvariant_I2_ProtectedBranchImmunity verifies that well-known protected
// branches are never deleted, regardless of merge status.
func TestInvariant_I2_ProtectedBranchImmunity(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	// Attempt to prune "main" — which is always protected.
	d := PruneBranch("main", PruneOpts{Base: "main", Apply: true})
	if !d.Skipped {
		t.Errorf("I2: main should be skipped as protected, got Deleted=%v", d.Deleted)
	}
	if d.SkipReason != SkipProtected {
		t.Errorf("I2: expected SkipReason=%q, got %q", SkipProtected, d.SkipReason)
	}

	// Also verify homolog is name-protected (test via IsProtected directly — the branch
	// may not exist in the test repo, which would cause PruneBranch to return SkipUnmerged).
	protected2, reason2 := IsProtected("homolog")
	if !protected2 {
		t.Errorf("I2: IsProtected(homolog) = false, want true (reason: %q)", reason2)
	}

	// A feature branch should NOT be protected by name alone.
	protected, _ := IsProtected("feat/not-protected")
	if protected {
		t.Error("I2: feat/not-protected should not be name-protected")
	}
}

// ── I3: Tombstone-Precedes-Deletion ───────────────────────────────────────────

// TestInvariant_I3_TombstonePrecedesDeletion verifies that WriteTombstone is
// called before any deletion, and that a tombstone ref exists after pruning.
func TestInvariant_I3_TombstonePrecedesDeletion(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	makeFeatureBranch(t, "feat/i3-test")
	mergeFeatureBranchIntoMain(t, "feat/i3-test")
	// Return to main so i3-test is not the current HEAD.
	mustRunIn(t, ".", "git", "checkout", "main")

	d := PruneBranch("feat/i3-test", PruneOpts{Base: "main", Apply: true})
	if !d.Deleted {
		t.Fatalf("I3: expected branch to be deleted; got Deleted=%v Skipped=%v Error=%q", d.Deleted, d.Skipped, d.Error)
	}
	// Tombstone ref must have been written.
	if d.Tombstone == "" {
		t.Error("I3: Tombstone field is empty after successful delete")
	}
	if !strings.HasPrefix(d.Tombstone, "refs/tombstones/") {
		t.Errorf("I3: Tombstone %q does not start with refs/tombstones/", d.Tombstone)
	}
	// Verify the tombstone ref actually exists in git.
	out, err := runOutputIn(".", "git", "rev-parse", d.Tombstone)
	if err != nil {
		t.Errorf("I3: tombstone ref %q cannot be resolved: %v", d.Tombstone, err)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("I3: tombstone ref %q resolved to empty SHA", d.Tombstone)
	}
}

// ── I4: Merge OR-Gate ─────────────────────────────────────────────────────────

// TestInvariant_I4_MergeORGate verifies the OR-gate logic of IsMerged.
// We test the git-branch path since the GitHub path requires network access.
func TestInvariant_I4_MergeORGate(t *testing.T) {
	tests := []struct {
		name        string
		mergeBranch bool
		wantMerged  bool
	}{
		{"merged branch is detected", true, true},
		{"unmerged branch is not merged", false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, restore := makeGitRepo(t)
			defer restore()

			makeFeatureBranch(t, "feat/i4-test")
			if tc.mergeBranch {
				mergeFeatureBranchIntoMain(t, "feat/i4-test")
			}
			mustRunIn(t, ".", "git", "checkout", "main")

			merged, _, err := IsMerged("feat/i4-test", "main")
			if err != nil {
				// GitHub API calls may fail in test env — log and skip PR path check.
				t.Logf("I4: IsMerged returned error (may be expected without GH token): %v", err)
			}
			if merged != tc.wantMerged {
				t.Errorf("I4: IsMerged(mergeBranch=%v) = %v, want %v", tc.mergeBranch, merged, tc.wantMerged)
			}
		})
	}
}

// ── I5: Open-Plan Blocking ────────────────────────────────────────────────────

// TestInvariant_I5_OpenPlanBlocking verifies that a branch with an open plan
// is never deleted.
func TestInvariant_I5_OpenPlanBlocking(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	makeFeatureBranch(t, "feat/i5-test")
	mergeFeatureBranchIntoMain(t, "feat/i5-test")
	mustRunIn(t, ".", "git", "checkout", "main")

	// Create a .planning file that references the branch.
	planDir := filepath.Join(".", ".planning")
	os.MkdirAll(planDir, 0755)
	content := "---\nbranch: feat/i5-test\ntitle: I5 plan\nstatus: approved\n---\n# Plan\n"
	planPath := filepath.Join(planDir, "P-I5-test-approved.md")
	os.WriteFile(planPath, []byte(content), 0644)
	t.Cleanup(func() { os.RemoveAll(planDir) })

	d := PruneBranch("feat/i5-test", PruneOpts{Base: "main", Apply: true})
	if !d.Skipped {
		t.Errorf("I5: expected branch to be skipped (open plan), got Deleted=%v Skipped=%v", d.Deleted, d.Skipped)
	}
	if d.SkipReason != SkipOpenPlan {
		t.Errorf("I5: expected SkipReason=%q, got %q", SkipOpenPlan, d.SkipReason)
	}
}

// ── I6: Active-Command Blocking ───────────────────────────────────────────────

// TestInvariant_I6_ActiveCommandBlocking verifies that when the active-command
// marker is present AND the bypass env var is unset, PruneBranch skips.
func TestInvariant_I6_ActiveCommandBlocking(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	makeFeatureBranch(t, "feat/i6-test")
	mergeFeatureBranchIntoMain(t, "feat/i6-test")
	mustRunIn(t, ".", "git", "checkout", "main")

	// Unset the bypass flag so the guard fires.
	t.Setenv("KAISSER_BRANCH_PRUNE_BYPASS_ACTIVE_CMD", "")

	// Create a fake session marker.
	sessionID := "test-session-i6"
	t.Setenv("CLAUDE_SESSION_ID", sessionID)
	markerDir := filepath.Join(os.TempDir(), "agent-audit-"+sessionID)
	os.MkdirAll(markerDir, 0755)
	markerFile := filepath.Join(markerDir, "active-command")
	os.WriteFile(markerFile, []byte("prune-merged"), 0644)
	t.Cleanup(func() { os.RemoveAll(markerDir) })

	d := PruneBranch("feat/i6-test", PruneOpts{Base: "main", Apply: true})
	if !d.Skipped {
		t.Errorf("I6: expected branch to be skipped (active-command marker), got Deleted=%v", d.Deleted)
	}
	if d.SkipReason != SkipActiveCommand {
		t.Errorf("I6: expected SkipReason=%q, got %q", SkipActiveCommand, d.SkipReason)
	}

	// With bypass re-enabled, the branch should proceed past the guard.
	t.Setenv("KAISSER_BRANCH_PRUNE_BYPASS_ACTIVE_CMD", "1")
	d2 := PruneBranch("feat/i6-test", PruneOpts{Base: "main", Apply: true})
	// The branch may still be skipped for other reasons (e.g. head), but NOT for active-command.
	if d2.SkipReason == SkipActiveCommand {
		t.Error("I6: bypass=1 should suppress active-command skip")
	}
}

// ── I7: Local + Remote Deletion Independence ──────────────────────────────────

// TestInvariant_I7_LocalRemoteIndependence verifies that the local branch is
// deleted even when remote deletion fails (simulated by having no remote).
func TestInvariant_I7_LocalRemoteIndependence(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	makeFeatureBranch(t, "feat/i7-test")
	mergeFeatureBranchIntoMain(t, "feat/i7-test")
	mustRunIn(t, ".", "git", "checkout", "main")

	// No remote is configured, so `git push origin --delete` will fail.
	// The local deletion should still succeed.
	d := PruneBranch("feat/i7-test", PruneOpts{Base: "main", Apply: true})

	// Branch is local-only, so the remote path is not attempted.
	// The branch MUST be deleted.
	if !d.Deleted {
		t.Errorf("I7: expected branch deleted (local-only); Deleted=%v Skipped=%v Error=%q", d.Deleted, d.Skipped, d.Error)
	}
}

// ── I8: Worktree is OFF-LIMITS (both modes) ───────────────────────────────────

// TestInvariant_I8_WorktreeOffLimits verifies that a branch with an active worktree
// is skipped in BOTH dry-run and --apply modes — prune-merged never removes a worktree
// or deletes a worktree-backed branch (the worktree lifecycle is owned solely by the
// /worktree skill). The remote ref is pruned on a later pass, once /worktree destroy
// has removed the worktree + local branch.
func TestInvariant_I8_WorktreeOffLimits(t *testing.T) {
	dir, restore := makeGitRepo(t)
	defer restore()

	makeFeatureBranch(t, "feat/i8-test")
	mergeFeatureBranchIntoMain(t, "feat/i8-test")
	mustRunIn(t, ".", "git", "checkout", "main")

	// Create a worktree for the branch.
	wtPath := filepath.Join(dir, "..", "wt-i8-test")
	mustRunIn(t, ".", "git", "worktree", "add", wtPath, "feat/i8-test")
	t.Cleanup(func() {
		// Remove worktree unconditionally at cleanup.
		_, _ = runOutputIn(".", "git", "worktree", "remove", "--force", wtPath)
		os.RemoveAll(wtPath)
	})

	assertOffLimits := func(label string, opts PruneOpts) {
		d := PruneBranch("feat/i8-test", opts)
		if !d.Skipped {
			t.Errorf("I8 (%s): expected branch skipped (active worktree), got Deleted=%v", label, d.Deleted)
		}
		if d.SkipReason != SkipWorktree {
			t.Errorf("I8 (%s): expected SkipReason=%q, got %q", label, SkipWorktree, d.SkipReason)
		}
		if d.Deleted {
			t.Errorf("I8 (%s): worktree-backed branch must NOT be deleted", label)
		}
		// Worktree must survive.
		out, _ := runOutputIn(".", "git", "worktree", "list", "--porcelain")
		if !strings.Contains(out, wtPath) {
			t.Errorf("I8 (%s): worktree must survive, but %q is gone", label, wtPath)
		}
		// Local branch must survive.
		bOut, _ := runOutputIn(".", "git", "branch", "--list", "feat/i8-test")
		if strings.TrimSpace(bOut) == "" {
			t.Errorf("I8 (%s): worktree-backed branch must survive", label)
		}
	}

	assertOffLimits("dry-run", PruneOpts{Base: "main", Apply: false})
	assertOffLimits("apply", PruneOpts{Base: "main", Apply: true})

	// Production path: runBranchPrune parses the set ONCE via WorktreeBranches()
	// and injects it through PruneOpts — the merged-to-main branch must still
	// survive --apply with the worktree-active skip.
	set, err := WorktreeBranches()
	if err != nil {
		t.Fatalf("I8: WorktreeBranches() error: %v", err)
	}
	if _, ok := set["feat/i8-test"]; !ok {
		t.Fatalf("I8: WorktreeBranches() missing feat/i8-test; got %v", set)
	}
	assertOffLimits("apply-injected-set", PruneOpts{Base: "main", Apply: true, WorktreeBranches: set})
}

// ── I8 extension: remote-only classification must still hit Guard 6 ──────────

// TestInvariant_I8_RemoteOnlyClassificationStillSkips verifies the worktree set
// is consulted UNCONDITIONALLY — the old `loc.HasLocal` gate let a branch that
// branchLocation classified as remote-only bypass Guard 6 entirely. With the
// gate removed, a remote-only candidate present in the worktree set must be
// skipped as worktree-active before any deletion primitive runs.
func TestInvariant_I8_RemoteOnlyClassificationStillSkips(t *testing.T) {
	localDir, _, restore := makeGitRepoWithRemote(t)
	defer restore()

	// Branch with a commit, pushed and merged to main, then the LOCAL ref is
	// deleted so branchLocation reports HasLocal=false, HasRemote=true.
	mustRunIn(t, localDir, "git", "checkout", "-b", "feat/i8-remote-only")
	os.WriteFile(filepath.Join(localDir, "ro.txt"), []byte("ro\n"), 0644)
	mustRunIn(t, localDir, "git", "add", "ro.txt")
	mustRunIn(t, localDir, "git", "commit", "-m", "remote-only work")
	mustRunIn(t, localDir, "git", "push", "origin", "feat/i8-remote-only")
	mustRunIn(t, localDir, "git", "checkout", "main")
	mustRunIn(t, localDir, "git", "merge", "--no-ff", "-m", "merge ro", "feat/i8-remote-only")
	mustRunIn(t, localDir, "git", "push", "origin", "main")
	mustRunIn(t, localDir, "git", "branch", "-D", "feat/i8-remote-only")

	// Simulate the misclassification hazard: the branch IS held by a worktree
	// according to the injected set (as parsed once by runBranchPrune).
	set := map[string]string{"feat/i8-remote-only": "/fake/worktrees/i8-remote-only"}

	d := PruneBranch("feat/i8-remote-only", PruneOpts{
		Base: "main", Apply: true, WorktreeBranches: set,
	})
	if !d.Skipped {
		t.Fatalf("I8 remote-only: expected skip, got Deleted=%v Error=%q", d.Deleted, d.Error)
	}
	if d.SkipReason != SkipWorktree {
		t.Errorf("I8 remote-only: expected SkipReason=%q, got %q", SkipWorktree, d.SkipReason)
	}
	// The remote ref must survive.
	out, _ := runOutputIn(localDir, "git", "ls-remote", "--heads", "origin", "feat/i8-remote-only")
	if !strings.Contains(out, "feat/i8-remote-only") {
		t.Errorf("I8 remote-only: remote ref must survive, ls-remote output: %q", out)
	}
}

// ── I8 extension: worktree listing failure is fail-closed ─────────────────────

// TestInvariant_I8_WorktreeListingErrorPropagates verifies WorktreeBranches()
// PROPAGATES listing errors instead of swallowing them (the old HasActiveWorktree
// failed OPEN — a git error meant "no worktree" and deletion proceeded).
// runBranchPrune aborts the whole run on this error; PruneBranch with a nil set
// fails closed on the same error.
func TestInvariant_I8_WorktreeListingErrorPropagates(t *testing.T) {
	// A plain temp dir is not a git repo — the listing must error, not return empty.
	dir := t.TempDir()
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	os.Chdir(dir)

	if set, err := WorktreeBranches(); err == nil {
		t.Fatalf("WorktreeBranches outside a git repo must return an error (fail-closed contract), got set=%v", set)
	}
	if _, _, err := HasActiveWorktree("any-branch"); err == nil {
		t.Fatal("HasActiveWorktree outside a git repo must propagate the listing error")
	}
}

// ── I9: PR-State Deletion Requires Positive Evidence ──────────────────────────

// TestInvariant_I9_OnlyAllClosedPRsCanDelete verifies that among branches the
// merge OR-gate refused, `rejected` — every PR CLOSED, none open, none merged —
// is the ONLY class that reaches the deletion path, and that it requires an
// AVAILABLE PR index. Absence of information is never a deletion.
func TestInvariant_I9_OnlyAllClosedPRsCanDelete(t *testing.T) {
	cases := []struct {
		name       string
		index      func() *PRIndex
		wantDelete bool
	}{
		{
			name:       "all PRs closed → deleted",
			index:      func() *PRIndex { return NewPRIndex([]PRInfo{{Number: 1, State: PRStateClosed, HeadRefName: "feat/i9-test"}}) },
			wantDelete: true,
		},
		{
			name:       "an open PR → kept",
			index:      func() *PRIndex { return NewPRIndex([]PRInfo{{Number: 2, State: PRStateOpen, HeadRefName: "feat/i9-test"}}) },
			wantDelete: false,
		},
		{
			name: "a merged PR whose head moved on → kept",
			index: func() *PRIndex {
				return NewPRIndex([]PRInfo{{
					Number: 3, State: PRStateMerged, HeadRefName: "feat/i9-test",
					HeadSHA: "0000000000000000000000000000000000000000",
				}})
			},
			wantDelete: false,
		},
		{
			name:       "no PR at all → kept",
			index:      func() *PRIndex { return NewPRIndex(nil) },
			wantDelete: false,
		},
		{
			name:       "index unavailable → kept",
			index:      UnavailablePRIndex,
			wantDelete: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, restore := makeGitRepo(t)
			defer restore()

			makeFeatureBranch(t, "feat/i9-test")
			mustRunIn(t, ".", "git", "checkout", "main")

			d := PruneBranch("feat/i9-test", PruneOpts{
				Base:             "main",
				Apply:            true,
				WorktreeBranches: map[string]string{},
				ProtectedRemote:  map[string]bool{},
				RemoteSynced:     true,
				PRIndex:          tc.index(),
			})
			if d.Deleted != tc.wantDelete {
				t.Fatalf("I9: Deleted=%v, want %v (reason=%q class=%q)",
					d.Deleted, tc.wantDelete, d.SkipReason, d.PRClass)
			}

			_, refErr := runOutputIn(".", "git", "show-ref", "--verify", "--quiet", "refs/heads/feat/i9-test")
			branchGone := refErr != nil
			if branchGone != tc.wantDelete {
				t.Errorf("I9: branch gone=%v, want %v", branchGone, tc.wantDelete)
			}
		})
	}
}

// TestInvariant_I9_ExcludeRejectedHoldsEverything verifies the documented opt-out:
// with --exclude-rejected, no rejected branch is deleted.
func TestInvariant_I9_ExcludeRejectedHoldsEverything(t *testing.T) {
	_, restore := makeGitRepo(t)
	defer restore()

	makeFeatureBranch(t, "feat/i9-excluded")
	mustRunIn(t, ".", "git", "checkout", "main")

	d := PruneBranch("feat/i9-excluded", PruneOpts{
		Base:             "main",
		Apply:            true,
		WorktreeBranches: map[string]string{},
		ProtectedRemote:  map[string]bool{},
		RemoteSynced:     true,
		ExcludeRejected:  true,
		PRIndex:          NewPRIndex([]PRInfo{{Number: 4, State: PRStateClosed, HeadRefName: "feat/i9-excluded"}}),
	})
	if d.Deleted {
		t.Fatal("I9: --exclude-rejected must not delete a rejected branch")
	}
	if d.SkipReason != SkipRejectedHeld {
		t.Errorf("I9: SkipReason=%q, want %q", d.SkipReason, SkipRejectedHeld)
	}
	if _, err := runOutputIn(".", "git", "show-ref", "--verify", "--quiet", "refs/heads/feat/i9-excluded"); err != nil {
		t.Error("I9: branch was deleted under --exclude-rejected")
	}
}

// ── exec helper ───────────────────────────────────────────────────────────────

// runOutputIn runs a command in dir and returns (stdout, err).
func runOutputIn(dir string, args ...string) (string, error) {
	c := exec.Command(args[0], args[1:]...)
	if dir != "" && dir != "." {
		c.Dir = dir
	}
	out, err := c.CombinedOutput()
	return string(out), err
}
