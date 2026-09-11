package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadConfiguredProjectName_JSON verifies the `project` key is read from
// .bravros/config.json via a generic map lookup, even though it isn't a
// modeled BravrosConfig struct field.
func TestReadConfiguredProjectName_JSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".bravros"), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"staging_branch": "homolog", "project": "my-json-project"}`
	if err := os.WriteFile(filepath.Join(dir, ".bravros", "config.json"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}

	if got := readConfiguredProjectName(dir); got != "my-json-project" {
		t.Errorf("readConfiguredProjectName() = %q, want %q", got, "my-json-project")
	}
}

// TestReadConfiguredProjectName_LegacyFallback verifies a legacy .bravros.yml
// `project:` line is still honored when .bravros/config.json is absent.
func TestReadConfiguredProjectName_LegacyFallback(t *testing.T) {
	dir := t.TempDir()
	yml := "staging_branch: homolog\nproject: my-legacy-project\n"
	if err := os.WriteFile(filepath.Join(dir, ".bravros.yml"), []byte(yml), 0644); err != nil {
		t.Fatal(err)
	}

	if got := readConfiguredProjectName(dir); got != "my-legacy-project" {
		t.Errorf("readConfiguredProjectName() = %q, want %q", got, "my-legacy-project")
	}
}

// TestReadConfiguredProjectName_Unset verifies an empty string comes back
// when neither config file sets a project field.
func TestReadConfiguredProjectName_Unset(t *testing.T) {
	dir := t.TempDir()

	if got := readConfiguredProjectName(dir); got != "" {
		t.Errorf("readConfiguredProjectName() = %q, want empty string", got)
	}
	if got := projectNameFromDir(dir); got != filepath.Base(dir) {
		t.Errorf("projectNameFromDir() = %q, want dir basename %q", got, filepath.Base(dir))
	}
}

// TestIsClean_CleanRepo verifies IsClean returns true on a repo with no
// untracked or modified files.
func TestIsClean_CleanRepo(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	clean, porcelain, err := IsClean()
	if err != nil {
		t.Fatalf("IsClean() error: %v", err)
	}
	if !clean {
		t.Errorf("expected clean=true, got false; porcelain=%q", porcelain)
	}
	if porcelain != "" {
		t.Errorf("expected empty porcelain output for clean repo, got %q", porcelain)
	}
}

// TestIsClean_UntrackedFile verifies IsClean returns false when an untracked
// file is present.
func TestIsClean_UntrackedFile(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Create an untracked file
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	clean, porcelain, err := IsClean()
	if err != nil {
		t.Fatalf("IsClean() error: %v", err)
	}
	if clean {
		t.Error("expected clean=false with untracked file, got true")
	}
	if !strings.Contains(porcelain, "untracked.txt") {
		t.Errorf("expected porcelain output to mention 'untracked.txt', got %q", porcelain)
	}
	if !strings.Contains(porcelain, "??") {
		t.Errorf("expected '??' marker for untracked file, got %q", porcelain)
	}
}

// TestIsClean_ModifiedFile verifies IsClean returns false when a tracked file
// has been modified but not staged.
func TestIsClean_ModifiedFile(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Modify the tracked README.md without staging
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# modified\n"), 0644); err != nil {
		t.Fatal(err)
	}

	clean, porcelain, err := IsClean()
	if err != nil {
		t.Fatalf("IsClean() error: %v", err)
	}
	if clean {
		t.Error("expected clean=false with modified tracked file, got true")
	}
	if !strings.Contains(porcelain, "README.md") {
		t.Errorf("expected porcelain output to mention 'README.md', got %q", porcelain)
	}
}

// TestIsClean_StagedFile verifies IsClean returns false when a file is staged
// but not yet committed (staged changes count as dirty).
func TestIsClean_StagedFile(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Stage a new file
	newFile := filepath.Join(dir, "staged.txt")
	if err := os.WriteFile(newFile, []byte("staged"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "git", "add", "staged.txt")

	clean, porcelain, err := IsClean()
	if err != nil {
		t.Fatalf("IsClean() error: %v", err)
	}
	if clean {
		t.Error("expected clean=false with staged-but-uncommitted file, got true")
	}
	if !strings.Contains(porcelain, "staged.txt") {
		t.Errorf("expected porcelain output to mention 'staged.txt', got %q", porcelain)
	}
}

// TestReadConfiguredProjectName_JSONWithoutProjectFallsBackToLegacy pins the
// documented fallback order: a .bravros/config.json that exists but sets no
// `project` key must not shadow a legacy .bravros.yml `project:` line.
func TestReadConfiguredProjectName_JSONWithoutProjectFallsBackToLegacy(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".bravros"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".bravros", "config.json"), []byte(`{"staging_branch": "homolog"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".bravros.yml"), []byte("project: my-legacy-project\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if got := readConfiguredProjectName(dir); got != "my-legacy-project" {
		t.Errorf("readConfiguredProjectName() = %q, want %q", got, "my-legacy-project")
	}
}

// TestReadConfiguredProjectName_SbravrosLegacyFallback verifies the second
// legacy filename (.sbravros.yml) is consulted too — the same pair
// cmd/config.go's loadConfigMap reads, so the two lookups cannot disagree
// about which projects have a config.
func TestReadConfiguredProjectName_SbravrosLegacyFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".sbravros.yml"), []byte("project: \"my-sbravros-project\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if got := readConfiguredProjectName(dir); got != "my-sbravros-project" {
		t.Errorf("readConfiguredProjectName() = %q, want %q", got, "my-sbravros-project")
	}
}
