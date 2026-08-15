package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bravros/bravros/cli/internal/config"
	"github.com/bravros/bravros/cli/internal/deploy"
	"github.com/bravros/bravros/cli/internal/fetch"
	"github.com/bravros/bravros/cli/internal/hooks"
	"github.com/bravros/bravros/cli/internal/nowtime"
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
)

var selfupdateCmd = &cobra.Command{
	Use:     "selfupdate",
	Aliases: []string{"update"},
	Short:   "Fetch and deploy the newest published skills payload",
	Long: `Resolves the newest published release and, when the payload on disk is behind it,
downloads and minisign-verifies bravros-payload.tar.gz and deploys skills/ + templates/
into ~/.claude/. Stays a silent no-op when already in sync or when offline.

There is no local checkout in the picture: selfupdate never runs git, never runs install.sh,
and behaves identically whether or not a portable-repo clone happens to exist on disk.

Use --dry-run to see what would be downloaded and updated without modifying anything on disk.
Use --verbose to see the full trace (remote check, offline skips, deploy summary).

The check itself is cached: after every completed check the marker
~/.claude/state/.bravros-last-check is stamped, and 'bravros selfupdate' within the TTL
(default 6h, override via BRAVROS_SELFUPDATE_TTL, "0" disables) returns immediately —
this keeps SessionStart hooks under 1s instead of ~9s. 'bravros update' and --force
always bypass the cache and run the real check.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}

		// B-0345: TTL cache for the SessionStart check. A full check costs ~9s
		// (the network fetch dominates); a cache-hit session skips it entirely.
		// The marker is touched after every COMPLETED check — drift found or not —
		// so unlike --skip-if-recent (whose marker moves only on real installs)
		// the TTL bounds how often the fetch runs, not how often installs happen.
		// Bypassed by --force and by the explicit `bravros update` alias (a manual
		// run must always really check). BRAVROS_SELFUPDATE_TTL overrides the 6h
		// default; "0" disables caching entirely.
		checkTTL := selfupdateCheckTTL()
		forceCheck := selfupdateForce || cmd.CalledAs() == "update"
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

		// The published-payload fetch is the ONE update path (P-0014). A
		// portable-repo clone on disk is irrelevant: presence or absence of
		// ~/Sites/claude/.git changes nothing about what happens next.
		return selfupdateViaFetch(home)
	},
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
	selfupdateCmd.Flags().BoolVar(&selfupdateForce, "force", false, "bypass the check-TTL cache and run the full drift check now (the `bravros update` alias always does this)")
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
