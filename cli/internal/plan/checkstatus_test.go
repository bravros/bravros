package plan

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initTestGitRepo creates a temp dir with a git repo and returns the path.
// Caller should defer os.RemoveAll(path) and restore the original working dir.
func initTestGitRepo(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	cleanup := func() {
		os.Chdir(origDir)
	}

	os.Chdir(dir)

	// Init git repo
	runGit(t, "init")
	runGit(t, "config", "user.email", "test@test.com")
	runGit(t, "config", "user.name", "Test")

	// Create .planning dir
	os.MkdirAll(filepath.Join(dir, ".planning"), 0755)

	return dir, cleanup
}

func runGit(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func TestCheckStatusFoundInGitLog(t *testing.T) {
	dir, cleanup := initTestGitRepo(t)
	defer cleanup()

	// Create plan file
	planFile := filepath.Join(dir, ".planning", "0020-feat-something-todo.md")
	os.WriteFile(planFile, []byte("# Plan 0020\n\n## Phases\n\n- [x] Task 1\n"), 0644)

	// Create initial commit
	runGit(t, "add", ".")
	runGit(t, "commit", "-m", "initial commit")

	// Create a plan-check commit
	os.WriteFile(filepath.Join(dir, ".planning", "dummy.txt"), []byte("check"), 0644)
	runGit(t, "add", ".")
	runGit(t, "commit", "-m", "🧹 chore: plan check 0020 — 0 mismatches")

	result, err := CheckPlanCheckStatus(planFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Checked {
		t.Error("expected Checked=true")
	}
	if result.Source != "git_log" {
		t.Errorf("expected source=git_log, got %q", result.Source)
	}
	if result.Commit == "" {
		t.Error("expected non-empty Commit")
	}
	if result.Timestamp == "" {
		t.Error("expected non-empty Timestamp")
	}
	if result.PlanNum != "0020" {
		t.Errorf("expected PlanNum=0020, got %q", result.PlanNum)
	}
}

func TestCheckStatusFoundInFileMarkerOnly(t *testing.T) {
	dir, cleanup := initTestGitRepo(t)
	defer cleanup()

	// Create plan file WITH a plan-check section marker
	planContent := `# Plan 0021

## Phases

- [x] Task 1

## Plan Check

All tasks verified. 0 mismatches.
`
	planFile := filepath.Join(dir, ".planning", "0021-feat-widgets-todo.md")
	os.WriteFile(planFile, []byte(planContent), 0644)

	// Create initial commit (no plan-check commit in log)
	runGit(t, "add", ".")
	runGit(t, "commit", "-m", "initial commit")

	result, err := CheckPlanCheckStatus(planFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Checked {
		t.Error("expected Checked=true")
	}
	if result.Source != "file_marker" {
		t.Errorf("expected source=file_marker, got %q", result.Source)
	}
	if result.Commit != "" {
		t.Errorf("expected empty Commit for file_marker source, got %q", result.Commit)
	}
	if result.PlanNum != "0021" {
		t.Errorf("expected PlanNum=0021, got %q", result.PlanNum)
	}
}

func TestCheckStatusBothSources(t *testing.T) {
	dir, cleanup := initTestGitRepo(t)
	defer cleanup()

	// Create plan file WITH marker
	planContent := `# Plan 0022

## Plan Check

Audited all tasks implemented.
`
	planFile := filepath.Join(dir, ".planning", "0022-fix-bug-todo.md")
	os.WriteFile(planFile, []byte(planContent), 0644)

	runGit(t, "add", ".")
	runGit(t, "commit", "-m", "initial commit")

	// Also add a plan-check commit
	os.WriteFile(filepath.Join(dir, ".planning", "extra.txt"), []byte("x"), 0644)
	runGit(t, "add", ".")
	runGit(t, "commit", "-m", "plan check 0022 done")

	result, err := CheckPlanCheckStatus(planFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Checked {
		t.Error("expected Checked=true")
	}
	if result.Source != "both" {
		t.Errorf("expected source=both, got %q", result.Source)
	}
}

func TestCheckStatusNotFoundAnywhere(t *testing.T) {
	dir, cleanup := initTestGitRepo(t)
	defer cleanup()

	// Create plan file WITHOUT marker
	planFile := filepath.Join(dir, ".planning", "0023-feat-clean-todo.md")
	os.WriteFile(planFile, []byte("# Plan 0023\n\n## Phases\n\n- [ ] Task 1\n"), 0644)

	runGit(t, "add", ".")
	runGit(t, "commit", "-m", "initial commit with no plan check")

	result, err := CheckPlanCheckStatus(planFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Checked {
		t.Error("expected Checked=false")
	}
	if result.Source != "none" {
		t.Errorf("expected source=none, got %q", result.Source)
	}
	if result.Commit != "" {
		t.Errorf("expected empty Commit, got %q", result.Commit)
	}
}

func TestCheckStatusNoPlanFile(t *testing.T) {
	dir, cleanup := initTestGitRepo(t)
	defer cleanup()

	// Create initial commit (no plan files at all)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0644)
	runGit(t, "add", ".")
	runGit(t, "commit", "-m", "initial")

	result, err := CheckPlanCheckStatus("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Checked {
		t.Error("expected Checked=false for no plan file")
	}
	if result.PlanFile != "" {
		t.Errorf("expected empty PlanFile, got %q", result.PlanFile)
	}
	if result.Source != "none" {
		t.Errorf("expected source=none, got %q", result.Source)
	}
}
