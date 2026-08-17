package cmd

import (
	"os"
	"strings"
	"testing"
)

// configFixture writes a .bravros.yml in the current directory with the given YAML content.
func configFixture(t *testing.T, yaml string) {
	t.Helper()
	if err := os.WriteFile(".bravros.yml", []byte(yaml), 0o644); err != nil {
		t.Fatalf("write .bravros.yml: %v", err)
	}
	t.Cleanup(func() { os.Remove(".bravros.yml") })
}

func TestConfigMergeStrategy_ByBase_Hit_Cmd(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)
	configFixture(t, "merge_strategy:\n  by_base:\n    main: merge\n")

	out := captureStdout(t, func() {
		// reset flag to default before each run
		mergeStrategyBase = "main"
		mergeStrategyCmd.RunE(mergeStrategyCmd, nil) //nolint:errcheck
	})

	got := strings.TrimSpace(out)
	if got != "merge" {
		t.Fatalf("expected 'merge', got %q", got)
	}
}

func TestConfigMergeStrategy_IntoMain_Hit_Cmd(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)
	configFixture(t, "merge_strategy:\n  into_main: rebase\n")

	out := captureStdout(t, func() {
		mergeStrategyBase = "main"
		mergeStrategyCmd.RunE(mergeStrategyCmd, nil) //nolint:errcheck
	})

	got := strings.TrimSpace(out)
	if got != "rebase" {
		t.Fatalf("expected 'rebase', got %q", got)
	}
}

func TestConfigMergeStrategy_Default_Hit_Cmd(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)
	configFixture(t, "merge_strategy:\n  default: merge\n")

	out := captureStdout(t, func() {
		mergeStrategyBase = "homolog"
		mergeStrategyCmd.RunE(mergeStrategyCmd, nil) //nolint:errcheck
	})

	got := strings.TrimSpace(out)
	if got != "merge" {
		t.Fatalf("expected 'merge', got %q", got)
	}
}

// TestConfigMergeStrategy_NoBlock_FallbackMerge — config present but no merge_strategy block;
// expects "merge" (B-0200: changed fallback from "squash" to "merge").
func TestConfigMergeStrategy_NoBlock_FallbackMerge_Cmd(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)
	configFixture(t, "staging_branch: homolog\n")

	out := captureStdout(t, func() {
		mergeStrategyBase = "main"
		mergeStrategyCmd.RunE(mergeStrategyCmd, nil) //nolint:errcheck
	})

	got := strings.TrimSpace(out)
	if got != "merge" {
		t.Fatalf("expected 'merge' (B-0200 new default), got %q", got)
	}
}

// TestConfigMergeStrategy_NoConfigFile_FallbackMerge — no config file at all;
// expects "merge" (B-0200: changed fallback from "squash" to "merge").
func TestConfigMergeStrategy_NoConfigFile_FallbackMerge_Cmd(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)
	// no config file written

	out := captureStdout(t, func() {
		mergeStrategyBase = "main"
		mergeStrategyCmd.RunE(mergeStrategyCmd, nil) //nolint:errcheck
	})

	got := strings.TrimSpace(out)
	if got != "merge" {
		t.Fatalf("expected 'merge' (B-0200 new default), got %q", got)
	}
}

// TestConfigGet_SkillsPreserve verifies that `config get skills.preserve` prints
// the space-separated list from .bravros.yml.
func TestConfigGet_SkillsPreserve(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)
	configFixture(t, "skills:\n  preserve:\n    - graphify\n    - myplugin\n")

	out := captureStdout(t, func() {
		configGetCmd.RunE(configGetCmd, []string{"skills.preserve"}) //nolint:errcheck
	})

	got := strings.TrimSpace(out)
	// dedup sorts, so output is sorted
	if got != "graphify myplugin" {
		t.Fatalf("expected 'graphify myplugin', got %q", got)
	}
}

// TestConfigGet_SkillsPreserve_Empty verifies that an unset preserve list produces no output.
func TestConfigGet_SkillsPreserve_Empty(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)
	// no skills.preserve in config

	out := captureStdout(t, func() {
		configGetCmd.RunE(configGetCmd, []string{"skills.preserve"}) //nolint:errcheck
	})

	got := strings.TrimSpace(out)
	if got != "" {
		t.Fatalf("expected empty output, got %q", got)
	}
}

// TestConfigGet_StagingBranch_FromLegacyYAML verifies that `config get staging_branch`
// prints the configured branch (legacy .bravros.yml is still honoured by the loader).
func TestConfigGet_StagingBranch_FromLegacyYAML(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)
	configFixture(t, "staging_branch: staging\n")

	out := captureStdout(t, func() {
		configGetCmd.RunE(configGetCmd, []string{"staging_branch"}) //nolint:errcheck
	})

	if out != "staging\n" {
		t.Fatalf("expected \"staging\\n\", got %q", out)
	}
}

// TestConfigGet_StagingBranch_DefaultsToHomolog verifies the "homolog" default
// when the project has no config file at all.
func TestConfigGet_StagingBranch_DefaultsToHomolog(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)
	// no config file written

	out := captureStdout(t, func() {
		configGetCmd.RunE(configGetCmd, []string{"staging_branch"}) //nolint:errcheck
	})

	if out != "homolog\n" {
		t.Fatalf("expected \"homolog\\n\", got %q", out)
	}
}

// TestConfigGet_StagingBranch_FromJSONConfig verifies the new .bravros/config.json
// path wins and its value is printed verbatim.
func TestConfigGet_StagingBranch_FromJSONConfig(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)
	if err := os.MkdirAll(".bravros", 0o755); err != nil {
		t.Fatalf("mkdir .bravros: %v", err)
	}
	if err := os.WriteFile(".bravros/config.json", []byte(`{"staging_branch": "develop"}`), 0o644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	out := captureStdout(t, func() {
		configGetCmd.RunE(configGetCmd, []string{"staging_branch"}) //nolint:errcheck
	})

	if out != "develop\n" {
		t.Fatalf("expected \"develop\\n\", got %q", out)
	}
}

// TestConfigGet_UnknownKey verifies that an unknown key returns an error.
func TestConfigGet_UnknownKey(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)

	err := configGetCmd.RunE(configGetCmd, []string{"unknown.key"})
	if err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
	if !strings.Contains(err.Error(), "unknown config key") {
		t.Fatalf("expected 'unknown config key' in error, got %q", err.Error())
	}
}
