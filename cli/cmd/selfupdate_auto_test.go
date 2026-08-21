package cmd

// selfupdate_auto_test.go — the SessionStart AUTO-UPDATE lane (P-0018 Phase 6).
//
// The 24h check that used to print a line now replaces the binary on machines
// install.sh owns. These tests read the FILESYSTEM (was the binary swapped? is
// there a .prev to roll back to?) and COUNT calls (did anything download after
// a refusal?) — an exit code proves nothing here, since this lane returns
// nothing and is not allowed to fail a session.
//
// The trust chain is exercised for real in internal/selfupdate/binary_test.go;
// the installer is faked here so the DECISIONS around it can be pinned.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bravros/bravros/cli/internal/selfupdate"
)

type fakeAger struct {
	calls int
	age   time.Duration
	err   error
}

func (f *fakeAger) ReleaseAge(context.Context, string) (time.Duration, error) {
	f.calls++
	return f.age, f.err
}

// autoUpdateTestEnv isolates every path the lane touches and seeds a stand-in
// executable under <root>/bin — the location that makes detectInstallMethod
// report "installer". Tests that need another owner re-point
// updateExePathOverride with seedBinaryAt (update_test.go).
func autoUpdateTestEnv(t *testing.T) (root, exePath string) {
	t.Helper()
	resetSelfupdateFlags(t) // also parks BRAVROS_SELFUPDATE_TTL / NO_UPDATE_CHECK

	home := t.TempDir()
	root = filepath.Join(home, ".claude")
	t.Setenv("HOME", home)
	t.Setenv("BRAVROS_CONFIG_DIR", root)
	t.Setenv(setupInstallMethodEnv, "")
	t.Setenv(updateInstallMethodOverrideEnv, "")
	t.Setenv(selfupdate.NoUpdateCheckEnv, "")
	// The canary is exercised explicitly by TestAutoUpdateDefersAYoungRelease;
	// everywhere else it is disabled so a swap is deterministic.
	t.Setenv(selfupdate.MinReleaseAgeEnv, "0")

	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	exePath = filepath.Join(binDir, "bravros")
	if err := os.WriteFile(exePath, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("seed executable: %v", err)
	}

	updateExePathOverride = exePath
	updateRefreshHook = func(string) error { return nil }
	t.Cleanup(func() {
		updateExePathOverride = ""
		updateRefreshHook = nil
		updateInstallerOverride = nil
		updateResolverOverride = nil
		selfupdateAgerOverride = nil
		selfupdateNoticeResolverOverride = nil
	})
	return root, exePath
}

func runAutoLane(t *testing.T) string {
	t.Helper()
	return captureStderr(t, func() { selfupdatePassiveNotice() })
}

// TestAutoUpdateSwapsAnInstallerOwnedBinary is the headline: setup.json records
// the installer, a newer release exists, and the SessionStart lane replaces the
// binary by itself — keeping one rollback generation and saying so in exactly
// one line.
func TestAutoUpdateSwapsAnInstallerOwnedBinary(t *testing.T) {
	root, exePath := autoUpdateTestEnv(t)
	writeStateWithInstallMethod(t, root, "installer")

	installer := &fakeInstaller{}
	updateInstallerOverride = installer
	selfupdateNoticeResolverOverride = &fakeTagResolver{tag: "v99.0.0"}
	ager := &fakeAger{age: 24 * time.Hour}
	selfupdateAgerOverride = ager
	refreshedWith := ""
	updateRefreshHook = func(p string) error { refreshedWith = p; return nil }

	out := runAutoLane(t)

	if installer.calls != 1 {
		t.Fatalf("expected exactly one install, got %d", installer.calls)
	}
	if got, _ := os.ReadFile(exePath); string(got) != "new binary v99.0.0" {
		t.Errorf("binary was not replaced, got %q", got)
	}
	if got, err := os.ReadFile(selfupdate.PrevBinaryPath(exePath)); err != nil || string(got) != "old binary" {
		t.Errorf("the outgoing binary must be preserved as bravros.prev (err=%v, content=%q)", err, got)
	}
	want := selfupdate.SwapLine(Version, "v99.0.0")
	if !strings.Contains(out, want) {
		t.Errorf("expected the one loud line %q, got:\n%s", want, out)
	}
	if strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Errorf("a swap prints exactly ONE line, got:\n%s", out)
	}
	// The notice must NOT also print — the swap already answered it.
	if strings.Contains(out, "run `bravros update`") {
		t.Errorf("a completed swap must not also nag, got:\n%s", out)
	}
	// The refresh has to run the NEW binary: the payload is embedded, so this
	// process could only ever redeploy the old skills.
	if refreshedWith != exePath {
		t.Errorf("post-swap refresh ran against %q, want %q", refreshedWith, exePath)
	}
}

// TestAutoUpdateRefusesABrewOwnedBinaryBeforeAnySwap — the P-0018 trap, stated
// as a test: the package-manager check happens BEFORE anything is downloaded,
// probed, or replaced.
func TestAutoUpdateRefusesABrewOwnedBinaryBeforeAnySwap(t *testing.T) {
	root, _ := autoUpdateTestEnv(t)
	writeStateWithInstallMethod(t, root, "brew")
	exePath := seedBinaryAt(t, filepath.Join(t.TempDir(), "homebrew", "Cellar", "bravros", "2.11.0", "bin", "bravros"), "brew binary")

	installer := &fakeInstaller{}
	updateInstallerOverride = installer
	selfupdateNoticeResolverOverride = &fakeTagResolver{tag: "v99.0.0"}
	ager := &fakeAger{age: 24 * time.Hour}
	selfupdateAgerOverride = ager

	out := runAutoLane(t)

	if installer.calls != 0 {
		t.Errorf("a brew-owned binary must never be downloaded over, got %d install call(s)", installer.calls)
	}
	if ager.calls != 0 {
		t.Errorf("the refusal must come before the age probe, got %d probe(s)", ager.calls)
	}
	if got, _ := os.ReadFile(exePath); string(got) != "brew binary" {
		t.Errorf("the brew binary must be untouched, got %q", got)
	}
	if _, err := os.Stat(selfupdate.PrevBinaryPath(exePath)); err == nil {
		t.Error("no .prev may be written when nothing is replaced")
	}
	// Notify-only is the pre-D2 behavior, and it is preserved exactly.
	if !strings.Contains(out, "v99.0.0 is available") {
		t.Errorf("a refused machine still gets the notice, got:\n%s", out)
	}
	if strings.Contains(out, "(auto)") {
		t.Errorf("nothing was swapped, so no swap line may print:\n%s", out)
	}
}

// TestAutoUpdateHonoursNoUpdateCheckEnv — one env var still switches the whole
// lane off, network call included.
func TestAutoUpdateHonoursNoUpdateCheckEnv(t *testing.T) {
	root, exePath := autoUpdateTestEnv(t)
	writeStateWithInstallMethod(t, root, "installer")
	t.Setenv(selfupdate.NoUpdateCheckEnv, "1")

	installer := &fakeInstaller{}
	updateInstallerOverride = installer
	resolver := &fakeTagResolver{tag: "v99.0.0"}
	selfupdateNoticeResolverOverride = resolver
	ager := &fakeAger{age: 24 * time.Hour}
	selfupdateAgerOverride = ager

	if out := runAutoLane(t); out != "" {
		t.Errorf("the opt-out must print nothing, got:\n%s", out)
	}
	if resolver.calls != 0 {
		t.Errorf("the opt-out must make no remote check, got %d", resolver.calls)
	}
	if installer.calls != 0 || ager.calls != 0 {
		t.Errorf("the opt-out must do no work at all (installs=%d probes=%d)", installer.calls, ager.calls)
	}
	if got, _ := os.ReadFile(exePath); string(got) != "old binary" {
		t.Errorf("binary must be untouched, got %q", got)
	}
}

// TestAutoUpdateHonoursAutoUpdateFalseInSetupJSON — an explicit opt-out in
// setup.json disables the automatic lane while leaving the notice in place.
func TestAutoUpdateHonoursAutoUpdateFalseInSetupJSON(t *testing.T) {
	root, exePath := autoUpdateTestEnv(t)
	writeAutoUpdateState(t, root, "installer", false)

	installer := &fakeInstaller{}
	updateInstallerOverride = installer
	selfupdateNoticeResolverOverride = &fakeTagResolver{tag: "v99.0.0"}
	ager := &fakeAger{age: 24 * time.Hour}
	selfupdateAgerOverride = ager

	out := runAutoLane(t)

	if installer.calls != 0 {
		t.Errorf("auto_update:false must not install anything, got %d", installer.calls)
	}
	if ager.calls != 0 {
		t.Errorf("auto_update:false must not even probe the release, got %d", ager.calls)
	}
	if got, _ := os.ReadFile(exePath); string(got) != "old binary" {
		t.Errorf("binary must be untouched, got %q", got)
	}
	if !strings.Contains(out, "v99.0.0 is available") {
		t.Errorf("the notice must survive the opt-out, got:\n%s", out)
	}
}

// TestAutoUpdateAutoUpdateTrueStillSwaps — the explicit opt-IN behaves exactly
// like the absent field, so writing the default down changes nothing.
func TestAutoUpdateAutoUpdateTrueStillSwaps(t *testing.T) {
	root, exePath := autoUpdateTestEnv(t)
	writeAutoUpdateState(t, root, "installer", true)

	installer := &fakeInstaller{}
	updateInstallerOverride = installer
	selfupdateNoticeResolverOverride = &fakeTagResolver{tag: "v99.0.0"}
	selfupdateAgerOverride = &fakeAger{age: 24 * time.Hour}

	if out := runAutoLane(t); !strings.Contains(out, "(auto)") {
		t.Errorf("auto_update:true must swap, got:\n%s", out)
	}
	if installer.calls != 1 {
		t.Errorf("expected one install, got %d", installer.calls)
	}
	if got, _ := os.ReadFile(exePath); string(got) != "new binary v99.0.0" {
		t.Errorf("binary was not replaced, got %q", got)
	}
}

// TestAutoUpdateDefersAYoungRelease — the canary. A release published minutes
// ago is not installed unattended; it is picked up at the next 24h check, by
// which time a yanked one is gone.
func TestAutoUpdateDefersAYoungRelease(t *testing.T) {
	root, exePath := autoUpdateTestEnv(t)
	writeStateWithInstallMethod(t, root, "installer")
	t.Setenv(selfupdate.MinReleaseAgeEnv, "6h")

	installer := &fakeInstaller{}
	updateInstallerOverride = installer
	selfupdateNoticeResolverOverride = &fakeTagResolver{tag: "v99.0.0"}
	ager := &fakeAger{age: 30 * time.Minute}
	selfupdateAgerOverride = ager

	out := runAutoLane(t)

	if ager.calls != 1 {
		t.Errorf("the canary must probe the release age exactly once, got %d", ager.calls)
	}
	if installer.calls != 0 {
		t.Errorf("a 30m-old release must not be installed, got %d install call(s)", installer.calls)
	}
	if got, _ := os.ReadFile(exePath); string(got) != "old binary" {
		t.Errorf("binary must be untouched while the release is young, got %q", got)
	}
	if _, err := os.Stat(selfupdate.PrevBinaryPath(exePath)); err == nil {
		t.Error("a deferred release must not leave a .prev behind")
	}
	if !strings.Contains(out, "v99.0.0 is available") {
		t.Errorf("a deferred swap still notifies, got:\n%s", out)
	}
}

// TestAutoUpdateFailsOpenToNotifyWhenTheAgeIsUnknown — losing the canary must
// cost an automatic install, never grant one.
func TestAutoUpdateFailsOpenToNotifyWhenTheAgeIsUnknown(t *testing.T) {
	root, exePath := autoUpdateTestEnv(t)
	writeStateWithInstallMethod(t, root, "installer")
	t.Setenv(selfupdate.MinReleaseAgeEnv, "6h")

	installer := &fakeInstaller{}
	updateInstallerOverride = installer
	selfupdateNoticeResolverOverride = &fakeTagResolver{tag: "v99.0.0"}
	selfupdateAgerOverride = &fakeAger{err: os.ErrDeadlineExceeded}

	out := runAutoLane(t)

	if installer.calls != 0 {
		t.Errorf("an unknown release age must not be swapped through, got %d", installer.calls)
	}
	if got, _ := os.ReadFile(exePath); string(got) != "old binary" {
		t.Errorf("binary must be untouched, got %q", got)
	}
	if !strings.Contains(out, "v99.0.0 is available") {
		t.Errorf("failing open means the notice, got:\n%s", out)
	}
}

// TestAutoUpdateKeeps24hCadence — a cache hit prints the cached notice and
// swaps nothing. The cadence file and its logic are untouched by this phase, so
// the swap is gated on a check that actually happened.
func TestAutoUpdateKeeps24hCadence(t *testing.T) {
	root, exePath := autoUpdateTestEnv(t)
	writeStateWithInstallMethod(t, root, "installer")

	installer := &fakeInstaller{}
	updateInstallerOverride = installer
	resolver := &fakeTagResolver{tag: "v99.0.0"}
	selfupdateNoticeResolverOverride = resolver
	selfupdateAgerOverride = &fakeAger{age: 24 * time.Hour}

	// The check ran an hour ago — inside the 24h window.
	if err := selfupdate.SaveNoticeState(selfupdate.NoticeStatePath(), selfupdate.NoticeState{
		LastCheck: time.Now().Add(-time.Hour),
		LatestTag: "v99.0.0",
	}); err != nil {
		t.Fatalf("seed notice state: %v", err)
	}

	out := runAutoLane(t)

	if resolver.calls != 0 {
		t.Errorf("a cache hit must make no remote request, got %d", resolver.calls)
	}
	if installer.calls != 0 {
		t.Errorf("a cache hit must not swap, got %d install call(s)", installer.calls)
	}
	if got, _ := os.ReadFile(exePath); string(got) != "old binary" {
		t.Errorf("binary must be untouched on a cache hit, got %q", got)
	}
	if !strings.Contains(out, "v99.0.0 is available") {
		t.Errorf("the cached notice must still print, got:\n%s", out)
	}
}

// TestAutoUpdateLeavesASourceBuildAlone — a locally built binary keeps the
// notice and nothing else; auto-replacing a developer's own build would be a
// hostile surprise.
func TestAutoUpdateLeavesASourceBuildAlone(t *testing.T) {
	_, _ = autoUpdateTestEnv(t)
	exePath := seedBinaryAt(t, filepath.Join(t.TempDir(), "go-build", "bravros"), "dev binary")

	installer := &fakeInstaller{}
	updateInstallerOverride = installer
	selfupdateNoticeResolverOverride = &fakeTagResolver{tag: "v99.0.0"}
	selfupdateAgerOverride = &fakeAger{age: 24 * time.Hour}

	out := runAutoLane(t)

	if installer.calls != 0 {
		t.Errorf("a source build must not be auto-replaced, got %d", installer.calls)
	}
	if got, _ := os.ReadFile(exePath); string(got) != "dev binary" {
		t.Errorf("the dev binary must be untouched, got %q", got)
	}
	if !strings.Contains(out, "v99.0.0 is available") {
		t.Errorf("a source build still gets the notice, got:\n%s", out)
	}
}

// ── announce lane (P-0020 Phase 3) ──────────────────────────────────────────

// writeStateWithAnnounce writes a setup.json carrying install_method plus the
// three announce fields, mirroring writeStateWithInstallMethod's shape so a
// missing field ("") reads exactly like a file that predates P-0020.
func writeStateWithAnnounce(t *testing.T, root, method, command, template, language string) {
	t.Helper()
	st := setupState{
		Schema:           setupStateSchema,
		InstallMethod:    method,
		ClaudeRoot:       root,
		AnnounceCommand:  command,
		AnnounceTemplate: template,
		AnnounceLanguage: language,
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(setupStatePath(root)), 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(setupStatePath(root), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write state.json: %v", err)
	}
}

// writeAnnounceStub writes a recording shell script: every invocation appends
// its argv, one element per line, to recordPath.
func writeAnnounceStub(t *testing.T, recordPath string) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "announce-stub.sh")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"" + recordPath + "\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write announce stub: %v", err)
	}
	return scriptPath
}

// waitForFile polls for path to appear, up to timeout, returning its content
// or nil if the deadline passes first. The dispatch under test is
// fire-and-forget async, so a fixed sleep would either flake or waste time.
func waitForFile(t *testing.T, path string, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if data, err := os.ReadFile(path); err == nil {
			return data
		}
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestAutoUpdateAnnouncesAfterSwap — a swap with announce_command set in
// setup.json invokes it with --force <rendered message> studio.
func TestAutoUpdateAnnouncesAfterSwap(t *testing.T) {
	root, _ := autoUpdateTestEnv(t)
	t.Setenv("BRAVROS_ANNOUNCE_CMD", "")
	recordPath := filepath.Join(t.TempDir(), "record.txt")
	stub := writeAnnounceStub(t, recordPath)
	writeStateWithAnnounce(t, root, "installer", stub, "", "")

	installer := &fakeInstaller{}
	updateInstallerOverride = installer
	selfupdateNoticeResolverOverride = &fakeTagResolver{tag: "v99.0.0"}
	selfupdateAgerOverride = &fakeAger{age: 24 * time.Hour}

	runAutoLane(t)

	data := waitForFile(t, recordPath, 2*time.Second)
	if data == nil {
		t.Fatal("announce_command was never invoked")
	}
	want := []string{"--force", selfupdate.RenderAnnouncement("", "", "v99.0.0"), "studio"}
	got := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("argv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestAutoUpdateAnnounceAbsentInvokesNothing — no announce_command anywhere
// (env or setup.json) means no notifier is ever run.
func TestAutoUpdateAnnounceAbsentInvokesNothing(t *testing.T) {
	root, _ := autoUpdateTestEnv(t)
	t.Setenv("BRAVROS_ANNOUNCE_CMD", "")
	recordPath := filepath.Join(t.TempDir(), "record.txt")
	_ = writeAnnounceStub(t, recordPath) // never wired into state — proves absence, not a broken path
	writeStateWithInstallMethod(t, root, "installer")

	installer := &fakeInstaller{}
	updateInstallerOverride = installer
	selfupdateNoticeResolverOverride = &fakeTagResolver{tag: "v99.0.0"}
	selfupdateAgerOverride = &fakeAger{age: 24 * time.Hour}

	runAutoLane(t)

	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(recordPath); err == nil {
		t.Error("no announce_command was configured, but the stub ran")
	}
}

// TestAutoUpdateAnnounceBadCommandStillSwaps — a nonexistent announce_command
// path must never block the swap or the component refresh.
func TestAutoUpdateAnnounceBadCommandStillSwaps(t *testing.T) {
	root, exePath := autoUpdateTestEnv(t)
	t.Setenv("BRAVROS_ANNOUNCE_CMD", "")
	nonexistent := filepath.Join(t.TempDir(), "does-not-exist", "stub.sh")
	writeStateWithAnnounce(t, root, "installer", nonexistent, "", "")

	installer := &fakeInstaller{}
	updateInstallerOverride = installer
	selfupdateAgerOverride = &fakeAger{age: 24 * time.Hour}
	refreshedWith := ""
	updateRefreshHook = func(p string) error { refreshedWith = p; return nil }

	var swapped bool
	captureStderr(t, func() { swapped = selfupdateAutoUpdate("v99.0.0") })

	if !swapped {
		t.Fatal("a bad announce_command must not block the swap")
	}
	if installer.calls != 1 {
		t.Errorf("expected exactly one install, got %d", installer.calls)
	}
	if got, _ := os.ReadFile(exePath); string(got) != "new binary v99.0.0" {
		t.Errorf("binary was not replaced, got %q", got)
	}
	if refreshedWith != exePath {
		t.Errorf("component refresh must still run, got %q", refreshedWith)
	}
}

// writeAutoUpdateState writes a setup.json carrying BOTH install_method and the
// auto_update field. It is hand-rolled JSON on purpose: auto_update is not a
// field of setupState (that record is owned elsewhere), and the lane must
// honour an operator's opt-out whether or not the struct has caught up.
func writeAutoUpdateState(t *testing.T, root, method string, autoUpdate bool) {
	t.Helper()
	body := `{
  "schema": 1,
  "install_method": "` + method + `",
  "claude_root": "` + root + `",
  "auto_update": ` + map[bool]string{true: "true", false: "false"}[autoUpdate] + `,
  "components": []
}
`
	if err := os.MkdirAll(filepath.Dir(setupStatePath(root)), 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(setupStatePath(root), []byte(body), 0o644); err != nil {
		t.Fatalf("write setup.json: %v", err)
	}
}
