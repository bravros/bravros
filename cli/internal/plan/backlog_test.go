package plan

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testBacklogContent = `---
id: "0050"
title: Test feature
type: feat
status: new
priority: high
size: medium
project: test
tags: []
created: "2026-01-01"
---

# Test feature

Description here.
`

const testBacklogPlannedContent = `---
id: "0051"
title: Already planned
type: feat
status: planned
priority: medium
size: small
project: test
tags: []
created: "2026-01-01"
---

# Already planned

Description here.
`

func TestBacklogPromote(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	writeBacklogFile(t, dir, "0050-feat-test-feature.md", testBacklogContent)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add backlog")

	result, err := PromoteBacklog("0050")
	if err != nil {
		t.Fatalf("PromoteBacklog failed: %v", err)
	}

	if result.Action != "promote" {
		t.Errorf("expected action=promote, got %s", result.Action)
	}
	if result.ID != "0050" {
		t.Errorf("expected id=0050, got %s", result.ID)
	}
	// New behavior: renamed to -complete.md in backlog/ dir (not moved to archive/)
	if !strings.HasSuffix(result.ArchivedPath, "-complete.md") {
		t.Errorf("expected archived path to end with -complete.md, got %s", result.ArchivedPath)
	}
	if result.Commit == "" {
		t.Error("expected a commit hash")
	}

	// Verify file was renamed in-place
	completePath := filepath.Join(dir, ".planning", "backlog", "0050-feat-test-feature-complete.md")
	if _, err := os.Stat(completePath); os.IsNotExist(err) {
		t.Error("renamed -complete.md file should exist")
	}

	// Verify status was updated to planned
	content, _ := os.ReadFile(completePath)
	if !strings.Contains(string(content), "status: planned") {
		t.Error("expected status: planned in frontmatter")
	}

	// Verify original file is gone
	origPath := filepath.Join(dir, ".planning", "backlog", "0050-feat-test-feature.md")
	if _, err := os.Stat(origPath); !os.IsNotExist(err) {
		t.Error("original backlog file should not exist after promote")
	}
}

func TestBacklogDone(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	writeBacklogFile(t, dir, "0050-feat-test-feature.md", testBacklogContent)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add backlog")

	result, err := DoneBacklog("0050")
	if err != nil {
		t.Fatalf("DoneBacklog failed: %v", err)
	}

	if result.Action != "done" {
		t.Errorf("expected action=done, got %s", result.Action)
	}
	if result.Commit == "" {
		t.Error("expected a commit hash")
	}

	// Verify status was updated to archived (renamed to -complete.md in place)
	completePath := filepath.Join(dir, ".planning", "backlog", "0050-feat-test-feature-complete.md")
	content, _ := os.ReadFile(completePath)
	if !strings.Contains(string(content), "status: archived") {
		t.Error("expected status: archived in frontmatter")
	}
}

func TestBacklogDropWithReason(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	writeBacklogFile(t, dir, "0050-feat-test-feature.md", testBacklogContent)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add backlog")

	result, err := DropBacklog("0050", "superseded by plan 0055")
	if err != nil {
		t.Fatalf("DropBacklog failed: %v", err)
	}

	if result.Action != "drop" {
		t.Errorf("expected action=drop, got %s", result.Action)
	}
	if result.Reason != "superseded by plan 0055" {
		t.Errorf("expected reason, got %q", result.Reason)
	}
	if result.Commit == "" {
		t.Error("expected a commit hash")
	}

	// Verify reason was added to frontmatter (renamed to -complete.md in place)
	completePath := filepath.Join(dir, ".planning", "backlog", "0050-feat-test-feature-complete.md")
	content, _ := os.ReadFile(completePath)
	if !strings.Contains(string(content), "reason: superseded by plan 0055") {
		t.Errorf("expected reason in frontmatter, got:\n%s", string(content))
	}

	// Verify status was updated to archived
	if !strings.Contains(string(content), "status: archived") {
		t.Error("expected status: archived in frontmatter")
	}
}

func TestBacklogAlreadyArchived(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Write directly to archive
	archivePath := filepath.Join(dir, ".planning", "backlog", "archive", "0050-feat-test-feature.md")
	os.WriteFile(archivePath, []byte(testBacklogContent), 0644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add archived backlog")

	result, err := PromoteBacklog("0050")
	if err != nil {
		t.Fatalf("PromoteBacklog should not error for already-archived: %v", err)
	}

	// Should return gracefully with the archived path
	if !strings.Contains(result.ArchivedPath, "archive/") {
		t.Errorf("expected archived path, got %s", result.ArchivedPath)
	}
	if result.Commit != "" {
		t.Error("expected no commit for already-archived item")
	}
}

func TestBacklogMissingID(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	_, err := PromoteBacklog("9999")
	if err == nil {
		t.Fatal("expected error for missing backlog ID")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

// BenchmarkPromoteBacklog benchmarks the promote operation.
// Old 3-step bash approach: edit YAML (sed/python) + git mv + git commit = ~3 separate process spawns
// New single-call approach: Go native YAML parse + git mv + git commit = 1 Go function call
// Expected improvement: eliminates shell overhead and YAML parsing via python3/sed.
func BenchmarkPromoteBacklog(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		dir := b.TempDir()

		// Init git repo
		gitCmds := [][]string{
			{"git", "init"},
			{"git", "config", "user.email", "test@test.com"},
			{"git", "config", "user.name", "Test"},
		}
		for _, args := range gitCmds {
			c := exec.Command(args[0], args[1:]...)
			c.Dir = dir
			c.Run()
		}

		os.MkdirAll(filepath.Join(dir, ".planning", "backlog", "archive"), 0755)
		placeholder := filepath.Join(dir, ".gitkeep")
		os.WriteFile(placeholder, []byte(""), 0644)

		backlogPath := filepath.Join(dir, ".planning", "backlog", "0050-feat-bench.md")
		os.WriteFile(backlogPath, []byte(testBacklogContent), 0644)

		addCmd := exec.Command("git", "add", ".")
		addCmd.Dir = dir
		addCmd.Run()

		commitCmd := exec.Command("git", "commit", "-m", "init")
		commitCmd.Dir = dir
		commitCmd.Run()

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		b.StartTimer()

		PromoteBacklog("0050")

		b.StopTimer()
		os.Chdir(origDir)
	}
}

func TestScanBacklog_CompleteFiltering(t *testing.T) {
	tmp := t.TempDir()
	bDir := filepath.Join(tmp, "backlog")
	os.MkdirAll(bDir, 0755)
	os.WriteFile(filepath.Join(bDir, "0001-feat-test-open.md"),
		[]byte("---\nid: \"0001\"\ntitle: Active\ntype: feat\nstatus: new\npriority: medium\nsize: S\n---\n"), 0644)
	os.WriteFile(filepath.Join(bDir, "0002-feat-done-complete.md"),
		[]byte("---\nid: \"0002\"\ntitle: Done\ntype: feat\nstatus: completed\npriority: low\nsize: S\n---\n"), 0644)
	result, err := ScanBacklog(tmp, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Summary.ActiveCount != 1 {
		t.Errorf("active = %d, want 1", result.Summary.ActiveCount)
	}
	result2, _ := ScanBacklog(tmp, true)
	if result2.Summary.ArchivedCount != 1 {
		t.Errorf("archived = %d, want 1", result2.Summary.ArchivedCount)
	}
}
