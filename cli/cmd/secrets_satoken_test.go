package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resetSATokenFlags clears the package-level flag vars between sub-tests since
// they persist across RunE invocations.
func resetSATokenFlags() {
	saTokenBackend = ""
	saTokenOpRef = ""
	saTokenVault = ""
	saTokenItem = ""
	saTokenField = ""
	saTokenZshenv = ""
	saTokenInteractive = false
	saTokenJSON = false
}

// TestSATokenCmd_OpBackend_WritesLookup drives the verb with --backend op and a
// user-provided op-ref, asserting the lookup line lands in the target file.
func TestSATokenCmd_OpBackend_WritesLookup(t *testing.T) {
	resetSATokenFlags()
	dir := t.TempDir()
	zshenv := filepath.Join(dir, ".zshenv")
	saTokenBackend = "op"
	saTokenOpRef = "op://MyVault/Claude SA Token - TestHost/token"
	saTokenZshenv = zshenv

	secretsSATokenCmd.SetIn(strings.NewReader(""))
	secretsSATokenCmd.SetOut(&bytes.Buffer{})
	secretsSATokenCmd.SetErr(&bytes.Buffer{})
	if err := secretsSATokenCmd.RunE(secretsSATokenCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	got, _ := os.ReadFile(zshenv)
	if !strings.Contains(string(got), "op read 'op://MyVault/Claude SA Token - TestHost/token'") {
		t.Errorf("op lookup missing:\n%s", got)
	}
	// No owner literal baked into the code path — the ref came purely from flags.
}

// TestSATokenCmd_PlaintextFromStdin reads the token from stdin (never an arg).
func TestSATokenCmd_PlaintextFromStdin(t *testing.T) {
	resetSATokenFlags()
	dir := t.TempDir()
	zshenv := filepath.Join(dir, ".zshenv")
	saTokenBackend = "plaintext"
	saTokenZshenv = zshenv

	secretsSATokenCmd.SetIn(strings.NewReader("ops_FROMSTDIN\n"))
	secretsSATokenCmd.SetOut(&bytes.Buffer{})
	secretsSATokenCmd.SetErr(&bytes.Buffer{})
	if err := secretsSATokenCmd.RunE(secretsSATokenCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	got, _ := os.ReadFile(zshenv)
	if !strings.Contains(string(got), "ops_FROMSTDIN") {
		t.Errorf("plaintext literal missing:\n%s", got)
	}
}

// TestSATokenCmd_PlaintextOverKeychain_Refused surfaces the clobber guard error
// from the verb (the regression guard at the CLI boundary).
func TestSATokenCmd_PlaintextOverKeychain_Refused(t *testing.T) {
	resetSATokenFlags()
	dir := t.TempDir()
	zshenv := filepath.Join(dir, ".zshenv")
	// Pre-seed a keychain lookup block by hand (no security needed — pure text).
	keychainBlock := strings.Join([]string{
		"# >>> kaisser sa-token (v1) >>>",
		`export OP_CLAUDE_CODE_SERVICE_ACCOUNT_SECRET="$(/usr/bin/security find-generic-password -a "$USER" -s 'claude-sa-test' -w 2>/dev/null)"`,
		`export OP_SERVICE_ACCOUNT_TOKEN="$OP_CLAUDE_CODE_SERVICE_ACCOUNT_SECRET"`,
		"# <<< kaisser sa-token (v1) <<<",
		"",
	}, "\n")
	if err := os.WriteFile(zshenv, []byte(keychainBlock), 0644); err != nil {
		t.Fatal(err)
	}

	saTokenBackend = "plaintext"
	saTokenZshenv = zshenv
	secretsSATokenCmd.SetIn(strings.NewReader("ops_LEAK\n"))
	secretsSATokenCmd.SetOut(&bytes.Buffer{})
	secretsSATokenCmd.SetErr(&bytes.Buffer{})

	err := secretsSATokenCmd.RunE(secretsSATokenCmd, nil)
	if err == nil {
		t.Fatal("expected clobber-guard error, got nil")
	}
	got, _ := os.ReadFile(zshenv)
	if strings.Contains(string(got), "ops_LEAK") {
		t.Errorf("plaintext literal leaked despite guard:\n%s", got)
	}
}

// TestSATokenCmd_MissingBackend errors cleanly.
func TestSATokenCmd_MissingBackend(t *testing.T) {
	resetSATokenFlags()
	saTokenZshenv = filepath.Join(t.TempDir(), ".zshenv")
	secretsSATokenCmd.SetIn(strings.NewReader(""))
	secretsSATokenCmd.SetOut(&bytes.Buffer{})
	secretsSATokenCmd.SetErr(&bytes.Buffer{})
	if err := secretsSATokenCmd.RunE(secretsSATokenCmd, nil); err == nil {
		t.Error("expected error when --backend is missing")
	}
}

// TestSATokenCmd_OpBackend_MissingCoords errors when neither op-ref nor
// vault+item are provided.
func TestSATokenCmd_OpBackend_MissingCoords(t *testing.T) {
	resetSATokenFlags()
	saTokenBackend = "op"
	saTokenZshenv = filepath.Join(t.TempDir(), ".zshenv")
	secretsSATokenCmd.SetIn(strings.NewReader(""))
	secretsSATokenCmd.SetOut(&bytes.Buffer{})
	secretsSATokenCmd.SetErr(&bytes.Buffer{})
	if err := secretsSATokenCmd.RunE(secretsSATokenCmd, nil); err == nil {
		t.Error("expected error when op coordinates are missing")
	}
}
