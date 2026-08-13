package secrets

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// StatusInfo summarizes the active secrets configuration for `bravros secrets
// status`.
type StatusInfo struct {
	Backend       Backend `json:"backend"`
	EnvFile       string  `json:"env_file"`
	EnvFileExists bool    `json:"env_file_exists"`
	OpAvailable   bool    `json:"op_available"`
	// SATokenBlock classifies the 1Password Service-Account token block in
	// ~/.zshenv: "none" | "plaintext" | "keychain" | "op". "none" means the SA
	// token is not yet bootstrapped on this machine (no managed block present).
	SATokenBlock string `json:"sa_token_block"`
}

// Status reports the detected backend, the resolved env-file path, whether that
// file exists on disk, whether op is installed + has a live session, and how the
// SA-token block in ~/.zshenv is configured.
func Status() StatusInfo {
	envFile := EnvFilePath()
	_, statErr := os.Stat(envFile)
	return StatusInfo{
		Backend:       DetectBackend(),
		EnvFile:       envFile,
		EnvFileExists: statErr == nil,
		OpAvailable:   opAvailableFn(),
		SATokenBlock:  SATokenBlockKind(""),
	}
}

// SplitKeyValue parses a "KEY=VALUE" argument. The key must be a valid POSIX
// shell identifier — it starts with a letter or underscore, followed by
// letters, digits, or underscores; the value is taken verbatim (may contain
// '=' and spaces). Surrounding quotes on the value are stripped so `KEY="v"`
// and `KEY=v` behave identically.
func SplitKeyValue(arg string) (key, value string, err error) {
	k, v, ok := strings.Cut(arg, "=")
	if !ok {
		return "", "", fmt.Errorf("invalid argument %q: expected KEY=VALUE", arg)
	}
	k = strings.TrimSpace(k)
	if k == "" {
		return "", "", fmt.Errorf("invalid argument %q: empty key", arg)
	}
	for i, r := range k {
		isLetter := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
		isDigit := r >= '0' && r <= '9'
		// A leading digit is a valid char but an invalid shell identifier
		// (`export 0FOO=x` fails at shell startup), so reject it up front at the
		// CLI rather than letting it surface as a silent rc syntax error later.
		if i == 0 && !(r == '_' || isLetter) {
			return "", "", fmt.Errorf("invalid key %q: must start with a letter or underscore", k)
		}
		if !(r == '_' || isLetter || isDigit) {
			return "", "", fmt.Errorf("invalid key %q: only letters, digits, and underscore allowed", k)
		}
	}
	v = strings.TrimSpace(v)
	v = strings.Trim(v, `"'`)
	return k, v, nil
}

// SetEnvSecret writes or updates KEY=VALUE in the env-backend secrets file
// ($BRAVROS_ENV_FILE, default ~/.config/bravros/secrets.env). The parent dir is
// created (0700) and the file is written 0600. Existing keys are updated in
// place; the rest of the file is preserved. Returns the file path written.
//
// The write is atomic (temp file + rename in the same dir) so a crash mid-write
// can never corrupt an existing secrets file.
func SetEnvSecret(key, value string) (string, error) {
	path := EnvFilePath()
	if path == "" {
		return "", fmt.Errorf("cannot determine env-file path (set BRAVROS_ENV_FILE or $HOME)")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("cannot create %s: %w", dir, err)
	}

	// Read existing lines (if any), replacing the target key in place.
	var lines []string
	replaced := false
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				if k, _, ok := strings.Cut(trimmed, "="); ok && strings.TrimSpace(k) == key {
					lines = append(lines, key+"="+value)
					replaced = true
					continue
				}
			}
			lines = append(lines, line)
		}
		f.Close()
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("cannot read %s: %w", path, err)
	}

	if !replaced {
		lines = append(lines, key+"="+value)
	}

	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	if err := writeFileAtomic(path, content, 0600); err != nil {
		return "", err
	}
	return path, nil
}

// keychainAddFn is the seam tests use to fake the `security add-generic-password`
// write without mutating the host login keychain. Production uses keychainAdd.
var keychainAddFn = keychainAdd

// keychainAdd stores (or updates with -U) a generic password in the login
// keychain via `security add-generic-password`. The value is passed via the
// `-w <value>` argument; we never echo it. The ACL flag `-A` grants silent reads
// to ANY application on the machine.
//
// ACL TRADEOFF (documented, deliberate — B-0337): `-A` (any-app) is used instead
// of `-T /usr/bin/security` + `set-generic-password-partition-list`. The
// partition-list path needs the login password and is finicky/interactive; `-A`
// is reliably prompt-free. The exposure is "any local process owned by this
// user can read the secret" — no worse than the plaintext dotfile it replaces,
// but now encrypted at rest (out of Time Machine / git / screen-share plaintext).
// Tighten to `-T` + partition-list only if a stricter per-binary ACL is needed.
func keychainAdd(service, account, value string) error {
	out, err := exec.Command("/usr/bin/security", "add-generic-password",
		"-a", account, "-s", service, "-A", "-U", "-w", value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("security add-generic-password failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SetKeychainSecret writes KEY=VALUE into the macOS login keychain under the
// registry's service/account mapping for KEY (default service "bravros-secrets",
// account = KEY). It is the keychain-backend analogue of SetEnvSecret. Returns
// the (service, account) pair written. Fails fast on non-darwin / no-`security`
// hosts rather than silently doing nothing.
func SetKeychainSecret(key, value string) (service, account string, err error) {
	if !securityAvailableFn() {
		return "", "", fmt.Errorf("keychain backend requires macOS `security` (not available on this host)")
	}
	service, account = keychainCoords(key)
	if err := keychainAddFn(service, account, value); err != nil {
		return "", "", err
	}
	return service, account, nil
}

// writeFileAtomic writes content to path with the given mode atomically (temp +
// rename in the same dir). Used for the env secrets file; mirrors the rc atomic
// writer but takes an explicit mode (0600 for secrets).
func writeFileAtomic(path, content string, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".bravros-env-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}
	if _, err := tmp.WriteString(content); err != nil {
		cleanup()
		return fmt.Errorf("cannot write temp env file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("cannot sync temp env file: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		cleanup()
		return fmt.Errorf("cannot chmod temp env file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("cannot close temp env file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("cannot rename temp env file over %s: %w", path, err)
	}
	return nil
}
