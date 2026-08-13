package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

// setupB0256Src creates a minimal source repo with two skills:
//   - alpha-skill: has SKILL.md + scripts/run.sh
//   - beta-skill:  has SKILL.md only
//
// The repo directory name ends in "claude" to pass IsClaudeRepo.
func setupB0256Src(t *testing.T) string {
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
	must(os.MkdirAll(filepath.Join(base, "config"), 0755))

	must(os.WriteFile(filepath.Join(base, "skills", "alpha-skill", "SKILL.md"), []byte("alpha skill"), 0644))
	must(os.WriteFile(filepath.Join(base, "skills", "alpha-skill", "scripts", "run.sh"), []byte("#!/bin/sh\necho run"), 0755))
	must(os.WriteFile(filepath.Join(base, "skills", "beta-skill", "SKILL.md"), []byte("beta skill"), 0644))

	// Minimal required root files so Deploy doesn't error on fileMappings.
	must(os.WriteFile(filepath.Join(base, "config", "settings.json"), []byte(`{}`), 0644))
	must(os.WriteFile(filepath.Join(base, "CLAUDE.md"), []byte("# Claude"), 0644))

	writeRepoMarkers(t, base)
	return base
}

// TestDeployB0256_RenameWithinSkill_RemovesOldFile verifies that renaming a
// file inside a skill (scripts/old.sh → scripts/new.sh) and redeploying
// results in ONLY scripts/new.sh present at the runtime — scripts/old.sh is
// gone because the per-skill atomic wipe+recopy path is used when SHA changes.
func TestDeployB0256_RenameWithinSkill_RemovesOldFile(t *testing.T) {
	src := setupB0256Src(t)
	dst := filepath.Join(t.TempDir(), ".claude")

	// Initial deploy: alpha-skill has scripts/run.sh.
	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst}); err != nil {
		t.Fatalf("initial deploy: %v", err)
	}

	// Verify the original file is deployed.
	oldFile := filepath.Join(dst, "skills", "alpha-skill", "scripts", "run.sh")
	if _, err := os.Stat(oldFile); err != nil {
		t.Fatalf("initial deploy: scripts/run.sh missing: %v", err)
	}

	// Rename in source: remove scripts/run.sh, add scripts/deploy.sh.
	if err := os.Remove(filepath.Join(src, "skills", "alpha-skill", "scripts", "run.sh")); err != nil {
		t.Fatalf("remove source run.sh: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "skills", "alpha-skill", "scripts", "deploy.sh"), []byte("#!/bin/sh\necho deploy"), 0755); err != nil {
		t.Fatalf("write deploy.sh: %v", err)
	}

	// Redeploy: SHA changed → atomic wipe+recopy.
	result, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst})
	if err != nil {
		t.Fatalf("redeploy: %v", err)
	}

	// Old file must be gone.
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("scripts/run.sh should be gone after rename (stat err: %v)", err)
	}

	// New file must be present.
	newFile := filepath.Join(dst, "skills", "alpha-skill", "scripts", "deploy.sh")
	if _, err := os.Stat(newFile); err != nil {
		t.Fatalf("scripts/deploy.sh missing after rename: %v", err)
	}

	// alpha-skill must appear in SkillsDeployed (not skipped).
	found := false
	for _, s := range result.SkillsDeployed {
		if s == "alpha-skill" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected alpha-skill in SkillsDeployed, got %v", result.SkillsDeployed)
	}
}

// TestDeployB0256_DeleteFileWithinSkill_PrunesAtomically verifies that
// deleting a file from a skill in source and redeploying removes the file
// from the runtime because the per-skill atomic wipe+recopy path is used.
func TestDeployB0256_DeleteFileWithinSkill_PrunesAtomically(t *testing.T) {
	src := setupB0256Src(t)
	dst := filepath.Join(t.TempDir(), ".claude")

	// Add a second file to alpha-skill that we will later delete.
	if err := os.WriteFile(filepath.Join(src, "skills", "alpha-skill", "references", "extra.md"), []byte("extra"), 0644); err != nil {
		// references dir doesn't exist yet; create it.
		if mkErr := os.MkdirAll(filepath.Join(src, "skills", "alpha-skill", "references"), 0755); mkErr != nil {
			t.Fatalf("mkdir references: %v", mkErr)
		}
		if err := os.WriteFile(filepath.Join(src, "skills", "alpha-skill", "references", "extra.md"), []byte("extra"), 0644); err != nil {
			t.Fatalf("write extra.md: %v", err)
		}
	}

	// Initial deploy.
	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst}); err != nil {
		t.Fatalf("initial deploy: %v", err)
	}

	extraDst := filepath.Join(dst, "skills", "alpha-skill", "references", "extra.md")
	if _, err := os.Stat(extraDst); err != nil {
		t.Fatalf("extra.md not deployed initially: %v", err)
	}

	// Delete from source.
	if err := os.Remove(filepath.Join(src, "skills", "alpha-skill", "references", "extra.md")); err != nil {
		t.Fatalf("remove source extra.md: %v", err)
	}

	// Redeploy.
	result, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst})
	if err != nil {
		t.Fatalf("redeploy after delete: %v", err)
	}

	// Deleted file must be gone (atomic wipe+recopy removed it).
	if _, err := os.Stat(extraDst); !os.IsNotExist(err) {
		t.Errorf("extra.md should be absent after source deletion (stat err: %v)", err)
	}

	// alpha-skill was redeployed (SHA changed).
	found := false
	for _, s := range result.SkillsDeployed {
		if s == "alpha-skill" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected alpha-skill in SkillsDeployed, got %v", result.SkillsDeployed)
	}
}

// TestDeployB0256_UnchangedSkill_IsSkipped verifies that when source files are
// not modified between deploys, the skill SHA matches and the skill is listed
// in result.SkillsSkipped (not redeployed). Runtime file mtimes must not change
// because no copy occurs.
func TestDeployB0256_UnchangedSkill_IsSkipped(t *testing.T) {
	src := setupB0256Src(t)
	dst := filepath.Join(t.TempDir(), ".claude")

	// Initial deploy to populate manifest.
	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst}); err != nil {
		t.Fatalf("initial deploy: %v", err)
	}

	// Capture mtime of a deployed file before second deploy.
	skillMD := filepath.Join(dst, "skills", "alpha-skill", "SKILL.md")
	beforeInfo, err := os.Stat(skillMD)
	if err != nil {
		t.Fatalf("stat skill SKILL.md: %v", err)
	}
	beforeMtime := beforeInfo.ModTime()

	// Second deploy: no source changes.
	result, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst})
	if err != nil {
		t.Fatalf("second deploy: %v", err)
	}

	// alpha-skill must be in SkillsSkipped, not SkillsDeployed.
	inSkipped := false
	for _, s := range result.SkillsSkipped {
		if s == "alpha-skill" {
			inSkipped = true
			break
		}
	}
	if !inSkipped {
		t.Errorf("expected alpha-skill in SkillsSkipped, got skipped=%v deployed=%v", result.SkillsSkipped, result.SkillsDeployed)
	}
	for _, s := range result.SkillsDeployed {
		if s == "alpha-skill" {
			t.Errorf("alpha-skill should NOT be in SkillsDeployed when unchanged")
		}
	}

	// SKILL.md mtime must not have changed (no copy occurred).
	afterInfo, err := os.Stat(skillMD)
	if err != nil {
		t.Fatalf("stat skill SKILL.md after second deploy: %v", err)
	}
	if !afterInfo.ModTime().Equal(beforeMtime) {
		t.Errorf("SKILL.md mtime changed on skipped skill: before=%v after=%v", beforeMtime, afterInfo.ModTime())
	}
}

// TestDeployB0256_FirstDeploy_NoManifest_DeploysAll verifies that a fresh
// destination with no .deploy-manifest.json causes every enabled skill to be
// deployed and the manifest file to be written with one entry per skill.
func TestDeployB0256_FirstDeploy_NoManifest_DeploysAll(t *testing.T) {
	src := setupB0256Src(t)
	dst := filepath.Join(t.TempDir(), ".claude")

	// No manifest exists yet at dst.
	manifestPath := filepath.Join(dst, "skills", manifestFileName)
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Fatalf("manifest should not exist before first deploy")
	}

	result, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst})
	if err != nil {
		t.Fatalf("first deploy: %v", err)
	}

	// Both skills must be deployed.
	deployed := map[string]bool{}
	for _, s := range result.SkillsDeployed {
		deployed[s] = true
	}
	for _, skill := range []string{"alpha-skill", "beta-skill"} {
		if !deployed[skill] {
			t.Errorf("expected %s in SkillsDeployed on first deploy, got %v", skill, result.SkillsDeployed)
		}
	}
	if len(result.SkillsSkipped) != 0 {
		t.Errorf("no skills should be skipped on first deploy, got %v", result.SkillsSkipped)
	}

	// Manifest must be written.
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest not written after first deploy: %v", err)
	}

	// Load and verify manifest has entries for both skills.
	m, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	for _, skill := range []string{"alpha-skill", "beta-skill"} {
		if sha, ok := m.Skills[skill]; !ok || sha == "" {
			t.Errorf("manifest missing or empty SHA for %s: skills=%v", skill, m.Skills)
		}
	}
}

// TestDeployB0256_SymlinkInSource_MaterializedAsFile verifies that a symlink
// inside a source skill (references/shared.md → ../../shared/foo.md) is
// materialized as a regular file at the destination — NOT as a symlink —
// containing the resolved target's content.
func TestDeployB0256_SymlinkInSource_MaterializedAsFile(t *testing.T) {
	src := setupB0256Src(t)
	dst := filepath.Join(t.TempDir(), ".claude")

	// Create shared dir with target file (sibling to skill dirs).
	sharedDir := filepath.Join(src, "skills", "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatalf("mkdir shared: %v", err)
	}
	sharedContent := "# Shared reference content"
	if err := os.WriteFile(filepath.Join(sharedDir, "foo.md"), []byte(sharedContent), 0644); err != nil {
		t.Fatalf("write shared/foo.md: %v", err)
	}

	// Create a references/ dir inside alpha-skill with a symlink to the shared file.
	refsDir := filepath.Join(src, "skills", "alpha-skill", "references")
	if err := os.MkdirAll(refsDir, 0755); err != nil {
		t.Fatalf("mkdir references: %v", err)
	}
	// Relative symlink: references/shared.md → ../../shared/foo.md
	symlinkTarget := filepath.Join("..", "..", "shared", "foo.md")
	if err := os.Symlink(symlinkTarget, filepath.Join(refsDir, "shared.md")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	// Deploy.
	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst}); err != nil {
		t.Fatalf("deploy: %v", err)
	}

	deployedRef := filepath.Join(dst, "skills", "alpha-skill", "references", "shared.md")

	// Must exist.
	if _, err := os.Stat(deployedRef); err != nil {
		t.Fatalf("materialized reference missing: %v", err)
	}

	// Must NOT be a symlink.
	linfo, err := os.Lstat(deployedRef)
	if err != nil {
		t.Fatalf("Lstat deployed ref: %v", err)
	}
	if linfo.Mode()&os.ModeSymlink != 0 {
		t.Errorf("deployed reference should be a regular file, not a symlink")
	}

	// Content must match the shared target.
	got, err := os.ReadFile(deployedRef)
	if err != nil {
		t.Fatalf("read deployed ref: %v", err)
	}
	if string(got) != sharedContent {
		t.Errorf("deployed ref content = %q, want %q", string(got), sharedContent)
	}
}

// TestDeployB0256_SkillLevelOrphan_Pruned verifies that a skill present in the
// manifest (and previously deployed) but absent from source is removed from the
// runtime and deleted from the manifest on the next deploy.
func TestDeployB0256_SkillLevelOrphan_Pruned(t *testing.T) {
	src := setupB0256Src(t)
	dst := filepath.Join(t.TempDir(), ".claude")

	// Initial deploy: both alpha-skill and beta-skill are in source.
	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst}); err != nil {
		t.Fatalf("initial deploy: %v", err)
	}

	// Simulate "obsolete-skill" already in manifest + runtime (injected directly).
	obsoleteRuntime := filepath.Join(dst, "skills", "obsolete-skill")
	if err := os.MkdirAll(obsoleteRuntime, 0755); err != nil {
		t.Fatalf("mkdir obsolete-skill runtime: %v", err)
	}
	if err := os.WriteFile(filepath.Join(obsoleteRuntime, "SKILL.md"), []byte("obsolete"), 0644); err != nil {
		t.Fatalf("write obsolete SKILL.md: %v", err)
	}
	// Inject into manifest.
	manifestPath := filepath.Join(dst, "skills", manifestFileName)
	m, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest for injection: %v", err)
	}
	m.Skills["obsolete-skill"] = "deadbeef"
	if err := SaveManifest(manifestPath, m); err != nil {
		t.Fatalf("save manifest with obsolete entry: %v", err)
	}

	// Redeploy: obsolete-skill not in source → should be pruned.
	result, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst})
	if err != nil {
		t.Fatalf("redeploy: %v", err)
	}

	// Runtime dir must be gone.
	if _, err := os.Stat(obsoleteRuntime); !os.IsNotExist(err) {
		t.Errorf("obsolete-skill runtime dir should be pruned (stat err: %v)", err)
	}

	// Must appear in result.Pruned.
	found := false
	for _, p := range result.Pruned {
		if p == filepath.Join("skills", "obsolete-skill") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected skills/obsolete-skill in result.Pruned, got %v", result.Pruned)
	}

	// Must be removed from manifest.
	m2, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest after prune: %v", err)
	}
	if _, ok := m2.Skills["obsolete-skill"]; ok {
		t.Errorf("obsolete-skill should be removed from manifest after prune, skills=%v", m2.Skills)
	}
}

// TestDeployB0256_NoPruneFlag_KeepsOrphanedSkill verifies that running with
// NoPrune: true leaves an orphaned manifest skill's runtime directory intact
// and does NOT remove it from the manifest. The skill may appear in
// result.Pruned as a reporting entry ("would-be-pruned"), but no actual
// removal occurs on disk or in the manifest.
func TestDeployB0256_NoPruneFlag_KeepsOrphanedSkill(t *testing.T) {
	src := setupB0256Src(t)
	dst := filepath.Join(t.TempDir(), ".claude")

	// Initial deploy.
	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst}); err != nil {
		t.Fatalf("initial deploy: %v", err)
	}

	// Inject orphan into manifest + runtime.
	orphanRuntime := filepath.Join(dst, "skills", "kept-orphan")
	if err := os.MkdirAll(orphanRuntime, 0755); err != nil {
		t.Fatalf("mkdir kept-orphan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(orphanRuntime, "SKILL.md"), []byte("kept"), 0644); err != nil {
		t.Fatalf("write kept-orphan SKILL.md: %v", err)
	}
	manifestPath := filepath.Join(dst, "skills", manifestFileName)
	m, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	m.Skills["kept-orphan"] = "cafebabe"
	if err := SaveManifest(manifestPath, m); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	// Redeploy with NoPrune.
	_, err = Deploy(DeployOpts{SourceDir: src, TargetDir: dst, NoPrune: true})
	if err != nil {
		t.Fatalf("NoPrune deploy: %v", err)
	}

	// Primary assertion: orphan runtime dir must still exist on disk.
	if _, err := os.Stat(orphanRuntime); err != nil {
		t.Errorf("kept-orphan runtime dir should remain with NoPrune=true: %v", err)
	}

	// Manifest must still retain the orphan entry (no delete when NoPrune).
	m2, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest after NoPrune deploy: %v", err)
	}
	if _, ok := m2.Skills["kept-orphan"]; !ok {
		t.Errorf("kept-orphan should remain in manifest with NoPrune=true, skills=%v", m2.Skills)
	}
}

// TestDeployB0256_ForceFlag_BypassesSHASkip verifies that --force causes an
// unchanged skill (whose SHA matches the manifest) to be redeployed and counted
// in SkillsDeployed rather than SkillsSkipped.
func TestDeployB0256_ForceFlag_BypassesSHASkip(t *testing.T) {
	src := setupB0256Src(t)
	dst := filepath.Join(t.TempDir(), ".claude")

	// Initial deploy to populate manifest.
	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst}); err != nil {
		t.Fatalf("initial deploy: %v", err)
	}

	// Verify that without Force, the skill is skipped (SHA unchanged).
	resultNoForce, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst})
	if err != nil {
		t.Fatalf("incremental deploy: %v", err)
	}
	inSkipped := false
	for _, s := range resultNoForce.SkillsSkipped {
		if s == "alpha-skill" {
			inSkipped = true
			break
		}
	}
	if !inSkipped {
		t.Errorf("expected alpha-skill in SkillsSkipped (no force), got skipped=%v", resultNoForce.SkillsSkipped)
	}

	// Now force: alpha-skill must be redeployed.
	resultForce, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst, Force: true})
	if err != nil {
		t.Fatalf("force deploy: %v", err)
	}

	inDeployed := false
	for _, s := range resultForce.SkillsDeployed {
		if s == "alpha-skill" {
			inDeployed = true
			break
		}
	}
	if !inDeployed {
		t.Errorf("expected alpha-skill in SkillsDeployed with Force=true, got deployed=%v", resultForce.SkillsDeployed)
	}
	for _, s := range resultForce.SkillsSkipped {
		if s == "alpha-skill" {
			t.Errorf("alpha-skill should NOT be in SkillsSkipped with Force=true")
		}
	}
}

// TestDeployB0256_DryRun_TouchesNothing verifies that --dry-run with a pending
// source change does not modify any runtime files or the manifest. The result
// must still populate SkillsDeployed (what would be deployed) so callers can
// preview the work.
func TestDeployB0256_DryRun_TouchesNothing(t *testing.T) {
	src := setupB0256Src(t)
	dst := filepath.Join(t.TempDir(), ".claude")

	// Initial real deploy to populate manifest.
	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst}); err != nil {
		t.Fatalf("initial deploy: %v", err)
	}

	// Capture manifest content before dry-run.
	manifestPath := filepath.Join(dst, "skills", manifestFileName)
	manifestBefore, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest before dry-run: %v", err)
	}

	// Capture SKILL.md mtime before dry-run.
	skillMD := filepath.Join(dst, "skills", "alpha-skill", "SKILL.md")
	infoBefore, err := os.Stat(skillMD)
	if err != nil {
		t.Fatalf("stat SKILL.md before dry-run: %v", err)
	}

	// Mutate source so a change would normally trigger redeploy.
	if err := os.WriteFile(filepath.Join(src, "skills", "alpha-skill", "SKILL.md"), []byte("alpha skill MODIFIED"), 0644); err != nil {
		t.Fatalf("modify source SKILL.md: %v", err)
	}

	// Dry-run deploy.
	result, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst, DryRun: true})
	if err != nil {
		t.Fatalf("dry-run deploy: %v", err)
	}

	if !result.DryRun {
		t.Errorf("expected DryRun=true in result")
	}

	// Manifest file must be unchanged.
	manifestAfter, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest after dry-run: %v", err)
	}
	if string(manifestAfter) != string(manifestBefore) {
		t.Errorf("manifest was modified during dry-run:\nbefore: %s\nafter:  %s", manifestBefore, manifestAfter)
	}

	// Runtime SKILL.md must be unchanged (mtime same, content same).
	infoAfter, err := os.Stat(skillMD)
	if err != nil {
		t.Fatalf("stat SKILL.md after dry-run: %v", err)
	}
	if !infoAfter.ModTime().Equal(infoBefore.ModTime()) {
		t.Errorf("SKILL.md mtime changed during dry-run: before=%v after=%v", infoBefore.ModTime(), infoAfter.ModTime())
	}

	// alpha-skill should appear in SkillsDeployed (preview set, would-be-deployed).
	found := false
	for _, s := range result.SkillsDeployed {
		if s == "alpha-skill" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected alpha-skill in dry-run SkillsDeployed preview, got %v", result.SkillsDeployed)
	}
}

// TestDeployB0256_DisabledSkill_TreatedAsOrphan verifies that when a skill is
// present in the manifest but excluded from the enabled-skills allowlist, it is
// treated as "not in source" and pruned from both the runtime and the manifest.
func TestDeployB0256_DisabledSkill_TreatedAsOrphan(t *testing.T) {
	src := setupB0256Src(t)
	dst := filepath.Join(t.TempDir(), ".claude")

	// Initial full deploy (no allowlist filter): both skills deployed.
	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst}); err != nil {
		t.Fatalf("initial deploy: %v", err)
	}

	betaRuntime := filepath.Join(dst, "skills", "beta-skill")
	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: dst}); err != nil {
		t.Fatalf("verify beta deployed: %v", err)
	}
	if _, err := os.Stat(betaRuntime); err != nil {
		t.Fatalf("beta-skill not deployed after initial: %v", err)
	}

	// Redeploy with allowlist that excludes beta-skill.
	// beta-skill exists in source but is disabled → treated as orphan.
	result, err := Deploy(DeployOpts{
		SourceDir:     src,
		TargetDir:     dst,
		EnabledSkills: []string{"alpha-skill"},
	})
	if err != nil {
		t.Fatalf("allowlist deploy: %v", err)
	}

	// beta-skill runtime dir must be pruned.
	if _, err := os.Stat(betaRuntime); !os.IsNotExist(err) {
		t.Errorf("beta-skill should be pruned when disabled via allowlist (stat err: %v)", err)
	}

	// Must appear in result.Pruned.
	found := false
	for _, p := range result.Pruned {
		if p == filepath.Join("skills", "beta-skill") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected skills/beta-skill in result.Pruned, got %v", result.Pruned)
	}

	// Must be removed from manifest.
	manifestPath := filepath.Join(dst, "skills", manifestFileName)
	m, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest after allowlist deploy: %v", err)
	}
	if _, ok := m.Skills["beta-skill"]; ok {
		t.Errorf("beta-skill should be removed from manifest when disabled, skills=%v", m.Skills)
	}
}
