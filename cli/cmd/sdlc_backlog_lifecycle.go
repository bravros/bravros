package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/bravros/private/internal/plan"
	"github.com/spf13/cobra"
)

var backlogPromoteCmd = &cobra.Command{
	Use:   "promote <id>",
	Short: "Promote a backlog item to planned status and archive it",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		result, err := plan.PromoteBacklog(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		printLifecycleResult(result)
	},
}

var backlogDoneCmd = &cobra.Command{
	Use:   "done <id>",
	Short: "Mark a backlog item as done and archive it",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		result, err := plan.DoneBacklog(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		printLifecycleResult(result)
	},
}

var backlogDropReason string

var backlogDropCmd = &cobra.Command{
	Use:   "drop <id>",
	Short: "Drop a backlog item with a reason and archive it",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if backlogDropReason == "" {
			fmt.Fprintf(os.Stderr, "Error: --reason is required for drop\n")
			os.Exit(1)
		}
		result, err := plan.DropBacklog(args[0], backlogDropReason)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		printLifecycleResult(result)
	},
}

func printLifecycleResult(result *plan.BacklogLifecycleResult) {
	b, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(b))
}

func init() {
	backlogDropCmd.Flags().StringVar(&backlogDropReason, "reason", "", "Reason for dropping the backlog item (required)")
	backlogCmd.AddCommand(backlogPromoteCmd)
	backlogCmd.AddCommand(backlogDoneCmd)
	backlogCmd.AddCommand(backlogDropCmd)
}
