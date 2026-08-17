package selfupdate

// autoupdate_test.go — the auto-update lane's POLICY. Every gate that decides
// whether an unattended machine may replace its own binary is pinned here as a
// table, because the cost of getting one of them wrong is a fleet-wide bad
// binary, not a failing command.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func boolPtr(b bool) *bool { return &b }

func TestDecideAuto(t *testing.T) {
	cases := []struct {
		name string
		in   AutoInput
		want AutoAction
	}{
		{
			name: "installer-owned with a newer release swaps",
			in:   AutoInput{CurrentVersion: "v1.0.0", LatestTag: "v1.1.0", ObservedMethod: "installer", RecordedMethod: "installer"},
			want: AutoSwap,
		},
		{
			name: "installer-owned with no recorded method still swaps",
			in:   AutoInput{CurrentVersion: "v1.0.0", LatestTag: "v1.1.0", ObservedMethod: "installer"},
			want: AutoSwap,
		},
		{
			name: "already current says nothing",
			in:   AutoInput{CurrentVersion: "v1.1.0", LatestTag: "v1.1.0", ObservedMethod: "installer"},
			want: AutoNone,
		},
		{
			name: "brew-owned binary is never swapped",
			in:   AutoInput{CurrentVersion: "v1.0.0", LatestTag: "v1.1.0", ObservedMethod: "brew"},
			want: AutoNotify,
		},
		{
			name: "scoop-owned binary is never swapped",
			in:   AutoInput{CurrentVersion: "v1.0.0", LatestTag: "v1.1.0", ObservedMethod: "scoop"},
			want: AutoNotify,
		},
		{
			// The explicit `bravros update` lets observed reality overrule a
			// stale brew record; the UNATTENDED lane deliberately does not.
			name: "a brew record vetoes the swap even when the path says installer",
			in:   AutoInput{CurrentVersion: "v1.0.0", LatestTag: "v1.1.0", ObservedMethod: "installer", RecordedMethod: "brew"},
			want: AutoNotify,
		},
		{
			name: "auto_update false disables the whole lane",
			in:   AutoInput{CurrentVersion: "v1.0.0", LatestTag: "v1.1.0", ObservedMethod: "installer", RecordedMethod: "installer", AutoUpdate: boolPtr(false)},
			want: AutoNotify,
		},
		{
			name: "auto_update true is the same as absent",
			in:   AutoInput{CurrentVersion: "v1.0.0", LatestTag: "v1.1.0", ObservedMethod: "installer", AutoUpdate: boolPtr(true)},
			want: AutoSwap,
		},
		{
			name: "a locally built binary is left alone",
			in:   AutoInput{CurrentVersion: "v1.0.0", LatestTag: "v1.1.0", ObservedMethod: "source"},
			want: AutoNotify,
		},
		{
			name: "an unknown owner is left alone",
			in:   AutoInput{CurrentVersion: "v1.0.0", LatestTag: "v1.1.0", ObservedMethod: "unknown"},
			want: AutoNotify,
		},
		{
			name: "a source record vetoes an installer-looking path",
			in:   AutoInput{CurrentVersion: "v1.0.0", LatestTag: "v1.1.0", ObservedMethod: "installer", RecordedMethod: "source"},
			want: AutoNotify,
		},
		{
			name: "an unparsable local version never counts as behind",
			in:   AutoInput{CurrentVersion: "dev", LatestTag: "v1.1.0", ObservedMethod: "installer"},
			want: AutoNone,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DecideAuto(c.in)
			if got.Action != c.want {
				t.Errorf("DecideAuto(%+v) = %q (%s), want %q", c.in, got.Action, got.Reason, c.want)
			}
			if got.Reason == "" {
				t.Error("every decision must carry a reason")
			}
		})
	}
}

// TestCanaryVerdict — the young-release guard, including the direction it fails
// in: an age it cannot establish must cost a swap, never grant one.
func TestCanaryVerdict(t *testing.T) {
	if got := CanaryVerdict(30*time.Minute, nil, MinReleaseAge); got.Action != AutoNotify {
		t.Errorf("a 30m-old release must be deferred, got %q (%s)", got.Action, got.Reason)
	}
	if got := CanaryVerdict(9*time.Hour, nil, MinReleaseAge); got.Action != AutoSwap {
		t.Errorf("a 9h-old release must be installable, got %q (%s)", got.Action, got.Reason)
	}
	if got := CanaryVerdict(0, errors.New("no Last-Modified header"), MinReleaseAge); got.Action != AutoNotify {
		t.Errorf("an unknown age must fail open to notify, got %q (%s)", got.Action, got.Reason)
	}
	// minAge 0 disables the canary — the seam tests use to be deterministic.
	if got := CanaryVerdict(time.Minute, nil, 0); got.Action != AutoSwap {
		t.Errorf("a disabled canary must not defer, got %q (%s)", got.Action, got.Reason)
	}
}

func TestReleaseAgeFloor_EnvContract(t *testing.T) {
	cases := map[string]time.Duration{
		"":        MinReleaseAge,
		"0":       0,
		"2h":      2 * time.Hour,
		"garbage": MinReleaseAge, // a typo must not remove the guard
		"-1h":     MinReleaseAge,
	}
	for raw, want := range cases {
		t.Setenv(MinReleaseAgeEnv, raw)
		if got := ReleaseAgeFloor(); got != want {
			t.Errorf("%s=%q: got %v, want %v", MinReleaseAgeEnv, raw, got, want)
		}
	}
}

// TestPreservePrevious keeps exactly ONE generation: the second swap's .prev is
// the binary that was running before it, not the original.
func TestPreservePrevious(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "bravros")
	if err := os.WriteFile(exe, []byte("v1"), 0o755); err != nil {
		t.Fatalf("seed exe: %v", err)
	}

	prev, err := PreservePrevious(exe)
	if err != nil {
		t.Fatalf("PreservePrevious: %v", err)
	}
	if prev != filepath.Join(dir, "bravros.prev") {
		t.Errorf("preserved at %q, want %q", prev, filepath.Join(dir, "bravros.prev"))
	}
	if got, _ := os.ReadFile(prev); string(got) != "v1" {
		t.Errorf(".prev holds %q, want the outgoing binary %q", got, "v1")
	}
	info, err := os.Stat(prev)
	if err != nil {
		t.Fatalf("stat .prev: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf(".prev must stay executable to be a rollback, got mode %v", info.Mode())
	}

	// Second generation overwrites the first.
	if err := os.WriteFile(exe, []byte("v2"), 0o755); err != nil {
		t.Fatalf("rewrite exe: %v", err)
	}
	if _, err := PreservePrevious(exe); err != nil {
		t.Fatalf("second PreservePrevious: %v", err)
	}
	if got, _ := os.ReadFile(prev); string(got) != "v2" {
		t.Errorf("only one generation is kept; .prev holds %q, want %q", got, "v2")
	}
	// No stray temp files left beside the binary.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 2 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected exactly bravros + bravros.prev, got %v", names)
	}
}

// TestAssetAger_ReleaseAge — the no-API age probe: a HEAD on the release asset,
// Last-Modified read off the response. The GitHub API is never touched.
func TestAssetAger_ReleaseAge(t *testing.T) {
	published := time.Now().Add(-8 * time.Hour).UTC()
	method, path := "", ""
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.Header().Set("Last-Modified", published.Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	ager := &AssetAger{BaseURL: ts.URL, HTTP: ts.Client()}
	age, err := ager.ReleaseAge(context.Background(), "v9.9.9")
	if err != nil {
		t.Fatalf("ReleaseAge: %v", err)
	}
	if age < 7*time.Hour || age > 9*time.Hour {
		t.Errorf("age %v, want ~8h", age)
	}
	if method != http.MethodHead {
		t.Errorf("probe used %s, want HEAD — a GET would download the asset", method)
	}
	if want := "/releases/download/v9.9.9/" + ChecksumsAsset; path != want {
		t.Errorf("probed %q, want %q", path, want)
	}
}

func TestAssetAger_MissingHeaderIsAnError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header()["Last-Modified"] = nil
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	if _, err := (&AssetAger{BaseURL: ts.URL, HTTP: ts.Client()}).ReleaseAge(context.Background(), "v9.9.9"); err == nil {
		t.Error("a response without Last-Modified must be an error, so the canary fails open to notify")
	}
}

func TestAssetAger_NotFoundIsAnError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	if _, err := (&AssetAger{BaseURL: ts.URL, HTTP: ts.Client()}).ReleaseAge(context.Background(), "v9.9.9"); err == nil {
		t.Error("a 404 must be an error, not an age of zero")
	}
}

func TestSwapLine(t *testing.T) {
	if got, want := SwapLine("0.1.0", "v1.2.3"), "🔄 bravros v0.1.0 → v1.2.3 (auto)"; got != want {
		t.Errorf("SwapLine = %q, want %q", got, want)
	}
}

// TestPassiveCheckDetail_ReportsTheTagAndWhetherItChecked — the auto lane gates
// the swap on Checked, so a cache hit must report false while still carrying
// the cached tag.
func TestPassiveCheckDetail_ReportsTheTagAndWhetherItChecked(t *testing.T) {
	t.Setenv(NoUpdateCheckEnv, "")
	statePath := filepath.Join(t.TempDir(), "state", "notice.json")
	r := &countingResolver{tag: "v9.9.9"}

	first := PassiveCheckDetail(context.Background(), r, statePath, 24*time.Hour, "v1.0.0")
	if !first.Checked || first.LatestTag != "v9.9.9" {
		t.Fatalf("first run: %+v, want Checked with tag v9.9.9", first)
	}
	second := PassiveCheckDetail(context.Background(), r, statePath, 24*time.Hour, "v1.0.0")
	if second.Checked {
		t.Error("a cache hit must report Checked=false so the lane does not swap on it")
	}
	if second.LatestTag != "v9.9.9" || second.Line == "" {
		t.Errorf("a cache hit must still carry the cached tag and notice: %+v", second)
	}
	if r.calls != 1 {
		t.Errorf("the cadence must stay one request per interval, got %d", r.calls)
	}
}

func TestPassiveCheckDetail_OptOutIsReported(t *testing.T) {
	t.Setenv(NoUpdateCheckEnv, "1")
	res := PassiveCheckDetail(context.Background(), &countingResolver{tag: "v9.9.9"},
		filepath.Join(t.TempDir(), "notice.json"), 24*time.Hour, "v1.0.0")
	if !res.Disabled || res.Checked || res.LatestTag != "" {
		t.Errorf("opt-out must short-circuit everything, got %+v", res)
	}
}
