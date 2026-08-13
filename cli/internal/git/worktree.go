package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bravros/bravros/cli/internal/config"
)

// WorktreeOpts configures the worktree setup operation.
type WorktreeOpts struct {
	NoRebase   bool
	BaseBranch string // auto-detected if empty
}

// WorktreeSetupResult holds the result of a worktree setup operation.
type WorktreeSetupResult struct {
	Path    string `json:"path"`
	Branch  string `json:"branch"`
	Created bool   `json:"created"`
	Error   string `json:"error,omitempty"`
}

// CleanupOpts configures the worktree cleanup operation.
type CleanupOpts struct {
	Force        bool // also bypasses the merge-checked destroy guard below
	DeleteRemote bool
	DryRun       bool   // print the teardown scope and exit without mutating anything
	BaseBranch   string // base branch to verify merge status against; auto-detected (staging_branch / "main") if empty
}

// WorktreeCleanupResult holds the result of a worktree cleanup operation.
type WorktreeCleanupResult struct {
	Path          string   `json:"path"`
	Branch        string   `json:"branch,omitempty"`
	Removed       bool     `json:"removed"`
	BranchDeleted bool     `json:"branch_deleted"`
	DryRun        bool     `json:"dry_run,omitempty"`
	Scope         []string `json:"scope,omitempty"` // dry-run only: labelled teardown scope
	// Liveness guard fields: live_processes is always emitted (empty slice
	// when nothing is alive) so --dry-run and --force output always carry the
	// report; liveness is "unknown" when the check degraded (missing
	// lsof/timeout) and cleanup proceeded on a warn basis.
	LiveProcesses []LiveProcess `json:"live_processes"`
	Liveness      string        `json:"liveness,omitempty"`
	LivenessNote  string        `json:"liveness_note,omitempty"` // "you are inside this worktree" when the calling process itself is inside
	Error         string        `json:"error,omitempty"`
}

// resolveDefaultBaseBranch returns explicit (with any "origin/" prefix
// stripped) if non-empty, else .kaisser.yml's staging_branch, else "main".
// Shared by WorktreeSetup (rebase target) and WorktreeCleanup (merge-checked
// destroy guard) so both derive the base branch the same way.
func resolveDefaultBaseBranch(explicit string) string {
	if b := strings.TrimPrefix(explicit, "origin/"); b != "" {
		return b
	}
	cfg, _ := config.LoadBravrosConfig()
	if cfg != nil && cfg.StagingBranch != "" {
		return cfg.StagingBranch
	}
	return "main"
}

// numericWorktreeIDRegex matches an identifier that is ENTIRELY a (possibly
// zero-padded, possibly "P-"/"p-"-prefixed) integer — e.g. "P-0109", "0109",
// "109". Anything else (arbitrary slugs, or structured branch names like
// "feat/0109-desc" that contain a "/") does not match and passes through
// NormalizeWorktreeIdentifier unchanged.
var numericWorktreeIDRegex = regexp.MustCompile(`(?i)^p?-?0*([0-9]+)$`)

// NormalizeWorktreeIdentifier collapses purely-numeric worktree identifiers
// — "P-0109", "0109", "109" — down to the bare integer ("109") so the same
// logical plan/ID always produces the same worktree dir + branch name
// regardless of which form the caller typed. Arbitrary slugs and full branch
// paths (anything containing non-numeric structure) are returned unchanged.
func NormalizeWorktreeIdentifier(input string) string {
	trimmed := strings.TrimSpace(input)
	m := numericWorktreeIDRegex.FindStringSubmatch(trimmed)
	if m == nil {
		return input
	}
	digits := strings.TrimLeft(m[1], "0")
	if digits == "" {
		return "0"
	}
	return digits
}

// evaluateWorktreeCollision is the pure decision core behind
// checkWorktreeCollision: given the already-resolved set of registered
// worktrees, decide whether creating a worktree at resolvedPath for branch
// would collide with an existing worktree directory or an already-checked-out
// branch. dirExists reports whether an unregistered directory already
// occupies rawPath (e.g. leftover from a prior failed teardown).
func evaluateWorktreeCollision(blocks []worktreeBlock, resolvedPath, rawPath, branch string, dirExists bool) error {
	for _, b := range blocks {
		if b.path == resolvedPath {
			return fmt.Errorf("worktree already exists at %s", rawPath)
		}
		if branch != "" && b.branch == branch {
			return fmt.Errorf("branch %q is already checked out at %s", branch, b.path)
		}
	}
	if dirExists {
		return fmt.Errorf("a directory already exists at %s (not a registered worktree — remove it or choose a different path)", rawPath)
	}
	return nil
}

// checkWorktreeCollision is the I/O wrapper: lists registered worktrees,
// resolves paths/branch, and stats the target path, then delegates the
// actual decision to evaluateWorktreeCollision. Never blocks on a transient
// `git worktree list` failure — a real conflict still surfaces from the
// `git worktree add` call itself.
func checkWorktreeCollision(path, branch string) error {
	out, _, err := Run("git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	rawBlocks := parseWorktreeList(out)
	blocks := make([]worktreeBlock, len(rawBlocks))
	for i, b := range rawBlocks {
		blocks[i] = worktreeBlock{path: resolvePath(b.path), branch: b.branch}
	}
	dirExists := false
	if _, statErr := os.Stat(path); statErr == nil {
		dirExists = true
	}
	return evaluateWorktreeCollision(blocks, resolvePath(path), path, branch, dirExists)
}

// evaluateDestroyGuard is the pure decision core of the merge-checked destroy
// guard: given whether merge status against base could be verified at all
// and whether it came back merged, decide whether to refuse teardown.
// Returns nil to proceed, or an error to abort. force always proceeds
// (explicit override); an empty branch or base (nothing meaningful to check)
// also always proceeds; an indeterminate check (checked=false — e.g. no
// origin remote) never blocks, since we can't distinguish "not merged" from
// "can't tell" and false positives here would strand the operator.
func evaluateDestroyGuard(branch, base string, checked, merged, force bool) error {
	if force || branch == "" || base == "" {
		return nil
	}
	if !checked || merged {
		return nil
	}
	return fmt.Errorf("branch %q is not merged into origin/%s — re-run with --force to destroy anyway", branch, base)
}

// isBranchMergedIntoBase reports whether branch has landed on origin/<base>,
// via `git merge-base --is-ancestor <branch> origin/<base>` run inside the
// worktree at worktreePath (so branch-local commits are visible even before
// a fetch). checked is false when the merge status could not be determined
// at all (e.g. no origin remote / unknown ref) — callers must not treat that
// the same as "not merged".
func isBranchMergedIntoBase(worktreePath, branch, base string) (merged bool, checked bool) {
	if branch == "" || base == "" {
		return false, false
	}
	_, _, err := RunInDir(worktreePath, "git", "merge-base", "--is-ancestor", branch, "origin/"+base)
	if err == nil {
		return true, true
	}
	// merge-base --is-ancestor exits non-zero both when the ref genuinely
	// isn't an ancestor AND when a ref fails to resolve. Disambiguate by
	// confirming origin/<base> actually resolves before trusting "not merged".
	if _, _, revErr := RunInDir(worktreePath, "git", "rev-parse", "--verify", "origin/"+base); revErr != nil {
		return false, false
	}
	return false, true
}

// ─── liveness guard (Cluster 4: don't yank worktrees out from under live agents) ───

// livenessTimeout bounds the lsof scan: `lsof +D` recurses the whole tree and
// can be slow on huge worktrees, and the guard must never hang teardown on its
// own tooling. On timeout the check degrades to "unknown" (warn-and-proceed).
const livenessTimeout = 5 * time.Second

// LiveProcess describes a process observed alive inside a worktree directory.
type LiveProcess struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
}

// LivenessReport is the outcome of LiveProcessesIn.
type LivenessReport struct {
	Checked    bool          // false = could not determine (missing lsof + no /proc, or timeout) — warn-and-proceed, never hard-fail
	SelfInside bool          // the calling process itself is inside the worktree — reported distinctly, never counted toward refusal
	Processes  []LiveProcess // live processes inside the worktree, excluding the calling process
}

// LivenessError is the typed refusal WorktreeCleanup returns when live
// processes are found inside the worktree and Force is not set. Callers
// (cli/cmd/worktree.go) unwrap it with errors.As to render the pid+command
// list and the --force hint in the JSON error output.
type LivenessError struct {
	Live []LiveProcess
}

func (e *LivenessError) Error() string {
	return fmt.Sprintf("%d live process(es) inside worktree — refusing teardown", len(e.Live))
}

// evaluateLivenessGuard is the pure decision core of the liveness guard:
// refuse teardown only when the check actually ran (checked), found live
// processes, and force was not given. An indeterminate check (missing
// lsof/timeout) never blocks — warn-and-proceed per spec, since hard-failing
// cleanup on the guard's own tooling would strand the operator.
func evaluateLivenessGuard(live []LiveProcess, checked, force bool) error {
	if force || !checked || len(live) == 0 {
		return nil
	}
	return &LivenessError{Live: live}
}

// LiveProcessesIn reports processes still alive inside the worktree at path
// (cwd or open files under it). macOS-first via `lsof +D <path> -F pc` with a
// short timeout; falls back to a /proc/*/cwd scan on Linux. When neither
// mechanism is available the report comes back Checked=false and callers must
// degrade to warn-and-proceed, never treat it as "no live processes verified".
func LiveProcessesIn(path string) LivenessReport {
	root := resolvePath(path)
	if rep, ok := lsofLiveProcesses(root); ok {
		return rep
	}
	if rep, ok := procScanLiveProcesses(root); ok {
		return rep
	}
	return LivenessReport{Checked: false}
}

// lsofLiveProcesses runs `lsof +D <root> -F pc` under livenessTimeout.
// ok=false means the mechanism itself was unavailable (no lsof binary,
// timeout, or an unparseable failure) — caller should try the next fallback.
func lsofLiveProcesses(root string) (LivenessReport, bool) {
	lsofPath, err := exec.LookPath("lsof")
	if err != nil {
		return LivenessReport{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), livenessTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, lsofPath, "+D", root, "-F", "pc")
	var stdout strings.Builder
	cmd.Stdout = &stdout
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return LivenessReport{}, false
	}
	out := stdout.String()
	if runErr != nil && strings.TrimSpace(out) == "" {
		// lsof exits 1 both when nothing is open under root and on real
		// errors. With empty output, trust exit status 1 as "nothing found";
		// anything else is a tooling failure → let the caller fall back.
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 1 {
			return LivenessReport{Checked: true}, true
		}
		return LivenessReport{}, false
	}
	procs, selfInside := parseLsofPC(out, os.Getpid())
	return LivenessReport{Checked: true, SelfInside: selfInside, Processes: procs}, true
}

// parseLsofPC parses `lsof -F pc` field output (p<pid> / c<command> lines)
// into deduplicated LiveProcess entries, splitting out selfPID (the calling
// process) into the selfInside flag instead of the process list.
func parseLsofPC(out string, selfPID int) (procs []LiveProcess, selfInside bool) {
	seen := map[int]bool{}
	pid := 0
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
		case 'c':
			if pid == 0 || seen[pid] {
				continue
			}
			seen[pid] = true
			if pid == selfPID {
				selfInside = true
				continue
			}
			procs = append(procs, LiveProcess{PID: pid, Command: line[1:]})
		}
	}
	return procs, selfInside
}

// procScanLiveProcesses is the Linux fallback: walk /proc/*/cwd symlinks and
// report processes whose working directory is root or inside it. ok=false
// when /proc is unavailable (e.g. macOS without lsof).
func procScanLiveProcesses(root string) (LivenessReport, bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return LivenessReport{}, false
	}
	rep := LivenessReport{Checked: true}
	self := os.Getpid()
	for _, e := range entries {
		pid, convErr := strconv.Atoi(e.Name())
		if convErr != nil {
			continue
		}
		cwd, linkErr := os.Readlink(filepath.Join("/proc", e.Name(), "cwd"))
		if linkErr != nil {
			continue // gone, or not ours to inspect
		}
		if cwd != root && !strings.HasPrefix(cwd, root+string(os.PathSeparator)) {
			continue
		}
		if pid == self {
			rep.SelfInside = true
			continue
		}
		comm, _ := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		rep.Processes = append(rep.Processes, LiveProcess{PID: pid, Command: strings.TrimSpace(string(comm))})
	}
	return rep, true
}

// buildTeardownScope renders the labelled, human-readable list of everything
// a (non-dry-run) teardown would touch — used by --dry-run to preview the
// blast radius before mutating anything. permanent mirrors the real teardown
// path: permanent branches skip both branch deletion and the merge-checked
// guard, so the preview must not claim a merge check will run for them.
func buildTeardownScope(path, branch, baseBranch string, permanent bool, opts CleanupOpts) []string {
	scope := []string{fmt.Sprintf("worktree dir: %s", path)}
	if branch == "" {
		scope = append(scope, "local branch: (detached — none to delete)")
	} else if permanent {
		scope = append(scope, fmt.Sprintf("local branch: %s (permanent — will NOT be deleted)", branch))
	} else {
		scope = append(scope, fmt.Sprintf("local branch: %s", branch))
		if opts.DeleteRemote {
			scope = append(scope, fmt.Sprintf("remote branch: origin/%s", branch))
		}
		if baseBranch != "" {
			scope = append(scope, fmt.Sprintf("merge check: branch must be an ancestor of origin/%s (bypass with --force)", baseBranch))
		}
	}
	return scope
}

// LIFT: from bravros/private/cli/internal/git/worktree.go (2026-04-18 DRY pass)
// The following helpers (extractPlanNum, computeWorktreePath, resolvePath,
// worktreeExists, worktreeBranch) are lifted verbatim from bravros's battle-tested
// worktree.go. They use `git worktree list --porcelain` for robustness over fragile
// path-string comparisons. Kaisser consumes them via plan 0047 Phase 6; future bravros
// revisions should sync back from here.

// extractPlanNum extracts a plan number from a branch name like "feat/0023-something".
var planNumRegex = regexp.MustCompile(`(\d{4})`)

func extractPlanNum(branch string) string {
	// Look for a 4-digit plan number in the branch name
	match := planNumRegex.FindString(branch)
	return match
}

// computeWorktreePath computes the worktree path from the repo root and branch name.
// Pattern: parentDir/repoName<planNum> with leading zeros stripped, no separator
// (e.g., /Users/x/Sites/myapp23 for plan 0023, /Users/x/Sites/claude111 for P-0111).
// Falls back to parentDir/repoName-<branch-suffix> when no plan number is detectable.
func computeWorktreePath(repoRoot, branch string) string {
	parentDir := filepath.Dir(repoRoot)
	repoName := filepath.Base(repoRoot)

	planNum := extractPlanNum(branch)
	if planNum != "" {
		trimmed := strings.TrimLeft(planNum, "0")
		if trimmed == "" {
			trimmed = "0"
		}
		return filepath.Join(parentDir, repoName+trimmed)
	}

	// Fallback: use branch name suffix (numberless branches keep the hyphen).
	suffix := strings.ReplaceAll(branch, "/", "-")
	return filepath.Join(parentDir, repoName+"-"+suffix)
}

// resolvePath returns the canonical absolute path, resolving symlinks.
func resolvePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	return resolved
}

// worktreeExists checks if a worktree at the given path is registered.
func worktreeExists(path string) bool {
	out, _, err := Run("git", "worktree", "list", "--porcelain")
	if err != nil {
		return false
	}
	resolvedPath := resolvePath(path)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			wtPath := strings.TrimPrefix(line, "worktree ")
			if wtPath == resolvedPath {
				return true
			}
		}
	}
	return false
}

// worktreeBranch returns the branch name for a worktree at the given path.
func worktreeBranch(path string) string {
	out, _, err := Run("git", "worktree", "list", "--porcelain")
	if err != nil {
		return ""
	}
	resolvedPath := resolvePath(path)
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			wtPath := strings.TrimPrefix(line, "worktree ")
			if wtPath == resolvedPath {
				// Look for "branch refs/heads/<name>" in following lines
				for j := i + 1; j < len(lines); j++ {
					if lines[j] == "" {
						break // end of this worktree entry
					}
					if strings.HasPrefix(lines[j], "branch ") {
						ref := strings.TrimPrefix(lines[j], "branch ")
						return strings.TrimPrefix(ref, "refs/heads/")
					}
				}
			}
		}
	}
	return ""
}

// WorktreeContext describes the current process's relationship to git worktrees.
type WorktreeContext struct {
	IsPrimary    bool   // true if running in the primary (main) worktree
	CurrentPath  string // absolute path of current worktree
	BaseHeldAt   string // path of worktree where BaseBranch is checked out (empty if not held elsewhere)
	BaseBranch   string // the base branch name checked
	ContextLabel string // "primary", "secondary", or "base_elsewhere"
}

// worktreeBlock holds parsed data for a single worktree entry from `git worktree list --porcelain`.
type worktreeBlock struct {
	path   string
	branch string // short branch name (without refs/heads/)
}

// parseWorktreeList parses the output of `git worktree list --porcelain` into blocks.
func parseWorktreeList(output string) []worktreeBlock {
	var blocks []worktreeBlock
	var current *worktreeBlock
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			if current != nil {
				blocks = append(blocks, *current)
			}
			current = &worktreeBlock{path: strings.TrimPrefix(line, "worktree ")}
		} else if current != nil && strings.HasPrefix(line, "branch ") {
			ref := strings.TrimPrefix(line, "branch ")
			current.branch = strings.TrimPrefix(ref, "refs/heads/")
		} else if line == "" && current != nil {
			blocks = append(blocks, *current)
			current = nil
		}
	}
	if current != nil {
		blocks = append(blocks, *current)
	}
	return blocks
}

// DetectWorktreeContext detects whether the current process runs in a primary
// or secondary worktree, and where baseBranch is checked out.
func DetectWorktreeContext(baseBranch string) (*WorktreeContext, error) {
	out, _, err := Run("git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git worktree list failed: %w", err)
	}

	blocks := parseWorktreeList(out)
	if len(blocks) == 0 {
		return nil, fmt.Errorf("no worktrees found")
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("cannot get working directory: %w", err)
	}
	currentPath := resolvePath(cwd)

	primaryPath := resolvePath(blocks[0].path)

	ctx := &WorktreeContext{
		BaseBranch:  baseBranch,
		CurrentPath: currentPath,
	}

	if currentPath == primaryPath {
		ctx.IsPrimary = true
		ctx.ContextLabel = "primary"
		return ctx, nil
	}

	// Secondary worktree — check if baseBranch is held elsewhere
	for _, block := range blocks {
		blockPath := resolvePath(block.path)
		if blockPath == currentPath {
			continue // skip current
		}
		if block.branch == baseBranch {
			ctx.BaseHeldAt = blockPath
			ctx.ContextLabel = "base_elsewhere"
			return ctx, nil
		}
	}

	ctx.ContextLabel = "secondary"
	return ctx, nil
}

// ListWorktreePaths returns the absolute paths of all registered git worktrees,
// excluding the current working directory. Returns an empty slice on error.
func ListWorktreePaths() []string {
	out, _, err := Run("git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	blocks := parseWorktreeList(out)

	cwd, _ := os.Getwd()
	currentPath := resolvePath(cwd)

	var paths []string
	for _, b := range blocks {
		p := resolvePath(b.path)
		if p != currentPath {
			paths = append(paths, p)
		}
	}
	return paths
}

// ComputeWorktreePath is the exported wrapper over computeWorktreePath. It lets
// other packages (internal/worktree) reuse the bravros-lifted path-derivation
// logic without re-implementing it. Pattern: parentDir/repoName-planNum.
func ComputeWorktreePath(repoRoot, branch string) string {
	return computeWorktreePath(repoRoot, branch)
}

// WorktreeExistsAt is the exported wrapper over worktreeExists, reading from
// `git worktree list --porcelain` (lifted from bravros). Returns true when a
// worktree registered at the resolved path already exists in the current repo.
func WorktreeExistsAt(path string) bool {
	return worktreeExists(path)
}

// WorktreeSetup creates a new git worktree for the given branch.
//
// Flow:
//  1. Load config for staging_branch / base branch
//  2. Compute path if not provided
//  3. git worktree add -b <branch> <path> (or attach existing branch)
//  4. Verify via git worktree list
//  5. If !NoRebase: rebase from base branch
//  6. Return result
func WorktreeSetup(branch, path string, opts WorktreeOpts) (*WorktreeSetupResult, error) {
	result := &WorktreeSetupResult{Branch: branch}

	// 1. Determine base branch
	baseBranch := resolveDefaultBaseBranch(opts.BaseBranch)

	// 2. Compute path if not provided
	if path == "" {
		repoRoot, _, err := Run("git", "rev-parse", "--show-toplevel")
		if err != nil {
			return nil, fmt.Errorf("not in a git repository")
		}
		path = computeWorktreePath(repoRoot, branch)
	}
	result.Path = path

	// Collision pre-check: refuse before creating anything if the target dir
	// or the branch itself already collide with an existing worktree.
	if err := checkWorktreeCollision(path, branch); err != nil {
		return nil, err
	}

	// 3. Create worktree
	if BranchExists(branch) {
		// Branch exists — attach it to the worktree
		_, stderr, err := Run("git", "worktree", "add", path, branch)
		if err != nil {
			return nil, fmt.Errorf("failed to create worktree: %s", stderr)
		}
	} else {
		// Create new branch in worktree
		_, stderr, err := Run("git", "worktree", "add", "-b", branch, path, baseBranch)
		if err != nil {
			return nil, fmt.Errorf("failed to create worktree: %s", stderr)
		}
	}

	// 4. Verify
	if !worktreeExists(path) {
		return nil, fmt.Errorf("worktree creation failed: not found in worktree list")
	}

	result.Created = true

	// 5. Rebase if requested
	if !opts.NoRebase {
		_, stderr, err := RunInDir(path, "git", "rebase", baseBranch)
		if err != nil {
			// Rebase failed — abort and report (worktree is still usable)
			RunInDir(path, "git", "rebase", "--abort")
			// Non-fatal: worktree was created successfully, just rebase failed
			result.Error = fmt.Sprintf("rebase failed (worktree created): %s", stderr)
		}
	}

	return result, nil
}

// WorktreeCleanup removes a git worktree and optionally deletes the branch.
//
// Flow:
//  1. Verify worktree exists
//  2. Get branch name from worktree
//  3. Liveness check: detect processes still alive inside the worktree
//  4. --dry-run: report the teardown scope + liveness and return without
//     mutating anything
//  5. Liveness guard: refuse (typed *LivenessError, pids listed) when live
//     processes were found, unless --force (which still reports the list);
//     an indeterminate check degrades to warn-and-proceed (liveness:
//     "unknown"), never a hard failure
//  6. Merge-checked destroy guard: refuse unless branch is merged into
//     origin/<base>, or --force overrides
//  7. git worktree remove <path> (--force if Force=true)
//  8. Check if permanent branch → skip branch deletion
//  9. Delete local branch
//  10. If DeleteRemote: delete remote branch
//  11. Return result
func WorktreeCleanup(path string, opts CleanupOpts) (*WorktreeCleanupResult, error) {
	result := &WorktreeCleanupResult{Path: path, LiveProcesses: []LiveProcess{}}

	// 1. Verify worktree exists
	if !worktreeExists(path) {
		return nil, fmt.Errorf("no worktree found at %s", path)
	}

	// 2. Get branch name
	branch := worktreeBranch(path)
	result.Branch = branch

	baseBranch := resolveDefaultBaseBranch(opts.BaseBranch)

	// 3. Liveness check (Cluster 4): find processes still running inside the
	// worktree BEFORE any teardown, so background agents aren't left with a
	// deleted cwd ("Working directory … was deleted" / EISDIR noise).
	live := LiveProcessesIn(path)
	result.LiveProcesses = append(result.LiveProcesses, live.Processes...)
	if !live.Checked {
		result.Liveness = "unknown"
	}
	if live.SelfInside {
		// The calling process itself is excluded from the refusal — being
		// inside the worktree you're deleting is its own, different footgun.
		result.LivenessNote = "you are inside this worktree"
	}

	// 4. --dry-run: preview the teardown scope, mutate nothing, exit 0.
	if opts.DryRun {
		result.DryRun = true
		cfg, _ := config.LoadBravrosConfig()
		permanent := branch != "" && IsPermanentBranch(branch, cfg)
		result.Scope = buildTeardownScope(path, branch, baseBranch, permanent, opts)
		return result, nil
	}

	// 5. Liveness guard: refuse teardown while live processes hold the
	// worktree, unless --force (the caller still gets the list via
	// result/LivenessError). Indeterminate checks never block.
	if err := evaluateLivenessGuard(live.Processes, live.Checked, opts.Force); err != nil {
		return nil, err
	}

	// 6. Merge-checked destroy guard. Skipped for permanent branches (their
	// removal already short-circuits branch deletion below) and bypassed
	// entirely by --force.
	if branch != "" && !opts.Force {
		cfg, _ := config.LoadBravrosConfig()
		if !IsPermanentBranch(branch, cfg) {
			merged, checked := isBranchMergedIntoBase(path, branch, baseBranch)
			if err := evaluateDestroyGuard(branch, baseBranch, checked, merged, opts.Force); err != nil {
				return nil, err
			}
		}
	}

	// 7. Remove worktree
	removeArgs := []string{"git", "worktree", "remove", path}
	if opts.Force {
		removeArgs = []string{"git", "worktree", "remove", "--force", path}
	}
	// Sanctioned-teardown bypass for audit Rule 51 (registered-worktree
	// destruction guard). This is the ONE place production code legitimately
	// removes a worktree, so it is allowed to set the bypass. Scoped tightly:
	// set immediately before the remove exec and unset right after so it never
	// leaks into the branch-delete / remote-delete execs below (or the wider
	// process). Audit rules read os.Getenv on a child Bash call, not on this
	// in-process exec, but setting it keeps the contract single-sourced and
	// covers any future audit-on-exec instrumentation.
	//
	// NOTE: os.Setenv mutates the process-wide environment and is NOT
	// goroutine-safe. WorktreeCleanup is user-triggered teardown and is not
	// expected to run concurrently with itself; if that ever changes, scope the
	// bypass to the child process via exec.Cmd.Env instead of the parent env.
	os.Setenv("KAISSER_WORKTREE_DESTROY", "1")
	_, stderr, err := Run(removeArgs...)
	os.Unsetenv("KAISSER_WORKTREE_DESTROY")
	if err != nil {
		return nil, fmt.Errorf("failed to remove worktree: %s", stderr)
	}

	result.Removed = true

	// 8-9. Delete branch if not permanent
	if branch != "" {
		cfg, _ := config.LoadBravrosConfig()
		if IsPermanentBranch(branch, cfg) {
			// Never delete permanent branches
			return result, nil
		}

		// Delete local branch
		deleteFlag := "-d"
		if opts.Force {
			deleteFlag = "-D"
		}
		_, _, delErr := Run("git", "branch", deleteFlag, branch)
		if delErr == nil {
			result.BranchDeleted = true
		}

		// 10. Delete remote branch if requested
		if opts.DeleteRemote {
			Run("git", "push", "origin", "--delete", branch)
		}
	}

	return result, nil
}
