package cmd

import (
	"os"

	"github.com/bravros/bravros/internal/audit"
	"github.com/spf13/cobra"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Pre-tool-use audit hook (reads JSON from stdin)",
	Long:  "Audits tool calls, catches violations, enforces prerequisite reads, blocks dangerous commands.",
	Run: func(cmd *cobra.Command, args []string) {
		// Parse payload from stdin
		payload, err := audit.ParsePayload(os.Stdin)
		if err != nil {
			// Can't parse → allow (don't block on errors)
			os.Exit(0)
		}

		// Create session state and logger
		state := audit.NewSessionState(payload.SessionID)
		logger := audit.NewLogger(payload.SessionID, payload.ToolName)
		defer logger.Close()

		// Log the call
		logger.LogCall(payload.Input)

		// Run all enforcement rules (may exit with code 2 on block)
		defer func() {
			if r := recover(); r != nil {
				// Never panic — would block all tool calls
				logger.Log("PANIC RECOVERED: " + payload.ToolName)
				os.Exit(0)
			}
		}()

		// Set license tier before running rules so rule 24 can gate free-tier skills.
		audit.LicenseTier = ""
		if claims := GetLicense(); claims != nil {
			audit.LicenseTier = claims.Tier
		}

		audit.RunRules(payload, state, logger)

		// All good
		os.Exit(0)
	},
}
