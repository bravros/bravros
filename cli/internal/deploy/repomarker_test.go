package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

// writeRepoMarkers makes base look like a real bravros source checkout.
//
// IsClaudeRepo used to accept any directory literally named "claude", so the
// fixtures below just created a temp dir with that name. Detection is now by
// content — skills/ plus a cli/go.mod declaring this module — so the fixtures
// have to write the marker instead of relying on the folder name.
func writeRepoMarkers(t *testing.T, base string) {
	t.Helper()
	// Both markers, so a fixture that only builds e.g. .githooks/ still qualifies.
	if err := os.MkdirAll(filepath.Join(base, "skills"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "cli"), 0755); err != nil {
		t.Fatal(err)
	}
	gomod := "module github.com/bravros/bravros/cli\n\ngo 1.26.2\n"
	if err := os.WriteFile(filepath.Join(base, "cli", "go.mod"), []byte(gomod), 0644); err != nil {
		t.Fatal(err)
	}
}
