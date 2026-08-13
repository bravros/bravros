package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type SkillMetadata struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
}

type SkillYaml struct {
	Slug     string                   `json:"slug"`
	Category string                   `yaml:"category" json:"category"`
	Icon     string                   `yaml:"icon" json:"icon"`
	Metadata map[string]SkillMetadata `yaml:"metadata" json:"metadata"`
	Body     string                   `yaml:"body" json:"body"`
}

func main() {
	skillsDir := "skills"
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		fmt.Printf("Error reading skills directory: %v\n", err)
		os.Exit(1)
	}

	var skills []SkillYaml

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		yamlPath := filepath.Join(skillsDir, entry.Name(), "skill.yaml")
		if _, err := os.Stat(yamlPath); os.IsNotExist(err) {
			continue
		}

		data, err := os.ReadFile(yamlPath)
		if err != nil {
			fmt.Printf("Error reading %s: %v\n", yamlPath, err)
			continue
		}

		var skill SkillYaml
		if err := yaml.Unmarshal(data, &skill); err != nil {
			fmt.Printf("Error parsing %s: %v\n", yamlPath, err)
			continue
		}

		skill.Slug = entry.Name()
		skills = append(skills, skill)
	}

	// Create docs/catalog directory if not exists
	catalogDir := filepath.Join("docs", "catalog")
	if err := os.MkdirAll(catalogDir, 0755); err != nil {
		fmt.Printf("Error creating catalog directory: %v\n", err)
		os.Exit(1)
	}

	// Write catalog.json
	jsonPath := filepath.Join(catalogDir, "catalog.json")
	jsonData, err := json.MarshalIndent(skills, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling catalog to JSON: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(jsonPath, jsonData, 0644); err != nil {
		fmt.Printf("Error writing catalog.json: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully generated catalog.json with %d skills\n", len(skills))
}
