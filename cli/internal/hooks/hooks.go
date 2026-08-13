// Package hooks provides detection, classification, and refresh logic for the
// kaisser-managed commit-msg git hook.
//
// The canonical hook lives at ~/.claude/templates/.githooks/commit-msg (deployed
// location) or at templates/.githooks/commit-msg in the source repo.
//
// Classification state machine:
//
//	Missing      – no file at target path
//	Pristine     – file matches a known historical MD5 (no marker; older installs)
//	OldCanonical – file has the sentinel marker but an older version number
//	Customized   – file has no marker and no MD5 match (user-edited)
//	Foreign      – file exists, has content, but shows no kaisser origin at all
//
// Refresh writes the canonical hook atomically and marks it 0755.
package hooks

import (
	"crypto/md5" //nolint:gosec // MD5 used for content-identity, not cryptographic security
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// MarkerLine is the prefix embedded on line 2 of every kaisser-managed commit-msg hook.
// The actual line in the file may append a version suffix (e.g., "v1"), so all
// detection uses HasPrefix matching rather than exact equality.
const MarkerLine = "# kaisser-managed-commit-msg-hook"

// CurrentMarkerVersion is the version number embedded in the current canonical hook.
const CurrentMarkerVersion = 1

// HistoricalCanonicalMD5s holds the MD5 hexdigest(s) of prior canonical hook versions
// that shipped WITHOUT the sentinel marker (pre-marker era).  When Classify sees a
// file that matches one of these hashes (and has no marker), it returns StatusPristine
// so callers know the hook is intact but needs a marker upgrade.
//
// The first entry is the MD5 of the pre-v1 canonical (the 193-line version that
// shipped before P-0165 Phase 1 promoted the marker-bearing canonical).  Future
// canonical bumps append the OLD MD5 here in a separate PR before the new
// canonical lands.
var HistoricalCanonicalMD5s = []string{
	"f4b0782636e1311ec132e5a0ebc63529",
}

// Status describes the state of a project's commit-msg hook relative to the
// kaisser canonical version.
type Status int

const (
	// StatusMissing – no commit-msg file at the target path.
	StatusMissing Status = iota

	// StatusPristine – file matches a known historical MD5 but has no sentinel
	// marker.  Content is correct; only the marker line is absent (drifted=true).
	StatusPristine

	// StatusOldCanonical – file has the sentinel marker but an older version number.
	// Content is stale; a Refresh is needed.
	StatusOldCanonical

	// StatusCurrent – file carries the current sentinel marker — content may or may not
	// match canonical; MD5 comparison happens upstream in detectHookDrift.
	StatusCurrent

	// StatusForeign – file exists but shows no kaisser origin (no marker, no
	// historical MD5 match).  Must not be overwritten without --force.
	StatusForeign
)

// String returns a human-readable label for the status.
func (s Status) String() string {
	switch s {
	case StatusMissing:
		return "missing"
	case StatusPristine:
		return "pristine"
	case StatusOldCanonical:
		return "old-canonical"
	case StatusCurrent:
		return "current"
	case StatusForeign:
		return "foreign"
	default:
		return "unknown"
	}
}

// ReadMarker reads the second line of the file at path and parses the sentinel
// marker.  Returns (version, true, nil) when the marker is present, (0, false, nil)
// when absent, or (0, false, err) on I/O errors.
//
// The second line is expected to follow the pattern:
//
//	# kaisser-managed-commit-msg-hook v<N>
func ReadMarker(path string) (version int, ok bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false, err
	}

	lines := strings.SplitN(string(data), "\n", 3)
	if len(lines) < 2 {
		return 0, false, nil
	}

	line := strings.TrimSpace(lines[1])
	if !strings.HasPrefix(line, MarkerLine) {
		return 0, false, nil
	}

	// Extract optional version suffix " v<N>"
	rest := strings.TrimPrefix(line, MarkerLine)
	rest = strings.TrimSpace(rest)
	if rest == "" {
		// Marker present but no version — treat as version 0
		return 0, true, nil
	}

	if !strings.HasPrefix(rest, "v") {
		// Malformed suffix — marker is still present, version unknown (0)
		return 0, true, nil
	}

	var v int
	if _, scanErr := fmt.Sscanf(rest[1:], "%d", &v); scanErr != nil {
		return 0, true, nil
	}
	return v, true, nil
}

// ComputeMD5 returns the lowercase hex MD5 digest of the file at path.
func ComputeMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New() //nolint:gosec
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Classify compares the hook at targetPath against the canonical at canonicalPath
// and returns the appropriate Status.
//
// canonicalPath may be empty: if empty, historical-MD5 matching is still performed,
// but version comparisons require reading the canonical marker and will fall back
// to treating any-version marker as StatusCurrent when the canonical is unavailable.
func Classify(targetPath, canonicalPath string) (Status, error) {
	// 1. Missing?
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		return StatusMissing, nil
	} else if err != nil {
		return StatusMissing, err
	}

	// 2. Read marker from target
	targetVersion, hasMarker, err := ReadMarker(targetPath)
	if err != nil {
		return StatusForeign, err
	}

	if hasMarker {
		// File has the marker — determine if it's current or old
		if targetVersion == CurrentMarkerVersion {
			return StatusCurrent, nil
		}
		return StatusOldCanonical, nil
	}

	// 3. No marker — check historical MD5s
	targetMD5, err := ComputeMD5(targetPath)
	if err != nil {
		return StatusForeign, err
	}
	for _, historical := range HistoricalCanonicalMD5s {
		if targetMD5 == historical {
			return StatusPristine, nil
		}
	}

	// 4. Also check if it matches the current canonical (sans marker)
	if canonicalPath != "" {
		canonicalMD5, err := ComputeMD5(canonicalPath)
		if err == nil && targetMD5 == canonicalMD5 {
			// Matches canonical byte-for-byte but has no marker — treat as Pristine
			return StatusPristine, nil
		}
	}

	// 5. No match — foreign file
	return StatusForeign, nil
}

// Refresh atomically copies canonicalPath to targetPath and sets mode 0755.
// The target directory is created if missing.  The write is atomic: content is
// written to a sibling ".tmp" file, fsynced, then renamed into place.
func Refresh(targetPath, canonicalPath string) error {
	// Ensure destination directory exists
	destDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("hooks: mkdir %s: %w", destDir, err)
	}

	// Open canonical source
	src, err := os.Open(canonicalPath)
	if err != nil {
		return fmt.Errorf("hooks: open canonical %s: %w", canonicalPath, err)
	}
	defer src.Close()

	// Write to a temp file in the same directory (same filesystem → rename is atomic)
	tmp := targetPath + ".tmp"
	dst, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("hooks: create tmp %s: %w", tmp, err)
	}

	_, copyErr := io.Copy(dst, src)
	syncErr := dst.Sync()
	closeErr := dst.Close()

	if copyErr != nil {
		os.Remove(tmp)
		return fmt.Errorf("hooks: copy to tmp: %w", copyErr)
	}
	if syncErr != nil {
		os.Remove(tmp)
		return fmt.Errorf("hooks: fsync tmp: %w", syncErr)
	}
	if closeErr != nil {
		os.Remove(tmp)
		return fmt.Errorf("hooks: close tmp: %w", closeErr)
	}

	// Atomic rename
	if err := os.Rename(tmp, targetPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("hooks: rename tmp→target: %w", err)
	}

	// Ensure +x (rename preserves mode from OpenFile, but set explicitly for safety)
	if err := os.Chmod(targetPath, 0755); err != nil {
		return fmt.Errorf("hooks: chmod %s: %w", targetPath, err)
	}

	return nil
}

// EnsureHooksPath idempotently sets `git config core.hooksPath .bravros/hooks` in
// the repo rooted at repoRoot.  It reads the current value first and skips the
// write if it is already ".bravros/hooks".
func EnsureHooksPath(repoRoot string) error {
	// Read current value
	readCmd := exec.Command("git", "config", "--local", "core.hooksPath")
	readCmd.Dir = repoRoot
	out, err := readCmd.Output()
	if err == nil {
		current := strings.TrimSpace(string(out))
		if current == ".bravros/hooks" || current == ".bravros/hooks/" {
			return nil // already correct — no-op
		}
	}
	// err here means unset or config error — proceed to write

	// Write the value
	writeCmd := exec.Command("git", "config", "core.hooksPath", ".bravros/hooks")
	writeCmd.Dir = repoRoot
	if out, err := writeCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("hooks: git config core.hooksPath: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
