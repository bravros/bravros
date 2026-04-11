package paths

import (
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
