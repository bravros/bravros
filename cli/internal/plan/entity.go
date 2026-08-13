package plan

import "path/filepath"

// EntityKind describes how an entity's ID maps to the filesystem.
//
//   - "file"      — each ID is a single Markdown file (plan, backlog, report,
//     user_report). The CLI writes a <id>.placeholder sentinel on reserve and
//     renames it to the final filename on commit.
//   - "directory" — each ID is a directory whose name encodes the lifecycle
//     stage as a suffix (e.g. D-NNNN-<slug>-open/ → …-complete/).
type EntityKind string

const (
	EntityKindFile      EntityKind = "file"
	EntityKindDirectory EntityKind = "directory"
)

// EntityDef is the single source of truth for one planning entity stream.
//
// Consumers (nextid reserve/release, graph, meta, advance, finish …) MUST
// derive {dir, prefix} from this registry instead of hardcoding the values.
// Adding a new entity means adding one entry to AllEntities — nothing else.
type EntityDef struct {
	// Name is the canonical CLI argument used by "kaisser nextid reserve <name>".
	Name string
	// Prefix is the single-letter prefix used in IDs (e.g. "P", "B", "R", "U").
	Prefix string
	// Dir is the path of the entity's directory RELATIVE to the planning root
	// (.planning/).  For plan this is "" (files live directly in .planning/);
	// for backlog it is "backlog"; etc.  Callers convert to an absolute path via
	// EntityDef.AbsDir(planningRoot).
	Dir string
	// Kind describes the entity's PRIMARY filesystem shape; see EntityKind
	// constants. Existing callers that switch on Kind (e.g. `Kind ==
	// EntityKindDirectory`) keep working unchanged for every entity.
	Kind EntityKind
	// AllowsDirectory reports whether this entity ALSO accepts a
	// directory-shaped instance in addition to its primary Kind — a plan can
	// be either a single `.planning/NNNN-slug.md` file (Kind stays
	// EntityKindFile, unchanged) OR a `.planning/P-NNNN-<slug>/` folder
	// (P-0180 locked decision #2). Only "plan" sets this to true; every other
	// entity is single-shape (AllowsDirectory stays false). Use
	// EntityDef.IsDualKind() rather than reading this field directly.
	AllowsDirectory bool
}

// IsDualKind reports whether this entity resolves both a file AND a
// directory instance (currently only "plan" — P-0180 locked decision #2).
func (e EntityDef) IsDualKind() bool {
	return e.Kind == EntityKindFile && e.AllowsDirectory
}

// AbsDir returns the absolute directory path for this entity given the
// planning root (the .planning/ directory itself, already absolute).
//
//	planningRoot = filepath.Join(gitRoot, ".planning")
func (e EntityDef) AbsDir(planningRoot string) string {
	if e.Dir == "" {
		return planningRoot
	}
	return filepath.Join(planningRoot, e.Dir)
}

// AllEntities is the canonical ordered registry of all planning entity streams.
// Phase 1 seeds the four existing "file" entities; Phase 2 adds "debug".
var AllEntities = []EntityDef{
	// plan is dual-kind (P-0180): a plan is either a single .planning/NNNN-slug.md
	// file (unchanged, legacy behavior — Kind stays EntityKindFile so every
	// existing `Kind == EntityKindFile` / `Kind == EntityKindDirectory` switch
	// keeps working as before) OR a .planning/P-NNNN-<slug>/ folder whose
	// canonical entry file is resolved via ResolvePlanEntryFile (resolve.go).
	{Name: "plan", Prefix: "P", Dir: "", Kind: EntityKindFile, AllowsDirectory: true},
	{Name: "backlog", Prefix: "B", Dir: "backlog", Kind: EntityKindFile},
	{Name: "report", Prefix: "R", Dir: "reports", Kind: EntityKindFile},
	{Name: "user_report", Prefix: "U", Dir: "user-reports", Kind: EntityKindFile},
	// debug is a directory-kind entity: each investigation lives in its own
	// .planning/debug/D-NNNN-<slug>-<stage>/ directory. Stage transitions rename
	// the directory via the CLI (never a raw mv call) so Rule 16 is always satisfied.
	{Name: "debug", Prefix: "D", Dir: "debug", Kind: EntityKindDirectory},
}

// EntityByName returns the EntityDef whose Name matches n (case-insensitive).
// Returns the zero value and false when no match is found.
func EntityByName(n string) (EntityDef, bool) {
	for _, e := range AllEntities {
		if e.Name == n {
			return e, true
		}
	}
	return EntityDef{}, false
}

// EntityByPrefix returns the EntityDef whose single-letter Prefix matches p
// (compared as-is; callers should uppercase before calling).
// Returns the zero value and false when no match is found.
func EntityByPrefix(p string) (EntityDef, bool) {
	for _, e := range AllEntities {
		if e.Prefix == p {
			return e, true
		}
	}
	return EntityDef{}, false
}

// AllPrefixes returns the "<Prefix>-" strings for all registered entities in
// registration order.  Used by NormalizePlanID and similar prefix-stripping
// loops that previously hardcoded []string{"P-", "B-", "R-", "U-"}.
func AllPrefixes() []string {
	out := make([]string, len(AllEntities))
	for i, e := range AllEntities {
		out[i] = e.Prefix + "-"
	}
	return out
}
