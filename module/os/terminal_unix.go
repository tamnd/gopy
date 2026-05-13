//go:build !windows

package os

import (
	"syscall"
	"unsafe"
)

// terminalSize returns the (columns, lines) of the controlling terminal by
// issuing TIOCGWINSZ against stdout (fd 1). Falls back to (80, 24) on error.
//
// CPython: Modules/posixmodule.c:15358 os_get_terminal_size_impl
func terminalSize() (int, int, error) {
	type winsize struct {
		Row, Col       uint16
		Xpixel, Ypixel uint16
	}
	var ws winsize
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, 1,
		syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws)))
	if errno != 0 {
		return 80, 24, errno
	}
	return int(ws.Col), int(ws.Row), nil
}
