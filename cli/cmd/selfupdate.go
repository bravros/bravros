package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

	// selfupdateRepoOverride is the portable repo path used by tests.
	// When non-empty it replaces the config.PortableRepo() default.
	// Tests set this via t.Cleanup to restore the original value.
	selfupdateRepoOverride string
)

var selfupdateCmd = &cobra.Command{
	Use:     "selfupdate",
	Aliases: []string{"update"},
	Short:   "Auto-update the toolkit when the source has drifted",
	Long: `Fetches the portable repo, detects real changes (commits, skills drift, CLI version drift),
and runs install.sh only when an update is actually needed. Stays silent with no 1Password prompts
when nothing changed.

Branch-agnostic: works on any git checkout (main, homolog, or any feature branch). Uses
'git fetch + git checkout origin/main' to update the working tree without switching branches,
so users on homolog or feature branches receive every update.

Use --dry-run to see what would be downloaded and updated without modifying anything on disk.
Use --verbose to see the full trace (fetching, applying overlay, running install.sh, etc.).

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

		repo := selfupdateRepoOverride
		if repo == "" {
			// Use config.PortableRepo() so BRAVROS_PORTABLE_REPO env var wins
			// over the auto-detection fallback in paths.PortableRepoDir().
			repo = config.PortableRepo()
		}
		cli := filepath.Join(home, ".claude", "bin", "bravros")

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

		gitDir := filepath.Join(repo, ".git")
		if info, err := os.Stat(gitDir); err != nil || !info.IsDir() {
			// No portable repo clone on disk — the curl|sh install path. Fall
			// back to fetching the published payload directly instead of
			// silently exiting 0 (P-0003: that silent-0 was the bug).
			return selfupdateViaFetch(home)
		}

		// Fetch origin/main to get the latest remote state.
		// Branch-agnostic — we fetch but do NOT pull (no branch switching).
		// --tags is required: named-refspec fetches (e.g. "main") do NOT auto-follow tags,
		// so without it `git describe --tags origin/main` would return a stale tag and
		// detectCliStale would silently miss tag-only version bumps.
		fetchCmd := exec.Command("git", "-C", repo, "fetch", "origin", "main", "--tags", "--quiet")
		if err := fetchCmd.Run(); err != nil {
			// Network failure is non-fatal: skip gracefully
			return nil
		}

		repoNeedsSync := selfupdate.HasOriginMainUpdates(repo)
		// Clobber guard (WS6): the working-tree overlay `git checkout origin/main -- .`
		// reverts ANY tracked file that differs from origin/main, regardless of branch.
		// That is safe only when HEAD is strictly an ancestor of origin/main (clean
		// catch-up / fast-forward). On a diverged or ahead HEAD — e.g. homolog or a
		// feature branch carrying committed-but-not-yet-on-main work — the overlay would
		// silently revert those local commits' content into a staged reversion (it ate a
		// feature-branch worktree live during the 2026-06-15 audit). IsBehindOriginMain
		// returns true ONLY for the ancestor-or-equal-behind case, so we gate the
		// destructive checkout on it. install.sh (skill/CLI/hook drift) still runs on a
		// diverged HEAD — only the working-tree overlay is skipped.
		safeToOverlay := selfupdate.IsBehindOriginMain(repo)

		// Cheap signals run on every invocation: git HEAD drift (HasOriginMainUpdates,
		// above), CLI version drift, and hook drift. These are sub-millisecond ref/stat
		// reads with no per-skill SHA walk.
		//
		// Expensive signals — per-skill SHA drift (each enabled skill's tree is walked +
		// hashed) and scripts drift — are gated behind --deep (4m6). They are redundant on
		// the common path: a skill or script edit pushed to main always lands as a new
		// origin/main commit, so HasOriginMainUpdates already fires and triggers install.sh.
		// --deep covers the rare case where the deployed runtime drifted WITHOUT a new main
		// commit (manual ~/.claude edit, partial deploy, manifest loss).
		claudeSkillsDrift := false
		scriptsDrift := false
		var scriptsDrifted []string

		manifestPath := filepath.Join(home, ".claude", "skills", deploy.ManifestFileName)
		manifest, _ := deploy.LoadManifest(manifestPath)

		if selfupdateDeep {
			claudeSkillsDrift = detectSkillsDrift(repo, home)
			scriptsDrifted, _ = selfupdate.DetectScriptsDrift(repo, manifest)
			scriptsDrift = len(scriptsDrifted) > 0
		}
		skillsDrift := claudeSkillsDrift
		cliStale := detectCliStale(cli, repo)

		hookReport := detectHookDrift(repo, home)

		if !repoNeedsSync && !skillsDrift && !cliStale && !scriptsDrift && !hookReport.NeedsRefresh {
			// Check completed, nothing to do — stamp the check cache so the next
			// session within the TTL skips the ~9s fetch entirely.
			touchSelfupdateCheckMarker(home)
			return nil
		}

		// Capture old version before install for the upgrade summary line.
		oldVer := ""
		if verOut, err := exec.Command(cli, "version").Output(); err == nil {
			parts := strings.Fields(strings.TrimSpace(string(verOut)))
			if len(parts) >= 2 {
				oldVer = parts[len(parts)-1]
			}
		}

		// manifest was loaded above (for scripts drift); use it as manifestBefore.
		manifestBefore := manifest

		if repoNeedsSync && safeToOverlay {
			if selfupdateVerbose {
				if selfupdateDryRun {
					fmt.Fprintln(os.Stderr, "🔍 [dry-run] SDLC repo has origin/main updates — would sync working tree")
				} else {
					fmt.Fprintln(os.Stderr, "🔄 SDLC updating — syncing working tree to origin/main…")
				}
			}
			// Branch-agnostic update: checkout origin/main into working tree WITHOUT switching branches.
			// Gated on safeToOverlay (IsBehindOriginMain) so it runs ONLY when HEAD is strictly an
			// ancestor of origin/main — never on a diverged/ahead HEAD, which would clobber local commits.
			if err := selfupdate.UpdatePortableRepo(repo, selfupdateDryRun); err != nil {
				fmt.Fprintf(os.Stderr, "⚠️  repo update failed — resolve manually: cd %s && git checkout origin/main -- .\n  (%v)\n", repo, err)
				return nil
			}
		} else if repoNeedsSync && !safeToOverlay {
			// Diverged or ahead of origin/main: HEAD carries local commits not on main.
			// Skip the destructive working-tree overlay so committed-but-not-on-main work
			// is never clobbered. install.sh below still runs for any skill/CLI/hook drift.
			if selfupdateVerbose {
				fmt.Fprintf(os.Stderr, "ℹ️  SDLC repo diverged from origin/main on this branch — skipping working-tree overlay to protect local commits. To sync, merge or rebase origin/main yourself.\n")
			}
		} else if skillsDrift {
			if selfupdateVerbose {
				fmt.Fprintln(os.Stderr, "🔄 Skills drift detected (claude) — running install.sh…")
			}
		} else if cliStale {
			if selfupdateVerbose {
				fmt.Fprintln(os.Stderr, "🔄 CLI version drift — running install.sh…")
			}
		} else if scriptsDrift {
			if selfupdateVerbose {
				fmt.Fprintf(os.Stderr, "🔄 Scripts drift detected (%s) — running install.sh…\n", strings.Join(scriptsDrifted, ", "))
			}
		}

		// Emit structured lines for customized hooks (always, not just verbose).
		// Write the same lines to the cache buffer file so auto-verify-install can read them.
		// Truncate the cache file every run — even when CustomizedPaths is empty — so
		// stale entries from a prior selfupdate don't keep surfacing after the user fixes
		// their hook.
		cacheDir := filepath.Join(home, ".claude", "cache")
		_ = os.MkdirAll(cacheDir, 0755)
		cacheFile := filepath.Join(cacheDir, "last-selfupdate-hooks.log")
		var cacheLines strings.Builder
		for _, p := range hookReport.CustomizedPaths {
			line := "HOOK_DRIFT_CUSTOMIZED: " + p
			fmt.Fprintln(os.Stderr, line)
			cacheLines.WriteString(line + "\n")
		}
		_ = os.WriteFile(cacheFile, []byte(cacheLines.String()), 0644)

		// Perform hook refresh for old-canonical hooks (silent unless verbose).
		if hookReport.NeedsRefresh {
			if selfupdateVerbose {
				fmt.Fprintln(os.Stderr, "🔄 Hook drift detected — refreshing commit-msg…")
			}
			if !selfupdateDryRun {
				canonicalPath := filepath.Join(home, ".claude", "templates", ".githooks", "commit-msg")
				for _, targetPath := range hookReport.RefreshedPaths {
					if err := hooks.Refresh(targetPath, canonicalPath); err != nil && selfupdateVerbose {
						fmt.Fprintf(os.Stderr, "⚠️  hook refresh failed for %s: %v\n", targetPath, err)
					}
				}
				_ = hooks.EnsureHooksPath(repo)
			}
		}

		if selfupdateDryRun {
			if selfupdateVerbose {
				fmt.Fprintln(os.Stderr, "🔍 [dry-run] would run install.sh to apply changes")
			}
			return nil
		}

		installCmd := exec.Command("bash", filepath.Join(repo, "install.sh"))
		installCmd.Stdout = nil
		installCmd.Stderr = nil
		if err := installCmd.Run(); err != nil {
			exitCode := 1
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
			fmt.Fprintf(os.Stderr, "⚠️  install.sh failed (exit %d) — run manually: bash %s/install.sh\n", exitCode, repo)
			return nil
		}

		newVer := "unknown"
		if verOut, err := exec.Command(cli, "version").Output(); err == nil {
			parts := strings.Fields(strings.TrimSpace(string(verOut)))
			if len(parts) >= 2 {
				newVer = parts[len(parts)-1]
			}
		}

		// Update the Scripts SHAs in the manifest after a successful install.sh run.
		// This prevents scripts/ drift from triggering a redeploy on every subsequent
		// selfupdate call when the install.sh already wrote the updated scripts.
		if newScriptsSHAs, err := selfupdate.ComputeScriptsSHAs(repo); err == nil {
			if manifestAfterInstall, err := deploy.LoadManifest(manifestPath); err == nil {
				manifestAfterInstall.Scripts = newScriptsSHAs
				_ = deploy.SaveManifest(manifestPath, manifestAfterInstall)
			}
		}

		// Compute which skills changed by diffing the manifest before and after install.sh.
		// Used for the skills-only upgrade summary line below. Sort the result so the
		// truncated display ("a, b, c +N more") is deterministic across runs (map
		// iteration order is randomized in Go).
		var updatedSkills []string
		if manifestAfter, err := deploy.LoadManifest(manifestPath); err == nil {
			updatedSkills = append(updatedSkills, manifestDiffSkillNames(manifestBefore, manifestAfter, "")...)
		}
		sort.Strings(updatedSkills)

		// Always emit exactly one upgrade line on real upgrade (default and verbose).
		if oldVer != "" && oldVer != newVer {
			fmt.Fprintf(os.Stderr, "🔄 bravros %s → %s\n", oldVer, newVer)
			// Audio notice on real version bump (100% PT-BR — script gates on Mac-unlock + HA reachability).
			// Strip a leading "v" so Alexa reads "três quarenta e quatro ponto zero" not "vê três…".
			announceVer := strings.TrimPrefix(newVer, "v")
			announce := exec.Command(os.Args[0], "ha", "say", "--force",
				"Nova versão Bravros instalada. Versão "+announceVer+".", "studio")
			announce.Stdout = nil
			announce.Stderr = nil
			if err := announce.Start(); err == nil {
				_ = announce.Process.Release()
			}
		} else if len(updatedSkills) > 0 {
			// Skills changed but CLI version stayed same — emit a skills-only summary.
			names := updatedSkills
			suffix := ""
			if len(names) > 3 {
				names = names[:3]
				suffix = fmt.Sprintf(" (+%d more)", len(updatedSkills)-3)
			}
			fmt.Fprintf(os.Stderr, "✨ bravros deployed %d skill update(s) from main: %s%s\n",
				len(updatedSkills), strings.Join(names, ", "), suffix)
		} else if selfupdateVerbose {
			fmt.Fprintf(os.Stderr, "✓ SDLC updated to %s\n", newVer)
		}

		writeSelfupdateMarkers(home, newVer)
		if selfupdateVerbose {
			fmt.Fprintln(os.Stderr, "✓ verify-install queued for next session")
		}

		return nil
	},
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

// selfupdateViaFetch is the no-clone path: resolve the published release, and if
// the payload on disk is behind it, fetch + verify + deploy it. Returns the
// process exit code (0 = success or nothing-to-do, non-zero = a real failure).
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

	if !res.Behind {
		// In-sync: must produce no output whatsoever.
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
		// NoPrune: orphan-pruning compares TargetDir against SourceDir and
		// deletes anything with no counterpart. The fetched payload is a
		// PARTIAL source tree by construction (only skills/ + templates/ ship
		// in it — no hooks/, no agents/), so "absent from the payload" means
		// "not shipped in the payload", never "deleted upstream". Pruning is
		// sound only when SourceDir is a full repo checkout (the clone path);
		// here it would wipe every hand-installed hook/agent/skill on the
		// first automatic SessionStart fetch.
		NoPrune:        true,
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
	selfupdateCmd.Flags().BoolVar(&selfupdateDeep, "deep", false, "also run the expensive per-skill SHA + scripts drift detectors (default off: only git HEAD, CLI version, and hooks are checked)")
	selfupdateCmd.Flags().BoolVar(&selfupdateForce, "force", false, "bypass the check-TTL cache and run the full drift check now (the `bravros update` alias always does this)")
}

// detectSkillsDrift returns true when deployed skills/ differs from source in any
// way — file added, removed, renamed, or content modified — regardless of which
// file inside a skill directory changed.
//
// Mechanism: compares each enabled source skill's SHA (computed via
// deploy.ComputeSkillSHA) against the corresponding entry in the deployed
// manifest (~/.claude/skills/.deploy-manifest.json written by install.sh).
//
// Returns true (force redeploy) when:
//   - The manifest file is missing entirely (deploy never ran since manifest
//     support landed, or the runtime directory is fresh).
//   - A source skill's SHA differs from the manifest entry.
//   - A skill exists in the manifest but has been removed from source.
//   - Any ComputeSkillSHA call fails (fail-safe: redeploy rather than miss a change).
//
// Returns false only when every enabled source skill's SHA matches its manifest entry.
func detectSkillsDrift(repo, home string) bool {
	manifestPath := filepath.Join(home, ".claude", "skills", deploy.ManifestFileName)
	return detectSourceSkillManifestDrift(repo, manifestPath)
}

func detectSourceSkillManifestDrift(repo, manifestPath string) bool {
	manifest, err := deploy.LoadManifest(manifestPath)
	if err != nil {
		// Unreadable manifest — treat as drift.
		return true
	}

	// If the manifest has no entries AND no source skills exist, nothing to do.
	// But an empty manifest when source skills exist signals "deploy never ran" — return true.
	srcSkillsDir := filepath.Join(repo, "skills")
	entries, err := os.ReadDir(srcSkillsDir)
	if err != nil {
		// Can't read source skills dir — can't determine drift; fail-safe is false.
		return false
	}

	// Collect enabled source skills using the same allowlist Deploy() uses.
	enabledSkills := config.EnabledSkills() // nil = all skills enabled

	sourceSeen := make(map[string]bool)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()

		// Skip non-runtime shared-asset dirs (shared/, _shared/) — the same gate
		// deploy.Deploy() applies. These hold shared reference docs, not
		// deployable skills, so deploy never writes them to the manifest.
		// Iterating them here would hit the !inManifest branch below and report
		// perpetual false-positive drift (R-0004).
		if deploy.NonRuntimeSkillDir(name) {
			continue
		}

		skillDir := filepath.Join(srcSkillsDir, name)

		// Apply enabled-skills allowlist (same logic as deploy.isSkillEnabled).
		if !skillIsEnabled(name, skillDir, enabledSkills) {
			continue
		}

		sourceSeen[name] = true

		currentSHA, err := deploy.ComputeSkillSHA(skillDir)
		if err != nil {
			// Broken symlink or unreadable file — treat as drift to force a clean redeploy.
			return true
		}

		manifestSHA, inManifest := manifest.Skills[name]
		if !inManifest {
			// Skill exists in source but not in manifest — newly added or manifest is stale.
			return true
		}
		if currentSHA != manifestSHA {
			return true
		}
	}

	// Check for skills in the manifest that no longer exist in source (removed skill).
	for name := range manifest.Skills {
		if !sourceSeen[name] {
			return true
		}
	}

	return false
}

func manifestDiffSkillNames(before, after deploy.Manifest, prefix string) []string {
	var changed []string
	for name, newSHA := range after.Skills {
		if before.Skills[name] != newSHA {
			changed = append(changed, prefix+name)
		}
	}
	for name := range before.Skills {
		if _, exists := after.Skills[name]; !exists {
			changed = append(changed, prefix+name)
		}
	}
	return changed
}

// skillIsEnabled mirrors deploy.isSkillEnabled for callers outside the deploy package.
// enabledList nil/empty means all skills are enabled. Delegates the `core: true`
// frontmatter scan to deploy.IsSkillCore so the two stay aligned (a previous
// 512-byte prefix-read here would miss `core: true` in frontmatter that exceeds
// 512 bytes — drift detection would silently skip such skills).
//
// Note: deploy.Deploy() gates skill discovery with TWO checks —
// deploy.NonRuntimeSkillDir and deploy.isSkillEnabled. This function mirrors only
// the isSkillEnabled half; the caller (detectSkillsDrift) applies
// deploy.NonRuntimeSkillDir separately before invoking this.
func skillIsEnabled(name, skillDir string, enabledList []string) bool {
	if len(enabledList) == 0 {
		return true
	}
	for _, n := range enabledList {
		if n == name {
			return true
		}
	}
	return deploy.IsSkillCore(filepath.Join(skillDir, "SKILL.md"))
}

// detectCliStale compares installed bravros version with the latest release tag in the portable repo.
// Uses `git -C <repo> describe --tags --abbrev=0 origin/main` — no network call beyond the
// already-performed fetch.
// Fail-safe: returns false if either version probe fails (don't trigger unnecessary install).
func detectCliStale(cli, repo string) bool {
	installedOut, err := exec.Command(cli, "version").Output()
	if err != nil {
		return false
	}
	tagOut, err := exec.Command("git", "-C", repo, "describe", "--tags", "--abbrev=0", "origin/main").Output()
	if err != nil {
		return false
	}
	// `bravros version` prints "bravros v3.0.1" — extract the "v3.0.1" portion
	installed := strings.TrimSpace(string(installedOut))
	parts := strings.Fields(installed)
	installedVer := ""
	if len(parts) >= 2 {
		installedVer = parts[len(parts)-1]
	}
	latestTag := strings.TrimSpace(string(tagOut))
	if installedVer == "" || latestTag == "" {
		return false
	}
	return installedVer != latestTag
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
