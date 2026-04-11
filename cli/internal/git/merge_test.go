package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bravros/bravros/internal/config"
)

// initTestRepo creates a temporary git repo with an initial commit.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "git", "init")
	gitRun(t, dir, "git", "config", "user.email", "test@test.com")
	gitRun(t, dir, "git", "config", "user.name", "Test")
	// Create initial commit
	writeFile(t, dir, "README.md", "# test repo")
	gitRun(t, dir, "git", "add", ".")
	gitRun(t, dir, "git", "commit", "-m", "initial commit")
	return dir
}

// gitRun runs a git command in the given directory.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, stderr, err := RunInDir(dir, args...)
	if err != nil {
		t.Fatalf("command %v failed: %v\nstderr: %s", args, err, stderr)
	}
	return out
}

// writeFile creates a file with given content in the directory.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestPermanentBranch(t *testing.T) {
	tests := []struct {
		branch   string
		staging  string
		expected bool
	}{
		{"main", "", true},
		{"master", "", true},
		{"homolog", "", true},
		{"staging", "", true},
		{"develop", "", true},
		{"feat/my-feature", "", false},
		{"fix/bug-123", "", false},
		{"refactor/cleanup", "", false},
		{"custom-staging", "custom-staging", true},
		{"custom-staging", "", false},
		{"release", "release", true},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			var cfg *config.BravrosConfig
			if tt.staging != "" {
				cfg = &config.BravrosConfig{StagingBranch: tt.staging}
			}
			got := IsPermanentBranch(tt.branch, cfg)
			if got != tt.expected {
				t.Errorf("IsPermanentBranch(%q, staging=%q) = %v, want %v",
					tt.branch, tt.staging, got, tt.expected)
			}
		})
	}
}

func TestPermanentBranchNilConfig(t *testing.T) {
	// nil config should not panic and should still detect built-in permanent branches
	if !IsPermanentBranch("main", nil) {
		t.Error("expected main to be permanent with nil config")
	}
	if IsPermanentBranch("feat/test", nil) {
		t.Error("expected feat/test to NOT be permanent with nil config")
	}
}

func TestBuildMergeArgs(t *testing.T) {
	tests := []struct {
		name         string
		prNumber     int
		deleteBranch bool
		isPermanent  bool
		mergeMethod  string
		wantDelete   bool   // should contain --delete-branch
		wantSquash   bool   // should contain --squash
	}{
		{"feature branch delete", 42, true, false, "squash", true, true},
		{"permanent branch", 42, true, true, "squash", false, true},
		{"feature no-delete", 42, false, false, "squash", false, true},
		{"permanent no-delete", 42, false, true, "squash", false, true},
		{"merge strategy", 42, true, false, "merge", true, false},
		{"rebase strategy", 42, true, false, "rebase", true, false},
		{"empty defaults to squash", 42, true, false, "", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildMergeArgs(tt.prNumber, tt.deleteBranch, tt.isPermanent, tt.mergeMethod)

			hasDelete := false
			hasNoDelete := false
			hasSquash := false
			for _, a := range args {
				switch a {
				case "--delete-branch":
					hasDelete = true
				case "--no-delete-branch":
					hasNoDelete = true
				case "--squash":
					hasSquash = true
				}
			}

			if tt.wantDelete {
				if !hasDelete {
					t.Errorf("expected --delete-branch in args %v", args)
				}
			} else {
				if hasDelete {
					t.Errorf("unexpected --delete-branch in args %v", args)
				}
			}
			// --no-delete-branch is not a valid gh flag; it should never appear
			if hasNoDelete {
				t.Errorf("unexpected --no-delete-branch in args %v (not a valid gh flag)", args)
			}

			if tt.wantSquash && !hasSquash {
				t.Errorf("expected --squash in args %v", args)
			}
			if !tt.wantSquash && hasSquash {
				t.Errorf("unexpected --squash in args %v", args)
			}
		})
	}
}

func TestMergeCheckBehindBase(t *testing.T) {
	dir := initTestRepo(t)

	// Create a "base" branch and a "feature" branch
	gitRun(t, dir, "git", "checkout", "-b", "base")
	writeFile(t, dir, "base.txt", "base content")
	gitRun(t, dir, "git", "add", ".")
	gitRun(t, dir, "git", "commit", "-m", "base commit")

	gitRun(t, dir, "git", "checkout", "-b", "feature", "base~1")
	writeFile(t, dir, "feature.txt", "feature content")
	gitRun(t, dir, "git", "add", ".")
	gitRun(t, dir, "git", "commit", "-m", "feature commit")

	// checkBehindBase requires a remote — skip for unit test
	// but we can test the parsing logic via rev-list directly
	out := gitRun(t, dir, "git", "rev-list", "--left-right", "--count", "base...feature")
	if out == "" {
		t.Fatal("rev-list returned empty output")
	}
	// Should show "1\t1" (1 behind, 1 ahead)
	if out != "1\t1" {
		t.Errorf("expected '1\\t1', got %q", out)
	}
}

func TestMergePlanningConflictResolution(t *testing.T) {
	dir := initTestRepo(t)

	// Save original dir and chdir to test repo
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	// Create .planning/ file on main
	os.MkdirAll(filepath.Join(dir, ".planning"), 0o755)
	writeFile(t, dir, ".planning/plan.md", "# Plan v1\n- task 1")
	gitRun(t, dir, "git", "add", ".")
	gitRun(t, dir, "git", "commit", "-m", "add plan")

	// Create feature branch from before the plan change
	gitRun(t, dir, "git", "checkout", "-b", "feature")
	writeFile(t, dir, ".planning/plan.md", "# Plan v1\n- task 1\n- [x] done by feature")
	gitRun(t, dir, "git", "add", ".")
	gitRun(t, dir, "git", "commit", "-m", "update plan in feature")

	// Go back to main and make a conflicting plan change
	gitRun(t, dir, "git", "checkout", "main")
	writeFile(t, dir, ".planning/plan.md", "# Plan v1\n- task 1\n- [x] done by main")
	gitRun(t, dir, "git", "add", ".")
	gitRun(t, dir, "git", "commit", "-m", "update plan in main")

	// Switch to feature and try merging main
	gitRun(t, dir, "git", "checkout", "feature")

	// Test mergeBaseIntoHead with auto-resolve — but we need to simulate "origin/main"
	// Since we don't have a remote, we test with a local ref trick
	// Create a fake "origin/main" ref pointing to main
	mainRef := gitRun(t, dir, "git", "rev-parse", "main")
	gitRun(t, dir, "git", "update-ref", "refs/remotes/origin/main", mainRef)

	resolved, err := mergeBaseIntoHead("main", "feature", true)
	if err != nil {
		t.Fatalf("mergeBaseIntoHead failed: %v", err)
	}
	if !resolved {
		t.Error("expected conflicts_resolved=true for .planning/ conflict")
	}

	// Verify the merge commit message uses emoji format (not default "Merge remote-tracking branch...")
	lastMsg := gitRun(t, dir, "git", "log", "-1", "--format=%s")
	if !strings.Contains(lastMsg, "🔀 merge:") {
		t.Errorf("merge commit should use emoji format, got: %s", lastMsg)
	}
	if strings.Contains(lastMsg, "Merge remote-tracking") {
		t.Errorf("merge commit should NOT use default git message, got: %s", lastMsg)
	}
}

func TestMergeCleanMergeUsesEmojiMessage(t *testing.T) {
	dir := initTestRepo(t)

	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	// Create a commit on main that doesn't conflict with feature
	writeFile(t, dir, "main-only.txt", "from main")
	gitRun(t, dir, "git", "add", ".")
	gitRun(t, dir, "git", "commit", "-m", "add main-only file")

	gitRun(t, dir, "git", "checkout", "-b", "homolog")
	writeFile(t, dir, "homolog-only.txt", "from homolog")
	gitRun(t, dir, "git", "add", ".")
	gitRun(t, dir, "git", "commit", "-m", "add homolog-only file")

	// Add another commit to main so branches diverge (no fast-forward)
	gitRun(t, dir, "git", "checkout", "main")
	writeFile(t, dir, "main-extra.txt", "extra")
	gitRun(t, dir, "git", "add", ".")
	gitRun(t, dir, "git", "commit", "-m", "add main-extra file")

	gitRun(t, dir, "git", "checkout", "homolog")

	// Create fake origin/main ref
	mainRef := gitRun(t, dir, "git", "rev-parse", "main")
	gitRun(t, dir, "git", "update-ref", "refs/remotes/origin/main", mainRef)

	resolved, err := mergeBaseIntoHead("main", "homolog", false)
	if err != nil {
		t.Fatalf("mergeBaseIntoHead failed: %v", err)
	}
	if resolved {
		t.Error("expected no conflicts to resolve")
	}

	// Verify merge commit uses emoji format
	lastMsg := gitRun(t, dir, "git", "log", "-1", "--format=%s")
	expected := "🔀 merge: sync main into homolog"
	if lastMsg != expected {
		t.Errorf("expected %q, got %q", expected, lastMsg)
	}
}

func TestMergeFailureOnNonPlanningConflict(t *testing.T) {
	dir := initTestRepo(t)

	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	// Create conflicting changes in a non-planning file
	writeFile(t, dir, "app.go", "package main\n// version 1")
	gitRun(t, dir, "git", "add", ".")
	gitRun(t, dir, "git", "commit", "-m", "add app v1")

	gitRun(t, dir, "git", "checkout", "-b", "feature")
	writeFile(t, dir, "app.go", "package main\n// version feature")
	gitRun(t, dir, "git", "add", ".")
	gitRun(t, dir, "git", "commit", "-m", "app feature change")

	gitRun(t, dir, "git", "checkout", "main")
	writeFile(t, dir, "app.go", "package main\n// version main")
	gitRun(t, dir, "git", "add", ".")
	gitRun(t, dir, "git", "commit", "-m", "app main change")

	gitRun(t, dir, "git", "checkout", "feature")

	// Create fake origin/main ref
	mainRef := gitRun(t, dir, "git", "rev-parse", "main")
	gitRun(t, dir, "git", "update-ref", "refs/remotes/origin/main", mainRef)

	_, err := mergeBaseIntoHead("main", "feature", true)
	if err == nil {
		t.Fatal("expected error for non-planning conflict, got nil")
	}
	if !strings.Contains(err.Error(), "non-planning") {
		t.Errorf("expected error about non-planning files, got: %s", err.Error())
	}
}

func TestIsAdminRetryable(t *testing.T) {
	tests := []struct {
		stderr   string
		expected bool
	}{
		{"Pull request is not mergeable", true},
		{"required status checks have not passed", true},
		{"UNSTABLE", true},
		{"required reviews from code owners", true},
		{"protected branch policy violation", true},
		{"unrelated error: something else", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isAdminRetryable(tt.stderr)
		if got != tt.expected {
			t.Errorf("isAdminRetryable(%q) = %v, want %v", tt.stderr, got, tt.expected)
		}
	}
}

func TestBuildMergeArgsAdmin(t *testing.T) {
	// Verify that buildMergeArgs returns args that can be extended with --admin
	args := buildMergeArgs(99, true, false, "squash")
	adminArgs := append(args, "--admin")

	hasAdmin := false
	hasSquash := false
	hasDelete := false
	for _, a := range adminArgs {
		switch a {
		case "--admin":
			hasAdmin = true
		case "--squash":
			hasSquash = true
		case "--delete-branch":
			hasDelete = true
		}
	}
	if !hasAdmin {
		t.Errorf("expected --admin in args %v", adminArgs)
	}
	if !hasSquash {
		t.Errorf("expected --squash in args %v", adminArgs)
	}
	if !hasDelete {
		t.Errorf("expected --delete-branch in args %v", adminArgs)
	}
}
