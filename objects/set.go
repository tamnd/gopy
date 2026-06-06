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
	"sync/atomic"
)

// setEntry is one slot in the set's open-addressed table.
// dummy marks a slot that was deleted: the probe chain must
// continue past it (tombstone) so that previously inserted entries
// further in the chain are still reachable.
//
// CPython: Objects/setobject.c:L78 setentry (DUMMY sentinel)
type setEntry struct {
	hash  int64
	key   Object
	used  bool
	dummy bool // tombstone: deleted but probe chain continues
}

// SetEntry is the public view of a live set slot. Used by dict.fromkeys
// to access cached hash values without rehashing.
type SetEntry struct {
	Hash int64
	Key  Object
}

// Set is the mutable Python set type.
//
// CPython: Objects/setobject.c:L544 PySetObject
type Set struct {
	Header
	entries    []setEntry
	used       int // live entries only
	fill       int // live + tombstone entries (CPython: so->fill)
	version    uint64
	frozen     bool
	cachedHash int64 // only valid for frozenset when hashValid is true
	hashValid  bool
	attrs      *Dict // per-instance dict for set/frozenset subclasses
}

// AttrDict returns the per-instance attribute dict or nil.
func (s *Set) AttrDict() *Dict { return s.attrs }

// EnsureAttrDict allocates the per-instance attribute dict on first use.
func (s *Set) EnsureAttrDict() *Dict {
	if s.attrs == nil {
		s.attrs = NewDict()
	}
	return s.attrs
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
	// CPython: Objects/setobject.c:2497 set.__hash__ = None signals
	// explicitly unhashable so subclasses inherit that property.
	SetTypeDescr(SetType, "__hash__", None())
	SetType.RichCmp = setRichCmp
	SetType.TpFlags |= TpFlagMatchSelf
	FrozensetType.TpFlags |= TpFlagMatchSelf
	SetType.Iter = setIter
	SetType.Sequence = &SequenceMethods{
		Length:   setLen,
		Contains: setContains,
	}
	SetType.TpTraverse = setTraverse
	SetType.Dealloc = setDealloc
	SetType.Getattro = GenericGetAttr
	SetType.Setattro = GenericSetAttr
	// set instances carry a weakref list but no __dict__: PySet_Type sets
	// tp_weaklistoffset to offsetof(PySetObject, weakreflist) and leaves
	// tp_dictoffset at 0. A plain set subclass still gains a managed dict
	// through configureManagedDict's no-__slots__ path, so HasWeakref is
	// what set itself contributes, and it leaves may_add_dict open so a
	// subclass can name __dict__ in __slots__.
	//
	// CPython: Objects/setobject.c:2611 PySet_Type (tp_weaklistoffset set,
	// tp_dictoffset 0)
	SetType.HasWeakref = true
	SetType.Number = &NumberMethods{
		And:        setAnd,
		Or:         setOr,
		Subtract:   setSubtract,
		Xor:        setXor,
		InPlaceAnd: setIAnd,
		InPlaceOr:  setIOr,
		InPlaceXor: setIXor,
	}
	SetTypeDescr(SetType, "__init__", NewMethodDescr(SetType, "__init__", setInitMethod))
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
	// __repr__ and __str__ descriptors so set subclasses inherit them via MRO
	// and fixupCallReprStr installs slotTpRepr (not generic object repr).
	//
	// CPython: Objects/typeobject.c add_operators (tp_repr row)
	SetTypeDescr(SetType, "__repr__", NewMethodDescr(SetType, "__repr__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, errors.New("TypeError: __repr__ expected 1 argument")
		}
		s, err := setRepr(args[0])
		if err != nil {
			return nil, err
		}
		return NewStr(s), nil
	}))
	SetTypeDescr(SetType, "__str__", NewMethodDescr(SetType, "__str__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, errors.New("TypeError: __str__ expected 1 argument")
		}
		s, err := setRepr(args[0])
		if err != nil {
			return nil, err
		}
		return NewStr(s), nil
	}))

	FrozensetType.Repr = frozensetRepr
	FrozensetType.Str = frozensetRepr
	FrozensetType.Hash = frozensetHash
	// Install __hash__ descriptor so frozenset subclasses inherit it through
	// fixupHashAndIter's LookupDescriptor(t, "__hash__") scan, getting
	// slotTpHash which delegates to this descriptor.
	//
	// CPython: Objects/typeobject.c add_operators (tp_hash row)
	SetTypeDescr(FrozensetType, "__hash__", NewMethodDescr(FrozensetType, "__hash__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, errors.New("TypeError: __hash__ expected 1 argument")
		}
		h, err := frozensetHash(args[0])
		if err != nil {
			return nil, err
		}
		return NewInt(h), nil
	}))
	FrozensetType.RichCmp = setRichCmp
	FrozensetType.Iter = setIter
	FrozensetType.Sequence = &SequenceMethods{
		Length:   setLen,
		Contains: setContains,
	}
	FrozensetType.TpTraverse = setTraverse
	FrozensetType.Dealloc = setDealloc
	FrozensetType.Getattro = GenericGetAttr
	FrozensetType.Setattro = GenericSetAttr
	// frozenset matches set: a weakref list, no __dict__. PyFrozenSet_Type
	// sets tp_weaklistoffset and leaves tp_dictoffset at 0.
	//
	// CPython: Objects/setobject.c:2703 PyFrozenSet_Type (tp_weaklistoffset
	// set, tp_dictoffset 0)
	FrozensetType.HasWeakref = true
	FrozensetType.Number = &NumberMethods{
		And:      setAnd,
		Or:       setOr,
		Subtract: setSubtract,
		Xor:      setXor,
	}
	// CPython: Objects/setobject.c:2697 PyFrozenSet_Type.tp_init = 0
	// frozenset has no tp_init; type_call never invokes it. We omit the
	// descriptor so subclasses that define their own __new__ (but not
	// __init__) don't have their kwargs rejected by an inherited
	// frozenset __init__. Keyword-arg rejection for exact frozenset()
	// calls is handled in frozensetCtorWithType (TpNew).
	SetTypeDescr(FrozensetType, "__contains__", NewMethodDescr(FrozensetType, "__contains__", setContainsMethod))
	SetTypeDescr(FrozensetType, "__len__", NewMethodDescr(FrozensetType, "__len__", setLenMethod))
	// __repr__ and __str__ descriptors so frozenset subclasses inherit them via MRO.
	//
	// CPython: Objects/typeobject.c add_operators (tp_repr row)
	SetTypeDescr(FrozensetType, "__repr__", NewMethodDescr(FrozensetType, "__repr__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, errors.New("TypeError: __repr__ expected 1 argument")
		}
		s, err := frozensetRepr(args[0])
		if err != nil {
			return nil, err
		}
		return NewStr(s), nil
	}))
	SetTypeDescr(FrozensetType, "__str__", NewMethodDescr(FrozensetType, "__str__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, errors.New("TypeError: __str__ expected 1 argument")
		}
		s, err := frozensetRepr(args[0])
		if err != nil {
			return nil, err
		}
		return NewStr(s), nil
	}))
	SetTypeDescr(FrozensetType, "intersection", NewMethodDescr(FrozensetType, "intersection", setIntersectionMethod))
	SetTypeDescr(FrozensetType, "union", NewMethodDescr(FrozensetType, "union", setUnionMethod))
	SetTypeDescr(FrozensetType, "issubset", NewMethodDescr(FrozensetType, "issubset", setIsSubsetMethod))
	SetTypeDescr(FrozensetType, "issuperset", NewMethodDescr(FrozensetType, "issuperset", setIsSupersetMethod))
	SetTypeDescr(FrozensetType, "difference", NewMethodDescr(FrozensetType, "difference", setDifferenceMethod))
	SetTypeDescr(FrozensetType, "symmetric_difference", NewMethodDescr(FrozensetType, "symmetric_difference", setSymmetricDifferenceMethod))
	SetTypeDescr(FrozensetType, "isdisjoint", NewMethodDescr(FrozensetType, "isdisjoint", setIsDisjointMethod))
	SetTypeDescr(FrozensetType, "copy", NewMethodDescr(FrozensetType, "copy", setCopyMethod))
	// __reduce__ returns (type, ([list_of_elements],), state) so pickle
	// and copy.deepcopy can reconstruct the set.
	//
	// CPython: Objects/setobject.c:2397 set___reduce___impl
	SetTypeDescr(SetType, "__reduce__", NewMethodDescr(SetType, "__reduce__", setReduceMethod))
	SetTypeDescr(FrozensetType, "__reduce__", NewMethodDescr(FrozensetType, "__reduce__", setReduceMethod))
	// CPython: Objects/typeobject.c add_operators slotdefs tp_iter row
	AddIterSlotWrappers(SetType)
	AddIterSlotWrappers(FrozensetType)
}

// setDealloc fires when the set's Python refcount reaches zero. For
// subclasses that define __del__ (tp_finalize), the finalizer runs
// first. The refcount is temporarily bumped to 1 ("resurrection guard")
// so that when the __del__ frame's local-close Decrefs self, the count
// lands on 0 rather than -1, and Dealloc does not re-enter. After the
// finalizer returns the refcount is decremented back; if it is now > 0,
// __del__ resurrected the object and we return early.
//
// CPython: Objects/object.c:489 PyObject_CallFinalizerFromDealloc
// CPython: Objects/setobject.c:L539 set_dealloc
func setDealloc(o Object) {
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
	return visitAttrDict(o, visit)
}

// NewSet creates an empty mutable set.
//
// CPython: Objects/setobject.c:L2267 PySet_New (empty case)
func NewSet() *Set {
	s := &Set{entries: make([]setEntry, setMinSize)}
	s.init(SetType)
	if h := GCTrackHook; h != nil {
		h(s)
	}
	return s
}

// NewSetOfType allocates a new set whose ob_type is tp. Used by the
// subtype-aware TpNew so that class H(set): ... instances carry type H.
//
// CPython: Objects/setobject.c:2267 set_new (subtype allocation)
func NewSetOfType(tp *Type) *Set {
	s := &Set{entries: make([]setEntry, setMinSize)}
	s.init(tp)
	if h := GCTrackHook; h != nil {
		h(s)
	}
	return s
}

// newEmptyLike creates an empty set or frozenset matching the frozen flag of
// src. Subclass instances always produce the canonical base type (set or
// frozenset), matching CPython's make_new_set(Py_TYPE(so)) with the base
// concrete type for set operations.
//
// CPython: Objects/setobject.c:2267 make_new_set (type is the receiver type,
// which for subclasses is the direct C type not the Python subclass)
func newEmptyLike(src *Set) *Set {
	if src.frozen {
		s := &Set{entries: make([]setEntry, setMinSize), frozen: true}
		s.init(FrozensetType)
		if h := GCTrackHook; h != nil {
			h(s)
		}
		return s
	}
	s := &Set{entries: make([]setEntry, setMinSize)}
	s.init(SetType)
	if h := GCTrackHook; h != nil {
		h(s)
	}
	return s
}

// newSetFromIterable drains an arbitrary iterable into a fresh mutable set.
// Used by issubset / issuperset when the argument is not already a set.
//
// CPython: Objects/setobject.c:2267 PySet_New (iterable case)
func newSetFromIterable(o Object) (*Set, error) {
	s := NewSet()
	if err := setUpdateFrom(s, o); err != nil {
		return nil, err
	}
	return s, nil
}

// NewFrozenset creates a frozenset from the given items.
//
// CPython: Objects/setobject.c:L2300 PyFrozenSet_New
func NewFrozenset(items []Object) (*Set, error) {
	return NewFrozensetOfType(FrozensetType, items)
}

// NewFrozensetOfType creates a frozenset of the given type. Used by the
// subtype-aware TpNew so frozenset subclass instances carry the subclass type.
//
// CPython: Objects/setobject.c:2361 frozenset_new (subtype)
func NewFrozensetOfType(tp *Type, items []Object) (*Set, error) {
	s := &Set{entries: make([]setEntry, setMinSize), frozen: true}
	s.init(tp)
	for _, item := range items {
		if err := s.add(item); err != nil {
			return nil, err
		}
	}
	if h := GCTrackHook; h != nil {
		h(s)
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
	h, err := hashKey(key)
	if err != nil {
		return err
	}
	return s.insert(h, key)
}

// hashKey computes the hash of key for use in set operations. Mutable
// set keys fall back to the frozenset hash algorithm. Other unhashable
// types produce the CPython "cannot use 'X' as a set element" message.
//
// CPython: Objects/setobject.c:228 set_unhashable_type + 2233 set_contains_lock_held
func hashKey(key Object) (int64, error) {
	h, err := Hash(key)
	if err == nil {
		return h, nil
	}
	// Mutable sets: fall back to frozenset hash (allows set(x) in {frozenset(x)}).
	if ks, ok := key.(*Set); ok {
		return frozensetHash(ks)
	}
	// Wrap TypeError into CPython's "cannot use ... as a set element" format.
	msg := err.Error()
	const typeErrPfx = "TypeError: "
	if len(msg) > len(typeErrPfx) && msg[:len(typeErrPfx)] == typeErrPfx {
		inner := msg[len(typeErrPfx):]
		return 0, fmt.Errorf("TypeError: cannot use '%s' as a set element (%s)",
			key.Type().Name, inner)
	}
	return 0, err
}

// Discard removes key if present. Does nothing on miss. Errors if frozen.
//
// CPython: Objects/setobject.c:L743 set_discard_key
func (s *Set) Discard(key Object) error {
	if s.frozen {
		return fmt.Errorf("AttributeError: 'frozenset' object has no attribute 'discard'")
	}
	h, err := hashKey(key)
	if err != nil {
		return err
	}
	idx, ok, err := s.lookup(h, key)
	if err != nil || !ok {
		return err
	}
	// Mark as tombstone so probe chains threading through this slot
	// remain intact. grow() rebuilds the table without tombstones.
	// CPython: Objects/setobject.c:L743 set_discard_key DUMMY marker
	s.entries[idx] = setEntry{dummy: true}
	s.used--
	s.version++
	return nil
}

// Contains reports whether key is in the set.
//
// CPython: Objects/setobject.c:L1777 set_contains_key
func (s *Set) Contains(key Object) (bool, error) {
	h, err := hashKey(key)
	if err != nil {
		return false, err
	}
	_, ok, err := s.lookup(h, key)
	return ok, err
}

// containsWithHash is like Contains but uses a pre-computed hash to avoid
// rehashing. Used by set_difference and similar operations that already
// hold the entry hash from iterating another set.
//
// CPython: Objects/setobject.c:L349 set_contains_entry
func (s *Set) containsWithHash(h int64, key Object) (bool, error) {
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

// Entries returns a snapshot of live (key, hash) pairs. Used by
// dict.fromkeys to reuse cached hashes without rehashing.
func (s *Set) Entries() []SetEntry {
	out := make([]SetEntry, 0, s.used)
	for _, e := range s.entries {
		if e.used {
			out = append(out, SetEntry{Hash: e.hash, Key: e.key})
		}
	}
	return out
}

func (s *Set) lookup(h int64, key Object) (idx int, found bool, err error) {
	startVersion := s.version
	mask := uint64(len(s.entries) - 1)
	i := uint64(h) & mask
	perturb := uint64(h)
	firstDummy := -1 // index of first tombstone slot in probe chain
	for {
		e := &s.entries[i]
		if !e.used {
			if e.dummy {
				// Tombstone: probe chain continues, but remember this
				// slot as a candidate for insertion.
				// CPython: Objects/setobject.c:L140 DUMMY sentinel
				if firstDummy < 0 {
					firstDummy = int(i)
				}
			} else {
				// Genuinely empty: chain ends here.
				if firstDummy >= 0 {
					return firstDummy, false, nil
				}
				return int(i), false, nil
			}
		} else if e.hash == h {
			eq, err := RichCmpBool(e.key, key, CompareEQ)
			if err != nil {
				return 0, false, err
			}
			// If __eq__ mutated the set (resize or removal), restart
			// the probe from scratch, mirroring CPython's recursive
			// set_lookkey call.
			//
			// CPython: Objects/setobject.c:L115 table != so->table guard
			if s.version != startVersion {
				return s.lookup(h, key)
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
// fill ratio crosses 3/5. fill = used + tombstones; mirrors CPython's
// set_add_entry which resizes on fill*5 > (mask+1)*3.
//
// CPython: Objects/setobject.c:220 set_add_entry
func (s *Set) insert(h int64, key Object) error {
	// Resize when fill (used+tombstones) exceeds 60% of capacity.
	// CPython: Objects/setobject.c:391 (so->fill+1)*5 > (so->mask+1)*3
	if (s.fill+1)*5 >= len(s.entries)*3 {
		s.grow()
	}
	idx, ok, err := s.lookup(h, key)
	if err != nil {
		return err
	}
	if ok {
		// Key already present; CPython keeps the first-inserted key, not the new one.
		// CPython: Objects/setobject.c:246 set_add_entry (existing key → return 0)
		return nil
	}
	wasTombstone := s.entries[idx].dummy
	s.entries[idx] = setEntry{hash: h, key: key, used: true}
	s.used++
	s.version++
	// Only increment fill when replacing a genuinely empty slot.
	// Tombstone → live does not change fill (tombstone was already counted).
	if !wasTombstone {
		s.fill++
	}
	return nil
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
	s.fill++
}

func (s *Set) grow() {
	old := s.entries
	// CPython: Objects/setobject.c:266 set_table_resize
	// New table is 4x used (so post-rehash fill == used, no tombstones).
	newSize := setMinSize
	for newSize < s.used*4 {
		newSize <<= 1
	}
	s.version++
	s.entries = make([]setEntry, newSize)
	s.used = 0
	s.fill = 0
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

// setRepr mirrors CPython's set_repr_lock_held. Exact set uses {elems};
// all other types (set subclasses, frozenset, frozenset subclasses) use
// TypeName({elems}).
//
// CPython: Objects/setobject.c:566 set_repr_lock_held
func setRepr(o Object) (string, error) {
	s := o.(*Set)
	return setReprLockHeld(s)
}

func frozensetRepr(o Object) (string, error) {
	s := o.(*Set)
	return setReprLockHeld(s)
}

func setReprLockHeld(s *Set) (string, error) {
	name := s.Type().Name
	// Cycle guard: return "TypeName(...)"
	// CPython: Objects/setobject.c:574
	if ReprEnter(s) {
		return name + "(...)", nil
	}
	defer ReprLeave(s)
	// Empty set: "TypeName()"
	// CPython: Objects/setobject.c:580
	if s.used == 0 {
		if s.Type() == SetType {
			return "set()", nil
		}
		return name + "()", nil
	}
	// Non-empty: exact set uses "{elems}", others use "TypeName({elems})"
	// CPython: Objects/setobject.c:606
	exactSet := s.Type() == SetType
	var b strings.Builder
	if !exactSet {
		b.WriteString(name)
		b.WriteString("({")
	} else {
		b.WriteString("{")
	}
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
	if !exactSet {
		b.WriteString("})")
	} else {
		b.WriteString("}")
	}
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

// setRichCmp implements all comparison ops for both set and frozenset.
// EQ/NE use mutual containment. Ordering ops are subset/superset checks:
// a <= b means a is a subset of b; a < b is a proper subset.
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
	case CompareLE:
		sub, err := setIsSubset(as, bs)
		if err != nil {
			return nil, err
		}
		return NewBool(sub), nil
	case CompareGE:
		sub, err := setIsSubset(bs, as)
		if err != nil {
			return nil, err
		}
		return NewBool(sub), nil
	case CompareLT:
		if as.used >= bs.used {
			return False(), nil
		}
		sub, err := setIsSubset(as, bs)
		if err != nil {
			return nil, err
		}
		return NewBool(sub), nil
	case CompareGT:
		if as.used <= bs.used {
			return False(), nil
		}
		sub, err := setIsSubset(bs, as)
		if err != nil {
			return nil, err
		}
		return NewBool(sub), nil
	}
	return NotImplemented(), nil
}

// setIsSubset reports whether every element of a is contained in b.
//
// CPython: Objects/setobject.c:L1641 set_issubset_impl
func setIsSubset(a, b *Set) (bool, error) {
	if a.used > b.used {
		return false, nil
	}
	for _, e := range a.entries {
		if !e.used {
			continue
		}
		ok, err := b.containsWithHash(e.hash, e.key)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

func setsEqual(a, b *Set) (bool, error) {
	if a.used != b.used {
		return false, nil
	}
	for _, e := range a.entries {
		if !e.used {
			continue
		}
		ok, err := b.containsWithHash(e.hash, e.key)
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
	src       *Set // live set, needed for size-change detection
	usedAt    int  // used count at iterator creation; -1 means exhausted/errored
	remaining int  // number of items yet to yield; mirrors si_len
	entries   []setEntry
	pos       int
}

var setIterType = NewType("set_iterator", []*Type{objectType})

func setIterDealloc(o Object) {
	it := o.(*setIterator)
	if it.src != nil {
		Decref(it.src)
		it.src = nil
	}
	if h := GCUntrackHook; h != nil {
		h(o)
	}
}

func init() {
	setIterType.Dealloc = setIterDealloc
	setIterType.Iter = SelfIter
	setIterType.IterNext = func(o Object) (Object, error) {
		it := o.(*setIterator)
		if it.usedAt < 0 {
			return nil, ErrStopIteration
		}
		// Detect concurrent modification: if the live set's used count
		// changed since the iterator was created, raise RuntimeError.
		//
		// CPython: Objects/setobject.c:958 setiter_iternext si_used check
		if it.src != nil && it.src.used != it.usedAt {
			it.usedAt = -1
			Decref(it.src)
			it.src = nil
			return nil, fmt.Errorf("RuntimeError: Set changed size during iteration")
		}
		for it.pos < len(it.entries) {
			e := it.entries[it.pos]
			it.pos++
			if e.used {
				it.remaining--
				return e.key, nil
			}
		}
		it.usedAt = -1
		it.remaining = 0
		// Release the source set reference once iteration is exhausted.
		// CPython: Objects/setobject.c:1007 setiter_iternext Py_CLEAR(si->si_set)
		if it.src != nil {
			Decref(it.src)
			it.src = nil
		}
		return nil, ErrStopIteration
	}
	// TpTraverse visits the source set and all snapshot entry keys so the
	// cyclic GC can collect cycles through a set iterator.
	//
	// CPython: Objects/setobject.c setiter_traverse
	setIterType.TpTraverse = func(o Object, visit Visitor) error {
		it := o.(*setIterator)
		if it.src != nil {
			if err := visit(it.src); err != nil {
				return err
			}
		}
		for _, e := range it.entries {
			if e.used && e.key != nil {
				if err := visit(e.key); err != nil {
					return err
				}
			}
		}
		return nil
	}
	AddIterSlotWrappers(setIterType)
	// __reduce__ returns (iter, ([remaining_items],)) so pickle can round-trip
	// the iterator as a list iterator (undefined order, so list is used).
	//
	// CPython: Objects/setobject.c:876 setiter_reduce
	SetTypeDescr(setIterType, "__reduce__", NewMethodDescr(setIterType, "__reduce__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments")
			}
			it := args[0].(*setIterator)
			// Collect remaining items by iterating a clone of the state.
			var items []Object
			if it.usedAt >= 0 {
				for i := it.pos; i < len(it.entries); i++ {
					if it.entries[i].used {
						items = append(items, it.entries[i].key)
					}
				}
			}
			lst := NewList(items)
			if BuiltinLookup == nil {
				return nil, fmt.Errorf("PicklingError: builtins not loaded")
			}
			iterFn, err := BuiltinLookup("iter")
			if err != nil {
				return nil, err
			}
			return NewTuple([]Object{iterFn, NewTuple([]Object{lst})}), nil
		},
	))
	// __length_hint__ returns the number of items remaining.
	// CPython: Objects/setobject.c:L966 setiter_len
	SetTypeDescr(setIterType, "__length_hint__", NewMethodDescr(setIterType, "__length_hint__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __length_hint__ takes no arguments")
			}
			it := args[0].(*setIterator)
			if it.usedAt < 0 {
				return NewInt(0), nil
			}
			if it.src != nil && it.src.used != it.usedAt {
				return NewInt(0), nil
			}
			return NewInt(int64(it.remaining)), nil
		},
	))
}

func setIter(o Object) (Object, error) {
	s := o.(*Set)
	snap := make([]setEntry, len(s.entries))
	copy(snap, s.entries)
	// Incref the source set so it stays alive for the iterator's lifetime.
	// CPython: Objects/setobject.c:883 set_iter (Py_INCREF(so))
	Incref(s)
	it := &setIterator{src: s, usedAt: s.used, remaining: s.used, entries: snap}
	it.init(setIterType)
	if h := GCTrackHook; h != nil {
		h(it)
	}
	return it, nil
}

// setIntersect returns a new set containing elements common to a and b.
// The output type mirrors proto (frozenset if proto is frozen).
//
// CPython: Objects/setobject.c:1315 set_intersection
func setIntersect(proto, a, b *Set) (*Set, error) {
	out := newEmptyLike(proto)
	for _, e := range a.entries {
		if !e.used {
			continue
		}
		ok, err := b.containsWithHash(e.hash, e.key)
		if err != nil {
			return nil, err
		}
		if ok {
			if err := out.insert(e.hash, e.key); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// setUnion returns a new set containing all elements from a and b.
// The output type mirrors proto (frozenset if proto is frozen).
//
// CPython: Objects/setobject.c:1505 set_union
func setUnion(proto, a, b *Set) (*Set, error) {
	out := newEmptyLike(proto)
	for _, e := range a.entries {
		if e.used {
			if err := out.insert(e.hash, e.key); err != nil {
				return nil, err
			}
		}
	}
	for _, e := range b.entries {
		if e.used {
			if err := out.insert(e.hash, e.key); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// setDiff returns a new set with elements in a but not b.
// The output type mirrors proto (frozenset if proto is frozen).
//
// CPython: Objects/setobject.c:1416 set_difference
func setDiff(proto, a, b *Set) (*Set, error) {
	out := newEmptyLike(proto)
	for _, e := range a.entries {
		if !e.used {
			continue
		}
		ok, err := b.containsWithHash(e.hash, e.key)
		if err != nil {
			return nil, err
		}
		if !ok {
			if err := out.insert(e.hash, e.key); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// setSymDiff returns a new set with elements in exactly one of a or b.
// The output type mirrors proto (frozenset if proto is frozen).
//
// CPython: Objects/setobject.c:1459 set_symmetric_difference
func setSymDiff(proto, a, b *Set) (*Set, error) {
	out := newEmptyLike(proto)
	for _, e := range a.entries {
		if !e.used {
			continue
		}
		ok, err := b.containsWithHash(e.hash, e.key)
		if err != nil {
			return nil, err
		}
		if !ok {
			if err := out.insert(e.hash, e.key); err != nil {
				return nil, err
			}
		}
	}
	for _, e := range b.entries {
		if !e.used {
			continue
		}
		ok, err := a.containsWithHash(e.hash, e.key)
		if err != nil {
			return nil, err
		}
		if !ok {
			if err := out.insert(e.hash, e.key); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func toSet(o Object) (*Set, bool) {
	s, ok := o.(*Set)
	return s, ok
}

// mutableSetCopy returns a fresh mutable set holding s's elements,
// whether or not s is frozen. The dict view set operators use it so
// their result is always a plain `set`, matching PySet_New.
//
// CPython: Objects/dictobject.c:6300 dictviews_or (PySet_New base)
func mutableSetCopy(s *Set) (*Set, error) {
	out := NewSet()
	for _, e := range s.entries {
		if e.used {
			if err := out.insert(e.hash, e.key); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
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
	return setIntersect(as, as, bs)
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
	return setUnion(as, as, bs)
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
	return setDiff(as, as, bs)
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
	return setSymDiff(as, as, bs)
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
	result, err := setIntersect(as, as, bs)
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
			if err := as.insert(e.hash, e.key); err != nil {
				return nil, err
			}
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
	result, err := setSymDiff(as, as, bs)
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
	h, err := hashKey(args[1])
	if err != nil {
		return nil, err
	}
	if err := s.insert(h, args[1]); err != nil {
		return nil, err
	}
	return None(), nil
}

// setInitMethod ports set_init: clear and repopulate from iterable.
// Keyword arguments are rejected. Called by set().__init__(iterable).
//
// CPython: Objects/setobject.c:2439 set_init
func setInitMethod(args []Object, kwargs map[string]Object) (Object, error) {
	if len(kwargs) > 0 {
		return nil, fmt.Errorf("TypeError: set() does not support keyword arguments")
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: descriptor '__init__' of 'set' object needs an argument")
	}
	s := args[0].(*Set)
	if len(args) > 2 {
		return nil, fmt.Errorf("TypeError: set expected at most 1 argument, got %d", len(args)-1)
	}
	if len(args) == 1 {
		return None(), nil
	}
	// Clear and repopulate.
	s.entries = make([]setEntry, setMinSize)
	s.fill = 0
	s.used = 0
	s.version++
	return None(), setUpdateFrom(s, args[1])
}

func setDiscardMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: discard() takes exactly one argument")
	}
	return None(), args[0].(*Set).Discard(args[1])
}

func setRemoveMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: remove() takes exactly one argument")
	}
	s := args[0].(*Set)
	h, err := hashKey(args[1])
	if err != nil {
		return nil, err
	}
	idx, ok, err := s.lookup(h, args[1])
	if err != nil {
		return nil, err
	}
	if !ok {
		if KeyErrorFactory != nil {
			return nil, KeyErrorFactory(args[1])
		}
		r, err2 := Repr(args[1])
		if err2 != nil {
			r = "?"
		}
		return nil, fmt.Errorf("KeyError: %s", r)
	}
	// Tombstone: mark deleted but keep probe chain intact.
	// CPython: Objects/setobject.c:L743 set_discard_key DUMMY marker
	s.entries[idx] = setEntry{dummy: true}
	s.used--
	s.version++
	return None(), nil
}

func setPopMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: pop() takes no arguments")
	}
	s := args[0].(*Set)
	for i, e := range s.entries {
		if e.used {
			s.entries[i] = setEntry{dummy: true}
			s.used--
			s.version++
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
	s.version++
	s.entries = make([]setEntry, setMinSize)
	s.used = 0
	s.fill = 0
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

// SetUpdateFrom is the exported entry point for setUpdateFrom, used by
// the builtins set constructor to avoid rehashing dict keys.
func SetUpdateFrom(dst *Set, src Object) error { return setUpdateFrom(dst, src) }

func setUpdateFrom(dst *Set, src Object) error {
	if ss, ok := src.(*Set); ok {
		for _, e := range ss.entries {
			if e.used {
				if err := dst.insert(e.hash, e.key); err != nil {
					return err
				}
			}
		}
		return nil
	}
	// Dict fast path: reuse cached hash values to avoid rehashing.
	// CPython: Objects/setobject.c:1983 set_update_internal (PyDict_CheckExact)
	if dd, ok := src.(*Dict); ok {
		return dd.ForEachWithHash(func(key Object, hash int64) error {
			return dst.insert(hash, key)
		})
	}
	it, err := Iter(src)
	if err != nil {
		return err
	}
	for {
		v, err := IterNext(it)
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
		if err := dst.insert(h, v); err != nil {
			return err
		}
	}
}

func isStopIteration(err error) bool {
	return errors.Is(err, ErrStopIteration)
}

func setIntersectionMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: intersection() requires the set")
	}
	s := args[0].(*Set)
	if len(args) == 1 {
		return setCopy(s), nil
	}
	result := s
	for _, other := range args[1:] {
		var err error
		if os, ok := toSet(other); ok {
			result, err = setIntersect(s, result, os)
		} else {
			result, err = setIntersectIterable(s, result, other)
		}
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// setIntersectIterable intersects a set with an arbitrary iterable,
// mirroring CPython's set_intersection fast path for non-set iterables.
//
// CPython: Objects/setobject.c:1350 set_intersection
func setIntersectIterable(proto, a *Set, other Object) (*Set, error) {
	items, err := IterToSlice(other)
	if err != nil {
		return nil, err
	}
	out := newEmptyLike(proto)
	for _, item := range items {
		ok, err := a.Contains(item)
		if err != nil {
			return nil, err
		}
		if ok {
			h, err := Hash(item)
			if err != nil {
				return nil, err
			}
			if err := out.insert(h, item); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
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
			// Accept any iterable: convert it to a set first.
			// CPython: Objects/setobject.c:1952 set_union (accepts any iterable)
			os2 := NewSet()
			if err := setUpdateFrom(os2, other); err != nil {
				return nil, err
			}
			os = os2
		}
		var err error
		result, err = setUnion(s, result, os)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func setDifferenceMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: difference() requires the set")
	}
	s := args[0].(*Set)
	// difference() with no args returns a shallow copy.
	// CPython: Objects/setobject.c set_difference (n==0 path)
	if len(args) == 1 {
		return setCopy(s), nil
	}
	result := s
	for _, other := range args[1:] {
		os, ok := toSet(other)
		if !ok {
			var err error
			os, err = newSetFromIterable(other)
			if err != nil {
				return nil, err
			}
		}
		var err error
		result, err = setDiff(s, result, os)
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
	a := args[0].(*Set)
	// CPython: Objects/setobject.c:2087 set_issubset_impl — converts other to
	// a temporary set when it is not already a set/frozenset.
	var b *Set
	switch v := args[1].(type) {
	case *Set:
		b = v
	default:
		tmp, err := newSetFromIterable(args[1])
		if err != nil {
			return nil, err
		}
		b = tmp
	}
	for _, e := range a.entries {
		if !e.used {
			continue
		}
		ok, err := b.containsWithHash(e.hash, e.key)
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
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: issuperset() takes exactly one argument")
	}
	// CPython: Objects/setobject.c:2132 set_issuperset_impl — converts other
	// to a temporary set when it is not already a set/frozenset.
	a := args[0].(*Set)
	var b *Set
	switch v := args[1].(type) {
	case *Set:
		b = v
	default:
		tmp, err := newSetFromIterable(args[1])
		if err != nil {
			return nil, err
		}
		b = tmp
	}
	return setIsSubsetMethod([]Object{b, a}, nil)
}

// setIsDisjointMethod implements set.isdisjoint(other). CPython accepts any
// iterable for other; when other is a set or frozenset the Contains fast
// path is used, otherwise each element from other is hashed and looked up.
//
// CPython: Objects/setobject.c:1424 set_isdisjoint_impl
func setIsDisjointMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: isdisjoint() takes exactly one argument")
	}
	a := args[0].(*Set)
	other := args[1]
	if b, ok := other.(*Set); ok {
		for _, e := range a.entries {
			if !e.used {
				continue
			}
			ok, err := b.containsWithHash(e.hash, e.key)
			if err != nil {
				return nil, err
			}
			if ok {
				return False(), nil
			}
		}
		return True(), nil
	}
	// Generic iterable: iterate other and check membership in a.
	items, err := SequenceList(other)
	if err != nil {
		return nil, fmt.Errorf("TypeError: isdisjoint() argument is not iterable")
	}
	for i := 0; i < items.Len(); i++ {
		item := items.Item(i)
		ok, err := a.Contains(item)
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
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: intersection_update() requires the set")
	}
	s := args[0].(*Set)
	for _, other := range args[1:] {
		var result *Set
		var err error
		if os, ok := toSet(other); ok {
			result, err = setIntersect(s, s, os)
		} else {
			result, err = setIntersectIterable(s, s, other)
		}
		if err != nil {
			return nil, err
		}
		s.entries = result.entries
		s.used = result.used
	}
	return None(), nil
}

func setDifferenceUpdateMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: difference_update() requires the set")
	}
	s := args[0].(*Set)
	for _, other := range args[1:] {
		os, ok := toSet(other)
		if !ok {
			var err error
			os, err = newSetFromIterable(other)
			if err != nil {
				return nil, err
			}
		}
		result, err := setDiff(s, s, os)
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
	a := args[0].(*Set)
	b, ok := toSet(args[1])
	if !ok {
		var err error
		b, err = newSetFromIterable(args[1])
		if err != nil {
			return nil, err
		}
	}
	return setSymDiff(a, a, b)
}

func setSymmetricDifferenceUpdateMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: symmetric_difference_update() takes exactly one argument")
	}
	s := args[0].(*Set)
	other, ok := toSet(args[1])
	if !ok {
		var err error
		other, err = newSetFromIterable(args[1])
		if err != nil {
			return nil, err
		}
	}
	result, err := setSymDiff(s, s, other)
	if err != nil {
		return nil, err
	}
	s.entries = result.entries
	s.used = result.used
	return None(), nil
}

func setCopy(s *Set) *Set {
	out := newEmptyLike(s)
	for _, e := range s.entries {
		if e.used {
			// Keys already in the set are guaranteed hashable; ignore error.
			_ = out.insert(e.hash, e.key)
		}
	}
	return out
}

// setReduceMethod ports set___reduce___impl: returns (type, ([elements],), state).
// Both set and frozenset use this so pickle and copy.deepcopy work correctly.
//
// CPython: Objects/setobject.c:2397 set___reduce___impl
func setReduceMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments")
	}
	s := args[0].(*Set)
	// Collect elements as a list.
	elems := make([]Object, 0, s.used)
	for _, e := range s.entries {
		if e.used {
			elems = append(elems, e.key)
		}
	}
	lst := NewList(elems)
	// state: per-instance dict (or None for subclasses without custom attrs).
	state := None()
	if d := s.AttrDict(); d != nil && d.Len() > 0 {
		state = d
	}
	// (type, ([elements],), state)
	return NewTuple([]Object{s.Type(), NewTuple([]Object{lst}), state}), nil
}

func setCopyMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: copy() takes no arguments")
	}
	s := args[0].(*Set)
	// CPython: Objects/setobject.c:1982 set_copy_impl — frozensets are
	// immutable so copy() returns self unchanged.
	if s.frozen && s.Type() == FrozensetType {
		return s, nil
	}
	return setCopy(s), nil
}
