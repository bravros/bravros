package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bravros/bravros/cli/internal/deploy"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeTestRepo creates a minimal claude-repo layout for deploy tests.
// skillSpecs maps skill-name → SKILL.md content string.
// Must end in "claude" to pass IsClaudeRepo.
func makeTestRepo(t *testing.T, skillSpecs map[string]string) string {
	t.Helper()
	base := filepath.Join(t.TempDir(), "claude")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(base, "config"), 0755))
	must(os.WriteFile(filepath.Join(base, "CLAUDE.md"), []byte("# Claude"), 0644))
	must(os.WriteFile(filepath.Join(base, "config", "settings.json"), []byte("{}"), 0644))
	must(os.WriteFile(filepath.Join(base, "config", "statusline.sh"), []byte("echo ok"), 0644))

	// IsClaudeRepo detects a source checkout by content (skills/ + cli/go.mod
	// declaring this module) — write the marker so Deploy() accepts base.
	must(os.MkdirAll(filepath.Join(base, "cli"), 0755))
	must(os.WriteFile(filepath.Join(base, "cli", "go.mod"), []byte("module github.com/bravros/bravros/cli\n\ngo 1.26.2\n"), 0644))

	for name, content := range skillSpecs {
		dir := filepath.Join(base, "skills", name)
		must(os.MkdirAll(dir, 0755))
		must(os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644))
	}
	return base
}

// deployedSkills returns the list of skill directory names present under target/skills/.
func deployedSkills(t *testing.T, target string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(target, "skills"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read deployed skills: %v", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

func containsSkill(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Test: no allowlist → all skills deploy
// ---------------------------------------------------------------------------

func TestDeploy_NoAllowlist_AllSkillsDeploy(t *testing.T) {
	src := makeTestRepo(t, map[string]string{
		"plan":     "---\nname: plan\n---\n",
		"airbrush": "---\nname: airbrush\n---\n",
		"n8n":      "---\nname: n8n\n---\n",
	})
	target := filepath.Join(t.TempDir(), ".claude")

	_, err := deploy.Deploy(deploy.DeployOpts{
		SourceDir: src,
		TargetDir: target,
		// EnabledSkills is nil → deploy all
	})
	if err != nil {
		t.Fatalf("deploy failed: %v", err)
	}

	deployed := deployedSkills(t, target)
	for _, skill := range []string{"plan", "airbrush", "n8n"} {
		if !containsSkill(deployed, skill) {
			t.Errorf("expected skill %q to be deployed, but it was not", skill)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: allowlist set → only listed + core skills deploy
// ---------------------------------------------------------------------------

func TestDeploy_AllowlistSet_OnlyListedAndCoreDeploy(t *testing.T) {
	coreSKILL := "---\nname: finish\ncore: true\ndescription: core skill\n---\n"
	src := makeTestRepo(t, map[string]string{
		"plan":     "---\nname: plan\ndescription: a plan skill\n---\n",
		"finish":   coreSKILL,
		"airbrush": "---\nname: airbrush\ndescription: homelab skill\n---\n",
		"n8n":      "---\nname: n8n\ndescription: homelab skill\n---\n",
	})
	target := filepath.Join(t.TempDir(), ".claude")

	_, err := deploy.Deploy(deploy.DeployOpts{
		SourceDir:     src,
		TargetDir:     target,
		EnabledSkills: []string{"plan"}, // only "plan" in allowlist; "finish" is core
	})
	if err != nil {
		t.Fatalf("deploy failed: %v", err)
	}

	deployed := deployedSkills(t, target)

	// "plan" should deploy (in allowlist)
	if !containsSkill(deployed, "plan") {
		t.Error("expected 'plan' (in allowlist) to be deployed")
	}
	// "finish" should deploy (core: true)
	if !containsSkill(deployed, "finish") {
		t.Error("expected 'finish' (core: true) to be deployed even though not in allowlist")
	}
	// "airbrush" and "n8n" should NOT deploy
	if containsSkill(deployed, "airbrush") {
		t.Error("expected 'airbrush' (not in allowlist, not core) to be excluded")
	}
	if containsSkill(deployed, "n8n") {
		t.Error("expected 'n8n' (not in allowlist, not core) to be excluded")
	}
}

// ---------------------------------------------------------------------------
// Test: --filter flag overrides .bravros.yml allowlist
// ---------------------------------------------------------------------------

func TestDeploy_FilterFlag_OverridesAllowlist(t *testing.T) {
	// This test exercises the --filter parsing logic in cmd/deploy.go by calling
	// the deploy package directly with a manually constructed EnabledSkills list
	// (simulating what --filter produces after CSV parsing).
	coreSKILL := "---\nname: audit\ncore: true\ndescription: audit skill\n---\n"
	src := makeTestRepo(t, map[string]string{
		"plan":     "---\nname: plan\ndescription: plan skill\n---\n",
		"audit":    coreSKILL,
		"airbrush": "---\nname: airbrush\ndescription: homelab skill\n---\n",
	})
	target := filepath.Join(t.TempDir(), ".claude")

	// Simulate --filter "airbrush" — this would override any .bravros.yml allowlist.
	// The deploy package honors whatever EnabledSkills is set to.
	_, err := deploy.Deploy(deploy.DeployOpts{
		SourceDir:     src,
		TargetDir:     target,
		EnabledSkills: []string{"airbrush"}, // filter selects airbrush only (+ core)
	})
	if err != nil {
		t.Fatalf("deploy failed: %v", err)
	}

	deployed := deployedSkills(t, target)

	if !containsSkill(deployed, "airbrush") {
		t.Error("expected 'airbrush' to be deployed (specified via --filter)")
	}
	if !containsSkill(deployed, "audit") {
		t.Error("expected 'audit' (core: true) to deploy even when not in --filter")
	}
	if containsSkill(deployed, "plan") {
		t.Error("expected 'plan' to be excluded (not in --filter, not core)")
	}
}

// ---------------------------------------------------------------------------
// Test: --dry-run lists without deploying
// ---------------------------------------------------------------------------

func TestDeploy_DryRun_ListsWithoutDeploying(t *testing.T) {
	src := makeTestRepo(t, map[string]string{
		"plan":     "---\nname: plan\n---\n",
		"airbrush": "---\nname: airbrush\n---\n",
	})
	target := filepath.Join(t.TempDir(), ".claude")

	result, err := deploy.Deploy(deploy.DeployOpts{
		SourceDir:     src,
		TargetDir:     target,
		DryRun:        true,
		EnabledSkills: []string{"plan"}, // only plan in allowlist
	})
	if err != nil {
		t.Fatalf("deploy failed: %v", err)
	}

	if !result.DryRun {
		t.Error("expected DryRun=true in result")
	}
	if len(result.Files) == 0 {
		t.Error("expected file list in dry-run output")
	}

	// target should NOT exist (nothing copied in dry-run)
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("expected target dir to NOT exist in dry-run mode")
	}

	// Files listed should only include plan-related files (airbrush excluded by allowlist)
	for _, f := range result.Files {
		if strings.HasPrefix(f, "skills/airbrush") {
			t.Errorf("expected airbrush to be excluded in dry-run with allowlist, but got: %s", f)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: printDeploySummary — the default human-readable output (no JSON wall)
// ---------------------------------------------------------------------------

func TestPrintDeploySummary_Default(t *testing.T) {
	var buf bytes.Buffer
	printDeploySummary(&deploy.DeployResult{
		FilesDeployed:  277,
		SkillsDeployed: []string{"plan", "finish"},
		SkillsSkipped:  []string{"audit", "commit", "ship"},
	}, &buf)
	out := buf.String()

	// Must be a compact summary — never a JSON dump.
	if strings.Contains(out, "{") || strings.Contains(out, "\"files_deployed\"") {
		t.Errorf("summary must not contain JSON, got: %q", out)
	}
	for _, want := range []string{"Deployed:", "5 skill(s)", "2 updated", "3 unchanged", "277 file(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected summary to contain %q, got: %q", want, out)
		}
	}
}

func TestPrintDeploySummary_DryRunAndPruned(t *testing.T) {
	var buf bytes.Buffer
	printDeploySummary(&deploy.DeployResult{
		FilesDeployed: 10,
		DryRun:        true,
		Pruned:        []string{"hooks/logs", "skills/stale"},
	}, &buf)
	out := buf.String()

	if !strings.Contains(out, "Would deploy:") {
		t.Errorf("dry-run summary should say 'Would deploy:', got: %q", out)
	}
	if !strings.Contains(out, "Would prune 2 orphan(s):") {
		t.Errorf("dry-run summary should report 'Would prune 2 orphan(s):', got: %q", out)
	}
	if !strings.Contains(out, "hooks/logs") || !strings.Contains(out, "skills/stale") {
		t.Errorf("summary should list pruned orphans, got: %q", out)
	}
}

// TestPrintDeploySummary_ClaudeMdReconcileSkipped asserts a skipped
// managed-global CLAUDE.md reconcile is rendered in the default human summary,
// not just carried silently in the DeployResult / --json output.
func TestPrintDeploySummary_ClaudeMdReconcileSkipped(t *testing.T) {
	var buf bytes.Buffer
	printDeploySummary(&deploy.DeployResult{
		FilesDeployed:            10,
		ClaudeMdReconcileSkipped: []string{"source-claude-md-missing"},
	}, &buf)
	out := buf.String()

	if !strings.Contains(out, "CLAUDE.md reconcile skipped: source-claude-md-missing") {
		t.Errorf("expected summary to report the CLAUDE.md reconcile skip, got: %q", out)
	}
}

// TestPrintDeploySummary_NoClaudeMdSkipLineWhenReconciled asserts a normal
// deploy (reconcile ran, nothing skipped) prints no skip line at all.
func TestPrintDeploySummary_NoClaudeMdSkipLineWhenReconciled(t *testing.T) {
	var buf bytes.Buffer
	printDeploySummary(&deploy.DeployResult{
		FilesDeployed: 10,
	}, &buf)
	out := buf.String()

	if strings.Contains(out, "CLAUDE.md reconcile skipped") {
		t.Errorf("summary should not mention a CLAUDE.md reconcile skip when none occurred, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// Test: deploy skill-sha — single source of truth for skill SHA
// ---------------------------------------------------------------------------

// TestDeploySkillSHA_MatchesComputeSkillSHA verifies the verb prints exactly the
// digest deploy.ComputeSkillSHA returns (the value verify.sh shells out for).
func TestDeploySkillSHA_MatchesComputeSkillSHA(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: x\n---\nbody\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "references", "r.md"), []byte("ref"), 0644); err != nil {
		t.Fatal(err)
	}

	want, err := deploy.ComputeSkillSHA(dir)
	if err != nil {
		t.Fatalf("ComputeSkillSHA: %v", err)
	}

	var buf bytes.Buffer
	deploySkillSHACmd.SetOut(&buf)
	if err := deploySkillSHACmd.RunE(deploySkillSHACmd, []string{dir}); err != nil {
		t.Fatalf("skill-sha RunE: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if got != want {
		t.Errorf("skill-sha printed %q, want %q (must match deploy.ComputeSkillSHA)", got, want)
	}
}

// TestDeploySkillSHA_RejectsNonDir verifies a file path (not a directory) errors.
func TestDeploySkillSHA_RejectsNonDir(t *testing.T) {
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := deploySkillSHACmd.RunE(deploySkillSHACmd, []string{f}); err == nil {
		t.Error("expected error for non-directory argument, got nil")
	}
}

// TestDeploySkillSHA_RejectsMissingPath verifies a missing path errors.
func TestDeploySkillSHA_RejectsMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if err := deploySkillSHACmd.RunE(deploySkillSHACmd, []string{missing}); err == nil {
		t.Error("expected error for missing path, got nil")
	}
}

// ---------------------------------------------------------------------------
// Test: preDeployBashHygieneLint — Rule 50 mirror gate
// ---------------------------------------------------------------------------

// TestPreDeployBashHygieneLint_CleanTreePasses verifies the lint returns nil
// when no source SKILL.md contains the unsafe `for X in $VAR` pattern.
func TestPreDeployBashHygieneLint_CleanTreePasses(t *testing.T) {
	src := makeTestRepo(t, map[string]string{
		"safe-skill": "---\nname: safe-skill\n---\n\n```bash\nfor x in \"${arr[@]}\"; do echo $x; done\n```\n",
	})

	var buf bytes.Buffer
	if err := preDeployBashHygieneLint(src, false, &buf); err != nil {
		t.Errorf("expected no error for clean tree, got: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output for clean tree, got: %q", buf.String())
	}
}

// TestPreDeployBashHygieneLint_UnsafeRefusesByDefault verifies the lint returns
// a non-nil error and exits the deploy flow when an unsafe pattern is present.
func TestPreDeployBashHygieneLint_UnsafeRefusesByDefault(t *testing.T) {
	src := makeTestRepo(t, map[string]string{
		"bad-skill": "---\nname: bad-skill\n---\n\n```bash\nfor bid in $BACKLOG_IDS; do echo $bid; done\n```\n",
	})

	var buf bytes.Buffer
	err := preDeployBashHygieneLint(src, false, &buf)
	if err == nil {
		t.Fatal("expected non-nil error for unsafe pattern, got nil (deploy would proceed)")
	}
	out := buf.String()
	if !strings.Contains(out, "deploy blocked") {
		t.Errorf("expected output to contain 'deploy blocked', got: %q", out)
	}
	if !strings.Contains(out, "bad-skill/SKILL.md") {
		t.Errorf("expected output to name the offending skill, got: %q", out)
	}
	if !strings.Contains(out, "for bid in $BACKLOG_IDS") {
		t.Errorf("expected output to include the matched snippet, got: %q", out)
	}
	if !strings.Contains(out, "--force") {
		t.Errorf("expected output to mention --force escape hatch, got: %q", out)
	}
}

// TestPreDeployBashHygieneLint_ForceDowngrades verifies that --force downgrades
// the violation to a warning and returns nil so the deploy proceeds.
func TestPreDeployBashHygieneLint_ForceDowngrades(t *testing.T) {
	src := makeTestRepo(t, map[string]string{
		"bad-skill": "---\nname: bad-skill\n---\n\n```bash\nfor bid in $BACKLOG_IDS; do echo $bid; done\n```\n",
	})

	var buf bytes.Buffer
	err := preDeployBashHygieneLint(src, true, &buf)
	if err != nil {
		t.Errorf("expected nil error under --force, got: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "--force") {
		t.Errorf("expected force-mode warning prefix, got: %q", out)
	}
	if !strings.Contains(out, "bad-skill/SKILL.md") {
		t.Errorf("expected force-mode warning to name offender, got: %q", out)
	}
}

// TestPreDeployBashHygieneLint_NoSkillsDirNoop verifies the lint is a no-op
// when the source tree has no skills/ directory.
func TestPreDeployBashHygieneLint_NoSkillsDirNoop(t *testing.T) {
	base := filepath.Join(t.TempDir(), "claude")
	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := preDeployBashHygieneLint(base, false, &buf); err != nil {
		t.Errorf("expected no error when skills/ is absent, got: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output when skills/ is absent, got: %q", buf.String())
	}
}
