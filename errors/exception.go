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
	return e
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
