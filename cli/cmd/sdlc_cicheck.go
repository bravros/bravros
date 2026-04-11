package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bravros/bravros/internal/ci"
	"github.com/spf13/cobra"
)

var (
	ciCheckWorkflow string
	ciCheckBranch   string
	ciCheckField    string
)

var ciCheckCmd = &cobra.Command{
	Use:   "ci-check",
	Short: "Detect CI workflow status (file exists, recent runs, branch relevance)",
	Run: func(cmd *cobra.Command, args []string) {
		branch := ciCheckBranch
		if branch == "" {
			// detect current branch
			out, err := exec.Command("git", "branch", "--show-current").Output()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error detecting branch: %v\n", err)
				os.Exit(1)
			}
			branch = strings.TrimSpace(string(out))
		}

		result, err := ci.Check(ciCheckWorkflow, branch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		jsonOutput := result.JSON()
		if ciCheckField != "" {
			fmt.Println(fieldExtract(jsonOutput, ciCheckField))
		} else {
			fmt.Println(jsonOutput)
		}
	},
}

func init() {
	ciCheckCmd.Flags().StringVar(&ciCheckWorkflow, "workflow", "tests.yml", "Workflow filename to check")
	ciCheckCmd.Flags().StringVar(&ciCheckBranch, "branch", "", "Branch to check (default: current branch)")
	ciCheckCmd.Flags().StringVar(&ciCheckField, "field", "", "Extract a single field value (dot notation)")
	rootCmd.AddCommand(ciCheckCmd)
}
