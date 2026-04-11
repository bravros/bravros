package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

// setupTestRepo creates a mock claude config repo with deployable files.
func setupTestRepo(t *testing.T) string {
	t.Helper()
	// Must end with "claude" to pass IsClaudeRepo check
	base := filepath.Join(t.TempDir(), "claude")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Create directory structure
	must(os.MkdirAll(filepath.Join(base, "skills", "commit"), 0755))
	must(os.MkdirAll(filepath.Join(base, "skills", "plan"), 0755))
	must(os.MkdirAll(filepath.Join(base, "hooks"), 0755))
	must(os.MkdirAll(filepath.Join(base, "templates"), 0755))
	must(os.MkdirAll(filepath.Join(base, "config"), 0755))

	// Create files
	must(os.WriteFile(filepath.Join(base, "skills", "commit", "SKILL.md"), []byte("commit skill"), 0644))
	must(os.WriteFile(filepath.Join(base, "skills", "plan", "SKILL.md"), []byte("plan skill"), 0644))
	must(os.WriteFile(filepath.Join(base, "hooks", "pre-commit"), []byte("#!/bin/sh"), 0755))
	must(os.WriteFile(filepath.Join(base, "templates", "plan.md"), []byte("# Plan"), 0644))
	must(os.WriteFile(filepath.Join(base, "config", "settings.json"), []byte(`{"key":"val"}`), 0644))
	must(os.WriteFile(filepath.Join(base, "config", "statusline.sh"), []byte("echo hi"), 0644))
	must(os.WriteFile(filepath.Join(base, "CLAUDE.md"), []byte("# Claude"), 0644))

	// Also create a .DS_Store that should be skipped
	must(os.WriteFile(filepath.Join(base, "skills", ".DS_Store"), []byte("junk"), 0644))

	// Create mcp.json that should NOT be deployed
	must(os.WriteFile(filepath.Join(base, "mcp.json"), []byte(`{"mcp":true}`), 0644))

	return base
}

func TestDryRunNoCopies(t *testing.T) {
	src := setupTestRepo(t)
	target := filepath.Join(t.TempDir(), ".claude")

	result, err := Deploy(DeployOpts{
		DryRun:    true,
		SourceDir: src,
		TargetDir: target,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.FilesDeployed == 0 {
		t.Fatal("expected files to deploy, got 0")
	}
	if !result.DryRun {
		t.Fatal("expected DryRun=true")
	}
	if len(result.Files) == 0 {
		t.Fatal("expected file list in dry-run mode")
	}

	// Target dir should NOT exist (nothing copied)
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("target dir should not exist in dry-run mode")
	}
}

func TestCountOnlyMode(t *testing.T) {
	src := setupTestRepo(t)
	target := filepath.Join(t.TempDir(), ".claude")

	result, err := Deploy(DeployOpts{
		CountOnly: true,
		SourceDir: src,
		TargetDir: target,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.CountOnly {
		t.Fatal("expected CountOnly=true")
	}
	if result.FilesDeployed == 0 {
		t.Fatal("expected non-zero file count")
	}
	// Files list should be empty in count-only mode
	if len(result.Files) != 0 {
		t.Fatalf("expected empty file list in count-only, got %d", len(result.Files))
	}

	// Target dir should NOT exist
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("target dir should not exist in count-only mode")
	}
}

func TestFileMappingCorrectness(t *testing.T) {
	src := setupTestRepo(t)
	target := filepath.Join(t.TempDir(), ".claude")

	result, err := Deploy(DeployOpts{
		SourceDir: src,
		TargetDir: target,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 7 files: 2 skills + 1 hook + 1 template + settings.json + statusline.sh + CLAUDE.md
	if result.FilesDeployed != 7 {
		t.Fatalf("expected 7 files deployed, got %d (files: %v)", result.FilesDeployed, result.Files)
	}

	// Verify config/settings.json → settings.json mapping
	settingsDst := filepath.Join(target, "settings.json")
	if _, err := os.Stat(settingsDst); os.IsNotExist(err) {
		t.Fatal("settings.json not deployed to target root")
	}

	// Verify config/statusline.sh → statusline.sh mapping
	statusDst := filepath.Join(target, "statusline.sh")
	if _, err := os.Stat(statusDst); os.IsNotExist(err) {
		t.Fatal("statusline.sh not deployed to target root")
	}

	// Verify CLAUDE.md deployed
	claudeDst := filepath.Join(target, "CLAUDE.md")
	if _, err := os.Stat(claudeDst); os.IsNotExist(err) {
		t.Fatal("CLAUDE.md not deployed")
	}

	// Verify skills directory deployed
	skillDst := filepath.Join(target, "skills", "commit", "SKILL.md")
	if _, err := os.Stat(skillDst); os.IsNotExist(err) {
		t.Fatal("skills/commit/SKILL.md not deployed")
	}

	// Verify mcp.json NOT deployed
	mcpDst := filepath.Join(target, "mcp.json")
	if _, err := os.Stat(mcpDst); !os.IsNotExist(err) {
		t.Fatal("mcp.json should NOT be deployed")
	}

	// Verify .DS_Store NOT deployed
	dsDst := filepath.Join(target, "skills", ".DS_Store")
	if _, err := os.Stat(dsDst); !os.IsNotExist(err) {
		t.Fatal(".DS_Store should NOT be deployed")
	}
}

func TestNonClaudeRepoDetection(t *testing.T) {
	// Create a temp dir that is NOT named "claude"
	notClaude := filepath.Join(t.TempDir(), "my-project")
	if err := os.MkdirAll(notClaude, 0755); err != nil {
		t.Fatal(err)
	}

	_, err := Deploy(DeployOpts{
		SourceDir: notClaude,
		TargetDir: filepath.Join(t.TempDir(), ".claude"),
	})
	if err == nil {
		t.Fatal("expected error for non-claude repo")
	}
	if err.Error() != `not the claude config repo (cwd basename must be "claude")` {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestIsClaudeRepo(t *testing.T) {
	tests := []struct {
		dir  string
		want bool
	}{
		{"/Users/me/Sites/claude", true},
		{"/Users/me/Sites/my-app", false},
		{"/tmp/claude", true},
		{"/tmp/bravros", false},
	}
	for _, tt := range tests {
		got := IsClaudeRepo(tt.dir)
		if got != tt.want {
			t.Errorf("IsClaudeRepo(%q) = %v, want %v", tt.dir, got, tt.want)
		}
	}
}

func TestDeployableFile(t *testing.T) {
	tests := []struct {
		rel  string
		want bool
	}{
		{"skills/commit/SKILL.md", true},
		{"hooks/pre-commit", true},
		{"templates/plan.md", true},
		{"config/settings.json", true},
		{"config/statusline.sh", true},
		{"CLAUDE.md", true},
		{"mcp.json", false},
		{"scripts/old.sh", false},
		{".planning/backlog/001.md", false},
	}
	for _, tt := range tests {
		got := DeployableFile(tt.rel)
		if got != tt.want {
			t.Errorf("DeployableFile(%q) = %v, want %v", tt.rel, got, tt.want)
		}
	}
}

func TestDeployDirsInResult(t *testing.T) {
	src := setupTestRepo(t)
	target := filepath.Join(t.TempDir(), ".claude")

	result, err := Deploy(DeployOpts{
		DryRun:    true,
		SourceDir: src,
		TargetDir: target,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have hooks, skills, templates
	dirMap := map[string]bool{}
	for _, d := range result.Dirs {
		dirMap[d] = true
	}
	for _, want := range []string{"skills", "hooks", "templates"} {
		if !dirMap[want] {
			t.Errorf("expected dir %q in result.Dirs, got %v", want, result.Dirs)
		}
	}
}
