package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvFilePath_OverrideAndDefault(t *testing.T) {
	t.Setenv("BRAVROS_ENV_FILE", "/tmp/custom-secrets.env")
	if got := EnvFilePath(); got != "/tmp/custom-secrets.env" {
		t.Errorf("override: got %q", got)
	}

	t.Setenv("BRAVROS_ENV_FILE", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".config", "bravros", "secrets.env")
	if got := EnvFilePath(); got != want {
		t.Errorf("default: got %q, want %q", got, want)
	}
}

func TestDetectBackend_ExplicitOverrideWins(t *testing.T) {
	// Even with op "available", an explicit backend overrides auto-detect.
	orig := opAvailableFn
	opAvailableFn = func() bool { return true }
	defer func() { opAvailableFn = orig }()

	for _, c := range []struct {
		val  string
		want Backend
	}{
		{"op", BackendOp},
		{"env", BackendEnv},
		{"none", BackendNone},
		{"ENV", BackendEnv},   // case-insensitive
		{" op ", BackendOp},   // trimmed
	} {
		t.Setenv(backendEnvVar, c.val)
		if got := DetectBackend(); got != c.want {
			t.Errorf("BRAVROS_SECRETS_BACKEND=%q: got %q, want %q", c.val, got, c.want)
		}
	}
}

func TestDetectBackend_AutoDetect(t *testing.T) {
	t.Setenv(backendEnvVar, "") // no explicit override
	orig := opAvailableFn
	defer func() { opAvailableFn = orig }()

	opAvailableFn = func() bool { return true }
	if got := DetectBackend(); got != BackendOp {
		t.Errorf("op available → want op, got %q", got)
	}
	opAvailableFn = func() bool { return false }
	if got := DetectBackend(); got != BackendEnv {
		t.Errorf("no op → want env (never op lines), got %q", got)
	}
}

func TestResolve_Precedence(t *testing.T) {
	orig := opAvailableFn
	opAvailableFn = func() bool { return false } // never reach op in this test
	defer func() { opAvailableFn = orig }()

	// 1. env var wins over everything.
	t.Setenv("MY_SECRET", "from-env")
	envFile := filepath.Join(t.TempDir(), "secrets.env")
	os.WriteFile(envFile, []byte("MY_SECRET=from-file\n"), 0600)
	t.Setenv("BRAVROS_ENV_FILE", envFile)
	if got := Resolve("MY_SECRET", "my-item", "credential"); got != "from-env" {
		t.Errorf("env var should win, got %q", got)
	}

	// 2. .env file is used when the env var is unset.
	t.Setenv("MY_SECRET", "")
	if got := Resolve("MY_SECRET", "my-item", "credential"); got != "from-file" {
		t.Errorf(".env fallback failed, got %q", got)
	}

	// 3. nothing set + op unavailable → empty (silent, no hang).
	if got := Resolve("ABSENT_SECRET", "absent", "credential"); got != "" {
		t.Errorf("missing secret should resolve empty, got %q", got)
	}
}

func TestReadEnvFile_IgnoresCommentsAndBlanks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.env")
	os.WriteFile(p, []byte("# comment\n\nA=1\nB = two \nC=\"quoted\"\nmalformed\n"), 0600)
	m := readEnvFile(p)
	if m["A"] != "1" || m["B"] != "two" || m["C"] != "quoted" {
		t.Errorf("parse mismatch: %#v", m)
	}
	if _, ok := m["malformed"]; ok {
		t.Errorf("malformed line should be skipped")
	}
	if readEnvFile("/no/such/file") != nil {
		t.Errorf("missing file should return nil")
	}
}

// TestResolve_BackendNone_SkipsLookups: BRAVROS_SECRETS_BACKEND=none must skip the
// .env + op lookups (even when op is "available"), while an already-exported env
// var still wins. (PR #318 review: Resolve must honor the backend like DetectBackend.)
func TestResolve_BackendNone_SkipsLookups(t *testing.T) {
	orig := opAvailableFn
	opAvailableFn = func() bool { return true } // op "available" — must still be skipped
	defer func() { opAvailableFn = orig }()
	t.Setenv(backendEnvVar, "none")

	envFile := filepath.Join(t.TempDir(), "secrets.env")
	os.WriteFile(envFile, []byte("MY_SECRET=from-file\n"), 0600)
	t.Setenv("BRAVROS_ENV_FILE", envFile)

	t.Setenv("MY_SECRET", "from-env")
	if got := Resolve("MY_SECRET", "my-item", "credential"); got != "from-env" {
		t.Errorf("backend=none: already-set env var should still win, got %q", got)
	}
	t.Setenv("MY_SECRET", "")
	if got := Resolve("MY_SECRET", "my-item", "credential"); got != "" {
		t.Errorf("backend=none should skip .env and op lookups, got %q", got)
	}
}

// TestResolve_BackendEnv_DoesNotReachOp: under the env backend, op is never consulted
// even when a session is live — so the result is deterministically empty on a .env miss
// (no blocking op read). This is the behavior the PR #318 review asked for.
func TestResolve_BackendEnv_DoesNotReachOp(t *testing.T) {
	orig := opAvailableFn
	opAvailableFn = func() bool { return true } // op "available" — env backend must ignore it
	defer func() { opAvailableFn = orig }()
	t.Setenv(backendEnvVar, "env")
	t.Setenv("MY_SECRET", "")
	t.Setenv("BRAVROS_ENV_FILE", filepath.Join(t.TempDir(), "absent.env")) // no file
	if got := Resolve("MY_SECRET", "my-item", "credential"); got != "" {
		t.Errorf("backend=env must not reach op, got %q", got)
	}
}
