package ci

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// CICheckResult holds the 3-tier CI detection result.
type CICheckResult struct {
	HasCI    bool   `json:"has_ci"`
	Workflow string `json:"workflow"`
	LastRun  string `json:"last_run"`
	Relevant bool   `json:"relevant"`
}

// JSON returns the result as a JSON string.
func (r *CICheckResult) JSON() string {
	b, _ := json.MarshalIndent(r, "", "  ")
	return string(b)
}

// runCommandFunc is the function used to execute commands. Tests replace this.
var runCommand = defaultRunCommand

func defaultRunCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%s: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// ghRunEntry represents a single workflow run from gh run list.
type ghRunEntry struct {
	DatabaseID int    `json:"databaseId"`
	CreatedAt  string `json:"createdAt"`
	Status     string `json:"status"`
}

// Check performs 3-tier CI detection for the given workflow and branch.
func Check(workflow, branch string) (*CICheckResult, error) {
	result := &CICheckResult{
		Workflow: workflow,
	}

	// Get owner/repo
	nameWithOwner, err := runCommand("gh", "repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner")
	if err != nil {
		return result, nil // not in a gh repo — no CI
	}

	// Tier 1: Check if workflow file exists
	apiPath := fmt.Sprintf("repos/%s/contents/.github/workflows/%s", nameWithOwner, workflow)
	_, err = runCommand("gh", "api", apiPath, "--jq", ".name")
	if err != nil {
		// Workflow file doesn't exist
		return result, nil
	}
	result.HasCI = true

	// Tier 2: Check recent runs
	runsJSON, err := runCommand("gh", "run", "list",
		"--workflow", workflow,
		"--limit", "5",
		"--json", "databaseId,createdAt,status")
	if err != nil || runsJSON == "" || runsJSON == "[]" {
		// Has file but no runs
		return result, nil
	}

	var runs []ghRunEntry
	if err := json.Unmarshal([]byte(runsJSON), &runs); err != nil {
		return result, nil
	}
	if len(runs) == 0 {
		return result, nil
	}

	result.LastRun = runs[0].CreatedAt

	// Tier 3: Check branch relevance
	branchRunsJSON, err := runCommand("gh", "run", "list",
		"--workflow", workflow,
		"--branch", branch,
		"--limit", "5",
		"--json", "databaseId")
	if err != nil || branchRunsJSON == "" || branchRunsJSON == "[]" {
		return result, nil
	}

	var branchRuns []ghRunEntry
	if err := json.Unmarshal([]byte(branchRunsJSON), &branchRuns); err != nil {
		return result, nil
	}
	if len(branchRuns) > 0 {
		result.Relevant = true
	}

	return result, nil
}
