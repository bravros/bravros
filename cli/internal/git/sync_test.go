package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// setupSyncRepo creates an origin + clone repo pair, advances origin by N
// commits, and returns the clone working dir. Origin lives at origin/<base>
// (bare remote).
func setupSyncRepo(t *testing.T, base string, advance int) (cloneDir string, cleanup func()) {
	t.Helper()
	tmp := t.TempDir()

	originDir := filepath.Join(tmp, "origin")
	workDir := filepath.Join(tmp, "work")
	cloneDir = filepath.Join(tmp, "clone")

	runIn := func(dir string, args ...string) {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v in %s failed: %v\n%s", args, dir, err, string(out))
		}
	}

	// Bare origin
	os.MkdirAll(originDir, 0755)
	runIn(originDir, "git", "init", "--bare", "--initial-branch="+base)

	// Work dir seed
	os.MkdirAll(workDir, 0755)
	runIn(workDir, "git", "init", "--initial-branch="+base)
	runIn(workDir, "git", "config", "user.email", "t@t.com")
	runIn(workDir, "git", "config", "user.name", "t")
	os.WriteFile(filepath.Join(workDir, "seed.txt"), []byte("seed"), 0644)
	runIn(workDir, "git", "add", ".")
	runIn(workDir, "git", "commit", "-m", "seed")
	runIn(workDir, "git", "remote", "add", "origin", originDir)
	runIn(workDir, "git", "push", "origin", base)

	// Clone
	c := exec.Command("git", "clone", originDir, cloneDir)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("clone failed: %v\n%s", err, string(out))
	}
	runIn(cloneDir, "git", "config", "user.email", "t@t.com")
	runIn(cloneDir, "git", "config", "user.name", "t")

	// Advance origin by N extra commits (on base)
	for i := 0; i < advance; i++ {
		name := filepath.Join(workDir, "advance.txt")
		os.WriteFile(name, []byte{byte('a' + i)}, 0644)
		runIn(workDir, "git", "add", ".")
		runIn(workDir, "git", "commit", "-m", "advance")
	}
	if advance > 0 {
		runIn(workDir, "git", "push", "origin", base)
	}

	cleanup = func() {}
	return cloneDir, cleanup
}

func withChdir(t *testing.T, dir string, fn func()) {
	t.Helper()
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)
	fn()
}

func TestSyncBranch_UpToDate(t *testing.T) {
	clone, _ := setupSyncRepo(t, "main", 0)
	withChdir(t, clone, func() {
		res, err := SyncBranch("main", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Behind != 0 {
			t.Errorf("expected behind=0, got %d", res.Behind)
		}
		if res.Rebased {
			t.Error("should not rebase when up to date")
		}
	})
}

func TestSyncBranch_BehindAndRebase(t *testing.T) {
	clone, _ := setupSyncRepo(t, "main", 2)
	withChdir(t, clone, func() {
		res, err := SyncBranch("main", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Behind != 2 {
			t.Errorf("expected behind=2, got %d", res.Behind)
		}
		if !res.Rebased {
			t.Error("should have rebased")
		}
	})
}

func TestSyncBranch_DryRun(t *testing.T) {
	clone, _ := setupSyncRepo(t, "main", 3)
	withChdir(t, clone, func() {
		res, err := SyncBranch("main", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Behind != 3 {
			t.Errorf("expected behind=3, got %d", res.Behind)
		}
		if res.Rebased {
			t.Error("dry-run should not rebase")
		}
		if !res.DryRun {
			t.Error("dry_run flag should be true")
		}
	})
}

func TestSyncBranch_ExplicitBase(t *testing.T) {
	clone, _ := setupSyncRepo(t, "homolog", 1)
	withChdir(t, clone, func() {
		res, err := SyncBranch("homolog", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Base != "homolog" {
			t.Errorf("expected base=homolog, got %q", res.Base)
		}
		if res.Behind != 1 {
			t.Errorf("expected behind=1, got %d", res.Behind)
		}
	})
}
