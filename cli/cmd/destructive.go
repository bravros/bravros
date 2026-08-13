package cmd

import (
	"github.com/bravros/bravros/cli/internal/token"
	"github.com/spf13/cobra"
)

// destructiveGate is the out-of-band human-presence token gate that lets ONE
// genuinely permanent destructive command bypass audit Rule 52 (irreversible
// content-loss guard, P-0184). The token lives at
// ~/.claude/state/destructive-token; its path and JSON shape are read
// independently by Rule 52 — keep them stable.
var destructiveGate = token.Gate{
	Name:       "destructive",
	SuccessMsg: "Destructive token minted",
	RefuseMsg:  "✋ kaisser destructive unlock REFUSED: running inside Claude Code.",
	UnlockHelp: `The destructive token must be minted from a separate terminal outside
Claude Code. This is intentional — it proves human presence before content
git has never seen is destroyed permanently.

  1. Open a new terminal (outside Claude Code, same machine)
  2. Run: kaisser destructive unlock --reason "why"
  3. Return to Claude Code and re-run the blocked command

Prefer the reversible path instead: kaisser discard <paths> needs no token.

Suggested alias: alias destructive-unlock='kaisser destructive unlock'`,
}

var destructiveTTLFlag int
var destructiveDryRunFlag bool
var destructiveReasonFlag string
var destructiveFieldFlag string

var destructiveCmd = &cobra.Command{
	Use:   "destructive",
	Short: "Gate permanently destructive commands with a human-presence token",
	Long: `Out-of-band token gate for audit Rule 52 (irreversible content-loss guard).

kaisser destructive unlock   — mint token (REFUSED inside Claude Code)
kaisser destructive status   — check token presence and expiry
kaisser destructive revoke   — delete token (safe inside Claude Code)

The token lives at ~/.claude/state/destructive-token, is single-use with a
5-minute TTL, and authorizes exactly ONE Rule-52-blocked command (it is
consumed on first use). For reversible discards no token is needed —
use kaisser discard <paths> instead.`,
}

var destructiveUnlockCmd = &cobra.Command{
	Use:   "unlock",
	Short: "Mint a destructive token (must be run OUTSIDE Claude Code)",
	Long: `Writes a time-limited destructive token to ~/.claude/state/destructive-token.

REFUSED when running inside a Claude Code session (CLAUDE_CODE_SESSION_ID
canonical Claude Code v2.1.132+, CLAUDE_SESSION_ID legacy fallback, or
CLAUDE_CODE_ENTRYPOINT env vars are set, or a "claude" process is detected
in the parent process chain). The user must run this from a separate terminal.

After minting, re-run the blocked destructive command inside Claude Code.
The token is single-use: Rule 52 consumes (deletes) it on first use.`,
	Run: func(cmd *cobra.Command, args []string) {
		mintGateTokenReason(destructiveGate, destructiveTTLFlag, destructiveDryRunFlag, destructiveReasonFlag)
	},
}

var destructiveStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check destructive token presence and expiry",
	Long:  `Reports JSON status of the destructive token: {present, age_seconds, ttl_remaining, tty, expires_at}.`,
	Run: func(cmd *cobra.Command, args []string) {
		gateStatus(destructiveGate, destructiveFieldFlag)
	},
}

var destructiveRevokeCmd = &cobra.Command{
	Use:   "revoke",
	Short: "Delete the destructive token (allowed inside Claude Code)",
	Long:  `Deletes ~/.claude/state/destructive-token. Safe to run from any context including inside Claude Code.`,
	Run: func(cmd *cobra.Command, args []string) {
		gateRevoke(destructiveGate, "Destructive token revoked")
	},
}

func init() {
	rootCmd.AddCommand(destructiveCmd)
	destructiveCmd.AddCommand(destructiveUnlockCmd)
	destructiveCmd.AddCommand(destructiveStatusCmd)
	destructiveCmd.AddCommand(destructiveRevokeCmd)

	destructiveUnlockCmd.Flags().IntVar(&destructiveTTLFlag, "ttl", 5, "Token TTL in minutes (default: 5)")
	destructiveUnlockCmd.Flags().BoolVar(&destructiveDryRunFlag, "dry-run", false, "Preview token mint without writing")
	destructiveUnlockCmd.Flags().StringVar(&destructiveReasonFlag, "reason", "", "Why the destruction must be permanent (recorded in the token)")
	destructiveStatusCmd.Flags().StringVar(&destructiveFieldFlag, "field", "", "Print only this field (present, age_seconds, ttl_remaining, tty, expires_at)")
}
