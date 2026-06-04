// Port of the iteration panel from Python/bltinmodule.c: len, iter,
// next, reversed, enumerate, zip, range. Each closure mirrors the
// CPython builtin_* implementation; the registration step lands in
// init.go.
//
// CPython: Python/bltinmodule.c builtin_len / builtin_iter /
// builtin_next / builtin_reversed and the dedicated builders for
// enumerate, zip, range.

package builtins

import (
	"fmt"
	"math/big"

	"github.com/tamnd/gopy/objects"
)

// Len ports builtin_len: routes through PyObject_Size, which prefers
// Sequence.Length and falls back to Mapping.Length.
//
// CPython: Python/bltinmodule.c:1908 builtin_len
func Len(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: len() takes exactly one argument (%d given)", len(args))
	}
	n, err := objectSize(args[0])
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(n)), nil
}

// objectSize is the gopy port of PyObject_Size: try sq_length, then
// mp_length, otherwise TypeError.
//
// CPython: Objects/abstract.c:60 PyObject_Size
func objectSize(o objects.Object) (int, error) {
	t := o.Type()
	if t.Sequence != nil && t.Sequence.Length != nil {
		return t.Sequence.Length(o)
	}
	if t.Mapping != nil && t.Mapping.Length != nil {
		return t.Mapping.Length(o)
	}
	return 0, fmt.Errorf("TypeError: object of type '%s' has no len()", t.Name)
}

// Iter ports builtin_iter. The single-arg form returns
// PyObject_GetIter(v); the two-arg form returns a callable iterator
// that calls v() until the result equals sentinel.
//
// CPython: Python/bltinmodule.c:1809 builtin_iter
func Iter(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	switch len(args) {
	case 1:
		return getIter(args[0])
	case 2:
		v := args[0]
		if v.Type().Call == nil && v.Type().Vectorcall == nil {
			return nil, fmt.Errorf("TypeError: iter(v, w): v must be callable")
		}
		return newCallIter(v, args[1]), nil
	default:
		return nil, fmt.Errorf("TypeError: iter expected 1 or 2 arguments, got %d", len(args))
	}
}

// getIter is the gopy port of PyObject_GetIter: drive tp_iter, fall
// back to a sequence-getitem iterator (CPython's PySeqIter), or
// return TypeError.
//
// CPython: Objects/abstract.c:2832 PyObject_GetIter
func getIter(o objects.Object) (objects.Object, error) {
	t := o.Type()
	if t.Iter != nil {
		it, err := t.Iter(o)
		if err != nil || it == nil {
			return it, err
		}
		// CPython: Objects/abstract.c:2832 PyObject_GetIter — check
		// that __iter__ returned an actual iterator (has tp_iternext).
		if it.Type().IterNext == nil {
			return nil, fmt.Errorf("TypeError: iter() returned non-iterator of type '%s'", it.Type().Name)
		}
		return it, nil
	}
	// CPython: Objects/abstract.c:2826 PyObject_GetIter — when __iter__
	// is explicitly set to None the type opts out; do not fall back to
	// sequence iteration even if __getitem__ is present.
	if d, _ := objects.LookupDescriptor(t, "__iter__"); d == objects.None() {
		return nil, fmt.Errorf("TypeError: '%s' object is not iterable", t.Name)
	}
	if t.Sequence != nil && t.Sequence.GetItem != nil {
		return newSeqIter(o), nil
	}
	return nil, fmt.Errorf("TypeError: '%s' object is not iterable", t.Name)
}

// Next ports builtin_next. The default-value branch turns
// StopIteration into the default; otherwise StopIteration propagates.
//
// CPython: Python/bltinmodule.c:1672 builtin_next
func Next(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: next expected 1 or 2 arguments, got %d", len(args))
	}
	it := args[0]
	t := it.Type()
	if t.IterNext == nil {
		return nil, fmt.Errorf("TypeError: '%s' object is not an iterator", t.Name)
	}
	v, err := t.IterNext(it)
	if err == nil {
		return v, nil
	}
	// next(it, default) swaps in the default for any StopIteration,
	// whether the built-in sentinel or a Python `raise StopIteration`
	// from a user __next__. Match the exception type, not just the
	// sentinel, mirroring builtin_next's PyErr_ExceptionMatches.
	//
	// CPython: Python/bltinmodule.c:1620 builtin_next
	if objects.IsStopIteration(err) {
		if len(args) == 2 {
			return args[1], nil
		}
		return nil, err
	}
	return nil, err
}

// Reversed ports builtin_reversed. Looks for __reversed__ on the type
// first (range, dict_keys, deque all provide their own reverse
// iterators) and falls back to the sequence protocol (length + getitem)
// when the type has no __reversed__.
//
// CPython: Objects/enumobject.c:1086 reversed_new_impl
func Reversed(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	// reversed_new runs _PyArg_NoKeywords before anything else, so
	// reversed(seq, a=1) is a TypeError regardless of the argument count.
	//
	// CPython: Objects/enumobject.c:454 reversed_new (PyArg_NoKeywords)
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: reversed() takes no keyword arguments")
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: reversed() takes exactly one argument (%d given)", len(args))
	}
	o := args[0]
	t := o.Type()
	if descr, _ := objects.LookupDescriptor(t, "__reversed__"); descr != nil {
		fn, err := objects.GetAttr(o, objects.NewStr("__reversed__"))
		if err == nil && fn != nil {
			res, callErr := objects.CallObject(fn, nil)
			// reversed_new owns the bound method returned by the special
			// lookup and drops it once the call is done; without this the
			// bound method (and the sequence it captured as __self__) stays
			// pinned, so check_free_after_iterating never sees __del__ run.
			//
			// CPython: Objects/enumobject.c:1110 reversed_new_impl (Py_DECREF reversed_meth)
			objects.Decref(fn)
			return res, callErr
		}
	}
	if t.Sequence == nil || t.Sequence.GetItem == nil || t.Sequence.Length == nil {
		return nil, fmt.Errorf("TypeError: argument to reversed() must be a sequence")
	}
	n, err := t.Sequence.Length(o)
	if err != nil {
		return nil, err
	}
	return newReversedIter(o, n), nil
}

// Enumerate ports the enumerate(iterable, start=0) constructor. Both
// arguments may be passed positionally or by keyword, mirroring
// enumerate_vectorcall; the result carries cls so subclass instances
// keep their own type the way enum_new_impl's tp_alloc(type) does.
//
// CPython: Objects/enumobject.c:48 enum_new_impl
// CPython: Objects/enumobject.c:103 enumerate_vectorcall
func Enumerate(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	var iterable, startObj objects.Object
	switch len(args) {
	case 0:
	case 1:
		iterable = args[0]
	case 2:
		iterable, startObj = args[0], args[1]
	default:
		return nil, fmt.Errorf("TypeError: enumerate() takes at most 2 arguments (%d given)", len(args)+len(kwargs))
	}
	for k, v := range kwargs {
		switch k {
		case "iterable":
			if iterable != nil {
				return nil, fmt.Errorf("TypeError: argument for enumerate() given by name ('iterable') and position (1)")
			}
			iterable = v
		case "start":
			if startObj != nil {
				return nil, fmt.Errorf("TypeError: argument for enumerate() given by name ('start') and position (2)")
			}
			startObj = v
		default:
			return nil, fmt.Errorf("TypeError: '%s' is an invalid keyword argument for enumerate()", k)
		}
	}
	if iterable == nil {
		return nil, fmt.Errorf("TypeError: enumerate() missing required argument 'iterable'")
	}
	// start: PyNumber_Index(start) so any __index__-bearing value works,
	// including ints wider than int64. A non-index start is a TypeError.
	var index *big.Int
	if startObj != nil {
		si, err := objects.NumberIndex(startObj)
		if err != nil {
			return nil, err
		}
		bi, ok := si.(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: '%s' object cannot be interpreted as an integer", startObj.Type().Name)
		}
		index = bi.BigInt()
	}
	it, err := getIter(iterable)
	if err != nil {
		return nil, err
	}
	if cls == nil {
		cls = objects.EnumerateType
	}
	return objects.NewEnumerateOfType(cls, it, index), nil
}

// Zip ports builtin_zip. The strict kwarg is honored: when true, an
// iterator running short while another still has items raises
// ValueError.
//
// CPython: Objects/enumobject.c:zip_new_impl
func Zip(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	strict := false
	if v, ok := kwargs["strict"]; ok {
		b, err := objects.IsTruthy(v)
		if err != nil {
			return nil, err
		}
		strict = b
	}
	iters := make([]objects.Object, len(args))
	for i, a := range args {
		it, err := getIter(a)
		if err != nil {
			return nil, err
		}
		iters[i] = it
	}
	return newZip(iters, strict), nil
}

// Range ports the range() builtin. CPython's range_from_array accepts
// (stop), (start, stop), or (start, stop, step). Each operand is
// converted through PyNumber_Index (so __index__ objects and bools are
// accepted), and step must be non-zero.
//
// CPython: Objects/rangeobject.c:81 range_from_array
func Range(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	switch len(args) {
	case 1:
		stop, err := rangeIndex(args[0])
		if err != nil {
			return nil, err
		}
		return objects.NewRange(objects.NewInt(0), stop, objects.NewInt(1))
	case 2:
		start, err := rangeIndex(args[0])
		if err != nil {
			return nil, err
		}
		stop, err := rangeIndex(args[1])
		if err != nil {
			return nil, err
		}
		return objects.NewRange(start, stop, objects.NewInt(1))
	case 3:
		start, err := rangeIndex(args[0])
		if err != nil {
			return nil, err
		}
		stop, err := rangeIndex(args[1])
		if err != nil {
			return nil, err
		}
		step, err := rangeIndex(args[2])
		if err != nil {
			return nil, err
		}
		return objects.NewRange(start, stop, step)
	case 0:
		return nil, fmt.Errorf("TypeError: range expected at least 1 argument, got 0")
	default:
		return nil, fmt.Errorf("TypeError: range expected at most 3 arguments, got %d", len(args))
	}
}

// rangeIndex converts a range() operand through PyNumber_Index. The
// result is always a plain int (bools and __index__ objects fold to
// int), which is what range stores for start/stop/step.
//
// CPython: Objects/rangeobject.c:88 PyNumber_Index in range_from_array
func rangeIndex(o objects.Object) (*objects.Int, error) {
	idx, err := objects.NumberIndex(o)
	if err != nil {
		return nil, err
	}
	return idx.(*objects.Int), nil
}

// indexAsInt unwraps an Int argument or raises TypeError. CPython
// also accepts __index__-implementing objects; gopy adds that branch
// once tp_as_number.nb_index lands.
func indexAsInt(o objects.Object) (*objects.Int, error) {
	if i, ok := o.(*objects.Int); ok {
		return i, nil
	}
	return nil, fmt.Errorf("TypeError: '%s' object cannot be interpreted as an integer", o.Type().Name)
}
