package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPIRequestBodyAndOptionContract(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    mergeVerdict
	}{
		{`gh api graphql --input=body.json`, mergeIndeterminate},
		{`gh api graphql --input=-`, mergeIndeterminate},
		{`gh api graphql --input=body.json -f query='query { viewer { login } }'`, mergeIndeterminate},
		{`gh api repos/o/r/merges --input=body.json -f base=homolog`, mergeIndeterminate},
		{`gh api repos/o/r/merges --input body.json -f base=homolog`, mergeIndeterminate},
		{`gh api repos/o/r/merges -f base=main -q '-XGET'`, mergeProtected},
		{`gh api repos/o/r/merges -f base=main --template '--method=GET'`, mergeProtected},
		{`gh api repos/o/r/merges -f base=main -q '-fbase=homolog'`, mergeProtected},
		{`gh api graphql -f query='mutation { mergePullRequest(input: {}) { clientMutationId } }' -q '-XGET'`, mergeIndeterminate},
		{`gh api graphql --input=body.json -X GET`, mergeAllowed},
		{`gh api repos/o/r/merges -q '--input=body.json'`, mergeAllowed},
		{`gh api repos/o/r/merges -X GET -q '-XPOST'`, mergeAllowed},
		{`gh api repos/o/r/merges -f base=homolog`, mergeAllowed},
		{`gh api graphql -f query='query { viewer { login } }'`, mergeAllowed},
	} {
		t.Run(tc.command, func(t *testing.T) {
			got, _, _ := apiMergeVerdict(seg(t, tc.command))
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGHFlagValuesAreNotOptions(t *testing.T) {
	for command, want := range map[string]string{
		`gh pr merge 90 -R other/repo --body '-Rskaisser/scratch'`:        "other/repo",
		`gh pr merge 90 -Rother/repo --subject '--repo=skaisser/scratch'`: "other/repo",
		`gh pr merge 90 -R first/repo -Rsecond/repo`:                      "second/repo",
	} {
		if got := ghFlagValue(seg(t, command), "-R", "--repo"); got != want {
			t.Errorf("%s: got %q want %q", command, got, want)
		}
	}
}

// A child process records PR lookup argv without making a forge request.
func recordingPRLookup(t *testing.T, base string) string {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(bin, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$POLICE_LOOKUP_LOG\"\nprintf '%s\\n' \"$POLICE_LOOKUP_BASE\"\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("POLICE_LOOKUP_LOG", log)
	t.Setenv("POLICE_LOOKUP_BASE", base)
	return log
}

func TestAPIExplicitHostReachesPRLookup(t *testing.T) {
	log := recordingPRLookup(t, "main")
	for _, tc := range []struct{ command, repo string }{
		{`gh api --hostname github.example -X PUT repos/o/r/pulls/9/merge`, "github.example/o/r"},
		{`gh api -X PUT https://github.example/api/v3/repos/o/r/pulls/9/merge`, "github.example/o/r"},
		{`gh api -X PUT https://api.github.com/repos/o/r/pulls/9/merge`, "github.com/o/r"},
	} {
		got, _, target := apiMergeVerdict(seg(t, tc.command))
		if got != mergeProtected || target != tc.repo {
			t.Fatalf("%s: got %v target %q", tc.command, got, target)
		}
		args, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(args), "--repo\n"+tc.repo+"\n9\n") {
			t.Fatalf("wrong lookup: %s", args)
		}
	}
}

func TestPRURLDoesNotBorrowLocalOptOut(t *testing.T) {
	gitRepo(t, "git@github.com:skaisser/scratch.git", `{"police":{"direct_main":true}}`)
	recordingPRLookup(t, "main")
	for _, command := range []string{
		`gh pr merge https://github.com/other/repo/pull/9 --merge`,
		`gh pr merge https://github.example/skaisser/scratch/pull/9 --merge`,
		`gh api --hostname github.example -X POST repos/skaisser/scratch/merges -f base=main`,
	} {
		if got, _ := evaluateMergeGate(command); got == mergeAllowed {
			t.Fatalf("foreign target excused: %s", command)
		}
	}
}

func TestHostQualifiedLocalOptOutStillWorks(t *testing.T) {
	gitRepo(t, "git@github.com:skaisser/scratch.git", `{"police":{"direct_main":true}}`)
	recordingPRLookup(t, "main")
	for _, command := range []string{
		`gh pr merge https://github.com/skaisser/scratch/pull/9 --merge`,
		`gh api --hostname github.com -X POST repos/skaisser/scratch/merges -f base=main`,
	} {
		if got, _ := evaluateMergeGate(command); got != mergeAllowed {
			t.Fatalf("local target blocked: %s -> %v", command, got)
		}
	}
}

func TestGHContextAndHTTPFallbackStayScoped(t *testing.T) {
	gitRepo(t, "git@github.com:skaisser/scratch.git", `{"police":{"direct_main":true}}`)
	recordingPRLookup(t, "homolog")
	for _, command := range []string{
		`GH_HOST=github.example gh pr merge 9 --merge`,
		`GH_REPO=other/repo gh pr merge 9 --merge`,
		`export GH_REPO=other/repo; gh pr merge 9 --merge`,
		`GH_HOST=github.example gh api -X PUT repos/o/r/pulls/9/merge`,
		`GH_REPO=other/repo gh api -X PUT repos/{owner}/{repo}/pulls/9/merge`,
		`gh api -X POST https://api.github.com/repos/o/r/merges -f base=homolog; curl -X PUT https://api.github.com/repos/o/r/pulls/9/merge`,
	} {
		if got, _ := evaluateMergeGate(command); got == mergeAllowed {
			t.Errorf("unsafe context cleared: %s", command)
		}
	}
	for _, command := range []string{
		`GH_HOST=github.example gh api repos/o/r/pulls/9/merge`,
		`GH_REPO=other/repo gh api repos/{owner}/{repo}/pulls/9/merge`,
		`cd sub && gh api repos/{owner}/{repo}/pulls/9/merge`,
		`GH_HOST=github.example gh api graphql -f query='query { viewer { login } }'`,
		`gh api -X POST https://api.github.com/repos/o/r/merges -f base=homolog`,
		`bash -c 'gh api -X POST https://api.github.com/repos/o/r/merges -f base=homolog'`,
		`curl https://api.github.com/repos/o/r/pulls/9/merge; curl -X POST https://example.test/telemetry`,
	} {
		if got, _ := evaluateMergeGate(command); got != mergeAllowed {
			t.Errorf("safe operation blocked %v: %s", got, command)
		}
	}
}
