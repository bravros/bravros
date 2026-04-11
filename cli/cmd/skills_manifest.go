package cmd

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed data/skill-manifest.json
var skillManifestJSON []byte

// SkillEntry represents a single skill in the manifest.
type SkillEntry struct {
	Name         string   `json:"name"`
	Tier         string   `json:"tier"`
	Description  string   `json:"description"`
	Version      string   `json:"version"`
	Dependencies []string `json:"dependencies"`
}

// SkillManifest represents the full skill manifest.
type SkillManifest struct {
	Version string       `json:"version"`
	Skills  []SkillEntry `json:"skills"`
}

// LoadManifest parses the embedded skill manifest JSON and returns the manifest.
func LoadManifest() (*SkillManifest, error) {
	var manifest SkillManifest
	if err := json.Unmarshal(skillManifestJSON, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse skill manifest: %w", err)
	}
	return &manifest, nil
}

// FindSkill returns the SkillEntry for the given name, or nil if not found.
func (m *SkillManifest) FindSkill(name string) *SkillEntry {
	for i := range m.Skills {
		if m.Skills[i].Name == name {
			return &m.Skills[i]
		}
	}
	return nil
}

// SkillsByTier returns all skills matching the given tier.
func (m *SkillManifest) SkillsByTier(tier string) []SkillEntry {
	var result []SkillEntry
	for _, s := range m.Skills {
		if s.Tier == tier {
			result = append(result, s)
		}
	}
	return result
}
