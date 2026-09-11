//go:build ignore

// gen.go is the sync step behind `//go:generate go run gen.go` in embed.go.
//
// Repo-root skills/, templates/, home/ and agents/ are the source of truth for
// those four whole subtrees; the payload's scripts/ entries are synced as NAMED
// SINGLE FILES (see syncSingleFiles) rather than as the whole repo-root
// scripts/ tree, which may hold entries unrelated to the payload. This script wipes
// cli/internal/payload/{skills,templates,home,agents,scripts} and re-copies them so
// the embedded mirror stays byte-for-byte in sync on disk. It is deterministic
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

	// agents/ is the custom-subagent roster (flat *.md). It ships with the
	// skills because five skills dispatch by subagent_type to these names
	// (docs/SUBAGENTS.md); a payload carrying the call sites but not the
	// agents is the same ownership hole B-0029 closed for announce.sh.
	for _, subtree := range []string{"skills", "templates", "home", "agents"} {
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

	// The payload's scripts/ holds NAMED SINGLE FILES, not the whole repo-root
	// scripts/ tree: repo-root scripts/ is free to grow entries with nothing to
	// do with the payload (scripts/planning-events/ is one today), and the
	// mirror + CI's exact-membership assertion (verify-manifest.yml) both
	// expect exactly this list under cli/internal/payload/scripts/. Adding a
	// file here means adding it to the //go:embed line in embed.go, to
	// EXPECTED_ENTRIES in verify-manifest.yml, and to the public mirror's
	// allowlist in publish-public.yml.
	if err := syncSingleFiles(repoRoot, payloadDir, "scripts", []string{
		"reconcile-global-claude.py",
		"announce.sh",
		"mute-announce.sh",
	}, &executable); err != nil {
		return err
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

// syncSingleFiles wipes payloadDir/subdir ONCE and then re-copies exactly
// repoRoot/subdir/<name> for each name into it, preserving each source's
// permission bits and appending the embed.FS-relative path to *executable for
// every source file that is executable. Wiping the destination subdirectory
// first is what gives this the same staleness-pruning guarantee copyTree
// provides for whole subtrees: a file that used to be synced here and no
// longer exists in the source cannot survive as a stale leftover.
//
// The wipe is deliberately hoisted out of the per-file loop — a per-file wipe
// would delete the previous name's copy on the next iteration, leaving the
// mirror with only the last file in the list.
func syncSingleFiles(repoRoot, payloadDir, subdir string, names []string, executable *[]string) error {
	dstDir := filepath.Join(payloadDir, subdir)

	if err := os.RemoveAll(dstDir); err != nil {
		return fmt.Errorf("wipe %q: %w", dstDir, err)
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %q: %w", dstDir, err)
	}

	for _, name := range names {
		src := filepath.Join(repoRoot, subdir, name)
		dst := filepath.Join(dstDir, name)

		info, err := os.Stat(src)
		if err != nil {
			return fmt.Errorf("source file %q: %w", src, err)
		}
		if info.IsDir() {
			return fmt.Errorf("source file %q is a directory, want a file", src)
		}

		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("read %q: %w", src, err)
		}
		if err := os.WriteFile(dst, data, info.Mode().Perm()); err != nil {
			return fmt.Errorf("write %q: %w", dst, err)
		}

		if info.Mode()&0o111 != 0 {
			*executable = append(*executable, subdir+"/"+name)
		}

		fmt.Printf("gen: synced %s -> %s\n", src, dst)
	}
	return nil
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
