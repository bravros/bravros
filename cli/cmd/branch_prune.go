package cmd

import (
	"fmt"
	"strings"

	branchpkg "github.com/bravros/bravros/cli/internal/branch"
	gitpkg "github.com/bravros/bravros/cli/internal/git"
	"github.com/bravros/bravros/cli/internal/trash"
	"github.com/spf13/cobra"
)

var (
	branchPruneApply           bool
	branchPruneGC              bool
	branchPruneBase            string
	branchPruneExcludeRejected bool
)

var branchPruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Identify and delete local branches merged into the base branch",
	Long: `prune inspects local branches against the base branch and deletes those that
pass all safety guards: not protected, not HEAD, no open plan references,
no active worktree. Merge detection uses an OR gate — git branch --merged
OR a verified merged GitHub PR.

The worktree guard is fail-closed: "git worktree list --porcelain" is parsed
ONCE up front, and if the listing fails the whole run aborts. A branch checked
out in any worktree is reported as SKIPPED-WORKTREE — even when merged into
the base — because worktree teardown is owned by /worktree destroy
(kaisser worktree cleanup <path>), never by prune.

Every pull request in the repo is fetched ONCE up front (gh api --paginate) and
indexed by head branch, so classification costs one round-trip for the whole
run rather than one per branch. Branches the merge check refuses are then
subdivided by PR state:

  rejected   every PR is CLOSED (none open, none merged) — deliberately
             rejected work. DELETED BY DEFAULT under --apply, tombstoned
             first, exactly like a merged branch. Opt out: --exclude-rejected.
  in-flight  at least one PR is OPEN — kept.
  stray-tip  a PR merged but the tip carries commits beyond it — kept and
             surfaced for human review.
  no-pr      no PR was ever opened — kept, NEVER auto-deleted.

If the PR index cannot be fetched, classification is skipped entirely: every
refusal reports the flat "unmerged" reason and nothing is treated as rejected.

By default (no flags) the command runs in dry-run mode and lists candidates
with their source attribution (git / pr / pr-verified / rejected).

Flags:
  --apply              Delete branches passing all guards (writes tombstone first).
  --gc                 Sweep 7+ day tombstones, reap orphaned review-stamps
                       (.planning/.review-stamp-*.json whose PR is MERGED/CLOSED,
                       fail-closed on uncertainty) AND reap .trash/ entries older
                       than 30 days; skip normal prune.
  --base <ref>         Override the base branch (default: auto-detected from kaisser meta).
  --exclude-rejected   Hold back rejected branches instead of deleting them.

Tombstones are written to refs/tombstones/<branch> and can be recovered within
7 days using: git checkout -b <branch> refs/tombstones/<branch-with-dashes>

Decision log: ~/.claude/logs/branch-prune.log`,
	SilenceUsage: true,
	RunE:         runBranchPrune,
}

func init() {
	branchPruneCmd.Flags().BoolVar(&branchPruneApply, "apply", false, "Delete branches passing all guards (dry-run by default)")
	branchPruneCmd.Flags().BoolVar(&branchPruneGC, "gc", false, "Sweep 7+ day tombstones AND reap orphaned review-stamps (fail-closed)")
	branchPruneCmd.Flags().StringVar(&branchPruneBase, "base", "", "Base branch to check merge against (default: auto-detected)")
	branchPruneCmd.Flags().BoolVar(&branchPruneExcludeRejected, "exclude-rejected", false, "Keep branches whose every PR is CLOSED (they are deleted by default)")
}

func runBranchPrune(cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()

	// Mutual-exclusion notice: --gc and --apply do unrelated work. If both flags
	// are set we honour --gc (GC mode exits early) and warn loud so the caller's
	// expectation gets challenged. Continuing silently with one flag winning was
	// the friction point flagged in PR #278 review.
	if branchPruneGC && branchPruneApply {
		fmt.Fprintln(out, "⚠️  --gc and --apply are mutually exclusive; --gc wins (skipping the prune pass).")
	}

	// --gc mode: sweep tombstones AND orphaned review-stamps, then exit.
	if branchPruneGC {
		pruned, err := branchpkg.GCTombstones(0)
		if err != nil {
			return fmt.Errorf("gc tombstones: %w", err)
		}

		// Reap orphaned .planning/.review-stamp-<PR>.json files whose PR has
		// already landed (MERGED/CLOSED). /finish deletes its own stamp, but
		// /auto-merge and /address-pr leak theirs — this clears the orphans.
		// Fail-closed: any gh lookup error keeps the stamp (never delete on
		// uncertainty). Errors are non-fatal — the tombstone GC already ran.
		reapedStamps, _ := branchpkg.GCReviewStamps()

		// Sweep the .trash/ preserve area (kaisser discard / clean-untracked
		// copies) past its 30-day retention window. Non-fatal, same as stamps.
		var reapedTrash []string
		if root, err := trash.RepoRoot(""); err == nil {
			reapedTrash, _ = trash.GC(root, 0)
		}

		if len(pruned) == 0 && len(reapedStamps) == 0 && len(reapedTrash) == 0 {
			fmt.Fprintln(out, "No tombstones, orphaned review-stamps, or expired trash entries eligible for GC.")
			return nil
		}
		if len(pruned) == 0 {
			fmt.Fprintln(out, "No tombstones eligible for GC (all younger than 7 days).")
		} else {
			fmt.Fprintf(out, "GC'd %d tombstone(s):\n", len(pruned))
			for _, ref := range pruned {
				fmt.Fprintf(out, "  %s\n", ref)
			}
		}
		if len(reapedStamps) > 0 {
			fmt.Fprintf(out, "Reaped %d orphaned review-stamp(s):\n", len(reapedStamps))
			for _, path := range reapedStamps {
				fmt.Fprintf(out, "  %s\n", path)
			}
		}
		if len(reapedTrash) > 0 {
			fmt.Fprintf(out, "Reaped %d expired trash entr%s:\n", len(reapedTrash), pluralY(len(reapedTrash)))
			for _, id := range reapedTrash {
				fmt.Fprintf(out, "  .trash/%s\n", id)
			}
		}
		return nil
	}

	// Resolve base branch.
	base := branchPruneBase
	if base == "" {
		repo, err := gitpkg.Open("")
		if err != nil {
			return fmt.Errorf("open git repo: %w", err)
		}
		base = repo.DetectBaseBranch()
	}

	// Resolve repo name for log attribution.
	repoName := gitpkg.ProjectName()

	// Parse the worktree set ONCE up front, shared across the whole candidate
	// loop. FAIL-CLOSED: if the listing errors we cannot know which branches
	// back live worktrees, so the whole prune run aborts — deleting blind is
	// exactly how a live worktree's branch gets nuked.
	worktrees, err := branchpkg.WorktreeBranches()
	if err != nil {
		return fmt.Errorf("aborting prune (fail-closed): cannot list worktrees: %w", err)
	}

	// One upfront `git remote prune origin` for the whole pass — it used to run
	// once per candidate, which is one network round-trip per branch.
	remoteSynced := branchpkg.SyncRemoteRefs() == nil

	// Fetch EVERY PR in the repo once and index it by head branch (B-0348).
	// A failed fetch is non-fatal but degrades classification: the run then
	// reports the flat `unmerged` reason and never deletes a rejected branch.
	prIndex, prErr := branchpkg.BuildPRIndex()
	if prErr != nil {
		fmt.Fprintf(out, "⚠️  PR index unavailable (%v)\n", prErr)
		fmt.Fprintln(out, "    Unmerged branches stay classified as `unmerged`; no rejected-PR branch will be deleted.")
	}

	// Fetch the remote-protected branch set once (Guard 2). A nil map on error
	// falls back to the per-branch protection API call.
	protectedRemote, protErr := branchpkg.ProtectedBranchSet()
	if protErr != nil {
		protectedRemote = nil
	}

	// List local + remote-only branch candidates (deduped).
	branches, err := branchpkg.ListAllBranches()
	if err != nil {
		return fmt.Errorf("listing branches: %w", err)
	}

	if len(branches) == 0 {
		fmt.Fprintln(out, "No local branches to inspect.")
		return nil
	}

	mode := "dry-run"
	if branchPruneApply {
		mode = "apply"
	}

	fmt.Fprintf(out, "Branch prune (%s, base: %s)\n", mode, base)
	if prIndex.Available() {
		rejectedPolicy := "deleted by default"
		if branchPruneExcludeRejected {
			rejectedPolicy = "held (--exclude-rejected)"
		}
		fmt.Fprintf(out, "PR index: %d pull request(s) — rejected branches %s\n", prIndex.Total(), rejectedPolicy)
	}
	fmt.Fprintln(out, strings.Repeat("-", 60))

	opts := branchpkg.PruneOpts{
		Base:             base,
		Apply:            branchPruneApply,
		RepoName:         repoName,
		WorktreeBranches: worktrees,
		PRIndex:          prIndex,
		ExcludeRejected:  branchPruneExcludeRejected,
		ProtectedRemote:  protectedRemote,
		RemoteSynced:     remoteSynced,
	}

	var candidates, rejectedCandidates, skipped, deleted, rejectedDeleted, errors int
	for _, branch := range branches {
		d := branchpkg.PruneBranch(branch, opts)
		switch {
		case d.Skipped && d.SkipReason == branchpkg.SkipWorktree:
			skipped++
			fmt.Fprintf(out, "  [SKIP]      %s  SKIPPED-WORKTREE (%s)\n", d.Branch, d.SkipDetail)
		case d.Skipped:
			skipped++
			detail := ""
			if d.SkipDetail != "" {
				detail = " (" + d.SkipDetail + ")"
			}
			fmt.Fprintf(out, "  [SKIP]      %s  reason=%s%s\n", d.Branch, d.SkipReason, detail)
		case d.Error != "":
			errors++
			fmt.Fprintf(out, "  [ERROR]     %s  %s\n", d.Branch, d.Error)
		case (d.Merged || d.Rejected) && !branchPruneApply:
			candidates++
			if d.Rejected {
				rejectedCandidates++
			}
			fmt.Fprintf(out, "  [CANDIDATE] %s  source=%s\n", d.Branch, pruneSourceLabel(d))
		case d.Deleted:
			deleted++
			if d.Rejected {
				rejectedDeleted++
			}
			fmt.Fprintf(out, "  [DELETED]   %s  source=%s  tombstone=%s\n", d.Branch, pruneSourceLabel(d), d.Tombstone)
		default:
			skipped++
			fmt.Fprintf(out, "  [SKIP]      %s  (no merge detected)\n", d.Branch)
		}
	}

	fmt.Fprintln(out, strings.Repeat("-", 60))
	if branchPruneApply {
		fmt.Fprintf(out, "  %d deleted (%d rejected-PR), %d skipped, %d errors\n", deleted, rejectedDeleted, skipped, errors)
		if deleted > 0 {
			fmt.Fprintln(out, "  Tombstones written — recover via: git checkout -b <name> <tombstone-ref>")
		}
	} else {
		fmt.Fprintf(out, "  %d candidate(s) eligible for deletion (%d rejected-PR), %d skipped\n", candidates, rejectedCandidates, skipped)
		if candidates > 0 {
			fmt.Fprintln(out, "  (no changes made — pass --apply to delete)")
		}
	}

	if errors > 0 {
		return fmt.Errorf("%d branch(es) encountered errors during prune", errors)
	}
	return nil
}

// pruneSourceLabel renders the `source=` attribution for a deletion candidate:
// the merge source for merged branches, or `rejected (all PRs closed: #N, #M)`
// for branches whose every PR was closed without merging.
func pruneSourceLabel(d branchpkg.PruneDecision) string {
	if d.Rejected {
		return branchpkg.FormatRejectedSource(d.PRNumbers)
	}
	return string(d.MergeSource)
}
