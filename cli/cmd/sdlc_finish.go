package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/bravros/private/internal/plan"
	"github.com/spf13/cobra"
)

var (
	finishPR          string
	finishSkipBacklog bool
	finishDryRun      bool
)

var finishCmd = &cobra.Command{
	Use:   "finish [plan-file]",
	Short: "Mark plan complete, rename file, archive backlog",
	Long: `Finish a plan: sync frontmatter as completed, rename -todo.md to -complete.md,
optionally archive linked backlog item, and commit all changes.`,
	Run: func(cmd *cobra.Command, args []string) {
		opts := plan.FinishOpts{
			PRNumber:    finishPR,
			SkipBacklog: finishSkipBacklog,
			DryRun:      finishDryRun,
		}
		if len(args) > 0 {
			opts.PlanFile = args[0]
		}

		result, err := plan.Finish(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(b))
	},
}

func init() {
	finishCmd.Flags().StringVar(&finishPR, "pr", "", "PR number to record in frontmatter")
	finishCmd.Flags().BoolVar(&finishSkipBacklog, "skip-backlog", false, "Skip backlog archiving")
	finishCmd.Flags().BoolVar(&finishDryRun, "dry-run", false, "Report what would happen without changes")
	rootCmd.AddCommand(finishCmd)
}
