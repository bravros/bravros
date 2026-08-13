package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bravros/bravros/cli/internal/autopr"
	"github.com/bravros/bravros/cli/internal/plan"
	"github.com/bravros/bravros/cli/internal/session"
	"github.com/spf13/cobra"
)

const lockFilePath = ".planning/.auto-pr-lock"

// lockNameRE matches the auto-<skill>-lock naming convention that audit rules
// glob-match (`.planning/.auto-*-lock`) and `list-locks` enumerates. A --lock
// flag value that fails this pattern would produce an orphan file that no rule
// detects, so we fail fast at the set-lock / clear-lock / status / force-clear
// boundary instead of silently creating drift.
var lockNameRE = regexp.MustCompile(`^auto-[a-z0-9]+(-[a-z0-9]+)*-lock$`)

// validateLockName enforces the auto-<skill>-lock convention on --lock flag
// values. Empty string is accepted — it means "use the default .auto-pr-lock".
func validateLockName(name string) error {
	if name == "" {
		return nil
	}
	if !lockNameRE.MatchString(name) {
		return fmt.Errorf(
			"invalid --lock value %q: expected format auto-<skill>-lock (e.g., auto-merge-lock, auto-pr-lock) — lowercase letters/digits separated by single hyphens, must start with \"auto-\" and end with \"-lock\"",
			name,
		)
	}
	return nil
}

var autoprSkillFlag string
var autoprFieldFlag string
var autoprStaleAfter int
var autoprLockFlag string
var autoprModeFlag string

var autoprCmd = &cobra.Command{
	Use:   "autopr",
	Short: "Manage autonomous pipeline lock",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

// resolveLockPath returns the lock file path for the given lock name flag.
// When lockName is empty, returns the default lockFilePath (.planning/.auto-pr-lock).
// When lockName is provided, constructs .planning/.<lockName>.
//
// `filepath.Base` is applied defensively to strip any path separators that
// slipped past `validateLockName` (which rejects "/" via its regex). This is
// belt-and-suspenders: callers that bypass validation still cannot escape
// `.planning/` via a crafted `--lock ../evil` value.
func resolveLockPath(lockName string) string {
	if lockName == "" {
		return lockFilePath
	}
	return filepath.Join(".planning", "."+filepath.Base(lockName))
}

// getTTY returns the current TTY device path, falling back to empty string.
func getTTY() string {
	tty := os.Getenv("TTY")
	if tty != "" {
		return tty
	}
	out, err := exec.Command("tty").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// autoprSkipProcessWalk disables parent process chain inspection during tests.
// Set to true in tests to avoid false positives when running inside Claude Code.
var autoprSkipProcessWalk bool

// currentBranch returns the current git branch name via git rev-parse.
// Returns "(detached)" on detached HEAD, or "(unknown)" on error.
// Uses the same exec.Command pattern as the rest of autopr.go.
func currentBranch() string {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "(unknown)"
	}
	branch := strings.TrimSpace(string(out))
	if branch == "HEAD" {
		return "(detached)"
	}
	return branch
}

// absPath resolves p to an absolute path; returns p unchanged on error.
func absPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// isInsideClaudeEnv returns true if any environment indicator suggests we're
// running inside a Claude Code session.
//
// Strong signals (definitive — block immediately):
//   - session.Resolve() != "" — covers CLAUDE_CODE_SESSION_ID (canonical, v2.1.132+),
//     CLAUDE_SESSION_ID (legacy fallback), and other agent runtimes (Codex, etc.)
//   - CLAUDE_CODE_ENTRYPOINT: set by Claude Code runtime
//
// Weak signals (common in dev setups — only block when combined with process walk):
//   - TMUX / STY: could be a regular tmux/screen session outside Claude
//
// Process walk: checks parent chain for "claude" process name (up to 5 levels).
func isInsideClaudeEnv() bool {
	// Strong signals — definitive
	if session.Resolve() != "" {
		return true
	}
	if os.Getenv("CLAUDE_CODE_ENTRYPOINT") != "" {
		return true
	}
	if autoprSkipProcessWalk {
		return false
	}
	// Walk up process chain (max 5 levels) looking for "claude" in process name.
	pid := os.Getppid()
	for i := 0; i < 5 && pid > 1; i++ {
		out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			break
		}
		comm := strings.TrimSpace(string(out))
		if strings.Contains(strings.ToLower(comm), "claude") {
			return true
		}
		// Get parent of current pid
		ppOut, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			break
		}
		ppid, err := strconv.Atoi(strings.TrimSpace(string(ppOut)))
		if err != nil || ppid <= 1 {
			break
		}
		pid = ppid
	}
	return false
}

var autoprSetLockCmd = &cobra.Command{
	Use:   "set-lock",
	Short: "Create autonomous pipeline lock",
	Run: func(cmd *cobra.Command, args []string) {
		if err := validateLockName(autoprLockFlag); err != nil {
			fmt.Fprintln(os.Stderr, "error: "+err.Error())
			os.Exit(1)
		}
		if _, err := os.Stat(".planning"); os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "error: .planning/ directory not found — run from project root")
			os.Exit(1)
		}
		targetPath := resolveLockPath(autoprLockFlag)
		mode := autoprModeFlag
		if mode == "" {
			mode = "single"
		}
		tty := getTTY()
		sessionID := session.Resolve()
		pid := strconv.Itoa(os.Getpid())
		content := fmt.Sprintf(
			"timestamp=%s\nskill=%s\nmode=%s\ntty=%s\nsession_id=%s\npid=%s\n",
			time.Now().UTC().Format(time.RFC3339),
			autoprSkillFlag,
			mode,
			tty,
			sessionID,
			pid,
		)
		if err := os.WriteFile(targetPath, []byte(content), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✅ autonomous lock set (skill: %s, lock: %s, mode: %s, worktree: %s)\n", autoprSkillFlag, absPath(targetPath), mode, currentBranch())
	},
}

var autoprForceFlag bool
var autoprAllFlag bool

// globOtherLocks returns all .auto-*-lock files found under .planning/ excluding targetPath.
func globOtherLocks(targetPath string) []string {
	matches, err := filepath.Glob(".planning/.auto-*-lock")
	if err != nil {
		return nil
	}
	var others []string
	for _, m := range matches {
		if m != targetPath {
			others = append(others, m)
		}
	}
	return others
}

// lockInfoString returns a human-friendly info string for a lock file,
// e.g. "(mode=batch, age=14m0s, session=0eeae1df)". Empty fields are omitted;
// the session ID is truncated to 8 characters.
func lockInfoString(path string) string {
	m, err := autopr.ParseLockMeta(path)
	if err != nil {
		return "(unknown)"
	}
	var parts []string
	if m.Mode != "" {
		parts = append(parts, "mode="+m.Mode)
	}
	if m.Timestamp.IsZero() {
		parts = append(parts, "age=unknown")
	} else {
		parts = append(parts, "age="+m.Age().Round(time.Second).String())
	}
	if m.SessionID != "" {
		sid := m.SessionID
		if len(sid) > 8 {
			sid = sid[:8]
		}
		parts = append(parts, "session="+sid)
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// lockBaseName derives the --lock flag value from a lock file path:
// ".planning/.auto-merge-lock" → "auto-merge-lock".
func lockBaseName(path string) string {
	return strings.TrimPrefix(filepath.Base(path), ".")
}

var autoprClearLockCmd = &cobra.Command{
	Use:   "clear-lock",
	Short: "Remove autonomous pipeline lock (outside Claude Code only)",
	Long: `Remove the autonomous pipeline lock.

By default targets .planning/.auto-pr-lock. Use --lock to target another lock file.

This command always refuses to run from inside Claude Code (or any agent session).
Lock presence is what denies autonomous merges to main, so clearing it is a safety
gate in the same class as the promote token: it must be a deliberate human action
from a separate terminal. There is no self-session exemption, and --force does not
override the refusal.

Use --all to remove all .planning/.auto-*-lock files in one command.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := validateLockName(autoprLockFlag); err != nil {
			fmt.Fprintln(os.Stderr, "error: "+err.Error())
			os.Exit(1)
		}

		// Safety gate: refuse on EVERY path — including --force and --all —
		// when running inside Claude Code. Clearing the lock is precisely what
		// re-enables an autonomous merge to main (audit rules 17/21b gate on
		// lock presence), so the clear must come from a separate-terminal
		// human action. B-0163's self-session exemption was reverted (P-0183 G3).
		if isInsideClaudeEnv() {
			fmt.Fprintln(os.Stderr, "This command must be run outside Claude Code. Open a fresh terminal (outside Claude Code, same folder) and try again.")
			os.Exit(1)
		}

		// --all mode: delete every .auto-*-lock file.
		if autoprAllFlag {
			matches, globErr := filepath.Glob(".planning/.auto-*-lock")
			if globErr != nil || len(matches) == 0 {
				fmt.Println("ℹ️ no lock files found (no-op)")
				return
			}
			branch := currentBranch()
			cleared := 0
			for _, m := range matches {
				if rmErr := os.Remove(m); rmErr == nil {
					fmt.Printf("✅ cleared %s (worktree: %s)\n", absPath(m), branch)
					cleared++
				} else {
					fmt.Fprintf(os.Stderr, "warning: could not remove %s: %v\n", m, rmErr)
				}
			}
			fmt.Printf("✅ %d lock file(s) cleared (worktree: %s)\n", cleared, branch)
			return
		}

		targetPath := resolveLockPath(autoprLockFlag)

		removeErr := os.Remove(targetPath)
		if removeErr != nil {
			if os.IsNotExist(removeErr) {
				// No-op: check for other lock files and warn if found.
				others := globOtherLocks(targetPath)
				switch {
				case len(others) == 1:
					fmt.Printf("ℹ️ lock was not present (no-op) — but another lock file found:\n")
					fmt.Printf("  %s  %s\n", others[0], lockInfoString(others[0]))
					fmt.Printf("  Clear with: bravros autopr clear-lock --lock %s\n", lockBaseName(others[0]))
				case len(others) > 1:
					fmt.Printf("ℹ️ lock was not present (no-op) — but other lock file(s) found:\n")
					for _, o := range others {
						fmt.Printf("  %s  %s\n", o, lockInfoString(o))
					}
					fmt.Println("  Clear all with: bravros autopr clear-lock --all")
				default:
					fmt.Println("ℹ️ lock was not present (no-op)")
				}
			} else {
				fmt.Fprintf(os.Stderr, "error: %v\n", removeErr)
				os.Exit(1)
			}
		} else {
			fmt.Printf("✅ autonomous lock cleared (worktree: %s, path: %s)\n", currentBranch(), absPath(targetPath))
		}

		// Auto-clear hook: when clearing an auto-merge-lock, also remove the
		// batch progress file so the next batch run starts with a clean slate.
		mode := autoprModeFlag
		lockName := autoprLockFlag
		if lockName == "" {
			lockName = "auto-pr-lock"
		}
		if mode == "auto-merge" || lockName == "auto-merge-lock" {
			batchClearedOK := false
			if clearErr := plan.ClearBatchProgress(); clearErr != nil {
				// Non-fatal: report but don't exit 1 — the lock itself was cleared.
				fmt.Fprintf(os.Stderr, "warning: could not clear batch progress file: %v\n", clearErr)
			} else {
				batchClearedOK = true
			}
			// B-0143: also remove the batch-decisions cache so the next batch
			// run starts with a fully clean slate (best-effort; ignore-if-missing).
			decisionsRemoved := false
			if err := os.Remove(".planning/.batch-decisions.json"); err == nil {
				decisionsRemoved = true
			}
			switch {
			case batchClearedOK && decisionsRemoved:
				fmt.Println("ℹ️ batch progress + batch-decisions cleared (auto-merge-lock)")
			case batchClearedOK:
				fmt.Println("ℹ️ batch progress cleared (auto-merge-lock)")
			case decisionsRemoved:
				fmt.Println("ℹ️ batch-decisions cleared (auto-merge-lock)")
			}
		}
	},
}

type lockStatus struct {
	Present    bool   `json:"present"`
	AgeSeconds int    `json:"age_seconds"`
	Skill      string `json:"skill"`
	Mode       string `json:"mode"`
	TTY        string `json:"tty"`
	SessionID  string `json:"session_id"`
	PID        string `json:"pid"`
	Timestamp  string `json:"timestamp"`
	Path       string `json:"path"`
}

var autoprStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Report autonomous lock status (JSON)",
	Run: func(cmd *cobra.Command, args []string) {
		if err := validateLockName(autoprLockFlag); err != nil {
			fmt.Fprintln(os.Stderr, "error: "+err.Error())
			os.Exit(1)
		}
		targetPath := resolveLockPath(autoprLockFlag)
		st := lockStatus{Path: targetPath}
		meta, err := autopr.ParseLockMeta(targetPath)
		// st.Present requires both a readable file AND a parseable timestamp,
		// matching the old parseLockFile semantics where a malformed timestamp
		// suppressed Present even though metadata fields were still populated.
		if err == nil && !meta.Timestamp.IsZero() {
			st.Present = true
			st.AgeSeconds = int(meta.Age().Seconds())
			st.Timestamp = meta.Timestamp.UTC().Format(time.RFC3339)
		}
		st.TTY = meta.TTY
		st.SessionID = meta.SessionID
		st.PID = meta.PID
		st.Mode = meta.Mode
		st.Skill = meta.Skill

		if autoprFieldFlag != "" {
			switch autoprFieldFlag {
			case "present":
				fmt.Printf("%v\n", st.Present)
			case "age_seconds":
				fmt.Printf("%d\n", st.AgeSeconds)
			case "skill":
				fmt.Printf("%s\n", st.Skill)
			case "mode":
				fmt.Printf("%s\n", st.Mode)
			case "tty":
				fmt.Printf("%s\n", st.TTY)
			case "session_id":
				fmt.Printf("%s\n", st.SessionID)
			case "pid":
				fmt.Printf("%s\n", st.PID)
			case "timestamp":
				fmt.Printf("%s\n", st.Timestamp)
			case "path":
				fmt.Printf("%s\n", st.Path)
			default:
				fmt.Fprintf(os.Stderr, "unknown field: %s\n", autoprFieldFlag)
				os.Exit(1)
			}
			return
		}
		b, _ := json.Marshal(st)
		fmt.Println(string(b))
	},
}

var autoprForceClearCmd = &cobra.Command{
	Use:    "force-clear",
	Short:  "Clear lock if older than --stale-after seconds (crash recovery)",
	Hidden: true,
	Run: func(cmd *cobra.Command, args []string) {
		if err := validateLockName(autoprLockFlag); err != nil {
			fmt.Fprintln(os.Stderr, "error: "+err.Error())
			os.Exit(1)
		}
		targetPath := resolveLockPath(autoprLockFlag)
		meta, err := autopr.ParseLockMeta(targetPath)
		if err != nil {
			fmt.Println("ℹ️ no lock present")
			return
		}
		age := int(meta.Age().Seconds())
		if age < autoprStaleAfter {
			fmt.Printf("ℹ️ lock is fresh (age: %ds < %ds threshold) — not cleared\n", age, autoprStaleAfter)
			return
		}
		if err := os.Remove(targetPath); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✅ stale lock cleared (age: %ds >= %ds threshold, worktree: %s, path: %s)\n", age, autoprStaleAfter, currentBranch(), absPath(targetPath))
	},
}

// lockInfo represents a single lock entry returned by list-locks.
type lockInfo struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	AgeSeconds int    `json:"age_seconds"`
	Skill      string `json:"skill"`
	Mode       string `json:"mode"`
	Timestamp  string `json:"timestamp"`
}

var autoprListLocksCmd = &cobra.Command{
	Use:   "list-locks",
	Short: "List all active autonomous pipeline locks (JSON array)",
	Run: func(cmd *cobra.Command, args []string) {
		matches, err := filepath.Glob(".planning/.auto-*-lock")
		if err != nil || len(matches) == 0 {
			fmt.Println("[]")
			return
		}
		locks := make([]lockInfo, 0, len(matches))
		for _, path := range matches {
			meta, _ := autopr.ParseLockMeta(path)
			info := lockInfo{
				Name:  filepath.Base(path)[1:], // strip leading dot
				Path:  path,
				Skill: meta.Skill,
				Mode:  meta.Mode,
			}
			if !meta.Timestamp.IsZero() {
				info.AgeSeconds = int(meta.Age().Seconds())
				info.Timestamp = meta.Timestamp.UTC().Format(time.RFC3339)
			}
			locks = append(locks, info)
		}
		b, _ := json.Marshal(locks)
		fmt.Println(string(b))
	},
}

// --- lock-status subcommand ---

var autoprLockStatusAllWorktrees bool

// worktreeEntry holds parsed data for one git worktree.
type worktreeEntry struct {
	Path   string
	Branch string // "" when detached
}

// parseWorktreeList parses the output of `git worktree list --porcelain`.
// Each worktree block is a sequence of "key value" lines separated by blank lines.
// Keys used: "worktree" (path), "branch" (refs/heads/<name>), "detached" (bare flag).
func parseWorktreeList(output string) []worktreeEntry {
	var entries []worktreeEntry
	var cur worktreeEntry
	inBlock := false

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			// Blank line = end of current block.
			if inBlock {
				entries = append(entries, cur)
				cur = worktreeEntry{}
				inBlock = false
			}
			continue
		}
		inBlock = true
		if strings.HasPrefix(line, "worktree ") {
			cur.Path = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "branch ") {
			// Branch is stored as "refs/heads/<name>"; strip the prefix.
			ref := strings.TrimPrefix(line, "branch ")
			cur.Branch = strings.TrimPrefix(ref, "refs/heads/")
		}
		// "detached" / "bare" lines are noted but we leave Branch as "".
	}
	// Flush last block if file doesn't end with a blank line.
	if inBlock {
		entries = append(entries, cur)
	}
	return entries
}

var autoprLockStatusCmd = &cobra.Command{
	Use:   "lock-status",
	Short: "Show lock status for the current worktree (or all worktrees with --all-worktrees)",
	Long: `Show autonomous pipeline lock status.

Without flags: lists all .planning/.auto-*-lock files in the current worktree.

With --all-worktrees: walks all linked worktrees via "git worktree list --porcelain"
and reports any .planning/.auto-*-lock found in each worktree, with branch name and age.
This is read-only and does not change any lock files.`,
	Run: func(cmd *cobra.Command, args []string) {
		if !autoprLockStatusAllWorktrees {
			// Single-worktree mode: same as list-locks but human-readable.
			matches, err := filepath.Glob(".planning/.auto-*-lock")
			if err != nil || len(matches) == 0 {
				fmt.Println("no locks present in current worktree")
				return
			}
			branch := currentBranch()
			cwd, _ := os.Getwd()
			fmt.Printf("worktree: %s (branch: %s)\n", cwd, branch)
			for _, m := range matches {
				fmt.Printf("  %s  %s\n", absPath(m), lockInfoString(m))
			}
			return
		}

		// --all-worktrees mode: walk git worktrees.
		out, err := exec.Command("git", "worktree", "list", "--porcelain").Output()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: git worktree list failed: %v\n", err)
			os.Exit(1)
		}

		entries := parseWorktreeList(string(out))
		if len(entries) == 0 {
			fmt.Println("no worktrees found")
			return
		}

		anyFound := false
		for _, wt := range entries {
			pattern := filepath.Join(wt.Path, ".planning", ".auto-*-lock")
			locks, globErr := filepath.Glob(pattern)
			if globErr != nil || len(locks) == 0 {
				continue
			}
			anyFound = true
			branch := wt.Branch
			if branch == "" {
				branch = "(detached)"
			}
			fmt.Printf("worktree: %s (branch: %s)\n", wt.Path, branch)
			for _, lk := range locks {
				fmt.Printf("  %s  %s\n", lk, lockInfoString(lk))
			}
		}
		if !anyFound {
			fmt.Println("no locks found across all worktrees")
		}
	},
}

// --- mode subcommand ---

var autoprModePrintLock bool
var autoprIsAutonomousFlag bool

var autoprModeCmd = &cobra.Command{
	Use:   "mode",
	Short: "Query autonomous pipeline mode",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

// autoprModeIsAutonomousCmd implements `bravros autopr mode --is-autonomous`.
// It glob-checks .planning/.auto-*-lock (not just .auto-pr-lock) to detect
// ANY active autonomous lock. This fixes B-0081: /auto-pr-wt previously only
// checked .auto-pr-lock, missing .auto-merge-lock and any future .auto-*-lock.
var autoprModeIsAutonomousCmd = &cobra.Command{
	Use:   "is-autonomous",
	Short: "Exit 0 if any .auto-*-lock present (B-0081: glob-match all lock types)",
	Long: `Checks .planning/.auto-*-lock via glob — matches all autonomous pipeline
lock files (.auto-pr-lock, .auto-merge-lock, and any future .auto-*-lock).

Exit 0:  at least one lock present (in autonomous mode).
Exit 1:  no locks found (not in autonomous mode).

Use --print-lock to emit the matched filename(s) to stdout for diagnostics.`,
	Run: func(cmd *cobra.Command, args []string) {
		matches, err := filepath.Glob(".planning/.auto-*-lock")
		if err != nil || len(matches) == 0 {
			if autoprModePrintLock {
				// no output
			}
			os.Exit(1)
		}
		if autoprModePrintLock {
			for _, m := range matches {
				fmt.Println(m)
			}
		}
		os.Exit(0)
	},
}

// --- preflight subcommand ---

var autoprPreflightSkill string
var autoprPreflightStaleAfter int

// autoprPreflightCmd implements `bravros autopr preflight --skill <name>`.
// It is the DRY replacement for the Step 0 lock setup block duplicated across
// auto-pr, auto-pr-wt, auto-merge, and flow skill files (B-0080).
//
// Behavior:
//  1. Clear any stale lock for this skill (default: >=6h old).
//  2. Acquire a fresh lock at .planning/.auto-<skill>-lock.
//  3. Emit a structured checkpoint echo.
//
// Exit 1 if a non-stale lock exists for a different skill (contention).
var autoprPreflightCmd = &cobra.Command{
	Use:   "preflight",
	Short: "Crash-recovery + lock acquire for autonomous pipeline (B-0080)",
	Long: `Step 0 preflight for autonomous skills (auto-pr, auto-pr-wt, auto-merge, flow).

1. Clears stale lock (age >= --stale-after seconds, default 21600 = 6h).
2. Acquires fresh lock at .planning/.auto-<skill>-lock.
3. Emits structured checkpoint: 🤖 [<skill>:0/N] preflight ok

Exit 1 if a non-stale lock already exists for a DIFFERENT skill (contention).
Use --stale-after 0 to force-clear any existing lock regardless of age.`,
	Run: func(cmd *cobra.Command, args []string) {
		if autoprPreflightSkill == "" {
			fmt.Fprintln(os.Stderr, "error: --skill is required")
			os.Exit(1)
		}
		skillLockName := "auto-" + autoprPreflightSkill + "-lock"
		if err := validateLockName(skillLockName); err != nil {
			fmt.Fprintln(os.Stderr, "error: invalid --skill value: "+err.Error())
			os.Exit(1)
		}

		if _, err := os.Stat(".planning"); os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "error: .planning/ directory not found — run from project root")
			os.Exit(1)
		}

		targetPath := filepath.Join(".planning", "."+skillLockName)

		// Step 1: check if a lock exists.
		meta, err := autopr.ParseLockMeta(targetPath)
		if err == nil {
			// Lock exists. Check staleness.
			age := int(meta.Age().Seconds())
			existingSkill := meta.Skill
			staleThreshold := autoprPreflightStaleAfter
			if staleThreshold < 0 {
				staleThreshold = 21600
			}
			if age >= staleThreshold {
				// Stale — clear it.
				os.Remove(targetPath)
				fmt.Printf("ℹ️ stale lock cleared (age: %ds >= %ds threshold)\n", age, staleThreshold)
			} else if existingSkill != "" && existingSkill != autoprPreflightSkill {
				// Non-stale lock for a different skill — contention.
				fmt.Fprintf(os.Stderr, "error: non-stale lock held by skill %q (age: %ds) — contention\n", existingSkill, age)
				os.Exit(1)
			} else {
				// Non-stale lock for same (or untagged) skill — clear and re-acquire.
				os.Remove(targetPath)
				fmt.Printf("ℹ️ refreshing lock for skill %q (age: %ds)\n", autoprPreflightSkill, age)
			}
		}

		// Step 2: acquire fresh lock.
		tty := getTTY()
		sessionID := session.Resolve()
		pid := strconv.Itoa(os.Getpid())
		content := fmt.Sprintf(
			"timestamp=%s\nskill=%s\nmode=single\ntty=%s\nsession_id=%s\npid=%s\n",
			time.Now().UTC().Format(time.RFC3339),
			autoprPreflightSkill,
			tty,
			sessionID,
			pid,
		)
		if err := os.WriteFile(targetPath, []byte(content), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		// Step 3: emit checkpoint.
		fmt.Printf("🤖 [%s:0/7] preflight ok (lock: %s)\n", autoprPreflightSkill, targetPath)
	},
}

// --- batch-status subcommand group ---

var batchStatusRoundFlag int
var batchStatusPlanFlag string
var batchStatusStateFlag string
var batchStatusFormatFlag string

var autoprBatchStatusCmd = &cobra.Command{
	Use:   "batch-status",
	Short: "Read or write batch pipeline progress (.planning/.batch-progress.json)",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var autoprBatchStatusWriteCmd = &cobra.Command{
	Use:   "write",
	Short: "Record the state of a plan in the current batch round",
	Run: func(cmd *cobra.Command, args []string) {
		if batchStatusRoundFlag <= 0 {
			fmt.Fprintln(os.Stderr, "error: --round must be a positive integer")
			os.Exit(1)
		}
		if batchStatusPlanFlag == "" {
			fmt.Fprintln(os.Stderr, "error: --plan is required")
			os.Exit(1)
		}
		if batchStatusStateFlag == "" {
			fmt.Fprintln(os.Stderr, "error: --state is required")
			os.Exit(1)
		}
		if err := plan.WriteBatchProgress(batchStatusRoundFlag, batchStatusPlanFlag, batchStatusStateFlag); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			fmt.Fprintf(os.Stderr, "valid states: %s\n", strings.Join(plan.ValidStates, ", "))
			os.Exit(1)
		}
		fmt.Printf("✅ batch progress: round=%d plan=%s state=%s\n", batchStatusRoundFlag, batchStatusPlanFlag, batchStatusStateFlag)
	},
}

var autoprBatchStatusReadCmd = &cobra.Command{
	Use:   "read",
	Short: "Show the current batch pipeline progress",
	Run: func(cmd *cobra.Command, args []string) {
		bp, err := plan.ReadBatchProgress()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		format := batchStatusFormatFlag
		if format == "" {
			format = "text"
		}

		switch format {
		case "json":
			b, _ := json.MarshalIndent(bp, "", "  ")
			fmt.Println(string(b))
		case "text":
			if len(bp.Rounds) == 0 {
				fmt.Println("no batch progress recorded")
				return
			}
			for _, rs := range bp.Rounds {
				fmt.Printf("Round %d:\n", rs.Round)
				for _, pe := range rs.Plans {
					fmt.Printf("  %-20s  %s  (updated: %s)\n", pe.Plan, pe.State, pe.UpdatedAt)
				}
			}
		default:
			fmt.Fprintf(os.Stderr, "error: unknown --format %q: valid values are json, text\n", format)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(autoprCmd)
	autoprCmd.AddCommand(autoprSetLockCmd)
	autoprCmd.AddCommand(autoprClearLockCmd)
	autoprCmd.AddCommand(autoprStatusCmd)
	autoprCmd.AddCommand(autoprForceClearCmd)
	autoprCmd.AddCommand(autoprListLocksCmd)
	autoprCmd.AddCommand(autoprLockStatusCmd)
	autoprLockStatusCmd.Flags().BoolVar(&autoprLockStatusAllWorktrees, "all-worktrees", false, "Walk all linked worktrees and report any active locks")
	autoprCmd.AddCommand(autoprBatchStatusCmd)
	autoprBatchStatusCmd.AddCommand(autoprBatchStatusWriteCmd)
	autoprBatchStatusCmd.AddCommand(autoprBatchStatusReadCmd)

	// mode subcommand
	autoprCmd.AddCommand(autoprModeCmd)
	autoprModeCmd.AddCommand(autoprModeIsAutonomousCmd)
	autoprModeIsAutonomousCmd.Flags().BoolVar(&autoprModePrintLock, "print-lock", false, "Print matched lock filename(s) to stdout")

	// preflight subcommand
	autoprCmd.AddCommand(autoprPreflightCmd)
	autoprPreflightCmd.Flags().StringVar(&autoprPreflightSkill, "skill", "", "Skill name (required): auto-pr, auto-pr-wt, auto-merge, flow")
	autoprPreflightCmd.Flags().IntVar(&autoprPreflightStaleAfter, "stale-after-seconds", 21600, "Age threshold in seconds before lock is considered stale (default: 21600 = 6h)")

	autoprSetLockCmd.Flags().StringVar(&autoprSkillFlag, "skill", "", "Skill name setting the lock")
	autoprSetLockCmd.Flags().StringVar(&autoprLockFlag, "lock", "", "Lock file base name (e.g. auto-merge-lock); defaults to auto-pr-lock")
	autoprSetLockCmd.Flags().StringVar(&autoprModeFlag, "mode", "single", "Lock mode: batch (holds across plans) or single (clears on merge)")
	autoprClearLockCmd.Flags().StringVar(&autoprLockFlag, "lock", "", "Lock file base name to clear; defaults to auto-pr-lock")
	autoprStatusCmd.Flags().StringVar(&autoprFieldFlag, "field", "", "Print only this field (present, age_seconds, skill, mode, tty, session_id, pid, timestamp, path)")
	autoprStatusCmd.Flags().StringVar(&autoprLockFlag, "lock", "", "Lock file base name to query; defaults to auto-pr-lock")
	autoprForceClearCmd.Flags().IntVar(&autoprStaleAfter, "stale-after", 21600, "Age threshold in seconds (default: 21600 = 6h)")
	autoprForceClearCmd.Flags().StringVar(&autoprLockFlag, "lock", "", "Lock file base name to force-clear; defaults to auto-pr-lock")
	autoprClearLockCmd.Flags().BoolVar(&autoprForceFlag, "force", false, "Deprecated: no effect — clear-lock always refuses inside Claude Code")
	_ = autoprClearLockCmd.Flags().MarkDeprecated("force", "clear-lock always refuses inside Claude Code; run from a separate terminal")
	autoprClearLockCmd.Flags().BoolVar(&autoprAllFlag, "all", false, "Remove all .planning/.auto-*-lock files")
	autoprClearLockCmd.Flags().StringVar(&autoprModeFlag, "mode", "", "Lock mode — when 'auto-merge', also clears .planning/.batch-progress.json")

	autoprBatchStatusWriteCmd.Flags().IntVar(&batchStatusRoundFlag, "round", 0, "Batch round number (required, positive integer)")
	autoprBatchStatusWriteCmd.Flags().StringVar(&batchStatusPlanFlag, "plan", "", "Plan ID (required)")
	autoprBatchStatusWriteCmd.Flags().StringVar(&batchStatusStateFlag, "state", "", "Plan state (required): "+strings.Join(plan.ValidStates, ", "))
	autoprBatchStatusReadCmd.Flags().StringVar(&batchStatusFormatFlag, "format", "text", "Output format: text or json")
}
