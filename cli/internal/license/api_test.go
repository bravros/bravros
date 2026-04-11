package license

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// respondJSON is a helper that writes a JSON body with the given status code.
func respondJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func TestActivate_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/activate" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("User-Agent") != userAgent {
			t.Errorf("expected User-Agent %q, got %q", userAgent, r.Header.Get("User-Agent"))
		}

		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["license_key"] != "ABCD-1234-EF56-7890" {
			t.Errorf("unexpected license_key: %q", req["license_key"])
		}
		if req["machine_id"] != "test-machine" {
			t.Errorf("unexpected machine_id: %q", req["machine_id"])
		}

		respondJSON(w, http.StatusOK, map[string]string{"token": "jwt-token-123"})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	token, err := client.Activate("ABCD-1234-EF56-7890", "test-machine")
	if err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if token != "jwt-token-123" {
		t.Errorf("Activate() token = %q, want %q", token, "jwt-token-123")
	}
}

func TestActivate_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"error": map[string]string{
				"code":    "machine_limit",
				"message": "maximum machines reached",
			},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.Activate("ABCD-1234-EF56-7890", "test-machine")
	if err == nil {
		t.Fatal("Activate() expected error, got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != "machine_limit" {
		t.Errorf("APIError.Code = %q, want %q", apiErr.Code, "machine_limit")
	}
	if apiErr.Message != "maximum machines reached" {
		t.Errorf("APIError.Message = %q, want %q", apiErr.Message, "maximum machines reached")
	}
}

func TestVerify_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/verify" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["token"] != "old-token" {
			t.Errorf("unexpected token: %q", req["token"])
		}
		if req["machine_id"] != "test-machine" {
			t.Errorf("unexpected machine_id: %q", req["machine_id"])
		}

		respondJSON(w, http.StatusOK, map[string]string{"token": "refreshed-token-456"})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	newToken, err := client.Verify("old-token", "test-machine")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if newToken != "refreshed-token-456" {
		t.Errorf("Verify() token = %q, want %q", newToken, "refreshed-token-456")
	}
}

func TestDeactivate_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/deactivate" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["token"] != "some-token" {
			t.Errorf("unexpected token: %q", req["token"])
		}
		if req["machine_id"] != "test-machine" {
			t.Errorf("unexpected machine_id: %q", req["machine_id"])
		}

		respondJSON(w, http.StatusOK, map[string]interface{}{})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	if err := client.Deactivate("some-token", "test-machine"); err != nil {
		t.Fatalf("Deactivate() error = %v", err)
	}
}

func TestActivate_NetworkError(t *testing.T) {
	// Use a server that immediately closes connections.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			respondJSON(w, http.StatusServiceUnavailable, map[string]interface{}{})
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.Activate("ABCD-1234-EF56-7890", "test-machine")
	if err == nil {
		t.Fatal("Activate() expected network error, got nil")
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Fatalf("expected non-APIError, got *APIError: %v", apiErr)
	}
}
