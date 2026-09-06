package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bravros/bravros/cli/internal/token"
	"github.com/bravros/bravros/cli/internal/trash"
	"github.com/spf13/cobra"
)

var policeCmd = &cobra.Command{
	Use:   "police",
	Short: "Bravros police engine (audit and enforcement)",
}

// policePreToolUseCmd intercepts tool usage.
var policePreToolUseCmd = &cobra.Command{
	Use:   "pretooluse",
	Short: "Hook for PreToolUse intercept",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Read stdin for the payload
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil
		}
		var payload struct {
			ToolName  string `json:"tool_name"`
			ToolName2 string `json:"toolName"` // legacy shim: nothing real sends this
			Input     struct {
				Command string `json:"command"`
			} `json:"tool_input"`
			Input2 struct {
				Command string `json:"command"`
			} `json:"input"` // legacy shim: nothing real sends this
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil
		}

		// Claude Code's real PreToolUse contract is snake_case
		// (tool_name/tool_input). The camelCase fallback below (toolName/input)
		// is defense-in-depth only: no supported caller emits it, but a payload
		// in that legacy shape silently approving a main push is the exact bug
		// this fixes, so the shim fails closed instead of being dropped.
		// Remove once a payload-contract version field lets us assert the shape
		// instead of guessing.
		toolName := payload.ToolName
		if toolName == "" {
			toolName = payload.ToolName2
		}
		command := payload.Input.Command
		if command == "" {
			command = payload.Input2.Command
		}

		// Only intercept bash commands
		if toolName != "bash" && toolName != "Bash" {
			return nil
		}
		// ───── SAFETY FLOOR (always-on; runs even under stand-down) ─────
		//   Members: rule 52 — irreversible content loss;
		//            checkAiSignature — AI attribution on commits and PR bodies.
		//   The floor is "never suppressible", not "content loss" alone: the
		//   zero-AI-attribution rule is absolute, so it may not sit below the
		//   stand-down divider.
		//   Nothing in this section may call isStandDownActive().
		if v := rule52Check(command); v != nil {
			if !rule52ConsumeToken() {
				out := struct {
					Stdout string `json:"stdout"`
					Stderr string `json:"stderr"`
					Exit   int    `json:"exitCode"`
				}{
					Stdout: "",
					Stderr: rule52BlockMessage(v),
					Exit:   2,
				}
				enc, _ := json.Marshal(out)
				fmt.Fprintln(cmd.OutOrStdout(), string(enc))
			}
			return nil
		}
		if msg := checkAiSignature(command); msg != "" {
			out := struct {
				Stdout string `json:"stdout"`
				Stderr string `json:"stderr"`
				Exit   int    `json:"exitCode"`
			}{
				Stdout: "",
				Stderr: msg,
				Exit:   2,
			}
			enc, _ := json.Marshal(out)
			fmt.Fprintln(cmd.OutOrStdout(), string(enc))
			return nil
		}
		// ───── STAND-DOWN-SUPPRESSIBLE RULES ─────

		if isMainMerge(command) {
			if isStandDownActive() {
				return nil
			}
			if !hasValidPoliceToken() {
				// Block execution
				out := struct {
					Stdout string `json:"stdout"`
					Stderr string `json:"stderr"`
					Exit   int    `json:"exitCode"`
				}{
					Stdout: "",
					Stderr: "✋🏽 Police Block: Merging or pushing to main is blocked for agents.\n" +
						"You must request the operator to mint a token from outside Claude Code:\n" +
						"  bravros police unlock\n",
					Exit: 2,
				}
				enc, _ := json.Marshal(out)
				fmt.Fprintln(cmd.OutOrStdout(), string(enc))
				return nil
			}
		}

		if !isStandDownActive() {
			if msg := checkPrCommentBody(command); msg != "" {
				out := struct {
					Stdout string `json:"stdout"`
					Stderr string `json:"stderr"`
					Exit   int    `json:"exitCode"`
				}{
					Stdout: "",
					Stderr: msg,
					Exit:   2,
				}
				enc, _ := json.Marshal(out)
				fmt.Fprintln(cmd.OutOrStdout(), string(enc))
				return nil
			}
		}
		return nil
	},
}

// protectedBranches are the only branches the Police gate defends. Everything
// else — homolog included — is routinely pushed and merged by /push, /hotfix,
// /finish, /batch-merge-prs and /auto-pr, and must never need a token.
var protectedBranches = map[string]bool{"main": true, "master": true}

// isMainMerge reports whether cmd would push to, or merge a PR into, a
// protected branch.
//
// Matching is word-boundary based, not substring: `git push origin
// fix/maintain-cache` contains "main" but targets nothing protected. A bare
// `git push` is resolved against the current branch, and a `gh pr merge` is
// resolved against the PR's real base — neither is knowable from the string
// alone.
func isMainMerge(cmd string) bool {
	for _, seg := range commandSegments(cmd) {
		switch {
		case startsWith(seg, "git", "push"):
			if pushTargetsProtected(seg) {
				return true
			}
		case startsWith(seg, "gh", "pr", "merge"):
			if mergeTargetsProtected(seg) {
				return true
			}
		}
	}
	return false
}

// unquote strips matching leading and trailing single or double quotes from s.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// commandSegments splits a command line on unquoted shell separators (&&, ||, ;, |, \n),
// respecting single quotes, double quotes, backslash escapes, and subshell parentheses.
func commandSegments(cmd string) [][]string {
	var segs [][]string
	var cur []string
	var token strings.Builder
	var inSingle, inDouble, escaped bool

	flushToken := func() {
		if token.Len() > 0 {
			t := token.String()
			token.Reset()
			t = strings.Trim(t, "()")
			if t != "" {
				cur = append(cur, unquote(t))
			}
		}
	}

	flushSegment := func() {
		flushToken()
		if len(cur) > 0 {
			segs = append(segs, cur)
			cur = []string{}
		}
	}

	runes := []rune(cmd)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if escaped {
			token.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && !inSingle {
			escaped = true
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			token.WriteRune(r)
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			token.WriteRune(r)
			continue
		}

		if inSingle || inDouble {
			token.WriteRune(r)
			continue
		}

		// Unquoted separators
		if r == ';' || r == '\n' {
			flushSegment()
			continue
		}
		if r == '&' && i+1 < len(runes) && runes[i+1] == '&' {
			flushSegment()
			i++
			continue
		}
		if r == '|' && i+1 < len(runes) && runes[i+1] == '|' {
			flushSegment()
			i++
			continue
		}
		if r == '|' {
			flushSegment()
			continue
		}
		if r == '(' || r == ')' {
			flushToken()
			continue
		}

		if r == ' ' || r == '\t' {
			flushToken()
			continue
		}

		token.WriteRune(r)
	}
	flushSegment()
	return segs
}

// startsWith reports whether the segment's leading words are exactly words,
// ignoring any `env VAR=x` style prefix assignments.
func startsWith(seg []string, words ...string) bool {
	for len(seg) > 0 && strings.Contains(seg[0], "=") {
		seg = seg[1:]
	}
	if len(seg) < len(words) {
		return false
	}
	for i, w := range words {
		if unquote(seg[i]) != w {
			return false
		}
	}
	return true
}

// pushTargetsProtected inspects the refspecs of a `git push`. Each refspec is
// split on ':' so `HEAD:main` and `+main:main` are caught, and leading '+' is
// stripped. With no refspec at all the push follows the tracked upstream, so
// the current branch decides.
func pushTargetsProtected(fields []string) bool {
	refspecs := 0
	for _, f := range fields {
		f = unquote(f)
		if strings.HasPrefix(f, "-") || f == "git" || f == "push" {
			continue
		}
		if f == "origin" || strings.Contains(f, "@") || strings.Contains(f, "://") {
			continue
		}
		refspecs++
		for _, part := range strings.Split(strings.TrimPrefix(f, "+"), ":") {
			part = unquote(strings.TrimPrefix(part, "refs/heads/"))
			if protectedBranches[part] {
				return true
			}
		}
	}
	if refspecs == 0 {
		return protectedBranches[currentBranch()]
	}
	return false
}

// mergeTargetsProtected resolves the base branch of the PR named in a
// `gh pr merge`. The base is not in the command string, so it is read from the
// forge. If that lookup cannot answer — offline, no auth, no PR — the command
// is allowed through: the merge-to-main path is independently gated by
// /promote's out-of-band token and the review stamp, and a hook that blocked
// whenever the network hiccuped would strand every routine homolog merge.
func mergeTargetsProtected(fields []string) bool {
	args := []string{"pr", "view", "--json", "baseRefName", "-q", ".baseRefName"}
	for _, f := range fields {
		if _, err := strconv.Atoi(f); err == nil {
			args = append(args, f)
			break
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "gh", args...).Output()
	if err != nil {
		return false
	}
	return protectedBranches[strings.TrimSpace(string(out))]
}

// ---------------------------------------------------------------------------
// Rule 52 — irreversible content-loss guard (P-0024, safety floor)
//
// Blocks any agent-issued Bash command that would erase content git has
// never seen — the one class of loss git cannot undo. The gate is on
// TARGET STATE, never on command pattern: every family below ends in a
// `git status --porcelain` probe of the actual target, so on a clean tree
// `git checkout -- .`, `git reset --hard` and `git clean -f` all pass
// silently. The two unconditional carve-outs are `git clean -x/-X`
// (deletes gitignored files porcelain never lists) and `git stash
// drop/clear` (the stash entry itself IS the never-seen content).
//
// This is a safety-floor member (decisions.md, P-0024 Phase 1): it must
// never read isStandDownActive(), BRAVROS_POLICE_STANDDOWN, or
// standDownPath. Its only bypass is the out-of-band destructive token.
// ---------------------------------------------------------------------------

// rule52Violation describes one detected content-loss violation, ready to
// render into the block message.
type rule52Violation struct {
	what       string // what is blocked, and the concrete at-risk paths
	sanctioned string // literal runnable sanctioned-path command(s)
}

// rule52Check inspects command for the six irreversible-content-loss
// families (git checkout / restore / reset --hard / clean / stash
// drop|clear, and recursive rm) and returns a violation when a matched
// target holds content git has never seen.
//
// FIRST STATEMENT is a cheap substring pre-filter: no git call, no
// filesystem access, no allocation-heavy parsing before it. Police runs on
// every Bash call at ~10 ms — this early return keeps non-matching commands
// at that floor. Backslashes are stripped BEFORE the scan: commandSegments
// consumes shell escapes, so `g\it reset --hard` tokenizes to `git` — a raw
// scan would early-return on it and skip the floor entirely (PR 75 review,
// finding 1). strings.ReplaceAll returns the original string unallocated
// when no backslash is present, so the common path stays free.
func rule52Check(command string) *rule52Violation {
	scan := strings.ReplaceAll(command, `\`, "")
	if !strings.Contains(scan, "git") && !strings.Contains(scan, "rm") {
		return nil
	}
	for _, seg := range commandSegments(command) {
		seg = rule52StripEnvPrefix(seg)
		if len(seg) == 0 {
			continue
		}
		var v *rule52Violation
		switch seg[0] {
		case "git":
			if len(seg) < 2 {
				continue
			}
			switch seg[1] {
			case "checkout":
				v = rule52CheckCheckout(seg[2:])
			case "restore":
				v = rule52CheckRestore(seg[2:])
			case "reset":
				v = rule52CheckResetHard(seg[2:])
			case "clean":
				v = rule52CheckClean(seg[2:])
			case "stash":
				v = rule52CheckStash(seg[2:])
			}
		case "rm":
			v = rule52CheckRm(seg)
		}
		if v != nil {
			return v
		}
	}
	return nil
}

// rule52StripEnvPrefix strips leading `VAR=x` assignments off a segment,
// mirroring startsWith's own loop, so `FOO=1 git checkout -- .` still
// anchors on "git".
func rule52StripEnvPrefix(seg []string) []string {
	for len(seg) > 0 && strings.Contains(seg[0], "=") {
		seg = seg[1:]
	}
	return seg
}

// rule52CheckCheckout gates `git checkout …`. Branch creation/switching is
// allowed unconditionally; the blocked shapes are pathspec checkout over
// files with uncommitted worktree modifications, and `-f/--force` (which
// discards ALL worktree modifications on switch).
func rule52CheckCheckout(args []string) *rule52Violation {
	var pathspec []string
	force := false
	sawDashDash := false
	for i, a := range args {
		switch {
		case a == "--":
			sawDashDash = true
			pathspec = append(pathspec, args[i+1:]...)
		case sawDashDash:
			// consumed above
		case a == "-b" || a == "-B" || a == "--orphan" || a == "--detach":
			return nil // branch creation / detach — never destructive to worktree mods
		case a == "-f" || a == "--force":
			force = true
		}
		if sawDashDash {
			break
		}
	}
	if !sawDashDash {
		for _, a := range args {
			if !strings.HasPrefix(a, "-") {
				pathspec = append(pathspec, a)
			}
		}
	}

	if force {
		entries, ok := rule52StatusEntries(nil)
		if !ok {
			return rule52IndeterminateViolation("`git checkout --force`")
		}
		if modified := rule52ModifiedPaths(entries); len(modified) > 0 {
			return &rule52Violation{
				what: fmt.Sprintf("`git checkout --force` would discard %d tracked file(s) carrying uncommitted changes:%s",
					len(modified), rule52FileList(modified, 8)),
				sanctioned: "bravros discard " + strings.Join(rule52ShortList(modified), " "),
			}
		}
		return nil
	}
	if len(pathspec) == 0 {
		return nil // plain branch switch or bare `git checkout`
	}
	entries, ok := rule52StatusEntries(pathspec)
	if !ok {
		return rule52IndeterminateViolation(fmt.Sprintf("`git checkout -- %s`", strings.Join(pathspec, " ")))
	}
	if modified := rule52ModifiedPaths(entries); len(modified) > 0 {
		return &rule52Violation{
			what: fmt.Sprintf("`git checkout -- %s` would discard %d tracked file(s) carrying uncommitted changes:%s",
				strings.Join(pathspec, " "), len(modified), rule52FileList(modified, 8)),
			sanctioned: "bravros discard " + strings.Join(rule52ShortList(modified), " "),
		}
	}
	return nil
}

// rule52CheckRestore gates `git restore …`. `--staged`/`-S` WITHOUT
// `--worktree`/`-W` only moves index entries back to HEAD — the worktree
// content survives and staged blobs remain in the object store, so it is
// allowed. Every worktree-touching form blocks when the pathspec covers
// files with uncommitted modifications. Every bare positional joins the
// pathspec (no first-match short-circuit — a first-match bug here silently
// dropped args 2..n in old PR #403 review).
func rule52CheckRestore(args []string) *rule52Violation {
	staged, worktree := false, false
	var pathspec []string
	skipNext := false
	for i, a := range args {
		switch {
		case skipNext:
			skipNext = false
		case a == "--":
			pathspec = append(pathspec, args[i+1:]...)
		case a == "--staged" || a == "-S":
			staged = true
		case a == "--worktree" || a == "-W":
			worktree = true
		case a == "--source" || a == "-s":
			skipNext = true
		case strings.HasPrefix(a, "-"):
			// other flags (incl. --source=<ref>) — ignore
		default:
			pathspec = append(pathspec, a)
		}
		if a == "--" {
			break
		}
	}
	if staged && !worktree {
		return nil // index-only restore; worktree content untouched
	}
	if len(pathspec) == 0 {
		return nil
	}
	entries, ok := rule52StatusEntries(pathspec)
	if !ok {
		return rule52IndeterminateViolation(fmt.Sprintf("`git restore %s`", strings.Join(pathspec, " ")))
	}
	if modified := rule52ModifiedPaths(entries); len(modified) > 0 {
		return &rule52Violation{
			what: fmt.Sprintf("`git restore %s` would discard %d tracked file(s) carrying uncommitted changes:%s",
				strings.Join(pathspec, " "), len(modified), rule52FileList(modified, 8)),
			sanctioned: "bravros discard " + strings.Join(rule52ShortList(modified), " "),
		}
	}
	return nil
}

// rule52CheckResetHard gates `git reset --hard [ref]`: blocked when ANY
// tracked file carries a worktree modification. Soft/mixed/keep resets and
// ref rewinds on a clean tree are allowed (reflog-recoverable).
func rule52CheckResetHard(args []string) *rule52Violation {
	hard := false
	for _, a := range args {
		if a == "--hard" {
			hard = true
		}
	}
	if !hard {
		return nil
	}
	entries, ok := rule52StatusEntries(nil)
	if !ok {
		return rule52IndeterminateViolation("`git reset --hard`")
	}
	if modified := rule52ModifiedPaths(entries); len(modified) > 0 {
		return &rule52Violation{
			what: fmt.Sprintf("`git reset --hard` would discard %d tracked file(s) carrying uncommitted changes:%s",
				len(modified), rule52FileList(modified, 8)),
			sanctioned: "bravros discard " + strings.Join(rule52ShortList(modified), " "),
		}
	}
	return nil
}

// rule52CheckClean gates `git clean …`. Dry runs and non-force forms pass.
// `-x`/`-X` block ALWAYS — they delete gitignored files (.env, vendor/)
// that `git status --porcelain` never lists, so no state probe can clear
// them. Plain force cleans block only when untracked files actually exist
// in the target. Combined short flags (`-fdx`) are parsed per-character.
func rule52CheckClean(args []string) *rule52Violation {
	force, ignored, dryRun := false, false, false
	var pathspec []string
	for i, a := range args {
		switch {
		case a == "--":
			pathspec = append(pathspec, args[i+1:]...)
		case a == "--force":
			force = true
		case a == "--dry-run" || a == "-n":
			dryRun = true
		case strings.HasPrefix(a, "--"):
			// other long flags — ignore
		case strings.HasPrefix(a, "-") && len(a) > 1:
			for _, c := range a[1:] {
				switch c {
				case 'f':
					force = true
				case 'x', 'X':
					ignored = true
				case 'n':
					dryRun = true
				}
			}
		default:
			pathspec = append(pathspec, a)
		}
		if a == "--" {
			break
		}
	}
	if dryRun || (!force && !ignored) {
		return nil
	}
	if ignored {
		return &rule52Violation{
			what: "`git clean -x`/`-X` deletes gitignored files (.env with credentials,\n" +
				"vendor/) that `git status` never lists — blocked ALWAYS, even on a\n" +
				"clean tree.",
			sanctioned: "bravros clean-untracked",
		}
	}
	entries, ok := rule52StatusEntries(pathspec)
	if !ok {
		return rule52IndeterminateViolation("`git clean`")
	}
	if untracked := rule52UntrackedPaths(entries); len(untracked) > 0 {
		target := "."
		if len(pathspec) > 0 {
			target = strings.Join(pathspec, " ")
		}
		return &rule52Violation{
			what: fmt.Sprintf("`git clean` over `%s` would delete %d untracked file(s) git has never seen:%s",
				target, len(untracked), rule52FileList(untracked, 8)),
			sanctioned: "bravros clean-untracked " + strings.Join(rule52ShortList(untracked), " "),
		}
	}
	return nil
}

// rule52CheckStash gates `git stash …`. `drop`/`clear` block ALWAYS — no
// worktree-state check applies, because the stash entry itself is the
// never-seen content. Every other stash subcommand is allowed.
func rule52CheckStash(args []string) *rule52Violation {
	if len(args) == 0 {
		return nil
	}
	if args[0] == "drop" || args[0] == "clear" {
		return &rule52Violation{
			what: fmt.Sprintf("`git stash %s` is irreversible — the stash entry itself is the\n"+
				"content git has never seen anywhere else.", args[0]),
			sanctioned: "git stash pop              # re-apply, keep working\n" +
				"  git stash branch <name>    # materialize the stash on its own branch",
		}
	}
	return nil
}

// rule52CheckRm gates recursive `rm` (`-r`/`-R`, including combined short
// flags like `-rf`/`-fr`) whose target lies INSIDE the repo and holds
// never-seen content (untracked files or uncommitted modifications).
// `$VAR`-containing targets skip (unresolvable — fail-open for rm target
// resolution). `~` expands; relative targets resolve against cwd. Targets
// outside the repo root are allowed. Being DETERMINED not to be in a git
// repository at all is also allowed — the rule gates repo content only —
// but git failing to answer that question at all (rule52RepoUnknown) is
// indeterminate and fails closed, never open.
func rule52CheckRm(tokens []string) *rule52Violation {
	recursive := false
	var targets []string
	sawDashDash := false
	for _, a := range tokens[1:] {
		switch {
		case sawDashDash:
			targets = append(targets, a)
		case a == "--":
			sawDashDash = true
		case a == "--recursive":
			recursive = true
		case strings.HasPrefix(a, "--"):
			// other long flags — ignore
		case strings.HasPrefix(a, "-") && len(a) > 1:
			for _, c := range a[1:] {
				if c == 'r' || c == 'R' {
					recursive = true
				}
			}
		default:
			targets = append(targets, a)
		}
	}
	if !recursive || len(targets) == 0 {
		return nil
	}
	switch rule52ProbeRepo() {
	case rule52RepoNo:
		return nil // determined not in a repo at all — nothing here is repo content
	case rule52RepoUnknown:
		return rule52IndeterminateViolation("`rm -r`") // git couldn't answer — fail closed
	}
	repoRoot, err := trash.RepoRoot("")
	if err != nil {
		return rule52IndeterminateViolation("`rm -r`")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return rule52IndeterminateViolation("`rm -r`")
	}

	var neverSeen []string
	var lastTarget string
	for _, t := range targets {
		if strings.Contains(t, "$") {
			continue // unresolvable shell expansion — fail-open for rm targets
		}
		abs := t
		if abs == "~" || strings.HasPrefix(abs, "~/") {
			if home, herr := os.UserHomeDir(); herr == nil {
				abs = filepath.Join(home, strings.TrimPrefix(abs, "~"))
			}
		}
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(cwd, abs)
		}
		abs = filepath.Clean(abs)
		rel, relErr := filepath.Rel(repoRoot, abs)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue // outside the repo
		}
		pathspec := []string{rel}
		if rel == "." {
			pathspec = nil
		}
		entries, ok := rule52ProbeStatus(pathspec)
		if !ok {
			return rule52IndeterminateViolation(fmt.Sprintf("`rm -r %s`", t))
		}
		modified := rule52ModifiedPaths(entries)
		untracked := rule52UntrackedPaths(entries)
		if len(modified)+len(untracked) > 0 {
			neverSeen = append(neverSeen, modified...)
			neverSeen = append(neverSeen, untracked...)
			lastTarget = t
		}
	}
	if len(neverSeen) == 0 {
		return nil
	}
	return &rule52Violation{
		what: fmt.Sprintf("`rm -r %s` would delete %d file(s) with uncommitted or untracked content:%s",
			lastTarget, len(neverSeen), rule52FileList(neverSeen, 8)),
		sanctioned: "bravros discard " + lastTarget + "          # tracked modifications\n" +
			"  bravros clean-untracked " + lastTarget + "  # untracked files",
	}
}

// rule52RepoState is the three-state result of probing repository presence.
// Two failure shapes must NOT collapse into one: git determinedly saying
// "not a repository" is a real allow (nothing here is repo content), while
// git failing to answer at all (broken binary, permission denied, a
// corrupted repo) is indeterminate and — per Decision 8 — must fail
// closed, never fail open.
type rule52RepoState int

const (
	rule52RepoUnknown rule52RepoState = iota // git could not answer — indeterminate, fail closed
	rule52RepoYes                            // cwd is inside a git work tree
	rule52RepoNo                             // cwd is determined NOT to be inside a git repository
)

// rule52ProbeRepo runs `git rev-parse --is-inside-work-tree` directly (not
// trash.RepoRoot, which collapses every failure into one generic error) and
// classifies stderr so "fatal: not a git repository" — git's own answer to
// this exact question — is told apart from any other failure. The
// fail-closed determination in rule52StatusEntries and rule52CheckRm
// depends on this distinction.
func rule52ProbeRepo() rule52RepoState {
	var stderr strings.Builder
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		if strings.TrimSpace(string(out)) == "true" {
			return rule52RepoYes
		}
		// "false" means inside a .git directory itself, not a work tree —
		// nothing here is checkable repo content either.
		return rule52RepoNo
	}
	if strings.Contains(stderr.String(), "not a git repository") {
		return rule52RepoNo
	}
	return rule52RepoUnknown
}

// rule52ProbeStatus runs trash.Status and reports ok=false (indeterminate)
// on any git failure. Callers that have not already established repo
// presence should go through rule52StatusEntries instead.
func rule52ProbeStatus(pathspecs []string) ([]trash.StatusEntry, bool) {
	entries, err := trash.Status("", pathspecs)
	if err != nil {
		return nil, false
	}
	return entries, true
}

// rule52StatusEntries is rule52ProbeStatus with the three-state repo probe
// folded in — determined not-a-repo short-circuits to a clean allow
// (nothing outside a repo is repo content), indeterminate short-circuits to
// ok=false (fail closed) — and $-widening: any pathspec containing an
// unresolvable shell expansion widens the check to the whole tree, since
// the command IS destructive and only its target is unknown.
func rule52StatusEntries(pathspecs []string) ([]trash.StatusEntry, bool) {
	switch rule52ProbeRepo() {
	case rule52RepoNo:
		return nil, true
	case rule52RepoUnknown:
		return nil, false
	}
	for _, p := range pathspecs {
		if strings.Contains(p, "$") {
			pathspecs = nil
			break
		}
	}
	return rule52ProbeStatus(pathspecs)
}

// rule52ModifiedPaths filters entries to worktree-side modifications of
// tracked files (porcelain Y = M) — the exact content class git cannot
// restore.
func rule52ModifiedPaths(entries []trash.StatusEntry) []string {
	var out []string
	for _, e := range entries {
		if !e.Untracked() && e.Y == 'M' {
			out = append(out, e.Path)
		}
	}
	return out
}

// rule52UntrackedPaths filters entries to untracked files (??).
func rule52UntrackedPaths(entries []trash.StatusEntry) []string {
	var out []string
	for _, e := range entries {
		if e.Untracked() {
			out = append(out, e.Path)
		}
	}
	return out
}

// rule52ShortList caps a path list for embedding in a literal runnable
// command: past 4 entries the whole-tree `.` form is both shorter and what
// the agent actually wants.
func rule52ShortList(paths []string) []string {
	if len(paths) > 4 {
		return []string{"."}
	}
	return paths
}

// rule52FileList renders up to max paths for the block message, appending
// an "… and N more" tail so the block message always names concrete files
// at risk.
func rule52FileList(paths []string, max int) string {
	if len(paths) == 0 {
		return ""
	}
	shown := paths
	tail := ""
	if len(shown) > max {
		tail = fmt.Sprintf("\n  … and %d more", len(shown)-max)
		shown = shown[:max]
	}
	return "\n  " + strings.Join(shown, "\n  ") + tail
}

// rule52IndeterminateViolation builds the fail-closed violation used when a
// status probe cannot determine cleanliness (git error while inside a
// repository). Uncertainty inside the safety floor resolves toward
// blocking — the sanctioned verb is always one command away.
func rule52IndeterminateViolation(what string) *rule52Violation {
	return &rule52Violation{
		what: what + " — cleanliness of the target could not be determined\n" +
			"(git status failed). Rule 52 fails closed: blocking rather than risking\n" +
			"irreversible content loss.",
		sanctioned: "bravros discard <paths>          # tracked modifications\n" +
			"  bravros clean-untracked <paths>  # untracked files",
	}
}

// rule52BlockMessage renders a violation into the four-part block message:
// (1) what is blocked and the concrete at-risk paths, (2) the sanctioned
// path, (3) the destructive-token escape hatch, (4) the safety-floor
// statement. Deliberately never mentions `bravros police unlock` or
// stand-down as an option.
func rule52BlockMessage(v *rule52Violation) string {
	return "✋🏽 Police Block: " + v.what + "\n\n" +
		"Content git has never seen is unrecoverable once destroyed — not in a\n" +
		"branch, not in the reflog, not in a stash entry.\n\n" +
		"Sanctioned path — preserves into .trash/ first, reversible for 30 days,\n" +
		"no token needed (general form: `bravros discard <paths>` for tracked\n" +
		"modifications, `bravros clean-untracked <paths>` for untracked files):\n\n" +
		"  " + v.sanctioned + "\n\n" +
		"If this destruction is genuinely intended, the operator mints a\n" +
		"single-use token from a SEPARATE terminal (Claude Code cannot mint it),\n" +
		"then the blocked command is re-run once:\n\n" +
		"  bravros destructive unlock --reason \"...\"\n\n" +
		"This is the safety floor (rule 52) — it fires even under stand-down."
}

// rule52ConsumeToken reads, validates and — when valid — CONSUMES (revokes)
// the destructive token (token.Gate{Name: "destructive"}, minted outside
// Claude Code by `bravros destructive unlock`). Returns true only when a
// non-expired token existed and was successfully revoked BEFORE the
// return (single-use invariant). No allow-without-consume path exists.
func rule52ConsumeToken() bool {
	gate := token.Gate{Name: "destructive"}
	tok := gate.Read()
	if tok == nil || tok.Expired() {
		os.Remove(gate.Path()) //nolint:errcheck // clean up malformed/expired token
		return false
	}
	if err := gate.Revoke(); err != nil {
		return false
	}
	return true
}

var policeUnlockCmd = &cobra.Command{
	Use:   "unlock",
	Short: "Mint a human-presence token to allow merging",
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getenv("CLAUDE_CODE_SESSION_ID") != "" || os.Getenv("CLAUDE_SESSION_ID") != "" {
			return fmt.Errorf("bravros police unlock MUST be run from a separate terminal, outside of Claude Code")
		}

		path := tokenPath()
		err := os.MkdirAll(filepath.Dir(path), 0755)
		if err != nil {
			return err
		}
		// Write token
		err = os.WriteFile(path, []byte("valid\n"), 0644)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "🔓 Police token minted. Agents can now merge to main.")
		return nil
	},
}

var policeRevokeCmd = &cobra.Command{
	Use:   "revoke",
	Short: "Revoke the human-presence token",
	RunE: func(cmd *cobra.Command, args []string) error {
		path := tokenPath()
		_ = os.Remove(path)
		fmt.Fprintln(cmd.OutOrStdout(), "🔒 Police token revoked.")
		return nil
	},
}

var policeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check police token status",
	RunE: func(cmd *cobra.Command, args []string) error {
		if hasValidPoliceToken() {
			fmt.Fprintln(cmd.OutOrStdout(), "🔓 Police token is VALID.")
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "🔒 Police token is MISSING or INVALID.")
		}
		return nil
	},
}

func tokenPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "state", "police-token")
}

func hasValidPoliceToken() bool {
	info, err := os.Stat(tokenPath())
	if err != nil {
		return false
	}
	// e.g. 10 minutes expiry
	if time.Since(info.ModTime()) > 10*time.Minute {
		os.Remove(tokenPath())
		return false
	}
	return true
}

func init() {
	policeCmd.AddCommand(policePreToolUseCmd)
	policeCmd.AddCommand(policeUnlockCmd)
	policeCmd.AddCommand(policeRevokeCmd)
	policeCmd.AddCommand(policeStatusCmd)
	rootCmd.AddCommand(policeCmd)
}

var policeStandDownCmd = &cobra.Command{
	Use:   "standdown",
	Short: "Manage Police stand-down state",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var standDownTTLFlag time.Duration

var policeStandDownOnCmd = &cobra.Command{
	Use:   "on",
	Short: "Enable Police stand-down for this session",
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID := resolveSession()
		if sessionID == "" {
			fmt.Fprintln(cmd.ErrOrStderr(), "police standdown on: no agent session detected. (Use env BRAVROS_POLICE_STANDDOWN=1 outside Claude)")
			return nil
		}
		ttl := standDownTTLFlag
		if ttl <= 0 {
			ttl = 4 * time.Hour
		}

		path := standDownPath(sessionID)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}

		now := time.Now().UTC()
		type marker struct {
			SessionID string    `json:"session_id"`
			ExpiresAt time.Time `json:"expires_at"`
			CreatedAt time.Time `json:"created_at"`
			TTL       string    `json:"ttl"`
		}
		m := marker{
			SessionID: sessionID,
			ExpiresAt: now.Add(ttl),
			CreatedAt: now,
			TTL:       ttl.String(),
		}
		data, _ := json.MarshalIndent(m, "", "  ")

		// write via temp file
		tmpPath := path + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0644); err != nil {
			return err
		}
		if err := os.Rename(tmpPath, path); err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "✓ Police stand-down ON for this session (TTL %s), expires %s.\nSafety floor stays active: irreversible content loss (rule 52).\n", ttl, m.ExpiresAt.Format(time.RFC3339))
		return nil
	},
}

var policeStandDownOffCmd = &cobra.Command{
	Use:   "off",
	Short: "Disable Police stand-down for this session",
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID := resolveSession()
		if sessionID == "" {
			return nil
		}
		path := standDownPath(sessionID)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "✓ Police stand-down marker cleared.")
		return nil
	},
}

var policeStandDownStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Report Police stand-down status",
	RunE: func(cmd *cobra.Command, args []string) error {
		out := struct {
			Active      bool   `json:"active"`
			Source      string `json:"source"`
			SessionID   string `json:"session_id"`
			ExpiresAt   string `json:"expires_at,omitempty"`
			SafetyFloor string `json:"safety_floor"`
		}{
			SessionID:   resolveSession(),
			SafetyFloor: "Safety floor stays active: irreversible content loss (rule 52).",
		}

		if os.Getenv("BRAVROS_POLICE_STANDDOWN") == "1" {
			out.Active = true
			out.Source = "env"
			emitStandDownStatus(cmd.OutOrStdout(), out)
			return nil
		}

		sessionID := resolveSession()
		if sessionID != "" {
			path := standDownPath(sessionID)
			data, err := os.ReadFile(path)
			if err == nil {
				var m struct {
					SessionID string    `json:"session_id"`
					ExpiresAt time.Time `json:"expires_at"`
				}
				if json.Unmarshal(data, &m) == nil && m.SessionID == sessionID {
					if !m.ExpiresAt.IsZero() && time.Now().Before(m.ExpiresAt) {
						out.Active = true
						out.Source = "marker"
						out.ExpiresAt = m.ExpiresAt.Format(time.RFC3339)
					} else {
						os.Remove(path) // auto-clean expired
					}
				}
			}
		}

		emitStandDownStatus(cmd.OutOrStdout(), out)
		return nil
	},
}

func emitStandDownStatus(out io.Writer, v interface{}) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Fprintln(out, string(data))
}

func standDownPath(sessionID string) string {
	tmpDir := os.TempDir()
	return filepath.Join(tmpDir, "agent-audit-"+sessionID, "standdown.json")
}

func isStandDownActive() bool {
	if os.Getenv("BRAVROS_POLICE_STANDDOWN") == "1" {
		return true
	}
	sessionID := resolveSession()
	if sessionID == "" {
		return false
	}
	data, err := os.ReadFile(standDownPath(sessionID))
	if err != nil {
		return false
	}
	var m struct {
		SessionID string    `json:"session_id"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if json.Unmarshal(data, &m) == nil && m.SessionID == sessionID {
		if !m.ExpiresAt.IsZero() && time.Now().Before(m.ExpiresAt) {
			return true
		}
	}
	return false
}

func init() {
	policeStandDownOnCmd.Flags().DurationVar(&standDownTTLFlag, "ttl", 4*time.Hour, "Stand-down marker TTL")
	policeStandDownCmd.AddCommand(policeStandDownOnCmd)
	policeStandDownCmd.AddCommand(policeStandDownOffCmd)
	policeStandDownCmd.AddCommand(policeStandDownStatusCmd)
	policeCmd.AddCommand(policeStandDownCmd)
}
