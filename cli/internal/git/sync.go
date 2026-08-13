package git

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// SyncResult is the JSON payload returned by SyncBranch.
type SyncResult struct {
	Base    string `json:"base"`
	Behind  int    `json:"behind"`
	Rebased bool   `json:"rebased"`
	DryRun  bool   `json:"dry_run"`
}

// SyncBranch fetches origin/<base> and counts commits behind. If not dry-run
// and behind > 0, rebases the current branch onto origin/<base>.
//
// Returns structured result; on rebase conflict, returns an error with the
// partial result (rebase was aborted, current branch unchanged beyond fetch).
func SyncBranch(base string, dryRun bool) (*SyncResult, error) {
	result := &SyncResult{Base: base, DryRun: dryRun}

	// Fetch the base ref from origin.
	if _, stderr, err := Run("git", "fetch", "origin", base); err != nil {
		return result, fmt.Errorf("fetch failed: %s", firstNonEmpty(stderr, err.Error()))
	}

	// Count commits behind: how many commits on origin/<base> are not in HEAD.
	out, _, err := Run("git", "rev-list", "--count", "HEAD..origin/"+base)
	if err != nil {
		return result, fmt.Errorf("rev-list failed: %v", err)
	}
	behind, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return result, fmt.Errorf("rev-list parse failed: %v", err)
	}
	result.Behind = behind

	if dryRun || behind == 0 {
		return result, nil
	}

	// Rebase onto origin/<base>. On conflict, abort the rebase so the branch
	// state stays clean — caller handles conflict resolution manually.
	if _, stderr, rebaseErr := Run("git", "rebase", "origin/"+base); rebaseErr != nil {
		// Best-effort abort; swallow error
		_ = exec.Command("git", "rebase", "--abort").Run()
		return result, fmt.Errorf("rebase failed (aborted): %s", firstNonEmpty(stderr, rebaseErr.Error()))
	}
	result.Rebased = true
	return result, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}
