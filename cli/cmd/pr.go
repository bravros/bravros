package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/bravros/bravros/cli/internal/github"
	"github.com/spf13/cobra"
)

// prReviewCmd is the write half of the old `bravros pr-review` verb (P-0187),
// plus ONE restored read verb (this plan): --latest.
//
// The READ paths --terse, --field, --bot-only, --full, diff, and --wait stay
// retired: skills read review data directly via `gh api --paginate`. What
// survives is the Tier-1 sentinel parse + gated stamp write (`--write-stamp`),
// the human-presence token subcommands (`unlock`, `status`, `revoke` —
// registered in reviewstamp.go), and `--latest [--json]` — a minimal,
// READ-ONLY report of the latest bot review/comment for an operator who wants
// to see it on their own terminal without a stamp write or a `gh api` call.
//
// Standalone --write-stamp: fetch the latest bot review/comment, parse its
// verdict, and write the stamp when approved+confident. Exit non-error on all
// outcomes: approved (stamp written), changes-requested (no stamp, clear
// message), unclear (warning), and no review found (safe no-op) — so skill
// callers don't abort on transient GitHub errors (the stamp is an
// optimization).
var prReviewCmd = &cobra.Command{
	Use:   "pr-review [PR_NUMBER]",
	Short: "Write the PR review stamp from the latest bot verdict (--write-stamp), or read it (--latest)",
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

		if prReviewJSON && !prReviewLatest {
			fmt.Fprintln(os.Stderr, "❌ --json requires --latest.")
			os.Exit(1)
		}

		if prReviewLatest {
			os.Exit(runPRReviewLatest(prNumber, prReviewJSON))
		}

		if !prReviewWriteStamp {
			for _, line := range retiredReadPathsMessage() {
				fmt.Fprintln(os.Stderr, line)
			}
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

// retiredReadPathsMessage is printed when neither --latest nor --write-stamp
// is set. Extracted into its own function so the surviving-verbs list is
// unit-testable without surviving the os.Exit(1) that follows it in Run() —
// same pattern as TestReviewStampGate_MintRefusedInsideClaude in
// reviewstamp_test.go.
func retiredReadPathsMessage() []string {
	return []string{
		"❌ the pr-review read paths were retired (P-0187) — read review data with `gh api --paginate` instead.",
		"   Surviving verbs: pr-review <PR> --latest [--json] · pr-review --write-stamp · pr-review unlock|status|revoke",
	}
}

func init() {
	prReviewCmd.Flags().BoolVar(&prReviewWriteStamp, "write-stamp", false, "Standalone stamp write: fetches the latest bot verdict and writes the review stamp only on approved/no-new-blockers.")
	prReviewCmd.Flags().BoolVar(&prReviewLatest, "latest", false, "Read-only: fetch the latest bot review/comment and print its verdict. No stamp write, no token consumption, no side effects.")
	prReviewCmd.Flags().BoolVar(&prReviewJSON, "json", false, "With --latest, emit the result as a single JSON object instead of human-readable text.")
}
