package deploy

import (
	"io/fs"
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

	// 6 files: 2 skills + 1 hook + 1 template + settings.json + statusline.sh.
	// CLAUDE.md is NOT a plain file mapping — it's reconciled from home/CLAUDE.md,
	// not whole-file copied (which would clobber the user's personal content).
	if result.FilesDeployed != 6 {
		t.Fatalf("expected 6 files deployed, got %d (files: %v)", result.FilesDeployed, result.Files)
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

	// CLAUDE.md is NOT deployed as a plain file — it's reconciled from
	// home/CLAUDE.md (absent in this test repo, so the reconcile no-ops and the
	// target CLAUDE.md is intentionally not created). The no-clobber behavior is
	// covered by TestDeploy_DoesNotClobberPersonalCLAUDEmd.

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

func TestSharedSkillDirSkippedAndConsumerSymlinksMaterialized(t *testing.T) {
	src := setupTestRepo(t)
	target := filepath.Join(t.TempDir(), ".claude")

	sharedDir := filepath.Join(src, "skills", "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}
	sharedFile := filepath.Join(sharedDir, "team-execution.md")
	if err := os.WriteFile(sharedFile, []byte("shared body"), 0644); err != nil {
		t.Fatal(err)
	}

	refsDir := filepath.Join(src, "skills", "plan", "references")
	if err := os.MkdirAll(refsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "..", "shared", "team-execution.md"), filepath.Join(refsDir, "team-execution.md")); err != nil {
		t.Fatal(err)
	}

	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target}); err != nil {
		t.Fatalf("deploy: %v", err)
	}

	if _, err := os.Stat(filepath.Join(target, "skills", "shared")); !os.IsNotExist(err) {
		t.Fatalf("skills/shared must not be deployed, stat err: %v", err)
	}

	deployedRef := filepath.Join(target, "skills", "plan", "references", "team-execution.md")
	info, err := os.Lstat(deployedRef)
	if err != nil {
		t.Fatalf("materialized reference missing: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("materialized reference should be a regular file, got symlink")
	}
	got, err := os.ReadFile(deployedRef)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "shared body" {
		t.Fatalf("materialized reference content = %q", got)
	}
}

func TestSharedSkillDirPrunedEvenWhenSourceExists(t *testing.T) {
	src := setupTestRepo(t)
	target := filepath.Join(t.TempDir(), ".claude")

	if err := os.MkdirAll(filepath.Join(src, "skills", "shared"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "skills", "shared", "x.md"), []byte("shared"), 0644); err != nil {
		t.Fatal(err)
	}

	deployedShared := filepath.Join(target, "skills", "shared")
	if err := os.MkdirAll(deployedShared, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deployedShared, "x.md"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}

	want := filepath.Join("skills", "shared")
	found := false
	for _, p := range result.Pruned {
		if p == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected %q in pruned list, got %v", want, result.Pruned)
	}
	if _, err := os.Stat(deployedShared); !os.IsNotExist(err) {
		t.Fatalf("deployed skills/shared should have been pruned, stat err: %v", err)
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
		{"/tmp/claude-cli", false},
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
		{"skills/shared/team-execution.md", false},
		{"hooks/pre-commit", true},
		{"templates/plan.md", true},
		{"config/settings.json", true},
		{"config/statusline.sh", true},
		{"CLAUDE.md", false}, // reconciled from home/CLAUDE.md, not a plain file mapping
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

// TestGithooksDeployedAndExecutable verifies that templates/.githooks/ hook files
// are deployed to the target directory with the executable bit set. This tests
// the explicit chmod step added by B-0154 — ensuring hooks work even if source
// file modes were accidentally stripped.
func TestGithooksDeployedAndExecutable(t *testing.T) {
	// Must end with "claude" to pass IsClaudeRepo check
	base := filepath.Join(t.TempDir(), "claude")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Create minimal repo structure
	must(os.MkdirAll(filepath.Join(base, "templates", ".githooks"), 0755))
	must(os.MkdirAll(filepath.Join(base, "config"), 0755))
	must(os.WriteFile(filepath.Join(base, "config", "settings.json"), []byte(`{}`), 0644))
	must(os.WriteFile(filepath.Join(base, "CLAUDE.md"), []byte("# Claude"), 0644))

	// Hook files: write with non-executable mode (0644) to test the explicit chmod
	hookFiles := []string{"commit-msg", "pre-push", "post-commit"}
	for _, name := range hookFiles {
		must(os.WriteFile(
			filepath.Join(base, "templates", ".githooks", name),
			[]byte("#!/bin/sh\nexit 0"),
			0644, // intentionally non-executable — deploy must fix this
		))
	}
	// README.md: documentation only, should NOT be required to be executable
	must(os.WriteFile(
		filepath.Join(base, "templates", ".githooks", "README.md"),
		[]byte("# hooks"),
		0644,
	))

	target := filepath.Join(t.TempDir(), ".claude")
	_, err := Deploy(DeployOpts{
		SourceDir: base,
		TargetDir: target,
	})
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}

	githooksDst := filepath.Join(target, "templates", ".githooks")

	// Each hook file must be executable after deploy
	for _, name := range hookFiles {
		path := filepath.Join(githooksDst, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("hook %s not deployed: %v", name, err)
		}
		if info.Mode()&0111 == 0 {
			t.Errorf("hook %s is not executable (mode %v)", name, info.Mode())
		}
	}

	// README.md must be present (Deploy does not filter it)
	readmePath := filepath.Join(githooksDst, "README.md")
	if _, err := os.Stat(readmePath); os.IsNotExist(err) {
		t.Fatal("README.md not deployed from templates/.githooks/")
	}
}

func TestPruneGithooksFileOrphansInsideDotDir(t *testing.T) {
	src := setupTestRepo(t)
	target := filepath.Join(t.TempDir(), ".claude")

	srcHooks := filepath.Join(src, "templates", ".githooks")
	if err := os.MkdirAll(srcHooks, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcHooks, "commit-msg"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target}); err != nil {
		t.Fatalf("initial deploy: %v", err)
	}

	orphan := filepath.Join(target, "templates", ".githooks", "pre-push")
	if err := os.WriteFile(orphan, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	result, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}

	want := filepath.Join("templates", ".githooks", "pre-push")
	found := false
	for _, p := range result.Pruned {
		if p == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected %q in result.Pruned, got %v", want, result.Pruned)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("githooks orphan should be pruned, stat err: %v", err)
	}
}

// TestPruneOrphansDefault verifies that orphan skills (deployed but absent
// from source) are removed by default and listed in result.Pruned. Covers
// B-0193 acceptance: rename flow auto-prunes leftover dirs.
func TestPruneOrphansDefault(t *testing.T) {
	src := setupTestRepo(t)
	target := filepath.Join(t.TempDir(), ".claude")

	// First deploy: populate target.
	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target}); err != nil {
		t.Fatalf("initial deploy: %v", err)
	}

	// Simulate a rename: drop a stray skill directory at the destination
	// that has no source counterpart.
	orphanDir := filepath.Join(target, "skills", "deprecated-skill")
	if err := os.MkdirAll(orphanDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphanDir, "SKILL.md"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	// Second deploy: prune (default) should remove the orphan.
	result, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target})
	if err != nil {
		t.Fatalf("second deploy: %v", err)
	}
	wantOrphan := filepath.Join("skills", "deprecated-skill")
	found := false
	for _, p := range result.Pruned {
		if p == wantOrphan {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected orphan %q in result.Pruned, got %v", wantOrphan, result.Pruned)
	}
	if _, err := os.Stat(orphanDir); !os.IsNotExist(err) {
		t.Fatalf("orphan dir %s should have been removed", orphanDir)
	}
}

// TestNoPrunePreservesOrphans verifies that --no-prune leaves orphans intact
// and result.Pruned is empty.
func TestNoPrunePreservesOrphans(t *testing.T) {
	src := setupTestRepo(t)
	target := filepath.Join(t.TempDir(), ".claude")

	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target}); err != nil {
		t.Fatalf("initial deploy: %v", err)
	}

	orphanDir := filepath.Join(target, "skills", "kept-orphan")
	if err := os.MkdirAll(orphanDir, 0755); err != nil {
		t.Fatal(err)
	}

	result, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target, NoPrune: true})
	if err != nil {
		t.Fatalf("deploy with NoPrune: %v", err)
	}
	if len(result.Pruned) != 0 {
		t.Fatalf("expected empty Pruned with NoPrune=true, got %v", result.Pruned)
	}
	if _, err := os.Stat(orphanDir); err != nil {
		t.Fatalf("orphan dir should remain with NoPrune=true: %v", err)
	}
}

// TestDryRunListsOrphansWithoutPruning verifies that --dry-run reports
// would-be-pruned orphans but does not remove anything.
func TestDryRunListsOrphansWithoutPruning(t *testing.T) {
	src := setupTestRepo(t)
	target := filepath.Join(t.TempDir(), ".claude")

	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target}); err != nil {
		t.Fatalf("initial deploy: %v", err)
	}

	orphanDir := filepath.Join(target, "skills", "ghost")
	if err := os.MkdirAll(orphanDir, 0755); err != nil {
		t.Fatal(err)
	}

	result, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target, DryRun: true})
	if err != nil {
		t.Fatalf("dry-run deploy: %v", err)
	}
	if len(result.Pruned) == 0 {
		t.Fatal("expected orphans in dry-run result.Pruned")
	}
	// Dry-run must not actually delete the orphan.
	if _, err := os.Stat(orphanDir); err != nil {
		t.Fatalf("orphan dir should remain in dry-run mode: %v", err)
	}
}

// TestPruneAllowlistGuardsBin verifies that the prune logic only walks the
// allowlisted subtrees (skills, hooks, templates) and never touches
// bin/, projects/, state/ etc. Even though those dirs have no source
// counterpart, they must remain untouched.
func TestPruneAllowlistGuardsBin(t *testing.T) {
	src := setupTestRepo(t)
	target := filepath.Join(t.TempDir(), ".claude")

	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target}); err != nil {
		t.Fatalf("initial deploy: %v", err)
	}

	// Create non-allowlisted user data that has no source counterpart.
	mustDir := func(p string) {
		t.Helper()
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
	}
	mustDir(filepath.Join(target, "bin"))
	mustDir(filepath.Join(target, "projects", "foo"))
	mustDir(filepath.Join(target, "state"))
	binFile := filepath.Join(target, "bin", "kaisser")
	if err := os.WriteFile(binFile, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}

	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target}); err != nil {
		t.Fatalf("deploy: %v", err)
	}

	// User data must be intact.
	for _, p := range []string{
		filepath.Join(target, "bin"),
		filepath.Join(target, "projects", "foo"),
		filepath.Join(target, "state"),
		binFile,
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("allowlist guard failed: %s removed (%v)", p, err)
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

// TestDeployBrokenSymlinkCleaned verifies that a broken symlink at the
// destination is removed before deploy and replaced with the real source
// content. This covers the B-0230 root cause: copyFile errors with ENOENT
// when writing through a broken symlink, leaving the broken link in place.
func TestDeployBrokenSymlinkCleaned(t *testing.T) {
	src := setupTestRepo(t)
	target := filepath.Join(t.TempDir(), ".claude")

	// First: do an initial deploy to set up the destination.
	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target}); err != nil {
		t.Fatalf("initial deploy: %v", err)
	}

	// Simulate a broken symlink left by an old installer run:
	// replace the deployed SKILL.md for "commit" with a broken symlink pointing
	// to a shared target that no longer exists.
	brokenLink := filepath.Join(target, "skills", "commit", "SKILL.md")
	if err := os.Remove(brokenLink); err != nil {
		t.Fatalf("remove real file: %v", err)
	}
	// Create a symlink pointing at a non-existent target.
	if err := os.Symlink("/nonexistent/shared/pipeline.md", brokenLink); err != nil {
		t.Fatalf("create broken symlink: %v", err)
	}
	// Verify the symlink is indeed broken (Lstat succeeds, Stat fails).
	if _, err := os.Lstat(brokenLink); err != nil {
		t.Fatalf("Lstat should succeed on broken symlink: %v", err)
	}
	if _, err := os.Stat(brokenLink); err == nil {
		t.Fatal("Stat should fail on broken symlink (target missing)")
	}

	// Second deploy: should clean the broken symlink and copy real content.
	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target}); err != nil {
		t.Fatalf("deploy with broken symlink: %v", err)
	}

	// After deploy, the path must be a regular file with the correct content —
	// not a symlink.
	info, err := os.Lstat(brokenLink)
	if err != nil {
		t.Fatalf("deployed file missing after broken-symlink cleanup: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("path is still a symlink after deploy — broken-symlink cleanup did not run")
	}
	got, err := os.ReadFile(brokenLink)
	if err != nil {
		t.Fatalf("read deployed file: %v", err)
	}
	if string(got) != "commit skill" {
		t.Fatalf("deployed content = %q, want %q", string(got), "commit skill")
	}
}

// TestDeployNoBrokenSymlinksNoSpuriousDeletions verifies that a clean
// destination with no broken symlinks is left entirely intact by the
// cleanBrokenSymlinks pre-pass — no regular files or directories are removed.
func TestDeployNoBrokenSymlinksNoSpuriousDeletions(t *testing.T) {
	src := setupTestRepo(t)
	target := filepath.Join(t.TempDir(), ".claude")

	// Initial deploy.
	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target}); err != nil {
		t.Fatalf("initial deploy: %v", err)
	}

	// Capture every file path present after the first deploy.
	var before []string
	err := filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		before = append(before, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk target before second deploy: %v", err)
	}

	// Second deploy on a clean destination — no broken symlinks present.
	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target}); err != nil {
		t.Fatalf("second deploy: %v", err)
	}

	// Every path that existed before should still exist after the second deploy.
	for _, p := range before {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("path %s was removed spuriously by second deploy: %v", p, err)
		}
	}
}

// TestDetectOrphans_HonorsPreserveList verifies that skill directories listed
// in the preserve slice are NOT marked as orphans even when absent from source.
//
// Covers B-0237: manually-added ~/.claude/skills/graphify/ must survive prune
// when the user opts in via .kaisser.yml:skills.preserve: [graphify].
func TestDetectOrphans_HonorsPreserveList(t *testing.T) {
	// Source: minimal claude repo with one real skill.
	src := filepath.Join(t.TempDir(), "claude")
	if err := os.MkdirAll(filepath.Join(src, "skills", "plan"), 0o755); err != nil {
		t.Fatalf("mkdir src skills/plan: %v", err)
	}

	// Target: deployed dir with the source skill PLUS graphify (manually added).
	dst := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dst, "skills", "plan"), 0o755); err != nil {
		t.Fatalf("mkdir dst skills/plan: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dst, "skills", "graphify"), 0o755); err != nil {
		t.Fatalf("mkdir dst skills/graphify: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dst, "skills", "graphify", "SKILL.md"), []byte("graphify"), 0o644); err != nil {
		t.Fatalf("write graphify SKILL.md: %v", err)
	}

	// With preserve list including "graphify" — graphify must NOT appear in orphans.
	orphans, err := detectOrphans(src, dst, []string{"graphify"})
	if err != nil {
		t.Fatalf("detectOrphans: %v", err)
	}
	for _, o := range orphans {
		if o == "skills/graphify" || filepath.Base(o) == "graphify" {
			t.Errorf("graphify appeared in orphan list despite being preserved: %v", orphans)
		}
	}

	// Control: without preserve list, graphify MUST appear in orphans.
	orphansNoPreserve, err := detectOrphans(src, dst, nil)
	if err != nil {
		t.Fatalf("detectOrphans (no preserve): %v", err)
	}
	found := false
	for _, o := range orphansNoPreserve {
		if o == "skills/graphify" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected skills/graphify in orphans without preserve list; got: %v", orphansNoPreserve)
	}
}

// TestDeployLockedFileSkippedNotFatal covers the operator-frozen-config path:
// a destination the operator deliberately locked (macOS `chflags uchg`, Linux
// `chattr +i`, or read-only perms) must be SKIPPED, and the deploy must carry
// on so every file after it still lands.
//
// Regression guard: a locked ~/.claude/settings.json used to abort the whole
// deploy, which silently left ~/.claude/scripts/ months stale while
// `kaisser update` still reported success.
func TestDeployLockedFileSkippedNotFatal(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — permission bits are not enforced")
	}
	src := setupTestRepo(t)
	target := filepath.Join(t.TempDir(), ".claude")

	// First deploy normally so the destination exists.
	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target}); err != nil {
		t.Fatalf("initial deploy: %v", err)
	}

	// Lock settings.json (read-only file in a writable dir → open-for-write
	// fails with EACCES, the same class as uchg/chattr +i).
	locked := filepath.Join(target, "settings.json")
	if err := os.Chmod(locked, 0444); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0644) })

	// Change the SOURCE so the deploy actually wants to rewrite the locked file,
	// and change a sibling so we can prove the loop continued past the failure.
	if err := os.WriteFile(filepath.Join(src, "config", "settings.json"), []byte(`{"key":"CHANGED"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "templates", "plan.md"), []byte("# Plan v2"), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target, Force: true})
	if err != nil {
		t.Fatalf("locked destination must NOT fail the deploy, got: %v", err)
	}

	// The locked file is reported, not silently dropped.
	found := false
	for _, f := range res.LockedSkipped {
		if f == "settings.json" {
			found = true
		}
	}
	if !found {
		t.Errorf("LockedSkipped = %v, want it to contain settings.json", res.LockedSkipped)
	}

	// The locked file kept its original content — never partially written.
	got, err := os.ReadFile(locked)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"key":"val"}` {
		t.Errorf("locked file content = %q, want the original %q", got, `{"key":"val"}`)
	}

	// The whole point: files after the locked one still deployed.
	after, err := os.ReadFile(filepath.Join(target, "templates", "plan.md"))
	if err != nil {
		t.Fatalf("file after the locked one was not deployed: %v", err)
	}
	if string(after) != "# Plan v2" {
		t.Errorf("templates/plan.md = %q, want %q — deploy stopped at the locked file", after, "# Plan v2")
	}
}
