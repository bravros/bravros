package cmd

import (
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Heredoc awareness
//
// commandSegments splits on newlines, so before this existed every line of a
// heredoc BODY was read as a shell command. A markdown doc written with
// `cat > notes.md <<'MD'`, or a Python script fed to `python3 - <<'EOF'`, was
// scanned word by word for program positions: a body line reading
// `$(command -v gh) api …` tripped "the program is named by a substitution",
// a README line `git push origin main` tripped the protected-main block, and a
// body naming .bravros/config.json tripped the gate-input floor (session audit,
// 2026-09-30 — every one a legitimate command).
//
// A heredoc whose delimiter is QUOTED (`<<'EOF'`, `<<"EOF"`, `<<\EOF`) is
// delivered to its consumer verbatim: no expansion, no substitution. Its body
// is data unless the consumer is itself a shell — `bash <<'EOF'`,
// `cat <<'EOF' | sh`, `ssh host <<'EOF'` — in which case it is commands and
// stays visible to every scan. An UNQUOTED delimiter keeps the old treatment:
// its body undergoes `$(…)` substitution, so it can execute.
// ---------------------------------------------------------------------------

// heredocMarker replaces each heredoc operator word in the stripped command,
// suffixed with the heredoc's index, so a scan can find the consumer segment.
const heredocMarker = "<<HEREDOC#"

// heredoc is one here-document lifted out of a command line.
type heredoc struct {
	delim  string
	quoted bool   // delimiter was quoted: the body reaches the consumer verbatim
	body   string // the lines between the operator line and the terminator
	shell  bool   // the command runs a shell that could read the body as commands
	interp bool   // the command runs a non-shell interpreter (python3 -, node, …)
}

// executable reports whether the body can run as shell: a shell consumer, or
// an unquoted delimiter whose `$(…)` substitutions the shell performs.
func (h heredoc) executable() bool { return h.shell || !h.quoted }

// parsedCommand is a command line with its heredoc bodies lifted out.
type parsedCommand struct {
	raw      string
	stripped string // raw with every heredoc body removed and operators replaced by markers
	docs     []heredoc
}

// heredocShellWords are the programs that treat a fed body as commands. Any of
// them anywhere on the command makes that body executable: `cat <<'EOF' | bash`
// runs it just as surely as `bash <<'EOF'` does.
var heredocShellWords = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "fish": true,
	"eval": true, "source": true, ".": true, "ssh": true, "xargs": true,
}

// parseHeredocs lifts every heredoc out of cmd. Quote-aware: a `<<` inside a
// quoted string (`git commit -m "$(cat <<'EOF' … EOF)"`) is part of that
// string's text and is left alone, so the attribution scan still reads it.
func parseHeredocs(cmd string) parsedCommand {
	if !strings.Contains(cmd, "<<") {
		return parsedCommand{raw: cmd, stripped: cmd}
	}
	var out strings.Builder
	var docs []heredoc
	var pending []int // docs whose bodies start after the next unquoted newline
	var inSingle, inDouble, escaped bool

	runes := []rune(cmd)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if escaped {
			out.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && !inSingle {
			escaped = true
			out.WriteRune(r)
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
		} else if r == '"' && !inSingle {
			inDouble = !inDouble
		}
		if inSingle || inDouble || r == '\'' || r == '"' {
			out.WriteRune(r)
			continue
		}

		if r == '<' && i+1 < len(runes) && runes[i+1] == '<' {
			if i+2 < len(runes) && runes[i+2] == '<' {
				out.WriteString("<<<") // here-string: its word is ordinary text
				i += 2
				continue
			}
			j := i + 2
			if j < len(runes) && runes[j] == '-' {
				j++ // `<<-`: readHeredocBody ignores leading tabs either way
			}
			for j < len(runes) && (runes[j] == ' ' || runes[j] == '\t') {
				j++
			}
			delim, quoted, end := readHeredocWord(runes, j)
			if delim == "" {
				out.WriteString("<<")
				i++
				continue
			}
			docs = append(docs, heredoc{delim: delim, quoted: quoted})
			pending = append(pending, len(docs)-1)
			out.WriteString(heredocMarker + strconv.Itoa(len(docs)-1) + " ")
			i = end - 1
			continue
		}

		if r == '\n' {
			out.WriteRune(r)
			if len(pending) > 0 {
				i++
				for _, idx := range pending {
					body, next := readHeredocBody(runes, i, docs[idx].delim)
					docs[idx].body = body
					i = next
				}
				pending = nil
				i-- // the loop increments
			}
			continue
		}
		out.WriteRune(r)
	}
	// Classified over the WHOLE command, not the operator's line: a body
	// written to a file and run later on the line (`cat > x.sh <<'EOF' … EOF`
	// then `bash x.sh`) is commands just as surely as `bash <<'EOF'`.
	shell, interp := heredocConsumers(out.String())
	for i := range docs {
		docs[i].shell, docs[i].interp = shell, interp
	}
	return parsedCommand{raw: cmd, stripped: out.String(), docs: docs}
}

// readHeredocWord reads the delimiter word starting at runes[j]: `EOF`,
// `'EOF'`, `"EOF"`, `\EOF`, or a mix. Any quoting marks the heredoc quoted.
// It returns the unquoted delimiter and the index just past the word.
func readHeredocWord(runes []rune, j int) (string, bool, int) {
	var w strings.Builder
	quoted := false
	for j < len(runes) {
		r := runes[j]
		switch {
		case r == '\'' || r == '"':
			quoted = true
			k := j + 1
			for k < len(runes) && runes[k] != r {
				w.WriteRune(runes[k])
				k++
			}
			j = k + 1
			continue
		case r == '\\' && j+1 < len(runes):
			quoted = true
			w.WriteRune(runes[j+1])
			j += 2
			continue
		case strings.ContainsRune(" \t\n;&|<>()", r):
			return w.String(), quoted, j
		}
		w.WriteRune(r)
		j++
	}
	return w.String(), quoted, j
}

// readHeredocBody reads lines from runes[i] up to (not including) the line
// equal to delim — leading tabs ignored, which covers `<<-`. It returns the
// body and the index just past the terminator line. An unterminated heredoc
// runs to the end of the command, as the shell reads it.
func readHeredocBody(runes []rune, i int, delim string) (string, int) {
	var body strings.Builder
	for i < len(runes) {
		end := i
		for end < len(runes) && runes[end] != '\n' {
			end++
		}
		line := string(runes[i:end])
		next := end
		if next < len(runes) {
			next++ // past the newline
		}
		if strings.TrimLeft(line, "\t") == delim {
			return body.String(), next
		}
		body.WriteString(line)
		body.WriteByte('\n')
		i = next
	}
	return body.String(), i
}

// heredocConsumers classifies the programs of a heredoc-stripped command:
// shell is true when anything on it could run a body as commands — a shell
// named anywhere (`docker exec -i c bash`, `xargs sh`), `eval`/`source`/`.`
// in program position, or a script run by path (`./x.sh`, `scripts/y.sh`).
func heredocConsumers(stripped string) (shell, interp bool) {
	for _, seg := range commandSegments(stripped) {
		if _, prog := gateProgram(seg); prog != "" {
			first := unquote(stripEnvPrefix(seg)[0])
			if heredocShellWords[prog] || strings.HasSuffix(prog, ".sh") || strings.HasPrefix(first, "./") {
				shell = true
			}
		}
		for _, raw := range seg {
			name := filepathBase(unquote(raw))
			if shellRunners[name] || name == "ssh" || name == "xargs" {
				shell = true
			}
			if codeRunners[name] && !shellRunners[name] {
				interp = true
			}
		}
	}
	return shell, interp
}

// execSegments returns the segments of every part of the command the shell
// could run as commands: the command with its bodies removed, plus the body of
// each heredoc that is executable.
func (p parsedCommand) execSegments() [][]string {
	segs := commandSegments(p.stripped)
	for _, d := range p.docs {
		if d.executable() {
			segs = append(segs, commandSegments(d.body)...)
		}
	}
	return segs
}

// execText is stripped plus every executable body — the raw-text view for the
// scans that read characters rather than tokens (function/alias definitions).
func (p parsedCommand) execText() string {
	var b strings.Builder
	b.WriteString(p.stripped)
	for _, d := range p.docs {
		if d.executable() {
			b.WriteByte('\n')
			b.WriteString(d.body)
		}
	}
	return b.String()
}

// interpBodies returns the bodies fed to a non-shell interpreter: code this
// gate does not parse, but whose writes the gate-input floor must still see.
func (p parsedCommand) interpBodies() []string {
	var out []string
	for _, d := range p.docs {
		if d.interp && !d.executable() {
			out = append(out, d.body)
		}
	}
	return out
}

// heredocIndexIn returns the index of the heredoc whose operator is in seg,
// or -1.
func heredocIndexIn(seg []string) int {
	for _, raw := range seg {
		if k := strings.Index(raw, heredocMarker); k >= 0 {
			rest := raw[k+len(heredocMarker):]
			n := 0
			for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
				n++
			}
			if idx, err := strconv.Atoi(rest[:n]); err == nil {
				return idx
			}
		}
	}
	return -1
}

// interpCodeBodies returns every body fed to a non-shell interpreter, quoted
// delimiter or not: the code the merge gate must still read for a spelled-out
// merge. (An unquoted body is also in execSegments, but shell tokenization
// leaves `subprocess.run(["gh","pr","merge","1"])` one opaque word.)
func (p parsedCommand) interpCodeBodies() []string {
	var out []string
	for _, d := range p.interpCodeDocs() {
		out = append(out, d.body)
	}
	return out
}

// interpCode is an interpreter heredoc body; direct is true when the
// heredoc's own command is the interpreter (`ruby <<'EOF'`), as opposed to a
// file written here and run later on the line.
type interpCode struct {
	body   string
	direct bool
}

func (p parsedCommand) interpCodeDocs() []interpCode {
	var out []interpCode
	if len(p.docs) == 0 {
		return nil
	}
	direct := map[int]bool{}
	for _, seg := range commandSegments(p.stripped) {
		idx := heredocIndexIn(seg)
		if idx < 0 {
			continue
		}
		for _, raw := range seg {
			if name := filepathBase(unquote(raw)); codeRunners[name] && !shellRunners[name] {
				direct[idx] = true
			}
		}
	}
	for i, d := range p.docs {
		if d.interp && !d.shell {
			out = append(out, interpCode{body: d.body, direct: direct[i]})
		}
	}
	return out
}

// processSpawners are the spellings by which interpreter code runs another
// program. A heredoc body that names none of them cannot run `gh` or `git`,
// whatever its string literals say — that is what lets a script writing
// "git push origin main" into README.md through.
var processSpawners = []string{
	"subprocess", "os.system", "os.popen", "os.exec", "os.spawn", "os.posix_spawn",
	"pty.spawn", "commands.", "__import__", "getattr(", "importlib", "eval(", "exec(",
	"child_process", "execSync", "execFile", "spawn", "Bun.spawn", "Bun.$", "Deno.Command", "Deno.run",
	"system(", "system ", "qx", "%x", "Open3", "IO.popen", "popen", "shell_exec",
	"passthru", "proc_open", "pcntl_exec", "getline", "do shell script",
}

// heredocMergeMarker reports a merge command spelled inside the string
// literals of a heredoc fed to an interpreter that can spawn a process — the
// heredoc twin of embeddedMergeMarker, which reads `python3 -c '…'`.
//
// Only literals are read, so a comment or a bare code line naming `gh api`
// is not a call. The literals are joined both with and without a space, so
// `["gh","pr","merge"]` and `"g" + "h pr merge"` both read as the command.
//
// A backtick runs a command in perl, ruby and php, so it counts as a spawner
// only when the heredoc feeds the interpreter directly: a markdown doc
// written on a line that also runs python is full of backticks that run
// nothing.
func heredocMergeMarker(docs []interpCode) (string, bool) {
	for _, doc := range docs {
		body := doc.body
		spawns := doc.direct && strings.Contains(body, "`")
		for _, s := range processSpawners {
			if !spawns && strings.Contains(body, s) {
				spawns = true
				break
			}
		}
		if !spawns {
			continue
		}
		lits := stringLiterals(body)
		for _, flat := range []string{
			strings.Join(strings.Fields(strings.Join(lits, " ")), " "),
			strings.Join(strings.Fields(strings.Join(lits, "")), " "),
		} {
			for _, marker := range []string{"gh pr merge", "gh api", "git push"} {
				if strings.Contains(flat, marker) {
					return marker, true
				}
			}
		}
	}
	return "", false
}

// stringLiterals returns the contents of every '…', "…" and `…` literal in
// code, in order, honouring backslash escapes. Deliberately naive: a stray
// quote in a comment can only merge text into a literal, which makes the scan
// stricter, never looser.
func stringLiterals(code string) []string {
	var out []string
	runes := []rune(code)
	for i := 0; i < len(runes); i++ {
		q := runes[i]
		if q != '\'' && q != '"' && q != '`' {
			continue
		}
		var lit strings.Builder
		j := i + 1
		for ; j < len(runes) && runes[j] != q; j++ {
			if runes[j] == '\\' && j+1 < len(runes) {
				j++
			}
			lit.WriteRune(runes[j])
		}
		out = append(out, lit.String())
		i = j
	}
	return out
}

// interpBodySegments wraps each interpreter heredoc body as a segment of its
// own, so the token-level HTTP scan reads a merge URL inside it as it does one
// inside `python3 -c '…'`.
func (p parsedCommand) interpBodySegments() [][]string {
	var out [][]string
	for _, body := range p.interpCodeBodies() {
		out = append(out, []string{"python3", "-", body})
	}
	return out
}
