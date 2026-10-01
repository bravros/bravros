package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Security-review blockers on PR 103 (the heredoc/body-file false-positive
// fix): an interpreter heredoc was a merge-gate bypass, and an unmodelled
// writer between a tracked body and --body-file was an attribution bypass.

func TestSecurityInterpreterHeredocMergeRoutes(t *testing.T) {
	rule52TestSetEnv(t)
	laneRepo(t, "")

	block := []string{
		"python3 - <<'EOF'\nimport subprocess\nsubprocess.run([\"gh\",\"pr\",\"merge\",\"12\",\"--merge\"])\nEOF",
		"python3 - <<'EOF'\nimport subprocess\nsubprocess.run([\"git\",\"push\",\"origin\",\"main\"])\nEOF",
		"node - <<'EOF'\nrequire('child_process').execSync('git push origin main')\nEOF",
		// Unquoted delimiter: shell-tokenized, but the call is still one word.
		"python3 - <<EOF\nimport subprocess\nsubprocess.run(['gh', 'pr', 'merge', '12'])\nEOF",
		// Split literal.
		"python3 - <<'EOF'\nimport os\nos.system('g' + 'h pr merge 12 --merge')\nEOF",
		// Written to a file and run later on the line.
		"cat > m.py <<'EOF'\nimport subprocess\nsubprocess.run(['gh','pr','merge','3'])\nEOF\npython3 m.py",
		// Merge endpoint over HTTP from a heredoc.
		"python3 - <<'EOF'\nimport requests\nrequests.put('https://api.github.com/repos/o/r/pulls/1/merge')\nEOF",
		// Backtick execution fed straight to ruby.
		"ruby <<'EOF'\n`gh pr merge 12 --merge`\nEOF",
	}
	for _, cmd := range block {
		if v, _ := evaluateMergeGate(cmd); v == mergeAllowed {
			t.Errorf("must block: %q", cmd)
		}
		if blocked, _, _ := rule52TestInvoke(t, cmd); !blocked {
			t.Errorf("hook must block: %q", cmd)
		}
	}

	pass := []string{
		// A script that edits README.md, spelling commands only as prose.
		"python3 - <<'EOF'\nfrom pathlib import Path\np = Path('README.md')\np.write_text(p.read_text() + '''\ngit push origin main\ngh pr merge 12\n''')\nEOF",
		// subprocess, but no merge route in any literal.
		"python3 - <<'EOF'\nimport subprocess\nprint(subprocess.run(['git','status'], capture_output=True).stdout)\nEOF",
		"cat > beast/github-actions-runners.md <<'MD'\n# Runners\nUse `gh api` and `git push origin main` with care.\nMD",
		// The same doc on a line that also runs python: its backticks run nothing.
		"cat > docs/runners.md <<'MD'\nUse `gh api` and `git push origin main` with care.\nMD\npython3 -c 'print(1)'",
	}
	for _, cmd := range pass {
		if v, d := evaluateMergeGate(cmd); v != mergeAllowed {
			t.Errorf("must pass: %q → verdict %v (%s)", cmd, v, d)
		}
		if blocked, _, reason := rule52TestInvoke(t, cmd); blocked {
			t.Errorf("hook must pass: %q\n%s", cmd, reason)
		}
	}
}

func TestSecurityUnmodelledWriterTaintsBodyFiles(t *testing.T) {
	rule52TestSetEnv(t)
	dir := t.TempDir()
	t.Chdir(dir)
	clean := filepath.Join(dir, "pr-body.md")
	if err := os.WriteFile(clean, []byte("## Summary\n\nFixes the thing.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty := filepath.Join(dir, "dirty.md")
	if err := os.WriteFile(dirty, []byte("## Summary\n\n"+auditFooter+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(dir, "b.md")

	block := []struct{ name, cmd string }{
		{"python -c appends to heredoc body", "cat > " + b + " <<'EOF'\nclean\nEOF\npython3 -c \"open('" + b + "','a').write('Co-Authored-By: Claude <noreply@anthropic.com>')\"\ngh pr create --title t --body-file " + b},
		{"node -e appends to heredoc body", "cat > " + b + " <<'EOF'\nclean\nEOF\nnode -e \"require('fs').appendFileSync('" + b + "','Co-Authored-By: Claude <noreply@anthropic.com>')\"\ngh pr create --title t --body-file " + b},
		{"python -c appends to on-disk body", "python3 -c \"open('" + clean + "','a').write('Made with Cursor')\" && gh pr create --title t --body-file " + clean},
		{"perl -e with path inside the script", "perl -e 'open(F,\">>" + clean + "\"); print F \"x\"' && gh pr create --title t --body-file " + clean},
		{"substitution in an assignment", "X=$(python3 -c \"open('" + clean + "','a').write('x')\"); gh pr create --title t --body-file " + clean},
		{"substitution in the gh segment", "gh pr create --title \"$(python3 -c \"open('" + clean + "','a').write('x')\")\" --body-file " + clean},
		{"keyword-prefixed writer", "if true; then python3 -c \"open('" + clean + "','a').write('x')\"; fi; gh pr create --title t --body-file " + clean},
		{"redirect to unknown path", "echo x > $UNKNOWN_SEC_VAR; gh pr create --title t --body-file " + clean},
		{"sed w command", "sed -n 's/Summary/Made with Cursor/w " + clean + "' " + dirty + "; gh pr create --title t --body-file " + clean},
		{"gh extension", "gh my-ext " + clean + " && gh pr create --title t --body-file " + clean},
	}
	for _, tc := range block {
		if msg := checkAiSignature(tc.cmd); msg == "" {
			t.Errorf("%s: must block\ncmd: %q", tc.name, tc.cmd)
		}
	}

	pass := []struct{ name, cmd string }{
		{"sed footer strip via var", "S=" + dir + "; sed -i '' '/Generated with/d' $S/dirty.md; gh pr create --base main --head homolog --title \"t\" --body-file $S/dirty.md"},
		{"heredoc then gh", "cat > " + b + " <<'EOF'\n## Summary\nclean\nEOF\ngh pr create --title t --body-file " + b},
		{"canonical add/commit/push flow", "cd " + dir + " && git add -A && git commit -m \"$(cat <<'EOF'\nfix: x\nEOF\n)\" && git push -u origin feat && gh pr create --title \"$(git log -1 --format=%s)\" --body-file " + clean},
		{"read-only noise", "ls " + dir + " && echo ok && gh pr view 3 --json body >/dev/null 2>&1; gh pr create --title t --body-file " + clean},
		{"bravros commit first", "bravros commit \"🐛 fix: x\" a.go && gh pr create --title t --body-file " + clean},
		{"writer after the gh call", "gh pr create --title t --body-file " + clean + " && python3 -c 'print(1)'"},
	}
	for _, tc := range pass {
		if msg := checkAiSignature(tc.cmd); msg != "" {
			t.Errorf("%s: must pass, got:\n%s\ncmd: %q", tc.name, msg, tc.cmd)
		}
	}
}

func TestSecurityStandDownBareTTLNamesUnit(t *testing.T) {
	_, err := parseStandDownTTL("90")
	if err == nil || !strings.Contains(err.Error(), "bare numbers are hours") || !strings.Contains(err.Error(), "90m") {
		t.Errorf("bare over-cap ttl must name the unit and suggest minutes: %v", err)
	}
	if _, err := parseStandDownTTL("100h"); err == nil || strings.Contains(err.Error(), "bare numbers") {
		t.Errorf("a unit-suffixed ttl must keep the plain cap message: %v", err)
	}
}
