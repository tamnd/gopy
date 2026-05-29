package objects

import (
	"errors"
	"fmt"
	"math/big"
)

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
	RangeType.TpFlags |= TpFlagSequence
	RangeType.Repr = rangeRepr
	RangeType.Str = rangeRepr
	RangeType.RichCmp = rangeRichCmp
	RangeType.Iter = rangeIter
	RangeType.Sequence = &SequenceMethods{
		Length:   rangeLen,
		GetItem:  rangeItem,
		Contains: rangeContains,
	}
	RangeType.Mapping = &MappingMethods{
		Length:  rangeLen,
		GetItem: rangeSubscript,
	}
	// CPython: Objects/rangeobject.c:939 range_reduce
	SetTypeDescr(RangeType, "__reduce__", NewMethodDescr(RangeType, "__reduce__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments")
			}
			r := args[0].(*Range)
			return NewTuple([]Object{
				RangeType,
				NewTuple([]Object{r.Start, r.Stop, r.Step}),
			}), nil
		},
	))
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

func rangeIter(o Object) (Object, error) {
	r := o.(*Range)
	return newRangeIterator(r.Start, r.Stop, r.Step), nil
}

// rangeRichCmp ports range_richcompare: only EQ / NE produce a real
// answer (anything else is NotImplemented). Two ranges are equal when
// their lengths match, and either both are empty or both start at the
// same value AND (length == 1 or both steps match). Identity of
// start/stop on Python ints stays through big.Int.Cmp because that's
// how integer equality is computed elsewhere in objects/.
//
// CPython: Objects/rangeobject.c:541 range_richcompare
func rangeRichCmp(a, b Object, op CompareOp) (Object, error) {
	if op != CompareEQ && op != CompareNE {
		return notImplemented(), nil
	}
	r0 := a.(*Range)
	r1, ok := b.(*Range)
	if !ok {
		return notImplemented(), nil
	}
	eq := rangeEquals(r0, r1)
	if op == CompareNE {
		eq = !eq
	}
	return NewBool(eq), nil
}

// rangeEquals returns true when r0 and r1 describe the same sequence.
//
// CPython: Objects/rangeobject.c:515 range_equals
func rangeEquals(r0, r1 *Range) bool {
	if r0 == r1 {
		return true
	}
	l0 := rangeLengthBig(&r0.Start.v, &r0.Stop.v, &r0.Step.v)
	l1 := rangeLengthBig(&r1.Start.v, &r1.Stop.v, &r1.Step.v)
	if l0.Cmp(l1) != 0 {
		return false
	}
	if l0.Sign() == 0 {
		return true
	}
	if r0.Start.v.Cmp(&r1.Start.v) != 0 {
		return false
	}
	if l0.Cmp(big.NewInt(1)) == 0 {
		return true
	}
	return r0.Step.v.Cmp(&r1.Step.v) == 0
}

// rangeLengthBig returns the number of elements in the range as a
// big.Int. Mirrors compute_range_length but uses math/big throughout
// so the arithmetic handles unbounded bounds.
//
// CPython: Objects/rangeobject.c:228 compute_range_length
func rangeLengthBig(start, stop, step *big.Int) *big.Int {
	var lo, hi, st *big.Int
	if step.Sign() > 0 {
		lo, hi = start, stop
		st = new(big.Int).Set(step)
	} else {
		lo, hi = stop, start
		st = new(big.Int).Neg(step)
	}
	if lo.Cmp(hi) >= 0 {
		return big.NewInt(0)
	}
	diff := new(big.Int).Sub(hi, lo)
	diff.Sub(diff, big.NewInt(1))
	diff.Quo(diff, st)
	diff.Add(diff, big.NewInt(1))
	return diff
}

// rangeLen returns len(r). Routed through both sq_length and
// mp_length.
//
// CPython: Objects/rangeobject.c:311 range_length
func rangeLen(o Object) (int, error) {
	r := o.(*Range)
	n := rangeLengthBig(&r.Start.v, &r.Stop.v, &r.Step.v)
	if !n.IsInt64() {
		return 0, fmt.Errorf("OverflowError: Python int too large to convert to C ssize_t")
	}
	v := n.Int64()
	const maxInt = int64(^uint(0) >> 1)
	if v > maxInt {
		return 0, fmt.Errorf("OverflowError: Python int too large to convert to C ssize_t")
	}
	return int(v), nil
}

// computeRangeItem returns start + i*step. Mirrors the inline body of
// compute_item but uses math/big so large ranges stay exact.
//
// CPython: Objects/rangeobject.c:318 compute_item
func computeRangeItem(r *Range, i *big.Int) *big.Int {
	out := new(big.Int).Mul(i, &r.Step.v)
	out.Add(out, &r.Start.v)
	return out
}

// rangeItem implements sq_item: range[i] with the index already
// normalised to a Go int. CPython routes the same call through
// compute_range_item which handles the negative-index wrap before
// calling compute_item.
//
// CPython: Objects/rangeobject.c:391 range_item
func rangeItem(o Object, i int) (Object, error) {
	r := o.(*Range)
	lenBig := rangeLengthBig(&r.Start.v, &r.Stop.v, &r.Step.v)
	bi := big.NewInt(int64(i))
	if bi.Sign() < 0 {
		bi.Add(bi, lenBig)
	}
	if bi.Sign() < 0 || bi.Cmp(lenBig) >= 0 {
		return nil, fmt.Errorf("IndexError: range object index out of range")
	}
	return NewIntFromBig(computeRangeItem(r, bi)), nil
}

// computeRangeSlice builds a fresh range covering self[slice]. Uses
// sliceGetLongIndices so that ranges with lengths beyond sys.maxsize
// (e.g., range(2**100)) are handled with arbitrary-precision arithmetic.
//
// CPython: Objects/rangeobject.c:404 compute_slice
func computeRangeSlice(r *Range, s *Slice) (*Range, error) {
	lenBig := rangeLengthBig(&r.Start.v, &r.Stop.v, &r.Step.v)
	lenObj := NewIntFromBig(lenBig)
	startObj, stopObj, stepObj, err := sliceGetLongIndices(s, lenObj)
	if err != nil {
		return nil, err
	}
	startBig := startObj.(*Int).BigInt()
	stopBig := stopObj.(*Int).BigInt()
	stepInt := stepObj.(*Int).BigInt()
	substep := new(big.Int).Mul(&r.Step.v, stepInt)
	substart := computeRangeItem(r, startBig)
	substop := computeRangeItem(r, stopBig)
	out := &Range{
		Start: NewIntFromBig(substart),
		Stop:  NewIntFromBig(substop),
		Step:  NewIntFromBig(substep),
	}
	out.init(RangeType)
	return out, nil
}

// rangeSubscript implements mp_subscript: range[i] handles both int
// and slice keys; slice returns a new range covering the subset.
//
// CPython: Objects/rangeobject.c:714 range_subscript
func rangeSubscript(o, key Object) (Object, error) {
	r := o.(*Range)
	if s, ok := key.(*Slice); ok {
		out, err := computeRangeSlice(r, s)
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	i, err := indexAsInt(key)
	if err != nil {
		return nil, err
	}
	return rangeItem(r, i)
}

// rangeContains implements `x in r`. CPython has a fast path for
// PyLong/Bool that checks start/stop bounds and step divisibility; we
// mirror it for *Int and fall back to iteration otherwise.
//
// CPython: Objects/rangeobject.c:488 range_contains
func rangeContains(o, v Object) (bool, error) {
	r := o.(*Range)
	var bv *big.Int
	switch x := v.(type) {
	case *Int:
		bv = &x.v
	case *Bool:
		bv = big.NewInt(0)
		if x == True() {
			bv.SetInt64(1)
		}
	default:
		return rangeContainsIter(o, v)
	}
	step := &r.Step.v
	start := &r.Start.v
	stop := &r.Stop.v
	if step.Sign() > 0 {
		if bv.Cmp(start) < 0 || bv.Cmp(stop) >= 0 {
			return false, nil
		}
	} else {
		if bv.Cmp(start) > 0 || bv.Cmp(stop) <= 0 {
			return false, nil
		}
	}
	diff := new(big.Int).Sub(bv, start)
	rem := new(big.Int).Rem(diff, step)
	return rem.Sign() == 0, nil
}

// rangeContainsIter falls back to walking the iterator when the
// element isn't an integer-like value. Mirrors CPython's
// _PySequence_IterSearch fallback.
//
// CPython: Objects/rangeobject.c:495 range_contains (iter fallback)
func rangeContainsIter(o, v Object) (bool, error) {
	it, err := Iter(o)
	if err != nil {
		return false, err
	}
	for {
		x, err := IterNext(it)
		if err != nil {
			if errors.Is(err, ErrStopIteration) {
				return false, nil
			}
			return false, err
		}
		eq, err := RichCmpBool(x, v, CompareEQ)
		if err != nil {
			return false, err
		}
		if eq {
			return true, nil
		}
	}
}
