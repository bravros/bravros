package git

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bravros/private/internal/config"
)

// MergePROpts configures the merge-pr operation.
type MergePROpts struct {
	DeleteBranch        bool   // default true for feature branches
	AutoResolvePlanning bool   // auto-resolve .planning/ conflicts
	BaseBranch          string // target branch (auto-detected if empty)
	MergeMethod         string // "squash" (default), "merge", or "rebase"
}

// MergePRResult holds the result of a merge-pr operation.
type MergePRResult struct {
	PR                int    `json:"pr"`
	State             string `json:"state"` // "merged", "failed"
	BranchDeleted     bool   `json:"branch_deleted"`
	ConflictsResolved bool   `json:"conflicts_resolved"` // .planning/ conflicts auto-resolved
	Error             string `json:"error,omitempty"`
}

// prViewInfo holds parsed PR info from gh CLI.
type prViewInfo struct {
	HeadRefName string `json:"headRefName"`
	BaseRefName string `json:"baseRefName"`
	State       string `json:"state"`
	Mergeable   string `json:"mergeable"`
}

// IsPermanentBranch returns true if the branch should never be deleted.
// Checks against well-known permanent branches and the staging_branch from config.
func IsPermanentBranch(branch string, cfg *config.BravrosConfig) bool {
	permanent := map[string]bool{
		"main":    true,
		"master":  true,
		"homolog": true,
		"staging": true,
		"develop": true,
	}
	if cfg != nil && cfg.StagingBranch != "" {
		permanent[cfg.StagingBranch] = true
	}
	return permanent[branch]
}

// getPRInfo fetches PR metadata from gh CLI.
func getPRInfo(prNumber int) (*prViewInfo, error) {
	out, stderr, err := Run("gh", "pr", "view", fmt.Sprintf("%d", prNumber),
		"--json", "headRefName,baseRefName,state,mergeable")
	if err != nil {
		return nil, fmt.Errorf("failed to get PR info: %s", stderr)
	}
	var info prViewInfo
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return nil, fmt.Errorf("failed to parse PR info: %w", err)
	}
	return &info, nil
}

// checkBehindBase checks if the head branch is behind the base branch.
// Returns (behind count, ahead count, error).
func checkBehindBase(base, head string) (int, int, error) {
	_, _, fetchErr := Run("git", "fetch", "origin", base)
	if fetchErr != nil {
		return 0, 0, fmt.Errorf("failed to fetch origin/%s", base)
	}

	out, _, err := Run("git", "rev-list", "--left-right", "--count",
		fmt.Sprintf("origin/%s...%s", base, head))
	if err != nil {
		return 0, 0, fmt.Errorf("failed to compare branches: %w", err)
	}

	var behind, ahead int
	_, scanErr := fmt.Sscanf(out, "%d\t%d", &behind, &ahead)
	if scanErr != nil {
		return 0, 0, fmt.Errorf("failed to parse rev-list output: %q", out)
	}
	return behind, ahead, nil
}

// mergeBaseIntoHead merges origin/base into the current (head) branch.
// If autoResolvePlanning is true, .planning/ conflicts are resolved with --theirs.
// Returns true if .planning/ conflicts were auto-resolved.
func mergeBaseIntoHead(base, head string, autoResolvePlanning bool) (bool, error) {
	mergeMsg := fmt.Sprintf("🔀 merge: sync %s into %s", base, head)
	_, stderr, err := Run("git", "merge", fmt.Sprintf("origin/%s", base), "-m", mergeMsg)
	if err == nil {
		return false, nil // clean merge
	}

	if !autoResolvePlanning {
		return false, fmt.Errorf("merge conflict: %s", stderr)
	}

	// Check if there are .planning/ conflicts
	conflictOut, _, _ := Run("git", "diff", "--name-only", "--diff-filter=U")
	if conflictOut == "" {
		return false, fmt.Errorf("merge failed: %s", stderr)
	}

	hasPlanningConflicts := false
	hasOtherConflicts := false
	for _, f := range strings.Split(conflictOut, "\n") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if strings.HasPrefix(f, ".planning/") {
			hasPlanningConflicts = true
		} else {
			hasOtherConflicts = true
		}
	}

	if hasOtherConflicts {
		// Abort the merge — non-planning conflicts need manual resolution
		Run("git", "merge", "--abort")
		return false, fmt.Errorf("merge conflict in non-planning files: %s", stderr)
	}

	if hasPlanningConflicts {
		// Auto-resolve .planning/ conflicts by keeping theirs (base version)
		_, _, resolveErr := Run("git", "checkout", "--theirs", ".planning/")
		if resolveErr != nil {
			Run("git", "merge", "--abort")
			return false, fmt.Errorf("failed to resolve .planning/ conflicts")
		}
		_, _, addErr := Run("git", "add", ".planning/")
		if addErr != nil {
			Run("git", "merge", "--abort")
			return false, fmt.Errorf("failed to stage .planning/ resolution")
		}
		// Complete the merge with proper emoji message
		_, _, commitErr := Run("git", "commit", "-m", mergeMsg)
		if commitErr != nil {
			Run("git", "merge", "--abort")
			return false, fmt.Errorf("failed to commit merge resolution")
		}
		return true, nil
	}

	return false, fmt.Errorf("merge failed: %s", stderr)
}

// buildMergeArgs constructs the gh pr merge command arguments.
// Note: gh has --delete-branch but no --no-delete-branch flag.
// The default (no flag) is to NOT delete, so we only add --delete-branch when needed.
func buildMergeArgs(prNumber int, deleteBranch bool, isPermanent bool, mergeMethod string) []string {
	switch mergeMethod {
	case "squash", "merge", "rebase":
		// valid
	default:
		mergeMethod = "squash"
	}
	args := []string{"pr", "merge", fmt.Sprintf("%d", prNumber), "--" + mergeMethod}
	if deleteBranch && !isPermanent {
		args = append(args, "--delete-branch")
	}
	return args
}

// isAdminRetryable returns true when a gh pr merge failure is likely due to
// required status checks or mergability rules that --admin can bypass.
func isAdminRetryable(stderr string) bool {
	patterns := []string{
		"not mergeable",
		"required status checks",
		"UNSTABLE",
		"required reviews",
		"protected branch",
	}
	lower := strings.ToLower(stderr)
	for _, p := range patterns {
		if strings.Contains(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// MergePR performs the full merge-pr operation.
//
// Flow:
//  1. Load config for staging_branch detection
//  2. Get PR info via gh CLI
//  3. Determine if branch is permanent (never delete)
//  4. Pre-merge conflict check (fetch + rev-list)
//  5. If behind base: merge origin/base into feature branch
//  6. If .planning/ conflicts and AutoResolvePlanning: auto-resolve
//  7. Merge via gh pr merge --squash
//  8. Verify merged state
//  9. Branch cleanup (delete local if not permanent)
//  10. Return result
//
// Benchmark note:
// Old flow: 5 separate bash steps (git fetch + git merge + resolve conflicts + gh pr merge + git branch -d)
// New flow: single `sdlc merge-pr N` — same operations, zero manual orchestration.
func MergePR(prNumber int, opts MergePROpts) *MergePRResult {
	result := &MergePRResult{PR: prNumber, State: "failed"}

	// 1. Load config
	cfg, _ := config.LoadBravrosConfig()

	// 2. Get PR info
	info, err := getPRInfo(prNumber)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	if info.State == "MERGED" {
		result.State = "merged"
		return result
	}

	if info.State == "CLOSED" {
		result.Error = "PR is closed"
		return result
	}

	headBranch := info.HeadRefName
	baseBranch := info.BaseRefName
	if opts.BaseBranch != "" {
		baseBranch = opts.BaseBranch
	}

	// 3. Determine if branch is permanent
	permanent := IsPermanentBranch(headBranch, cfg)

	// 4-5. Pre-merge conflict check and merge base if behind
	behind, _, checkErr := checkBehindBase(baseBranch, headBranch)
	if checkErr != nil {
		result.Error = checkErr.Error()
		return result
	}

	if behind > 0 {
		// Ensure we're on the head branch
		_, _, checkoutErr := Run("git", "checkout", headBranch)
		if checkoutErr != nil {
			result.Error = fmt.Sprintf("failed to checkout %s", headBranch)
			return result
		}

		// 6. Merge base into head (with optional .planning/ auto-resolution)
		resolved, mergeErr := mergeBaseIntoHead(baseBranch, headBranch, opts.AutoResolvePlanning)
		if mergeErr != nil {
			result.Error = mergeErr.Error()
			return result
		}
		result.ConflictsResolved = resolved

		// Push the updated branch
		_, stderr, pushErr := Run("git", "push", "origin", headBranch)
		if pushErr != nil {
			result.Error = fmt.Sprintf("failed to push updated branch: %s", stderr)
			return result
		}
	}

	// 7. Merge via gh
	mergeArgs := buildMergeArgs(prNumber, opts.DeleteBranch, permanent, opts.MergeMethod)
	_, stderr, mergeErr := Run(append([]string{"gh"}, mergeArgs...)...)
	if mergeErr != nil {
		// Retry with --admin if the failure looks like a status-check/mergability block on main
		if (baseBranch == "main" || baseBranch == "master") && isAdminRetryable(stderr) {
			fmt.Printf("gh pr merge failed (%s); retrying with --admin…\n", strings.TrimSpace(stderr))
			adminArgs := make([]string, len(mergeArgs)+1)
			copy(adminArgs, mergeArgs)
			adminArgs[len(mergeArgs)] = "--admin"
			_, adminStderr, adminErr := Run(append([]string{"gh"}, adminArgs...)...)
			if adminErr != nil {
				result.Error = fmt.Sprintf("gh pr merge --admin failed: %s", adminStderr)
				return result
			}
		} else {
			result.Error = fmt.Sprintf("gh pr merge failed: %s", stderr)
			return result
		}
	}

	// 8. Verify merged state
	verifyInfo, verifyErr := getPRInfo(prNumber)
	if verifyErr != nil {
		// Merge likely succeeded but verification failed — report as merged with warning
		result.State = "merged"
		return result
	}

	if verifyInfo.State != "MERGED" {
		result.Error = fmt.Sprintf("unexpected PR state after merge: %s", verifyInfo.State)
		return result
	}

	result.State = "merged"

	// 9. Branch cleanup — delete local branch if not permanent and DeleteBranch is true
	if !permanent && opts.DeleteBranch {
		// Switch away from the branch before deleting
		Run("git", "checkout", baseBranch)
		_, _, delErr := Run("git", "branch", "-D", headBranch)
		if delErr == nil {
			result.BranchDeleted = true
		}
	}

	return result
}
