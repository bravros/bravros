package payload

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// repoRootSkillsDir returns the path to repo-root skills/ as seen from this
// test file's own directory (cli/internal/payload), without hardcoding a
// count: the dossier said 34, the live tree has 35, and a magic number rots.
func repoRootSkillsDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "skills"))
	if err != nil {
		t.Fatalf("resolve repo-root skills dir: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("repo-root skills dir not found at %q (is the payload package still at cli/internal/payload?): %v", dir, err)
	}
	return dir
}

func TestListTopLevel_SkillsMatchesRepoRoot(t *testing.T) {
	srcDir := repoRootSkillsDir(t)

	srcEntries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("read repo-root skills dir: %v", err)
	}
	var want []string
	for _, e := range srcEntries {
		if e.IsDir() {
			want = append(want, e.Name())
		}
	}
	sort.Strings(want)

	if len(want) == 0 {
		t.Fatalf("repo-root skills dir at %q yielded zero directories — refusing a vacuous test", srcDir)
	}

	got, err := ListTopLevel("skills")
	if err != nil {
		t.Fatalf("ListTopLevel(skills): %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("embedded skills top-level count = %d, repo-root skills/ has %d\nembedded: %v\nrepo-root: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("embedded skills top-level entry[%d] = %q, want %q\nembedded: %v\nrepo-root: %v", i, got[i], want[i], got, want)
		}
	}
}

func TestEmbeddedGithooksCommitMsg(t *testing.T) {
	data, err := FS.ReadFile("templates/.githooks/commit-msg")
	if err != nil {
		t.Fatalf("read templates/.githooks/commit-msg from embedded FS: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("templates/.githooks/commit-msg is embedded but empty")
	}
}

func TestExtract_WritesFilesAndPreservesExecutableBit(t *testing.T) {
	// Skip if the source commit-msg hook isn't executable on this checkout —
	// the assertion is conditional on the source having the bit set, per the
	// dispatch brief.
	srcInfo, err := os.Stat(filepath.Join("templates", ".githooks", "commit-msg"))
	if err != nil {
		t.Fatalf("stat source templates/.githooks/commit-msg: %v", err)
	}
	srcExecutable := srcInfo.Mode()&0o111 != 0

	destDir := t.TempDir()
	if err := Extract("templates", destDir); err != nil {
		t.Fatalf("Extract(templates, %q): %v", destDir, err)
	}

	hookPath := filepath.Join(destDir, ".githooks", "commit-msg")
	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatalf("Extract did not write %q: %v", hookPath, err)
	}
	if info.Size() == 0 {
		t.Fatalf("extracted %q is empty", hookPath)
	}

	gotExecutable := info.Mode()&0o111 != 0
	if srcExecutable && !gotExecutable {
		t.Fatalf("extracted %q lost the executable bit: mode = %v", hookPath, info.Mode())
	}

	// Spot-check a plain (non-executable) file also landed correctly.
	claudeMD := filepath.Join(destDir, "CLAUDE.md")
	if _, err := os.Stat(claudeMD); err != nil {
		t.Fatalf("Extract did not write %q: %v", claudeMD, err)
	}
}
