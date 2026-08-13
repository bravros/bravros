package cmd

// LIFT: from bravros/private/cli/cmd/testutil_test.go (2026-04-18 DRY pass)

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

// ── cobra test helpers ────────────────────────────────────────────────────────

// executeCommand runs a cobra command with the given args and returns
// stdout, stderr, and any error. It does NOT call os.Exit.
func executeCommand(root *cobra.Command, args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	_, err := root.ExecuteC()
	return stdout.String(), stderr.String(), err
}

// ── git repo helpers ──────────────────────────────────────────────────────────

// initTestGitRepo creates a temporary git repo with an initial commit.
// The caller's working directory is NOT changed; the repo path is returned.
func initTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustGitRun(t, dir, "git", "init", "-b", "main")
	mustGitRun(t, dir, "git", "config", "user.email", "test@test.com")
	mustGitRun(t, dir, "git", "config", "user.name", "Test")
	writeTestFile(t, dir, "README.md", "# test repo")
	mustGitRun(t, dir, "git", "add", ".")
	mustGitRun(t, dir, "git", "commit", "-m", "initial commit")
	return dir
}

// mustGitRun runs a git (or other) command in dir, failing the test on error.
func mustGitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("command %v in %s failed: %v\nstderr: %s", args, dir, err, stderr.String())
	}
	return stdout.String()
}

// writeTestFile writes content to path inside dir, creating parent directories.
func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", p, err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

// chdirTo changes the working directory to dir and restores it when the test ends.
func chdirTo(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Logf("warning: failed to restore cwd to %s: %v", orig, err)
		}
	})
}

// ── stdout capture ────────────────────────────────────────────────────────────

// captureStdout redirects os.Stdout to a pipe, calls fn, and returns the output.
func captureStdout(t *testing.T, fn func()) string {
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
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	r.Close()
	return buf.String()
}

// ── backlog fixture helpers ───────────────────────────────────────────────────

// backlogFrontmatter returns minimal YAML frontmatter for a backlog file.
func backlogFrontmatter(id, title, status string) string {
	return "---\nid: \"" + id + "\"\ntitle: \"" + title + "\"\ntype: feat\nstatus: " + status + "\npriority: medium\nsize: M\n---\n\n# " + title + "\n\nDescription here.\n"
}
