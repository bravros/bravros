package cmd

// setup.go — `bravros setup`, the component wizard (P-0015 Phase 3).
//
// WHAT THIS VERB IS FOR. `bravros deploy` copies a *source checkout* into
// ~/.claude; `bravros selfupdate` refreshes from a fetched payload. Neither can
// run on a machine that has only the binary, and neither ever asks the operator
// what they want. `setup` installs from the payload EMBEDDED in this binary
// (cli/internal/payload) and gives the choice its first GLOBAL home:
// config.EnabledSkills() resolves .bravros.yml from CWD
// (cli/internal/config/preserve.go:10-13), so "I only want core skills" is
// today a per-project answer that depends on which repo the operator happened
// to be standing in when the SessionStart hook fired. state.json (written
// below) is the machine-wide answer, and the resolved skill list it stores is
// exactly the shape deploy.DeployOpts.EnabledSkills consumes — this wizard
// drives the EXISTING per-skill selection path, it does not invent a parallel
// one.
//
// ONE SELECTION AXIS: components (D11). There is no plugin-category picker;
// granularity below "all skills" is the claude-skills component's scope
// (core|all, default core). See cli/internal/payload/manifest.go for why.
//
// NON-DESTRUCTIVE BY CONTRACT, which is why this does NOT call deploy.Deploy.
// deploy's per-skill step is an atomic wipe-and-recopy: a skill whose SHA
// differs is deleted and rewritten wholesale, so a hand-edited SKILL.md is
// gone without a word. `setup` instead compares byte-for-byte and writes a
// differing file as <name>.new, reporting it. Every other deploy contract that
// matters here is preserved explicitly: the plugin-managed guard
// (deploy.IsPluginManaged) runs over every planned path, and pruning stays
// scoped to skills+templates (setupPruneSubtrees, from P-0014 Phase 4).

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/bravros/bravros/cli/internal/config"
	"github.com/bravros/bravros/cli/internal/deploy"
	"github.com/bravros/bravros/cli/internal/managed"
	"github.com/bravros/bravros/cli/internal/payload"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	setupAll        bool
	setupComponents string
	setupSkills     string
	setupYes        bool
)

// setupPruneSubtrees is the ONLY set of ~/.claude subtrees `setup` may remove
// anything from — the scoping P-0014 Phase 4 established and this plan is
// required to preserve. deploy.DeployOpts.PruneSubtrees carries the same two
// names for the selfupdate lane (cmd/selfupdate.go:269); ~/.claude/hooks and
// ~/.claude/agents are content bravros does not own at the target and must stay
// untouched no matter what is selected.
//
// Setup's pruning is narrower still than deploy's: it removes ONLY a skill that
// (a) a previous `setup` run recorded as installed, (b) the current selection
// drops, and (c) is still byte-identical to the embedded payload — i.e.
// provably ours and provably unmodified. A skill the user edited, or one they
// added by hand, is never removed.
var setupPruneSubtrees = []string{"skills", "templates"}

// setupStateSchema versions the on-disk state.json shape. Phase 6 reads this
// file after a binary swap; bump on any incompatible change.
const setupStateSchema = 1

// setupAllowPluginManagedEnv lets an operator who knowingly wants both
// delivery mechanisms proceed past the plugin-managed refusal (D7). It is an
// env var rather than a flag on purpose: the safe path must be the short one.
const setupAllowPluginManagedEnv = "BRAVROS_ALLOW_PLUGIN_MANAGED"

// setupComponentsEnv is the non-interactive component list honored when
// --components is absent.
const setupComponentsEnv = "BRAVROS_COMPONENTS"

// setupInstallMethodEnv lets install.sh / install.ps1 declare how the binary
// arrived; Phase 6 refuses to self-replace a brew/scoop-managed install.
const setupInstallMethodEnv = "BRAVROS_INSTALL_METHOD"

// ─── state.json ────────────────────────────────────────────────────────────

// setupStateComponent is one component's recorded install. payload.Selection
// carries id + scope + the RESOLVED skill list (stored rather than recomputed
// so a later release that moves the core/all split can still tell exactly what
// this machine has); Target is the ~/.claude-relative destination.
type setupStateComponent struct {
	payload.Selection
	Target string `json:"target"`
}

// setupState is the machine-wide record of what `setup` installed. It carries
// no timestamp deliberately: two identical runs must produce byte-identical
// state, which is what makes "the second run reports no changes" observable
// rather than asserted.
type setupState struct {
	Schema         int                   `json:"schema"`
	BravrosVersion string                `json:"bravros_version"`
	InstallMethod  string                `json:"install_method"`
	ClaudeRoot     string                `json:"claude_root"`
	SkillsScope    payload.SkillScope    `json:"skills_scope,omitempty"`
	Components     []setupStateComponent `json:"components"`
}

// setupStatePath is where the state record lives: <config dir>/state/setup.json.
// state/ is on deploy.go's never-prune allowlist (deploy.go:57-65), so neither
// `bravros deploy` nor `selfupdate` can ever remove it.
func setupStatePath(root string) string {
	return filepath.Join(root, "state", "setup.json")
}

func readSetupState(root string) (*setupState, error) {
	data, err := os.ReadFile(setupStatePath(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", setupStatePath(root), err)
	}
	var st setupState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON (%v) — move it aside and re-run `bravros setup`", setupStatePath(root), err)
	}
	return &st, nil
}

// detectInstallMethod records how this binary got here so Phase 6's updater can
// refuse to self-replace something a package manager owns.
func detectInstallMethod(execPath, root string) string {
	if v := strings.TrimSpace(os.Getenv(setupInstallMethodEnv)); v != "" {
		return v
	}
	p := filepath.ToSlash(execPath)
	switch {
	case strings.Contains(p, "/Cellar/"), strings.Contains(p, "/homebrew/"), strings.Contains(p, "/linuxbrew/"):
		return "brew"
	case strings.Contains(strings.ToLower(p), "/scoop/"):
		return "scoop"
	case execPath != "" && strings.HasPrefix(p, filepath.ToSlash(filepath.Join(root, "bin"))+"/"):
		return "installer"
	case execPath == "":
		return "unknown"
	default:
		return "source"
	}
}

// ─── selection ─────────────────────────────────────────────────────────────

// setupResolveScope picks the skills scope. Precedence: --skills, then --all
// (which means "everything", scope included — the acceptance smoke depends on
// `setup --all --yes` producing a full ~/.claude/skills listing), then a
// previous run's recorded scope, then the manifest default (core).
//
// NOTE the pre-v2 migration case is NOT here: an install with no state.json at
// all defaults to `all` only in Phase 6's refresh path (D11 migration), because
// silently deleting 17 skills a user already had is worse than installing more
// than they asked for. `core` is the default for a FRESH wizard run, which is
// what this function serves.
func setupResolveScope(flagScope string, all bool, prev *setupState, def payload.SkillScope) (payload.SkillScope, error) {
	if flagScope != "" {
		s := payload.SkillScope(flagScope)
		if !s.Valid() {
			return "", fmt.Errorf("unknown --skills value %q (valid: %s, %s)", flagScope, payload.ScopeCore, payload.ScopeAll)
		}
		return s, nil
	}
	if all {
		return payload.ScopeAll, nil
	}
	if prev != nil && prev.SkillsScope.Valid() {
		return prev.SkillsScope, nil
	}
	if def == "" {
		return payload.ScopeCore, nil
	}
	return def, nil
}

// setupResolveIDs decides WHICH components to install, without prompting.
// Precedence: --all, --components, $BRAVROS_COMPONENTS, a previous run's
// record, then nil meaning "no explicit answer — ask, or fall back to the
// manifest defaults".
func setupResolveIDs(all bool, flagList, envList string, prev *setupState) ([]string, error) {
	if all {
		var ids []string
		for _, c := range payload.Components() {
			ids = append(ids, c.ID)
		}
		return ids, nil
	}
	raw := flagList
	if raw == "" {
		raw = envList
	}
	if raw != "" {
		var ids []string
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, ok := payload.ComponentByID(part); !ok {
				return nil, fmt.Errorf("unknown component %q (known: %s)", part, strings.Join(setupComponentIDs(), ", "))
			}
			ids = append(ids, part)
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("empty component list")
		}
		return ids, nil
	}
	if prev != nil && len(prev.Components) > 0 {
		var ids []string
		for _, c := range prev.Components {
			if _, ok := payload.ComponentByID(c.ID); ok {
				ids = append(ids, c.ID)
			}
		}
		if len(ids) > 0 {
			return ids, nil
		}
	}
	return nil, nil
}

// setupDefaultIDs lists the components the manifest starts checked — what the
// wizard opens with and what a bare `--yes` installs.
func setupDefaultIDs() []string {
	var ids []string
	for _, c := range payload.Components() {
		if c.Default || c.Required {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

func setupComponentIDs() []string {
	var ids []string
	for _, c := range payload.Components() {
		ids = append(ids, c.ID)
	}
	return ids
}

// setupSelections turns component ids + a scope into resolved Selections.
// Required components are always included, even when the caller forgot them.
func setupSelections(ids []string, scope payload.SkillScope) ([]payload.Selection, error) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []payload.Selection
	for _, c := range payload.Components() {
		if !want[c.ID] && !c.Required {
			continue
		}
		sel, err := c.Select(scope)
		if err != nil {
			return nil, err
		}
		out = append(out, sel)
	}
	return out, nil
}

// ─── plugin-managed detection (D7) ─────────────────────────────────────────

// setupDetectPluginManaged reports the plugin-managed directories under root
// that already carry a bravros install. Claude Code's marketplace delivers the
// same skills from the public repo; a second writer would fight it (P-0003).
// bravros must never write into those trees, and under D7 setup DETECTS and
// warns rather than double-writing.
func setupDetectPluginManaged(root string) []string {
	var found []string
	for _, dir := range []string{"plugins", ".claude-plugin", "extensions"} {
		abs := filepath.Join(root, dir)
		if _, err := os.Stat(abs); err != nil {
			continue
		}
		if setupTreeMentionsBravros(abs, 0) {
			found = append(found, dir)
		}
	}
	return found
}

// setupTreeMentionsBravros looks (max 3 levels deep — a marketplace checkout
// nests owner/repo/plugin) for an entry whose name names bravros.
func setupTreeMentionsBravros(dir string, depth int) bool {
	if depth > 3 {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Name()), "bravros") {
			return true
		}
		if e.IsDir() && setupTreeMentionsBravros(filepath.Join(dir, e.Name()), depth+1) {
			return true
		}
	}
	return false
}

// ─── plan ──────────────────────────────────────────────────────────────────

type setupAction string

const (
	setupActionCreate    setupAction = "create"    // no file at the target yet
	setupActionUnchanged setupAction = "unchanged" // byte-identical — nothing to do
	setupActionConflict  setupAction = "conflict"  // differs → written as <name>.new
	setupActionMerge     setupAction = "merge"     // managed JSON deep-merge
	setupActionRecord    setupAction = "record"    // nothing written (the binary)
	setupActionPrune     setupAction = "prune"     // dropped from the selection, unmodified
)

// setupPlanItem is one planned filesystem outcome. src is the staged source
// file (empty for non-file actions); rel is ALWAYS root-relative slash form,
// which is exactly what deploy.IsPluginManaged consumes.
type setupPlanItem struct {
	Component string
	Rel       string
	Abs       string
	Src       string
	Action    setupAction
	Note      string
}

// setupPlan is the whole intended outcome, computed before a single byte is
// written — it is what the confirm screen shows and what the summary counts.
type setupPlan struct {
	Root          string
	Items         []setupPlanItem
	Selections    []payload.Selection
	SkippedIDs    []string
	Warnings      []string
	PluginManaged []string
}

func (p *setupPlan) count(a setupAction) int {
	n := 0
	for _, it := range p.Items {
		if it.Action == a {
			n++
		}
	}
	return n
}

// setupStage extracts the selected part of the embedded payload into a staging
// directory. Extraction goes through payload.Extract rather than a hand-rolled
// walk so the executable bit — which go:embed itself discards, and which
// templates/.githooks/commit-msg depends on — is restored from the payload's
// own manifest in exactly one place.
func setupStage(sel payload.Selection, c payload.Component, staging string) (string, error) {
	sub, ok := c.EmbedSubtree()
	if !ok {
		return "", nil
	}
	dst := filepath.Join(staging, sub)

	// claude-skills stages only the resolved list. An empty list keeps
	// deploy's backward-compatible meaning ("everything"), never "nothing".
	if sub == "skills" && len(sel.Skills) > 0 {
		for _, name := range sel.Skills {
			if err := payload.Extract(path.Join("skills", name), filepath.Join(dst, name)); err != nil {
				return "", err
			}
		}
		return dst, nil
	}
	if err := payload.Extract(sub, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// setupBuildPlan computes every outcome without touching the target tree.
func setupBuildPlan(root, staging string, sels []payload.Selection, prev *setupState) (*setupPlan, error) {
	plan := &setupPlan{Root: root, Selections: sels}

	plan.PluginManaged = setupDetectPluginManaged(root)
	allowPluginManaged := os.Getenv(setupAllowPluginManagedEnv) == "1"

	for _, sel := range sels {
		c, ok := payload.ComponentByID(sel.ID)
		if !ok {
			return nil, fmt.Errorf("unknown component %q in selection", sel.ID)
		}

		// Never write into a host's plugin tree — checked against the
		// component's declared target before anything is staged.
		if deploy.IsPluginManaged(c.TargetRel()) {
			return nil, fmt.Errorf("component %q targets plugin-managed path %q — refusing to write", c.ID, c.TargetRel())
		}

		if len(plan.PluginManaged) > 0 && c.Kind == payload.KindEmbeddedTree && !allowPluginManaged {
			plan.SkippedIDs = append(plan.SkippedIDs, c.ID)
			plan.Warnings = append(plan.Warnings, fmt.Sprintf(
				"%s: a plugin-managed Claude Code install was found (%s) — skipping to avoid a second writer. Set %s=1 to install anyway.",
				c.ID, strings.Join(plan.PluginManaged, ", "), setupAllowPluginManagedEnv))
			continue
		}

		switch c.Kind {
		case payload.KindBinary:
			exe, _ := os.Executable()
			plan.Items = append(plan.Items, setupPlanItem{
				Component: c.ID,
				Rel:       c.TargetRel(),
				Abs:       c.TargetPathUnder(root),
				Action:    setupActionRecord,
				Note:      "installed by install.sh / install.ps1: " + exe,
			})

		case payload.KindEmbeddedTree:
			staged, err := setupStage(sel, c, staging)
			if err != nil {
				return nil, err
			}
			sub, _ := c.EmbedSubtree()
			items, err := setupPlanTree(root, staged, sub, c.ID)
			if err != nil {
				return nil, err
			}
			plan.Items = append(plan.Items, items...)

		case payload.KindMergedSettings:
			plan.Items = append(plan.Items, setupPlanItem{
				Component: c.ID,
				Rel:       c.TargetRel(),
				Abs:       c.TargetPathUnder(root),
				Action:    setupActionMerge,
				Note:      "entry-level deep merge; user-owned keys preserved",
			})
		}
	}

	pruned, err := setupPlanPrune(root, sels, prev)
	if err != nil {
		return nil, err
	}
	plan.Items = append(plan.Items, pruned...)

	return plan, nil
}

// setupPlanTree diffs one staged subtree against the target.
func setupPlanTree(root, staged, sub, componentID string) ([]setupPlanItem, error) {
	var items []setupPlanItem
	err := filepath.WalkDir(staged, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		relInSub, err := filepath.Rel(staged, p)
		if err != nil {
			return err
		}
		rel := path.Join(sub, filepath.ToSlash(relInSub))
		if deploy.IsPluginManaged(rel) {
			return fmt.Errorf("internal invariant violated: %q is plugin-managed — refusing to write", rel)
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))

		action, err := setupCompare(p, abs)
		if err != nil {
			return err
		}
		it := setupPlanItem{Component: componentID, Rel: rel, Abs: abs, Src: p, Action: action}
		if action == setupActionConflict {
			it.Note = "existing file differs — will be preserved; new content goes to " + rel + ".new"
		}
		items = append(items, it)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

// setupCompare decides one file's fate: absent → create, identical →
// unchanged, different → conflict (never an overwrite).
func setupCompare(src, dst string) (setupAction, error) {
	want, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("read staged %s: %w", src, err)
	}
	have, err := os.ReadFile(dst)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return setupActionCreate, nil
	case err != nil:
		return "", fmt.Errorf("read %s: %w", dst, err)
	case bytes.Equal(want, have):
		return setupActionUnchanged, nil
	default:
		return setupActionConflict, nil
	}
}

// setupPlanPrune finds skills a previous run installed that this run drops.
//
// Three conditions, all required: the skill was recorded by a previous `setup`
// (so it is provably ours), it is absent from the new selection, and its
// on-disk content still matches the embedded payload exactly (so it is
// provably unmodified). Anything a user added by hand or edited fails at least
// one of them and survives. Pruning is confined to setupPruneSubtrees.
func setupPlanPrune(root string, sels []payload.Selection, prev *setupState) ([]setupPlanItem, error) {
	if prev == nil {
		return nil, nil
	}
	keep := map[string]bool{}
	installing := false
	for _, sel := range sels {
		if sel.ID != "claude-skills" {
			continue
		}
		installing = true
		for _, name := range sel.Skills {
			keep[name] = true
		}
	}
	if !installing {
		// The skills component is not part of this run; leave the tree alone.
		return nil, nil
	}

	var prevSkills []string
	for _, c := range prev.Components {
		if c.ID == "claude-skills" {
			prevSkills = c.Skills
		}
	}

	var items []setupPlanItem
	for _, name := range prevSkills {
		if keep[name] {
			continue
		}
		rel := path.Join("skills", name)
		if !setupSubtreeIsPrunable(rel) || deploy.IsPluginManaged(rel) {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(abs); err != nil {
			continue
		}
		matches, err := setupSkillMatchesEmbed(abs, name)
		if err != nil {
			return nil, err
		}
		if !matches {
			items = append(items, setupPlanItem{
				Component: "claude-skills",
				Rel:       rel,
				Abs:       abs,
				Action:    setupActionUnchanged,
				Note:      "dropped from the selection but modified on disk — left in place",
			})
			continue
		}
		items = append(items, setupPlanItem{
			Component: "claude-skills",
			Rel:       rel,
			Abs:       abs,
			Action:    setupActionPrune,
			Note:      "dropped from the selection, unmodified — will be removed",
		})
	}
	return items, nil
}

// setupSubtreeIsPrunable enforces the P-0014 Phase 4 scoping on every removal.
func setupSubtreeIsPrunable(rel string) bool {
	first := strings.SplitN(filepath.ToSlash(path.Clean(rel)), "/", 2)[0]
	for _, sub := range setupPruneSubtrees {
		if first == sub {
			return true
		}
	}
	return false
}

// setupSkillMatchesEmbed reports whether the installed skill at dir is
// byte-identical to the embedded one — same file set, same contents.
func setupSkillMatchesEmbed(dir, name string) (bool, error) {
	embedRoot := path.Join("skills", name)
	seen := map[string]bool{}

	err := fs.WalkDir(payload.FS, embedRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(embedRoot, p)
		if err != nil {
			return err
		}
		seen[filepath.ToSlash(rel)] = true
		want, err := fs.ReadFile(payload.FS, p)
		if err != nil {
			return err
		}
		have, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil || !bytes.Equal(want, have) {
			return errSetupSkillDiffers
		}
		return nil
	})
	if errors.Is(err, errSetupSkillDiffers) {
		return false, nil
	}
	if err != nil {
		// The skill is not in the embed at all (user-added) → not ours.
		return false, nil
	}

	// Any extra file on disk means the operator added something.
	extra := false
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // unreadable entries are not evidence of ownership
		}
		rel, relErr := filepath.Rel(dir, p)
		if relErr != nil {
			return nil
		}
		if !seen[filepath.ToSlash(rel)] {
			extra = true
			return errSetupSkillDiffers
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errSetupSkillDiffers) {
		return false, walkErr
	}
	return !extra, nil
}

var errSetupSkillDiffers = errors.New("skill differs from embedded payload")

// ─── apply ─────────────────────────────────────────────────────────────────

type setupApplyResult struct {
	Created   int
	Unchanged int
	Conflicts []string
	Pruned    []string
	Settings  string
	State     string
}

func setupApply(plan *setupPlan) (*setupApplyResult, error) {
	res := &setupApplyResult{}

	for _, it := range plan.Items {
		switch it.Action {
		case setupActionCreate, setupActionConflict:
			target := it.Abs
			if it.Action == setupActionConflict {
				// NEVER overwrite: the operator's file stays exactly where it
				// is and the payload's version lands beside it. The .new file
				// itself is a regenerable artifact, so re-running replaces it.
				target += ".new"
			}
			if err := setupCopyFile(it.Src, target); err != nil {
				return nil, err
			}
			if it.Action == setupActionCreate {
				res.Created++
			} else {
				res.Conflicts = append(res.Conflicts, it.Rel)
			}

		case setupActionUnchanged:
			res.Unchanged++

		case setupActionPrune:
			if !setupSubtreeIsPrunable(it.Rel) || deploy.IsPluginManaged(it.Rel) {
				return nil, fmt.Errorf("internal invariant violated: refusing to remove %q", it.Rel)
			}
			if err := os.RemoveAll(it.Abs); err != nil {
				return nil, fmt.Errorf("prune %s: %w", it.Abs, err)
			}
			res.Pruned = append(res.Pruned, it.Rel)

		case setupActionMerge:
			sync, err := managed.SyncClaudeSettings(it.Abs)
			if err != nil {
				return nil, fmt.Errorf("settings.json: %w", err)
			}
			switch {
			case sync.Created:
				res.Settings = "created"
				res.Created++
			case sync.Changed:
				res.Settings = "merged"
				res.Created++
			default:
				res.Settings = "unchanged"
				res.Unchanged++
			}
			if sync.StatusLineSkipped {
				plan.Warnings = append(plan.Warnings, "settings.json: your own \"statusLine\" was left untouched")
			}

		case setupActionRecord:
			// Nothing to write — the installer script placed the binary.
		}
	}
	return res, nil
}

// setupCopyFile writes src to dst, preserving src's mode (payload.Extract has
// already restored the executable bit from the payload manifest).
func setupCopyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, data, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}

// setupWriteState records the install. Returns "written" or "unchanged" so a
// re-run can report honestly — exit codes prove nothing here.
func setupWriteState(root string, plan *setupPlan, scope payload.SkillScope) (string, string, error) {
	// EvalSymlinks before detection: Homebrew on Intel macOS installs the binary
	// at /usr/local/bin/bravros as a SYMLINK into ../Cellar/. The raw path matches
	// none of detectInstallMethod's brew patterns, so an unresolved path records a
	// brew install as "source" — and `bravros update` would then self-replace a
	// Homebrew-managed binary instead of refusing. Apple Silicon happens to work
	// unresolved (/opt/homebrew/bin/... already contains "/homebrew/"), which is
	// exactly why this is easy to miss. Resolution failure falls back to the raw
	// path rather than losing detection entirely.
	exe, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != "" {
		exe = resolved
	}
	st := setupState{
		Schema:         setupStateSchema,
		BravrosVersion: strings.TrimPrefix(Version, "v"),
		InstallMethod:  detectInstallMethod(exe, root),
		ClaudeRoot:     root,
		SkillsScope:    scope,
	}
	skipped := map[string]bool{}
	for _, id := range plan.SkippedIDs {
		skipped[id] = true
	}
	for _, sel := range plan.Selections {
		if skipped[sel.ID] {
			continue
		}
		c, ok := payload.ComponentByID(sel.ID)
		if !ok {
			continue
		}
		st.Components = append(st.Components, setupStateComponent{Selection: sel, Target: c.TargetRel()})
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return "", "", err
	}
	data = append(data, '\n')

	p := setupStatePath(root)
	if existing, readErr := os.ReadFile(p); readErr == nil && bytes.Equal(existing, data) {
		return p, "unchanged", nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", "", fmt.Errorf("mkdir %s: %w", filepath.Dir(p), err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return "", "", fmt.Errorf("write %s: %w", p, err)
	}
	return p, "written", nil
}

// ─── presentation ──────────────────────────────────────────────────────────

var (
	setupTitleStyle = lipgloss.NewStyle().Bold(true)
	setupDimStyle   = lipgloss.NewStyle().Faint(true)
)

// setupRenderPlan names every destination path the run will touch — component
// targets, each selected skill directory, and every conflict individually.
func setupRenderPlan(w io.Writer, plan *setupPlan, scope payload.SkillScope) {
	fmt.Fprintln(w, setupTitleStyle.Render("bravros setup")+" → "+plan.Root)
	fmt.Fprintln(w)

	byComponent := map[string][]setupPlanItem{}
	for _, it := range plan.Items {
		byComponent[it.Component] = append(byComponent[it.Component], it)
	}
	skipped := map[string]bool{}
	for _, id := range plan.SkippedIDs {
		skipped[id] = true
	}

	for _, sel := range plan.Selections {
		c, ok := payload.ComponentByID(sel.ID)
		if !ok {
			continue
		}
		if skipped[sel.ID] {
			fmt.Fprintf(w, "  %-18s %s\n", c.ID, setupDimStyle.Render("skipped — see the warning below"))
			continue
		}
		items := byComponent[sel.ID]
		head := fmt.Sprintf("  %-18s %s", c.ID, c.TargetPathUnder(plan.Root))
		if c.Scoped {
			head += fmt.Sprintf("  (scope: %s, %d skill(s))", sel.Scope, len(sel.Skills))
		}
		fmt.Fprintln(w, head)

		switch c.Kind {
		case payload.KindBinary:
			for _, it := range items {
				fmt.Fprintln(w, setupDimStyle.Render("      "+it.Note))
			}
		case payload.KindMergedSettings:
			for _, it := range items {
				fmt.Fprintln(w, setupDimStyle.Render("      "+it.Note))
			}
		case payload.KindEmbeddedTree:
			if sub, _ := c.EmbedSubtree(); sub == "skills" {
				for _, name := range sel.Skills {
					fmt.Fprintln(w, setupDimStyle.Render("      skills/"+name))
				}
			}
			create, unchanged := 0, 0
			for _, it := range items {
				switch it.Action {
				case setupActionCreate:
					create++
				case setupActionUnchanged:
					unchanged++
				}
			}
			fmt.Fprintln(w, setupDimStyle.Render(fmt.Sprintf("      %d file(s) to write, %d already up to date", create, unchanged)))
		}
	}

	if n := plan.count(setupActionConflict); n > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  %d existing file(s) differ and will NOT be overwritten:\n", n)
		for _, it := range plan.Items {
			if it.Action == setupActionConflict {
				fmt.Fprintf(w, "      %s  →  %s.new\n", it.Rel, it.Rel)
			}
		}
	}
	if n := plan.count(setupActionPrune); n > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  %d unmodified skill(s) dropped from the selection will be removed:\n", n)
		for _, it := range plan.Items {
			if it.Action == setupActionPrune {
				fmt.Fprintln(w, "      "+it.Rel)
			}
		}
	}
	for _, warn := range plan.Warnings {
		fmt.Fprintln(w, "  ⚠️  "+warn)
	}
	fmt.Fprintln(w)
}

// ─── interactive wizard ────────────────────────────────────────────────────

func setupIsInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	if fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	fo, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fo.Mode()&os.ModeCharDevice != 0
}

// setupRunWizard asks which components to install and at which skills scope,
// pre-checked from the manifest defaults (or from a previous run).
func setupRunWizard(preselect []string, scope payload.SkillScope) ([]string, payload.SkillScope, error) {
	pre := map[string]bool{}
	for _, id := range preselect {
		pre[id] = true
	}

	var opts []huh.Option[string]
	for _, c := range payload.Components() {
		if c.Required {
			// Required components are not offered: deselecting the binary
			// would leave nothing able to drive the rest.
			continue
		}
		selected := pre[c.ID]
		if len(preselect) == 0 {
			selected = c.Default
		}
		opts = append(opts, huh.NewOption(c.Label+" — "+c.Description, c.ID).Selected(selected))
	}

	// The MultiSelect's Value must start out holding exactly the pre-checked
	// ids, or huh renders the checkmarks and returns a different answer.
	chosen := []string{}
	for _, c := range payload.Components() {
		if c.Required {
			continue
		}
		sel := pre[c.ID]
		if len(preselect) == 0 {
			sel = c.Default
		}
		if sel {
			chosen = append(chosen, c.ID)
		}
	}

	scopeChoice := string(scope)
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Which components should bravros install?").
				Description("Space toggles, enter confirms. The bravros binary itself is always installed.").
				Options(opts...).
				Value(&chosen),
		),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("How many Claude skills?").
				Description("core = the SDLC essentials; all = every skill in the payload. An always-on skill list has a context budget.").
				Options(
					huh.NewOption("core — SDLC essentials only (recommended)", string(payload.ScopeCore)),
					huh.NewOption("all — every skill in the payload", string(payload.ScopeAll)),
				).
				Value(&scopeChoice),
		).WithHideFunc(func() bool {
			for _, id := range chosen {
				if id == "claude-skills" {
					return false
				}
			}
			return true
		}),
	)
	if err := form.Run(); err != nil {
		return nil, "", err
	}

	resolved := payload.SkillScope(scopeChoice)
	if !resolved.Valid() {
		resolved = payload.ScopeCore
	}
	return chosen, resolved, nil
}

func setupConfirm() (bool, error) {
	ok := true
	err := huh.NewConfirm().
		Title("Write these files?").
		Description("Existing files that differ are never overwritten — they are preserved and the new content lands as <name>.new.").
		Affirmative("Install").
		Negative("Cancel").
		Value(&ok).
		Run()
	return ok, err
}

// ─── command ───────────────────────────────────────────────────────────────

var setupCmd = &cobra.Command{
	// NOTE: `worktree setup` and `worktree setup-full` already exist
	// (cmd/worktree.go:30,133). They are SUBcommands of `worktree`, so this
	// top-level verb cannot shadow them in dispatch, help text, or shell
	// completion — `bravros setup` and `bravros worktree setup` resolve to
	// different commands, and TestSetupDoesNotCollideWithWorktreeSetup pins it.
	Use:   "setup",
	Short: "Install bravros components from the embedded payload",
	Long: `Choose which bravros components to install into ~/.claude and write them from
the payload embedded in this binary — no network, no source checkout required.

Components (one selection axis; there is no plugin-category picker):
  cli                the bravros binary itself (always, placed by the installer)
  claude-skills      ~/.claude/skills, at scope core (default) or all
  claude-templates   ~/.claude/templates (git hooks, project templates)
  claude-settings    the managed settings.json block, deep-merged

Non-destructive by contract: a file that already exists and differs is never
overwritten — it stays put and the payload's version is written as <name>.new
and reported. settings.json is deep-merged entry-by-entry, never replaced.

Re-running is idempotent: a second run with the same selection reports no
changes. The choice is recorded in <config dir>/state/setup.json, which is the
first machine-wide home for it (.bravros.yml's skills.enabled is resolved from
the current working directory, so it is per-project by construction).

Interactive by default when stdin is a TTY. Non-interactive:
  bravros setup --all --yes
  bravros setup --components=claude-skills,claude-settings --skills=all --yes
  BRAVROS_COMPONENTS=claude-skills bravros setup --yes`,
	Args: cobra.NoArgs,
	RunE: runSetup,
}

func runSetup(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	root := config.ConfigDir()

	prev, err := readSetupState(root)
	if err != nil {
		return err
	}

	ids, err := setupResolveIDs(setupAll, setupComponents, os.Getenv(setupComponentsEnv), prev)
	if err != nil {
		return err
	}
	scope, err := setupResolveScope(setupSkills, setupAll, prev, payload.ScopeCore)
	if err != nil {
		return err
	}

	interactive := !setupYes && setupIsInteractive()
	switch {
	case interactive:
		preselect := ids
		if preselect == nil {
			for _, c := range payload.Components() {
				if c.Default && !c.Required {
					preselect = append(preselect, c.ID)
				}
			}
		}
		chosen, chosenScope, wizErr := setupRunWizard(preselect, scope)
		if errors.Is(wizErr, huh.ErrUserAborted) {
			fmt.Fprintln(out, "Cancelled — nothing was written.")
			return nil
		}
		if wizErr != nil {
			return wizErr
		}
		ids, scope = chosen, chosenScope
	case ids == nil && setupYes:
		// `--yes` alone means "install the manifest defaults" — the contract
		// payload.DefaultSelections documents ("the state the wizard opens
		// with, and the state --yes without an explicit component list
		// installs"). Defaults are default-on components at scope core, so
		// this can never install more than the operator implicitly asked for.
		ids = setupDefaultIDs()
	case ids == nil:
		// No TTY, no flags, no previous run: refuse to guess. Installing
		// something unasked into ~/.claude from a non-interactive context is
		// exactly the surprise this verb exists to remove.
		return fmt.Errorf("no components selected and stdin is not a terminal — re-run with --yes (manifest defaults), --all --yes, --components=<ids>, or set %s (known components: %s)",
			setupComponentsEnv, strings.Join(setupComponentIDs(), ", "))
	}

	sels, err := setupSelections(ids, scope)
	if err != nil {
		return err
	}

	staging, err := os.MkdirTemp("", "bravros-setup-")
	if err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	defer os.RemoveAll(staging)

	plan, err := setupBuildPlan(root, staging, sels, prev)
	if err != nil {
		return err
	}

	setupRenderPlan(out, plan, scope)

	if interactive {
		ok, confirmErr := setupConfirm()
		if errors.Is(confirmErr, huh.ErrUserAborted) || (confirmErr == nil && !ok) {
			fmt.Fprintln(out, "Cancelled — nothing was written.")
			return nil
		}
		if confirmErr != nil {
			return confirmErr
		}
	}

	res, err := setupApply(plan)
	if err != nil {
		return err
	}

	statePath, stateStatus, err := setupWriteState(root, plan, scope)
	if err != nil {
		return err
	}
	res.State = stateStatus

	changed := res.Created + len(res.Conflicts) + len(res.Pruned)
	if changed == 0 && stateStatus == "unchanged" {
		fmt.Fprintf(out, "✓ already up to date — no changes (%d file(s) verified)\n", res.Unchanged)
	} else {
		fmt.Fprintf(out, "✓ setup complete — %d written, %d unchanged, %d preserved as .new, %d pruned\n",
			res.Created, res.Unchanged, len(res.Conflicts), len(res.Pruned))
	}
	fmt.Fprintf(out, "  state: %s (%s)\n", statePath, stateStatus)
	if res.Settings != "" {
		fmt.Fprintf(out, "  settings.json: %s\n", res.Settings)
	}
	for _, c := range res.Conflicts {
		fmt.Fprintf(out, "  kept your %s — new version at %s.new\n", c, c)
	}
	for _, w := range plan.Warnings {
		fmt.Fprintln(out, "  ⚠️  "+w)
	}
	return nil
}

func init() {
	setupCmd.Flags().BoolVar(&setupAll, "all", false, "Install every component (implies --skills=all)")
	setupCmd.Flags().StringVar(&setupComponents, "components", "", "Comma-separated component ids to install (overrides "+setupComponentsEnv+")")
	setupCmd.Flags().StringVar(&setupSkills, "skills", "", "Skill scope for claude-skills: core (default) or all")
	setupCmd.Flags().BoolVar(&setupYes, "yes", false, "Non-interactive: skip the picker and the confirm screen")
	setupCmd.MarkFlagsMutuallyExclusive("all", "components")
	rootCmd.AddCommand(setupCmd)
}
