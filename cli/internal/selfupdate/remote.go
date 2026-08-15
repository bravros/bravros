// Remote-freshness check for bravros selfupdate — P-0003.
//
// A machine installed via `curl | sh` has no local git clone, so the git-HEAD drift
// signal in selfupdate.go never fires (cli/cmd/selfupdate.go:109-111). This file
// answers a cheaper, standalone question: is the skill payload on disk behind the
// published GitHub release? It never touches the GitHub API (rate-limited to ~60
// req/hour per IP) — the TagResolver is expected to use the unauthenticated redirect
// on the /releases/latest page instead. See cli/internal/fetch for the resolver
// implementation; this package only declares the interface it consumes.
package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bravros/bravros/cli/internal/config"
)

// TagResolver is the minimal dependency: something that can name the newest
// published release tag. cli/internal/fetch.Client satisfies it. Declaring it
// here (consumer-side interface) keeps this package free of a fetch import.
type TagResolver interface {
	ResolveLatestTag(ctx context.Context) (string, error)
}

// RemoteState is the cached result of the last remote check, persisted as JSON.
type RemoteState struct {
	LastCheck    time.Time `json:"last_check"`    // when the remote was last contacted (success OR failure)
	RemoteTag    string    `json:"remote_tag"`    // newest tag observed
	InstalledTag string    `json:"installed_tag"` // tag of the payload currently on disk
}

// RemoteResult reports what a check concluded.
type RemoteResult struct {
	Checked      bool   // false when the TTL suppressed the remote request
	Behind       bool   // true when RemoteTag differs from InstalledTag
	RemoteTag    string
	InstalledTag string
	Offline      bool // the remote request failed; Behind is meaningless
}

// DefaultRemoteCheckTTL is the minimum interval between remote requests.
const DefaultRemoteCheckTTL = time.Hour

// remoteStateFileName is the JSON state file name inside config.ConfigDir()/state.
const remoteStateFileName = ".bravros-remote-check.json"

// RemoteCheckTTL reads BRAVROS_REMOTE_CHECK_TTL (Go duration; "0" disables the
// rate limit entirely — every call checks) and falls back to DefaultRemoteCheckTTL.
// Mirrors the semantics of selfupdateCheckTTL() in cli/cmd/selfupdate.go:360.
func RemoteCheckTTL() time.Duration {
	raw := os.Getenv("BRAVROS_REMOTE_CHECK_TTL")
	if raw == "" {
		return DefaultRemoteCheckTTL
	}
	if raw == "0" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return DefaultRemoteCheckTTL
	}
	if d < 0 {
		return 0
	}
	return d
}

// RemoteStatePath returns the JSON state file location:
// config.ConfigDir() + "/state/.bravros-remote-check.json".
func RemoteStatePath() string {
	return filepath.Join(config.ConfigDir(), "state", remoteStateFileName)
}

// LoadRemoteState reads the state at path. If the file does not exist it returns a
// zero-value RemoteState and no error.
func LoadRemoteState(path string) (RemoteState, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return RemoteState{}, nil
		}
		return RemoteState{}, fmt.Errorf("open remote state: %w", err)
	}
	defer f.Close()

	var s RemoteState
	if err := json.NewDecoder(f).Decode(&s); err != nil {
		return RemoteState{}, fmt.Errorf("decode remote state: %w", err)
	}
	return s, nil
}

// SaveRemoteState writes s to path atomically (temp file in the same dir + rename),
// mirroring deploy.SaveManifest. Parent directories are created if they do not exist.
func SaveRemoteState(path string, s RemoteState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir remote state parent: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".bravros-remote-check-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp remote state: %w", err)
	}
	tmpName := tmp.Name()

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("encode remote state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp remote state: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename remote state: %w", err)
	}
	return nil
}

// CheckRemote answers "is the payload on disk behind the published release?".
//
//   - If ttl > 0 and time.Since(state.LastCheck) < ttl, it makes NO remote request
//     and returns RemoteResult{Checked: false} (Behind reported from cached tags).
//   - Otherwise it calls r.ResolveLatestTag, stamps LastCheck (whether the call
//     succeeded or failed, so a flapping network cannot bypass the rate limit),
//     persists the state, and reports Behind = RemoteTag != InstalledTag.
//   - A resolver error is NOT an error return: it yields
//     RemoteResult{Checked: true, Offline: true, Behind: false} and a nil error,
//     because offline must never block or spam. Only state-file I/O errors that
//     the caller could act on are returned.
//
// installedTag is supplied by the caller (the tag recorded when the payload was
// last fetched); pass "" when no payload has ever been fetched, which always
// counts as Behind once a remote tag is known.
func CheckRemote(ctx context.Context, r TagResolver, statePath string, ttl time.Duration, installedTag string) (RemoteResult, error) {
	state, err := LoadRemoteState(statePath)
	if err != nil {
		return RemoteResult{}, err
	}

	if ttl > 0 && time.Since(state.LastCheck) < ttl {
		return RemoteResult{
			Checked:      false,
			Behind:       state.RemoteTag != installedTag,
			RemoteTag:    state.RemoteTag,
			InstalledTag: installedTag,
		}, nil
	}

	tag, resolveErr := r.ResolveLatestTag(ctx)
	state.LastCheck = time.Now()
	state.InstalledTag = installedTag

	if resolveErr != nil {
		if err := SaveRemoteState(statePath, state); err != nil {
			return RemoteResult{}, err
		}
		return RemoteResult{
			Checked:      true,
			Offline:      true,
			Behind:       false,
			RemoteTag:    state.RemoteTag,
			InstalledTag: installedTag,
		}, nil
	}

	state.RemoteTag = tag
	if err := SaveRemoteState(statePath, state); err != nil {
		return RemoteResult{}, err
	}

	return RemoteResult{
		Checked:      true,
		Offline:      false,
		Behind:       tag != installedTag,
		RemoteTag:    tag,
		InstalledTag: installedTag,
	}, nil
}
