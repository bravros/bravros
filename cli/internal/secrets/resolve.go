package secrets

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bravros/bravros/cli/internal/config"
)

// Backend identifies where secrets are resolved from.
type Backend string

const (
	BackendOp       Backend = "op"       // 1Password CLI (lazy, op-whoami-gated)
	BackendEnv      Backend = "env"      // $KAISSER_ENV_FILE (KEY=value lines)
	BackendNone     Backend = "none"     // no secret hydration; features degrade gracefully
	BackendKeychain Backend = "keychain" // macOS login keychain (darwin-only, opt-in)
)

// securityAvailableFn is the seam tests use to simulate the macOS `security`
// binary's presence/absence without depending on the host. Production code uses
// securityAvailable.
var securityAvailableFn = securityAvailable

// securityAvailable reports whether the macOS `security` CLI is on PATH. The
// keychain backend degrades to env when this is false (Linux, or a Mac with a
// stripped PATH), so a keychain selection never errors on a non-mac host.
func securityAvailable() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	_, err := exec.LookPath("security")
	return err == nil
}

// keychainLookupFn is the seam tests use to fake a keychain read without
// shelling out. Production code uses keychainLookup.
var keychainLookupFn = keychainLookup

// keychainLookup reads a generic password from the login keychain via
// `/usr/bin/security find-generic-password -s <service> -a <account> -w`. It
// returns ("", false) on any error (item absent, keychain locked) so the caller
// falls through cleanly. Never prompts: a missing item just returns non-zero.
func keychainLookup(service, account string) (string, bool) {
	out, err := exec.Command("/usr/bin/security", "find-generic-password",
		"-s", service, "-a", account, "-w").Output()
	if err != nil {
		return "", false
	}
	return strings.TrimRight(string(out), "\n"), true
}

// backendEnvVar lets a user or orchestrator force the backend explicitly,
// overriding auto-detection.
const backendEnvVar = "KAISSER_SECRETS_BACKEND"

// opAvailableFn is the seam tests use to simulate op presence without invoking
// the real 1Password CLI. Production code uses OpAvailable.
var opAvailableFn = OpAvailable

// EnvFilePath returns the .env-style secrets file path: $KAISSER_ENV_FILE when
// set, otherwise the default ~/.config/kaisser/secrets.env.
func EnvFilePath() string {
	if p := os.Getenv("KAISSER_ENV_FILE"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "kaisser", "secrets.env")
}

// OpAvailable reports whether the 1Password CLI is installed AND has a live
// session. `op whoami` returns in milliseconds and NEVER triggers a biometric
// prompt, so it is safe on a hot path — unlike a bare `op read`, which can pop a
// blocking Touch ID dialog. This precheck is the core fix for no-op / no-session
// machines: they fast-fail to empty instead of hanging at shell startup.
func OpAvailable() bool {
	if _, err := exec.LookPath("op"); err != nil {
		return false
	}
	return exec.Command("op", "whoami").Run() == nil
}

// DetectBackend resolves the active backend. An explicit KAISSER_SECRETS_BACKEND
// wins; otherwise auto-detect: op installed + a live session → op, else env.
// A no-op machine therefore defaults to env and never gets op lines written.
func DetectBackend() Backend {
	switch Backend(strings.ToLower(strings.TrimSpace(os.Getenv(backendEnvVar)))) {
	case BackendOp:
		return BackendOp
	case BackendEnv:
		return BackendEnv
	case BackendNone:
		return BackendNone
	case BackendKeychain:
		// Opt-in only — honored when explicitly set, but auto-detect NEVER
		// returns keychain (same posture as none). If `security` is absent
		// (Linux / stripped PATH), degrade to env so the selection is harmless.
		if securityAvailableFn() {
			return BackendKeychain
		}
		return BackendEnv
	}
	// Auto-detect: keychain is never auto-selected (opt-in only).
	if opAvailableFn() {
		return BackendOp
	}
	return BackendEnv
}

// readEnvFile parses KEY=value lines from path (ignoring blanks and # comments).
// Returns nil when the file is absent — callers treat a nil map as "no match".
func readEnvFile(path string) map[string]string {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return out
}

// Resolve returns a secret value using a single fixed precedence, identical for
// the shell kaisser_secret() function and any Go caller:
//
//  1. an already-set environment variable (highest)
//  2. $KAISSER_ENV_FILE (KEY=value)
//  3. 1Password — ONLY when op is installed AND `op whoami` passes (never prompts)
//  4. "" (empty, silent)
//
// op users with a live session get byte-identical behavior to the old eager
// `$(op read …)`; everyone else fast-fails to empty without spam or a blocking
// biometric prompt.
func Resolve(envVar, opItem, opField string) string {
	// 1. An already-exported env var always wins — it is the value already in the
	//    process, not a backend lookup, so it holds regardless of the backend.
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	// Honor the selected backend for the LOOKUP steps so Resolve stays consistent
	// with DetectBackend: `none` does no lookups; `env` reads the .env file only;
	// only `op` ever shells out to 1Password (and only when op is reachable — the
	// `op whoami` gate prevents a blocking biometric prompt).
	backend := DetectBackend()
	if backend == BackendNone {
		return ""
	}
	// 2. .env file.
	if m := readEnvFile(EnvFilePath()); m != nil {
		if v := m[envVar]; v != "" {
			return v
		}
	}
	// 2b. macOS login keychain — only under the keychain backend, only on a host
	//     where `security` is present. On Linux / no-security it never reaches
	//     here (DetectBackend already degraded keychain → env), but we re-guard
	//     defensively so a forced backend can't shell out on the wrong OS.
	if backend == BackendKeychain {
		if securityAvailableFn() {
			service, account := keychainCoords(envVar)
			if v, ok := keychainLookupFn(service, account); ok && v != "" {
				return v
			}
		}
		return ""
	}
	// 3. 1Password — only under the op backend, and only with a live session.
	if backend == BackendOp && opAvailableFn() {
		if opField == "" {
			opField = "credential"
		}
		ref := "op://" + config.OpVault() + "/" + opItem + "/" + opField
		if out, err := exec.Command("op", "read", ref).Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}
