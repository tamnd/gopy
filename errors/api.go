package errors

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
	"github.com/tamnd/gopy/traceback"
)

// Set raises an exception of type t with the given args.
//
// CPython: Python/errors.c:L83 _PyErr_SetObject
func Set(ts *state.Thread, t *objects.Type, args *objects.Tuple) {
	exc := New(t, args)
	Raise(ts, exc)
}

// SetString raises an exception of type t with a single-string args.
//
// CPython: Python/errors.c:L283 _PyErr_SetString
func SetString(ts *state.Thread, t *objects.Type, msg string) {
	args := objects.NewTuple([]objects.Object{objects.NewStr(msg)})
	Set(ts, t, args)
}

// Format raises an exception built from a printf-style template.
// Returns nil so callers can `return errors.Format(ts, ...)`.
//
// CPython: Python/errors.c:L1138 _PyErr_FormatV
func Format(ts *state.Thread, t *objects.Type, format string, args ...any) *Exception {
	SetString(ts, t, fmt.Sprintf(format, args...))
	return Occurred(ts)
}

// Occurred returns the current exception or nil. Mirrors PyErr_Occurred.
//
// CPython: Python/errors.c:L138 _PyErr_Occurred
func Occurred(ts *state.Thread) *Exception {
	v := ts.CurrentException()
	if v == nil {
		return nil
	}
	exc, _ := v.(*Exception)
	return exc
}

// Clear drops the current exception. Mirrors PyErr_Clear.
//
// CPython: Python/errors.c:L488 _PyErr_Clear
func Clear(ts *state.Thread) {
	ts.SetException(nil)
}

// Fetch atomically removes and returns the current exception triple
// (type, value, traceback). Mirrors PyErr_Fetch.
//
// CPython: Python/errors.c:L460 _PyErr_Fetch
func Fetch(ts *state.Thread) (typ *objects.Type, value *Exception, tb *traceback.Traceback) {
	v := ts.SwapException(nil)
	if v == nil {
		return nil, nil, nil
	}
	exc, _ := v.(*Exception)
	if exc == nil {
		return nil, nil, nil
	}
	return exc.ExcType, exc, exc.TB
}

// Handled returns the currently-handled exception (the one a running
// except handler is processing), or nil. Mirrors reading
// exc_info->exc_value off the thread state.
//
// CPython: Python/errors.c _PyErr_GetHandledException
func Handled(ts *state.Thread) *Exception {
	v := ts.HandledException()
	if v == nil {
		return nil
	}
	exc, _ := v.(*Exception)
	return exc
}

// HandledAsObject returns the currently-handled exception as an objects.Object
// so generator structs (which cannot import the errors package) can store it.
// Returns nil when there is no active handled exception.
//
// CPython: Python/errors.c _PyErr_GetHandledException
func HandledAsObject(ts *state.Thread) objects.Object {
	exc := Handled(ts)
	if exc == nil {
		return nil
	}
	return exc
}

// SetHandledFromObject installs an objects.Object previously saved by
// HandledAsObject as the new handled exception. Pass nil to clear.
//
// CPython: Python/errors.c _PyErr_SetHandledException
func SetHandledFromObject(ts *state.Thread, o objects.Object) {
	if o == nil {
		ts.SetHandledException(nil)
		return
	}
	if exc, ok := o.(*Exception); ok {
		ts.SetHandledException(exc)
	}
}

// SetHandled installs exc as the currently-handled exception. PUSH_EXC_INFO
// and POP_EXCEPT call this so sys.exc_info() can read it.
//
// CPython: Python/errors.c _PyErr_SetHandledException
func SetHandled(ts *state.Thread, exc *Exception) {
	if exc == nil {
		ts.SetHandledException(nil)
		return
	}
	ts.SetHandledException(exc)
}

// Restore atomically installs an exception triple. Mirrors PyErr_Restore.
//
// CPython: Python/errors.c:L37 _PyErr_Restore
func Restore(ts *state.Thread, typ *objects.Type, value *Exception, tb *traceback.Traceback) {
	if value == nil {
		ts.SetException(nil)
		return
	}
	if typ != nil && value.ExcType == nil {
		value.ExcType = typ
	}
	if tb != nil {
		value.TB = tb
	}
	ts.SetException(value)
}

// Raise installs exc as the current exception. If a previous
// exception was current, it becomes exc.Context.
//
// CPython: Python/errors.c:L83 _PyErr_SetObject (chaining branch)
func Raise(ts *state.Thread, exc *Exception) {
	if exc == nil {
		ts.SetException(nil)
		return
	}
	// CPython _PyErr_SetObject reads _PyErr_GetTopmostException, which
	// walks tstate->exc_info: first the live exception, then the
	// currently-handled one if no exception is set. That second branch
	// is the one that turns `raise NewError` inside an except clause
	// into NewError.__context__ = caught_exception.
	prev := Occurred(ts)
	if prev == nil {
		prev = Handled(ts)
	}
	if prev != nil && prev != exc && exc.Context == nil {
		exc.Context = prev
	}
	ts.SetException(exc)
}

// RaiseFrom is `raise exc from cause`. It sets exc.Cause and
// suppresses context display.
//
// CPython: Python/errors.c:L1438 _PyErr_SetFromCause
func RaiseFrom(ts *state.Thread, exc, cause *Exception) {
	if exc != nil {
		exc.Cause = cause
		exc.Suppress = true
	}
	Raise(ts, exc)
}

// NormalizeException is a no-op in the modern API path; Set / Raise
// already produce a normalized Exception. Preserved for source-shape
// parity with CPython's legacy three-argument form.
//
// CPython: Python/errors.c:L501 _PyErr_NormalizeException
func NormalizeException(ts *state.Thread) {
	_ = ts
}

// AttachTraceback prepends entry to the current exception's
// traceback. The VM (v0.6+) calls this on every frame unwind; v0.3
// callers (Go-side) use it to record their own positions.
//
// CPython: Python/traceback.c:L154 PyTraceBack_Here
func AttachTraceback(ts *state.Thread, entry traceback.Entry) {
	exc := Occurred(ts)
	if exc == nil {
		return
	}
	exc.TB = traceback.Push(exc.TB, entry)
}
