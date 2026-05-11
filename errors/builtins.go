package errors

import "github.com/tamnd/gopy/objects"

// The exception class hierarchy ported in v0.3. Each Type entry has
// the same MRO shape as CPython. The class objects are created lazily
// at package init so the var-init dependency graph stays acyclic.

// Built-in exception type singletons. Names match CPython.
//
// CPython: Objects/exceptions.c:L1937 PyExc_BaseException and friends
var (
	PyExc_BaseException       = newExcType("BaseException", []*objects.Type{objects.ObjectType()})
	PyExc_Exception           = newExcType("Exception", []*objects.Type{PyExc_BaseException})
	PyExc_LookupError         = newExcType("LookupError", []*objects.Type{PyExc_Exception})
	PyExc_ArithmeticError     = newExcType("ArithmeticError", []*objects.Type{PyExc_Exception})
	PyExc_RuntimeError        = newExcType("RuntimeError", []*objects.Type{PyExc_Exception})
	PyExc_KeyError            = newExcType("KeyError", []*objects.Type{PyExc_LookupError})
	PyExc_IndexError          = newExcType("IndexError", []*objects.Type{PyExc_LookupError})
	PyExc_OverflowError       = newExcType("OverflowError", []*objects.Type{PyExc_ArithmeticError})
	PyExc_ZeroDivisionError   = newExcType("ZeroDivisionError", []*objects.Type{PyExc_ArithmeticError})
	PyExc_FloatingPointError  = newExcType("FloatingPointError", []*objects.Type{PyExc_ArithmeticError})
	PyExc_NotImplementedError = newExcType("NotImplementedError", []*objects.Type{PyExc_RuntimeError})
	PyExc_AttributeError      = newExcType("AttributeError", []*objects.Type{PyExc_Exception})
	PyExc_NameError           = newExcType("NameError", []*objects.Type{PyExc_Exception})
	PyExc_TypeError           = newExcType("TypeError", []*objects.Type{PyExc_Exception})
	PyExc_ValueError          = newExcType("ValueError", []*objects.Type{PyExc_Exception})
	PyExc_StopIteration       = newExcType("StopIteration", []*objects.Type{PyExc_Exception})
	PyExc_StopAsyncIteration  = newExcType("StopAsyncIteration", []*objects.Type{PyExc_Exception})
	PyExc_SystemExit          = newExcType("SystemExit", []*objects.Type{PyExc_BaseException})
	PyExc_KeyboardInterrupt   = newExcType("KeyboardInterrupt", []*objects.Type{PyExc_BaseException})
	PyExc_ImportError         = newExcType("ImportError", []*objects.Type{PyExc_Exception})
	PyExc_ModuleNotFoundError = newExcType("ModuleNotFoundError", []*objects.Type{PyExc_ImportError})

	// Additional exceptions covered by CPython's hierarchy, ported so
	// stdlib code paths that reference them by name resolve at import.
	//
	// CPython: Objects/exceptions.c:L2200 the remainder of the family
	PyExc_AssertionError    = newExcType("AssertionError", []*objects.Type{PyExc_Exception})
	PyExc_MemoryError       = newExcType("MemoryError", []*objects.Type{PyExc_Exception})
	PyExc_EOFError          = newExcType("EOFError", []*objects.Type{PyExc_Exception})
	PyExc_BufferError       = newExcType("BufferError", []*objects.Type{PyExc_Exception})
	PyExc_RecursionError    = newExcType("RecursionError", []*objects.Type{PyExc_RuntimeError})
	PyExc_UnboundLocalError = newExcType("UnboundLocalError", []*objects.Type{PyExc_NameError})
	PyExc_ReferenceError    = newExcType("ReferenceError", []*objects.Type{PyExc_Exception})
	PyExc_SystemError       = newExcType("SystemError", []*objects.Type{PyExc_Exception})
	PyExc_GeneratorExit     = newExcType("GeneratorExit", []*objects.Type{PyExc_BaseException})
)

func newExcType(name string, bases []*objects.Type) *objects.Type {
	t := objects.NewType(name, bases)
	t.Call = excCall
	t.Str = excStr
	t.Repr = excRepr
	return t
}

// excStr ports BaseException_str: empty for no args, str(args[0]) for
// a single arg, repr(args) otherwise.
//
// CPython: Objects/exceptions.c:171 BaseException_str
func excStr(o objects.Object) (string, error) {
	e, ok := o.(*Exception)
	if !ok || e.Args == nil {
		return "", nil
	}
	switch e.Args.Len() {
	case 0:
		return "", nil
	case 1:
		return objects.Str(e.Args.Item(0))
	default:
		return objects.Repr(e.Args)
	}
}

// excRepr ports BaseException_repr: "TypeName(arg)" for a single arg,
// "TypeName(args)" otherwise.
//
// CPython: Objects/exceptions.c:193 BaseException_repr
func excRepr(o objects.Object) (string, error) {
	e, ok := o.(*Exception)
	if !ok {
		return "", nil
	}
	name := e.TypeName()
	if e.Args == nil || e.Args.Len() == 0 {
		return name + "()", nil
	}
	if e.Args.Len() == 1 {
		s, err := objects.Repr(e.Args.Item(0))
		if err != nil {
			return "", err
		}
		return name + "(" + s + ")", nil
	}
	s, err := objects.Repr(e.Args)
	if err != nil {
		return "", err
	}
	return name + s, nil
}

// excCall is the tp_call slot for every built-in exception type. It
// mirrors BaseException_new + BaseException_init: store positional args
// on .args, ignore keyword arguments (CPython's BaseException_init also
// rejects them, but tolerating them here keeps stdlib call sites that
// pass `name=` / `path=` from blowing up before the proper ImportError
// init lands).
//
// CPython: Objects/exceptions.c:L42 BaseException_new
// CPython: Objects/exceptions.c:L84 BaseException_init
func excCall(callable objects.Object, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	cls, _ := callable.(*objects.Type)
	return New(cls, objects.NewTuple(args)), nil
}

// IsSubtype reports whether sub inherits from super, walking the MRO.
//
// CPython: Objects/typeobject.c:L2556 PyType_IsSubtype
func IsSubtype(sub, super *objects.Type) bool {
	return objects.IsSubtype(sub, super)
}

// Match reports whether exc's type inherits from t. Mirrors
// PyErr_GivenExceptionMatches.
//
// CPython: Python/errors.c:L327 PyErr_GivenExceptionMatches
func Match(exc *Exception, t *objects.Type) bool {
	if exc == nil || exc.ExcType == nil {
		return false
	}
	return IsSubtype(exc.ExcType, t)
}
