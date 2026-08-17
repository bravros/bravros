package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// errWouldBlock is the platform-neutral sentinel tryLock returns when the
// lock is already held by another process. Each platform's tryLock
// implementation (mergelock_unix.go / mergelock_windows.go) maps its native
// "would block" condition (EWOULDBLOCK on unix, ERROR_LOCK_VIOLATION on
// Windows) onto this same error so the retry loop below is written once.
var errWouldBlock = errors.New("merge lock: would block")

// MergeLock provides per-repo exclusive locking for merge-pr operations.
// It uses syscall.Flock (POSIX advisory lock) which is safe on Linux and macOS.
//
// Lock file location: <gitRoot>/.git/bravros-merge.lock
// This avoids placing the lock inside .planning/ (which is user-visible and
// tracked by audit rules), and keeps it co-located with the git internals.
//
// Usage:
//
//	lock, err := AcquireMergeLock(60 * time.Second)
//	if err != nil { return conflictError }
//	defer lock.Release()
type MergeLock struct {
	file *os.File
	path string
}

// lockFilePath returns the merge lock file path under the repo's .git dir.
// Falls back to the cwd-relative path if git root detection fails.
func mergeLockFilePath() (string, error) {
	// Find the .git directory using git rev-parse
	out, _, err := Run("git", "rev-parse", "--git-dir")
	if err != nil || out == "" {
		// Fallback: use cwd/.git/bravros-merge.lock
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			return "", fmt.Errorf("cannot determine git dir or cwd: %v", cwdErr)
		}
		return filepath.Join(cwd, ".git", "bravros-merge.lock"), nil
	}
	// git rev-parse --git-dir returns the .git dir path (may be relative or absolute)
	gitDir := out
	if !filepath.IsAbs(gitDir) {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			return "", fmt.Errorf("cannot make git dir absolute: %v", cwdErr)
		}
		gitDir = filepath.Join(cwd, gitDir)
	}
	return filepath.Join(gitDir, "bravros-merge.lock"), nil
}

// AcquireMergeLock creates and exclusively flocks the merge lock file.
// If another process holds the lock, it retries until timeout elapses.
// Returns ErrMergeLockTimeout if the lock cannot be acquired within the deadline.
func AcquireMergeLock(timeout time.Duration) (*MergeLock, error) {
	path, err := mergeLockFilePath()
	if err != nil {
		return nil, fmt.Errorf("merge lock: cannot resolve lock path: %w", err)
	}

	// Ensure the parent directory exists.
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
		return nil, fmt.Errorf("merge lock: cannot create lock dir: %w", mkErr)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("merge lock: cannot open lock file %s: %w", path, err)
	}

	deadline := time.Now().Add(timeout)
	for {
		// Non-blocking exclusive lock attempt (syscall.Flock on unix,
		// LockFileEx on Windows — see mergelock_unix.go / mergelock_windows.go).
		lockErr := tryLock(f)
		if lockErr == nil {
			// Lock acquired — write metadata and return
			_ = f.Truncate(0)
			_, _ = fmt.Fprintf(f, "pid=%d\ntime=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
			return &MergeLock{file: f, path: path}, nil
		}

		// errWouldBlock means another process holds the lock — keep retrying
		if !errors.Is(lockErr, errWouldBlock) {
			f.Close()
			return nil, fmt.Errorf("merge lock: flock error: %w", lockErr)
		}

		if time.Now().After(deadline) {
			f.Close()
			return nil, &MergeLockTimeoutError{Path: path, Timeout: timeout}
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// Release releases the exclusive lock and closes the file.
// Safe to call multiple times (idempotent after first call).
func (l *MergeLock) Release() {
	if l == nil || l.file == nil {
		return
	}
	unlock(l.file)
	_ = l.file.Close()
	l.file = nil
}

// Path returns the file path of the lock.
func (l *MergeLock) Path() string {
	return l.path
}

// MergeLockTimeoutError is returned when the lock cannot be acquired within timeout.
type MergeLockTimeoutError struct {
	Path    string
	Timeout time.Duration
}

func (e *MergeLockTimeoutError) Error() string {
	return fmt.Sprintf("another merge is in progress (could not acquire %s within %s)", e.Path, e.Timeout)
}

// IsMergeLockTimeout returns true if the error is a lock timeout.
func IsMergeLockTimeout(err error) bool {
	_, ok := err.(*MergeLockTimeoutError)
	return ok
}

// MergeConflictError is returned when gh pr merge fails due to merge conflicts.
// The caller (mergepr.go) should exit with code 2 for this error type,
// so the skill loop can distinguish "skip this plan" from hard failures.
type MergeConflictError struct {
	PR     int
	Stderr string
}

func (e *MergeConflictError) Error() string {
	return fmt.Sprintf("PR #%d has merge conflicts and cannot be merged: %s", e.PR, e.Stderr)
}

// IsMergeConflict returns true if err is a MergeConflictError.
func IsMergeConflict(err error) bool {
	_, ok := err.(*MergeConflictError)
	return ok
}
