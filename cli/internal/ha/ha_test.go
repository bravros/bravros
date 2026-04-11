//go:build personal

package ha

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// --- Device mapping tests ---

func TestResolveDevice_KnownDevices(t *testing.T) {
	cases := map[string]string{
		"studio":   "notify/alexa_media_echo_studio",
		"sala":     "notify/alexa_media_echo_dot_sala",
		"suite":    "notify/alexa_media_echo_show_suite",
		"banheiro": "notify/alexa_media_echo_banheiro_suite",
		"gourmet":  "notify/alexa_media_echo_area_gourmet",
		"todos":    "notify/alexa_media_todo_lugar",
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			got := ResolveDevice(name)
			if got != want {
				t.Errorf("ResolveDevice(%q) = %q, want %q", name, got, want)
			}
		})
	}
}

func TestResolveDevice_UnknownFallback(t *testing.T) {
	got := ResolveDevice("escritorio")
	want := "notify/alexa_media_escritorio"
	if got != want {
		t.Errorf("ResolveDevice(unknown) = %q, want %q", got, want)
	}
}

func TestDeviceMap_AllEntriesPresent(t *testing.T) {
	expected := []string{"studio", "sala", "suite", "banheiro", "gourmet", "todos"}
	for _, name := range expected {
		if _, ok := DeviceMap[name]; !ok {
			t.Errorf("DeviceMap missing key %q", name)
		}
	}
}

func TestColorMap_KnownColors(t *testing.T) {
	cases := map[string][3]int{
		"blue":   {0, 0, 255},
		"red":    {255, 0, 0},
		"green":  {0, 255, 0},
		"yellow": {255, 200, 0},
		"white":  {255, 255, 255},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := ColorMap[name]
			if !ok {
				t.Fatalf("ColorMap missing key %q", name)
			}
			if got != want {
				t.Errorf("ColorMap[%q] = %v, want %v", name, got, want)
			}
		})
	}
}

func TestStudioLights_HasEntries(t *testing.T) {
	if len(StudioLights) == 0 {
		t.Fatal("StudioLights is empty")
	}
	for _, id := range StudioLights {
		if id == "" {
			t.Error("StudioLights contains empty entity ID")
		}
		if len(id) < 6 || id[:6] != "light." {
			t.Errorf("StudioLights entry %q does not start with 'light.'", id)
		}
	}
}

// --- Client construction tests ---

func TestNewClient_DefaultServer(t *testing.T) {
	t.Setenv("HASS_TOKEN", "test-token-123")
	t.Setenv("HASS_SERVER", "")

	c, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	if c.Server != "http://homeassistant.local:8123" {
		t.Errorf("default server = %q, want http://homeassistant.local:8123", c.Server)
	}
	if c.Token != "test-token-123" {
		t.Errorf("token = %q, want test-token-123", c.Token)
	}
}

func TestNewClient_CustomServer(t *testing.T) {
	t.Setenv("HASS_TOKEN", "tok")
	t.Setenv("HASS_SERVER", "http://custom:9999")

	c, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	if c.Server != "http://custom:9999" {
		t.Errorf("server = %q, want http://custom:9999", c.Server)
	}
}

func TestNewClient_MissingToken(t *testing.T) {
	t.Setenv("HASS_TOKEN", "")
	t.Setenv("HASS_SERVER", "")

	_, err := NewClient()
	if err == nil {
		t.Fatal("expected error when HASS_TOKEN is empty")
	}
}

// --- URL construction & request tests (using httptest) ---

func newTestClient(serverURL string) *Client {
	return &Client{
		Server: serverURL,
		Token:  "test-bearer-token",
		http:   &http.Client{},
	}
}

func TestCallService_URLAndHeaders(t *testing.T) {
	var gotPath, gotAuth, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.CallService("light/turn_on", `{"entity_id":"light.test"}`)
	if err != nil {
		t.Fatalf("CallService error: %v", err)
	}
	if gotPath != "/api/services/light/turn_on" {
		t.Errorf("path = %q, want /api/services/light/turn_on", gotPath)
	}
	if gotAuth != "Bearer test-bearer-token" {
		t.Errorf("auth = %q, want Bearer test-bearer-token", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotCT)
	}
}

func TestGetState_URLAndParsing(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"state":"on","entity_id":"light.test","attributes":{"brightness":200}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	result, err := c.GetState("light.test")
	if err != nil {
		t.Fatalf("GetState error: %v", err)
	}
	if gotPath != "/api/states/light.test" {
		t.Errorf("path = %q, want /api/states/light.test", gotPath)
	}
	if result["state"] != "on" {
		t.Errorf("state = %v, want 'on'", result["state"])
	}
	if result["entity_id"] != "light.test" {
		t.Errorf("entity_id = %v, want 'light.test'", result["entity_id"])
	}
}

func TestGetStateValue_ReturnsState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"state":"off"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	val, err := c.GetStateValue("switch.test")
	if err != nil {
		t.Fatalf("GetStateValue error: %v", err)
	}
	if val != "off" {
		t.Errorf("state value = %q, want 'off'", val)
	}
}

func TestGetStateValue_MissingStateField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"attributes":{}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.GetStateValue("sensor.missing")
	if err == nil {
		t.Fatal("expected error when state field is missing")
	}
}

func TestIsMacUnlocked_On(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"state":"on"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if !c.IsMacUnlocked() {
		t.Error("IsMacUnlocked() = false, want true when state is 'on'")
	}
}

func TestIsMacUnlocked_Off(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"state":"off"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if c.IsMacUnlocked() {
		t.Error("IsMacUnlocked() = true, want false when state is 'off'")
	}
}

func TestIsMacUnlocked_ServerError(t *testing.T) {
	// Use an unreachable server to trigger error path
	c := newTestClient("http://127.0.0.1:1")
	if c.IsMacUnlocked() {
		t.Error("IsMacUnlocked() = true, want false on connection error")
	}
}

// --- Env cleanup helper (for older Go without t.Setenv) ---

func init() {
	// Ensure test isolation — clear HA env vars that may exist on the host
	_ = os.Getenv("HASS_TOKEN") // no-op, just for clarity
}
