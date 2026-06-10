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
	"strings"
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
	// Type objects: __class_getitem__ (PEP 560 / PEP 695 generic classes).
	// This mirrors the PyType_Check branch in PyObject_GetItem.
	//
	// CPython: Objects/abstract.c:181 PyObject_GetItem
	if cls, ok := o.(*Type); ok {
		if descr, _ := LookupDescriptor(cls, "__class_getitem__"); descr != nil {
			dt := descr.Type()
			bound := descr
			if dt.DescrGet != nil {
				v, err := dt.DescrGet(descr, cls, cls)
				if err != nil {
					return nil, err
				}
				bound = v
			}
			return Call(bound, NewTuple([]Object{key}), nil)
		}
		return NewGenericAlias(o, key), nil
	}
	// User-defined objects: fall back to __getitem__ descriptor lookup.
	// This covers typing._SpecialForm (Unpack.__getitem__) and any other
	// Python-level __getitem__ that is not wired as a Go Mapping slot.
	//
	// CPython: Objects/abstract.c:188 PyObject_GetItem (fallback path)
	if descr, _ := LookupDescriptor(tp, "__getitem__"); descr != nil {
		fn, err := bindDescriptor(descr, o)
		if err == nil && fn != nil {
			return Call(fn, NewTuple([]Object{key}), nil)
		}
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
		// CPython: Objects/abstract.c:2113 _PySequence_IterSearch
		// PyObject_RichCompareBool(obj, item, Py_EQ) — needle (v) is left.
		eq, err := RichCmpBool(v, x, CompareEQ)
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
	// Python-level __getitem__ without __iter__: create a SeqIter that will
	// call __getitem__(0), __getitem__(1), ... until IndexError.
	//
	// CPython: Objects/abstract.c:2830 PyObject_GetIter — sq_item fallback
	if descr, _ := LookupDescriptor(tp, "__getitem__"); descr != nil {
		return NewSeqIter(o), nil
	}
	return nil, fmt.Errorf("TypeError: '%s' object is not iterable", tp.Name)
}

// Iterable reports whether o can be iterated without actually starting
// iteration: it has a tp_iter slot (or an __iter__ descriptor) or it
// satisfies the sequence protocol (sq_item / __getitem__). This mirrors
// the (tp_iter != NULL || PySequence_Check(o)) probe CPython runs in
// check_args_iterable and the LIST_EXTEND opcode before reformatting an
// "argument after *" error, so an object whose __iter__ itself raises
// still reports as iterable and lets the real error propagate.
//
// CPython: Python/ceval.c check_args_iterable (tp_iter / PySequence_Check)
func Iterable(o Object) bool {
	tp := o.Type()
	if tp.Iter != nil {
		return true
	}
	if descr, _ := LookupDescriptor(tp, "__iter__"); descr != nil {
		return true
	}
	if s := tp.Sequence; s != nil && s.GetItem != nil {
		return true
	}
	if descr, _ := LookupDescriptor(tp, "__getitem__"); descr != nil {
		return true
	}
	return false
}

// LengthHint mirrors PyObject_LengthHint. Returns __len__'s result when
// available, then __length_hint__, else default. A non-TypeError out of
// either dunder propagates to the caller, matching CPython's behavior.
//
// CPython: Objects/abstract.c:64 PyObject_LengthHint
func LengthHint(o Object, defaultVal int64) (int64, error) {
	if n, err := Length(o); err == nil {
		return int64(n), nil
	} else if !strings.HasPrefix(err.Error(), "TypeError") {
		return 0, err
	}
	hint, hintErr := GetAttr(o, NewStr("__length_hint__"))
	if hintErr != nil {
		return defaultVal, nil //nolint:nilerr // CPython: missing __length_hint__ → default
	}
	res, callErr := Call(hint, NewTuple(nil), nil)
	if callErr != nil {
		if strings.HasPrefix(callErr.Error(), "TypeError") {
			return defaultVal, nil
		}
		return 0, callErr
	}
	if IsNotImplemented(res) {
		return defaultVal, nil
	}
	n, ok := res.(*Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: __length_hint__ must be integer, not %s", res.Type().Name)
	}
	v, _ := n.Int64()
	if v < 0 {
		return 0, fmt.Errorf("ValueError: __length_hint__() should return >= 0")
	}
	return v, nil
}

// IterNext advances o by one. Equivalent to next(o), and propagates
// ErrStopIteration when the iterator is exhausted.
//
// Mirrors PyIter_Next: if the tp_iternext call sets a Python-level
// StopIteration, that exception is cleared and ErrStopIteration is
// returned so Go-level iteration loops can use errors.Is uniformly.
//
// CPython: Objects/abstract.c:2852 PyIter_Next
func IterNext(o Object) (Object, error) {
	if next := o.Type().IterNext; next != nil {
		v, err := next(o)
		if err != nil {
			if errors.Is(err, ErrStopIteration) {
				// CPython: Objects/abstract.c:2856 PyIter_Next clears the
				// pending exception only when it matches StopIteration, so
				// the caller sees only the NULL return. The guarded hook
				// leaves an unrelated pending exception (e.g. one being
				// raised while its repr walks this iterator) intact.
				if ClearCurrentExceptionIfIterStopHook != nil {
					ClearCurrentExceptionIfIterStopHook()
				}
				return nil, ErrStopIteration
			}
			if IsStopIterationHook != nil && IsStopIterationHook(err) {
				if ClearCurrentExceptionIfIterStopHook != nil {
					ClearCurrentExceptionIfIterStopHook()
				}
				return nil, ErrStopIteration
			}
		}
		return v, err
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
// PyNumber_Index/PyNumber_AsSsize_t does. Calls __index__ on non-int
// objects so a class with only __index__ defined is accepted as a
// sequence subscript or repeat count.
//
// CPython: Objects/abstract.c:1486 PyNumber_AsSsize_t
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
	idx, err := NumberIndex(o)
	if err != nil {
		return 0, fmt.Errorf("TypeError: indices must be integers, not '%s'", o.Type().Name)
	}
	i, ok := idx.(*Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: __index__ returned non-int (type %s)", idx.Type().Name)
	}
	v, fits := i.Int64()
	if !fits {
		return 0, fmt.Errorf("OverflowError: cannot fit integer into index")
	}
	return int(v), nil
}
