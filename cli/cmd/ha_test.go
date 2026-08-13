package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bravros/bravros/cli/internal/ha"
)

// sendHASay's mute gate is the only branch reachable without a live HA client — it
// deliberately returns before NewClient() so a muted host needs no HASS_TOKEN to stay
// quiet. Everything past that point requires a real server and is covered in
// internal/ha via httptest.

func TestSendHASay_MutedSkipsWithoutClient(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// No HASS_TOKEN on purpose: if the mute gate ever moves below NewClient(), this
	// test fails with a token error instead of passing, which is the signal we want.
	t.Setenv("HASS_TOKEN", "")

	if err := ha.SetMute(0); err != nil {
		t.Fatalf("SetMute: %v", err)
	}

	var buf bytes.Buffer
	if err := sendHASay("deploy pronto", "suite", false, false, &buf); err != nil {
		t.Fatalf("sendHASay while muted = %v, want nil", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Skipped") {
		t.Errorf("output = %q, want it to report the message was skipped", out)
	}
	if strings.Contains(out, "Sent to") {
		t.Errorf("output = %q, must not claim delivery while muted", out)
	}
}

func TestSendHASay_MuteBeatsForce(t *testing.T) {
	// --force bypasses the Mac-unlock presence gate only. Mute is an explicit operator
	// action and must outrank it, matching scripts/announce.sh ("kill-switch wins over
	// every route").
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HASS_TOKEN", "")
	if err := ha.SetMute(0); err != nil {
		t.Fatalf("SetMute: %v", err)
	}

	var buf bytes.Buffer
	if err := sendHASay("urgente", "studio", true /* force */, false, &buf); err != nil {
		t.Fatalf("sendHASay --force while muted = %v, want nil", err)
	}
	if strings.Contains(buf.String(), "Sent to") {
		t.Errorf("--force bypassed the mute kill-switch: %q", buf.String())
	}
}

func TestSendHASay_TimedMuteReportsExpiryInOutput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HASS_TOKEN", "")
	if err := ha.SetMute(45 * time.Minute); err != nil {
		t.Fatalf("SetMute: %v", err)
	}

	var buf bytes.Buffer
	if err := sendHASay("oi", "suite", false, false, &buf); err != nil {
		t.Fatalf("sendHASay: %v", err)
	}
	if !strings.Contains(buf.String(), "muted until") {
		t.Errorf("output = %q, want the expiry time so the operator knows when it lifts", buf.String())
	}
}

func TestSendHASay_UnmutedFallsThroughToClient(t *testing.T) {
	// Guard against the mute gate accidentally swallowing every send: with no mute set
	// and no token, we must reach NewClient() and surface its error.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HASS_TOKEN", "")

	var buf bytes.Buffer
	err := sendHASay("oi", "suite", false, false, &buf)
	if err == nil {
		t.Fatal("sendHASay unmuted with no HASS_TOKEN = nil, want the NewClient error")
	}
	if strings.Contains(buf.String(), "Skipped") {
		t.Errorf("unmuted send reported Skipped: %q", buf.String())
	}
}

// --- room / mute state files are the contract shared with scripts/announce.sh ---

func TestRoomFile_LivesWhereAnnounceScriptLooks(t *testing.T) {
	// announce.sh reads these paths literally; if they move, room routing and mute
	// silently stop working on any host whose bravros binary is newer than its scripts.
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got, want := ha.RoomFile(), filepath.Join(home, ".claude", ".echo-room"); got != want {
		t.Errorf("RoomFile() = %q, want %q", got, want)
	}
	if got, want := ha.MuteFile(), filepath.Join(home, ".claude", ".mute"); got != want {
		t.Errorf("MuteFile() = %q, want %q", got, want)
	}
}

func TestRoomFile_FormatIsReadableByShell(t *testing.T) {
	// announce.sh does `tr -d '[:space:]' < .echo-room`, so a bare device name plus a
	// trailing newline is the whole contract.
	t.Setenv("HOME", t.TempDir())
	if err := ha.SetRoom("banheiro"); err != nil {
		t.Fatalf("SetRoom: %v", err)
	}
	b, err := os.ReadFile(ha.RoomFile())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(b) != "banheiro\n" {
		t.Errorf("room file = %q, want %q", string(b), "banheiro\n")
	}
}

func TestMuteFile_IndefiniteFormatIsForeverLiteral(t *testing.T) {
	// announce.sh and mute-announce.sh both special-case the literal "forever"; a change
	// here would make an indefinite mute parse as garbage (which fails safe, but reports
	// the wrong state to the operator).
	t.Setenv("HOME", t.TempDir())
	if err := ha.SetMute(0); err != nil {
		t.Fatalf("SetMute: %v", err)
	}
	b, err := os.ReadFile(ha.MuteFile())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.TrimSpace(string(b)) != "forever" {
		t.Errorf("mute file = %q, want %q", string(b), "forever")
	}
}
