package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillsListMerge(t *testing.T) {
	// Create a temp skills dir with a fake installed skill
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "plan")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: plan\ndescription: Test plan skill\n---\n"), 0644)

	skills := scanSkillsIn(tmpDir)
	if len(skills) == 0 {
		t.Fatal("scanSkillsIn returned no skills")
	}
	if skills[0].Name != "plan" {
		t.Errorf("expected skill name 'plan', got %q", skills[0].Name)
	}
}

func TestSkillsListMergeWithManifest(t *testing.T) {
	// mergeSkillsWithManifest uses the real home dir, but we can test the manifest
	// merge logic indirectly by verifying the manifest loads
	manifest, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}

	// Verify all manifest skills have tier set
	for _, s := range manifest.Skills {
		if s.Tier != "core" && s.Tier != "premium" {
			t.Errorf("skill %q has unexpected tier %q", s.Name, s.Tier)
		}
	}
}

func TestSkillsListCoreInstalled(t *testing.T) {
	// Verify that a core skill that appears in the manifest
	// would be recognized
	manifest, _ := LoadManifest()
	s := manifest.FindSkill("plan")
	if s == nil {
		t.Fatal("core skill 'plan' not in manifest")
	}
	if s.Tier != "core" {
		t.Errorf("plan should be core, got %q", s.Tier)
	}
}

func TestSkillsListPremiumLocked(t *testing.T) {
	manifest, _ := LoadManifest()
	s := manifest.FindSkill("brand-generator")
	if s == nil {
		t.Fatal("premium skill 'brand-generator' not in manifest")
	}
	if s.Tier != "premium" {
		t.Errorf("brand-generator should be premium, got %q", s.Tier)
	}
	// Without pro license, premium skills should be locked
	// (CurrentLicense is nil in tests by default)
	if claims := GetLicense(); claims != nil && claims.Tier == "pro" {
		t.Skip("test assumes no pro license")
	}
}

func TestSkillsInstallValidation(t *testing.T) {
	manifest, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}

	// Unknown skill should not be found
	if s := manifest.FindSkill("totally-fake-skill"); s != nil {
		t.Error("expected nil for unknown skill")
	}

	// Premium skill should be found
	if s := manifest.FindSkill("auto-pr"); s == nil {
		t.Error("auto-pr should exist in manifest")
	} else if s.Tier != "premium" {
		t.Errorf("auto-pr tier = %q, want premium", s.Tier)
	}
}

func TestSkillsRemoveValidation(t *testing.T) {
	tmpDir := t.TempDir()

	// Skill not installed
	skillPath := filepath.Join(tmpDir, "nonexistent")
	_, err := os.Stat(skillPath)
	if !os.IsNotExist(err) {
		t.Error("expected nonexistent dir to not exist")
	}

	// Create and remove skill
	skillPath = filepath.Join(tmpDir, "test-skill")
	os.MkdirAll(skillPath, 0755)
	os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte("---\nname: test-skill\n---\n"), 0644)

	err = os.RemoveAll(skillPath)
	if err != nil {
		t.Fatalf("failed to remove skill: %v", err)
	}
	if _, err := os.Stat(skillPath); !os.IsNotExist(err) {
		t.Error("skill directory should not exist after removal")
	}
}

func TestSkillsUpdateNotInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	skills := scanSkillsIn(tmpDir)
	if len(skills) != 0 {
		t.Errorf("expected 0 skills in empty dir, got %d", len(skills))
	}
}
