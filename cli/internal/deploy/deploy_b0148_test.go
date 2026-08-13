package deploy

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDeploy_ForceOverwritesUnchangedFile asserts that a destination file
// whose mtime+size matches the source is normally skipped, but is
// unconditionally overwritten when Force=true (B-0148).
func TestDeploy_ForceOverwritesUnchangedFile(t *testing.T) {
	src := setupTestRepo(t)
	target := filepath.Join(t.TempDir(), ".claude")

	// First deploy: populate the target so subsequent runs see byte-identical
	// destinations.
	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target}); err != nil {
		t.Fatalf("initial Deploy: %v", err)
	}

	// Use statusline.sh as the skip/force fixture — CLAUDE.md is no longer a plain
	// file mapping (it's reconciled from home/CLAUDE.md, not whole-file copied).
	dst := filepath.Join(target, "statusline.sh")

	// Touch source so it has a newer mtime than dst — the default (non-force)
	// path will copy. Then mark dst with the new content's mtime equal to or
	// later than source to ensure the skip-check would skip it. Without
	// Force, the second deploy MUST skip; with Force, it MUST copy.
	srcInfo, err := os.Stat(filepath.Join(src, "config", "statusline.sh"))
	if err != nil {
		t.Fatalf("stat src: %v", err)
	}
	// Build stale content with EXACTLY the same size as source but DIFFERENT
	// bytes so we can detect overwrite via content comparison.
	stale := make([]byte, srcInfo.Size())
	for i := range stale {
		stale[i] = 'X'
	}
	if err := os.WriteFile(dst, stale, 0o644); err != nil {
		t.Fatalf("write stale dst: %v", err)
	}
	// Make dst newer than the source so fileUpToDate returns true (size match
	// + dst mtime >= src mtime ⇒ skip).
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(dst, future, future); err != nil {
		t.Fatalf("chtimes dst: %v", err)
	}

	// Without Force, the second deploy should skip CLAUDE.md (size+mtime match
	// the skip rule).
	res, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target})
	if err != nil {
		t.Fatalf("incremental Deploy: %v", err)
	}
	skipped := false
	for _, s := range res.Skipped {
		if s == "config/statusline.sh" {
			skipped = true
			break
		}
	}
	if !skipped {
		t.Fatalf("incremental deploy did not skip config/statusline.sh (skipped=%v)", res.Skipped)
	}
	post, _ := os.ReadFile(dst)
	if string(post) != string(stale) {
		t.Fatalf("incremental deploy unexpectedly overwrote stale dst content\nwant: %q\ngot:  %q", string(stale), string(post))
	}

	// Now force-deploy: the same file MUST be overwritten with source content.
	res, err = Deploy(DeployOpts{SourceDir: src, TargetDir: target, Force: true})
	if err != nil {
		t.Fatalf("force Deploy: %v", err)
	}
	if !res.Forced {
		t.Errorf("expected res.Forced=true on Force deploy")
	}
	if len(res.Skipped) != 0 {
		t.Errorf("force deploy should not skip any files, got skipped=%v", res.Skipped)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst after force: %v", err)
	}
	if string(got) != "echo hi" {
		t.Fatalf("force deploy did not overwrite statusline.sh\nwant: %q\ngot:  %q", "echo hi", string(got))
	}
}

// TestDeploy_DoesNotClobberPersonalCLAUDEmd is the regression test for the
// managed-global clobber: Deploy must NEVER whole-file copy the repo's root
// CLAUDE.md over ~/.claude/CLAUDE.md. With no home/CLAUDE.md in the source
// (reconcile no-ops), an existing personal CLAUDE.md at the target must survive
// a deploy untouched — previously the {"CLAUDE.md","CLAUDE.md"} fileMapping
// overwrote it with the repo's project doc.
func TestDeploy_DoesNotClobberPersonalCLAUDEmd(t *testing.T) {
	src := setupTestRepo(t) // has root CLAUDE.md "# Claude" but NO home/CLAUDE.md
	target := filepath.Join(t.TempDir(), ".claude")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	personal := "Fale comigo em portugues.\n\n# >>> kaisser-managed-global >>>\n...\n# <<< kaisser-managed-global <<<\n"
	dst := filepath.Join(target, "CLAUDE.md")
	if err := os.WriteFile(dst, []byte(personal), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read CLAUDE.md after deploy: %v", err)
	}
	if string(got) != personal {
		t.Fatalf("deploy clobbered personal CLAUDE.md\nwant: %q\ngot:  %q", personal, string(got))
	}
}

// TestDeploy_ForceDryRunListsAllFilesWithoutWriting asserts that --force
// --dry-run lists every source file (the would-copy set) and writes nothing
// to disk (B-0148).
func TestDeploy_ForceDryRunListsAllFilesWithoutWriting(t *testing.T) {
	src := setupTestRepo(t)
	target := filepath.Join(t.TempDir(), ".claude")

	res, err := Deploy(DeployOpts{
		SourceDir: src,
		TargetDir: target,
		Force:     true,
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if !res.DryRun {
		t.Errorf("expected DryRun=true in result")
	}
	if !res.Forced {
		t.Errorf("expected Forced=true in result")
	}
	if res.FilesDeployed == 0 || len(res.Files) == 0 {
		t.Fatalf("expected non-empty would-copy file list, got %d files", res.FilesDeployed)
	}
	if len(res.Files) != res.FilesDeployed {
		t.Errorf("FilesDeployed (%d) != len(Files) (%d)", res.FilesDeployed, len(res.Files))
	}

	// Target dir must NOT have been written to.
	if entries, err := os.ReadDir(target); err == nil && len(entries) > 0 {
		t.Errorf("force --dry-run wrote to target dir (entries=%d)", len(entries))
	}
}
