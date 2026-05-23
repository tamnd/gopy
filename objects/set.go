// Set and frozenset objects. Mirrors CPython's open-addressed hash
// table layout. frozenset is an immutable set that supports all read
// operations but disallows mutation; it stores its hash lazily.
//
// CPython: Objects/setobject.c
package objects

import (
	"errors"
	"fmt"
	"strings"
)

// setEntry is one slot in the set's open-addressed table.
//
// CPython: Objects/setobject.c:L78 setentry
type setEntry struct {
	hash int64
	key  Object
	used bool
}

// Set is the mutable Python set type.
//
// CPython: Objects/setobject.c:L544 PySetObject
type Set struct {
	Header
	entries    []setEntry
	used       int
	frozen     bool
	cachedHash int64 // only valid for frozenset when hashValid is true
	hashValid  bool
}

// SetType is the type singleton for set.
//
// CPython: Objects/setobject.c:L2318 PySet_Type
var SetType = NewType("set", []*Type{objectType})

// FrozensetType is the type singleton for frozenset.
//
// CPython: Objects/setobject.c:L2468 PyFrozenSet_Type
var FrozensetType = NewType("frozenset", []*Type{objectType})

const setMinSize = 8

func init() {
	SetType.Repr = setRepr
	SetType.Str = setRepr
	SetType.Hash = nil // sets are not hashable
	SetType.RichCmp = setRichCmp
	SetType.TpFlags |= TpFlagMatchSelf
	FrozensetType.TpFlags |= TpFlagMatchSelf
	SetType.Iter = setIter
	SetType.Sequence = &SequenceMethods{
		Length:   setLen,
		Contains: setContains,
	}
	SetType.TpTraverse = setTraverse
	SetType.Getattro = GenericGetAttr
	SetType.Number = &NumberMethods{
		And:        setAnd,
		Or:         setOr,
		Subtract:   setSubtract,
		Xor:        setXor,
		InPlaceAnd: setIAnd,
		InPlaceOr:  setIOr,
		InPlaceXor: setIXor,
	}
	SetTypeDescr(SetType, "__contains__", NewMethodDescr(SetType, "__contains__", setContainsMethod))
	SetTypeDescr(SetType, "add", NewMethodDescr(SetType, "add", setAddMethod))
	SetTypeDescr(SetType, "discard", NewMethodDescr(SetType, "discard", setDiscardMethod))
	SetTypeDescr(SetType, "remove", NewMethodDescr(SetType, "remove", setRemoveMethod))
	SetTypeDescr(SetType, "pop", NewMethodDescr(SetType, "pop", setPopMethod))
	SetTypeDescr(SetType, "clear", NewMethodDescr(SetType, "clear", setClearMethod))
	SetTypeDescr(SetType, "update", NewMethodDescr(SetType, "update", setUpdateMethod))
	SetTypeDescr(SetType, "intersection", NewMethodDescr(SetType, "intersection", setIntersectionMethod))
	SetTypeDescr(SetType, "union", NewMethodDescr(SetType, "union", setUnionMethod))
	SetTypeDescr(SetType, "difference", NewMethodDescr(SetType, "difference", setDifferenceMethod))
	SetTypeDescr(SetType, "issubset", NewMethodDescr(SetType, "issubset", setIsSubsetMethod))
	SetTypeDescr(SetType, "issuperset", NewMethodDescr(SetType, "issuperset", setIsSupersetMethod))
	SetTypeDescr(SetType, "isdisjoint", NewMethodDescr(SetType, "isdisjoint", setIsDisjointMethod))
	SetTypeDescr(SetType, "intersection_update", NewMethodDescr(SetType, "intersection_update", setIntersectionUpdateMethod))
	SetTypeDescr(SetType, "difference_update", NewMethodDescr(SetType, "difference_update", setDifferenceUpdateMethod))
	SetTypeDescr(SetType, "symmetric_difference", NewMethodDescr(SetType, "symmetric_difference", setSymmetricDifferenceMethod))
	SetTypeDescr(SetType, "symmetric_difference_update", NewMethodDescr(SetType, "symmetric_difference_update", setSymmetricDifferenceUpdateMethod))
	SetTypeDescr(SetType, "copy", NewMethodDescr(SetType, "copy", setCopyMethod))
	SetTypeDescr(SetType, "__len__", NewMethodDescr(SetType, "__len__", setLenMethod))

	FrozensetType.Repr = frozensetRepr
	FrozensetType.Str = frozensetRepr
	FrozensetType.Hash = frozensetHash
	FrozensetType.RichCmp = setRichCmp
	FrozensetType.Iter = setIter
	FrozensetType.Sequence = &SequenceMethods{
		Length:   setLen,
		Contains: setContains,
	}
	FrozensetType.TpTraverse = setTraverse
	FrozensetType.Getattro = GenericGetAttr
	FrozensetType.Number = &NumberMethods{
		And:      setAnd,
		Or:       setOr,
		Subtract: setSubtract,
		Xor:      setXor,
	}
	SetTypeDescr(FrozensetType, "__contains__", NewMethodDescr(FrozensetType, "__contains__", setContainsMethod))
	SetTypeDescr(FrozensetType, "__len__", NewMethodDescr(FrozensetType, "__len__", setLenMethod))
	SetTypeDescr(FrozensetType, "intersection", NewMethodDescr(FrozensetType, "intersection", setIntersectionMethod))
	SetTypeDescr(FrozensetType, "union", NewMethodDescr(FrozensetType, "union", setUnionMethod))
	SetTypeDescr(FrozensetType, "issubset", NewMethodDescr(FrozensetType, "issubset", setIsSubsetMethod))
	SetTypeDescr(FrozensetType, "issuperset", NewMethodDescr(FrozensetType, "issuperset", setIsSupersetMethod))
	SetTypeDescr(FrozensetType, "difference", NewMethodDescr(FrozensetType, "difference", setDifferenceMethod))
	SetTypeDescr(FrozensetType, "symmetric_difference", NewMethodDescr(FrozensetType, "symmetric_difference", setSymmetricDifferenceMethod))
	SetTypeDescr(FrozensetType, "isdisjoint", NewMethodDescr(FrozensetType, "isdisjoint", setIsDisjointMethod))
	SetTypeDescr(FrozensetType, "copy", NewMethodDescr(FrozensetType, "copy", setCopyMethod))
}

// setTraverse visits each element of a set or frozenset.
//
// CPython: Objects/setobject.c:1956 set_traverse
func setTraverse(o Object, visit Visitor) error {
	s := o.(*Set)
	for _, e := range s.entries {
		if !e.used || e.key == nil {
			continue
		}
		if err := visit(e.key); err != nil {
			return err
		}
	}
	return nil
}

// NewSet creates an empty mutable set.
//
// CPython: Objects/setobject.c:L2267 PySet_New (empty case)
func NewSet() *Set {
	s := &Set{entries: make([]setEntry, setMinSize)}
	s.init(SetType)
	return s
}

// NewFrozenset creates a frozenset from the given items.
//
// CPython: Objects/setobject.c:L2300 PyFrozenSet_New
func NewFrozenset(items []Object) (*Set, error) {
	s := &Set{entries: make([]setEntry, setMinSize), frozen: true}
	s.init(FrozensetType)
	for _, item := range items {
		if err := s.add(item); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Len returns the number of items.
//
// CPython: Objects/setobject.c:L2201 PySet_Size
func (s *Set) Len() int { return s.used }

// Add inserts key into the set. Returns an error if key is unhashable
// or if the set is frozen.
//
// CPython: Objects/setobject.c:L413 set_add_key
func (s *Set) Add(key Object) error {
	if s.frozen {
		return fmt.Errorf("TypeError: 'frozenset' object does not support item assignment")
	}
	return s.add(key)
}

// add is the internal insert that bypasses the frozen check.
func (s *Set) add(key Object) error {
	h, err := Hash(key)
	if err != nil {
		return err
	}
	s.insert(h, key)
	return nil
}

// Discard removes key if present. Does nothing on miss. Errors if frozen.
//
// CPython: Objects/setobject.c:L743 set_discard_key
func (s *Set) Discard(key Object) error {
	if s.frozen {
		return fmt.Errorf("AttributeError: 'frozenset' object has no attribute 'discard'")
	}
	h, err := Hash(key)
	if err != nil {
		return err
	}
	idx, ok, err := s.lookup(h, key)
	if err != nil || !ok {
		return err
	}
	s.entries[idx] = setEntry{}
	s.used--
	return nil
}

// Contains reports whether key is in the set.
//
// CPython: Objects/setobject.c:L1777 set_contains_key
func (s *Set) Contains(key Object) (bool, error) {
	h, err := Hash(key)
	if err != nil {
		return false, err
	}
	_, ok, err := s.lookup(h, key)
	return ok, err
}

// Items returns a snapshot of the current members.
//
// CPython: Objects/setobject.c:L1815 PySet_GET_SIZE + iteration
func (s *Set) Items() []Object {
	out := make([]Object, 0, s.used)
	for _, e := range s.entries {
		if e.used {
			out = append(out, e.key)
		}
	}
	return out
}

func (s *Set) lookup(h int64, key Object) (idx int, found bool, err error) {
	mask := uint64(len(s.entries) - 1)
	i := uint64(h) & mask
	perturb := uint64(h)
	for {
		e := &s.entries[i]
		if !e.used {
			return int(i), false, nil
		}
		if e.hash == h {
			eq, err := RichCmpBool(e.key, key, CompareEQ)
			if err != nil {
				return 0, false, err
			}
			if eq {
				return int(i), true, nil
			}
		}
		// CPython: Objects/setobject.c:L172 PERTURB_SHIFT probing
		perturb >>= 5
		i = (5*i + 1 + perturb) & mask
	}
}

// insert places (h, key) into the table, growing first if the
// fill ratio crosses 2/3. Mirrors CPython's set_add_entry which
// resizes before placing the new element.
//
// CPython: Objects/setobject.c:220 set_add_entry
func (s *Set) insert(h int64, key Object) {
	if (s.used+1)*3 >= len(s.entries)*2 {
		s.grow()
	}
	idx, ok, _ := s.lookup(h, key)
	s.entries[idx] = setEntry{hash: h, key: key, used: true}
	if !ok {
		s.used++
	}
}

// insertClean places (h, key) without checking fill ratio. Only used
// during grow's rehash, where the destination is guaranteed to have
// room. Matches CPython's set_insert_clean.
//
// CPython: Objects/setobject.c:266 set_insert_clean
func (s *Set) insertClean(h int64, key Object) {
	idx, _, _ := s.lookup(h, key)
	s.entries[idx] = setEntry{hash: h, key: key, used: true}
	s.used++
}

func (s *Set) grow() {
	old := s.entries
	s.entries = make([]setEntry, len(old)*2)
	s.used = 0
	for _, e := range old {
		if e.used {
			s.insertClean(e.hash, e.key)
		}
	}
}

func setLen(o Object) (int, error) { return o.(*Set).Len(), nil }

func setContains(o, key Object) (bool, error) { return o.(*Set).Contains(key) }

// setContainsMethod is the __contains__ method-descriptor entry. The
// receiver arrives as args[0] when the descriptor is dispatched
// unbound, and BoundMethod prepends self for the bound case.
//
// CPython: Objects/setobject.c:1804 set_contains
func setContainsMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __contains__() takes exactly one argument (%d given)", len(args)-1)
	}
	ok, err := args[0].(*Set).Contains(args[1])
	if err != nil {
		return nil, err
	}
	return NewBool(ok), nil
}

func setRepr(o Object) (string, error) {
	s := o.(*Set)
	if s.used == 0 {
		return "set()", nil
	}
	return setReprInner(s, "{", "}")
}

func frozensetRepr(o Object) (string, error) {
	s := o.(*Set)
	if s.used == 0 {
		return "frozenset()", nil
	}
	return setReprInner(s, "frozenset({", "})")
}

func setReprInner(s *Set, open, suffix string) (string, error) {
	var b strings.Builder
	b.WriteString(open)
	first := true
	for _, e := range s.entries {
		if !e.used {
			continue
		}
		if !first {
			b.WriteString(", ")
		}
		first = false
		r, err := Repr(e.key)
		if err != nil {
			return "", err
		}
		b.WriteString(r)
	}
	b.WriteString(suffix)
	return b.String(), nil
}

// frozensetHash computes the hash of a frozenset. Each element hash
// is run through _shuffle_bits before being XOR-folded so that nested
// frozensets disperse properly, the count is mixed in, and a final
// avalanche step matches CPython byte for byte.
//
// CPython: Objects/setobject.c:793 frozenset_hash
func frozensetHash(o Object) (int64, error) {
	s := o.(*Set)
	if s.hashValid {
		return s.cachedHash, nil
	}
	var h uint64
	for _, e := range s.entries {
		if !e.used {
			continue
		}
		h ^= shuffleBits(uint64(e.hash))
	}
	h ^= (uint64(s.used) + 1) * 1927868237
	h ^= (h >> 11) ^ (h >> 25)
	h = h*69069 + 907133923
	out := int64(h)
	if out == -1 {
		out = 590923713
	}
	s.cachedHash = out
	s.hashValid = true
	return out, nil
}

// shuffleBits is the per-element disperse used by frozenset_hash so
// that small element-hash differences don't collide after the XOR.
//
// CPython: Objects/setobject.c:768 _shuffle_bits
func shuffleBits(h uint64) uint64 {
	return ((h ^ 89869747) ^ (h << 16)) * 3644798167
}

// setRichCmp implements == and != for both set and frozenset by
// checking equal cardinality and mutual containment.
//
// CPython: Objects/setobject.c:L1683 set_richcompare
func setRichCmp(a, b Object, op CompareOp) (Object, error) {
	as, ok := a.(*Set)
	if !ok {
		return NotImplemented(), nil
	}
	bs, ok := b.(*Set)
	if !ok {
		return NotImplemented(), nil
	}
	switch op {
	case CompareEQ:
		eq, err := setsEqual(as, bs)
		if err != nil {
			return nil, err
		}
		return NewBool(eq), nil
	case CompareNE:
		eq, err := setsEqual(as, bs)
		if err != nil {
			return nil, err
		}
		return NewBool(!eq), nil
	}
	return NotImplemented(), nil
}

func setsEqual(a, b *Set) (bool, error) {
	if a.used != b.used {
		return false, nil
	}
	for _, e := range a.entries {
		if !e.used {
			continue
		}
		ok, err := b.Contains(e.key)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// setIterator iterates set members.
//
// CPython: Objects/setobject.c:L958 setiter_iternext
type setIterator struct {
	Header
	entries []setEntry
	pos     int
}

var setIterType = NewType("set_iterator", []*Type{objectType})

func init() {
	setIterType.Iter = func(o Object) (Object, error) { return o, nil }
	setIterType.IterNext = func(o Object) (Object, error) {
		it := o.(*setIterator)
		for it.pos < len(it.entries) {
			e := it.entries[it.pos]
			it.pos++
			if e.used {
				return e.key, nil
			}
		}
		return nil, ErrStopIteration
	}
	AddIterSlotWrappers(setIterType)
}

func setIter(o Object) (Object, error) {
	s := o.(*Set)
	snap := make([]setEntry, len(s.entries))
	copy(snap, s.entries)
	it := &setIterator{entries: snap}
	it.init(setIterType)
	return it, nil
}

// setIntersect returns a new set containing elements common to a and b.
//
// CPython: Objects/setobject.c:1315 set_intersection
func setIntersect(a, b *Set) (*Set, error) {
	out := NewSet()
	for _, e := range a.entries {
		if !e.used {
			continue
		}
		ok, err := b.Contains(e.key)
		if err != nil {
			return nil, err
		}
		if ok {
			out.insert(e.hash, e.key)
		}
	}
	return out, nil
}

// setUnion returns a new set containing all elements from a and b.
//
// CPython: Objects/setobject.c:1505 set_union
func setUnion(a, b *Set) *Set {
	out := NewSet()
	for _, e := range a.entries {
		if e.used {
			out.insert(e.hash, e.key)
		}
	}
	for _, e := range b.entries {
		if e.used {
			out.insert(e.hash, e.key)
		}
	}
	return out
}

// setDiff returns a new set with elements in a but not b.
//
// CPython: Objects/setobject.c:1416 set_difference
func setDiff(a, b *Set) (*Set, error) {
	out := NewSet()
	for _, e := range a.entries {
		if !e.used {
			continue
		}
		ok, err := b.Contains(e.key)
		if err != nil {
			return nil, err
		}
		if !ok {
			out.insert(e.hash, e.key)
		}
	}
	return out, nil
}

// setSymDiff returns a new set with elements in exactly one of a or b.
//
// CPython: Objects/setobject.c:1459 set_symmetric_difference
func setSymDiff(a, b *Set) (*Set, error) {
	out := NewSet()
	for _, e := range a.entries {
		if !e.used {
			continue
		}
		ok, err := b.Contains(e.key)
		if err != nil {
			return nil, err
		}
		if !ok {
			out.insert(e.hash, e.key)
		}
	}
	for _, e := range b.entries {
		if !e.used {
			continue
		}
		ok, err := a.Contains(e.key)
		if err != nil {
			return nil, err
		}
		if !ok {
			out.insert(e.hash, e.key)
		}
	}
	return out, nil
}

func toSet(o Object) (*Set, bool) {
	s, ok := o.(*Set)
	return s, ok
}

// Set binary operations (Number slots).
//
// CPython: Objects/setobject.c set_and, set_or, set_sub, set_xor
func setAnd(a, b Object) (Object, error) {
	as, ok := toSet(a)
	if !ok {
		return NotImplemented(), nil
	}
	bs, ok := toSet(b)
	if !ok {
		return NotImplemented(), nil
	}
	return setIntersect(as, bs)
}

func setOr(a, b Object) (Object, error) {
	as, ok := toSet(a)
	if !ok {
		return NotImplemented(), nil
	}
	bs, ok := toSet(b)
	if !ok {
		return NotImplemented(), nil
	}
	return setUnion(as, bs), nil
}

func setSubtract(a, b Object) (Object, error) {
	as, ok := toSet(a)
	if !ok {
		return NotImplemented(), nil
	}
	bs, ok := toSet(b)
	if !ok {
		return NotImplemented(), nil
	}
	return setDiff(as, bs)
}

func setXor(a, b Object) (Object, error) {
	as, ok := toSet(a)
	if !ok {
		return NotImplemented(), nil
	}
	bs, ok := toSet(b)
	if !ok {
		return NotImplemented(), nil
	}
	return setSymDiff(as, bs)
}

func setIAnd(a, b Object) (Object, error) {
	as, ok := toSet(a)
	if !ok {
		return NotImplemented(), nil
	}
	bs, ok := toSet(b)
	if !ok {
		return NotImplemented(), nil
	}
	result, err := setIntersect(as, bs)
	if err != nil {
		return nil, err
	}
	as.entries = result.entries
	as.used = result.used
	return as, nil
}

func setIOr(a, b Object) (Object, error) {
	as, ok := toSet(a)
	if !ok {
		return NotImplemented(), nil
	}
	bs, ok := toSet(b)
	if !ok {
		return NotImplemented(), nil
	}
	for _, e := range bs.entries {
		if e.used {
			as.insert(e.hash, e.key)
		}
	}
	return as, nil
}

func setIXor(a, b Object) (Object, error) {
	as, ok := toSet(a)
	if !ok {
		return NotImplemented(), nil
	}
	bs, ok := toSet(b)
	if !ok {
		return NotImplemented(), nil
	}
	result, err := setSymDiff(as, bs)
	if err != nil {
		return nil, err
	}
	as.entries = result.entries
	as.used = result.used
	return as, nil
}

// Set method descriptors.

func setLenMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __len__() takes no arguments")
	}
	return NewInt(int64(args[0].(*Set).used)), nil
}

func setAddMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: add() takes exactly one argument")
	}
	s := args[0].(*Set)
	h, err := Hash(args[1])
	if err != nil {
		return nil, err
	}
	s.insert(h, args[1])
	return None(), nil
}

func setDiscardMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: discard() takes exactly one argument")
	}
	s := args[0].(*Set)
	h, err := Hash(args[1])
	if err != nil {
		return nil, err
	}
	idx, ok, err := s.lookup(h, args[1])
	if err != nil {
		return nil, err
	}
	if ok {
		s.entries[idx] = setEntry{used: false}
		s.used--
	}
	return None(), nil
}

func setRemoveMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: remove() takes exactly one argument")
	}
	s := args[0].(*Set)
	h, err := Hash(args[1])
	if err != nil {
		return nil, err
	}
	idx, ok, err := s.lookup(h, args[1])
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("KeyError: %v", args[1])
	}
	s.entries[idx] = setEntry{used: false}
	s.used--
	return None(), nil
}

func setPopMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: pop() takes no arguments")
	}
	s := args[0].(*Set)
	for i, e := range s.entries {
		if e.used {
			s.entries[i] = setEntry{used: false}
			s.used--
			return e.key, nil
		}
	}
	return nil, fmt.Errorf("KeyError: 'pop from an empty set'")
}

func setClearMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: clear() takes no arguments")
	}
	s := args[0].(*Set)
	s.entries = make([]setEntry, setMinSize)
	s.used = 0
	return None(), nil
}

func setUpdateMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: update() requires at least one argument")
	}
	s := args[0].(*Set)
	for _, src := range args[1:] {
		if err := setUpdateFrom(s, src); err != nil {
			return nil, err
		}
	}
	return None(), nil
}

func setUpdateFrom(dst *Set, src Object) error {
	if ss, ok := src.(*Set); ok {
		for _, e := range ss.entries {
			if e.used {
				dst.insert(e.hash, e.key)
			}
		}
		return nil
	}
	it, err := src.Type().Iter(src)
	if err != nil {
		return err
	}
	for {
		v, err := it.Type().IterNext(it)
		if err != nil {
			if isStopIteration(err) {
				return nil
			}
			return err
		}
		h, err := Hash(v)
		if err != nil {
			return err
		}
		dst.insert(h, v)
	}
}

func isStopIteration(err error) bool {
	return errors.Is(err, ErrStopIteration)
}

func setIntersectionMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: intersection() requires at least one argument")
	}
	s := args[0].(*Set)
	result := s
	for _, other := range args[1:] {
		os, ok := toSet(other)
		if !ok {
			return nil, fmt.Errorf("TypeError: intersection() argument must be a set")
		}
		var err error
		result, err = setIntersect(result, os)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func setUnionMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: union() requires the set")
	}
	s := args[0].(*Set)
	result := s
	for _, other := range args[1:] {
		os, ok := toSet(other)
		if !ok {
			return nil, fmt.Errorf("TypeError: union() argument must be a set")
		}
		result = setUnion(result, os)
	}
	return result, nil
}

func setDifferenceMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: difference() requires at least one argument")
	}
	s := args[0].(*Set)
	result := s
	for _, other := range args[1:] {
		os, ok := toSet(other)
		if !ok {
			return nil, fmt.Errorf("TypeError: difference() argument must be a set")
		}
		var err error
		result, err = setDiff(result, os)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func setIsSubsetMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: issubset() takes exactly one argument")
	}
	a, b := args[0].(*Set), args[1].(*Set)
	for _, e := range a.entries {
		if !e.used {
			continue
		}
		ok, err := b.Contains(e.key)
		if err != nil {
			return nil, err
		}
		if !ok {
			return False(), nil
		}
	}
	return True(), nil
}

func setIsSupersetMethod(args []Object, _ map[string]Object) (Object, error) {
	return setIsSubsetMethod([]Object{args[1], args[0]}, nil)
}

func setIsDisjointMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: isdisjoint() takes exactly one argument")
	}
	a, b := args[0].(*Set), args[1].(*Set)
	for _, e := range a.entries {
		if !e.used {
			continue
		}
		ok, err := b.Contains(e.key)
		if err != nil {
			return nil, err
		}
		if ok {
			return False(), nil
		}
	}
	return True(), nil
}

func setIntersectionUpdateMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: intersection_update() requires at least one argument")
	}
	s := args[0].(*Set)
	for _, other := range args[1:] {
		os, ok := toSet(other)
		if !ok {
			return nil, fmt.Errorf("TypeError: intersection_update() argument must be a set")
		}
		result, err := setIntersect(s, os)
		if err != nil {
			return nil, err
		}
		s.entries = result.entries
		s.used = result.used
	}
	return None(), nil
}

func setDifferenceUpdateMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("TypeError: difference_update() requires at least one argument")
	}
	s := args[0].(*Set)
	for _, other := range args[1:] {
		os, ok := toSet(other)
		if !ok {
			return nil, fmt.Errorf("TypeError: difference_update() argument must be a set")
		}
		result, err := setDiff(s, os)
		if err != nil {
			return nil, err
		}
		s.entries = result.entries
		s.used = result.used
	}
	return None(), nil
}

func setSymmetricDifferenceMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: symmetric_difference() takes exactly one argument")
	}
	a, b := args[0].(*Set), args[1].(*Set)
	return setSymDiff(a, b)
}

func setSymmetricDifferenceUpdateMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: symmetric_difference_update() takes exactly one argument")
	}
	s, other := args[0].(*Set), args[1].(*Set)
	result, err := setSymDiff(s, other)
	if err != nil {
		return nil, err
	}
	s.entries = result.entries
	s.used = result.used
	return None(), nil
}

func setCopyMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: copy() takes no arguments")
	}
	s := args[0].(*Set)
	out := NewSet()
	for _, e := range s.entries {
		if e.used {
			out.insert(e.hash, e.key)
		}
	}
	return out, nil
}
