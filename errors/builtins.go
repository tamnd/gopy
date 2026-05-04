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
	PyExc_NotImplementedError = newExcType("NotImplementedError", []*objects.Type{PyExc_RuntimeError})
	PyExc_AttributeError      = newExcType("AttributeError", []*objects.Type{PyExc_Exception})
	PyExc_NameError           = newExcType("NameError", []*objects.Type{PyExc_Exception})
	PyExc_TypeError           = newExcType("TypeError", []*objects.Type{PyExc_Exception})
	PyExc_ValueError          = newExcType("ValueError", []*objects.Type{PyExc_Exception})
	PyExc_StopIteration       = newExcType("StopIteration", []*objects.Type{PyExc_Exception})
)

func newExcType(name string, bases []*objects.Type) *objects.Type {
	return objects.NewType(name, bases)
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
