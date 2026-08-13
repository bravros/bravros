package deploy

import (
	"fmt"
	"os"
	"path/filepath"
)

// SymlinkResult is the outcome of a symlink deploy operation.
type SymlinkResult struct {
	Mode     string   `json:"mode"`      // "symlinks" | "copies" | "already-symlinked"
	Linked   []string `json:"linked,omitempty"`
	Fallback bool     `json:"fallback,omitempty"` // true when copies were used as fallback
	Message  string   `json:"message,omitempty"`
}

// SymlinkDirs is the list of subdirectories to symlink/copy from src to dst.
var SymlinkDirs = []string{"skills", "hooks", "templates"}

// SymlinkDeploy attempts to create symlinks from dstDir subdirectories →
// srcDir subdirectories.  If the filesystem does not support symlinks
// (cross-filesystem, Docker, restricted FS), it falls back to copy mode with
// a stderr warning.
//
// Idempotent: if the symlink already exists and points to the correct target,
// it is a no-op.  Returns (result, nil) on success.
func SymlinkDeploy(srcDir, dstDir string) (*SymlinkResult, error) {
	result := &SymlinkResult{Mode: "symlinks"}

	for _, sub := range SymlinkDirs {
		src := filepath.Join(srcDir, sub)
		dst := filepath.Join(dstDir, sub)

		// Skip source dirs that don't exist.
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}

		// If dst exists as a valid symlink pointing to src, skip.
		if existing, err := os.Readlink(dst); err == nil {
			if existing == src {
				// Already correctly symlinked.
				continue
			}
			// Points elsewhere — remove and re-link.
			_ = os.Remove(dst)
		} else if _, statErr := os.Stat(dst); statErr == nil {
			// dst exists and is NOT a symlink.
			// Return an error unless the caller passes --force (see IsSymlinkDeployed).
			return nil, fmt.Errorf(
				"deploy: %s exists and is not a symlink; pass --force to overwrite or use copy mode",
				dst,
			)
		}

		if err := os.Symlink(src, dst); err != nil {
			// Symlink failed (cross-filesystem, restricted FS, etc.).
			// Fall back to copy mode for this entry.
			fmt.Fprintf(os.Stderr, "⚠️  symlink %s → %s failed (%v), falling back to copy\n", dst, src, err)
			result.Fallback = true
			result.Mode = "copies"
			if copyErr := copyDir(src, dst); copyErr != nil {
				return nil, fmt.Errorf("deploy: copy fallback for %s: %w", sub, copyErr)
			}
			continue
		}

		result.Linked = append(result.Linked, dst)
	}

	if len(result.Linked) == 0 && !result.Fallback {
		result.Mode = "already-symlinked"
		result.Message = "all symlinks already in place"
	}

	return result, nil
}

// IsSymlinkDeployed reports whether dstDir is symlink-deployed from srcDir.
// Returns true only when at least one expected subdirectory exists in srcDir
// AND every such directory has a valid symlink in dstDir pointing to srcDir.
func IsSymlinkDeployed(srcDir, dstDir string) bool {
	found := false
	for _, sub := range SymlinkDirs {
		src := filepath.Join(srcDir, sub)
		dst := filepath.Join(dstDir, sub)

		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue // source absent — skip
		}
		found = true

		target, err := os.Readlink(dst)
		if err != nil || target != src {
			return false
		}
	}
	// If no source dirs existed at all, we cannot claim symlinks are deployed.
	return found
}

// copyDir recursively copies src directory to dst.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(target, data, 0644)
	})
}
