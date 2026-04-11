package cmd

import (
	"fmt"
	"os"

	"github.com/bravros/bravros/internal/plan"
	"github.com/spf13/cobra"
)

var (
	checkStatusPlanFile string
	checkStatusField    string
)

var checkStatusCmd = &cobra.Command{
	Use:   "plan-check-status",
	Short: "Check whether a plan-check has been performed (git log + file marker)",
	Run: func(cmd *cobra.Command, args []string) {
		planFile := checkStatusPlanFile
		if planFile == "" && len(args) > 0 {
			planFile = args[0]
		}

		result, err := plan.CheckPlanCheckStatus(planFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		jsonOutput := result.JSON()
		if checkStatusField != "" {
			fmt.Println(fieldExtract(jsonOutput, checkStatusField))
		} else {
			fmt.Println(jsonOutput)
		}
	},
}

func init() {
	checkStatusCmd.Flags().StringVar(&checkStatusPlanFile, "plan-file", "", "Plan file path (default: auto-detect)")
	checkStatusCmd.Flags().StringVar(&checkStatusField, "field", "", "Extract a single field value (dot notation)")
	rootCmd.AddCommand(checkStatusCmd)
}
