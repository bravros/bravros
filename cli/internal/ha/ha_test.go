package ha

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
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

// --- BuildTTSPayload tests ---

func TestBuildTTSPayload_DefaultIsAnnounce(t *testing.T) {
	got := BuildTTSPayload("Plano 150 finalizado", false)
	want := `{"message":"Plano 150 finalizado","data":{"type":"announce"}}`
	if got != want {
		t.Errorf("BuildTTSPayload default = %q, want %q", got, want)
	}
}

func TestBuildTTSPayload_TTSFlagSwitchesToTTS(t *testing.T) {
	got := BuildTTSPayload("silent prefix", true)
	want := `{"message":"silent prefix","data":{"type":"tts"}}`
	if got != want {
		t.Errorf("BuildTTSPayload useTTS=true = %q, want %q", got, want)
	}
}

func TestBuildTTSPayload_EscapesQuotesAndAccents(t *testing.T) {
	// Real-world message: PT-BR with accents + an embedded quote
	got := BuildTTSPayload(`próximas "instruções"`, false)
	want := `{"message":"próximas \"instruções\"","data":{"type":"announce"}}`
	if got != want {
		t.Errorf("BuildTTSPayload escape = %q, want %q", got, want)
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

// --- MacUnlockEntity — env-var override vs default fallback ---

func TestMacUnlockEntity_Default(t *testing.T) {
	t.Setenv("HASS_MAC_UNLOCK_ENTITY", "")

	got := MacUnlockEntity()
	want := "input_boolean.macstudio_is_unlocked"
	if got != want {
		t.Errorf("MacUnlockEntity() = %q, want %q", got, want)
	}
}

func TestMacUnlockEntity_Override(t *testing.T) {
	t.Setenv("HASS_MAC_UNLOCK_ENTITY", "input_boolean.my_custom_entity")

	got := MacUnlockEntity()
	want := "input_boolean.my_custom_entity"
	if got != want {
		t.Errorf("MacUnlockEntity() = %q, want %q", got, want)
	}
}

// TestIsMacUnlocked_UsesEnvEntity verifies that IsMacUnlocked() queries the
// entity returned by MacUnlockEntity() — i.e., the override is respected.
func TestIsMacUnlocked_UsesEnvEntity(t *testing.T) {
	const customEntity = "input_boolean.office_presence"
	t.Setenv("HASS_MAC_UNLOCK_ENTITY", customEntity)

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"state":"on"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	result := c.IsMacUnlocked()
	if !result {
		t.Error("IsMacUnlocked() = false, want true when state is 'on'")
	}
	wantPath := "/api/states/" + customEntity
	if gotPath != wantPath {
		t.Errorf("request path = %q, want %q (env override not respected)", gotPath, wantPath)
	}
}

// TestIsMacUnlocked_DefaultEntityPath verifies that IsMacUnlocked() falls back
// to the default entity when HASS_MAC_UNLOCK_ENTITY is unset.
func TestIsMacUnlocked_DefaultEntityPath(t *testing.T) {
	t.Setenv("HASS_MAC_UNLOCK_ENTITY", "")

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"state":"off"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.IsMacUnlocked() // result doesn't matter — we're checking the path
	wantPath := "/api/states/input_boolean.macstudio_is_unlocked"
	if gotPath != wantPath {
		t.Errorf("request path = %q, want %q (default entity not used)", gotPath, wantPath)
	}
}

// --- Env cleanup helper (for older Go without t.Setenv) ---

func init() {
	// Ensure test isolation — clear HA env vars that may exist on the host
	_ = os.Getenv("HASS_TOKEN") // no-op, just for clarity
}

// --- DeviceSlug tests ---

func TestDeviceSlug_KnownDevices(t *testing.T) {
	// The slug is the shared middle of every entity a device owns —
	// media_player.<slug>, switch.<slug>_do_not_disturb_switch — so the DND
	// clear in `ha say` targets the wrong entity if this drifts.
	cases := map[string]string{
		"studio":   "echo_studio",
		"sala":     "echo_dot_sala",
		"suite":    "echo_show_suite",
		"banheiro": "echo_banheiro_suite",
		"gourmet":  "echo_area_gourmet",
		"todos":    "todo_lugar",
	}
	for name, want := range cases {
		if got := DeviceSlug(name); got != want {
			t.Errorf("DeviceSlug(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestDeviceSlug_UnknownPassesThrough(t *testing.T) {
	if got := DeviceSlug("escritorio"); got != "escritorio" {
		t.Errorf("DeviceSlug(unknown) = %q, want %q", got, "escritorio")
	}
}

// --- Mute kill-switch tests ---

func TestMute_RoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if muted, _ := MuteStatus(); muted {
		t.Fatal("MuteStatus() = true on a fresh home, want false")
	}
	if err := SetMute(0); err != nil {
		t.Fatalf("SetMute(0): %v", err)
	}
	muted, until := MuteStatus()
	if !muted {
		t.Error("MuteStatus() = false after SetMute, want true")
	}
	if !until.IsZero() {
		t.Errorf("indefinite mute returned until=%v, want zero", until)
	}
	if err := ClearMute(); err != nil {
		t.Fatalf("ClearMute: %v", err)
	}
	if muted, _ := MuteStatus(); muted {
		t.Error("MuteStatus() = true after ClearMute, want false")
	}
}

func TestMute_ClearWhenAlreadyUnmutedIsNotAnError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := ClearMute(); err != nil {
		t.Errorf("ClearMute() on unmuted state = %v, want nil", err)
	}
}

func TestMute_TimedMuteReportsExpiry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := SetMute(45 * time.Minute); err != nil {
		t.Fatalf("SetMute: %v", err)
	}
	muted, until := MuteStatus()
	if !muted {
		t.Fatal("MuteStatus() = false during a timed mute, want true")
	}
	if until.IsZero() {
		t.Fatal("timed mute returned zero expiry, want a real time")
	}
	if d := time.Until(until); d < 44*time.Minute || d > 46*time.Minute {
		t.Errorf("expiry is %v out, want ~45m", d)
	}
}

func TestMute_ExpiredMuteSelfHeals(t *testing.T) {
	// A mute set before a call must never strand the operator in permanent
	// silence: once past expiry it reports unmuted AND removes the file.
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	past := strconv.FormatInt(time.Now().Add(-1*time.Minute).Unix(), 10)
	if err := os.WriteFile(MuteFile(), []byte(past+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if muted, _ := MuteStatus(); muted {
		t.Error("MuteStatus() = true for an expired mute, want false")
	}
	if _, err := os.Stat(MuteFile()); !os.IsNotExist(err) {
		t.Error("expired mute file still present, want it removed")
	}
}

func TestMute_UnparseableContentFailsSafe(t *testing.T) {
	// Garbage in the file must keep us SILENT rather than blurt into a call.
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(MuteFile(), []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if muted, _ := MuteStatus(); !muted {
		t.Error("MuteStatus() = false for unparseable content, want true (fail safe)")
	}
}

// --- Room override tests ---

func TestRoom_RoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if got := CurrentRoom(); got != "" {
		t.Errorf("CurrentRoom() = %q on fresh home, want empty", got)
	}
	if err := SetRoom("suite"); err != nil {
		t.Fatalf("SetRoom: %v", err)
	}
	if got := CurrentRoom(); got != "suite" {
		t.Errorf("CurrentRoom() = %q, want %q", got, "suite")
	}
	if err := SetRoom(""); err != nil {
		t.Fatalf("SetRoom(clear): %v", err)
	}
	if got := CurrentRoom(); got != "" {
		t.Errorf("CurrentRoom() = %q after clear, want empty", got)
	}
}

func TestRoom_TrailingNewlineIsTrimmed(t *testing.T) {
	// announce.sh writes/reads the same file; a stray newline must not become
	// part of the device name or the notify service path breaks.
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RoomFile(), []byte("  banheiro \n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := CurrentRoom(); got != "banheiro" {
		t.Errorf("CurrentRoom() = %q, want %q", got, "banheiro")
	}
}

func TestCallService_NonSuccessStatusIsAnError(t *testing.T) {
	// Regression guard: CallService used to discard the status code, so `ha say`
	// printed "Sent to <device>" for calls HA had rejected. A 401 from a stale
	// token or a 404 from an unknown notify service must reach the caller.
	for _, code := range []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusInternalServerError} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			w.Write([]byte(`{"message":"nope"}`))
		}))
		c := newTestClient(srv.URL)
		_, err := c.CallService("notify/alexa_media_echo_studio", `{"message":"hi"}`)
		if err == nil {
			t.Errorf("CallService with HTTP %d returned nil error, want an error", code)
		}
		srv.Close()
	}
}

func TestCallService_SuccessStatusesAreNotErrors(t *testing.T) {
	for _, code := range []int{http.StatusOK, http.StatusCreated, http.StatusNoContent} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		c := newTestClient(srv.URL)
		if _, err := c.CallService("switch/turn_off", `{}`); err != nil {
			t.Errorf("CallService with HTTP %d = %v, want nil", code, err)
		}
		srv.Close()
	}
}
