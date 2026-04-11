package git

import (
	"os"
	"path/filepath"
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
	cwd, _ := os.Getwd()
	expected := filepath.Base(cwd)
	got := ProjectName()
	if got != expected {
		t.Errorf("expected ProjectName() = %q, got %q", expected, got)
	}
}

func TestOpen(t *testing.T) {
	repo, err := Open("")
	if err != nil {
		t.Fatalf("Open('') failed: %v", err)
	}
	if repo == nil {
		t.Fatal("Open('') returned nil repo")
	}
	if repo.R == nil {
		t.Fatal("Open('') returned repo with nil R")
	}
}

func TestCurrentBranch(t *testing.T) {
	repo, err := Open("")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	branch := repo.CurrentBranch()
	if branch == "" {
		t.Error("CurrentBranch() returned empty string")
	}
}

func TestDetectBaseBranch(t *testing.T) {
	repo, err := Open("")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	base := repo.DetectBaseBranch()
	if base != "main" {
		t.Errorf("expected DetectBaseBranch() = 'main', got %q", base)
	}
}

func TestDetectBaseBranchSimple(t *testing.T) {
	repo, err := Open("")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	base := repo.DetectBaseBranchSimple()
	if base != "main" {
		t.Errorf("expected DetectBaseBranchSimple() = 'main', got %q", base)
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
	// This repo has no homolog branch
	has := HasHomologBranch("")
	if has {
		t.Error("expected HasHomologBranch() = false for this repo")
	}
}
