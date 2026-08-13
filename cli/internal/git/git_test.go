package git

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	stdout, stderr, err := Run("echo", "hello")
	if err != nil {
		t.Fatalf("Run('echo hello') returned error: %v, stderr: %s", err, stderr)
	}
	if stdout != "hello" {
		t.Errorf("expected stdout 'hello', got %q", stdout)
	}
}

func TestRunError(t *testing.T) {
	_, _, err := Run("false")
	if err == nil {
		t.Fatal("expected error from Run('false'), got nil")
	}
}

func TestSection(t *testing.T) {
	result := Section("Changes")
	if !strings.HasPrefix(result, "\n── Changes ") {
		t.Errorf("Section header missing prefix, got %q", result)
	}
	if !strings.Contains(result, "─") {
		t.Errorf("Section header missing padding dashes, got %q", result)
	}
	// Title "Changes" is 7 chars, padding should be 54-7=47 dashes
	expected := "\n── Changes " + strings.Repeat("─", 47)
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestProjectName(t *testing.T) {
	got := ProjectName()
	if got == "" {
		t.Error("ProjectName() returned empty string")
	}
	// ProjectName() now returns the primary worktree's project name (from .bravros.yml
	// or the primary root dir basename) — not necessarily the cwd's dirname.
	// We can't assert a fixed string here because the test may run inside a linked
	// worktree (e.g. claude58) where the correct answer is "claude" (the primary root)
	// rather than "git" (the cwd's last component).
	// The table-driven tests in worktree_branch_test.go cover the specific scenarios.
}

// setupTempGitRepo creates a minimal git repo in a temp dir, chdir into it,
// and returns a cleanup function that restores the original cwd.
func setupTempGitRepo(t *testing.T, branches ...string) func() {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	// Init repo with initial commit so HEAD exists
	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, extra := range branches {
		cmds = append(cmds, []string{"git", "branch", extra})
	}
	for _, args := range cmds {
		out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
		if err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
	}
	return func() { os.Chdir(orig) }
}

func TestOpen(t *testing.T) {
	cleanup := setupTempGitRepo(t)
	defer cleanup()

	repo, err := Open("")
	if err != nil {
		t.Fatalf("Open('') failed: %v", err)
	}
	if repo == nil {
		t.Fatal("Open('') returned nil repo")
	}
	if repo.R == nil && !repo.fallback {
		t.Fatal("Open('') returned repo with nil R and not in fallback mode")
	}
}

func TestCurrentBranch(t *testing.T) {
	cleanup := setupTempGitRepo(t)
	defer cleanup()

	repo, err := Open("")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	branch := repo.CurrentBranch()
	if branch != "main" {
		t.Errorf("expected CurrentBranch() = 'main', got %q", branch)
	}
}

func TestDetectBaseBranch(t *testing.T) {
	tests := []struct {
		name     string
		branches []string
		want     string
	}{
		{"main only", nil, "main"},
		{"with homolog", []string{"homolog"}, "homolog"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := setupTempGitRepo(t, tt.branches...)
			defer cleanup()

			repo, err := Open("")
			if err != nil {
				t.Fatalf("Open failed: %v", err)
			}
			base := repo.DetectBaseBranch()
			if base != tt.want {
				t.Errorf("expected DetectBaseBranch() = %q, got %q", tt.want, base)
			}
		})
	}
}

func TestDetectBaseBranchSimple(t *testing.T) {
	tests := []struct {
		name     string
		branches []string
		want     string
	}{
		{"main only", nil, "main"},
		{"with homolog", []string{"homolog"}, "homolog"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := setupTempGitRepo(t, tt.branches...)
			defer cleanup()

			repo, err := Open("")
			if err != nil {
				t.Fatalf("Open failed: %v", err)
			}
			base := repo.DetectBaseBranchSimple()
			if base != tt.want {
				t.Errorf("expected DetectBaseBranchSimple() = %q, got %q", tt.want, base)
			}
		})
	}
}

func TestCommitsSince(t *testing.T) {
	// Use HEAD~1 as base to get at least the latest commit
	out, err := CommitsSince("HEAD~1")
	if err != nil {
		t.Fatalf("CommitsSince('HEAD~1') failed: %v", err)
	}
	if out == "" {
		t.Error("CommitsSince('HEAD~1') returned empty output")
	}
}

func TestChangedFiles(t *testing.T) {
	// HEAD~1...HEAD should show files changed in the last commit
	files, err := ChangedFiles("HEAD~1")
	if err != nil {
		t.Fatalf("ChangedFiles('HEAD~1') failed: %v", err)
	}
	if len(files) == 0 {
		t.Error("ChangedFiles('HEAD~1') returned no files")
	}
}

func TestDiffStat(t *testing.T) {
	out, err := DiffStat("HEAD~1")
	if err != nil {
		t.Fatalf("DiffStat('HEAD~1') failed: %v", err)
	}
	if out == "" {
		t.Error("DiffStat('HEAD~1') returned empty output")
	}
}

func TestRemoteURL(t *testing.T) {
	repo, err := Open("")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	url := repo.RemoteURL("origin")
	if url == "" {
		t.Error("RemoteURL('origin') returned empty string")
	}
}

func TestHasHomologBranch(t *testing.T) {
	tests := []struct {
		name     string
		branches []string
		want     bool
	}{
		{"no homolog", nil, false},
		{"with homolog", []string{"homolog"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := setupTempGitRepo(t, tt.branches...)
			defer cleanup()

			got := HasHomologBranch("")
			if got != tt.want {
				t.Errorf("HasHomologBranch() = %v, want %v", got, tt.want)
			}
		})
	}
}
