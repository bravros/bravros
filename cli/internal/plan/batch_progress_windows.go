//go:build windows

package plan

import (
	"os"

	"golang.org/x/sys/windows"
)

// batchFlockLock acquires a blocking exclusive lock via LockFileEx —
// LOCKFILE_EXCLUSIVE_LOCK only, deliberately omitting
// LOCKFILE_FAIL_IMMEDIATELY so the call blocks until the lock is free,
// matching the pre-split unix syscall.Flock(LOCK_EX) behavior (no LOCK_NB).
func batchFlockLock(f *os.File) error {
	handle := windows.Handle(f.Fd())
	overlapped := new(windows.Overlapped)
	return windows.LockFileEx(
		handle,
		windows.LOCKFILE_EXCLUSIVE_LOCK,
		0,
		1, 0,
		overlapped,
	)
}

// batchFlockUnlock releases the lock acquired by batchFlockLock.
func batchFlockUnlock(f *os.File) {
	handle := windows.Handle(f.Fd())
	overlapped := new(windows.Overlapped)
	_ = windows.UnlockFileEx(handle, 0, 1, 0, overlapped)
}
