// WeakKeyDictionary ports Lib/weakref.py:298. Keys are weakly held,
// values are strong. CPython stores the table as `dict[ref(key)] =
// value` and lets ref-equality (which hashes by referent) handle key
// lookups; the _remove callback fires when a key dies and `del
// self.data[wr]` removes the slot.
//
// gopy keeps the same shape but uses an internal map keyed by
// *objects.Weakref so the callback can locate its row in O(1) without
// reimplementing weakref hashing.
//
// CPython: Lib/weakref.py:298 WeakKeyDictionary

package weakref

import (
	"fmt"
	"sync"

	"github.com/tamnd/gopy/gc"
	"github.com/tamnd/gopy/objects"
)

// WeakKeyDict is the Go-side WeakKeyDictionary object.
//
// CPython: Lib/weakref.py:298 WeakKeyDictionary
type WeakKeyDict struct {
	objects.Header

	mu      sync.Mutex
	data    map[*objects.Weakref]objects.Object
	remover *objects.BuiltinFunction
}

// WeakKeyDictType is the type singleton for weakref.WeakKeyDictionary.
//
// CPython: Lib/weakref.py:298 WeakKeyDictionary (class object)
var WeakKeyDictType = objects.NewType("WeakKeyDictionary", []*objects.Type{objects.ObjectType()})

func init() {
	WeakKeyDictType.Repr = weakKeyDictRepr
	WeakKeyDictType.Str = weakKeyDictRepr
	WeakKeyDictType.Iter = weakKeyDictIter
	WeakKeyDictType.Mapping = &objects.MappingMethods{
		Length:  weakKeyDictLen,
		GetItem: weakKeyDictGetItem,
		SetItem: weakKeyDictSetItem,
		DelItem: weakKeyDictDelItem,
	}
	WeakKeyDictType.TpTraverse = weakKeyDictTraverse
}

// NewWeakKeyDict builds an empty WeakKeyDictionary. Pass a *Dict (or
// nil) to seed via update().
//
// CPython: Lib/weakref.py:309 WeakKeyDictionary.__init__
func NewWeakKeyDict(other objects.Object) (*WeakKeyDict, error) {
	d := &WeakKeyDict{data: make(map[*objects.Weakref]objects.Object)}
	d.Init(WeakKeyDictType)
	d.remover = objects.NewBuiltinFunction("_weakkeydict_remove", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 1 {
			return objects.None(), nil
		}
		wr, ok := args[0].(*objects.Weakref)
		if !ok {
			return objects.None(), nil
		}
		d.mu.Lock()
		delete(d.data, wr)
		d.mu.Unlock()
		return objects.None(), nil
	})
	if other != nil {
		if err := d.Update(other); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// Len returns the entry count.
//
// CPython: Lib/weakref.py:328 WeakKeyDictionary.__len__
func (d *WeakKeyDict) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.data)
}

// SetItem inserts or replaces key->value, dedup-ing by Python equality
// against existing live keys.
//
// CPython: Lib/weakref.py:334 WeakKeyDictionary.__setitem__
func (d *WeakKeyDict) SetItem(key, value objects.Object) error {
	wr, err := d.findRef(key)
	if err != nil {
		return err
	}
	if wr != nil {
		d.mu.Lock()
		d.data[wr] = value
		d.mu.Unlock()
		return nil
	}
	newWr := objects.NewWeakref(key, d.remover)
	gc.RegisterWeakref(newWr)
	d.mu.Lock()
	d.data[newWr] = value
	d.mu.Unlock()
	return nil
}

// GetItem returns the value for key, or KeyError.
//
// CPython: Lib/weakref.py:325 WeakKeyDictionary.__getitem__
func (d *WeakKeyDict) GetItem(key objects.Object) (objects.Object, error) {
	wr, err := d.findRef(key)
	if err != nil {
		return nil, err
	}
	if wr == nil {
		return nil, fmt.Errorf("KeyError: %v", key)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	v, ok := d.data[wr]
	if !ok {
		return nil, fmt.Errorf("KeyError: %v", key)
	}
	return v, nil
}

// DelItem removes the entry for key.
//
// CPython: Lib/weakref.py:322 WeakKeyDictionary.__delitem__
func (d *WeakKeyDict) DelItem(key objects.Object) error {
	wr, err := d.findRef(key)
	if err != nil {
		return err
	}
	if wr == nil {
		return fmt.Errorf("KeyError: %v", key)
	}
	d.mu.Lock()
	delete(d.data, wr)
	d.mu.Unlock()
	return nil
}

// Contains reports whether key resolves to an entry.
//
// CPython: Lib/weakref.py:359 WeakKeyDictionary.__contains__
func (d *WeakKeyDict) Contains(key objects.Object) (bool, error) {
	wr, err := d.findRef(key)
	if err != nil {
		return false, err
	}
	return wr != nil, nil
}

// Get returns the value for key or default.
//
// CPython: Lib/weakref.py:356 WeakKeyDictionary.get
func (d *WeakKeyDict) Get(key, def objects.Object) objects.Object {
	v, err := d.GetItem(key)
	if err != nil {
		return def
	}
	return v
}

// Keys returns the live keys in non-deterministic order.
//
// CPython: Lib/weakref.py:372 WeakKeyDictionary.keys
func (d *WeakKeyDict) Keys() []objects.Object {
	d.mu.Lock()
	refs := d.snapshotLocked()
	d.mu.Unlock()
	out := make([]objects.Object, 0, len(refs))
	for _, wr := range refs {
		if r := wr.Referent(); r != objects.None() {
			out = append(out, r)
		}
	}
	return out
}

// Values returns a snapshot of values whose keys are still live.
//
// CPython: Lib/weakref.py:380 WeakKeyDictionary.values
func (d *WeakKeyDict) Values() []objects.Object {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]objects.Object, 0, len(d.data))
	for wr, v := range d.data {
		if wr.Referent() != objects.None() {
			out = append(out, v)
		}
	}
	return out
}

// Items returns (key, value) snapshots for live entries.
//
// CPython: Lib/weakref.py:366 WeakKeyDictionary.items
func (d *WeakKeyDict) Items() [][2]objects.Object {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([][2]objects.Object, 0, len(d.data))
	for wr, v := range d.data {
		if r := wr.Referent(); r != objects.None() {
			out = append(out, [2]objects.Object{r, v})
		}
	}
	return out
}

// Update copies entries from a *objects.Dict or another WeakKeyDict.
//
// CPython: Lib/weakref.py:410 WeakKeyDictionary.update
func (d *WeakKeyDict) Update(other objects.Object) error {
	if od, ok := other.(*objects.Dict); ok {
		for _, k := range od.Keys() {
			v, err := od.GetItem(k)
			if err != nil {
				return err
			}
			if err := d.SetItem(k, v); err != nil {
				return err
			}
		}
		return nil
	}
	if owd, ok := other.(*WeakKeyDict); ok {
		for _, kv := range owd.Items() {
			if err := d.SetItem(kv[0], kv[1]); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("TypeError: WeakKeyDictionary.update requires a mapping")
}

// findRef walks the live entries looking for a key Python-equal to
// key. Returns nil weakref when no match is found.
func (d *WeakKeyDict) findRef(key objects.Object) (*objects.Weakref, error) {
	d.mu.Lock()
	refs := d.snapshotLocked()
	d.mu.Unlock()
	for _, wr := range refs {
		r := wr.Referent()
		if r == objects.None() {
			continue
		}
		eq, err := objects.RichCmpBool(r, key, objects.CompareEQ)
		if err != nil {
			return nil, err
		}
		if eq {
			return wr, nil
		}
	}
	return nil, nil
}

func (d *WeakKeyDict) snapshotLocked() []*objects.Weakref {
	out := make([]*objects.Weakref, 0, len(d.data))
	for wr := range d.data {
		out = append(out, wr)
	}
	return out
}

func weakKeyDictLen(o objects.Object) (int, error) {
	return o.(*WeakKeyDict).Len(), nil
}

func weakKeyDictGetItem(o, key objects.Object) (objects.Object, error) {
	return o.(*WeakKeyDict).GetItem(key)
}

func weakKeyDictSetItem(o, key, value objects.Object) error {
	return o.(*WeakKeyDict).SetItem(key, value)
}

func weakKeyDictDelItem(o, key objects.Object) error {
	return o.(*WeakKeyDict).DelItem(key)
}

// weakKeyDictTraverse visits the values strongly and the weakref keys.
// Keys are NOT visited as referents — that is what gives the container
// its weak-key semantics for the cycle collector.
//
// CPython: Lib/weakref.py — dict_traverse over a dict whose keys are
// PyWeakReferences
func weakKeyDictTraverse(o objects.Object, visit objects.Visitor) error {
	d := o.(*WeakKeyDict)
	d.mu.Lock()
	defer d.mu.Unlock()
	for wr, v := range d.data {
		if err := visit(wr); err != nil {
			return err
		}
		if v == nil {
			continue
		}
		if err := visit(v); err != nil {
			return err
		}
	}
	return nil
}

func weakKeyDictRepr(o objects.Object) (string, error) {
	d := o.(*WeakKeyDict)
	return fmt.Sprintf("<WeakKeyDictionary at %p>", d), nil
}

// weakKeyDictIterator yields a snapshot of live keys.
//
// CPython: Lib/weakref.py:378 WeakKeyDictionary.__iter__ = keys
type weakKeyDictIterator struct {
	objects.Header
	keys []objects.Object
	pos  int
}

var weakKeyDictIterType = objects.NewType("WeakKeyDictionary_keyiterator", []*objects.Type{objects.ObjectType()})

func init() {
	weakKeyDictIterType.Iter = func(o objects.Object) (objects.Object, error) { return o, nil }
	weakKeyDictIterType.IterNext = func(o objects.Object) (objects.Object, error) {
		it := o.(*weakKeyDictIterator)
		if it.pos >= len(it.keys) {
			return nil, objects.ErrStopIteration
		}
		v := it.keys[it.pos]
		it.pos++
		return v, nil
	}
}

func weakKeyDictIter(o objects.Object) (objects.Object, error) {
	d := o.(*WeakKeyDict)
	it := &weakKeyDictIterator{keys: d.Keys()}
	it.Init(weakKeyDictIterType)
	return it, nil
}
