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
