package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	projectinit "github.com/bravros/bravros/cli/internal/init"
	"github.com/spf13/cobra"
)

var (
	initStack     string
	initSkipHooks bool

	// Deprecated no-op flags — accepted so existing callers do not break.
	initSkipWorkflows bool
	initSkipStaging   bool

	// expand-placeholders flags
	expandOutput string

	// initExitFn is the function used to exit the process on fatal errors.
	// Replaced in tests to avoid os.Exit killing the test binary.
	initExitFn = os.Exit
)

var initCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Initialize the current repository with the SDLC structure",
	Long: `Initialize a repository with the Bravros SDLC structure:
- Detect the tech stack and write .bravros/config.json
- Create the .planning/backlog/archive/ directory structure
- Install the commit-msg and pre-push hooks into .bravros/hooks/
  and point git at them via core.hooksPath

init is strictly repo-local: everything it writes lives inside the repository
working tree. It never touches ~/.claude/, global git config, or any other
machine-wide state — installing the agent runtime is what "bravros install"
and "bravros deploy" are for.

Next step after init:
  bravros commit "<emoji> <type>: <subject>" <files...>`,
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
			Root:          root,
			StackOverride: initStack,
			SkipHooks:     initSkipHooks,
		}

		result, err := projectinit.Init(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			initExitFn(1)
			return
		}

		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(b))
		fmt.Println(`next: bravros commit "<emoji> <type>: <subject>" <files...>`)

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

	// Deprecated no-ops, kept so existing callers keep working: init no longer
	// creates .github/workflows/ or a staging branch under any circumstances.
	initCmd.Flags().BoolVar(&initSkipWorkflows, "skip-workflows", false, "Deprecated no-op")
	initCmd.Flags().BoolVar(&initSkipStaging, "skip-staging-branch", false, "Deprecated no-op")
	_ = initCmd.Flags().MarkDeprecated("skip-workflows", "init never creates .github/workflows/ — the flag is ignored")
	_ = initCmd.Flags().MarkDeprecated("skip-staging-branch", "init never creates branches — the flag is ignored")

	expandPlaceholdersCmd.Flags().StringVar(&expandOutput, "output", "", "Output file path (default: in-place)")

	initCmd.AddCommand(expandPlaceholdersCmd)
	rootCmd.AddCommand(initCmd)
}
