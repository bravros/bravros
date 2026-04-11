package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	gitpkg "github.com/bravros/private/internal/git"
	"github.com/spf13/cobra"
)

var (
	mergePRDeleteBranch        bool
	mergePRAutoResolvePlanning bool
	mergePRMergeStrategy       string
)

var mergePRCmd = &cobra.Command{
	Use:   "merge-pr <number>",
	Short: "Merge a PR with conflict resolution and branch cleanup",
	Long: `Merge a GitHub PR with automatic conflict handling:
- Fetches and checks for conflicts before merging
- Merges base into feature branch if behind
- Auto-resolves .planning/ conflicts (optional)
- Merges via gh CLI using the specified strategy (squash, merge, or rebase)
- Cleans up local and remote branches (skips permanent branches)`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		prNumber, err := strconv.Atoi(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid PR number %q\n", args[0])
			os.Exit(1)
		}

		opts := gitpkg.MergePROpts{
			DeleteBranch:        mergePRDeleteBranch,
			AutoResolvePlanning: mergePRAutoResolvePlanning,
			MergeMethod:         mergePRMergeStrategy,
		}

		result := gitpkg.MergePR(prNumber, opts)

		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(b))

		if result.State != "merged" {
			os.Exit(1)
		}
	},
}

func init() {
	mergePRCmd.Flags().BoolVar(&mergePRDeleteBranch, "delete-branch", true, "Delete branch after merge (skipped for permanent branches)")
	mergePRCmd.Flags().BoolVar(&mergePRAutoResolvePlanning, "auto-resolve-planning", false, "Auto-resolve .planning/ merge conflicts")
	mergePRCmd.Flags().StringVar(&mergePRMergeStrategy, "merge-strategy", "squash", "Merge strategy: squash (default), merge, or rebase")
	rootCmd.AddCommand(mergePRCmd)
}
