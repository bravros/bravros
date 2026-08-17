package selfupdate

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// These tests reuse countingResolver from remote_test.go — the same fake the
// payload lane's rate-limit test uses, so both rate limits are measured the
// same way: by counting resolver calls, never by an exit code.

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v1.2.3", "v1.2.4", true},
		{"v1.2.3", "v1.3.0", true},
		{"v1.2.3", "v2.0.0", true},
		{"1.2.3", "v1.2.3", false},
		{"v1.2.3", "v1.2.2", false},
		{"v1.10.0", "v1.9.0", false}, // numeric, not lexicographic
		{"v1.9.0", "v1.10.0", true},
		{"v1.2", "v1.2.1", true},
		{"v1.2.3", "v1.2.4-rc1", true},
		// Unparsable on either side is quiet, never an "upgrade".
		{"dev", "v1.2.3", false},
		{"v1.2.3", "nightly", false},
		{"", "v1.2.3", false},
	}
	for _, c := range cases {
		if got := IsNewer(c.current, c.latest); got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

// TestPassiveCheck_OneRequestPerInterval is the rate-limit contract: however
// many sessions start inside the interval, the remote is contacted once.
func TestPassiveCheck_OneRequestPerInterval(t *testing.T) {
	t.Setenv(NoUpdateCheckEnv, "")
	statePath := filepath.Join(t.TempDir(), "state", "notice.json")
	r := &countingResolver{tag: "v9.9.9"}

	for i := 0; i < 5; i++ {
		line := PassiveCheck(context.Background(), r, statePath, 24*time.Hour, "v1.0.0")
		if line == "" {
			t.Fatalf("run %d: expected a notice for v9.9.9 over v1.0.0", i)
		}
	}
	if r.calls != 1 {
		t.Errorf("5 runs inside the interval made %d remote requests, want 1", r.calls)
	}
}

// TestPassiveCheck_FailureStillStampsTheInterval — a flapping network must not
// turn the 24h rate limit into a request every session.
func TestPassiveCheck_FailureStillStampsTheInterval(t *testing.T) {
	t.Setenv(NoUpdateCheckEnv, "")
	statePath := filepath.Join(t.TempDir(), "state", "notice.json")
	r := &countingResolver{err: errors.New("dial tcp: network is unreachable")}

	for i := 0; i < 3; i++ {
		if line := PassiveCheck(context.Background(), r, statePath, 24*time.Hour, "v1.0.0"); line != "" {
			t.Errorf("offline must be silent, got %q", line)
		}
	}
	if r.calls != 1 {
		t.Errorf("offline runs made %d remote requests, want 1", r.calls)
	}
}

func TestPassiveCheck_OptOutMakesNoRequest(t *testing.T) {
	t.Setenv(NoUpdateCheckEnv, "1")
	statePath := filepath.Join(t.TempDir(), "notice.json")
	r := &countingResolver{tag: "v9.9.9"}

	if line := PassiveCheck(context.Background(), r, statePath, 24*time.Hour, "v1.0.0"); line != "" {
		t.Errorf("opt-out must print nothing, got %q", line)
	}
	if r.calls != 0 {
		t.Errorf("opt-out must make no remote request, got %d", r.calls)
	}
}

func TestPassiveCheck_NoNoticeWhenCurrent(t *testing.T) {
	t.Setenv(NoUpdateCheckEnv, "")
	statePath := filepath.Join(t.TempDir(), "notice.json")
	r := &countingResolver{tag: "v1.0.0"}

	if line := PassiveCheck(context.Background(), r, statePath, 24*time.Hour, "v1.0.0"); line != "" {
		t.Errorf("an up-to-date binary must stay silent, got %q", line)
	}
	if r.calls != 1 {
		t.Errorf("first run must contact the remote once, got %d", r.calls)
	}
}

func TestNoticeInterval_EnvContract(t *testing.T) {
	cases := map[string]time.Duration{
		"":        DefaultNoticeInterval,
		"0":       0,
		"1h":      time.Hour,
		"garbage": DefaultNoticeInterval,
		"-5m":     DefaultNoticeInterval,
	}
	for raw, want := range cases {
		t.Setenv(NoticeIntervalEnv, raw)
		if got := NoticeInterval(); got != want {
			t.Errorf("%s=%q: got %v, want %v", NoticeIntervalEnv, raw, got, want)
		}
	}
}

func TestPackageManagerCommand(t *testing.T) {
	cases := map[string]string{
		"brew":      "brew upgrade bravros",
		"Homebrew":  "brew upgrade bravros",
		"linuxbrew": "brew upgrade bravros",
		"scoop":     "scoop update bravros",
		"SCOOP":     "scoop update bravros",
		"installer": "",
		"source":    "",
		"unknown":   "",
		"":          "",
	}
	for method, want := range cases {
		if got := PackageManagerCommand(method); got != want {
			t.Errorf("PackageManagerCommand(%q) = %q, want %q", method, got, want)
		}
	}
}
