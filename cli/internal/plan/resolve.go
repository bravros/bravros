package plan

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// planFolderIDRe matches the "P-NNNN" id prefix of a plan folder name
// (e.g. "P-0180-feat-plan-folders-first-class" → "P-0180").
var planFolderIDRe = regexp.MustCompile(`^P-\d{4}`)

// ResolvePlanEntryFile resolves a plan reference — either a single-file plan
// path or a P-NNNN-<slug>/ folder — to its canonical entry Markdown file.
//
// Locked decision #3/#4 (P-0180 interview): this is the ONE shared resolver
// every plan consumer (FindPlanFile, ParsePlanHeader, CheckPlanCheckStatus,
// lint, …) routes through — no per-call-site ad-hoc folder logic.
//
// Behavior:
//   - If pathOrDir does not exist, or exists but is NOT a directory, it is
//     returned unchanged. This is the backward-compatible fast path for every
//     existing single-file plan caller — zero behavior change.
//   - If pathOrDir IS a directory, the canonical entry file is resolved via
//     fallback order:
//     1. PLAN.md — the canonical entry file for new folder-plans.
//     2. an id-prefixed *.md file (e.g. P-0180-*.md) — legacy/manually-made
//     folders that carry the plan body under the id-stamped name instead.
//     3. TASKLIST.md — legacy folders (e.g. paylog-style) that predate the
//     PLAN.md convention.
//     4. the first *.md file (sorted by name for determinism) that carries a
//     YAML frontmatter block (a literal "---" as its first non-blank line).
//   - Returns "" when the directory contains no resolvable entry file (e.g.
//     an empty folder).
func ResolvePlanEntryFile(pathOrDir string) string {
	info, err := os.Stat(pathOrDir)
	if err != nil || !info.IsDir() {
		// Not a directory (plain file, or path doesn't exist yet) — hand back
		// unchanged so existing single-file plan callers are unaffected.
		return pathOrDir
	}

	// Fallback 1: PLAN.md.
	planMD := filepath.Join(pathOrDir, "PLAN.md")
	if fi, statErr := os.Stat(planMD); statErr == nil && !fi.IsDir() {
		return planMD
	}

	entries, readErr := os.ReadDir(pathOrDir)
	if readErr != nil {
		return ""
	}

	var mdNames []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".md") {
			mdNames = append(mdNames, e.Name())
		}
	}
	sort.Strings(mdNames)

	// Fallback 2: id-prefixed *.md (e.g. P-0180-foo.md).
	if idPrefix := planFolderIDRe.FindString(filepath.Base(pathOrDir)); idPrefix != "" {
		for _, name := range mdNames {
			if strings.HasPrefix(name, idPrefix) {
				return filepath.Join(pathOrDir, name)
			}
		}
	}

	// Fallback 3: TASKLIST.md.
	taskList := filepath.Join(pathOrDir, "TASKLIST.md")
	if fi, statErr := os.Stat(taskList); statErr == nil && !fi.IsDir() {
		return taskList
	}

	// Fallback 4: first frontmatter-bearing *.md file.
	for _, name := range mdNames {
		if hasFrontmatter(filepath.Join(pathOrDir, name)) {
			return filepath.Join(pathOrDir, name)
		}
	}

	return ""
}

// hasFrontmatter reports whether the file at path begins with a YAML
// frontmatter block — a literal "---" line as its first non-blank line.
func hasFrontmatter(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		return line == "---"
	}
	return false
}

// planDirSeedTemplate is the seeded PLAN.md placeholder body written by
// ReservePlanDir. It carries the minimal frontmatter a folder-plan needs to
// be resolved by ResolvePlanEntryFile and read by ParsePlanHeader before the
// caller fills in the real plan content.
const planDirSeedTemplate = "---\nid: %q\ntitle: %q\n---\n\n# %s\n"

// ReservePlanDir atomically reserves the next available P-NNNN ID inside dir
// and creates the folder-plan directory P-NNNN-<slug>/ with a seeded PLAN.md
// placeholder, directly — mirroring ReserveDebugDir's directory-creation path
// (P-0180 locked decision #1). When slug is empty, "plan" is used as the
// default slug.
//
// scanMode is passed through to GetNextNumAtomic ("single-tree" for legacy
// single-dir, "" or "auto" for the default cross-worktree scan, P-0170).
//
// Unlike file-kind reservation there is no *.placeholder sentinel file — the
// directory (with its seeded PLAN.md) IS the reservation. The caller receives
// the full ID (e.g. "P-0181") and the absolute path of the created directory.
func ReservePlanDir(dir, slug, scanMode string) (id, dirPath string, err error) {
	if slug == "" {
		slug = "plan"
	}
	slug = sanitizeSlug(slug)
	if slug == "" {
		slug = "plan"
	}

	for attempts := 0; attempts < 10; attempts++ {
		num, _, scanErr := GetNextNumAtomic(dir, "P", scanMode)
		if scanErr != nil {
			return "", "", scanErr
		}
		fullID := fmt.Sprintf("P-%s", num)
		targetDir := filepath.Join(dir, fullID+"-"+slug)
		// Use Mkdir (not MkdirAll) so O_EXCL-equivalent semantics: fails if
		// the directory already exists.
		if mkErr := os.Mkdir(targetDir, 0o755); mkErr != nil {
			if os.IsExist(mkErr) {
				// Race: another caller created this dir — retry.
				continue
			}
			return "", "", fmt.Errorf("cannot create plan directory %s: %w", targetDir, mkErr)
		}

		seedPath := filepath.Join(targetDir, "PLAN.md")
		seed := fmt.Sprintf(planDirSeedTemplate, fullID, slug, slug)
		if writeErr := os.WriteFile(seedPath, []byte(seed), 0o644); writeErr != nil {
			return "", "", fmt.Errorf("cannot seed %s: %w", seedPath, writeErr)
		}

		return fullID, targetDir, nil
	}
	return "", "", fmt.Errorf("ReservePlanDir: exhausted retries in %s — possible concurrent storm", dir)
}
