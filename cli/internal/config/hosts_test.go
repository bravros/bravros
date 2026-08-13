package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOptedInHosts_EnvOverride(t *testing.T) {
	t.Setenv("BRAVROS_HOSTS", "codex, PI ,foo,claude")
	got := OptedInHosts()
	// foo (unsupported) and claude (implicit) dropped; lowercased; sorted.
	if len(got) != 2 || got[0] != "codex" || got[1] != "pi" {
		t.Errorf("got %v, want [codex pi]", got)
	}
}

func TestOptedInHosts_FileAndFailSafe(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BRAVROS_CONFIG_DIR", dir)
	t.Setenv("BRAVROS_HOSTS", "") // no env override → read file

	// No file → nil (Claude-only default).
	if got := OptedInHosts(); got != nil {
		t.Errorf("no file should yield nil, got %v", got)
	}

	// Valid file.
	os.WriteFile(filepath.Join(dir, "hosts.json"), []byte(`{"hosts":["pi","codex"]}`), 0o644)
	got := OptedInHosts()
	if len(got) != 2 || got[0] != "codex" || got[1] != "pi" {
		t.Errorf("file read: got %v, want [codex pi]", got)
	}

	// Malformed JSON → nil (fail-safe: never auto-opt-in).
	os.WriteFile(filepath.Join(dir, "hosts.json"), []byte(`{not json`), 0o644)
	if got := OptedInHosts(); got != nil {
		t.Errorf("malformed JSON should yield nil, got %v", got)
	}
}

func TestHostOptedIn(t *testing.T) {
	t.Setenv("BRAVROS_HOSTS", "codex")
	cases := []struct {
		host string
		want bool
	}{
		{"claude", true},  // always implicit
		{"Claude", true},  // case-insensitive
		{"codex", true},   // in set
		{"Codex", true},   // case-insensitive
		{"opencode", false}, // supported but not opted in
		{"pi", false},
		{"unknown", false}, // unsupported
	}
	for _, c := range cases {
		if got := HostOptedIn(c.host); got != c.want {
			t.Errorf("HostOptedIn(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestSetOptedInHosts_MergeDedupIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BRAVROS_CONFIG_DIR", dir)
	t.Setenv("BRAVROS_HOSTS", "") // force file path

	if err := SetOptedInHosts([]string{"codex"}); err != nil {
		t.Fatalf("set 1: %v", err)
	}
	// Additive merge + drop claude + drop unsupported + dedup.
	if err := SetOptedInHosts([]string{"pi", "codex", "claude", "foo"}); err != nil {
		t.Fatalf("set 2: %v", err)
	}
	got := OptedInHosts()
	if len(got) != 2 || got[0] != "codex" || got[1] != "pi" {
		t.Errorf("after merge: got %v, want [codex pi]", got)
	}

	// Idempotent: re-setting the same set yields the identical file content.
	before, _ := os.ReadFile(filepath.Join(dir, "hosts.json"))
	if err := SetOptedInHosts([]string{"codex", "pi"}); err != nil {
		t.Fatalf("set 3: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "hosts.json"))
	if string(before) != string(after) {
		t.Errorf("not idempotent:\nbefore=%s\nafter=%s", before, after)
	}
	// No temp files leaked.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "hosts.json" {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}
