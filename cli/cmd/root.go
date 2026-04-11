package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/bravros/bravros/internal/i18n"
	"github.com/spf13/cobra"
)

var Version = "1.9.0"

var rootCmd = &cobra.Command{
	Use:   "bravros",
	Short: "Bravros — SDLC pipeline for Claude Code",
	Long:  "bravros consolidates SDLC scripts (audit, pr-review, deploy, and more) into a single high-performance Go binary.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		i18n.DetectLocale()
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	rootCmd.AddCommand(auditCmd)
	rootCmd.AddCommand(prReviewCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(statuslineCmd)
	rootCmd.AddCommand(selfupdateCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version",
	Run: func(cmd *cobra.Command, args []string) {
		v := strings.TrimPrefix(Version, "v")
		fmt.Println("bravros v" + v)
	},
}

func Execute() error {
	// If called as just "bravros" with no args, show help
	if len(os.Args) == 1 {
		rootCmd.Help()
		return nil
	}
	return rootCmd.Execute()
}
