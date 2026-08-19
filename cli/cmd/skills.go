package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Manage bravros skills",
}

var skillsDepsFormat string
var skillsDepsDir string

var skillsDepsCmd = &cobra.Command{
	Use:   "deps",
	Short: "List dependencies for all skills",
	RunE: func(cmd *cobra.Command, args []string) error {
		searchDir := skillsDepsDir
		if searchDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			searchDir = filepath.Join(home, ".claude", "skills")
		}

		type depEntry struct {
			Name            string `json:"name" yaml:"name"`
			InstallCmdMacos string `json:"install_cmd_macos" yaml:"install_cmd_macos"`
		}

		depsMap := make(map[string]depEntry)

		err := filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || filepath.Base(path) != "SKILL.md" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			content := string(data)
			if !strings.HasPrefix(content, "---") {
				return nil
			}
			endIdx := strings.Index(content[3:], "---")
			if endIdx == -1 {
				return nil
			}
			frontmatter := content[3 : endIdx+3]

			var meta struct {
				Dependencies []depEntry `yaml:"dependencies"`
			}
			if err := yaml.Unmarshal([]byte(frontmatter), &meta); err == nil {
				for _, d := range meta.Dependencies {
					depsMap[d.Name] = d
				}
			}
			return nil
		})
		if err != nil {
			return err
		}

		var deps []depEntry
		for _, d := range depsMap {
			deps = append(deps, d)
		}
		sort.Slice(deps, func(i, j int) bool { return deps[i].Name < deps[j].Name })

		out := cmd.OutOrStdout()
		if skillsDepsFormat == "json" {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(deps)
		}

		for _, d := range deps {
			fmt.Fprintf(out, "%s: %s\n", d.Name, d.InstallCmdMacos)
		}
		return nil
	},
}

func init() {
	skillsDepsCmd.Flags().StringVarP(&skillsDepsFormat, "format", "f", "plain", "Output format (plain, json)")
	skillsDepsCmd.Flags().StringVar(&skillsDepsDir, "skills-dir", "", "Directory to scan (default: ~/.claude/skills)")
	skillsCmd.AddCommand(skillsDepsCmd)
	rootCmd.AddCommand(skillsCmd)
}
