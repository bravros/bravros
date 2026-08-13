package plan

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bravros/bravros/cli/internal/config"
)

// gitCommandTimeout is the maximum time a git subprocess may run before being
// killed. 30 seconds is generous for `git ls-tree -r` over many branches; the
// primary guard is against hung subprocesses on network-mounted repos (e.g. NFS
// or slow SMB shares) that would otherwise block `kaisser nextid reserve`
// indefinitely.
const gitCommandTimeout = 30 * time.Second

// numberedFileRe matches both old-style (0067-foo.md) and new-style
// (P-0067-foo.md / B-0067-foo.md) numbered plan/backlog/placeholder filenames,
// extracting the bare 4-digit ID in capture group 1. We intentionally do NOT
// reuse the package-level numPrefix here — that regex is anchored to ^(\d{4})
// for real plan/backlog file scanning in 5+ other places and must remain unchanged.
var numberedFileRe = regexp.MustCompile(`^(?:[A-Z]-)?(\d{4})-`)

// placeholderRe matches P-0119.placeholder / B-0042.placeholder etc.
// B-0141's sweepLegacyPlaceholders handles the old *-.placeholder pattern
// (files like "P-0042-.placeholder"); this regex handles the NEW format
// introduced in P-0116 ("P-0042.placeholder") used by ReservePlaceholder.
// Both regex patterns are kept separate so sweepLegacyPlaceholders does NOT
// accidentally sweep new-format reservations.
var placeholderRe = regexp.MustCompile(`^([A-Z])-(\d{4})\.placeholder$`)

// bareNumRe matches a bare numeric plan ID (3–4 digits, no prefix) as input from
// the user — e.g. "108" or "0108". B-0185: used in findEntityFileByID to normalize
// bare numeric input to "P-NNNN" for the filename-prefix scan.
var bareNumRe = regexp.MustCompile(`^\d{3,4}$`)

// sweepLegacyPlaceholders removes any *-.placeholder files in dir on a
// best-effort basis. The placeholder mechanism was retired in B-0141 — this
// function exists only to clean up leftover files from older binaries the
// first time the new binary runs. No TTL, no real-file check; it's a one-shot
// unconditional sweep.
func sweepLegacyPlaceholders(dir string) {
	matches, err := filepath.Glob(filepath.Join(dir, "*-.placeholder"))
	if err != nil || len(matches) == 0 {
		return
	}
	for _, ph := range matches {
		os.Remove(ph)
	}
}

// GetNextNumAtomic scans dir for the highest numbered file and returns the
// next available 4-digit ID. The signature is preserved for caller compat
// (callers in cli/cmd/plan_cmds.go ignore cleanup with `_, _, err`), but the
// returned cleanup is a no-op — the placeholder reservation mechanism was
// removed in B-0141 because it produced more complexity (TTL GC, race retry,
// stamp leakage) than it solved. The accepted theoretical race trade-off:
// two processes calling this simultaneously may both compute the same next
// number; in practice this is irrelevant because plan creation is a serial
// human-driven action.
//
// prefix is retained for signature compatibility but no longer affects file
// creation (no file is created — the caller writes the real plan file).
//
// scanMode controls the cross-worktree scan behaviour introduced in P-0170:
//   - "single-tree" — skip the cross-worktree scan (old single-directory behaviour,
//     B-0208 escape hatch).
//   - "" / "auto" (or any other value) — perform the cross-worktree scan via
//     ScanAllSources so that IDs committed on any branch or held by any active
//     worktree are counted. Falls back to the KAISSER_NEXTID_SCAN_MODE env var
//     when scanMode is empty, so external library users retain the same override
//     mechanism they had before this parameter was added.
func GetNextNumAtomic(dir, prefix, scanMode string) (string, func(), error) {
	noop := func() {}

	// Best-effort sweep of any *-.placeholder files left by older binaries.
	// Runs on every call (Glob with no matches is essentially free); this keeps
	// the directory clean without requiring a separate migration step.
	sweepLegacyPlaceholders(dir)

	// P-0170: normalise prefix to a single uppercase letter so EntityByPrefix
	// can look it up. Callers may pass "B-" (with trailing dash) or "P" (bare).
	normPrefix := strings.ToUpper(strings.TrimRight(prefix, "-"))
	if len(normPrefix) > 1 {
		normPrefix = string(normPrefix[0])
	}

	// Resolve effective scan mode: explicit parameter wins; fall back to env var
	// when the parameter is empty so that external library users (e.g. tests that
	// set KAISSER_NEXTID_SCAN_MODE=single-tree) retain the same override path.
	effectiveScanMode := scanMode
	if effectiveScanMode == "" {
		effectiveScanMode = os.Getenv("KAISSER_NEXTID_SCAN_MODE")
	}
	// "strict" = auto, but a scan error is returned instead of silently
	// degrading to the single-tree fallback. Callers that must not hand out a
	// possibly-colliding id (backlog add) use it to get the hard-fail contract
	// without paying for a second full scan just to probe. The single-tree
	// escape hatch still wins so the override remains reachable.
	if effectiveScanMode == "strict" && os.Getenv("KAISSER_NEXTID_SCAN_MODE") == "single-tree" {
		effectiveScanMode = "single-tree"
	}

	// Cross-worktree scan (auto mode). Activated unless effectiveScanMode is
	// "single-tree" (escape hatch per B-0208).
	//
	// On scan error (e.g. outside a git repo), fall through to the single-tree path
	// below. The "hard fail" contract from locked decision #5 is enforced at the
	// cmd layer (nextidReserveCmd calls ScanAllSources directly and returns a non-zero
	// error to the user). GetNextNumAtomic itself is a library function that must
	// remain callable outside git repos (used by deprecated nextidCmd, tests, etc.).
	if effectiveScanMode != "single-tree" && normPrefix != "" {
		if _, ok := EntityByPrefix(normPrefix); ok {
			highest, _, scanErr := ScanAllSources(normPrefix)
			if scanErr == nil {
				// Also scan dir directly for locally-created placeholders (e.g. a
				// concurrent call in this process already reserved one). This merges the
				// cross-worktree result with the local directory state so that the O_EXCL
				// retry loop in ReservePlaceholder can advance past a locally-created
				// placeholder not yet visible in the git tree.
				if entries, dirErr := os.ReadDir(dir); dirErr == nil {
					for _, e := range entries {
						var n int
						if m := numberedFileRe.FindStringSubmatch(e.Name()); m != nil {
							if strings.HasPrefix(e.Name(), normPrefix+"-") || (len(e.Name()) > 0 && e.Name()[0] >= '0' && e.Name()[0] <= '9') {
								n, _ = strconv.Atoi(m[1])
							}
						} else if m := placeholderRe.FindStringSubmatch(e.Name()); m != nil {
							if m[1] == normPrefix {
								n, _ = strconv.Atoi(m[2])
							}
						}
						if n > highest {
							highest = n
						}
					}
				}
				nextNum := fmt.Sprintf("%04d", highest+1)
				return nextNum, noop, nil
			}
			// Strict callers refuse the degraded id rather than accept a likely
			// collision.
			if effectiveScanMode == "strict" {
				return "", noop, fmt.Errorf(
					"cross-worktree id scan failed: %w\n"+
						"Refusing to allocate an id from a single-directory scan — it cannot see ids held\n"+
						"by other worktrees or branches and would likely collide.\n"+
						"Set KAISSER_NEXTID_SCAN_MODE=single-tree to override once the cause is understood",
					scanErr)
			}
			// scanErr != nil — fall through to single-tree below.
			//
			// That fallback is a correctness cliff, not a nicety: a single-tree
			// scan cannot see ids held by other worktrees or branches, so it
			// hands out an id somebody else already used. Outside a git repo the
			// degradation is expected and silent; INSIDE one it means the
			// cross-worktree scan actually broke, and staying quiet about it is
			// how duplicate ids get minted. Warn loudly there.
			if _, gitErr := runGitCommand("rev-parse", "--show-toplevel"); gitErr == nil {
				fmt.Fprintf(os.Stderr,
					"⚠️  kaisser: cross-worktree id scan failed (%v)\n"+
						"    Falling back to a single-directory scan, which CANNOT see ids held by\n"+
						"    other worktrees or branches — the id returned may collide.\n",
					scanErr)
			}
		}
	}

	// Single-tree fallback: original directory-scan logic (used when
	// KAISSER_NEXTID_SCAN_MODE=single-tree, outside a git repo, or for an
	// unrecognised prefix).
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", noop, fmt.Errorf("cannot read directory %s: %w", dir, err)
	}

	maxNum := 0
	for _, e := range entries {
		// Check standard numbered files (P-0119-foo.md, B-0042-bar.md, 0001-old.md)
		if m := numberedFileRe.FindStringSubmatch(e.Name()); m != nil {
			n, _ := strconv.Atoi(m[1])
			if n > maxNum {
				maxNum = n
			}
			continue
		}
		// Also count new-format placeholder reservations (P-0119.placeholder) so that
		// a reserved-but-unwritten ID is never reused. B-0116.
		if m := placeholderRe.FindStringSubmatch(e.Name()); m != nil {
			n, _ := strconv.Atoi(m[2])
			if n > maxNum {
				maxNum = n
			}
		}
	}

	nextNum := fmt.Sprintf("%04d", maxNum+1)
	return nextNum, noop, nil
}

// ReservePlaceholder atomically reserves the next available ID in dir for the
// given prefix letter (e.g. "P" for plans, "B" for backlogs). It writes a
// <prefix>-<NNNN>.placeholder sentinel file using O_CREATE|O_EXCL so that a
// concurrent call cannot receive the same ID. Returns the full ID string
// (e.g. "P-0119") and the path to the placeholder file.
//
// scanMode is passed through to GetNextNumAtomic to control cross-worktree scan
// behaviour ("single-tree" for legacy single-dir, "" or "auto" for the default
// cross-worktree scan introduced in P-0170).
//
// Design note (B-0141 lesson): placeholder files are the correct mechanism for
// atomic ID reservation — earlier removal of this pattern was wrong in
// conclusion. The new design makes consumer-side REUSE load-bearing: skills
// call ReservePlaceholder, then rename the placeholder to the final filename.
// There is NO automatic TTL/GC in v1; use ReleasePlaceholder for explicit
// cleanup on abort.
func ReservePlaceholder(dir, prefix, scanMode string) (id, path string, err error) {
	// Retry up to 10 times in case of race (another caller reserved the
	// same number between our scan and our O_EXCL create).
	for attempts := 0; attempts < 10; attempts++ {
		num, _, scanErr := GetNextNumAtomic(dir, prefix, scanMode)
		if scanErr != nil {
			return "", "", scanErr
		}
		fullID := fmt.Sprintf("%s-%s", prefix, num)
		phPath := filepath.Join(dir, fullID+".placeholder")
		f, createErr := os.OpenFile(phPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if createErr == nil {
			// Write empty JSON object so the file is valid for any consumer that reads it.
			_, _ = f.WriteString("{}\n")
			f.Close()
			return fullID, phPath, nil
		}
		// O_EXCL failure means another goroutine/process beat us — retry.
		if os.IsExist(createErr) {
			continue
		}
		return "", "", fmt.Errorf("cannot create placeholder %s: %w", phPath, createErr)
	}
	return "", "", fmt.Errorf("ReservePlaceholder: exhausted retries in %s — possible concurrent storm or stale placeholders", dir)
}

// ReleasePlaceholder deletes the placeholder file for the given full ID
// (e.g. "P-0119") in dir. Idempotent: returns nil when the file is already gone.
// Use this to release a reservation when the consumer aborts before renaming.
func ReleasePlaceholder(dir, id string) error {
	phPath := filepath.Join(dir, id+".placeholder")
	err := os.Remove(phPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ReleasePlaceholder %s: %w", phPath, err)
	}
	return nil
}

// planDirIDRe matches a P-NNNN-<slug>/ folder-plan directory name (optionally
// suffixed "-complete" on completion) and captures the numeric ID — the
// directory precedent mirrored from debugDirRe below (P-0180 locked decision
// #1/#2). Kept distinct from resolve.go's planFolderIDRe (no capture group,
// used only for the id-prefixed-*.md fallback inside ResolvePlanEntryFile).
var planDirIDRe = regexp.MustCompile(`^P-(\d{4})-`)

// debugDirRe matches a D-NNNN-<slug>-<stage>/ directory name and extracts the
// four-digit number. Used by directory-kind ID resolution and GetNextNumAtomic
// scanning (directories are now legal entries in the debug subdirectory).
var debugDirRe = regexp.MustCompile(`^D-(\d{4})-`)

// ReserveDebugDir atomically reserves the next available D-NNNN ID inside dir
// and creates the investigation directory D-NNNN-<slug>-open/ directly.
// When slug is empty, "investigation" is used as the default slug.
//
// scanMode is passed through to GetNextNumAtomic ("single-tree" for legacy
// single-dir, "" or "auto" for the default cross-worktree scan, P-0170).
//
// Unlike file-kind entities there is no placeholder file — the directory itself
// is the reservation. The caller receives the full ID (e.g. "D-0001") and the
// absolute path of the created directory.
func ReserveDebugDir(dir, slug, scanMode string) (id, dirPath string, err error) {
	if slug == "" {
		slug = "investigation"
	}
	// Sanitize slug: lowercase, collapse non-alphanumeric runs to "-", trim.
	slug = sanitizeSlug(slug)
	if slug == "" {
		slug = "investigation"
	}

	for attempts := 0; attempts < 10; attempts++ {
		num, _, scanErr := GetNextNumAtomic(dir, "D", scanMode)
		if scanErr != nil {
			return "", "", scanErr
		}
		fullID := fmt.Sprintf("D-%s", num)
		targetDir := filepath.Join(dir, fullID+"-"+slug+"-open")
		// Use Mkdir (not MkdirAll) so O_EXCL semantics: fails if already exists.
		if mkErr := os.Mkdir(targetDir, 0o755); mkErr != nil {
			if os.IsExist(mkErr) {
				// Race: another caller created this dir — retry.
				continue
			}
			return "", "", fmt.Errorf("cannot create debug directory %s: %w", targetDir, mkErr)
		}
		return fullID, targetDir, nil
	}
	return "", "", fmt.Errorf("ReserveDebugDir: exhausted retries in %s — possible concurrent storm", dir)
}

// sanitizeSlug converts an arbitrary string to a slug: lowercase, replace
// non-alphanumeric runs with "-", trim leading/trailing dashes.
func sanitizeSlug(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prev := '-'
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prev = r
		} else if prev != '-' {
			b.WriteRune('-')
			prev = '-'
		}
	}
	return strings.Trim(b.String(), "-")
}

// ReleaseDebugDir removes a debug investigation directory created by
// ReserveDebugDir. The directory is identified by the full ID (e.g. "D-0001");
// the function scans dir for any entry matching D-NNNN-* and removes it.
// Idempotent: returns nil when no matching directory is found.
func ReleaseDebugDir(dir, id string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("ReleaseDebugDir: cannot read %s: %w", dir, err)
	}
	prefix := id + "-"
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			target := filepath.Join(dir, e.Name())
			if err := os.RemoveAll(target); err != nil {
				return fmt.Errorf("ReleaseDebugDir: cannot remove %s: %w", target, err)
			}
			return nil
		}
	}
	return nil // idempotent: not found is OK
}

// FindEntityDirByID resolves a directory-kind entity ID (e.g. "D-0001") to its
// on-disk directory inside dir. The lookup scans for any subdirectory whose name
// starts with "<id>-", regardless of the stage suffix (e.g. -open, -complete).
// Returns the absolute path and nil error on success; returns an error when zero
// or multiple matches are found.
func FindEntityDirByID(dir, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("entity ID is required")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", dir, err)
	}
	prefix := id + "-"
	var matches []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			matches = append(matches, filepath.Join(dir, e.Name()))
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("debug dir %s not found in %s", id, dir)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("debug dir %s ambiguous (%d matches): %s", id, len(matches), strings.Join(matches, ", "))
	}
}

var numPrefix = regexp.MustCompile(`^(\d{4})`)
var branchTypePrefix = regexp.MustCompile(`^[^/]+/`)

// planNumFromPath extracts the bare 4-digit plan number from a plan path,
// handling both single-file plans and folder-plans (P-0180).
//
// It tries the basename first — covers single-file plans (0068-x.md /
// P-0068-x.md) and a folder path passed directly (P-0068-x/). For a folder-plan
// that has ALREADY been resolved to its canonical entry file
// (…/P-0068-x/PLAN.md), the basename is PLAN.md and carries no id, so it falls
// back to the PARENT directory's basename where the id actually lives. This is
// the production path: FindPlanFile hands consumers the resolved entry file,
// not the directory.
func planNumFromPath(path string) string {
	for _, seg := range []string{filepath.Base(path), filepath.Base(filepath.Dir(path))} {
		if m := numPrefix.FindStringSubmatch(seg); m != nil {
			return m[1]
		}
		if m := numberedFileRe.FindStringSubmatch(seg); m != nil {
			return m[1]
		}
	}
	return ""
}

// MetaResult is the JSON output of the meta command.
type MetaResult struct {
	NextNum          string                        `json:"next_num"`
	BacklogNext      string                        `json:"backlog_next,omitempty"`
	BaseBranch       string                        `json:"base_branch"`
	Branch           string                        `json:"branch"`
	PlanFile         string                        `json:"plan_file"`
	PlanNum          string                        `json:"plan_num"`
	PR               string                        `json:"pr,omitempty"`
	Status           string                        `json:"status"`
	Progress         string                        `json:"progress"`
	Project          string                        `json:"project"`
	GitRemote        string                        `json:"git_remote"`
	Today            string                        `json:"today"`
	PrimaryWorktree  string                        `json:"primary_worktree,omitempty"`
	LastReviewCommit string                        `json:"last_review_commit,omitempty"`
	Stack            config.StackConfig            `json:"stack,omitempty"`
	Stacks           map[string]config.StackConfig `json:"stacks,omitempty"`
	Git              config.GitConfig              `json:"git,omitempty"`
	Monorepo         bool                          `json:"monorepo,omitempty"`
}

// GetNextNum scans .planning/ for the highest numbered plan file and returns next.
func GetNextNum(planningDir string) string {
	entries, err := os.ReadDir(planningDir)
	if err != nil {
		return "0001"
	}

	maxNum := 0
	for _, e := range entries {
		m := numberedFileRe.FindStringSubmatch(e.Name())
		if m != nil {
			n, _ := strconv.Atoi(m[1])
			if n > maxNum {
				maxNum = n
			}
		}
	}

	if maxNum == 0 {
		return "0001"
	}
	return fmt.Sprintf("%04d", maxNum+1)
}

// FindPlanFile finds the active plan file matching the branch, or falls back to most recent.
//
// P-0180 locked decision #1/#2/#4: a plan may also be a P-NNNN-<slug>/ folder
// alongside the legacy single-file shape. Folder-plan directories are treated
// as active candidates the same way *-todo.md/*-approved.md/etc. files are; a
// directory suffixed "-complete" is terminal-stage and excluded from the
// active pool, mirroring the existing *-complete.md exclusion. The RETURNED
// path for a folder-plan candidate is always its resolved entry file (e.g.
// PLAN.md) via ResolvePlanEntryFile — the one shared resolver every plan
// consumer routes through — never the bare directory path.
func FindPlanFile(planningDir, branch string) string {
	entries, err := os.ReadDir(planningDir)
	if err != nil {
		return ""
	}

	// activeFiles: plans in active stages (not yet complete or cancelled).
	// This pool is preferred over allPlanFiles so that a plan file renamed from
	// *-todo.md to *-approved.md (via `kaisser plan advance`) is still found by
	// FindPlanFile without falling through to the unfiltered allPlanFiles fallback.
	var activeFiles []string
	var allPlanFiles []string
	// candidateEntry maps a candidate name (file or plan-folder dir name) to the
	// path used for content reads / the final return value. For plain files
	// this is the same path; for a folder-plan directory it is the resolved
	// entry file.
	candidateEntry := make(map[string]string)

	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() {
			// Folder-plan directory (P-0180). Only P-NNNN-<slug>/ dirs
			// participate. A "-complete" dir is terminal-stage — excluded
			// from activeFiles but still added to allPlanFiles, mirroring
			// how *-complete.md files are handled below (fallback-only).
			if !planFolderIDRe.MatchString(name) {
				continue
			}
			entry := ResolvePlanEntryFile(filepath.Join(planningDir, name))
			if entry == "" {
				continue
			}
			candidateEntry[name] = entry
			if !strings.HasSuffix(name, "-complete") {
				activeFiles = append(activeFiles, name)
			}
			allPlanFiles = append(allPlanFiles, name)
			continue
		}
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		candidateEntry[name] = filepath.Join(planningDir, name)
		if strings.HasSuffix(name, "-todo.md") ||
			strings.HasSuffix(name, "-reviewed.md") ||
			strings.HasSuffix(name, "-approved.md") ||
			strings.HasSuffix(name, "-blocked.md") ||
			strings.HasSuffix(name, "-on-hold.md") {
			activeFiles = append(activeFiles, name)
		}
		if numPrefix.MatchString(name) || numberedFileRe.MatchString(name) {
			allPlanFiles = append(allPlanFiles, name)
		}
	}

	candidates := activeFiles
	if len(candidates) == 0 {
		candidates = allPlanFiles
	}
	if len(candidates) == 0 {
		return ""
	}

	// Priority 1: frontmatter branch: exact match
	if branch != "" {
		for _, f := range candidates {
			if fm := readFrontmatterBranch(candidateEntry[f]); fm == branch {
				return candidateEntry[f]
			}
		}
	}

	// Extract suffix after type prefix (feat/fix-name → fix-name)
	branchSuffix := branchTypePrefix.ReplaceAllString(branch, "")

	// Sort descending (newest first)
	sort.Sort(sort.Reverse(sort.StringSlice(candidates)))

	// Priority 2: match branch suffix against filename
	for _, f := range candidates {
		fSlug := regexp.MustCompile(`^\d{4}-[a-z]+-`).ReplaceAllString(f, "")
		// Strip all known stage suffixes so the slug comparison is stage-agnostic.
		for _, suffix := range []string{"-todo.md", "-reviewed.md", "-approved.md", "-blocked.md", "-on-hold.md", "-completed.md", "-cancelled.md", "-complete.md"} {
			fSlug = strings.TrimSuffix(fSlug, suffix)
		}

		if fSlug != "" && branchSuffix != "" &&
			(strings.Contains(fSlug, branchSuffix) || strings.Contains(branchSuffix, fSlug)) {
			return candidateEntry[f]
		}
	}

	// Fallback: return empty string when branch is clearly unrelated to all plans.
	// Only fall back to a file if branch is empty (no branch context available).
	if branch != "" {
		return ""
	}

	// No branch context — return highest-numbered candidate (candidates already sorted descending).
	if len(candidates) > 0 {
		return candidateEntry[candidates[0]]
	}
	return ""
}

// ReadFrontmatterField is the exported counterpart of readFrontmatterField for
// use by the cmd layer. See readFrontmatterField for behaviour.
func ReadFrontmatterField(path, field string) string {
	return readFrontmatterField(path, field)
}

// findEntityFileByID resolves an entity ID (with optional prefix like "P-" or "B-")
// to an on-disk file inside dir. The lookup tries:
//  1. Exact match against the `id:` frontmatter field across every *.md file.
//  2. Filename pattern match — any *.md whose basename starts with `<id>-`.
//
// prefix is used only in error messages to identify the entity type.
func findEntityFileByID(prefix, dir, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("%sid is required", prefix)
	}

	// B-0185: normalize bare numeric input (e.g. "0108" or "108") to "P-0108" so that
	// kaisser plan advance accepts bare numbers at any pipeline stage.
	// The P- prefix is used for plan IDs; backlog/report use separate lookup paths.
	// Priority-1 frontmatter match still tries the original id verbatim (legacy plans
	// may have a bare numeric id: field), so we only inject the normalized form for
	// the priority-2 prefix scan below.
	normalizedID := id
	if bareNumRe.MatchString(id) {
		// bareNumRe matches exactly 3–4 digits, so we always zero-pad to 4 with P- prefix.
		normalizedID = fmt.Sprintf("P-%04s", id)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", dir, err)
	}

	// Priority 1: frontmatter id match (try original id — handles legacy bare-num ids).
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if v := readFrontmatterField(path, "id"); v == id {
			return path, nil
		}
	}

	// Priority 2: filename prefix match (`<id>-...` OR `<normalizedID>-...`).
	// Both prefixes are checked so "0108" matches "P-0108-foo-reviewed.md".
	var matches []string
	seen := make(map[string]bool)
	filePrefix := id + "-"
	normalizedPrefix := normalizedID + "-"
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		fullPath := filepath.Join(dir, e.Name())
		if seen[fullPath] {
			continue
		}
		if strings.HasPrefix(e.Name(), filePrefix) || strings.HasPrefix(e.Name(), normalizedPrefix) {
			matches = append(matches, fullPath)
			seen[fullPath] = true
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("%s%s not found", prefix, id)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("%s%s ambiguous (%d matches): %s", prefix, id, len(matches), strings.Join(matches, ", "))
	}
}

// FindPlanFileByID resolves a plan ID to an on-disk file inside planningDir.
func FindPlanFileByID(planningDir, id string) (string, error) {
	return findEntityFileByID("plan ", planningDir, id)
}

// readFrontmatterField extracts the YAML frontmatter value for the given field
// name, scanning the FULL frontmatter block — from the opening `---` on line 1
// through the closing `---` — with no line cap. The former 20-line cap made a
// `pr:` field past line 20 invisible, silently degrading finish resolution to
// heuristics (P-0185). Returns "" if the file has no frontmatter, the field is
// absent, or on any error.
func readFrontmatterField(path, field string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	prefix := field + ":"
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() || strings.TrimRight(scanner.Text(), "\r") != "---" {
		return ""
	}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimRight(line, "\r") == "---" {
			break
		}
		if strings.HasPrefix(line, prefix) {
			val := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			// Strip optional surrounding quotes
			val = strings.Trim(val, `"'`)
			return val
		}
	}
	return ""
}

// readFrontmatterBranch reads the `branch:` field from YAML frontmatter.
func readFrontmatterBranch(path string) string {
	return readFrontmatterField(path, "branch")
}

// PlanHeader holds parsed header fields from a plan file.
type PlanHeader struct {
	PlanNum  string
	Status   string
	Progress string
}

// ParsePlanHeader reads the first 30 lines of a plan file for status/progress.
// Accepts either a single-file plan path or a P-NNNN-<slug>/ folder-plan
// directory (P-0180 locked decision #1/#2/#4) — the folder is resolved to its
// canonical entry file via ResolvePlanEntryFile before scanning.
func ParsePlanHeader(planFile string) PlanHeader {
	result := PlanHeader{}
	if planFile == "" {
		return result
	}

	// Extract the plan number. planNumFromPath handles single-file plans, a
	// folder passed directly, AND a folder-plan already resolved to its entry
	// file (…/P-NNNN-slug/PLAN.md — id comes from the parent dir). P-0180.
	result.PlanNum = planNumFromPath(planFile)

	// Resolve a folder-plan to its canonical entry file (the one shared
	// resolver every plan consumer routes through). Single-file plans pass
	// through unchanged.
	entryFile := ResolvePlanEntryFile(planFile)
	if entryFile == "" {
		return result
	}

	f, err := os.Open(entryFile)
	if err != nil {
		return result
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() && lineNum < 30 {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "> **Status:**") {
			result.Status = strings.TrimSpace(strings.TrimPrefix(line, "> **Status:**"))
		} else if strings.HasPrefix(line, "> **Progress:**") {
			result.Progress = strings.TrimSpace(strings.TrimPrefix(line, "> **Progress:**"))
		}
		lineNum++
	}

	return result
}

// reportPrefix matches R-NNNN or U-NNNN prefix patterns
var reportPrefix = regexp.MustCompile(`^([RU])-(\d{4})`)

// GetNextReportNum scans a directory for R-NNNN or U-NNNN prefixed files and returns next.
// Unlike GetNextNumAtomic, this does NOT use atomic file reservation — reports are created
// interactively (one at a time by user request), so no concurrency risk.
func GetNextReportNum(dir, prefix string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return prefix + "-0001"
	}

	maxNum := 0
	for _, e := range entries {
		m := reportPrefix.FindStringSubmatch(e.Name())
		if m != nil && m[1] == prefix {
			n, _ := strconv.Atoi(m[2])
			if n > maxNum {
				maxNum = n
			}
		}
	}

	return fmt.Sprintf("%s-%04d", prefix, maxNum+1)
}

// MetaJSON returns the meta result as indented JSON string.
func (m *MetaResult) JSON() string {
	b, _ := json.MarshalIndent(m, "", "  ")
	return string(b)
}

// ReadPRField reads the `pr:` frontmatter field from the given plan file.
// Returns empty string if unset, null, or file not found.
func ReadPRField(planFile string) string {
	if planFile == "" {
		return ""
	}
	val := readFrontmatterField(planFile, "pr")
	if val == "null" || val == "" {
		return ""
	}
	return val
}

// PrimaryWorktreePath parses `git worktree list --porcelain` and returns the path of the
// first (primary) worktree — the one that holds the main/master branch.
// Falls back to running git subprocess. Returns "" on any error.
func PrimaryWorktreePath() string {
	out, err := runGitCommand("worktree", "list", "--porcelain")
	if err != nil {
		return ""
	}
	// The first "worktree <path>" line in the output is always the primary worktree.
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			return strings.TrimPrefix(line, "worktree ")
		}
	}
	return ""
}

// LastReviewCommit returns the hash of the most recent commit whose message contains
// "plan: review <planNum>". Falls back to any "plan: review" commit when planNum is empty.
// Returns "" when no matching commit is found.
func LastReviewCommit(planNum string) string {
	var hash string
	if planNum != "" {
		// Try exact plan-scoped grep first
		hash = runGitLogGrep("plan: review " + planNum)
		if hash == "" {
			// Fallback: any "plan: review" commit, then confirm its message mentions planNum
			hash = runGitLogGrep("plan: review")
			if hash != "" {
				msg := runGitCommitMessage(hash)
				if !strings.Contains(msg, planNum) {
					hash = ""
				}
			}
		}
	} else {
		hash = runGitLogGrep("plan: review")
	}
	return strings.TrimSpace(hash)
}

// ResolveGitRoot returns the absolute path of the git repository root by
// running `git rev-parse --show-toplevel` in the current working directory.
//
// If the caller is not inside a git repository, or git is not installed,
// it falls back to the current working directory (preserving prior behaviour).
// A warning is printed to stderr only when the fallback triggers.
func ResolveGitRoot() string {
	root, err := runGitCommand("rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		// Not a git repo or git unavailable — fall back to cwd gracefully.
		cwd, cwdErr := os.Getwd()
		if cwdErr == nil {
			fmt.Fprintf(os.Stderr, "warning: kaisser nextid: not in a git repo; using cwd for .planning/ resolution\n")
			return cwd
		}
		return "."
	}
	return root
}

// ScanSource describes one source to scan when computing the highest existing
// plan/backlog/etc. ID. Used by ResolveScanRoots and consumed by ScanAllSources
// (Phase 2).
//
// Kind values:
//   - "worktree-fs" — an active worktree whose .planning/ directory is accessible
//     on the filesystem. Path is the absolute worktree root.
//   - "branch-tree" — a git branch (local or remote tracking ref) whose .planning/
//     content is accessible via git-ls-tree. Path is empty; Branch holds the ref.
type ScanSource struct {
	Kind   string // "worktree-fs" or "branch-tree"
	Path   string // absolute FS path (Kind="worktree-fs") or "" (Kind="branch-tree")
	Branch string // git ref / branch name (Kind="branch-tree") or "" (Kind="worktree-fs")
}

// ResolveScanRoots returns the full set of sources that should be consulted when
// computing the highest existing ID across all in-flight work. It combines two legs:
//
//  1. Filesystem leg: every active worktree returned by `git worktree list --porcelain`
//     contributes one ScanSource{Kind:"worktree-fs", Path:<worktree-root>}.
//  2. Git-tree leg: every branch returned by
//     `git for-each-ref refs/heads/* refs/remotes/origin/*` contributes one
//     ScanSource{Kind:"branch-tree", Branch:<refname>}.
//
// Both legs together form the union scan used by ScanAllSources (Phase 2) to find
// the true maximum ID across all active work.
//
// Errors from either git subprocess propagate — the caller hard-fails per locked
// decision #5 in the plan. Individual git invocation failures are wrapped with a
// descriptive message identifying which command failed.
func ResolveScanRoots() ([]ScanSource, error) {
	var sources []ScanSource

	// ── Leg 1: filesystem worktrees ──────────────────────────────────────────
	wtOut, err := runGitCommand("worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("ResolveScanRoots: git worktree list failed: %w", err)
	}
	for _, line := range strings.Split(wtOut, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			wtPath := strings.TrimPrefix(line, "worktree ")
			if wtPath != "" {
				sources = append(sources, ScanSource{Kind: "worktree-fs", Path: wtPath})
			}
		}
	}

	// ── Leg 2: git-tree branches (local + origin tracking refs) ─────────────
	refOut, err := runGitCommand("for-each-ref", "--format=%(refname:short)", "refs/heads/*", "refs/remotes/origin/*")
	if err != nil {
		return nil, fmt.Errorf("ResolveScanRoots: git for-each-ref failed: %w", err)
	}
	seen := make(map[string]bool)
	for _, ref := range strings.Split(refOut, "\n") {
		ref = strings.TrimSpace(ref)
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		sources = append(sources, ScanSource{Kind: "branch-tree", Branch: ref})
	}

	return sources, nil
}

// ResolveWriteRoot returns the absolute path of the current checkout's git
// toplevel — the directory where the calling shell lives. This is always the
// calling worktree's own root, whether that is the primary worktree or a linked
// one.
//
// Use ResolveWriteRoot for write operations (placeholder creation, new plan files)
// so that the placeholder lands in the calling checkout's .planning/ rather than
// always in the primary's. This supersedes the old B-0208 redirect for write paths.
//
// For ID collision prevention (read side), use ResolveScanRoots instead.
func ResolveWriteRoot() string {
	return ResolveGitRoot()
}

// runGitCommand is a minimal subprocess helper used by plan-level meta helpers.
// Returns (stdout, error). Stderr is discarded.
//
// A 30-second timeout (gitCommandTimeout) is applied via context.WithTimeout to
// prevent hung git subprocesses from blocking `kaisser nextid reserve` indefinitely
// on network-mounted repos or when git is waiting for credential input.
func runGitCommand(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// runGitLogGrep runs `git log --all --grep=<pattern> --format=%H -n 1` and returns the hash.
func runGitLogGrep(pattern string) string {
	out, err := runGitCommand("log", "--all", "--grep="+pattern, "--format=%H", "-n", "1")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// runGitCommitMessage returns the subject line of a commit by hash.
func runGitCommitMessage(hash string) string {
	if hash == "" {
		return ""
	}
	out, err := runGitCommand("log", "-n", "1", "--format=%s", hash)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
