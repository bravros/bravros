package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var commitTypesFormat string

var commitTypesCmd = &cobra.Command{
	Use:   "commit-types",
	Short: "List canonical commit types from templates/commit-types.txt",
	Long: `Print the bravros canonical commit-type list in the requested format.

The canonical list lives in templates/commit-types.txt relative to the repo root
(located via git rev-parse --show-toplevel). All downstream consumers (the commit-msg
hook, auto-release.yml, skills/commit/SKILL.md) are derived from this single file.

Formats:
  plain     One slug per line (default). Suitable for hook consumption.
  markdown  A markdown table suitable for embedding in skills/commit/SKILL.md.
  regex     A pipe-alternation regex group: (slug1|slug2|...). Suitable for CI.
  json      Full JSON array of {slug, emoji, description} objects.

Examples:
  bravros commit-types
  bravros commit-types --format plain | wc -l
  bravros commit-types --format regex
  bravros commit-types --format markdown
  bravros commit-types --format json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := gitRepoRoot()
		if err != nil {
			return fmt.Errorf("commit-types: cannot locate repo root: %w", err)
		}
		canonicalPath := filepath.Join(repoRoot, "templates", "commit-types.txt")

		types, err := loadCommitTypes(canonicalPath)
		if err != nil {
			return fmt.Errorf("commit-types: %w", err)
		}

		out := cmd.OutOrStdout()
		switch commitTypesFormat {
		case "plain", "":
			for _, t := range types {
				fmt.Fprintln(out, t.Slug)
			}
		case "markdown":
			fmt.Fprintln(out, "<!-- generated-from: templates/commit-types.txt — regenerate with: bravros commit-types --format markdown -->")
			fmt.Fprintln(out, "| Emoji | Type | Description |")
			fmt.Fprintln(out, "|-------|------|-------------|")
			for _, t := range types {
				fmt.Fprintf(out, "| %s | `%s` | %s |\n", t.Emoji, t.Slug, t.Description)
			}
			fmt.Fprintln(out, "<!-- end-generated -->")
		case "regex":
			slugs := make([]string, len(types))
			for i, t := range types {
				slugs[i] = t.Slug
			}
			fmt.Fprintf(out, "(%s)\n", strings.Join(slugs, "|"))
		case "json":
			type jsonType struct {
				Slug        string `json:"slug"`
				Emoji       string `json:"emoji"`
				Description string `json:"description"`
			}
			outData := make([]jsonType, len(types))
			for i, t := range types {
				outData[i] = jsonType{Slug: t.Slug, Emoji: t.Emoji, Description: t.Description}
			}
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(outData)
		default:
			return fmt.Errorf("commit-types: unknown format %q (want: plain|markdown|regex|json)", commitTypesFormat)
		}
		return nil
	},
}

func init() {
	commitTypesCmd.Flags().StringVarP(&commitTypesFormat, "format", "f", "plain",
		"Output format: plain|markdown|regex|json")
	rootCmd.AddCommand(commitTypesCmd)
}

// gitRepoRoot returns the absolute path of the git repository root by walking
// up from the current working directory looking for a .git entry.
func gitRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("not inside a git repository")
}

type commitType struct {
	Slug        string
	Emoji       string
	Description string
}

func loadCommitTypes(path string) ([]commitType, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var types []commitType
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		types = append(types, commitType{
			Slug:        strings.TrimSpace(parts[0]),
			Emoji:       strings.TrimSpace(parts[1]),
			Description: strings.TrimSpace(parts[2]),
		})
	}
	return types, nil
}
