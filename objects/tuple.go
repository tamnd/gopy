package objects

import (
	"fmt"
	"strings"
)

// Tuple is the Python tuple, an immutable ordered sequence.
//
// CPython: Include/cpython/tupleobject.h:L8 PyTupleObject
type Tuple struct {
	VarHeader
	items    []Object
	attrDict *Dict
}

// AttrDict returns the per-instance attribute dict, or nil if no
// attribute has been set yet. Only meaningful for tuple subclasses;
// plain tuple does not advertise HasDict so this is never consulted.
//
// CPython: Objects/tupleobject.c subtype dict (managed via tp_dictoffset)
func (t *Tuple) AttrDict() *Dict { return t.attrDict }

// EnsureAttrDict allocates the per-instance dict on first store.
func (t *Tuple) EnsureAttrDict() *Dict {
	if t.attrDict == nil {
		t.attrDict = NewDict()
	}
	return t.attrDict
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
	TupleType.TpFlags |= TpFlagSequence | TpFlagMatchSelf
	TupleType.Repr = tupleRepr
	TupleType.Str = tupleRepr
	TupleType.Hash = tupleHash
	TupleType.RichCmp = tupleRichCmp
	TupleType.Iter = tupleIter
	TupleType.Sequence = &SequenceMethods{
		Length:  tupleLen,
		GetItem: tupleGetItem,
		Concat:  tupleConcat,
		Repeat:  tupleRepeat,
	}
	TupleType.TpTraverse = tupleTraverse
	TupleType.Getattro = GenericGetAttr
	// __repr__ slot wrapper. add_operators exposes tp_repr as a
	// distinct descriptor so callers can do `tuple.__repr__(t)` and so
	// pprint's dispatch table can key on it instead of collapsing onto
	// object.__repr__.
	//
	// CPython: Objects/typeobject.c add_operators slot wrapper for tp_repr
	SetTypeDescr(TupleType, "__repr__", NewMethodDescr(TupleType, "__repr__", tupleReprMethod))
	// __getitem__ / __len__ slot wrappers. Until add_operators is
	// ported wholesale (task #647) callers like dis.py that grab
	// `tuple.__getitem__` directly need this exposed by hand.
	//
	// CPython: Objects/typeobject.c add_operators slot wrapper for sq_item / sq_length
	SetTypeDescr(TupleType, "__getitem__", NewMethodDescr(TupleType, "__getitem__", tupleGetItemMethod))
	SetTypeDescr(TupleType, "__len__", NewMethodDescr(TupleType, "__len__", tupleLenMethod))
	SetTypeDescr(TupleType, "index", NewMethodDescr(TupleType, "index", tupleIndexMethod))
	SetTypeDescr(TupleType, "count", NewMethodDescr(TupleType, "count", tupleCountMethod))
	// TpNew honors cls so `class T(tuple): pass; T((1,2))` returns a T
	// instance instead of a plain tuple. tuple is immutable, so unlike
	// list we populate items here rather than deferring to __init__.
	//
	// CPython: Objects/tupleobject.c:778 tuple_new_impl
	TupleType.TpNew = func(cls *Type, args []Object, kwargs map[string]Object) (Object, error) {
		if len(args) > 1 {
			return nil, fmt.Errorf("TypeError: tuple expected at most 1 argument, got %d", len(args))
		}
		var items []Object
		if len(args) == 1 {
			drained, err := drainIterableForSlice(args[0])
			if err != nil {
				return nil, err
			}
			items = drained
		}
		if cls == TupleType && len(items) == 0 {
			return emptyTuple, nil
		}
		t := &Tuple{items: items}
		t.init(cls)
		t.size = int64(len(items))
		return t, nil
	}
	emptyTuple = &Tuple{}
	emptyTuple.init(TupleType)
	// CPython: Objects/typeobject.c add_operators slotdefs tp_iter row
	AddIterSlotWrappers(TupleType)
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

func tupleGetItemMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __getitem__() takes exactly one argument (%d given)", len(args)-1)
	}
	t, ok := args[0].(*Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__getitem__' requires a 'tuple' object")
	}
	return GetItem(t, args[1])
}

func tupleLenMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __len__() takes no arguments (%d given)", len(args)-1)
	}
	t, ok := args[0].(*Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__len__' requires a 'tuple' object")
	}
	return NewInt(int64(t.Len())), nil
}

func tupleReprMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __repr__() takes no arguments (%d given)", len(args)-1)
	}
	t, ok := args[0].(*Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__repr__' requires a 'tuple' object")
	}
	s, err := tupleRepr(t)
	if err != nil {
		return nil, err
	}
	return NewStr(s), nil
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

// tupleRichCmp ports tuplerichcompare: EQ/NE compare length+element-wise,
// LT/LE/GT/GE compare lexicographically, returning the result of the
// first differing pair's comparison or the length comparison when one
// tuple is a prefix of the other.
//
// CPython: Objects/tupleobject.c:L703 tuplerichcompare
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
	// Find the first differing slot, then return the result of that
	// element's comparison for op. If one tuple is a prefix of the
	// other, fall back to comparing lengths.
	la, lb := len(at.items), len(bt.items)
	n := la
	if lb < n {
		n = lb
	}
	for i := 0; i < n; i++ {
		eq, err := RichCmpBool(at.items[i], bt.items[i], CompareEQ)
		if err != nil {
			return nil, err
		}
		if !eq {
			return RichCmp(at.items[i], bt.items[i], op)
		}
	}
	switch op {
	case CompareLT:
		return NewBool(la < lb), nil
	case CompareLE:
		return NewBool(la <= lb), nil
	case CompareGT:
		return NewBool(la > lb), nil
	case CompareGE:
		return NewBool(la >= lb), nil
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

// tupleConcat is sq_concat for tuple: a + b builds a new tuple.
//
// CPython: Objects/tupleobject.c:L625 tupleconcat
func tupleConcat(a, b Object) (Object, error) {
	at := a.(*Tuple)
	bt, ok := b.(*Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: can only concatenate tuple (not %q) to tuple", typeNameOf(b))
	}
	out := make([]Object, 0, len(at.items)+len(bt.items))
	out = append(out, at.items...)
	out = append(out, bt.items...)
	return NewTuple(out), nil
}

// tupleRepeat is sq_repeat for tuple: t * n builds a new tuple.
//
// CPython: Objects/tupleobject.c:L641 tuplerepeat
func tupleRepeat(o Object, n int) (Object, error) {
	t := o.(*Tuple)
	if n <= 0 || len(t.items) == 0 {
		return emptyTuple, nil
	}
	out := make([]Object, 0, len(t.items)*n)
	for i := 0; i < n; i++ {
		out = append(out, t.items...)
	}
	return NewTuple(out), nil
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
		if it.src == nil || it.pos >= len(it.src.items) {
			it.src = nil
			return nil, ErrStopIteration
		}
		v := it.src.items[it.pos]
		it.pos++
		return v, nil
	}
	tupleIterType.Iter = func(o Object) (Object, error) { return o, nil }
	AddIterSlotWrappers(tupleIterType)
	// CPython: Objects/tupleobject.c:1132 tupleiter_reduce
	SetTypeDescr(tupleIterType, "__reduce__", NewMethodDescr(tupleIterType, "__reduce__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments")
			}
			it := args[0].(*tupleIterator)
			if BuiltinLookup == nil {
				return nil, fmt.Errorf("PicklingError: builtins not loaded")
			}
			iterFn, err := BuiltinLookup("iter")
			if err != nil {
				return nil, err
			}
			if it.src == nil {
				return NewTuple([]Object{iterFn, NewTuple([]Object{NewTuple(nil)})}), nil
			}
			return NewTuple([]Object{iterFn, NewTuple([]Object{it.src}), NewInt(int64(it.pos))}), nil
		},
	))
	// CPython: Objects/tupleobject.c:1148 tupleiter_setstate
	SetTypeDescr(tupleIterType, "__setstate__", NewMethodDescr(tupleIterType, "__setstate__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __setstate__() takes exactly one argument")
			}
			it := args[0].(*tupleIterator)
			idx, ok := args[1].(*Int)
			if !ok {
				return nil, fmt.Errorf("TypeError: __setstate__ requires int argument")
			}
			n := int64(0)
			if it.src != nil {
				n = int64(len(it.src.items))
			}
			v, _ := idx.Int64()
			if v < 0 {
				v = 0
			} else if v > n {
				v = n
			}
			it.pos = int(v)
			return None(), nil
		},
	))
}

func tupleIter(o Object) (Object, error) {
	it := &tupleIterator{src: o.(*Tuple)}
	it.init(tupleIterType)
	return it, nil
}

// tupleIndexMethod ports tuple.index(value, [start, [stop]]): returns
// the first index of value, raising ValueError if not present.
//
// CPython: Objects/tupleobject.c:736 tupleindex
func tupleIndexMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: index() takes at least 1 argument")
	}
	t, ok := args[0].(*Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'index' requires a 'tuple' object")
	}
	target := args[1]
	n := len(t.items)
	start := 0
	stop := n
	if len(args) >= 3 {
		s, err := toGoInt(args[2])
		if err != nil {
			return nil, fmt.Errorf("TypeError: 'start' must be an integer")
		}
		start = s
		if start < 0 {
			start += n
			if start < 0 {
				start = 0
			}
		}
	}
	if len(args) >= 4 {
		s, err := toGoInt(args[3])
		if err != nil {
			return nil, fmt.Errorf("TypeError: 'stop' must be an integer")
		}
		stop = s
		if stop < 0 {
			stop += n
			if stop < 0 {
				stop = 0
			}
		}
	}
	if stop > n {
		stop = n
	}
	for i := start; i < stop; i++ {
		eq, err := RichCmpBool(t.items[i], target, CompareEQ)
		if err != nil {
			return nil, err
		}
		if eq {
			return NewInt(int64(i)), nil
		}
	}
	return nil, fmt.Errorf("ValueError: tuple.index(x): x not in tuple")
}

// tupleCountMethod ports tuple.count(value): counts occurrences.
//
// CPython: Objects/tupleobject.c:777 tuplecount
func tupleCountMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: count() takes exactly one argument (%d given)", len(args)-1)
	}
	t, ok := args[0].(*Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'count' requires a 'tuple' object")
	}
	target := args[1]
	count := 0
	for _, item := range t.items {
		eq, err := RichCmpBool(item, target, CompareEQ)
		if err != nil {
			return nil, err
		}
		if eq {
			count++
		}
	}
	return NewInt(int64(count)), nil
}

// toGoInt converts an Object to a Go int for use as a sequence index.
func toGoInt(o Object) (int, error) {
	if i, ok := o.(*Int); ok {
		n, _ := i.Int64()
		return int(n), nil
	}
	return 0, fmt.Errorf("not an integer")
}

// TupleIterNextFast advances o as a tuple_iterator without going
// through the type-table tp_iternext indirection. Returns the next
// value or (nil, true) on exhaustion. ok=false means o was not
// exactly a tuple_iterator and the FOR_ITER_TUPLE fast arm must deopt.
//
// On exhaustion the function nulls it.src so a re-entered FOR_ITER on
// the dead iterator releases its grip on the source tuple, mirroring
// CPython's `it->it_seq = NULL; Py_DECREF(seq);` in _ITER_JUMP_TUPLE.
//
// CPython: Python/bytecodes.c _ITER_CHECK_TUPLE + _ITER_JUMP_TUPLE + _ITER_NEXT_TUPLE
func TupleIterNextFast(o Object) (value Object, exhausted bool, ok bool) {
	it, asserted := o.(*tupleIterator)
	if !asserted || it.Type() != tupleIterType {
		return nil, false, false
	}
	if it.src == nil || it.pos >= len(it.src.items) {
		it.src = nil
		return nil, true, true
	}
	v := it.src.items[it.pos]
	it.pos++
	return v, false, true
}
