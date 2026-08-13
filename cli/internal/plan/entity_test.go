package plan_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bravros/bravros/cli/internal/plan"
)

// TestAllEntities_RegistrySeeded verifies the four baseline entities are present
// in AllEntities with the expected Name/Prefix/Dir/Kind values.
func TestAllEntities_RegistrySeeded(t *testing.T) {
	want := []struct {
		name   string
		prefix string
		dir    string
		kind   plan.EntityKind
	}{
		{"plan", "P", "", plan.EntityKindFile},
		{"backlog", "B", "backlog", plan.EntityKindFile},
		{"report", "R", "reports", plan.EntityKindFile},
		{"user_report", "U", "user-reports", plan.EntityKindFile},
	}

	if len(plan.AllEntities) < len(want) {
		t.Fatalf("AllEntities has %d entries, want at least %d", len(plan.AllEntities), len(want))
	}

	for i, w := range want {
		e := plan.AllEntities[i]
		if e.Name != w.name {
			t.Errorf("[%d] Name = %q, want %q", i, e.Name, w.name)
		}
		if e.Prefix != w.prefix {
			t.Errorf("[%d] Prefix = %q, want %q", i, e.Prefix, w.prefix)
		}
		if e.Dir != w.dir {
			t.Errorf("[%d] Dir = %q, want %q", i, e.Dir, w.dir)
		}
		if e.Kind != w.kind {
			t.Errorf("[%d] Kind = %q, want %q", i, e.Kind, w.kind)
		}
	}
}

// TestEntityByName_HappyPath checks that EntityByName returns the correct def
// for each registered entity name.
func TestEntityByName_HappyPath(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
	}{
		{"plan", "P"},
		{"backlog", "B"},
		{"report", "R"},
		{"user_report", "U"},
	}
	for _, tc := range cases {
		e, ok := plan.EntityByName(tc.name)
		if !ok {
			t.Errorf("EntityByName(%q): not found", tc.name)
			continue
		}
		if e.Prefix != tc.prefix {
			t.Errorf("EntityByName(%q).Prefix = %q, want %q", tc.name, e.Prefix, tc.prefix)
		}
	}
}

// TestEntityByName_Unknown verifies that an unknown name returns false.
func TestEntityByName_Unknown(t *testing.T) {
	_, ok := plan.EntityByName("nonexistent")
	if ok {
		t.Error("EntityByName(\"nonexistent\") returned ok=true, want false")
	}
}

// TestEntityByPrefix_HappyPath checks that EntityByPrefix returns the correct def.
func TestEntityByPrefix_HappyPath(t *testing.T) {
	cases := []struct {
		prefix string
		name   string
	}{
		{"P", "plan"},
		{"B", "backlog"},
		{"R", "report"},
		{"U", "user_report"},
	}
	for _, tc := range cases {
		e, ok := plan.EntityByPrefix(tc.prefix)
		if !ok {
			t.Errorf("EntityByPrefix(%q): not found", tc.prefix)
			continue
		}
		if e.Name != tc.name {
			t.Errorf("EntityByPrefix(%q).Name = %q, want %q", tc.prefix, e.Name, tc.name)
		}
	}
}

// TestEntityByPrefix_Unknown verifies that an unknown prefix returns false.
func TestEntityByPrefix_Unknown(t *testing.T) {
	_, ok := plan.EntityByPrefix("Z")
	if ok {
		t.Error("EntityByPrefix(\"Z\") returned ok=true, want false")
	}
}

// TestAllPrefixes_ContainsAllFour verifies AllPrefixes contains the expected
// "<letter>-" strings for the four baseline entities.
func TestAllPrefixes_ContainsAllFour(t *testing.T) {
	prefixes := plan.AllPrefixes()
	want := []string{"P-", "B-", "R-", "U-"}
	if len(prefixes) < len(want) {
		t.Fatalf("AllPrefixes() len = %d, want at least %d; got %v", len(prefixes), len(want), prefixes)
	}
	// Build a set for membership checks.
	set := make(map[string]bool, len(prefixes))
	for _, p := range prefixes {
		set[p] = true
	}
	for _, w := range want {
		if !set[w] {
			t.Errorf("AllPrefixes() missing %q; got %v", w, prefixes)
		}
	}
}

// TestEntityDef_AbsDir verifies that AbsDir joins planningRoot + Dir correctly
// and that plan (Dir=="") returns planningRoot unchanged.
func TestEntityDef_AbsDir(t *testing.T) {
	root := "/repo/.planning"

	cases := []struct {
		name    string
		wantDir string
	}{
		{"plan", root},
		{"backlog", filepath.Join(root, "backlog")},
		{"report", filepath.Join(root, "reports")},
		{"user_report", filepath.Join(root, "user-reports")},
	}
	for _, tc := range cases {
		e, ok := plan.EntityByName(tc.name)
		if !ok {
			t.Fatalf("EntityByName(%q) not found", tc.name)
		}
		got := e.AbsDir(root)
		if got != tc.wantDir {
			t.Errorf("EntityByName(%q).AbsDir(%q) = %q, want %q", tc.name, root, got, tc.wantDir)
		}
	}
}

// TestNormalizePlanID_UsesRegistry checks that NormalizePlanID correctly strips
// all four entity prefixes (derived from AllPrefixes, not hardcoded).
func TestNormalizePlanID_UsesRegistry(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"P-0091", "0091"},
		{"B-0145", "0145"},
		{"R-0001", "0001"},
		{"U-0003", "0003"},
		{"0057", "0057"},
		{"57", "0057"},
	}
	for _, tc := range cases {
		got := plan.NormalizePlanID(tc.input)
		if got != tc.want {
			t.Errorf("NormalizePlanID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestPlanEntity_IsDualKind verifies the "plan" entity is dual-kind
// (AllowsDirectory=true, Kind stays EntityKindFile so existing
// `Kind == EntityKindFile` callers are unaffected) while every other
// registered entity remains single-shape (P-0180).
func TestPlanEntity_IsDualKind(t *testing.T) {
	planDef, ok := plan.EntityByName("plan")
	if !ok {
		t.Fatal("EntityByName(\"plan\") not found")
	}
	if planDef.Kind != plan.EntityKindFile {
		t.Errorf("plan.Kind = %q, want %q (Kind must stay EntityKindFile for backward compat)", planDef.Kind, plan.EntityKindFile)
	}
	if !planDef.AllowsDirectory {
		t.Error("plan.AllowsDirectory = false, want true")
	}
	if !planDef.IsDualKind() {
		t.Error("plan.IsDualKind() = false, want true")
	}

	for _, name := range []string{"backlog", "report", "user_report", "debug"} {
		e, ok := plan.EntityByName(name)
		if !ok {
			t.Fatalf("EntityByName(%q) not found", name)
		}
		if e.IsDualKind() {
			t.Errorf("%s.IsDualKind() = true, want false", name)
		}
	}
}

// TestResolvePlanEntryFile_FileUnchanged verifies that a plain file path
// (the existing single-file plan shape) is returned unchanged, and that a
// nonexistent path is also returned unchanged (not treated as a directory).
func TestResolvePlanEntryFile_FileUnchanged(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "0180-feat-example-todo.md")
	if err := os.WriteFile(filePath, []byte("---\nid: P-0180\n---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got := plan.ResolvePlanEntryFile(filePath); got != filePath {
		t.Errorf("ResolvePlanEntryFile(file) = %q, want %q", got, filePath)
	}

	nonexistent := filepath.Join(tmp, "does-not-exist.md")
	if got := plan.ResolvePlanEntryFile(nonexistent); got != nonexistent {
		t.Errorf("ResolvePlanEntryFile(nonexistent) = %q, want %q (unchanged)", got, nonexistent)
	}
}

// TestResolvePlanEntryFile_PlanMDFolder verifies the canonical PLAN.md entry
// file wins even when other .md files are present in the folder.
func TestResolvePlanEntryFile_PlanMDFolder(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "P-0180-feat-example")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	planMD := filepath.Join(dir, "PLAN.md")
	if err := os.WriteFile(planMD, []byte("---\nid: P-0180\n---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "NOTES.md"), []byte("scratch notes\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got := plan.ResolvePlanEntryFile(dir); got != planMD {
		t.Errorf("ResolvePlanEntryFile(dir) = %q, want %q", got, planMD)
	}
}

// TestResolvePlanEntryFile_IDPrefixedFolder verifies the id-prefixed *.md
// fallback (no PLAN.md present) for legacy/manually-made folders.
func TestResolvePlanEntryFile_IDPrefixedFolder(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "P-0180-feat-example")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	idMD := filepath.Join(dir, "P-0180-feat-example.md")
	if err := os.WriteFile(idMD, []byte("---\nid: P-0180\n---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.md"), []byte("other\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got := plan.ResolvePlanEntryFile(dir); got != idMD {
		t.Errorf("ResolvePlanEntryFile(dir) = %q, want %q", got, idMD)
	}
}

// TestResolvePlanEntryFile_TaskListFolder verifies the legacy TASKLIST.md
// fallback fires only when neither PLAN.md nor an id-prefixed *.md exists.
func TestResolvePlanEntryFile_TaskListFolder(t *testing.T) {
	tmp := t.TempDir()
	// A legacy folder without a P-NNNN id prefix in its own name (mirrors
	// paylog's correios-tracking-realtime/-style folders).
	dir := filepath.Join(tmp, "correios-tracking-realtime")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	taskList := filepath.Join(dir, "TASKLIST.md")
	if err := os.WriteFile(taskList, []byte("# tasks\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got := plan.ResolvePlanEntryFile(dir); got != taskList {
		t.Errorf("ResolvePlanEntryFile(dir) = %q, want %q", got, taskList)
	}
}

// TestResolvePlanEntryFile_FrontmatterFallback verifies the final fallback:
// the first *.md file (sorted by name) that carries a YAML frontmatter block,
// when none of PLAN.md / id-prefixed *.md / TASKLIST.md exist.
func TestResolvePlanEntryFile_FrontmatterFallback(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "some-folder")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	// "aaa.md" sorts before "zzz.md" but has no frontmatter — must be skipped.
	if err := os.WriteFile(filepath.Join(dir, "aaa.md"), []byte("no frontmatter here\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	zzz := filepath.Join(dir, "zzz.md")
	if err := os.WriteFile(zzz, []byte("---\ntitle: example\n---\nbody\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got := plan.ResolvePlanEntryFile(dir); got != zzz {
		t.Errorf("ResolvePlanEntryFile(dir) = %q, want %q", got, zzz)
	}
}

// TestResolvePlanEntryFile_EmptyFolder verifies "" is returned when nothing
// resolves (no PLAN.md, no id-prefixed *.md, no TASKLIST.md, no frontmatter
// .md file — including a truly empty directory).
func TestResolvePlanEntryFile_EmptyFolder(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "empty-folder")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if got := plan.ResolvePlanEntryFile(dir); got != "" {
		t.Errorf("ResolvePlanEntryFile(empty dir) = %q, want \"\"", got)
	}

	// Also verify a folder with only non-matching .md content (no frontmatter)
	// resolves to "" rather than an arbitrary file.
	dir2 := filepath.Join(tmp, "no-frontmatter-folder")
	if err := os.Mkdir(dir2, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "plain.md"), []byte("no frontmatter\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := plan.ResolvePlanEntryFile(dir2); got != "" {
		t.Errorf("ResolvePlanEntryFile(no-frontmatter dir) = %q, want \"\"", got)
	}
}

// TestReservePlanDir_CreatesFolderAndSeed verifies ReservePlanDir creates a
// P-NNNN-<slug>/ directory (mirroring ReserveDebugDir's directory-creation
// path) seeded with a PLAN.md that ResolvePlanEntryFile can immediately
// resolve.
func TestReservePlanDir_CreatesFolderAndSeed(t *testing.T) {
	tmp := t.TempDir()

	id, dirPath, err := plan.ReservePlanDir(tmp, "feat-example", "single-tree")
	if err != nil {
		t.Fatalf("ReservePlanDir: %v", err)
	}
	if id == "" || dirPath == "" {
		t.Fatalf("ReservePlanDir returned empty id/dirPath: id=%q dirPath=%q", id, dirPath)
	}

	wantDirName := id + "-feat-example"
	if filepath.Base(dirPath) != wantDirName {
		t.Errorf("dirPath base = %q, want %q", filepath.Base(dirPath), wantDirName)
	}

	entry := plan.ResolvePlanEntryFile(dirPath)
	wantEntry := filepath.Join(dirPath, "PLAN.md")
	if entry != wantEntry {
		t.Errorf("ResolvePlanEntryFile(reserved dir) = %q, want %q", entry, wantEntry)
	}

	if _, statErr := os.Stat(wantEntry); statErr != nil {
		t.Errorf("seeded PLAN.md missing: %v", statErr)
	}
}
