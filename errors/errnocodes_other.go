//go:build !windows

package errors

import "syscall"

// errno codes for the errnomap promotion table. On non-Windows
// platforms Go's syscall package carries the real POSIX values, which
// vary across systems (ETIMEDOUT is 110 on Linux, 60 on macOS), so we
// take them from syscall rather than hard-coding.
//
// CPython: Objects/exceptions.c:4470 _PyExc_InitState ADD_ERRNO panel
var (
	errEAGAIN       = int(syscall.EAGAIN)
	errEALREADY     = int(syscall.EALREADY)
	errEINPROGRESS  = int(syscall.EINPROGRESS)
	errEWOULDBLOCK  = int(syscall.EWOULDBLOCK)
	errEPIPE        = int(syscall.EPIPE)
	errESHUTDOWN    = int(syscall.ESHUTDOWN)
	errECHILD       = int(syscall.ECHILD)
	errECONNABORTED = int(syscall.ECONNABORTED)
	errECONNREFUSED = int(syscall.ECONNREFUSED)
	errECONNRESET   = int(syscall.ECONNRESET)
	errEEXIST       = int(syscall.EEXIST)
	errENOENT       = int(syscall.ENOENT)
	errEISDIR       = int(syscall.EISDIR)
	errENOTDIR      = int(syscall.ENOTDIR)
	errEINTR        = int(syscall.EINTR)
	errEACCES       = int(syscall.EACCES)
	errEPERM        = int(syscall.EPERM)
	errESRCH        = int(syscall.ESRCH)
	errETIMEDOUT    = int(syscall.ETIMEDOUT)
)
