package errors

import (
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
		// Codes a platform does not define arrive as a negative
		// sentinel from the errnocodes table; skip them so they never
		// collide with a real errno (errnos are always positive).
		if code < 0 {
			return
		}
		if _, dup := errnomap[code]; !dup {
			errnomap[code] = t
		}
	}
	add(errEAGAIN, PyExc_BlockingIOError)
	add(errEALREADY, PyExc_BlockingIOError)
	add(errEINPROGRESS, PyExc_BlockingIOError)
	add(errEWOULDBLOCK, PyExc_BlockingIOError)
	add(errEPIPE, PyExc_BrokenPipeError)
	add(errESHUTDOWN, PyExc_BrokenPipeError)
	add(errECHILD, PyExc_ChildProcessError)
	add(errECONNABORTED, PyExc_ConnectionAbortedError)
	add(errECONNREFUSED, PyExc_ConnectionRefusedError)
	add(errECONNRESET, PyExc_ConnectionResetError)
	add(errEEXIST, PyExc_FileExistsError)
	add(errENOENT, PyExc_FileNotFoundError)
	add(errEISDIR, PyExc_IsADirectoryError)
	add(errENOTDIR, PyExc_NotADirectoryError)
	add(errEINTR, PyExc_InterruptedError)
	add(errEACCES, PyExc_PermissionError)
	add(errEPERM, PyExc_PermissionError)
	add(errESRCH, PyExc_ProcessLookupError)
	add(errETIMEDOUT, PyExc_TimeoutError)
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
