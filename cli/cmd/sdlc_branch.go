package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	gitpkg "github.com/bravros/private/internal/git"
	"github.com/spf13/cobra"
)

var branchCheckoutOnly bool

var branchCmd = &cobra.Command{
	Use:   "branch",
	Short: "Branch management utilities",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var branchCreateCmd = &cobra.Command{
	Use:   "create <branch-name>",
	Short: "Create a new branch from the detected base branch",
	Long: `Create a new feature branch from the base branch.

Base branch priority:
  1. .bravros.yml staging_branch (if exists)
  2. DetectBaseBranch() (homolog → main → master)

With --checkout-only, only checks out and pulls the base branch
(no new branch creation). Used by /plan to sync before plan-review.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if branchCheckoutOnly {
			return nil // no branch name needed
		}
		if len(args) < 1 {
			return fmt.Errorf("requires a branch name argument")
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		var result *gitpkg.BranchCreateResult
		var err error

		if branchCheckoutOnly {
			result, err = gitpkg.CheckoutBase()
		} else {
			result, err = gitpkg.CreateBranch(args[0])
		}

		if err != nil {
			errJSON, _ := json.Marshal(map[string]string{"error": err.Error()})
			fmt.Fprintln(os.Stderr, string(errJSON))
			os.Exit(1)
		}

		b, _ := json.Marshal(result)
		fmt.Println(string(b))
	},
}

func init() {
	branchCreateCmd.Flags().BoolVar(&branchCheckoutOnly, "checkout-only", false, "Only checkout and pull base branch (no new branch)")
	branchCmd.AddCommand(branchCreateCmd)
	rootCmd.AddCommand(branchCmd)
}
