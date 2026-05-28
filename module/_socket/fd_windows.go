//go:build windows

// Platform fd helpers for Windows. Sockets on Windows are SOCKET
// handles (uintptr-shaped) rather than integer fds; INVALID_SOCKET
// is the sentinel. Closesocket replaces close(2), and read/write
// route through WSARecv / WSASend on stream sockets.
//
// CPython: Modules/socketmodule.c HAVE_WSARECV / closesocket path

package _socket

import (
	"syscall"
	"unsafe"
)

type socketFd = syscall.Handle

// invalidFd is the equivalent of POSIX -1. Windows defines
// INVALID_SOCKET as (SOCKET)(~0); syscall.InvalidHandle has the same
// bit pattern.
const invalidFd socketFd = syscall.InvalidHandle

// socketFdValid reports whether s refers to an open socket.
func socketFdValid(s socketFd) bool { return s != invalidFd }

// socketFdInt returns s as an int suitable for Python's fileno().
// On Windows the SOCKET value is an opaque pointer-sized handle;
// CPython exposes it directly as an integer.
func socketFdInt(s socketFd) int { return int(s) }

// closeFd closes the socket with closesocket(2).
func closeFd(s socketFd) error { return syscall.Closesocket(s) }

// readFd reads from the socket via WSARecv. Mirrors POSIX read(2)
// for the no-flags recv(bufsize) shorthand. flags=0 here matches
// MSG_NONE in CPython's sock_recv_impl.
//
// CPython: Modules/socketmodule.c:3900 sock_recv_impl
func readFd(s socketFd, p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	buf := syscall.WSABuf{Len: uint32(len(p)), Buf: &p[0]}
	var recvd, flags uint32
	if err := syscall.WSARecv(s, &buf, 1, &recvd, &flags, nil, nil); err != nil {
		return 0, err
	}
	return int(recvd), nil
}

// writeFd writes to the socket via WSASend. Mirrors POSIX write(2)
// for the no-flags send(data) shorthand.
//
// CPython: Modules/socketmodule.c:4587 sock_send_impl
func writeFd(s socketFd, p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	buf := syscall.WSABuf{Len: uint32(len(p)), Buf: &p[0]}
	var sent uint32
	if err := syscall.WSASend(s, &buf, 1, &sent, 0, nil, nil); err != nil {
		return 0, err
	}
	return int(sent), nil
}

// setsockoptBytes calls setsockopt with an arbitrary byte slice.
//
// CPython: Modules/socketmodule.c:3370 sock_setsockopt (bytes branch)
func setsockoptBytes(fd socketFd, level, opt int, buf []byte) error {
	if len(buf) == 0 {
		return syscall.Setsockopt(fd, int32(level), int32(opt), nil, 0)
	}
	return syscall.Setsockopt(fd, int32(level), int32(opt), &buf[0], int32(len(buf)))
}

// dupFd duplicates a socket handle via DuplicateHandle.
//
// CPython: Modules/socketmodule.c:3226 sock_dup_impl
func dupFd(s socketFd) (socketFd, error) {
	proc, err := syscall.GetCurrentProcess()
	if err != nil {
		return invalidFd, err
	}
	var out syscall.Handle
	if err := syscall.DuplicateHandle(proc, s, proc, &out, 0, false, syscall.DUPLICATE_SAME_ACCESS); err != nil {
		return invalidFd, err
	}
	return out, nil
}

// getsockoptBytes calls getsockopt and returns the optval bytes.
//
// CPython: Modules/socketmodule.c:3412 sock_getsockopt (bytes branch)
func getsockoptBytes(fd socketFd, level, opt, buflen int) ([]byte, error) {
	buf := make([]byte, buflen)
	sz := int32(buflen)
	var p *byte
	if buflen > 0 {
		p = &buf[0]
	}
	if err := syscall.Getsockopt(fd, int32(level), int32(opt), p, &sz); err != nil {
		return nil, err
	}
	_ = unsafe.Sizeof(buf)
	return buf[:sz], nil
}
