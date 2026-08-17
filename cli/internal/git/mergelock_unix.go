//go:build !windows

package git

import (
	"os"
	"syscall"
)

// tryLock attempts a non-blocking exclusive advisory lock via syscall.Flock
// (LOCK_EX|LOCK_NB). Behavior is unchanged from the pre-split implementation:
// EWOULDBLOCK is mapped onto the shared errWouldBlock sentinel so the retry
// loop in mergelock.go can stay platform-neutral.
func tryLock(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == syscall.EWOULDBLOCK {
		return errWouldBlock
	}
	return err
}

// unlock releases the advisory lock acquired by tryLock.
func unlock(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
