// dict iterators and view objects. Iteration walks the live entry
// table and snapshots ma_used at iter creation; if the dict's used
// count changes mid-iteration the next iternext raises RuntimeError
// the way CPython does. The three view types (keys/values/items)
// stay backed by the source dict, so they reflect mutations and
// the set algebra on keys() and items() runs against the current
// state of the dict.
//
// CPython: Objects/dictobject.c:5132 dictiter_new
// CPython: Objects/dictobject.c:5226 dictiter_iternextkey_lock_held

package objects

import (
	"errors"
	"fmt"
)

// dictIterKind tags which payload the iterator yields. Three CPython
// iter types collapse to one Go struct with a kind discriminant.
type dictIterKind uint8

const (
	dictIterKeys dictIterKind = iota
	dictIterValues
	dictIterItems
)

// dictIterObj is the live walker. snapUsed pins the dict's ma_used
// at construction; advance compares the snapshot against the dict's
// current used count and bails with RuntimeError on a mismatch.
//
// CPython: Objects/dictobject.c:5117 dictiterobject
type dictIterObj struct {
	Header
	src      *Dict
	pos      int
	snapUsed int
	// snapStruct pins the dict's structVersion at construction. A
	// delete+reinsert leaves ma_used unchanged so the snapUsed check
	// alone misses it; comparing structVersion catches any change to
	// the entry table during the walk.
	snapStruct uint64
	// length mirrors di->len: the count of entries the iterator still
	// expects to yield. dictiter_iternextkey decrements it on each hit
	// and, if it finds an entry once length has reached 0, raises
	// "dictionary keys changed during iteration" (a delete+reinsert that
	// keeps ma_used unchanged but shuffles the entry table).
	//
	// CPython: Objects/dictobject.c:5279 (the di->len == 0 branch)
	length int
	kind   dictIterKind
	// reversed walks it.src.order from the tail toward the head, mirroring
	// the dict_reversekeyiterator family. pos counts down from the last
	// order slot to 0 instead of up from 0 to len(order).
	//
	// CPython: Objects/dictobject.c:5408 dict___reversed___impl
	reversed bool
	// owns is true when this iterator holds a counted reference on src
	// (the dictiter_new path). The transient walker dictiter_reduce
	// builds for draining borrows src instead, so it leaves owns false
	// and never touches the source dict's refcount.
	owns bool
}

var (
	dictKeyIterType   = NewType("dict_keyiterator", []*Type{objectType})
	dictValueIterType = NewType("dict_valueiterator", []*Type{objectType})
	dictItemIterType  = NewType("dict_itemiterator", []*Type{objectType})

	dictReverseKeyIterType   = NewType("dict_reversekeyiterator", []*Type{objectType})
	dictReverseValueIterType = NewType("dict_reversevalueiterator", []*Type{objectType})
	dictReverseItemIterType  = NewType("dict_reverseitemiterator", []*Type{objectType})
)

func init() {
	identity := func(o Object) (Object, error) { return o, nil }

	dictKeyIterType.Iter = identity
	dictKeyIterType.IterNext = dictIterNextKey
	dictKeyIterType.Dealloc = dictIterDealloc

	dictValueIterType.Iter = identity
	dictValueIterType.IterNext = dictIterNextValue
	dictValueIterType.Dealloc = dictIterDealloc

	dictItemIterType.Iter = identity
	dictItemIterType.IterNext = dictIterNextItem
	dictItemIterType.Dealloc = dictIterDealloc

	// Reverse iterators share the same slots; advance() walks the order
	// list backward when it.reversed is set.
	//
	// CPython: Objects/dictobject.c:5408 dict___reversed___impl
	dictReverseKeyIterType.Iter = identity
	dictReverseKeyIterType.IterNext = dictIterNextKey
	dictReverseKeyIterType.Dealloc = dictIterDealloc

	dictReverseValueIterType.Iter = identity
	dictReverseValueIterType.IterNext = dictIterNextValue
	dictReverseValueIterType.Dealloc = dictIterDealloc

	dictReverseItemIterType.Iter = identity
	dictReverseItemIterType.IterNext = dictIterNextItem
	dictReverseItemIterType.Dealloc = dictIterDealloc

	AddIterSlotWrappers(dictKeyIterType)
	AddIterSlotWrappers(dictValueIterType)
	AddIterSlotWrappers(dictItemIterType)
	AddIterSlotWrappers(dictReverseKeyIterType)
	AddIterSlotWrappers(dictReverseValueIterType)
	AddIterSlotWrappers(dictReverseItemIterType)

	dictReduceFn := func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments")
		}
		it := args[0].(*dictIterObj)
		if BuiltinLookup == nil {
			return nil, fmt.Errorf("PicklingError: builtins not loaded")
		}
		iterFn, err := BuiltinLookup("iter")
		if err != nil {
			return nil, err
		}
		// CPython: Objects/dictobject.c:5310 dictiter_reduce — drain
		// remaining items into a list, then pickle as (iter, (list,)).
		// The unpickled iterator will be a list_iterator, not a dict
		// iterator — this matches CPython's documented behavior.
		items := []Object{}
		tmp := &dictIterObj{src: it.src, pos: it.pos, snapUsed: it.snapUsed, snapStruct: it.snapStruct, length: it.length, kind: it.kind, reversed: it.reversed}
		tmp.init(it.Type())
		for {
			var v Object
			switch it.kind {
			case dictIterKeys:
				k, _, e := tmp.advance()
				if errors.Is(e, ErrStopIteration) {
					goto done
				}
				if e != nil {
					return nil, e
				}
				v = k
			case dictIterValues:
				_, val, e := tmp.advance()
				if errors.Is(e, ErrStopIteration) {
					goto done
				}
				if e != nil {
					return nil, e
				}
				v = val
			case dictIterItems:
				k, val, e := tmp.advance()
				if errors.Is(e, ErrStopIteration) {
					goto done
				}
				if e != nil {
					return nil, e
				}
				v = NewTuple([]Object{k, val})
			}
			items = append(items, v)
		}
	done:
		return NewTuple([]Object{iterFn, NewTuple([]Object{NewList(items)})}), nil
	}
	// CPython: Objects/dictobject.c:5310 dictiter_reduce
	SetTypeDescr(dictKeyIterType, "__reduce__", NewMethodDescr(dictKeyIterType, "__reduce__", dictReduceFn))
	SetTypeDescr(dictValueIterType, "__reduce__", NewMethodDescr(dictValueIterType, "__reduce__", dictReduceFn))
	SetTypeDescr(dictItemIterType, "__reduce__", NewMethodDescr(dictItemIterType, "__reduce__", dictReduceFn))
	SetTypeDescr(dictReverseKeyIterType, "__reduce__", NewMethodDescr(dictReverseKeyIterType, "__reduce__", dictReduceFn))
	SetTypeDescr(dictReverseValueIterType, "__reduce__", NewMethodDescr(dictReverseValueIterType, "__reduce__", dictReduceFn))
	SetTypeDescr(dictReverseItemIterType, "__reduce__", NewMethodDescr(dictReverseItemIterType, "__reduce__", dictReduceFn))

	// CPython: Objects/dictobject.c:5300 dictiter_len
	// Returns 0 if the dict was mutated (size mismatch) so callers don't
	// over-allocate when length is no longer trustworthy.
	dictLenHintFn := func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: __length_hint__ takes no arguments")
		}
		it := args[0].(*dictIterObj)
		if it.src == nil {
			return NewInt(0), nil
		}
		if it.snapUsed != it.src.used {
			return NewInt(0), nil
		}
		remaining := 0
		if it.reversed {
			for p := it.pos; p >= 0; p-- {
				if it.src.slotIsLive(it.src.order[p]) {
					remaining++
				}
			}
		} else {
			for p := it.pos; p < len(it.src.order); p++ {
				if it.src.slotIsLive(it.src.order[p]) {
					remaining++
				}
			}
		}
		return NewInt(int64(remaining)), nil
	}
	SetTypeDescr(dictKeyIterType, "__length_hint__", NewMethodDescr(dictKeyIterType, "__length_hint__", dictLenHintFn))
	SetTypeDescr(dictValueIterType, "__length_hint__", NewMethodDescr(dictValueIterType, "__length_hint__", dictLenHintFn))
	SetTypeDescr(dictItemIterType, "__length_hint__", NewMethodDescr(dictItemIterType, "__length_hint__", dictLenHintFn))
	SetTypeDescr(dictReverseKeyIterType, "__length_hint__", NewMethodDescr(dictReverseKeyIterType, "__length_hint__", dictLenHintFn))
	SetTypeDescr(dictReverseValueIterType, "__length_hint__", NewMethodDescr(dictReverseValueIterType, "__length_hint__", dictLenHintFn))
	SetTypeDescr(dictReverseItemIterType, "__length_hint__", NewMethodDescr(dictReverseItemIterType, "__length_hint__", dictLenHintFn))
}

// dictIter is the type-level Iter slot that DictType registers in
// its init. Returns a key-iterator since iterating a dict yields its
// keys.
//
// CPython: Objects/dictobject.c:4978 dict_iter
func dictIter(o Object) (Object, error) {
	return newDictIter(o.(*Dict), dictIterKeys), nil
}

func newDictIter(d *Dict, kind dictIterKind) *dictIterObj {
	it := &dictIterObj{src: d, snapUsed: d.used, snapStruct: d.structVersion, length: d.used, kind: kind, owns: true}
	// Hold a strong reference to the dict for the iterator's lifetime so
	// a dict whose only other owner is consumed (e.g. the temporary that
	// `for k in globals()` iterates) cannot reach refcount zero and run
	// dict_dealloc out from under the walk.
	//
	// CPython: Objects/dictobject.c:5132 dictiter_new (di_dict = Py_NEWREF(dict))
	Incref(d)
	switch kind {
	case dictIterKeys:
		it.init(dictKeyIterType)
	case dictIterValues:
		it.init(dictValueIterType)
	case dictIterItems:
		it.init(dictItemIterType)
	}
	return it
}

// newDictIterReversed builds a tail-to-head walker. pos starts at the
// last order slot so advance() yields entries in reverse insertion
// order. An empty dict gives pos == -1, which advance() reports as
// immediate StopIteration.
//
// CPython: Objects/dictobject.c:5408 dict___reversed___impl
// dictReversedMethod implements dict.__reversed__(): a reverse key
// iterator over the dict.
//
// CPython: Objects/dictobject.c:5408 dict___reversed___impl
func dictReversedMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __reversed__() takes no arguments")
	}
	d, ok := args[0].(*Dict)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__reversed__' requires a 'dict' object")
	}
	return newDictIterReversed(d, dictIterKeys), nil
}

func newDictIterReversed(d *Dict, kind dictIterKind) *dictIterObj {
	it := &dictIterObj{src: d, snapUsed: d.used, snapStruct: d.structVersion, length: d.used, kind: kind, owns: true, reversed: true}
	it.pos = len(d.order) - 1
	Incref(d)
	switch kind {
	case dictIterKeys:
		it.init(dictReverseKeyIterType)
	case dictIterValues:
		it.init(dictReverseValueIterType)
	case dictIterItems:
		it.init(dictReverseItemIterType)
	}
	return it
}

// dictIterDealloc drops the strong reference the iterator holds on its
// source dict.
//
// CPython: Objects/dictobject.c:5147 dictiter_dealloc (Py_XDECREF(di->di_dict))
func dictIterDealloc(o Object) {
	it, ok := o.(*dictIterObj)
	if !ok {
		return
	}
	if it.owns && it.src != nil {
		Decref(it.src)
	}
	it.src = nil
	it.owns = false
}

// advance walks past dummy and empty slots until it finds the next
// active entry. The size-change check fires before the walk so a
// mutation that happened during the previous iternext (between the
// caller seeing the value and asking for the next one) is caught.
//
// CPython: Objects/dictobject.c:5237 (the di_used != ma_used branch)
// advance returns (key, value, error). The dictEntry struct can't
// carry per-instance split values, so iteration returns key+value
// pairs directly. The size-change check fires before the walk so a
// mutation that happened during the previous iternext is caught.
func (it *dictIterObj) advance() (Object, Object, error) {
	if it.src == nil {
		return nil, nil, ErrStopIteration
	}
	if it.snapUsed != it.src.used {
		if it.owns {
			Decref(it.src)
			it.owns = false
		}
		it.src = nil
		return nil, nil, fmt.Errorf("RuntimeError: dictionary changed size during iteration")
	}
	if it.snapStruct != it.src.structVersion {
		if it.owns {
			Decref(it.src)
			it.owns = false
		}
		it.src = nil
		return nil, nil, fmt.Errorf("RuntimeError: dictionary keys changed during iteration")
	}
	if it.reversed {
		for it.pos >= 0 {
			slot := it.src.order[it.pos]
			it.pos--
			if it.src.slotIsLive(slot) {
				if err := it.consume(); err != nil {
					return nil, nil, err
				}
				return it.src.slotKey(slot), it.src.slotValue(slot), nil
			}
		}
		return nil, nil, ErrStopIteration
	}
	for it.pos < len(it.src.order) {
		slot := it.src.order[it.pos]
		it.pos++
		if it.src.slotIsLive(slot) {
			if err := it.consume(); err != nil {
				return nil, nil, err
			}
			return it.src.slotKey(slot), it.src.slotValue(slot), nil
		}
	}
	return nil, nil, ErrStopIteration
}

// consume accounts for one yielded entry. Finding a live entry after
// di->len has already reached zero means the table grew via a
// delete+reinsert that left ma_used unchanged, so the size-change check
// did not fire; CPython reports this as a keys-changed RuntimeError.
//
// CPython: Objects/dictobject.c:5279 (the di->len == 0 branch)
func (it *dictIterObj) consume() error {
	if it.length == 0 {
		if it.owns {
			Decref(it.src)
			it.owns = false
		}
		it.src = nil
		return fmt.Errorf("RuntimeError: dictionary keys changed during iteration")
	}
	it.length--
	return nil
}

func dictIterNextKey(o Object) (Object, error) {
	k, _, err := o.(*dictIterObj).advance()
	if err != nil {
		return nil, err
	}
	return k, nil
}

func dictIterNextValue(o Object) (Object, error) {
	_, v, err := o.(*dictIterObj).advance()
	if err != nil {
		return nil, err
	}
	return v, nil
}

func dictIterNextItem(o Object) (Object, error) {
	k, v, err := o.(*dictIterObj).advance()
	if err != nil {
		return nil, err
	}
	return NewTuple([]Object{k, v}), nil
}

// dictView is the shared payload behind dict.keys()/values()/items().
// It carries a back-reference to the source dict and the view kind;
// the view types differ in which iterator kind their __iter__ slot
// constructs and which protocols they expose (only keys() and items()
// are set-like).
//
// CPython: Objects/dictobject.c:5928 _PyDictView_New
type dictView struct {
	Header
	src  *Dict
	kind dictIterKind
}

var (
	dictKeysViewType   = NewType("dict_keys", []*Type{objectType})
	dictValuesViewType = NewType("dict_values", []*Type{objectType})
	dictItemsViewType  = NewType("dict_items", []*Type{objectType})
)

func init() {
	dictKeysViewType.Iter = dictViewIter
	dictKeysViewType.Sequence = &SequenceMethods{
		Length:   dictViewLen,
		Contains: dictKeysViewContains,
	}
	dictKeysViewType.Number = &NumberMethods{
		And:      dictKeysViewAnd,
		Or:       dictKeysViewOr,
		Subtract: dictKeysViewSub,
		Xor:      dictKeysViewXor,
	}
	dictKeysViewType.Repr = dictViewRepr
	dictKeysViewType.RichCmp = dictKeysViewRichCmp

	dictValuesViewType.Iter = dictViewIter
	dictValuesViewType.Sequence = &SequenceMethods{
		Length: dictViewLen,
	}
	dictValuesViewType.Repr = dictViewRepr

	dictItemsViewType.Iter = dictViewIter
	dictItemsViewType.Sequence = &SequenceMethods{
		Length:   dictViewLen,
		Contains: dictItemsViewContains,
	}
	dictItemsViewType.Number = &NumberMethods{
		And:      dictItemsViewAnd,
		Or:       dictItemsViewOr,
		Subtract: dictItemsViewSub,
		Xor:      dictItemsViewXor,
	}
	dictItemsViewType.Repr = dictViewRepr
	// dictviews_richcompare is shared between keys and items views; both
	// lift to sets and compare. values views are not set-like, so they
	// keep object identity comparison.
	//
	// CPython: Objects/dictobject.c:6447 dictviews_richcompare
	dictItemsViewType.RichCmp = dictKeysViewRichCmp

	// reversed() over a view walks the backing dict's entries tail-first.
	//
	// CPython: Objects/dictobject.c:6588 dictkeys_reversed
	//          Objects/dictobject.c:6790 dictvalues_reversed
	//          Objects/dictobject.c:6700 dictitems_reversed
	for _, vt := range []*Type{dictKeysViewType, dictValuesViewType, dictItemsViewType} {
		SetTypeDescr(vt, "__reversed__", NewMethodDescr(vt, "__reversed__", dictViewReversed))
		SetTypeDescr(vt, "mapping", NewGetSetDescr("mapping", dictViewMappingGet, nil))
	}
	// isdisjoint is only defined on the set-like views (keys, items).
	//
	// CPython: Objects/dictobject.c:6650 dictkeys_methods / dictitems_methods
	SetTypeDescr(dictKeysViewType, "isdisjoint", NewMethodDescr(dictKeysViewType, "isdisjoint", dictViewIsDisjoint))
	SetTypeDescr(dictItemsViewType, "isdisjoint", NewMethodDescr(dictItemsViewType, "isdisjoint", dictViewIsDisjoint))
}

// dictViewReversed implements view.__reversed__(): a reverse iterator
// over the backing dict in the view's kind.
//
// CPython: Objects/dictobject.c:6588 dictkeys_reversed
func dictViewReversed(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __reversed__() takes no arguments")
	}
	v := args[0].(*dictView)
	return newDictIterReversed(v.src, v.kind), nil
}

// dictViewMappingGet returns a read-only mappingproxy over the backing
// dict, exposed as the view's .mapping attribute.
//
// CPython: Objects/dictobject.c:6175 dictview_mapping
func dictViewMappingGet(o Object) (Object, error) {
	return NewMappingProxy(o.(*dictView).src)
}

// dictViewIsDisjoint reports whether the view and other share no
// elements, draining the smaller operand against the larger.
//
// CPython: Objects/dictobject.c:6332 dictviews_isdisjoint
func dictViewIsDisjoint(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: isdisjoint() takes exactly one argument")
	}
	v := args[0].(*dictView)
	left, err := dictViewToSet(v)
	if err != nil {
		return nil, err
	}
	right, err := otherToSet(args[1])
	if err != nil {
		return nil, err
	}
	dis, err := left.IsDisjoint(right)
	if err != nil {
		return nil, err
	}
	return NewBool(dis), nil
}

// KeysView returns a dict_keys view over d. The view stays bound to
// d, so subsequent mutations show through.
//
// CPython: Objects/dictobject.c:6562 dict_keys_impl
func (d *Dict) KeysView() Object {
	v := &dictView{src: d, kind: dictIterKeys}
	v.init(dictKeysViewType)
	return v
}

// ValuesView returns a dict_values view over d.
//
// CPython: Objects/dictobject.c:6764 dict_values_impl
func (d *Dict) ValuesView() Object {
	v := &dictView{src: d, kind: dictIterValues}
	v.init(dictValuesViewType)
	return v
}

// ItemsView returns a dict_items view over d.
//
// CPython: Objects/dictobject.c:6674 dict_items_impl
func (d *Dict) ItemsView() Object {
	v := &dictView{src: d, kind: dictIterItems}
	v.init(dictItemsViewType)
	return v
}

func dictViewIter(o Object) (Object, error) {
	v := o.(*dictView)
	return newDictIter(v.src, v.kind), nil
}

func dictViewLen(o Object) (int, error) {
	return o.(*dictView).src.used, nil
}

// dictKeysViewContains routes through PyDict_Contains.
//
// CPython: Objects/dictobject.c:5973 dictview_contains
func dictKeysViewContains(o, key Object) (bool, error) {
	return o.(*dictView).src.Contains(key)
}

// dictItemsViewContains accepts a 2-tuple (k, v); the item is in the
// view iff the key is in the dict and the dict's value compares
// equal to v under richcompare.
//
// CPython: Objects/dictobject.c:6716 dictitems_contains
func dictItemsViewContains(o, key Object) (bool, error) {
	t, ok := key.(*Tuple)
	if !ok || t.Len() != 2 {
		return false, nil
	}
	got, err := o.(*dictView).src.GetItem(t.Item(0))
	if err != nil {
		if errors.Is(err, errKeyNotFound) {
			return false, nil
		}
		return false, err
	}
	return RichCmpBool(got, t.Item(1), CompareEQ)
}

func dictViewRepr(o Object) (string, error) {
	v := o.(*dictView)
	name := v.Type().Name
	body, err := dictViewIterRepr(v)
	if err != nil {
		return "", err
	}
	return name + "(" + body + ")", nil
}

// dictViewIterRepr drains the view into the "[k1, k2, ...]" body that
// dict_keys/values/items repr wraps in their type name.
//
// CPython: Objects/dictobject.c:6109 dictview_repr
func dictViewIterRepr(v *dictView) (string, error) {
	out := "["
	first := true
	it, err := dictViewIter(v)
	if err != nil {
		return "", err
	}
	for {
		item, err := IterNext(it)
		if errors.Is(err, ErrStopIteration) {
			break
		}
		if err != nil {
			return "", err
		}
		if !first {
			out += ", "
		}
		first = false
		s, err := Repr(item)
		if err != nil {
			return "", err
		}
		out += s
	}
	out += "]"
	return out, nil
}

// View set algebra: keys() and items() are set-like, so &, |, -, ^
// route into the existing set machinery. CPython lifts the view into
// a plain set and delegates to set_intersection / set_union /
// set_difference / set_symmetric_difference.
//
// CPython: Objects/dictobject.c:6207 dictviews_and
//          Objects/dictobject.c:6230 dictviews_or
//          Objects/dictobject.c:6256 dictviews_sub
//          Objects/dictobject.c:6286 dictviews_xor

func dictViewToSet(v *dictView) (*Set, error) {
	out := NewSet()
	it, err := dictViewIter(v)
	if err != nil {
		return nil, err
	}
	for {
		item, err := IterNext(it)
		if errors.Is(err, ErrStopIteration) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if err := out.Add(item); err != nil {
			return nil, err
		}
	}
}

// otherToSet drains an arbitrary iterable into a set so binary view
// operators can accept any iterable, not just another view.
func otherToSet(o Object) (*Set, error) {
	if s, ok := o.(*Set); ok {
		return s, nil
	}
	if v, ok := o.(*dictView); ok {
		return dictViewToSet(v)
	}
	out := NewSet()
	it, err := Iter(o)
	if err != nil {
		return nil, err
	}
	for {
		item, err := IterNext(it)
		if errors.Is(err, ErrStopIteration) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if err := out.Add(item); err != nil {
			return nil, err
		}
	}
}

func dictViewBinop(a, b Object, op func(left, right *Set) (*Set, error)) (Object, error) {
	v, ok := a.(*dictView)
	if !ok {
		return NotImplemented(), nil
	}
	left, err := dictViewToSet(v)
	if err != nil {
		return nil, err
	}
	right, err := otherToSet(b)
	if err != nil {
		return nil, err
	}
	return op(left, right)
}

// dictKeysViewRichCmp implements set-like comparisons for dict_keys views.
// Mirrors CPython's dictviews_richcompare: converts both operands to sets
// and compares them.
//
// CPython: Objects/dictobject.c:6447 dictviews_richcompare
func dictKeysViewRichCmp(a, b Object, op CompareOp) (Object, error) {
	v, ok := a.(*dictView)
	if !ok {
		return NotImplemented(), nil
	}
	aSet, err := dictViewToSet(v)
	if err != nil {
		return nil, err
	}
	bSet, err := otherToSet(b)
	if err != nil {
		return NotImplemented(), nil //nolint:nilerr // mirrors Py_NotImplemented return when other can't be coerced to set
	}
	return setRichCmp(aSet, bSet, op)
}

func dictKeysViewAnd(a, b Object) (Object, error) {
	return dictViewBinop(a, b, func(l, r *Set) (*Set, error) { return l.Intersection(r) })
}

func dictKeysViewOr(a, b Object) (Object, error) {
	return dictViewBinop(a, b, func(l, r *Set) (*Set, error) { return l.Union(r) })
}

func dictKeysViewSub(a, b Object) (Object, error) {
	return dictViewBinop(a, b, func(l, r *Set) (*Set, error) { return l.Difference(r) })
}

func dictKeysViewXor(a, b Object) (Object, error) {
	return dictViewBinop(a, b, func(l, r *Set) (*Set, error) { return l.SymmetricDifference(r) })
}

func dictItemsViewAnd(a, b Object) (Object, error) { return dictKeysViewAnd(a, b) }
func dictItemsViewOr(a, b Object) (Object, error)  { return dictKeysViewOr(a, b) }
func dictItemsViewSub(a, b Object) (Object, error) { return dictKeysViewSub(a, b) }
func dictItemsViewXor(a, b Object) (Object, error) { return dictKeysViewXor(a, b) }
