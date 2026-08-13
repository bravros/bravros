package plan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TestScoutEntity_RegistryEntry — entity.go
// ---------------------------------------------------------------------------

// TestScoutEntity_RegistryEntry verifies the scout entity is registered with
// the correct fields (prefix=D, dir=scout, kind=directory).
func TestScoutEntity_RegistryEntry(t *testing.T) {
	e, ok := EntityByName("scout")
	if !ok {
		t.Fatal("EntityByName(\"scout\") not found — entity not registered")
	}
	if e.Prefix != "S" {
		t.Errorf("Prefix = %q, want %q", e.Prefix, "S")
	}
	if e.Dir != "scout" {
		t.Errorf("Dir = %q, want %q", e.Dir, "scout")
	}
	if e.Kind != EntityKindDirectory {
		t.Errorf("Kind = %q, want %q", e.Kind, EntityKindDirectory)
	}

	// EntityByPrefix lookup
	ep, ok := EntityByPrefix("S")
	if !ok {
		t.Fatal("EntityByPrefix(\"D\") not found")
	}
	if ep.Name != "scout" {
		t.Errorf("EntityByPrefix(\"D\").Name = %q, want %q", ep.Name, "scout")
	}

	// AllPrefixes includes S-
	prefixes := AllPrefixes()
	found := false
	for _, p := range prefixes {
		if p == "S-" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("AllPrefixes() does not contain \"S-\"; got %v", prefixes)
	}
}

// ---------------------------------------------------------------------------
// TestScoutEntity_AbsDir — entity.go
// ---------------------------------------------------------------------------

func TestScoutEntity_AbsDir(t *testing.T) {
	root := "/repo/.planning"
	e, _ := EntityByName("scout")
	got := e.AbsDir(root)
	want := filepath.Join(root, "scout")
	if got != want {
		t.Errorf("AbsDir(%q) = %q, want %q", root, got, want)
	}
}

// ---------------------------------------------------------------------------
// TestReserveScoutDir — meta.go: directory creation
// ---------------------------------------------------------------------------

func TestReserveScoutDir_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	// Pass "" scanMode: auto mode falls through gracefully when outside a git repo.
	id, dirPath, err := ReserveScoutDir(dir, "my-bug", "")
	if err != nil {
		t.Fatalf("ReserveScoutDir: %v", err)
	}
	if id == "" {
		t.Error("id is empty")
	}
	if !strings.HasPrefix(id, "S-") {
		t.Errorf("id = %q, want S- prefix", id)
	}
	// The directory name should encode the slug and the -open stage.
	base := filepath.Base(dirPath)
	if !strings.Contains(base, "my-bug") {
		t.Errorf("dirPath base = %q, want slug 'my-bug'", base)
	}
	if !strings.HasSuffix(base, "-open") {
		t.Errorf("dirPath base = %q, want -open suffix", base)
	}
	// Directory must exist on disk.
	info, err := os.Stat(dirPath)
	if err != nil {
		t.Fatalf("directory not on disk: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("path exists but is not a directory: %q", dirPath)
	}
}

func TestReserveScoutDir_DefaultSlug(t *testing.T) {
	dir := t.TempDir()
	// Pass "" scanMode: auto mode falls through gracefully when outside a git repo.
	_, dirPath, err := ReserveScoutDir(dir, "", "")
	if err != nil {
		t.Fatalf("ReserveScoutDir (empty slug): %v", err)
	}
	base := filepath.Base(dirPath)
	if !strings.Contains(base, "investigation") {
		t.Errorf("dirPath base = %q, want default slug 'investigation'", base)
	}
}

// TestReserveScoutDir_SequentialIDs verifies that two sequential reservations in
// the same dir return distinct sequential IDs.
// Uses single-tree mode to isolate from any cross-worktree state. P-0170.
func TestReserveScoutDir_SequentialIDs(t *testing.T) {
	dir := t.TempDir()
	// Pass "single-tree" via scanMode parameter to isolate from cross-worktree state.
	id1, _, err := ReserveScoutDir(dir, "first", "single-tree")
	if err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	id2, _, err := ReserveScoutDir(dir, "second", "single-tree")
	if err != nil {
		t.Fatalf("second reserve: %v", err)
	}
	if id1 == id2 {
		t.Errorf("sequential IDs must differ, both = %q", id1)
	}
}

// ---------------------------------------------------------------------------
// TestReleaseScoutDir — meta.go: directory removal
// ---------------------------------------------------------------------------

func TestReleaseScoutDir_RemovesDirectory(t *testing.T) {
	dir := t.TempDir()
	// Pass "" scanMode: auto mode falls through gracefully when outside a git repo.
	id, dirPath, err := ReserveScoutDir(dir, "release-test", "")
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	// Directory exists before release.
	if _, err := os.Stat(dirPath); err != nil {
		t.Fatalf("dir should exist before release: %v", err)
	}
	if err := ReleaseScoutDir(dir, id); err != nil {
		t.Fatalf("ReleaseScoutDir: %v", err)
	}
	// Directory should be gone.
	if _, err := os.Stat(dirPath); !os.IsNotExist(err) {
		t.Errorf("dir should be gone after release; stat err = %v", err)
	}
}

func TestReleaseScoutDir_Idempotent(t *testing.T) {
	dir := t.TempDir()
	// Release a non-existent ID — must return nil (idempotent).
	if err := ReleaseScoutDir(dir, "S-9999"); err != nil {
		t.Errorf("ReleaseScoutDir on non-existent ID should be nil, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestFindEntityDirByID — meta.go: ID → directory resolution
// ---------------------------------------------------------------------------

func TestFindEntityDirByID_HappyPath(t *testing.T) {
	dir := t.TempDir()
	// Create a scout investigation directory manually.
	target := filepath.Join(dir, "S-0001-my-bug-open")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got, err := FindEntityDirByID(dir, "S-0001")
	if err != nil {
		t.Fatalf("FindEntityDirByID: %v", err)
	}
	if got != target {
		t.Errorf("got %q, want %q", got, target)
	}
}

func TestFindEntityDirByID_StageAgnostic(t *testing.T) {
	dir := t.TempDir()
	// Same ID but at -complete stage.
	target := filepath.Join(dir, "S-0002-foo-complete")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got, err := FindEntityDirByID(dir, "S-0002")
	if err != nil {
		t.Fatalf("FindEntityDirByID: %v", err)
	}
	if got != target {
		t.Errorf("got %q, want %q", got, target)
	}
}

func TestFindEntityDirByID_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := FindEntityDirByID(dir, "S-9999")
	if err == nil {
		t.Error("expected error for missing ID, got nil")
	}
}

func TestFindEntityDirByID_Ambiguous(t *testing.T) {
	dir := t.TempDir()
	// Create two directories with the same numeric ID (should not happen in practice
	// but the resolver must handle it gracefully).
	os.Mkdir(filepath.Join(dir, "S-0003-foo-open"), 0755)
	os.Mkdir(filepath.Join(dir, "S-0003-foo-complete"), 0755)
	_, err := FindEntityDirByID(dir, "S-0003")
	if err == nil {
		t.Error("expected error for ambiguous ID, got nil")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error message = %q, want 'ambiguous'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// TestSanitizeSlug — meta.go: slug sanitisation
// ---------------------------------------------------------------------------

func TestSanitizeSlug(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"my-bug", "my-bug"},
		{"My Bug Report", "my-bug-report"},
		{"nil pointer dereference", "nil-pointer-dereference"},
		{"--leading--dashes--", "leading-dashes"},
		{"UPPERCASE", "uppercase"},
		{"123-numbers", "123-numbers"},
		{"", ""},
		{"!!!special---chars!!!", "special-chars"},
	}
	for _, tc := range cases {
		got := sanitizeSlug(tc.input)
		if got != tc.want {
			t.Errorf("sanitizeSlug(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
