package managed

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── JSON section tests ────────────────────────────────────────────────────

func TestRewriteJSONSection_InsertNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	content := map[string]string{"val": "hello"}
	if err := RewriteJSONSection(path, "hooks", content); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify file exists and contains expected keys.
	raw := map[string]json.RawMessage{}
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	if _, ok := raw["hooks"]; !ok {
		t.Error("expected 'hooks' key in output")
	}
	if _, ok := raw["_kaisser_managed_keys"]; !ok {
		t.Error("expected '_kaisser_managed_keys' in output")
	}
}

func TestRewriteJSONSection_ReplaceExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Write initial managed section.
	if err := RewriteJSONSection(path, "hooks", map[string]string{"old": "value"}); err != nil {
		t.Fatal(err)
	}

	// Rewrite with new content.
	if err := RewriteJSONSection(path, "hooks", map[string]string{"new": "value"}); err != nil {
		t.Fatal(err)
	}

	sec, err := ReadJSONSection(path, "hooks")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sec), "new") {
		t.Errorf("expected new content, got: %s", string(sec))
	}
	if strings.Contains(string(sec), "old") {
		t.Error("old content should have been replaced")
	}
}

func TestRewriteJSONSection_PreservesUserContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Seed with a user key.
	initial := `{"userKey": "preserved", "anotherKey": 42}`
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	// Add a managed section.
	if err := RewriteJSONSection(path, "hooks", map[string]string{"managed": "yes"}); err != nil {
		t.Fatal(err)
	}

	// User keys must survive.
	raw := map[string]json.RawMessage{}
	data, _ := os.ReadFile(path)
	json.Unmarshal(data, &raw)

	if _, ok := raw["userKey"]; !ok {
		t.Error("userKey was lost after rewrite")
	}
	if _, ok := raw["anotherKey"]; !ok {
		t.Error("anotherKey was lost after rewrite")
	}
}

func TestRewriteJSONSection_MalformedJSONTolerance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Write malformed JSON.
	os.WriteFile(path, []byte(`{ this is not valid JSON`), 0644)

	// Should not error — should back up + start fresh.
	if err := RewriteJSONSection(path, "hooks", "value"); err != nil {
		t.Fatalf("expected tolerance of malformed JSON, got: %v", err)
	}

	// Backup file should exist.
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Error("expected .bak file to be created for malformed JSON")
	}
}

func TestRewriteJSONSection_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	content := map[string]string{"k": "v"}
	if err := RewriteJSONSection(path, "hooks", content); err != nil {
		t.Fatal(err)
	}

	data1, _ := os.ReadFile(path)

	// Run again — must produce identical output.
	if err := RewriteJSONSection(path, "hooks", content); err != nil {
		t.Fatal(err)
	}

	data2, _ := os.ReadFile(path)
	if string(data1) != string(data2) {
		t.Errorf("idempotency violated:\nbefore: %s\nafter:  %s", data1, data2)
	}
}

func TestRewriteJSONSection_OrphanedManagedKeyAtEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Seed a file where _kaisser_managed_keys references a key that is no
	// longer present in the JSON body — the orphan ("orphan") sorts last.
	initial := `{"_kaisser_managed_keys":["actual","orphan"],"actual":"val"}`
	os.WriteFile(path, []byte(initial), 0644)

	// Rewrite an unrelated section. Output must remain valid JSON — the
	// previous bug emitted a trailing comma on the last present key.
	if err := RewriteJSONSection(path, "hooks", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("output is not valid JSON (orphan key produced trailing comma): %v\noutput:\n%s", err, data)
	}
	if _, ok := raw["actual"]; !ok {
		t.Error("'actual' key was lost")
	}
	if _, ok := raw["hooks"]; !ok {
		t.Error("'hooks' key was not inserted")
	}
}

// ─── Markdown section tests ────────────────────────────────────────────────

func TestRewriteMarkdownSection_InsertNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	os.WriteFile(path, []byte("# My Config\n\nUser content.\n"), 0644)

	if err := RewriteMarkdownSection(path, "globalRules", "## Auto Rules\n- rule 1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	if !strings.Contains(content, "<!-- kaisser-managed:start globalRules -->") {
		t.Error("start marker missing")
	}
	if !strings.Contains(content, "<!-- kaisser-managed:end globalRules -->") {
		t.Error("end marker missing")
	}
	if !strings.Contains(content, "## Auto Rules") {
		t.Error("inserted content missing")
	}
	// Original user content must remain.
	if !strings.Contains(content, "User content.") {
		t.Error("user content was lost")
	}
}

func TestRewriteMarkdownSection_ReplaceExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	initial := "# Config\n\n<!-- kaisser-managed:start rules -->\nOLD CONTENT\n<!-- kaisser-managed:end rules -->\n\nUser stuff.\n"
	os.WriteFile(path, []byte(initial), 0644)

	if err := RewriteMarkdownSection(path, "rules", "NEW CONTENT"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	if strings.Contains(content, "OLD CONTENT") {
		t.Error("old content should have been replaced")
	}
	if !strings.Contains(content, "NEW CONTENT") {
		t.Error("new content missing")
	}
	if !strings.Contains(content, "User stuff.") {
		t.Error("user content outside markers was lost")
	}
}

func TestRewriteMarkdownSection_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	os.WriteFile(path, []byte("# Config\n"), 0644)
	if err := RewriteMarkdownSection(path, "rules", "CONTENT"); err != nil {
		t.Fatal(err)
	}
	data1, _ := os.ReadFile(path)

	// Second write — must produce same output.
	if err := RewriteMarkdownSection(path, "rules", "CONTENT"); err != nil {
		t.Fatal(err)
	}
	data2, _ := os.ReadFile(path)

	if string(data1) != string(data2) {
		t.Errorf("idempotency violated")
	}
}

func TestRewriteMarkdownSection_MissingEndMarkerError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	// Only start marker present.
	os.WriteFile(path, []byte("<!-- kaisser-managed:start rules -->\nsome content\n"), 0644)

	err := RewriteMarkdownSection(path, "rules", "NEW")
	if err == nil {
		t.Error("expected error for missing end marker")
	}
}

func TestReadMarkdownSection_ReturnsContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	os.WriteFile(path, []byte("<!-- kaisser-managed:start rules -->\nHELLO\n<!-- kaisser-managed:end rules -->\n"), 0644)

	content, err := ReadMarkdownSection(path, "rules")
	if err != nil {
		t.Fatal(err)
	}
	if content != "HELLO" {
		t.Errorf("expected HELLO, got %q", content)
	}
}
