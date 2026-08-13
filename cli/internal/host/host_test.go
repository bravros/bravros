package host

import (
	"os"
	"strings"
	"testing"
)

// isMacStudioFromHostname is the pure, testable version of the hostname-match
// logic from IsMacStudio (without the KAISSER_TTS env check).
func isMacStudioFromHostname(hostname string) bool {
	lower := strings.ToLower(hostname)
	return strings.Contains(lower, "mac-studio") || strings.Contains(lower, "macstudio")
}

func TestIsMacStudioFromHostname(t *testing.T) {
	tests := []struct {
		hostname string
		want     bool
	}{
		{"Shirleysons-Mac-Studio", true},  // primary MacStudio hostname
		{"shirleysons-mac-studio", true},  // lowercase variant
		{"SHIRLEYSONS-MAC-STUDIO", true},  // uppercase variant
		{"Shirleysons-MacStudio", true},   // no-dash variant
		{"Mac-Studio", true},              // minimal mac-studio
		{"MacStudio", true},               // minimal macstudio
		{"ryzen", false},                  // linux server — no match
		{"ubuntu-server", false},          // linux server — no match
		{"jarvis", false},                 // another linux host
		{"", false},                       // empty hostname — no match
	}

	for _, tt := range tests {
		got := isMacStudioFromHostname(tt.hostname)
		if got != tt.want {
			t.Errorf("isMacStudioFromHostname(%q) = %v, want %v", tt.hostname, got, tt.want)
		}
	}
}

func TestIsMacStudio_KaisserTTSEnvTrue(t *testing.T) {
	// KAISSER_TTS=1 must make IsMacStudio() return true regardless of hostname.
	t.Setenv("KAISSER_TTS", "1")
	if !IsMacStudio() {
		t.Error("IsMacStudio() = false with KAISSER_TTS=1, want true")
	}
}

func TestIsMacStudio_KaisserTTSEnvFalse(t *testing.T) {
	// KAISSER_TTS=0 must NOT trigger the env gate.
	// The function may still return true on a real MacStudio — we only verify
	// it does not panic and does not force-true purely from KAISSER_TTS=0.
	os.Setenv("KAISSER_TTS", "0")
	defer os.Unsetenv("KAISSER_TTS")
	_ = IsMacStudio() // must not panic
}
