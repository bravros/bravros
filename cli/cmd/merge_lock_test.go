package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	gitpkg "github.com/bravros/bravros/cli/internal/git"
)

// initMergeLockTestRepo creates a temporary git repo with an initial commit on "main".
// Duplicated from cmd/mergepr_test.go's initMergePRTestRepo so this test file is
// self-contained against future merge-pr removal (Phase 4).
func initMergeLockTestRepo(t *testing.T) string {
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

func chdirForMergeLockTest(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

// resetMergeLockFlags zeroes the cobra-bound flag globals so a test starts
// from a clean slate even if a sibling test already ran the command.
func resetMergeLockFlags() {
	mergeLockAcquireTimeout = 60 * time.Second
	mergeLockAcquireTTL = 10 * time.Minute
	mergeLockAcquireMeta = nil
	mergeLockAcquireHold = false
	mergeLockStatusJSON = false
}

// pickDeadPIDCmd returns a PID that the kernel has reaped. Used to simulate
// stale lock state where the holder PID is dead.
func pickDeadPIDCmd(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("unexpectedly still alive: %d", pid)
	}
	return pid
}

// TestMergeLock_AcquireReleaseStatusCycle is the smoke-test mirror — the
// exact sequence the acceptance gate exercises after `go install`.
func TestMergeLock_AcquireReleaseStatusCycle(t *testing.T) {
	dir := initMergeLockTestRepo(t)
	chdirForMergeLockTest(t, dir)
	resetMergeLockFlags()

	// 1. acquire
	mergeLockAcquireTTL = 1 * time.Minute
	mergeLockAcquireMeta = []string{"reason=smoke"}
	var acquireOut bytes.Buffer
	mergeLockAcquireCmd.SetOut(&acquireOut)
	if err := mergeLockAcquireCmd.RunE(mergeLockAcquireCmd, nil); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !strings.Contains(acquireOut.String(), "merge-lock: acquired") {
		t.Errorf("acquire stdout missing marker: %q", acquireOut.String())
	}

	// 2. status --json (held)
	mergeLockStatusJSON = true
	var statusOut bytes.Buffer
	mergeLockStatusCmd.SetOut(&statusOut)
	if err := mergeLockStatusCmd.RunE(mergeLockStatusCmd, nil); err != nil {
		t.Fatalf("status held: %v", err)
	}
	var held statusOutput
	if err := json.Unmarshal(statusOut.Bytes(), &held); err != nil {
		t.Fatalf("status JSON parse: %v: %s", err, statusOut.String())
	}
	if !held.Held {
		t.Fatalf("expected held=true after acquire, got: %+v", held)
	}
	if held.PID != os.Getpid() {
		t.Errorf("PID mismatch: want %d got %d", os.Getpid(), held.PID)
	}
	if held.Meta["reason"] != "smoke" {
		t.Errorf("meta.reason: want smoke got %q (full meta=%v)", held.Meta["reason"], held.Meta)
	}
	if held.TTLRemainingSeconds <= 0 || held.TTLRemainingSeconds > 60 {
		t.Errorf("ttl_remaining_seconds out of expected range (0,60]: %d", held.TTLRemainingSeconds)
	}

	// 3. release
	var releaseOut bytes.Buffer
	mergeLockReleaseCmd.SetOut(&releaseOut)
	if err := mergeLockReleaseCmd.RunE(mergeLockReleaseCmd, nil); err != nil {
		t.Fatalf("release: %v", err)
	}
	if !strings.Contains(releaseOut.String(), "merge-lock: released") {
		t.Errorf("release stdout missing marker: %q", releaseOut.String())
	}

	// 4. status --json (not held)
	statusOut.Reset()
	mergeLockStatusCmd.SetOut(&statusOut)
	if err := mergeLockStatusCmd.RunE(mergeLockStatusCmd, nil); err != nil {
		t.Fatalf("status after release: %v", err)
	}
	var notHeld statusOutput
	if err := json.Unmarshal(statusOut.Bytes(), &notHeld); err != nil {
		t.Fatalf("status JSON parse: %v: %s", err, statusOut.String())
	}
	if notHeld.Held {
		t.Errorf("expected held=false after release, got: %+v", notHeld)
	}
}

// TestMergeLock_ReleaseIdempotent — second release exits 0 even when no file.
func TestMergeLock_ReleaseIdempotent(t *testing.T) {
	dir := initMergeLockTestRepo(t)
	chdirForMergeLockTest(t, dir)
	resetMergeLockFlags()

	// First release (no lock present) — should succeed silently.
	var out bytes.Buffer
	mergeLockReleaseCmd.SetOut(&out)
	if err := mergeLockReleaseCmd.RunE(mergeLockReleaseCmd, nil); err != nil {
		t.Fatalf("first release: %v", err)
	}
	if !strings.Contains(out.String(), "not held") {
		t.Errorf("expected 'not held' marker for idempotent release: %q", out.String())
	}

	// Second release — still ok.
	out.Reset()
	if err := mergeLockReleaseCmd.RunE(mergeLockReleaseCmd, nil); err != nil {
		t.Fatalf("second release: %v", err)
	}
}

// TestMergeLock_Status_HumanText verifies the non-JSON output shape.
func TestMergeLock_Status_HumanText(t *testing.T) {
	dir := initMergeLockTestRepo(t)
	chdirForMergeLockTest(t, dir)
	resetMergeLockFlags()

	mergeLockAcquireTTL = 30 * time.Second
	mergeLockAcquireMeta = []string{"reason=human", "operator=worker"}
	var acquireOut bytes.Buffer
	mergeLockAcquireCmd.SetOut(&acquireOut)
	if err := mergeLockAcquireCmd.RunE(mergeLockAcquireCmd, nil); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	mergeLockStatusJSON = false
	var statusOut bytes.Buffer
	mergeLockStatusCmd.SetOut(&statusOut)
	if err := mergeLockStatusCmd.RunE(mergeLockStatusCmd, nil); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(statusOut.String(), "held (pid=") {
		t.Errorf("expected human held marker: %q", statusOut.String())
	}
	if !strings.Contains(statusOut.String(), "meta.reason=human") {
		t.Errorf("expected meta.reason in human output: %q", statusOut.String())
	}

	// Cleanup.
	mergeLockReleaseCmd.SetOut(&bytes.Buffer{})
	_ = mergeLockReleaseCmd.RunE(mergeLockReleaseCmd, nil)
}

// TestMergeLock_StaleClearOnAcquire — pre-existing stale state should be auto-cleared.
func TestMergeLock_StaleClearOnAcquire(t *testing.T) {
	dir := initMergeLockTestRepo(t)
	chdirForMergeLockTest(t, dir)
	resetMergeLockFlags()

	// Plant a stale state with a dead PID + old mtime.
	path, _ := gitpkg.MergeLockFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stale := &gitpkg.LockState{
		PID:        pickDeadPIDCmd(t),
		AcquiredAt: time.Now().Add(-30 * time.Minute),
		TTLSeconds: 60,
		Meta:       map[string]string{"reason": "stale"},
	}
	data, _ := json.Marshal(stale)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	old := time.Now().Add(-30 * time.Minute)
	_ = os.Chtimes(path, old, old)

	// Acquire should succeed despite the prior state.
	mergeLockAcquireTTL = 1 * time.Minute
	mergeLockAcquireMeta = []string{"reason=fresh"}
	var out bytes.Buffer
	mergeLockAcquireCmd.SetOut(&out)
	if err := mergeLockAcquireCmd.RunE(mergeLockAcquireCmd, nil); err != nil {
		t.Fatalf("acquire after stale: %v", err)
	}

	got, _, ok, err := gitpkg.ReadLockState(path)
	if err != nil || !ok {
		t.Fatalf("read: %v ok=%v", err, ok)
	}
	if got.Meta["reason"] != "fresh" {
		t.Errorf("stale state should have been replaced; got meta=%v", got.Meta)
	}

	// Cleanup.
	mergeLockReleaseCmd.SetOut(&bytes.Buffer{})
	_ = mergeLockReleaseCmd.RunE(mergeLockReleaseCmd, nil)
}

// TestParseMergeLockMeta covers the --meta flag parser.
func TestParseMergeLockMeta(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		input   []string
		want    map[string]string
		wantErr bool
	}{
		{"empty", nil, nil, false},
		{"single", []string{"reason=batch"}, map[string]string{"reason": "batch"}, false},
		{"multiple", []string{"reason=batch", "plan=P-0142"}, map[string]string{"reason": "batch", "plan": "P-0142"}, false},
		{"value-with-equals", []string{"raw=k=v"}, map[string]string{"raw": "k=v"}, false},
		{"missing-eq", []string{"orphan"}, nil, true},
		{"empty-key", []string{"=v"}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMergeLockMeta(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err want %v got %v", tc.wantErr, err)
			}
			if tc.wantErr {
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len: want %d got %d (%v)", len(tc.want), len(got), got)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("key %q: want %q got %q", k, v, got[k])
				}
			}
		})
	}
}

// TestMergeLock_HoldHeartbeatBumpsMtime verifies the --hold heartbeat goroutine
// touches the lockfile mtime on each tick. We start `acquire --hold` in a
// goroutine with a small heartbeat interval, then observe the mtime advance.
func TestMergeLock_HoldHeartbeatBumpsMtime(t *testing.T) {
	dir := initMergeLockTestRepo(t)
	chdirForMergeLockTest(t, dir)
	resetMergeLockFlags()

	// Tighten the heartbeat for test responsiveness; restore on exit.
	orig := mergeLockHeartbeatInterval
	mergeLockHeartbeatInterval = 50 * time.Millisecond
	t.Cleanup(func() { mergeLockHeartbeatInterval = orig })

	mergeLockAcquireTTL = 1 * time.Minute
	mergeLockAcquireHold = true

	// Acquire with --hold runs the heartbeat in the foreground; we run it
	// in a goroutine and shut it down via SIGTERM after observing mtime bump.
	var wg sync.WaitGroup
	wg.Add(1)
	runErr := make(chan error, 1)
	go func() {
		defer wg.Done()
		runErr <- mergeLockAcquireCmd.RunE(mergeLockAcquireCmd, nil)
	}()

	// Wait briefly for the lockfile to appear.
	path, _ := gitpkg.MergeLockFilePath()
	var before os.FileInfo
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fi, err := os.Stat(path)
		if err == nil {
			before = fi
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if before == nil {
		t.Fatal("lockfile never appeared")
	}

	// Wait for at least one heartbeat tick.
	time.Sleep(200 * time.Millisecond)
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !after.ModTime().After(before.ModTime()) {
		t.Errorf("expected heartbeat to bump mtime: before=%v after=%v", before.ModTime(), after.ModTime())
	}

	// Shut down the heartbeat by sending SIGTERM to ourselves.
	_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)

	// Wait for the goroutine to exit.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("acquire --hold did not exit on SIGTERM")
	}
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("acquire --hold returned err: %v", err)
		}
	default:
	}

	// Lockfile should have been removed on signal.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected lockfile removed after SIGTERM, stat err: %v", err)
	}
}
