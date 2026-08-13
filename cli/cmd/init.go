package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/bravros/bravros/cli/internal/config"
	ideploy "github.com/bravros/bravros/cli/internal/deploy"
	projectinit "github.com/bravros/bravros/cli/internal/init"
	"github.com/spf13/cobra"
)

var (
	initStack         string
	initSkipHooks     bool
	initSkipWorkflows bool
	initSkipStaging   bool

	// B-0117: bootstrap flags
	initPortableRepo string
	initDeployMode   string
	initSkipSecrets  bool
	initSkipMCP      bool
	initForce        bool

	// expand-placeholders flags
	expandOutput string

	// initExitFn is the function used to exit the process on fatal errors.
	// Replaced in tests to avoid os.Exit killing the test binary.
	initExitFn = os.Exit
)

// resolveBootstrapDir returns the directory that init will deploy from.
// Priority: positional arg > --portable-repo / BRAVROS_PORTABLE_REPO > cwd.
// args is the cobra positional args slice; portableRepo is the resolved flag value.
func resolveBootstrapDir(root, portableRepo string, args []string) string {
	// Positional arg was provided — it already lives in root.
	if len(args) > 0 {
		return root
	}
	// Portable-repo flag/env wins over cwd when no positional arg given.
	if portableRepo != "" {
		return portableRepo
	}
	return root
}

var initCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Initialize project with SDLC structure (or bootstrap ~/.claude/)",
	Long: `Initialize a project with the Bravros SDLC structure:
- Detect tech stack and write .bravros.yml
- Create .planning/backlog/archive/ directory structure
- Copy git hooks from templates
- Create .github/workflows/ directory
- Create staging branch (homolog) if missing

When --portable-repo is provided (or BRAVROS_PORTABLE_REPO is set and the cwd
is a bravros config repo), also bootstraps ~/.claude/ using the specified
--deploy-mode (symlinks or copies). Symlinks are the default; falls back to
copies when the filesystem does not support them.

Next steps after init:
  bravros doctor && bravros secrets bootstrap && bravros mcp register --from config/mcp.json`,
	Run: func(cmd *cobra.Command, args []string) {
		root, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			initExitFn(1)
			return
		}
		if len(args) > 0 {
			root = args[0]
		}

		opts := projectinit.InitOpts{
			Root:              root,
			StackOverride:     initStack,
			SkipHooks:         initSkipHooks,
			SkipWorkflows:     initSkipWorkflows,
			SkipStagingBranch: initSkipStaging,
		}

		result, err := projectinit.Init(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			initExitFn(1)
			return
		}

		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(b))

		// B-0117 / B-0120: bootstrap ~/.claude/ when portable-repo is set.
		//
		// Resolve portableRepo from flag → env → platform default.
		portableRepo := initPortableRepo
		if portableRepo == "" {
			portableRepo = config.PortableRepo()
		}
		deployMode := initDeployMode
		if deployMode == "" {
			deployMode = config.DeployMode()
		}

		configDir := config.ConfigDir()

		// bootstrapDir is the directory to deploy from.
		// When --portable-repo is supplied explicitly (or resolved from env) AND no
		// positional arg was provided, the portable-repo path wins over cwd; this
		// is the path install.sh always passes.  When a positional arg was given
		// (bravros init /some/path), that path already sits in `root` and takes
		// priority — bootstrapDir == root in that case too.
		bootstrapDir := resolveBootstrapDir(root, portableRepo, args)

		// Only deploy when running from the bravros config repo.
		if ideploy.IsClaudeRepo(bootstrapDir) {
			if deployMode != "copies" {
				// Symlink mode.
				symResult, symlinkErr := ideploy.SymlinkDeploy(bootstrapDir, configDir)
				if symlinkErr != nil {
					// SymlinkDeploy cannot proceed (existing real dir at destination).
					// Auto-fall-back to copy-mode Deploy() which includes all hardening:
					// cleanBrokenSymlinks, prune-by-default, post-copy verify.
					if !initForce {
						fmt.Fprintf(os.Stderr, "ℹ️  symlink deploy unavailable (%v); falling back to copy-mode deploy with hardening\n", symlinkErr)
					} else {
						fmt.Fprintf(os.Stderr, "ℹ️  symlink deploy unavailable (%v); falling back to copy-mode deploy with hardening (--force acknowledged)\n", symlinkErr)
					}
					deployOpts := ideploy.DeployOpts{
						SourceDir:      bootstrapDir,
						TargetDir:      configDir,
						PreserveSkills: config.PreservedSkills(),
					}
					if _, fallbackErr := ideploy.Deploy(deployOpts); fallbackErr != nil {
						fmt.Fprintf(os.Stderr, "⚠️  copy-mode fallback deploy failed: %v\n", fallbackErr)
						initExitFn(1)
						return
					}
					fmt.Printf("✅ deployed ~/.claude/ via copy (symlink fallback)\n")
				} else {
					fmt.Printf("✅ deployed ~/.claude/ via %s (mode: %s)\n", symResult.Mode, deployMode)
				}
			} else {
				// Copy mode — use existing deploy logic.
				deployOpts := ideploy.DeployOpts{
					SourceDir:      bootstrapDir,
					TargetDir:      configDir,
					PreserveSkills: config.PreservedSkills(),
				}
				if _, deployErr := ideploy.Deploy(deployOpts); deployErr != nil {
					fmt.Fprintf(os.Stderr, "⚠️  copy deploy: %v\n", deployErr)
					initExitFn(1)
					return
				}
				fmt.Printf("✅ deployed ~/.claude/ via copy\n")
			}

			if !initSkipSecrets {
				fmt.Printf("next: bravros doctor && bravros secrets bootstrap && bravros mcp register --from config/mcp.json\n")
			}
		} else if initPortableRepo != "" {
			// --portable-repo was explicitly passed but bootstrapDir is not a bravros
			// config repo.  Fail loudly so install.sh surfaces the misconfiguration
			// instead of silently skipping the bootstrap step.
			//
			// Loud failure is gated on the explicit flag (initPortableRepo), NOT on
			// the resolved portableRepo from env/config.  install.sh always passes
			// --portable-repo, so the flag is the right gate for the install.sh
			// contract.  Treating env/config fallback the same way would surface a
			// noisy error in interactive `bravros init` sessions where the env var
			// happens to be set to a stale path — those should silently skip.
			//
			// We check existence first because IsClaudeRepo is basename-only —
			// /tmp/does-not-exist/claude would pass the basename check but fail
			// at deploy with only a warning.  Catching the missing path here
			// gives a clearer error.
			if _, err := os.Stat(bootstrapDir); err != nil {
				fmt.Fprintf(os.Stderr, "Error: --portable-repo %q does not exist: %v\n", bootstrapDir, err)
				initExitFn(1)
				return
			} else if !ideploy.IsClaudeRepo(bootstrapDir) {
				fmt.Fprintf(os.Stderr, "Error: --portable-repo %q is not a bravros config repo (expected basename \"claude\")\n", bootstrapDir)
				initExitFn(1)
				return
			}
		}
	},
}

// expandPlaceholdersCmd implements `bravros init expand-placeholders <file>`.
var expandPlaceholdersCmd = &cobra.Command{
	Use:   "expand-placeholders <file>",
	Short: "Expand {{PLACEHOLDER}} tokens in a template file using detected stack values",
	Long: `Read a template file and replace placeholder tokens with values sourced from
stack detection (bravros detect-stack) and project metadata (bravros meta).

Supported placeholders:
  {{PROJECT_NAME}}  — project name from git remote or directory basename
  {{STACK}}         — detected language (go, php, node, python, rust)
  {{FRAMEWORK}}     — detected framework (laravel, nextjs, express, django, none)
  {{TEST_RUNNER}}   — detected test runner (pest, jest, vitest, pytest, go test, none)
  {{BASE_BRANCH}}   — base/default branch (main or master)

The operation is idempotent: running twice on an already-expanded file is a no-op.
If the file contains no placeholders, exits 0 silently without writing anything.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		inputFile := args[0]

		root, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("cannot determine working directory: %w", err)
		}

		opts := projectinit.ExpandOpts{
			Root:       root,
			InputFile:  inputFile,
			OutputFile: expandOutput,
		}

		result, err := projectinit.ExpandPlaceholders(opts)
		if err != nil {
			return fmt.Errorf("expand placeholders: %w", err)
		}

		if result.NoOp {
			// No placeholders found — silent no-op.
			return nil
		}

		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(b))
		return nil
	},
}

func init() {
	initCmd.Flags().StringVar(&initStack, "stack", "", "Override detected stack (e.g., laravel, nextjs, go)")
	initCmd.Flags().BoolVar(&initSkipHooks, "skip-hooks", false, "Skip git hooks installation")
	initCmd.Flags().BoolVar(&initSkipWorkflows, "skip-workflows", false, "Skip .github/workflows/ creation")
	initCmd.Flags().BoolVar(&initSkipStaging, "skip-staging-branch", false, "Skip staging branch creation")

	// B-0117 bootstrap flags
	initCmd.Flags().StringVar(&initPortableRepo, "portable-repo", "", "Path to bravros config repo (default: $BRAVROS_PORTABLE_REPO)")
	initCmd.Flags().StringVar(&initDeployMode, "deploy-mode", "", "Deploy mode: symlinks|copies|auto (default: $BRAVROS_DEPLOY_MODE or symlinks)")
	initCmd.Flags().BoolVar(&initSkipSecrets, "skip-secrets", false, "Skip secrets bootstrap prompt in post-init summary")
	initCmd.Flags().BoolVar(&initSkipMCP, "skip-mcp", false, "Skip MCP register prompt in post-init summary")
	initCmd.Flags().BoolVar(&initForce, "force", false, "Force overwrite of existing non-symlink directories during deploy")

	expandPlaceholdersCmd.Flags().StringVar(&expandOutput, "output", "", "Output file path (default: in-place)")

	initCmd.AddCommand(expandPlaceholdersCmd)
	rootCmd.AddCommand(initCmd)
}
