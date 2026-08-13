// Package host provides host-identity helpers for the bravros CLI.
// Use IsMacStudio() to gate features that only make sense on the primary
// Mac Studio workstation (e.g., Alexa TTS announce.sh deployment).
package host

import (
	"os"
	"os/exec"
	"strings"
)

// IsMacStudio reports whether the current host is a Mac Studio workstation.
//
// Detection logic (OR of two conditions):
//  1. `hostname -s` matches *Mac-Studio* or *MacStudio* (case-insensitive).
//     This covers the standard macOS hostname format used on the primary
//     MacStudio machine (e.g. "Shirleysons-Mac-Studio").
//  2. BRAVROS_TTS=1 env var is set — forward-extension hook for headless /
//     non-standard hostnames that still want TTS features.
//     (Not yet consumed by selfupdate scripts/ drift — stub for Phase 2.)
func IsMacStudio() bool {
	// Condition 2: explicit opt-in env override.
	if os.Getenv("BRAVROS_TTS") == "1" {
		return true
	}

	// Condition 1: hostname glob match.
	out, err := exec.Command("hostname", "-s").Output()
	if err != nil {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(string(out)))
	return strings.Contains(lower, "mac-studio") || strings.Contains(lower, "macstudio")
}
