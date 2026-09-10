package cmd

import (
	"os/exec"
	"testing"
)

func pushContractGit(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func TestPushContractRefSets(t *testing.T) {
	gitRepo(t, "git@github.com:owner/repo.git", "")
	pushContractGit(t, "symbolic-ref", "HEAD", "refs/heads/feature/work")
	for _, command := range []string{
		"git push --all", "git push --mirror", "git push --branches", "git push origin :",
		"git push origin 'refs/heads/*:refs/heads/*'", "git push origin 'HEAD:refs/heads/m*'",
		"git push --repo origin HEAD:main", "git push --repo=origin HEAD:main", "git push origin -- :main",
	} {
		t.Run(command, func(t *testing.T) {
			if v, _ := evaluateMergeGate(command); v == mergeAllowed {
				t.Fatal("allowed protected push")
			}
		})
	}
	for _, command := range []string{"git push origin feature/work", "git push origin homolog", "git push origin main:homolog", "git push --tags", "git push --dry-run origin main", "git push -n --all", "git push origin refs/tags/main"} {
		t.Run(command, func(t *testing.T) {
			if v, d := evaluateMergeGate(command); v != mergeAllowed {
				t.Fatalf("blocked ordinary push: %v %s", v, d)
			}
		})
	}
}

func TestPushContractConfiguration(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"remote.origin.push", "HEAD:refs/heads/main"}, {"remote.origin.push", ":"},
		{"remote.origin.mirror", "true"}, {"push.default", "matching"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			gitRepo(t, "git@github.com:owner/repo.git", "")
			pushContractGit(t, "symbolic-ref", "HEAD", "refs/heads/feature/work")
			pushContractGit(t, "config", tc.key, tc.value)
			if v, _ := evaluateMergeGate("git push"); v == mergeAllowed {
				t.Fatal("configured protected destination allowed")
			}
		})
	}
	t.Run("upstream", func(t *testing.T) {
		gitRepo(t, "git@github.com:owner/repo.git", "")
		pushContractGit(t, "symbolic-ref", "HEAD", "refs/heads/feature/work")
		pushContractGit(t, "config", "push.default", "upstream")
		pushContractGit(t, "config", "branch.feature/work.merge", "refs/heads/main")
		if v, _ := evaluateMergeGate("git push"); v == mergeAllowed {
			t.Fatal("upstream main allowed")
		}
	})
}

func TestPushContractOptOutScope(t *testing.T) {
	gitRepo(t, "git@github.com:owner/repo.git", `{"police":{"direct_main":true}}`)
	for _, command := range []string{
		"git push origin main && git push https://elsewhere.example/owner/repo.git main",
		"git push https://elsewhere.example/owner/repo.git main && git push origin main",
		"git push origin main; git push --no-verify origin feature/work",
		"git push origin main; gh api graphql -f query='mutation { mergePullRequest(input:{pullRequestId:\"x\"}) { clientMutationId } }'",
	} {
		t.Run(command, func(t *testing.T) {
			if v, _ := evaluateMergeGate(command); v == mergeAllowed {
				t.Fatal("local opt-out excused foreign or unenforced operation")
			}
		})
	}
	if v, d := evaluateMergeGate("git push origin main && git push origin homolog"); v != mergeAllowed {
		t.Fatalf("local opt-out blocked: %v %s", v, d)
	}
	pushContractGit(t, "config", "remote.origin.pushurl", "https://elsewhere.example/owner/repo.git")
	if v, _ := evaluateMergeGate("git push origin main"); v == mergeAllowed {
		t.Fatal("foreign pushurl excused")
	}
}

// Prove the multi-ref premise with real Git and local bare repositories only.
func TestPushContractRealGitAll(t *testing.T) {
	remote := t.TempDir()
	pushContractGit(t, "init", "--bare", "-q", remote)
	gitRepo(t, remote, "")
	pushContractGit(t, "-c", "commit.gpgsign=false", "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "initial")
	pushContractGit(t, "branch", "-M", "main")
	pushContractGit(t, "checkout", "-qb", "feature/work")
	if v, _ := evaluateMergeGate("git push --all"); v == mergeAllowed {
		t.Fatal("gate allowed all branches")
	}
	pushContractGit(t, "-c", "core.hooksPath=/dev/null", "push", "--all", "origin")
	out, err := exec.Command("git", "--git-dir", remote, "show-ref", "--verify", "refs/heads/main").CombinedOutput()
	if err != nil {
		t.Fatalf("Git did not actually push main: %v %s", err, out)
	}
}

func TestPushContractFlagValuesAreNotOptions(t *testing.T) {
	gitRepo(t, "git@github.com:owner/repo.git", "")
	pushContractGit(t, "symbolic-ref", "HEAD", "refs/heads/main")
	for _, command := range []string{"git push -o --dry-run origin main", "git push -o --tags origin", "git push --dry-run --no-dry-run origin main"} {
		if v, _ := evaluateMergeGate(command); v == mergeAllowed {
			t.Errorf("option value bypass: %s", command)
		}
	}
}

func TestPushContractAttachedRelocation(t *testing.T) {
	gitRepo(t, "git@github.com:owner/repo.git", `{"police":{"direct_main":true}}`)
	pushContractGit(t, "symbolic-ref", "HEAD", "refs/heads/main")
	for _, prefix := range []string{
		"git -C/tmp/other", "git -C../other", "git '-C/tmp/other tree'",
		"git --git-dir=/tmp/other/.git", "git --work-tree=/tmp/other",
		"GIT_DIR=/tmp/other/.git git", "GIT_WORK_TREE=/tmp/other git",
	} {
		for _, suffix := range []string{" push origin main", " push"} {
			command := prefix + suffix
			t.Run(command, func(t *testing.T) {
				if v, _ := evaluateMergeGate(command); v == mergeAllowed {
					t.Fatal("relocated push borrowed local opt-out")
				}
			})
		}
	}
	if v, d := evaluateMergeGate("git push origin main"); v != mergeAllowed {
		t.Fatalf("local opt-out blocked: %v %s", v, d)
	}
}

func TestPushContractPlumbingMultiRefNeverOptsOut(t *testing.T) {
	gitRepo(t, "git@github.com:owner/repo.git", `{"police":{"direct_main":true}}`)
	for _, command := range []string{"git send-pack origin --all", "git send-pack origin --mirror", "git send-pack ../bare.git --all", "git send-pack ../bare.git --mirror"} {
		if v, _ := evaluateMergeGate(command); v != mergeUnenforced {
			t.Errorf("%s: got %v, want unenforced", command, v)
		}
	}
}
