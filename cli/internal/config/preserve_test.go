package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeKaisserYML writes a JSON config with a skills.preserve list to dir.
func writeKaisserYML(t *testing.T, dir string, skills []string) {
	t.Helper()
	type SkillObj struct {
		Preserve []string `json:"preserve"`
	}
	type ConfigObj struct {
		StagingBranch string   `json:"staging_branch"`
		Skills        SkillObj `json:"skills"`
	}
	cfg := ConfigObj{
		StagingBranch: "homolog",
		Skills: SkillObj{
			Preserve: skills,
		},
	}
	jsonData, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	target := filepath.Join(dir, ConfigFilename)
	targetDir := filepath.Dir(target)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.WriteFile(target, jsonData, 0644); err != nil {
		t.Fatalf("writeKaisserYML: %v", err)
	}
}

func TestPreservedSkills_Dedup(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()

	writeKaisserYML(t, dir, []string{"graphify", "graphify", "myplugin"})
	got := PreservedSkills()
	if len(got) != 2 {
		t.Fatalf("expected 2 deduplicated skills, got %d: %v", len(got), got)
	}
	// dedup also sorts
	if got[0] != "graphify" || got[1] != "myplugin" {
		t.Fatalf("expected [graphify myplugin], got %v", got)
	}
}

func TestPreservedSkills_CwdWinsOverEnv(t *testing.T) {
	orig, _ := os.Getwd()
	cwdDir := t.TempDir()
	envDir := t.TempDir()
	if err := os.Chdir(cwdDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()

	writeKaisserYML(t, cwdDir, []string{"cwd-skill"})
	writeKaisserYML(t, envDir, []string{"env-skill"})

	t.Setenv("KAISSER_PORTABLE_REPO", envDir)

	got := PreservedSkills()
	if len(got) != 1 || got[0] != "cwd-skill" {
		t.Fatalf("expected cwd to win (cwd-skill), got %v", got)
	}
}

func TestPreservedSkills_EnvWinsOverHome(t *testing.T) {
	orig, _ := os.Getwd()
	cwdDir := t.TempDir() // no .kaisser.yml here
	envDir := t.TempDir()
	homeDir := t.TempDir()
	if err := os.Chdir(cwdDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()

	writeKaisserYML(t, envDir, []string{"env-skill"})
	writeKaisserYML(t, homeDir, []string{"home-skill"})

	t.Setenv("KAISSER_PORTABLE_REPO", envDir)
	t.Setenv("HOME", homeDir)

	got := PreservedSkills()
	if len(got) != 1 || got[0] != "env-skill" {
		t.Fatalf("expected env to win (env-skill), got %v", got)
	}
}

func TestPreservedSkills_NonePresent(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir() // no .kaisser.yml
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()

	t.Setenv("KAISSER_PORTABLE_REPO", "")
	t.Setenv("HOME", dir) // also has no .kaisser.yml

	got := PreservedSkills()
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestPreservedSkills_EmptyPreserveList(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()

	// .kaisser.yml exists but skills.preserve is empty
	writeKaisserYML(t, dir, nil)
	got := PreservedSkills()
	if got != nil {
		t.Fatalf("expected nil for empty preserve list, got %v", got)
	}
}

func TestDedup(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{"empty", nil, nil},
		{"single", []string{"a"}, []string{"a"}},
		{"duplicates removed", []string{"b", "a", "b", "c", "a"}, []string{"a", "b", "c"}},
		{"empty strings skipped", []string{"a", "", "b"}, []string{"a", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := dedup(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("dedup(%v) = %v, want %v", tc.input, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("dedup[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}
