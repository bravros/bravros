package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bravros/bravros/internal/paths"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// skillMeta represents SKILL.md YAML frontmatter.
type skillMeta struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	Dir         string `yaml:"-" json:"dir"`
	HasEvals    bool   `yaml:"-" json:"has_evals"`
	HasRefs     bool   `yaml:"-" json:"has_refs"`
}

var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Skill health check and management",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

// ─── list ───────────────────────────────────────────────────────────────────

var skillsListFormat string

var skillsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all installed skills with name, description, evals, and refs status",
	Run: func(cmd *cobra.Command, args []string) {
		skills := scanSkills()
		if len(skills) == 0 {
			fmt.Println("No skills found.")
			return
		}

		switch strings.ToLower(skillsListFormat) {
		case "json":
			data, _ := json.MarshalIndent(skills, "", "  ")
			fmt.Println(string(data))
		default:
			fmt.Printf("%-25s %-6s %-5s %s\n", "SKILL", "EVALS", "REFS", "DESCRIPTION")
			fmt.Println(strings.Repeat("─", 90))
			for _, s := range skills {
				evals := "  ✗"
				if s.HasEvals {
					evals = "  ✓"
				}
				refs := " ✗"
				if s.HasRefs {
					refs = " ✓"
				}
				desc := s.Description
				if len(desc) > 50 {
					desc = desc[:47] + "..."
				}
				// Collapse multi-line descriptions to single line
				desc = strings.Join(strings.Fields(desc), " ")
				if len(desc) > 50 {
					desc = desc[:47] + "..."
				}
				fmt.Printf("%-25s %s   %s   %s\n", s.Name, evals, refs, desc)
			}
			fmt.Printf("\nTotal: %d skills\n", len(skills))
		}
	},
}

// ─── verify ─────────────────────────────────────────────────────────────────

var skillsVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Validate all skills have required frontmatter, evals, and structure",
	Run: func(cmd *cobra.Command, args []string) {
		skills := scanSkills()
		issues := 0

		for _, s := range skills {
			problems := []string{}

			if s.Name == "" {
				problems = append(problems, "missing 'name' in frontmatter")
			}
			if s.Description == "" {
				problems = append(problems, "missing 'description' in frontmatter")
			}
			if !s.HasEvals {
				problems = append(problems, "no evals/ directory")
			}

			if len(problems) > 0 {
				issues += len(problems)
				fmt.Printf("⚠️  %s:\n", s.Dir)
				for _, p := range problems {
					fmt.Printf("   - %s\n", p)
				}
			}
		}

		if issues == 0 {
			fmt.Printf("✅ All %d skills pass verification.\n", len(skills))
		} else {
			fmt.Printf("\n%d issue(s) across %d skills.\n", issues, len(skills))
			os.Exit(1)
		}
	},
}

// ─── outdated ───────────────────────────────────────────────────────────────

var skillsOutdatedCmd = &cobra.Command{
	Use:   "outdated",
	Short: "Compare source (portable repo skills) vs deployed (~/.claude/skills)",
	Run: func(cmd *cobra.Command, args []string) {
		// portable repo skills
		sourceDir := filepath.Join(paths.PortableRepoDir(), "skills")
		home, _ := os.UserHomeDir()
		deployedDir := filepath.Join(home, ".claude", "skills")

		sourceSkills := scanSkillsIn(sourceDir)
		deployedSkills := scanSkillsIn(deployedDir)

		// Build maps
		srcMap := map[string]string{}
		for _, s := range sourceSkills {
			srcMap[s.Name] = s.Dir
		}
		depMap := map[string]string{}
		for _, s := range deployedSkills {
			depMap[s.Name] = s.Dir
		}

		outdated := 0
		missing := 0

		// Check for outdated (source newer than deployed)
		for _, s := range sourceSkills {
			depDir, exists := depMap[s.Name]
			if !exists {
				fmt.Printf("🆕 %s — in source but not deployed\n", s.Name)
				missing++
				continue
			}

			// Compare SKILL.md modification times
			srcInfo, err1 := os.Stat(filepath.Join(s.Dir, "SKILL.md"))
			depInfo, err2 := os.Stat(filepath.Join(depDir, "SKILL.md"))
			if err1 != nil || err2 != nil {
				continue
			}

			if srcInfo.ModTime().After(depInfo.ModTime()) {
				fmt.Printf("📦 %s — source is newer (run install.sh to update)\n", s.Name)
				outdated++
			}
		}

		// Check for deployed-only (removed from source)
		for _, s := range deployedSkills {
			if _, exists := srcMap[s.Name]; !exists {
				fmt.Printf("🗑️  %s — deployed but not in source (orphaned)\n", s.Name)
			}
		}

		if outdated == 0 && missing == 0 {
			fmt.Printf("✅ All %d deployed skills are up to date.\n", len(deployedSkills))
		} else {
			fmt.Printf("\n%d outdated, %d missing. Run install.sh to sync.\n", outdated, missing)
		}
	},
}

// ─── helpers ────────────────────────────────────────────────────────────────

func scanSkills() []skillMeta {
	home, _ := os.UserHomeDir()
	skillsDir := filepath.Join(home, ".claude", "skills")
	return scanSkillsIn(skillsDir)
}

func scanSkillsIn(dir string) []skillMeta {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var skills []skillMeta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillDir := filepath.Join(dir, e.Name())
		skillFile := filepath.Join(skillDir, "SKILL.md")

		data, err := os.ReadFile(skillFile)
		if err != nil {
			continue
		}

		meta := parseSkillFrontmatter(data)
		meta.Dir = skillDir
		if meta.Name == "" {
			meta.Name = e.Name()
		}

		// Check for evals
		if info, err := os.Stat(filepath.Join(skillDir, "evals")); err == nil && info.IsDir() {
			meta.HasEvals = true
		}

		// Check for references
		if info, err := os.Stat(filepath.Join(skillDir, "references")); err == nil && info.IsDir() {
			meta.HasRefs = true
		}

		skills = append(skills, meta)
	}
	return skills
}

func parseSkillFrontmatter(data []byte) skillMeta {
	content := string(data)
	if !strings.HasPrefix(content, "---") {
		return skillMeta{}
	}

	end := strings.Index(content[3:], "---")
	if end < 0 {
		return skillMeta{}
	}

	yamlBlock := content[3 : 3+end]
	var meta skillMeta
	yaml.Unmarshal([]byte(yamlBlock), &meta)
	return meta
}

func init() {
	skillsListCmd.Flags().StringVar(&skillsListFormat, "format", "table", "Output format: table or json")

	skillsCmd.AddCommand(skillsListCmd)
	skillsCmd.AddCommand(skillsVerifyCmd)
	skillsCmd.AddCommand(skillsOutdatedCmd)

	rootCmd.AddCommand(skillsCmd)
}
