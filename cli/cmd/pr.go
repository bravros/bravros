package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/bravros/bravros/internal/github"
	"github.com/spf13/cobra"
)

var latestOnly bool

var prReviewCmd = &cobra.Command{
	Use:   "pr-review [PR_NUMBER]",
	Short: "Fetch comprehensive PR review data",
	Run: func(cmd *cobra.Command, args []string) {
		prArg := ""
		if len(args) > 0 {
			if args[0] == "diff" {
				// Diff subcommand
				diffArg := ""
				if len(args) > 1 {
					diffArg = args[1]
				}
				prNum, err := github.GetPRNumber(diffArg)
				if err != nil {
					fmt.Fprintf(os.Stderr, "❌ %v\n", err)
					os.Exit(1)
				}
				diff, err := github.FetchDiff(prNum)
				if err != nil {
					fmt.Println("(no diff available)")
				} else {
					fmt.Println(diff)
				}
				return
			}
			prArg = args[0]
		}

		prNumber, err := github.GetPRNumber(prArg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		repo, err := github.GetRepo()
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		botLogin := os.Getenv("REVIEW_BOT_LOGIN")
		if botLogin == "" {
			botLogin = "claude[bot]"
		}

		sep := strings.Repeat("=", 60)
		fmt.Printf("\n%s\n", sep)
		fmt.Printf("PR #%s — %s\n", prNumber, repo)
		fmt.Printf("%s\n", sep)

		// 1. PR info
		info, err := github.FetchPRInfo(prNumber)
		if err == nil {
			author := ""
			if a, ok := info["author"].(map[string]interface{}); ok {
				author = fmt.Sprintf("%v", a["login"])
			}
			fmt.Printf("\n📌 %v\n", info["title"])
			fmt.Printf("   State:   %v\n", info["state"])
			fmt.Printf("   Branch:  %v → %v\n", info["headRefName"], info["baseRefName"])
			fmt.Printf("   Author:  %s\n", author)
			fmt.Printf("   URL:     %v\n", info["url"])

			// PR body
			body := fmt.Sprintf("%v", info["body"])
			if body != "" && body != "<nil>" {
				fmt.Println(github.Section("PR Description"))
				if len(body) > 800 {
					fmt.Println(body[:800] + "... [truncated]")
				} else {
					fmt.Println(body)
				}
			}
		}

		// 2. Changed files
		fmt.Println(github.Section("Changed Files"))
		files, err := github.FetchChangedFiles(prNumber)
		if err != nil || files == "" {
			fmt.Println("(no changed files)")
		} else {
			fmt.Println(files)
		}

		// 3. Formal reviews
		fmt.Println(github.Section("Formal Reviews"))
		reviews, err := github.FetchReviews(prNumber)
		if err != nil || reviews == "" {
			fmt.Println("(no formal reviews)")
		} else {
			fmt.Println(reviews)
		}

		// 4. Inline comments
		fmt.Println(github.Section("Inline Code Comments"))
		inline, err := github.FetchInlineComments(repo, prNumber)
		if err != nil || inline == "" {
			fmt.Println("(no inline comments)")
		} else {
			fmt.Println(inline)
		}

		// 5. Bot comments
		if latestOnly {
			fmt.Println(github.Section("Latest Bot Comment"))
			bot, err := github.FetchLatestBotComment(repo, prNumber, botLogin)
			if err != nil || bot == "" {
				fmt.Println("(no bot comments)")
			} else {
				fmt.Println(bot)
			}
		} else {
			fmt.Println(github.Section("Automated Bot Comments"))
			bot, err := github.FetchBotComments(repo, prNumber, botLogin)
			if err != nil || bot == "" {
				fmt.Println("(no bot comments)")
			} else {
				lines := strings.Split(bot, "\n")
				if len(lines) > 100 {
					fmt.Println(strings.Join(lines[:100], "\n"))
					fmt.Printf("\n... [%d more lines — see PR URL for full review]\n", len(lines)-100)
				} else {
					fmt.Println(bot)
				}
			}
		}

		// 6. Human comments
		fmt.Println(github.Section("Human Comments"))
		human, err := github.FetchHumanComments(repo, prNumber, botLogin)
		if err != nil || human == "" {
			fmt.Println("(no human comments)")
		} else {
			fmt.Println(human)
		}

		// 7. PR checks
		fmt.Println(github.Section("PR Checks"))
		checks, _ := github.FetchPRChecks(prNumber)
		fmt.Println(checks)

		fmt.Printf("\n%s\n", sep)
		fmt.Println("✅ All review data fetched.")
		fmt.Printf("%s\n\n", sep)
	},
}

func init() {
	prReviewCmd.Flags().BoolVar(&latestOnly, "latest", false, "Show only the most recent bot review comment")
}
