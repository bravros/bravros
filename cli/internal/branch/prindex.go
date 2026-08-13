package branch

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	gitpkg "github.com/bravros/bravros/cli/internal/git"
)

// PRState is the normalised GitHub pull-request state. The REST API only ever
// reports "open" or "closed"; a merged PR is a closed PR with a non-null
// merged_at, so the fetch normalises that third state explicitly.
type PRState string

const (
	PRStateOpen   PRState = "OPEN"
	PRStateMerged PRState = "MERGED"
	PRStateClosed PRState = "CLOSED"
)

// PRInfo is the slice of pull-request metadata the prune flow needs.
type PRInfo struct {
	Number      int
	State       PRState
	HeadRefName string
	MergeCommit string // only meaningful when State == PRStateMerged
	// HeadSHA is the head commit GitHub recorded for the PR — for a merged PR,
	// the commit that was merged. Comparing it against the live branch tip is
	// what separates a cleanly squash-merged branch (tip unchanged) from a
	// stray tip (commits pushed after the merge).
	HeadSHA string
}

// PRIndex is ONE upfront fetch of every pull request in the repository, indexed
// by head branch name. It replaces the per-branch `gh pr list --head <branch>`
// calls that made prune O(branches) network round-trips (B-0348).
//
// Availability matters as much as content: a branch that the index says has no
// PR is only trustworthy when the fetch actually succeeded. When the fetch
// fails, Available() reports false and every caller must degrade to the
// conservative pre-index behaviour — never to "this branch has no PR".
type PRIndex struct {
	byHead    map[string][]PRInfo
	available bool
	total     int
}

// Available reports whether the batch fetch succeeded. A false value means the
// index carries no information at all — callers MUST NOT read absence of a
// branch as "this branch has no PR".
func (ix *PRIndex) Available() bool { return ix != nil && ix.available }

// Total returns how many pull requests the index holds.
func (ix *PRIndex) Total() int {
	if ix == nil {
		return 0
	}
	return ix.total
}

// For returns every PR whose head ref is branch, newest PR number first.
// Returns nil for an unavailable index or an unknown branch.
func (ix *PRIndex) For(branch string) []PRInfo {
	if !ix.Available() {
		return nil
	}
	return ix.byHead[branch]
}

// NewPRIndex builds an available index from an already-materialised PR slice.
// Exported for tests and for callers that source PR metadata elsewhere.
func NewPRIndex(prs []PRInfo) *PRIndex {
	ix := &PRIndex{byHead: map[string][]PRInfo{}, available: true, total: len(prs)}
	for _, pr := range prs {
		if pr.HeadRefName == "" {
			continue
		}
		ix.byHead[pr.HeadRefName] = append(ix.byHead[pr.HeadRefName], pr)
	}
	for head := range ix.byHead {
		list := ix.byHead[head]
		sort.Slice(list, func(i, j int) bool { return list[i].Number > list[j].Number })
	}
	return ix
}

// UnavailablePRIndex returns an index that reports Available() == false. Used
// when the batch fetch fails so callers keep a non-nil value to pass around.
func UnavailablePRIndex() *PRIndex { return &PRIndex{} }

// prListJQ filters the REST pull-request payload down to the four fields prune
// needs. Two normalisations happen here rather than in Go:
//
//   - state: REST reports open/closed only, so merged_at promotes a closed PR
//     to "MERGED".
//   - fork PRs are dropped (head.repo.full_name != base.repo.full_name). A fork
//     branch called `main` or `fix-thing` would otherwise index against a
//     same-named local branch and misclassify it.
const prListJQ = `.[] | select(.head.repo != null and .head.repo.full_name == .base.repo.full_name) | ` +
	`{number: .number, state: (if .merged_at != null then "MERGED" else (.state | ascii_upcase) end), ` +
	`head: .head.ref, mergeCommit: (.merge_commit_sha // ""), headSha: (.head.sha // "")}`

// BuildPRIndex fetches every pull request in the current repository in one
// paged `gh api --paginate` call and indexes it by head branch.
//
// REST + --paginate is used rather than `gh pr list --limit N` because the
// latter silently truncates at its limit — a repo with more PRs than the limit
// would hand back a partial index, and a missing PR reads as "no PR", which is
// exactly the misclassification the index exists to eliminate. --paginate walks
// Link headers to exhaustion, so a successful fetch is a complete fetch.
//
// A failed fetch returns an UNAVAILABLE index plus the error; it never returns
// a partial-but-available index.
func BuildPRIndex() (*PRIndex, error) {
	out, stderr, err := gitpkg.Run("gh", "api", "--paginate",
		"-H", "Accept: application/vnd.github+json",
		"repos/{owner}/{repo}/pulls?state=all&per_page=100",
		"--jq", prListJQ,
	)
	if err != nil {
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = err.Error()
		}
		return UnavailablePRIndex(), fmt.Errorf("gh api pulls: %s", detail)
	}
	return ParsePRIndex(out)
}

// prJSON mirrors one line of prListJQ output.
type prJSON struct {
	Number      int    `json:"number"`
	State       string `json:"state"`
	Head        string `json:"head"`
	MergeCommit string `json:"mergeCommit"`
	HeadSHA     string `json:"headSha"`
}

// ParsePRIndex decodes the newline-delimited JSON objects emitted by prListJQ
// into an available index. A decode error yields an UNAVAILABLE index — a
// half-parsed index is indistinguishable from a repo with fewer PRs, and that
// ambiguity is what makes "no PR" unsafe.
func ParsePRIndex(jsonLines string) (*PRIndex, error) {
	dec := json.NewDecoder(strings.NewReader(jsonLines))
	var prs []PRInfo
	for {
		var raw prJSON
		if err := dec.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			return UnavailablePRIndex(), fmt.Errorf("decoding PR index: %w", err)
		}
		prs = append(prs, PRInfo{
			Number:      raw.Number,
			State:       PRState(strings.ToUpper(strings.TrimSpace(raw.State))),
			HeadRefName: raw.Head,
			MergeCommit: raw.MergeCommit,
			HeadSHA:     raw.HeadSHA,
		})
	}
	return NewPRIndex(prs), nil
}

// PRClass classifies a branch that the merge OR-gate refused, by the state of
// the pull requests opened from it.
type PRClass string

const (
	// PRClassRejected — the branch has at least one PR and every one of them is
	// CLOSED (none open, none merged). Deliberately rejected work: pruned BY
	// DEFAULT (operator standing decision, 2026-08-09), opt out with
	// --exclude-rejected.
	PRClassRejected PRClass = "rejected"
	// PRClassInFlight — at least one PR is OPEN. Live work; always kept.
	PRClassInFlight PRClass = "in-flight"
	// PRClassStrayTip — a PR merged but the merge OR-gate still says unmerged,
	// so the tip carries commits beyond the merge. Surfaced for a human; never
	// auto-deleted.
	PRClassStrayTip PRClass = "stray-tip"
	// PRClassNoPR — no PR was ever opened from this branch. Possible local-only
	// work; NEVER auto-deleted.
	PRClassNoPR PRClass = "no-pr"
	// PRClassUnknown — the PR index was unavailable, so nothing can be said.
	// Degrades to the pre-B-0348 flat "unmerged" skip.
	PRClassUnknown PRClass = "unknown"
)

// ClassifyPRs maps a branch's PR set to its class, plus the PR numbers that
// justify the classification (ascending, for stable output).
//
// Precedence is OPEN > MERGED > CLOSED: an open PR means live work regardless
// of how many earlier attempts were closed or merged.
func ClassifyPRs(prs []PRInfo) (PRClass, []int) {
	if len(prs) == 0 {
		return PRClassNoPR, nil
	}
	var open, merged, closed []int
	for _, pr := range prs {
		switch pr.State {
		case PRStateOpen:
			open = append(open, pr.Number)
		case PRStateMerged:
			merged = append(merged, pr.Number)
		case PRStateClosed:
			closed = append(closed, pr.Number)
		}
	}
	switch {
	case len(open) > 0:
		sort.Ints(open)
		return PRClassInFlight, open
	case len(merged) > 0:
		sort.Ints(merged)
		return PRClassStrayTip, merged
	case len(closed) > 0:
		sort.Ints(closed)
		return PRClassRejected, closed
	}
	// Every PR carried an unrecognised state — say nothing rather than guess.
	return PRClassUnknown, nil
}

// FormatRejectedSource renders the `source=` / log attribution for a branch
// classified `rejected`. Single source of truth: the CLI's [CANDIDATE]/[DELETED]
// lines and PruneDecision.eligibilityReason() both render through this, and the
// cmd-level golden tests assert on the exact string.
func FormatRejectedSource(nums []int) string {
	return "rejected (all PRs closed: " + FormatPRNumbers(nums) + ")"
}

// FormatPRNumbers renders PR numbers as "#12, #34" for CLI output and logs.
func FormatPRNumbers(nums []int) string {
	if len(nums) == 0 {
		return ""
	}
	parts := make([]string, 0, len(nums))
	for _, n := range nums {
		parts = append(parts, fmt.Sprintf("#%d", n))
	}
	return strings.Join(parts, ", ")
}

// mergedPRFromIndex returns the newest MERGED PR the index holds for branch.
func mergedPRFromIndex(ix *PRIndex, branch string) (PRInfo, bool) {
	for _, pr := range ix.For(branch) {
		if pr.State == PRStateMerged {
			return pr, true
		}
	}
	return PRInfo{}, false
}

// ProtectedBranchSet fetches, in ONE paged call, the names of every branch the
// remote reports as protected (classic branch protection or a ruleset — GitHub
// sets `protected: true` for both).
//
// This replaces the per-branch `gh repo view` + `gh api .../protection` pair in
// IsProtectedOnGitHub, which cost two network round-trips per candidate. Same
// best-effort semantics as the per-branch check: an error returns a nil map and
// the caller falls back to the per-branch path rather than assuming anything.
func ProtectedBranchSet() (map[string]bool, error) {
	out, stderr, err := gitpkg.Run("gh", "api", "--paginate",
		"-H", "Accept: application/vnd.github+json",
		"repos/{owner}/{repo}/branches?protected=true&per_page=100",
		"--jq", ".[].name",
	)
	if err != nil {
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("gh api protected branches: %s", detail)
	}
	set := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name != "" {
			set[name] = true
		}
	}
	return set, nil
}

// SyncRemoteRefs runs `git remote prune origin` once so stale origin/<name>
// tracking refs left by server-side deletes don't confuse branch classification.
// Hoisted out of the per-branch path (it was one network round-trip per
// candidate); errors are returned for logging but are never fatal.
func SyncRemoteRefs() error {
	if _, stderr, err := gitpkg.Run("git", "remote", "prune", "origin"); err != nil {
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("git remote prune origin: %s", detail)
	}
	return nil
}
