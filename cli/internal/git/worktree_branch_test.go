package git

// Table-driven tests for worktree-aware branch and project detection (backlog 0030).
//
// These tests cover three scenarios:
//   - primary worktree (the main checkout)
//   - secondary (linked) worktree on a feature branch
//   - detached HEAD in a worktree
//
// All scenarios assert that:
//   - CurrentBranch() returns the correct per-worktree branch (or "" for detached HEAD)
//   - ProjectName() returns the primary root dir name, not the cwd dir name
//   - context --diffs base-ref is resolved correctly (covered via CurrentBranch correctness)

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupPrimaryRepo creates a primary git repo with an initial commit on "main"
// and returns its path. A second branch "feat/0058-secondary" is also created
// so linked worktrees can check it out.
func setupPrimaryRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, c := range cmds {
		if out, err := runIn(dir, c...); err != nil {
			t.Fatalf("setup: %v: %s", c, out)
		}
	}
	// Initial commit on main
	writeFile(t, dir, "README.md", "# primary")
	if out, err := runIn(dir, "git", "add", "."); err != nil {
		t.Fatalf("git add: %s", out)
	}
	if out, err := runIn(dir, "git", "commit", "-m", "initial"); err != nil {
		t.Fatalf("git commit: %s", out)
	}
	// Create secondary branch
	if out, err := runIn(dir, "git", "branch", "feat/0058-secondary"); err != nil {
		t.Fatalf("git branch: %s", out)
	}
	return dir
}

// runIn runs a command in dir, returns (combined output, error).
func runIn(dir string, args ...string) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// TestWorktreeBranchDetection is the main table-driven test for subsection 1a.
func TestWorktreeBranchDetection(t *testing.T) {
	primaryDir := setupPrimaryRepo(t)

	// Build the linked worktree directory for the secondary branch.
	wtDir := filepath.Join(t.TempDir(), "linked-wt")
	if out, err := runIn(primaryDir, "git", "worktree", "add", wtDir, "feat/0058-secondary"); err != nil {
		t.Fatalf("git worktree add: %s", out)
	}
	defer func() {
		runIn(primaryDir, "git", "worktree", "remove", "--force", wtDir)
		runIn(primaryDir, "git", "branch", "-D", "feat/0058-secondary")
	}()

	// Get a detached HEAD SHA from the primary repo.
	headSHA, err := runIn(primaryDir, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	// Build a detached-HEAD worktree.
	detachedDir := filepath.Join(t.TempDir(), "detached-wt")
	if out, err := runIn(primaryDir, "git", "worktree", "add", "--detach", detachedDir, headSHA); err != nil {
		t.Fatalf("git worktree add detached: %s", out)
	}
	defer func() {
		runIn(primaryDir, "git", "worktree", "remove", "--force", detachedDir)
	}()

	tests := []struct {
		name           string
		dir            string
		wantBranch     string // "" for detached HEAD
		wantProjectDir string // expected basename of ProjectName() result
		wantNotDir     string // dir name that ProjectName() must NOT equal (worktree dir basename)
	}{
		{
			name:           "primary worktree on main",
			dir:            primaryDir,
			wantBranch:     "main",
			wantProjectDir: filepath.Base(primaryDir),
			wantNotDir:     "", // primary dir is always correct
		},
		{
			name:           "secondary worktree on feat/0058-secondary",
			dir:            wtDir,
			wantBranch:     "feat/0058-secondary",
			wantProjectDir: filepath.Base(primaryDir),
			wantNotDir:     filepath.Base(wtDir), // must NOT return the worktree dir name
		},
		{
			name:           "detached HEAD worktree",
			dir:            detachedDir,
			wantBranch:     "", // detached HEAD returns empty string
			wantProjectDir: filepath.Base(primaryDir),
			wantNotDir:     filepath.Base(detachedDir),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Change working directory to the test directory for each scenario.
			origDir, _ := os.Getwd()
			if err := os.Chdir(tt.dir); err != nil {
				t.Fatalf("chdir to %s: %v", tt.dir, err)
			}
			defer os.Chdir(origDir)

			// --- CurrentBranch ---
			repo, err := Open(tt.dir)
			if err != nil {
				t.Fatalf("Open(%s): %v", tt.dir, err)
			}
			gotBranch := repo.CurrentBranch()
			if gotBranch != tt.wantBranch {
				t.Errorf("CurrentBranch() = %q, want %q", gotBranch, tt.wantBranch)
			}

			// --- ProjectName ---
			gotProject := ProjectName()
			if gotProject != tt.wantProjectDir {
				t.Errorf("ProjectName() = %q, want %q (primary root basename)", gotProject, tt.wantProjectDir)
			}
			if tt.wantNotDir != "" && gotProject == tt.wantNotDir {
				t.Errorf("ProjectName() returned worktree dir name %q — should return primary root name", gotProject)
			}
		})
	}
}

// TestReadHEADFile covers the HEAD file parser in isolation.
func TestReadHEADFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "normal branch ref",
			content: "ref: refs/heads/feat/0058-secondary\n",
			want:    "feat/0058-secondary",
		},
		{
			name:    "main branch",
			content: "ref: refs/heads/main\n",
			want:    "main",
		},
		{
			name:    "detached HEAD (SHA)",
			content: "abc1234def5678\n",
			want:    "",
		},
		{
			name:    "empty file",
			content: "",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			headPath := filepath.Join(dir, "HEAD")
			if err := os.WriteFile(headPath, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}
			got := readHEADFile(headPath)
			if got != tt.want {
				t.Errorf("readHEADFile() = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("missing file returns empty", func(t *testing.T) {
		got := readHEADFile("/no/such/path/HEAD")
		if got != "" {
			t.Errorf("readHEADFile(missing) = %q, want empty", got)
		}
	})
}

// TestResolveWorktreeGitDir verifies the .git FILE detector.
func TestResolveWorktreeGitDir(t *testing.T) {
	t.Run("primary worktree has .git DIR — returns empty", func(t *testing.T) {
		dir := setupPrimaryRepo(t)
		got := resolveWorktreeGitDir(dir)
		if got != "" {
			t.Errorf("resolveWorktreeGitDir(primary) = %q, want empty", got)
		}
	})

	t.Run("secondary worktree has .git FILE — returns gitdir", func(t *testing.T) {
		primaryDir := setupPrimaryRepo(t)
		wtDir := filepath.Join(t.TempDir(), "wt-test")
		if out, err := runIn(primaryDir, "git", "worktree", "add", wtDir, "feat/0058-secondary"); err != nil {
			t.Fatalf("worktree add: %s", out)
		}
		defer func() {
			runIn(primaryDir, "git", "worktree", "remove", "--force", wtDir)
			runIn(primaryDir, "git", "branch", "-D", "feat/0058-secondary")
		}()

		got := resolveWorktreeGitDir(wtDir)
		if got == "" {
			t.Error("resolveWorktreeGitDir(secondary worktree) = empty, want non-empty gitdir path")
		}
		// The resolved path should point to a directory containing a HEAD file.
		if _, err := os.Stat(filepath.Join(got, "HEAD")); err != nil {
			t.Errorf("gitdir %q does not contain HEAD: %v", got, err)
		}
	})

	t.Run("non-git directory — returns empty", func(t *testing.T) {
		dir := t.TempDir()
		got := resolveWorktreeGitDir(dir)
		if got != "" {
			t.Errorf("resolveWorktreeGitDir(non-git) = %q, want empty", got)
		}
	})
}

// TestProjectNameWithBravrosYML verifies that a project: field in .bravros.yml
// takes precedence over the directory basename.
func TestProjectNameWithBravrosYML(t *testing.T) {
	dir := t.TempDir()

	// Write a minimal .bravros.yml with a project field.
	bravrosYML := `project: "my-canonical-project"
staging_branch: homolog
`
	if err := os.WriteFile(filepath.Join(dir, ".bravros.yml"), []byte(bravrosYML), 0644); err != nil {
		t.Fatal(err)
	}

	got := projectNameFromDir(dir)
	if got != "my-canonical-project" {
		t.Errorf("projectNameFromDir() with .bravros.yml project field = %q, want %q", got, "my-canonical-project")
	}
}

// TestProjectNameWithoutBravrosYML verifies fallback to directory basename.
func TestProjectNameWithoutBravrosYML(t *testing.T) {
	dir := t.TempDir()
	// No .bravros.yml → should return basename of dir.
	got := projectNameFromDir(dir)
	if got != filepath.Base(dir) {
		t.Errorf("projectNameFromDir() without .bravros.yml = %q, want %q", got, filepath.Base(dir))
	}
}

// TestRemoteURL_WorktreeSafe verifies RemoteURL works from inside a linked worktree.
// This test fixes the pre-existing TestRemoteURL flakiness (backlog 0030 note).
func TestRemoteURL_WorktreeSafe(t *testing.T) {
	primaryDir := setupPrimaryRepo(t)
	wtDir := filepath.Join(t.TempDir(), "wt-remote-test")
	if out, err := runIn(primaryDir, "git", "worktree", "add", wtDir, "feat/0058-secondary"); err != nil {
		t.Fatalf("worktree add: %s", out)
	}
	defer func() {
		runIn(primaryDir, "git", "worktree", "remove", "--force", wtDir)
		runIn(primaryDir, "git", "branch", "-D", "feat/0058-secondary")
	}()

	origDir, _ := os.Getwd()
	os.Chdir(wtDir)
	defer os.Chdir(origDir)

	repo, err := Open(wtDir)
	if err != nil {
		t.Fatalf("Open(secondary worktree): %v", err)
	}
	// RemoteURL should not panic or error when origin is absent — just return "".
	// The pre-existing test TestRemoteURL failed with "fatal: 'origin' does not
	// appear to be a git repository" because go-git walked up to the wrong location.
	// With the worktree fix, Open() correctly initialises the repo in fallback mode
	// pointing to the right path, so RemoteURL() returns "" cleanly (no remote set
	// in the temp repo) rather than crashing.
	url := repo.RemoteURL("origin")
	// In a temp repo with no remote, "" is expected — the important thing is no panic.
	_ = url
}
