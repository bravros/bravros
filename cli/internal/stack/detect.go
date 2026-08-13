package stack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/bravros/bravros/cli/internal/config"
	"gopkg.in/yaml.v3"
)

// commonCodeSubdirs is the list of well-known single-stack subdirectories to
// scan when root-level detection finds no marker files and detectMonorepo (apps/
// packages/) also finds nothing. Exported so Phase 3 (Rule 18) can reference it.
var CommonCodeSubdirs = []string{"cli", "src", "server", "backend", "api", "app", "frontend", "web"}

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
		} else {
			// Subdir fallback: scan well-known code subdirectories (cli/, src/, server/, etc.)
			// This covers single-stack repos whose root has no marker files (e.g. this very repo).
			subdirStacks := detectSubdirStack(root, opts.Versions, CommonCodeSubdirs)
			switch {
			case len(subdirStacks) == 1:
				// Exactly one subdir matched → single-stack semantics.
				for _, s := range subdirStacks {
					result.Stack = s.Config
					if opts.Versions && len(s.Versions) > 0 {
						result.Versions = s.Versions
					}
				}
			case len(subdirStacks) > 1:
				// Multiple distinct stacks → monorepo semantics.
				result.Monorepo = true
				result.Stacks = make(map[string]config.StackConfig)
				if opts.Versions {
					result.Versions = make(map[string]string)
				}
				for path, s := range subdirStacks {
					result.Stacks[path] = s.Config
					if opts.Versions {
						for k, v := range s.Versions {
							result.Versions[path+"/"+k] = v
						}
					}
				}
				// 0 matches → leave result empty (no case = fall through, do nothing)
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

// WriteConfig writes detection results to .bravros/config.json, preserving user-set fields.
// If a legacy .bravros.yml or .sbravros.yml exists alongside, it is migrated (renamed) before write.
// Early-exit: when the detection result carries no stack info AND no prior file exists,
// we skip writing to avoid fabricating an empty config that misleads consumers.
func WriteConfig(root string, result *DetectResult) error {
	return writeConfig(root, result, false)
}

// WriteConfigAlways is WriteConfig without the empty-detection early-exit: the config
// file is written even when detection found no stack and none existed before.
// `bravros init` uses this — it promises a .bravros/config.json unconditionally,
// including in a repo that is still empty.
func WriteConfigAlways(root string, result *DetectResult) error {
	return writeConfig(root, result, true)
}

func writeConfig(root string, result *DetectResult, always bool) error {
	cfgPath := filepath.Join(root, config.ConfigFilename)
	legacyPath := filepath.Join(root, config.LegacyConfigFilename)
	legacySbravrosPath := filepath.Join(root, config.LegacySbravrosFilename)

	// Early-exit: nothing detected AND no prior file → don't create an empty stub.
	if !always && result.Stack.Language == "" && len(result.Stacks) == 0 {
		// Check whether a file already exists.
		_, newErr := os.Stat(cfgPath)
		_, legacyErr := os.Stat(legacyPath)
		_, legacySbravrosErr := os.Stat(legacySbravrosPath)
		if os.IsNotExist(newErr) && os.IsNotExist(legacyErr) && os.IsNotExist(legacySbravrosErr) {
			return nil
		}
	}

	// Load existing config to preserve user-set fields
	cfg := &config.BravrosConfig{StagingBranch: "homolog", Language: "auto"}

	if data, err := os.ReadFile(cfgPath); err == nil {
		_ = json.Unmarshal(data, cfg)
	} else {
		if data, err := os.ReadFile(legacyPath); err == nil {
			_ = yaml.Unmarshal(data, cfg)
		} else if data, err := os.ReadFile(legacySbravrosPath); err == nil {
			_ = yaml.Unmarshal(data, cfg)
		}
	}

	// Snapshot the pre-write state so we can decide whether to bump DetectedAt.
	// Goal: detected_at is a "stack changed" marker, not a "we ran detect-stack" marker —
	// otherwise every install/hook execution leaves a one-line dirty diff in git.
	prevMonorepo := cfg.Monorepo
	prevStack := cfg.Stack
	prevStacks := cfg.Stacks
	prevGit := cfg.Git

	// Update detected fields, preserve user-set ones
	cfg.Monorepo = result.Monorepo
	cfg.Stack = result.Stack
	cfg.Stacks = result.Stacks
	cfg.Git = result.Git

	// Only bump DetectedAt when the actual detection result changed, or when it was empty.
	// Canonicalize Runtime map values and LanguageVersion to major.minor before comparing —
	// patch-level bumps (e.g. go 1.22.0 → 1.22.1) must NOT trigger a DetectedAt bump
	// because they represent routine toolchain updates, not a real stack change.
	if cfg.DetectedAt == "" ||
		!reflect.DeepEqual(prevMonorepo, cfg.Monorepo) ||
		!reflect.DeepEqual(canonicalizeStack(prevStack), canonicalizeStack(cfg.Stack)) ||
		!reflect.DeepEqual(canonicalizeStacks(prevStacks), canonicalizeStacks(cfg.Stacks)) ||
		!reflect.DeepEqual(prevGit, cfg.Git) {
		cfg.DetectedAt = time.Now().Format(time.RFC3339)
	}

	if cfg.StagingBranch == "" {
		cfg.StagingBranch = "homolog"
	}
	if cfg.Language == "" {
		cfg.Language = "auto"
	}

	// Canonicalize patch versions on the write path so the on-disk config
	// stores only major.minor (e.g. "1.26" not "1.26.2"), preventing spurious diffs
	// across patch-level toolchain updates.
	cfg.Stack = canonicalizeStack(cfg.Stack)
	cfg.Stacks = canonicalizeStacks(cfg.Stacks)

	cfg.Schema = "https://bravros.dev/schemas/config.v0.json"

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(cfgPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		return err
	}

	// Delete legacy files if they exist
	_ = os.Remove(legacyPath)
	_ = os.Remove(legacySbravrosPath)

	return nil
}

// checkStaleness returns true if any lockfile has been modified after the last detect-stack run.
func checkStaleness(root string) bool {
	cfgPath := filepath.Join(root, config.ConfigFilename)
	data, err := os.ReadFile(cfgPath)
	var detectedAtStr string
	if err == nil {
		var cfg struct {
			DetectedAt string `json:"detected_at"`
		}
		if err := json.Unmarshal(data, &cfg); err == nil {
			detectedAtStr = cfg.DetectedAt
		}
	} else {
		// Fall back to legacy filename during deprecation window.
		legacyPath := filepath.Join(root, config.LegacyConfigFilename)
		data, err = os.ReadFile(legacyPath)
		if err != nil {
			data, err = os.ReadFile(filepath.Join(root, config.LegacySbravrosFilename))
		}
		if err == nil {
			var cfg struct {
				DetectedAt string `yaml:"detected_at"`
			}
			if err := yaml.Unmarshal(data, &cfg); err == nil {
				detectedAtStr = cfg.DetectedAt
			}
		}
	}

	if detectedAtStr == "" {
		return false // no config = not stale
	}

	detectedAt, err := time.Parse(time.RFC3339, detectedAtStr)
	if err != nil {
		return false
	}

	// Check lockfile mtimes.
	// Node PM variants (pnpm, bun, yarn) are included so staleness is detected
	// regardless of which package manager the project uses.
	lockfiles := []string{
		"composer.lock", "package-lock.json", "pnpm-lock.yaml", "bun.lockb", "bun.lock", "yarn.lock",
		"go.mod", "go.sum",
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
		if det.Config.HasAssets {
			det.Config.NodePackageManager = DetectNodePackageManager(dir)
		}
		if withVersions {
			// Manifest first (composer.json — constraint versions), lockfile second
			// (composer.lock — exact resolved versions). Lockfile entries override
			// manifest entries when both exist, so a freshly-cloned project still
			// surfaces sensible `versions.<lib>` keys before `composer install`.
			if manifestVersions, err := ParseComposerJSON(filepath.Join(dir, "composer.json")); err == nil {
				for k, v := range manifestVersions {
					det.Versions[k] = v
				}
			}
			if lockVersions, err := ParseComposerLock(filepath.Join(dir, "composer.lock")); err == nil {
				for k, v := range lockVersions {
					det.Versions[k] = v
				}
			}
			// Also parse Node packages if has_assets (e.g. tailwindcss, daisyui).
			// Same order: package.json → package-lock.json (lockfile wins).
			if det.Config.HasAssets {
				if manifestVersions, err := ParsePackageJSON(filepath.Join(dir, "package.json")); err == nil {
					for k, v := range manifestVersions {
						det.Versions[k] = v
					}
				}
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
		det.Config.NodePackageManager = DetectNodePackageManager(dir)
		if withVersions {
			// Manifest first (package.json — constraint versions), lockfile second
			// (package-lock.json — exact resolved versions). Lockfile wins when both
			// exist; manifest fills in pre-install / pnpm/bun/yarn cases.
			if manifestVersions, err := ParsePackageJSON(filepath.Join(dir, "package.json")); err == nil {
				for k, v := range manifestVersions {
					det.Versions[k] = v
				}
			}
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

// detectSubdirStack scans each named subdir under root for a single-stack marker.
// Returns a map from relative-subdir-path to *stackDetection for each subdir that matched.
// Subdirs that don't exist are silently skipped.
func detectSubdirStack(root string, withVersions bool, subdirs []string) map[string]*stackDetection {
	stacks := make(map[string]*stackDetection)
	for _, subdir := range subdirs {
		fullPath := filepath.Join(root, subdir)
		if _, err := os.Stat(fullPath); err != nil {
			continue // subdir doesn't exist
		}
		if det, found := detectSingleStack(fullPath, withVersions); found {
			stacks[subdir] = det
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
		// Priority-ranked scan: prefer release/deploy/CI signals over generic names.
		// Highest priority first; if no priority match, fall back to alphabetical first.
		workflowPriority := []string{
			"release.yml", "auto-release.yml", "publish.yml",
			"deploy.yml", "tests.yml", "test.yml", "ci.yml",
		}
		bestRank := len(workflowPriority) // sentinel: lower index = higher priority
		bestName := ""
		alphaFallback := ""
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
				continue
			}
			matched := false
			for rank, p := range workflowPriority {
				if name == p {
					if rank < bestRank {
						bestRank = rank
						bestName = name
					}
					matched = true
					break
				}
			}
			// Track alphabetical-first among non-priority files for the fallback path
			if !matched {
				if alphaFallback == "" || name < alphaFallback {
					alphaFallback = name
				}
			}
		}
		if bestName == "" {
			// No priority match — use alphabetical first among all yml/yaml files
			bestName = alphaFallback
		}
		if bestName == "" {
			// No .yml/.yaml files at all — use first entry name as a safe fallback
			bestName = entries[0].Name()
		}
		git.CIWorkflow = bestName
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

// DetectNodePackageManager returns the Node package manager hinted by lockfiles
// present in dir. Priority matches common community conventions — bun/pnpm/yarn
// beat npm because their presence is an explicit opt-in, while package-lock.json
// is often the implicit fallback.
//
// Returns one of: "bun", "pnpm", "yarn", "npm" — or empty string when no
// package.json is present. bravros-style lockfile slice is intentionally more
// complete here: bravros's lockfile list was missing pnpm/bun/yarn entirely; the
// full set is added on bravros first and should be synced back on next DRY pass.
func DetectNodePackageManager(dir string) string {
	if !fileExists(filepath.Join(dir, "package.json")) {
		return ""
	}
	// bun.lockb (binary) or bun.lock (text) — both signal bun
	if fileExists(filepath.Join(dir, "bun.lockb")) || fileExists(filepath.Join(dir, "bun.lock")) {
		return "bun"
	}
	if fileExists(filepath.Join(dir, "pnpm-lock.yaml")) {
		return "pnpm"
	}
	if fileExists(filepath.Join(dir, "yarn.lock")) {
		return "yarn"
	}
	if fileExists(filepath.Join(dir, "package-lock.json")) {
		return "npm"
	}
	// package.json without a lockfile — default to npm (most universal)
	return "npm"
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

// stripPatch reduces a semver string to its major.minor components, discarding the
// patch segment and any pre-release/build-metadata suffix. Strings that are already
// in major.minor form, or that don't look like semver, are returned unchanged.
//
// Examples: "1.22.3" → "1.22", "8.3.15" → "8.3", "1.22" → "1.22", "go1.22" → "go1.22"
func stripPatch(v string) string {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 3 {
		return v // already major.minor or non-semver — leave as-is
	}
	return parts[0] + "." + parts[1]
}

// canonicalizeRuntimeMap returns a shallow copy of m with all values passed through
// stripPatch, so patch-level runtime version churn (e.g. go 1.22.0 → 1.22.1) does
// not register as a meaningful stack change when compared with reflect.DeepEqual.
func canonicalizeRuntimeMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = stripPatch(v)
	}
	return out
}

// canonicalizeStack returns a shallow copy of s with Runtime and LanguageVersion
// canonicalized to major.minor. Used both for the DetectedAt equality check and
// on the WriteConfig write path, so the on-disk config stores only major.minor.
func canonicalizeStack(s config.StackConfig) config.StackConfig {
	s.Runtime = canonicalizeRuntimeMap(s.Runtime)
	s.LanguageVersion = stripPatch(s.LanguageVersion)
	return s
}

// canonicalizeStacks applies canonicalizeStack to every entry in a monorepo stacks
// map. Used both for the DetectedAt equality check and on the WriteConfig write path.
func canonicalizeStacks(m map[string]config.StackConfig) map[string]config.StackConfig {
	if m == nil {
		return nil
	}
	out := make(map[string]config.StackConfig, len(m))
	for k, v := range m {
		out[k] = canonicalizeStack(v)
	}
	return out
}
