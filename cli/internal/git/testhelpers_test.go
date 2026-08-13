package git

import (
	"os"
	"path/filepath"
	"testing"
)

// initTestRepo creates a temporary git repo with an initial commit.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "git", "init")
	gitRun(t, dir, "git", "config", "user.email", "test@test.com")
	gitRun(t, dir, "git", "config", "user.name", "Test")
	writeFile(t, dir, "README.md", "# test repo")
	gitRun(t, dir, "git", "add", ".")
	gitRun(t, dir, "git", "commit", "-m", "initial commit")
	return dir
}

// gitRun runs a git command in the given directory, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, stderr, err := RunInDir(dir, args...)
	if err != nil {
		t.Fatalf("command %v failed: %v\nstderr: %s", args, err, stderr)
	}
	return out
}

// writeFile creates a file with the given content under dir.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
