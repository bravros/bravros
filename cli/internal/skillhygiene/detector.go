// Package skillhygiene detects shell word-split bugs in skill markdown files.
//
// Patterns caught:
//   - for x in $VAR;   — unquoted variable in for-in loop (word-splits on IFS)
//   - for x in $LIST;  — any bare $IDENTIFIER after "in"
//   - for x in $ITEMS  — same with implicit newline terminator
//
// Patterns explicitly ignored:
//   - for x in $(cmd); — command substitution, $ followed by '('
//   - for x in "${arr[@]}"; — array expansion, $ followed by '{'
//
// Lines inside fenced code blocks whose language tag is not bash, sh, or zsh
// are skipped — prose examples in ```text or ```markdown blocks do not trigger.
package skillhygiene

import (
	"regexp"
	"strings"
)

// Match describes one detected occurrence of the word-split anti-pattern.
type Match struct {
	Line    int // 1-indexed
	Col     int // 1-indexed, byte offset
	Snippet string
}

var (
	detectorRe   = regexp.MustCompile(`\bfor\s+[A-Za-z_][A-Za-z0-9_]*\s+in\s+\$[A-Za-z_][A-Za-z0-9_]*\s*[;\n]`)
	bashFenceRe  = regexp.MustCompile("^```(bash|sh|zsh)(\\s|$)")
	anyFenceRe   = regexp.MustCompile("^```\\S")
	closeFenceRe = regexp.MustCompile("^```\\s*$")
)

// Detect scans content and returns all word-split anti-pattern matches.
// Matches inside non-bash fenced blocks (e.g. ```text, ```yaml) are suppressed.
func Detect(content string) []Match {
	var results []Match
	lines := strings.Split(content, "\n")

	inFence := false
	bashFence := false

	for i, line := range lines {
		lineNum := i + 1

		if !inFence {
			if bashFenceRe.MatchString(line) {
				inFence = true
				bashFence = true
				continue
			}
			if anyFenceRe.MatchString(line) {
				inFence = true
				bashFence = false
				continue
			}
		} else {
			if closeFenceRe.MatchString(line) {
				inFence = false
				bashFence = false
				continue
			}
			if !bashFence {
				continue
			}
		}

		// Append newline so the [;\n] terminator matches end-of-line too.
		checkLine := line + "\n"
		locs := detectorRe.FindAllStringIndex(checkLine, -1)
		for _, loc := range locs {
			results = append(results, Match{
				Line:    lineNum,
				Col:     loc[0] + 1,
				Snippet: strings.TrimRight(checkLine[loc[0]:loc[1]], "\n"),
			})
		}
	}

	return results
}
