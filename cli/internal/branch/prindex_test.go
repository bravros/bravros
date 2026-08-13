package branch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---- ParsePRIndex ------------------------------------------------------------

func TestParsePRIndex_GroupsByHeadNewestFirst(t *testing.T) {
	lines := strings.Join([]string{
		`{"number":10,"state":"CLOSED","head":"fix/a","mergeCommit":""}`,
		`{"number":42,"state":"CLOSED","head":"fix/a","mergeCommit":""}`,
		`{"number":7,"state":"MERGED","head":"feat/b","mergeCommit":"abc123"}`,
	}, "\n")

	ix, err := ParsePRIndex(lines)
	if err != nil {
		t.Fatalf("ParsePRIndex: %v", err)
	}
	if !ix.Available() {
		t.Fatal("index should be available after a clean parse")
	}
	if ix.Total() != 3 {
		t.Errorf("Total() = %d, want 3", ix.Total())
	}

	a := ix.For("fix/a")
	if len(a) != 2 {
		t.Fatalf("For(fix/a) returned %d PRs, want 2", len(a))
	}
	if a[0].Number != 42 || a[1].Number != 10 {
		t.Errorf("PRs not sorted newest-first: got %d, %d", a[0].Number, a[1].Number)
	}

	b := ix.For("feat/b")
	if len(b) != 1 || b[0].State != PRStateMerged || b[0].MergeCommit != "abc123" {
		t.Errorf("For(feat/b) = %+v, want one MERGED PR with sha abc123", b)
	}

	if got := ix.For("never-existed"); got != nil {
		t.Errorf("For(unknown branch) = %+v, want nil", got)
	}
}

func TestParsePRIndex_EmptyOutputIsAvailableAndEmpty(t *testing.T) {
	ix, err := ParsePRIndex("")
	if err != nil {
		t.Fatalf("ParsePRIndex(\"\"): %v", err)
	}
	// A repo with zero PRs is a real, trustworthy answer — distinct from a
	// failed fetch.
	if !ix.Available() {
		t.Error("empty-but-successful parse must stay available")
	}
	if ix.Total() != 0 {
		t.Errorf("Total() = %d, want 0", ix.Total())
	}
}

func TestParsePRIndex_MalformedOutputIsUnavailable(t *testing.T) {
	ix, err := ParsePRIndex(`{"number":1,"state":"OPEN","head":"x"` + "\n" + `not json at all`)
	if err == nil {
		t.Fatal("expected a decode error for malformed input")
	}
	// The key property: a half-parsed index must never look like a complete one,
	// or a truncated fetch would read as "these branches have no PR".
	if ix.Available() {
		t.Error("malformed parse must yield an UNAVAILABLE index")
	}
	if got := ix.For("x"); got != nil {
		t.Errorf("unavailable index returned %+v, want nil", got)
	}
}

func TestUnavailablePRIndex_ReportsNothing(t *testing.T) {
	ix := UnavailablePRIndex()
	if ix.Available() {
		t.Error("UnavailablePRIndex().Available() must be false")
	}
	if ix.Total() != 0 {
		t.Errorf("Total() = %d, want 0", ix.Total())
	}
	var nilIndex *PRIndex
	if nilIndex.Available() {
		t.Error("nil *PRIndex must report Available() == false, not panic")
	}
	if nilIndex.For("anything") != nil || nilIndex.Total() != 0 {
		t.Error("nil *PRIndex accessors must be safe")
	}
}

// ---- ClassifyPRs -------------------------------------------------------------

func TestClassifyPRs(t *testing.T) {
	tests := []struct {
		name      string
		prs       []PRInfo
		wantClass PRClass
		wantNums  []int
	}{
		{
			name:      "no PRs at all is no-pr",
			prs:       nil,
			wantClass: PRClassNoPR,
			wantNums:  nil,
		},
		{
			name:      "every PR closed is rejected",
			prs:       []PRInfo{{Number: 9, State: PRStateClosed}, {Number: 3, State: PRStateClosed}},
			wantClass: PRClassRejected,
			wantNums:  []int{3, 9},
		},
		{
			name:      "an open PR is in-flight",
			prs:       []PRInfo{{Number: 5, State: PRStateOpen}},
			wantClass: PRClassInFlight,
			wantNums:  []int{5},
		},
		{
			name:      "open wins over closed",
			prs:       []PRInfo{{Number: 1, State: PRStateClosed}, {Number: 2, State: PRStateOpen}},
			wantClass: PRClassInFlight,
			wantNums:  []int{2},
		},
		{
			name:      "open wins over merged",
			prs:       []PRInfo{{Number: 1, State: PRStateMerged}, {Number: 2, State: PRStateOpen}},
			wantClass: PRClassInFlight,
			wantNums:  []int{2},
		},
		{
			name:      "merged with no open is stray-tip",
			prs:       []PRInfo{{Number: 4, State: PRStateMerged}, {Number: 6, State: PRStateClosed}},
			wantClass: PRClassStrayTip,
			wantNums:  []int{4},
		},
		{
			name:      "unrecognised states classify as unknown",
			prs:       []PRInfo{{Number: 8, State: PRState("DRAFTED")}},
			wantClass: PRClassUnknown,
			wantNums:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			class, nums := ClassifyPRs(tc.prs)
			if class != tc.wantClass {
				t.Errorf("class = %q, want %q", class, tc.wantClass)
			}
			if len(nums) != len(tc.wantNums) {
				t.Fatalf("nums = %v, want %v", nums, tc.wantNums)
			}
			for i := range nums {
				if nums[i] != tc.wantNums[i] {
					t.Errorf("nums = %v, want %v", nums, tc.wantNums)
					break
				}
			}
		})
	}
}

func TestFormatPRNumbers(t *testing.T) {
	if got := FormatPRNumbers(nil); got != "" {
		t.Errorf("FormatPRNumbers(nil) = %q, want empty", got)
	}
	if got := FormatPRNumbers([]int{7}); got != "#7" {
		t.Errorf("FormatPRNumbers([7]) = %q, want #7", got)
	}
	if got := FormatPRNumbers([]int{3, 9}); got != "#3, #9" {
		t.Errorf("FormatPRNumbers([3,9]) = %q, want '#3, #9'", got)
	}
}

// ---- isProtectedRemote -------------------------------------------------------

func TestIsProtectedRemote_BatchedSet(t *testing.T) {
	set := map[string]bool{"main": true}
	if !isProtectedRemote("main", set) {
		t.Error("branch in the batched set must be protected")
	}
	if isProtectedRemote("feat/x", set) {
		t.Error("branch absent from the batched set must not be protected")
	}
	// An EMPTY non-nil set means "fetched, nothing protected" — it must be
	// honoured, not mistaken for "not fetched" and re-queried per branch.
	if isProtectedRemote("main", map[string]bool{}) {
		t.Error("empty non-nil set must answer 'not protected' without falling back")
	}
}

// ---- PruneBranch PR-state classification -------------------------------------

// makeUnmergedBranch creates a branch with a commit that is NOT on main, so the
// merge OR-gate refuses it and PR-state classification takes over.
func makeUnmergedBranch(t *testing.T, dir, name string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "checkout", "-b", name)
	// Branch names carry slashes; flatten them for the on-disk fixture file.
	fixture := strings.ReplaceAll(name, "/", "-") + ".txt"
	if err := os.WriteFile(filepath.Join(dir, fixture), []byte("work\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "work on "+name)
	run("git", "checkout", "main")
}

// classifyOpts builds hermetic PruneOpts: the batched protected set and the
// pre-synced flag keep every network call out of these tests.
func classifyOpts(ix *PRIndex, apply, excludeRejected bool) PruneOpts {
	return PruneOpts{
		Base:             "main",
		Apply:            apply,
		RepoName:         "test",
		WorktreeBranches: map[string]string{},
		PRIndex:          ix,
		ExcludeRejected:  excludeRejected,
		ProtectedRemote:  map[string]bool{},
		RemoteSynced:     true,
	}
}

func TestPruneBranch_RejectedIsCandidateByDefault(t *testing.T) {
	dir, restore := makeGitRepo(t)
	defer restore()
	makeUnmergedBranch(t, dir, "fix/rejected")

	ix := NewPRIndex([]PRInfo{
		{Number: 71, State: PRStateClosed, HeadRefName: "fix/rejected"},
		{Number: 147, State: PRStateClosed, HeadRefName: "fix/rejected"},
	})

	d := PruneBranch("fix/rejected", classifyOpts(ix, false, false))
	if d.Skipped {
		t.Fatalf("rejected branch was skipped: reason=%q detail=%q", d.SkipReason, d.SkipDetail)
	}
	if !d.Rejected {
		t.Error("Rejected should be true")
	}
	if d.Merged {
		t.Error("a rejected branch is NOT merged — Merged must stay false")
	}
	if d.PRClass != PRClassRejected {
		t.Errorf("PRClass = %q, want %q", d.PRClass, PRClassRejected)
	}
	if got := FormatPRNumbers(d.PRNumbers); got != "#71, #147" {
		t.Errorf("PRNumbers rendered %q, want '#71, #147'", got)
	}
}

func TestPruneBranch_RejectedHeldByExcludeFlag(t *testing.T) {
	dir, restore := makeGitRepo(t)
	defer restore()
	makeUnmergedBranch(t, dir, "fix/rejected")

	ix := NewPRIndex([]PRInfo{{Number: 71, State: PRStateClosed, HeadRefName: "fix/rejected"}})

	d := PruneBranch("fix/rejected", classifyOpts(ix, false, true))
	if !d.Skipped || d.SkipReason != SkipRejectedHeld {
		t.Fatalf("expected SkipRejectedHeld, got skipped=%v reason=%q", d.Skipped, d.SkipReason)
	}
	if d.Rejected {
		t.Error("a held branch must not be marked Rejected (it is not a candidate)")
	}
}

func TestPruneBranch_RejectedApplyTombstonesAndDeletes(t *testing.T) {
	dir, restore := makeGitRepo(t)
	defer restore()
	makeUnmergedBranch(t, dir, "fix/rejected")

	tipOut, _, err := gitRunIn(dir, "git", "rev-parse", "fix/rejected")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	tip := strings.TrimSpace(tipOut)

	ix := NewPRIndex([]PRInfo{{Number: 71, State: PRStateClosed, HeadRefName: "fix/rejected"}})

	d := PruneBranch("fix/rejected", classifyOpts(ix, true, false))
	if d.Error != "" {
		t.Fatalf("unexpected error: %s", d.Error)
	}
	if !d.Deleted {
		t.Fatalf("branch not deleted: %+v", d)
	}
	if d.Tombstone != "refs/tombstones/fix-rejected" {
		t.Errorf("tombstone = %q, want refs/tombstones/fix-rejected", d.Tombstone)
	}

	// Same recovery contract as a merged deletion: the tombstone must point at
	// the pre-deletion tip.
	tombOut, _, err := gitRunIn(dir, "git", "rev-parse", "refs/tombstones/fix-rejected")
	if err != nil {
		t.Fatalf("tombstone ref missing: %v", err)
	}
	if strings.TrimSpace(tombOut) != tip {
		t.Errorf("tombstone points at %s, want %s", strings.TrimSpace(tombOut), tip)
	}

	if _, _, err := gitRunIn(dir, "git", "show-ref", "--verify", "--quiet", "refs/heads/fix/rejected"); err == nil {
		t.Error("local branch still present after --apply")
	}
}

func TestPruneBranch_PRStateSkipClasses(t *testing.T) {
	tests := []struct {
		name       string
		prs        func(tip string) []PRInfo
		wantReason SkipReason
		wantClass  PRClass
	}{
		{
			name: "open PR keeps the branch in flight",
			prs: func(string) []PRInfo {
				return []PRInfo{{Number: 12, State: PRStateOpen, HeadRefName: "fix/x"}}
			},
			wantReason: SkipInFlight,
			wantClass:  PRClassInFlight,
		},
		{
			name: "merged PR whose head moved on is a stray tip",
			prs: func(string) []PRInfo {
				// headSha is a commit that is NOT the branch tip → work was
				// pushed after the merge.
				return []PRInfo{{
					Number:      334,
					State:       PRStateMerged,
					HeadRefName: "fix/x",
					HeadSHA:     "0000000000000000000000000000000000000000",
				}}
			},
			wantReason: SkipStrayTip,
			wantClass:  PRClassStrayTip,
		},
		{
			name:       "no PR at all is never auto-deleted",
			prs:        func(string) []PRInfo { return nil },
			wantReason: SkipNoPR,
			wantClass:  PRClassNoPR,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir, restore := makeGitRepo(t)
			defer restore()
			makeUnmergedBranch(t, dir, "fix/x")

			tipOut, _, err := gitRunIn(dir, "git", "rev-parse", "fix/x")
			if err != nil {
				t.Fatalf("rev-parse: %v", err)
			}

			d := PruneBranch("fix/x", classifyOpts(NewPRIndex(tc.prs(strings.TrimSpace(tipOut))), false, false))
			if !d.Skipped {
				t.Fatalf("branch should have been skipped, got %+v", d)
			}
			if d.SkipReason != tc.wantReason {
				t.Errorf("SkipReason = %q, want %q", d.SkipReason, tc.wantReason)
			}
			if d.PRClass != tc.wantClass {
				t.Errorf("PRClass = %q, want %q", d.PRClass, tc.wantClass)
			}
			if d.Rejected {
				t.Error("Rejected must be false for every non-rejected class")
			}
		})
	}
}

// TestPruneBranch_UnavailableIndexNeverRejects is the fail-safe: when the batch
// fetch failed we know nothing about any branch's PRs, so a branch whose PRs
// happen to all be closed must NOT be deleted — it degrades to plain `unmerged`.
func TestPruneBranch_UnavailableIndexNeverRejects(t *testing.T) {
	dir, restore := makeGitRepo(t)
	defer restore()
	makeUnmergedBranch(t, dir, "fix/rejected")

	d := PruneBranch("fix/rejected", classifyOpts(UnavailablePRIndex(), true, false))
	if !d.Skipped || d.SkipReason != SkipUnmerged {
		t.Fatalf("expected SkipUnmerged with an unavailable index, got skipped=%v reason=%q", d.Skipped, d.SkipReason)
	}
	if d.Rejected {
		t.Error("an unavailable index must never produce a rejected candidate")
	}
	if _, _, err := gitRunIn(dir, "git", "show-ref", "--verify", "--quiet", "refs/heads/fix/rejected"); err != nil {
		t.Error("branch was deleted despite an unavailable PR index")
	}
}

// TestPruneBranch_RejectedStillYieldsToEarlierGuards proves the backlog's
// guard-order requirement: worktree-held and protected branches are excluded
// BEFORE PR-state classification ever runs.
func TestPruneBranch_RejectedStillYieldsToEarlierGuards(t *testing.T) {
	ix := NewPRIndex([]PRInfo{{Number: 71, State: PRStateClosed, HeadRefName: "fix/rejected"}})

	t.Run("worktree-held", func(t *testing.T) {
		dir, restore := makeGitRepo(t)
		defer restore()
		makeUnmergedBranch(t, dir, "fix/rejected")

		opts := classifyOpts(ix, true, false)
		opts.WorktreeBranches = map[string]string{"fix/rejected": "/tmp/wt-fix-rejected"}

		d := PruneBranch("fix/rejected", opts)
		if !d.Skipped || d.SkipReason != SkipWorktree {
			t.Fatalf("expected SkipWorktree, got skipped=%v reason=%q", d.Skipped, d.SkipReason)
		}
		if _, _, err := gitRunIn(dir, "git", "show-ref", "--verify", "--quiet", "refs/heads/fix/rejected"); err != nil {
			t.Error("worktree-held rejected branch was deleted")
		}
	})

	t.Run("remote-protected", func(t *testing.T) {
		dir, restore := makeGitRepo(t)
		defer restore()
		makeUnmergedBranch(t, dir, "fix/rejected")

		opts := classifyOpts(ix, true, false)
		opts.ProtectedRemote = map[string]bool{"fix/rejected": true}

		d := PruneBranch("fix/rejected", opts)
		if !d.Skipped || d.SkipReason != SkipGitHubProtected {
			t.Fatalf("expected SkipGitHubProtected, got skipped=%v reason=%q", d.Skipped, d.SkipReason)
		}
		if _, _, err := gitRunIn(dir, "git", "show-ref", "--verify", "--quiet", "refs/heads/fix/rejected"); err != nil {
			t.Error("protected rejected branch was deleted")
		}
	})
}

// ---- IsMergedWithIndex -------------------------------------------------------

// TestIsMergedWithIndex_GitPathWins confirms the index does not disturb the fast
// ancestry path of the OR-gate (invariant I4, signal a).
func TestIsMergedWithIndex_GitPathWins(t *testing.T) {
	dir, restore := makeGitRepo(t)
	defer restore()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup cmd %v: %v\n%s", args, err, out)
		}
	}
	// A branch pointing at main's tip is an ancestor of main → git-merged.
	run("git", "branch", "feat/ancestor", "main")

	merged, src, err := IsMergedWithIndex("feat/ancestor", "main", NewPRIndex(nil))
	if err != nil {
		t.Fatalf("IsMergedWithIndex: %v", err)
	}
	if !merged || src != MergeSourceGit {
		t.Errorf("merged=%v src=%q, want true/%q", merged, src, MergeSourceGit)
	}
}

// TestIsMergedWithIndex_PRMergedButShaNotAncestorRefuses keeps I4's second half
// honest on the indexed path: a MERGED PR whose merge commit is not reachable
// from base does NOT count as merged.
func TestIsMergedWithIndex_PRMergedButShaNotAncestorRefuses(t *testing.T) {
	dir, restore := makeGitRepo(t)
	defer restore()
	makeUnmergedBranch(t, dir, "fix/stray")

	tipOut, _, err := gitRunIn(dir, "git", "rev-parse", "fix/stray")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	tip := strings.TrimSpace(tipOut)
	// The branch tip is a real commit that is NOT an ancestor of main.
	ix := NewPRIndex([]PRInfo{{
		Number:      334,
		State:       PRStateMerged,
		HeadRefName: "fix/stray",
		MergeCommit: tip,
		HeadSHA:     tip,
	}})

	merged, src, err := IsMergedWithIndex("fix/stray", "main", ix)
	if err != nil {
		t.Fatalf("IsMergedWithIndex: %v", err)
	}
	if merged {
		t.Errorf("merged=%v src=%q, want false (merge commit is not an ancestor of main)", merged, src)
	}
}

// TestIsMergedWithIndex_SquashMergedBranchStillPrunes is the regression guard on
// the stray-tip discriminator. Under squash-merge, a cleanly merged branch's tip
// is NEVER an ancestor of base — if stray-tip were defined that way, every
// squash-merged branch would be held and prune would delete nothing. The head-SHA
// comparison must let this branch through as pr-verified.
func TestIsMergedWithIndex_SquashMergedBranchStillPrunes(t *testing.T) {
	dir, restore := makeGitRepo(t)
	defer restore()
	makeUnmergedBranch(t, dir, "feat/squashed")

	tipOut, _, err := gitRunIn(dir, "git", "rev-parse", "feat/squashed")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	tip := strings.TrimSpace(tipOut)

	// Simulate the squash landing on main: a NEW commit on main carrying the
	// branch's content, so the branch tip stays off main's ancestry.
	mustRun := func(args ...string) {
		t.Helper()
		if out, _, runErr := gitRunIn(dir, args...); runErr != nil {
			t.Fatalf("setup %v: %v\n%s", args, runErr, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "squashed.txt"), []byte("work\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	mustRun("git", "add", ".")
	mustRun("git", "commit", "-m", "squash merge of feat/squashed")

	squashOut, _, err := gitRunIn(dir, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}

	ix := NewPRIndex([]PRInfo{{
		Number:      12,
		State:       PRStateMerged,
		HeadRefName: "feat/squashed",
		MergeCommit: strings.TrimSpace(squashOut), // on main → ancestor
		HeadSHA:     tip,                          // branch never moved after the merge
	}})

	merged, src, err := IsMergedWithIndex("feat/squashed", "main", ix)
	if err != nil {
		t.Fatalf("IsMergedWithIndex: %v", err)
	}
	if !merged || src != MergeSourcePRVerified {
		t.Errorf("merged=%v src=%q, want true/%q — a squash-merged branch must stay prunable",
			merged, src, MergeSourcePRVerified)
	}
}

// TestIsMergedWithIndex_CommitsPushedAfterMergeAreRefused is the afterpay296
// case from B-0348: PR merged and its merge commit IS on base, but the branch
// picked up commits afterwards. Those commits exist nowhere else, so the branch
// must not be deleted.
func TestIsMergedWithIndex_CommitsPushedAfterMergeAreRefused(t *testing.T) {
	dir, restore := makeGitRepo(t)
	defer restore()
	makeUnmergedBranch(t, dir, "fix/afterpay296")

	mergedHeadOut, _, err := gitRunIn(dir, "git", "rev-parse", "fix/afterpay296")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	mergedHead := strings.TrimSpace(mergedHeadOut)

	mustRun := func(args ...string) {
		t.Helper()
		if out, _, runErr := gitRunIn(dir, args...); runErr != nil {
			t.Fatalf("setup %v: %v\n%s", args, runErr, out)
		}
	}
	// The squash lands on main…
	if err := os.WriteFile(filepath.Join(dir, "afterpay296.txt"), []byte("work\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	mustRun("git", "add", ".")
	mustRun("git", "commit", "-m", "squash merge of fix/afterpay296")
	squashOut, _, err := gitRunIn(dir, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}

	// …and afterwards someone pushes one more commit onto the branch.
	mustRun("git", "checkout", "fix/afterpay296")
	if err := os.WriteFile(filepath.Join(dir, "post-merge.txt"), []byte("late\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	mustRun("git", "add", ".")
	mustRun("git", "commit", "-m", "post-merge work")
	mustRun("git", "checkout", "main")

	ix := NewPRIndex([]PRInfo{{
		Number:      334,
		State:       PRStateMerged,
		HeadRefName: "fix/afterpay296",
		MergeCommit: strings.TrimSpace(squashOut),
		HeadSHA:     mergedHead, // what GitHub merged — the tip has moved past it
	}})

	merged, _, err := IsMergedWithIndex("fix/afterpay296", "main", ix)
	if err != nil {
		t.Fatalf("IsMergedWithIndex: %v", err)
	}
	if merged {
		t.Error("a branch with commits pushed after the merge must not count as merged")
	}

	// End to end: it surfaces as stray-tip, and --apply leaves it alone.
	d := PruneBranch("fix/afterpay296", classifyOpts(ix, true, false))
	if !d.Skipped || d.SkipReason != SkipStrayTip {
		t.Fatalf("expected SkipStrayTip, got skipped=%v reason=%q", d.Skipped, d.SkipReason)
	}
	if _, _, err := gitRunIn(dir, "git", "show-ref", "--verify", "--quiet", "refs/heads/fix/afterpay296"); err != nil {
		t.Error("stray-tip branch was deleted")
	}
}

// gitRunIn runs a git command inside dir. The prune tests chdir into the repo,
// but being explicit keeps these assertions independent of that.
func gitRunIn(dir string, args ...string) (string, string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}
