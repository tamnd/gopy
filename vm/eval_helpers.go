// Small helpers the dispatch arms call into. The action translator
// (1621/B6) emits some of these; the hand-written panel uses them
// directly. CPython has them as macros / inline helpers in
// Python/ceval_macros.h and Python/ceval.c.
package vm

import (
	"errors"

	"github.com/tamnd/gopy/intrinsics"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// decrefInputs drops n stack-ref inputs the action translator already
// consumed via push/pop. CPython's PyStackRef_CLOSE is reduced to a
// no-op in gopy because Go's GC handles object lifetime; the stack
// ref already cleared its slot when it was popped. The helper is
// kept so generated arms can call it without conditional emission.
//
// CPython: Python/ceval_macros.h DECREF_INPUTS
func (e *evalState) decrefInputs(n int) {
	_ = n
}

// incref / decref / newref are CPython refcount macros. With Go GC
// they reduce to identity on the Object pointer; the helpers exist so
// generated arms compile against the same surface.
//
// CPython: Include/object.h Py_INCREF / Py_DECREF / Py_NewRef
func (e *evalState) incref(o objects.Object) objects.Object   { return o }
func (e *evalState) decref(o objects.Object)                  { _ = o }
func (e *evalState) newref(o objects.Object) objects.Object   { return o }

// iterToSlice walks any iterable and returns its items. Used by
// LIST_EXTEND, UNPACK_EX, CALL_FUNCTION_EX, and similar arms that
// need to drain an iterator into Go memory before doing the real
// work.
//
// CPython: Python/bytecodes.c GET_ITER + FOR_ITER loop, drained.
func iterToSlice(o objects.Object) ([]objects.Object, error) {
	if o == nil {
		return nil, errors.New("TypeError: cannot iterate nil")
	}
	t := o.Type()
	if t.Iter == nil {
		// Tuple/List shortcut: avoid iterator allocation for the common case.
		switch v := o.(type) {
		case *objects.Tuple:
			out := make([]objects.Object, v.Len())
			for i := range out {
				out[i] = v.Item(i)
			}
			return out, nil
		case *objects.List:
			out := make([]objects.Object, v.Len())
			for i := range out {
				out[i] = v.Item(i)
			}
			return out, nil
		}
		return nil, errors.New("TypeError: '" + t.Name + "' object is not iterable")
	}
	it, ierr := t.Iter(o)
	if ierr != nil {
		return nil, ierr
	}
	itType := it.Type()
	if itType.IterNext == nil {
		return nil, errors.New("TypeError: iter() returned non-iterator of type '" + itType.Name + "'")
	}
	var out []objects.Object
	for {
		v, nerr := itType.IterNext(it)
		if errors.Is(nerr, objects.ErrStopIteration) {
			return out, nil
		}
		if nerr != nil {
			return nil, nerr
		}
		out = append(out, v)
	}
}

// intrinsicsUnary is the CALL_INTRINSIC_1 dispatch table indexed by
// oparg. Reads through to the intrinsics package so the eval loop
// stays free of helper bodies.
//
// CPython: Python/intrinsics.c _PyIntrinsics_UnaryFunctions
var intrinsicsUnary = unaryTable()

// intrinsicsBinary is the CALL_INTRINSIC_2 dispatch table.
//
// CPython: Python/intrinsics.c _PyIntrinsics_BinaryFunctions
var intrinsicsBinary = binaryTable()

// sliceIndex normalises a slice bound to a Go int. None means
// "use the length"; negative means "from the end". Out-of-range
// indexes clamp to [0, length].
func sliceIndex(o objects.Object, length int, isStop bool) (int, error) {
	if o == nil || o == objects.None() {
		if isStop {
			return length, nil
		}
		return 0, nil
	}
	i, ok := o.(*objects.Int)
	if !ok {
		return 0, errors.New("TypeError: slice indices must be integers or None")
	}
	v, _ := i.Int64()
	idx := int(v)
	if idx < 0 {
		idx += length
		if idx < 0 {
			idx = 0
		}
	}
	if idx > length {
		idx = length
	}
	return idx, nil
}

func sliceContainer(container, start, stop objects.Object) (objects.Object, error) {
	switch c := container.(type) {
	case *objects.List:
		s, err := sliceIndex(start, c.Len(), false)
		if err != nil {
			return nil, err
		}
		e, err := sliceIndex(stop, c.Len(), true)
		if err != nil {
			return nil, err
		}
		if e < s {
			e = s
		}
		out := make([]objects.Object, 0, e-s)
		for i := s; i < e; i++ {
			out = append(out, c.Item(i))
		}
		return objects.NewList(out), nil
	case *objects.Tuple:
		s, err := sliceIndex(start, c.Len(), false)
		if err != nil {
			return nil, err
		}
		e, err := sliceIndex(stop, c.Len(), true)
		if err != nil {
			return nil, err
		}
		if e < s {
			e = s
		}
		out := make([]objects.Object, 0, e-s)
		for i := s; i < e; i++ {
			out = append(out, c.Item(i))
		}
		return objects.NewTuple(out), nil
	}
	return nil, errors.New("TypeError: BINARY_SLICE: unsupported container type '" + container.Type().Name + "'")
}

func storeSlice(container, start, stop, value objects.Object) error {
	l, ok := container.(*objects.List)
	if !ok {
		return errors.New("TypeError: STORE_SLICE: unsupported container type '" + container.Type().Name + "'")
	}
	s, err := sliceIndex(start, l.Len(), false)
	if err != nil {
		return err
	}
	e, err := sliceIndex(stop, l.Len(), true)
	if err != nil {
		return err
	}
	if e < s {
		e = s
	}
	items, err := iterToSlice(value)
	if err != nil {
		return err
	}
	l.SetSlice(s, e, items)
	return nil
}

func unaryTable() []func(*state.Thread, objects.Object) (objects.Object, error) {
	out := make([]func(*state.Thread, objects.Object) (objects.Object, error), len(intrinsics.UnaryTable))
	for i, fn := range intrinsics.UnaryTable {
		out[i] = fn
	}
	return out
}

func binaryTable() []func(*state.Thread, objects.Object, objects.Object) (objects.Object, error) {
	out := make([]func(*state.Thread, objects.Object, objects.Object) (objects.Object, error), len(intrinsics.BinaryTable))
	for i, fn := range intrinsics.BinaryTable {
		out[i] = fn
	}
	return out
}
