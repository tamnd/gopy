//go:build windows

package errors

// errno codes for the errnomap promotion table on Windows. Go's
// syscall package fabricates E* constants as 1<<29+iota there, so we
// hard-code the Universal CRT <errno.h> values CPython actually uses
// (EEXIST == 17). These line up with the winerror->errno translation
// the VM applies before promotion and with the errno module's table.
// ESHUTDOWN has no ucrt definition, so CPython omits it on Windows; the
// negative sentinel makes errnomap skip it.
//
// CPython: Objects/exceptions.c:4470 _PyExc_InitState ADD_ERRNO panel
// (values from ucrt <errno.h>)
const (
	errEAGAIN       = 11
	errEALREADY     = 103
	errEINPROGRESS  = 112
	errEWOULDBLOCK  = 140
	errEPIPE        = 32
	errESHUTDOWN    = -1
	errECHILD       = 10
	errECONNABORTED = 106
	errECONNREFUSED = 107
	errECONNRESET   = 108
	errEEXIST       = 17
	errENOENT       = 2
	errEISDIR       = 21
	errENOTDIR      = 20
	errEINTR        = 4
	errEACCES       = 13
	errEPERM        = 1
	errESRCH        = 3
	errETIMEDOUT    = 138
)
