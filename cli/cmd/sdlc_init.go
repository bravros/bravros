package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	projectinit "github.com/bravros/bravros/internal/init"
	"github.com/spf13/cobra"
)

var (
	initStack         string
	initSkipHooks     bool
	initSkipWorkflows bool
	initSkipStaging   bool
)

var initCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Initialize project with SDLC structure",
	Long: `Initialize a project with the Bravros SDLC structure:
- Detect tech stack and write .bravros.yml
- Create .planning/backlog/archive/ directory structure
- Copy git hooks from templates
- Create .github/workflows/ directory
- Create staging branch (homolog) if missing`,
	Run: func(cmd *cobra.Command, args []string) {
		root, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
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
			os.Exit(1)
		}

		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(b))
	},
}

func init() {
	initCmd.Flags().StringVar(&initStack, "stack", "", "Override detected stack (e.g., laravel, nextjs, go)")
	initCmd.Flags().BoolVar(&initSkipHooks, "skip-hooks", false, "Skip git hooks installation")
	initCmd.Flags().BoolVar(&initSkipWorkflows, "skip-workflows", false, "Skip .github/workflows/ creation")
	initCmd.Flags().BoolVar(&initSkipStaging, "skip-staging-branch", false, "Skip staging branch creation")
	rootCmd.AddCommand(initCmd)
}
