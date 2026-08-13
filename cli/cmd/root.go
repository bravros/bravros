package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var Version = "0.1.0"

var rootVersionFlag bool

var rootCmd = &cobra.Command{
	Use:   "bravros",
	Short: "Free, public, host-neutral agent toolkit",
	Long:  "bravros is the SDLC kernel binary: atomic, destructive, token-gated, and headless primitives (commit, merge-lock, promote, worktree, selfupdate, ha, …).",
	Run: func(cmd *cobra.Command, args []string) {
		if rootVersionFlag {
			v := strings.TrimPrefix(Version, "v")
			fmt.Println("bravros v" + v)
			os.Exit(0)
		}
		cmd.Help()
	},
}

func init() {
	rootCmd.Flags().BoolVarP(&rootVersionFlag, "version", "v", false, "Print version and exit")

	rootCmd.AddCommand(prReviewCmd)
	rootCmd.AddCommand(haCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(statuslineCmd)
	rootCmd.AddCommand(hookCmd)
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
