package errors

import (
	"syscall"

	"github.com/tamnd/gopy/objects"
)

// OSError is the base for every errno-driven exception. CPython
// dispatches OSError(errno, msg, ...) at construction time and
// promotes the result to the matching subclass via errnomap. We
// expose ErrnoSubclass for callers that build OSError instances on
// the Go side.
//
// CPython: Objects/exceptions.c:1970 OSError_init
var (
	PyExc_OSError                = newExcType("OSError", []*objects.Type{PyExc_Exception})
	PyExc_BlockingIOError        = newExcType("BlockingIOError", []*objects.Type{PyExc_OSError})
	PyExc_ConnectionError        = newExcType("ConnectionError", []*objects.Type{PyExc_OSError})
	PyExc_ChildProcessError      = newExcType("ChildProcessError", []*objects.Type{PyExc_OSError})
	PyExc_BrokenPipeError        = newExcType("BrokenPipeError", []*objects.Type{PyExc_ConnectionError})
	PyExc_ConnectionAbortedError = newExcType("ConnectionAbortedError", []*objects.Type{PyExc_ConnectionError})
	PyExc_ConnectionRefusedError = newExcType("ConnectionRefusedError", []*objects.Type{PyExc_ConnectionError})
	PyExc_ConnectionResetError   = newExcType("ConnectionResetError", []*objects.Type{PyExc_ConnectionError})
	PyExc_FileExistsError        = newExcType("FileExistsError", []*objects.Type{PyExc_OSError})
	PyExc_FileNotFoundError      = newExcType("FileNotFoundError", []*objects.Type{PyExc_OSError})
	PyExc_IsADirectoryError      = newExcType("IsADirectoryError", []*objects.Type{PyExc_OSError})
	PyExc_NotADirectoryError     = newExcType("NotADirectoryError", []*objects.Type{PyExc_OSError})
	PyExc_InterruptedError       = newExcType("InterruptedError", []*objects.Type{PyExc_OSError})
	PyExc_PermissionError        = newExcType("PermissionError", []*objects.Type{PyExc_OSError})
	PyExc_ProcessLookupError     = newExcType("ProcessLookupError", []*objects.Type{PyExc_OSError})
	PyExc_TimeoutError           = newExcType("TimeoutError", []*objects.Type{PyExc_OSError})
)

// errnomap mirrors CPython's errno → exception class map. Codes that
// are absent fall through to PyExc_OSError itself, matching CPython's
// "no entry → OSError" rule. Some errnos alias on certain platforms
// (EAGAIN == EWOULDBLOCK on Linux); the init loop ignores duplicates
// so the first ADD_ERRNO call wins, the same order CPython uses.
//
// CPython: Objects/exceptions.c:4470 _PyExc_InitState ADD_ERRNO panel
var errnomap = map[int]*objects.Type{}

func init() {
	add := func(code int, t *objects.Type) {
		if _, dup := errnomap[code]; !dup {
			errnomap[code] = t
		}
	}
	add(int(syscall.EAGAIN), PyExc_BlockingIOError)
	add(int(syscall.EALREADY), PyExc_BlockingIOError)
	add(int(syscall.EINPROGRESS), PyExc_BlockingIOError)
	add(int(syscall.EWOULDBLOCK), PyExc_BlockingIOError)
	add(int(syscall.EPIPE), PyExc_BrokenPipeError)
	add(int(syscall.ESHUTDOWN), PyExc_BrokenPipeError)
	add(int(syscall.ECHILD), PyExc_ChildProcessError)
	add(int(syscall.ECONNABORTED), PyExc_ConnectionAbortedError)
	add(int(syscall.ECONNREFUSED), PyExc_ConnectionRefusedError)
	add(int(syscall.ECONNRESET), PyExc_ConnectionResetError)
	add(int(syscall.EEXIST), PyExc_FileExistsError)
	add(int(syscall.ENOENT), PyExc_FileNotFoundError)
	add(int(syscall.EISDIR), PyExc_IsADirectoryError)
	add(int(syscall.ENOTDIR), PyExc_NotADirectoryError)
	add(int(syscall.EINTR), PyExc_InterruptedError)
	add(int(syscall.EACCES), PyExc_PermissionError)
	add(int(syscall.EPERM), PyExc_PermissionError)
	add(int(syscall.ESRCH), PyExc_ProcessLookupError)
	add(int(syscall.ETIMEDOUT), PyExc_TimeoutError)
}

// ErrnoSubclass returns the OSError subclass that CPython would pick
// for the given errno. Unknown codes fall back to PyExc_OSError.
//
// CPython: Objects/exceptions.c:2158 errnomap promotion in OSError_new
func ErrnoSubclass(errno int) *objects.Type {
	if t, ok := errnomap[errno]; ok {
		return t
	}
	return PyExc_OSError
}
