package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestFinalSweep is the two-sided contract, kept together on purpose: this gate
// runs on EVERY Bash call an agent makes, so an over-block is a real cost and
// the security cases are what it is for. Twelve review rounds optimised one
// side; the adversarial review found the other side had drifted badly.
func TestFinalSweep(t *testing.T) {
	session, other := twoReposOn(t, "feature/x", "main")
	installPrePush(t, session)
	if err := os.MkdirAll(filepath.Join(session, "cli", "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Run("all-13-overblocks-cleared", func(t *testing.T) {
		for _, c := range []string{
			`grep -rn -e "gh pr merge" skills/`, `rg -e 'git push' docs/`,
			`sed -i -e 's/git push origin main/x/' f.md`,
			`grep -c "git push" f.md`, `grep -c "git push origin main" f.md`,
			"git commit --no-verify -m wip && git push origin feature/x",
			"git commit --no-verify -m wip && git push",
			"cd cli && go test ./cmd/ && git push",
			"cd " + session + " && git push",
			"git -C cli status && git push",
			"git add .bravros.yml && git commit -m x && git push",
			`bravros commit "fix: pre-push hook" x && git push`,
			`bash -c "git fetch && git push"`,
			`bash -c "gh api repos/o/r/pulls/5 --jq .baseRefName"`,
			`curl -s https://api.github.com/repos/o/r/pulls/1/merge`,
			`curl https://api.github.com/repos/o/r/pulls/1`,
		} {
			if v, _ := evaluateMergeGate(c); v != mergeAllowed {
				t.Errorf("OVER-BLOCK [%v]: %s", v, c)
			}
		}
	})
	t.Run("nothing-security-relevant-regressed", func(t *testing.T) {
		for _, c := range []string{
			"git push origin main", "git push --no-verify",
			"git -c core.hookspath=/dev/null push",
			"git send-pack ../bare.git --all", "git send-pack origin HEAD:main",
			"gh api -X PUT repos/o/r/pulls/$PR/merge",
			"gh api -X POST repos/o/r/merges -f base=$BASE",
			`gh api graphql -f query="$Q"`,
			"gh pr merge https://github.com/other/repo/pull/5 --merge",
			`curl -X PUT https://api.github.com/repos/o/r/pulls/1/merge`,
			`curl -X POST https://api.github.com/graphql -d '{"query":"mutation{mergePullRequest(input:{})}"}'`,
			`python3 -c "import requests; requests.put('https://api.github.com/repos/o/r/pulls/1/merge')"`,
			`bash -c "gh pr merge 5 --merge"`,
			`node -e "execSync('gh pr merge 5 --merge')"`,
			`bash -c "git push --no-verify origin main"`,
			"sudo git push origin main", "{ git push origin main; }",
			"git push " + other + " main", "git push /var/repos/mirror.git main",
		} {
			if v, _ := evaluateMergeGate(c); v == mergeAllowed {
				t.Errorf("SECURITY REGRESSION: %s", c)
			}
		}
	})
}

// TestPushArmScopeIsDeliberatelyNarrow records what the push arm gave up, and
// why — so the next reader finds a decision here rather than an oversight.
//
// Through round 14 this arm also tried to protect the pre-push hook itself:
// refusing a push that relocated to another checkout, one that reached into the
// hooks directory, and one running where no hook was installed. Four
// consecutive review rounds found a defect in that machinery, each introduced
// by the previous round's fix, and a commissioned adversarial review showed the
// whole of it falls to `printf '…' | sh`, a heredoc, or
// `git config alias.p 'push --no-verify'` — verified against real git. It cost
// every `cd` in the working day and bought nothing against intent, so the
// operator's call was to delete it.
//
// Each command below is therefore ALLOWED BY THIS GATE on purpose. None is
// unprotected in practice: a push still runs the pre-push hook of whatever repo
// it lands in, and that hook reads the real ref list. What is gone is this
// gate's attempt to second-guess it.
func TestPushArmScopeIsDeliberatelyNarrow(t *testing.T) {
	session, other := twoReposOn(t, "feature/x", "feature/y")
	installPrePush(t, session)

	for _, cmd := range []string{
		// Reaches the hook file. The push that follows still runs whatever hook
		// is there at the time; this gate no longer tries to notice.
		"rm -f .bravros/hooks/pre-push && git push",
		"rm -rf .bravros && git push",
		"chmod -x .bravros/hooks/pre-push && git push",
		// Lands in another checkout, which enforces itself.
		"cd " + other + " && git push",
		"git -C " + other + " push",
		"GIT_DIR=" + other + "/.git git push",
		// A bare push to another repo, from a non-protected branch.
		"git push upstream",
	} {
		if v, _ := evaluateMergeGate(cmd); v != mergeAllowed {
			t.Errorf("deliberately out of scope, must not block: %s (got %v)", cmd, v)
		}
	}

	// What the arm still holds, and must keep holding.
	for _, cmd := range []string{
		"git push origin main",                 // literal protected refspec
		"git push --no-verify",                 // disables the hook it defers to
		"git -c core.hookspath=/dev/null push", // redirects the hook away
		"BRAVROS_ALLOW_PROTECTED_PUSH=1 git push",
		"git send-pack ../bare.git --all", // plumbing: no hook exists
		"sudo git push origin main",       // any wrapper
		`bash -c "git push origin main"`,  // any quoting
	} {
		if v, _ := evaluateMergeGate(cmd); v == mergeAllowed {
			t.Errorf("still in scope, must block: %s", cmd)
		}
	}
}

// TestBarePushOnProtectedBranch is the regression test for PR 90 round 15, and
// the case whose absence let the regression ship.
//
// `pushTargetsProtected`'s bare-push fallback — `protectedBranches[currentBranch()]`
// — predates this entire PR. It was deleted along with the hook-protection
// machinery that had grown around it, which left `git checkout main && git push`
// unblocked in a repo with no working hook: the exact outcome the gate exists
// to prevent, via the most ordinary command there is.
//
// Every test that touched a bare push put the session on a FEATURE branch, so
// none of them could see it. This one stands on main, and stands there with and
// without a hook, because the answer must not depend on that.
func TestBarePushOnProtectedBranch(t *testing.T) {
	onMain := func(t *testing.T, withHook bool) string {
		t.Helper()
		dir := t.TempDir()
		for _, a := range [][]string{
			{"init", "-q", "-b", "main", "."},
			{"remote", "add", "origin", "git@github.com:skaisser/scratch.git"},
			{"commit", "-q", "--allow-empty", "-m", "seed"},
		} {
			c := exec.Command("git", a...)
			c.Dir = dir
			c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
				"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
			if out, err := c.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", a, err, out)
			}
		}
		if withHook {
			installPrePush(t, dir)
		}
		t.Chdir(dir)
		return dir
	}

	t.Run("no hook installed", func(t *testing.T) {
		onMain(t, false)
		if v, _ := evaluateMergeGate("git push"); v == mergeAllowed {
			t.Error("bare push on main with no hook must not be allowed")
		}
	})

	t.Run("hook installed", func(t *testing.T) {
		onMain(t, true)
		if v, _ := evaluateMergeGate("git push"); v == mergeAllowed {
			t.Error("bare push on main must not be allowed")
		}
	})

	t.Run("the opt-out still applies", func(t *testing.T) {
		dir := onMain(t, true)
		if err := os.MkdirAll(filepath.Join(dir, ".bravros"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".bravros", "config.json"),
			[]byte(`{"police":{"direct_main":true}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if v, _ := evaluateMergeGate("git push"); v != mergeAllowed {
			t.Errorf("direct_main must excuse a bare push on its own main, got %v", v)
		}
	})

	t.Run("a feature branch is untouched", func(t *testing.T) {
		session, _ := twoReposOn(t, "feature/x", "feature/y")
		installPrePush(t, session)
		for _, c := range []string{"git push", "git push upstream", "git push origin feature/x"} {
			if v, _ := evaluateMergeGate(c); v != mergeAllowed {
				t.Errorf("%s from a feature branch must stay allowed, got %v", c, v)
			}
		}
	})
}

// TestOptOutNeverExcusesARelocatedBarePush is the regression test for PR 90
// round 16: the bare-push case restored in round 15 copied the pre-PR check
// verbatim, but pre-PR had no relocation concept — so it did not inherit the
// `relocates` guard its sibling `case protected:` already carried.
//
// A bare push names no destination, so pushTargetRepo always answers "", and
// the branch is read from the SESSION's checkout. Under direct_main that let a
// push into a different repo's main be excused by a local opt-out.
func TestOptOutNeverExcusesARelocatedBarePush(t *testing.T) {
	mk := func(t *testing.T, branch, slug string) string {
		t.Helper()
		dir := t.TempDir()
		for _, a := range [][]string{
			{"init", "-q", "-b", branch, "."},
			{"remote", "add", "origin", "git@github.com:" + slug + ".git"},
			{"commit", "-q", "--allow-empty", "-m", "seed"},
		} {
			c := exec.Command("git", a...)
			c.Dir = dir
			c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
				"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
			if out, err := c.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", a, err, out)
			}
		}
		return dir
	}
	session := mk(t, "main", "skaisser/scratch")
	other := mk(t, "main", "other-org/protected")
	if err := os.MkdirAll(filepath.Join(session, ".bravros"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(session, ".bravros", "config.json"),
		[]byte(`{"police":{"direct_main":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(session)

	for _, cmd := range []string{
		"cd " + other + " && git push",
		"git -C " + other + " push",
		"GIT_DIR=" + other + "/.git git push",
		"(cd " + other + ") ; git push",
	} {
		if v, _ := evaluateMergeGate(cmd); v == mergeAllowed {
			t.Errorf("the local opt-out must not excuse a relocated bare push: %s", cmd)
		}
	}

	// In place, the opt-out is exactly what it is for.
	if v, _ := evaluateMergeGate("git push"); v != mergeAllowed {
		t.Errorf("direct_main must excuse a bare push on its own main, got %v", v)
	}
}
