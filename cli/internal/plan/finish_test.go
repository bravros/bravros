package plan

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initTestRepo creates a temp dir with a git repo and .planning/ structure.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Init git repo
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git init cmd %v failed: %v\n%s", args, err, out)
		}
	}

	// Create .planning/ and .planning/backlog/archive/
	os.MkdirAll(filepath.Join(dir, ".planning", "backlog", "archive"), 0755)

	// Initial commit so HEAD exists
	placeholder := filepath.Join(dir, ".gitkeep")
	os.WriteFile(placeholder, []byte(""), 0644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "initial")

	return dir
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writePlanFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, ".planning", name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeBacklogFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, ".planning", "backlog", name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

const finishTestPlanContent = `---
title: test plan
status: in-progress
backlog: "0042"
tasks_done: 2
tasks_total: 3
phases_done: 1
phases_total: 2
---

# Test Plan

### Phase 1

- [x] Task 1
- [x] Task 2

### Phase 2

- [ ] Task 3
`

const finishTestPlanNoBacklog = `---
title: test plan no backlog
status: in-progress
tasks_done: 1
tasks_total: 2
phases_done: 0
phases_total: 1
---

# Test Plan

### Phase 1

- [x] Task 1
- [ ] Task 2
`

const finishTestBacklogContent = `---
id: "0042"
title: Some feature
type: feat
status: planned
priority: high
size: medium
project: test
tags: []
created: "2026-01-01"
---

# Some feature

Description here.
`

const finishTestCompletedPlan = `---
title: old plan
status: completed
---

# Old Plan
`

func TestFinish_WithBacklog(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Write plan and backlog files
	planPath := writePlanFile(t, dir, "0020-feat-test-feature-todo.md", finishTestPlanContent)
	writeBacklogFile(t, dir, "0042-feat-some-feature.md", finishTestBacklogContent)

	// Stage and commit so git mv works
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add plan files")

	result, err := Finish(FinishOpts{
		PlanFile: planPath,
		PRNumber: "123",
	})
	if err != nil {
		t.Fatalf("Finish failed: %v", err)
	}

	// Verify plan was renamed
	if !strings.HasSuffix(result.NewFile, "-complete.md") {
		t.Errorf("expected new file to end with -complete.md, got %s", result.NewFile)
	}

	// Verify old file is gone
	if _, err := os.Stat(planPath); !os.IsNotExist(err) {
		t.Error("old plan file should not exist after finish")
	}

	// Verify new file exists
	if _, err := os.Stat(result.NewFile); os.IsNotExist(err) {
		t.Error("new plan file should exist after finish")
	}

	// Verify backlog was archived
	if result.BacklogArchived == "" {
		t.Error("expected backlog to be archived")
	}

	// Verify commit was made
	if result.CommitHash == "" {
		t.Error("expected a commit hash")
	}

	// Verify the renamed file has status=completed in frontmatter
	content, _ := os.ReadFile(result.NewFile)
	if !strings.Contains(string(content), "status: completed") {
		t.Error("expected status: completed in frontmatter")
	}
}

func TestFinish_WithoutBacklog(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	planPath := writePlanFile(t, dir, "0021-fix-something-todo.md", finishTestPlanNoBacklog)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add plan")

	result, err := Finish(FinishOpts{
		PlanFile: planPath,
		PRNumber: "456",
	})
	if err != nil {
		t.Fatalf("Finish failed: %v", err)
	}

	if !strings.HasSuffix(result.NewFile, "-complete.md") {
		t.Errorf("expected -complete.md, got %s", result.NewFile)
	}

	if result.BacklogArchived != "" {
		t.Errorf("expected no backlog archived, got %s", result.BacklogArchived)
	}

	if result.CommitHash == "" {
		t.Error("expected a commit hash")
	}
}

func TestFinish_AlreadyCompleted(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	planPath := writePlanFile(t, dir, "0019-feat-old-complete.md", finishTestCompletedPlan)

	result, err := Finish(FinishOpts{
		PlanFile: planPath,
	})
	if err != nil {
		t.Fatalf("Finish failed: %v", err)
	}

	if result.Skipped == "" {
		t.Error("expected plan to be skipped as already completed")
	}

	if result.CommitHash != "" {
		t.Error("expected no commit for already-completed plan")
	}
}

func TestFinish_DryRun(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	planPath := writePlanFile(t, dir, "0022-feat-dry-run-todo.md", finishTestPlanContent)
	writeBacklogFile(t, dir, "0042-feat-some-feature.md", finishTestBacklogContent)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add plan files")

	result, err := Finish(FinishOpts{
		PlanFile: planPath,
		DryRun:   true,
	})
	if err != nil {
		t.Fatalf("Finish failed: %v", err)
	}

	if !result.DryRun {
		t.Error("expected dry_run=true")
	}

	// Plan file should NOT be renamed
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		t.Error("plan file should still exist in dry-run mode")
	}

	// No commit
	if result.CommitHash != "" {
		t.Error("expected no commit in dry-run mode")
	}

	// Should report backlog would be archived
	if !strings.Contains(result.BacklogArchived, "would archive") {
		t.Errorf("expected dry-run backlog message, got %q", result.BacklogArchived)
	}
}

func TestFinish_PlanNotFound(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	_, err := Finish(FinishOpts{
		PlanFile: "/nonexistent/plan-todo.md",
	})
	if err == nil {
		t.Fatal("expected error for missing plan file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestCleanOrphanTodos(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Create a -complete.md AND a -todo.md (the orphan scenario)
	writePlanFile(t, dir, "0030-feat-something-complete.md", finishTestCompletedPlan)
	writePlanFile(t, dir, "0030-feat-something-todo.md", finishTestPlanContent)
	// Also create a normal -todo.md with no complete counterpart (should NOT be removed)
	writePlanFile(t, dir, "0031-feat-active-todo.md", finishTestPlanNoBacklog)

	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add plan files")

	removed, err := CleanOrphanTodos()
	if err != nil {
		t.Fatalf("CleanOrphanTodos failed: %v", err)
	}

	if len(removed) != 1 {
		t.Fatalf("expected 1 removed, got %d: %v", len(removed), removed)
	}
	if !strings.Contains(removed[0], "0030-feat-something-todo.md") {
		t.Errorf("expected 0030 todo to be removed, got %s", removed[0])
	}

	// Orphan should be gone
	if _, err := os.Stat(filepath.Join(dir, ".planning", "0030-feat-something-todo.md")); !os.IsNotExist(err) {
		t.Error("orphan -todo.md should have been removed")
	}

	// Complete should still exist
	if _, err := os.Stat(filepath.Join(dir, ".planning", "0030-feat-something-complete.md")); os.IsNotExist(err) {
		t.Error("-complete.md should still exist")
	}

	// Active todo (no complete counterpart) should still exist
	if _, err := os.Stat(filepath.Join(dir, ".planning", "0031-feat-active-todo.md")); os.IsNotExist(err) {
		t.Error("active -todo.md without -complete.md should NOT be removed")
	}
}

func TestCleanOrphanTodos_BacklogOrphans(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Create a backlog file in both active and archive (orphan scenario)
	writeBacklogFile(t, dir, "0061-fix-something.md", "---\nstatus: archived\n---\n# Fix\n")
	archiveDir := filepath.Join(dir, ".planning", "backlog", "archive")
	os.MkdirAll(archiveDir, 0755)
	os.WriteFile(filepath.Join(archiveDir, "0061-fix-something.md"), []byte("---\nstatus: archived\n---\n# Fix\n"), 0644)
	// Also a normal active backlog (no archive counterpart — should NOT be removed)
	writeBacklogFile(t, dir, "0070-feat-new-thing.md", "---\nstatus: planned\n---\n# New\n")

	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add backlog files")

	removed, err := CleanOrphanTodos()
	if err != nil {
		t.Fatalf("CleanOrphanTodos failed: %v", err)
	}

	// Should remove the orphan backlog
	foundBacklog := false
	for _, r := range removed {
		if strings.Contains(r, "0061-fix-something.md") {
			foundBacklog = true
		}
	}
	if !foundBacklog {
		t.Errorf("expected orphan backlog 0061 to be removed, got: %v", removed)
	}

	// Active backlog without archive counterpart should still exist
	if _, err := os.Stat(filepath.Join(dir, ".planning", "backlog", "0070-feat-new-thing.md")); os.IsNotExist(err) {
		t.Error("active backlog 0070 without archive counterpart should NOT be removed")
	}
}

func TestCleanOrphanTodos_NoneFound(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	writePlanFile(t, dir, "0032-feat-only-todo.md", finishTestPlanNoBacklog)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add plan")

	removed, err := CleanOrphanTodos()
	if err != nil {
		t.Fatalf("CleanOrphanTodos failed: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("expected 0 removed, got %d", len(removed))
	}
}

func TestFinish_AutoDetect(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	writePlanFile(t, dir, "0025-feat-autodetect-todo.md", finishTestPlanNoBacklog)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add plan")

	// No PlanFile specified — should auto-detect
	result, err := Finish(FinishOpts{
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("Finish auto-detect failed: %v", err)
	}

	if !strings.Contains(result.PlanFile, "0025-feat-autodetect-todo.md") {
		t.Errorf("expected auto-detected plan file, got %s", result.PlanFile)
	}
}

// ---------------------------------------------------------------------------
// Task 7: finish.go plan detection tests
// ---------------------------------------------------------------------------

func TestFindPlanFileForFinish_PRMatchesTodoFile(t *testing.T) {
	dir := t.TempDir()
	planningDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planningDir, 0755)

	// Plan A has pr: 42
	contentA := "---\ntitle: plan a\npr: \"42\"\n---\n# Plan A\n"
	os.WriteFile(filepath.Join(planningDir, "0010-feat-a-todo.md"), []byte(contentA), 0644)

	// Plan B has pr: 99
	contentB := "---\ntitle: plan b\npr: \"99\"\n---\n# Plan B\n"
	os.WriteFile(filepath.Join(planningDir, "0011-feat-b-todo.md"), []byte(contentB), 0644)

	got, err := findPlanFileForFinish(planningDir, "42")
	if err != nil {
		t.Fatalf("findPlanFileForFinish failed: %v", err)
	}
	if !strings.Contains(got, "0010-feat-a-todo.md") {
		t.Errorf("expected 0010-feat-a-todo.md, got %q", got)
	}
}

func TestFindPlanFileForFinish_PRMatchesCompleteFile(t *testing.T) {
	dir := t.TempDir()
	planningDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planningDir, 0755)

	// Already-completed plan with pr: 77
	content := "---\ntitle: done plan\npr: \"77\"\nstatus: completed\n---\n# Done\n"
	os.WriteFile(filepath.Join(planningDir, "0012-feat-done-complete.md"), []byte(content), 0644)

	got, err := findPlanFileForFinish(planningDir, "77")
	if err != nil {
		t.Fatalf("findPlanFileForFinish failed: %v", err)
	}
	if !strings.Contains(got, "0012-feat-done-complete.md") {
		t.Errorf("expected 0012-feat-done-complete.md, got %q", got)
	}
}

func TestFindPlanFileForFinish_NoPRMultiplePlansErrors(t *testing.T) {
	dir := t.TempDir()
	planningDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planningDir, 0755)

	// Two plans without frontmatter branch — should error (ambiguous)
	os.WriteFile(filepath.Join(planningDir, "0020-feat-alpha-todo.md"), []byte("# Alpha\n"), 0644)
	os.WriteFile(filepath.Join(planningDir, "0021-feat-beta-todo.md"), []byte("# Beta\n"), 0644)

	_, err := findPlanFileForFinish(planningDir, "")
	if err == nil {
		t.Fatal("expected error when multiple -todo.md files exist with no PR and no branch match")
	}
	if !strings.Contains(err.Error(), "multiple active plans") {
		t.Errorf("expected 'multiple active plans' error, got: %v", err)
	}
}

func TestFindPlanFileForFinish_MultipleTodosPicksCorrectOne(t *testing.T) {
	dir := t.TempDir()
	planningDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planningDir, 0755)

	// Multiple todo files, one has PR 55
	contentA := "---\npr: \"10\"\n---\n# A\n"
	contentB := "---\npr: \"55\"\n---\n# B\n"
	contentC := "---\npr: \"88\"\n---\n# C\n"
	os.WriteFile(filepath.Join(planningDir, "0030-feat-a-todo.md"), []byte(contentA), 0644)
	os.WriteFile(filepath.Join(planningDir, "0031-feat-b-todo.md"), []byte(contentB), 0644)
	os.WriteFile(filepath.Join(planningDir, "0032-feat-c-todo.md"), []byte(contentC), 0644)

	got, err := findPlanFileForFinish(planningDir, "55")
	if err != nil {
		t.Fatalf("findPlanFileForFinish multi-todo failed: %v", err)
	}
	if !strings.Contains(got, "0031-feat-b-todo.md") {
		t.Errorf("expected 0031-feat-b-todo.md for PR 55, got %q", got)
	}
}

func TestReadFrontmatterPR(t *testing.T) {
	dir := t.TempDir()

	// File with pr in frontmatter
	path := filepath.Join(dir, "plan-todo.md")
	content := "---\ntitle: test\npr: \"123\"\nstatus: in_progress\n---\n# Plan\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	got := readFrontmatterPR(path)
	if got != "123" {
		t.Errorf("readFrontmatterPR: got %q, want %q", got, "123")
	}

	// File without pr in frontmatter
	nopr := filepath.Join(dir, "nopr.md")
	if err := os.WriteFile(nopr, []byte("---\ntitle: test\n---\n# Plan\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got2 := readFrontmatterPR(nopr)
	if got2 != "" {
		t.Errorf("readFrontmatterPR no-pr: got %q, want empty", got2)
	}

	// Nonexistent file
	got3 := readFrontmatterPR(filepath.Join(dir, "nonexistent.md"))
	if got3 != "" {
		t.Errorf("readFrontmatterPR nonexistent: got %q, want empty", got3)
	}
}

// ---------------------------------------------------------------------------
// Phase 2: new error handling and updateWikilinks tests
// ---------------------------------------------------------------------------

func TestFindPlanFileForFinish_PRMismatchErrors(t *testing.T) {
	tmp := t.TempDir()
	planDir := filepath.Join(tmp, ".planning")
	os.MkdirAll(planDir, 0755)
	os.WriteFile(filepath.Join(planDir, "0001-test-todo.md"),
		[]byte("---\npr: 42\n---\n# Test"), 0644)
	_, err := findPlanFileForFinish(planDir, "99")
	if err == nil {
		t.Error("expected error when --pr doesn't match any plan")
	}
}

func TestFindPlanFileForFinish_MultiplePlansErrors(t *testing.T) {
	tmp := t.TempDir()
	planDir := filepath.Join(tmp, ".planning")
	os.MkdirAll(planDir, 0755)
	os.WriteFile(filepath.Join(planDir, "0001-test-todo.md"), []byte("---\n---\n"), 0644)
	os.WriteFile(filepath.Join(planDir, "0002-other-todo.md"), []byte("---\n---\n"), 0644)
	_, err := findPlanFileForFinish(planDir, "")
	if err == nil {
		t.Error("expected error when multiple -todo.md files exist")
	}
}

func TestFindPlanFileForFinish_SinglePlanWorks(t *testing.T) {
	tmp := t.TempDir()
	planDir := filepath.Join(tmp, ".planning")
	os.MkdirAll(planDir, 0755)
	os.WriteFile(filepath.Join(planDir, "0001-test-todo.md"), []byte("---\n---\n"), 0644)
	result, err := findPlanFileForFinish(planDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(result, "0001-test-todo.md") {
		t.Errorf("expected single plan, got %s", result)
	}
}

func TestUpdateWikilinks(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "backlog.md"),
		[]byte("See [[0031-test-todo]] for details"), 0644)
	os.WriteFile(filepath.Join(tmp, "report.md"),
		[]byte("Related: [[0031-test-todo]] and [[other]]"), 0644)
	updateWikilinks(tmp, "0031-test-todo", "0031-test-complete")
	c1, _ := os.ReadFile(filepath.Join(tmp, "backlog.md"))
	if !strings.Contains(string(c1), "[[0031-test-complete]]") {
		t.Error("wikilink not updated in backlog.md")
	}
	c2, _ := os.ReadFile(filepath.Join(tmp, "report.md"))
	if !strings.Contains(string(c2), "[[0031-test-complete]]") {
		t.Error("wikilink not updated in report.md")
	}
	if !strings.Contains(string(c2), "[[other]]") {
		t.Error("unrelated wikilink should be preserved")
	}
}
