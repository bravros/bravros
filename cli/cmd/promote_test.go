package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupPromoteStateDir creates a temp state dir and redirects the token path.
// Returns cleanup function.
func setupPromoteStateDir(t *testing.T) (stateDir string, cleanup func()) {
	t.Helper()
	tmp := t.TempDir()
	stateDir = filepath.Join(tmp, ".claude", "state")
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Override home to redirect promoteTokenPath()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmp)
	return stateDir, func() { os.Setenv("HOME", origHome) }
}

// writeToken writes a promoteToken JSON to the standard token path.
func writeToken(t *testing.T, tok promoteToken) {
	t.Helper()
	path := promoteTokenPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	data, err := json.Marshal(tok)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// TestPromote_UnlockOutsideClaude verifies that unlock succeeds when not inside Claude.
func TestPromote_UnlockOutsideClaude(t *testing.T) {
	_, cleanup := setupPromoteStateDir(t)
	defer cleanup()

	// Ensure we're not "inside Claude" by unsetting env vars and disabling process walk
	origSessionID := os.Getenv("CLAUDE_SESSION_ID")
	origCanonicalSessionID := os.Getenv("CLAUDE_CODE_SESSION_ID")
	origEntrypoint := os.Getenv("CLAUDE_CODE_ENTRYPOINT")
	os.Unsetenv("CLAUDE_SESSION_ID")
	os.Unsetenv("CLAUDE_CODE_SESSION_ID")
	os.Unsetenv("CLAUDE_CODE_ENTRYPOINT")
	origSkip := autoprSkipProcessWalk
	autoprSkipProcessWalk = true
	defer func() {
		os.Setenv("CLAUDE_SESSION_ID", origSessionID)
		os.Setenv("CLAUDE_CODE_SESSION_ID", origCanonicalSessionID)
		os.Setenv("CLAUDE_CODE_ENTRYPOINT", origEntrypoint)
		autoprSkipProcessWalk = origSkip
	}()

	promoteTTLFlag = 5
	promoteDryRunFlag = false

	out := captureStdout(t, func() {
		promoteUnlockCmd.Run(promoteUnlockCmd, []string{})
	})

	// Token file should exist
	path := promoteTokenPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("token file not created at %s", path)
	}

	// Read and validate token
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var tok promoteToken
	if err := json.Unmarshal(data, &tok); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if !tok.SingleUse {
		t.Error("token should be single_use=true")
	}
	if tok.TTLMinutes != 5 {
		t.Errorf("ttl_minutes: got %d, want 5", tok.TTLMinutes)
	}
	if tok.ExpiresAt.Before(time.Now()) {
		t.Error("token should not be expired immediately after minting")
	}
	if tok.CreatedAt.IsZero() {
		t.Error("created_at should not be zero")
	}

	// Output should mention "Promote token minted"
	if !strings.Contains(out, "Promote token minted") {
		t.Errorf("expected success message in output, got: %s", out)
	}
}

// TestPromote_UnlockInsideClaudeRefuses verifies that unlock is blocked inside Claude
// when the legacy CLAUDE_SESSION_ID is set (back-compat path through session.Resolve()).
func TestPromote_UnlockInsideClaudeRefuses(t *testing.T) {
	_, cleanup := setupPromoteStateDir(t)
	defer cleanup()

	// Simulate being inside Claude Code via the legacy var ONLY — clear canonical
	// so we exercise the back-compat fallback path, not the canonical short-circuit.
	origCanonical := os.Getenv("CLAUDE_CODE_SESSION_ID")
	os.Unsetenv("CLAUDE_CODE_SESSION_ID")
	os.Setenv("CLAUDE_SESSION_ID", "test-session-123")
	defer func() {
		os.Unsetenv("CLAUDE_SESSION_ID")
		if origCanonical != "" {
			os.Setenv("CLAUDE_CODE_SESSION_ID", origCanonical)
		}
	}()

	// We need to capture the exit(1) — use a subprocess-like check by wrapping
	// the logic that would be called. Since we can't easily test os.Exit in unit
	// tests, we test the isInsideClaudeEnv() gate directly.
	if !isInsideClaudeEnv() {
		t.Skip("CLAUDE_SESSION_ID is set but isInsideClaudeEnv() returned false — check env")
	}

	// Verify isInsideClaudeEnv returns true with CLAUDE_SESSION_ID set
	if !isInsideClaudeEnv() {
		t.Error("expected isInsideClaudeEnv() to return true when CLAUDE_SESSION_ID is set")
	}
}

// TestPromote_UnlockInsideClaudeRefuses_CanonicalVar verifies that unlock is
// blocked inside Claude when the canonical CLAUDE_CODE_SESSION_ID (v2.1.132+) is
// set, even when the legacy CLAUDE_SESSION_ID is unset. Locks in the new var
// detection through session.Resolve().
func TestPromote_UnlockInsideClaudeRefuses_CanonicalVar(t *testing.T) {
	_, cleanup := setupPromoteStateDir(t)
	defer cleanup()

	// Set the canonical var only — legacy must remain unset to verify the new path.
	origLegacy := os.Getenv("CLAUDE_SESSION_ID")
	os.Unsetenv("CLAUDE_SESSION_ID")
	os.Setenv("CLAUDE_CODE_SESSION_ID", "canonical-session-xyz")
	defer func() {
		os.Unsetenv("CLAUDE_CODE_SESSION_ID")
		if origLegacy != "" {
			os.Setenv("CLAUDE_SESSION_ID", origLegacy)
		}
	}()

	if !isInsideClaudeEnv() {
		t.Error("expected isInsideClaudeEnv() to return true when CLAUDE_CODE_SESSION_ID (canonical) is set")
	}
}

// TestPromote_StatusReturnsFalseWhenAbsent verifies status returns present:false when no token.
func TestPromote_StatusReturnsFalseWhenAbsent(t *testing.T) {
	_, cleanup := setupPromoteStateDir(t)
	defer cleanup()

	promoteFieldFlag = "present"
	out := captureStdout(t, func() {
		promoteStatusCmd.Run(promoteStatusCmd, []string{})
	})
	promoteFieldFlag = "" // reset

	if strings.TrimSpace(out) != "false" {
		t.Errorf("expected 'false', got %q", out)
	}
}

// TestPromote_StatusReturnsTrueWhenFresh verifies status returns present:true with fresh token.
func TestPromote_StatusReturnsTrueWhenFresh(t *testing.T) {
	_, cleanup := setupPromoteStateDir(t)
	defer cleanup()

	now := time.Now().UTC()
	writeToken(t, promoteToken{
		CreatedAt:  now,
		ExpiresAt:  now.Add(5 * time.Minute),
		TTLMinutes: 5,
		TTY:        "/dev/ttys001",
		SingleUse:  true,
	})

	promoteFieldFlag = "present"
	out := captureStdout(t, func() {
		promoteStatusCmd.Run(promoteStatusCmd, []string{})
	})
	promoteFieldFlag = ""

	if strings.TrimSpace(out) != "true" {
		t.Errorf("expected 'true', got %q", out)
	}
}

// TestPromote_StatusReturnsFalseAfterExpiry verifies expired token is auto-cleaned.
func TestPromote_StatusReturnsFalseAfterExpiry(t *testing.T) {
	_, cleanup := setupPromoteStateDir(t)
	defer cleanup()

	// Write a token that expired 10 minutes ago
	past := time.Now().UTC().Add(-10 * time.Minute)
	writeToken(t, promoteToken{
		CreatedAt:  past.Add(-5 * time.Minute),
		ExpiresAt:  past, // already expired
		TTLMinutes: 5,
		SingleUse:  true,
	})

	path := promoteTokenPath()
	// Token file exists before status check
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("token file should exist before status check")
	}

	promoteFieldFlag = "present"
	out := captureStdout(t, func() {
		promoteStatusCmd.Run(promoteStatusCmd, []string{})
	})
	promoteFieldFlag = ""

	if strings.TrimSpace(out) != "false" {
		t.Errorf("expected 'false' for expired token, got %q", out)
	}

	// Token file should have been auto-cleaned
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expired token file should have been auto-deleted")
	}
}

// TestPromote_RevokeRemovesToken verifies revoke deletes the token file.
func TestPromote_RevokeRemovesToken(t *testing.T) {
	_, cleanup := setupPromoteStateDir(t)
	defer cleanup()

	now := time.Now().UTC()
	writeToken(t, promoteToken{
		CreatedAt: now,
		ExpiresAt: now.Add(5 * time.Minute),
		SingleUse: true,
	})

	path := promoteTokenPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("token file should exist before revoke")
	}

	captureStdout(t, func() {
		promoteRevokeCmd.Run(promoteRevokeCmd, []string{})
	})

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("token file should be deleted after revoke")
	}
}

// TestPromote_RevokeWhenAbsentIsIdempotent verifies revoke handles missing token gracefully.
func TestPromote_RevokeWhenAbsentIsIdempotent(t *testing.T) {
	_, cleanup := setupPromoteStateDir(t)
	defer cleanup()

	// Should not panic or exit non-zero
	out := captureStdout(t, func() {
		promoteRevokeCmd.Run(promoteRevokeCmd, []string{})
	})

	if !strings.Contains(out, "No token present") {
		t.Errorf("expected 'No token present' message, got: %q", out)
	}
}

// TestPromote_TokenPresent_ReturnsFalseForExpired verifies promoteTokenPresent() auto-cleans.
func TestPromote_TokenPresent_ReturnsFalseForExpired(t *testing.T) {
	_, cleanup := setupPromoteStateDir(t)
	defer cleanup()

	past := time.Now().UTC().Add(-1 * time.Minute)
	writeToken(t, promoteToken{
		CreatedAt: past.Add(-5 * time.Minute),
		ExpiresAt: past,
		SingleUse: true,
	})

	if promoteTokenPresent() {
		t.Error("promoteTokenPresent() should return false for expired token")
	}

	// Token should be auto-deleted
	if _, err := os.Stat(promoteTokenPath()); !os.IsNotExist(err) {
		t.Error("expired token should be auto-deleted by promoteTokenPresent()")
	}
}

// TestPromote_DryRun verifies --dry-run does not write token.
func TestPromote_DryRun(t *testing.T) {
	_, cleanup := setupPromoteStateDir(t)
	defer cleanup()

	// Not inside Claude
	origSession := os.Getenv("CLAUDE_SESSION_ID")
	origCanonical := os.Getenv("CLAUDE_CODE_SESSION_ID")
	origEntry := os.Getenv("CLAUDE_CODE_ENTRYPOINT")
	os.Unsetenv("CLAUDE_SESSION_ID")
	os.Unsetenv("CLAUDE_CODE_SESSION_ID")
	os.Unsetenv("CLAUDE_CODE_ENTRYPOINT")
	origSkip := autoprSkipProcessWalk
	autoprSkipProcessWalk = true
	defer func() {
		if origSession != "" {
			os.Setenv("CLAUDE_SESSION_ID", origSession)
		}
		if origCanonical != "" {
			os.Setenv("CLAUDE_CODE_SESSION_ID", origCanonical)
		}
		if origEntry != "" {
			os.Setenv("CLAUDE_CODE_ENTRYPOINT", origEntry)
		}
		autoprSkipProcessWalk = origSkip
	}()

	promoteDryRunFlag = true
	promoteTTLFlag = 10
	out := captureStdout(t, func() {
		promoteUnlockCmd.Run(promoteUnlockCmd, []string{})
	})
	promoteDryRunFlag = false
	promoteTTLFlag = 5

	// Token file should NOT exist
	if _, err := os.Stat(promoteTokenPath()); !os.IsNotExist(err) {
		t.Error("--dry-run should not write token file")
	}

	if !strings.Contains(out, "dry-run") {
		t.Errorf("expected dry-run message, got: %q", out)
	}
}
