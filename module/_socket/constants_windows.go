//go:build windows

// Platform-specific socket constants for Windows (Winsock2). Values
// taken from WinSock2.h / ws2tcpip.h.
//
// CPython: Modules/socketmodule.c PySocketModule_InsertConstants

package _socket

const (
	// INADDR_* — WinSock2.h
	inaddr_any       = 0x00000000
	inaddr_broadcast = 0xffffffff
	inaddr_loopback  = 0x7f000001
	inaddr_none      = 0xffffffff

	// getaddrinfo error codes — ws2tcpip.h
	eai_noname = 11001 // WSAHOST_NOT_FOUND
	eai_again  = 11002 // WSATRY_AGAIN
	eai_fail   = 11003 // WSANO_RECOVERY
	eai_nodata = 11001 // same as EAI_NONAME on Windows

	// getaddrinfo flags — ws2tcpip.h
	ai_passive     = 0x00000001
	ai_canonname   = 0x00000002
	ai_numerichost = 0x00000004
	ai_v4mapped    = 0x00000800
	ai_all         = 0x00000100
	ai_addrconfig  = 0x00000400
	ai_numericserv = 0x00000008

	// getnameinfo flags — ws2tcpip.h
	ni_nofqdn      = 0x00000001
	ni_numerichost = 0x00000002
	ni_namereqd    = 0x00000004
	ni_numericserv = 0x00000008
	ni_dgram       = 0x00000010

	// MSG_* send/recv flags — WinSock2.h
	msg_oob       = int(0x1)
	msg_peek      = int(0x2)
	msg_dontroute = int(0x4)
	msg_waitall   = int(0x00000008)
	msg_dontwait  = int(0x00000040) // not a real Winsock flag; stub for CPython compat

	// SO_* socket options not exported by Go's Windows syscall package
	// Values from WinSock2.h.
	so_error     = int(0x1007)
	so_type      = int(0x1008)
	so_oobinline  = int(0x0100)
	so_rcvlowat  = int(0x1004)
	so_sndlowat  = int(0x1003)
	so_rcvtimeo  = int(0x1006)
	so_sndtimeo  = int(0x1005)
)
