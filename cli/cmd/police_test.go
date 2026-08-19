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

	payload := `{"toolName":"bash","input":{"command":"git push origin main"}}`
	var out bytes.Buffer
	policePreToolUseCmd.SetIn(strings.NewReader(payload))
	policePreToolUseCmd.SetOut(&out)
	policePreToolUseCmd.SetErr(&bytes.Buffer{})

	if err := policePreToolUseCmd.RunE(policePreToolUseCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	var res struct {
		Stdout string `json:"stdout"`
		Stderr string `json:"stderr"`
		Exit   int    `json:"exitCode"`
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("failed to unmarshal block json: %v, raw output: %q", err, out.String())
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

	payload := `{"toolName":"bash","input":{"command":"gh pr merge 123 --auto --squash"}}`
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

	payload := `{"toolName":"bash","input":{"command":"git push origin main"}}`
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

	payload := `{"toolName":"bash","input":{"command":"git push origin homolog"}}`
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

	payload := `{"toolName":"readFile","input":{"command":"git push origin main"}}`
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

	payload := `{"toolName":"bash","input":{"command":"git checkout -b feat/my-feature"}}`
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
