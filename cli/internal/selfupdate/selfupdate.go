// Package selfupdate provides portable repo update logic for kaisser selfupdate.
//
// The portable repo is updated via git fetch + checkout overlay, which works on any branch
// including homolog. Skills, hooks, scripts, and the portable repo working tree are all
// updated atomically via this layer. The binary itself is sourced from install.sh, which
// handles falling back from GitHub Releases to a committed bin/kaisser as needed.
package selfupdate

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// UpdatePortableRepo updates the portable repo's working tree to match origin/main WITHOUT
// switching branches. This is the branch-agnostic replacement for `git pull --ff-only origin main`.
//
// Precondition: the caller must have already run `git fetch origin main` so that
// origin/main is current. This function does NOT re-fetch to avoid a redundant round-trip.
//
// Approach:
//  1. Pre-flight check: abort if working tree has uncommitted tracked-file changes.
//  2. git -C repo checkout origin/main -- . (updates working tree without touching HEAD)
//
// This works on any branch, including homolog. The user's local commits and untracked
// files are untouched; only tracked files whose content differs from origin/main are updated.
func UpdatePortableRepo(repo string, dryRun bool) error {
	if dryRun {
		fmt.Fprintf(os.Stderr, "  [dry-run] would run: git -C %s checkout origin/main -- .\n", repo)
		return nil
	}

	// Pre-flight: refuse to overwrite local modifications in tracked files.
	// `git status --porcelain` emits one line per dirty/staged/untracked file.
	// We filter out "??" (untracked) lines — untracked files are untouched by
	// `git checkout origin/main -- .` so they must NOT block the update.
	//
	// Exception: .kaisser.yml and the legacy .skaisser.yml are generated/cached
	// portable-repo metadata. Claude session startup and context refreshes can
	// rewrite them as a side effect of simply opening the repo. Those disposable
	// diffs must not permanently block the auto-update hook; origin/main remains
	// authoritative for the portable source repo copy.
	dirtyOut, _ := exec.Command("git", "-C", repo, "status", "--porcelain").Output()
	var trackedDirty []string
	for _, line := range strings.Split(string(dirtyOut), "\n") {
		if line == "" || strings.HasPrefix(line, "?? ") {
			continue
		}
		if isDisposablePortableConfigDirty(line) {
			continue
		}
		trackedDirty = append(trackedDirty, line)
	}
	if len(trackedDirty) > 0 {
		return fmt.Errorf("selfupdate: working tree has uncommitted changes — stash or commit before updating")
	}

	// Update working tree to match origin/main without switching branches.
	// Precondition: caller already fetched origin/main.
	checkoutCmd := exec.Command("git", "-C", repo, "checkout", "origin/main", "--", ".")
	var stderr strings.Builder
	checkoutCmd.Stderr = &stderr
	if err := checkoutCmd.Run(); err != nil {
		return fmt.Errorf("selfupdate: git checkout origin/main failed: %w — %s", err, strings.TrimSpace(stderr.String()))
	}

	return nil
}

func isDisposablePortableConfigDirty(porcelainLine string) bool {
	if len(porcelainLine) < 4 {
		return false
	}
	path := strings.TrimSpace(porcelainLine[3:])
	if idx := strings.LastIndex(path, " -> "); idx >= 0 {
		src := strings.TrimSpace(path[:idx])
		dst := strings.TrimSpace(path[idx+4:])
		return isDisposableConfigPath(src) && isDisposableConfigPath(dst)
	}
	return isDisposableConfigPath(path)
}

func isDisposableConfigPath(path string) bool {
	return path == ".kaisser.yml" || path == ".skaisser.yml"
}

// HasOriginMainUpdates returns true when the fetched origin/main tree contains
// commits that are not present in the current HEAD. This includes both the
// normal strictly-behind case and the homolog/feature-branch diverged case
// where HEAD has local commits but origin/main also moved forward.
//
// Uses the already-fetched origin/main ref — no network call.
func HasOriginMainUpdates(repo string) bool {
	localOut, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		return false
	}
	remoteOut, err := exec.Command("git", "-C", repo, "rev-parse", "origin/main").Output()
	if err != nil {
		return false
	}
	local := strings.TrimSpace(string(localOut))
	remote := strings.TrimSpace(string(remoteOut))
	if local == remote {
		return false
	}

	// Exit 0 → origin/main is already contained in HEAD (HEAD is ahead only).
	// Exit 1 → origin/main has commits HEAD does not contain (behind/diverged).
	// Other errors are treated as no-op for selfupdate's fail-soft hook behavior.
	containedCmd := exec.Command("git", "-C", repo, "merge-base", "--is-ancestor", "origin/main", "HEAD")
	if err := containedCmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return true
		}
		return false
	}
	return false
}

// IsBehindOriginMain returns true only when the local HEAD is STRICTLY behind origin/main
// (HEAD is an ancestor of origin/main). Returns false in three cases:
//   - HEAD == origin/main (already up to date)
//   - HEAD is ahead of origin/main (local commits not yet on main)
//   - HEAD is diverged from origin/main (commits on both sides — e.g. homolog ahead with
//     origin/main also having new commits from another branch's promotion)
//
// The "diverged" case is critical: previously this function returned true on diverged HEADs,
// which caused `UpdatePortableRepo` to run `git checkout origin/main -- .`, silently rewriting
// homolog's working tree to main's content and resurrecting deleted-on-homolog files. By
// requiring HEAD to be an ancestor of origin/main, diverged branches are left alone.
//
// Uses the already-fetched origin/main ref — no network call.
func IsBehindOriginMain(repo string) bool {
	// Fast path: if HEAD == origin/main, nothing to do.
	localOut, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		return false
	}
	remoteOut, err := exec.Command("git", "-C", repo, "rev-parse", "origin/main").Output()
	if err != nil {
		return false
	}
	local := strings.TrimSpace(string(localOut))
	remote := strings.TrimSpace(string(remoteOut))
	if local == remote {
		return false // already up to date
	}

	// HEAD must be an ancestor of origin/main for "strictly behind" to hold.
	// Exit 0 → HEAD IS an ancestor → strictly behind → update.
	// Exit 1 → HEAD is NOT an ancestor → ahead OR diverged → don't clobber.
	behindCmd := exec.Command("git", "-C", repo, "merge-base", "--is-ancestor", "HEAD", "origin/main")
	if err := behindCmd.Run(); err != nil {
		return false
	}
	return true
}
