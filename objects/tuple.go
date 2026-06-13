package objects

import (
	"fmt"
	"strings"
	"sync/atomic"
)

// Tuple is the Python tuple, an immutable ordered sequence.
//
// CPython: Include/cpython/tupleobject.h:L8 PyTupleObject
type Tuple struct {
	VarHeader
	items     []Object
	attrDict  *Dict
	hashValid bool
	hashValue int64
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
	TupleType.TpFlags |= TpFlagSequence | TpFlagMatchSelf | TpFlagHaveGC
	TupleType.Repr = tupleRepr
	TupleType.Str = tupleRepr
	TupleType.Hash = tupleHash
	TupleType.RichCmp = tupleRichCmp
	// CPython: Objects/tupleobject.c:1055 PyTuple_Type.tp_richcompare slot wrapper
	BindRichCmpDescriptors(TupleType)
	TupleType.Iter = tupleIter
	TupleType.Sequence = &SequenceMethods{
		Length:   tupleLen,
		GetItem:  tupleGetItem,
		Concat:   tupleConcat,
		Repeat:   tupleRepeat,
		Contains: tupleContains,
	}
	// tuple_as_mapping carries mp_length and mp_subscript; the subscript
	// slot is what handles slice keys (sq_item is integer-only). With the
	// mapping present, GetItem(tuple, key) routes through tupleSubscript
	// first, matching PyObject_GetItem's mp_subscript precedence.
	//
	// CPython: Objects/tupleobject.c:886 tuple_as_mapping
	TupleType.Mapping = &MappingMethods{
		Length:  tupleLen,
		GetItem: tupleSubscript,
	}
	TupleType.TpTraverse = tupleTraverse
	// CPython: Objects/tupleobject.c:233 tuple_dealloc
	TupleType.Dealloc = tupleDealloc
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
	SetTypeDescr(TupleType, "__contains__", NewMethodDescr(TupleType, "__contains__", tupleContainsMethod))
	SetTypeDescr(TupleType, "__add__", NewMethodDescr(TupleType, "__add__", tupleAddMethod))
	SetTypeDescr(TupleType, "__mul__", NewMethodDescr(TupleType, "__mul__", tupleMulMethod))
	SetTypeDescr(TupleType, "__rmul__", NewMethodDescr(TupleType, "__rmul__", tupleMulMethod))
	SetTypeDescr(TupleType, "index", NewMethodDescr(TupleType, "index", tupleIndexMethod))
	SetTypeDescr(TupleType, "count", NewMethodDescrConv(TupleType, "count", MethO, tupleCountMethod))
	SetTypeDescr(TupleType, "__getnewargs__", NewMethodDescrConv(TupleType, "__getnewargs__", MethNoArgs, tupleGetNewArgsMethod))
	// CPython: Objects/typeobject.c:8230 slotdefs (TPSLOT __hash__)
	SetTypeDescr(TupleType, "__hash__", NewMethodDescr(TupleType, "__hash__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: __hash__() takes exactly 1 argument (%d given)", len(args))
		}
		h, err := tupleHash(args[0])
		if err != nil {
			return nil, err
		}
		return NewInt(h), nil
	}))
	// TpNew honors cls so `class T(tuple): pass; T((1,2))` returns a T
	// instance instead of a plain tuple. tuple is immutable, so unlike
	// list we populate items here rather than deferring to __init__.
	//
	// CPython: Objects/tupleobject.c:778 tuple_new_impl
	TupleType.TpNew = func(cls *Type, args []Object, kwargs map[string]Object) (Object, error) {
		// The clinic-generated tuple_new rejects keywords when
		// type == &PyTuple_Type OR the subtype has not overridden __init__
		// (type->tp_init == base_tp->tp_init). tuple itself inherits
		// object.__init__, so this means: a bare subclass still rejects
		// kwargs, but subclass_with_init(tuple) (which defines __init__)
		// may pass them through to __init__.
		//
		// CPython: Objects/clinic/tupleobject.c.h:96 tuple_new
		if len(kwargs) > 0 && (cls == TupleType || initInheritedFromObject(cls)) {
			return nil, fmt.Errorf("TypeError: tuple() takes no keyword arguments")
		}
		if len(args) > 1 {
			return nil, fmt.Errorf("TypeError: tuple expected at most 1 argument, got %d", len(args))
		}
		// tuple(some_exact_tuple) returns that very object: PySequence_Tuple
		// fast-paths PyTuple_CheckExact with a bare incref.
		//
		// CPython: Objects/abstract.c:2820 PySequence_Tuple
		if cls == TupleType && len(args) == 1 {
			if t, ok := args[0].(*Tuple); ok && t.Type() == TupleType {
				// Hand back a counted reference, not a borrow: the caller
				// owns the result and the argument it was built from is a
				// distinct stack temporary that the eval loop decrefs after
				// the call. Without the incref a bare temporary (tuple(x*2))
				// hits zero and tuple_dealloc nils its shared item slice.
				//
				// CPython: Objects/abstract.c:2820 PySequence_Tuple (Py_INCREF(v))
				Incref(t)
				return t, nil
			}
		}
		var items []Object
		if len(args) == 1 {
			drained, err := DrainIterable(args[0])
			if err != nil {
				return nil, err
			}
			items = drained
		}
		if cls == TupleType && len(items) == 0 {
			return emptyTuple, nil
		}
		// The tuple takes a counted reference on every element it stores.
		// gopy's list and dict eagerly release their contents on teardown
		// (listDealloc, ReleaseDeadDictContents), so any object that is also
		// reachable through one of those containers would be over-decref'd if
		// the tuple borrowed it. Owning the reference keeps the shared object
		// alive for as long as the tuple does. No matching teardown decref is
		// wired: a blanket tuple tp_dealloc is unsafe (tuples back co_consts
		// and other un-counted Go fields whose Python refcount sits at zero
		// while live), so the reference is intentionally leaked and reclaimed
		// by the Go GC, matching the over-incref bias the refcount layer uses.
		//
		// DrainIterable already handed back one owned reference per element,
		// so the tuple steals those rather than taking a second count. An
		// empty subtype build (tuple() on a subclass, items==nil) has nothing
		// to steal.
		//
		// CPython: Objects/tupleobject.c:784 tuple_new_impl (PyTuple_SET_ITEM steals an owned ref)
		t := &Tuple{items: items}
		t.init(cls)
		t.size = int64(len(items))
		// PyObject_GC_Track tail of tuple_new_impl: subtypes are heap GC
		// types (always tracked), exact tuples track only when an item's
		// type participates in GC (gh-139951).
		//
		// CPython: Objects/tupleobject.c:784 tuple_new_impl (GC_Track)
		gcTrackTupleOnCreate(t, cls)
		return t, nil
	}
	emptyTuple = &Tuple{}
	emptyTuple.init(TupleType)
	// The empty tuple is a process-wide singleton handed back by every
	// NewTuple(nil) and PyTuple_New(0). Stamp it immortal so the throwaway
	// argument tuples that dunder dispatchers build and release (a bound
	// __hash__/__repr__ call carries no positionals, so NewTuple(nil)
	// returns this singleton) cannot drive its refcount toward zero.
	// CPython: Objects/tupleobject.c the static empty tuple is immortal.
	emptyTuple.Hdr().MakeImmortal()
	// CPython: Objects/typeobject.c add_operators slotdefs tp_iter row
	AddIterSlotWrappers(TupleType)
}

// tupleDealloc fires when a tuple's Python refcount reaches zero. Like
// list_dealloc it releases the counted reference the tuple holds on each
// element (tail-first), so a contained object whose last holder is this
// tuple gets its own __del__ run deterministically. A subclass __del__
// (tp_finalize) runs first under a resurrection guard, matching
// listDealloc / set_dealloc.
//
// This balances the incref-on-store in tuple_new / NewTuple. It is only
// safe because every tuple reachable through an un-counted Go field
// (co_consts on a Code object, a function's __defaults__/__kwdefaults__,
// constant tuples cached in a type __dict__) is pinned with an explicit
// Incref by its holder, so such a tuple never sits at refcount zero while
// it is still live. A transient tuple (BUILD_TUPLE result, call argument
// pack, items() pair) is owned by an honest refcount and tears down here.
//
// CPython: Objects/tupleobject.c:233 tuple_dealloc
func tupleDealloc(o Object) {
	t := o.(*Tuple)
	if t == emptyTuple {
		return
	}
	if fn := o.Type().Finalize; fn != nil {
		h := o.Hdr()
		atomic.StoreInt64(&h.refcnt, 1)
		fn(o)
		atomic.AddInt64(&h.refcnt, -1)
		if atomic.LoadInt64(&h.refcnt) != 0 {
			return
		}
	}
	if h := GCUntrackHook; h != nil {
		h(o)
	}
	ClearWeakRefs(o)
	// Tail-first item release mirrors tuple_dealloc's reversed walk.
	for i := len(t.items) - 1; i >= 0; i-- {
		it := t.items[i]
		t.items[i] = nil
		if it != nil {
			Decref(it)
		}
	}
	t.items = nil
}

// gcTrackTupleOnCreate decides whether a freshly built tuple is handed to
// the cyclic collector at creation time, mirroring gh-139951: an exact
// tuple is tracked iff at least one item's type participates in GC
// (_PyType_IS_GC), so atom-only tuples like (1, 2) are never tracked and
// gc.is_tracked reports False immediately. A tuple subtype is a heap type
// and is always tracked, matching tp_alloc's unconditional track for GC
// types.
//
// CPython: Objects/tupleobject.c PyTuple_New (gh-139951 conditional track),
// Objects/typeobject.c type_new (heap subtypes are GC-tracked)
func gcTrackTupleOnCreate(t *Tuple, cls *Type) {
	h := GCTrackHook
	if h == nil {
		return
	}
	track := cls != TupleType
	if !track {
		for _, it := range t.items {
			if it != nil && it.Type().HasGC() {
				track = true
				break
			}
		}
	}
	if track {
		h(t)
	}
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
	// Own a counted reference per stored item, balancing the eager content
	// release that list and dict perform on teardown so a shared object is
	// never over-decref'd while this tuple still references it. tupleDealloc
	// drops one reference per item on the last Decref, so a tuple released
	// through the refcount path (rather than abandoned to the Go GC) returns
	// each item to its prior count.
	//
	// CPython: Objects/tupleobject.c:163 PyTuple_New + the PyTuple_SET_ITEM contract
	for _, it := range t.items {
		if it != nil {
			Incref(it)
		}
	}
	// CPython: Objects/tupleobject.c:170 PyTuple_New (PyObject_GC_Track tail)
	gcTrackTupleOnCreate(t, TupleType)
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

// tupleItemsOf returns the element slice backing a tuple or any tuple
// subclass instance. CPython stores PyStructSequence data in the inherited
// PyTupleObject ob_item, so every tuple sequence slot reads a subclass
// through the same field. gopy models structseq as a separate Go struct, so
// the tuple slots extract the slice through this helper rather than an
// unchecked *Tuple cast, which would panic on a *StructSeq.
//
// CPython: Objects/tupleobject.c PyTuple_Check + ob_item access
func tupleItemsOf(o Object) ([]Object, bool) {
	switch t := o.(type) {
	case *Tuple:
		return t.items, true
	case *StructSeq:
		return t.visible(), true
	}
	return nil, false
}

// isTupleOrSubclass mirrors PyTuple_Check: true for an exact tuple or any
// tuple subclass instance (structseq).
func isTupleOrSubclass(o Object) bool {
	_, ok := tupleItemsOf(o)
	return ok
}

func tupleLen(o Object) (int, error) {
	items, _ := tupleItemsOf(o)
	return len(items), nil
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
	return visitAttrDict(o, visit)
}

func tupleGetItem(o Object, i int) (Object, error) {
	items, _ := tupleItemsOf(o)
	if i < 0 {
		i += len(items)
	}
	if i < 0 || i >= len(items) {
		return nil, errIndexOutOfRange
	}
	return items[i], nil
}

// tupleSubscript ports tuple_subscript (mp_subscript): an index-like key
// goes through tupleGetItem, a slice key returns a fresh tuple of the
// selected region, and anything else raises the same TypeError CPython
// emits.
//
// CPython: Objects/tupleobject.c:811 tuple_subscript
func tupleSubscript(o, key Object) (Object, error) {
	items, _ := tupleItemsOf(o)
	if s, ok := key.(*Slice); ok {
		start, _, step, slicelen, err := s.GetIndices(len(items))
		if err != nil {
			return nil, err
		}
		if slicelen <= 0 {
			return emptyTuple, nil
		}
		out := make([]Object, slicelen)
		for i, idx := 0, start; i < slicelen; i, idx = i+1, idx+step {
			out[i] = items[idx]
		}
		return NewTuple(out), nil
	}
	idx, err := indexValueAsInt(key, "tuple")
	if err != nil {
		return nil, err
	}
	return tupleGetItem(o, idx)
}

// tupleContains ports tuple_contains (sq_contains): linear scan with
// RichCmpBool, needle on the left.
//
// CPython: Objects/tupleobject.c:692 tuple_contains
func tupleContains(o, v Object) (bool, error) {
	items, _ := tupleItemsOf(o)
	for _, item := range items {
		// tuple_contains compares the element on the left and the needle
		// on the right: PyObject_RichCompareBool(item, el, Py_EQ).
		eq, err := RichCmpBool(item, v, CompareEQ)
		if err != nil {
			return false, err
		}
		if eq {
			return true, nil
		}
	}
	return false, nil
}

// tupleContainsMethod backs tuple.__contains__.
func tupleContainsMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __contains__() takes exactly one argument (%d given)", len(args)-1)
	}
	if !isTupleOrSubclass(args[0]) {
		return nil, fmt.Errorf("TypeError: descriptor '__contains__' requires a 'tuple' object")
	}
	found, err := tupleContains(args[0], args[1])
	if err != nil {
		return nil, err
	}
	return NewBool(found), nil
}

func tupleGetItemMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __getitem__() takes exactly one argument (%d given)", len(args)-1)
	}
	if !isTupleOrSubclass(args[0]) {
		return nil, fmt.Errorf("TypeError: descriptor '__getitem__' requires a 'tuple' object")
	}
	return tupleSubscript(args[0], args[1])
}

// tupleAddMethod backs tuple.__add__. Returns NotImplemented when other
// is not a tuple, matching tuple_concat's PyTuple_Check guard.
//
// CPython: Objects/tupleobject.c:559 tuple_concat
func tupleAddMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __add__() takes exactly one argument (%d given)", len(args)-1)
	}
	if !isTupleOrSubclass(args[1]) {
		return NotImplemented(), nil
	}
	return tupleConcat(args[0], args[1])
}

// tupleMulMethod backs tuple.__mul__ / tuple.__rmul__. n is coerced via
// PyNumber_Index.
//
// CPython: Objects/tupleobject.c:606 tuple_repeat
func tupleMulMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __mul__() takes exactly one argument (%d given)", len(args)-1)
	}
	idx, err := NumberIndex(args[1])
	if err != nil {
		return NotImplemented(), nil //nolint:nilerr // mirrors Py_NotImplemented return when other can't be coerced to an index
	}
	n, ok := idx.(*Int)
	if !ok {
		return NotImplemented(), nil
	}
	v, ok := n.Int64()
	if !ok {
		return nil, fmt.Errorf("OverflowError: cannot fit 'int' into an index-sized integer")
	}
	return tupleRepeat(args[0], int(v))
}

func tupleLenMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __len__() takes no arguments (%d given)", len(args)-1)
	}
	items, ok := tupleItemsOf(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__len__' requires a 'tuple' object")
	}
	return NewInt(int64(len(items))), nil
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
// The result is cached in t.hashValue after the first computation,
// matching CPython's ob_hash caching in PyTupleObject so that repeated
// hash(some_tuple) calls do not re-invoke element __hash__ methods.
//
// CPython: Objects/tupleobject.c:329 tuplehash (ob_hash cache)
func tupleHash(o Object) (int64, error) {
	t := o.(*Tuple)
	if t.hashValid {
		return t.hashValue, nil
	}
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
	result := int64(acc)
	if result == -1 {
		result = 1546275796
	}
	t.hashValid = true
	t.hashValue = result
	return result, nil
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
	aitems, _ := tupleItemsOf(a)
	bitems, ok := tupleItemsOf(b)
	if !ok {
		return nil, fmt.Errorf("TypeError: can only concatenate tuple (not %q) to tuple", typeNameOf(b))
	}
	out := make([]Object, 0, len(aitems)+len(bitems))
	out = append(out, aitems...)
	out = append(out, bitems...)
	return NewTuple(out), nil
}

// tupleRepeat is sq_repeat for tuple: t * n builds a new tuple. Two
// short circuits mirror CPython tuple_repeat: (a) for an exact tuple
// (not a subclass), n==1 or input==0 returns the same object since
// tuples are immutable; (b) otherwise n<=0 returns the singleton empty
// tuple. Multiplying past PY_SSIZE_T_MAX raises MemoryError.
//
// CPython: Objects/tupleobject.c:533 tuple_repeat
func tupleRepeat(o Object, n int) (rv Object, rerr error) {
	items, _ := tupleItemsOf(o)
	input := len(items)
	if input == 0 || n == 1 {
		if o.Type() == TupleType {
			return o, nil
		}
	}
	if input == 0 || n <= 0 {
		return emptyTuple, nil
	}
	if input > int(^uint(0)>>1)/n {
		return nil, fmt.Errorf("MemoryError")
	}
	defer func() {
		if r := recover(); r != nil {
			rv = nil
			rerr = fmt.Errorf("MemoryError")
		}
	}()
	out := make([]Object, 0, input*n)
	for i := 0; i < n; i++ {
		out = append(out, items...)
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

// tupleIterNext yields the next item, clearing the source reference on
// exhaustion so the tuple (and its contents) can be released right away.
//
// CPython: Objects/tupleobject.c:1073 tupleiter_next
func tupleIterNext(o Object) (Object, error) {
	it := o.(*tupleIterator)
	if it.src == nil || it.pos >= len(it.src.items) {
		if it.src != nil {
			old := it.src
			it.src = nil
			Decref(old)
		}
		return nil, ErrStopIteration
	}
	v := it.src.items[it.pos]
	it.pos++
	return v, nil
}

// tupleIterDealloc drops the iterator's reference on the source tuple if
// exhaustion has not already cleared it.
//
// CPython: Objects/tupleobject.c:1063 tupleiter_dealloc
func tupleIterDealloc(o Object) {
	if h := GCUntrackHook; h != nil {
		h(o)
	}
	ClearWeakRefs(o)
	it := o.(*tupleIterator)
	if it.src != nil {
		Decref(it.src)
		it.src = nil
	}
}

func init() {
	tupleIterType.IterNext = tupleIterNext
	tupleIterType.Dealloc = tupleIterDealloc
	tupleIterType.Iter = SelfIter
	AddIterSlotWrappers(tupleIterType)
	// CPython: Objects/tupleobject.c:1132 tupleiter_reduce
	SetTypeDescr(tupleIterType, "__reduce__", NewMethodDescrConv(tupleIterType, "__reduce__", MethNoArgs,
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
	SetTypeDescr(tupleIterType, "__setstate__", NewMethodDescrConv(tupleIterType, "__setstate__", MethO,
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
	// CPython: Objects/tupleobject.c:1115 tupleiter_len
	SetTypeDescr(tupleIterType, "__length_hint__", NewMethodDescrConv(tupleIterType, "__length_hint__", MethNoArgs,
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __length_hint__ takes no arguments")
			}
			it := args[0].(*tupleIterator)
			if it.src == nil {
				return NewInt(0), nil
			}
			n := len(it.src.items) - it.pos
			if n < 0 {
				n = 0
			}
			return NewInt(int64(n)), nil
		},
	))
}

func tupleIter(o Object) (Object, error) {
	t, ok := o.(*Tuple)
	if !ok {
		// A tuple subclass (structseq) reaching the inherited tp_iter:
		// snapshot its items into a plain tuple to drive the iterator,
		// matching CPython where the iterator holds the tuple storage.
		items, isTuple := tupleItemsOf(o)
		if !isTuple {
			return nil, fmt.Errorf("TypeError: 'iter()' requires a tuple, not '%s'", typeNameOf(o))
		}
		t = NewTuple(items)
	}
	// tupleiter holds a counted reference on the tuple (it_seq), matching
	// CPython where PyObject_GetIter does Py_INCREF on the sequence. The
	// matching Decref runs on exhaustion or in tupleiter_dealloc.
	//
	// CPython: Objects/tupleobject.c:1091 tuple_iter
	Incref(t)
	it := &tupleIterator{src: t}
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
	items, ok := tupleItemsOf(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'index' requires a 'tuple' object")
	}
	target := args[1]
	n := len(items)
	start := 0
	stop := n
	// start / stop go through the slice_index clinic converter, which
	// clamps values outside the ssize_t range (e.g. -4*sys.maxsize) to
	// the platform min / max rather than raising, so a huge window still
	// scans the whole tuple.
	//
	// CPython: Python/ceval.c:1786 _PyEval_SliceIndex
	if len(args) >= 3 {
		s, err := sliceIndex(args[2])
		if err != nil {
			return nil, err
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
		s, err := sliceIndex(args[3])
		if err != nil {
			return nil, err
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
		eq, err := RichCmpBool(items[i], target, CompareEQ)
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
	items, ok := tupleItemsOf(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'count' requires a 'tuple' object")
	}
	target := args[1]
	count := 0
	for _, item := range items {
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

// tupleGetNewArgsMethod implements tuple.__getnewargs__. It returns a
// 1-tuple holding a plain-tuple copy of self's items so a tuple subclass
// can round-trip through pickle: copyreg builds __newobj__(cls, *args)
// where args is this getnewargs result, reconstructing the subclass with
// its original contents.
//
// CPython: Objects/tupleobject.c:790 tuple___getnewargs___impl
func tupleGetNewArgsMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __getnewargs__() takes no arguments (%d given)", len(args)-1)
	}
	items, ok := tupleItemsOf(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__getnewargs__' requires a 'tuple' object")
	}
	cp := make([]Object, len(items))
	copy(cp, items)
	return NewTuple([]Object{NewTuple(cp)}), nil
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
		if it.src != nil {
			old := it.src
			it.src = nil
			Decref(old)
		}
		return nil, true, true
	}
	v := it.src.items[it.pos]
	it.pos++
	return v, false, true
}
