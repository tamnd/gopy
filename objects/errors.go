package objects

import "errors"

// v0.2 has no Python-level exception machinery (that lands in v0.3).
// Until then, the runtime uses sentinel Go errors that the abstract
// layer maps to placeholder strings; v0.3 rewires these to real
// PyExc_* singletons.

// ErrStopIteration signals end of iteration. Mirrors PyExc_StopIteration.
//
// CPython: Objects/exceptions.c:L1937 PyExc_StopIteration
var ErrStopIteration = errors.New("StopIteration")

// ErrIndexOutOfRange signals out-of-range sequence access. Mirrors
// PyExc_IndexError. The exported alias lets packages outside objects/
// check for this sentinel via errors.Is without importing the full
// error hierarchy.
//
// CPython: Objects/exceptions.c:L2229 PyExc_IndexError
var ErrIndexOutOfRange = errors.New("IndexError: index out of range")

// errIndexOutOfRange is an alias kept for internal use.
var errIndexOutOfRange = ErrIndexOutOfRange

// errKeyNotFound signals a missing dict key. Mirrors PyExc_KeyError.
// The message carries the "KeyError:" prefix so the vm unwind path
// can promote it to a real PyExc_KeyError instance instead of a bare
// Exception. isKeyError still recognizes the sentinel via errors.Is.
//
// CPython: Objects/exceptions.c:L2261 PyExc_KeyError
var errKeyNotFound = errors.New("KeyError: key not found")

// ExceptionInstance is the structural interface a Python exception
// object satisfies so packages outside errors/ can read its type and
// args without importing errors and creating layering cycles. The
// canonical implementation is *errors.Exception.
//
// CPython: Objects/exceptions.c:L34 BaseExceptionObject (the fields
// PyExc_*Object exposes through tp_getset)
type ExceptionInstance interface {
	Object
	ExceptionType() *Type
	ExceptionArgs() *Tuple
}
