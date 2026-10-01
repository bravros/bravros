package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Same-command body-file resolution for the attribution check
//
// The attribution gate fails closed on a body it cannot read. Before this, it
// could read only a file that already existed on disk under a literal path, so
// the shapes agents actually use to open a PR were refused (session audit,
// ×13 across sessions):
//
//	S=/tmp/…/scratchpad; gh pr create … --body-file $S/pr-body.md
//	cat > /tmp/…/pr-body.md <<'EOF' … EOF  &&  gh pr create --body-file /tmp/…/pr-body.md
//	cp /tmp/…/x.md ./.pr-body.md && gh pr create --body-file ./.pr-body.md
//	sed -i '' '/Generated with/d' $S/pr-body.md; gh pr create --body-file $S/pr-body.md
//
// bodyFiles walks the segments in order and models just enough of them:
// literal variable assignments, files a quoted heredoc writes, copies, and a
// `sed -i '/re/d'` line deletion. Every resolved body still goes through the
// attribution regexes. Anything it cannot model — a variable set from a
// substitution, a file some other program writes on the same line — stays
// unreadable, and the gate still refuses it.
// ---------------------------------------------------------------------------

// bodyFiles is the state of the command line as far as the walk has read it.
type bodyFiles struct {
	vars  map[string]string  // NAME → literal value, from `NAME=/path` segments
	files map[string]*string // cleaned path → content; nil = written by something unmodelled
	docs  []heredoc
	// tainted: a segment the walk cannot model ran, so ANY file — tracked or
	// only on disk — may have been rewritten. Reads of untracked files then
	// fail closed; a later modelled write re-establishes a file's content.
	tainted bool
}

func newBodyFiles(docs []heredoc) *bodyFiles {
	return &bodyFiles{vars: map[string]string{}, files: map[string]*string{}, docs: docs}
}

var (
	shellVarRE    = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)
	assignmentRE  = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)
	sedDeleteRE   = regexp.MustCompile(`^/(.+)/d$`)
	breOnlySyntax = []string{`\(`, `\)`, `\{`, `\}`, `\|`, `\+`, `\?`, `\<`, `\>`}
)

// expand substitutes $NAME / ${NAME} from literal assignments seen earlier on
// the line (and $HOME from the environment). ok is false when anything the
// shell would still expand remains — an unknown variable, `$(…)`, a backtick.
func (b *bodyFiles) expand(s string) (string, bool) {
	ok := true
	out := shellVarRE.ReplaceAllStringFunc(s, func(m string) string {
		name := strings.Trim(m, "${}")
		if v, found := b.vars[name]; found {
			return v
		}
		if name == "HOME" {
			if h, err := os.UserHomeDir(); err == nil {
				return h
			}
		}
		ok = false
		return m
	})
	if strings.ContainsAny(out, "$`") {
		ok = false
	}
	return out, ok
}

// key normalises a path for the file map: `~/` expanded, then Clean, so
// `./.pr-body.md` and `.pr-body.md` are the same file.
func bodyFileKey(p string) string {
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(h, p[2:])
		}
	}
	return filepath.Clean(p)
}

// resolve returns the content of the body file named by raw, or a non-empty
// opaque description when it cannot be read.
func (b *bodyFiles) resolve(raw string, seg []string) (string, string) {
	if raw == "-" {
		// `--body-file - <<'EOF'`: stdin is the heredoc on this segment.
		if idx := heredocIndexIn(seg); idx >= 0 && idx < len(b.docs) && heredocContentKnown(b.docs[idx]) {
			return b.docs[idx].body, ""
		}
		return "", describeOpaquePath(raw)
	}
	if raw == "" || strings.ContainsAny(raw, "<>") {
		return "", describeOpaquePath(raw)
	}
	path, ok := b.expand(raw)
	if !ok {
		return "", describeOpaquePath(raw)
	}
	key := bodyFileKey(path)
	if c, tracked := b.files[key]; tracked {
		if c == nil {
			return "", "a body file another command on this line rewrites in a way the hook cannot model (" + raw + ")"
		}
		return *c, ""
	}
	if b.tainted {
		return "", "a body file an earlier command on this line could rewrite in a way the hook cannot model (" + raw + ")"
	}
	data, err := os.ReadFile(key)
	if err != nil {
		return "", describeOpaquePath(raw)
	}
	return string(data), ""
}

// current returns the content of path as the walk knows it: tracked content,
// else the file on disk. ok is false when neither is available.
func (b *bodyFiles) current(key string) (string, bool) {
	if c, tracked := b.files[key]; tracked {
		if c == nil {
			return "", false
		}
		return *c, true
	}
	if b.tainted {
		return "", false
	}
	data, err := os.ReadFile(key)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// taint records that an unmodelled command ran: every file's content is now
// unknown, including files the walk has never seen.
func (b *bodyFiles) taint() {
	for k := range b.files {
		b.files[k] = nil
	}
	b.tainted = true
}

func (b *bodyFiles) set(key string, content string, known bool) {
	if !known {
		b.files[key] = nil
		return
	}
	c := content
	b.files[key] = &c
}

// heredocContentKnown reports whether a heredoc body reaches its consumer as
// written: a quoted delimiter, or an unquoted one with nothing to expand.
func heredocContentKnown(d heredoc) bool {
	return d.quoted || !strings.ContainsAny(d.body, "$`")
}

// observe folds one segment into the state, after it has been checked.
//
// Fail closed: a segment is either modelled precisely (assignments, heredoc
// writers, cp/mv, sed -i, rm-like), a recognised no-op (bodyNoop), or it
// taints every file. An allowlist rather than a list of known writers,
// because the writer that matters is the one nobody listed:
// `python3 -c "open('/tmp/b.md','a').write('Co-Authored-By: …')"` between a
// clean heredoc and `--body-file /tmp/b.md` was read as the clean heredoc.
func (b *bodyFiles) observe(seg []string) {
	if len(seg) == 0 {
		return
	}
	// A command substitution runs before its segment does, whatever the
	// segment is — an assignment included.
	if !segmentSubstitutionsSafe(seg, substitutionDepth) {
		b.taint()
	}
	// `NAME=value …` with nothing after it assigns for the rest of the line;
	// `export NAME=value` too. `NAME=value cmd` only sets cmd's environment.
	assigns := seg
	if w := unquote(seg[0]); w == "export" || w == "declare" || w == "local" || w == "readonly" || w == "typeset" {
		assigns = seg[1:]
	}
	allAssign := len(assigns) > 0
	for _, t := range assigns {
		if !assignmentRE.MatchString(t) {
			allAssign = false
			break
		}
	}
	if allAssign {
		for _, t := range assigns {
			m := assignmentRE.FindStringSubmatch(t)
			if v, ok := b.expand(unquote(m[2])); ok {
				b.vars[m[1]] = v
			} else {
				delete(b.vars, m[1]) // set from something unreadable: later $NAME stays opaque
			}
		}
		return
	}

	bare, prog := gateProgram(seg)
	for shellKeywords[prog] && len(bare) > 1 {
		bare, prog = gateProgram(bare[1:]) // `then python3 …`, `do cp …`
	}
	if prog == "" || shellKeywords[prog] {
		return
	}

	// Heredoc producers: `cat > F <<'EOF'`, `cat <<'EOF' >> F`, `tee [-a] F <<'EOF'`.
	doc := -1
	if idx := heredocIndexIn(bare); idx >= 0 && idx < len(b.docs) {
		doc = idx
	}
	var body string
	bodyKnown := false
	if doc >= 0 && (prog == "cat" || prog == "tee") {
		body, bodyKnown = b.docs[doc].body, heredocContentKnown(b.docs[doc])
	}

	// Redirect targets.
	handled := map[string]bool{}
	for i, raw := range bare {
		tok := raw
		op := strings.TrimLeft(tok, "0123456789&")
		if !strings.HasPrefix(op, ">") || strings.HasPrefix(op, ">&") {
			continue
		}
		appendMode := strings.HasPrefix(op, ">>")
		target := strings.TrimLeft(op, ">|")
		if target == "" && i+1 < len(bare) {
			target = unquote(bare[i+1])
		}
		if target == "" || strings.HasPrefix(target, "&") {
			continue
		}
		path, ok := b.expand(target)
		if !ok {
			b.taint() // a write to a path the walk cannot name: it could be any file
			continue
		}
		key := bodyFileKey(path)
		if key == "/dev/null" {
			continue
		}
		handled[key] = true
		if prog == "cat" && doc >= 0 && len(bare) >= 1 {
			b.write(key, body, bodyKnown, appendMode)
			continue
		}
		b.files[key] = nil // written by something this walk does not model
	}

	switch prog {
	case "tee":
		appendMode := false
		for _, raw := range bare[1:] {
			t := unquote(raw)
			if t == "-a" || t == "--append" {
				appendMode = true
				continue
			}
			if strings.HasPrefix(t, "-") || strings.Contains(t, heredocMarker) || strings.HasPrefix(t, ">") {
				continue
			}
			if path, ok := b.expand(t); ok {
				key := bodyFileKey(path)
				if !handled[key] {
					b.write(key, body, doc >= 0 && bodyKnown, appendMode)
				}
			}
		}
	case "cp", "mv":
		var pos []string
		for _, raw := range bare[1:] {
			if t := unquote(raw); !strings.HasPrefix(t, "-") {
				pos = append(pos, t)
			}
		}
		if len(pos) != 2 {
			for _, p := range pos {
				if path, ok := b.expand(p); ok {
					b.files[bodyFileKey(path)] = nil
				}
			}
			return
		}
		src, sok := b.expand(pos[0])
		dst, dok := b.expand(pos[1])
		if !dok {
			return
		}
		dkey := bodyFileKey(dst)
		if st, err := os.Stat(dkey); strings.HasSuffix(pos[1], "/") || (err == nil && st.IsDir()) {
			dkey = bodyFileKey(filepath.Join(dst, filepath.Base(src)))
		}
		if !sok {
			b.files[dkey] = nil
			return
		}
		skey := bodyFileKey(src)
		content, ok := b.current(skey)
		b.set(dkey, content, ok)
		if prog == "mv" {
			b.files[skey] = nil
		}
	case "sed":
		b.observeSed(bare)
	case "rm", "unlink", "truncate", "dd", "ed", "ex":
		// Anything else that can change a file in place: every path argument
		// becomes unknown. Over-broad on purpose — it only matters if that
		// same path is then passed as a body.
		for _, raw := range bare[1:] {
			t := strings.TrimPrefix(unquote(raw), "of=")
			if path, ok := b.expand(t); ok && !strings.HasPrefix(t, "-") {
				b.files[bodyFileKey(path)] = nil
			}
		}
	case "cat", "echo", "printf":
		// Writers only through the redirects handled above.
	default:
		if !bodyNoop(bare, prog) {
			b.taint()
		}
	}
}

// write stores content for key, appending to what the walk knows when asked.
func (b *bodyFiles) write(key, content string, known, appendMode bool) {
	if appendMode {
		prev, ok := b.current(key)
		if !ok {
			// Appending to a file that does not exist yet is a plain write.
			if _, err := os.Stat(key); err == nil {
				b.files[key] = nil
				return
			}
			prev = ""
		}
		content = prev + content
	}
	b.set(key, content, known)
}

// observeSed models `sed -i` on a body file. Only line deletion by pattern —
// `/re/d`, the shape agents use to strip a footer — is simulated; any other
// in-place script leaves the file unknown, because it could just as well ADD
// an attribution line.
func (b *bodyFiles) observeSed(bare []string) {
	inPlace := false
	for _, raw := range bare[1:] {
		f := unquote(raw)
		if f == "--in-place" || strings.HasPrefix(f, "--in-place=") ||
			(strings.HasPrefix(f, "-") && !strings.HasPrefix(f, "--") && strings.ContainsRune(f, 'i')) {
			inPlace = true
		}
	}
	scripts := sedScripts(bare)
	if !inPlace {
		// `w FILE` writes and `e` executes, in-place or not.
		for _, s := range scripts {
			if strings.ContainsAny(s, "wWe\x00") {
				b.taint()
				return
			}
		}
		return
	}
	var deletes []*regexp.Regexp
	for _, s := range scripts {
		m := sedDeleteRE.FindStringSubmatch(s)
		if m == nil || containsAny(m[1], breOnlySyntax) {
			deletes = nil
			break
		}
		re, err := regexp.Compile(m[1])
		if err != nil {
			deletes = nil
			break
		}
		deletes = append(deletes, re)
	}
	if len(deletes) == 0 {
		// Any other in-place script may `w` another file or `e`xecute a
		// command, so its reach is not limited to the named operands.
		b.taint()
		return
	}
	for _, raw := range sedFileOperands(bare) {
		path, ok := b.expand(unquote(raw))
		if !ok {
			continue
		}
		key := bodyFileKey(path)
		content, known := b.current(key)
		if !known || len(deletes) == 0 {
			b.files[key] = nil
			continue
		}
		var kept []string
		for _, line := range strings.SplitAfter(content, "\n") {
			drop := false
			for _, re := range deletes {
				if re.MatchString(strings.TrimSuffix(line, "\n")) {
					drop = true
					break
				}
			}
			if !drop {
				kept = append(kept, line)
			}
		}
		b.set(key, strings.Join(kept, ""), true)
	}
}

// sedScripts returns the script(s) of a sed invocation: each -e/--expression
// value, else the first positional. A -f script file is unreadable here, so
// it is returned as a non-matching placeholder.
func sedScripts(bare []string) []string {
	var scripts []string
	positional := ""
	for i := 1; i < len(bare); i++ {
		t := unquote(bare[i])
		switch {
		case t == "-e" || t == "--expression":
			if i+1 < len(bare) {
				scripts = append(scripts, unquote(bare[i+1]))
			}
			i++
		case strings.HasPrefix(t, "--expression="):
			scripts = append(scripts, strings.TrimPrefix(t, "--expression="))
		case t == "-f" || t == "--file" || strings.HasPrefix(t, "--file="):
			scripts = append(scripts, "\x00unreadable-script-file")
			if t == "-f" || t == "--file" {
				i++
			}
		case t == "-i" && i+1 < len(bare) && unquote(bare[i+1]) == "":
			i++
		case strings.HasPrefix(t, "-") && len(t) > 1:
		case positional == "" && len(scripts) == 0:
			positional = t
		}
	}
	if len(scripts) == 0 && positional != "" {
		scripts = append(scripts, positional)
	}
	return scripts
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// shellKeywords open or close a compound command; the program, if any, is the
// word after them (`then python3 …`). None of them runs anything by itself.
var shellKeywords = map[string]bool{
	"if": true, "then": true, "else": true, "elif": true, "fi": true,
	"do": true, "done": true, "while": true, "until": true, "for": true,
	"{": true, "}": true, "!": true,
}

// bodyNoopPrograms cannot change a file's content except through a shell
// redirect, which observe models separately.
var bodyNoopPrograms = map[string]bool{
	"echo": true, "printf": true, "cat": true, "cd": true, "pwd": true, "ls": true,
	"test": true, "[": true, "[[": true, "true": true, "false": true, ":": true,
	"head": true, "tail": true, "wc": true, "grep": true, "egrep": true, "fgrep": true,
	"rg": true, "which": true, "type": true, "date": true, "sleep": true,
	"mkdir": true, "touch": true, "chmod": true, "jq": true, "basename": true,
	"dirname": true, "realpath": true, "readlink": true, "stat": true, "diff": true,
	"sort": true, "uniq": true, "cut": true, "tr": true, "set": true, "unset": true,
	"bravros": true,
}

// bodyNoop reports whether a segment is known not to write any file.
func bodyNoop(bare []string, prog string) bool {
	switch prog {
	case "git":
		return gitBodyNoop(bare)
	case "gh":
		return ghBodyNoop(bare)
	}
	return bodyNoopPrograms[prog]
}

// gitBodyNoopVerbs leave the working tree's file contents alone. add, commit
// and push are here because `git add … && git commit … && git push && gh pr
// create --body-file …` is the canonical flow.
var gitBodyNoopVerbs = map[string]bool{
	"status": true, "log": true, "diff": true, "show": true, "branch": true,
	"rev-parse": true, "fetch": true, "push": true, "remote": true, "add": true,
	"commit": true, "ls-files": true, "ls-remote": true, "describe": true, "tag": true,
	"symbolic-ref": true, "merge-base": true, "blame": true, "grep": true,
	"shortlog": true, "reflog": true, "cat-file": true, "rev-list": true,
	"for-each-ref": true, "show-ref": true, "name-rev": true,
}

func gitBodyNoop(bare []string) bool {
	verb := ""
	for i := 1; i < len(bare); i++ {
		t := unquote(bare[i])
		switch {
		case t == "-c" || strings.HasPrefix(t, "--config-env") || strings.HasPrefix(t, "--exec-path"):
			return false // `-c core.fsmonitor=…` / `-c alias.x=!…` run programs
		case t == "-C" || t == "--git-dir" || t == "--work-tree":
			i++
		case strings.HasPrefix(t, "-"):
		default:
			if verb == "" {
				verb = t
			}
		}
		if strings.HasPrefix(t, "--output") || t == "--ext-diff" || t == "--exec" ||
			strings.HasPrefix(t, "--receive-pack") || strings.HasPrefix(t, "--upload-pack") {
			return false
		}
	}
	return gitBodyNoopVerbs[verb]
}

// ghBodyNoopGroups are gh's built-in command groups (anything else may be an
// extension, which is arbitrary code); ghBodyWriteVerbs write local files.
var (
	ghBodyNoopGroups = map[string]bool{
		"pr": true, "issue": true, "api": true, "repo": true, "run": true, "auth": true,
		"release": true, "label": true, "workflow": true, "search": true, "browse": true,
		"status": true, "ruleset": true, "secret": true, "variable": true, "cache": true,
		"project": true, "gist": true, "org": true,
	}
	ghBodyWriteVerbs = map[string]bool{
		"download": true, "clone": true, "checkout": true, "sync": true, "--clone": true,
		"--output": true, "-O": true, "--dir": true, "-D": true,
	}
)

func ghBodyNoop(bare []string) bool {
	if len(bare) < 2 || !ghBodyNoopGroups[unquote(bare[1])] {
		return false
	}
	for _, raw := range bare[2:] {
		t := unquote(raw)
		name, _, _ := strings.Cut(t, "=")
		if ghBodyWriteVerbs[t] || ghBodyWriteVerbs[name] {
			return false
		}
	}
	return true
}

// substitutionDepth caps how deep nested `$(…)` are followed before the
// check gives up and fails closed.
const substitutionDepth = 4

// segmentSubstitutionsSafe reports whether every command substitution in seg
// ($(…), `…`, <(…), >(…)) runs only no-op commands.
func segmentSubstitutionsSafe(seg []string, depth int) bool {
	for _, tok := range seg {
		if !textSubstitutionsSafe(tok, depth) {
			return false
		}
	}
	return true
}

// textSubstitutionsSafe applies segmentSubstitutionsSafe's test to raw text.
func textSubstitutionsSafe(text string, depth int) bool {
	inners, ok := substitutionBodies(text)
	if !ok {
		return false
	}
	if len(inners) > 0 && depth <= 0 {
		return false
	}
	for _, inner := range inners {
		if !commandTextNoop(inner, depth-1) {
			return false
		}
	}
	return true
}

// commandTextNoop reports whether a nested command line writes nothing: every
// segment a no-op, no redirect to a file, and its own substitutions safe.
// Quoted heredoc bodies inside it are data (`$(cat <<'EOF' … EOF)`).
func commandTextNoop(text string, depth int) bool {
	pc := parseHeredocs(text)
	for _, d := range pc.docs {
		if d.shell || (!d.quoted && !textSubstitutionsSafe(d.body, depth)) {
			return false
		}
	}
	for _, seg := range commandSegments(pc.stripped) {
		if !segmentSubstitutionsSafe(seg, depth) {
			return false
		}
		bare, prog := gateProgram(seg)
		for shellKeywords[prog] && len(bare) > 1 {
			bare, prog = gateProgram(bare[1:])
		}
		if prog == "" || shellKeywords[prog] {
			continue
		}
		for i, raw := range bare {
			op := strings.TrimLeft(raw, "0123456789&")
			if !strings.HasPrefix(op, ">") || strings.HasPrefix(op, ">&") {
				continue
			}
			target := strings.TrimLeft(op, ">|")
			if target == "" && i+1 < len(bare) {
				target = unquote(bare[i+1])
			}
			if target != "/dev/null" && !strings.HasPrefix(target, "&") {
				return false
			}
		}
		if !bodyNoop(bare, prog) {
			return false
		}
	}
	return true
}

// substitutionBodies returns the inner text of every top-level command
// substitution in text. ok is false for an unbalanced one.
func substitutionBodies(text string) ([]string, bool) {
	if !strings.ContainsAny(text, "$`<>") {
		return nil, true
	}
	var out []string
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '`':
			j := i + 1
			for j < len(runes) && runes[j] != '`' {
				if runes[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(runes) {
				return nil, false
			}
			out = append(out, string(runes[i+1:j]))
			i = j
		case (r == '$' || r == '<' || r == '>') && i+1 < len(runes) && runes[i+1] == '(':
			depth, j := 1, i+2
			for j < len(runes) && depth > 0 {
				switch runes[j] {
				case '\\':
					j++
				case '(':
					depth++
				case ')':
					depth--
				}
				j++
			}
			if depth != 0 {
				return nil, false
			}
			out = append(out, string(runes[i+2:j-1]))
			i = j - 1
		}
	}
	return out, true
}
