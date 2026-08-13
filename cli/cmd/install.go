package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// installCmd is the host-oriented install entry point. Since the multi-harness
// adapters (Codex/OpenCode/Pi) and the per-host skills compiler were retired
// (P-0187), the only supported platform is Claude Code, whose global install
// remains `bash install.sh` (project-scoped setup is `bravros init`).
var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the toolkit runtime (delegates to install.sh)",
	Long: `Install bravros for Claude Code.

Global install remains: bash install.sh
Project-scoped setup:   bravros init

This preserves the existing ~/.claude runtime, settings, skills, secrets, MCP,
and auto-update flow.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("unknown install platform %q (only Claude Code is supported; multi-harness adapters were retired)", args[0])
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Claude Code install remains: bash install.sh")
		fmt.Fprintln(cmd.OutOrStdout(), "This preserves the existing ~/.claude runtime, settings, skills, secrets, MCP, and auto-update flow.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(installCmd)
}
