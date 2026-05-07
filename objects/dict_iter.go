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
	kind     dictIterKind
}

var (
	dictKeyIterType   = NewType("dict_keyiterator", []*Type{objectType})
	dictValueIterType = NewType("dict_valueiterator", []*Type{objectType})
	dictItemIterType  = NewType("dict_itemiterator", []*Type{objectType})
)

func init() {
	identity := func(o Object) (Object, error) { return o, nil }

	dictKeyIterType.Iter = identity
	dictKeyIterType.IterNext = dictIterNextKey

	dictValueIterType.Iter = identity
	dictValueIterType.IterNext = dictIterNextValue

	dictItemIterType.Iter = identity
	dictItemIterType.IterNext = dictIterNextItem
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
	it := &dictIterObj{src: d, snapUsed: d.used, kind: kind}
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

// advance walks past dummy and empty slots until it finds the next
// active entry. The size-change check fires before the walk so a
// mutation that happened during the previous iternext (between the
// caller seeing the value and asking for the next one) is caught.
//
// CPython: Objects/dictobject.c:5237 (the di_used != ma_used branch)
func (it *dictIterObj) advance() (*dictEntry, error) {
	if it.src == nil {
		return nil, ErrStopIteration
	}
	if it.snapUsed != it.src.used {
		it.src = nil
		return nil, fmt.Errorf("RuntimeError: dictionary changed size during iteration")
	}
	for it.pos < len(it.src.entries) {
		e := &it.src.entries[it.pos]
		it.pos++
		if e.used {
			return e, nil
		}
	}
	return nil, ErrStopIteration
}

func dictIterNextKey(o Object) (Object, error) {
	e, err := o.(*dictIterObj).advance()
	if err != nil {
		return nil, err
	}
	return e.key, nil
}

func dictIterNextValue(o Object) (Object, error) {
	e, err := o.(*dictIterObj).advance()
	if err != nil {
		return nil, err
	}
	return e.value, nil
}

func dictIterNextItem(o Object) (Object, error) {
	e, err := o.(*dictIterObj).advance()
	if err != nil {
		return nil, err
	}
	return NewTuple([]Object{e.key, e.value}), nil
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
