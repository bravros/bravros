package secrets

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// assertShellParses runs `sh -n` (plus bash/zsh when present) on a rendered
// block file and fails the test on any syntax error.
func assertShellParses(t *testing.T, file, block string) {
	t.Helper()
	shells := []string{}
	for _, sh := range []string{"sh", "bash", "zsh"} {
		if _, err := exec.LookPath(sh); err == nil {
			shells = append(shells, sh)
		}
	}
	if len(shells) == 0 {
		t.Skip("no shell available to syntax-check")
	}
	for _, sh := range shells {
		if out, err := exec.Command(sh, "-n", file).CombinedOutput(); err != nil {
			t.Errorf("%s -n failed: %v\n%s\n--- block ---\n%s", sh, err, out, block)
		}
	}
}

// TestRenderSATokenBlock_Backends: each backend renders the expected lookup
// shape with the swarm gate, and op/keychain blocks carry no plaintext literal.
func TestRenderSATokenBlock_Backends(t *testing.T) {
	origMachine := machineNameFn
	machineNameFn = func() string { return "testhost" }
	defer func() { machineNameFn = origMachine }()

	// op (full ref)
	b, err := RenderSATokenBlock(SATokenSpec{Backend: SABackendOp, OpRef: "op://MyVault/Claude SA/token"}, lookupNone)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b, "op read 'op://MyVault/Claude SA/token'") {
		t.Errorf("op block missing user-provided ref:\n%s", b)
	}
	if !strings.Contains(b, swarmGate) {
		t.Error("op block missing swarm gate")
	}

	// op (vault/item/field trio, default field)
	b, _ = RenderSATokenBlock(SATokenSpec{Backend: SABackendOp, OpVault: "MyVault", OpItem: "Claude SA Token - TestHost"}, lookupNone)
	if !strings.Contains(b, "op://MyVault/Claude SA Token - TestHost/credential") {
		t.Errorf("op trio block wrong ref:\n%s", b)
	}

	// keychain — machine-derived service, no plaintext token in the block.
	b, _ = RenderSATokenBlock(SATokenSpec{Backend: SABackendKeychain, Token: "ops_SECRET"}, lookupNone)
	if !strings.Contains(b, "security find-generic-password -a \"$USER\" -s 'claude-sa-testhost'") {
		t.Errorf("keychain block wrong lookup:\n%s", b)
	}
	if strings.Contains(b, "ops_SECRET") {
		t.Errorf("keychain block must NOT contain the literal token:\n%s", b)
	}

	// plaintext — literal present.
	b, _ = RenderSATokenBlock(SATokenSpec{Backend: SABackendPlaintext, Token: "ops_LITERAL"}, lookupNone)
	if !strings.Contains(b, "'ops_LITERAL'") {
		t.Errorf("plaintext block missing literal:\n%s", b)
	}

	// none — empty block.
	b, _ = RenderSATokenBlock(SATokenSpec{Backend: SABackendNone}, lookupNone)
	if b != "" {
		t.Errorf("none backend should render empty, got:\n%s", b)
	}
}

// TestRenderSATokenBlock_ClobberGuard: the load-bearing regression test. A
// plaintext write MUST be refused when an existing keychain OR op lookup is
// present — never downgrade an encrypted-at-rest lookup to a literal.
func TestRenderSATokenBlock_ClobberGuard(t *testing.T) {
	for _, existing := range []existingLookupKind{lookupKeychain, lookupOp} {
		_, err := RenderSATokenBlock(SATokenSpec{Backend: SABackendPlaintext, Token: "ops_X"}, existing)
		if err == nil {
			t.Errorf("existing=%v: expected clobber-guard error, got nil", existing)
			continue
		}
		if _, ok := err.(*ErrClobberGuard); !ok {
			t.Errorf("existing=%v: expected *ErrClobberGuard, got %T: %v", existing, err, err)
		}
	}

	// Plaintext OVER plaintext is allowed (no downgrade).
	if _, err := RenderSATokenBlock(SATokenSpec{Backend: SABackendPlaintext, Token: "ops_X"}, lookupPlaintext); err != nil {
		t.Errorf("plaintext-over-plaintext should be allowed, got %v", err)
	}
	// Keychain OVER plaintext (an UPGRADE) is allowed.
	if _, err := RenderSATokenBlock(SATokenSpec{Backend: SABackendKeychain, Token: "ops_X"}, lookupPlaintext); err != nil {
		t.Errorf("keychain-over-plaintext upgrade should be allowed, got %v", err)
	}
	// Keychain OVER keychain (refresh) is allowed.
	if _, err := RenderSATokenBlock(SATokenSpec{Backend: SABackendKeychain, Token: "ops_X"}, lookupKeychain); err != nil {
		t.Errorf("keychain refresh should be allowed, got %v", err)
	}
}

// TestClassifyExisting reads each lookup kind out of a marker-fenced block.
func TestClassifyExisting(t *testing.T) {
	cases := []struct {
		name string
		body string
		want existingLookupKind
	}{
		{"none", "export FOO=1\n", lookupNone},
		{"plaintext", saMarkerStart + "\nexport " + saExportVar + "=\"ops_abc\"\n" + saMarkerEnd + "\n", lookupPlaintext},
		{"keychain", saMarkerStart + "\nexport " + saExportVar + "=\"$(/usr/bin/security find-generic-password -a \"$USER\" -s 'claude-sa-testhost' -w)\"\n" + saMarkerEnd + "\n", lookupKeychain},
		{"op", saMarkerStart + "\nexport " + saExportVar + "=\"$(op read 'op://V/I/F')\"\n" + saMarkerEnd + "\n", lookupOp},
		// A keychain literal OUTSIDE the markers must NOT be classified.
		{"unmanaged-keychain", "export " + saExportVar + "=\"$(security find-generic-password ...)\"\n", lookupNone},
	}
	for _, c := range cases {
		if got := classifyExisting(c.body); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// TestComposeSAZshenv_SwarmGateAtTop: the block (with swarm gate) lands at the
// TOP of ~/.zshenv, existing content preserved below, and re-running strips the
// old block (idempotent — one block only).
func TestComposeSAZshenv_SwarmGateAtTop(t *testing.T) {
	origMachine := machineNameFn
	machineNameFn = func() string { return "testhost" }
	defer func() { machineNameFn = origMachine }()

	existing := "export PATH=/usr/local/bin:$PATH\nexport EDITOR=vim\n"
	block, _ := RenderSATokenBlock(SATokenSpec{Backend: SABackendKeychain, Token: "t"}, lookupNone)
	got := composeSAZshenv(existing, block)

	if !strings.HasPrefix(got, saMarkerStart) {
		t.Errorf("block must be at the top:\n%s", got)
	}
	// swarm gate appears before the user PATH line.
	gi := strings.Index(got, "SWARM_AGENT_NAME")
	pi := strings.Index(got, "export PATH=")
	if gi < 0 || pi < 0 || gi > pi {
		t.Errorf("swarm gate must precede user content:\n%s", got)
	}
	if !strings.Contains(got, "export EDITOR=vim") {
		t.Error("user content lost")
	}

	// Idempotent: feed the composed output back in with a fresh block.
	block2, _ := RenderSATokenBlock(SATokenSpec{Backend: SABackendKeychain, Token: "t"}, classifyExisting(got))
	got2 := composeSAZshenv(got, block2)
	if n := strings.Count(got2, saMarkerStart); n != 1 {
		t.Errorf("expected exactly 1 sa-token block, got %d:\n%s", n, got2)
	}
}

// TestWriteSAToken_ClobberGuardEndToEnd: writing plaintext over an existing
// keychain block fails and leaves the original ~/.zshenv untouched.
func TestWriteSAToken_ClobberGuardEndToEnd(t *testing.T) {
	origMachine := machineNameFn
	machineNameFn = func() string { return "testhost" }
	defer func() { machineNameFn = origMachine }()

	dir := t.TempDir()
	zshenv := filepath.Join(dir, ".zshenv")
	// Seed an existing keychain block.
	kcBlock, _ := RenderSATokenBlock(SATokenSpec{Backend: SABackendKeychain, Token: "t"}, lookupNone)
	orig := composeSAZshenv("export KEEP=1\n", kcBlock)
	if err := os.WriteFile(zshenv, []byte(orig), 0644); err != nil {
		t.Fatal(err)
	}

	// Now attempt a plaintext write — must be refused.
	_, err := WriteSAToken(SATokenSpec{Backend: SABackendPlaintext, Token: "ops_LEAK"}, zshenv)
	if err == nil {
		t.Fatal("expected clobber-guard error on plaintext-over-keychain write")
	}
	if _, ok := err.(*ErrClobberGuard); !ok {
		t.Errorf("expected *ErrClobberGuard, got %T: %v", err, err)
	}
	// Original file untouched (still keychain, no leaked literal).
	after, _ := os.ReadFile(zshenv)
	if string(after) != orig {
		t.Errorf("zshenv mutated on guard failure:\n--- want ---\n%s\n--- got ---\n%s", orig, after)
	}
	if strings.Contains(string(after), "ops_LEAK") {
		t.Error("plaintext literal leaked into zshenv despite guard")
	}
}

// TestWriteSAToken_OpBackendWritesFile: op backend writes a marker-fenced lookup
// line with the user-provided ref and no token seeding.
func TestWriteSAToken_OpBackendWritesFile(t *testing.T) {
	dir := t.TempDir()
	zshenv := filepath.Join(dir, ".zshenv")
	res, err := WriteSAToken(SATokenSpec{Backend: SABackendOp, OpRef: "op://MyVault/My SA/token"}, zshenv)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Wrote || res.KeychainSeeded {
		t.Errorf("unexpected result: %+v", res)
	}
	got, _ := os.ReadFile(zshenv)
	if !strings.Contains(string(got), "op read 'op://MyVault/My SA/token'") {
		t.Errorf("op lookup missing:\n%s", got)
	}
}

// TestWriteSAToken_KeychainSeeds: keychain backend seeds the keychain via the
// fake seam and writes the lookup line (no plaintext literal).
func TestWriteSAToken_KeychainSeeds(t *testing.T) {
	origMachine := machineNameFn
	origSec := securityAvailableFn
	origAdd := keychainAddFn
	defer func() { machineNameFn = origMachine; securityAvailableFn = origSec; keychainAddFn = origAdd }()
	machineNameFn = func() string { return "testhost" }
	securityAvailableFn = func() bool { return true }

	var seededService, seededValue string
	keychainAddFn = func(service, account, value string) error {
		seededService, seededValue = service, value
		return nil
	}

	dir := t.TempDir()
	zshenv := filepath.Join(dir, ".zshenv")
	res, err := WriteSAToken(SATokenSpec{Backend: SABackendKeychain, Token: "ops_REAL"}, zshenv)
	if err != nil {
		t.Fatal(err)
	}
	if !res.KeychainSeeded || res.KeychainService != "claude-sa-testhost" {
		t.Errorf("unexpected result: %+v", res)
	}
	if seededService != "claude-sa-testhost" || seededValue != "ops_REAL" {
		t.Errorf("keychain seed: got (%q,%q)", seededService, seededValue)
	}
	got, _ := os.ReadFile(zshenv)
	if strings.Contains(string(got), "ops_REAL") {
		t.Errorf("keychain backend must not write the literal token:\n%s", got)
	}
	if !strings.Contains(string(got), "security find-generic-password") {
		t.Errorf("keychain lookup line missing:\n%s", got)
	}
}

// TestRenderedSAToken_ShellSyntax: every non-none backend block must pass sh -n.
func TestRenderedSAToken_ShellSyntax(t *testing.T) {
	origMachine := machineNameFn
	machineNameFn = func() string { return "testhost" }
	defer func() { machineNameFn = origMachine }()

	specs := []SATokenSpec{
		{Backend: SABackendOp, OpRef: "op://V/I/F"},
		{Backend: SABackendKeychain, Token: "t"},
		{Backend: SABackendPlaintext, Token: "ops_lit"},
	}
	for _, sp := range specs {
		block, err := RenderSATokenBlock(sp, lookupNone)
		if err != nil {
			t.Fatal(err)
		}
		f := filepath.Join(t.TempDir(), "sa-"+string(sp.Backend)+".sh")
		if err := writeRCAtomic(f, block); err != nil {
			t.Fatal(err)
		}
		assertShellParses(t, f, block)
	}
}
