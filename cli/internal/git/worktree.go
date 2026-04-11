package git

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bravros/bravros/internal/config"
)

// WorktreeOpts configures the worktree setup operation.
type WorktreeOpts struct {
	NoRebase   bool
	BaseBranch string // auto-detected if empty
}

// WorktreeSetupResult holds the result of a worktree setup operation.
type WorktreeSetupResult struct {
	Path    string `json:"path"`
	Branch  string `json:"branch"`
	Created bool   `json:"created"`
	Error   string `json:"error,omitempty"`
}

// CleanupOpts configures the worktree cleanup operation.
type CleanupOpts struct {
	Force        bool
	DeleteRemote bool
}

// WorktreeCleanupResult holds the result of a worktree cleanup operation.
type WorktreeCleanupResult struct {
	Path          string `json:"path"`
	Removed       bool   `json:"removed"`
	BranchDeleted bool   `json:"branch_deleted"`
	Error         string `json:"error,omitempty"`
}

// extractPlanNum extracts a plan number from a branch name like "feat/0023-something".
var planNumRegex = regexp.MustCompile(`(\d{4})`)

func extractPlanNum(branch string) string {
	// Look for a 4-digit plan number in the branch name
	match := planNumRegex.FindString(branch)
	return match
}

// computeWorktreePath computes the worktree path from the repo root and branch name.
// Pattern: parentDir/repoName-planNum (e.g., /Users/x/Sites/myapp-0023)
func computeWorktreePath(repoRoot, branch string) string {
	parentDir := filepath.Dir(repoRoot)
	repoName := filepath.Base(repoRoot)

	planNum := extractPlanNum(branch)
	if planNum != "" {
		return filepath.Join(parentDir, repoName+"-"+planNum)
	}

	// Fallback: use branch name suffix
	suffix := strings.ReplaceAll(branch, "/", "-")
	return filepath.Join(parentDir, repoName+"-"+suffix)
}

// resolvePath returns the canonical absolute path, resolving symlinks.
func resolvePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	return resolved
}

// worktreeExists checks if a worktree at the given path is registered.
func worktreeExists(path string) bool {
	out, _, err := Run("git", "worktree", "list", "--porcelain")
	if err != nil {
		return false
	}
	resolvedPath := resolvePath(path)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			wtPath := strings.TrimPrefix(line, "worktree ")
			if wtPath == resolvedPath {
				return true
			}
		}
	}
	return false
}

// worktreeBranch returns the branch name for a worktree at the given path.
func worktreeBranch(path string) string {
	out, _, err := Run("git", "worktree", "list", "--porcelain")
	if err != nil {
		return ""
	}
	resolvedPath := resolvePath(path)
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			wtPath := strings.TrimPrefix(line, "worktree ")
			if wtPath == resolvedPath {
				// Look for "branch refs/heads/<name>" in following lines
				for j := i + 1; j < len(lines); j++ {
					if lines[j] == "" {
						break // end of this worktree entry
					}
					if strings.HasPrefix(lines[j], "branch ") {
						ref := strings.TrimPrefix(lines[j], "branch ")
						return strings.TrimPrefix(ref, "refs/heads/")
					}
				}
			}
		}
	}
	return ""
}

// WorktreeSetup creates a new git worktree for the given branch.
//
// Flow:
//  1. Load config for staging_branch / base branch
//  2. Compute path if not provided
//  3. git worktree add -b <branch> <path> (or attach existing branch)
//  4. Verify via git worktree list
//  5. If !NoRebase: rebase from base branch
//  6. Return result
func WorktreeSetup(branch, path string, opts WorktreeOpts) (*WorktreeSetupResult, error) {
	result := &WorktreeSetupResult{Branch: branch}

	// 1. Determine base branch
	baseBranch := opts.BaseBranch
	if baseBranch == "" {
		cfg, _ := config.LoadBravrosConfig()
		if cfg != nil && cfg.StagingBranch != "" {
			baseBranch = cfg.StagingBranch
		} else {
			baseBranch = "main"
		}
	}

	// 2. Compute path if not provided
	if path == "" {
		repoRoot, _, err := Run("git", "rev-parse", "--show-toplevel")
		if err != nil {
			return nil, fmt.Errorf("not in a git repository")
		}
		path = computeWorktreePath(repoRoot, branch)
	}
	result.Path = path

	// Check for duplicate
	if worktreeExists(path) {
		return nil, fmt.Errorf("worktree already exists at %s", path)
	}

	// 3. Create worktree
	if BranchExists(branch) {
		// Branch exists — attach it to the worktree
		_, stderr, err := Run("git", "worktree", "add", path, branch)
		if err != nil {
			return nil, fmt.Errorf("failed to create worktree: %s", stderr)
		}
	} else {
		// Create new branch in worktree
		_, stderr, err := Run("git", "worktree", "add", "-b", branch, path, baseBranch)
		if err != nil {
			return nil, fmt.Errorf("failed to create worktree: %s", stderr)
		}
	}

	// 4. Verify
	if !worktreeExists(path) {
		return nil, fmt.Errorf("worktree creation failed: not found in worktree list")
	}

	result.Created = true

	// 5. Rebase if requested
	if !opts.NoRebase {
		_, stderr, err := RunInDir(path, "git", "rebase", baseBranch)
		if err != nil {
			// Rebase failed — abort and report (worktree is still usable)
			RunInDir(path, "git", "rebase", "--abort")
			// Non-fatal: worktree was created successfully, just rebase failed
			result.Error = fmt.Sprintf("rebase failed (worktree created): %s", stderr)
		}
	}

	return result, nil
}

// WorktreeCleanup removes a git worktree and optionally deletes the branch.
//
// Flow:
//  1. Verify worktree exists
//  2. Get branch name from worktree
//  3. git worktree remove <path> (--force if Force=true)
//  4. Check if permanent branch → skip branch deletion
//  5. Delete local branch
//  6. If DeleteRemote: delete remote branch
//  7. Return result
func WorktreeCleanup(path string, opts CleanupOpts) (*WorktreeCleanupResult, error) {
	result := &WorktreeCleanupResult{Path: path}

	// 1. Verify worktree exists
	if !worktreeExists(path) {
		return nil, fmt.Errorf("no worktree found at %s", path)
	}

	// 2. Get branch name
	branch := worktreeBranch(path)

	// 3. Remove worktree
	removeArgs := []string{"git", "worktree", "remove", path}
	if opts.Force {
		removeArgs = []string{"git", "worktree", "remove", "--force", path}
	}
	_, stderr, err := Run(removeArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to remove worktree: %s", stderr)
	}

	result.Removed = true

	// 4-5. Delete branch if not permanent
	if branch != "" {
		cfg, _ := config.LoadBravrosConfig()
		if IsPermanentBranch(branch, cfg) {
			// Never delete permanent branches
			return result, nil
		}

		// Delete local branch
		deleteFlag := "-d"
		if opts.Force {
			deleteFlag = "-D"
		}
		_, _, delErr := Run("git", "branch", deleteFlag, branch)
		if delErr == nil {
			result.BranchDeleted = true
		}

		// 6. Delete remote branch if requested
		if opts.DeleteRemote {
			Run("git", "push", "origin", "--delete", branch)
		}
	}

	return result, nil
}
