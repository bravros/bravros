package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bravros/bravros/cli/internal/secrets"
	"github.com/spf13/cobra"
)

// writeUpgradeMarker seeds a verify-install-pending marker in the shared bravros
// state dir under homeDir, simulating a real post-upgrade SessionStart.
func writeUpgradeMarker(t *testing.T, homeDir string) {
	t.Helper()
	stateDir := filepath.Join(homeDir, ".config", "bravros", "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(stateDir, verifyInstallMarker)
	content := "version=v9.9.9\nts=2026-06-21T00:00:00Z\n"
	if err := os.WriteFile(markerPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// withSecretsStatus swaps secretsStatusFn for the duration of the test, restoring
// it on cleanup. This injects a fake status so tests never shell out to a live op
// session or read the test host's ~/.zshenv.
func withSecretsStatus(t *testing.T, st secrets.StatusInfo) {
	t.Helper()
	prev := secretsStatusFn
	secretsStatusFn = func() secrets.StatusInfo { return st }
	t.Cleanup(func() { secretsStatusFn = prev })
}

// execVerifyInstallCheck runs `bravros hook verify-install-check` against a temp HOME dir
// and returns the captured stdout output.
func execVerifyInstallCheck(t *testing.T, homeDir string) string {
	t.Helper()

	t.Setenv("HOME", homeDir)

	buf := &bytes.Buffer{}

	root := &cobra.Command{Use: "bravros"}
	h := &cobra.Command{Use: "hook"}
	// Re-create child so we don't pollute shared state between tests.
	child := &cobra.Command{
		Use:  hookVerifyInstallCheckCmd.Use,
		RunE: hookVerifyInstallCheckCmd.RunE,
	}
	h.AddCommand(child)
	root.AddCommand(h)
	root.SetOut(buf)
	root.SetErr(&bytes.Buffer{}) // suppress cobra error output
	root.SetArgs([]string{"hook", "verify-install-check"})

	_ = root.Execute()
	return buf.String()
}

func TestVerifyInstallCheck_MarkerMissing(t *testing.T) {
	dir := t.TempDir()
	out := execVerifyInstallCheck(t, dir)
	if out != "" {
		t.Fatalf("expected no stdout output when marker absent, got %q", out)
	}
}

func TestVerifyInstallCheck_MarkerPresent(t *testing.T) {
	dir := t.TempDir()
	// Neutral backend keeps the secrets nudge out of this test's assertions.
	withSecretsStatus(t, secrets.StatusInfo{Backend: secrets.BackendEnv, SATokenBlock: "none"})

	stateDir := filepath.Join(dir, ".config", "bravros", "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(stateDir, verifyInstallMarker)
	content := "version=v1.10.2\nts=2026-04-14T05:00:00Z\n"
	if err := os.WriteFile(markerPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	out := execVerifyInstallCheck(t, dir)

	// Must be valid JSON.
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("expected valid JSON, got %q: %v", out, err)
	}

	hso, ok := result["hookSpecificOutput"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing hookSpecificOutput in JSON: %v", result)
	}

	hookEventName, _ := hso["hookEventName"].(string)
	if hookEventName != "SessionStart" {
		t.Errorf("hookEventName must be \"SessionStart\", got %q", hookEventName)
	}

	ctx, _ := hso["additionalContext"].(string)
	if ctx == "" {
		t.Fatal("additionalContext is empty")
	}
	if !containsStr(ctx, "v1.10.2") {
		t.Errorf("additionalContext should mention version v1.10.2, got %q", ctx)
	}
	if !containsStr(ctx, "/auto-verify-install") {
		t.Errorf("additionalContext should mention /auto-verify-install, got %q", ctx)
	}

	// Marker must be deleted.
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Error("expected marker to be deleted after command runs")
	}
}

func TestVerifyInstallCheck_LegacyClaudeMarkerFallback(t *testing.T) {
	dir := t.TempDir()
	withSecretsStatus(t, secrets.StatusInfo{Backend: secrets.BackendEnv, SATokenBlock: "none"})

	stateDir := filepath.Join(dir, ".claude", "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(stateDir, verifyInstallMarker)
	if err := os.WriteFile(markerPath, []byte("version=v1.11.0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out := execVerifyInstallCheck(t, dir)
	if !containsStr(out, "v1.11.0") {
		t.Fatalf("expected legacy marker version in output, got %q", out)
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Error("expected legacy marker to be deleted after fallback check")
	}
}

func TestVerifyInstallCheck_MalformedMarker(t *testing.T) {
	dir := t.TempDir()
	withSecretsStatus(t, secrets.StatusInfo{Backend: secrets.BackendEnv, SATokenBlock: "none"})

	stateDir := filepath.Join(dir, ".claude", "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(stateDir, verifyInstallMarker)
	// No "version=" line — parseMarkerVersion returns "unknown".
	if err := os.WriteFile(markerPath, []byte("not valid content at all\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out := execVerifyInstallCheck(t, dir)

	// Still emits JSON (version = "unknown").
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("expected valid JSON even for malformed marker, got %q: %v", out, err)
	}

	hso, _ := result["hookSpecificOutput"].(map[string]interface{})
	ctx, _ := hso["additionalContext"].(string)
	if !containsStr(ctx, "unknown") {
		t.Errorf("expected 'unknown' version in output for malformed marker, got %q", ctx)
	}

	// Marker deleted.
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Error("expected marker to be deleted even on malformed content")
	}
}

func TestParseMarkerVersion(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"normal", "version=v1.10.2\nts=2026-04-14T05:00:00Z\n", "v1.10.2"},
		{"no version line", "ts=2026-04-14T05:00:00Z\n", "unknown"},
		{"empty", "", "unknown"},
		{"version empty value", "version=\nts=...\n", "unknown"},
		{"version with spaces", "version=  v1.9.0  \nts=...\n", "v1.9.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMarkerVersion(tt.content)
			if got != tt.want {
				t.Errorf("parseMarkerVersion(%q) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}
}

func containsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ── install-codex tests ───────────────────────────────────────────────────────

// ── /secrets-setup post-upgrade nudge (piggybacks verify-install one-shot) ─────

const secretsSetupSentinel = "/secrets-setup"

func secretsSetupNudgedMarkerExists(home string) bool {
	for _, p := range secretsSetupNudgedMarkerPaths(home) {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// (a) upgrade + op backend + SATokenBlock=="none" + not-yet-nudged ⇒ the
// /secrets-setup sentence is present AND the permanent marker is written.
func TestVerifyInstallCheck_SecretsNudge_Fires(t *testing.T) {
	dir := t.TempDir()
	writeUpgradeMarker(t, dir)
	withSecretsStatus(t, secrets.StatusInfo{Backend: secrets.BackendOp, SATokenBlock: "none"})

	out := execVerifyInstallCheck(t, dir)

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("expected valid JSON, got %q: %v", out, err)
	}
	hso, _ := result["hookSpecificOutput"].(map[string]interface{})
	ctx, _ := hso["additionalContext"].(string)
	if !containsStr(ctx, secretsSetupSentinel) {
		t.Errorf("expected additionalContext to mention %s, got %q", secretsSetupSentinel, ctx)
	}
	if !containsStr(ctx, "/auto-verify-install") {
		t.Errorf("verify-install sentence must still be present, got %q", ctx)
	}
	if !secretsSetupNudgedMarkerExists(dir) {
		t.Error("expected the permanent .secrets-setup-nudged marker to be written")
	}
}

// keychain backend behaves identically to op for the nudge predicate.
func TestVerifyInstallCheck_SecretsNudge_FiresKeychain(t *testing.T) {
	dir := t.TempDir()
	writeUpgradeMarker(t, dir)
	withSecretsStatus(t, secrets.StatusInfo{Backend: secrets.BackendKeychain, SATokenBlock: "none"})

	out := execVerifyInstallCheck(t, dir)
	if !containsStr(out, secretsSetupSentinel) {
		t.Errorf("keychain backend should also nudge, got %q", out)
	}
	if !secretsSetupNudgedMarkerExists(dir) {
		t.Error("expected the permanent marker to be written for keychain backend")
	}
}

// (b) upgrade + SATokenBlock!="none" (already configured) ⇒ NO secrets sentence,
// and no marker is written.
func TestVerifyInstallCheck_SecretsNudge_Configured(t *testing.T) {
	dir := t.TempDir()
	writeUpgradeMarker(t, dir)
	withSecretsStatus(t, secrets.StatusInfo{Backend: secrets.BackendOp, SATokenBlock: "op"})

	out := execVerifyInstallCheck(t, dir)
	if containsStr(out, secretsSetupSentinel) {
		t.Errorf("configured SA-token block must NOT nudge, got %q", out)
	}
	if secretsSetupNudgedMarkerExists(dir) {
		t.Error("no marker should be written when already configured")
	}
}

// (c) upgrade + already-nudged marker present ⇒ NO secrets sentence (once-EVER).
func TestVerifyInstallCheck_SecretsNudge_AlreadyNudged(t *testing.T) {
	dir := t.TempDir()
	writeUpgradeMarker(t, dir)
	withSecretsStatus(t, secrets.StatusInfo{Backend: secrets.BackendOp, SATokenBlock: "none"})

	// Pre-seed the permanent marker.
	stateDir := filepath.Join(dir, ".config", "bravros", "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, secretsSetupNudgedMarker), []byte("nudged\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out := execVerifyInstallCheck(t, dir)
	if containsStr(out, secretsSetupSentinel) {
		t.Errorf("already-nudged machine must NOT nudge again, got %q", out)
	}
}

// (d) upgrade + backend=="none"/"env" ⇒ NO secrets sentence.
func TestVerifyInstallCheck_SecretsNudge_NonOpBackend(t *testing.T) {
	for _, backend := range []secrets.Backend{secrets.BackendNone, secrets.BackendEnv} {
		t.Run(string(backend), func(t *testing.T) {
			dir := t.TempDir()
			writeUpgradeMarker(t, dir)
			withSecretsStatus(t, secrets.StatusInfo{Backend: backend, SATokenBlock: "none"})

			out := execVerifyInstallCheck(t, dir)
			if containsStr(out, secretsSetupSentinel) {
				t.Errorf("backend %q must NOT nudge, got %q", backend, out)
			}
			if secretsSetupNudgedMarkerExists(dir) {
				t.Errorf("backend %q must not write the nudge marker", backend)
			}
		})
	}
}

// (e) NO verify-install marker (normal session) ⇒ no emit at all, even when the
// secrets predicate would otherwise be true.
func TestVerifyInstallCheck_SecretsNudge_NoEmitOnNormalSession(t *testing.T) {
	dir := t.TempDir()
	// No upgrade marker seeded.
	withSecretsStatus(t, secrets.StatusInfo{Backend: secrets.BackendOp, SATokenBlock: "none"})

	out := execVerifyInstallCheck(t, dir)
	if out != "" {
		t.Fatalf("normal session (no upgrade marker) must emit nothing, got %q", out)
	}
	if secretsSetupNudgedMarkerExists(dir) {
		t.Error("normal session must not write the nudge marker")
	}
}
