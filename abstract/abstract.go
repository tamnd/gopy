// Package abstract ports the gating subset of cpython/Objects/abstract.c.
// It dispatches the object protocol (length, get/set item, numeric
// operations, iteration) through the type slot table in objects.
package abstract

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// ErrTypeError is the v0.2 placeholder for PyExc_TypeError. v0.3 wires
// up the real exception machinery.
//
// CPython: Objects/exceptions.c:L2196 PyExc_TypeError
var ErrTypeError = errors.New("TypeError")

// Length returns the length of o. Tries the sequence slot first,
// then the mapping slot, matching PyObject_Length.
//
// CPython: Objects/abstract.c:L48 PyObject_Length
func Length(o objects.Object) (int, error) {
	if s := o.Type().Sequence; s != nil && s.Length != nil {
		return s.Length(o)
	}
	if m := o.Type().Mapping; m != nil && m.Length != nil {
		return m.Length(o)
	}
	return 0, fmt.Errorf("%w: object of type %q has no len()", ErrTypeError, o.Type().Name)
}

// GetItem fetches o[key]. For sequence types, key must be an int.
// Mapping types accept any hashable key.
//
// CPython: Objects/abstract.c:L138 PyObject_GetItem
func GetItem(o, key objects.Object) (objects.Object, error) {
	if m := o.Type().Mapping; m != nil && m.GetItem != nil {
		return m.GetItem(o, key)
	}
	if s := o.Type().Sequence; s != nil && s.GetItem != nil {
		i, ok := key.(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("%w: sequence index must be integer, not %q", ErrTypeError, key.Type().Name)
		}
		v, hasInt := i.Int64()
		if !hasInt {
			return nil, fmt.Errorf("%w: sequence index out of range", ErrTypeError)
		}
		return s.GetItem(o, int(v))
	}
	return nil, fmt.Errorf("%w: %q is not subscriptable", ErrTypeError, o.Type().Name)
}

// SetItem assigns o[key] = value.
//
// CPython: Objects/abstract.c:L194 PyObject_SetItem
func SetItem(o, key, value objects.Object) error {
	if m := o.Type().Mapping; m != nil && m.SetItem != nil {
		return m.SetItem(o, key, value)
	}
	if s := o.Type().Sequence; s != nil && s.SetItem != nil {
		i, ok := key.(*objects.Int)
		if !ok {
			return fmt.Errorf("%w: sequence index must be integer", ErrTypeError)
		}
		v, hasInt := i.Int64()
		if !hasInt {
			return fmt.Errorf("%w: sequence index out of range", ErrTypeError)
		}
		return s.SetItem(o, int(v), value)
	}
	return fmt.Errorf("%w: %q does not support item assignment", ErrTypeError, o.Type().Name)
}

// Add dispatches a + b through the numeric slots. Falls back to b's
// reflected slot when a returns NotImplemented.
//
// CPython: Objects/abstract.c:L971 PyNumber_Add
func Add(a, b objects.Object) (objects.Object, error) {
	return binaryOp(a, b, "+", func(n *objects.NumberMethods) func(a, b objects.Object) (objects.Object, error) {
		return n.Add
	})
}

// Subtract dispatches a - b.
//
// CPython: Objects/abstract.c:L1013 PyNumber_Subtract
func Subtract(a, b objects.Object) (objects.Object, error) {
	return binaryOp(a, b, "-", func(n *objects.NumberMethods) func(a, b objects.Object) (objects.Object, error) {
		return n.Subtract
	})
}

// Multiply dispatches a * b.
//
// CPython: Objects/abstract.c:L1021 PyNumber_Multiply
func Multiply(a, b objects.Object) (objects.Object, error) {
	return binaryOp(a, b, "*", func(n *objects.NumberMethods) func(a, b objects.Object) (objects.Object, error) {
		return n.Multiply
	})
}

// IterNext advances an iterator. Returns ErrStopIteration when the
// iterator is exhausted, mirroring PyIter_Next.
//
// CPython: Objects/abstract.c:L2895 PyIter_Next
func IterNext(it objects.Object) (objects.Object, error) {
	if it.Type().IterNext == nil {
		return nil, fmt.Errorf("%w: %q is not an iterator", ErrTypeError, it.Type().Name)
	}
	return it.Type().IterNext(it)
}

// Iter returns an iterator for o.
//
// CPython: Objects/abstract.c:L2873 PyObject_GetIter
func Iter(o objects.Object) (objects.Object, error) {
	if o.Type().Iter == nil {
		return nil, fmt.Errorf("%w: %q is not iterable", ErrTypeError, o.Type().Name)
	}
	return o.Type().Iter(o)
}

func binaryOp(a, b objects.Object, sym string,
	pick func(*objects.NumberMethods) func(a, b objects.Object) (objects.Object, error),
) (objects.Object, error) {
	if n := a.Type().Number; n != nil {
		if op := pick(n); op != nil {
			out, err := op(a, b)
			if err != nil {
				return nil, err
			}
			if !objects.IsNotImplemented(out) {
				return out, nil
			}
		}
	}
	if n := b.Type().Number; n != nil {
		if op := pick(n); op != nil {
			out, err := op(b, a)
			if err != nil {
				return nil, err
			}
			if !objects.IsNotImplemented(out) {
				return out, nil
			}
		}
	}
	return nil, fmt.Errorf("%w: unsupported operand type(s) for %s: %q and %q",
		ErrTypeError, sym, a.Type().Name, b.Type().Name)
}
