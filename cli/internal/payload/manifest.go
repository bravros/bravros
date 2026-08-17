package payload

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// The component manifest — what `bravros setup` (P-0015 Phase 3) may install
// into a user's home, and where each piece lands.
//
// ONE SELECTION AXIS: components, never plugin categories. `plugins/` is
// generated wholesale by skillgen from the same SKILL.md frontmatter that
// `skills/` carries (tools/skillgen/main.go:342-364) — the categories are a
// packaging *view* of these very files, not a second source of truth, and that
// view is demonstrably unstable (skillgen declares 6 categories,
// .claude-plugin/marketplace.json declares 5, plugins/ holds 34 skills against
// skills/'s 35). Unlike the embedded mirror it has no CI drift check, so it is
// not a surface to build a wizard on. Granularity below "all skills" is
// expressed instead by the claude-skills component's SCOPE (ScopeCore /
// ScopeAll), resolved at runtime from the `core: true` frontmatter already
// inside the embedded payload. ScopeCore is the default, which is what keeps
// P-0004's carried-forward requirement — the picker must not preselect
// everything — satisfied: core is 18 of 35 skills, and an always-on skill list
// has a context budget.
//
// The component list is deliberately short: only things something actually
// writes into a user's home. `cursor`, `copilot`, `aider` and `opencode` are
// NOT components (P-0015 Traps) — the first three appear in no Go source at
// all, and the per-host artifacts that do exist (CLAUDE.md, GEMINI.md,
// AGENTS.md, .cursorrules, marketplace.json, gemini-extension.json) are
// generated into the repo and consumed in place, never installed.

// Kind classifies HOW a component is installed. It exists so the
// "does this component have files to extract?" question is answered by the
// type system rather than by a comment: only KindEmbeddedTree names a subtree
// of the embedded FS, and the subtree is reachable solely through
// Component.EmbedSubtree, which reports ok=false for every other kind. A
// caller therefore cannot hand Extract the empty string by accident.
type Kind int

const (
	// KindBinary is the running bravros executable itself. The installer
	// script (install.sh / install.ps1) places it; setup only records it.
	// Nothing in the embedded FS corresponds to it.
	KindBinary Kind = iota + 1

	// KindEmbeddedTree is a file tree carried inside the binary by go:embed
	// and written out with Extract.
	KindEmbeddedTree

	// KindMergedSettings is install LOGIC, not files: the managed JSON
	// deep-merge in cli/internal/managed/. There is no settings/ directory
	// anywhere in this repo and `//go:embed all:settings` would not compile
	// (P-0015 Traps) — this kind is what stops a caller from looking for one.
	KindMergedSettings
)

func (k Kind) String() string {
	switch k {
	case KindBinary:
		return "binary"
	case KindEmbeddedTree:
		return "embedded-tree"
	case KindMergedSettings:
		return "merged-settings"
	default:
		return fmt.Sprintf("Kind(%d)", int(k))
	}
}

// SkillScope narrows which skills the claude-skills component installs.
//
// FORWARD COMPATIBILITY: this is a string enum, not a bool, and every consumer
// pairs it with the RESOLVED skill list (see Selection). A later release that
// wants finer granularity than core/all adds a scope value and keeps writing
// the same resolved list, so an older binary reading a newer state.json still
// knows exactly which skills are installed even when it does not recognise the
// scope name. Nothing here presumes the scope set stays at two.
type SkillScope string

const (
	// ScopeCore installs only skills whose SKILL.md frontmatter carries
	// `core: true` — the same predicate deploy.IsSkillCore applies, and the
	// same set the marketplace's core plugin ships.
	ScopeCore SkillScope = "core"

	// ScopeAll installs every embedded skill.
	ScopeAll SkillScope = "all"
)

// Valid reports whether s is a scope THIS build knows how to resolve.
func (s SkillScope) Valid() bool {
	return s == ScopeCore || s == ScopeAll
}

// Component is one installable unit of the toolkit.
//
// Target paths are always expressed relative to the per-user Claude root
// (ClaudeRoot: <home>/.claude on every platform, per D10 — Windows mirrors the
// POSIX layout under %USERPROFILE% and never uses %LOCALAPPDATA%). They are
// stored as path SEGMENTS and joined with filepath.Join at use time, so the
// separator is correct per GOOS without a single hand-written backslash.
type Component struct {
	// ID is the stable identifier used by --components, BRAVROS_COMPONENTS
	// and state.json. Never rename one without a migration.
	ID string

	// Label is the short human name shown in the wizard.
	Label string

	// Description is the one-line explanation shown under the label.
	Description string

	// Kind says how the component installs. See Kind.
	Kind Kind

	// Default is whether the component starts checked in the picker.
	Default bool

	// Required marks a component the user cannot deselect (the CLI binary:
	// without it nothing else is reachable). A Required component is always
	// Default.
	Required bool

	// Scoped marks a component whose selection carries a SkillScope.
	Scoped bool

	// DefaultScope is the scope preselected for a Scoped component; the zero
	// value ("") for every other component.
	DefaultScope SkillScope

	// embedSubtree is the top-level directory inside FS this component
	// extracts. Unexported on purpose: read it through EmbedSubtree, which
	// makes "this component has no subtree" an explicit, checkable outcome.
	embedSubtree string

	// targetRel is the target path, in segments, relative to ClaudeRoot.
	targetRel []string
}

// components is the canonical, ordered manifest. Order is wizard order.
var components = []Component{
	{
		ID:          "cli",
		Label:       "Bravros CLI",
		Description: "The bravros binary itself — required, everything else is driven by it.",
		Kind:        KindBinary,
		Default:     true,
		Required:    true,
		targetRel:   []string{"bin"},
	},
	{
		ID:           "claude-skills",
		Label:        "Claude skills",
		Description:  "Agent skills for Claude Code. Scope 'core' installs the SDLC essentials; 'all' installs every skill.",
		Kind:         KindEmbeddedTree,
		Default:      true,
		Scoped:       true,
		DefaultScope: ScopeCore,
		embedSubtree: "skills",
		targetRel:    []string{"skills"},
	},
	{
		ID:           "claude-templates",
		Label:        "Claude templates",
		Description:  "Git hooks and project templates used by bravros init and the commit-msg gate.",
		Kind:         KindEmbeddedTree,
		Default:      true,
		embedSubtree: "templates",
		targetRel:    []string{"templates"},
	},
	{
		ID:          "claude-settings",
		Label:       "Claude settings",
		Description: "Managed settings.json block (SessionStart hook and friends), deep-merged into any existing file — never overwritten.",
		Kind:        KindMergedSettings,
		Default:     true,
		targetRel:   []string{"settings.json"},
	},
}

// Components returns the manifest in wizard order. The slice is a copy: the
// canonical manifest is package state and callers must not mutate it.
func Components() []Component {
	out := make([]Component, len(components))
	copy(out, components)
	return out
}

// ComponentByID looks a component up by its stable id.
func ComponentByID(id string) (Component, bool) {
	for _, c := range components {
		if c.ID == id {
			return c, true
		}
	}
	return Component{}, false
}

// EmbedSubtree returns the embedded-FS subtree this component extracts.
//
// ok is false when the component has NO subtree — the CLI binary (installed by
// the installer script) and the settings merge (logic in
// cli/internal/managed/, with no file tree behind it). Callers MUST check ok
// before calling Extract; there is no other way to reach the subtree name.
func (c Component) EmbedSubtree() (string, bool) {
	if c.Kind != KindEmbeddedTree || c.embedSubtree == "" {
		return "", false
	}
	return c.embedSubtree, true
}

// TargetRel returns the component's target path relative to ClaudeRoot, in
// slash form ("skills", "templates", "bin", "settings.json").
//
// This is exactly the shape deploy.IsPluginManaged consumes — it takes a path
// relative to the target dir and matches on the FIRST segment. Recording the
// relative target here (rather than only the absolute one) is what lets the
// Phase 3 wizard run the plugin-managed check without re-deriving anything:
//
//	if deploy.IsPluginManaged(c.TargetRel()) { warn and skip — never write }
//
// Under D7 setup DETECTS a plugin-managed Claude Code install and warns
// instead of double-writing; bravros must never write into plugins/,
// .claude-plugin/ or extensions/. No component in this manifest targets one of
// those today, and manifest_test.go asserts it stays that way.
func (c Component) TargetRel() string {
	return path.Join(c.targetRel...)
}

// TargetPathUnder returns the component's absolute target path under an
// explicit Claude root. Use it in tests and anywhere a throwaway HOME is in
// play; TargetPath is the production convenience wrapper.
func (c Component) TargetPathUnder(claudeRoot string) string {
	return filepath.Join(append([]string{claudeRoot}, c.targetRel...)...)
}

// TargetPath returns the component's absolute target path for the current
// user, resolved through os.UserHomeDir — never a hardcoded "~". On Windows
// os.UserHomeDir yields %USERPROFILE%, giving
// %USERPROFILE%\.claude\... per D10 (NOT %LOCALAPPDATA%).
func (c Component) TargetPath() (string, error) {
	root, err := ClaudeRoot()
	if err != nil {
		return "", err
	}
	return c.TargetPathUnder(root), nil
}

// ClaudeRoot returns <home>/.claude — the single per-user root every component
// target hangs off, identical in shape on macOS, Linux and Windows (D10), so
// docs, this manifest and state.json need no per-OS branching.
func ClaudeRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("payload: resolve user home dir: %w", err)
	}
	return filepath.Join(home, ".claude"), nil
}

// Selection is one component's resolved choice — the shape Phase 3 persists in
// state.json and Phase 6 replays after a binary swap.
//
// Skills is the RESOLVED list, stored alongside Scope rather than derived from
// it at read time. That redundancy is deliberate: it pins exactly what was
// installed even if the core/all split (or the scope vocabulary itself) moves
// in a later release, and it is what makes an idempotent, non-destructive
// re-run comparable against reality.
//
// The list is deliberately plain skill DIRECTORY NAMES, i.e. exactly what
// deploy.DeployOpts.EnabledSkills already takes — see EnabledSkills below.
// Scope is not a new selection mechanism; it is a way to compute the existing
// one's input.
type Selection struct {
	ID     string     `json:"id"`
	Scope  SkillScope `json:"scope,omitempty"`
	Skills []string   `json:"skills,omitempty"`
}

// EnabledSkills returns the resolved skill names in the shape
// deploy.DeployOpts.EnabledSkills consumes, so the wizard drives the EXISTING
// per-skill selection path instead of inventing a parallel one.
//
// Why this exists at all, given that path already works: config.EnabledSkills
// reads .bravros.yml:skills.enabled relative to CWD
// (cli/internal/config/preserve.go:10-13, and cmd/selfupdate.go:271 says so in
// as many words), which makes "I only want core skills" a PER-PROJECT answer
// that depends on which repo the operator happened to be standing in when the
// SessionStart hook fired. `bravros setup` plus state.json give that choice its
// first GLOBAL home; the resolved list here is what a global caller passes.
//
// Two semantics for the two consumers — this type supports both and prescribes
// neither:
//
//   - initial `setup` (Phase 3): FilterMode false. deploy.isSkillEnabled keeps
//     listed skills plus anything with `core: true`, and the rest are never
//     written.
//   - SessionStart refresh (Phase 6): FilterMode true, as cmd/selfupdate.go:278
//     already passes — the allowlist stays additive, so a refresh re-extracts
//     exactly the state.json set and never eats skills the user added by hand.
//
// Note the empty slice is NOT a safe stand-in for "everything": an empty
// EnabledSkills means "deploy all" by backward-compatible default, so a
// ScopeAll selection carries the full explicit list, which resolves to the same
// deploy set while keeping state.json a literal record of what was installed.
func (s Selection) EnabledSkills() []string {
	if len(s.Skills) == 0 {
		return nil
	}
	out := make([]string, len(s.Skills))
	copy(out, s.Skills)
	return out
}

// Select resolves a component plus a scope into a Selection. scope is ignored
// for unscoped components and must be valid for scoped ones.
func (c Component) Select(scope SkillScope) (Selection, error) {
	if !c.Scoped {
		return Selection{ID: c.ID}, nil
	}
	skills, err := ResolveSkills(scope)
	if err != nil {
		return Selection{}, err
	}
	return Selection{ID: c.ID, Scope: scope, Skills: skills}, nil
}

// DefaultSelections resolves every default-on component at its default scope —
// the state the wizard opens with, and the state `--yes` without an explicit
// component list installs.
func DefaultSelections() ([]Selection, error) {
	var out []Selection
	for _, c := range components {
		if !c.Default && !c.Required {
			continue
		}
		sel, err := c.Select(c.DefaultScope)
		if err != nil {
			return nil, err
		}
		out = append(out, sel)
	}
	return out, nil
}

// SkillNames returns every skill directory name in the embedded payload,
// sorted.
func SkillNames() ([]string, error) {
	entries, err := fs.ReadDir(FS, "skills")
	if err != nil {
		return nil, fmt.Errorf("payload: read embedded skills dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// ResolveSkills returns the sorted skill directory names a scope selects.
//
// An unknown scope is an ERROR, never a silent fallback to "all": a newer
// state.json read by an older binary must fail loudly rather than quietly
// installing more than the user asked for.
func ResolveSkills(scope SkillScope) ([]string, error) {
	all, err := SkillNames()
	if err != nil {
		return nil, err
	}

	switch scope {
	case ScopeAll:
		return all, nil
	case ScopeCore:
		var core []string
		for _, name := range all {
			isCore, err := SkillIsCore(name)
			if err != nil {
				return nil, err
			}
			if isCore {
				core = append(core, name)
			}
		}
		return core, nil
	default:
		return nil, fmt.Errorf("payload: unknown skill scope %q (known: %q, %q)", string(scope), string(ScopeCore), string(ScopeAll))
	}
}

// SkillIsCore reports whether the embedded skill named name carries
// `core: true` in its SKILL.md frontmatter.
//
// This is the embedded-FS-native twin of deploy.IsSkillCore, which reads a
// filesystem PATH and therefore cannot be pointed at an embed.FS without first
// spilling the payload to disk. The parse below is deliberately the same
// algorithm — scan only between the first pair of "---" delimiters, match the
// exact line `core: true` — and TestSkillIsCore_AgreesWithDeployIsSkillCore
// asserts the two agree on every embedded skill, so the duplication can never
// drift unnoticed. Reimplementing it here (rather than importing deploy) keeps
// payload dependency-free, which matters because the later phases point the
// deploy path AT this package; an import in this direction would foreclose
// that.
func SkillIsCore(name string) (bool, error) {
	skillMD := path.Join("skills", name, "SKILL.md")
	f, err := FS.Open(skillMD)
	if err != nil {
		return false, fmt.Errorf("payload: open embedded %q: %w", skillMD, err)
	}
	defer f.Close()

	inFrontmatter := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			}
			// Second --- closes the frontmatter.
			break
		}
		if !inFrontmatter {
			continue
		}
		if strings.TrimSpace(line) == "core: true" {
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("payload: scan embedded %q: %w", skillMD, err)
	}
	return false, nil
}
