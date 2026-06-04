package objects

import (
	"errors"
	"fmt"
)

// IsStopIteration reports whether err is a StopIteration: either the
// ErrStopIteration sentinel raised by built-in iterators or a Python
// exception (RaisedError) whose type is StopIteration or a subclass.
// Mirrors PyErr_ExceptionMatches(PyExc_StopIteration), which next(it,
// default) uses to decide when to swap in the default.
//
// CPython: Python/errors.c:259 PyErr_GivenExceptionMatches
func IsStopIteration(err error) bool {
	if errors.Is(err, ErrStopIteration) {
		return true
	}
	var re *RaisedError
	if errors.As(err, &re) && re.Exc != nil {
		for cur := re.Exc.Type(); cur != nil; cur = parentBase(cur) {
			if cur.Name == "StopIteration" {
				return true
			}
		}
	}
	return false
}

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

// KeyErrorHook builds a Go error that the vm unwind path promotes to a
// PyExc_KeyError carrying key as its single argument. CPython's
// dict_subscript raises KeyError(key) with the actual key object, so
// `except KeyError as e: e.args[0]` recovers the missing key (and a
// tuple key stays a tuple instead of being flattened to its repr).
// objects cannot import the errors package, so the vm layer installs
// this hook in its init.
//
// CPython: Objects/dictobject.c:2247 dict_subscript (set_key_error(key))
var KeyErrorHook func(key Object) error

// raiseKeyError returns the structured KeyError(key) when the vm hook is
// installed, falling back to the repr-based sentinel message otherwise
// (the hook is always present in a running interpreter; the fallback
// only matters for objects-only unit tests).
//
// CPython: Objects/dictobject.c:147 set_key_error
func raiseKeyError(key Object) error {
	if KeyErrorHook != nil {
		return KeyErrorHook(key)
	}
	repr, err := Repr(key)
	if err != nil {
		repr = "?"
	}
	return fmt.Errorf("KeyError: %s", repr)
}

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
