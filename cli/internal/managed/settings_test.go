package managed

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readSettings(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, data)
	}
	return out
}

// sessionStartEntries flattens every SessionStart command entry in the file.
func sessionStartEntries(t *testing.T, path string) []map[string]interface{} {
	t.Helper()
	settings := readSettings(t, path)
	hooks, _ := settings["hooks"].(map[string]interface{})
	groups, _ := hooks["SessionStart"].([]interface{})
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

func TestSyncClaudeSettings_CreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	res, err := SyncClaudeSettings(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Created || !res.Changed {
		t.Fatalf("expected Created+Changed, got %+v", res)
	}
	if res.BackupPath != "" {
		t.Errorf("no backup expected for a fresh file, got %q", res.BackupPath)
	}

	entries := sessionStartEntries(t, path)
	if len(entries) != 2 {
		t.Fatalf("expected 2 SessionStart entries, got %d", len(entries))
	}
	for _, want := range []string{"selfupdate", "hook verify-install-check"} {
		found := false
		for _, e := range entries {
			cmd, _ := e["command"].(string)
			if strings.Contains(cmd, want) {
				found = true
				if !strings.Contains(cmd, "com.anthropic.*") {
					t.Errorf("desktop-app guard missing from %q", cmd)
				}
				if e[ManagedByKey] != ManagedByValue {
					t.Errorf("entry %q lacks the bravros marker", cmd)
				}
			}
		}
		if !found {
			t.Errorf("no SessionStart entry running %q", want)
		}
	}

	settings := readSettings(t, path)
	sl, ok := settings["statusLine"].(map[string]interface{})
	if !ok {
		t.Fatal("expected a statusLine block")
	}
	if sl[ManagedByKey] != ManagedByValue {
		t.Error("statusLine lacks the bravros marker")
	}
	if cmd, _ := sl["command"].(string); !strings.Contains(cmd, "statusline") {
		t.Errorf("unexpected statusLine command: %q", cmd)
	}
}

func TestSyncClaudeSettings_PreservesUserContentAndHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	seed := `{
  "model": "opus",
  "env": {
    "FOO": "bar"
  },
  "permissions": {
    "allow": [
      "Bash(ls:*)"
    ]
  },
  "hooks": {
    "PreToolUse": [
      {
        "matcher": ".*",
        "hooks": [
          {
            "type": "command",
            "command": "my-audit"
          }
        ]
      }
    ],
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo mine"
          }
        ]
      }
    ]
  }
}
`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := SyncClaudeSettings(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.BackupPath == "" {
		t.Error("expected a backup of the pre-existing file")
	}
	if backup, rErr := os.ReadFile(res.BackupPath); rErr != nil || string(backup) != seed {
		t.Errorf("backup does not hold the original content (err=%v)", rErr)
	}

	settings := readSettings(t, path)
	if settings["model"] != "opus" {
		t.Errorf("user key 'model' lost: %v", settings["model"])
	}
	env, _ := settings["env"].(map[string]interface{})
	if env["FOO"] != "bar" {
		t.Errorf("user key 'env' lost: %v", settings["env"])
	}
	perms, _ := settings["permissions"].(map[string]interface{})
	allow, _ := perms["allow"].([]interface{})
	if len(allow) != 1 || allow[0] != "Bash(ls:*)" {
		t.Errorf("user key 'permissions' lost: %v", settings["permissions"])
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Error("user PreToolUse hooks lost")
	}

	entries := sessionStartEntries(t, path)
	if len(entries) != 3 {
		t.Fatalf("expected the user entry + 2 bravros entries, got %d: %v", len(entries), entries)
	}
	if cmd, _ := entries[0]["command"].(string); cmd != "echo mine" {
		t.Errorf("user SessionStart hook must come first and survive verbatim, got %q", cmd)
	}
	if _, marked := entries[0][ManagedByKey]; marked {
		t.Error("bravros must not stamp its marker on a user entry")
	}
}

func TestSyncClaudeSettings_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	seed := `{"model":"opus","hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo mine"}]}]}}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := SyncClaudeSettings(path); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	res, err := SyncClaudeSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Error("second run must be a no-op")
	}
	if res.BackupPath != "" {
		t.Errorf("second run must not write a backup, got %q", res.BackupPath)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("second run changed the file:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
	if got := len(sessionStartEntries(t, path)); got != 3 {
		t.Errorf("entries duplicated across runs: got %d, want 3", got)
	}
}

func TestSyncClaudeSettings_ReplacesStaleManagedEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	seed := `{"hooks":{"SessionStart":[{"hooks":[
	  {"__managed_by":"bravros","type":"command","command":"old-bravros-command"},
	  {"type":"command","command":"echo mine"}
	]}]}}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := SyncClaudeSettings(path); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "old-bravros-command") {
		t.Error("stale bravros-managed entry must be replaced")
	}
	if !strings.Contains(string(data), "echo mine") {
		t.Error("user entry in the same group must survive")
	}
	if got := len(sessionStartEntries(t, path)); got != 3 {
		t.Errorf("expected user entry + 2 fresh bravros entries, got %d", got)
	}
}

func TestSyncClaudeSettings_UserStatusLineUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	seed := `{"statusLine":{"type":"command","command":"my-own-statusline"}}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := SyncClaudeSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if !res.StatusLineSkipped {
		t.Error("expected StatusLineSkipped for a user-authored statusLine")
	}

	settings := readSettings(t, path)
	sl, _ := settings["statusLine"].(map[string]interface{})
	if sl["command"] != "my-own-statusline" {
		t.Errorf("user statusLine was overwritten: %v", sl)
	}
	sentinel, _ := settings["_bravros_managed_keys"].([]interface{})
	for _, k := range sentinel {
		if k == "statusLine" {
			t.Error("statusLine must not be claimed while the user owns it")
		}
	}
}

func TestSyncClaudeSettings_BackupNeverClobbered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"model":"opus"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".bak", []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := SyncClaudeSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if res.BackupPath != path+".bak.1" {
		t.Errorf("expected a non-colliding backup name, got %q", res.BackupPath)
	}
	existing, _ := os.ReadFile(path + ".bak")
	if string(existing) != "precious" {
		t.Error("pre-existing backup was clobbered")
	}
}

func TestSyncClaudeSettings_MalformedJSONFailsLoudly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	broken := `{"model": "opus"` // truncated
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := SyncClaudeSettings(path); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
	data, _ := os.ReadFile(path)
	if string(data) != broken {
		t.Errorf("malformed file must be left untouched, got %q", data)
	}
}

func TestSyncClaudeSettings_UnwritableFileFailsLoudly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"model":"opus"}`), 0o400); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: mode bits do not block writes")
	}

	_, err := SyncClaudeSettings(path)
	if err == nil {
		t.Fatal("expected a loud failure on an unwritable settings.json")
	}
	if !strings.Contains(err.Error(), "cannot write") {
		t.Errorf("error should say the file cannot be written, got: %v", err)
	}
	if !strings.Contains(err.Error(), "chflags nouchg") {
		t.Errorf("error should tell the operator how to unlock the file, got: %v", err)
	}
}
