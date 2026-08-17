package selfupdate

// The passive "a newer bravros exists" notice, and the package-manager guard
// that decides whether `bravros update` may replace the binary at all.
//
// Cost budget (P-0015 Traps — "the SessionStart hook must stay near-free"):
// the SessionStart lane is local-only, so this is the ONLY thing in it that can
// touch the network, and it is allowed to do so at most once per NoticeInterval
// (24h by default) — behind the 6h TTL cache that already guards the whole run.
// Between checks the notice is printed from cached state, which costs one file
// read. BRAVROS_NO_UPDATE_CHECK=1 turns it off entirely.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bravros/bravros/cli/internal/config"
)

const (
	// NoUpdateCheckEnv disables the passive check completely when set to "1"
	// (or "true"). Nothing else in the update path reads it — `bravros update`
	// run by hand always checks, because the operator just asked it to.
	NoUpdateCheckEnv = "BRAVROS_NO_UPDATE_CHECK"

	// NoticeIntervalEnv overrides the 24h interval (Go duration; "0" disables
	// rate limiting, which is what tests that want a request per run use).
	NoticeIntervalEnv = "BRAVROS_UPDATE_NOTICE_TTL"

	// DefaultNoticeInterval is the minimum spacing between passive checks.
	DefaultNoticeInterval = 24 * time.Hour

	noticeStateFileName = ".bravros-update-notice.json"
)

// NoticeDisabled reports whether the operator opted out of the passive check.
func NoticeDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(NoUpdateCheckEnv))) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// NoticeInterval is the minimum time between two passive remote checks.
func NoticeInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv(NoticeIntervalEnv))
	if raw == "" {
		return DefaultNoticeInterval
	}
	if raw == "0" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return DefaultNoticeInterval
	}
	return d
}

// NoticeStatePath is where the passive check records its last run:
// <config dir>/state/.bravros-update-notice.json. state/ is on deploy's
// never-prune allowlist, so no deploy or refresh can remove it.
func NoticeStatePath() string {
	return filepath.Join(config.ConfigDir(), "state", noticeStateFileName)
}

// NoticeState is the cached result of the last passive check.
type NoticeState struct {
	LastCheck time.Time `json:"last_check"`
	LatestTag string    `json:"latest_tag"`
}

// LoadNoticeState reads the state at path; a missing file is the zero value.
func LoadNoticeState(path string) NoticeState {
	data, err := os.ReadFile(path)
	if err != nil {
		return NoticeState{}
	}
	var s NoticeState
	if err := json.Unmarshal(data, &s); err != nil {
		return NoticeState{}
	}
	return s
}

// SaveNoticeState persists s at path, creating parent directories.
func SaveNoticeState(path string, s NoticeState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// NoticeResult is what a passive check concluded. The auto-update lane
// (autoupdate.go) needs the TAG and whether this call actually went to the
// network, not just the rendered line — but the cadence, the state file and the
// offline-is-silent contract must stay identical for both consumers, so there
// is exactly one implementation and PassiveCheck is a projection of it.
type NoticeResult struct {
	// Line is the notice to print, "" when there is nothing to say.
	Line string
	// LatestTag is the newest tag known — freshly resolved, or the one the
	// last check recorded. "" when no check has ever succeeded.
	LatestTag string
	// Checked is true only when THIS call contacted the remote. The auto lane
	// gates the swap on it, which is what keeps the 24h cadence honest: a
	// cache-hit session prints the cached notice and does nothing else, so a
	// deferred release is retried on the next real check, not on every session.
	Checked bool
	// Disabled is true when BRAVROS_NO_UPDATE_CHECK opted the machine out.
	Disabled bool
}

// PassiveCheck returns the one-line notice to print, or "" when there is
// nothing to say. See PassiveCheckDetail — this is its Line.
func PassiveCheck(ctx context.Context, r TagResolver, statePath string, interval time.Duration, currentVersion string) string {
	return PassiveCheckDetail(ctx, r, statePath, interval, currentVersion).Line
}

// PassiveCheckDetail runs the passive check and reports everything it learned.
//
// It NEVER returns an error and never blocks longer than ctx allows: a session
// start must not be held up, or fail, because GitHub was unreachable. The
// LastCheck stamp is written whether the request succeeded or failed, so a
// flapping network cannot turn the 24h rate limit into a per-session request.
//
// Cache-hit path (the common one): zero network, one file read — the notice is
// re-derived from the tag recorded by the last check.
func PassiveCheckDetail(ctx context.Context, r TagResolver, statePath string, interval time.Duration, currentVersion string) NoticeResult {
	if NoticeDisabled() {
		return NoticeResult{Disabled: true}
	}
	state := LoadNoticeState(statePath)

	if interval > 0 && !state.LastCheck.IsZero() && time.Since(state.LastCheck) < interval {
		return NoticeResult{
			Line:      NoticeLine(currentVersion, state.LatestTag),
			LatestTag: state.LatestTag,
		}
	}

	tag, err := r.ResolveLatestTag(ctx)
	state.LastCheck = time.Now()
	if err == nil && tag != "" {
		state.LatestTag = tag
	}
	_ = SaveNoticeState(statePath, state)
	if err != nil {
		// Offline is normal and silent.
		return NoticeResult{Checked: true, LatestTag: state.LatestTag}
	}
	return NoticeResult{
		Line:      NoticeLine(currentVersion, tag),
		LatestTag: tag,
		Checked:   true,
	}
}

// NoticeLine renders the notice, or "" when latest is not newer than current.
func NoticeLine(currentVersion, latestTag string) string {
	if !IsNewer(currentVersion, latestTag) {
		return ""
	}
	return fmt.Sprintf("ℹ️  bravros %s is available (you have %s) — run `bravros update`",
		normalizeVersion(latestTag), normalizeVersion(currentVersion))
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "unknown"
	}
	if !strings.HasPrefix(v, "v") {
		return "v" + v
	}
	return v
}

// IsNewer reports whether latest is a strictly newer semver than current.
//
// An unparsable version on either side returns false, which is the quiet
// outcome on purpose: a locally built binary (Version "0.1.0-dev", a commit
// sha, an empty ldflags stamp) must not nag its author on every session, and a
// malformed remote tag must not be treated as an upgrade.
func IsNewer(current, latest string) bool {
	c, ok := parseSemver(current)
	if !ok {
		return false
	}
	l, ok := parseSemver(latest)
	if !ok {
		return false
	}
	for i := range c {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// parseSemver accepts "v1.2.3", "1.2.3", "1.2", and pre-release/build suffixes
// ("1.2.3-rc1", "1.2.3+meta"), which are ignored for ordering.
func parseSemver(s string) ([3]int, bool) {
	var out [3]int
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return out, false
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// ─── package-manager guard ──────────────────────────────────────────────────

// PackageManagerCommand maps an install method recorded in state.json to the
// command that owns upgrades for it. Empty means bravros may replace the binary
// itself.
//
// The method is READ from state.json (`bravros setup` records it at install
// time, cmd/setup.go:139), never re-derived here — but it is absent on every
// pre-v2 install, so callers fall back to detection and pass whatever they got.
func PackageManagerCommand(method string) string {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "brew", "homebrew", "linuxbrew":
		return "brew upgrade bravros"
	case "scoop":
		return "scoop update bravros"
	default:
		return ""
	}
}

// RefusalMessage is what `bravros update` prints instead of replacing a binary
// a package manager owns. Replacing it would leave brew/scoop's metadata
// pointing at a file they no longer control, and the next `brew upgrade` would
// silently roll the user back.
func RefusalMessage(method, upgradeCmd string) string {
	return fmt.Sprintf(
		"bravros was installed with %s — refusing to replace a package-manager-owned binary.\nRun `%s` instead.",
		strings.ToLower(strings.TrimSpace(method)), upgradeCmd)
}
