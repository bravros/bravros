// Package cmd — reviewstamp.go
//
// The THIRD out-of-band human-presence gate (after promote and verify-suite): the
// review-stamp token at ~/.claude/state/review-stamp-token.
//
// WHY IT EXISTS. `kaisser pr-review --write-stamp` writes the review stamp that audit
// Rules 28/31 accept as authorization for an autonomous merge. It decides from the bot's
// review body, in two tiers (see prreview.go):
//
//	TIER 1 — the bot emitted "<!-- kaisser-verdict: approved -->". An explicit, structured
//	         assertion. Sound. Self-authorizing. Unchanged.
//	TIER 2 — no marker; we INFERRED "approved" from free-form English. Six adversarial
//	         panels proved this inference cannot be made sound in the approval direction,
//	         so a tier-2 approval no longer authorizes anything by itself.
//
// That leaves a real gap: reviews that predate the marker, and repos not yet posting their
// review request through our skills, produce genuine approvals we can no longer stamp. The
// escape hatch is a HUMAN, not a better classifier — the operator reads the review, agrees,
// and mints this token from a terminal OUTSIDE Claude Code. The stamp is then written on
// THEIR authority, and the token is consumed on the spot.
//
// CONSUMPTION SEMANTICS — this gate follows verify-suite, NOT promote, and the difference
// is deliberate:
//
//   - promote's token is revoked by the /promote SKILL (Step 5) after a successful merge.
//     Consumption is prose-driven: it depends on an LLM reaching a later step of a
//     markdown file. That is fine for /promote (whose merge is the last step anyway) and
//     wrong here — this token's whole job is to defend against trusting an LLM's reading.
//   - verify-suite's token is consumed IN CODE, before the allow returns (audit Rule 47:
//     "no allow-without-consume path"). The decision and the consumption are the same
//     instruction.
//
// The review-stamp decision is likewise made in Go (writeStampFromVerdict), so
// consumption belongs there too: consumeReviewStampToken() deletes the token BEFORE the
// stamp is written. One token authorizes exactly one stamp — a leftover token cannot
// silently authorize a second, unreviewed PR later in the same autonomous batch.
package cmd

import (
	"os"

	"github.com/bravros/bravros/cli/internal/token"
	"github.com/spf13/cobra"
)

// reviewStampGate is the out-of-band human-presence token gate backing
// `kaisser pr-review --write-stamp` on the tier-2 (prose) path. The token lives at
// ~/.claude/state/review-stamp-token. Path and JSON shape are the same stable contract
// the promote and verify-suite tokens use (internal/token.Gate).
var reviewStampGate = token.Gate{
	Name:       "review-stamp",
	SuccessMsg: "Review-stamp token minted",
	RefuseMsg:  "✋ kaisser pr-review unlock REFUSED: running inside Claude Code.",
	UnlockHelp: `The review-stamp token must be minted from a separate terminal outside
Claude Code. This is intentional — it proves a HUMAN read the review and
agreed with it. A Claude session cannot authorize its own merge.

  1. Open a new terminal (outside Claude Code, same machine)
  2. Run: kaisser pr-review unlock
  3. Return to Claude Code and re-run: kaisser pr-review <PR> --write-stamp

The token is single-use (5-min TTL) and is consumed by the stamp it authorizes.

Suggested alias: alias stamp-unlock='kaisser pr-review unlock'`,
}

// consumeReviewStampToken reads, validates, and — when valid — CONSUMES (deletes) the
// review-stamp token. Returns true only when a non-expired token existed AND was
// successfully deleted.
//
// CRITICAL: the token is deleted BEFORE this returns true. There is no
// authorize-without-consume path (the invariant Rule 47 holds for verify-suite). A
// consume that fails DENIES — a filesystem race that left the token on disk would
// otherwise let it authorize a second, unreviewed stamp.
//
// Mirrors audit.verifySuiteTokenPresent. Absent and malformed tokens both read as nil;
// a malformed file is cleaned up on the deny path.
func consumeReviewStampToken() bool {
	path := reviewStampGate.Path()
	if path == "" {
		return false
	}
	tok := reviewStampGate.Read()
	if tok == nil {
		// Absent or malformed. Clean up a malformed file (no-op if absent).
		os.Remove(path) //nolint:errcheck
		return false
	}
	if tok.Expired() {
		os.Remove(path) //nolint:errcheck
		return false
	}
	// Valid: consume BEFORE authorizing. Single-use enforced.
	if err := reviewStampGate.Revoke(); err != nil {
		return false
	}
	return true
}

var reviewStampTTLFlag int
var reviewStampDryRunFlag bool
var reviewStampFieldFlag string

var prReviewUnlockCmd = &cobra.Command{
	Use:   "unlock",
	Short: "Mint a review-stamp token (must be run OUTSIDE Claude Code)",
	Long: `Writes a time-limited review-stamp token to ~/.claude/state/review-stamp-token.

The token lets ` + "`kaisser pr-review --write-stamp`" + ` write the merge-gate stamp for a bot
review that APPEARS to approve but carries no <!-- kaisser-verdict: approved --> marker.
Without it, such a review is reported but never stamped: a prose guess cannot authorize
an autonomous merge.

REFUSED when running inside a Claude Code session (CLAUDE_CODE_SESSION_ID canonical
Claude Code v2.1.132+, CLAUDE_SESSION_ID legacy fallback, or CLAUDE_CODE_ENTRYPOINT env
vars are set, or a "claude" process is detected in the parent process chain). That
refusal is the entire point of the gate: the session that wants the merge must not be
able to authorize it. Mint from a separate terminal, after you have READ the review.

Single-use: the token is consumed by the first stamp it authorizes.`,
	Run: func(cmd *cobra.Command, args []string) {
		mintGateToken(reviewStampGate, reviewStampTTLFlag, reviewStampDryRunFlag)
	},
}

var prReviewStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check review-stamp token presence and expiry",
	Long:  `Reports JSON status of the review-stamp token: {present, age_seconds, ttl_remaining, tty, expires_at}. Use --field present for a bare true/false.`,
	Run: func(cmd *cobra.Command, args []string) {
		gateStatus(reviewStampGate, reviewStampFieldFlag)
	},
}

var prReviewRevokeCmd = &cobra.Command{
	Use:   "revoke",
	Short: "Delete the review-stamp token (allowed inside Claude Code)",
	Long:  `Deletes ~/.claude/state/review-stamp-token. Safe to run from any context including inside Claude Code — revoking only ever removes authority.`,
	Run: func(cmd *cobra.Command, args []string) {
		gateRevoke(reviewStampGate, "Review-stamp token revoked")
	},
}

func init() {
	// Subcommands of the existing `kaisser pr-review [PR_NUMBER]` command. Cobra routes
	// `pr-review unlock|status|revoke` to these; any other first arg (a PR number, or
	// "diff") still falls through to prReviewCmd's own Run.
	prReviewCmd.AddCommand(prReviewUnlockCmd)
	prReviewCmd.AddCommand(prReviewStatusCmd)
	prReviewCmd.AddCommand(prReviewRevokeCmd)

	prReviewUnlockCmd.Flags().IntVar(&reviewStampTTLFlag, "ttl", 5, "Token TTL in minutes (default: 5)")
	prReviewUnlockCmd.Flags().BoolVar(&reviewStampDryRunFlag, "dry-run", false, "Preview token mint without writing")
	prReviewStatusCmd.Flags().StringVar(&reviewStampFieldFlag, "field", "", "Print only this field (present, age_seconds, ttl_remaining, tty, expires_at)")
}
