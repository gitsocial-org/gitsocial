// lock_windows.go - The advisory locks of the cache, with LockFileEx
//go:build windows

package cache

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile takes the shared or exclusive lock on the first byte of f; without wait, a held lock gives errLockBusy.
func lockFile(f *os.File, exclusive, wait bool) error {
	var flags uint32
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	if !wait {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, new(windows.Overlapped))
	if err == windows.ERROR_LOCK_VIOLATION {
		return errLockBusy
	}
	return err
}

// unlockFile releases the lock that lockFile took.
func unlockFile(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
}
