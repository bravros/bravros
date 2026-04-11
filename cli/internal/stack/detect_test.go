package stack

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/bravros/private/internal/config"
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
	data, err := os.ReadFile(filepath.Join(dir, ".bravros.yml"))
	if err != nil {
		t.Fatal(err)
	}

	var cfg config.BravrosConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
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

	// Write existing config with user-set fields
	existing := `staging_branch: develop
language: pt-BR
stack:
  language: ""
  framework: ""
  test_runner: ""
  has_assets: false
git:
  remote: ""
  has_ci: false
  ci_workflow: ""
env:
  deployed: true
  production: https://app.example.com
  staging: ""
  local: ""
`
	writeFile(t, filepath.Join(dir, ".bravros.yml"), existing)
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

	data, err := os.ReadFile(filepath.Join(dir, ".bravros.yml"))
	if err != nil {
		t.Fatal(err)
	}

	var cfg config.BravrosConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
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
