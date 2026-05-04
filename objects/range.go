package objects

import "fmt"

// Range is the Python range object. Bounds are stored as Int so the
// arithmetic stays correct for the full int range.
//
// CPython: Objects/rangeobject.c:L19 rangeobject
type Range struct {
	Header
	Start *Int
	Stop  *Int
	Step  *Int
}

// RangeType is the type singleton for range. Mirrors PyRange_Type.
//
// CPython: Objects/rangeobject.c:L949 PyRange_Type
var RangeType = NewType("range", []*Type{objectType})

func init() {
	RangeType.Repr = rangeRepr
	RangeType.Str = rangeRepr
	RangeType.Iter = rangeIter
}

// NewRange builds a range. Step must be non-zero.
//
// CPython: Objects/rangeobject.c:L92 range_from_array
func NewRange(start, stop, step *Int) (*Range, error) {
	if step.v.Sign() == 0 {
		return nil, fmt.Errorf("ValueError: range() arg 3 must not be zero")
	}
	r := &Range{Start: start, Stop: stop, Step: step}
	r.init(RangeType)
	return r, nil
}

func rangeRepr(o Object) (string, error) {
	r := o.(*Range)
	if v, ok := r.Step.Int64(); ok && v == 1 {
		return fmt.Sprintf("range(%s, %s)", r.Start.v.String(), r.Stop.v.String()), nil
	}
	return fmt.Sprintf("range(%s, %s, %s)", r.Start.v.String(), r.Stop.v.String(), r.Step.v.String()), nil
}

// rangeIterator yields Ints from start (inclusive) to stop (exclusive)
// stepping by step. CPython has long-range and short-range fast paths;
// v0.2 ships only the long-range path.
//
// CPython: Objects/rangeobject.c:L877 PyLongRangeIter_Type
type rangeIterator struct {
	Header
	cur  *Int
	stop *Int
	step *Int
	asc  bool
}

var rangeIterType = NewType("range_iterator", []*Type{objectType})

func init() {
	rangeIterType.Iter = func(o Object) (Object, error) { return o, nil }
	rangeIterType.IterNext = func(o Object) (Object, error) {
		it := o.(*rangeIterator)
		c := it.cur.v.Cmp(&it.stop.v)
		if (it.asc && c >= 0) || (!it.asc && c <= 0) {
			return nil, ErrStopIteration
		}
		out := NewIntFromBig(&it.cur.v)
		next := &Int{}
		next.init(IntType)
		next.v.Add(&it.cur.v, &it.step.v)
		it.cur = next
		return out, nil
	}
}

func rangeIter(o Object) (Object, error) {
	r := o.(*Range)
	it := &rangeIterator{cur: r.Start, stop: r.Stop, step: r.Step, asc: r.Step.v.Sign() > 0}
	it.init(rangeIterType)
	return it, nil
}
