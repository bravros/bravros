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

// TestConfigGet_UnknownKey_GenericEmpty verifies that a dotted key path with
// no config file on disk (and, more generally, any unset generic key) is
// treated as unset — empty output, exit 0 — rather than an error. Generic
// key resolution has no notion of "unknown key" anymore: every non-special
// key is a path to walk.
func TestConfigGet_UnknownKey_GenericEmpty(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)

	err := configGetCmd.RunE(configGetCmd, []string{"unknown.key"})
	if err != nil {
		t.Fatalf("expected nil error for unset generic key, got %v", err)
	}

	out := captureStdout(t, func() {
		configGetCmd.RunE(configGetCmd, []string{"unknown.key"}) //nolint:errcheck
	})
	if out != "" {
		t.Fatalf("expected empty output for unset generic key, got %q", out)
	}
}

// TestConfigGet_Generic_NestedKey verifies a nested dotted key path
// (stack.test_runner) is walked into .bravros/config.json and printed as-is.
func TestConfigGet_Generic_NestedKey(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)
	if err := os.MkdirAll(".bravros", 0o755); err != nil {
		t.Fatalf("mkdir .bravros: %v", err)
	}
	cfg := `{"stack": {"test_runner": "go test", "framework": "go"}}`
	if err := os.WriteFile(".bravros/config.json", []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	out := captureStdout(t, func() {
		configGetCmd.RunE(configGetCmd, []string{"stack.test_runner"}) //nolint:errcheck
	})
	if out != "go test\n" {
		t.Fatalf("expected \"go test\\n\", got %q", out)
	}
}

// TestConfigGet_Generic_Array verifies an array value (permanent_branches)
// prints space-separated, mirroring the skills.preserve special case.
func TestConfigGet_Generic_Array(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)
	if err := os.MkdirAll(".bravros", 0o755); err != nil {
		t.Fatalf("mkdir .bravros: %v", err)
	}
	cfg := `{"permanent_branches": ["main", "homolog"]}`
	if err := os.WriteFile(".bravros/config.json", []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	out := captureStdout(t, func() {
		configGetCmd.RunE(configGetCmd, []string{"permanent_branches"}) //nolint:errcheck
	})
	if out != "main homolog\n" {
		t.Fatalf("expected \"main homolog\\n\", got %q", out)
	}
}

// TestConfigGet_Generic_Object verifies an object value (police) prints as
// compact JSON.
func TestConfigGet_Generic_Object(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)
	if err := os.MkdirAll(".bravros", 0o755); err != nil {
		t.Fatalf("mkdir .bravros: %v", err)
	}
	cfg := `{"police": {"direct_main": true}}`
	if err := os.WriteFile(".bravros/config.json", []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	out := captureStdout(t, func() {
		configGetCmd.RunE(configGetCmd, []string{"police"}) //nolint:errcheck
	})
	if out != `{"direct_main":true}`+"\n" {
		t.Fatalf("expected compact JSON object, got %q", out)
	}
}

// TestConfigGet_Generic_UnsetNestedKey verifies a nested key that doesn't
// exist in an otherwise-present config produces empty output, exit 0 — not
// an error.
func TestConfigGet_Generic_UnsetNestedKey(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)
	if err := os.MkdirAll(".bravros", 0o755); err != nil {
		t.Fatalf("mkdir .bravros: %v", err)
	}
	cfg := `{"staging_branch": "homolog"}`
	if err := os.WriteFile(".bravros/config.json", []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	err := configGetCmd.RunE(configGetCmd, []string{"stack.test_runner"})
	if err != nil {
		t.Fatalf("expected nil error for unset nested key, got %v", err)
	}
	out := captureStdout(t, func() {
		configGetCmd.RunE(configGetCmd, []string{"stack.test_runner"}) //nolint:errcheck
	})
	if out != "" {
		t.Fatalf("expected empty output for unset nested key, got %q", out)
	}
}

// TestConfigGet_Generic_FromLegacyYAML verifies the generic path also
// respects the legacy .bravros.yml fallback for a key with no special case.
func TestConfigGet_Generic_FromLegacyYAML(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)
	configFixture(t, "permanent_branches:\n  - main\n  - homolog\n")

	out := captureStdout(t, func() {
		configGetCmd.RunE(configGetCmd, []string{"permanent_branches"}) //nolint:errcheck
	})
	if out != "main homolog\n" {
		t.Fatalf("expected \"main homolog\\n\", got %q", out)
	}
}

// TestConfigGet_Generic_CorruptJSON_WarnsAndFails pins the difference between
// "unset" and "broken": a .bravros/config.json that exists but does not parse
// must NOT read as an empty value (exit 0) — a script consuming
// `$(bravros config get x)` would then silently fall back to its default for
// every key. It gets a warning naming the file on stderr and a non-zero exit.
func TestConfigGet_Generic_CorruptJSON_WarnsAndFails(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)
	if err := os.MkdirAll(".bravros", 0o755); err != nil {
		t.Fatalf("mkdir .bravros: %v", err)
	}
	if err := os.WriteFile(".bravros/config.json", []byte(`{"stack": {"test_runner": "go test"`), 0o644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	var err error
	var out string
	stderr := captureStderr(t, func() {
		out = captureStdout(t, func() {
			err = configGetCmd.RunE(configGetCmd, []string{"stack.test_runner"})
		})
	})
	if err == nil {
		t.Fatal("expected a non-nil error for a corrupt config.json, got nil")
	}
	if out != "" {
		t.Errorf("a corrupt config must print nothing on stdout, got %q", out)
	}
	if !strings.HasPrefix(stderr, "warning: .bravros/config.json is not valid JSON: ") {
		t.Errorf("stderr must start with the canonical warning, got %q", stderr)
	}
	// The warning is the whole message: cobra must not print it again as
	// "Error: …" nor dump usage — both are silenced on configGetCmd.
	if !configGetCmd.SilenceErrors || !configGetCmd.SilenceUsage {
		t.Error("configGetCmd must set SilenceErrors and SilenceUsage so the warning is printed exactly once")
	}
}

// TestConfigGet_Generic_CorruptLegacyYAML_WarnsAndFails — the legacy fallback
// gets the same treatment: a .bravros.yml that fails to parse is a warning +
// exit 1, not an empty value.
func TestConfigGet_Generic_CorruptLegacyYAML_WarnsAndFails(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)
	configFixture(t, "permanent_branches: [main, homolog\n")

	var err error
	stderr := captureStderr(t, func() {
		captureStdout(t, func() {
			err = configGetCmd.RunE(configGetCmd, []string{"permanent_branches"})
		})
	})
	if err == nil {
		t.Fatal("expected a non-nil error for a corrupt .bravros.yml, got nil")
	}
	if !strings.HasPrefix(stderr, "warning: .bravros.yml is not valid YAML: ") {
		t.Errorf("stderr must start with the canonical warning, got %q", stderr)
	}
}

// TestConfigGet_Generic_MissingConfig_StaysSilent guards the one exit-0 path
// that must survive the corrupt-file change: no config on disk at all is still
// "unset", empty output, nil error.
func TestConfigGet_Generic_MissingConfig_StaysSilent(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)

	var err error
	stderr := captureStderr(t, func() {
		captureStdout(t, func() {
			err = configGetCmd.RunE(configGetCmd, []string{"stack.test_runner"})
		})
	})
	if err != nil {
		t.Fatalf("missing config must be exit 0, got %v", err)
	}
	if stderr != "" {
		t.Errorf("missing config must print no warning, got %q", stderr)
	}
}
