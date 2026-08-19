package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestActiveCommand_SetAndClear(t *testing.T) {
	sessionID := "test-active-cmd-sess-1"
	t.Setenv("CLAUDE_SESSION_ID", sessionID)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")

	dir := agentAuditDir(sessionID)
	t.Cleanup(func() { os.RemoveAll(dir) })

	// 1. Set command to "commit"
	if err := activeCommandSetCmd.RunE(activeCommandSetCmd, []string{"commit"}); err != nil {
		t.Fatalf("set commit: %v", err)
	}

	markerPath := filepath.Join(dir, "active-command")
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if string(data) != "commit" {
		t.Errorf("marker = %q; want 'commit'", string(data))
	}

	historyPath := filepath.Join(dir, "command-history.txt")
	histData, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if string(histData) != "commit\n" {
		t.Errorf("history = %q; want 'commit\\n'", string(histData))
	}

	// 2. Set command to "prreview"
	if err := activeCommandSetCmd.RunE(activeCommandSetCmd, []string{"prreview"}); err != nil {
		t.Fatalf("set prreview: %v", err)
	}

	data, err = os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if string(data) != "prreview" {
		t.Errorf("marker = %q; want 'prreview'", string(data))
	}

	histData, err = os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if string(histData) != "commit\nprreview\n" {
		t.Errorf("history = %q; want 'commit\\nprreview\\n'", string(histData))
	}

	// 3. Clear command
	if err := activeCommandClearCmd.RunE(activeCommandClearCmd, nil); err != nil {
		t.Fatalf("clear: %v", err)
	}

	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Errorf("marker file still exists after clear")
	}

	// History file should be preserved
	histData, err = os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("read history after clear: %v", err)
	}
	if string(histData) != "commit\nprreview\n" {
		t.Errorf("history corrupted after clear: %q", string(histData))
	}
}

func TestActiveCommand_Clear_Idempotent(t *testing.T) {
	sessionID := "test-active-cmd-idempotent"
	t.Setenv("CLAUDE_SESSION_ID", sessionID)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")

	dir := agentAuditDir(sessionID)
	t.Cleanup(func() { os.RemoveAll(dir) })

	// Clear on non-existent directory / file should succeed without error
	if err := activeCommandClearCmd.RunE(activeCommandClearCmd, nil); err != nil {
		t.Fatalf("clear non-existent: %v", err)
	}

	if err := activeCommandClearCmd.RunE(activeCommandClearCmd, nil); err != nil {
		t.Fatalf("second clear: %v", err)
	}
}

func TestActiveCommand_NoSession_NoOp(t *testing.T) {
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")

	if err := activeCommandSetCmd.RunE(activeCommandSetCmd, []string{"commit"}); err != nil {
		t.Fatalf("set with no session: %v", err)
	}

	if err := activeCommandClearCmd.RunE(activeCommandClearCmd, nil); err != nil {
		t.Fatalf("clear with no session: %v", err)
	}
}

// set always replaces an existing marker — there is no non-overwriting mode,
// which is why the ported --override flag was dropped rather than implemented.
func TestActiveCommand_SetReplacesExistingMarker(t *testing.T) {
	sessionID := "test-active-cmd-replace"
	t.Setenv("CLAUDE_SESSION_ID", sessionID)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")

	dir := agentAuditDir(sessionID)
	t.Cleanup(func() { os.RemoveAll(dir) })

	for _, name := range []string{"setup", "deploy"} {
		if err := activeCommandSetCmd.RunE(activeCommandSetCmd, []string{name}); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, "active-command"))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if string(data) != "deploy" {
		t.Errorf("marker = %q; want 'deploy' (second set must replace the first)", string(data))
	}

	history, err := os.ReadFile(filepath.Join(dir, "command-history.txt"))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if string(history) != "setup\ndeploy\n" {
		t.Errorf("history = %q; want both entries appended", string(history))
	}
}

func TestResolveSession(t *testing.T) {
	// Neither set
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "")
	if got := resolveSession(); got != "" {
		t.Errorf("resolveSession() = %q; want empty", got)
	}

	// Only CLAUDE_SESSION_ID
	t.Setenv("CLAUDE_SESSION_ID", "sess-1")
	if got := resolveSession(); got != "sess-1" {
		t.Errorf("resolveSession() = %q; want 'sess-1'", got)
	}

	// Both set -> CLAUDE_CODE_SESSION_ID wins
	t.Setenv("CLAUDE_CODE_SESSION_ID", "code-sess-2")
	if got := resolveSession(); got != "code-sess-2" {
		t.Errorf("resolveSession() = %q; want 'code-sess-2'", got)
	}
}
