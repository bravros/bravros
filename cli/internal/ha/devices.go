package ha

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DevicesFile is the path of the operator's local room→Echo mapping.
//
// This file is deliberately NOT part of the repo. `bravros/bravros` is a PUBLIC mirror of
// this tree, so a hardcoded map here publishes the operator's home layout — which room has
// which Echo, and the entity IDs of their lights. Keeping it on disk means one binary serves
// everyone and no personal topology ever ships.
//
// Shape (see templates/ha-devices.example.json):
//
//	{"devices": {"kitchen": "echo_kitchen"}, "studio_lights": ["light.office_ceiling"]}
//
// The values are HA **entity slugs**, not notify-service names — see ResolveDevice.
func DevicesFile() string {
	if p := os.Getenv("BRAVROS_HA_DEVICES"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "ha-devices.json")
}

type devicesConfig struct {
	Devices      map[string]string `json:"devices"`
	StudioLights []string          `json:"studio_lights"`
}

var (
	devicesOnce sync.Once
	devicesCfg  devicesConfig
)

// loadDevices reads DevicesFile once. A missing or malformed file is not an error: the
// caller falls back to treating the room name as its own slug, which keeps `bravros ha say`
// usable on a fresh machine that has no mapping yet.
func loadDevices() devicesConfig {
	devicesOnce.Do(func() {
		path := DevicesFile()
		if path == "" {
			return
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return
		}
		_ = json.Unmarshal(b, &devicesCfg)
	})
	return devicesCfg
}

// resetDevicesCache clears the memoised config so a test can point BRAVROS_HA_DEVICES at a
// fresh fixture. Not exported: production code loads the map exactly once per process.
func resetDevicesCache() {
	devicesOnce = sync.Once{}
	devicesCfg = devicesConfig{}
}

// DeviceMap returns the room→entity-slug mapping from the operator's local config.
// Empty when no config file exists.
func DeviceMap() map[string]string {
	return loadDevices().Devices
}

// ColorMap maps color names to RGB arrays.
var ColorMap = map[string][3]int{
	"blue":   {0, 0, 255},
	"red":    {255, 0, 0},
	"green":  {0, 255, 0},
	"yellow": {255, 200, 0},
	"white":  {255, 255, 255},
}

// StudioLights returns the entity IDs of the studio light group, from local config.
// Empty when unconfigured — `bravros ha lights` then reports nothing rather than acting on
// entity IDs that do not exist on this operator's HA.
func StudioLights() []string {
	return loadDevices().StudioLights
}

// ResolveDevice returns the HA service path used to speak to a device.
//
// It is always the GENERIC `notify/alexa_media` service, with the destination carried in the
// payload's `target` (see DeviceTarget). The per-device `notify/alexa_media_<name>` services
// are NOT used, because Alexa Media Player derives those names from the device name in the
// Alexa account: renaming an Echo in the Alexa app deletes its notify service on the next
// integration reload, and every announcement to it goes silent with no error at the call
// site. Entity IDs are registry-stable and survive renames, so targeting one cannot break
// that way. (Observed 2026-09-02: renaming "Echo Studio" to "Estúdio Echo Dot" left
// the device's old notify service alive only until the next reload.)
func ResolveDevice(name string) string {
	return "notify/alexa_media"
}

// DeviceTarget returns the stable media_player entity ID for a device name — the rename-proof
// destination passed as `target` in the notify payload.
func DeviceTarget(name string) string {
	slug := DeviceSlug(name)
	if slug == "" {
		return ""
	}
	return "media_player." + slug
}

// DeviceSlug returns the HA entity slug for a device name — the middle part shared by that
// device's entities (media_player.<slug>, switch.<slug>_do_not_disturb_switch, …). Derived
// from DeviceMap so the two can never drift apart. Mirrored by device_slug() in
// scripts/announce.sh, which needs the same mapping without invoking the binary.
func DeviceSlug(name string) string {
	if slug, ok := DeviceMap()[name]; ok && slug != "" {
		return slug
	}
	return name
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
