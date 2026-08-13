package token

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withHome redirects $HOME to a temp dir for the duration of the test and
// returns the gate's resolved token path.
func withHome(t *testing.T, g Gate) string {
	t.Helper()
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmp)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })
	return g.Path()
}

func testGate() Gate {
	return Gate{Name: "promote", UnlockHelp: "help", SuccessMsg: "ok", RefuseMsg: "refused"}
}

// TestGate_PathLayout pins the on-disk path so the audit-rule readers stay in sync.
func TestGate_PathLayout(t *testing.T) {
	g := testGate()
	path := withHome(t, g)
	if filepath.Base(path) != "promote-token" {
		t.Errorf("token basename: got %q, want %q", filepath.Base(path), "promote-token")
	}
	if filepath.Base(filepath.Dir(path)) != "state" {
		t.Errorf("token parent dir: got %q, want %q", filepath.Base(filepath.Dir(path)), "state")
	}
}

// TestGate_MintReadRoundTrip verifies Mint writes a token that Read parses back
// with the documented single-use + TTL fields, and that the JSON tags match the
// historical on-disk contract that audit Rule 17b/47 unmarshal.
func TestGate_MintReadRoundTrip(t *testing.T) {
	g := testGate()
	path := withHome(t, g)

	tok, err := g.Mint(5, "/dev/ttys001")
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if !tok.SingleUse {
		t.Error("minted token should be single_use=true")
	}
	if tok.TTLMinutes != 5 {
		t.Errorf("ttl_minutes: got %d, want 5", tok.TTLMinutes)
	}
	if tok.Expired() {
		t.Error("freshly minted token should not be expired")
	}

	// Read back from disk.
	got := g.Read()
	if got == nil {
		t.Fatal("Read() returned nil after Mint")
	}
	if got.TTY != "/dev/ttys001" {
		t.Errorf("tty: got %q, want %q", got.TTY, "/dev/ttys001")
	}

	// Raw JSON tags must match the audit-rule contract.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, key := range []string{"created_at", "expires_at", "ttl_minutes", "tty", "single_use"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("on-disk token missing json key %q (audit rules depend on it)", key)
		}
	}
}

// TestGate_MintDefaultTTL verifies non-positive TTLs default to 5 minutes.
func TestGate_MintDefaultTTL(t *testing.T) {
	g := testGate()
	withHome(t, g)
	tok, err := g.Mint(0, "")
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if tok.TTLMinutes != 5 {
		t.Errorf("default ttl_minutes: got %d, want 5", tok.TTLMinutes)
	}
}

// TestGate_PresentAutoCleansExpired verifies Present() returns false for an
// expired token AND deletes it (matching legacy *TokenPresent behavior).
func TestGate_PresentAutoCleansExpired(t *testing.T) {
	g := testGate()
	path := withHome(t, g)

	past := time.Now().UTC().Add(-10 * time.Minute)
	writeRaw(t, path, Token{CreatedAt: past.Add(-5 * time.Minute), ExpiresAt: past, SingleUse: true})

	if g.Present() {
		t.Error("Present() should be false for an expired token")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Present() should auto-delete an expired token")
	}
}

// TestGate_ValidDoesNotMutate verifies Valid() reports expiry without deleting.
func TestGate_ValidDoesNotMutate(t *testing.T) {
	g := testGate()
	path := withHome(t, g)

	past := time.Now().UTC().Add(-1 * time.Minute)
	writeRaw(t, path, Token{CreatedAt: past, ExpiresAt: past, SingleUse: true})

	if g.Valid() {
		t.Error("Valid() should be false for an expired token")
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("Valid() must NOT delete the token file")
	}

	// Fresh token → valid.
	now := time.Now().UTC()
	writeRaw(t, path, Token{CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute), SingleUse: true})
	if !g.Valid() {
		t.Error("Valid() should be true for a fresh token")
	}
}

// TestGate_PresentFresh verifies a fresh token reports present.
func TestGate_PresentFresh(t *testing.T) {
	g := testGate()
	path := withHome(t, g)
	now := time.Now().UTC()
	writeRaw(t, path, Token{CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute), SingleUse: true})
	if !g.Present() {
		t.Error("Present() should be true for a fresh token")
	}
}

// TestGate_RevokeRemoves verifies Revoke deletes the file and surfaces
// os.IsNotExist when absent.
func TestGate_RevokeRemoves(t *testing.T) {
	g := testGate()
	path := withHome(t, g)
	now := time.Now().UTC()
	writeRaw(t, path, Token{CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute), SingleUse: true})

	if err := g.Revoke(); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Revoke should delete the token file")
	}

	// Revoke when absent → os.IsNotExist error (caller treats as idempotent).
	err := g.Revoke()
	if !os.IsNotExist(err) {
		t.Errorf("Revoke on absent token: want os.IsNotExist, got %v", err)
	}
}

// TestGate_ReadMalformed verifies Read returns nil for malformed JSON.
func TestGate_ReadMalformed(t *testing.T) {
	g := testGate()
	path := withHome(t, g)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if g.Read() != nil {
		t.Error("Read() should return nil for malformed JSON")
	}
}

func writeRaw(t *testing.T, path string, tok Token) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	data, err := json.Marshal(tok)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
