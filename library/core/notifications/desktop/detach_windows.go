// detach_windows.go - The process attributes of a detached child on Windows, where the Linux sender does not run.
package desktop

import "syscall"

// detached returns no attributes.
func detached() *syscall.SysProcAttr {
	return nil
}
