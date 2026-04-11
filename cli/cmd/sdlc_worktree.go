package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	gitpkg "github.com/bravros/bravros/internal/git"
	"github.com/spf13/cobra"
)

var worktreeCmd = &cobra.Command{
	Use:   "worktree",
	Short: "Worktree lifecycle management",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

// ─── setup ─────────────────────────────────────────────────────────────────

var (
	worktreeSetupPath     string
	worktreeSetupNoRebase bool
)

var worktreeSetupCmd = &cobra.Command{
	Use:   "setup <branch>",
	Short: "Create a worktree for the given branch",
	Long: `Create a git worktree for parallel development:
- Computes path from repo name + plan number (or use --path)
- Creates branch from base branch (staging_branch from .bravros.yml)
- Optionally rebases from base branch (skip with --no-rebase)`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		branch := args[0]

		opts := gitpkg.WorktreeOpts{
			NoRebase: worktreeSetupNoRebase,
		}

		result, err := gitpkg.WorktreeSetup(branch, worktreeSetupPath, opts)
		if err != nil {
			errResult := map[string]interface{}{
				"error": err.Error(),
			}
			b, _ := json.MarshalIndent(errResult, "", "  ")
			fmt.Println(string(b))
			os.Exit(1)
		}

		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(b))
	},
}

// ─── cleanup ───────────────────────────────────────────────────────────────

var (
	worktreeCleanupForce        bool
	worktreeCleanupDeleteRemote bool
)

var worktreeCleanupCmd = &cobra.Command{
	Use:   "cleanup <path>",
	Short: "Remove a worktree and clean up branches",
	Long: `Remove a git worktree and optionally delete branches:
- Removes the worktree directory
- Deletes the local branch (unless permanent)
- Optionally deletes the remote branch (--delete-remote)
- Use --force to remove dirty worktrees`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		path := args[0]

		opts := gitpkg.CleanupOpts{
			Force:        worktreeCleanupForce,
			DeleteRemote: worktreeCleanupDeleteRemote,
		}

		result, err := gitpkg.WorktreeCleanup(path, opts)
		if err != nil {
			errResult := map[string]interface{}{
				"error": err.Error(),
			}
			b, _ := json.MarshalIndent(errResult, "", "  ")
			fmt.Println(string(b))
			os.Exit(1)
		}

		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(b))
	},
}

func init() {
	// setup flags
	worktreeSetupCmd.Flags().StringVar(&worktreeSetupPath, "path", "", "Override worktree path (default: auto-computed)")
	worktreeSetupCmd.Flags().BoolVar(&worktreeSetupNoRebase, "no-rebase", false, "Skip rebase from base branch")

	// cleanup flags
	worktreeCleanupCmd.Flags().BoolVar(&worktreeCleanupForce, "force", false, "Force remove dirty worktree")
	worktreeCleanupCmd.Flags().BoolVar(&worktreeCleanupDeleteRemote, "delete-remote", false, "Also delete the remote branch")

	worktreeCmd.AddCommand(worktreeSetupCmd)
	worktreeCmd.AddCommand(worktreeCleanupCmd)
	rootCmd.AddCommand(worktreeCmd)
}
