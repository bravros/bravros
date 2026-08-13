package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/bravros/bravros/cli/internal/token"
	"github.com/spf13/cobra"
)

// promoteGate is the out-of-band human-presence token gate backing the
// /promote skill. The token lives at ~/.claude/state/promote-token and is
// consumed by the /promote skill after a successful merge. The on-disk path and
// JSON shape are read independently by audit Rule 17b — keep them stable.
var promoteGate = token.Gate{
	Name:       "promote",
	SuccessMsg: "Promote token minted",
	RefuseMsg:  "✋ kaisser promote unlock REFUSED: running inside Claude Code.",
	UnlockHelp: `The promote token must be minted from a separate terminal outside
Claude Code. This is intentional — it proves human presence.

  1. Open a new terminal (outside Claude Code, same machine)
  2. Run: kaisser promote unlock
  3. Return to Claude Code and run: /promote

Suggested alias: alias promote-unlock='kaisser promote unlock'`,
}

// promoteToken is retained as the JSON shape backing the promote token. It is an
// alias for token.Token so existing helpers/tests keep their field names.
type promoteToken = token.Token

// promoteTokenPath returns the canonical path for the promote token
// (~/.claude/state/promote-token).
func promoteTokenPath() string { return promoteGate.Path() }

// promoteReadToken reads and parses the promote token from disk. Returns nil
// when absent or unreadable.
func promoteReadToken() *promoteToken { return promoteGate.Read() }

// promoteTokenPresent returns true when a non-expired token exists. Auto-cleans
// expired tokens.
func promoteTokenPresent() bool { return promoteGate.Present() }

var promoteTTLFlag int
var promoteDryRunFlag bool
var promoteFieldFlag string

var promoteCmd = &cobra.Command{
	Use:   "promote",
	Short: "Promote homolog → main with human-presence token",
	Long: `Out-of-band token gate for fast homolog→main promotion.

kaisser promote unlock   — mint token (REFUSED inside Claude Code)
kaisser promote status   — check token presence and expiry
kaisser promote revoke   — delete token (safe inside Claude Code)

The token lives at ~/.claude/state/promote-token and is consumed by
the /promote skill after a successful merge.`,
}

var promoteUnlockCmd = &cobra.Command{
	Use:   "unlock",
	Short: "Mint a promote token (must be run OUTSIDE Claude Code)",
	Long: `Writes a time-limited promote token to ~/.claude/state/promote-token.

REFUSED when running inside a Claude Code session (CLAUDE_CODE_SESSION_ID
canonical Claude Code v2.1.132+, CLAUDE_SESSION_ID legacy fallback, or
CLAUDE_CODE_ENTRYPOINT env vars are set, or a "claude" process is detected
in the parent process chain). The user must run this from a separate terminal.

After minting, invoke /promote inside Claude Code to consume the token.`,
	Run: func(cmd *cobra.Command, args []string) {
		mintGateToken(promoteGate, promoteTTLFlag, promoteDryRunFlag)
	},
}

var promoteStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check promote token presence and expiry",
	Long:  `Reports JSON status of the promote token: {present, age_seconds, ttl_remaining, tty, expires_at}.`,
	Run: func(cmd *cobra.Command, args []string) {
		gateStatus(promoteGate, promoteFieldFlag)
	},
}

var promoteRevokeCmd = &cobra.Command{
	Use:   "revoke",
	Short: "Delete the promote token (allowed inside Claude Code)",
	Long:  `Deletes ~/.claude/state/promote-token. Safe to run from any context including inside Claude Code.`,
	Run: func(cmd *cobra.Command, args []string) {
		gateRevoke(promoteGate, "Promote token revoked")
	},
}

// mintGateToken implements the shared `<gate> unlock` behavior: refuse inside
// Claude Code, honor --dry-run, otherwise mint and print the TTL/path/expiry.
func mintGateToken(g token.Gate, ttl int, dryRun bool) {
	mintGateTokenReason(g, ttl, dryRun, "")
}

// mintGateTokenReason is mintGateToken with an operator-supplied justification
// recorded in the token (the destructive gate's `--reason`).
func mintGateTokenReason(g token.Gate, ttl int, dryRun bool, reason string) {
	// Block execution inside Claude Code — invert isInsideClaudeEnv() pattern
	if isInsideClaudeEnv() {
		fmt.Fprintln(os.Stderr, g.RefuseMsg)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, g.UnlockHelp)
		os.Exit(1)
	}

	if ttl <= 0 {
		ttl = 5
	}

	if dryRun {
		fmt.Printf("dry-run: would mint token with TTL=%dm at %s\n", ttl, g.Path())
		return
	}

	tok, err := g.MintWithReason(ttl, currentTTY(), reason)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not mint token: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ %s. Valid for %dm. %s\n", g.SuccessMsg, ttl, unlockNextStep(g.Name))
	fmt.Printf("  Token path: %s\n", g.Path())
	fmt.Printf("  Expires at: %s\n", tok.ExpiresAt.Format(time.RFC3339))
}

// unlockNextStep returns the gate-specific "what to do next" hint printed after
// a successful mint. Preserves the original per-gate phrasing.
func unlockNextStep(name string) string {
	switch name {
	case "promote":
		return "Run /promote in Claude Code now."
	case "verify-suite":
		return "Run the full test suite in Claude Code now."
	case "review-stamp":
		return "Re-run `kaisser pr-review <PR> --write-stamp` in Claude Code now."
	case "destructive":
		return "Re-run the blocked destructive command in Claude Code now (one execution)."
	default:
		return "Token minted."
	}
}

// gateStatus implements the shared `<gate> status` behavior, printing either a
// single requested field or the full JSON status object.
func gateStatus(g token.Gate, field string) {
	tok := g.Read()
	now := time.Now()

	type statusOutput struct {
		Present      bool      `json:"present"`
		AgeSeconds   float64   `json:"age_seconds"`
		TTLRemaining float64   `json:"ttl_remaining"`
		TTY          string    `json:"tty"`
		ExpiresAt    time.Time `json:"expires_at"`
	}

	out := statusOutput{}

	if tok == nil {
		// Token absent
	} else if now.After(tok.ExpiresAt) {
		// Expired — auto-clean via the gate's single cleanup path. Present()
		// deletes the expired token as a side effect and returns false, so we
		// avoid a second os.Remove(g.Path()) cleanup code path here.
		_ = g.Present()
		// present stays false
	} else {
		out.Present = true
		out.AgeSeconds = now.Sub(tok.CreatedAt).Seconds()
		out.TTLRemaining = tok.ExpiresAt.Sub(now).Seconds()
		out.TTY = tok.TTY
		out.ExpiresAt = tok.ExpiresAt
	}

	if field != "" {
		switch field {
		case "present":
			if out.Present {
				fmt.Println("true")
			} else {
				fmt.Println("false")
			}
		case "age_seconds":
			fmt.Printf("%.0f\n", out.AgeSeconds)
		case "ttl_remaining":
			fmt.Printf("%.0f\n", out.TTLRemaining)
		case "tty":
			fmt.Println(out.TTY)
		case "expires_at":
			if out.Present {
				fmt.Println(out.ExpiresAt.Format(time.RFC3339))
			}
		default:
			fmt.Fprintf(os.Stderr, "unknown field %q; valid: present, age_seconds, ttl_remaining, tty, expires_at\n", field)
			os.Exit(1)
		}
		return
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}

// gateRevoke implements the shared `<gate> revoke` behavior. successMsg is the
// confirmation line printed when a token was actually removed.
func gateRevoke(g token.Gate, successMsg string) {
	if g.Path() == "" {
		fmt.Fprintln(os.Stderr, "error: could not determine token path")
		os.Exit(1)
	}
	if err := g.Revoke(); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("✓ No token present (nothing to revoke)")
			return
		}
		fmt.Fprintf(os.Stderr, "error: could not remove token: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ %s\n", successMsg)
}

// currentTTY returns the current TTY device name, or "" if unavailable.
func currentTTY() string {
	// Try /proc/self/fd/0 (Linux) or tty command (cross-platform)
	if tty, err := os.Readlink("/proc/self/fd/0"); err == nil {
		return tty
	}
	// Fallback: read from os.Stdin if it's a char device
	fi, err := os.Stdin.Stat()
	if err != nil {
		return ""
	}
	if fi.Mode()&os.ModeCharDevice != 0 {
		return fi.Name()
	}
	return ""
}

func init() {
	rootCmd.AddCommand(promoteCmd)
	promoteCmd.AddCommand(promoteUnlockCmd)
	promoteCmd.AddCommand(promoteStatusCmd)
	promoteCmd.AddCommand(promoteRevokeCmd)

	promoteUnlockCmd.Flags().IntVar(&promoteTTLFlag, "ttl", 5, "Token TTL in minutes (default: 5)")
	promoteUnlockCmd.Flags().BoolVar(&promoteDryRunFlag, "dry-run", false, "Preview token mint without writing")
	promoteStatusCmd.Flags().StringVar(&promoteFieldFlag, "field", "", "Print only this field (present, age_seconds, ttl_remaining, tty, expires_at)")
}
