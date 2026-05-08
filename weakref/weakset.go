// WeakSet ports Lib/_weakrefset.py. The underlying storage is a map
// of *objects.Weakref to satisfy the weak-membership semantics: items
// die naturally even while the WeakSet is alive, and the collector's
// weakref-callback path (gc.handleWeakrefs) drops the cleared weakref
// from the map.
//
// CPython: Lib/_weakrefset.py:11 WeakSet

package weakref

import (
	"errors"
	"strings"
	"sync"

	"github.com/tamnd/gopy/module/gc"
	"github.com/tamnd/gopy/objects"
)

// WeakSet is the Go-side WeakSet object.
//
// CPython: Lib/_weakrefset.py:11 WeakSet
type WeakSet struct {
	objects.Header

	mu      sync.Mutex
	refs    map[*objects.Weakref]struct{}
	remover *objects.BuiltinFunction
}

// WeakSetType is the type singleton for weakref.WeakSet.
//
// CPython: Lib/_weakrefset.py:11 WeakSet (class object)
var WeakSetType = objects.NewType("WeakSet", []*objects.Type{objects.ObjectType()})

func init() {
	WeakSetType.Repr = weakSetRepr
	WeakSetType.Str = weakSetRepr
	WeakSetType.Iter = weakSetIter
	WeakSetType.Sequence = &objects.SequenceMethods{
		Length:   weakSetLen,
		Contains: weakSetContains,
	}
	WeakSetType.TpTraverse = weakSetTraverse
}

// NewWeakSet builds an empty WeakSet. data, when non-nil, is iterated
// and its members added (matching Lib/_weakrefset.py:21 WeakSet.update).
//
// CPython: Lib/_weakrefset.py:12 WeakSet.__init__
func NewWeakSet(data objects.Object) (*WeakSet, error) {
	s := &WeakSet{refs: make(map[*objects.Weakref]struct{})}
	s.Init(WeakSetType)
	s.remover = objects.NewBuiltinFunction("_weakset_remove", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 1 {
			return objects.None(), nil
		}
		wr, ok := args[0].(*objects.Weakref)
		if !ok {
			return objects.None(), nil
		}
		s.mu.Lock()
		delete(s.refs, wr)
		s.mu.Unlock()
		return objects.None(), nil
	})
	if data != nil {
		if err := s.Update(data); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Len returns the number of stored weakrefs (live or not). Mirrors
// Lib/_weakrefset.py:32 WeakSet.__len__: CPython returns len(self.data)
// without filtering dead refs because the _remove callback prunes them
// promptly.
//
// CPython: Lib/_weakrefset.py:32 WeakSet.__len__
func (s *WeakSet) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.refs)
}

// Contains reports whether item is a live member.
//
// CPython: Lib/_weakrefset.py:35 WeakSet.__contains__
func (s *WeakSet) Contains(item objects.Object) (bool, error) {
	s.mu.Lock()
	refs := s.snapshotLocked()
	s.mu.Unlock()
	for _, wr := range refs {
		r := wr.Referent()
		if r == objects.None() {
			continue
		}
		eq, err := objects.RichCmpBool(r, item, objects.CompareEQ)
		if err != nil {
			return false, err
		}
		if eq {
			return true, nil
		}
	}
	return false, nil
}

// Add inserts item, dedup-ing by Python equality against the current
// live members.
//
// CPython: Lib/_weakrefset.py:45 WeakSet.add
func (s *WeakSet) Add(item objects.Object) error {
	already, err := s.Contains(item)
	if err != nil {
		return err
	}
	if already {
		return nil
	}
	wr := objects.NewWeakref(item, s.remover)
	gc.RegisterWeakref(wr)
	s.mu.Lock()
	s.refs[wr] = struct{}{}
	s.mu.Unlock()
	return nil
}

// Discard removes item if present. Missing items are a no-op.
//
// CPython: Lib/_weakrefset.py:67 WeakSet.discard
func (s *WeakSet) Discard(item objects.Object) error {
	s.mu.Lock()
	refs := s.snapshotLocked()
	s.mu.Unlock()
	for _, wr := range refs {
		r := wr.Referent()
		if r == objects.None() {
			continue
		}
		eq, err := objects.RichCmpBool(r, item, objects.CompareEQ)
		if err != nil {
			return err
		}
		if eq {
			s.mu.Lock()
			delete(s.refs, wr)
			s.mu.Unlock()
			return nil
		}
	}
	return nil
}

// Remove deletes item, raising KeyError if absent.
//
// CPython: Lib/_weakrefset.py:64 WeakSet.remove
func (s *WeakSet) Remove(item objects.Object) error {
	present, err := s.Contains(item)
	if err != nil {
		return err
	}
	if !present {
		return errKeyNotFound
	}
	return s.Discard(item)
}

// Clear drops every member.
//
// CPython: Lib/_weakrefset.py:48 WeakSet.clear
func (s *WeakSet) Clear() {
	s.mu.Lock()
	s.refs = make(map[*objects.Weakref]struct{})
	s.mu.Unlock()
}

// Pop removes and returns one live member, or KeyError if empty.
//
// CPython: Lib/_weakrefset.py:54 WeakSet.pop
func (s *WeakSet) Pop() (objects.Object, error) {
	for {
		s.mu.Lock()
		var wr *objects.Weakref
		for r := range s.refs {
			wr = r
			break
		}
		if wr == nil {
			s.mu.Unlock()
			return nil, errKeyNotFoundPop
		}
		delete(s.refs, wr)
		s.mu.Unlock()
		if r := wr.Referent(); r != objects.None() {
			return r, nil
		}
	}
}

// Update adds every element of other.
//
// CPython: Lib/_weakrefset.py:70 WeakSet.update
func (s *WeakSet) Update(other objects.Object) error {
	it, err := objects.Iter(other)
	if err != nil {
		return err
	}
	for {
		item, err := objects.IterNext(it)
		if err != nil {
			if errors.Is(err, objects.ErrStopIteration) {
				return nil
			}
			return err
		}
		if item == nil {
			return nil
		}
		if err := s.Add(item); err != nil {
			return err
		}
	}
}

// Copy returns a fresh WeakSet with the same live members.
//
// CPython: Lib/_weakrefset.py:51 WeakSet.copy
func (s *WeakSet) Copy() (*WeakSet, error) {
	out, err := NewWeakSet(nil)
	if err != nil {
		return nil, err
	}
	for _, item := range s.live() {
		if err := out.Add(item); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// live returns a snapshot of currently live referents. Dead weakrefs
// are skipped. Order is non-deterministic, matching set semantics.
func (s *WeakSet) live() []objects.Object {
	s.mu.Lock()
	refs := s.snapshotLocked()
	s.mu.Unlock()
	out := make([]objects.Object, 0, len(refs))
	for _, wr := range refs {
		if r := wr.Referent(); r != objects.None() {
			out = append(out, r)
		}
	}
	return out
}

// snapshotLocked returns a copy of the refs slice. Callers iterate
// the copy so concurrent _remove calls don't mutate the live map
// underneath them.
func (s *WeakSet) snapshotLocked() []*objects.Weakref {
	out := make([]*objects.Weakref, 0, len(s.refs))
	for wr := range s.refs {
		out = append(out, wr)
	}
	return out
}

func weakSetLen(o objects.Object) (int, error) { return o.(*WeakSet).Len(), nil }

func weakSetContains(o, v objects.Object) (bool, error) {
	return o.(*WeakSet).Contains(v)
}

// weakSetTraverse visits the stored weakrefs, not their referents.
// That is what makes WeakSet's membership weak from the cycle
// collector's perspective.
//
// CPython: Lib/_weakrefset.py — set traversal visits the weakrefs only
func weakSetTraverse(o objects.Object, visit objects.Visitor) error {
	s := o.(*WeakSet)
	s.mu.Lock()
	refs := s.snapshotLocked()
	s.mu.Unlock()
	for _, wr := range refs {
		if err := visit(wr); err != nil {
			return err
		}
	}
	return nil
}

func weakSetRepr(o objects.Object) (string, error) {
	s := o.(*WeakSet)
	live := s.live()
	var b strings.Builder
	b.WriteString("WeakSet({")
	for i, item := range live {
		if i > 0 {
			b.WriteString(", ")
		}
		r, err := objects.Repr(item)
		if err != nil {
			return "", err
		}
		b.WriteString(r)
	}
	b.WriteString("})")
	return b.String(), nil
}

// weakSetIterator walks a snapshot of the live members. Mirrors
// CPython's "for itemref in self.data.copy(): item = itemref(); ..."
// loop in Lib/_weakrefset.py:24.
//
// CPython: Lib/_weakrefset.py:24 WeakSet.__iter__
type weakSetIterator struct {
	objects.Header
	items []objects.Object
	pos   int
}

var weakSetIterType = objects.NewType("WeakSet_iterator", []*objects.Type{objects.ObjectType()})

func init() {
	weakSetIterType.Iter = func(o objects.Object) (objects.Object, error) { return o, nil }
	weakSetIterType.IterNext = func(o objects.Object) (objects.Object, error) {
		it := o.(*weakSetIterator)
		if it.pos >= len(it.items) {
			return nil, objects.ErrStopIteration
		}
		v := it.items[it.pos]
		it.pos++
		return v, nil
	}
}

func weakSetIter(o objects.Object) (objects.Object, error) {
	s := o.(*WeakSet)
	it := &weakSetIterator{items: s.live()}
	it.Init(weakSetIterType)
	return it, nil
}
