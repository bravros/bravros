// Package trash implements the repo-local .trash/ preserve area backing
// `kaisser discard`, `kaisser clean-untracked` and `kaisser trash *` (P-0184).
//
// Design principle: git can only restore what it has already seen. Content that
// was never staged or committed exists nowhere in git, so before any discard the
// affected files are COPIED (never moved — a copy cannot fail halfway and leave
// the tree inconsistent) into <repo-root>/.trash/<UTC-stamp>-<slug>/ preserving
// their repo-relative layout. The directory is gitignored and swept after
// DefaultRetentionDays by `kaisser trash gc` / `kaisser branch prune --gc`.
package trash

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DirName is the preserve area's directory name at the repo root.
const DirName = ".trash"

// DefaultRetentionDays is the GC window: entries older than this are reaped.
const DefaultRetentionDays = 30

// stampLayout formats entry timestamps: 20260730T153000Z. Lexicographic order
// equals chronological order, so `ls .trash/` reads oldest-first.
const stampLayout = "20060102T150405Z"

// Entry describes one preserved .trash/ entry directory.
type Entry struct {
	ID        string    // directory basename, e.g. 20260730T153000Z-discard
	Path      string    // absolute path of the entry directory
	CreatedAt time.Time // parsed from the ID stamp; directory mtime fallback
	Files     int       // regular files preserved in the entry
	Bytes     int64     // total size of those files
}

// RepoRoot resolves the enclosing repository root for dir ("" = cwd).
// Worktree-aware: inside a linked worktree it returns the worktree's own root.
func RepoRoot(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository")
	}
	return strings.TrimSpace(string(out)), nil
}

// Root returns the absolute path of the preserve area for repoRoot.
func Root(repoRoot string) string {
	return filepath.Join(repoRoot, DirName)
}

// EnsureGitignore idempotently appends ".trash/" to <repoRoot>/.gitignore.
// A no-op when any existing line already covers the directory.
func EnsureGitignore(repoRoot string) error {
	path := filepath.Join(repoRoot, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		switch strings.TrimSpace(line) {
		case ".trash", ".trash/", "/.trash", "/.trash/":
			return nil
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := ""
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		prefix = "\n"
	}
	_, err = f.WriteString(prefix + ".trash/\n")
	return err
}

// NewEntryDir creates a fresh, collision-safe entry directory
// .trash/<UTC-stamp>-<slug>[-N]/ and returns its absolute path. It also
// ensures the preserve area exists and is gitignored.
func NewEntryDir(repoRoot, slug string) (string, error) {
	if err := EnsureGitignore(repoRoot); err != nil {
		return "", err
	}
	stamp := time.Now().UTC().Format(stampLayout)
	base := filepath.Join(Root(repoRoot), stamp+"-"+slug)
	dir := base
	for n := 2; ; n++ {
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return "", err
		}
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			return dir, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
		dir = fmt.Sprintf("%s-%d", base, n)
	}
}

// Preserve copies each repo-relative path into entryDir, recreating the
// relative layout (`cp -a` — permissions and times survive the round-trip).
// Copy, never move: the working tree is untouched until the caller acts.
func Preserve(repoRoot, entryDir string, relPaths []string) error {
	for _, rel := range relPaths {
		src := filepath.Join(repoRoot, rel)
		dst := filepath.Join(entryDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if out, err := exec.Command("cp", "-a", src, dst).CombinedOutput(); err != nil {
			return fmt.Errorf("preserve %s: %v: %s", rel, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// List returns all entries in the preserve area, oldest first.
func List(repoRoot string) ([]Entry, error) {
	root := Root(repoRoot)
	dirents, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, d := range dirents {
		if !d.IsDir() {
			continue
		}
		e := Entry{ID: d.Name(), Path: filepath.Join(root, d.Name())}
		e.CreatedAt = entryTime(e.Path, e.ID)
		filepath.Walk(e.Path, func(_ string, info os.FileInfo, err error) error { //nolint:errcheck
			if err == nil && info.Mode().IsRegular() {
				e.Files++
				e.Bytes += info.Size()
			}
			return nil
		})
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, nil
}

// entryTime derives an entry's creation time from its ID stamp, falling back
// to directory mtime when the name does not carry a parseable stamp.
func entryTime(path, id string) time.Time {
	if len(id) >= len(stampLayout) {
		if ts, err := time.Parse(stampLayout, id[:len(stampLayout)]); err == nil {
			return ts
		}
	}
	if info, err := os.Stat(path); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

// Restore copies every file of entry id back into the repo at its original
// relative location (`cp -a`, byte-identical, overwriting what is there now).
// Returns the number of files restored.
func Restore(repoRoot, id string) (int, error) {
	if id != filepath.Base(id) || id == "." || id == ".." {
		return 0, fmt.Errorf("invalid trash id %q", id)
	}
	entryDir := filepath.Join(Root(repoRoot), id)
	if info, err := os.Stat(entryDir); err != nil || !info.IsDir() {
		return 0, fmt.Errorf("no trash entry %q (see `kaisser trash list`)", id)
	}
	restored := 0
	err := filepath.Walk(entryDir, func(src string, info os.FileInfo, err error) error {
		if err != nil || !info.Mode().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(entryDir, src)
		if err != nil {
			return err
		}
		dst := filepath.Join(repoRoot, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if out, err := exec.Command("cp", "-a", src, dst).CombinedOutput(); err != nil {
			return fmt.Errorf("restore %s: %v: %s", rel, err, strings.TrimSpace(string(out)))
		}
		restored++
		return nil
	})
	return restored, err
}

// GC removes entries older than days (<=0 defaults to DefaultRetentionDays)
// and returns the removed entry IDs. Missing preserve area is a silent no-op.
func GC(repoRoot string, days int) ([]string, error) {
	if days <= 0 {
		days = DefaultRetentionDays
	}
	entries, err := List(repoRoot)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	var removed []string
	for _, e := range entries {
		if e.CreatedAt.IsZero() || e.CreatedAt.After(cutoff) {
			continue
		}
		if err := os.RemoveAll(e.Path); err != nil {
			return removed, err
		}
		removed = append(removed, e.ID)
	}
	return removed, nil
}

// StatusEntry is one parsed `git status --porcelain` record.
type StatusEntry struct {
	X, Y byte   // index / worktree status columns
	Path string // repo-relative path
}

// Untracked reports whether the entry is an untracked file (??).
func (s StatusEntry) Untracked() bool { return s.X == '?' && s.Y == '?' }

// WorktreeModified reports whether the entry carries a worktree-side
// modification to a tracked file (content git has never seen: Y = M),
// or a worktree deletion (Y = D). A worktree deletion holds no content to
// preserve, but marks the path as carrying uncommitted state.
func (s StatusEntry) WorktreeModified() bool { return s.Y == 'M' || s.Y == 'D' }

// NeverSeen reports whether the entry's current worktree content exists
// nowhere in git: an untracked file, or a worktree modification.
func (s StatusEntry) NeverSeen() bool { return s.Untracked() || s.WorktreeModified() }

// Status runs `git status --porcelain -uall -z [-- pathspecs]` in dir and
// parses the entries. -z gives NUL-separated unquoted paths; -uall expands
// untracked directories into individual files so each can be preserved.
func Status(dir string, pathspecs []string) ([]StatusEntry, error) {
	args := []string{"status", "--porcelain", "-uall", "-z"}
	if len(pathspecs) > 0 {
		args = append(args, "--")
		args = append(args, pathspecs...)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git status: %v", err)
	}
	return parsePorcelainZ(out), nil
}

// parsePorcelainZ parses NUL-separated `git status --porcelain -z` output.
// Rename/copy records (X in {R,C}) carry a second NUL-separated origin path,
// which is skipped — the FIRST path is the current one.
func parsePorcelainZ(out []byte) []StatusEntry {
	var entries []StatusEntry
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		e := StatusEntry{X: f[0], Y: f[1], Path: f[3:]}
		entries = append(entries, e)
		if e.X == 'R' || e.X == 'C' {
			i++ // skip the rename/copy origin path record
		}
	}
	return entries
}
