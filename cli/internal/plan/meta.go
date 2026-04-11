package plan

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/bravros/bravros/internal/config"
)

// GetNextNumAtomic scans dir for the highest numbered file, picks next number,
// and atomically creates a placeholder using O_CREATE|O_EXCL to prevent races.
// Returns: the number string, a cleanup function to remove the placeholder, and any error.
// Retries up to 3 times if another process wins the race.
func GetNextNumAtomic(dir string) (string, func(), error) {
	noop := func() {}

	for attempt := 0; attempt < 3; attempt++ {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return "", noop, fmt.Errorf("cannot read directory %s: %w", dir, err)
		}

		maxNum := 0
		for _, e := range entries {
			m := numPrefix.FindStringSubmatch(e.Name())
			if m != nil {
				n, _ := strconv.Atoi(m[1])
				if n > maxNum {
					maxNum = n
				}
			}
		}

		nextNum := fmt.Sprintf("%04d", maxNum+1)
		placeholder := filepath.Join(dir, nextNum+"-.placeholder")

		f, err := os.OpenFile(placeholder, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			// Another process won the race — retry
			continue
		}
		f.Close()

		cleanup := func() {
			os.Remove(placeholder)
		}
		return nextNum, cleanup, nil
	}

	return "", noop, fmt.Errorf("failed to acquire atomic ID after 3 attempts in %s", dir)
}

var numPrefix = regexp.MustCompile(`^(\d{4})`)
var branchTypePrefix = regexp.MustCompile(`^[^/]+/`)

// MetaResult is the JSON output of the meta command.
type MetaResult struct {
	NextNum     string                        `json:"next_num"`
	BacklogNext string                        `json:"backlog_next,omitempty"`
	BaseBranch  string                        `json:"base_branch"`
	Branch      string                        `json:"branch"`
	PlanFile    string                        `json:"plan_file"`
	PlanNum     string                        `json:"plan_num"`
	Status      string                        `json:"status"`
	Progress    string                        `json:"progress"`
	Project     string                        `json:"project"`
	GitRemote   string                        `json:"git_remote"`
	Today       string                        `json:"today"`
	Stack       config.StackConfig            `json:"stack,omitempty"`
	Stacks      map[string]config.StackConfig `json:"stacks,omitempty"`
	Git         config.GitConfig              `json:"git,omitempty"`
	Monorepo    bool                          `json:"monorepo,omitempty"`
}

// GetNextNum scans .planning/ for the highest numbered plan file and returns next.
func GetNextNum(planningDir string) string {
	entries, err := os.ReadDir(planningDir)
	if err != nil {
		return "0001"
	}

	maxNum := 0
	for _, e := range entries {
		m := numPrefix.FindStringSubmatch(e.Name())
		if m != nil {
			n, _ := strconv.Atoi(m[1])
			if n > maxNum {
				maxNum = n
			}
		}
	}

	if maxNum == 0 {
		return "0001"
	}
	return fmt.Sprintf("%04d", maxNum+1)
}

// FindPlanFile finds the active plan file matching the branch, or falls back to most recent.
func FindPlanFile(planningDir, branch string) string {
	entries, err := os.ReadDir(planningDir)
	if err != nil {
		return ""
	}

	var todoFiles []string
	var allPlanFiles []string

	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") {
			continue
		}
		if strings.HasSuffix(name, "-todo.md") {
			todoFiles = append(todoFiles, name)
		}
		if numPrefix.MatchString(name) {
			allPlanFiles = append(allPlanFiles, name)
		}
	}

	candidates := todoFiles
	if len(candidates) == 0 {
		candidates = allPlanFiles
	}
	if len(candidates) == 0 {
		return ""
	}

	// Priority 1: frontmatter branch: exact match
	if branch != "" {
		for _, f := range candidates {
			if fm := readFrontmatterBranch(filepath.Join(planningDir, f)); fm == branch {
				return filepath.Join(planningDir, f)
			}
		}
	}

	// Extract suffix after type prefix (feat/fix-name → fix-name)
	branchSuffix := branchTypePrefix.ReplaceAllString(branch, "")

	// Sort descending (newest first)
	sort.Sort(sort.Reverse(sort.StringSlice(candidates)))

	// Priority 2: match branch suffix against filename
	for _, f := range candidates {
		fSlug := regexp.MustCompile(`^\d{4}-[a-z]+-`).ReplaceAllString(f, "")
		fSlug = strings.TrimSuffix(fSlug, "-todo.md")
		fSlug = strings.TrimSuffix(fSlug, "-completed.md")

		if fSlug != "" && branchSuffix != "" &&
			(strings.Contains(fSlug, branchSuffix) || strings.Contains(branchSuffix, fSlug)) {
			return filepath.Join(planningDir, f)
		}
	}

	// Fallback: return empty string when branch is clearly unrelated to all plans.
	// Only fall back to a file if branch is empty (no branch context available).
	if branch != "" {
		return ""
	}

	// No branch context — return highest-numbered candidate (candidates already sorted descending).
	if len(candidates) > 0 {
		return filepath.Join(planningDir, candidates[0])
	}
	return ""
}

// readFrontmatterField reads up to the first 20 lines of a plan file and extracts
// the YAML frontmatter value for the given field name. Returns "" if not found or on any error.
func readFrontmatterField(path, field string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	prefix := field + ":"
	scanner := bufio.NewScanner(f)
	lineCount := 0
	inFrontmatter := false
	for scanner.Scan() {
		line := scanner.Text()
		lineCount++
		if lineCount > 20 {
			break
		}
		if lineCount == 1 && line == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter && line == "---" {
			break
		}
		if inFrontmatter && strings.HasPrefix(line, prefix) {
			val := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			// Strip optional surrounding quotes
			val = strings.Trim(val, `"'`)
			return val
		}
	}
	return ""
}

// readFrontmatterBranch reads the `branch:` field from YAML frontmatter.
func readFrontmatterBranch(path string) string {
	return readFrontmatterField(path, "branch")
}

// PlanHeader holds parsed header fields from a plan file.
type PlanHeader struct {
	PlanNum  string
	Status   string
	Progress string
}

// ParsePlanHeader reads the first 30 lines of a plan file for status/progress.
func ParsePlanHeader(planFile string) PlanHeader {
	result := PlanHeader{}
	if planFile == "" {
		return result
	}

	// Extract plan number from filename
	base := filepath.Base(planFile)
	m := numPrefix.FindStringSubmatch(base)
	if m != nil {
		result.PlanNum = m[1]
	}

	f, err := os.Open(planFile)
	if err != nil {
		return result
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() && lineNum < 30 {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "> **Status:**") {
			result.Status = strings.TrimSpace(strings.TrimPrefix(line, "> **Status:**"))
		} else if strings.HasPrefix(line, "> **Progress:**") {
			result.Progress = strings.TrimSpace(strings.TrimPrefix(line, "> **Progress:**"))
		}
		lineNum++
	}

	return result
}

// reportPrefix matches R-NNNN or U-NNNN prefix patterns
var reportPrefix = regexp.MustCompile(`^([RU])-(\d{4})`)

// GetNextReportNum scans a directory for R-NNNN or U-NNNN prefixed files and returns next.
// Unlike GetNextNumAtomic, this does NOT use atomic file reservation — reports are created
// interactively (one at a time by user request), so no concurrency risk.
func GetNextReportNum(dir, prefix string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return prefix + "-0001"
	}

	maxNum := 0
	for _, e := range entries {
		m := reportPrefix.FindStringSubmatch(e.Name())
		if m != nil && m[1] == prefix {
			n, _ := strconv.Atoi(m[2])
			if n > maxNum {
				maxNum = n
			}
		}
	}

	return fmt.Sprintf("%s-%04d", prefix, maxNum+1)
}

// MetaJSON returns the meta result as indented JSON string.
func (m *MetaResult) JSON() string {
	b, _ := json.MarshalIndent(m, "", "  ")
	return string(b)
}
