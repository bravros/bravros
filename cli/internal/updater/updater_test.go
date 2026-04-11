package updater

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSemverLT(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"1.0.0", "1.0.1", true},
		{"v1.0.0", "v1.0.1", true},
		{"1.2.0", "2.0.0", true},
		{"0.9.9", "1.0.0", true},
		{"2.0.0", "1.9.9", false},
		{"1.0.0", "1.0.0", false},
		{"v1.0.0", "1.0.0", false},
		{"1.0.1", "1.0.0", false},
		{"3.0.0", "2.99.99", false},
		// Malformed versions return false (safe default).
		{"abc", "1.0.0", false},
		{"1.0.0", "abc", false},
		{"", "1.0.0", false},
		{"1.0", "1.0.1", false},
		{"1.0.0.0", "1.0.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			got := semverLT(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("semverLT(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestCheck_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("expected User-Agent header")
		}
		release := githubRelease{
			TagName: "v2.0.0",
			Assets: []githubAsset{
				{Name: "bravros-linux-amd64", BrowserDownloadURL: "https://example.com/bravros-linux-amd64"},
				{Name: "bravros-darwin-arm64", BrowserDownloadURL: "https://example.com/bravros-darwin-arm64"},
				{Name: "bravros-darwin-amd64", BrowserDownloadURL: "https://example.com/bravros-darwin-amd64"},
			},
			Body: "changelog here",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(release)
	}))
	defer srv.Close()

	// Clear any existing cache to ensure a fresh network call.
	t.Setenv("HOME", t.TempDir())

	result, err := checkWithURL("v1.0.0", true, srv.URL)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if result.Latest != "2.0.0" {
		t.Errorf("Latest = %q, want %q", result.Latest, "2.0.0")
	}
	if !result.HasUpdate {
		t.Error("HasUpdate should be true")
	}
	if result.IsForced {
		t.Error("IsForced should be false (MIN_VERSION is 0.0.0)")
	}
	if result.URL == "" {
		t.Error("URL should not be empty")
	}
}

func TestCheck_NetworkError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Point to a non-existent server.
	result, err := checkWithURL("v1.0.0", true, "http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
	if result != nil {
		t.Error("result should be nil on network error")
	}
}

func TestCheck_CacheFresh(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Write a fresh cache.
	cacheDir := filepath.Join(tmpHome, ".claude", "cache")
	_ = os.MkdirAll(cacheDir, 0755)
	payload := cachePayload{
		CheckedAt:  time.Now(),
		Latest:     "3.0.0",
		MinVersion: "0.0.0",
		URL:        "https://example.com/binary",
	}
	data, _ := json.Marshal(payload)
	_ = os.WriteFile(filepath.Join(cacheDir, "update-check.json"), data, 0644)

	// Should NOT hit the server (no server running).
	result, err := checkWithURL("v1.0.0", false, "http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("Check() with fresh cache should not error: %v", err)
	}
	if result.Latest != "3.0.0" {
		t.Errorf("Latest = %q, want cached %q", result.Latest, "3.0.0")
	}
	if !result.HasUpdate {
		t.Error("HasUpdate should be true")
	}
}

func TestCheck_CacheStale(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Write a stale cache (25 hours old).
	cacheDir := filepath.Join(tmpHome, ".claude", "cache")
	_ = os.MkdirAll(cacheDir, 0755)
	payload := cachePayload{
		CheckedAt:  time.Now().Add(-25 * time.Hour),
		Latest:     "1.0.0",
		MinVersion: "0.0.0",
		URL:        "https://example.com/old",
	}
	data, _ := json.Marshal(payload)
	_ = os.WriteFile(filepath.Join(cacheDir, "update-check.json"), data, 0644)

	// Should try network call (which will fail), proving cache was stale.
	_, err := checkWithURL("v1.0.0", false, "http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error when cache is stale and server unreachable")
	}
}

func TestCheck_ForceBypassesCache(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Write a fresh cache.
	cacheDir := filepath.Join(tmpHome, ".claude", "cache")
	_ = os.MkdirAll(cacheDir, 0755)
	payload := cachePayload{
		CheckedAt:  time.Now(),
		Latest:     "1.0.0",
		MinVersion: "0.0.0",
		URL:        "https://example.com/old",
	}
	data, _ := json.Marshal(payload)
	_ = os.WriteFile(filepath.Join(cacheDir, "update-check.json"), data, 0644)

	// force=true should bypass cache and hit the server (which will fail).
	_, err := checkWithURL("v1.0.0", true, "http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error when force=true and server unreachable")
	}
}

func TestBuildResult_Flags(t *testing.T) {
	tests := []struct {
		name       string
		current    string
		latest     string
		minVersion string
		wantForced bool
		wantUpdate bool
	}{
		{"up to date", "2.0.0", "2.0.0", "0.0.0", false, false},
		{"update available", "1.0.0", "2.0.0", "0.0.0", false, true},
		{"forced update", "0.5.0", "2.0.0", "1.0.0", true, true},
		{"current equals min", "1.0.0", "2.0.0", "1.0.0", false, true},
		{"ahead of latest", "3.0.0", "2.0.0", "0.0.0", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := buildResult(tt.current, tt.latest, tt.minVersion, "", time.Now())
			if r.IsForced != tt.wantForced {
				t.Errorf("IsForced = %v, want %v", r.IsForced, tt.wantForced)
			}
			if r.HasUpdate != tt.wantUpdate {
				t.Errorf("HasUpdate = %v, want %v", r.HasUpdate, tt.wantUpdate)
			}
		})
	}
}

func TestCacheReadWrite(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	now := time.Now().Truncate(time.Second)
	payload := cachePayload{
		CheckedAt:  now,
		Latest:     "1.5.0",
		MinVersion: "0.0.0",
		URL:        "https://example.com/bin",
	}

	if err := writeCache(payload); err != nil {
		t.Fatalf("writeCache() error = %v", err)
	}

	got, err := readCache()
	if err != nil {
		t.Fatalf("readCache() error = %v", err)
	}

	if got.Latest != "1.5.0" {
		t.Errorf("Latest = %q, want %q", got.Latest, "1.5.0")
	}
	if got.URL != "https://example.com/bin" {
		t.Errorf("URL = %q, want %q", got.URL, "https://example.com/bin")
	}
	// CheckedAt should round-trip correctly (within 1s due to JSON marshaling).
	if got.CheckedAt.Sub(now).Abs() > time.Second {
		t.Errorf("CheckedAt = %v, want ~%v", got.CheckedAt, now)
	}
}

func TestFindAssetURL(t *testing.T) {
	assets := []githubAsset{
		{Name: "bravros-linux-amd64", BrowserDownloadURL: "https://example.com/linux-amd64"},
		{Name: "bravros-darwin-arm64", BrowserDownloadURL: "https://example.com/darwin-arm64"},
		{Name: "bravros-darwin-amd64", BrowserDownloadURL: "https://example.com/darwin-amd64"},
		{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums"},
	}

	url := findAssetURL(assets)
	if url == "" {
		t.Fatal("expected to find a matching asset URL")
	}
	// The URL should match the current runtime OS/ARCH.
	t.Logf("found asset URL for %s/%s: %s", "runtime.GOOS", "runtime.GOARCH", url)
}
