// Package payload embeds the repo-root skills/ and templates/ trees into the
// bravros binary via go:embed.
//
// go:embed can only reference paths inside its own package directory — it
// cannot cross the module boundary with "..". skills/ and templates/ live at
// the repo root, one level above the cli/ Go module, so they cannot be
// embedded directly. Instead, cli/internal/payload/skills and
// cli/internal/payload/templates are a generated MIRROR of the repo-root
// trees, kept in sync by `go generate` (see gen.go) and enforced in CI by
// .github/workflows/verify-manifest.yml (`git diff --exit-code` after
// re-running the generator).
//
// LOCKED DECISION: the synced mirror is COMMITTED, not gitignored. go:embed
// is a compile-time error against a missing or empty directory, so a
// gitignored mirror would break `go build` and `go test` on a fresh clone
// (no generator has run yet). Committing it keeps the module self-contained;
// staleness is caught by the CI drift check instead of by breaking builds.
//
// Do NOT write `all:settings` here — a `settings/` directory does not exist
// anywhere in this repo (per P-0015 Traps). "claude-settings" is merge
// *logic* that lives in cli/internal/managed/, not a file tree to embed.
package payload

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:generate go run gen.go

// FS is the embedded mirror of the repo-root skills/ and templates/ trees.
// Regenerate it with `cd cli && go generate ./internal/payload/...` whenever
// the repo-root trees change.
//
//go:embed all:skills all:templates
var FS embed.FS

// executableManifest is written by gen.go alongside the synced mirror: one
// embed.FS-relative path per line for every file that was executable in the
// repo-root source. go:embed itself does NOT preserve the executable bit —
// every file it stores comes back as mode 0444 regardless of source
// permissions — so Extract consults this manifest instead.
//
//go:embed executable_manifest.txt
var executableManifestRaw string

var executableManifest = parseExecutableManifest(executableManifestRaw)

func parseExecutableManifest(raw string) map[string]bool {
	set := make(map[string]bool)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		set[line] = true
	}
	return set
}

// ListTopLevel returns the names of the top-level entries (files and
// directories) directly under subtree ("skills" or "templates") in the
// embedded FS, sorted lexicographically.
func ListTopLevel(subtree string) ([]string, error) {
	entries, err := fs.ReadDir(FS, subtree)
	if err != nil {
		return nil, fmt.Errorf("payload: list top-level entries of %q: %w", subtree, err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// Extract writes every file under subtree ("skills" or "templates", or a
// deeper path such as "templates/.githooks") in the embedded FS to destDir,
// preserving the subtree's relative layout and restoring the executable bit
// from executableManifest (go:embed itself discards it — see above).
func Extract(subtree, destDir string) error {
	return fs.WalkDir(FS, subtree, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("payload: walk %q: %w", path, err)
		}

		rel, err := filepath.Rel(subtree, path)
		if err != nil {
			return fmt.Errorf("payload: relativize %q against %q: %w", path, subtree, err)
		}
		target := filepath.Join(destDir, rel)

		if d.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("payload: mkdir %q: %w", target, err)
			}
			return nil
		}

		data, err := fs.ReadFile(FS, path)
		if err != nil {
			return fmt.Errorf("payload: read %q: %w", path, err)
		}

		mode := os.FileMode(0o644)
		if executableManifest[path] {
			mode = 0o755
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("payload: mkdir %q: %w", filepath.Dir(target), err)
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return fmt.Errorf("payload: write %q: %w", target, err)
		}
		return nil
	})
}
