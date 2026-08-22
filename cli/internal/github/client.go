package github

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// Run executes a command and returns (stdout, stderr, error).
func Run(args ...string) (string, string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

// GetPRNumber detects the PR number from args or current branch.
func GetPRNumber(arg string) (string, error) {
	if arg != "" {
		return arg, nil
	}
	out, _, err := Run("gh", "pr", "view", "--json", "number", "-q", ".number")
	if err != nil {
		return "", fmt.Errorf("could not detect PR number: not on a PR branch")
	}
	return strings.TrimSpace(out), nil
}

// GetRepo returns the owner/repo string.
func GetRepo() (string, error) {
	out, _, err := Run("gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
	if err != nil {
		return "", fmt.Errorf("could not get repo")
	}
	return strings.TrimSpace(out), nil
}

// Section prints a formatted section header.
func Section(title string) string {
	padding := 54 - len(title)
	if padding < 0 {
		padding = 0
	}
	return fmt.Sprintf("\n── %s %s", title, strings.Repeat("─", padding))
}

// FetchPRInfo fetches PR metadata.
func FetchPRInfo(prNumber string) (map[string]interface{}, error) {
	out, _, err := Run("gh", "pr", "view", prNumber,
		"--json", "title,state,author,headRefName,baseRefName,url,mergeable,body")
	if err != nil {
		return nil, err
	}
	var info map[string]interface{}
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return nil, err
	}
	return info, nil
}

// FetchChangedFiles fetches files changed in the PR.
func FetchChangedFiles(prNumber string) (string, error) {
	out, _, err := Run("gh", "pr", "diff", prNumber, "--name-only")
	if err != nil {
		return "", err
	}
	return out, nil
}

// FetchReviews fetches formal reviews.
func FetchReviews(prNumber string) (string, error) {
	out, _, err := Run("gh", "pr", "view", prNumber,
		"--json", "reviews",
		"--jq", `.reviews[] | "[\(.state)] \(.author.login): \(.body[:300])"`)
	return out, err
}

// FetchInlineComments fetches inline code comments.
func FetchInlineComments(repo, prNumber string) (string, error) {
	out, _, err := Run("gh", "api",
		fmt.Sprintf("repos/%s/pulls/%s/comments", repo, prNumber),
		"--jq", `.[] | "[\(.user.login)] \(.path):\(.line // .original_line // "?") — \(.body)\n  context: \(.diff_hunk | split("\n") | last)"`)
	return out, err
}

// botLoginPredicate returns the jq boolean expression selecting a comment
// authored by botLogin, by any GitHub App ("...[bot]") login, or — when
// botLogin itself ends with "[bot]" — the BARE Action login (e.g.
// "claude[bot]" also matches "claude"). This mirrors cmd.isBotOrAction's
// widening: the @claude GitHub Action posts issue comments as user login
// "claude", not "claude[bot]", so the default --bot flag value would match
// nothing in these queries without it (B-0023).
func botLoginPredicate(botLogin string) string {
	pred := fmt.Sprintf(`.user.login == %q or (.user.login | endswith("[bot]"))`, botLogin)
	if strings.HasSuffix(botLogin, "[bot]") {
		bare := strings.TrimSuffix(botLogin, "[bot]")
		pred += fmt.Sprintf(` or .user.login == %q`, bare)
	}
	return pred
}

// BotCommentJSON is the structured shape for --json output from pr-review.
type BotCommentJSON struct {
	Author   string `json:"author"`
	Body     string `json:"body"`
	PostedAt string `json:"posted_at"`
	URL      string `json:"url"`
	State    string `json:"state"`
}

// fetchBotCommentsPaginated runs `gh api --paginate` (no --slurp) against the
// PR's issue-comments endpoint, applies the bot-selection jq filter, and
// collects the result across every page in Go.
//
// gh 2.97+ rejects `--paginate --slurp` combined with `--jq` outright ("the
// --slurp option is not supported with --jq or --template"), so the previous
// --slurp-based approach (flatten pages, then sort/pick/join across ALL pages
// inside one jq expression) no longer runs. Without --slurp, --paginate still
// applies the --jq filter to EACH page and concatenates every page's filtered
// output — per-comment selection is unaffected (it only ever looked at one
// comment at a time), but any cross-page aggregation (latest-by-posted_at,
// join-in-fetch-order) has to happen after Go has the full stream, which is
// what this helper's callers do.
func fetchBotCommentsPaginated(repo, prNumber, botLogin string) ([]BotCommentJSON, error) {
	jq := fmt.Sprintf(`.[] | select(%s) | {author: .user.login, body: .body, posted_at: .created_at, url: .html_url}`, botLoginPredicate(botLogin))
	out, _, err := Run("gh", "api", "--paginate",
		fmt.Sprintf("repos/%s/issues/%s/comments", repo, prNumber),
		"--jq", jq)
	if err != nil {
		return nil, err
	}
	return parseBotCommentStream(strings.NewReader(out))
}

// parseBotCommentStream decodes a stream of concatenated JSON objects — one
// per matched comment, possibly spanning several `gh --paginate` pages with
// no guaranteed separator between them — into a slice of BotCommentJSON.
// Uses json.Decoder rather than a line-split so it tolerates however `gh`
// chooses to whitespace-separate consecutive pages' output. Factored out of
// fetchBotCommentsPaginated so the stream-parsing/aggregation logic is
// testable without shelling out to `gh`.
func parseBotCommentStream(r io.Reader) ([]BotCommentJSON, error) {
	comments := make([]BotCommentJSON, 0)
	dec := json.NewDecoder(r)
	for dec.More() {
		var c struct {
			Author   string `json:"author"`
			Body     string `json:"body"`
			PostedAt string `json:"posted_at"`
			URL      string `json:"url"`
		}
		if err := dec.Decode(&c); err != nil {
			return nil, fmt.Errorf("failed to parse bot comment JSON: %w", err)
		}
		comments = append(comments, BotCommentJSON{
			Author:   c.Author,
			Body:     c.Body,
			PostedAt: c.PostedAt,
			URL:      c.URL,
			State:    "posted",
		})
	}
	return comments, nil
}

// latestBotComment returns the comment with the maximum PostedAt across
// comments, or nil when comments is empty. Timestamps are parsed as RFC3339
// when possible; a comment whose timestamp fails to parse falls back to a
// lexicographic comparison, which stays correct for well-formed ISO-8601 UTC
// timestamps like GitHub's "2026-08-21T10:00:00Z".
func latestBotComment(comments []BotCommentJSON) *BotCommentJSON {
	if len(comments) == 0 {
		return nil
	}
	best := comments[0]
	for _, c := range comments[1:] {
		if botCommentAfter(c, best) {
			best = c
		}
	}
	return &best
}

// botCommentAfter reports whether a's PostedAt is later than b's.
func botCommentAfter(a, b BotCommentJSON) bool {
	at, aErr := time.Parse(time.RFC3339, a.PostedAt)
	bt, bErr := time.Parse(time.RFC3339, b.PostedAt)
	if aErr == nil && bErr == nil {
		return at.After(bt)
	}
	return a.PostedAt > b.PostedAt
}

// FetchBotComments fetches bot comments on the PR, joined in the order gh
// returned them across ALL pages (see fetchBotCommentsPaginated).
func FetchBotComments(repo, prNumber, botLogin string) (string, error) {
	comments, err := fetchBotCommentsPaginated(repo, prNumber, botLogin)
	if err != nil {
		return "", err
	}
	lines := make([]string, len(comments))
	for i, c := range comments {
		lines[i] = "[" + c.Author + "] " + c.Body
	}
	return strings.Join(lines, "\n"), nil
}

// FetchLatestBotComment fetches only the most recent bot comment on the PR.
// "Latest" is computed in Go (see latestBotComment) across ALL pages, not
// just the first.
func FetchLatestBotComment(repo, prNumber, botLogin string) (string, error) {
	comments, err := fetchBotCommentsPaginated(repo, prNumber, botLogin)
	if err != nil {
		return "", err
	}
	latest := latestBotComment(comments)
	if latest == nil {
		return "", nil
	}
	return "[" + latest.Author + "] " + latest.Body, nil
}

// FetchLatestBotCommentJSON fetches the most recent bot comment and returns it
// as a structured BotCommentJSON. Used by bravros pr-review --json --latest.
// "Latest" is computed in Go (see latestBotComment) across ALL pages.
func FetchLatestBotCommentJSON(repo, prNumber, botLogin string) (*BotCommentJSON, error) {
	comments, err := fetchBotCommentsPaginated(repo, prNumber, botLogin)
	if err != nil {
		return nil, err
	}
	latest := latestBotComment(comments)
	if latest == nil {
		return nil, fmt.Errorf("no bot comment found")
	}
	return latest, nil
}

// FetchBotCommentsJSON fetches all bot comments and returns them as a slice of
// BotCommentJSON, across ALL pages (see fetchBotCommentsPaginated).
func FetchBotCommentsJSON(repo, prNumber, botLogin string) ([]BotCommentJSON, error) {
	return fetchBotCommentsPaginated(repo, prNumber, botLogin)
}

// FetchHumanComments fetches non-bot comments.
func FetchHumanComments(repo, prNumber, botLogin string) (string, error) {
	jq := fmt.Sprintf(`.[] | select(.user.login != "%s" and (.user.login | endswith("[bot]") | not)) | "[" + .user.login + "] " + .body`, botLogin)
	out, _, err := Run("gh", "api",
		fmt.Sprintf("repos/%s/issues/%s/comments", repo, prNumber),
		"--jq", jq)
	return out, err
}

// FetchPRChecks fetches PR checks status.
func FetchPRChecks(prNumber string) (string, error) {
	out, errOut, err := Run("gh", "pr", "checks", prNumber)
	if out != "" {
		return out, nil
	}
	if errOut != "" {
		return errOut, nil
	}
	return "(no checks)", err
}

// FetchDiff fetches the full PR diff.
func FetchDiff(prNumber string) (string, error) {
	out, _, err := Run("gh", "pr", "diff", prNumber)
	return out, err
}
