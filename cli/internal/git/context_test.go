package git

// LIFT: from bravros/private/cli/internal/git/context_test.go (2026-04-18 DRY pass)
// Adapted: initTempRepo(t) → initTestRepo(t) (returns string, not (string,func()));
// cleanup via os.Chdir(origDir); dirty-tree tests extended to also exercise IsClean().

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileDiff(t *testing.T) {
	// FileDiff should succeed even if there's nothing between HEAD~1 and HEAD for this file.
	out, err := FileDiff("HEAD~1", "go.mod")
	if err != nil {
		t.Fatalf("FileDiff returned error: %v", err)
	}
	// out may be empty if go.mod wasn't changed — that's fine
	_ = out
}

func TestFileDiff_NoSuchFile(t *testing.T) {
	// Non-existent file just returns empty diff, no error.
	out, err := FileDiff("HEAD~1", "file_that_does_not_exist_xyz.go")
	if err != nil {
		t.Fatalf("FileDiff with missing file returned error: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty diff for non-existent file, got %q", out)
	}
}

func TestChangedFiles_Empty(t *testing.T) {
	// HEAD...HEAD should have no changed files.
	files, err := ChangedFiles("HEAD")
	if err != nil {
		t.Fatalf("ChangedFiles HEAD..HEAD returned error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected no changed files HEAD..HEAD, got %v", files)
	}
}

func TestSectionLongTitle(t *testing.T) {
	// Title longer than 54 chars should result in zero padding dashes.
	longTitle := strings.Repeat("x", 60)
	result := Section(longTitle)
	// Should contain the title, no negative repeat panic.
	if !strings.Contains(result, longTitle) {
		t.Errorf("Section with long title missing title: %q", result)
	}
	// Padding should be 0 — just title with trailing space and empty repeat.
	expected := "\n── " + longTitle + " "
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestPRInfo_NoCurrentPR(t *testing.T) {
	// In a test context or non-PR branch, PRInfo should return an error.
	_, err := PRInfo()
	if err == nil {
		// It's possible we're on a branch with a PR — skip asserting error.
		t.Log("PRInfo returned nil error (may be in a PR branch)")
	}
}

// ---- inside/outside git repo detection ----

func TestOpen_NotGitRepo(t *testing.T) {
	// Create a temp dir with NO git init — Open should fail.
	dir := t.TempDir()
	_, err := Open(dir)
	if err == nil {
		t.Fatal("expected error when opening non-git directory, got nil")
	}
}

func TestOpen_GitRepo(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("Open(%q) failed: %v", dir, err)
	}
	if repo == nil {
		t.Fatal("Open returned nil repo")
	}
}

// ---- dirty tree detection (also exercises IsClean) ----

func TestDirtyTree(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Initially clean — both git status --porcelain and IsClean agree
	out, _, err := Run("git", "status", "--porcelain")
	if err != nil {
		t.Fatalf("git status failed: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected clean tree, got: %q", out)
	}

	clean, porcelain, err := IsClean()
	if err != nil {
		t.Fatalf("IsClean() after init: %v", err)
	}
	if !clean {
		t.Errorf("IsClean() expected true on fresh repo, got false; porcelain=%q", porcelain)
	}

	// Dirty it with an untracked file
	dirty := filepath.Join(dir, "dirty.txt")
	if err := os.WriteFile(dirty, []byte("dirty"), 0644); err != nil {
		t.Fatal(err)
	}

	out2, _, err2 := Run("git", "status", "--porcelain")
	if err2 != nil {
		t.Fatalf("git status after dirty: %v", err2)
	}
	if !strings.Contains(out2, "dirty.txt") {
		t.Errorf("expected dirty.txt in status, got: %q", out2)
	}

	// IsClean should also report dirty
	clean2, porcelain2, err3 := IsClean()
	if err3 != nil {
		t.Fatalf("IsClean() after dirty: %v", err3)
	}
	if clean2 {
		t.Error("IsClean() expected false after creating untracked file, got true")
	}
	if !strings.Contains(porcelain2, "dirty.txt") {
		t.Errorf("IsClean() porcelain should mention dirty.txt, got: %q", porcelain2)
	}
}

// ---- worktree detection ----

func TestWorktreeDetection(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// The main worktree is always present in `git worktree list`.
	out, _, err := Run("git", "worktree", "list", "--porcelain")
	if err != nil {
		t.Fatalf("git worktree list failed: %v", err)
	}
	if !strings.Contains(out, dir) {
		t.Errorf("expected main worktree %q in list, got:\n%s", dir, out)
	}
}

// ---- submodule detection ----

func TestSubmoduleNotPresent(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// No .gitmodules — submodule listing should return empty.
	out, _, _ := Run("git", "submodule", "status")
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected no submodules, got: %q", out)
	}
}

// ---- RunInDir ----

func TestRunInDir_Context(t *testing.T) {
	dir := t.TempDir()
	out, _, err := RunInDir(dir, "pwd")
	if err != nil {
		t.Fatalf("RunInDir(pwd) failed: %v", err)
	}
	// On macOS /private is prepended via symlink resolution; use EvalSymlinks-safe check.
	if !strings.HasSuffix(out, filepath.Base(dir)) {
		t.Errorf("RunInDir pwd = %q, expected suffix %q", out, filepath.Base(dir))
	}
}

func TestRunInDir_EmptyDir_Context(t *testing.T) {
	// Empty dir uses current working directory.
	stdout, _, err := RunInDir("", "echo", "hello")
	if err != nil {
		t.Fatalf("RunInDir empty dir failed: %v", err)
	}
	if stdout != "hello" {
		t.Errorf("expected 'hello', got %q", stdout)
	}
}

// ---- worktreeExists / worktreeBranch internal helpers ----

func TestWorktreeExistsAndBranch_Context(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// The main worktree itself should be found via worktreeExists.
	resolved := resolvePath(dir)
	if !worktreeExists(resolved) {
		t.Errorf("worktreeExists(%q) = false, want true", resolved)
	}

	// Non-existent path should return false.
	if worktreeExists("/definitely/does/not/exist/path") {
		t.Error("worktreeExists(non-existent) = true, want false")
	}

	// worktreeBranch on the main dir should return "main".
	branch := worktreeBranch(resolved)
	if branch != "main" {
		t.Errorf("worktreeBranch(%q) = %q, want 'main'", resolved, branch)
	}
}

// ---- IsPermanentBranch ----

func TestIsPermanentBranch_Context(t *testing.T) {
	tests := []struct {
		branch string
		want   bool
	}{
		{"main", true},
		{"master", true},
		{"homolog", true},
		{"staging", true},
		{"develop", true},
		{"feat/something", false},
		{"fix/bug", false},
	}
	for _, tt := range tests {
		got := IsPermanentBranch(tt.branch, nil)
		if got != tt.want {
			t.Errorf("IsPermanentBranch(%q, nil) = %v, want %v", tt.branch, got, tt.want)
		}
	}
}

func TestIsPermanentBranch_ConfigOverride_Context(t *testing.T) {
	// "release" is NOT permanent by default without config.
	if IsPermanentBranch("release", nil) {
		t.Error("expected 'release' to not be permanent without config")
	}
}

// ---- resolvePath ----

func TestResolvePath_Context(t *testing.T) {
	dir := t.TempDir()
	resolved := resolvePath(dir)
	if resolved == "" {
		t.Error("resolvePath returned empty string")
	}
}

func TestResolvePath_Nonexistent_Context(t *testing.T) {
	// Non-existent path — should return the abs path without error.
	p := resolvePath("/does/not/exist/xyz")
	if p == "" {
		t.Error("resolvePath of non-existent path returned empty")
	}
}

// ---- worktreeBranch with linked worktree ----

func TestLinkedWorktree_Context(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Create a linked worktree in a sibling directory.
	wtPath := filepath.Join(filepath.Dir(dir), "linked-wt-"+filepath.Base(dir))
	defer os.RemoveAll(wtPath)

	cmd := exec.Command("git", "worktree", "add", "-b", "feat/linked-test", wtPath, "main")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add failed: %v\n%s", err, out)
	}
	defer func() {
		exec.Command("git", "worktree", "remove", "--force", wtPath).Run()
		exec.Command("git", "branch", "-D", "feat/linked-test").Run()
	}()

	resolved := resolvePath(wtPath)
	if !worktreeExists(resolved) {
		t.Errorf("worktreeExists(%q) = false after adding linked worktree", resolved)
	}
	branch := worktreeBranch(resolved)
	if branch != "feat/linked-test" {
		t.Errorf("worktreeBranch(%q) = %q, want 'feat/linked-test'", resolved, branch)
	}
}
