// Set and frozenset objects. Mirrors CPython's open-addressed hash
// table layout. frozenset is an immutable set that supports all read
// operations but disallows mutation; it stores its hash lazily.
//
// CPython: Objects/setobject.c
package objects

import (
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
	SetType.Iter = setIter
	SetType.Sequence = &SequenceMethods{
		Length:   setLen,
		Contains: setContains,
	}
	SetType.TpTraverse = setTraverse
	SetType.Getattro = GenericGetAttr
	SetTypeDescr(SetType, "__contains__", NewMethodDescr(SetType, "__contains__", setContainsMethod))

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
	SetTypeDescr(FrozensetType, "__contains__", NewMethodDescr(FrozensetType, "__contains__", setContainsMethod))
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
	if s.used*3 >= len(s.entries)*2 {
		s.grow()
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

func (s *Set) insert(h int64, key Object) {
	idx, ok, _ := s.lookup(h, key)
	s.entries[idx] = setEntry{hash: h, key: key, used: true}
	if !ok {
		s.used++
	}
}

func (s *Set) grow() {
	old := s.entries
	s.entries = make([]setEntry, len(old)*2)
	s.used = 0
	for _, e := range old {
		if e.used {
			s.insert(e.hash, e.key)
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
}

func setIter(o Object) (Object, error) {
	s := o.(*Set)
	snap := make([]setEntry, len(s.entries))
	copy(snap, s.entries)
	it := &setIterator{entries: snap}
	it.init(setIterType)
	return it, nil
}
