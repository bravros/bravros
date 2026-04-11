package projectinit

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// setupTestRepo creates a temp dir with git init and returns the path.
func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	// Configure git user for commits
	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = dir
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test")
	cmd.Dir = dir
	cmd.Run()

	// Create an initial commit so HEAD exists
	dummy := filepath.Join(dir, ".gitkeep")
	os.WriteFile(dummy, []byte(""), 0644)
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = dir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "initial")
	cmd.Dir = dir
	cmd.Run()

	return dir
}

// setupHooksTemplates creates fake hook templates in a temp dir and sets the override.
func setupHooksTemplates(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "commit-msg"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	os.WriteFile(filepath.Join(dir, "pre-push"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Hooks\n"), 0644)

	HooksSourceOverride = dir
	t.Cleanup(func() { HooksSourceOverride = "" })

	return dir
}

func TestFullInit(t *testing.T) {
	root := setupTestRepo(t)
	setupHooksTemplates(t)

	// Create a go.mod so stack detection finds something
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644)

	result, err := Init(InitOpts{
		Root: root,
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if result.AlreadyInitialized {
		t.Error("expected AlreadyInitialized=false on fresh init")
	}
	if !result.ConfigWritten {
		t.Error("expected ConfigWritten=true")
	}
	if !result.PlanningDirCreated {
		t.Error("expected PlanningDirCreated=true")
	}
	if !result.HooksInstalled {
		t.Error("expected HooksInstalled=true")
	}
	if result.Stack != "none" {
		// go.mod detected → language=go, framework=none
		t.Errorf("expected stack='none', got '%s'", result.Stack)
	}

	// Verify .bravros.yml exists
	if _, err := os.Stat(filepath.Join(root, ".bravros.yml")); os.IsNotExist(err) {
		t.Error(".bravros.yml not created")
	}

	// Verify .planning/backlog/archive/ exists
	if _, err := os.Stat(filepath.Join(root, ".planning", "backlog", "archive")); os.IsNotExist(err) {
		t.Error(".planning/backlog/archive/ not created")
	}

	// Verify hooks copied
	if _, err := os.Stat(filepath.Join(root, ".githooks", "commit-msg")); os.IsNotExist(err) {
		t.Error(".githooks/commit-msg not copied")
	}
	if _, err := os.Stat(filepath.Join(root, ".githooks", "pre-push")); os.IsNotExist(err) {
		t.Error(".githooks/pre-push not copied")
	}

	// Verify .github/workflows/ exists
	if _, err := os.Stat(filepath.Join(root, ".github", "workflows")); os.IsNotExist(err) {
		t.Error(".github/workflows/ not created")
	}

	// Verify staging branch created
	if !result.StagingBranchCreated {
		t.Error("expected StagingBranchCreated=true")
	}
	cmd := exec.Command("git", "branch", "--list", "homolog")
	cmd.Dir = root
	out, _ := cmd.Output()
	if len(out) == 0 {
		t.Error("homolog branch not created")
	}
}

func TestSkipFlags(t *testing.T) {
	root := setupTestRepo(t)
	setupHooksTemplates(t)

	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644)

	result, err := Init(InitOpts{
		Root:              root,
		SkipHooks:         true,
		SkipWorkflows:     true,
		SkipStagingBranch: true,
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if result.HooksInstalled {
		t.Error("expected HooksInstalled=false with --skip-hooks")
	}
	if result.WorkflowsCreated != nil {
		t.Error("expected WorkflowsCreated=nil with --skip-workflows")
	}
	if result.StagingBranchCreated {
		t.Error("expected StagingBranchCreated=false with --skip-staging-branch")
	}

	// Verify hooks dir NOT created
	if _, err := os.Stat(filepath.Join(root, ".githooks")); !os.IsNotExist(err) {
		t.Error(".githooks should not exist with --skip-hooks")
	}

	// Verify workflows dir NOT created
	if _, err := os.Stat(filepath.Join(root, ".github", "workflows")); !os.IsNotExist(err) {
		t.Error(".github/workflows should not exist with --skip-workflows")
	}

	// Config and planning should still be created
	if !result.ConfigWritten {
		t.Error("expected ConfigWritten=true even with skip flags")
	}
	if !result.PlanningDirCreated {
		t.Error("expected PlanningDirCreated=true even with skip flags")
	}
}

func TestAlreadyInitialized(t *testing.T) {
	root := setupTestRepo(t)
	setupHooksTemplates(t)

	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644)

	// Create .bravros.yml first
	os.WriteFile(filepath.Join(root, ".bravros.yml"), []byte("staging_branch: homolog\n"), 0644)

	result, err := Init(InitOpts{
		Root:              root,
		SkipStagingBranch: true,
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if !result.AlreadyInitialized {
		t.Error("expected AlreadyInitialized=true when .bravros.yml exists")
	}
	// Should still update config
	if !result.ConfigWritten {
		t.Error("expected ConfigWritten=true even when already initialized")
	}
}

func TestStackOverride(t *testing.T) {
	root := setupTestRepo(t)
	setupHooksTemplates(t)

	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644)

	result, err := Init(InitOpts{
		Root:              root,
		StackOverride:     "laravel",
		SkipHooks:         true,
		SkipWorkflows:     true,
		SkipStagingBranch: true,
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if result.Stack != "laravel" {
		t.Errorf("expected stack='laravel' with override, got '%s'", result.Stack)
	}
}

func TestConfigGeneratedCorrectly(t *testing.T) {
	root := setupTestRepo(t)
	setupHooksTemplates(t)

	// Create package.json with next dependency
	os.WriteFile(filepath.Join(root, "package.json"), []byte(`{
  "dependencies": {
    "next": "14.0.0",
    "react": "18.0.0"
  }
}`), 0644)

	result, err := Init(InitOpts{
		Root:              root,
		SkipHooks:         true,
		SkipWorkflows:     true,
		SkipStagingBranch: true,
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if result.Stack != "nextjs" {
		t.Errorf("expected stack='nextjs', got '%s'", result.Stack)
	}

	// Read and verify .bravros.yml content
	data, err := os.ReadFile(filepath.Join(root, ".bravros.yml"))
	if err != nil {
		t.Fatalf("failed to read .bravros.yml: %v", err)
	}

	content := string(data)
	if len(content) == 0 {
		t.Error(".bravros.yml is empty")
	}
}
