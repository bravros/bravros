package git

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

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
		// LOCK_EX | LOCK_NB — non-blocking exclusive lock attempt
		flockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if flockErr == nil {
			// Lock acquired — write metadata and return
			_ = f.Truncate(0)
			_, _ = fmt.Fprintf(f, "pid=%d\ntime=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
			return &MergeLock{file: f, path: path}, nil
		}

		// EWOULDBLOCK means another process holds the lock — keep retrying
		if flockErr != syscall.EWOULDBLOCK {
			f.Close()
			return nil, fmt.Errorf("merge lock: flock error: %w", flockErr)
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
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
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
