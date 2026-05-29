package objects

import (
	"fmt"
	"sync"
)

// Slice is the Python slice object: start, stop, step.
//
// CPython: Include/cpython/sliceobject.h:L8 PySliceObject
type Slice struct {
	Header
	Start Object
	Stop  Object
	Step  Object
}

// SliceType is the type singleton for slice. Mirrors PySlice_Type.
//
// CPython: Objects/sliceobject.c:L325 PySlice_Type
var SliceType = NewType("slice", []*Type{objectType})

// sliceFreeList recycles slice carcasses. CPython keeps a single-slot
// freelist per interpreter (Py_slices_MAXFREELIST = 1) because slices
// are dominated by hot BUILD_SLICE / extended-slicing patterns that
// alloc and free one slice at a time. gopy uses sync.Pool so the cache
// composes with Go's GC: under pressure the pool drains on its own.
//
// CPython: Include/internal/pycore_freelist_state.h Py_slices_MAXFREELIST
var sliceFreeList = sync.Pool{
	New: func() any { return &Slice{} },
}

func init() {
	SliceType.Repr = sliceRepr
	SliceType.Str = sliceRepr
	SliceType.Dealloc = sliceDealloc
	SliceType.Hash = sliceHash
	SliceType.Getattro = GenericGetAttr
	// CPython: Objects/sliceobject.c:683 tp_hash = slice_hash
	SetTypeDescr(SliceType, "__hash__", NewMethodDescr(SliceType, "__hash__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: __hash__() takes no arguments (%d given)", len(args)-1)
		}
		h, err := sliceHash(args[0])
		if err != nil {
			return nil, err
		}
		return NewInt(h), nil
	}))
	// slice(stop) / slice(start, stop) / slice(start, stop, step).
	// CPython: Objects/sliceobject.c:319 slice_new
	SliceType.TpNew = sliceTpNew
	// CPython: Objects/sliceobject.c:576 slice_richcompare
	SliceType.RichCmp = sliceRichCmp
	SetTypeDescr(SliceType, "start", NewGetSetDescr("start",
		func(o Object) (Object, error) {
			s, ok := asSlice(o)
			if !ok {
				return nil, fmt.Errorf("TypeError: descriptor 'start' for 'slice' objects doesn't apply to a '%s' object", typeNameOf(o))
			}
			return s.Start, nil
		}, nil))
	SetTypeDescr(SliceType, "stop", NewGetSetDescr("stop",
		func(o Object) (Object, error) {
			s, ok := asSlice(o)
			if !ok {
				return nil, fmt.Errorf("TypeError: descriptor 'stop' for 'slice' objects doesn't apply to a '%s' object", typeNameOf(o))
			}
			return s.Stop, nil
		}, nil))
	SetTypeDescr(SliceType, "step", NewGetSetDescr("step",
		func(o Object) (Object, error) {
			s, ok := asSlice(o)
			if !ok {
				return nil, fmt.Errorf("TypeError: descriptor 'step' for 'slice' objects doesn't apply to a '%s' object", typeNameOf(o))
			}
			return s.Step, nil
		}, nil))
	// CPython: Objects/sliceobject.c:L249 slice_getnewargs
	SetTypeDescr(SliceType, "__getnewargs__", NewMethodDescr(SliceType, "__getnewargs__", sliceGetNewArgs))
	// CPython: Objects/sliceobject.c:561 slice_reduce
	SetTypeDescr(SliceType, "__reduce__", NewMethodDescr(SliceType, "__reduce__", sliceReduce))
	// CPython: Objects/sliceobject.c:201 slice_indices
	SetTypeDescr(SliceType, "indices", NewMethodDescr(SliceType, "indices", sliceIndicesMethod))
	// Expose __new__ so slice.__new__(slice, ...) routes to sliceTpNew
	// instead of falling back to object.__new__ which returns *Instance.
	// CPython: Objects/sliceobject.c:L319 slice_new via staticmethod wrapper
	SetTypeDescr(SliceType, "__new__", NewBuiltinFunction("slice.__new__",
		func(args []Object, kwargs map[string]Object) (Object, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("TypeError: slice.__new__(): not enough arguments")
			}
			cls, ok := args[0].(*Type)
			if !ok {
				return nil, fmt.Errorf("TypeError: slice.__new__(X): X is not a type object")
			}
			return sliceTpNew(cls, args[1:], kwargs)
		}))
}

// sliceTpNew implements the slice constructor: slice(stop),
// slice(start, stop), or slice(start, stop, step).
//
// CPython: Objects/sliceobject.c:319 slice_new
func sliceTpNew(_ *Type, args []Object, kwargs map[string]Object) (Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: slice() takes no keyword arguments")
	}
	var start, stop, step Object
	switch len(args) {
	case 1:
		stop = args[0]
	case 2:
		start, stop = args[0], args[1]
	case 3:
		start, stop, step = args[0], args[1], args[2]
	default:
		return nil, fmt.Errorf("TypeError: slice expected at most 3 arguments, got %d", len(args))
	}
	return NewSlice(start, stop, step), nil
}

// NewSlice builds a slice. Any of start/stop/step may be None. The
// carcass comes from the freelist when available so the hot BUILD_SLICE
// path skips the allocator. start/stop/step are Increfed because the
// slice now owns its references, matching PySlice_New's Py_XNewRef.
//
// CPython: Objects/sliceobject.c:L143 PySlice_New
func NewSlice(start, stop, step Object) *Slice {
	if start == nil {
		start = None()
	}
	if stop == nil {
		stop = None()
	}
	if step == nil {
		step = None()
	}
	s := sliceFreeList.Get().(*Slice)
	s.init(SliceType)
	s.Start = start
	s.Stop = stop
	s.Step = step
	Incref(start)
	Incref(stop)
	Incref(step)
	return s
}

// sliceDealloc releases the references the slice owns and returns the
// carcass to the freelist. Mirrors slice_dealloc: Py_DECREF each field,
// then either free the object or push it onto the per-interpreter
// freelist when there is room.
//
// CPython: Objects/sliceobject.c:L347 slice_dealloc
func sliceDealloc(o Object) {
	s, ok := o.(*Slice)
	if !ok {
		return
	}
	Decref(s.Start)
	Decref(s.Stop)
	Decref(s.Step)
	s.Start = nil
	s.Stop = nil
	s.Step = nil
	sliceFreeList.Put(s)
}

// sliceRichCmp compares two slice objects by comparing their (start, stop, step)
// tuples, mirroring slice_richcompare.
//
// CPython: Objects/sliceobject.c:576 slice_richcompare
func sliceRichCmp(a, b Object, op CompareOp) (Object, error) {
	sa, ok1 := a.(*Slice)
	sb, ok2 := b.(*Slice)
	if !ok1 || !ok2 {
		return NotImplemented(), nil
	}
	t1 := NewTuple([]Object{sa.Start, sa.Stop, sa.Step})
	t2 := NewTuple([]Object{sb.Start, sb.Stop, sb.Step})
	return RichCmp(t1, t2, op)
}

// asSlice extracts a *Slice from o.
//
// CPython: Objects/sliceobject.c PySlice_Check
func asSlice(o Object) (*Slice, bool) {
	s, ok := o.(*Slice)
	return s, ok
}

func sliceRepr(o Object) (string, error) {
	s, ok := asSlice(o)
	if !ok {
		return "", fmt.Errorf("TypeError: descriptor 'repr' for 'slice' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	startR, err := Repr(s.Start)
	if err != nil {
		return "", err
	}
	stopR, err := Repr(s.Stop)
	if err != nil {
		return "", err
	}
	stepR, err := Repr(s.Step)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("slice(%s, %s, %s)", startR, stopR, stepR), nil
}

// sliceGetNewArgs implements slice.__getnewargs__(): returns (start, stop, step)
// so that pickle/copy can reconstruct the slice via slice.__new__.
//
// CPython: Objects/sliceobject.c:L249 slice_getnewargs
func sliceGetNewArgs(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __getnewargs__() takes no arguments (%d given)", len(args)-1)
	}
	s, ok := asSlice(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__getnewargs__' for 'slice' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	return NewTuple([]Object{s.Start, s.Stop, s.Step}), nil
}

// sliceHash implements slice.__hash__. A slice is hashable when all
// three components (start, stop, step) are hashable. The accumulator
// mirrors CPython's tuple-based xxHash scheme in tupleobject.c.
//
// CPython: Objects/sliceobject.c:646 slice_hash
func sliceHash(o Object) (int64, error) {
	s, ok := asSlice(o)
	if !ok {
		return 0, fmt.Errorf("TypeError: descriptor '__hash__' for 'slice' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	// xxHash constants (64-bit build).
	// CPython: Objects/sliceobject.c:634-637 _PyHASH_XXPRIME_{1,2,5}
	const (
		prime1 uint64 = 11400714785074694791
		prime2 uint64 = 14029467366897019727
		prime5 uint64 = 2870177450012600261
	)
	xxrotate := func(v uint64) uint64 { return (v << 31) | (v >> 33) }

	acc := prime5
	for _, part := range []Object{s.Start, s.Stop, s.Step} {
		lane64, err := Hash(part)
		if err != nil {
			return 0, err
		}
		lane := uint64(lane64)
		acc += lane * prime2
		acc = xxrotate(acc)
		acc *= prime1
	}
	if acc == ^uint64(0) {
		return 1546275796, nil
	}
	return int64(acc), nil
}

// sliceReduce implements slice.__reduce__: returns (slice, (start, stop, step))
// so that pickle can reconstruct the slice via slice(start, stop, step).
// This is the CPython implementation.
//
// CPython: Objects/sliceobject.c:561 slice_reduce
func sliceReduce(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments (%d given)", len(args)-1)
	}
	s, ok := asSlice(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__reduce__' for 'slice' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	return NewTuple([]Object{SliceType, NewTuple([]Object{s.Start, s.Stop, s.Step})}), nil
}
