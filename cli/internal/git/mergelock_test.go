package git

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// initLockTestRepo creates a temporary git repo with an initial commit on "main".
// Duplicated minimally from cmd/mergepr_test.go so the lock primitive tests can
// live in the git package without a circular dependency.
func initLockTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v failed: %v\n%s", args, err, out)
		}
	}
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# test"), 0644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial commit"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v failed: %v\n%s", args, err, out)
		}
	}
	return dir
}

// chdirForLockTest changes the working directory to dir and registers a
// cleanup to restore the original directory when the test ends.
func chdirForLockTest(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(orig)
	})
}

// TestMergeLock_Acquire verifies that the first caller acquires the lock successfully.
func TestMergeLock_Acquire(t *testing.T) {
	dir := initLockTestRepo(t)
	chdirForLockTest(t, dir)

	lock, err := AcquireMergeLock(5 * time.Second)
	if err != nil {
		t.Fatalf("expected lock acquisition to succeed, got: %v", err)
	}
	defer lock.Release()

	if lock.Path() == "" {
		t.Error("expected non-empty lock path")
	}
	if !filepath.IsAbs(lock.Path()) {
		t.Errorf("expected absolute lock path, got: %s", lock.Path())
	}
	if filepath.Base(lock.Path()) != "bravros-merge.lock" {
		t.Errorf("expected lock file named bravros-merge.lock, got: %s", filepath.Base(lock.Path()))
	}
}

// TestMergeLock_ConcurrentBlocks verifies that a second caller blocks and
// times out while the first caller holds the lock.
func TestMergeLock_ConcurrentBlocks(t *testing.T) {
	dir := initLockTestRepo(t)
	chdirForLockTest(t, dir)

	lock1, err := AcquireMergeLock(5 * time.Second)
	if err != nil {
		t.Fatalf("first caller: expected success, got: %v", err)
	}
	defer lock1.Release()

	start := time.Now()
	_, err2 := AcquireMergeLock(1 * time.Second)
	elapsed := time.Since(start)

	if err2 == nil {
		t.Fatal("second caller: expected timeout error, got nil (lock should have been held)")
	}
	if !IsMergeLockTimeout(err2) {
		t.Errorf("second caller: expected MergeLockTimeoutError, got: %T %v", err2, err2)
	}
	if elapsed < 800*time.Millisecond {
		t.Errorf("second caller returned too quickly (%v); expected ~1s", elapsed)
	}
	if elapsed > 4*time.Second {
		t.Errorf("second caller waited too long (%v); timeout was 1s", elapsed)
	}
}

// TestMergeLock_ReleasedOnError verifies that Release() allows re-acquisition.
func TestMergeLock_ReleasedOnError(t *testing.T) {
	dir := initLockTestRepo(t)
	chdirForLockTest(t, dir)

	lock1, err := AcquireMergeLock(5 * time.Second)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	lock1.Release()

	lock2, err2 := AcquireMergeLock(5 * time.Second)
	if err2 != nil {
		t.Fatalf("second acquire after release: expected success, got: %v", err2)
	}
	defer lock2.Release()
}

// TestMergePr_LockTimeoutIsConflict verifies MergeLockTimeoutError identification.
func TestMergePr_LockTimeoutIsConflict(t *testing.T) {
	t.Parallel()

	timeoutErr := &MergeLockTimeoutError{Path: "/tmp/test.lock", Timeout: 60 * time.Second}
	if !IsMergeLockTimeout(timeoutErr) {
		t.Error("expected IsMergeLockTimeout=true for MergeLockTimeoutError")
	}
	if timeoutErr.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

// TestMergePr_ConflictErrorType verifies MergeConflictError + IsMergeConflict.
func TestMergePr_ConflictErrorType(t *testing.T) {
	t.Parallel()

	conflictErr := &MergeConflictError{PR: 42, Stderr: "pull request is not mergeable"}
	if !IsMergeConflict(conflictErr) {
		t.Error("expected IsMergeConflict=true for MergeConflictError")
	}
	if conflictErr.Error() == "" {
		t.Error("expected non-empty error message for MergeConflictError")
	}
	lockTimeoutErr := &MergeLockTimeoutError{Path: "/tmp/x", Timeout: time.Second}
	if IsMergeConflict(lockTimeoutErr) {
		t.Error("MergeLockTimeoutError should not be identified as MergeConflictError")
	}
}

// TestMergeLock_ReleaseIdempotent verifies that calling Release multiple times
// does not panic.
func TestMergeLock_ReleaseIdempotent(t *testing.T) {
	dir := initLockTestRepo(t)
	chdirForLockTest(t, dir)

	lock, err := AcquireMergeLock(5 * time.Second)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	lock.Release()
	lock.Release()
	lock.Release()
}

// --- B-0259 Phase 1: TTL + state + heartbeat coverage ---

// TestLockState_ReadWrite_RoundTrip verifies ReadLockState round-trips through JSON.
func TestLockState_ReadWrite_RoundTrip(t *testing.T) {
	dir := initLockTestRepo(t)
	chdirForLockTest(t, dir)

	path, err := MergeLockFilePath()
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	state := &LockState{
		PID:        os.Getpid(),
		AcquiredAt: time.Now().UTC().Truncate(time.Second),
		TTLSeconds: 600,
		Meta:       map[string]string{"reason": "test"},
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, mtime, ok, err := ReadLockState(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !ok {
		t.Fatal("expected JSON parse ok=true")
	}
	if got.PID != state.PID {
		t.Errorf("PID: want %d got %d", state.PID, got.PID)
	}
	if got.TTLSeconds != state.TTLSeconds {
		t.Errorf("TTL: want %d got %d", state.TTLSeconds, got.TTLSeconds)
	}
	if got.Meta["reason"] != "test" {
		t.Errorf("Meta: want test got %q", got.Meta["reason"])
	}
	if mtime.IsZero() {
		t.Error("expected non-zero mtime")
	}
}

// TestLockState_LegacyTextFile returns ok=false so the new verb does not
// confuse old `pid=...` content (written by AcquireMergeLock) for a held lock.
func TestLockState_LegacyTextFile(t *testing.T) {
	dir := initLockTestRepo(t)
	chdirForLockTest(t, dir)

	path, err := MergeLockFilePath()
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("pid=12345\ntime=2026-05-14T00:00:00Z\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, _, ok, err := ReadLockState(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if ok {
		t.Error("legacy pid= text should NOT be parsed as JSON state")
	}
}

// TestLockState_IsStale_NotYetExpired guards against premature stale-reaping.
func TestLockState_IsStale_NotYetExpired(t *testing.T) {
	state := &LockState{
		PID:        os.Getpid(), // live process
		AcquiredAt: time.Now().Add(-5 * time.Minute),
		TTLSeconds: 600,
	}
	if IsLockStateStale(state, time.Now().Add(-5*time.Minute), 30*time.Second) {
		t.Error("fresh state should not be stale")
	}
}

// TestLockState_IsStale_ExpiredAndDead verifies stale-reap fires only when
// BOTH TTL+grace elapsed AND holder PID is dead.
func TestLockState_IsStale_ExpiredAndDead(t *testing.T) {
	deadPID := pickDeadPID(t)
	state := &LockState{
		PID:        deadPID,
		AcquiredAt: time.Now().Add(-20 * time.Minute),
		TTLSeconds: 600, // 10m TTL, acquired 20m ago → expired
	}
	mtime := time.Now().Add(-20 * time.Minute)
	if !IsLockStateStale(state, mtime, 30*time.Second) {
		t.Error("expired state with dead PID should be stale")
	}
}

// TestLockState_IsStale_ExpiredButAlive must NOT reap a still-running merge.
func TestLockState_IsStale_ExpiredButAlive(t *testing.T) {
	state := &LockState{
		PID:        os.Getpid(),
		AcquiredAt: time.Now().Add(-20 * time.Minute),
		TTLSeconds: 600,
	}
	mtime := time.Now().Add(-20 * time.Minute)
	if IsLockStateStale(state, mtime, 30*time.Second) {
		t.Error("expired-but-alive holder should NOT be reaped")
	}
}

// TestLockState_IsStale_RespectsHeartbeatMtime verifies that a fresh mtime
// (from a heartbeat touch) prevents reaping even when acquired_at is old.
func TestLockState_IsStale_RespectsHeartbeatMtime(t *testing.T) {
	deadPID := pickDeadPID(t)
	state := &LockState{
		PID:        deadPID,
		AcquiredAt: time.Now().Add(-20 * time.Minute),
		TTLSeconds: 60, // short TTL
	}
	// Heartbeat just touched the file 5s ago — should NOT be stale despite
	// dead PID, because the mtime anchor is fresh.
	freshMtime := time.Now().Add(-5 * time.Second)
	if IsLockStateStale(state, freshMtime, 30*time.Second) {
		t.Error("fresh heartbeat mtime should keep lock non-stale")
	}
}

// TestClearStaleLockfile verifies ClearStaleLockfile removes stale state.
func TestClearStaleLockfile(t *testing.T) {
	dir := initLockTestRepo(t)
	chdirForLockTest(t, dir)

	path, _ := MergeLockFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Plant a stale JSON state with a dead PID.
	state := &LockState{
		PID:        pickDeadPID(t),
		AcquiredAt: time.Now().Add(-30 * time.Minute),
		TTLSeconds: 60,
	}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Force a stale mtime too.
	old := time.Now().Add(-30 * time.Minute)
	_ = os.Chtimes(path, old, old)

	cleared, err := ClearStaleLockfile(path, 30*time.Second)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if !cleared {
		t.Error("expected ClearStaleLockfile to remove stale state")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected lockfile gone, stat err: %v", err)
	}
}

// TestClearStaleLockfile_NoOp on a non-stale state must NOT delete the file.
func TestClearStaleLockfile_NoOp(t *testing.T) {
	dir := initLockTestRepo(t)
	chdirForLockTest(t, dir)

	path, _ := MergeLockFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	state := &LockState{
		PID:        os.Getpid(),
		AcquiredAt: time.Now(),
		TTLSeconds: 600,
	}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cleared, err := ClearStaleLockfile(path, 30*time.Second)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if cleared {
		t.Error("non-stale state should not be cleared")
	}
}

// TestAcquireWithLockState_HappyPath: succeeds and the file ends up as parseable JSON.
func TestAcquireWithLockState_HappyPath(t *testing.T) {
	dir := initLockTestRepo(t)
	chdirForLockTest(t, dir)

	state := &LockState{
		PID:        os.Getpid(),
		AcquiredAt: time.Now().UTC().Truncate(time.Second),
		TTLSeconds: 60,
		Meta:       map[string]string{"reason": "happy"},
	}
	if err := AcquireWithLockState(state, 5*time.Second, 30*time.Second); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	path, _ := MergeLockFilePath()
	got, _, ok, err := ReadLockState(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !ok {
		t.Fatal("expected JSON state on disk after acquire")
	}
	if got.PID != state.PID {
		t.Errorf("PID: want %d got %d", state.PID, got.PID)
	}
	if got.Meta["reason"] != "happy" {
		t.Errorf("Meta: want happy got %q", got.Meta["reason"])
	}
}

// TestAcquireWithLockState_RejectsActiveState returns ErrLockHeldByActiveState
// when a non-stale JSON state is already on disk.
func TestAcquireWithLockState_RejectsActiveState(t *testing.T) {
	dir := initLockTestRepo(t)
	chdirForLockTest(t, dir)

	path, _ := MergeLockFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := &LockState{
		PID:        os.Getpid(),
		AcquiredAt: time.Now(),
		TTLSeconds: 600,
	}
	data, _ := json.Marshal(existing)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	newState := &LockState{
		PID:        os.Getpid() + 1,
		AcquiredAt: time.Now(),
		TTLSeconds: 600,
	}
	err := AcquireWithLockState(newState, 1*time.Second, 30*time.Second)
	if err != ErrLockHeldByActiveState {
		t.Fatalf("expected ErrLockHeldByActiveState, got: %v", err)
	}
}

// TestAcquireWithLockState_ReplacesStale: stale prior state should be auto-cleared.
func TestAcquireWithLockState_ReplacesStale(t *testing.T) {
	dir := initLockTestRepo(t)
	chdirForLockTest(t, dir)

	path, _ := MergeLockFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stale := &LockState{
		PID:        pickDeadPID(t),
		AcquiredAt: time.Now().Add(-30 * time.Minute),
		TTLSeconds: 60,
	}
	data, _ := json.Marshal(stale)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	old := time.Now().Add(-30 * time.Minute)
	_ = os.Chtimes(path, old, old)

	newState := &LockState{
		PID:        os.Getpid(),
		AcquiredAt: time.Now(),
		TTLSeconds: 60,
		Meta:       map[string]string{"reason": "replaced"},
	}
	if err := AcquireWithLockState(newState, 5*time.Second, 30*time.Second); err != nil {
		t.Fatalf("acquire after stale: %v", err)
	}
	got, _, ok, err := ReadLockState(path)
	if err != nil || !ok {
		t.Fatalf("read: %v ok=%v", err, ok)
	}
	if got.Meta["reason"] != "replaced" {
		t.Errorf("expected stale state to be replaced, got Meta=%v", got.Meta)
	}
}

// TestTouchLockfile_BumpsMtime verifies heartbeat semantics work.
func TestTouchLockfile_BumpsMtime(t *testing.T) {
	dir := initLockTestRepo(t)
	chdirForLockTest(t, dir)

	path, _ := MergeLockFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	old := time.Now().Add(-30 * time.Minute)
	_ = os.Chtimes(path, old, old)

	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if err := TouchLockfile(path); err != nil {
		t.Fatalf("touch: %v", err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !after.ModTime().After(before.ModTime()) {
		t.Errorf("expected mtime to advance: before=%v after=%v", before.ModTime(), after.ModTime())
	}
}

// TestPIDAlive verifies basic alive vs dead PID detection.
func TestPIDAlive_Self(t *testing.T) {
	if !PIDAlive(os.Getpid()) {
		t.Error("self PID should be reported alive")
	}
	if PIDAlive(0) {
		t.Error("PID 0 should be reported not-alive")
	}
	if PIDAlive(-1) {
		t.Error("negative PID should be reported not-alive")
	}
}

// pickDeadPID finds a PID that is definitively not running, by spawning a
// short-lived process, waiting for it to exit, and returning its (now-dead)
// PID. Using a fixed magic number like 99999 risks colliding with a real
// process on long-uptime hosts.
func pickDeadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn dummy: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait dummy: %v", err)
	}
	// Verify with kill -0 that the kernel now considers the PID dead.
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("unexpectedly still alive after Wait: %d", pid)
	}
	return pid
}
