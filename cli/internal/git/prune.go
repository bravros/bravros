package git

import (
	"strings"
	"time"
)

// ListMergedBranches returns remote branches already merged into `against`.
// It runs `git branch -r --merged origin/<against>`, strips the `origin/` prefix,
// and skips the HEAD ref and the `against` branch itself.
func ListMergedBranches(against string) ([]string, error) {
	out, _, err := Run("git", "branch", "-r", "--merged", "origin/"+against)
	if err != nil {
		// Fallback: try without the origin/ prefix (works for local-only repos in tests)
		out, _, err = Run("git", "branch", "--merged", against)
		if err != nil {
			return nil, err
		}
		// Parse local branch output (no origin/ prefix, no HEAD ref)
		var branches []string
		for _, line := range strings.Split(out, "\n") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "* "))
			if name == "" || name == against {
				continue
			}
			branches = append(branches, name)
		}
		return branches, nil
	}

	var branches []string
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		// Skip HEAD pointer
		if strings.Contains(name, "HEAD ->") || strings.HasSuffix(name, "/HEAD") {
			continue
		}
		// Strip origin/ prefix
		name = strings.TrimPrefix(name, "origin/")
		// Skip the against branch itself
		if name == against {
			continue
		}
		branches = append(branches, name)
	}
	return branches, nil
}

// FilterProtected splits branches into two slices: kept (protected) and removed (eligible).
// A branch is protected if it exactly matches any entry in the protected list.
func FilterProtected(branches []string, protected []string) (kept, removed []string) {
	protectedSet := make(map[string]bool, len(protected))
	for _, p := range protected {
		protectedSet[p] = true
	}
	for _, b := range branches {
		if protectedSet[b] {
			kept = append(kept, b)
		} else {
			removed = append(removed, b)
		}
	}
	return kept, removed
}

// BranchLastCommitAge returns the duration since the last commit on a remote branch.
// Returns (age, error). If the branch has no commits, returns (0, nil).
func BranchLastCommitAge(branch string) (time.Duration, error) {
	// Use git log to get the last commit date for the remote branch
	out, _, err := Run("git", "log", "-1", "--format=%ct", "origin/"+branch)
	if err != nil || strings.TrimSpace(out) == "" {
		// Fallback: try local branch
		out, _, err = Run("git", "log", "-1", "--format=%ct", branch)
		if err != nil || strings.TrimSpace(out) == "" {
			return 0, err
		}
	}
	var epochSecs int64
	_, parseErr := strings.NewReader(out), error(nil)
	// Manual parse to avoid importing strconv indirection
	for _, ch := range strings.TrimSpace(out) {
		if ch < '0' || ch > '9' {
			break
		}
		epochSecs = epochSecs*10 + int64(ch-'0')
	}
	_ = parseErr
	if epochSecs == 0 {
		return 0, nil
	}
	commitTime := time.Unix(epochSecs, 0)
	return time.Since(commitTime), nil
}
