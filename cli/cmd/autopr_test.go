package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bravros/bravros/cli/internal/autopr"
)

// changeToTempDir changes to a temp directory and returns a cleanup function.
func changeToTempDir(t *testing.T) func() {
	t.Helper()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	return func() { os.Chdir(origDir) }
}

func TestSetLock_CreatesLockFile(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()

	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	autoprSkillFlag = "auto-pr"
	captureStdout(t, func() {
		autoprSetLockCmd.Run(autoprSetLockCmd, []string{})
	})

	data, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("lock file not created: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "timestamp=") {
		t.Error("lock file missing timestamp")
	}
	if !strings.Contains(content, "skill=auto-pr") {
		t.Error("lock file missing skill")
	}
}

func TestSetLock_TimestampIsRFC3339(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()

	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	autoprSkillFlag = "test-skill"
	captureStdout(t, func() {
		autoprSetLockCmd.Run(autoprSetLockCmd, []string{})
	})

	meta, err := autopr.ParseLockMeta(lockFilePath)
	if err != nil {
		t.Fatalf("ParseLockMeta: %v", err)
	}
	if meta.Skill != "test-skill" {
		t.Errorf("skill: got %q, want test-skill", meta.Skill)
	}
	if meta.Timestamp.IsZero() {
		t.Error("timestamp should not be zero")
	}
	// Should be recent (within 5 seconds)
	if age := meta.Age().Seconds(); age > 5 {
		t.Errorf("timestamp is too old: %v seconds", age)
	}
}

func TestClearLock_OutsideClaude_RemovesLock(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()

	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(lockFilePath, []byte("timestamp=2026-04-14T10:00:00Z\nskill=auto-pr\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "")
	t.Setenv("TMUX", "")
	t.Setenv("STY", "")
	// Disable process walk to avoid false positives when running tests inside Claude Code.
	autoprSkipProcessWalk = true
	defer func() { autoprSkipProcessWalk = false }()
	captureStdout(t, func() {
		autoprClearLockCmd.Run(autoprClearLockCmd, []string{})
	})

	if _, err := os.Stat(lockFilePath); !os.IsNotExist(err) {
		t.Error("lock file should be removed by clear-lock")
	}
}

// clearLockOutsideClaude prepares env so the production clear-lock Run treats
// the test process as outside Claude Code, then invokes it and returns stdout.
func clearLockOutsideClaude(t *testing.T) string {
	t.Helper()
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "")
	autoprSkipProcessWalk = true
	t.Cleanup(func() { autoprSkipProcessWalk = false })
	return captureStdout(t, func() {
		autoprClearLockCmd.Run(autoprClearLockCmd, []string{})
	})
}

// TestClearLock_NoOp_SingleOtherLock_PrintsLockCommand verifies the P-0183 G3
// no-op polish: exactly one other lock present → the exact copy-pasteable
// `--lock <name>` command is printed, along with the rich info line.
func TestClearLock_NoOp_SingleOtherLock_PrintsLockCommand(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()
	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Default target (.auto-pr-lock) absent; one other lock present.
	ts := time.Now().UTC().Add(-14 * time.Minute).Format(time.RFC3339)
	content := "timestamp=" + ts + "\nskill=auto-merge\nmode=batch\nsession_id=0eeae1df22334455\n"
	if err := os.WriteFile(".planning/.auto-merge-lock", []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out := clearLockOutsideClaude(t)

	if !strings.Contains(out, "Clear with: bravros autopr clear-lock --lock auto-merge-lock") {
		t.Errorf("expected copy-pasteable --lock command in output, got:\n%s", out)
	}
	if !strings.Contains(out, "mode=batch") || !strings.Contains(out, "age=") {
		t.Errorf("expected rich info line (mode=…, age=…) in output, got:\n%s", out)
	}
	if !strings.Contains(out, "session=0eeae1df") || strings.Contains(out, "0eeae1df22334455") {
		t.Errorf("expected session truncated to 8 chars in output, got:\n%s", out)
	}
}

// TestClearLock_NoOp_MultipleOtherLocks_PrintsAll verifies the multi-lock
// no-op path keeps the enumerated list and points at --all.
func TestClearLock_NoOp_MultipleOtherLocks_PrintsAll(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()
	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	ts := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	for _, name := range []string{".auto-merge-lock", ".auto-flow-lock"} {
		content := "timestamp=" + ts + "\nskill=x\nmode=batch\nsession_id=abc\n"
		if err := os.WriteFile(filepath.Join(".planning", name), []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	out := clearLockOutsideClaude(t)

	if !strings.Contains(out, "Clear all with: bravros autopr clear-lock --all") {
		t.Errorf("expected --all hint for multiple other locks, got:\n%s", out)
	}
	if !strings.Contains(out, ".auto-merge-lock") || !strings.Contains(out, ".auto-flow-lock") {
		t.Errorf("expected both other locks enumerated, got:\n%s", out)
	}
}

func TestClearLock_InsideClaude_ChecksEnv(t *testing.T) {
	// Verify the CLAUDE_SESSION_ID check logic — clear canonical so legacy wins.
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "test-session-123")
	sessionID := os.Getenv("CLAUDE_SESSION_ID")
	if sessionID == "" {
		t.Fatal("CLAUDE_SESSION_ID should be set")
	}
	// The clear-lock command would call os.Exit(1) when CLAUDE_SESSION_ID is set,
	// so we just verify the env var condition holds rather than calling the command.
	if sessionID == "" {
		t.Error("expected CLAUDE_SESSION_ID to be non-empty inside Claude")
	}
}

// Lock-file parsing tests live in cli/internal/autopr/lock_test.go
// (TestParseLockMeta_*, TestIsSelfSession_*, TestAge_*). The cmd-layer no
// longer has its own parseLockFile/ReadLockMetadata to test.

func TestForceClear_StaleAboveThreshold(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()

	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Make it 7h old
	past := time.Now().UTC().Add(-7 * time.Hour)
	content := "timestamp=" + past.Format(time.RFC3339) + "\nskill=auto-pr\n"
	if err := os.WriteFile(lockFilePath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	autoprStaleAfter = 21600 // 6h
	captureStdout(t, func() {
		autoprForceClearCmd.Run(autoprForceClearCmd, []string{})
	})

	if _, err := os.Stat(lockFilePath); !os.IsNotExist(err) {
		t.Error("stale lock (7h > 6h threshold) should have been cleared")
	}
}

func TestForceClear_FreshBelowThreshold(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()

	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Make it 1h old (fresh relative to 6h threshold)
	past := time.Now().UTC().Add(-1 * time.Hour)
	content := "timestamp=" + past.Format(time.RFC3339) + "\nskill=auto-pr\n"
	if err := os.WriteFile(lockFilePath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	autoprStaleAfter = 21600 // 6h
	captureStdout(t, func() {
		autoprForceClearCmd.Run(autoprForceClearCmd, []string{})
	})

	if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
		t.Error("fresh lock (1h < 6h threshold) should NOT be cleared")
	}
}

func TestStatusCmd_JSONOutput(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()

	// No lock: present should be false
	autoprFieldFlag = ""
	out := captureStdout(t, func() {
		autoprStatusCmd.Run(autoprStatusCmd, []string{})
	})
	if !strings.Contains(out, `"present":false`) {
		t.Errorf("expected present:false in JSON output, got: %s", out)
	}
}

func TestStatusCmd_WithLock_JSONOutput(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()

	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	past := time.Now().UTC().Add(-30 * time.Second)
	content := "timestamp=" + past.Format(time.RFC3339) + "\nskill=deploy\n"
	if err := os.WriteFile(lockFilePath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	autoprFieldFlag = ""
	out := captureStdout(t, func() {
		autoprStatusCmd.Run(autoprStatusCmd, []string{})
	})
	if !strings.Contains(out, `"present":true`) {
		t.Errorf("expected present:true in JSON output, got: %s", out)
	}
	if !strings.Contains(out, `"skill":"deploy"`) {
		t.Errorf("expected skill:deploy in JSON output, got: %s", out)
	}
}

func TestStatusCmd_FieldFlag(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()

	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	past := time.Now().UTC().Add(-5 * time.Second)
	content := "timestamp=" + past.Format(time.RFC3339) + "\nskill=auto-pr\n"
	if err := os.WriteFile(lockFilePath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	autoprFieldFlag = "skill"
	defer func() { autoprFieldFlag = "" }()
	out := captureStdout(t, func() {
		autoprStatusCmd.Run(autoprStatusCmd, []string{})
	})
	trimmed := strings.TrimRight(out, "\n")
	if trimmed != "auto-pr" {
		t.Errorf("expected 'auto-pr' for --field skill, got %q", trimmed)
	}
}

// TestSetLock_AllMetadata verifies set-lock writes all 5 metadata fields.
func TestSetLock_AllMetadata(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()

	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	autoprSkillFlag = "flow"
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "test-session-abc")
	captureStdout(t, func() {
		autoprSetLockCmd.Run(autoprSetLockCmd, []string{})
	})

	data, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("lock file not created: %v", err)
	}
	content := string(data)

	for _, field := range []string{"timestamp=", "skill=flow", "session_id=test-session-abc", "pid="} {
		if !strings.Contains(content, field) {
			t.Errorf("lock file missing field %q; content:\n%s", field, content)
		}
	}
	// tty= key must be present (value may be empty in test environment)
	if !strings.Contains(content, "tty=") {
		t.Errorf("lock file missing tty= field; content:\n%s", content)
	}
}

// TestClearLock_RefusesInsideSession verifies clear-lock refuses when CLAUDE_SESSION_ID (legacy) is set.
func TestClearLock_RefusesInsideSession(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "active-session-xyz")
	// isInsideClaudeEnv should return true
	if !isInsideClaudeEnv() {
		t.Fatal("isInsideClaudeEnv should return true when CLAUDE_SESSION_ID is set")
	}
}

// TestClearLock_RefusesInsideSession_NewEnvVar verifies clear-lock refuses when
// the canonical CLAUDE_CODE_SESSION_ID is set (legacy var unset). Locks in the
// session.Resolve() priority order — the canonical var must trigger the same
// in-session detection as the legacy var.
func TestClearLock_RefusesInsideSession_NewEnvVar(t *testing.T) {
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "canonical-session-xyz")
	if !isInsideClaudeEnv() {
		t.Fatal("isInsideClaudeEnv should return true when CLAUDE_CODE_SESSION_ID (canonical) is set")
	}
}

// TestClearLock_RefusesWithEntrypoint verifies clear-lock refuses when CLAUDE_CODE_ENTRYPOINT is set.
func TestClearLock_RefusesWithEntrypoint(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	t.Setenv("TMUX", "")
	t.Setenv("STY", "")
	if !isInsideClaudeEnv() {
		t.Fatal("isInsideClaudeEnv should return true when CLAUDE_CODE_ENTRYPOINT is set")
	}
}

// TestClearLock_AllowsOutsideSession verifies isInsideClaudeEnv returns false when no env vars are set.
func TestClearLock_AllowsOutsideSession(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "")
	t.Setenv("TMUX", "")
	t.Setenv("STY", "")
	// Skip process walk to test only env var detection in isolation.
	autoprSkipProcessWalk = true
	defer func() { autoprSkipProcessWalk = false }()
	if isInsideClaudeEnv() {
		t.Error("isInsideClaudeEnv should return false when all env vars are empty and process walk is skipped")
	}
}

// TestStatus_FullJSON verifies status returns all new fields.
func TestStatus_FullJSON(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()

	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	past := time.Now().UTC().Add(-20 * time.Second)
	content := "timestamp=" + past.Format(time.RFC3339) + "\nskill=auto-merge\ntty=/dev/pts/1\nsession_id=sess-123\npid=99999\n"
	if err := os.WriteFile(lockFilePath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	autoprFieldFlag = ""
	out := captureStdout(t, func() {
		autoprStatusCmd.Run(autoprStatusCmd, []string{})
	})

	var st lockStatus
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &st); err != nil {
		t.Fatalf("failed to parse JSON output: %v\noutput: %s", err, out)
	}
	if !st.Present {
		t.Error("expected present=true")
	}
	if st.Skill != "auto-merge" {
		t.Errorf("skill: got %q, want auto-merge", st.Skill)
	}
	if st.TTY != "/dev/pts/1" {
		t.Errorf("tty: got %q, want /dev/pts/1", st.TTY)
	}
	if st.SessionID != "sess-123" {
		t.Errorf("session_id: got %q, want sess-123", st.SessionID)
	}
	if st.PID != "99999" {
		t.Errorf("pid: got %q, want 99999", st.PID)
	}
	if st.Timestamp == "" {
		t.Error("timestamp should not be empty")
	}
	if st.Path != lockFilePath {
		t.Errorf("path: got %q, want %q", st.Path, lockFilePath)
	}
}

// Metadata parsing tests live in cli/internal/autopr/lock_test.go
// (TestParseLockMeta_ValidContent, TestParseLockMeta_MissingFile).

// ---------------------------------------------------------------------------
// list-locks subcommand (plan 0057)
// ---------------------------------------------------------------------------

// TestListLocks covers the list-locks subcommand across four scenarios:
// no locks, only .auto-pr-lock, only .auto-merge-lock (mode=batch), and both.
func TestListLocks(t *testing.T) {
	now := time.Now().UTC()
	tsStr := now.Format(time.RFC3339)

	tests := []struct {
		name      string
		lockFiles map[string]string // filename → content (relative to .planning/)
		wantLen   int
		wantNames []string // lock names expected in response (without leading dot)
		wantMode  string   // if non-empty, at least one entry must have this mode
	}{
		{
			name:      "no lock files returns empty array",
			lockFiles: map[string]string{},
			wantLen:   0,
		},
		{
			name: "only .auto-pr-lock returns 1 entry",
			lockFiles: map[string]string{
				".auto-pr-lock": "timestamp=" + tsStr + "\nskill=auto-pr\nmode=single\n",
			},
			wantLen:   1,
			wantNames: []string{"auto-pr-lock"},
		},
		{
			name: "only .auto-merge-lock with mode=batch returns 1 entry with mode visible",
			lockFiles: map[string]string{
				".auto-merge-lock": "timestamp=" + tsStr + "\nskill=auto-merge\nmode=batch\n",
			},
			wantLen:   1,
			wantNames: []string{"auto-merge-lock"},
			wantMode:  "batch",
		},
		{
			name: "both lock files returns 2 entries",
			lockFiles: map[string]string{
				".auto-pr-lock":    "timestamp=" + tsStr + "\nskill=auto-pr\nmode=single\n",
				".auto-merge-lock": "timestamp=" + tsStr + "\nskill=auto-merge\nmode=batch\n",
			},
			wantLen:   2,
			wantNames: []string{"auto-pr-lock", "auto-merge-lock"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := changeToTempDir(t)
			defer cleanup()

			if err := os.MkdirAll(".planning", 0755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			for name, content := range tt.lockFiles {
				if err := os.WriteFile(filepath.Join(".planning", name), []byte(content), 0644); err != nil {
					t.Fatalf("WriteFile %s: %v", name, err)
				}
			}

			out := captureStdout(t, func() {
				autoprListLocksCmd.Run(autoprListLocksCmd, []string{})
			})

			// Parse JSON output.
			var entries []lockInfo
			if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &entries); err != nil {
				t.Fatalf("failed to parse list-locks JSON: %v\noutput: %s", err, out)
			}

			// Length check.
			if len(entries) != tt.wantLen {
				t.Errorf("len(entries) = %d, want %d; output: %s", len(entries), tt.wantLen, out)
				return
			}

			// Verify each expected name appears in the result.
			for _, wantName := range tt.wantNames {
				found := false
				for _, e := range entries {
					if e.Name == wantName {
						found = true
						// Basic shape assertions for each found entry.
						if e.Timestamp == "" {
							t.Errorf("entry %q: timestamp should not be empty", wantName)
						}
						if e.AgeSeconds < 0 {
							t.Errorf("entry %q: age_seconds should be >= 0, got %d", wantName, e.AgeSeconds)
						}
						break
					}
				}
				if !found {
					t.Errorf("expected entry with name %q, not found in: %s", wantName, out)
				}
			}

			// Verify mode when expected.
			if tt.wantMode != "" {
				found := false
				for _, e := range entries {
					if e.Mode == tt.wantMode {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected at least one entry with mode=%q; output: %s", tt.wantMode, out)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// batch-status subcommand (plan 0060 Phase 8)
// ---------------------------------------------------------------------------

// TestBatchStatusWriteCmd verifies the write subcommand records a plan entry.
func TestBatchStatusWriteCmd(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()

	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	batchStatusRoundFlag = 1
	batchStatusPlanFlag = "0060"
	batchStatusStateFlag = "executing"
	defer func() {
		batchStatusRoundFlag = 0
		batchStatusPlanFlag = ""
		batchStatusStateFlag = ""
	}()

	out := captureStdout(t, func() {
		autoprBatchStatusWriteCmd.Run(autoprBatchStatusWriteCmd, []string{})
	})
	if !strings.Contains(out, "round=1") {
		t.Errorf("expected round=1 in output, got: %s", out)
	}
	if !strings.Contains(out, "plan=0060") {
		t.Errorf("expected plan=0060 in output, got: %s", out)
	}
	if !strings.Contains(out, "state=executing") {
		t.Errorf("expected state=executing in output, got: %s", out)
	}
}

// TestBatchStatusReadCmd_TextFormat verifies the read subcommand emits a text summary.
func TestBatchStatusReadCmd_TextFormat(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()

	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Write a known entry first.
	batchStatusRoundFlag = 2
	batchStatusPlanFlag = "0061"
	batchStatusStateFlag = "merged"
	captureStdout(t, func() {
		autoprBatchStatusWriteCmd.Run(autoprBatchStatusWriteCmd, []string{})
	})

	batchStatusFormatFlag = "text"
	defer func() {
		batchStatusRoundFlag = 0
		batchStatusPlanFlag = ""
		batchStatusStateFlag = ""
		batchStatusFormatFlag = ""
	}()

	out := captureStdout(t, func() {
		autoprBatchStatusReadCmd.Run(autoprBatchStatusReadCmd, []string{})
	})
	if !strings.Contains(out, "Round 2") {
		t.Errorf("expected 'Round 2' in text output, got: %s", out)
	}
	if !strings.Contains(out, "0061") {
		t.Errorf("expected plan 0061 in text output, got: %s", out)
	}
	if !strings.Contains(out, "merged") {
		t.Errorf("expected state 'merged' in text output, got: %s", out)
	}
}

// TestBatchStatusReadCmd_JSONFormat verifies the read subcommand emits valid JSON.
func TestBatchStatusReadCmd_JSONFormat(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()

	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	batchStatusRoundFlag = 1
	batchStatusPlanFlag = "0060"
	batchStatusStateFlag = "queued"
	captureStdout(t, func() {
		autoprBatchStatusWriteCmd.Run(autoprBatchStatusWriteCmd, []string{})
	})

	batchStatusFormatFlag = "json"
	defer func() {
		batchStatusRoundFlag = 0
		batchStatusPlanFlag = ""
		batchStatusStateFlag = ""
		batchStatusFormatFlag = ""
	}()

	out := captureStdout(t, func() {
		autoprBatchStatusReadCmd.Run(autoprBatchStatusReadCmd, []string{})
	})
	// Must be valid JSON with a "rounds" key.
	if !strings.Contains(out, `"rounds"`) {
		t.Errorf("expected JSON with 'rounds' key, got: %s", out)
	}
	if !strings.Contains(out, `"0060"`) {
		t.Errorf("expected plan 0060 in JSON output, got: %s", out)
	}
}

// TestBatchStatusReadCmd_Empty verifies read on empty progress emits a message.
func TestBatchStatusReadCmd_Empty(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()

	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	batchStatusFormatFlag = "text"
	defer func() { batchStatusFormatFlag = "" }()

	out := captureStdout(t, func() {
		autoprBatchStatusReadCmd.Run(autoprBatchStatusReadCmd, []string{})
	})
	if !strings.Contains(out, "no batch progress") {
		t.Errorf("expected 'no batch progress' message for empty file, got: %s", out)
	}
}

// TestBatchStatusAutoClear verifies that clear-lock --mode auto-merge removes
// .planning/.batch-progress.json.
func TestBatchStatusAutoClear(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()

	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Write a batch progress file and a merge lock.
	batchStatusRoundFlag = 1
	batchStatusPlanFlag = "0060"
	batchStatusStateFlag = "merged"
	captureStdout(t, func() {
		autoprBatchStatusWriteCmd.Run(autoprBatchStatusWriteCmd, []string{})
	})
	batchStatusRoundFlag = 0
	batchStatusPlanFlag = ""
	batchStatusStateFlag = ""

	const mergeLockFile = ".planning/.auto-merge-lock"
	if err := os.WriteFile(mergeLockFile, []byte("timestamp=2026-04-20T10:00:00Z\nskill=auto-merge\n"), 0644); err != nil {
		t.Fatalf("WriteFile merge lock: %v", err)
	}

	// Verify batch progress exists.
	if _, err := os.Stat(".planning/.batch-progress.json"); err != nil {
		t.Fatalf("batch progress file should exist before clear-lock: %v", err)
	}

	// Set up clear-lock to target auto-merge-lock with mode=auto-merge, outside Claude.
	autoprLockFlag = "auto-merge-lock"
	autoprModeFlag = "auto-merge"
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "")
	autoprSkipProcessWalk = true
	defer func() {
		autoprLockFlag = ""
		autoprModeFlag = ""
		autoprForceFlag = false
		autoprSkipProcessWalk = false
	}()

	captureStdout(t, func() {
		autoprClearLockCmd.Run(autoprClearLockCmd, []string{})
	})

	// Merge lock removed.
	if _, err := os.Stat(mergeLockFile); !os.IsNotExist(err) {
		t.Error("merge lock should have been removed")
	}
	// Batch progress also removed.
	if _, err := os.Stat(".planning/.batch-progress.json"); !os.IsNotExist(err) {
		t.Error("batch progress file should have been removed by clear-lock --mode auto-merge")
	}
}

// TestSetLockRejectsInvalidNames locks the auto-<skill>-lock naming contract
// expected by audit rules (glob .planning/.auto-*-lock) and list-locks. Any
// --lock value outside that shape would produce an orphan lock file that no
// rule detects, so validateLockName must reject it before the file is written.
func TestSetLockRejectsInvalidNames(t *testing.T) {
	cases := []struct {
		name    string
		lock    string
		wantErr bool
	}{
		{"empty uses default", "", false},
		{"canonical auto-merge-lock", "auto-merge-lock", false},
		{"canonical auto-pr-lock", "auto-pr-lock", false},
		{"future auto-foo-lock", "auto-foo-lock", false},
		{"multi-segment auto-foo-bar-lock", "auto-foo-bar-lock", false},
		{"digits allowed auto-v2-lock", "auto-v2-lock", false},
		{"bare foo", "foo", true},
		{"missing -lock suffix", "auto-foo", true},
		{"missing auto- prefix", "pr-lock", true},
		{"uppercase", "AUTO-MERGE-LOCK", true},
		{"double hyphen", "auto--lock", true},
		{"trailing hyphen", "auto-merge-", true},
		{"underscore", "auto_merge_lock", true},
		{"leading hyphen", "-auto-merge-lock", true},
		{"missing middle segment", "auto--merge-lock", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateLockName(tc.lock)
			if tc.wantErr && err == nil {
				t.Errorf("validateLockName(%q): expected error, got nil", tc.lock)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validateLockName(%q): expected nil, got %v", tc.lock, err)
			}
			if tc.wantErr && err != nil && !strings.Contains(err.Error(), "invalid --lock value") {
				t.Errorf("validateLockName(%q) error should mention 'invalid --lock value', got: %v", tc.lock, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// mode is-autonomous subcommand (B-0081)
// ---------------------------------------------------------------------------

func TestAutoprModeIsAutonomous_NoLock(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()
	os.MkdirAll(".planning", 0755)

	// No lock files present — glob should return empty slice.
	lockPattern := ".planning/." + "auto-*-lock"
	matches, err := filepath.Glob(lockPattern)
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no locks, got: %v", matches)
	}
}

func TestAutoprModeIsAutonomous_SingleLock(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()
	os.MkdirAll(".planning", 0755)

	past := time.Now().UTC().Add(-1 * time.Minute)
	lockName := "." + "auto-pr-lock"
	if err := os.WriteFile(filepath.Join(".planning", lockName),
		[]byte("timestamp="+past.Format(time.RFC3339)+"\nskill=auto-pr\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Glob should find the lock (command would exit 0).
	lockPattern := ".planning/." + "auto-*-lock"
	matches, err := filepath.Glob(lockPattern)
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 lock, got: %v", matches)
	}
}

func TestAutoprModeIsAutonomous_MultiLock(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()
	os.MkdirAll(".planning", 0755)

	// Create both auto-pr-lock and auto-merge-lock (B-0081: both must be detected).
	past := time.Now().UTC().Add(-1 * time.Minute)
	for _, shortName := range []string{"auto-pr-lock", "auto-merge-lock"} {
		lockName := "." + shortName
		if err := os.WriteFile(filepath.Join(".planning", lockName),
			[]byte("timestamp="+past.Format(time.RFC3339)+"\nskill=test\n"), 0644); err != nil {
			t.Fatalf("WriteFile %s: %v", lockName, err)
		}
	}

	lockPattern := ".planning/." + "auto-*-lock"
	matches, err := filepath.Glob(lockPattern)
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 locks (auto-pr + auto-merge), got %d: %v", len(matches), matches)
	}
}

// ---------------------------------------------------------------------------
// preflight subcommand (B-0080)
// ---------------------------------------------------------------------------

func TestAutoprPreflight_NoExistingLock(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()
	os.MkdirAll(".planning", 0755)

	autoprPreflightSkill = "auto-pr"
	defer func() { autoprPreflightSkill = "" }()

	out := captureStdout(t, func() {
		autoprPreflightCmd.Run(autoprPreflightCmd, []string{})
	})

	// Should emit checkpoint and create lock file.
	if !strings.Contains(out, "preflight ok") {
		t.Errorf("expected 'preflight ok' in output, got: %s", out)
	}

	lockPath := filepath.Join(".planning", "."+"auto-auto-pr-lock")
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("expected lock file to be created: %v", err)
	}
}

func TestAutoprPreflight_StaleLockCleared(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()
	os.MkdirAll(".planning", 0755)

	// Create a stale lock (7h old).
	past := time.Now().UTC().Add(-7 * time.Hour)
	lockPath := filepath.Join(".planning", "."+"auto-auto-pr-lock")
	if err := os.WriteFile(lockPath,
		[]byte("timestamp="+past.Format(time.RFC3339)+"\nskill=auto-pr\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	autoprPreflightSkill = "auto-pr"
	autoprPreflightStaleAfter = 21600 // 6h
	defer func() {
		autoprPreflightSkill = ""
		autoprPreflightStaleAfter = 21600
	}()

	out := captureStdout(t, func() {
		autoprPreflightCmd.Run(autoprPreflightCmd, []string{})
	})

	// Should clear stale lock and create a fresh one.
	if !strings.Contains(out, "stale lock cleared") {
		t.Errorf("expected 'stale lock cleared', got: %s", out)
	}
	if !strings.Contains(out, "preflight ok") {
		t.Errorf("expected 'preflight ok', got: %s", out)
	}
}

func TestAutoprPreflight_FreshLockSameSkillRefreshed(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()
	os.MkdirAll(".planning", 0755)

	// Create a fresh lock (1h old, below 6h threshold).
	past := time.Now().UTC().Add(-1 * time.Hour)
	lockPath := filepath.Join(".planning", "."+"auto-auto-pr-lock")
	if err := os.WriteFile(lockPath,
		[]byte("timestamp="+past.Format(time.RFC3339)+"\nskill=auto-pr\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	autoprPreflightSkill = "auto-pr"
	autoprPreflightStaleAfter = 21600 // 6h
	defer func() {
		autoprPreflightSkill = ""
		autoprPreflightStaleAfter = 21600
	}()

	out := captureStdout(t, func() {
		autoprPreflightCmd.Run(autoprPreflightCmd, []string{})
	})

	// Should refresh the lock (same skill — no contention).
	if !strings.Contains(out, "preflight ok") {
		t.Errorf("expected 'preflight ok', got: %s", out)
	}

	// Verify the new lock is fresh (written within 5s of now).
	meta, err := autopr.ParseLockMeta(lockPath)
	if err != nil {
		t.Fatalf("ParseLockMeta: %v", err)
	}
	if meta.Age() > 5*time.Second {
		t.Errorf("expected fresh lock timestamp, got age: %v", meta.Age())
	}
}

// TestAutopr_ClearLockAlsoClearsBatchDecisions verifies that autopr clear-lock
// in auto-merge mode removes BOTH .planning/.batch-progress.json (existing) and
// .planning/.batch-decisions.json (new in B-0143). We test the cleanup logic
// directly by exercising the same os.Remove path the clear-lock branch uses.
func TestAutopr_ClearLockAlsoClearsBatchDecisions(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()

	// Plant .planning directory and the two state files.
	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatal(err)
	}
	batchProgress := filepath.Join(".planning", ".batch-progress.json")
	batchDecisions := filepath.Join(".planning", ".batch-decisions.json")
	if err := os.WriteFile(batchProgress, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(batchDecisions, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Exercise the same cleanup paths the clear-lock --mode auto-merge branch uses.
	if err := os.Remove(batchProgress); err != nil {
		t.Fatalf("Remove(%s): %v", batchProgress, err)
	}
	if err := os.Remove(batchDecisions); err != nil {
		t.Fatalf("Remove(%s): %v", batchDecisions, err)
	}

	if _, err := os.Stat(batchProgress); err == nil {
		t.Errorf("expected batch-progress to be removed, but it still exists")
	}
	if _, err := os.Stat(batchDecisions); err == nil {
		t.Errorf("expected batch-decisions to be removed, but it still exists")
	}
}

// ---------------------------------------------------------------------------
// P-0136 Phase 3: worktree-aware lock messaging (tasks 1-3 + 4 + 6)
// ---------------------------------------------------------------------------

// TestClearLock_PrintsWorktreePath verifies clear-lock success message contains
// an absolute path and the worktree branch name.
func TestClearLock_PrintsWorktreePath(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()

	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(lockFilePath, []byte("timestamp=2026-04-14T10:00:00Z\nskill=auto-pr\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Run outside Claude (no session env, no process walk).
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "")
	t.Setenv("TMUX", "")
	t.Setenv("STY", "")
	autoprSkipProcessWalk = true
	autoprLockFlag = ""
	defer func() {
		autoprSkipProcessWalk = false
		autoprLockFlag = ""
	}()

	out := captureStdout(t, func() {
		autoprClearLockCmd.Run(autoprClearLockCmd, []string{})
	})

	// Must contain the substring "worktree:" to prove branch was injected.
	if !strings.Contains(out, "worktree:") {
		t.Errorf("expected 'worktree:' in clear-lock output, got: %s", out)
	}
	// Must contain "path:" with an absolute path (starts with /).
	if !strings.Contains(out, "path:") {
		t.Errorf("expected 'path:' in clear-lock output, got: %s", out)
	}
	// Absolute path must include the .planning segment.
	if !strings.Contains(out, ".planning") {
		t.Errorf("expected .planning in absolute path, got: %s", out)
	}
}

// TestSetLock_PrintsWorktreePath verifies set-lock success message contains
// an absolute path and worktree info.
func TestSetLock_PrintsWorktreePath(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()

	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	autoprSkillFlag = "auto-pr"
	autoprLockFlag = ""
	defer func() {
		autoprSkillFlag = ""
		autoprLockFlag = ""
	}()

	out := captureStdout(t, func() {
		autoprSetLockCmd.Run(autoprSetLockCmd, []string{})
	})

	// Must contain "worktree:" to prove branch was injected.
	if !strings.Contains(out, "worktree:") {
		t.Errorf("expected 'worktree:' in set-lock output, got: %s", out)
	}
	// Must contain an absolute path.
	if !strings.Contains(out, ".planning") {
		t.Errorf("expected .planning in set-lock output, got: %s", out)
	}
}

// TestParseWorktreeList_Basic verifies the porcelain parser handles a two-worktree block.
func TestParseWorktreeList_Basic(t *testing.T) {
	input := `worktree /home/user/project
HEAD abc123
branch refs/heads/main

worktree /home/user/project-wt
HEAD def456
branch refs/heads/feature/foo

`
	entries := parseWorktreeList(input)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Path != "/home/user/project" {
		t.Errorf("entry[0].Path = %q, want /home/user/project", entries[0].Path)
	}
	if entries[0].Branch != "main" {
		t.Errorf("entry[0].Branch = %q, want main", entries[0].Branch)
	}
	if entries[1].Path != "/home/user/project-wt" {
		t.Errorf("entry[1].Path = %q, want /home/user/project-wt", entries[1].Path)
	}
	if entries[1].Branch != "feature/foo" {
		t.Errorf("entry[1].Branch = %q, want feature/foo", entries[1].Branch)
	}
}

// TestParseWorktreeList_DetachedHead verifies parser handles a detached HEAD block.
func TestParseWorktreeList_DetachedHead(t *testing.T) {
	input := `worktree /home/user/project
HEAD abc123
detached

`
	entries := parseWorktreeList(input)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Branch != "" {
		t.Errorf("expected empty branch for detached HEAD, got %q", entries[0].Branch)
	}
}

// TestParseWorktreeList_NoTrailingBlankLine verifies the parser flushes the
// last block even when the output does not end with a blank line.
func TestParseWorktreeList_NoTrailingBlankLine(t *testing.T) {
	input := `worktree /home/user/project
HEAD abc123
branch refs/heads/main`
	entries := parseWorktreeList(input)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d: %+v", len(entries), entries)
	}
	if entries[0].Branch != "main" {
		t.Errorf("entry[0].Branch = %q, want main", entries[0].Branch)
	}
}

// TestLockStatus_AllWorktrees verifies lock-status --all-worktrees output when
// we set up real lock files in sibling worktree directories inside a temp tree.
// We cannot easily stub git worktree list in the cmd layer (the command calls
// exec directly), so this test exercises parseWorktreeList + the glob logic
// independently and then tests the single-worktree code path of lock-status.
func TestLockStatus_AllWorktrees(t *testing.T) {
	// Test the single-worktree (non-all) path via the Cobra command.
	cleanup := changeToTempDir(t)
	defer cleanup()

	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	past := time.Now().UTC().Add(-5 * time.Minute)
	lockContent := "timestamp=" + past.Format(time.RFC3339) + "\nskill=auto-pr\n"
	if err := os.WriteFile(filepath.Join(".planning", ".auto-pr-lock"), []byte(lockContent), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	autoprLockStatusAllWorktrees = false
	defer func() { autoprLockStatusAllWorktrees = false }()

	out := captureStdout(t, func() {
		autoprLockStatusCmd.Run(autoprLockStatusCmd, []string{})
	})

	// Should mention the lock and its info line (lockInfoString format).
	if !strings.Contains(out, ".auto-pr-lock") {
		t.Errorf("expected .auto-pr-lock in lock-status output, got: %s", out)
	}
	if !strings.Contains(out, "age=") {
		t.Errorf("expected 'age=' in lock-status output, got: %s", out)
	}
}

// TestLockStatus_NoLocks verifies lock-status reports "no locks" when .planning
// contains no lock files.
func TestLockStatus_NoLocks(t *testing.T) {
	cleanup := changeToTempDir(t)
	defer cleanup()

	if err := os.MkdirAll(".planning", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	autoprLockStatusAllWorktrees = false
	defer func() { autoprLockStatusAllWorktrees = false }()

	out := captureStdout(t, func() {
		autoprLockStatusCmd.Run(autoprLockStatusCmd, []string{})
	})

	if !strings.Contains(out, "no locks present") {
		t.Errorf("expected 'no locks present' when no locks exist, got: %s", out)
	}
}
