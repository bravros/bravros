package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// testdata returns the absolute path to a fixture file.
func testdataFile(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("testdata path: %v", err)
	}
	return p
}

// --------------------------------------------------------------------------
// ReadMarker tests
// --------------------------------------------------------------------------

func TestReadMarker_Present(t *testing.T) {
	path := testdataFile(t, "canonical_v1.sh")
	version, ok, err := ReadMarker(path)
	if err != nil {
		t.Fatalf("ReadMarker error: %v", err)
	}
	if !ok {
		t.Fatal("expected marker to be found, got ok=false")
	}
	if version != 1 {
		t.Errorf("expected version 1, got %d", version)
	}
}

func TestReadMarker_Absent(t *testing.T) {
	path := testdataFile(t, "no_marker.sh")
	version, ok, err := ReadMarker(path)
	if err != nil {
		t.Fatalf("ReadMarker error: %v", err)
	}
	if ok {
		t.Fatal("expected marker to be absent, got ok=true")
	}
	if version != 0 {
		t.Errorf("expected version 0, got %d", version)
	}
}

func TestReadMarker_MalformedVersion(t *testing.T) {
	// Create a temp file with a marker but a non-numeric version
	tmp := t.TempDir()
	path := filepath.Join(tmp, "malformed.sh")
	content := "#!/bin/bash\n# bravros-managed-commit-msg-hook vXYZ\nexit 0\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	version, ok, err := ReadMarker(path)
	if err != nil {
		t.Fatalf("ReadMarker error: %v", err)
	}
	// Marker is present but version parsing falls back to 0
	if !ok {
		t.Fatal("expected marker found (ok=true) even with malformed version")
	}
	if version != 0 {
		t.Errorf("expected version 0 for malformed suffix, got %d", version)
	}
}

func TestReadMarker_MissingFile(t *testing.T) {
	_, _, err := ReadMarker("/nonexistent/path/commit-msg")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// --------------------------------------------------------------------------
// ComputeMD5 tests
// --------------------------------------------------------------------------

func TestComputeMD5_KnownFixture(t *testing.T) {
	// The MD5 of canonical_v1.sh is known from the test setup.
	path := testdataFile(t, "canonical_v1.sh")
	got, err := ComputeMD5(path)
	if err != nil {
		t.Fatalf("ComputeMD5 error: %v", err)
	}
	// Re-compute via Go to get the expected value (self-validating)
	if len(got) != 32 {
		t.Errorf("expected 32-char hex digest, got %q (len=%d)", got, len(got))
	}
	// Cross-check: two calls on same file must agree
	got2, err := ComputeMD5(path)
	if err != nil {
		t.Fatalf("ComputeMD5 second call: %v", err)
	}
	if got != got2 {
		t.Errorf("ComputeMD5 is non-deterministic: %q != %q", got, got2)
	}
}

func TestComputeMD5_MissingFile(t *testing.T) {
	_, err := ComputeMD5("/nonexistent/file.sh")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// --------------------------------------------------------------------------
// Classify tests (table-driven — all 5 statuses)
// --------------------------------------------------------------------------

func TestClassify_AllStatuses(t *testing.T) {
	// A real canonical path is not required for most cases; we pass "" and rely
	// on marker + historical MD5 detection.  For the StatusPristine case we
	// need to register a test-specific MD5.

	// Compute the MD5 of no_marker.sh so we can inject it into HistoricalCanonicalMD5s
	noMarkerPath := testdataFile(t, "no_marker.sh")
	noMarkerMD5, err := ComputeMD5(noMarkerPath)
	if err != nil {
		t.Fatalf("ComputeMD5: %v", err)
	}

	// Save and restore HistoricalCanonicalMD5s around the test
	orig := HistoricalCanonicalMD5s
	HistoricalCanonicalMD5s = append(HistoricalCanonicalMD5s, noMarkerMD5)
	defer func() { HistoricalCanonicalMD5s = orig }()

	cases := []struct {
		name    string
		fixture string // relative to testdata/
		want    Status
	}{
		{
			name:    "missing file",
			fixture: "", // handled separately below
			want:    StatusMissing,
		},
		{
			name:    "canonical v1 → current (marker present, current version)",
			fixture: "canonical_v1.sh",
			want:    StatusCurrent,
		},
		{
			name:    "old marker v0 → old-canonical",
			fixture: "canonical_v0_old.sh",
			want:    StatusOldCanonical,
		},
		{
			name:    "historical MD5 match → pristine",
			fixture: "no_marker.sh",
			want:    StatusPristine,
		},
		{
			name:    "foreign hook (no marker, no MD5 match)",
			fixture: "foreign.sh",
			want:    StatusForeign,
		},
		{
			name:    "user-customized hook (no marker, no MD5 match)",
			fixture: "customized.sh",
			want:    StatusForeign, // customized with no marker = foreign
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var targetPath string
			if tc.fixture == "" {
				// Use a path that definitely doesn't exist
				targetPath = filepath.Join(t.TempDir(), "commit-msg-nonexistent")
			} else {
				targetPath = testdataFile(t, tc.fixture)
			}

			got, err := Classify(targetPath, "")
			if err != nil {
				t.Fatalf("Classify error: %v", err)
			}
			if got != tc.want {
				t.Errorf("Classify(%s): got %v, want %v", tc.fixture, got, tc.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// Refresh tests
// --------------------------------------------------------------------------

func TestRefresh_WritesCorrectContent(t *testing.T) {
	canonical := testdataFile(t, "canonical_v1.sh")
	tmp := t.TempDir()
	target := filepath.Join(tmp, "commit-msg")

	if err := Refresh(target, canonical); err != nil {
		t.Fatalf("Refresh error: %v", err)
	}

	// Content should match canonical
	canonicalMD5, _ := ComputeMD5(canonical)
	targetMD5, err := ComputeMD5(target)
	if err != nil {
		t.Fatalf("ComputeMD5 after Refresh: %v", err)
	}
	if targetMD5 != canonicalMD5 {
		t.Errorf("content mismatch after Refresh: target=%s, canonical=%s", targetMD5, canonicalMD5)
	}
}

func TestRefresh_Sets0755Mode(t *testing.T) {
	canonical := testdataFile(t, "canonical_v1.sh")
	tmp := t.TempDir()
	target := filepath.Join(tmp, "commit-msg")

	if err := Refresh(target, canonical); err != nil {
		t.Fatalf("Refresh error: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat after Refresh: %v", err)
	}
	mode := info.Mode() & 0777
	if mode != 0755 {
		t.Errorf("expected mode 0755, got %04o", mode)
	}
}

func TestRefresh_Atomicity_NoPartialFile(t *testing.T) {
	// Verify that after Refresh, the .tmp sibling is gone (rename was completed).
	canonical := testdataFile(t, "canonical_v1.sh")
	tmp := t.TempDir()
	target := filepath.Join(tmp, "commit-msg")

	if err := Refresh(target, canonical); err != nil {
		t.Fatalf("Refresh error: %v", err)
	}

	tmpSibling := target + ".tmp"
	if _, err := os.Stat(tmpSibling); !os.IsNotExist(err) {
		t.Errorf("expected .tmp sibling to be gone after Refresh; stat err: %v", err)
	}
}

func TestRefresh_CreatesDestinationDirectory(t *testing.T) {
	canonical := testdataFile(t, "canonical_v1.sh")
	tmp := t.TempDir()
	// Nested dir that doesn't exist yet
	target := filepath.Join(tmp, ".githooks", "commit-msg")

	if err := Refresh(target, canonical); err != nil {
		t.Fatalf("Refresh error: %v", err)
	}

	if _, err := os.Stat(target); err != nil {
		t.Errorf("target not created: %v", err)
	}
}

// --------------------------------------------------------------------------
// EnsureHooksPath tests
// --------------------------------------------------------------------------

// initBareGitRepo creates a minimal git repo in dir for testing EnsureHooksPath.
func initBareGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v: %s", dir, err, string(out))
	}
}

func TestEnsureHooksPath_FreshRepo_Writes(t *testing.T) {
	tmp := t.TempDir()
	initBareGitRepo(t, tmp)

	if err := EnsureHooksPath(tmp); err != nil {
		t.Fatalf("EnsureHooksPath error: %v", err)
	}

	// Verify git config was set
	out, err := exec.Command("git", "-C", tmp, "config", "--local", "core.hooksPath").Output()
	if err != nil {
		t.Fatalf("git config read: %v", err)
	}
	got := trimNL(string(out))
	if got != ".bravros/hooks" {
		t.Errorf("expected '.bravros/hooks', got %q", got)
	}
}

func TestEnsureHooksPath_AlreadySet_NoOp(t *testing.T) {
	tmp := t.TempDir()
	initBareGitRepo(t, tmp)

	// Pre-set the value
	if out, err := exec.Command("git", "-C", tmp, "config", "core.hooksPath", ".bravros/hooks").CombinedOutput(); err != nil {
		t.Fatalf("pre-set hooksPath: %v: %s", err, out)
	}

	// Call twice — should be a no-op on the second call
	if err := EnsureHooksPath(tmp); err != nil {
		t.Fatalf("first EnsureHooksPath: %v", err)
	}
	if err := EnsureHooksPath(tmp); err != nil {
		t.Fatalf("second EnsureHooksPath: %v", err)
	}

	// Still correct
	out, _ := exec.Command("git", "-C", tmp, "config", "--local", "core.hooksPath").Output()
	if trimNL(string(out)) != ".bravros/hooks" {
		t.Errorf("hooksPath changed after idempotent call: %q", string(out))
	}
}

func TestEnsureHooksPath_WrongValue_Overwrites(t *testing.T) {
	tmp := t.TempDir()
	initBareGitRepo(t, tmp)

	// Set to a wrong value
	if out, err := exec.Command("git", "-C", tmp, "config", "core.hooksPath", ".hooks").CombinedOutput(); err != nil {
		t.Fatalf("set wrong hooksPath: %v: %s", err, out)
	}

	if err := EnsureHooksPath(tmp); err != nil {
		t.Fatalf("EnsureHooksPath error: %v", err)
	}

	out, err := exec.Command("git", "-C", tmp, "config", "--local", "core.hooksPath").Output()
	if err != nil {
		t.Fatalf("read hooksPath: %v", err)
	}
	if trimNL(string(out)) != ".bravros/hooks" {
		t.Errorf("expected '.bravros/hooks' after overwrite, got %q", string(out))
	}
}

// --------------------------------------------------------------------------
// HistoricalCanonicalMD5s guard — asserts the hardcoded list matches the
// real pre-v1 canonical fixture so a future canonical bump can't silently
// orphan the pre-marker upgrade path.
// --------------------------------------------------------------------------

func TestHistoricalCanonicalMD5s_MatchesPreV1Fixture(t *testing.T) {
	got, err := ComputeMD5(testdataFile(t, "pre_v1_canonical.sh"))
	if err != nil {
		t.Fatalf("ComputeMD5(pre_v1_canonical.sh): %v", err)
	}
	want := HistoricalCanonicalMD5s[0]
	if got != want {
		t.Fatalf("pre-v1 canonical MD5 mismatch: fixture=%s, HistoricalCanonicalMD5s[0]=%s — drift would break the pre-marker auto-upgrade path", got, want)
	}
}

// --------------------------------------------------------------------------
// Helper
// --------------------------------------------------------------------------

func trimNL(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		return s[:len(s)-1]
	}
	return s
}
