package git

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bravros/private/internal/config"
)

// BranchCreateResult holds the result of a branch creation operation.
type BranchCreateResult struct {
	Branch       string `json:"branch"`
	Base         string `json:"base"`
	Created      bool   `json:"created"`
	CheckoutOnly bool   `json:"checkout_only,omitempty"`
}

// resolveBaseBranch determines the base branch to use.
// Priority: .bravros.yml staging_branch → DetectBaseBranch()
func resolveBaseBranch(repo *Repo) string {
	cfg, found := config.LoadBravrosConfig()
	if found && cfg.StagingBranch != "" {
		// Verify the staging branch actually exists
		_, _, err := Run("git", "show-ref", "--verify", "refs/heads/"+cfg.StagingBranch)
		if err == nil {
			return cfg.StagingBranch
		}
		// Also check remote
		_, _, err = Run("git", "show-ref", "--verify", "refs/remotes/origin/"+cfg.StagingBranch)
		if err == nil {
			return cfg.StagingBranch
		}
	}
	return repo.DetectBaseBranch()
}

// BranchExists checks if a local branch exists.
func BranchExists(name string) bool {
	_, _, err := Run("git", "show-ref", "--verify", "refs/heads/"+name)
	return err == nil
}

// CreateBranch creates a new branch from the detected base branch.
// It checks out the base, pulls latest, and creates the new branch.
func CreateBranch(name string) (*BranchCreateResult, error) {
	repo, err := Open("")
	if err != nil {
		return nil, fmt.Errorf("not in a git repository: %w", err)
	}

	base := resolveBaseBranch(repo)

	// Check if branch already exists
	if BranchExists(name) {
		return nil, fmt.Errorf("branch %q already exists", name)
	}

	// Checkout base branch
	if _, stderr, err := Run("git", "checkout", base); err != nil {
		return nil, fmt.Errorf("failed to checkout %s: %s", base, stderr)
	}

	// Pull latest (best-effort — may fail if no remote tracking)
	pullCmd := exec.Command("git", "pull", "origin", base)
	pullCmd.Stdout = os.Stdout
	pullCmd.Stderr = os.Stderr
	_ = pullCmd.Run() // ignore errors (e.g., no remote)

	// Create new branch
	if _, stderr, err := Run("git", "checkout", "-b", name); err != nil {
		return nil, fmt.Errorf("failed to create branch %s: %s", name, stderr)
	}

	return &BranchCreateResult{
		Branch:  name,
		Base:    base,
		Created: true,
	}, nil
}

// CheckoutBase checks out the base branch and pulls latest.
// Used by /plan skill to sync base without creating a new branch.
func CheckoutBase() (*BranchCreateResult, error) {
	repo, err := Open("")
	if err != nil {
		return nil, fmt.Errorf("not in a git repository: %w", err)
	}

	base := resolveBaseBranch(repo)

	// Checkout base branch
	if _, stderr, err := Run("git", "checkout", base); err != nil {
		return nil, fmt.Errorf("failed to checkout %s: %s", base, stderr)
	}

	// Pull latest
	pullCmd := exec.Command("git", "pull", "origin", base)
	pullCmd.Stdout = os.Stdout
	pullCmd.Stderr = os.Stderr
	_ = pullCmd.Run()

	return &BranchCreateResult{
		Branch:       base,
		Base:         base,
		Created:      false,
		CheckoutOnly: true,
	}, nil
}

// RunInDir executes a command in the given directory and returns (stdout, stderr, error).
func RunInDir(dir string, args ...string) (string, string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}
