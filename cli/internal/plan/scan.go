package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ─── Audit types ──────────────────────────────────────────────────────────────

// CollisionSource describes one occurrence of an ID within a specific scan source.
type CollisionSource struct {
	// Kind is the source type: "worktree-fs" or "branch-tree".
	Kind string
	// Location is the filesystem path (worktree-fs) or branch ref (branch-tree).
	Location string
	// ContentHash is the SHA256 hex hash of the file bytes (worktree-fs) or the
	// git blob SHA from git ls-tree (branch-tree). Placeholder files hash to the
	// sentinel value placeholderSentinelHash so placeholder-vs-real collisions are
	// always detected as divergent.
	ContentHash string
}

// IDCollision represents a (prefix, ID) pair that appears in ≥2 distinct sources
// with ≥2 distinct content hashes — indicating a true divergent duplicate.
type IDCollision struct {
	// Prefix is the entity prefix letter (e.g. "P", "B").
	Prefix string
	// ID is the numeric ID that collides.
	ID int
	// Sources lists all occurrences with their content hashes.
	Sources []CollisionSource
}

// placeholderSentinelHash is a synthetic content hash assigned to every
// .placeholder file so a placeholder-vs-real collision still appears as divergent.
// It is NOT a valid SHA256 of any real file content.
const placeholderSentinelHash = "00000000placeholder00000000000000000000000000000000000000000000"

// ─── Audit public API ─────────────────────────────────────────────────────────

// AuditDuplicateIDs scans all sources for the given entity prefix and returns
// every (prefix, ID) pair that appears in ≥2 distinct sources with ≥2 distinct
// content hashes. Results are sorted by ID ascending.
//
// The branch-tree leg uses the blob SHA from "git ls-tree -r <branch>" as the
// content hash (no file read needed). The worktree-fs leg sha256-hashes the
// actual file bytes. Placeholder files (*.placeholder) receive the sentinel hash.
//
// The sources slice returned is the full list of ScanSources consulted (same as
// returned by ResolveScanRoots), for caller diagnostics.
func AuditDuplicateIDs(prefix string) ([]IDCollision, []ScanSource, error) {
	entity, ok := EntityByPrefix(strings.ToUpper(prefix))
	if !ok {
		return nil, nil, fmt.Errorf("AuditDuplicateIDs: unknown entity prefix %q", prefix)
	}

	sources, err := ResolveScanRoots()
	if err != nil {
		return nil, nil, fmt.Errorf("AuditDuplicateIDs: %w", err)
	}

	collisions, err := auditDuplicateIDsForEntity(entity, sources)
	return collisions, sources, err
}

// AuditAllEntities scans all sources for every registered entity and returns
// the concatenated set of collisions sorted by (prefix, ID). The sources slice
// returned is the result of one ResolveScanRoots call shared across all entities.
func AuditAllEntities() ([]IDCollision, []ScanSource, error) {
	sources, err := ResolveScanRoots()
	if err != nil {
		return nil, nil, fmt.Errorf("AuditAllEntities: %w", err)
	}

	var all []IDCollision
	for _, entity := range AllEntities {
		collisions, auditErr := auditDuplicateIDsForEntity(entity, sources)
		if auditErr != nil {
			return nil, sources, fmt.Errorf("AuditAllEntities [%s]: %w", entity.Prefix, auditErr)
		}
		all = append(all, collisions...)
	}

	// Sort by prefix then by ID.
	sort.Slice(all, func(i, j int) bool {
		if all[i].Prefix != all[j].Prefix {
			return all[i].Prefix < all[j].Prefix
		}
		return all[i].ID < all[j].ID
	})
	return all, sources, nil
}

// ─── Audit internal implementation ───────────────────────────────────────────

// auditDuplicateIDsForEntity is the inner implementation: given an entity and a
// resolved list of scan sources, build an ID→occurrences map and emit IDCollision
// entries for every ID with ≥2 sources AND ≥2 distinct content hashes.
func auditDuplicateIDsForEntity(entity EntityDef, sources []ScanSource) ([]IDCollision, error) {
	// idEntries maps numeric ID → accumulated CollisionSources across all sources.
	idEntries := make(map[int][]CollisionSource)

	for _, src := range sources {
		var idHashPairs map[int]string
		var err error

		switch src.Kind {
		case "worktree-fs":
			idHashPairs, err = auditWorktreeFSPairs(src.Path, entity)
			if err != nil {
				return nil, fmt.Errorf("auditDuplicateIDsForEntity [worktree-fs %s]: %w", src.Path, err)
			}
			for id, hash := range idHashPairs {
				idEntries[id] = append(idEntries[id], CollisionSource{
					Kind:        "worktree-fs",
					Location:    src.Path,
					ContentHash: hash,
				})
			}
		case "branch-tree":
			idHashPairs, err = auditBranchTreePairs(src.Branch, entity)
			if err != nil {
				return nil, fmt.Errorf("auditDuplicateIDsForEntity [branch-tree %s]: %w", src.Branch, err)
			}
			for id, hash := range idHashPairs {
				idEntries[id] = append(idEntries[id], CollisionSource{
					Kind:        "branch-tree",
					Location:    src.Branch,
					ContentHash: hash,
				})
			}
		}
	}

	var collisions []IDCollision
	for id, occurrences := range idEntries {
		if len(occurrences) < 2 {
			continue
		}
		// A divergent collision requires ≥2 distinct content hashes.
		hashSet := make(map[string]bool)
		for _, occ := range occurrences {
			hashSet[occ.ContentHash] = true
		}
		if len(hashSet) < 2 {
			// Same content across all sources (e.g. the same committed file visible via
			// both the worktree-fs leg and the branch-tree leg) — not a collision.
			continue
		}
		collisions = append(collisions, IDCollision{
			Prefix:  entity.Prefix,
			ID:      id,
			Sources: occurrences,
		})
	}

	sort.Slice(collisions, func(i, j int) bool {
		return collisions[i].ID < collisions[j].ID
	})
	return collisions, nil
}

// auditWorktreeFSPairs returns a map of numeric ID → content hash for all
// planning files (or directories) matching the given entity under worktreeRoot.
//
// To make hashes comparable with the branch-tree leg (which uses git blob SHAs),
// this function uses "git hash-object <file>" to compute the git blob SHA for each
// real file. This means the same file committed on a branch and present in a worktree
// will yield the same hash, and will NOT be flagged as a collision.
//
// Placeholder files receive the sentinel hash.
// Directory-kind entities receive the sentinel hash (no stable file to hash).
func auditWorktreeFSPairs(worktreeRoot string, entity EntityDef) (map[int]string, error) {
	planningRoot := filepath.Join(worktreeRoot, ".planning")
	entityDir := entity.AbsDir(planningRoot)

	entries, err := os.ReadDir(entityDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("auditWorktreeFSPairs: cannot read %s: %w", entityDir, err)
	}

	result := make(map[int]string)
	for _, e := range entries {
		name := e.Name()
		id := extractIDFromName(name, entity)
		if id == 0 {
			continue
		}

		var hash string
		switch {
		case entity.Kind == EntityKindDirectory:
			// Directory-kind: use sentinel (no stable file to hash).
			hash = placeholderSentinelHash
		case e.IsDir():
			// Dual-kind entity (the plan entity, P-0180) whose Kind is
			// EntityKindFile but which ALSO accepts `P-NNNN-<slug>/` folder
			// plans — and, in older trees, bare `NNNN-<slug>/` folders. Those
			// reach this switch as directories; hashing one as a blob fails with
			// "is a directory" and aborted the ENTIRE audit, so `nextid audit`
			// could never run in any repo containing a folder plan.
			hash = placeholderSentinelHash
		case strings.HasSuffix(name, ".placeholder"):
			hash = placeholderSentinelHash
		default:
			// Use git hash-object to compute the blob SHA so the result is directly
			// comparable with the branch-tree leg's blob SHAs.
			fullPath := filepath.Join(entityDir, name)
			blobSHA, hashErr := gitHashObject(fullPath)
			if hashErr != nil {
				// Fallback: use sha256 so we still emit a hash (best effort).
				h, sha256Err := hashFile(fullPath)
				if sha256Err != nil {
					return nil, fmt.Errorf("auditWorktreeFSPairs: hash %s: %w", fullPath, sha256Err)
				}
				hash = "sha256:" + h
			} else {
				hash = blobSHA
			}
		}
		result[id] = hash
	}
	return result, nil
}

// gitHashObject runs "git hash-object <path>" and returns the blob SHA.
func gitHashObject(path string) (string, error) {
	out, err := runGitCommand("hash-object", path)
	if err != nil {
		return "", fmt.Errorf("git hash-object %s: %w", path, err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", fmt.Errorf("git hash-object %s: empty output", path)
	}
	return out, nil
}

// auditBranchTreePairs returns a map of numeric ID → git blob SHA for all
// planning files matching the given entity on the specified branch.
// Uses "git ls-tree -r --format=%(objectname)\t%(path)" to get blob SHA + path.
// Placeholder files are assigned the sentinel hash.
func auditBranchTreePairs(branch string, entity EntityDef) (map[int]string, error) {
	var treePathArg string
	if entity.Dir == "" {
		treePathArg = ".planning/"
	} else {
		treePathArg = ".planning/" + entity.Dir + "/"
	}

	// --format=%(objectname)\t%(path) emits "<blobSHA>\t<full/path>" per entry.
	out, err := runGitCommand("ls-tree", "-r", "--format=%(objectname)\t%(path)", branch, "--", treePathArg)
	if err != nil {
		return nil, fmt.Errorf("auditBranchTreePairs: git ls-tree %s -- %s: %w", branch, treePathArg, err)
	}

	result := make(map[int]string)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		blobSHA := parts[0]
		base := filepath.Base(parts[1])

		// Key on the path segment under the entity dir, not the basename. A
		// folder plan's recursive listing yields `.planning/P-0011-slug/PLAN.md`,
		// whose basename carries no id — keying on it dropped every folder plan
		// from the audit's branch leg, so a folder-plan id colliding with an
		// entry on another branch went unreported.
		id := idFromEntityRelPath(parts[1], entity, treePathArg)
		if id == 0 {
			continue
		}

		// A folder plan contributes MANY blobs (PLAN.md, TASKLIST.md, …), so no
		// single blob SHA identifies it and "last one wins" would be arbitrary.
		// Use the same sentinel auditWorktreeFSPairs assigns to directory
		// entries: the two legs then agree for directories, and a folder-vs-file
		// collision on the same id still shows up as divergent.
		if strings.Contains(strings.TrimPrefix(parts[1], treePathArg), "/") {
			blobSHA = placeholderSentinelHash
		}

		// Placeholder files on a branch get the sentinel so a placeholder-vs-real
		// collision across two sources is always flagged as divergent.
		if strings.HasSuffix(base, ".placeholder") {
			blobSHA = placeholderSentinelHash
		}
		result[id] = blobSHA
	}
	return result, nil
}

// hashFile computes the SHA256 hex digest of the file at path.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ─── ScanAllSources (original highest-ID scanner) ────────────────────────────

// ScanAllSources scans every ScanSource returned by ResolveScanRoots and returns
// the highest existing ID number for the given entity prefix (e.g. "P", "B", "R",
// "U", "D"). It also returns the list of sources consulted so that callers can
// surface diagnostic information (e.g. via --verbose).
//
// Two scan legs run for each source kind:
//
//   - "worktree-fs": walk the entity's subdirectory under <worktree>/.planning/,
//     counting both real plan/backlog files and uncommitted placeholder files.
//   - "branch-tree": run `git ls-tree -r <branch> -- .planning/` and parse the
//     output lines to extract IDs from committed filenames.
//
// Locked decision #5 (P-0170 interview): any git subprocess error propagates up.
// The caller is responsible for hard-failing or retrying with --scan-mode single-tree.
func ScanAllSources(prefix string) (highest int, sources []ScanSource, err error) {
	// Resolve entity definition for the given prefix.
	entity, ok := EntityByPrefix(strings.ToUpper(prefix))
	if !ok {
		return 0, nil, fmt.Errorf("ScanAllSources: unknown entity prefix %q", prefix)
	}

	sources, err = ResolveScanRoots()
	if err != nil {
		return 0, nil, fmt.Errorf("ScanAllSources: %w", err)
	}

	maxID := 0

	for _, src := range sources {
		switch src.Kind {
		case "worktree-fs":
			n, scanErr := scanWorktreeFS(src.Path, entity)
			if scanErr != nil {
				return 0, sources, fmt.Errorf("ScanAllSources [worktree-fs %s]: %w", src.Path, scanErr)
			}
			if n > maxID {
				maxID = n
			}

		case "branch-tree":
			n, scanErr := scanBranchTree(src.Branch, entity)
			if scanErr != nil {
				return 0, sources, fmt.Errorf("ScanAllSources [branch-tree %s]: %w", src.Branch, scanErr)
			}
			if n > maxID {
				maxID = n
			}
		}
	}

	// History leg: ids that were allocated and later deleted are absent from
	// every current tree above, so without this the number is silently reissued.
	// One `git log --all` regardless of how many refs and worktrees exist.
	histID, histErr := scanHistory(entity)
	if histErr != nil {
		return 0, sources, fmt.Errorf("ScanAllSources [history]: %w", histErr)
	}
	if histID > maxID {
		maxID = histID
	}
	sources = append(sources, ScanSource{Kind: "history", Branch: "--all"})

	return maxID, sources, nil
}

// scanWorktreeFS walks the entity's planning subdirectory under worktreeRoot and
// returns the highest ID number found. It counts real files (matching
// numberedFileRe / placeholderRe), debug directories (matching scoutDirRe),
// and — for the dual-kind plan entity (P-0180) — folder-plan directories
// (matching planDirIDRe) via extractIDFromName.
// Returns 0 when the directory does not exist (no IDs allocated yet).
func scanWorktreeFS(worktreeRoot string, entity EntityDef) (int, error) {
	planningRoot := filepath.Join(worktreeRoot, ".planning")
	entityDir := entity.AbsDir(planningRoot)

	entries, err := os.ReadDir(entityDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("scanWorktreeFS: cannot read %s: %w", entityDir, err)
	}

	maxNum := 0
	for _, e := range entries {
		n := extractIDFromName(e.Name(), entity)
		if n > maxNum {
			maxNum = n
		}
	}
	return maxNum, nil
}

// scanBranchTree runs `git ls-tree -r <branch> -- .planning/` and parses the
// output to extract the highest existing ID for the given entity.
// Returns 0 when .planning/ does not exist on that branch.
// Any git subprocess error propagates (locked decision #5).
func scanBranchTree(branch string, entity EntityDef) (int, error) {
	// Construct the subtree path argument: ".planning/" or ".planning/backlog/" etc.
	var treePathArg string
	if entity.Dir == "" {
		treePathArg = ".planning/"
	} else {
		treePathArg = ".planning/" + entity.Dir + "/"
	}

	out, err := runGitCommand("ls-tree", "-r", "--name-only", branch, "--", treePathArg)
	if err != nil {
		return 0, fmt.Errorf("scanBranchTree: git ls-tree -r %s -- %s: %w", branch, treePathArg, err)
	}

	maxNum := 0
	for _, line := range strings.Split(out, "\n") {
		n := idFromEntityRelPath(line, entity, treePathArg)
		if n > maxNum {
			maxNum = n
		}
	}
	return maxNum, nil
}

// idFromEntityRelPath extracts the entity ID from one `git ls-tree` / `git log
// --name-only` path line.
//
// It keys on the path SEGMENT directly under the entity directory, not on
// filepath.Base. For a folder plan the recursive listing yields
// `.planning/P-0011-slug/PLAN.md`, whose basename is `PLAN.md` — carrying no id
// at all. Using the basename therefore made every folder-plan id invisible to
// the branch leg, so a folder plan living only on another branch could not
// block its own id from being reissued.
func idFromEntityRelPath(line string, entity EntityDef, treePathArg string) int {
	line = strings.TrimSpace(line)
	if line == "" {
		return 0
	}
	rel := strings.TrimPrefix(line, treePathArg)
	if rel == line {
		// Not under this entity's directory (recursive output can cross into
		// sibling entity subdirs) — fall back to the basename so a plain file
		// listing still resolves.
		rel = filepath.Base(line)
	}
	// First segment: the file itself, or the folder-plan / debug directory.
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		rel = rel[:i]
	}
	return extractIDFromName(rel, entity)
}

// scanHistory returns the highest entity ID EVER allocated across all refs,
// including ids whose files have since been deleted.
//
// Every other leg reads a current tree — `ls-tree` on a branch, or the worktree
// filesystem — so an id that was created and later deleted becomes invisible and
// is handed straight back out. That is not theoretical: a backlog item was closed
// and deleted in one session, and the next `backlog add` reissued its number
// while a commit message still referenced the original meaning.
//
// Ids must be monotonic: allocation uses a high-water mark that never regresses,
// so deleting an item leaves a permanent gap rather than freeing the number.
// Cost is a single `git log --all` (~0.9s on a repo with 350+ planning entries).
func scanHistory(entity EntityDef) (int, error) {
	var treePathArg string
	if entity.Dir == "" {
		treePathArg = ".planning/"
	} else {
		treePathArg = ".planning/" + entity.Dir + "/"
	}

	out, err := runGitCommand("log", "--all", "--diff-filter=A", "--name-only",
		"--format=", "--", treePathArg)
	if err != nil {
		return 0, fmt.Errorf("scanHistory: git log --all -- %s: %w", treePathArg, err)
	}

	maxNum := 0
	for _, line := range strings.Split(out, "\n") {
		n := idFromEntityRelPath(line, entity, treePathArg)
		if n > maxNum {
			maxNum = n
		}
	}
	return maxNum, nil
}

// ScanHistoryForDiag is an exported thin wrapper around scanHistory for
// cmd-layer callers that need per-source diagnostic information.
func ScanHistoryForDiag(entity EntityDef) (int, error) {
	return scanHistory(entity)
}

// ScanWorktreeFSForDiag is an exported thin wrapper around scanWorktreeFS for
// use by cmd-layer callers that need per-source diagnostic information.
// Returns (highest, error). The entity must be a known EntityDef.
func ScanWorktreeFSForDiag(worktreeRoot string, entity EntityDef) (int, error) {
	return scanWorktreeFS(worktreeRoot, entity)
}

// ScanBranchTreeForDiag is an exported thin wrapper around scanBranchTree for
// use by cmd-layer callers that need per-source diagnostic information.
// Returns (highest, error).
func ScanBranchTreeForDiag(branch string, entity EntityDef) (int, error) {
	return scanBranchTree(branch, entity)
}

// extractIDFromName returns the numeric ID encoded in a planning filename or
// directory name. Returns 0 when the name does not match any known pattern or
// does not belong to the given entity (prefix mismatch is silently skipped so
// that recursive git ls-tree output crossing entity subdirectories is harmless).
//
// Patterns handled:
//   - "<Prefix>-NNNN-*" real file (P-0042-foo-todo.md)  → 42
//   - "NNNN-*" bare legacy file (0001-old.md)            → 1
//   - "<Prefix>-NNNN.placeholder"                        → 119
//   - "S-NNNN-<slug>-<stage>" directory                  → 7
//   - "P-NNNN-<slug>" folder-plan directory (optionally
//     "-complete" suffixed), dual-kind plan entity only  → 180
func extractIDFromName(name string, entity EntityDef) int {
	if entity.Kind == EntityKindDirectory {
		// Directory-kind entity: match S-NNNN- pattern, prefix must match.
		if m := scoutDirRe.FindStringSubmatch(name); m != nil {
			n, _ := strconv.Atoi(m[1])
			return n
		}
		return 0
	}

	// Dual-kind plan entity (P-0180 locked decision #2): explicitly recognize
	// a P-NNNN-<slug>/ folder-plan directory name (with an optional
	// "-complete" stage suffix, mirroring the S-NNNN-<slug>-<stage> directory
	// branch above) so folder-plan IDs are counted the same way debug
	// directory IDs are.
	if entity.IsDualKind() {
		if m := planDirIDRe.FindStringSubmatch(name); m != nil {
			n, _ := strconv.Atoi(m[1])
			return n
		}
	}

	// File-kind entity: match only files whose prefix matches this entity.
	// numberedFileRe captures (\d{4}) from both "P-NNNN-*" and bare "NNNN-*" forms.
	if m := numberedFileRe.FindStringSubmatch(name); m != nil {
		// Check that the prefix matches: name must start with "<Prefix>-" or be bare numeric.
		if strings.HasPrefix(name, entity.Prefix+"-") || name[0] >= '0' && name[0] <= '9' {
			n, _ := strconv.Atoi(m[1])
			return n
		}
		return 0
	}
	// Placeholder: "<Prefix>-NNNN.placeholder" — group 1 is the prefix letter.
	if m := placeholderRe.FindStringSubmatch(name); m != nil {
		if m[1] == entity.Prefix {
			n, _ := strconv.Atoi(m[2])
			return n
		}
	}
	return 0
}
