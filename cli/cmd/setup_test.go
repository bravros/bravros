package cmd

// setup_test.go — every assertion here observes the FILESYSTEM, never an exit
// code. Several paths in this codebase return nil while having done nothing
// (cmd/selfupdate.go is the canonical example), so "err == nil" proves only
// that nothing blew up; what was installed, preserved, or removed is decided by
// reading the target tree back.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bravros/bravros/cli/internal/payload"
	"github.com/spf13/cobra"
)

// setupTestRoot points config.ConfigDir() (and therefore the whole verb) at a
// throwaway directory, and clears every env var the command reads.
func setupTestRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".claude")
	t.Setenv("BRAVROS_CONFIG_DIR", root)
	t.Setenv(setupComponentsEnv, "")
	t.Setenv(setupAllowPluginManagedEnv, "")
	t.Setenv(setupInstallMethodEnv, "")
	return root
}

type setupFlags struct {
	all        bool
	components string
	skills     string
	yes        bool
}

// runSetupForTest drives runSetup with an explicit flag set and captures its
// output. The command's flags are package-level vars, so they are reset on
// every call — a leaked flag from a previous test would otherwise change the
// meaning of the next one.
func runSetupForTest(t *testing.T, f setupFlags) (string, error) {
	t.Helper()
	setupAll, setupComponents, setupSkills, setupYes = f.all, f.components, f.skills, f.yes
	t.Cleanup(func() {
		setupAll, setupComponents, setupSkills, setupYes = false, "", "", false
	})

	var buf bytes.Buffer
	c := &cobra.Command{}
	c.SetOut(&buf)
	err := runSetup(c, nil)
	return buf.String(), err
}

func readState(t *testing.T, root string) setupState {
	t.Helper()
	data, err := os.ReadFile(setupStatePath(root))
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}
	var st setupState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("state.json is not valid JSON: %v", err)
	}
	return st
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// TestSetupAllYesInstallsEverythingAndIsIdempotent is the acceptance path:
// `setup --all --yes` on a clean root, then a second run that must change
// nothing.
func TestSetupAllYesInstallsEverythingAndIsIdempotent(t *testing.T) {
	root := setupTestRoot(t)

	out, err := runSetupForTest(t, setupFlags{all: true, yes: true})
	if err != nil {
		t.Fatalf("first run: %v\n%s", err, out)
	}

	allSkills, err := payload.SkillNames()
	if err != nil {
		t.Fatal(err)
	}
	installed := dirNames(t, filepath.Join(root, "skills"))
	if len(installed) != len(allSkills) {
		t.Fatalf("--all installed %d skill dirs, embed carries %d", len(installed), len(allSkills))
	}
	for _, name := range allSkills {
		if _, statErr := os.Stat(filepath.Join(root, "skills", name, "SKILL.md")); statErr != nil {
			t.Fatalf("skill %s missing after --all: %v", name, statErr)
		}
	}

	// templates + the executable bit that go:embed drops and payload.Extract
	// restores from its own manifest.
	hook := filepath.Join(root, "templates", ".githooks", "commit-msg")
	info, err := os.Stat(hook)
	if err != nil {
		t.Fatalf("templates/.githooks/commit-msg missing: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("commit-msg hook is not executable: %v", info.Mode())
	}

	if _, err := os.Stat(filepath.Join(root, "settings.json")); err != nil {
		t.Fatalf("settings.json not written: %v", err)
	}

	st := readState(t, root)
	if st.Schema != setupStateSchema {
		t.Errorf("state schema = %d, want %d", st.Schema, setupStateSchema)
	}
	if st.SkillsScope != payload.ScopeAll {
		t.Errorf("state skills scope = %q, want %q", st.SkillsScope, payload.ScopeAll)
	}
	if len(st.Components) != len(payload.Components()) {
		t.Errorf("state records %d components, want %d", len(st.Components), len(payload.Components()))
	}
	var skillsRecorded []string
	for _, c := range st.Components {
		if c.ID == "claude-skills" {
			skillsRecorded = c.Skills
		}
	}
	if len(skillsRecorded) != len(allSkills) {
		t.Errorf("state records %d resolved skills, want %d", len(skillsRecorded), len(allSkills))
	}
	if st.InstallMethod == "" {
		t.Error("state records no install method")
	}

	// Second run: same selection, nothing to do.
	out2, err := runSetupForTest(t, setupFlags{all: true, yes: true})
	if err != nil {
		t.Fatalf("second run: %v\n%s", err, out2)
	}
	if !strings.Contains(out2, "already up to date") {
		t.Errorf("second run did not report an unchanged install:\n%s", out2)
	}
	if strings.Contains(out2, ".new") && strings.Contains(out2, "kept your") {
		t.Errorf("second run produced .new conflicts:\n%s", out2)
	}
}

// TestSetupNeverOverwritesADifferingFile pins the non-destructive contract: a
// hand-edited file survives byte-for-byte and the payload's version lands
// beside it as <name>.new.
func TestSetupNeverOverwritesADifferingFile(t *testing.T) {
	root := setupTestRoot(t)
	if out, err := runSetupForTest(t, setupFlags{all: true, yes: true}); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}

	names, err := payload.ResolveSkills(payload.ScopeCore)
	if err != nil || len(names) == 0 {
		t.Fatalf("resolve core skills: %v", err)
	}
	victim := filepath.Join(root, "skills", names[0], "SKILL.md")
	mine := []byte("# my own edit, do not clobber\n")
	if err := os.WriteFile(victim, mine, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runSetupForTest(t, setupFlags{all: true, yes: true})
	if err != nil {
		t.Fatalf("re-run: %v\n%s", err, out)
	}

	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, mine) {
		t.Fatalf("hand-edited file was overwritten:\n%s", got)
	}
	newFile := victim + ".new"
	payloadVersion, err := os.ReadFile(newFile)
	if err != nil {
		t.Fatalf("expected %s to be written: %v", newFile, err)
	}
	if bytes.Equal(payloadVersion, mine) {
		t.Error(".new file does not carry the payload version")
	}
	if !strings.Contains(out, "kept your") {
		t.Errorf("conflict was not reported:\n%s", out)
	}
}

// TestSetupCoreScopeInstallsOnlyCoreSkills — the D11 default. Scope core must
// be a strict subset of the payload, and must match payload.ResolveSkills.
func TestSetupCoreScopeInstallsOnlyCoreSkills(t *testing.T) {
	root := setupTestRoot(t)

	if out, err := runSetupForTest(t, setupFlags{components: "claude-skills", skills: string(payload.ScopeCore), yes: true}); err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}

	core, err := payload.ResolveSkills(payload.ScopeCore)
	if err != nil {
		t.Fatal(err)
	}
	all, err := payload.SkillNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(core) >= len(all) {
		t.Fatalf("core (%d) is not a strict subset of all (%d) — the manifest premise moved", len(core), len(all))
	}

	installed := dirNames(t, filepath.Join(root, "skills"))
	if len(installed) != len(core) {
		t.Fatalf("installed %d skills, core resolves to %d", len(installed), len(core))
	}
	have := map[string]bool{}
	for _, n := range installed {
		have[n] = true
	}
	for _, n := range core {
		if !have[n] {
			t.Errorf("core skill %s was not installed", n)
		}
	}

	// claude-templates was not selected, so nothing may appear there.
	if _, err := os.Stat(filepath.Join(root, "templates")); !os.IsNotExist(err) {
		t.Errorf("templates/ written although claude-templates was not selected (err=%v)", err)
	}
}

// TestSetupPluginManagedInstallWarnsAndWritesNothing covers D7 and the
// acceptance criterion: a warning, and no write into any plugin directory.
func TestSetupPluginManagedInstallWarnsAndWritesNothing(t *testing.T) {
	root := setupTestRoot(t)
	pluginSkill := filepath.Join(root, "plugins", "marketplaces", "bravros", "skills", "commit")
	if err := os.MkdirAll(pluginSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(pluginSkill, "SKILL.md")
	if err := os.WriteFile(marker, []byte("marketplace copy\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runSetupForTest(t, setupFlags{all: true, yes: true})
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	if !strings.Contains(out, "plugin-managed") {
		t.Errorf("no plugin-managed warning in output:\n%s", out)
	}

	if _, statErr := os.Stat(filepath.Join(root, "skills")); !os.IsNotExist(statErr) {
		t.Errorf("skills/ was written despite a plugin-managed install (err=%v)", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "templates")); !os.IsNotExist(statErr) {
		t.Errorf("templates/ was written despite a plugin-managed install (err=%v)", statErr)
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "marketplace copy\n" {
		t.Fatalf("the plugin tree was touched: %q %v", string(data), err)
	}

	// The escape hatch still works, and still never writes into plugins/.
	t.Setenv(setupAllowPluginManagedEnv, "1")
	if out, err := runSetupForTest(t, setupFlags{all: true, yes: true}); err != nil {
		t.Fatalf("override run: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(root, "skills")); statErr != nil {
		t.Errorf("override did not install skills: %v", statErr)
	}
	data, err = os.ReadFile(marker)
	if err != nil || string(data) != "marketplace copy\n" {
		t.Fatalf("the plugin tree was touched under the override: %q %v", string(data), err)
	}
}

// TestSetupPruneOnlyRemovesUnmodifiedSkillsItInstalled — narrowing all → core
// may remove only skills a previous run installed AND that are still identical
// to the payload. Everything else stays.
func TestSetupPruneOnlyRemovesUnmodifiedSkillsItInstalled(t *testing.T) {
	root := setupTestRoot(t)
	if out, err := runSetupForTest(t, setupFlags{all: true, yes: true}); err != nil {
		t.Fatalf("install all: %v\n%s", err, out)
	}

	all, err := payload.SkillNames()
	if err != nil {
		t.Fatal(err)
	}
	core, err := payload.ResolveSkills(payload.ScopeCore)
	if err != nil {
		t.Fatal(err)
	}
	isCore := map[string]bool{}
	for _, n := range core {
		isCore[n] = true
	}
	var nonCore []string
	for _, n := range all {
		if !isCore[n] {
			nonCore = append(nonCore, n)
		}
	}
	if len(nonCore) < 2 {
		t.Skip("need at least two non-core skills to exercise pruning")
	}

	edited := nonCore[0]
	doomed := nonCore[1]
	if err := os.WriteFile(filepath.Join(root, "skills", edited, "SKILL.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	handmade := filepath.Join(root, "skills", "my-own-skill")
	if err := os.MkdirAll(handmade, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(handmade, "SKILL.md"), []byte("hand written\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runSetupForTest(t, setupFlags{components: "claude-skills", skills: string(payload.ScopeCore), yes: true})
	if err != nil {
		t.Fatalf("narrow to core: %v\n%s", err, out)
	}

	if _, statErr := os.Stat(filepath.Join(root, "skills", doomed)); !os.IsNotExist(statErr) {
		t.Errorf("unmodified dropped skill %s was not pruned (err=%v)", doomed, statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "skills", edited, "SKILL.md")); statErr != nil {
		t.Errorf("modified dropped skill %s was pruned: %v", edited, statErr)
	}
	if _, statErr := os.Stat(filepath.Join(handmade, "SKILL.md")); statErr != nil {
		t.Errorf("hand-written skill was pruned: %v", statErr)
	}
	for _, n := range core {
		if _, statErr := os.Stat(filepath.Join(root, "skills", n, "SKILL.md")); statErr != nil {
			t.Fatalf("core skill %s lost during prune: %v", n, statErr)
		}
	}
	// Pruning may never leave the two allowed subtrees.
	if !setupSubtreeIsPrunable("skills/x") || !setupSubtreeIsPrunable("templates/x") {
		t.Error("skills and templates must be prunable")
	}
	for _, sub := range []string{"hooks/x", "agents/x", "bin/bravros", "projects/p", "state/setup.json"} {
		if setupSubtreeIsPrunable(sub) {
			t.Errorf("%s must never be prunable", sub)
		}
	}
}

// TestSetupHonorsComponentsEnv — BRAVROS_COMPONENTS drives a non-interactive run.
func TestSetupHonorsComponentsEnv(t *testing.T) {
	root := setupTestRoot(t)
	t.Setenv(setupComponentsEnv, "claude-templates")

	if out, err := runSetupForTest(t, setupFlags{yes: true}); err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(root, "templates")); err != nil {
		t.Fatalf("templates not installed from %s: %v", setupComponentsEnv, err)
	}
	if _, err := os.Stat(filepath.Join(root, "skills")); !os.IsNotExist(err) {
		t.Errorf("skills/ written although only claude-templates was requested (err=%v)", err)
	}

	st := readState(t, root)
	ids := map[string]bool{}
	for _, c := range st.Components {
		ids[c.ID] = true
	}
	if !ids["claude-templates"] {
		t.Error("state does not record claude-templates")
	}
	if !ids["cli"] {
		t.Error("the required cli component must always be recorded")
	}
}

// TestSetupRefusesToGuessWithoutATTY — no flags, no env, no previous run and no
// terminal must fail loudly rather than installing something unasked.
func TestSetupRefusesToGuessWithoutATTY(t *testing.T) {
	root := setupTestRoot(t)
	if setupIsInteractive() {
		t.Skip("test run attached to a TTY")
	}

	_, err := runSetupForTest(t, setupFlags{})
	if err == nil {
		t.Fatal("expected a refusal when nothing selects components")
	}
	if !strings.Contains(err.Error(), setupComponentsEnv) {
		t.Errorf("refusal does not name the escape hatches: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "skills")); !os.IsNotExist(statErr) {
		t.Errorf("something was written despite the refusal (err=%v)", statErr)
	}
}

// TestSetupYesAloneInstallsManifestDefaults — `--yes` with no component list is
// the contract payload.DefaultSelections documents: default-on components at
// the default scope (core), never everything.
func TestSetupYesAloneInstallsManifestDefaults(t *testing.T) {
	root := setupTestRoot(t)

	if out, err := runSetupForTest(t, setupFlags{yes: true}); err != nil {
		t.Fatalf("setup --yes: %v\n%s", err, out)
	}

	core, err := payload.ResolveSkills(payload.ScopeCore)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(dirNames(t, filepath.Join(root, "skills"))); got != len(core) {
		t.Errorf("--yes installed %d skills, core default resolves to %d", got, len(core))
	}
	if _, err := os.Stat(filepath.Join(root, "templates")); err != nil {
		t.Errorf("default-on claude-templates not installed: %v", err)
	}
	if st := readState(t, root); st.SkillsScope != payload.ScopeCore {
		t.Errorf("recorded scope = %q, want %q", st.SkillsScope, payload.ScopeCore)
	}
}

// TestSetupUnknownSelectorsFailLoudly — an unknown component or scope must be an
// error, never a silent fallback to "everything".
func TestSetupUnknownSelectorsFailLoudly(t *testing.T) {
	setupTestRoot(t)

	if _, err := runSetupForTest(t, setupFlags{components: "cursor", yes: true}); err == nil {
		t.Error("unknown component id was accepted")
	}
	if _, err := runSetupForTest(t, setupFlags{components: "claude-skills", skills: "some", yes: true}); err == nil {
		t.Error("unknown skills scope was accepted")
	}
}

// TestSetupReplaysRecordedSelectionOnBareRerun — a bare `bravros setup` after a
// recorded install must reproduce that install, not the manifest defaults. This
// is what makes the acceptance smoke's second, flagless invocation meaningful.
func TestSetupReplaysRecordedSelectionOnBareRerun(t *testing.T) {
	root := setupTestRoot(t)
	if setupIsInteractive() {
		t.Skip("test run attached to a TTY")
	}
	if out, err := runSetupForTest(t, setupFlags{all: true, yes: true}); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}

	out, err := runSetupForTest(t, setupFlags{})
	if err != nil {
		t.Fatalf("bare re-run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "already up to date") {
		t.Errorf("bare re-run was not a no-op:\n%s", out)
	}
	st := readState(t, root)
	if st.SkillsScope != payload.ScopeAll {
		t.Errorf("bare re-run changed the recorded scope to %q", st.SkillsScope)
	}
	all, err := payload.SkillNames()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(dirNames(t, filepath.Join(root, "skills"))); got != len(all) {
		t.Errorf("bare re-run left %d skills installed, want %d", got, len(all))
	}
}

// TestSetupDoesNotCollideWithWorktreeSetup — `bravros setup` and
// `bravros worktree setup` / `setup-full` must resolve to different commands
// (P-0015 Traps).
func TestSetupDoesNotCollideWithWorktreeSetup(t *testing.T) {
	top, _, err := rootCmd.Find([]string{"setup"})
	if err != nil {
		t.Fatalf("bravros setup does not resolve: %v", err)
	}
	if top != setupCmd {
		t.Fatalf("bravros setup resolved to %q, want the top-level setup verb", top.CommandPath())
	}

	wt, _, err := rootCmd.Find([]string{"worktree", "setup", "some-branch"})
	if err != nil {
		t.Fatalf("bravros worktree setup does not resolve: %v", err)
	}
	if wt != worktreeSetupCmd {
		t.Fatalf("bravros worktree setup resolved to %q", wt.CommandPath())
	}
	full, _, err := rootCmd.Find([]string{"worktree", "setup-full", "some-branch"})
	if err != nil {
		t.Fatalf("bravros worktree setup-full does not resolve: %v", err)
	}
	if full != worktreeSetupFullCmd {
		t.Fatalf("bravros worktree setup-full resolved to %q", full.CommandPath())
	}

	// Help text lists the top-level verb exactly once.
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	t.Cleanup(func() { rootCmd.SetOut(nil) })
	if err := rootCmd.Usage(); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(buf.String(), "\n  setup "); n != 1 {
		t.Errorf("root help lists the setup verb %d time(s), want 1:\n%s", n, buf.String())
	}
}

// TestSetupInstallMethodDetection — Phase 6 refuses to self-replace a
// package-manager-owned binary, so the recorded method has to be right.
func TestSetupInstallMethodDetection(t *testing.T) {
	root := "/home/u/.claude"
	cases := []struct {
		exec string
		want string
	}{
		{"/opt/homebrew/Cellar/bravros/3.1.0/bin/bravros", "brew"},
		{"/home/linuxbrew/.linuxbrew/bin/bravros", "brew"},
		{"C:/Users/u/scoop/apps/bravros/current/bravros.exe", "scoop"},
		{"/home/u/.claude/bin/bravros", "installer"},
		{"/tmp/bravros-dev", "source"},
		{"", "unknown"},
	}
	for _, tc := range cases {
		if got := detectInstallMethod(tc.exec, root); got != tc.want {
			t.Errorf("detectInstallMethod(%q) = %q, want %q", tc.exec, got, tc.want)
		}
	}

	t.Setenv(setupInstallMethodEnv, "curl")
	if got := detectInstallMethod("/opt/homebrew/Cellar/bravros/3.1.0/bin/bravros", root); got != "curl" {
		t.Errorf("env override ignored: %q", got)
	}
}

// TestSetupInstallMethodDetection_ResolvesSymlink pins the Intel-macOS Homebrew
// shape, which the table test above cannot catch because it feeds paths that are
// already resolved.
//
// Homebrew on Intel installs to /usr/local/bin/bravros as a SYMLINK into
// ../Cellar/. Only the RESOLVED path contains "/Cellar/", so detecting against
// the raw os.Executable() records a brew install as "source" — and Phase 6's
// `bravros update` would then self-replace a Homebrew-managed binary instead of
// refusing and pointing at `brew upgrade`. Apple Silicon masks the bug entirely
// (/opt/homebrew/bin/... already contains "/homebrew/" unresolved), so this is
// only reproducible via an actual symlink.
func TestSetupInstallMethodDetection_ResolvesSymlink(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, ".claude")

	cellar := filepath.Join(tmp, "Cellar", "bravros", "3.1.0", "bin")
	if err := os.MkdirAll(cellar, 0o755); err != nil {
		t.Fatalf("mkdir cellar: %v", err)
	}
	real := filepath.Join(cellar, "bravros")
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write real binary: %v", err)
	}

	linkDir := filepath.Join(tmp, "usr", "local", "bin")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatalf("mkdir link dir: %v", err)
	}
	link := filepath.Join(linkDir, "bravros")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}

	// The raw symlink path must NOT look like brew — if this ever starts
	// returning "brew", the test below stops proving anything.
	if got := detectInstallMethod(link, root); got == "brew" {
		t.Fatalf("precondition broken: raw symlink path %q already detects as brew, so this test no longer proves symlink resolution matters", link)
	}

	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", link, err)
	}
	if got := detectInstallMethod(resolved, root); got != "brew" {
		t.Errorf("detectInstallMethod(resolved %q) = %q, want \"brew\" — a Homebrew install recorded as %q would let `bravros update` self-replace a brew-managed binary", resolved, got, got)
	}
}

// TestSetupScopeResolutionPrecedence — --skills beats --all beats the recorded
// scope beats the manifest default (core for a fresh run).
func TestSetupScopeResolutionPrecedence(t *testing.T) {
	prevAll := &setupState{SkillsScope: payload.ScopeAll}

	cases := []struct {
		name string
		flag string
		all  bool
		prev *setupState
		want payload.SkillScope
	}{
		{"fresh default", "", false, nil, payload.ScopeCore},
		{"--all implies all", "", true, nil, payload.ScopeAll},
		{"--skills wins over --all", string(payload.ScopeCore), true, nil, payload.ScopeCore},
		{"recorded scope replayed", "", false, prevAll, payload.ScopeAll},
		{"--skills wins over record", string(payload.ScopeCore), false, prevAll, payload.ScopeCore},
	}
	for _, tc := range cases {
		got, err := setupResolveScope(tc.flag, tc.all, tc.prev, payload.ScopeCore)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: scope = %q, want %q", tc.name, got, tc.want)
		}
	}
}
