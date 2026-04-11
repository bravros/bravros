package plan

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	gitpkg "github.com/bravros/private/internal/git"
)

// CheckStatusResult holds the result of a plan-check-status query.
type CheckStatusResult struct {
	Checked   bool   `json:"checked"`
	Source    string `json:"source"` // "git_log", "file_marker", "both", "none"
	Commit    string `json:"commit,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	PlanFile  string `json:"plan_file"`
	PlanNum   string `json:"plan_num"`
}

// JSON returns the result as indented JSON.
func (r *CheckStatusResult) JSON() string {
	b, _ := json.MarshalIndent(r, "", "  ")
	return string(b)
}

// CheckPlanCheckStatus determines whether a plan-check has been performed.
// It searches git log for a plan-check commit and the plan file for a marker section.
// If planFile is empty, it auto-detects via .planning/*-todo.md.
func CheckPlanCheckStatus(planFile string) (*CheckStatusResult, error) {
	if planFile == "" {
		repo, err := gitpkg.Open("")
		if err != nil {
			return nil, err
		}
		branch := repo.CurrentBranch()
		planFile = FindPlanFile(".planning", branch)
	}

	result := &CheckStatusResult{
		PlanFile: planFile,
		Source:   "none",
	}

	if planFile == "" {
		return result, nil
	}

	// Extract plan number from filename
	base := filepath.Base(planFile)
	m := numPrefix.FindStringSubmatch(base)
	if m != nil {
		result.PlanNum = m[1]
	}

	if result.PlanNum == "" {
		return result, nil
	}

	foundInGit := false
	foundInFile := false

	// Search git log for plan-check commit
	commit, ts := searchGitLogForPlanCheck(result.PlanNum)
	if commit != "" {
		foundInGit = true
		result.Commit = commit
		result.Timestamp = ts
	}

	// Search plan file for marker section
	if hasPlanCheckMarker(planFile) {
		foundInFile = true
	}

	// Determine source and checked status
	switch {
	case foundInGit && foundInFile:
		result.Source = "both"
		result.Checked = true
	case foundInGit:
		result.Source = "git_log"
		result.Checked = true
	case foundInFile:
		result.Source = "file_marker"
		result.Checked = true
	default:
		result.Source = "none"
		result.Checked = false
	}

	return result, nil
}

// searchGitLogForPlanCheck searches git log for a plan-check commit matching the plan number.
// Returns (commit_hash, timestamp) or ("", "") if not found.
func searchGitLogForPlanCheck(planNum string) (string, string) {
	// Use git log with format to get hash and date
	out, _, err := gitpkg.Run("git", "log", "--oneline", "--format=%H %aI %s")
	if err != nil || out == "" {
		return "", ""
	}

	// Case-insensitive pattern: "plan check NNNN" or "plan-check NNNN" or "plan.check.*NNNN"
	pattern := regexp.MustCompile(`(?i)plan[\s._-]*check[\s._-]*` + regexp.QuoteMeta(planNum))

	for _, line := range strings.Split(out, "\n") {
		if pattern.MatchString(line) {
			parts := strings.SplitN(line, " ", 3)
			if len(parts) >= 2 {
				hash := parts[0][:minInt(7, len(parts[0]))] // short hash
				return hash, parts[1]
			}
		}
	}

	return "", ""
}

// hasPlanCheckMarker checks whether the plan file contains a plan-check section marker.
func hasPlanCheckMarker(planFile string) bool {
	f, err := os.Open(planFile)
	if err != nil {
		return false
	}
	defer f.Close()

	markers := []string{
		"## plan check",
		"plan vs implementation",
		"audited",
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lower := strings.ToLower(scanner.Text())
		for _, marker := range markers {
			if strings.Contains(lower, marker) {
				return true
			}
		}
	}

	return false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
