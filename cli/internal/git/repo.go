package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// Repo wraps a go-git repository with helper methods.
// When go-git cannot open the repo (e.g. extensions.worktreeConfig),
// it falls back to git subprocess calls.
type Repo struct {
	R        *gogit.Repository // nil when using fallback mode
	Path     string
	fallback bool // true when using git subprocess fallback
	// worktreeGitDir is the resolved gitdir for a linked worktree (non-empty when
	// the cwd's .git is a FILE pointing into .git/worktrees/<name>). When set,
	// CurrentBranch reads HEAD from this directory instead of the primary .git dir,
	// which is critical for correctness: go-git opens the primary repo object store
	// but each linked worktree has its own HEAD file under .git/worktrees/<name>/HEAD.
	worktreeGitDir string
	// cwdPath is the absolute cwd at Open time — used by ProjectName resolution.
	cwdPath string
}

// resolveWorktreeGitDir walks up from dir to find a linked-worktree .git FILE.
// A linked worktree has .git as a plain FILE whose first line is "gitdir: <path>".
// Returns the resolved gitdir path (e.g. /primary/.git/worktrees/feat-0058) or "".
//
// We walk up because callers may be a subdirectory of the worktree root — the .git
// FILE only exists at the worktree root, not in every subdirectory.
func resolveWorktreeGitDir(dir string) string {
	// Walk up from dir looking for a .git entry.
	current := dir
	for {
		gitPath := filepath.Join(current, ".git")
		fi, err := os.Lstat(gitPath)
		if err == nil {
			if fi.IsDir() {
				// Found a .git DIRECTORY — this is either the primary worktree or a
				// plain repo. Not a linked worktree.
				return ""
			}
			// Found a .git FILE — this is a linked worktree root.
			data, err := os.ReadFile(gitPath)
			if err != nil {
				return ""
			}
			line := strings.TrimSpace(string(data))
			if !strings.HasPrefix(line, "gitdir:") {
				return ""
			}
			rel := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
			// rel may be absolute or relative to the worktree root (current).
			if !filepath.IsAbs(rel) {
				rel = filepath.Join(current, rel)
			}
			abs, err := filepath.Abs(rel)
			if err != nil {
				return rel
			}
			return abs
		}
		// Walk up.
		parent := filepath.Dir(current)
		if parent == current {
			// Reached filesystem root without finding .git.
			return ""
		}
		current = parent
	}
}

// Open opens the git repository at the given path (or cwd if empty).
// Falls back to git subprocess mode when go-git cannot handle the repo
// (e.g. repos with extensions.worktreeConfig set by VS Code/GitKraken).
//
// Worktree-aware: when the cwd has a .git FILE (linked worktree marker),
// Open records the worktree-specific gitdir so CurrentBranch reads the
// correct per-worktree HEAD instead of the primary repo HEAD.
func Open(path string) (*Repo, error) {
	if path == "" {
		var err error
		path, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}

	// Detect linked-worktree .git FILE before calling go-git, because go-git
	// correctly opens the shared object store but loses track of which worktree's
	// HEAD to read (it always reads the primary HEAD).
	worktreeGitDir := resolveWorktreeGitDir(path)

	r, err := gogit.PlainOpenWithOptions(path, &gogit.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		// go-git failed — try git subprocess as fallback
		cmd := exec.Command("git", "rev-parse", "--show-toplevel")
		cmd.Dir = path
		out, gitErr := cmd.Output()
		if gitErr != nil {
			return nil, err // return the original go-git error
		}
		repoPath := strings.TrimSpace(string(out))
		return &Repo{R: nil, Path: repoPath, fallback: true, worktreeGitDir: worktreeGitDir, cwdPath: path}, nil
	}
	return &Repo{R: r, Path: path, worktreeGitDir: worktreeGitDir, cwdPath: path}, nil
}

// gitCmd runs a git command in the repo directory and returns trimmed stdout.
func (r *Repo) gitCmd(args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Path
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// CurrentBranch returns the current branch name.
//
// For linked worktrees (secondary worktrees), go-git opens the shared object store
// of the primary repo and reads the primary HEAD — not the per-worktree HEAD. We
// detect this at Open time by checking for a .git FILE, and when present we read
// HEAD directly from the worktree-specific gitdir (e.g. .git/worktrees/<name>/HEAD).
func (r *Repo) CurrentBranch() string {
	// Linked-worktree fast path: read HEAD from the worktree-specific gitdir.
	if r.worktreeGitDir != "" {
		branch := readHEADFile(filepath.Join(r.worktreeGitDir, "HEAD"))
		if branch != "" {
			return branch
		}
	}
	if r.fallback {
		return r.gitCmd("rev-parse", "--abbrev-ref", "HEAD")
	}
	ref, err := r.R.Head()
	if err != nil {
		return ""
	}
	if ref.Name().IsBranch() {
		return ref.Name().Short()
	}
	return ""
}

// readHEADFile reads a git HEAD file and returns the branch name.
// HEAD format for a checked-out branch: "ref: refs/heads/<branch>\n"
// HEAD format for detached HEAD: "<sha>\n"
// Returns "" for detached HEAD or on read error.
func readHEADFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	const prefix = "ref: refs/heads/"
	if strings.HasPrefix(line, prefix) {
		return strings.TrimPrefix(line, prefix)
	}
	// Detached HEAD — return empty (callers treat "" as detached)
	return ""
}

// DetectBaseBranch returns "homolog" if it exists, else "main" or "master".
func (r *Repo) DetectBaseBranch() string {
	if r.fallback {
		return r.detectBaseBranchFallback(true)
	}
	// Check homolog first (local and remote)
	for _, refName := range []string{"refs/heads/homolog", "refs/remotes/origin/homolog"} {
		_, err := r.R.Reference(plumbing.ReferenceName(refName), false)
		if err == nil {
			return "homolog"
		}
	}

	// Check main/master
	for _, pair := range []struct {
		ref  string
		name string
	}{
		{"refs/heads/main", "main"},
		{"refs/remotes/origin/main", "main"},
		{"refs/heads/master", "master"},
		{"refs/remotes/origin/master", "master"},
	} {
		_, err := r.R.Reference(plumbing.ReferenceName(pair.ref), false)
		if err == nil {
			return pair.name
		}
	}

	return "main"
}

// DetectBaseBranchSimple returns "homolog" if exists, else "main" (no master fallback).
func (r *Repo) DetectBaseBranchSimple() string {
	if r.fallback {
		return r.detectBaseBranchFallback(false)
	}
	for _, refName := range []string{"refs/heads/homolog", "refs/remotes/origin/homolog"} {
		_, err := r.R.Reference(plumbing.ReferenceName(refName), false)
		if err == nil {
			return "homolog"
		}
	}
	return "main"
}

// detectBaseBranchFallback uses git subprocess to detect the base branch.
func (r *Repo) detectBaseBranchFallback(withMaster bool) string {
	branches := r.gitCmd("branch", "-a")
	for _, line := range strings.Split(branches, "\n") {
		name := strings.TrimSpace(strings.TrimPrefix(line, "*"))
		name = strings.TrimSpace(name)
		if name == "homolog" || strings.HasSuffix(name, "/homolog") {
			return "homolog"
		}
	}
	if withMaster {
		for _, line := range strings.Split(branches, "\n") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "*"))
			name = strings.TrimSpace(name)
			if name == "master" || strings.HasSuffix(name, "/master") {
				return "master"
			}
		}
	}
	return "main"
}

// RemoteURL returns the URL of the given remote (default "origin").
//
// When running inside a linked worktree, go-git reads the per-worktree config
// which does not contain [remote] sections (those live in the primary .git/config).
// In that case, fall back to a git subprocess which resolves config via commondir.
func (r *Repo) RemoteURL(name string) string {
	if name == "" {
		name = "origin"
	}
	// Linked worktree: go-git's Remote() will fail because the worktree-specific
	// config has no [remote] sections. Use subprocess which follows commondir.
	if r.fallback || r.worktreeGitDir != "" {
		return r.gitCmd("remote", "get-url", name)
	}
	remote, err := r.R.Remote(name)
	if err != nil {
		return ""
	}
	urls := remote.Config().URLs
	if len(urls) > 0 {
		return urls[0]
	}
	return ""
}

// HasHomologBranch checks if the repo has a homolog branch (local or remote).
// Uses subprocess for reliability (audit rule 10 needs this).
func HasHomologBranch(cwd string) bool {
	cmd := exec.Command("git", "branch", "-a", "--list", "*homolog*")
	if cwd != "" {
		cmd.Dir = cwd
	}
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// ProjectName returns the canonical project name.
//
// Resolution order:
//  1. If inside a linked worktree, walk up to the primary repo root and look for
//     `.bravros.yml` there (the primary worktree holds the authoritative config).
//  2. If `.bravros.yml` exists in cwd (primary worktree), read `project:` from it.
//  3. Fallback: basename of the primary worktree root directory (not cwd, which may
//     be a worktree directory like "myapp-0058" rather than the project "myapp").
//
// Note: the `.bravros.yml` `project:` field is optional. If it's absent this
// function falls back to the primary root dir name — still more correct than cwd.
func ProjectName() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	// Detect linked worktree: .git FILE present at cwd.
	worktreeGitDir := resolveWorktreeGitDir(cwd)
	if worktreeGitDir != "" {
		// Resolve primary worktree root from the gitdir path.
		// worktreeGitDir == /primary/.git/worktrees/<name>  →  primary == /primary
		primaryRoot := resolvePrimaryRoot(worktreeGitDir)
		if primaryRoot != "" {
			if name := projectNameFromDir(primaryRoot); name != "" {
				return name
			}
		}
	}

	// Primary worktree or standalone repo.
	return projectNameFromDir(cwd)
}

// resolvePrimaryRoot derives the primary worktree root from a worktree-specific
// gitdir path. Pattern: <primary>/.git/worktrees/<name> → <primary>.
func resolvePrimaryRoot(worktreeGitDir string) string {
	// Walk up: worktreeGitDir → .git/worktrees → .git → primary root
	// Check if grandparent looks like a .git directory (contains HEAD).
	parent := filepath.Dir(worktreeGitDir)   // .git/worktrees
	grandparent := filepath.Dir(parent)      // .git
	primaryRoot := filepath.Dir(grandparent) // repo root

	// Sanity-check: grandparent should be named ".git" and contain a HEAD file.
	if filepath.Base(grandparent) != ".git" {
		return ""
	}
	if _, err := os.Stat(filepath.Join(grandparent, "HEAD")); err != nil {
		return ""
	}
	return primaryRoot
}

// projectNameFromDir tries to read the project name from .bravros.yml in dir,
// falling back to the directory basename.
func projectNameFromDir(dir string) string {
	// Try .bravros.yml project field.
	if name := readBravrosYMLProject(dir); name != "" {
		return name
	}
	return filepath.Base(dir)
}

// readBravrosYMLProject reads the optional `project:` field from .bravros.yml.
// Returns "" if the file is absent or the field is not set.
func readBravrosYMLProject(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, ".bravros.yml"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "project:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "project:"))
			// Strip optional inline YAML quotes.
			val = strings.Trim(val, `"'`)
			return val
		}
	}
	return ""
}

// IsClean runs `git status --porcelain` and reports whether the working tree
// is clean. Returns (clean, porcelainOutput, error). porcelainOutput is the
// raw output of git status --porcelain (may be non-empty even on clean trees
// if there are ignored-file edge-cases, but in practice empty == clean).
// Callers should gate on clean==true rather than porcelainOutput=="".
// Reusable by any future commit-gating verb (finish, hotfix, etc.) —
// keep this helper, never inline exec.Command in command handlers.
func IsClean() (bool, string, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return false, "", err
	}
	output := strings.TrimRight(string(out), "\n")
	return output == "", output, nil
}
