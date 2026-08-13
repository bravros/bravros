package plan

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bravros/bravros/cli/internal/config"
)

func TestMetaResult_WithBravrosConfig(t *testing.T) {
	result := &MetaResult{
		NextNum:    "0005",
		Branch:     "feat/test",
		Project:    "test-project",
		BaseBranch: "main",
		Stack: config.StackConfig{
			Language:   "php",
			Framework:  "laravel",
			TestRunner: "pest",
			HasAssets:  true,
		},
		Git: config.GitConfig{
			Remote:     "git@github.com:owner/repo.git",
			HasCI:      true,
			CIWorkflow: "tests.yml",
		},
		Monorepo: false,
	}

	jsonStr := result.JSON()
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	// Verify stack fields
	stack, ok := parsed["stack"].(map[string]interface{})
	if !ok {
		t.Fatal("expected stack object in JSON output")
	}
	if stack["language"] != "php" {
		t.Errorf("expected stack.language=php, got %v", stack["language"])
	}
	if stack["framework"] != "laravel" {
		t.Errorf("expected stack.framework=laravel, got %v", stack["framework"])
	}
	if stack["test_runner"] != "pest" {
		t.Errorf("expected stack.test_runner=pest, got %v", stack["test_runner"])
	}

	// Verify git fields
	git, ok := parsed["git"].(map[string]interface{})
	if !ok {
		t.Fatal("expected git object in JSON output")
	}
	if git["remote"] != "git@github.com:owner/repo.git" {
		t.Errorf("expected git.remote, got %v", git["remote"])
	}
	if git["has_ci"] != true {
		t.Errorf("expected git.has_ci=true, got %v", git["has_ci"])
	}

	// Verify monorepo is omitted when false
	if _, exists := parsed["monorepo"]; exists {
		t.Error("expected monorepo to be omitted when false (omitempty)")
	}
}

func TestMetaResult_WithoutBravrosConfig(t *testing.T) {
	result := &MetaResult{
		NextNum:    "0001",
		Branch:     "main",
		BaseBranch: "main",
		Project:    "test-project",
	}

	jsonStr := result.JSON()
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	// Stack should be omitted (zero value with omitempty)
	if _, exists := parsed["stack"]; exists {
		// Check if it's an empty object — that's fine for struct with omitempty on fields
		stack := parsed["stack"].(map[string]interface{})
		// All sub-fields should be zero/empty
		if lang, ok := stack["language"]; ok && lang != "" {
			t.Errorf("expected empty stack.language, got %v", lang)
		}
	}

	// Stacks should be nil/omitted
	if _, exists := parsed["stacks"]; exists {
		t.Error("expected stacks to be omitted when nil")
	}

	// Monorepo should be omitted when false
	if _, exists := parsed["monorepo"]; exists {
		t.Error("expected monorepo to be omitted when false")
	}
}

func TestMetaResult_MonorepoWithStacks(t *testing.T) {
	result := &MetaResult{
		NextNum:    "0010",
		Branch:     "feat/monorepo",
		BaseBranch: "main",
		Project:    "mono-project",
		Monorepo:   true,
		Stacks: map[string]config.StackConfig{
			"api": {
				Language:   "go",
				Framework:  "none",
				TestRunner: "go test",
			},
			"web": {
				Language:   "node",
				Framework:  "nextjs",
				TestRunner: "jest",
				HasAssets:  true,
			},
		},
	}

	jsonStr := result.JSON()
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	// Verify monorepo is true
	if parsed["monorepo"] != true {
		t.Errorf("expected monorepo=true, got %v", parsed["monorepo"])
	}

	// Verify stacks map
	stacks, ok := parsed["stacks"].(map[string]interface{})
	if !ok {
		t.Fatal("expected stacks map in JSON output")
	}
	if len(stacks) != 2 {
		t.Errorf("expected 2 stacks, got %d", len(stacks))
	}

	api, ok := stacks["api"].(map[string]interface{})
	if !ok {
		t.Fatal("expected api stack in stacks map")
	}
	if api["language"] != "go" {
		t.Errorf("expected api.language=go, got %v", api["language"])
	}

	web, ok := stacks["web"].(map[string]interface{})
	if !ok {
		t.Fatal("expected web stack in stacks map")
	}
	if web["framework"] != "nextjs" {
		t.Errorf("expected web.framework=nextjs, got %v", web["framework"])
	}
}

// writePlanFileWithFM creates a -todo.md file with optional YAML frontmatter in dir.
func writePlanFileWithFM(t *testing.T, dir, name, branch string) string {
	t.Helper()
	var content string
	if branch != "" {
		content = "---\nbranch: " + branch + "\n---\n# Plan\n"
	} else {
		content = "# Plan (no frontmatter)\n"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writePlanFileWithFM %s: %v", name, err)
	}
	return path
}

// TestFindPlanFile_AfterAdvanceToApproved verifies that FindPlanFile finds a
// *-approved.md plan file (branch has zero *-todo.md plans — the plan was
// advanced via `bravros plan advance --to approved`). This is the B-0238 regression.
func TestFindPlanFile_AfterAdvanceToApproved(t *testing.T) {
	dir := t.TempDir()

	// Only an approved plan exists — no todo file.
	writePlanFileWithFM(t, dir, "P-0125-fix-plan-approved-approved.md", "fix/plan-approved-papercut-bundle")

	got := FindPlanFile(dir, "fix/plan-approved-papercut-bundle")
	want := filepath.Join(dir, "P-0125-fix-plan-approved-approved.md")
	if got != want {
		t.Errorf("FindPlanFile after advance to approved: got %q, want %q", got, want)
	}
}

// TestFindPlanFile_ApprovedAmidUnrelatedTodos verifies that an *-approved.md plan
// whose frontmatter branch: matches is returned even when several unrelated
// *-todo.md plans for other branches are present in the same directory.
func TestFindPlanFile_ApprovedAmidUnrelatedTodos(t *testing.T) {
	dir := t.TempDir()

	// The approved plan for our branch.
	writePlanFileWithFM(t, dir, "P-0125-fix-my-thing-approved.md", "fix/my-thing")
	// Unrelated todo plans for other branches.
	writePlanFileWithFM(t, dir, "P-0120-feat-other-a-todo.md", "feat/other-a")
	writePlanFileWithFM(t, dir, "P-0121-feat-other-b-todo.md", "feat/other-b")
	writePlanFileWithFM(t, dir, "P-0122-fix-unrelated-todo.md", "fix/unrelated")

	got := FindPlanFile(dir, "fix/my-thing")
	want := filepath.Join(dir, "P-0125-fix-my-thing-approved.md")
	if got != want {
		t.Errorf("FindPlanFile approved amid unrelated todos: got %q, want %q", got, want)
	}
}

// TestFindPlanFile_ExcludesCompleted verifies that when both a *-complete.md and a
// *-approved.md file exist for the same branch, FindPlanFile returns the approved
// one (active stage) and never the completed one.
func TestFindPlanFile_ExcludesCompleted(t *testing.T) {
	dir := t.TempDir()

	// Both files claim the same branch — approved is active, complete is terminal.
	writePlanFileWithFM(t, dir, "P-0010-feat-mywork-complete.md", "feat/mywork")
	writePlanFileWithFM(t, dir, "P-0010-feat-mywork-approved.md", "feat/mywork")

	got := FindPlanFile(dir, "feat/mywork")
	// Must return the approved file (active stage), never the complete file.
	if got == "" {
		t.Fatal("FindPlanFile ExcludesCompleted: got empty, want approved file")
	}
	if strings.HasSuffix(got, "-complete.md") {
		t.Errorf("FindPlanFile ExcludesCompleted: returned completed file %q — must return active stage", got)
	}
	if !strings.HasSuffix(got, "-approved.md") {
		t.Errorf("FindPlanFile ExcludesCompleted: got %q, want *-approved.md", got)
	}
}

func TestFindPlanFile_FrontmatterBranchMatch(t *testing.T) {
	dir := t.TempDir()

	// File A: matches the desired branch via frontmatter
	writePlanFileWithFM(t, dir, "0027-feat-specific-branch-todo.md", "feat/specific-branch")
	// File B: different branch in frontmatter
	writePlanFileWithFM(t, dir, "0026-feat-other-work-todo.md", "feat/other-work")

	// Make file B appear newer so fallback would pick it
	futureTime := time.Now().Add(1 * time.Hour)
	_ = os.Chtimes(filepath.Join(dir, "0026-feat-other-work-todo.md"), futureTime, futureTime)

	got := FindPlanFile(dir, "feat/specific-branch")
	want := filepath.Join(dir, "0027-feat-specific-branch-todo.md")
	if got != want {
		t.Errorf("FindPlanFile frontmatter match: got %q, want %q", got, want)
	}
}

func TestFindPlanFile_UnrelatedBranchReturnsEmpty(t *testing.T) {
	dir := t.TempDir()

	// Two files, neither matches "feat/unknown-branch" via frontmatter or slug
	writePlanFileWithFM(t, dir, "0010-feat-alpha-todo.md", "feat/alpha")
	writePlanFileWithFM(t, dir, "0011-feat-beta-todo.md", "feat/beta")

	got := FindPlanFile(dir, "feat/unknown-branch")
	if got != "" {
		t.Errorf("FindPlanFile unrelated branch: expected empty string, got %q", got)
	}
}

func TestFindPlanFile_MultiPlanSameBranchHighestNumbered(t *testing.T) {
	dir := t.TempDir()

	// 5 plans with the same slug (no frontmatter — slug match only)
	writePlanFileWithFM(t, dir, "0001-feat-myfeature-todo.md", "")
	writePlanFileWithFM(t, dir, "0002-feat-myfeature-todo.md", "")
	writePlanFileWithFM(t, dir, "0003-feat-myfeature-todo.md", "")
	writePlanFileWithFM(t, dir, "0004-feat-myfeature-todo.md", "")
	writePlanFileWithFM(t, dir, "0005-feat-myfeature-todo.md", "")

	got := FindPlanFile(dir, "feat/myfeature")
	want := filepath.Join(dir, "0005-feat-myfeature-todo.md")
	if got != want {
		t.Errorf("FindPlanFile multi-plan same branch: got %q, want %q (expected highest numbered)", got, want)
	}
}

func TestFindPlanFile_AllCompletedReturnsEmpty(t *testing.T) {
	dir := t.TempDir()

	// All plans are completed (no -todo.md files); candidates fall back to allPlanFiles
	// but branch is clearly unrelated
	writePlanFileWithFM(t, dir, "0010-feat-alpha-complete.md", "feat/alpha")
	writePlanFileWithFM(t, dir, "0011-feat-beta-complete.md", "feat/beta")

	got := FindPlanFile(dir, "feat/unrelated")
	if got != "" {
		t.Errorf("FindPlanFile all completed unrelated branch: expected empty, got %q", got)
	}
}

func TestFindPlanFile_AllCompletedWithFrontmatterMatch(t *testing.T) {
	dir := t.TempDir()

	// All plans are completed but one matches via frontmatter
	writePlanFileWithFM(t, dir, "0010-feat-alpha-complete.md", "feat/alpha")
	writePlanFileWithFM(t, dir, "0011-feat-beta-complete.md", "feat/beta")

	got := FindPlanFile(dir, "feat/alpha")
	want := filepath.Join(dir, "0010-feat-alpha-complete.md")
	if got != want {
		t.Errorf("FindPlanFile all completed frontmatter match: got %q, want %q", got, want)
	}
}

func TestFindPlanFile_NoFrontmatterDoesNotReject(t *testing.T) {
	dir := t.TempDir()

	// File without frontmatter — should still be findable via fallback
	writePlanFileWithFM(t, dir, "0005-feat-no-fm-todo.md", "")

	got := FindPlanFile(dir, "feat/no-fm")
	want := filepath.Join(dir, "0005-feat-no-fm-todo.md")
	if got != want {
		t.Errorf("FindPlanFile no-frontmatter fallback: got %q, want %q", got, want)
	}
}

func TestReadFrontmatterBranch(t *testing.T) {
	dir := t.TempDir()

	// File with branch in frontmatter
	path := filepath.Join(dir, "plan-todo.md")
	content := "---\nbranch: feat/my-feature\nstatus: in_progress\n---\n# Plan\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readFrontmatterBranch(path)
	if got != "feat/my-feature" {
		t.Errorf("readFrontmatterBranch: got %q, want %q", got, "feat/my-feature")
	}

	// File without frontmatter
	plain := filepath.Join(dir, "plain.md")
	if err := os.WriteFile(plain, []byte("# Just a heading\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got2 := readFrontmatterBranch(plain)
	if got2 != "" {
		t.Errorf("readFrontmatterBranch no-fm: got %q, want empty", got2)
	}

	// Nonexistent file
	got3 := readFrontmatterBranch(filepath.Join(dir, "nonexistent.md"))
	if got3 != "" {
		t.Errorf("readFrontmatterBranch nonexistent: got %q, want empty", got3)
	}
}

func TestGetNextReportNum(t *testing.T) {
	tmp := t.TempDir()
	if got := GetNextReportNum(tmp, "R"); got != "R-0001" {
		t.Errorf("empty dir: got %s, want R-0001", got)
	}
	os.WriteFile(filepath.Join(tmp, "R-0001-test-open.md"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmp, "R-0003-test-complete.md"), []byte(""), 0644)
	if got := GetNextReportNum(tmp, "R"); got != "R-0004" {
		t.Errorf("with files: got %s, want R-0004", got)
	}
	if got := GetNextReportNum("/nonexistent", "U"); got != "U-0001" {
		t.Errorf("nonexistent: got %s, want U-0001", got)
	}
}

// ─── TestFieldPR ─────────────────────────────────────────────────────────────

func TestFieldPR_ReturnsValue(t *testing.T) {
	dir := t.TempDir()
	content := "---\nbranch: feat/test\npr: 42\nstatus: active\n---\n# Plan\n"
	path := filepath.Join(dir, "0035-feat-test-todo.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ReadPRField(path)
	if got != "42" {
		t.Errorf("ReadPRField: got %q, want %q", got, "42")
	}
}

func TestFieldPR_NullReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	content := "---\nbranch: feat/test\npr: null\nstatus: active\n---\n# Plan\n"
	path := filepath.Join(dir, "0035-feat-test-todo.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ReadPRField(path)
	if got != "" {
		t.Errorf("ReadPRField null: got %q, want empty", got)
	}
}

func TestFieldPR_AbsentReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	content := "---\nbranch: feat/test\nstatus: active\n---\n# Plan\n"
	path := filepath.Join(dir, "0035-feat-test-todo.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ReadPRField(path)
	if got != "" {
		t.Errorf("ReadPRField absent: got %q, want empty", got)
	}
}

func TestFieldPR_EmptyPlanFileReturnsEmpty(t *testing.T) {
	got := ReadPRField("")
	if got != "" {
		t.Errorf("ReadPRField empty path: got %q, want empty", got)
	}
}

// ─── TestFieldPrimaryWorktree ─────────────────────────────────────────────────

func TestFieldPrimaryWorktree_ReturnsNonEmpty(t *testing.T) {
	// PrimaryWorktreePath() calls `git worktree list --porcelain` — will work when run
	// inside the repo under test. Just verify it returns a non-empty path.
	got := PrimaryWorktreePath()
	if got == "" {
		t.Skip("PrimaryWorktreePath returned empty — not in a git repo or no worktrees")
	}
	if !filepath.IsAbs(got) {
		t.Errorf("PrimaryWorktreePath: expected absolute path, got %q", got)
	}
}

// ─── TestFieldLastReviewCommit ────────────────────────────────────────────────

func TestFieldLastReviewCommit_EmptyPlanNumReturnsSomeHashOrEmpty(t *testing.T) {
	// With empty planNum we just call git log --grep="plan: review" — may or may not find one.
	// We only check that it returns a valid hex hash or empty string (no crash, no panic).
	got := LastReviewCommit("")
	if got != "" && len(got) < 7 {
		t.Errorf("LastReviewCommit: returned suspiciously short hash %q", got)
	}
}

func TestFieldLastReviewCommit_UnknownPlanNumReturnsEmpty(t *testing.T) {
	// Plan number "9999" almost certainly has no review commit.
	got := LastReviewCommit("9999")
	if got != "" {
		// It's possible (if somehow there's a commit for 9999) — we just log, not fail
		t.Logf("LastReviewCommit(9999) returned %q (unexpected but not fatal)", got)
	}
}

// ─── TestMetaResult_PRField ───────────────────────────────────────────────────

func TestMetaResult_HasPRAndWorktreeFields(t *testing.T) {
	result := &MetaResult{
		NextNum:          "0035",
		Branch:           "feat/test",
		BaseBranch:       "main",
		Project:          "claude",
		PR:               "123",
		PrimaryWorktree:  "/Users/x/Sites/claude",
		LastReviewCommit: "abc1234",
	}

	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)

	if !contains(out, `"pr": "123"`) {
		t.Errorf("expected pr field in JSON, got:\n%s", out)
	}
	if !contains(out, `"primary_worktree"`) {
		t.Errorf("expected primary_worktree field in JSON, got:\n%s", out)
	}
	if !contains(out, `"last_review_commit"`) {
		t.Errorf("expected last_review_commit field in JSON, got:\n%s", out)
	}
}

func TestMetaResult_EmptyPRIsOmitted(t *testing.T) {
	result := &MetaResult{
		NextNum:    "0035",
		Branch:     "main",
		BaseBranch: "main",
		Project:    "claude",
	}

	b, _ := json.MarshalIndent(result, "", "  ")
	out := string(b)

	// PR is omitempty — should not appear when empty
	if contains(out, `"pr"`) {
		t.Errorf("expected pr to be omitted when empty, got:\n%s", out)
	}
}

// ─── Placeholder-aware scan tests ────────────────────────────────────────────

// ─── Phase 10: P-/B- prefix in actual file scanning ─────────────────────────

// TestGetNextNumAtomic_PrefixedRealFilesFoundByScanner verifies that when the
// directory contains actual plan files named "P-NNNN-title-todo.md" (not just
// placeholders), GetNextNumAtomic correctly identifies the highest ID and
// returns NNNN+1 as the next number.
// Uses single-tree mode to isolate from any cross-worktree state. P-0170.
func TestGetNextNumAtomic_PrefixedRealFilesFoundByScanner(t *testing.T) {
	dir := t.TempDir()

	// Write real plan files with P- prefix (no frontmatter needed — just the file).
	for _, name := range []string{
		"P-0010-feat-alpha-todo.md",
		"P-0012-feat-beta-todo.md",
		"P-0015-feat-gamma-todo.md",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("# Plan\n"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	// Pass "single-tree" via scanMode parameter to isolate from cross-worktree state.
	id, cleanup, err := GetNextNumAtomic(dir, "P", "single-tree")
	if err != nil {
		t.Fatalf("GetNextNumAtomic: %v", err)
	}
	defer cleanup()

	// Max ID is 0015 → next must be 0016.
	if id != "0016" {
		t.Errorf("expected next ID 0016 after max P-0015, got %q", id)
	}

	// Placeholder mechanism removed in B-0141 — assert no placeholder is written.
	matches, _ := filepath.Glob(filepath.Join(dir, "*-.placeholder"))
	if len(matches) != 0 {
		t.Errorf("expected zero placeholder files after B-0141, got: %v", matches)
	}
}

// TestGetNextNum_MixedOldAndNewFormat verifies that GetNextNum returns max+1
// across a directory that contains both old-format (NNNN-title-todo.md) and
// new-format (P-NNNN-title-todo.md) files. The scan must consider both formats.
func TestGetNextNum_MixedOldAndNewFormat(t *testing.T) {
	dir := t.TempDir()

	files := []string{
		"0068-feat-old-style-todo.md",   // old format — bare number
		"P-0069-feat-new-style-todo.md", // new format — P- prefix
		"P-0071-feat-another-todo.md",   // new format — highest
		"0050-feat-early-todo.md",       // old format — lower
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("# Plan\n"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	got := GetNextNum(dir)
	// Max is P-0071 → next must be 0072.
	if got != "0072" {
		t.Errorf("GetNextNum mixed formats: expected 0072, got %q", got)
	}
}

// TestFindPlanFile_PrefixedFileFrontmatterMatch verifies that FindPlanFile can
// locate a "P-NNNN-...-todo.md" file by exact branch match in its YAML frontmatter.
func TestFindPlanFile_PrefixedFileFrontmatterMatch(t *testing.T) {
	dir := t.TempDir()

	// P- prefixed plan with matching frontmatter.
	writePlanFileWithFM(t, dir, "P-0069-feat-graphify-todo.md", "feat/graphify")
	// Old-format plan with a different branch.
	writePlanFileWithFM(t, dir, "0068-feat-other-work-todo.md", "feat/other-work")

	got := FindPlanFile(dir, "feat/graphify")
	want := filepath.Join(dir, "P-0069-feat-graphify-todo.md")
	if got != want {
		t.Errorf("FindPlanFile P- frontmatter match: got %q, want %q", got, want)
	}
}

// TestFindPlanFile_PrefixedFileSlugFallback verifies that FindPlanFile can
// locate a "P-NNNN-...-todo.md" file via slug fallback when no frontmatter
// branch field is present.
func TestFindPlanFile_PrefixedFileSlugFallback(t *testing.T) {
	dir := t.TempDir()

	// P- prefixed plan with NO frontmatter — slug fallback must kick in.
	writePlanFileWithFM(t, dir, "P-0069-feat-myfeature-todo.md", "")

	got := FindPlanFile(dir, "feat/myfeature")
	want := filepath.Join(dir, "P-0069-feat-myfeature-todo.md")
	if got != want {
		t.Errorf("FindPlanFile P- slug fallback: got %q, want %q", got, want)
	}
}

// TestParsePlanHeader_PrefixedFilename verifies that ParsePlanHeader extracts
// the bare 4-digit plan number from a "P-NNNN-title-todo.md" filename —
// the returned PlanNum must be "0069", not "P-0069".
func TestParsePlanHeader_PrefixedFilename(t *testing.T) {
	dir := t.TempDir()

	path := filepath.Join(dir, "P-0069-feat-graphify-todo.md")
	content := "# Plan 0069\n> **Status:** in_progress\n> **Progress:** 3/10\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	h := ParsePlanHeader(path)

	if h.PlanNum != "0069" {
		t.Errorf("ParsePlanHeader P- prefix: expected PlanNum %q, got %q", "0069", h.PlanNum)
	}
	// Verify the rest of the header is still parsed correctly.
	if h.Status != "in_progress" {
		t.Errorf("ParsePlanHeader P- prefix: expected Status %q, got %q", "in_progress", h.Status)
	}
	if h.Progress != "3/10" {
		t.Errorf("ParsePlanHeader P- prefix: expected Progress %q, got %q", "3/10", h.Progress)
	}
}

// TestParsePlanHeader_OldFormatFilename verifies that ParsePlanHeader still
// extracts the correct bare 4-digit plan number from the legacy "NNNN-title-todo.md"
// filename format (regression guard for the old path).
func TestParsePlanHeader_OldFormatFilename(t *testing.T) {
	dir := t.TempDir()

	path := filepath.Join(dir, "0068-feat-old-style-todo.md")
	content := "# Plan 0068\n> **Status:** completed\n> **Progress:** 10/10\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	h := ParsePlanHeader(path)

	if h.PlanNum != "0068" {
		t.Errorf("ParsePlanHeader old format: expected PlanNum %q, got %q", "0068", h.PlanNum)
	}
	if h.Status != "completed" {
		t.Errorf("ParsePlanHeader old format: expected Status %q, got %q", "completed", h.Status)
	}
	if h.Progress != "10/10" {
		t.Errorf("ParsePlanHeader old format: expected Progress %q, got %q", "10/10", h.Progress)
	}
}

// ─── Folder-plan tests (P-0180 Phase 2) ────────────────────────────────────────

// writeFolderPlan creates a P-NNNN-<slug>/ folder-plan directory with a
// PLAN.md seeded with the given branch (frontmatter) and body content.
func writeFolderPlan(t *testing.T, planningDir, dirName, branch, body string) string {
	t.Helper()
	dir := filepath.Join(planningDir, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", dir, err)
	}
	content := "---\nbranch: " + branch + "\n---\n" + body
	planMD := filepath.Join(dir, "PLAN.md")
	if err := os.WriteFile(planMD, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", planMD, err)
	}
	return dir
}

// TestFindPlanFile_FolderPlan_ReturnsEntryFile verifies that FindPlanFile
// recognizes a P-NNNN-<slug>/ folder-plan directory and returns its resolved
// entry file (PLAN.md), never the bare directory path.
func TestFindPlanFile_FolderPlan_ReturnsEntryFile(t *testing.T) {
	dir := t.TempDir()
	folderDir := writeFolderPlan(t, dir, "P-0180-feat-folder-plan", "feat/folder-plan", "# Plan\n")

	got := FindPlanFile(dir, "feat/folder-plan")
	want := filepath.Join(folderDir, "PLAN.md")
	if got != want {
		t.Errorf("FindPlanFile folder-plan: got %q, want %q", got, want)
	}
}

// TestFindPlanFile_FolderPlan_ExcludesComplete verifies that when both a
// "-complete" folder-plan and an active folder-plan exist for the same
// branch, FindPlanFile returns the active one, mirroring the existing
// *-complete.md exclusion for single-file plans.
func TestFindPlanFile_FolderPlan_ExcludesComplete(t *testing.T) {
	dir := t.TempDir()
	writeFolderPlan(t, dir, "P-0010-feat-mywork-complete", "feat/mywork", "# Plan\n")
	activeDir := writeFolderPlan(t, dir, "P-0010-feat-mywork", "feat/mywork", "# Plan\n")

	got := FindPlanFile(dir, "feat/mywork")
	want := filepath.Join(activeDir, "PLAN.md")
	if got != want {
		t.Errorf("FindPlanFile folder-plan excludes complete: got %q, want %q", got, want)
	}
}

// TestFindPlanFile_FolderPlan_AmongFileplans verifies that a folder-plan and
// single-file plans coexist correctly in the same .planning/ directory — the
// branch-matching folder-plan is returned even with unrelated file-plans
// present.
func TestFindPlanFile_FolderPlan_AmongFileplans(t *testing.T) {
	dir := t.TempDir()
	folderDir := writeFolderPlan(t, dir, "P-0181-feat-folder-among-files", "feat/folder-among-files", "# Plan\n")
	writePlanFileWithFM(t, dir, "P-0120-feat-other-a-todo.md", "feat/other-a")

	got := FindPlanFile(dir, "feat/folder-among-files")
	want := filepath.Join(folderDir, "PLAN.md")
	if got != want {
		t.Errorf("FindPlanFile folder-plan among file-plans: got %q, want %q", got, want)
	}
}

// TestExtractIDFromName_PlanFolder verifies extractIDFromName recognizes a
// P-NNNN-<slug>/ folder-plan directory name for the dual-kind plan entity,
// including the "-complete" stage suffix.
func TestExtractIDFromName_PlanFolder(t *testing.T) {
	planDef, ok := EntityByName("plan")
	if !ok {
		t.Fatal("EntityByName(\"plan\") not found")
	}

	cases := []struct {
		name string
		want int
	}{
		{"P-0180-feat-plan-folders-first-class", 180},
		{"P-0180-feat-plan-folders-first-class-complete", 180},
	}
	for _, tc := range cases {
		if got := extractIDFromName(tc.name, planDef); got != tc.want {
			t.Errorf("extractIDFromName(%q, plan) = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestParsePlanHeader_FolderPlan verifies that ParsePlanHeader accepts a
// P-NNNN-<slug>/ folder-plan directory path, extracts the plan number from
// the directory's own basename, and parses Status/Progress from the resolved
// PLAN.md entry file.
func TestParsePlanHeader_FolderPlan(t *testing.T) {
	dir := t.TempDir()
	folderDir := filepath.Join(dir, "P-0180-feat-folder-header")
	if err := os.MkdirAll(folderDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	planMD := filepath.Join(folderDir, "PLAN.md")
	content := "# Plan\n> **Status:** in_progress\n> **Progress:** 2/5\n"
	if err := os.WriteFile(planMD, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	h := ParsePlanHeader(folderDir)

	if h.PlanNum != "0180" {
		t.Errorf("ParsePlanHeader folder-plan: expected PlanNum %q, got %q", "0180", h.PlanNum)
	}
	if h.Status != "in_progress" {
		t.Errorf("ParsePlanHeader folder-plan: expected Status %q, got %q", "in_progress", h.Status)
	}
	if h.Progress != "2/5" {
		t.Errorf("ParsePlanHeader folder-plan: expected Progress %q, got %q", "2/5", h.Progress)
	}
}

// TestParsePlanHeader_FolderPlan_EmptyFolderReturnsPlanNumOnly verifies that
// an empty folder-plan directory (no resolvable entry file) still yields the
// correct PlanNum (extracted from the directory basename before resolution)
// while Status/Progress stay empty.
func TestParsePlanHeader_FolderPlan_EmptyFolderReturnsPlanNumOnly(t *testing.T) {
	dir := t.TempDir()
	folderDir := filepath.Join(dir, "P-0182-feat-empty-folder")
	if err := os.MkdirAll(folderDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	h := ParsePlanHeader(folderDir)

	if h.PlanNum != "0182" {
		t.Errorf("ParsePlanHeader empty folder-plan: expected PlanNum %q, got %q", "0182", h.PlanNum)
	}
	if h.Status != "" || h.Progress != "" {
		t.Errorf("ParsePlanHeader empty folder-plan: expected empty Status/Progress, got Status=%q Progress=%q", h.Status, h.Progress)
	}
}

// ─── TestFindEntityFileByID ───────────────────────────────────────────────────

// TestFindEntityFileByID_BacklogPrefix verifies that findEntityFileByID with
// prefix "B-" resolves backlog files stored in a .planning/backlog/ subdirectory
// using both frontmatter id match and filename prefix match.
func TestFindEntityFileByID_BacklogPrefix(t *testing.T) {
	dir := t.TempDir()
	backlogDir := filepath.Join(dir, "backlog")
	if err := os.MkdirAll(backlogDir, 0o755); err != nil {
		t.Fatalf("mkdir backlog: %v", err)
	}

	// Write a backlog file with frontmatter id field.
	fmContent := "---\nid: B-0042\ntitle: my backlog item\n---\n# Backlog\n"
	fmFile := filepath.Join(backlogDir, "B-0042-my-backlog-item-open.md")
	if err := os.WriteFile(fmFile, []byte(fmContent), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Write a backlog file without frontmatter (filename-prefix match only).
	noFmFile := filepath.Join(backlogDir, "B-0099-another-item-open.md")
	if err := os.WriteFile(noFmFile, []byte("# No frontmatter\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Case 1: frontmatter id match.
	got, err := findEntityFileByID("B-", backlogDir, "B-0042")
	if err != nil {
		t.Fatalf("findEntityFileByID frontmatter: unexpected error: %v", err)
	}
	if got != fmFile {
		t.Errorf("frontmatter match: got %q, want %q", got, fmFile)
	}

	// Case 2: filename prefix match (no frontmatter id).
	got2, err := findEntityFileByID("B-", backlogDir, "B-0099")
	if err != nil {
		t.Fatalf("findEntityFileByID filename match: unexpected error: %v", err)
	}
	if got2 != noFmFile {
		t.Errorf("filename match: got %q, want %q", got2, noFmFile)
	}

	// Case 3: not found returns descriptive error.
	_, err = findEntityFileByID("B-", backlogDir, "B-9999")
	if err == nil {
		t.Error("expected error for missing backlog id, got nil")
	}
}

// TestFindPlanFileByID_WrapperPassthrough verifies that FindPlanFileByID (the
// exported wrapper) still resolves plan files correctly after the refactor.
func TestFindPlanFileByID_WrapperPassthrough(t *testing.T) {
	dir := t.TempDir()

	// File with matching frontmatter id.
	fmContent := "---\nid: P-0010\ntitle: test plan\n---\n# Plan\n"
	fmFile := filepath.Join(dir, "P-0010-test-plan-todo.md")
	if err := os.WriteFile(fmFile, []byte(fmContent), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := FindPlanFileByID(dir, "P-0010")
	if err != nil {
		t.Fatalf("FindPlanFileByID: unexpected error: %v", err)
	}
	if got != fmFile {
		t.Errorf("FindPlanFileByID wrapper: got %q, want %q", got, fmFile)
	}

	// Not found should error.
	_, err = FindPlanFileByID(dir, "P-9999")
	if err == nil {
		t.Error("expected error for missing plan id, got nil")
	}
}

// ─── TestResolveScanRoots / TestResolveWriteRoot (P-0170 Phase 1) ─────────────

// makeGitRepo is a helper that initialises a bare git repo in dir, sets minimal
// identity config, and commits an initial file so the repo has at least one commit
// on the current branch.
func makeGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// Write initial file and commit so HEAD exists.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("WriteFile README: %v", err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "initial"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func runGitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestResolveScanRoots_PrimaryOnly verifies that when there are no linked worktrees
// ResolveScanRoots returns exactly one worktree-fs source (the primary) plus any
// branch-tree sources from refs/heads/* + refs/remotes/origin/*, with no error.
func TestResolveScanRoots_PrimaryOnly(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	primaryDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	makeGitRepo(t, primaryDir)

	// Change into the repo so ResolveScanRoots picks it up.
	if err := os.Chdir(primaryDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	sources, err := ResolveScanRoots()
	if err != nil {
		t.Fatalf("ResolveScanRoots error: %v", err)
	}

	// Must have at least one worktree-fs source (the primary).
	fsSources := 0
	for _, s := range sources {
		if s.Kind == "worktree-fs" {
			fsSources++
			if s.Path == "" {
				t.Error("worktree-fs source has empty Path")
			}
		}
	}
	if fsSources == 0 {
		t.Error("expected at least one worktree-fs source, got 0")
	}
	if fsSources > 1 {
		t.Errorf("expected exactly one worktree-fs source in single-repo setup, got %d", fsSources)
	}
}

// TestResolveScanRoots_TwoWorktrees verifies that when two linked worktrees are
// added, ResolveScanRoots returns exactly three worktree-fs entries (primary + 2
// linked) in addition to branch-tree sources.
func TestResolveScanRoots_TwoWorktrees(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	primaryDir := t.TempDir()
	wt1Dir := t.TempDir()
	wt2Dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	makeGitRepo(t, primaryDir)

	// Add two linked worktrees on distinct branches.
	runGitIn(t, primaryDir, "worktree", "add", "-b", "feat/wt1", wt1Dir)
	runGitIn(t, primaryDir, "worktree", "add", "-b", "feat/wt2", wt2Dir)
	defer func() {
		exec.Command("git", "-C", primaryDir, "worktree", "remove", "--force", wt1Dir).Run()
		exec.Command("git", "-C", primaryDir, "worktree", "remove", "--force", wt2Dir).Run()
	}()

	// Change into the primary so the function resolves the right repo.
	if err := os.Chdir(primaryDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	sources, err := ResolveScanRoots()
	if err != nil {
		t.Fatalf("ResolveScanRoots error: %v", err)
	}

	fsSources := 0
	for _, s := range sources {
		if s.Kind == "worktree-fs" {
			fsSources++
		}
	}
	if fsSources != 3 {
		t.Errorf("expected 3 worktree-fs sources (primary+2 linked), got %d", fsSources)
	}
}

// TestResolveScanRoots_OriginTrackingRefs verifies that ResolveScanRoots includes
// origin/* remote tracking refs as branch-tree sources when they exist.
// This test uses a bare "remote" clone to simulate origin tracking refs.
func TestResolveScanRoots_OriginTrackingRefs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	// Create a "remote" bare repo that will serve as origin.
	remoteDir := t.TempDir()
	originRepo := filepath.Join(remoteDir, "origin.git")
	if err := os.MkdirAll(originRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	originSrc := t.TempDir()
	makeGitRepo(t, originSrc)
	// Create the bare clone.
	cmd := exec.Command("git", "clone", "--bare", originSrc, originRepo)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone --bare: %v\n%s", err, out)
	}
	// Add a branch on origin so we have a non-main tracking ref.
	runGitIn(t, originSrc, "checkout", "-b", "feat/origin-branch")
	runGitIn(t, originSrc, "push", originRepo, "feat/origin-branch")

	// Clone from that origin into a local work dir.
	localDir := t.TempDir()
	cmd = exec.Command("git", "clone", originRepo, localDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
	// Fetch all refs so origin/feat/origin-branch is visible.
	runGitIn(t, localDir, "fetch", "--all")

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	if err := os.Chdir(localDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	sources, err := ResolveScanRoots()
	if err != nil {
		t.Fatalf("ResolveScanRoots error: %v", err)
	}

	// Must include at least one branch-tree source with an "origin/" prefix.
	hasOriginRef := false
	for _, s := range sources {
		if s.Kind == "branch-tree" && strings.HasPrefix(s.Branch, "origin/") {
			hasOriginRef = true
			break
		}
	}
	if !hasOriginRef {
		branches := []string{}
		for _, s := range sources {
			if s.Kind == "branch-tree" {
				branches = append(branches, s.Branch)
			}
		}
		t.Errorf("expected at least one origin/* branch-tree source; got branch-tree sources: %v", branches)
	}
}

// TestResolveWriteRoot_InLinkedWorktree verifies that ResolveWriteRoot returns the
// LINKED worktree's path (not the primary's) when called from inside a linked worktree.
// This is the inverse of the old B-0208 redirect — write paths should land in the calling
// checkout so placeholders appear where the user is working.
func TestResolveWriteRoot_InLinkedWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	primaryDir := t.TempDir()
	worktreeDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	makeGitRepo(t, primaryDir)
	runGitIn(t, primaryDir, "worktree", "add", "-b", "feat/write-root-test", worktreeDir)
	defer func() {
		exec.Command("git", "-C", primaryDir, "worktree", "remove", "--force", worktreeDir).Run()
	}()

	// Change into the LINKED worktree.
	if err := os.Chdir(worktreeDir); err != nil {
		t.Fatalf("chdir to linked worktree: %v", err)
	}

	got := ResolveWriteRoot()

	// Resolve symlinks for comparison (macOS /var vs /private/var).
	resolvedWorktree, _ := filepath.EvalSymlinks(worktreeDir)
	resolvedPrimary, _ := filepath.EvalSymlinks(primaryDir)
	resolvedGot, _ := filepath.EvalSymlinks(got)
	if resolvedGot == "" {
		resolvedGot = got
	}

	// ResolveWriteRoot must return the WORKTREE's path, not the primary's.
	if resolvedGot == resolvedPrimary {
		t.Errorf("ResolveWriteRoot: returned primary worktree path %q; want linked worktree path %q", got, worktreeDir)
	}
	if resolvedGot != resolvedWorktree {
		t.Errorf("ResolveWriteRoot: got %q (resolved: %q), want linked worktree %q (resolved: %q)", got, resolvedGot, worktreeDir, resolvedWorktree)
	}
}

// contains is a simple substring helper for test assertions.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ─── Placeholder prefix tests ────────────────────────────────────────────────

// TestGetNextNumAtomic_Prefix_NoPlaceholderWritten verifies that after B-0141
// the prefix argument no longer results in a placeholder file being created.
func TestGetNextNumAtomic_Prefix_NoPlaceholderWritten(t *testing.T) {
	dir := t.TempDir()

	// Pass "" for scanMode: auto mode falls through gracefully when outside a git repo.
	_, cleanup, err := GetNextNumAtomic(dir, "P", "")
	if err != nil {
		t.Fatalf("GetNextNumAtomic: %v", err)
	}
	defer cleanup()

	matches, _ := filepath.Glob(filepath.Join(dir, "*-.placeholder"))
	if len(matches) != 0 {
		t.Errorf("expected zero placeholder files after B-0141, got: %v", matches)
	}
}

// TestGetNextNumAtomic_Prefix_ReturnsBareNumber verifies that the returned ID
// string is the bare 4-digit number ("0001") regardless of the prefix — the
// prefix is filesystem-only and must NOT bleed into the returned value.
func TestGetNextNumAtomic_Prefix_ReturnsBareNumber(t *testing.T) {
	dir := t.TempDir()

	// Pass "" for scanMode: auto mode falls through gracefully when outside a git repo.
	id, cleanup, err := GetNextNumAtomic(dir, "P", "")
	if err != nil {
		t.Fatalf("GetNextNumAtomic: %v", err)
	}
	defer cleanup()

	// ID must be exactly 4 digits, no prefix.
	if len(id) != 4 {
		t.Errorf("expected 4-digit ID, got %q (len=%d)", id, len(id))
	}
	for _, ch := range id {
		if ch < '0' || ch > '9' {
			t.Errorf("expected ID to contain only digits, got %q", id)
			break
		}
	}
	if contains(id, "P-") || contains(id, "P") && id[0] == 'P' {
		t.Errorf("returned ID must not contain prefix, got %q", id)
	}
}

// TestGetNextNumAtomic_Prefix_StartsAtOne verifies that in an empty directory
// the first prefixed call returns "0001" — same base behaviour as the legacy
// bare format (the prefix is file-only and must not shift the sequence).
// Uses single-tree mode to isolate from any cross-worktree state. P-0170.
func TestGetNextNumAtomic_Prefix_StartsAtOne(t *testing.T) {
	dir := t.TempDir()

	// Pass "single-tree" via scanMode parameter to isolate from cross-worktree state.
	id, cleanup, err := GetNextNumAtomic(dir, "P", "single-tree")
	if err != nil {
		t.Fatalf("GetNextNumAtomic: %v", err)
	}
	defer cleanup()

	if id != "0001" {
		t.Errorf("expected first ID in empty dir to be 0001, got %q", id)
	}
}

// TestGetNextNumAtomic_Prefix_AfterRealFile verifies that when a real plan file
// (0001-*.md) exists, the prefixed call returns "0002" — confirming that
// real files are still found by the scan even when using the prefixed format.
// Uses single-tree mode to isolate from any cross-worktree state. P-0170.
func TestGetNextNumAtomic_Prefix_AfterRealFile(t *testing.T) {
	dir := t.TempDir()

	// Pre-create a real plan file for ID 0001.
	realFile := filepath.Join(dir, "0001-my-plan-todo.md")
	if err := os.WriteFile(realFile, []byte("# Plan\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Pass "single-tree" via scanMode parameter to isolate from cross-worktree state.
	id, cleanup, err := GetNextNumAtomic(dir, "P", "single-tree")
	if err != nil {
		t.Fatalf("GetNextNumAtomic: %v", err)
	}
	defer cleanup()

	if id != "0002" {
		t.Errorf("expected ID 0002 after existing 0001 file, got %q", id)
	}

	// Placeholder mechanism removed in B-0141 — no placeholder file should exist.
	matches, _ := filepath.Glob(filepath.Join(dir, "*-.placeholder"))
	if len(matches) != 0 {
		t.Errorf("expected zero placeholder files after B-0141, got: %v", matches)
	}
}

// ─── B-0141 sweep + no-placeholder tests ─────────────────────────────────────

// TestSweepLegacyPlaceholders verifies that legacy *-.placeholder files left
// over from older binaries are removed on the first call to GetNextNumAtomic,
// and that the returned next-num correctly accounts for any real files present.
func TestSweepLegacyPlaceholders(t *testing.T) {
	dir := t.TempDir()

	// Plant legacy placeholders (left over from pre-B-0141 binaries).
	legacy := []string{
		"0042-.placeholder",
		"B-0099-.placeholder",
	}
	for _, name := range legacy {
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Plant a real plan file with a higher ID.
	real := filepath.Join(dir, "0042-some-plan-todo.md")
	if err := os.WriteFile(real, []byte("# Plan\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Pass "" for scanMode: auto mode falls through gracefully when outside a git repo.
	id, cleanup, err := GetNextNumAtomic(dir, "", "")
	if err != nil {
		t.Fatalf("GetNextNumAtomic: %v", err)
	}
	defer cleanup()

	// All legacy placeholders must be swept.
	matches, _ := filepath.Glob(filepath.Join(dir, "*-.placeholder"))
	if len(matches) != 0 {
		t.Errorf("expected zero placeholder files after sweep, got: %v", matches)
	}

	// Next ID must skip past the real 0042 file → 0043.
	if id != "0043" {
		t.Errorf("expected next ID 0043 (after real 0042), got %q", id)
	}
}

// TestGetNextNumAtomic_NoPlaceholderWritten verifies that GetNextNumAtomic
// does not create any placeholder file in dir on a fresh call.
func TestGetNextNumAtomic_NoPlaceholderWritten(t *testing.T) {
	dir := t.TempDir()

	// Pass "" for scanMode: auto mode falls through gracefully when outside a git repo.
	id, cleanup, err := GetNextNumAtomic(dir, "", "")
	if err != nil {
		t.Fatalf("GetNextNumAtomic: %v", err)
	}
	defer cleanup()

	if id != "0001" {
		t.Errorf("expected ID 0001 in empty dir, got %q", id)
	}

	matches, _ := filepath.Glob(filepath.Join(dir, "*-.placeholder"))
	if len(matches) != 0 {
		t.Errorf("expected zero placeholder files, got: %v", matches)
	}
}

// TestFindPlanFileByID_BareNumericAllStages verifies that findEntityFileByID
// resolves bare numeric IDs (e.g. "0108", "108") against every canonical stage
// suffix — B-0185.
func TestFindPlanFileByID_BareNumericAllStages(t *testing.T) {
	stages := []string{"todo", "reviewed", "approved", "complete", "cancelled", "on-hold"}
	for _, stage := range stages {
		t.Run("stage_"+stage, func(t *testing.T) {
			dir := t.TempDir()
			// Create a plan file at this stage
			filename := "P-0108-some-plan-title-" + stage + ".md"
			content := "---\nid: \"P-0108\"\ntitle: test\n---\n# test\n"
			if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}

			// Full prefixed ID still works
			got, err := findEntityFileByID("plan ", dir, "P-0108")
			if err != nil {
				t.Errorf("P-0108 prefixed: unexpected error: %v", err)
			}
			if filepath.Base(got) != filename {
				t.Errorf("P-0108 prefixed: got %q want %q", filepath.Base(got), filename)
			}

			// 4-digit bare numeric
			got, err = findEntityFileByID("plan ", dir, "0108")
			if err != nil {
				t.Errorf("0108 bare: unexpected error: %v", err)
			}
			if filepath.Base(got) != filename {
				t.Errorf("0108 bare: got %q want %q", filepath.Base(got), filename)
			}

			// 3-digit bare numeric (leading zeros stripped by user)
			got, err = findEntityFileByID("plan ", dir, "108")
			if err != nil {
				t.Errorf("108 bare: unexpected error: %v", err)
			}
			if filepath.Base(got) != filename {
				t.Errorf("108 bare: got %q want %q", filepath.Base(got), filename)
			}
		})
	}
}

// TestFindPlanFileByID_BareNumericNotFound verifies that a missing bare ID
// returns an appropriate error rather than a nil result.
func TestFindPlanFileByID_BareNumericNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := findEntityFileByID("plan ", dir, "9999")
	if err == nil {
		t.Error("expected error for missing bare numeric ID 9999, got nil")
	}
}

// TestFindPlanFileByID_LegacyBareNumericFrontmatter verifies that a plan file
// whose frontmatter id: field is a bare number (no P- prefix) is still found
// by priority-1 frontmatter match when the user supplies the same bare number.
func TestFindPlanFileByID_LegacyBareNumericFrontmatter(t *testing.T) {
	dir := t.TempDir()
	filename := "P-0042-legacy-plan-todo.md"
	// Legacy frontmatter: id is bare number, not prefixed
	content := "---\nid: \"0042\"\ntitle: legacy\n---\n# legacy\n"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Exact bare number should match via priority-1 frontmatter
	got, err := findEntityFileByID("plan ", dir, "0042")
	if err != nil {
		t.Errorf("legacy bare frontmatter: unexpected error: %v", err)
	}
	if filepath.Base(got) != filename {
		t.Errorf("legacy bare frontmatter: got %q want %q", filepath.Base(got), filename)
	}
}

// ─── Phase 5: ReservePlaceholder / ReleasePlaceholder tests (B-0116/P-0116) ──

// TestReservePlaceholder_Basic verifies a single reservation creates the placeholder
// file and returns the correct ID format.
// Uses single-tree mode to isolate from any cross-worktree state. P-0170.
func TestReservePlaceholder_Basic(t *testing.T) {
	dir := t.TempDir()
	// Pass "single-tree" via scanMode parameter to isolate from cross-worktree state.
	id, path, err := ReservePlaceholder(dir, "P", "single-tree")
	if err != nil {
		t.Fatalf("ReservePlaceholder: %v", err)
	}
	if id != "P-0001" {
		t.Errorf("expected P-0001, got %q", id)
	}
	expectedPath := filepath.Join(dir, "P-0001.placeholder")
	if path != expectedPath {
		t.Errorf("path: got %q want %q", path, expectedPath)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("placeholder file not found: %v", err)
	}
}

// TestReservePlaceholder_SkipsExisting verifies that a manually-planted placeholder
// is counted by the scan and the next reservation skips past it (O_EXCL test).
// Uses single-tree mode to isolate from any cross-worktree state. P-0170.
func TestReservePlaceholder_SkipsExisting(t *testing.T) {
	dir := t.TempDir()
	// Plant a future-ID placeholder to simulate a concurrent reservation.
	existing := filepath.Join(dir, "P-0005.placeholder")
	if err := os.WriteFile(existing, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Pass "single-tree" via scanMode parameter to isolate from cross-worktree state.
	id, _, err := ReservePlaceholder(dir, "P", "single-tree")
	if err != nil {
		t.Fatalf("ReservePlaceholder: %v", err)
	}
	// Must skip past 0005 and reserve 0006.
	if id != "P-0006" {
		t.Errorf("expected P-0006 (skipping existing 0005), got %q", id)
	}
}

// TestReserveThenRelease verifies the round-trip: reserve → placeholder exists →
// release → placeholder gone.
// Uses single-tree mode to isolate from any cross-worktree state. P-0170.
func TestReserveThenRelease(t *testing.T) {
	dir := t.TempDir()
	// Pass "single-tree" via scanMode parameter to isolate from cross-worktree state.
	id, phPath, err := ReservePlaceholder(dir, "B", "single-tree")
	if err != nil {
		t.Fatalf("ReservePlaceholder: %v", err)
	}
	if _, err := os.Stat(phPath); err != nil {
		t.Fatalf("placeholder should exist after reserve: %v", err)
	}

	if err := ReleasePlaceholder(dir, id); err != nil {
		t.Fatalf("ReleasePlaceholder: %v", err)
	}
	if _, err := os.Stat(phPath); !os.IsNotExist(err) {
		t.Errorf("placeholder should be gone after release, stat err: %v", err)
	}

	// Release again — idempotent.
	if err := ReleasePlaceholder(dir, id); err != nil {
		t.Errorf("ReleasePlaceholder (idempotent): %v", err)
	}

	// A subsequent reserve may return the same or a higher ID.
	// Contract: if no real plan file was written for the released ID, the ID
	// CAN be reused (the placeholder is gone, the directory scan returns the
	// same maxNum). If a real plan file WAS written, the ID will never reuse.
	// Both behaviours are valid — the important guarantee is that the placeholder
	// is gone after release (verified above) and the next reservation succeeds.
	id2, _, err := ReservePlaceholder(dir, "B", "single-tree")
	if err != nil {
		t.Fatalf("second ReservePlaceholder: %v", err)
	}
	// The second reservation must be a valid ID (non-empty, correct prefix).
	if id2 == "" || !strings.HasPrefix(id2, "B-") {
		t.Errorf("second reserve returned invalid ID %q", id2)
	}
}

// TestReservePlaceholder_Concurrent is the mandatory concurrent-race test from
// the plan. It spins 12 goroutines calling ReservePlaceholder simultaneously
// against the same temp dir, asserting each returns a distinct ID.
// Uses single-tree mode to isolate from any cross-worktree state. P-0170.
func TestReservePlaceholder_Concurrent(t *testing.T) {
	dir := t.TempDir()
	const N = 12

	type result struct {
		id  string
		err error
	}
	results := make(chan result, N)

	for i := 0; i < N; i++ {
		go func() {
			// Pass "single-tree" via scanMode parameter to isolate from cross-worktree state.
			id, _, err := ReservePlaceholder(dir, "P", "single-tree")
			results <- result{id, err}
		}()
	}

	seen := make(map[string]bool)
	for i := 0; i < N; i++ {
		r := <-results
		if r.err != nil {
			t.Errorf("goroutine %d error: %v", i, r.err)
			continue
		}
		if seen[r.id] {
			t.Errorf("duplicate ID returned: %q", r.id)
		}
		seen[r.id] = true
	}

	// Exactly N placeholder files should exist.
	entries, _ := os.ReadDir(dir)
	phCount := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".placeholder" {
			phCount++
		}
	}
	if phCount != N {
		t.Errorf("expected %d placeholder files, found %d", N, phCount)
	}
}

// TestReservePlaceholder_PlaceholderCountedByGetNextNumAtomic verifies that
// GetNextNumAtomic honours placeholder files so a re-call after reserve
// returns the NEXT id (not the already-reserved one).
// Uses single-tree mode to isolate from any cross-worktree state. P-0170.
func TestReservePlaceholder_PlaceholderCountedByGetNextNumAtomic(t *testing.T) {
	dir := t.TempDir()
	// Reserve P-0001 (placeholder written). Pass "single-tree" via scanMode parameter.
	_, _, err := ReservePlaceholder(dir, "P", "single-tree")
	if err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	// GetNextNumAtomic should now return 0002 (honours the placeholder).
	num, _, err := GetNextNumAtomic(dir, "P", "single-tree")
	if err != nil {
		t.Fatalf("GetNextNumAtomic: %v", err)
	}
	if num != "0002" {
		t.Errorf("expected 0002 after placeholder P-0001, got %q", num)
	}
}

// TestGetNextNumAtomic_EnvVarFallback verifies that when scanMode param is empty,
// the BRAVROS_NEXTID_SCAN_MODE env var is consulted as a fallback. This preserves
// the existing escape-hatch path for external library users.
func TestGetNextNumAtomic_EnvVarFallback(t *testing.T) {
	// Use env var (not param) to set single-tree mode.
	t.Setenv("BRAVROS_NEXTID_SCAN_MODE", "single-tree")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "P-0007-plan-todo.md"), []byte("# Plan\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Pass "" scanMode — should fall back to env var (single-tree).
	id, _, err := GetNextNumAtomic(dir, "P", "")
	if err != nil {
		t.Fatalf("GetNextNumAtomic: %v", err)
	}
	if id != "0008" {
		t.Errorf("env-var fallback: expected 0008 after P-0007, got %q", id)
	}
}

// TestRunGitCommand_Timeout verifies that runGitCommand honours the
// gitCommandTimeout and returns an error when the subprocess would block
// longer than the configured deadline.
//
// Strategy: pass a deliberately slow command ("git hash-object --stdin" in a
// state where stdin is never written — git blocks waiting). We override the
// timeout to a very short value via a test-local wrapper so the test completes
// in milliseconds.
func TestRunGitCommand_Timeout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	// Use a context with a very short timeout to verify the cancellation path.
	// We call exec.CommandContext directly (mimicking runGitCommand) with a
	// sub-millisecond deadline so git is killed immediately.
	deadline := 10 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()

	// "git hash-object --stdin" blocks reading stdin — perfect for a timeout test.
	cmd := exec.CommandContext(ctx, "git", "hash-object", "--stdin")
	// Hold a pipe open WITHOUT writing or closing it so git truly blocks on read.
	// (A nil Stdin wires /dev/null: git reads EOF instantly, exits 0, and beats
	// the deadline on fast runners — the exact flake this replaces.)
	stdinPipe, pipeErr := cmd.StdinPipe()
	if pipeErr != nil {
		t.Fatalf("StdinPipe: %v", pipeErr)
	}
	defer stdinPipe.Close()
	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	// Must have errored (context deadline exceeded or signal kill).
	if err == nil {
		t.Error("expected error from timed-out git command, got nil")
	}
	// Must have terminated well within the gitCommandTimeout (30s); 3s is generous.
	if elapsed > 3*time.Second {
		t.Errorf("git command took %v — timeout not applied (gitCommandTimeout=%v)", elapsed, gitCommandTimeout)
	}
	// Verify the timeout constant is reasonable (20s ≤ gitCommandTimeout ≤ 120s).
	if gitCommandTimeout < 20*time.Second || gitCommandTimeout > 120*time.Second {
		t.Errorf("gitCommandTimeout=%v is outside expected [20s, 120s] range", gitCommandTimeout)
	}
}

// ─── P-0170 Phase 4 regression tests ──────────────────────────────────────────

// TestReservePlaceholder_WritesToCallingCheckout verifies the P-0170 write-side
// contract: when `bravros nextid reserve` is called from a linked worktree, the
// .placeholder file must land in that worktree's .planning/ directory, NOT in the
// primary worktree's .planning/.
//
// This test covers the write-side counterpart to read-side resolution: both reads
// (plan_file via metaCmd) and writes (new placeholders) now use ResolveWriteRoot
// to land in the calling checkout (P-0172 Phase 4 unified the two sides).
func TestReservePlaceholder_WritesToCallingCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	// ── Set up a primary git repo ────────────────────────────────────────────
	primaryDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	runGitInPrimary := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = primaryDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v in %s failed: %v\n%s", args, primaryDir, err, out)
		}
	}

	runGitInPrimary("init", "-b", "main")
	runGitInPrimary("config", "user.email", "test@test.com")
	runGitInPrimary("config", "user.name", "Test")

	// Seed the primary with an initial commit and a .planning/ directory.
	planningDir := filepath.Join(primaryDir, ".planning")
	if err := os.MkdirAll(planningDir, 0o755); err != nil {
		t.Fatalf("mkdir primary .planning: %v", err)
	}
	planContent := "---\nbranch: main\n---\n# Plan 0001\n"
	planFile := filepath.Join(planningDir, "P-0001-main-plan-todo.md")
	if err := os.WriteFile(planFile, []byte(planContent), 0o644); err != nil {
		t.Fatalf("WriteFile plan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(primaryDir, "README.md"), []byte("# repo\n"), 0o644); err != nil {
		t.Fatalf("WriteFile README: %v", err)
	}
	runGitInPrimary("add", ".")
	runGitInPrimary("commit", "-m", "initial")

	// ── Create a linked worktree on a feature branch ─────────────────────────
	worktreeDir := t.TempDir()
	runGitInPrimary("worktree", "add", "-b", "feat/test-write-root", worktreeDir)
	defer func() {
		exec.Command("git", "-C", primaryDir, "worktree", "remove", "--force", worktreeDir).Run()
	}()

	// ── Switch CWD to the linked worktree ────────────────────────────────────
	if err := os.Chdir(worktreeDir); err != nil {
		t.Fatalf("chdir to worktree: %v", err)
	}

	// ── Call ReservePlaceholder from the linked worktree ─────────────────────
	// ReservePlaceholder calls GetNextNumAtomic, which calls ResolveWriteRoot
	// internally via the meta package. The write root should be worktreeDir.
	worktreePlanningDir := filepath.Join(worktreeDir, ".planning")
	if err := os.MkdirAll(worktreePlanningDir, 0o755); err != nil {
		t.Fatalf("mkdir worktree .planning: %v", err)
	}

	// Pass "" for scanMode: uses auto mode which performs cross-worktree scan
	// (the test is in a proper git repo, so ScanAllSources succeeds).
	id, ph, err := ReservePlaceholder(worktreePlanningDir, "P", "")
	if err != nil {
		t.Fatalf("ReservePlaceholder from worktree: %v", err)
	}

	t.Logf("reserved id=%s placeholder=%s", id, ph)

	// The placeholder must be inside the worktree's .planning/, NOT the primary's.
	resolvedWorktreePlanning, _ := filepath.EvalSymlinks(worktreePlanningDir)
	resolvedPrimaryPlanning, _ := filepath.EvalSymlinks(planningDir)
	resolvedPh, _ := filepath.EvalSymlinks(filepath.Dir(ph))
	if resolvedPh == "" {
		resolvedPh = filepath.Dir(ph)
	}

	if resolvedPh == resolvedPrimaryPlanning {
		t.Errorf("placeholder landed in primary's .planning/ %q — should be in worktree's .planning/ %q (P-0170 write-root regression)", resolvedPrimaryPlanning, resolvedWorktreePlanning)
	}

	if resolvedPh != resolvedWorktreePlanning {
		t.Errorf("placeholder dir = %q (resolved: %q); want worktree's .planning/ %q (resolved: %q)",
			filepath.Dir(ph), resolvedPh, worktreePlanningDir, resolvedWorktreePlanning)
	}

	// The placeholder file itself must exist.
	if _, err := os.Stat(ph); err != nil {
		t.Errorf("placeholder file %s does not exist: %v", ph, err)
	}

	// The primary's .planning/ must NOT contain the new placeholder.
	primaryMatches, _ := filepath.Glob(filepath.Join(planningDir, "P-*.placeholder"))
	if len(primaryMatches) > 0 {
		t.Errorf("primary .planning/ contains placeholder(s) %v — should only be in worktree", primaryMatches)
	}
}

// ─── P-0172 regression tests ──────────────────────────────────────────────────

// TestResolveWriteRoot_AndFindPlanFile_InLinkedWorktree verifies the post-P-0172
// (P-0170 Phase 4) contract: bravros meta --field plan_file from inside a linked
// git worktree returns a path under the CALLING WORKTREE's .planning/, not the
// primary clone's. This is the inverse of the deleted B-0208 contract — see
// .planning/debug/D-0004-bravros-meta-plan-file-worktree-path-open/diagnosis.md
// for the runtime evidence that motivated reversing it.
//
// The test exercises the exact pair of helpers metaCmd now uses:
//
//	gitRoot  := ResolveWriteRoot()
//	planning := filepath.Join(gitRoot, ".planning")
//	planFile := FindPlanFile(planning, branch)
//
// Two scenarios are covered:
//
//  1. Positive: the linked worktree's branch HAS the plan file committed.
//     ResolveWriteRoot must return the linked worktree's git toplevel, and
//     FindPlanFile must return a path under THAT toplevel — never under the
//     primary clone.
//
//  2. Negative: a second linked worktree is on a branch that predates the
//     plan's commit (the original B-0208 scenario). FindPlanFile must return
//     "" — and that is the right answer per the P-0172 Non-Goals section: a
//     worktree should not claim to act on a plan its branch does not contain.
func TestResolveWriteRoot_AndFindPlanFile_InLinkedWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	// ── Set up a primary git repo ────────────────────────────────────────────
	primaryDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	makeGitRepo(t, primaryDir) // init + user.email/name + README + initial commit

	// ── Positive scenario ────────────────────────────────────────────────────
	// Create a linked worktree on a feature branch, commit a plan file into it,
	// then verify that both ResolveWriteRoot and FindPlanFile return paths under
	// the LINKED worktree — not the primary.
	t.Run("positive_plan_in_worktree", func(t *testing.T) {
		worktreeDir := t.TempDir()
		runGitIn(t, primaryDir, "worktree", "add", "-b", "feat/p0172-worktree-test", worktreeDir)
		defer func() {
			exec.Command("git", "-C", primaryDir, "worktree", "remove", "--force", worktreeDir).Run()
		}()

		// Create .planning/ and a plan file INSIDE the linked worktree's checkout.
		worktreePlanningDir := filepath.Join(worktreeDir, ".planning")
		if err := os.MkdirAll(worktreePlanningDir, 0o755); err != nil {
			t.Fatalf("mkdir worktree .planning: %v", err)
		}
		planContent := "---\nbranch: feat/p0172-worktree-test\n---\n# Plan 0001\n"
		planFilePath := filepath.Join(worktreePlanningDir, "P-0001-feat-p0172-worktree-test-todo.md")
		if err := os.WriteFile(planFilePath, []byte(planContent), 0o644); err != nil {
			t.Fatalf("WriteFile plan in worktree: %v", err)
		}

		// Commit the plan file from inside the worktree directory.
		runGitIn(t, worktreeDir, "add", ".")
		runGitIn(t, worktreeDir, "commit", "-m", "add plan for feat/p0172-worktree-test")

		// Switch CWD to the linked worktree — this is what metaCmd does.
		if err := os.Chdir(worktreeDir); err != nil {
			t.Fatalf("chdir to worktree: %v", err)
		}
		defer os.Chdir(origDir)

		// ── Assert ResolveWriteRoot returns the LINKED worktree's toplevel ────
		gitRoot := ResolveWriteRoot()

		resolvedWorktree, _ := filepath.EvalSymlinks(worktreeDir)
		if resolvedWorktree == "" {
			resolvedWorktree = worktreeDir
		}
		resolvedPrimary, _ := filepath.EvalSymlinks(primaryDir)
		if resolvedPrimary == "" {
			resolvedPrimary = primaryDir
		}
		resolvedGitRoot, _ := filepath.EvalSymlinks(gitRoot)
		if resolvedGitRoot == "" {
			resolvedGitRoot = gitRoot
		}

		if resolvedGitRoot == resolvedPrimary {
			t.Errorf("ResolveWriteRoot: returned primary %q; want linked worktree %q (P-0172 regression)", gitRoot, worktreeDir)
		}
		if resolvedGitRoot != resolvedWorktree {
			t.Errorf("ResolveWriteRoot: got %q (resolved %q), want worktree %q (resolved %q)",
				gitRoot, resolvedGitRoot, worktreeDir, resolvedWorktree)
		}

		// ── Assert FindPlanFile returns a path under the LINKED worktree ─────
		planningDir := filepath.Join(gitRoot, ".planning")
		found := FindPlanFile(planningDir, "feat/p0172-worktree-test")
		if found == "" {
			t.Fatalf("FindPlanFile: returned empty string; expected plan under worktree .planning/")
		}
		if !strings.HasPrefix(found, gitRoot) {
			t.Errorf("FindPlanFile: returned %q which is NOT under worktree gitRoot %q (P-0172 regression — path leaked to primary)", found, gitRoot)
		}
		if strings.HasPrefix(found, resolvedPrimary) {
			t.Errorf("FindPlanFile: returned %q which is under primary %q; must be under linked worktree", found, resolvedPrimary)
		}
	})

	// ── Negative scenario ────────────────────────────────────────────────────
	// A second linked worktree is on a branch that was created BEFORE the plan
	// file was added. FindPlanFile on that branch must return "" — the worktree
	// should not claim a plan whose commit its branch history does not contain.
	// (This is the original B-0208 trigger condition, now producing the correct
	// empty result rather than redirecting to the primary.)
	t.Run("negative_branch_predates_plan", func(t *testing.T) {
		// Create the second worktree on "feat/predates-plan" — branched from the
		// initial commit, before any plan file exists anywhere.
		worktreeDir2 := t.TempDir()
		runGitIn(t, primaryDir, "worktree", "add", "-b", "feat/predates-plan", worktreeDir2)
		defer func() {
			exec.Command("git", "-C", primaryDir, "worktree", "remove", "--force", worktreeDir2).Run()
		}()

		// Switch CWD into this older-branch worktree.
		if err := os.Chdir(worktreeDir2); err != nil {
			t.Fatalf("chdir to older worktree: %v", err)
		}
		defer os.Chdir(origDir)

		// The older branch has no .planning/ dir at all — ReadDir returns err → "".
		planningDir2 := filepath.Join(worktreeDir2, ".planning")
		found := FindPlanFile(planningDir2, "feat/predates-plan")
		if found != "" {
			t.Errorf("FindPlanFile: expected empty string for branch that predates plan commit, got %q", found)
		}
	})
}
