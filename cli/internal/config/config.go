package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ConfigFilename is the canonical project config filename read by all bravros tooling.
const (
	ConfigFilename         = ".bravros/config.json"
	LegacyConfigFilename   = ".kaisser.yml"
	LegacySkaisserFilename = ".skaisser.yml"
)

// SkillsConfig holds per-project skill configuration.
type SkillsConfig struct {
	Preserve []string `json:"preserve,omitempty" yaml:"preserve,omitempty"`
	Enabled  []string `json:"enabled,omitempty" yaml:"enabled,omitempty"`
}

// AuditConfig holds per-repo audit rule configuration.
type AuditConfig struct {
	DisabledRules []string `json:"disabled_rules,omitempty" yaml:"disabled_rules,omitempty"`
}

// BravrosConfig holds per-project configuration from .bravros/config.json
type BravrosConfig struct {
	Schema            string                 `json:"$schema,omitempty"`
	StagingBranch     string                 `json:"staging_branch,omitempty" yaml:"staging_branch,omitempty"`
	Language          string                 `json:"language,omitempty" yaml:"language,omitempty"` // auto, en, pt-BR, es, fr, etc.
	Monorepo          bool                   `json:"monorepo,omitempty" yaml:"monorepo,omitempty"`
	Stack             StackConfig            `json:"stack,omitempty" yaml:"stack,omitempty"`
	Stacks            map[string]StackConfig `json:"stacks,omitempty" yaml:"stacks,omitempty"`
	Git               GitConfig              `json:"git,omitempty" yaml:"git,omitempty"`
	Env               EnvConfig              `json:"env,omitempty" yaml:"env,omitempty"`
	DetectedAt        string                 `json:"detected_at,omitempty" yaml:"detected_at,omitempty"`
	MergeStrategy     *MergeStrategyConfig   `json:"merge_strategy,omitempty" yaml:"merge_strategy,omitempty"`
	Features          *FeaturesConfig        `json:"features,omitempty" yaml:"features,omitempty"`
	PermanentBranches []string               `json:"permanent_branches,omitempty" yaml:"permanent_branches,omitempty"`
	Skills            SkillsConfig           `json:"skills,omitempty" yaml:"skills,omitempty"`
	Audit             *AuditConfig           `json:"audit,omitempty" yaml:"audit,omitempty"`
}

// MergeStrategyConfig controls how PRs are merged at different base branches.
type MergeStrategyConfig struct {
	Default  string            `json:"default" yaml:"default"`
	IntoMain string            `json:"into_main" yaml:"into_main"`
	ByBase   map[string]string `json:"by_base" yaml:"by_base"`
}

// FeaturesConfig holds feature-flag toggles for optional kaisser behaviours.
type FeaturesConfig struct {
	ReviewStampGate bool            `json:"review_stamp_gate" yaml:"review_stamp_gate"`
	Extra           map[string]bool `json:"extra,omitempty" yaml:"extra,omitempty"`
	ExtraDebounce   map[string]int  `json:"extra_debounce,omitempty" yaml:"extra_debounce,omitempty"`
}

// clone returns a deep copy of the config.
func (c *BravrosConfig) clone() *BravrosConfig {
	if c == nil {
		return nil
	}
	cp := *c

	cp.Stack = c.Stack.clone()

	if c.Stacks != nil {
		cp.Stacks = make(map[string]StackConfig, len(c.Stacks))
		for k, v := range c.Stacks {
			cp.Stacks[k] = v.clone()
		}
	}

	if c.PermanentBranches != nil {
		cp.PermanentBranches = append([]string(nil), c.PermanentBranches...)
	}

	if c.Skills.Preserve != nil {
		cp.Skills.Preserve = append([]string(nil), c.Skills.Preserve...)
	}
	if c.Skills.Enabled != nil {
		cp.Skills.Enabled = append([]string(nil), c.Skills.Enabled...)
	}

	if c.MergeStrategy != nil {
		ms := *c.MergeStrategy
		if c.MergeStrategy.ByBase != nil {
			ms.ByBase = make(map[string]string, len(c.MergeStrategy.ByBase))
			for k, v := range c.MergeStrategy.ByBase {
				ms.ByBase[k] = v
			}
		}
		cp.MergeStrategy = &ms
	}

	if c.Features != nil {
		fc := *c.Features
		if c.Features.Extra != nil {
			fc.Extra = make(map[string]bool, len(c.Features.Extra))
			for k, v := range c.Features.Extra {
				fc.Extra[k] = v
			}
		}
		if c.Features.ExtraDebounce != nil {
			fc.ExtraDebounce = make(map[string]int, len(c.Features.ExtraDebounce))
			for k, v := range c.Features.ExtraDebounce {
				fc.ExtraDebounce[k] = v
			}
		}
		cp.Features = &fc
	}

	if c.Audit != nil {
		ac := *c.Audit
		if c.Audit.DisabledRules != nil {
			ac.DisabledRules = append([]string(nil), c.Audit.DisabledRules...)
		}
		cp.Audit = &ac
	}

	return &cp
}

// clone returns a deep copy of the StackConfig.
func (s StackConfig) clone() StackConfig {
	cp := s
	if s.Runtime != nil {
		cp.Runtime = make(map[string]string, len(s.Runtime))
		for k, v := range s.Runtime {
			cp.Runtime[k] = v
		}
	}
	return cp
}

// IsMonorepo returns whether the project is a monorepo with multiple stacks.
func (c *BravrosConfig) IsMonorepo() bool {
	return c.Monorepo
}

// StackForPath returns the StackConfig for the given relative path.
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
type StackConfig struct {
	Language           string            `yaml:"language,omitempty"            json:"language,omitempty"`
	Framework          string            `yaml:"framework,omitempty"           json:"framework,omitempty"`
	TestRunner         string            `yaml:"test_runner,omitempty"         json:"test_runner,omitempty"`
	HasAssets          bool              `yaml:"has_assets,omitempty"          json:"has_assets,omitempty"`
	LanguageVersion    string            `yaml:"language_version,omitempty"    json:"language_version,omitempty"`
	ProjectType        string            `yaml:"project_type,omitempty"        json:"project_type,omitempty"`
	Runtime            map[string]string `yaml:"runtime,omitempty"             json:"runtime,omitempty"`
	NodePackageManager string            `yaml:"node_package_manager,omitempty" json:"node_package_manager,omitempty"`
}

// GitConfig holds git/CI info cached from remote.
type GitConfig struct {
	Remote     string `yaml:"remote,omitempty"      json:"remote,omitempty"`
	HasCI      bool   `yaml:"has_ci,omitempty"      json:"has_ci,omitempty"`
	CIWorkflow string `yaml:"ci_workflow,omitempty" json:"ci_workflow,omitempty"`
}

// EnvConfig holds environment URLs and deployment state.
type EnvConfig struct {
	Deployed   bool   `yaml:"deployed,omitempty"    json:"deployed,omitempty"`
	Production string `yaml:"production,omitempty"  json:"production,omitempty"`
	Staging    string `yaml:"staging,omitempty"     json:"staging,omitempty"`
	Local      string `yaml:"local,omitempty"       json:"local,omitempty"`
}

// ReadFeature returns the boolean value of a named feature flag.
func ReadFeature(name string) bool {
	cfg, found := LoadBravrosConfig()
	if !found || cfg.Features == nil {
		return false
	}
	switch name {
	case "review_stamp_gate":
		return cfg.Features.ReviewStampGate
	default:
		return false
	}
}

// ReadPermanentBranches returns the list of branches that should never be deleted.
func ReadPermanentBranches() []string {
	cfg, found := LoadBravrosConfig()
	if !found || len(cfg.PermanentBranches) == 0 {
		return []string{"main", "homolog", "staging", "develop"}
	}
	return cfg.PermanentBranches
}

type configCacheEntry struct {
	cfg     *BravrosConfig
	found   bool
	mtime   time.Time
	size    int64
	missing bool
}

var (
	configCacheMu sync.Mutex
	configCache   = map[string]configCacheEntry{}
)

// configFileIdentity returns the identity of the current config file.
func configFileIdentity() (mtime time.Time, size int64, exists bool) {
	if fi, err := os.Stat(ConfigFilename); err == nil {
		return fi.ModTime(), fi.Size(), true
	}
	if fi, err := os.Stat(LegacyConfigFilename); err == nil {
		return fi.ModTime(), fi.Size(), true
	}
	if fi, err := os.Stat(LegacySkaisserFilename); err == nil {
		return fi.ModTime(), fi.Size(), true
	}
	return time.Time{}, 0, false
}

// LoadBravrosConfig reads the project config from the current working directory.
func LoadBravrosConfig() (*BravrosConfig, bool) {
	cwd, cwdErr := os.Getwd()
	mtime, size, exists := configFileIdentity()

	if cwdErr == nil {
		configCacheMu.Lock()
		if e, ok := configCache[cwd]; ok {
			if e.missing == !exists && (!exists || (e.mtime.Equal(mtime) && e.size == size)) {
				cfg := e.cfg.clone()
				configCacheMu.Unlock()
				return cfg, e.found
			}
		}
		configCacheMu.Unlock()
	}

	cfg, found := loadBravrosConfigUncached()

	if cwdErr == nil {
		postMtime, postSize, postExists := configFileIdentity()
		configCacheMu.Lock()
		configCache[cwd] = configCacheEntry{
			cfg:     cfg.clone(),
			found:   found,
			mtime:   postMtime,
			size:    postSize,
			missing: !postExists,
		}
		configCacheMu.Unlock()
	}

	return cfg, found
}

// loadBravrosConfigUncached performs the actual disk read + parse.
func loadBravrosConfigUncached() (*BravrosConfig, bool) {
	cfg := &BravrosConfig{StagingBranch: "homolog"} // default

	// 1. Try to read new JSON config
	data, err := os.ReadFile(ConfigFilename)
	if err == nil {
		if jsonErr := json.Unmarshal(data, cfg); jsonErr != nil {
			return cfg, false
		}
		if cfg.StagingBranch == "" {
			cfg.StagingBranch = "homolog"
		}
		return cfg, true
	}

	// 2. Check for legacy YAML configs and migrate on the fly
	legacyData, legacyFilename, legacyErr := readLegacyData()
	if legacyErr == nil {
		if yamlErr := yaml.Unmarshal(legacyData, cfg); yamlErr != nil {
			return cfg, false
		}
		if cfg.StagingBranch == "" {
			cfg.StagingBranch = "homolog"
		}
		// Migrate to new JSON format
		cfg.Schema = "https://bravros.dev/schemas/config.v0.json"
		dir := filepath.Dir(ConfigFilename)
		_ = os.MkdirAll(dir, 0755)
		if jsonData, jsonErr := json.MarshalIndent(cfg, "", "  "); jsonErr == nil {
			if writeErr := os.WriteFile(ConfigFilename, jsonData, 0644); writeErr == nil {
				_ = os.Remove(legacyFilename)
				fmt.Fprintf(os.Stderr, "ℹ️  migrated %s → %s\n", legacyFilename, ConfigFilename)
			}
		}
		return cfg, true
	}

	return cfg, false
}

func readLegacyData() ([]byte, string, error) {
	if data, err := os.ReadFile(LegacyConfigFilename); err == nil {
		return data, LegacyConfigFilename, nil
	}
	if data, err := os.ReadFile(LegacySkaisserFilename); err == nil {
		return data, LegacySkaisserFilename, nil
	}
	return nil, "", os.ErrNotExist
}

// parseConfig parses data (either JSON or YAML) into cfg, applying staging branch defaults.
func parseConfig(cfg *BravrosConfig, data []byte) (*BravrosConfig, bool) {
	if json.Unmarshal(data, cfg) == nil {
		if cfg.StagingBranch == "" {
			cfg.StagingBranch = "homolog"
		}
		return cfg, true
	}
	if yaml.Unmarshal(data, cfg) == nil {
		if cfg.StagingBranch == "" {
			cfg.StagingBranch = "homolog"
		}
		return cfg, true
	}
	return cfg, false
}

