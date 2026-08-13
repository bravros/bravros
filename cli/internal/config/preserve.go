package config

import (
	"os"
	"path/filepath"
	"sort"
)

// EnabledSkills returns the opt-in allowlist of skill directory names from
// .bravros.yml:skills.enabled. When non-empty, `bravros deploy` only copies
// the listed skills plus any skill with `core: true` in its SKILL.md frontmatter.
// When empty or absent, all skills deploy (backward-compatible default).
func EnabledSkills() []string {
	cfg, found := loadPreserveConfig()
	if !found || len(cfg.Skills.Enabled) == 0 {
		return nil
	}
	return dedup(cfg.Skills.Enabled)
}

// PreservedSkills returns the sorted, deduplicated list of skill directory names
// that should NOT be pruned during deploy operations.
//
// Resolution chain for the .bravros.yml file:
//  1. cwd (.bravros.yml in the current working directory)
//  2. $BRAVROS_PORTABLE_REPO env var (the canonical source repo path)
//  3. $HOME (the user's home directory)
//
// Returns an empty slice when no .bravros.yml is found, when the config has
// no skills.preserve key, or when the preserve list is empty — all equivalent
// to "opt-in not set, prune everything" (the default behavior).
func PreservedSkills() []string {
	cfg, found := loadPreserveConfig()
	if !found || len(cfg.Skills.Preserve) == 0 {
		return nil
	}
	return dedup(cfg.Skills.Preserve)
}

// loadPreserveConfig tries to load .bravros.yml from the resolution chain,
// returning the first config found.
func loadPreserveConfig() (*BravrosConfig, bool) {
	// 1. cwd
	if cfg, ok := loadFromDir("."); ok {
		return cfg, true
	}

	// 2. $BRAVROS_PORTABLE_REPO
	if repo := os.Getenv("BRAVROS_PORTABLE_REPO"); repo != "" {
		if cfg, ok := loadFromDir(repo); ok {
			return cfg, true
		}
	}

	// 3. $HOME
	if home, err := os.UserHomeDir(); err == nil {
		if cfg, ok := loadFromDir(home); ok {
			return cfg, true
		}
	}

	return nil, false
}

// loadFromDir attempts to parse .bravros.yml from the given directory.
func loadFromDir(dir string) (*BravrosConfig, bool) {
	path := filepath.Join(dir, ConfigFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		// Try legacy filename as well.
		legacyPath := filepath.Join(dir, LegacyConfigFilename)
		legacyData, legacyErr := os.ReadFile(legacyPath)
		if legacyErr != nil {
			return nil, false
		}
		data = legacyData
	}
	cfg := &BravrosConfig{StagingBranch: "homolog"}
	return parseConfig(cfg, data)
}

// dedup sorts and deduplicates a string slice.
func dedup(ss []string) []string {
	seen := make(map[string]struct{}, len(ss))
	out := ss[:0:0] // cap=0 forces a new backing array on first append (equivalent to var out []string)
	for _, s := range ss {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
