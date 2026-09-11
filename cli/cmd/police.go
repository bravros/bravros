package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/bravros/bravros/cli/internal/config"
	"github.com/bravros/bravros/cli/internal/token"
	"github.com/bravros/bravros/cli/internal/trash"
	"github.com/spf13/cobra"
)

var policeCmd = &cobra.Command{
	Use:   "police",
	Short: "Bravros police engine (audit and enforcement)",
	Long: `PreToolUse gate for agent Bash commands.

Merge gate: pushes and server-side merges into main/master need an out-of-band
token (bravros police unlock, or bravros promote unlock) — EXCEPT the staging
lane: a same-repo ` + "`gh pr merge <N>`" + ` whose head is the repo's staging branch
(staging_branch, default homolog), whose base is protected, whose GitHub
mergeStateStatus is CLEAN and which runs with no .planning/.auto-*-lock present
is allowed without a token and recorded in ~/.claude/state/police-merge-audit.log.
Per-repo knob: police.staging_lane = open (default) | reviewed | off.

Direct-to-main repos declare it with: bravros police direct-main on.

Safety floor — never suppressible by stand-down, no token lifts it:
  rule 52          irreversible content loss (git checkout/restore/reset --hard/
                   clean/stash drop, recursive rm over never-committed content)
  AI signature     AI attribution on commit messages, PR titles and bodies
  gate inputs      a Bash WRITE to a file the gate itself reads: the tokens under
                   ~/.claude/state (police-token, promote-token, destructive-token),
                   .planning/.review-stamp-<N>.json, and .bravros/config.json.
                   Reads (cat/ls/stat, bravros police status) and revokes pass; the
                   sanctioned writers are bravros police unlock / promote unlock /
                   destructive unlock (outside Claude Code), bravros pr-review
                   --write-stamp, bravros police direct-main / init / config, and
                   the Write/Edit tools — commit the config change.`,
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
			// Claude Code sends the session's working directory as top-level
			// "cwd". The merge gate uses it for exactly one thing: recognising
			// `cd <cwd> && …` as a no-op relocation (paylog 2aaa8ca8, 07:11).
			// Decoded tolerantly: a non-string cwd must not make the whole
			// Unmarshal fail — that was a fail-open on every gate in this file.
			Cwd json.RawMessage `json:"cwd"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			// The shape is not the documented one. If it still names Bash, it is
			// a Bash call this hook could not read — and a command it cannot
			// read is a command it has not cleared. Any other tool passes.
			if payloadNamesBash(data) {
				return writePoliceDeny(cmd.OutOrStdout(), unparseablePayloadBlock)
			}
			return nil
		}
		cwd := ""
		_ = json.Unmarshal(payload.Cwd, &cwd) // non-string cwd → "" (carve-out off)

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
		//            checkAiSignature — AI attribution on commits and PR bodies;
		//            checkGateInputWrite — Bash writes to the gate's own inputs.
		//   The floor is "never suppressible", not "content loss" alone: the
		//   zero-AI-attribution rule is absolute, so it may not sit below the
		//   stand-down divider.
		//   Nothing in this section may call isStandDownActive().
		if v := rule52Check(command); v != nil {
			if !rule52ConsumeToken() {
				return writePoliceDeny(cmd.OutOrStdout(), rule52BlockMessage(v))
			}
			// This token authorizes destructive work only; other gates still run.
		}
		if msg := checkAiSignature(command); msg != "" {
			return writePoliceDeny(cmd.OutOrStdout(), msg)
		}
		//   Member: checkGateInputWrite — a Bash write to the files the gates
		//   below READ (tokens, review stamps, .bravros/config.json). No token
		//   lifts it: a token that could authorise forging a token is no gate.
		if msg := checkGateInputWrite(command); msg != "" {
			return writePoliceDeny(cmd.OutOrStdout(), msg)
		}
		// ───── STAND-DOWN-SUPPRESSIBLE RULES ─────
		laneAudit := ""
		switch verdict, detail := evaluateMergeGateIn(command, cwd); verdict {
		case mergeAllowed:
		case mergeStagingLane:
			// Allowed without a token — but recorded. The audit line is the
			// detective control that replaces the per-merge token for this shape.
			laneAudit = detail
		default:
			if isStandDownActive() {
				return nil
			}
			if !hasValidMergeToken() {
				return writePoliceDeny(cmd.OutOrStdout(), mergeBlockMessage(verdict, detail))
			}
		}
		if !isStandDownActive() {
			if msg := checkPrCommentBody(command); msg != "" {
				return writePoliceDeny(cmd.OutOrStdout(), msg)
			}
		}
		if laneAudit != "" {
			auditLaneMerge(laneAudit)
		}
		return nil
	},
}

// protectedBranches are the only branches the Police gate defends. Everything
// else — homolog included — is routinely pushed and merged by /push, /hotfix,
// /finish, /batch-merge-prs and /auto-pr, and must never need a token.
var protectedBranches = map[string]bool{"main": true, "master": true}

// mergeVerdict is the merge gate's answer. Two-state was the bug:
// "could not determine the base" and "base is not protected" were both spelled
// `false`, so an unresolvable lookup silently read as approval (B-0036).
type mergeVerdict int

const (
	mergeAllowed       mergeVerdict = iota // resolved, targets nothing protected
	mergeProtected                         // resolved, targets a protected branch
	mergeIndeterminate                     // the target could not be determined
	mergeUnenforced                        // the command disables, or outruns, the pre-push hook
	mergeStagingLane                       // allowed without a token via the staging lane; audited
)

// evaluateMergeGate reports whether cmd would reach a protected branch.
//
// Matching is word-boundary based, not substring: `git push origin
// fix/maintain-cache` contains "main" but targets nothing protected. A bare
// `git push` is resolved against the current branch; `gh pr merge` and the
// `gh api` merge routes are resolved against the PR's real base — none of
// which is knowable from the string alone.
func evaluateMergeGate(cmd string) (mergeVerdict, string) {
	return evaluateMergeGateIn(cmd, "")
}

// evaluateMergeGateIn is evaluateMergeGate with the session's working directory,
// as the PreToolUse payload reports it. cwd is used ONLY to recognise
// `cd <cwd>` as a relocation that goes nowhere; "" disables that carve-out.
func evaluateMergeGateIn(cmd, cwd string) (mergeVerdict, string) {
	verdict, detail := mergeAllowed, ""
	// Lane merges on this line, remembered separately: they never excuse a
	// later definite hit (`gh pr merge 12 --merge && git push origin main` must
	// still block), and they surface only when nothing else did.
	var lane []string

	// Expand before anything reads the segments: every scan below walks tokens,
	// and a quoted payload is one token until it is re-tokenized.
	segs := expandInlineScripts(commandSegments(cmd), 4)

	// Every scan below anchors on the words `gh` and `git`. A shell function or
	// alias of that name rebinds the word for the rest of the line, so nothing
	// the anchors see is what runs: `gh(){ command gh "$@" -R o/r; }; gh pr
	// merge 5` merged into ANOTHER repo through the same-repo lane (adversarial
	// review, blocker 1). Not modelled — refused outright.
	if redefinesGitOrGH(cmd, codePayloads(segs)) {
		return mergeIndeterminate, redefinesDetail
	}

	// Does anything on this line move the command somewhere else? Detection is
	// enough — the gate no longer needs to resolve WHERE, because `git push` is
	// enforced where it lands, by that repo's own pre-push hook. Only the gh
	// merge routes, which perform no push, still care (see below).
	relocates := commandRelocates(segs, cwd)
	ghContextChanged := commandGHContextChanged(segs)

	for _, seg := range segs {
		// Strip the `VAR=x` prefix here rather than leaving it to the scans
		// below: an unstripped assignment is mistaken for a push destination
		// or a refspec.
		bare := stripEnvPrefix(seg)

		var (
			v mergeVerdict
			d string
			t string
		)
		// `"$(command -v gh)" pr merge 5 -R o/r` and `$GH push origin main`
		// name the program through an expansion the shell performs after this
		// gate has answered; no anchor can match it. The verb words that follow
		// are what make it a merge route rather than an unrelated expansion.
		if substitutedProgram(bare) {
			v, d, t = mergeIndeterminate, substitutedProgramDetail, relocatedPushTarget
		}
		switch push, hookless := gitPushArgs(bare); {
		case v != mergeAllowed:
			// already decided above
		case push != nil:
			if gitPushDryRun(push) {
				continue
			}
			if why, skipped := disablesPrePush(seg, push); skipped {
				v, d = mergeUnenforced, "this push would run with the pre-push hook disabled ("+why+")"
				break
			}
			protected, explicit := pushTargetsProtected(push)
			if !explicit {
				protected = defaultPushTargetsProtected(push)
			}
			switch {
			case hookless && len(gitPushPositionals(push)) <= 1:
				v, d = mergeUnenforced, "this is a plumbing push, which no pre-push hook ever sees"
			case protected:
				v, d, t = mergeProtected, "pushing to a protected branch", pushTargetRepo(push)
				if relocates || pushConfigOverridden(seg) {
					t = relocatedPushTarget
				}
			case hookless && !explicit:
				v, d = mergeUnenforced, "this is a plumbing push, which no pre-push hook ever sees"
			case !explicit && pushConfigOverridden(seg):
				v, d = mergeIndeterminate, "push configuration is changed by this command"
			}

		default:
			// gh merges reach a protected branch with no git push at all, so
			// no hook ever sees them (B-0036). This arm is the only reason the
			// gate reads command text, and it is where fail-closed belongs.
			if merge := ghArgs(bare, "pr", "merge"); merge != nil {
				// Context and relocation are decided from the command string
				// alone, BEFORE the forge lookup: a merge the gate will refuse
				// anyway must not pay a 5 s round-trip to find that out.
				switch {
				case ghContextChanged:
					v, d, t = mergeIndeterminate, "merging with a forge context changed by this command", relocatedPushTarget
				case relocates && ghFlagValue(merge, "--repo", "-R") == "":
					v, d, t = mergeIndeterminate, "merging a pull request in a directory this command changes", relocatedPushTarget
				default:
					v, d, t = mergePRVerdict(merge)
				}
			} else if api := ghArgs(bare, "api"); api != nil {
				v, d, t = apiMergeVerdict(api)
				if ghContextChanged && ghAPIMethod(api) != "GET" && ghAPIMethod(api) != "HEAD" && (v != mergeAllowed || apiPRMergeRoute(api)) {
					v, d, t = mergeIndeterminate, "merging with a forge context changed by this command", relocatedPushTarget
				}
				// Only a `{owner}/{repo}` endpoint takes its repo from the
				// working directory. A spelled-out path names the repo outright,
				// so a `cd` cannot redirect it.
				if v == mergeAllowed && relocates && ghAPIMethod(api) != "GET" && ghAPIMethod(api) != "HEAD" &&
					strings.Contains(ghAPIEndpoint(api), "{") &&
					ghFlagValue(api, "--repo", "-R") == "" {
					v, d, t = mergeIndeterminate, "merging in a repo named by a directory this command changes", relocatedPushTarget
				}
			}
		}

		// An opt-out excuses only this operation, never the remaining commands.
		if v == mergeProtected && policeDirectMainAllowed(t) {
			continue
		}
		// A lane merge is allowed for THIS segment only; keep scanning.
		if v == mergeStagingLane {
			lane = append(lane, d)
			continue
		}
		// A definite hit anywhere in the command line decides it outright;
		// a softer one is remembered but keeps scanning for a definite.
		if v == mergeProtected || v == mergeUnenforced {
			verdict, detail = v, d
			break
		}
		if v == mergeIndeterminate && verdict == mergeAllowed {
			verdict, detail = v, d
		}
	}

	if verdict == mergeAllowed {
		// A merge route reached over plain HTTP carries no git or gh token, so
		// none of the command scans above can see it.
		if route, found := httpMergeRoute(segs); found {
			return mergeIndeterminate, "a request to " + route
		}
		// Last resort: nothing matched as a command, but a payload this gate
		// hands to an interpreter spells one out. Not parseable, not clearable.
		if marker, found := embeddedMergeMarker(segs); found {
			return mergeIndeterminate, "code passed to an interpreter that spells out `" + marker + "`"
		}
		if len(lane) > 0 {
			return mergeStagingLane, strings.Join(lane, "; ")
		}
	}
	return verdict, detail
}

// codePayloadFlags are the flags whose following argument is EXECUTED rather
// than read as data — `bash -c`, `zsh -lc`, `node -e`, and friends.
//
// Keyed on the flag, not the program, so an interpreter nobody listed is still
// covered. `-m` is deliberately absent: a commit message is data, and
// `git commit -m "fix; git push origin main"` must stay allowed.
var codePayloadFlags = map[string]bool{
	"-c": true, "-lc": true, "-ic": true, "-lic": true, "-ilc": true,
	"--command": true, "--eval": true,
}

// interpreterEvalFlags are the other spellings of "run this string".
var interpreterEvalFlags = map[string]bool{"-e": true, "--execute": true}

// codeRunners are the programs that execute a string argument. Every payload
// flag is gated on one being named earlier in the segment, because each of
// those flags means something else entirely elsewhere: `-e` is grep's and rg's
// pattern and sed's script, `-c` is grep's count. Reading those as code blocked
// `grep -e "gh pr merge" notes.md`, `rg -e 'git push' docs/`, `sed -i -e …` and
// `grep -c "git push" f.md` (round 12 adversarial review).
var codeRunners = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "fish": true,
	"node": true, "deno": true, "bun": true,
	"perl": true, "ruby": true, "python": true, "python3": true, "php": true,
	"osascript": true, "awk": true,
}

// isCodePayloadFlag reports whether seg[i] introduces an executed payload.
func isCodePayloadFlag(seg []string, i int) bool {
	f := unquote(seg[i])
	if f == "eval" {
		return true // a shell builtin: no program token to look for
	}
	if !codePayloadFlags[f] && !interpreterEvalFlags[f] {
		return false
	}
	for j := 0; j < i; j++ {
		if codeRunners[filepathBase(unquote(seg[j]))] {
			return true
		}
	}
	return false
}

// filepathBase is filepath.Base without importing it into every call site's
// mental model: `/usr/bin/python3` is still python3.
func filepathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// expandInlineScripts appends the segments of any argument the command would
// execute as code, so the gate sees the real invocation inside it.
//
// commandSegments splits on unquoted separators only, so a quoted payload
// arrives as ONE opaque token: `bash -c "gh pr merge 2044 --merge"` tokenizes to
// ["bash", "-c", "gh pr merge 2044 --merge"], where no token equals "gh" and
// every scan in this file walks straight past it. That was an unconditional
// bypass of the gh merge routes — which have no hook behind them — and, with
// --no-verify inside the quotes, of the pre-push hook as well, since the nested
// shell really does pass it to git (PR 90 round 10).
//
// Appends rather than replaces: the outer tokens stay visible, and the inner
// ones become segments in their own right, so relocation, hook-tampering and
// merge-route scans all reach inside. Depth-capped — a payload can nest.
func expandInlineScripts(segs [][]string, depth int) [][]string {
	if depth <= 0 {
		return segs
	}
	out := segs
	for _, seg := range segs {
		for i := range seg {
			if !isCodePayloadFlag(seg, i) || i+1 >= len(seg) {
				continue
			}
			payload := unquote(seg[i+1])
			// A single word is an option value, a filename or a ref — nothing
			// with a command in it. Re-tokenizing it would only duplicate work.
			if !strings.ContainsAny(payload, " \t\n") {
				continue
			}
			out = append(out, expandInlineScripts(commandSegments(payload), depth-1)...)
		}
	}
	return out
}

// codePayloads returns every argument the command would execute as code.
func codePayloads(segs [][]string) []string {
	var out []string
	for _, seg := range segs {
		for i := range seg {
			if !isCodePayloadFlag(seg, i) || i+1 >= len(seg) {
				continue
			}
			if p := unquote(seg[i+1]); strings.ContainsAny(p, " \t\n") {
				out = append(out, p)
			}
		}
	}
	return out
}

// payloadParsesAsCommands reports whether re-tokenizing payload as shell yields
// a recognisable git/gh invocation — i.e. whether expandInlineScripts turned it
// into segments the real arms could judge.
//
// Shell payloads do; another language's string literal does not, which is
// exactly the split that decides whether the substring scan is needed.
func payloadParsesAsCommands(payload string) bool {
	for _, seg := range commandSegments(payload) {
		if push, _ := gitPushArgs(seg); push != nil {
			return true
		}
		if ghArgs(seg, "pr", "merge") != nil || ghArgs(seg, "api") != nil {
			return true
		}
	}
	return false
}

// embeddedMergeMarker reports a merge command spelled inside a code payload
// that re-tokenizing could not surface as its own tokens.
//
// expandInlineScripts handles a shell payload properly, because shell syntax is
// what commandSegments parses. It cannot reach a command buried in another
// language's string literal — `node -e "…execSync('gh pr merge 5 --merge')…"`
// re-tokenizes to one blob with no standalone "gh". Parsing JavaScript, then
// Python, then Perl to find out is the treadmill this gate has been climbing
// out of since round 7.
//
// So this does not parse: it looks. A payload that spells a merge command is
// one the gate cannot clear, whatever language wraps it. Scoped to code
// payloads, never to ordinary arguments, so a commit message quoting a command
// stays untouched.
func embeddedMergeMarker(segs [][]string) (string, bool) {
	for _, payload := range codePayloads(segs) {
		// A payload expandInlineScripts could genuinely re-tokenize has already
		// been judged by the arms above, on its real argv. Re-blocking it here
		// on a substring would override that with a worse answer:
		// `bash -c "git fetch && git push"` is an ordinary deferred push, and
		// the marker scan alone called it indeterminate (review finding 6).
		if payloadParsesAsCommands(payload) {
			continue
		}
		// Punctuation to spaces first, so a call built as a list reads the same
		// as one written as a command string: subprocess.run(['gh','api',…])
		// spells "gh api" only once the quotes and commas are gone.
		flat := strings.Join(strings.Fields(strings.Map(func(r rune) rune {
			switch r {
			case '\'', '"', ',', '[', ']', '(', ')':
				return ' '
			}
			return r
		}, payload)), " ")
		for _, marker := range []string{"gh pr merge", "gh api", "git push"} {
			if strings.Contains(flat, marker) {
				return marker, true
			}
		}
	}
	return "", false
}

// mergeMutations are the GraphQL mutations that land a pull request. Shared by
// the gh-api arm and the raw-HTTP scan, so a mutation added in one place can
// never be missing from the other.
var mergeMutations = []string{
	"mergePullRequest",
	"mergeBranch",
	"enablePullRequestAutoMerge",
	"enqueuePullRequest",
}

// httpMergeRoute reports whether any segment addresses a forge merge endpoint
// over plain HTTP, naming the marker that matched.
//
// `gh` is a convenience, not a gatekeeper: the same PUT /pulls/{n}/merge and the
// same GraphQL mutation are one curl away, with a token `gh auth token` will
// hand over. That reaches B-0036's exact routes carrying neither a "git" nor a
// "gh" token, so every scan in this file walked past it (PR 90 round 11).
//
// Matched on the ROUTE, not the client, so python's requests or node's fetch
// are covered as readily as curl — the URL is the part that cannot be omitted.
// Read-only calls to the same host stay untouched: only a merge path, or a
// GraphQL endpoint alongside a merge mutation, matches.
func httpMergeRoute(segs [][]string) (string, bool) {
	for _, seg := range segs {
		if ghArgs(stripEnvPrefix(seg), "api") != nil {
			continue
		}
		// Expanded shell payloads have their own segments below. Re-reading the
		// opaque outer string would override the API arm's resolved answer.
		expanded := false
		for _, payload := range codePayloads([][]string{seg}) {
			if payloadParsesAsCommands(payload) {
				expanded = true
				break
			}
		}
		if expanded || !httpWrites([][]string{seg}) {
			continue
		}
		// Scoped to one segment: a GraphQL URL in one command and the word
		// "mergePullRequest" in an unrelated one are not the same request.
		graphql, mutation := false, ""
		for _, raw := range seg {
			u := unquote(raw)
			if strings.Contains(u, "://") || strings.Contains(u, "api.github.com") {
				lower := strings.ToLower(u)
				switch {
				case strings.Contains(lower, "/pulls/") && strings.Contains(lower, "/merge"):
					return "a pull-request merge endpoint", true
				case strings.HasSuffix(strings.TrimRight(lower, "/"), "/merges") ||
					strings.Contains(lower, "/merges?"):
					return "a branch merge endpoint", true
				case strings.Contains(lower, "/graphql"):
					graphql = true
				}
			}
			for _, m := range mergeMutations {
				if strings.Contains(u, m) {
					mutation = m
				}
			}
		}
		if graphql && mutation != "" {
			return "a GraphQL " + mutation + " mutation", true
		}
	}
	return "", false
}

// httpWrites reports whether the request would use a write method. curl and
// friends GET by default, and `curl -s <merge-route>` is how you READ a merge
// state — blocking it was an over-block with no merge behind it.
//
// For curl/wget, data and upload flags select a default write method; explicit
// methods and read options take precedence. Other inline clients retain the
// conservative method-marker backstop below.
func httpWrites(segs [][]string) bool {
	for _, seg := range segs {
		client, start := "", 0
		for i, raw := range seg {
			name := filepathBase(unquote(raw))
			if name == "curl" || name == "wget" {
				client, start = name, i+1
				break
			}
		}
		if client != "" {
			// Track request semantics, not substrings inside argument values. curl's
			// explicit method wins over data/upload defaults, and --get keeps data a
			// read. --next starts an independent request in the same invocation.
			method, body, upload, get, head := "", false, false, false, false
			writes := func() bool {
				if method != "" {
					return httpWriteMethod(method)
				}
				return !get && !head && (body || upload)
			}
			valueFlags := map[string]bool{
				"--header": true, "--output": true, "--user-agent": true, "--user": true,
				"--proxy": true, "--proxy-user": true, "--referer": true, "--cookie": true,
				"--cookie-jar": true, "--url": true, "--url-query": true, "--request-target": true,
				"--write-out": true, "--dump-header": true, "--config": true,
				"--connect-timeout": true, "--max-time": true, "--retry": true,
				"--retry-delay": true, "--retry-max-time": true, "--resolve": true,
				"--connect-to": true, "--cacert": true, "--capath": true,
				"--cert": true, "--key": true, "--pass": true, "--range": true,
				"--limit-rate": true, "--interface": true, "--proxy-header": true,
				"--output-document": true, "--directory-prefix": true,
			}
			dataFlags := map[string]bool{
				"--data": true, "--data-raw": true, "--data-binary": true,
				"--data-urlencode": true, "--data-ascii": true, "--json": true,
				"--form": true, "--form-string": true,
				"--post-data": true, "--post-file": true, "--body-data": true, "--body-file": true,
			}
			literal := false
			for i := start; i < len(seg); i++ {
				option := unquote(seg[i])
				if literal {
					continue
				}
				if option == "--" {
					literal = true
					continue
				}
				if client == "curl" && (option == "--next" || option == "-:") {
					if writes() {
						return true
					}
					method, body, upload, get, head = "", false, false, false, false
					continue
				}
				if strings.HasPrefix(option, "--") {
					name, value, attached := strings.Cut(option, "=")
					takesValue := valueFlags[name] || dataFlags[name] || name == "--upload-file" || name == "--request" || name == "--method"
					if takesValue && !attached && i+1 < len(seg) {
						i++
						value = unquote(seg[i])
					}
					switch {
					case name == "--request" || name == "--method":
						method = unquote(value)
					case name == "--get":
						get = true
					case name == "--no-get":
						get = false
					case name == "--head" || name == "--spider":
						head = true
					case name == "--no-head":
						head = false
					case dataFlags[name]:
						body = true
					case name == "--upload-file":
						upload = true
					}
					continue
				}
				if client == "wget" && strings.HasPrefix(option, "-") && len(option) >= 2 {
					if strings.ContainsRune("OoaPUetTwlDARIXi", rune(option[1])) && len(option) == 2 && i+1 < len(seg) {
						i++
					}
					continue
				}
				if client == "curl" && strings.HasPrefix(option, "-") {
					// Short options can be clustered (-sSd...), and a value-taking one
					// consumes the cluster remainder or the next argv element verbatim.
					for j := 1; j < len(option); j++ {
						flag := option[j]
						takesValue := strings.ContainsRune("dFTXHoAUuxbecrwyDCmzEQK", rune(flag))
						value := ""
						if takesValue {
							value = option[j+1:]
							if value == "" && i+1 < len(seg) {
								i++
								value = unquote(seg[i])
							}
						}
						switch flag {
						case ':':
							if writes() {
								return true
							}
							method, body, upload, get, head = "", false, false, false, false
						case 'X':
							method = unquote(value)
						case 'd', 'F':
							body = true
						case 'T':
							upload = true
						case 'G':
							get = true
						case 'I':
							head = true
						}
						if takesValue {
							break
						}
					}
				}
			}
			if writes() {
				return true
			}
			continue
		}
		// Preserve the existing library-client backstop for inline code payloads.
		// curl/wget argv never reaches this heuristic: their option values are data.
		for _, raw := range seg {
			u := unquote(raw)
			if httpWriteMethod(u) {
				return true
			}
			lower := strings.ToLower(u)
			for _, m := range []string{"put(", "post(", "patch(", "delete(",
				"'put'", "\"put\"", "'post'", "\"post\"", "'patch'", "\"patch\"", "'delete'", "\"delete\""} {
				if strings.Contains(lower, m) {
					return true
				}
			}
		}
	}
	return false
}

func httpWriteMethod(m string) bool {
	switch strings.ToUpper(strings.TrimSpace(m)) {
	case "PUT", "POST", "PATCH", "DELETE":
		return true
	}
	return false
}

// commandRelocates reports whether any segment moves the command to another
// directory, or repoints git at another repo or configuration — a `cd`/`pushd`,
// git's -C/--git-dir/--work-tree, or the GIT_DIR/GIT_CONFIG_* environment.
//
// Detection only, deliberately. Resolving the destination meant modelling cd
// semantics, subshell scoping and shell expansion, and every one of PR 90's six
// review rounds found a hole in some part of that model. The push arm no longer
// needs the answer at all; the gh arms need only to know the question exists,
// and fail closed when it does.
func commandRelocates(segs [][]string, cwd string) bool {
	for _, seg := range segs {
		if segmentRunsCd(seg) && !cdIsNoOp(seg, cwd) {
			return true
		}
		// Scanned UNSTRIPPED: git's own environment relocates a push just as
		// -C does, and an env prefix is exactly what stripEnvPrefix removes.
		// `GIT_DIR=<other>/.git git push` really does push the other repo —
		// verified against git 2.50.1 from an unrelated working directory
		// (found while fixing PR 90 round 9).
		for _, raw := range seg {
			name, _, _ := strings.Cut(unquote(raw), "=")
			// Git accepts both -C dir and -Cdir. The attached spelling must
			// not borrow the session repo's direct_main authorization.
			if strings.HasPrefix(name, "-C") {
				return true
			}
			switch name {
			case "-C", "--git-dir", "--work-tree",
				"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY",
				"GIT_CONFIG", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM":
				return true
			}
		}
	}
	return false
}

// hookBypassMarkers are the tokens that stop the pre-push hook from running, or
// from blocking: git's own --no-verify, a redirected core.hooksPath (by -c, by
// `git config`, or via the GIT_CONFIG_* environment), and the hook's documented
// escape hatch.
//
// Note `-n` is NOT here: on `git push` it means --dry-run, which runs no push
// at all. Treating it as --no-verify would block a routine rehearsal.
func disablesPrePush(seg, push []string) (string, bool) {
	// --no-verify is read from the PUSH's own argv: `git commit --no-verify &&
	// git push origin feature` skips the commit hooks, not the pre-push hook.
	for _, raw := range push {
		if unquote(raw) == "--no-verify" {
			return "--no-verify", true
		}
	}
	// The rest are read from the push's own SEGMENT — which still holds git's
	// global options and any env prefix, both stripped out of `push` itself.
	// A `git config core.hooksPath …` in some other segment is not chased: that
	// is the hook-protection machinery this arm deliberately dropped.
	for _, raw := range seg {
		u := unquote(raw)
		switch {
		case strings.HasPrefix(u, "BRAVROS_ALLOW_PROTECTED_PUSH="):
			return "BRAVROS_ALLOW_PROTECTED_PUSH", true
		// Lowercased: git config keys are case-insensitive, so
		// `-c core.hookspath=/dev/null` redirects hooks exactly as the
		// camelCase spelling does (verified against git 2.50.1).
		case strings.Contains(strings.ToLower(u), "core.hookspath"):
			return "core.hooksPath", true
		}
	}
	return "", false
}

// redefinesDetail is the indeterminate detail for a command that rebinds the
// words every scan in this file anchors on.
const redefinesDetail = "the command redefines gh/git"

// substitutedProgramDetail is the indeterminate detail for a merge or push
// whose program is named by an expansion the shell resolves after the gate.
const substitutedProgramDetail = "the program is named by a substitution"

// prUnreadableDetail is the indeterminate detail for `gh pr merge "$PR"`: the
// PR argument holds a variable, not a number, so no lookup can run.
const prUnreadableDetail = "merging a pull request whose number could not be read"

var (
	// A function definition of gh or git, in either spelling: `gh() {`,
	// `gh () {`, `function gh {`, `function gh() {`. The leading class keeps
	// `regh()` and `mygit()` out; `{` is in it because `{ gh(){ …; }; }` is
	// legal and commandSegments does not split on braces.
	shellFuncRE = regexp.MustCompile(`(?:^|[;&|(\s{])(?:function\s+(?:gh|git)\b|(?:gh|git)\s*\(\s*\))`)
	// `alias gh=…`, `alias git=…` — bash expands aliases in scripts only after
	// `shopt -s expand_aliases`, but zsh does so by default, and the agent
	// shell is zsh.
	shellAliasRE = regexp.MustCompile(`(?:^|[;&|(\s{])alias\s+(?:gh|git)=`)
	// The documented PreToolUse shape names the tool as "tool_name": "Bash".
	// Matched on the raw bytes so a payload json.Unmarshal rejected can still
	// be recognised as a Bash call and failed closed.
	payloadBashRE = regexp.MustCompile(`"tool_name"\s*:\s*"[Bb]ash"`)
)

// redefinesGitOrGH reports whether the raw command, or any executed payload
// inside it, defines a function or alias named gh or git.
//
// Scanned on the raw text rather than the tokens: `gh(){` tokenizes to `gh` and
// `{` with the parens dropped, which is indistinguishable from an ordinary
// `gh` invocation. The payloads are scanned too so an `eval` or `bash -c`
// string that carries the definition is caught even when quoting keeps it out
// of the raw scan's word boundaries.
func redefinesGitOrGH(raw string, payloads []string) bool {
	for _, text := range append([]string{raw}, payloads...) {
		if shellFuncRE.MatchString(text) || shellAliasRE.MatchString(text) {
			return true
		}
	}
	return false
}

// substitutedProgram reports whether a token of seg names the program through
// a shell expansion — `$(…)`, a backtick, `${…}` or a bare `$VAR` — and is
// followed by the words of a route this gate gates: `pr merge`, `api`, `push`.
//
// The gate anchors on the literal words gh and git; an expansion is resolved
// by the shell AFTER the hook has answered, so no anchor can ever see it. The
// following words are what tell a merge route apart from `"$(date)" push` in
// an unrelated program's argv — the false-positive cost is a token, the
// false-negative cost is the branch.
func substitutedProgram(seg []string) bool {
	for i, raw := range seg {
		tok := unquote(raw)
		if !strings.HasPrefix(tok, "$") && !strings.Contains(tok, "$(") && !strings.Contains(tok, "${") && !strings.Contains(tok, "`") {
			continue
		}
		if strings.Contains(tok, "=") && !strings.Contains(tok, "$(") && !strings.Contains(tok, "`") {
			continue // `FOO=$BAR gh …` is an env prefix, not a program
		}
		rest := seg[i+1:]
		switch {
		case len(rest) >= 2 && unquote(rest[0]) == "pr" && unquote(rest[1]) == "merge":
			return true
		case len(rest) >= 1 && (unquote(rest[0]) == "api" || unquote(rest[0]) == "push"):
			return true
		}
	}
	return false
}

// payloadNamesBash reports whether raw PreToolUse bytes the JSON decoder
// rejected still declare the Bash tool.
func payloadNamesBash(data []byte) bool {
	return payloadBashRE.Match(data)
}

const unparseablePayloadBlock = "✋🏽 Police Block: the PreToolUse payload for this Bash call could not be parsed, so the\n" +
	"command inside it could not be inspected. The gate fails closed rather than clearing a\n" +
	"command it never read. This usually means a field of an unexpected type (tool_input.command\n" +
	"or cwd); re-issue the command as an ordinary Bash call.\n"

// ---------------------------------------------------------------------------
// Gate-input writes — the safety-floor rule that keeps the other rules honest
//
// Every decision above reads a file an agent can also write from Bash: the
// police/promote/destructive tokens under ~/.claude/state (a `touch` mints
// one), `.planning/.review-stamp-<N>.json` (a `printf` forges "reviewed"), and
// `.bravros/config.json` (a redirect widens the staging lane or declares
// direct_main). Each was reproduced against a built binary (adversarial
// review, major 3). The hook cannot make the files unwritable; it can refuse
// to be the thing that writes them.
//
// Only WRITES are denied. Reading a token (`cat`, `ls`, `stat`), the read-only
// verbs (`bravros police status`, `bravros promote status`), revoking
// (`bravros police revoke`, `rm` of a token — revocation only ever removes
// authority) and the sanctioned writers (`bravros pr-review --write-stamp`,
// `bravros police direct-main`, `bravros init`, `bravros config`) all pass:
// a segment anchored on `bravros` is never a write here.
//
// Safety-floor member: never reads isStandDownActive(), and no token lifts it
// — a token that could authorise forging a token would not be a gate.
// ---------------------------------------------------------------------------

// gateInputKind classifies a gate-input path so the block can name the
// sanctioned writer for that kind.
type gateInputKind int

const (
	gateInputNone gateInputKind = iota
	gateInputToken
	gateInputStamp
	gateInputConfig
)

// gateInputRef classifies one token as a reference to a gate input. Substring
// on the unquoted token, so `~/`, `$HOME/`, `./`, absolute and quoted spellings
// all match; the bare token file names match too, so `cd ~/.claude/state &&
// touch police-token` cannot split the path across segments.
func gateInputRef(tok string) gateInputKind {
	t := strings.TrimRight(unquote(tok), "/")
	switch {
	case strings.Contains(t, ".claude/state/"), strings.Contains(t, ".agent_config/state/"),
		strings.HasSuffix(t, ".claude/state"), strings.HasSuffix(t, ".agent_config/state"):
		return gateInputToken
	case strings.Contains(t, ".review-stamp-"):
		return gateInputStamp
	case strings.Contains(t, ".bravros/config.json"):
		// The file itself, not a sibling (`config.json.example`), wherever it
		// sits inside the token (`open('.bravros/config.json','w')`).
		if strings.HasSuffix(gateInputPath(t), ".bravros/config.json") {
			return gateInputConfig
		}
	}
	switch filepathBase(t) {
	case "police-token", "promote-token", "destructive-token", "review-stamp-token":
		return gateInputToken
	}
	return gateInputNone
}

// gateInputPathRE pulls the gate path out of a token that carries more than
// the path — `open('/home/x/.claude/state/police-token','w')`, `of=…` — so the
// block names the file, not the program text around it.
var gateInputPathRE = regexp.MustCompile(`[^\s'"(),;=]*(?:\.claude/state|\.agent_config/state|\.review-stamp-|\.bravros/config\.json)[^\s'"(),;=]*`)

// gateInputPath returns the gate path spelled inside tok, or tok itself.
func gateInputPath(tok string) string {
	t := unquote(tok)
	if m := gateInputPathRE.FindString(t); m != "" {
		return m
	}
	return t
}

// gateInputDirRef classifies a copy DESTINATION that is the parent directory
// of a gate input, paired with a source whose basename is the gate file:
// `cp /tmp/config.json .bravros/` writes .bravros/config.json without either
// token spelling the full path.
func gateInputDirRef(dest, src string) gateInputKind {
	d := filepathBase(strings.TrimRight(unquote(dest), "/"))
	s := filepathBase(unquote(src))
	switch {
	case d == ".bravros" && s == "config.json":
		return gateInputConfig
	case d == ".planning" && strings.HasPrefix(s, ".review-stamp-"):
		return gateInputStamp
	case d == "state" && gateInputRef(s) == gateInputToken:
		return gateInputToken
	}
	return gateInputNone
}

// gateMessageFlags are the git/gh flags whose value is data, never a path:
// `git commit -m "fix police-token docs"` names a token file and writes
// nothing. Applied only to segments anchored on git or gh.
var gateMessageFlags = map[string]bool{
	"-m": true, "--message": true, "-b": true, "--body": true,
	"-t": true, "--title": true, "--subject": true, "--notes": true, "-F": true, "--body-file": true,
}

// checkGateInputWrite returns a block message when command would write one of
// the gate's own inputs from Bash, or "" when it may proceed.
func checkGateInputWrite(command string) string {
	// Cheap pre-filter: nothing here matches without one of these substrings.
	if !strings.Contains(command, "state") && !strings.Contains(command, "review-stamp") &&
		!strings.Contains(command, "config.json") && !strings.Contains(command, "-token") {
		return ""
	}
	segs := expandInlineScripts(commandSegments(command), 4)
	interpreter := false
	refKind, refPath := gateInputNone, ""
	for _, seg := range segs {
		if kind, path := segmentWritesGateInput(seg); kind != gateInputNone {
			return gateInputBlockMessage(kind, path)
		}
		if segmentRunsInterpreter(seg) {
			interpreter = true
		}
		if refKind == gateInputNone {
			if refs := gateRefsIn(seg); len(refs) > 0 {
				refKind, refPath = refs[0].kind, refs[0].path
			}
		}
	}
	// An interpreter program — `python3 -c '…'`, `perl -e`, `node -e`, or a
	// heredoc fed to `python3 -` — writes from inside a string this gate does
	// not parse. A gate path anywhere on the same line is enough.
	if interpreter && refKind != gateInputNone {
		return gateInputBlockMessage(refKind, refPath)
	}
	return ""
}

// gateRef is one token of a segment that names a gate input.
type gateRef struct {
	index int
	kind  gateInputKind
	path  string
}

// gateProgram returns the segment with wrappers (`sudo`, `env`, `command`,
// `time`, `nohup`, `exec`) and env assignments stripped, plus the program
// name that anchors it.
func gateProgram(seg []string) ([]string, string) {
	bare := stripEnvPrefix(seg)
	for len(bare) > 0 {
		switch filepathBase(unquote(bare[0])) {
		case "sudo", "command", "env", "exec", "nohup", "time", "builtin":
			bare = stripEnvPrefix(bare[1:])
			continue
		}
		break
	}
	if len(bare) == 0 {
		return nil, ""
	}
	return bare, filepathBase(unquote(bare[0]))
}

// gateRefsIn lists the tokens of seg that name a gate input, excluding the
// values of git/gh message flags and every segment anchored on bravros — the
// sanctioned verbs read and write these files by design.
func gateRefsIn(seg []string) []gateRef {
	bare, program := gateProgram(seg)
	if program == "" || program == "bravros" {
		return nil
	}
	gitOrGH := program == "git" || program == "gh"
	var out []gateRef
	for i := 0; i < len(bare); i++ {
		tok := unquote(bare[i])
		if gitOrGH && strings.HasPrefix(tok, "-") {
			name, _, attached := strings.Cut(tok, "=")
			switch {
			case gateMessageFlags[name] && !attached:
				i++ // detached value: `-m MSG`
			case gateMessageFlags[name]:
				// attached: `--message=MSG`
			case len(tok) > 2 && tok[1] != '-' && gateMessageFlags["-"+tok[len(tok)-1:]] && strings.Trim(tok[1:], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") == "":
				i++ // bundled: `-am MSG`
			case len(tok) > 2 && tok[1] != '-' && gateMessageFlags["-"+tok[1:2]]:
				// glued: `-mMSG`
			}
			continue
		}
		if kind := gateInputRef(tok); kind != gateInputNone {
			out = append(out, gateRef{i, kind, gateInputPath(tok)})
		}
	}
	return out
}

// segmentRunsInterpreter reports whether seg runs a non-shell interpreter —
// a program whose writes live inside a string argument or a heredoc.
func segmentRunsInterpreter(seg []string) bool {
	_, program := gateProgram(seg)
	if program == "" || program == "bravros" {
		return false
	}
	for _, raw := range seg {
		name := filepathBase(unquote(raw))
		if codeRunners[name] && !shellRunners[name] {
			return true
		}
	}
	return false
}

// shellRunners are the codeRunners whose payload commandSegments can parse;
// expandInlineScripts already turned it into segments of its own, so the
// outer token is transparent here.
var shellRunners = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "fish": true}

// segmentWritesGateInput reports which gate input, if any, one segment writes,
// and the token that named it.
//
// Three write shapes: a redirect whose target is the path; a program that
// writes a path argument in place (`touch`, `chmod`, `chown`, `truncate`,
// `tee`, `mv`, `rm` of the config, `sed -i`); and a copy whose destination is
// the path or its parent directory (`cp`, `ln`, `install`, `rsync`, `dd of=`).
// Reads — `cat`, `ls`, `stat`, `grep`, a `cp` FROM the path — are not writes.
func segmentWritesGateInput(seg []string) (gateInputKind, string) {
	refs := gateRefsIn(seg)
	bare, program := gateProgram(seg)
	if program == "" || program == "bravros" || shellRunners[program] {
		return gateInputNone, ""
	}

	// Redirects: `> path`, `>> path`, `>path`, `1>path`, `&>path`, `>|path`.
	for i, raw := range bare {
		tok := unquote(raw)
		op := strings.TrimLeft(tok, "0123456789&")
		if !strings.HasPrefix(op, ">") {
			continue
		}
		target := strings.TrimLeft(op, ">|")
		if target == "" && i+1 < len(bare) {
			target = unquote(bare[i+1])
		}
		if kind := gateInputRef(target); kind != gateInputNone {
			return kind, gateInputPath(target)
		}
	}
	if len(refs) == 0 {
		// A copy into the parent directory names no gate path outright.
		if len(bare) >= 3 {
			switch filepathBase(unquote(bare[0])) {
			case "cp", "ln", "install", "rsync", "mv":
				dest := bare[len(bare)-1]
				for _, src := range bare[1 : len(bare)-1] {
					if kind := gateInputDirRef(dest, src); kind != gateInputNone {
						return kind, strings.TrimRight(unquote(dest), "/") + "/" + filepathBase(unquote(src))
					}
				}
			}
		}
		return gateInputNone, ""
	}

	// Program names anywhere in the segment (`sudo touch …` is already
	// unwrapped; `xargs touch` is not, and this catches it).
	for i, raw := range bare {
		name := filepathBase(unquote(raw))
		switch name {
		case "touch", "chmod", "chown", "truncate", "tee", "mv":
			return refs[0].kind, refs[0].path
		case "rm", "unlink":
			// Deleting a token or a stamp only removes authority; deleting the
			// config file drops `staging_lane: off`, which widens the lane.
			for _, r := range refs {
				if r.kind == gateInputConfig {
					return r.kind, r.path
				}
			}
		case "cp", "ln", "install", "rsync":
			dest := bare[len(bare)-1]
			// Directory destination first, so the block names the file that
			// lands there rather than the directory.
			for _, src := range bare[i+1 : len(bare)-1] {
				if kind := gateInputDirRef(dest, src); kind != gateInputNone {
					return kind, strings.TrimRight(unquote(dest), "/") + "/" + filepathBase(unquote(src))
				}
			}
			if kind := gateInputRef(dest); kind != gateInputNone {
				return kind, gateInputPath(dest)
			}
		case "dd":
			for _, r := range refs {
				if strings.HasPrefix(unquote(bare[r.index]), "of=") {
					return r.kind, r.path
				}
			}
		case "sed":
			for _, later := range bare[i+1:] {
				f := unquote(later)
				if f == "--in-place" || strings.HasPrefix(f, "--in-place=") ||
					(strings.HasPrefix(f, "-") && !strings.HasPrefix(f, "--") && strings.ContainsRune(f, 'i')) {
					return refs[0].kind, refs[0].path
				}
			}
		}
	}
	return gateInputNone, ""
}

// gateInputBlockMessage renders the block, naming the path and the sanctioned
// writer for its kind.
func gateInputBlockMessage(kind gateInputKind, path string) string {
	head := "✋🏽 Police Block: this command writes the gate's own input (" + path + ").\n"
	var body string
	switch kind {
	case gateInputToken:
		body = "Merge and destructive tokens under ~/.claude/state are minted only OUTSIDE Claude Code —\n" +
			"bravros police unlock, bravros promote unlock, bravros destructive unlock — by the operator.\n" +
			"Read-only access is fine (bravros police status, bravros promote status, cat/ls/stat), and so\n" +
			"is revoking (bravros police revoke, bravros promote revoke). Writing one from Bash is never.\n"
	case gateInputStamp:
		body = "Review stamps (.planning/.review-stamp-<N>.json) are written only by\n" +
			"  bravros pr-review <N> --write-stamp\n" +
			"which keys them to the reviewed commit. A hand-written stamp is a forged review.\n"
	case gateInputConfig:
		body = ".bravros/config.json declares the merge gate's per-repo policy (staging_lane, direct_main,\n" +
			"staging_branch). Edit it with the Write tool or bravros police direct-main / bravros init,\n" +
			"and commit it — the key is a commit-visible declaration, reviewed like code.\n"
	}
	return head + body + "This is the safety floor — it fires even under stand-down, and no token lifts it.\n"
}

// mergeBlockMessage renders the block for each verdict. The indeterminate case
// gets its own wording because the operator's fix differs: a token authorizes a
// deliberate merge, but an unresolvable base usually means the forge could not
// be reached, and re-running is the right first move.
//
// Every block ends with the same fallback line — the token — so the sanctioned
// path is always visible, and names the exact failed condition above it so the
// agent relays something actionable rather than "blocked".
func mergeBlockMessage(v mergeVerdict, detail string) string {
	const tokenLine = "If the merge is deliberate, ask the operator to mint a token outside Claude Code:\n" +
		"  bravros police unlock   (bravros promote unlock also works)\n"
	if v == mergeUnenforced {
		return "✋🏽 Police Block: " + detail + ".\n" +
			"Protected branches are enforced by the repo's pre-push hook, which reads the\n" +
			"real ref list git is about to send. This command would run without it, so no\n" +
			"layer would check what it pushes.\n" +
			"Drop the flag, or " + strings.ToLower(tokenLine[:1]) + tokenLine[1:]
	}
	if v == mergeIndeterminate {
		switch detail {
		case laneUnknownDetail:
			return "✋🏽 Police Block: " + detail + ".\n" +
				"This PR is a staging-lane candidate; once GitHub reports mergeStateStatus CLEAN\n" +
				"the same command merges without a token.\n" + tokenLine
		case prUnreadableDetail:
			return "✋🏽 Police Block: " + detail + ".\n" +
				"A $VAR PR argument is unreadable to the hook — put the literal PR number on the merge\n" +
				"line (`gh pr merge 123 --merge`); a CLEAN staging → main PR then merges with no token.\n" + tokenLine
		case redefinesDetail:
			return "✋🏽 Police Block: " + detail + ".\n" +
				"A function or alias named gh or git rebinds the words this gate anchors on, so nothing\n" +
				"on the line can be shown to be the real program. Drop the definition and run gh/git directly.\n" + tokenLine
		case substitutedProgramDetail:
			return "✋🏽 Police Block: " + detail + ".\n" +
				"`$(…)`, a backtick or `$VAR` in the program position is resolved by the shell after this\n" +
				"gate has answered. Spell the program as gh or git.\n" + tokenLine
		}
		return "✋🏽 Police Block: " + detail + ", and the target branch could not be determined.\n" +
			"The forge lookup failed (offline, unauthenticated, or no such PR), so this\n" +
			"command cannot be shown to be safe. Re-run once connectivity is restored.\n" + tokenLine
	}

	// mergeProtected. Compute the hint once, here — off the hot path.
	cfg, found := config.LoadBravrosConfig()
	staging := cfg.StagingBranch
	if staging == "" {
		staging = "homolog"
	}
	if i := strings.Index(detail, laneRefusedPrefix); i >= 0 {
		why := strings.TrimSuffix(detail[i+len(laneRefusedPrefix):], ")")
		return "✋🏽 Police Block: merging a pull request into a protected branch.\n" +
			"Staging lane refused: " + why + ".\n" + tokenLine
	}
	head := "✋🏽 Police Block: Merging or pushing to main is blocked for agents"
	if detail != "" {
		head += " (" + detail + ")"
	}
	var hint string
	switch {
	case !found:
		hint = "This repo has no " + config.ConfigFilename + ", so main is protected by default.\n"
	case cfg.Police == nil || !cfg.Police.DirectMain:
		hint = "police.direct_main is not set in " + config.ConfigFilename + ".\n"
	}
	return head + ".\n" + hint +
		"If it is direct-to-main by design, ask the operator, then run: bravros police direct-main on  (commit it).\n" +
		"Otherwise open a PR from " + staging + " — a CLEAN " + staging + " → main PR merges with `gh pr merge <N>` and no token.\n" +
		tokenLine
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
//
// Parens are dropped rather than scoped. Round 6 tracked their nesting to scope
// a working directory across segments; the push arm no longer keeps one, so the
// depth had no reader left. Reintroducing it would mean reintroducing the model
// that needed it.
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
		got := unquote(seg[i])
		if i == 0 {
			got = filepathBase(got) // `/usr/bin/git commit` is still git
		}
		if got != w {
			return false
		}
	}
	return true
}

// pushTargetsProtected reads destination refspecs, including sets of refs.
// A mirror can delete a protected remote branch even if it is absent locally.
func pushTargetsProtected(fields []string) (protected, explicit bool) {
	tags := false
	for i := 0; i < len(fields); i++ {
		flag := unquote(fields[i])
		if flag == "--" {
			break
		}
		if gitPushValueFlags[flag] {
			i++
			continue
		}
		switch flag {
		case "--all", "--branches", "--mirror":
			return true, true
		case "--tags":
			tags = true
		}
	}
	pos := gitPushPositionals(fields)
	if len(pos) > 0 {
		pos = pos[1:]
	}
	for _, ref := range pos {
		if pushRefProtected(ref) {
			return true, true
		}
	}
	return false, len(pos) > 0 || tags
}

func pushRefProtected(ref string) bool {
	ref = strings.TrimPrefix(unquote(ref), "+")
	if ref == ":" {
		return true
	} // matching branches includes main/master
	if _, dst, ok := strings.Cut(ref, ":"); ok {
		ref = dst
	} else if ref == "HEAD" {
		ref = gitIn("symbolic-ref", "--quiet", "--short", "HEAD")
	}
	for branch := range protectedBranches {
		if ref == branch || ref == "refs/heads/"+branch {
			return true
		}
		if strings.Contains(ref, "*") {
			for _, candidate := range []string{branch, "refs/heads/" + branch} {
				if matched, _ := filepath.Match(ref, candidate); matched {
					return true
				}
			}
		}
	}
	return false
}

func gitPushDryRun(fields []string) bool {
	dry := false
	for i := 0; i < len(fields); i++ {
		f := unquote(fields[i])
		if f == "--" {
			break
		}
		if gitPushValueFlags[f] {
			i++
			continue
		}
		if f == "--dry-run" || f == "-n" {
			dry = true
		}
		if f == "--no-dry-run" {
			dry = false
		}
	}
	return dry
}

// Configuration changes cannot borrow the session's known routing or opt-out.
func pushConfigOverridden(seg []string) bool {
	for _, raw := range seg {
		f := unquote(raw)
		if f == "-c" || strings.HasPrefix(f, "-c") && strings.Contains(f, "=") ||
			strings.HasPrefix(f, "--config-env") || strings.HasPrefix(f, "GIT_CONFIG") {
			return true
		}
	}
	return false
}

func defaultPushTargetsProtected(fields []string) bool {
	remote := pushRemote(fields)
	if gitIn("config", "--bool", "--get", "remote."+remote+".mirror") == "true" {
		return true
	}
	if specs := gitIn("config", "--get-all", "remote."+remote+".push"); specs != "" {
		for _, spec := range strings.Split(specs, "\n") {
			if pushRefProtected(spec) {
				return true
			}
		}
		return false
	}
	mode := gitIn("config", "--get", "push.default")
	if mode == "nothing" {
		return false
	}
	if mode == "matching" {
		return true
	}
	branch := gitIn("symbolic-ref", "--quiet", "--short", "HEAD")
	if mode == "upstream" || mode == "tracking" || mode == "simple" || mode == "" {
		upstream := gitIn("config", "--get", "branch."+branch+".merge")
		if pushRefProtected(upstream) {
			return true
		}
	}
	return protectedBranches[branch]
}

// mergePRVerdict resolves a `gh pr merge` against the PR's real base branch.
//
// The base is not in the command string, so it is read from the forge. When
// that lookup cannot answer — offline, unauthenticated, no such PR — the
// command is BLOCKED as indeterminate, not allowed. It read the opposite way
// until B-0036, and that is exactly how an agent reached production main
// unreviewed: the base was resolved in the wrong repo, errored, and was waved
// through.
func mergePRVerdict(fields []string) (mergeVerdict, string, string) {
	// Scan for the PR argument, skipping flags and the values they consume so a
	// numeric flag value is never mistaken for it.
	//
	// `gh pr merge` accepts a number, a URL, or a branch name. Keeping only a
	// numeric one dropped the other two on the floor and asked `gh pr view`
	// with no argument at all — which answers for the CURRENT branch's PR, a
	// different pull request than the one being merged (PR 90 round 12 review).
	//
	// fields[0..2] are the anchor words themselves (`gh pr merge`, however gh
	// was spelled — `/opt/homebrew/bin/gh` is not a PR argument), so the scan
	// starts after them.
	var pr string
	for i := 3; i < len(fields); i++ {
		f := unquote(fields[i])
		if strings.HasPrefix(f, "-") {
			if !strings.Contains(f, "=") && ghPRMergeValueFlags[f] {
				i++
			}
			continue
		}
		pr = f
		break
	}
	// -R/--repo retargets the merge at a repo that is NOT the cwd. Forwarding
	// it is what makes the lookup ask about the PR actually being merged:
	// without it the base was resolved in whatever repo the shell happened to
	// be in, which errored and (under the old fail-open) allowed the merge.
	// Proven open against production on 2026-09-09 — see B-0036.
	repo := ghFlagValue(fields, "--repo", "-R")
	target := mergePRTarget(repo, pr)
	if apiFieldUnreadable(pr) {
		return mergeIndeterminate, prUnreadableDetail, target
	}
	if apiFieldUnreadable(repo) {
		return mergeIndeterminate, "merging a pull request whose target could not be read", target
	}
	facts, ok := lookupPR(repo, pr)
	if !ok {
		return mergeIndeterminate, "merging a pull request", target
	}
	if !protectedBranches[facts.Base] {
		return mergeAllowed, "", target
	}
	// The staging lane: same-repo, numeric-or-bare PR, head == staging branch,
	// CLEAN, no autonomous lock (see police_lane.go). URL, branch and -R forms
	// never use it — they can name a PR in another repository.
	if laneEligible(repo, pr) {
		// The lane merges the staging branch INTO main; it never deletes it.
		// `--delete-branch` would remove homolog on the forge and locally, and
		// every flow that writes there next would recreate it from main.
		if ghHasOption(fields, "--delete-branch", "-d") {
			return mergeProtected, "merging a pull request into a protected branch (" + laneRefusedPrefix + "--delete-branch/-d is not allowed on a lane merge — the staging branch must survive it; drop the flag)", target
		}
		v, d := stagingLaneVerdict(pr, facts)
		if v == mergeProtected {
			d = "merging a pull request into a protected branch (" + d + ")"
		}
		return v, d, target
	}
	return mergeProtected, "merging a pull request into a protected branch (" + laneRefusedPrefix + "-R/--repo, URL and branch forms never use the lane)", target
}

// prBaseProtected is the base-only view of lookupPR, kept for the gh api merge
// route, which has no lane: protected reports whether the PR's base is a
// protected branch, ok whether the forge answered at all.
func prBaseProtected(repo, pr string) (protected bool, ok bool) {
	facts, ok := lookupPR(repo, pr)
	if !ok {
		return false, false
	}
	return protectedBranches[facts.Base], true
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
		seg = stripEnvPrefix(seg)
		if len(seg) == 0 {
			continue
		}
		var v *rule52Violation
		switch filepathBase(seg[0]) { // `/usr/bin/git reset --hard`, `/bin/rm -rf`
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

// gitPushArgs returns the push invocation inside seg, with git's global options
// stripped, and whether it is one no pre-push hook will ever see. Returns nil
// when the segment runs no push.
//
// It scans for the command word ANYWHERE in the segment rather than requiring
// it first. Anchoring on position 0 meant any prefix at all defeated the gate:
// `sudo git push origin main`, `time git push …`, `nohup`, `command`, `exec`,
// `eval`, `! git push …`, a `{ …; }` brace group, and the bodies of `if`/`for`/
// `while` (which arrive as `then …` / `do …` segments) were ALL ungated, with
// no opt-out involved. Measured on 2026-09-09; see the round-6 tests.
//
// Scanning wide over-blocks an unquoted `echo git push origin main`. That is the
// safe direction and the deliberate trade: a gate that answers "no" to a
// sentence about pushing costs a token; a gate that answers "yes" to sudo costs
// the branch.
func gitPushArgs(seg []string) (args []string, hookless bool) {
	for i := range seg {
		// filepathBase: `/usr/bin/git push origin main` is still git.
		if filepathBase(unquote(seg[i])) != "git" {
			continue
		}
		rest := stripGitGlobalOpts(seg[i:])
		if len(rest) < 2 {
			continue
		}
		switch unquote(rest[1]) {
		case "push":
			return rest, false
		case "send-pack", "http-push":
			// The plumbing behind `git push`. It transfers objects and updates
			// remote refs exactly as the porcelain does, but the pre-push hook
			// is invoked by builtin/push.c, so send-pack never runs it —
			// verified against git 2.50.1: `git push` fired the hook and was
			// blocked, `git send-pack` pushed the same ref with the hook silent
			// (PR 90 round 11). There is no layer behind these, so nothing here
			// may ever be deferred to one.
			return rest, true
		}
	}
	return nil, false
}

// ghArgs returns the `gh` invocation inside seg whose first word after `gh`
// matches words, or nil. Same anchor-anywhere rule as gitPushArgs.
func ghArgs(seg []string, words ...string) []string {
	for i := range seg {
		// filepathBase: `/opt/homebrew/bin/gh pr merge 5` and `./gh …` are the
		// same program; anchoring on the bare word let both through unread.
		if filepathBase(unquote(seg[i])) != "gh" {
			continue
		}
		rest := seg[i:]
		if len(rest) <= len(words) {
			continue
		}
		match := true
		for j, w := range words {
			if unquote(rest[j+1]) != w {
				match = false
				break
			}
		}
		if match {
			return rest
		}
	}
	return nil
}

// stripGitGlobalOpts removes `git`'s global options from between "git" and its
// subcommand, so `git -c push.default=simple push origin main` still anchors on
// "push". Left in place they defeated startsWith outright and the command fell
// out of the merge gate entirely — unconditionally, with no opt-out needed
// (found while fixing PR 90 round 4; the reviewer named only the `git push -o`
// case).
//
// It reports only the argv, not where -C points: the push arm defers to the
// pre-push hook, which runs in the real directory whatever this parser thinks.
// commandRelocates still notices such a flag is present, which is all the gh
// arms need.
func stripGitGlobalOpts(seg []string) []string {
	if len(seg) == 0 || filepathBase(unquote(seg[0])) != "git" {
		return seg
	}
	out := []string{"git"}
	i := 1
	for ; i < len(seg); i++ {
		f := unquote(seg[i])
		if !strings.HasPrefix(f, "-") {
			break
		}
		name, _, hasEq := strings.Cut(f, "=")
		if !hasEq && gitGlobalValueFlags[name] {
			i++ // the option's value is a separate token
		}
	}
	return append(out, seg[i:]...)
}

// segmentRunsCd reports whether a segment runs `cd` or `pushd`.
//
// It looks for the verb ANYWHERE in the segment, not just first. Anchoring on
// position 0 missed `{ cd other; git push; }`, because the brace is its own
// token — the same position-0 assumption that let twelve command prefixes walk
// past the gate in round 6, resurfacing in the one place still checking it.
//
// Only the fact is wanted, never the destination: callers fail closed on it, so
// over-detecting an `echo cd` costs a token and under-detecting costs a branch.
func segmentRunsCd(seg []string) bool {
	for _, raw := range seg {
		switch unquote(raw) {
		case "cd", "pushd", "popd":
			return true
		}
	}
	return false
}

// cdIsNoOp reports whether seg is exactly `cd <abs>` where <abs> is the
// session's own working directory, i.e. a relocation that goes nowhere.
//
// This is the one carve-out from "any cd fails closed": skills routinely spell
// `cd /abs/repo && gh pr merge N --merge` from inside that very repo, and the
// gate blocked it as indeterminate (paylog 2aaa8ca8, 2026-09-11T07:11). The
// shape accepted is deliberately narrow — two tokens, an absolute literal path
// with no shell expansion characters, equal to cwd after Clean. Relative paths,
// `~`, variables, `cd -`, pushd, and any extra argument keep failing closed.
func cdIsNoOp(seg []string, cwd string) bool {
	if cwd == "" || !filepath.IsAbs(cwd) {
		return false
	}
	if len(seg) != 2 || unquote(seg[0]) != "cd" {
		return false
	}
	arg := seg[1]
	if arg == "" || !filepath.IsAbs(arg) || strings.ContainsAny(arg, "$`~*?[{} \t\n") {
		return false
	}
	return filepath.Clean(arg) == filepath.Clean(cwd)
}

// stripEnvPrefix strips leading `VAR=x` assignments off a segment, mirroring
// startsWith's own loop, so `FOO=1 git checkout -- .` still anchors on "git".
//
// Callers that go on to scan the segment's own tokens must strip BEFORE that
// scan, not rely on startsWith: startsWith reslices a copy of its parameter, so
// the assignment token survives in the caller's slice. `FOO=1 git push
// git@github.com:other/repo.git x:main` was matched as a push, then had
// "FOO=1" picked as its destination — unresolvable, so the target collapsed to
// "" and the local direct_main opt-out excused a push to another repo's main
// (PR 90 review, blocking 2).
func stripEnvPrefix(seg []string) []string {
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
	Short: "Mint a human-presence token to allow a merge the staging lane refuses",
	Long: `Mint the 10-minute police token at ~/.claude/state/police-token.

Needed only for merges the staging lane does not cover: git push to main,
gh api / GraphQL merge routes, -R/URL/branch-form gh pr merge, a non-CLEAN or
non-staging-head PR, a repo with police.staging_lane "reviewed"/"off", or any
merge while a .planning/.auto-*-lock is present. A CLEAN <staging> → main
` + "`gh pr merge <N>`" + ` needs no token. ` + "`bravros promote unlock`" + ` is honoured too.
Must be run from a terminal OUTSIDE Claude Code.`,
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
	Short: "Check the police and promote merge tokens",
	Long: `Report both tokens the merge gate honours: the police token
(~/.claude/state/police-token, 10 min from mtime) and the promote token
(~/.claude/state/promote-token, minted by bravros promote unlock).
Says nothing about stand-down (bravros police standdown status) or the
staging lane (bravros police direct-main status).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if hasValidPoliceToken() {
			fmt.Fprintln(cmd.OutOrStdout(), "🔓 Police token is VALID.")
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "🔒 Police token is MISSING or INVALID.")
		}
		promote := token.Gate{Name: "promote"}
		if tok := promote.Read(); tok != nil && !tok.Expired() {
			fmt.Fprintf(cmd.OutOrStdout(), "🔓 Promote token is VALID (expires %s).\n", tok.ExpiresAt.Format(time.RFC3339))
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "🔒 Promote token is MISSING or INVALID.")
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
