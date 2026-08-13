package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSymlinkDeploy_SymlinkMode(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Create source dirs.
	for _, sub := range SymlinkDirs {
		os.MkdirAll(filepath.Join(src, sub), 0755)
		os.WriteFile(filepath.Join(src, sub, "file.txt"), []byte("data"), 0644)
	}

	result, err := SymlinkDeploy(src, dst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Fallback {
		t.Skip("filesystem does not support symlinks — skipping symlink assertions")
	}

	if result.Mode != "symlinks" {
		t.Errorf("expected mode symlinks, got %s", result.Mode)
	}

	// Verify symlinks exist.
	for _, sub := range SymlinkDirs {
		link := filepath.Join(dst, sub)
		target, err := os.Readlink(link)
		if err != nil {
			t.Errorf("expected symlink at %s: %v", link, err)
			continue
		}
		if target != filepath.Join(src, sub) {
			t.Errorf("symlink %s → %s, want %s", link, target, filepath.Join(src, sub))
		}
	}
}

func TestSymlinkDeploy_Idempotent(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	os.MkdirAll(filepath.Join(src, "skills"), 0755)

	result1, err := SymlinkDeploy(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if result1.Fallback {
		t.Skip("filesystem does not support symlinks")
	}

	// Second run should be a no-op.
	result2, err := SymlinkDeploy(src, dst)
	if err != nil {
		t.Fatalf("second run error: %v", err)
	}

	if result2.Mode != "already-symlinked" {
		t.Errorf("expected already-symlinked on second run, got %s", result2.Mode)
	}
}

func TestSymlinkDeploy_NonEmptyDstError(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	os.MkdirAll(filepath.Join(src, "skills"), 0755)
	// Create a non-symlink directory at dst/skills.
	os.MkdirAll(filepath.Join(dst, "skills"), 0755)
	os.WriteFile(filepath.Join(dst, "skills", "existing.txt"), []byte("x"), 0644)

	_, err := SymlinkDeploy(src, dst)
	if err == nil {
		t.Error("expected error when dst/skills is a non-symlink directory")
	}
}

func TestIsSymlinkDeployed_True(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	for _, sub := range SymlinkDirs {
		os.MkdirAll(filepath.Join(src, sub), 0755)
	}

	result, err := SymlinkDeploy(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback {
		t.Skip("filesystem does not support symlinks")
	}

	if !IsSymlinkDeployed(src, dst) {
		t.Error("expected IsSymlinkDeployed to return true after SymlinkDeploy")
	}
}

func TestIsSymlinkDeployed_False(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	if IsSymlinkDeployed(src, dst) {
		t.Error("expected false when no symlinks are present")
	}
}
