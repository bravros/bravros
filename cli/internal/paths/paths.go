package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// PortableRepoDir returns the default portable repo directory.
//
// Resolution order (first hit wins):
//  1. ~/Sites/claude/.git exists  — canonical layout on macOS and on Linux
//     machines that mirror the mac path (e.g. when ~/Sites/context is also
//     present and the user wants both repos under ~/Sites).
//  2. ~/claude/.git exists        — legacy Linux default.
//  3. Per-OS default              — ~/Sites/claude on darwin, ~/claude
//     elsewhere. Returned even when the directory doesn't exist so callers
//     get a stable error path (verify-install reports the missing dir).
//
// Filesystem detection means a single binary works on any host without
// requiring BRAVROS_PORTABLE_REPO to be set — important because the env-var
// override at config.PortableRepo() is only checked by callers that route
// through that helper.
func PortableRepoDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	candidates := []string{
		filepath.Join(home, "Sites", "claude"),
		filepath.Join(home, "claude"),
	}
	for _, c := range candidates {
		if info, err := os.Stat(filepath.Join(c, ".git")); err == nil && info.IsDir() {
			return c
		}
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Sites", "claude")
	}
	return filepath.Join(home, "claude")
}
