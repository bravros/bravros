package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// initTestRepo and gitRun are defined in merge_test.go (same package)

func TestWorktreeSetup(t *testing.T) {
	repoDir := initTestRepo(t)

	// Change to the repo dir so git commands work
	origDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origDir)

	branch := "feat/0042-test-feature"
	wtPath := filepath.Join(t.TempDir(), "test-worktree")

	result, err := WorktreeSetup(branch, wtPath, WorktreeOpts{
		NoRebase:   true,
		BaseBranch: "main",
	})
	if err != nil {
		t.Fatalf("WorktreeSetup failed: %v", err)
	}

	if !result.Created {
		t.Error("expected Created=true")
	}
	if result.Path != wtPath {
		t.Errorf("expected Path=%q, got %q", wtPath, result.Path)
	}
	if result.Branch != branch {
		t.Errorf("expected Branch=%q, got %q", branch, result.Branch)
	}

	// Verify the worktree directory exists
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Error("worktree directory was not created")
	}

	// Verify it shows up in worktree list
	if !worktreeExists(wtPath) {
		t.Error("worktree not found in git worktree list")
	}
}

func TestWorktreeSetupAutoPath(t *testing.T) {
	repoDir := initTestRepo(t)

	origDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origDir)

	branch := "feat/0099-auto-path"

	result, err := WorktreeSetup(branch, "", WorktreeOpts{
		NoRebase:   true,
		BaseBranch: "main",
	})
	if err != nil {
		t.Fatalf("WorktreeSetup with auto path failed: %v", err)
	}
	defer func() {
		// Clean up the auto-created worktree
		Run("git", "worktree", "remove", "--force", result.Path)
	}()

	if !result.Created {
		t.Error("expected Created=true")
	}

	// Path should contain the plan number
	if !strings.Contains(result.Path, "0099") {
		t.Errorf("expected auto path to contain plan number '0099', got %q", result.Path)
	}
}

func TestWorktreeCleanup(t *testing.T) {
	repoDir := initTestRepo(t)

	origDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origDir)

	branch := "feat/0050-cleanup-test"
	wtPath := filepath.Join(t.TempDir(), "cleanup-worktree")

	// Setup first
	_, err := WorktreeSetup(branch, wtPath, WorktreeOpts{
		NoRebase:   true,
		BaseBranch: "main",
	})
	if err != nil {
		t.Fatalf("WorktreeSetup failed: %v", err)
	}

	// Cleanup
	result, err := WorktreeCleanup(wtPath, CleanupOpts{Force: true})
	if err != nil {
		t.Fatalf("WorktreeCleanup failed: %v", err)
	}

	if !result.Removed {
		t.Error("expected Removed=true")
	}
	if !result.BranchDeleted {
		t.Error("expected BranchDeleted=true for feature branch")
	}

	// Verify worktree is gone
	if worktreeExists(wtPath) {
		t.Error("worktree still exists after cleanup")
	}

	// Verify branch is gone
	if BranchExists(branch) {
		t.Errorf("branch %q still exists after cleanup", branch)
	}
}

func TestWorktreeCleanupPermanentBranch(t *testing.T) {
	repoDir := initTestRepo(t)

	origDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origDir)

	// Create a "develop" branch (permanent) and a worktree for it
	branch := "develop"
	RunInDir(repoDir, "git", "branch", branch)

	wtPath := filepath.Join(t.TempDir(), "permanent-worktree")

	_, err := WorktreeSetup(branch, wtPath, WorktreeOpts{
		NoRebase:   true,
		BaseBranch: "main",
	})
	if err != nil {
		t.Fatalf("WorktreeSetup failed: %v", err)
	}

	// Cleanup
	result, err := WorktreeCleanup(wtPath, CleanupOpts{Force: true})
	if err != nil {
		t.Fatalf("WorktreeCleanup failed: %v", err)
	}

	if !result.Removed {
		t.Error("expected Removed=true")
	}
	if result.BranchDeleted {
		t.Error("expected BranchDeleted=false for permanent branch 'develop'")
	}

	// Verify branch still exists
	if !BranchExists(branch) {
		t.Errorf("permanent branch %q was deleted", branch)
	}
}

func TestWorktreeDuplicateDetection(t *testing.T) {
	repoDir := initTestRepo(t)

	origDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origDir)

	branch := "feat/0060-duplicate"
	wtPath := filepath.Join(t.TempDir(), "dup-worktree")

	// Setup first
	_, err := WorktreeSetup(branch, wtPath, WorktreeOpts{
		NoRebase:   true,
		BaseBranch: "main",
	})
	if err != nil {
		t.Fatalf("first WorktreeSetup failed: %v", err)
	}

	// Try to create again at the same path — should fail
	_, err = WorktreeSetup("feat/0061-other", wtPath, WorktreeOpts{
		NoRebase:   true,
		BaseBranch: "main",
	})
	if err == nil {
		t.Error("expected error for duplicate worktree path, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestWorktreeCleanupMissingPath(t *testing.T) {
	repoDir := initTestRepo(t)

	origDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origDir)

	_, err := WorktreeCleanup("/nonexistent/path/that/does/not/exist", CleanupOpts{})
	if err == nil {
		t.Error("expected error for missing worktree path, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "no worktree found") {
		t.Errorf("expected 'no worktree found' error, got: %v", err)
	}
}

func TestExtractPlanNum(t *testing.T) {
	tests := []struct {
		branch   string
		expected string
	}{
		{"feat/0023-add-worktree", "0023"},
		{"fix/0001-bug", "0001"},
		{"refactor/some-thing", ""},
		{"feat/0099-test", "0099"},
	}

	for _, tc := range tests {
		got := extractPlanNum(tc.branch)
		if got != tc.expected {
			t.Errorf("extractPlanNum(%q) = %q, want %q", tc.branch, got, tc.expected)
		}
	}
}

func TestComputeWorktreePath(t *testing.T) {
	tests := []struct {
		repoRoot string
		branch   string
		contains string
	}{
		{"/Users/x/Sites/myapp", "feat/0023-thing", "myapp-0023"},
		{"/Users/x/Sites/myapp", "refactor/no-number", "myapp-refactor-no-number"},
	}

	for _, tc := range tests {
		got := computeWorktreePath(tc.repoRoot, tc.branch)
		if !strings.Contains(got, tc.contains) {
			t.Errorf("computeWorktreePath(%q, %q) = %q, expected to contain %q",
				tc.repoRoot, tc.branch, got, tc.contains)
		}
	}
}
