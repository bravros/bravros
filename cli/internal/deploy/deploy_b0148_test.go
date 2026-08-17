package deploy

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// disableClaudeMdEmbeddedFallback overrides the package-level
// claudeMdEmbeddedFallback var for the duration of the calling test, so
// reconcileGlobalClaudeMd behaves as if the embedded payload ALSO lacks
// home/CLAUDE.md and scripts/reconcile-global-claude.py — simulating "both
// tiers absent" without needing a build of this binary whose embedded
// payload actually omits those files (the real payload.FS always carries
// them, kept in sync by gen.go). t.Cleanup restores the real
// payload.Extract-based implementation.
func disableClaudeMdEmbeddedFallback(t *testing.T) {
	t.Helper()
	prev := claudeMdEmbeddedFallback
	claudeMdEmbeddedFallback = func(string) (bool, error) { return false, nil }
	t.Cleanup(func() { claudeMdEmbeddedFallback = prev })
}

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
// CLAUDE.md over ~/.claude/CLAUDE.md. With NEITHER tier able to supply a
// managed source (no home/CLAUDE.md in the source tree, and the embedded
// payload fallback disabled for this test — see
// disableClaudeMdEmbeddedFallback), reconcile no-ops and an existing personal
// CLAUDE.md at the target must survive a deploy untouched — previously the
// {"CLAUDE.md","CLAUDE.md"} fileMapping overwrote it with the repo's project
// doc. (P-0018 Phase 3: with the embedded fallback left ENABLED, the source
// lacking home/CLAUDE.md no longer means "nothing to reconcile" — see
// TestDeploy_ReconcilesFromEmbeddedPayloadWhenSourceLacksHome.)
func TestDeploy_DoesNotClobberPersonalCLAUDEmd(t *testing.T) {
	disableClaudeMdEmbeddedFallback(t)

	src := setupTestRepo(t) // has root CLAUDE.md "# Claude" but NO home/CLAUDE.md
	target := filepath.Join(t.TempDir(), ".claude")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	personal := "Fale comigo em portugues.\n\n# >>> bravros-managed-global >>>\n...\n# <<< bravros-managed-global <<<\n"
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

// TestDeploy_ReportsSkippedClaudeMdReconcile is the regression test for the
// silent no-op: with NEITHER tier able to supply a managed source (no
// home/CLAUDE.md in the source tree, embedded fallback disabled for this
// test — see disableClaudeMdEmbeddedFallback), the reconcile used to bail out
// via a bare `return nil` that never reached the caller — visible only as an
// absence, never as a reported outcome. Deploy must now count and name the
// skip in DeployResult.ClaudeMdReconcileSkipped instead of swallowing it.
func TestDeploy_ReportsSkippedClaudeMdReconcile(t *testing.T) {
	disableClaudeMdEmbeddedFallback(t)

	src := setupTestRepo(t) // has no home/CLAUDE.md
	target := filepath.Join(t.TempDir(), ".claude")

	res, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if len(res.ClaudeMdReconcileSkipped) != 1 {
		t.Fatalf("expected exactly one reported CLAUDE.md reconcile skip, got %v", res.ClaudeMdReconcileSkipped)
	}
	if res.ClaudeMdReconcileSkipped[0] != claudeMdSkipNoSource {
		t.Errorf("expected skip reason %q, got %q", claudeMdSkipNoSource, res.ClaudeMdReconcileSkipped[0])
	}

	// The JSON shape must carry the same visibility --json consumers rely on.
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if !strings.Contains(string(data), `"claude_md_reconcile_skipped":["source-claude-md-missing"]`) {
		t.Errorf("expected claude_md_reconcile_skipped in JSON output, got: %s", data)
	}
}

// TestDeploy_ReconcilesFromEmbeddedPayloadWhenSourceLacksHome is the P-0018
// Phase 3 regression test for the OTHER half of the fallback: with the
// embedded payload's claudeMdEmbeddedFallback left ENABLED (the production
// default — not overridden here, unlike the two tests above), a source tree
// with no home/CLAUDE.md or scripts/reconcile-global-claude.py (the same
// shape setupTestRepo has always produced) must still reconcile — using the
// compiled-in payload copy — instead of reporting a skip. This is the whole
// point of embedding home/ and scripts/reconcile-global-claude.py
// (gen.go/embed.go): a missing source checkout no longer means "nothing to
// reconcile", it means "read it from the binary instead".
func TestDeploy_ReconcilesFromEmbeddedPayloadWhenSourceLacksHome(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available on this machine — reconcileGlobalClaudeMd needs it regardless of which tier supplies the source")
	}

	src := setupTestRepo(t) // has no home/CLAUDE.md, no scripts/ dir
	target := filepath.Join(t.TempDir(), ".claude")

	res, err := Deploy(DeployOpts{SourceDir: src, TargetDir: target})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if len(res.ClaudeMdReconcileSkipped) != 0 {
		t.Fatalf("expected no reconcile skip — the embedded fallback should have supplied the managed source, got %v", res.ClaudeMdReconcileSkipped)
	}

	dest := filepath.Join(target, "CLAUDE.md")
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read reconciled CLAUDE.md: %v", err)
	}
	if !strings.Contains(string(got), "# >>> bravros-managed-global >>>") || !strings.Contains(string(got), "# <<< bravros-managed-global <<<") {
		t.Fatalf("reconciled CLAUDE.md is missing the managed-global markers — the embedded fallback did not actually reconcile:\n%s", got)
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
