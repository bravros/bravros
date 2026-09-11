package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bravros/bravros/cli/internal/token"
)

// laneRepo is gitRepo plus a fixed origin — the lane never reads origin, but
// gitRepo needs one — and returns the repo path (cwd) for the cd carve-out tests.
func laneRepo(t *testing.T, cfg string) string {
	t.Helper()
	gitRepo(t, "git@github.com:paylog/ev.git", cfg)
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

const laneOID = "0123456789abcdef0123456789abcdef01234567"

func writeLaneStamp(t *testing.T, pr, sha string) {
	t.Helper()
	if err := os.MkdirAll(".planning", 0o755); err != nil {
		t.Fatal(err)
	}
	doc, _ := json.Marshal(map[string]any{"pr": 12, "reviewer_verdict": "approved", "commit_sha": sha, "source": "test"})
	if err := os.WriteFile(filepath.Join(".planning", ".review-stamp-"+pr+".json"), doc, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestStagingLane_HomologToMainAllowed is the headline: a CLEAN homolog→main
// PR merges from a no-config repo with no token, and the merge is audited.
func TestStagingLane_HomologToMainAllowed(t *testing.T) {
	home := rule52TestSetEnv(t)
	laneRepo(t, "")
	fakePRView(t, "main", "homolog", "CLEAN", "", laneOID, false)

	v, d := evaluateMergeGate("gh pr merge 12 --merge")
	if v != mergeStagingLane {
		t.Fatalf("want mergeStagingLane, got %v (%s)", v, d)
	}
	if !strings.Contains(d, "pr=12 homolog->main mode=open") {
		t.Errorf("lane detail must summarise the merge, got %q", d)
	}

	if blocked, _, reason := rule52TestInvoke(t, "gh pr merge 12 --merge"); blocked {
		t.Fatalf("lane merge blocked: %s", reason)
	}
	audit, err := os.ReadFile(filepath.Join(home, ".claude", "state", "police-merge-audit.log"))
	if err != nil {
		t.Fatalf("audit line not written: %v", err)
	}
	line := strings.TrimSpace(string(audit))
	if !strings.Contains(line, "pr=12 homolog->main mode=open") || !strings.Contains(line, "session=") {
		t.Errorf("audit line shape: %q", line)
	}
	if _, err := time.Parse(time.RFC3339, strings.Fields(line)[0]); err != nil {
		t.Errorf("audit line must start with an RFC3339 timestamp: %q", line)
	}

	// The bare form (current branch's PR) is eligible too.
	if v, _ := evaluateMergeGate("gh pr merge --merge"); v != mergeStagingLane {
		t.Errorf("bare `gh pr merge` must use the lane, got %v", v)
	}
}

func TestStagingLane_FeatureHeadStaysGated(t *testing.T) {
	rule52TestSetEnv(t)
	laneRepo(t, "")
	fakePRView(t, "main", "feature/x", "CLEAN", "APPROVED", laneOID, false)

	v, d := evaluateMergeGate("gh pr merge 12 --merge")
	if v != mergeProtected {
		t.Fatalf("want mergeProtected, got %v", v)
	}
	if !strings.Contains(d, "head branch 'feature/x' is not the staging branch 'homolog'") {
		t.Errorf("detail must name the head and the staging branch: %q", d)
	}
	msg := mergeBlockMessage(v, d)
	if !strings.Contains(msg, "Staging lane refused: head branch 'feature/x'") || !strings.Contains(msg, "bravros police unlock") {
		t.Errorf("block must name the condition and the fallback: %q", msg)
	}
	if strings.Contains(msg, "open a PR from") {
		t.Errorf("a lane-refused block must not give the generic open-a-PR advice: %q", msg)
	}
}

func TestStagingLane_CustomStagingBranch(t *testing.T) {
	rule52TestSetEnv(t)
	laneRepo(t, `{"staging_branch":"staging"}`)

	fakePRView(t, "main", "staging", "CLEAN", "", laneOID, false)
	if v, d := evaluateMergeGate("gh pr merge 12 --merge"); v != mergeStagingLane {
		t.Errorf("custom staging head must use the lane, got %v (%s)", v, d)
	}
	t.Setenv("POLICE_LOOKUP_HEAD", "homolog")
	if v, d := evaluateMergeGate("gh pr merge 12 --merge"); v != mergeProtected || !strings.Contains(d, "'homolog' is not the staging branch 'staging'") {
		t.Errorf("homolog is not this repo's staging branch: got %v (%s)", v, d)
	}
}

func TestStagingLane_MergeStateGates(t *testing.T) {
	rule52TestSetEnv(t)
	laneRepo(t, "")
	for state, want := range map[string]mergeVerdict{
		"CLEAN":    mergeStagingLane,
		"UNKNOWN":  mergeIndeterminate,
		"UNSTABLE": mergeProtected,
		"BLOCKED":  mergeProtected,
		"DIRTY":    mergeProtected,
		"BEHIND":   mergeProtected,
		"DRAFT":    mergeProtected,
	} {
		fakePRView(t, "main", "homolog", state, "", laneOID, false)
		v, d := evaluateMergeGate("gh pr merge 12 --merge")
		if v != want {
			t.Errorf("%s: got %v want %v (%s)", state, v, want, d)
			continue
		}
		switch want {
		case mergeIndeterminate:
			if !strings.Contains(d, "has not computed mergeability yet; re-run in a few seconds") {
				t.Errorf("UNKNOWN wording: %q", d)
			}
			msg := mergeBlockMessage(v, d)
			if strings.Contains(msg, "could not be determined") || !strings.Contains(msg, "re-run in a few seconds") || !strings.Contains(msg, "bravros police unlock") {
				t.Errorf("UNKNOWN block must use the retry wording, not the offline one: %q", msg)
			}
		case mergeProtected:
			if !strings.Contains(d, "mergeStateStatus is "+state) {
				t.Errorf("%s: detail must name the state: %q", state, d)
			}
		}
	}
}

func TestStagingLane_AutoprLockClosesLane(t *testing.T) {
	rule52TestSetEnv(t)
	laneRepo(t, "")
	fakePRView(t, "main", "homolog", "CLEAN", "", laneOID, false)
	if err := os.MkdirAll(".planning", 0o755); err != nil {
		t.Fatal(err)
	}
	for _, lock := range []string{".auto-pr-lock", ".auto-merge-lock"} {
		path := filepath.Join(".planning", lock)
		if err := os.WriteFile(path, []byte("skill=auto-pr\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		v, d := evaluateMergeGate("gh pr merge 12 --merge")
		if v != mergeProtected || !strings.Contains(d, lock) || !strings.Contains(d, "autonomous") {
			t.Errorf("%s: got %v (%s)", lock, v, d)
		}
		if blocked, _, reason := rule52TestInvoke(t, "gh pr merge 12 --merge"); !blocked || !strings.Contains(reason, lock) {
			t.Errorf("%s: hook must block and name the file, got blocked=%v %q", lock, blocked, reason)
		}
		os.Remove(path)
	}
	if v, _ := evaluateMergeGate("gh pr merge 12 --merge"); v != mergeStagingLane {
		t.Errorf("lane must reopen once the lock is gone, got %v", v)
	}
}

func TestStagingLane_NeverForRepoFlagURLOrBranch(t *testing.T) {
	rule52TestSetEnv(t)
	laneRepo(t, "")
	log := fakePRView(t, "main", "homolog", "CLEAN", "APPROVED", laneOID, false)
	for cmd, wantArgv := range map[string]string{
		"gh pr merge 12 -R paylog/ev --merge":                      "--repo\npaylog/ev\n12\n",
		"gh pr merge 12 --repo=paylog/ev --merge":                  "--repo\npaylog/ev\n12\n",
		"gh pr merge https://github.com/paylog/ev/pull/12 --merge": "https://github.com/paylog/ev/pull/12\n",
		"gh pr merge homolog --merge":                              "homolog\n",
	} {
		v, d := evaluateMergeGate(cmd)
		if v != mergeProtected || !strings.Contains(d, "never use the lane") {
			t.Errorf("%s: got %v (%s)", cmd, v, d)
		}
		argv, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(string(argv), wantArgv) {
			t.Errorf("%s: lookup argv %q must end with %q", cmd, argv, wantArgv)
		}
	}
}

func TestStagingLane_CrossRepositoryHeadRejected(t *testing.T) {
	rule52TestSetEnv(t)
	laneRepo(t, "")
	fakePRView(t, "main", "homolog", "CLEAN", "", laneOID, true)
	if v, d := evaluateMergeGate("gh pr merge 12 --merge"); v != mergeProtected || !strings.Contains(d, "isCrossRepository") {
		t.Errorf("cross-repo head must be refused: %v (%s)", v, d)
	}
}

func TestStagingLane_ReviewedMode(t *testing.T) {
	rule52TestSetEnv(t)
	laneRepo(t, `{"police":{"staging_lane":"reviewed"}}`)

	fakePRView(t, "main", "homolog", "CLEAN", "APPROVED", laneOID, false)
	if v, d := evaluateMergeGate("gh pr merge 12 --merge"); v != mergeStagingLane || !strings.Contains(d, "mode=reviewed") {
		t.Errorf("APPROVED must use the lane: %v (%s)", v, d)
	}

	t.Setenv("POLICE_LOOKUP_REVIEW", "")
	v, d := evaluateMergeGate("gh pr merge 12 --merge")
	if v != mergeProtected || !strings.Contains(d, `"reviewed"`) || !strings.Contains(d, ".review-stamp-12.json") {
		t.Errorf("no review, no stamp must be refused with the reviewed wording: %v (%s)", v, d)
	}

	writeLaneStamp(t, "12", laneOID)
	if v, d := evaluateMergeGate("gh pr merge 12 --merge"); v != mergeStagingLane {
		t.Errorf("fresh stamp must use the lane: %v (%s)", v, d)
	}

	writeLaneStamp(t, "12", "ffffffffffffffffffffffffffffffffffffffff")
	if v, _ := evaluateMergeGate("gh pr merge 12 --merge"); v != mergeProtected {
		t.Errorf("stale stamp must be refused: %v", v)
	}
}

func TestStagingLane_OffAndUnknownModeAlwaysToken(t *testing.T) {
	rule52TestSetEnv(t)
	for cfg, want := range map[string]string{
		`{"police":{"staging_lane":"off"}}`:       `"off"`,
		`{"police":{"staging_lane":"opne"}}`:      `"opne"`,
		`{"police":{"staging_lane":"REVIEWED "}}`: "", // trimmed + lowercased: still valid
	} {
		laneRepo(t, cfg)
		fakePRView(t, "main", "homolog", "CLEAN", "APPROVED", laneOID, false)
		v, d := evaluateMergeGate("gh pr merge 12 --merge")
		if want == "" {
			if v != mergeStagingLane {
				t.Errorf("%s: got %v (%s)", cfg, v, d)
			}
			continue
		}
		if v != mergeProtected || !strings.Contains(d, want) {
			t.Errorf("%s: got %v (%s), want protected naming %s", cfg, v, d, want)
		}
		if blocked, _, _ := rule52TestInvoke(t, "gh pr merge 12 --merge"); !blocked {
			t.Errorf("%s: hook must block without a token", cfg)
		}
	}
}

func TestStagingLane_APIRouteNotEligible(t *testing.T) {
	rule52TestSetEnv(t)
	laneRepo(t, "")
	fakePRView(t, "main", "homolog", "CLEAN", "APPROVED", laneOID, false)
	for _, cmd := range []string{
		"gh api -X PUT repos/paylog/ev/pulls/12/merge",
		"gh api -X POST repos/paylog/ev/merges -f base=main -f head=homolog",
		"git push origin homolog:main",
	} {
		if v, _ := evaluateMergeGate(cmd); v != mergeProtected {
			t.Errorf("%s: got %v, the lane is gh pr merge only", cmd, v)
		}
	}
}

func TestStagingLane_LaterProtectedHitStillBlocks(t *testing.T) {
	rule52TestSetEnv(t)
	laneRepo(t, "")
	fakePRView(t, "main", "homolog", "CLEAN", "", laneOID, false)
	// Spelled from parts so the literal never appears in a shell command that
	// this repo's own police hook would read.
	skipHooks := "--no-" + "verify"
	for cmd, want := range map[string]mergeVerdict{
		"gh pr merge 12 --merge && git push origin main":                              mergeProtected,
		"gh pr merge 12 --merge && git push " + skipHooks + " origin homolog":         mergeUnenforced,
		"gh pr merge 12 --merge && gh api -X POST repos/o/r/merges --input body.json": mergeIndeterminate,
		"gh pr merge 12 --merge && git push origin homolog":                           mergeStagingLane,
	} {
		if v, d := evaluateMergeGate(cmd); v != want {
			t.Errorf("%s: got %v want %v (%s)", cmd, v, want, d)
		}
	}
	if blocked, _, _ := rule52TestInvoke(t, "gh pr merge 12 --merge && git push origin main"); !blocked {
		t.Error("a lane merge must not excuse a later protected push on the same line")
	}
}

func TestStagingLane_CwdCarveOut(t *testing.T) {
	rule52TestSetEnv(t)
	dir := laneRepo(t, "")
	fakePRView(t, "main", "homolog", "CLEAN", "", laneOID, false)
	merge := " && gh pr merge 2053 --merge"

	if v, d := evaluateMergeGateIn("cd "+dir+merge, dir); v != mergeStagingLane {
		t.Errorf("cd <cwd> must not count as relocation: %v (%s)", v, d)
	}
	if v, _ := evaluateMergeGateIn("cd '"+dir+"'"+merge, dir); v != mergeStagingLane {
		t.Errorf("quoted cd <cwd> is the same path: %v", v)
	}
	if v, _ := evaluateMergeGateIn("cd "+dir+"/."+merge, dir); v != mergeStagingLane {
		t.Errorf("Clean-equal path must match: %v", v)
	}
	if v, _ := evaluateMergeGateIn("cd "+dir+merge, ""); v != mergeIndeterminate {
		t.Errorf("no cwd known: got %v, want today's fail-closed indeterminate", v)
	}
	for _, cmd := range []string{
		"cd repo" + merge,
		"cd ." + merge,
		"cd /tmp/other" + merge,
		"cd ~" + merge,
		"cd ~/Code" + merge,
		"cd $HOME" + merge,
		"cd -" + merge,
		"pushd " + dir + merge,
		"cd -P " + dir + merge,
		"cd " + dir + " extra" + merge,
	} {
		if v, _ := evaluateMergeGateIn(cmd, dir); v != mergeIndeterminate {
			t.Errorf("%s: got %v, must stay fail-closed", cmd, v)
		}
	}
	// The same helper serves the push arm, so a cd-to-cwd push resolves too.
	if v, _ := evaluateMergeGateIn("cd "+dir+" && git push origin main", dir); v != mergeProtected {
		t.Errorf("cd <cwd> && push main must resolve as a plain protected push, got %v", v)
	}
	// The hook reads cwd from the payload.
	payload := `{"tool_name":"bash","tool_input":{"command":"cd ` + dir + ` && gh pr merge 2053 --merge"},"cwd":"` + dir + `"}`
	var out bytes.Buffer
	policePreToolUseCmd.SetIn(strings.NewReader(payload))
	policePreToolUseCmd.SetOut(&out)
	if err := policePreToolUseCmd.RunE(policePreToolUseCmd, nil); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("payload cwd must drive the carve-out, got %s", out.String())
	}
}

func TestMergeBlockMessage_Hints(t *testing.T) {
	rule52TestSetEnv(t)

	laneRepo(t, "")
	msg := mergeBlockMessage(mergeProtected, "pushing to a protected branch")
	for _, want := range []string{"has no .bravros/config.json, so main is protected by default", "bravros police direct-main on", "open a PR from homolog", "bravros police unlock", "bravros promote unlock"} {
		if !strings.Contains(msg, want) {
			t.Errorf("no-config block missing %q: %q", want, msg)
		}
	}

	laneRepo(t, `{"staging_branch":"staging"}`)
	msg = mergeBlockMessage(mergeProtected, "pushing to a protected branch")
	if strings.Contains(msg, "has no .bravros/config.json") || !strings.Contains(msg, "police.direct_main is not set") || !strings.Contains(msg, "open a PR from staging") {
		t.Errorf("key-absent block: %q", msg)
	}

	for _, m := range []string{
		mergeBlockMessage(mergeUnenforced, "x"),
		mergeBlockMessage(mergeIndeterminate, "merging a pull request"),
		mergeBlockMessage(mergeIndeterminate, laneUnknownDetail),
		mergeBlockMessage(mergeProtected, "merging a pull request into a protected branch ("+laneRefusedPrefix+"an autonomous lock is present (.planning/.auto-pr-lock))"),
	} {
		if !strings.HasPrefix(m, "✋🏽 Police Block:") || !strings.Contains(m, "bravros police unlock") {
			t.Errorf("every block keeps the prefix and the fallback: %q", m)
		}
	}
	lock := mergeBlockMessage(mergeProtected, "merging a pull request into a protected branch ("+laneRefusedPrefix+"an autonomous lock is present (.planning/.auto-pr-lock) — autonomous pipelines never merge to main)")
	if !strings.Contains(lock, "Staging lane refused: an autonomous lock is present (.planning/.auto-pr-lock)") {
		t.Errorf("lock block must keep the file name intact: %q", lock)
	}
}

func TestPoliceDirectMainCmd(t *testing.T) {
	rule52TestSetEnv(t)
	run := func(arg string) string {
		var out bytes.Buffer
		policeDirectMainCmd.SetOut(&out)
		if err := policeDirectMainCmd.RunE(policeDirectMainCmd, []string{arg}); err != nil {
			t.Fatalf("direct-main %s: %v", arg, err)
		}
		return out.String()
	}
	read := func() map[string]any {
		data, err := os.ReadFile(".bravros/config.json")
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("not JSON: %v\n%s", err, data)
		}
		return doc
	}

	// No config at all: a minimal file is created.
	laneRepo(t, "")
	if out := run("status"); !strings.Contains(out, "police.direct_main: off") || !strings.Contains(out, "absent") {
		t.Errorf("status before: %q", out)
	}
	if out := run("on"); !strings.Contains(out, "created .bravros/config.json") || !strings.Contains(out, "Commit it") {
		t.Errorf("on output: %q", out)
	}
	doc := read()
	if len(doc) != 1 || doc["police"].(map[string]any)["direct_main"] != true {
		t.Errorf("minimal file expected, got %v", doc)
	}
	if !policeDirectMainAllowed("") {
		t.Error("the written key must be what the gate reads")
	}
	if out := run("status"); !strings.Contains(out, "police.direct_main: on") || !strings.Contains(out, "present") {
		t.Errorf("status after on: %q", out)
	}

	// Existing config: other keys survive on and off.
	laneRepo(t, `{"$schema":"https://bravros.dev/schemas/config.v0.json","staging_branch":"staging","police":{"staging_lane":"reviewed"},"stack":{"name":"go","version":1.26}}`)
	run("on")
	doc = read()
	if doc["staging_branch"] != "staging" || doc["$schema"] == nil || doc["stack"].(map[string]any)["version"] != 1.26 {
		t.Errorf("other keys must be preserved: %v", doc)
	}
	police := doc["police"].(map[string]any)
	if police["direct_main"] != true || police["staging_lane"] != "reviewed" {
		t.Errorf("police block must merge, not replace: %v", police)
	}
	if out := run("off"); !strings.Contains(out, "removed police.direct_main") {
		t.Errorf("off output: %q", out)
	}
	doc = read()
	if _, has := doc["police"].(map[string]any)["direct_main"]; has || doc["police"].(map[string]any)["staging_lane"] != "reviewed" || doc["staging_branch"] != "staging" {
		t.Errorf("off must remove only the key: %v", doc)
	}
	if policeDirectMainAllowed("") {
		t.Error("off must close the opt-out")
	}

	// off on a police block that only held direct_main drops the empty block.
	laneRepo(t, `{"staging_branch":"homolog","police":{"direct_main":true}}`)
	run("off")
	if doc = read(); doc["police"] != nil || doc["staging_branch"] != "homolog" {
		t.Errorf("empty police block should be dropped: %v", doc)
	}
}

func TestPolicePreToolUse_PermittedWithPromoteToken(t *testing.T) {
	rule52TestSetEnv(t)
	laneRepo(t, "")
	// Feature head so the lane does not mask what the token is proving.
	fakePRView(t, "main", "feature/x", "CLEAN", "", laneOID, false)

	if blocked, _, _ := rule52TestInvoke(t, "gh pr merge 123 --merge"); !blocked {
		t.Fatal("precondition: blocked without any token")
	}
	if _, err := (token.Gate{Name: "promote"}).Mint(5, ""); err != nil {
		t.Fatal(err)
	}
	if blocked, _, reason := rule52TestInvoke(t, "gh pr merge 123 --merge"); blocked {
		t.Errorf("promote token must permit the merge: %s", reason)
	}
	if blocked, _, _ := rule52TestInvoke(t, "git push origin main"); blocked {
		t.Error("promote token must permit a protected push too")
	}
}

func TestPolicePreToolUse_ExpiredPromoteTokenDenies(t *testing.T) {
	home := rule52TestSetEnv(t)
	laneRepo(t, "")
	fakePRView(t, "main", "feature/x", "CLEAN", "", laneOID, false)

	path := filepath.Join(home, ".claude", "state", "promote-token")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	expired, _ := json.Marshal(token.Token{CreatedAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(-time.Minute), TTLMinutes: 5, SingleUse: true})
	if err := os.WriteFile(path, expired, 0o600); err != nil {
		t.Fatal(err)
	}
	if blocked, _, _ := rule52TestInvoke(t, "gh pr merge 123 --merge"); !blocked {
		t.Error("an expired promote token must not permit a merge")
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("the hot path must not delete the expired token (Valid, not Present)")
	}
}

// TestMergeGate_RedefinedGhOrGit — adversarial review, blocker 1: a same-line
// function or alias named gh/git rebinds the word every scan anchors on, and
// all three shapes below took the same-repo lane into ANOTHER repo.
func TestMergeGate_RedefinedGhOrGit(t *testing.T) {
	rule52TestSetEnv(t)
	laneRepo(t, "")
	fakePRView(t, "main", "homolog", "CLEAN", "", laneOID, false)

	for _, cmd := range []string{
		`gh(){ command gh "$@" -R o/r; }; gh pr merge 5 --merge`,
		`function gh { /usr/bin/gh "$@" -R o/r; }; gh pr merge 5`,
		`alias gh='gh -R o/r'; shopt -s expand_aliases; gh pr merge 5`,
		`function gh() { command gh "$@" -R o/r; }; gh pr merge 5 --merge`,
		`gh () { command gh "$@" -R o/r; }; gh pr merge 5 --merge`,
		`git(){ command git "$@"; }; git push origin homolog`,
		`alias git='git -C /tmp/other'; git push origin main`,
		`bash -c 'gh(){ command gh "$@" -R o/r; }; gh pr merge 5 --merge'`,
		`eval 'alias gh="gh -R o/r"'; gh pr merge 5 --merge`,
		`{ gh(){ command gh "$@" -R o/r; }; }; gh pr merge 5 --merge`,
	} {
		v, d := evaluateMergeGate(cmd)
		if v != mergeIndeterminate || d != redefinesDetail {
			t.Errorf("%s: got %v (%s), want indeterminate %q", cmd, v, d, redefinesDetail)
		}
	}
	blocked, _, reason := rule52TestInvoke(t, `gh(){ command gh "$@" -R o/r; }; gh pr merge 5 --merge`)
	if !blocked || !strings.Contains(reason, "redefines gh/git") || !strings.Contains(reason, "bravros police unlock") {
		t.Errorf("hook must block a redefinition and name it: blocked=%v %q", blocked, reason)
	}

	// Prose that merely mentions a definition is not one: the match needs the
	// definition at a command position.
	for _, cmd := range []string{
		`git commit -m "alias gh=foo breaks the hook"`,
		`echo "gh() is a shell function"`,
		`regh() { :; }; gh pr merge 12 --merge`,
		`mygit(){ :; }; gh pr merge 12 --merge`,
	} {
		if v, d := evaluateMergeGate(cmd); v == mergeIndeterminate && d == redefinesDetail {
			t.Errorf("%s: a mention is not a redefinition", cmd)
		}
	}
}

// TestMergeGate_PathFormAndSubstitutedProgram — adversarial review, major 2:
// `/opt/homebrew/bin/gh`, `./gh`, `"$(command -v gh)"` and `/usr/bin/git`
// were all ALLOWED because every anchor compared the bare word.
func TestMergeGate_PathFormAndSubstitutedProgram(t *testing.T) {
	rule52TestSetEnv(t)
	laneRepo(t, "")
	fakePRView(t, "main", "homolog", "CLEAN", "", laneOID, false)

	for cmd, want := range map[string]mergeVerdict{
		`/opt/homebrew/bin/gh pr merge 5 -R o/r --merge`:       mergeProtected,
		`./gh pr merge 5 -R o/r --merge`:                       mergeProtected,
		`/usr/local/bin/gh api -X PUT repos/o/r/pulls/5/merge`: mergeProtected,
		`/usr/bin/git push origin main`:                        mergeProtected,
		`/usr/bin/git -c push.default=simple push origin main`: mergeProtected,
		`/usr/bin/git send-pack origin HEAD:main`:              mergeProtected,
		// The path form is still the same program, so the lane still applies.
		`/opt/homebrew/bin/gh pr merge 12 --merge`: mergeStagingLane,
	} {
		if v, d := evaluateMergeGate(cmd); v != want {
			t.Errorf("%s: got %v want %v (%s)", cmd, v, want, d)
		}
	}
	for _, cmd := range []string{
		`"$(command -v gh)" pr merge 5 -R o/r --merge`,
		"`which gh` pr merge 5 --merge",
		`${GH} pr merge 5 --merge`,
		`$GH pr merge 5 --merge`,
		`"$(command -v gh)" api -X PUT repos/o/r/pulls/5/merge`,
		`$GIT push origin main`,
		`"${GIT_BIN}" push origin main`,
	} {
		v, d := evaluateMergeGate(cmd)
		if v != mergeIndeterminate || d != substitutedProgramDetail {
			t.Errorf("%s: got %v (%s), want indeterminate %q", cmd, v, d, substitutedProgramDetail)
		}
	}
	// An expansion that is not in the program position is not a substitution.
	for _, cmd := range []string{
		`FOO=$BAR gh pr merge 12 --merge`,
		`echo "$X" && gh pr merge 12 --merge`,
		`gh pr merge 12 --merge --body "$(cat notes.md)"`,
	} {
		if v, d := evaluateMergeGate(cmd); v != mergeStagingLane {
			t.Errorf("%s: got %v (%s), want the lane", cmd, v, d)
		}
	}
	if blocked, _, reason := rule52TestInvoke(t, `"$(command -v gh)" pr merge 5 -R o/r --merge`); !blocked || !strings.Contains(reason, "named by a substitution") {
		t.Errorf("hook must block and name the substitution: %v %q", blocked, reason)
	}
}

// TestStagingLane_EvasionTable — the reviewer's nit rows plus finding 1, as a
// table so the next shape has a row to join.
func TestStagingLane_EvasionTable(t *testing.T) {
	rule52TestSetEnv(t)
	laneRepo(t, "")
	fakePRView(t, "main", "homolog", "CLEAN", "", laneOID, false)
	for _, tc := range []struct {
		cmd    string
		want   mergeVerdict
		detail string
	}{
		{`gh pr merge 12 -Rother/repo --merge`, mergeProtected, "never use the lane"},
		{`gh pr merge 12 --merge --delete-branch`, mergeProtected, "--delete-branch/-d"},
		{`gh pr merge 12 -d --merge`, mergeProtected, "--delete-branch/-d"},
		{`gh pr merge 12 --merge -d`, mergeProtected, "--delete-branch/-d"},
		{`GH_REPO=other/repo gh pr merge 12 --merge`, mergeIndeterminate, "forge context changed"},
		{`GH_HOST=github.example gh pr merge 12 --merge`, mergeIndeterminate, "forge context changed"},
		{`export GH_REPO=other/repo; gh pr merge 12 --merge`, mergeIndeterminate, "forge context changed"},
		{`gh(){ command gh "$@" -R o/r; }; gh pr merge 12 --merge`, mergeIndeterminate, redefinesDetail},
		{`function gh { /usr/bin/gh "$@" -R o/r; }; gh pr merge 12`, mergeIndeterminate, redefinesDetail},
		{`alias gh='gh -R o/r'; shopt -s expand_aliases; gh pr merge 12`, mergeIndeterminate, redefinesDetail},
		{`/opt/homebrew/bin/gh pr merge 12 -R o/r --merge`, mergeProtected, "never use the lane"},
		{`"$(command -v gh)" pr merge 12 -R o/r --merge`, mergeIndeterminate, substitutedProgramDetail},
		{`gh pr merge "$PR" --merge`, mergeIndeterminate, prUnreadableDetail},
		{`gh pr merge 12 --merge`, mergeStagingLane, "pr=12 homolog->main mode=open"},
	} {
		v, d := evaluateMergeGate(tc.cmd)
		if v != tc.want || !strings.Contains(d, tc.detail) {
			t.Errorf("%s: got %v (%s), want %v containing %q", tc.cmd, v, d, tc.want, tc.detail)
		}
	}
	msg := mergeBlockMessage(mergeIndeterminate, prUnreadableDetail)
	if !strings.Contains(msg, "put the literal PR number on the merge") || !strings.Contains(msg, "bravros police unlock") {
		t.Errorf("$VAR PR block must tell the agent to spell the number: %q", msg)
	}
	msg = mergeBlockMessage(mergeProtected, "merging a pull request into a protected branch ("+laneRefusedPrefix+"--delete-branch/-d is not allowed on a lane merge — the staging branch must survive it; drop the flag)")
	if !strings.Contains(msg, "Staging lane refused: --delete-branch/-d") {
		t.Errorf("delete-branch block must name the flag: %q", msg)
	}
}

// TestMergeGate_RelocatedMergeSkipsLookup: a `cd`/GH_REPO merge is refused from
// the command string alone, so the forge lookup — 5 s on a bad network — must
// never run for it.
func TestMergeGate_RelocatedMergeSkipsLookup(t *testing.T) {
	rule52TestSetEnv(t)
	dir := laneRepo(t, "")
	log := fakePRView(t, "main", "homolog", "CLEAN", "", laneOID, false)
	for _, cmd := range []string{
		`cd /tmp/other && gh pr merge 12 --merge`,
		`GH_REPO=other/repo gh pr merge 12 --merge`,
		`pushd sub; gh pr merge 12 --merge`,
	} {
		if v, _ := evaluateMergeGateIn(cmd, dir); v != mergeIndeterminate {
			t.Errorf("%s: got %v want indeterminate", cmd, v)
		}
		if _, err := os.Stat(log); !os.IsNotExist(err) {
			t.Errorf("%s: the forge was consulted for a merge refused from the string (%v)", cmd, err)
		}
	}
	// Relocation with an explicit -R names the repo, so the lookup does run.
	if v, _ := evaluateMergeGateIn(`cd /tmp/other && gh pr merge 12 -R o/r --merge`, dir); v != mergeProtected {
		t.Errorf("-R merge after cd: got %v want protected", v)
	}
	if _, err := os.Stat(log); err != nil {
		t.Errorf("-R merge must consult the forge: %v", err)
	}
}

// TestLookupPR_HungGhReturns: a gh that never answers — or whose child keeps
// the pipe open after the context kills it — must not hang the hook. WaitDelay
// closes the pipe one second after the kill.
func TestLookupPR_HungGhReturns(t *testing.T) {
	bin := t.TempDir()
	// `sleep` runs as a CHILD of sh (the trailing exit prevents a tail exec), so
	// killing sh leaves sleep holding stdout — the exact shape WaitDelay exists for.
	script := "#!/bin/sh\nsleep 20\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := prLookupTimeout
	prLookupTimeout = 300 * time.Millisecond
	t.Cleanup(func() { prLookupTimeout = old })

	start := time.Now()
	if _, ok := lookupPR("", "12"); ok {
		t.Error("a hung lookup must fail closed")
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("hung gh held the hook for %s; WaitDelay must release it shortly after the context kill", took)
	}
}

// TestPolicePreToolUse_PayloadShapeFailsClosed — adversarial review, minor: a
// non-string cwd made json.Unmarshal fail and the hook returned nil — a
// fail-open on every gate. The cwd is now decoded tolerantly, and a Bash
// payload that still cannot be parsed is denied rather than waved through.
func TestPolicePreToolUse_PayloadShapeFailsClosed(t *testing.T) {
	rule52TestSetEnv(t)
	laneRepo(t, "")
	run := func(payload string) string {
		var out bytes.Buffer
		policePreToolUseCmd.SetIn(strings.NewReader(payload))
		policePreToolUseCmd.SetOut(&out)
		if err := policePreToolUseCmd.RunE(policePreToolUseCmd, nil); err != nil {
			t.Fatal(err)
		}
		if out.Len() == 0 {
			return ""
		}
		return assertPoliceDeny(t, out.Bytes())
	}
	// Non-string cwd: the command is still read and still gated.
	if reason := run(`{"tool_name":"Bash","tool_input":{"command":"git push origin main"},"cwd":123}`); !strings.Contains(reason, "Merging or pushing to main") {
		t.Errorf("numeric cwd must not fail open: %q", reason)
	}
	if reason := run(`{"tool_name":"Bash","tool_input":{"command":"git push origin main"},"cwd":{"path":"/x"}}`); !strings.Contains(reason, "Merging or pushing to main") {
		t.Errorf("object cwd must not fail open: %q", reason)
	}
	if reason := run(`{"tool_name":"Bash","tool_input":{"command":"printf hello"},"cwd":null}`); reason != "" {
		t.Errorf("null cwd on a harmless command must pass: %q", reason)
	}
	// A Bash payload the decoder rejects outright is denied, not passed.
	for _, payload := range []string{
		`{"tool_name":"Bash","tool_input":{"command":42}}`,
		`{"tool_name":"bash","tool_input":"git push origin main"}`,
		`{"tool_name":"Bash", "tool_input":{"command":"git push origin main"`,
	} {
		if reason := run(payload); !strings.Contains(reason, "could not be parsed") {
			t.Errorf("%s: unparseable Bash payload must fail closed, got %q", payload, reason)
		}
	}
	// A non-Bash payload that cannot be parsed is still a silent pass.
	if reason := run(`{"tool_name":"Read","tool_input":{"command":42}}`); reason != "" {
		t.Errorf("unparseable non-Bash payload must pass: %q", reason)
	}
	if reason := run(`not json at all`); reason != "" {
		t.Errorf("garbage naming no tool must pass: %q", reason)
	}
}

// TestGateInputWriteFloor — adversarial review, major 3: the gate's inputs are
// writable from Bash. `touch ~/.claude/state/police-token` minted a merge
// token, a `printf` forged a "reviewed" stamp, a redirect widened the lane.
func TestGateInputWriteFloor(t *testing.T) {
	home := rule52TestSetEnv(t)
	laneRepo(t, "")

	denied := []struct{ cmd, path, hint string }{
		{`touch ~/.claude/state/police-token`, "~/.claude/state/police-token", "minted only OUTSIDE Claude Code"},
		{`touch ` + home + `/.claude/state/police-token`, home + "/.claude/state/police-token", "bravros police unlock"},
		{`touch "$HOME/.claude/state/promote-token"`, "$HOME/.claude/state/promote-token", "bravros promote unlock"},
		{`echo valid > ~/.claude/state/police-token`, "~/.claude/state/police-token", "Writing one from Bash is never"},
		{`echo valid >> ~/.claude/state/police-token`, "~/.claude/state/police-token", ""},
		{`printf valid >~/.claude/state/police-token`, "~/.claude/state/police-token", ""},
		{`printf '{}' 2>/dev/null > ~/.agent_config/state/destructive-token`, "~/.agent_config/state/destructive-token", ""},
		{`cp /tmp/t "$HOME/.claude/state/destructive-token"`, "$HOME/.claude/state/destructive-token", ""},
		{`cp /tmp/police-token ~/.claude/state/`, "~/.claude/state/police-token", ""},
		{`ln -s /tmp/t ~/.claude/state/promote-token`, "~/.claude/state/promote-token", ""},
		{`chmod 600 ~/.claude/state/police-token`, "~/.claude/state/police-token", ""},
		{`dd if=/dev/zero of=~/.claude/state/police-token bs=1 count=1`, "~/.claude/state/police-token", ""},
		{`cat /tmp/t | tee ~/.claude/state/police-token`, "~/.claude/state/police-token", ""},
		{`cd ~/.claude/state && touch police-token`, "police-token", ""},
		{`sudo touch ~/.claude/state/police-token`, "~/.claude/state/police-token", ""},
		{`bash -c 'touch ~/.claude/state/police-token'`, "~/.claude/state/police-token", ""},
		{`eval "touch ~/.claude/state/police-token"`, "~/.claude/state/police-token", ""},
		{`python3 -c "open('` + home + `/.claude/state/police-token','w').close()"`, home + "/.claude/state/police-token", ""},
		{`printf '{"commit_sha":"abc"}' > .planning/.review-stamp-5.json`, ".planning/.review-stamp-5.json", "bravros pr-review <N> --write-stamp"},
		{`echo '{}' > ./.planning/.review-stamp-5.json`, "./.planning/.review-stamp-5.json", "forged review"},
		{`cp /tmp/.review-stamp-5.json .planning/`, ".planning/.review-stamp-5.json", ""},
		{`mv /tmp/stamp.json .planning/.review-stamp-5.json`, ".planning/.review-stamp-5.json", ""},
		{`sed -i 's/ffff/abcd/' .planning/.review-stamp-5.json`, ".planning/.review-stamp-5.json", ""},
		{`printf '{"staging_branch":"feature/x"}' > .bravros/config.json`, ".bravros/config.json", "Write tool or bravros police direct-main / bravros init"},
		{`cat > .bravros/config.json <<'EOF'` + "\n{}\nEOF", ".bravros/config.json", "commit it"},
		{`tee .bravros/config.json < /tmp/cfg.json`, ".bravros/config.json", ""},
		{`cp /tmp/config.json .bravros/`, ".bravros/config.json", ""},
		{`cp /tmp/config.json .bravros`, ".bravros/config.json", ""},
		{`mv /tmp/cfg.json .bravros/config.json`, ".bravros/config.json", ""},
		{`mv .bravros/config.json /tmp/`, ".bravros/config.json", ""},
		{`rm .bravros/config.json`, ".bravros/config.json", ""},
		{`rm -f ./.bravros/config.json`, "./.bravros/config.json", ""},
		{`sed -i 's/"off"/"open"/' .bravros/config.json`, ".bravros/config.json", ""},
		{`sed -i.bak -e 's/off/open/' .bravros/config.json`, ".bravros/config.json", ""},
		{`sed --in-place 's/off/open/' .bravros/config.json`, ".bravros/config.json", ""},
		{`perl -i -pe 's/off/open/' .bravros/config.json`, ".bravros/config.json", ""},
		{`perl -pi -e 's/off/open/' .bravros/config.json`, ".bravros/config.json", ""},
		// Cuddled flag clusters and bare positional scripts (PR 94 review):
		// no exact `-e`/`-c` flag, yet the payload can shell out.
		{`perl -pe 'system("touch ~/.claude/state/police-token")' .bravros/config.json`, "~/.claude/state/police-token", ""},
		{`ruby -ne 'system("touch ~/.claude/state/police-token")' .bravros/config.json`, "~/.claude/state/police-token", ""},
		{`awk '{system("touch ~/.claude/state/police-token")}' .bravros/config.json`, "~/.claude/state/police-token", ""},
		// The redirect inside the payload is the write shape here — the printed
		// value carries no marker on purpose.
		{`awk 'BEGIN{print "minted" > ".planning/.review-stamp-12.json"}'`, ".planning/.review-stamp-12.json", ""},
		{`python3 -c "import os;os.system('rm .bravros/config.json')"`, ".bravros/config.json", ""},
		// Append and put_contents families (PR 94 review round 3): a token
		// file only has to EXIST with a fresh mtime to count as minted.
		{`node -e "require('fs').promises.appendFile('~/.claude/state/police-token','minted')"`, "~/.claude/state/police-token", ""},
		{`node -e "require('fs').appendFileSync('` + home + `/.claude/state/promote-token','{}')"`, home + "/.claude/state/promote-token", ""},
		{`php -r "file_put_contents('.bravros/config.json', '{}');"`, ".bravros/config.json", ""},
		{`php -r "file_put_contents('~/.claude/state/destructive-token', 'x');"`, "~/.claude/state/destructive-token", ""},
		// Delete family (round 4): removing the config widens the lane.
		{`node -e "require('fs').rmSync('.bravros/config.json')"`, ".bravros/config.json", ""},
		{`node -e "require('fs').promises.rm('.bravros/config.json')"`, ".bravros/config.json", ""},
		{`ruby -e "File.delete('.bravros/config.json')"`, ".bravros/config.json", ""},
		{`ruby -e "require 'fileutils'; FileUtils.rm('.bravros/config.json')"`, ".bravros/config.json", ""},
		{`python3 -c "import os;os.unlink('.bravros/config.json')"`, ".bravros/config.json", ""},
		{`php -r "unlink('.bravros/config.json');"`, ".bravros/config.json", ""},
		// Copy family (round 5) and the allowlist principle: an API nobody
		// listed is a write.
		{`node -e "require('fs').cpSync('/tmp/malicious.json','.bravros/config.json')"`, ".bravros/config.json", ""},
		{`node -e "require('fs').promises.cp('/tmp/m.json','.bravros/config.json')"`, ".bravros/config.json", ""},
		{`python3 -c "import shutil;shutil.copy2('/tmp/m.json','.bravros/config.json')"`, ".bravros/config.json", ""},
		{`python3 -c "import shutil;shutil.copytree('/tmp/d','.bravros/config.json')"`, ".bravros/config.json", ""},
		{`python3 -c "import pathlib;pathlib.Path('.bravros/config.json').something_new('{}')"`, ".bravros/config.json", ""},
		{`node -e "require('fs').newApiNobodyListed('~/.claude/state/police-token')"`, "~/.claude/state/police-token", ""},
		// awk in-place (round 6): gawk's inplace extension and the aliases.
		{`awk -i inplace '{sub(/off/,"open")}1' .bravros/config.json`, ".bravros/config.json", ""},
		{`awk -i inplace=.bak '{sub(/off/,"open")}1' .bravros/config.json`, ".bravros/config.json", ""},
		{`gawk -i inplace '{print}' .bravros/config.json`, ".bravros/config.json", ""},
		{`gawk --include=inplace '{print}' .bravros/config.json`, ".bravros/config.json", ""},
		{`mawk '{system("touch ~/.claude/state/police-token")}' .bravros/config.json`, "~/.claude/state/police-token", ""},
		// A write mode smuggled into an allowlisted read call (round 7).
		{`node -e "require('fs').readFileSync('~/.claude/state/police-token',{flag:'wx'})"`, "~/.claude/state/police-token", ""},
		{`node -e "require('fs').readFileSync('~/.claude/state/promote-token',{flag:'ax'})"`, "~/.claude/state/promote-token", ""},
		{`node -e "require('fs').readFile('~/.claude/state/destructive-token',{flag:'wx'},()=>{})"`, "~/.claude/state/destructive-token", ""},
		{`node -e "require('fs').promises.readFile('.planning/.review-stamp-12.json',{flag: \"ax\"})"`, ".planning/.review-stamp-12.json", ""},
		{`node -e "require('fs').readFileSync('.bravros/config.json',{flag:'r+'})"`, ".bravros/config.json", ""},
		{`python3 -c "open('.bravros/config.json','r+').read()"`, ".bravros/config.json", ""},
		{`ruby -e "File.open('.bravros/config.json', mode: 'a').read"`, ".bravros/config.json", ""},
		// Numeric and constant flag values (round 8): the argument is an allowlist.
		{`node -e "require('fs').readFileSync('~/.claude/state/police-token',{flag:65})"`, "~/.claude/state/police-token", ""},
		{`node -e "require('fs').readFileSync('~/.claude/state/promote-token',{flag:0x601})"`, "~/.claude/state/promote-token", ""},
		{`node -e "const fs=require('fs');fs.readFileSync('~/.claude/state/destructive-token',{flag:fs.constants.O_CREAT})"`, "~/.claude/state/destructive-token", ""},
		{`node -e "require('fs').promises.readFile('.bravros/config.json',{flag:fs.constants.O_CREAT|fs.constants.O_WRONLY})"`, ".bravros/config.json", ""},
		{`node -e "const f='wx';require('fs').readFileSync('.bravros/config.json',{flag:f})"`, ".bravros/config.json", ""},
		{`python3 -c "open('.bravros/config.json','w').write('{}')"`, ".bravros/config.json", ""},
		{`python3 - <<'EOF'` + "\nopen('.bravros/config.json','w').write('{}')\nEOF", ".bravros/config.json", ""},
		{`node -e "require('fs').writeFileSync('.bravros/config.json','{}')"`, ".bravros/config.json", ""},
		{`ruby -e "File.write('.bravros/config.json','{}')"`, ".bravros/config.json", ""},
		{`install -m 644 /tmp/cfg.json .bravros/config.json`, ".bravros/config.json", ""},
		{`rsync /tmp/cfg.json .bravros/config.json`, ".bravros/config.json", ""},
		{`git add . && printf '{}' > .bravros/config.json && git commit -m x`, ".bravros/config.json", ""},
		{`cd /tmp && echo x > ` + home + `/.claude/state/review-stamp-token`, home + "/.claude/state/review-stamp-token", ""},
	}
	for _, tc := range denied {
		msg := checkGateInputWrite(tc.cmd)
		if msg == "" {
			t.Errorf("%q: must be denied", tc.cmd)
			continue
		}
		if !strings.HasPrefix(msg, "✋🏽 Police Block: this command writes the gate's own input ("+tc.path+")") {
			t.Errorf("%q: block must name the path %q: %q", tc.cmd, tc.path, msg)
		}
		if tc.hint != "" && !strings.Contains(msg, tc.hint) {
			t.Errorf("%q: block missing hint %q: %q", tc.cmd, tc.hint, msg)
		}
		if !strings.Contains(msg, "safety floor") {
			t.Errorf("%q: block must declare the floor: %q", tc.cmd, msg)
		}
		if blocked, _, reason := rule52TestInvoke(t, tc.cmd); !blocked || !strings.Contains(reason, "gate's own input") {
			t.Errorf("%q: hook must deny: blocked=%v %q", tc.cmd, blocked, reason)
		}
	}

	allowed := []string{
		// Interpreter READS — blocked on v2.21.0's first day for merely naming
		// the state dir; an agent must be able to inspect setup.json.
		`python3 -c "import json;print(json.load(open('` + home + `/.claude/state/setup.json')).get('components'))"`,
		`python3 -c "import json,sys;print(json.load(open('.bravros/config.json'))['staging_branch'])"`,
		`node -e "console.log(require('fs').readFileSync('.planning/.review-stamp-12.json','utf8'))"`,
		`ruby -e "puts File.read('.bravros/config.json')"`,
		`perl -pe 's/off/open/' .bravros/config.json > /tmp/x`,
		`gawk '{print $1}' .bravros/config.json`,
		`node -e "console.log(require('fs').readFileSync('.bravros/config.json',{encoding:'utf8',flag:'r'}))"`,
		`node -e "console.log(require('fs').readFileSync('.bravros/config.json',{flag:0}))"`,
		`node -e "console.log(require('fs').readFileSync('.bravros/config.json',{flag:\"rs\"}))"`,
		`python3 -c "import pathlib;print(pathlib.Path('.bravros/config.json').read_text())"`,
		// Reads.
		`cat ~/.claude/state/police-token`,
		`cat ` + home + `/.claude/state/promote-token`,
		`ls -la ~/.claude/state`,
		`ls ~/.claude/state/`,
		`stat ~/.claude/state/promote-token`,
		`test -f ~/.claude/state/police-token && echo present`,
		`[ -f .planning/.review-stamp-12.json ] && cat .planning/.review-stamp-12.json`,
		`grep -o '"commit_sha": *"[^"]*"' .planning/.review-stamp-12.json | cut -d'"' -f4`,
		`jq .staging_branch .bravros/config.json`,
		`cat .bravros/config.json`,
		`cat .bravros/config.json > /tmp/backup.json`,
		`cp .bravros/config.json /tmp/backup.json`,
		`sed -n 1,5p .bravros/config.json`,
		`sed -e 's/x/y/' .bravros/config.json`,
		`diff .bravros/config.json /tmp/other.json`,
		`git diff .bravros/config.json`,
		`git add .bravros/config.json && git commit -m "config: lane reviewed"`,
		`git show HEAD:.bravros/config.json`,
		`cd ~/.claude/state && ls`,
		// Sanctioned verbs.
		`bravros police status`,
		`bravros promote status`,
		`bravros police revoke`,
		`bravros promote revoke`,
		`bravros police direct-main on`,
		`bravros police direct-main status`,
		`bravros pr-review 12 --write-stamp`,
		`bravros init`,
		`bravros config get staging_branch`,
		`bravros police status 2>/dev/null; echo rc=$?`,
		// Revocation is not a write.
		`rm -f .planning/.review-stamp-12.json`,
		`rm ~/.claude/state/police-token`,
		`for s in .planning/.review-stamp-*.json; do [ -e "$s" ] || continue; rm -f "$s"; done`,
		// Data arguments that merely mention a gate file.
		`git commit -m "fix police-token docs"`,
		`git commit -am "touch police-token before merge"`,
		`git commit -m"police-token: document .bravros/config.json"`,
		`git commit --message="stamp: .planning/.review-stamp-5.json"`,
		`gh pr create --title "police-token" --body "writes .bravros/config.json via the Write tool"`,
		`gh pr comment 5 -b "see ~/.claude/state/police-token"`,
		`echo "see ~/.claude/state/police-token"`,
		`echo "run: touch ~/.claude/state/police-token"`,
		`STAMP=".planning/.review-stamp-12.json"; [ -f "$STAMP" ] && echo stale`,
		// Unrelated files under similar names.
		`touch ~/.claude/state-notes.md`,
		`echo x > /tmp/state/police-token.md`,
		`printf x > .bravros/config.json.example`,
		`touch .planning/review-stamps.md`,
		`echo x > config.json`,
	}
	for _, cmd := range allowed {
		if msg := checkGateInputWrite(cmd); msg != "" {
			t.Errorf("%q: must pass, got %q", cmd, msg)
		}
	}

	// The floor: neither stand-down nor a valid merge token lifts it.
	t.Setenv("BRAVROS_POLICE_STANDDOWN", "1")
	if blocked, _, _ := rule52TestInvoke(t, `touch ~/.claude/state/police-token`); !blocked {
		t.Error("stand-down must not suppress a gate-input write")
	}
	t.Setenv("BRAVROS_POLICE_STANDDOWN", "")
	if _, err := (token.Gate{Name: "promote"}).Mint(5, ""); err != nil {
		t.Fatal(err)
	}
	if blocked, _, _ := rule52TestInvoke(t, `printf '{}' > .planning/.review-stamp-5.json`); !blocked {
		t.Error("a merge token must not authorise forging a review stamp")
	}
}

func TestPoliceStatus_ReportsBothTokens(t *testing.T) {
	rule52TestSetEnv(t)
	status := func() string {
		var out bytes.Buffer
		policeStatusCmd.SetOut(&out)
		if err := policeStatusCmd.RunE(policeStatusCmd, nil); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if out := status(); !strings.Contains(out, "Police token is MISSING or INVALID") || !strings.Contains(out, "Promote token is MISSING or INVALID") {
		t.Errorf("both tokens reported missing: %q", out)
	}
	if _, err := (token.Gate{Name: "promote"}).Mint(5, ""); err != nil {
		t.Fatal(err)
	}
	if out := status(); !strings.Contains(out, "Promote token is VALID (expires ") || !strings.Contains(out, "Police token is MISSING") {
		t.Errorf("promote valid, police missing: %q", out)
	}
}
