// OrderedDict is the C accelerator for collections.OrderedDict.
// Distinct from dict despite both preserving insertion order: equality
// is order-sensitive, move_to_end re-orders without re-inserting, and
// popitem accepts last=True/False to pop from either end.
//
// CPython keeps a hash table (the embedded PyDictObject) and a
// doubly-linked list of nodes that mirror the live dict slots; the
// list is what defines iteration order. gopy follows the same shape
// but stores the linked-list nodes in a parallel inner Dict whose
// values are *odictNode. That gives O(1) key-to-node lookup without
// the od_fast_nodes resize sentinel CPython tracks against the inner
// dict's address.
//
// CPython: Objects/odictobject.c:489 PyODictObject

package objects

import (
	"errors"
	"fmt"
	"strings"
)

// odictNode is one entry in OrderedDict's doubly-linked list. CPython
// allocates each node with PyMem_Malloc and stores the key plus its
// hash; the value lives back in the embedded dict. gopy collapses
// both into the node since the inner dict only stores the node
// itself, not the user value. odictNode embeds Header so it satisfies
// the Object interface (the inner dict's value slot needs an Object);
// it's a private type and never escapes the OrderedDict surface.
//
// CPython: Objects/odictobject.c:512 _odictnode
type odictNode struct {
	Header
	key   Object
	value Object
	hash  int64
	prev  *odictNode
	next  *odictNode
}

var odictNodeType = NewType("_odictnode", []*Type{objectType})

// OrderedDict is the OrderedDict singleton. Mirrors PyODictObject.
//
// CPython: Objects/odictobject.c:489 _odictobject
type OrderedDict struct {
	Header
	inner *Dict      // key -> *odictNode
	first *odictNode // head of insertion-order list
	last  *odictNode // tail of insertion-order list
	state uint64     // bumps on link-list mutation; iter checks
}

// OrderedDictType is the type singleton for collections.OrderedDict.
//
// CPython: Objects/odictobject.c:1840 PyODict_Type
var OrderedDictType = NewType("OrderedDict", []*Type{DictType})

// AsDictBacking exposes the inner *Dict (the hash table half of the
// odict layout) so dict's tp_richcompare and MATCH_KEYS treat an
// OrderedDict as a dict subclass.
//
// CPython: Objects/odictobject.c:489 _odictobject (PyDictObject substructure)
func (od *OrderedDict) AsDictBacking() *Dict { return od.inner }

func init() {
	OrderedDictType.TpFlags |= TpFlagMapping
	OrderedDictType.Repr = odictRepr
	OrderedDictType.Str = odictRepr
	OrderedDictType.Iter = odictIter
	OrderedDictType.RichCmp = odictRichCompare
	OrderedDictType.Mapping = &MappingMethods{
		Length:  odictLen,
		GetItem: odictMappingGet,
		SetItem: odictMappingSet,
		DelItem: odictMappingDel,
	}
	OrderedDictType.TpTraverse = odictTraverse
}

// NewOrderedDict builds an empty OrderedDict.
//
// CPython: Objects/odictobject.c:1693 PyODict_New
func NewOrderedDict() *OrderedDict {
	od := &OrderedDict{inner: NewDict()}
	od.init(OrderedDictType)
	return od
}

// Len returns the number of items.
func (od *OrderedDict) Len() int { return od.inner.Len() }

// SetItem inserts or replaces. New keys go to the tail of the link
// list; replacement of an existing key leaves the order alone.
//
// CPython: Objects/odictobject.c:1622 _PyODict_SetItem_KnownHash_LockHeld
func (od *OrderedDict) SetItem(key, value Object) error {
	got, err := od.inner.GetItem(key)
	if err == nil {
		got.(*odictNode).value = value
		return nil
	}
	if !errors.Is(err, errKeyNotFound) {
		return err
	}
	h, err := Hash(key)
	if err != nil {
		return err
	}
	n := &odictNode{key: key, value: value, hash: h}
	n.init(odictNodeType)
	od.addTail(n)
	return od.inner.SetItem(key, n)
}

// GetItem looks up key. Returns errKeyNotFound when absent.
func (od *OrderedDict) GetItem(key Object) (Object, error) {
	got, err := od.inner.GetItem(key)
	if err != nil {
		return nil, err
	}
	return got.(*odictNode).value, nil
}

// DelItem removes key, unlinking the node from the order list.
//
// CPython: Objects/odictobject.c:1662 PyODict_DelItem_LockHeld
func (od *OrderedDict) DelItem(key Object) error {
	got, err := od.inner.GetItem(key)
	if err != nil {
		return err
	}
	od.removeNode(got.(*odictNode))
	return od.inner.DelItem(key)
}

// Contains reports whether key is in the dict.
func (od *OrderedDict) Contains(key Object) (bool, error) {
	return od.inner.Contains(key)
}

// MoveToEnd re-positions an existing key to the tail of the link list
// (last=true) or the head (last=false). Raises KeyError when absent.
//
// CPython: Objects/odictobject.c:1325 OrderedDict_move_to_end_impl
func (od *OrderedDict) MoveToEnd(key Object, last bool) error {
	got, err := od.inner.GetItem(key)
	if err != nil {
		return err
	}
	n := got.(*odictNode)
	if last {
		if n == od.last {
			return nil
		}
		od.unlink(n)
		od.addTail(n)
	} else {
		if n == od.first {
			return nil
		}
		od.unlink(n)
		od.addHead(n)
	}
	return nil
}

// PopItem removes and returns the (key, value) at the tail (last=true)
// or head (last=false). Raises KeyError on an empty dict.
//
// CPython: Objects/odictobject.c:1155 OrderedDict_popitem_impl
func (od *OrderedDict) PopItem(last bool) (Object, Object, error) {
	if od.first == nil {
		return nil, nil, fmt.Errorf("KeyError: dictionary is empty")
	}
	n := od.last
	if !last {
		n = od.first
	}
	if err := od.DelItem(n.key); err != nil {
		return nil, nil, err
	}
	return n.key, n.value, nil
}

// addTail appends node to the end of the linked list.
//
// CPython: Objects/odictobject.c:668 _odict_add_tail
func (od *OrderedDict) addTail(n *odictNode) {
	n.prev = od.last
	n.next = nil
	if od.last == nil {
		od.first = n
	} else {
		od.last.next = n
	}
	od.last = n
	od.state++
}

// addHead prepends node to the start of the linked list.
//
// CPython: Objects/odictobject.c:654 _odict_add_head
func (od *OrderedDict) addHead(n *odictNode) {
	n.prev = nil
	n.next = od.first
	if od.first == nil {
		od.last = n
	} else {
		od.first.prev = n
	}
	od.first = n
	od.state++
}

// unlink severs node from the list without freeing it; the caller
// either reinserts (move_to_end) or hands the node to the dict's
// remove path.
//
// CPython: Objects/odictobject.c:728 _odict_remove_node
func (od *OrderedDict) unlink(n *odictNode) {
	if n.prev != nil {
		n.prev.next = n.next
	} else {
		od.first = n.next
	}
	if n.next != nil {
		n.next.prev = n.prev
	} else {
		od.last = n.prev
	}
	n.prev = nil
	n.next = nil
	od.state++
}

// removeNode is the unlink-and-discard path delete uses. Splitting
// out of the inner dict happens in DelItem; this only handles the
// list bookkeeping.
func (od *OrderedDict) removeNode(n *odictNode) { od.unlink(n) }

// odictKeysEqual walks both lists in parallel and confirms the keys
// match position-by-position. The state-pin is CPython's guard
// against an __eq__ side effect mutating either operand mid-walk.
//
// CPython: Objects/odictobject.c:820 _odict_keys_equal
func odictKeysEqual(a, b *OrderedDict) (bool, error) {
	stateA, stateB := a.state, b.state
	na, nb := a.first, b.first
	for {
		if na == nil && nb == nil {
			return true, nil
		}
		if na == nil || nb == nil {
			return false, nil
		}
		eq, err := RichCmpBool(na.key, nb.key, CompareEQ)
		if err != nil {
			return false, err
		}
		if a.state != stateA || b.state != stateB {
			return false, fmt.Errorf("RuntimeError: OrderedDict mutated during iteration")
		}
		if !eq {
			return false, nil
		}
		na = na.next
		nb = nb.next
	}
}

func odictLen(o Object) (int, error) { return o.(*OrderedDict).Len(), nil }

func odictMappingGet(o, key Object) (Object, error) { return o.(*OrderedDict).GetItem(key) }

func odictMappingSet(o, key, value Object) error { return o.(*OrderedDict).SetItem(key, value) }

func odictMappingDel(o, key Object) error { return o.(*OrderedDict).DelItem(key) }

// odictTraverse visits every key and value via the link list, which
// matches iteration order. Walking the inner dict's entry table would
// also work but might surface keys in hash-bucket order.
//
// CPython: Objects/odictobject.c:1461 odict_traverse
func odictTraverse(o Object, visit Visitor) error {
	od := o.(*OrderedDict)
	for n := od.first; n != nil; n = n.next {
		if err := visit(n.key); err != nil {
			return err
		}
		if err := visit(n.value); err != nil {
			return err
		}
	}
	return nil
}

// odictRichCompare implements PyODict's equality. Two ODicts compare
// equal iff (a) they have the same key/value pairs and (b) the link
// list order matches. An ODict against a plain dict only needs (a),
// matching dict's own order-insensitive equality.
//
// CPython: Objects/odictobject.c:1489 odict_richcompare_lock_held
func odictRichCompare(v, w Object, op CompareOp) (Object, error) {
	if op != CompareEQ && op != CompareNE {
		return notImplemented(), nil
	}
	odV, ok := v.(*OrderedDict)
	if !ok {
		return notImplemented(), nil
	}
	switch r := w.(type) {
	case *OrderedDict:
		eq, err := odictPairsEqual(odV, r)
		if err != nil {
			return nil, err
		}
		if !eq {
			return NewBool(op == CompareNE), nil
		}
		keysEq, err := odictKeysEqual(odV, r)
		if err != nil {
			return nil, err
		}
		if op == CompareEQ {
			return NewBool(keysEq), nil
		}
		return NewBool(!keysEq), nil
	case *Dict:
		eq, err := odictMatchesDict(odV, r)
		if err != nil {
			return nil, err
		}
		if op == CompareEQ {
			return NewBool(eq), nil
		}
		return NewBool(!eq), nil
	default:
		return notImplemented(), nil
	}
}

// odictPairsEqual walks one ODict's link list and confirms every
// (key, value) shows up in the other with a richcompare-equal value.
// Length is matched first so a shorter list with all-matching prefix
// can't pass.
func odictPairsEqual(a, b *OrderedDict) (bool, error) {
	if a.Len() != b.Len() {
		return false, nil
	}
	for n := a.first; n != nil; n = n.next {
		got, err := b.GetItem(n.key)
		if err != nil {
			if errors.Is(err, errKeyNotFound) {
				return false, nil
			}
			return false, err
		}
		eq, err := RichCmpBool(n.value, got, CompareEQ)
		if err != nil {
			return false, err
		}
		if !eq {
			return false, nil
		}
	}
	return true, nil
}

// odictMatchesDict is the ODict-against-plain-dict comparison: it's
// the same as dict-equal-dict (order doesn't matter) but has to
// unwrap the inner-dict's odictNode value to get the user value.
func odictMatchesDict(a *OrderedDict, b *Dict) (bool, error) {
	if a.Len() != b.Len() {
		return false, nil
	}
	for n := a.first; n != nil; n = n.next {
		got, err := b.GetItem(n.key)
		if err != nil {
			if errors.Is(err, errKeyNotFound) {
				return false, nil
			}
			return false, err
		}
		eq, err := RichCmpBool(n.value, got, CompareEQ)
		if err != nil {
			return false, err
		}
		if !eq {
			return false, nil
		}
	}
	return true, nil
}

// odictIter returns a key iterator that walks the link list. Unlike
// dict's iterator it doesn't snapshot ma_used since the link list is
// what defines order; mid-iteration mutations are caught by checking
// state.
//
// CPython: Objects/odictobject.c:1535 odict_iter
func odictIter(o Object) (Object, error) {
	od := o.(*OrderedDict)
	it := &odictIterObj{src: od, next: od.first, snapState: od.state}
	it.init(odictKeyIterType)
	return it, nil
}

// odictIterObj is the live walker. snapState pins state at iter
// creation and the next iternext after a link-list mutation raises
// RuntimeError.
//
// CPython: Objects/odictobject.c:1689 odictiterobject
type odictIterObj struct {
	Header
	src       *OrderedDict
	next      *odictNode
	snapState uint64
}

var odictKeyIterType = NewType("odict_keyiterator", []*Type{objectType})

func init() {
	odictKeyIterType.Iter = func(o Object) (Object, error) { return o, nil }
	odictKeyIterType.IterNext = odictIterNextKey
	AddIterSlotWrappers(odictKeyIterType)
}

func odictIterNextKey(o Object) (Object, error) {
	it := o.(*odictIterObj)
	if it.src == nil || it.next == nil {
		return nil, ErrStopIteration
	}
	if it.src.state != it.snapState {
		it.src = nil
		return nil, fmt.Errorf("RuntimeError: OrderedDict mutated during iteration")
	}
	n := it.next
	it.next = n.next
	return n.key, nil
}

// odictRepr formats as "OrderedDict({'a': 1, 'b': 2})". CPython uses
// the inner list-of-tuples form for backwards compatibility, but
// 3.12+ switched to the dict-literal display so we follow that.
//
// CPython: Objects/odictobject.c:1424 odict_repr
func odictRepr(o Object) (string, error) {
	od := o.(*OrderedDict)
	if od.first == nil {
		return "OrderedDict()", nil
	}
	var b strings.Builder
	b.WriteString("OrderedDict({")
	first := true
	for n := od.first; n != nil; n = n.next {
		if !first {
			b.WriteString(", ")
		}
		first = false
		ks, err := Repr(n.key)
		if err != nil {
			return "", err
		}
		vs, err := Repr(n.value)
		if err != nil {
			return "", err
		}
		b.WriteString(ks)
		b.WriteString(": ")
		b.WriteString(vs)
	}
	b.WriteString("})")
	return b.String(), nil
}
