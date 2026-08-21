package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bravros/bravros/cli/internal/config"
	"github.com/bravros/bravros/cli/internal/deploy"
	"github.com/bravros/bravros/cli/internal/fetch"
	"github.com/bravros/bravros/cli/internal/hooks"
	"github.com/bravros/bravros/cli/internal/nowtime"
	"github.com/bravros/bravros/cli/internal/payload"
	"github.com/bravros/bravros/cli/internal/selfupdate"
	"github.com/spf13/cobra"
)

var (
	selfupdateSilent       bool
	selfupdateVerbose      bool
	selfupdateSkipIfRecent string
	selfupdateDryRun       bool
	selfupdateDeep         bool
	selfupdateForce        bool
	selfupdateFetchPayload bool
)

var selfupdateCmd = &cobra.Command{
	Use:   "selfupdate",
	Short: "Refresh installed components from this binary's embedded payload",
	Long: `Refreshes ~/.claude from the payload EMBEDDED in this binary — a local file copy,
no network, no source checkout, nothing that can leave the machine half-updated.
This is the automatic half of the split update model (P-0015 D2): the risk-free
work stays on the SessionStart hook, and replacing the binary itself became an
explicit verb, 'bravros update'.

What it refreshes is whatever <config dir>/state/setup.json records — the choice
'bravros setup' wrote. An install predating that file (no setup.json, but a
populated ~/.claude/skills) is refreshed at scope 'all', which is exactly what
such a machine already had: defaulting to 'core' there would silently delete
every non-core skill the user was relying on.

Nothing is ever overwritten: a file that exists and differs is left alone and the
payload's version is written beside it as <name>.new. Skills the user added by
hand are never touched, and pruning stays scoped to skills/ + templates/.

At most once every 24h it also checks whether a newer bravros was published.
On a binary install.sh owns (install_method "installer" in setup.json) that check
also INSTALLS it: the previous executable is kept beside the new one as
bravros.prev, and the swap announces itself with one line. Everywhere else —
brew, scoop, a locally built binary — the check only prints the notice and never
replaces anything.

Releases younger than 6h are deferred to the next check, so a release yanked in
its first hours never reaches the fleet (BRAVROS_MIN_RELEASE_AGE overrides the
window; "0" disables it). BRAVROS_NO_UPDATE_CHECK=1 turns the whole lane off, as
does "auto_update": false in setup.json, which leaves the notice in place. The
check never blocks and never fails the run.

The whole run is cached: after each completed run the marker
~/.claude/state/.bravros-last-check is stamped, and a run within the TTL (default
6h, override via BRAVROS_SELFUPDATE_TTL, "0" disables) returns immediately. --force
bypasses the cache.

--fetch-payload selects the legacy network lane instead: resolve the newest
release, download and minisign-verify bravros-payload.tar.gz, and deploy it. It is
opt-in because D2 moved the SessionStart hook off the network entirely.

When the auto-update lane swaps the binary, it fires an optional operator-supplied
notifier (P-0020). Three setup.json fields govern this: announce_command (path to
notifier executable; empty/absent disables), announce_template (message template with
{version} placeholder; overrides language-picked template if set), and announce_language
(en or pt-BR; defaults to en when announce_template is empty). The announce lane is off
unless announce_command is set. The notifier is invoked fire-and-forget as
<announce_command> --force <message> studio, with failures silent and non-fatal.
BRAVROS_ANNOUNCE_CMD env var overrides announce_command for testing.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}

		// B-0345: TTL cache, and it stays in FRONT of everything. It is what
		// keeps the SessionStart hook near-free — a cache-hit session does no
		// remote check, no embed walk, and no file I/O beyond one stat. The
		// marker is touched after every COMPLETED run, so unlike
		// --skip-if-recent (whose marker moves only on real installs) the TTL
		// bounds how often the work runs, not how often installs happen.
		// BRAVROS_SELFUPDATE_TTL overrides the 6h default; "0" disables caching.
		//
		// Only --force bypasses it now. It used to be bypassed by `bravros
		// update` as well, because that alias was the manual "really check now"
		// entry point; P-0015 D2 gave `update` an independent meaning (network
		// fetch + binary self-replace, cmd/update.go), and that command drops
		// this marker itself after a successful swap so the very next session
		// refreshes against the new embed.
		checkTTL := selfupdateCheckTTL()
		forceCheck := selfupdateForce
		if checkTTL > 0 && !forceCheck && !selfupdateDryRun {
			markerFile := filepath.Join(legacyClaudeStateDir(home), selfupdateCheckMarker)
			if info, err := os.Stat(markerFile); err == nil {
				if elapsed := time.Since(info.ModTime()); elapsed < checkTTL {
					if selfupdateVerbose {
						fmt.Fprintf(os.Stderr, "ℹ️  selfupdate checked %v ago (TTL %v) — skipping; use --force to check now\n", elapsed.Round(time.Second), checkTTL)
					}
					return nil
				}
			}
		}

		// Check skip-if-recent condition
		if selfupdateSkipIfRecent != "" {
			dur, err := time.ParseDuration(selfupdateSkipIfRecent)
			if err == nil {
				stateDir := filepath.Join(home, ".claude", "state")
				markerFile := filepath.Join(stateDir, ".bravros-last-update")
				if info, err := os.Stat(markerFile); err == nil {
					elapsed := time.Since(info.ModTime())
					if elapsed < dur {
						if selfupdateVerbose {
							fmt.Fprintf(os.Stderr, "ℹ️  SDLC updated %v ago (within %v, skipping)\n", elapsed.Round(time.Second), dur)
						}
						return nil
					}
				}
			}
		}

		// The legacy network lane, opt-in since D2 (see --fetch-payload).
		if selfupdateFetchPayload {
			return selfupdateViaFetch(home)
		}

		// The D2 SessionStart lane: local refresh from the embedded payload,
		// then at most a one-line "newer version available" notice.
		return selfupdateRefreshLane(home)
	},
}

// selfupdateRefreshLane is what SessionStart runs post-D2: refresh ~/.claude
// from THIS binary's embedded payload (zero network), re-check commit-msg hook
// drift against the freshly deployed canonical, stamp the TTL marker, and only
// then consider the passive version notice.
//
// Ordering matters. The refresh runs before detectHookDrift because the
// canonical hook it compares against (~/.claude/templates/.githooks/commit-msg)
// is a file this very refresh may have just created; and the notice runs last
// because it is the only step that can touch the network, so nothing local
// waits on it.
func selfupdateRefreshLane(home string) error {
	root := config.ConfigDir()

	outcome, err := selfupdateRefreshFromEmbed(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  bravros: refresh from embedded payload failed: %v\n", err)
		return err
	}
	selfupdateReportRefresh(outcome)

	if cwd, cwdErr := os.Getwd(); cwdErr == nil {
		applyHookDrift(home, detectHookDrift(cwd, home))
	}

	if !outcome.Skipped {
		// Sweep any executable a previous `bravros update` had to leave
		// behind (Windows cannot delete a running .exe; it renames it and
		// the next run cleans up). Best effort, one ReadDir of ~/.claude/bin.
		if !selfupdateDryRun {
			selfupdate.CleanupOldBinaries(filepath.Join(root, "bin"))
		}
	}

	touchSelfupdateCheckMarker(home)
	selfupdatePassiveNotice()
	return nil
}

// selfupdateReportRefresh prints the refresh outcome. Silence is the contract
// for the common case: a SessionStart run that changed nothing says nothing.
func selfupdateReportRefresh(o embedRefreshOutcome) {
	if o.Skipped {
		if selfupdateVerbose {
			fmt.Fprintf(os.Stderr, "ℹ️  bravros: nothing installed at %s yet — run `bravros setup` to choose components\n", o.Root)
		}
		return
	}
	res := o.Result
	if res == nil {
		return
	}
	changed := res.Created + len(res.Conflicts) + len(res.Pruned)
	switch {
	case selfupdateDryRun:
		fmt.Fprintf(os.Stderr, "ℹ️  bravros (dry run): %d file(s) would be written, %d already current\n", res.Created, res.Unchanged)
	case changed > 0:
		fmt.Fprintf(os.Stderr, "✨ bravros refreshed %s from the embedded payload (%d written, %d unchanged, %d kept as .new)\n",
			o.Root, res.Created, res.Unchanged, len(res.Conflicts))
	case selfupdateVerbose:
		fmt.Fprintf(os.Stderr, "ℹ️  bravros: %s already matches the embedded payload (%d file(s) verified)\n", o.Root, res.Unchanged)
	}
	for _, rel := range res.Conflicts {
		fmt.Fprintf(os.Stderr, "  kept your %s — new version at %s.new\n", rel, rel)
	}
	if selfupdateVerbose {
		for _, w := range o.Warnings {
			fmt.Fprintln(os.Stderr, "  ⚠️  "+w)
		}
	}
}

// selfupdateNoticeTimeout is the hard ceiling on the passive check. A session
// start must never wait on GitHub; three seconds, at most once every 24h, is
// the entire network budget of this lane.
const selfupdateNoticeTimeout = 3 * time.Second

// selfupdateAutoTimeout bounds the auto-update swap: the canary HEAD plus the
// download, verify and replace. It is deliberately far larger than
// selfupdateNoticeTimeout — by the time it applies, the 24h check has already
// concluded that this machine is behind and owns its own binary, so the work is
// worth waiting for. It is still bounded, because a hung download must not hold
// a session start open indefinitely.
const selfupdateAutoTimeout = 120 * time.Second

// Test seams for the version lane. Production leaves both nil.
var (
	// selfupdateNoticeResolverOverride replaces the real tag resolver in tests,
	// so the request count is observable against an httptest server.
	selfupdateNoticeResolverOverride selfupdate.TagResolver
	// selfupdateAgerOverride replaces the release-age probe, so the canary can
	// be driven to both verdicts without publishing a release.
	selfupdateAgerOverride selfupdate.ReleaseAger
)

// selfupdatePassiveNotice runs the version lane at the end of a SessionStart
// refresh: at most one remote check per notice interval (24h default), then
// either the one-line notice this lane has always printed, or — on a binary
// install.sh owns — the auto-update swap D2 turned that check into.
//
// EVERY FAILURE IS NON-FATAL AND SILENT. This function returns nothing and
// cannot fail a run: the SessionStart hook's contract is that a session starts
// even when GitHub is down, the release is broken, or the binary is not
// replaceable.
func selfupdatePassiveNotice() {
	if selfupdateDryRun || selfupdate.NoticeDisabled() {
		return
	}
	resolver := selfupdateNoticeResolverOverride
	if resolver == nil {
		resolver = fetch.NewClient()
	}
	ctx, cancel := context.WithTimeout(context.Background(), selfupdateNoticeTimeout)
	defer cancel()

	res := selfupdate.PassiveCheckDetail(ctx, resolver, selfupdate.NoticeStatePath(), selfupdate.NoticeInterval(), Version)
	cancel() // the notice's 3s budget covers the check only, not the swap

	// A cache hit prints the cached notice and does nothing else. Gating the
	// swap on Checked is what keeps the cadence at 24h: a release deferred by
	// the canary is retried at the next real check, not on every session.
	if !res.Checked || res.LatestTag == "" {
		selfupdatePrintNotice(res.Line)
		return
	}
	if selfupdateAutoUpdate(res.LatestTag) {
		return // the swap printed its own single line
	}
	selfupdatePrintNotice(res.Line)
}

func selfupdatePrintNotice(line string) {
	if line != "" {
		fmt.Fprintln(os.Stderr, line)
	}
}

// selfupdateAutoUpdate is the trigger half of D2: given the tag the 24h check
// just resolved, decide whether this machine may replace its own binary, and do
// it. Reports whether a swap happened — false means the caller prints the plain
// notice instead, which is the pre-D2 behavior every non-installer machine
// keeps.
//
// The download/verify/replace machinery is `bravros update`'s, reused verbatim
// (updateUpdater, updateDropCheckMarker, updateRefreshComponents): the trust
// chain — minisign-signed checksums.txt, sha256 of the archive, extract, atomic
// rename — has exactly one implementation, and an automatic swap is the last
// place to grow a second one.
func selfupdateAutoUpdate(tag string) bool {
	exePath, err := updateExecutablePath()
	if err != nil {
		return false
	}
	root := config.ConfigDir()

	decision := selfupdate.DecideAuto(selfupdate.AutoInput{
		CurrentVersion: Version,
		LatestTag:      tag,
		ObservedMethod: detectInstallMethod(exePath, root),
		RecordedMethod: selfupdateRecordedInstallMethod(root),
		AutoUpdate:     selfupdateAutoUpdatePreference(root),
	})
	if decision.Action != selfupdate.AutoSwap {
		selfupdateTraceAuto(decision)
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), selfupdateAutoTimeout)
	defer cancel()

	// The canary comes after the ownership gates and before the download, so a
	// machine that may not swap never pays for the probe.
	age, ageErr := selfupdateReleaseAger().ReleaseAge(ctx, tag)
	if decision = selfupdate.CanaryVerdict(age, ageErr, selfupdate.ReleaseAgeFloor()); decision.Action != selfupdate.AutoSwap {
		selfupdateTraceAuto(decision)
		return false
	}

	// One generation of rollback, in place before anything is replaced.
	if _, err := selfupdate.PreservePrevious(exePath); err != nil {
		selfupdateTraceAuto(selfupdate.AutoDecision{Action: selfupdate.AutoNotify, Reason: err.Error()})
		return false
	}

	res, err := updateUpdater().Install(ctx, tag, exePath)
	if err != nil {
		// A failed download leaves the binary untouched (binary.go rolls back);
		// fall back to the notice so the operator can retry by hand.
		selfupdateTraceAuto(selfupdate.AutoDecision{Action: selfupdate.AutoNotify, Reason: err.Error()})
		return false
	}

	fmt.Fprintln(os.Stderr, selfupdate.SwapLine(Version, tag))

	selfupdateAnnounce(root, tag)

	// The new binary carries a new embedded payload, and the TTL marker would
	// otherwise suppress the refresh for up to 6h. Drop it, then refresh from
	// the NEW binary's embed — this process still holds the old one.
	updateDropCheckMarker()
	if err := updateRefreshComponents(res.ExePath); err != nil && selfupdateVerbose {
		fmt.Fprintf(os.Stderr, "  ⚠️  component refresh failed: %v — run `bravros selfupdate --force`\n", err)
	}
	return true
}

// selfupdateTraceAuto explains a non-swap under --verbose and stays silent
// otherwise. The reason is never printed by default: on every brew, scoop and
// source machine this lane declines on every single check, and a hook that
// narrates its own inaction is noise.
func selfupdateTraceAuto(d selfupdate.AutoDecision) {
	if selfupdateVerbose {
		fmt.Fprintf(os.Stderr, "ℹ️  bravros auto-update: %s (%s)\n", d.Action, d.Reason)
	}
}

func selfupdateReleaseAger() selfupdate.ReleaseAger {
	if selfupdateAgerOverride != nil {
		return selfupdateAgerOverride
	}
	return &selfupdate.AssetAger{}
}

// selfupdateAutoState is a deliberately minimal, forward-compatible view of
// setup.json. It decodes ONLY the two fields the auto lane reads; every other
// key — including any this file has never heard of — is ignored by
// encoding/json, so it cannot break when the canonical record grows.
//
// It exists instead of readSetupState because `auto_update` is not a field of
// setupState (cmd/setup.go, owned elsewhere): an opt-out an operator wrote must
// be honoured whether or not the struct has caught up. Decoding two named keys
// is cheaper than coupling this lane to that struct's schedule.
type selfupdateAutoState struct {
	InstallMethod string `json:"install_method"`
	AutoUpdate    *bool  `json:"auto_update"`
}

func selfupdateReadAutoState(root string) selfupdateAutoState {
	var st selfupdateAutoState
	data, err := os.ReadFile(setupStatePath(root))
	if err != nil {
		return st
	}
	// Unparsable state is treated as absent: the ownership gates below then
	// decide on the observed path alone, and a corrupt file can never read as
	// "auto_update: true".
	_ = json.Unmarshal(data, &st)
	return st
}

// selfupdateRecordedInstallMethod is setup.json's install_method ("" when the
// file is missing, unreadable or predates the field).
func selfupdateRecordedInstallMethod(root string) string {
	return selfupdateReadAutoState(root).InstallMethod
}

// selfupdateAutoUpdatePreference is setup.json's auto_update field: nil when
// absent (default ON for installer-owned binaries, per D2), false to switch the
// whole auto lane back to notify-only.
func selfupdateAutoUpdatePreference(root string) *bool {
	return selfupdateReadAutoState(root).AutoUpdate
}

// selfupdateAnnounceStateFields is a minimal, forward-compatible view of
// setup.json decoding only the three announce fields — same rationale as
// selfupdateAutoState: this lane must not couple to setupState's schedule.
type selfupdateAnnounceStateFields struct {
	AnnounceCommand  string `json:"announce_command"`
	AnnounceTemplate string `json:"announce_template"`
	AnnounceLanguage string `json:"announce_language"`
}

// selfupdateAnnounceState reads the three announce fields from setup.json.
// Missing or unparsable state reads as all-empty, exactly like
// selfupdateReadAutoState.
func selfupdateAnnounceState(root string) selfupdateAnnounceStateFields {
	var st selfupdateAnnounceStateFields
	data, err := os.ReadFile(setupStatePath(root))
	if err != nil {
		return st
	}
	_ = json.Unmarshal(data, &st)
	return st
}

// selfupdateAnnounce fires a fire-and-forget notifier command telling the
// operator an unattended swap just happened. It never blocks the SessionStart
// hook: the command is started and immediately released, its stdout/stderr
// discarded, and every error — resolving the command, running it, anything —
// is silently swallowed. A missing command (env var AND setup.json both
// empty) is a silent no-op, not an error.
func selfupdateAnnounce(root, tag string) {
	command := strings.TrimSpace(os.Getenv("BRAVROS_ANNOUNCE_CMD"))
	st := selfupdateAnnounceState(root)
	if command == "" {
		command = strings.TrimSpace(st.AnnounceCommand)
	}
	if command == "" {
		return
	}
	command = selfupdate.ExpandHome(command)

	message := selfupdate.RenderAnnouncement(st.AnnounceTemplate, st.AnnounceLanguage, tag)

	// argv, never a shell string: the message is one argument, so it can
	// never be reinterpreted by a shell no matter what it contains.
	cmd := exec.Command(command, "--force", message, "studio")
	if err := cmd.Start(); err != nil {
		return
	}
	_ = cmd.Process.Release()
}

// ─── embedded-payload refresh (D2's local half) ─────────────────────────────

// embedRefreshOutcome is what a refresh actually did — exit codes prove nothing
// here (this command returns nil on almost every path, including "did
// nothing"), so callers and tests read this and the filesystem instead.
type embedRefreshOutcome struct {
	Root string
	// Skipped is true when there is nothing to refresh: no setup.json AND no
	// pre-v2 install under Root. Installing components nobody asked for is
	// `bravros setup`'s job, never a hook's.
	Skipped bool
	// Migrated is true when a pre-v2 install (no setup.json) was refreshed.
	Migrated bool
	Scope    payload.SkillScope
	Result   *setupApplyResult
	Warnings []string
}

// selfupdateRefreshFromEmbed re-materialises the recorded component selection
// from THIS binary's embedded payload.
//
// It reuses the Phase 3 planner (setupBuildPlan/setupApply) rather than
// deploy.Deploy on purpose: deploy's per-skill step is an atomic wipe-and-
// recopy, so a hand-edited SKILL.md would vanish without a word. The planner
// compares byte-for-byte, writes a differing file as <name>.new, and confines
// removals to skills recorded by a previous `setup` that are still identical to
// the embed. That is the same preservation guarantee FilterMode gave the deploy
// lane (cmd/selfupdate.go's fetch path passes FilterMode: true for exactly this
// reason) — an allowlist that is additive, never a prune instruction. Skills a
// user added by hand are not in any recorded selection and can never be removed.
//
// MIGRATION (D11), and it is the data-loss-shaped decision of this phase: an
// install predating `bravros setup` has no setup.json. It gets scope `all`,
// because that is what today's deploy-everything behavior already put on that
// machine. Defaulting to `core` here would drop the 17 non-core skills the user
// already had. `core` is the default only for a FRESH wizard run
// (setupResolveScope, cmd/setup.go:170).
func selfupdateRefreshFromEmbed(root string) (embedRefreshOutcome, error) {
	out := embedRefreshOutcome{Root: root}

	prev, err := readSetupState(root)
	if err != nil {
		return out, err
	}

	sels, scope, migrated, err := selfupdateRefreshSelections(root, prev)
	if err != nil {
		return out, err
	}
	out.Scope, out.Migrated = scope, migrated
	if len(sels) == 0 {
		out.Skipped = true
		return out, nil
	}

	staging, err := os.MkdirTemp("", "bravros-refresh-")
	if err != nil {
		return out, fmt.Errorf("create staging dir: %w", err)
	}
	defer os.RemoveAll(staging)

	plan, err := setupBuildPlan(root, staging, sels, prev)
	if err != nil {
		return out, err
	}
	out.Warnings = plan.Warnings

	if selfupdateDryRun {
		res := &setupApplyResult{}
		for _, it := range plan.Items {
			switch it.Action {
			case setupActionCreate:
				res.Created++
			case setupActionUnchanged:
				res.Unchanged++
			case setupActionConflict:
				res.Conflicts = append(res.Conflicts, it.Rel)
			case setupActionPrune:
				res.Pruned = append(res.Pruned, it.Rel)
			}
		}
		out.Result = res
		return out, nil
	}

	res, err := setupApply(plan)
	if err != nil {
		return out, err
	}
	out.Result = res
	out.Warnings = plan.Warnings

	// Record the migration ONCE, so the next refresh replays a real selection
	// instead of re-deriving one. An install that already has a setup.json is
	// left alone: rewriting it here would re-derive install_method from the
	// running binary's path and could clobber what install.sh recorded — and
	// `bravros update` reads that field to decide whether it may replace the
	// binary at all.
	if migrated {
		if _, _, wErr := setupWriteState(root, plan, scope); wErr != nil {
			return out, wErr
		}
	}
	return out, nil
}

// selfupdateRefreshSelections decides WHAT to refresh. Empty result means
// "nothing is installed here" — the caller reports and returns.
func selfupdateRefreshSelections(root string, prev *setupState) ([]payload.Selection, payload.SkillScope, bool, error) {
	if prev != nil && len(prev.Components) > 0 {
		scope := prev.SkillsScope
		if !scope.Valid() {
			// A state file written by a newer binary can name a scope this
			// build cannot resolve. The recorded skill LIST is still exact
			// (payload.Selection stores it precisely so this stays possible),
			// so replay that verbatim rather than guessing a scope.
			return selfupdateReplayRecorded(prev), prev.SkillsScope, false, nil
		}
		var sels []payload.Selection
		for _, c := range prev.Components {
			comp, ok := payload.ComponentByID(c.ID)
			if !ok {
				continue // a component this build no longer ships
			}
			// Re-resolve against THIS embed rather than replaying the stored
			// list: a release that adds a core skill must reach a machine
			// whose recorded scope already covers it.
			sel, err := comp.Select(scope)
			if err != nil {
				return nil, scope, false, err
			}
			sels = append(sels, sel)
		}
		return sels, scope, false, nil
	}

	// No setup.json. Pre-v2 install → migrate at scope all; otherwise skip.
	var ids []string
	if _, err := os.Stat(filepath.Join(root, "skills")); err == nil {
		ids = append(ids, "claude-skills")
	}
	if _, err := os.Stat(filepath.Join(root, "templates")); err == nil {
		ids = append(ids, "claude-templates")
	}
	if len(ids) == 0 {
		return nil, payload.ScopeAll, false, nil
	}
	sels, err := setupSelections(ids, payload.ScopeAll)
	if err != nil {
		return nil, payload.ScopeAll, false, err
	}
	return sels, payload.ScopeAll, true, nil
}

// selfupdateReplayRecorded rebuilds selections straight from state.json without
// re-resolving a scope this build does not understand.
func selfupdateReplayRecorded(prev *setupState) []payload.Selection {
	var sels []payload.Selection
	for _, c := range prev.Components {
		if _, ok := payload.ComponentByID(c.ID); !ok {
			continue
		}
		sels = append(sels, c.Selection)
	}
	return sels
}

// applyHookDrift emits the HOOK_DRIFT_CUSTOMIZED: lines, rewrites the
// ~/.claude/cache/last-selfupdate-hooks.log buffer, and refreshes any
// old-canonical commit-msg hooks against the payload-deployed canonical
// (~/.claude/templates/.githooks/commit-msg). It takes no repo argument:
// the canonical source is the payload deployed to `home`, not a clone on
// disk. The one repo-dependent step of the pre-P-0014 inlined block —
// `hooks.EnsureHooksPath(repo)`, which set `core.hooksPath` on the portable
// repo's git checkout — died with the clone lane: selfupdate no longer has
// a checkout of its own to point a hooks path at.
func applyHookDrift(home string, report HookDriftReport) {
	// Emit structured lines for customized hooks (always, not just verbose).
	// Write the same lines to the cache buffer file so auto-verify-install can read them.
	// Truncate the cache file every run — even when CustomizedPaths is empty — so
	// stale entries from a prior selfupdate don't keep surfacing after the user fixes
	// their hook.
	cacheDir := filepath.Join(home, ".claude", "cache")
	_ = os.MkdirAll(cacheDir, 0755)
	cacheFile := filepath.Join(cacheDir, "last-selfupdate-hooks.log")
	var cacheLines strings.Builder
	for _, p := range report.CustomizedPaths {
		line := "HOOK_DRIFT_CUSTOMIZED: " + p
		fmt.Fprintln(os.Stderr, line)
		cacheLines.WriteString(line + "\n")
	}
	_ = os.WriteFile(cacheFile, []byte(cacheLines.String()), 0644)

	// Perform hook refresh for old-canonical hooks (silent unless verbose).
	if report.NeedsRefresh {
		if selfupdateVerbose {
			fmt.Fprintln(os.Stderr, "🔄 Hook drift detected — refreshing commit-msg…")
		}
		if !selfupdateDryRun {
			canonicalPath := filepath.Join(home, ".claude", "templates", ".githooks", "commit-msg")
			for _, targetPath := range report.RefreshedPaths {
				if err := hooks.Refresh(targetPath, canonicalPath); err != nil && selfupdateVerbose {
					fmt.Fprintf(os.Stderr, "⚠️  hook refresh failed for %s: %v\n", targetPath, err)
				}
			}
		}
	}
}

// selfupdateFetchClient is the minimal surface selfupdateViaFetch needs from
// *fetch.Client. Declared here (consumer-side interface) so tests can inject
// a fake without touching the network or the filesystem outside a temp dir.
type selfupdateFetchClient interface {
	ResolveLatestTag(ctx context.Context) (string, error)
	FetchPayload(ctx context.Context, tag, destDir string) (string, error)
}

var (
	// selfupdateFetchClientOverride replaces fetch.NewClient() in tests.
	// Tests set this via t.Cleanup to restore the original (nil) value.
	selfupdateFetchClientOverride selfupdateFetchClient

	// selfupdateFetchPayloadDirOverride replaces fetch.DefaultPayloadDir() in
	// tests. Tests set this via t.Cleanup to restore the original ("") value.
	selfupdateFetchPayloadDirOverride string

	// selfupdateFetchTargetDirOverride replaces deploy's default TargetDir
	// (~/.claude) in tests, so tests never write outside their own temp dirs.
	// Tests set this via t.Cleanup to restore the original ("") value.
	selfupdateFetchTargetDirOverride string
)

// payloadTagFileName records, inside the payload dir itself, the tag of the
// payload currently on disk — the fetch-path analogue of the git-clone path's
// "installed version" probe.
const payloadTagFileName = ".bravros-payload-tag"

// selfupdateViaFetch is THE update path (P-0014 retired the clone lane): resolve
// the published release, and if the payload on disk is behind it, fetch + verify
// + deploy it. Returns nil for success and for every nothing-to-do outcome
// (in sync, offline, release without a payload asset); non-nil only on a real
// failure that must not look like success.
func selfupdateViaFetch(home string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var client selfupdateFetchClient
	if selfupdateFetchClientOverride != nil {
		client = selfupdateFetchClientOverride
	} else {
		client = fetch.NewClient()
	}

	payloadDir := selfupdateFetchPayloadDirOverride
	if payloadDir == "" {
		payloadDir = fetch.DefaultPayloadDir()
	}

	installedTag := readPayloadTag(payloadDir)

	res, err := selfupdate.CheckRemote(ctx, client, selfupdate.RemoteStatePath(), selfupdate.RemoteCheckTTL(), installedTag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  bravros: remote check failed: %v\n", err)
		return err
	}

	if res.Offline {
		// Offline is normal — previously-fetched skills keep working.
		if selfupdateVerbose {
			fmt.Fprintln(os.Stderr, "ℹ️  bravros: offline — skipping remote skills check")
		}
		return nil
	}

	// Hook drift must be checked even when the payload is already in sync —
	// a project's commit-msg hook can drift from the deployed canonical
	// without a new release landing, so this runs before the !res.Behind
	// early return, not after it. detectHookDrift scans a project checkout's
	// .githooks/commit-msg and .git/hooks/commit-msg — the fetch path has no
	// portable-repo clone to scan, so the right "repoRoot" here is the
	// current working directory: the project whose hooks would actually
	// drift. On os.Getwd() failure, skip hook drift silently rather than
	// fail the whole update over an unrelated stat error.
	//
	// applyHookDrift only writes output when CustomizedPaths/NeedsRefresh
	// are non-empty, so an in-sync run with clean hooks still produces no
	// output — the "must produce no output whatsoever" contract on the
	// !res.Behind branch below is preserved; a drifting hook is exactly the
	// case that contract is not meant to suppress.
	if cwd, cwdErr := os.Getwd(); cwdErr == nil {
		applyHookDrift(home, detectHookDrift(cwd, home))
	}

	if !res.Behind {
		// Check completed with nothing to do — stamp the check cache so the
		// next session within the TTL short-circuits before the remote check.
		// This is the fetch-lane home of the stamp the clone lane used to
		// place on its own no-drift early return; without it the TTL cache
		// would only ever be written by a real update. Silent by
		// construction: in-sync must produce no output whatsoever.
		touchSelfupdateCheckMarker(home)
		return nil
	}

	if _, fetchErr := client.FetchPayload(ctx, res.RemoteTag, payloadDir); fetchErr != nil {
		if errors.Is(fetchErr, fetch.ErrNoPayload) {
			// Every release cut before P-0003 publishes no payload asset —
			// that is "nothing to do", not a failure.
			if selfupdateVerbose {
				fmt.Fprintf(os.Stderr, "ℹ️  bravros: release %s publishes no payload asset — skipping\n", res.RemoteTag)
			}
			return nil
		}
		fmt.Fprintf(os.Stderr, "⚠️  bravros: fetch failed for %s: %v\n", res.RemoteTag, fetchErr)
		return fetchErr
	}

	deployResult, deployErr := deploy.Deploy(deploy.DeployOpts{
		SourceDir: payloadDir,
		TargetDir: selfupdateFetchTargetDirOverride,
		// Pruning is ON here, and scoped. The payload ships skills/ + templates/
		// (.goreleaser.yml), which is the COMPLETE deployable tree — the source
		// repo has no hooks/ or agents/ either. So for those two subtrees
		// "absent from the payload" genuinely means "deleted upstream", and with
		// the clone lane retired this fetch is the only delivery path: without
		// pruning, a skill removed upstream would linger on every machine
		// forever.
		//
		// PruneSubtrees narrows orphan detection to exactly what the payload
		// carries. ~/.claude/hooks and ~/.claude/agents are content bravros does
		// not own at the target, and they stay untouched. (deploy.detectOrphans
		// already skips any subtree missing from SourceDir; this scoping is the
		// explicit contract, so a payload that ever shipped an empty hooks/ or
		// agents/ still could not trigger a wipe.)
		PruneSubtrees: []string{"skills", "templates"},
		// FilterMode: EnabledSkills here is a deploy filter, not a prune
		// instruction. config.EnabledSkills() resolves .bravros.yml from CWD
		// first, and selfupdate fires unattended from the SessionStart hook —
		// so without this, opening a session in a project that sets
		// skills.enabled would delete every other skill from the shared
		// ~/.claude runtime. Filter mode keeps the allowlist additive:
		// non-listed skills are preserved, and only genuine payload orphans
		// (deleted upstream) are pruned.
		FilterMode:     true,
		PreserveSkills: config.PreservedSkills(),
		EnabledSkills:  config.EnabledSkills(),
	})
	if deployErr != nil {
		fmt.Fprintf(os.Stderr, "⚠️  bravros: deploy failed for %s: %v\n", res.RemoteTag, deployErr)
		return deployErr
	}

	tagFile := filepath.Join(payloadDir, payloadTagFileName)
	if err := os.MkdirAll(filepath.Dir(tagFile), 0755); err == nil {
		_ = os.WriteFile(tagFile, []byte(res.RemoteTag), 0644)
	}
	writeSelfupdateMarkers(home, res.RemoteTag)

	fmt.Fprintf(os.Stderr, "✨ bravros fetched skills from %s (%d skill(s))\n", res.RemoteTag, len(deployResult.SkillsDeployed))
	return nil
}

// readPayloadTag reads the tag recorded for the payload currently on disk.
// Missing or unreadable → "" (treated the same as "no payload ever fetched",
// which CheckRemote always reports as Behind once a remote tag is known).
func readPayloadTag(payloadDir string) string {
	data, err := os.ReadFile(filepath.Join(payloadDir, payloadTagFileName))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func writeSelfupdateMarkers(home, version string) {
	// Write last-update marker for --skip-if-recent. Keep this in the legacy
	// Claude state directory so existing installs preserve their behavior.
	if err := os.MkdirAll(legacyClaudeStateDir(home), 0755); err == nil {
		_ = os.WriteFile(filepath.Join(legacyClaudeStateDir(home), ".bravros-last-update"), []byte{}, 0644)
	}

	// A successful install is also a completed check — stamp the check cache.
	touchSelfupdateCheckMarker(home)

	content := fmt.Sprintf("version=%s\nts=%s\n", version, nowtime.NowRFC3339())
	for _, markerPath := range verifyInstallMarkerPaths(home) {
		if err := os.MkdirAll(filepath.Dir(markerPath), 0755); err == nil {
			_ = os.WriteFile(markerPath, []byte(content), 0644)
		}
	}
}

// selfupdateCheckMarker is touched in the legacy Claude state dir after every
// completed drift check (drift found or not). Its mtime drives the TTL cache
// that lets cache-hit session starts skip the ~9s fetch+check entirely (B-0345).
const selfupdateCheckMarker = ".bravros-last-check"

// selfupdateCheckTTL returns the TTL for the session-start check cache.
// Reads BRAVROS_SELFUPDATE_TTL (Go duration, e.g. "6h", "30m"); "0" or a
// negative value disables caching; unset or unparsable falls back to 6h.
func selfupdateCheckTTL() time.Duration {
	const defaultTTL = 6 * time.Hour
	raw := strings.TrimSpace(os.Getenv("BRAVROS_SELFUPDATE_TTL"))
	if raw == "" {
		return defaultTTL
	}
	if raw == "0" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return defaultTTL
	}
	if d < 0 {
		return 0
	}
	return d
}

// touchSelfupdateCheckMarker stamps the check-cache marker. No-op when caching
// is disabled (TTL 0) or under --dry-run, so neither leaves side effects.
func touchSelfupdateCheckMarker(home string) {
	if selfupdateCheckTTL() <= 0 || selfupdateDryRun {
		return
	}
	if err := os.MkdirAll(legacyClaudeStateDir(home), 0755); err != nil {
		return
	}
	markerFile := filepath.Join(legacyClaudeStateDir(home), selfupdateCheckMarker)
	now := time.Now()
	if err := os.Chtimes(markerFile, now, now); err != nil {
		_ = os.WriteFile(markerFile, []byte{}, 0644)
	}
}

func init() {
	rootCmd.AddCommand(selfupdateCmd)
	selfupdateCmd.Flags().BoolVar(&selfupdateVerbose, "verbose", false, "print detailed trace output")
	selfupdateCmd.Flags().BoolVar(&selfupdateSilent, "silent", false, "deprecated no-op: silence is now the default")
	_ = selfupdateCmd.Flags().MarkDeprecated("silent", "silence is now the default; use --verbose to see output")
	selfupdateCmd.Flags().StringVar(&selfupdateSkipIfRecent, "skip-if-recent", "", "skip update if last one was within this duration (e.g., '6h', '30m')")
	selfupdateCmd.Flags().BoolVar(&selfupdateDryRun, "dry-run", false, "show what would be updated without modifying anything on disk")
	// --deep gated the clone-based per-skill SHA + scripts drift detectors,
	// which P-0014 deleted along with the clone lane. The flag stays
	// registered as a deprecated no-op (mirroring --silent above) so a user
	// or hook still passing it gets a deprecation notice, not "unknown flag".
	selfupdateCmd.Flags().BoolVar(&selfupdateDeep, "deep", false, "deprecated no-op: the expensive clone-based drift detectors were removed")
	_ = selfupdateCmd.Flags().MarkDeprecated("deep", "the clone-based drift detectors it gated no longer exist")
	selfupdateCmd.Flags().BoolVar(&selfupdateForce, "force", false, "bypass the check-TTL cache and refresh now")
	// The legacy P-0003/P-0014 network lane. D2 took it off the SessionStart
	// hook (the embedded payload makes it unnecessary: skills now ship with
	// the binary), but it still works for anyone who wants the published
	// payload rather than this binary's embed.
	selfupdateCmd.Flags().BoolVar(&selfupdateFetchPayload, "fetch-payload", false, "legacy lane: fetch and deploy the published bravros-payload.tar.gz instead of refreshing from the embedded payload")
}

// HookDriftReport summarises the state of commit-msg hooks found in the project.
//
// NeedsRefresh is true when one or more hooks are old-canonical (they have the
// bravros marker but an older version number) — these are silently refreshed.
// CustomizedPaths lists hooks that have the bravros marker at the current version
// but differ from the canonical MD5 — user-edited, do NOT auto-refresh.
// RefreshedPaths lists the absolute paths that will be (or were) refreshed.
type HookDriftReport struct {
	NeedsRefresh    bool
	CustomizedPaths []string
	RefreshedPaths  []string
}

// detectHookDrift scans the two standard hook locations in the project:
//   - <repoRoot>/.githooks/commit-msg  (preferred, core.hooksPath = .githooks)
//   - <repoRoot>/.git/hooks/commit-msg  (fallback, classic git hooks dir)
//
// For each found file it calls hooks.Classify against the canonical hook at
// ~/.claude/templates/.githooks/commit-msg.  If the canonical file is missing
// (fresh install or non-hook project), the function returns an empty report
// (no error, no panic).
//
// Classification → action mapping:
//   - StatusPristine    → no-op (already current)
//   - StatusOldCanonical → add to RefreshedPaths, set NeedsRefresh = true
//   - StatusCurrent     → add to CustomizedPaths (do NOT auto-refresh)
//   - StatusForeign / StatusMissing → skip silently
//
// The MD5 equality check (target == canonical) is performed INDEPENDENTLY of
// Classify: Classify tells us how to handle drift; the MD5 check tells us
// whether drift exists at all.  A StatusPristine file that already matches the
// canonical byte-for-byte is a true no-op.
func detectHookDrift(repoRoot, home string) HookDriftReport {
	var report HookDriftReport

	canonicalPath := filepath.Join(home, ".claude", "templates", ".githooks", "commit-msg")
	if _, err := os.Stat(canonicalPath); err != nil {
		// Canonical missing — fresh install or non-hook project; silently skip.
		return report
	}

	canonicalMD5, err := hooks.ComputeMD5(canonicalPath)
	if err != nil {
		// Can't read canonical — fail-safe: return empty report.
		return report
	}

	// The two locations to scan.
	candidates := []string{
		filepath.Join(repoRoot, ".githooks", "commit-msg"),
		filepath.Join(repoRoot, ".git", "hooks", "commit-msg"),
	}

	for _, targetPath := range candidates {
		if _, err := os.Stat(targetPath); err != nil {
			// File doesn't exist — skip (StatusMissing handled here for speed).
			continue
		}

		status, err := hooks.Classify(targetPath, canonicalPath)
		if err != nil {
			// Unreadable — skip silently.
			continue
		}

		// MD5-equality check: is there actual content drift?
		targetMD5, err := hooks.ComputeMD5(targetPath)
		if err != nil {
			continue
		}
		drifted := targetMD5 != canonicalMD5

		switch status {
		case hooks.StatusPristine:
			// No marker present; matches historical MD5.  If it also matches the
			// current canonical byte-for-byte, it's a true no-op.  If not, it
			// needs a refresh to pick up the marker + any canonical content change.
			if drifted {
				report.NeedsRefresh = true
				report.RefreshedPaths = append(report.RefreshedPaths, targetPath)
			}
			// !drifted → already current, true no-op.

		case hooks.StatusOldCanonical:
			// Has marker but older version — always refresh (content will differ).
			report.NeedsRefresh = true
			report.RefreshedPaths = append(report.RefreshedPaths, targetPath)

		case hooks.StatusCurrent:
			// Has marker at current version but MD5 differs — user-edited.
			// Emit structured line; do NOT auto-refresh.
			if drifted {
				report.CustomizedPaths = append(report.CustomizedPaths, targetPath)
			}
			// !drifted means it has the current marker AND matches canonical MD5
			// (perfect state) — no-op.

		default:
			// StatusForeign, StatusMissing — skip silently.
		}
	}

	return report
}
