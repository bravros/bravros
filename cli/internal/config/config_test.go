package config

import (
	"os"
	"testing"
)

func TestLoadBravrosConfig_MissingFile(t *testing.T) {
	// Run from temp dir with no .bravros.yml
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	cfg, found := LoadBravrosConfig()
	if found {
		t.Fatal("expected found=false for missing file")
	}
	if cfg.StagingBranch != "homolog" {
		t.Fatalf("expected default 'homolog', got %q", cfg.StagingBranch)
	}
}

func TestLoadBravrosConfig_CustomBranch(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	os.WriteFile(".bravros.yml", []byte("staging_branch: staging\n"), 0644)

	cfg, found := LoadBravrosConfig()
	if !found {
		t.Fatal("expected found=true")
	}
	if cfg.StagingBranch != "staging" {
		t.Fatalf("expected 'staging', got %q", cfg.StagingBranch)
	}
}

func TestLoadBravrosConfig_EmptyDefaults(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	os.WriteFile(".bravros.yml", []byte("staging_branch: \"\"\n"), 0644)

	cfg, found := LoadBravrosConfig()
	if !found {
		t.Fatal("expected found=true")
	}
	if cfg.StagingBranch != "homolog" {
		t.Fatalf("expected default 'homolog' for empty string, got %q", cfg.StagingBranch)
	}
}
