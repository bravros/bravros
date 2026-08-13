// Package worktree — pure-helper unit tests.
// Integration coverage (SetupFull, end-to-end git+install flow) lives in
// cli/cmd/worktree_test.go.  This file covers the stateless/pure helpers that
// can be exercised without a real git repo or network.
package worktree

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bravros/bravros/cli/internal/config"
)

// --- assetDirFor ---

func TestAssetDirFor_Frameworks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		sc        config.StackConfig
		wantPath  string
		wantEmpty bool
	}{
		{
			name:     "laravel",
			sc:       config.StackConfig{Framework: "laravel"},
			wantPath: filepath.Join("public", "build"),
		},
		{
			name:     "nextjs",
			sc:       config.StackConfig{Framework: "nextjs"},
			wantPath: ".next",
		},
		{
			name:     "nuxt",
			sc:       config.StackConfig{Framework: "nuxt"},
			wantPath: ".output",
		},
		{
			name:     "node_language_no_framework",
			sc:       config.StackConfig{Language: "node"},
			wantPath: "dist",
		},
		{
			name:      "go_no_assets",
			sc:        config.StackConfig{Language: "go", Framework: ""},
			wantEmpty: true,
		},
		{
			name:      "python_no_assets",
			sc:        config.StackConfig{Language: "python"},
			wantEmpty: true,
		},
		{
			name:      "empty_stack",
			sc:        config.StackConfig{},
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := assetDirFor(tt.sc)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("assetDirFor(%+v) = %q, want empty string", tt.sc, got)
				}
				return
			}
			if got != tt.wantPath {
				t.Errorf("assetDirFor(%+v) = %q, want %q", tt.sc, got, tt.wantPath)
			}
		})
	}
}

// --- fileExists ---

func TestFileExists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	realFile := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(realFile, []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	if !fileExists(realFile) {
		t.Errorf("fileExists(%q) = false, want true for existing file", realFile)
	}
	if fileExists(filepath.Join(dir, "ghost.txt")) {
		t.Errorf("fileExists returned true for non-existent file")
	}
}

func TestFileExists_Directory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A directory counts as "exists"
	if !fileExists(dir) {
		t.Errorf("fileExists(%q) = false, want true for existing directory", dir)
	}
}

// --- copyEnv ---

func TestCopyEnv_SuccessfulCopy(t *testing.T) {
	t.Parallel()
	primary := t.TempDir()
	wt := t.TempDir()

	content := []byte("APP_URL=http://localhost\nDB_DATABASE=app\n")
	if err := os.WriteFile(filepath.Join(primary, ".env"), content, 0644); err != nil {
		t.Fatal(err)
	}

	copied, skip := copyEnv(primary, wt)
	if !copied {
		t.Errorf("copyEnv: copied = false, want true; skip=%q", skip)
	}
	if skip != "" {
		t.Errorf("copyEnv: skip = %q, want empty", skip)
	}

	got, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		t.Fatalf("could not read destination .env: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("destination .env content mismatch: got %q, want %q", got, content)
	}
}

func TestCopyEnv_NoSourceFile(t *testing.T) {
	t.Parallel()
	primary := t.TempDir()
	wt := t.TempDir()

	copied, skip := copyEnv(primary, wt)
	if copied {
		t.Error("copyEnv: copied = true, want false when source .env missing")
	}
	if skip != "env_no_source" {
		t.Errorf("copyEnv: skip = %q, want %q", skip, "env_no_source")
	}
}

func TestCopyEnv_DestAlreadyExists(t *testing.T) {
	t.Parallel()
	primary := t.TempDir()
	wt := t.TempDir()

	if err := os.WriteFile(filepath.Join(primary, ".env"), []byte("A=1"), 0644); err != nil {
		t.Fatal(err)
	}
	// Pre-populate destination
	if err := os.WriteFile(filepath.Join(wt, ".env"), []byte("A=existing"), 0644); err != nil {
		t.Fatal(err)
	}

	copied, skip := copyEnv(primary, wt)
	if copied {
		t.Error("copyEnv: copied = true, want false when dest .env already present")
	}
	if skip != "env_already_present" {
		t.Errorf("copyEnv: skip = %q, want %q", skip, "env_already_present")
	}
}

// --- linkAssets ---

func TestLinkAssets_SuccessfulLink(t *testing.T) {
	t.Parallel()
	primary := t.TempDir()
	wt := t.TempDir()
	sc := config.StackConfig{Framework: "nextjs"} // target = .next

	// Create the source asset dir
	srcDir := filepath.Join(primary, ".next")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	linked, skip := linkAssets(primary, wt, sc)
	if !linked {
		t.Errorf("linkAssets: linked = false, want true; skip=%q", skip)
	}
	if skip != "" {
		t.Errorf("linkAssets: skip = %q, want empty", skip)
	}

	// Confirm symlink exists
	info, err := os.Lstat(filepath.Join(wt, ".next"))
	if err != nil {
		t.Fatalf("symlink not found: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected symlink at worktree/.next, got mode %v", info.Mode())
	}
}

func TestLinkAssets_NoConventionalDir(t *testing.T) {
	t.Parallel()
	primary := t.TempDir()
	wt := t.TempDir()
	sc := config.StackConfig{Language: "go"} // assetDirFor → ""

	linked, skip := linkAssets(primary, wt, sc)
	if linked {
		t.Error("linkAssets: linked = true, want false for stack with no asset dir")
	}
	if skip != "build_no_target" {
		t.Errorf("linkAssets: skip = %q, want %q", skip, "build_no_target")
	}
}

func TestLinkAssets_NoSourceDir(t *testing.T) {
	t.Parallel()
	primary := t.TempDir()
	wt := t.TempDir()
	sc := config.StackConfig{Framework: "laravel"} // target = public/build — not created

	linked, skip := linkAssets(primary, wt, sc)
	if linked {
		t.Error("linkAssets: linked = true, want false when source asset dir absent")
	}
	if skip != "build_no_source" {
		t.Errorf("linkAssets: skip = %q, want %q", skip, "build_no_source")
	}
}

func TestLinkAssets_AlreadyPresent(t *testing.T) {
	t.Parallel()
	primary := t.TempDir()
	wt := t.TempDir()
	sc := config.StackConfig{Framework: "nextjs"} // .next

	srcDir := filepath.Join(primary, ".next")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Pre-populate destination
	dstDir := filepath.Join(wt, ".next")
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		t.Fatal(err)
	}

	linked, skip := linkAssets(primary, wt, sc)
	if linked {
		t.Error("linkAssets: linked = true, want false when dest already present")
	}
	if skip != "build_already_present" {
		t.Errorf("linkAssets: skip = %q, want %q", skip, "build_already_present")
	}
}

// --- SetupFull ready-marker short-circuit ---
// SetupFull delegates the "already done" check to gitpkg.WorktreeExistsAt (git
// worktree step) and file-presence guards on vendor/, node_modules/, .venv, etc.
// There is no separate "ready marker file" — the result.Ready field is set
// unconditionally at the end of a successful run.  End-to-end coverage for the
// idempotency path is provided by cli/cmd/worktree_test.go (integration tests
// that exercise SetupFull 18 times across 5 functions with a real git worktree).
func TestSetupFull_ReadyMarkerCoveredByIntegration(t *testing.T) {
	t.Skip("ready-marker/idempotency short-circuit covered by integration test in cli/cmd/worktree_test.go")
}

// --- RuntimeDirsFor ---

func TestRuntimeDirsFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		sc   config.StackConfig
		want []string
	}{
		{
			name: "php_no_framework",
			sc:   config.StackConfig{Language: "php"},
			want: []string{"vendor", filepath.Join("bootstrap", "cache")},
		},
		{
			name: "laravel_adds_asset_dir",
			sc:   config.StackConfig{Language: "php", Framework: "laravel"},
			want: []string{"vendor", filepath.Join("bootstrap", "cache"), filepath.Join("public", "build")},
		},
		{
			name: "node_no_framework",
			sc:   config.StackConfig{Language: "node"},
			want: []string{"node_modules", "dist"},
		},
		{
			name: "nextjs",
			sc:   config.StackConfig{Language: "node", Framework: "nextjs"},
			want: []string{"node_modules", ".next"},
		},
		{
			name: "go_no_runtime_dirs",
			sc:   config.StackConfig{Language: "go"},
			want: nil,
		},
		{
			name: "python_no_runtime_dirs",
			sc:   config.StackConfig{Language: "python"},
			want: nil,
		},
		{
			name: "empty_stack",
			sc:   config.StackConfig{},
			want: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RuntimeDirsFor(tt.sc)
			if len(got) != len(tt.want) {
				t.Fatalf("RuntimeDirsFor(%+v) = %v, want %v", tt.sc, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("RuntimeDirsFor(%+v)[%d] = %q, want %q", tt.sc, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// --- cloneRuntimeDirs ---

func TestCloneRuntimeDirs_SuccessfulClone(t *testing.T) {
	t.Parallel()
	primary := t.TempDir()
	wt := t.TempDir()

	src := filepath.Join(primary, "vendor")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "autoload.php"), []byte("<?php"), 0644); err != nil {
		t.Fatal(err)
	}

	cloned, skipped, warnings := cloneRuntimeDirs(primary, wt, []string{"vendor"})
	if len(cloned) != 1 || cloned[0] != "vendor" {
		t.Errorf("cloneRuntimeDirs: cloned = %v, want [vendor]; warnings=%v", cloned, warnings)
	}
	if len(skipped) != 0 {
		t.Errorf("cloneRuntimeDirs: skipped = %v, want empty", skipped)
	}
	if _, err := os.Stat(filepath.Join(wt, "vendor", "autoload.php")); err != nil {
		t.Errorf("expected cloned file to exist: %v", err)
	}
}

func TestCloneRuntimeDirs_MissingInParent(t *testing.T) {
	t.Parallel()
	primary := t.TempDir()
	wt := t.TempDir()

	cloned, skipped, warnings := cloneRuntimeDirs(primary, wt, []string{"node_modules"})
	if len(cloned) != 0 {
		t.Errorf("cloneRuntimeDirs: cloned = %v, want empty when source missing", cloned)
	}
	if len(warnings) != 0 {
		t.Errorf("cloneRuntimeDirs: warnings = %v, want empty (missing source is not a warning)", warnings)
	}
	if len(skipped) != 1 || skipped[0] != "clone_missing:node_modules" {
		t.Errorf("cloneRuntimeDirs: skipped = %v, want [clone_missing:node_modules]", skipped)
	}
}

func TestCloneRuntimeDirs_AlreadyPresentInWorktree(t *testing.T) {
	t.Parallel()
	primary := t.TempDir()
	wt := t.TempDir()

	if err := os.MkdirAll(filepath.Join(primary, "node_modules"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wt, "node_modules"), 0755); err != nil {
		t.Fatal(err)
	}

	cloned, skipped, _ := cloneRuntimeDirs(primary, wt, []string{"node_modules"})
	if len(cloned) != 0 {
		t.Errorf("cloneRuntimeDirs: cloned = %v, want empty when dest already present", cloned)
	}
	if len(skipped) != 1 || skipped[0] != "clone_already_present:node_modules" {
		t.Errorf("cloneRuntimeDirs: skipped = %v, want [clone_already_present:node_modules]", skipped)
	}
}

func TestCloneRuntimeDirs_DropsCompiledCache(t *testing.T) {
	t.Parallel()
	primary := t.TempDir()
	wt := t.TempDir()

	cacheDir := filepath.Join(primary, "bootstrap", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "config.php"), []byte("<?php return [];"), 0644); err != nil {
		t.Fatal(err)
	}

	cloned, _, warnings := cloneRuntimeDirs(primary, wt, []string{filepath.Join("bootstrap", "cache")})
	if len(cloned) != 1 {
		t.Fatalf("expected bootstrap/cache to clone, got cloned=%v warnings=%v", cloned, warnings)
	}
	if _, err := os.Stat(filepath.Join(wt, "bootstrap", "cache", "config.php")); !os.IsNotExist(err) {
		t.Errorf("expected cloned bootstrap/cache/config.php to be deleted, stat err=%v", err)
	}
}

// --- driftDecision ---

func TestDriftDecision(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		head string
		wt   string
		want bool
	}{
		{name: "identical", head: `{"a":1}`, wt: `{"a":1}`, want: false},
		{name: "trailing_newline_ignored", head: "{\"a\":1}\n", wt: "{\"a\":1}", want: false},
		{name: "surrounding_whitespace_ignored", head: "  {\"a\":1}  ", wt: "{\"a\":1}", want: false},
		{name: "content_differs", head: `{"a":1}`, wt: `{"a":2}`, want: true},
		{name: "empty_vs_content", head: "", wt: `{"a":1}`, want: true},
		{name: "both_empty", head: "", wt: "", want: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := driftDecision(tt.head, tt.wt)
			if got != tt.want {
				t.Errorf("driftDecision(%q, %q) = %v, want %v", tt.head, tt.wt, got, tt.want)
			}
		})
	}
}

// --- checkLockfileDrift (error-path: no git repo / untracked file) ---

func TestCheckLockfileDrift_FileNotTrackedAtHead(t *testing.T) {
	t.Parallel()
	primary := t.TempDir() // not a git repo — `git show` will fail for any path
	wt := t.TempDir()

	if err := os.WriteFile(filepath.Join(wt, "package.json"), []byte(`{"name":"x"}`), 0644); err != nil {
		t.Fatal(err)
	}

	warnings := checkLockfileDrift(primary, wt, []string{"package.json"})
	if len(warnings) != 1 {
		t.Fatalf("checkLockfileDrift: warnings = %v, want exactly one warning", warnings)
	}
}

func TestCheckLockfileDrift_NoCandidatesInWorktree(t *testing.T) {
	t.Parallel()
	primary := t.TempDir()
	wt := t.TempDir()

	warnings := checkLockfileDrift(primary, wt, lockfileCandidates)
	if len(warnings) != 0 {
		t.Errorf("checkLockfileDrift: warnings = %v, want empty when worktree has no candidate files", warnings)
	}
}

// --- resolveBaseBranchName ---

func TestResolveBaseBranchName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		baseRef string
		want    string
	}{
		{name: "bare_branch_name", baseRef: "homolog", want: "homolog"},
		{name: "origin_prefixed", baseRef: "origin/main", want: "main"},
		{name: "origin_prefixed_homolog", baseRef: "origin/homolog", want: "homolog"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveBaseBranchName(tt.baseRef)
			if got != tt.want {
				t.Errorf("resolveBaseBranchName(%q) = %q, want %q", tt.baseRef, got, tt.want)
			}
		})
	}
}

func TestResolveBaseBranchName_EmptyFallsBackToConfigOrMain(t *testing.T) {
	t.Parallel()
	// No .kaisser.yml in this tempdir and no cwd override — falls back to the
	// config package's own default ("homolog") or ultimately "main". Either
	// way it must never return empty.
	got := resolveBaseBranchName("")
	if got == "" {
		t.Error("resolveBaseBranchName(\"\") returned empty string, want a non-empty fallback branch")
	}
}

// --- evaluateSmokeChecks ---

func TestEvaluateSmokeChecks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		in          smokeCheckInputs
		wantWarnLen int
	}{
		{
			name: "all_pass",
			in: smokeCheckInputs{
				BaseBranch:  "homolog",
				IsAncestor:  true,
				StatusClean: true,
				MissingDirs: nil,
			},
			wantWarnLen: 0,
		},
		{
			name: "not_ancestor",
			in: smokeCheckInputs{
				BaseBranch:  "homolog",
				IsAncestor:  false,
				StatusClean: true,
			},
			wantWarnLen: 1,
		},
		{
			name: "dirty_tree",
			in: smokeCheckInputs{
				BaseBranch:  "homolog",
				IsAncestor:  true,
				StatusClean: false,
			},
			wantWarnLen: 1,
		},
		{
			name: "missing_dirs",
			in: smokeCheckInputs{
				BaseBranch:  "homolog",
				IsAncestor:  true,
				StatusClean: true,
				MissingDirs: []string{"vendor", "node_modules"},
			},
			wantWarnLen: 2,
		},
		{
			name: "base_branch_empty_skips_ancestor_check",
			in: smokeCheckInputs{
				BaseBranch:  "",
				IsAncestor:  false,
				StatusClean: true,
			},
			wantWarnLen: 0,
		},
		{
			name: "everything_fails",
			in: smokeCheckInputs{
				BaseBranch:  "homolog",
				IsAncestor:  false,
				StatusClean: false,
				MissingDirs: []string{"vendor"},
			},
			wantWarnLen: 3,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := evaluateSmokeChecks(tt.in)
			if len(got) != tt.wantWarnLen {
				t.Errorf("evaluateSmokeChecks(%+v) = %v (len %d), want len %d", tt.in, got, len(got), tt.wantWarnLen)
			}
		})
	}
}

// --- runSmokeChecks (error-path: not a git repo at all) ---

func TestRunSmokeChecks_NonGitDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // no .git — merge-base and status both fail

	warnings := runSmokeChecks(dir, "homolog", []string{"vendor"})
	// Expect: ancestor check fails, status check fails (treated as dirty), and
	// the missing "vendor" dir is flagged — all three should surface.
	if len(warnings) != 3 {
		t.Errorf("runSmokeChecks(non-git dir) = %v (len %d), want len 3", warnings, len(warnings))
	}
}

func TestRunSmokeChecks_EmptyBaseBranchSkipsAncestorCheck(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	warnings := runSmokeChecks(dir, "", nil)
	for _, w := range warnings {
		if strings.Contains(w, "does not descend from origin/") {
			t.Errorf("runSmokeChecks with empty baseBranch should skip ancestor check, got warning: %q", w)
		}
	}
}
