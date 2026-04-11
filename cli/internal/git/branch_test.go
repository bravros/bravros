package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initTempRepo creates a bare-minimum git repo in a temp dir with an initial commit.
// Returns the temp dir path and a cleanup function.
func initTempRepo(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "checkout", "-b", "main"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v failed: %v\n%s", c, err, out)
		}
	}

	// Create initial commit (need at least one)
	f := filepath.Join(dir, "README.md")
	os.WriteFile(f, []byte("# test\n"), 0644)
	for _, c := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial"},
	} {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v failed: %v\n%s", c, err, out)
		}
	}

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	return dir, func() { os.Chdir(origDir) }
}

func TestBranchCreate(t *testing.T) {
	_, cleanup := initTempRepo(t)
	defer cleanup()

	result, err := CreateBranch("feat/test-feature")
	if err != nil {
		t.Fatalf("CreateBranch failed: %v", err)
	}

	if result.Branch != "feat/test-feature" {
		t.Errorf("expected branch 'feat/test-feature', got %q", result.Branch)
	}
	if result.Base != "main" {
		t.Errorf("expected base 'main', got %q", result.Base)
	}
	if !result.Created {
		t.Error("expected Created=true")
	}

	// Verify we're on the new branch
	repo, err := Open("")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	current := repo.CurrentBranch()
	if current != "feat/test-feature" {
		t.Errorf("expected current branch 'feat/test-feature', got %q", current)
	}
}

func TestBranchDuplicate(t *testing.T) {
	_, cleanup := initTempRepo(t)
	defer cleanup()

	// Create the branch first
	_, err := CreateBranch("feat/duplicate")
	if err != nil {
		t.Fatalf("first CreateBranch failed: %v", err)
	}

	// Go back to main so we can attempt duplicate
	Run("git", "checkout", "main")

	// Try to create the same branch again
	_, err = CreateBranch("feat/duplicate")
	if err == nil {
		t.Fatal("expected error for duplicate branch, got nil")
	}
	if err.Error() != `branch "feat/duplicate" already exists` {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBranchExists(t *testing.T) {
	_, cleanup := initTempRepo(t)
	defer cleanup()

	if !BranchExists("main") {
		t.Error("expected main branch to exist")
	}
	if BranchExists("nonexistent-branch") {
		t.Error("expected nonexistent-branch to not exist")
	}
}

func TestBranchBaseDetection(t *testing.T) {
	_, cleanup := initTempRepo(t)
	defer cleanup()

	repo, err := Open("")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	base := resolveBaseBranch(repo)
	if base != "main" {
		t.Errorf("expected base 'main', got %q", base)
	}
}

func TestBranchSkaisserYmlOverride(t *testing.T) {
	dir, cleanup := initTempRepo(t)
	defer cleanup()

	// Create a staging branch so the override has something to find
	cmds := [][]string{
		{"git", "checkout", "-b", "staging"},
		{"git", "checkout", "main"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v failed: %v\n%s", c, err, out)
		}
	}

	// Write .bravros.yml with staging_branch override
	yml := []byte("staging_branch: staging\n")
	os.WriteFile(filepath.Join(dir, ".bravros.yml"), yml, 0644)

	repo, err := Open("")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	base := resolveBaseBranch(repo)
	if base != "staging" {
		t.Errorf("expected base 'staging' from .bravros.yml, got %q", base)
	}
}

func TestCheckoutBase(t *testing.T) {
	_, cleanup := initTempRepo(t)
	defer cleanup()

	// Create a feature branch first
	Run("git", "checkout", "-b", "feat/something")

	result, err := CheckoutBase()
	if err != nil {
		t.Fatalf("CheckoutBase failed: %v", err)
	}

	if result.Created {
		t.Error("expected Created=false for checkout-only")
	}
	if !result.CheckoutOnly {
		t.Error("expected CheckoutOnly=true")
	}
	if result.Base != "main" {
		t.Errorf("expected base 'main', got %q", result.Base)
	}

	// Verify we're on main
	repo, err := Open("")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	current := repo.CurrentBranch()
	if current != "main" {
		t.Errorf("expected current branch 'main', got %q", current)
	}
}
