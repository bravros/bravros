package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// supportedHosts are the non-Claude harnesses bravros can adapt to. "claude" is
// implicit and always-on — it is never stored in hosts.json.
var supportedHosts = map[string]struct{}{
	"codex":    {},
	"opencode": {},
	"pi":       {},
}

type hostsFile struct {
	Hosts []string `json:"hosts"`
}

// hostsFilePath is the machine-global opt-in store. It MUST be machine-global
// (under ConfigDir) rather than a per-project .bravros.yml key: `bravros
// selfupdate` runs from the SessionStart hook with an arbitrary CWD, and host
// install is a machine-level fact ("I installed the bravros-codex adapter on
// THIS box"), not a per-repo one.
func hostsFilePath() string { return filepath.Join(ConfigDir(), "hosts.json") }

// normalizeHosts lowercases, trims, drops "claude" (implicit) and any
// unsupported names, then dedups and sorts.
func normalizeHosts(in []string) []string {
	seen := map[string]struct{}{}
	for _, h := range in {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" || h == "claude" {
			continue
		}
		if _, ok := supportedHosts[h]; !ok {
			continue
		}
		seen[h] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for h := range seen {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// OptedInHosts returns the lowercase set of opted-in non-Claude hosts.
// Precedence: BRAVROS_HOSTS env (comma-separated) > ConfigDir/hosts.json > nil.
// Fail-safe: any read/parse error yields nil (Claude-only) — bravros never
// auto-opts a machine into a host.
func OptedInHosts() []string {
	if env := strings.TrimSpace(os.Getenv("BRAVROS_HOSTS")); env != "" {
		return normalizeHosts(strings.Split(env, ","))
	}
	data, err := os.ReadFile(hostsFilePath())
	if err != nil {
		return nil
	}
	var hf hostsFile
	if err := json.Unmarshal(data, &hf); err != nil {
		return nil
	}
	return normalizeHosts(hf.Hosts)
}

// HostOptedIn reports whether a host is enabled for bravros on this machine.
// "claude" is always true; unknown/unsupported hosts are always false.
func HostOptedIn(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "claude" {
		return true
	}
	if _, ok := supportedHosts[host]; !ok {
		return false
	}
	for _, h := range OptedInHosts() {
		if h == host {
			return true
		}
	}
	return false
}

// SetOptedInHosts merges the given hosts into the machine-global store
// (dedup/lowercase/drop-claude), then writes hosts.json atomically. Existing
// entries are preserved — this is additive opt-in, never a reset.
func SetOptedInHosts(add []string) error {
	merged := normalizeHosts(append(OptedInHosts(), add...))
	data, err := json.MarshalIndent(hostsFile{Hosts: merged}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Atomic write: temp-in-same-dir + rename so a crash never leaves a
	// half-written hosts.json.
	tmp, err := os.CreateTemp(dir, ".hosts-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	// fsync before rename so the bytes are durable before the rename publishes
	// them — mirrors writeRCAtomic/writeFileAtomic in internal/secrets. Without
	// this, a power loss mid-write on ext4 (data=writeback) could rename a
	// zero-byte hosts.json into place even though the rename itself is atomic.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, hostsFilePath())
}
