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
func Reversed(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: reversed() takes exactly one argument (%d given)", len(args))
	}
	o := args[0]
	t := o.Type()
	if descr, _ := objects.LookupDescriptor(t, "__reversed__"); descr != nil {
		fn, err := objects.GetAttr(o, objects.NewStr("__reversed__"))
		if err == nil && fn != nil {
			return objects.CallObject(fn, nil)
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

// Enumerate ports builtin_enumerate. Returns an iterator yielding
// (index, value) tuples. Optional start kwarg / second positional
// shifts the index.
//
// CPython: Objects/enumobject.c:enumerate_new_impl
func Enumerate(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: enumerate expected 1 or 2 arguments, got %d", len(args))
	}
	start := int64(0)
	if len(args) == 2 {
		v, err := indexAsInt64(args[1], "enumerate")
		if err != nil {
			return nil, err
		}
		start = v
	}
	if v, ok := kwargs["start"]; ok {
		s, err := indexAsInt64(v, "enumerate")
		if err != nil {
			return nil, err
		}
		start = s
	}
	it, err := getIter(args[0])
	if err != nil {
		return nil, err
	}
	return newEnumerate(it, start), nil
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

// Range ports the range() builtin. CPython's range_new accepts
// (stop), (start, stop), or (start, stop, step). All three operands
// must be ints; step must be non-zero.
//
// CPython: Objects/rangeobject.c:97 range_new
func Range(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	switch len(args) {
	case 1:
		stop, err := indexAsInt(args[0])
		if err != nil {
			return nil, err
		}
		return objects.NewRange(objects.NewInt(0), stop, objects.NewInt(1))
	case 2:
		start, err := indexAsInt(args[0])
		if err != nil {
			return nil, err
		}
		stop, err := indexAsInt(args[1])
		if err != nil {
			return nil, err
		}
		return objects.NewRange(start, stop, objects.NewInt(1))
	case 3:
		start, err := indexAsInt(args[0])
		if err != nil {
			return nil, err
		}
		stop, err := indexAsInt(args[1])
		if err != nil {
			return nil, err
		}
		step, err := indexAsInt(args[2])
		if err != nil {
			return nil, err
		}
		return objects.NewRange(start, stop, step)
	default:
		return nil, fmt.Errorf("TypeError: range expected 1 to 3 arguments, got %d", len(args))
	}
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

func indexAsInt64(o objects.Object, where string) (int64, error) {
	i, err := indexAsInt(o)
	if err != nil {
		return 0, err
	}
	v, ok := i.Int64()
	if !ok {
		return 0, fmt.Errorf("OverflowError: %s argument does not fit in int64", where)
	}
	return v, nil
}
