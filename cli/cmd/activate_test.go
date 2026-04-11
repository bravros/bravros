package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bravros/bravros/internal/license"
)

// setupActivateTest wires up:
//   - a temp HOME dir so SaveToken writes to a temp file
//   - the DefaultClient pointed at the given httptest server URL
//
// It returns a cleanup func that must be deferred.
func setupActivateTest(t *testing.T, srv *httptest.Server) func() {
	t.Helper()

	// Redirect HOME so SaveToken writes to a temp location.
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	if err := os.MkdirAll(filepath.Join(tmpHome, ".claude"), 0700); err != nil {
		t.Fatalf("setup: mkdir .claude: %v", err)
	}

	// Point DefaultClient at the test server.
	origClient := license.DefaultClient
	if srv != nil {
		license.DefaultClient = license.NewClient(srv.URL)
	}

	return func() {
		// Restore HOME.
		if origHome == "" {
			os.Unsetenv("HOME")
		} else {
			os.Setenv("HOME", origHome)
		}
		// Restore DefaultClient.
		license.DefaultClient = origClient
		if srv != nil {
			srv.Close()
		}
	}
}

// newActivateServer creates an httptest server that responds to POST /activate.
// If token is non-empty it returns 200 with {"token": token}.
// If apiErrCode is non-empty it returns 422 with {"error": {"code": ..., "message": ...}}.
func newActivateServer(t *testing.T, token, apiErrCode, apiErrMsg string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/activate" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if apiErrCode != "" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]string{"code": apiErrCode, "message": apiErrMsg},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
	}))
}

// TestActivate_ValidKey_Success tests the happy path: valid key format,
// API returns a token, token is saved and success message is printed.
func TestActivate_ValidKey_Success(t *testing.T) {
	srv := newActivateServer(t, "jwt-token-abc", "", "")
	cleanup := setupActivateTest(t, srv)
	defer cleanup()

	var out bytes.Buffer
	activateCmd.SetOut(&out)
	activateCmd.SetErr(&out)

	err := activateCmd.RunE(activateCmd, []string{"ABCD-1234-EF56-7890"})
	if err != nil {
		t.Fatalf("RunE() error = %v", err)
	}

	if !strings.Contains(out.String(), "activated") {
		t.Errorf("expected success message containing 'activated', got: %q", out.String())
	}

	// Token file should exist in the temp home.
	home := os.Getenv("HOME")
	cachePath := filepath.Join(home, ".claude", ".bravros-auth")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("expected token file at %s, got error: %v", cachePath, err)
	}
	if string(data) != "jwt-token-abc" {
		t.Errorf("saved token = %q, want %q", string(data), "jwt-token-abc")
	}
}

// TestActivate_InvalidKeyFormat tests that a key with wrong format is rejected
// before hitting the network.
func TestActivate_InvalidKeyFormat(t *testing.T) {
	// No server needed — bad format is caught before the HTTP call.
	cleanup := setupActivateTest(t, nil)
	defer cleanup()

	var errBuf bytes.Buffer
	activateCmd.SetErr(&errBuf)

	err := activateCmd.RunE(activateCmd, []string{"not-a-valid-key"})
	if err == nil {
		t.Fatal("RunE() expected error for invalid key format, got nil")
	}

	if !strings.Contains(errBuf.String(), "Invalid") && !strings.Contains(errBuf.String(), "invalid") {
		t.Errorf("expected error message about invalid key format, got: %q", errBuf.String())
	}
}

// TestActivate_APIError_AlreadyActive tests the "already_active" API error code.
func TestActivate_APIError_AlreadyActive(t *testing.T) {
	srv := newActivateServer(t, "", "already_active", "key already activated on another machine")
	cleanup := setupActivateTest(t, srv)
	defer cleanup()

	var errBuf bytes.Buffer
	activateCmd.SetErr(&errBuf)

	err := activateCmd.RunE(activateCmd, []string{"ABCD-1234-EF56-7890"})
	if err == nil {
		t.Fatal("RunE() expected error for already_active, got nil")
	}

	// Should print the localized already_active message.
	if !strings.Contains(errBuf.String(), "already") {
		t.Errorf("expected 'already' in error output, got: %q", errBuf.String())
	}
}

// TestActivate_APIError_MachinelLimit tests the "machine_limit" API error code.
func TestActivate_APIError_MachineLimit(t *testing.T) {
	srv := newActivateServer(t, "", "machine_limit", "max machines reached")
	cleanup := setupActivateTest(t, srv)
	defer cleanup()

	var errBuf bytes.Buffer
	activateCmd.SetErr(&errBuf)

	err := activateCmd.RunE(activateCmd, []string{"ABCD-1234-EF56-7890"})
	if err == nil {
		t.Fatal("RunE() expected error for machine_limit, got nil")
	}

	if !strings.Contains(errBuf.String(), "slot") || !strings.Contains(errBuf.String(), "machine") {
		t.Errorf("expected machine slot error message, got: %q", errBuf.String())
	}
}

// TestActivate_APIError_InvalidKey tests the "invalid_key" API error code.
func TestActivate_APIError_InvalidKey(t *testing.T) {
	srv := newActivateServer(t, "", "invalid_key", "key not found")
	cleanup := setupActivateTest(t, srv)
	defer cleanup()

	var errBuf bytes.Buffer
	activateCmd.SetErr(&errBuf)

	err := activateCmd.RunE(activateCmd, []string{"ABCD-1234-EF56-7890"})
	if err == nil {
		t.Fatal("RunE() expected error for invalid_key, got nil")
	}

	if !strings.Contains(errBuf.String(), "Invalid") && !strings.Contains(errBuf.String(), "invalid") {
		t.Errorf("expected invalid key error message, got: %q", errBuf.String())
	}
}

// TestActivate_NetworkError tests that a network failure prints the network error message.
func TestActivate_NetworkError(t *testing.T) {
	// Server that immediately closes the connection.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijack not supported", http.StatusServiceUnavailable)
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	cleanup := setupActivateTest(t, srv)
	defer cleanup()

	var errBuf bytes.Buffer
	activateCmd.SetErr(&errBuf)

	err := activateCmd.RunE(activateCmd, []string{"ABCD-1234-EF56-7890"})
	if err == nil {
		t.Fatal("RunE() expected error for network failure, got nil")
	}

	if !strings.Contains(errBuf.String(), "connect") && !strings.Contains(errBuf.String(), "server") && !strings.Contains(errBuf.String(), "internet") {
		t.Errorf("expected network error message, got: %q", errBuf.String())
	}
}
