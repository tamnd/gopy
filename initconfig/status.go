package initconfig

// StatusType discriminates Status values: ok, error (with message),
// or exit (with code).
//
// CPython: Include/cpython/initconfig.h:11 _PyStatus_TYPE_*
type StatusType int

const (
	StatusOK    StatusType = 0
	StatusError StatusType = 1
	StatusExit  StatusType = 2
)

// Status is the return type lifecycle helpers carry: ok, error with a
// message, or exit with a code. Mirrors PyStatus.
//
// The Func and ErrMsg fields are written by helpers that set an
// error so callers can format consistent diagnostics.
//
// CPython: Include/cpython/initconfig.h:10 PyStatus
type Status struct {
	Type     StatusType
	Func     string
	ErrMsg   string
	ExitCode int
}

// StatusOk returns the success Status.
//
// CPython: Python/initconfig.c PyStatus_Ok
func StatusOk() Status {
	return Status{Type: StatusOK}
}

// StatusErr builds an error Status carrying msg.
//
// CPython: Python/initconfig.c PyStatus_Error
func StatusErr(msg string) Status {
	return Status{Type: StatusError, ErrMsg: msg}
}

// StatusNoMemory builds the "memory allocation failed" Status. gopy
// rarely hits this because Go owns allocation, but the helper is kept
// for shape parity with PyStatus_NoMemory.
//
// CPython: Python/initconfig.c PyStatus_NoMemory
func StatusNoMemory() Status {
	return Status{Type: StatusError, ErrMsg: "memory allocation failed"}
}

// StatusExitCode builds an exit Status with the given code.
//
// CPython: Python/initconfig.c PyStatus_Exit
func StatusExitCode(code int) Status {
	return Status{Type: StatusExit, ExitCode: code}
}

// IsError reports whether s is an error Status.
//
// CPython: Python/initconfig.c PyStatus_IsError
func (s Status) IsError() bool { return s.Type == StatusError }

// IsExit reports whether s is an exit Status.
//
// CPython: Python/initconfig.c PyStatus_IsExit
func (s Status) IsExit() bool { return s.Type == StatusExit }

// IsException reports whether s is either an error or an exit, the
// "did the call abort early" check the lifecycle phases run after
// every step.
//
// CPython: Python/initconfig.c PyStatus_Exception
func (s Status) IsException() bool {
	return s.Type != StatusOK
}
