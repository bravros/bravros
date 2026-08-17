//go:build !windows

package plan

import (
	"os"
	"syscall"
)

// batchFlockLock acquires a blocking exclusive advisory lock via
// syscall.Flock(LOCK_EX) — no LOCK_NB, matching the pre-split behavior:
// the caller waits until the lock is free rather than retrying.
func batchFlockLock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

// batchFlockUnlock releases the lock acquired by batchFlockLock.
func batchFlockUnlock(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
