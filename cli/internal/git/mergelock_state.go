package git

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// LockState is the on-disk JSON representation of the merge lock written by
// the `kaisser merge-lock` verb. The lock is considered held while the file
// exists and its acquired_at + ttl_seconds window has not elapsed
// (with a grace period) AND the holder process is alive.
//
// This is layered ON TOP of the existing flock primitive in mergelock.go:
// flock atomizes the acquire write; the file-content + PID-aliveness define
// whether the lock is held between caller invocations.
type LockState struct {
	PID        int               `json:"pid"`
	AcquiredAt time.Time         `json:"acquired_at"`
	TTLSeconds int               `json:"ttl_seconds"`
	Meta       map[string]string `json:"meta,omitempty"`
}

// DefaultStaleGrace is the extra slack added to TTL before a lock is
// considered stale (so a heartbeat tick that arrives slightly late
// does not get treated as a dead lock).
const DefaultStaleGrace = 30 * time.Second

// ErrLockHeldByActiveState is returned by AcquireWithLockState when the
// existing lockfile still represents a valid (non-stale) hold.
var ErrLockHeldByActiveState = errors.New("merge lock state held by another holder")

// MergeLockFilePath returns the canonical merge-lock filepath.
// Exported wrapper around the package-internal mergeLockFilePath().
func MergeLockFilePath() (string, error) {
	return mergeLockFilePath()
}

// ReadLockState reads + parses the JSON LockState from the given path.
//
//	(state, mtime, true,  nil)  — JSON parsed successfully
//	(nil,   mtime, false, nil)  — file exists but is not JSON (e.g. legacy `pid=...` text)
//	(nil,   zero,  false, nil)  — file does not exist
//	(nil,   zero,  false, err)  — filesystem error
func ReadLockState(path string) (*LockState, time.Time, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, time.Time{}, false, nil
		}
		return nil, time.Time{}, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, false, err
	}
	if len(data) == 0 {
		return nil, info.ModTime(), false, nil
	}
	var state LockState
	if err := json.Unmarshal(data, &state); err != nil {
		// Legacy `pid=...` text format (written by AcquireMergeLock) or other
		// non-JSON content — not our JSON state.
		return nil, info.ModTime(), false, nil
	}
	if state.PID == 0 || state.AcquiredAt.IsZero() {
		return nil, info.ModTime(), false, nil
	}
	return &state, info.ModTime(), true, nil
}

// IsLockStateStale reports whether the lock state has aged past TTL+grace AND
// the holder process is no longer alive. Both conditions must hold for a lock
// to be considered stale — a long-running merge whose mtime is fresh but whose
// PID is missing (very rare; usually only on cross-host NFS scenarios) should
// not be reaped, and vice versa.
func IsLockStateStale(state *LockState, mtime time.Time, grace time.Duration) bool {
	if state == nil {
		return false
	}
	ttl := time.Duration(state.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	// Use the later of acquired_at and mtime as the "last touched" anchor —
	// heartbeats bump mtime to keep a long merge alive.
	anchor := state.AcquiredAt
	if mtime.After(anchor) {
		anchor = mtime
	}
	expired := time.Since(anchor) > (ttl + grace)
	if !expired {
		return false
	}
	return !PIDAlive(state.PID)
}

// PIDAlive reports whether a process with the given PID exists on this host.
// Implemented via `kill -0 <pid>`: returns true if the signal-zero call succeeds
// or returns EPERM (process exists but is owned by another uid).
func PIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	// EPERM = process exists but we don't own it (still "alive").
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}

// ClearStaleLockfile deletes the lockfile at path iff it contains a stale
// JSON LockState (per IsLockStateStale). Returns true if it was cleared.
// Non-JSON content (e.g. orphaned `pid=...` text) is NOT touched here — that
// would interfere with concurrent merge-pr usage during the transition window.
func ClearStaleLockfile(path string, grace time.Duration) (bool, error) {
	state, mtime, ok, err := ReadLockState(path)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	if !IsLockStateStale(state, mtime, grace) {
		return false, nil
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// TouchLockfile updates the lockfile's mtime to now. Used by the heartbeat
// ticker (--hold mode) to keep a long-running merge alive past its initial
// TTL window without re-acquiring.
func TouchLockfile(path string) error {
	now := time.Now()
	return os.Chtimes(path, now, now)
}

// AcquireWithLockState performs the full atomic acquire dance for the
// merge-lock verb:
//
//  1. Pre-clears the lockfile if it contains a stale JSON LockState.
//  2. If a non-stale JSON LockState exists, returns ErrLockHeldByActiveState
//     without attempting flock — the holder is still within TTL.
//  3. Acquires the underlying flock via AcquireMergeLock (with timeout),
//     which atomizes the write window against concurrent merge-pr / merge-lock
//     processes.
//  4. Under flock, overwrites the file with the provided JSON state.
//  5. Releases flock and returns nil. The JSON state remains on disk to
//     represent the held lock until release/stale.
//
// On flock timeout, returns a *MergeLockTimeoutError (use IsMergeLockTimeout).
//
// NOTE: There is a small TOCTOU window between step 2 (read) and step 3
// (flock) where another acquire could race in. That race is mitigated — but
// not eliminated — by the underlying flock serialization. For the common
// sequential-acquire pattern this is sufficient.
func AcquireWithLockState(state *LockState, timeout time.Duration, grace time.Duration) error {
	path, err := MergeLockFilePath()
	if err != nil {
		return fmt.Errorf("merge-lock: resolve path: %w", err)
	}
	// Step 1: stale-clear.
	if _, err := ClearStaleLockfile(path, grace); err != nil {
		return fmt.Errorf("merge-lock: stale-clear: %w", err)
	}
	// Step 2: active-state pre-check.
	existing, mtime, ok, err := ReadLockState(path)
	if err != nil {
		return fmt.Errorf("merge-lock: read existing state: %w", err)
	}
	if ok && !IsLockStateStale(existing, mtime, grace) {
		return ErrLockHeldByActiveState
	}
	// Step 3: flock via the existing primitive.
	lock, err := AcquireMergeLock(timeout)
	if err != nil {
		return err
	}
	defer lock.Release()
	// Step 4: overwrite with our JSON state. AcquireMergeLock wrote the
	// legacy `pid=...` text into the file; that is now stale and gets
	// replaced by our JSON payload.
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("merge-lock: marshal state: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("merge-lock: write state: %w", err)
	}
	return nil
}
