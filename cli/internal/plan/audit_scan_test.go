package plan

import (
	"os"
	"path/filepath"
	"testing"
)

// ─── AuditDuplicateIDs tests ──────────────────────────────────────────────────

// TestAuditDuplicateIDs_BranchConflict verifies that two branch-tree sources
// with the same ID but different blob SHAs produce exactly one IDCollision.
//
// Fixture:
//   - branch "main": P-0010 with content "version A"
//   - branch "feat": P-0010 with content "version B" (different content = different blob SHA)
func TestAuditDuplicateIDs_BranchConflict(t *testing.T) {
	// Create a temp git repo with two branches that share an ID but differ in content.
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runIn(t, repo, "git", "init", "-b", "main")
	runIn(t, repo, "git", "config", "user.email", "test@example.com")
	runIn(t, repo, "git", "config", "user.name", "Test")

	// main branch: P-0010 with content "version A"
	writeFile(t, filepath.Join(repo, ".planning", "P-0010-plan-version-a-todo.md"), "# version A\n")
	runIn(t, repo, "git", "add", ".")
	runIn(t, repo, "git", "commit", "-m", "chore: main P-0010 version A")

	// feat branch: P-0010 with DIFFERENT content "version B"
	runIn(t, repo, "git", "checkout", "-b", "feat")
	// Remove the A file and write a different file at the same ID.
	if err := os.Remove(filepath.Join(repo, ".planning", "P-0010-plan-version-a-todo.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	writeFile(t, filepath.Join(repo, ".planning", "P-0010-plan-version-b-todo.md"), "# version B\n")
	runIn(t, repo, "git", "add", ".")
	runIn(t, repo, "git", "commit", "-m", "chore: feat P-0010 version B")
	runIn(t, repo, "git", "checkout", "main")

	// Run the audit from inside the repo.
	collisions, sources, err := runAuditInDir(t, repo, "P")
	if err != nil {
		t.Fatalf("AuditDuplicateIDs: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("no sources returned")
	}

	// Must detect exactly one collision at ID 10.
	found := findCollision(collisions, 10)
	if found == nil {
		t.Errorf("expected collision at P-0010; collisions = %+v", collisions)
	}
}

// TestAuditDuplicateIDs_SameBlobAcrossSources verifies that when the same file
// content (same blob SHA) appears in both branches, it is NOT flagged as a collision.
//
// Fixture:
//   - branch "main": P-0020 with content "shared"
//   - branch "feat": P-0020 with THE SAME content "shared" (same blob SHA)
func TestAuditDuplicateIDs_SameBlobAcrossSources(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runIn(t, repo, "git", "init", "-b", "main")
	runIn(t, repo, "git", "config", "user.email", "test@example.com")
	runIn(t, repo, "git", "config", "user.name", "Test")

	// main branch: P-0020 with "shared" content.
	writeFile(t, filepath.Join(repo, ".planning", "P-0020-shared-todo.md"), "# shared content\n")
	runIn(t, repo, "git", "add", ".")
	runIn(t, repo, "git", "commit", "-m", "chore: main P-0020 shared")

	// feat branch: P-0020 with THE SAME content (identical bytes → same blob SHA).
	runIn(t, repo, "git", "checkout", "-b", "feat")
	// File content is identical — no changes, so it's the same blob.
	runIn(t, repo, "git", "checkout", "main")

	collisions, sources, err := runAuditInDir(t, repo, "P")
	if err != nil {
		t.Fatalf("AuditDuplicateIDs: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("no sources returned")
	}

	// Must NOT find a collision for P-0020 (same content = not divergent).
	found := findCollision(collisions, 20)
	if found != nil {
		t.Errorf("unexpected collision at P-0020 (same content should not be flagged): %+v", found)
	}
}

// TestAuditDuplicateIDs_PlaceholderVsReal verifies that a placeholder file in one
// worktree directory and a real file at the same ID in another worktree directory
// produce one collision, because the placeholder hash (sentinel) differs from the
// real file hash.
func TestAuditDuplicateIDs_PlaceholderVsReal(t *testing.T) {
	// Use two worktree-fs directories to simulate placeholder-vs-real.
	// We can't easily control ResolveScanRoots here, so we test the inner helpers directly.
	tmp := t.TempDir()

	// Dir A: real file P-0030.
	dirA := filepath.Join(tmp, "wt-a", ".planning")
	if err := os.MkdirAll(dirA, 0o755); err != nil {
		t.Fatalf("MkdirAll dirA: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirA, "P-0030-real-plan-todo.md"), []byte("# real content\n"), 0o644); err != nil {
		t.Fatalf("write real file: %v", err)
	}

	// Dir B: placeholder P-0030.
	dirB := filepath.Join(tmp, "wt-b", ".planning")
	if err := os.MkdirAll(dirB, 0o755); err != nil {
		t.Fatalf("MkdirAll dirB: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirB, "P-0030.placeholder"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write placeholder: %v", err)
	}

	entity, ok := EntityByPrefix("P")
	if !ok {
		t.Fatal("EntityByPrefix P not found")
	}

	pairsA, err := auditWorktreeFSPairs(filepath.Join(tmp, "wt-a"), entity)
	if err != nil {
		t.Fatalf("auditWorktreeFSPairs A: %v", err)
	}
	pairsB, err := auditWorktreeFSPairs(filepath.Join(tmp, "wt-b"), entity)
	if err != nil {
		t.Fatalf("auditWorktreeFSPairs B: %v", err)
	}

	// Both should have an entry for ID 30.
	hashA, okA := pairsA[30]
	hashB, okB := pairsB[30]

	if !okA {
		t.Fatal("pairsA: missing entry for ID 30 (real file)")
	}
	if !okB {
		t.Fatal("pairsB: missing entry for ID 30 (placeholder)")
	}

	// The real file must NOT have the sentinel hash.
	if hashA == placeholderSentinelHash {
		t.Error("real file should not have the sentinel hash")
	}

	// The placeholder MUST have the sentinel hash.
	if hashB != placeholderSentinelHash {
		t.Errorf("placeholder should have sentinel hash %q, got %q", placeholderSentinelHash, hashB)
	}

	// The two hashes must differ → a collision would be emitted.
	if hashA == hashB {
		t.Error("expected real file and placeholder to have different hashes (collision trigger)")
	}
}

// TestAuditDuplicateIDs_CleanTree verifies that a repo with no duplicate IDs
// returns an empty collision slice with a nil error.
func TestAuditDuplicateIDs_CleanTree(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runIn(t, repo, "git", "init", "-b", "main")
	runIn(t, repo, "git", "config", "user.email", "test@example.com")
	runIn(t, repo, "git", "config", "user.name", "Test")

	// Unique IDs on each branch.
	writeFile(t, filepath.Join(repo, ".planning", "P-0001-plan-a-todo.md"), "# plan 1\n")
	runIn(t, repo, "git", "add", ".")
	runIn(t, repo, "git", "commit", "-m", "chore: main P-0001")

	runIn(t, repo, "git", "checkout", "-b", "feat")
	writeFile(t, filepath.Join(repo, ".planning", "P-0002-plan-b-todo.md"), "# plan 2\n")
	runIn(t, repo, "git", "add", ".")
	runIn(t, repo, "git", "commit", "-m", "chore: feat P-0002")
	runIn(t, repo, "git", "checkout", "main")

	collisions, _, err := runAuditInDir(t, repo, "P")
	if err != nil {
		t.Fatalf("AuditDuplicateIDs: %v", err)
	}

	if len(collisions) != 0 {
		t.Errorf("expected no collisions on clean tree; got %d: %+v", len(collisions), collisions)
	}
}

// TestAuditAllEntities_ReturnsCombinedResults verifies that AuditAllEntities
// scans all entity prefixes and correctly aggregates collisions across multiple
// entity types.
func TestAuditAllEntities_ReturnsCombinedResults(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runIn(t, repo, "git", "init", "-b", "main")
	runIn(t, repo, "git", "config", "user.email", "test@example.com")
	runIn(t, repo, "git", "config", "user.name", "Test")

	// main: P-0001 content "main", B-0001 content "main backlog"
	writeFile(t, filepath.Join(repo, ".planning", "P-0001-plan-main-todo.md"), "# plan main\n")
	writeFile(t, filepath.Join(repo, ".planning", "backlog", "B-0001-backlog-main.md"), "# backlog main\n")
	runIn(t, repo, "git", "add", ".")
	runIn(t, repo, "git", "commit", "-m", "chore: main seed")

	// feat: P-0001 with DIFFERENT content, B-0001 with DIFFERENT content
	runIn(t, repo, "git", "checkout", "-b", "feat")
	if err := os.Remove(filepath.Join(repo, ".planning", "P-0001-plan-main-todo.md")); err != nil {
		t.Fatalf("remove P: %v", err)
	}
	writeFile(t, filepath.Join(repo, ".planning", "P-0001-plan-feat-todo.md"), "# plan feat\n")
	if err := os.Remove(filepath.Join(repo, ".planning", "backlog", "B-0001-backlog-main.md")); err != nil {
		t.Fatalf("remove B: %v", err)
	}
	writeFile(t, filepath.Join(repo, ".planning", "backlog", "B-0001-backlog-feat.md"), "# backlog feat\n")
	runIn(t, repo, "git", "add", ".")
	runIn(t, repo, "git", "commit", "-m", "chore: feat seed")
	runIn(t, repo, "git", "checkout", "main")

	// Run AuditAllEntities from inside the repo.
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()

	collisions, sources, err := AuditAllEntities()
	if err != nil {
		t.Fatalf("AuditAllEntities: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("no sources returned")
	}

	// Should find P-0001 and B-0001 collisions.
	pFound := findCollision(collisions, 1)
	if pFound == nil {
		t.Errorf("expected P-0001 collision from AuditAllEntities; got: %+v", collisions)
	}

	bFound := findCollisionWithPrefix(collisions, "B", 1)
	if bFound == nil {
		t.Errorf("expected B-0001 collision from AuditAllEntities; got: %+v", collisions)
	}
}

// ─── Helper functions ─────────────────────────────────────────────────────────

// runAuditInDir changes cwd to dir, calls AuditDuplicateIDs(prefix), and restores cwd.
func runAuditInDir(t *testing.T, dir, prefix string) ([]IDCollision, []ScanSource, error) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir %s: %v", dir, err)
	}
	defer func() {
		if err := os.Chdir(orig); err != nil {
			t.Fatalf("Chdir restore %s: %v", orig, err)
		}
	}()
	return AuditDuplicateIDs(prefix)
}

// findCollision returns the first IDCollision with the given numeric ID (any prefix).
func findCollision(collisions []IDCollision, id int) *IDCollision {
	for i := range collisions {
		if collisions[i].ID == id {
			return &collisions[i]
		}
	}
	return nil
}

// findCollisionWithPrefix returns the first IDCollision with the given prefix AND numeric ID.
func findCollisionWithPrefix(collisions []IDCollision, prefix string, id int) *IDCollision {
	for i := range collisions {
		if collisions[i].Prefix == prefix && collisions[i].ID == id {
			return &collisions[i]
		}
	}
	return nil
}

// TestAuditDuplicateIDs_FolderPlanDoesNotAbort covers the crash that made
// `bravros nextid audit` unusable in any repo containing a folder plan.
//
// The `plan` entity is dual-kind (P-0180): its Kind is EntityKindFile, but it
// also accepts `P-NNNN-<slug>/` directories — and older trees carry bare
// `NNNN-<slug>/` folders too. Those reach auditWorktreeFSPairs as directories,
// where the pre-fix code fell through to `git hash-object` and failed with
// "is a directory", aborting the ENTIRE audit rather than skipping one entry.
func TestAuditDuplicateIDs_FolderPlanDoesNotAbort(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runIn(t, repo, "git", "init", "-b", "main")
	runIn(t, repo, "git", "config", "user.email", "test@example.com")
	runIn(t, repo, "git", "config", "user.name", "Test")

	// A normal file plan, a canonical folder plan, and a legacy bare-numbered
	// folder plan — all three shapes coexist in real trees.
	writeFile(t, filepath.Join(repo, ".planning", "P-0010-file-plan-todo.md"), "# file plan\n")
	writeFile(t, filepath.Join(repo, ".planning", "P-0011-folder-plan", "PLAN.md"), "# folder plan\n")
	writeFile(t, filepath.Join(repo, ".planning", "0012-legacy-folder", "PLAN.md"), "# legacy folder\n")
	runIn(t, repo, "git", "add", ".")
	runIn(t, repo, "git", "commit", "-m", "chore: mixed plan shapes")

	collisions, _, err := runAuditInDir(t, repo, "P")
	if err != nil {
		t.Fatalf("audit must not abort on folder plans, got: %v", err)
	}
	// The three entries hold distinct ids, so nothing should be reported.
	if len(collisions) != 0 {
		t.Errorf("expected no collisions across distinct ids, got %+v", collisions)
	}
}

// TestAuditDuplicateIDs_FolderPlanBranchConflict covers the gap found reviewing
// PR #397: auditBranchTreePairs keyed on filepath.Base, so a folder plan's leaf
// blob (`.planning/P-0011-slug/PLAN.md` → basename `PLAN.md`) carried no id and
// the entry was silently dropped from the branch leg. `nextid audit` therefore
// reported ZERO collisions for a folder-plan id that genuinely collided across
// branches — a silent miss in the tool whose entire job is detecting them.
//
// Mirrors TestAuditDuplicateIDs_BranchConflict but with folder plans.
func TestAuditDuplicateIDs_FolderPlanBranchConflict(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runIn(t, repo, "git", "init", "-b", "main")
	runIn(t, repo, "git", "config", "user.email", "test@example.com")
	runIn(t, repo, "git", "config", "user.name", "Test")

	// main: P-0010 as a FILE.
	writeFile(t, filepath.Join(repo, ".planning", "P-0010-file-shape-todo.md"), "# file shape\n")
	runIn(t, repo, "git", "add", ".")
	runIn(t, repo, "git", "commit", "-m", "chore: main P-0010 as file")

	// feat: the same id as a FOLDER plan — a genuine divergent duplicate.
	runIn(t, repo, "git", "checkout", "-b", "feat")
	if err := os.Remove(filepath.Join(repo, ".planning", "P-0010-file-shape-todo.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	writeFile(t, filepath.Join(repo, ".planning", "P-0010-folder-shape", "PLAN.md"), "# folder shape\n")
	writeFile(t, filepath.Join(repo, ".planning", "P-0010-folder-shape", "TASKLIST.md"), "# tasks\n")
	runIn(t, repo, "git", "add", ".")
	runIn(t, repo, "git", "commit", "-m", "chore: feat P-0010 as folder")
	runIn(t, repo, "git", "checkout", "main")

	collisions, _, err := runAuditInDir(t, repo, "P")
	if err != nil {
		t.Fatalf("AuditDuplicateIDs: %v", err)
	}
	if findCollision(collisions, 10) == nil {
		t.Errorf("folder-plan id colliding across branches must be reported; collisions = %+v",
			collisions)
	}
}

// TestAuditBranchTreePairs_FolderPlanIsDeterministic pins the sentinel choice for
// folder plans. A folder contributes MANY blobs, so keying the map by blob SHA
// would be "last entry wins" — arbitrary, and it would make two identical folder
// plans compare unequal depending on listing order.
func TestAuditBranchTreePairs_FolderPlanIsDeterministic(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runIn(t, repo, "git", "init", "-b", "main")
	runIn(t, repo, "git", "config", "user.email", "test@example.com")
	runIn(t, repo, "git", "config", "user.name", "Test")

	writeFile(t, filepath.Join(repo, ".planning", "P-0021-multi", "PLAN.md"), "# a\n")
	writeFile(t, filepath.Join(repo, ".planning", "P-0021-multi", "TASKLIST.md"), "# b\n")
	writeFile(t, filepath.Join(repo, ".planning", "P-0021-multi", "NOTES.md"), "# c\n")
	runIn(t, repo, "git", "add", ".")
	runIn(t, repo, "git", "commit", "-m", "chore: folder plan with 3 files")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	planDef, ok := EntityByPrefix("P")
	if !ok {
		t.Fatal("plan entity not found")
	}
	pairs, err := auditBranchTreePairs("main", planDef)
	if err != nil {
		t.Fatalf("auditBranchTreePairs: %v", err)
	}
	got, present := pairs[21]
	if !present {
		t.Fatalf("folder-plan id 21 missing from branch pairs: %+v", pairs)
	}
	if got != placeholderSentinelHash {
		t.Errorf("folder plan must map to the sentinel (matching the worktree-fs leg), got %q", got)
	}
}
