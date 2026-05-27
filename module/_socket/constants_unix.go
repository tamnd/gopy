//go:build !windows

// Platform-specific socket constants not exported by the cross-platform
// syscall package. Values taken from Darwin/Linux headers.
//
// CPython: Modules/socketmodule.c PySocketModule_InsertConstants

package _socket

import "syscall"

const (
	// INADDR_* — net/in.h
	inaddr_any       = 0x00000000
	inaddr_broadcast = 0xffffffff
	inaddr_loopback  = 0x7f000001
	inaddr_none      = 0xffffffff

	// getaddrinfo error codes — netdb.h (POSIX values)
	eai_noname = 8  // EAI_NONAME
	eai_again  = 2  // EAI_AGAIN
	eai_fail   = 4  // EAI_FAIL
	eai_nodata = 7  // EAI_NODATA

	// getaddrinfo flags — netdb.h
	ai_passive     = 0x00000001
	ai_canonname   = 0x00000002
	ai_numerichost = 0x00000004
	ai_v4mapped    = 0x00000800
	ai_all         = 0x00000100
	ai_addrconfig  = 0x00000400
	ai_numericserv = 0x00001000

	// getnameinfo flags — netdb.h
	ni_nofqdn      = 0x00000001
	ni_numerichost = 0x00000002
	ni_namereqd    = 0x00000004
	ni_numericserv = 0x00000008
	ni_dgram       = 0x00000010

	// MSG_* send/recv flags
	msg_waitall  = int(syscall.MSG_WAITALL)
	msg_dontwait = int(syscall.MSG_DONTWAIT)
)
