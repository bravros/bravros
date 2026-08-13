package cmd

import (
	"strings"
	"testing"
)

// TestRootVersionFlag verifies that passing --version (or -v) to rootCmd
// produces the same output as `bravros version` and does not return an error.
// Because the flag triggers os.Exit(0), we test the output path via the
// versionCmd directly and confirm the flag is registered on rootCmd.
func TestRootVersionFlag(t *testing.T) {
	// Verify --version and -v flags are registered on rootCmd.
	vLong := rootCmd.Flags().Lookup("version")
	if vLong == nil {
		t.Fatal("--version flag not registered on rootCmd")
	}
	if vLong.Shorthand != "v" {
		t.Errorf("expected shorthand 'v', got %q", vLong.Shorthand)
	}

	// Verify that the version output matches the expected format.
	// We call versionCmd.Run directly (same code path as --version) to avoid
	// triggering os.Exit from the rootVersionFlag path.
	out := captureStdout(t, func() {
		versionCmd.Run(versionCmd, nil)
	})

	trimmed := strings.TrimSpace(out)
	v := strings.TrimPrefix(Version, "v")
	expected := "bravros v" + v
	if trimmed != expected {
		t.Errorf("version output: got %q, want %q", trimmed, expected)
	}
}

// TestRootVersionFlagValue verifies that the rootVersionFlag variable is bound
// to the --version persistent flag and starts as false.
func TestRootVersionFlagValue(t *testing.T) {
	f := rootCmd.Flags().Lookup("version")
	if f == nil {
		t.Fatal("--version flag not found on rootCmd")
	}
	if f.DefValue != "false" {
		t.Errorf("expected default value 'false', got %q", f.DefValue)
	}
}
