package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/bravros/bravros/internal/license"
)

// withTempHomeDeactivate sets HOME to a temp dir so that AuthCachePath()
// resolves inside the temp directory, isolating the auth cache from real data.
func withTempHomeDeactivate(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0700); err != nil {
		t.Fatalf("setup: create temp .claude dir: %v", err)
	}
}

// writeTestToken writes a token to the auth cache file (via license.SaveToken).
func writeTestToken(t *testing.T, token string) {
	t.Helper()
	if err := license.SaveToken(token); err != nil {
		t.Fatalf("setup: SaveToken: %v", err)
	}
}

// respondJSON writes a JSON response with the given status code.
func respondJSONDeactivate(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func TestDeactivateCmd_NotActivated(t *testing.T) {
	withTempHomeDeactivate(t)
	// No token file → LoadToken returns ErrNotActivated

	var buf bytes.Buffer
	deactivateCmd.SetOut(&buf)
	deactivateCmd.SetErr(&buf)

	err := deactivateCmd.RunE(deactivateCmd, []string{})
	if err != nil {
		t.Fatalf("expected nil error for not-activated path, got: %v", err)
	}
}

func TestDeactivateCmd_Success(t *testing.T) {
	withTempHomeDeactivate(t)
	writeTestToken(t, "test-jwt-token")

	// Spin up a fake API server that returns success
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/deactivate" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		respondJSONDeactivate(w, http.StatusOK, map[string]interface{}{})
	}))
	defer srv.Close()

	origClient := license.DefaultClient
	license.DefaultClient = license.NewClient(srv.URL)
	defer func() { license.DefaultClient = origClient }()

	var buf bytes.Buffer
	deactivateCmd.SetOut(&buf)
	deactivateCmd.SetErr(&buf)

	err := deactivateCmd.RunE(deactivateCmd, []string{})
	if err != nil {
		t.Fatalf("expected nil error on success, got: %v", err)
	}

	// Token should be cleared after successful deactivation
	_, loadErr := license.LoadToken()
	if !errors.Is(loadErr, license.ErrNotActivated) {
		t.Errorf("expected token to be cleared, LoadToken() error = %v, want ErrNotActivated", loadErr)
	}
}

func TestDeactivateCmd_NetworkFailure_StillClearsCache(t *testing.T) {
	withTempHomeDeactivate(t)
	writeTestToken(t, "test-jwt-token")

	// Spin up a fake API server that returns a network-level failure
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			respondJSONDeactivate(w, http.StatusServiceUnavailable, map[string]interface{}{})
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	origClient := license.DefaultClient
	license.DefaultClient = license.NewClient(srv.URL)
	defer func() { license.DefaultClient = origClient }()

	var buf bytes.Buffer
	deactivateCmd.SetOut(&buf)
	deactivateCmd.SetErr(&buf)

	err := deactivateCmd.RunE(deactivateCmd, []string{})
	if err != nil {
		t.Fatalf("expected nil error on network failure (graceful), got: %v", err)
	}

	// Token should still be cleared even when the API call fails
	_, loadErr := license.LoadToken()
	if !errors.Is(loadErr, license.ErrNotActivated) {
		t.Errorf("expected token to be cleared on network failure, LoadToken() error = %v, want ErrNotActivated", loadErr)
	}
}
