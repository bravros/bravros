package git

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

// CommitsSince returns oneline log of commits since base..HEAD.
func CommitsSince(base string) (string, error) {
	out, _, err := Run("git", "log", base+"...HEAD", "--oneline")
	return out, err
}

// ChangedFiles returns files changed between base..HEAD.
func ChangedFiles(base string) ([]string, error) {
	out, _, err := Run("git", "diff", base+"...HEAD", "--name-only")
	if err != nil || out == "" {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}

// DiffStat returns diff stat between base..HEAD.
func DiffStat(base string) (string, error) {
	out, _, err := Run("git", "diff", base+"...HEAD", "--stat")
	return out, err
}

// FileDiff returns diff for a specific file between base..HEAD.
func FileDiff(base, file string) (string, error) {
	out, _, err := Run("git", "diff", base+"...HEAD", "--", file)
	return out, err
}

// PRInfo fetches PR info for the current branch via gh CLI.
func PRInfo() (map[string]interface{}, error) {
	out, _, err := Run("gh", "pr", "view", "--json", "number,url,title,state")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, fmt.Errorf("no PR found")
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return nil, err
	}
	return result, nil
}

// Section prints a formatted section header.
func Section(title string) string {
	padding := 54 - len(title)
	if padding < 0 {
		padding = 0
	}
	return fmt.Sprintf("\n── %s %s", title, strings.Repeat("─", padding))
}
