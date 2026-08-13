package config

import (
	"os"
	"path/filepath"

	"github.com/bravros/bravros/cli/internal/paths"
)

// Env-var externalization — B-0115.
//
// Each getter reads its KAISSER_* environment variable first; if unset, it
// falls back to the OS-aware default.  This allows operators and CI systems to
// override every hardcoded path/vault without modifying any config file.
//
// Env var → default mapping:
//
//   KAISSER_PORTABLE_REPO   ~/Sites/claude (macOS) / ~/claude (Linux)
//   KAISSER_CONFIG_DIR      ~/.claude
//   KAISSER_PLANNING_DIR    .planning
//   KAISSER_OP_VAULT        ClaudeCode
//   KAISSER_BASE_BRANCH     homolog
//   KAISSER_HASS_SERVER     homeassistant.local:8123
//   KAISSER_HASS_ENTITY_ID  input_boolean.claude_session_lock
//   KAISSER_DEPLOY_MODE     symlinks

// PortableRepo returns the path to the kaisser source repository.
// Reads KAISSER_PORTABLE_REPO; defaults to the OS-aware path from paths package.
func PortableRepo() string {
	if v := os.Getenv("KAISSER_PORTABLE_REPO"); v != "" {
		return v
	}
	return paths.PortableRepoDir()
}

// ConfigDir returns the path to the deployed ~/.claude directory.
// Reads KAISSER_CONFIG_DIR; defaults to ~/.claude.
func ConfigDir() string {
	if v := os.Getenv("KAISSER_CONFIG_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("~", ".claude")
	}
	return filepath.Join(home, ".claude")
}

// PlanningDir returns the relative planning directory name within a repo.
// Reads KAISSER_PLANNING_DIR; defaults to ".planning".
func PlanningDir() string {
	if v := os.Getenv("KAISSER_PLANNING_DIR"); v != "" {
		return v
	}
	return ".planning"
}

// OpVault returns the 1Password vault name used by kaisser secrets.
// Reads KAISSER_OP_VAULT; defaults to "ClaudeCode".
func OpVault() string {
	if v := os.Getenv("KAISSER_OP_VAULT"); v != "" {
		return v
	}
	return "ClaudeCode"
}

// BaseBranch returns the staging/integration branch name.
// Reads KAISSER_BASE_BRANCH; defaults to "homolog".
func BaseBranch() string {
	if v := os.Getenv("KAISSER_BASE_BRANCH"); v != "" {
		return v
	}
	return "homolog"
}

// HassServer returns the Home Assistant server address.
// Reads KAISSER_HASS_SERVER; defaults to "homeassistant.local:8123".
func HassServer() string {
	if v := os.Getenv("KAISSER_HASS_SERVER"); v != "" {
		return v
	}
	return "homeassistant.local:8123"
}

// HassEntityID returns the Home Assistant entity ID used for mac lock/unlock.
// Reads KAISSER_HASS_ENTITY_ID; defaults to "input_boolean.claude_session_lock".
func HassEntityID() string {
	if v := os.Getenv("KAISSER_HASS_ENTITY_ID"); v != "" {
		return v
	}
	return "input_boolean.claude_session_lock"
}

// DeployMode returns the preferred deployment mode ("symlinks" or "copies").
// Reads KAISSER_DEPLOY_MODE; defaults to "symlinks".
func DeployMode() string {
	if v := os.Getenv("KAISSER_DEPLOY_MODE"); v != "" {
		return v
	}
	return "symlinks"
}
