package objects

import "fmt"

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

func init() {
	SliceType.Repr = sliceRepr
	SliceType.Str = sliceRepr
}

// NewSlice builds a slice. Any of start/stop/step may be None.
//
// CPython: Objects/sliceobject.c:L165 PySlice_New
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
	s := &Slice{Start: start, Stop: stop, Step: step}
	s.init(SliceType)
	return s
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
