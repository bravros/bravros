package plan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const batchProgressFile = ".planning/.batch-progress.json"
const batchProgressLock = ".planning/.batch-progress.lock"

// ValidStates is the canonical list of valid batch progress states.
var ValidStates = []string{
	"queued",
	"executing",
	"merged",
	"pr-open",
	"conflict-skipped",
	"failed",
	"skipped",
	"blocked-by-dep",
}

// PlanEntry records the state of one plan within a round.
type PlanEntry struct {
	Plan      string `json:"plan"`
	State     string `json:"state"`
	UpdatedAt string `json:"updated_at"` // RFC3339
}

// RoundState holds all plan entries for one round.
type RoundState struct {
	Round int         `json:"round"`
	Plans []PlanEntry `json:"plans"`
}

// BatchProgress is the aggregate view of all rounds.
type BatchProgress struct {
	Rounds []RoundState `json:"rounds"`
}

// isValidState returns true if s is in ValidStates.
func isValidState(s string) bool {
	for _, v := range ValidStates {
		if s == v {
			return true
		}
	}
	return false
}

// acquireBatchLock opens (or creates) the batch progress lock file and
// acquires an exclusive flock on it. The caller must call releaseBatchLock
// when done. Pattern mirrors cli/internal/git/mergelock.go but inlined here
// to avoid a cross-package import.
func acquireBatchLock() (*os.File, error) {
	f, err := os.OpenFile(batchProgressLock, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("batch progress: cannot open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("batch progress: flock acquire failed: %w", err)
	}
	return f, nil
}

// releaseBatchLock releases the flock and closes the file.
func releaseBatchLock(f *os.File) {
	if f == nil {
		return
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	_ = f.Close()
}

// WriteBatchProgress appends or updates the (round, plan) entry in
// .planning/.batch-progress.json atomically (tmp file + rename).
// Uses syscall.Flock for concurrent-write safety.
// Returns an error if state is not in ValidStates or .planning/ does not exist.
func WriteBatchProgress(round int, plan, state string) error {
	if !isValidState(state) {
		return fmt.Errorf("invalid state %q: valid states are %v", state, ValidStates)
	}
	if _, err := os.Stat(".planning"); os.IsNotExist(err) {
		return fmt.Errorf("batch progress: .planning/ directory not found — run from project root")
	}

	lf, err := acquireBatchLock()
	if err != nil {
		return err
	}
	defer releaseBatchLock(lf)

	// Read existing progress (or start fresh).
	bp, err := readBatchProgressLocked()
	if err != nil {
		return err
	}

	// Find or create the RoundState for this round.
	roundIdx := -1
	for i, rs := range bp.Rounds {
		if rs.Round == round {
			roundIdx = i
			break
		}
	}
	if roundIdx == -1 {
		bp.Rounds = append(bp.Rounds, RoundState{Round: round})
		roundIdx = len(bp.Rounds) - 1
	}

	// Find or create the PlanEntry for this plan within the round.
	now := time.Now().UTC().Format(time.RFC3339)
	planIdx := -1
	for i, pe := range bp.Rounds[roundIdx].Plans {
		if pe.Plan == plan {
			planIdx = i
			break
		}
	}
	entry := PlanEntry{Plan: plan, State: state, UpdatedAt: now}
	if planIdx == -1 {
		bp.Rounds[roundIdx].Plans = append(bp.Rounds[roundIdx].Plans, entry)
	} else {
		bp.Rounds[roundIdx].Plans[planIdx] = entry
	}

	return writeBatchProgressAtomic(bp)
}

// readBatchProgressLocked reads .planning/.batch-progress.json without acquiring a lock.
// Used internally by WriteBatchProgress (which holds the lock).
func readBatchProgressLocked() (*BatchProgress, error) {
	bp := &BatchProgress{}
	data, err := os.ReadFile(batchProgressFile)
	if os.IsNotExist(err) {
		return bp, nil
	}
	if err != nil {
		return nil, fmt.Errorf("batch progress: read error: %w", err)
	}
	if err := json.Unmarshal(data, bp); err != nil {
		return nil, fmt.Errorf("batch progress: JSON parse error: %w", err)
	}
	return bp, nil
}

// writeBatchProgressAtomic marshals bp to a temp file then renames it over the
// canonical path. Must be called while holding the batch lock.
func writeBatchProgressAtomic(bp *BatchProgress) error {
	dir := filepath.Dir(batchProgressFile)
	tmp, err := os.CreateTemp(dir, ".batch-progress-*.json")
	if err != nil {
		return fmt.Errorf("batch progress: cannot create temp file: %w", err)
	}
	tmpName := tmp.Name()

	data, err := json.MarshalIndent(bp, "", "  ")
	if err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("batch progress: JSON marshal error: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("batch progress: write error: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("batch progress: close temp file error: %w", err)
	}
	if err := os.Rename(tmpName, batchProgressFile); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("batch progress: rename error: %w", err)
	}
	return nil
}

// ReadBatchProgress reads .planning/.batch-progress.json and returns the
// aggregate BatchProgress. Returns an empty struct (not an error) if the
// file is absent.
func ReadBatchProgress() (*BatchProgress, error) {
	bp := &BatchProgress{}
	data, err := os.ReadFile(batchProgressFile)
	if os.IsNotExist(err) {
		return bp, nil
	}
	if err != nil {
		return nil, fmt.Errorf("batch progress: read error: %w", err)
	}
	if err := json.Unmarshal(data, bp); err != nil {
		return nil, fmt.Errorf("batch progress: JSON parse error: %w", err)
	}
	return bp, nil
}

// ClearBatchProgress removes .planning/.batch-progress.json.
// No-op (nil error) if the file does not exist.
func ClearBatchProgress() error {
	err := os.Remove(batchProgressFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("batch progress: remove error: %w", err)
	}
	return nil
}
