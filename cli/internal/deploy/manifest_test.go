package deploy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ComputeSkillSHA tests
// ---------------------------------------------------------------------------

func TestManifestComputeSkillSHA_Deterministic(t *testing.T) {
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# skill"), 0644))
	must(os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\necho hi"), 0644))

	sha1, err := ComputeSkillSHA(dir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	sha2, err := ComputeSkillSHA(dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if sha1 != sha2 {
		t.Errorf("non-deterministic: %q != %q", sha1, sha2)
	}
	if sha1 == "" {
		t.Error("SHA must not be empty")
	}
}

func TestManifestComputeSkillSHA_ResolvesSymlinks(t *testing.T) {
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Shared target file lives outside the skill directory.
	sharedDir := t.TempDir()
	target := filepath.Join(sharedDir, "foo.md")
	must(os.WriteFile(target, []byte("original content"), 0644))

	// Skill directory with a references/ subdir containing a symlink.
	skillDir := t.TempDir()
	refsDir := filepath.Join(skillDir, "references")
	must(os.MkdirAll(refsDir, 0755))
	must(os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# skill"), 0644))
	// Symlink points to the absolute path of the target (avoids relative-path
	// issues across different temp dirs).
	must(os.Symlink(target, filepath.Join(refsDir, "shared.md")))

	sha1, err := ComputeSkillSHA(skillDir)
	if err != nil {
		t.Fatalf("before modify: %v", err)
	}

	// Modify the symlink target — SHA must change.
	must(os.WriteFile(target, []byte("modified content"), 0644))

	sha2, err := ComputeSkillSHA(skillDir)
	if err != nil {
		t.Fatalf("after modify: %v", err)
	}

	if sha1 == sha2 {
		t.Error("SHA did not change after modifying symlink target")
	}
}

func TestManifestComputeSkillSHA_DetectsContentChange(t *testing.T) {
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "SKILL.md")
	must(os.WriteFile(file, []byte("version one"), 0644))

	sha1, err := ComputeSkillSHA(dir)
	if err != nil {
		t.Fatalf("before change: %v", err)
	}

	must(os.WriteFile(file, []byte("version twoX"), 0644)) // one byte different

	sha2, err := ComputeSkillSHA(dir)
	if err != nil {
		t.Fatalf("after change: %v", err)
	}

	if sha1 == sha2 {
		t.Error("SHA did not change after content modification")
	}
}

func TestManifestComputeSkillSHA_DetectsFileRename(t *testing.T) {
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.sh")
	newPath := filepath.Join(dir, "new.sh")
	must(os.WriteFile(oldPath, []byte("#!/bin/sh"), 0644))

	sha1, err := ComputeSkillSHA(dir)
	if err != nil {
		t.Fatalf("before rename: %v", err)
	}

	must(os.Rename(oldPath, newPath))

	sha2, err := ComputeSkillSHA(dir)
	if err != nil {
		t.Fatalf("after rename: %v", err)
	}

	// The relative path is included in each record, so a rename changes the SHA
	// even though the file content is identical.
	if sha1 == sha2 {
		t.Error("SHA did not change after file rename (rel path must be part of record)")
	}
}

func TestManifestComputeSkillSHA_BrokenSymlinkErrors(t *testing.T) {
	dir := t.TempDir()
	dangling := filepath.Join(dir, "dangling.md")
	if err := os.Symlink("/nonexistent/path/that/does/not/exist", dangling); err != nil {
		t.Fatalf("create dangling symlink: %v", err)
	}

	_, err := ComputeSkillSHA(dir)
	if err == nil {
		t.Error("expected error for dangling symlink, got nil")
	}
	if !strings.Contains(err.Error(), "broken symlink") {
		t.Errorf("expected 'broken symlink' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// LoadManifest tests
// ---------------------------------------------------------------------------

func TestManifestLoadManifest_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".deploy-manifest.json")

	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}
	if m.Version != 1 {
		t.Errorf("Version: got %d, want 1", m.Version)
	}
	if m.Skills == nil {
		t.Error("Skills map must not be nil")
	}
	if len(m.Skills) != 0 {
		t.Errorf("Skills map must be empty, got %v", m.Skills)
	}
}

// ---------------------------------------------------------------------------
// SaveManifest tests
// ---------------------------------------------------------------------------

func TestManifestSaveManifest_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".deploy-manifest.json")

	original := Manifest{
		Version: 1,
		Skills: map[string]string{
			"commit": "abcdef1234567890",
			"plan":   "0987654321fedcba",
		},
	}

	if err := SaveManifest(path, original); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	if loaded.Version != original.Version {
		t.Errorf("Version: got %d, want %d", loaded.Version, original.Version)
	}
	if len(loaded.Skills) != len(original.Skills) {
		t.Errorf("Skills count: got %d, want %d", len(loaded.Skills), len(original.Skills))
	}
	for k, v := range original.Skills {
		if loaded.Skills[k] != v {
			t.Errorf("Skills[%q]: got %q, want %q", k, loaded.Skills[k], v)
		}
	}
}

func TestManifestSaveManifest_Atomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".deploy-manifest.json")

	m := Manifest{
		Version: 1,
		Skills:  map[string]string{"skill-a": "sha123"},
	}

	if err := SaveManifest(path, m); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// The final file must exist at the target path.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("manifest file not found at path: %v", err)
	}

	// No temp file must remain in the same directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("stale temp file found after SaveManifest: %s", e.Name())
		}
	}

	// Verify the written file is valid JSON (atomic write must not corrupt).
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var check Manifest
	if err := json.Unmarshal(raw, &check); err != nil {
		t.Errorf("written file is not valid JSON: %v", err)
	}
}
