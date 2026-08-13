package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEnv_DefaultsByOS(t *testing.T) {
	// Unset all KAISSER_* env vars to test defaults.
	vars := []string{
		"KAISSER_PORTABLE_REPO",
		"KAISSER_CONFIG_DIR",
		"KAISSER_PLANNING_DIR",
		"KAISSER_OP_VAULT",
		"KAISSER_BASE_BRANCH",
		"KAISSER_HASS_SERVER",
		"KAISSER_HASS_ENTITY_ID",
		"KAISSER_DEPLOY_MODE",
	}
	for _, v := range vars {
		t.Setenv(v, "")
	}

	home, _ := os.UserHomeDir()

	// PortableRepo — OS-dependent
	got := PortableRepo()
	var wantRepo string
	if runtime.GOOS == "darwin" {
		wantRepo = filepath.Join(home, "Sites", "claude")
	} else {
		wantRepo = filepath.Join(home, "claude")
	}
	if got != wantRepo {
		t.Errorf("PortableRepo() = %q, want %q", got, wantRepo)
	}

	// ConfigDir
	if want := filepath.Join(home, ".claude"); ConfigDir() != want {
		t.Errorf("ConfigDir() = %q, want %q", ConfigDir(), want)
	}

	// PlanningDir
	if want := ".planning"; PlanningDir() != want {
		t.Errorf("PlanningDir() = %q, want %q", PlanningDir(), want)
	}

	// OpVault
	if want := "ClaudeCode"; OpVault() != want {
		t.Errorf("OpVault() = %q, want %q", OpVault(), want)
	}

	// BaseBranch
	if want := "homolog"; BaseBranch() != want {
		t.Errorf("BaseBranch() = %q, want %q", BaseBranch(), want)
	}

	// HassServer
	if want := "homeassistant.local:8123"; HassServer() != want {
		t.Errorf("HassServer() = %q, want %q", HassServer(), want)
	}

	// HassEntityID
	if want := "input_boolean.claude_session_lock"; HassEntityID() != want {
		t.Errorf("HassEntityID() = %q, want %q", HassEntityID(), want)
	}

	// DeployMode
	if want := "symlinks"; DeployMode() != want {
		t.Errorf("DeployMode() = %q, want %q", DeployMode(), want)
	}
}

func TestEnv_EnvOverride(t *testing.T) {
	t.Setenv("KAISSER_PORTABLE_REPO", "/custom/repo")
	t.Setenv("KAISSER_CONFIG_DIR", "/custom/.claude")
	t.Setenv("KAISSER_PLANNING_DIR", "custom-planning")
	t.Setenv("KAISSER_OP_VAULT", "MyVault")
	t.Setenv("KAISSER_BASE_BRANCH", "main")
	t.Setenv("KAISSER_HASS_SERVER", "my-ha.local:8123")
	t.Setenv("KAISSER_HASS_ENTITY_ID", "input_boolean.my_lock")
	t.Setenv("KAISSER_DEPLOY_MODE", "copies")

	if got, want := PortableRepo(), "/custom/repo"; got != want {
		t.Errorf("PortableRepo() = %q, want %q", got, want)
	}
	if got, want := ConfigDir(), "/custom/.claude"; got != want {
		t.Errorf("ConfigDir() = %q, want %q", got, want)
	}
	if got, want := PlanningDir(), "custom-planning"; got != want {
		t.Errorf("PlanningDir() = %q, want %q", got, want)
	}
	if got, want := OpVault(), "MyVault"; got != want {
		t.Errorf("OpVault() = %q, want %q", got, want)
	}
	if got, want := BaseBranch(), "main"; got != want {
		t.Errorf("BaseBranch() = %q, want %q", got, want)
	}
	if got, want := HassServer(), "my-ha.local:my-ha.local:8123"; got == want {
		t.Errorf("HassServer() = %q, unexpected match with malformed want", got)
	}
	if got, want := HassServer(), "my-ha.local:8123"; got != want {
		t.Errorf("HassServer() = %q, want %q", got, want)
	}
	if got, want := HassEntityID(), "input_boolean.my_lock"; got != want {
		t.Errorf("HassEntityID() = %q, want %q", got, want)
	}
	if got, want := DeployMode(), "copies"; got != want {
		t.Errorf("DeployMode() = %q, want %q", got, want)
	}
}
