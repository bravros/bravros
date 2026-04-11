package license

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// overrideAuthCachePath temporarily redirects AuthCachePath to a temp file
// by monkey-patching paths via env trick — instead we inject via a test helper
// that writes/reads a temp file directly using the exported functions with a
// custom path set via env variable (we patch the package-level function for tests).

// For isolation we use the real SaveToken/LoadToken/ClearToken but redirect the
// underlying path by temporarily setting HOME to t.TempDir() so that
// paths.AuthCachePath() resolves inside the temp directory.
func withTempHome(t *testing.T, fn func()) {
	t.Helper()
	tmp := t.TempDir()
	orig := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	// Also create the expected .claude dir structure
	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0700); err != nil {
		t.Fatalf("failed to create temp .claude dir: %v", err)
	}
	_ = orig
	fn()
}

func TestSaveAndLoadToken_RoundTrip(t *testing.T) {
	withTempHome(t, func() {
		const tok = "eyJhbGciOiJFUzI1NiJ9.test.payload"

		if err := SaveToken(tok); err != nil {
			t.Fatalf("SaveToken() error = %v", err)
		}

		got, err := LoadToken()
		if err != nil {
			t.Fatalf("LoadToken() error = %v", err)
		}
		if got != tok {
			t.Errorf("LoadToken() = %q, want %q", got, tok)
		}
	})
}

func TestSaveToken_FilePermissions(t *testing.T) {
	withTempHome(t, func() {
		if err := SaveToken("test-token"); err != nil {
			t.Fatalf("SaveToken() error = %v", err)
		}

		home := os.Getenv("HOME")
		cachePath := filepath.Join(home, ".claude", ".bravros-auth")
		info, err := os.Stat(cachePath)
		if err != nil {
			t.Fatalf("Stat() error = %v", err)
		}
		// Permissions should be 0600
		if info.Mode().Perm() != 0600 {
			t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
		}
	})
}

func TestLoadToken_MissingFile_ReturnsErrNotActivated(t *testing.T) {
	withTempHome(t, func() {
		_, err := LoadToken()
		if !errors.Is(err, ErrNotActivated) {
			t.Errorf("LoadToken() error = %v, want ErrNotActivated", err)
		}
	})
}

func TestClearToken_RemovesFile(t *testing.T) {
	withTempHome(t, func() {
		if err := SaveToken("some-token"); err != nil {
			t.Fatalf("SaveToken() error = %v", err)
		}

		if err := ClearToken(); err != nil {
			t.Fatalf("ClearToken() error = %v", err)
		}

		_, err := LoadToken()
		if !errors.Is(err, ErrNotActivated) {
			t.Errorf("after ClearToken(), LoadToken() error = %v, want ErrNotActivated", err)
		}
	})
}

func TestClearToken_NoopWhenMissing(t *testing.T) {
	withTempHome(t, func() {
		// Should not return an error even when the file doesn't exist
		if err := ClearToken(); err != nil {
			t.Errorf("ClearToken() on missing file error = %v, want nil", err)
		}
	})
}
