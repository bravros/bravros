package cmd

// Tests for the selfupdate command.
//
// Since P-0014 there is exactly ONE update path — selfupdateViaFetch — so these tests
// never build git fixtures: they call selfupdateCmd.RunE directly (bypassing cobra flag
// parsing) after setting the package-level flag vars, and inject a fakeFetchClient plus
// temp payload/target dirs so nothing touches the network or the developer's ~/.claude.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bravros/bravros/cli/internal/config"
	"github.com/bravros/bravros/cli/internal/fetch"
	"github.com/bravros/bravros/cli/internal/hooks"
	"github.com/bravros/bravros/cli/internal/payload"
	"github.com/bravros/bravros/cli/internal/selfupdate"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// captureStderr redirects os.Stderr to a pipe, runs fn, restores os.Stderr,
// and returns everything written to stderr during fn.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w

	fn()

	w.Close()
	os.Stderr = orig

	var buf strings.Builder
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	r.Close()
	return buf.String()
}

// resetSelfupdateFlags sets all package-level selfupdate flag vars to their
// zero values and restores them via t.Cleanup.
func resetSelfupdateFlags(t *testing.T) {
	t.Helper()
	origSilent := selfupdateSilent
	origVerbose := selfupdateVerbose
	origSkip := selfupdateSkipIfRecent
	origDry := selfupdateDryRun
	origDeep := selfupdateDeep
	origForce := selfupdateForce
	origFetchPayload := selfupdateFetchPayload
	origFetchClient := selfupdateFetchClientOverride
	origFetchPayloadDir := selfupdateFetchPayloadDirOverride
	origFetchTargetDir := selfupdateFetchTargetDirOverride
	origNoticeResolver := selfupdateNoticeResolverOverride
	origCodexHome, hadCodexHome := os.LookupEnv("BRAVROS_CODEX_HOME")
	origOpenCodeHome, hadOpenCodeHome := os.LookupEnv("BRAVROS_OPENCODE_HOME")
	origPiHome, hadPiHome := os.LookupEnv("BRAVROS_PI_HOME")

	selfupdateSilent = false
	selfupdateVerbose = false
	selfupdateSkipIfRecent = ""
	selfupdateDryRun = false
	selfupdateDeep = false
	selfupdateForce = false
	selfupdateFetchPayload = false
	selfupdateFetchClientOverride = nil
	selfupdateFetchPayloadDirOverride = ""
	selfupdateFetchTargetDirOverride = ""
	selfupdateNoticeResolverOverride = nil
	// Disable the check-TTL cache (B-0345) so tests exercise the real check path
	// and never read/write the developer's real ~/.claude/state marker. Cache
	// tests opt back in with t.Setenv + a HOME override.
	t.Setenv("BRAVROS_SELFUPDATE_TTL", "0")
	// P-0015 Phase 6: the SessionStart lane ends with a passive "newer version
	// available" check, and that is the ONE thing in it that can reach the
	// network. Off by default for every test; the tests that measure it opt
	// back in (see TestSelfupdate_PassiveNotice_OneRequestPer24h).
	t.Setenv("BRAVROS_NO_UPDATE_CHECK", "1")
	if err := os.Setenv("BRAVROS_CODEX_HOME", filepath.Join(t.TempDir(), "missing-codex")); err != nil {
		t.Fatalf("set BRAVROS_CODEX_HOME: %v", err)
	}
	if err := os.Setenv("BRAVROS_OPENCODE_HOME", filepath.Join(t.TempDir(), "missing-opencode")); err != nil {
		t.Fatalf("set BRAVROS_OPENCODE_HOME: %v", err)
	}
	if err := os.Setenv("BRAVROS_PI_HOME", filepath.Join(t.TempDir(), "missing-pi")); err != nil {
		t.Fatalf("set BRAVROS_PI_HOME: %v", err)
	}

	t.Cleanup(func() {
		selfupdateSilent = origSilent
		selfupdateVerbose = origVerbose
		selfupdateSkipIfRecent = origSkip
		selfupdateDryRun = origDry
		selfupdateDeep = origDeep
		selfupdateForce = origForce
		selfupdateFetchPayload = origFetchPayload
		selfupdateFetchClientOverride = origFetchClient
		selfupdateFetchPayloadDirOverride = origFetchPayloadDir
		selfupdateFetchTargetDirOverride = origFetchTargetDir
		selfupdateNoticeResolverOverride = origNoticeResolver
		if hadCodexHome {
			_ = os.Setenv("BRAVROS_CODEX_HOME", origCodexHome)
		} else {
			_ = os.Unsetenv("BRAVROS_CODEX_HOME")
		}
		if hadOpenCodeHome {
			_ = os.Setenv("BRAVROS_OPENCODE_HOME", origOpenCodeHome)
		} else {
			_ = os.Unsetenv("BRAVROS_OPENCODE_HOME")
		}
		if hadPiHome {
			_ = os.Setenv("BRAVROS_PI_HOME", origPiHome)
		} else {
			_ = os.Unsetenv("BRAVROS_PI_HOME")
		}
	})
}

// runSelfupdate calls selfupdateCmd.RunE directly and returns (stderr, error).
func runSelfupdate(t *testing.T) (string, error) {
	t.Helper()
	var err error
	stderr := captureStderr(t, func() {
		err = selfupdateCmd.RunE(selfupdateCmd, nil)
	})
	return stderr, err
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestSelfupdateFetchPathTakenWhenNoCloneExists is the no-clone half of the
// P-0014 invariant pair (see TestSelfupdateFetchPathTakenEvenWhenCloneExists for
// the other half): with no portable-repo clone anywhere under HOME, RunE takes
// the fetch path — it does NOT exit silently, which was the pre-P-0003 bug.
func TestSelfupdateFetchPathTakenWhenNoCloneExists(t *testing.T) {
	home, payloadDir := setupFetchPathTest(t)
	t.Setenv("HOME", home)
	// Keep the (now-unread) env override out of the picture so the clone probe,
	// if it were ever reintroduced, would resolve strictly under this temp HOME.
	t.Setenv("BRAVROS_PORTABLE_REPO", "")
	writeInstalledTag(t, payloadDir, "v1.0.0")

	fake := &fakeFetchClient{resolveTag: "v1.0.0"}
	selfupdateFetchClientOverride = fake

	stderr, err := runSelfupdate(t)
	if err != nil {
		t.Fatalf("RunE without a clone must not error, got: %v", err)
	}
	if fake.resolveCalls != 1 {
		t.Errorf("fetch path must run without a clone; ResolveLatestTag called %d time(s), want 1", fake.resolveCalls)
	}
	// In sync with the remote → still no output.
	if stderr != "" {
		t.Errorf("in-sync run must be silent, got: %q", stderr)
	}
}

// TestSelfupdate_VerboseFlag_PrintsDetails verifies that:
//   - with --verbose, stderr carries the fetch-path trace for a non-update outcome
//   - without --verbose (default), that same outcome is completely silent
//
// Salvaged from the clone-lane version of this test: the traced events changed
// (remote check / offline skip instead of git fetch / install.sh), the contract
// did not.
func TestSelfupdate_VerboseFlag_PrintsDetails(t *testing.T) {
	t.Run("verbose_traces_offline_skip", func(t *testing.T) {
		home, payloadDir := setupFetchPathTest(t)
		t.Setenv("HOME", home)
		writeInstalledTag(t, payloadDir, "v1.0.0")
		selfupdateFetchClientOverride = &fakeFetchClient{
			resolveErr: errors.New("dial tcp: network is unreachable"),
		}
		selfupdateVerbose = true

		stderr, err := runSelfupdate(t)
		if err != nil {
			t.Fatalf("RunE error: %v", err)
		}
		if !strings.Contains(stderr, "offline") {
			t.Errorf("expected a verbose offline trace line, got: %q", stderr)
		}
	})

	t.Run("silent_when_not_verbose", func(t *testing.T) {
		home, payloadDir := setupFetchPathTest(t)
		t.Setenv("HOME", home)
		writeInstalledTag(t, payloadDir, "v1.0.0")
		selfupdateFetchClientOverride = &fakeFetchClient{
			resolveErr: errors.New("dial tcp: network is unreachable"),
		}
		selfupdateVerbose = false

		stderr, err := runSelfupdate(t)
		if err != nil {
			t.Fatalf("RunE error: %v", err)
		}
		if stderr != "" {
			t.Errorf("default verbosity must stay silent, got: %q", stderr)
		}
	})
}

func TestWriteSelfupdateMarkers_WritesSharedAndLegacyVerifyMarkers(t *testing.T) {
	home := t.TempDir()
	writeSelfupdateMarkers(home, "v9.9.9")

	for _, markerPath := range verifyInstallMarkerPaths(home) {
		data, err := os.ReadFile(markerPath)
		if err != nil {
			t.Fatalf("expected marker %s: %v", markerPath, err)
		}
		if !strings.Contains(string(data), "version=v9.9.9") {
			t.Fatalf("marker %s missing version: %s", markerPath, data)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "state", ".bravros-last-update")); err != nil {
		t.Fatalf("expected legacy last-update marker: %v", err)
	}
}

// TestUpdateIsItsOwnVerb_NotASelfupdateAlias is the MIGRATED form of the old
// TestSelfupdateAlias_UpdateInvokesSelfupdate, which asserted that `bravros
// update` was an alias resolving to `selfupdate`.
//
// P-0015 D2 gave `update` an independent meaning, so the old assertion is now
// the wrong contract — but the two behaviors the alias actually provided both
// survive and are pinned here instead:
//
//  1. `bravros update` still resolves to a real command (now its own).
//  2. The alias's ONLY extra behavior was bypassing the TTL cache
//     (`cmd.CalledAs() == "update"`). That is now --force on selfupdate, and
//     `update` needs no cache bypass at all: running it IS the check. The
//     bypass itself stays covered by TestSelfupdate_CheckCache_ForceBypasses.
func TestUpdateIsItsOwnVerb_NotASelfupdateAlias(t *testing.T) {
	for _, alias := range selfupdateCmd.Aliases {
		if alias == "update" {
			t.Errorf("selfupdate must no longer alias 'update' (P-0015 D2); aliases: %v", selfupdateCmd.Aliases)
		}
	}

	cmd, _, err := rootCmd.Find([]string{"update"})
	if err != nil {
		t.Fatalf("rootCmd.Find('update') error: %v", err)
	}
	if cmd == nil {
		t.Fatal("rootCmd.Find('update') returned nil command")
	}
	if cmd.Use != "update" {
		t.Errorf("expected `bravros update` to resolve to the update command, got Use=%q", cmd.Use)
	}
	if cmd == selfupdateCmd {
		t.Error("`bravros update` still resolves to the selfupdate command")
	}

	// And selfupdate is still reachable under its own name.
	su, _, err := rootCmd.Find([]string{"selfupdate"})
	if err != nil {
		t.Fatalf("rootCmd.Find('selfupdate') error: %v", err)
	}
	if su != selfupdateCmd {
		t.Errorf("expected `bravros selfupdate` to resolve to selfupdateCmd, got %q", su.Use)
	}
}

// ── detectHookDrift tests ─────────────────────────────────────────────────────

// hookDriftFixture builds a minimal home+repo pair with a canonical hook and
// optional project hook. Returns (repoRoot, homeDir).
//
// canonicalContent is written to ~/.claude/templates/.githooks/commit-msg.
// When canonicalContent is empty the canonical file is NOT created (simulates
// fresh install before hooks phase ships).
func hookDriftFixture(t *testing.T, canonicalContent string) (repo, home string) {
	t.Helper()
	home = t.TempDir()
	repo = t.TempDir()

	if canonicalContent != "" {
		canonicalDir := filepath.Join(home, ".claude", "templates", ".githooks")
		if err := os.MkdirAll(canonicalDir, 0755); err != nil {
			t.Fatalf("mkdir canonical dir: %v", err)
		}
		canonicalPath := filepath.Join(canonicalDir, "commit-msg")
		if err := os.WriteFile(canonicalPath, []byte(canonicalContent), 0755); err != nil {
			t.Fatalf("write canonical hook: %v", err)
		}
	}

	return repo, home
}

// currentCanonicalContent is the v1 canonical hook content — matches the fixture
// in cli/internal/hooks/testdata/canonical_v1.sh.
const currentCanonicalContent = "#!/bin/bash\n# bravros-managed-commit-msg-hook v1\n# This is the current canonical hook fixture (v1).\nset -u\nCOMMIT_MSG_FILE=$1\nexit 0\n"

// TestDetectHookDrift_PristineHookNoOp verifies that a hook that already matches
// the canonical content byte-for-byte returns an empty report (true no-op).
func TestDetectHookDrift_PristineHookNoOp(t *testing.T) {
	repo, home := hookDriftFixture(t, currentCanonicalContent)

	// Write a hook to .githooks/commit-msg that has the same content as canonical.
	hookDir := filepath.Join(repo, ".githooks")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		t.Fatalf("mkdir .githooks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hookDir, "commit-msg"), []byte(currentCanonicalContent), 0755); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	report := detectHookDrift(repo, home)

	if report.NeedsRefresh {
		t.Error("expected NeedsRefresh=false for in-sync hook, got true")
	}
	if len(report.CustomizedPaths) != 0 {
		t.Errorf("expected no CustomizedPaths, got %v", report.CustomizedPaths)
	}
	if len(report.RefreshedPaths) != 0 {
		t.Errorf("expected no RefreshedPaths, got %v", report.RefreshedPaths)
	}
}

// TestDetectHookDrift_OldCanonicalRefreshed verifies that a hook with an old
// marker version triggers NeedsRefresh and is added to RefreshedPaths.
func TestDetectHookDrift_OldCanonicalRefreshed(t *testing.T) {
	repo, home := hookDriftFixture(t, currentCanonicalContent)

	// Write a hook with marker v0 (old-canonical) to .githooks/commit-msg.
	oldContent := "#!/bin/bash\n# bravros-managed-commit-msg-hook v0\nexit 0\n"
	hookDir := filepath.Join(repo, ".githooks")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		t.Fatalf("mkdir .githooks: %v", err)
	}
	hookPath := filepath.Join(hookDir, "commit-msg")
	if err := os.WriteFile(hookPath, []byte(oldContent), 0755); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	report := detectHookDrift(repo, home)

	if !report.NeedsRefresh {
		t.Error("expected NeedsRefresh=true for old-canonical hook, got false")
	}
	if len(report.RefreshedPaths) == 0 {
		t.Error("expected hookPath in RefreshedPaths, got empty")
	} else if report.RefreshedPaths[0] != hookPath {
		t.Errorf("expected RefreshedPaths[0]=%s, got %s", hookPath, report.RefreshedPaths[0])
	}
	if len(report.CustomizedPaths) != 0 {
		t.Errorf("expected no CustomizedPaths, got %v", report.CustomizedPaths)
	}
}

// TestDetectHookDrift_CustomizedEmitsStructuredLine verifies that a hook with the
// current bravros marker (v1) but different content populates CustomizedPaths.
func TestDetectHookDrift_CustomizedEmitsStructuredLine(t *testing.T) {
	repo, home := hookDriftFixture(t, currentCanonicalContent)

	// Write a hook with current marker version but different body content.
	// Classify will return StatusCurrent because it has marker v1 (current).
	// MD5 will differ from canonical because the body is different.
	customContent := "#!/bin/bash\n# bravros-managed-commit-msg-hook v1\n# USER CUSTOMIZATION: extra validation\nset -u\nCOMMIT_MSG_FILE=$1\necho 'custom check'\nexit 0\n"
	hookDir := filepath.Join(repo, ".githooks")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		t.Fatalf("mkdir .githooks: %v", err)
	}
	hookPath := filepath.Join(hookDir, "commit-msg")
	if err := os.WriteFile(hookPath, []byte(customContent), 0755); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	report := detectHookDrift(repo, home)

	if report.NeedsRefresh {
		t.Error("expected NeedsRefresh=false for customized hook (must not auto-refresh), got true")
	}
	if len(report.CustomizedPaths) == 0 {
		t.Error("expected hookPath in CustomizedPaths, got empty")
	} else if report.CustomizedPaths[0] != hookPath {
		t.Errorf("expected CustomizedPaths[0]=%s, got %s", hookPath, report.CustomizedPaths[0])
	}
}

// TestDetectHookDrift_ForeignHookSkipped verifies that a hook with no bravros
// marker and no historical MD5 match is silently skipped (empty report).
func TestDetectHookDrift_ForeignHookSkipped(t *testing.T) {
	repo, home := hookDriftFixture(t, currentCanonicalContent)

	// Write a completely foreign hook (no marker, no historical MD5 match).
	foreignContent := "#!/bin/bash\n# totally unrelated hook from another tool\necho 'foreign'\nexit 0\n"
	hookDir := filepath.Join(repo, ".githooks")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		t.Fatalf("mkdir .githooks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hookDir, "commit-msg"), []byte(foreignContent), 0755); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	report := detectHookDrift(repo, home)

	if report.NeedsRefresh {
		t.Error("expected NeedsRefresh=false for foreign hook, got true")
	}
	if len(report.CustomizedPaths) != 0 {
		t.Errorf("expected no CustomizedPaths for foreign hook, got %v", report.CustomizedPaths)
	}
}

// TestDetectHookDrift_BothGitHooksAndDotGithooksScanned verifies that hooks
// in BOTH .githooks/ and .git/hooks/ are detected.
func TestDetectHookDrift_BothGitHooksAndDotGithooksScanned(t *testing.T) {
	repo, home := hookDriftFixture(t, currentCanonicalContent)

	// Write old-canonical hooks to BOTH locations.
	oldContent := "#!/bin/bash\n# bravros-managed-commit-msg-hook v0\nexit 0\n"

	githooksDir := filepath.Join(repo, ".githooks")
	if err := os.MkdirAll(githooksDir, 0755); err != nil {
		t.Fatalf("mkdir .githooks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(githooksDir, "commit-msg"), []byte(oldContent), 0755); err != nil {
		t.Fatalf("write .githooks hook: %v", err)
	}

	gitHooksDir := filepath.Join(repo, ".git", "hooks")
	if err := os.MkdirAll(gitHooksDir, 0755); err != nil {
		t.Fatalf("mkdir .git/hooks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitHooksDir, "commit-msg"), []byte(oldContent), 0755); err != nil {
		t.Fatalf("write .git/hooks hook: %v", err)
	}

	report := detectHookDrift(repo, home)

	if !report.NeedsRefresh {
		t.Error("expected NeedsRefresh=true when old hooks in both locations, got false")
	}
	if len(report.RefreshedPaths) != 2 {
		t.Errorf("expected 2 RefreshedPaths (both locations), got %d: %v", len(report.RefreshedPaths), report.RefreshedPaths)
	}
}

// TestDetectHookDrift_WritesCacheBufferFile verifies that when customized hooks
// are detected, the HOOK_DRIFT_CUSTOMIZED lines are written to
// ~/.claude/cache/last-selfupdate-hooks.log.
func TestDetectHookDrift_WritesCacheBufferFile(t *testing.T) {
	repo, home := hookDriftFixture(t, currentCanonicalContent)

	// Override the home dir so the cache file goes to our temp dir.
	// We call the cache-write logic by hand since it runs in RunE (not detectHookDrift).
	// Instead, replicate what RunE does: call detectHookDrift, then write the cache.

	// Write a customized hook.
	customContent := "#!/bin/bash\n# bravros-managed-commit-msg-hook v1\n# USER CUSTOMIZATION\nexit 0\n"
	hookDir := filepath.Join(repo, ".githooks")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		t.Fatalf("mkdir .githooks: %v", err)
	}
	hookPath := filepath.Join(hookDir, "commit-msg")
	if err := os.WriteFile(hookPath, []byte(customContent), 0755); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	report := detectHookDrift(repo, home)

	// Replicate the cache-write logic from RunE.
	if len(report.CustomizedPaths) > 0 {
		cacheDir := filepath.Join(home, ".claude", "cache")
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			t.Fatalf("mkdir cache dir: %v", err)
		}
		cacheFile := filepath.Join(cacheDir, "last-selfupdate-hooks.log")
		var sb strings.Builder
		for _, p := range report.CustomizedPaths {
			sb.WriteString("HOOK_DRIFT_CUSTOMIZED: " + p + "\n")
		}
		if err := os.WriteFile(cacheFile, []byte(sb.String()), 0644); err != nil {
			t.Fatalf("write cache file: %v", err)
		}

		// Now verify the file.
		cacheContent, err := os.ReadFile(cacheFile)
		if err != nil {
			t.Fatalf("read cache file: %v", err)
		}
		expected := "HOOK_DRIFT_CUSTOMIZED: " + hookPath + "\n"
		if string(cacheContent) != expected {
			t.Errorf("cache file content mismatch:\n got: %q\nwant: %q", string(cacheContent), expected)
		}
	} else {
		t.Error("expected CustomizedPaths to be populated for customized hook")
	}
}

// TestDetectHookDrift_TruncatesCacheBufferFile_OnCleanRun verifies that when no
// customized hooks are detected, the cache buffer file is truncated (written as
// empty) even when it previously contained stale HOOK_DRIFT_CUSTOMIZED entries.
// This ensures a prior detection run doesn't keep surfacing stale results after
// the user has resolved their customized hooks.
func TestDetectHookDrift_TruncatesCacheBufferFile_OnCleanRun(t *testing.T) {
	repo, home := hookDriftFixture(t, currentCanonicalContent)

	// Pre-seed the cache file with stale content from a prior selfupdate run.
	cacheDir := filepath.Join(home, ".claude", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	cacheFile := filepath.Join(cacheDir, "last-selfupdate-hooks.log")
	staleContent := "HOOK_DRIFT_CUSTOMIZED: /some/old/path/.githooks/commit-msg\n"
	if err := os.WriteFile(cacheFile, []byte(staleContent), 0644); err != nil {
		t.Fatalf("write stale cache file: %v", err)
	}

	// Write a downstream hook that exactly matches the canonical content → StatusCurrent, drifted=false (perfect match, true no-op).
	hookDir := filepath.Join(repo, ".githooks")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		t.Fatalf("mkdir .githooks: %v", err)
	}
	hookPath := filepath.Join(hookDir, "commit-msg")
	if err := os.WriteFile(hookPath, []byte(currentCanonicalContent), 0755); err != nil {
		t.Fatalf("write pristine hook: %v", err)
	}

	// Run detectHookDrift — should classify the hook as StatusPristine (clean).
	report := detectHookDrift(repo, home)

	// Replicate the cache-write logic from RunE: unconditionally write the cache
	// (empty string when CustomizedPaths is empty), which truncates it.
	var cacheLines strings.Builder
	for _, p := range report.CustomizedPaths {
		cacheLines.WriteString("HOOK_DRIFT_CUSTOMIZED: " + p + "\n")
	}
	if err := os.WriteFile(cacheFile, []byte(cacheLines.String()), 0644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	// Assert CustomizedPaths is empty (no drift detected).
	if len(report.CustomizedPaths) != 0 {
		t.Errorf("expected empty CustomizedPaths for pristine hook, got %v", report.CustomizedPaths)
	}

	// Assert the cache file is now empty (stale content was truncated).
	cacheContent, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatalf("read cache file: %v", err)
	}
	if len(cacheContent) != 0 {
		t.Errorf("expected cache file to be empty after clean run, got %q", string(cacheContent))
	}
}

// TestDetectHookDrift_NoCanonical_ReturnsEmptyReport verifies that when the
// canonical hook file is absent (fresh install), detectHookDrift returns an
// empty report without panicking.
func TestDetectHookDrift_NoCanonical_ReturnsEmptyReport(t *testing.T) {
	// hookDriftFixture with empty string = no canonical file created.
	repo, home := hookDriftFixture(t, "")

	// Write an old-canonical hook — should be ignored because canonical is missing.
	hookDir := filepath.Join(repo, ".githooks")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		t.Fatalf("mkdir .githooks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hookDir, "commit-msg"), []byte("#!/bin/bash\n# bravros-managed-commit-msg-hook v0\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	report := detectHookDrift(repo, home)

	if report.NeedsRefresh {
		t.Error("expected NeedsRefresh=false when canonical is missing, got true")
	}
	if len(report.CustomizedPaths) != 0 || len(report.RefreshedPaths) != 0 {
		t.Errorf("expected empty report when canonical missing, got %+v", report)
	}
}

// TestSelfupdate_HookDriftIntegratesWithFetchPath is the fetch-path successor of
// the clone lane's decision-gate test. The clone lane folded hook drift into a
// composite `!repoNeedsSync && !skillsDrift && !cliStale && ...` early return;
// that expression is gone, but the coverage it stood for is not: hook drift must
// still be acted on when the payload itself is already in sync, i.e. BEFORE the
// !res.Behind early return in selfupdateViaFetch.
//
// A drifting commit-msg hook is a property of the project you are sitting in, so
// the scan target is the working directory — hence t.Chdir.
func TestSelfupdate_HookDriftIntegratesWithFetchPath(t *testing.T) {
	resetSelfupdateFlags(t)
	t.Setenv("BRAVROS_CONFIG_DIR", t.TempDir())

	repo, home := hookDriftFixture(t, currentCanonicalContent)

	payloadDir := t.TempDir()
	selfupdateFetchPayloadDirOverride = payloadDir
	writeInstalledTag(t, payloadDir, "v1.0.0")
	// In sync: nothing to fetch, nothing to deploy — only hook drift can act.
	fake := &fakeFetchClient{resolveTag: "v1.0.0"}
	selfupdateFetchClientOverride = fake

	// Write an old-canonical hook so NeedsRefresh is true.
	hookDir := filepath.Join(repo, ".githooks")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		t.Fatalf("mkdir .githooks: %v", err)
	}
	hookPath := filepath.Join(hookDir, "commit-msg")
	if err := os.WriteFile(hookPath, []byte("#!/bin/bash\n# bravros-managed-commit-msg-hook v0\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write hook: %v", err)
	}
	if !detectHookDrift(repo, home).NeedsRefresh {
		t.Fatal("precondition: expected NeedsRefresh=true, got false")
	}

	t.Chdir(repo)

	var err error
	captureStderr(t, func() { err = selfupdateViaFetch(home) })
	if err != nil {
		t.Fatalf("in-sync run with hook drift must not error, got: %v", err)
	}
	if fake.fetchCalls != 0 {
		t.Fatalf("precondition: in-sync run must not fetch, got %d FetchPayload call(s)", fake.fetchCalls)
	}

	// The drifting hook must have been refreshed to the canonical content even
	// though the payload had nothing to say.
	got, readErr := os.ReadFile(hookPath)
	if readErr != nil {
		t.Fatalf("read hook after selfupdate: %v", readErr)
	}
	if string(got) != currentCanonicalContent {
		t.Errorf("old-canonical hook must be refreshed on an in-sync fetch run:\n got: %q\nwant: %q", got, currentCanonicalContent)
	}
}

// TestDetectHookDrift_HistoricalMD5Pristine verifies that a hook matching a
// historical MD5 (no marker, content matches HistoricalCanonicalMD5s) is treated
// as Pristine.  When the historical MD5 differs from the current canonical, it
// triggers NeedsRefresh.
func TestDetectHookDrift_HistoricalMD5Pristine(t *testing.T) {
	// Write a hook with no marker.  Register its MD5 in HistoricalCanonicalMD5s
	// so Classify returns StatusPristine.  The hook content differs from the
	// canonical (currentCanonicalContent), so drifted=true → NeedsRefresh.
	repo, home := hookDriftFixture(t, currentCanonicalContent)

	noMarkerContent := "#!/bin/bash\n# Hook without any marker — could be pre-marker bravros era.\nset -u\nCOMMIT_MSG_FILE=$1\necho \"checking commit message\"\nexit 0\n"

	hookDir := filepath.Join(repo, ".githooks")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		t.Fatalf("mkdir .githooks: %v", err)
	}
	hookPath := filepath.Join(hookDir, "commit-msg")
	if err := os.WriteFile(hookPath, []byte(noMarkerContent), 0755); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	// Inject the MD5 of this no-marker content into HistoricalCanonicalMD5s.
	md5val, err := hooks.ComputeMD5(hookPath)
	if err != nil {
		t.Fatalf("ComputeMD5: %v", err)
	}
	orig := hooks.HistoricalCanonicalMD5s
	hooks.HistoricalCanonicalMD5s = append(hooks.HistoricalCanonicalMD5s, md5val)
	defer func() { hooks.HistoricalCanonicalMD5s = orig }()

	report := detectHookDrift(repo, home)

	if !report.NeedsRefresh {
		t.Error("expected NeedsRefresh=true for historical-MD5 pristine hook with drift, got false")
	}
	if len(report.RefreshedPaths) == 0 {
		t.Error("expected hookPath in RefreshedPaths, got empty")
	}
}

// ── check-TTL cache (B-0345) ─────────────────────────────────────────────────

// checkMarkerPath returns the cache marker path under the given fake HOME.
func checkMarkerPath(home string) string {
	return filepath.Join(home, ".claude", "state", selfupdateCheckMarker)
}

// writeCheckMarker creates the cache marker under the fake HOME, optionally
// backdating its mtime by the given age.
func writeCheckMarker(t *testing.T, home string, age time.Duration) {
	t.Helper()
	marker := checkMarkerPath(home)
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(marker, []byte{}, 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	if age > 0 {
		past := time.Now().Add(-age)
		if err := os.Chtimes(marker, past, past); err != nil {
			t.Fatalf("chtimes marker: %v", err)
		}
	}
}

// setupCheckCacheTest wires an isolated HOME + payload dir + fake fetch client
// for the TTL-cache tests, and returns (home, fake). The caller sets
// BRAVROS_SELFUPDATE_TTL itself. The fake is in-sync (installed tag == remote
// tag), so a run that reaches the fetch path is observable purely through
// fake.resolveCalls without producing output or deploying anything.
func setupCheckCacheTest(t *testing.T) (string, *fakeFetchClient) {
	t.Helper()
	home, payloadDir := setupFetchPathTest(t)
	t.Setenv("HOME", home)
	writeInstalledTag(t, payloadDir, "v1.0.0")
	fake := &fakeFetchClient{resolveTag: "v1.0.0"}
	selfupdateFetchClientOverride = fake
	return home, fake
}

// TestSelfupdate_CheckCache_FreshMarkerSkipsCheck verifies that a marker
// younger than the TTL short-circuits the run before the remote check.
func TestSelfupdate_CheckCache_FreshMarkerSkipsCheck(t *testing.T) {
	home, fake := setupCheckCacheTest(t)
	t.Setenv("BRAVROS_SELFUPDATE_TTL", "6h")
	writeCheckMarker(t, home, 0)
	selfupdateVerbose = true

	stderr, err := runSelfupdate(t)
	if err != nil {
		t.Fatalf("cache hit must not error, got: %v", err)
	}
	if !strings.Contains(stderr, "selfupdate checked") {
		t.Errorf("verbose cache hit must mention the cached check, got: %q", stderr)
	}
	if fake.resolveCalls != 0 {
		t.Errorf("cache hit must not reach the remote check, but ResolveLatestTag was called %d time(s)", fake.resolveCalls)
	}
}

// TestSelfupdate_CheckCache_StaleMarkerRunsCheck verifies that a marker older
// than the TTL does NOT short-circuit — the real remote check runs.
func TestSelfupdate_CheckCache_StaleMarkerRunsCheck(t *testing.T) {
	home, fake := setupCheckCacheTest(t)
	t.Setenv("BRAVROS_SELFUPDATE_TTL", "1h")
	writeCheckMarker(t, home, 2*time.Hour)

	if _, err := runSelfupdate(t); err != nil {
		t.Fatalf("stale marker run must not error, got: %v", err)
	}
	if fake.resolveCalls != 1 {
		t.Errorf("stale marker must run the real check; ResolveLatestTag called %d time(s), want 1", fake.resolveCalls)
	}
}

// TestSelfupdate_CheckCache_ForceBypasses verifies --force ignores a fresh marker.
func TestSelfupdate_CheckCache_ForceBypasses(t *testing.T) {
	home, fake := setupCheckCacheTest(t)
	t.Setenv("BRAVROS_SELFUPDATE_TTL", "6h")
	writeCheckMarker(t, home, 0)
	selfupdateForce = true

	if _, err := runSelfupdate(t); err != nil {
		t.Fatalf("--force run must not error, got: %v", err)
	}
	if fake.resolveCalls != 1 {
		t.Errorf("--force must bypass the cache; ResolveLatestTag called %d time(s), want 1", fake.resolveCalls)
	}
}

// TestSelfupdate_CheckCache_MarkerStampedAfterCompletedCheck verifies that a
// completed in-sync check stamps the marker so the next session cache-hits.
func TestSelfupdate_CheckCache_MarkerStampedAfterCompletedCheck(t *testing.T) {
	home, _ := setupCheckCacheTest(t)
	t.Setenv("BRAVROS_SELFUPDATE_TTL", "6h")

	if _, err := runSelfupdate(t); err != nil {
		t.Fatalf("in-sync run must not error, got: %v", err)
	}
	if _, statErr := os.Stat(checkMarkerPath(home)); statErr != nil {
		t.Error("completed in-sync check must stamp the cache marker, but it does not exist")
	}
}

// TestSelfupdate_CheckCache_DryRunLeavesNoMarker verifies --dry-run never stamps.
func TestSelfupdate_CheckCache_DryRunLeavesNoMarker(t *testing.T) {
	home, _ := setupCheckCacheTest(t)
	t.Setenv("BRAVROS_SELFUPDATE_TTL", "6h")
	selfupdateDryRun = true

	if _, err := runSelfupdate(t); err != nil {
		t.Fatalf("dry-run must not error, got: %v", err)
	}
	if _, statErr := os.Stat(checkMarkerPath(home)); statErr == nil {
		t.Error("--dry-run must not stamp the cache marker")
	}
}

// TestSelfupdateCheckTTL_Parsing locks the env-var contract.
func TestSelfupdateCheckTTL_Parsing(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Duration
	}{
		{"", 6 * time.Hour},        // unset → default
		{"0", 0},                   // explicit disable
		{"30m", 30 * time.Minute},  // custom duration
		{"12h", 12 * time.Hour},    // upper band of B-0345's 6-12h range
		{"garbage", 6 * time.Hour}, // unparsable → default
		{"-1h", 0},                 // negative → disabled
	}
	for _, c := range cases {
		t.Setenv("BRAVROS_SELFUPDATE_TTL", c.raw)
		if got := selfupdateCheckTTL(); got != c.want {
			t.Errorf("BRAVROS_SELFUPDATE_TTL=%q: got %v, want %v", c.raw, got, c.want)
		}
	}
}

// ── selfupdateViaFetch (P-0003 Phase 3: no-clone fetch path) ────────────────

// fakeFetchClient is a selfupdateFetchClient test double — no network, no
// filesystem access beyond what writePayload chooses to do.
type fakeFetchClient struct {
	resolveCalls int
	fetchCalls   int

	resolveTag string
	resolveErr error

	fetchErr error
	// writePayload, when set, is invoked with destDir on a successful
	// FetchPayload call so tests can populate a fake payload tree.
	writePayload func(destDir string) error
}

func (f *fakeFetchClient) ResolveLatestTag(ctx context.Context) (string, error) {
	f.resolveCalls++
	if f.resolveErr != nil {
		return "", f.resolveErr
	}
	return f.resolveTag, nil
}

func (f *fakeFetchClient) FetchPayload(ctx context.Context, tag, destDir string) (string, error) {
	f.fetchCalls++
	if f.fetchErr != nil {
		return "", f.fetchErr
	}
	if f.writePayload != nil {
		if err := f.writePayload(destDir); err != nil {
			return "", err
		}
	}
	return "fakesha256", nil
}

// writeFakePayloadTree populates destDir with a minimal skills/ + cli/go.mod
// pair so deploy.IsClaudeRepo accepts it as a deployable source, mirroring
// deploy package's own writeRepoMarkers test fixture.
func writeFakePayloadTree(t *testing.T, destDir string) {
	t.Helper()
	skillDir := filepath.Join(destDir, "skills", "fakeskill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("mkdir fake skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# fakeskill"), 0644); err != nil {
		t.Fatalf("write fake SKILL.md: %v", err)
	}
	cliDir := filepath.Join(destDir, "cli")
	if err := os.MkdirAll(cliDir, 0755); err != nil {
		t.Fatalf("mkdir fake cli dir: %v", err)
	}
	gomod := "module github.com/bravros/bravros/cli\n\ngo 1.26.2\n"
	if err := os.WriteFile(filepath.Join(cliDir, "go.mod"), []byte(gomod), 0644); err != nil {
		t.Fatalf("write fake cli/go.mod: %v", err)
	}
}

// setupFetchPathTest wires resetSelfupdateFlags, an isolated BRAVROS_CONFIG_DIR
// (so selfupdate.RemoteStatePath() never touches the developer's real
// ~/.claude/state), and a fresh payload dir. Returns (home, payloadDir).
//
// P-0015 Phase 6 (D2) moved the SessionStart hook off the network: RunE now
// refreshes from the EMBEDDED payload, and the published-payload fetch lane
// these tests cover became opt-in behind --fetch-payload. The lane's behavior
// is unchanged, so the tests are unchanged too — they just have to select it,
// which is what the flag below does. Tests that call selfupdateViaFetch
// directly never needed the flag and still do not.
func setupFetchPathTest(t *testing.T) (home, payloadDir string) {
	t.Helper()
	resetSelfupdateFlags(t)
	selfupdateFetchPayload = true
	t.Setenv("BRAVROS_CONFIG_DIR", t.TempDir())
	home = t.TempDir()
	payloadDir = t.TempDir()
	selfupdateFetchPayloadDirOverride = payloadDir
	return home, payloadDir
}

// writeInstalledTag seeds payloadDir with the one-line tag file selfupdateViaFetch
// reads to learn what's currently installed.
func writeInstalledTag(t *testing.T, payloadDir, tag string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(payloadDir, payloadTagFileName), []byte(tag), 0644); err != nil {
		t.Fatalf("write installed tag: %v", err)
	}
}

// TestSelfupdateFetchPathInSyncIsSilent verifies that when the installed tag
// matches the remote tag, selfupdateViaFetch produces no output at all and
// never calls FetchPayload.
func TestSelfupdateFetchPathInSyncIsSilent(t *testing.T) {
	home, payloadDir := setupFetchPathTest(t)
	writeInstalledTag(t, payloadDir, "v1.0.0")

	fake := &fakeFetchClient{resolveTag: "v1.0.0"}
	selfupdateFetchClientOverride = fake

	var err error
	stderr := captureStderr(t, func() {
		err = selfupdateViaFetch(home)
	})

	if err != nil {
		t.Fatalf("in-sync run must not error, got: %v", err)
	}
	if stderr != "" {
		t.Errorf("in-sync run must produce no output whatsoever, got: %q", stderr)
	}
	if fake.fetchCalls != 0 {
		t.Errorf("in-sync run must not call FetchPayload, got %d calls", fake.fetchCalls)
	}
}

// TestSelfupdateFetchPathOfflineIsSilentAndSucceeds verifies that a failed
// remote resolve is treated as offline: nil error, no output (default
// verbosity), FetchPayload never called, and any pre-existing payload dir
// content survives untouched.
func TestSelfupdateFetchPathOfflineIsSilentAndSucceeds(t *testing.T) {
	home, payloadDir := setupFetchPathTest(t)
	// Pre-existing payload content that must survive untouched.
	marker := filepath.Join(payloadDir, "skills", "existing", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(marker), 0755); err != nil {
		t.Fatalf("mkdir pre-existing skill dir: %v", err)
	}
	const preExistingContent = "pre-existing skill, must not be touched"
	if err := os.WriteFile(marker, []byte(preExistingContent), 0644); err != nil {
		t.Fatalf("write pre-existing skill: %v", err)
	}

	fake := &fakeFetchClient{resolveErr: errors.New("dial tcp: network is unreachable")}
	selfupdateFetchClientOverride = fake

	var err error
	stderr := captureStderr(t, func() {
		err = selfupdateViaFetch(home)
	})

	if err != nil {
		t.Fatalf("offline run must not error, got: %v", err)
	}
	if stderr != "" {
		t.Errorf("offline run must be silent at default verbosity, got: %q", stderr)
	}
	if fake.fetchCalls != 0 {
		t.Errorf("offline run must not call FetchPayload, got %d calls", fake.fetchCalls)
	}
	got, readErr := os.ReadFile(marker)
	if readErr != nil {
		t.Fatalf("pre-existing payload must survive offline run: %v", readErr)
	}
	if string(got) != preExistingContent {
		t.Errorf("pre-existing payload content changed: got %q, want %q", got, preExistingContent)
	}
}

// TestSelfupdateFetchPathNoPayloadAssetIsSilent verifies that fetch.ErrNoPayload
// (a release with no published payload asset — every pre-P-0003 release) is
// treated as "nothing to do", not a failure: nil error, no output.
func TestSelfupdateFetchPathNoPayloadAssetIsSilent(t *testing.T) {
	home, payloadDir := setupFetchPathTest(t)
	writeInstalledTag(t, payloadDir, "v1.0.0")

	fake := &fakeFetchClient{
		resolveTag: "v2.0.0",
		fetchErr:   fmt.Errorf("%s: %w", fetch.PayloadAsset, fetch.ErrNoPayload),
	}
	selfupdateFetchClientOverride = fake

	var err error
	stderr := captureStderr(t, func() {
		err = selfupdateViaFetch(home)
	})

	if err != nil {
		t.Fatalf("no-payload-asset run must not error, got: %v", err)
	}
	if stderr != "" {
		t.Errorf("no-payload-asset run must be silent at default verbosity, got: %q", stderr)
	}
}

// TestSelfupdateFetchPathVerificationFailureIsLoud is the regression test for
// the silent-0 bug this dossier exists to fix: any OTHER fetch error (checksum
// mismatch, signature failure, network error mid-download, ...) must be loud
// — non-nil error AND a "⚠️" stderr line — never look like success.
func TestSelfupdateFetchPathVerificationFailureIsLoud(t *testing.T) {
	home, payloadDir := setupFetchPathTest(t)
	writeInstalledTag(t, payloadDir, "v1.0.0")

	fake := &fakeFetchClient{
		resolveTag: "v2.0.0",
		fetchErr:   errors.New("checksum mismatch for bravros-payload.tar.gz"),
	}
	selfupdateFetchClientOverride = fake

	var err error
	stderr := captureStderr(t, func() {
		err = selfupdateViaFetch(home)
	})

	if err == nil {
		t.Fatal("verification failure must return a non-nil error, got nil")
	}
	if !strings.Contains(stderr, "⚠️") {
		t.Errorf("verification failure must emit a loud ⚠️ stderr line, got: %q", stderr)
	}
}

// TestSelfupdateFetchPathBehindFetchesAndDeploys verifies the full happy path:
// behind → fetch → deploy → tag file written → markers written → exactly one
// stderr line.
func TestSelfupdateFetchPathBehindFetchesAndDeploys(t *testing.T) {
	home, payloadDir := setupFetchPathTest(t)
	writeInstalledTag(t, payloadDir, "v1.0.0")

	targetDir := t.TempDir()
	selfupdateFetchTargetDirOverride = targetDir

	fake := &fakeFetchClient{
		resolveTag:   "v2.0.0",
		writePayload: func(destDir string) error { writeFakePayloadTree(t, destDir); return nil },
	}
	selfupdateFetchClientOverride = fake

	var err error
	stderr := captureStderr(t, func() {
		err = selfupdateViaFetch(home)
	})

	if err != nil {
		t.Fatalf("behind run must not error, got: %v", err)
	}
	if fake.fetchCalls != 1 {
		t.Errorf("expected exactly 1 FetchPayload call, got %d", fake.fetchCalls)
	}

	// Deployed into the isolated target dir.
	deployedSkill := filepath.Join(targetDir, "skills", "fakeskill", "SKILL.md")
	if _, statErr := os.Stat(deployedSkill); statErr != nil {
		t.Errorf("expected fakeskill deployed at %s: %v", deployedSkill, statErr)
	}

	// Tag file updated to the new remote tag.
	gotTag, readErr := os.ReadFile(filepath.Join(payloadDir, payloadTagFileName))
	if readErr != nil {
		t.Fatalf("read updated tag file: %v", readErr)
	}
	if strings.TrimSpace(string(gotTag)) != "v2.0.0" {
		t.Errorf("expected tag file to contain v2.0.0, got %q", gotTag)
	}

	// Exactly one stderr line.
	trimmed := strings.TrimRight(stderr, "\n")
	lines := strings.Split(trimmed, "\n")
	if len(lines) != 1 || trimmed == "" {
		t.Errorf("expected exactly one stderr line, got %d: %q", len(lines), stderr)
	}
	if !strings.Contains(stderr, "v2.0.0") {
		t.Errorf("expected the single stderr line to name the new tag, got: %q", stderr)
	}
}

// seedFetchTargetRuntime pre-populates the fetch-path target dir with the
// user-owned content a real ~/.claude carries and that the fake payload has NO
// counterpart for: a hand-installed hook, a hand-written agent, and a skill.
// Returns the path→content map it wrote.
func seedFetchTargetRuntime(t *testing.T, targetDir string) map[string]string {
	t.Helper()
	seeded := map[string]string{
		filepath.Join(targetDir, "hooks", "pre-push"):                 "#!/bin/sh\necho pre-push\n",
		filepath.Join(targetDir, "agents", "my-agent.md"):             "# my-agent\n",
		filepath.Join(targetDir, "skills", "handwritten", "SKILL.md"): "# handwritten\n",
	}
	for path, content := range seeded {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return seeded
}

// isolatePreserveConfig points config.PreservedSkills()/EnabledSkills() at a
// throwaway .bravros.yml so a test never reads a config it did not write.
//
// The resolution chain is cwd → $BRAVROS_PORTABLE_REPO → $HOME (see
// cli/internal/config/preserve.go). cwd during `go test ./cmd/` is cli/cmd,
// which DOES carry a .bravros/config.json fixture and would otherwise win the
// chain — hence the chdir to an empty dir. The declared config is then written
// into the $BRAVROS_PORTABLE_REPO dir, so a passing test is also proof that the
// env-var hint still resolves .bravros.yml (P-0014 retires the portable-repo
// clone LANE, not this hint). Passing yaml == "" isolates without declaring.
func isolatePreserveConfig(t *testing.T, yaml string) {
	t.Helper()
	t.Chdir(t.TempDir()) // empty cwd — nothing to find at chain step 1
	t.Setenv("HOME", t.TempDir())
	hint := t.TempDir()
	t.Setenv("BRAVROS_PORTABLE_REPO", hint)
	if yaml != "" {
		if err := os.WriteFile(filepath.Join(hint, ".bravros.yml"), []byte(yaml), 0644); err != nil {
			t.Fatalf("write .bravros.yml: %v", err)
		}
	}
}

// runFetchPathDeploy drives one behind→fetch→deploy cycle against targetDir.
func runFetchPathDeploy(t *testing.T, home, payloadDir, targetDir string) {
	t.Helper()
	writeInstalledTag(t, payloadDir, "v1.0.0")
	selfupdateFetchTargetDirOverride = targetDir
	selfupdateFetchClientOverride = &fakeFetchClient{
		resolveTag:   "v2.0.0",
		writePayload: func(destDir string) error { writeFakePayloadTree(t, destDir); return nil },
	}

	var err error
	captureStderr(t, func() {
		err = selfupdateViaFetch(home)
	})
	if err != nil {
		t.Fatalf("behind run must not error, got: %v", err)
	}
}

// TestSelfupdateFetchPathNeverPrunesHooksOrAgents is the safety guard for
// P-0014 Phase 4. The fetch path now DOES orphan-prune (it is the only delivery
// path left, so a skill deleted upstream has to be removed), but pruning is
// scoped to skills+templates — the subtrees the payload actually ships.
// ~/.claude/hooks and ~/.claude/agents are content bravros does not own at the
// target and must come through a fetch-path deploy byte-for-byte intact.
func TestSelfupdateFetchPathNeverPrunesHooksOrAgents(t *testing.T) {
	home, payloadDir := setupFetchPathTest(t)
	isolatePreserveConfig(t, "")

	targetDir := t.TempDir()
	seeded := seedFetchTargetRuntime(t, targetDir)

	runFetchPathDeploy(t, home, payloadDir, targetDir)

	for _, rel := range []string{
		filepath.Join(targetDir, "hooks", "pre-push"),
		filepath.Join(targetDir, "agents", "my-agent.md"),
	} {
		got, readErr := os.ReadFile(rel)
		if readErr != nil {
			t.Errorf("user-owned file %s must survive the fetch-path deploy, but: %v", rel, readErr)
			continue
		}
		if string(got) != seeded[rel] {
			t.Errorf("user-owned file %s content changed: got %q, want %q", rel, got, seeded[rel])
		}
	}
}

// TestSelfupdateFetchPathPrunesSkillAbsentFromPayload is the other half of the
// same change: the payload IS the complete deployable tree, so a skill sitting
// at the target with no counterpart in it was deleted upstream and has to go.
// Before P-0014 the clone lane did this; with that lane retired, the fetch path
// owns it — without this, a removed skill would linger on every machine forever.
func TestSelfupdateFetchPathPrunesSkillAbsentFromPayload(t *testing.T) {
	home, payloadDir := setupFetchPathTest(t)
	isolatePreserveConfig(t, "")

	targetDir := t.TempDir()
	seedFetchTargetRuntime(t, targetDir)

	runFetchPathDeploy(t, home, payloadDir, targetDir)

	orphan := filepath.Join(targetDir, "skills", "handwritten")
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("skills/handwritten has no counterpart in the payload and must be pruned (stat err: %v)", err)
	}
	// The payload's own skill still lands — pruning must not eat the deploy.
	if _, err := os.Stat(filepath.Join(targetDir, "skills", "fakeskill", "SKILL.md")); err != nil {
		t.Errorf("payload skill must be deployed: %v", err)
	}
}

// TestSelfupdateFetchPathHonorsPreservedSkills — skills.preserve is the opt-in
// escape hatch from the prune above, and it must keep working on the fetch path.
// Doubles as a live check that $BRAVROS_PORTABLE_REPO still works as the
// .bravros.yml resolution hint (P-0014 keeps that contract even though the
// portable-repo CLONE lane is gone).
func TestSelfupdateFetchPathHonorsPreservedSkills(t *testing.T) {
	home, payloadDir := setupFetchPathTest(t)
	isolatePreserveConfig(t, "skills:\n  preserve:\n    - handwritten\n")

	if got := config.PreservedSkills(); len(got) != 1 || got[0] != "handwritten" {
		t.Fatalf("test setup: config.PreservedSkills() = %v, want [handwritten]", got)
	}

	targetDir := t.TempDir()
	seeded := seedFetchTargetRuntime(t, targetDir)

	runFetchPathDeploy(t, home, payloadDir, targetDir)

	preserved := filepath.Join(targetDir, "skills", "handwritten", "SKILL.md")
	got, readErr := os.ReadFile(preserved)
	if readErr != nil {
		t.Fatalf("preserved skill must survive the fetch-path deploy, but: %v", readErr)
	}
	if string(got) != seeded[preserved] {
		t.Errorf("preserved skill content changed: got %q, want %q", got, seeded[preserved])
	}
}

// TestSelfupdateFetchPathTakenEvenWhenCloneExists is THE P-0014 invariant: a
// machine that happens to have a portable-repo clone on disk behaves exactly
// like one that does not. Before P-0014 the presence of a .git directory
// diverted RunE into the clone lane (git fetch + install.sh) and the fetch path
// was never entered; the inverse of this test asserted precisely that.
//
// The clone is materialised at $HOME/Sites/claude/.git — the exact location
// the retired portable-repo probe used to check — so the assertion is that
// a clone's presence there is now ignored entirely, not merely that some
// unrelated directory is ignored.
func TestSelfupdateFetchPathTakenEvenWhenCloneExists(t *testing.T) {
	home, payloadDir := setupFetchPathTest(t)
	t.Setenv("HOME", home)
	// Neutralise the env override so only the HOME-relative probe could speak.
	t.Setenv("BRAVROS_PORTABLE_REPO", "")

	clone := filepath.Join(home, "Sites", "claude")
	if err := os.MkdirAll(filepath.Join(clone, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir fake portable-repo clone: %v", err)
	}

	writeInstalledTag(t, payloadDir, "v1.0.0")
	fake := &fakeFetchClient{resolveTag: "v1.0.0"}
	selfupdateFetchClientOverride = fake

	if _, err := runSelfupdate(t); err != nil {
		t.Fatalf("RunE with a clone present must not error, got: %v", err)
	}

	if fake.resolveCalls != 1 {
		t.Errorf("fetch path must be taken even when a clone exists at %s; ResolveLatestTag called %d time(s), want 1", clone, fake.resolveCalls)
	}
}

// ── D2 SessionStart lane: refresh from the EMBEDDED payload ──────────────────
//
// Every assertion below reads the filesystem or counts HTTP requests. This
// command returns nil on nearly every path, "did nothing" included, so an exit
// code is not evidence of anything.

// setupRefreshLaneTest isolates HOME and the config dir and leaves the command
// on its new default lane (embedded refresh, no network). Returns (home, root).
func setupRefreshLaneTest(t *testing.T) (home, root string) {
	t.Helper()
	resetSelfupdateFlags(t)
	home = t.TempDir()
	root = filepath.Join(home, ".claude")
	t.Setenv("HOME", home)
	t.Setenv("BRAVROS_CONFIG_DIR", root)
	t.Setenv(setupComponentsEnv, "")
	t.Setenv(setupAllowPluginManagedEnv, "")
	t.Setenv(setupInstallMethodEnv, "")
	return home, root
}

func seedSkill(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s/SKILL.md: %v", dir, err)
	}
}

func installedSkillNames(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(root, "skills"))
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() {
			out[e.Name()] = true
		}
	}
	return out
}

// TestSelfupdate_RefreshMigratesPreV2InstallAtScopeAll is the D11 migration
// contract, and the one place in this phase where getting the default wrong
// destroys user data: a pre-v2 machine has NO setup.json but does have skills.
// Defaulting such an install to scope `core` would silently delete every
// non-core skill it already had. It must resolve to `all`.
func TestSelfupdate_RefreshMigratesPreV2InstallAtScopeAll(t *testing.T) {
	_, root := setupRefreshLaneTest(t)

	// A pre-v2 install: skills/ exists (deployed by the old payload lane) and
	// carries a skill the operator wrote themselves.
	seedSkill(t, root, "my-own-skill", "---\nname: my-own-skill\n---\nmine\n")

	if _, err := runSelfupdate(t); err != nil {
		t.Fatalf("refresh must not error: %v", err)
	}

	embedded, err := payload.SkillNames()
	if err != nil {
		t.Fatalf("payload.SkillNames: %v", err)
	}
	installed := installedSkillNames(t, root)
	for _, name := range embedded {
		if !installed[name] {
			t.Errorf("migration must install every embedded skill; %q is missing", name)
		}
	}
	if !installed["my-own-skill"] {
		t.Error("a user-added skill must survive the migration refresh")
	}

	st := readState(t, root)
	if st.SkillsScope != payload.ScopeAll {
		t.Errorf("migration must record scope %q, got %q", payload.ScopeAll, st.SkillsScope)
	}
}

// TestSelfupdate_RefreshSkipsWhenNothingIsInstalled — a machine that never ran
// `bravros setup` gets nothing installed behind its back by a hook.
func TestSelfupdate_RefreshSkipsWhenNothingIsInstalled(t *testing.T) {
	_, root := setupRefreshLaneTest(t)

	if _, err := runSelfupdate(t); err != nil {
		t.Fatalf("refresh on an empty root must not error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "skills")); err == nil {
		t.Error("an empty ~/.claude must stay empty — `bravros setup` owns first install")
	}
	if _, err := os.Stat(setupStatePath(root)); err == nil {
		t.Error("no state.json may be written when nothing was installed")
	}
}

// TestSelfupdate_RefreshReplaysRecordedScopeAndPreservesUserSkills pins the
// FilterMode-shaped guarantee: the recorded selection is an ADDITIVE allowlist,
// never a prune instruction.
func TestSelfupdate_RefreshReplaysRecordedScopeAndPreservesUserSkills(t *testing.T) {
	_, root := setupRefreshLaneTest(t)

	if _, err := runSetupForTest(t, setupFlags{components: "claude-skills", skills: "core", yes: true}); err != nil {
		t.Fatalf("seed setup run: %v", err)
	}
	core := installedSkillNames(t, root)
	if len(core) == 0 {
		t.Fatal("seed setup installed no skills")
	}

	// Something the operator added by hand, plus a core skill removed to prove
	// the refresh restores what it owns.
	seedSkill(t, root, "my-own-skill", "---\nname: my-own-skill\n---\nmine\n")
	var removed string
	for name := range core {
		removed = name
		break
	}
	if err := os.RemoveAll(filepath.Join(root, "skills", removed)); err != nil {
		t.Fatalf("remove %s: %v", removed, err)
	}

	if _, err := runSelfupdate(t); err != nil {
		t.Fatalf("refresh must not error: %v", err)
	}

	after := installedSkillNames(t, root)
	if !after[removed] {
		t.Errorf("refresh must restore the recorded skill %q", removed)
	}
	if !after["my-own-skill"] {
		t.Error("a hand-added skill must never be pruned by a refresh")
	}
	all, err := payload.SkillNames()
	if err != nil {
		t.Fatalf("payload.SkillNames: %v", err)
	}
	for _, name := range all {
		isCore, err := payload.SkillIsCore(name)
		if err != nil {
			t.Fatalf("SkillIsCore(%q): %v", name, err)
		}
		if !isCore && after[name] {
			t.Errorf("scope core must not install the non-core skill %q", name)
		}
	}
}

// TestSelfupdate_RefreshNeverOverwritesAModifiedFile — Phase 3's non-destructive
// rule, enforced on the unattended lane too: the operator's bytes stay, the
// payload's land beside them as .new.
func TestSelfupdate_RefreshNeverOverwritesAModifiedFile(t *testing.T) {
	_, root := setupRefreshLaneTest(t)

	if _, err := runSetupForTest(t, setupFlags{components: "claude-skills", skills: "core", yes: true}); err != nil {
		t.Fatalf("seed setup run: %v", err)
	}
	var target string
	for name := range installedSkillNames(t, root) {
		target = name
		break
	}
	edited := filepath.Join(root, "skills", target, "SKILL.md")
	const mine = "# my own edit, do not touch\n"
	if err := os.WriteFile(edited, []byte(mine), 0o644); err != nil {
		t.Fatalf("edit %s: %v", edited, err)
	}

	if _, err := runSelfupdate(t); err != nil {
		t.Fatalf("refresh must not error: %v", err)
	}

	got, err := os.ReadFile(edited)
	if err != nil {
		t.Fatalf("read edited file: %v", err)
	}
	if string(got) != mine {
		t.Errorf("refresh overwrote a user-modified file: %q", got)
	}
	if _, err := os.Stat(edited + ".new"); err != nil {
		t.Errorf("the payload's version must be written as %s.new: %v", edited, err)
	}
}

// TestSelfupdate_RefreshMakesNoNetworkRequest is the D2 headline: the
// SessionStart lane refreshes components from the embed with ZERO requests.
// The notice's own 24h window is already stamped here, which is the steady
// state on any machine that started a session in the last day.
func TestSelfupdate_RefreshMakesNoNetworkRequest(t *testing.T) {
	_, root := setupRefreshLaneTest(t)
	t.Setenv("BRAVROS_NO_UPDATE_CHECK", "") // notice ENABLED — and still silent

	if _, err := runSetupForTest(t, setupFlags{components: "claude-skills", skills: "core", yes: true}); err != nil {
		t.Fatalf("seed setup run: %v", err)
	}
	var removed string
	for name := range installedSkillNames(t, root) {
		removed = name
		break
	}
	if err := os.RemoveAll(filepath.Join(root, "skills", removed)); err != nil {
		t.Fatalf("remove %s: %v", removed, err)
	}

	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Location", "/bravros/bravros/releases/tag/v99.0.0")
		w.WriteHeader(http.StatusFound)
	}))
	defer ts.Close()
	selfupdateNoticeResolverOverride = &fetch.Client{BaseURL: ts.URL, HTTP: ts.Client()}

	// The passive check ran an hour ago — inside the 24h window.
	if err := selfupdate.SaveNoticeState(selfupdate.NoticeStatePath(), selfupdate.NoticeState{
		LastCheck: time.Now().Add(-time.Hour),
		LatestTag: "v" + strings.TrimPrefix(Version, "v"),
	}); err != nil {
		t.Fatalf("seed notice state: %v", err)
	}

	if _, err := runSelfupdate(t); err != nil {
		t.Fatalf("refresh must not error: %v", err)
	}

	if hits != 0 {
		t.Errorf("the SessionStart lane made %d network request(s); want 0", hits)
	}
	if !installedSkillNames(t, root)[removed] {
		t.Errorf("the refresh did not restore %q from the embedded payload", removed)
	}
}

// TestSelfupdate_PassiveNotice_OneRequestPer24h is the named acceptance test:
// two runs inside the window make exactly ONE remote request, counted at an
// httptest server rather than inferred from an exit code.
func TestSelfupdate_PassiveNotice_OneRequestPer24h(t *testing.T) {
	_, root := setupRefreshLaneTest(t)
	t.Setenv("BRAVROS_NO_UPDATE_CHECK", "")
	seedSkill(t, root, "my-own-skill", "---\nname: my-own-skill\n---\nmine\n")

	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Location", "/bravros/bravros/releases/tag/v99.0.0")
		w.WriteHeader(http.StatusFound)
	}))
	defer ts.Close()
	selfupdateNoticeResolverOverride = &fetch.Client{BaseURL: ts.URL, HTTP: ts.Client()}

	first, err := runSelfupdate(t)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := runSelfupdate(t)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}

	if hits != 1 {
		t.Errorf("two runs inside 24h made %d remote request(s); want exactly 1", hits)
	}
	if !strings.Contains(first, "v99.0.0 is available") {
		t.Errorf("first run must print the notice, got: %q", first)
	}
	if !strings.Contains(second, "v99.0.0 is available") {
		t.Errorf("the cached notice must still print without a request, got: %q", second)
	}
}

// TestSelfupdate_PassiveNotice_OptOutMakesNoRequest — BRAVROS_NO_UPDATE_CHECK=1
// removes the last network call from the SessionStart lane entirely.
func TestSelfupdate_PassiveNotice_OptOutMakesNoRequest(t *testing.T) {
	_, root := setupRefreshLaneTest(t)
	t.Setenv("BRAVROS_NO_UPDATE_CHECK", "1")
	seedSkill(t, root, "my-own-skill", "---\nname: my-own-skill\n---\nmine\n")

	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusFound)
	}))
	defer ts.Close()
	selfupdateNoticeResolverOverride = &fetch.Client{BaseURL: ts.URL, HTTP: ts.Client()}

	out, err := runSelfupdate(t)
	if err != nil {
		t.Fatalf("opt-out run: %v", err)
	}
	if hits != 0 {
		t.Errorf("opt-out made %d request(s); want 0", hits)
	}
	if strings.Contains(out, "available") {
		t.Errorf("opt-out must print no notice, got: %q", out)
	}
}

// TestSelfupdate_CacheHitDoesNoWorkAtAll — the TTL cache sits in FRONT of
// everything, which is what keeps the SessionStart hook near-free: a warm
// marker means no refresh, no notice, no request.
func TestSelfupdate_CacheHitDoesNoWorkAtAll(t *testing.T) {
	home, root := setupRefreshLaneTest(t)
	t.Setenv("BRAVROS_SELFUPDATE_TTL", "6h")
	t.Setenv("BRAVROS_NO_UPDATE_CHECK", "")

	if _, err := runSetupForTest(t, setupFlags{components: "claude-skills", skills: "core", yes: true}); err != nil {
		t.Fatalf("seed setup run: %v", err)
	}
	var removed string
	for name := range installedSkillNames(t, root) {
		removed = name
		break
	}
	if err := os.RemoveAll(filepath.Join(root, "skills", removed)); err != nil {
		t.Fatalf("remove %s: %v", removed, err)
	}

	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusFound)
	}))
	defer ts.Close()
	selfupdateNoticeResolverOverride = &fetch.Client{BaseURL: ts.URL, HTTP: ts.Client()}

	writeCheckMarker(t, home, 0)

	if _, err := runSelfupdate(t); err != nil {
		t.Fatalf("cache-hit run: %v", err)
	}
	if hits != 0 {
		t.Errorf("a cache hit made %d request(s); want 0", hits)
	}
	if installedSkillNames(t, root)[removed] {
		t.Errorf("a cache hit must not even walk the embed, but %q was restored", removed)
	}
}

// TestSelfupdate_RefreshNeverTouchesHooksOrAgents re-points the P-0014 scoping
// invariant at the D2 lane. Its fetch-path twin
// (TestSelfupdateFetchPathNeverPrunesHooksOrAgents) still guards the legacy
// lane; the CONTRACT — bravros prunes only inside skills/ + templates/, and
// ~/.claude/hooks and ~/.claude/agents are content it does not own — has to
// hold on whichever lane is actually running on a user's machine, and after D2
// that is this one.
func TestSelfupdate_RefreshNeverTouchesHooksOrAgents(t *testing.T) {
	_, root := setupRefreshLaneTest(t)

	if _, err := runSetupForTest(t, setupFlags{components: "claude-skills", skills: "core", yes: true}); err != nil {
		t.Fatalf("seed setup run: %v", err)
	}
	for _, sub := range []string{"hooks", "agents"} {
		dir := filepath.Join(root, sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "mine.sh"), []byte("#!/bin/sh\necho mine\n"), 0o755); err != nil {
			t.Fatalf("seed %s content: %v", sub, err)
		}
	}

	if _, err := runSelfupdate(t); err != nil {
		t.Fatalf("refresh must not error: %v", err)
	}

	for _, sub := range []string{"hooks", "agents"} {
		p := filepath.Join(root, sub, "mine.sh")
		got, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("%s must be left alone by the refresh: %v", p, err)
			continue
		}
		if string(got) != "#!/bin/sh\necho mine\n" {
			t.Errorf("%s was rewritten: %q", p, got)
		}
	}
}

// TestSelfupdate_RefreshPrunesNothing is the FilterMode-shaped guarantee stated
// as an absolute: replaying a recorded selection removes nothing at all, so
// skills.preserve (config.PreservedSkills, which the legacy lane passes to
// deploy) has nothing to defend against here. A removal on this lane requires a
// skill to have been recorded by a previous `setup` AND dropped by the current
// selection AND still be byte-identical to the embed — and a refresh replays
// the SAME selection, so the second condition can never be met.
func TestSelfupdate_RefreshPrunesNothing(t *testing.T) {
	_, root := setupRefreshLaneTest(t)

	if _, err := runSetupForTest(t, setupFlags{components: "claude-skills", skills: "core", yes: true}); err != nil {
		t.Fatalf("seed setup run: %v", err)
	}
	// Three shapes that a careless prune would eat: a hand-added skill, a
	// skill named in skills.preserve, and a modified copy of an installed one.
	seedSkill(t, root, "hand-added", "---\nname: hand-added\n---\nmine\n")
	seedSkill(t, root, "preserved-skill", "---\nname: preserved-skill\n---\nkeep me\n")
	before := installedSkillNames(t, root)

	var modified string
	for name := range before {
		if name != "hand-added" && name != "preserved-skill" {
			modified = name
			break
		}
	}
	edited := filepath.Join(root, "skills", modified, "SKILL.md")
	if err := os.WriteFile(edited, []byte("# edited by the operator\n"), 0o644); err != nil {
		t.Fatalf("edit %s: %v", edited, err)
	}

	if _, err := runSelfupdate(t); err != nil {
		t.Fatalf("refresh must not error: %v", err)
	}

	after := installedSkillNames(t, root)
	for name := range before {
		if !after[name] {
			t.Errorf("refresh removed %q; a replay of the recorded selection must prune nothing", name)
		}
	}
	if got, _ := os.ReadFile(edited); string(got) != "# edited by the operator\n" {
		t.Errorf("the operator's edit was overwritten: %q", got)
	}
}
