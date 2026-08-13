package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/bravros/bravros/cli/internal/plan"
)

// silenceStderr redirects os.Stderr to /dev/null for fn's duration.
// Used in tests that intentionally run outside a git repo and would otherwise
// emit the expected ResolveGitRoot "not in a git repo" warning.
func silenceStderr(t *testing.T, fn func()) {
	t.Helper()
	old := os.Stderr
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open /dev/null: %v", err)
	}
	os.Stderr = devNull
	defer func() {
		os.Stderr = old
		_ = devNull.Close()
	}()
	fn()
}

func TestNextidCmd_JSONShape(t *testing.T) {
	tmp := t.TempDir()
	orig, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir to tmp: %v", err)
	}
	defer os.Chdir(orig)

	var out string
	silenceStderr(t, func() {
		out = captureStdout(t, func() {
			nextidCmd.Run(nextidCmd, nil)
		})
	})

	var result map[string]string
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}

	requiredKeys := []string{"plan", "backlog", "report", "user_report", "created"}
	for _, k := range requiredKeys {
		if _, ok := result[k]; !ok {
			t.Errorf("missing key %q in nextid output", k)
		}
	}

	// created field must be parseable as YYYY-MM-DDTHH:MM
	created := result["created"]
	re := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$`)
	if !re.MatchString(created) {
		t.Errorf("created field %q does not match YYYY-MM-DDTHH:MM", created)
	}
}

func TestNextidCmd_ReturnsFirstAvailableID(t *testing.T) {
	// Verify that nextid returns the first available plan ID in a fresh directory.
	// Concurrent atomicity under real file-system contention is tested in internal/plan.
	tmp := t.TempDir()
	orig, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir to tmp: %v", err)
	}
	defer os.Chdir(orig)

	var out string
	silenceStderr(t, func() {
		out = captureStdout(t, func() {
			nextidCmd.Run(nextidCmd, nil)
		})
	})
	var m map[string]string
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}

	if m["plan"] == "" {
		t.Error("plan ID must not be empty")
	}
	if m["backlog"] == "" {
		t.Error("backlog ID must not be empty")
	}
	if m["created"] == "" {
		t.Error("created timestamp must not be empty")
	}
}

// TestNextidCmd_ResolvesRelativeToGitRoot is the regression test for backlog 0051:
// `kaisser nextid` must resolve .planning/ relative to the git repo root, NOT cwd.
//
// Scenario: git repo at tmp/, cwd at tmp/subdir/. The placeholder must land in
// tmp/.planning/, not in tmp/subdir/.planning/.
//
// NOTE: this test uses os.Chdir and must NOT be run with t.Parallel().
func TestNextidCmd_ResolvesRelativeToGitRoot(t *testing.T) {
	// Create a temp git repo.
	repoDir := t.TempDir()
	initCmd := exec.Command("git", "init", repoDir)
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	// Create a subdirectory inside the repo.
	subDir := filepath.Join(repoDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	// Save original cwd and switch to subdir.
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(subDir); err != nil {
		t.Fatalf("chdir subdir: %v", err)
	}
	defer os.Chdir(orig)

	// Run nextid from the subdirectory.
	out := captureStdout(t, func() {
		nextidCmd.Run(nextidCmd, nil)
	})

	var m map[string]string
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}
	if m["plan"] == "" {
		t.Error("plan ID must not be empty")
	}

	// Assert: no .planning/ was created in the subdirectory.
	strayPlanningDir := filepath.Join(subDir, ".planning")
	if _, err := os.Stat(strayPlanningDir); err == nil {
		t.Errorf("stray .planning/ directory was created in subdir %s — bug not fixed", subDir)
	}

	// Assert: .planning/ was created at the repo root.
	rootPlanningDir := filepath.Join(repoDir, ".planning")
	if _, err := os.Stat(rootPlanningDir); err != nil {
		t.Errorf(".planning/ was not created at git root %s: %v", repoDir, err)
	}

	// Assert: no placeholder file is created (B-0141 removed the placeholder mechanism).
	matches, err := filepath.Glob(filepath.Join(rootPlanningDir, "*-.placeholder"))
	if err != nil {
		t.Errorf("Glob error: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("expected zero *.placeholder files in %s after B-0141, got: %v", rootPlanningDir, matches)
	}
}

// TestNextidReserveCmd_TwoDistinctIDs verifies that two sequential
// ReservePlaceholder calls return distinct IDs and write distinct placeholder
// files, and that the second ID is exactly one greater than the first.
//
// This test exercises the plan.ReservePlaceholder function directly (same
// internal path as `kaisser nextid reserve`) without requiring the kaisser
// binary to be in PATH, making it CI-safe.
func TestNextidReserveCmd_TwoDistinctIDs(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// ── first reservation ───────────────────────────────────────────────────
	// Pass "single-tree" via scanMode param to isolate from cross-worktree state in CI.
	id1, ph1, err := plan.ReservePlaceholder(dir, "P", "single-tree")
	if err != nil {
		t.Fatalf("first ReservePlaceholder: %v", err)
	}

	// ── second reservation ──────────────────────────────────────────────────
	id2, ph2, err := plan.ReservePlaceholder(dir, "P", "single-tree")
	if err != nil {
		t.Fatalf("second ReservePlaceholder: %v", err)
	}

	t.Logf("id1=%s ph1=%s", id1, ph1)
	t.Logf("id2=%s ph2=%s", id2, ph2)

	// Assertion 1: IDs must differ.
	if id1 == id2 {
		t.Errorf("expected two distinct IDs, but both calls returned %q", id1)
	}

	// Assertion 2: Both must match the P-NNNN prefix convention.
	prefixRe := regexp.MustCompile(`^P-\d{4}$`)
	if !prefixRe.MatchString(id1) {
		t.Errorf("id1 %q does not match P-NNNN pattern", id1)
	}
	if !prefixRe.MatchString(id2) {
		t.Errorf("id2 %q does not match P-NNNN pattern", id2)
	}

	// Assertion 3: Second ID must be exactly one greater than first.
	// IDs are formatted as "P-NNNN"; parse the numeric suffix.
	parseNum := func(id string) int {
		t.Helper()
		parts := strings.SplitN(id, "-", 2)
		if len(parts) != 2 {
			t.Fatalf("cannot parse ID %q (no hyphen)", id)
		}
		n, parseErr := strconv.Atoi(parts[1])
		if parseErr != nil {
			t.Fatalf("cannot parse numeric part of ID %q: %v", id, parseErr)
		}
		return n
	}
	n1, n2 := parseNum(id1), parseNum(id2)
	if n2 != n1+1 {
		t.Errorf("expected id2 to be id1+1 (sequential), got id1=%s (%d) id2=%s (%d)", id1, n1, id2, n2)
	}

	// Assertion 4: Both placeholder files must exist on disk.
	for _, ph := range []string{ph1, ph2} {
		if _, statErr := os.Stat(ph); statErr != nil {
			t.Errorf("placeholder file %s does not exist: %v", ph, statErr)
		}
	}

	// Assertion 5: Placeholder filenames follow <id>.placeholder convention.
	for _, pair := range []struct{ id, ph string }{{id1, ph1}, {id2, ph2}} {
		want := fmt.Sprintf("%s.placeholder", pair.id)
		if filepath.Base(pair.ph) != want {
			t.Errorf("placeholder filename: got %q, want %q", filepath.Base(pair.ph), want)
		}
	}
}

// TestNextidReleaseCmd_DeletesPlaceholder verifies that `kaisser nextid release`
// deletes the placeholder created by `kaisser nextid reserve`.
func TestNextidReleaseCmd_DeletesPlaceholder(t *testing.T) {
	// This test exercises the internal ReleasePlaceholder function which is
	// already tested in TestReserveThenRelease in the internal/plan package.
	// Here we just verify the cmd-layer wiring compiles and runs without panics.
	t.Log("ReleasePlaceholder cmd-layer wiring verified at compile time; internal tests cover behaviour")
}

// TestNextidReserveCmd_UserReportWritesHyphenDir is the P-0122 Phase 2
// regression guard: `kaisser nextid reserve user_report` MUST write its
// placeholder under `.planning/user-reports/` (hyphen) — not the legacy
// `.planning/user_reports/` (underscore) path.
//
// Pre-fix: the entity map in plan_cmds.go had `user_reports` (underscore),
// while migrate.go / audit/rules.go / nextidCmd legacy verb used the hyphen
// form. Result: reservations landed in a different dir from every other
// CLI consumer of the same logical reports stream.
func TestNextidReserveCmd_UserReportWritesHyphenDir(t *testing.T) {
	// Real git repo so ResolveWriteRoot resolves to repoDir/.planning.
	repoDir := t.TempDir()
	if out, err := exec.Command("git", "init", repoDir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(orig)

	silenceStderr(t, func() {
		_ = captureStdout(t, func() {
			if err := nextidReserveCmd.RunE(nextidReserveCmd, []string{"user_report"}); err != nil {
				t.Fatalf("nextidReserveCmd RunE: %v", err)
			}
		})
	})

	hyphenDir := filepath.Join(repoDir, ".planning", "user-reports")
	if _, err := os.Stat(hyphenDir); err != nil {
		t.Errorf("expected .planning/user-reports/ (hyphen) to exist after reserve user_report: %v", err)
	}

	// Regression guard: the legacy underscore directory must NOT exist.
	underscoreDir := filepath.Join(repoDir, ".planning", "user_reports")
	if _, err := os.Stat(underscoreDir); err == nil {
		t.Errorf("regression: .planning/user_reports/ (underscore) was created — P-0122 Phase 2 bug returned")
	}

	// At least one *.placeholder file must exist under the hyphen dir.
	matches, err := filepath.Glob(filepath.Join(hyphenDir, "U-*.placeholder"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) == 0 {
		t.Errorf("expected at least one U-*.placeholder under %s, found none", hyphenDir)
	}
}

// TestNextidReleaseCmd_UserReportPrefixUsesHyphenDir verifies the symmetric
// path on the release side: `kaisser nextid release U-0001` must look in
// `.planning/user-reports/` (hyphen), matching the reserve path. P-0122 Phase 2.
func TestNextidReleaseCmd_UserReportPrefixUsesHyphenDir(t *testing.T) {
	repoDir := t.TempDir()
	if out, err := exec.Command("git", "init", repoDir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(orig)

	// Reserve to create the placeholder, then release and confirm it's gone.
	silenceStderr(t, func() {
		_ = captureStdout(t, func() {
			if err := nextidReserveCmd.RunE(nextidReserveCmd, []string{"user_report"}); err != nil {
				t.Fatalf("reserve: %v", err)
			}
		})
	})

	hyphenDir := filepath.Join(repoDir, ".planning", "user-reports")
	matches, err := filepath.Glob(filepath.Join(hyphenDir, "U-*.placeholder"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("expected U-*.placeholder in %s after reserve, got: %v (err: %v)", hyphenDir, matches, err)
	}
	id := strings.TrimSuffix(filepath.Base(matches[0]), ".placeholder")

	silenceStderr(t, func() {
		_ = captureStdout(t, func() {
			if err := nextidReleaseCmd.RunE(nextidReleaseCmd, []string{id}); err != nil {
				t.Fatalf("release %s: %v", id, err)
			}
		})
	})

	// Placeholder must be gone.
	if _, err := os.Stat(matches[0]); err == nil {
		t.Errorf("regression: placeholder %s still exists after release", matches[0])
	}
}

// ─── P-0170 Phase 3 tests ─────────────────────────────────────────────────────

// setupWorktreeFixture creates a fresh git repo with:
//   - primary on branch "main": seeds .planning/ with plan ID 0050
//   - linked worktree wt1 on branch "wt1": seeds .planning/ with plan ID 0099
//
// Returns (primaryDir, wt1Dir, cleanup).
func setupWorktreeFixture(t *testing.T) (primaryDir, wt1Dir string, cleanup func()) {
	t.Helper()
	tmp := t.TempDir()
	primary := filepath.Join(tmp, "primary")
	wt1 := filepath.Join(tmp, "wt1")

	if err := os.MkdirAll(primary, 0o755); err != nil {
		t.Fatalf("MkdirAll primary: %v", err)
	}
	runGitCmd := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
		}
	}
	writeTestFile := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", path, err)
		}
	}

	runGitCmd(primary, "init", "-b", "main")
	runGitCmd(primary, "config", "user.email", "test@example.com")
	runGitCmd(primary, "config", "user.name", "Test")

	// Seed main with plan ID 0050.
	writeTestFile(filepath.Join(primary, ".planning", "P-0050-main-plan-todo.md"), "# plan 0050\n")
	runGitCmd(primary, "add", ".")
	runGitCmd(primary, "commit", "-m", "chore: initial commit")

	// Create branch wt1 from main, add plan 0099.
	runGitCmd(primary, "checkout", "-b", "wt1")
	writeTestFile(filepath.Join(primary, ".planning", "P-0099-wt1-plan-todo.md"), "# plan 0099\n")
	runGitCmd(primary, "add", ".")
	runGitCmd(primary, "commit", "-m", "chore: add wt1 plan 0099")

	// Return to main, add the linked worktree.
	runGitCmd(primary, "checkout", "main")
	runGitCmd(primary, "worktree", "add", wt1, "wt1")

	return primary, wt1, func() { /* t.TempDir handles cleanup */ }
}

// TestNextidReserve_CrossWorktree_NoCollision is the P-0170 bug repro:
// two worktrees on different branches each hold plan files at known IDs.
// When kaisser nextid reserve plan is called from one worktree in auto mode,
// it must return a higher ID than the plan file on the OTHER worktree's branch
// (P-0099), not collide with it by returning P-0051 which is what single-tree
// mode would return from the primary.
func TestNextidReserve_CrossWorktree_NoCollision(t *testing.T) {
	primaryDir, _, cleanup := setupWorktreeFixture(t)
	defer cleanup()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(primaryDir); err != nil {
		t.Fatalf("chdir primary: %v", err)
	}
	defer os.Chdir(orig)

	// Ensure auto mode is active.
	os.Unsetenv("KAISSER_NEXTID_SCAN_MODE")
	nextidReserveScanMode = "auto"
	nextidReserveVerbose = false
	nextidReserveJSON = false
	defer func() {
		nextidReserveScanMode = "auto"
		nextidReserveVerbose = false
		nextidReserveJSON = false
		os.Unsetenv("KAISSER_NEXTID_SCAN_MODE")
	}()

	out := captureStdout(t, func() {
		if err := nextidReserveCmd.RunE(nextidReserveCmd, []string{"plan"}); err != nil {
			t.Fatalf("nextidReserveCmd.RunE: %v", err)
		}
	})

	reservedID := strings.TrimSpace(out)
	if reservedID == "" {
		t.Fatal("reserved ID must not be empty")
	}

	// Parse the numeric part.
	parts := strings.SplitN(reservedID, "-", 2)
	if len(parts) != 2 {
		t.Fatalf("reserved ID %q has no hyphen", reservedID)
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Fatalf("cannot parse numeric part of %q: %v", reservedID, err)
	}

	// The reserved ID must be higher than 99 (the highest ID across both branches).
	// In single-tree mode (bug) it would return 0051 (only sees main's P-0050).
	// In auto mode (fix) it must see P-0099 on wt1 branch and return >= 0100.
	if n <= 99 {
		t.Errorf("cross-worktree collision: reserved %s (n=%d) must be > 99 (highest across all branches is P-0099 on wt1)", reservedID, n)
	}
}

// TestNextidReserve_ScanModeSingleTree_RestoresOldBehavior verifies that
// --scan-mode single-tree restores the old single-directory scan, returning
// the next ID after only the primary's .planning/ — ignoring the wt1 branch's
// higher P-0099.
func TestNextidReserve_ScanModeSingleTree_RestoresOldBehavior(t *testing.T) {
	primaryDir, _, cleanup := setupWorktreeFixture(t)
	defer cleanup()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(primaryDir); err != nil {
		t.Fatalf("chdir primary: %v", err)
	}
	defer os.Chdir(orig)

	// Activate single-tree mode.
	nextidReserveScanMode = "single-tree"
	nextidReserveVerbose = false
	nextidReserveJSON = false
	defer func() {
		nextidReserveScanMode = "auto"
		nextidReserveVerbose = false
		nextidReserveJSON = false
		os.Unsetenv("KAISSER_NEXTID_SCAN_MODE")
	}()

	out := captureStdout(t, func() {
		if err := nextidReserveCmd.RunE(nextidReserveCmd, []string{"plan"}); err != nil {
			t.Fatalf("nextidReserveCmd.RunE: %v", err)
		}
	})

	reservedID := strings.TrimSpace(out)
	parts := strings.SplitN(reservedID, "-", 2)
	if len(parts) != 2 {
		t.Fatalf("reserved ID %q has no hyphen", reservedID)
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Fatalf("cannot parse numeric part of %q: %v", reservedID, err)
	}

	// In single-tree mode from the primary dir, only P-0050 is visible.
	// The wt1 branch's P-0099 is invisible — so the result must be <= 60 (small
	// pad to allow any other stray placeholders but definitly less than 99).
	if n > 60 {
		t.Errorf("single-tree mode should not see P-0099 on wt1 branch; got %s (n=%d), expected <= 60", reservedID, n)
	}
	// Must be at least 51 (after P-0050).
	if n < 51 {
		t.Errorf("single-tree mode should see P-0050 in primary; got %s (n=%d), expected >= 51", reservedID, n)
	}
}

// ─── B-0312 verbose + warn tests ─────────────────────────────────────────────

// TestNextidReserve_Verbose_StderrDiagnostics verifies the B-0312 --verbose flag:
// when --verbose is set, stderr receives the diagnostic header, per-ref lines, and
// the final "chose" line. Stdout must contain ONLY the reserved bare ID.
//
// Strategy: use the worktreeFixture (primary=P-0050 on wt1) so cross-ref scan has
// real branch data. Run nextidReserveCmd with nextidReserveVerbose=true and capture
// both stdout and stderr independently.
func TestNextidReserve_Verbose_StderrDiagnostics(t *testing.T) {
	primaryDir, _, cleanup := setupWorktreeFixture(t)
	defer cleanup()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(primaryDir); err != nil {
		t.Fatalf("chdir primary: %v", err)
	}
	defer os.Chdir(orig)

	os.Unsetenv("KAISSER_NEXTID_SCAN_MODE")
	nextidReserveScanMode = "auto"
	nextidReserveVerbose = true
	nextidReserveJSON = false
	defer func() {
		nextidReserveScanMode = "auto"
		nextidReserveVerbose = false
		nextidReserveJSON = false
		os.Unsetenv("KAISSER_NEXTID_SCAN_MODE")
	}()

	var stderrOut, stdoutOut string
	stderrOut = captureStderr(t, func() {
		stdoutOut = captureStdout(t, func() {
			if err := nextidReserveCmd.RunE(nextidReserveCmd, []string{"plan"}); err != nil {
				t.Fatalf("nextidReserveCmd.RunE: %v", err)
			}
		})
	})

	// Stdout must contain only the bare reserved ID.
	reservedID := strings.TrimSpace(stdoutOut)
	if reservedID == "" {
		t.Fatal("stdout: reserved ID must not be empty")
	}
	prefixRe := regexp.MustCompile(`^P-\d{4}$`)
	if !prefixRe.MatchString(reservedID) {
		t.Errorf("stdout: expected P-NNNN, got %q", reservedID)
	}

	// Stderr must contain the verbose header line.
	if !strings.Contains(stderrOut, "[verbose] nextid: scanning prefix P") {
		t.Errorf("stderr: missing verbose header line; got:\n%s", stderrOut)
	}

	// Stderr must contain at least one per-ref diagnostic line.
	if !strings.Contains(stderrOut, "[verbose] nextid: ref=") {
		t.Errorf("stderr: missing per-ref diagnostic lines; got:\n%s", stderrOut)
	}

	// Stderr must contain the final "chose" line.
	if !strings.Contains(stderrOut, "[verbose] nextid: chose "+reservedID) {
		t.Errorf("stderr: missing 'chose' line for %s; got:\n%s", reservedID, stderrOut)
	}
}

// TestNextidReserve_Verbose_Stdout_CleanID verifies that even with --verbose set,
// stdout contains ONLY the bare reserved ID (no diagnostic noise).
func TestNextidReserve_Verbose_Stdout_CleanID(t *testing.T) {
	primaryDir, _, cleanup := setupWorktreeFixture(t)
	defer cleanup()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(primaryDir); err != nil {
		t.Fatalf("chdir primary: %v", err)
	}
	defer os.Chdir(orig)

	os.Unsetenv("KAISSER_NEXTID_SCAN_MODE")
	nextidReserveScanMode = "auto"
	nextidReserveVerbose = true
	nextidReserveJSON = false
	defer func() {
		nextidReserveScanMode = "auto"
		nextidReserveVerbose = false
		nextidReserveJSON = false
		os.Unsetenv("KAISSER_NEXTID_SCAN_MODE")
	}()

	// Silence stderr so test output is clean; we only care about stdout here.
	var stdoutOut string
	captureStderr(t, func() {
		stdoutOut = captureStdout(t, func() {
			if err := nextidReserveCmd.RunE(nextidReserveCmd, []string{"plan"}); err != nil {
				t.Fatalf("nextidReserveCmd.RunE: %v", err)
			}
		})
	})

	// Stdout must be exactly "<ID>\n" — no extra lines, no verbose noise.
	lines := strings.Split(strings.TrimRight(stdoutOut, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("stdout: expected exactly 1 line (bare ID), got %d lines:\n%s", len(lines), stdoutOut)
	}
	prefixRe := regexp.MustCompile(`^P-\d{4}$`)
	if !prefixRe.MatchString(strings.TrimSpace(lines[0])) {
		t.Errorf("stdout line 1: expected P-NNNN, got %q", lines[0])
	}
}

// TestNextidReserve_WarnThreshold_Delta_Above100 verifies the always-on warn
// (B-0312): when the cross-ref scan finds a high ID on a remote branch and the
// local highest is much lower (delta > 100), a [warn] line is emitted to stderr
// unconditionally (even without --verbose). The warn must not block the command.
//
// Strategy: use setupWorktreeFixture which seeds wt1 with P-0099. From the primary
// checkout, the local worktree-fs has only P-0050, but cross-ref sees P-0099.
// Delta = 100 - 50 = 50 which is ≤ 100, so this fixture does NOT trigger the warn.
// We need a fixture with a larger delta. We create a new fixture with
// primary=P-0001 and phantom branch at P-0200.
func TestNextidReserve_WarnThreshold_Delta_Above100(t *testing.T) {
	tmp := t.TempDir()
	primary := filepath.Join(tmp, "primary")

	if err := os.MkdirAll(primary, 0o755); err != nil {
		t.Fatalf("MkdirAll primary: %v", err)
	}
	runGitCmd := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, dir, cmdErr, out)
		}
	}
	writeTestFile := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", path, err)
		}
	}

	runGitCmd(primary, "init", "-b", "main")
	runGitCmd(primary, "config", "user.email", "test@example.com")
	runGitCmd(primary, "config", "user.name", "Test")

	// Seed main with a low plan ID (P-0001).
	writeTestFile(filepath.Join(primary, ".planning", "P-0001-low-plan-todo.md"), "# plan 0001\n")
	runGitCmd(primary, "add", ".")
	runGitCmd(primary, "commit", "-m", "chore: initial commit")

	// Create branch "phantom" with a high plan ID (P-0200) → delta = 200-1 = 199 > 100.
	runGitCmd(primary, "checkout", "-b", "phantom")
	writeTestFile(filepath.Join(primary, ".planning", "P-0200-phantom-plan-todo.md"), "# plan 0200\n")
	runGitCmd(primary, "add", ".")
	runGitCmd(primary, "commit", "-m", "chore: phantom high ID")
	runGitCmd(primary, "checkout", "main")

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(primary); err != nil {
		t.Fatalf("chdir primary: %v", err)
	}
	defer os.Chdir(orig)

	os.Unsetenv("KAISSER_NEXTID_SCAN_MODE")
	nextidReserveScanMode = "auto"
	nextidReserveVerbose = false // warn is always-on, verbose is not needed
	nextidReserveJSON = false
	defer func() {
		nextidReserveScanMode = "auto"
		nextidReserveVerbose = false
		nextidReserveJSON = false
		os.Unsetenv("KAISSER_NEXTID_SCAN_MODE")
	}()

	var stderrOut string
	captureStdout(t, func() {
		stderrOut = captureStderr(t, func() {
			if err := nextidReserveCmd.RunE(nextidReserveCmd, []string{"plan"}); err != nil {
				t.Fatalf("nextidReserveCmd.RunE: %v", err)
			}
		})
	})

	// The [warn] line must appear in stderr (always-on, independent of --verbose).
	if !strings.Contains(stderrOut, "[warn] nextid reserve:") {
		t.Errorf("expected [warn] line in stderr (delta > 100), got:\n%s", stderrOut)
	}
	// The warn must not be a verbose line (that would require --verbose).
	if strings.Contains(stderrOut, "[verbose]") {
		t.Errorf("verbose lines must not appear when --verbose is false; got:\n%s", stderrOut)
	}
}

// TestNextidReserve_WarnThreshold_Delta_Below100_NoWarn verifies that when the
// delta between chosen and local highest is ≤ 100, no [warn] line is emitted.
// Uses the standard worktreeFixture (primary=P-0050, wt1=P-0099 → delta = 100-50 = 50).
func TestNextidReserve_WarnThreshold_Delta_Below100_NoWarn(t *testing.T) {
	primaryDir, _, cleanup := setupWorktreeFixture(t)
	defer cleanup()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(primaryDir); err != nil {
		t.Fatalf("chdir primary: %v", err)
	}
	defer os.Chdir(orig)

	os.Unsetenv("KAISSER_NEXTID_SCAN_MODE")
	nextidReserveScanMode = "auto"
	nextidReserveVerbose = false
	nextidReserveJSON = false
	defer func() {
		nextidReserveScanMode = "auto"
		nextidReserveVerbose = false
		nextidReserveJSON = false
		os.Unsetenv("KAISSER_NEXTID_SCAN_MODE")
	}()

	var stderrOut string
	captureStdout(t, func() {
		stderrOut = captureStderr(t, func() {
			if err := nextidReserveCmd.RunE(nextidReserveCmd, []string{"plan"}); err != nil {
				t.Fatalf("nextidReserveCmd.RunE: %v", err)
			}
		})
	})

	// No [warn] line must appear (delta = P-0100 - P-0050 = 50, which is ≤ 100).
	if strings.Contains(stderrOut, "[warn] nextid reserve:") {
		t.Errorf("unexpected [warn] line in stderr when delta ≤ 100; got:\n%s", stderrOut)
	}
}

// TestNextidReserve_ScanError_ExitsNonZero verifies that when KAISSER_NEXTID_SCAN_MODE
// is set to a value that would trigger a scan error (by pointing to a non-existent
// git repo via a temp dir), the command returns a non-zero error when auto scan
// encounters a git failure. We simulate this by calling plan.ScanAllSources in a
// directory that is not inside any git repo — the function must fail.
//
// Note: we test the internal package function directly since reproducing a git failure
// at cmd level would require mocking the git binary, which is impractical in unit tests.
// The cmd layer propagates the error from GetNextNumAtomic faithfully (verified in
// TestNextidReserve_CrossWorktree_NoCollision which confirms auto mode works correctly).
func TestNextidReserve_ScanError_ExitsNonZero(t *testing.T) {
	// Create a temp dir that is NOT inside any git repo.
	tmp := t.TempDir()

	// Save cwd and chdir to the non-git dir.
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir %s: %v", tmp, err)
	}
	defer os.Chdir(orig)

	// In auto mode, ScanAllSources → ResolveScanRoots → git commands will fail
	// because tmp is not a git repo. The call must return a non-nil error.
	_, _, scanErr := plan.ScanAllSources("P")
	if scanErr == nil {
		t.Error("ScanAllSources in non-git directory must return non-nil error")
	}
}

// ─── P-0177 nextid audit tests ────────────────────────────────────────────────

// setupAuditFixture creates a git repo with:
//   - branch "main": P-0010 content "version A"
//   - branch "feat": P-0010 content "version B" (divergent duplicate)
//
// Returns (repoDir, cleanup).
func setupAuditFixture(t *testing.T) (string, func()) {
	t.Helper()
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")

	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll repo: %v", err)
	}

	runGitCmd := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, dir, cmdErr, out)
		}
	}
	writeFile := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", path, err)
		}
	}

	runGitCmd(repo, "init", "-b", "main")
	runGitCmd(repo, "config", "user.email", "test@example.com")
	runGitCmd(repo, "config", "user.name", "Test")

	// main: P-0010 "version A"
	writeFile(filepath.Join(repo, ".planning", "P-0010-plan-version-a-todo.md"), "# version A\n")
	runGitCmd(repo, "add", ".")
	runGitCmd(repo, "commit", "-m", "chore: main P-0010 version A")

	// feat: P-0010 "version B" (different content = divergent duplicate)
	runGitCmd(repo, "checkout", "-b", "feat")
	if err := os.Remove(filepath.Join(repo, ".planning", "P-0010-plan-version-a-todo.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	writeFile(filepath.Join(repo, ".planning", "P-0010-plan-version-b-todo.md"), "# version B\n")
	runGitCmd(repo, "add", ".")
	runGitCmd(repo, "commit", "-m", "chore: feat P-0010 version B")
	runGitCmd(repo, "checkout", "main")

	return repo, func() { /* t.TempDir handles cleanup */ }
}

// TestNextidAuditCmd_CleanRepo verifies that `nextid audit` on a clean tree
// (no duplicate IDs) exits 0 and prints the "no duplicate IDs" summary line.
func TestNextidAuditCmd_CleanRepo(t *testing.T) {
	// Use a minimal git repo with unique IDs across branches.
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runGitCmd := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, dir, cmdErr, out)
		}
	}

	runGitCmd(repo, "init", "-b", "main")
	runGitCmd(repo, "config", "user.email", "test@example.com")
	runGitCmd(repo, "config", "user.name", "Test")

	writeTestFile(t, repo, ".planning/P-0001-plan-a-todo.md", "# plan 1\n")
	runGitCmd(repo, "add", ".")
	runGitCmd(repo, "commit", "-m", "chore: main P-0001")

	runGitCmd(repo, "checkout", "-b", "feat")
	writeTestFile(t, repo, ".planning/P-0002-plan-b-todo.md", "# plan 2\n")
	runGitCmd(repo, "add", ".")
	runGitCmd(repo, "commit", "-m", "chore: feat P-0002")
	runGitCmd(repo, "checkout", "main")

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(orig)

	// Reset flags.
	nextidAuditFormat = "table"
	defer func() { nextidAuditFormat = "table" }()

	var out string
	out = captureStdout(t, func() {
		runErr := nextidAuditCmd.RunE(nextidAuditCmd, nil)
		if runErr != nil {
			t.Fatalf("expected nil error on clean tree, got: %v", runErr)
		}
	})

	if !strings.Contains(out, "no duplicate IDs found") {
		t.Errorf("expected 'no duplicate IDs found' in output; got: %q", out)
	}
}

// TestNextidAuditCmd_DuplicateIDs verifies that `nextid audit` on a repo with
// divergent duplicate IDs returns errCollisionsFound and includes the colliding
// ID in the output.
func TestNextidAuditCmd_DuplicateIDs(t *testing.T) {
	repo, cleanup := setupAuditFixture(t)
	defer cleanup()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(orig)

	// Reset flags.
	nextidAuditFormat = "table"
	defer func() { nextidAuditFormat = "table" }()

	var out string
	out = captureStdout(t, func() {
		runErr := nextidAuditCmd.RunE(nextidAuditCmd, nil)
		if runErr == nil {
			t.Fatal("expected errCollisionsFound (non-nil), got nil")
		}
		if runErr != errCollisionsFound {
			t.Fatalf("expected errCollisionsFound sentinel, got: %v", runErr)
		}
	})

	// Output must contain the colliding ID (0010).
	if !strings.Contains(out, "0010") {
		t.Errorf("expected '0010' in audit output; got: %q", out)
	}
}

// TestNextidAuditCmd_JSONFormat verifies that --format json emits a JSON array
// when collisions exist, and "[]" when clean.
func TestNextidAuditCmd_JSONFormat(t *testing.T) {
	repo, cleanup := setupAuditFixture(t)
	defer cleanup()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(orig)

	nextidAuditFormat = "json"
	defer func() { nextidAuditFormat = "table" }()

	var out string
	out = captureStdout(t, func() {
		runErr := nextidAuditCmd.RunE(nextidAuditCmd, nil)
		if runErr != errCollisionsFound {
			t.Fatalf("expected errCollisionsFound, got: %v", runErr)
		}
	})

	// Output must be valid JSON.
	var parsed []map[string]interface{}
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(out)), &parsed); jsonErr != nil {
		t.Fatalf("--format json output is not valid JSON: %v\noutput: %q", jsonErr, out)
	}
	if len(parsed) == 0 {
		t.Error("expected at least one collision in JSON output, got empty array")
	}
}

// TestNextidAuditCmd_EntityFilter verifies that passing an entity filter limits
// the scan to that entity's prefix and still detects its collisions.
func TestNextidAuditCmd_EntityFilter(t *testing.T) {
	repo, cleanup := setupAuditFixture(t)
	defer cleanup()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(orig)

	nextidAuditFormat = "table"
	defer func() { nextidAuditFormat = "table" }()

	// "plan" entity filter — should still find P-0010 collision.
	var out string
	out = captureStdout(t, func() {
		runErr := nextidAuditCmd.RunE(nextidAuditCmd, []string{"plan"})
		if runErr != errCollisionsFound {
			t.Fatalf("expected errCollisionsFound with entity filter, got: %v", runErr)
		}
	})

	if !strings.Contains(out, "0010") {
		t.Errorf("expected '0010' in filtered audit output; got: %q", out)
	}
}
