package license

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/bravros/bravros/internal/paths"
)

// Sentinel errors returned by license cache operations.
var (
	// ErrNotActivated is returned when no license token is found in the cache.
	ErrNotActivated = errors.New("not activated")

	// ErrExpired is returned when a cached token has expired.
	ErrExpired = errors.New("license expired")
)

// SaveToken writes the raw JWT string to the auth cache file (mode 0600).
// The parent directory is created if it does not exist.
func SaveToken(token string) error {
	path := paths.AuthCachePath()
	if path == "" {
		return errors.New("license: could not determine auth cache path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(token), 0600)
}

// LoadToken reads the raw JWT string from the auth cache file.
// Returns ErrNotActivated if the file does not exist.
func LoadToken() (string, error) {
	path := paths.AuthCachePath()
	if path == "" {
		return "", errors.New("license: could not determine auth cache path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNotActivated
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// ClearToken removes the auth cache file.
// It is a no-op (returns nil) if the file does not exist.
func ClearToken() error {
	path := paths.AuthCachePath()
	if path == "" {
		return errors.New("license: could not determine auth cache path")
	}
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
