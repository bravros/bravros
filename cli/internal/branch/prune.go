// Package branch provides branch lifecycle helpers — primarily the prune workflow
// that detects merged branches, enforces safety guards, writes tombstones for
// recoverability, and optionally performs deletions.
package branch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bravros/bravros/cli/internal/config"
	gitpkg "github.com/bravros/bravros/cli/internal/git"
)

// MergeSource describes how a branch was determined to be merged.
type MergeSource string

const (
	MergeSourceGit        MergeSource = "git"         // git branch --merged confirmed
	MergeSourcePR         MergeSource = "pr"          // gh pr list --merged confirmed
	MergeSourcePRVerified MergeSource = "pr-verified" // PR confirmed + merge commit is ancestor of base
)

// SkipReason describes why a branch was skipped during prune.
type SkipReason string

const (
	SkipProtected         SkipReason = "protected"
	SkipCurrentHEAD       SkipReason = "current-head"
	SkipOpenPlan          SkipReason = "open-plan"
	SkipWorktree          SkipReason = "worktree-active"
	SkipUnmerged          SkipReason = "unmerged"
	SkipActiveCommand     SkipReason = "active-command"
	SkipGitHubProtected   SkipReason = "github-protected"
	SkipRemoteAlreadyGone SkipReason = "remote-already-gone"
	// PR-state classes for branches the merge OR-gate refused (B-0348). They
	// subdivide what used to be one flat SkipUnmerged; SkipUnmerged remains the
	// fallback for when the PR index is unavailable.
	SkipInFlight     SkipReason = "in-flight"
	SkipStrayTip     SkipReason = "stray-tip"
	SkipNoPR         SkipReason = "no-pr"
	SkipRejectedHeld SkipReason = "rejected-held"
)

// PruneDecision records the outcome for a single branch candidate.
type PruneDecision struct {
	Branch      string
	Merged      bool
	MergeSource MergeSource
	// Rejected marks a branch whose every PR is CLOSED — deliberately rejected
	// work. Not merged, but a deletion candidate by default (B-0348).
	Rejected bool
	// PRClass records how the branch's pull requests classified it. Only set
	// when the merge OR-gate refused the branch.
	PRClass PRClass
	// PRNumbers holds the pull requests that justify PRClass.
	PRNumbers   []int
	Skipped     bool
	SkipReason  SkipReason
	SkipDetail  string // extra context (plan path, worktree path, etc.)
	Tombstone   string // ref written on apply, e.g. refs/tombstones/<branch>
	Deleted     bool
	Error       string
}

// IsMerged checks whether branch is merged into base with no PR index, i.e.
// resolving the PR half of the OR-gate with a per-branch `gh pr list` call.
// Prefer IsMergedWithIndex — this wrapper exists for callers and tests that
// have no batched index to hand.
func IsMerged(branch, base string) (bool, MergeSource, error) {
	return IsMergedWithIndex(branch, base, nil)
}

// IsMergedWithIndex checks whether branch is merged into base.
// It runs two checks (OR gate): git branch --merged AND a merged GitHub PR.
// Returns (merged, source, error). On PR path it additionally verifies the
// merge commit SHA is an ancestor of base (pr-verified). If ancestor check
// fails the function returns (false, "", nil) — treat as unmerged.
//
// When ix is available the PR half is answered from the batched index (one
// upfront fetch for the whole run); otherwise it falls back to the per-branch
// `gh pr list --head <branch>` call. Both paths run the SAME ancestor
// verification — invariant I4 does not depend on where the SHA came from.
//
// Stray tips are refused (I9): even with the merge commit verified in base, a
// branch whose live tip has moved past the head commit GitHub recorded for the
// PR carries work the merge never took, so it is reported as NOT merged and the
// caller classifies it `stray-tip`.
func IsMergedWithIndex(branch, base string, ix *PRIndex) (bool, MergeSource, error) {
	// Fast path: git branch --merged. A tip that is an ancestor of base has
	// nothing outside base by definition, so no stray-tip check is needed here.
	merged, err := isMergedViaGit(branch, base)
	if err == nil && merged {
		return true, MergeSourceGit, nil
	}

	// Slow path: batched PR index, or a per-branch gh call when unavailable.
	var pr PRInfo
	if ix.Available() {
		var found bool
		pr, found = mergedPRFromIndex(ix, branch)
		if !found {
			return false, "", nil
		}
	} else {
		var prErr error
		var found bool
		pr, found, prErr = mergedPRViaGH(branch)
		if prErr != nil || !found {
			return false, "", nil
		}
	}

	// Verify the PR merge commit is an ancestor of base.
	if pr.MergeCommit != "" {
		ancestor, _ := isMergeCommitAncestor(pr.MergeCommit, base)
		if !ancestor {
			// Merge commit present but NOT in base — refuse.
			return false, "", nil
		}
		if hasStrayTip(branch, pr) {
			return false, "", nil
		}
		return true, MergeSourcePRVerified, nil
	}

	// PR merged but we couldn't get the merge SHA — accept without the ancestor
	// check, still refusing a tip that moved past the merged head.
	if hasStrayTip(branch, pr) {
		return false, "", nil
	}
	return true, MergeSourcePR, nil
}

// hasStrayTip reports whether branch's live tip differs from the head commit
// GitHub recorded for its merged PR — i.e. commits were pushed after the merge.
//
// Note this is NOT "tip is not an ancestor of base": under the squash-merge
// workflow this repo uses, a cleanly merged branch's tip is never an ancestor
// of base, so that test would reclassify every squash-merged branch as stray
// and prune would delete nothing. The head-SHA comparison is exact instead.
//
// Fails OPEN (returns false) when either SHA is unknown: an unresolvable tip
// means there is no branch to delete anyway, and an absent head SHA is the
// pre-existing lenient path.
func hasStrayTip(branch string, pr PRInfo) bool {
	if pr.HeadSHA == "" {
		return false
	}
	tip := resolveBranchTip(branch)
	if tip == "" {
		return false
	}
	return tip != pr.HeadSHA
}

// resolveBranchTip returns the commit SHA at the tip of branch, preferring the
// local ref and falling back to origin/<branch>. Empty when neither resolves.
func resolveBranchTip(branch string) string {
	for _, ref := range []string{"refs/heads/" + branch, "refs/remotes/origin/" + branch} {
		out, _, err := gitpkg.Run("git", "rev-parse", "--verify", "--quiet", ref)
		if err == nil {
			if sha := strings.TrimSpace(out); sha != "" {
				return sha
			}
		}
	}
	return ""
}

// isMergedViaGit checks both the remote and local `git branch --merged` listings.
//
// The previous early-return on the remote check missed local-only branches: when
// `git branch -r --merged origin/<base>` succeeded with err == nil, the function
// returned without consulting `git branch --merged <base>`. Local branches whose
// tip is an ancestor of <base> but which have no remote counterpart were silently
// classified as unmerged — see the cc/trusting-driscoll-9bd5e9 case after the
// P-0161 promote, where the local branch was fully merged but never pushed.
//
// We now OR both lists. Either match counts as merged.
func isMergedViaGit(branch, base string) (bool, error) {
	remoteOut, _, remoteErr := gitpkg.Run("git", "branch", "-r", "--merged", "origin/"+base)
	if remoteErr == nil {
		if containsBranch(remoteOut, "origin/"+branch) || containsBranch(remoteOut, branch) {
			return true, nil
		}
	}
	localOut, _, localErr := gitpkg.Run("git", "branch", "--merged", base)
	if localErr == nil {
		if containsBranch(localOut, branch) {
			return true, nil
		}
	}
	// If both calls erred, surface the local error (more relevant — base often
	// exists locally even when origin/base lookup fails on slow networks).
	if remoteErr != nil && localErr != nil {
		return false, localErr
	}
	return false, nil
}

// containsBranch checks if a branch name appears in multi-line git branch output.
func containsBranch(output, branch string) bool {
	for _, line := range strings.Split(output, "\n") {
		name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "* "))
		if name == branch {
			return true
		}
	}
	return false
}

// mergedPRViaGH queries gh pr list for a merged PR matching the branch head.
// This is the per-branch fallback used only when no batched PRIndex is
// available. Returns (pr, found, error); pr.MergeCommit may be empty if gh
// doesn't return it.
func mergedPRViaGH(branch string) (PRInfo, bool, error) {
	out, _, err := gitpkg.Run("gh", "pr", "list",
		"--head", branch,
		"--state", "merged",
		"--json", "mergeCommit,state,headRefOid",
		"--limit", "1",
	)
	if err != nil {
		return PRInfo{}, false, err
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "[]" || trimmed == "" {
		return PRInfo{}, false, nil
	}
	// Extract the fields cheaply without importing encoding/json. The needle for
	// mergeCommit.oid is `"oid":"`, which does NOT match inside `"headRefOid":"`
	// (that has no quote immediately before a lowercase `oid`).
	return PRInfo{
		State:       PRStateMerged,
		HeadRefName: branch,
		MergeCommit: extractJSONField(trimmed, "oid"),
		HeadSHA:     extractJSONField(trimmed, "headRefOid"),
	}, true, nil
}

// extractJSONField extracts a string value for key from a simple JSON blob.
// Used to avoid importing encoding/json for a single field.
func extractJSONField(jsonStr, key string) string {
	needle := `"` + key + `":"`
	idx := strings.Index(jsonStr, needle)
	if idx < 0 {
		return ""
	}
	rest := jsonStr[idx+len(needle):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// isMergeCommitAncestor checks if sha is an ancestor of base (i.e., reachable from base).
func isMergeCommitAncestor(sha, base string) (bool, error) {
	_, _, err := gitpkg.Run("git", "merge-base", "--is-ancestor", sha, base)
	return err == nil, nil
}

// IsProtected reports whether branch should never be deleted.
// Checks: default protected set, .kaisser.yml permanent_branches, and well-known names.
func IsProtected(branch string) (bool, string) {
	protected := config.ReadPermanentBranches()
	for _, p := range protected {
		if branch == p {
			return true, fmt.Sprintf("permanent branch (matches %q)", p)
		}
	}
	// Belt-and-suspenders for common names not in the config.
	switch branch {
	case "main", "master", "homolog", "staging", "develop", "production":
		return true, fmt.Sprintf("well-known permanent branch %q", branch)
	}
	return false, ""
}

// IsCurrentHEAD reports whether branch is the currently checked-out branch.
func IsCurrentHEAD(branch string) (bool, error) {
	out, _, err := gitpkg.Run("git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == branch, nil
}

// HasOpenPlanRef checks whether a plan file in .planning/ references branch in its
// frontmatter (branch: <name>) and is in an open state (approved, reviewed, or in-progress).
// Returns (true, planPath) on first match.
func HasOpenPlanRef(branch string) (bool, string) {
	patterns := []string{
		".planning/*-approved.md",
		".planning/*-reviewed.md",
		".planning/*-in-progress.md",
	}
	for _, pat := range patterns {
		matches, err := filepath.Glob(pat)
		if err != nil {
			continue
		}
		for _, m := range matches {
			if planReferencingBranch(m, branch) {
				return true, m
			}
		}
	}
	return false, ""
}

// planReferencingBranch reads planPath and checks YAML frontmatter for branch: <name>.
// Canonical YAML frontmatter starts at byte 0 of the file with "---" on its own line —
// require that strict form so a "---" inside a code block doesn't accidentally yield
// a fake frontmatter window. Accept an optional leading BOM and CRLF line endings.
func planReferencingBranch(planPath, branch string) bool {
	data, err := os.ReadFile(planPath)
	if err != nil {
		return false
	}
	content := strings.TrimPrefix(string(data), "\uFEFF")
	if !(strings.HasPrefix(content, "---\n") || strings.HasPrefix(content, "---\r\n")) {
		return false
	}
	// Skip the opening "---" line.
	body := content[3:]
	body = strings.TrimLeft(body, "\r\n")
	// Find the closing "---" on its own line.
	end := strings.Index(body, "\n---")
	if end < 0 {
		return false
	}
	frontmatter := body[:end]
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "branch:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "branch:"))
			val = strings.Trim(val, `"' `)
			if val == branch {
				return true
			}
		}
	}
	return false
}

// WorktreeBranches parses `git worktree list --porcelain` ONCE and returns the
// branch → worktree-path set for every worktree with a checked-out branch
// (detached-HEAD and bare entries carry no `branch` line and are omitted).
// Errors are PROPAGATED, never swallowed — callers must fail CLOSED on error
// (treat the listing as unknown and refuse to delete anything), because an
// invisible worktree is exactly the state that lets prune nuke a live branch.
func WorktreeBranches() (map[string]string, error) {
	out, stderr, err := gitpkg.Run("git", "worktree", "list", "--porcelain")
	if err != nil {
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("git worktree list --porcelain: %s", detail)
	}
	// Parse porcelain output: blocks separated by blank lines.
	set := map[string]string{}
	var currentPath string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			currentPath = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "branch refs/heads/") {
			wBranch := strings.TrimPrefix(line, "branch refs/heads/")
			if wBranch != "" && currentPath != "" {
				set[wBranch] = currentPath
			}
		}
	}
	return set, nil
}

// HasActiveWorktree checks whether branch is currently checked out in any worktree.
// Returns (true, worktreePath, nil) on match. A listing error is propagated —
// callers MUST fail closed (skip the branch, never proceed to deletion) on error.
func HasActiveWorktree(branch string) (bool, string, error) {
	set, err := WorktreeBranches()
	if err != nil {
		return false, "", err
	}
	path, ok := set[branch]
	return ok, path, nil
}

// HasActiveCommand returns true if any kaisser skill is currently running, by checking
// for active-command marker files at /tmp/agent-audit-*/active-command. While a marker
// exists, pruning is unsafe (a skill could be mid-flight on these branches).
// Marker layout matches cli/cmd/active_command.go and audit Rules 5/29.
// The TMPDIR env var is honoured so the check works inside macOS /var/folders/... paths too.
//
// Test-only hatch: setting KAISSER_BRANCH_PRUNE_BYPASS_ACTIVE_CMD=1 short-circuits to
// (false, ""). The variable is intentionally undocumented in user-facing CLI help — it
// exists so unit tests run inside a live Claude Code session (which itself sets the
// marker) don't trip the guard. Production code never sets this.
func HasActiveCommand() (bool, string) {
	if os.Getenv("KAISSER_BRANCH_PRUNE_BYPASS_ACTIVE_CMD") == "1" {
		return false, ""
	}
	dirs := []string{"/tmp", "/private/tmp"}
	if td := os.Getenv("TMPDIR"); td != "" {
		dirs = append(dirs, strings.TrimRight(td, "/"))
	}
	for _, d := range dirs {
		matches, err := filepath.Glob(filepath.Join(d, "agent-audit-*", "active-command"))
		if err != nil {
			continue
		}
		for _, m := range matches {
			if info, statErr := os.Stat(m); statErr == nil && !info.IsDir() {
				return true, m
			}
		}
	}
	return false, ""
}

// IsProtectedOnGitHub queries the GitHub API for branch protection rules.
// Returns (true, "") when the branch has protection rules registered.
// Returns (false, "") for unprotected branches (HTTP 404 → "Branch not protected").
// Network errors, unauthenticated gh, or repos without remote → returns (false, "")
// silently — branch protection is an OPTIONAL extra guard, not a hard requirement.
func IsProtectedOnGitHub(branch string) (bool, string) {
	slug, _, err := gitpkg.Run("gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
	if err != nil {
		return false, ""
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return false, ""
	}
	path := fmt.Sprintf("repos/%s/branches/%s/protection", slug, branch)
	_, stderr, apiErr := gitpkg.Run("gh", "api", "-H", "Accept: application/vnd.github+json", path)
	if apiErr == nil {
		return true, ""
	}
	// 404 with "Branch not protected" message is the expected "no rules" case.
	if strings.Contains(stderr, "Branch not protected") || strings.Contains(stderr, "HTTP 404") {
		return false, ""
	}
	// Any other error (auth failure, network down, repo missing) — treat as unprotected.
	return false, ""
}

// WriteTombstone writes a lightweight ref at refs/tombstones/<branch> pointing to
// the branch tip SHA. This enables recovery for up to 7 days (GCTombstones default).
// The branch name is sanitised — slashes become dashes to form a valid ref component.
func WriteTombstone(branch, sha string) (string, error) {
	if sha == "" {
		// Resolve SHA from branch tip.
		out, _, err := gitpkg.Run("git", "rev-parse", branch)
		if err != nil {
			out, _, err = gitpkg.Run("git", "rev-parse", "origin/"+branch)
			if err != nil {
				return "", fmt.Errorf("cannot resolve branch tip for %q: %w", branch, err)
			}
		}
		sha = strings.TrimSpace(out)
	}

	// Sanitise branch name for use as a ref path component.
	refName := "refs/tombstones/" + strings.ReplaceAll(branch, "/", "-")

	_, _, err := gitpkg.Run("git", "update-ref", refName, sha)
	if err != nil {
		return "", fmt.Errorf("writing tombstone %s: %w", refName, err)
	}
	return refName, nil
}

// GCTombstones removes tombstone refs older than maxAge by reading reflog timestamps.
// When maxAge == 0 the default of 7 days is used.
func GCTombstones(maxAge time.Duration) ([]string, error) {
	return GCTombstonesAt(maxAge, time.Now())
}

// GCTombstonesAt is the testable variant that accepts an explicit "now" timestamp.
func GCTombstonesAt(maxAge time.Duration, now time.Time) ([]string, error) {
	if maxAge == 0 {
		maxAge = 7 * 24 * time.Hour
	}

	// List all refs under refs/tombstones/.
	out, _, err := gitpkg.Run("git", "for-each-ref", "--format=%(refname) %(creatordate:unix)", "refs/tombstones/")
	if err != nil {
		return nil, nil // no tombstones or git not available — silent
	}

	var pruned []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		refName := parts[0]
		epochSecs, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || epochSecs == 0 {
			continue
		}
		refTime := time.Unix(epochSecs, 0)
		age := now.Sub(refTime)
		if age >= maxAge {
			if _, _, delErr := gitpkg.Run("git", "update-ref", "-d", refName); delErr == nil {
				pruned = append(pruned, refName)
			}
		}
	}
	return pruned, nil
}

// reviewStampRe captures the PR number from a .review-stamp-<PR>.json filename.
// Matches both bare-digit forms; the leading "." in ".review-stamp" is part of the
// canonical name written by /finish, /auto-merge, /address-pr, /plan-link-review.
var reviewStampRe = regexp.MustCompile(`\.review-stamp-(\d+)\.json$`)

// PRStateFunc resolves a PR number to its GitHub state ("OPEN", "MERGED", "CLOSED").
// It returns an error when the state cannot be determined (offline, gh missing,
// auth failure, PR not found). Injected into GCReviewStampsWith so the reaper is
// unit-testable without a live network. The production default is ghPRState.
type PRStateFunc func(pr int) (string, error)

// ghPRState shells out to `gh pr view <n> --json state` and returns the state
// string ("OPEN", "MERGED", "CLOSED"). Any gh failure (offline, unauthenticated,
// PR not found) is surfaced as an error so the caller can fail-closed.
func ghPRState(pr int) (string, error) {
	out, stderr, err := gitpkg.Run("gh", "pr", "view", strconv.Itoa(pr), "--json", "state", "-q", ".state")
	if err != nil {
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("gh pr view %d: %s", pr, detail)
	}
	return strings.ToUpper(strings.TrimSpace(out)), nil
}

// GCReviewStamps reaps orphaned .planning/.review-stamp-<PR>.json files whose PR
// has already landed (MERGED or CLOSED). Only /finish deletes its own stamp today;
// /auto-merge and /address-pr leak theirs, so they accumulate as orphans mapping to
// already-closed PRs. This sweep, wired into `kaisser branch prune --gc`, clears them.
//
// Decision policy:
//   - state == MERGED or CLOSED → reap immediately (no grace period — stamps are
//     one-shot gate artifacts with no recovery value once the PR closes).
//   - state == OPEN → keep (the gate may still need it).
//   - gh lookup error / offline / unparseable filename → FAIL-CLOSED, keep the stamp.
//     Never delete on uncertainty.
//
// Returns the list of reaped file paths (relative to repo root). Globbing errors
// are swallowed silently (returns nil) — consistent with GCTombstones.
func GCReviewStamps() ([]string, error) {
	return GCReviewStampsWith(ghPRState)
}

// GCReviewStampsWith is the testable variant accepting an injectable PR-state lookup.
func GCReviewStampsWith(lookup PRStateFunc) ([]string, error) {
	matches, err := filepath.Glob(".planning/.review-stamp-*.json")
	if err != nil {
		return nil, nil // bad glob pattern — treat as no stamps, silent
	}

	var reaped []string
	for _, path := range matches {
		base := filepath.Base(path)
		m := reviewStampRe.FindStringSubmatch(base)
		if m == nil {
			continue // not a canonical stamp filename — leave it untouched (fail-closed)
		}
		pr, convErr := strconv.Atoi(m[1])
		if convErr != nil {
			continue // unparseable PR number — fail-closed
		}

		state, lookupErr := lookup(pr)
		if lookupErr != nil {
			// FAIL-CLOSED: cannot confirm the PR is done → keep the stamp.
			continue
		}

		if state == "MERGED" || state == "CLOSED" {
			if rmErr := os.Remove(path); rmErr == nil {
				reaped = append(reaped, path)
			}
		}
		// state == OPEN (or any other live state) → keep.
	}
	return reaped, nil
}

// WriteLog appends a structured JSON-like log line to ~/.claude/logs/branch-prune.log.
// Format: [ISO-TS] repo=<name> branch=<name> action=<action> reason=<text> tombstone=<ref>
func WriteLog(repo, branch, action, reason, tombstone string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	logDir := filepath.Join(home, ".claude", "logs")
	if mkErr := os.MkdirAll(logDir, 0755); mkErr != nil {
		return
	}
	logPath := filepath.Join(logDir, "branch-prune.log")

	ts := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf("[%s] repo=%s branch=%s action=%s reason=%s tombstone=%s\n",
		ts, quoteVal(repo), quoteVal(branch), quoteVal(action), quoteVal(reason), quoteVal(tombstone))

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// quoteVal quotes a log field value if it contains spaces.
func quoteVal(v string) string {
	if v == "" {
		return `""`
	}
	if strings.ContainsAny(v, " \t\n") {
		return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
	}
	return v
}

// PruneOpts controls how PruneBranch behaves.
type PruneOpts struct {
	Base     string // base branch to check merge against
	Apply    bool   // false = dry-run (default)
	RepoName string // for log attribution
	// WorktreeBranches is the branch → worktree-path set parsed ONCE by the
	// caller via WorktreeBranches() and shared across the whole candidate loop
	// (runBranchPrune aborts fail-closed when the listing errors). When nil,
	// PruneBranch lists worktrees itself and fails CLOSED on a listing error.
	WorktreeBranches map[string]string
	// PRIndex is every PR in the repo, fetched ONCE by the caller and indexed by
	// head branch (B-0348). When nil or unavailable, the merge check falls back
	// to a per-branch `gh pr list` and PR-state classification is skipped
	// entirely — an unmerged branch then reports the flat `unmerged` reason and
	// is never treated as `rejected`.
	PRIndex *PRIndex
	// ExcludeRejected holds back `rejected` branches (every PR CLOSED) instead
	// of deleting them. Inclusion is the DEFAULT — this is the opt-out.
	ExcludeRejected bool
	// ProtectedRemote is the set of remote-protected branch names fetched ONCE
	// by the caller via ProtectedBranchSet(). A nil map means "not fetched" and
	// PruneBranch falls back to the per-branch IsProtectedOnGitHub call; an
	// empty non-nil map means "fetched, nothing is protected".
	ProtectedRemote map[string]bool
	// RemoteSynced tells PruneBranch the caller already ran SyncRemoteRefs()
	// for this pass, so it can skip the per-branch `git remote prune origin`
	// (one network round-trip per candidate).
	RemoteSynced bool
}

// BranchRef describes where a branch exists. Used by ListAllBranches so the
// prune flow can pick the right deletion strategy:
//   - HasLocal + HasRemote: full delete (git branch -D + git push origin --delete)
//   - HasLocal only: local delete only
//   - HasRemote only: remote delete only (the gap that surfaced in the v3.46.1
//     follow-up — merged PRs that landed without --delete-branch leave dangling
//     origin/<name> refs that local-only ListLocalBranches never saw)
type BranchRef struct {
	Name      string
	HasLocal  bool
	HasRemote bool
}

// isRemoteRefGone queries the remote for a specific ref using ls-remote and
// returns true only when the server is reachable but the ref is absent
// (git ls-remote --exit-code exits 2). A fatal network error (exit 128) or
// any other non-2 exit returns false — we cannot confirm the ref is gone, so
// we let the caller's push attempt surface the real error.
//
// remoteURL may be a named remote (e.g. "origin") or a raw URL/path.
func isRemoteRefGone(refSpec, remoteURL string) bool {
	_, _, lsErr := gitpkg.Run("git", "ls-remote", "--exit-code", remoteURL, refSpec)
	if lsErr == nil {
		return false // exit 0 → ref found
	}
	if exitErr, ok := lsErr.(*exec.ExitError); ok {
		return exitErr.ExitCode() == 2
	}
	return false // non-ExitError (e.g., binary not found) → treat as unknown
}

// branchLocation queries git for a single branch's local/remote presence.
// Cheap: two `show-ref` invocations.
func branchLocation(name string) BranchRef {
	ref := BranchRef{Name: name}
	if _, _, err := gitpkg.Run("git", "show-ref", "--verify", "--quiet", "refs/heads/"+name); err == nil {
		ref.HasLocal = true
	}
	if _, _, err := gitpkg.Run("git", "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+name); err == nil {
		ref.HasRemote = true
	}
	return ref
}

// PruneBranch orchestrates all safety guards and (if Apply) performs deletion.
// Returns a PruneDecision describing what happened. Never returns a hard error —
// errors are embedded in PruneDecision.Error for per-branch reporting.
//
// Works for branches that live locally, on the remote only, or both. The
// current-HEAD guard only applies to local refs; the worktree guard is
// UNCONDITIONAL (consulted for every candidate regardless of classification).
// The delete step picks `git branch -D` and `git push origin --delete`
// independently based on which sides exist.
//
// Guard order (fail-fast, each guard returns early on hit):
//  1. protected branch name (local config + well-known)
//  2. GitHub branch protection rules (network, best-effort)
//  3. currently checked-out HEAD (local refs only)
//  4. open plan reference in .planning/
//  5. active-command marker (a kaisser skill is mid-flight)
//  6. active worktree — UNCONDITIONAL, fail-closed, both modes; teardown owned by /worktree destroy
//  7. merge check (git OR gh pr), then PR-state classification of the refusals
func PruneBranch(branch string, opts PruneOpts) PruneDecision {
	d := PruneDecision{Branch: branch}

	// Pre-prune: sync remote-tracking refs so that stale origin/<name> entries
	// left by server-side deletes don't confuse branchLocation or the merge check.
	// Failure is non-fatal — offline or no-remote repos keep working normally.
	// Skipped when the caller already synced once for the whole pass.
	if !opts.RemoteSynced {
		if syncErr := SyncRemoteRefs(); syncErr != nil {
			WriteLog(opts.RepoName, branch, "warn", "remote-prune-failed: "+syncErr.Error(), "")
		}
	}

	loc := branchLocation(branch)
	if !loc.HasLocal && !loc.HasRemote {
		// Nothing to prune.
		d.Skipped = true
		d.SkipReason = SkipUnmerged
		d.SkipDetail = "branch does not exist locally or on origin"
		return d
	}

	// Guard 1: protected branch name
	if prot, reason := IsProtected(branch); prot {
		d.Skipped = true
		d.SkipReason = SkipProtected
		d.SkipDetail = reason
		WriteLog(opts.RepoName, branch, "skipped", string(SkipProtected)+" "+reason, "")
		return d
	}

	// Guard 2: GitHub branch protection rules — best-effort network check.
	// Answered from the batched ProtectedRemote set when the caller fetched one
	// (nil map = not fetched → per-branch API call, the pre-B-0348 behaviour).
	if ghProt := isProtectedRemote(branch, opts.ProtectedRemote); ghProt {
		d.Skipped = true
		d.SkipReason = SkipGitHubProtected
		d.SkipDetail = "GitHub branch protection rules apply"
		WriteLog(opts.RepoName, branch, "skipped", string(SkipGitHubProtected), "")
		return d
	}

	// Guard 3: currently checked-out HEAD — only meaningful for local refs.
	if loc.HasLocal {
		if head, err := IsCurrentHEAD(branch); head || err != nil {
			d.Skipped = true
			d.SkipReason = SkipCurrentHEAD
			d.SkipDetail = "branch is currently checked out"
			if err != nil {
				d.Error = err.Error()
			}
			WriteLog(opts.RepoName, branch, "skipped", string(SkipCurrentHEAD), "")
			return d
		}
	}

	// Guard 4: open plan reference
	if hasRef, planPath := HasOpenPlanRef(branch); hasRef {
		d.Skipped = true
		d.SkipReason = SkipOpenPlan
		d.SkipDetail = planPath
		WriteLog(opts.RepoName, branch, "skipped", string(SkipOpenPlan)+" "+planPath, "")
		return d
	}

	// Guard 5: active-command marker — a kaisser skill is currently running
	if active, markerPath := HasActiveCommand(); active {
		d.Skipped = true
		d.SkipReason = SkipActiveCommand
		d.SkipDetail = markerPath
		WriteLog(opts.RepoName, branch, "skipped", string(SkipActiveCommand)+" "+markerPath, "")
		return d
	}

	// Guard 6: active worktree — a branch checked out in any worktree is OFF-LIMITS,
	// UNCONDITIONALLY. The set is consulted for every candidate — the old
	// `loc.HasLocal` gate let a remote-only misclassification bypass this guard.
	// prune never removes worktrees or deletes a worktree-backed branch (neither
	// the local ref nor the remote ref); teardown is owned solely by /worktree
	// destroy (the sanctioned `kaisser worktree cleanup <path>` path does the
	// proper Herd unlink + cert + Redis teardown that prune cannot). Once
	// /worktree destroy has removed the worktree and local branch, the remaining
	// remote-only ref no longer appears in the set and a later pass prunes it.
	// Skip applies in BOTH dry-run and --apply modes, even when merged to base.
	// FAIL-CLOSED: if the worktree listing cannot be obtained, skip — never
	// proceed to deletion blind.
	wtSet := opts.WorktreeBranches
	if wtSet == nil {
		var wtErr error
		wtSet, wtErr = WorktreeBranches()
		if wtErr != nil {
			d.Skipped = true
			d.SkipReason = SkipWorktree
			d.SkipDetail = "worktree listing failed (fail-closed): " + wtErr.Error()
			WriteLog(opts.RepoName, branch, "skipped", string(SkipWorktree)+" listing-error", "")
			return d
		}
	}
	if wtPath, held := wtSet[branch]; held {
		d.Skipped = true
		d.SkipReason = SkipWorktree
		d.SkipDetail = wtPath + " — active worktree; teardown via /worktree destroy (kaisser worktree cleanup)"
		WriteLog(opts.RepoName, branch, "skipped", string(SkipWorktree)+" "+wtPath, "")
		return d
	}

	// Merge check: OR-gate (git --merged OR a merged PR from the index)
	merged, src, err := IsMergedWithIndex(branch, opts.Base, opts.PRIndex)
	if err != nil {
		d.Error = fmt.Sprintf("merge check failed: %v", err)
		WriteLog(opts.RepoName, branch, "skipped", "merge-check-error: "+d.Error, "")
		return d
	}

	// Classify the refusals by PR state. `rejected` is the only class that
	// continues to the deletion path; every other class ends the flow here.
	if !merged {
		if !classifyAndMaybeReject(&d, opts) {
			return d
		}
	} else {
		d.Merged = true
		d.MergeSource = src
	}

	reason := d.eligibilityReason()
	if !opts.Apply {
		WriteLog(opts.RepoName, branch, "dry-run", reason, "")
		return d
	}

	// Write tombstone before deletion. WriteTombstone resolves the SHA from
	// either refs/heads/<name> or refs/remotes/origin/<name>, so remote-only
	// branches get a recoverable ref too.
	tombRef, tombErr := WriteTombstone(branch, "")
	if tombErr != nil {
		d.Error = fmt.Sprintf("tombstone write failed: %v", tombErr)
		WriteLog(opts.RepoName, branch, "skipped", "tombstone-error: "+d.Error, "")
		return d
	}
	d.Tombstone = tombRef

	// Local delete — only if there's a local ref.
	if loc.HasLocal {
		if _, _, delErr := gitpkg.Run("git", "branch", "-D", branch); delErr != nil {
			d.Error = fmt.Sprintf("local delete failed: %v", delErr)
			WriteLog(opts.RepoName, branch, "skipped", "delete-error: "+d.Error, tombRef)
			return d
		}
	}

	// Remote delete — only if there's a remote ref. For remote-only branches
	// this is the SOLE delete step and must succeed; for local+remote it's a
	// follow-up. Treat remote-delete failure as hard for remote-only refs
	// (otherwise the branch never actually leaves the remote — the user-visible
	// bug we're fixing here).
	if loc.HasRemote {
		// ls-remote pre-check: verify the ref still exists on the server before
		// attempting the push --delete. If it's already gone (another CI job,
		// GitHub auto-delete-branch, a concurrent prune run) we classify the
		// outcome as [SKIP]/remote-already-gone rather than [ERROR].
		//
		// isRemoteRefGone returns true only on exit-code 2 ("ref absent on reachable
		// server"); fatal network errors (exit 128) return false so the push attempt
		// can surface the real error.
		if isRemoteRefGone("refs/heads/"+branch, "origin") {
			d.Skipped = true
			d.SkipReason = SkipRemoteAlreadyGone
			d.SkipDetail = "remote ref refs/heads/" + branch + " already absent on origin"
			d.Deleted = loc.HasLocal // local half was deleted above; partial success
			WriteLog(opts.RepoName, branch, "skipped", string(SkipRemoteAlreadyGone), tombRef)
			return d
		}

		_, pushStderr, pushErr := gitpkg.Run("git", "push", "origin", "--delete", branch)
		if pushErr != nil {
			if !loc.HasLocal {
				d.Error = fmt.Sprintf("remote delete failed: %v (%s)", pushErr, strings.TrimSpace(pushStderr))
				WriteLog(opts.RepoName, branch, "error", "remote-delete-error: "+d.Error, tombRef)
				return d
			}
			// Local already deleted; remote-delete failure logged but not fatal.
			WriteLog(opts.RepoName, branch, "warn", "remote-delete-failed: "+strings.TrimSpace(pushStderr), tombRef)
		}
	}

	d.Deleted = true
	WriteLog(opts.RepoName, branch, "deleted", reason, tombRef)
	return d
}

// isProtectedRemote answers Guard 2 from the batched protected-branch set when
// the caller fetched one, and from the per-branch API call otherwise. A nil set
// means "not fetched" — an EMPTY non-nil set legitimately means "the remote has
// no protected branches" and must not trigger the fallback.
func isProtectedRemote(branch string, set map[string]bool) bool {
	if set != nil {
		return set[branch]
	}
	prot, _ := IsProtectedOnGitHub(branch)
	return prot
}

// classifyAndMaybeReject runs PR-state classification on a branch the merge
// OR-gate refused, records the class on d, and reports whether the branch
// continues to the deletion path.
//
// Only PRClassRejected continues (and only when --exclude-rejected is off).
// Every other class — including PRClassUnknown, which means the PR index was
// unavailable — writes a skip onto d and returns false. Absence of information
// NEVER becomes a deletion.
func classifyAndMaybeReject(d *PruneDecision, opts PruneOpts) bool {
	class, nums := PRClassUnknown, []int(nil)
	if opts.PRIndex.Available() {
		class, nums = ClassifyPRs(opts.PRIndex.For(d.Branch))
	}
	d.PRClass = class
	d.PRNumbers = nums

	prs := FormatPRNumbers(nums)
	switch class {
	case PRClassRejected:
		if opts.ExcludeRejected {
			d.Skipped = true
			d.SkipReason = SkipRejectedHeld
			d.SkipDetail = "all PRs closed (" + prs + ") — held by --exclude-rejected"
			WriteLog(opts.RepoName, d.Branch, "skipped", string(SkipRejectedHeld)+" "+prs, "")
			return false
		}
		d.Rejected = true
		return true

	case PRClassInFlight:
		d.Skipped = true
		d.SkipReason = SkipInFlight
		d.SkipDetail = "open PR " + prs
		WriteLog(opts.RepoName, d.Branch, "skipped", string(SkipInFlight)+" "+prs, "")

	case PRClassStrayTip:
		d.Skipped = true
		d.SkipReason = SkipStrayTip
		d.SkipDetail = "PR " + prs + " merged, tip ahead of " + opts.Base
		WriteLog(opts.RepoName, d.Branch, "skipped", string(SkipStrayTip)+" "+prs, "")

	case PRClassNoPR:
		d.Skipped = true
		d.SkipReason = SkipNoPR
		d.SkipDetail = "possible local-only work"
		WriteLog(opts.RepoName, d.Branch, "skipped", string(SkipNoPR), "")

	default: // PRClassUnknown — no PR index, so nothing can be claimed.
		d.Skipped = true
		d.SkipReason = SkipUnmerged
		d.SkipDetail = fmt.Sprintf("not merged into %s", opts.Base)
		WriteLog(opts.RepoName, d.Branch, "skipped", string(SkipUnmerged), "")
	}
	return false
}

// eligibilityReason renders the log/report reason for a branch that reached the
// deletion path — either merged, or rejected-by-PR-state.
func (d *PruneDecision) eligibilityReason() string {
	if d.Rejected {
		return FormatRejectedSource(d.PRNumbers)
	}
	return fmt.Sprintf("merged-via-%s", d.MergeSource)
}

// ListAllBranches returns local branches AND remote-only branches (origin/<name>
// with no matching local ref) deduped into a single sorted slice, excluding
// the current HEAD and the `origin/HEAD` symbolic ref.
//
// Existed remote-only refs are reachable via `refs/remotes/origin/<name>` — they
// surface here so prune can act on stale remote branches whose merged PRs landed
// without --delete-branch (the v3.46.1 follow-up bug class).
func ListAllBranches() ([]string, error) {
	headOut, _, _ := gitpkg.Run("git", "rev-parse", "--abbrev-ref", "HEAD")
	head := strings.TrimSpace(headOut)

	seen := map[string]bool{}
	var branches []string

	// Local.
	localOut, _, localErr := gitpkg.Run("git", "branch", "--format=%(refname:short)")
	if localErr == nil {
		for _, line := range strings.Split(localOut, "\n") {
			name := strings.TrimSpace(line)
			if name == "" || name == head || seen[name] {
				continue
			}
			seen[name] = true
			branches = append(branches, name)
		}
	}

	// Remote-only: walk `git branch -r --format='%(refname:short)'` and strip
	// the `origin/` prefix. Skip origin/HEAD (symbolic ref to default branch).
	remoteOut, _, remoteErr := gitpkg.Run("git", "branch", "-r", "--format=%(refname:short)")
	if remoteErr == nil {
		for _, line := range strings.Split(remoteOut, "\n") {
			name := strings.TrimSpace(line)
			if name == "" {
				continue
			}
			// Skip "origin/HEAD -> origin/main" form (when format includes ->)
			if strings.Contains(name, "->") {
				continue
			}
			// Only consider origin/* — other remotes are out of scope for prune.
			if !strings.HasPrefix(name, "origin/") {
				continue
			}
			short := strings.TrimPrefix(name, "origin/")
			if short == "HEAD" || short == head || seen[short] {
				continue
			}
			seen[short] = true
			branches = append(branches, short)
		}
	}

	// If both failed, surface the local error (more relevant — most repos have a
	// local ref to enumerate even when offline).
	if localErr != nil && remoteErr != nil {
		return nil, localErr
	}
	sort.Strings(branches)
	return branches, nil
}

// ListLocalBranches returns all local branch names, excluding the current HEAD.
// Kept for backwards compatibility — new code should use ListAllBranches.
func ListLocalBranches() ([]string, error) {
	out, _, err := gitpkg.Run("git", "branch", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	headOut, _, _ := gitpkg.Run("git", "rev-parse", "--abbrev-ref", "HEAD")
	head := strings.TrimSpace(headOut)

	var branches []string
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || name == head {
			continue
		}
		branches = append(branches, name)
	}
	return branches, nil
}
