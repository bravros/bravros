package cmd

import (
	"fmt"
	"os"

	"github.com/bravros/private/internal/plan"
	"github.com/spf13/cobra"
)

var cleanTodosCmd = &cobra.Command{
	Use:   "clean-todos",
	Short: "Remove orphan -todo.md files that have a -complete.md counterpart",
	Long: `After squash-merging a PR and syncing main back into homolog, git may
recreate -todo.md files because the squash lost the rename history. This command
finds and removes any -todo.md that already has a -complete.md sibling.`,
	Run: func(cmd *cobra.Command, args []string) {
		removed, err := plan.CleanOrphanTodos()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(removed) == 0 {
			fmt.Println("No orphan -todo.md files found")
			return
		}
		for _, f := range removed {
			fmt.Printf("Removed: %s\n", f)
		}
	},
}

func init() {
	rootCmd.AddCommand(cleanTodosCmd)
}
