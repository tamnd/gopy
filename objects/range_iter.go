// Iterator for range. Mirrors PyLongRangeIter_Type and the
// __length_hint__ method that lets callers (sorted, reversed, etc.)
// pre-size their buffers from a half-consumed range_iterator.
//
// CPython: Objects/rangeobject.c:1128 PyLongRangeIter_Type
// CPython: Objects/rangeobject.c:1006 longrangeiter_len

package objects

import "math/big"

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
	AddIterSlotWrappers(rangeIterType)
}

func newRangeIterator(start, stop, step *Int) *rangeIterator {
	it := &rangeIterator{cur: start, stop: stop, step: step, asc: step.v.Sign() > 0}
	it.init(rangeIterType)
	return it
}

// RangeIterNextFast advances o as a range_iterator without going
// through the type-table tp_iternext indirection. Returns the next
// value or (nil, true) on exhaustion. ok=false means o was not
// exactly a range_iterator and the FOR_ITER_RANGE fast arm must
// deopt.
//
// CPython: Python/bytecodes.c _ITER_CHECK_RANGE + _ITER_JUMP_RANGE + _ITER_NEXT_RANGE
func RangeIterNextFast(o Object) (value Object, exhausted bool, ok bool) {
	it, asserted := o.(*rangeIterator)
	if !asserted || it.Type() != rangeIterType {
		return nil, false, false
	}
	c := it.cur.v.Cmp(&it.stop.v)
	if (it.asc && c >= 0) || (!it.asc && c <= 0) {
		return nil, true, true
	}
	out := NewIntFromBig(&it.cur.v)
	next := &Int{}
	next.init(IntType)
	next.v.Add(&it.cur.v, &it.step.v)
	it.cur = next
	return out, false, true
}

// LengthHint returns the number of values the iterator will still
// produce. Mirrors __length_hint__ on a range_iterator: ceil((stop -
// cur) / step), clamped to 0.
//
// CPython: Objects/rangeobject.c:1006 longrangeiter_len
func (it *rangeIterator) LengthHint() *Int {
	diff := new(big.Int).Sub(&it.stop.v, &it.cur.v)
	if it.asc {
		if diff.Sign() <= 0 {
			return NewInt(0)
		}
	} else {
		if diff.Sign() >= 0 {
			return NewInt(0)
		}
		diff.Neg(diff)
	}
	step := new(big.Int).Set(&it.step.v)
	if step.Sign() < 0 {
		step.Neg(step)
	}
	q, r := new(big.Int).QuoRem(diff, step, new(big.Int))
	if r.Sign() != 0 {
		q.Add(q, big.NewInt(1))
	}
	return NewIntFromBig(q)
}
