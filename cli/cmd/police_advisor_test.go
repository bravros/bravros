package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// The findings of a fresh-eyes adversarial review commissioned after twelve
// incremental rounds, each verified by execution before it was fixed.
//
// F1 is the one that matters most: `gh api -X PUT repos/o/r/pulls/$PR/merge` is
// B-0036's exact route with one shell variable, and it was mergeALLOWED. Twelve
// rounds of narrow review never reached it, because every round asked "what
// spelling of the command escapes the parser" and none asked "what does the
// parser do when it reads the route correctly but cannot read a value in it".
func TestAdvisorFindingsFixed(t *testing.T) {
	t.Run("F1-unreadable-fails-closed", func(t *testing.T) {
		gitRepo(t, "git@github.com:skaisser/scratch.git", "")
		for _, c := range []string{
			`gh api -X PUT repos/o/r/pulls/$PR/merge`,
			`gh api -X PUT "repos/o/r/pulls/${PR}/merge"`,
			`gh api -X PUT repos/o/r/pulls/{pull_number}/merge`,
			`gh api -X POST repos/o/r/merges -f base=$BASE`,
			`gh api graphql -f query="$Q"`,
			"gh api graphql -f query=`cat m.gql`",
		} {
			if v, _ := evaluateMergeGate(c); v == mergeAllowed {
				t.Errorf("STILL FAIL-OPEN: %s", c)
			}
		}
	})
	t.Run("F2-optout-cannot-excuse-indeterminate", func(t *testing.T) {
		gitRepo(t, "git@github.com:skaisser/scratch.git", `{"police":{"direct_main":true}}`)
		for _, c := range []string{
			`gh api graphql -f query=mutation{mergePullRequest(input:{pullRequestId:"PR_kwDO"})}`,
			`gh api graphql --input body.json`,
			`gh api -X PUT repos/o/r/pulls/$PR/merge`,
		} {
			if v, _ := evaluateMergeGate(c); v == mergeAllowed {
				t.Errorf("STILL EXCUSED: %s", c)
			}
		}
		// direct_main must still do its actual job.
		if v, _ := evaluateMergeGate("git push origin main"); v != mergeAllowed {
			t.Errorf("direct_main broke: got %v", v)
		}
	})
	t.Run("F3-pr-arg-forwarded", func(t *testing.T) {
		gitRepo(t, "git@github.com:skaisser/scratch.git", "")
		for _, c := range []string{
			"gh pr merge https://github.com/other/repo/pull/5 --merge",
			"gh pr merge some-branch --merge",
			"gh pr merge 2044 -R paylog/ev --merge",
		} {
			if v, _ := evaluateMergeGate(c); v == mergeAllowed {
				t.Errorf("STILL ALLOWED: %s", c)
			}
		}
	})
	t.Run("over-blocks-cleared", func(t *testing.T) {
		session, _ := twoReposOn(t, "feature/x", "feature/y")
		installPrePush(t, session)
		if err := os.MkdirAll(filepath.Join(session, "cli"), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, c := range []string{
			`grep -e "gh pr merge" notes.md`,
			`grep -e "git push" -r .`,
			"cd cli && go build ./... && git push",
			"cd cli && git push",
			"git commit --no-verify -m x && git push origin feature/x",
			"git commit --no-verify -m x && git push",
		} {
			if v, _ := evaluateMergeGate(c); v != mergeAllowed {
				t.Errorf("STILL OVER-BLOCKED: %s -> %v", c, v)
			}
		}
	})
	// Findings 4 and 7 share one root cause: pushTargetsProtected special-cased
	// only `origin`/URLs, so any other destination counted as a refspec, set
	// explicit=true, and skipped every refspec-less safety case below it.
	t.Run("F4-F7-first-positional-is-the-repository", func(t *testing.T) {
		session, _ := twoReposOn(t, "feature/x", "feature/y")
		installPrePush(t, session)
		// Hookless plumbing pushing every ref, cleared because the destination
		// path was mistaken for a refspec.
		for _, c := range []string{
			"git send-pack ../bare.git --all",
			"git send-pack ../bare.git --mirror",
			"git send-pack ../bare.git",
		} {
			if v, _ := evaluateMergeGate(c); v != mergeUnenforced {
				t.Errorf("%s: got %v want mergeUnenforced", c, v)
			}
		}
		// A bare push to a non-origin remote is still classified as BARE, which
		// is what routes it to the branch-in-hand check rather than being
		// mistaken for a refspec.
		if _, explicit := pushTargetsProtected(seg(t, "git push upstream")); explicit {
			t.Error("a non-origin remote name is the repository, not a refspec")
		}
	})

	// Finding 6: the substring scan was overriding a payload the expansion had
	// already judged correctly, turning an ordinary deferred push into a block.
	t.Run("F6-marker-scan-defers-to-expansion", func(t *testing.T) {
		session, _ := twoReposOn(t, "feature/x", "feature/y")
		installPrePush(t, session)
		if v, _ := evaluateMergeGate(`bash -c "git fetch && git push"`); v != mergeAllowed {
			t.Errorf(`bash -c "git fetch && git push" must defer like any push, got %v`, v)
		}
		// The expansion still judges a real one, and the scan still catches a
		// payload it could not parse.
		if v, _ := evaluateMergeGate(`bash -c "git push --no-verify origin main"`); v != mergeUnenforced {
			t.Errorf("expansion must still judge a real push, got %v", v)
		}
		if v, _ := evaluateMergeGate(`node -e "execSync('gh pr merge 5 --merge')"`); v == mergeAllowed {
			t.Error("an unparseable payload must still be caught by the marker scan")
		}
	})

}
