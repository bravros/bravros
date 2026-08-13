package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestScanDependencies_Empty verifies that an empty skills directory returns an empty manifest.
func TestScanDependencies_Empty(t *testing.T) {
	dir := t.TempDir()
	deps, err := ScanDependencies(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deps) != 0 {
		t.Fatalf("expected 0 deps, got %d", len(deps))
	}
}

// TestScanDependencies_OneSkill verifies parsing a single skill with one dep.
func TestScanDependencies_OneSkill(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "user-report")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	skillMD := `---
name: user-report
description: Test skill
dependencies:
  - name: weasyprint
    install_cmd_macos: "uv tool install weasyprint"
    install_cmd_linux: "uv tool install weasyprint"
    check_cmd: "command -v weasyprint"
    optional: false
---

# user-report
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0644); err != nil {
		t.Fatal(err)
	}

	deps, err := ScanDependencies(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("expected 1 dep, got %d", len(deps))
	}
	d := deps[0]
	if d.Name != "weasyprint" {
		t.Errorf("expected name=weasyprint, got %q", d.Name)
	}
	if d.InstallCmdMacos != "uv tool install weasyprint" {
		t.Errorf("unexpected install_cmd_macos: %q", d.InstallCmdMacos)
	}
	if d.Optional {
		t.Error("expected optional=false")
	}
	if d.SourceSkill != "user-report" {
		t.Errorf("expected source_skill=user-report, got %q", d.SourceSkill)
	}
}

// TestScanDependencies_Deduplication verifies that two skills declaring the same dep name
// produce only one entry (first occurrence wins).
func TestScanDependencies_Deduplication(t *testing.T) {
	dir := t.TempDir()

	skills := []struct {
		name string
		dep  string
	}{
		{"skill-a", "weasyprint"},
		{"skill-b", "weasyprint"}, // duplicate
	}

	for _, s := range skills {
		skillDir := filepath.Join(dir, s.name)
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + s.name + "\ndescription: Test\ndependencies:\n  - name: " + s.dep + "\n    install_cmd_macos: \"brew install " + s.dep + "\"\n    install_cmd_linux: \"apt install " + s.dep + "\"\n    check_cmd: \"command -v " + s.dep + "\"\n    optional: false\n---\n"
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	deps, err := ScanDependencies(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("expected 1 dep after dedup, got %d", len(deps))
	}
	if deps[0].SourceSkill != "skill-a" {
		t.Errorf("expected first-occurrence skill-a to win, got %q", deps[0].SourceSkill)
	}
}

// TestScanDependencies_SkipSkills verifies that skipped skills are excluded.
func TestScanDependencies_SkipSkills(t *testing.T) {
	dir := t.TempDir()

	for _, s := range []string{"yt-search", "user-report"} {
		skillDir := filepath.Join(dir, s)
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + s + "\ndescription: Test\ndependencies:\n  - name: dep-" + s + "\n    install_cmd_macos: \"brew install dep-" + s + "\"\n    install_cmd_linux: \"apt install dep-" + s + "\"\n    check_cmd: \"command -v dep-" + s + "\"\n    optional: false\n---\n"
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	deps, err := ScanDependencies(dir, DefaultSkipSkills())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// yt-search is skipped, user-report is not
	if len(deps) != 1 {
		t.Fatalf("expected 1 dep (yt-search skipped), got %d", len(deps))
	}
	if deps[0].SourceSkill != "user-report" {
		t.Errorf("expected user-report dep, got %q", deps[0].SourceSkill)
	}
}

// TestScanDependencies_NoDeps verifies a skill with no dependencies block produces zero entries.
func TestScanDependencies_NoDeps(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "simple-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: simple-skill\ndescription: No deps\n---\n# body\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	deps, err := ScanDependencies(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deps) != 0 {
		t.Fatalf("expected 0 deps, got %d", len(deps))
	}
}

// TestScanDependencies_JSON verifies JSON marshaling of the result.
func TestScanDependencies_JSON(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "pagespeed-optimizer")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `---
name: pagespeed-optimizer
description: Test
dependencies:
  - name: lighthouse
    install_cmd_macos: "npm install -g lighthouse"
    install_cmd_linux: "npm install -g lighthouse"
    check_cmd: "command -v lighthouse"
    optional: false
  - name: cwebp
    install_cmd_macos: "brew install webp"
    install_cmd_linux: "sudo apt-get install -y webp"
    check_cmd: "command -v cwebp"
    optional: true
---
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	deps, err := ScanDependencies(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("expected 2 deps, got %d", len(deps))
	}

	// Verify JSON serialization works
	b, err := json.MarshalIndent(deps, "", "  ")
	if err != nil {
		t.Fatalf("JSON marshal failed: %v", err)
	}
	if len(b) == 0 {
		t.Error("empty JSON output")
	}
}
