package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func writeConfig(t *testing.T, filename string, content string) {
	dir := filepath.Dir(filename)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("MkdirAll %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile %s: %v", filename, err)
	}
}

func TestLoadBravrosConfig_MissingFile(t *testing.T) {
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

	writeConfig(t, ConfigFilename, `{"staging_branch": "staging"}`)

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

	writeConfig(t, ConfigFilename, `{"staging_branch": ""}`)

	cfg, found := LoadBravrosConfig()
	if !found {
		t.Fatal("expected found=true")
	}
	if cfg.StagingBranch != "homolog" {
		t.Fatalf("expected default 'homolog' for empty string, got %q", cfg.StagingBranch)
	}
}

func TestLoadBravrosConfig_LegacyAutoMigrate(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	writeConfig(t, LegacyConfigFilename, "staging_branch: staging\n")

	cfg, found := LoadBravrosConfig()
	if !found {
		t.Fatal("expected found=true when legacy file is present")
	}
	if cfg.StagingBranch != "staging" {
		t.Fatalf("expected 'staging' from legacy file, got %q", cfg.StagingBranch)
	}

	if _, err := os.Stat(ConfigFilename); err != nil {
		t.Fatalf("expected %s to exist after migration: %v", ConfigFilename, err)
	}
	if _, err := os.Stat(LegacyConfigFilename); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be removed after migration, got err=%v", LegacyConfigFilename, err)
	}
}

func TestLoadBravrosConfig_BothFilesNewWins(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	writeConfig(t, ConfigFilename, `{"staging_branch": "homolog"}`)
	writeConfig(t, LegacyConfigFilename, "staging_branch: legacy-value\n")

	cfg, found := LoadBravrosConfig()
	if !found {
		t.Fatal("expected found=true")
	}
	if cfg.StagingBranch != "homolog" {
		t.Fatalf("expected new filename to win ('homolog'), got %q", cfg.StagingBranch)
	}
	if _, err := os.Stat(LegacyConfigFilename); err != nil {
		t.Fatalf("expected legacy file to remain untouched: %v", err)
	}
}

func TestConfigMergeStrategy_ByBase_Hit(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	writeConfig(t, ConfigFilename, `{"merge_strategy": {"by_base": {"main": "merge"}}}`)

	cfg, found := LoadBravrosConfig()
	if !found {
		t.Fatal("expected found=true")
	}
	if cfg.MergeStrategy == nil {
		t.Fatal("expected MergeStrategy to be non-nil")
	}
	got := cfg.MergeStrategy.ByBase["main"]
	if got != "merge" {
		t.Fatalf("expected 'merge', got %q", got)
	}
}

func TestConfigMergeStrategy_IntoMain_Hit(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	writeConfig(t, ConfigFilename, `{"merge_strategy": {"into_main": "rebase"}}`)

	cfg, found := LoadBravrosConfig()
	if !found {
		t.Fatal("expected found=true")
	}
	if cfg.MergeStrategy == nil {
		t.Fatal("expected MergeStrategy to be non-nil")
	}
	if cfg.MergeStrategy.IntoMain != "rebase" {
		t.Fatalf("expected 'rebase', got %q", cfg.MergeStrategy.IntoMain)
	}
}

func TestConfigMergeStrategy_Default_Hit(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	writeConfig(t, ConfigFilename, `{"merge_strategy": {"default": "merge"}}`)

	cfg, found := LoadBravrosConfig()
	if !found {
		t.Fatal("expected found=true")
	}
	if cfg.MergeStrategy == nil {
		t.Fatal("expected MergeStrategy to be non-nil")
	}
	if cfg.MergeStrategy.Default != "merge" {
		t.Fatalf("expected 'merge', got %q", cfg.MergeStrategy.Default)
	}
}

func TestConfigMergeStrategy_NoMergeStrategyBlock_FallbackSquash(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	writeConfig(t, ConfigFilename, `{"staging_branch": "homolog"}`)

	cfg, found := LoadBravrosConfig()
	if !found {
		t.Fatal("expected found=true")
	}
	if cfg.MergeStrategy != nil {
		t.Fatalf("expected MergeStrategy to be nil when block absent, got %+v", cfg.MergeStrategy)
	}
}

func TestConfigMergeStrategy_NoConfigFile_FallbackSquash(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	cfg, found := LoadBravrosConfig()
	if found {
		t.Fatal("expected found=false for missing file")
	}
	if cfg.MergeStrategy != nil {
		t.Fatalf("expected MergeStrategy to be nil when no config file, got %+v", cfg.MergeStrategy)
	}
}

func TestConfigFeatures_ReviewStampGate_True(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	writeConfig(t, ConfigFilename, `{"features": {"review_stamp_gate": true}}`)

	got := ReadFeature("review_stamp_gate")
	if !got {
		t.Fatal("expected ReadFeature('review_stamp_gate') == true")
	}
}

func TestConfigFeatures_Absent_False(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	writeConfig(t, ConfigFilename, `{"staging_branch": "homolog"}`)

	got := ReadFeature("review_stamp_gate")
	if got {
		t.Fatal("expected ReadFeature('review_stamp_gate') == false when features block absent")
	}
}

func TestConfigPermanentBranches_Custom_Respected(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	writeConfig(t, ConfigFilename, `{"permanent_branches": ["main", "release", "prod"]}`)

	got := ReadPermanentBranches()
	want := []string{"main", "release", "prod"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestConfigPermanentBranches_Absent_Fallback(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	writeConfig(t, ConfigFilename, `{"staging_branch": "homolog"}`)

	got := ReadPermanentBranches()
	want := []string{"main", "homolog", "staging", "develop"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected default fallback %v, got %v", want, got)
	}
}

func TestAuditConfig_RoundTrip(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	writeConfig(t, ConfigFilename, `{"staging_branch": "homolog", "audit": {"disabled_rules": ["6", "19"]}}`)

	cfg, found := LoadBravrosConfig()
	if !found {
		t.Fatal("expected found=true")
	}
	if cfg.StagingBranch != "homolog" {
		t.Errorf("expected staging_branch=homolog, got %q", cfg.StagingBranch)
	}
	if cfg.Audit == nil {
		t.Fatal("expected cfg.Audit to be non-nil")
	}
	wantRules := []string{"6", "19"}
	if !reflect.DeepEqual(cfg.Audit.DisabledRules, wantRules) {
		t.Errorf("disabled_rules = %v, want %v", cfg.Audit.DisabledRules, wantRules)
	}
}

func TestAuditConfig_AbsentBlock(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	writeConfig(t, ConfigFilename, `{"staging_branch": "homolog"}`)

	cfg, found := LoadBravrosConfig()
	if !found {
		t.Fatal("expected found=true")
	}
	if cfg.Audit != nil {
		t.Errorf("expected cfg.Audit to be nil when audit block absent, got %+v", cfg.Audit)
	}
}

func TestAuditConfig_EmptyDisabledRules(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	writeConfig(t, ConfigFilename, `{"audit": {"disabled_rules": []}}`)

	cfg, found := LoadBravrosConfig()
	if !found {
		t.Fatal("expected found=true")
	}
	if cfg.Audit == nil {
		t.Fatal("expected cfg.Audit to be non-nil when audit block is present")
	}
	if len(cfg.Audit.DisabledRules) != 0 {
		t.Errorf("expected empty disabled_rules, got %v", cfg.Audit.DisabledRules)
	}
}

func TestLoadBravrosConfig_CacheInvalidatesOnEdit(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	writeConfig(t, ConfigFilename, `{"staging_branch": "alpha"}`)
	cfg, _ := LoadBravrosConfig()
	if cfg.StagingBranch != "alpha" {
		t.Fatalf("first read: expected 'alpha', got %q", cfg.StagingBranch)
	}

	time.Sleep(15 * time.Millisecond)
	writeConfig(t, ConfigFilename, `{"staging_branch": "beta-longer-value"}`)

	cfg2, _ := LoadBravrosConfig()
	if cfg2.StagingBranch != "beta-longer-value" {
		t.Fatalf("post-edit read: expected fresh 'beta-longer-value', got stale %q", cfg2.StagingBranch)
	}
}

func TestLoadBravrosConfig_CacheDistinctCwd(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dirA := t.TempDir()
	dirB := t.TempDir()
	writeConfig(t, filepath.Join(dirA, ConfigFilename), `{"staging_branch": "aaa"}`)
	writeConfig(t, filepath.Join(dirB, ConfigFilename), `{"staging_branch": "bbb"}`)

	os.Chdir(dirA)
	if cfg, _ := LoadBravrosConfig(); cfg.StagingBranch != "aaa" {
		t.Fatalf("dirA: expected 'aaa', got %q", cfg.StagingBranch)
	}
	os.Chdir(dirB)
	if cfg, _ := LoadBravrosConfig(); cfg.StagingBranch != "bbb" {
		t.Fatalf("dirB: expected 'bbb', got %q", cfg.StagingBranch)
	}
	os.Chdir(dirA)
	if cfg, _ := LoadBravrosConfig(); cfg.StagingBranch != "aaa" {
		t.Fatalf("dirA re-read: expected 'aaa', got %q", cfg.StagingBranch)
	}
}

func TestLoadBravrosConfig_CacheCloneIsolation(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	writeConfig(t, ConfigFilename, `{"staging_branch": "keep", "audit": {"disabled_rules": ["6"]}}`)

	cfg1, _ := LoadBravrosConfig()
	cfg1.StagingBranch = "MUTATED"
	if cfg1.Audit != nil && len(cfg1.Audit.DisabledRules) > 0 {
		cfg1.Audit.DisabledRules[0] = "MUTATED"
	}

	cfg2, _ := LoadBravrosConfig()
	if cfg2.StagingBranch != "keep" {
		t.Fatalf("cache poisoned: scalar mutation leaked, got %q", cfg2.StagingBranch)
	}
	if cfg2.Audit == nil || len(cfg2.Audit.DisabledRules) != 1 || cfg2.Audit.DisabledRules[0] != "6" {
		t.Fatalf("cache poisoned: slice mutation leaked, got %+v", cfg2.Audit)
	}
}
