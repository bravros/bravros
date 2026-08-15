package fetch

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// maxExtractedBytes caps the total decompressed size of a payload archive,
	// guarding against decompression-bomb style attacks.
	maxExtractedBytes = 256 * 1024 * 1024 // 256 MiB

	// maxExtractedEntries caps the number of entries a payload archive may contain.
	maxExtractedEntries = 20000
)

// extractTarGz extracts the gzip-compressed tar archive at srcPath into destDir.
// destDir is created (it must not already hold conflicting content — callers pass
// a freshly-allocated staging directory). Refuses path traversal (".."), absolute
// paths, symlink/hardlink entries, and archives exceeding the extracted-size or
// entry-count caps above.
func extractTarGz(srcPath, destDir string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer gz.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	tr := tar.NewReader(gz)
	var totalBytes int64
	var entries int

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		entries++
		if entries > maxExtractedEntries {
			return fmt.Errorf("archive exceeds maximum entry count (%d)", maxExtractedEntries)
		}

		// PAX extended-header entries — 'x' (per-entry) and 'g' (global) — are
		// metadata records GNU/POSIX tar emits for long paths, non-ASCII
		// names, and similar cases that don't fit the classic ustar header
		// (exactly what `tar -czf` in the release build's before-hook,
		// running on ubuntu-latest, produces for this payload). archive/tar
		// folds 'x' into the *next* real entry's Header automatically but
		// still hands both types back as their own tar.Next() entries here.
		// They carry no file content of their own, so skip them outright —
		// no disk write, no byte-budget accounting — rather than tripping
		// the "unsupported tar entry type" refusal below.
		if header.Typeflag == tar.TypeXHeader || header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}

		cleanName := filepath.Clean(header.Name)
		if cleanName == "." {
			continue
		}
		if filepath.IsAbs(cleanName) || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
			return fmt.Errorf("refusing to extract entry with unsafe path: %q", header.Name)
		}

		targetPath := filepath.Join(destDir, cleanName)
		rel, err := filepath.Rel(destDir, targetPath)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("refusing to extract entry escaping destination: %q", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return err
			}
			remaining := maxExtractedBytes - totalBytes
			if remaining <= 0 {
				return fmt.Errorf("archive exceeds maximum extracted size (%d bytes)", maxExtractedBytes)
			}
			mode := regularFileMode(header)
			written, err := extractRegularFile(targetPath, tr, mode, remaining)
			if err != nil {
				return err
			}
			totalBytes += written
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("refusing to extract link entry: %q", header.Name)
		default:
			return fmt.Errorf("unsupported tar entry type %v for %q", header.Typeflag, header.Name)
		}
	}

	return nil
}

// regularFileMode preserves the source executable bit (scripts under skills/
// and templates/ rely on it) while otherwise normalizing permissions to a safe
// default, stripping setuid/setgid/sticky bits the archive might carry.
func regularFileMode(header *tar.Header) os.FileMode {
	if header.FileInfo().Mode()&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

// extractRegularFile copies at most limit+1 bytes from r into a new file at
// targetPath, returning the number of bytes written. Returns an error if the
// entry is larger than the remaining byte budget (limit).
func extractRegularFile(targetPath string, r io.Reader, mode os.FileMode, limit int64) (int64, error) {
	out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	written, err := io.CopyN(out, r, limit+1)
	if err != nil && err != io.EOF {
		return written, fmt.Errorf("write %s: %w", targetPath, err)
	}
	if written > limit {
		return written, fmt.Errorf("archive exceeds maximum extracted size (%d bytes)", maxExtractedBytes)
	}
	return written, nil
}
