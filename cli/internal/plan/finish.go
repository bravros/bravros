package plan

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// FinishOpts configures the finish operation.
type FinishOpts struct {
	PlanFile    string // optional — auto-detect if empty
	PRNumber    string // optional PR number
	SkipBacklog bool   // skip backlog archiving
	DryRun      bool   // report what would happen without changes
}

// FinishResult holds the output of a finish operation.
type FinishResult struct {
	PlanFile        string `json:"plan_file"`
	NewFile         string `json:"new_file"`
	BacklogArchived string `json:"backlog_archived,omitempty"`
	CommitHash      string `json:"commit_hash,omitempty"`
	DryRun          bool   `json:"dry_run"`
	Skipped         string `json:"skipped,omitempty"`
}

// readFrontmatterPR reads the `pr:` field from YAML frontmatter.
func readFrontmatterPR(path string) string {
	return readFrontmatterField(path, "pr")
}

// findPlanFileForFinish finds the plan file to finish using a priority chain:
// 1. When --pr N is provided: match by pr: frontmatter across all plan files (including -complete.md)
// 2. Fall back to FindPlanFile (branch-based detection)
// 3. Auto-pick only when exactly ONE -todo.md exists; error if multiple
func findPlanFileForFinish(planningDir, prNumber string) (string, error) {
	// Priority 1: match by pr: frontmatter when PR number is given
	if prNumber != "" {
		allFiles, _ := filepath.Glob(filepath.Join(planningDir, "*.md"))
		for _, f := range allFiles {
			if pr := readFrontmatterPR(f); pr == prNumber {
				return f, nil
			}
		}
		return "", fmt.Errorf("no plan file found with pr: %s in frontmatter", prNumber)
	}

	// Priority 2: branch-based detection via FindPlanFile
	branch := currentBranch()
	if branch != "" {
		if found := FindPlanFile(planningDir, branch); found != "" {
			return found, nil
		}
	}

	// Priority 3: only auto-pick when exactly ONE -todo.md
	matches, _ := filepath.Glob(filepath.Join(planningDir, "*-todo.md"))
	if len(matches) == 0 {
		return "", fmt.Errorf("no plan file found (no *-todo.md in %s)", planningDir)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return "", fmt.Errorf("multiple active plans found (%d), specify --plan or ensure branch: frontmatter matches current branch:\n  %s", len(matches), strings.Join(matches, "\n  "))
}

// currentBranch returns the current git branch name, or "" on error.
func currentBranch() string {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Finish performs atomic plan completion: sync, rename, archive backlog, commit.
func Finish(opts FinishOpts) (*FinishResult, error) {
	result := &FinishResult{DryRun: opts.DryRun}

	// 1. Find plan file
	planFile := opts.PlanFile
	if planFile == "" {
		var err error
		planFile, err = findPlanFileForFinish(".planning", opts.PRNumber)
		if err != nil {
			return nil, err
		}
	}

	// Check file exists
	if _, err := os.Stat(planFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("plan file not found: %s", planFile)
	}

	result.PlanFile = planFile

	// 2. If already complete, skip
	if strings.HasSuffix(planFile, "-complete.md") || strings.HasSuffix(planFile, "-completed.md") {
		result.Skipped = "plan already completed"
		result.NewFile = planFile
		return result, nil
	}

	// 3. Compute new filename: -todo.md → -complete.md
	newFile := strings.TrimSuffix(planFile, "-todo.md") + "-complete.md"
	result.NewFile = newFile

	// 4. Read plan frontmatter for backlog field (before any mutations)
	backlogID := extractBacklogID(planFile)

	if opts.DryRun {
		if backlogID != "" && !opts.SkipBacklog {
			result.BacklogArchived = "would archive backlog " + backlogID
		}
		result.DryRun = true
		return result, nil
	}

	// 5. Sync frontmatter (mark status=completed)
	_, err := SyncPlanFile(planFile, true, opts.PRNumber)
	if err != nil {
		return nil, fmt.Errorf("sync failed: %w", err)
	}

	// 6. git mv plan file
	if err := gitMv(planFile, newFile); err != nil {
		return nil, fmt.Errorf("git mv plan file: %w", err)
	}

	// Update wikilinks in all .planning/ files
	oldBase := strings.TrimSuffix(filepath.Base(planFile), ".md")
	newBase := strings.TrimSuffix(filepath.Base(newFile), ".md")
	if err := updateWikilinks(".planning", oldBase, newBase); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: wikilink sync failed: %v\n", err)
	}

	// 7. Archive backlog if applicable
	if backlogID != "" && !opts.SkipBacklog {
		archived, err := archiveBacklogItem(backlogID)
		if err != nil {
			// Non-fatal: log but continue
			fmt.Fprintf(os.Stderr, "Warning: backlog archive failed: %v\n", err)
		} else if archived != "" {
			result.BacklogArchived = archived
		}
	}

	// 8. Commit
	commitMsg := buildFinishCommitMsg(planFile)
	hash, err := finishCommit(commitMsg)
	if err != nil {
		return nil, fmt.Errorf("commit failed: %w", err)
	}
	result.CommitHash = hash

	return result, nil
}

// extractBacklogID reads the plan file frontmatter and returns the backlog field value.
func extractBacklogID(planFile string) string {
	content, err := os.ReadFile(planFile)
	if err != nil {
		return ""
	}

	text := string(content)
	parts := strings.SplitN(text, "---", 3)
	if len(parts) < 3 {
		return ""
	}

	var fm map[string]interface{}
	if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
		return ""
	}

	val, ok := fm["backlog"]
	if !ok || val == nil {
		return ""
	}

	return fmt.Sprintf("%v", val)
}

// archiveBacklogItem finds a backlog file by ID, updates status to completed, and renames to -complete.md.
func archiveBacklogItem(backlogID string) (string, error) {
	backlogDir := ".planning/backlog"

	entries, err := os.ReadDir(backlogDir)
	if err != nil {
		return "", fmt.Errorf("cannot read backlog dir: %w", err)
	}

	idPrefix := regexp.MustCompile(`^(\d{4})`)
	var backlogFile string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		m := idPrefix.FindStringSubmatch(e.Name())
		if m != nil && m[1] == backlogID {
			backlogFile = filepath.Join(backlogDir, e.Name())
			break
		}
	}

	if backlogFile == "" {
		return "", fmt.Errorf("backlog file not found for ID %s", backlogID)
	}

	// Check if already completed
	if strings.HasSuffix(backlogFile, "-complete.md") {
		return backlogFile, nil
	}

	// Update status
	if err := updateBacklogStatus(backlogFile, "completed"); err != nil {
		return "", fmt.Errorf("update backlog status: %w", err)
	}

	// Rename to -complete.md (stays in place)
	newName := backlogFile
	if strings.HasSuffix(newName, "-open.md") {
		newName = strings.TrimSuffix(newName, "-open.md") + "-complete.md"
	} else if !strings.HasSuffix(newName, "-complete.md") {
		newName = strings.TrimSuffix(newName, ".md") + "-complete.md"
	}

	if err := gitMv(backlogFile, newName); err != nil {
		return "", fmt.Errorf("git mv backlog: %w", err)
	}

	return newName, nil
}

// updateBacklogStatus reads a backlog file, updates the status in YAML frontmatter, and writes it back.
func updateBacklogStatus(path, status string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	text := string(content)
	parts := strings.SplitN(text, "---", 3)
	if len(parts) < 3 {
		return fmt.Errorf("no frontmatter in %s", path)
	}

	var fm map[string]interface{}
	if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
		return fmt.Errorf("YAML parse error: %w", err)
	}

	fm["status"] = status

	yamlBytes, err := yaml.Marshal(fm)
	if err != nil {
		return fmt.Errorf("YAML marshal error: %w", err)
	}

	output := "---\n" + string(yamlBytes) + "---" + parts[2]
	return os.WriteFile(path, []byte(output), 0644)
}

// CleanOrphanTodos finds orphan files left after squash-merge + sync:
// 1. -todo.md files in .planning/ that have a corresponding -complete.md
// 2. backlog files in .planning/backlog/ that already exist in .planning/backlog/archive/
// Returns the list of removed files.
func CleanOrphanTodos() ([]string, error) {
	var removed []string

	// 1. Clean orphan plan -todo.md files
	planDir := ".planning"
	entries, err := os.ReadDir(planDir)
	if err == nil {
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, "-todo.md") {
				continue
			}
			completeName := strings.TrimSuffix(name, "-todo.md") + "-complete.md"
			completePath := filepath.Join(planDir, completeName)
			if _, err := os.Stat(completePath); err == nil {
				todoPath := filepath.Join(planDir, name)
				rmCmd := exec.Command("git", "rm", "-f", todoPath)
				rmCmd.Stderr = os.Stderr
				if err := rmCmd.Run(); err != nil {
					os.Remove(todoPath)
				}
				removed = append(removed, todoPath)
			}
		}
	}

	// 2. Clean orphan backlog files (active copy exists alongside archived copy)
	backlogDir := filepath.Join(planDir, "backlog")
	archiveDir := filepath.Join(backlogDir, "archive")
	backlogEntries, err := os.ReadDir(backlogDir)
	if err == nil {
		for _, e := range backlogEntries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".md") {
				continue
			}
			archivePath := filepath.Join(archiveDir, name)
			if _, err := os.Stat(archivePath); err == nil {
				activePath := filepath.Join(backlogDir, name)
				rmCmd := exec.Command("git", "rm", "-f", activePath)
				rmCmd.Stderr = os.Stderr
				if err := rmCmd.Run(); err != nil {
					os.Remove(activePath)
				}
				removed = append(removed, activePath)
			}
		}
	}

	return removed, nil
}

// updateWikilinks scans all .md files under planningDir for wikilink references
// and replaces old base names with new ones.
func updateWikilinks(planningDir, oldBase, newBase string) error {
	return filepath.Walk(planningDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		text := string(content)
		oldLink := "[[" + oldBase + "]]"
		newLink := "[[" + newBase + "]]"
		if strings.Contains(text, oldLink) {
			updated := strings.ReplaceAll(text, oldLink, newLink)
			os.WriteFile(path, []byte(updated), info.Mode())
		}
		return nil
	})
}

// gitMv runs git mv src dst.
func gitMv(src, dst string) error {
	cmd := exec.Command("git", "mv", src, dst)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// buildFinishCommitMsg generates a commit message from the plan filename.
func buildFinishCommitMsg(planFile string) string {
	base := filepath.Base(planFile)
	// Strip -todo.md suffix and extract the slug
	slug := strings.TrimSuffix(base, "-todo.md")
	return fmt.Sprintf("🧹 chore: finish %s", slug)
}

// finishCommit stages .planning/ and commits, returning the short hash.
func finishCommit(message string) (string, error) {
	// Stage all .planning/ changes
	addCmd := exec.Command("git", "add", ".planning/")
	if err := addCmd.Run(); err != nil {
		return "", fmt.Errorf("git add failed: %w", err)
	}

	// Check if there's anything to commit
	checkCmd := exec.Command("git", "diff", "--cached", "--quiet")
	if err := checkCmd.Run(); err == nil {
		// Nothing staged
		return "", nil
	}

	// Commit
	commitCmd := exec.Command("git", "commit", "-m", message)
	commitCmd.Stdout = os.Stdout
	commitCmd.Stderr = os.Stderr
	if err := commitCmd.Run(); err != nil {
		return "", fmt.Errorf("git commit failed: %w", err)
	}

	// Get hash
	hashOut, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "", nil
	}

	return strings.TrimSpace(string(hashOut)), nil
}
