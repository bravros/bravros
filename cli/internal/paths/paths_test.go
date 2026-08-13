package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPortableRepoDir(t *testing.T) {
	dir := PortableRepoDir()

	if dir == "" {
		t.Error("PortableRepoDir() returned empty string")
	}

	if !strings.Contains(dir, "claude") {
		t.Errorf("PortableRepoDir() = %q, want path containing 'claude'", dir)
	}
}

// TestPortableRepoDir_DetectsSitesLayout — when ~/Sites/claude/.git exists,
// PortableRepoDir should return ~/Sites/claude regardless of OS (this is the
// canonical layout on macOS and is mirrored on Linux machines that put both
// ~/Sites/claude and ~/Sites/context under ~/Sites).
func TestPortableRepoDir_DetectsSitesLayout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sitesClaude := filepath.Join(home, "Sites", "claude", ".git")
	if err := os.MkdirAll(sitesClaude, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	got := PortableRepoDir()
	want := filepath.Join(home, "Sites", "claude")
	if got != want {
		t.Errorf("PortableRepoDir() = %q, want %q (Sites layout should win)", got, want)
	}
}

// TestPortableRepoDir_DetectsLegacyLinuxLayout — when only ~/claude/.git
// exists (legacy Linux default), PortableRepoDir should pick it up.
func TestPortableRepoDir_DetectsLegacyLinuxLayout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	legacyClaude := filepath.Join(home, "claude", ".git")
	if err := os.MkdirAll(legacyClaude, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	got := PortableRepoDir()
	want := filepath.Join(home, "claude")
	if got != want {
		t.Errorf("PortableRepoDir() = %q, want %q (legacy layout should match when Sites absent)", got, want)
	}
}

// TestPortableRepoDir_FallbackWhenNothingExists — when neither candidate has
// a .git dir, return the per-OS default (used by error messages).
func TestPortableRepoDir_FallbackWhenNothingExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := PortableRepoDir()
	var want string
	if runtime.GOOS == "darwin" {
		want = filepath.Join(home, "Sites", "claude")
	} else {
		want = filepath.Join(home, "claude")
	}
	if got != want {
		t.Errorf("PortableRepoDir() = %q, want %q (per-OS fallback)", got, want)
	}
}

// TestPortableRepoDir_PrefersSitesWhenBothExist — defense-in-depth: if a
// machine has BOTH ~/Sites/claude/.git AND ~/claude/.git, prefer ~/Sites.
func TestPortableRepoDir_PrefersSitesWhenBothExist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "Sites", "claude", ".git"), 0o755); err != nil {
		t.Fatalf("setup sites: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, "claude", ".git"), 0o755); err != nil {
		t.Fatalf("setup legacy: %v", err)
	}

	got := PortableRepoDir()
	want := filepath.Join(home, "Sites", "claude")
	if got != want {
		t.Errorf("PortableRepoDir() = %q, want %q (Sites must beat legacy)", got, want)
	}
}
