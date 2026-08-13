package secrets

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestBackendKeychain_NoOpReferences: the keychain block must render
// darwin-guarded and contain ZERO op references — mirrors
// TestBackendNone_NoOpReferences. This is the load-bearing multi-user guard:
// non-1Password users must never see an op:// literal in their RC.
func TestBackendKeychain_NoOpReferences(t *testing.T) {
	block := RenderBlock(BackendKeychain)
	for _, forbidden := range []string{"op read", "op://", "op_lazy", "op whoami"} {
		if strings.Contains(block, forbidden) {
			t.Errorf("keychain backend leaked %q:\n%s", forbidden, block)
		}
	}
	// Darwin-guarded: marker export only happens inside a uname=Darwin gate.
	if !strings.Contains(block, `[ "$(uname -s)" = "Darwin" ]`) {
		t.Errorf("keychain block missing darwin guard:\n%s", block)
	}
	if !strings.Contains(block, "export "+backendEnvVar+"=keychain") {
		t.Error("keychain block missing backend marker")
	}
	// And via the rc-write path too.
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	if _, err := Bootstrap(BootstrapOpts{ShellRC: rc, Backend: BackendKeychain}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	got := readFile(t, rc)
	for _, forbidden := range []string{"op read", "op://", "op_lazy"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("keychain backend rc leaked %q", forbidden)
		}
	}
}

// TestBackendKeychain_RendersShellSyntax: the keychain block must pass `sh -n`
// (and bash/zsh when present) — the darwin guard must be valid POSIX.
func TestBackendKeychain_RendersShellSyntax(t *testing.T) {
	shells := []string{}
	for _, sh := range []string{"sh", "bash", "zsh"} {
		if _, err := exec.LookPath(sh); err == nil {
			shells = append(shells, sh)
		}
	}
	if len(shells) == 0 {
		t.Skip("no shell available to syntax-check")
	}
	block := RenderBlock(BackendKeychain)
	f := filepath.Join(t.TempDir(), "block-keychain.sh")
	if err := writeRCAtomic(f, block); err != nil {
		t.Fatal(err)
	}
	for _, sh := range shells {
		if out, err := exec.Command(sh, "-n", f).CombinedOutput(); err != nil {
			t.Errorf("%s -n failed for keychain backend: %v\n%s\n--- block ---\n%s", sh, err, out, block)
		}
	}
}

// TestDetectBackend_KeychainOptInOnly: keychain is honored when explicitly set
// (on a security-capable host) but NEVER auto-selected; on a no-security host
// the explicit keychain selection degrades to env.
func TestDetectBackend_KeychainOptInOnly(t *testing.T) {
	origOp := opAvailableFn
	origSec := securityAvailableFn
	defer func() { opAvailableFn = origOp; securityAvailableFn = origSec }()

	// Explicit keychain + security present → keychain.
	securityAvailableFn = func() bool { return true }
	t.Setenv(backendEnvVar, "keychain")
	if got := DetectBackend(); got != BackendKeychain {
		t.Errorf("explicit keychain + security → want keychain, got %q", got)
	}

	// Explicit keychain + NO security (Linux) → degrade to env, no error.
	securityAvailableFn = func() bool { return false }
	if got := DetectBackend(); got != BackendEnv {
		t.Errorf("explicit keychain + no security → want env, got %q", got)
	}

	// Auto-detect NEVER returns keychain even when security is present.
	securityAvailableFn = func() bool { return true }
	opAvailableFn = func() bool { return false }
	t.Setenv(backendEnvVar, "")
	if got := DetectBackend(); got == BackendKeychain {
		t.Error("auto-detect must never select keychain")
	}
}

// TestResolve_KeychainReadsBack: under the keychain backend, Resolve reads the
// value via the keychain lookup seam. Real security-shelling is exercised only
// on darwin; elsewhere we drive the fake seam to prove the precedence wiring.
func TestResolve_KeychainReadsBack(t *testing.T) {
	origSec := securityAvailableFn
	origLookup := keychainLookupFn
	defer func() { securityAvailableFn = origSec; keychainLookupFn = origLookup }()

	securityAvailableFn = func() bool { return true }
	var gotService, gotAccount string
	keychainLookupFn = func(service, account string) (string, bool) {
		gotService, gotAccount = service, account
		if account == "FIRECRAWL_API_KEY" {
			return "fc-from-keychain", true
		}
		return "", false
	}
	t.Setenv(backendEnvVar, "keychain")
	t.Setenv("FIRECRAWL_API_KEY", "")
	t.Setenv("BRAVROS_ENV_FILE", filepath.Join(t.TempDir(), "absent.env"))

	if got := Resolve("FIRECRAWL_API_KEY", "Firecrawl API", "credencial"); got != "fc-from-keychain" {
		t.Errorf("keychain backend should resolve from keychain, got %q", got)
	}
	// Registry maps FIRECRAWL_API_KEY → default service, account=EnvVar.
	if gotService != defaultKeychainService || gotAccount != "FIRECRAWL_API_KEY" {
		t.Errorf("keychain coords: got (%q,%q), want (%q,%q)", gotService, gotAccount, defaultKeychainService, "FIRECRAWL_API_KEY")
	}

	// A keychain miss resolves empty (no op fallthrough).
	if got := Resolve("ABSENT", "x", "y"); got != "" {
		t.Errorf("keychain miss should resolve empty, got %q", got)
	}
}

// TestResolve_KeychainDegradesWithoutSecurity: a forced keychain backend on a
// host without `security` resolves empty (no shelling, no error) rather than
// reaching op.
func TestResolve_KeychainDegradesWithoutSecurity(t *testing.T) {
	origSec := securityAvailableFn
	origLookup := keychainLookupFn
	defer func() { securityAvailableFn = origSec; keychainLookupFn = origLookup }()

	securityAvailableFn = func() bool { return false }
	called := false
	keychainLookupFn = func(string, string) (string, bool) { called = true; return "x", true }
	t.Setenv(backendEnvVar, "keychain")
	t.Setenv("FIRECRAWL_API_KEY", "")
	t.Setenv("BRAVROS_ENV_FILE", filepath.Join(t.TempDir(), "absent.env"))

	// DetectBackend would degrade keychain→env here, so Resolve's keychain branch
	// is unreachable; still must not shell out.
	if got := Resolve("FIRECRAWL_API_KEY", "Firecrawl API", "credencial"); got != "" {
		t.Errorf("want empty on no-security host, got %q", got)
	}
	if called {
		t.Error("keychain lookup must not be called when security is absent")
	}
}

// TestStatus_ReportsKeychain: status reflects an explicit keychain backend.
func TestStatus_ReportsKeychain(t *testing.T) {
	origSec := securityAvailableFn
	defer func() { securityAvailableFn = origSec }()
	securityAvailableFn = func() bool { return true }
	t.Setenv(backendEnvVar, "keychain")
	if st := Status(); st.Backend != BackendKeychain {
		t.Errorf("status backend: got %q, want keychain", st.Backend)
	}
}

// TestSetKeychainSecret_RoundTrip exercises the real macOS keychain end to end:
// write FOO=bar via security add-generic-password, read it back, then clean up.
// Gated on darwin + the security binary; skipped everywhere else.
func TestSetKeychainSecret_RoundTrip(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("keychain round-trip requires darwin")
	}
	if _, err := exec.LookPath("security"); err != nil {
		t.Skip("security CLI not available")
	}
	origSec := securityAvailableFn
	defer func() { securityAvailableFn = origSec }()
	securityAvailableFn = func() bool { return true }

	const service = "bravros-secrets-test"
	const account = "BRAVROS_KEYCHAIN_TEST"
	// Best-effort pre-clean + deferred clean.
	cleanup := func() {
		_ = exec.Command("/usr/bin/security", "delete-generic-password", "-a", account, "-s", service).Run()
	}
	cleanup()
	defer cleanup()

	// Direct keychainAdd (SetKeychainSecret uses the registry mapping; here we
	// drive the low-level primitive against an isolated test service).
	if err := keychainAdd(service, account, "bar-secret"); err != nil {
		t.Fatalf("keychainAdd: %v", err)
	}
	got, ok := keychainLookup(service, account)
	if !ok || got != "bar-secret" {
		t.Errorf("keychain round-trip: got (%q,%v), want (%q,true)", got, ok, "bar-secret")
	}
}
