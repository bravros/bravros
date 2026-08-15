package config

import (
	"os"
	"path/filepath"
)

// Env-var externalization — B-0115.
//
// Each getter below reads its BRAVROS_* environment variable first; if unset,
// it falls back to the OS-aware default. This allows operators and CI systems
// to override every hardcoded path/vault without modifying any config file.
//
// Env var → default mapping:
//
//   BRAVROS_CONFIG_DIR      ~/.claude
//   BRAVROS_PLANNING_DIR    .planning
//   BRAVROS_OP_VAULT        ClaudeCode
//   BRAVROS_BASE_BRANCH     homolog
//   BRAVROS_HASS_SERVER     homeassistant.local:8123
//   BRAVROS_HASS_ENTITY_ID  input_boolean.claude_session_lock
//   BRAVROS_DEPLOY_MODE     symlinks
//
// BRAVROS_PORTABLE_REPO has no getter in this file. It is no longer a
// selfupdate source (the clone-based selfupdate lane was retired) — it now
// serves only as a lookup hint consumed directly by
// config.PreservedSkills() (see preserve.go), used as step 2 of the
// .bravros.yml resolution chain (cwd → $BRAVROS_PORTABLE_REPO → $HOME).

// ConfigDir returns the path to the deployed ~/.claude directory.
// Reads BRAVROS_CONFIG_DIR; defaults to ~/.claude.
func ConfigDir() string {
	if v := os.Getenv("BRAVROS_CONFIG_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("~", ".claude")
	}
	return filepath.Join(home, ".claude")
}

// PlanningDir returns the relative planning directory name within a repo.
// Reads BRAVROS_PLANNING_DIR; defaults to ".planning".
func PlanningDir() string {
	if v := os.Getenv("BRAVROS_PLANNING_DIR"); v != "" {
		return v
	}
	return ".planning"
}

// OpVault returns the 1Password vault name used by bravros secrets.
// Reads BRAVROS_OP_VAULT; defaults to "ClaudeCode".
func OpVault() string {
	if v := os.Getenv("BRAVROS_OP_VAULT"); v != "" {
		return v
	}
	return "ClaudeCode"
}

// BaseBranch returns the staging/integration branch name.
// Reads BRAVROS_BASE_BRANCH; defaults to "homolog".
func BaseBranch() string {
	if v := os.Getenv("BRAVROS_BASE_BRANCH"); v != "" {
		return v
	}
	return "homolog"
}

// HassServer returns the Home Assistant server address.
// Reads BRAVROS_HASS_SERVER; defaults to "homeassistant.local:8123".
func HassServer() string {
	if v := os.Getenv("BRAVROS_HASS_SERVER"); v != "" {
		return v
	}
	return "homeassistant.local:8123"
}

// HassEntityID returns the Home Assistant entity ID used for mac lock/unlock.
// Reads BRAVROS_HASS_ENTITY_ID; defaults to "input_boolean.claude_session_lock".
func HassEntityID() string {
	if v := os.Getenv("BRAVROS_HASS_ENTITY_ID"); v != "" {
		return v
	}
	return "input_boolean.claude_session_lock"
}

// DeployMode returns the preferred deployment mode ("symlinks" or "copies").
// Reads BRAVROS_DEPLOY_MODE; defaults to "symlinks".
func DeployMode() string {
	if v := os.Getenv("BRAVROS_DEPLOY_MODE"); v != "" {
		return v
	}
	return "symlinks"
}
