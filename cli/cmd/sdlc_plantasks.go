package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bravros/bravros/internal/plan"
	"github.com/spf13/cobra"
)

var (
	planTasksDiffCommit string
	planTasksField      string
)

var planTasksCmd = &cobra.Command{
	Use:   "plan-tasks [plan-file]",
	Short: "List or diff plan tasks as JSON",
	Run: func(cmd *cobra.Command, args []string) {
		planFile := ""
		if len(args) > 0 {
			planFile = args[0]
		}

		// Diff mode
		if planTasksDiffCommit != "" {
			result, err := plan.TaskDiff(planFile, planTasksDiffCommit)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			b, _ := json.MarshalIndent(result, "", "  ")
			jsonOutput := string(b)
			if planTasksField != "" {
				fmt.Println(fieldExtract(jsonOutput, planTasksField))
			} else {
				fmt.Println(jsonOutput)
			}
			return
		}

		// List mode: parse current plan file
		if planFile == "" {
			matches, _ := filepath.Glob(".planning/*-todo.md")
			if len(matches) == 0 {
				fmt.Fprintf(os.Stderr, "Error: no plan file found\n")
				os.Exit(1)
			}
			planFile = matches[0]
		}

		content, err := os.ReadFile(planFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot read %s: %v\n", planFile, err)
			os.Exit(1)
		}

		tasks := plan.ParseTasks(string(content))
		b, _ := json.MarshalIndent(tasks, "", "  ")
		jsonOutput := string(b)
		if planTasksField != "" {
			fmt.Println(fieldExtract(jsonOutput, planTasksField))
		} else {
			fmt.Println(jsonOutput)
		}
	},
}

func init() {
	planTasksCmd.Flags().StringVar(&planTasksDiffCommit, "diff", "", "Baseline commit for diff (or 'auto' for auto-detect)")
	planTasksCmd.Flags().StringVar(&planTasksField, "field", "", "Extract a single field value (dot notation)")
	rootCmd.AddCommand(planTasksCmd)
}
