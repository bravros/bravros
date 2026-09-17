package managed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// trimmedOperatorSettings mirrors a real ~/.claude/settings.json after the
// operator stripped iTerm2's ten `cc-status` hook entries (they cost ~380 ms
// per call because the Swift binary shells out to `it2`, a pipx Python script;
// registered on both PreToolUse and PostToolUse that was ~800 ms per tool call).
//
// Stripping them empties seven hook events entirely. bravros's own entries here
// carry NO "__managed_by" marker, which is the state that previously caused
// duplicated hooks — see TestSyncClaudeSettings_AdoptsLegacyUnmarkedEntries.
const trimmedOperatorSettings = `{
  "model": "opus[1m]",
  "hooks": {
    "Notification": [
      {
        "matcher": "permission_prompt",
        "hooks": [
          { "type": "command", "command": "$HOME/.claude/scripts/announce-hook.sh" }
        ]
      }
    ],
    "PreToolUse": [
      {
        "matcher": ".*",
        "hooks": [
          { "type": "command", "command": "$HOME/.claude/bin/bravros police pretooluse" }
        ]
      }
    ],
    "SessionStart": [
      {
        "hooks": [
          { "type": "command", "command": "sh -c 'exec $HOME/.claude/bin/bravros selfupdate'" },
          { "type": "command", "command": "sh -c 'exec $HOME/.claude/bin/bravros hook verify-install-check'" }
        ]
      }
    ]
  },
  "bashOutputMaxChars": 40000,
  "taskOutputMaxChars": 40000,
  "statusLine": { "type": "command", "command": "$HOME/.claude/bin/bravros statusline" },
  "enabledPlugins": {
    "clangd-lsp@claude-plugins-official": false,
    "ralph-loop@claude-plugins-official": true
  },
  "_bravros_managed_keys": ["hooks", "statusLine"],
  "_bravros_version": "v3"
}`

// TestSyncClaudeSettings_PreservesTrimmedHookSet pins the guarantee the operator
// relies on: a sync must never resurrect hooks they deliberately removed, and
// must never duplicate bravros's own unmarked entries.
func TestSyncClaudeSettings_PreservesTrimmedHookSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(trimmedOperatorSettings), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := SyncClaudeSettings(path); err != nil {
		t.Fatalf("sync: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	// 1. iTerm2's status hook must never come back. bravros has no business
	//    knowing about it, and re-adding it silently costs ~800 ms per tool call.
	if strings.Contains(string(raw), "cc-status") {
		t.Errorf("sync resurrected a cc-status hook:\n%s", raw)
	}

	settings := readSettings(t, path)
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		t.Fatalf("hooks missing or not an object")
	}

	// 2. The seven emptied events must stay gone.
	for _, dead := range []string{
		"PostToolUse", "PermissionRequest", "SessionEnd",
		"Stop", "StopFailure", "SubagentStop", "UserPromptSubmit",
	} {
		if _, present := hooks[dead]; present {
			t.Errorf("sync re-added the %q hook event, which the operator removed", dead)
		}
	}

	// 3. The operator's own Notification hook survives.
	if _, present := hooks["Notification"]; !present {
		t.Error("sync dropped the user-authored Notification hook")
	}

	// 4. Unmarked bravros entries are adopted, not duplicated — on both the
	//    SessionStart and the PreToolUse event.
	if got := len(sessionStartEntries(t, path)); got != 2 {
		t.Errorf("SessionStart entries = %d, want 2 (unmarked entries must be adopted, not twinned)", got)
	}
	if got := len(preToolUseEntries(t, path)); got != 1 {
		t.Errorf("PreToolUse entries = %d, want 1 (the unmarked police entry must be adopted, not twinned)", got)
	}

	// 5. Unrelated top-level keys are untouched.
	if got := settings["bashOutputMaxChars"]; got != float64(40000) {
		t.Errorf("bashOutputMaxChars = %v, want 40000", got)
	}
	if got := settings["taskOutputMaxChars"]; got != float64(40000) {
		t.Errorf("taskOutputMaxChars = %v, want 40000", got)
	}
	if got := settings["model"]; got != "opus[1m]" {
		t.Errorf("model = %v, want opus[1m]", got)
	}
	plugins, _ := settings["enabledPlugins"].(map[string]interface{})
	if got := plugins["clangd-lsp@claude-plugins-official"]; got != false {
		t.Errorf("a disabled plugin was flipped: got %v, want false", got)
	}

	// 6. A second sync writes nothing.
	before, _ := os.ReadFile(path)
	res, err := SyncClaudeSettings(path)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	after, _ := os.ReadFile(path)
	if res.Changed || string(before) != string(after) {
		t.Errorf("second sync was not a no-op (changed=%v)", res.Changed)
	}
}

// preToolUseEntries flattens every PreToolUse command entry in the file, the
// PreToolUse counterpart of settings_test.go's sessionStartEntries.
func preToolUseEntries(t *testing.T, path string) []map[string]interface{} {
	t.Helper()
	settings := readSettings(t, path)
	hooks, _ := settings["hooks"].(map[string]interface{})
	groups, _ := hooks["PreToolUse"].([]interface{})
	var entries []map[string]interface{}
	for _, g := range groups {
		group, _ := g.(map[string]interface{})
		inner, _ := group["hooks"].([]interface{})
		for _, e := range inner {
			entry, _ := e.(map[string]interface{})
			entries = append(entries, entry)
		}
	}
	return entries
}
