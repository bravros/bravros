package cmd

import (
	"os"
	"regexp"
	"strings"
)

// AI-attribution detection is PORTED from the commit-msg hook's block 1b
// (templates/.githooks/commit-msg, "AI_ATTRIB"/"AI_NAMES") rather than
// hand-rolled. That cross-product of attribution verbs x AI names replaced an
// enumerated trailer list precisely because enumerating does not scale: 12
// loosened wordings ("Assisted by Claude", "Co-authored with Claude",
// "Signed-off-by: Claude <noreply@anthropic.com>", ...) slipped past the
// literal list. The hook's own note records it validated at 0 false positives
// against 200 real commits. Keep the two lists in sync with the hook.
//
// Two deliberate differences from the hook, both because this gate is the only
// thing that sees PR TITLES and BODIES — long markdown prose — where the hook
// only ever sees a commit message:
//
//  1. Matching is LINE-ANCHORED. A real footer owns its line (optionally behind
//     markdown list/quote markers or an emoji); a prose mention sits
//     mid-sentence. Unanchored, a PR body that merely *documents* these strings
//     is blocked — including the PR that ships this gate. Measured on a 38-case
//     corpus: anchored blocks 28/28 real footers with 0 false positives;
//     unanchored blocks the same 28 but also 6 legitimate bodies.
//  2. Fenced code blocks and inline code spans are stripped before matching, so
//     a body can quote `Made with Cursor` to document it.
//
// The hook stays unanchored and remains the authority on commit messages.
//
// KNOWN LIMITS — this gate reads a shell string, so it cannot be the whole
// answer. Each of these is accepted, not overlooked:
//
//   - Mid-sentence attribution slips: "This PR was made with Cursor" passes,
//     because line-anchoring is what buys zero false positives on prose. A
//     hand-written sentence is disclosure; the machine-appended footer this
//     gate exists to stop always owns its own line.
//   - A footer inside a fenced block is not detected. It renders as a code
//     listing rather than as attribution, so it fails at the thing attribution
//     is for. The alternative — matching inside fences — blocks every PR that
//     documents this gate, including the one that shipped it.
//   - Shell-level opacity: "$MSG" expansion, an editor-driven commit with no
//     -m at all, and a here-doc written to a file in an EARLIER tool call are
//     all invisible here. For commit messages the commit-msg hook backstops
//     every one of them.
//   - PR titles and bodies have NO backstop. The hook cannot see them, so a
//     miss here is a miss outright. Only a server-side check would be durable.
//   - git merge -m, git tag -a -m and git notes add -m are covered here
//     because nothing else covers them: commit-msg skips merges by design
//     (templates/.githooks/commit-msg, section 0) and git has no hook at all
//     for tag or notes messages.
const aiAttribVerbs = `(?:co[-_ ]?authored?|assisted|generated|created|written|authored|made|powered|pair[-_ ]?programmed|reviewed|prompted)[-_ ]?(?:by|with|from)|with help from|signed[-_ ]?off[-_ ]?by|co[-_ ]?authors?[-_ ]?[:=]`

const aiNames = `claude|chat ?gpt|gpt-?[0-9.]*|openai|anthropic|copilot|gemini|bard|codeium|windsurf|cursor|devin|aider|cline|roo[-_ ]?code|cody|tabby|amazon q|code ?whisperer|deep ?seek|grok|mistral|llama|phind|bolt\.new|v0\.dev|augment|continue\.dev|jarvis|sourcegraph|codex|junie|factory-?droid|\bdroid\b|openhands|all-hands|google-labs-jules|ai assistant|assistant ai|artificial intelligence|language model|\bllm\b|\bai\b`

// aiSignatureRE anchors to line start, tolerating leading whitespace, markdown
// list/quote/heading markers, and emoji (Cursor and Claude Code both prefix
// their footer with one).
// aiTrailerRE catches two shapes the verb x name cross-product cannot: tool
// session trailers, which carry no attribution verb, and the agent-badge LINKS
// that are Cursor's actual PR attribution — Cursor appends an "Open in Cursor"
// image, not a text footer, so a text-only gate would miss most Cursor PRs
// entirely. Deliberately unanchored: these tokens are unambiguous enough that
// position adds nothing, and a badge can sit inline.
var aiTrailerRE = regexp.MustCompile(`(?i)` +
	`(?:claude|copilot|cursor|codex|devin|droid)-session[[:space:]]*:` +
	`|cursor\.com/(?:agents|background-agent)` +
	`|claude\.ai/code/session_`)

var aiSignatureRE = regexp.MustCompile(
	`(?im)^[\s>*\-+#\x{1F000}-\x{1FAFF}\x{2600}-\x{27BF}\x{FE0F}]*(?:` +
		aiAttribVerbs + `)[[:space:]:<@_-]{0,4}.{0,40}(?:` + aiNames + `)`)

var codeFenceRE = regexp.MustCompile("(?s)```.*?```")

// stripCode removes FENCED blocks — the sanctioned way for a body to document
// these strings — and then deletes stray backtick CHARACTERS without deleting
// the text under them.
//
// Removing whole inline spans instead would be directly weaponizable: "Made
// with `Cursor`" renders as an attribution footer but reduces to "Made with "
// once the span is dropped, so a single backtick pair would disable the gate.
// Deleting only the backticks keeps the rendered text intact and closes that.
// The price is that an inline-backticked footer is blocked; documenting one in
// a fenced block is the remedy, and is better markdown anyway.
func stripCode(s string) string {
	return strings.ReplaceAll(codeFenceRE.ReplaceAllString(s, ""), "`", "")
}

const aiSignatureBlock = "✋🏽 Police Block: AI signature detected in a git or PR artifact.\n" +
	"No AI attribution is allowed on commits, PR titles, or PR bodies " +
	"(including \"Made with Cursor\" / \"Made with [Cursor](https://cursor.com)\").\n" +
	"Remove the line and retry. To *document* the string rather than sign with it, " +
	"put it in backticks or a fenced code block."

const aiSignatureOpaque = "✋🏽 Police Block: this command supplies a body the hook cannot read, " +
	"so it cannot be checked for AI attribution.\n" +
	"Seen: %s\n" +
	"Write the body to a real file first and pass that path, or use inline --body \"...\".\n" +
	"Stdin (-), process substitution and here-docs are refused for the same reason " +
	"`gh pr comment` refuses --body-file: unreadable content cannot be validated."

// checkAiSignature inspects the text-carrying flags of git and gh commands that
// write commit messages, PR titles, PR bodies, issue bodies and release notes.
// An empty return means the command may proceed.
//
// It fails CLOSED: a body routed through a path this hook cannot read is
// blocked rather than waved through. Fail-open was the original behaviour and
// made `--body-file <(printf 'Made with Cursor')`, `--body-file -`, and a file
// written later in the same command into silent bypasses.
func checkAiSignature(command string) string {
	for _, seg := range commandSegments(command) {
		texts, opaque := scanTargets(seg)
		if opaque != "" {
			return strings.Replace(aiSignatureOpaque, "%s", opaque, 1)
		}
		for _, text := range texts {
			clean := stripCode(text)
			for _, re := range []*regexp.Regexp{aiSignatureRE, aiTrailerRE} {
				if match := re.FindString(clean); match != "" {
					return aiSignatureBlock + "\n  Matched: " + strings.TrimSpace(match) + "\n"
				}
			}
		}
	}
	return ""
}

// scanTargets returns the texts to scan for one command segment, plus a
// non-empty "opaque" reason when the segment routes a body through something
// unreadable.
func scanTargets(seg []string) (texts []string, opaque string) {
	switch {
	case startsWith(seg, "gh", "pr", "create"), startsWith(seg, "gh", "pr", "edit"),
		startsWith(seg, "gh", "pr", "comment"), startsWith(seg, "gh", "pr", "merge"),
		startsWith(seg, "gh", "issue", "create"), startsWith(seg, "gh", "issue", "edit"),
		startsWith(seg, "gh", "issue", "comment"):
		texts = append(texts, inlineValues(seg, "--title", "-t", "--body", "-b", "--subject")...)
		fileTexts, reason := fileValues(seg, "--body-file", "-F")
		return append(texts, fileTexts...), reason

	case startsWith(seg, "gh", "release", "create"), startsWith(seg, "gh", "release", "edit"):
		texts = append(texts, inlineValues(seg, "--title", "-t", "--notes", "-n")...)
		fileTexts, reason := fileValues(seg, "--notes-file", "-F")
		return append(texts, fileTexts...), reason

	case startsWith(seg, "git", "commit"), startsWith(seg, "git", "tag"),
		startsWith(seg, "git", "merge"), startsWith(seg, "git", "notes"):
		texts = append(texts, inlineValues(seg, "--message", "-m")...)
		fileTexts, reason := fileValues(seg, "--file", "-F", "--template")
		return append(texts, fileTexts...), reason

	default:
		return nil, ""
	}
}

// fileValues reads each path-valued flag. An unreadable path is reported as
// opaque rather than skipped — see checkAiSignature's fail-closed note.
func fileValues(seg []string, names ...string) (texts []string, opaque string) {
	for _, path := range inlineValues(seg, names...) {
		if path == "" || path == "-" || strings.ContainsAny(path, "<>$") {
			return nil, describeOpaquePath(path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, describeOpaquePath(path)
		}
		texts = append(texts, string(data))
	}
	return texts, ""
}

func describeOpaquePath(path string) string {
	switch {
	case path == "-" || path == "":
		return "a body read from stdin (-)"
	case strings.ContainsAny(path, "<>"):
		return "a body from process substitution (" + path + ")"
	case strings.Contains(path, "$"):
		return "a body from an unexpanded variable path (" + path + ")"
	default:
		return "an unreadable body file (" + path + ")"
	}
}

// inlineValues collects the values of the named flags, unquoting each one:
// commandSegments unquotes a standalone token, but an attached value such as
// --body="..." or -b"..." keeps its quotes, and a leading quote would defeat
// the line anchor. It understands the three shapes git and gh actually accept: a separate value ("-m msg"), an attached
// long value ("--body=msg"), and — for single-letter flags — getopt bundling
// and glued values ("-am msg", "-mmsg"), which an exact-token comparison misses.
func inlineValues(seg []string, names ...string) []string {
	var values []string
	for i := 0; i < len(seg); i++ {
		f := seg[i]
		for _, name := range names {
			switch {
			case f == name:
				if i+1 < len(seg) {
					values = append(values, unquote(seg[i+1]))
				}
			case strings.HasPrefix(f, name+"="):
				values = append(values, unquote(strings.TrimPrefix(f, name+"=")))
			default:
				if v, next, ok := shortFlagValue(f, name); ok {
					if next {
						if i+1 < len(seg) {
							values = append(values, unquote(seg[i+1]))
						}
					} else {
						values = append(values, unquote(v))
					}
				}
			}
		}
	}
	return values
}

// shortFlagValue handles a bundled or glued single-letter flag. For "-m" it
// matches "-am" (value is the next token, next=true) and "-mmsg" / "-ammsg"
// (value is glued, next=false). Long flags and non-flag tokens return ok=false.
func shortFlagValue(token, name string) (value string, next bool, ok bool) {
	if len(name) != 2 || name[0] != '-' || name[1] == '-' {
		return "", false, false
	}
	if len(token) < 2 || token[0] != '-' || token[1] == '-' {
		return "", false, false
	}
	letter := name[1]
	cluster := token[1:]
	idx := strings.IndexByte(cluster, letter)
	if idx < 0 {
		return "", false, false
	}
	// Everything before the letter must itself look like flag letters.
	for _, c := range cluster[:idx] {
		if !isASCIILetter(byte(c)) {
			return "", false, false
		}
	}
	if idx == len(cluster)-1 {
		return "", true, true
	}
	return cluster[idx+1:], false, true
}
