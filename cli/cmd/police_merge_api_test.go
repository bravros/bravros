package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// seg tokenizes a command line the same way the hook does, so these tests
// exercise the real segment shape rather than a hand-built slice.
func seg(t *testing.T, cmd string) []string {
	t.Helper()
	segs := commandSegments(cmd)
	if len(segs) != 1 {
		t.Fatalf("want 1 segment from %q, got %d", cmd, len(segs))
	}
	return segs[0]
}

func TestGhFlagValue_AllFourShapes(t *testing.T) {
	cases := map[string]string{
		"gh pr merge 2044 --repo paylog/ev": "paylog/ev",
		"gh pr merge 2044 --repo=paylog/ev": "paylog/ev",
		"gh pr merge 2044 -R paylog/ev":     "paylog/ev",
		"gh pr merge 2044 -Rpaylog/ev":      "paylog/ev",
		"gh pr merge 2044 --merge":          "",
	}
	for cmd, want := range cases {
		if got := ghFlagValue(seg(t, cmd), "--repo", "-R"); got != want {
			t.Errorf("%s: got %q want %q", cmd, got, want)
		}
	}
}

func TestGhAPIMethod(t *testing.T) {
	cases := map[string]string{
		"gh api repos/o/r/pulls/1/merge":                   "GET",
		"gh api -X PUT repos/o/r/pulls/1/merge":            "PUT",
		"gh api --method PUT repos/o/r/pulls/1/merge":      "PUT",
		"gh api --method=put repos/o/r/pulls/1/merge":      "PUT",
		"gh api repos/o/r/merges -f base=main -f head=dev": "POST",
	}
	for cmd, want := range cases {
		if got := ghAPIMethod(seg(t, cmd)); got != want {
			t.Errorf("%s: got %q want %q", cmd, got, want)
		}
	}
}

func TestGhAPIEndpoint_SkipsFlagValues(t *testing.T) {
	cases := map[string]string{
		"gh api repos/o/r/merges":                               "repos/o/r/merges",
		"gh api -X PUT repos/o/r/pulls/1/merge":                 "repos/o/r/pulls/1/merge",
		"gh api -H 'Accept: application/json' repos/o/r/merges": "repos/o/r/merges",
		"gh api --method PUT -q .sha repos/o/r/merges":          "repos/o/r/merges",
	}
	for cmd, want := range cases {
		if got := ghAPIEndpoint(seg(t, cmd)); got != want {
			t.Errorf("%s: got %q want %q", cmd, got, want)
		}
	}
}

// TestAPIMergeVerdict_MergesRoute covers POST /repos/{o}/{r}/merges, whose base
// branch is inline in the command, so no forge lookup is involved.
func TestAPIMergeVerdict_MergesRoute(t *testing.T) {
	cases := map[string]mergeVerdict{
		// Resolved and protected.
		"gh api -X POST repos/paylog/ev/merges -f base=main -f head=homolog": mergeProtected,
		"gh api --method POST repos/paylog/ev/merges -f base=master":         mergeProtected,
		"gh api -X POST /repos/paylog/ev/merges -f base=main":                mergeProtected,
		"gh api -X POST repos/paylog/ev/merges --field base=main":            mergeProtected,
		// Unreadable body: the target cannot be seen, so it is not cleared.
		"gh api -X POST repos/paylog/ev/merges --input body.json": mergeIndeterminate,
		// Resolved and harmless.
		"gh api -X POST repos/paylog/ev/merges -f base=homolog -f head=fix/x": mergeAllowed,
		"gh api repos/paylog/ev/merges":                                       mergeAllowed,
		"gh api -X GET repos/paylog/ev/pulls/2044/merge":                      mergeAllowed,
		"gh api repos/paylog/ev/pulls":                                        mergeAllowed,
	}
	for cmd, want := range cases {
		got, _, _ := apiMergeVerdict(seg(t, cmd))
		if got != want {
			t.Errorf("%s: got %v want %v", cmd, got, want)
		}
	}
}

// TestEvaluateMergeGate_ApiRouteWired proves the gh api route reaches the gate
// at all — the pre-B-0036 gap was that it was never inspected.
func TestEvaluateMergeGate_ApiRouteWired(t *testing.T) {
	blocked := "gh api -X POST repos/paylog/ev/merges -f base=main -f head=homolog"
	if v, _ := evaluateMergeGate(blocked); v != mergeProtected {
		t.Errorf("gh api merge into main must be gated, got %v", v)
	}
	if v, _ := evaluateMergeGate("cd /tmp && " + blocked); v != mergeProtected {
		t.Error("must be gated inside a compound command segment too")
	}
	if v, _ := evaluateMergeGate("gh api repos/paylog/ev/pulls/2044"); v != mergeAllowed {
		t.Error("a plain read must not be gated")
	}
}

// TestEvaluateMergeGate_DefiniteBeatsIndeterminate pins the precedence rule: a
// resolved protected target anywhere in the line decides the whole command.
func TestEvaluateMergeGate_DefiniteBeatsIndeterminate(t *testing.T) {
	cmd := "gh api -X POST repos/o/r/merges --input body.json && git push origin main"
	v, _ := evaluateMergeGate(cmd)
	if v != mergeProtected {
		t.Errorf("want mergeProtected, got %v", v)
	}
}

// TestMergeBlockMessage_DistinctWording keeps the two block reasons apart: the
// operator's fix differs (retry vs. mint a token).
func TestMergeBlockMessage_DistinctWording(t *testing.T) {
	ind := mergeBlockMessage(mergeIndeterminate, "merging a pull request")
	if !strings.Contains(ind, "could not be determined") {
		t.Errorf("indeterminate message must say so: %q", ind)
	}
	prot := mergeBlockMessage(mergeProtected, "")
	if strings.Contains(prot, "could not be determined") {
		t.Errorf("protected message must not claim indeterminacy: %q", prot)
	}
	for _, m := range []string{ind, prot} {
		if !strings.Contains(m, "bravros police unlock") {
			t.Errorf("every block must name the sanctioned path: %q", m)
		}
	}
}

// TestPoliceDirectMainAllowed_ExplicitOptOutOnly pins the fail-safe direction:
// protection is lost only by an explicit, correctly-scoped opt-out.
func TestPoliceDirectMainAllowed_ExplicitOptOutOnly(t *testing.T) {
	write := func(t *testing.T, body string) {
		t.Helper()
		dir := t.TempDir()
		if body != "" {
			if err := os.MkdirAll(filepath.Join(dir, ".bravros"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, ".bravros", "config.json"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		t.Chdir(dir)
	}

	t.Run("no config means protected", func(t *testing.T) {
		write(t, "")
		if policeDirectMainAllowed("") {
			t.Error("a repo with no config must stay protected")
		}
	})

	t.Run("config without the key means protected", func(t *testing.T) {
		write(t, `{"staging_branch":"homolog"}`)
		if policeDirectMainAllowed("") {
			t.Error("absence of police.direct_main must not drop the guard")
		}
	})

	t.Run("explicit false means protected", func(t *testing.T) {
		write(t, `{"police":{"direct_main":false}}`)
		if policeDirectMainAllowed("") {
			t.Error("direct_main:false must stay protected")
		}
	})

	t.Run("explicit true opts the local repo out", func(t *testing.T) {
		write(t, `{"police":{"direct_main":true}}`)
		if !policeDirectMainAllowed("") {
			t.Error("direct_main:true must excuse a local-repo merge")
		}
	})

	t.Run("opt-out never reaches another repo", func(t *testing.T) {
		write(t, `{"police":{"direct_main":true}}`)
		if policeDirectMainAllowed("paylog/ev") {
			t.Error("local opt-out must not excuse a merge aimed at another repo")
		}
	})
}

// gitRepo builds a throwaway repo with an origin remote and optional bravros
// config, and chdirs into it for the duration of the test.
func gitRepo(t *testing.T, originURL, cfg string) {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "."},
		{"remote", "add", "origin", originURL},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if cfg != "" {
		if err := os.MkdirAll(filepath.Join(dir, ".bravros"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".bravros", "config.json"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
}

// TestGraphqlMergeVerdict covers the GraphQL merge surface (PR 90 blocking 1):
// mergePullRequest/mergeBranch mutations merge server-side with no REST call
// and no git push, so nothing else can see them.
func TestGraphqlMergeVerdict(t *testing.T) {
	cases := map[string]mergeVerdict{
		`gh api graphql -f query=mutation{mergePullRequest(input:{pullRequestId:"PR_kwDO"}){pullRequest{merged}}}`: mergeIndeterminate,
		`gh api graphql -f query=mutation{mergeBranch(input:{base:"main"}){mergeCommit{oid}}}`:                     mergeIndeterminate,
		// Unreadable body must fail closed, same rule as the REST /merges route.
		`gh api graphql --input mutation.json`: mergeIndeterminate,
		// gh's magic values: `@file` reads the query from a file, `-` from
		// stdin. The literal token holds no mutation name, so the substring
		// check cleared a real merge (PR 90 round 2, blocking 1).
		`gh api graphql -F query=@mutation.graphql`:      mergeIndeterminate,
		`gh api graphql -f query=@mutation.graphql`:      mergeIndeterminate,
		`gh api graphql --field query=@mutation.graphql`: mergeIndeterminate,
		`gh api graphql --raw-field query=@merge.gql`:    mergeIndeterminate,
		`gh api graphql -F query=-`:                      mergeIndeterminate,
		`gh api graphql -F query=@-`:                     mergeIndeterminate,
		// Read-only GraphQL must stay allowed — over-blocking these would be costly.
		`gh api graphql -f query=query{viewer{login}}`:                                      mergeAllowed,
		`gh api graphql -f query=query{repository(name:"ev"){pullRequests{nodes{number}}}}`: mergeAllowed,
		`gh api graphql`: mergeAllowed,
	}
	for cmd, want := range cases {
		got, _, _ := apiMergeVerdict(seg(t, cmd))
		if got != want {
			t.Errorf("%s\n  got %v want %v", cmd, got, want)
		}
	}
}

// TestPushTargetRepo_ResolvesDestination pins that a push naming another repo
// resolves to that repo, while an ordinary push to origin resolves to "".
func TestPushTargetRepo_ResolvesDestination(t *testing.T) {
	gitRepo(t, "git@github.com:skaisser/scratch.git", "")

	cases := map[string]string{
		"git push origin main": "",
		// `git push main` names a REPOSITORY called "main", not a refspec —
		// real git answers `fatal: 'main' does not appear to be a git
		// repository` (2.50.1). It is not the session's repo, so it must not
		// resolve to "" and hand itself to the opt-out.
		"git push main": "main",
		"git push":      "",
		"git push git@github.com:skaisser/scratch.git b:main":     "", // same repo by URL
		"git push https://github.com/skaisser/scratch.git b:main": "",
		"git push git@github.com:other-org/protected.git b:main":  "other-org/protected",
		"git push https://github.com/other-org/protected b:main":  "other-org/protected",
		// A filesystem path is a destination too — the third addressing shape,
		// after the URL and the named remote (PR 90 round 12). Verified against
		// git 2.50.1: `git send-pack ../o.git HEAD:refs/heads/x` pushed.
		"git push /var/repos/mirror.git main":     "/var/repos/mirror.git",
		"git push ../sibling.git HEAD:main":       "../sibling.git",
		"git push /tmp/other-repo-elsewhere main": "/tmp/other-repo-elsewhere",
	}
	for cmd, want := range cases {
		if got := pushTargetRepo(seg(t, cmd)); got != want {
			t.Errorf("%s: got %q want %q", cmd, got, want)
		}
	}
}

// TestOptOutNeverExcusesPushToAnotherRepo is the regression test for PR 90
// blocking 2: the git push arm never populated a target repo, so an empty
// target read as "local repo" and police.direct_main waved through a push to a
// DIFFERENT repo's main — the "unlock main everywhere" case the design forbids.
func TestOptOutNeverExcusesPushToAnotherRepo(t *testing.T) {
	gitRepo(t, "git@github.com:skaisser/scratch.git", `{"police":{"direct_main":true}}`)

	// The opt-out still works for the local repo.
	if v, _ := evaluateMergeGate("git push origin main"); v != mergeAllowed {
		t.Errorf("direct_main must excuse a local main push, got %v", v)
	}

	// It must NOT reach another repo, by URL or by refspec form.
	for _, cmd := range []string{
		"git push git@github.com:other-org/protected.git some-branch:main",
		"git push https://github.com/other-org/protected.git main",
		"git push git@github.com:other-org/protected.git +main:main",
	} {
		if v, _ := evaluateMergeGate(cmd); v != mergeProtected {
			t.Errorf("opt-out must not excuse a push to another repo: %s (got %v)", cmd, v)
		}
	}
}

// TestApiMergeBaseMagicValue pins the same magic-value rule on the REST
// /merges route: a base read from a file or stdin hides the target branch just
// as --input does, so it must fail closed rather than miss protectedBranches.
func TestApiMergeBaseMagicValue(t *testing.T) {
	cases := map[string]mergeVerdict{
		"gh api -X POST repos/o/r/merges -f base=@body.json": mergeIndeterminate,
		"gh api -X POST repos/o/r/merges -f base=-":          mergeIndeterminate,
		"gh api -X POST repos/o/r/merges -f base=main":       mergeProtected,
		"gh api -X POST repos/o/r/merges -f base=feature":    mergeAllowed,
	}
	for cmd, want := range cases {
		got, _, _ := apiMergeVerdict(seg(t, cmd))
		if got != want {
			t.Errorf("%s\n  got %v want %v", cmd, got, want)
		}
	}
}

// TestEnvPrefixSurvivesIntoAPIArms pins the same strip for the `gh api` arms.
// No direct_main opt-out here: a GraphQL merge resolves no target repo, so the
// opt-out would legitimately excuse it and hide whether the strip ran.
func TestEnvPrefixSurvivesIntoAPIArms(t *testing.T) {
	gitRepo(t, "git@github.com:skaisser/scratch.git", "")

	cases := map[string]mergeVerdict{
		`FOO=1 gh api graphql -F query=@mutation.graphql`:                               mergeIndeterminate,
		`GH_TOKEN=x gh api graphql -f query=mutation{mergeBranch(input:{base:"main"})}`: mergeIndeterminate,
		"FOO=1 gh api -X POST repos/o/r/merges -f base=main":                            mergeProtected,
		"FOO=1 gh api -X POST repos/o/r/merges -f base=@body.json":                      mergeIndeterminate,
		"FOO=1 gh api repos/o/r/pulls/1/merge":                                          mergeAllowed,
	}
	for cmd, want := range cases {
		if v, _ := evaluateMergeGate(cmd); v != want {
			t.Errorf("%s\n  got %v want %v", cmd, v, want)
		}
	}
}

// TestRepeatedFlagLastWins is the regression test for PR 90 round 3: both
// scanners returned on the FIRST occurrence of a flag or field, while gh (via
// pflag, whose Set overwrites) acts on the LAST. A repeated flag therefore
// showed the gate a benign value while the real request merged.
//
// Verified against gh on 2026-09-09: `gh api -X GET -X POST repos/bravros/private`
// hit the write route (HTTP 400 "Body should be a JSON object" from
// update-a-repository), and `gh pr view 90 -R bravros/bravros -R bravros/private`
// resolved the LAST repo. The -f/-F field cases are defence in depth only — gh
// rejects a repeated field key ("unexpected override existing field"), so those
// commands never reach the forge.
func TestRepeatedFlagLastWins(t *testing.T) {
	t.Run("ghFlagValue", func(t *testing.T) {
		cases := []struct {
			cmd, want string
		}{
			{"gh api -X GET -X POST repos/o/r/pulls/1/merge", "POST"},
			{"gh api -X POST -X GET repos/o/r/pulls/1/merge", "GET"},
			{"gh api --method GET --method PUT repos/o/r/pulls/1/merge", "PUT"},
			{"gh api -X GET --method=PUT repos/o/r/pulls/1/merge", "PUT"},
			{"gh api -X PUT repos/o/r/pulls/1/merge", "PUT"},
		}
		for _, c := range cases {
			if got := ghFlagValue(seg(t, c.cmd), "--method", "-X"); got != c.want {
				t.Errorf("%s: got %q want %q", c.cmd, got, c.want)
			}
		}

		repoCases := []struct {
			cmd, want string
		}{
			{"gh pr merge 2044 -R harmless/repo -R paylog/ev --merge", "paylog/ev"},
			{"gh pr merge 2044 --repo harmless/repo --repo=paylog/ev", "paylog/ev"},
			{"gh pr merge 2044 -Rharmless/repo -Rpaylog/ev", "paylog/ev"},
			{"gh pr merge 2044 -R paylog/ev", "paylog/ev"},
		}
		for _, c := range repoCases {
			if got := ghFlagValue(seg(t, c.cmd), "--repo", "-R"); got != c.want {
				t.Errorf("%s: got %q want %q", c.cmd, got, c.want)
			}
		}
	})

	t.Run("apiFieldValue", func(t *testing.T) {
		cases := []struct {
			cmd, key, want string
		}{
			{"gh api -X POST repos/o/r/merges -f base=feature -f base=main", "base", "main"},
			{"gh api -X POST repos/o/r/merges -f base=main -f base=feature", "base", "feature"},
			{"gh api -X POST repos/o/r/merges -f base=feature -F base=main", "base", "main"},
			// A different key in between must not clear the recorded value.
			{"gh api -X POST repos/o/r/merges -f base=main -f head=topic", "base", "main"},
		}
		for _, c := range cases {
			if got := apiFieldValue(seg(t, c.cmd), c.key); got != c.want {
				t.Errorf("%s [%s]: got %q want %q", c.cmd, c.key, got, c.want)
			}
		}
	})

	// End to end: the reviewer's three reproductions must all be gated.
	t.Run("verdicts", func(t *testing.T) {
		gitRepo(t, "git@github.com:skaisser/scratch.git", "")

		if v, _, _ := apiMergeVerdict(seg(t, "gh api -X GET -X POST repos/paylog/ev/pulls/2044/merge")); v == mergeAllowed {
			t.Error("repeated -X still lets a PR merge through as GET")
		}
		// gh would reject this command; the gate fails closed on it regardless.
		if v, _, _ := apiMergeVerdict(seg(t, "gh api -X POST repos/o/r/merges -f base=feature -f base=main")); v != mergeProtected {
			t.Errorf("repeated -f base: got %v want mergeProtected", v)
		}
		// A genuine read-only GET must still be allowed — no over-blocking.
		if v, _, _ := apiMergeVerdict(seg(t, "gh api repos/o/r/pulls/1/merge")); v != mergeAllowed {
			t.Errorf("read-only GET must stay allowed, got %v", v)
		}
	})
}

// TestGraphqlAutoMergeMutation covers enablePullRequestAutoMerge (PR 90 round 3,
// minor): it lands the PR on its base as soon as requirements are met, which
// with no branch protection is immediately.
func TestGraphqlAutoMergeMutation(t *testing.T) {
	cases := map[string]mergeVerdict{
		`gh api graphql -f query=mutation{enablePullRequestAutoMerge(input:{pullRequestId:"PR_kwDO"}){clientMutationId}}`: mergeIndeterminate,
		`gh api graphql -f query=mutation{enqueuePullRequest(input:{pullRequestId:"PR_kwDO"}){clientMutationId}}`:         mergeIndeterminate,
		// Reading auto-merge state is not arming it.
		`gh api graphql -f query=query{repository{pullRequest{autoMergeRequest{enabledAt}}}}`: mergeAllowed,
	}
	for cmd, want := range cases {
		got, _, _ := apiMergeVerdict(seg(t, cmd))
		if got != want {
			t.Errorf("%s\n  got %v want %v", cmd, got, want)
		}
	}
}

// TestGitFlagValuesNeverBecomeThePushTarget is the regression test for PR 90
// round 4: pushTargetRepo had no model of which `git push` flags consume the
// following token, so `git push -o ci.skip <url> main` picked "ci.skip" as the
// destination. That resolved to no known remote, fell back to "" ("the local
// repo"), and a local direct_main opt-out excused a push to ANOTHER repo's main.
func TestGitFlagValuesNeverBecomeThePushTarget(t *testing.T) {
	gitRepo(t, "git@github.com:skaisser/scratch.git", `{"police":{"direct_main":true}}`)

	cases := map[string]string{
		"git push -o ci.skip git@github.com:other-org/protected.git main":                "other-org/protected",
		"git push --push-option ci.skip https://github.com/other-org/protected main":     "other-org/protected",
		"git push --receive-pack /usr/bin/x git@github.com:other-org/protected.git main": "other-org/protected",
		"git push --exec /usr/bin/x git@github.com:other-org/protected.git main":         "other-org/protected",
		// Attached form carries its own value and eats no token.
		"git push -o=ci.skip git@github.com:other-org/protected.git main":            "other-org/protected",
		"git push --push-option=ci.skip git@github.com:other-org/protected.git main": "other-org/protected",
		// Ordinary pushes are unchanged.
		"git push origin main": "",
		"git push":             "",
	}
	for cmd, want := range cases {
		if got := pushTargetRepo(seg(t, cmd)); got != want {
			t.Errorf("%s: got %q want %q", cmd, got, want)
		}
	}

	// End to end: the opt-out must not excuse any of them.
	for _, cmd := range []string{
		"git push -o ci.skip git@github.com:other-org/protected.git main",
		"git push --push-option ci.skip https://github.com/other-org/protected main",
	} {
		if v, _ := evaluateMergeGate(cmd); v != mergeProtected {
			t.Errorf("opt-out must not excuse %s (got %v)", cmd, v)
		}
	}
}

// TestGitGlobalOptionsStillAnchorOnSubcommand covers three bypasses found while
// fixing round 4 that the review did not name, and which needed no opt-out at
// all: a git global option between "git" and "push" defeated startsWith, so the
// command fell out of the merge gate entirely.
func TestGitGlobalOptionsStillAnchorOnSubcommand(t *testing.T) {
	gitRepo(t, "git@github.com:skaisser/scratch.git", "")

	for _, cmd := range []string{
		"git -c push.default=simple push origin main",
		"git -C /tmp push origin main",
		"git --git-dir=/tmp/x/.git push origin main",
		"git --no-pager push origin main",
		"git -c a=b -C /tmp --no-pager push origin main",
	} {
		if v, _ := evaluateMergeGate(cmd); v != mergeProtected {
			t.Errorf("global options must not defeat the gate: %s (got %v)", cmd, v)
		}
	}

	// `git -c push.default simple push …` is deliberately absent: -c consumes
	// the next token as its value, so git reads "simple" as the subcommand and
	// refuses to run ("'simple' is not a git command", verified 2026-09-09).
	// The strip models that identically, leaving nothing for the gate to catch.

	// A non-protected push stays allowed — the strip must not over-block.
	if v, _ := evaluateMergeGate("git -c a=b push origin feature"); v != mergeAllowed {
		t.Errorf("git -c push to a feature branch must stay allowed, got %v", v)
	}
}

// TestRelocatedPushNeverExcusedByLocalOptOut pins the second half of the same
// gap: -C/--git-dir push into a tree whose config and origin this process never
// read, so an empty target cannot be trusted to mean "the local repo".
func TestRelocatedPushNeverExcusedByLocalOptOut(t *testing.T) {
	gitRepo(t, "git@github.com:skaisser/scratch.git", `{"police":{"direct_main":true}}`)

	// The opt-out still works for an ordinary local push.
	if v, _ := evaluateMergeGate("git push origin main"); v != mergeAllowed {
		t.Errorf("direct_main must excuse a local main push, got %v", v)
	}
	for _, cmd := range []string{
		"git -C /tmp/other push origin main",
		"git --git-dir=/tmp/other/.git push origin main",
	} {
		if v, _ := evaluateMergeGate(cmd); v != mergeProtected {
			t.Errorf("opt-out must not reach a relocated tree: %s (got %v)", cmd, v)
		}
	}
}

// twoRepos builds a session repo (on a non-protected branch) and a second,
// DIFFERENT repo checked out on main, and chdirs into the session. Returns both
// paths. The branch asymmetry is the point: a gate that answers from its own
// checkout instead of the command's reads "feature/x" and clears a push that
// actually lands on the other repo's main.
// installPrePush gives dir a bravros-managed pre-push hook, the layer the gate
// defers to for `git push`. Without one the gate cannot defer, so tests that
// exercise deferral must install it and tests that exercise the no-hook case
// must not.
func installPrePush(t *testing.T, dir string) {
	t.Helper()
	hooks := filepath.Join(dir, ".bravros", "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "#!/bin/sh\n# bravros-managed-pre-push-hook v1\nexit 0\n"
	if err := os.WriteFile(filepath.Join(hooks, "pre-push"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	c := exec.Command("git", "config", "core.hooksPath", ".bravros/hooks")
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git config core.hooksPath: %v\n%s", err, out)
	}
}

// twoReposOn is twoRepos with the branches named explicitly.
func twoReposOn(t *testing.T, sessionBranch, otherBranch string) (session, other string) {
	t.Helper()
	mk := func(origin, branch string) string {
		dir := t.TempDir()
		for _, args := range [][]string{
			{"init", "-q", "-b", branch, "."},
			{"remote", "add", "origin", origin},
			{"commit", "-q", "--allow-empty", "-m", "seed"},
		} {
			c := exec.Command("git", args...)
			c.Dir = dir
			c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
				"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
			if out, err := c.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		return dir
	}
	session = mk("git@github.com:skaisser/scratch.git", sessionBranch)
	other = mk("git@github.com:other-org/protected.git", otherBranch)
	t.Chdir(session)
	return session, other
}

func twoRepos(t *testing.T, cfg string) (session, other string) {
	t.Helper()
	mk := func(origin, branch string) string {
		dir := t.TempDir()
		for _, args := range [][]string{
			{"init", "-q", "-b", branch, "."},
			{"remote", "add", "origin", origin},
			{"commit", "-q", "--allow-empty", "-m", "seed"},
		} {
			c := exec.Command("git", args...)
			c.Dir = dir
			c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
				"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
			if out, err := c.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		return dir
	}
	session = mk("git@github.com:skaisser/scratch.git", "feature/x")
	other = mk("git@github.com:other-org/protected.git", "main")
	if err := os.MkdirAll(filepath.Join(session, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if cfg != "" {
		if err := os.MkdirAll(filepath.Join(session, ".bravros"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(session, ".bravros", "config.json"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(session)
	return session, other
}

// TestCommandPrefixesNeverEscapeTheGate covers twelve bypasses found while
// fixing round 6 that the review did not name. The gate anchored on the FIRST
// token of a segment, so any prefix at all — a shell builtin, a wrapper
// command, or the `then`/`do` body of a conditional — sailed straight past it
// with no opt-out involved. Measured allowed on 2026-09-09, before the fix.
func TestCommandPrefixesNeverEscapeTheGate(t *testing.T) {
	gitRepo(t, "git@github.com:skaisser/scratch.git", "")

	for _, cmd := range []string{
		"{ git push origin main; }",
		"! git push origin main",
		"time git push origin main",
		"nohup git push origin main",
		"command git push origin main",
		"exec git push origin main",
		"eval git push origin main",
		"sudo git push origin main",
		"if true; then git push origin main; fi",
		"for i in 1; do git push origin main; done",
		"while true; do git push origin main; done",
		"sudo git -c a=b push origin main",
		"{ gh pr merge 5 --merge; }",
		"sudo gh api -X PUT repos/o/r/pulls/1/merge",
	} {
		if v, _ := evaluateMergeGate(cmd); v == mergeAllowed {
			t.Errorf("prefix must not escape the gate: %s", cmd)
		}
	}
}

// TestAnchorAnywhereDoesNotOverBlock pins the cost side of that trade: scanning
// the whole segment for the command word must not turn ordinary commands, or a
// merge command quoted inside another one, into blocks.
func TestAnchorAnywhereDoesNotOverBlock(t *testing.T) {
	gitRepo(t, "git@github.com:skaisser/scratch.git", "")

	for _, cmd := range []string{
		"npm test",
		"echo 'gh pr merge'",
		`git commit -m "fix bug; git push origin main"`,
		"git push origin feature",
		"git status",
		"gh pr list",
		"git log --oneline origin/main",
	} {
		if v, _ := evaluateMergeGate(cmd); v != mergeAllowed {
			t.Errorf("must stay allowed: %s (got %v)", cmd, v)
		}
	}
}

// TestQuotedShellPayloadIsNotInert is the regression test for PR 90 round 10.
// commandSegments splits on unquoted separators only, so a quoted payload
// arrives as ONE token: `bash -c "gh pr merge 2044 --merge"` tokenized to
// ["bash","-c","gh pr merge 2044 --merge"], where no token equals "gh" and
// every scan walked past it.
//
// This was the worst shape in the thread: the gh routes have no hook behind
// them, so it was unconditional there — and with --no-verify inside the quotes
// it defeated the pre-push hook too, since the nested shell really does pass
// the flag to git.
func TestQuotedShellPayloadIsNotInert(t *testing.T) {
	session, _ := twoReposOn(t, "main", "feature/y")
	installPrePush(t, session)

	for _, cmd := range []string{
		`bash -c "gh pr merge 2044 -R paylog/ev --merge"`,
		`sh -c 'gh api -X PUT repos/paylog/ev/pulls/2044/merge'`,
		`zsh -c "gh api -X POST repos/o/r/merges -f base=main"`,
		`eval "gh pr merge 5 --merge"`,
		`bash -c "git push origin main"`,
		`bash -c "git push --no-verify origin main"`,
		`bash -lc "git push --no-verify"`,
		`sudo bash -c "gh api -X PUT repos/o/r/pulls/1/merge"`,
		// The inner scans reach inside too, not just the merge arms.
		`bash -c "git push origin main"`,
		`bash -c "git send-pack ../bare.git --all"`,
		// Nested one level deeper.
		`bash -c "bash -c \"gh pr merge 5 --merge\""`,
		// Another language's string literal: not parsed, but seen.
		`node -e "require('child_process').execSync('gh pr merge 5 --merge')"`,
		`python3 -c "import os; os.system('gh api -X PUT repos/o/r/pulls/1/merge')"`,
	} {
		if v, _ := evaluateMergeGate(cmd); v == mergeAllowed {
			t.Errorf("a payload the shell would execute must not be inert: %s", cmd)
		}
	}
}

// TestCodePayloadScanSparesData pins the distinction the fix rests on: an
// argument a command EXECUTES is re-tokenized, an argument it merely carries is
// not. `-m` is data and must stay untouched, or every commit message quoting a
// command becomes a block.
func TestCodePayloadScanSparesData(t *testing.T) {
	session, _ := twoReposOn(t, "feature/x", "feature/y")
	installPrePush(t, session)

	for _, cmd := range []string{
		`git commit -m "fix bug; git push origin main"`,
		`git commit -m "wip"`,
		`bash -c "npm test"`,
		`bash -c "ls -la && echo done"`,
		`echo "gh pr merge"`,
		"git push",
		"npm test && git push",
		"gh pr list",
		"git push origin feature/x",
	} {
		if v, _ := evaluateMergeGate(cmd); v != mergeAllowed {
			t.Errorf("must stay allowed: %s (got %v)", cmd, v)
		}
	}
}

// TestPlumbingPushIsNeverDeferred is the regression test for PR 90 round 11.
// `git send-pack` does the same work `git push` wraps, but the pre-push hook is
// invoked by the porcelain, so send-pack never runs it.
//
// Verified against git 2.50.1 on 2026-09-10 with a bare remote and a blocking
// hook installed: `git push origin HEAD:main` printed the hook's output and
// failed, while `git send-pack ../o.git HEAD:refs/heads/main` pushed the same
// ref with the hook silent. It defeats the string gate AND the backstop the
// rounds 7-10 redesign rests on, so it can never be deferred.
func TestPlumbingPushIsNeverDeferred(t *testing.T) {
	session, _ := twoReposOn(t, "main", "feature/y")
	installPrePush(t, session)

	for _, cmd := range []string{
		"git send-pack origin HEAD:main",
		"git send-pack ../other.git HEAD:refs/heads/main",
		"git send-pack origin", // no refspec: nothing to read, and no hook coming
		"git http-push origin main",
		"sudo git send-pack origin HEAD:main",
		`bash -c "git send-pack origin HEAD:main"`,
	} {
		if v, _ := evaluateMergeGate(cmd); v == mergeAllowed {
			t.Errorf("a plumbing push has no hook behind it: %s", cmd)
		}
	}

	// An explicit non-protected refspec is a pure string fact and stays allowed
	// — no hook is needed to read it.
	if v, _ := evaluateMergeGate("git send-pack origin HEAD:feature/x"); v != mergeAllowed {
		t.Errorf("send-pack to a feature branch must stay allowed, got %v", v)
	}
}

// TestRawHTTPMergeRouteIsGated covers the other half of round 11: gh is a
// convenience, not a gatekeeper. The same PUT /pulls/{n}/merge and the same
// GraphQL mutation are one curl away, carrying neither a "git" nor a "gh"
// token, so every command scan in the gate walked past them — and no hook
// exists for these routes either, which is B-0036's whole premise.
//
// Matched on the ROUTE rather than the client, so an HTTP library is covered as
// readily as curl.
func TestRawHTTPMergeRouteIsGated(t *testing.T) {
	gitRepo(t, "git@github.com:skaisser/scratch.git", "")

	for _, cmd := range []string{
		`curl -X PUT -H "Authorization: Bearer x" https://api.github.com/repos/paylog/ev/pulls/2044/merge`,
		`curl -X POST https://api.github.com/repos/o/r/merges -d '{"base":"main"}'`,
		`curl -X POST https://api.github.com/graphql -d '{"query":"mutation{mergePullRequest(input:{})}"}'`,
		`wget --method=PUT https://api.github.com/repos/o/r/pulls/1/merge`,
		// GitHub Enterprise: the host differs, the route does not.
		`curl -X PUT https://github.company.com/api/v3/repos/o/r/pulls/1/merge`,
		// Reached from inside a shell payload, or an HTTP library.
		`bash -c "curl -X PUT https://api.github.com/repos/o/r/pulls/1/merge"`,
		`python3 -c "import requests; requests.put('https://api.github.com/repos/o/r/pulls/1/merge')"`,
	} {
		if v, _ := evaluateMergeGate(cmd); v == mergeAllowed {
			t.Errorf("a merge route over HTTP must not be cleared: %s", cmd)
		}
	}

	// Read-only traffic to the same host is untouched — only a merge path, or
	// graphql alongside a merge mutation, matches.
	for _, cmd := range []string{
		`curl https://api.github.com/repos/o/r/pulls/1`,
		`curl -s https://api.github.com/user`,
		`curl -X POST https://api.github.com/graphql -d '{"query":"query{viewer{login}}"}'`,
		`curl https://example.com/merge`,
	} {
		if v, _ := evaluateMergeGate(cmd); v != mergeAllowed {
			t.Errorf("read-only HTTP must stay allowed: %s (got %v)", cmd, v)
		}
	}
}

// TestCodePayloadSeesListCallForms closes the round-11 minor: the marker scan
// required the words contiguous, so a call built as a list rather than a
// command string spelled nothing. Punctuation is now flattened first.
func TestCodePayloadSeesListCallForms(t *testing.T) {
	gitRepo(t, "git@github.com:skaisser/scratch.git", "")

	for _, cmd := range []string{
		`python3 -c "import subprocess; subprocess.run(['gh','api','-X','PUT','repos/o/r/pulls/1/merge'])"`,
		`python3 -c "import subprocess; subprocess.run(['git','push','origin','main'])"`,
		`node -e "spawnSync('gh', ['pr','merge','5','--merge'])"`,
	} {
		if v, _ := evaluateMergeGate(cmd); v == mergeAllowed {
			t.Errorf("a list-form call must not be inert: %s", cmd)
		}
	}

	if v, _ := evaluateMergeGate(`python3 -c "import os; print(os.getcwd())"`); v != mergeAllowed {
		t.Errorf("ordinary inline code must stay allowed, got %v", v)
	}
}

// TestPathDestinationIsNeverExcused is the regression test for PR 90 round 12.
// pushTargetRepo assumed a destination that is neither a URL nor a known remote
// must be a refspec — but git's grammar is `git push [<repository>]
// [<refspec>...]`, so the FIRST positional is always the repository, and a
// plain filesystem path is an ordinary way to name one.
//
// The token therefore collapsed to "", which policeDirectMainAllowed reads as
// "the session's own repo", and a direct_main opt-out excused a push landing in
// a different repository on disk.
func TestPathDestinationIsNeverExcused(t *testing.T) {
	session, other := twoReposOn(t, "feature/x", "feature/y")
	installPrePush(t, session)
	if err := os.MkdirAll(filepath.Join(session, ".bravros"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(session, ".bravros", "config.json"),
		[]byte(`{"police":{"direct_main":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, cmd := range []string{
		"git push " + other + " main",
		"git push ../sibling.git HEAD:main",
		"git push /var/repos/mirror.git main",
		"git send-pack ../sibling.git HEAD:refs/heads/main",
		"git push /tmp/other-repo-elsewhere +main:main",
	} {
		if v, _ := evaluateMergeGate(cmd); v == mergeAllowed {
			t.Errorf("a path destination must not be excused by the opt-out: %s", cmd)
		}
	}

	// The opt-out still works where it plainly applies.
	if v, _ := evaluateMergeGate("git push origin main"); v != mergeAllowed {
		t.Errorf("direct_main must still excuse a push to the session's own origin, got %v", v)
	}
}
