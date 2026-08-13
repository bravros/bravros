package git

import (
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

// ─── pure cores ──────────────────────────────────────────────────────────────

func TestParseLsofPC(t *testing.T) {
	out := "p4711\ncclaude\np4720\ncnpm\np4720\ncnpm\n"
	procs, selfInside := parseLsofPC(out, 9999)
	if selfInside {
		t.Fatalf("selfInside = true, want false")
	}
	if len(procs) != 2 {
		t.Fatalf("got %d procs, want 2 (deduplicated): %+v", len(procs), procs)
	}
	if procs[0].PID != 4711 || procs[0].Command != "claude" {
		t.Errorf("procs[0] = %+v, want {4711 claude}", procs[0])
	}
	if procs[1].PID != 4720 || procs[1].Command != "npm" {
		t.Errorf("procs[1] = %+v, want {4720 npm}", procs[1])
	}
}

func TestParseLsofPCSelfExcluded(t *testing.T) {
	out := "p4711\ncclaude\np500\nckaisser\n"
	procs, selfInside := parseLsofPC(out, 500)
	if !selfInside {
		t.Fatalf("selfInside = false, want true (pid 500 is self)")
	}
	if len(procs) != 1 || procs[0].PID != 4711 {
		t.Fatalf("procs = %+v, want only pid 4711 (self excluded)", procs)
	}
}

func TestParseLsofPCEmpty(t *testing.T) {
	procs, selfInside := parseLsofPC("", 1)
	if len(procs) != 0 || selfInside {
		t.Fatalf("empty output: procs=%+v selfInside=%v, want none/false", procs, selfInside)
	}
}

func TestEvaluateLivenessGuard(t *testing.T) {
	live := []LiveProcess{{PID: 4711, Command: "claude"}}
	cases := []struct {
		name    string
		live    []LiveProcess
		checked bool
		force   bool
		wantErr bool
	}{
		{"live+checked refuses", live, true, false, true},
		{"force bypasses", live, true, true, false},
		{"unchecked never blocks", live, false, false, false},
		{"no live procs proceeds", nil, true, false, false},
		{"empty slice proceeds", []LiveProcess{}, true, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := evaluateLivenessGuard(tc.live, tc.checked, tc.force)
			if tc.wantErr && err == nil {
				t.Fatal("expected refusal error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected nil, got %v", err)
			}
			if tc.wantErr {
				var lerr *LivenessError
				if !errors.As(err, &lerr) {
					t.Fatalf("error %T is not *LivenessError", err)
				}
				if len(lerr.Live) != 1 || lerr.Live[0].PID != 4711 {
					t.Fatalf("LivenessError.Live = %+v, want the live pid list", lerr.Live)
				}
			}
		})
	}
}

// ─── integration: spawned sleeper inside a real worktree ────────────────────

// spawnSleeper starts a `sleep` process with its cwd inside dir and returns
// its pid plus a kill func. Waits for LiveProcessesIn to actually observe it
// (lsof visibility is not instant) before returning.
func spawnSleeper(t *testing.T, dir string) (int, func()) {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to spawn sleeper: %v", err)
	}
	kill := func() {
		cmd.Process.Kill()
		cmd.Wait()
	}
	// Wait until the guard can see it, so the test doesn't race lsof.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rep := LiveProcessesIn(dir)
		if !rep.Checked {
			kill()
			t.Skip("liveness tooling unavailable on this host (no lsof / no /proc)")
		}
		for _, p := range rep.Processes {
			if p.PID == cmd.Process.Pid {
				return cmd.Process.Pid, kill
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	kill()
	t.Fatalf("sleeper pid %d never became visible to LiveProcessesIn", cmd.Process.Pid)
	return 0, nil
}

// setupCleanupWorktree creates a repo + worktree and chdirs into the repo.
func setupCleanupWorktree(t *testing.T, branch string) (repoDir, wtPath string) {
	t.Helper()
	repoDir = initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(repoDir)
	t.Cleanup(func() { os.Chdir(origDir) })
	wtPath = repoDir + "-livewt"
	gitRun(t, repoDir, "git", "worktree", "add", "-b", branch, wtPath)
	t.Cleanup(func() {
		RunInDir(repoDir, "git", "worktree", "remove", "--force", wtPath)
	})
	return repoDir, wtPath
}

func TestWorktreeCleanupLivenessRefusal(t *testing.T) {
	_, wtPath := setupCleanupWorktree(t, "feat/live-refusal")
	pid, kill := spawnSleeper(t, wtPath)
	defer kill()

	// Live process inside → refusal, typed error, pid listed.
	_, err := WorktreeCleanup(wtPath, CleanupOpts{})
	if err == nil {
		t.Fatal("cleanup succeeded despite live process inside worktree")
	}
	var lerr *LivenessError
	if !errors.As(err, &lerr) {
		t.Fatalf("error %T (%v) is not *LivenessError", err, err)
	}
	found := false
	for _, p := range lerr.Live {
		if p.PID == pid {
			found = true
		}
	}
	if !found {
		t.Fatalf("refusal list %+v does not contain sleeper pid %d", lerr.Live, pid)
	}
	if !worktreeExists(wtPath) {
		t.Fatal("worktree was removed despite refusal")
	}

	// After the process exits → cleanup proceeds (test repo has no origin, so
	// the merge guard is indeterminate and never blocks).
	kill()
	waitGone(t, wtPath, pid)
	result, err := WorktreeCleanup(wtPath, CleanupOpts{})
	if err != nil {
		t.Fatalf("cleanup after process exit failed: %v", err)
	}
	if !result.Removed {
		t.Fatal("worktree not removed after process exit")
	}
}

// waitGone waits until pid is no longer reported inside dir.
func waitGone(t *testing.T, dir string, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rep := LiveProcessesIn(dir)
		alive := false
		for _, p := range rep.Processes {
			if p.PID == pid {
				alive = true
			}
		}
		if !alive {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("pid %d still reported inside %s after kill", pid, dir)
}

func TestWorktreeCleanupLivenessForceProceeds(t *testing.T) {
	_, wtPath := setupCleanupWorktree(t, "feat/live-force")
	pid, kill := spawnSleeper(t, wtPath)
	defer kill()

	result, err := WorktreeCleanup(wtPath, CleanupOpts{Force: true})
	if err != nil {
		t.Fatalf("--force cleanup failed: %v", err)
	}
	if !result.Removed {
		t.Fatal("--force cleanup did not remove worktree")
	}
	// The list is still reported even though the guard was bypassed.
	found := false
	for _, p := range result.LiveProcesses {
		if p.PID == pid {
			found = true
		}
	}
	if !found {
		t.Fatalf("force result live_processes %+v missing sleeper pid %d", result.LiveProcesses, pid)
	}
}

func TestWorktreeCleanupLivenessDryRunReports(t *testing.T) {
	_, wtPath := setupCleanupWorktree(t, "feat/live-dryrun")
	pid, kill := spawnSleeper(t, wtPath)
	defer kill()

	result, err := WorktreeCleanup(wtPath, CleanupOpts{DryRun: true})
	if err != nil {
		t.Fatalf("--dry-run failed: %v", err)
	}
	if !result.DryRun {
		t.Fatal("result.DryRun = false")
	}
	found := false
	for _, p := range result.LiveProcesses {
		if p.PID == pid {
			found = true
		}
	}
	if !found {
		t.Fatalf("dry-run live_processes %+v missing sleeper pid %d", result.LiveProcesses, pid)
	}
	if !worktreeExists(wtPath) {
		t.Fatal("--dry-run removed the worktree")
	}
}

func TestLiveProcessesInEmptyDir(t *testing.T) {
	dir := t.TempDir()
	rep := LiveProcessesIn(dir)
	if !rep.Checked {
		t.Skip("liveness tooling unavailable on this host (no lsof / no /proc)")
	}
	if len(rep.Processes) != 0 {
		t.Fatalf("expected no live processes in fresh temp dir, got %+v", rep.Processes)
	}
}
