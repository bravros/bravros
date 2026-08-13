package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

// setupB0286Src creates a minimal source repo with three skills:
//   - alpha-skill: has SKILL.md + scripts/run.sh
//   - beta-skill:  has SKILL.md only
//   - gamma-skill: has SKILL.md only
//
// The repo directory name ends in "claude" to pass IsClaudeRepo.
func setupB0286Src(t *testing.T) string {
	t.Helper()
	base := filepath.Join(t.TempDir(), "claude")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	must(os.MkdirAll(filepath.Join(base, "skills", "alpha-skill", "scripts"), 0755))
	must(os.MkdirAll(filepath.Join(base, "skills", "beta-skill"), 0755))
	must(os.MkdirAll(filepath.Join(base, "skills", "gamma-skill"), 0755))
	must(os.MkdirAll(filepath.Join(base, "config"), 0755))

	must(os.WriteFile(filepath.Join(base, "skills", "alpha-skill", "SKILL.md"), []byte("alpha skill"), 0644))
	must(os.WriteFile(filepath.Join(base, "skills", "alpha-skill", "scripts", "run.sh"), []byte("#!/bin/sh\necho run"), 0755))
	must(os.WriteFile(filepath.Join(base, "skills", "beta-skill", "SKILL.md"), []byte("beta skill"), 0644))
	must(os.WriteFile(filepath.Join(base, "skills", "gamma-skill", "SKILL.md"), []byte("gamma skill"), 0644))

	// Minimal required root files so Deploy doesn't error on fileMappings.
	must(os.WriteFile(filepath.Join(base, "config", "settings.json"), []byte(`{}`), 0644))
	must(os.WriteFile(filepath.Join(base, "CLAUDE.md"), []byte("# Claude"), 0644))

	writeRepoMarkers(t, base)
	return base
}

// TestDeployB0286_FilteredDeploy_IsAdditive verifies that running a filtered
// deploy (EnabledSkills set to a subset) after a full deploy does NOT prune the
// skills that were deployed by the full run but are absent from the filter.
//
// Concretely: full deploy → alpha-skill + beta-skill + gamma-skill all in
// manifest and on-disk. Filtered deploy of only alpha-skill → beta-skill and
// gamma-skill must still exist on disk and remain in the manifest.
func TestDeployB0286_FilteredDeploy_IsAdditive(t *testing.T) {
	src := setupB0286Src(t)
	dst := filepath.Join(t.TempDir(), ".claude")

	// Step 1: full deploy — all three skills deployed.
	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst}); err != nil {
		t.Fatalf("full deploy: %v", err)
	}

	// Verify all three are on disk after the full deploy.
	for _, skill := range []string{"alpha-skill", "beta-skill", "gamma-skill"} {
		dir := filepath.Join(dst, "skills", skill)
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("full deploy: %s not on disk: %v", skill, err)
		}
	}

	// Step 2: filtered deploy — only alpha-skill in filter.
	// FilterMode: true mirrors `kaisser deploy --filter alpha-skill`: additive,
	// non-filtered skills must survive.
	result, err := Deploy(DeployOpts{
		SourceDir:     src,
		TargetDir:     dst,
		EnabledSkills: []string{"alpha-skill"},
		FilterMode:    true,
	})
	if err != nil {
		t.Fatalf("filtered deploy: %v", err)
	}

	// beta-skill and gamma-skill must still exist on disk (additive — not pruned).
	for _, skill := range []string{"beta-skill", "gamma-skill"} {
		dir := filepath.Join(dst, "skills", skill)
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("filtered deploy pruned %s which was outside filter scope: %v", skill, err)
		}
	}

	// beta-skill and gamma-skill must NOT appear in result.Pruned.
	for _, p := range result.Pruned {
		if p == filepath.Join("skills", "beta-skill") || p == filepath.Join("skills", "gamma-skill") {
			t.Errorf("filtered deploy incorrectly reported %s as pruned (additive deploy must preserve non-filtered skills)", p)
		}
	}

	// Manifest must still contain entries for beta-skill and gamma-skill.
	manifestPath := filepath.Join(dst, "skills", manifestFileName)
	m, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest after filtered deploy: %v", err)
	}
	for _, skill := range []string{"beta-skill", "gamma-skill"} {
		if sha, ok := m.Skills[skill]; !ok || sha == "" {
			t.Errorf("filtered deploy removed %s from manifest — additive deploy must preserve non-filtered skills; skills=%v", skill, m.Skills)
		}
	}
}

// TestDeployB0286_FullDeploy_StillPrunesRemovedSkill verifies that the prune
// guard added for filtered deploys does NOT affect full (unfiltered) deploys.
// A skill genuinely removed from source must still be pruned on a full deploy.
func TestDeployB0286_FullDeploy_StillPrunesRemovedSkill(t *testing.T) {
	src := setupB0286Src(t)
	dst := filepath.Join(t.TempDir(), ".claude")

	// Step 1: full deploy — all three skills on disk + in manifest.
	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst}); err != nil {
		t.Fatalf("initial full deploy: %v", err)
	}

	// Verify gamma-skill is on disk.
	gammaRuntime := filepath.Join(dst, "skills", "gamma-skill")
	if _, err := os.Stat(gammaRuntime); err != nil {
		t.Fatalf("gamma-skill not on disk after initial deploy: %v", err)
	}

	// Step 2: remove gamma-skill from source.
	if err := os.RemoveAll(filepath.Join(src, "skills", "gamma-skill")); err != nil {
		t.Fatalf("remove gamma-skill from source: %v", err)
	}

	// Step 3: full deploy (no filter) — gamma-skill must be pruned.
	result, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst})
	if err != nil {
		t.Fatalf("full deploy after source removal: %v", err)
	}

	// gamma-skill runtime dir must be gone.
	if _, err := os.Stat(gammaRuntime); !os.IsNotExist(err) {
		t.Errorf("gamma-skill should be pruned after removal from source (stat err: %v)", err)
	}

	// Must appear in result.Pruned.
	found := false
	for _, p := range result.Pruned {
		if p == filepath.Join("skills", "gamma-skill") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected skills/gamma-skill in result.Pruned after source removal, got %v", result.Pruned)
	}

	// Must be removed from manifest.
	manifestPath := filepath.Join(dst, "skills", manifestFileName)
	m, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest after full prune deploy: %v", err)
	}
	if _, ok := m.Skills["gamma-skill"]; ok {
		t.Errorf("gamma-skill should be removed from manifest after full deploy prune, skills=%v", m.Skills)
	}

	// alpha-skill and beta-skill must still be present (not collateral damage).
	for _, skill := range []string{"alpha-skill", "beta-skill"} {
		if sha, ok := m.Skills[skill]; !ok || sha == "" {
			t.Errorf("full prune deploy unexpectedly removed %s from manifest; skills=%v", skill, m.Skills)
		}
		dir := filepath.Join(dst, "skills", skill)
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("full prune deploy unexpectedly removed %s from disk: %v", skill, err)
		}
	}
}
