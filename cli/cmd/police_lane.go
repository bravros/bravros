package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bravros/bravros/cli/internal/config"
	"github.com/bravros/bravros/cli/internal/token"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// The staging lane — tokenless `gh pr merge <N>` for <staging> → main
//
// B-0036 made every server-side merge into a protected branch demand an
// out-of-band token. That closed the incident shape (an agent shipping an
// unreviewed homolog→main PR on its own) but also blocked the two flows that
// legitimately merge to main from inside a session — /finish Step 7 and
// /hotfix — costing a token mint per merge (F5 in the design of record).
//
// The lane is a deliberate, bounded relaxation: a same-repo `gh pr merge <N>`
// whose head IS the repo's staging branch, whose base is protected, whose
// mergeability GitHub reports as CLEAN, and which runs with no autonomous
// pipeline lock present, is allowed without a token and written to an audit
// log. Everything else — `gh api` merge routes, GraphQL, git push, plumbing,
// raw HTTP, -R/URL/branch forms, relocated commands — is unchanged.
//
// Honest note: in the default "open" mode the B-0036 command shape itself
// (interactive session, CLEAN homolog→main PR, no lock) IS allowed. What
// remains is fail-closed lookups, repo scoping, the autonomous lock, and the
// audit log. "reviewed" mode closes the shape again at the cost of a token for
// /hotfix. See docs/cli/police.md § The staging lane.
// ---------------------------------------------------------------------------

// prFacts is what one `gh pr view --json …` answers about the PR being merged.
type prFacts struct {
	Number         int    `json:"number"`
	Base           string `json:"baseRefName"`
	Head           string `json:"headRefName"`
	MergeState     string `json:"mergeStateStatus"`
	ReviewDecision string `json:"reviewDecision"`
	HeadOID        string `json:"headRefOid"`
	CrossRepo      bool   `json:"isCrossRepository"`
}

// prLookupTimeout bounds the forge round-trip. A variable so a test can shrink
// it and prove the hang path returns.
var prLookupTimeout = 5 * time.Second

// prLookupFields is the single --json field list the gate asks for. One call
// answers both "is the base protected" and every lane condition, so the lane
// adds no forge round-trip to the merge path (F4).
const prLookupFields = "number,baseRefName,headRefName,mergeStateStatus,reviewDecision,headRefOid,isCrossRepository"

// lookupPR asks the forge about the PR being merged. ok is false when the
// lookup could not answer (offline, unauthenticated, no such PR, unparseable
// output) — callers fail CLOSED on that, never open.
//
// repo is forwarded as --repo so the question is asked about the PR actually
// being merged, not whatever repo the shell happens to be in (B-0036). pr may
// be a number, a URL or a branch name; "" asks about the current branch's PR.
func lookupPR(repo, pr string) (prFacts, bool) {
	args := []string{"pr", "view", "--json", prLookupFields}
	if repo != "" {
		args = append(args, "--repo", repo)
	}
	if pr != "" {
		args = append(args, pr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), prLookupTimeout)
	defer cancel()

	c := exec.CommandContext(ctx, "gh", args...)
	// Output() waits for the stdout pipe to close, not just for gh to die. A
	// hung gh whose child (a pager, a credential helper, a sleeping shim) still
	// holds the pipe kept the hook — and every Bash call behind it — waiting
	// long after the context killed the parent. WaitDelay closes the pipe.
	c.WaitDelay = time.Second
	out, err := c.Output()
	if err != nil {
		return prFacts{}, false
	}
	var f prFacts
	if err := json.Unmarshal(out, &f); err != nil {
		return prFacts{}, false
	}
	if strings.TrimSpace(f.Base) == "" {
		return prFacts{}, false
	}
	return f, true
}

// laneRefusedPrefix marks a mergeProtected detail whose PR was a lane
// candidate that failed one condition. mergeBlockMessage keys its wording on
// it so the block names the condition instead of the generic "open a PR from
// <staging>" advice, which would be nonsense for a PR that already is one.
const laneRefusedPrefix = "staging lane refused: "

// laneUnknownDetail is the indeterminate detail for a lane candidate whose
// mergeability GitHub has not computed yet — common right after `gh pr create`.
// Its fix is "wait and re-run", not "check connectivity", so the block message
// keys on it.
const laneUnknownDetail = "merging a pull request whose mergeStateStatus is UNKNOWN — GitHub has not computed mergeability yet; re-run in a few seconds"

// laneEligible reports whether the command shape may use the lane at all: no
// -R/--repo, and the PR argument is a plain number or absent (the current
// branch's PR). URL and branch forms keep the token path — they can name a PR
// in another repo, and the lane is a same-repo permission.
func laneEligible(repo, pr string) bool {
	if repo != "" {
		return false
	}
	if pr == "" {
		return true
	}
	for _, r := range pr {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// stagingLaneVerdict decides the lane for a PR whose base is already known to
// be protected and whose command shape passed laneEligible.
//
// It returns exactly one of:
//   - mergeStagingLane with an audit summary ("pr=N head->base mode=open"),
//   - mergeIndeterminate with laneUnknownDetail (UNKNOWN mergeability),
//   - mergeProtected with laneRefusedPrefix + the failed condition.
//
// Conditions are checked cheapest-first and the FIRST failure is reported, so
// the block names one concrete thing to fix.
func stagingLaneVerdict(pr string, f prFacts) (mergeVerdict, string) {
	cfg, _ := config.LoadBravrosConfig() // found=false still yields the homolog default
	staging := cfg.StagingBranch
	if staging == "" {
		staging = "homolog"
	}
	mode, modeReason := cfg.StagingLaneMode()
	refused := func(why string) (mergeVerdict, string) {
		return mergeProtected, laneRefusedPrefix + why
	}

	if mode == config.StagingLaneOff {
		if modeReason != "" {
			return refused(modeReason + "; every merge into a protected branch needs a token")
		}
		return refused(`police.staging_lane is "off" in .bravros/config.json; every merge into a protected branch needs a token`)
	}
	if f.Head != staging {
		return refused(fmt.Sprintf("head branch '%s' is not the staging branch '%s'; the lane accepts only %s → %s", f.Head, staging, staging, f.Base))
	}
	if f.CrossRepo {
		return refused("the PR head lives in another repository (isCrossRepository); the lane is a same-repo permission")
	}
	if lock := autoprLockPresent(); lock != "" {
		return refused(fmt.Sprintf("an autonomous lock is present (%s) — autonomous pipelines never merge to main; clear it from a separate terminal (bravros autopr clear-lock) or mint a token", lock))
	}
	switch f.MergeState {
	case "CLEAN":
	case "UNKNOWN", "":
		return mergeIndeterminate, laneUnknownDetail
	default:
		return refused(fmt.Sprintf("mergeStateStatus is %s (the lane needs CLEAN: %s); fix the PR and re-run, or mint a token", f.MergeState, mergeStateHint(f.MergeState)))
	}
	if mode == config.StagingLaneReviewed {
		prNum := pr
		if prNum == "" && f.Number > 0 {
			prNum = fmt.Sprint(f.Number)
		}
		if f.ReviewDecision != "APPROVED" && !reviewStampMatches(prNum, f.HeadOID) {
			return refused(fmt.Sprintf(`police.staging_lane is "reviewed": the PR needs reviewDecision APPROVED (it is %q) or a .planning/.review-stamp-%s.json whose commit_sha equals the PR head %s`, f.ReviewDecision, orPlaceholder(prNum, "<N>"), shortOID(f.HeadOID)))
		}
	}

	prLabel := pr
	if prLabel == "" && f.Number > 0 {
		prLabel = fmt.Sprint(f.Number)
	}
	return mergeStagingLane, fmt.Sprintf("pr=%s %s->%s mode=%s", orPlaceholder(prLabel, "current-branch"), f.Head, f.Base, mode)
}

func orPlaceholder(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func shortOID(oid string) string {
	if len(oid) > 12 {
		return oid[:12]
	}
	return oid
}

// mergeStateHint spells out what each non-CLEAN GitHub mergeStateStatus means
// so the block is actionable without a trip to the docs.
func mergeStateHint(state string) string {
	switch state {
	case "BLOCKED":
		return "a branch-protection rule blocks it — required review or status check missing"
	case "UNSTABLE":
		return "a status check is failing"
	case "DIRTY":
		return "the PR has merge conflicts"
	case "BEHIND":
		return "the head branch is behind the base; update it"
	case "DRAFT":
		return "the PR is a draft"
	case "HAS_HOOKS":
		return "a pre-receive hook must run first"
	}
	return "GitHub does not report it as mergeable"
}

// autoprLockPresent returns the repo-relative path (`.planning/.auto-pr-lock`)
// of the first `.planning/.auto-*-lock` file found (the /auto-pr family:
// .auto-pr-lock, .auto-merge-lock, …), or "" when none is present. It is
// repo-local by design — the lane already refuses -R and relocation, so cwd IS
// the repo the merge lands in. Any lock, this session's or another's, closes
// the lane: that is what makes "/auto-pr never merges to main" enforced rather
// than promised (F3).
//
// Repo-relative so the block message reads the same from any subdirectory and
// matches `bravros autopr clear-lock`'s own spelling.
func autoprLockPresent() string {
	matches, _ := filepath.Glob(filepath.Join(planningDir(), ".auto-*-lock"))
	if len(matches) == 0 {
		return ""
	}
	return filepath.Join(".planning", filepath.Base(matches[0]))
}

// planningDir resolves `.planning/` from the repo root when git can name it
// (the lane may be exercised from a subdirectory), else from cwd.
func planningDir() string {
	if root := gitIn("rev-parse", "--show-toplevel"); root != "" {
		return filepath.Join(root, ".planning")
	}
	return ".planning"
}

// reviewStampMatches reports whether .planning/.review-stamp-<pr>.json exists
// and its commit_sha equals headOID — the schema written by
// internal/github.WriteReviewStamp (`bravros pr-review --write-stamp`). A stale
// stamp (any other sha) is a review of a different commit and does not count.
func reviewStampMatches(pr, headOID string) bool {
	if pr == "" || headOID == "" {
		return false
	}
	data, err := os.ReadFile(filepath.Join(planningDir(), ".review-stamp-"+pr+".json"))
	if err != nil {
		return false
	}
	var stamp struct {
		CommitSHA string `json:"commit_sha"`
	}
	if json.Unmarshal(data, &stamp) != nil {
		return false
	}
	return stamp.CommitSHA != "" && strings.EqualFold(stamp.CommitSHA, headOID)
}

// laneAuditPath is where every tokenless lane merge is recorded. One line per
// merge, append-only, user-scoped like the token files.
func laneAuditPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".claude", "state", "police-merge-audit.log")
}

// auditLaneMerge appends `ts cwd pr head->base mode session_id` to the audit
// log. It never fails the command: the merge was already allowed, and a log
// write error must not turn an allowed command into a denied one.
func auditLaneMerge(summary string) {
	path := laneAuditPath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	cwd, _ := os.Getwd()
	session := resolveSession()
	if session == "" {
		session = "-"
	}
	fmt.Fprintf(f, "%s %s %s session=%s\n", time.Now().UTC().Format(time.RFC3339), cwd, summary, session)
}

// hasValidMergeToken reports whether either out-of-band human-presence token
// authorizes a merge: the police token (mtime-based, 10 min) or the promote
// token minted by `bravros promote unlock`. Valid() — not Present() — so the
// hot path never deletes a file.
func hasValidMergeToken() bool {
	return hasValidPoliceToken() || token.Gate{Name: "promote"}.Valid()
}

// ---------------------------------------------------------------------------
// `bravros police direct-main on|off|status`
//
// Writes the explicit opt-out key police.direct_main into .bravros/config.json.
// Deliberately NOT refused inside Claude Code: the file is agent-writable
// anyway, so a refusing verb would be theatre. The key is a commit-visible
// declaration against omission — the token is the only agent-proof boundary.
// ---------------------------------------------------------------------------

var policeDirectMainCmd = &cobra.Command{
	Use:   "direct-main on|off|status",
	Short: "Declare this repo direct-to-main (writes police.direct_main to .bravros/config.json)",
	Long: `Manage the explicit police.direct_main opt-out in .bravros/config.json.

  bravros police direct-main on      write {"police":{"direct_main":true}} (merging into any existing config)
  bravros police direct-main off     remove the key
  bravros police direct-main status  report on/off and whether a config file exists

The key is a commit-visible declaration that main is pushed directly by design
(personal repos, /git-this repos, workspace meta-repos). It is not an agent-proof
boundary — the file is writable by anyone in the repo — so commit it and review
it like code. Only the out-of-band token is agent-proof.`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"on", "off", "status"},
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "on":
			return directMainWrite(cmd, true)
		case "off":
			return directMainWrite(cmd, false)
		case "status":
			return directMainStatus(cmd)
		}
		return fmt.Errorf("police direct-main: want on|off|status, got %q", args[0])
	},
}

// directMainState reports whether a config file exists and whether it carries
// police.direct_main: true. Used by the block message to say which fix applies.
func directMainState() (found bool, key bool) {
	cfg, found := config.LoadBravrosConfig()
	return found, found && cfg.Police != nil && cfg.Police.DirectMain
}

// directMainWrite sets or clears police.direct_main, preserving every other key
// in the file. The document is decoded into a generic map so unknown keys
// survive; numbers keep their literal spelling via UseNumber. Key ORDER is not
// preserved (Go encodes map keys sorted) — the content is.
func directMainWrite(cmd *cobra.Command, on bool) error {
	// Triggers the legacy .bravros.yml → config.json migration when needed, so
	// the file below is the canonical one.
	config.LoadBravrosConfig()

	doc := map[string]any{}
	existed := false
	if data, err := os.ReadFile(config.ConfigFilename); err == nil {
		existed = true
		dec := json.NewDecoder(strings.NewReader(string(data)))
		dec.UseNumber()
		if err := dec.Decode(&doc); err != nil {
			return fmt.Errorf("police direct-main: %s is not valid JSON: %w", config.ConfigFilename, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("police direct-main: read %s: %w", config.ConfigFilename, err)
	}

	police, _ := doc["police"].(map[string]any)
	if police == nil {
		police = map[string]any{}
	}
	if on {
		police["direct_main"] = true
		doc["police"] = police
	} else {
		delete(police, "direct_main")
		if len(police) == 0 {
			delete(doc, "police")
		} else {
			doc["police"] = police
		}
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if err := os.MkdirAll(filepath.Dir(config.ConfigFilename), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(config.ConfigFilename, out, 0o644); err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	switch {
	case on && !existed:
		fmt.Fprintf(w, "✓ created %s with police.direct_main: true\n", config.ConfigFilename)
	case on:
		fmt.Fprintf(w, "✓ set police.direct_main: true in %s (other keys preserved)\n", config.ConfigFilename)
	default:
		fmt.Fprintf(w, "✓ removed police.direct_main from %s — main is protected again\n", config.ConfigFilename)
	}
	fmt.Fprintf(w, "%s", out)
	fmt.Fprintln(w, "Commit it: the key is a commit-visible declaration, not an agent-proof boundary (only the token is).")
	return nil
}

func directMainStatus(cmd *cobra.Command) error {
	found, key := directMainState()
	w := cmd.OutOrStdout()
	state := "off"
	if key {
		state = "on"
	}
	fmt.Fprintf(w, "police.direct_main: %s\n", state)
	if found {
		fmt.Fprintf(w, "config: %s present\n", config.ConfigFilename)
	} else {
		fmt.Fprintf(w, "config: %s absent (main is protected by default)\n", config.ConfigFilename)
	}
	cfg, _ := config.LoadBravrosConfig()
	mode, reason := cfg.StagingLaneMode()
	fmt.Fprintf(w, "police.staging_lane: %s (staging branch %s)\n", mode, cfg.StagingBranch)
	if reason != "" {
		fmt.Fprintf(w, "  %s\n", reason)
	}
	return nil
}

func init() {
	policeCmd.AddCommand(policeDirectMainCmd)
}
