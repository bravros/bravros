package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// canonicalHookPath resolves the source-repo canonical commit-msg hook
// (templates/.githooks/commit-msg) relative to this test's package directory.
// Package dir is <repo>/cli/internal/hooks, so the repo root is three levels up.
func canonicalHookPath(t *testing.T) (hookPath, repoRoot string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	p := filepath.Join(root, "templates", ".githooks", "commit-msg")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("canonical hook missing at %s: %v", p, err)
	}
	return p, root
}

// runHook execs the bash commit-msg hook against a message file containing msg.
// It runs with cwd=repoRoot so the hook's `git rev-parse --show-toplevel`
// lookup finds templates/commit-types.txt and loads the valid-type patterns.
func runHook(t *testing.T, msg string) (exitCode int, output string) {
	t.Helper()
	hookPath, repoRoot := canonicalHookPath(t)

	msgFile := filepath.Join(t.TempDir(), "COMMIT_EDITMSG")
	if err := os.WriteFile(msgFile, []byte(msg), 0644); err != nil {
		t.Fatalf("write msg file: %v", err)
	}

	cmd := exec.Command("bash", hookPath, msgFile)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), string(out)
		}
		t.Fatalf("exec hook: %v", err)
	}
	return 0, string(out)
}

// TestCommitMsgHook_LongSubjectWarnsNotFails is the B-0330 regression guard:
// a valid subject over 72 chars must pass (exit 0) with a WARNING, not exit 1.
func TestCommitMsgHook_LongSubjectWarnsNotFails(t *testing.T) {
	// "🐛 fix: " prefix + filler → well over 72 characters.
	long := "🐛 fix: " + strings.Repeat("a", 80)
	code, out := runHook(t, long)

	if code != 0 {
		t.Fatalf("long subject should no longer block (B-0330) — got exit %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "WARNING") {
		t.Errorf("expected a length WARNING for an over-72-char subject, got:\n%s", out)
	}
	if strings.Contains(out, "ERROR") {
		t.Errorf("over-72-char subject must not emit an ERROR, got:\n%s", out)
	}
}

// TestCommitMsgHook_MidSubjectWarns covers the 50<len≤72 band — still a warning, exit 0.
func TestCommitMsgHook_MidSubjectWarns(t *testing.T) {
	mid := "🐛 fix: " + strings.Repeat("a", 55) // ~62 chars, in the warn band
	code, out := runHook(t, mid)

	if code != 0 {
		t.Fatalf("mid-length subject should pass — got exit %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "WARNING") {
		t.Errorf("expected a length WARNING in the 50–72 band, got:\n%s", out)
	}
}

// TestCommitMsgHook_ShortSubjectClean ensures a normal short subject still passes silently.
func TestCommitMsgHook_ShortSubjectClean(t *testing.T) {
	code, out := runHook(t, "🐛 fix: resolve null pointer in user lookup")

	if code != 0 {
		t.Fatalf("short valid subject should pass — got exit %d\noutput:\n%s", code, out)
	}
	if strings.Contains(out, "WARNING") || strings.Contains(out, "ERROR") {
		t.Errorf("short valid subject should produce no warnings/errors, got:\n%s", out)
	}
}
