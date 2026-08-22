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
// in a row — with no intervening commit, so the recorded commit_sha still
// matches HEAD — is a silent no-op (same-SHA skip path).
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
	if r1.Refreshed {
		t.Error("expected Refreshed=false on first (fresh) call")
	}

	// Capture mtime.
	info1, _ := os.Stat(r1.Path)

	// Second write, same HEAD SHA — should be skipped.
	r2, err := WriteReviewStamp("203", "changes-requested", dir)
	if err != nil {
		t.Fatalf("second WriteReviewStamp error: %v", err)
	}
	if r2.Written {
		t.Error("expected Written=false on second call (commit_sha still matches HEAD)")
	}
	if !r2.Skipped {
		t.Error("expected Skipped=true on second call")
	}
	if r2.Refreshed {
		t.Error("expected Refreshed=false on a skipped call")
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

// TestWriteReviewStamp_RefreshesOnStaleCommitSHA verifies that an existing
// stamp whose commit_sha differs from the current HEAD is overwritten in
// place with fresh data (plan_id, reviewed_at, reviewer_verdict, commit_sha),
// preserving the schema and reporting Written=true, Refreshed=true.
func TestWriteReviewStamp_RefreshesOnStaleCommitSHA(t *testing.T) {
	dir := t.TempDir()

	// First write establishes the file and the real HEAD SHA.
	r1, err := WriteReviewStamp("204", "approved", dir)
	if err != nil {
		t.Fatalf("first WriteReviewStamp error: %v", err)
	}

	data, err := os.ReadFile(r1.Path)
	if err != nil {
		t.Fatalf("failed to read stamp file: %v", err)
	}
	var original reviewStamp
	if err := json.Unmarshal(data, &original); err != nil {
		t.Fatalf("stamp is not valid JSON: %v", err)
	}
	realSHA := original.CommitSHA

	// Hand-craft a stale stamp: same schema, but commit_sha pointing at a
	// commit that is not HEAD.
	stale := original
	stale.CommitSHA = "0000000000000000000000000000000000dead"
	stale.PlanID = "stale-plan-id"
	stale.ReviewerVerdict = "changes-requested"
	staleData, err := json.MarshalIndent(stale, "", "  ")
	if err != nil {
		t.Fatalf("marshal stale stamp: %v", err)
	}
	if err := os.WriteFile(r1.Path, append(staleData, '\n'), 0o644); err != nil {
		t.Fatalf("write stale stamp: %v", err)
	}

	// Refresh call — commit_sha in the stale file no longer matches HEAD.
	r2, err := WriteReviewStamp("204", "approved", dir)
	if err != nil {
		t.Fatalf("refresh WriteReviewStamp error: %v", err)
	}
	if !r2.Written {
		t.Error("expected Written=true when refreshing a stale-SHA stamp")
	}
	if r2.Skipped {
		t.Error("expected Skipped=false when refreshing a stale-SHA stamp")
	}
	if !r2.Refreshed {
		t.Error("expected Refreshed=true when refreshing a stale-SHA stamp")
	}

	refreshedData, err := os.ReadFile(r1.Path)
	if err != nil {
		t.Fatalf("failed to read refreshed stamp file: %v", err)
	}
	var refreshed reviewStamp
	if err := json.Unmarshal(refreshedData, &refreshed); err != nil {
		t.Fatalf("refreshed stamp is not valid JSON: %v", err)
	}

	// Schema fields unchanged in shape.
	if refreshed.PR != original.PR {
		t.Errorf("expected pr to remain %d, got %d", original.PR, refreshed.PR)
	}
	// commit_sha recomputed to the real current HEAD, not the stale value.
	if refreshed.CommitSHA != realSHA {
		t.Errorf("expected commit_sha refreshed to %q, got %q", realSHA, refreshed.CommitSHA)
	}
	// plan_id recomputed rather than carried over from the stale file.
	if refreshed.PlanID == "stale-plan-id" {
		t.Error("expected plan_id to be recomputed on refresh, got stale value")
	}
	// reviewer_verdict updated to the value passed on the refresh call.
	if refreshed.ReviewerVerdict != "approved" {
		t.Errorf("expected reviewer_verdict=approved after refresh, got %q", refreshed.ReviewerVerdict)
	}
	// reviewed_at updated (non-empty, and not equal to a zero value).
	if refreshed.ReviewedAt == "" {
		t.Error("expected reviewed_at to be set on refresh")
	}
	// bypass/source untouched by the refresh — schema invariants hold.
	if refreshed.Bypass != false {
		t.Errorf("expected bypass=false after refresh, got %v", refreshed.Bypass)
	}
	if refreshed.Source != "github" {
		t.Errorf("expected source=github after refresh, got %q", refreshed.Source)
	}
}

// TestWriteReviewStamp_RefreshesOnCorruptExistingStamp verifies that an
// existing stamp file containing invalid JSON is treated as stale and
// overwritten (fail open toward fresh data), rather than erroring out.
func TestWriteReviewStamp_RefreshesOnCorruptExistingStamp(t *testing.T) {
	dir := t.TempDir()
	planningDir := filepath.Join(dir, ".planning")
	if err := os.MkdirAll(planningDir, 0o755); err != nil {
		t.Fatalf("failed to create .planning dir: %v", err)
	}
	stampPath := filepath.Join(planningDir, ".review-stamp-205.json")
	if err := os.WriteFile(stampPath, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("failed to write corrupt stamp: %v", err)
	}

	result, err := WriteReviewStamp("205", "approved", dir)
	if err != nil {
		t.Fatalf("WriteReviewStamp returned error on corrupt existing stamp: %v", err)
	}
	if !result.Written {
		t.Error("expected Written=true when the existing stamp is corrupt")
	}
	if result.Skipped {
		t.Error("expected Skipped=false when the existing stamp is corrupt")
	}
	if !result.Refreshed {
		t.Error("expected Refreshed=true when the existing stamp is corrupt")
	}

	data, err := os.ReadFile(stampPath)
	if err != nil {
		t.Fatalf("failed to read stamp file after refresh: %v", err)
	}
	var stamp map[string]interface{}
	if err := json.Unmarshal(data, &stamp); err != nil {
		t.Fatalf("stamp is not valid JSON after refresh: %v", err)
	}
	if stamp["reviewer_verdict"] != "approved" {
		t.Errorf("expected reviewer_verdict=approved after refresh, got %v", stamp["reviewer_verdict"])
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
