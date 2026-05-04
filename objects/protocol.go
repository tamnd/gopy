package objects

import "fmt"

// Repr returns the Python repr of o. Falls back to a generic
// `<type at addr>` when the type lacks a Repr slot.
//
// CPython: Objects/object.c:L518 PyObject_Repr
func Repr(o Object) (string, error) {
	if o == nil {
		return "<nil>", nil
	}
	if r := o.Type().Repr; r != nil {
		return r(o)
	}
	return fmt.Sprintf("<%s object at %p>", o.Type().Name, o), nil
}

// Str returns the Python str of o. Falls back to Repr.
//
// CPython: Objects/object.c:L463 PyObject_Str
func Str(o Object) (string, error) {
	if o == nil {
		return "<nil>", nil
	}
	if s := o.Type().Str; s != nil {
		return s(o)
	}
	return Repr(o)
}

// Hash returns the hash of o. Errors with errUnhashable when the
// type has no Hash slot. Mirrors PyObject_Hash.
//
// CPython: Objects/object.c:L869 PyObject_Hash
func Hash(o Object) (int64, error) {
	if h := o.Type().Hash; h != nil {
		return h(o)
	}
	return 0, errUnhashable
}

// RichCmp dispatches to tp_richcompare. Returns NotImplemented when
// neither operand handles the comparison. Mirrors PyObject_RichCompare.
//
// CPython: Objects/object.c:L737 PyObject_RichCompare
func RichCmp(a, b Object, op CompareOp) (Object, error) {
	if r := a.Type().RichCmp; r != nil {
		out, err := r(a, b, op)
		if err != nil {
			return nil, err
		}
		if !IsNotImplemented(out) {
			return out, nil
		}
	}
	if r := b.Type().RichCmp; r != nil {
		out, err := r(b, a, reflectOp(op))
		if err != nil {
			return nil, err
		}
		if !IsNotImplemented(out) {
			return out, nil
		}
	}
	switch op {
	case CompareEQ:
		return NewBool(a == b), nil
	case CompareNE:
		return NewBool(a != b), nil
	}
	return notImplemented(), nil
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
