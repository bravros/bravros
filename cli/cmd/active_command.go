package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var activeCommandCmd = &cobra.Command{
	Use:   "active-command",
	Short: "Manage the per-session active-command marker",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var activeCommandClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear the per-session active-command marker (idempotent)",
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID := resolveSession()
		if sessionID == "" {
			return nil
		}
		markerPath := filepath.Join(agentAuditDir(sessionID), "active-command")
		if err := os.Remove(markerPath); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			fmt.Fprintf(os.Stderr, "bravros active-command clear: %v\n", err)
			return err
		}
		return nil
	},
}

var activeCommandSetCmd = &cobra.Command{
	Use:   "set <skill-name>",
	Short: "Set the per-session active-command marker to <skill-name>",
	// No --override flag: the kaisser original needed one to bypass a
	// "must be run outside Claude Code" refusal that this port does not have,
	// so set always writes and the flag would be a no-op.
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		skillName := args[0]
		sessionID := resolveSession()
		if sessionID == "" {
			return nil
		}
		dir := agentAuditDir(sessionID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "bravros active-command set: mkdir %v\n", err)
			return err
		}
		markerPath := filepath.Join(dir, "active-command")
		if err := os.WriteFile(markerPath, []byte(skillName), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "bravros active-command set: %v\n", err)
			return err
		}

		historyPath := filepath.Join(dir, "command-history.txt")
		f, err := os.OpenFile(historyPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bravros active-command set: history %v\n", err)
			return err
		}
		defer f.Close()
		if _, err := f.WriteString(skillName + "\n"); err != nil {
			fmt.Fprintf(os.Stderr, "bravros active-command set: history %v\n", err)
			return err
		}
		return nil
	},
}

func init() {
	activeCommandCmd.AddCommand(activeCommandClearCmd)
	activeCommandCmd.AddCommand(activeCommandSetCmd)
	rootCmd.AddCommand(activeCommandCmd)
}

func resolveSession() string {
	if s := os.Getenv("CLAUDE_CODE_SESSION_ID"); s != "" {
		return s
	}
	if s := os.Getenv("CLAUDE_SESSION_ID"); s != "" {
		return s
	}
	return ""
}

func agentAuditDir(sessionID string) string {
	tmpDir := os.TempDir()
	return filepath.Join(tmpDir, "agent-audit-"+sessionID)
}
