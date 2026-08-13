package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	gitpkg "github.com/bravros/bravros/cli/internal/git"
	wtpkg "github.com/bravros/bravros/cli/internal/worktree"
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
		branch := gitpkg.NormalizeWorktreeIdentifier(args[0])

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
	worktreeCleanupDryRun       bool
	worktreeCleanupBase         string
)

var worktreeCleanupCmd = &cobra.Command{
	Use:   "cleanup <path>",
	Short: "Remove a worktree and clean up branches",
	Long: `Remove a git worktree and optionally delete branches:
- Removes the worktree directory
- Deletes the local branch (unless permanent)
- Optionally deletes the remote branch (--delete-remote)
- Merge-checked destroy: refuses when the branch is not merged into
  origin/<base> (--base, default: staging_branch from .bravros.yml) unless
  --force is also passed
- Liveness guard: refuses (pids + commands listed) when live processes still
  have their working directory or open files inside the worktree — stop them
  or re-run with --force (which still prints the list); if the check cannot
  run (no lsof, timeout) cleanup proceeds with liveness: "unknown"
- --dry-run prints the labelled teardown scope (dir, branch, anything else
  it would remove) plus the live-process report and exits without touching
  anything
- Use --force to remove dirty worktrees and bypass the merge check and the
  liveness guard`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		path := args[0]

		opts := gitpkg.CleanupOpts{
			Force:        worktreeCleanupForce,
			DeleteRemote: worktreeCleanupDeleteRemote,
			DryRun:       worktreeCleanupDryRun,
			BaseBranch:   worktreeCleanupBase,
		}

		result, err := gitpkg.WorktreeCleanup(path, opts)
		if err != nil {
			errResult := map[string]interface{}{
				"error": err.Error(),
			}
			// Liveness refusal: surface the pid+command list and the
			// override hint alongside the error message.
			var lerr *gitpkg.LivenessError
			if errors.As(err, &lerr) {
				errResult["live"] = lerr.Live
				errResult["hint"] = "stop them or re-run with --force"
			}
			b, _ := json.MarshalIndent(errResult, "", "  ")
			fmt.Println(string(b))
			os.Exit(1)
		}

		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(b))
	},
}

// ─── setup-full ────────────────────────────────────────────────────────────

var (
	worktreeSetupFullFrom      string
	worktreeSetupFullBase      string
	worktreeSetupFullNoInstall bool
	worktreeSetupFullNoEnv     bool
	worktreeSetupFullNoBuild   bool
	worktreeSetupFullHerd      bool
	worktreeSetupFullInstall   bool
)

var worktreeSetupFullCmd = &cobra.Command{
	Use:   "setup-full <branch>",
	Short: "Create a ready-to-execute worktree: git-worktree-add + deps + .env + asset symlink",
	Long: `Create a worktree and prepare it for immediate execution:
- git worktree add <path> -b <branch> (idempotent — skips if already present)
- Runtime dirs (vendor, node_modules, built assets) are APFS copy-on-write
  cloned from the parent when present there; falls back to a stack-aware
  install (composer / npm|pnpm|bun|yarn / uv / go mod) when parent dirs are
  missing, or always with --install
- Copies primary .env (not symlink — worktrees need distinct ports/APP_URL)
- Symlinks framework-conventional asset directory (public/build, dist, .next, .output)
- Warns (non-fatal) on lockfile/manifest drift vs the parent's tracked HEAD,
  and on post-create smoke-check failures (HEAD not descended from base,
  dirty tree, missing runtime dirs)

Flags let callers skip individual steps (--no-install / --no-env / --no-build),
force a real install instead of cloning (--install), or run Laravel Herd
linking (--herd). --from overrides primary worktree detection.

Emits JSON: {path, branch, created, framework, package_manager, deps_installed,
cloned, env_copied, build_linked, ready, skipped, warnings, error}. All
subsequent runs are idempotent — existing worktrees and already-installed dep
outputs are skipped.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		branch := gitpkg.NormalizeWorktreeIdentifier(args[0])
		opts := wtpkg.Opts{
			From:      worktreeSetupFullFrom,
			BaseRef:   worktreeSetupFullBase,
			NoInstall: worktreeSetupFullNoInstall,
			NoEnv:     worktreeSetupFullNoEnv,
			NoBuild:   worktreeSetupFullNoBuild,
			Herd:      worktreeSetupFullHerd,
			Install:   worktreeSetupFullInstall,
		}

		result, err := wtpkg.SetupFull(branch, opts)
		if err != nil {
			// Still emit the partial result so callers can see what completed.
			if result != nil && result.Error == "" {
				result.Error = err.Error()
			}
			if result == nil {
				result = &wtpkg.SetupResult{Branch: branch, Error: err.Error()}
			}
			b, _ := json.MarshalIndent(result, "", "  ")
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

	// setup-full flags
	worktreeSetupFullCmd.Flags().StringVar(&worktreeSetupFullFrom, "from", "", "Primary worktree path (for .env copy + build symlink). Default: cwd")
	worktreeSetupFullCmd.Flags().StringVar(&worktreeSetupFullBase, "base", "", "Base branch (e.g. origin/homolog). Default: staging_branch from .bravros.yml")
	worktreeSetupFullCmd.Flags().BoolVar(&worktreeSetupFullNoInstall, "no-install", false, "Skip dependency install step")
	worktreeSetupFullCmd.Flags().BoolVar(&worktreeSetupFullNoEnv, "no-env", false, "Skip .env copy step")
	worktreeSetupFullCmd.Flags().BoolVar(&worktreeSetupFullNoBuild, "no-build", false, "Skip asset symlink step")
	worktreeSetupFullCmd.Flags().BoolVar(&worktreeSetupFullHerd, "herd", false, "Laravel-only: prefix composer install with `herd`")
	worktreeSetupFullCmd.Flags().BoolVar(&worktreeSetupFullInstall, "install", false, "Force real dependency installs; skip APFS copy-on-write runtime-dir cloning from the parent")

	// cleanup flags
	worktreeCleanupCmd.Flags().BoolVar(&worktreeCleanupForce, "force", false, "Force remove dirty worktree; also bypasses the merge-checked destroy guard and the liveness guard")
	worktreeCleanupCmd.Flags().BoolVar(&worktreeCleanupDeleteRemote, "delete-remote", false, "Also delete the remote branch")
	worktreeCleanupCmd.Flags().BoolVar(&worktreeCleanupDryRun, "dry-run", false, "Print the teardown scope and exit without removing anything")
	worktreeCleanupCmd.Flags().StringVar(&worktreeCleanupBase, "base", "", "Base branch to verify merge status against (default: staging_branch from .bravros.yml)")

	worktreeCmd.AddCommand(worktreeSetupCmd)
	worktreeCmd.AddCommand(worktreeSetupFullCmd)
	worktreeCmd.AddCommand(worktreeCleanupCmd)
	rootCmd.AddCommand(worktreeCmd)
}
