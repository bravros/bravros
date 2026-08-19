package payload_test

// This file is package payload_test (black-box), not package payload, on
// purpose: cli/internal/deploy imports cli/internal/payload (P-0018 Phase 3 —
// deploy.reconcileGlobalClaudeMd's embedded-payload fallback), so any test
// file that imports "deploy" from INSIDE package payload creates a genuine
// import cycle ("payload" -> test file -> "deploy" -> "payload"). The tests
// below all exercise payload's exported surface against deploy's exported
// helpers, so moving them to the external test package is a pure mechanical
// split — no unexported access needed. Tests that don't touch "deploy" stay
// in manifest_test.go (package payload).

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/bravros/bravros/cli/internal/deploy"
	"github.com/bravros/bravros/cli/internal/payload"
)

// repoRootSkillsDir returns the path to repo-root skills/ as seen from this
// test file's own directory (cli/internal/payload) — duplicated from
// payload_test.go's internal helper of the same name because an external
// test package cannot reach an unexported helper defined in an internal one.
func repoRootSkillsDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "skills"))
	if err != nil {
		t.Fatalf("resolve repo-root skills dir: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("repo-root skills dir not found at %q (is the payload package still at cli/internal/payload?): %v", dir, err)
	}
	return dir
}

// TestNoComponentTargetsPluginManaged is the manifest-side half of the D7
// guard: setup DETECTS a plugin-managed Claude Code install and warns, and
// never writes into a host's plugin tree. TargetRel is exactly the shape
// deploy.IsPluginManaged consumes, so Phase 3 can run this check verbatim;
// here we assert no component target is plugin-managed to begin with.
func TestNoComponentTargetsPluginManaged(t *testing.T) {
	for _, c := range payload.Components() {
		if deploy.IsPluginManaged(c.TargetRel()) {
			t.Errorf("component %q targets %q, which deploy.IsPluginManaged reports as plugin-managed — bravros must never write there", c.ID, c.TargetRel())
		}
	}
	// Sanity: the check is live, not vacuously false for every input.
	if !deploy.IsPluginManaged("plugins") {
		t.Fatalf("deploy.IsPluginManaged(%q) = false — the guard this test relies on is not working", "plugins")
	}
}

// TestResolveSkills_ScopesDerivedFromTree derives BOTH expected sets from the
// live tree at test time. No hardcoded counts: the dossier said 34, the tree
// has 35, and a magic number rots.
func TestResolveSkills_ScopesDerivedFromTree(t *testing.T) {
	srcDir := repoRootSkillsDir(t)
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("read repo-root skills dir: %v", err)
	}

	var wantAll, wantCore []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if deploy.NonRuntimeSkillDir(e.Name()) {
			continue
		}
		wantAll = append(wantAll, e.Name())
		if deploy.IsSkillCore(filepath.Join(srcDir, e.Name(), "SKILL.md")) {
			wantCore = append(wantCore, e.Name())
		}
	}
	sort.Strings(wantAll)
	sort.Strings(wantCore)

	if len(wantAll) == 0 || len(wantCore) == 0 {
		t.Fatalf("derived %d skills and %d core skills from %q — refusing a vacuous test", len(wantAll), len(wantCore), srcDir)
	}
	// The whole point of scope=core as the DEFAULT is that it is a strict
	// subset: P-0004's carried-forward requirement is that the picker must
	// not preselect everything.
	if len(wantCore) >= len(wantAll) {
		t.Fatalf("core skills (%d) is not a strict subset of all skills (%d) — scope=core would preselect everything", len(wantCore), len(wantAll))
	}

	gotAll, err := payload.ResolveSkills(payload.ScopeAll)
	if err != nil {
		t.Fatalf("ResolveSkills(all): %v", err)
	}
	if !reflect.DeepEqual(gotAll, wantAll) {
		t.Errorf("ResolveSkills(all) = %v (%d), want %v (%d)", gotAll, len(gotAll), wantAll, len(wantAll))
	}

	gotCore, err := payload.ResolveSkills(payload.ScopeCore)
	if err != nil {
		t.Fatalf("ResolveSkills(core): %v", err)
	}
	if !reflect.DeepEqual(gotCore, wantCore) {
		t.Errorf("ResolveSkills(core) = %v (%d), want %v (%d)", gotCore, len(gotCore), wantCore, len(wantCore))
	}
}

// TestSkillIsCore_AgreesWithDeployIsSkillCore proves the embedded-FS-native
// frontmatter parse in SkillIsCore is the same predicate deploy.IsSkillCore
// applies to an on-disk path, for EVERY embedded skill. deploy.IsSkillCore
// cannot read an embed.FS (it takes a filesystem path), so the logic is
// duplicated — this test is what keeps the duplicate from drifting.
func TestSkillIsCore_AgreesWithDeployIsSkillCore(t *testing.T) {
	srcDir := repoRootSkillsDir(t)

	names, err := payload.SkillNames()
	if err != nil {
		t.Fatalf("SkillNames: %v", err)
	}
	if len(names) == 0 {
		t.Fatalf("SkillNames returned nothing — refusing a vacuous test")
	}

	agreedCore := 0
	for _, name := range names {
		got, err := payload.SkillIsCore(name)
		if err != nil {
			t.Fatalf("SkillIsCore(%q): %v", name, err)
		}
		want := deploy.IsSkillCore(filepath.Join(srcDir, name, "SKILL.md"))
		if got != want {
			t.Errorf("SkillIsCore(%q) = %v, deploy.IsSkillCore = %v", name, got, want)
		}
		if got {
			agreedCore++
		}
	}
	if agreedCore == 0 {
		t.Fatalf("no embedded skill was detected as core — the parser is not doing anything")
	}
	t.Logf("agreement over %d embedded skills (%d core)", len(names), agreedCore)
}

func TestSelection_EnabledSkillsFeedsDeployOpts(t *testing.T) {
	c, ok := payload.ComponentByID("claude-skills")
	if !ok {
		t.Fatalf("ComponentByID(claude-skills) not found")
	}
	sel, err := c.Select(payload.ScopeCore)
	if err != nil {
		t.Fatalf("Select(core): %v", err)
	}

	// The list must be usable verbatim as DeployOpts.EnabledSkills: plain
	// skill directory names that exist in the embedded payload.
	enabled := sel.EnabledSkills()
	if len(enabled) == 0 {
		t.Fatalf("EnabledSkills() is empty — an empty allowlist means 'deploy everything', the opposite of scope=core")
	}
	if !reflect.DeepEqual(enabled, sel.Skills) {
		t.Errorf("EnabledSkills() = %v, want the resolved list %v", enabled, sel.Skills)
	}
	for _, name := range enabled {
		if strings.ContainsAny(name, `/\`) {
			t.Errorf("EnabledSkills() entry %q is a path — DeployOpts.EnabledSkills takes bare skill directory names", name)
		}
		if _, err := fs.Stat(payload.FS, path.Join("skills", name)); err != nil {
			t.Errorf("EnabledSkills() entry %q is not an embedded skill: %v", name, err)
		}
	}

	// It is a copy: mutating it must not corrupt the selection a caller is
	// about to persist to state.json.
	enabled[0] = "mutated"
	if sel.Skills[0] == "mutated" {
		t.Errorf("EnabledSkills() aliases Selection.Skills")
	}

	// _ = deploy.DeployOpts consumption shape.
	opts := deploy.DeployOpts{EnabledSkills: sel.EnabledSkills(), FilterMode: false}
	if len(opts.EnabledSkills) != len(sel.Skills) {
		t.Errorf("DeployOpts.EnabledSkills = %d entries, want %d", len(opts.EnabledSkills), len(sel.Skills))
	}
}
