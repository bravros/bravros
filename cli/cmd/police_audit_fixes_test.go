package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression tests for the false positives a session audit of ~1,100 Claude
// Code transcripts found in `bravros police pretooluse` (2026-09-30). Every
// "pass" case below was a real, legitimate command the hook refused; every
// "block" case proves the matching genuine violation is still caught.

const auditFooter = "🤖 Generated with [Claude Code](https://claude.com/claude-code)"

// ── Evidence 1: PR bodies from variables, same-command heredocs, copies ──

func TestAuditBodyFileResolution(t *testing.T) {
	rule52TestSetEnv(t)
	dir := t.TempDir()
	t.Chdir(dir)
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("pr-body.md", "## Summary\n\nFixes the thing.\n")
	write("dirty.md", "## Summary\n\nFixes the thing.\n\n"+auditFooter+"\n")
	write("src.md", "## Summary\n\nclean copy source\n")

	pass := []struct{ name, cmd string }{
		{"var dir + suffix", "S=" + dir + "; gh pr create --base main --head homolog --title \"t\" --body-file $S/pr-body.md"},
		{"var whole path", "B=" + dir + "/pr-body.md; gh pr create --title t --body-file $B"},
		{"quoted assignment, braced use", "B=\"" + dir + "/pr-body.md\"; gh pr create --title t --body-file \"${B}\""},
		{"export assignment", "export S=" + dir + " && gh pr create --title t --body-file $S/pr-body.md"},
		{"heredoc then gh on next line", "cat > " + dir + "/new.md <<'EOF'\n## Summary\nclean body\nEOF\ngh pr create --title t --body-file " + dir + "/new.md"},
		{"heredoc with && on operator line", "cat > " + dir + "/new2.md <<'EOF' && gh pr create --title t --body-file " + dir + "/new2.md\n## Summary\nclean\nEOF"},
		{"heredoc via var path", "S=" + dir + "; cat > $S/new3.md <<'EOF'\nclean\nEOF\ngh pr create --title t --body-file $S/new3.md"},
		{"tee heredoc", "tee " + dir + "/new4.md <<'EOF' >/dev/null\nclean\nEOF\ngh pr create --title t --body-file " + dir + "/new4.md"},
		{"cp readable source", "cp " + dir + "/src.md ./.pr-body.md && gh pr create --title t --body-file ./.pr-body.md"},
		{"sed deletes the footer first", "S=" + dir + "; sed -i '' '/Generated with/d' $S/dirty.md; gh pr create --base main --head homolog --title \"t\" --body-file $S/dirty.md"},
		{"stdin from quoted heredoc", "gh pr create --title t --body-file - <<'EOF'\nclean body\nEOF"},
	}
	for _, tc := range pass {
		if msg := checkAiSignature(tc.cmd); msg != "" {
			t.Errorf("%s: must pass, got:\n%s\ncmd: %q", tc.name, msg, tc.cmd)
		}
	}

	attribution := []struct{ name, cmd string }{
		{"var path to attributed file", "S=" + dir + "; gh pr create --title t --body-file $S/dirty.md"},
		{"heredoc body carries footer", "cat > " + dir + "/bad.md <<'EOF'\n## Summary\n\n" + auditFooter + "\nEOF\ngh pr create --title t --body-file " + dir + "/bad.md"},
		{"cp of attributed file", "cp " + dir + "/dirty.md ./.pr-body2.md && gh pr create --title t --body-file ./.pr-body2.md"},
		{"stdin heredoc with footer", "gh pr create --title t --body-file - <<'EOF'\nbody\nCo-Authored-By: Claude <noreply@anthropic.com>\nEOF"},
		{"commit -m heredoc in quotes", "git commit -m \"$(cat <<'EOF'\nfix: x\n\nCo-Authored-By: Claude <noreply@anthropic.com>\nEOF\n)\""},
	}
	for _, tc := range attribution {
		if msg := checkAiSignature(tc.cmd); !strings.Contains(msg, "AI signature detected") {
			t.Errorf("%s: must block as attribution, got %q", tc.name, msg)
		}
	}

	opaque := []struct{ name, cmd string }{
		{"var from substitution", "S=$(mktemp); gh pr create --title t --body-file $S"},
		{"unset var", "gh pr create --title t --body-file $UNSET_AUDIT_VAR/x.md"},
		{"env-prefix is not a line assignment", "S=" + dir + " gh pr create --title t --body-file $S/pr-body.md"},
		{"unquoted heredoc with substitution", "cat > " + dir + "/u.md <<EOF\n$(printf 'x')\nEOF\ngh pr create --title t --body-file " + dir + "/u.md"},
		{"append by unmodelled echo", "echo 'Made with Cursor' >> " + dir + "/pr-body.md && gh pr create --title t --body-file " + dir + "/pr-body.md"},
		{"sed non-delete script", "sed -i '' 's/Fixes/Made with Cursor/' " + dir + "/pr-body.md; gh pr create --title t --body-file " + dir + "/pr-body.md"},
		{"process substitution", "gh pr create --title t --body-file <(printf x)"},
		{"bare stdin", "gh pr create --title t --body-file -"},
		{"missing file", "gh pr create --title t --body-file " + dir + "/nope.md"},
	}
	for _, tc := range opaque {
		msg := checkAiSignature(tc.cmd)
		if !strings.Contains(msg, "cannot read") {
			t.Errorf("%s: must stay fail-closed, got %q", tc.name, msg)
			continue
		}
		if !strings.Contains(msg, "Run instead:") {
			t.Errorf("%s: opaque block must name the alternative: %q", tc.name, msg)
		}
	}
}

// ── Evidence 2 + 3: gate-input floor narrowed to real writes of real inputs ──

func TestAuditGateInputNarrowed(t *testing.T) {
	home := rule52TestSetEnv(t)
	laneRepo(t, "")

	pass := []string{
		// 2: state subdirectories are not gate inputs.
		`cp /tmp/audit/* ~/.claude/state/jev-audit/latest/`,
		`echo x > ~/.claude/state/jev-audit/notes.txt`,
		`mkdir -p ~/.claude/state/jev-audit && cp /tmp/a.jsonl ~/.claude/state/jev-audit/cache.jsonl`,
		// 2: a state path inside the sed SCRIPT is text, not the file edited.
		`sed -i 's#~/.claude/state/jev-audit/cache.jsonl#/tmp/cache.jsonl#' script.py`,
		`sed -i '' 's#/tmp/x#~/.claude/state/police-token#' script.py`,
		// 2: setup.json is read by no gate.
		`bravros version; ls ~/.claude/agents/*.md; python3 -c "import json;print(json.dumps(json.load(open('` + home + `/.claude/state/setup.json'))))"`,
		// 2: the hook's own block of this very audit's transcript mining.
		"cd ~/.claude/state/jev-audit && python3 - <<'EOF'\nimport json\nfor line in open('cache.jsonl'):\n    print(json.dumps(json.loads(line))[:80])\nEOF",
		// 3: a read of the config and an unrelated interpreter on one line.
		`echo "== chain-core"; find chain-core -type f -not -path '*/node_modules/*' -not -path '*/dist/*' | head -40; echo "== composer scripts"; python3 -c "import json;print(json.dumps(json.load(open('composer.json'))['scripts'],indent=1))"; echo "== bravros config"; cat .bravros/config.json`,
		`gh secret list && cat .planning/.review-stamp-1.json && python3 -c "import json;print(json.dumps({'a':1}))"`,
		// 3/4: a doc heredoc that merely names the gate files.
		"cat > docs/police.md <<'MD'\nNever run `touch ~/.claude/state/police-token` or\n`printf x > .bravros/config.json`.\nMD",
	}
	for _, cmd := range pass {
		if msg := checkGateInputWrite(cmd); msg != "" {
			t.Errorf("must pass: %q\n%s", cmd, msg)
		}
	}

	block := []struct{ cmd, path string }{
		{`sed -i 's/a/b/' ~/.claude/state/police-token`, "~/.claude/state/police-token"},
		{`sed -i -e 's#x#y#' ~/.claude/state/promote-token`, "~/.claude/state/promote-token"},
		{`cp /tmp/t/* ~/.claude/state/`, "~/.claude/state/"},
		{`echo x > ~/.claude/state/{police,promote}-token`, "~/.claude/state/{police"},
		{`touch ~/.claude/state/destructive-token`, "~/.claude/state/destructive-token"},
		{`cat .bravros/config.json; python3 -c "open('.bravros/config.json','w').write('{}')"`, ".bravros/config.json"},
		{"cat > .bravros/config.json <<'EOF'\n{}\nEOF", ".bravros/config.json"},
		{"python3 - <<'EOF'\nopen('" + home + "/.claude/state/police-token','w').write('x')\nEOF", home + "/.claude/state/police-token"},
		{"cat <<'EOF' | bash\ntouch ~/.claude/state/police-token\nEOF", "~/.claude/state/police-token"},
	}
	for _, tc := range block {
		msg := checkGateInputWrite(tc.cmd)
		if msg == "" {
			t.Errorf("must block: %q", tc.cmd)
			continue
		}
		if !strings.Contains(msg, "("+tc.path+")") {
			t.Errorf("%q: block must name %q: %q", tc.cmd, tc.path, msg)
		}
	}
}

// ── Evidence 4 + 5: heredoc bodies are data unless a shell runs them ──

func TestAuditHeredocBodiesAreData(t *testing.T) {
	rule52TestSetEnv(t)
	laneRepo(t, "")

	pass := []string{
		// 4: substitution-shaped text inside a Python heredoc.
		"python3 - <<'EOF'\nimport subprocess\n# $GH push origin main\nx = \"$(command -v gh)\" \n$(command -v gh) api repos/o/r\nprint(x)\nEOF",
		// 4: a markdown doc.
		"cat > beast/github-actions-runners.md <<'MD'\n# Runners\n$(command -v gh) api repos/o/r/actions/runners\ngit push origin main\ngh pr merge 12 --merge\nMD",
		// 5: a Python heredoc editing README.md; no push anywhere.
		"python3 - <<'EOF'\nfrom pathlib import Path\np = Path('README.md')\np.write_text(p.read_text() + '''\ngit push origin main\n''')\nEOF",
		// <<- with tab-indented terminator.
		"cat > notes.md <<-'EOF'\n\tgit push origin main\n\tEOF",
	}
	for _, cmd := range pass {
		if v, d := evaluateMergeGate(cmd); v != mergeAllowed {
			t.Errorf("must pass: %q → verdict %v (%s)", cmd, v, d)
		}
		if blocked, _, reason := rule52TestInvoke(t, cmd); blocked {
			t.Errorf("hook must pass: %q\n%s", cmd, reason)
		}
	}

	block := []string{
		"bash <<'EOF'\ngit push origin main\nEOF",
		"cat <<'EOF' | sh\ngit push origin main\nEOF",
		"ssh host <<'EOF'\ngit push origin main\nEOF",
		// Written as data, then run later on the same command.
		"cat > x.sh <<'EOF'\ngit push origin main\nEOF\nbash x.sh",
		"cat > x.sh <<'EOF'\ngit push origin main\nEOF\nchmod +x x.sh && ./x.sh",
		"cat > x.sh <<EOF\n$(git push origin main)\nEOF",
		"python3 - <<'EOF'\nprint(1)\nEOF\ngit push origin main",
		"cat > a.md <<'MD'\ntext\nMD\n$GH push origin main",
		"git push origin main",
	}
	for _, cmd := range block {
		if v, _ := evaluateMergeGate(cmd); v == mergeAllowed {
			t.Errorf("must block: %q", cmd)
		}
		if blocked, _, _ := rule52TestInvoke(t, cmd); !blocked {
			t.Errorf("hook must block: %q", cmd)
		}
	}
}

func TestAuditParseHeredocs(t *testing.T) {
	cases := []struct {
		name       string
		cmd        string
		docs       int
		quoted     bool
		executable bool
		body       string
	}{
		{"quoted data", "cat > f <<'EOF'\nhello\nEOF", 1, true, false, "hello\n"},
		{"double-quoted delim", "cat > f <<\"EOF\"\nhello\nEOF", 1, true, false, "hello\n"},
		{"backslash delim", "cat > f <<\\EOF\nhello\nEOF", 1, true, false, "hello\n"},
		{"unquoted is executable", "cat > f <<EOF\nhello\nEOF", 1, false, true, "hello\n"},
		{"shell consumer", "bash <<'EOF'\nls\nEOF", 1, true, true, "ls\n"},
		{"piped into shell", "cat <<'EOF' | zsh\nls\nEOF", 1, true, true, "ls\n"},
		{"inside double quotes is text", "git commit -m \"$(cat <<'EOF'\nmsg\nEOF\n)\"", 0, false, false, ""},
		{"here-string is not a heredoc", "cat <<< 'x'", 0, false, false, ""},
	}
	for _, tc := range cases {
		p := parseHeredocs(tc.cmd)
		if len(p.docs) != tc.docs {
			t.Errorf("%s: docs = %d, want %d", tc.name, len(p.docs), tc.docs)
			continue
		}
		if tc.docs == 0 {
			if p.stripped != tc.cmd {
				t.Errorf("%s: stripped changed: %q", tc.name, p.stripped)
			}
			continue
		}
		d := p.docs[0]
		if d.quoted != tc.quoted || d.executable() != tc.executable || d.body != tc.body {
			t.Errorf("%s: got quoted=%v exec=%v body=%q", tc.name, d.quoted, d.executable(), d.body)
		}
		if strings.Contains(p.stripped, tc.body) && tc.body != "" && !d.executable() {
			t.Errorf("%s: body left in stripped command: %q", tc.name, p.stripped)
		}
	}
}

// ── Evidence 6: every block ends with the exact alternative to run ──

func TestAuditBlockMessagesEndWithAlternative(t *testing.T) {
	for kind, want := range map[gateInputKind]string{
		gateInputToken:  "bravros police unlock",
		gateInputStamp:  "bravros pr-review <N> --write-stamp",
		gateInputConfig: "bravros police direct-main on",
	} {
		msg := strings.TrimRight(gateInputBlockMessage(kind, "x"), "\n")
		last := msg[strings.LastIndex(msg, "\n")+1:]
		if !strings.HasPrefix(last, "Run instead:") || !strings.Contains(last, want) {
			t.Errorf("kind %d: last line must be the alternative (%q): %q", kind, want, last)
		}
	}
	msg := strings.TrimRight(strings.Replace(aiSignatureOpaque, "%s", "x", 1), "\n")
	last := msg[strings.LastIndex(msg, "\n")+1:]
	if !strings.HasPrefix(last, "Run instead:") || !strings.Contains(last, "--body-file") {
		t.Errorf("opaque body block must end with the alternative: %q", last)
	}
}
