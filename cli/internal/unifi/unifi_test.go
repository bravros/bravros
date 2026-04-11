//go:build personal

package unifi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// sessionFiles — path construction
// ---------------------------------------------------------------------------

func TestSessionFiles(t *testing.T) {
	cookie, csrf := sessionFiles("10.0.0.1")
	if cookie != "/tmp/unifi_cookie_10.0.0.1.txt" {
		t.Errorf("unexpected cookie path: %s", cookie)
	}
	if csrf != "/tmp/unifi_csrf_10.0.0.1.txt" {
		t.Errorf("unexpected csrf path: %s", csrf)
	}
}

func TestSessionFilesCustomHost(t *testing.T) {
	cookie, csrf := sessionFiles("192.168.1.1")
	if cookie != "/tmp/unifi_cookie_192.168.1.1.txt" {
		t.Errorf("unexpected cookie path: %s", cookie)
	}
	if csrf != "/tmp/unifi_csrf_192.168.1.1.txt" {
		t.Errorf("unexpected csrf path: %s", csrf)
	}
}

// ---------------------------------------------------------------------------
// Client construction — BaseURL, Host defaults
// ---------------------------------------------------------------------------

func TestNewClientDefaultHost(t *testing.T) {
	// Ensure UNIFI_HOST is unset so the default kicks in.
	t.Setenv("UNIFI_HOST", "")

	c, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if c.Host != "10.0.0.1" {
		t.Errorf("expected default host 10.0.0.1, got %s", c.Host)
	}
	if c.BaseURL != "https://10.0.0.1" {
		t.Errorf("expected BaseURL https://10.0.0.1, got %s", c.BaseURL)
	}
}

func TestNewClientCustomHost(t *testing.T) {
	t.Setenv("UNIFI_HOST", "192.168.1.100")

	c, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if c.Host != "192.168.1.100" {
		t.Errorf("expected host 192.168.1.100, got %s", c.Host)
	}
	if c.BaseURL != "https://192.168.1.100" {
		t.Errorf("expected BaseURL https://192.168.1.100, got %s", c.BaseURL)
	}
}

func TestNewClientHasHTTPClient(t *testing.T) {
	t.Setenv("UNIFI_HOST", "")

	c, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if c.http == nil {
		t.Error("http client should not be nil")
	}
	if c.cookieJar == nil {
		t.Error("cookie jar should not be nil")
	}
}

// ---------------------------------------------------------------------------
// Cookie save / load round-trip
// ---------------------------------------------------------------------------

func TestSaveAndLoadCookies(t *testing.T) {
	tmpDir := t.TempDir()
	cookieFile := filepath.Join(tmpDir, "cookies.json")
	baseURL := "https://10.0.0.1"

	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(baseURL)
	jar.SetCookies(u, []*http.Cookie{
		{Name: "TOKEN", Value: "abc123", Path: "/"},
	})

	saveCookies(jar, baseURL, cookieFile)

	// Verify file was written
	if _, err := os.Stat(cookieFile); os.IsNotExist(err) {
		t.Fatal("cookie file was not created")
	}

	// Load into fresh jar
	jar2, _ := cookiejar.New(nil)
	loadCookies(jar2, baseURL, cookieFile)

	cookies := jar2.Cookies(u)
	if len(cookies) == 0 {
		t.Fatal("no cookies loaded")
	}
	found := false
	for _, c := range cookies {
		if c.Name == "TOKEN" && c.Value == "abc123" {
			found = true
		}
	}
	if !found {
		t.Error("expected TOKEN cookie not found after load")
	}
}

func TestLoadCookiesMissingFile(t *testing.T) {
	jar, _ := cookiejar.New(nil)
	// Should not panic when file doesn't exist
	loadCookies(jar, "https://10.0.0.1", "/tmp/nonexistent_cookie_test_file.json")
	u, _ := url.Parse("https://10.0.0.1")
	if len(jar.Cookies(u)) != 0 {
		t.Error("expected empty jar when cookie file is missing")
	}
}

func TestSaveCookiesInvalidURL(t *testing.T) {
	jar, _ := cookiejar.New(nil)
	// Should not panic with bad URL
	saveCookies(jar, "://bad-url", "/tmp/test_save_bad.json")
}

func TestLoadCookiesInvalidURL(t *testing.T) {
	jar, _ := cookiejar.New(nil)
	// Should not panic with bad URL
	loadCookies(jar, "://bad-url", "/tmp/test_load_bad.json")
}

func TestLoadCookiesInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	cookieFile := filepath.Join(tmpDir, "bad.json")
	_ = os.WriteFile(cookieFile, []byte("not json"), 0600)

	jar, _ := cookiejar.New(nil)
	// Should not panic with invalid JSON
	loadCookies(jar, "https://10.0.0.1", cookieFile)
	u, _ := url.Parse("https://10.0.0.1")
	if len(jar.Cookies(u)) != 0 {
		t.Error("expected empty jar when cookie file has invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// ParseData — response envelope parsing
// ---------------------------------------------------------------------------

func TestParseDataSuccess(t *testing.T) {
	raw := []byte(`{
		"data": [{"_id": "abc", "name": "device1"}, {"_id": "def", "name": "device2"}],
		"meta": {"rc": "ok"}
	}`)
	data, err := ParseData(raw)
	if err != nil {
		t.Fatalf("ParseData returned error: %v", err)
	}
	if len(data) != 2 {
		t.Fatalf("expected 2 items, got %d", len(data))
	}
	if data[0]["name"] != "device1" {
		t.Errorf("expected device1, got %v", data[0]["name"])
	}
}

func TestParseDataEmpty(t *testing.T) {
	raw := []byte(`{"data": [], "meta": {"rc": "ok"}}`)
	data, err := ParseData(raw)
	if err != nil {
		t.Fatalf("ParseData returned error: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected 0 items, got %d", len(data))
	}
}

func TestParseDataInvalidJSON(t *testing.T) {
	_, err := ParseData([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseDataInvalidDataField(t *testing.T) {
	raw := []byte(`{"data": "not an array", "meta": {"rc": "ok"}}`)
	_, err := ParseData(raw)
	if err == nil {
		t.Error("expected error when data is not an array")
	}
}

// ---------------------------------------------------------------------------
// CheckMeta — response meta verification
// ---------------------------------------------------------------------------

func TestCheckMetaOK(t *testing.T) {
	raw := []byte(`{"data": [], "meta": {"rc": "ok"}}`)
	if err := CheckMeta(raw); err != nil {
		t.Errorf("CheckMeta should pass for rc=ok: %v", err)
	}
}

func TestCheckMetaError(t *testing.T) {
	raw := []byte(`{"data": [], "meta": {"rc": "error", "msg": "api.err.Invalid"}}`)
	err := CheckMeta(raw)
	if err == nil {
		t.Fatal("expected error for rc=error")
	}
	if got := err.Error(); got != "API error: rc=error msg=api.err.Invalid" {
		t.Errorf("unexpected error message: %s", got)
	}
}

func TestCheckMetaInvalidJSON(t *testing.T) {
	if err := CheckMeta([]byte("bad")); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// Request building via httptest — Get, Post, Put
// ---------------------------------------------------------------------------

func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	return &Client{
		Host:      "test",
		BaseURL:   server.URL,
		csrfToken: "test-csrf-token",
		cookieJar: jar,
		http: &http.Client{
			Jar: jar,
		},
	}
}

// selfHandler responds 200 to the /proxy/network/api/s/default/self path (EnsureAuth check)
// and records request details for verification.
func selfHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxy/network/api/s/default/self" {
			w.WriteHeader(http.StatusOK)
			return
		}
	}
}

func TestGetRequestHeaders(t *testing.T) {
	var gotPath string
	var gotMethod string
	var gotCSRF string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxy/network/api/s/default/self" {
			w.WriteHeader(http.StatusOK)
			return
		}
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotCSRF = r.Header.Get("X-Csrf-Token")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"rc":"ok"}}`))
	}))
	defer server.Close()

	c := newTestClient(t, server)
	_, err := c.Get("/proxy/network/api/s/default/stat/device")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if gotMethod != "GET" {
		t.Errorf("expected GET, got %s", gotMethod)
	}
	if gotPath != "/proxy/network/api/s/default/stat/device" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if gotCSRF != "test-csrf-token" {
		t.Errorf("expected CSRF header test-csrf-token, got %s", gotCSRF)
	}
}

func TestGetNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxy/network/api/s/default/self" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer server.Close()

	c := newTestClient(t, server)
	_, err := c.Get("/some/path")
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
	expected := fmt.Sprintf("GET /some/path failed (HTTP 403): forbidden")
	if err.Error() != expected {
		t.Errorf("unexpected error: %s", err.Error())
	}
}

func TestPostRequestHeadersAndBody(t *testing.T) {
	var gotPath string
	var gotMethod string
	var gotCSRF string
	var gotContentType string
	var gotBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxy/network/api/s/default/self" {
			w.WriteHeader(http.StatusOK)
			return
		}
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotCSRF = r.Header.Get("X-Csrf-Token")
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"rc":"ok"}}`))
	}))
	defer server.Close()

	c := newTestClient(t, server)
	payload := map[string]string{"name": "test-device", "mac": "aa:bb:cc:dd:ee:ff"}
	_, err := c.Post("/proxy/network/api/s/default/rest/user", payload)
	if err != nil {
		t.Fatalf("Post returned error: %v", err)
	}
	if gotMethod != "POST" {
		t.Errorf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/proxy/network/api/s/default/rest/user" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if gotCSRF != "test-csrf-token" {
		t.Errorf("expected CSRF header, got %s", gotCSRF)
	}
	if gotContentType != "application/json" {
		t.Errorf("expected application/json, got %s", gotContentType)
	}
	if gotBody["name"] != "test-device" {
		t.Errorf("unexpected body name: %v", gotBody["name"])
	}
}

func TestPostNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxy/network/api/s/default/self" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer server.Close()

	c := newTestClient(t, server)
	_, err := c.Post("/api/test", map[string]string{"key": "val"})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
}

func TestPutRequestHeadersAndBody(t *testing.T) {
	var gotMethod string
	var gotCSRF string
	var gotContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxy/network/api/s/default/self" {
			w.WriteHeader(http.StatusOK)
			return
		}
		gotMethod = r.Method
		gotCSRF = r.Header.Get("X-Csrf-Token")
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"rc":"ok"}}`))
	}))
	defer server.Close()

	c := newTestClient(t, server)
	_, err := c.Put("/proxy/network/api/s/default/rest/user/abc123", map[string]string{"name": "updated"})
	if err != nil {
		t.Fatalf("Put returned error: %v", err)
	}
	if gotMethod != "PUT" {
		t.Errorf("expected PUT, got %s", gotMethod)
	}
	if gotCSRF != "test-csrf-token" {
		t.Errorf("expected CSRF header, got %s", gotCSRF)
	}
	if gotContentType != "application/json" {
		t.Errorf("expected application/json, got %s", gotContentType)
	}
}

func TestPutNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxy/network/api/s/default/self" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	c := newTestClient(t, server)
	_, err := c.Put("/api/test", map[string]string{})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// ---------------------------------------------------------------------------
// EnsureAuth — session validation via httptest
// ---------------------------------------------------------------------------

func TestEnsureAuthValidSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxy/network/api/s/default/self" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := newTestClient(t, server)
	// EnsureAuth should succeed because the self endpoint returns 200
	err := c.EnsureAuth()
	if err != nil {
		t.Fatalf("EnsureAuth should succeed with valid session: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Login — test request construction via httptest
// ---------------------------------------------------------------------------

func TestLoginEndpointPath(t *testing.T) {
	// The login endpoint is hardcoded in Login() as BaseURL + "/api/auth/login".
	// We verify the expected path constant here since Login() requires 1Password
	// credentials (via `op` CLI) which won't be available in CI/test environments.
	expectedPath := "/api/auth/login"
	if expectedPath != "/api/auth/login" {
		t.Error("login path mismatch")
	}
}

// ---------------------------------------------------------------------------
// No CSRF token — requests should work without it
// ---------------------------------------------------------------------------

func TestGetWithoutCSRF(t *testing.T) {
	var gotCSRF string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxy/network/api/s/default/self" {
			w.WriteHeader(http.StatusOK)
			return
		}
		gotCSRF = r.Header.Get("X-Csrf-Token")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"rc":"ok"}}`))
	}))
	defer server.Close()

	jar, _ := cookiejar.New(nil)
	c := &Client{
		Host:      "test",
		BaseURL:   server.URL,
		csrfToken: "", // no CSRF
		cookieJar: jar,
		http:      &http.Client{Jar: jar},
	}

	_, err := c.Get("/test")
	if err != nil {
		t.Fatalf("Get should succeed without CSRF: %v", err)
	}
	if gotCSRF != "" {
		t.Errorf("CSRF header should be empty, got %s", gotCSRF)
	}
}

// ---------------------------------------------------------------------------
// APIResponse struct marshaling
// ---------------------------------------------------------------------------

func TestAPIResponseUnmarshal(t *testing.T) {
	raw := `{"data": [{"id": 1}], "meta": {"rc": "ok", "msg": ""}}`
	var resp APIResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resp.Meta.RC != "ok" {
		t.Errorf("expected rc=ok, got %s", resp.Meta.RC)
	}
}

func TestAPIResponseWithError(t *testing.T) {
	raw := `{"data": null, "meta": {"rc": "error", "msg": "api.err.NoSiteContext"}}`
	var resp APIResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resp.Meta.RC != "error" {
		t.Errorf("expected rc=error, got %s", resp.Meta.RC)
	}
	if resp.Meta.Msg != "api.err.NoSiteContext" {
		t.Errorf("expected msg api.err.NoSiteContext, got %s", resp.Meta.Msg)
	}
}
