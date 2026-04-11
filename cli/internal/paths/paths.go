package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// PortableRepoDir returns the default portable repo directory.
// On macOS (darwin): ~/Sites/claude
// On Linux: ~/claude
func PortableRepoDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Sites", "claude")
	}
	return filepath.Join(home, "claude")
}

// AuthCachePath returns the path used to cache the activated license JWT.
// The file is stored at ~/.claude/.bravros-auth and is created with mode 0600.
func AuthCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", ".bravros-auth")
}

// SkillsDir returns the path to the installed skills directory (~/.claude/skills/).
func SkillsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "skills")
}

// SkillManifestCachePath returns the path for the cached skill manifest.
// The file is stored at ~/.claude/.bravros-skill-manifest.json.
func SkillManifestCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", ".bravros-skill-manifest.json")
}
