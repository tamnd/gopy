//go:build !windows

// Platform fd helpers for unix-family systems. socketFd is the
// integer file descriptor returned by syscall.Socket; invalidFd is
// the sentinel used to mark a closed socket. The thin wrappers below
// resolve the operations whose names or argument shapes differ from
// the Windows Winsock surface.
//
// CPython: Modules/socketmodule.c (HAVE_SOCKETPAIR / sock_close path)

package _socket

import (
	"syscall"
	"unsafe"
)

type socketFd = int

const invalidFd socketFd = -1

// socketFdValid reports whether s refers to an open socket.
func socketFdValid(s socketFd) bool { return s >= 0 }

// socketFdInt returns s as an int suitable for Python's fileno().
func socketFdInt(s socketFd) int { return int(s) }

// closeFd closes the socket fd. CPython calls close(2) here.
//
// CPython: Modules/socketmodule.c sock_close
func closeFd(s socketFd) error { return syscall.Close(s) }

// readFd reads from the socket using read(2). The wider gopy port
// only uses this for the recv(bufsize) shorthand where no flags are
// supplied.
func readFd(s socketFd, p []byte) (int, error) { return syscall.Read(s, p) }

// writeFd writes to the socket using write(2).
func writeFd(s socketFd, p []byte) (int, error) { return syscall.Write(s, p) }

// setsockoptBytes calls setsockopt(2) with an arbitrary byte slice.
//
// CPython: Modules/socketmodule.c:3370 sock_setsockopt (bytes branch)
func setsockoptBytes(fd socketFd, level, opt int, buf []byte) error {
	_, _, errno := syscall.Syscall6(
		syscall.SYS_SETSOCKOPT,
		uintptr(fd),
		uintptr(level),
		uintptr(opt),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
		0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}

// getsockoptBytes calls getsockopt(2) and returns the optval bytes.
//
// CPython: Modules/socketmodule.c:3412 sock_getsockopt (bytes branch)
func getsockoptBytes(fd socketFd, level, opt, buflen int) ([]byte, error) {
	buf := make([]byte, buflen)
	bufSize := uint32(buflen)
	_, _, errno := syscall.Syscall6(
		syscall.SYS_GETSOCKOPT,
		uintptr(fd),
		uintptr(level),
		uintptr(opt),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
		0,
	)
	if errno != 0 {
		return nil, errno
	}
	return buf[:bufSize], nil
}
