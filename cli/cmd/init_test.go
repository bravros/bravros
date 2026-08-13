package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// resetInitGlobals resets the init command globals and restores them afterwards.
// Call at the start of every TestInit_* test.
func resetInitGlobals(t *testing.T) {
	t.Helper()
	origStack := initStack
	origSkipHooks := initSkipHooks
	origExitFn := initExitFn

	t.Cleanup(func() {
		initStack = origStack
		initSkipHooks = origSkipHooks
		initExitFn = origExitFn
	})

	initStack = ""
	initSkipHooks = false
}

// captureInitStderr captures stderr writes made by initCmd.Run via fmt.Fprintf(os.Stderr).
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

// captureInitStdout captures stdout while fn runs.
func captureInitStdout(t *testing.T, fn func()) string {
	t.Helper()
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	r.Close()
	return buf.String()
}

// initGitRepo turns dir into a git repo, skipping the test when git is unavailable.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := exec.Command("git", "init", dir).Run(); err != nil {
		t.Skipf("git not available — skipping: %v", err)
	}
}

// TestInit_RepoLocalOnly is the regression test for the critical bug: `bravros init`
// deployed into the user's real ~/.claude/. It must now write only inside the repo —
// .bravros/config.json, both hooks, core.hooksPath — and nothing anywhere else.
func TestInit_RepoLocalOnly(t *testing.T) {
	resetInitGlobals(t)

	workDir := t.TempDir()
	initGitRepo(t, workDir)
	chdirTo(t, workDir)

	// A throwaway HOME: anything init writes globally lands here and fails the test.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("BRAVROS_CONFIG_DIR", filepath.Join(fakeHome, ".claude"))

	exitCode := -1
	initExitFn = func(code int) { exitCode = code }

	stdout := captureInitStdout(t, func() {
		initCmd.Run(initCmd, []string{})
	})

	if exitCode != -1 {
		t.Fatalf("expected no exit, got exit(%d)", exitCode)
	}

	// .bravros/config.json exists and is valid JSON carrying the schema key.
	data, err := os.ReadFile(filepath.Join(workDir, ".bravros", "config.json"))
	if err != nil {
		t.Fatalf(".bravros/config.json not created: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf(".bravros/config.json is not valid JSON: %v", err)
	}
	if _, ok := cfg["$schema"]; !ok {
		t.Errorf(".bravros/config.json has no $schema key: %s", data)
	}

	// Both hooks installed.
	for _, hook := range []string{"commit-msg", "pre-push"} {
		if _, err := os.Stat(filepath.Join(workDir, ".bravros", "hooks", hook)); err != nil {
			t.Errorf(".bravros/hooks/%s not installed: %v", hook, err)
		}
	}

	// core.hooksPath points at the repo-local hooks dir.
	cmd := exec.Command("git", "config", "--local", "core.hooksPath")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("reading core.hooksPath failed: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != ".bravros/hooks" {
		t.Errorf("core.hooksPath = %q, want .bravros/hooks", got)
	}

	// No undocumented side effects.
	if _, err := os.Stat(filepath.Join(workDir, ".github")); !os.IsNotExist(err) {
		t.Error("init must not create .github/")
	}
	cmd = exec.Command("git", "branch", "--list", "homolog")
	cmd.Dir = workDir
	if out, _ := cmd.Output(); len(out) != 0 {
		t.Error("init must not create the homolog branch")
	}

	// Nothing outside the repo.
	entries, err := os.ReadDir(fakeHome)
	if err != nil {
		t.Fatalf("reading fake HOME failed: %v", err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("init wrote outside the repo — HOME contains %v", names)
	}

	// The printed next step must reference a verb that exists.
	if !strings.Contains(stdout, "bravros commit") {
		t.Errorf("expected a real next-step verb in stdout, got: %s", stdout)
	}
	if strings.Contains(stdout, "bravros mcp") || strings.Contains(stdout, "bravros doctor") {
		t.Errorf("stdout advertises a verb this CLI does not have: %s", stdout)
	}
}

// TestInit_NoDeployFlags asserts the global-deploy flags are gone from init.
// Deploying the agent runtime belongs to `bravros install` / `bravros deploy`.
func TestInit_NoDeployFlags(t *testing.T) {
	for _, name := range []string{"portable-repo", "deploy-mode", "skip-secrets", "skip-mcp", "force"} {
		if f := initCmd.Flags().Lookup(name); f != nil {
			t.Errorf("init still exposes --%s — init must be repo-local only", name)
		}
	}
	for _, name := range []string{"stack", "skip-hooks"} {
		if f := initCmd.Flags().Lookup(name); f == nil {
			t.Errorf("init lost its --%s flag", name)
		}
	}
	// Kept as accepted no-ops so existing skill callers do not break.
	for _, name := range []string{"skip-workflows", "skip-staging-branch"} {
		f := initCmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("--%s must still be accepted (as a no-op) for backwards compatibility", name)
			continue
		}
		if f.Deprecated == "" {
			t.Errorf("--%s should be marked deprecated", name)
		}
	}
}

// TestInit_SkipHooks verifies --skip-hooks leaves .bravros/hooks/ alone while still
// writing the config.
func TestInit_SkipHooks(t *testing.T) {
	resetInitGlobals(t)

	workDir := t.TempDir()
	initGitRepo(t, workDir)
	chdirTo(t, workDir)
	t.Setenv("HOME", t.TempDir())

	initSkipHooks = true

	exitCode := -1
	initExitFn = func(code int) { exitCode = code }

	stderr := captureInitStderr(t, func() {
		captureInitStdout(t, func() {
			initCmd.Run(initCmd, []string{})
		})
	})

	if exitCode != -1 {
		t.Fatalf("expected no exit, got exit(%d); stderr: %s", exitCode, stderr)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".bravros", "hooks")); !os.IsNotExist(err) {
		t.Error(".bravros/hooks must not exist with --skip-hooks")
	}
	if _, err := os.Stat(filepath.Join(workDir, ".bravros", "config.json")); err != nil {
		t.Errorf(".bravros/config.json should still be written with --skip-hooks: %v", err)
	}
}

// TestInit_PositionalPathTargetsThatRepo verifies `bravros init <path>` initializes
// the given path rather than the working directory.
func TestInit_PositionalPathTargetsThatRepo(t *testing.T) {
	resetInitGlobals(t)

	cwd := t.TempDir()
	target := t.TempDir()
	initGitRepo(t, target)
	chdirTo(t, cwd)
	t.Setenv("HOME", t.TempDir())

	exitCode := -1
	initExitFn = func(code int) { exitCode = code }

	captureInitStdout(t, func() {
		initCmd.Run(initCmd, []string{target})
	})

	if exitCode != -1 {
		t.Fatalf("expected no exit, got exit(%d)", exitCode)
	}
	if _, err := os.Stat(filepath.Join(target, ".bravros", "config.json")); err != nil {
		t.Errorf("target repo not initialized: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".bravros")); !os.IsNotExist(err) {
		t.Error("cwd must be untouched when a positional path is given")
	}
}
