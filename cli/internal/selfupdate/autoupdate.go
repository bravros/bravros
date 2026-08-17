package selfupdate

// autoupdate.go — the POLICY half of the SessionStart auto-update lane
// (P-0018 Phase 6, decision D2).
//
// The 24h redirect check in notice.go used to end at a printed line. D2 turns
// it into a TRIGGER for exactly one population: binaries install.sh owns, under
// <claude root>/bin. Everything else — brew, scoop, a locally built binary, an
// install whose owner cannot be established — keeps the old notify-only
// behavior, unchanged.
//
// This file decides; cmd/selfupdate.go executes. Keeping the decision here and
// pure is what makes "a brew binary is refused BEFORE anything is downloaded"
// a table test rather than an integration test with a fake HTTP server.
//
// THE FLEET RISK IS THE WHOLE DESIGN. Once the trigger exists, a bad release
// reaches every installer-owned machine within 24h with nobody typing anything.
// Two guards ship in the same phase and neither is optional:
//
//   - the canary window (CanaryVerdict): a release younger than MinReleaseAge
//     is deferred to the next check, so a release yanked in its first hours
//     never reaches most of the fleet;
//   - the previous binary (PreservePrevious): one generation kept beside the
//     new one as bravros.prev, so a rollback is a `mv`, not a reinstall.
//
// EVERY UNKNOWN FAILS OPEN TO NOTIFY, never to swap. An unreadable setup.json,
// an unresolvable install method, a release whose age cannot be determined —
// each of those produces the notice the operator has been getting all along.
// The costly outcome of failing to swap is a stale binary; the costly outcome
// of swapping blind is a broken one.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// MinReleaseAge is the canary window: a release published less than this
	// ago is not auto-installed. ~6h is long enough for a yanked release to be
	// pulled before the fleet takes it, short enough that a good release still
	// lands the same day.
	MinReleaseAge = 6 * time.Hour

	// MinReleaseAgeEnv overrides the canary window (Go duration; "0" disables
	// the canary, which is what a test that wants a deterministic swap uses).
	MinReleaseAgeEnv = "BRAVROS_MIN_RELEASE_AGE"

	// PrevBinarySuffix names the one preserved generation: "bravros" →
	// "bravros.prev". One generation only — every swap overwrites it.
	PrevBinarySuffix = ".prev"

	// InstallerMethod is the only install method the auto lane will swap.
	// It means install.sh put the binary under <claude root>/bin
	// (cmd/setup.go detectInstallMethod).
	InstallerMethod = "installer"
)

// ReleaseAgeFloor is the effective canary window, honouring MinReleaseAgeEnv.
// A garbage or negative value falls back to the default rather than disabling
// the guard — a typo must not silently remove the fleet's protection.
func ReleaseAgeFloor() time.Duration {
	raw := strings.TrimSpace(os.Getenv(MinReleaseAgeEnv))
	if raw == "" {
		return MinReleaseAge
	}
	if raw == "0" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return MinReleaseAge
	}
	return d
}

// AutoAction is what the passive lane concluded.
type AutoAction string

const (
	// AutoNone: nothing to say — the running binary is current (or the check
	// produced no usable tag).
	AutoNone AutoAction = "none"
	// AutoNotify: a newer release exists but this machine must not swap
	// itself. Print the notice, exactly as before D2.
	AutoNotify AutoAction = "notify"
	// AutoSwap: download, verify and replace the binary.
	AutoSwap AutoAction = "swap"
)

// AutoInput is everything the eligibility decision reads. Release age is NOT
// here on purpose: establishing it costs a network round trip, and it is only
// worth paying once every other gate has passed. See CanaryVerdict.
type AutoInput struct {
	// CurrentVersion is the running binary's version stamp.
	CurrentVersion string
	// LatestTag is what the 24h check resolved.
	LatestTag string
	// ObservedMethod is the install method derived from the resolved path of
	// the binary that would be replaced. This is the authoritative one: it
	// describes the actual file on disk.
	ObservedMethod string
	// RecordedMethod is setup.json's install_method ("" when absent).
	RecordedMethod string
	// AutoUpdate is setup.json's auto_update field: nil when absent (default
	// on for installer-owned binaries), false to disable the whole auto lane.
	AutoUpdate *bool
}

// AutoDecision is a verdict plus the reason, which callers print under
// --verbose and tests assert on instead of reverse-engineering behavior.
type AutoDecision struct {
	Action AutoAction
	Reason string
}

// DecideAuto answers "may this machine replace its own binary?" — everything
// except the release-age canary, which is applied afterwards by CanaryVerdict.
//
// ORDER IS THE CONTRACT. The package-manager refusal is evaluated before any
// step that could download or replace anything (P-0018 trap: "the
// package-manager refusal must be checked BEFORE the swap, not after"), and it
// is evaluated against BOTH the observed path and setup.json's record. The
// explicit `bravros update` lets observed reality overrule a stale brew record
// (cmd/update.go updateOwnershipGuard) because an operator asked for it by
// name; the AUTOMATIC lane does not — an unattended swap on a machine whose own
// record says brew is exactly the situation to stay out of.
func DecideAuto(in AutoInput) AutoDecision {
	if !IsNewer(in.CurrentVersion, in.LatestTag) {
		return AutoDecision{AutoNone, "already on the newest published release"}
	}

	observed := strings.ToLower(strings.TrimSpace(in.ObservedMethod))
	recorded := strings.ToLower(strings.TrimSpace(in.RecordedMethod))

	if cmd := PackageManagerCommand(observed); cmd != "" {
		return AutoDecision{AutoNotify, fmt.Sprintf("%s owns this binary — run `%s`", observed, cmd)}
	}
	if cmd := PackageManagerCommand(recorded); cmd != "" {
		return AutoDecision{AutoNotify, fmt.Sprintf("setup.json records install_method %q — run `%s`", recorded, cmd)}
	}
	if in.AutoUpdate != nil && !*in.AutoUpdate {
		return AutoDecision{AutoNotify, "auto_update is false in setup.json"}
	}
	if observed != InstallerMethod {
		return AutoDecision{AutoNotify, fmt.Sprintf("the binary is not installer-owned (detected %q)", observed)}
	}
	if recorded != "" && recorded != InstallerMethod {
		return AutoDecision{AutoNotify, fmt.Sprintf("setup.json records install_method %q, not %q", recorded, InstallerMethod)}
	}
	return AutoDecision{AutoSwap, "installer-owned binary with a newer release published"}
}

// CanaryVerdict applies the young-release guard once DecideAuto has returned
// AutoSwap. err != nil means the age could not be established — that FAILS
// OPEN TO NOTIFY, never to swapping: the guard exists precisely for the
// releases nobody has vetted yet, so losing it must cost an automatic install,
// not grant one.
func CanaryVerdict(age time.Duration, err error, minAge time.Duration) AutoDecision {
	if err != nil {
		return AutoDecision{AutoNotify, fmt.Sprintf("release age unknown (%v) — not swapping blind", err)}
	}
	if minAge > 0 && age < minAge {
		return AutoDecision{AutoNotify, fmt.Sprintf("release is %v old, inside the %v canary window — deferred to the next check",
			age.Round(time.Minute), minAge)}
	}
	return AutoDecision{AutoSwap, fmt.Sprintf("release is %v old, past the %v canary window", age.Round(time.Minute), minAge)}
}

// SwapLine is the ONE loud line a swap prints. Everything else about the auto
// lane is silent by design; this is the single thing an operator must be able
// to find in a scrollback when a version changed under them.
func SwapLine(from, to string) string {
	return fmt.Sprintf("🔄 bravros %s → %s (auto)", normalizeVersion(from), normalizeVersion(to))
}

// ─── previous-generation preservation ───────────────────────────────────────

// PrevBinaryPath is where PreservePrevious keeps the outgoing executable.
func PrevBinaryPath(exePath string) string {
	return exePath + PrevBinarySuffix
}

// PreservePrevious copies the executable at exePath to "<exePath>.prev" before
// it is replaced, and returns that path.
//
// COPY, not rename — deliberately. ReplaceExecutable's POSIX fast path is
// rename(staged, exePath), which needs exePath to still be there; moving the
// old file aside first would push every platform onto the Windows aside-dance
// and manufacture a window with no binary at exePath (binary.go documents why
// that is strictly worse). Copying costs one file's worth of I/O and keeps the
// swap itself atomic.
//
// ONE GENERATION. The write goes to a temp file in the same directory and is
// renamed over any existing .prev, so a crash mid-copy cannot leave a truncated
// rollback target where a working one used to be.
func PreservePrevious(exePath string) (string, error) {
	if strings.TrimSpace(exePath) == "" {
		return "", fmt.Errorf("no executable path to preserve")
	}
	prev := PrevBinaryPath(exePath)
	tmp := filepath.Join(filepath.Dir(exePath), "."+filepath.Base(exePath)+".prev-"+randomSuffix())
	if err := copyFileMode(exePath, tmp, 0o755); err != nil {
		return "", fmt.Errorf("preserve the current binary: %w", err)
	}
	if err := os.Rename(tmp, prev); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("install %s: %w", prev, err)
	}
	return prev, nil
}

// ─── release age, without the GitHub API ────────────────────────────────────

// ReleaseAger reports how long ago a release tag was published. Consumer-side
// interface, same shape as TagResolver: the cmd layer swaps a fake in.
type ReleaseAger interface {
	ReleaseAge(ctx context.Context, tag string) (time.Duration, error)
}

// AssetAger derives a release's age from the Last-Modified header of one of
// its assets.
//
// WHY NOT api.github.com/releases/latest, which carries published_at directly:
// the unauthenticated API allows ~60 requests/hour PER IP, shared by every tool
// on the machine and by every machine behind one NAT. The whole update path
// (install.sh, fetch.ResolveLatestTag) already refuses to spend that budget —
// the redirect on /releases/latest is the house technique — and the auto lane
// firing on every SessionStart is the last place to start.
//
// A HEAD on the asset download URL costs nothing from that budget: GitHub
// redirects it to the object store, which answers with the asset's
// Last-Modified — the moment the release's assets were uploaded, which is the
// moment the release became installable. That is the number the canary wants.
// It is a bound, not a timestamp of record: re-uploading an asset would make a
// release look younger, which errs toward NOT auto-installing.
//
// checksums.txt is the asset chosen because it exists for every release and
// every platform, and is the smallest of them.
type AssetAger struct {
	BaseURL string
	HTTP    *http.Client
	// Asset overrides which release asset is probed (defaults to checksums.txt).
	Asset string
}

func (a *AssetAger) asset() string {
	if strings.TrimSpace(a.Asset) != "" {
		return a.Asset
	}
	return ChecksumsAsset
}

func (a *AssetAger) baseURL() string {
	if a.BaseURL != "" {
		return strings.TrimRight(a.BaseURL, "/")
	}
	return defaultBaseURL
}

func (a *AssetAger) httpClient() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// ReleaseAge HEADs the release asset and reads Last-Modified. Any failure —
// transport, non-200, missing or unparsable header — is returned as an error,
// which CanaryVerdict turns into notify-only.
func (a *AssetAger) ReleaseAge(ctx context.Context, tag string) (time.Duration, error) {
	url := fmt.Sprintf("%s/releases/download/%s/%s", a.baseURL(), strings.TrimSpace(tag), a.asset())
	if strings.TrimSpace(tag) == "" {
		url = fmt.Sprintf("%s/releases/latest/download/%s", a.baseURL(), a.asset())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	resp, err := a.httpClient().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, url)
	}
	raw := resp.Header.Get("Last-Modified")
	if raw == "" {
		return 0, fmt.Errorf("no Last-Modified header for %s", url)
	}
	published, err := http.ParseTime(raw)
	if err != nil {
		return 0, fmt.Errorf("parse Last-Modified %q: %w", raw, err)
	}
	age := time.Since(published)
	if age < 0 {
		// Clock skew between this machine and the object store. Treat it as
		// brand new, which the canary then defers.
		return 0, nil
	}
	return age, nil
}
