package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// clearAgentEnv removes every agent-specific variable, so the tests prove the
// stand-down depends on none of them.
func clearAgentEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BRAVROS_POLICE_STANDDOWN", "")
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	return home
}

func TestStandDownTTLParsing(t *testing.T) {
	ok := map[string]time.Duration{
		"":      4 * time.Hour,
		"4h":    4 * time.Hour,
		"90m":   90 * time.Minute,
		"1h30m": 90 * time.Minute,
		"6":     6 * time.Hour,
		" 2 ":   2 * time.Hour,
		"72":    72 * time.Hour,
	}
	for in, want := range ok {
		got, err := parseStandDownTTL(in)
		if err != nil || got != want {
			t.Errorf("parseStandDownTTL(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"0", "-1", "-2h", "abc", "73", "100h"} {
		if _, err := parseStandDownTTL(in); err == nil {
			t.Errorf("parseStandDownTTL(%q): want error", in)
		}
	}
}

// TestStandDownOnBareIntegerTTL: `--ttl 6` writes a marker ~6h out.
func TestStandDownOnBareIntegerTTL(t *testing.T) {
	clearAgentEnv(t)
	standDownTTLFlag = "6"
	t.Cleanup(func() { standDownTTLFlag = "4h" })
	policeStandDownOnCmd.SetOut(&bytes.Buffer{})
	if err := policeStandDownOnCmd.RunE(policeStandDownOnCmd, nil); err != nil {
		t.Fatalf("on: %v", err)
	}
	m, ok := readStandDown()
	if !ok {
		t.Fatal("marker not live after on")
	}
	if left := time.Until(m.ExpiresAt); left < 5*time.Hour+59*time.Minute || left > 6*time.Hour {
		t.Errorf("expires in %v; want ~6h", left)
	}
	if m.Scope != "machine" {
		t.Errorf("scope = %q; want machine", m.Scope)
	}
}

// TestStandDownIsAgentAgnostic: a marker written from one session is honoured
// by a hook invocation from any other session, or from an agent with no
// session env at all (Codex, Antigravity, Grok Build).
func TestStandDownIsAgentAgnostic(t *testing.T) {
	home := clearAgentEnv(t)
	laneRepo(t, "")

	t.Setenv("CLAUDE_CODE_SESSION_ID", "lead-session")
	policeStandDownOnCmd.SetOut(&bytes.Buffer{})
	if err := policeStandDownOnCmd.RunE(policeStandDownOnCmd, nil); err != nil {
		t.Fatalf("on: %v", err)
	}

	for _, sess := range []string{"worker-session-2", ""} {
		t.Setenv("CLAUDE_CODE_SESSION_ID", sess)
		payload := `{"tool_name":"Bash","session_id":"some-other-agent","tool_input":{"command":"git push origin main"}}`
		var out bytes.Buffer
		policePreToolUseCmd.SetIn(strings.NewReader(payload))
		policePreToolUseCmd.SetOut(&out)
		if err := policePreToolUseCmd.RunE(policePreToolUseCmd, nil); err != nil {
			t.Fatalf("RunE: %v", err)
		}
		if out.Len() != 0 {
			t.Errorf("session %q: stand-down must suppress the merge gate, got %q", sess, out.String())
		}
	}

	log, err := os.ReadFile(home + "/.claude/state/police-standdown-audit.log")
	if err != nil {
		t.Fatalf("audit log: %v", err)
	}
	if n := strings.Count(string(log), "git push origin main"); n != 2 || !strings.Contains(string(log), "merge-gate") {
		t.Errorf("audit log must record both suppressed pushes: %q", log)
	}

	// Without the marker the same push is blocked again.
	policeStandDownOffCmd.SetOut(&bytes.Buffer{})
	if err := policeStandDownOffCmd.RunE(policeStandDownOffCmd, nil); err != nil {
		t.Fatalf("off: %v", err)
	}
	if blocked, _, _ := rule52TestInvoke(t, "git push origin main"); !blocked {
		t.Error("after off, git push origin main must block")
	}
}

// TestStandDownExpiryReEnforces: an expired marker is ignored and removed.
func TestStandDownExpiryReEnforces(t *testing.T) {
	clearAgentEnv(t)
	laneRepo(t, "")
	writeStandDownMarker(t, time.Now().Add(-time.Minute))
	if blocked, _, _ := rule52TestInvoke(t, "git push origin main"); !blocked {
		t.Error("expired stand-down must not suppress the merge gate")
	}
	if _, err := os.Stat(standDownPath()); !os.IsNotExist(err) {
		t.Error("expired marker must be auto-cleaned")
	}
}

// TestStandDownFloorStillBlocks: the safety floor never reads the marker.
func TestStandDownFloorStillBlocks(t *testing.T) {
	clearAgentEnv(t)
	laneRepo(t, "")
	writeStandDownMarker(t, time.Now().Add(time.Hour))
	if !isStandDownActive() {
		t.Fatal("marker must be live")
	}
	cases := map[string]string{
		"git commit -m \"fix: x\n\nCo-Authored-By: Claude <noreply@anthropic.com>\"": "AI signature",
		"touch ~/.claude/state/police-token":                                         "gate's own input",
		"printf '{}' > .bravros/config.json":                                         "gate's own input",
		"echo '{}' > .planning/.review-stamp-3.json":                                 "gate's own input",
	}
	for cmd, want := range cases {
		blocked, _, reason := rule52TestInvoke(t, cmd)
		if !blocked || !strings.Contains(reason, want) {
			t.Errorf("%q: floor must block with %q under stand-down; blocked=%v %q", cmd, want, blocked, reason)
		}
	}
}

// TestStandDownMarkerIsNotAGateInput: the agent may turn stand-down on/off
// itself, and touching the marker from Bash is not a token forgery.
func TestStandDownMarkerIsNotAGateInput(t *testing.T) {
	clearAgentEnv(t)
	for _, cmd := range []string{
		"bravros police standdown on --ttl 6 --reason \"workflow\"",
		"bravros police standdown off",
		"rm -f ~/.claude/state/police-standdown.json",
		"cat ~/.claude/state/police-standdown.json ~/.claude/state/police-standdown-audit.log",
	} {
		if msg := checkGateInputWrite(cmd); msg != "" {
			t.Errorf("%q must pass the gate-input floor: %s", cmd, msg)
		}
	}
}

func TestStandDownStatusReportsRemaining(t *testing.T) {
	clearAgentEnv(t)
	writeStandDownMarker(t, time.Now().Add(90*time.Minute))
	var out bytes.Buffer
	policeStandDownStatusCmd.SetOut(&out)
	if err := policeStandDownStatusCmd.RunE(policeStandDownStatusCmd, nil); err != nil {
		t.Fatal(err)
	}
	var st struct {
		Active    bool   `json:"active"`
		Scope     string `json:"scope"`
		ExpiresAt string `json:"expires_at"`
		Remaining string `json:"remaining"`
	}
	if err := json.Unmarshal(out.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	d, err := time.ParseDuration(st.Remaining)
	if !st.Active || st.Scope != "machine" || st.ExpiresAt == "" || err != nil || d < 89*time.Minute || d > 90*time.Minute {
		t.Errorf("status = %+v (remaining parse err %v)", st, err)
	}
}
