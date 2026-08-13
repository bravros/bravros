package plan

// LIFT: from bravros/private/cli/internal/plan/commit_test.go (2026-04-18 DRY pass)

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initGitRepoForCommit creates a temp git repo, chdirs into it, and returns cleanup.
func initGitRepoForCommit(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "checkout", "-b", "main"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v failed: %v\n%s", c, err, out)
		}
	}

	// Initial commit
	f := filepath.Join(dir, "README.md")
	os.WriteFile(f, []byte("# test\n"), 0644)
	for _, c := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial"},
	} {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v failed: %v\n%s", c, err, out)
		}
	}

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	return dir, func() { os.Chdir(origDir) }
}

func TestCommit_EmptyMessage(t *testing.T) {
	_, cleanup := initGitRepoForCommit(t)
	defer cleanup()

	err := Commit("", nil)
	if err == nil {
		t.Fatal("expected error for empty message, got nil")
	}
	if !strings.Contains(err.Error(), "commit message required") {
		t.Errorf("expected 'commit message required', got %q", err.Error())
	}
}

func TestCommit_NothingToCommit(t *testing.T) {
	_, cleanup := initGitRepoForCommit(t)
	defer cleanup()

	// Nothing staged — should print skip message and return nil.
	var buf bytes.Buffer
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := Commit("✨ feat: test nothing", nil)

	w.Close()
	buf.ReadFrom(r)
	os.Stdout = orig

	if err != nil {
		t.Fatalf("expected nil error for nothing-to-commit, got: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "nothing to commit") {
		t.Errorf("expected 'nothing to commit' in output, got: %q", output)
	}
}

func TestCommit_WithExtraFiles(t *testing.T) {
	dir, cleanup := initGitRepoForCommit(t)
	defer cleanup()

	// Create a new file and commit it via extraFiles.
	newFile := filepath.Join(dir, "new.txt")
	os.WriteFile(newFile, []byte("hello"), 0644)

	err := Commit("✨ feat: add new file", []string{"new.txt"})
	if err != nil {
		t.Fatalf("Commit with extraFiles failed: %v", err)
	}

	// Verify the commit was created.
	cmd := exec.Command("git", "log", "--oneline", "-1")
	cmd.Dir = dir
	out, _ := cmd.Output()
	if !strings.Contains(string(out), "add new file") {
		t.Errorf("expected commit with 'add new file', got: %q", string(out))
	}
}

func TestCommit_StagesTrackedModifiedFiles(t *testing.T) {
	dir, cleanup := initGitRepoForCommit(t)
	defer cleanup()

	// Modify README.md (tracked file) — no extraFiles means git add -u.
	readme := filepath.Join(dir, "README.md")
	os.WriteFile(readme, []byte("# modified\n"), 0644)

	err := Commit("📚 docs: update readme", nil)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify the commit.
	cmd := exec.Command("git", "log", "--oneline", "-1")
	cmd.Dir = dir
	out, _ := cmd.Output()
	if !strings.Contains(string(out), "update readme") {
		t.Errorf("expected commit with 'update readme', got: %q", string(out))
	}
}

func TestCommit_PlanningDirAlwaysStaged(t *testing.T) {
	dir, cleanup := initGitRepoForCommit(t)
	defer cleanup()

	// Create .planning/ dir and file.
	planDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planDir, 0755)
	planFile := filepath.Join(planDir, "0001-test-todo.md")
	os.WriteFile(planFile, []byte("---\nstatus: new\n---\n# Plan\n"), 0644)

	err := Commit("📋 plan: add plan file", nil)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify .planning/ was committed.
	cmd := exec.Command("git", "show", "--name-only", "--format=%s")
	cmd.Dir = dir
	out, _ := cmd.Output()
	if !strings.Contains(string(out), ".planning/") {
		t.Errorf("expected .planning/ in commit, output:\n%s", string(out))
	}
}

func TestCommit_EmojiPrefixedMessage(t *testing.T) {
	dir, cleanup := initGitRepoForCommit(t)
	defer cleanup()

	// Verify that emoji-prefixed messages pass through correctly.
	readme := filepath.Join(dir, "README.md")
	os.WriteFile(readme, []byte("# updated\n"), 0644)

	err := Commit("✨ feat: emoji prefix test", nil)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	cmd := exec.Command("git", "log", "-1", "--format=%s")
	cmd.Dir = dir
	out, _ := cmd.Output()
	msg := strings.TrimSpace(string(out))
	if msg != "✨ feat: emoji prefix test" {
		t.Errorf("expected exact emoji message, got %q", msg)
	}
}

// ---- VerifyClean tests ----

func TestCommitWithOptions_VerifyClean_CleanTree(t *testing.T) {
	dir, cleanup := initGitRepoForCommit(t)
	defer cleanup()

	// Modify tracked file and commit with --verify-clean — tree should be clean after.
	readme := filepath.Join(dir, "README.md")
	os.WriteFile(readme, []byte("# clean\n"), 0644)

	err := CommitWithOptions("✨ feat: verify-clean passes", nil, CommitOptions{VerifyClean: true})
	if err != nil {
		t.Fatalf("CommitWithOptions with clean tree failed: %v", err)
	}
}

func TestCommitWithOptions_VerifyClean_UntrackedFile(t *testing.T) {
	dir, cleanup := initGitRepoForCommit(t)
	defer cleanup()

	// Modify tracked file (so there is something to commit)
	readme := filepath.Join(dir, "README.md")
	os.WriteFile(readme, []byte("# updated\n"), 0644)

	// Also create an untracked file that will be left behind
	untracked := filepath.Join(dir, "forgotten.txt")
	os.WriteFile(untracked, []byte("oops"), 0644)

	err := CommitWithOptions("✨ feat: left untracked behind", nil, CommitOptions{VerifyClean: true})
	if err == nil {
		t.Fatal("expected error when untracked file remains, got nil")
	}
	if !strings.Contains(err.Error(), "forgotten.txt") {
		t.Errorf("expected error to mention 'forgotten.txt', got: %v", err)
	}
	if !strings.Contains(err.Error(), "??") {
		t.Errorf("expected porcelain '??' marker in error, got: %v", err)
	}
}

func TestCommitWithOptions_VerifyClean_ModifiedUnstaged(t *testing.T) {
	dir, cleanup := initGitRepoForCommit(t)
	defer cleanup()

	// Create and commit a second file first
	secondFile := filepath.Join(dir, "second.txt")
	os.WriteFile(secondFile, []byte("v1"), 0644)
	cmd := exec.Command("git", "add", "second.txt")
	cmd.Dir = dir
	cmd.Run()
	cmd2 := exec.Command("git", "commit", "-m", "add second")
	cmd2.Dir = dir
	cmd2.Run()

	// Stage only README.md (via extraFiles) but leave second.txt modified unstaged.
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# updated\n"), 0644)
	os.WriteFile(secondFile, []byte("v2-unstaged"), 0644)

	// Use extraFiles to stage only README — second.txt stays dirty
	err := CommitWithOptions("✨ feat: modified unstaged remains", []string{"README.md"}, CommitOptions{VerifyClean: true})
	if err == nil {
		t.Fatal("expected error when modified-unstaged file remains, got nil")
	}
	if !strings.Contains(err.Error(), "second.txt") {
		t.Errorf("expected error to mention 'second.txt', got: %v", err)
	}
}

func TestCommitWithOptions_NoFlag_BehaviorUnchanged(t *testing.T) {
	dir, cleanup := initGitRepoForCommit(t)
	defer cleanup()

	// Modify tracked file
	readme := filepath.Join(dir, "README.md")
	os.WriteFile(readme, []byte("# no flag\n"), 0644)

	// Also create an untracked file — bare commit (no --verify-clean) should NOT fail
	untracked := filepath.Join(dir, "ignored-by-commit.txt")
	os.WriteFile(untracked, []byte("not committed"), 0644)

	err := Commit("✨ feat: bare commit ignores untracked", nil)
	if err != nil {
		t.Fatalf("bare Commit() should not fail with untracked file present, got: %v", err)
	}
}

func TestCommit_NoPintWhenNoVendor(t *testing.T) {
	dir, cleanup := initGitRepoForCommit(t)
	defer cleanup()

	// Stage a fake .php file — no vendor/bin/pint so it should be skipped.
	phpFile := filepath.Join(dir, "fake.php")
	os.WriteFile(phpFile, []byte("<?php echo 'hello'; ?>"), 0644)

	err := Commit("🐛 fix: php file without pint", []string{"fake.php"})
	if err != nil {
		t.Fatalf("Commit without pint failed: %v", err)
	}

	cmd := exec.Command("git", "log", "-1", "--format=%s")
	cmd.Dir = dir
	out, _ := cmd.Output()
	msg := strings.TrimSpace(string(out))
	if msg != "🐛 fix: php file without pint" {
		t.Errorf("expected commit message, got %q", msg)
	}
}

func TestCommit_ShowsHashAfterCommit(t *testing.T) {
	dir, cleanup := initGitRepoForCommit(t)
	defer cleanup()

	readme := filepath.Join(dir, "README.md")
	os.WriteFile(readme, []byte("# hash test\n"), 0644)

	// Capture stdout to verify hash output.
	r, w, _ := os.Pipe()
	orig := os.Stdout
	os.Stdout = w

	err := Commit("🧪 test: hash output", nil)
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	os.Stdout = orig

	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	output := buf.String()
	// Should contain ✅ and the message.
	if !strings.Contains(output, "✅") {
		t.Errorf("expected ✅ in output, got: %q", output)
	}
	if !strings.Contains(output, "hash output") {
		t.Errorf("expected commit message in output, got: %q", output)
	}
}

// ---- Parallel-session safety tests (B-0077) ----

// commitTreeFiles returns the list of files in the latest commit's tree.
func commitTreeFiles(t *testing.T, dir string) []string {
	t.Helper()
	cmd := exec.Command("git", "show", "--name-only", "--format=")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git show failed: %v", err)
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files
}

// TestCommit_ExtraFilesDoNotSweepPlanning verifies that when the caller supplies
// positional files, untracked .planning/ files from sibling sessions are NOT
// captured (B-0077 regression guard).
func TestCommit_ExtraFilesDoNotSweepPlanning(t *testing.T) {
	dir, cleanup := initGitRepoForCommit(t)
	defer cleanup()

	// Create a tracked file that we'll pass as extraFiles arg.
	trackedFile := filepath.Join(dir, "existing.go")
	os.WriteFile(trackedFile, []byte("package main\n"), 0644)
	// Stage + commit so it becomes tracked
	exec.Command("git", "-C", dir, "add", "existing.go").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "add existing.go").Run()
	// Modify it so it has a change to commit
	os.WriteFile(trackedFile, []byte("package main // updated\n"), 0644)

	// Simulate a parallel session creating an untracked .planning/ file
	planDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planDir, 0755)
	untrackedPlan := filepath.Join(planDir, "B-9999-untracked.md")
	os.WriteFile(untrackedPlan, []byte("---\nstatus: todo\n---\n# Parallel session plan\n"), 0644)

	// Commit only existing.go — the untracked plan file must NOT be captured
	err := Commit("🐛 fix: update existing.go only", []string{"existing.go"})
	if err != nil {
		t.Fatalf("Commit with extraFiles failed: %v", err)
	}

	// Verify commit tree contains existing.go but NOT .planning/B-9999-untracked.md
	files := commitTreeFiles(t, dir)
	for _, f := range files {
		if strings.Contains(f, "B-9999-untracked") {
			t.Errorf("untracked .planning/ file was captured in commit (B-0077 regression): %s", f)
		}
	}
	foundExisting := false
	for _, f := range files {
		if f == "existing.go" {
			foundExisting = true
		}
	}
	if !foundExisting {
		t.Errorf("expected existing.go in commit tree, got: %v", files)
	}

	// The untracked plan file must still be untracked after the commit.
	// git shows the parent dir as "?? .planning/" when the directory itself is
	// untracked (no tracked children yet), so accept either form.
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = dir
	statusOut, _ := statusCmd.Output()
	statusStr := string(statusOut)
	if !strings.Contains(statusStr, "B-9999-untracked") && !strings.Contains(statusStr, ".planning/") {
		t.Errorf("expected .planning/ (or B-9999-untracked.md) to remain untracked, status:\n%s", statusStr)
	}
}

// TestCommit_ScopeStagedFlag_OnlyCommitsIndex verifies that --scope-staged (ScopeStaged: true)
// commits only what is already staged, ignoring unstaged modified files and untracked .planning/ files.
func TestCommit_ScopeStagedFlag_OnlyCommitsIndex(t *testing.T) {
	dir, cleanup := initGitRepoForCommit(t)
	defer cleanup()

	// Create fileA.go (will be staged) and fileB.go (will be modified but unstaged)
	fileA := filepath.Join(dir, "fileA.go")
	fileB := filepath.Join(dir, "fileB.go")
	os.WriteFile(fileA, []byte("package main\n"), 0644)
	os.WriteFile(fileB, []byte("package main\n"), 0644)
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "add fileA and fileB").Run()

	// Stage only fileA.go
	os.WriteFile(fileA, []byte("package main // v2\n"), 0644)
	exec.Command("git", "-C", dir, "add", "fileA.go").Run()

	// Modify fileB but do NOT stage it
	os.WriteFile(fileB, []byte("package main // unstaged\n"), 0644)

	// Create untracked .planning/ file
	planDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planDir, 0755)
	os.WriteFile(filepath.Join(planDir, "X.md"), []byte("# untracked plan\n"), 0644)

	// Commit with ScopeStaged — only fileA.go (pre-staged) should land in the commit
	err := CommitWithOptions("🔧 build: scope-staged commit", nil, CommitOptions{ScopeStaged: true})
	if err != nil {
		t.Fatalf("CommitWithOptions ScopeStaged failed: %v", err)
	}

	files := commitTreeFiles(t, dir)

	// fileA.go must be in the commit
	foundA := false
	for _, f := range files {
		if f == "fileA.go" {
			foundA = true
		}
	}
	if !foundA {
		t.Errorf("expected fileA.go in commit tree, got: %v", files)
	}

	// fileB.go and .planning/X.md must NOT be in the commit
	for _, f := range files {
		if f == "fileB.go" {
			t.Errorf("fileB.go (unstaged) should not be in commit")
		}
		if strings.Contains(f, "X.md") {
			t.Errorf(".planning/X.md (untracked) should not be in commit")
		}
	}
}

// TestCommit_DefaultBehaviorUnchanged_NoExtraFiles is a regression guard: when called
// with no positional args and no special flags, git add -u + .planning/ sweep still runs.
func TestCommit_DefaultBehaviorUnchanged_NoExtraFiles(t *testing.T) {
	dir, cleanup := initGitRepoForCommit(t)
	defer cleanup()

	// Modify the tracked README (git add -u will pick it up)
	readme := filepath.Join(dir, "README.md")
	os.WriteFile(readme, []byte("# default behavior\n"), 0644)

	// Add a new .planning/ file (should be swept in)
	planDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planDir, 0755)
	planFile := filepath.Join(planDir, "P-0001-default-todo.md")
	os.WriteFile(planFile, []byte("---\nstatus: todo\n---\n# Default plan\n"), 0644)

	err := Commit("📋 plan: default behavior unchanged", nil)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	files := commitTreeFiles(t, dir)

	foundReadme := false
	foundPlan := false
	for _, f := range files {
		if f == "README.md" {
			foundReadme = true
		}
		if strings.Contains(f, "P-0001-default-todo.md") {
			foundPlan = true
		}
	}
	if !foundReadme {
		t.Errorf("expected README.md in commit (git add -u should have picked it up), got: %v", files)
	}
	if !foundPlan {
		t.Errorf("expected .planning/P-0001-default-todo.md in commit (.planning/ sweep should have picked it up), got: %v", files)
	}
}

// TestCommit_ExtraFilesPlusScopeStaged_PreferScope verifies that when both positional
// extraFiles and --scope-staged are set, ScopeStaged wins: only the pre-staged index
// is committed and positional args are ignored.
func TestCommit_ExtraFilesPlusScopeStaged_PreferScope(t *testing.T) {
	dir, cleanup := initGitRepoForCommit(t)
	defer cleanup()

	// Create two tracked files
	fileC := filepath.Join(dir, "fileC.go")
	fileD := filepath.Join(dir, "fileD.go")
	os.WriteFile(fileC, []byte("package main\n"), 0644)
	os.WriteFile(fileD, []byte("package main\n"), 0644)
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "add fileC and fileD").Run()

	// Modify both files
	os.WriteFile(fileC, []byte("package main // staged\n"), 0644)
	os.WriteFile(fileD, []byte("package main // NOT staged\n"), 0644)

	// Stage only fileC
	exec.Command("git", "-C", dir, "add", "fileC.go").Run()

	// Call with both extraFiles (fileD.go) and ScopeStaged — ScopeStaged wins
	err := CommitWithOptions("🔧 build: scope-staged wins over extra-files", []string{"fileD.go"}, CommitOptions{ScopeStaged: true})
	if err != nil {
		t.Fatalf("CommitWithOptions failed: %v", err)
	}

	files := commitTreeFiles(t, dir)

	// fileC.go must be in the commit (it was pre-staged)
	foundC := false
	for _, f := range files {
		if f == "fileC.go" {
			foundC = true
		}
	}
	if !foundC {
		t.Errorf("expected fileC.go in commit (was pre-staged), got: %v", files)
	}

	// fileD.go must NOT be in the commit (ScopeStaged skips extraFiles staging)
	for _, f := range files {
		if f == "fileD.go" {
			t.Errorf("fileD.go should NOT be in commit when ScopeStaged is true (was not pre-staged)")
		}
	}
}

// writeFakePint installs an executable vendor/bin/pint stub in dir that records
// its argv (one path per line) into pint-args.txt and rewrites each file it is
// given to the literal content "formatted\n" — so tests can assert both which
// paths reached pint and that pint-modified files are re-staged into the commit.
func writeFakePint(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "vendor", "bin"), 0o755); err != nil {
		t.Fatalf("mkdir vendor/bin: %v", err)
	}
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" >> pint-args.txt\n" +
		"for f in \"$@\"; do echo formatted > \"$f\"; done\n"
	if err := os.WriteFile(filepath.Join(dir, "vendor", "bin", "pint"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake pint: %v", err)
	}
}

// TestCommit_PintGlyphAndSpacePaths_ExcludesBlade pins the P-0183 G4 / recap F3
// fixes: with git's DEFAULT core.quotepath=true (set repo-locally so the test
// fails against pre-fix code even when a machine's global config disables it),
// a ⚡-glyph Blade path and a space-containing .php path are staged together.
// Pint must receive the .php file as a clean argv element (no C-quoting), must
// NEVER see the .blade.php, and the pint-formatted content must be re-staged
// into the commit.
func TestCommit_PintGlyphAndSpacePaths_ExcludesBlade(t *testing.T) {
	dir, cleanup := initGitRepoForCommit(t)
	defer cleanup()

	// Force git's default quoting behavior regardless of the machine's config.
	cfg := exec.Command("git", "config", "core.quotepath", "true")
	cfg.Dir = dir
	if out, err := cfg.CombinedOutput(); err != nil {
		t.Fatalf("git config: %v\n%s", err, out)
	}

	writeFakePint(t, dir)

	bladeRel := filepath.Join("resources", "views", "⚡index.blade.php")
	phpRel := filepath.Join("src", "App with space.php")
	for _, rel := range []string{bladeRel, phpRel} {
		abs := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(abs, []byte("<?php echo 'original'; ?>\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	if err := Commit("🐛 fix: glyph and space paths", []string{bladeRel, phpRel}); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	argsData, err := os.ReadFile(filepath.Join(dir, "pint-args.txt"))
	if err != nil {
		t.Fatalf("pint was never invoked (pint-args.txt missing): %v", err)
	}
	args := strings.TrimSpace(string(argsData))
	if strings.Contains(args, ".blade.php") {
		t.Errorf("a .blade.php path reached pint:\n%s", args)
	}
	if !strings.Contains(args, phpRel) {
		t.Errorf("pint did not receive the exact space-containing path %q; argv:\n%s", phpRel, args)
	}
	if strings.Contains(args, "\"") || strings.Contains(args, "\\342") {
		t.Errorf("pint received a C-quoted path (core.quotepath leak); argv:\n%s", args)
	}

	// The pint-formatted .php content must be re-staged and land in the commit.
	show := exec.Command("git", "show", "HEAD:"+filepath.ToSlash(phpRel))
	show.Dir = dir
	committed, err := show.Output()
	if err != nil {
		t.Fatalf("git show: %v", err)
	}
	if strings.TrimSpace(string(committed)) != "formatted" {
		t.Errorf("pint-formatted content was not re-staged; committed content: %q", committed)
	}
	// The Blade file must land in the commit UNTOUCHED by pint.
	showBlade := exec.Command("git", "show", "HEAD:"+filepath.ToSlash(bladeRel))
	showBlade.Dir = dir
	bladeCommitted, err := showBlade.Output()
	if err != nil {
		t.Fatalf("git show blade: %v", err)
	}
	if !strings.Contains(string(bladeCommitted), "original") {
		t.Errorf("blade file content changed; got %q", bladeCommitted)
	}
}

// TestCommit_OnlyBladeStaged_PintNotInvoked: when the staged PHP set is
// Blade-only, pint must not be invoked at all and the commit still succeeds.
func TestCommit_OnlyBladeStaged_PintNotInvoked(t *testing.T) {
	dir, cleanup := initGitRepoForCommit(t)
	defer cleanup()

	writeFakePint(t, dir)

	bladeRel := filepath.Join("resources", "views", "home.blade.php")
	abs := filepath.Join(dir, bladeRel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte("<div>hi</div>\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := Commit("💄 style: blade only", []string{bladeRel}); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "pint-args.txt")); !os.IsNotExist(err) {
		t.Error("pint was invoked for a Blade-only staging set")
	}

	log := exec.Command("git", "log", "-1", "--format=%s")
	log.Dir = dir
	out, _ := log.Output()
	if strings.TrimSpace(string(out)) != "💄 style: blade only" {
		t.Errorf("commit did not land; last subject: %q", out)
	}
}
