package updater

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// MIN_VERSION is the minimum supported CLI version. If the running binary
// is older than this, the updater will force an update. Bump this constant
// manually before publishing a breaking release.
const MIN_VERSION = "0.0.0"

// cacheTTL is how long a cached check result is considered fresh.
const cacheTTL = 24 * time.Hour

// defaultTimeout for HTTP requests to GitHub API.
const defaultTimeout = 5 * time.Second

// releasesURL is the GitHub Releases API endpoint. Exported for testing.
var releasesURL = "https://api.github.com/repos/bravros/bravros/releases/latest"

// CheckResult holds the outcome of a version check.
type CheckResult struct {
	Latest     string    `json:"latest"`
	MinVersion string    `json:"min_version"`
	URL        string    `json:"url"`
	CachedAt   time.Time `json:"cached_at"`
	IsForced   bool      `json:"is_forced"`
	HasUpdate  bool      `json:"has_update"`
}

// cachePayload is the on-disk JSON format for the update cache file.
type cachePayload struct {
	CheckedAt  time.Time `json:"checked_at"`
	Latest     string    `json:"latest"`
	MinVersion string    `json:"min_version"`
	URL        string    `json:"url"`
}

// githubRelease represents the relevant fields from the GitHub Releases API.
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
	Body    string        `json:"body"`
}

// githubAsset represents a single release asset.
type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Check queries GitHub Releases for the latest version, using a 24h on-disk
// cache to avoid excessive network calls. If force is true, the cache is
// bypassed. Returns nil on network errors (never blocks CLI usage).
func Check(currentVersion string, force bool) (*CheckResult, error) {
	return checkWithURL(currentVersion, force, releasesURL)
}

// checkWithURL is the internal implementation that accepts a configurable URL
// for testing purposes.
func checkWithURL(currentVersion string, force bool, apiURL string) (*CheckResult, error) {
	// Try cache first (unless forced).
	if !force {
		if cached, err := readCache(); err == nil {
			if time.Since(cached.CheckedAt) < cacheTTL {
				return buildResult(currentVersion, cached.Latest, cached.MinVersion, cached.URL, cached.CheckedAt), nil
			}
		}
	}

	// Fetch from GitHub Releases API.
	client := &http.Client{Timeout: defaultTimeout}
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "bravros-cli/"+strings.TrimPrefix(currentVersion, "v"))
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	assetURL := findAssetURL(release.Assets)

	// Write cache.
	now := time.Now()
	_ = writeCache(cachePayload{
		CheckedAt:  now,
		Latest:     latest,
		MinVersion: MIN_VERSION,
		URL:        assetURL,
	})

	return buildResult(currentVersion, latest, MIN_VERSION, assetURL, now), nil
}

// buildResult constructs a CheckResult from raw values.
func buildResult(currentVersion, latest, minVersion, url string, cachedAt time.Time) *CheckResult {
	cv := strings.TrimPrefix(currentVersion, "v")
	return &CheckResult{
		Latest:     latest,
		MinVersion: minVersion,
		URL:        url,
		CachedAt:   cachedAt,
		IsForced:   semverLT(cv, minVersion),
		HasUpdate:  semverLT(cv, latest),
	}
}

// findAssetURL picks the correct binary asset for the current OS/ARCH.
func findAssetURL(assets []githubAsset) string {
	osName := runtime.GOOS
	archName := runtime.GOARCH
	// Map Go arch names to the naming convention used in releases.
	if archName == "amd64" {
		archName = "amd64"
	}
	if archName == "arm64" {
		archName = "arm64"
	}

	target := fmt.Sprintf("bravros-%s-%s", osName, archName)
	for _, a := range assets {
		if strings.Contains(a.Name, target) {
			return a.BrowserDownloadURL
		}
	}
	// Fallback: return empty string if no matching asset found.
	return ""
}

// cachePath returns the path to the update cache file.
func cachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "cache", "update-check.json")
}

// readCache reads the cached check result from disk.
func readCache() (*cachePayload, error) {
	p := cachePath()
	if p == "" {
		return nil, fmt.Errorf("no home dir")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var payload cachePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// writeCache writes the check result to disk.
func writeCache(payload cachePayload) error {
	p := cachePath()
	if p == "" {
		return fmt.Errorf("no home dir")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0644)
}

// semverLT returns true if version a is strictly less than version b.
// Both versions may optionally have a "v" prefix. Malformed versions
// return false (safe default — never force an update on bad input).
func semverLT(a, b string) bool {
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")

	aParts := strings.SplitN(a, ".", 3)
	bParts := strings.SplitN(b, ".", 3)

	if len(aParts) != 3 || len(bParts) != 3 {
		return false
	}

	for i := 0; i < 3; i++ {
		ai, errA := strconv.Atoi(aParts[i])
		bi, errB := strconv.Atoi(bParts[i])
		if errA != nil || errB != nil {
			return false
		}
		if ai < bi {
			return true
		}
		if ai > bi {
			return false
		}
	}
	return false // equal
}
