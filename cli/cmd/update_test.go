package cmd

// update_test.go — `bravros update` orchestration. Like setup_test.go, every
// assertion reads the FILESYSTEM or counts requests; an exit code proves
// nothing in this area of the codebase.
//
// The trust chain (download → minisign → sha256 → extract → atomic swap) is
// exercised end to end against a real signed archive in
// internal/selfupdate/binary_test.go. Here the installer is faked so the tests
// can pin the decisions AROUND it: the package-manager refusal, the version
// gate, and the post-swap component refresh.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bravros/bravros/cli/internal/selfupdate"
	"github.com/spf13/cobra"
)

// ── fakes ───────────────────────────────────────────────────────────────────

type fakeInstaller struct {
	calls   int
	tag     string
	exePath string
	result  selfupdate.UpdateResult
	err     error
}

func (f *fakeInstaller) AssetName() string { return "bravros-test-arch.tar.gz" }

func (f *fakeInstaller) Install(_ context.Context, tag, exePath string) (selfupdate.UpdateResult, error) {
	f.calls++
	f.tag, f.exePath = tag, exePath
	if f.err != nil {
		return selfupdate.UpdateResult{}, f.err
	}
	res := f.result
	res.Tag, res.ExePath = tag, exePath
	// Model the real swap: the file at exePath now holds the new build.
	if err := os.WriteFile(exePath, []byte("new binary "+tag), 0o755); err != nil {
		return res, err
	}
	return res, nil
}

type fakeTagResolver struct {
	calls int
	tag   string
	err   error
}

func (f *fakeTagResolver) ResolveLatestTag(context.Context) (string, error) {
	f.calls++
	return f.tag, f.err
}

// updateTestEnv isolates every path `update` touches and returns
// (root, exePath). The seeded executable stands in for the running binary, so
// a test can never replace the binary running the test.
func updateTestEnv(t *testing.T) (root, exePath string) {
	t.Helper()
	home := t.TempDir()
	root = filepath.Join(home, ".claude")
	t.Setenv("HOME", home)
	t.Setenv("BRAVROS_CONFIG_DIR", root)
	t.Setenv(updateInstallMethodOverrideEnv, "")
	t.Setenv(selfupdate.NoUpdateCheckEnv, "")

	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	exePath = filepath.Join(binDir, "bravros")
	if err := os.WriteFile(exePath, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("seed executable: %v", err)
	}

	updateExePathOverride = exePath
	refreshed := 0
	updateRefreshHook = func(string) error { refreshed++; return nil }
	t.Cleanup(func() {
		updateExePathOverride = ""
		updateRefreshHook = nil
		updateInstallerOverride = nil
		updateResolverOverride = nil
		updateCheckOnly, updateForce, updateTag = false, false, ""
	})
	return root, exePath
}

func writeStateWithInstallMethod(t *testing.T, root, method string) {
	t.Helper()
	st := setupState{
		Schema:        setupStateSchema,
		InstallMethod: method,
		ClaudeRoot:    root,
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

func runUpdateForTest(t *testing.T) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	c := &cobra.Command{}
	c.SetOut(&buf)
	err := runUpdate(c, nil)
	return buf.String(), err
}

// ── package-manager refusal ─────────────────────────────────────────────────

// seedBinaryAt writes a stand-in executable at an arbitrary path and points the
// command at it, modelling where a given install method really puts the file.
func seedBinaryAt(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("seed binary at %s: %v", path, err)
	}
	updateExePathOverride = path
	return path
}

// TestUpdateRefusesBrewInstall is the named acceptance test: a genuinely
// brew-owned binary (Cellar path, and brew recorded in state.json) must be
// refused, must name `brew upgrade bravros`, and must be left byte-identical.
func TestUpdateRefusesBrewInstall(t *testing.T) {
	root, _ := updateTestEnv(t)
	writeStateWithInstallMethod(t, root, "brew")
	exePath := seedBinaryAt(t, filepath.Join(t.TempDir(), "homebrew", "Cellar", "bravros", "2.11.0", "bin", "bravros"), "old binary")

	resolver := &fakeTagResolver{tag: "v99.0.0"}
	installer := &fakeInstaller{}
	updateResolverOverride = resolver
	updateInstallerOverride = installer

	out, err := runUpdateForTest(t)
	if err == nil {
		t.Fatal("update must refuse a brew-managed binary")
	}
	if !strings.Contains(out, "brew upgrade bravros") {
		t.Errorf("refusal must name the right upgrade command, got:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "brew") || !strings.Contains(out, "refusing") {
		t.Errorf("refusal message is not explicit enough:\n%s", out)
	}
	// Nothing may happen after a refusal — no network, no download, no swap.
	if resolver.calls != 0 {
		t.Errorf("refusal must come before any remote call, got %d", resolver.calls)
	}
	if installer.calls != 0 {
		t.Errorf("refusal must not download anything, got %d install call(s)", installer.calls)
	}
	if got, _ := os.ReadFile(exePath); string(got) != "old binary" {
		t.Errorf("binary must be untouched after a refusal, got %q", got)
	}
}

func TestUpdateRefusesScoopInstall(t *testing.T) {
	root, _ := updateTestEnv(t)
	writeStateWithInstallMethod(t, root, "scoop")
	exePath := seedBinaryAt(t, filepath.Join(t.TempDir(), "scoop", "apps", "bravros", "current", "bravros.exe"), "old binary")
	updateInstallerOverride = &fakeInstaller{}
	updateResolverOverride = &fakeTagResolver{tag: "v99.0.0"}

	out, err := runUpdateForTest(t)
	if err == nil {
		t.Fatal("update must refuse a scoop-managed binary")
	}
	if !strings.Contains(out, "scoop update bravros") {
		t.Errorf("refusal must name `scoop update bravros`, got:\n%s", out)
	}
	if got, _ := os.ReadFile(exePath); string(got) != "old binary" {
		t.Errorf("binary must be untouched after a refusal, got %q", got)
	}
}

// TestUpdateRefusesDetectedBrewWhenStateIsMissing covers the pre-v2 install:
// no state.json at all, so the method has to be detected from the binary's own
// path. A missing field must never read as "safe to replace".
func TestUpdateRefusesDetectedBrewWhenStateIsMissing(t *testing.T) {
	_, _ = updateTestEnv(t)

	cellar := filepath.Join(t.TempDir(), "homebrew", "Cellar", "bravros", "2.11.0", "bin")
	if err := os.MkdirAll(cellar, 0o755); err != nil {
		t.Fatalf("mkdir cellar: %v", err)
	}
	brewExe := filepath.Join(cellar, "bravros")
	if err := os.WriteFile(brewExe, []byte("brew binary"), 0o755); err != nil {
		t.Fatalf("seed brew binary: %v", err)
	}
	updateExePathOverride = brewExe

	installer := &fakeInstaller{}
	updateInstallerOverride = installer

	out, err := runUpdateForTest(t)
	if err == nil {
		t.Fatal("a brew-path binary with no state.json must still be refused")
	}
	if !strings.Contains(out, "brew upgrade bravros") {
		t.Errorf("refusal must name the right command, got:\n%s", out)
	}
	if installer.calls != 0 {
		t.Errorf("nothing may be downloaded after a refusal, got %d", installer.calls)
	}
	if got, _ := os.ReadFile(brewExe); string(got) != "brew binary" {
		t.Errorf("brew binary must be untouched, got %q", got)
	}
}

// TestUpdateProceedsWhenRecordSaysBrewButTheBinaryIsNot — observed reality wins
// over the record. The file at os.Executable() is the one being replaced; if it
// lives in ~/.claude/bin then brew's copy is a different file and is untouched,
// so refusing would block a legitimate update on a stale record.
func TestUpdateProceedsWhenRecordSaysBrewButTheBinaryIsNot(t *testing.T) {
	root, exePath := updateTestEnv(t)
	writeStateWithInstallMethod(t, root, "brew") // stale/incorrect record
	// exePath is <root>/bin/bravros → detection says "installer".

	installer := &fakeInstaller{}
	updateInstallerOverride = installer
	updateResolverOverride = &fakeTagResolver{tag: "v99.0.0"}

	out, err := runUpdateForTest(t)
	if err != nil {
		t.Fatalf("a non-brew binary must be updatable despite the record: %v", err)
	}
	if installer.calls != 1 {
		t.Errorf("expected the update to proceed, got %d install call(s)", installer.calls)
	}
	if !strings.Contains(out, "install_method") || !strings.Contains(out, "leaves that install alone") {
		t.Errorf("the mismatch should be surfaced as one informational line, got:\n%s", out)
	}
	if got, _ := os.ReadFile(exePath); !strings.HasPrefix(string(got), "new binary") {
		t.Errorf("binary was not replaced, got %q", got)
	}
}

// TestUpdateRefusesIntelMacBrewSymlink is the case a raw-path check misses:
// Intel-mac brew installs to /usr/local/bin/bravros, a SYMLINK into ../Cellar/.
// Only the resolved path names brew, and the resolved path is the file that
// would be replaced.
func TestUpdateRefusesIntelMacBrewSymlink(t *testing.T) {
	root, _ := updateTestEnv(t)
	_ = root

	base := t.TempDir()
	cellarBin := filepath.Join(base, "Cellar", "bravros", "2.11.0", "bin")
	if err := os.MkdirAll(cellarBin, 0o755); err != nil {
		t.Fatalf("mkdir cellar: %v", err)
	}
	realExe := filepath.Join(cellarBin, "bravros")
	if err := os.WriteFile(realExe, []byte("brew binary"), 0o755); err != nil {
		t.Fatalf("seed cellar binary: %v", err)
	}
	linkDir := filepath.Join(base, "usr-local-bin")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatalf("mkdir link dir: %v", err)
	}
	link := filepath.Join(linkDir, "bravros")
	if err := os.Symlink(realExe, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// updateExecutablePath resolves symlinks; model that here, since the test
	// bypasses os.Executable().
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	updateExePathOverride = resolved

	installer := &fakeInstaller{}
	updateInstallerOverride = installer

	out, err := runUpdateForTest(t)
	if err == nil {
		t.Fatal("a symlink into Cellar must still be recognised as brew-owned")
	}
	if !strings.Contains(out, "brew upgrade bravros") {
		t.Errorf("refusal must name the right command, got:\n%s", out)
	}
	if installer.calls != 0 {
		t.Errorf("nothing may be downloaded after a refusal, got %d", installer.calls)
	}
	if got, _ := os.ReadFile(realExe); string(got) != "brew binary" {
		t.Errorf("the Cellar binary must be untouched, got %q", got)
	}
}

func TestUpdateProceedsForInstallerManagedBinary(t *testing.T) {
	root, exePath := updateTestEnv(t)
	writeStateWithInstallMethod(t, root, "installer")

	refreshedWith := ""
	updateRefreshHook = func(p string) error { refreshedWith = p; return nil }
	updateResolverOverride = &fakeTagResolver{tag: "v99.0.0"}
	installer := &fakeInstaller{}
	updateInstallerOverride = installer

	out, err := runUpdateForTest(t)
	if err != nil {
		t.Fatalf("installer-managed update must proceed: %v", err)
	}
	if installer.calls != 1 {
		t.Fatalf("expected exactly one install, got %d", installer.calls)
	}
	if installer.tag != "v99.0.0" {
		t.Errorf("installed tag %q, want v99.0.0", installer.tag)
	}
	if got, _ := os.ReadFile(exePath); string(got) != "new binary v99.0.0" {
		t.Errorf("binary was not replaced, got %q", got)
	}
	// The post-swap refresh must run the NEW binary — the payload is embedded,
	// so the still-running process could only ever redeploy the OLD skills.
	if refreshedWith != exePath {
		t.Errorf("post-swap refresh ran against %q, want %q", refreshedWith, exePath)
	}
	if !strings.Contains(out, "components refreshed") {
		t.Errorf("output should report the component refresh:\n%s", out)
	}
}

// TestUpdateDropsTheCheckMarkerAfterASwap — a new binary carries a new embedded
// payload, and the 6h TTL marker would otherwise suppress the refresh.
func TestUpdateDropsTheCheckMarkerAfterASwap(t *testing.T) {
	root, _ := updateTestEnv(t)
	writeStateWithInstallMethod(t, root, "installer")

	home := filepath.Dir(root)
	marker := filepath.Join(legacyClaudeStateDir(home), selfupdateCheckMarker)
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(marker, []byte{}, 0o644); err != nil {
		t.Fatalf("seed marker: %v", err)
	}

	updateResolverOverride = &fakeTagResolver{tag: "v99.0.0"}
	updateInstallerOverride = &fakeInstaller{}

	if _, err := runUpdateForTest(t); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the TTL marker must be dropped after a swap so the next session refreshes")
	}
}

// ── version gate ────────────────────────────────────────────────────────────

func TestUpdateSkipsDownloadWhenAlreadyCurrent(t *testing.T) {
	root, exePath := updateTestEnv(t)
	writeStateWithInstallMethod(t, root, "installer")

	installer := &fakeInstaller{}
	updateInstallerOverride = installer
	updateResolverOverride = &fakeTagResolver{tag: "v" + strings.TrimPrefix(Version, "v")}

	out, err := runUpdateForTest(t)
	if err != nil {
		t.Fatalf("an up-to-date check must not error: %v", err)
	}
	if installer.calls != 0 {
		t.Errorf("nothing to install, but Install was called %d time(s)", installer.calls)
	}
	if !strings.Contains(out, "up to date") {
		t.Errorf("expected an up-to-date report, got:\n%s", out)
	}
	if got, _ := os.ReadFile(exePath); string(got) != "old binary" {
		t.Errorf("binary must be untouched, got %q", got)
	}
}

func TestUpdateForceReinstallsCurrentVersion(t *testing.T) {
	root, exePath := updateTestEnv(t)
	writeStateWithInstallMethod(t, root, "installer")

	installer := &fakeInstaller{}
	updateInstallerOverride = installer
	updateResolverOverride = &fakeTagResolver{tag: "v" + strings.TrimPrefix(Version, "v")}
	updateForce = true

	if _, err := runUpdateForTest(t); err != nil {
		t.Fatalf("--force update: %v", err)
	}
	if installer.calls != 1 {
		t.Errorf("--force must reinstall, got %d install call(s)", installer.calls)
	}
	if got, _ := os.ReadFile(exePath); !strings.HasPrefix(string(got), "new binary") {
		t.Errorf("--force did not replace the binary, got %q", got)
	}
}

func TestUpdateCheckOnlyReplacesNothing(t *testing.T) {
	root, exePath := updateTestEnv(t)
	writeStateWithInstallMethod(t, root, "installer")

	installer := &fakeInstaller{}
	updateInstallerOverride = installer
	updateResolverOverride = &fakeTagResolver{tag: "v99.0.0"}
	updateCheckOnly = true

	out, err := runUpdateForTest(t)
	if err != nil {
		t.Fatalf("--check must not error: %v", err)
	}
	if installer.calls != 0 {
		t.Errorf("--check must download nothing, got %d install call(s)", installer.calls)
	}
	if !strings.Contains(out, "v99.0.0 is available") {
		t.Errorf("--check should report the newer release, got:\n%s", out)
	}
	if got, _ := os.ReadFile(exePath); string(got) != "old binary" {
		t.Errorf("--check must leave the binary alone, got %q", got)
	}
}

// TestUpdateRecordsTheNoticeCheck — an explicit update is also a version check,
// so the passive notice must not immediately repeat it on the next session.
func TestUpdateRecordsTheNoticeCheck(t *testing.T) {
	root, _ := updateTestEnv(t)
	writeStateWithInstallMethod(t, root, "installer")
	updateInstallerOverride = &fakeInstaller{}
	updateResolverOverride = &fakeTagResolver{tag: "v99.0.0"}

	if _, err := runUpdateForTest(t); err != nil {
		t.Fatalf("update: %v", err)
	}
	state := selfupdate.LoadNoticeState(selfupdate.NoticeStatePath())
	if state.LatestTag != "v99.0.0" {
		t.Errorf("notice state records %q, want v99.0.0", state.LatestTag)
	}
	if state.LastCheck.IsZero() {
		t.Error("notice state must carry the check timestamp")
	}
}

// TestUpdateSurvivesAFailedComponentRefresh — the binary is already in place by
// then, so a refresh failure is a warning with a retry hint, not a hard error.
func TestUpdateSurvivesAFailedComponentRefresh(t *testing.T) {
	root, exePath := updateTestEnv(t)
	writeStateWithInstallMethod(t, root, "installer")
	updateInstallerOverride = &fakeInstaller{}
	updateResolverOverride = &fakeTagResolver{tag: "v99.0.0"}
	updateRefreshHook = func(string) error { return os.ErrPermission }

	out, err := runUpdateForTest(t)
	if err != nil {
		t.Fatalf("a failed refresh must not fail the update: %v", err)
	}
	if !strings.Contains(out, "component refresh failed") || !strings.Contains(out, "selfupdate --force") {
		t.Errorf("expected a warning naming the retry command, got:\n%s", out)
	}
	if got, _ := os.ReadFile(exePath); !strings.HasPrefix(string(got), "new binary") {
		t.Errorf("the swap itself must have stuck, got %q", got)
	}
}
