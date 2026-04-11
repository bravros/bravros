package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bravros/private/internal/config"
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
