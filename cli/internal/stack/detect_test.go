package stack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bravros/bravros/cli/internal/config"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestDetect_LaravelProject(t *testing.T) {
	dir := setupTestDir(t)
	writeFile(t, filepath.Join(dir, "composer.json"), `{
		"require": {"laravel/framework": "^11.0"},
		"require-dev": {"pestphp/pest": "^2.0"}
	}`)
	writeFile(t, filepath.Join(dir, "package.json"), `{"devDependencies": {"vite": "^5.0"}}`)

	result, err := Detect(dir, DetectOpts{SkipGit: true})
	if err != nil {
		t.Fatal(err)
	}

	if result.Monorepo {
		t.Error("expected single-stack, got monorepo")
	}
	if result.Stack.Language != "php" {
		t.Errorf("expected language=php, got %s", result.Stack.Language)
	}
	if result.Stack.Framework != "laravel" {
		t.Errorf("expected framework=laravel, got %s", result.Stack.Framework)
	}
	if result.Stack.TestRunner != "pest" {
		t.Errorf("expected test_runner=pest, got %s", result.Stack.TestRunner)
	}
	if !result.Stack.HasAssets {
		t.Error("expected has_assets=true (package.json exists)")
	}
}

func TestDetect_NextjsProject(t *testing.T) {
	dir := setupTestDir(t)
	writeFile(t, filepath.Join(dir, "package.json"), `{
		"dependencies": {"next": "^14.0", "react": "^18.0"},
		"devDependencies": {"jest": "^29.0"}
	}`)

	result, err := Detect(dir, DetectOpts{SkipGit: true})
	if err != nil {
		t.Fatal(err)
	}

	if result.Stack.Language != "node" {
		t.Errorf("expected language=node, got %s", result.Stack.Language)
	}
	if result.Stack.Framework != "nextjs" {
		t.Errorf("expected framework=nextjs, got %s", result.Stack.Framework)
	}
	if result.Stack.TestRunner != "jest" {
		t.Errorf("expected test_runner=jest, got %s", result.Stack.TestRunner)
	}
}

func TestDetect_GoProject(t *testing.T) {
	dir := setupTestDir(t)
	writeFile(t, filepath.Join(dir, "go.mod"), `module example.com/myapp

go 1.22.0

require github.com/spf13/cobra v1.8.0
`)

	result, err := Detect(dir, DetectOpts{SkipGit: true})
	if err != nil {
		t.Fatal(err)
	}

	if result.Stack.Language != "go" {
		t.Errorf("expected language=go, got %s", result.Stack.Language)
	}
	if result.Stack.Framework != "none" {
		t.Errorf("expected framework=none, got %s", result.Stack.Framework)
	}
	if result.Stack.TestRunner != "go test" {
		t.Errorf("expected test_runner='go test', got %s", result.Stack.TestRunner)
	}
}

func TestDetect_PythonProject(t *testing.T) {
	dir := setupTestDir(t)
	writeFile(t, filepath.Join(dir, "requirements.txt"), `django==4.2.0
pytest==7.4.0
`)

	result, err := Detect(dir, DetectOpts{SkipGit: true})
	if err != nil {
		t.Fatal(err)
	}

	if result.Stack.Language != "python" {
		t.Errorf("expected language=python, got %s", result.Stack.Language)
	}
	if result.Stack.Framework != "django" {
		t.Errorf("expected framework=django, got %s", result.Stack.Framework)
	}
	if result.Stack.TestRunner != "pytest" {
		t.Errorf("expected test_runner=pytest, got %s", result.Stack.TestRunner)
	}
}

func TestDetect_EmptyProject(t *testing.T) {
	dir := setupTestDir(t)

	result, err := Detect(dir, DetectOpts{SkipGit: true})
	if err != nil {
		t.Fatal(err)
	}

	if result.Monorepo {
		t.Error("expected not monorepo")
	}
	if result.Stack.Language != "" {
		t.Errorf("expected empty language, got %s", result.Stack.Language)
	}
}

func TestDetect_Monorepo(t *testing.T) {
	dir := setupTestDir(t)

	// apps/api → Laravel
	writeFile(t, filepath.Join(dir, "apps", "api", "composer.json"), `{
		"require": {"laravel/framework": "^11.0"},
		"require-dev": {"pestphp/pest": "^2.0"}
	}`)

	// apps/mobile → Expo
	writeFile(t, filepath.Join(dir, "apps", "mobile", "package.json"), `{
		"dependencies": {"expo": "^51.0", "react": "^18.0"},
		"devDependencies": {"jest": "^29.0"}
	}`)

	result, err := Detect(dir, DetectOpts{SkipGit: true})
	if err != nil {
		t.Fatal(err)
	}

	if !result.Monorepo {
		t.Error("expected monorepo=true")
	}

	apiStack, ok := result.Stacks["apps/api"]
	if !ok {
		t.Fatal("expected stacks to contain apps/api")
	}
	if apiStack.Language != "php" || apiStack.Framework != "laravel" {
		t.Errorf("expected apps/api to be php/laravel, got %s/%s", apiStack.Language, apiStack.Framework)
	}

	mobileStack, ok := result.Stacks["apps/mobile"]
	if !ok {
		t.Fatal("expected stacks to contain apps/mobile")
	}
	if mobileStack.Language != "node" || mobileStack.Framework != "expo" {
		t.Errorf("expected apps/mobile to be node/expo, got %s/%s", mobileStack.Language, mobileStack.Framework)
	}
}

func TestDetect_WriteCreatesConfig(t *testing.T) {
	dir := setupTestDir(t)
	writeFile(t, filepath.Join(dir, "go.mod"), `module example.com/app

go 1.22.0
`)

	result, err := Detect(dir, DetectOpts{SkipGit: true})
	if err != nil {
		t.Fatal(err)
	}

	if err := WriteConfig(dir, result); err != nil {
		t.Fatal(err)
	}

	// Read back and verify
	data, err := os.ReadFile(filepath.Join(dir, config.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}

	var cfg config.BravrosConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}

	if cfg.StagingBranch != "homolog" {
		t.Errorf("expected staging_branch=homolog, got %s", cfg.StagingBranch)
	}
	if cfg.Language != "auto" {
		t.Errorf("expected language=auto, got %s", cfg.Language)
	}
	if cfg.Stack.Language != "go" {
		t.Errorf("expected stack.language=go, got %s", cfg.Stack.Language)
	}
}

func TestDetect_WritePreservesExistingFields(t *testing.T) {
	dir := setupTestDir(t)

	// Write existing config with user-set fields in JSON format
	existing := `{
  "staging_branch": "develop",
  "language": "pt-BR",
  "env": {
    "deployed": true,
    "production": "https://app.example.com"
  }
}`
	cfgPath := filepath.Join(dir, config.ConfigFilename)
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0755)
	writeFile(t, cfgPath, existing)
	writeFile(t, filepath.Join(dir, "go.mod"), `module example.com/app

go 1.22.0
`)

	result, err := Detect(dir, DetectOpts{SkipGit: true})
	if err != nil {
		t.Fatal(err)
	}

	if err := WriteConfig(dir, result); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, config.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}

	var cfg config.BravrosConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}

	// User-set fields preserved
	if cfg.StagingBranch != "develop" {
		t.Errorf("expected staging_branch=develop (preserved), got %s", cfg.StagingBranch)
	}
	if cfg.Language != "pt-BR" {
		t.Errorf("expected language=pt-BR (preserved), got %s", cfg.Language)
	}
	if !cfg.Env.Deployed {
		t.Error("expected env.deployed=true (preserved)")
	}
	if cfg.Env.Production != "https://app.example.com" {
		t.Errorf("expected env.production preserved, got %s", cfg.Env.Production)
	}

	// Detected fields updated
	if cfg.Stack.Language != "go" {
		t.Errorf("expected stack.language=go (detected), got %s", cfg.Stack.Language)
	}
}

func TestDetect_WithVersions(t *testing.T) {
	dir := setupTestDir(t)
	writeFile(t, filepath.Join(dir, "go.mod"), `module example.com/app

go 1.22.0
`)

	result, err := Detect(dir, DetectOpts{SkipGit: true, Versions: true})
	if err != nil {
		t.Fatal(err)
	}

	if result.Versions == nil {
		t.Fatal("expected versions map, got nil")
	}
	if v, ok := result.Versions["go"]; !ok || v != "1.22.0" {
		t.Errorf("expected versions[go]=1.22.0, got %s (ok=%v)", v, ok)
	}
}

func TestDetect_RustProject(t *testing.T) {
	dir := setupTestDir(t)
	writeFile(t, filepath.Join(dir, "Cargo.toml"), `[package]
name = "myapp"
version = "0.1.0"
`)

	result, err := Detect(dir, DetectOpts{SkipGit: true})
	if err != nil {
		t.Fatal(err)
	}

	if result.Stack.Language != "rust" {
		t.Errorf("expected language=rust, got %s", result.Stack.Language)
	}
	if result.Stack.TestRunner != "cargo test" {
		t.Errorf("expected test_runner='cargo test', got %s", result.Stack.TestRunner)
	}
}

func TestDetect_GitDetection(t *testing.T) {
	dir := setupTestDir(t)
	writeFile(t, filepath.Join(dir, "go.mod"), `module example.com/app

go 1.22.0
`)

	// Create a fake .git/config
	gitConfig := `[core]
	repositoryformatversion = 0
[remote "origin"]
	url = git@github.com:user/repo.git
	fetch = +refs/heads/*:refs/remotes/origin/*
[branch "main"]
	remote = origin
`
	writeFile(t, filepath.Join(dir, ".git", "config"), gitConfig)

	// Create a fake workflow file
	writeFile(t, filepath.Join(dir, ".github", "workflows", "tests.yml"), `name: Tests`)

	result, err := Detect(dir, DetectOpts{SkipGit: false})
	if err != nil {
		t.Fatal(err)
	}

	if result.Git.Remote != "git@github.com:user/repo.git" {
		t.Errorf("expected remote, got %s", result.Git.Remote)
	}
	if !result.Git.HasCI {
		t.Error("expected has_ci=true")
	}
	if result.Git.CIWorkflow != "tests.yml" {
		t.Errorf("expected ci_workflow=tests.yml, got %s", result.Git.CIWorkflow)
	}
}

// ---------------------------------------------------------------------------
// Phase 1: subdir-aware single-stack detection
// ---------------------------------------------------------------------------

func TestDetect_SubdirGoCliProject(t *testing.T) {
	dir := setupTestDir(t)
	// Repo root has no marker files; code lives in cli/ subdir.
	writeFile(t, filepath.Join(dir, "cli", "go.mod"), `module example.com/myapp

go 1.22.0

require github.com/spf13/cobra v1.8.0
`)

	result, err := Detect(dir, DetectOpts{SkipGit: true})
	if err != nil {
		t.Fatal(err)
	}

	if result.Monorepo {
		t.Error("expected single-stack, got monorepo")
	}
	if result.Stack.Language != "go" {
		t.Errorf("expected language=go, got %q", result.Stack.Language)
	}
	if result.Stack.Framework != "none" {
		t.Errorf("expected framework=none, got %q", result.Stack.Framework)
	}
	if result.Stack.TestRunner != "go test" {
		t.Errorf("expected test_runner='go test', got %q", result.Stack.TestRunner)
	}
}

func TestDetect_SubdirNodeServerProject(t *testing.T) {
	dir := setupTestDir(t)
	// Code lives in server/ subdir.
	writeFile(t, filepath.Join(dir, "server", "package.json"), `{
		"dependencies": {"express": "^4.18"},
		"devDependencies": {"jest": "^29.0"}
	}`)

	result, err := Detect(dir, DetectOpts{SkipGit: true})
	if err != nil {
		t.Fatal(err)
	}

	if result.Monorepo {
		t.Error("expected single-stack, got monorepo")
	}
	if result.Stack.Language != "node" {
		t.Errorf("expected language=node, got %q", result.Stack.Language)
	}
	if result.Stack.Framework != "express" {
		t.Errorf("expected framework=express, got %q", result.Stack.Framework)
	}
}

func TestDetect_MultiSubdirBecomesMonorepo(t *testing.T) {
	dir := setupTestDir(t)
	// cli/ → Go, frontend/ → Node — two distinct stacks → monorepo.
	writeFile(t, filepath.Join(dir, "cli", "go.mod"), `module example.com/cli

go 1.22.0
`)
	writeFile(t, filepath.Join(dir, "frontend", "package.json"), `{
		"dependencies": {"react": "^18.0"},
		"devDependencies": {"jest": "^29.0"}
	}`)

	result, err := Detect(dir, DetectOpts{SkipGit: true})
	if err != nil {
		t.Fatal(err)
	}

	if !result.Monorepo {
		t.Error("expected monorepo=true when multiple subdirs match")
	}
	if _, ok := result.Stacks["cli"]; !ok {
		t.Error("expected stacks to contain 'cli'")
	}
	if _, ok := result.Stacks["frontend"]; !ok {
		t.Error("expected stacks to contain 'frontend'")
	}
}

func TestDetect_EmptyRepoNoFile(t *testing.T) {
	dir := setupTestDir(t)
	// Completely empty repo — no marker files anywhere.
	result, err := Detect(dir, DetectOpts{SkipGit: true})
	if err != nil {
		t.Fatal(err)
	}

	if result.Monorepo {
		t.Error("expected monorepo=false for empty repo")
	}
	if result.Stack.Language != "" {
		t.Errorf("expected empty language for empty repo, got %q", result.Stack.Language)
	}
	if len(result.Stacks) != 0 {
		t.Errorf("expected no stacks for empty repo, got %v", result.Stacks)
	}
}

func TestDetect_AppsPackagesMonorepoStillWorks(t *testing.T) {
	// Regression: existing apps/+packages/ layout must still detect as monorepo
	// and not be eclipsed by the new subdir fallback.
	dir := setupTestDir(t)

	writeFile(t, filepath.Join(dir, "apps", "api", "composer.json"), `{
		"require": {"laravel/framework": "^11.0"}
	}`)
	writeFile(t, filepath.Join(dir, "apps", "mobile", "package.json"), `{
		"dependencies": {"expo": "^51.0"}
	}`)

	result, err := Detect(dir, DetectOpts{SkipGit: true})
	if err != nil {
		t.Fatal(err)
	}

	if !result.Monorepo {
		t.Error("expected monorepo=true for apps/ layout")
	}
	if _, ok := result.Stacks["apps/api"]; !ok {
		t.Error("expected stacks key 'apps/api'")
	}
	if _, ok := result.Stacks["apps/mobile"]; !ok {
		t.Error("expected stacks key 'apps/mobile'")
	}
}

// ---------------------------------------------------------------------------
// Phase 2: omitempty + skip-write-when-empty
// ---------------------------------------------------------------------------

func TestWriteConfig_EmptyDetection_NoFile_DoesNotCreate(t *testing.T) {
	dir := setupTestDir(t)
	// Empty detection (no stack), no prior .kaisser.yml → must NOT create the file.
	result := &DetectResult{}

	if err := WriteConfig(dir, result); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(dir, config.ConfigFilename)
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Error("expected WriteConfig NOT to create .kaisser.yml for empty detection with no prior file")
	}
}

func TestWriteConfig_EmptyDetection_ExistingFile_Preserved(t *testing.T) {
	dir := setupTestDir(t)
	// A .kaisser.yml already exists (manually edited). Empty detection must not
	// overwrite it with a blank file, but the file itself should be preserved.
	writeFile(t, filepath.Join(dir, config.ConfigFilename), `staging_branch: custom
permanent_branches:
  - main
  - release
`)

	result := &DetectResult{}
	if err := WriteConfig(dir, result); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, config.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(data), "staging_branch") {
		t.Error("expected existing .kaisser.yml content to be preserved when detection is empty")
	}
}

func TestWriteConfig_ZeroStackConfig_NoEmptyFields(t *testing.T) {
	dir := setupTestDir(t)
	// A detected Go project must serialize without empty string fields in the stack block.
	writeFile(t, filepath.Join(dir, "go.mod"), `module example.com/app

go 1.22.0
`)
	result, err := Detect(dir, DetectOpts{SkipGit: true})
	if err != nil {
		t.Fatal(err)
	}

	if err := WriteConfig(dir, result); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, config.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	// Verify no empty-string YAML values leak through.
	for _, badPattern := range []string{
		`framework: ""`,
		`language_version: ""`,
		`project_type: ""`,
		`language: ""`,
	} {
		if contains(content, badPattern) {
			t.Errorf("expected no empty-string field %q in YAML output:\n%s", badPattern, content)
		}
	}
}

// contains is a substring check helper (kept as a one-liner over strings.Contains
// to preserve a stable call shape across the test file).
func contains(s, sub string) bool { return strings.Contains(s, sub) }

// ---------------------------------------------------------------------------
// B-0258 — patch-version churn must NOT bump DetectedAt
// ---------------------------------------------------------------------------

// TestWriteConfig_PatchOnlyDelta_NoDetectedAtBump verifies that two WriteConfig calls
// differing ONLY in Runtime patch version (e.g. "1.22.0" → "1.22.1") do NOT update
// detected_at. This is the core regression guard for B-0258.
//
// The test pre-seeds .kaisser.yml with a known detected_at so it does not depend on
// RFC3339 clock-second granularity.
func TestWriteConfig_PatchOnlyDelta_NoDetectedAtBump(t *testing.T) {
	dir := setupTestDir(t)

	// Seed existing .kaisser.yml with a known detected_at and go 1.22 stack.
	// The seed uses canonicalized (major.minor) versions as Phase 1 now writes them.
	seed := `{
  "staging_branch": "homolog",
  "language": "auto",
  "stack": {
    "language": "go",
    "framework": "none",
    "test_runner": "go test",
    "language_version": "1.22",
    "runtime": {
      "go": "1.22"
    }
  },
  "detected_at": "2020-01-01T00:00:00Z"
}`
	writeFile(t, filepath.Join(dir, config.ConfigFilename), seed)
	const seedDetectedAt = "2020-01-01T00:00:00Z"

	// Write with only patch changed (1.22.0 → 1.22.1) — detected_at must NOT change.
	result := &DetectResult{
		Stack: config.StackConfig{
			Language:        "go",
			Framework:       "none",
			TestRunner:      "go test",
			LanguageVersion: "1.22.1",
			Runtime:         map[string]string{"go": "1.22.1"},
		},
	}
	if err := WriteConfig(dir, result); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, config.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}
	var cfg config.BravrosConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}

	if cfg.DetectedAt != seedDetectedAt {
		t.Errorf("patch-only Runtime/LanguageVersion delta must NOT bump detected_at\n  seeded:  %s\n  got:     %s", seedDetectedAt, cfg.DetectedAt)
	}
}

// TestWriteConfig_MinorBump_DoesDetectedAtBump verifies that a minor-version bump
// (e.g. "1.26.2" → "1.27.0") DOES update detected_at.
//
// To avoid a 1-second clock-granularity race (RFC3339 truncates at seconds), the
// test pre-seeds .kaisser.yml with a known detected_at far in the past, then
// asserts the second WriteConfig call changes it.
func TestWriteConfig_MinorBump_DoesDetectedAtBump(t *testing.T) {
	dir := setupTestDir(t)

	// Seed existing .kaisser.yml with an old detected_at and go 1.26 stack.
	// The seed uses canonicalized (major.minor) versions as Phase 1 now writes them.
	seed := `{
  "staging_branch": "homolog",
  "language": "auto",
  "stack": {
    "language": "go",
    "framework": "none",
    "test_runner": "go test",
    "language_version": "1.26",
    "runtime": {
      "go": "1.26"
    }
  },
  "detected_at": "2020-01-01T00:00:00Z"
}`
	writeFile(t, filepath.Join(dir, config.ConfigFilename), seed)
	const seedDetectedAt = "2020-01-01T00:00:00Z"

	// Write with a minor bump (1.26.2 → 1.27.0) — detected_at MUST change.
	result := &DetectResult{
		Stack: config.StackConfig{
			Language:        "go",
			Framework:       "none",
			TestRunner:      "go test",
			LanguageVersion: "1.27.0",
			Runtime:         map[string]string{"go": "1.27.0"},
		},
	}
	if err := WriteConfig(dir, result); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, config.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}
	var cfg config.BravrosConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}

	if cfg.DetectedAt == seedDetectedAt {
		t.Errorf("minor-version bump (1.26→1.27) MUST bump detected_at, but it was unchanged: %s", cfg.DetectedAt)
	}
	if cfg.DetectedAt == "" {
		t.Error("expected detected_at to be non-empty after minor bump write")
	}
}

// TestWriteConfig_PatchVersionStableAcrossRuns verifies that multiple WriteConfig calls
// with different patch versions on the same major.minor produce byte-identical on-disk
// output. This is the core regression guard for P-0145 (B-0258 followup): the on-disk
// .kaisser.yml must not churn when only the patch component changes.
func TestWriteConfig_PatchVersionStableAcrossRuns(t *testing.T) {
	dir := setupTestDir(t)

	// Seed existing .kaisser.yml with canonicalized versions and a fixed detected_at.
	seed := `{
  "staging_branch": "homolog",
  "language": "auto",
  "stack": {
    "language": "go",
    "framework": "none",
    "test_runner": "go test",
    "language_version": "1.26",
    "runtime": {
      "go": "1.26"
    }
  },
  "detected_at": "2020-01-01T00:00:00Z"
}`
	writeFile(t, filepath.Join(dir, config.ConfigFilename), seed)
	const seedDetectedAt = "2020-01-01T00:00:00Z"

	base := config.StackConfig{
		Language:   "go",
		Framework:  "none",
		TestRunner: "go test",
	}

	// First write: incoming result carries 1.26.2 (same major.minor as seeded 1.26).
	r1 := &DetectResult{Stack: base}
	r1.Stack.LanguageVersion = "1.26.2"
	r1.Stack.Runtime = map[string]string{"go": "1.26.2"}
	if err := WriteConfig(dir, r1); err != nil {
		t.Fatal(err)
	}
	data1, err := os.ReadFile(filepath.Join(dir, config.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}

	// Second write: incoming result carries 1.26.0 (different patch, same major.minor).
	r2 := &DetectResult{Stack: base}
	r2.Stack.LanguageVersion = "1.26.0"
	r2.Stack.Runtime = map[string]string{"go": "1.26.0"}
	if err := WriteConfig(dir, r2); err != nil {
		t.Fatal(err)
	}
	data2, err := os.ReadFile(filepath.Join(dir, config.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}

	// (a) File content must be byte-identical between the two writes.
	if string(data1) != string(data2) {
		t.Errorf("patch-only version change produced different on-disk content:\n--- write1 ---\n%s\n--- write2 ---\n%s", data1, data2)
	}

	// Unmarshal for field-level assertions.
	var cfg config.BravrosConfig
	if err := json.Unmarshal(data2, &cfg); err != nil {
		t.Fatal(err)
	}

	// (b) language_version must be major.minor only — no patch component.
	if cfg.Stack.LanguageVersion != "1.26" {
		t.Errorf("expected language_version=1.26 (no patch), got %q", cfg.Stack.LanguageVersion)
	}

	// (c) runtime.go must be major.minor only — no patch component.
	if v, ok := cfg.Stack.Runtime["go"]; !ok || v != "1.26" {
		t.Errorf("expected runtime.go=1.26 (no patch), got %q (ok=%v)", v, ok)
	}

	// (d) detected_at must be unchanged across patch-only writes.
	if cfg.DetectedAt != seedDetectedAt {
		t.Errorf("patch-only writes must NOT change detected_at\n  seeded:  %s\n  got:     %s", seedDetectedAt, cfg.DetectedAt)
	}
}

// TestWriteConfig_PatchVersionStableAcrossRuns_Monorepo verifies the same patch-stability
// guarantee for the cfg.Stacks (monorepo) path: multiple WriteConfig calls with different
// patch versions across two stacks must produce byte-identical on-disk output.
func TestWriteConfig_PatchVersionStableAcrossRuns_Monorepo(t *testing.T) {
	dir := setupTestDir(t)

	// Seed existing .kaisser.yml with a monorepo shape and canonicalized versions.
	seed := `{
  "staging_branch": "homolog",
  "language": "auto",
  "monorepo": true,
  "stacks": {
    "apps/api": {
      "language": "go",
      "framework": "none",
      "test_runner": "go test",
      "language_version": "1.26",
      "runtime": {
        "go": "1.26"
      }
    },
    "apps/worker": {
      "language": "go",
      "framework": "none",
      "test_runner": "go test",
      "language_version": "1.22",
      "runtime": {
        "go": "1.22"
      }
    }
  },
  "detected_at": "2020-01-01T00:00:00Z"
}`
	writeFile(t, filepath.Join(dir, config.ConfigFilename), seed)
	const seedDetectedAt = "2020-01-01T00:00:00Z"

	buildResult := func(apiPatch, workerPatch string) *DetectResult {
		return &DetectResult{
			Monorepo: true,
			Stacks: map[string]config.StackConfig{
				"apps/api": {
					Language:        "go",
					Framework:       "none",
					TestRunner:      "go test",
					LanguageVersion: "1.26." + apiPatch,
					Runtime:         map[string]string{"go": "1.26." + apiPatch},
				},
				"apps/worker": {
					Language:        "go",
					Framework:       "none",
					TestRunner:      "go test",
					LanguageVersion: "1.22." + workerPatch,
					Runtime:         map[string]string{"go": "1.22." + workerPatch},
				},
			},
		}
	}

	// First write: api=1.26.2, worker=1.22.0
	if err := WriteConfig(dir, buildResult("2", "0")); err != nil {
		t.Fatal(err)
	}
	data1, err := os.ReadFile(filepath.Join(dir, config.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}

	// Second write: api=1.26.5, worker=1.22.3 (different patches, same major.minor)
	if err := WriteConfig(dir, buildResult("5", "3")); err != nil {
		t.Fatal(err)
	}
	data2, err := os.ReadFile(filepath.Join(dir, config.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}

	// (a) File content must be byte-identical between the two writes.
	if string(data1) != string(data2) {
		t.Errorf("patch-only version change (monorepo) produced different on-disk content:\n--- write1 ---\n%s\n--- write2 ---\n%s", data1, data2)
	}

	// Unmarshal for field-level assertions.
	var cfg config.BravrosConfig
	if err := json.Unmarshal(data2, &cfg); err != nil {
		t.Fatal(err)
	}

	// (b)+(c) Each stack entry: language_version and runtime must be major.minor only.
	for path, wantVersion := range map[string]string{
		"apps/api":    "1.26",
		"apps/worker": "1.22",
	} {
		sc, ok := cfg.Stacks[path]
		if !ok {
			t.Errorf("expected stacks[%q] to exist", path)
			continue
		}
		if sc.LanguageVersion != wantVersion {
			t.Errorf("stacks[%q].language_version: want %q, got %q", path, wantVersion, sc.LanguageVersion)
		}
		if v, ok := sc.Runtime["go"]; !ok || v != wantVersion {
			t.Errorf("stacks[%q].runtime.go: want %q, got %q (ok=%v)", path, wantVersion, v, ok)
		}
	}

	// (d) detected_at must be unchanged across patch-only writes.
	if cfg.DetectedAt != seedDetectedAt {
		t.Errorf("patch-only writes must NOT change detected_at (monorepo)\n  seeded:  %s\n  got:     %s", seedDetectedAt, cfg.DetectedAt)
	}
}

// TestWriteConfig_OutputIsSelfStable verifies that the on-disk form produced by WriteConfig
// is byte-identical to what a second WriteConfig call would produce when seeded with that
// same on-disk content. This catches drift caused by quoting, key ordering, or any future
// yaml encoder behaviour change — regardless of whether the patch version changes.
func TestWriteConfig_OutputIsSelfStable(t *testing.T) {
	// Step 1: write once on a fresh dir with LanguageVersion "1.26.2" → capture canonical bytes.
	dir1 := setupTestDir(t)
	r1 := &DetectResult{
		Stack: config.StackConfig{
			Language:        "go",
			Framework:       "none",
			TestRunner:      "go test",
			LanguageVersion: "1.26.2",
			Runtime:         map[string]string{"go": "1.26.2"},
		},
	}
	if err := WriteConfig(dir1, r1); err != nil {
		t.Fatal(err)
	}
	canonical, err := os.ReadFile(filepath.Join(dir1, config.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}

	// Step 2: seed a fresh dir with the canonical bytes, then call WriteConfig with a
	// *different* patch version ("1.26.0"). The two on-disk outputs must be byte-identical.
	dir2 := setupTestDir(t)
	writeFile(t, filepath.Join(dir2, config.ConfigFilename), string(canonical))
	r2 := &DetectResult{
		Stack: config.StackConfig{
			Language:        "go",
			Framework:       "none",
			TestRunner:      "go test",
			LanguageVersion: "1.26.0",
			Runtime:         map[string]string{"go": "1.26.0"},
		},
	}
	if err := WriteConfig(dir2, r2); err != nil {
		t.Fatal(err)
	}
	roundtripped, err := os.ReadFile(filepath.Join(dir2, config.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}

	if string(canonical) != string(roundtripped) {
		t.Errorf("WriteConfig output is not self-stable across a round-trip:\n--- canonical ---\n%s\n--- roundtripped ---\n%s",
			canonical, roundtripped)
	}

	// Sanity: language_version on disk must be major.minor only (quoted string "1.26").
	var cfg config.BravrosConfig
	if err := json.Unmarshal(roundtripped, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Stack.LanguageVersion != "1.26" {
		t.Errorf("expected language_version=1.26 (no patch, quoted), got %q", cfg.Stack.LanguageVersion)
	}
	if cfg.Stack.Runtime["go"] != "1.26" {
		t.Errorf("expected runtime.go=1.26 (no patch, quoted), got %q", cfg.Stack.Runtime["go"])
	}
}

// TestWriteConfig_BareYamlNumberSeed_GetsQuoted documents the encoder's intentional
// behaviour: when .kaisser.yml is seeded with a bare YAML float (language_version: 1.26
// without quotes), WriteConfig must re-emit it in quoted form ("1.26"). This prevents
// anyone from "fixing" hand-edited files back to the bare form and re-introducing the
// yaml-marshal quoting drift bug.
func TestWriteConfig_BareYamlNumberSeed_GetsQuoted(t *testing.T) {
	dir := setupTestDir(t)

	// Seed with the bare (unquoted) YAML float form — the problematic hand-edited shape.
	seed := `{
  "staging_branch": "homolog",
  "language": "auto",
  "stack": {
    "language": "go",
    "framework": "none",
    "test_runner": "go test",
    "language_version": "1.26",
    "runtime": {
      "go": "1.26"
    }
  },
  "detected_at": "2020-01-01T00:00:00Z"
}`
	writeFile(t, filepath.Join(dir, config.ConfigFilename), seed)

	r := &DetectResult{
		Stack: config.StackConfig{
			Language:        "go",
			Framework:       "none",
			TestRunner:      "go test",
			LanguageVersion: "1.26.1",
			Runtime:         map[string]string{"go": "1.26.1"},
		},
	}
	if err := WriteConfig(dir, r); err != nil {
		t.Fatal(err)
	}

	out, err := os.ReadFile(filepath.Join(dir, config.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}

	// The encoder must produce the quoted form "1.26" (not bare 1.26).
	if !strings.Contains(string(out), `"language_version": "1.26"`) {
		t.Errorf("expected quoted language_version: \"1.26\" in output, got:\n%s", out)
	}
	if strings.Contains(string(out), "\"language_version\": 1.26") {
		t.Errorf("found bare (unquoted) language_version: 1.26 in output — encoder must quote it:\n%s", out)
	}
}

// TestWriteConfig_OutputIsSelfStable_Monorepo is the monorepo-path variant of
// TestWriteConfig_OutputIsSelfStable. It seeds via cfg.Stacks with two entries on
// different patches and asserts that the canonical output is self-stable across a
// round-trip WriteConfig call with a further patch change.
func TestWriteConfig_OutputIsSelfStable_Monorepo(t *testing.T) {
	buildResult := func(apiPatch, workerPatch string) *DetectResult {
		return &DetectResult{
			Monorepo: true,
			Stacks: map[string]config.StackConfig{
				"apps/api": {
					Language:        "go",
					Framework:       "none",
					TestRunner:      "go test",
					LanguageVersion: "1.26." + apiPatch,
					Runtime:         map[string]string{"go": "1.26." + apiPatch},
				},
				"apps/worker": {
					Language:        "go",
					Framework:       "none",
					TestRunner:      "go test",
					LanguageVersion: "1.22." + workerPatch,
					Runtime:         map[string]string{"go": "1.22." + workerPatch},
				},
			},
		}
	}

	// Step 1: write once on a fresh dir → capture canonical bytes.
	dir1 := setupTestDir(t)
	if err := WriteConfig(dir1, buildResult("2", "0")); err != nil {
		t.Fatal(err)
	}
	canonical, err := os.ReadFile(filepath.Join(dir1, config.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}

	// Step 2: seed a fresh dir with canonical bytes, call WriteConfig with different patches.
	dir2 := setupTestDir(t)
	writeFile(t, filepath.Join(dir2, config.ConfigFilename), string(canonical))
	if err := WriteConfig(dir2, buildResult("9", "7")); err != nil {
		t.Fatal(err)
	}
	roundtripped, err := os.ReadFile(filepath.Join(dir2, config.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}

	if string(canonical) != string(roundtripped) {
		t.Errorf("WriteConfig monorepo output is not self-stable across a round-trip:\n--- canonical ---\n%s\n--- roundtripped ---\n%s",
			canonical, roundtripped)
	}

	// Sanity: each stack's language_version must be major.minor only.
	var cfg config.BravrosConfig
	if err := json.Unmarshal(roundtripped, &cfg); err != nil {
		t.Fatal(err)
	}
	for path, wantVersion := range map[string]string{
		"apps/api":    "1.26",
		"apps/worker": "1.22",
	} {
		sc, ok := cfg.Stacks[path]
		if !ok {
			t.Errorf("expected stacks[%q] to exist", path)
			continue
		}
		if sc.LanguageVersion != wantVersion {
			t.Errorf("stacks[%q].language_version: want %q, got %q", path, wantVersion, sc.LanguageVersion)
		}
		if v := sc.Runtime["go"]; v != wantVersion {
			t.Errorf("stacks[%q].runtime.go: want %q, got %q", path, wantVersion, v)
		}
	}
}

// TestStripPatch verifies the stripPatch helper covers all expected input shapes.
func TestStripPatch(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"1.22.0", "1.22"},
		{"1.22.3", "1.22"},
		{"8.3.15", "8.3"},
		{"20.11.0", "20.11"},
		{"1.22", "1.22"},     // already major.minor — unchanged
		{"1", "1"},           // single segment — unchanged
		{"", ""},             // empty — unchanged
		{"go1.22", "go1.22"}, // non-numeric first segment — unchanged (not standard semver)
	}
	for _, tc := range cases {
		got := stripPatch(tc.input)
		if got != tc.want {
			t.Errorf("stripPatch(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestConfigIsMonorepo(t *testing.T) {
	cfg := &config.BravrosConfig{Monorepo: true}
	if !cfg.IsMonorepo() {
		t.Error("expected IsMonorepo()=true")
	}
	cfg.Monorepo = false
	if cfg.IsMonorepo() {
		t.Error("expected IsMonorepo()=false")
	}
}

func TestConfigStackForPath_SingleStack(t *testing.T) {
	cfg := &config.BravrosConfig{
		Stack: config.StackConfig{Language: "go", Framework: "none"},
	}
	s := cfg.StackForPath("anything")
	if s == nil || s.Language != "go" {
		t.Error("expected root stack for single-stack project")
	}
}

func TestConfigStackForPath_Monorepo(t *testing.T) {
	cfg := &config.BravrosConfig{
		Monorepo: true,
		Stacks: map[string]config.StackConfig{
			"apps/api":    {Language: "php", Framework: "laravel"},
			"apps/mobile": {Language: "node", Framework: "expo"},
		},
	}

	api := cfg.StackForPath("apps/api")
	if api == nil || api.Framework != "laravel" {
		t.Error("expected apps/api → laravel")
	}

	mobile := cfg.StackForPath("apps/mobile")
	if mobile == nil || mobile.Framework != "expo" {
		t.Error("expected apps/mobile → expo")
	}

	missing := cfg.StackForPath("apps/unknown")
	if missing != nil {
		t.Error("expected nil for unknown path")
	}
}

// ---------------------------------------------------------------------------
// Phase 5: Golden-file integration test — subdir Go CLI detection + WriteConfig
// ---------------------------------------------------------------------------

// TestDetectIntegration_SubdirGoCLI_GoldenFile feeds the testdata/subdir-go-cli
// fixture (a Go CLI project whose go.mod lives in cli/) into Detect+WriteConfig and
// compares the resulting .kaisser.yml against the checked-in golden file.
//
// The golden file (testdata/subdir-go-cli/golden.json) intentionally omits the
// dynamic `detected_at` field — the comparison strips that line before diffing.
// This test acts as a regression guard: if `,omitempty` is ever removed from
// StackConfig fields, or if the subdir-fallback detection is broken, this test fails.
func TestDetectIntegration_SubdirGoCLI_GoldenFile(t *testing.T) {
	// Load the fixture directory (source-of-truth, never written to).
	fixtureDir, err := filepath.Abs(filepath.Join("testdata", "subdir-go-cli"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fixtureDir); err != nil {
		t.Fatalf("testdata fixture not found at %s: %v", fixtureDir, err)
	}

	// Load the golden file.
	goldenPath := filepath.Join(fixtureDir, "golden.json")
	goldenBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("golden file not found at %s: %v", goldenPath, err)
	}

	// Copy the fixture into a temp dir so WriteConfig doesn't pollute the source tree.
	tmpDir := t.TempDir()
	cliSubdir := filepath.Join(tmpDir, "cli")
	if err := os.MkdirAll(cliSubdir, 0755); err != nil {
		t.Fatal(err)
	}
	fixtureGoMod, err := os.ReadFile(filepath.Join(fixtureDir, "cli", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cliSubdir, "go.mod"), fixtureGoMod, 0644); err != nil {
		t.Fatal(err)
	}

	// Run detection on the temp copy.
	result, err := Detect(tmpDir, DetectOpts{SkipGit: true})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	// Verify core stack fields before writing (belt-and-suspenders).
	if result.Stack.Language != "go" {
		t.Errorf("expected stack.language=go from subdir detection, got %q", result.Stack.Language)
	}
	if result.Monorepo {
		t.Error("expected single-stack (not monorepo) for one-subdir fixture")
	}
	if result.Stack.Framework != "none" {
		t.Errorf("expected stack.framework=none, got %q", result.Stack.Framework)
	}
	if result.Stack.TestRunner != "go test" {
		t.Errorf("expected stack.test_runner='go test', got %q", result.Stack.TestRunner)
	}

	// Write the config to temp dir.
	if err := WriteConfig(tmpDir, result); err != nil {
		t.Fatalf("WriteConfig failed: %v", err)
	}

	// Read back the written file.
	written, err := os.ReadFile(filepath.Join(tmpDir, config.ConfigFilename))
	if err != nil {
		t.Fatalf("failed to read written config: %v", err)
	}

	// Strip the dynamic `detected_at` line from both sides before comparing.
	normalize := func(raw []byte) string {
		lines := strings.Split(string(raw), "\n")
		out := make([]string, 0, len(lines))
		for _, l := range lines {
			if strings.Contains(l, "\"detected_at\":") {
				continue
			}
			out = append(out, l)
		}
		// Trim trailing blank lines for a stable comparison.
		result := strings.TrimRight(strings.Join(out, "\n"), "\n")
		return result
	}

	got := normalize(written)
	want := normalize(goldenBytes)
	if got != want {
		t.Errorf("WriteConfig output does not match golden file.\n--- golden ---\n%s\n--- got ---\n%s", want, got)
	}
}

// ---------------------------------------------------------------------------
// P-0173 Phase 3: workflow priority ranking tests
// ---------------------------------------------------------------------------

// setupGitDir writes a minimal .git/config so detectGit can parse a remote URL.
func setupGitDir(t *testing.T, root string) {
	t.Helper()
	gitConfig := `[core]
	repositoryformatversion = 0
[remote "origin"]
	url = git@github.com:user/repo.git
`
	writeFile(t, filepath.Join(root, ".git", "config"), gitConfig)
}

// TestCIWorkflowPriority_ReleaseBeatsAutoRelease checks that release.yml wins
// over auto-release.yml when both are present (along with an unranked file).
func TestCIWorkflowPriority_ReleaseBeatsAutoRelease(t *testing.T) {
	dir := setupTestDir(t)
	setupGitDir(t, dir)
	writeFile(t, filepath.Join(dir, ".github", "workflows", "audit-docs.yml"), `name: Audit Docs`)
	writeFile(t, filepath.Join(dir, ".github", "workflows", "auto-release.yml"), `name: Auto Release`)
	writeFile(t, filepath.Join(dir, ".github", "workflows", "release.yml"), `name: Release`)

	result, err := Detect(dir, DetectOpts{SkipGit: false})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Git.HasCI {
		t.Error("expected has_ci=true")
	}
	if result.Git.CIWorkflow != "release.yml" {
		t.Errorf("expected ci_workflow=release.yml, got %s", result.Git.CIWorkflow)
	}
}

// TestCIWorkflowPriority_TestsBeatsCI is a regression guard: tests.yml beats ci.yml
// (existing P-0156-era behaviour must be preserved).
func TestCIWorkflowPriority_TestsBeatsCI(t *testing.T) {
	dir := setupTestDir(t)
	setupGitDir(t, dir)
	writeFile(t, filepath.Join(dir, ".github", "workflows", "tests.yml"), `name: Tests`)
	writeFile(t, filepath.Join(dir, ".github", "workflows", "ci.yml"), `name: CI`)

	result, err := Detect(dir, DetectOpts{SkipGit: false})
	if err != nil {
		t.Fatal(err)
	}
	if result.Git.CIWorkflow != "tests.yml" {
		t.Errorf("expected ci_workflow=tests.yml, got %s", result.Git.CIWorkflow)
	}
}

// TestCIWorkflowPriority_CIBeatsAlpha checks that a priority name (ci.yml)
// wins over an alphabetically-earlier but unranked file (audit-docs.yml).
func TestCIWorkflowPriority_CIBeatsAlpha(t *testing.T) {
	dir := setupTestDir(t)
	setupGitDir(t, dir)
	writeFile(t, filepath.Join(dir, ".github", "workflows", "audit-docs.yml"), `name: Audit Docs`)
	writeFile(t, filepath.Join(dir, ".github", "workflows", "ci.yml"), `name: CI`)

	result, err := Detect(dir, DetectOpts{SkipGit: false})
	if err != nil {
		t.Fatal(err)
	}
	if result.Git.CIWorkflow != "ci.yml" {
		t.Errorf("expected ci_workflow=ci.yml, got %s", result.Git.CIWorkflow)
	}
}

// TestCIWorkflowPriority_SingleCI is a regression guard: a single ci.yml still resolves.
func TestCIWorkflowPriority_SingleCI(t *testing.T) {
	dir := setupTestDir(t)
	setupGitDir(t, dir)
	writeFile(t, filepath.Join(dir, ".github", "workflows", "ci.yml"), `name: CI`)

	result, err := Detect(dir, DetectOpts{SkipGit: false})
	if err != nil {
		t.Fatal(err)
	}
	if result.Git.CIWorkflow != "ci.yml" {
		t.Errorf("expected ci_workflow=ci.yml, got %s", result.Git.CIWorkflow)
	}
}
