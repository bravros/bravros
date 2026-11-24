package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bravros/bravros/cli/internal/skillsretired"
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

var skillsRetiredJSON bool

var skillsRetiredCmd = &cobra.Command{
	Use:   "retired",
	Short: "List retired skill names (renamed or removed) that setup/deploy/update prune",
	Long: `Print every skill name the toolkit used to ship and no longer does, one per line.

bravros setup, deploy and update move any of these found in the host skills dir
to <host>/.trash/<stamp>-retired-skills/ unless skills.preserve lists it.
--json adds the replacement and the first release without it.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		if skillsRetiredJSON {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(skillsretired.All())
		}
		for _, name := range skillsretired.Names() {
			fmt.Fprintln(out, name)
		}
		return nil
	},
}

func init() {
	skillsRetiredCmd.Flags().BoolVar(&skillsRetiredJSON, "json", false, "Emit name, replacement and retired_in as JSON")
	skillsCmd.AddCommand(skillsRetiredCmd)
	skillsDepsCmd.Flags().StringVarP(&skillsDepsFormat, "format", "f", "plain", "Output format (plain, json)")
	skillsDepsCmd.Flags().StringVar(&skillsDepsDir, "skills-dir", "", "Directory to scan (default: ~/.claude/skills)")
	skillsCmd.AddCommand(skillsDepsCmd)
	rootCmd.AddCommand(skillsCmd)
}
