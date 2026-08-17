//go:build windows

package git

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// tryLock attempts a non-blocking exclusive lock via LockFileEx, mirroring the
// unix syscall.Flock(LOCK_EX|LOCK_NB) semantics: LOCKFILE_EXCLUSIVE_LOCK asks
// for an exclusive lock, LOCKFILE_FAIL_IMMEDIATELY makes the call non-blocking
// instead of waiting for the lock to free up.
//
// ERROR_LOCK_VIOLATION is the "another process holds it" condition on
// Windows — the direct analog of EWOULDBLOCK on unix — so it maps onto the
// same errWouldBlock sentinel the shared retry loop in mergelock.go expects.
func tryLock(f *os.File) error {
	handle := windows.Handle(f.Fd())
	overlapped := new(windows.Overlapped)
	err := windows.LockFileEx(
		handle,
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,    // reserved, must be zero
		1, 0, // lock a single byte — enough to represent exclusive ownership
		overlapped,
	)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return errWouldBlock
	}
	return err
}

// unlock releases the lock acquired by tryLock.
func unlock(f *os.File) {
	handle := windows.Handle(f.Fd())
	overlapped := new(windows.Overlapped)
	_ = windows.UnlockFileEx(handle, 0, 1, 0, overlapped)
}
