package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestCommitTypesRepo(t *testing.T) string {
	t.Helper()
	dir := initTestGitRepo(t)
	templatesDir := filepath.Join(dir, "templates")
	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `# canonical commit types
feat|✨|New features
fix|🐛|Bug fixes
docs|📚|Documentation
refactor|♻️|Restructuring
chore|🧹|Maintenance
`
	if err := os.WriteFile(filepath.Join(templatesDir, "commit-types.txt"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	chdirTo(t, dir)
	return dir
}

func TestCommitTypes_Plain(t *testing.T) {
	setupTestCommitTypesRepo(t)

	for _, format := range []string{"plain", ""} {
		commitTypesFormat = format
		var out bytes.Buffer
		commitTypesCmd.SetOut(&out)

		if err := commitTypesCmd.RunE(commitTypesCmd, nil); err != nil {
			t.Fatalf("format=%q RunE: %v", format, err)
		}

		expected := "feat\nfix\ndocs\nrefactor\nchore\n"
		if out.String() != expected {
			t.Errorf("format=%q got %q; want %q", format, out.String(), expected)
		}
	}
}

func TestCommitTypes_Markdown(t *testing.T) {
	setupTestCommitTypesRepo(t)

	commitTypesFormat = "markdown"
	var out bytes.Buffer
	commitTypesCmd.SetOut(&out)

	if err := commitTypesCmd.RunE(commitTypesCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "| Emoji | Type | Description |") {
		t.Errorf("markdown table header missing:\n%s", got)
	}
	if !strings.Contains(got, "| ✨ | `feat` | New features |") {
		t.Errorf("feat row missing in markdown:\n%s", got)
	}
	if !strings.Contains(got, "| 🐛 | `fix` | Bug fixes |") {
		t.Errorf("fix row missing in markdown:\n%s", got)
	}
	if !strings.Contains(got, "<!-- generated-from:") || !strings.Contains(got, "<!-- end-generated -->") {
		t.Errorf("generation comments missing in markdown:\n%s", got)
	}
}

func TestCommitTypes_Regex(t *testing.T) {
	setupTestCommitTypesRepo(t)

	commitTypesFormat = "regex"
	var out bytes.Buffer
	commitTypesCmd.SetOut(&out)

	if err := commitTypesCmd.RunE(commitTypesCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	expected := "(feat|fix|docs|refactor|chore)\n"
	if out.String() != expected {
		t.Errorf("got %q; want %q", out.String(), expected)
	}
}

func TestCommitTypes_JSON(t *testing.T) {
	setupTestCommitTypesRepo(t)

	commitTypesFormat = "json"
	var out bytes.Buffer
	commitTypesCmd.SetOut(&out)

	if err := commitTypesCmd.RunE(commitTypesCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	var types []struct {
		Slug        string `json:"slug"`
		Emoji       string `json:"emoji"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(out.Bytes(), &types); err != nil {
		t.Fatalf("unmarshal json: %v, raw output:\n%s", err, out.String())
	}

	if len(types) != 5 {
		t.Fatalf("expected 5 commit types, got %d", len(types))
	}
	if types[0].Slug != "feat" || types[0].Emoji != "✨" || types[0].Description != "New features" {
		t.Errorf("unexpected first type: %+v", types[0])
	}
	if types[1].Slug != "fix" || types[1].Emoji != "🐛" || types[1].Description != "Bug fixes" {
		t.Errorf("unexpected second type: %+v", types[1])
	}
}

func TestCommitTypes_UnknownFormat(t *testing.T) {
	setupTestCommitTypesRepo(t)

	commitTypesFormat = "yaml"
	var out bytes.Buffer
	commitTypesCmd.SetOut(&out)

	err := commitTypesCmd.RunE(commitTypesCmd, nil)
	if err == nil {
		t.Fatal("expected error for unknown format, got nil")
	}
	if !strings.Contains(err.Error(), `unknown format "yaml"`) {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCommitTypes_NotInGitRepo(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)

	commitTypesFormat = "plain"
	err := commitTypesCmd.RunE(commitTypesCmd, nil)
	if err == nil {
		t.Fatal("expected error when outside git repo, got nil")
	}
	if !strings.Contains(err.Error(), "cannot locate repo root") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCommitTypes_MissingCanonicalFile(t *testing.T) {
	dir := initTestGitRepo(t)
	chdirTo(t, dir)

	commitTypesFormat = "plain"
	err := commitTypesCmd.RunE(commitTypesCmd, nil)
	if err == nil {
		t.Fatal("expected error when commit-types.txt is missing, got nil")
	}
}

func TestLoadCommitTypes_Parsing(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "types.txt")
	content := `
# Header comment
  # Another indented comment

feat | ✨ | New features
fix|🐛|Bug fixes
invalid_no_pipes
only_one|pipe
chore | 🧹 | Maintenance work
`
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	types, err := loadCommitTypes(filePath)
	if err != nil {
		t.Fatalf("loadCommitTypes: %v", err)
	}

	if len(types) != 3 {
		t.Fatalf("expected 3 parsed types, got %d", len(types))
	}
	if types[0].Slug != "feat" || types[0].Emoji != "✨" || types[0].Description != "New features" {
		t.Errorf("type 0 mismatch: %+v", types[0])
	}
	if types[1].Slug != "fix" || types[1].Emoji != "🐛" || types[1].Description != "Bug fixes" {
		t.Errorf("type 1 mismatch: %+v", types[1])
	}
	if types[2].Slug != "chore" || types[2].Emoji != "🧹" || types[2].Description != "Maintenance work" {
		t.Errorf("type 2 mismatch: %+v", types[2])
	}
}
