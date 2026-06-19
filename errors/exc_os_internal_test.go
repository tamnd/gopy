package errors

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestErrnoSubclass drives ErrnoSubclass off the same platform errno
// codes errnomap is built from (errEEXIST and friends), so the mapping
// is exercised with the values that actually reach it at runtime: real
// POSIX numbers on Unix, ucrt numbers on Windows.
func TestErrnoSubclass(t *testing.T) {
	cases := []struct {
		errno int
		want  *objects.Type
	}{
		{errENOENT, PyExc_FileNotFoundError},
		{errEEXIST, PyExc_FileExistsError},
		{errEACCES, PyExc_PermissionError},
		{errEPERM, PyExc_PermissionError},
		{errEINTR, PyExc_InterruptedError},
		{errEPIPE, PyExc_BrokenPipeError},
		{errECHILD, PyExc_ChildProcessError},
		{errEISDIR, PyExc_IsADirectoryError},
		{errENOTDIR, PyExc_NotADirectoryError},
		{errECONNREFUSED, PyExc_ConnectionRefusedError},
		{errECONNRESET, PyExc_ConnectionResetError},
		{errECONNABORTED, PyExc_ConnectionAbortedError},
		{errESRCH, PyExc_ProcessLookupError},
		{errETIMEDOUT, PyExc_TimeoutError},
		{0, PyExc_OSError},
		{99999, PyExc_OSError},
	}
	for _, c := range cases {
		if got := ErrnoSubclass(c.errno); got != c.want {
			t.Errorf("ErrnoSubclass(%d) = %v, want %v", c.errno, got, c.want)
		}
	}
}
