package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIsClean_CleanRepo verifies IsClean returns true on a repo with no
// untracked or modified files.
func TestIsClean_CleanRepo(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	clean, porcelain, err := IsClean()
	if err != nil {
		t.Fatalf("IsClean() error: %v", err)
	}
	if !clean {
		t.Errorf("expected clean=true, got false; porcelain=%q", porcelain)
	}
	if porcelain != "" {
		t.Errorf("expected empty porcelain output for clean repo, got %q", porcelain)
	}
}

// TestIsClean_UntrackedFile verifies IsClean returns false when an untracked
// file is present.
func TestIsClean_UntrackedFile(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Create an untracked file
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	clean, porcelain, err := IsClean()
	if err != nil {
		t.Fatalf("IsClean() error: %v", err)
	}
	if clean {
		t.Error("expected clean=false with untracked file, got true")
	}
	if !strings.Contains(porcelain, "untracked.txt") {
		t.Errorf("expected porcelain output to mention 'untracked.txt', got %q", porcelain)
	}
	if !strings.Contains(porcelain, "??") {
		t.Errorf("expected '??' marker for untracked file, got %q", porcelain)
	}
}

// TestIsClean_ModifiedFile verifies IsClean returns false when a tracked file
// has been modified but not staged.
func TestIsClean_ModifiedFile(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Modify the tracked README.md without staging
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# modified\n"), 0644); err != nil {
		t.Fatal(err)
	}

	clean, porcelain, err := IsClean()
	if err != nil {
		t.Fatalf("IsClean() error: %v", err)
	}
	if clean {
		t.Error("expected clean=false with modified tracked file, got true")
	}
	if !strings.Contains(porcelain, "README.md") {
		t.Errorf("expected porcelain output to mention 'README.md', got %q", porcelain)
	}
}

// TestIsClean_StagedFile verifies IsClean returns false when a file is staged
// but not yet committed (staged changes count as dirty).
func TestIsClean_StagedFile(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Stage a new file
	newFile := filepath.Join(dir, "staged.txt")
	if err := os.WriteFile(newFile, []byte("staged"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "git", "add", "staged.txt")

	clean, porcelain, err := IsClean()
	if err != nil {
		t.Fatalf("IsClean() error: %v", err)
	}
	if clean {
		t.Error("expected clean=false with staged-but-uncommitted file, got true")
	}
	if !strings.Contains(porcelain, "staged.txt") {
		t.Errorf("expected porcelain output to mention 'staged.txt', got %q", porcelain)
	}
}
