package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

	// Path should contain the plan number with leading zeros stripped (0099 → 99).
	if !strings.HasSuffix(result.Path, "99") || strings.HasSuffix(result.Path, "099") {
		t.Errorf("expected auto path to end with stripped plan number '99', got %q", result.Path)
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
		{"/Users/x/Sites/myapp", "feat/0023-thing", "myapp23"},
		{"/Users/x/Sites/myapp", "feat/P-0111-vite-fonts", "myapp111"},
		{"/Users/x/Sites/myapp", "refactor/0091-audit-rule", "myapp91"},
		{"/Users/x/Sites/myapp", "feat/9999-bigid", "myapp9999"},
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

// ─── Behavior 5: ID/name normalization + collision pre-checks ──────────────

func TestNormalizeWorktreeIdentifier(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"P-0109", "109"},
		{"p-0109", "109"},
		{"0109", "109"},
		{"109", "109"},
		{"P109", "109"},
		{"P-109", "109"},
		{"0000", "0"},
		{"0", "0"},
		{"ui", "ui"},
		{"my-slug", "my-slug"},
		{"feat/0109-desc", "feat/0109-desc"},
		{"feat123", "feat123"},
		{"", ""},
	}

	for _, tc := range tests {
		got := NormalizeWorktreeIdentifier(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeWorktreeIdentifier(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestEvaluateWorktreeCollision(t *testing.T) {
	tests := []struct {
		name       string
		blocks     []worktreeBlock
		resolved   string
		raw        string
		branch     string
		dirExists  bool
		wantErr    bool
		wantSubstr string
	}{
		{
			name:     "no collision",
			blocks:   []worktreeBlock{{path: "/repo", branch: "main"}},
			resolved: "/repo/wt1",
			raw:      "/repo/wt1",
			branch:   "feat/x",
		},
		{
			name:       "path collision with registered worktree",
			blocks:     []worktreeBlock{{path: "/repo/wt1", branch: "feat/other"}},
			resolved:   "/repo/wt1",
			raw:        "/repo/wt1",
			branch:     "feat/x",
			wantErr:    true,
			wantSubstr: "already exists at",
		},
		{
			name:       "branch already checked out elsewhere",
			blocks:     []worktreeBlock{{path: "/repo/wt-other", branch: "feat/x"}},
			resolved:   "/repo/wt1",
			raw:        "/repo/wt1",
			branch:     "feat/x",
			wantErr:    true,
			wantSubstr: "already checked out",
		},
		{
			name:       "stray unregistered directory",
			resolved:   "/repo/wt1",
			raw:        "/repo/wt1",
			branch:     "feat/x",
			dirExists:  true,
			wantErr:    true,
			wantSubstr: "not a registered worktree",
		},
		{
			name:     "empty branch never collides on branch",
			blocks:   []worktreeBlock{{path: "/repo/wt-other", branch: ""}},
			resolved: "/repo/wt1",
			raw:      "/repo/wt1",
			branch:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := evaluateWorktreeCollision(tc.blocks, tc.resolved, tc.raw, tc.branch, tc.dirExists)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if tc.wantErr && !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}

func TestWorktreeSetupBranchCollision(t *testing.T) {
	repoDir := initTestRepo(t)

	origDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origDir)

	branch := "feat/0120-shared"
	firstPath := filepath.Join(t.TempDir(), "first-worktree")
	if _, err := WorktreeSetup(branch, firstPath, WorktreeOpts{NoRebase: true, BaseBranch: "main"}); err != nil {
		t.Fatalf("first WorktreeSetup failed: %v", err)
	}

	secondPath := filepath.Join(t.TempDir(), "second-worktree")
	_, err := WorktreeSetup(branch, secondPath, WorktreeOpts{NoRebase: true, BaseBranch: "main"})
	if err == nil {
		t.Fatal("expected error creating a second worktree for a branch already checked out elsewhere, got nil")
	}
	if !strings.Contains(err.Error(), "already checked out") {
		t.Errorf("expected 'already checked out' error, got: %v", err)
	}
}

// ─── Behavior 4: merge-checked destroy (--dry-run / --force) ──────────────

func TestEvaluateDestroyGuard(t *testing.T) {
	tests := []struct {
		name    string
		branch  string
		base    string
		checked bool
		merged  bool
		force   bool
		wantErr bool
	}{
		{"force bypasses everything", "feat/x", "main", true, false, true, false},
		{"empty branch always proceeds", "", "main", true, false, false, false},
		{"empty base always proceeds", "feat/x", "", true, false, false, false},
		{"indeterminate merge status never blocks", "feat/x", "main", false, false, false, false},
		{"merged proceeds", "feat/x", "main", true, true, false, false},
		{"unmerged blocks without force", "feat/x", "main", true, false, false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := evaluateDestroyGuard(tc.branch, tc.base, tc.checked, tc.merged, tc.force)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}

func TestBuildTeardownScope(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		branch        string
		baseBranch    string
		permanent     bool
		opts          CleanupOpts
		wantSubstrs   []string
		wantNoSubstrs []string
	}{
		{
			name:        "full scope with remote delete",
			path:        "/repo/wt1",
			branch:      "feat/x",
			baseBranch:  "main",
			opts:        CleanupOpts{DeleteRemote: true},
			wantSubstrs: []string{"worktree dir: /repo/wt1", "local branch: feat/x", "remote branch: origin/feat/x", "merge check"},
		},
		{
			name:          "no remote delete",
			path:          "/repo/wt2",
			branch:        "feat/y",
			baseBranch:    "main",
			opts:          CleanupOpts{},
			wantSubstrs:   []string{"worktree dir: /repo/wt2", "local branch: feat/y", "merge check"},
			wantNoSubstrs: []string{"remote branch:"},
		},
		{
			name:          "detached branch",
			path:          "/repo/wt3",
			branch:        "",
			baseBranch:    "main",
			opts:          CleanupOpts{},
			wantSubstrs:   []string{"worktree dir: /repo/wt3", "local branch: (detached"},
			wantNoSubstrs: []string{"merge check"},
		},
		{
			name:          "permanent branch omits merge check and deletions",
			path:          "/repo/wt4",
			branch:        "homolog",
			baseBranch:    "main",
			permanent:     true,
			opts:          CleanupOpts{DeleteRemote: true},
			wantSubstrs:   []string{"worktree dir: /repo/wt4", "local branch: homolog (permanent — will NOT be deleted)"},
			wantNoSubstrs: []string{"merge check", "remote branch:"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scope := buildTeardownScope(tc.path, tc.branch, tc.baseBranch, tc.permanent, tc.opts)
			joined := strings.Join(scope, "\n")
			for _, want := range tc.wantSubstrs {
				if !strings.Contains(joined, want) {
					t.Errorf("scope %v missing expected substring %q", scope, want)
				}
			}
			for _, notWant := range tc.wantNoSubstrs {
				if strings.Contains(joined, notWant) {
					t.Errorf("scope %v unexpectedly contains %q", scope, notWant)
				}
			}
		})
	}
}

func TestWorktreeCleanupDryRun(t *testing.T) {
	repoDir := initTestRepo(t)

	origDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origDir)

	branch := "feat/0110-dryrun"
	wtPath := filepath.Join(t.TempDir(), "dryrun-worktree")
	if _, err := WorktreeSetup(branch, wtPath, WorktreeOpts{NoRebase: true, BaseBranch: "main"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	result, err := WorktreeCleanup(wtPath, CleanupOpts{DryRun: true, DeleteRemote: true, BaseBranch: "main"})
	if err != nil {
		t.Fatalf("dry-run cleanup failed: %v", err)
	}
	if !result.DryRun {
		t.Error("expected DryRun=true")
	}
	if result.Removed {
		t.Error("dry-run must not remove the worktree")
	}
	if len(result.Scope) == 0 {
		t.Error("expected a non-empty teardown scope")
	}
	if !worktreeExists(wtPath) {
		t.Error("dry-run must leave the worktree in place")
	}
}

func TestWorktreeCleanupMergeGuard(t *testing.T) {
	repoDir := initTestRepo(t)

	origDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origDir)

	// Wire up a bare "origin" remote so origin/main resolves.
	originDir := t.TempDir()
	gitRun(t, repoDir, "git", "init", "--bare", originDir)
	gitRun(t, repoDir, "git", "remote", "add", "origin", originDir)
	gitRun(t, repoDir, "git", "push", "origin", "main")

	// Branch off main with no new commits — it IS an ancestor of
	// origin/main, so a plain (non-forced) destroy should succeed.
	mergedBranch := "feat/0100-merged"
	mergedPath := filepath.Join(t.TempDir(), "merged-worktree")
	if _, err := WorktreeSetup(mergedBranch, mergedPath, WorktreeOpts{NoRebase: true, BaseBranch: "main"}); err != nil {
		t.Fatalf("setup merged branch failed: %v", err)
	}
	if _, err := WorktreeCleanup(mergedPath, CleanupOpts{BaseBranch: "main"}); err != nil {
		t.Fatalf("expected merged branch cleanup to succeed, got: %v", err)
	}

	// A branch with a new local commit is NOT an ancestor of origin/main —
	// destroy must refuse unless --force.
	unmergedBranch := "feat/0101-unmerged"
	unmergedPath := filepath.Join(t.TempDir(), "unmerged-worktree")
	if _, err := WorktreeSetup(unmergedBranch, unmergedPath, WorktreeOpts{NoRebase: true, BaseBranch: "main"}); err != nil {
		t.Fatalf("setup unmerged branch failed: %v", err)
	}
	writeFile(t, unmergedPath, "new-file.txt", "unmerged change")
	gitRun(t, unmergedPath, "git", "add", ".")
	gitRun(t, unmergedPath, "git", "commit", "-m", "unmerged commit")

	_, err := WorktreeCleanup(unmergedPath, CleanupOpts{BaseBranch: "main"})
	if err == nil {
		t.Fatal("expected error destroying an unmerged branch without --force, got nil")
	}
	if !strings.Contains(err.Error(), "not merged into origin/main") {
		t.Errorf("expected 'not merged into origin/main' error, got: %v", err)
	}

	// --force bypasses the guard.
	if _, err := WorktreeCleanup(unmergedPath, CleanupOpts{BaseBranch: "main", Force: true}); err != nil {
		t.Fatalf("expected --force cleanup to succeed, got: %v", err)
	}
}

func TestResolveDefaultBaseBranch(t *testing.T) {
	tests := []struct {
		name     string
		explicit string
		want     string
	}{
		{"explicit bare branch", "release", "release"},
		{"explicit origin-prefixed branch", "origin/homolog", "homolog"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveDefaultBaseBranch(tc.explicit)
			if got != tc.want {
				t.Errorf("resolveDefaultBaseBranch(%q) = %q, want %q", tc.explicit, got, tc.want)
			}
		})
	}
}
