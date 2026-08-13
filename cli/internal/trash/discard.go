package trash

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DiscardResult reports what a Discard run preserved and acted on.
// All paths are repo-relative.
type DiscardResult struct {
	EntryID    string   // .trash/ entry holding the preserved copies ("" when nothing preserved)
	Preserved  []string // files copied into the entry before acting
	CheckedOut []string // tracked paths restored via `git checkout --`
	Removed    []string // untracked files deleted
}

// Empty reports whether the run found nothing to discard.
func (r *DiscardResult) Empty() bool {
	return len(r.CheckedOut) == 0 && len(r.Removed) == 0
}

// Discard is the sanctioned preserve-then-discard path (P-0184 layer 1).
// It resolves pathspecs via `git status --porcelain`, copies every file whose
// current content git has never seen into a fresh .trash/ entry, THEN discards:
// `git checkout --` for tracked modifications, delete for untracked files.
// No token required — the discard is reversible via `kaisser trash restore`.
//
// untrackedOnly restricts the run to untracked files (`kaisser clean-untracked`).
// dryRun reports what would happen without touching the tree.
func Discard(repoRoot string, pathspecs []string, untrackedOnly, dryRun bool) (*DiscardResult, error) {
	entries, err := Status(repoRoot, pathspecs)
	if err != nil {
		return nil, err
	}

	res := &DiscardResult{}
	var preserve []string
	for _, e := range entries {
		switch {
		case e.Untracked():
			preserve = append(preserve, e.Path)
			res.Removed = append(res.Removed, e.Path)
		case untrackedOnly:
			// clean-untracked leaves tracked modifications alone.
		case e.WorktreeModified():
			if e.Y == 'M' { // Y=D has no worktree content to preserve
				preserve = append(preserve, e.Path)
			}
			res.CheckedOut = append(res.CheckedOut, e.Path)
		}
	}
	if res.Empty() || dryRun {
		res.Preserved = preserve
		return res, nil
	}

	// Preserve FIRST — by copy, so a failure here leaves the tree untouched.
	if len(preserve) > 0 {
		slug := "discard"
		if untrackedOnly {
			slug = "clean-untracked"
		}
		entryDir, err := NewEntryDir(repoRoot, slug)
		if err != nil {
			return nil, err
		}
		if err := Preserve(repoRoot, entryDir, preserve); err != nil {
			return nil, err
		}
		res.EntryID = filepath.Base(entryDir)
		res.Preserved = preserve
	}

	// Discard tracked modifications via git itself.
	if len(res.CheckedOut) > 0 {
		args := append([]string{"checkout", "--"}, res.CheckedOut...)
		cmd := exec.Command("git", args...)
		cmd.Dir = repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			return res, fmt.Errorf("git checkout --: %v: %s", err, strings.TrimSpace(string(out)))
		}
	}

	// Delete untracked files, pruning directories the deletions emptied.
	for _, rel := range res.Removed {
		abs := filepath.Join(repoRoot, rel)
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return res, fmt.Errorf("remove %s: %v", rel, err)
		}
		pruneEmptyParents(repoRoot, filepath.Dir(abs))
	}
	return res, nil
}

// pruneEmptyParents removes now-empty directories from dir up toward repoRoot
// (exclusive). Stops at the first non-empty directory.
func pruneEmptyParents(repoRoot, dir string) {
	for dir != repoRoot && strings.HasPrefix(dir, repoRoot+string(filepath.Separator)) {
		if err := os.Remove(dir); err != nil {
			return // non-empty or otherwise busy — stop
		}
		dir = filepath.Dir(dir)
	}
}
