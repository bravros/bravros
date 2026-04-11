package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/bravros/private/internal/stack"
	"github.com/spf13/cobra"
)

var (
	detectStackWrite    bool
	detectStackVersions bool
	detectStackField    string
)

var detectStackCmd = &cobra.Command{
	Use:   "detect-stack [path]",
	Short: "Detect project stack, framework, and test runner",
	Long: `Detect the project's tech stack from marker files (composer.json, package.json, go.mod, etc.).
Supports single-stack and monorepo layouts. Use --write to persist results to .bravros.yml.`,
	Run: func(cmd *cobra.Command, args []string) {
		root, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(args) > 0 {
			root = args[0]
		}

		result, err := stack.Detect(root, stack.DetectOpts{
			Versions: detectStackVersions,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if detectStackWrite {
			if err := stack.WriteConfig(root, result); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintln(os.Stderr, "Wrote .bravros.yml")
		}

		b, _ := json.MarshalIndent(result, "", "  ")
		jsonOutput := string(b)
		if detectStackField != "" {
			fmt.Println(fieldExtract(jsonOutput, detectStackField))
		} else {
			fmt.Println(jsonOutput)
		}
	},
}

func init() {
	detectStackCmd.Flags().BoolVar(&detectStackWrite, "write", false, "Write detection results to .bravros.yml")
	detectStackCmd.Flags().BoolVar(&detectStackVersions, "versions", false, "Include detailed package version output")
	detectStackCmd.Flags().StringVar(&detectStackField, "field", "", "Extract a single field value (dot notation)")
	rootCmd.AddCommand(detectStackCmd)
}
