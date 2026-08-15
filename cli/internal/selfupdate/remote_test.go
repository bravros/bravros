package selfupdate

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// countingResolver is a fake TagResolver that counts how many times
// ResolveLatestTag is invoked, and optionally returns an error instead of a tag.
type countingResolver struct {
	calls int
	tag   string
	err   error
}

func (c *countingResolver) ResolveLatestTag(ctx context.Context) (string, error) {
	c.calls++
	if c.err != nil {
		return "", c.err
	}
	return c.tag, nil
}

// TestRemoteCheckRateLimit is the plan's named acceptance test: two CheckRemote
// calls inside the TTL must produce exactly one resolver call.
func TestRemoteCheckRateLimit(t *testing.T) {
	t.Setenv("BRAVROS_CONFIG_DIR", t.TempDir())
	statePath := filepath.Join(t.TempDir(), "remote-check.json")
	resolver := &countingResolver{tag: "v1.2.3"}

	res1, err := CheckRemote(context.Background(), resolver, statePath, time.Hour, "v1.2.2")
	if err != nil {
		t.Fatalf("first CheckRemote: %v", err)
	}
	if !res1.Checked {
		t.Fatalf("first call: expected Checked=true, got %+v", res1)
	}
	if resolver.calls != 1 {
		t.Fatalf("expected 1 resolver call after first CheckRemote, got %d", resolver.calls)
	}

	res2, err := CheckRemote(context.Background(), resolver, statePath, time.Hour, "v1.2.2")
	if err != nil {
		t.Fatalf("second CheckRemote: %v", err)
	}
	if res2.Checked {
		t.Fatalf("second call within TTL: expected Checked=false, got %+v", res2)
	}
	if resolver.calls != 1 {
		t.Fatalf("expected still 1 resolver call after second (cached) CheckRemote, got %d", resolver.calls)
	}
}

// TestRemoteCheckAfterTTLElapsed verifies a second CheckRemote call after the
// TTL window has passed (simulated by backdating LastCheck in the state file)
// makes a fresh resolver call.
func TestRemoteCheckAfterTTLElapsed(t *testing.T) {
	t.Setenv("BRAVROS_CONFIG_DIR", t.TempDir())
	statePath := filepath.Join(t.TempDir(), "remote-check.json")
	resolver := &countingResolver{tag: "v1.2.3"}

	if _, err := CheckRemote(context.Background(), resolver, statePath, time.Hour, "v1.2.2"); err != nil {
		t.Fatalf("first CheckRemote: %v", err)
	}
	if resolver.calls != 1 {
		t.Fatalf("expected 1 resolver call, got %d", resolver.calls)
	}

	// Backdate LastCheck beyond the TTL.
	state, err := LoadRemoteState(statePath)
	if err != nil {
		t.Fatalf("LoadRemoteState: %v", err)
	}
	state.LastCheck = time.Now().Add(-2 * time.Hour)
	if err := SaveRemoteState(statePath, state); err != nil {
		t.Fatalf("SaveRemoteState: %v", err)
	}

	res, err := CheckRemote(context.Background(), resolver, statePath, time.Hour, "v1.2.2")
	if err != nil {
		t.Fatalf("second CheckRemote: %v", err)
	}
	if !res.Checked {
		t.Fatalf("expected Checked=true after TTL elapsed, got %+v", res)
	}
	if resolver.calls != 2 {
		t.Fatalf("expected 2 resolver calls after TTL elapsed, got %d", resolver.calls)
	}
}

// TestRemoteCheckZeroTTLDisablesRateLimit verifies ttl == 0 checks on every call.
func TestRemoteCheckZeroTTLDisablesRateLimit(t *testing.T) {
	t.Setenv("BRAVROS_CONFIG_DIR", t.TempDir())
	statePath := filepath.Join(t.TempDir(), "remote-check.json")
	resolver := &countingResolver{tag: "v1.2.3"}

	for i := 0; i < 3; i++ {
		if _, err := CheckRemote(context.Background(), resolver, statePath, 0, "v1.2.2"); err != nil {
			t.Fatalf("CheckRemote call %d: %v", i, err)
		}
	}
	if resolver.calls != 3 {
		t.Fatalf("expected 3 resolver calls with ttl=0, got %d", resolver.calls)
	}
}

// TestRemoteCheckOfflineDoesNotError verifies a resolver error yields
// Offline: true, Behind: false, a nil error, and still advances LastCheck so a
// flapping network cannot bypass the rate limit.
func TestRemoteCheckOfflineDoesNotError(t *testing.T) {
	t.Setenv("BRAVROS_CONFIG_DIR", t.TempDir())
	statePath := filepath.Join(t.TempDir(), "remote-check.json")
	resolver := &countingResolver{err: errors.New("network unreachable")}

	res, err := CheckRemote(context.Background(), resolver, statePath, time.Hour, "v1.2.2")
	if err != nil {
		t.Fatalf("expected nil error on resolver failure, got %v", err)
	}
	if !res.Offline {
		t.Fatalf("expected Offline=true, got %+v", res)
	}
	if res.Behind {
		t.Fatalf("expected Behind=false on offline result, got %+v", res)
	}
	if resolver.calls != 1 {
		t.Fatalf("expected 1 resolver call, got %d", resolver.calls)
	}

	// A second immediate call must be suppressed by the TTL (LastCheck advanced
	// on the failed attempt too), so the resolver call count must not increase.
	res2, err := CheckRemote(context.Background(), resolver, statePath, time.Hour, "v1.2.2")
	if err != nil {
		t.Fatalf("second CheckRemote: %v", err)
	}
	if res2.Checked {
		t.Fatalf("expected second call to be suppressed by TTL, got %+v", res2)
	}
	if resolver.calls != 1 {
		t.Fatalf("expected resolver call count to stay at 1, got %d", resolver.calls)
	}
}

// TestRemoteCheckEmptyInstalledTagIsBehind verifies an empty installedTag
// (no payload ever fetched) is always reported Behind when a remote tag exists.
func TestRemoteCheckEmptyInstalledTagIsBehind(t *testing.T) {
	t.Setenv("BRAVROS_CONFIG_DIR", t.TempDir())
	statePath := filepath.Join(t.TempDir(), "remote-check.json")
	resolver := &countingResolver{tag: "v1.2.3"}

	res, err := CheckRemote(context.Background(), resolver, statePath, time.Hour, "")
	if err != nil {
		t.Fatalf("CheckRemote: %v", err)
	}
	if !res.Behind {
		t.Fatalf("expected Behind=true with empty installedTag, got %+v", res)
	}
}

// TestRemoteCheckMatchingTagsNotBehind verifies matching tags report Behind=false.
func TestRemoteCheckMatchingTagsNotBehind(t *testing.T) {
	t.Setenv("BRAVROS_CONFIG_DIR", t.TempDir())
	statePath := filepath.Join(t.TempDir(), "remote-check.json")
	resolver := &countingResolver{tag: "v1.2.3"}

	res, err := CheckRemote(context.Background(), resolver, statePath, time.Hour, "v1.2.3")
	if err != nil {
		t.Fatalf("CheckRemote: %v", err)
	}
	if res.Behind {
		t.Fatalf("expected Behind=false with matching tags, got %+v", res)
	}
}

// TestRemoteCheckTTLEnv verifies RemoteCheckTTL honors BRAVROS_REMOTE_CHECK_TTL.
func TestRemoteCheckTTLEnv(t *testing.T) {
	t.Run("explicit duration", func(t *testing.T) {
		t.Setenv("BRAVROS_REMOTE_CHECK_TTL", "30m")
		if got := RemoteCheckTTL(); got != 30*time.Minute {
			t.Fatalf("expected 30m, got %v", got)
		}
	})

	t.Run("zero disables rate limit", func(t *testing.T) {
		t.Setenv("BRAVROS_REMOTE_CHECK_TTL", "0")
		if got := RemoteCheckTTL(); got != 0 {
			t.Fatalf("expected 0, got %v", got)
		}
	})

	t.Run("garbage falls back to default", func(t *testing.T) {
		t.Setenv("BRAVROS_REMOTE_CHECK_TTL", "not-a-duration")
		if got := RemoteCheckTTL(); got != DefaultRemoteCheckTTL {
			t.Fatalf("expected default %v, got %v", DefaultRemoteCheckTTL, got)
		}
	})

	t.Run("unset falls back to default", func(t *testing.T) {
		if got := RemoteCheckTTL(); got != DefaultRemoteCheckTTL {
			t.Fatalf("expected default %v, got %v", DefaultRemoteCheckTTL, got)
		}
	})
}

// TestRemoteStateRoundTrip verifies SaveRemoteState then LoadRemoteState
// round-trips, and LoadRemoteState on a missing file returns a zero state and
// no error.
func TestRemoteStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Missing file → zero state, no error.
	empty, err := LoadRemoteState(path)
	if err != nil {
		t.Fatalf("LoadRemoteState on missing file: %v", err)
	}
	if empty != (RemoteState{}) {
		t.Fatalf("expected zero state for missing file, got %+v", empty)
	}

	want := RemoteState{
		LastCheck:    time.Now().Truncate(time.Second),
		RemoteTag:    "v1.2.3",
		InstalledTag: "v1.2.2",
	}
	if err := SaveRemoteState(path, want); err != nil {
		t.Fatalf("SaveRemoteState: %v", err)
	}

	got, err := LoadRemoteState(path)
	if err != nil {
		t.Fatalf("LoadRemoteState: %v", err)
	}
	if !got.LastCheck.Equal(want.LastCheck) || got.RemoteTag != want.RemoteTag || got.InstalledTag != want.InstalledTag {
		t.Fatalf("round trip mismatch: want %+v, got %+v", want, got)
	}
}

// TestRemoteStatePathLayout verifies RemoteStatePath composes ConfigDir() with
// the expected state subpath.
func TestRemoteStatePathLayout(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BRAVROS_CONFIG_DIR", dir)

	want := filepath.Join(dir, "state", ".bravros-remote-check.json")
	if got := RemoteStatePath(); got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}
