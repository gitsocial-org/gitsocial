// lock_unix.go - The advisory locks of the cache, with flock
//go:build !windows

package cache

import (
	"os"
	"syscall"
)

// lockFile takes the shared or exclusive lock on f; without wait, a held lock gives errLockBusy.
func lockFile(f *os.File, exclusive, wait bool) error {
	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	if !wait {
		how |= syscall.LOCK_NB
	}
	err := flock(f, how)
	if err == syscall.EWOULDBLOCK {
		return errLockBusy
	}
	return err
}

// unlockFile releases the lock that lockFile took.
func unlockFile(f *os.File) error {
	return flock(f, syscall.LOCK_UN)
}

// flock calls syscall.Flock again while a signal interrupts it.
func flock(f *os.File, how int) error {
	for {
		err := syscall.Flock(int(f.Fd()), how)
		if err != syscall.EINTR {
			return err
		}
	}
}
