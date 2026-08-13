package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSplitKeyValue covers valid and invalid KEY=VALUE arguments.
func TestSplitKeyValue(t *testing.T) {
	cases := []struct {
		in        string
		wantKey   string
		wantValue string
		wantErr   bool
	}{
		{"FIRECRAWL_API_KEY=fc-123", "FIRECRAWL_API_KEY", "fc-123", false},
		{`HASS_TOKEN="quoted value"`, "HASS_TOKEN", "quoted value", false},
		{"KEY=a=b=c", "KEY", "a=b=c", false}, // value may contain '='
		{"no-equals", "", "", true},
		{"=novalue", "", "", true},          // empty key
		{"bad-key=1", "", "", true},          // hyphen not allowed in key
		{"0FOO=bar", "", "", true},           // leading digit is not a valid shell identifier
		{"_OK=v", "_OK", "v", false},         // leading underscore is valid
	}
	for _, c := range cases {
		k, v, err := SplitKeyValue(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("SplitKeyValue(%q) err=%v wantErr=%v", c.in, err, c.wantErr)
			continue
		}
		if c.wantErr {
			continue
		}
		if k != c.wantKey || v != c.wantValue {
			t.Errorf("SplitKeyValue(%q) = (%q,%q), want (%q,%q)", c.in, k, v, c.wantKey, c.wantValue)
		}
	}
}

// TestSetEnvSecret_WritesAndUpdates: first write creates the file 0600 with the
// pair; a second write to the same key updates in place (no duplicate); a write
// to a different key appends.
func TestSetEnvSecret_WritesAndUpdates(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "bravros", "secrets.env")
	t.Setenv("BRAVROS_ENV_FILE", envFile)

	path, err := SetEnvSecret("FIRECRAWL_API_KEY", "v1")
	if err != nil {
		t.Fatalf("first set: %v", err)
	}
	if path != envFile {
		t.Errorf("path: got %q, want %q", path, envFile)
	}
	fi, err := os.Stat(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected 0600, got %o", fi.Mode().Perm())
	}

	// Update same key.
	if _, err := SetEnvSecret("FIRECRAWL_API_KEY", "v2"); err != nil {
		t.Fatalf("update: %v", err)
	}
	// Add a second key.
	if _, err := SetEnvSecret("HASS_TOKEN", "ha-tok"); err != nil {
		t.Fatalf("second key: %v", err)
	}

	m := readEnvFile(envFile)
	if m["FIRECRAWL_API_KEY"] != "v2" {
		t.Errorf("FIRECRAWL_API_KEY: got %q, want v2 (in-place update)", m["FIRECRAWL_API_KEY"])
	}
	if m["HASS_TOKEN"] != "ha-tok" {
		t.Errorf("HASS_TOKEN: got %q", m["HASS_TOKEN"])
	}
	// Exactly one FIRECRAWL_API_KEY line (no duplicate-append).
	data, _ := os.ReadFile(envFile)
	if n := strings.Count(string(data), "FIRECRAWL_API_KEY="); n != 1 {
		t.Errorf("expected 1 FIRECRAWL_API_KEY line, got %d:\n%s", n, data)
	}
	// No .tmp leftover.
	entries, _ := os.ReadDir(filepath.Dir(envFile))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".bravros-env-") {
			t.Errorf("leftover temp env file: %s", e.Name())
		}
	}
}

// TestStatus reports backend, env-file path, and op availability.
func TestStatus(t *testing.T) {
	orig := opAvailableFn
	opAvailableFn = func() bool { return false }
	defer func() { opAvailableFn = orig }()

	envFile := filepath.Join(t.TempDir(), "secrets.env")
	t.Setenv("BRAVROS_ENV_FILE", envFile)
	t.Setenv(backendEnvVar, "env")

	st := Status()
	if st.Backend != BackendEnv {
		t.Errorf("backend: got %q", st.Backend)
	}
	if st.EnvFile != envFile {
		t.Errorf("env_file: got %q, want %q", st.EnvFile, envFile)
	}
	if st.EnvFileExists {
		t.Error("env_file_exists should be false before write")
	}
	if st.OpAvailable {
		t.Error("op_available should be false")
	}

	os.WriteFile(envFile, []byte("A=1\n"), 0600)
	if st := Status(); !st.EnvFileExists {
		t.Error("env_file_exists should be true after write")
	}
}
