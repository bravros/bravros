package github

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
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

// FetchBotComments fetches bot comments on the PR.
func FetchBotComments(repo, prNumber, botLogin string) (string, error) {
	jq := fmt.Sprintf(`.[] | select(.user.login == "%s" or (.user.login | endswith("[bot]"))) | "[" + .user.login + "] " + .body`, botLogin)
	out, _, err := Run("gh", "api",
		fmt.Sprintf("repos/%s/issues/%s/comments", repo, prNumber),
		"--jq", jq)
	return out, err
}

// FetchLatestBotComment fetches only the most recent bot comment on the PR.
func FetchLatestBotComment(repo, prNumber, botLogin string) (string, error) {
	jq := fmt.Sprintf(`[.[] | select(.user.login == "%s" or (.user.login | endswith("[bot]")))] | sort_by(.created_at) | last | "[" + .user.login + "] " + .body`, botLogin)
	out, _, err := Run("gh", "api",
		fmt.Sprintf("repos/%s/issues/%s/comments", repo, prNumber),
		"--jq", jq)
	return out, err
}

// BotCommentJSON is the structured shape for --json output from pr-review.
type BotCommentJSON struct {
	Author   string `json:"author"`
	Body     string `json:"body"`
	PostedAt string `json:"posted_at"`
	URL      string `json:"url"`
	State    string `json:"state"`
}

// FetchLatestBotCommentJSON fetches the most recent bot comment and returns it
// as a structured BotCommentJSON. Used by bravros pr-review --json --latest.
func FetchLatestBotCommentJSON(repo, prNumber, botLogin string) (*BotCommentJSON, error) {
	jq := fmt.Sprintf(`[.[] | select(.user.login == "%s" or (.user.login | endswith("[bot]")))] | sort_by(.created_at) | last | {author: .user.login, body: .body, posted_at: .created_at, url: .html_url}`, botLogin)
	out, _, err := Run("gh", "api",
		fmt.Sprintf("repos/%s/issues/%s/comments", repo, prNumber),
		"--jq", jq)
	if err != nil || strings.TrimSpace(out) == "" || strings.TrimSpace(out) == "null" {
		return nil, fmt.Errorf("no bot comment found")
	}
	var comment struct {
		Author   string `json:"author"`
		Body     string `json:"body"`
		PostedAt string `json:"posted_at"`
		URL      string `json:"url"`
	}
	if err := json.Unmarshal([]byte(out), &comment); err != nil {
		return nil, fmt.Errorf("failed to parse bot comment JSON: %w", err)
	}
	return &BotCommentJSON{
		Author:   comment.Author,
		Body:     comment.Body,
		PostedAt: comment.PostedAt,
		URL:      comment.URL,
		State:    "posted",
	}, nil
}

// FetchBotCommentsJSON fetches all bot comments and returns them as a slice of BotCommentJSON.
func FetchBotCommentsJSON(repo, prNumber, botLogin string) ([]BotCommentJSON, error) {
	jq := fmt.Sprintf(`[.[] | select(.user.login == "%s" or (.user.login | endswith("[bot]"))) | {author: .user.login, body: .body, posted_at: .created_at, url: .html_url}]`, botLogin)
	out, _, err := Run("gh", "api",
		fmt.Sprintf("repos/%s/issues/%s/comments", repo, prNumber),
		"--jq", jq)
	if err != nil {
		return nil, err
	}
	var comments []struct {
		Author   string `json:"author"`
		Body     string `json:"body"`
		PostedAt string `json:"posted_at"`
		URL      string `json:"url"`
	}
	if err := json.Unmarshal([]byte(out), &comments); err != nil {
		return nil, fmt.Errorf("failed to parse bot comments JSON: %w", err)
	}
	result := make([]BotCommentJSON, len(comments))
	for i, c := range comments {
		result[i] = BotCommentJSON{
			Author:   c.Author,
			Body:     c.Body,
			PostedAt: c.PostedAt,
			URL:      c.URL,
			State:    "posted",
		}
	}
	return result, nil
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
