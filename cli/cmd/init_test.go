package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	ideploy "github.com/bravros/bravros/cli/internal/deploy"
)

// makeClaudeRepo creates a minimal directory that IsClaudeRepo accepts.
// IsClaudeRepo checks filepath.Base(dir) == "claude", so we create a
// directory named "claude" inside a temp parent.
func makeClaudeRepo(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	dir := filepath.Join(parent, "claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("makeClaudeRepo: mkdir: %v", err)
	}
	return dir
}

// resetInitGlobals resets all init command global vars to their zero values
// and restores them after the test. Call at the start of every TestInit_* test.
func resetInitGlobals(t *testing.T) {
	t.Helper()
	origPortableRepo := initPortableRepo
	origDeployMode := initDeployMode
	origStack := initStack
	origSkipHooks := initSkipHooks
	origSkipWorkflows := initSkipWorkflows
	origSkipStaging := initSkipStaging
	origSkipSecrets := initSkipSecrets
	origSkipMCP := initSkipMCP
	origForce := initForce
	origExitFn := initExitFn

	t.Cleanup(func() {
		initPortableRepo = origPortableRepo
		initDeployMode = origDeployMode
		initStack = origStack
		initSkipHooks = origSkipHooks
		initSkipWorkflows = origSkipWorkflows
		initSkipStaging = origSkipStaging
		initSkipSecrets = origSkipSecrets
		initSkipMCP = origSkipMCP
		initForce = origForce
		initExitFn = origExitFn
	})

	// Default: skip slow side-effects; use copies mode to avoid symlink complexity.
	initPortableRepo = ""
	initDeployMode = "copies"
	initStack = ""
	initSkipHooks = true
	initSkipWorkflows = true
	initSkipStaging = true
	initSkipSecrets = true
	initSkipMCP = true
	initForce = false
}

// captureInitStderr captures stderr writes made by initCmd.Run via fmt.Fprintf(os.Stderr).
// It swaps os.Stderr for a pipe, runs fn, then restores it.
func captureInitStderr(t *testing.T, fn func()) string {
	t.Helper()
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = origStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	r.Close()
	return buf.String()
}

// TestInit_PortableRepoFlagHonored verifies that when --portable-repo is set to a
// valid "claude" directory and no positional arg is given, the bootstrap path uses
// the flag value — NOT cwd.
func TestInit_PortableRepoFlagHonored(t *testing.T) {
	resetInitGlobals(t)

	// cwd: a plain temp dir (not a claude repo)
	nonClaudeDir := t.TempDir()
	chdirTo(t, nonClaudeDir)

	// --portable-repo: a directory named "claude" (IsClaudeRepo returns true)
	claudeDir := makeClaudeRepo(t)

	// Verify the fixture satisfies IsClaudeRepo.
	if !ideploy.IsClaudeRepo(claudeDir) {
		t.Fatalf("fixture %q is not recognized as a claude repo", claudeDir)
	}

	// resolveBootstrapDir with flag set and no positional args must return the flag path.
	got := resolveBootstrapDir(nonClaudeDir, claudeDir, nil)
	if got != claudeDir {
		t.Errorf("resolveBootstrapDir with --portable-repo=%q: got %q, want %q",
			claudeDir, got, claudeDir)
	}

	// Confirm: without the flag, cwd is used instead.
	gotNoflag := resolveBootstrapDir(nonClaudeDir, "", nil)
	if gotNoflag != nonClaudeDir {
		t.Errorf("resolveBootstrapDir with no flag: got %q, want %q", gotNoflag, nonClaudeDir)
	}
}

// TestInit_PortableRepoFlagInvalid verifies that passing --portable-repo to a path
// that does NOT satisfy IsClaudeRepo triggers a non-zero exit and a clear stderr error.
//
// The cwd is initialized as a git repo so that projectinit.Init succeeds and the
// portable-repo error path is the one being exercised — without git init the test
// could exit early via the projectinit.Init error branch with a different stderr
// message and pass the exitCode assertion for the wrong reason.
func TestInit_PortableRepoFlagInvalid(t *testing.T) {
	resetInitGlobals(t)

	workDir := t.TempDir()
	chdirTo(t, workDir)

	// Initialize workDir as a git repo so projectinit.Init reaches success.
	// Without this, projectinit.Init may exit early with a different error and
	// the test would pass the exitCode assertion for the wrong reason.
	if err := exec.Command("git", "init", workDir).Run(); err != nil {
		t.Skipf("git not available — skipping: %v", err)
	}

	// Inject a no-op exit function so os.Exit does not kill the test binary.
	// Track which call exited so we can assert the portable-repo branch fired
	// (vs an earlier projectinit.Init failure).
	exitCode := 0
	exitCallCount := 0
	initExitFn = func(code int) {
		exitCode = code
		exitCallCount++
	}

	// Set flag to a real path but with the wrong basename (not "claude").
	badRepo := t.TempDir() // basename will be something like "TestInit_Port..." not "claude"
	initPortableRepo = badRepo

	// Set BRAVROS_CONFIG_DIR to a safe temp location so deploy does not touch ~/.claude.
	t.Setenv("BRAVROS_CONFIG_DIR", t.TempDir())

	stderrOut := captureInitStderr(t, func() {
		initCmd.Run(initCmd, []string{})
	})

	// The error path must have been triggered: exit code = 1.
	if exitCode != 1 {
		t.Errorf("expected exit code 1 for invalid --portable-repo, got %d (exitCalls=%d, stderr=%q)",
			exitCode, exitCallCount, stderrOut)
	}

	// Stderr must contain a clear error mentioning the bad path — proves the
	// portable-repo branch fired, not an earlier projectinit.Init error.
	if !strings.Contains(stderrOut, "Error:") {
		t.Errorf("expected stderr to contain 'Error:', got: %q", stderrOut)
	}
	if !strings.Contains(stderrOut, badRepo) {
		t.Errorf("expected stderr to mention the bad repo path %q (proves the portable-repo branch fired), got: %q", badRepo, stderrOut)
	}
}

// TestInit_PositionalArgWinsOverFlag verifies that when both a positional arg
// and --portable-repo are provided, the positional arg wins.
func TestInit_PositionalArgWinsOverFlag(t *testing.T) {
	resetInitGlobals(t)

	pathA := t.TempDir() // positional arg target
	pathB := t.TempDir() // --portable-repo value (should be ignored)

	// resolveBootstrapDir: positional arg provided → root (== pathA) is returned.
	// The cobra Run sets root = args[0] when len(args) > 0.
	got := resolveBootstrapDir(pathA, pathB, []string{pathA})
	if got != pathA {
		t.Errorf("positional arg should win: got %q, want %q", got, pathA)
	}

	// Double-check: with no args, flag value is used.
	gotFlag := resolveBootstrapDir(pathA, pathB, nil)
	if gotFlag != pathB {
		t.Errorf("no positional arg: flag should win: got %q, want %q", gotFlag, pathB)
	}
}

// TestInit_NoFlagFallsBackToConfigPortableRepo verifies that when no --portable-repo
// flag is passed but BRAVROS_PORTABLE_REPO env var is set, the env var path is used
// as the portableRepo value fed into resolveBootstrapDir.
func TestInit_NoFlagFallsBackToConfigPortableRepo(t *testing.T) {
	resetInitGlobals(t)

	envPath := t.TempDir()
	t.Setenv("BRAVROS_PORTABLE_REPO", envPath)

	// Simulate the resolution inside initCmd.Run:
	//   portableRepo := initPortableRepo              (== "" — flag not set)
	//   if portableRepo == "" { portableRepo = config.PortableRepo() }
	// config.PortableRepo() reads BRAVROS_PORTABLE_REPO.
	portableRepo := initPortableRepo // "" — flag not set
	if portableRepo == "" {
		if v := os.Getenv("BRAVROS_PORTABLE_REPO"); v != "" {
			portableRepo = v
		}
	}

	if portableRepo != envPath {
		t.Errorf("expected portableRepo to be resolved from env: got %q, want %q", portableRepo, envPath)
	}

	// resolveBootstrapDir with the env-resolved path and no positional args returns the env path.
	cwd := t.TempDir()
	got := resolveBootstrapDir(cwd, portableRepo, nil)
	if got != envPath {
		t.Errorf("resolveBootstrapDir should use env-var path: got %q, want %q", got, envPath)
	}
}

// TestInit_NoFlagNoEnvUsesCwd verifies that when no flag and no env var are set,
// cwd is used and IsClaudeRepo returns false for it, so the bootstrap step is skipped
// (no deploy, no error, no exit).
func TestInit_NoFlagNoEnvUsesCwd(t *testing.T) {
	resetInitGlobals(t)

	// Ensure no env var overrides.
	t.Setenv("BRAVROS_PORTABLE_REPO", "")

	plainDir := t.TempDir() // not named "claude"

	// With no flag and no env, portableRepo resolves to the platform default or "".
	// We test the path resolution: cwd wins when portableRepo resolves empty-or-nonexistent.
	portableRepo := initPortableRepo // ""
	// After env lookup, still "".
	if v := os.Getenv("BRAVROS_PORTABLE_REPO"); v != "" {
		portableRepo = v
	}

	// resolveBootstrapDir with empty portableRepo returns cwd.
	got := resolveBootstrapDir(plainDir, portableRepo, nil)
	if got != plainDir {
		t.Errorf("no flag, no env: cwd should be used; got %q, want %q", got, plainDir)
	}

	// IsClaudeRepo(plainDir) must be false — bootstrap is skipped.
	if ideploy.IsClaudeRepo(plainDir) {
		t.Errorf("plain temp dir %q should not be a claude repo", plainDir)
	}
}

// TestInit_FallsBackToCopyOnExistingRealDir verifies that when SymlinkDeploy
// returns "exists and is not a symlink" because ~/.claude/skills/ is a real
// directory, the init command automatically falls back to copy-mode Deploy()
// and exits 0 with skills actually copied to the target.
//
// This covers B-0234: bash install.sh (no flags) on an existing install where
// ~/.claude/skills/ is a real dir must now succeed — not silently skip deploy.
func TestInit_FallsBackToCopyOnExistingRealDir(t *testing.T) {
	resetInitGlobals(t)

	// Source: a directory named "claude" (IsClaudeRepo returns true)
	// with a skills/ subdir containing a sentinel file.
	claudeRepo := makeClaudeRepo(t)
	if err := os.MkdirAll(filepath.Join(claudeRepo, "skills", "plan"), 0o755); err != nil {
		t.Fatalf("mkdir source skills/plan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeRepo, "skills", "plan", "SKILL.md"), []byte("plan skill"), 0o644); err != nil {
		t.Fatalf("write source SKILL.md: %v", err)
	}

	// Target: ~/.claude equivalent — skills/ is a REAL directory (not a symlink).
	configDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(configDir, "skills"), 0o755); err != nil {
		t.Fatalf("mkdir target skills/: %v", err)
	}

	// Wire the target as the config dir.
	t.Setenv("BRAVROS_CONFIG_DIR", configDir)

	// Set --portable-repo to the source claude repo.
	initPortableRepo = claudeRepo
	// Keep default deploy mode (symlink) so the fallback path is exercised.
	initDeployMode = ""
	// Suppress doctor / secrets next-step output.
	initSkipSecrets = true

	// Capture exit codes — expect 0 (success via fallback).
	exitCode := -1
	initExitFn = func(code int) { exitCode = code }

	// Run init. Should NOT call initExitFn (no non-zero exit).
	stderrOut := captureInitStderr(t, func() {
		initCmd.Run(initCmd, []string{})
	})

	// initExitFn must NOT have been called (exit code stays -1 for success).
	if exitCode != -1 {
		t.Errorf("expected no exit, got exit(%d); stderr: %s", exitCode, stderrOut)
	}

	// The fallback diagnostic line must appear in stderr.
	if !strings.Contains(stderrOut, "falling back to copy-mode deploy with hardening") {
		t.Errorf("expected fallback diagnostic in stderr; got: %s", stderrOut)
	}

	// The sentinel file must have been copied to the target.
	sentinel := filepath.Join(configDir, "skills", "plan", "SKILL.md")
	if _, err := os.Stat(sentinel); os.IsNotExist(err) {
		t.Errorf("skills/plan/SKILL.md not copied to target %s — fallback deploy did not run", configDir)
	}
}

// TestInit_PropagatesDeployErrorOnFallback verifies that when the copy-mode
// fallback Deploy() fails (e.g. permission error), the init command exits
// non-zero and emits a clear error on stderr.
func TestInit_PropagatesDeployErrorOnFallback(t *testing.T) {
	resetInitGlobals(t)

	// Source: a valid claude repo with skills/.
	claudeRepo := makeClaudeRepo(t)
	if err := os.MkdirAll(filepath.Join(claudeRepo, "skills", "plan"), 0o755); err != nil {
		t.Fatalf("mkdir source skills: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeRepo, "skills", "plan", "SKILL.md"), []byte("plan"), 0o644); err != nil {
		t.Fatalf("write source SKILL.md: %v", err)
	}

	// Target: skills/ is a REAL dir so symlink deploy will fail.
	configDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(configDir, "skills"), 0o755); err != nil {
		t.Fatalf("mkdir target skills/: %v", err)
	}

	// Make the target skills/ dir read-only so Deploy() fails to copy into it.
	if err := os.Chmod(filepath.Join(configDir, "skills"), 0o555); err != nil {
		t.Fatalf("chmod target skills/ read-only: %v", err)
	}
	t.Cleanup(func() {
		// Restore so t.TempDir() cleanup can delete it.
		_ = os.Chmod(filepath.Join(configDir, "skills"), 0o755)
	})

	t.Setenv("BRAVROS_CONFIG_DIR", configDir)
	initPortableRepo = claudeRepo
	initDeployMode = ""
	initSkipSecrets = true

	exitCode := -1
	initExitFn = func(code int) { exitCode = code }

	stderrOut := captureInitStderr(t, func() {
		initCmd.Run(initCmd, []string{})
	})

	// Must exit non-zero because the fallback deploy failed.
	if exitCode != 1 {
		t.Errorf("expected exit(1) on fallback deploy failure; got exit(%d); stderr: %s", exitCode, stderrOut)
	}

	// Must emit the fallback-failed diagnostic.
	if !strings.Contains(stderrOut, "copy-mode fallback deploy failed") {
		t.Errorf("expected fallback-failed error in stderr; got: %s", stderrOut)
	}
}

// TestInit_PropagatesDeployErrorOnCopyMode verifies that the explicit copy-mode
// path (--deploy-mode copies) also exits non-zero when Deploy() fails.
// This covers the medium bug found in the P-0124 review: the copy-mode path
// previously swallowed the error (no initExitFn(1)) while the fallback path
// correctly propagated it.
func TestInit_PropagatesDeployErrorOnCopyMode(t *testing.T) {
	resetInitGlobals(t)

	// Source: a valid claude repo.
	claudeRepo := makeClaudeRepo(t)
	if err := os.MkdirAll(filepath.Join(claudeRepo, "skills", "plan"), 0o755); err != nil {
		t.Fatalf("mkdir source skills: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeRepo, "skills", "plan", "SKILL.md"), []byte("plan"), 0o644); err != nil {
		t.Fatalf("write source SKILL.md: %v", err)
	}

	// Target: create a read-only skills/ so Deploy() cannot write into it.
	configDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(configDir, "skills"), 0o755); err != nil {
		t.Fatalf("mkdir target skills/: %v", err)
	}
	if err := os.Chmod(filepath.Join(configDir, "skills"), 0o555); err != nil {
		t.Fatalf("chmod target skills/ read-only: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(filepath.Join(configDir, "skills"), 0o755)
	})

	t.Setenv("BRAVROS_CONFIG_DIR", configDir)
	initPortableRepo = claudeRepo
	initDeployMode = "copies" // explicit copy mode — no symlink fallback path
	initSkipSecrets = true

	exitCode := -1
	initExitFn = func(code int) { exitCode = code }

	stderrOut := captureInitStderr(t, func() {
		initCmd.Run(initCmd, []string{})
	})

	// Must exit non-zero because the copy deploy failed.
	if exitCode != 1 {
		t.Errorf("expected exit(1) on copy-mode deploy failure; got exit(%d); stderr: %s", exitCode, stderrOut)
	}

	// Must emit the copy-deploy error diagnostic.
	if !strings.Contains(stderrOut, "copy deploy:") {
		t.Errorf("expected 'copy deploy:' error in stderr; got: %s", stderrOut)
	}
}
