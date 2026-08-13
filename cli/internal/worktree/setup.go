// Test coverage: cli/cmd/worktree_test.go (integration); cli/internal/worktree/setup_test.go (pure helpers)

// Package worktree implements the full `kaisser worktree setup-full` flow:
// git-worktree-add + framework-aware dependency install + .env copy + asset
// symlink, all in one idempotent verb.
//
// It builds on cli/internal/git for the raw worktree-add (which in turn LIFTS
// bravros's porcelain-aware helpers — see git/worktree.go attribution comment)
// and on cli/internal/stack for framework detection (package manager, asset
// directory, etc.).
//
// The package is intentionally a thin dispatcher: each framework branch is a
// small helper that shells out to the language's native install tool. The
// result is emitted as JSON so callers (skills, subagents) can inspect what
// actually happened without parsing log output.
package worktree

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bravros/bravros/cli/internal/config"
	gitpkg "github.com/bravros/bravros/cli/internal/git"
	"github.com/bravros/bravros/cli/internal/stack"
)

// Opts configures the full worktree setup operation.
// Zero value = install everything, copy .env, link build assets.
type Opts struct {
	From      string // primary worktree path (for .env copy + build symlink); empty = autodetect
	BaseRef   string // base branch ref (e.g. "origin/homolog"); empty = auto from config
	NoInstall bool   // skip dependency install step
	NoEnv     bool   // skip .env copy step
	NoBuild   bool   // skip asset symlink step
	Herd      bool   // Laravel-only: also run `herd link` / `herd secure`
	Install   bool   // force real dependency installs; skip APFS CoW runtime-dir cloning
}

// SetupResult is the JSON payload emitted by `kaisser worktree setup-full`.
// Layout lifted from bravros WorktreeSetupResult and extended for the
// framework-aware fields this verb adds.
//
// Consumers (skills, subagents) depend on these exact field names — do not
// rename without updating skills/auto-merge, skills/plan-wt, and anything that
// pipes this JSON through jq.
type SetupResult struct {
	Path           string   `json:"path"`
	Branch         string   `json:"branch"`
	Created        bool     `json:"created"`
	Framework      string   `json:"framework,omitempty"`
	PackageManager string   `json:"package_manager,omitempty"`
	DepsInstalled  []string `json:"deps_installed,omitempty"`
	Cloned         []string `json:"cloned,omitempty"` // runtime dirs APFS/plain-copy cloned from parent instead of installed
	EnvCopied      bool     `json:"env_copied"`
	BuildLinked    bool     `json:"build_linked"`
	Ready          bool     `json:"ready"`
	Skipped        []string `json:"skipped,omitempty"`  // idempotent re-run indicators
	Warnings       []string `json:"warnings,omitempty"` // non-fatal: lockfile drift + post-create smoke check failures
	Error          string   `json:"error,omitempty"`
}

// SetupFull performs the complete worktree setup flow. It is idempotent:
// re-running on an existing worktree skips git-worktree-add (using the lifted
// worktreeExists helper) and skips install steps whose outputs (vendor/,
// node_modules/, etc.) are already present.
func SetupFull(branch string, opts Opts) (*SetupResult, error) {
	result := &SetupResult{Branch: branch}

	// Resolve primary (FROM) first — we need it for .env copy + build symlink,
	// and to compute the worktree path when it's not explicit.
	primary := opts.From
	if primary == "" {
		if cwd, err := os.Getwd(); err == nil {
			primary = cwd
		}
	}
	if primary == "" {
		return nil, fmt.Errorf("cannot determine primary worktree path (pass --from)")
	}

	// Resolve repo root from the primary (which may itself be a worktree).
	repoRoot, _, err := gitpkg.RunInDir(primary, "git", "rev-parse", "--show-toplevel")
	if err != nil || repoRoot == "" {
		// Fall back to primary itself — better than failing outright.
		repoRoot = primary
	}

	// Compute worktree path using the LIFTed helper (via public wrapper).
	path := gitpkg.ComputeWorktreePath(repoRoot, branch)
	result.Path = path

	// Step 1: git worktree add (idempotent — skip if already present).
	if gitpkg.WorktreeExistsAt(path) {
		result.Skipped = append(result.Skipped, "worktree_create")
		result.Created = false
	} else {
		wtOpts := gitpkg.WorktreeOpts{
			NoRebase:   true, // setup-full never rebases; caller must do that explicitly
			BaseBranch: strings.TrimPrefix(opts.BaseRef, "origin/"),
		}
		_, err := gitpkg.WorktreeSetup(branch, path, wtOpts)
		if err != nil {
			result.Error = err.Error()
			return result, err
		}
		result.Created = true
	}

	// Step 2: detect stack inside the worktree (uses the worktree's own files).
	det, derr := stack.Detect(path, stack.DetectOpts{SkipGit: true})
	var sc config.StackConfig
	if derr == nil && det != nil {
		sc = det.Stack
		result.Framework = sc.Framework
		result.PackageManager = sc.NodePackageManager
	}

	// Step 3: dispatch per-framework install, or clone runtime dirs from the
	// parent worktree (APFS copy-on-write) when they're already present there
	// and --install wasn't passed to force a real install.
	if !opts.NoInstall && derr == nil {
		ranInstall := false
		if !opts.Install {
			dirs := RuntimeDirsFor(sc)
			cloned, cloneSkipped, cloneWarnings := cloneRuntimeDirs(primary, path, dirs)
			result.Cloned = cloned
			result.Skipped = append(result.Skipped, cloneSkipped...)
			result.Warnings = append(result.Warnings, cloneWarnings...)
			ranInstall = len(cloned) > 0
		}
		if !ranInstall {
			// Either --install forced a real install, or none of the
			// expected runtime dirs existed in the parent yet — fall back
			// to the existing per-framework install path.
			installed, skipped := runInstalls(path, sc, opts.Herd)
			result.DepsInstalled = installed
			if len(skipped) > 0 {
				result.Skipped = append(result.Skipped, skipped...)
			}
		}
	} else if opts.NoInstall {
		result.Skipped = append(result.Skipped, "install")
	}

	// Step 4: copy .env (non-symlink — worktrees need distinct APP_URL/ports).
	if !opts.NoEnv {
		copied, skipReason := copyEnv(primary, path)
		result.EnvCopied = copied
		if skipReason != "" {
			result.Skipped = append(result.Skipped, skipReason)
		}
	} else {
		result.Skipped = append(result.Skipped, "env")
	}

	// Step 5: asset symlink (public/build for Laravel, dist/ for Next, etc.).
	if !opts.NoBuild && derr == nil {
		linked, skipReason := linkAssets(primary, path, sc)
		result.BuildLinked = linked
		if skipReason != "" {
			result.Skipped = append(result.Skipped, skipReason)
		}
	} else if opts.NoBuild {
		result.Skipped = append(result.Skipped, "build")
	}

	// Step 6: lockfile drift check — diff the worktree's manifests/lockfiles
	// against the parent's tracked HEAD content. Never fatal; surfaced as
	// warnings only so the operator can catch a leak before committing.
	result.Warnings = append(result.Warnings, checkLockfileDrift(primary, path, lockfileCandidates)...)

	// Step 7: post-create smoke checks — HEAD descends from the base branch,
	// the tree is clean, and expected runtime dirs are present. Never fatal:
	// failures are surfaced as warnings and the worktree is left in place.
	baseBranchUsed := resolveBaseBranchName(opts.BaseRef)
	result.Warnings = append(result.Warnings, runSmokeChecks(path, baseBranchUsed, RuntimeDirsFor(sc))...)

	result.Ready = true
	return result, nil
}

// runInstalls dispatches framework-aware install commands and returns which
// ones actually ran (installed) vs skipped because outputs already existed.
func runInstalls(path string, sc config.StackConfig, herd bool) ([]string, []string) {
	var installed, skipped []string

	// PHP / Laravel — composer install
	if sc.Language == "php" && fileExists(filepath.Join(path, "composer.json")) {
		if fileExists(filepath.Join(path, "vendor")) {
			skipped = append(skipped, "composer_install")
		} else {
			composerCmd := []string{"composer", "install", "--no-interaction"}
			if herd && hasBin("herd") {
				composerCmd = append([]string{"herd"}, composerCmd...)
			}
			if _, _, err := gitpkg.RunInDir(path, composerCmd...); err == nil {
				installed = append(installed, "composer")
			}
		}
	}

	// Node — pick package manager from detected lockfile
	if fileExists(filepath.Join(path, "package.json")) {
		if fileExists(filepath.Join(path, "node_modules")) {
			skipped = append(skipped, "node_install")
		} else {
			pm := sc.NodePackageManager
			if pm == "" {
				pm = "npm"
			}
			cmd := []string{pm, "install"}
			if _, _, err := gitpkg.RunInDir(path, cmd...); err == nil {
				installed = append(installed, pm)
			}
		}
	}

	// Python — uv-first (per CLAUDE.md Python-is-uv-only rule).
	if sc.Language == "python" {
		hasPyproject := fileExists(filepath.Join(path, "pyproject.toml"))
		hasReqs := fileExists(filepath.Join(path, "requirements.txt"))
		switch {
		case fileExists(filepath.Join(path, ".venv")):
			skipped = append(skipped, "python_install")
		case (hasPyproject || hasReqs) && !hasBin("uv"):
			skipped = append(skipped, "python_no_uv")
		case hasPyproject && hasBin("uv"):
			if _, _, err := gitpkg.RunInDir(path, "uv", "sync"); err == nil {
				installed = append(installed, "uv")
			}
		case hasReqs && hasBin("uv"):
			if _, _, err := gitpkg.RunInDir(path, "uv", "pip", "install", "-r", "requirements.txt"); err == nil {
				installed = append(installed, "uv-pip")
			}
		}
	}

	// Go — go mod download always runs (no idempotency skip). Cache-aware in Go
	// itself, so the cost is near-zero on re-entry; skipping would mean inspecting
	// GOPATH/pkg/mod for every module in go.sum. Documented inconsistency vs
	// composer/node/python branches; see cli/CLAUDE.md StackConfig section.
	if sc.Language == "go" && fileExists(filepath.Join(path, "go.mod")) {
		if _, _, err := gitpkg.RunInDir(path, "go", "mod", "download"); err == nil {
			installed = append(installed, "go-mod")
		}
	}

	return installed, skipped
}

// copyEnv reads primary/.env and writes it to worktree/.env. Uses copy (not
// symlink) because worktrees need distinct APP_URL, ports, session domains.
// Returns (copied bool, skipReason string).
func copyEnv(primary, worktree string) (bool, string) {
	src := filepath.Join(primary, ".env")
	dst := filepath.Join(worktree, ".env")

	if _, err := os.Stat(src); err != nil {
		return false, "env_no_source"
	}
	if _, err := os.Stat(dst); err == nil {
		return false, "env_already_present"
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return false, fmt.Sprintf("env_open_failed:%v", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return false, fmt.Sprintf("env_create_failed:%v", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return false, fmt.Sprintf("env_copy_failed:%v", err)
	}
	return true, ""
}

// linkAssets creates a symlink from primary's built-asset directory to the
// worktree's, so sub-agents can skip rebuilding in autonomous pipelines.
// Returns (linked bool, skipReason string).
func linkAssets(primary, worktree string, sc config.StackConfig) (bool, string) {
	targetRel := assetDirFor(sc)
	if targetRel == "" {
		return false, "build_no_target"
	}

	src := filepath.Join(primary, targetRel)
	dst := filepath.Join(worktree, targetRel)

	if _, err := os.Stat(src); err != nil {
		return false, "build_no_source"
	}
	// Idempotent: if the target already exists (file, dir, or symlink), leave it.
	if _, err := os.Lstat(dst); err == nil {
		return false, "build_already_present"
	}

	// Ensure the parent directory exists in the worktree before symlinking.
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, fmt.Sprintf("build_mkdir_failed:%v", err)
	}
	if err := os.Symlink(src, dst); err != nil {
		return false, fmt.Sprintf("build_symlink_failed:%v", err)
	}
	return true, ""
}

// assetDirFor returns the conventional built-asset directory path (relative
// to the repo root) for the given stack. Empty string means "no conventional
// asset dir — skip linking".
func assetDirFor(sc config.StackConfig) string {
	switch sc.Framework {
	case "laravel":
		return filepath.Join("public", "build")
	case "nextjs":
		return ".next"
	case "nuxt":
		return ".output"
	}
	switch sc.Language {
	case "node":
		return "dist"
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// hasBin returns true if the named binary is on PATH.
func hasBin(name string) bool {
	p, err := exec.LookPath(name)
	return err == nil && p != ""
}

// ─── Behavior 1: APFS copy-on-write runtime-dir cloning ────────────────────

// RuntimeDirsFor derives the set of gitignored, install-produced runtime
// directories for the given stack — e.g. `vendor` + `bootstrap/cache` for
// PHP, `node_modules` (+ the conventional built-asset dir) for Node. Exported
// so both cloneRuntimeDirs (behavior 1) and runSmokeChecks (behavior 3) key
// off the same derivation.
func RuntimeDirsFor(sc config.StackConfig) []string {
	var dirs []string
	switch sc.Language {
	case "php":
		dirs = append(dirs, "vendor", filepath.Join("bootstrap", "cache"))
	case "node":
		dirs = append(dirs, "node_modules")
	}
	if asset := assetDirFor(sc); asset != "" {
		dirs = append(dirs, asset)
	}
	return dirs
}

// cloneRuntimeDirs clones each dir in dirs from primary into worktree using
// APFS copy-on-write (`cp -cR`), falling back to a plain recursive copy when
// cloning isn't supported (non-APFS volumes, or a `cp` without clonefile
// support). Dirs absent from the parent, or already present in the worktree,
// are skipped (not an error) so the caller's install-fallback path can cover
// them instead. Compiled-cache files that bake in the parent's absolute path
// (bootstrap/cache/*.php) are deleted from the clone so the worktree
// recompiles fresh against its own .env/paths on first use.
func cloneRuntimeDirs(primary, worktree string, dirs []string) (cloned, skipped, warnings []string) {
	for _, d := range dirs {
		src := filepath.Join(primary, d)
		dst := filepath.Join(worktree, d)

		if !fileExists(src) {
			skipped = append(skipped, "clone_missing:"+d)
			continue
		}
		if fileExists(dst) {
			skipped = append(skipped, "clone_already_present:"+d)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			warnings = append(warnings, fmt.Sprintf("clone %s: mkdir failed: %v", d, err))
			continue
		}

		if _, _, err := gitpkg.RunInDir("", "cp", "-cR", src, dst); err == nil {
			cloned = append(cloned, d)
			continue
		}
		if _, _, err := gitpkg.RunInDir("", "cp", "-R", src, dst); err == nil {
			cloned = append(cloned, d)
			warnings = append(warnings, fmt.Sprintf("APFS clone unavailable for %s — used plain copy", d))
			continue
		} else {
			warnings = append(warnings, fmt.Sprintf("clone %s failed: %v", d, err))
		}
	}

	// Drop compiled caches that bake in the parent's absolute path (e.g.
	// Laravel's bootstrap/cache/config.php holds the parent's APP_URL) so the
	// worktree reads its own config fresh instead of the parent's.
	cacheDir := filepath.Join(worktree, "bootstrap", "cache")
	if matches, err := filepath.Glob(filepath.Join(cacheDir, "*.php")); err == nil {
		for _, m := range matches {
			os.Remove(m)
		}
	}

	return cloned, skipped, warnings
}

// ─── Behavior 2: lockfile drift check ──────────────────────────────────────

// lockfileCandidates is the fixed set of manifest/lockfiles checked for
// drift after a worktree is created — covers every ecosystem this CLI
// detects (composer, npm/pnpm/bun/yarn) per the StackConfig.NodePackageManager
// table in cli/CLAUDE.md.
var lockfileCandidates = []string{
	"composer.json",
	"composer.lock",
	"package.json",
	"package-lock.json",
	"pnpm-lock.yaml",
	"bun.lock",
	"bun.lockb",
	"yarn.lock",
}

// driftDecision reports whether two file contents differ, ignoring
// leading/trailing whitespace (avoids false positives from a trailing
// newline mismatch introduced by `git show`). Pure — no I/O.
func driftDecision(headContent, worktreeContent string) bool {
	return strings.TrimSpace(headContent) != strings.TrimSpace(worktreeContent)
}

// checkLockfileDrift diffs each candidate manifest/lockfile present in the
// worktree against the parent's tracked HEAD content (`git show HEAD:<file>`
// run in the parent worktree). Returns one warning per drifted or
// newly-untracked file. Never fatal — the caller only surfaces the warnings.
func checkLockfileDrift(primary, worktree string, files []string) []string {
	var warnings []string
	for _, f := range files {
		wtPath := filepath.Join(worktree, f)
		if !fileExists(wtPath) {
			continue
		}
		wtBytes, err := os.ReadFile(wtPath)
		if err != nil {
			continue
		}
		headContent, _, err := gitpkg.RunInDir(primary, "git", "show", "HEAD:"+f)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("lockfile drift: %s exists in worktree but not at parent HEAD", f))
			continue
		}
		if driftDecision(headContent, string(wtBytes)) {
			warnings = append(warnings, fmt.Sprintf("lockfile drift: %s differs from parent HEAD", f))
		}
	}
	return warnings
}

// ─── Behavior 3: post-create smoke checks ──────────────────────────────────

// resolveBaseBranchName mirrors gitpkg.WorktreeSetup's base-branch resolution
// (explicit ref override > .kaisser.yml staging_branch > "main") so the
// post-create smoke check verifies against the same base the worktree was
// actually created from. baseRef may be a bare branch name or an
// "origin/<branch>" ref; the "origin/" prefix (if any) is stripped.
func resolveBaseBranchName(baseRef string) string {
	if b := strings.TrimPrefix(baseRef, "origin/"); b != "" {
		return b
	}
	cfg, _ := config.LoadBravrosConfig()
	if cfg != nil && cfg.StagingBranch != "" {
		return cfg.StagingBranch
	}
	return "main"
}

// smokeCheckInputs carries the pre-computed facts evaluateSmokeChecks needs
// to decide which warnings to emit. Kept separate from runSmokeChecks (which
// performs the actual git/fs I/O) so the decision logic is pure and
// table-testable.
type smokeCheckInputs struct {
	BaseBranch  string   // empty = skip the ancestor check
	IsAncestor  bool     // HEAD descends from origin/<BaseBranch>
	StatusClean bool     // `git status --porcelain` is empty
	MissingDirs []string // expected runtime dirs not found in the worktree
}

// evaluateSmokeChecks turns smokeCheckInputs into human-readable warnings.
// Pure — no I/O. Never returns an error; smoke-check failures are always
// advisory (the worktree is left in place regardless).
func evaluateSmokeChecks(in smokeCheckInputs) []string {
	var warnings []string
	if in.BaseBranch != "" && !in.IsAncestor {
		warnings = append(warnings, fmt.Sprintf("smoke check: HEAD does not descend from origin/%s", in.BaseBranch))
	}
	if !in.StatusClean {
		warnings = append(warnings, "smoke check: worktree has uncommitted changes")
	}
	for _, d := range in.MissingDirs {
		warnings = append(warnings, fmt.Sprintf("smoke check: expected runtime dir %q is missing", d))
	}
	return warnings
}

// runSmokeChecks performs the post-create sanity checks against a real
// worktree: HEAD-descends-from-base (`git merge-base --is-ancestor`), a clean
// `git status`, and presence of expectedDirs. Delegates the pass/fail
// decision to evaluateSmokeChecks.
func runSmokeChecks(worktreePath, baseBranch string, expectedDirs []string) []string {
	isAncestor := false
	if baseBranch != "" {
		_, _, err := gitpkg.RunInDir(worktreePath, "git", "merge-base", "--is-ancestor", "origin/"+baseBranch, "HEAD")
		isAncestor = err == nil
	}

	statusClean := false
	if out, _, err := gitpkg.RunInDir(worktreePath, "git", "status", "--porcelain"); err == nil && strings.TrimSpace(out) == "" {
		statusClean = true
	}

	var missing []string
	for _, d := range expectedDirs {
		if !fileExists(filepath.Join(worktreePath, d)) {
			missing = append(missing, d)
		}
	}

	return evaluateSmokeChecks(smokeCheckInputs{
		BaseBranch:  baseBranch,
		IsAncestor:  isAncestor,
		StatusClean: statusClean,
		MissingDirs: missing,
	})
}
