package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/bravros/bravros/cli/internal/github"
	"github.com/spf13/cobra"
)

// prReviewCmd is the write half of the old `bravros pr-review` verb (P-0187).
//
// The READ paths (--latest, --terse, --field, --bot-only, --json, --full,
// diff, --wait) were retired: skills now read review data directly via
// `gh api --paginate`. What survives is the Tier-1 sentinel parse + gated
// stamp write (`--write-stamp`) and the human-presence token subcommands
// (`unlock`, `status`, `revoke`) registered in reviewstamp.go.
//
// Standalone --write-stamp: fetch the latest bot review/comment, parse its
// verdict, and write the stamp when approved+confident. Exit non-error on all
// outcomes: approved (stamp written), changes-requested (no stamp, clear
// message), unclear (warning), and no review found (safe no-op) — so skill
// callers don't abort on transient GitHub errors (the stamp is an
// optimization).
var prReviewCmd = &cobra.Command{
	Use:   "pr-review [PR_NUMBER]",
	Short: "Write the PR review stamp from the latest bot verdict (--write-stamp)",
	Run: func(cmd *cobra.Command, args []string) {
		prArg := ""
		if len(args) > 0 {
			prArg = args[0]
		}

		prNumber, err := github.GetPRNumber(prArg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		if !prReviewWriteStamp {
			fmt.Fprintln(os.Stderr, "❌ the pr-review read paths were retired (P-0187) — read review data with `gh api --paginate` instead.")
			fmt.Fprintln(os.Stderr, "   Surviving verbs: pr-review --write-stamp · pr-review unlock|status|revoke")
			os.Exit(1)
		}

		botLogin := os.Getenv("REVIEW_BOT_LOGIN")
		if botLogin == "" {
			botLogin = "claude[bot]"
		}

		candidate, found, fetchErr := fetchLatestBotReviewOrComment(prNumber, botLogin, time.Time{})
		if fetchErr != nil {
			fmt.Fprintf(os.Stderr, "⚠️  could not fetch bot review: %v\n", fetchErr)
			// Non-fatal: exit 0 so skill callers don't abort on transient errors.
			return
		}
		if !found {
			fmt.Fprintf(os.Stderr, "no bot review found for PR #%s — no stamp written\n", prNumber)
			return
		}
		os.Exit(writeStampFromVerdict(prNumber, candidate.Body))
	},
}

func init() {
	prReviewCmd.Flags().BoolVar(&prReviewWriteStamp, "write-stamp", false, "Standalone stamp write: fetches the latest bot verdict and writes the review stamp only on approved/no-new-blockers.")
}
