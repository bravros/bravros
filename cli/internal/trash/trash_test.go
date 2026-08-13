package trash

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gitT runs a git command in dir and fails the test on error.
func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newRepo creates a throwaway repo with one committed file (tracked.txt).
func newRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// Resolve symlinks (macOS $TMPDIR is /var → /private/var) so paths compare
	// equal to `git rev-parse --show-toplevel` output.
	root, _ = filepath.EvalSymlinks(root)
	gitT(t, root, "init", "-q", "-b", "main")
	writeFile(t, root, "tracked.txt", "committed content\n")
	gitT(t, root, "add", ".")
	gitT(t, root, "commit", "-q", "-m", "init")
	return root
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, root, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return data
}

// TestDiscard_PreservesThenDiscards: a modified tracked file and an untracked
// file are both preserved into .trash/ and then discarded/removed.
func TestDiscard_PreservesThenDiscards(t *testing.T) {
	root := newRepo(t)
	writeFile(t, root, "tracked.txt", "uncommitted modification\n")
	writeFile(t, root, "sub/dir/new.txt", "never seen by git\n")

	res, err := Discard(root, []string{"."}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.EntryID == "" {
		t.Fatal("expected a .trash entry to be created")
	}
	if len(res.CheckedOut) != 1 || res.CheckedOut[0] != "tracked.txt" {
		t.Errorf("CheckedOut = %v, want [tracked.txt]", res.CheckedOut)
	}
	if len(res.Removed) != 1 || res.Removed[0] != "sub/dir/new.txt" {
		t.Errorf("Removed = %v, want [sub/dir/new.txt]", res.Removed)
	}

	// Tree state: tracked restored to committed content, untracked gone,
	// emptied dirs pruned.
	if got := readFile(t, root, "tracked.txt"); string(got) != "committed content\n" {
		t.Errorf("tracked.txt = %q, want committed content", got)
	}
	if _, err := os.Stat(filepath.Join(root, "sub")); !os.IsNotExist(err) {
		t.Error("emptied untracked dir sub/ should have been pruned")
	}

	// Preserved copies exist inside the entry.
	entry := filepath.Join(root, DirName, res.EntryID)
	if got := readFile(t, entry, "tracked.txt"); string(got) != "uncommitted modification\n" {
		t.Errorf("preserved tracked.txt = %q", got)
	}
	if got := readFile(t, entry, "sub/dir/new.txt"); string(got) != "never seen by git\n" {
		t.Errorf("preserved new.txt = %q", got)
	}
}

// TestRestore_ByteIdentical: restore returns files byte-identical to what was
// preserved (acceptance criterion 6).
func TestRestore_ByteIdentical(t *testing.T) {
	root := newRepo(t)
	original := "uncommitted modification with bytes \x00\xff\n"
	// NUL bytes can't go through git, but CAN go through cp -a — write directly.
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Discard(root, []string{"tracked.txt"}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, root, "tracked.txt"); string(got) == original {
		t.Fatal("discard did not reset the file")
	}

	n, err := Restore(root, res.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("restored %d files, want 1", n)
	}
	if got := readFile(t, root, "tracked.txt"); !bytes.Equal(got, []byte(original)) {
		t.Errorf("restored content differs: %q vs %q", got, original)
	}
}

// TestRestore_RejectsBadID: path-traversal ids are refused.
func TestRestore_RejectsBadID(t *testing.T) {
	root := newRepo(t)
	for _, id := range []string{"../evil", "a/b", "..", "."} {
		if _, err := Restore(root, id); err == nil {
			t.Errorf("Restore(%q) should fail", id)
		}
	}
}

// TestDiscard_CleanUntrackedLeavesTrackedAlone: untrackedOnly mode removes the
// untracked file but keeps the tracked modification in place.
func TestDiscard_CleanUntrackedLeavesTrackedAlone(t *testing.T) {
	root := newRepo(t)
	writeFile(t, root, "tracked.txt", "keep this modification\n")
	writeFile(t, root, "loose.txt", "untracked\n")

	res, err := Discard(root, nil, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.CheckedOut) != 0 {
		t.Errorf("clean-untracked must not checkout tracked files, got %v", res.CheckedOut)
	}
	if len(res.Removed) != 1 || res.Removed[0] != "loose.txt" {
		t.Errorf("Removed = %v, want [loose.txt]", res.Removed)
	}
	if got := readFile(t, root, "tracked.txt"); string(got) != "keep this modification\n" {
		t.Errorf("tracked modification must survive clean-untracked, got %q", got)
	}
}

// TestDiscard_NothingToDo: clean tree returns an empty result, creates no entry.
func TestDiscard_NothingToDo(t *testing.T) {
	root := newRepo(t)
	res, err := Discard(root, []string{"."}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Empty() {
		t.Errorf("clean tree should be a no-op, got %+v", res)
	}
	if _, err := os.Stat(Root(root)); !os.IsNotExist(err) {
		t.Error("no .trash/ should be created on a no-op run")
	}
}

// TestDiscard_DryRun: dry-run reports the plan but touches nothing.
func TestDiscard_DryRun(t *testing.T) {
	root := newRepo(t)
	writeFile(t, root, "tracked.txt", "modified\n")

	res, err := Discard(root, []string{"."}, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.CheckedOut) != 1 {
		t.Errorf("dry-run should report the checkout plan, got %v", res.CheckedOut)
	}
	if res.EntryID != "" {
		t.Error("dry-run must not create a trash entry")
	}
	if got := readFile(t, root, "tracked.txt"); string(got) != "modified\n" {
		t.Error("dry-run must not touch the tree")
	}
}

// TestGC_HonoursWindow: entries older than the window are reaped, younger kept.
func TestGC_HonoursWindow(t *testing.T) {
	root := newRepo(t)
	trashRoot := Root(root)

	oldStamp := time.Now().UTC().Add(-40 * 24 * time.Hour).Format(stampLayout)
	newStamp := time.Now().UTC().Format(stampLayout)
	oldDir := filepath.Join(trashRoot, oldStamp+"-discard")
	newDir := filepath.Join(trashRoot, newStamp+"-discard")
	for _, d := range []string{oldDir, newDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := GC(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != filepath.Base(oldDir) {
		t.Errorf("removed = %v, want only the 40-day-old entry", removed)
	}
	if _, err := os.Stat(newDir); err != nil {
		t.Error("young entry must survive GC")
	}
}

// TestEnsureGitignore_Idempotent: the append happens once, in any spelling.
func TestEnsureGitignore_Idempotent(t *testing.T) {
	root := newRepo(t)
	for i := 0; i < 3; i++ {
		if err := EnsureGitignore(root); err != nil {
			t.Fatal(err)
		}
	}
	data := readFile(t, root, ".gitignore")
	if got := strings.Count(string(data), ".trash"); got != 1 {
		t.Errorf(".gitignore contains %d .trash lines, want 1:\n%s", got, data)
	}
}

// TestDiscard_TrashEntryInvisibleToGit: after a discard, git status is clean —
// the preserved copies must not themselves show up as untracked content.
func TestDiscard_TrashEntryInvisibleToGit(t *testing.T) {
	root := newRepo(t)
	writeFile(t, root, "tracked.txt", "modified\n")
	if _, err := Discard(root, []string{"."}, false, false); err != nil {
		t.Fatal(err)
	}
	entries, err := Status(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Path != ".gitignore" { // the idempotent append legitimately dirties it
			t.Errorf("unexpected status entry after discard: %c%c %s", e.X, e.Y, e.Path)
		}
	}
}
