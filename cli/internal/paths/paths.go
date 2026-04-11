package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// PortableRepoDir returns the default portable repo directory.
// On macOS (darwin): ~/Sites/claude
// On Linux: ~/claude
func PortableRepoDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Sites", "claude")
	}
	return filepath.Join(home, "claude")
}
