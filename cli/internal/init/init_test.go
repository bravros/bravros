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

	// Verify config.json exists
	if _, err := os.Stat(filepath.Join(root, ".bravros", "config.json")); os.IsNotExist(err) {
		t.Error(".bravros/config.json not created")
	}

	// Verify .planning/backlog/archive/ exists
	if _, err := os.Stat(filepath.Join(root, ".planning", "backlog", "archive")); os.IsNotExist(err) {
		t.Error(".planning/backlog/archive/ not created")
	}

	// Verify hooks copied
	if _, err := os.Stat(filepath.Join(root, ".bravros", "hooks", "commit-msg")); os.IsNotExist(err) {
		t.Error(".bravros/hooks/commit-msg not copied")
	}
	if _, err := os.Stat(filepath.Join(root, ".bravros", "hooks", "pre-push")); os.IsNotExist(err) {
		t.Error(".bravros/hooks/pre-push not copied")
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
	if _, err := os.Stat(filepath.Join(root, ".bravros", "hooks")); !os.IsNotExist(err) {
		t.Error(".bravros/hooks should not exist with --skip-hooks")
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

func TestInstallHooksRerunPreservesTrackedCustomHook(t *testing.T) {
	root := setupTestRepo(t)
	setupHooksTemplates(t)

	if _, err := installHooks(root); err != nil {
		t.Fatalf("initial installHooks failed: %v", err)
	}

	customHook := filepath.Join(root, ".bravros", "hooks", "commit-msg")
	customBody := []byte("#!/bin/sh\n# project custom hook\nexit 0\n")
	if err := os.WriteFile(customHook, customBody, 0755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("git", "add", ".bravros")
	cmd.Dir = root
	if err := cmd.Run(); err != nil {
		t.Fatalf("git add .bravros failed: %v", err)
	}
	cmd = exec.Command("git", "commit", "-m", "track custom hooks")
	cmd.Dir = root
	if err := cmd.Run(); err != nil {
		t.Fatalf("git commit hooks failed: %v", err)
	}

	if _, err := installHooks(root); err != nil {
		t.Fatalf("second installHooks failed: %v", err)
	}

	got, err := os.ReadFile(customHook)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(customBody) {
		t.Fatalf("custom hook was overwritten: %q", got)
	}

	cmd = exec.Command("git", "status", "--short", "--", ".bravros")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git status failed: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("installHooks rerun caused .bravros drift:\n%s", out)
	}
}

func TestAlreadyInitialized(t *testing.T) {
	root := setupTestRepo(t)
	setupHooksTemplates(t)

	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644)

	// Create config.json first
	_ = os.MkdirAll(filepath.Join(root, ".bravros"), 0755)
	os.WriteFile(filepath.Join(root, ".bravros", "config.json"), []byte(`{"staging_branch": "homolog"}`), 0644)

	result, err := Init(InitOpts{
		Root:              root,
		SkipStagingBranch: true,
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if !result.AlreadyInitialized {
		t.Error("expected AlreadyInitialized=true when config.json exists")
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

	// Read and verify config.json content
	data, err := os.ReadFile(filepath.Join(root, ".bravros", "config.json"))
	if err != nil {
		t.Fatalf("failed to read config.json: %v", err)
	}

	content := string(data)
	if len(content) == 0 {
		t.Error("config.json is empty")
	}
}
