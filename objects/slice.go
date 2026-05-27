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
	SliceType.Getattro = GenericGetAttr
	// slice(stop) / slice(start, stop) / slice(start, stop, step).
	// CPython: Objects/sliceobject.c:319 slice_new
	SliceType.TpNew = sliceTpNew
	// CPython: Objects/sliceobject.c:576 slice_richcompare
	SliceType.RichCmp = sliceRichCmp
	SetTypeDescr(SliceType, "start", NewGetSetDescr("start",
		func(o Object) (Object, error) { return o.(*Slice).Start, nil }, nil))
	SetTypeDescr(SliceType, "stop", NewGetSetDescr("stop",
		func(o Object) (Object, error) { return o.(*Slice).Stop, nil }, nil))
	SetTypeDescr(SliceType, "step", NewGetSetDescr("step",
		func(o Object) (Object, error) { return o.(*Slice).Step, nil }, nil))
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
	s := o.(*Slice)
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

func sliceRepr(o Object) (string, error) {
	s := o.(*Slice)
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
