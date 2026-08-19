package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillsDeps_Plain(t *testing.T) {
	tempDir := t.TempDir()

	// Skill 1: git-skill
	s1 := filepath.Join(tempDir, "git-skill")
	if err := os.MkdirAll(s1, 0755); err != nil {
		t.Fatal(err)
	}
	content1 := `---
name: git-skill
dependencies:
  - name: git
    install_cmd_macos: brew install git
  - name: jq
    install_cmd_macos: brew install jq
---

# Git Skill
`
	if err := os.WriteFile(filepath.Join(s1, "SKILL.md"), []byte(content1), 0644); err != nil {
		t.Fatal(err)
	}

	// Skill 2: gh-skill (overlaps on jq)
	s2 := filepath.Join(tempDir, "gh-skill")
	if err := os.MkdirAll(s2, 0755); err != nil {
		t.Fatal(err)
	}
	content2 := `---
name: gh-skill
dependencies:
  - name: gh
    install_cmd_macos: brew install gh
  - name: jq
    install_cmd_macos: brew install jq
---

# GH Skill
`
	if err := os.WriteFile(filepath.Join(s2, "SKILL.md"), []byte(content2), 0644); err != nil {
		t.Fatal(err)
	}

	for _, format := range []string{"plain", ""} {
		skillsDepsFormat = format
		skillsDepsDir = tempDir

		var out bytes.Buffer
		skillsDepsCmd.SetOut(&out)

		if err := skillsDepsCmd.RunE(skillsDepsCmd, nil); err != nil {
			t.Fatalf("format=%q RunE: %v", format, err)
		}

		expected := "gh: brew install gh\ngit: brew install git\njq: brew install jq\n"
		if out.String() != expected {
			t.Errorf("format=%q got %q; want %q", format, out.String(), expected)
		}
	}
}

func TestSkillsDeps_JSON(t *testing.T) {
	tempDir := t.TempDir()

	sDir := filepath.Join(tempDir, "test-skill")
	if err := os.MkdirAll(sDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `---
name: test-skill
dependencies:
  - name: ripgrep
    install_cmd_macos: brew install ripgrep
  - name: fd
    install_cmd_macos: brew install fd
---

# Body
`
	if err := os.WriteFile(filepath.Join(sDir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	skillsDepsFormat = "json"
	skillsDepsDir = tempDir

	var out bytes.Buffer
	skillsDepsCmd.SetOut(&out)

	if err := skillsDepsCmd.RunE(skillsDepsCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	var deps []struct {
		Name            string `json:"name"`
		InstallCmdMacos string `json:"install_cmd_macos"`
	}
	if err := json.Unmarshal(out.Bytes(), &deps); err != nil {
		t.Fatalf("unmarshal json: %v, raw output: %q", err, out.String())
	}

	if len(deps) != 2 {
		t.Fatalf("expected 2 deps, got %d", len(deps))
	}
	if deps[0].Name != "fd" || deps[0].InstallCmdMacos != "brew install fd" {
		t.Errorf("unexpected dep 0: %+v", deps[0])
	}
	if deps[1].Name != "ripgrep" || deps[1].InstallCmdMacos != "brew install ripgrep" {
		t.Errorf("unexpected dep 1: %+v", deps[1])
	}
}

func TestSkillsDeps_DefaultSearchDir(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	defaultSkillsDir := filepath.Join(tempHome, ".claude", "skills", "sample")
	if err := os.MkdirAll(defaultSkillsDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `---
name: sample
dependencies:
  - name: fzf
    install_cmd_macos: brew install fzf
---
`
	if err := os.WriteFile(filepath.Join(defaultSkillsDir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	skillsDepsFormat = "plain"
	skillsDepsDir = "" // triggers default ~/.claude/skills

	var out bytes.Buffer
	skillsDepsCmd.SetOut(&out)

	if err := skillsDepsCmd.RunE(skillsDepsCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if !strings.Contains(out.String(), "fzf: brew install fzf") {
		t.Errorf("expected fzf dependency from default dir, got %q", out.String())
	}
}

func TestSkillsDeps_IgnoresNonSkillsAndInvalidFrontmatter(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Non-SKILL.md file
	_ = os.WriteFile(filepath.Join(tempDir, "README.md"), []byte("---\nname: not-a-skill\n---"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "SKILL.txt"), []byte("---\nname: not-a-skill\n---"), 0644)

	// 2. SKILL.md without leading ---
	s1 := filepath.Join(tempDir, "no-frontmatter")
	_ = os.MkdirAll(s1, 0755)
	_ = os.WriteFile(filepath.Join(s1, "SKILL.md"), []byte("# No frontmatter here"), 0644)

	// 3. SKILL.md with unclosed frontmatter
	s2 := filepath.Join(tempDir, "unclosed-frontmatter")
	_ = os.MkdirAll(s2, 0755)
	_ = os.WriteFile(filepath.Join(s2, "SKILL.md"), []byte("---\nname: unclosed\n"), 0644)

	// 4. SKILL.md with frontmatter but no dependencies
	s3 := filepath.Join(tempDir, "no-deps")
	_ = os.MkdirAll(s3, 0755)
	_ = os.WriteFile(filepath.Join(s3, "SKILL.md"), []byte("---\nname: no-deps\ndescription: test\n---\n"), 0644)

	skillsDepsFormat = "plain"
	skillsDepsDir = tempDir

	var out bytes.Buffer
	skillsDepsCmd.SetOut(&out)

	if err := skillsDepsCmd.RunE(skillsDepsCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if out.String() != "" {
		t.Errorf("expected empty output for files without dependencies, got %q", out.String())
	}
}
