// Package github — stamp.go
//
// WriteReviewStamp writes (or refreshes, or skips idempotently) a review-stamp
// JSON file at .planning/.review-stamp-<PR>.json. The schema is canonical and
// matches the inline jq patterns used by /auto-merge Step 3E and /local-review.
//
// Stamp schema:
//
//	{
//	  "pr": <int>,
//	  "plan_id": "<string>",
//	  "reviewed_at": "<RFC3339>",
//	  "reviewer_verdict": "approved|changes-requested|unclear",
//	  "commit_sha": "<string>",
//	  "bypass": false,
//	  "source": "github"
//	}
//
// The file is written to <repoRoot>/.planning/.review-stamp-<PR>.json.
//
// Commit-sha-keyed refresh: when the stamp file already exists, WriteReviewStamp
// compares its recorded commit_sha against the current git HEAD SHA.
//   - Same SHA → the stamp still describes HEAD; the write is a silent no-op
//     (Skipped: true).
//   - Different SHA (or the existing file is unreadable/corrupt) → the stamp is
//     stale and is overwritten in place with fresh data (Written: true,
//     Refreshed: true on the differing-SHA path).
//
// ResolvePlanID resolves the plan ID for the stamp:
//  1. Read `id:` frontmatter field from the active plan file
//  2. Fallback: derive from git branch slug
//  3. Last resort: empty string with a stderr warning
package github

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// reviewStamp is the canonical stamp schema shared with /auto-merge and /local-review.
type reviewStamp struct {
	PR              int    `json:"pr"`
	PlanID          string `json:"plan_id"`
	ReviewedAt      string `json:"reviewed_at"`
	ReviewerVerdict string `json:"reviewer_verdict"`
	CommitSHA       string `json:"commit_sha"`
	Bypass          bool   `json:"bypass"`
	Source          string `json:"source"`
}

// StampResult describes the outcome of a WriteReviewStamp call.
type StampResult struct {
	// Path is the absolute path of the stamp file (written or already present).
	Path string
	// Written is true when the stamp was newly written to disk — either because
	// no stamp existed yet, or because an existing stamp was refreshed (stale
	// commit_sha). Callers that only care "is there fresh data on disk now"
	// should keep checking Written; Refreshed distinguishes the two cases.
	Written bool
	// Skipped is true when an existing stamp's commit_sha already matched the
	// current HEAD and the write was a no-op.
	Skipped bool
	// Refreshed is true when an existing stamp was overwritten in place because
	// its commit_sha no longer matched HEAD (or the existing file was
	// unreadable/corrupt). Written is also true in this case.
	Refreshed bool
}

// WriteReviewStamp writes (or refreshes) a real review stamp at
// <repoRoot>/.planning/.review-stamp-<prNumber>.json.
//
// When the stamp file already exists, WriteReviewStamp compares its recorded
// commit_sha against the current git HEAD SHA: a match means the stamp still
// describes HEAD and the write is a silent no-op (Skipped: true); a mismatch
// (or an unreadable/corrupt existing file) means the stamp is stale and gets
// overwritten in place with fresh data (Written: true, Refreshed: true).
//
// prNumber must be a non-empty decimal string (e.g. "202").
// verdict must be one of "approved", "changes-requested", "unclear".
// repoRoot may be "" to use the current working directory.
func WriteReviewStamp(prNumber, verdict, repoRoot string) (StampResult, error) {
	if prNumber == "" {
		return StampResult{}, fmt.Errorf("prNumber is required")
	}

	// Resolve repo root: use explicit arg, then git, then cwd.
	root := repoRoot
	if root == "" {
		root = gitRepoRoot()
	}

	planningDir := filepath.Join(root, ".planning")
	stampPath := filepath.Join(planningDir, fmt.Sprintf(".review-stamp-%s.json", prNumber))

	commitSHA := gitHeadSHA()

	// Commit-sha-keyed refresh: if a stamp already exists, only skip when its
	// commit_sha still matches HEAD. A stale or unreadable/corrupt stamp falls
	// through to the write path below (fail open toward fresh data).
	isRefresh := false
	if existing, err := os.ReadFile(stampPath); err == nil {
		var prev reviewStamp
		if jsonErr := json.Unmarshal(existing, &prev); jsonErr == nil {
			if prev.CommitSHA == commitSHA {
				fmt.Fprintf(os.Stderr, "stamp already present at %s and matches HEAD (%s) — skipping write\n", stampPath, commitSHA)
				return StampResult{Path: stampPath, Written: false, Skipped: true}, nil
			}
			// commit_sha differs from HEAD — stale, refresh it.
			isRefresh = true
		} else {
			// Existing stamp is unreadable JSON — treat as stale and refresh.
			fmt.Fprintf(os.Stderr, "stamp at %s is corrupt/unreadable (%v) — refreshing\n", stampPath, jsonErr)
			isRefresh = true
		}
	}

	// Ensure .planning/ exists.
	if err := os.MkdirAll(planningDir, 0o755); err != nil {
		return StampResult{}, fmt.Errorf("create .planning dir: %w", err)
	}

	// Build stamp fields.
	prInt := 0
	fmt.Sscanf(prNumber, "%d", &prInt)

	planID := ResolvePlanID(root)
	reviewedAt := time.Now().UTC().Format(time.RFC3339)

	stamp := reviewStamp{
		PR:              prInt,
		PlanID:          planID,
		ReviewedAt:      reviewedAt,
		ReviewerVerdict: verdict,
		CommitSHA:       commitSHA,
		Bypass:          false,
		Source:          "github",
	}

	data, err := json.MarshalIndent(stamp, "", "  ")
	if err != nil {
		return StampResult{}, fmt.Errorf("marshal stamp: %w", err)
	}

	if err := os.WriteFile(stampPath, append(data, '\n'), 0o644); err != nil {
		return StampResult{}, fmt.Errorf("write stamp file: %w", err)
	}

	return StampResult{Path: stampPath, Written: true, Skipped: false, Refreshed: isRefresh}, nil
}

// ResolvePlanID returns the plan ID string for use in the stamp.
//
// Resolution order:
//  1. Read `id:` frontmatter field from the active plan file (located via
//     git branch name matching .planning/ files).
//  2. Derive from the current git branch slug (e.g.
//     "improvement/autonomous-pipeline-review-bundle" →
//     "P-0136-improvement-autonomous-pipeline-review-bundle" if a matching
//     .planning/ file exists, else the slug as-is).
//  3. Last resort: empty string with a stderr warning.
func ResolvePlanID(repoRoot string) string {
	root := repoRoot
	if root == "" {
		root = gitRepoRoot()
	}
	planningDir := filepath.Join(root, ".planning")

	// Priority 1: scan .planning/ for an active plan file, read id: frontmatter.
	branch := gitCurrentBranch()
	if id := planIDFromBranchFile(planningDir, branch); id != "" {
		return id
	}

	// Priority 2: derive P-NNNN-slug from branch if a matching file exists.
	if id := planIDFromBranchSlug(planningDir, branch); id != "" {
		return id
	}

	// Priority 3: empty string with warning.
	fmt.Fprintf(os.Stderr, "warning: could not resolve plan_id for stamp — using empty string\n")
	return ""
}

// planIDFromBranchFile finds the active plan file for the branch and reads
// its `id:` frontmatter field.
func planIDFromBranchFile(planningDir, branch string) string {
	entries, err := os.ReadDir(planningDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(planningDir, e.Name())
		if id := readFrontmatterID(path, branch); id != "" {
			return id
		}
	}
	return ""
}

// readFrontmatterID reads the `id:` frontmatter field from a YAML front matter
// block. It also checks that the plan's `branch:` field matches (if present)
// to avoid picking up a wrong plan when multiple plans are present.
func readFrontmatterID(path, branch string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var (
		inFM      bool
		idVal     string
		branchVal string
	)

	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	lines := strings.Split(string(buf[:n]), "\n")

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if !inFM {
			if line == "---" {
				inFM = true
			}
			continue
		}
		if line == "---" {
			break
		}
		if strings.HasPrefix(line, "id:") {
			idVal = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
			idVal = strings.Trim(idVal, "\"'")
		}
		if strings.HasPrefix(line, "branch:") {
			branchVal = strings.TrimSpace(strings.TrimPrefix(line, "branch:"))
			branchVal = strings.Trim(branchVal, "\"'")
		}
	}

	if idVal == "" {
		return ""
	}
	// If the file has a branch: field, it must match.
	if branchVal != "" && branch != "" && branchVal != branch {
		return ""
	}
	return idVal
}

// planIDFromBranchSlug tries to match the branch name against .planning/ filenames.
// Returns the stem of the first matching file (which is the plan ID slug).
var planFileRe = regexp.MustCompile(`^([A-Z]-\d{4}-[^.]+)`)

func planIDFromBranchSlug(planningDir, branch string) string {
	if branch == "" {
		return ""
	}
	// Strip common branch prefixes to get the slug.
	slug := branch
	for _, pfx := range []string{"improvement/", "feature/", "fix/", "feat/", "chore/"} {
		slug = strings.TrimPrefix(slug, pfx)
	}

	entries, err := os.ReadDir(planningDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".md")
		// Strip status suffixes.
		for _, sfx := range []string{"-approved", "-todo", "-doing", "-done", "-in-progress", "-wip"} {
			stem = strings.TrimSuffix(stem, sfx)
		}
		// Check if the stem contains the slug.
		if strings.Contains(stem, slug) {
			return stem
		}
	}
	return ""
}

// gitRepoRoot returns the git working tree root (or "." on error).
func gitRepoRoot() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "."
	}
	return strings.TrimSpace(string(out))
}

// gitCurrentBranch returns the current git branch name (or "" on error).
func gitCurrentBranch() string {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitHeadSHA returns the current HEAD commit SHA (or "unknown" on error).
func gitHeadSHA() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
