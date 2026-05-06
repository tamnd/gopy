package objects

import (
	"strings"
)

// Tuple is the Python tuple, an immutable ordered sequence.
//
// CPython: Include/cpython/tupleobject.h:L8 PyTupleObject
type Tuple struct {
	VarHeader
	items []Object
}

// TupleType is the type singleton for tuple. Mirrors PyTuple_Type.
//
// CPython: Objects/tupleobject.c:L961 PyTuple_Type
var TupleType = NewType("tuple", []*Type{objectType})

// emptyTuple is the cached empty tuple singleton, matching CPython's
// `() is ()` invariant.
//
// CPython: Objects/tupleobject.c:L23 _Py_SINGLETON(tuple_empty)
var emptyTuple *Tuple

func init() {
	TupleType.TpFlags = TpFlagSequence
	TupleType.Repr = tupleRepr
	TupleType.Str = tupleRepr
	TupleType.Hash = tupleHash
	TupleType.RichCmp = tupleRichCmp
	TupleType.Iter = tupleIter
	TupleType.Sequence = &SequenceMethods{
		Length:  tupleLen,
		GetItem: tupleGetItem,
	}
	TupleType.TpTraverse = tupleTraverse
	emptyTuple = &Tuple{}
	emptyTuple.init(TupleType)
}

// NewTuple builds a tuple from items. The empty tuple returns the
// cached singleton.
//
// CPython: Objects/tupleobject.c:L155 PyTuple_New
func NewTuple(items []Object) *Tuple {
	if len(items) == 0 {
		return emptyTuple
	}
	t := &Tuple{items: append([]Object(nil), items...)}
	t.init(TupleType)
	t.size = int64(len(items))
	return t
}

// Len returns the number of items.
//
// CPython: Objects/tupleobject.c:L296 PyTuple_Size
func (t *Tuple) Len() int { return len(t.items) }

// Item returns the item at index i. No bounds check; callers are
// expected to clamp.
//
// CPython: Objects/tupleobject.c:L319 PyTuple_GetItem
func (t *Tuple) Item(i int) Object { return t.items[i] }

func tupleLen(o Object) (int, error) {
	return o.(*Tuple).Len(), nil
}

// tupleTraverse visits every element. Mirrors tupletraverse.
//
// CPython: Objects/tupleobject.c:644 tupletraverse
func tupleTraverse(o Object, visit Visitor) error {
	t := o.(*Tuple)
	for _, it := range t.items {
		if it == nil {
			continue
		}
		if err := visit(it); err != nil {
			return err
		}
	}
	return nil
}

func tupleGetItem(o Object, i int) (Object, error) {
	t := o.(*Tuple)
	if i < 0 {
		i += len(t.items)
	}
	if i < 0 || i >= len(t.items) {
		return nil, errIndexOutOfRange
	}
	return t.items[i], nil
}

func tupleRepr(o Object) (string, error) {
	t := o.(*Tuple)
	if len(t.items) == 0 {
		return "()", nil
	}
	var b strings.Builder
	b.WriteByte('(')
	for i, it := range t.items {
		if i > 0 {
			b.WriteString(", ")
		}
		s, err := Repr(it)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
	}
	if len(t.items) == 1 {
		b.WriteByte(',')
	}
	b.WriteByte(')')
	return b.String(), nil
}

// tupleHash mirrors CPython's tuplehash: a 64-bit xxHash-style mix
// over the per-element hashes plus a length-dependent finalizer. The
// constants are taken straight from cpython/Objects/tupleobject.c so
// the output matches CPython byte for byte.
//
// CPython: Objects/tupleobject.c:L329 tuplehash
func tupleHash(o Object) (int64, error) {
	t := o.(*Tuple)
	const (
		p1 uint64 = 11400714785074694791
		p2 uint64 = 14029467366897019727
		p5 uint64 = 2870177450012600261
	)
	acc := p5
	for _, it := range t.items {
		h, err := Hash(it)
		if err != nil {
			return 0, err
		}
		lane := uint64(h)
		acc += lane * p2
		acc = (acc << 31) | (acc >> 33)
		acc *= p1
	}
	acc += uint64(len(t.items)) ^ (p5 ^ 3527539)
	if int64(acc) == -1 {
		return 1546275796, nil
	}
	return int64(acc), nil
}

func tupleRichCmp(a, b Object, op CompareOp) (Object, error) {
	at, ok := a.(*Tuple)
	if !ok {
		return notImplemented(), nil
	}
	bt, ok := b.(*Tuple)
	if !ok {
		return notImplemented(), nil
	}
	switch op {
	case CompareEQ, CompareNE:
		eq, err := tupleEq(at, bt)
		if err != nil {
			return nil, err
		}
		if op == CompareNE {
			eq = !eq
		}
		return NewBool(eq), nil
	}
	return notImplemented(), nil
}

func tupleEq(a, b *Tuple) (bool, error) {
	if len(a.items) != len(b.items) {
		return false, nil
	}
	for i := range a.items {
		eq, err := RichCmpBool(a.items[i], b.items[i], CompareEQ)
		if err != nil {
			return false, err
		}
		if !eq {
			return false, nil
		}
	}
	return true, nil
}

// tupleIterator is the iterator returned by iter(tuple).
//
// CPython: Objects/tupleobject.c:L1059 tupleiter_type
type tupleIterator struct {
	Header
	src *Tuple
	pos int
}

var tupleIterType = NewType("tuple_iterator", []*Type{objectType})

func init() {
	tupleIterType.IterNext = func(o Object) (Object, error) {
		it := o.(*tupleIterator)
		if it.pos >= len(it.src.items) {
			return nil, ErrStopIteration
		}
		v := it.src.items[it.pos]
		it.pos++
		return v, nil
	}
	tupleIterType.Iter = func(o Object) (Object, error) { return o, nil }
}

func tupleIter(o Object) (Object, error) {
	it := &tupleIterator{src: o.(*Tuple)}
	it.init(tupleIterType)
	return it, nil
}
