package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/bravros/bravros/internal/deploy"
	"github.com/spf13/cobra"
)

var (
	deployDryRun    bool
	deployCountOnly bool
	deployField     string
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy claude config repo to ~/.claude/",
	Long: `Copy skills, hooks, templates, config/settings.json, config/statusline.sh,
and CLAUDE.md from the source repo to ~/.claude/.
Skips mcp.json (machine-specific) and scripts/ (empty).`,
	Run: func(cmd *cobra.Command, args []string) {
		opts := deploy.DeployOpts{
			DryRun:    deployDryRun,
			CountOnly: deployCountOnly,
		}

		result, err := deploy.Deploy(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		b, _ := json.MarshalIndent(result, "", "  ")
		jsonOutput := string(b)
		if deployField != "" {
			fmt.Println(fieldExtract(jsonOutput, deployField))
		} else {
			fmt.Println(jsonOutput)
		}
	},
}

func init() {
	deployCmd.Flags().BoolVar(&deployDryRun, "dry-run", false, "List files that would be deployed without copying")
	deployCmd.Flags().BoolVar(&deployCountOnly, "count-only", false, "Only output count of files to deploy")
	deployCmd.Flags().StringVar(&deployField, "field", "", "Extract a single field value (dot notation)")
	rootCmd.AddCommand(deployCmd)
}
