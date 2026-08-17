package payload

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
)

// embeddedTopLevelDirs returns the directories directly under the embedded FS
// root. Files at that level (executable_manifest.txt) are not component
// subtrees and are excluded.
func embeddedTopLevelDirs(t *testing.T) []string {
	t.Helper()
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatalf("read embedded FS root: %v", err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	if len(dirs) == 0 {
		t.Fatalf("embedded FS root has no directories — refusing a vacuous test")
	}
	return dirs
}

// TestComponents_EmbeddedSubtreeBijection states the relationship precisely:
// the bijection is between embedded top-level DIRECTORIES and the components
// of Kind KindEmbeddedTree — not between directories and all components. `cli`
// and `claude-settings` name no subtree at all (a binary the installer script
// places, and merge logic in cli/internal/managed/ respectively), and the type
// must report that as an explicit ok=false rather than an empty string.
func TestComponents_EmbeddedSubtreeBijection(t *testing.T) {
	dirs := embeddedTopLevelDirs(t)

	// Forward: every KindEmbeddedTree component names an existing subtree,
	// and no two components claim the same one.
	claimedBy := map[string]string{} // subtree -> component id
	for _, c := range Components() {
		subtree, ok := c.EmbedSubtree()
		if c.Kind != KindEmbeddedTree {
			if ok {
				t.Errorf("component %q has kind %s but still reported an embed subtree %q — non-tree components must name no subtree", c.ID, c.Kind, subtree)
			}
			continue
		}
		if !ok {
			t.Errorf("component %q has kind %s but reported no embed subtree", c.ID, c.Kind)
			continue
		}
		if info, err := fs.Stat(FS, subtree); err != nil || !info.IsDir() {
			t.Errorf("component %q names embed subtree %q which is not a directory in the embedded FS: %v", c.ID, subtree, err)
			continue
		}
		if prev, dup := claimedBy[subtree]; dup {
			t.Errorf("embed subtree %q is claimed by two components: %q and %q", subtree, prev, c.ID)
			continue
		}
		claimedBy[subtree] = c.ID
	}

	// Reverse: every embedded top-level directory is claimed by exactly one
	// component. An unclaimed directory means the payload grew a tree no
	// component installs.
	for _, dir := range dirs {
		if _, ok := claimedBy[dir]; !ok {
			t.Errorf("embedded top-level directory %q is not claimed by any component — add a component or stop embedding it", dir)
		}
	}
	if len(claimedBy) != len(dirs) {
		t.Errorf("claimed subtrees = %v, embedded top-level directories = %v", claimedBy, dirs)
	}
}

func TestComponents_IDsAndShape(t *testing.T) {
	want := []string{"cli", "claude-skills", "claude-templates", "claude-settings"}
	var got []string
	seen := map[string]bool{}
	for _, c := range Components() {
		if c.ID == "" || c.Label == "" || c.Description == "" {
			t.Errorf("component %+v has an empty id, label or description", c)
		}
		if c.Kind == 0 {
			t.Errorf("component %q has no kind", c.ID)
		}
		if c.Required && !c.Default {
			t.Errorf("component %q is Required but not Default — a non-deselectable component must start checked", c.ID)
		}
		if c.Scoped != (c.DefaultScope != "") {
			t.Errorf("component %q: Scoped=%v but DefaultScope=%q — they must agree", c.ID, c.Scoped, c.DefaultScope)
		}
		if seen[c.ID] {
			t.Errorf("duplicate component id %q", c.ID)
		}
		seen[c.ID] = true
		got = append(got, c.ID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("component ids = %v, want %v", got, want)
	}

	// P-0015 Traps: these are kaisser-era and appear in no Go source. They
	// must never become components.
	for _, forbidden := range []string{"cursor", "copilot", "aider", "opencode"} {
		if _, ok := ComponentByID(forbidden); ok {
			t.Errorf("component %q must not exist — see P-0015 Traps", forbidden)
		}
	}

	cli, ok := ComponentByID("cli")
	if !ok {
		t.Fatalf("ComponentByID(cli) not found")
	}
	if !cli.Required {
		t.Errorf("the cli component must be Required (not deselectable)")
	}

	skills, ok := ComponentByID("claude-skills")
	if !ok {
		t.Fatalf("ComponentByID(claude-skills) not found")
	}
	if !skills.Scoped || skills.DefaultScope != ScopeCore {
		t.Errorf("claude-skills must be scoped with default scope %q, got scoped=%v scope=%q", ScopeCore, skills.Scoped, skills.DefaultScope)
	}
}

func TestComponents_ReturnsACopy(t *testing.T) {
	first := Components()
	first[0].ID = "mutated"
	if Components()[0].ID != "cli" {
		t.Fatalf("mutating the slice returned by Components() changed package state")
	}
}

func TestTargetPaths_ResolveUnderUserHomeDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}
	root, err := ClaudeRoot()
	if err != nil {
		t.Fatalf("ClaudeRoot: %v", err)
	}
	if want := filepath.Join(home, ".claude"); root != want {
		t.Fatalf("ClaudeRoot() = %q, want %q", root, want)
	}

	wantRel := map[string]string{
		"cli":              "bin",
		"claude-skills":    "skills",
		"claude-templates": "templates",
		"claude-settings":  "settings.json",
	}

	fakeRoot := filepath.Join(t.TempDir(), ".claude")
	for _, c := range Components() {
		rel, ok := wantRel[c.ID]
		if !ok {
			t.Fatalf("no expected target recorded for component %q", c.ID)
		}
		if c.TargetRel() != rel {
			t.Errorf("component %q TargetRel() = %q, want %q", c.ID, c.TargetRel(), rel)
		}
		if strings.ContainsAny(c.TargetRel(), `~\`) {
			t.Errorf("component %q TargetRel() = %q — must be a slash-form relative path with no ~ and no backslash", c.ID, c.TargetRel())
		}

		got := c.TargetPathUnder(fakeRoot)
		if want := filepath.Join(fakeRoot, rel); got != want {
			t.Errorf("component %q TargetPathUnder(%q) = %q, want %q", c.ID, fakeRoot, got, want)
		}

		abs, err := c.TargetPath()
		if err != nil {
			t.Fatalf("component %q TargetPath(): %v", c.ID, err)
		}
		if want := filepath.Join(root, rel); abs != want {
			t.Errorf("component %q TargetPath() = %q, want %q", c.ID, abs, want)
		}
		if strings.Contains(abs, "~") {
			t.Errorf("component %q TargetPath() = %q contains a literal ~ — paths must resolve through os.UserHomeDir", c.ID, abs)
		}
	}
}

// TestNoComponentTargetsPluginManaged is the manifest-side half of the D7
// guard: setup DETECTS a plugin-managed Claude Code install and warns, and
// never writes into a host's plugin tree. TargetRel is exactly the shape
// deploy.IsPluginManaged consumes, so Phase 3 can run this check verbatim;
// here we assert no component target is plugin-managed to begin with.
func TestNoComponentTargetsPluginManaged(t *testing.T) {
	for _, c := range Components() {
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

	gotAll, err := ResolveSkills(ScopeAll)
	if err != nil {
		t.Fatalf("ResolveSkills(all): %v", err)
	}
	if !reflect.DeepEqual(gotAll, wantAll) {
		t.Errorf("ResolveSkills(all) = %v (%d), want %v (%d)", gotAll, len(gotAll), wantAll, len(wantAll))
	}

	gotCore, err := ResolveSkills(ScopeCore)
	if err != nil {
		t.Fatalf("ResolveSkills(core): %v", err)
	}
	if !reflect.DeepEqual(gotCore, wantCore) {
		t.Errorf("ResolveSkills(core) = %v (%d), want %v (%d)", gotCore, len(gotCore), wantCore, len(wantCore))
	}
}

func TestResolveSkills_UnknownScopeIsAnError(t *testing.T) {
	if _, err := ResolveSkills(SkillScope("everything")); err == nil {
		t.Fatalf("ResolveSkills with an unknown scope returned no error — an unrecognised scope must fail loudly, never fall back to all")
	}
	if ScopeCore.Valid() != true || ScopeAll.Valid() != true || SkillScope("everything").Valid() {
		t.Fatalf("SkillScope.Valid disagrees with the known scope set")
	}
}

// TestSkillIsCore_AgreesWithDeployIsSkillCore proves the embedded-FS-native
// frontmatter parse in SkillIsCore is the same predicate deploy.IsSkillCore
// applies to an on-disk path, for EVERY embedded skill. deploy.IsSkillCore
// cannot read an embed.FS (it takes a filesystem path), so the logic is
// duplicated — this test is what keeps the duplicate from drifting.
func TestSkillIsCore_AgreesWithDeployIsSkillCore(t *testing.T) {
	srcDir := repoRootSkillsDir(t)

	names, err := SkillNames()
	if err != nil {
		t.Fatalf("SkillNames: %v", err)
	}
	if len(names) == 0 {
		t.Fatalf("SkillNames returned nothing — refusing a vacuous test")
	}

	agreedCore := 0
	for _, name := range names {
		got, err := SkillIsCore(name)
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

// pluginCoreSkillsDir returns repo-root plugins/core/skills — the marketplace
// core plugin's skill set, generated by skillgen.
func pluginCoreSkillsDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "plugins", "core", "skills"))
	if err != nil {
		t.Fatalf("resolve plugins/core/skills dir: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("plugins/core/skills not found at %q: %v", dir, err)
	}
	return dir
}

// TestCoreSet_MatchesMarketplaceCorePlugin guards a latent SPLIT-BRAIN.
//
// Two independent parsers decide what `core: true` means, and they do not
// agree in general:
//
//   - tools/skillgen/main.go:137 — a Core bool field with a yaml:"core"
//     struct tag, i.e. a real YAML unmarshal. This is what generates
//     plugins/core/, which is what the marketplace's core plugin ships.
//   - deploy.IsSkillCore (deploy.go:271-273) — and SkillIsCore here, its
//     embedded twin — an EXACT string match on the trimmed line `core: true`.
//
// So `core:  true` (two spaces), `core: True`, or `core: "true"` would be core
// to skillgen and to the marketplace, and NOT core to the CLI. The marketplace
// core set and the CLI's scope=core install would diverge silently, with no
// error raised anywhere. Every SKILL.md today satisfies both parsers, which is
// precisely why this is latent rather than already broken — and why the check
// belongs in CI rather than in a comment.
//
// Both sides are derived at test time; neither the list nor its size is
// hardcoded. Failure reports the symmetric difference.
func TestCoreSet_MatchesMarketplaceCorePlugin(t *testing.T) {
	names, err := SkillNames()
	if err != nil {
		t.Fatalf("SkillNames: %v", err)
	}
	cliCore := map[string]bool{}
	for _, name := range names {
		isCore, err := SkillIsCore(name)
		if err != nil {
			t.Fatalf("SkillIsCore(%q): %v", name, err)
		}
		if isCore {
			cliCore[name] = true
		}
	}

	entries, err := os.ReadDir(pluginCoreSkillsDir(t))
	if err != nil {
		t.Fatalf("read plugins/core/skills: %v", err)
	}
	pluginCore := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			pluginCore[e.Name()] = true
		}
	}

	if len(cliCore) == 0 || len(pluginCore) == 0 {
		t.Fatalf("derived %d CLI-core and %d plugin-core skills — refusing a vacuous test", len(cliCore), len(pluginCore))
	}

	var onlyCLI, onlyPlugin []string
	for name := range cliCore {
		if !pluginCore[name] {
			onlyCLI = append(onlyCLI, name)
		}
	}
	for name := range pluginCore {
		if !cliCore[name] {
			onlyPlugin = append(onlyPlugin, name)
		}
	}
	sort.Strings(onlyCLI)
	sort.Strings(onlyPlugin)

	if len(onlyCLI) > 0 || len(onlyPlugin) > 0 {
		t.Fatalf("core-set split-brain between the CLI's `core: true` string match and skillgen's YAML unmarshal:\n"+
			"  core to the CLI but absent from plugins/core/skills: %v\n"+
			"  in plugins/core/skills but not core to the CLI:      %v\n"+
			"Check the SKILL.md frontmatter of the listed skills for a form only one parser accepts "+
			"(`core:  true`, `core: True`, `core: \"true\"`), or re-run skillgen if plugins/ is stale.",
			onlyCLI, onlyPlugin)
	}
	t.Logf("CLI core set and plugins/core/skills agree on %d skills", len(cliCore))
}

func TestSelection_EnabledSkillsFeedsDeployOpts(t *testing.T) {
	c, ok := ComponentByID("claude-skills")
	if !ok {
		t.Fatalf("ComponentByID(claude-skills) not found")
	}
	sel, err := c.Select(ScopeCore)
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
		if _, err := fs.Stat(FS, path.Join("skills", name)); err != nil {
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

func TestDefaultSelections(t *testing.T) {
	sels, err := DefaultSelections()
	if err != nil {
		t.Fatalf("DefaultSelections: %v", err)
	}
	if len(sels) != len(Components()) {
		t.Fatalf("DefaultSelections returned %d selections, want all %d components on by default", len(sels), len(Components()))
	}

	byID := map[string]Selection{}
	for _, s := range sels {
		byID[s.ID] = s
	}

	cli, ok := byID["cli"]
	if !ok {
		t.Fatalf("cli missing from default selections")
	}
	if cli.Scope != "" || len(cli.Skills) != 0 {
		t.Errorf("cli selection carries scope/skills it should not: %+v", cli)
	}

	skills, ok := byID["claude-skills"]
	if !ok {
		t.Fatalf("claude-skills missing from default selections")
	}
	if skills.Scope != ScopeCore {
		t.Errorf("claude-skills default scope = %q, want %q", skills.Scope, ScopeCore)
	}
	wantCore, err := ResolveSkills(ScopeCore)
	if err != nil {
		t.Fatalf("ResolveSkills(core): %v", err)
	}
	if !reflect.DeepEqual(skills.Skills, wantCore) {
		t.Errorf("claude-skills default selection resolved %v, want %v", skills.Skills, wantCore)
	}
	// The resolved list must be stored alongside the enum so a later,
	// finer-grained scope vocabulary stays readable by this release.
	if len(skills.Skills) == 0 {
		t.Errorf("claude-skills selection stored no resolved skill list")
	}

	skillsComponent, found := ComponentByID("claude-skills")
	if !found {
		t.Fatalf("ComponentByID(claude-skills) not found")
	}
	selAll, err := skillsComponent.Select(ScopeAll)
	if err != nil {
		t.Fatalf("Select(all): %v", err)
	}
	if len(selAll.Skills) <= len(skills.Skills) {
		t.Errorf("scope=all resolved %d skills, scope=core resolved %d — all must be strictly larger", len(selAll.Skills), len(skills.Skills))
	}
}
