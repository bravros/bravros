package audit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bravros/bravros/internal/i18n"
)

// Pre-compiled regex patterns for performance (hot path).
var (
	reCheckpoint        = regexp.MustCompile(`\[([a-z-]+:[0-9a-z-]+)\]`)
	reMigrateFresh      = regexp.MustCompile(`migrate:fresh`)
	reMigrateRollback   = regexp.MustCompile(`migrate:rollback`)
	reDbWipe            = regexp.MustCompile(`db:wipe`)
	reDropTable         = regexp.MustCompile(`(?i)DROP\s+TABLE`)
	reTruncateTable     = regexp.MustCompile(`(?i)TRUNCATE\s+TABLE`)
	reDeleteFromNoWhere = regexp.MustCompile(`(?i)DELETE\s+FROM\s+\S+\s*[;"'` + "`" + `|&$)}\]]`)
	reGitPushMain       = regexp.MustCompile(`git\s+push(?:\s+-\S+)*\s+\S+\s+main\b`)
	reBarePush          = regexp.MustCompile(`(?:^|&&|;)\s*git\s+push\s*$`)
	reCdPath            = regexp.MustCompile(`(?:^|&&|;)\s*cd\s+(\S+)`)
	reGitCommit         = regexp.MustCompile(`git\s+commit`)
	reAISig             = regexp.MustCompile(`(?i)Co-Authored-By|Generated.by.AI|Generated.by.Claude`)
	reGhPrCreateEdit    = regexp.MustCompile(`gh\s+pr\s+(create|edit)`)
	reGhPrComment       = regexp.MustCompile(`gh\s+pr\s+comment`)
	reClaudeReview      = regexp.MustCompile(`(?i)@claude\s+review`)
	reCheckMerge        = regexp.MustCompile(`(?i)check if we are able to merge`)
	reGhPrMerge         = regexp.MustCompile(`gh pr merge`)
	reGhPrMergeNum      = regexp.MustCompile(`gh\s+pr\s+merge\s+(\d+)`)
	reGhApiMergeNum     = regexp.MustCompile(`gh\s+api\s+repos/\S+/pulls/(\d+)/merge`)
	reGhPrCreate        = regexp.MustCompile(`gh pr create`)
	reGhApiPulls        = regexp.MustCompile(`gh api repos/\S+/pulls\b.*-X POST`)
	reGhPrEdit          = regexp.MustCompile(`gh\s+pr\s+edit`)
	reTestCmd           = regexp.MustCompile(`(vendor/bin/pest|php\s+artisan\s+test|artisan\s+test|herd\s+coverage)`)
	reFilter            = regexp.MustCompile(`--filter`)
	reTestFile          = regexp.MustCompile(`tests/\S+`)
	reParallel          = regexp.MustCompile(`--parallel`)
	reProcesses10       = regexp.MustCompile(`--processes[=\s]+10`)
	reTaskChecked       = regexp.MustCompile(`(?i)- \[x\]`)
	reTaskUnchecked     = regexp.MustCompile(`- \[ \]`)
	reTaskAny           = regexp.MustCompile(`(?i)- \[[x ]\]`)
	rePlanningTodo      = regexp.MustCompile(`\.planning/.*-todo\.md$`)
	rePlanningMd        = regexp.MustCompile(`\.planning/.*\.md$`)
	reAcceptCriteria    = regexp.MustCompile(`(?is)##\s*Acceptance\s*Criteria\s*\n(.*?)(?:\n##\s|\z)`)

	aiSigPatternsPR = []*regexp.Regexp{
		regexp.MustCompile(`(?i)Generated\s+with\s+\[?Claude`),
		regexp.MustCompile(`(?i)Generated\s+with\s+Claude\s+Code`),
		regexp.MustCompile(`(?i)Co-Authored-By.*claude`),
		regexp.MustCompile(`(?i)Co-Authored-By.*anthropic`),
		regexp.MustCompile(`(?i)Co-Authored-By.*noreply@anthropic`),
		regexp.MustCompile(`🤖\s*Generated`),
		regexp.MustCompile(`(?i)Generated\s+by\s+AI`),
		regexp.MustCompile(`(?i)Generated\s+by\s+Claude`),
		regexp.MustCompile(`(?i)claude\.com/claude-code`),
	}

	aiSigPatternsComment = []*regexp.Regexp{
		regexp.MustCompile(`(?i)Generated\s+with\s+\[?Claude`),
		regexp.MustCompile(`🤖\s*Generated`),
		regexp.MustCompile(`(?i)Generated\s+by\s+AI`),
		regexp.MustCompile(`(?i)Generated\s+by\s+Claude`),
		regexp.MustCompile(`(?i)claude\.com/claude-code`),
	}
)

// skillMap maps file extensions to required skill names.
var skillMap = map[string]string{
	".jsx":  "frontend-design",
	".tsx":  "frontend-design",
	".html": "frontend-design",
	".css":  "frontend-design",
}

// skillAliases maps skill names to the set of requirement markers they satisfy.
// When a skill is invoked, all its aliases are also touched in session state.
var skillAliases = map[string][]string{
	"taste-skill":      {"frontend-design"},
	"brand-guidelines": {"frontend-design"},
}

// Pre-compiled patterns for rule 15 (backlog CLI enforcement).
var (
	reBacklogGrep = regexp.MustCompile(`(grep|sed|awk).*\.planning/backlog`)
	reBacklogLoop = regexp.MustCompile(`for\s+\w+\s+in\s+.*\.planning/backlog`)
	reBacklogCat  = regexp.MustCompile(`cat\s+.*\.planning/backlog`)
)

// Pre-compiled patterns for rule 16 (planning mv check).
var (
	reGitMv  = regexp.MustCompile(`(^|[;&|\s])git\s+mv\s`)
	reBareMv = regexp.MustCompile(`(^|[;&|\s])mv\s`)
)

// Pre-compiled patterns for rule 20 (bash redirect to .planning/).
var (
	reBashRedirectPlanning = regexp.MustCompile(`(?:cat|echo|tee)\s+[^|]*>\s*[^>]*\.planning/|<<\s*['"]?\w+['"]?\s*>\s*\.planning/|heredoc.*\.planning/|tee\s+[^|&;]*\.planning/`)
)

// Pre-compiled patterns for rule 17 (block merge to main).
var (
	reGhApiMergePut      = regexp.MustCompile(`gh\s+api\s+repos/\S+/pulls/\d+/merge`)
	reGitPushRefspecMain = regexp.MustCompile(`git\s+push\s+\S+\s+\S+:main\b`)
)

// isSDLCFlow returns true for skills that are part of the structured SDLC pipeline.
// NOT hotfix (emergency fast-path) and NOT finish (exit gate that always needs user approval).
func isSDLCFlow(activeCmd string) bool {
	switch activeCmd {
	case "plan", "plan-review", "plan-approved", "plan-check",
		"flow", "auto-pr", "auto-pr-wt", "auto-merge":
		return true
	}
	return false
}

// detectPortableRepo returns the portable repo path based on OS.
func detectPortableRepo() string {
	home, _ := os.UserHomeDir()
	// macOS: ~/Sites/claude, Linux: ~/claude
	macPath := filepath.Join(home, "Sites", "claude")
	if _, err := os.Stat(macPath); err == nil {
		return macPath
	}
	return filepath.Join(home, "claude")
}

// extractMvSource extracts the source path from an mv command.
func extractMvSource(command string) string {
	// Match: mv <source> <dest> — extract source
	parts := strings.Fields(command)
	for i, p := range parts {
		if p == "mv" && i+1 < len(parts) {
			src := parts[i+1]
			if !strings.HasPrefix(src, "-") { // skip flags
				return src
			}
		}
	}
	return ""
}

// RunRules executes all 23 enforcement rules.
func RunRules(p *Payload, state *SessionState, log *Logger) {
	paths := p.AllPaths()

	// ENFORCEMENT 1: Skill read before frontend edits
	rule1SkillReadBeforeFrontend(p, state, log, paths)

	// ENFORCEMENT 2: Track SKILL.md and reference file reads
	rule2TrackSkillReads(p, state, log)

	// ENFORCEMENT 3: Team vs Subagent compliance
	rule3TeamCompliance(p, state, log)

	// ENFORCEMENT 4: Track AskUserQuestion calls
	rule4AskUserTracking(p, state, log)

	// ENFORCEMENT 5: Command checkpoint tracking & prerequisites
	rule5CommandCheckpoints(p, state, log)

	// ENFORCEMENT 6: Plan file writes require template read
	rule6PlanTemplateRead(p, state, log, paths)

	// ENFORCEMENT 7: Block GitHub workflow creation without homolog
	rule7BlockWorkflowWithoutHomolog(p, state, log, paths)

	// ENFORCEMENT 8: Full test suite must run in parallel
	rule8TestParallel(p, state, log)

	// ENFORCEMENT 9: Plan task deletion detection
	rule9TaskDeletion(p, state, log, paths)

	// ENFORCEMENT 10: Block dangerous commands
	rule10BlockDangerous(p, state, log)

	// ENFORCEMENT 11: @claude review prompt must be comprehensive
	rule11ClaudeReviewPrompt(p, state, log)

	// ENFORCEMENT 12: Plan-check skip detection
	rule12PlanCheckSkip(p, state, log)

	// ENFORCEMENT 13: Unchecked acceptance criteria on PR creation
	rule13UncheckedAcceptance(p, state, log)

	// ENFORCEMENT 14: auto-pr step enforcement
	rule14FlowAutoEnforcement(p, state, log)

	// ENFORCEMENT 15: Backlog CLI enforcement — block manual parsing
	rule15BacklogCLI(p, state, log)

	// ENFORCEMENT 16: Planning mv check — require git mv for .planning/ files
	rule16PlanningMvCheck(p, state, log)

	// ENFORCEMENT 17: Block merge to main outside allowed skills
	rule17BlockMainMerge(p, state, log)

	// ENFORCEMENT 18: Project config detection
	rule18ProjectConfig(p, state, log)

	// ENFORCEMENT 19: Agent model must match task markers [H]/[S]/[O]
	rule19AgentModelEnforcement(p, state, log)

	// ENFORCEMENT 20: Bash redirect bypass to .planning/
	rule20BashRedirectToPlanning(p, state, log)

	// ENFORCEMENT 21: Auto skill gate
	rule21AutoSkillGate(p, state, log)

	// ENFORCEMENT 21b: Lock file tamper protection
	rule21bLockFileTamper(p, state, log)

	// ENFORCEMENT 21c: Autonomous mode sticky tracking
	rule21cTrackAutonomousMode(p, state, log)

	// ENFORCEMENT 22: Read-only skill enforcement
	rule22ReadOnlySkillEnforcement(p, state, log)

	// ENFORCEMENT 23: Block writes to ~/.claude/ deployed dir
	rule23BlockDeployedDirWrites(p, state, log)
}

func rule1SkillReadBeforeFrontend(p *Payload, state *SessionState, log *Logger, paths []string) {
	if !p.IsWriteLike() || len(paths) == 0 {
		return
	}
	for _, path := range paths {
		ext := filepath.Ext(path)
		skillName, ok := skillMap[ext]
		if !ok {
			continue
		}
		if !state.Exists("skill-read-" + skillName) {
			log.Block(i18n.Tf("audit.block_skill_read_first", path, skillName))
		}
	}
}

func rule2TrackSkillReads(p *Payload, state *SessionState, log *Logger) {
	// Track Skill tool invocations
	if p.ToolName == "skill" {
		skillArg := strings.TrimSpace(firstInputString(p.Input, "skill"))
		if skillArg != "" {
			state.Touch("skill-read-" + skillArg)
			// Also check if this matches a known skill
			for _, known := range uniqueSkillNames() {
				if strings.HasPrefix(skillArg, known) || strings.HasPrefix(known, skillArg) {
					state.Touch("skill-read-" + known)
				}
			}
			// Touch all aliases defined for this skill
			if aliases, ok := skillAliases[skillArg]; ok {
				for _, alias := range aliases {
					state.Touch("skill-read-" + alias)
				}
			}
			log.Log("✅ SKILL INVOKED: " + skillArg)
		}
	}

	// Track Read tool for SKILL.md and reference files
	if p.ToolName == "read" && p.FilePath != "" {
		if strings.Contains(p.FilePath, "SKILL.md") {
			skillName := filepath.Base(filepath.Dir(p.FilePath))
			state.Touch("skill-read-" + skillName)
			log.Log(fmt.Sprintf("✅ SKILL READ: %s (%s)", skillName, p.FilePath))
		}
		if strings.Contains(p.FilePath, "plan-template.md") {
			state.Touch("read-plan-template")
			log.Log("✅ REF READ: plan-template.md")
		}
		if strings.Contains(p.FilePath, "team-execution.md") {
			state.Touch("read-team-execution")
			log.Log("✅ REF READ: team-execution.md")
		}
	}
}

func rule3TeamCompliance(p *Payload, state *SessionState, log *Logger) {
	if p.ToolName == "teamcreate" {
		teamName := firstInputString(p.Input, "name", "team_name")
		if teamName == "" {
			teamName = "unnamed"
		}
		if !state.Exists("read-team-execution") {
			log.Warn(i18n.Tf("audit.warn_team_no_execution_read", teamName))
		}
		state.Touch("team-created")
		state.AppendLine("teams-created.txt", teamName)
		log.Log("✅ TEAM CREATED: " + teamName)
	}

	if p.ToolName == "task" {
		hasTeam := firstInputString(p.Input, "team_name") != ""
		if hasTeam {
			log.Log("✅ TEAM WORKER TASK")
		} else {
			count := state.IncrInt("standalone-task-count")
			if count >= 3 && !state.Exists("team-created") {
				log.Warn(i18n.Tf("audit.warn_standalone_tasks", count))
				log.Log(fmt.Sprintf("🚨 POTENTIAL LIE: %d standalone Task calls, 0 TeamCreate calls", count))
			} else {
				log.Log(fmt.Sprintf("✅ STANDALONE SUBAGENT #%d (1-2 tasks, acceptable)", count))
			}
		}
	}
}

func rule4AskUserTracking(p *Payload, state *SessionState, log *Logger) {
	if p.ToolName == "askuserquestion" || p.ToolName == "question" {
		state.Touch("asked-user")
		log.Log("✅ ASKED USER (AskUserQuestion called)")
	}
}

func rule5CommandCheckpoints(p *Payload, state *SessionState, log *Logger) {
	if p.ToolName != "bash" || p.Command == "" {
		return
	}

	// Track checkpoints
	if m := reCheckpoint.FindStringSubmatch(p.Command); m != nil {
		checkpoint := m[1]
		state.AppendLine("checkpoints.txt", checkpoint)
		activeCmd := strings.Split(checkpoint, ":")[0]
		state.WriteText("active-command", activeCmd)
		log.Log("🏁 CHECKPOINT: " + checkpoint)
	}

	// Prerequisite: /finish must AskUserQuestion before second gh pr merge
	// Exception: auto-merge, auto-pr, auto-pr-wt are autonomous pipelines
	// where merging PRs between rounds is a hard dependency, not discretionary.
	if reGhPrMerge.MatchString(p.Command) {
		activeCmd := state.ReadText("active-command")
		if isAutonomousPipeline(activeCmd) {
			log.Log("✅ gh pr merge in autonomous pipeline (" + activeCmd + ") — allowed")
		} else if activeCmd == "finish" {
			count := state.IncrInt("gh-pr-merge-count")
			if count >= 2 && !state.Exists("asked-user") {
				log.Warn(i18n.T("audit.warn_merge_without_ask"))
			}
		}
	}

	// Prerequisite: gh pr create requires /pr, /finish, or pipeline context
	if strings.Contains(p.Command, "gh pr create") {
		activeCmd := state.ReadText("active-command")
		if isAutonomousPipeline(activeCmd) {
			log.Log("✅ gh pr create in autonomous pipeline (" + activeCmd + ") — allowed")
		} else if activeCmd != "pr" && activeCmd != "finish" && activeCmd != "hotfix" {
			log.Warn(i18n.Tf("audit.warn_pr_create_outside_context", activeCmd))
		}
	}
}

func rule6PlanTemplateRead(p *Payload, state *SessionState, log *Logger, paths []string) {
	if !p.IsWriteLike() || len(paths) == 0 {
		return
	}
	for _, path := range paths {
		if rePlanningMd.MatchString(path) && !strings.Contains(path, "/backlog/") && !strings.Contains(path, "/reports/") && !strings.Contains(path, "/user-reports/") {
			if _, err := os.Stat(path); os.IsNotExist(err) {
				if !state.Exists("read-plan-template") {
					log.Block(i18n.T("audit.block_plan_no_template"))
				}
			}
		}
	}
}

func rule7BlockWorkflowWithoutHomolog(p *Payload, state *SessionState, log *Logger, paths []string) {
	if !p.IsWriteLike() || len(paths) == 0 {
		return
	}
	for _, path := range paths {
		base := filepath.Base(path)
		if strings.Contains(path, ".github/workflows/") && (base == "claude.yml" || base == "tests.yml") {
			cmd := exec.Command("git", "branch", "-a", "--list", "*homolog*")
			out, err := cmd.Output()
			if err != nil || strings.TrimSpace(string(out)) == "" {
				log.Block(i18n.Tf("audit.block_workflow_no_homolog", base))
			}
		}
	}
}

func rule8TestParallel(p *Payload, state *SessionState, log *Logger) {
	if p.ToolName != "bash" || p.Command == "" {
		return
	}
	if !reTestCmd.MatchString(p.Command) {
		return
	}

	// Only enforce for Laravel projects
	cfg, cfgFound := LoadBravrosConfig()
	if cfgFound && cfg.Stack.Framework != "" && cfg.Stack.Framework != "laravel" {
		return
	}

	hasFilter := reFilter.MatchString(p.Command)
	hasTestFile := reTestFile.MatchString(p.Command)
	isFullSuite := !hasFilter && !hasTestFile

	if isFullSuite {
		hasParallel := reParallel.MatchString(p.Command)
		hasProcesses := reProcesses10.MatchString(p.Command)
		if !hasParallel || !hasProcesses {
			log.Block(i18n.T("audit.block_test_not_parallel"))
		}
	}
}

func rule9TaskDeletion(p *Payload, state *SessionState, log *Logger, paths []string) {
	if !p.IsWriteLike() || len(paths) == 0 {
		return
	}
	for _, path := range paths {
		if !rePlanningTodo.MatchString(path) {
			continue
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}

		currentContent, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(currentContent)
		currentChecked := len(reTaskChecked.FindAllString(content, -1))
		currentUnchecked := len(reTaskUnchecked.FindAllString(content, -1))
		currentTotal := currentChecked + currentUnchecked

		oldString := firstInputString(p.Input, "old_string")
		newString := firstInputString(p.Input, "new_string")

		if oldString != "" && newString != "" {
			// Edit tool
			oldTasks := len(reTaskAny.FindAllString(oldString, -1))
			newTasks := len(reTaskAny.FindAllString(newString, -1))
			tasksRemoved := oldTasks - newTasks
			if tasksRemoved > 0 {
				oldUnchecked := len(reTaskUnchecked.FindAllString(oldString, -1))
				newUnchecked := len(reTaskUnchecked.FindAllString(newString, -1))
				uncheckedRemoved := oldUnchecked - newUnchecked
				if uncheckedRemoved > 0 {
					log.Warn(i18n.Tf("audit.warn_task_deletion_edit", uncheckedRemoved, filepath.Base(path), oldTasks, oldUnchecked, newTasks, newUnchecked))
				}
			}
		} else if p.ToolName == "write" {
			// Write tool (full file replacement)
			newContent := firstInputString(p.Input, "content")
			if newContent != "" {
				newChecked := len(reTaskChecked.FindAllString(newContent, -1))
				newUnchecked := len(reTaskUnchecked.FindAllString(newContent, -1))
				newTotal := newChecked + newUnchecked
				tasksLost := currentTotal - newTotal
				if tasksLost > 0 {
					uncheckedLost := currentUnchecked - newUnchecked
					if uncheckedLost > 0 {
						log.Warn(i18n.Tf("audit.warn_task_deletion_write", tasksLost, filepath.Base(path), currentTotal, currentUnchecked, newTotal, newUnchecked, uncheckedLost))
					}
				}
			}
		}

		// Save snapshot
		state.WriteText(
			fmt.Sprintf("plan-tasks-%s", strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))),
			fmt.Sprintf("%d:%d:%d", currentTotal, currentChecked, currentUnchecked),
		)
	}
}

func rule10BlockDangerous(p *Payload, state *SessionState, log *Logger) {
	if p.ToolName != "bash" || p.Command == "" {
		return
	}

	// Block migrate:fresh
	if reMigrateFresh.MatchString(p.Command) {
		if !state.Exists("skill-read-squash-migrations") {
			log.Block(i18n.T("audit.block_migrate_fresh"))
		} else {
			log.Warn(i18n.T("audit.warn_migrate_fresh_allowed"))
		}
	}

	// Block destructive DB commands when env.deployed is true
	cfg, cfgFound := LoadBravrosConfig()
	if cfgFound && cfg.Env.Deployed {
		destructivePatterns := []struct {
			re   *regexp.Regexp
			name string
		}{
			{reMigrateRollback, "migrate:rollback"},
			{reDbWipe, "db:wipe"},
			{reDropTable, "DROP TABLE"},
			{reTruncateTable, "TRUNCATE TABLE"},
			{reDeleteFromNoWhere, "DELETE FROM (without WHERE)"},
		}
		for _, dp := range destructivePatterns {
			if dp.re.MatchString(p.Command) {
				log.Block(i18n.Tf("audit.block_destructive_db", dp.name))
			}
		}
		// Also block migrate:fresh when deployed, even if squash-migrations is active
		if reMigrateFresh.MatchString(p.Command) && !state.Exists("skill-read-squash-migrations") {
			// Already handled above — but if squash-migrations IS active on a deployed project, warn
			if state.Exists("skill-read-squash-migrations") {
				log.Warn(i18n.T("audit.warn_migrate_fresh_deployed"))
			}
		}
	}

	// Block direct push to main
	isExplicitMainPush := reGitPushMain.MatchString(p.Command)
	isBarePush := reBarePush.MatchString(p.Command)
	if isExplicitMainPush || isBarePush {
		cwd, _ := os.Getwd()
		if m := reCdPath.FindStringSubmatch(p.Command); m != nil {
			cwd = os.ExpandEnv(m[1])
			if strings.HasPrefix(cwd, "~") {
				home, _ := os.UserHomeDir()
				cwd = filepath.Join(home, cwd[1:])
			}
		}

		branchCmd := exec.Command("git", "branch", "--show-current")
		if cwd != "" {
			branchCmd.Dir = cwd
		}
		branchOut, _ := branchCmd.Output()
		branch := strings.TrimSpace(string(branchOut))

		cfg, _ := LoadBravrosConfig()
		stagingPattern := "*" + cfg.StagingBranch + "*"
		stagingCmd := exec.Command("git", "branch", "-a", "--list", stagingPattern)
		if cwd != "" {
			stagingCmd.Dir = cwd
		}
		stagingOut, _ := stagingCmd.Output()
		hasStaging := strings.TrimSpace(string(stagingOut)) != ""

		isMainBranch := branch == "main"
		if hasStaging && (isExplicitMainPush || isMainBranch) {
			log.Block(i18n.Tf("audit.block_push_main", cfg.StagingBranch))
		}
	}

	// Block AI signatures in commits
	if reGitCommit.MatchString(p.Command) && reAISig.MatchString(p.Command) {
		log.Block(i18n.T("audit.block_ai_sig_commit"))
	}

	// Block AI signatures in PR creation/editing
	if reGhPrCreateEdit.MatchString(p.Command) {
		for _, pattern := range aiSigPatternsPR {
			if pattern.MatchString(p.Command) {
				log.Block(i18n.Tf("audit.block_ai_sig_pr", pattern.String()))
			}
		}
	}

	// Block AI signatures in PR comments
	if reGhPrComment.MatchString(p.Command) {
		for _, pattern := range aiSigPatternsComment {
			if pattern.MatchString(p.Command) {
				log.Block(i18n.Tf("audit.block_ai_sig_comment", pattern.String()))
			}
		}
	}
}

func rule11ClaudeReviewPrompt(p *Payload, state *SessionState, log *Logger) {
	if p.ToolName != "bash" || p.Command == "" {
		return
	}
	if reGhPrComment.MatchString(p.Command) && reClaudeReview.MatchString(p.Command) {
		if !reCheckMerge.MatchString(p.Command) {
			log.Block(i18n.T("audit.block_review_too_short"))
		}
	}
}

func rule12PlanCheckSkip(p *Payload, state *SessionState, log *Logger) {
	if p.ToolName != "bash" || p.Command == "" {
		return
	}
	if !strings.Contains(p.Command, "gh pr create") && !reGhApiPulls.MatchString(p.Command) {
		return
	}

	checkpoints := state.ReadText("checkpoints.txt")
	hadPlanApproved := strings.Contains(checkpoints, "plan-approved:")
	hadPlanCheck := strings.Contains(checkpoints, "plan-check:")

	if hadPlanApproved && !hadPlanCheck {
		activeCmd := state.ReadText("active-command")
		if isSDLCFlow(activeCmd) {
			log.Block(i18n.T("audit.block_plan_check_skip"))
		} else {
			log.Warn(i18n.T("audit.warn_plan_check_skip"))
		}
	}
}

func rule13UncheckedAcceptance(p *Payload, state *SessionState, log *Logger) {
	if p.ToolName != "bash" || p.Command == "" {
		return
	}
	if !strings.Contains(p.Command, "gh pr create") && !reGhPrEdit.MatchString(p.Command) {
		return
	}

	matches, _ := filepath.Glob(".planning/*-todo.md")
	for _, pf := range matches {
		content, err := os.ReadFile(pf)
		if err != nil {
			continue
		}
		acMatch := reAcceptCriteria.FindSubmatch(content)
		if acMatch == nil {
			continue
		}
		acSection := string(acMatch[1])
		unchecked := reTaskUnchecked.FindAllString(acSection, -1)
		total := reTaskAny.FindAllString(acSection, -1)
		if len(unchecked) > 0 && len(total) > 0 {
			activeCmd := state.ReadText("active-command")
			if isSDLCFlow(activeCmd) {
				log.Block(i18n.Tf("audit.block_unchecked_acceptance", len(unchecked), len(total), filepath.Base(pf)))
			} else {
				log.Warn(i18n.Tf("audit.warn_unchecked_acceptance", len(unchecked), len(total), filepath.Base(pf)))
			}
		}
	}
}

func rule14FlowAutoEnforcement(p *Payload, state *SessionState, log *Logger) {
	if p.ToolName != "bash" || p.Command == "" {
		return
	}

	checkpoints := state.ReadText("checkpoints.txt")
	isAutoPr := strings.Contains(checkpoints, "auto-pr:")

	isPRCreation := strings.Contains(p.Command, "gh pr create") || reGhApiPulls.MatchString(p.Command)

	if isAutoPr && isPRCreation {
		hadStep5 := strings.Contains(checkpoints, "auto-pr:5")
		hadStep4 := strings.Contains(checkpoints, "auto-pr:4")

		if hadStep4 && !hadStep5 {
			log.Block(i18n.T("audit.block_autopr_skip_plancheck"))
		}
	}

	// Enforce review loop
	isPRComment := reGhPrComment.MatchString(p.Command) && strings.Contains(p.Command, "Final Report")
	if isAutoPr && isPRComment {
		hadStep7 := strings.Contains(checkpoints, "auto-pr:7")
		hadStep6 := strings.Contains(checkpoints, "auto-pr:6")
		if hadStep6 && !hadStep7 {
			log.Warn(i18n.T("audit.warn_autopr_skip_review"))
		}
	}
}

// isAutonomousPipeline returns true for contexts where the agent manages
// the full PR lifecycle (create, merge, push) autonomously. In these
// pipelines, merging PRs between rounds is a hard dependency — blocking
// it kills the pipeline with no workaround.
func isAutonomousPipeline(activeCmd string) bool {
	switch activeCmd {
	case "auto-merge", "auto-pr", "auto-pr-wt":
		return true
	}
	return false
}

func rule15BacklogCLI(p *Payload, state *SessionState, log *Logger) {
	if p.ToolName != "bash" || p.Command == "" {
		return
	}

	if reBacklogGrep.MatchString(p.Command) || reBacklogLoop.MatchString(p.Command) || reBacklogCat.MatchString(p.Command) {
		log.Block(i18n.T("audit.block_backlog_manual"))
	}
}

func rule16PlanningMvCheck(p *Payload, state *SessionState, log *Logger) {
	if p.ToolName != "bash" || p.Command == "" {
		return
	}
	if !strings.Contains(p.Command, ".planning/") {
		return
	}
	// Allow git mv
	if reGitMv.MatchString(p.Command) {
		return
	}
	// Block bare mv
	if reBareMv.MatchString(p.Command) {
		// Extract source path and check if it's git-tracked
		src := extractMvSource(p.Command)
		if src != "" && strings.Contains(src, ".planning/") {
			cmd := exec.Command("git", "ls-files", "--error-unmatch", src)
			if err := cmd.Run(); err != nil {
				// File is untracked — allow bare mv
				return
			}
		}
		log.Block(i18n.T("audit.block_planning_mv"))
	}
}

func rule17BlockMainMerge(p *Payload, state *SessionState, log *Logger) {
	if p.ToolName != "bash" || p.Command == "" {
		return
	}

	isMergeAttempt := reGhPrMerge.MatchString(p.Command) ||
		reGhApiMergePut.MatchString(p.Command) ||
		reGitPushRefspecMain.MatchString(p.Command)

	if !isMergeAttempt {
		return
	}

	// For gh pr merge / gh api merge: check the PR's actual base branch.
	// Only block if it targets main. Merges to other branches are always allowed.
	// gh pr view runs from the hook's inherited cwd (the project directory).
	if prNum := extractPRNumber(p.Command); prNum != "" {
		cwd := detectCwd(p.Command)
		log.Log("🔍 Checking PR #" + prNum + " base branch (cwd: " + cwd + ")")
		baseBranch := getPRBaseBranch(prNum, cwd)
		log.Log("🔍 PR #" + prNum + " base branch result: '" + baseBranch + "'")
		if baseBranch != "" && baseBranch != "main" {
			log.Log("✅ Merge allowed — PR #" + prNum + " targets " + baseBranch + " (not main)")
			return
		}
		// baseBranch == "main" or empty (gh failed) → fall through to skill context check
	} else {
		log.Log("🔍 No PR number extracted from command: " + p.Command)
	}

	if isAllowedMergeContext(state) {
		log.Log("✅ Merge to main allowed — inside " + state.ReadText("active-command") + " context")
		return
	}

	// Check if blocked due to autonomous mode specifically
	activeCmd := state.ReadText("active-command")
	if isAutonomousPipeline(activeCmd) || state.Exists("was-autonomous") {
		log.Block(i18n.T("audit.block_main_merge_autonomous"))
	}

	cfg, _ := LoadBravrosConfig()
	log.Block(i18n.Tf("audit.block_main_merge", cfg.StagingBranch))
}

// extractPRNumber extracts the PR number from gh pr merge or gh api merge commands.
func extractPRNumber(command string) string {
	if m := reGhPrMergeNum.FindStringSubmatch(command); len(m) > 1 {
		return m[1]
	}
	if m := reGhApiMergeNum.FindStringSubmatch(command); len(m) > 1 {
		return m[1]
	}
	return ""
}

// detectCwd extracts the working directory from a command containing `cd /path`.
func detectCwd(command string) string {
	cwd, _ := os.Getwd()
	if m := reCdPath.FindStringSubmatch(command); m != nil {
		cwd = os.ExpandEnv(m[1])
		if strings.HasPrefix(cwd, "~") {
			home, _ := os.UserHomeDir()
			cwd = filepath.Join(home, cwd[1:])
		}
	}
	return cwd
}

// getPRBaseBranch runs gh pr view to get the PR's base branch.
// Returns empty string on failure — caller falls through to isAllowedMergeContext,
// which still blocks outside allowed skill contexts (safe failure mode).
func getPRBaseBranch(prNum string, cwd string) string {
	cmd := exec.Command("gh", "pr", "view", prNum, "--json", "baseRefName", "-q", ".baseRefName")
	if cwd != "" {
		cmd.Dir = cwd
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// isAllowedMergeContext returns true when the session is inside a skill
// that is permitted to merge code into main.
func isAllowedMergeContext(state *SessionState) bool {
	activeCmd := state.ReadText("active-command")
	// CRITICAL: autonomous pipelines CANNOT merge to main.
	// They can merge to other branches (checked by caller via getPRBaseBranch).
	if isAutonomousPipeline(activeCmd) || state.Exists("was-autonomous") {
		return false
	}
	switch activeCmd {
	case "finish", "hotfix":
		return true
	}
	// Fallback: check if finish or hotfix skill was invoked.
	// Checkpoint-based active-command can be overwritten by nested skill calls
	// (e.g., /finish resumes after /address-pr, keeping address-pr as active-command).
	if state.Exists("skill-read-finish") || state.Exists("skill-read-hotfix") {
		return true
	}
	return false
}

func rule18ProjectConfig(p *Payload, state *SessionState, log *Logger) {
	// Only check once per session
	if state.Exists("bravros-config-prompted") {
		return
	}

	state.Touch("bravros-config-prompted")
	_, found := LoadBravrosConfig()
	if !found {
		log.Warn(i18n.T("audit.warn_no_config"))
	}
}

func rule19AgentModelEnforcement(p *Payload, state *SessionState, log *Logger) {
	if p.ToolName != "agent" {
		return
	}

	prompt := firstInputString(p.Input, "prompt")
	model := firstInputString(p.Input, "model")

	hasO := strings.Contains(prompt, "[O]")
	hasS := strings.Contains(prompt, "[S]")
	hasH := strings.Contains(prompt, "[H]")

	if !hasH && !hasS && !hasO {
		return
	}

	var required string
	switch {
	case hasO:
		required = "opus"
	case hasS:
		required = "sonnet"
	default:
		required = "haiku"
	}

	if model == "" {
		log.Block(i18n.Tf("audit.block_agent_no_model", markersFound(hasH, hasS, hasO), required, required))
	}

	if model != "" && model != required {
		log.Block(i18n.Tf("audit.block_agent_wrong_model", markersFound(hasH, hasS, hasO), required, model, model, required))
	}

	log.Log(fmt.Sprintf("✅ AGENT MODEL OK: markers=%s model=%s",
		markersFound(hasH, hasS, hasO), model))
}

// markersFound returns a comma-joined string of the markers present in the prompt.
func markersFound(h, s, o bool) string {
	var m []string
	if h {
		m = append(m, "[H]")
	}
	if s {
		m = append(m, "[S]")
	}
	if o {
		m = append(m, "[O]")
	}
	return strings.Join(m, ",")
}

func uniqueSkillNames() []string {
	seen := make(map[string]bool)
	var names []string
	for _, name := range skillMap {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

func rule20BashRedirectToPlanning(p *Payload, state *SessionState, log *Logger) {
	if p.ToolName != "bash" || p.Command == "" {
		return
	}
	if !reBashRedirectPlanning.MatchString(p.Command) {
		return
	}
	// Exclude backlog/ and reports/ subdirectories
	if strings.Contains(p.Command, ".planning/backlog/") || strings.Contains(p.Command, ".planning/reports/") || strings.Contains(p.Command, ".planning/user-reports/") {
		return
	}
	if !state.Exists("read-plan-template") {
		log.Block(i18n.T("audit.block_bash_redirect_planning"))
	}
}

func rule21AutoSkillGate(p *Payload, state *SessionState, log *Logger) {
	if p.ToolName != "skill" {
		return
	}
	skillName := firstInputString(p.Input, "skill")
	autoSkills := map[string]bool{"auto-pr": true, "auto-pr-wt": true, "auto-merge": true}
	if !autoSkills[skillName] {
		return
	}
	if _, err := os.Stat(".planning/.auto-pr-lock"); err == nil {
		// Lock exists — re-entry (resume after context compact). Allow.
		log.Log("✅ AUTO SKILL RE-ENTRY: " + skillName + " — lock file exists, resuming")
		return
	}
	// No lock — fresh invocation. Track it.
	log.Log("⚠️ AUTO SKILL INVOKED: " + skillName + " — no lock file found, first invocation")
	state.Touch("auto-skill-invoked-" + skillName)
}

func rule21bLockFileTamper(p *Payload, state *SessionState, log *Logger) {
	lockPath := ".auto-pr-lock"
	planningLock := ".planning/.auto-pr-lock"

	// Block Bash tampering
	if p.ToolName == "bash" && p.Command != "" {
		if (strings.Contains(p.Command, lockPath) || strings.Contains(p.Command, planningLock)) &&
			(strings.Contains(p.Command, "rm ") ||
				strings.Contains(p.Command, "> "+planningLock) ||
				strings.Contains(p.Command, "> "+lockPath) ||
				strings.Contains(p.Command, "cat /dev/null") ||
				strings.Contains(p.Command, "truncate")) {
			log.Block(i18n.T("audit.block_lock_tamper_bash"))
		}
	}

	// Block Write tool overwrite
	if p.IsWriteLike() {
		for _, path := range p.AllPaths() {
			if strings.HasSuffix(path, ".auto-pr-lock") {
				log.Block(i18n.T("audit.block_lock_tamper_write"))
			}
		}
	}
}

func rule21cTrackAutonomousMode(p *Payload, state *SessionState, log *Logger) {
	// Check lock file on EVERY tool call
	if _, err := os.Stat(".planning/.auto-pr-lock"); err == nil {
		if !state.Exists("was-autonomous") {
			state.Touch("was-autonomous")
			log.Log("🔒 AUTONOMOUS MODE DETECTED — .auto-pr-lock found. Main merge blocked for this session.")
		}
	}
}

func rule22ReadOnlySkillEnforcement(p *Payload, state *SessionState, log *Logger) {
	if !p.IsWriteLike() {
		return
	}

	activeCmd := state.ReadText("active-command")
	readOnlySkills := map[string]bool{"debug": true}

	if !readOnlySkills[activeCmd] {
		return
	}

	// Allow writes to .planning/
	for _, path := range p.AllPaths() {
		if strings.Contains(path, ".planning/") {
			continue
		}
		log.Block(i18n.T("audit.block_debug_readonly"))
		return
	}
}

func rule23BlockDeployedDirWrites(p *Payload, state *SessionState, log *Logger) {
	if !p.IsWriteLike() {
		return
	}

	homeDir, _ := os.UserHomeDir()
	deployedDir := filepath.Join(homeDir, ".claude")

	// Also exempt verify-install skill (needs to read+write ~/.claude/ for verification)
	activeCmd := state.ReadText("active-command")
	if activeCmd == "verify-install" {
		return
	}

	for _, path := range p.AllPaths() {
		absPath := path
		if !filepath.IsAbs(absPath) {
			absPath, _ = filepath.Abs(absPath)
		}
		if !strings.HasPrefix(absPath, deployedDir+"/") {
			continue
		}
		// Allow exceptions
		if strings.Contains(absPath, "/hooks/logs/") ||
			strings.HasSuffix(absPath, "/settings.json") ||
			strings.HasSuffix(absPath, "/bin/bravros") ||
			strings.Contains(absPath, "/projects/") {
			continue
		}
		portableRepo := detectPortableRepo()
		log.Block(i18n.Tf("audit.block_deployed_dir_write", portableRepo, portableRepo))
		return
	}
}
