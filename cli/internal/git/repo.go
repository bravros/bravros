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
}

// Open opens the git repository at the given path (or cwd if empty).
// Falls back to git subprocess mode when go-git cannot handle the repo
// (e.g. repos with extensions.worktreeConfig set by VS Code/GitKraken).
func Open(path string) (*Repo, error) {
	if path == "" {
		var err error
		path, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
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
		return &Repo{R: nil, Path: repoPath, fallback: true}, nil
	}
	return &Repo{R: r, Path: path}, nil
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
func (r *Repo) CurrentBranch() string {
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
func (r *Repo) RemoteURL(name string) string {
	if name == "" {
		name = "origin"
	}
	if r.fallback {
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

// ProjectName returns the current directory name (used as project name).
func ProjectName() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Base(cwd)
}
