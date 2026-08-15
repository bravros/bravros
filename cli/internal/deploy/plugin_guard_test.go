package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// TestIsPluginManaged — table test over the path-segment-aware matcher.
// ---------------------------------------------------------------------------

func TestIsPluginManaged(t *testing.T) {
	tests := []struct {
		rel  string
		want bool
	}{
		{"plugins/x", true},
		{".claude-plugin/y", true},
		{"extensions/z", true},
		// String-prefix matching would wrongly flag this — must be FALSE.
		{"skills/plugins-helper", false},
		// A "plugins" segment nested under a non-plugin-managed top-level dir
		// (here "skills") is not a host's plugin root — decided FALSE, and
		// documented on IsPluginManaged: only the FIRST path segment counts.
		{"skills/a/plugins/b", false},
		// Sanity: ordinary managed subtrees are not plugin-managed.
		{"skills/commit", false},
		{"templates/plan.md", false},
		{"hooks/pre-commit", false},
		{"agents/foo.md", false},
	}
	for _, tt := range tests {
		got := IsPluginManaged(tt.rel)
		if got != tt.want {
			t.Errorf("IsPluginManaged(%q) = %v, want %v", tt.rel, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// sha256Hex is a small test helper for byte-for-byte comparison of files
// before/after Deploy.
// ---------------------------------------------------------------------------

func sha256Hex(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// TestDeployNeverWritesPluginManagedDirs
// ---------------------------------------------------------------------------

func TestDeployNeverWritesPluginManagedDirs(t *testing.T) {
	src := filepath.Join(t.TempDir(), "claude")
	writeRepoMarkers(t, src)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(src, "skills", "x"), 0755))
	must(os.WriteFile(filepath.Join(src, "skills", "x", "SKILL.md"), []byte("x skill"), 0644))
	must(os.MkdirAll(filepath.Join(src, "templates"), 0755))
	must(os.WriteFile(filepath.Join(src, "templates", "y"), []byte("template y"), 0644))

	target := t.TempDir()

	// Pre-populate plugin-managed content at the destination.
	pluginSkill := filepath.Join(target, "plugins", "bravros", "skills", "fake", "SKILL.md")
	must(os.MkdirAll(filepath.Dir(pluginSkill), 0755))
	must(os.WriteFile(pluginSkill, []byte("fake plugin-managed skill"), 0644))

	marketplace := filepath.Join(target, ".claude-plugin", "marketplace.json")
	must(os.MkdirAll(filepath.Dir(marketplace), 0755))
	must(os.WriteFile(marketplace, []byte(`{"name":"bravros"}`), 0644))

	beforePlugin := sha256Hex(t, pluginSkill)
	beforeMarketplace := sha256Hex(t, marketplace)

	result, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	afterPlugin := sha256Hex(t, pluginSkill)
	afterMarketplace := sha256Hex(t, marketplace)

	if beforePlugin != afterPlugin {
		t.Errorf("plugins/ skill content changed: before=%s after=%s", beforePlugin, afterPlugin)
	}
	if beforeMarketplace != afterMarketplace {
		t.Errorf(".claude-plugin/ content changed: before=%s after=%s", beforeMarketplace, afterMarketplace)
	}

	for _, p := range result.Pruned {
		if IsPluginManaged(p) {
			t.Errorf("plugin-managed path %q appeared in result.Pruned", p)
		}
	}
	for _, f := range result.Files {
		if IsPluginManaged(f) {
			t.Errorf("plugin-managed path %q appeared in result.Files", f)
		}
	}
}

// ---------------------------------------------------------------------------
// TestDeployPreservesUserOwnedFiles
// ---------------------------------------------------------------------------

func TestDeployPreservesUserOwnedFiles(t *testing.T) {
	src := filepath.Join(t.TempDir(), "claude")
	writeRepoMarkers(t, src)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(src, "skills", "x"), 0755))
	must(os.WriteFile(filepath.Join(src, "skills", "x", "SKILL.md"), []byte("x skill"), 0644))

	target := t.TempDir()

	settingsLocal := filepath.Join(target, "settings.local.json")
	must(os.WriteFile(settingsLocal, []byte(`{"mine":true}`), 0644))

	claudeMd := filepath.Join(target, "CLAUDE.md")
	must(os.WriteFile(claudeMd, []byte("# My personal notes\nOutside any managed marker.\n"), 0644))

	outputStyle := filepath.Join(target, "output-styles", "mine.md")
	must(os.MkdirAll(filepath.Dir(outputStyle), 0755))
	must(os.WriteFile(outputStyle, []byte("# my hand-written style"), 0644))

	beforeSettings := sha256Hex(t, settingsLocal)
	beforeClaudeMd := sha256Hex(t, claudeMd)
	beforeStyle := sha256Hex(t, outputStyle)

	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	afterSettings := sha256Hex(t, settingsLocal)
	afterClaudeMd := sha256Hex(t, claudeMd)
	afterStyle := sha256Hex(t, outputStyle)

	if beforeSettings != afterSettings {
		t.Errorf("settings.local.json content changed: before=%s after=%s", beforeSettings, afterSettings)
	}
	if beforeClaudeMd != afterClaudeMd {
		t.Errorf("CLAUDE.md personal content changed: before=%s after=%s", beforeClaudeMd, afterClaudeMd)
	}
	if beforeStyle != afterStyle {
		t.Errorf("output-styles/mine.md content changed: before=%s after=%s", beforeStyle, afterStyle)
	}
}

// ---------------------------------------------------------------------------
// TestDeployAbsentSourceSubtreeIsNotPruned / TestDeployPresentSourceSubtreeStillPrunes
//
// detectOrphans/detectNestedOrphans must distinguish "the source subtree is
// entirely absent" (unknown — e.g. a selfupdate fetched payload that ships
// only skills/ + templates/, no hooks/ or agents/ at all) from "the source
// subtree exists and this particular entry was removed upstream" (a genuine
// orphan). Only the latter may be pruned.
// ---------------------------------------------------------------------------

func TestDeployAbsentSourceSubtreeIsNotPruned(t *testing.T) {
	// Source shaped like a fetched selfupdate payload: only skills/ and
	// templates/ — no hooks/, no agents/ at all.
	src := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(src, "skills", "x"), 0755))
	must(os.WriteFile(filepath.Join(src, "skills", "x", "SKILL.md"), []byte("x skill"), 0644))
	must(os.MkdirAll(filepath.Join(src, "templates"), 0755))

	target := t.TempDir()
	hookFile := filepath.Join(target, "hooks", "pre-push")
	must(os.MkdirAll(filepath.Dir(hookFile), 0755))
	must(os.WriteFile(hookFile, []byte("#!/bin/sh\n"), 0755))

	agentFile := filepath.Join(target, "agents", "mine.md")
	must(os.MkdirAll(filepath.Dir(agentFile), 0755))
	must(os.WriteFile(agentFile, []byte("# my agent"), 0644))

	beforeHook := sha256Hex(t, hookFile)
	beforeAgent := sha256Hex(t, agentFile)

	result, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if _, err := os.Stat(hookFile); err != nil {
		t.Fatalf("hooks/pre-push should survive when source has no hooks/ subtree at all: %v", err)
	}
	if _, err := os.Stat(agentFile); err != nil {
		t.Fatalf("agents/mine.md should survive when source has no agents/ subtree at all: %v", err)
	}
	if sha256Hex(t, hookFile) != beforeHook {
		t.Error("hooks/pre-push content changed")
	}
	if sha256Hex(t, agentFile) != beforeAgent {
		t.Error("agents/mine.md content changed")
	}

	for _, p := range result.Pruned {
		if p == filepath.Join("hooks", "pre-push") || p == filepath.Join("agents", "mine.md") {
			t.Errorf("unexpected orphan from an entirely-absent source subtree: %q (Pruned=%v)", p, result.Pruned)
		}
	}
}

func TestDeployPresentSourceSubtreeStillPrunes(t *testing.T) {
	// Source HAS skills/ but is missing skills/gone — a genuine, narrower
	// removal (contrast case: pruning within an existing subtree must still
	// work; the guard narrows detection, it does not disable it).
	src := filepath.Join(t.TempDir(), "claude")
	writeRepoMarkers(t, src)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(src, "skills", "kept"), 0755))
	must(os.WriteFile(filepath.Join(src, "skills", "kept", "SKILL.md"), []byte("kept skill"), 0644))

	target := t.TempDir()
	goneDir := filepath.Join(target, "skills", "gone")
	must(os.MkdirAll(goneDir, 0755))
	must(os.WriteFile(filepath.Join(goneDir, "SKILL.md"), []byte("old"), 0644))

	result, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	want := filepath.Join("skills", "gone")
	found := false
	for _, p := range result.Pruned {
		if p == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %q in result.Pruned (present subtree, missing entry must still be an orphan), got %v", want, result.Pruned)
	}
	if _, err := os.Stat(goneDir); !os.IsNotExist(err) {
		t.Fatalf("skills/gone should have been pruned, stat err: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestDeployFromFetchedPayloadSourceDir
// ---------------------------------------------------------------------------

// TestDeployFromFetchedPayloadSourceDir proves that a SourceDir shaped exactly
// like the payload cli/internal/fetch downloads (only skills/ and templates/,
// no hooks/, no agents/, no config/, no cli/go.mod) is a valid Deploy source.
func TestDeployFromFetchedPayloadSourceDir(t *testing.T) {
	payload := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(payload, "skills", "example"), 0755))
	must(os.WriteFile(filepath.Join(payload, "skills", "example", "SKILL.md"), []byte("# example skill\n"), 0644))
	must(os.MkdirAll(filepath.Join(payload, "templates"), 0755))
	must(os.WriteFile(filepath.Join(payload, "templates", "CLAUDE.md"), []byte("# template\n"), 0644))

	target := t.TempDir()

	result, err := Deploy(DeployOpts{SourceDir: payload, TargetDir: target})
	if err != nil {
		t.Fatalf("Deploy from fetched-payload-shaped source must succeed, got: %v", err)
	}

	deployedSkill := filepath.Join(target, "skills", "example", "SKILL.md")
	if _, err := os.Stat(deployedSkill); err != nil {
		t.Errorf("skills/example/SKILL.md not deployed: %v", err)
	}
	found := false
	for _, s := range result.SkillsDeployed {
		if s == "example" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'example' in SkillsDeployed, got %v", result.SkillsDeployed)
	}

	deployedTemplate := filepath.Join(target, "templates", "CLAUDE.md")
	if _, err := os.Stat(deployedTemplate); err != nil {
		t.Errorf("templates/CLAUDE.md not deployed: %v", err)
	}
}
