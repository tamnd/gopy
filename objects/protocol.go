package objects

import (
	"fmt"
	"unsafe"
)

// reprDepth tracks objects currently being repr'd to detect cycles.
// CPython: Objects/object.c:L514 Py_ReprEnter / Py_ReprLeave
var reprDepth = make(map[uintptr]bool)

// objectID extracts the data pointer from a Go interface value, giving
// a stable per-object identity for cycle detection.
func objectID(o Object) uintptr {
	type iface struct{ _, data uintptr }
	return (*iface)(unsafe.Pointer(&o)).data
}

// ReprEnter reports whether o is already being repr'd (cycle). If not,
// it marks o as in-progress and returns false. The caller must call
// ReprLeave(o) when done.
//
// CPython: Objects/object.c:L514 Py_ReprEnter
func ReprEnter(o Object) bool {
	id := objectID(o)
	if reprDepth[id] {
		return true
	}
	reprDepth[id] = true
	return false
}

// ReprLeave removes o from the in-progress repr set.
//
// CPython: Objects/object.c:L533 Py_ReprLeave
func ReprLeave(o Object) {
	delete(reprDepth, objectID(o))
}

// cRecursionLimit bounds nested C-level slot calls (repr, str) the way
// CPython's c_stack_soft_limit bounds the native call stack. CPython 3.14
// measures the real machine stack pointer against a soft limit
// (_Py_MakeRecCheck), but gopy runs these slots on the Go stack where the
// pointer is not portably reachable, so we count nesting depth instead.
// This mirrors the pre-stack-pointer c_recursion_remaining design whose
// Py_C_RECURSION_LIMIT was 8000 on most platforms; deeply self-nested
// containers (repr of a list 150k deep) hit it and raise RecursionError
// rather than overflowing the goroutine stack.
//
// CPython: Include/internal/pycore_ceval.h:222 _Py_EnterRecursiveCallTstate
const cRecursionLimit = 8000

// cRecursionRemaining is the live budget. It is a package global to match
// the existing reprDepth cycle tracker: gopy's GIL-style cooperative model
// runs object slots one logical thread at a time, so a single counter is
// consistent with that design.
var cRecursionRemaining = cRecursionLimit

// enterRecursiveCall decrements the C-recursion budget and returns a
// RecursionError when it is exhausted. where names the operation for the
// message (" while getting the repr of an object"). The caller must pair a
// successful enter with leaveRecursiveCall, normally via defer.
//
// CPython: Include/internal/pycore_ceval.h:222 _Py_EnterRecursiveCallTstate
// CPython: Python/ceval.c:1012 _Py_CheckRecursiveCall
func enterRecursiveCall(where string) error {
	cRecursionRemaining--
	if cRecursionRemaining <= 0 {
		cRecursionRemaining++
		return fmt.Errorf("RecursionError: maximum recursion depth exceeded%s", where)
	}
	return nil
}

// leaveRecursiveCall returns one unit of C-recursion budget.
//
// CPython: Include/internal/pycore_ceval.h:233 _Py_LeaveRecursiveCallTstate
func leaveRecursiveCall() {
	cRecursionRemaining++
}

// Repr returns the Python repr of o. Falls back to a generic
// `<type at addr>` when the type lacks a Repr slot.
//
// CPython: Objects/object.c:L518 PyObject_Repr
func Repr(o Object) (string, error) {
	if o == nil {
		return "<nil>", nil
	}
	// PyObject_Repr guards tp_repr with the C-recursion check so a type
	// whose repr loops (or a deeply self-nested container) raises instead
	// of overflowing the native stack.
	//
	// CPython: Objects/object.c:777 PyObject_Repr
	if err := enterRecursiveCall(" while getting the repr of an object"); err != nil {
		return "", err
	}
	defer leaveRecursiveCall()
	if r := o.Type().Repr; r != nil {
		return r(o)
	}
	return fmt.Sprintf("<%s object at %p>", o.Type().Name, o), nil
}

// ReprObject returns the result of repr(o) as an Object, preserving
// str subclass types. Mirrors PyObject_Repr's return-value semantics.
//
// CPython: Objects/object.c:L518 PyObject_Repr
func ReprObject(o Object) (Object, error) {
	if o == nil {
		return NewStr("<nil>"), nil
	}
	// For user-defined types, call __repr__ directly and return the Object.
	if o.Type().IsUser {
		meth, err := lookupSpecial(o, "__repr__")
		if err != nil {
			return nil, err
		}
		if meth != nil {
			result, err := Call(meth, NewTuple(nil), nil)
			if err != nil {
				return nil, err
			}
			if !IsSubtype(result.Type(), strType) {
				return nil, fmt.Errorf("TypeError: __repr__ returned non-string (type %s)", result.Type().Name)
			}
			return result, nil
		}
	}
	s, err := Repr(o)
	if err != nil {
		return nil, err
	}
	return NewStr(s), nil
}

// StrObject returns the result of str(o) as an Object, preserving
// str subclass types. Mirrors PyObject_Str's return-value semantics.
//
// CPython: Objects/object.c:L463 PyObject_Str
func StrObject(o Object) (Object, error) {
	if o == nil {
		return NewStr("<nil>"), nil
	}
	// For user-defined types, call __str__ directly and return the Object.
	if o.Type().IsUser {
		meth, err := lookupSpecial(o, "__str__")
		if err != nil {
			return nil, err
		}
		if meth != nil {
			result, err := Call(meth, NewTuple(nil), nil)
			if err != nil {
				return nil, err
			}
			if !IsSubtype(result.Type(), strType) {
				return nil, fmt.Errorf("TypeError: __str__ returned non-string (type %s)", result.Type().Name)
			}
			return result, nil
		}
	}
	s, err := Str(o)
	if err != nil {
		return nil, err
	}
	return NewStr(s), nil
}

// Str returns the Python str of o. Falls back to Repr.
//
// CPython: Objects/object.c:L463 PyObject_Str
func Str(o Object) (string, error) {
	if o == nil {
		return "<nil>", nil
	}
	// PyObject_Str guards tp_str with the same C-recursion check as repr.
	//
	// CPython: Objects/object.c:822 PyObject_Str
	if err := enterRecursiveCall(" while getting the str of an object"); err != nil {
		return "", err
	}
	defer leaveRecursiveCall()
	if s := o.Type().Str; s != nil {
		return s(o)
	}
	return Repr(o)
}

// Format ports PyObject_Format. Built-in types (str, int, float, …) have
// a Go-level Format slot; for those, an empty spec short-circuits to
// str(o) and a non-empty spec dispatches to the slot. User-defined types
// have no Go-level slot; for those, __format__ is always called via
// attribute lookup — even for an empty spec — because CPython's
// PyObject_Format calls _PyObject_LookupSpecial for every type that is
// not PyUnicode_CheckExact or PyLong_CheckExact.
//
// When a user type explicitly defines __format__ (or inherits it from
// another user type in the MRO), that override must take priority over
// any inherited Go-level Format slot.  This mirrors CPython's
// _PyObject_LookupSpecial-first ordering in PyObject_Format.
//
// CPython: Objects/abstract.c:843 PyObject_Format
func Format(o Object, spec string) (string, error) {
	if o == nil {
		return "<nil>", nil
	}
	// User-defined __format__ overrides an inherited Go-level Format slot.
	//
	// CPython: Objects/abstract.c:876 _PyObject_LookupSpecial (always called first)
	if o.Type().IsUser && userTypeHasFormatOverride(o.Type()) {
		meth, err := lookupSpecial(o, "__format__")
		if err != nil {
			return "", err
		}
		if meth != nil {
			result, callErr := Call(meth, NewTuple([]Object{NewStr(spec)}), nil)
			if callErr != nil {
				return "", callErr
			}
			s, ok := result.(*Unicode)
			if !ok {
				return "", fmt.Errorf("TypeError: __format__ must return a str, not %s", result.Type().Name)
			}
			return s.v, nil
		}
	}
	if f := o.Type().Format; f != nil {
		if spec == "" {
			return Str(o)
		}
		return f(o, spec)
	}
	// No Go-level slot: always call the Python-level __format__ method,
	// even when spec is empty. object.__format__('') returns str(self),
	// so the semantics match CPython's fast path for exact types.
	//
	// CPython uses _PyObject_LookupSpecial which does a TYPE-level MRO
	// lookup (_PyType_Lookup) and then binds the descriptor to the
	// instance. Using GetAttr (instance-level) fails for types like
	// module whose Getattro slot ignores the type MRO.
	//
	// CPython: Objects/abstract.c:876 meth = _PyObject_LookupSpecial(obj, __format__)
	// CPython: Objects/typeobject.c:1776 _PyObject_LookupSpecial
	meth, err := lookupSpecial(o, "__format__")
	if err != nil {
		return "", err
	}
	if meth == nil {
		return "", fmt.Errorf("TypeError: Type %s doesn't define __format__", o.Type().Name)
	}
	result, callErr := Call(meth, NewTuple([]Object{NewStr(spec)}), nil)
	if callErr != nil {
		return "", callErr
	}
	s, ok := result.(*Unicode)
	if !ok {
		return "", fmt.Errorf("TypeError: __format__ must return a str, not %s", result.Type().Name)
	}
	return s.v, nil
}

// userTypeHasFormatOverride reports whether t or any user-defined ancestor
// in the MRO explicitly defines __format__.  The walk stops at the first
// built-in (non-user) type so that classes like class C(int) without an
// override correctly fall back to int's Go-level Format slot.
//
// CPython: Objects/typeobject.c:5063 type_getattro (tp_dict check per base)
func userTypeHasFormatOverride(t *Type) bool {
	for _, base := range t.MRO {
		if base == nil || !base.IsUser {
			return false
		}
		if descrs, ok := typeDescrTable[base]; ok {
			if _, ok := descrs["__format__"]; ok {
				return true
			}
		}
	}
	return false
}

// lookupSpecial mirrors CPython's _PyObject_LookupSpecial: it walks
// the TYPE's MRO for the named dunder method, then binds the descriptor
// to the instance via DescrGet. Unlike GetAttr / LookupAttrString this
// never goes through the instance's Getattro slot, so it works for
// types whose Getattro ignores the type MRO (e.g. modules).
//
// CPython: Objects/typeobject.c:1776 _PyObject_LookupSpecial
func lookupSpecial(o Object, name string) (Object, error) {
	descr, _ := LookupDescriptor(o.Type(), name)
	if descr == nil {
		return nil, nil
	}
	if dg := descr.Type().DescrGet; dg != nil {
		return dg(descr, o, o.Type())
	}
	return descr, nil
}

// Hash returns the hash of o. Errors with errUnhashable when the
// type has no Hash slot. Mirrors PyObject_Hash.
//
// CPython: Objects/object.c:L869 PyObject_Hash
func Hash(o Object) (int64, error) {
	if h := o.Type().Hash; h != nil {
		return h(o)
	}
	return 0, fmt.Errorf("TypeError: unhashable type: '%s'", o.Type().Name)
}

// RichCmp dispatches to tp_richcompare. Returns NotImplemented when
// neither operand handles the comparison. Mirrors PyObject_RichCompare.
//
// CPython: Objects/object.c:L737 PyObject_RichCompare
// CPython: Objects/object.c:L827 do_richcompare
func RichCmp(a, b Object, op CompareOp) (Object, error) {
	checkedReverse := false
	// When b's type is a strict subtype of a's type and overrides the
	// comparison slot, give b a chance first (with the swapped op).
	// This mirrors do_richcompare's subtype-first check so that a
	// subclass that blocks a comparison with None is respected before
	// the base class's slot runs.
	//
	// CPython: Objects/object.c:L840 do_richcompare (subtype check)
	if a.Type() != b.Type() && IsSubtype(b.Type(), a.Type()) {
		if r := b.Type().RichCmp; r != nil {
			checkedReverse = true
			out, err := r(b, a, reflectOp(op))
			if err != nil {
				return nil, err
			}
			if !IsNotImplemented(out) {
				return out, nil
			}
		}
	}
	if r := a.Type().RichCmp; r != nil {
		out, err := r(a, b, op)
		if err != nil {
			return nil, err
		}
		if !IsNotImplemented(out) {
			return out, nil
		}
	}
	if !checkedReverse {
		if r := b.Type().RichCmp; r != nil {
			out, err := r(b, a, reflectOp(op))
			if err != nil {
				return nil, err
			}
			if !IsNotImplemented(out) {
				return out, nil
			}
		}
	}
	switch op {
	case CompareEQ:
		return NewBool(a == b), nil
	case CompareNE:
		return NewBool(a != b), nil
	}
	// For ordering comparisons (LT, LE, GT, GE) where both sides return
	// NotImplemented, CPython raises TypeError. Returning NotImplemented
	// here would be truthy and silently mis-branch.
	//
	// CPython: Objects/object.c:876 do_richcompare (ordering fallthrough)
	opStr := map[CompareOp]string{
		CompareLT: "<",
		CompareLE: "<=",
		CompareGT: ">",
		CompareGE: ">=",
	}[op]
	return nil, fmt.Errorf("TypeError: '%s' not supported between instances of '%s' and '%s'",
		opStr, a.Type().Name, b.Type().Name)
}

// RichCmpBool runs RichCmp and converts the result to a Go bool.
// Mirrors PyObject_RichCompareBool.
//
// CPython: Objects/object.c:L795 PyObject_RichCompareBool
func RichCmpBool(a, b Object, op CompareOp) (bool, error) {
	if a == b {
		switch op {
		case CompareEQ:
			return true, nil
		case CompareNE:
			return false, nil
		}
	}
	r, err := RichCmp(a, b, op)
	if err != nil {
		return false, err
	}
	return IsTruthy(r)
}

// IsTruthy mirrors PyObject_IsTrue.
//
// CPython: Objects/object.c:L1671 PyObject_IsTrue
func IsTruthy(o Object) (bool, error) {
	if IsNone(o) {
		return false, nil
	}
	if o == trueSingleton {
		return true, nil
	}
	if o == falseSingleton {
		return false, nil
	}
	if n := o.Type().Number; n != nil && n.Bool != nil {
		return n.Bool(o)
	}
	if s := o.Type().Sequence; s != nil && s.Length != nil {
		l, err := s.Length(o)
		if err != nil {
			return false, err
		}
		return l != 0, nil
	}
	if m := o.Type().Mapping; m != nil && m.Length != nil {
		l, err := m.Length(o)
		if err != nil {
			return false, err
		}
		return l != 0, nil
	}
	return true, nil
}

// GetAttr fetches o.name. Routes through the type's Getattro slot.
// Returns AttributeError when the type exposes no attribute access.
//
// CPython: Objects/object.c:1290 PyObject_GetAttr
func GetAttr(o Object, name Object) (Object, error) {
	if name == nil || !IsSubtype(name.Type(), strType) {
		return nil, fmt.Errorf("TypeError: attribute name must be string, not '%s'", typeNameOf(name))
	}
	tp := o.Type()
	if tp.Getattro != nil {
		return tp.Getattro(o, name)
	}
	// CPython inherits tp_getattro from the base type; object's slot is
	// PyObject_GenericGetAttr. Built-in types that leave Getattro nil
	// behave the same here so e.g. None.__class__ reaches the getset
	// installed on object.
	//
	// CPython: Objects/typeobject.c inherit_slots (tp_getattro)
	return GenericGetAttr(o, name)
}

// SetAttr writes o.name = value. value==nil deletes the attribute
// (CPython routes PyObject_DelAttr through here too).
//
// CPython: Objects/object.c:1440 PyObject_SetAttr
func SetAttr(o Object, name Object, value Object) error {
	if name == nil || !IsSubtype(name.Type(), strType) {
		return fmt.Errorf("TypeError: attribute name must be string, not '%s'", typeNameOf(name))
	}
	tp := o.Type()
	if tp.Setattro != nil {
		return tp.Setattro(o, name, value)
	}
	verb := "assign to"
	if value == nil {
		verb = "del"
	}
	if tp.Getattro == nil {
		return fmt.Errorf("TypeError: '%s' object has no attributes (%s .%s)", tp.Name, verb, attrNameStr(name))
	}
	return fmt.Errorf("TypeError: '%s' object has only read-only attributes (%s .%s)", tp.Name, verb, attrNameStr(name))
}

// DelAttr deletes o.name. Forwards to SetAttr with value==nil to
// match CPython's single dispatch.
//
// CPython: Objects/object.c:1490 PyObject_DelAttr
func DelAttr(o Object, name Object) error {
	return SetAttr(o, name, nil)
}

// attrNameStr extracts the underlying Go string of an attribute-name
// Object. Falls back to the type name when extraction fails so error
// messages stay readable.
func attrNameStr(name Object) string {
	if s, ok := name.(*Unicode); ok {
		return s.v
	}
	return typeNameOf(name)
}

// typeNameOf returns the type name of o, or "<nil>" when o is nil.
// Used for protocol-level error formatting.
func typeNameOf(o Object) string {
	if o == nil {
		return "<nil>"
	}
	return o.Type().Name
}

// typeFullNameOf returns module.qualname for user-defined types (always
// includes the module, even for __main__), and just the type name for
// built-in types. Mirrors CPython's _PyType_GetFullyQualifiedName.
func typeFullNameOf(o Object) string {
	if o == nil {
		return "<nil>"
	}
	t := o.Type()
	if !t.IsUser {
		return t.Name
	}
	qual := unicodeOf(TypeGetQualName(t))
	if t.Module != "" {
		return t.Module + "." + qual
	}
	return qual
}

// reflectOp returns the operator that swaps the operands. < becomes
// >, <= becomes >=, == and != stay.
//
// CPython: Objects/object.c:L727 _Py_SwappedOp
func reflectOp(op CompareOp) CompareOp {
	switch op {
	case CompareLT:
		return CompareGT
	case CompareLE:
		return CompareGE
	case CompareGT:
		return CompareLT
	case CompareGE:
		return CompareLE
	}
	return op
}
