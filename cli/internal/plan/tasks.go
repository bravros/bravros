package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bravros/bravros/internal/git"
)

// TaskLine represents a single task line from a plan file.
type TaskLine struct {
	Text    string `json:"text"`
	Checked bool   `json:"checked"`
	Phase   string `json:"phase"`
	Marker  string `json:"marker"`
	Line    int    `json:"line"`
}

// TaskDiffResult holds the comparison between baseline and current tasks.
type TaskDiffResult struct {
	Baseline      []TaskLine `json:"baseline"`
	Current       []TaskLine `json:"current"`
	Deleted       []TaskLine `json:"deleted"`
	Added         []TaskLine `json:"added"`
	Modified      []TaskLine `json:"modified"`
	DeletedCount  int        `json:"deleted_count"`
	AddedCount    int        `json:"added_count"`
	ModifiedCount int        `json:"modified_count"`
}

var (
	taskPhaseRe  = regexp.MustCompile(`^### Phase \d+:\s*(.+)`)
	taskLineRe   = regexp.MustCompile(`(?i)^- \[[ x]\]\s+`)
	taskCheckRe  = regexp.MustCompile(`(?i)^- \[x\]`)
	taskMarkerRe = regexp.MustCompile(`\[([HSO])\]`)
)

// normalizeTaskText strips checkbox, marker, and leading/trailing whitespace
// to produce a comparable key for diffing.
func normalizeTaskText(text string) string {
	return strings.TrimSpace(text)
}

// ParseTasks extracts task lines from plan file content.
func ParseTasks(content string) []TaskLine {
	var tasks []TaskLine
	currentPhase := ""
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		// Track phase headers
		if m := taskPhaseRe.FindStringSubmatch(line); m != nil {
			currentPhase = strings.TrimSpace(m[0])
			continue
		}

		// Match task lines
		if !taskLineRe.MatchString(line) {
			continue
		}

		checked := taskCheckRe.MatchString(line)

		// Extract text after checkbox
		// Remove the "- [x] " or "- [ ] " prefix
		text := regexp.MustCompile(`(?i)^- \[[ x]\]\s*`).ReplaceAllString(line, "")

		// Extract marker
		marker := ""
		if m := taskMarkerRe.FindStringSubmatch(text); m != nil {
			marker = m[1]
		}

		tasks = append(tasks, TaskLine{
			Text:    text,
			Checked: checked,
			Phase:   currentPhase,
			Marker:  marker,
			Line:    i + 1, // 1-based
		})
	}

	return tasks
}

// computeDiff compares baseline and current task lists.
func computeDiff(baseline, current []TaskLine) *TaskDiffResult {
	result := &TaskDiffResult{
		Baseline: baseline,
		Current:  current,
	}

	// Build maps keyed by normalized text.
	// Note: duplicate task text will silently collapse — last wins.
	// This is acceptable given plan naming conventions (tasks are unique).
	baseMap := make(map[string]TaskLine, len(baseline))
	for _, t := range baseline {
		baseMap[normalizeTaskText(t.Text)] = t
	}

	curMap := make(map[string]TaskLine, len(current))
	for _, t := range current {
		curMap[normalizeTaskText(t.Text)] = t
	}

	// Deleted: in baseline but not current
	for key, t := range baseMap {
		if _, ok := curMap[key]; !ok {
			result.Deleted = append(result.Deleted, t)
		}
	}

	// Added: in current but not baseline
	for key, t := range curMap {
		if _, ok := baseMap[key]; !ok {
			result.Added = append(result.Added, t)
		}
	}

	// Modified: same text but different checked status
	for key, curTask := range curMap {
		if baseTask, ok := baseMap[key]; ok {
			if baseTask.Checked != curTask.Checked {
				result.Modified = append(result.Modified, curTask)
			}
		}
	}

	result.DeletedCount = len(result.Deleted)
	result.AddedCount = len(result.Added)
	result.ModifiedCount = len(result.Modified)

	return result
}

// FindAutoBaseline finds the most recent "plan: review" commit hash.
func FindAutoBaseline() (string, error) {
	out, _, err := git.Run("git", "log", "--oneline", "--all", "-n", "200")
	if err != nil {
		return "", fmt.Errorf("git log failed: %w", err)
	}

	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(strings.ToLower(line), "plan: review") ||
			strings.Contains(strings.ToLower(line), "📋 plan: review") {
			parts := strings.SplitN(strings.TrimSpace(line), " ", 2)
			if len(parts) >= 1 && parts[0] != "" {
				return parts[0], nil
			}
		}
	}

	return "", fmt.Errorf("no 'plan: review' commit found in recent history")
}

// TaskDiff compares tasks between the current plan file and a baseline commit.
func TaskDiff(planFile, baselineCommit string) (*TaskDiffResult, error) {
	if planFile == "" {
		matches, _ := filepath.Glob(".planning/*-todo.md")
		if len(matches) == 0 {
			return nil, fmt.Errorf("no plan file found")
		}
		planFile = matches[0]
	}

	// Resolve auto baseline
	commit := baselineCommit
	if commit == "auto" {
		var err error
		commit, err = FindAutoBaseline()
		if err != nil {
			return nil, err
		}
	}

	// Get baseline content from git
	baselineContent, stderr, err := git.Run("git", "show", commit+":"+planFile)
	if err != nil {
		return nil, fmt.Errorf("git show %s:%s failed: %s", commit, planFile, stderr)
	}

	// Read current file directly (no subprocess needed)
	currentBytes, err := os.ReadFile(planFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", planFile, err)
	}
	currentContent := string(currentBytes)

	baselineTasks := ParseTasks(baselineContent)
	currentTasks := ParseTasks(currentContent)

	return computeDiff(baselineTasks, currentTasks), nil
}
