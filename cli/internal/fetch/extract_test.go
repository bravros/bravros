package fetch

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractTarGz_SkipsPaxGlobalHeader proves the actual production fix in
// isolation: a synthetic archive carrying an explicit PAX global-header entry
// (tar.TypeXGlobalHeader, 'g') — the one type Go's archive/tar surfaces to
// callers instead of consuming internally — must be skipped rather than
// tripping the "unsupported tar entry type" refusal, and must not touch disk
// or count toward the extracted-byte budget.
func TestExtractTarGz_SkipsPaxGlobalHeader(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	if err := tw.WriteHeader(&tar.Header{
		Name:       "pax_global_header",
		Typeflag:   tar.TypeXGlobalHeader,
		PAXRecords: map[string]string{"comment": "synthetic global header"},
	}); err != nil {
		t.Fatalf("write global header: %v", err)
	}

	body := []byte("real file content\n")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "skills/example.md",
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     int64(len(body)),
	}); err != nil {
		t.Fatalf("write file header: %v", err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatalf("write file body: %v", err)
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}

	srcPath := filepath.Join(t.TempDir(), "archive.tar.gz")
	if err := os.WriteFile(srcPath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	destDir := filepath.Join(t.TempDir(), "dest")
	if err := extractTarGz(srcPath, destDir); err != nil {
		t.Fatalf("extractTarGz: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(destDir, "skills/example.md"))
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("content mismatch: got %q want %q", got, body)
	}

	if _, err := os.Stat(filepath.Join(destDir, "pax_global_header")); !os.IsNotExist(err) {
		t.Fatalf("global header entry must not be written to disk, stat err=%v", err)
	}
}

// TestExtractRealSystemTarball shells out to the system `tar` to build a
// payload-shaped tree — nested dirs, an executable script, a UTF-8-named
// file, and a file whose path exceeds 100 chars (the condition that forces
// PAX extended headers in the first place, per GNU/POSIX tar) — then extracts
// it with extractTarGz and verifies a byte-for-byte, mode-preserving round
// trip. This is the same tar -czf invocation .goreleaser.yml's before-hook
// uses; a synthetic Go-built tarball cannot exercise real tar's header
// choices the way this does.
func TestExtractRealSystemTarball(t *testing.T) {
	tarBin, err := exec.LookPath("tar")
	if err != nil {
		t.Skip("system tar not found on PATH, skipping real-tarball extraction test")
	}

	srcDir := filepath.Join(t.TempDir(), "src")
	files := map[string][]byte{
		"a/b/c/file1.txt":      []byte("hello nested file\n"),
		"bin/script.sh":        []byte("#!/bin/sh\necho hi\n"),
		"unicode/héllo-世界.txt": []byte("unicode filename content\n"),
		"longnames/" + strings.Repeat("x", 150) + "-long-filename.txt": []byte("long path content\n"),
	}
	for rel, content := range files {
		full := filepath.Join(srcDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	execPath := filepath.Join(srcDir, "bin", "script.sh")
	if err := os.Chmod(execPath, 0o755); err != nil {
		t.Fatalf("chmod executable: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "real.tar.gz")
	cmd := exec.Command(tarBin, "-czf", archivePath, "-C", srcDir, ".")
	// COPYFILE_DISABLE keeps macOS bsdtar from emitting AppleDouble "._name"
	// resource-fork entries, which would otherwise pollute the archive with
	// files this test's source tree never created. No-op on Linux/GNU tar.
	cmd.Env = append(os.Environ(), "COPYFILE_DISABLE=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("system tar failed: %v\n%s", err, out)
	}

	destDir := filepath.Join(t.TempDir(), "dest")
	if err := extractTarGz(archivePath, destDir); err != nil {
		t.Fatalf("extractTarGz: %v", err)
	}

	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(destDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read extracted %s: %v", rel, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("content mismatch for %s: got %q want %q", rel, got, want)
		}
	}

	info, err := os.Stat(filepath.Join(destDir, "bin", "script.sh"))
	if err != nil {
		t.Fatalf("stat extracted script: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected executable bit preserved on bin/script.sh, got mode %o", info.Mode())
	}
}
