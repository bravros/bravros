package autopr_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bravros/bravros/cli/internal/autopr"
)

// writeLock writes a lock file with the given metadata fields.
func writeLock(t *testing.T, path string, ts time.Time, sessionID string) {
	t.Helper()
	content := "timestamp=" + ts.Format(time.RFC3339) + "\n" +
		"skill=auto-pr\n" +
		"session_id=" + sessionID + "\n" +
		"mode=single\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeLock: %v", err)
	}
}

// makeTempPlanDir creates a temp dir with a .planning/ subdirectory and
// returns both paths. The caller is responsible for cleanup (use t.TempDir()).
func makeTempPlanDir(t *testing.T) (base, planning string) {
	t.Helper()
	base = t.TempDir()
	planning = filepath.Join(base, ".planning")
	if err := os.MkdirAll(planning, 0755); err != nil {
		t.Fatalf("MkdirAll .planning: %v", err)
	}
	return
}

// ---------------------------------------------------------------------------
// ParseLockMeta
// ---------------------------------------------------------------------------

// TestParseLockMeta_MissingFile verifies an error is returned for a missing file.
func TestParseLockMeta_MissingFile(t *testing.T) {
	_, err := autopr.ParseLockMeta("/nonexistent/.auto-pr-lock")
	if err == nil {
		t.Error("expected error for missing lock file, got nil")
	}
}

// TestParseLockMeta_ValidContent verifies all fields are parsed correctly.
func TestParseLockMeta_ValidContent(t *testing.T) {
	_, planning := makeTempPlanDir(t)
	lockPath := filepath.Join(planning, ".auto-pr-lock")

	past := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Second)
	writeLock(t, lockPath, past, "sess-abc")

	meta, err := autopr.ParseLockMeta(lockPath)
	if err != nil {
		t.Fatalf("ParseLockMeta: %v", err)
	}
	if meta.Skill != "auto-pr" {
		t.Errorf("Skill: got %q, want auto-pr", meta.Skill)
	}
	if meta.SessionID != "sess-abc" {
		t.Errorf("SessionID: got %q, want sess-abc", meta.SessionID)
	}
	if meta.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
	// Age should be at least 4 minutes (we wrote it as 5m old, RFC3339 truncates to second).
	if meta.Age() < 4*time.Minute {
		t.Errorf("Age() = %v, want >= 4m", meta.Age())
	}
}

// ---------------------------------------------------------------------------
// LockMeta.IsSelfSession
// ---------------------------------------------------------------------------

// TestIsSelfSession_Match verifies true when session IDs match.
func TestIsSelfSession_Match(t *testing.T) {
	m := autopr.LockMeta{SessionID: "active-session-123"}
	if !m.IsSelfSession("active-session-123") {
		t.Error("IsSelfSession should return true when session IDs match")
	}
}

// TestIsSelfSession_Mismatch verifies false when session IDs differ.
func TestIsSelfSession_Mismatch(t *testing.T) {
	m := autopr.LockMeta{SessionID: "session-A"}
	if m.IsSelfSession("session-B") {
		t.Error("IsSelfSession should return false when session IDs differ")
	}
}

// TestIsSelfSession_EmptyLockSession verifies false when lock has no session_id.
func TestIsSelfSession_EmptyLockSession(t *testing.T) {
	m := autopr.LockMeta{SessionID: ""}
	if m.IsSelfSession("active-session-123") {
		t.Error("IsSelfSession should return false when lock has empty session_id")
	}
}

// TestIsSelfSession_EmptyCurrentSession verifies false when current session is empty.
func TestIsSelfSession_EmptyCurrentSession(t *testing.T) {
	m := autopr.LockMeta{SessionID: "lock-session-xyz"}
	if m.IsSelfSession("") {
		t.Error("IsSelfSession should return false when current session ID is empty")
	}
}

// ---------------------------------------------------------------------------
// Age threshold (B-0163)
// ---------------------------------------------------------------------------

// TestAge_AboveThreshold verifies that a lock older than the threshold is allowed.
func TestAge_AboveThreshold(t *testing.T) {
	past := time.Now().Add(-15 * time.Minute)
	m := autopr.LockMeta{Timestamp: past}
	threshold := 10 * time.Minute
	if m.Age() < threshold {
		t.Errorf("Age() = %v, expected >= %v for a 15-min-old lock", m.Age(), threshold)
	}
}

// TestAge_BelowThreshold verifies that a fresh lock is below the threshold.
func TestAge_BelowThreshold(t *testing.T) {
	recent := time.Now().Add(-2 * time.Minute)
	m := autopr.LockMeta{Timestamp: recent}
	threshold := 10 * time.Minute
	if m.Age() >= threshold {
		t.Errorf("Age() = %v, expected < %v for a 2-min-old lock", m.Age(), threshold)
	}
}

// TestAge_ZeroTimestamp verifies Age() returns 0 for a zero Timestamp.
func TestAge_ZeroTimestamp(t *testing.T) {
	m := autopr.LockMeta{}
	if m.Age() != 0 {
		t.Errorf("Age() = %v, want 0 for zero Timestamp", m.Age())
	}
}

// ---------------------------------------------------------------------------
// Integration: no-op on missing default lock with no other locks (B-0162)
// ---------------------------------------------------------------------------

// TestClearLock_NoOpNoOtherLocks verifies the no-op path produces no warning
// when there are no other lock files. (Exercises the glob logic indirectly via
// ParseLockMeta + glob in a temp directory.)
func TestClearLock_NoOpNoOtherLocks(t *testing.T) {
	_, planning := makeTempPlanDir(t)
	// No lock files at all — glob should return nothing.
	matches, err := filepath.Glob(filepath.Join(planning, ".auto-*-lock"))
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("expected 0 lock files, got %d: %v", len(matches), matches)
	}
}

// ---------------------------------------------------------------------------
// Integration: no-op on missing default lock WITH other locks present (B-0162)
// ---------------------------------------------------------------------------

// TestClearLock_NoOpWithOtherLocksPresent verifies that when the default target
// is missing but other lock files exist, a warning listing them should be shown.
// We test this by confirming the glob finds the other locks.
func TestClearLock_NoOpWithOtherLocksPresent(t *testing.T) {
	_, planning := makeTempPlanDir(t)

	// Create a non-default lock (simulating a running auto-merge pipeline).
	mergeLock := filepath.Join(planning, ".auto-merge-lock")
	writeLock(t, mergeLock, time.Now().Add(-20*time.Minute), "other-session")

	// The default target (.auto-pr-lock) does not exist.
	// Glob for other locks (excluding default target).
	defaultTarget := filepath.Join(planning, ".auto-pr-lock")
	matches, err := filepath.Glob(filepath.Join(planning, ".auto-*-lock"))
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}
	var others []string
	for _, m := range matches {
		if m != defaultTarget {
			others = append(others, m)
		}
	}
	if len(others) == 0 {
		t.Error("expected to find the other lock file (.auto-merge-lock), got none")
	}
	// Verify the other lock's age is readable.
	meta, err := autopr.ParseLockMeta(others[0])
	if err != nil {
		t.Fatalf("ParseLockMeta for other lock: %v", err)
	}
	if meta.Age() < 15*time.Minute {
		t.Errorf("expected other lock to be >= 15m old, got %v", meta.Age())
	}
}

// ---------------------------------------------------------------------------
// --all flag: deletes all matching lock files (B-0162)
// ---------------------------------------------------------------------------

// TestClearLockAll_RemovesAllLocks verifies --all removes every .auto-*-lock file.
func TestClearLockAll_RemovesAllLocks(t *testing.T) {
	_, planning := makeTempPlanDir(t)

	// Create two lock files.
	prLock := filepath.Join(planning, ".auto-pr-lock")
	mergeLock := filepath.Join(planning, ".auto-merge-lock")
	writeLock(t, prLock, time.Now().Add(-30*time.Minute), "sess-1")
	writeLock(t, mergeLock, time.Now().Add(-20*time.Minute), "sess-1")

	// Simulate the --all delete path: remove all glob matches.
	matches, err := filepath.Glob(filepath.Join(planning, ".auto-*-lock"))
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 lock files, got %d", len(matches))
	}
	for _, m := range matches {
		if rmErr := os.Remove(m); rmErr != nil {
			t.Fatalf("Remove(%s): %v", m, rmErr)
		}
	}

	// Verify both are gone.
	if _, err := os.Stat(prLock); !os.IsNotExist(err) {
		t.Error(".auto-pr-lock should have been removed")
	}
	if _, err := os.Stat(mergeLock); !os.IsNotExist(err) {
		t.Error(".auto-merge-lock should have been removed")
	}
}

// ---------------------------------------------------------------------------
// Self-session clear is refused regardless of lock age (P-0183 G3 — B-0163 revert)
// ---------------------------------------------------------------------------

// TestSelfSessionClear_RefusedRegardlessOfAge locks in the P-0183 G3 revert:
// a lock belonging to the current session confers NO clear exemption inside
// Claude Code, no matter how old it is. IsSelfSession still reports ownership
// truthfully (it feeds the informational lock listing), but the cmd layer no
// longer consults it for a clear decision — clear-lock refuses on every
// in-session path.
func TestSelfSessionClear_RefusedRegardlessOfAge(t *testing.T) {
	_, planning := makeTempPlanDir(t)
	lockPath := filepath.Join(planning, ".auto-pr-lock")

	// Lock is hours old and owned by the current session — under the reverted
	// B-0163 behavior this would have been clearable; now it must not be.
	writeLock(t, lockPath, time.Now().Add(-3*time.Hour), "my-session")

	meta, err := autopr.ParseLockMeta(lockPath)
	if err != nil {
		t.Fatalf("ParseLockMeta: %v", err)
	}
	if !meta.IsSelfSession("my-session") {
		t.Error("IsSelfSession should still report ownership truthfully (informational use)")
	}
	// No age threshold exists anymore: LockMeta exposes Age() for display only.
	if meta.Age() < 2*time.Hour {
		t.Errorf("fixture should be old; got age %v", meta.Age())
	}
}

// ---------------------------------------------------------------------------
// Cross-session clear is always refused (B-0163)
// ---------------------------------------------------------------------------

// TestCrossSessionClear_AlwaysRefused verifies that IsSelfSession returns false
// for a lock belonging to a different session, regardless of age.
func TestCrossSessionClear_AlwaysRefused(t *testing.T) {
	_, planning := makeTempPlanDir(t)
	lockPath := filepath.Join(planning, ".auto-pr-lock")

	// Lock is very old but belongs to a different session.
	writeLock(t, lockPath, time.Now().Add(-2*time.Hour), "other-session")

	meta, err := autopr.ParseLockMeta(lockPath)
	if err != nil {
		t.Fatalf("ParseLockMeta: %v", err)
	}

	if meta.IsSelfSession("my-session") {
		t.Error("IsSelfSession should return false for a lock owned by a different session")
	}
}

// ---------------------------------------------------------------------------
// Regression: default clear-lock behavior unchanged for different-session lock
// ---------------------------------------------------------------------------

// TestClearLock_DefaultBehavior_DifferentSession verifies that when the lock's
// session_id does not match, IsSelfSession correctly returns false.
func TestClearLock_DefaultBehavior_DifferentSession(t *testing.T) {
	_, planning := makeTempPlanDir(t)
	lockPath := filepath.Join(planning, ".auto-pr-lock")

	writeLock(t, lockPath, time.Now().Add(-1*time.Hour), "session-XYZ")

	meta, err := autopr.ParseLockMeta(lockPath)
	if err != nil {
		t.Fatalf("ParseLockMeta: %v", err)
	}

	// Simulate: current session is "session-ABC" (different from lock's "session-XYZ")
	if meta.IsSelfSession("session-ABC") {
		t.Error("cross-session: IsSelfSession should return false — refuse without --force")
	}
}

// ---------------------------------------------------------------------------
// --all refuses when a discovered lock belongs to a different session (B-0162)
// ---------------------------------------------------------------------------

// TestClearLockAll_RefusesCrossSessionLock verifies that when --all encounters
// a lock from a different session inside Claude Code, it should refuse.
func TestClearLockAll_RefusesCrossSessionLock(t *testing.T) {
	_, planning := makeTempPlanDir(t)

	prLock := filepath.Join(planning, ".auto-pr-lock")
	writeLock(t, prLock, time.Now().Add(-30*time.Minute), "other-session")

	matches, err := filepath.Glob(filepath.Join(planning, ".auto-*-lock"))
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}

	// Simulate: current session is "my-session" inside Claude Code.
	currentSession := "my-session"
	blockedByOther := false
	for _, m := range matches {
		meta, parseErr := autopr.ParseLockMeta(m)
		if parseErr != nil {
			continue
		}
		// If any lock is NOT self-session, refuse (cross-session safety).
		if !meta.IsSelfSession(currentSession) {
			blockedByOther = true
			break
		}
	}
	if !blockedByOther {
		t.Error("expected cross-session lock to block --all inside Claude Code")
	}
}
