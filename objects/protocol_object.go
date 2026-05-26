// Object-protocol helpers: the PyObject_* free functions that route
// through the type slots. Most of these already exist as type-slot
// dispatchers (Repr, Str, Hash, RichCmp); this file adds the
// container-shaped ones (Length, GetItem, SetItem, DelItem,
// Contains) plus the iter pair so callers don't have to poke at the
// type slots directly.
//
// CPython: Objects/abstract.c

package objects

import (
	"errors"
	"fmt"
)

// Length returns len(o). Routes through Mapping.Length first, then
// Sequence.Length, mirroring CPython's PyObject_Size dispatch.
//
// CPython: Objects/abstract.c:55 PyObject_Size
func Length(o Object) (int, error) {
	if o == nil {
		return 0, fmt.Errorf("TypeError: object of type 'NoneType' has no len()")
	}
	tp := o.Type()
	if m := tp.Mapping; m != nil && m.Length != nil {
		return m.Length(o)
	}
	if s := tp.Sequence; s != nil && s.Length != nil {
		return s.Length(o)
	}
	return 0, fmt.Errorf("TypeError: object of type '%s' has no len()", tp.Name)
}

// GetItem returns o[key]. Mappings receive the key as-is; sequences
// require an integer key and route through Sequence.GetItem.
//
// CPython: Objects/abstract.c:154 PyObject_GetItem
func GetItem(o, key Object) (Object, error) {
	tp := o.Type()
	if m := tp.Mapping; m != nil && m.GetItem != nil {
		return m.GetItem(o, key)
	}
	if s := tp.Sequence; s != nil && s.GetItem != nil {
		i, err := indexAsInt(key)
		if err != nil {
			return nil, err
		}
		return s.GetItem(o, i)
	}
	return nil, fmt.Errorf("TypeError: '%s' object is not subscriptable", tp.Name)
}

// SetItem assigns o[key] = value. value==nil signals delete (DelItem
// forwards through here).
//
// CPython: Objects/abstract.c:200 PyObject_SetItem
func SetItem(o, key, value Object) error {
	tp := o.Type()
	if m := tp.Mapping; m != nil {
		if value == nil {
			if m.DelItem != nil {
				return m.DelItem(o, key)
			}
		} else if m.SetItem != nil {
			return m.SetItem(o, key, value)
		}
	}
	if s := tp.Sequence; s != nil && s.SetItem != nil && value != nil {
		i, err := indexAsInt(key)
		if err != nil {
			return err
		}
		return s.SetItem(o, i, value)
	}
	verb := "item assignment"
	if value == nil {
		verb = "item deletion"
	}
	return fmt.Errorf("TypeError: '%s' object does not support %s", tp.Name, verb)
}

// DelItem deletes o[key]. Forwards to SetItem with value==nil.
//
// CPython: Objects/abstract.c:227 PyObject_DelItem
func DelItem(o, key Object) error {
	return SetItem(o, key, nil)
}

// Contains reports whether v is in o. Routes through Sequence.Contains
// when present; otherwise falls back to a manual iterator walk.
//
// CPython: Objects/abstract.c:2113 PySequence_Contains
func Contains(o, v Object) (bool, error) {
	if s := o.Type().Sequence; s != nil && s.Contains != nil {
		return s.Contains(o, v)
	}
	it, err := Iter(o)
	if err != nil {
		return false, err
	}
	next := it.Type().IterNext
	if next == nil {
		return false, fmt.Errorf("TypeError: '%s' is not iterable", o.Type().Name)
	}
	for {
		x, err := next(it)
		if err != nil {
			if errors.Is(err, ErrStopIteration) {
				return false, nil
			}
			return false, err
		}
		eq, err := RichCmpBool(x, v, CompareEQ)
		if err != nil {
			return false, err
		}
		if eq {
			return true, nil
		}
	}
}

// Iter returns iter(o). Mirrors PyObject_GetIter.
//
// CPython: Objects/abstract.c:2786 PyObject_GetIter
func Iter(o Object) (Object, error) {
	tp := o.Type()
	if tp.Iter != nil {
		return tp.Iter(o)
	}
	// Fall back to __iter__ descriptor lookup. This handles Go-level types
	// that have __iter__ installed via SetTypeDescr (e.g. after typing.py
	// wires TypeVarTuple and TypeAliasType iteration).
	//
	// CPython: Objects/typeobject.c slot_tp_iter (analogous path)
	if descr, _ := LookupDescriptor(tp, "__iter__"); descr != nil {
		iterFn, err := bindDescriptor(descr, o)
		if err == nil && iterFn != nil {
			return CallObject(iterFn, nil)
		}
	}
	if s := tp.Sequence; s != nil && s.GetItem != nil {
		return NewSeqIter(o), nil
	}
	return nil, fmt.Errorf("TypeError: '%s' object is not iterable", tp.Name)
}

// IterNext advances o by one. Equivalent to next(o), and propagates
// ErrStopIteration when the iterator is exhausted.
//
// CPython: Objects/abstract.c:2842 PyIter_Next
func IterNext(o Object) (Object, error) {
	if next := o.Type().IterNext; next != nil {
		return next(o)
	}
	return nil, fmt.Errorf("TypeError: '%s' object is not an iterator", o.Type().Name)
}

// TypeOf returns o's type as an Object. Mirrors PyObject_Type; the
// result is the type singleton, not a fresh value.
//
// CPython: Objects/abstract.c:42 PyObject_Type
func TypeOf(o Object) Object {
	return o.Type()
}

// indexAsInt coerces a subscripting key to int the same way CPython's
// PyNumber_Index/PyNumber_AsSsize_t does for the small range of
// integer-like values gopy currently exposes.
//
// CPython: Objects/abstract.c:1356 PyNumber_AsSsize_t
func indexAsInt(o Object) (int, error) {
	if i, ok := o.(*Int); ok {
		v, fits := i.Int64()
		if !fits {
			return 0, fmt.Errorf("OverflowError: cannot fit integer into index")
		}
		return int(v), nil
	}
	if b, ok := o.(*Bool); ok {
		if b == True() {
			return 1, nil
		}
		return 0, nil
	}
	return 0, fmt.Errorf("TypeError: indices must be integers, not '%s'", o.Type().Name)
}
