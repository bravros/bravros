package ha

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DeviceMap maps friendly names to HA notify service paths.
var DeviceMap = map[string]string{
	"studio":   "notify/alexa_media_echo_studio",
	"sala":     "notify/alexa_media_echo_dot_sala",
	"suite":    "notify/alexa_media_echo_show_suite",
	"banheiro": "notify/alexa_media_echo_banheiro_suite",
	"gourmet":  "notify/alexa_media_echo_area_gourmet",
	"todos":    "notify/alexa_media_todo_lugar",
}

// ColorMap maps color names to RGB arrays.
var ColorMap = map[string][3]int{
	"blue":   {0, 0, 255},
	"red":    {255, 0, 0},
	"green":  {0, 255, 0},
	"yellow": {255, 200, 0},
	"white":  {255, 255, 255},
}

// StudioLights are the entity IDs for studio light group.
var StudioLights = []string{
	"light.teto_do_estudio",
	"light.luz_teto_estudio_armario",
	"light.luz_teto_estudio",
}

// ResolveDevice returns the HA service path for a device name.
func ResolveDevice(name string) string {
	if svc, ok := DeviceMap[name]; ok {
		return svc
	}
	return "notify/alexa_media_" + name
}

// DeviceSlug returns the HA entity slug for a device name — the middle part shared by that
// device's entities (media_player.<slug>, switch.<slug>_do_not_disturb_switch, …). Derived
// from DeviceMap so the two can never drift apart. Mirrored by device_slug() in
// scripts/announce.sh, which needs the same mapping without invoking the binary.
func DeviceSlug(name string) string {
	svc, ok := DeviceMap[name]
	if !ok {
		return name
	}
	return strings.TrimPrefix(svc, "notify/alexa_media_")
}

// RoomFile is the path of the current-room override consumed by both `bravros ha room` and
// scripts/announce.sh.
func RoomFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", ".echo-room")
}

// CurrentRoom reads the room override, or "" when unset.
func CurrentRoom() string {
	path := RoomFile()
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// MuteFile is the path of the global announcement kill-switch. Its presence silences EVERY
// audio surface — Echo and the local macOS `say` fallback alike. Silencing both is
// deliberate: mute exists for calls and meetings, where a laptop speaking aloud would be
// picked up by the microphone and is worse than the Echo.
// Contents: a Unix expiry timestamp, or "forever" for an indefinite mute.
func MuteFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", ".mute")
}

// MuteStatus reports whether announcements are currently muted, and until when.
// An expired mute is self-healing: the file is removed and the mute reported as inactive,
// so a "mute 30m" before a call never strands the operator in permanent silence.
// A zero `until` with muted=true means an indefinite mute.
func MuteStatus() (muted bool, until time.Time) {
	path := MuteFile()
	if path == "" {
		return false, time.Time{}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false, time.Time{}
	}
	raw := strings.TrimSpace(string(b))
	if raw == "" || raw == "forever" {
		return true, time.Time{}
	}
	epoch, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return true, time.Time{} // unparseable → fail safe (stay muted)
	}
	exp := time.Unix(epoch, 0)
	if time.Now().After(exp) {
		os.Remove(path)
		return false, time.Time{}
	}
	return true, exp
}

// SetMute enables the kill-switch. A zero duration mutes indefinitely.
func SetMute(d time.Duration) error {
	path := MuteFile()
	if path == "" {
		return fmt.Errorf("cannot resolve home directory")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := "forever"
	if d > 0 {
		body = strconv.FormatInt(time.Now().Add(d).Unix(), 10)
	}
	return os.WriteFile(path, []byte(body+"\n"), 0o644)
}

// ClearMute lifts the kill-switch. Clearing an already-unmuted state is not an error.
func ClearMute() error {
	path := MuteFile()
	if path == "" {
		return fmt.Errorf("cannot resolve home directory")
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// SetRoom writes the room override. An empty name clears it.
func SetRoom(name string) error {
	path := RoomFile()
	if path == "" {
		return fmt.Errorf("cannot resolve home directory")
	}
	if name == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(name+"\n"), 0o644)
}
