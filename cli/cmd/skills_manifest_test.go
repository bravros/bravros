package cmd

import (
	"testing"
)

func TestManifestLoad(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() returned error: %v", err)
	}
	if m.Version == "" {
		t.Error("manifest version is empty")
	}
	if len(m.Skills) == 0 {
		t.Error("manifest has no skills")
	}
}

func TestManifestFindSkill(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}

	// Known core skill
	s := m.FindSkill("plan")
	if s == nil {
		t.Fatal("FindSkill(\"plan\") returned nil")
	}
	if s.Tier != "core" {
		t.Errorf("plan tier = %q, want \"core\"", s.Tier)
	}

	// Known premium skill
	s = m.FindSkill("brand-generator")
	if s == nil {
		t.Fatal("FindSkill(\"brand-generator\") returned nil")
	}
	if s.Tier != "premium" {
		t.Errorf("brand-generator tier = %q, want \"premium\"", s.Tier)
	}

	// Unknown skill
	s = m.FindSkill("nonexistent-skill")
	if s != nil {
		t.Error("FindSkill(\"nonexistent-skill\") should return nil")
	}
}

func TestManifestSkillsByTier(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}

	core := m.SkillsByTier("core")
	if len(core) == 0 {
		t.Error("no core skills found")
	}
	for _, s := range core {
		if s.Tier != "core" {
			t.Errorf("SkillsByTier(\"core\") returned skill %q with tier %q", s.Name, s.Tier)
		}
	}

	premium := m.SkillsByTier("premium")
	if len(premium) == 0 {
		t.Error("no premium skills found")
	}
	for _, s := range premium {
		if s.Tier != "premium" {
			t.Errorf("SkillsByTier(\"premium\") returned skill %q with tier %q", s.Name, s.Tier)
		}
	}

	// Unknown tier
	unknown := m.SkillsByTier("personal")
	if len(unknown) != 0 {
		t.Errorf("SkillsByTier(\"personal\") returned %d skills, want 0", len(unknown))
	}
}

func TestManifestAllSkillsHaveRequiredFields(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}

	for _, s := range m.Skills {
		if s.Name == "" {
			t.Error("skill with empty name found")
		}
		if s.Tier == "" {
			t.Errorf("skill %q has empty tier", s.Name)
		}
		if s.Tier != "core" && s.Tier != "premium" {
			t.Errorf("skill %q has unknown tier %q", s.Name, s.Tier)
		}
		if s.Description == "" {
			t.Errorf("skill %q has empty description", s.Name)
		}
		if s.Version == "" {
			t.Errorf("skill %q has empty version", s.Name)
		}
	}
}
