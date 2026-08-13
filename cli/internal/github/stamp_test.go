package github

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// WriteReviewStamp tests
// ---------------------------------------------------------------------------

// TestWriteReviewStamp_WritesFile verifies that WriteReviewStamp creates the
// expected stamp file with the correct schema fields.
func TestWriteReviewStamp_WritesFile(t *testing.T) {
	dir := t.TempDir()
	result, err := WriteReviewStamp("202", "approved", dir)
	if err != nil {
		t.Fatalf("WriteReviewStamp returned error: %v", err)
	}
	if !result.Written {
		t.Error("expected Written=true on first call")
	}
	if result.Skipped {
		t.Error("expected Skipped=false on first call")
	}

	// Verify file exists.
	stampPath := filepath.Join(dir, ".planning", ".review-stamp-202.json")
	if result.Path != stampPath {
		t.Errorf("expected path %q, got %q", stampPath, result.Path)
	}

	data, err := os.ReadFile(stampPath)
	if err != nil {
		t.Fatalf("failed to read stamp file: %v", err)
	}

	// Verify schema fields.
	var stamp map[string]interface{}
	if err := json.Unmarshal(data, &stamp); err != nil {
		t.Fatalf("stamp is not valid JSON: %v", err)
	}

	// pr must be numeric 202.
	prVal, ok := stamp["pr"].(float64)
	if !ok || int(prVal) != 202 {
		t.Errorf("expected pr=202, got %v", stamp["pr"])
	}
	// reviewer_verdict must match.
	if stamp["reviewer_verdict"] != "approved" {
		t.Errorf("expected reviewer_verdict=approved, got %v", stamp["reviewer_verdict"])
	}
	// bypass must be false.
	if stamp["bypass"] != false {
		t.Errorf("expected bypass=false, got %v", stamp["bypass"])
	}
	// source must be "github".
	if stamp["source"] != "github" {
		t.Errorf("expected source=github, got %v", stamp["source"])
	}
	// reviewed_at must be present.
	if stamp["reviewed_at"] == "" || stamp["reviewed_at"] == nil {
		t.Error("expected reviewed_at to be non-empty")
	}
}

// TestWriteReviewStamp_Idempotent verifies that calling WriteReviewStamp twice
// does NOT overwrite the existing stamp (idempotency).
func TestWriteReviewStamp_Idempotent(t *testing.T) {
	dir := t.TempDir()

	// First write.
	r1, err := WriteReviewStamp("203", "approved", dir)
	if err != nil {
		t.Fatalf("first WriteReviewStamp error: %v", err)
	}
	if !r1.Written {
		t.Fatal("expected Written=true on first call")
	}

	// Capture mtime.
	info1, _ := os.Stat(r1.Path)

	// Second write — should be skipped.
	r2, err := WriteReviewStamp("203", "changes-requested", dir)
	if err != nil {
		t.Fatalf("second WriteReviewStamp error: %v", err)
	}
	if r2.Written {
		t.Error("expected Written=false on second call (stamp already exists)")
	}
	if !r2.Skipped {
		t.Error("expected Skipped=true on second call")
	}

	// Verify mtime unchanged (file was not re-written).
	info2, _ := os.Stat(r1.Path)
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("stamp file mtime changed — idempotency violated")
	}

	// Verify content still has original verdict "approved".
	data, _ := os.ReadFile(r1.Path)
	var stamp map[string]interface{}
	json.Unmarshal(data, &stamp)
	if stamp["reviewer_verdict"] != "approved" {
		t.Errorf("expected verdict to remain approved after second call, got %v", stamp["reviewer_verdict"])
	}
}

// TestWriteReviewStamp_CreatesPlanning verifies that .planning/ is created if absent.
func TestWriteReviewStamp_CreatesPlanning(t *testing.T) {
	dir := t.TempDir()
	// Verify .planning does NOT exist before the call.
	planningPath := filepath.Join(dir, ".planning")
	if _, err := os.Stat(planningPath); err == nil {
		t.Skip(".planning already exists in temp dir")
	}

	_, err := WriteReviewStamp("99", "unclear", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(planningPath); err != nil {
		t.Errorf(".planning dir was not created: %v", err)
	}
}

// TestWriteReviewStamp_EmptyPRNumber returns an error for an empty prNumber.
func TestWriteReviewStamp_EmptyPRNumber(t *testing.T) {
	dir := t.TempDir()
	_, err := WriteReviewStamp("", "approved", dir)
	if err == nil {
		t.Error("expected error for empty prNumber, got nil")
	}
}

// ---------------------------------------------------------------------------
// ResolvePlanID tests
// ---------------------------------------------------------------------------

// TestResolvePlanID_FrontmatterMatch verifies priority-1 resolution via
// a plan file with an `id:` frontmatter field and a matching `branch:` field.
func TestResolvePlanID_FrontmatterMatch(t *testing.T) {
	dir := t.TempDir()
	planningDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planningDir, 0o755)

	content := `---
id: P-0136-improvement-autonomous-pipeline-review-bundle
branch: improvement/autonomous-pipeline-review-bundle
title: "Autonomous pipeline review bundle"
---

# Plan body
`
	planFile := filepath.Join(planningDir, "P-0136-improvement-autonomous-pipeline-review-bundle-approved.md")
	os.WriteFile(planFile, []byte(content), 0o644)

	// Override gitCurrentBranch is not possible without a real git repo;
	// test the readFrontmatterID helper directly with the branch.
	got := readFrontmatterID(planFile, "improvement/autonomous-pipeline-review-bundle")
	if got != "P-0136-improvement-autonomous-pipeline-review-bundle" {
		t.Errorf("readFrontmatterID: expected P-0136-improvement-autonomous-pipeline-review-bundle, got %q", got)
	}
}

// TestResolvePlanID_BranchMismatch verifies that a plan file with a non-matching
// branch: field is skipped by readFrontmatterID.
func TestResolvePlanID_BranchMismatch(t *testing.T) {
	dir := t.TempDir()
	content := `---
id: P-0100-some-other-plan
branch: feature/other-plan
---
`
	planFile := filepath.Join(dir, "P-0100-some-other-plan-approved.md")
	os.WriteFile(planFile, []byte(content), 0o644)

	got := readFrontmatterID(planFile, "improvement/autonomous-pipeline-review-bundle")
	if got != "" {
		t.Errorf("expected empty string for branch mismatch, got %q", got)
	}
}

// TestResolvePlanID_NoBranchFieldAcceptsAny verifies that a plan file with NO
// branch: field returns the id: regardless of the branch argument.
func TestResolvePlanID_NoBranchFieldAcceptsAny(t *testing.T) {
	dir := t.TempDir()
	content := `---
id: P-0200-generic-plan
title: "Generic"
---
`
	planFile := filepath.Join(dir, "P-0200-generic-plan-approved.md")
	os.WriteFile(planFile, []byte(content), 0o644)

	got := readFrontmatterID(planFile, "some/random-branch")
	if got != "P-0200-generic-plan" {
		t.Errorf("expected P-0200-generic-plan, got %q", got)
	}
}

// TestPlanIDFromBranchSlug_Matches verifies priority-2 slug resolution.
func TestPlanIDFromBranchSlug_Matches(t *testing.T) {
	dir := t.TempDir()
	planningDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planningDir, 0o755)

	// Create a plan file without id: frontmatter.
	os.WriteFile(filepath.Join(planningDir, "P-0136-improvement-autonomous-pipeline-review-bundle-approved.md"), []byte("# Plan\n"), 0o644)

	got := planIDFromBranchSlug(planningDir, "improvement/autonomous-pipeline-review-bundle")
	if got != "P-0136-improvement-autonomous-pipeline-review-bundle" {
		t.Errorf("expected slug match, got %q", got)
	}
}

// TestPlanIDFromBranchSlug_NoMatch verifies empty return when no file matches.
func TestPlanIDFromBranchSlug_NoMatch(t *testing.T) {
	dir := t.TempDir()
	planningDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planningDir, 0o755)

	got := planIDFromBranchSlug(planningDir, "feature/completely-unrelated")
	if got != "" {
		t.Errorf("expected empty string for no match, got %q", got)
	}
}
