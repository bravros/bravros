package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bravros/private/internal/config"
	gitpkg "github.com/bravros/private/internal/git"
	"github.com/bravros/private/internal/plan"
	"github.com/spf13/cobra"
)

// fieldExtract extracts a value from JSON using dot-path notation.
// Examples:
//
//	fieldExtract(`{"base_branch": "main"}`, "base_branch") → "main"
//	fieldExtract(`{"stack": {"framework": "laravel"}}`, "stack.framework") → "laravel"
//	fieldExtract(`{"has_ci": true}`, "has_ci") → "true"
//	fieldExtract(`{"missing": "field"}`, "nonexistent") → ""
func fieldExtract(jsonStr string, field string) string {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return ""
	}

	parts := strings.Split(field, ".")
	var current interface{} = data

	for i, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return ""
		}

		// Try exact key first
		val, found := m[part]
		if !found {
			// Try joining remaining parts with dots as a single key (for keys containing dots/slashes)
			remaining := strings.Join(parts[i:], ".")
			val, found = m[remaining]
			if found {
				current = val
				break
			}
			return ""
		}
		current = val
	}

	switch v := current.(type) {
	case string:
		return v
	case bool:
		return fmt.Sprintf("%v", v)
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%v", v)
	case nil:
		return ""
	default:
		// For nested objects/arrays, return JSON
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// ─── meta ───────────────────────────────────────────────────────────────────

var (
	metaReserve bool
	metaField   string
)

var metaCmd = &cobra.Command{
	Use:   "meta",
	Short: "Plan metadata as JSON (use --reserve to atomically reserve IDs)",
	Run: func(cmd *cobra.Command, args []string) {
		repo, err := gitpkg.Open("")
		if err != nil {
			fmt.Println(`{"error": "Not in a git repository"}`)
			os.Exit(1)
		}

		branch := repo.CurrentBranch()
		if branch == "" {
			branch = "main"
		}

		baseBranch := repo.DetectBaseBranchSimple()
		planFile := plan.FindPlanFile(".planning", branch)
		header := plan.ParsePlanHeader(planFile)
		project := gitpkg.ProjectName()
		gitRemote := repo.RemoteURL("origin")

		var nextNum, backlogNext string

		if metaReserve {
			// Atomic reservation (replaces nextid)
			os.MkdirAll(".planning", 0755)
			os.MkdirAll(".planning/backlog", 0755)

			pNum, pCleanup, err := plan.GetNextNumAtomic(".planning")
			if err == nil {
				nextNum = pNum
				defer pCleanup()
			} else {
				nextNum = plan.GetNextNum(".planning")
			}

			bNum, bCleanup, err := plan.GetNextNumAtomic(".planning/backlog")
			if err == nil {
				backlogNext = bNum
				defer bCleanup()
			}
		} else {
			nextNum = plan.GetNextNum(".planning")
		}

		result := &plan.MetaResult{
			NextNum:     nextNum,
			BacklogNext: backlogNext,
			BaseBranch:  baseBranch,
			Branch:      branch,
			PlanFile:    planFile,
			PlanNum:     header.PlanNum,
			Status:      header.Status,
			Progress:    header.Progress,
			Project:     project,
			GitRemote:   gitRemote,
			Today:       time.Now().Format("02/01/2006 15:04"),
		}

		// Load .bravros.yml if present
		cfg, found := config.LoadBravrosConfig()
		if found {
			result.Stack = cfg.Stack
			result.Stacks = cfg.Stacks
			result.Git = cfg.Git
			result.Monorepo = cfg.Monorepo
		}

		jsonOutput := result.JSON()
		if metaField != "" {
			fmt.Println(fieldExtract(jsonOutput, metaField))
		} else {
			fmt.Println(jsonOutput)
		}
	},
}

// ─── context ────────────────────────────────────────────────────────────────

var (
	contextDiffs  bool
	contextSkipPR bool
)

var contextCmd = &cobra.Command{
	Use:   "context [BASE_BRANCH]",
	Short: "Git context for PR/plan-check",
	Run: func(cmd *cobra.Command, args []string) {
		repo, err := gitpkg.Open("")
		if err != nil {
			fmt.Println("❌ Not in a git repository")
			os.Exit(1)
		}

		var base string
		if len(args) > 0 {
			base = args[0]
		} else {
			base = repo.DetectBaseBranch()
		}

		current := repo.CurrentBranch()
		if current == "" {
			fmt.Println("❌ Could not determine current branch")
			os.Exit(1)
		}

		fmt.Printf("\n%s\n", strings.Repeat("=", 60))
		fmt.Printf("BASE_BRANCH:    %s\n", base)
		fmt.Printf("CURRENT_BRANCH: %s\n", current)
		fmt.Printf("%s\n", strings.Repeat("=", 60))

		// Commits since base
		fmt.Println(gitpkg.Section("Commits Since " + base))
		commits, err := gitpkg.CommitsSince(base)
		if err != nil || commits == "" {
			fmt.Printf("(no commits since %s)\n", base)
		} else {
			fmt.Println(commits)
		}

		// Changed files
		fmt.Println(gitpkg.Section("Changed Files"))
		changedFiles, err := gitpkg.ChangedFiles(base)
		if err != nil || len(changedFiles) == 0 {
			fmt.Println("(no changed files)")
		} else {
			fmt.Println(strings.Join(changedFiles, "\n"))
		}

		// Diff stat
		fmt.Println(gitpkg.Section("Diff Stat"))
		stat, err := gitpkg.DiffStat(base)
		if err != nil || stat == "" {
			fmt.Println("(no diff)")
		} else {
			lines := strings.Split(stat, "\n")
			if len(lines) <= 20 {
				fmt.Println(stat)
			} else {
				fmt.Println(strings.Join(lines[:15], "\n"))
				fmt.Printf("  ... (%d more files)\n", len(lines)-16)
				fmt.Println(lines[len(lines)-1])
			}
		}

		// Per-file diffs
		if contextDiffs && len(changedFiles) > 0 {
			fmt.Println(gitpkg.Section("File Diffs"))
			for _, fname := range changedFiles {
				padding := 54 - len(fname)
				if padding < 0 {
					padding = 0
				}
				fmt.Printf("\n──── %s %s\n", fname, strings.Repeat("─", padding))
				diff, err := gitpkg.FileDiff(base, fname)
				if err != nil || diff == "" {
					fmt.Println("  (no diff)")
				} else {
					diffLines := strings.Split(diff, "\n")
					if len(diffLines) > 120 {
						fmt.Println(strings.Join(diffLines[:120], "\n"))
						fmt.Printf("  ... (%d more lines)\n", len(diffLines)-120)
					} else {
						fmt.Println(diff)
					}
				}
			}
		}

		// Associated PR
		if !contextSkipPR {
			fmt.Println(gitpkg.Section("Associated PR"))
			pr, err := gitpkg.PRInfo()
			if err != nil {
				fmt.Println("(no open PR for this branch)")
			} else {
				num := pr["number"]
				state := pr["state"]
				title := pr["title"]
				url := pr["url"]
				fmt.Printf("#%v — %v — %v\n", num, state, title)
				if url != nil {
					fmt.Println(url)
				}
			}
		}

		fmt.Printf("\n%s\n\n", strings.Repeat("=", 60))
	},
}

// ─── sync ───────────────────────────────────────────────────────────────────

var (
	syncFinish   bool
	syncPRNumber string
)

var syncCmd = &cobra.Command{
	Use:   "sync [plan-file]",
	Short: "Sync plan frontmatter counts",
	Run: func(cmd *cobra.Command, args []string) {
		planFile := ""
		if len(args) > 0 {
			planFile = args[0]
		}

		result, err := plan.SyncPlanFile(planFile, syncFinish, syncPRNumber)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		if result.Finished {
			fmt.Printf("Finished: status=completed pr=%s tasks=%d/%d\n",
				result.PR, result.TasksDone, result.TasksTotal)
		} else {
			fmt.Printf("Synced: %d/%d tasks, %d/%d phases, %d sessions\n",
				result.TasksDone, result.TasksTotal,
				result.PhasesDone, result.PhasesTotal,
				result.Sessions)
		}
	},
}

// ─── full ───────────────────────────────────────────────────────────────────

var (
	fullDiffs  bool
	fullSkipPR bool
)

var fullCmd = &cobra.Command{
	Use:   "full",
	Short: "Meta JSON + git context in one call",
	Run: func(cmd *cobra.Command, args []string) {
		// Run meta
		metaCmd.Run(cmd, nil)
		fmt.Print("\n---\n\n")
		// Run context with full flags
		contextDiffs = fullDiffs
		contextSkipPR = fullSkipPR
		contextCmd.Run(cmd, nil)
	},
}

// ─── commit ─────────────────────────────────────────────────────────────────

// ─── nextid ────────────────────────────────────────────────────────────────

var nextidCmd = &cobra.Command{
	Use:   "nextid",
	Short: "Atomically reserve next plan, backlog, report, and user-report IDs (JSON)",
	Run: func(cmd *cobra.Command, args []string) {
		planDir := ".planning"
		backlogDir := ".planning/backlog"
		reportsDir := ".planning/reports"
		userReportsDir := ".planning/user-reports"

		os.MkdirAll(planDir, 0755)
		os.MkdirAll(backlogDir, 0755)
		os.MkdirAll(reportsDir, 0755)
		os.MkdirAll(userReportsDir, 0755)

		planNum, planCleanup, err := plan.GetNextNumAtomic(planDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reserving plan ID: %v\n", err)
			os.Exit(1)
		}
		defer planCleanup()

		backlogNum, backlogCleanup, err := plan.GetNextNumAtomic(backlogDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reserving backlog ID: %v\n", err)
			os.Exit(1)
		}
		defer backlogCleanup()

		reportNum := plan.GetNextReportNum(reportsDir, "R")
		userReportNum := plan.GetNextReportNum(userReportsDir, "U")

		result := map[string]string{
			"plan":        planNum,
			"backlog":     backlogNum,
			"report":      reportNum,
			"user_report": userReportNum,
		}
		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(b))
	},
}

// ─── commit ─────────────────────────────────────────────────────────────────

var commitCmd = &cobra.Command{
	Use:   "commit <message> [files...]",
	Short: "Commit plan + code changes",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		message := args[0]
		files := args[1:]
		if err := plan.Commit(message, files); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

// ─── backlog ────────────────────────────────────────────────────────────────

var (
	backlogArchive bool
	backlogFormat  string
)

var backlogCmd = &cobra.Command{
	Use:   "backlog",
	Short: "List backlog items (JSON default, --format table for pretty output)",
	Run: func(cmd *cobra.Command, args []string) {
		result, err := plan.ScanBacklog(".planning", backlogArchive)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		switch strings.ToLower(backlogFormat) {
		case "table":
			fmt.Print(result.Table())
		default:
			fmt.Println(result.JSON())
		}
	},
}

var backlogMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Convert old blockquote backlog files to YAML frontmatter format",
	Run: func(cmd *cobra.Command, args []string) {
		project := gitpkg.ProjectName()

		// Migrate active
		activeResult, err := plan.MigrateBacklogDir(".planning/backlog", project)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Migrate archive
		archiveResult, err := plan.MigrateBacklogDir(".planning/backlog/archive", project)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: archive migration error: %v\n", err)
		}

		total := len(activeResult.Migrated)
		if archiveResult != nil {
			total += len(archiveResult.Migrated)
		}

		if total == 0 {
			fmt.Println("No old-format backlog files found. All files already use YAML frontmatter.")
			return
		}

		fmt.Printf("Migrated %d files to YAML frontmatter:\n", total)
		for _, f := range activeResult.Migrated {
			fmt.Printf("  ✅ backlog/%s\n", f)
		}
		if archiveResult != nil {
			for _, f := range archiveResult.Migrated {
				fmt.Printf("  ✅ archive/%s\n", f)
			}
		}
	},
}

func init() {
	// meta flags
	metaCmd.Flags().BoolVar(&metaReserve, "reserve", false, "Atomically reserve plan + backlog IDs (replaces nextid)")
	metaCmd.Flags().StringVar(&metaField, "field", "", "Extract a single field value (dot notation, e.g. stack.framework)")

	// context flags
	contextCmd.Flags().BoolVar(&contextDiffs, "diffs", false, "Include full per-file diffs")
	contextCmd.Flags().BoolVar(&contextSkipPR, "skip-pr", false, "Skip gh pr view call (saves ~500ms)")

	// sync flags
	syncCmd.Flags().BoolVar(&syncFinish, "finish", false, "Mark plan as completed")
	syncCmd.Flags().StringVar(&syncPRNumber, "pr", "", "PR number (use with --finish)")

	// full flags
	fullCmd.Flags().BoolVar(&fullDiffs, "diffs", false, "Include full per-file diffs")
	fullCmd.Flags().BoolVar(&fullSkipPR, "skip-pr", false, "Skip gh pr view call")

	// backlog flags
	backlogCmd.Flags().BoolVar(&backlogArchive, "archive", false, "Include archived items")
	backlogCmd.Flags().StringVar(&backlogFormat, "format", "json", "Output format: json or table")
	backlogCmd.AddCommand(backlogMigrateCmd)

	rootCmd.AddCommand(metaCmd)
	rootCmd.AddCommand(contextCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(fullCmd)
	rootCmd.AddCommand(commitCmd)
	rootCmd.AddCommand(backlogCmd)
	rootCmd.AddCommand(nextidCmd)
}
