package cmd

// setup_hostpath_rewrite_test.go pins the two functions the fix touches
// directly: setupCompare must rewrite staged content before diffing against
// an already-rewritten target (or a spurious conflict follows on every run),
// and setupCopyFile must rewrite what it actually writes — including into a
// <name>.new conflict file. See TestSetupConflictNewFileRewritesHostPaths in
// setup_test.go for the end-to-end reproduction through the real embedded
// payload.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestSetupCompareRewritesBeforeComparing: raw staged content and an
// already-rewritten target must compare as unchanged when rewrite is on
// (Claude target), and as a genuine conflict when rewrite is off.
func TestSetupCompareRewritesBeforeComparing(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "SKILL.md")
	dst := filepath.Join(dir, "target.md")

	raw := "run ~/.bravros/scripts/announce.sh --force\n"
	if err := os.WriteFile(src, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	rewritten := "run ~/.claude/scripts/announce.sh --force\n"
	if err := os.WriteFile(dst, []byte(rewritten), 0o644); err != nil {
		t.Fatal(err)
	}

	if action, err := setupCompare(src, dst, true); err != nil {
		t.Fatal(err)
	} else if action != setupActionUnchanged {
		t.Errorf("setupCompare(rewrite=true) = %q, want %q (raw staged content should match the already-rewritten target)", action, setupActionUnchanged)
	}

	// Same inputs, rewrite disabled (non-Claude target): the raw staged
	// content and the ~/.claude target genuinely differ.
	if action, err := setupCompare(src, dst, false); err != nil {
		t.Fatal(err)
	} else if action != setupActionConflict {
		t.Errorf("setupCompare(rewrite=false) = %q, want %q", action, setupActionConflict)
	}
}

// TestSetupCopyFileRewritesConflictNewFile: the file setupApply writes for a
// conflict (<name>.new — the one an operator `mv`s over the original) must
// carry the rewritten ~/.claude path, never the raw ~/.bravros /
// ~/.agent_config token, and text-file detection must key off src's name
// (not dst's, which carries the ".new" suffix and would defeat the check).
func TestSetupCopyFileRewritesConflictNewFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "SKILL.md")
	raw := "cp ~/.bravros/scripts/announce.sh --force\nsee also ~/.agent_config/skills/foo\n"
	if err := os.WriteFile(src, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(dir, "claude-target", "skills", "foo", "SKILL.md")
	newFile := target + ".new"

	if err := setupCopyFile(src, newFile, true); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(newFile)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got, []byte("~/.bravros/scripts")) || bytes.Contains(got, []byte("~/.agent_config")) {
		t.Fatalf(".new file still carries a raw host-agnostic token:\n%s", got)
	}
	want := "cp ~/.claude/scripts/announce.sh --force\nsee also ~/.claude/skills/foo\n"
	if string(got) != want {
		t.Errorf(".new file = %q, want %q", got, want)
	}

	// rewrite disabled (non-Claude target): copy must stay byte-for-byte.
	newFile2 := target + ".new2"
	if err := setupCopyFile(src, newFile2, false); err != nil {
		t.Fatal(err)
	}
	got2, err := os.ReadFile(newFile2)
	if err != nil {
		t.Fatal(err)
	}
	if string(got2) != raw {
		t.Errorf("rewrite=false copy = %q, want raw %q", got2, raw)
	}
}
