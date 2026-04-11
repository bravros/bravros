package stack

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bravros/private/internal/config"
	"gopkg.in/yaml.v3"
)

// DetectResult holds the full stack detection output.
type DetectResult struct {
	Monorepo bool                          `json:"monorepo"`
	Stack    config.StackConfig            `json:"stack,omitempty"`
	Stacks   map[string]config.StackConfig `json:"stacks,omitempty"`
	Git      config.GitConfig              `json:"git"`
	Versions map[string]string             `json:"versions,omitempty"`
	Stale    bool                          `json:"stale,omitempty"`
}

// DetectOpts controls detection behavior.
type DetectOpts struct {
	Versions bool // include version details
	SkipGit  bool // skip git remote detection (for tests)
}

// Detect performs stack detection on the given root directory.
func Detect(root string, opts DetectOpts) (*DetectResult, error) {
	result := &DetectResult{}

	// Fast path: check root for framework markers
	rootStack, rootFound := detectSingleStack(root, opts.Versions)
	if rootFound {
		result.Stack = rootStack.Config
		if opts.Versions && len(rootStack.Versions) > 0 {
			result.Versions = rootStack.Versions
		}
	} else {
		// Monorepo detection: scan apps/ and packages/ subdirs
		stacks := detectMonorepo(root, opts.Versions)
		if len(stacks) > 0 {
			result.Monorepo = true
			result.Stacks = make(map[string]config.StackConfig)
			if opts.Versions {
				result.Versions = make(map[string]string)
			}
			for path, s := range stacks {
				result.Stacks[path] = s.Config
				if opts.Versions {
					for k, v := range s.Versions {
						result.Versions[path+"/"+k] = v
					}
				}
			}
		}
	}

	// Project type classification
	if result.Stack.Language != "" {
		result.Stack.ProjectType = DetectProjectType(result.Stack.Language, result.Stack.Framework, root)
	}

	// Runtime detection (when --versions is set)
	if opts.Versions && result.Stack.Language != "" {
		result.Stack.Runtime = DetectRuntime(result.Stack.Language)
		// Also detect Node runtime for projects with frontend assets (e.g. PHP + Vite/Tailwind)
		if result.Stack.HasAssets && result.Stack.Language != "node" {
			nodeRuntime := DetectRuntime("node")
			if result.Stack.Runtime == nil {
				result.Stack.Runtime = nodeRuntime
			} else {
				for k, v := range nodeRuntime {
					result.Stack.Runtime[k] = v
				}
			}
		}
		// Populate LanguageVersion from runtime map (e.g. runtime["php"] → language_version)
		if v, ok := result.Stack.Runtime[result.Stack.Language]; ok {
			result.Stack.LanguageVersion = v
		}
	}

	// Staleness detection: compare lockfile mtime vs detected_at from .bravros.yml
	if opts.Versions {
		result.Stale = checkStaleness(root)
	}

	// Git detection
	if !opts.SkipGit {
		result.Git = detectGit(root)
	}

	return result, nil
}

// WriteConfig writes detection results to .bravros.yml, preserving user-set fields.
func WriteConfig(root string, result *DetectResult) error {
	cfgPath := filepath.Join(root, ".bravros.yml")

	// Load existing config to preserve user-set fields
	cfg := &config.BravrosConfig{StagingBranch: "homolog", Language: "auto"}

	if data, err := os.ReadFile(cfgPath); err == nil {
		_ = yaml.Unmarshal(data, cfg)
	}

	// Update detected fields, preserve user-set ones
	cfg.Monorepo = result.Monorepo
	cfg.Stack = result.Stack
	cfg.Stacks = result.Stacks
	cfg.Git = result.Git
	cfg.DetectedAt = time.Now().Format(time.RFC3339)

	if cfg.StagingBranch == "" {
		cfg.StagingBranch = "homolog"
	}
	if cfg.Language == "" {
		cfg.Language = "auto"
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(cfgPath, data, 0644)
}

// checkStaleness returns true if any lockfile has been modified after the last detect-stack run.
func checkStaleness(root string) bool {
	cfgPath := filepath.Join(root, ".bravros.yml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return false // no config = not stale
	}

	var cfg struct {
		DetectedAt string `yaml:"detected_at"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil || cfg.DetectedAt == "" {
		return false
	}

	detectedAt, err := time.Parse(time.RFC3339, cfg.DetectedAt)
	if err != nil {
		return false
	}

	// Check lockfile mtimes
	lockfiles := []string{
		"composer.lock", "package-lock.json", "go.mod", "go.sum",
		"pyproject.toml", "requirements.txt", "Cargo.lock",
	}
	for _, lf := range lockfiles {
		info, err := os.Stat(filepath.Join(root, lf))
		if err == nil && info.ModTime().After(detectedAt) {
			return true
		}
	}
	return false
}

// stackDetection holds intermediate detection results.
type stackDetection struct {
	Config   config.StackConfig
	Versions map[string]string
}

func detectSingleStack(dir string, withVersions bool) (*stackDetection, bool) {
	det := &stackDetection{
		Versions: make(map[string]string),
	}

	// Check composer.json → PHP/Laravel
	if fileExists(filepath.Join(dir, "composer.json")) {
		det.Config.Language = "php"
		det.Config.Framework = detectPHPFramework(dir)
		det.Config.TestRunner = detectPHPTestRunner(dir)
		det.Config.HasAssets = fileExists(filepath.Join(dir, "package.json"))
		if withVersions {
			if lockVersions, err := ParseComposerLock(filepath.Join(dir, "composer.lock")); err == nil {
				for k, v := range lockVersions {
					det.Versions[k] = v
				}
			}
			// Also parse Node packages if has_assets (e.g. tailwindcss, daisyui)
			if det.Config.HasAssets {
				if nodeVersions, err := ParsePackageLock(filepath.Join(dir, "package-lock.json")); err == nil {
					for k, v := range nodeVersions {
						det.Versions[k] = v
					}
				}
			}
		}
		return det, true
	}

	// Check go.mod → Go
	if fileExists(filepath.Join(dir, "go.mod")) {
		det.Config.Language = "go"
		det.Config.Framework = "none"
		det.Config.TestRunner = "go test"
		det.Config.HasAssets = fileExists(filepath.Join(dir, "package.json"))
		if withVersions {
			if lockVersions, err := ParseGoMod(filepath.Join(dir, "go.mod")); err == nil {
				for k, v := range lockVersions {
					det.Versions[k] = v
				}
			}
		}
		return det, true
	}

	// Check Cargo.toml → Rust
	if fileExists(filepath.Join(dir, "Cargo.toml")) {
		det.Config.Language = "rust"
		det.Config.Framework = "none"
		det.Config.TestRunner = "cargo test"
		det.Config.HasAssets = false
		return det, true
	}

	// Check requirements.txt or pyproject.toml → Python
	if fileExists(filepath.Join(dir, "requirements.txt")) || fileExists(filepath.Join(dir, "pyproject.toml")) {
		det.Config.Language = "python"
		det.Config.Framework = detectPythonFramework(dir)
		det.Config.TestRunner = detectPythonTestRunner(dir)
		det.Config.HasAssets = fileExists(filepath.Join(dir, "package.json"))
		if withVersions {
			if lockVersions, err := ParsePyProject(filepath.Join(dir, "pyproject.toml")); err == nil {
				for k, v := range lockVersions {
					det.Versions[k] = v
				}
			}
		}
		return det, true
	}

	// Check package.json → Node/React/Next/Expo
	if fileExists(filepath.Join(dir, "package.json")) {
		det.Config.Language = "node"
		det.Config.Framework = detectNodeFramework(dir)
		det.Config.TestRunner = detectNodeTestRunner(dir)
		det.Config.HasAssets = true
		if withVersions {
			if lockVersions, err := ParsePackageLock(filepath.Join(dir, "package-lock.json")); err == nil {
				for k, v := range lockVersions {
					det.Versions[k] = v
				}
			}
		}
		return det, true
	}

	return nil, false
}

func detectMonorepo(root string, withVersions bool) map[string]*stackDetection {
	stacks := make(map[string]*stackDetection)

	for _, subdir := range []string{"apps", "packages"} {
		dirPath := filepath.Join(root, subdir)
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			fullPath := filepath.Join(dirPath, entry.Name())
			if det, found := detectSingleStack(fullPath, withVersions); found {
				relPath := filepath.Join(subdir, entry.Name())
				stacks[relPath] = det
			}
		}
	}

	return stacks
}

func detectGit(root string) config.GitConfig {
	git := config.GitConfig{}

	// Read git remote from config file directly (no exec needed)
	gitConfigPath := filepath.Join(root, ".git", "config")
	data, err := os.ReadFile(gitConfigPath)
	if err != nil {
		return git
	}

	lines := strings.Split(string(data), "\n")
	inRemoteOrigin := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == `[remote "origin"]` {
			inRemoteOrigin = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			inRemoteOrigin = false
			continue
		}
		if inRemoteOrigin && strings.HasPrefix(trimmed, "url = ") {
			git.Remote = strings.TrimPrefix(trimmed, "url = ")
		}
	}

	// Check for CI workflow files
	workflowDir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(workflowDir)
	if err == nil && len(entries) > 0 {
		git.HasCI = true
		// Prefer tests.yml, then ci.yml, then first found
		git.CIWorkflow = entries[0].Name()
		for _, e := range entries {
			name := e.Name()
			if name == "tests.yml" || name == "test.yml" {
				git.CIWorkflow = name
				break
			}
			if name == "ci.yml" {
				git.CIWorkflow = name
			}
		}
	}

	return git
}

// PHP detection helpers

func detectPHPFramework(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "composer.json"))
	if err != nil {
		return "none"
	}
	if strings.Contains(string(data), "laravel/framework") {
		return "laravel"
	}
	if strings.Contains(string(data), "symfony/framework-bundle") {
		return "symfony"
	}
	return "php"
}

func detectPHPTestRunner(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "composer.json"))
	if err != nil {
		return "none"
	}
	content := string(data)
	if strings.Contains(content, "pestphp/pest") {
		return "pest"
	}
	if strings.Contains(content, "phpunit/phpunit") {
		return "phpunit"
	}
	return "none"
}

// Node detection helpers

func detectNodeFramework(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return "none"
	}
	content := string(data)
	if strings.Contains(content, `"next"`) {
		return "nextjs"
	}
	if strings.Contains(content, `"expo"`) {
		return "expo"
	}
	if strings.Contains(content, `"nuxt"`) {
		return "nuxt"
	}
	if strings.Contains(content, `"vue"`) {
		return "vue"
	}
	if strings.Contains(content, `"react"`) {
		return "react"
	}
	if strings.Contains(content, `"express"`) {
		return "express"
	}
	return "node"
}

func detectNodeTestRunner(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return "none"
	}
	content := string(data)
	if strings.Contains(content, `"vitest"`) {
		return "vitest"
	}
	if strings.Contains(content, `"jest"`) {
		return "jest"
	}
	return "none"
}

// Go detection helpers (none needed — ParseGoMod handles all version extraction)

// Python detection helpers

func detectPythonFramework(dir string) string {
	// Check requirements.txt
	if data, err := os.ReadFile(filepath.Join(dir, "requirements.txt")); err == nil {
		content := strings.ToLower(string(data))
		if strings.Contains(content, "django") {
			return "django"
		}
		if strings.Contains(content, "flask") {
			return "flask"
		}
		if strings.Contains(content, "fastapi") {
			return "fastapi"
		}
	}

	// Check pyproject.toml
	if data, err := os.ReadFile(filepath.Join(dir, "pyproject.toml")); err == nil {
		content := strings.ToLower(string(data))
		if strings.Contains(content, "django") {
			return "django"
		}
		if strings.Contains(content, "flask") {
			return "flask"
		}
		if strings.Contains(content, "fastapi") {
			return "fastapi"
		}
	}

	return "none"
}

func detectPythonTestRunner(dir string) string {
	// Check requirements.txt
	if data, err := os.ReadFile(filepath.Join(dir, "requirements.txt")); err == nil {
		content := strings.ToLower(string(data))
		if strings.Contains(content, "pytest") {
			return "pytest"
		}
	}

	// Check pyproject.toml
	if data, err := os.ReadFile(filepath.Join(dir, "pyproject.toml")); err == nil {
		content := strings.ToLower(string(data))
		if strings.Contains(content, "pytest") {
			return "pytest"
		}
	}

	return "none"
}

// DetectProjectType classifies the project as one of: mobile, webapp, api, cli, library.
// It examines the filesystem layout relative to root using language and framework hints.
func DetectProjectType(language, framework, root string) string {
	// mobile: app.json exists AND (expo framework OR react-native in package.json)
	if fileExists(filepath.Join(root, "app.json")) {
		if framework == "expo" {
			return "mobile"
		}
		if data, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil {
			if strings.Contains(string(data), "react-native") {
				return "mobile"
			}
		}
	}

	// webapp: has routes AND views/pages directory
	switch language {
	case "php":
		if fileExists(filepath.Join(root, "routes", "web.php")) &&
			fileExists(filepath.Join(root, "resources", "views")) {
			return "webapp"
		}
	case "node":
		if framework == "nextjs" || framework == "nuxt" {
			if fileExists(filepath.Join(root, "pages")) ||
				fileExists(filepath.Join(root, "app")) {
				return "webapp"
			}
		}
	case "python":
		if framework == "django" {
			if fileExists(filepath.Join(root, "urls.py")) &&
				fileExists(filepath.Join(root, "templates")) {
				return "webapp"
			}
		}
	}

	// api: has routes but no views
	switch language {
	case "php":
		if fileExists(filepath.Join(root, "routes", "api.php")) &&
			!fileExists(filepath.Join(root, "resources", "views")) {
			return "api"
		}
		// Laravel with only api routes (no web.php)
		if fileExists(filepath.Join(root, "routes", "api.php")) &&
			!fileExists(filepath.Join(root, "routes", "web.php")) {
			return "api"
		}
	case "node":
		if framework == "express" {
			if !fileExists(filepath.Join(root, "pages")) &&
				!fileExists(filepath.Join(root, "app")) {
				return "api"
			}
		}
	case "python":
		if framework == "fastapi" || framework == "flask" {
			if !fileExists(filepath.Join(root, "templates")) {
				return "api"
			}
		}
		if fileExists(filepath.Join(root, "main.py")) || fileExists(filepath.Join(root, "app.py")) {
			if !fileExists(filepath.Join(root, "templates")) {
				return "api"
			}
		}
	}

	// cli: Go main.go or recognizable CLI entry points
	if language == "go" && fileExists(filepath.Join(root, "main.go")) {
		return "cli"
	}

	// library: none of the above
	return "library"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
