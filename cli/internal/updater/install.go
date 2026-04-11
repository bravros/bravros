package updater

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Install downloads a new binary from url, verifies it, and replaces the
// currently running binary with an atomic rename (falling back to copy if
// the rename fails due to cross-device issues).
func Install(url string) error {
	if url == "" {
		return fmt.Errorf("no download URL available for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// Resolve the current binary path.
	currentBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}
	currentBin, err = filepath.EvalSymlinks(currentBin)
	if err != nil {
		return fmt.Errorf("cannot resolve symlinks: %w", err)
	}

	// Download to a temp file in the same directory as the current binary
	// (same filesystem → atomic rename is possible).
	tmpPath := filepath.Join(filepath.Dir(currentBin), ".bravros-update-tmp")
	defer os.Remove(tmpPath) // cleanup on any error path

	if err := downloadFile(url, tmpPath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Make executable.
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("chmod failed: %w", err)
	}

	// Verify the downloaded binary runs.
	if err := verifyBinary(tmpPath); err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}

	// Atomic replacement: try rename first, fallback to copy.
	if err := os.Rename(tmpPath, currentBin); err != nil {
		// Cross-device fallback: copy contents.
		if err := copyFile(tmpPath, currentBin); err != nil {
			return fmt.Errorf("replacement failed: %w", err)
		}
	}

	// On macOS, ad-hoc codesign to avoid Gatekeeper prompts.
	if runtime.GOOS == "darwin" {
		_ = exec.Command("codesign", "-s", "-", currentBin).Run()
	}

	return nil
}

// downloadFile fetches url and writes to dst.
func downloadFile(url, dst string) error {
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// verifyBinary runs the downloaded binary with --version to confirm it
// is a valid bravros executable.
func verifyBinary(path string) error {
	cmd := exec.Command(path, "version")
	cmd.Env = append(os.Environ(), "BRAVROS_SKIP_UPDATE=1")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("binary failed --version check: %w", err)
	}
	if len(out) == 0 {
		return fmt.Errorf("binary produced no output")
	}
	return nil
}

// copyFile is a fallback when os.Rename fails across filesystems.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
