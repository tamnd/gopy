package objects

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"
)

// List is the Python list, a mutable ordered sequence.
//
// CPython: Include/cpython/listobject.h:L7 PyListObject
type List struct {
	VarHeader
	items []Object
	// attrs holds instance attributes for list subclass objects. Nil
	// for plain list instances; allocated lazily by EnsureAttrDict on
	// first store. Mirrors CPython's tp_dictoffset on list subclasses
	// (which type_new_descriptors stamps when the subclass picks up
	// __dict__ from object).
	attrs *Dict
}

// AttrDict implements AttrDictHolder so list subclasses can carry
// instance attributes through generic_attr's HasDict path.
//
// CPython: Objects/object.c _PyObject_GetDictPtr (list-subclass path)
func (l *List) AttrDict() *Dict { return l.attrs }

// EnsureAttrDict allocates the attrs dict on first use.
func (l *List) EnsureAttrDict() *Dict {
	if l.attrs == nil {
		l.attrs = NewDict()
	}
	return l.attrs
}

// ListType is the type singleton for list. Mirrors PyList_Type.
//
// CPython: Objects/listobject.c:L3380 PyList_Type
var ListType = NewType("list", []*Type{objectType})

func init() {
	ListType.TpFlags |= TpFlagSequence | TpFlagMatchSelf
	ListType.Repr = listRepr
	ListType.Str = listRepr
	ListType.RichCmp = listRichCmp
	// CPython: Objects/listobject.c:3307 PyList_Type.tp_richcompare slot wrapper
	BindRichCmpDescriptors(ListType)
	ListType.Iter = listIter
	ListType.Sequence = &SequenceMethods{
		Length:        listLen,
		Concat:        listConcat,
		Repeat:        listRepeat,
		GetItem:       listGetItem,
		SetItem:       listSetItem,
		InPlaceConcat: listInPlaceConcat,
		InPlaceRepeat: listInPlaceRepeat,
		Contains:      listContains,
	}
	// Mapping protocol entries for list mirror CPython's
	// list_subscript / list_ass_subscript, which is what handles
	// list[a:b] and del list[a:b] alongside the integer cases.
	//
	// CPython: Objects/listobject.c:3216 list_as_mapping
	ListType.Mapping = &MappingMethods{
		Length:  listLen,
		GetItem: listMappingGet,
		SetItem: listMappingSet,
		DelItem: listMappingDel,
	}
	ListType.TpTraverse = listTraverse
	// CPython: Objects/listobject.c:2841 list_dealloc
	ListType.Dealloc = listDealloc
	// TpNew allocates a bare *List bound to the requested class so
	// `class S(list): pass; S()` returns an S instance rather than a
	// plain list. Population happens in __init__ (wired in
	// builtins/ctor.go via bindListCtor), matching CPython's split of
	// list_new (allocate) and list___init___impl (populate).
	//
	// CPython: Objects/listobject.c:2855 list_new
	ListType.TpNew = func(cls *Type, args []Object, kwargs map[string]Object) (Object, error) {
		l := &List{}
		l.init(cls)
		return l, nil
	}
	// CPython: Objects/typeobject.c add_operators slotdefs tp_iter row
	AddIterSlotWrappers(ListType)
	// CPython: Objects/listobject.c:3373 list.__hash__ = None
	SetTypeDescr(ListType, "__hash__", None())
}

// listDealloc fires when the list's Python refcount reaches zero. For
// subclasses that define __del__ (tp_finalize), the finalizer runs first
// under a resurrection guard, matching set_dealloc's structure. Then the
// list releases its reference on every item, tail-first like CPython's
// list_dealloc, so a contained object whose last holder is this list has
// its own __del__ fired deterministically.
//
// Unlike the generic Decref dance, listDealloc runs the finalizer inline
// here rather than delegating, so the per-item Decref below cannot
// re-enter and double-fire the list's own __del__.
//
// CPython: Objects/listobject.c:2841 list_dealloc
func listDealloc(o Object) {
	l := o.(*List)
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
	// Tail-first item release mirrors list_dealloc's reversed walk.
	for i := len(l.items) - 1; i >= 0; i-- {
		it := l.items[i]
		l.items[i] = nil
		if it != nil {
			Decref(it)
		}
	}
	l.items = nil
	l.size = 0
}

// listTraverse visits every item. Mirrors list_traverse.
//
// CPython: Objects/listobject.c:2829 list_traverse
func listTraverse(o Object, visit Visitor) error {
	l := o.(*List)
	for _, it := range l.items {
		if it == nil {
			continue
		}
		if err := visit(it); err != nil {
			return err
		}
	}
	return visitAttrDict(o, visit)
}

// NewList builds a list from items. The slice is copied so callers can
// keep using or mutating the input.
//
// CPython: Objects/listobject.c:L156 PyList_New
func NewList(items []Object) *List {
	l := &List{items: append([]Object(nil), items...)}
	l.init(ListType)
	l.size = int64(len(items))
	// The list owns one counted reference per stored item. CPython fills
	// the freshly allocated vector with PyList_SET_ITEM, which steals; the
	// gopy callers hand NewList a slice of borrowed objects, so the list
	// takes its own reference here and listDealloc releases one per item on
	// teardown. This is what makes a contained __del__ fire deterministically
	// when the list is the last holder.
	//
	// CPython: Objects/listobject.c:271 PyList_SET_ITEM (steal into slots)
	for _, it := range l.items {
		if it != nil {
			Incref(it)
		}
	}
	// CPython: Objects/listobject.c:159 PyList_New (PyObject_GC_Track tail)
	if h := GCTrackHook; h != nil {
		h(l)
	}
	return l
}

// newListAdopt wraps an already-fresh items slice as a *List without a
// defensive copy. The caller must not reuse items after the call.
// CPython's PyList_New(size) allocates the item vector exactly once;
// callers that build the vector inline can hand it over instead of
// going through NewList's copy path.
//
// CPython: Objects/listobject.c:L235 PyList_New (single-allocation path)
func newListAdopt(items []Object) *List {
	l := &List{items: items}
	l.init(ListType)
	l.size = int64(len(items))
	// Same ownership contract as NewList: the list takes a counted
	// reference per item. newListAdopt only differs in skipping the
	// defensive slice copy; the items themselves are still borrowed by the
	// caller (built by copying l.items or appending), so they are increfed
	// here just like NewList.
	//
	// CPython: Objects/listobject.c:271 PyList_SET_ITEM (steal into slots)
	for _, it := range l.items {
		if it != nil {
			Incref(it)
		}
	}
	// CPython: Objects/listobject.c:159 PyList_New (PyObject_GC_Track tail)
	if h := GCTrackHook; h != nil {
		h(l)
	}
	return l
}

// Len returns the number of items.
//
// CPython: Objects/listobject.c:L286 PyList_Size
func (l *List) Len() int { return len(l.items) }

// Append adds v to the end. Mirrors PyList_Append.
//
// CPython: Objects/listobject.c:L351 PyList_Append
func (l *List) Append(v Object) {
	// The list takes a counted reference on the appended value; listDealloc
	// releases it. CPython's app1 does Py_INCREF via PyList_SET_ITEM after
	// list_resize.
	//
	// CPython: Objects/listobject.c:351 PyList_Append (app1 -> Py_INCREF)
	if v != nil {
		Incref(v)
	}
	l.items = append(l.items, v)
	l.size = int64(len(l.items))
}

// Item returns the item at index i without bounds check.
//
// CPython: Objects/listobject.c:L308 PyList_GetItem
func (l *List) Item(i int) Object { return l.items[i] }

// SetItem stores v at index i. The caller is responsible for bounds
// checking; out-of-range indices panic.
//
// CPython: Objects/listobject.c:271 PyList_SET_ITEM
//
// gopy's slot lives one level up from CPython's macro: list_ass_item is
// the slot that does the Py_INCREF(new)/Py_SETREF(old) dance, while the
// PyList_SET_ITEM macro is the raw steal-into-slot used right after
// PyList_New. Since gopy lists own a counted reference per item, SetItem
// takes a reference on the incoming value and drops the displaced one,
// matching list_ass_item rather than the bare macro.
//
// CPython: Objects/listobject.c:3119 list_ass_item (Py_INCREF + Py_SETREF)
func (l *List) SetItem(i int, v Object) {
	old := l.items[i]
	if v != nil {
		Incref(v)
	}
	l.items[i] = v
	if old != nil && old != v {
		Decref(old)
	}
}

// SetSlice replaces items[start:stop] with values. CPython implements
// this through PyList_SetSlice; the gopy port keeps the contract
// (start and stop already clamped/normalized by the caller).
//
// CPython: Objects/listobject.c PyList_SetSlice
func (l *List) SetSlice(start, stop int, values []Object) {
	// The [start:stop] region is dropped and replaced by values. The list
	// owns a reference per item, so release the displaced items and take a
	// reference on the incoming ones. Incref the new values before
	// decreffing the old, in case a value is also one of the displaced
	// items (it must not be torn down before the list re-adopts it).
	//
	// CPython: Objects/listobject.c:892 list_ass_slice_lock_held
	for _, v := range values {
		if v != nil {
			Incref(v)
		}
	}
	for _, old := range l.items[start:stop] {
		if old != nil {
			Decref(old)
		}
	}
	tail := append([]Object(nil), l.items[stop:]...)
	l.items = append(append(l.items[:start], values...), tail...)
	l.size = int64(len(l.items))
}

func listLen(o Object) (int, error) {
	return o.(*List).Len(), nil
}

// listContains mirrors list_contains / PySequence_Contains. Identity
// check before equality so non-reflexive values like NaN are found.
//
// CPython: Objects/listobject.c:L466 list_contains
func listContains(haystack, needle Object) (bool, error) {
	for _, item := range haystack.(*List).items {
		eq, err := RichCmpBool(item, needle, CompareEQ)
		if err != nil {
			return false, err
		}
		if eq {
			return true, nil
		}
	}
	return false, nil
}

func listGetItem(o Object, i int) (Object, error) {
	l := o.(*List)
	if i < 0 {
		i += len(l.items)
	}
	if i < 0 || i >= len(l.items) {
		return nil, errIndexOutOfRange
	}
	return l.items[i], nil
}

// listConcat ports list_concat: build a fresh list with a's items
// followed by b's. b must be a list; mismatched types raise TypeError
// like CPython does (not NotImplemented; the list slot itself rejects
// non-lists).
//
// CPython: Objects/listobject.c:541 list_concat
func listConcat(a, b Object) (Object, error) {
	la := a.(*List)
	lb, ok := b.(*List)
	if !ok {
		return nil, fmt.Errorf("TypeError: can only concatenate list (not \"%s\") to list", b.Type().Name)
	}
	out := make([]Object, 0, len(la.items)+len(lb.items))
	out = append(out, la.items...)
	out = append(out, lb.items...)
	return newListAdopt(out), nil
}

// listRepeat ports list_repeat: produce a fresh list containing n
// copies of o's items. Negative or zero n produces an empty list,
// matching the CPython behavior. A multiplication that would exceed
// the platform allocator's reach raises MemoryError, mirroring
// PyErr_NoMemory in list_repeat_lock_held.
//
// CPython: Objects/listobject.c:790 list_repeat_lock_held
func listRepeat(o Object, n int) (rv Object, rerr error) {
	l := o.(*List)
	input := len(l.items)
	if input == 0 || n <= 0 {
		return newListAdopt(nil), nil
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
		out = append(out, l.items...)
	}
	return newListAdopt(out), nil
}

// listInPlaceConcat extends a with b's items. b must be iterable; the
// CPython slot delegates to list_extend, which accepts any iterable.
// Three fast paths mirror _list_extend's dispatch:
//   - a is b: degenerate self-extend, equivalent to a *= 2.
//   - b is a list or tuple: snapshot the items, then append. A bare
//     iterator loop would observe the destination's growth when
//     a is b's storage (it isn't here, but the snapshot keeps the
//     generic case predictable when callers stash an alias).
//   - otherwise: iterate and append, surfacing __length_hint__
//     exceptions exactly once before the loop starts.
//
// CPython: Objects/listobject.c:1402 _list_extend
// CPython: Objects/listobject.c:1187 list_extend_fast
// CPython: Objects/listobject.c:1225 list_extend_iter_lock_held
func listInPlaceConcat(a, b Object) (Object, error) {
	la := a.(*List)
	if la == b {
		// a.extend(a) → a *= 2. Reading l.items live during a plain
		// iterator loop would never terminate.
		//
		// CPython: Objects/listobject.c:1407 (PyObject *)self == iterable
		_, err := listInPlaceRepeat(la, 2)
		return la, err
	}
	if lb, ok := b.(*List); ok {
		snap := append([]Object(nil), lb.items...)
		for _, v := range snap {
			la.Append(v)
		}
		return la, nil
	}
	if tb, ok := b.(*Tuple); ok {
		for i := 0; i < tb.Len(); i++ {
			la.Append(tb.Item(i))
		}
		return la, nil
	}
	tp := b.Type()
	if tp.Iter == nil {
		return nil, fmt.Errorf("TypeError: '%s' object is not iterable", tp.Name)
	}
	it, err := tp.Iter(b)
	if err != nil {
		return nil, err
	}
	itType := it.Type()
	if itType.IterNext == nil {
		return nil, fmt.Errorf("TypeError: iter() returned non-iterator of type '%s'", itType.Name)
	}
	// CPython: Objects/listobject.c:1234 PyObject_LengthHint propagates
	// exceptions out of __len__ / __length_hint__ so a broken iterable
	// surfaces here instead of looking like a silent empty extend.
	if _, lhErr := LengthHint(b, 8); lhErr != nil {
		return nil, lhErr
	}
	for {
		v, err := itType.IterNext(it)
		if err != nil {
			if errors.Is(err, ErrStopIteration) || (IsStopIterationHook != nil && IsStopIterationHook(err)) {
				if ClearCurrentExceptionHook != nil {
					ClearCurrentExceptionHook()
				}
				break
			}
			return nil, err
		}
		la.Append(v)
	}
	return la, nil
}

// listInPlaceRepeat repeats a's contents n times in place. n<=0 wipes
// the list. A multiplication that would exceed the platform
// allocator's reach raises MemoryError, mirroring PyErr_NoMemory in
// list_inplace_repeat_lock_held.
//
// CPython: Objects/listobject.c:1033 list_inplace_repeat_lock_held
func listInPlaceRepeat(o Object, n int) (rv Object, rerr error) {
	l := o.(*List)
	input := len(l.items)
	if input == 0 || n == 1 {
		return l, nil
	}
	if n < 1 {
		// n<=0 empties the list: release the list's reference on each
		// dropped item.
		for i := range l.items {
			if l.items[i] != nil {
				Decref(l.items[i])
			}
			l.items[i] = nil
		}
		l.items = l.items[:0]
		l.size = 0
		return l, nil
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
	// Pre-allocate the destination so makeslice's cap-out-of-range
	// panic catches absurd n*input products instead of looping forever
	// before the append heuristic notices.
	dst := make([]Object, input*n)
	for i := 0; i < n; i++ {
		copy(dst[i*input:], l.items)
	}
	// The original items keep their existing reference; each of the n-1
	// extra copies is a new stored reference, so incref the items n-1 times.
	//
	// CPython: Objects/listobject.c:1033 list_inplace_repeat_lock_held
	// (_Py_RefcntAdd over the copied region)
	for _, it := range l.items {
		if it == nil {
			continue
		}
		for k := 0; k < n-1; k++ {
			Incref(it)
		}
	}
	l.items = dst
	l.size = int64(len(l.items))
	return l, nil
}

func listSetItem(o Object, i int, v Object) error {
	l := o.(*List)
	if i < 0 {
		i += len(l.items)
	}
	if i < 0 || i >= len(l.items) {
		return errIndexOutOfRange
	}
	// Route through SetItem so the displaced item is decreffed and the new
	// value increfed (list_ass_item's Py_SETREF), keeping the per-item
	// ownership invariant.
	l.SetItem(i, v)
	return nil
}

// listMappingGet ports list_subscript: integer keys go through
// listGetItem, slice keys return a fresh list slice.
//
// CPython: Objects/listobject.c:3162 list_subscript
func listMappingGet(o, key Object) (Object, error) {
	if s, ok := key.(*Slice); ok {
		return listGetSlice(o.(*List), s)
	}
	idx, err := indexValueAsInt(key, "list")
	if err != nil {
		return nil, err
	}
	return listGetItem(o, idx)
}

// listMappingSet ports list_ass_subscript: integer assignment goes
// through listSetItem; slice assignment replaces the slice region in
// place. value==nil means delete (the path used by mp_ass_subscript
// for both `del list[i]` and `del list[a:b]`).
//
// CPython: Objects/listobject.c:3198 list_ass_subscript
func listMappingSet(o, key, v Object) error {
	l := o.(*List)
	if s, ok := key.(*Slice); ok {
		if v == nil {
			return listDelSlice(l, s)
		}
		return listSetSlice(l, s, v)
	}
	idx, err := indexValueAsInt(key, "list")
	if err != nil {
		return err
	}
	if v == nil {
		return listDelIndex(l, idx)
	}
	return listSetItem(o, idx, v)
}

// listMappingDel is the standalone delitem entry. CPython routes
// `del obj[key]` through mp_ass_subscript with NULL value; gopy's vm
// calls mp.DelItem instead, so dispatch here delegates to the same
// shared helper.
//
// CPython: Objects/listobject.c:3198 list_ass_subscript (NULL v arm)
func listMappingDel(o, key Object) error {
	return listMappingSet(o, key, nil)
}

// listDelIndex removes l.items[i]. Mirrors list_ass_item with v=NULL.
//
// CPython: Objects/listobject.c:3041 list_ass_item
func listDelIndex(l *List, i int) error {
	if i < 0 {
		i += len(l.items)
	}
	if i < 0 || i >= len(l.items) {
		return errIndexOutOfRange
	}
	// Release the list's reference on the removed item.
	//
	// CPython: Objects/listobject.c:806 list_ass_slice (Py_DECREF on removed)
	if old := l.items[i]; old != nil {
		Decref(old)
	}
	l.items = append(l.items[:i], l.items[i+1:]...)
	l.size = int64(len(l.items))
	return nil
}

// listGetSlice returns a fresh list containing the slice region.
//
// CPython: Objects/listobject.c:482 list_slice
func listGetSlice(l *List, s *Slice) (Object, error) {
	start, _, step, slicelen, err := s.GetIndices(len(l.items))
	if err != nil {
		return nil, err
	}
	out := make([]Object, slicelen)
	for i, idx := 0, start; i < slicelen; i, idx = i+1, idx+step {
		out[i] = l.items[idx]
	}
	return newListAdopt(out), nil
}

// listSetSlice replaces l[start:stop:step] with the values produced by
// iterating v. Step==1 supports differing lengths (grow / shrink);
// extended steps require matching lengths. CPython drains the source
// before recomputing slicelen so an iterable that mutates self (e.g.,
// generator that clears the list mid-iteration) is observed against
// the live size.
//
// CPython: Objects/listobject.c:3775 list_ass_subscript_lock_held
//
//	(extended-slice arm: PySequence_Fast first, then adjust_slice_indexes,
//	then size-mismatch ValueError).
func listSetSlice(l *List, s *Slice, v Object) error {
	_, _, step, _, err := s.GetIndices(len(l.items))
	if err != nil {
		return err
	}
	if step == 1 {
		start, stop, _, _, err := s.GetIndices(len(l.items))
		if err != nil {
			return err
		}
		return listAssSlice(l, start, stop, v)
	}
	src, err := drainIterableForSlice(v)
	if err != nil {
		return err
	}
	start, _, step, slicelen, err := s.GetIndices(len(l.items))
	if err != nil {
		return err
	}
	if len(src) != slicelen {
		return fmt.Errorf("ValueError: attempt to assign sequence of size %d to extended slice of size %d", len(src), slicelen)
	}
	// Each extended-slice slot is overwritten; SetItem drops the displaced
	// item and takes a reference on the new one, keeping the per-item
	// ownership invariant.
	for i, idx := 0, start; i < slicelen; i, idx = i+1, idx+step {
		l.SetItem(idx, src[i])
	}
	return nil
}

// listAssSlice ports list_ass_slice for the step==1 path. Equal-size
// assignment writes the n items directly into the existing backing
// array (no allocation); shrink and grow paths resize in place and use
// copy() instead of building a fresh slice from three appends.
//
// CPython: Objects/listobject.c:892 list_ass_slice_lock_held
// CPython: Objects/listobject.c:993 list_ass_slice
func listAssSlice(l *List, ilow, ihigh int, v Object) error {
	if v == Object(l) {
		// Aliased self-assign: copy v first, matching CPython's
		// list_ass_slice fall-through that calls list_slice_lock_held
		// to capture the source before overwriting the target.
		dup := make([]Object, len(l.items))
		copy(dup, l.items)
		return listAssSliceItems(l, ilow, ihigh, dup)
	}
	var src []Object
	switch t := v.(type) {
	case nil:
		// nil v means delete.
	case *List:
		src = t.items
	case *Tuple:
		src = t.items
	default:
		drained, err := drainIterableForSlice(v)
		if err != nil {
			return err
		}
		src = drained
	}
	return listAssSliceItems(l, ilow, ihigh, src)
}

// listAssSliceItems is the in-place body of list_ass_slice once v has
// been resolved into a flat items slice. items may alias neither l.items
// nor any region that overlaps with the [ilow:ihigh] range.
func listAssSliceItems(l *List, ilow, ihigh int, items []Object) error {
	n := len(items)
	if ilow < 0 {
		ilow = 0
	} else if ilow > len(l.items) {
		ilow = len(l.items)
	}
	if ihigh < ilow {
		ihigh = ilow
	} else if ihigh > len(l.items) {
		ihigh = len(l.items)
	}
	// The [ilow:ihigh] region is dropped and the items slice adopted. The
	// list owns one counted reference per item, so take references on the
	// incoming items and release the displaced ones. Below this point the
	// copy() shuffles only move existing pointers between slots, which is
	// reference-neutral. Incref before decref so a self-slice-assign (where
	// items is a snapshot of l's own contents) does not tear an object down
	// before it is re-adopted.
	//
	// CPython: Objects/listobject.c:892 list_ass_slice_lock_held
	for _, v := range items {
		if v != nil {
			Incref(v)
		}
	}
	for _, old := range l.items[ilow:ihigh] {
		if old != nil {
			Decref(old)
		}
	}
	norig := ihigh - ilow
	d := n - norig
	if d == 0 {
		// Equal-size replacement: store directly into existing slots.
		if n > 0 {
			copy(l.items[ilow:ihigh], items)
		}
		return nil
	}
	if d < 0 {
		// Shrink: shift tail left, then truncate.
		copy(l.items[ihigh+d:], l.items[ihigh:])
		l.items = l.items[:len(l.items)+d]
	} else {
		// Grow: extend by d, then shift tail right.
		newLen := len(l.items) + d
		if cap(l.items) >= newLen {
			l.items = l.items[:newLen]
		} else {
			grown := make([]Object, newLen, growCap(newLen))
			copy(grown, l.items[:ilow])
			copy(grown[ihigh+d:], l.items[ihigh:])
			l.items = grown
			l.size = int64(len(l.items))
			if n > 0 {
				copy(l.items[ilow:], items)
			}
			return nil
		}
		copy(l.items[ihigh+d:], l.items[ihigh:newLen-d])
	}
	if n > 0 {
		copy(l.items[ilow:ilow+n], items)
	}
	l.size = int64(len(l.items))
	return nil
}

// growCap mirrors list_resize's growth schedule (newLen + newLen/8 + 6).
//
// CPython: Objects/listobject.c:74 list_resize
func growCap(n int) int {
	return n + (n >> 3) + 6
}

// listDelSlice removes l[start:stop:step] in place.
//
// CPython: Objects/listobject.c:806 list_ass_slice (NULL v) +
// extended-slice arm of list_ass_subscript
func listDelSlice(l *List, s *Slice) error {
	start, stop, step, slicelen, err := s.GetIndices(len(l.items))
	if err != nil {
		return err
	}
	if slicelen == 0 {
		return nil
	}
	if step == 1 {
		// Release the list's reference on each removed item.
		//
		// CPython: Objects/listobject.c:806 list_ass_slice (Py_DECREF loop)
		for _, old := range l.items[start:stop] {
			if old != nil {
				Decref(old)
			}
		}
		l.items = append(l.items[:start], l.items[stop:]...)
		l.size = int64(len(l.items))
		return nil
	}
	drop := make(map[int]bool, slicelen)
	for i, idx := 0, start; i < slicelen; i, idx = i+1, idx+step {
		drop[idx] = true
	}
	out := l.items[:0]
	for i, v := range l.items {
		if drop[i] {
			// Removed by the extended-step delete: drop the list's
			// reference on it.
			if v != nil {
				Decref(v)
			}
			continue
		}
		out = append(out, v)
	}
	l.items = out
	l.size = int64(len(l.items))
	return nil
}

// indexValueAsInt coerces an integer-shaped object to a Go int for
// container subscript dispatch. Mirrors PyNumber_AsSsize_t through the
// __index__ slot, with the type name used in error messages.
//
// CPython: Objects/abstract.c PyNumber_AsSsize_t
func indexValueAsInt(key Object, typeName string) (int, error) {
	if i, ok := key.(*Int); ok {
		n, _ := i.Int64()
		return int(n), nil
	}
	if b, ok := key.(*Bool); ok {
		if b == True() {
			return 1, nil
		}
		return 0, nil
	}
	// CPython routes the subscript through PyNumber_AsSsize_t which
	// invokes __index__ when present. A class with only __index__ must
	// work as a sequence subscript.
	//
	// CPython: Objects/abstract.c:1486 PyNumber_AsSsize_t
	if idx, err := NumberIndex(key); err == nil {
		if i, ok := idx.(*Int); ok {
			n, _ := i.Int64()
			return int(n), nil
		}
	}
	return 0, fmt.Errorf("TypeError: %s indices must be integers or slices, not %s", typeName, key.Type().Name)
}

// drainIterableForSlice materializes an iterable into a slice for the
// slice-assignment path. Mirrors PySequence_Fast: route through the
// general Iter() (which falls back to the __getitem__-driven SeqIter
// when an object has no __iter__) so anything CPython treats as an
// iterable here works the same way.
//
// CPython: Objects/abstract.c:1846 PySequence_Fast
func drainIterableForSlice(o Object) ([]Object, error) {
	// PySequence_Fast fast-paths only PyList_CheckExact / PyTuple_CheckExact.
	// A subclass may override __iter__ (seq_tests.LyingTuple yields values
	// unrelated to its storage), so it must go through the iterator protocol.
	if l, ok := o.(*List); ok && l.Type() == ListType {
		out := make([]Object, len(l.items))
		copy(out, l.items)
		return out, nil
	}
	if t, ok := o.(*Tuple); ok && t.Type() == TupleType {
		out := make([]Object, len(t.items))
		copy(out, t.items)
		return out, nil
	}
	it, err := Iter(o)
	if err != nil {
		return nil, fmt.Errorf("TypeError: can only assign an iterable")
	}
	itT := it.Type()
	if itT.IterNext == nil {
		return nil, fmt.Errorf("TypeError: iter() returned non-iterator of type '%s'", itT.Name)
	}
	var out []Object
	for {
		v, err := itT.IterNext(it)
		if errors.Is(err, ErrStopIteration) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
}

// DrainIterable materializes any iterable into a slice. Unlike
// drainIterableForSlice it propagates the underlying TypeError from
// PyObject_GetIter, so callers like list() / tuple() / set() surface
// "'X' object is not iterable" instead of the slice-assign wording.
//
// CPython: Objects/abstract.c:2820 PySequence_Fast (constructor path)
func DrainIterable(o Object) ([]Object, error) {
	// PySequence_Fast fast-paths only PyList_CheckExact / PyTuple_CheckExact.
	// A subclass may override __iter__ (seq_tests.LyingTuple yields values
	// unrelated to its storage), so it must go through the iterator protocol.
	// Every returned element carries one owned reference the caller must
	// release (or steal). IterNext hands back a borrowed reference under
	// gopy's iterator convention, so a self-recycling slot such as the dict
	// item iterator would free an element already collected here; owning each
	// element keeps it alive for the whole batch. CPython's PyIter_Next /
	// PySequence_Fast own their elements for the same reason.
	//
	// CPython: Objects/abstract.c:2852 PyIter_Next (owned return)
	if l, ok := o.(*List); ok && l.Type() == ListType {
		out := make([]Object, len(l.items))
		copy(out, l.items)
		for _, v := range out {
			Incref(v)
		}
		return out, nil
	}
	if t, ok := o.(*Tuple); ok && t.Type() == TupleType {
		out := make([]Object, len(t.items))
		copy(out, t.items)
		for _, v := range out {
			Incref(v)
		}
		return out, nil
	}
	it, err := Iter(o)
	if err != nil {
		return nil, err
	}
	if it.Type().IterNext == nil {
		return nil, fmt.Errorf("TypeError: iter() returned non-iterator of type '%s'", it.Type().Name)
	}
	var out []Object
	for {
		v, err := IterNext(it)
		if errors.Is(err, ErrStopIteration) {
			return out, nil
		}
		if err != nil {
			for _, x := range out {
				Decref(x)
			}
			return nil, err
		}
		Incref(v)
		out = append(out, v)
	}
}

// listReprInProgress guards against recursive list repr: [[...]].
//
// CPython: Objects/object.c:2256 Py_ReprEnter
var listReprInProgress sync.Map

func listRepr(o Object) (string, error) {
	l := o.(*List)
	ptr := uintptr(unsafe.Pointer(l))
	if _, loaded := listReprInProgress.LoadOrStore(ptr, struct{}{}); loaded {
		return "[...]", nil
	}
	defer listReprInProgress.Delete(ptr)

	var b strings.Builder
	b.WriteByte('[')
	// CPython: Objects/listobject.c:596 list_repr_impl reloads
	// Py_SIZE(v) on every iteration so a side effect in repr() that
	// shrinks the list shortens the walk in step (test_repr_mutate).
	for i := 0; i < len(l.items); i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		s, err := Repr(l.items[i])
		if err != nil {
			return "", err
		}
		b.WriteString(s)
	}
	b.WriteByte(']')
	return b.String(), nil
}

// listIterator is the iterator returned by iter(list).
//
// CPython: Objects/listobject.c:L3539 PyListIter_Type
type listIterator struct {
	Header
	src *List
	pos int
}

var listIterType = NewType("list_iterator", []*Type{objectType})

// listIterNext yields the next item, clearing the source reference on
// exhaustion so the iterator becomes a sink state and frees its hold on the
// list (free_after_iterating).
//
// CPython: Objects/listobject.c:3573 listiter_next
func listIterNext(o Object) (Object, error) {
	it := o.(*listIterator)
	if it.src == nil {
		return nil, ErrStopIteration
	}
	if it.pos >= len(it.src.items) {
		old := it.src
		it.src = nil
		Decref(old)
		return nil, ErrStopIteration
	}
	v := it.src.items[it.pos]
	it.pos++
	return v, nil
}

// listIterDealloc drops the iterator's reference on the source list if it has
// not already been cleared by exhaustion.
//
// CPython: Objects/listobject.c:3530 listiter_dealloc
func listIterDealloc(o Object) {
	if h := GCUntrackHook; h != nil {
		h(o)
	}
	ClearWeakRefs(o)
	it := o.(*listIterator)
	if it.src != nil {
		Decref(it.src)
		it.src = nil
	}
}

func init() {
	listIterType.Iter = SelfIter
	listIterType.IterNext = listIterNext
	listIterType.Dealloc = listIterDealloc
	AddIterSlotWrappers(listIterType)
	// __reduce__ returns (iter, (list_snapshot,), current_pos) so pickle
	// can round-trip the iterator including its current position.
	//
	// CPython: Objects/listobject.c listiter_reduce
	SetTypeDescr(listIterType, "__reduce__", NewMethodDescrConv(listIterType, "__reduce__", MethNoArgs,
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments")
			}
			it := args[0].(*listIterator)
			if BuiltinLookup == nil {
				return nil, fmt.Errorf("PicklingError: builtins not loaded")
			}
			iterFn, err := BuiltinLookup("iter")
			if err != nil {
				return nil, err
			}
			if it.src == nil {
				return NewTuple([]Object{iterFn, NewTuple([]Object{NewList(nil)})}), nil
			}
			return NewTuple([]Object{iterFn, NewTuple([]Object{it.src}), NewInt(int64(it.pos))}), nil
		},
	))
	// __setstate__ restores the iterator position after unpickling.
	//
	// CPython: Objects/listobject.c listiter_setstate
	SetTypeDescr(listIterType, "__setstate__", NewMethodDescrConv(listIterType, "__setstate__", MethO,
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __setstate__() takes exactly one argument")
			}
			it := args[0].(*listIterator)
			pos, ok := args[1].(*Int)
			if !ok {
				return nil, fmt.Errorf("TypeError: __setstate__() argument must be int")
			}
			p, fits := pos.Int64()
			if !fits {
				return nil, fmt.Errorf("OverflowError: iterator position out of range")
			}
			n := int64(0)
			if it.src != nil {
				n = int64(len(it.src.items))
			}
			if p < 0 {
				p = 0
			} else if p > n {
				p = n
			}
			it.pos = int(p)
			return None(), nil
		},
	))
	// CPython: Objects/listobject.c:3593 listiter_len
	SetTypeDescr(listIterType, "__length_hint__", NewMethodDescrConv(listIterType, "__length_hint__", MethNoArgs,
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __length_hint__ takes no arguments")
			}
			it := args[0].(*listIterator)
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

func listIter(o Object) (Object, error) {
	src := o.(*List)
	// The iterator holds a counted reference on the list for its lifetime
	// (Py_INCREF(seq) in list___iter___), released on exhaustion or in
	// listiter_dealloc. Without this, a list held only by a live iterator
	// could reach refcount 0 and listDealloc would tear down its items
	// while the iterator still walks them.
	//
	// CPython: Objects/listobject.c:3552 list___iter___ (Py_INCREF(seq))
	Incref(src)
	it := &listIterator{src: src}
	it.init(listIterType)
	return it, nil
}

// listRevIterator is the iterator returned by list.__reversed__(). It
// walks the source list from the tail toward the head and shares its
// backing storage with the original list so pickle round-trips, slice
// mutations, and length-hint queries all observe the live values.
//
// CPython: Objects/listobject.c:4080 listreviterobject
type listRevIterator struct {
	Header
	src   *List
	index int
}

var listRevIterType = NewType("list_reverseiterator", []*Type{objectType})

//nolint:gocognit // method-descriptor registration table for the list type; flat sequence of SetTypeDescr calls
func init() {
	listRevIterType.Iter = SelfIter
	listRevIterType.IterNext = func(o Object) (Object, error) {
		it := o.(*listRevIterator)
		if it.src == nil || it.index < 0 {
			revIterClear(it)
			return nil, ErrStopIteration
		}
		if it.index >= len(it.src.items) {
			revIterClear(it)
			return nil, ErrStopIteration
		}
		v := it.src.items[it.index]
		it.index--
		return v, nil
	}
	// listreviter_dealloc drops the iterator's reference on the source
	// list when it has not already been cleared by exhaustion.
	//
	// CPython: Objects/listobject.c:4140 listreviter_dealloc
	listRevIterType.Dealloc = func(o Object) {
		if h := GCUntrackHook; h != nil {
			h(o)
		}
		ClearWeakRefs(o)
		it := o.(*listRevIterator)
		if it.src != nil {
			Decref(it.src)
			it.src = nil
		}
	}
	AddIterSlotWrappers(listRevIterType)
	// __reduce__ returns (iter_fn, (list,), index) so pickle preserves
	// the shared source list (test_reversed_pickle) and the current
	// position. iter_fn is `reversed` so unpickling rebuilds a
	// list_reverseiterator over the same list, then __setstate__
	// restores the index.
	//
	// CPython: Objects/listobject.c:4207 listreviter_reduce
	SetTypeDescr(listRevIterType, "__reduce__", NewMethodDescrConv(listRevIterType, "__reduce__", MethNoArgs,
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments")
			}
			it := args[0].(*listRevIterator)
			if BuiltinLookup == nil {
				return nil, fmt.Errorf("PicklingError: builtins not loaded")
			}
			revFn, err := BuiltinLookup("reversed")
			if err != nil {
				return nil, err
			}
			if it.src == nil {
				return NewTuple([]Object{revFn, NewTuple([]Object{NewList(nil)})}), nil
			}
			return NewTuple([]Object{revFn, NewTuple([]Object{it.src}), NewInt(int64(it.index))}), nil
		},
	))
	// __setstate__ restores the iterator index after unpickling. CPython
	// clamps to [-1, len(seq)-1] so a bogus state can't drive the
	// next pointer past either end.
	//
	// CPython: Objects/listobject.c:4213 listreviter_setstate
	SetTypeDescr(listRevIterType, "__setstate__", NewMethodDescrConv(listRevIterType, "__setstate__", MethO,
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __setstate__() takes exactly one argument")
			}
			it := args[0].(*listRevIterator)
			pos, ok := args[1].(*Int)
			if !ok {
				return nil, fmt.Errorf("TypeError: __setstate__() argument must be int")
			}
			p, fits := pos.Int64()
			if !fits {
				return nil, fmt.Errorf("OverflowError: iterator position out of range")
			}
			if it.src != nil {
				n := int64(len(it.src.items)) - 1
				if p < -1 {
					p = -1
				} else if p > n {
					p = n
				}
				it.index = int(p)
			}
			return None(), nil
		},
	))
	// CPython: Objects/listobject.c:4196 listreviter_len
	SetTypeDescr(listRevIterType, "__length_hint__", NewMethodDescrConv(listRevIterType, "__length_hint__", MethNoArgs,
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __length_hint__ takes no arguments")
			}
			it := args[0].(*listRevIterator)
			if it.src == nil {
				return NewInt(0), nil
			}
			n := it.index + 1
			if n < 0 || n > len(it.src.items) {
				n = 0
			}
			return NewInt(int64(n)), nil
		},
	))
}

// listRevIter is the constructor for list.__reversed__ used by
// listReversedMethod. It holds the source list (so pickle preserves
// identity) and starts at len-1.
//
// CPython: Objects/listobject.c:4140 list___reversed___impl
func listRevIter(l *List) Object {
	// The reverse iterator holds a counted reference on the source list,
	// released on exhaustion (revIterClear) or in listreviter_dealloc.
	//
	// CPython: Objects/listobject.c:4140 list___reversed___impl (Py_INCREF)
	Incref(l)
	it := &listRevIterator{src: l, index: len(l.items) - 1}
	it.init(listRevIterType)
	return it
}

// revIterClear drops the reverse iterator into its exhausted sink state,
// releasing its reference on the source list exactly once.
func revIterClear(it *listRevIterator) {
	if it.src != nil {
		Decref(it.src)
		it.src = nil
	}
	it.index = -1
}

// ListIterNextFast advances o as a list_iterator without going through
// the type-table tp_iternext indirection. Returns the next value, or
// (nil, true) on exhaustion. ok=false means o was not exactly a
// list_iterator and the FOR_ITER_LIST fast arm must deopt.
//
// On exhaustion the function nulls it.src so a re-entered FOR_ITER on
// the dead iterator releases its grip on the source list, mirroring
// CPython's `it->it_seq = NULL; Py_DECREF(seq);` in _ITER_JUMP_LIST.
//
// CPython: Python/bytecodes.c _ITER_CHECK_LIST + _ITER_JUMP_LIST + _ITER_NEXT_LIST
func ListIterNextFast(o Object) (value Object, exhausted bool, ok bool) {
	it, asserted := o.(*listIterator)
	if !asserted || it.Type() != listIterType {
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
