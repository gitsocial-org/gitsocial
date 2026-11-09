//go:build !windows

// detach_unix.go - The process attributes of a detached child on Unix.
package desktop

import "syscall"

// detached returns the attributes that start a child in its own session.
func detached() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
