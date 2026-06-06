// Package errors ports cpython/Python/errors.c and the gating subset
// of cpython/Objects/exceptions.c. v0.3 ships the BaseException class
// hierarchy needed by v0.1 and v0.2, plus the Set/SetString/Format/
// Occurred/Clear/Fetch/Restore/Raise/RaiseFrom/Print API.
package errors

import (
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/traceback"
)

// Exception is the runtime representation of a raised Python
// exception. Mirrors PyBaseExceptionObject.
//
// CPython: Objects/exceptions.c:L34 BaseExceptionObject
type Exception struct {
	objects.Header
	ExcType  *objects.Type
	Args     *objects.Tuple
	Cause    *Exception
	Context  *Exception
	Suppress bool
	Notes    *objects.List
	TB       *traceback.Traceback
	attrs    *objects.Dict

	// StopValue stores StopIteration's separate value slot per
	// PyStopIterationObject. CPython keeps args and value independent:
	// `e = StopIteration("spam"); e.value = "eggs"` leaves args[0] as
	// "spam" but flips value to "eggs", so str(e) still prints "spam".
	// Populated by StopIteration_init from args[0] or None; only
	// meaningful when ExcType is StopIteration or a subclass.
	//
	// CPython: Objects/exceptions.c:746 PyStopIterationObject
	// CPython: Objects/exceptions.c:754 StopIteration_init
	StopValue objects.Object

	// SyntaxErr carries the SyntaxError-specific PyMemberDef payload
	// (msg, filename, lineno, offset, text, end_lineno, end_offset,
	// print_file_and_line, _metadata). Non-nil only when ExcType is a
	// SyntaxError subclass. Populated by SyntaxError_init through the
	// type's tp_call dispatch.
	//
	// CPython: Objects/exceptions.c:2670 PySyntaxErrorObject
	SyntaxErr *SyntaxErrorState

	// OSErr carries the OSError-specific PyMemberDef payload (myerrno,
	// strerror, filename, filename2, characters_written). Non-nil only
	// when ExcType is an OSError subclass. Populated by OSError_init
	// through the type's tp_call / tp_new dispatch.
	//
	// CPython: Objects/exceptions.c:1940 PyOSErrorObject
	OSErr *OSErrorState
}

// AttrDict implements objects.AttrDictHolder so a Python subclass of a
// built-in exception (e.g. `class MyExc(Exception)`) can store
// per-instance attributes set in __init__ through GenericSetAttr.
//
// CPython: Objects/exceptions.c:L34 BaseExceptionObject (dict member at
// offsetof(PyBaseExceptionObject, dict))
func (e *Exception) AttrDict() *objects.Dict { return e.attrs }

// EnsureAttrDict allocates the per-instance attribute dict on first
// write. Matches CPython's lazy materialization of tp_dictoffset.
//
// CPython: Objects/object.c:_PyObject_GetDictPtr (lazy dict alloc)
func (e *Exception) EnsureAttrDict() *objects.Dict {
	if e.attrs == nil {
		e.attrs = objects.NewDict()
	}
	return e.attrs
}

// IsException implements the state.Exception marker.
func (e *Exception) IsException() {}

// ExceptionType implements objects.ExceptionInstance: returns the
// exception's class. Used by packages that can't import errors/ but
// need to read the exception's metadata, such as the vm's
// CLEANUP_THROW handler.
//
// CPython: Objects/exceptions.c:L34 BaseExceptionObject (Py_TYPE
// access on tp_getset)
func (e *Exception) ExceptionType() *objects.Type {
	return e.ExcType
}

// ExceptionArgs implements objects.ExceptionInstance: returns the
// args tuple, never nil.
//
// CPython: Objects/exceptions.c:L184 BaseException_args_getter
func (e *Exception) ExceptionArgs() *objects.Tuple {
	if e.Args == nil {
		return objects.NewTuple(nil)
	}
	return e.Args
}

// New constructs an exception with the given type and args. Mirrors
// BaseException_new + BaseException_init.
//
// CPython: Objects/exceptions.c:L42 BaseException_new
func New(t *objects.Type, args *objects.Tuple) *Exception {
	if args == nil {
		args = objects.NewTuple(nil)
	}
	e := &Exception{ExcType: t, Args: args}
	if t != nil {
		e.Init(t)
	}
	// StopIteration_init seeds value from args[0] or None.
	//
	// CPython: Objects/exceptions.c:754 StopIteration_init
	if t != nil && isStopIterationType(t) {
		if args.Len() > 0 {
			e.StopValue = args.Item(0)
		} else {
			e.StopValue = objects.None()
		}
	}
	// Track for cycle GC so the collector can walk Exception→TB→Frame chains
	// and identify unreachable exception objects (e.g. from PUSH_EXC_INFO bugs).
	//
	// CPython: Objects/exceptions.c BaseException_new PyObject_GC_Track
	if h := objects.GCTrackHook; h != nil {
		h(e)
	}
	return e
}

// isStopIterationType reports whether t is StopIteration or a subclass
// of it, mirroring the C inheritance check in StopIteration_init.
func isStopIterationType(t *objects.Type) bool {
	for cur := t; cur != nil; cur = parentType(cur) {
		if cur.Name == "StopIteration" {
			return true
		}
	}
	return false
}

// parentType returns the first base of t (its primary parent), or nil
// when t has no bases. Used by isStopIterationType to walk the MRO.
func parentType(t *objects.Type) *objects.Type {
	if len(t.Bases) == 0 {
		return nil
	}
	return t.Bases[0]
}

// Message returns the exception's args[0] as a string when args has
// one item, matching CPython's BaseException.__str__ for single-arg
// exceptions.
//
// CPython: Objects/exceptions.c:L226 BaseException_str
func (e *Exception) Message() string {
	if e.Args == nil || e.Args.Len() == 0 {
		return ""
	}
	if e.Args.Len() == 1 {
		s, err := objects.Str(e.Args.Item(0))
		if err != nil {
			return ""
		}
		return s
	}
	s, err := objects.Repr(e.Args)
	if err != nil {
		return ""
	}
	return s
}

// TypeName returns the class name. Mirrors `type(exc).__name__`.
//
// CPython: Objects/exceptions.c:L226 BaseException_str
func (e *Exception) TypeName() string {
	if e.ExcType == nil {
		return "Exception"
	}
	return e.ExcType.Name
}
