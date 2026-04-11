package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// BravrosConfig holds per-project configuration from .bravros.yml
// Generated once by `sdlc detect-stack` or `sdlc init`, read by all skills via `sdlc meta`.
type BravrosConfig struct {
	StagingBranch string                 `yaml:"staging_branch"`
	Language      string                 `yaml:"language"` // auto, en, pt-BR, es, fr, etc.
	Monorepo      bool                   `yaml:"monorepo"`
	Stack         StackConfig            `yaml:"stack"`
	Stacks        map[string]StackConfig `yaml:"stacks,omitempty"`
	Git           GitConfig              `yaml:"git"`
	Env           EnvConfig              `yaml:"env"`
	DetectedAt    string                 `yaml:"detected_at,omitempty"`
}

// IsMonorepo returns whether the project is a monorepo with multiple stacks.
func (c *BravrosConfig) IsMonorepo() bool {
	return c.Monorepo
}

// StackForPath returns the StackConfig for the given relative path.
// For single-stack projects, it returns the root Stack.
// For monorepos, it looks up the path in the Stacks map.
func (c *BravrosConfig) StackForPath(path string) *StackConfig {
	if !c.Monorepo {
		return &c.Stack
	}
	if c.Stacks == nil {
		return nil
	}
	if sc, ok := c.Stacks[path]; ok {
		return &sc
	}
	return nil
}

// StackConfig holds detected project stack info.
// Populated by `sdlc detect-stack`, read by skills to avoid re-detection.
type StackConfig struct {
	Language        string            `yaml:"language"         json:"language,omitempty"`    // go, php, node, python, rust
	Framework       string            `yaml:"framework"        json:"framework,omitempty"`   // laravel, nextjs, express, django, none
	TestRunner      string            `yaml:"test_runner"      json:"test_runner,omitempty"` // pest, jest, vitest, pytest, go test, none
	HasAssets       bool              `yaml:"has_assets"       json:"has_assets,omitempty"`
	LanguageVersion string            `yaml:"language_version" json:"language_version,omitempty"` // e.g. 1.22.0, 8.2, 20
	ProjectType     string            `yaml:"project_type"     json:"project_type,omitempty"`     // api, fullstack, cli, library, etc.
	Runtime         map[string]string `yaml:"runtime,omitempty" json:"runtime,omitempty"`         // key package versions from lockfile
}

// GitConfig holds git/CI info cached from remote.
// Populated by `sdlc detect-stack`, avoids repeated `gh` API calls.
type GitConfig struct {
	Remote     string `yaml:"remote"      json:"remote,omitempty"`      // git@github.com:owner/repo.git
	HasCI      bool   `yaml:"has_ci"      json:"has_ci,omitempty"`      // tests.yml exists and has recent runs
	CIWorkflow string `yaml:"ci_workflow" json:"ci_workflow,omitempty"` // tests.yml, ci.yml, etc.
}

// EnvConfig holds environment URLs and deployment state.
// Replaces the old `.deployed` marker file with richer context.
type EnvConfig struct {
	Deployed   bool   `yaml:"deployed"`   // true = has been deployed to production (replaces .deployed file)
	Production string `yaml:"production"` // https://app.example.com
	Staging    string `yaml:"staging"`    // https://staging.example.com (optional)
	Local      string `yaml:"local"`      // https://app.test or http://localhost:3000
}

// LoadBravrosConfig reads project config from .bravros.yml.
// Returns the config and whether the file was found.
// If the file is not found or unparseable, returns defaults with found=false.
func LoadBravrosConfig() (*BravrosConfig, bool) {
	cfg := &BravrosConfig{StagingBranch: "homolog"} // default

	data, err := os.ReadFile(".bravros.yml")
	if err != nil {
		return cfg, false
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return cfg, false
	}

	if cfg.StagingBranch == "" {
		cfg.StagingBranch = "homolog"
	}

	return cfg, true
}
