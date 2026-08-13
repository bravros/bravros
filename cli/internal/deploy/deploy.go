package deploy

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// DeployOpts configures the deploy operation.
type DeployOpts struct {
	DryRun         bool
	CountOnly      bool
	Force          bool     // overwrite every source file unconditionally; skip mtime/hash skip-unchanged check
	NoPrune        bool     // when true, skip orphan-pruning (preserves pre-B-0193 behavior)
	SourceDir      string   // defaults to cwd
	TargetDir      string   // defaults to ~/.claude/
	PreserveSkills []string // skill dir names that must NOT be pruned even when absent from source
	EnabledSkills  []string // opt-in allowlist; when non-empty only listed + core skills deploy
	FilterMode     bool     // when true, EnabledSkills is a per-invocation additive filter (--filter flag); non-filtered skills are preserved and never pruned
}

// DeployResult holds the outcome of a deploy operation.
type DeployResult struct {
	FilesDeployed  int      `json:"files_deployed"`
	Dirs           []string `json:"dirs"`
	DryRun         bool     `json:"dry_run"`
	CountOnly      bool     `json:"count_only,omitempty"`
	Forced         bool     `json:"forced,omitempty"`
	NoPrune        bool     `json:"no_prune,omitempty"`
	Files          []string `json:"files,omitempty"`
	Skipped        []string `json:"skipped,omitempty"`
	Pruned         []string `json:"pruned,omitempty"`          // orphans removed (or would be removed in dry-run)
	SkillsDeployed []string `json:"skills_deployed,omitempty"` // skill names deployed via SHA manifest
	SkillsSkipped  []string `json:"skills_skipped,omitempty"`  // skill names skipped (SHA unchanged)
	LockedSkipped  []string `json:"locked_skipped,omitempty"`  // destinations the operator froze (uchg / chattr +i / read-only)
}

// pruneSubtrees lists the subdirectories under TargetDir that are eligible
// for orphan pruning. Top-level entries directly inside each subtree are
// compared against the source repo; missing source counterparts are pruned.
//
// Allowlist (NEVER prune):
//   - bin/        (bravros binary)
//   - projects/   (per-project state, .planning, MEMORY.md)
//   - state/      (promote-token, locks)
//   - settings.local.json (user-local overrides)
//   - CLAUDE.md   (managed-global block reconciled from home/CLAUDE.md — see
//                  reconcileGlobalClaudeMd; NEVER whole-file copied, which would
//                  clobber the user's personal content outside the markers)
//   - mcp.json    (machine-specific)
var pruneSubtrees = []string{"skills", "templates", "hooks", "agents"}

// dirMappings maps source subdirs to target subdirs for the per-file copy loop.
// skills/ is included here for file-counting and Dirs population; the actual
// per-skill atomic deployment is handled by the SHA-manifest loop in Deploy().
// templates/ is included so that templates/.githooks/ stays in sync with the
// source repo on every `bravros deploy` — mirroring what install.sh does at
// install.sh:492-496. copyFile preserves source file modes (including the
// executable bit), but we also apply an explicit chmod after the copy loop
// so that hook files are always executable even if the source mode was lost.
var dirMappings = []struct {
	src string // relative to SourceDir
	dst string // relative to TargetDir
}{
	{"skills", "skills"},
	{"hooks", "hooks"},
	{"templates", "templates"},
	// agents/ holds Claude Code custom subagent definitions (flat *.md files,
	// no per-skill SHA semantics). Deployed by the same per-file copy loop as
	// hooks/templates; orphan-pruned via pruneSubtrees above.
	{"agents", "agents"},
	// output-styles/ holds custom Claude Code output styles (flat *.md, selected
	// via /config). Copy-only — deliberately NOT in pruneSubtrees, so a
	// hand-written style living only in ~/.claude/output-styles/ survives deploys.
	{"output-styles", "output-styles"},
}

// fileMappings maps individual source files to target paths.
var fileMappings = []struct {
	src string
	dst string
}{
	{"config/settings.json", "settings.json"},
	{"config/statusline.sh", "statusline.sh"},
	// NOTE: root CLAUDE.md is deliberately NOT here. It is the repo's project doc,
	// not the deployed global. Whole-file copying it to ~/.claude/CLAUDE.md clobbers
	// the user's personal content. The managed global (home/CLAUDE.md) is reconciled
	// into a marker block by reconcileGlobalClaudeMd() after the copy loop.
}

// cleanBrokenSymlinks walks the prunable subtrees under targetDir (skills/,
// hooks/, templates/) and removes any broken symlink — an entry where
// os.Lstat succeeds (the directory entry exists) but os.Stat fails (the
// symlink target is missing). This pre-pass runs unconditionally before every
// deploy so that copyFile never hits ENOENT when trying to open a broken
// symlink at the destination.
//
// Only pruneSubtrees (skills, hooks, templates, agents) are walked; top-level
// targetDir entries and allowlisted dirs (bin/, projects/, state/) are never
// touched.
func cleanBrokenSymlinks(targetDir string) error {
	for _, sub := range pruneSubtrees {
		subDir := filepath.Join(targetDir, sub)
		if _, err := os.Lstat(subDir); err != nil {
			// Subtree doesn't exist yet — nothing to clean.
			continue
		}
		err := filepath.WalkDir(subDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// Unreadable entry — skip; don't abort the whole walk.
				return nil
			}
			if d.Type()&os.ModeSymlink == 0 {
				// Not a symlink — regular file or dir, leave it alone.
				return nil
			}
			// It IS a symlink. Check whether the target exists.
			if _, statErr := os.Stat(path); statErr != nil {
				// Target missing → broken symlink → remove it.
				return os.Remove(path)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("clean broken symlinks in %s: %w", sub, err)
		}
	}
	return nil
}

// isSkillEnabled returns true when the skill should be included in the deploy.
//
// Decision tree (first match wins):
//  1. enabledList is empty → deploy ALL skills (backward-compat default).
//  2. Skill directory name is in enabledList → deploy.
//  3. SKILL.md frontmatter contains `core: true` → deploy (SDLC essentials always deploy).
//  4. Otherwise → skip.
func isSkillEnabled(skillName, skillDir string, enabledList []string) bool {
	if len(enabledList) == 0 {
		return true
	}
	for _, name := range enabledList {
		if name == skillName {
			return true
		}
	}
	// Check for `core: true` in SKILL.md frontmatter.
	return IsSkillCore(filepath.Join(skillDir, "SKILL.md"))
}

// IsSkillCore reads a SKILL.md file and returns true if the frontmatter contains
// a `core: true` field. Only the YAML frontmatter (between the first pair of ---
// delimiters) is scanned; the body is ignored. Exported so callers outside the
// deploy package (e.g. selfupdate) can apply the same enabled-skill semantics
// without re-implementing a fragile prefix-bytes heuristic.
func IsSkillCore(skillMDPath string) bool {
	f, err := os.Open(skillMDPath)
	if err != nil {
		return false
	}
	defer f.Close()

	inFrontmatter := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			}
			// Second --- closes the frontmatter.
			break
		}
		if !inFrontmatter {
			continue
		}
		// Look for: core: true
		trimmed := strings.TrimSpace(line)
		if trimmed == "core: true" {
			return true
		}
	}
	return false
}

// Deploy copies the claude config repo to ~/.claude/.
func Deploy(opts DeployOpts) (*DeployResult, error) {
	if opts.SourceDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("cannot determine cwd: %w", err)
		}
		opts.SourceDir = cwd
	}

	// Validate: must be a toolkit source checkout (detected by content — see IsClaudeRepo).
	if !IsClaudeRepo(opts.SourceDir) {
		return nil, fmt.Errorf("%s is not a bravros source checkout: expected a skills/ directory and a cli/go.mod for this module.\n\nRun deploy from the root of your bravros clone, or pass --source-dir <path>", opts.SourceDir)
	}

	if opts.TargetDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot determine home dir: %w", err)
		}
		opts.TargetDir = filepath.Join(home, ".claude")
	}

	// Load the manifest from the runtime skills directory.
	manifestPath := filepath.Join(opts.TargetDir, "skills", manifestFileName)
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("load manifest: %w", err)
	}

	// Collect all files to deploy (all dirMappings + fileMappings).
	// This includes skills/ for file-counting and Dirs population; actual skill
	// file copying is done by the SHA-manifest loop below.
	var files []string
	dirSet := map[string]bool{}

	for _, dm := range dirMappings {
		srcDir := filepath.Join(opts.SourceDir, dm.src)
		if _, err := os.Stat(srcDir); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				rel, _ := filepath.Rel(opts.SourceDir, path)
				if dm.src == "skills" && isNonRuntimeSkillPath(rel) {
					return filepath.SkipDir
				}
				// When an enabled-skills allowlist is set, skip skill directories
				// that are not in the list AND are not core skills.
				if dm.src == "skills" && len(opts.EnabledSkills) > 0 {
					// Identify skill dirs: one level below skills/ (e.g. skills/plan-wt).
					// path == srcDir means we're at the top-level skills/ dir — don't skip.
					if path != srcDir {
						parentRel, _ := filepath.Rel(srcDir, filepath.Dir(path))
						if parentRel == "." {
							// This is a top-level skill directory.
							skillName := d.Name()
							if !isSkillEnabled(skillName, path, opts.EnabledSkills) {
								return filepath.SkipDir
							}
						}
					}
				}
				return nil
			}
			// Skip .DS_Store
			if d.Name() == ".DS_Store" {
				return nil
			}
			rel, _ := filepath.Rel(opts.SourceDir, path)
			files = append(files, rel)
			dirSet[dm.dst] = true
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walking %s: %w", dm.src, err)
		}
	}

	for _, fm := range fileMappings {
		srcPath := filepath.Join(opts.SourceDir, fm.src)
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			continue
		}
		files = append(files, fm.src)
	}

	sort.Strings(files)

	// Build sorted dirs list
	var dirs []string
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	result := &DeployResult{
		FilesDeployed: len(files),
		Dirs:          dirs,
		DryRun:        opts.DryRun,
		CountOnly:     opts.CountOnly,
		Forced:        opts.Force,
		NoPrune:       opts.NoPrune,
	}

	if opts.CountOnly {
		return result, nil
	}

	// Pre-pass: remove broken symlinks at the destination before processing.
	// Skipped in dry-run so the preview never mutates the runtime tree; the
	// trade-off is that a dry-run cannot observe the post-cleanup `dstIncomplete`
	// state that a real deploy would surface (the SHA-manifest loop below still
	// reports the SHA-mismatch / missing-dest cases accurately).
	if !opts.DryRun {
		if err := cleanBrokenSymlinks(opts.TargetDir); err != nil {
			return nil, fmt.Errorf("pre-deploy symlink cleanup: %w", err)
		}
	}

	// --- SHA-manifest skill loop ---
	// For each enabled source skill, compare content SHA against the manifest.
	// Deploy (atomic wipe+copy) when SHA differs, skill is new, or --force.
	// Skip when SHA matches (unless --force).
	//
	// This loop runs for both real deploys and dry-runs: dry-run computes SHAs
	// and populates SkillsDeployed/SkillsSkipped but does NOT touch the runtime
	// or write the manifest file.
	srcSkillsDir := filepath.Join(opts.SourceDir, "skills")
	skillEntries, _ := os.ReadDir(srcSkillsDir) // nil entries OK if dir missing

	// Track which skills are enabled in source (for manifest prune pass).
	enabledSourceSkills := make(map[string]struct{})

	for _, e := range skillEntries {
		if !e.IsDir() {
			continue
		}
		skillName := e.Name()

		// Skip non-runtime dirs (shared/, _shared/).
		if NonRuntimeSkillDir(skillName) {
			continue
		}

		skillSrcDir := filepath.Join(srcSkillsDir, skillName)

		// Skip skills that are not enabled.
		if !isSkillEnabled(skillName, skillSrcDir, opts.EnabledSkills) {
			continue
		}

		enabledSourceSkills[skillName] = struct{}{}

		// Compute content SHA for this skill (symlink-resolving walk).
		newSHA, err := ComputeSkillSHA(skillSrcDir)
		if err != nil {
			return nil, fmt.Errorf("compute SHA for skill %s: %w", skillName, err)
		}

		existingSHA, inManifest := manifest.Skills[skillName]
		dstSkillDir := filepath.Join(opts.TargetDir, "skills", skillName)

		// Force redeploy if:
		//   - SHA differs from manifest
		//   - skill not yet in manifest
		//   - --force flag
		//   - destination skill dir is missing
		//   - destination skill is incomplete (e.g. cleanBrokenSymlinks removed a
		//     broken symlink from inside the dir, leaving it partially populated;
		//     SKILL.md absence is used as the completeness proxy)
		_, dstStatErr := os.Stat(dstSkillDir)
		dstMissing := os.IsNotExist(dstStatErr)
		dstIncomplete := false
		// Run the SKILL.md presence check in both real and dry-run modes so the
		// preview reports the same `needsDeploy` decision a real deploy would
		// make. The check is read-only (no mutation), safe for dry-run.
		if !dstMissing {
			if _, err := os.Stat(filepath.Join(dstSkillDir, "SKILL.md")); os.IsNotExist(err) {
				dstIncomplete = true
			}
		}
		needsDeploy := !inManifest || existingSHA != newSHA || opts.Force || dstMissing || dstIncomplete

		if needsDeploy {
			if !opts.DryRun {
				// Atomic replacement: wipe then recopy (resolving symlinks to real files).
				if err := os.RemoveAll(dstSkillDir); err != nil {
					return nil, fmt.Errorf("remove skill %s before redeploy: %w", skillName, err)
				}
				if err := copySkillDir(skillSrcDir, dstSkillDir); err != nil {
					return nil, fmt.Errorf("copy skill %s: %w", skillName, err)
				}
			}
			manifest.Skills[skillName] = newSHA
			result.SkillsDeployed = append(result.SkillsDeployed, skillName)
		} else {
			result.SkillsSkipped = append(result.SkillsSkipped, skillName)
		}
	}

	// Prune manifest entries for skills no longer in enabled source.
	// These are skills that WERE deployed (exist in manifest) but are now
	// disabled or removed from source.
	//
	// Guard: when FilterMode is set (i.e. --filter was passed per-invocation), a
	// filtered deploy is additive — it must NEVER prune skills that were deployed
	// by a prior full run but are simply absent from the current filter. Those
	// skills remain valid entries in the manifest and their on-disk dirs must
	// survive untouched. Only prune when every skill in source was considered
	// (i.e. FilterMode is false — either no EnabledSkills or config-level allowlist).
	var manifestToPrune []string
	for name := range manifest.Skills {
		if _, ok := enabledSourceSkills[name]; !ok {
			if opts.FilterMode {
				// Filtered deploy: this skill was not in the filter scope.
				// Preserve it — do not add to the prune list.
				continue
			}
			manifestToPrune = append(manifestToPrune, name)
		}
	}
	sort.Strings(manifestToPrune)

	// Build preserve set for skills.
	preserveSet := make(map[string]struct{}, len(opts.PreserveSkills))
	for _, name := range opts.PreserveSkills {
		if name != "" {
			preserveSet[name] = struct{}{}
		}
	}

	for _, name := range manifestToPrune {
		if _, preserved := preserveSet[name]; preserved {
			continue
		}
		if !opts.DryRun && !opts.NoPrune {
			if err := os.RemoveAll(filepath.Join(opts.TargetDir, "skills", name)); err != nil {
				return nil, fmt.Errorf("prune skill %s: %w", name, err)
			}
			delete(manifest.Skills, name)
		}
		result.Pruned = append(result.Pruned, filepath.Join("skills", name))
	}

	// Sort for deterministic output.
	sort.Strings(result.SkillsDeployed)
	sort.Strings(result.SkillsSkipped)

	// Save manifest after all skill processing (skipped on dry-run).
	if !opts.DryRun {
		if err := SaveManifest(manifestPath, manifest); err != nil {
			return nil, fmt.Errorf("save manifest: %w", err)
		}
	}

	// Detect orphans (deployed entries with no source counterpart) for both
	// dry-run and real deploys. Pruning is the default; --no-prune preserves
	// orphans for migration scenarios where the user wants the old behavior.
	//
	// detectOrphans covers all pruneSubtrees (skills, hooks, templates, agents). For
	// skills, it catches entries in the runtime that were never managed by the
	// manifest (e.g. manually placed skill dirs, nonRuntime dirs like "shared").
	if !opts.NoPrune {
		orphans, err := detectOrphans(opts.SourceDir, opts.TargetDir, opts.PreserveSkills)
		if err != nil {
			return nil, fmt.Errorf("detect orphans: %w", err)
		}
		// Merge with manifest-based prune list (dedup).
		existing := make(map[string]struct{}, len(result.Pruned))
		for _, p := range result.Pruned {
			existing[p] = struct{}{}
		}
		for _, o := range orphans {
			if _, dup := existing[o]; !dup {
				result.Pruned = append(result.Pruned, o)
			}
		}
		sort.Strings(result.Pruned)
	}

	// Include file list for dry-run.
	// In --force --dry-run, every source file is listed as "would copy" even
	// if its destination is byte-for-byte identical — that's the whole point
	// of forcing.
	if opts.DryRun {
		result.Files = files
		return result, nil
	}

	// Actual deploy of non-skill files (hooks/, templates/, root files).
	// Skills are handled by the SHA-manifest loop above.
	// With --force, every source file is unconditionally copied.
	// Without --force, files whose destination already matches by mtime+size
	// are skipped (incremental deploy).
	result.Files = files
	for _, rel := range files {
		// Skip skill files — they are deployed atomically per-skill above.
		if strings.HasPrefix(rel, "skills/") {
			continue
		}

		dstRel := mapSourceToDest(rel)
		src := filepath.Join(opts.SourceDir, rel)
		dst := filepath.Join(opts.TargetDir, dstRel)

		if !opts.Force && fileUpToDate(src, dst) {
			result.Skipped = append(result.Skipped, rel)
			continue
		}

		if err := copyFile(src, dst); err != nil {
			// A destination the operator deliberately froze — macOS `chflags uchg`,
			// Linux `chattr +i`, or plain read-only perms — surfaces here as
			// EPERM/EACCES. That is an intentional signal, not a deploy failure.
			//
			// Aborting on it is actively harmful: every file after this one in the
			// loop stays stale, silently. A locked ~/.claude/settings.json used to
			// kill the whole install, which is how ~/.claude/scripts/ ended up
			// months behind while `bravros update` still reported success — the
			// giveaway was two graphify scripts deployed from two DIFFERENT commits.
			if errors.Is(err, fs.ErrPermission) {
				fmt.Fprintf(os.Stderr, "⏭️  %s is locked (permission denied) — skipped, deploy continues\n", dstRel)
				result.LockedSkipped = append(result.LockedSkipped, dstRel)
				continue
			}
			return nil, fmt.Errorf("copying %s → %s: %w", rel, dstRel, err)
		}
	}

	// Reconcile the managed-global CLAUDE.md block (home/CLAUDE.md → <target>/CLAUDE.md).
	// This replaces the old whole-file copy that clobbered the user's personal content.
	// Non-fatal: a reconcile failure must never abort a deploy. Skipped in dry-run.
	if !opts.DryRun {
		if err := reconcileGlobalClaudeMd(opts.SourceDir, opts.TargetDir); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  managed CLAUDE.md reconcile skipped: %v\n", err)
		}
	}

	// Apply pruning AFTER deploy so any in-flight rename that introduces a new
	// dst before removing the old src is handled atomically (deploy-then-prune).
	if !opts.NoPrune {
		for _, rel := range result.Pruned {
			// Skill orphans from the manifest prune pass were already removed above.
			// Here we remove non-skill orphans (hooks/, templates/) and any runtime
			// skill entries that detectOrphans found (nonRuntime dirs, manually placed).
			// To avoid double-RemoveAll, only remove if the path still exists.
			full := filepath.Join(opts.TargetDir, rel)
			if _, err := os.Stat(full); !os.IsNotExist(err) {
				if err := os.RemoveAll(full); err != nil {
					return nil, fmt.Errorf("prune %s: %w", rel, err)
				}
			}
		}
	}

	// Ensure .githooks/* files are executable — this mirrors install.sh:492-496
	// which does an explicit chmod +x after copying templates. copyFile preserves
	// source modes, but an explicit chmod here guarantees executability even if
	// the source file mode was accidentally stripped (e.g. by a git reset on a
	// system that doesn't track executable bits).
	// README.md inside .githooks/ is intentionally excluded — it is documentation only.
	githooksDst := filepath.Join(opts.TargetDir, "templates", ".githooks")
	if entries, err := os.ReadDir(githooksDst); err == nil {
		for _, e := range entries {
			if e.IsDir() || e.Name() == "README.md" {
				continue
			}
			_ = os.Chmod(filepath.Join(githooksDst, e.Name()), 0755)
		}
	}

	return result, nil
}

// copySkillDir recursively copies a skill directory from src to dst,
// materializing symlinks as real files (no symlinks in the destination).
//
// For each file entry (including symlinks that resolve to files):
//   - os.Stat(path) is used to follow symlinks and get the resolved file info.
//   - If os.Stat fails (broken symlink), an error is returned.
//   - The file content is read via os.Open (which follows symlinks on Go) and
//     written to the destination as a regular file with the resolved mode.
//
// The manifest file (.deploy-manifest.json) is skipped if encountered (defensive
// guard — it lives at skills/.deploy-manifest.json, sibling to skill dirs, but
// guard here for safety).
func copySkillDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip the manifest file itself (defensive guard).
		if d.Name() == manifestFileName {
			return nil
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		// Always use os.Stat to follow symlinks.
		info, statErr := os.Stat(path)
		if statErr != nil {
			// Broken symlink — surface as hard error.
			return fmt.Errorf("broken symlink at %s: %w", path, statErr)
		}

		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		// Regular file (or symlink resolved to file): materialize as a real file.
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}

		in, err := os.Open(path) // os.Open follows symlinks
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		in.Close()
		out.Close()
		return copyErr
	})
}

// fileUpToDate returns true when dst exists and has the same size and a
// modification time >= src. This is the cheap incremental-deploy skip-check
// that --force bypasses.
func fileUpToDate(src, dst string) bool {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return false
	}
	dstInfo, err := os.Stat(dst)
	if err != nil {
		return false
	}
	if srcInfo.Size() != dstInfo.Size() {
		return false
	}
	return !dstInfo.ModTime().Before(srcInfo.ModTime())
}

// IsClaudeRepo reports whether dir is the toolkit source repo.
//
// Detection is by CONTENT, not directory name. The previous check was
// `filepath.Base(dir) == "claude"`, which was already brittle in the repo it was
// written for — any worktree, rename, or fresh clone under a different folder name
// failed it — and is simply wrong here, where the repo is called "bravros".
// A source checkout is identified by the two things it always has: a skills/
// directory and a cli/go.mod declaring this module.
func IsClaudeRepo(dir string) bool {
	if fi, err := os.Stat(filepath.Join(dir, "skills")); err != nil || !fi.IsDir() {
		return false
	}
	data, err := os.ReadFile(filepath.Join(dir, "cli", "go.mod"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "module github.com/bravros/bravros/cli")
}

// mapSourceToDest converts a source-relative path to its target-relative path.
func mapSourceToDest(rel string) string {
	// Check file mappings first (config/settings.json → settings.json)
	for _, fm := range fileMappings {
		if rel == fm.src {
			return fm.dst
		}
	}
	// Directory mappings keep the same relative structure
	return rel
}

// reconcileGlobalClaudeMd reconciles the managed-global CLAUDE.md block into
// <targetDir>/CLAUDE.md from <sourceDir>/home/CLAUDE.md, preserving the user's
// personal content outside the # >>> bravros-managed-global >>> markers. It shells
// out to the deterministic reconcile script (scripts/reconcile-global-claude.py) —
// the same one install.sh and verify-install use — so the marker/migration logic
// lives in exactly one place and can never diverge across deploy paths.
//
// Never falls back to a whole-file copy: if home/CLAUDE.md is absent (pre-split
// checkout) or python3 is unavailable, it leaves the destination untouched rather
// than risk clobbering personal content. Also stashes the managed source at
// <targetDir>/templates/global-CLAUDE.md so verify-install can reconcile without
// the repo checkout. Callers treat the returned error as non-fatal.
func reconcileGlobalClaudeMd(sourceDir, targetDir string) error {
	src := filepath.Join(sourceDir, "home", "CLAUDE.md")
	if _, err := os.Stat(src); err != nil {
		return nil // pre-split checkout — nothing to reconcile, and NOT a fallback-copy case
	}
	script := filepath.Join(sourceDir, "scripts", "reconcile-global-claude.py")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("reconcile script missing at %s", script)
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		return fmt.Errorf("python3 not found — dest left untouched (no fallback copy)")
	}

	// Stash the managed source for verify-install / auto-verify-install (non-fatal).
	tmplDir := filepath.Join(targetDir, "templates")
	if mkErr := os.MkdirAll(tmplDir, 0o755); mkErr == nil {
		if cpErr := copyFile(src, filepath.Join(tmplDir, "global-CLAUDE.md")); cpErr != nil {
			fmt.Fprintf(os.Stderr, "⚠️  could not stash global-CLAUDE.md template: %v\n", cpErr)
		}
	}

	dest := filepath.Join(targetDir, "CLAUDE.md")
	out, runErr := exec.Command(py, script, "--src", src, "--dest", dest).CombinedOutput()
	if runErr != nil {
		return fmt.Errorf("reconcile script failed: %v (%s)", runErr, strings.TrimSpace(string(out)))
	}
	return nil
}

// copyFile copies src to dst, creating parent directories as needed.
// It preserves the source file's permission mode (including executable bit).
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	base := filepath.Base(dst)
	if base == "settings.json" || base == "mcp.json" {
		return mergeJSON(src, dst)
	}

	return copyFileRaw(src, dst)
}

func copyFileRaw(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func mergeJSON(src, dst string) error {
	// If dst does not exist, just copy the raw file to preserve exact bytes and formatting
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		return copyFileRaw(src, dst)
	}

	// Write backup if dst exists
	backupPath := dst + ".bak"
	data, err := os.ReadFile(dst)
	if err == nil {
		_ = os.WriteFile(backupPath, data, 0644)
	}

	srcData, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	var srcMap map[string]interface{}
	if err := json.Unmarshal(srcData, &srcMap); err != nil {
		return fmt.Errorf("parse source JSON %s: %w", src, err)
	}

	dstMap := make(map[string]interface{})
	if _, err := os.Stat(dst); err == nil {
		dstData, err := os.ReadFile(dst)
		if err == nil {
			_ = json.Unmarshal(dstData, &dstMap)
		}
	}

	mergeMaps(dstMap, srcMap)

	mergedData, err := json.MarshalIndent(dstMap, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(dst, mergedData, 0644)
}

func mergeMaps(dst, src map[string]interface{}) {
	for k, v := range src {
		if vMap, ok := v.(map[string]interface{}); ok {
			if dstVal, exists := dst[k]; exists {
				if dstMap, ok := dstVal.(map[string]interface{}); ok {
					mergeMaps(dstMap, vMap)
					continue
				}
			}
		}
		dst[k] = v
	}
}

// detectOrphans walks the prunable subtrees under targetDir and returns
// relative paths (e.g. "skills/old-skill") whose counterpart is absent in
// srcDir. Most subtrees are compared at top level, but templates/.githooks is
// checked recursively because dot-dir template orphans are real runtime hooks.
//
// For the skills subtree, detectOrphans catches entries in the runtime that
// were never managed by the SHA manifest (e.g. manually placed skill dirs,
// nonRuntime dirs like "shared", skills orphaned before manifest adoption).
//
// preserveSkills lists skill directory names (not full paths) that should NOT
// be marked as orphans even when absent from the source repo — opt-in allowlist
// for manually-added out-of-source skills (e.g. graphify). Pass nil or empty
// for the default prune-all behaviour.
//
// Returns paths sorted ascending; nil when no orphans found.
func detectOrphans(srcDir, targetDir string, preserveSkills []string) ([]string, error) {
	// Build a fast-lookup set for the preserve list.
	preserveSet := make(map[string]struct{}, len(preserveSkills))
	for _, name := range preserveSkills {
		if name != "" {
			preserveSet[name] = struct{}{}
		}
	}

	var orphans []string
	for _, sub := range pruneSubtrees {
		dstSub := filepath.Join(targetDir, sub)
		entries, err := os.ReadDir(dstSub)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if sub == "templates" && e.Name() == ".githooks" {
				githookOrphans, err := detectNestedOrphans(srcDir, targetDir, filepath.Join("templates", ".githooks"))
				if err != nil {
					return nil, err
				}
				orphans = append(orphans, githookOrphans...)
				continue
			}
			// global-CLAUDE.md is a deploy-generated artifact (stashed from
			// home/CLAUDE.md by reconcileGlobalClaudeMd for verify-install). It has
			// no source counterpart under templates/, so it must NEVER be pruned as
			// an orphan — same rationale as the .deploy-manifest.json skip.
			if sub == "templates" && e.Name() == "global-CLAUDE.md" {
				continue
			}
			// Skip dotfiles at the top level (e.g. .DS_Store, .gitkeep)
			// Exception: .deploy-manifest.json must always be skipped (it's not a skill).
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			// Honor the preserve list for the skills subtree — opt-in allowlist
			// for manually-added out-of-source skills (P-0124 / B-0237).
			// NonRuntimeSkillDir check MUST come first: shared/ and _shared/ must
			// always be pruned regardless of the preserve list (they are build
			// artifacts, not user skills).
			if sub == "skills" {
				if NonRuntimeSkillDir(e.Name()) {
					orphans = append(orphans, filepath.Join(sub, e.Name()))
					continue
				}
				if _, preserved := preserveSet[e.Name()]; preserved {
					continue // not an orphan — user opted in to keep it
				}
			}
			srcPath := filepath.Join(srcDir, sub, e.Name())
			if _, err := os.Stat(srcPath); os.IsNotExist(err) {
				orphans = append(orphans, filepath.Join(sub, e.Name()))
			}
		}
	}
	sort.Strings(orphans)
	return orphans, nil
}

func detectNestedOrphans(srcDir, targetDir, relRoot string) ([]string, error) {
	dstRoot := filepath.Join(targetDir, relRoot)
	srcRoot := filepath.Join(srcDir, relRoot)
	if _, err := os.Stat(srcRoot); os.IsNotExist(err) {
		return []string{relRoot}, nil
	}

	var orphans []string
	err := filepath.WalkDir(dstRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dstRoot {
			return nil
		}
		rel, err := filepath.Rel(targetDir, path)
		if err != nil {
			return err
		}
		srcPath := filepath.Join(srcDir, rel)
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			orphans = append(orphans, rel)
			if d.IsDir() {
				return filepath.SkipDir
			}
		}
		return nil
	})
	return orphans, err
}

// DeployableFile returns true if the given path (relative to repo root) is
// part of the deploy set.
func DeployableFile(rel string) bool {
	if isNonRuntimeSkillPath(rel) || strings.HasPrefix(rel, "skills/shared/") || strings.HasPrefix(rel, "skills/_shared/") {
		return false
	}
	for _, fm := range fileMappings {
		if rel == fm.src {
			return true
		}
	}
	for _, dm := range dirMappings {
		if strings.HasPrefix(rel, dm.src+"/") {
			return true
		}
	}
	return false
}

// NonRuntimeSkillDir reports whether a skills/ subdirectory is a non-runtime
// shared-asset directory (shared/, _shared/) rather than a deployable skill.
// Both Deploy() and selfupdate.detectSkillsDrift() gate on this so the manifest
// writer and the drift detector agree on what counts as a skill — see R-0004,
// where detectSkillsDrift iterated shared/ (never in the manifest) and reported
// perpetual false-positive drift.
func NonRuntimeSkillDir(name string) bool {
	return name == "shared" || name == "_shared"
}

func isNonRuntimeSkillPath(rel string) bool {
	return rel == filepath.Join("skills", "shared") || rel == filepath.Join("skills", "_shared")
}
