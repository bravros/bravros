package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsMainMerge(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		expected bool
	}{
		// Protected: explicit main/master refspecs.
		{"push main", "git push origin main", true},
		{"push master", "git push origin master", true},
		{"push -u main", "git push -u origin main", true},
		{"push HEAD:main", "git push origin HEAD:main", true},
		{"force push main:main", "git push origin +main:main", true},
		{"push refs/heads/main", "git push origin refs/heads/main", true},

		// Allowed: homolog is a routine target for /push, /hotfix, /finish.
		{"push homolog", "git push origin homolog", false},
		{"push -u homolog", "git push -u origin homolog", false},
		{"push feature branch", "git push origin feat/test-branch", false},

		// Allowed: "main" as a substring of a longer branch name.
		{"branch containing main", "git push origin fix/maintain-cache", false},
		{"branch prefixed main", "git push origin mainline-experiment", false},

		// Not a push or a merge at all.
		{"commit mentioning main", "git commit -m 'update main branch docs'", false},
		{"status", "git status", false},
		{"npm test", "npm test", false},
		{"echoed merge", "echo 'gh pr merge'", false},
		{"subshell with git show origin/main", `(sed -n '80,100p' cli/cmd/active_command.go; echo "=== pre-existing? ==="; git stash list >/dev/null; git show origin/main:cli/cmd/police_test.go >/dev/null 2>&1 && echo "exists on main" || echo "new file (not on main)")`, false},
		{"quoted main target", `git push origin "main"`, true},
		{"single quoted main target", `git push origin 'main'`, true},
		{"commit message with semicolon", `git commit -m "fix bug; git push origin main"`, false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMainMerge(tt.cmd); got != tt.expected {
				t.Errorf("isMainMerge(%q) = %v; want %v", tt.cmd, got, tt.expected)
			}
		})
	}
}

// A bare `git push` carries no refspec, so the current branch decides. The
// helper is exercised directly against a scratch repo rather than through
// isMainMerge, which would read the branch of whatever repo the test runs in.
func TestPushTargetsProtected_BareePushFollowsCurrentBranch(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"commit", "-q", "--allow-empty", "-m", "seed"},
	} {
		c := exec.Command("git", args...)
		c.Dir = repo
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if err := c.Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}

	restore, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(restore) })

	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	if !pushTargetsProtected([]string{"git", "push"}) {
		t.Error("bare push while on main should be protected")
	}

	c := exec.Command("git", "checkout", "-q", "-b", "homolog")
	c.Dir = repo
	if err := c.Run(); err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if pushTargetsProtected([]string{"git", "push"}) {
		t.Error("bare push while on homolog must not be protected")
	}
}

func TestStartsWith(t *testing.T) {
	tests := []struct {
		seg      []string
		words    []string
		expected bool
	}{
		{[]string{"gh", "pr", "merge", "62"}, []string{"gh", "pr", "merge"}, true},
		{[]string{"GH_TOKEN=x", "gh", "pr", "merge"}, []string{"gh", "pr", "merge"}, true},
		{[]string{"echo", "gh", "pr", "merge"}, []string{"gh", "pr", "merge"}, false},
		{[]string{"gh", "pr", "view"}, []string{"gh", "pr", "merge"}, false},
		{[]string{"gh"}, []string{"gh", "pr", "merge"}, false},
	}
	for _, tt := range tests {
		if got := startsWith(tt.seg, tt.words...); got != tt.expected {
			t.Errorf("startsWith(%v, %v) = %v; want %v", tt.seg, tt.words, got, tt.expected)
		}
	}
}

func TestCommandSegments(t *testing.T) {
	got := commandSegments("cd repo && git push origin main")
	if len(got) != 2 || got[1][0] != "git" {
		t.Fatalf("commandSegments split = %v; want second segment to start at git", got)
	}
	if !isMainMerge("cd repo && git push origin main") {
		t.Error("a chained push to main must still be caught")
	}
}

func TestPoliceUnlock_RefusedInClaudeSession(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Test CLAUDE_CODE_SESSION_ID
	t.Setenv("CLAUDE_CODE_SESSION_ID", "session-code-123")
	t.Setenv("CLAUDE_SESSION_ID", "")
	policeUnlockCmd.SetOut(&bytes.Buffer{})
	policeUnlockCmd.SetErr(&bytes.Buffer{})
	if err := policeUnlockCmd.RunE(policeUnlockCmd, nil); err == nil {
		t.Fatal("expected error when running unlock inside CLAUDE_CODE_SESSION_ID")
	}

	// Test CLAUDE_SESSION_ID
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "session-claude-123")
	if err := policeUnlockCmd.RunE(policeUnlockCmd, nil); err == nil {
		t.Fatal("expected error when running unlock inside CLAUDE_SESSION_ID")
	}
}

func TestPoliceUnlock_Revoke_Status_Flow(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "")

	// 1. Check initial status (no token)
	var out bytes.Buffer
	policeStatusCmd.SetOut(&out)
	if err := policeStatusCmd.RunE(policeStatusCmd, nil); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out.String(), "MISSING or INVALID") {
		t.Errorf("expected MISSING status, got %q", out.String())
	}

	// 2. Unlock (mint token)
	out.Reset()
	policeUnlockCmd.SetOut(&out)
	if err := policeUnlockCmd.RunE(policeUnlockCmd, nil); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if !strings.Contains(out.String(), "Police token minted") {
		t.Errorf("expected minted message, got %q", out.String())
	}

	// Verify token file on disk
	tokPath := filepath.Join(tempHome, ".claude", "state", "police-token")
	data, err := os.ReadFile(tokPath)
	if err != nil {
		t.Fatalf("read token: %v", err)
	}
	if strings.TrimSpace(string(data)) != "valid" {
		t.Errorf("token content = %q; want 'valid'", string(data))
	}

	// 3. Status should now be VALID
	out.Reset()
	policeStatusCmd.SetOut(&out)
	if err := policeStatusCmd.RunE(policeStatusCmd, nil); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out.String(), "VALID") {
		t.Errorf("expected VALID status, got %q", out.String())
	}

	// 4. Revoke token
	out.Reset()
	policeRevokeCmd.SetOut(&out)
	if err := policeRevokeCmd.RunE(policeRevokeCmd, nil); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if !strings.Contains(out.String(), "revoked") {
		t.Errorf("expected revoked message, got %q", out.String())
	}

	// Verify token file was removed
	if _, err := os.Stat(tokPath); !os.IsNotExist(err) {
		t.Errorf("token file still exists after revoke")
	}

	// 5. Status should again be MISSING
	out.Reset()
	policeStatusCmd.SetOut(&out)
	if err := policeStatusCmd.RunE(policeStatusCmd, nil); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out.String(), "MISSING or INVALID") {
		t.Errorf("expected MISSING status, got %q", out.String())
	}
}

func TestPoliceToken_Expiry(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	tokPath := filepath.Join(tempHome, ".claude", "state", "police-token")
	if err := os.MkdirAll(filepath.Dir(tokPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokPath, []byte("valid\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Set mod time to 15 minutes ago (> 10 min TTL)
	fifteenMinutesAgo := time.Now().Add(-15 * time.Minute)
	if err := os.Chtimes(tokPath, fifteenMinutesAgo, fifteenMinutesAgo); err != nil {
		t.Fatal(err)
	}

	if hasValidPoliceToken() {
		t.Errorf("expected hasValidPoliceToken() to return false for expired token")
	}

	// Verify file was auto-removed on check
	if _, err := os.Stat(tokPath); !os.IsNotExist(err) {
		t.Errorf("expired token file was not removed")
	}
}

func TestPolicePreToolUse_BlockedWithoutToken(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("BRAVROS_POLICE_STANDDOWN", "")
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")

	payload := `{"tool_name":"bash","tool_input":{"command":"git push origin main"}}`
	var out bytes.Buffer
	policePreToolUseCmd.SetIn(strings.NewReader(payload))
	policePreToolUseCmd.SetOut(&out)
	policePreToolUseCmd.SetErr(&bytes.Buffer{})

	if err := policePreToolUseCmd.RunE(policePreToolUseCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	// The command always exits rc=0; the block is a JSON envelope printed to
	// stdout carrying exitCode:2 — assert on that stdout content directly.
	raw := out.String()
	if !strings.Contains(raw, "Police Block") {
		t.Errorf("expected Police Block in stdout, got %q", raw)
	}
	if !strings.Contains(raw, `"exitCode":2`) {
		t.Errorf("expected exitCode 2 in stdout, got %q", raw)
	}

	var res struct {
		Stdout string `json:"stdout"`
		Stderr string `json:"stderr"`
		Exit   int    `json:"exitCode"`
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("failed to unmarshal block json: %v, raw output: %q", err, raw)
	}
	if res.Exit != 2 {
		t.Errorf("exitCode = %d; want 2", res.Exit)
	}
	if !strings.Contains(res.Stderr, "Police Block") {
		t.Errorf("expected Police Block in stderr, got %q", res.Stderr)
	}
}

func TestPolicePreToolUse_PermittedWithToken(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("BRAVROS_POLICE_STANDDOWN", "")
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")

	// Create valid token
	tokPath := filepath.Join(tempHome, ".claude", "state", "police-token")
	_ = os.MkdirAll(filepath.Dir(tokPath), 0755)
	_ = os.WriteFile(tokPath, []byte("valid\n"), 0644)

	payload := `{"tool_name":"bash","tool_input":{"command":"gh pr merge 123 --auto --squash"}}`
	var out bytes.Buffer
	policePreToolUseCmd.SetIn(strings.NewReader(payload))
	policePreToolUseCmd.SetOut(&out)

	if err := policePreToolUseCmd.RunE(policePreToolUseCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if out.Len() != 0 {
		t.Errorf("expected no output (permitted), got %q", out.String())
	}
}

func TestPolicePreToolUse_PermittedWithStandDownEnv(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("BRAVROS_POLICE_STANDDOWN", "1")
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")

	payload := `{"tool_name":"bash","tool_input":{"command":"git push origin main"}}`
	var out bytes.Buffer
	policePreToolUseCmd.SetIn(strings.NewReader(payload))
	policePreToolUseCmd.SetOut(&out)

	if err := policePreToolUseCmd.RunE(policePreToolUseCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if out.Len() != 0 {
		t.Errorf("expected no output when standdown env is active, got %q", out.String())
	}
}

func TestPolicePreToolUse_PermittedWithStandDownMarker(t *testing.T) {
	sessionID := "test-session-sd-1"
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", sessionID)
	t.Setenv("BRAVROS_POLICE_STANDDOWN", "")
	t.Setenv("HOME", t.TempDir())

	// Create valid standdown marker
	sdPath := standDownPath(sessionID)
	_ = os.MkdirAll(filepath.Dir(sdPath), 0755)
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(sdPath)) })

	markerData := `{"session_id":"` + sessionID + `","expires_at":"` + time.Now().Add(1*time.Hour).Format(time.RFC3339) + `"}`
	if err := os.WriteFile(sdPath, []byte(markerData), 0644); err != nil {
		t.Fatal(err)
	}

	payload := `{"tool_name":"bash","tool_input":{"command":"git push origin homolog"}}`
	var out bytes.Buffer
	policePreToolUseCmd.SetIn(strings.NewReader(payload))
	policePreToolUseCmd.SetOut(&out)

	if err := policePreToolUseCmd.RunE(policePreToolUseCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if out.Len() != 0 {
		t.Errorf("expected permitted when standdown marker is active, got %q", out.String())
	}
}

func TestPolicePreToolUse_NonBash_Permitted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BRAVROS_POLICE_STANDDOWN", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "")

	payload := `{"tool_name":"readFile","tool_input":{"command":"git push origin main"}}`
	var out bytes.Buffer
	policePreToolUseCmd.SetIn(strings.NewReader(payload))
	policePreToolUseCmd.SetOut(&out)

	if err := policePreToolUseCmd.RunE(policePreToolUseCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if out.Len() != 0 {
		t.Errorf("expected non-bash tool to be permitted, got %q", out.String())
	}
}

func TestPolicePreToolUse_NonMainBash_Permitted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BRAVROS_POLICE_STANDDOWN", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "")

	payload := `{"tool_name":"bash","tool_input":{"command":"git checkout -b feat/my-feature"}}`
	var out bytes.Buffer
	policePreToolUseCmd.SetIn(strings.NewReader(payload))
	policePreToolUseCmd.SetOut(&out)

	if err := policePreToolUseCmd.RunE(policePreToolUseCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if out.Len() != 0 {
		t.Errorf("expected non-main command to be permitted, got %q", out.String())
	}
}

func TestPoliceStandDown_FullLifecycle(t *testing.T) {
	sessionID := "test-sd-lifecycle-session"
	// resolveSession prefers CLAUDE_CODE_SESSION_ID; clear it so the test does
	// not resolve to the ambient session when run inside Claude Code.
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", sessionID)
	t.Setenv("BRAVROS_POLICE_STANDDOWN", "")

	sdDir := filepath.Dir(standDownPath(sessionID))
	t.Cleanup(func() { os.RemoveAll(sdDir) })

	// 1. Initial status - inactive
	var out bytes.Buffer
	policeStandDownStatusCmd.SetOut(&out)
	if err := policeStandDownStatusCmd.RunE(policeStandDownStatusCmd, nil); err != nil {
		t.Fatalf("status: %v", err)
	}
	var st struct {
		Active    bool   `json:"active"`
		Source    string `json:"source"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(out.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if st.Active {
		t.Errorf("expected standdown to be initially inactive")
	}

	// 2. Enable standdown
	out.Reset()
	standDownTTLFlag = 2 * time.Hour
	policeStandDownOnCmd.SetOut(&out)
	if err := policeStandDownOnCmd.RunE(policeStandDownOnCmd, nil); err != nil {
		t.Fatalf("on: %v", err)
	}
	if !strings.Contains(out.String(), "Police stand-down ON") {
		t.Errorf("expected ON message, got %q", out.String())
	}

	// 3. Status should now be active from marker
	out.Reset()
	if err := policeStandDownStatusCmd.RunE(policeStandDownStatusCmd, nil); err != nil {
		t.Fatalf("status: %v", err)
	}
	if err := json.Unmarshal(out.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if !st.Active || st.Source != "marker" || st.SessionID != sessionID {
		t.Errorf("status = %+v; want Active=true, Source=marker, SessionID=%s", st, sessionID)
	}

	// 4. Disable standdown
	out.Reset()
	policeStandDownOffCmd.SetOut(&out)
	if err := policeStandDownOffCmd.RunE(policeStandDownOffCmd, nil); err != nil {
		t.Fatalf("off: %v", err)
	}
	if !strings.Contains(out.String(), "marker cleared") {
		t.Errorf("expected cleared message, got %q", out.String())
	}

	// 5. Status should be inactive again
	out.Reset()
	if err := policeStandDownStatusCmd.RunE(policeStandDownStatusCmd, nil); err != nil {
		t.Fatalf("status: %v", err)
	}
	if err := json.Unmarshal(out.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if st.Active {
		t.Errorf("expected standdown to be inactive after off")
	}
}

func TestPoliceStandDown_ExpiredMarker_AutoCleaned(t *testing.T) {
	sessionID := "test-sd-expired"
	// resolveSession prefers CLAUDE_CODE_SESSION_ID; clear it so the test does
	// not resolve to the ambient session when run inside Claude Code.
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", sessionID)
	t.Setenv("BRAVROS_POLICE_STANDDOWN", "")

	sdPath := standDownPath(sessionID)
	_ = os.MkdirAll(filepath.Dir(sdPath), 0755)
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(sdPath)) })

	// Marker expired 1 hour ago
	markerData := `{"session_id":"` + sessionID + `","expires_at":"` + time.Now().Add(-1*time.Hour).Format(time.RFC3339) + `"}`
	if err := os.WriteFile(sdPath, []byte(markerData), 0644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	policeStandDownStatusCmd.SetOut(&out)
	if err := policeStandDownStatusCmd.RunE(policeStandDownStatusCmd, nil); err != nil {
		t.Fatalf("status: %v", err)
	}

	var st struct {
		Active bool `json:"active"`
	}
	if err := json.Unmarshal(out.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if st.Active {
		t.Errorf("expected expired standdown to report active=false")
	}

	// File should be removed
	if _, err := os.Stat(sdPath); !os.IsNotExist(err) {
		t.Errorf("expired standdown file was not cleaned up")
	}
}

func TestPoliceStandDown_On_NoSession_Warns(t *testing.T) {
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")

	var errBuf bytes.Buffer
	policeStandDownOnCmd.SetErr(&errBuf)
	if err := policeStandDownOnCmd.RunE(policeStandDownOnCmd, nil); err != nil {
		t.Fatalf("expected nil return on no session, got %v", err)
	}
	if !strings.Contains(errBuf.String(), "no agent session detected") {
		t.Errorf("expected warning on stderr, got %q", errBuf.String())
	}
}

// TestPolicePreToolUse_LegacyCamelCasePayload_StillBlocked pins the Phase 1
// fail-closed shim: Claude Code's real PreToolUse contract is snake_case
// (tool_name/tool_input), but a payload in the legacy camelCase shape below —
// which no supported caller emits — must still be blocked, not silently
// approved. This is the exact bug the shim exists to prevent from
// reintroducing itself once someone "cleans up" the legacy fallback.
func TestPolicePreToolUse_LegacyCamelCasePayload_StillBlocked(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("BRAVROS_POLICE_STANDDOWN", "")
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")

	// Deliberately camelCase/legacy shape — this is the one literal the
	// cleanliness grep in Phase 2 is expected to find. Do not convert it to
	// snake_case; that would delete the regression this test exists for.
	payload := `{"toolName":"bash","input":{"command":"git push origin main"}}`
	var out bytes.Buffer
	policePreToolUseCmd.SetIn(strings.NewReader(payload))
	policePreToolUseCmd.SetOut(&out)
	policePreToolUseCmd.SetErr(&bytes.Buffer{})

	if err := policePreToolUseCmd.RunE(policePreToolUseCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	raw := out.String()
	if !strings.Contains(raw, "Police Block") {
		t.Errorf("legacy camelCase payload must still be blocked, got %q", raw)
	}
	if !strings.Contains(raw, `"exitCode":2`) {
		t.Errorf("expected exitCode 2 for legacy camelCase payload, got %q", raw)
	}
}

// ---------------------------------------------------------------------------
// Rule 52 — irreversible content-loss guard (P-0024 Phase 3)
//
// Test-only helpers below are prefixed rule52Test to stay unambiguous next to
// police.go's own rule52* implementation symbols.
// ---------------------------------------------------------------------------

// rule52TestRunGit runs a git command in dir with a fixed committer identity
// (mirrors TestPushTargetsProtected_BareePushFollowsCurrentBranch's pattern),
// so no global/user gitconfig is required.
func rule52TestRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// rule52TestWriteFile writes rel (relative to repo) with content, creating
// parent directories as needed.
func rule52TestWriteFile(t *testing.T, repo, rel, content string) {
	t.Helper()
	full := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// rule52TestNewRepo builds a scratch git repo with one committed, unmodified
// tracked file (seed.txt) — the baseline clean state each case mutates.
func rule52TestNewRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	rule52TestRunGit(t, repo, "init", "-q", "-b", "main")
	rule52TestWriteFile(t, repo, "seed.txt", "seed\n")
	rule52TestRunGit(t, repo, "add", "seed.txt")
	rule52TestRunGit(t, repo, "commit", "-q", "-m", "seed")
	return repo
}

// rule52TestChdir chdirs into dir for the duration of the (sub)test,
// restoring the original cwd on cleanup. Rule 52 probes the PROCESS cwd, so
// every test that needs a specific repo state as the target must chdir in.
func rule52TestChdir(t *testing.T, dir string) {
	t.Helper()
	restore, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(restore) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
}

// rule52TestSetEnv isolates ~/.claude/state (HOME) for token/marker state and
// clears the stand-down env var and both session-id env vars, matching the
// existing tests' convention. Returns the isolated HOME.
func rule52TestSetEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BRAVROS_POLICE_STANDDOWN", "")
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	return home
}

// rule52TestInvoke feeds command through policePreToolUseCmd as a real
// snake_case PreToolUse payload and reports whether it was blocked, along
// with the JSON envelope's exitCode and stderr field.
func rule52TestInvoke(t *testing.T, command string) (blocked bool, exitCode int, stderr string) {
	t.Helper()
	type toolInput struct {
		Command string `json:"command"`
	}
	type payload struct {
		ToolName string    `json:"tool_name"`
		Input    toolInput `json:"tool_input"`
	}
	data, err := json.Marshal(payload{ToolName: "bash", Input: toolInput{Command: command}})
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	policePreToolUseCmd.SetIn(bytes.NewReader(data))
	policePreToolUseCmd.SetOut(&out)
	policePreToolUseCmd.SetErr(&bytes.Buffer{})

	if err := policePreToolUseCmd.RunE(policePreToolUseCmd, nil); err != nil {
		t.Fatalf("RunE(%q): %v", command, err)
	}
	if out.Len() == 0 {
		return false, 0, ""
	}
	var res struct {
		Stderr string `json:"stderr"`
		Exit   int    `json:"exitCode"`
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal block json for %q: %v, raw=%q", command, err, out.String())
	}
	return true, res.Exit, res.Stderr
}

// rule52TestWriteDestructiveToken writes a token file in the shape
// token.Gate{Name:"destructive"} reads (cli/internal/token/gate.go), at the
// path it resolves to under the given (isolated) HOME.
func rule52TestWriteDestructiveToken(t *testing.T, home string, expiresAt time.Time) string {
	t.Helper()
	path := filepath.Join(home, ".claude", "state", "destructive-token")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	tok := struct {
		CreatedAt  time.Time `json:"created_at"`
		ExpiresAt  time.Time `json:"expires_at"`
		TTLMinutes int       `json:"ttl_minutes"`
		TTY        string    `json:"tty"`
		SingleUse  bool      `json:"single_use"`
	}{
		CreatedAt:  time.Now().UTC(),
		ExpiresAt:  expiresAt,
		TTLMinutes: 5,
		TTY:        "",
		SingleUse:  true,
	}
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Required-coverage item 1: each of the six families blocked on a dirty target.
func TestRule52_SixFamilies_BlockedOnDirtyTarget(t *testing.T) {
	cases := []struct {
		name           string
		command        string
		setup          func(t *testing.T, repo string)
		wantSanctioned string
	}{
		{
			name:    "git checkout -- modified tracked file",
			command: "git checkout -- seed.txt",
			setup: func(t *testing.T, repo string) {
				rule52TestWriteFile(t, repo, "seed.txt", "modified\n")
			},
			wantSanctioned: "bravros discard",
		},
		{
			name:    "git restore modified tracked file",
			command: "git restore seed.txt",
			setup: func(t *testing.T, repo string) {
				rule52TestWriteFile(t, repo, "seed.txt", "modified\n")
			},
			wantSanctioned: "bravros discard",
		},
		{
			name:    "git reset --hard with modified tracked file",
			command: "git reset --hard",
			setup: func(t *testing.T, repo string) {
				rule52TestWriteFile(t, repo, "seed.txt", "modified\n")
			},
			wantSanctioned: "bravros discard",
		},
		{
			name:    "git clean -f with untracked file",
			command: "git clean -f",
			setup: func(t *testing.T, repo string) {
				rule52TestWriteFile(t, repo, "untracked.txt", "new\n")
			},
			wantSanctioned: "bravros clean-untracked",
		},
		{
			name:           "git stash drop is unconditional",
			command:        "git stash drop",
			setup:          func(t *testing.T, repo string) {},
			wantSanctioned: "git stash pop",
		},
		{
			name:    "rm -rf dir containing untracked file",
			command: "rm -rf sub",
			setup: func(t *testing.T, repo string) {
				rule52TestWriteFile(t, repo, "sub/untracked.txt", "new\n")
			},
			wantSanctioned: "bravros discard",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule52TestSetEnv(t)
			repo := rule52TestNewRepo(t)
			rule52TestChdir(t, repo)
			tc.setup(t, repo)

			blocked, exit, stderr := rule52TestInvoke(t, tc.command)
			if !blocked || exit != 2 {
				t.Fatalf("%s: expected blocked exitCode 2, got blocked=%v exit=%d stderr=%q", tc.command, blocked, exit, stderr)
			}
			if !strings.Contains(stderr, "Police Block") {
				t.Errorf("expected Police Block message, got %q", stderr)
			}
			if !strings.Contains(stderr, tc.wantSanctioned) {
				t.Errorf("expected sanctioned verb %q in message, got %q", tc.wantSanctioned, stderr)
			}
			if !strings.Contains(stderr, "bravros destructive unlock") {
				t.Errorf("expected destructive-token escape hatch mentioned, got %q", stderr)
			}
		})
	}
}

// Required-coverage item 2: each of the five state-checked families allowed
// on a clean target (stash drop/clear is unconditional and excluded here).
func TestRule52_FiveFamilies_AllowedOnCleanTarget(t *testing.T) {
	cases := []struct {
		name    string
		command string
		setup   func(t *testing.T, repo string)
	}{
		{"git checkout -- unmodified committed file", "git checkout -- seed.txt", func(t *testing.T, repo string) {}},
		{"git restore unmodified file", "git restore seed.txt", func(t *testing.T, repo string) {}},
		{"git reset --hard on clean tree", "git reset --hard", func(t *testing.T, repo string) {}},
		{"git clean -f with nothing untracked", "git clean -f", func(t *testing.T, repo string) {}},
		{"rm -rf dir whose contents are all committed and unmodified", "rm -rf sub", func(t *testing.T, repo string) {
			rule52TestWriteFile(t, repo, "sub/file.txt", "committed\n")
			rule52TestRunGit(t, repo, "add", "sub/file.txt")
			rule52TestRunGit(t, repo, "commit", "-q", "-m", "add sub")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule52TestSetEnv(t)
			repo := rule52TestNewRepo(t)
			rule52TestChdir(t, repo)
			tc.setup(t, repo)

			if blocked, _, stderr := rule52TestInvoke(t, tc.command); blocked {
				t.Errorf("%s: expected allowed (no output) on a clean target, got blocked stderr=%q", tc.command, stderr)
			}
		})
	}
}

// Required-coverage item 3: `restore --staged` is index-only and allowed even
// with staged changes; `restore <path>` on a worktree-modified file blocks.
func TestRule52_RestoreStagedVsWorktree(t *testing.T) {
	t.Run("restore --staged allowed even with staged changes", func(t *testing.T) {
		rule52TestSetEnv(t)
		repo := rule52TestNewRepo(t)
		rule52TestChdir(t, repo)
		rule52TestWriteFile(t, repo, "seed.txt", "staged change\n")
		rule52TestRunGit(t, repo, "add", "seed.txt")

		if blocked, _, stderr := rule52TestInvoke(t, "git restore --staged seed.txt"); blocked {
			t.Errorf("git restore --staged should be allowed (index-only), got blocked: %q", stderr)
		}
	})

	t.Run("restore path on worktree-modified file blocked", func(t *testing.T) {
		rule52TestSetEnv(t)
		repo := rule52TestNewRepo(t)
		rule52TestChdir(t, repo)
		rule52TestWriteFile(t, repo, "seed.txt", "worktree change\n")

		blocked, exit, stderr := rule52TestInvoke(t, "git restore seed.txt")
		if !blocked || exit != 2 {
			t.Errorf("git restore <path> on a modified worktree file should block, blocked=%v exit=%d stderr=%q", blocked, exit, stderr)
		}
	})
}

// Required-coverage item 3 (continued): stash clear blocks; list/push allow.
func TestRule52_StashClearBlocked_ListAndPushAllowed(t *testing.T) {
	rule52TestSetEnv(t)
	repo := rule52TestNewRepo(t)
	rule52TestChdir(t, repo)

	if blocked, exit, stderr := rule52TestInvoke(t, "git stash clear"); !blocked || exit != 2 {
		t.Errorf("git stash clear should block, blocked=%v exit=%d stderr=%q", blocked, exit, stderr)
	}
	if blocked, _, stderr := rule52TestInvoke(t, "git stash list"); blocked {
		t.Errorf("git stash list should be allowed, got %q", stderr)
	}
	if blocked, _, stderr := rule52TestInvoke(t, "git stash push"); blocked {
		t.Errorf("git stash push should be allowed, got %q", stderr)
	}
}

// Required-coverage item 4: branch creation, dry-run clean, and the -x/-X
// carve-out (always blocked, even clean).
func TestRule52_CheckoutBranchCreate_AllowedOnDirtyTree(t *testing.T) {
	rule52TestSetEnv(t)
	repo := rule52TestNewRepo(t)
	rule52TestChdir(t, repo)
	rule52TestWriteFile(t, repo, "seed.txt", "dirty\n")

	if blocked, _, stderr := rule52TestInvoke(t, "git checkout -b newbranch"); blocked {
		t.Errorf("git checkout -b should be allowed even on a dirty tree, got %q", stderr)
	}
}

func TestRule52_CleanDryRun_Allowed(t *testing.T) {
	rule52TestSetEnv(t)
	repo := rule52TestNewRepo(t)
	rule52TestChdir(t, repo)
	rule52TestWriteFile(t, repo, "untracked.txt", "x\n")

	if blocked, _, stderr := rule52TestInvoke(t, "git clean -n"); blocked {
		t.Errorf("git clean -n (dry run) should be allowed, got %q", stderr)
	}
}

func TestRule52_CleanFdx_BlockedAlways_EvenOnCleanTree(t *testing.T) {
	rule52TestSetEnv(t)
	repo := rule52TestNewRepo(t)
	rule52TestChdir(t, repo) // clean tree: no modifications, no untracked files

	blocked, exit, stderr := rule52TestInvoke(t, "git clean -fdx")
	if !blocked || exit != 2 {
		t.Fatalf("git clean -fdx must block unconditionally, blocked=%v exit=%d", blocked, exit)
	}
	if !strings.Contains(stderr, "gitignored") {
		t.Errorf("expected message about gitignored files (-x carve-out), got %q", stderr)
	}
}

// Required-coverage item 5: `bravros discard` is exempt — the command anchors
// on `bravros`, never matched by rule 52's git/rm dispatch — even dirty.
func TestRule52_BravrosDiscardExempt(t *testing.T) {
	rule52TestSetEnv(t)
	repo := rule52TestNewRepo(t)
	rule52TestChdir(t, repo)
	rule52TestWriteFile(t, repo, "seed.txt", "dirty\n")

	if blocked, _, stderr := rule52TestInvoke(t, "bravros discard seed.txt"); blocked {
		t.Errorf("bravros discard must never be matched by rule 52, got %q", stderr)
	}
}

// Required-coverage item 6: non-matching command allowed end-to-end, and the
// early-return path unit-tested directly.
func TestRule52Check_NonMatchingCommand_EarlyReturn(t *testing.T) {
	if v := rule52Check("ls -la"); v != nil {
		t.Errorf(`rule52Check("ls -la") = %+v; want nil (early-return path)`, v)
	}
}

// TestRule52Check_BackslashEscapedVerb_NotSkippedByPrefilter pins PR 75
// review finding 1: commandSegments consumes shell escapes, so `g\it stash
// drop` tokenizes to `git stash drop` — but a pre-filter scanning the RAW
// string would see g-\-i-t, early-return, and skip the safety floor for a
// command the parser would have blocked. The scan must strip backslashes
// first. `stash drop` is used because it blocks unconditionally, needing no
// repo fixture.
func TestRule52Check_BackslashEscapedVerb_NotSkippedByPrefilter(t *testing.T) {
	if v := rule52Check(`g\it stash drop`); v == nil {
		t.Fatal(`rule52Check("g\it stash drop") = nil; the backslash-escaped verb slipped past the pre-filter`)
	}
}

func TestRule52_NonMatchingCommand_Allowed(t *testing.T) {
	rule52TestSetEnv(t)
	repo := rule52TestNewRepo(t)
	rule52TestChdir(t, repo)

	if blocked, _, stderr := rule52TestInvoke(t, "ls -la"); blocked {
		t.Errorf("ls -la should be allowed, got %q", stderr)
	}
}

// Required-coverage item 7: the stand-down decision pin (decisions.md
// Decision 3). Rule 52 is a safety-floor member; stand-down must not
// suppress it. Mirrored against the stand-down-suppressible main-merge gate,
// which DOES go quiet under the same env — proving the two are on separate
// switches, not the same one.
func TestRule52_SurvivesStandDown_MainMergeGateDoesNot(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("BRAVROS_POLICE_STANDDOWN", "1")

	repo := rule52TestNewRepo(t)
	rule52TestChdir(t, repo)
	rule52TestWriteFile(t, repo, "seed.txt", "dirty\n")

	// Safety floor: rule 52 still blocks under stand-down.
	blocked, exit, stderr := rule52TestInvoke(t, "git checkout -- seed.txt")
	if !blocked || exit != 2 {
		t.Fatalf("rule 52 must survive stand-down (decisions.md Decision 3): blocked=%v exit=%d stderr=%q", blocked, exit, stderr)
	}
	if !strings.Contains(stderr, "Police Block") {
		t.Errorf("expected Police Block message, got %q", stderr)
	}

	// Mirror: the stand-down-suppressible main-merge gate goes quiet under
	// the identical env — same command shape, different classification.
	if blocked, _, stderr := rule52TestInvoke(t, "git push origin main"); blocked {
		t.Errorf("main-merge gate should be suppressed by stand-down, got %q", stderr)
	}
}

// Required-coverage item 8: the destructive-token escape hatch (Decision 7)
// — valid token allows once and is consumed; a second identical invocation
// blocks again; an expired token neither allows nor survives on disk.
func TestRule52_DestructiveTokenEscapeHatch_SingleUse(t *testing.T) {
	tempHome := rule52TestSetEnv(t)
	repo := rule52TestNewRepo(t)
	rule52TestChdir(t, repo)
	rule52TestWriteFile(t, repo, "seed.txt", "dirty\n")

	tokPath := rule52TestWriteDestructiveToken(t, tempHome, time.Now().Add(5*time.Minute))

	// First invocation: valid token present -> allowed, and consumed.
	if blocked, _, stderr := rule52TestInvoke(t, "git checkout -- seed.txt"); blocked {
		t.Fatalf("a valid destructive token should allow the blocked command once, got %q", stderr)
	}
	if _, err := os.Stat(tokPath); !os.IsNotExist(err) {
		t.Errorf("destructive token should be consumed (removed) after a single use")
	}

	// Second identical invocation: token is gone -> blocks again.
	blocked, exit, _ := rule52TestInvoke(t, "git checkout -- seed.txt")
	if !blocked || exit != 2 {
		t.Errorf("second invocation without a token should block again, blocked=%v exit=%d", blocked, exit)
	}
}

func TestRule52_ExpiredDestructiveToken_DoesNotAllow_AndIsCleanedUp(t *testing.T) {
	tempHome := rule52TestSetEnv(t)
	repo := rule52TestNewRepo(t)
	rule52TestChdir(t, repo)
	rule52TestWriteFile(t, repo, "seed.txt", "dirty\n")

	tokPath := rule52TestWriteDestructiveToken(t, tempHome, time.Now().Add(-5*time.Minute))

	blocked, exit, _ := rule52TestInvoke(t, "git checkout -- seed.txt")
	if !blocked || exit != 2 {
		t.Fatalf("an expired destructive token must not allow, blocked=%v exit=%d", blocked, exit)
	}
	if _, err := os.Stat(tokPath); !os.IsNotExist(err) {
		t.Errorf("expired destructive token should be cleaned up (removed)")
	}
}

// Required-coverage item 9: indeterminate repo state fails CLOSED
// (decisions.md Decision 8). Simulated by removing `git` from PATH so every
// internal git invocation fails WITHOUT producing git's own "not a git
// repository" message — exercising the true rule52RepoUnknown branch (see
// rule52ProbeRepo's stderr classification) rather than the determined-no
// branch. This needs no change to police.go: it drives the existing seam.
func TestRule52Check_IndeterminateRepoState_FailsClosed(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty dir: `git` cannot be resolved at all

	v := rule52Check("git checkout -- foo.txt")
	if v == nil {
		t.Fatal("expected a violation when git cannot answer at all; got nil (fail-open)")
	}
	if !strings.Contains(v.what, "could not be determined") {
		t.Errorf("expected the fail-closed cleanliness message, got %q", v.what)
	}
}

// Required-coverage item 10: determined NOT a repo at all is a real allow —
// nothing outside a repository is repo content.
func TestRule52Check_NotARepoAtAll_Allowed(t *testing.T) {
	dir := t.TempDir()
	rule52TestChdir(t, dir)

	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "unrelated.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if v := rule52Check("rm -rf sub"); v != nil {
		t.Errorf("rule52Check should allow rm -rf outside any git repository, got %+v", v)
	}
}

// Cheap grep-as-Go-test: the block message never routes an agent toward
// `bravros police unlock` or toward turning stand-down ON as a bypass — the
// destructive token is the only escape hatch (decisions.md Decision 7). The
// message IS allowed to state, factually, that the floor fires even under
// stand-down (that is the opposite of offering it as an option) — see
// rule52BlockMessage's own "Deliberately never mentions... as an option" doc.
func TestRule52BlockMessage_NeverMentionsPoliceUnlockOrStandDownAsAnOption(t *testing.T) {
	v := rule52IndeterminateViolation("`git checkout -- foo`")
	msg := rule52BlockMessage(v)

	if strings.Contains(msg, "police unlock") {
		t.Errorf("rule 52 block message must never mention police unlock, got %q", msg)
	}
	if strings.Contains(msg, "police standdown") {
		t.Errorf("rule 52 block message must never instruct running police standdown, got %q", msg)
	}
	if !strings.Contains(msg, "bravros destructive unlock") {
		t.Errorf("expected the destructive-token escape hatch to be named, got %q", msg)
	}
}
