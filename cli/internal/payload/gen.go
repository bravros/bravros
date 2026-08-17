//go:build ignore

// gen.go is the sync step behind `//go:generate go run gen.go` in embed.go.
//
// Repo-root skills/ and templates/ are the source of truth; this script
// wipes cli/internal/payload/{skills,templates} and re-copies them so the
// embedded mirror stays byte-for-byte in sync on disk. It is deterministic
// (files are visited and written in the sorted order fs.WalkDir already
// guarantees). It also writes executable_manifest.txt, listing every synced
// file that was executable in the source (templates/.githooks/commit-msg
// plus several skills/*/scripts/*.sh|py) — go:embed itself does NOT preserve
// the executable bit (verified: every file comes back mode 0444), so
// Extract() in embed.go consults this manifest to restore it on write.
//
// Invocation: run from anywhere inside the repo via
//
//	cd cli && go generate ./internal/payload/...
//
// `go generate` sets the working directory to the package directory holding
// the //go:generate directive (cli/internal/payload), but this script does
// not rely on that: it locates its own source file via runtime.Caller(0) and
// walks up from there to find the repo root (the nearest ancestor containing
// a .git entry), so it also works when invoked directly with
// `go run gen.go` from cli/internal/payload.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// executableManifestName is the file gen.go writes and embed.go reads back.
// go:embed does NOT preserve the executable bit — every embedded file comes
// back as mode 0444 regardless of its source permissions (verified against
// this Go toolchain; the "0555 for executables" behavior sometimes cited for
// embed.FS does not hold in practice). Executable scripts are scattered
// throughout skills/ (not just templates/.githooks/), so instead of losing
// that bit, gen.go records which synced paths were executable in the source,
// and Extract (embed.go) consults this manifest to restore the bit on write.
const executableManifestName = "executable_manifest.txt"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("resolve gen.go's own path via runtime.Caller")
	}
	payloadDir := filepath.Dir(thisFile)

	repoRoot, err := findRepoRoot(payloadDir)
	if err != nil {
		return err
	}

	var executable []string

	for _, subtree := range []string{"skills", "templates"} {
		src := filepath.Join(repoRoot, subtree)
		dst := filepath.Join(payloadDir, subtree)

		if _, err := os.Stat(src); err != nil {
			return fmt.Errorf("source tree %q: %w", src, err)
		}

		if err := os.RemoveAll(dst); err != nil {
			return fmt.Errorf("wipe %q: %w", dst, err)
		}

		exec, err := copyTree(src, dst, subtree)
		if err != nil {
			return fmt.Errorf("copy %q -> %q: %w", src, dst, err)
		}
		executable = append(executable, exec...)

		fmt.Printf("gen: synced %s -> %s\n", src, dst)
	}

	sort.Strings(executable)
	manifestPath := filepath.Join(payloadDir, executableManifestName)
	manifestBody := strings.Join(executable, "\n")
	if len(executable) > 0 {
		manifestBody += "\n"
	}
	if err := os.WriteFile(manifestPath, []byte(manifestBody), 0o644); err != nil {
		return fmt.Errorf("write %q: %w", manifestPath, err)
	}
	fmt.Printf("gen: recorded %d executable file(s) in %s\n", len(executable), manifestPath)

	return nil
}

// findRepoRoot walks up from dir looking for the nearest ancestor containing
// a .git entry (directory for a normal checkout, file for a worktree).
func findRepoRoot(dir string) (string, error) {
	for cur := dir; ; {
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			return cur, nil
		}

		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("no .git found walking up from %q", dir)
		}
		cur = parent
	}
}

// copyTree recursively copies src to dst, preserving each file's permission
// bits and visiting entries in the deterministic sorted order fs.WalkDir
// provides. It returns the embed.FS-relative paths (prefixed with subtree,
// forward-slash separated) of every file that was executable in src, for the
// executable-bit manifest.
func copyTree(src, dst, subtree string) ([]string, error) {
	var executable []string

	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not supported in the payload source tree: %q", path)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
			return err
		}

		if info.Mode()&0o111 != 0 {
			embedPath := subtree + "/" + filepath.ToSlash(rel)
			executable = append(executable, embedPath)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return executable, nil
}
