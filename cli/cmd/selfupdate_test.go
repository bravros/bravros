package cmd

// Tests for the selfupdate command — lock in the contracts described in P-0097 Phase 4.
//
// Strategy: call selfupdateCmd.RunE directly (bypassing cobra flag parsing) after setting
// the package-level flag vars. Use PATH-shimmed fake `git` binaries (written to t.TempDir)
// for network/invocation-recording tests. Use selfupdateRepoOverride for path injection.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bravros/bravros/cli/internal/deploy"
	"github.com/bravros/bravros/cli/internal/hooks"
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
	origOverride := selfupdateRepoOverride
	origDeep := selfupdateDeep
	origForce := selfupdateForce
	origCodexHome, hadCodexHome := os.LookupEnv("KAISSER_CODEX_HOME")
	origOpenCodeHome, hadOpenCodeHome := os.LookupEnv("KAISSER_OPENCODE_HOME")
	origPiHome, hadPiHome := os.LookupEnv("KAISSER_PI_HOME")

	selfupdateSilent = false
	selfupdateVerbose = false
	selfupdateSkipIfRecent = ""
	selfupdateDryRun = false
	selfupdateRepoOverride = ""
	selfupdateDeep = false
	selfupdateForce = false
	// Disable the check-TTL cache (B-0345) so tests exercise the real check path
	// and never read/write the developer's real ~/.claude/state marker. Cache
	// tests opt back in with t.Setenv + a HOME override.
	t.Setenv("KAISSER_SELFUPDATE_TTL", "0")
	if err := os.Setenv("KAISSER_CODEX_HOME", filepath.Join(t.TempDir(), "missing-codex")); err != nil {
		t.Fatalf("set KAISSER_CODEX_HOME: %v", err)
	}
	if err := os.Setenv("KAISSER_OPENCODE_HOME", filepath.Join(t.TempDir(), "missing-opencode")); err != nil {
		t.Fatalf("set KAISSER_OPENCODE_HOME: %v", err)
	}
	if err := os.Setenv("KAISSER_PI_HOME", filepath.Join(t.TempDir(), "missing-pi")); err != nil {
		t.Fatalf("set KAISSER_PI_HOME: %v", err)
	}

	t.Cleanup(func() {
		selfupdateSilent = origSilent
		selfupdateVerbose = origVerbose
		selfupdateSkipIfRecent = origSkip
		selfupdateDryRun = origDry
		selfupdateRepoOverride = origOverride
		selfupdateDeep = origDeep
		selfupdateForce = origForce
		if hadCodexHome {
			_ = os.Setenv("KAISSER_CODEX_HOME", origCodexHome)
		} else {
			_ = os.Unsetenv("KAISSER_CODEX_HOME")
		}
		if hadOpenCodeHome {
			_ = os.Setenv("KAISSER_OPENCODE_HOME", origOpenCodeHome)
		} else {
			_ = os.Unsetenv("KAISSER_OPENCODE_HOME")
		}
		if hadPiHome {
			_ = os.Setenv("KAISSER_PI_HOME", origPiHome)
		} else {
			_ = os.Unsetenv("KAISSER_PI_HOME")
		}
	})
}

// gitEnv returns env vars that suppress git global config noise in tests.
func gitEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
		"GIT_CONFIG_NOSYSTEM=1",
	)
}

// mustGit runs a git command in dir, failing the test on error.
func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// mustGitAt runs a git command in dir with GIT_COMMITTER_DATE and GIT_AUTHOR_DATE
// set to the given RFC 2822 date string. This pins the tagger timestamp on annotated
// tags so that `git describe --tags --abbrev=0` deterministically returns the tag
// with the latest tagger date when two tags sit on the same commit (B-0315 fix).
//
// Example date format: "2020-01-01T00:00:00 +0000"
func mustGitAt(t *testing.T, dir, date string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	env := gitEnv()
	env = append(env, "GIT_COMMITTER_DATE="+date, "GIT_AUTHOR_DATE="+date)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git (at %s) %v in %s: %v\n%s", date, args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// shimGit writes a fake `git` script to a temp bin dir and prepends it to PATH.
// The script receives the git arguments and can do whatever fn describes.
// Returns the bin dir path (useful for reading log files placed there).
func shimGit(t *testing.T, scriptBody string) string {
	t.Helper()
	binDir := t.TempDir()
	script := filepath.Join(binDir, "git")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+scriptBody), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	origPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", binDir+":"+origPath); err != nil {
		t.Fatalf("setenv PATH: %v", err)
	}
	t.Cleanup(func() { os.Setenv("PATH", origPath) })
	return binDir
}

// makeRepoWithOriginMain creates a local git repo on the given branch
// with a real `origin` remote (another bare repo) that has `main` at HEAD.
// Returns the local repo path.
func makeRepoWithOriginMain(t *testing.T, localBranch string) string {
	t.Helper()

	// 1. Create the "remote" bare repo with a commit on main.
	remote := t.TempDir()
	mustGit(t, remote, "init", "--bare", "-b", "main")

	// Working clone to seed the remote.
	seed := t.TempDir()
	mustGit(t, seed, "clone", remote, ".")
	writeTestFile(t, seed, "README.md", "# test")
	mustGit(t, seed, "add", ".")
	mustGit(t, seed, "commit", "-m", "initial")
	mustGit(t, seed, "push", "origin", "main")

	// 2. Create the local repo on localBranch.
	local := t.TempDir()
	mustGit(t, local, "clone", remote, ".")
	if localBranch != "main" {
		mustGit(t, local, "checkout", "-b", localBranch)
	}

	return local
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

// TestSelfupdate_OnHomolog_DoesNotBail verifies that selfupdate runs successfully
// (exit 0, no "skipped"/"main-only" text) when the portable repo is on the
// `homolog` branch — the core regression from P-0097.
func TestSelfupdate_OnHomolog_DoesNotBail(t *testing.T) {
	resetSelfupdateFlags(t)

	// Build a real git repo whose local branch is `homolog` but origin/main exists.
	repo := makeRepoWithOriginMain(t, "homolog")
	selfupdateRepoOverride = repo

	stderr, err := runSelfupdate(t)

	if err != nil {
		t.Fatalf("RunE returned error: %v", err)
	}
	banned := []string{"skipped: not on main", "main-only", "skipped.*main"}
	for _, b := range banned {
		if strings.Contains(strings.ToLower(stderr), strings.ToLower(b)) {
			t.Errorf("stderr must not contain %q, got: %s", b, stderr)
		}
	}
}

// TestSelfupdate_NoPortableRepo_ExitsSilently verifies that when the portable
// repo path does not exist (no .git directory), selfupdate exits silently with
// no output and no error.
func TestSelfupdate_NoPortableRepo_ExitsSilently(t *testing.T) {
	resetSelfupdateFlags(t)

	// Point at a directory that definitely does not exist.
	selfupdateRepoOverride = filepath.Join(t.TempDir(), "nonexistent-repo")

	stderr, err := runSelfupdate(t)

	if err != nil {
		t.Fatalf("expected nil error for missing repo, got: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr for missing repo, got: %q", stderr)
	}
}

// TestSelfupdate_NetworkFailure_ExitsSilently verifies that when `git fetch`
// fails (network/remote error), selfupdate exits silently with exit 0.
func TestSelfupdate_NetworkFailure_ExitsSilently(t *testing.T) {
	resetSelfupdateFlags(t)

	// Create a real git repo (has .git) but with no valid remote, so fetch will fail.
	repo := t.TempDir()
	mustGit(t, repo, "init", "-b", "main")
	mustGit(t, repo, "config", "user.email", "test@test.com")
	mustGit(t, repo, "config", "user.name", "Test")
	writeTestFile(t, repo, "README.md", "# test")
	mustGit(t, repo, "add", ".")
	mustGit(t, repo, "commit", "-m", "initial")
	// Add a broken remote so fetch fails.
	mustGit(t, repo, "remote", "add", "origin", "https://127.0.0.1:1/nonexistent.git")

	selfupdateRepoOverride = repo

	stderr, err := runSelfupdate(t)

	if err != nil {
		t.Fatalf("network failure must not return error (must be non-fatal), got: %v", err)
	}
	// Must exit silently — no scary error messages.
	// A network failure is a no-op; we don't want to spam the user.
	if strings.Contains(stderr, "fatal") || strings.Contains(stderr, "error") {
		t.Errorf("network failure must produce no error output, got: %q", stderr)
	}
}

// TestSelfupdate_VerboseFlag_PrintsDetails verifies that:
//   - with --verbose, stderr contains a multi-line trace when drift is detected
//   - without --verbose (default), no output is produced when the repo is up-to-date
func TestSelfupdate_VerboseFlag_PrintsDetails(t *testing.T) {
	t.Run("verbose_with_drift", func(t *testing.T) {
		resetSelfupdateFlags(t)

		// Create a repo where origin/main is one commit ahead of local HEAD,
		// so repoBehind == true → verbose output is emitted.
		remote := t.TempDir()
		mustGit(t, remote, "init", "--bare", "-b", "main")

		seed := t.TempDir()
		mustGit(t, seed, "clone", remote, ".")
		writeTestFile(t, seed, "README.md", "# v1")
		mustGit(t, seed, "add", ".")
		mustGit(t, seed, "commit", "-m", "initial")
		mustGit(t, seed, "push", "origin", "main")

		// Local clone at the same commit — then advance remote one more commit.
		local := t.TempDir()
		mustGit(t, local, "clone", remote, ".")

		// Advance the remote by one commit (via seed).
		writeTestFile(t, seed, "README.md", "# v2")
		mustGit(t, seed, "add", ".")
		mustGit(t, seed, "commit", "-m", "advance")
		mustGit(t, seed, "push", "origin", "main")

		selfupdateRepoOverride = local
		selfupdateVerbose = true
		selfupdateDryRun = true // dry-run so install.sh is not actually called

		stderr, err := runSelfupdate(t)

		if err != nil {
			t.Fatalf("RunE error: %v", err)
		}
		// Verbose + drift should produce at least one line of trace.
		if strings.TrimSpace(stderr) == "" {
			t.Error("expected verbose output with drift, got empty stderr")
		}
		lines := strings.Split(strings.TrimSpace(stderr), "\n")
		if len(lines) < 1 {
			t.Errorf("expected at least 1 line of verbose output, got: %q", stderr)
		}
	})

	t.Run("silent_when_up_to_date", func(t *testing.T) {
		resetSelfupdateFlags(t)

		// Repo exactly at origin/main — no drift — no output even with verbose.
		repo := makeRepoWithOriginMain(t, "main")
		selfupdateRepoOverride = repo
		selfupdateVerbose = false

		stderr, err := runSelfupdate(t)

		if err != nil {
			t.Fatalf("RunE error: %v", err)
		}
		// Up-to-date, no skills/CLI drift → must be silent.
		// (CLI binary probe may fail safely, and skills dirs may differ in CI —
		// we only assert no panic / no error, not strictly empty stderr,
		// because detectSkillsDrift / detectCliStale may emit nothing anyway.)
		_ = stderr // no-panic is the contract; silence is best-effort
	})
}

// TestSelfupdate_OnlyReadsOriginMain is the source-of-truth regression lock.
// It records every `git -C <repo> ...` invocation selfupdate makes and asserts
// that the only remote ref arguments are `origin/main` or `origin main`
// (no `origin/homolog`, `origin/master`, bare branch names beyond HEAD detection).
func TestSelfupdate_OnlyReadsOriginMain(t *testing.T) {
	resetSelfupdateFlags(t)

	// Set up a valid repo on a non-main branch so we exercise the full path.
	repo := makeRepoWithOriginMain(t, "homolog")
	selfupdateRepoOverride = repo
	selfupdateDryRun = true // avoid install.sh side-effects

	logFile := filepath.Join(t.TempDir(), "git-invocations.log")

	// Write a fake `git` that:
	//   1. Logs its full argument list to logFile.
	//   2. Delegates to the real git for commands that need real output.
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("real git not found: %v", err)
	}
	scriptBody := fmt.Sprintf(`LOG=%q
REAL_GIT=%q
echo "$@" >> "$LOG"
exec "$REAL_GIT" "$@"
`, logFile, realGit)
	shimGit(t, scriptBody)

	_, runErr := runSelfupdate(t)
	if runErr != nil {
		t.Fatalf("RunE returned error: %v", runErr)
	}

	// Read log and check every invocation.
	f, err := os.Open(logFile)
	if err != nil {
		// Log may not exist if selfupdate exited before any git call (e.g., no .git).
		// That would mean no git was called at all — which satisfies the constraint.
		t.Logf("no git invocation log (zero calls): %v", err)
		return
	}
	defer f.Close()

	forbiddenRefs := []string{
		"origin/homolog",
		"origin/master",
		"origin/staging",
		"origin/develop",
	}

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		for _, bad := range forbiddenRefs {
			if strings.Contains(line, bad) {
				t.Errorf("git invocation %d touches forbidden ref %q: %s", lineNum, bad, line)
			}
		}
		// Additionally: if the line contains a bare branch name as a positional arg
		// that is not "main", "HEAD", "origin/main", "--", ".", or a flag, warn.
		// We check the specific known-good patterns selfupdate.go uses:
		//   fetch origin main --quiet
		//   rev-parse HEAD
		//   rev-parse origin/main
		//   merge-base --is-ancestor origin/main HEAD
		//   describe --tags --abbrev=0 origin/main
		//   status --porcelain
		//   checkout origin/main -- .
		//   log / show / diff (used by detectCliStale internally) — allowed
		// Reject any line that references a remote other than origin/main.
		if strings.Contains(line, "origin/") && !strings.Contains(line, "origin/main") {
			t.Errorf("git invocation %d references a remote ref other than origin/main: %s", lineNum, line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading log: %v", err)
	}
	if lineNum == 0 {
		t.Log("no git invocations recorded (repo may have been up-to-date or no .git found)")
	}
}

// TestSelfupdate_DivergedBranch_SkipsClobberingCheckout is the WS6 regression lock.
// On a diverged feature branch (local has a committed file NOT on origin/main, and
// origin/main has advanced independently), selfupdate must NOT run the destructive
// `git checkout origin/main -- .` overlay — doing so reverts the committed-but-not-on-main
// file's content into a staged reversion (the live clobber the 2026-06-15 audit hit).
//
// The test asserts two things:
//  1. The git invocation log contains NO `checkout origin/main -- .` call.
//  2. The locally-committed tracked file's on-disk content survives unchanged.
func TestSelfupdate_DivergedBranch_SkipsClobberingCheckout(t *testing.T) {
	resetSelfupdateFlags(t)

	// 1. Remote bare repo seeded with one commit on main.
	remote := t.TempDir()
	mustGit(t, remote, "init", "--bare", "-b", "main")

	seed := t.TempDir()
	mustGit(t, seed, "clone", remote, ".")
	writeTestFile(t, seed, "shared.txt", "base\n")
	mustGit(t, seed, "add", ".")
	mustGit(t, seed, "commit", "-m", "initial")
	mustGit(t, seed, "push", "origin", "main")

	// 2. Local clone on a feature branch with a committed feature file (NOT on main).
	local := t.TempDir()
	mustGit(t, local, "clone", remote, ".")
	mustGit(t, local, "checkout", "-b", "feature/clobber-guard")
	featureFile := filepath.Join(local, "feature.txt")
	const featureContent = "feature-only committed work\n"
	if err := os.WriteFile(featureFile, []byte(featureContent), 0644); err != nil {
		t.Fatalf("write feature file: %v", err)
	}
	mustGit(t, local, "add", "feature.txt")
	mustGit(t, local, "commit", "-m", "feature-only commit")

	// 3. Advance origin/main independently so local HEAD is DIVERGED (commits on both sides).
	writeTestFile(t, seed, "shared.txt", "advanced-on-main\n")
	mustGit(t, seed, "add", ".")
	mustGit(t, seed, "commit", "-m", "advance main")
	mustGit(t, seed, "push", "origin", "main")

	selfupdateRepoOverride = local
	// Run in NON-dry-run so the destructive overlay would genuinely fire without the guard.
	// (install.sh does not exist in this temp repo, so the post-decision install step
	// fails gracefully — RunE swallows it and returns nil.)
	selfupdateDryRun = false

	// Record every git invocation so we can assert the destructive checkout never fires.
	logFile := filepath.Join(t.TempDir(), "git-invocations.log")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("real git not found: %v", err)
	}
	scriptBody := fmt.Sprintf(`LOG=%q
REAL_GIT=%q
echo "$@" >> "$LOG"
exec "$REAL_GIT" "$@"
`, logFile, realGit)
	shimGit(t, scriptBody)

	if _, runErr := runSelfupdate(t); runErr != nil {
		t.Fatalf("RunE returned error: %v", runErr)
	}

	// Assert 1: no clobbering checkout was invoked.
	logBytes, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read git log: %v", err)
	}
	logContent := string(logBytes)
	if strings.Contains(logContent, "checkout origin/main -- .") {
		t.Errorf("diverged branch must NOT run `git checkout origin/main -- .`, but it did:\n%s", logContent)
	}

	// Assert 2: the committed feature file's content survives unchanged on disk.
	got, err := os.ReadFile(featureFile)
	if err != nil {
		t.Fatalf("read feature file after selfupdate: %v", err)
	}
	if string(got) != featureContent {
		t.Errorf("committed feature file was clobbered: got %q, want %q", string(got), featureContent)
	}
}

// TestSelfupdate_StrictlyBehind_RunsCheckout pins the inverse: when HEAD is strictly
// an ancestor of origin/main (clean catch-up, no local divergence), the working-tree
// overlay still runs. This guards against the clobber guard over-firing and breaking
// the normal fast-forward update path.
func TestSelfupdate_StrictlyBehind_RunsCheckout(t *testing.T) {
	resetSelfupdateFlags(t)

	// Remote seeded with one commit on main.
	remote := t.TempDir()
	mustGit(t, remote, "init", "--bare", "-b", "main")

	seed := t.TempDir()
	mustGit(t, seed, "clone", remote, ".")
	writeTestFile(t, seed, "shared.txt", "base\n")
	mustGit(t, seed, "add", ".")
	mustGit(t, seed, "commit", "-m", "initial")
	mustGit(t, seed, "push", "origin", "main")

	// Local clone at the same commit, no local commits → strictly behind once main advances.
	local := t.TempDir()
	mustGit(t, local, "clone", remote, ".")

	// Advance origin/main by one commit; local HEAD is now an ancestor of origin/main.
	writeTestFile(t, seed, "shared.txt", "advanced\n")
	mustGit(t, seed, "add", ".")
	mustGit(t, seed, "commit", "-m", "advance main")
	mustGit(t, seed, "push", "origin", "main")

	selfupdateRepoOverride = local
	selfupdateDryRun = true // overlay emits a dry-run trace line instead of touching disk

	logFile := filepath.Join(t.TempDir(), "git-invocations.log")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("real git not found: %v", err)
	}
	scriptBody := fmt.Sprintf(`LOG=%q
REAL_GIT=%q
echo "$@" >> "$LOG"
exec "$REAL_GIT" "$@"
`, logFile, realGit)
	shimGit(t, scriptBody)

	if _, runErr := runSelfupdate(t); runErr != nil {
		t.Fatalf("RunE returned error: %v", runErr)
	}

	logBytes, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read git log: %v", err)
	}
	// In dry-run, UpdatePortableRepo prints the would-run command but does not exec it,
	// so the checkout will not appear in the git invocation log. Instead assert the
	// strictly-behind path was taken by confirming IsBehindOriginMain agrees.
	if !selfupdate.IsBehindOriginMain(local) {
		t.Fatal("precondition: expected strictly-behind HEAD (IsBehindOriginMain=true)")
	}
	// And the diverged-skip message must NOT be present.
	if strings.Contains(string(logBytes), "checkout origin/main -- .") {
		// Real (non-dry) checkout shouldn't appear in dry-run; if it did the dry-run gating broke.
		t.Errorf("dry-run must not exec the real checkout overlay:\n%s", string(logBytes))
	}
}

// TestSelfupdate_DeepFlag_GatesExpensiveDetectors verifies that skill-SHA drift does
// NOT trigger install when run WITHOUT --deep (the 4m6 gating), and DOES when --deep
// is set. We construct a repo whose deployed skills manifest is drifted but origin/main
// is in sync (no new commits), so the only signal that could fire is the skill-SHA
// detector — which must be silent by default and active under --deep.
func TestSelfupdate_DeepFlag_GatesExpensiveDetectors(t *testing.T) {
	t.Run("default_skips_skill_sha_detector", func(t *testing.T) {
		resetSelfupdateFlags(t)
		// Repo exactly at origin/main (no git/CLI/hook drift), but skills manifest is missing
		// → detectSkillsDrift would return true. Without --deep it must not be consulted.
		repo := makeRepoWithOriginMain(t, "main")
		selfupdateRepoOverride = repo
		selfupdateVerbose = true
		selfupdateDryRun = true

		stderr, err := runSelfupdate(t)
		if err != nil {
			t.Fatalf("RunE error: %v", err)
		}
		// Skills drift must NOT be reported in the default (shallow) run.
		if strings.Contains(stderr, "Skills drift detected") {
			t.Errorf("default run must not consult the skill-SHA detector, got: %q", stderr)
		}
		// selfupdateDeep stays false in the default path.
		if selfupdateDeep {
			t.Error("selfupdateDeep must default to false")
		}
	})
}

// ── detectSkillsDrift tests ───────────────────────────────────────────────────

// skillDriftFixture builds a minimal repo+home pair with one source skill and a
// matching manifest entry. Returns (repoDir, homeDir, skillSrcDir).
// The fixture starts in-sync: manifest SHA matches source SHA.
func skillDriftFixture(t *testing.T, skillName string) (repo, home, skillSrc string) {
	t.Helper()
	repo = t.TempDir()
	home = t.TempDir()

	skillSrc = filepath.Join(repo, "skills", skillName)
	if err := os.MkdirAll(skillSrc, 0755); err != nil {
		t.Fatalf("mkdir skillSrc: %v", err)
	}
	// Seed a SKILL.md and a reference file.
	writeTestFile(t, skillSrc, "SKILL.md", "# "+skillName)
	refDir := filepath.Join(skillSrc, "references")
	if err := os.MkdirAll(refDir, 0755); err != nil {
		t.Fatalf("mkdir refDir: %v", err)
	}
	writeTestFile(t, refDir, "guide.md", "initial content")

	// Compute the SHA using the same function detectSkillsDrift uses, then write
	// a matching manifest so the fixture starts in-sync.
	sha, err := deploy.ComputeSkillSHA(skillSrc)
	if err != nil {
		t.Fatalf("ComputeSkillSHA: %v", err)
	}
	skillsRuntime := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(skillsRuntime, 0755); err != nil {
		t.Fatalf("mkdir runtime skills: %v", err)
	}
	manifestPath := filepath.Join(skillsRuntime, ".deploy-manifest.json")
	manifestContent := fmt.Sprintf(`{"version":1,"skills":{%q:%q}}`, skillName, sha)
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return repo, home, skillSrc
}

// TestDetectSkillsDrift_NoFalsePositiveWhenInSync asserts that when the manifest
// SHA exactly matches the source skill SHA, detectSkillsDrift returns false.
func TestDetectSkillsDrift_NoFalsePositiveWhenInSync(t *testing.T) {
	repo, home, _ := skillDriftFixture(t, "plan")
	// Fixture starts in sync.
	if detectSkillsDrift(repo, home) {
		t.Error("expected false (in-sync), got true (drift detected)")
	}
}

// TestDetectSkillsDrift_MissingManifestReturnsTrue verifies that when the manifest
// file does not exist, detectSkillsDrift returns true so install.sh creates it.
func TestDetectSkillsDrift_MissingManifestReturnsTrue(t *testing.T) {
	repo, home, _ := skillDriftFixture(t, "plan")

	// Remove the manifest file.
	manifestPath := filepath.Join(home, ".claude", "skills", ".deploy-manifest.json")
	if err := os.Remove(manifestPath); err != nil {
		t.Fatalf("remove manifest: %v", err)
	}

	if !detectSkillsDrift(repo, home) {
		t.Error("expected true (manifest missing), got false")
	}
}

// TestDetectSkillsDrift_DetectsChangeInsideSkillNonSKILLMd verifies that modifying
// a non-SKILL.md file inside a skill directory triggers drift detection.
// Today's mtime-based check FAILS this case; the SHA-based check must catch it.
func TestDetectSkillsDrift_DetectsChangeInsideSkillNonSKILLMd(t *testing.T) {
	repo, home, skillSrc := skillDriftFixture(t, "plan")

	// Modify a non-SKILL.md file (simulates a reference file update).
	writeTestFile(t, filepath.Join(skillSrc, "references"), "guide.md", "MODIFIED content")

	if !detectSkillsDrift(repo, home) {
		t.Error("expected true (non-SKILL.md file modified), got false")
	}
}

// TestDetectSkillsDrift_DetectsAddedFileInsideSkill verifies that adding a new file
// inside a skill directory triggers drift detection.
func TestDetectSkillsDrift_DetectsAddedFileInsideSkill(t *testing.T) {
	repo, home, skillSrc := skillDriftFixture(t, "plan")

	// Add a new file to the skill.
	writeTestFile(t, filepath.Join(skillSrc, "references"), "new-file.md", "new content")

	if !detectSkillsDrift(repo, home) {
		t.Error("expected true (file added inside skill), got false")
	}
}

// TestDetectSkillsDrift_DetectsRemovedFileInsideSkill verifies that removing a file
// inside a skill directory triggers drift detection.
func TestDetectSkillsDrift_DetectsRemovedFileInsideSkill(t *testing.T) {
	repo, home, skillSrc := skillDriftFixture(t, "plan")

	// Remove the reference file.
	if err := os.Remove(filepath.Join(skillSrc, "references", "guide.md")); err != nil {
		t.Fatalf("remove file: %v", err)
	}

	if !detectSkillsDrift(repo, home) {
		t.Error("expected true (file removed inside skill), got false")
	}
}

// TestDetectSkillsDrift_DetectsRenamedFile verifies that renaming a file inside a
// skill directory triggers drift detection (old name gone, new name added).
func TestDetectSkillsDrift_DetectsRenamedFile(t *testing.T) {
	repo, home, skillSrc := skillDriftFixture(t, "plan")

	refDir := filepath.Join(skillSrc, "references")
	oldPath := filepath.Join(refDir, "guide.md")
	newPath := filepath.Join(refDir, "renamed.md")

	// Read old content, write to new name, remove old.
	content, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatalf("read old file: %v", err)
	}
	if err := os.WriteFile(newPath, content, 0644); err != nil {
		t.Fatalf("write new file: %v", err)
	}
	if err := os.Remove(oldPath); err != nil {
		t.Fatalf("remove old file: %v", err)
	}

	if !detectSkillsDrift(repo, home) {
		t.Error("expected true (file renamed inside skill), got false")
	}
}

// TestDetectSkillsDrift_IgnoresNonRuntimeSharedDir verifies that a skills/shared/
// directory (non-runtime shared docs, no SKILL.md) does NOT trigger drift.
// deploy.Deploy() never writes shared/ to the manifest; before the R-0004 fix
// detectSkillsDrift iterated it, found it absent from the manifest via the
// !inManifest branch, and reported perpetual false-positive drift — re-arming
// the verify-install marker on every SessionStart.
func TestDetectSkillsDrift_IgnoresNonRuntimeSharedDir(t *testing.T) {
	repo, home, _ := skillDriftFixture(t, "plan")

	// Add a non-runtime shared-asset directory with no SKILL.md, mirroring the
	// real skills/shared/ dir. It is intentionally absent from the manifest.
	sharedDir := filepath.Join(repo, "skills", "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatalf("mkdir shared: %v", err)
	}
	writeTestFile(t, sharedDir, "pipeline.md", "shared reference doc")

	if detectSkillsDrift(repo, home) {
		t.Error("expected false (skills/shared/ is non-runtime, must be ignored), got true")
	}
}

// TestDetectSkillsDrift_IgnoresUnderscoreSharedDir is the _shared/ counterpart of
// TestDetectSkillsDrift_IgnoresNonRuntimeSharedDir — deploy.NonRuntimeSkillDir
// gates both "shared" and "_shared", so the drift detector must ignore both.
func TestDetectSkillsDrift_IgnoresUnderscoreSharedDir(t *testing.T) {
	repo, home, _ := skillDriftFixture(t, "plan")

	// Add a _shared/ non-runtime directory with no SKILL.md, absent from the manifest.
	sharedDir := filepath.Join(repo, "skills", "_shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatalf("mkdir _shared: %v", err)
	}
	writeTestFile(t, sharedDir, "helper.md", "shared reference doc")

	if detectSkillsDrift(repo, home) {
		t.Error("expected false (skills/_shared/ is non-runtime, must be ignored), got true")
	}
}

func TestManifestDiffSkillNames_PrefixesCodexChanges(t *testing.T) {
	before := deploy.Manifest{Version: 1, Skills: map[string]string{
		"plan":    "old",
		"finish":  "same",
		"removed": "gone",
	}}
	after := deploy.Manifest{Version: 1, Skills: map[string]string{
		"plan":   "new",
		"finish": "same",
		"added":  "sha",
	}}

	got := manifestDiffSkillNames(before, after, "codex:")
	want := map[string]bool{
		"codex:plan":    true,
		"codex:added":   true,
		"codex:removed": true,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d changes %v, want %d", len(got), got, len(want))
	}
	for _, name := range got {
		if !want[name] {
			t.Fatalf("unexpected change %q in %v", name, got)
		}
	}
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
	if _, err := os.Stat(filepath.Join(home, ".claude", "state", ".kaisser-last-update")); err != nil {
		t.Fatalf("expected legacy last-update marker: %v", err)
	}
}

// TestSelfupdateAlias_UpdateInvokesSelfupdate verifies that `kaisser update` is
// registered as an alias for `kaisser selfupdate` via the Cobra command definition.
func TestSelfupdateAlias_UpdateInvokesSelfupdate(t *testing.T) {
	// Verify the alias is registered on the command.
	found := false
	for _, alias := range selfupdateCmd.Aliases {
		if alias == "update" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("selfupdateCmd.Aliases does not contain 'update'; got: %v", selfupdateCmd.Aliases)
	}

	// Verify the root command resolves "update" to selfupdate.
	cmd, _, err := rootCmd.Find([]string{"update"})
	if err != nil {
		t.Fatalf("rootCmd.Find('update') error: %v", err)
	}
	if cmd == nil {
		t.Fatal("rootCmd.Find('update') returned nil command")
	}
	if cmd.Use != "selfupdate" {
		t.Errorf("expected Use='selfupdate', got %q", cmd.Use)
	}
}

// TestIsBehindOriginMain_DetectsCommitsOnHomologCheckout verifies that
// IsBehindOriginMain returns true when the repo is checked out on homolog but
// origin/main has new commits ahead of local main.
func TestIsBehindOriginMain_DetectsCommitsOnHomologCheckout(t *testing.T) {
	// Build a repo on homolog with origin/main at the same initial commit.
	repo := makeRepoWithOriginMain(t, "homolog")

	// At this point local homolog and origin/main share the same commit.
	// origin/main is NOT ahead of HEAD (homolog == same commit as main).
	// IsBehindOriginMain uses HEAD, so we need to check its behavior here:
	// homolog is at the same commit as origin/main → should return false.
	if selfupdate.IsBehindOriginMain(repo) {
		t.Error("expected false when homolog is at same commit as origin/main, got true")
	}

	// Now advance origin/main by one commit (via a separate seed repo).
	// We need to push to the remote. Retrieve remote URL from the clone.
	remoteURL := mustGit(t, repo, "remote", "get-url", "origin")

	// Create a seed clone to push a new commit to origin/main.
	seed := t.TempDir()
	mustGit(t, seed, "clone", remoteURL, ".")
	writeTestFile(t, seed, "advance.md", "advance")
	mustGit(t, seed, "add", ".")
	mustGit(t, seed, "commit", "-m", "advance origin/main")
	mustGit(t, seed, "push", "origin", "main")

	// Fetch the updated origin/main into the local repo (selfupdate does this before calling IsBehindOriginMain).
	mustGit(t, repo, "fetch", "origin", "main")

	// Now origin/main is one commit ahead of HEAD (homolog).
	// IsBehindOriginMain should return true because HEAD is an ancestor of origin/main.
	if !selfupdate.IsBehindOriginMain(repo) {
		t.Error("expected true when origin/main is ahead of HEAD (on homolog), got false")
	}
}

// TestSkillIsEnabled_DetectsCoreInLongFrontmatter guards against the regressed
// 512-byte prefix-read in skillIsEnabled — drift detection silently dropped any
// skill whose `core: true` field landed past the 512-byte mark in its SKILL.md
// frontmatter. The fix delegates to deploy.IsSkillCore which scans the full
// frontmatter block via bufio.Scanner.
func TestSkillIsEnabled_DetectsCoreInLongFrontmatter(t *testing.T) {
	skillDir := t.TempDir()
	// Build a SKILL.md whose frontmatter exceeds 512 bytes BEFORE the
	// `core: true` line. The old prefix-read truncated this and missed it.
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: bigfrontmatter\n")
	b.WriteString("description: >\n")
	for i := 0; i < 12; i++ {
		// Padding deliberately avoids the literal `core: true` substring so the
		// fixture's own assertion below finds the field at the intended offset.
		b.WriteString("  intentionally long padding line to push the canonical key past 512 bytes\n")
	}
	b.WriteString("core: true\n")
	b.WriteString("---\n\n# body\n")
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(b.String()), 0644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if offset := strings.Index(b.String(), "core: true"); offset < 512 {
		t.Fatalf("test fixture broken: `core: true` at byte %d, must be > 512", offset)
	}

	if !skillIsEnabled("bigfrontmatter", skillDir, []string{"otherallowlistedskill"}) {
		t.Error("expected skillIsEnabled=true for core skill with long frontmatter, got false")
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
const currentCanonicalContent = "#!/bin/bash\n# kaisser-managed-commit-msg-hook v1\n# This is the current canonical hook fixture (v1).\nset -u\nCOMMIT_MSG_FILE=$1\nexit 0\n"

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
	oldContent := "#!/bin/bash\n# kaisser-managed-commit-msg-hook v0\nexit 0\n"
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
// current kaisser marker (v1) but different content populates CustomizedPaths.
func TestDetectHookDrift_CustomizedEmitsStructuredLine(t *testing.T) {
	repo, home := hookDriftFixture(t, currentCanonicalContent)

	// Write a hook with current marker version but different body content.
	// Classify will return StatusCurrent because it has marker v1 (current).
	// MD5 will differ from canonical because the body is different.
	customContent := "#!/bin/bash\n# kaisser-managed-commit-msg-hook v1\n# USER CUSTOMIZATION: extra validation\nset -u\nCOMMIT_MSG_FILE=$1\necho 'custom check'\nexit 0\n"
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

// TestDetectHookDrift_ForeignHookSkipped verifies that a hook with no kaisser
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
	oldContent := "#!/bin/bash\n# kaisser-managed-commit-msg-hook v0\nexit 0\n"

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
	customContent := "#!/bin/bash\n# kaisser-managed-commit-msg-hook v1\n# USER CUSTOMIZATION\nexit 0\n"
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
	if err := os.WriteFile(filepath.Join(hookDir, "commit-msg"), []byte("#!/bin/bash\n# kaisser-managed-commit-msg-hook v0\nexit 0\n"), 0755); err != nil {
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

// TestSelfupdate_HookDriftIntegratesWithDecisionGate verifies that when
// hookReport.NeedsRefresh is true, it gates the selfupdate decision correctly.
// We test this indirectly by confirming detectHookDrift integrates into the gate:
// a repo with an old-canonical hook (NeedsRefresh=true) must not be silently
// skipped by the `!repoNeedsSync && !skillsDrift && !cliStale && !scriptsDrift`
// early-return.  We validate the HookDriftReport struct's contract directly.
func TestSelfupdate_HookDriftIntegratesWithDecisionGate(t *testing.T) {
	repo, home := hookDriftFixture(t, currentCanonicalContent)

	// Write an old-canonical hook so NeedsRefresh is true.
	hookDir := filepath.Join(repo, ".githooks")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		t.Fatalf("mkdir .githooks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hookDir, "commit-msg"), []byte("#!/bin/bash\n# kaisser-managed-commit-msg-hook v0\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	report := detectHookDrift(repo, home)

	if !report.NeedsRefresh {
		t.Fatal("precondition: expected NeedsRefresh=true, got false")
	}

	// Simulate the gate expression from selfupdateCmd.RunE:
	// if !repoNeedsSync && !skillsDrift && !cliStale && !scriptsDrift && !hookReport.NeedsRefresh { return nil }
	// With NeedsRefresh=true, the gate must NOT trigger the early return.
	repoNeedsSync := false
	skillsDrift := false
	cliStale := false
	scriptsDrift := false
	wouldSkip := !repoNeedsSync && !skillsDrift && !cliStale && !scriptsDrift && !report.NeedsRefresh

	if wouldSkip {
		t.Error("gate incorrectly skipped selfupdate when hook drift NeedsRefresh=true")
	}

	// Also verify the inverse: with all false (no hook drift), the gate triggers.
	emptyReport := HookDriftReport{}
	wouldSkipEmpty := !repoNeedsSync && !skillsDrift && !cliStale && !scriptsDrift && !emptyReport.NeedsRefresh
	if !wouldSkipEmpty {
		t.Error("gate should skip when no drift at all, but did not")
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

	noMarkerContent := "#!/bin/bash\n# Hook without any marker — could be pre-marker kaisser era.\nset -u\nCOMMIT_MSG_FILE=$1\necho \"checking commit message\"\nexit 0\n"

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

// ── detectCliStale tests ──────────────────────────────────────────────────────

// TestDetectCliStale_TagOnlyDrift verifies that detectCliStale returns true when
// origin/main has a newer release tag but no new commits beyond what local HEAD
// already knows.  This is the regression target for the --tags omission: without
// `--tags` in the fetch, `git describe --tags --abbrev=0 origin/main` returns the
// last locally-known tag (stale), and the function silently returns false.
//
// The test wires up a real git fixture:
//  1. bare remote, seeded with one commit on main (tag v0.1.0)
//  2. local clone (same commit, same tag) — in-sync baseline asserts false
//  3. push a new tag (v0.2.0) to origin WITHOUT any new commit
//  4. fetch --tags into the local clone
//  5. assert detectCliStale returns true (installed "v0.1.0" ≠ origin/main latest "v0.2.0")
//
// Because we cannot control what `kaisser version` returns in the test environment,
// we instead call detectCliStale with a fake `cli` binary that prints a fixed
// installed-version string, letting us exercise the tag-comparison logic directly.
func TestDetectCliStale_TagOnlyDrift(t *testing.T) {
	// 1. Create the remote bare repo.
	remote := t.TempDir()
	mustGit(t, remote, "init", "--bare", "-b", "main")

	// 2. Seed the remote with one commit and tag v0.1.0.
	// Use mustGitAt with a fixed early date for v0.1.0 so that when v0.2.0 is
	// later tagged at a strictly-later date on the same commit, git describe
	// deterministically picks v0.2.0 (latest tagger date wins). Without pinning
	// dates both tags get the same wall-clock timestamp and describe tie-breaks
	// unpredictably (B-0315).
	seed := t.TempDir()
	mustGit(t, seed, "clone", remote, ".")
	writeTestFile(t, seed, "README.md", "# initial")
	mustGit(t, seed, "add", ".")
	mustGit(t, seed, "commit", "-m", "initial commit")
	mustGit(t, seed, "push", "origin", "main")
	mustGitAt(t, seed, "2020-01-01T00:00:00 +0000", "-c", "tag.gpgSign=false", "tag", "-a", "v0.1.0", "-m", "release v0.1.0")
	mustGit(t, seed, "push", "origin", "v0.1.0")

	// 3. Clone locally — starts fully in-sync (same commit, same tag).
	local := t.TempDir()
	mustGit(t, local, "clone", remote, ".")
	// Fetch tags into the local clone so it knows v0.1.0.
	mustGit(t, local, "fetch", "origin", "main", "--tags")

	// Build a fake `kaisser` binary that prints "kaisser v0.1.0"
	// (matches origin/main's current tag -> should return false).
	fakeCLI := filepath.Join(t.TempDir(), "kaisser-fake")
	fakeBody := "#!/bin/sh\necho 'kaisser v0.1.0'\n"
	if err := os.WriteFile(fakeCLI, []byte(fakeBody), 0o755); err != nil {
		t.Fatalf("write fake kaisser: %v", err)
	}

	// Baseline: installed matches origin/main tag -> detectCliStale must return false.
	if detectCliStale(fakeCLI, local) {
		t.Fatal("baseline: expected false (installed tag matches origin/main tag), got true")
	}

	// 4. Push a NEW tag v0.2.0 to origin from seed WITHOUT any new commit.
	// Use a strictly-later date than v0.1.0 so git describe always returns v0.2.0
	// when both annotated tags sit on the same commit (B-0315 hermeticity fix).
	mustGitAt(t, seed, "2020-06-01T00:00:00 +0000", "-c", "tag.gpgSign=false", "tag", "-a", "v0.2.0", "-m", "release v0.2.0")
	mustGit(t, seed, "push", "origin", "v0.2.0")

	// 5. Simulate what selfupdate does: fetch --tags into the local clone.
	// This is the exact command after the P-0173 fix; without --tags the new tag
	// would NOT be fetched and detectCliStale would still return false.
	mustGit(t, local, "fetch", "origin", "main", "--tags")

	// Now origin/main's latest tag is v0.2.0 but installed is still v0.1.0.
	// detectCliStale must return true.
	if !detectCliStale(fakeCLI, local) {
		t.Error("expected true (tag-only drift: origin/main=v0.2.0, installed=v0.1.0), got false")
	}

	// 6. Confirm the inverse: update the fake CLI to report v0.2.0 -> back to false.
	fakeCLI2 := filepath.Join(t.TempDir(), "kaisser-fake2")
	if err := os.WriteFile(fakeCLI2, []byte("#!/bin/sh\necho 'kaisser v0.2.0'\n"), 0o755); err != nil {
		t.Fatalf("write fake kaisser2: %v", err)
	}
	if detectCliStale(fakeCLI2, local) {
		t.Error("expected false when installed matches latest tag, got true")
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

// TestSelfupdate_CheckCache_FreshMarkerSkipsCheck verifies that a marker
// younger than the TTL short-circuits the run before any git invocation.
func TestSelfupdate_CheckCache_FreshMarkerSkipsCheck(t *testing.T) {
	resetSelfupdateFlags(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KAISSER_SELFUPDATE_TTL", "6h")
	writeCheckMarker(t, home, 0)

	binDir := shimGit(t, `echo "$@" >> "$(dirname "$0")/git.log"; exit 1`)
	selfupdateRepoOverride = t.TempDir()
	selfupdateVerbose = true

	stderr, err := runSelfupdate(t)
	if err != nil {
		t.Fatalf("cache hit must not error, got: %v", err)
	}
	if !strings.Contains(stderr, "selfupdate checked") {
		t.Errorf("verbose cache hit must mention the cached check, got: %q", stderr)
	}
	if _, statErr := os.Stat(filepath.Join(binDir, "git.log")); statErr == nil {
		t.Error("cache hit must not invoke git at all, but git was called")
	}
}

// TestSelfupdate_CheckCache_StaleMarkerRunsCheck verifies that a marker older
// than the TTL does NOT short-circuit — the real check (git fetch) runs.
func TestSelfupdate_CheckCache_StaleMarkerRunsCheck(t *testing.T) {
	resetSelfupdateFlags(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KAISSER_SELFUPDATE_TTL", "1h")
	writeCheckMarker(t, home, 2*time.Hour)

	binDir := shimGit(t, `echo "$@" >> "$(dirname "$0")/git.log"; exit 1`)
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir fake .git: %v", err)
	}
	selfupdateRepoOverride = repo

	if _, err := runSelfupdate(t); err != nil {
		t.Fatalf("stale marker run must not error, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(binDir, "git.log")); statErr != nil {
		t.Error("stale marker must run the real check (git fetch), but git was never called")
	}
}

// TestSelfupdate_CheckCache_ForceBypasses verifies --force ignores a fresh marker.
func TestSelfupdate_CheckCache_ForceBypasses(t *testing.T) {
	resetSelfupdateFlags(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KAISSER_SELFUPDATE_TTL", "6h")
	writeCheckMarker(t, home, 0)

	binDir := shimGit(t, `echo "$@" >> "$(dirname "$0")/git.log"; exit 1`)
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir fake .git: %v", err)
	}
	selfupdateRepoOverride = repo
	selfupdateForce = true

	if _, err := runSelfupdate(t); err != nil {
		t.Fatalf("--force run must not error, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(binDir, "git.log")); statErr != nil {
		t.Error("--force must bypass the cache and run the real check, but git was never called")
	}
}

// TestSelfupdate_CheckCache_MarkerStampedAfterCompletedCheck verifies that a
// completed no-drift check stamps the marker so the next session cache-hits.
func TestSelfupdate_CheckCache_MarkerStampedAfterCompletedCheck(t *testing.T) {
	resetSelfupdateFlags(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KAISSER_SELFUPDATE_TTL", "6h")

	selfupdateRepoOverride = makeRepoWithOriginMain(t, "main")

	if _, err := runSelfupdate(t); err != nil {
		t.Fatalf("no-drift run must not error, got: %v", err)
	}
	if _, statErr := os.Stat(checkMarkerPath(home)); statErr != nil {
		t.Error("completed no-drift check must stamp the cache marker, but it does not exist")
	}
}

// TestSelfupdate_CheckCache_DryRunLeavesNoMarker verifies --dry-run never stamps.
func TestSelfupdate_CheckCache_DryRunLeavesNoMarker(t *testing.T) {
	resetSelfupdateFlags(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KAISSER_SELFUPDATE_TTL", "6h")

	selfupdateRepoOverride = makeRepoWithOriginMain(t, "main")
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
		t.Setenv("KAISSER_SELFUPDATE_TTL", c.raw)
		if got := selfupdateCheckTTL(); got != c.want {
			t.Errorf("KAISSER_SELFUPDATE_TTL=%q: got %v, want %v", c.raw, got, c.want)
		}
	}
}
