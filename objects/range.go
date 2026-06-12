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
	// range_hash hashes the (length, start, step) triple, collapsing
	// empty/length-1 ranges so equal ranges hash equal.
	//
	// CPython: Objects/rangeobject.c:579 range_hash
	RangeType.Hash = rangeHash
	// range_as_number only fills nb_bool. bool(range) goes through the
	// stored length, so bool(range(-sys.maxsize, sys.maxsize)) is True
	// even though len() of that range overflows Py_ssize_t.
	//
	// CPython: Objects/rangeobject.c:743 range_bool
	RangeType.Number = &NumberMethods{
		Bool: rangeBool,
	}
	// range inherits tp_setattro from object (PyObject_GenericSetAttr),
	// so assigning to the read-only start/stop/step getsets raises
	// AttributeError rather than the "no attributes" fallback.
	//
	// CPython: Objects/rangeobject.c:781 PyRange_Type (tp_setattro inherited)
	RangeType.Setattro = GenericSetAttr
	RangeType.Sequence = &SequenceMethods{
		Length:   rangeLen,
		GetItem:  rangeItem,
		Contains: rangeContains,
	}
	RangeType.Mapping = &MappingMethods{
		Length:  rangeLen,
		GetItem: rangeSubscript,
	}
	// range_members: start, stop, step are read-only int attributes.
	//
	// CPython: Objects/rangeobject.c:774 range_members
	SetTypeDescr(RangeType, "start", NewGetSetDescr("start",
		func(o Object) (Object, error) { return o.(*Range).Start, nil }, nil))
	SetTypeDescr(RangeType, "stop", NewGetSetDescr("stop",
		func(o Object) (Object, error) { return o.(*Range).Stop, nil }, nil))
	SetTypeDescr(RangeType, "step", NewGetSetDescr("step",
		func(o Object) (Object, error) { return o.(*Range).Step, nil }, nil))
	// range_methods: count and index, both METH_O.
	//
	// CPython: Objects/rangeobject.c:769 range_methods
	SetTypeDescr(RangeType, "count", NewMethodDescrConv(RangeType, "count", MethO,
		func(args []Object, _ map[string]Object) (Object, error) {
			return rangeCount(args[0], args[1])
		},
	))
	SetTypeDescr(RangeType, "index", NewMethodDescrConv(RangeType, "index", MethO,
		func(args []Object, _ map[string]Object) (Object, error) {
			return rangeIndex(args[0], args[1])
		},
	))
	// CPython: Objects/rangeobject.c:939 range_reduce
	SetTypeDescr(RangeType, "__reduce__", NewMethodDescrConv(RangeType, "__reduce__", MethNoArgs,
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
	// __getitem__ and __len__ slot wrappers so attribute lookup finds
	// range.__getitem__ / range.__len__ without bouncing through
	// add_operators on the C side.
	//
	// CPython: Objects/typeobject.c add_operators slot wrappers for
	// sq_item / sq_length.
	SetTypeDescr(RangeType, "__getitem__", NewMethodDescr(RangeType, "__getitem__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __getitem__() takes exactly one argument (%d given)", len(args)-1)
			}
			return rangeSubscript(args[0], args[1])
		},
	))
	SetTypeDescr(RangeType, "__len__", NewMethodDescr(RangeType, "__len__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __len__() takes no arguments (%d given)", len(args)-1)
			}
			n, err := rangeLen(args[0])
			if err != nil {
				return nil, err
			}
			return NewInt(int64(n)), nil
		},
	))
	SetTypeDescr(RangeType, "__contains__", NewMethodDescr(RangeType, "__contains__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __contains__() takes exactly one argument (%d given)", len(args)-1)
			}
			ok, err := rangeContains(args[0], args[1])
			if err != nil {
				return nil, err
			}
			return NewBool(ok), nil
		},
	))
	SetTypeDescr(RangeType, "__iter__", NewMethodDescr(RangeType, "__iter__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __iter__() takes no arguments (%d given)", len(args)-1)
			}
			return rangeIter(args[0])
		},
	))
	// CPython: Objects/rangeobject.c:1215 range_reverse
	SetTypeDescr(RangeType, "__reversed__", NewMethodDescrConv(RangeType, "__reversed__", MethNoArgs,
		func(args []Object, _ map[string]Object) (Object, error) {
			return rangeReverse(args[0])
		},
	))
}

// rangeReverse ports range_reverse: reversed(range(start, stop, step))
// is range(start+(n-1)*step, start-step, -step), where n is the range
// length. The result is a range_iterator, the same type iter(range)
// yields, so reversed() and iter() agree on type.
//
// CPython: Objects/rangeobject.c:1215 range_reverse
func rangeReverse(o Object) (Object, error) {
	r := o.(*Range)
	n := rangeLengthBig(&r.Start.v, &r.Stop.v, &r.Step.v)
	newStep := NewIntFromBig(new(big.Int).Neg(&r.Step.v))
	newStop := NewIntFromBig(new(big.Int).Sub(&r.Start.v, &r.Step.v))
	nm1 := new(big.Int).Sub(n, big.NewInt(1))
	prod := new(big.Int).Mul(nm1, &r.Step.v)
	newStart := NewIntFromBig(new(big.Int).Add(&r.Start.v, prod))
	return newRangeIterator(newStart, newStop, newStep), nil
}

// rangeBool ports range_bool: the truth value is whether the stored
// length is non-zero. Going through the length keeps bool(range(...))
// correct for ranges whose length overflows Py_ssize_t.
//
// CPython: Objects/rangeobject.c:743 range_bool
func rangeBool(o Object) (bool, error) {
	r := o.(*Range)
	n := rangeLengthBig(&r.Start.v, &r.Stop.v, &r.Step.v)
	return n.Sign() != 0, nil
}

// rangeHash ports range_hash. The hash is computed over a (length,
// start, step) tuple. An empty range substitutes None for start and
// step, and a length-1 range substitutes None for step, so ranges that
// compare equal under range_equals also hash equal.
//
// CPython: Objects/rangeobject.c:579 range_hash
func rangeHash(o Object) (int64, error) {
	r := o.(*Range)
	n := rangeLengthBig(&r.Start.v, &r.Stop.v, &r.Step.v)
	lenObj := NewIntFromBig(n)
	var t1, t2 Object
	switch {
	case n.Sign() == 0:
		t1, t2 = None(), None()
	case n.Cmp(big.NewInt(1)) == 0:
		t1, t2 = r.Start, None()
	default:
		t1, t2 = r.Start, r.Step
	}
	return Hash(NewTuple([]Object{lenObj, t1, t2}))
}

// rangeCount ports range_count: exact int / bool values resolve through
// the O(1) containment test (a range holds each value at most once);
// other objects fall back to the generic iterator search.
//
// CPython: Objects/rangeobject.c:616 range_count
func rangeCount(o, ob Object) (Object, error) {
	if rangeIsExactIntOrBool(ob) {
		in, err := rangeContains(o, ob)
		if err != nil {
			return nil, err
		}
		if in {
			return NewInt(1), nil
		}
		return NewInt(0), nil
	}
	n := 0
	it, err := Iter(o)
	if err != nil {
		return nil, err
	}
	for {
		x, err := IterNext(it)
		if err != nil {
			if errors.Is(err, ErrStopIteration) {
				break
			}
			return nil, err
		}
		eq, err := RichCmpBool(x, ob, CompareEQ)
		if err != nil {
			return nil, err
		}
		if eq {
			n++
		}
	}
	return NewInt(int64(n)), nil
}

// rangeIndex ports range_index. For an exact int / bool it computes the
// position arithmetically as (ob - start) // step when the value is in
// range, raising ValueError otherwise. Other objects fall back to an
// iterator search.
//
// CPython: Objects/rangeobject.c:634 range_index
func rangeIndex(o, ob Object) (Object, error) {
	r := o.(*Range)
	if !rangeIsExactIntOrBool(ob) {
		it, err := Iter(o)
		if err != nil {
			return nil, err
		}
		i := 0
		for {
			x, err := IterNext(it)
			if err != nil {
				if errors.Is(err, ErrStopIteration) {
					break
				}
				return nil, err
			}
			eq, err := RichCmpBool(x, ob, CompareEQ)
			if err != nil {
				return nil, err
			}
			if eq {
				return NewInt(int64(i)), nil
			}
			i++
		}
		return nil, fmt.Errorf("ValueError: %s is not in range", mustRepr(ob))
	}
	in, err := rangeContains(o, ob)
	if err != nil {
		return nil, err
	}
	if !in {
		return nil, errors.New("ValueError: range.index(x): x not in range")
	}
	bv := rangeIntValue(ob)
	idx := new(big.Int).Sub(bv, &r.Start.v)
	if r.Step.v.Cmp(big.NewInt(1)) == 0 {
		return NewIntFromBig(idx), nil
	}
	idx.Quo(idx, &r.Step.v)
	return NewIntFromBig(idx), nil
}

// rangeIsExactIntOrBool reports whether ob is an exact int (not a
// subclass) or a bool, matching CPython's PyLong_CheckExact ||
// PyBool_Check guard on the fast containment / index paths.
//
// CPython: Objects/rangeobject.c:616 range_count, :634 range_index
func rangeIsExactIntOrBool(ob Object) bool {
	switch v := ob.(type) {
	case *Bool:
		return true
	case *Int:
		return v.Type() == IntType
	}
	return false
}

// rangeIntValue returns the big.Int value of an exact int or bool.
func rangeIntValue(ob Object) *big.Int {
	switch v := ob.(type) {
	case *Bool:
		if v == True() {
			return big.NewInt(1)
		}
		return big.NewInt(0)
	case *Int:
		return &v.v
	}
	return big.NewInt(0)
}

// mustRepr returns repr(o) for error messages, falling back to the type
// name when repr itself fails.
func mustRepr(o Object) string {
	s, err := Repr(o)
	if err != nil {
		return o.Type().Name
	}
	return s
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
	idx, err := NumberIndex(key)
	if err != nil {
		return nil, err
	}
	return rangeItemBig(r, idx.(*Int).BigInt())
}

// rangeItemBig computes range[i] for an arbitrary-precision index,
// applying the negative-index wrap and bounds check entirely in
// math/big so subscripts beyond Py_ssize_t (e.g. range(-maxsize,
// maxsize)[maxsize+1]) stay exact.
//
// CPython: Objects/rangeobject.c:335 compute_range_item
func rangeItemBig(r *Range, i *big.Int) (Object, error) {
	lenBig := rangeLengthBig(&r.Start.v, &r.Stop.v, &r.Step.v)
	idx := new(big.Int).Set(i)
	if idx.Sign() < 0 {
		idx.Add(idx, lenBig)
	}
	if idx.Sign() < 0 || idx.Cmp(lenBig) >= 0 {
		return nil, fmt.Errorf("IndexError: range object index out of range")
	}
	return NewIntFromBig(computeRangeItem(r, idx)), nil
}

// rangeContains implements `x in r`. CPython has a fast path for
// PyLong/Bool that checks start/stop bounds and step divisibility; we
// mirror it for *Int and fall back to iteration otherwise.
//
// CPython: Objects/rangeobject.c:488 range_contains
func rangeContains(o, v Object) (bool, error) {
	r := o.(*Range)
	// CPython gates the arithmetic fast path on PyLong_CheckExact ||
	// PyBool_Check, so an int subclass (which may override __eq__) takes
	// the iterator search instead.
	//
	// CPython: Objects/rangeobject.c:466 range_contains
	if !rangeIsExactIntOrBool(v) {
		return rangeContainsIter(o, v)
	}
	bv := rangeIntValue(v)
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
