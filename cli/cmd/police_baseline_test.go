package cmd

import "testing"

// Every case from origin/homolog's own TestIsMainMerge, run verbatim against
// the current gate.
//
// This is the pre-PR contract. Fifteen review rounds rewrote nearly every line
// behind it, and in round 15 one of those rewrites silently deleted a check
// that predated the whole effort — a bare push on a protected branch — because
// the machinery built around it was being removed and nothing pinned the
// original behaviour underneath. Anything that flips here is a regression this
// PR introduced, whatever else it fixed.
func TestPrePRBaselineHolds(t *testing.T) {
	session, _ := twoReposOn(t, "feature/x", "feature/y")
	installPrePush(t, session)
	cases := []struct {
		name    string
		cmd     string
		blocked bool
	}{
		{"push main", "git push origin main", true},
		{"push master", "git push origin master", true},
		{"push -u main", "git push -u origin main", true},
		{"push HEAD:main", "git push origin HEAD:main", true},
		{"force push main:main", "git push origin +main:main", true},
		{"push refs/heads/main", "git push origin refs/heads/main", true},
		{"push homolog", "git push origin homolog", false},
		{"push -u homolog", "git push -u origin homolog", false},
		{"push feature branch", "git push origin feat/test-branch", false},
		{"branch containing main", "git push origin fix/maintain-cache", false},
		{"branch prefixed main", "git push origin mainline-experiment", false},
		{"commit mentioning main", "git commit -m 'update main branch docs'", false},
		{"status", "git status", false},
		{"npm test", "npm test", false},
		{"echoed merge", "echo 'gh pr merge'", false},
		{"subshell git show", `(sed -n '80,100p' cli/cmd/active_command.go; echo "=== x ==="; git stash list >/dev/null; git show origin/main:cli/cmd/police_test.go >/dev/null 2>&1 && echo a || echo b)`, false},
		{"quoted main target", `git push origin "main"`, true},
		{"single quoted main target", `git push origin 'main'`, true},
		{"commit message with semicolon", `git commit -m "fix bug; git push origin main"`, false},
		{"empty", "", false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			v, _ := evaluateMergeGate(tt.cmd)
			if got := v != mergeAllowed; got != tt.blocked {
				t.Errorf("blocked=%v want %v (verdict %v) for %q", got, tt.blocked, v, tt.cmd)
			}
		})
	}
}
